package security

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Lost & Found (PRD P2 v2.1 §6.7) ----------
// Barang temuan (foto, lokasi, penemu, lokasi simpan) → pencocokan dengan laporan kehilangan → serah terima ke pemilik
// (identitas, tanda tangan, foto) atau disposal setelah masa simpan (retention). Data klaim bersifat pribadi.

// LostFoundRetentionDays: masa simpan default barang temuan sebelum boleh di-disposal.
var LostFoundRetentionDays = 90

var lostFoundCategories = map[string]bool{"electronics": true, "wallet": true, "document": true, "keys": true, "jewelry": true, "bag": true, "clothing": true, "other": true}
var disposalMethods = map[string]bool{"donated": true, "destroyed": true, "handed_to_police": true, "auctioned": true, "other": true}

type LostFoundItem struct {
	ID                    uuid.UUID              `json:"id"`
	PropertyID            uuid.UUID              `json:"property_id"`
	ItemNumber            string                 `json:"item_number"`
	Category              string                 `json:"category"`
	Description           string                 `json:"description"`
	FoundLocation         operations.LocationRef `json:"found_location"`
	FoundAt               time.Time              `json:"found_at"`
	FoundByUserID         *uuid.UUID             `json:"found_by_user_id"`
	FoundByName           *string                `json:"found_by_name"`
	FinderName            *string                `json:"finder_name"`
	StorageLocation       *string                `json:"storage_location"`
	Status                string                 `json:"status"` // stored | returned | disposed
	RetentionUntil        string                 `json:"retention_until"`
	DisposalDue           bool                   `json:"disposal_due"`
	MatchedReportID       *uuid.UUID             `json:"matched_report_id"`
	MatchedReportNumber   *string                `json:"matched_report_number"`
	ClaimantName          *string                `json:"claimant_name"`
	ClaimantContact       *string                `json:"claimant_contact"`
	ClaimantIdentity      *string                `json:"claimant_identity,omitempty"` // hanya security.lost_found.manage
	ReturnedAt            *time.Time             `json:"returned_at"`
	ReturnedByName        *string                `json:"returned_by_name"`
	SignatureAttachmentID *uuid.UUID             `json:"signature_attachment_id"`
	DisposedAt            *time.Time             `json:"disposed_at"`
	DisposalMethod        *string                `json:"disposal_method"`
	DisposalNote          *string                `json:"disposal_note"`
	AttachmentCount       int                    `json:"attachment_count"`
	AllowedActions        []string               `json:"allowed_actions"`
	CreatedAt             time.Time              `json:"created_at"`
	Version               int                    `json:"version"`
}

type LostFoundItemInput struct {
	PropertyID      *uuid.UUID `json:"property_id"`
	Category        *string    `json:"category"`
	Description     *string    `json:"description"`
	FoundLocationID *uuid.UUID `json:"found_location_id"`
	FoundAt         *time.Time `json:"found_at"`
	FinderName      *string    `json:"finder_name"`
	StorageLocation *string    `json:"storage_location"`
	RetentionDays   *int       `json:"retention_days"`
}

const lfSelect = `SELECT x.id, x.property_id, x.item_number, x.category, x.description, x.found_location_id, l.name, x.found_at, x.found_by_user_id, fu.full_name, x.finder_name, x.storage_location,
	x.status, x.retention_until::text, x.matched_report_id, lr.report_number, x.claimant_name, x.claimant_contact, x.claimant_identity, x.returned_at, ru.full_name, x.signature_attachment_id,
	x.disposed_at, x.disposal_method, x.disposal_note, (SELECT count(*) FROM attachments a WHERE a.object_type = 'lost_found_item' AND a.object_id = x.id AND a.deleted_at IS NULL), x.created_at, x.version
	FROM lost_found_items x LEFT JOIN locations l ON l.id = x.found_location_id LEFT JOIN users fu ON fu.id = x.found_by_user_id LEFT JOIN users ru ON ru.id = x.returned_by
	LEFT JOIN lost_reports lr ON lr.id = x.matched_report_id`

