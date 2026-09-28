// Package parcels: Package (paket masuk) — PRD P3 v2.1 §5.9 P3-PKG-01..04 (Roadmap v2.1 §11, §39.2). Resepsionis/security
// mencatat paket (penerima unit/tenant, kurir, resi, foto, lokasi simpan) → tenant dinotifikasi → serah terima (penerima,
// waktu, foto/tanda tangan) atau retur. Status: received → notified → picked_up | returned. Pengingat bila belum diambil.
// Object API/DB: `package` (nama Go `parcels` karena `package` kata kunci).
package parcels

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantscope"
)

type Service struct {
	DB          *db.DB
	Jobs        jobs.Enqueuer
	Attachments *attachments.Service
}

func New(d *db.DB, j jobs.Enqueuer, att *attachments.Service) *Service {
	return &Service{DB: d, Jobs: j, Attachments: att}
}

var packageTypes = map[string]bool{"document": true, "parcel": true, "food": true, "large": true, "other": true}

type Photo struct {
	ID             uuid.UUID `json:"id"`
	AttachmentType string    `json:"attachment_type"`
	URL            string    `json:"url,omitempty"`
	ThumbURL       string    `json:"thumb_url,omitempty"`
}

type Package struct {
	ID               uuid.UUID  `json:"id"`
	PackageNumber    string     `json:"package_number"`
	PropertyID       uuid.UUID  `json:"property_id"`
	UnitLocationID   *uuid.UUID `json:"unit_location_id"`
	UnitName         *string    `json:"unit_name"`
	TenantID         *uuid.UUID `json:"tenant_id"`
	TenantName       *string    `json:"tenant_name"`
	RecipientUserID  *uuid.UUID `json:"recipient_user_id"`
	RecipientName    string     `json:"recipient_name"`
	PackageType      string     `json:"package_type"`
	Courier          *string    `json:"courier"`
	TrackingNumber   *string    `json:"tracking_number"`
	Description      *string    `json:"description"`
	StorageLocation  *string    `json:"storage_location"`
	Status           string     `json:"status"`
	ReceivedAt       time.Time  `json:"received_at"`
	ReceivedByName   *string    `json:"received_by_name"`
	NotifiedAt       *time.Time `json:"notified_at"`
	ReminderCount    int        `json:"reminder_count"`
	LastRemindedAt   *time.Time `json:"last_reminded_at"`
	PickedUpAt       *time.Time `json:"picked_up_at"`
	PickedUpByName   *string    `json:"picked_up_by_name"`
	HandedOverByName *string    `json:"handed_over_by_name"`
	HandoverNote     *string    `json:"handover_note"`
	ReturnedAt       *time.Time `json:"returned_at"`
	ReturnReason     *string    `json:"return_reason"`
	DaysWaiting      int        `json:"days_waiting"`
	Photos           []Photo    `json:"photos"`
	AllowedActions   []string   `json:"allowed_actions"`
	Version          int        `json:"version"`
}

const pkgSelect = `SELECT pk.id, pk.package_number, pk.property_id, pk.unit_location_id, COALESCE('Unit ' || un.unit_number, ul.name), pk.tenant_id, t.name, pk.recipient_user_id, pk.recipient_name,
	pk.package_type, pk.courier, pk.tracking_number, pk.description, pk.storage_location, pk.status, pk.received_at, rb.full_name, pk.notified_at, pk.reminder_count, pk.last_reminded_at,
	pk.picked_up_at, pk.picked_up_by_name, hb.full_name, pk.handover_note, pk.returned_at, pk.return_reason,
	GREATEST(0, EXTRACT(DAY FROM (COALESCE(pk.picked_up_at, pk.returned_at, now()) - pk.received_at)))::int, pk.version
	FROM packages pk LEFT JOIN locations ul ON ul.id = pk.unit_location_id LEFT JOIN units un ON un.location_id = pk.unit_location_id LEFT JOIN tenants t ON t.id = pk.tenant_id
	LEFT JOIN users rb ON rb.id = pk.received_by LEFT JOIN users hb ON hb.id = pk.handed_over_by`

