// Package tenantrelation: modul Tenant Relation di Dashboard (PRD P1 v1.3 §3.2, §6 persona Tenant Relation, brief §6) —
// validasi akun tenant, akses unit, komunikasi (ServiceRequestMessage), feedback/CSAT, announcement.
// Tenant Relation bukan pengganti Engineering/Security/Housekeeping: eksekusi tetap lewat Task/WO Engine.
package tenantrelation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/tenantapp"
)

type Service struct {
	DB   *db.DB
	Jobs jobs.Enqueuer
	IAM  *iam.Service
}

func New(d *db.DB, j jobs.Enqueuer, iamSvc *iam.Service) *Service {
	return &Service{DB: d, Jobs: j, IAM: iamSvc}
}

// ---------- Tenant Users ----------

type TenantUser struct {
	ID                 uuid.UUID             `json:"id"`
	UserID             uuid.UUID             `json:"user_id"`
	FullName           string                `json:"full_name"`
	Email              *string               `json:"email"`
	Phone              *string               `json:"phone"`
	PropertyID         uuid.UUID             `json:"property_id"`
	PropertyName       string                `json:"property_name"`
	TenantID           *uuid.UUID            `json:"tenant_id"`
	TenantName         *string               `json:"tenant_name"`
	OccupantID         *uuid.UUID            `json:"occupant_id"`
	Role               string                `json:"role"`
	Status             string                `json:"status"`
	RegistrationSource string                `json:"registration_source"`
	OwnershipStatus    *string               `json:"ownership_status"`
	RequestedAt        time.Time             `json:"requested_at"`
	ValidatedAt        *time.Time            `json:"validated_at"`
	ValidatedByName    *string               `json:"validated_by_name"`
	RejectionReason    *string               `json:"rejection_reason"`
	SuspensionReason   *string               `json:"suspension_reason"`
	LastSeenAt         *time.Time            `json:"last_seen_at"`
	Access             []tenantapp.AccessLoc `json:"access"`
	OpenRequests       int                   `json:"open_requests"`
	AllowedActions     []string              `json:"allowed_actions"`
	Version            int                   `json:"version"`
}

const tuSelect = `SELECT tu.id, tu.user_id, u.full_name, u.email, u.phone, tu.property_id, pl.name, tu.tenant_id, t.name, tu.occupant_id, tu.role, tu.status, tu.registration_source, tu.ownership_status,
	tu.requested_at, tu.validated_at, vb.full_name, tu.rejection_reason, tu.suspension_reason, tu.last_seen_at, tu.version,
	(SELECT count(*) FROM service_requests sr WHERE sr.tenant_user_id = tu.user_id AND sr.status NOT IN ('closed','cancelled'))
	FROM tenant_users tu JOIN users u ON u.id = tu.user_id JOIN locations pl ON pl.id = tu.property_id
	LEFT JOIN tenants t ON t.id = tu.tenant_id LEFT JOIN users vb ON vb.id = tu.validated_by`

func scanTU(row pgx.Row) (*TenantUser, error) {
	var t TenantUser
	if err := row.Scan(&t.ID, &t.UserID, &t.FullName, &t.Email, &t.Phone, &t.PropertyID, &t.PropertyName, &t.TenantID, &t.TenantName, &t.OccupantID, &t.Role, &t.Status, &t.RegistrationSource, &t.OwnershipStatus,
		&t.RequestedAt, &t.ValidatedAt, &t.ValidatedByName, &t.RejectionReason, &t.SuspensionReason, &t.LastSeenAt, &t.Version, &t.OpenRequests); err != nil {
		return nil, err
	}
	t.Access = []tenantapp.AccessLoc{}
	return &t, nil
}