func (s *Service) scanLF(ctx context.Context, row pgx.Row) (*LostFoundItem, error) {
	var x LostFoundItem
	if err := row.Scan(&x.ID, &x.PropertyID, &x.ItemNumber, &x.Category, &x.Description, &x.FoundLocation.ID, &x.FoundLocation.Name, &x.FoundAt, &x.FoundByUserID, &x.FoundByName, &x.FinderName, &x.StorageLocation,
		&x.Status, &x.RetentionUntil, &x.MatchedReportID, &x.MatchedReportNumber, &x.ClaimantName, &x.ClaimantContact, &x.ClaimantIdentity, &x.ReturnedAt, &x.ReturnedByName, &x.SignatureAttachmentID,
		&x.DisposedAt, &x.DisposalMethod, &x.DisposalNote, &x.AttachmentCount, &x.CreatedAt, &x.Version); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	manage := p.HasAnyOnProperty("security.lost_found.manage", x.PropertyID)
	if !manage {
		x.ClaimantIdentity = nil
	}
	x.DisposalDue = x.Status == "stored" && x.RetentionUntil <= time.Now().Format("2006-01-02")
	x.AllowedActions = []string{"view"}
	if x.Status == "stored" && manage {
		x.AllowedActions = append(x.AllowedActions, "update", "return", "match")
		if x.DisposalDue {
			x.AllowedActions = append(x.AllowedActions, "dispose")
		}
	}
	if p.HasAnyOnProperty("operations.attachments.create", x.PropertyID) && x.Status == "stored" {
		x.AllowedActions = append(x.AllowedActions, "attach")
	}
	return &x, nil
}

func lfPerm(ctx context.Context, action string, propertyID uuid.UUID) error {
	if authctx.Must(ctx).HasAnyOnProperty("security.lost_found."+action, propertyID) {
		return nil
	}
	return apperr.Forbidden("Memerlukan security.lost_found." + action)
}

func (s *Service) getLFTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*LostFoundItem, error) {
	x, err := s.scanLF(ctx, tx.QueryRow(ctx, lfSelect+` WHERE x.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Lost & Found item")
		}
		return nil, err
	}
	if x.FoundLocation.ID != nil {
		pt := property.LocationPathText(ctx, tx, *x.FoundLocation.ID)
		x.FoundLocation.PathText = &pt
	}
	return x, nil
}

func (s *Service) CreateLostFoundItem(ctx context.Context, in LostFoundItemInput) (*LostFoundItem, error) {
	p := authctx.Must(ctx)
	if in.Description == nil || strings.TrimSpace(*in.Description) == "" {
		return nil, apperr.Validation("description wajib").WithField("description", "wajib")
	}
	if in.Category != nil && !lostFoundCategories[*in.Category] {
		return nil, apperr.Validation("category tidak valid").WithField("category", "tidak valid")
	}
	var out *LostFoundItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pid uuid.UUID
		switch {
		case in.FoundLocationID != nil:
			lp, err := property.ResolvePropertyOfLocation(ctx, tx, *in.FoundLocationID)
			if err != nil {
				return apperr.Validation("found_location_id tidak ditemukan").WithField("found_location_id", "tidak valid")
			}
			pid = lp
		case in.PropertyID != nil:
			pid = *in.PropertyID
		default:
			return apperr.Validation("property_id wajib")
		}
		if err := lfPerm(ctx, "create", pid); err != nil {
			return err
		}
		found := time.Now().UTC()
		if in.FoundAt != nil {
			found = *in.FoundAt
		}
		days := LostFoundRetentionDays
		if in.RetentionDays != nil && *in.RetentionDays > 0 {
			days = *in.RetentionDays
		}
		loc := property.PropertyTimezone(ctx, tx, pid)
		retention := found.In(loc).AddDate(0, 0, days).Format("2006-01-02")
		number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixLostFoundItem, time.Now(), loc)
		if err != nil {
			return err
		}
		cat := "other"
		if in.Category != nil {
			cat = *in.Category
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO lost_found_items (organization_id, property_id, item_number, category, description, found_location_id, found_at, found_by_user_id, finder_name, storage_location, retention_until, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::date,$8,$8) RETURNING id`,
			p.OrganizationID, pid, number, cat, strings.TrimSpace(*in.Description), in.FoundLocationID, found, actorOrNil(p), in.FinderName, in.StorageLocation, retention).Scan(&id); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "lost_found_item", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "category": cat}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "lost_found_item", EntityID: &id, EntityLabel: number})
		out, err = s.getLFTx(ctx, tx, id)
		return err
	})
	return out, err
}

