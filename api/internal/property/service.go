// Package property: Organization → Property → Building → Tower → Floor → Area/Space/Unit (PRD §8, TAD §7.2, ADR-006),
// Tenant & Occupant (PRD §16.1).
package property

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
)

type Service struct {
	DB   *db.DB
	Jobs jobs.Enqueuer
}

// ---------- Location model ----------

type LocationType string

const (
	LTProperty LocationType = "property"
	LTBuilding LocationType = "building"
	LTTower    LocationType = "tower"
	LTFloor    LocationType = "floor"
	LTArea     LocationType = "area"
	LTSpace    LocationType = "space"
	LTUnit     LocationType = "unit"
)

// allowedParents: validasi parent-child di domain (OD-005) — Tower opsional: Floor boleh di bawah Building.
var allowedParents = map[LocationType][]LocationType{
	LTBuilding: {LTProperty},
	LTTower:    {LTBuilding},
	LTFloor:    {LTBuilding, LTTower},
	LTArea:     {LTFloor, LTBuilding, LTTower, LTProperty}, // area outdoor (garden, parking) boleh langsung di property/building
	LTSpace:    {LTFloor, LTArea},
	LTUnit:     {LTFloor, LTArea},
}

var prefixByType = map[LocationType]string{
	LTProperty: ids.PrefixProperty, LTBuilding: ids.PrefixBuilding, LTTower: ids.PrefixTower, LTFloor: ids.PrefixFloor,
	LTArea: ids.PrefixArea, LTSpace: ids.PrefixSpace, LTUnit: ids.PrefixUnit,
}

type PathItem struct {
	ID   uuid.UUID    `json:"id"`
	Type LocationType `json:"type"`
	Name string       `json:"name"`
}

type Location struct {
	ID           uuid.UUID      `json:"id"`
	PropertyID   uuid.UUID      `json:"property_id"`
	LocationType LocationType   `json:"location_type"`
	ParentID     *uuid.UUID     `json:"parent_id"`
	Name         string         `json:"name"`
	Code         string         `json:"code"`
	Depth        int            `json:"depth"`
	SortOrder    int            `json:"sort_order"`
	IsActive     bool           `json:"is_active"`
	QRCode       *string        `json:"qr_code"`
	Path         []PathItem     `json:"path"`      // LocationPath component
	PathText     string         `json:"path_text"` // "Tower A / Floor 12 / Mechanical Room"
	Details      map[string]any `json:"details"`
	ChildCount   int            `json:"child_count"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	Version      int            `json:"version"`
}

type CreateLocationInput struct {
	LocationType LocationType   `json:"location_type"`
	ParentID     *uuid.UUID     `json:"parent_id"`
	Name         string         `json:"name"`
	SortOrder    int            `json:"sort_order"`
	Details      map[string]any `json:"details"` // field spesifik per level (timezone, floor_number, area_type, unit_number, ...)
}

type UpdateLocationInput struct {
	Name      *string        `json:"name"`
	SortOrder *int           `json:"sort_order"`
	IsActive  *bool          `json:"is_active"`
	Details   map[string]any `json:"details"`
}

func pathLabel(id uuid.UUID) string { return strings.ReplaceAll(id.String(), "-", "_") }

// CreateLocation membuat node + row detail per level dalam satu transaksi.
func (s *Service) CreateLocation(ctx context.Context, in CreateLocationInput) (*Location, error) {
	var out *Location
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.CreateLocationInTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = s.getLocationTx(ctx, tx, id)
		return err
	})
	return out, err
}

// CreateLocationInTx: implementasi di dalam transaksi yang sudah ada (dipakai service & seed).
func (s *Service) CreateLocationInTx(ctx context.Context, tx pgx.Tx, in CreateLocationInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return uuid.Nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	prefix, ok := prefixByType[in.LocationType]
	if !ok {
		return uuid.Nil, apperr.Validation("location_type tidak valid")
	}
	if in.LocationType != LTProperty && in.ParentID == nil {
		return uuid.Nil, apperr.Validation("parent_id wajib untuk " + string(in.LocationType))
	}
	if in.LocationType == LTProperty && !p.Has("property.properties.create") {
		return uuid.Nil, apperr.Forbidden("Memerlukan property.properties.create")
	}
	var parentPath string
	var parentType LocationType
	var propertyID uuid.UUID
	depth := 0
	if in.ParentID != nil {
		err := tx.QueryRow(ctx, `SELECT path::text, location_type, property_id, depth FROM locations WHERE id = $1 AND deleted_at IS NULL`, *in.ParentID).Scan(&parentPath, &parentType, &propertyID, &depth)
		if err != nil {
			if db.IsNoRows(err) {
				return uuid.Nil, apperr.Validation("parent tidak ditemukan")
			}
			return uuid.Nil, err
		}
		if !contains(allowedParents[in.LocationType], parentType) {
			return uuid.Nil, apperr.Validation(fmt.Sprintf("%s tidak boleh berada di bawah %s", in.LocationType, parentType))
		}
		depth++
		if err := requirePropertyPerm(ctx, "property.locations.create", propertyID); err != nil {
			return uuid.Nil, err
		}
	}
	code, err := ids.NextPlain(ctx, tx, p.OrganizationID, prefix)
	if err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	label := pathLabel(id)
	path := label
	if parentPath != "" {
		path = parentPath + "." + label
	}
	if in.LocationType == LTProperty {
		propertyID = id
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO locations (id, organization_id, property_id, location_type, parent_id, name, code, path, depth, sort_order, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::ltree,$9,$10,$11,$11)`,
		id, p.OrganizationID, propertyID, in.LocationType, in.ParentID, in.Name, code, path, depth, in.SortOrder, p.UserID)
	if err != nil {
		return uuid.Nil, err
	}
	if err := s.upsertDetails(ctx, tx, id, p.OrganizationID, in.LocationType, in.Details, true); err != nil {
		return uuid.Nil, err
	}
	if in.LocationType == LTArea || in.LocationType == LTUnit || in.LocationType == LTSpace {
		// Area QR (PRD §22)
		qr, err := ids.NewQRCode()
		if err != nil {
			return uuid.Nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO qr_codes (code, organization_id, object_type, object_id) VALUES ($1,$2,'location',$3)`, qr, p.OrganizationID, id); err != nil {
			return uuid.Nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE locations SET qr_code = $2 WHERE id = $1`, id, qr); err != nil {
			return uuid.Nil, err
		}
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: string(in.LocationType), EntityID: &id, EntityLabel: code, After: in})
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "location", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"name": in.Name, "type": in.LocationType}})
	if s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.LocationCreated, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "location", ObjectID: id, ObjectLabel: code, ActorUserID: &p.UserID})
	}
	return id, nil
}

