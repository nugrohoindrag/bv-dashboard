// Package commercial: Apartment Unit Sales & Rental Management (PRD P1 v1.3 §3.10, WF-P1-008/009, AT-P1-000D/E;
// NC §71 Commercial › Unit Sales Management / Unit Rental Management).
// Inventori = units milik property (One Building Data Model); listing = ekstensi komersial per unit. Hanya aktif pada
// profile Apartment (capability unit_sales / unit_rental). Bukan public multi-property marketplace (guardrail #22).
// Sales & rental memakai Property, Unit, Tenant/Occupant, Billing, Notification bersama (PRD §3.10).
package commercial

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantrelation"
)

const (
	EventListingPublished          = "unit_listing.published"
	EventLeadCreated               = "unit_sales_lead.created"
	EventLeadStatusChanged         = "unit_sales_lead.status_changed"
	EventSaleReservationCreated    = "unit_sale_reservation.created"
	EventSaleReservationSold       = "unit_sale_reservation.sold"
	EventSaleReservationHandedOver = "unit_sale_reservation.handed_over"
	EventSaleReservationCancelled  = "unit_sale_reservation.cancelled"
	EventRentalListingPublished    = "unit_rental_listing.published"
	EventRentalReservationCreated  = "unit_rental_reservation.created"
	EventRentalReservationConfirmed = "unit_rental_reservation.confirmed"
	EventRentalReservationActivated = "unit_rental_reservation.activated"
	EventRentalReservationCompleted = "unit_rental_reservation.completed"
	EventRentalReservationCancelled = "unit_rental_reservation.cancelled"
)

type Service struct {
	DB        *db.DB
	Jobs      jobs.Enqueuer
	Profile   *profile.Service
	Property  *property.Service
	Billing   *billing.Service
	TenantRel *tenantrelation.Service
}

func New(d *db.DB, j jobs.Enqueuer, prof *profile.Service, prop *property.Service, bill *billing.Service, tr *tenantrelation.Service) *Service {
	s := &Service{DB: d, Jobs: j, Profile: prof, Property: prop, Billing: bill, TenantRel: tr}
	// Guard perubahan profile (Onboarding Brief §19 / AC-19..20): listing & reservasi aktif memblokir pindah dari Apartment
	prof.RegisterChangeGuard(func(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, from, to profile.Profile) (string, error) {
		if from != profile.Apartment {
			return "", nil
		}
		var listings, sales, rentals int
		if err := tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM unit_listings WHERE property_id = $1 AND status IN ('published','reserved')) +
			(SELECT count(*) FROM unit_rental_listings WHERE property_id = $1 AND status = 'published'),
			(SELECT count(*) FROM unit_sale_reservations WHERE property_id = $1 AND status IN ('reserved','contract_signed','sold')),
			(SELECT count(*) FROM unit_rental_reservations WHERE property_id = $1 AND status IN ('reserved','active'))`, propertyID).Scan(&listings, &sales, &rentals); err != nil {
			return "", err
		}
		var parts []string
		if listings > 0 {
			parts = append(parts, fmt.Sprintf("%d listing unit masih dipublikasikan", listings))
		}
		if sales > 0 {
			parts = append(parts, fmt.Sprintf("%d reservasi penjualan unit masih aktif", sales))
		}
		if rentals > 0 {
			parts = append(parts, fmt.Sprintf("%d reservasi sewa unit masih aktif", rentals))
		}
		return strings.Join(parts, "; "), nil
	})
	return s
}

func (s *Service) requireSales(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID) error {
	return s.Profile.RequireCapabilityTx(ctx, tx, propertyID, profile.CapUnitSales)
}

func (s *Service) requireRental(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID) error {
	return s.Profile.RequireCapabilityTx(ctx, tx, propertyID, profile.CapUnitRental)
}

// Document: document/reference tracking (PRD §3.10) — referensi dokumen (PPJB, AJB, kontrak sewa, KTP) tanpa menyimpan
// isi sensitif; attachment_id opsional menunjuk attachments (S3-compatible) yang sudah ada.
type Document struct {
	Name         string     `json:"name"`
	Reference    *string    `json:"reference,omitempty"`
	AttachmentID *uuid.UUID `json:"attachment_id,omitempty"`
	Note         *string    `json:"note,omitempty"`
	AddedAt      *time.Time `json:"added_at,omitempty"`
}

func normalizeDocs(docs []Document) ([]Document, error) {
	out := make([]Document, 0, len(docs))
	now := time.Now()
	for _, d := range docs {
		d.Name = strings.TrimSpace(d.Name)
		if d.Name == "" {
			return nil, apperr.Validation("documents[].name wajib")
		}
		if d.AddedAt == nil {
			d.AddedAt = &now
		}
		out = append(out, d)
	}
	return out, nil
}

// unitRef: unit yang dijual/disewakan (validasi milik property & bukan kamar hotel).
type unitRef struct {
	LocationID      uuid.UUID
	PropertyID      uuid.UUID
	UnitNumber      string
	Name            string
	UnitType        string
	OccupancyStatus string
	TenantID        *uuid.UUID
	FloorName       *string
	AreaM2          *float64
}

func (s *Service) unitTx(ctx context.Context, tx pgx.Tx, unitID uuid.UUID) (*unitRef, error) {
	var u unitRef
	err := tx.QueryRow(ctx, `SELECT u.location_id, l.property_id, u.unit_number, l.name, u.unit_type, u.occupancy_status, u.tenant_id, pl.name, u.area_m2
		FROM units u JOIN locations l ON l.id = u.location_id LEFT JOIN locations pl ON pl.id = l.parent_id WHERE u.location_id = $1 AND l.deleted_at IS NULL`, unitID).
		Scan(&u.LocationID, &u.PropertyID, &u.UnitNumber, &u.Name, &u.UnitType, &u.OccupancyStatus, &u.TenantID, &u.FloorName, &u.AreaM2)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.Validation("unit_location_id tidak ditemukan").WithField("unit_location_id", "tidak ditemukan")
		}
		return nil, err
	}
	return &u, nil
}

func (s *Service) setUnitOccupancyTx(ctx context.Context, tx pgx.Tx, unitID uuid.UUID, status string, tenantID *uuid.UUID, keepTenant bool) error {
	if keepTenant {
		_, err := tx.Exec(ctx, `UPDATE units SET occupancy_status = $2 WHERE location_id = $1`, unitID, status)
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE units SET occupancy_status = $2, tenant_id = $3 WHERE location_id = $1`, unitID, status, tenantID)
	return err
}