func (s *Service) loadAccess(ctx context.Context, tx pgx.Tx, t *TenantUser) error {
	rows, err := tx.Query(ctx, `SELECT ta.id, l.id, l.location_type, l.name, l.code, u.unit_number, ta.is_primary, ta.access_type, ta.valid_from, ta.valid_until, ta.status,
		COALESCE((SELECT string_agg(a.name, ' · ' ORDER BY a.depth) FROM locations a WHERE a.path @> l.path AND a.depth > 0 AND a.id <> l.id), '')
		FROM tenant_access ta JOIN locations l ON l.id = ta.location_id LEFT JOIN units u ON u.location_id = l.id
		WHERE ta.tenant_user_id = $1 AND ta.status <> 'revoked' ORDER BY ta.is_primary DESC, l.name`, t.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a tenantapp.AccessLoc
		var st string
		if err := rows.Scan(&a.AccessID, &a.ID, &a.LocationType, &a.Name, &a.Code, &a.UnitNumber, &a.IsPrimary, &a.AccessType, &a.ValidFrom, &a.ValidUntil, &st, &a.PathText); err != nil {
			return err
		}
		t.Access = append(t.Access, a)
	}
	return rows.Err()
}

func (s *Service) finalize(ctx context.Context, t *TenantUser) {
	p := authctx.Must(ctx)
	t.AllowedActions = []string{"view"}
	can := func(perm string) bool { return p.HasOnProperty(perm, t.PropertyID) }
	switch t.Status {
	case "pending_validation":
		if can("tenant_relation.tenant_users.validate") {
			t.AllowedActions = append(t.AllowedActions, "approve", "reject")
		}
	case "active":
		if can("tenant_relation.tenant_users.suspend") {
			t.AllowedActions = append(t.AllowedActions, "suspend")
		}
	case "suspended", "rejected":
		if can("tenant_relation.tenant_users.validate") {
			t.AllowedActions = append(t.AllowedActions, "reactivate")
		}
	}
	if can("tenant_relation.tenant_users.update") {
		t.AllowedActions = append(t.AllowedActions, "update", "manage_access", "reset_password")
	}
}

type Filter struct {
	PropertyID *uuid.UUID
	Status     []string
	TenantID   *uuid.UUID
	Q          string
}

func (s *Service) List(ctx context.Context, f Filter, page httpx.Page) ([]TenantUser, *string, error) {
	p := authctx.Must(ctx)
	var out []TenantUser
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if f.PropertyID != nil {
			if err := iam.CanOnProperty(ctx, "tenant_relation.tenant_users.view", *f.PropertyID); err != nil {
				return err
			}
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND tu.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("tenant_relation.tenant_users.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND tu.property_id = ANY($%d)", len(args))
		}
		if len(f.Status) > 0 {
			args = append(args, f.Status)
			where += fmt.Sprintf(" AND tu.status = ANY($%d)", len(args))
		}
		if f.TenantID != nil {
			args = append(args, *f.TenantID)
			where += fmt.Sprintf(" AND tu.tenant_id = $%d", len(args))
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (u.full_name ILIKE $%d OR u.email ILIKE $%d OR u.phone ILIKE $%d OR t.name ILIKE $%d)", len(args), len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (tu.requested_at, tu.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, tuSelect+where+fmt.Sprintf(" ORDER BY tu.requested_at DESC, tu.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		var items []TenantUser
		for rows.Next() {
			t, err := scanTU(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, *t)
		}
		rows.Close()
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.RequestedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			if err := s.loadAccess(ctx, tx, &items[i]); err != nil {
				return err
			}
			s.finalize(ctx, &items[i])
		}
		out = items
		return nil
	})
	if out == nil {
		out = []TenantUser{}
	}
	return out, next, err
}

