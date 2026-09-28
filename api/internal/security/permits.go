package security

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
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantscope"
)

// ---------- Parking tenant (PRD P3 v2.1 §5.8 P3-PRK-01..03, di atas entitas P2 D-P2-06) ----------
// Tenant mendaftarkan kendaraan unitnya (plat, jenis, merek/warna, foto STNK opsional) lalu mengajukan izin/stiker parkir;
// Security/Building Management menyetujui (area, masa berlaku, nomor stiker, tarif khusus) atau menolak. Izin disetujui =
// kendaraan ber-permit (vehicles.permit_until) dan menjadi dasar tagihan parkir P4 (billing rule per_vehicle).

var permitTypes = map[string]bool{"monthly": true, "annual": true, "temporary": true}

type ParkingPermit struct {
	ID              uuid.UUID  `json:"id"`
	PermitNumber    string     `json:"permit_number"`
	PropertyID      uuid.UUID  `json:"property_id"`
	VehicleID       uuid.UUID  `json:"vehicle_id"`
	PlateNumber     string     `json:"plate_number"`
	VehicleType     string     `json:"vehicle_type"`
	VehicleLabel    string     `json:"vehicle_label"`
	TenantID        *uuid.UUID `json:"tenant_id"`
	TenantName      *string    `json:"tenant_name"`
	UnitLocationID  *uuid.UUID `json:"unit_location_id"`
	UnitName        *string    `json:"unit_name"`
	RequestedByName *string    `json:"requested_by_name"`
	ParkingAreaID   *uuid.UUID `json:"parking_area_id"`
	ParkingAreaName *string    `json:"parking_area_name"`
	PermitType      string     `json:"permit_type"`
	Status          string     `json:"status"`
	IsActive        bool       `json:"is_active"` // approved & dalam masa berlaku
	ValidFrom       *string    `json:"valid_from"`
	ValidUntil      *string    `json:"valid_until"`
	StickerNumber   *string    `json:"sticker_number"`
	FeeAmount       *int64     `json:"fee_amount"`
	Notes           *string    `json:"notes"`
	DecisionReason  *string    `json:"decision_reason"`
	DecidedByName   *string    `json:"decided_by_name"`
	DecidedAt       *time.Time `json:"decided_at"`
	RequestedAt     time.Time  `json:"requested_at"`
	AllowedActions  []string   `json:"allowed_actions"`
	Version         int        `json:"version"`
}

const permitSelect = `SELECT pp.id, pp.permit_number, pp.property_id, pp.vehicle_id, v.plate_number, v.vehicle_type, concat_ws(' ', v.brand, v.color),
	pp.tenant_id, t.name, pp.unit_location_id, COALESCE('Unit ' || un.unit_number, ul.name), rb.full_name, pp.parking_area_id, pa.name, pp.permit_type, pp.status,
	(pp.status = 'approved' AND (pp.valid_from IS NULL OR pp.valid_from <= current_date) AND (pp.valid_until IS NULL OR pp.valid_until >= current_date)),
	to_char(pp.valid_from, 'YYYY-MM-DD'), to_char(pp.valid_until, 'YYYY-MM-DD'), pp.sticker_number, pp.fee_amount, pp.notes, pp.decision_reason, db.full_name, pp.decided_at, pp.requested_at, pp.version
	FROM parking_permits pp JOIN vehicles v ON v.id = pp.vehicle_id LEFT JOIN tenants t ON t.id = pp.tenant_id LEFT JOIN locations ul ON ul.id = pp.unit_location_id
	LEFT JOIN units un ON un.location_id = pp.unit_location_id LEFT JOIN users rb ON rb.id = pp.requested_by LEFT JOIN parking_areas pa ON pa.id = pp.parking_area_id
	LEFT JOIN users db ON db.id = pp.decided_by`

