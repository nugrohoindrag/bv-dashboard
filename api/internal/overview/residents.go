package overview

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
)

// ---------- Sensus Penghuni (Building Management Overview, Roadmap v2.1 §25.3 Apartment "Unit Occupancy / Resident Service") ----------
// Ringkasan orang, bukan unit: penghuni aktif yang menempati unit, mutasi 30 hari, tenant aktif, dan akun Tenant App per
// status kepemilikan (owner/tenant/family/guest/employee) termasuk yang menunggu validasi. Unit (vacant/occupied) tetap
// dari GET /occupancy/summary.

type Residents struct {
	OccupantsActive    int            `json:"occupants_active"`
	UnitsWithOccupants int            `json:"units_with_occupants"`
	MovedIn30d         int            `json:"moved_in_30d"`
	MovedOut30d        int            `json:"moved_out_30d"`
	TenantsActive      int            `json:"tenants_active"`
	AppUsers           map[string]int `json:"app_users"` // ownership_status → akun Tenant App aktif ("unknown" bila kosong)
	PendingValidation  int            `json:"pending_validation"`
	Link               string         `json:"link"`
}

func (s *Service) Residents(ctx context.Context, propertyID *uuid.UUID) (*Residents, error) {
	if !authctx.Must(ctx).Has("property.occupants.view") {
		return nil, apperr.Forbidden("")
	}
	sq, err := s.scope(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	v, err := s.cached(cacheKey(ctx, "residents", propertyID, ""), func() (any, error) {
		out := &Residents{AppUsers: map[string]int{}, Link: "/tenant/tenants"}
		err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			a := sq.args
			if err := tx.QueryRow(ctx, `
				SELECT count(DISTINCT o.id) FILTER (WHERE uo.moved_out_at IS NULL),
				       count(DISTINCT uo.unit_location_id) FILTER (WHERE uo.moved_out_at IS NULL),
				       count(DISTINCT o.id) FILTER (WHERE uo.moved_out_at IS NULL AND uo.moved_in_at >= current_date - 30),
				       count(DISTINCT o.id) FILTER (WHERE uo.moved_out_at >= current_date - 30)
				FROM unit_occupants uo
				JOIN occupants o ON o.id = uo.occupant_id AND o.deleted_at IS NULL AND o.status = 'active'
				JOIN locations l ON l.id = uo.unit_location_id
				WHERE true`+sq.where("l.property_id", "l.id"), a...).Scan(&out.OccupantsActive, &out.UnitsWithOccupants, &out.MovedIn30d, &out.MovedOut30d); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM tenants t WHERE t.deleted_at IS NULL AND t.status = 'active'`+sq.where("t.property_id", ""), a...).Scan(&out.TenantsActive); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `
				SELECT COALESCE(tu.ownership_status, 'unknown'), count(*) FILTER (WHERE tu.status = 'active'), count(*) FILTER (WHERE tu.status = 'pending_validation')
				FROM tenant_users tu WHERE true`+sq.where("tu.property_id", "")+` GROUP BY 1`, a...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var k string
				var active, pending int
				if err := rows.Scan(&k, &active, &pending); err != nil {
					return err
				}
				if active > 0 {
					out.AppUsers[k] = active
				}
				out.PendingValidation += pending
			}
			return rows.Err()
		})
		return out, err
	})
	if err != nil {
		return nil, err
	}
	return v.(*Residents), nil
}
