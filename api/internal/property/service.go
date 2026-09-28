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
	"github.com/buildingvision/api/internal/platform/phone"
	"github.com/buildingvision/api/internal/profile"
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

// UnitTypes & OccupancyStatuses (CHECK units; PRD P1 v2 §8: Vacant / Occupied / Inactive + Reserved untuk rental/sales).
var UnitTypes = map[string]bool{"commercial": true, "residential": true, "hotel_room": true}
var OccupancyStatuses = map[string]bool{"vacant": true, "occupied": true, "reserved": true, "inactive": true}

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
	Metadata     map[string]any `json:"metadata"` // metadata bebas per level (PRD P0 v2 §7.3)
	ChildCount   int            `json:"child_count"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	Version      int            `json:"version"`
	ltreePath    string
}

type CreateLocationInput struct {
	LocationType LocationType   `json:"location_type"`
	ParentID     *uuid.UUID     `json:"parent_id"`
	Name         string         `json:"name"`
	Code         *string        `json:"code"` // opsional (PRD P0 v2 §7): kosong = otomatis PROP-/BLD-/...
	SortOrder    int            `json:"sort_order"`
	Details      map[string]any `json:"details"` // field spesifik per level (timezone, floor_number, area_type, unit_number, ...)
	Metadata     map[string]any `json:"metadata"`
}

type UpdateLocationInput struct {
	Name      *string        `json:"name"`
	Code      *string        `json:"code"`
	SortOrder *int           `json:"sort_order"`
	IsActive  *bool          `json:"is_active"`
	Details   map[string]any `json:"details"`
	Metadata  map[string]any `json:"metadata"`
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
		if err := requireLocationPerm(ctx, "property.locations.create", propertyID, parentPath); err != nil {
			return uuid.Nil, err
		}
	}
	code := ""
	if in.Code != nil {
		code = strings.ToUpper(strings.TrimSpace(*in.Code))
	}
	if code == "" {
		c, err := ids.NextPlain(ctx, tx, p.OrganizationID, prefix)
		if err != nil {
			return uuid.Nil, err
		}
		code = c
	}
	if in.Metadata == nil {
		in.Metadata = map[string]any{}
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
	_, err := tx.Exec(ctx, `
		INSERT INTO locations (id, organization_id, property_id, location_type, parent_id, name, code, path, depth, sort_order, metadata, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::ltree,$9,$10,$12,$11,$11)`,
		id, p.OrganizationID, propertyID, in.LocationType, in.ParentID, in.Name, code, path, depth, in.SortOrder, p.UserID, in.Metadata)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return uuid.Nil, apperr.Conflict("DUPLICATE_CODE", "Kode lokasi sudah dipakai").WithField("code", "sudah dipakai")
		}
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
			// Property Profile wajib dipilih saat membuat property (Onboarding Brief PS-001/AC-02; TD-P1-001)
			prof := strings.ToLower(str("profile", ""))
			if prof == "" {
				return apperr.Validation("details.profile wajib: hotel|apartment|office").WithField("details.profile", "wajib")
			}
			if !profile.Valid(prof) {
				return apperr.Validation("details.profile harus hotel|apartment|office").WithField("details.profile", "tidak valid")
			}
			_, err = tx.Exec(ctx, `INSERT INTO properties (location_id, organization_id, timezone, address, city, property_type, sla_calendar, profile) VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7,$8)`,
				id, orgID, tz, str("address", ""), str("city", ""), str("property_type", ""), cal, prof)
			if err == nil {
				err = updatePropertyContact(ctx, tx, id, d)
			}
			if err == nil {
				_, err = tx.Exec(ctx, `INSERT INTO property_profile_configs (property_id, organization_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, orgID)
			}
		} else {
			// profile TIDAK diubah lewat PATCH biasa (PS-006) — hanya lewat POST /properties/{id}/profile
			if v := str("profile", ""); v != "" {
				var cur string
				_ = tx.QueryRow(ctx, `SELECT profile FROM properties WHERE location_id = $1`, id).Scan(&cur)
				if !strings.EqualFold(v, cur) {
					return apperr.Validation("Perubahan profile harus melalui aksi administratif Ubah Profile (POST /properties/{id}/profile)")
				}
			}
			_, err = tx.Exec(ctx, `UPDATE properties SET timezone = COALESCE(NULLIF($2,''), timezone), address = COALESCE(NULLIF($3,''), address), city = COALESCE(NULLIF($4,''), city), property_type = COALESCE(NULLIF($5,''), property_type), sla_calendar = $6 WHERE location_id = $1`,
				id, str("timezone", ""), str("address", ""), str("city", ""), str("property_type", ""), cal)
			if err == nil {
				err = updatePropertyContact(ctx, tx, id, d)
			}
		}
	case LTBuilding:
		if insert {
			_, err = tx.Exec(ctx, `INSERT INTO buildings (location_id, organization_id, floors_count, year_built, gross_area_m2, building_type, address) VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''))`,
				id, orgID, intp("floors_count"), intp("year_built"), num("gross_area_m2"), str("building_type", ""), str("address", ""))
		} else {
			_, err = tx.Exec(ctx, `UPDATE buildings SET floors_count = COALESCE($2, floors_count), year_built = COALESCE($3, year_built), gross_area_m2 = COALESCE($4, gross_area_m2),
				building_type = COALESCE(NULLIF($5,''), building_type), address = COALESCE(NULLIF($6,''), address) WHERE location_id = $1`,
				id, intp("floors_count"), intp("year_built"), num("gross_area_m2"), str("building_type", ""), str("address", ""))
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
		// PRD P1 v2 §8: validasi eksplisit (sebelumnya pelanggaran CHECK DB → 500)
		if v := str("unit_type", ""); v != "" && !UnitTypes[v] {
			return apperr.Validation("unit_type harus commercial|residential|hotel_room").WithField("details.unit_type", "tidak valid")
		}
		if v := str("occupancy_status", ""); v != "" && !OccupancyStatuses[v] {
			return apperr.Validation("occupancy_status harus vacant|occupied|reserved|inactive").WithField("details.occupancy_status", "tidak valid")
		}
		// PRD P4 v2.1 P4-BRL-04: tanggal hunian (prorata tagihan periode) — kunci ada = set ("" / null = kosongkan)
		occDate := func(key string) (any, bool, error) {
			v, ok := d[key]
			if !ok {
				return nil, false, nil
			}
			sv, _ := v.(string)
			if v == nil || sv == "" {
				return nil, true, nil
			}
			t, e := time.Parse("2006-01-02", sv)
			if e != nil {
				return nil, true, apperr.Validation("details."+key+" harus YYYY-MM-DD").WithField("details."+key, "format tanggal")
			}
			return t, true, nil
		}
		occFrom, setFrom, err1 := occDate("occupied_from")
		occUntil, setUntil, err2 := occDate("occupied_until")
		if err1 != nil {
			return err1
		}
		if err2 != nil {
			return err2
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
		if err == nil && (setFrom || setUntil) {
			_, err = tx.Exec(ctx, `UPDATE units SET occupied_from = CASE WHEN $2::bool THEN $3::date ELSE occupied_from END, occupied_until = CASE WHEN $4::bool THEN $5::date ELSE occupied_until END WHERE location_id = $1`,
				id, setFrom, occFrom, setUntil, occUntil)
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
		if err := requireLocationPerm(ctx, "property.locations.update", before.PropertyID, before.ltreePath); err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != before.Version {
			return apperr.StaleVersion()
		}
		if _, err := tx.Exec(ctx, `UPDATE locations SET name = COALESCE(NULLIF($2,''), name), sort_order = COALESCE($3, sort_order), is_active = COALESCE($4, is_active),
			metadata = COALESCE($6, metadata), code = COALESCE(NULLIF(upper(trim($7)),''), code), updated_by = $5 WHERE id = $1`,
			id, deref(in.Name), in.SortOrder, in.IsActive, p.UserID, jsonOrNil(in.Metadata), in.Code); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("DUPLICATE_CODE", "Kode lokasi sudah dipakai").WithField("code", "sudah dipakai")
			}
			return err
		}
		if in.IsActive != nil && *in.IsActive != before.IsActive {
			if err := syncActiveStatus(ctx, tx, before, *in.IsActive, ""); err != nil {
				return err
			}
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
		if err := requireLocationPerm(ctx, "property.locations.delete", loc.PropertyID, loc.ltreePath); err != nil {
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
		if s.Jobs != nil {
			p := authctx.Must(ctx)
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.LocationDeleted, OrganizationID: p.OrganizationID, PropertyID: &loc.PropertyID, ObjectType: "location", ObjectID: id, ObjectLabel: loc.Code, ActorUserID: &p.UserID})
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: string(loc.LocationType), EntityID: &id, EntityLabel: loc.Code, Before: loc})
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
		if !canViewLocation(authctx.Must(ctx), out.PropertyID, out.ltreePath) {
			return apperr.Forbidden("Tidak memiliki property.locations.view pada lokasi ini")
		}
		return nil
	})
	return out, err
}