func scanPermit(row pgx.Row) (*ParkingPermit, error) {
	var p ParkingPermit
	if err := row.Scan(&p.ID, &p.PermitNumber, &p.PropertyID, &p.VehicleID, &p.PlateNumber, &p.VehicleType, &p.VehicleLabel, &p.TenantID, &p.TenantName, &p.UnitLocationID, &p.UnitName,
		&p.RequestedByName, &p.ParkingAreaID, &p.ParkingAreaName, &p.PermitType, &p.Status, &p.IsActive, &p.ValidFrom, &p.ValidUntil, &p.StickerNumber, &p.FeeAmount, &p.Notes,
		&p.DecisionReason, &p.DecidedByName, &p.DecidedAt, &p.RequestedAt, &p.Version); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Service) permitActions(ctx context.Context, pm *ParkingPermit) {
	p := authctx.Must(ctx)
	pm.AllowedActions = []string{"view"}
	if p.IsTenant {
		if pm.Status == "requested" {
			pm.AllowedActions = append(pm.AllowedActions, "cancel")
		}
		return
	}
	if !p.HasAnyOnProperty("security.parking_permits.approve", pm.PropertyID) {
		return
	}
	switch pm.Status {
	case "requested":
		pm.AllowedActions = append(pm.AllowedActions, "approve", "reject")
	case "approved":
		pm.AllowedActions = append(pm.AllowedActions, "update", "revoke")
	}
}