func (s *Service) upsertDetails(ctx context.Context, tx pgx.Tx, id, orgID uuid.UUID, lt LocationType, d map[string]any, insert bool) error {
	if d == nil {
		d = map[string]any{}
	}
	str := func(k, def string) string {
		if v, ok := d[k].(string); ok && v != "" {
			return v
		}
		return def
	}
	num := func(k string) *float64 {
		if v, ok := d[k].(float64); ok {
			return &v
		}
		return nil
	}
	intp := func(k string) *int {
		if v, ok := d[k].(float64); ok {
			n := int(v)
			return &n
		}
		return nil
	}
	var err error
	switch lt {
	case LTProperty:
		tz := str("timezone", "Asia/Jakarta")
		if _, e := time.LoadLocation(tz); e != nil {
			return apperr.Validation("timezone tidak valid")
		}
		cal := str("sla_calendar", "always")
		if cal != "always" && cal != "business_hours" {
			return apperr.Validation("sla_calendar harus always|business_hours")
		}
		if insert {
			_, err = tx.Exec(ctx, `INSERT INTO properties (location_id, organization_id, timezone, address, city, property_type, sla_calendar) VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7)`,
				id, orgID, tz, str("address", ""), str("city", ""), str("property_type", ""), cal)
		} else {
			_, err = tx.Exec(ctx, `UPDATE properties SET timezone = COALESCE(NULLIF($2,''), timezone), address = COALESCE(NULLIF($3,''), address), city = COALESCE(NULLIF($4,''), city), property_type = COALESCE(NULLIF($5,''), property_type), sla_calendar = $6 WHERE location_id = $1`,
				id, str("timezone", ""), str("address", ""), str("city", ""), str("property_type", ""), cal)
		}
	case LTBuilding:
		if insert {
			_, err = tx.Exec(ctx, `INSERT INTO buildings (location_id, organization_id, floors_count, year_built, gross_area_m2) VALUES ($1,$2,$3,$4,$5)`, id, orgID, intp("floors_count"), intp("year_built"), num("gross_area_m2"))
		} else {
			_, err = tx.Exec(ctx, `UPDATE buildings SET floors_count = COALESCE($2, floors_count), year_built = COALESCE($3, year_built), gross_area_m2 = COALESCE($4, gross_area_m2) WHERE location_id = $1`, id, intp("floors_count"), intp("year_built"), num("gross_area_m2"))
		}
	case LTTower:
		if insert {
			_, err = tx.Exec(ctx, `INSERT INTO towers (location_id, organization_id, floors_count) VALUES ($1,$2,$3)`, id, orgID, intp("floors_count"))
		} else {
			_, err = tx.Exec(ctx, `UPDATE towers SET floors_count = COALESCE($2, floors_count) WHERE location_id = $1`, id, intp("floors_count"))
		}
	case LTFloor:
		fn := intp("floor_number")
		if insert {
			if fn == nil {
				return apperr.Validation("details.floor_number wajib untuk floor")
			}
			_, err = tx.Exec(ctx, `INSERT INTO floors (location_id, organization_id, floor_number, floor_label) VALUES ($1,$2,$3,NULLIF($4,''))`, id, orgID, *fn, str("floor_label", ""))
		} else {
			_, err = tx.Exec(ctx, `UPDATE floors SET floor_number = COALESCE($2, floor_number), floor_label = COALESCE(NULLIF($3,''), floor_label) WHERE location_id = $1`, id, fn, str("floor_label", ""))
		}
	case LTArea:
		if insert {
			_, err = tx.Exec(ctx, `INSERT INTO areas (location_id, organization_id, area_type) VALUES ($1,$2,$3)`, id, orgID, str("area_type", "other"))
		} else {
			_, err = tx.Exec(ctx, `UPDATE areas SET area_type = COALESCE(NULLIF($2,''), area_type) WHERE location_id = $1`, id, str("area_type", ""))
		}
	case LTSpace:
		if insert {
			_, err = tx.Exec(ctx, `INSERT INTO spaces (location_id, organization_id, space_type, area_m2) VALUES ($1,$2,$3,$4)`, id, orgID, str("space_type", "other"), num("area_m2"))
		} else {
			_, err = tx.Exec(ctx, `UPDATE spaces SET space_type = COALESCE(NULLIF($2,''), space_type), area_m2 = COALESCE($3, area_m2) WHERE location_id = $1`, id, str("space_type", ""), num("area_m2"))
		}
	case LTUnit:
		var tenantID *uuid.UUID
		if v, ok := d["tenant_id"].(string); ok && v != "" {
			tid, e := uuid.Parse(v)
			if e != nil {
				return apperr.Validation("tenant_id tidak valid")
			}
			tenantID = &tid
		}
		if insert {
			un := str("unit_number", "")
			if un == "" {
				return apperr.Validation("details.unit_number wajib untuk unit")
			}
			ut := str("unit_type", "commercial")
			occ := "vacant"
			if tenantID != nil {
				occ = "occupied"
			}
			_, err = tx.Exec(ctx, `INSERT INTO units (location_id, organization_id, unit_number, unit_type, area_m2, tenant_id, occupancy_status) VALUES ($1,$2,$3,$4,$5,$6,$7)`, id, orgID, un, ut, num("area_m2"), tenantID, str("occupancy_status", occ))
		} else {
			_, err = tx.Exec(ctx, `UPDATE units SET unit_number = COALESCE(NULLIF($2,''), unit_number), unit_type = COALESCE(NULLIF($3,''), unit_type), area_m2 = COALESCE($4, area_m2),
				tenant_id = CASE WHEN $6::bool THEN $5 ELSE tenant_id END, occupancy_status = COALESCE(NULLIF($7,''), occupancy_status) WHERE location_id = $1`,
				id, str("unit_number", ""), str("unit_type", ""), num("area_m2"), tenantID, d["tenant_id"] != nil, str("occupancy_status", ""))
		}
	}
	return err
}

