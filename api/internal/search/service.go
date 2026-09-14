// Package search: global search PostgreSQL full-text + pg_trgm (PRD §23; TAD §5.15; Naming Convention §54).
// Hasil: Object · Type · Location · Status.
package search

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
)

type Service struct {
	DB *db.DB
}

func (s *Service) Name() string { return "search" }

// Handle: subscriber domain event → index object (fallback selain job search.index).
func (s *Service) Handle(ctx context.Context, ev events.Event) error {
	if !indexable(ev.ObjectType) {
		return nil
	}
	return s.Index(ctx, ev.OrganizationID, ev.ObjectType, ev.ObjectID)
}

var indexableTypes = map[string]bool{"property": true, "building": true, "unit": true, "location": true, "tenant": true, "asset": true, "task": true, "work_order": true, "service_request": true, "incident": true}

func indexable(t string) bool { return indexableTypes[t] }

// Index: upsert search_documents untuk satu object (idempotent).
func (s *Service) Index(ctx context.Context, orgID uuid.UUID, objectType string, objectID uuid.UUID) error {
	ctx = authctx.With(ctx, authctx.System(orgID))
	return s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		return IndexTx(ctx, tx, orgID, objectType, objectID)
	})
}

func IndexTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, objectType string, objectID uuid.UUID) error {
	var q string
	switch objectType {
	case "task":
		q = `SELECT t.property_id, t.task_number, t.title, t.task_type, t.location_id, t.status FROM tasks t WHERE t.id = $1`
	case "work_order":
		q = `SELECT w.property_id, w.work_order_number, w.title, w.work_order_type, w.location_id, w.status FROM work_orders w WHERE w.id = $1`
	case "service_request":
		q = `SELECT sr.property_id, sr.request_number, sr.title, sr.category_code, sr.location_id, sr.status FROM service_requests sr WHERE sr.id = $1`
	case "incident":
		q = `SELECT i.property_id, i.incident_number, i.title, i.category, i.location_id, i.status FROM incidents i WHERE i.id = $1`
	case "asset":
		q = `SELECT a.property_id, a.asset_code, a.name, e.category_name || COALESCE(' · ' || e.type_name, ''), a.location_id, a.status FROM assets a JOIN equipment e ON e.id = a.equipment_id WHERE a.id = $1 AND a.deleted_at IS NULL`
	case "tenant":
		q = `SELECT t.property_id, t.tenant_code, t.name, COALESCE(t.contact_name,''), (SELECT u.location_id FROM units u WHERE u.tenant_id = t.id LIMIT 1), t.status FROM tenants t WHERE t.id = $1 AND t.deleted_at IS NULL`
	case "location", "property", "building", "unit":
		q = `SELECT l.property_id, l.code, l.name, l.location_type, l.id, CASE WHEN l.is_active THEN 'active' ELSE 'inactive' END FROM locations l WHERE l.id = $1 AND l.deleted_at IS NULL`
	default:
		return nil
	}
	var propertyID *uuid.UUID
	var businessID, title, subtitle, status string
	var locID *uuid.UUID
	if err := tx.QueryRow(ctx, q, objectID).Scan(&propertyID, &businessID, &title, &subtitle, &locID, &status); err != nil {
		if db.IsNoRows(err) {
			_, _ = tx.Exec(ctx, `DELETE FROM search_documents WHERE object_type = $1 AND object_id = $2`, objectType, objectID)
			return nil
		}
		return err
	}
	ot := objectType
	if ot == "location" || ot == "property" || ot == "building" || ot == "unit" {
		ot = "location"
		if subtitle == "property" || subtitle == "building" || subtitle == "unit" {
			ot = subtitle
		}
	}
	var locPath *string
	if locID != nil {
		_ = tx.QueryRow(ctx, `SELECT string_agg(a.name, ' / ' ORDER BY a.depth) FROM locations l JOIN locations a ON a.path @> l.path AND a.depth > 0 WHERE l.id = $1`, *locID).Scan(&locPath)
	}
	_, err := tx.Exec(ctx, `INSERT INTO search_documents (organization_id, property_id, object_type, object_id, business_id, title, subtitle, location_path, status, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
		ON CONFLICT (object_type, object_id) DO UPDATE SET property_id = EXCLUDED.property_id, business_id = EXCLUDED.business_id, title = EXCLUDED.title, subtitle = EXCLUDED.subtitle, location_path = EXCLUDED.location_path, status = EXCLUDED.status, updated_at = now()`,
		orgID, propertyID, ot, objectID, businessID, title, subtitle, locPath, status)
	return err
}