// ---------- Tenant Onboarding (PRD §3.10 "tenant onboarding"; WF-P1-009 Tenant Onboarding → Active Rental) ----------

type OnboardingInput struct {
	FullName        string
	Phone           string
	Email           string
	Company         string
	TenantType      string // individual | company
	OwnershipStatus string // tenant | owner
	MovedInAt       time.Time
	CreateAccount   bool   // akun Tenant App (tenant_user) — Onboarding Brief AC-11
	Password        string // opsional (kosong → sementara)
	Source          string // rental_onboarding | sale_handover
	Notes           string
}

type OnboardingResult struct {
	TenantID          uuid.UUID  `json:"tenant_id"`
	TenantCode        string     `json:"tenant_code"`
	OccupantID        uuid.UUID  `json:"occupant_id"`
	TenantUserID      *uuid.UUID `json:"tenant_user_id,omitempty"` // users.id
	AccountEmail      string     `json:"account_email,omitempty"`
	TemporaryPassword string     `json:"temporary_password,omitempty"`
}

// onboardTx: Tenant (individual/company) + Occupant (primary contact) + unit_occupants + units.tenant_id/occupied
// (+ akun Tenant App opsional dengan tenant_access ke unit). Memakai model Tenant/Occupant bersama — tidak ada entitas "penyewa" terpisah.
func (s *Service) onboardTx(ctx context.Context, tx pgx.Tx, propertyID, unitID uuid.UUID, in OnboardingInput, accessUntil *time.Time) (*OnboardingResult, error) {
	p := authctx.Must(ctx)
	in.FullName = strings.TrimSpace(in.FullName)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = strings.TrimSpace(in.Phone)
	if in.FullName == "" {
		return nil, apperr.Validation("nama penghuni wajib").WithField("full_name", "wajib")
	}
	if in.TenantType != "company" {
		in.TenantType = "individual"
	}
	if in.OwnershipStatus == "" {
		in.OwnershipStatus = "tenant"
	}
	name := in.FullName
	if in.TenantType == "company" && strings.TrimSpace(in.Company) != "" {
		name = strings.TrimSpace(in.Company)
	}
	res := &OnboardingResult{}
	code, err := ids.NextPlain(ctx, tx, p.OrganizationID, ids.PrefixTenant)
	if err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO tenants (organization_id, property_id, tenant_code, name, tenant_type, contact_name, contact_phone, contact_email, status, notes, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),'active',NULLIF($9,''),$10,$10) RETURNING id`,
		p.OrganizationID, propertyID, code, name, in.TenantType, in.FullName, in.Phone, in.Email, in.Notes, p.UserID).Scan(&res.TenantID); err != nil {
		return nil, err
	}
	res.TenantCode = code
	if err := tx.QueryRow(ctx, `INSERT INTO occupants (organization_id, tenant_id, full_name, phone, email, is_primary_contact, status, created_by, updated_by)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),true,'active',$6,$6) RETURNING id`,
		p.OrganizationID, res.TenantID, in.FullName, in.Phone, in.Email, p.UserID).Scan(&res.OccupantID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO unit_occupants (unit_location_id, occupant_id, moved_in_at) VALUES ($1,$2,$3) ON CONFLICT (unit_location_id, occupant_id) DO UPDATE SET moved_in_at = EXCLUDED.moved_in_at, moved_out_at = NULL`,
		unitID, res.OccupantID, in.MovedInAt); err != nil {
		return nil, err
	}
	if err := s.setUnitOccupancyTx(ctx, tx, unitID, "occupied", &res.TenantID, false); err != nil {
		return nil, err
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "tenant", EntityID: &res.TenantID, EntityLabel: code + " " + name, After: map[string]any{"source": in.Source, "unit": unitID}})
	if in.CreateAccount {
		if in.Email == "" {
			return nil, apperr.Validation("email wajib untuk membuat akun Tenant App").WithField("email", "wajib")
		}
		cr, err := s.TenantRel.CreateTx(ctx, tx, tenantrelation.CreateInput{PropertyID: propertyID, TenantID: &res.TenantID, OccupantID: &res.OccupantID, FullName: in.FullName, Email: in.Email, Phone: in.Phone,
			Role: "tenant_admin", OwnershipStatus: in.OwnershipStatus, UnitIDs: []uuid.UUID{unitID}, Password: in.Password, Source: in.Source})
		if err != nil {
			return nil, err
		}
		if accessUntil != nil {
			_, _ = tx.Exec(ctx, `UPDATE tenant_access SET valid_until = $2 WHERE tenant_user_id = $1`, cr.TenantUser.ID, *accessUntil)
		}
		uid := cr.TenantUser.UserID
		res.TenantUserID, res.AccountEmail, res.TemporaryPassword = &uid, in.Email, cr.TemporaryPassword
	}
	return res, nil
}