func (s *Service) getTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*TenantUser, error) {
	t, err := scanTU(tx.QueryRow(ctx, tuSelect+` WHERE tu.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Tenant user")
		}
		return nil, err
	}
	if err := iam.CanOnProperty(ctx, "tenant_relation.tenant_users.view", t.PropertyID); err != nil {
		return nil, err
	}
	if err := s.loadAccess(ctx, tx, t); err != nil {
		return nil, err
	}
	s.finalize(ctx, t)
	return t, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*TenantUser, error) {
	var out *TenantUser
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

type CreateInput struct {
	PropertyID      uuid.UUID   `json:"property_id"`
	TenantID        *uuid.UUID  `json:"tenant_id"`
	OccupantID      *uuid.UUID  `json:"occupant_id"`
	FullName        string      `json:"full_name"`
	Email           string      `json:"email"`
	Phone           string      `json:"phone"`
	Role            string      `json:"role"`
	OwnershipStatus string      `json:"ownership_status"`
	UnitIDs         []uuid.UUID `json:"unit_ids"`
	Password        string      `json:"password"` // opsional; kosong → dibuat sementara dan dikembalikan sekali
	Source          string      `json:"-"`        // staff | rental_onboarding | hotel_checkin
}

type CreateResult struct {
	TenantUser        *TenantUser `json:"tenant_user"`
	TemporaryPassword string      `json:"temporary_password,omitempty"`
}

// Create: staf membuat akun tenant untuk occupant (langsung aktif; PRD §7 "Tenant Admin dapat mengelola beberapa occupant").
func (s *Service) Create(ctx context.Context, in CreateInput) (*CreateResult, error) {
	var out *CreateResult
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.CreateTx(ctx, tx, in)
		return err
	})
	return out, err
}

func (s *Service) CreateTx(ctx context.Context, tx pgx.Tx, in CreateInput) (*CreateResult, error) {
	p := authctx.Must(ctx)
	if !p.IsSystem {
		if err := iam.CanOnProperty(ctx, "tenant_relation.tenant_users.update", in.PropertyID); err != nil {
			return nil, err
		}
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.FullName = strings.TrimSpace(in.FullName)
	if in.Email == "" || !strings.Contains(in.Email, "@") {
		return nil, apperr.Validation("email wajib").WithField("email", "wajib")
	}
	if in.Role == "" {
		in.Role = "tenant_user"
	}
	if in.Role != "tenant_user" && in.Role != "tenant_admin" {
		return nil, apperr.Validation("role harus tenant_user|tenant_admin")
	}
	if in.Source == "" {
		in.Source = "staff"
	}
	// occupant → nama/telepon/tenant dari occupant bila tidak diisi
	if in.OccupantID != nil {
		var name string
		var phone, email *string
		var tid *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT full_name, phone, email, tenant_id FROM occupants WHERE id = $1 AND deleted_at IS NULL`, *in.OccupantID).Scan(&name, &phone, &email, &tid); err != nil {
			return nil, apperr.Validation("occupant_id tidak ditemukan")
		}
		if in.FullName == "" {
			in.FullName = name
		}
		if in.Phone == "" && phone != nil {
			in.Phone = *phone
		}
		if in.TenantID == nil {
			in.TenantID = tid
		}
	}
	if in.FullName == "" {
		return nil, apperr.Validation("full_name wajib").WithField("full_name", "wajib")
	}
	if in.TenantID != nil {
		var tpid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM tenants WHERE id = $1 AND deleted_at IS NULL`, *in.TenantID).Scan(&tpid); err != nil || tpid != in.PropertyID {
			return nil, apperr.Validation("tenant_id tidak ditemukan di property ini")
		}
	}
	var exists bool
	_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = $1 AND deleted_at IS NULL)`, in.Email).Scan(&exists)
	if exists {
		return nil, apperr.Conflict("EMAIL_TAKEN", "Email sudah terdaftar").WithField("email", "sudah terdaftar")
	}
	pw, temp := in.Password, ""
	if pw == "" {
		raw, _, err := iam.NewRefreshToken()
		if err != nil {
			return nil, err
		}
		pw = "Bv" + raw[:8] + "1!"
		temp = pw
	}
	hash, err := iam.HashPassword(pw)
	if err != nil {
		return nil, err
	}
	code, err := ids.NextPlain(ctx, tx, p.OrganizationID, ids.PrefixUser)
	if err != nil {
		return nil, err
	}
	var userID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO users (organization_id, user_code, email, full_name, phone, password_hash, is_active, created_by) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,true,$7) RETURNING id`,
		p.OrganizationID, code, in.Email, in.FullName, strings.TrimSpace(in.Phone), hash, actorOrNil(p)).Scan(&userID); err != nil {
		if db.IsUniqueViolation(err) {
			return nil, apperr.Conflict("EMAIL_TAKEN", "Email sudah terdaftar")
		}
		return nil, err
	}
	var roleID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE organization_id = $1 AND code = $2 AND deleted_at IS NULL`, p.OrganizationID, in.Role).Scan(&roleID); err != nil {
		return nil, apperr.Internal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id, property_id, granted_by) VALUES ($1,$2,$3,$4)`, userID, roleID, in.PropertyID, actorOrNil(p)); err != nil {
		return nil, err
	}
	var owner *string
	if in.OwnershipStatus != "" {
		owner = &in.OwnershipStatus
	}
	var tuID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO tenant_users (organization_id, user_id, property_id, tenant_id, occupant_id, role, status, registration_source, ownership_status, validated_at, validated_by, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,'active',$7,$8,now(),$9,$9) RETURNING id`, p.OrganizationID, userID, in.PropertyID, in.TenantID, in.OccupantID, in.Role, in.Source, owner, actorOrNil(p)).Scan(&tuID); err != nil {
		return nil, err
	}
	for i, uid := range in.UnitIDs {
		if err := s.grantTx(ctx, tx, tuID, in.PropertyID, uid, "unit", i == 0); err != nil {
			return nil, err
		}
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "tenant_user", EntityID: &tuID, EntityLabel: in.Email, After: map[string]any{"role": in.Role, "units": len(in.UnitIDs), "source": in.Source}})
	t, err := s.getTxNoPerm(ctx, tx, tuID)
	if err != nil {
		return nil, err
	}
	return &CreateResult{TenantUser: t, TemporaryPassword: temp}, nil
}