// Reindex: backfill seluruh object satu org (bvctl reindex).
func (s *Service) Reindex(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	n := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		for _, src := range []struct{ ot, q string }{
			{"task", `SELECT id FROM tasks`}, {"work_order", `SELECT id FROM work_orders`}, {"service_request", `SELECT id FROM service_requests`},
			{"incident", `SELECT id FROM incidents`}, {"asset", `SELECT id FROM assets WHERE deleted_at IS NULL`}, {"tenant", `SELECT id FROM tenants WHERE deleted_at IS NULL`},
			{"location", `SELECT id FROM locations WHERE deleted_at IS NULL AND location_type IN ('property','building','unit')`},
		} {
			rows, err := tx.Query(ctx, src.q)
			if err != nil {
				return err
			}
			var idList []uuid.UUID
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil {
					idList = append(idList, id)
				}
			}
			rows.Close()
			for _, id := range idList {
				if err := IndexTx(ctx, tx, orgID, src.ot, id); err != nil {
					return err
				}
				n++
			}
		}
		return nil
	})
	return n, err
}

type Result struct {
	ObjectType   string     `json:"object_type"`
	ObjectID     uuid.UUID  `json:"object_id"`
	BusinessID   *string    `json:"business_id"`
	Title        string     `json:"title"`
	Subtitle     *string    `json:"subtitle"`
	LocationPath *string    `json:"location_path"`
	Status       *string    `json:"status"`
	PropertyID   *uuid.UUID `json:"property_id"`
	Rank         float64    `json:"rank"`
	DeepLink     string     `json:"deep_link"`
}

var viewPerm = map[string]string{"task": "operations.tasks.view", "work_order": "operations.work_orders.view", "service_request": "tenant.service_requests.view", "incident": "operations.incidents.view",
	"asset": "engineering.assets.view", "tenant": "property.tenants.view", "location": "property.locations.view", "property": "property.locations.view", "building": "property.locations.view", "unit": "property.locations.view"}

var deepLinks = map[string]string{"task": "/operations/tasks/", "work_order": "/operations/work-orders/", "service_request": "/operations/service-requests/", "incident": "/operations/incidents/",
	"asset": "/assets/", "tenant": "/tenant/tenants/", "location": "/property/locations/", "property": "/property/properties/", "building": "/property/buildings/", "unit": "/property/units/"}

// Query: websearch tsquery OR trigram similarity pada business_id/title (pencarian parsial WO-2026-0001).
func (s *Service) Query(ctx context.Context, q string, propertyID *uuid.UUID, types []string, limit int) ([]Result, error) {
	p := authctx.Must(ctx)
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return []Result{}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var out []Result
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{q, limit * 3}
		where := ""
		if propertyID != nil {
			args = append(args, *propertyID)
			where += " AND (d.property_id = $3 OR d.property_id IS NULL)"
		}
		if len(types) > 0 {
			args = append(args, types)
			where += " AND d.object_type = ANY($" + itoa(len(args)) + ")"
		}
		rows, err := tx.Query(ctx, `
			SELECT d.object_type, d.object_id, d.business_id, d.title, d.subtitle, d.location_path, d.status, d.property_id,
			  GREATEST(ts_rank(d.tsv, websearch_to_tsquery('simple', $1)), similarity(COALESCE(d.business_id,''), $1), similarity(d.title, $1)) AS rank
			FROM search_documents d
			WHERE (d.tsv @@ websearch_to_tsquery('simple', $1) OR COALESCE(d.business_id,'') ILIKE '%' || $1 || '%' OR d.title ILIKE '%' || $1 || '%' OR similarity(d.title, $1) > 0.3)`+where+`
			ORDER BY rank DESC, d.updated_at DESC LIMIT $2`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r Result
			if err := rows.Scan(&r.ObjectType, &r.ObjectID, &r.BusinessID, &r.Title, &r.Subtitle, &r.LocationPath, &r.Status, &r.PropertyID, &r.Rank); err != nil {
				return err
			}
			// permission per object type & property scope
			perm := viewPerm[r.ObjectType]
			if perm == "" {
				continue
			}
			if r.PropertyID != nil {
				if !p.HasOnProperty(perm, *r.PropertyID) {
					continue
				}
			} else if !p.Has(perm) {
				continue
			}
			r.DeepLink = deepLinks[r.ObjectType] + r.ObjectID.String()
			out = append(out, r)
			if len(out) >= limit {
				break
			}
		}
		return rows.Err()
	})
	if out == nil {
		out = []Result{}
	}
	return out, err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

var _ = time.Now