func scan(row pgx.Row) (*Package, error) {
	var p Package
	if err := row.Scan(&p.ID, &p.PackageNumber, &p.PropertyID, &p.UnitLocationID, &p.UnitName, &p.TenantID, &p.TenantName, &p.RecipientUserID, &p.RecipientName,
		&p.PackageType, &p.Courier, &p.TrackingNumber, &p.Description, &p.StorageLocation, &p.Status, &p.ReceivedAt, &p.ReceivedByName, &p.NotifiedAt, &p.ReminderCount, &p.LastRemindedAt,
		&p.PickedUpAt, &p.PickedUpByName, &p.HandedOverByName, &p.HandoverNote, &p.ReturnedAt, &p.ReturnReason, &p.DaysWaiting, &p.Version); err != nil {
		return nil, err
	}
	p.Photos = []Photo{}
	return &p, nil
}

func (s *Service) loadPhotos(ctx context.Context, tx pgx.Tx, p *Package) {
	list, err := attachments.ListForObjectTx(ctx, tx, "package", p.ID)
	if err != nil || len(list) == 0 {
		return
	}
	if s.Attachments != nil {
		s.Attachments.FillURLs(ctx, list)
	}
	for _, a := range list {
		p.Photos = append(p.Photos, Photo{ID: a.ID, AttachmentType: a.AttachmentType, URL: a.URL, ThumbURL: a.ThumbURL})
	}
}

func perm(ctx context.Context, action string, propertyID uuid.UUID) error {
	if authctx.Must(ctx).HasAnyOnProperty("security.packages."+action, propertyID) {
		return nil
	}
	return apperr.Forbidden("Memerlukan security.packages." + action)
}

func (s *Service) actions(ctx context.Context, pk *Package) {
	p := authctx.Must(ctx)
	pk.AllowedActions = []string{"view"}
	if p.IsTenant {
		return
	}
	can := func(a string) bool { return p.HasAnyOnProperty("security.packages."+a, pk.PropertyID) }
	switch pk.Status {
	case "received", "notified":
		if can("handover") {
			pk.AllowedActions = append(pk.AllowedActions, "pickup", "notify")
		}
		if can("record") {
			pk.AllowedActions = append(pk.AllowedActions, "update")
		}
		if can("manage") || can("handover") {
			pk.AllowedActions = append(pk.AllowedActions, "return")
		}
	}
}

type Filter struct {
	PropertyID *uuid.UUID
	Statuses   []string
	UnitID     *uuid.UUID
	TenantID   *uuid.UUID
	Waiting    bool // received | notified
	Q          string
}