// LostFoundActionInput: update | return (serah terima) | match (cocokkan laporan kehilangan) | dispose.
type LostFoundActionInput struct {
	// update
	Category        *string `json:"category"`
	Description     *string `json:"description"`
	StorageLocation *string `json:"storage_location"`
	// return
	ClaimantName          string     `json:"claimant_name"`
	ClaimantContact       *string    `json:"claimant_contact"`
	ClaimantIdentity      string     `json:"claimant_identity"`
	SignatureAttachmentID *uuid.UUID `json:"signature_attachment_id"`
	// match
	ReportID *uuid.UUID `json:"report_id"`
	// dispose
	DisposalMethod string  `json:"disposal_method"`
	DisposalNote   *string `json:"disposal_note"`
}

func (s *Service) ActLostFoundItem(ctx context.Context, id uuid.UUID, action string, in LostFoundActionInput) (*LostFoundItem, error) {
	p := authctx.Must(ctx)
	var out *LostFoundItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM lost_found_items WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		x, err := s.getLFTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := lfPerm(ctx, "view", x.PropertyID); err != nil {
			return err
		}
		if !contains(x.AllowedActions, action) {
			if x.Status != "stored" {
				return apperr.Conflict("OBJECT_TERMINAL", "Barang sudah "+x.Status)
			}
			if action == "dispose" && !x.DisposalDue {
				return apperr.Conflict("RETENTION_NOT_REACHED", "Barang baru boleh di-disposal setelah masa simpan ("+x.RetentionUntil+")")
			}
			return apperr.Forbidden("Memerlukan security.lost_found.manage")
		}
		switch action {
		case "update":
			if in.Category != nil && !lostFoundCategories[*in.Category] {
				return apperr.Validation("category tidak valid").WithField("category", "tidak valid")
			}
			if _, err := tx.Exec(ctx, `UPDATE lost_found_items SET category = COALESCE($2, category), description = COALESCE(NULLIF(TRIM($3),''), description), storage_location = COALESCE($4, storage_location), updated_by = $5 WHERE id = $1`,
				id, in.Category, deref(in.Description), in.StorageLocation, p.UserID); err != nil {
				return err
			}
		case "return":
			if strings.TrimSpace(in.ClaimantName) == "" || strings.TrimSpace(in.ClaimantIdentity) == "" {
				return apperr.Validation("claimant_name dan claimant_identity wajib untuk serah terima").WithField("claimant_identity", "wajib")
			}
			if in.SignatureAttachmentID != nil {
				var ot string
				var oid uuid.UUID
				if err := tx.QueryRow(ctx, `SELECT object_type, object_id FROM attachments WHERE id = $1 AND deleted_at IS NULL`, *in.SignatureAttachmentID).Scan(&ot, &oid); err != nil || ot != "lost_found_item" || oid != id {
					return apperr.Validation("signature_attachment_id harus diunggah ke barang ini").WithField("signature_attachment_id", "tidak valid")
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE lost_found_items SET status = 'returned', claimant_name = $2, claimant_contact = $3, claimant_identity = $4, signature_attachment_id = $5, returned_at = now(), returned_by = $6, updated_by = $6 WHERE id = $1`,
				id, strings.TrimSpace(in.ClaimantName), in.ClaimantContact, strings.TrimSpace(in.ClaimantIdentity), in.SignatureAttachmentID, p.UserID); err != nil {
				return err
			}
			if x.MatchedReportID != nil {
				_, _ = tx.Exec(ctx, `UPDATE lost_reports SET status = 'closed', updated_by = $2 WHERE id = $1 AND status IN ('open','matched')`, *x.MatchedReportID, p.UserID)
			}
		case "match":
			if in.ReportID == nil {
				return apperr.Validation("report_id wajib").WithField("report_id", "wajib")
			}
			var rpid uuid.UUID
			var rstatus string
			if err := tx.QueryRow(ctx, `SELECT property_id, status FROM lost_reports WHERE id = $1 FOR UPDATE`, *in.ReportID).Scan(&rpid, &rstatus); err != nil || rpid != x.PropertyID {
				return apperr.Validation("report_id tidak ditemukan di property ini").WithField("report_id", "tidak valid")
			}
			if rstatus != "open" {
				return apperr.Conflict("REPORT_NOT_OPEN", "Laporan kehilangan tidak dalam status open")
			}
			if _, err := tx.Exec(ctx, `UPDATE lost_found_items SET matched_report_id = $2, updated_by = $3 WHERE id = $1`, id, *in.ReportID, p.UserID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE lost_reports SET status = 'matched', matched_item_id = $2, updated_by = $3 WHERE id = $1`, *in.ReportID, id, p.UserID); err != nil {
				return err
			}
		case "dispose":
			if !disposalMethods[in.DisposalMethod] {
				return apperr.Validation("disposal_method harus donated|destroyed|handed_to_police|auctioned|other").WithField("disposal_method", "tidak valid")
			}
			if _, err := tx.Exec(ctx, `UPDATE lost_found_items SET status = 'disposed', disposed_at = now(), disposal_method = $2, disposal_note = $3, updated_by = $4 WHERE id = $1`, id, in.DisposalMethod, in.DisposalNote, p.UserID); err != nil {
				return err
			}
		default:
			return apperr.Validation("aksi tidak dikenal")
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "lost_found_item", ObjectID: id, Action: audit.ActStatusChanged, From: x.Status, To: action, Payload: map[string]any{"action": action}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "lost_found_item", EntityID: &id, EntityLabel: x.ItemNumber, After: map[string]any{"action": action, "claimant_name": in.ClaimantName}})
		out, err = s.getLFTx(ctx, tx, id)
		return err
	})
	return out, err
}