func (s *Service) getTxNoPerm(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*TenantUser, error) {
	t, err := scanTU(tx.QueryRow(ctx, tuSelect+` WHERE tu.id = $1`, id))
	if err != nil {
		return nil, err
	}
	if err := s.loadAccess(ctx, tx, t); err != nil {
		return nil, err
	}
	s.finalize(ctx, t)
	return t, nil
}

func (s *Service) grantTx(ctx context.Context, tx pgx.Tx, tuID, propertyID, locationID uuid.UUID, accessType string, primary bool) error {
	p := authctx.Must(ctx)
	var lt string
	var lpid uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT location_type, property_id FROM locations WHERE id = $1 AND deleted_at IS NULL`, locationID).Scan(&lt, &lpid); err != nil || lpid != propertyID {
		return apperr.Validation("location_id tidak ditemukan di property ini")
	}
	if accessType == "" {
		if lt == "unit" {
			accessType = "unit"
		} else {
			accessType = "area"
		}
	}
	if accessType == "unit" && lt != "unit" {
		return apperr.Validation("access_type unit hanya untuk lokasi bertipe unit")
	}
	if primary {
		_, _ = tx.Exec(ctx, `UPDATE tenant_access SET is_primary = false WHERE tenant_user_id = $1`, tuID)
	}
	_, err := tx.Exec(ctx, `INSERT INTO tenant_access (organization_id, tenant_user_id, property_id, location_id, access_type, is_primary, status, granted_by)
		VALUES ($1,$2,$3,$4,$5,$6,'active',$7)
		ON CONFLICT (tenant_user_id, location_id) DO UPDATE SET status = 'active', is_primary = EXCLUDED.is_primary, revoked_at = NULL, revoked_by = NULL, granted_by = EXCLUDED.granted_by, granted_at = now()`,
		p.OrganizationID, tuID, propertyID, locationID, accessType, primary, actorOrNil(p))
	return err
}

type AccessInput struct {
	LocationID uuid.UUID `json:"location_id"`
	AccessType string    `json:"access_type"`
	IsPrimary  bool      `json:"is_primary"`
}

func (s *Service) GrantAccess(ctx context.Context, id uuid.UUID, in AccessInput) (*TenantUser, error) {
	var out *TenantUser
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "tenant_relation.tenant_users.update", t.PropertyID); err != nil {
			return err
		}
		if err := s.grantTx(ctx, tx, id, t.PropertyID, in.LocationID, in.AccessType, in.IsPrimary); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "tenant_access", EntityID: &id, EntityLabel: t.FullName, After: in})
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) RevokeAccess(ctx context.Context, id, accessID uuid.UUID) (*TenantUser, error) {
	var out *TenantUser
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "tenant_relation.tenant_users.update", t.PropertyID); err != nil {
			return err
		}
		p := authctx.Must(ctx)
		ct, err := tx.Exec(ctx, `UPDATE tenant_access SET status = 'revoked', revoked_at = now(), revoked_by = $3, is_primary = false WHERE id = $1 AND tenant_user_id = $2`, accessID, id, p.UserID)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return apperr.NotFound("Access")
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "tenant_access", EntityID: &accessID, EntityLabel: t.FullName, After: map[string]any{"status": "revoked"}})
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

type DecisionInput struct {
	Reason string `json:"reason"`
}

// Decide: approve | reject | suspend | reactivate (validasi akun oleh Tenant Relation).
func (s *Service) Decide(ctx context.Context, id uuid.UUID, action string, in DecisionInput) (*TenantUser, error) {
	p := authctx.Must(ctx)
	var out *TenantUser
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		allowed := false
		for _, a := range t.AllowedActions {
			if a == action {
				allowed = true
			}
		}
		if !allowed {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk akun berstatus %s", action, t.Status))
		}
		reason := strings.TrimSpace(in.Reason)
		var evType string
		switch action {
		case "approve", "reactivate":
			if _, err := tx.Exec(ctx, `UPDATE tenant_users SET status = 'active', validated_at = now(), validated_by = $2, rejection_reason = NULL, suspended_at = NULL, suspended_by = NULL, suspension_reason = NULL, updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE users SET is_active = true, permission_version = permission_version + 1 WHERE id = $1`, t.UserID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE tenant_access SET status = 'active' WHERE tenant_user_id = $1 AND status = 'pending'`, id); err != nil {
				return err
			}
			// aktifkan occupant/tenant hasil registrasi & isi relasi unit (One Building Data Model)
			if t.OccupantID != nil {
				_, _ = tx.Exec(ctx, `UPDATE occupants SET status = 'active' WHERE id = $1`, *t.OccupantID)
				_, _ = tx.Exec(ctx, `UPDATE unit_occupants SET moved_in_at = COALESCE(moved_in_at, current_date) WHERE occupant_id = $1`, *t.OccupantID)
			}
			if t.TenantID != nil {
				_, _ = tx.Exec(ctx, `UPDATE tenants SET status = 'active', notes = NULL WHERE id = $1 AND status = 'inactive'`, *t.TenantID)
				_, _ = tx.Exec(ctx, `UPDATE units u SET tenant_id = $1, occupancy_status = 'occupied' FROM tenant_access ta WHERE ta.tenant_user_id = $2 AND ta.access_type = 'unit' AND ta.status = 'active' AND u.location_id = ta.location_id AND u.tenant_id IS NULL`, *t.TenantID, id)
			}
			evType = tenantapp.EventTenantUserApproved
		case "reject":
			if reason == "" {
				return apperr.Validation("reason wajib").WithField("reason", "wajib")
			}
			if _, err := tx.Exec(ctx, `UPDATE tenant_users SET status = 'rejected', rejection_reason = $2, validated_at = now(), validated_by = $3, updated_by = $3 WHERE id = $1`, id, reason, p.UserID); err != nil {
				return err
			}
			_, _ = tx.Exec(ctx, `UPDATE users SET is_active = false WHERE id = $1`, t.UserID)
			evType = tenantapp.EventTenantUserRejected
		case "suspend":
			if reason == "" {
				return apperr.Validation("reason wajib").WithField("reason", "wajib")
			}
			if _, err := tx.Exec(ctx, `UPDATE tenant_users SET status = 'suspended', suspended_at = now(), suspended_by = $3, suspension_reason = $2, updated_by = $3 WHERE id = $1`, id, reason, p.UserID); err != nil {
				return err
			}
			// putus sesi aktif (server-side)
			_, _ = tx.Exec(ctx, `UPDATE users SET is_active = false, permission_version = permission_version + 1 WHERE id = $1`, t.UserID)
			_, _ = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, t.UserID)
			evType = tenantapp.EventTenantUserSuspended
		}
		s.IAM.InvalidateUser(t.UserID)
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "tenant_user", EntityID: &id, EntityLabel: t.FullName, Before: map[string]any{"status": t.Status}, After: map[string]any{"action": action, "reason": reason}})
		if s.Jobs != nil && evType != "" {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: evType, OrganizationID: p.OrganizationID, PropertyID: &t.PropertyID, ObjectType: "tenant_user", ObjectID: id, ObjectLabel: t.FullName, ActorUserID: &p.UserID,
				Payload: map[string]any{"tenant_user_id": t.UserID, "reason": reason, "domain": "tenant_relation"}})
		}
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

