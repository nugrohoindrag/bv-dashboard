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
	"github.com/buildingvision/api/internal/searchindex"
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

// PRD P0 v2 §17.1: + user (User Name), vendor (Vendor Name), finding, dan seluruh level lokasi.
var indexableTypes = map[string]bool{"property": true, "building": true, "tower": true, "floor": true, "area": true, "space": true, "unit": true, "location": true,
	"tenant": true, "asset": true, "task": true, "work_order": true, "service_request": true, "incident": true, "finding": true, "user": true, "vendor": true}

var locationTypes = searchindex.LocationTypes

func indexable(t string) bool { return indexableTypes[t] }

// Index: upsert search_documents untuk satu object (idempotent).
func (s *Service) Index(ctx context.Context, orgID uuid.UUID, objectType string, objectID uuid.UUID) error {
	ctx = authctx.With(ctx, authctx.System(orgID))
	return s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		return IndexTx(ctx, tx, orgID, objectType, objectID)
	})
}

// IndexTx: lihat searchindex.IndexTx (dipisah agar modul IAM/vendor dapat meng-index tanpa siklus import).
func IndexTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, objectType string, objectID uuid.UUID) error {
	return searchindex.IndexTx(ctx, tx, orgID, objectType, objectID)
}

// Reindex: backfill seluruh object satu org (bvctl reindex).
func (s *Service) Reindex(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	n := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		for _, src := range []struct{ ot, q string }{
			{"task", `SELECT id FROM tasks`}, {"work_order", `SELECT id FROM work_orders`}, {"service_request", `SELECT id FROM service_requests`},
			{"incident", `SELECT id FROM incidents`}, {"asset", `SELECT id FROM assets WHERE deleted_at IS NULL`}, {"tenant", `SELECT id FROM tenants WHERE deleted_at IS NULL`},
			{"location", `SELECT id FROM locations WHERE deleted_at IS NULL`},
			{"finding", `SELECT id FROM findings`}, {"user", `SELECT id FROM users WHERE deleted_at IS NULL`}, {"vendor", `SELECT id FROM vendors`},
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
	"finding": "operations.findings.view", "asset": "engineering.assets.view", "tenant": "property.tenants.view", "user": "iam.users.view", "vendor": "vendor.vendors.view",
	"location": "property.locations.view", "property": "property.locations.view", "building": "property.locations.view", "tower": "property.locations.view",
	"floor": "property.locations.view", "area": "property.locations.view", "space": "property.locations.view", "unit": "property.locations.view"}

// deepLinks: semua level lokasi membuka detail lokasi generik (route /property/locations/:id).
var deepLinks = map[string]string{"task": "/operations/tasks/", "work_order": "/operations/work-orders/", "service_request": "/operations/service-requests/", "incident": "/operations/incidents/",
	"finding": "/findings/", "asset": "/assets/", "tenant": "/tenant/tenants/", "user": "/settings/users?user=", "vendor": "/vendors/",
	"location": "/property/locations/", "property": "/property/locations/", "building": "/property/locations/", "tower": "/property/locations/",
	"floor": "/property/locations/", "area": "/property/locations/", "space": "/property/locations/", "unit": "/property/locations/"}

// objectPathSQL: ltree lokasi object untuk pemeriksaan scope building (PRD P0 v2 §8.4).
var objectPathSQL = map[string]string{
	"task":            `SELECT l.path::text FROM tasks o JOIN locations l ON l.id = o.location_id WHERE o.id = $1`,
	"work_order":      `SELECT l.path::text FROM work_orders o JOIN locations l ON l.id = o.location_id WHERE o.id = $1`,
	"service_request": `SELECT l.path::text FROM service_requests o JOIN locations l ON l.id = o.location_id WHERE o.id = $1`,
	"incident":        `SELECT l.path::text FROM incidents o JOIN locations l ON l.id = o.location_id WHERE o.id = $1`,
	"finding":         `SELECT l.path::text FROM findings o JOIN locations l ON l.id = o.location_id WHERE o.id = $1`,
	"asset":           `SELECT l.path::text FROM assets o JOIN locations l ON l.id = o.location_id WHERE o.id = $1`,
}

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
			WHERE (d.tsv @@ websearch_to_tsquery('simple', $1) OR COALESCE(d.business_id,'') ILIKE '%' || $1 || '%' OR d.title ILIKE '%' || $1 || '%' OR COALESCE(d.subtitle,'') ILIKE '%' || $1 || '%' OR similarity(d.title, $1) > 0.3)`+where+`
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
			if p.VendorID != nil && !p.IsSystem {
				continue // akun vendor tidak memakai global search staf
			}
			if r.PropertyID != nil {
				if !p.HasOnProperty(perm, *r.PropertyID) {
					// grant ber-scope building: object harus berada di subtree scope
					oid, ot := r.ObjectID, r.ObjectType
					if !p.HasOnPropertyAt(perm, *r.PropertyID, func() string { return objectPath(ctx, tx, ot, oid) }) {
						continue
					}
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

func objectPath(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID) string {
	var path string
	if q, ok := objectPathSQL[objectType]; ok {
		_ = tx.QueryRow(ctx, q, id).Scan(&path)
		return path
	}
	if locationTypes[objectType] || objectType == "location" {
		_ = tx.QueryRow(ctx, `SELECT path::text FROM locations WHERE id = $1`, id).Scan(&path)
	}
	return path
}