type LostFoundFilter struct {
	PropertyID  *uuid.UUID
	Statuses    []string
	Category    string
	DisposalDue bool
	Q           string
}

func (s *Service) ListLostFoundItems(ctx context.Context, f LostFoundFilter, page httpx.Page) ([]LostFoundItem, *string, error) {
	p := authctx.Must(ctx)
	out := []LostFoundItem{}
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE TRUE"
		if f.PropertyID != nil {
			if err := lfPerm(ctx, "view", *f.PropertyID); err != nil {
				return err
			}
			where += " AND x.property_id = " + add(*f.PropertyID)
		}
		where += " AND " + p.ScopeSQL("security.lost_found.view", "x.property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = x.found_location_id)", add)
		if len(f.Statuses) > 0 {
			where += " AND x.status = ANY(" + add(f.Statuses) + ")"
		}
		if f.Category != "" {
			where += " AND x.category = " + add(f.Category)
		}
		if f.DisposalDue {
			where += " AND x.status = 'stored' AND x.retention_until <= CURRENT_DATE"
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			v := add("%" + q + "%")
			where += " AND (x.item_number ILIKE " + v + " OR x.description ILIKE " + v + ")"
		}
		if page.Cursor != nil {
			where += " AND (x.found_at, x.id) < (" + add(page.Cursor.Value) + "::timestamptz, " + add(page.Cursor.ID) + ")"
		}
		rows, err := tx.Query(ctx, lfSelect+where+" ORDER BY x.found_at DESC, x.id DESC LIMIT "+add(page.Limit+1), args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			x, err := s.scanLF(ctx, rows)
			if err != nil {
				rows.Close()
				return err
			}
			out = append(out, *x)
		}
		rows.Close()
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.FoundAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return nil
	})
	return out, next, err
}