func (s *Service) getPermitTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*ParkingPermit, error) {
	pm, err := scanPermit(tx.QueryRow(ctx, permitSelect+` WHERE pp.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Izin parkir")
		}
		return nil, err
	}
	return pm, nil
}

type PermitFilter struct {
	PropertyID *uuid.UUID
	Statuses   []string
	VehicleID  *uuid.UUID
	Q          string
}

func (s *Service) ListParkingPermits(ctx context.Context, f PermitFilter, page httpx.Page) ([]ParkingPermit, *string, error) {
	p := authctx.Must(ctx)
	var out []ParkingPermit
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("security.parking_permits.view", *f.PropertyID) {
				return apperr.Forbidden("")
			}
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND pp.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("security.parking_permits.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND pp.property_id = ANY($%d)", len(args))
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND pp.status = ANY($%d)", len(args))
		}
		if f.VehicleID != nil {
			args = append(args, *f.VehicleID)
			where += fmt.Sprintf(" AND pp.vehicle_id = $%d", len(args))
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+NormalizePlate(q)+"%", "%"+q+"%")
			where += fmt.Sprintf(" AND (v.plate_number ILIKE $%d OR pp.permit_number ILIKE $%d OR t.name ILIKE $%d)", len(args)-1, len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (pp.requested_at, pp.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, permitSelect+where+fmt.Sprintf(" ORDER BY pp.requested_at DESC, pp.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			pm, err := scanPermit(rows)
			if err != nil {
				return err
			}
			s.permitActions(ctx, pm)
			out = append(out, *pm)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.RequestedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	if out == nil {
		out = []ParkingPermit{}
	}
	return out, next, err
}

func (s *Service) GetParkingPermit(ctx context.Context, id uuid.UUID) (*ParkingPermit, error) {
	var out *ParkingPermit
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pm, err := s.getPermitTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasAnyOnProperty("security.parking_permits.view", pm.PropertyID) {
			return apperr.Forbidden("")
		}
		s.permitActions(ctx, pm)
		out = pm
		return nil
	})
	return out, err
}

type PermitDecisionInput struct {
	ParkingAreaID *uuid.UUID `json:"parking_area_id"`
	ValidFrom     *string    `json:"valid_from"`  // YYYY-MM-DD
	ValidUntil    *string    `json:"valid_until"` // YYYY-MM-DD
	StickerNumber *string    `json:"sticker_number"`
	FeeAmount     *int64     `json:"fee_amount"`
	Notes         *string    `json:"notes"`
	Reason        string     `json:"reason"`
}

func parseDay(v *string, field string) (*time.Time, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", *v)
	if err != nil {
		return nil, apperr.Validation(field+" harus YYYY-MM-DD").WithField(field, "format tidak valid")
	}
	return &t, nil
}

// ActParkingPermit: approve | reject | revoke | update (staf, security.parking_permits.approve).
func (s *Service) ActParkingPermit(ctx context.Context, id uuid.UUID, action string, in PermitDecisionInput) (*ParkingPermit, error) {
	p := authctx.Must(ctx)
	var out *ParkingPermit
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pm, err := s.getPermitTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s.permitActions(ctx, pm)
		ok := false
		for _, a := range pm.AllowedActions {
			ok = ok || a == action
		}
		if !ok {
			return apperr.InvalidTransition("Aksi " + action + " tidak tersedia untuk izin parkir berstatus " + pm.Status)
		}
		reason := strings.TrimSpace(in.Reason)
		evVerb := ""
		switch action {
		case "approve", "update":
			from, err := parseDay(in.ValidFrom, "valid_from")
			if err != nil {
				return err
			}
			until, err := parseDay(in.ValidUntil, "valid_until")
			if err != nil {
				return err
			}
			if action == "approve" {
				if from == nil {
					t := time.Now().In(property.PropertyTimezone(ctx, tx, pm.PropertyID))
					d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
					from = &d
				}
				if until == nil {
					var d time.Time
					switch pm.PermitType {
					case "annual":
						d = from.AddDate(1, 0, -1)
					case "temporary":
						d = from.AddDate(0, 0, 6)
					default:
						d = from.AddDate(0, 1, -1)
					}
					until = &d
				}
			}
			if from != nil && until != nil && until.Before(*from) {
				return apperr.Validation("valid_until harus setelah valid_from").WithField("valid_until", "sebelum valid_from")
			}
			if in.ParkingAreaID != nil {
				var ok bool
				_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM parking_areas WHERE id = $1 AND property_id = $2 AND is_active)`, *in.ParkingAreaID, pm.PropertyID).Scan(&ok)
				if !ok {
					return apperr.Validation("parking_area_id tidak ditemukan di property ini").WithField("parking_area_id", "tidak valid")
				}
			}
			if in.FeeAmount != nil && *in.FeeAmount < 0 {
				return apperr.Validation("fee_amount tidak boleh negatif")
			}
			newStatus := pm.Status
			if action == "approve" {
				newStatus, evVerb = "approved", "approved"
			}
			if _, err := tx.Exec(ctx, `UPDATE parking_permits SET status = $2, parking_area_id = COALESCE($3, parking_area_id), valid_from = COALESCE($4, valid_from), valid_until = COALESCE($5, valid_until),
				sticker_number = COALESCE(NULLIF(TRIM($6),''), sticker_number), fee_amount = COALESCE($7, fee_amount), notes = COALESCE($8, notes),
				decided_by = CASE WHEN $9 THEN $10 ELSE decided_by END, decided_at = CASE WHEN $9 THEN now() ELSE decided_at END, updated_by = $10 WHERE id = $1`,
				id, newStatus, in.ParkingAreaID, from, until, in.StickerNumber, in.FeeAmount, in.Notes, action == "approve", p.UserID); err != nil {
				return err
			}
			// kendaraan ber-permit: dipakai pemeriksaan security (P2 permit_valid)
			_, _ = tx.Exec(ctx, `UPDATE vehicles v SET permit_until = pp.valid_until, parking_area_id = COALESCE(pp.parking_area_id, v.parking_area_id), status = 'active', updated_by = $2
				FROM parking_permits pp WHERE pp.id = $1 AND v.id = pp.vehicle_id`, id, p.UserID)
		case "reject", "revoke":
			if reason == "" {
				return apperr.Validation("reason wajib").WithField("reason", "wajib")
			}
			st := map[string]string{"reject": "rejected", "revoke": "revoked"}[action]
			evVerb = st
			if _, err := tx.Exec(ctx, `UPDATE parking_permits SET status = $2, decision_reason = $3, decided_by = $4, decided_at = now(), updated_by = $4 WHERE id = $1`, id, st, reason, p.UserID); err != nil {
				return err
			}
			if action == "revoke" {
				_, _ = tx.Exec(ctx, `UPDATE vehicles v SET permit_until = NULL, updated_by = $2 FROM parking_permits pp WHERE pp.id = $1 AND v.id = pp.vehicle_id
					AND NOT EXISTS (SELECT 1 FROM parking_permits o WHERE o.vehicle_id = v.id AND o.id <> pp.id AND o.status = 'approved')`, id, p.UserID)
			}
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "parking_permit", ObjectID: id, Action: audit.ActStatusChanged, From: pm.Status, To: action, Payload: map[string]any{"reason": reason}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "parking_permit", EntityID: &id, EntityLabel: pm.PermitNumber, Before: map[string]any{"status": pm.Status}, After: map[string]any{"action": action, "reason": reason}})
		if evVerb != "" && s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "parking_permit." + evVerb, OrganizationID: p.OrganizationID, PropertyID: &pm.PropertyID, ObjectType: "parking_permit", ObjectID: id, ObjectLabel: pm.PermitNumber, ActorUserID: &p.UserID,
				Payload: map[string]any{"reason": reason, "domain": "security"}})
		}
		out, err = s.getPermitTx(ctx, tx, id)
		if err == nil {
			s.permitActions(ctx, out)
		}
		return err
	})
	return out, err
}