type UpdateInput struct {
	Role            *string    `json:"role"`
	OwnershipStatus *string    `json:"ownership_status"`
	TenantID        *uuid.UUID `json:"tenant_id"`
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*TenantUser, error) {
	var out *TenantUser
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "tenant_relation.tenant_users.update", t.PropertyID); err != nil {
			return err
		}
		p := authctx.Must(ctx)
		if in.Role != nil {
			if *in.Role != "tenant_user" && *in.Role != "tenant_admin" {
				return apperr.Validation("role harus tenant_user|tenant_admin")
			}
			var roleID uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE organization_id = $1 AND code = $2 AND deleted_at IS NULL`, p.OrganizationID, *in.Role).Scan(&roleID); err != nil {
				return apperr.Internal(err)
			}
			_, _ = tx.Exec(ctx, `DELETE FROM user_roles ur USING roles r WHERE ur.role_id = r.id AND ur.user_id = $1 AND r.code IN ('tenant_user','tenant_admin')`, t.UserID)
			if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id, property_id, granted_by) VALUES ($1,$2,$3,$4)`, t.UserID, roleID, t.PropertyID, p.UserID); err != nil {
				return err
			}
			_, _ = tx.Exec(ctx, `UPDATE users SET permission_version = permission_version + 1 WHERE id = $1`, t.UserID)
			s.IAM.InvalidateUser(t.UserID)
		}
		if _, err := tx.Exec(ctx, `UPDATE tenant_users SET role = COALESCE($2, role), ownership_status = COALESCE($3, ownership_status), tenant_id = COALESCE($4, tenant_id), updated_by = $5 WHERE id = $1`, id, in.Role, in.OwnershipStatus, in.TenantID, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "tenant_user", EntityID: &id, EntityLabel: t.FullName, After: in})
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