func (s *Service) GetLostFoundItem(ctx context.Context, id uuid.UUID) (*LostFoundItem, error) {
	var out *LostFoundItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getLFTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := lfPerm(ctx, "view", x.PropertyID); err != nil {
			return err
		}
		out = x
		return nil
	})
	return out, err
}

// ---------- Laporan kehilangan & pencocokan ----------

type LostReport struct {
	ID              uuid.UUID              `json:"id"`
	PropertyID      uuid.UUID              `json:"property_id"`
	ReportNumber    string                 `json:"report_number"`
	Category        string                 `json:"category"`
	Description     string                 `json:"description"`
	LostLocation    operations.LocationRef `json:"lost_location"`
	LostAt          *time.Time             `json:"lost_at"`
	ReporterName    string                 `json:"reporter_name"`
	ReporterContact *string                `json:"reporter_contact"`
	TenantID        *uuid.UUID             `json:"tenant_id"`
	TenantName      *string                `json:"tenant_name"`
	Status          string                 `json:"status"` // open | matched | closed | cancelled
	MatchedItemID   *uuid.UUID             `json:"matched_item_id"`
	MatchedItem     *string                `json:"matched_item_number"`
	CreatedAt       time.Time              `json:"created_at"`
	Version         int                    `json:"version"`
}

type LostReportInput struct {
	PropertyID      *uuid.UUID `json:"property_id"`
	Category        *string    `json:"category"`
	Description     *string    `json:"description"`
	LostLocationID  *uuid.UUID `json:"lost_location_id"`
	LostAt          *time.Time `json:"lost_at"`
	ReporterName    *string    `json:"reporter_name"`
	ReporterContact *string    `json:"reporter_contact"`
	TenantID        *uuid.UUID `json:"tenant_id"`
}

const lrSelect = `SELECT r.id, r.property_id, r.report_number, r.category, r.description, r.lost_location_id, l.name, r.lost_at, r.reporter_name, r.reporter_contact, r.tenant_id, t.name,
	r.status, r.matched_item_id, x.item_number, r.created_at, r.version
	FROM lost_reports r LEFT JOIN locations l ON l.id = r.lost_location_id LEFT JOIN tenants t ON t.id = r.tenant_id LEFT JOIN lost_found_items x ON x.id = r.matched_item_id`

func scanLR(row pgx.Row) (*LostReport, error) {
	var r LostReport
	err := row.Scan(&r.ID, &r.PropertyID, &r.ReportNumber, &r.Category, &r.Description, &r.LostLocation.ID, &r.LostLocation.Name, &r.LostAt, &r.ReporterName, &r.ReporterContact, &r.TenantID, &r.TenantName,
		&r.Status, &r.MatchedItemID, &r.MatchedItem, &r.CreatedAt, &r.Version)
	return &r, err
}