func (s *Service) UpdateLocation(ctx context.Context, id uuid.UUID, in UpdateLocationInput, ifVersion *int) (*Location, error) {
	p := authctx.Must(ctx)
	var out *Location
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getLocationTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := requirePropertyPerm(ctx, "property.locations.update", before.PropertyID); err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != before.Version {
			return apperr.StaleVersion()
		}
		if _, err := tx.Exec(ctx, `UPDATE locations SET name = COALESCE(NULLIF($2,''), name), sort_order = COALESCE($3, sort_order), is_active = COALESCE($4, is_active), updated_by = $5 WHERE id = $1`,
			id, deref(in.Name), in.SortOrder, in.IsActive, p.UserID); err != nil {
			return err
		}
		if in.Details != nil {
			if err := s.upsertDetails(ctx, tx, id, p.OrganizationID, before.LocationType, in.Details, false); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: string(before.LocationType), EntityID: &id, EntityLabel: before.Code, Before: before, After: in})
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "location", ObjectID: id, Action: audit.ActUpdated})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.LocationUpdated, OrganizationID: p.OrganizationID, PropertyID: &before.PropertyID, ObjectType: "location", ObjectID: id, ObjectLabel: before.Code, ActorUserID: &p.UserID})
		}
		out, err = s.getLocationTx(ctx, tx, id)
		return err
	})
	return out, err
}