// ParkingPermitSweep (worker, harian via engineering/6 jam): izin lewat masa berlaku → expired; H-7 → pengingat tenant.
func (s *Service) ParkingPermitSweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	n := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE parking_permits SET status = 'expired' WHERE status = 'approved' AND valid_until < current_date RETURNING id, permit_number, property_id`)
		if err != nil {
			return err
		}
		type ref struct {
			id  uuid.UUID
			num string
			pid uuid.UUID
		}
		var expired, expiring []ref
		for rows.Next() {
			var r ref
			if rows.Scan(&r.id, &r.num, &r.pid) == nil {
				expired = append(expired, r)
			}
		}
		rows.Close()
		// pengingat H-7 sekali (dedup lewat activity)
		rows, err = tx.Query(ctx, `SELECT pp.id, pp.permit_number, pp.property_id FROM parking_permits pp WHERE pp.status = 'approved' AND pp.valid_until = current_date + 7
			AND NOT EXISTS (SELECT 1 FROM activities a WHERE a.object_type = 'parking_permit' AND a.object_id = pp.id AND a.action = 'expiry_reminder')`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r ref
			if rows.Scan(&r.id, &r.num, &r.pid) == nil {
				expiring = append(expiring, r)
			}
		}
		rows.Close()
		for _, r := range expiring {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "parking_permit", ObjectID: r.id, Action: "expiry_reminder"})
		}
		if s.Jobs != nil {
			for _, r := range expired {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "parking_permit.expired", OrganizationID: orgID, PropertyID: &r.pid, ObjectType: "parking_permit", ObjectID: r.id, ObjectLabel: r.num, Payload: map[string]any{"domain": "security"}})
			}
			for _, r := range expiring {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "parking_permit.expiring", OrganizationID: orgID, PropertyID: &r.pid, ObjectType: "parking_permit", ObjectID: r.id, ObjectLabel: r.num, Payload: map[string]any{"domain": "security"}})
			}
		}
		n = len(expired) + len(expiring)
		return nil
	})
	return n, err
}

// ---------- Tenant App: kendaraan & izin parkir ----------

type TenantVehicle struct {
	ID             uuid.UUID       `json:"id"`
	PlateNumber    string          `json:"plate_number"`
	VehicleType    string          `json:"vehicle_type"`
	Brand          *string         `json:"brand"`
	Color          *string         `json:"color"`
	UnitLocationID *uuid.UUID      `json:"unit_location_id"`
	UnitName       *string         `json:"unit_name"`
	Status         string          `json:"status"`
	PermitUntil    *string         `json:"permit_until"`
	PermitValid    bool            `json:"permit_valid"`
	IsMine         bool            `json:"is_mine"`
	Permits        []ParkingPermit `json:"permits"`
	DocumentCount  int             `json:"document_count"`
	// Documents: foto kendaraan / STNK — hanya di detail & hanya untuk pemilik (sejalan VehicleAccess)
	Documents []VehicleDocument `json:"documents,omitempty"`
}

type VehicleDocument struct {
	ID             uuid.UUID `json:"id"`
	AttachmentType string    `json:"attachment_type"`
	FileName       *string   `json:"file_name"`
	ContentType    string    `json:"content_type"`
	URL            string    `json:"url"`
	ThumbURL       string    `json:"thumb_url,omitempty"`
	UploadedAt     time.Time `json:"uploaded_at"`
}

type TenantVehicleInput struct {
	PlateNumber *string    `json:"plate_number"`
	VehicleType *string    `json:"vehicle_type"`
	Brand       *string    `json:"brand"`
	Color       *string    `json:"color"`
	UnitID      *uuid.UUID `json:"unit_id"`
}

// tenantVehicleWhere: kendaraan milik akun ini; Tenant Admin juga melihat kendaraan tenant-nya. $1 user, $2 tenant, $3 role.
const tenantVehicleWhere = ` v.owner_type = 'tenant' AND v.status <> 'inactive' AND (v.user_id = $1 OR ($3 = 'tenant_admin' AND $2::uuid IS NOT NULL AND v.tenant_id = $2))`

func (s *Service) tenantVehiclesTx(ctx context.Context, tx pgx.Tx, sc *tenantscope.Scope, only *uuid.UUID) ([]TenantVehicle, error) {
	rows, err := tx.Query(ctx, `SELECT v.id, v.plate_number, v.vehicle_type, v.brand, v.color, v.unit_location_id, COALESCE('Unit ' || un.unit_number, ul.name), v.status,
		to_char(v.permit_until, 'YYYY-MM-DD'), (v.permit_until IS NOT NULL AND v.permit_until >= current_date), v.user_id = $1,
		(SELECT count(*) FROM attachments a WHERE a.object_type = 'vehicle' AND a.object_id = v.id AND a.deleted_at IS NULL)
		FROM vehicles v LEFT JOIN locations ul ON ul.id = v.unit_location_id LEFT JOIN units un ON un.location_id = v.unit_location_id
		WHERE v.property_id = $4 AND`+tenantVehicleWhere+` AND ($5::uuid IS NULL OR v.id = $5) ORDER BY v.created_at`, sc.UserID, sc.TenantID, sc.Role, sc.PropertyID, only)
	if err != nil {
		return nil, err
	}
	var out []TenantVehicle
	for rows.Next() {
		var v TenantVehicle
		if err := rows.Scan(&v.ID, &v.PlateNumber, &v.VehicleType, &v.Brand, &v.Color, &v.UnitLocationID, &v.UnitName, &v.Status, &v.PermitUntil, &v.PermitValid, &v.IsMine, &v.DocumentCount); err != nil {
			rows.Close()
			return nil, err
		}
		v.Permits = []ParkingPermit{}
		out = append(out, v)
	}
	rows.Close()
	for i := range out {
		prows, err := tx.Query(ctx, permitSelect+` WHERE pp.vehicle_id = $1 ORDER BY pp.requested_at DESC LIMIT 10`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for prows.Next() {
			pm, err := scanPermit(prows)
			if err != nil {
				prows.Close()
				return nil, err
			}
			s.permitActions(ctx, pm)
			out[i].Permits = append(out[i].Permits, *pm)
		}
		prows.Close()
	}
	if out == nil {
		out = []TenantVehicle{}
	}
	return out, nil
}

func (s *Service) TenantVehicles(ctx context.Context) ([]TenantVehicle, error) {
	var out []TenantVehicle
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		out, err = s.tenantVehiclesTx(ctx, tx, sc, nil)
		return err
	})
	return out, err
}

// TenantVehicle: detail kendaraan + riwayat izin + dokumen (P3-PRK-01).
func (s *Service) TenantVehicle(ctx context.Context, id uuid.UUID) (*TenantVehicle, error) {
	var out *TenantVehicle
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		out, err = s.tenantVehicleTx(ctx, tx, sc, id)
		if err != nil || !out.IsMine {
			return err
		}
		atts, err := attachments.ListForObjectTx(ctx, tx, "vehicle", id)
		if err != nil {
			return err
		}
		if s.Ops != nil && s.Ops.Attachments != nil {
			s.Ops.Attachments.FillURLs(ctx, atts)
		}
		out.Documents = []VehicleDocument{}
		for _, a := range atts {
			out.Documents = append(out.Documents, VehicleDocument{ID: a.ID, AttachmentType: a.AttachmentType, FileName: a.OriginalFilename, ContentType: a.ContentType, URL: a.URL, ThumbURL: a.ThumbURL, UploadedAt: a.UploadedAt})
		}
		return nil
	})
	return out, err
}

// TenantParkingPermits: seluruh izin parkir kendaraan yang terlihat oleh akun ini (terbaru dulu), filter status opsional.
func (s *Service) TenantParkingPermits(ctx context.Context, statuses []string) ([]ParkingPermit, error) {
	out := []ParkingPermit{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, permitSelect+` WHERE pp.property_id = $4 AND (cardinality($5::text[]) = 0 OR pp.status = ANY($5))
			AND EXISTS (SELECT 1 FROM vehicles v WHERE v.id = pp.vehicle_id AND`+tenantVehicleWhere+`) ORDER BY pp.requested_at DESC, pp.id DESC LIMIT 200`,
			sc.UserID, sc.TenantID, sc.Role, sc.PropertyID, statuses)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			pm, err := scanPermit(rows)
			if err != nil {
				return err
			}
			s.permitActions(ctx, pm)
			out = append(out, *pm)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) tenantVehicleTx(ctx context.Context, tx pgx.Tx, sc *tenantscope.Scope, id uuid.UUID) (*TenantVehicle, error) {
	list, err := s.tenantVehiclesTx(ctx, tx, sc, &id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, apperr.NotFound("Kendaraan")
	}
	return &list[0], nil
}

// RegisterTenantVehicle: P3-PRK-01 — plat unik per property (entitas vehicles P2).
func (s *Service) RegisterTenantVehicle(ctx context.Context, in TenantVehicleInput) (*TenantVehicle, error) {
	var out *TenantVehicle
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		if in.PlateNumber == nil || NormalizePlate(*in.PlateNumber) == "" {
			return apperr.Validation("plate_number wajib").WithField("plate_number", "wajib")
		}
		plate := NormalizePlate(*in.PlateNumber)
		vt := "car"
		if in.VehicleType != nil {
			vt = *in.VehicleType
		}
		if !vehicleTypes[vt] {
			return apperr.Validation("vehicle_type harus car|motorcycle|truck|bicycle|other").WithField("vehicle_type", "tidak valid")
		}
		var unit *uuid.UUID
		if in.UnitID != nil {
			if !sc.HasUnit(*in.UnitID) {
				return apperr.Validation("unit_id bukan unit Anda").WithField("unit_id", "tidak valid")
			}
			unit = in.UnitID
		} else if pu := sc.PrimaryUnit(); pu != nil {
			unit = &pu.ID
		}
		var existing *uuid.UUID
		var existingStatus string
		var existingUser *uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT id, status, user_id FROM vehicles WHERE property_id = $1 AND plate_number = $2`, sc.PropertyID, plate).Scan(&existing, &existingStatus, &existingUser)
		var id uuid.UUID
		switch {
		case existing == nil:
			var fullName, phone *string
			_ = tx.QueryRow(ctx, `SELECT full_name, phone FROM users WHERE id = $1`, sc.UserID).Scan(&fullName, &phone)
			if err := tx.QueryRow(ctx, `INSERT INTO vehicles (organization_id, property_id, plate_number, vehicle_type, brand, color, owner_type, tenant_id, unit_location_id, user_id, owner_name, owner_phone, status, registered_by_tenant, created_by, updated_by)
				VALUES ($1,$2,$3,$4,NULLIF(TRIM($5),''),NULLIF(TRIM($6),''),'tenant',$7,$8,$9,$10,$11,'active',true,$9,$9) RETURNING id`,
				sc.OrganizationID, sc.PropertyID, plate, vt, deref(in.Brand), deref(in.Color), sc.TenantID, unit, sc.UserID, fullName, phone).Scan(&id); err != nil {
				return err
			}
		case existingStatus == "inactive" && (existingUser == nil || *existingUser == sc.UserID):
			// kendaraan lama milik sendiri yang pernah dihapus → aktifkan kembali
			id = *existing
			if _, err := tx.Exec(ctx, `UPDATE vehicles SET status = 'active', vehicle_type = $2, brand = NULLIF(TRIM($3),''), color = NULLIF(TRIM($4),''), owner_type = 'tenant', tenant_id = $5, unit_location_id = $6, user_id = $7, registered_by_tenant = true, updated_by = $7 WHERE id = $1`,
				id, vt, deref(in.Brand), deref(in.Color), sc.TenantID, unit, sc.UserID); err != nil {
				return err
			}
		default:
			return apperr.Conflict("VEHICLE_EXISTS", "Plat nomor ini sudah terdaftar di gedung; hubungi pengelola bila ini kendaraan Anda").WithField("plate_number", "sudah terdaftar")
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "vehicle", EntityID: &id, EntityLabel: plate, After: map[string]any{"by": "tenant", "type": vt}})
		out, err = s.tenantVehicleTx(ctx, tx, sc, id)
		return err
	})
	return out, err
}

