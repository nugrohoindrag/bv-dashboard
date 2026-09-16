// Package tenantscope: konteks akun Mobile Tenant (tenant_users + tenant_access) untuk otorisasi server-side (TD-P1-003).
// Dipakai tenantapp, booking, visitor, billing — UI filtering bukan security boundary (PRD §30–§31).
package tenantscope

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
)

// AccessLoc: unit/area yang dapat diakses tenant user.
type AccessLoc struct {
	ID           uuid.UUID  `json:"id"`
	AccessID     uuid.UUID  `json:"access_id"`
	LocationType string     `json:"location_type"`
	Name         string     `json:"name"`
	Code         string     `json:"code"`
	UnitNumber   *string    `json:"unit_number,omitempty"`
	PathText     string     `json:"path_text"`
	IsPrimary    bool       `json:"is_primary"`
	AccessType   string     `json:"access_type"`
	ValidFrom    *time.Time `json:"valid_from,omitempty"`
	ValidUntil   *time.Time `json:"valid_until,omitempty"`
}

// Scope: konteks akun tenant untuk satu request (TD-P1-003).
type Scope struct {
	TenantUserID    uuid.UUID
	UserID          uuid.UUID
	OrganizationID  uuid.UUID
	PropertyID      uuid.UUID
	TenantID        *uuid.UUID
	OccupantID      *uuid.UUID
	Role            string // tenant_user | tenant_admin
	Status          string
	OwnershipStatus *string
	Units           []AccessLoc
	Areas           []AccessLoc
}

func (sc *Scope) PrimaryUnit() *AccessLoc {
	for i := range sc.Units {
		if sc.Units[i].IsPrimary {
			return &sc.Units[i]
		}
	}
	if len(sc.Units) > 0 {
		return &sc.Units[0]
	}
	return nil
}

func (sc *Scope) HasUnit(locationID uuid.UUID) bool {
	for _, u := range sc.Units {
		if u.ID == locationID {
			return true
		}
	}
	return false
}

// LoadScopeTx: akun tenant aktif untuk principal saat ini; 403 bila bukan tenant / belum aktif.
func LoadScopeTx(ctx context.Context, tx pgx.Tx) (*Scope, error) {
	p := authctx.Must(ctx)
	if !p.IsTenant {
		return nil, apperr.Forbidden("Endpoint ini hanya untuk akun Tenant App")
	}
	sc := &Scope{UserID: p.UserID, OrganizationID: p.OrganizationID}
	err := tx.QueryRow(ctx, `SELECT id, property_id, tenant_id, occupant_id, role, status, ownership_status FROM tenant_users WHERE user_id = $1`, p.UserID).
		Scan(&sc.TenantUserID, &sc.PropertyID, &sc.TenantID, &sc.OccupantID, &sc.Role, &sc.Status, &sc.OwnershipStatus)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.Forbidden("Akun tenant tidak ditemukan")
		}
		return nil, err
	}
	if sc.Status != "active" {
		return nil, apperr.New(403, "ACCOUNT_"+strMap(sc.Status), "Account not active", "Akun tenant tidak aktif")
	}
	rows, err := tx.Query(ctx, `
		SELECT ta.id, l.id, l.location_type, l.name, l.code, u.unit_number, ta.is_primary, ta.access_type, ta.valid_from, ta.valid_until,
		       COALESCE((SELECT string_agg(a.name, ' · ' ORDER BY a.depth) FROM locations a WHERE a.path @> l.path AND a.depth > 0 AND a.id <> l.id), '')
		FROM tenant_access ta JOIN locations l ON l.id = ta.location_id LEFT JOIN units u ON u.location_id = l.id
		WHERE ta.tenant_user_id = $1 AND ta.status = 'active' AND l.deleted_at IS NULL
		  AND (ta.valid_from IS NULL OR ta.valid_from <= current_date) AND (ta.valid_until IS NULL OR ta.valid_until >= current_date)
		ORDER BY ta.is_primary DESC, l.name`, sc.TenantUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a AccessLoc
		if err := rows.Scan(&a.AccessID, &a.ID, &a.LocationType, &a.Name, &a.Code, &a.UnitNumber, &a.IsPrimary, &a.AccessType, &a.ValidFrom, &a.ValidUntil, &a.PathText); err != nil {
			return nil, err
		}
		if a.AccessType == "unit" {
			sc.Units = append(sc.Units, a)
		} else {
			sc.Areas = append(sc.Areas, a)
		}
	}
	if sc.Units == nil {
		sc.Units = []AccessLoc{}
	}
	if sc.Areas == nil {
		sc.Areas = []AccessLoc{}
	}
	return sc, rows.Err()
}

func strMap(status string) string {
	switch status {
	case "pending_validation":
		return "PENDING"
	case "rejected":
		return "REJECTED"
	case "suspended":
		return "SUSPENDED"
	}
	return "INACTIVE"
}