func (s *Service) List(ctx context.Context, f Filter, page httpx.Page) ([]Package, *string, error) {
	p := authctx.Must(ctx)
	var out []Package
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if f.PropertyID != nil {
			if err := perm(ctx, "view", *f.PropertyID); err != nil {
				return err
			}
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND pk.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("security.packages.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND pk.property_id = ANY($%d)", len(args))
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND pk.status = ANY($%d)", len(args))
		}
		if f.Waiting {
			where += " AND pk.status IN ('received','notified')"
		}
		if f.UnitID != nil {
			args = append(args, *f.UnitID)
			where += fmt.Sprintf(" AND pk.unit_location_id = $%d", len(args))
		}
		if f.TenantID != nil {
			args = append(args, *f.TenantID)
			where += fmt.Sprintf(" AND pk.tenant_id = $%d", len(args))
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (pk.package_number ILIKE $%d OR pk.recipient_name ILIKE $%d OR pk.tracking_number ILIKE $%d OR pk.courier ILIKE $%d OR un.unit_number ILIKE $%d)", len(args), len(args), len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (pk.received_at, pk.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, pkgSelect+where+fmt.Sprintf(" ORDER BY pk.received_at DESC, pk.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			pk, err := scan(rows)
			if err != nil {
				return err
			}
			s.actions(ctx, pk)
			out = append(out, *pk)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.ReceivedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	if out == nil {
		out = []Package{}
	}
	return out, next, err
}

func (s *Service) getTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Package, error) {
	pk, err := scan(tx.QueryRow(ctx, pkgSelect+` WHERE pk.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Paket")
		}
		return nil, err
	}
	return pk, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Package, error) {
	var out *Package
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pk, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := perm(ctx, "view", pk.PropertyID); err != nil {
			return err
		}
		s.loadPhotos(ctx, tx, pk)
		s.actions(ctx, pk)
		out = pk
		return nil
	})
	return out, err
}

type Input struct {
	PropertyID      *uuid.UUID `json:"property_id"`
	UnitLocationID  *uuid.UUID `json:"unit_location_id"`
	TenantID        *uuid.UUID `json:"tenant_id"`
	RecipientUserID *uuid.UUID `json:"recipient_user_id"`
	RecipientName   *string    `json:"recipient_name"`
	PackageType     *string    `json:"package_type"`
	Courier         *string    `json:"courier"`
	TrackingNumber  *string    `json:"tracking_number"`
	Description     *string    `json:"description"`
	StorageLocation *string    `json:"storage_location"`
	ReceivedAt      *time.Time `json:"received_at"`
	Notify          *bool      `json:"notify"` // default true: tenant langsung diberi tahu (P3-PKG-02)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// Create: catat paket masuk (P3-PKG-01). Penerima: unit (wajib bila tenant/akun tidak diisi); tenant diisi otomatis dari unit.
func (s *Service) Create(ctx context.Context, in Input) (*Package, error) {
	p := authctx.Must(ctx)
	var out *Package
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pid uuid.UUID
		switch {
		case in.UnitLocationID != nil:
			lp, err := property.ResolvePropertyOfLocation(ctx, tx, *in.UnitLocationID)
			if err != nil {
				return apperr.Validation("unit_location_id tidak ditemukan").WithField("unit_location_id", "tidak valid")
			}
			pid = lp
		case in.PropertyID != nil:
			pid = *in.PropertyID
		default:
			return apperr.Validation("unit_location_id atau property_id wajib").WithField("unit_location_id", "wajib")
		}
		if in.PropertyID != nil && *in.PropertyID != pid {
			return apperr.Validation("unit_location_id tidak berada di property ini")
		}
		if err := perm(ctx, "record", pid); err != nil {
			return err
		}
		pt := deref(in.PackageType)
		if pt == "" {
			pt = "parcel"
		}
		if !packageTypes[pt] {
			return apperr.Validation("package_type harus document|parcel|food|large|other").WithField("package_type", "tidak valid")
		}
		tenantID := in.TenantID
		if tenantID == nil && in.UnitLocationID != nil {
			_ = tx.QueryRow(ctx, `SELECT tenant_id FROM units WHERE location_id = $1`, *in.UnitLocationID).Scan(&tenantID)
		}
		if tenantID != nil {
			var tp uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT property_id FROM tenants WHERE id = $1 AND deleted_at IS NULL`, *tenantID).Scan(&tp); err != nil || tp != pid {
				return apperr.Validation("tenant_id tidak ditemukan di property ini").WithField("tenant_id", "tidak valid")
			}
		}
		name := deref(in.RecipientName)
		if in.RecipientUserID != nil {
			// akun penerima harus tenant user aktif di property ini
			var n string
			if err := tx.QueryRow(ctx, `SELECT u.full_name FROM tenant_users tu JOIN users u ON u.id = tu.user_id WHERE tu.user_id = $1 AND tu.property_id = $2 AND tu.status = 'active'`, *in.RecipientUserID, pid).Scan(&n); err != nil {
				return apperr.Validation("recipient_user_id bukan akun tenant aktif di property ini").WithField("recipient_user_id", "tidak valid")
			}
			if name == "" {
				name = n
			}
		}
		if name == "" && tenantID != nil {
			_ = tx.QueryRow(ctx, `SELECT name FROM tenants WHERE id = $1`, *tenantID).Scan(&name)
		}
		if name == "" {
			return apperr.Validation("recipient_name wajib").WithField("recipient_name", "wajib")
		}
		if in.UnitLocationID == nil && tenantID == nil && in.RecipientUserID == nil {
			return apperr.Validation("penerima (unit, tenant, atau akun) wajib")
		}
		received := time.Now().UTC()
		if in.ReceivedAt != nil {
			received = *in.ReceivedAt
		}
		number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixPackage, time.Now(), property.PropertyTimezone(ctx, tx, pid))
		if err != nil {
			return err
		}
		notify := in.Notify == nil || *in.Notify
		status := "received"
		if notify {
			status = "notified"
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO packages (organization_id, property_id, package_number, unit_location_id, tenant_id, recipient_user_id, recipient_name, package_type, courier, tracking_number, description, storage_location, status, received_at, received_by, notified_at, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),$13,$14,$15,CASE WHEN $16 THEN now() END,$15) RETURNING id`,
			p.OrganizationID, pid, number, in.UnitLocationID, tenantID, in.RecipientUserID, name, pt, deref(in.Courier), deref(in.TrackingNumber), deref(in.Description), deref(in.StorageLocation), status, received, p.UserID, notify).Scan(&id); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "package", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "courier": deref(in.Courier)}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "package", EntityID: &id, EntityLabel: number})
		if notify {
			s.emit(ctx, tx, id, number, pid, "package.received", nil)
		}
		out, err = s.getTx(ctx, tx, id)
		if err == nil {
			s.actions(ctx, out)
		}
		return err
	})
	return out, err
}

func (s *Service) emit(ctx context.Context, tx pgx.Tx, id uuid.UUID, number string, propertyID uuid.UUID, evType string, extra map[string]any) {
	if s.Jobs == nil {
		return
	}
	p := authctx.Must(ctx)
	payload := map[string]any{"domain": "security"}
	for k, v := range extra {
		payload[k] = v
	}
	var actor *uuid.UUID
	if !p.IsSystem {
		actor = &p.UserID
	}
	_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: evType, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "package", ObjectID: id, ObjectLabel: number, ActorUserID: actor, Payload: payload})
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (*Package, error) {
	p := authctx.Must(ctx)
	var out *Package
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pk, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s.actions(ctx, pk)
		if !contains(pk.AllowedActions, "update") {
			return apperr.InvalidTransition("Paket berstatus " + pk.Status + " tidak dapat diubah")
		}
		if in.PackageType != nil && !packageTypes[*in.PackageType] {
			return apperr.Validation("package_type tidak valid")
		}
		if _, err := tx.Exec(ctx, `UPDATE packages SET recipient_name = COALESCE(NULLIF(TRIM($2),''), recipient_name), package_type = COALESCE($3, package_type), courier = COALESCE($4, courier),
			tracking_number = COALESCE($5, tracking_number), description = COALESCE($6, description), storage_location = COALESCE($7, storage_location), updated_by = $8 WHERE id = $1`,
			id, deref(in.RecipientName), in.PackageType, in.Courier, in.TrackingNumber, in.Description, in.StorageLocation, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "package", EntityID: &id, EntityLabel: pk.PackageNumber, After: in})
		out, err = s.getTx(ctx, tx, id)
		if err == nil {
			s.loadPhotos(ctx, tx, out)
			s.actions(ctx, out)
		}
		return err
	})
	return out, err
}

type ActionInput struct {
	PickedUpByName   string     `json:"picked_up_by_name"`
	PickedUpByUserID *uuid.UUID `json:"picked_up_by_user_id"`
	Note             string     `json:"note"`
	Reason           string     `json:"reason"`
	// SignatureAttachmentID / foto serah terima diunggah ke object package (attachment_type signature / photo) sebelum aksi
	SignatureAttachmentID *uuid.UUID `json:"signature_attachment_id"`
}

// Act: pickup (serah terima, P3-PKG-03) | notify (kirim ulang notifikasi) | return (dikembalikan ke kurir/pengirim).
func (s *Service) Act(ctx context.Context, id uuid.UUID, action string, in ActionInput) (*Package, error) {
	p := authctx.Must(ctx)
	var out *Package
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pk, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s.actions(ctx, pk)
		if !contains(pk.AllowedActions, action) {
			return apperr.InvalidTransition("Aksi " + action + " tidak tersedia untuk paket berstatus " + pk.Status)
		}
		switch action {
		case "pickup":
			name := strings.TrimSpace(in.PickedUpByName)
			if in.PickedUpByUserID != nil {
				var n string
				if err := tx.QueryRow(ctx, `SELECT u.full_name FROM tenant_users tu JOIN users u ON u.id = tu.user_id WHERE tu.user_id = $1 AND tu.property_id = $2`, *in.PickedUpByUserID, pk.PropertyID).Scan(&n); err != nil {
					return apperr.Validation("picked_up_by_user_id tidak valid")
				}
				if name == "" {
					name = n
				}
			}
			if name == "" {
				return apperr.Validation("picked_up_by_name wajib").WithField("picked_up_by_name", "wajib")
			}
			if in.SignatureAttachmentID != nil {
				var ok bool
				_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attachments WHERE id = $1 AND object_type = 'package' AND object_id = $2 AND deleted_at IS NULL)`, *in.SignatureAttachmentID, id).Scan(&ok)
				if !ok {
					return apperr.Validation("signature_attachment_id harus lampiran paket ini")
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE packages SET status = 'picked_up', picked_up_at = now(), picked_up_by_name = $2, picked_up_by_user_id = $3, handed_over_by = $4, handover_note = NULLIF($5,''), updated_by = $4 WHERE id = $1`,
				id, name, in.PickedUpByUserID, p.UserID, strings.TrimSpace(in.Note)); err != nil {
				return err
			}
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "package", ObjectID: id, Action: "picked_up", Payload: map[string]any{"by": name, "signature": in.SignatureAttachmentID != nil}})
			s.emit(ctx, tx, id, pk.PackageNumber, pk.PropertyID, "package.picked_up", nil)
		case "notify":
			if _, err := tx.Exec(ctx, `UPDATE packages SET status = 'notified', notified_at = COALESCE(notified_at, now()), last_reminded_at = now(), reminder_count = reminder_count + CASE WHEN notified_at IS NULL THEN 0 ELSE 1 END, updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
			ev := "package.received"
			if pk.NotifiedAt != nil {
				ev = "package.reminder"
			}
			s.emit(ctx, tx, id, pk.PackageNumber, pk.PropertyID, ev, nil)
		case "return":
			reason := strings.TrimSpace(in.Reason)
			if reason == "" {
				return apperr.Validation("reason wajib").WithField("reason", "wajib")
			}
			if _, err := tx.Exec(ctx, `UPDATE packages SET status = 'returned', returned_at = now(), returned_by = $2, return_reason = $3, updated_by = $2 WHERE id = $1`, id, p.UserID, reason); err != nil {
				return err
			}
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "package", ObjectID: id, Action: "returned", Payload: map[string]any{"reason": reason}})
			s.emit(ctx, tx, id, pk.PackageNumber, pk.PropertyID, "package.returned", map[string]any{"reason": reason})
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "package", EntityID: &id, EntityLabel: pk.PackageNumber, Before: map[string]any{"status": pk.Status}, After: map[string]any{"action": action}})
		out, err = s.getTx(ctx, tx, id)
		if err == nil {
			s.loadPhotos(ctx, tx, out)
			s.actions(ctx, out)
		}
		return err
	})
	return out, err
}