// ---------- Feedback / metrics (PRD §28 dashboard Tenant Relation) ----------

type Metrics struct {
	OpenTickets      int      `json:"open_tickets"`
	SLARisk          int      `json:"sla_risk"`
	Overdue          int      `json:"overdue"`
	ResolvedToday    int      `json:"resolved_today"`
	Reopened30d      int      `json:"reopened_30d"`
	WaitingForTenant int      `json:"waiting_for_tenant"`
	WaitingForStaff  int      `json:"waiting_for_staff"` // pesan tenant belum dibaca staf
	PendingAccounts  int      `json:"pending_accounts"`
	CSAT             *float64 `json:"csat"`
	CSATCount        int      `json:"csat_count"`
	ReopenRatePct    *float64 `json:"reopen_rate_pct"`
	TenantAppTickets int      `json:"tenant_app_tickets_30d"`
}

func (s *Service) Metrics(ctx context.Context, propertyID *uuid.UUID) (*Metrics, error) {
	p := authctx.Must(ctx)
	var m Metrics
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pids []uuid.UUID
		all := true
		if propertyID != nil {
			if err := iam.CanOnProperty(ctx, "tenant.service_requests.view", *propertyID); err != nil {
				return err
			}
			pids, all = []uuid.UUID{*propertyID}, false
		} else {
			pids, all = p.PropertyIDsFor("tenant.service_requests.view")
		}
		scope := func(col string) string {
			if all {
				return "true"
			}
			return col + " = ANY($1)"
		}
		args := []any{}
		if !all {
			args = append(args, pids)
		}
		q := `SELECT
			count(*) FILTER (WHERE status NOT IN ('closed','cancelled')),
			count(*) FILTER (WHERE sla_risk_at IS NOT NULL AND sla_breached_at IS NULL AND status NOT IN ('resolved','closed','cancelled')),
			count(*) FILTER (WHERE sla_breached_at IS NOT NULL AND status NOT IN ('resolved','closed','cancelled')),
			count(*) FILTER (WHERE resolved_at >= date_trunc('day', now())),
			count(*) FILTER (WHERE reopen_count > 0 AND updated_at >= now() - interval '30 days'),
			count(*) FILTER (WHERE status = 'waiting_for_tenant'),
			count(*) FILTER (WHERE channel = 'tenant_app' AND created_at >= now() - interval '30 days'),
			count(*) FILTER (WHERE closed_at >= now() - interval '30 days'),
			count(*) FILTER (WHERE closed_at >= now() - interval '30 days' AND reopen_count > 0)
			FROM service_requests sr WHERE ` + scope("sr.property_id")
		var closed30, reopened30 int
		if err := tx.QueryRow(ctx, q, args...).Scan(&m.OpenTickets, &m.SLARisk, &m.Overdue, &m.ResolvedToday, &m.Reopened30d, &m.WaitingForTenant, &m.TenantAppTickets, &closed30, &reopened30); err != nil {
			return err
		}
		if closed30 > 0 {
			r := float64(reopened30) * 100 / float64(closed30)
			m.ReopenRatePct = &r
		}
		_ = tx.QueryRow(ctx, `SELECT count(DISTINCT m.service_request_id) FROM service_request_messages m JOIN service_requests sr ON sr.id = m.service_request_id WHERE m.author_kind = 'tenant' AND m.read_by_staff_at IS NULL AND `+scope("sr.property_id"), args...).Scan(&m.WaitingForStaff)
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM tenant_users tu WHERE tu.status = 'pending_validation' AND `+scope("tu.property_id"), args...).Scan(&m.PendingAccounts)
		var avg *float64
		_ = tx.QueryRow(ctx, `SELECT avg(rating)::float8, count(*) FROM service_request_feedback fb WHERE fb.created_at >= now() - interval '90 days' AND `+scope("fb.property_id"), args...).Scan(&avg, &m.CSATCount)
		m.CSAT = avg
		return nil
	})
	return &m, err
}

type FeedbackRow struct {
	ServiceRequestID uuid.UUID `json:"service_request_id"`
	RequestNumber    string    `json:"request_number"`
	Title            string    `json:"title"`
	CategoryCode     string    `json:"category_code"`
	Rating           int       `json:"rating"`
	Comment          *string   `json:"comment"`
	TenantName       *string   `json:"tenant_name"`
	CreatedAt        time.Time `json:"created_at"`
}

func (s *Service) ListFeedback(ctx context.Context, propertyID *uuid.UUID, page httpx.Page) ([]FeedbackRow, *string, error) {
	p := authctx.Must(ctx)
	var out []FeedbackRow
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if propertyID != nil {
			if err := iam.CanOnProperty(ctx, "tenant_relation.feedback.view", *propertyID); err != nil {
				return err
			}
			args = append(args, *propertyID)
			where += fmt.Sprintf(" AND fb.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("tenant_relation.feedback.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND fb.property_id = ANY($%d)", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (fb.created_at, fb.service_request_id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, `SELECT fb.service_request_id, sr.request_number, sr.title, sr.category_code, fb.rating, fb.comment, u.full_name, fb.created_at
			FROM service_request_feedback fb JOIN service_requests sr ON sr.id = fb.service_request_id LEFT JOIN users u ON u.id = fb.tenant_user_id`+where+fmt.Sprintf(" ORDER BY fb.created_at DESC, fb.service_request_id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var items []FeedbackRow
		for rows.Next() {
			var r FeedbackRow
			if err := rows.Scan(&r.ServiceRequestID, &r.RequestNumber, &r.Title, &r.CategoryCode, &r.Rating, &r.Comment, &r.TenantName, &r.CreatedAt); err != nil {
				return err
			}
			items = append(items, r)
		}
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ServiceRequestID)
			next = &c
			items = items[:page.Limit]
		}
		out = items
		return rows.Err()
	})
	if out == nil {
		out = []FeedbackRow{}
	}
	return out, next, err
}

func actorOrNil(p *authctx.Principal) *uuid.UUID {
	if p.IsSystem || p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}
