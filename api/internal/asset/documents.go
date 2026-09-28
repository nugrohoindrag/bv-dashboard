package asset

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Equipment Documents (PRD P2 v2.1 §5.6 P2-DOC-01..03; Roadmap v2.1 §8.1, §39.2) ----------
// Dokumen bertipe (manual, warranty, certificate/izin, permit, laporan inspeksi, gambar, kontrak) dengan nomor, tanggal terbit,
// kedaluwarsa, dan file (attachment object_type asset_document). Pengingat H-30 / H-7 / kedaluwarsa (termasuk warranty
// asset) ke Engineering Supervisor + sinyal Attention Required.

var documentTypes = map[string]string{"manual": "Manual", "warranty": "Warranty", "certificate": "Sertifikat", "permit": "Izin/Permit", "inspection_report": "Laporan inspeksi",
	"drawing": "Gambar teknik", "contract": "Kontrak", "other": "Lainnya"}

type AssetDocument struct {
	ID             uuid.UUID  `json:"id"`
	PropertyID     uuid.UUID  `json:"property_id"`
	AssetID        uuid.UUID  `json:"asset_id"`
	AssetCode      string     `json:"asset_code"`
	AssetName      string     `json:"asset_name"`
	DocumentCode   string     `json:"document_code"`
	DocumentType   string     `json:"document_type"`
	TypeLabel      string     `json:"type_label"`
	Title          string     `json:"title"`
	DocumentNumber *string    `json:"document_number"`
	Issuer         *string    `json:"issuer"`
	IssuedOn       *string    `json:"issued_on"`
	ExpiresOn      *string    `json:"expires_on"`
	Status         string     `json:"status"` // valid | expiring | expired | no_expiry
	DaysToExpire   *int       `json:"days_to_expire"`
	AttachmentID   *uuid.UUID `json:"attachment_id"`
	FileURL        *string    `json:"file_url,omitempty"`
	FileName       *string    `json:"file_name"`
	Notes          *string    `json:"notes"`
	IsActive       bool       `json:"is_active"`
	CreatedAt      time.Time  `json:"created_at"`
	Version        int        `json:"version"`
}

type AssetDocumentInput struct {
	DocumentType   *string    `json:"document_type"`
	Title          *string    `json:"title"`
	DocumentNumber *string    `json:"document_number"`
	Issuer         *string    `json:"issuer"`
	IssuedOn       *string    `json:"issued_on"`
	ExpiresOn      *string    `json:"expires_on"`
	AttachmentID   *uuid.UUID `json:"attachment_id"`
	Notes          *string    `json:"notes"`
	IsActive       *bool      `json:"is_active"`
}

var docLoc = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Jakarta"); err == nil {
		return l
	}
	return time.FixedZone("WIB", 7*3600)
}()

