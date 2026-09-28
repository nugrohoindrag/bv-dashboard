// Package searchindex: menulis dokumen search_documents per object (PRD §23, PRD P0 v2 §17.1).
// Terpisah dari package search (yang bergantung ke IAM untuk HTTP) agar IAM/vendor dapat meng-index langsung.
package searchindex

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/db"
)

// LocationTypes: seluruh level hierarki lokasi.
var LocationTypes = map[string]bool{"property": true, "building": true, "tower": true, "floor": true, "area": true, "space": true, "unit": true}

// IndexTx: upsert (atau hapus bila object sudah tidak ada) dokumen search satu object.
func IndexTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, objectType string, objectID uuid.UUID) error {
	var q string
	switch objectType {
	// PRD P1 v2 §40: Task dicari via assignee, WO via asset, Request via tenant — ikut di subtitle (bobot B)
	case "task":
		q = `SELECT t.property_id, t.task_number, t.title, t.task_type || COALESCE(' · ' || u.full_name, '') || COALESCE(' · ' || tm.name, '') || COALESCE(' · ' || t.category, ''), t.location_id, t.status
			FROM tasks t LEFT JOIN users u ON u.id = t.assignee_user_id LEFT JOIN teams tm ON tm.id = t.assignee_team_id WHERE t.id = $1`
	case "work_order":
		q = `SELECT w.property_id, w.work_order_number, w.title, w.work_order_type || COALESCE(' · ' || a.asset_code || ' ' || a.name, '') || COALESCE(' · ' || u.full_name, ''), w.location_id, w.status
			FROM work_orders w LEFT JOIN assets a ON a.id = w.asset_id LEFT JOIN users u ON u.id = w.assignee_user_id WHERE w.id = $1`
	case "service_request":
		q = `SELECT sr.property_id, sr.request_number, sr.title, sr.category_code || COALESCE(' · ' || t.name, '') || COALESCE(' · ' || sr.requester_name, ''), sr.location_id, sr.status
			FROM service_requests sr LEFT JOIN tenants t ON t.id = sr.tenant_id WHERE sr.id = $1`
	case "incident":
		q = `SELECT i.property_id, i.incident_number, i.title, i.category, i.location_id, i.status FROM incidents i WHERE i.id = $1`
	case "asset":
		q = `SELECT a.property_id, a.asset_code, a.name, e.category_name || COALESCE(' · ' || e.type_name, ''), a.location_id, a.status FROM assets a JOIN equipment e ON e.id = a.equipment_id WHERE a.id = $1 AND a.deleted_at IS NULL`
	case "tenant":
		q = `SELECT t.property_id, t.tenant_code, t.name, COALESCE(t.contact_name,''), (SELECT u.location_id FROM units u WHERE u.tenant_id = t.id LIMIT 1), t.status FROM tenants t WHERE t.id = $1 AND t.deleted_at IS NULL`
	case "location", "property", "building", "tower", "floor", "area", "space", "unit":
		q = `SELECT l.property_id, l.code, l.name, l.location_type, l.id, CASE WHEN l.is_active THEN 'active' ELSE 'inactive' END FROM locations l WHERE l.id = $1 AND l.deleted_at IS NULL`
	case "finding":
		q = `SELECT f.property_id, f.finding_number, f.title, f.finding_type, f.location_id, f.status FROM findings f WHERE f.id = $1`
	case "user":
		// staf saja (akun tenant tidak dicari lewat global search staf)
		q = `SELECT NULL::uuid, u.user_code, u.full_name, COALESCE(u.email, u.username, ''), NULL::uuid, CASE WHEN u.is_active THEN 'active' ELSE 'inactive' END
			FROM users u WHERE u.id = $1 AND u.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM tenant_users tu WHERE tu.user_id = u.id)`
	case "vendor":
		q = `SELECT NULL::uuid, v.vendor_code, v.name, array_to_string(v.service_categories, ', '), NULL::uuid, v.status FROM vendors v WHERE v.id = $1`
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
	if ot == "location" || LocationTypes[ot] {
		ot = "location"
		if LocationTypes[subtitle] {
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
