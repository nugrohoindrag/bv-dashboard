package buildingmap

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Occupancy (PRD P1 v2 §8): Unit · status unit · status occupancy · referensi occupant ----------

type OccupancyCounts struct {
	Total    int `json:"total"`
	Vacant   int `json:"vacant"`
	Occupied int `json:"occupied"`
	Reserved int `json:"reserved"`
	Inactive int `json:"inactive"`
	// occupancy rate = occupied / (total - inactive)
	OccupancyPct float64 `json:"occupancy_pct"`
}

type OccupancyGroup struct {
	LocationID   uuid.UUID `json:"location_id"`
	LocationType string    `json:"location_type"`
	Name         string    `json:"name"`
	PathText     string    `json:"path_text"` // membedakan lantai bernama sama di tower berbeda
	OccupancyCounts
}

type OccupancySummary struct {
	OccupancyCounts
	ByUnitType map[string]int   `json:"by_unit_type"`
	Buildings  []OccupancyGroup `json:"buildings"`
	Floors     []OccupancyGroup `json:"floors"`
}

type OccupancyUnit struct {
	LocationID      uuid.UUID     `json:"location_id"`
	Code            string        `json:"code"`
	UnitNumber      string        `json:"unit_number"`
	UnitType        string        `json:"unit_type"`
	PathText        string        `json:"path_text"`
	UnitStatus      string        `json:"unit_status"` // active | inactive (status lokasi)
	OccupancyStatus string        `json:"occupancy_status"`
	AreaM2          *float64      `json:"area_m2"`
	Tenant          *OccupantRef  `json:"tenant"`
	Occupants       []OccupantRef `json:"occupants"`
}

type OccupantRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type OccupancyFilter struct {
	PropertyID *uuid.UUID
	LocationID *uuid.UUID // subtree (building/tower/floor/area)
	Statuses   []string
	UnitTypes  []string
	Q          string
}

func (f *OccupancyCounts) finish() {
	base := f.Total - f.Inactive
	if base > 0 {
		f.OccupancyPct = float64(int(float64(f.Occupied)*1000/float64(base))) / 10
	}
}

// unitWhere: filter unit + scope permission (property.occupancy.view).
func unitWhere(p *authctx.Principal, f OccupancyFilter, add func(any) string) (string, error) {
	where := " WHERE l.deleted_at IS NULL AND l.location_type = 'unit'"
	if f.PropertyID != nil {
		if !p.HasAnyOnProperty("property.occupancy.view", *f.PropertyID) {
			return "", apperr.Forbidden("")
		}
		where += " AND l.property_id = " + add(*f.PropertyID)
	}
	where += " AND " + p.ScopeSQL("property.occupancy.view", "l.property_id", "l.path", add)
	if f.LocationID != nil {
		where += " AND l.path <@ (SELECT r.path FROM locations r WHERE r.id = " + add(*f.LocationID) + ")"
	}
	if len(f.Statuses) > 0 {
		where += " AND (CASE WHEN NOT l.is_active THEN 'inactive' ELSE u.occupancy_status END) = ANY(" + add(f.Statuses) + ")"
	}
	if len(f.UnitTypes) > 0 {
		where += " AND u.unit_type = ANY(" + add(f.UnitTypes) + ")"
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		v := add("%" + q + "%")
		where += " AND (u.unit_number ILIKE " + v + " OR l.name ILIKE " + v + " OR l.code ILIKE " + v + " OR t.name ILIKE " + v + ")"
	}
	return where, nil
}

// statusExpr: unit nonaktif (lokasi inactive) dihitung Inactive walau occupancy_status lain.
const statusExpr = `CASE WHEN NOT l.is_active THEN 'inactive' ELSE u.occupancy_status END`