func (s *Service) CreateLostReport(ctx context.Context, in LostReportInput) (*LostReport, error) {
	p := authctx.Must(ctx)
	if in.Description == nil || strings.TrimSpace(*in.Description) == "" || in.ReporterName == nil || strings.TrimSpace(*in.ReporterName) == "" {
		return nil, apperr.Validation("description dan reporter_name wajib")
	}
	if in.Category != nil && !lostFoundCategories[*in.Category] {
		return nil, apperr.Validation("category tidak valid").WithField("category", "tidak valid")
	}
	var out *LostReport
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pid uuid.UUID
		switch {
		case in.LostLocationID != nil:
			lp, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LostLocationID)
			if err != nil {
				return apperr.Validation("lost_location_id tidak ditemukan").WithField("lost_location_id", "tidak valid")
			}
			pid = lp
		case in.PropertyID != nil:
			pid = *in.PropertyID
		default:
			return apperr.Validation("property_id wajib")
		}
		if err := lfPerm(ctx, "create", pid); err != nil {
			return err
		}
		loc := property.PropertyTimezone(ctx, tx, pid)
		number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixLostReport, time.Now(), loc)
		if err != nil {
			return err
		}
		cat := "other"
		if in.Category != nil {
			cat = *in.Category
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO lost_reports (organization_id, property_id, report_number, category, description, lost_location_id, lost_at, reporter_name, reporter_contact, tenant_id, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11) RETURNING id`,
			p.OrganizationID, pid, number, cat, strings.TrimSpace(*in.Description), in.LostLocationID, in.LostAt, strings.TrimSpace(*in.ReporterName), in.ReporterContact, in.TenantID, p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "lost_report", EntityID: &id, EntityLabel: number})
		r, err := scanLR(tx.QueryRow(ctx, lrSelect+` WHERE r.id = $1`, id))
		out = r
		return err
	})
	return out, err
}

func (s *Service) ListLostReports(ctx context.Context, propertyID *uuid.UUID, statuses []string, page httpx.Page) ([]LostReport, *string, error) {
	p := authctx.Must(ctx)
	out := []LostReport{}
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE TRUE"
		if propertyID != nil {
			if err := lfPerm(ctx, "view", *propertyID); err != nil {
				return err
			}
			where += " AND r.property_id = " + add(*propertyID)
		}
		where += " AND " + p.ScopeSQL("security.lost_found.view", "r.property_id", "", add)
		if len(statuses) > 0 {
			where += " AND r.status = ANY(" + add(statuses) + ")"
		}
		if page.Cursor != nil {
			where += " AND (r.created_at, r.id) < (" + add(page.Cursor.Value) + "::timestamptz, " + add(page.Cursor.ID) + ")"
		}
		rows, err := tx.Query(ctx, lrSelect+where+" ORDER BY r.created_at DESC, r.id DESC LIMIT "+add(page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanLR(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	return out, next, err
}

// LostFoundMatches: kandidat barang temuan untuk laporan kehilangan — kategori sama, status stored, ditemukan
// sesudah (lost_at − 1 hari), diurutkan kedekatan waktu & lokasi (P2-LNF-02).
func (s *Service) LostFoundMatches(ctx context.Context, reportID uuid.UUID) ([]LostFoundItem, error) {
	out := []LostFoundItem{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := scanLR(tx.QueryRow(ctx, lrSelect+` WHERE r.id = $1`, reportID))
		if err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Lost report")
			}
			return err
		}
		if err := lfPerm(ctx, "view", r.PropertyID); err != nil {
			return err
		}
		lostAt := r.CreatedAt
		if r.LostAt != nil {
			lostAt = *r.LostAt
		}
		rows, err := tx.Query(ctx, lfSelect+` WHERE x.property_id = $1 AND x.status = 'stored' AND x.category = $2 AND x.found_at >= $3::timestamptz - interval '1 day'
			ORDER BY (x.found_location_id IS NOT DISTINCT FROM $4) DESC, abs(EXTRACT(EPOCH FROM (x.found_at - $3::timestamptz))) LIMIT 20`, r.PropertyID, r.Category, lostAt, r.LostLocation.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := s.scanLF(ctx, rows)
			if err != nil {
				return err
			}
			out = append(out, *x)
		}
		return rows.Err()
	})
	return out, err
}