func (s *Service) UpdateTenantVehicle(ctx context.Context, id uuid.UUID, in TenantVehicleInput) (*TenantVehicle, error) {
	var out *TenantVehicle
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		v, err := s.tenantVehicleTx(ctx, tx, sc, id)
		if err != nil {
			return err
		}
		if in.VehicleType != nil && !vehicleTypes[*in.VehicleType] {
			return apperr.Validation("vehicle_type tidak valid")
		}
		if in.UnitID != nil && !sc.HasUnit(*in.UnitID) {
			return apperr.Validation("unit_id bukan unit Anda")
		}
		if _, err := tx.Exec(ctx, `UPDATE vehicles SET vehicle_type = COALESCE($2, vehicle_type), brand = COALESCE($3, brand), color = COALESCE($4, color), unit_location_id = COALESCE($5, unit_location_id), updated_by = $6 WHERE id = $1`,
			id, in.VehicleType, in.Brand, in.Color, in.UnitID, sc.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "vehicle", EntityID: &id, EntityLabel: v.PlateNumber, After: in})
		out, err = s.tenantVehicleTx(ctx, tx, sc, id)
		return err
	})
	return out, err
}

// RemoveTenantVehicle: nonaktifkan kendaraan (izin menunggu dibatalkan; izin aktif harus dicabut pengelola dulu).
func (s *Service) RemoveTenantVehicle(ctx context.Context, id uuid.UUID) error {
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		v, err := s.tenantVehicleTx(ctx, tx, sc, id)
		if err != nil {
			return err
		}
		for _, pm := range v.Permits {
			if pm.Status == "approved" && pm.IsActive {
				return apperr.Conflict("PERMIT_ACTIVE", "Kendaraan masih memiliki izin parkir aktif; hubungi pengelola untuk mencabutnya")
			}
		}
		_, _ = tx.Exec(ctx, `UPDATE parking_permits SET status = 'cancelled', updated_by = $2 WHERE vehicle_id = $1 AND status = 'requested'`, id, sc.UserID)
		if _, err := tx.Exec(ctx, `UPDATE vehicles SET status = 'inactive', updated_by = $2 WHERE id = $1`, id, sc.UserID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "vehicle", EntityID: &id, EntityLabel: v.PlateNumber, After: map[string]any{"by": "tenant"}})
	})
}

