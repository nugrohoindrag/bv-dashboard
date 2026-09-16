// Package tenantapp: API Mobile Tenant (`/api/v1/tenant/*`) — consumer dari core BuildingVision (PRD P1 v1.3 §7–§20, §30–§31).
// Semua akses dibatasi server-side oleh tenantscope.Scope; UI filtering bukan security boundary.
package tenantapp

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/tenantscope"
)

// Alias agar pemanggil lama (tenantrelation) tetap kompatibel.
type (
	Scope     = tenantscope.Scope
	AccessLoc = tenantscope.AccessLoc
)

var LoadScopeTx = tenantscope.LoadScopeTx

// ReportableLocation: lokasi yang dapat dipilih tenant saat Report an Issue (PRD §10.2).
type ReportableLocation struct {
	ID           uuid.UUID `json:"id"`
	LocationType string    `json:"location_type"`
	Name         string    `json:"name"`
	PathText     string    `json:"path_text"`
	Scope        string    `json:"scope"` // unit | common_area | other
	IsPrimary    bool      `json:"is_primary"`
}

// ReportableLocationsTx: unit tenant + common area yang diizinkan property (tenant_reportable) + area akses khusus.
func ReportableLocationsTx(ctx context.Context, tx pgx.Tx, sc *Scope) ([]ReportableLocation, error) {
	out := []ReportableLocation{}
	for _, u := range sc.Units {
		out = append(out, ReportableLocation{ID: u.ID, LocationType: u.LocationType, Name: u.Name, PathText: u.PathText, Scope: "unit", IsPrimary: u.IsPrimary})
	}
	for _, a := range sc.Areas {
		out = append(out, ReportableLocation{ID: a.ID, LocationType: a.LocationType, Name: a.Name, PathText: a.PathText, Scope: "other"})
	}
	rows, err := tx.Query(ctx, `
		SELECT l.id, l.location_type, l.name,
		       COALESCE((SELECT string_agg(a.name, ' · ' ORDER BY a.depth) FROM locations a WHERE a.path @> l.path AND a.depth > 0 AND a.id <> l.id), '')
		FROM locations l WHERE l.property_id = $1 AND l.location_type IN ('area','space') AND l.is_active AND l.deleted_at IS NULL AND l.tenant_reportable
		ORDER BY l.name LIMIT 500`, sc.PropertyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[uuid.UUID]bool{}
	for _, o := range out {
		seen[o.ID] = true
	}
	for rows.Next() {
		var r ReportableLocation
		if err := rows.Scan(&r.ID, &r.LocationType, &r.Name, &r.PathText); err != nil {
			return nil, err
		}
		if seen[r.ID] {
			continue
		}
		r.Scope = "common_area"
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResolveLocationTx: validasi lokasi laporan (guardrail #2–#4): unit → harus dalam tenant_access; area/space → harus
// tenant_reportable pada property tenant atau ada akses khusus. Mengembalikan area_scope.
func ResolveLocationTx(ctx context.Context, tx pgx.Tx, sc *Scope, locationID uuid.UUID) (string, error) {
	if sc.HasUnit(locationID) {
		return "unit", nil
	}
	for _, a := range sc.Areas {
		if a.ID == locationID {
			return "other", nil
		}
	}
	var lt string
	var reportable bool
	var propertyID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT location_type, tenant_reportable, property_id FROM locations WHERE id = $1 AND deleted_at IS NULL AND is_active`, locationID).Scan(&lt, &reportable, &propertyID)
	if err != nil || propertyID != sc.PropertyID {
		return "", apperr.New(403, "LOCATION_NOT_AUTHORIZED", "Location not authorized", "Lokasi tidak berada dalam scope akses Anda")
	}
	if (lt == "area" || lt == "space") && reportable {
		return "common_area", nil
	}
	return "", apperr.New(403, "LOCATION_NOT_AUTHORIZED", "Location not authorized", "Lokasi tidak berada dalam scope akses Anda")
}

// srOwnershipWhere: klausa WHERE kepemilikan SR untuk tenant user (tenant_admin melihat seluruh SR tenant-nya).
func srOwnershipWhere(sc *Scope, argIdx int) (string, []any) {
	if sc.Role == "tenant_admin" && sc.TenantID != nil {
		return "(sr.tenant_user_id = $" + itoa(argIdx) + " OR sr.tenant_id = $" + itoa(argIdx+1) + ")", []any{sc.UserID, *sc.TenantID}
	}
	return "sr.tenant_user_id = $" + itoa(argIdx), []any{sc.UserID}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
