package property

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
)

// ---------- Organization (PRD P0 v2 §6) ----------

type Organization struct {
	ID              uuid.UUID      `json:"id"`
	Code            string         `json:"code"`
	Slug            string         `json:"slug"`
	Name            string         `json:"name"`
	LegalName       *string        `json:"legal_name"`
	Timezone        string         `json:"timezone"`
	Status          string         `json:"status"` // active | inactive | suspended
	StatusReason    *string        `json:"status_reason"`
	StatusChangedAt *time.Time     `json:"status_changed_at"`
	IsActive        bool           `json:"is_active"`
	Email           *string        `json:"email"`
	Phone           *string        `json:"phone"`
	Website         *string        `json:"website"`
	Address         *string        `json:"address"`
	City            *string        `json:"city"`
	Province        *string        `json:"province"`
	PostalCode      *string        `json:"postal_code"`
	Country         string         `json:"country"`
	TaxID           *string        `json:"tax_id"`
	Industry        *string        `json:"industry"`
	IsInternal      bool           `json:"is_internal"`
	TrialStatus     string         `json:"trial_status"`
	PlanCode        *string        `json:"plan_code"`
	Settings        map[string]any `json:"settings"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	Version         int            `json:"version"`
}

// OrganizationInput: field profil yang boleh diubah (nil = tidak diubah).
type OrganizationInput struct {
	Name       *string        `json:"name"`
	LegalName  *string        `json:"legal_name"`
	Timezone   *string        `json:"timezone"`
	Email      *string        `json:"email"`
	Phone      *string        `json:"phone"`
	Website    *string        `json:"website"`
	Address    *string        `json:"address"`
	City       *string        `json:"city"`
	Province   *string        `json:"province"`
	PostalCode *string        `json:"postal_code"`
	Country    *string        `json:"country"`
	TaxID      *string        `json:"tax_id"`
	Industry   *string        `json:"industry"`
	Settings   map[string]any `json:"settings"`
}

const orgSelect = `SELECT id, code, slug, name, legal_name, timezone, status, status_reason, status_changed_at, is_active, email, phone, website,
	address, city, province, postal_code, country, tax_id, industry, is_internal, trial_status, plan_code, settings, created_at, updated_at, version
	FROM organizations`

func scanOrg(row pgx.Row) (*Organization, error) {
	var o Organization
	err := row.Scan(&o.ID, &o.Code, &o.Slug, &o.Name, &o.LegalName, &o.Timezone, &o.Status, &o.StatusReason, &o.StatusChangedAt, &o.IsActive,
		&o.Email, &o.Phone, &o.Website, &o.Address, &o.City, &o.Province, &o.PostalCode, &o.Country, &o.TaxID, &o.Industry, &o.IsInternal,
		&o.TrialStatus, &o.PlanCode, &o.Settings, &o.CreatedAt, &o.UpdatedAt, &o.Version)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Organization")
		}
		return nil, err
	}
	if o.Settings == nil {
		o.Settings = map[string]any{}
	}
	return &o, nil
}

// GetOrganizationTx: organization mana pun (tabel organizations tidak ber-RLS; akses dibatasi di pemanggil).
func GetOrganizationTx(ctx context.Context, q db.Querier, id uuid.UUID) (*Organization, error) {
	return scanOrg(q.QueryRow(ctx, orgSelect+` WHERE id = $1`, id))
}

func (s *Service) GetOrganization(ctx context.Context) (*Organization, error) {
	p := authctx.Must(ctx)
	var o *Organization
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		o, err = GetOrganizationTx(ctx, tx, p.OrganizationID)
		return err
	})
	return o, err
}

func (s *Service) UpdateOrganization(ctx context.Context, in OrganizationInput, ifVersion *int) (*Organization, error) {
	p := authctx.Must(ctx)
	var out *Organization
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = UpdateOrganizationTx(ctx, tx, p.OrganizationID, in, ifVersion)
		return err
	})
	return out, err
}

// UpdateOrganizationTx: update profil + audit before/after (dipakai /organizations/me dan registry Platform Admin).
func UpdateOrganizationTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in OrganizationInput, ifVersion *int) (*Organization, error) {
	p := authctx.Must(ctx)
	before, err := GetOrganizationTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if ifVersion != nil && *ifVersion != before.Version {
		return nil, apperr.StaleVersion()
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, apperr.Validation("name tidak boleh kosong").WithField("name", "wajib")
	}
	if in.Timezone != nil {
		if _, err := time.LoadLocation(*in.Timezone); err != nil {
			return nil, apperr.Validation("timezone tidak valid").WithField("timezone", "tidak valid")
		}
	}
	if in.Email != nil && *in.Email != "" && !strings.Contains(*in.Email, "@") {
		return nil, apperr.Validation("email tidak valid").WithField("email", "tidak valid")
	}
	if in.Country != nil && len(strings.TrimSpace(*in.Country)) != 2 {
		return nil, apperr.Validation("country harus kode ISO 3166-1 alpha-2").WithField("country", "2 huruf")
	}
	var actor *uuid.UUID
	if !p.IsSystem {
		actor = &p.UserID
	}
	_, err = tx.Exec(ctx, `UPDATE organizations SET
		name = COALESCE(NULLIF(trim($2),''), name), legal_name = COALESCE($3, legal_name), timezone = COALESCE($4, timezone),
		email = COALESCE($5, email), phone = COALESCE($6, phone), website = COALESCE($7, website), address = COALESCE($8, address),
		city = COALESCE($9, city), province = COALESCE($10, province), postal_code = COALESCE($11, postal_code),
		country = COALESCE(upper($12), country), tax_id = COALESCE($13, tax_id), industry = COALESCE($14, industry),
		settings = COALESCE($15, settings), updated_by = $16
		WHERE id = $1`, id, in.Name, in.LegalName, in.Timezone, in.Email, in.Phone, in.Website, in.Address, in.City, in.Province,
		in.PostalCode, in.Country, in.TaxID, in.Industry, jsonOrNil(in.Settings), actor)
	if err != nil {
		return nil, err
	}
	after, err := GetOrganizationTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	_ = audit.LogAs(ctx, tx, id, actor, p.IP, p.UserAgent, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "organization", EntityID: &id, EntityLabel: before.Code, Before: before, After: after})
	return after, nil
}

// SetOrganizationStatusTx: aktivasi/deaktivasi/suspensi organization (Platform Admin). Seluruh user dipaksa
// memuat ulang principal (permission_version++) sehingga token yang masih berlaku langsung ditolak.
func SetOrganizationStatusTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, status, reason string) (*Organization, error) {
	p := authctx.Must(ctx)
	if status != "active" && status != "inactive" && status != "suspended" {
		return nil, apperr.Validation("status harus active|inactive|suspended").WithField("status", "tidak valid")
	}
	before, err := GetOrganizationTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if before.IsInternal && status != "active" {
		return nil, apperr.Forbidden("Organization internal BuildingVision tidak dapat dinonaktifkan")
	}
	if status != "active" && strings.TrimSpace(reason) == "" {
		return nil, apperr.Validation("reason wajib saat menonaktifkan/menangguhkan").WithField("reason", "wajib")
	}
	if before.Status == status {
		return before, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE organizations SET status = $2, is_active = ($2 = 'active'), status_reason = NULLIF($3,''), status_changed_at = now(), updated_by = $4 WHERE id = $1`,
		id, status, reason, p.UserID); err != nil {
		return nil, err
	}
	// sesi user organization tersebut divalidasi ulang (lintas org → set konteks RLS sementara)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.organization_id', $1, true)`, id.String()); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET permission_version = permission_version + 1 WHERE organization_id = $1`, id); err != nil {
		return nil, err
	}
	if status != "active" {
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE organization_id = $1 AND revoked_at IS NULL`, id); err != nil {
			return nil, err
		}
	}
	// audit dicatat di organization target (jejak terlihat oleh admin organization tsb) dan organization pelaku
	entry := audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "organization", EntityID: &id, EntityLabel: before.Code,
		Before: map[string]any{"status": before.Status}, After: map[string]any{"status": status, "reason": reason, "by_organization_id": p.OrganizationID}}
	_ = audit.LogAs(ctx, tx, id, nil, p.IP, p.UserAgent, entry)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.organization_id', $1, true)`, p.OrganizationID.String()); err != nil {
		return nil, err
	}
	_ = audit.Log(ctx, tx, entry)
	return GetOrganizationTx(ctx, tx, id)
}