// offboardTx: akhir sewa — occupant moved-out, tenant moved_out, unit vacant, akses Tenant App berakhir.
func (s *Service) offboardTx(ctx context.Context, tx pgx.Tx, unitID uuid.UUID, tenantID, occupantID, tenantUserID *uuid.UUID, movedOut time.Time) error {
	if occupantID != nil {
		_, _ = tx.Exec(ctx, `UPDATE unit_occupants SET moved_out_at = $3 WHERE unit_location_id = $1 AND occupant_id = $2`, unitID, *occupantID, movedOut)
		_, _ = tx.Exec(ctx, `UPDATE occupants SET status = 'inactive' WHERE id = $1`, *occupantID)
	}
	if tenantID != nil {
		_, _ = tx.Exec(ctx, `UPDATE tenants SET status = 'moved_out' WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM units u WHERE u.tenant_id = tenants.id AND u.location_id <> $2)`, *tenantID, unitID)
	}
	if tenantUserID != nil {
		_, _ = tx.Exec(ctx, `UPDATE tenant_access ta SET valid_until = LEAST(COALESCE(ta.valid_until, $2::date), $2::date) FROM tenant_users tu WHERE tu.id = ta.tenant_user_id AND tu.user_id = $1`, *tenantUserID, movedOut)
	}
	return s.setUnitOccupancyTx(ctx, tx, unitID, "vacant", nil, false)
}

// ---------- helpers ----------

func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strPtr(s string) *string { return &s }

func nilIfEmpty(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func parseDate(v string, field string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(v))
	if err != nil {
		return time.Time{}, apperr.Validation(field + " harus YYYY-MM-DD").WithField(field, "format YYYY-MM-DD")
	}
	return t, nil
}