func docStatus(expires *string) (string, *int) {
	if expires == nil {
		return "no_expiry", nil
	}
	d, err := time.Parse("2006-01-02", *expires)
	if err != nil {
		return "no_expiry", nil
	}
	// "hari ini" = tanggal lokal WIB (tanggal dokumen adalah tanggal kalender lokal; UTC salah antara 00:00–07:00 WIB)
	now := time.Now().In(docLoc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	days := int(d.Sub(today).Hours() / 24)
	switch {
	case days < 0:
		return "expired", &days
	case days <= 30:
		return "expiring", &days
	}
	return "valid", &days
}

const docSelect = `SELECT d.id, d.property_id, d.asset_id, a.asset_code, a.name, d.document_code, d.document_type, d.title, d.document_number, d.issuer, d.issued_on::text, d.expires_on::text,
	d.attachment_id, att.original_filename, d.notes, d.is_active, d.created_at, d.version
	FROM asset_documents d JOIN assets a ON a.id = d.asset_id LEFT JOIN attachments att ON att.id = d.attachment_id AND att.deleted_at IS NULL`

func scanDoc(row pgx.Row) (*AssetDocument, error) {
	var d AssetDocument
	if err := row.Scan(&d.ID, &d.PropertyID, &d.AssetID, &d.AssetCode, &d.AssetName, &d.DocumentCode, &d.DocumentType, &d.Title, &d.DocumentNumber, &d.Issuer, &d.IssuedOn, &d.ExpiresOn,
		&d.AttachmentID, &d.FileName, &d.Notes, &d.IsActive, &d.CreatedAt, &d.Version); err != nil {
		return nil, err
	}
	d.TypeLabel = documentTypes[d.DocumentType]
	d.Status, d.DaysToExpire = docStatus(d.ExpiresOn)
	return &d, nil
}

func validDocDates(in AssetDocumentInput) error {
	for f, v := range map[string]*string{"issued_on": in.IssuedOn, "expires_on": in.ExpiresOn} {
		if v != nil && *v != "" {
			if _, err := time.Parse("2006-01-02", *v); err != nil {
				return apperr.Validation(f+" harus YYYY-MM-DD").WithField(f, "format tanggal")
			}
		}
	}
	if in.IssuedOn != nil && in.ExpiresOn != nil && *in.IssuedOn != "" && *in.ExpiresOn != "" && *in.ExpiresOn < *in.IssuedOn {
		return apperr.Validation("expires_on tidak boleh sebelum issued_on").WithField("expires_on", "setelah issued_on")
	}
	return nil
}

func docPerm(ctx context.Context, tx pgx.Tx, action string, a *Asset) error {
	if authctx.Must(ctx).HasOnPropertyAt("engineering.asset_documents."+action, a.PropertyID, func() string {
		var path string
		_ = tx.QueryRow(ctx, `SELECT path::text FROM locations WHERE id = $1`, a.LocationID).Scan(&path)
		return path
	}) {
		return nil
	}
	return apperr.Forbidden("Memerlukan engineering.asset_documents." + action)
}

func nilIfEmpty(v *string) *string {
	if v == nil || *v == "" {
		return nil
	}
	return v
}

func (s *Service) ListDocuments(ctx context.Context, assetID uuid.UUID) ([]AssetDocument, error) {
	out := []AssetDocument{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := s.getTx(ctx, tx, assetID)
		if err != nil {
			return err
		}
		if err := docPerm(ctx, tx, "view", a); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, docSelect+` WHERE d.asset_id = $1 ORDER BY d.is_active DESC, d.expires_on NULLS LAST, d.created_at DESC`, assetID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDoc(rows)
			if err != nil {
				return err
			}
			out = append(out, *d)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) CreateDocument(ctx context.Context, assetID uuid.UUID, in AssetDocumentInput) (*AssetDocument, error) {
	p := authctx.Must(ctx)
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		return nil, apperr.Validation("title wajib").WithField("title", "wajib")
	}
	dt := "other"
	if in.DocumentType != nil {
		dt = *in.DocumentType
	}
	if _, ok := documentTypes[dt]; !ok {
		return nil, apperr.Validation("document_type harus manual|warranty|certificate|permit|inspection_report|drawing|contract|other").WithField("document_type", "tidak valid")
	}
	if err := validDocDates(in); err != nil {
		return nil, err
	}
	var out *AssetDocument
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := s.getTx(ctx, tx, assetID)
		if err != nil {
			return err
		}
		if err := docPerm(ctx, tx, "create", a); err != nil {
			return err
		}
		loc := property.PropertyTimezone(ctx, tx, a.PropertyID)
		code, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixAssetDocument, time.Now(), loc)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO asset_documents (organization_id, property_id, asset_id, document_code, document_type, title, document_number, issuer, issued_on, expires_on, attachment_id, notes, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::date,$10::date,$11,$12,$13,$13) RETURNING id`,
			p.OrganizationID, a.PropertyID, assetID, code, dt, strings.TrimSpace(*in.Title), in.DocumentNumber, in.Issuer, nilIfEmpty(in.IssuedOn), nilIfEmpty(in.ExpiresOn), in.AttachmentID, in.Notes, p.UserID).Scan(&id); err != nil {
			return err
		}
		if in.AttachmentID != nil {
			if err := s.linkDocAttachment(ctx, tx, id, *in.AttachmentID); err != nil {
				return err
			}
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: assetID, Action: "document_added", Payload: map[string]any{"document_code": code, "document_type": dt, "title": *in.Title, "expires_on": in.ExpiresOn}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "asset_document", EntityID: &id, EntityLabel: code + " " + *in.Title})
		out, err = scanDoc(tx.QueryRow(ctx, docSelect+` WHERE d.id = $1`, id))
		return err
	})
	return out, err
}

// linkDocAttachment: file dokumen diunggah dengan object_type asset (sebelum dokumen ada) atau asset_document; dipindahkan ke dokumen.
func (s *Service) linkDocAttachment(ctx context.Context, tx pgx.Tx, docID, attID uuid.UUID) error {
	var ot string
	if err := tx.QueryRow(ctx, `SELECT object_type FROM attachments WHERE id = $1 AND deleted_at IS NULL`, attID).Scan(&ot); err != nil {
		return apperr.Validation("attachment_id tidak ditemukan").WithField("attachment_id", "tidak valid")
	}
	if ot != "asset" && ot != "asset_document" {
		return apperr.Validation("attachment_id harus file dokumen asset").WithField("attachment_id", "tidak valid")
	}
	_, err := tx.Exec(ctx, `UPDATE attachments SET object_type = 'asset_document', object_id = $2 WHERE id = $1`, attID, docID)
	return err
}

func (s *Service) UpdateDocument(ctx context.Context, docID uuid.UUID, in AssetDocumentInput, ifVersion *int) (*AssetDocument, error) {
	p := authctx.Must(ctx)
	if in.DocumentType != nil {
		if _, ok := documentTypes[*in.DocumentType]; !ok {
			return nil, apperr.Validation("document_type tidak valid").WithField("document_type", "tidak valid")
		}
	}
	if err := validDocDates(in); err != nil {
		return nil, err
	}
	var out *AssetDocument
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		d, err := scanDoc(tx.QueryRow(ctx, docSelect+` WHERE d.id = $1`, docID))
		if err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Dokumen")
			}
			return err
		}
		a, err := s.getTx(ctx, tx, d.AssetID)
		if err != nil {
			return err
		}
		if err := docPerm(ctx, tx, "update", a); err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != d.Version {
			return apperr.StaleVersion()
		}
		// string kosong mengosongkan field teks/tanggal; UUID nol melepas file
		if _, err := tx.Exec(ctx, `UPDATE asset_documents SET document_type = COALESCE($2, document_type), title = COALESCE(NULLIF(TRIM($3),''), title), document_number = CASE WHEN $4::text IS NULL THEN document_number ELSE NULLIF(btrim($4::text), '') END,
			issuer = CASE WHEN $5::text IS NULL THEN issuer ELSE NULLIF(btrim($5::text), '') END, issued_on = CASE WHEN $6::text IS NULL THEN issued_on WHEN $6::text = '' THEN NULL ELSE $6::date END, expires_on = CASE WHEN $7::text IS NULL THEN expires_on WHEN $7::text = '' THEN NULL ELSE $7::date END, attachment_id = CASE WHEN $8::uuid IS NULL THEN attachment_id WHEN $8::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL ELSE $8::uuid END, notes = CASE WHEN $9::text IS NULL THEN notes ELSE NULLIF(btrim($9::text), '') END,
			is_active = COALESCE($10, is_active), reminder_stage = CASE WHEN $7::text IS NOT NULL THEN 0 ELSE reminder_stage END, updated_by = $11 WHERE id = $1`,
			docID, in.DocumentType, derefStr(in.Title), in.DocumentNumber, in.Issuer, in.IssuedOn, in.ExpiresOn, in.AttachmentID, in.Notes, in.IsActive, p.UserID); err != nil {
			return err
		}
		if in.AttachmentID != nil && *in.AttachmentID != uuid.Nil {
			if err := s.linkDocAttachment(ctx, tx, docID, *in.AttachmentID); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "asset_document", EntityID: &docID, EntityLabel: d.DocumentCode, Before: d, After: in})
		out, err = scanDoc(tx.QueryRow(ctx, docSelect+` WHERE d.id = $1`, docID))
		return err
	})
	return out, err
}

func (s *Service) DeleteDocument(ctx context.Context, docID uuid.UUID) error {
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		d, err := scanDoc(tx.QueryRow(ctx, docSelect+` WHERE d.id = $1`, docID))
		if err != nil {
			return apperr.NotFound("Dokumen")
		}
		a, err := s.getTx(ctx, tx, d.AssetID)
		if err != nil {
			return err
		}
		if err := docPerm(ctx, tx, "delete", a); err != nil {
			return err
		}
		// soft: nonaktifkan agar riwayat & file tetap tertelusur
		if _, err := tx.Exec(ctx, `UPDATE asset_documents SET is_active = false WHERE id = $1`, docID); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: d.AssetID, Action: "document_removed", Payload: map[string]any{"document_code": d.DocumentCode}})
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "asset_document", EntityID: &docID, EntityLabel: d.DocumentCode})
	})
}

// ExpiringDocuments: dokumen (aktif) + warranty asset yang kedaluwarsa ≤ N hari (P2-DOC-02) — daftar Engineering.
type ExpiringItem struct {
	Kind         string     `json:"kind"` // document | warranty
	ID           uuid.UUID  `json:"id"`
	AssetID      uuid.UUID  `json:"asset_id"`
	AssetCode    string     `json:"asset_code"`
	AssetName    string     `json:"asset_name"`
	Title        string     `json:"title"`
	DocumentType *string    `json:"document_type"`
	ExpiresOn    string     `json:"expires_on"`
	Status       string     `json:"status"`
	DaysToExpire *int       `json:"days_to_expire"`
	DeepLink     string     `json:"deep_link"`
	PropertyID   uuid.UUID  `json:"property_id"`
	LocationID   *uuid.UUID `json:"-"`
}

func (s *Service) ExpiringDocuments(ctx context.Context, propertyID *uuid.UUID, withinDays int) ([]ExpiringItem, error) {
	p := authctx.Must(ctx)
	if withinDays <= 0 {
		withinDays = 30
	}
	out := []ExpiringItem{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{withinDays}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := ""
		if propertyID != nil {
			if !p.HasAnyOnProperty("engineering.asset_documents.view", *propertyID) {
				return apperr.Forbidden("")
			}
			where += " AND a.property_id = " + add(*propertyID)
		}
		where += " AND " + p.ScopeSQL("engineering.asset_documents.view", "a.property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = a.location_id)", add)
		rows, err := tx.Query(ctx, `
			SELECT 'document', d.id, a.id, a.asset_code, a.name, d.title, d.document_type, d.expires_on::text, a.property_id
			FROM asset_documents d JOIN assets a ON a.id = d.asset_id WHERE d.is_active AND a.deleted_at IS NULL AND d.expires_on IS NOT NULL AND d.expires_on <= CURRENT_DATE + $1::int`+where+`
			UNION ALL
			SELECT 'warranty', a.id, a.id, a.asset_code, a.name, 'Warranty ' || a.name, 'warranty', a.warranty_until::date::text, a.property_id
			FROM assets a WHERE a.deleted_at IS NULL AND a.status <> 'decommissioned' AND a.warranty_until IS NOT NULL AND a.warranty_until::date <= CURRENT_DATE + $1::int`+where+`
			ORDER BY 8 LIMIT 500`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it ExpiringItem
			if err := rows.Scan(&it.Kind, &it.ID, &it.AssetID, &it.AssetCode, &it.AssetName, &it.Title, &it.DocumentType, &it.ExpiresOn, &it.PropertyID); err != nil {
				return err
			}
			it.Status, it.DaysToExpire = docStatus(&it.ExpiresOn)
			it.DeepLink = "/assets/" + it.AssetID.String() + "?tab=documents"
			out = append(out, it)
		}
		return rows.Err()
	})
	return out, err
}

// DocumentSweep (P2-DOC-02): pengingat H-30 / H-7 / kedaluwarsa dokumen & warranty → Engineering Supervisor.
func (s *Service) DocumentSweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	n := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		type due struct {
			kind                string
			id, assetID, propID uuid.UUID
			label, expires      string
			stage               int
		}
		var list []due
		rows, err := tx.Query(ctx, `SELECT 'document', d.id, a.id, a.property_id, a.asset_code || ' · ' || d.title, d.expires_on::text, d.reminder_stage
			FROM asset_documents d JOIN assets a ON a.id = d.asset_id WHERE d.is_active AND a.deleted_at IS NULL AND d.expires_on IS NOT NULL AND d.expires_on <= CURRENT_DATE + 30 AND d.reminder_stage < 3
			UNION ALL
			SELECT 'warranty', a.id, a.id, a.property_id, a.asset_code || ' · Warranty', a.warranty_until::date::text, a.warranty_reminder_stage
			FROM assets a WHERE a.deleted_at IS NULL AND a.status <> 'decommissioned' AND a.warranty_until IS NOT NULL AND a.warranty_until::date <= CURRENT_DATE + 30 AND a.warranty_reminder_stage < 3`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.kind, &d.id, &d.assetID, &d.propID, &d.label, &d.expires, &d.stage); err != nil {
				rows.Close()
				return err
			}
			list = append(list, d)
		}
		rows.Close()
		for _, d := range list {
			status, days := docStatus(&d.expires)
			stage, verb := 1, "expiring"
			switch {
			case status == "expired":
				stage, verb = 3, "expired"
			case days != nil && *days <= 7:
				stage = 2
			}
			if stage <= d.stage {
				continue
			}
			if d.kind == "document" {
				_, err = tx.Exec(ctx, `UPDATE asset_documents SET reminder_stage = $2, last_reminded_at = now() WHERE id = $1`, d.id, stage)
			} else {
				_, err = tx.Exec(ctx, `UPDATE assets SET warranty_reminder_stage = $2 WHERE id = $1`, d.id, stage)
			}
			if err != nil {
				return err
			}
			if s.Jobs != nil {
				pid := d.propID
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "asset_document." + verb, OrganizationID: orgID, PropertyID: &pid, ObjectType: "asset", ObjectID: d.assetID,
					ObjectLabel: d.label, Payload: map[string]any{"domain": "engineering", "kind": d.kind, "document_id": d.id, "expires_on": d.expires, "days_to_expire": days, "reason": "Berlaku s/d " + d.expires}})
			}
			n++
		}
		return nil
	})
	return n, err
}

// DocumentAccess: validator lampiran object_type asset_document (file dokumen equipment / warranty).
// Terdaftar di operations.RegisterObjectAccess (lihat app/extensions.go).
func (s *Service) DocumentAccess(ctx context.Context, tx pgx.Tx, docID uuid.UUID, write bool) error {
	var assetID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT asset_id FROM asset_documents WHERE id = $1`, docID).Scan(&assetID); err != nil {
		return apperr.NotFound("Dokumen asset")
	}
	a, err := s.getTx(ctx, tx, assetID)
	if err != nil {
		return err
	}
	if authctx.Must(ctx).VendorID != nil {
		return apperr.Forbidden("")
	}
	if write {
		return docPerm(ctx, tx, "update", a)
	}
	return docPerm(ctx, tx, "view", a)
}
