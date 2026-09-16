package tenantapp

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/ids"
)

const (
	EventTenantUserRegistered = "tenant_user.registered"
	EventTenantUserApproved   = "tenant_user.approved"
	EventTenantUserRejected   = "tenant_user.rejected"
	EventTenantUserSuspended  = "tenant_user.suspended"
)

// ---------- rate limiter sederhana per IP (registrasi publik; PRD §33 abuse protection) ----------

type ipLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	max  int
	win  time.Duration
}

func newIPLimiter(max int, win time.Duration) *ipLimiter {
	return &ipLimiter{hits: map[string][]time.Time{}, max: max, win: win}
}

func (l *ipLimiter) Allow(ip string) bool {
	if l == nil || l.max <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	var keep []time.Time
	for _, t := range l.hits[ip] {
		if now.Sub(t) < l.win {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.max {
		l.hits[ip] = keep
		return false
	}
	l.hits[ip] = append(keep, now)
	return true
}

// ---------- master data registrasi (tanpa auth) ----------

type Option struct {
	ID       uuid.UUID  `json:"id"`
	Name     string     `json:"name"`
	Code     string     `json:"code,omitempty"`
	ParentID *uuid.UUID `json:"parent_id,omitempty"`
	Profile  string     `json:"profile,omitempty"`
}

func (s *Service) resolveOrg(ctx context.Context, slug string) (uuid.UUID, error) {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return uuid.Nil, apperr.Validation("organization_slug wajib")
	}
	var id uuid.UUID
	var active bool
	if err := s.DB.Pool.QueryRow(ctx, `SELECT id, is_active FROM organizations WHERE slug = $1`, slug).Scan(&id, &active); err != nil || !active {
		return uuid.Nil, apperr.NotFound("Organization")
	}
	return id, nil
}

// RegistrationProperties: property yang mengizinkan self-registration (property_profile_configs.tenant_self_registration).
func (s *Service) RegistrationProperties(ctx context.Context, orgSlug string) ([]Option, error) {
	orgID, err := s.resolveOrg(ctx, orgSlug)
	if err != nil {
		return nil, err
	}
	out := []Option{}
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT l.id, l.name, l.code, p.profile FROM properties p JOIN locations l ON l.id = p.location_id
			LEFT JOIN property_profile_configs c ON c.property_id = p.location_id
			WHERE p.status = 'active' AND l.is_active AND l.deleted_at IS NULL AND COALESCE(c.tenant_self_registration, true) ORDER BY l.name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o Option
			if err := rows.Scan(&o.ID, &o.Name, &o.Code, &o.Profile); err != nil {
				return err
			}
			out = append(out, o)
		}
		return rows.Err()
	})
	return out, err
}