// DeleteLocation: soft delete; ditolak bila punya anak aktif atau object operasional.
func (s *Service) DeleteLocation(ctx context.Context, id uuid.UUID) error {
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		loc, err := s.getLocationTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := requirePropertyPerm(ctx, "property.locations.delete", loc.PropertyID); err != nil {
			return err
		}
		if loc.LocationType == LTProperty {
			return apperr.Forbidden("Property tidak dapat dihapus; nonaktifkan saja")
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE parent_id = $1 AND deleted_at IS NULL`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return apperr.Conflict("LOCATION_HAS_CHILDREN", "Lokasi masih memiliki sub-lokasi")
		}
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM assets WHERE location_id = $1 AND deleted_at IS NULL) + (SELECT count(*) FROM work_orders WHERE location_id = $1 AND status NOT IN ('closed','cancelled')) + (SELECT count(*) FROM tasks WHERE location_id = $1 AND status NOT IN ('closed','cancelled'))`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return apperr.Conflict("LOCATION_IN_USE", "Lokasi masih dipakai oleh asset/task/work order aktif")
		}
		if _, err := tx.Exec(ctx, `UPDATE locations SET deleted_at = now(), is_active = false WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: string(loc.LocationType), EntityID: &id, EntityLabel: loc.Code})
	})
}

func (s *Service) GetLocation(ctx context.Context, id uuid.UUID) (*Location, error) {
	var out *Location
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.getLocationTx(ctx, tx, id)
		if err != nil {
			return err
		}
		return requirePropertyPerm(ctx, "property.locations.view", out.PropertyID)
	})
	return out, err
}

const locationSelect = `
	SELECT l.id, l.property_id, l.location_type, l.parent_id, l.name, l.code, l.depth, l.sort_order, l.is_active, l.qr_code, l.created_at, l.updated_at, l.version,
	  (SELECT count(*) FROM locations c WHERE c.parent_id = l.id AND c.deleted_at IS NULL),
	  COALESCE((SELECT jsonb_agg(jsonb_build_object('id', a.id, 'type', a.location_type, 'name', a.name) ORDER BY a.depth)
	            FROM locations a WHERE a.path @> l.path AND a.deleted_at IS NULL), '[]'::jsonb),
	  COALESCE(to_jsonb(pr) - 'location_id' - 'organization_id' - 'public_intake_key', '{}'::jsonb) ||
	  COALESCE(to_jsonb(b) - 'location_id' - 'organization_id', '{}'::jsonb) ||
	  COALESCE(to_jsonb(t) - 'location_id' - 'organization_id', '{}'::jsonb) ||
	  COALESCE(to_jsonb(f) - 'location_id' - 'organization_id', '{}'::jsonb) ||
	  COALESCE(to_jsonb(ar) - 'location_id' - 'organization_id', '{}'::jsonb) ||
	  COALESCE(to_jsonb(sp) - 'location_id' - 'organization_id', '{}'::jsonb) ||
	  COALESCE(to_jsonb(u) - 'location_id' - 'organization_id', '{}'::jsonb) ||
	  COALESCE(jsonb_build_object('tenant_name', tn.name), '{}'::jsonb)
	FROM locations l
	LEFT JOIN properties pr ON pr.location_id = l.id
	LEFT JOIN buildings b ON b.location_id = l.id
	LEFT JOIN towers t ON t.location_id = l.id
	LEFT JOIN floors f ON f.location_id = l.id
	LEFT JOIN areas ar ON ar.location_id = l.id
	LEFT JOIN spaces sp ON sp.location_id = l.id
	LEFT JOIN units u ON u.location_id = l.id
	LEFT JOIN tenants tn ON tn.id = u.tenant_id`

func scanLocation(row pgx.Row) (*Location, error) {
	var l Location
	var path []PathItem
	var details map[string]any
	if err := row.Scan(&l.ID, &l.PropertyID, &l.LocationType, &l.ParentID, &l.Name, &l.Code, &l.Depth, &l.SortOrder, &l.IsActive, &l.QRCode, &l.CreatedAt, &l.UpdatedAt, &l.Version, &l.ChildCount, &path, &details); err != nil {
		return nil, err
	}
	l.Path = path
	l.Details = details
	if l.Details == nil {
		l.Details = map[string]any{}
	}
	delete(l.Details, "tenant_name_null")
	names := make([]string, 0, len(path))
	for i, it := range path {
		if i == 0 && len(path) > 1 {
			continue // property tidak ditampilkan di path bila ada level di bawahnya
		}
		names = append(names, it.Name)
	}
	l.PathText = strings.Join(names, " / ")
	return &l, nil
}

func (s *Service) getLocationTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Location, error) {
	l, err := scanLocation(tx.QueryRow(ctx, locationSelect+` WHERE l.id = $1 AND l.deleted_at IS NULL`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Lokasi")
		}
		return nil, err
	}
	return l, nil
}

type LocationFilter struct {
	PropertyID      *uuid.UUID
	ParentID        *uuid.UUID
	LocationType    LocationType
	Q               string
	IncludeInactive bool
}

// ListLocations: flat list terfilter (untuk halaman Buildings/Floors/Units dst).
func (s *Service) ListLocations(ctx context.Context, f LocationFilter) ([]Location, error) {
	p := authctx.Must(ctx)
	var out []Location
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		where := " WHERE l.deleted_at IS NULL"
		if f.PropertyID != nil {
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND l.property_id = $%d", len(args))
		} else {
			// batasi ke property yang boleh dilihat user
			if pids, all := p.PropertyIDsFor("property.locations.view"); !all {
				args = append(args, pids)
				where += fmt.Sprintf(" AND l.property_id = ANY($%d::uuid[])", len(args))
			}
		}
		if f.ParentID != nil {
			args = append(args, *f.ParentID)
			where += fmt.Sprintf(" AND l.parent_id = $%d", len(args))
		}
		if f.LocationType != "" {
			args = append(args, string(f.LocationType))
			where += fmt.Sprintf(" AND l.location_type = $%d", len(args))
		}
		if f.Q != "" {
			args = append(args, "%"+f.Q+"%")
			where += fmt.Sprintf(" AND (l.name ILIKE $%d OR l.code ILIKE $%d)", len(args), len(args))
		}
		if !f.IncludeInactive {
			where += " AND l.is_active"
		}
		rows, err := tx.Query(ctx, locationSelect+where+` ORDER BY l.path, l.sort_order, l.name LIMIT 2000`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			l, err := scanLocation(rows)
			if err != nil {
				return err
			}
			out = append(out, *l)
		}
		return rows.Err()
	})
	return out, err
}

type TreeNode struct {
	Location
	Children []*TreeNode `json:"children"`
}

// Tree: seluruh hierarchy satu property (LocationPicker).
func (s *Service) Tree(ctx context.Context, propertyID uuid.UUID) (*TreeNode, error) {
	if err := requirePropertyPerm(ctx, "property.locations.view", propertyID); err != nil {
		return nil, err
	}
	locs, err := s.ListLocations(ctx, LocationFilter{PropertyID: &propertyID})
	if err != nil {
		return nil, err
	}
	nodes := map[uuid.UUID]*TreeNode{}
	var root *TreeNode
	for i := range locs {
		n := &TreeNode{Location: locs[i], Children: []*TreeNode{}}
		nodes[n.ID] = n
	}
	for _, n := range nodes {
		if n.ParentID == nil {
			root = n
			continue
		}
		if parent, ok := nodes[*n.ParentID]; ok {
			parent.Children = append(parent.Children, n)
		}
	}
	if root == nil {
		return nil, apperr.NotFound("Property")
	}
	sortTree(root)
	return root, nil
}

func sortTree(n *TreeNode) {
	for i := 1; i < len(n.Children); i++ {
		for j := i; j > 0; j-- {
			a, b := n.Children[j-1], n.Children[j]
			if a.SortOrder > b.SortOrder || (a.SortOrder == b.SortOrder && a.Name > b.Name) {
				n.Children[j-1], n.Children[j] = b, a
			}
		}
	}
	for _, c := range n.Children {
		sortTree(c)
	}
}

// ListProperties: property yang boleh dilihat user (Property Switcher).
func (s *Service) ListProperties(ctx context.Context) ([]Location, error) {
	return s.ListLocations(ctx, LocationFilter{LocationType: LTProperty, IncludeInactive: true})
}

// PropertyTimezone: helper untuk business ID & SLA.
func PropertyTimezone(ctx context.Context, q db.Querier, propertyID uuid.UUID) *time.Location {
	var tz string
	if err := q.QueryRow(ctx, `SELECT timezone FROM properties WHERE location_id = $1`, propertyID).Scan(&tz); err != nil {
		tz = "Asia/Jakarta"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc, _ = time.LoadLocation("Asia/Jakarta")
	}
	return loc
}

// ResolvePropertyOfLocation: property_id dari location_id (validasi org via RLS).
func ResolvePropertyOfLocation(ctx context.Context, q db.Querier, locationID uuid.UUID) (uuid.UUID, error) {
	var pid uuid.UUID
	if err := q.QueryRow(ctx, `SELECT property_id FROM locations WHERE id = $1 AND deleted_at IS NULL`, locationID).Scan(&pid); err != nil {
		if db.IsNoRows(err) {
			return uuid.Nil, apperr.Validation("location_id tidak ditemukan")
		}
		return uuid.Nil, err
	}
	return pid, nil
}

// LocationPathText: "Tower A / Floor 12 / Mechanical Room".
func LocationPathText(ctx context.Context, q db.Querier, locationID uuid.UUID) string {
	var txt string
	_ = q.QueryRow(ctx, `
		SELECT string_agg(a.name, ' / ' ORDER BY a.depth)
		FROM locations l JOIN locations a ON a.path @> l.path AND a.depth > 0 AND a.deleted_at IS NULL
		WHERE l.id = $1`, locationID).Scan(&txt)
	if txt == "" {
		_ = q.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, locationID).Scan(&txt)
	}
	return txt
}

// ---------- Organization ----------

type Organization struct {
	ID       uuid.UUID      `json:"id"`
	Code     string         `json:"code"`
	Slug     string         `json:"slug"`
	Name     string         `json:"name"`
	Timezone string         `json:"timezone"`
	IsActive bool           `json:"is_active"`
	Settings map[string]any `json:"settings"`
	Version  int            `json:"version"`
}

func (s *Service) GetOrganization(ctx context.Context) (*Organization, error) {
	p := authctx.Must(ctx)
	var o Organization
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id, code, slug, name, timezone, is_active, settings, version FROM organizations WHERE id = $1`, p.OrganizationID).
			Scan(&o.ID, &o.Code, &o.Slug, &o.Name, &o.Timezone, &o.IsActive, &o.Settings, &o.Version)
	})
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *Service) UpdateOrganization(ctx context.Context, name *string, settings map[string]any) (*Organization, error) {
	p := authctx.Must(ctx)
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE organizations SET name = COALESCE(NULLIF($2,''), name), settings = COALESCE($3, settings), updated_by = $4 WHERE id = $1`, p.OrganizationID, deref(name), settings, p.UserID)
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "organization", EntityID: &p.OrganizationID, After: map[string]any{"name": name, "settings": settings}})
	})
	if err != nil {
		return nil, err
	}
	return s.GetOrganization(ctx)
}

func requirePropertyPerm(ctx context.Context, perm string, propertyID uuid.UUID) error {
	p := authctx.Must(ctx)
	if !p.HasOnProperty(perm, propertyID) {
		return apperr.Forbidden("Tidak memiliki " + perm + " pada property ini")
	}
	return nil
}

func contains[T comparable](xs []T, x T) bool {
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