type TenantPermitInput struct {
	VehicleID     uuid.UUID  `json:"vehicle_id"`
	PermitType    string     `json:"permit_type"`
	ParkingAreaID *uuid.UUID `json:"parking_area_id"`
	ValidFrom     *string    `json:"valid_from"`
	Notes         *string    `json:"notes"`
}

// RequestParkingPermit: P3-PRK-02 — permohonan izin → notifikasi Security/Tenant Relation untuk persetujuan.
func (s *Service) RequestParkingPermit(ctx context.Context, in TenantPermitInput) (*ParkingPermit, error) {
	var out *ParkingPermit
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		v, err := s.tenantVehicleTx(ctx, tx, sc, in.VehicleID)
		if err != nil {
			return err
		}
		if in.PermitType == "" {
			in.PermitType = "monthly"
		}
		if !permitTypes[in.PermitType] {
			return apperr.Validation("permit_type harus monthly|annual|temporary").WithField("permit_type", "tidak valid")
		}
		for _, pm := range v.Permits {
			if pm.Status == "requested" || (pm.Status == "approved" && pm.IsActive) {
				return apperr.Conflict("PERMIT_EXISTS", "Kendaraan ini sudah memiliki izin aktif atau permohonan yang sedang diproses")
			}
		}
		if in.ParkingAreaID != nil {
			var ok bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM parking_areas WHERE id = $1 AND property_id = $2 AND is_active AND area_type IN ('tenant','mixed'))`, *in.ParkingAreaID, sc.PropertyID).Scan(&ok)
			if !ok {
				return apperr.Validation("parking_area_id tidak tersedia untuk tenant").WithField("parking_area_id", "tidak valid")
			}
		}
		from, err := parseDay(in.ValidFrom, "valid_from")
		if err != nil {
			return err
		}
		number, err := ids.NextYearly(ctx, tx, sc.OrganizationID, ids.PrefixParkingPermit, time.Now(), property.PropertyTimezone(ctx, tx, sc.PropertyID))
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO parking_permits (organization_id, property_id, permit_number, vehicle_id, tenant_id, unit_location_id, requested_by, parking_area_id, permit_type, valid_from, notes, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF(TRIM($11),''),$7) RETURNING id`,
			sc.OrganizationID, sc.PropertyID, number, v.ID, sc.TenantID, v.UnitLocationID, sc.UserID, in.ParkingAreaID, in.PermitType, from, deref(in.Notes)).Scan(&id); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "parking_permit", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "plate": v.PlateNumber, "by": "tenant"}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "parking_permit", EntityID: &id, EntityLabel: number + " " + v.PlateNumber})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "parking_permit.requested", OrganizationID: sc.OrganizationID, PropertyID: &sc.PropertyID, ObjectType: "parking_permit", ObjectID: id, ObjectLabel: number + " · " + v.PlateNumber, ActorUserID: &sc.UserID,
				Payload: map[string]any{"domain": "security"}})
		}
		out, err = s.getPermitTx(ctx, tx, id)
		if err == nil {
			s.permitActions(ctx, out)
		}
		return err
	})
	return out, err
}