// ReminderSweep (worker): paket belum diambil ≥ package_reminder_days (konfigurasi profile; 0 = nonaktif) → pengingat
// harian ke tenant, paling banyak 3 kali (P3-PKG-04).
func (s *Service) ReminderSweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	n := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE packages pk SET reminder_count = pk.reminder_count + 1, last_reminded_at = now(), status = 'notified', notified_at = COALESCE(pk.notified_at, now())
			FROM property_profile_configs c
			WHERE c.property_id = pk.property_id AND c.package_reminder_days > 0 AND pk.status IN ('received','notified') AND pk.reminder_count < 3
			  AND pk.received_at < now() - make_interval(days => c.package_reminder_days)
			  AND (pk.last_reminded_at IS NULL OR pk.last_reminded_at < now() - interval '1 day')
			RETURNING pk.id, pk.package_number, pk.property_id`)
		if err != nil {
			return err
		}
		type ref struct {
			id  uuid.UUID
			num string
			pid uuid.UUID
		}
		var list []ref
		for rows.Next() {
			var r ref
			if rows.Scan(&r.id, &r.num, &r.pid) == nil {
				list = append(list, r)
			}
		}
		rows.Close()
		for _, r := range list {
			s.emit(ctx, tx, r.id, r.num, r.pid, "package.reminder", nil)
		}
		n = len(list)
		return nil
	})
	return n, err
}

// ---------- Tenant App (P3-PKG-02) ----------

// tenantWhere: paket untuk akun ini — penerima langsung, unit yang dapat diakses, atau tenant-nya (Tenant Admin).
func tenantWhere(sc *tenantscope.Scope) (string, []any) {
	units := []uuid.UUID{}
	for _, u := range sc.Units {
		units = append(units, u.ID)
	}
	return ` WHERE pk.property_id = $1 AND (pk.recipient_user_id = $2 OR pk.unit_location_id = ANY($3) OR ($4 = 'tenant_admin' AND $5::uuid IS NOT NULL AND pk.tenant_id = $5))`,
		[]any{sc.PropertyID, sc.UserID, units, sc.Role, sc.TenantID}
}

func (s *Service) TenantList(ctx context.Context, waiting *bool, page httpx.Page) ([]Package, *string, error) {
	var out []Package
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		where, args := tenantWhere(sc)
		if waiting != nil && *waiting {
			where += " AND pk.status IN ('received','notified')"
		} else if waiting != nil {
			where += " AND pk.status IN ('picked_up','returned')"
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (pk.received_at, pk.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, pkgSelect+where+fmt.Sprintf(" ORDER BY pk.received_at DESC, pk.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			pk, err := scan(rows)
			if err != nil {
				rows.Close()
				return err
			}
			pk.AllowedActions = []string{"view"}
			pk.ReceivedByName, pk.HandedOverByName = nil, nil // nama staf tidak ditampilkan ke tenant
			out = append(out, *pk)
		}
		rows.Close()
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.ReceivedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		for i := range out {
			s.loadPhotos(ctx, tx, &out[i])
		}
		return nil
	})
	if out == nil {
		out = []Package{}
	}
	return out, next, err
}

func (s *Service) TenantGet(ctx context.Context, id uuid.UUID) (*Package, error) {
	var out *Package
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		where, args := tenantWhere(sc)
		args = append(args, id)
		pk, err := scan(tx.QueryRow(ctx, pkgSelect+where+fmt.Sprintf(" AND pk.id = $%d", len(args)), args...))
		if err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Paket")
			}
			return err
		}
		pk.AllowedActions = []string{"view"}
		pk.ReceivedByName, pk.HandedOverByName = nil, nil
		s.loadPhotos(ctx, tx, pk)
		out = pk
		return nil
	})
	return out, err
}

// Access: lampiran object package (foto paket, foto/tanda tangan serah terima) — staf security.packages.*; tenant penerima (baca).
func Access(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, write bool) error {
	p := authctx.Must(ctx)
	var propertyID uuid.UUID
	var recipient, tenantID, unitID *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT property_id, recipient_user_id, tenant_id, unit_location_id FROM packages WHERE id = $1`, objectID).Scan(&propertyID, &recipient, &tenantID, &unitID); err != nil {
		return apperr.NotFound("Paket")
	}
	if p.IsTenant {
		if write {
			return apperr.Forbidden("")
		}
		var ok bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tenant_users tu WHERE tu.user_id = $1 AND tu.status = 'active' AND ($2::uuid = tu.user_id OR ($3::uuid IS NOT NULL AND tu.tenant_id = $3)
			OR EXISTS (SELECT 1 FROM tenant_access ta WHERE ta.tenant_user_id = tu.id AND ta.location_id = $4 AND ta.status = 'active')))`, p.UserID, recipient, tenantID, unitID).Scan(&ok)
		if !ok {
			return apperr.NotFound("Paket")
		}
		return nil
	}
	act := "view"
	if write {
		act = "record"
		if !p.HasAnyOnProperty("security.packages.record", propertyID) && p.HasAnyOnProperty("security.packages.handover", propertyID) {
			act = "handover"
		}
	}
	return perm(ctx, act, propertyID)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