func (s *Service) OccupancySummary(ctx context.Context, f OccupancyFilter) (*OccupancySummary, error) {
	p := authctx.Must(ctx)
	out := &OccupancySummary{ByUnitType: map[string]int{}, Buildings: []OccupancyGroup{}, Floors: []OccupancyGroup{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where, err := unitWhere(p, f, add)
		if err != nil {
			return err
		}
		base := `FROM locations l JOIN units u ON u.location_id = l.id LEFT JOIN tenants t ON t.id = u.tenant_id` + where
		row := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE `+statusExpr+` = 'vacant'), count(*) FILTER (WHERE `+statusExpr+` = 'occupied'),
			count(*) FILTER (WHERE `+statusExpr+` = 'reserved'), count(*) FILTER (WHERE `+statusExpr+` = 'inactive') `+base, args...)
		if err := row.Scan(&out.Total, &out.Vacant, &out.Occupied, &out.Reserved, &out.Inactive); err != nil {
			return err
		}
		out.finish()
		rows, err := tx.Query(ctx, `SELECT u.unit_type, count(*) `+base+` GROUP BY u.unit_type`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var t string
			var n int
			if err := rows.Scan(&t, &n); err != nil {
				rows.Close()
				return err
			}
			out.ByUnitType[t] = n
		}
		rows.Close()
		for _, level := range []string{"building", "floor"} {
			q := `SELECT g.id, g.location_type, g.name, count(*), count(*) FILTER (WHERE ` + statusExpr + ` = 'vacant'), count(*) FILTER (WHERE ` + statusExpr + ` = 'occupied'),
				count(*) FILTER (WHERE ` + statusExpr + ` = 'reserved'), count(*) FILTER (WHERE ` + statusExpr + ` = 'inactive')
				FROM locations l JOIN units u ON u.location_id = l.id LEFT JOIN tenants t ON t.id = u.tenant_id
				JOIN locations g ON g.location_type = '` + level + `' AND l.path <@ g.path AND g.deleted_at IS NULL` + where + `
				GROUP BY g.id, g.location_type, g.name, g.path ORDER BY g.path`
			rows, err := tx.Query(ctx, q, args...)
			if err != nil {
				return err
			}
			var list []OccupancyGroup
			for rows.Next() {
				var g OccupancyGroup
				if err := rows.Scan(&g.LocationID, &g.LocationType, &g.Name, &g.Total, &g.Vacant, &g.Occupied, &g.Reserved, &g.Inactive); err != nil {
					rows.Close()
					return err
				}
				g.finish()
				list = append(list, g)
			}
			rows.Close()
			for i := range list {
				list[i].PathText = property.LocationPathText(ctx, tx, list[i].LocationID)
			}
			if list == nil {
				list = []OccupancyGroup{}
			}
			if level == "building" {
				out.Buildings = list
			} else {
				out.Floors = list
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) OccupancyUnits(ctx context.Context, f OccupancyFilter, limit int) ([]OccupancyUnit, error) {
	p := authctx.Must(ctx)
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	out := []OccupancyUnit{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where, err := unitWhere(p, f, add)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT l.id, l.code, u.unit_number, u.unit_type, CASE WHEN l.is_active THEN 'active' ELSE 'inactive' END, `+statusExpr+`, u.area_m2::float8, t.id, t.name,
			(SELECT string_agg(a.name, ' / ' ORDER BY a.depth) FROM locations a WHERE a.path @> l.path AND a.depth > 0)
			FROM locations l JOIN units u ON u.location_id = l.id LEFT JOIN tenants t ON t.id = u.tenant_id`+where+` ORDER BY l.path LIMIT `+add(limit), args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var u OccupancyUnit
			var tid *uuid.UUID
			var tname, path *string
			if err := rows.Scan(&u.LocationID, &u.Code, &u.UnitNumber, &u.UnitType, &u.UnitStatus, &u.OccupancyStatus, &u.AreaM2, &tid, &tname, &path); err != nil {
				rows.Close()
				return err
			}
			if tid != nil && tname != nil {
				u.Tenant = &OccupantRef{ID: *tid, Name: *tname}
			}
			if path != nil {
				u.PathText = *path
			}
			u.Occupants = []OccupantRef{}
			out = append(out, u)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// referensi occupant aktif per unit
		for i := range out {
			orows, err := tx.Query(ctx, `SELECT o.id, o.full_name FROM unit_occupants uo JOIN occupants o ON o.id = uo.occupant_id
				WHERE uo.unit_location_id = $1 AND uo.moved_out_at IS NULL AND o.status = 'active' ORDER BY o.full_name LIMIT 10`, out[i].LocationID)
			if err != nil {
				return err
			}
			for orows.Next() {
				var o OccupantRef
				if err := orows.Scan(&o.ID, &o.Name); err != nil {
					orows.Close()
					return err
				}
				out[i].Occupants = append(out[i].Occupants, o)
			}
			orows.Close()
		}
		return nil
	})
	return out, err
}