func (s *Service) tenantPermitTx(ctx context.Context, tx pgx.Tx, sc *tenantscope.Scope, id uuid.UUID) (*ParkingPermit, error) {
	pm, err := scanPermit(tx.QueryRow(ctx, permitSelect+` WHERE pp.id = $4 AND pp.property_id = $5 AND EXISTS (SELECT 1 FROM vehicles v WHERE v.id = pp.vehicle_id AND`+tenantVehicleWhere+`)`,
		sc.UserID, sc.TenantID, sc.Role, id, sc.PropertyID))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Izin parkir")
		}
		return nil, err
	}
	s.permitActions(ctx, pm)
	return pm, nil
}

func (s *Service) TenantParkingPermit(ctx context.Context, id uuid.UUID) (*ParkingPermit, error) {
	var out *ParkingPermit
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		out, err = s.tenantPermitTx(ctx, tx, sc, id)
		return err
	})
	return out, err
}

func (s *Service) CancelTenantParkingPermit(ctx context.Context, id uuid.UUID) (*ParkingPermit, error) {
	var out *ParkingPermit
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		pm, err := s.tenantPermitTx(ctx, tx, sc, id)
		if err != nil {
			return err
		}
		if pm.Status != "requested" {
			return apperr.InvalidTransition("Hanya permohonan yang belum diproses yang dapat dibatalkan")
		}
		if _, err := tx.Exec(ctx, `UPDATE parking_permits SET status = 'cancelled', updated_by = $2 WHERE id = $1`, id, sc.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "parking_permit", EntityID: &id, EntityLabel: pm.PermitNumber, After: map[string]any{"action": "cancel", "by": "tenant"}})
		out, err = s.tenantPermitTx(ctx, tx, sc, id)
		return err
	})
	return out, err
}