const locationSelect = `
	SELECT l.id, l.property_id, l.location_type, l.parent_id, l.name, l.code, l.depth, l.sort_order, l.is_active, l.qr_code, l.created_at, l.updated_at, l.version,
	  l.metadata, l.path::text,
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
	if err := row.Scan(&l.ID, &l.PropertyID, &l.LocationType, &l.ParentID, &l.Name, &l.Code, &l.Depth, &l.SortOrder, &l.IsActive, &l.QRCode, &l.CreatedAt, &l.UpdatedAt, &l.Version, &l.Metadata, &l.ltreePath, &l.ChildCount, &path, &details); err != nil {
		return nil, err
	}
	if l.Metadata == nil {
		l.Metadata = map[string]any{}
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
	AncestorID      *uuid.UUID // seluruh subtree (mis. semua floor di building, termasuk di bawah tower)
	PortfolioID     *uuid.UUID // property dalam portfolio
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
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE l.deleted_at IS NULL"
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("property.locations.view", *f.PropertyID) {
				return apperr.Forbidden("Tidak memiliki property.locations.view pada property ini")
			}
			where += " AND l.property_id = " + add(*f.PropertyID)
		}
		// batasi ke scope user: property-wide, subtree building/tower, dan leluhur scope (agar tree/picker utuh)
		where += " AND " + locationScopeSQL(p, add)
		if f.ParentID != nil {
			where += " AND l.parent_id = " + add(*f.ParentID)
		}
		if f.AncestorID != nil {
			where += " AND l.id <> " + add(*f.AncestorID) + " AND l.path <@ (SELECT a.path FROM locations a WHERE a.id = " + add(*f.AncestorID) + ")"
		}
		if f.PortfolioID != nil {
			where += " AND l.location_type = 'property' AND EXISTS (SELECT 1 FROM properties pp WHERE pp.location_id = l.id AND pp.portfolio_id = " + add(*f.PortfolioID) + ")"
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
	if !authctx.Must(ctx).HasAnyOnProperty("property.locations.view", propertyID) {
		return nil, apperr.Forbidden("Tidak memiliki property.locations.view pada property ini")
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

// ---------- Scope Building/Tower (PRD P0 v2 §8.4) ----------

// requireLocationPerm: perm pada property, atau grant ber-scope building/tower yang memuat path lokasi.
func requireLocationPerm(ctx context.Context, perm string, propertyID uuid.UUID, path string) error {
	p := authctx.Must(ctx)
	if !p.HasOnLocation(perm, propertyID, path) {
		return apperr.Forbidden("Tidak memiliki " + perm + " pada lokasi ini")
	}
	return nil
}

// canViewLocation: lokasi di dalam scope, atau leluhur dari scope building/tower user (property/induk tetap terlihat).
func canViewLocation(p *authctx.Principal, propertyID uuid.UUID, path string) bool {
	const perm = "property.locations.view"
	if p.HasOnLocation(perm, propertyID, path) {
		return true
	}
	for _, sc := range p.LocationScopesFor(perm) {
		if sc.PropertyID == propertyID && strings.HasPrefix(sc.Path, path+".") {
			return true
		}
	}
	return false
}

func locationScopeSQL(p *authctx.Principal, add func(any) string) string {
	const perm = "property.locations.view"
	cond := p.ScopeSQL(perm, "l.property_id", "l.path", add)
	scopes := p.LocationScopesFor(perm)
	if cond == "TRUE" || len(scopes) == 0 {
		return cond
	}
	paths := make([]string, 0, len(scopes))
	for _, sc := range scopes {
		paths = append(paths, sc.Path)
	}
	return "(" + cond + " OR l.path @> ANY(" + add(paths) + "::ltree[]))"
}

// syncActiveStatus: status property (draft|active|inactive) mengikuti is_active + audit status_change.
func syncActiveStatus(ctx context.Context, tx pgx.Tx, loc *Location, active bool, reason string) error {
	if loc.LocationType == LTProperty {
		st := "inactive"
		if active {
			st = "active"
		}
		if _, err := tx.Exec(ctx, `UPDATE properties SET status = $2 WHERE location_id = $1`, loc.ID, st); err != nil {
			return err
		}
	}
	id := loc.ID
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: string(loc.LocationType), EntityID: &id, EntityLabel: loc.Code,
		Before: map[string]any{"is_active": loc.IsActive}, After: map[string]any{"is_active": active, "reason": reason}})
	return nil
}

// SetLocationActive: aktivasi/deaktivasi eksplisit Property/Building/Tower/Floor/Area/Unit (PRD P0 v2 §7, US-P0-002).
func (s *Service) SetLocationActive(ctx context.Context, id uuid.UUID, active bool, reason string) (*Location, error) {
	p := authctx.Must(ctx)
	var out *Location
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		loc, err := s.getLocationTx(ctx, tx, id)
		if err != nil {
			return err
		}
		perm := "property.locations.update"
		if loc.LocationType == LTProperty {
			perm = "property.properties.update"
		}
		if err := requireLocationPerm(ctx, perm, loc.PropertyID, loc.ltreePath); err != nil {
			return err
		}
		if loc.IsActive != active {
			if _, err := tx.Exec(ctx, `UPDATE locations SET is_active = $2, updated_by = $3 WHERE id = $1`, id, active, p.UserID); err != nil {
				return err
			}
			if err := syncActiveStatus(ctx, tx, loc, active, reason); err != nil {
				return err
			}
			if s.Jobs != nil {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.LocationUpdated, OrganizationID: p.OrganizationID, PropertyID: &loc.PropertyID, ObjectType: "location", ObjectID: id, ObjectLabel: loc.Code, ActorUserID: &p.UserID})
			}
		}
		out, err = s.getLocationTx(ctx, tx, id)
		return err
	})
	return out, err
}

// updatePropertyContact: kontak & alamat lengkap property + portfolio (PRD P0 v2 §7.2).
func updatePropertyContact(ctx context.Context, tx pgx.Tx, id uuid.UUID, d map[string]any) error {
	str := func(k string) *string {
		if v, ok := d[k].(string); ok {
			v = strings.TrimSpace(v)
			return &v
		}
		return nil
	}
	num := func(k string) *float64 {
		if v, ok := d[k].(float64); ok {
			return &v
		}
		return nil
	}
	if e := str("contact_email"); e != nil && *e != "" && !strings.Contains(*e, "@") {
		return apperr.Validation("contact_email tidak valid").WithField("details.contact_email", "tidak valid")
	}
	// PRD P3 v2.1 P3-WAM-05: nomor WhatsApp pengelola (dinormalisasi 62…; string kosong = hapus)
	waNumber, clearWA := str("whatsapp_number"), false
	if waNumber != nil {
		if strings.TrimSpace(*waNumber) == "" {
			clearWA, waNumber = true, nil
		} else {
			n, ok := phone.Normalize(*waNumber)
			if !ok {
				return apperr.Validation("whatsapp_number tidak valid").WithField("details.whatsapp_number", "nomor WhatsApp tidak valid")
			}
			waNumber = &n
		}
	}
	if c := str("country"); c != nil && len(*c) != 2 {
		return apperr.Validation("country harus kode ISO 2 huruf").WithField("details.country", "2 huruf")
	}
	var portfolio *uuid.UUID
	clearPortfolio := false
	if v, ok := d["portfolio_id"]; ok {
		if v == nil || v == "" {
			clearPortfolio = true
		} else if sv, ok := v.(string); ok {
			pid, err := uuid.Parse(sv)
			if err != nil {
				return apperr.Validation("portfolio_id tidak valid")
			}
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM portfolios WHERE id = $1 AND deleted_at IS NULL)`, pid).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return apperr.Validation("portfolio_id tidak ditemukan").WithField("details.portfolio_id", "tidak ditemukan")
			}
			portfolio = &pid
		}
	}
	_, err := tx.Exec(ctx, `UPDATE properties SET contact_name = COALESCE($2, contact_name), contact_phone = COALESCE($3, contact_phone),
		contact_email = COALESCE($4, contact_email), province = COALESCE($5, province), postal_code = COALESCE($6, postal_code),
		country = COALESCE(upper($7), country), latitude = COALESCE($8, latitude), longitude = COALESCE($9, longitude),
		portfolio_id = CASE WHEN $11 THEN NULL ELSE COALESCE($10, portfolio_id) END,
		whatsapp_number = CASE WHEN $13 THEN NULL ELSE COALESCE($12, whatsapp_number) END WHERE location_id = $1`,
		id, str("contact_name"), str("contact_phone"), str("contact_email"), str("province"), str("postal_code"), str("country"),
		num("latitude"), num("longitude"), portfolio, clearPortfolio, waNumber, clearWA)
	return err
}

// jsonOrNil: map nil → SQL NULL (bukan jsonb 'null') agar COALESCE mempertahankan nilai lama.
func jsonOrNil(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m
}