// RegistrationLocations: floor (parent=property) atau unit (parent=floor) untuk pilihan pendaftaran.
func (s *Service) RegistrationLocations(ctx context.Context, orgSlug string, propertyID uuid.UUID, parentID *uuid.UUID, want string) ([]Option, error) {
	orgID, err := s.resolveOrg(ctx, orgSlug)
	if err != nil {
		return nil, err
	}
	if want != "floor" && want != "unit" {
		return nil, apperr.Validation("type harus floor|unit")
	}
	out := []Option{}
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var ok bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM properties p LEFT JOIN property_profile_configs c ON c.property_id = p.location_id WHERE p.location_id = $1 AND p.status = 'active' AND COALESCE(c.tenant_self_registration,true))`, propertyID).Scan(&ok)
		if !ok {
			return apperr.NotFound("Property")
		}
		var rows pgx.Rows
		if want == "floor" {
			// seluruh floor di property (lintas building/tower) — label menyertakan induk agar tidak ambigu
			rows, err = tx.Query(ctx, `SELECT l.id, COALESCE(pa.name || ' · ', '') || l.name, l.code, l.parent_id FROM locations l LEFT JOIN locations pa ON pa.id = l.parent_id
				WHERE l.property_id = $1 AND l.location_type = 'floor' AND l.is_active AND l.deleted_at IS NULL ORDER BY pa.sort_order, l.sort_order, l.name`, propertyID)
		} else {
			if parentID == nil {
				return apperr.Validation("parent_id (floor) wajib")
			}
			rows, err = tx.Query(ctx, `SELECT l.id, l.name, COALESCE(u.unit_number, l.code), l.parent_id FROM locations l LEFT JOIN units u ON u.location_id = l.id
				WHERE l.property_id = $1 AND l.parent_id = $2 AND l.location_type = 'unit' AND l.is_active AND l.deleted_at IS NULL ORDER BY l.sort_order, l.name`, propertyID, *parentID)
		}
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o Option
			if err := rows.Scan(&o.ID, &o.Name, &o.Code, &o.ParentID); err != nil {
				return err
			}
			out = append(out, o)
		}
		return rows.Err()
	})
	return out, err
}

// ---------- registrasi ----------

type RegisterInput struct {
	OrganizationSlug string    `json:"organization_slug"`
	FullName         string    `json:"full_name"`
	FirstName        string    `json:"first_name"`
	LastName         string    `json:"last_name"`
	Gender           *string   `json:"gender"`
	Phone            string    `json:"phone"`
	Email            string    `json:"email"`
	Password         string    `json:"password"`
	PropertyID       uuid.UUID `json:"property_id"`
	UnitID           uuid.UUID `json:"unit_id"`
	OwnershipStatus  string    `json:"ownership_status"` // owner | tenant | family | employee
}

type RegisterResult struct {
	TenantUserID  uuid.UUID `json:"tenant_user_id"`
	UserID        uuid.UUID `json:"user_id"`
	AccountStatus string    `json:"account_status"`
	Message       string    `json:"message"`
}

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Register: pendaftaran mandiri dari Tenant App → akun pending_validation; Tenant Relation memvalidasi (PRD §7, brief PWA).
func (s *Service) Register(ctx context.Context, in RegisterInput, ip string) (*RegisterResult, error) {
	if !s.regLimiter.Allow(ip) {
		return nil, apperr.RateLimited()
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.FullName = strings.TrimSpace(in.FullName)
	if in.FullName == "" {
		in.FullName = strings.TrimSpace(strings.TrimSpace(in.FirstName) + " " + strings.TrimSpace(in.LastName))
	}
	if in.FullName == "" {
		return nil, apperr.Validation("full_name wajib").WithField("full_name", "wajib")
	}
	if !emailRe.MatchString(in.Email) {
		return nil, apperr.Validation("email tidak valid").WithField("email", "tidak valid")
	}
	if len(in.Password) < 8 || !strings.ContainsAny(in.Password, "0123456789") {
		return nil, apperr.Validation("password minimal 8 karakter dan mengandung angka").WithField("password", "lemah")
	}
	if in.PropertyID == uuid.Nil || in.UnitID == uuid.Nil {
		return nil, apperr.Validation("property_id dan unit_id wajib")
	}
	if in.OwnershipStatus == "" {
		in.OwnershipStatus = "tenant"
	}
	if !map[string]bool{"owner": true, "tenant": true, "family": true, "employee": true}[in.OwnershipStatus] {
		return nil, apperr.Validation("ownership_status harus owner|tenant|family|employee")
	}
	orgID, err := s.resolveOrg(ctx, in.OrganizationSlug)
	if err != nil {
		return nil, err
	}
	hash, err := iam.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	out := &RegisterResult{AccountStatus: "pending_validation", Message: "Pendaftaran diterima; menunggu validasi building management"}
	ctx = authctx.With(ctx, authctx.System(orgID))
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		// property mengizinkan self-registration & unit valid di property
		var ok bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM properties p LEFT JOIN property_profile_configs c ON c.property_id = p.location_id WHERE p.location_id = $1 AND p.status = 'active' AND COALESCE(c.tenant_self_registration,true))`, in.PropertyID).Scan(&ok)
		if !ok {
			return apperr.Validation("Property tidak menerima pendaftaran mandiri")
		}
		var unitTenant *uuid.UUID
		var unitName string
		if err := tx.QueryRow(ctx, `SELECT u.tenant_id, l.name FROM units u JOIN locations l ON l.id = u.location_id WHERE u.location_id = $1 AND l.property_id = $2 AND l.deleted_at IS NULL`, in.UnitID, in.PropertyID).Scan(&unitTenant, &unitName); err != nil {
			return apperr.Validation("unit_id tidak ditemukan di property ini").WithField("unit_id", "tidak valid")
		}
		// email unik global (uq_users_email_global)
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = $1 AND deleted_at IS NULL)`, in.Email).Scan(&exists)
		if exists {
			return apperr.Conflict("EMAIL_TAKEN", "Email sudah terdaftar").WithField("email", "sudah terdaftar")
		}
		code, err := ids.NextPlain(ctx, tx, orgID, ids.PrefixUser)
		if err != nil {
			return err
		}
		var userID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO users (organization_id, user_code, email, full_name, phone, password_hash, is_active) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,false) RETURNING id`,
			orgID, code, in.Email, in.FullName, strings.TrimSpace(in.Phone), hash).Scan(&userID); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("EMAIL_TAKEN", "Email sudah terdaftar").WithField("email", "sudah terdaftar")
			}
			return err
		}
		var roleID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE organization_id = $1 AND code = 'tenant_user' AND deleted_at IS NULL`, orgID).Scan(&roleID); err != nil {
			return apperr.Internal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id, property_id) VALUES ($1,$2,$3)`, userID, roleID, in.PropertyID); err != nil {
			return err
		}
		// occupant (+ tenant individual bila unit belum memiliki tenant) — aktif setelah validasi
		tenantID := unitTenant
		if tenantID == nil {
			tcode, err := ids.NextPlain(ctx, tx, orgID, ids.PrefixTenant)
			if err != nil {
				return err
			}
			var tid uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO tenants (organization_id, property_id, tenant_code, name, tenant_type, contact_name, contact_phone, contact_email, status, notes) VALUES ($1,$2,$3,$4,'individual',$4,NULLIF($5,''),$6,'inactive','Dibuat dari pendaftaran Tenant App; menunggu validasi') RETURNING id`,
				orgID, in.PropertyID, tcode, in.FullName, strings.TrimSpace(in.Phone), in.Email).Scan(&tid); err != nil {
				return err
			}
			tenantID = &tid
		}
		var occupantID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO occupants (organization_id, tenant_id, full_name, phone, email, is_primary_contact, status) VALUES ($1,$2,$3,NULLIF($4,''),$5,false,'inactive') RETURNING id`,
			orgID, tenantID, in.FullName, strings.TrimSpace(in.Phone), in.Email).Scan(&occupantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO unit_occupants (unit_location_id, occupant_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, in.UnitID, occupantID); err != nil {
			return err
		}
		var tuID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO tenant_users (organization_id, user_id, property_id, tenant_id, occupant_id, role, status, registration_source, ownership_status)
			VALUES ($1,$2,$3,$4,$5,'tenant_user','pending_validation','self',$6) RETURNING id`, orgID, userID, in.PropertyID, tenantID, occupantID, in.OwnershipStatus).Scan(&tuID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tenant_access (organization_id, tenant_user_id, property_id, location_id, access_type, is_primary, status) VALUES ($1,$2,$3,$4,'unit',true,'pending')`,
			orgID, tuID, in.PropertyID, in.UnitID); err != nil {
			return err
		}
		_ = audit.LogAs(ctx, tx, orgID, nil, ip, "", audit.AuditEntry{Action: audit.AuditCreate, EntityType: "tenant_user", EntityID: &tuID, EntityLabel: in.Email, After: map[string]any{"unit": unitName, "ownership_status": in.OwnershipStatus, "source": "self"}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventTenantUserRegistered, OrganizationID: orgID, PropertyID: &in.PropertyID, ObjectType: "tenant_user", ObjectID: tuID, ObjectLabel: in.FullName + " · " + unitName,
				Payload: map[string]any{"unit": unitName, "email": in.Email, "domain": "tenant_relation"}})
		}
		out.TenantUserID, out.UserID = tuID, userID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