type TenantParkingArea struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Code     string    `json:"code"`
	AreaType string    `json:"area_type"`
}

// TenantParkingAreas: area yang dapat dipilih tenant saat mengajukan izin (tenant/mixed, aktif).
func (s *Service) TenantParkingAreas(ctx context.Context) ([]TenantParkingArea, error) {
	out := []TenantParkingArea{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, name, code, area_type FROM parking_areas WHERE property_id = $1 AND is_active AND area_type IN ('tenant','mixed') ORDER BY code`, sc.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a TenantParkingArea
			if err := rows.Scan(&a.ID, &a.Name, &a.Code, &a.AreaType); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

// VehicleAccess: lampiran object vehicle (foto/STNK) — tenant pemilik atau staf security.parking.view.
func VehicleAccess(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, write bool) error {
	p := authctx.Must(ctx)
	var propertyID uuid.UUID
	var userID, tenantID *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT property_id, user_id, tenant_id FROM vehicles WHERE id = $1`, objectID).Scan(&propertyID, &userID, &tenantID); err != nil {
		return apperr.NotFound("Kendaraan")
	}
	if p.IsTenant {
		if userID != nil && *userID == p.UserID {
			return nil
		}
		return apperr.NotFound("Kendaraan")
	}
	perm := "security.parking.view"
	if write {
		perm = "security.parking.manage"
	}
	if !p.HasAnyOnProperty(perm, propertyID) {
		return apperr.Forbidden("")
	}
	return nil
}
