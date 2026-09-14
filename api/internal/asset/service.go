// Package asset: Asset & Equipment (PRD §12.1; Naming Convention §11–§13), QR (PRD §22), asset history.
package asset

import (
	"context"
	"encoding/json"
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
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/property"
)

type Service struct {
	DB   *db.DB
	Jobs jobs.Enqueuer
}

// ---------- Equipment (kategori/tipe) ----------

type Equipment struct {
	ID                 uuid.UUID  `json:"id"`
	CategoryCode       string     `json:"category_code"`
	CategoryName       string     `json:"category_name"`
	TypeName           *string    `json:"type_name"`
	ParentID           *uuid.UUID `json:"parent_id"`
	DefaultCriticality *string    `json:"default_criticality"`
	IsActive           bool       `json:"is_active"`
	AssetCount         int        `json:"asset_count"`
	Version            int        `json:"version"`
}

type EquipmentInput struct {
	CategoryCode       string  `json:"category_code"`
	CategoryName       string  `json:"category_name"`
	TypeName           *string `json:"type_name"`
	DefaultCriticality *string `json:"default_criticality"`
	IsActive           *bool   `json:"is_active"`
}

var categoryCodes = map[string]bool{"HVAC": true, "LIFT": true, "GEN": true, "PUMP": true, "ELEC": true, "FIRE": true, "PLMB": true, "SECU": true, "FAC": true, "OTH": true}

func (s *Service) ListEquipment(ctx context.Context, q string) ([]Equipment, error) {
	var out []Equipment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT e.id, e.category_code, e.category_name, e.type_name, e.parent_id, e.default_criticality, e.is_active, e.version,
			(SELECT count(*) FROM assets a WHERE a.equipment_id = e.id AND a.deleted_at IS NULL)
			FROM equipment e WHERE e.deleted_at IS NULL AND ($1 = '' OR e.category_name ILIKE '%' || $1 || '%' OR e.type_name ILIKE '%' || $1 || '%')
			ORDER BY e.category_code, e.type_name NULLS FIRST`, q)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Equipment
			if err := rows.Scan(&e.ID, &e.CategoryCode, &e.CategoryName, &e.TypeName, &e.ParentID, &e.DefaultCriticality, &e.IsActive, &e.Version, &e.AssetCount); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if out == nil {
		out = []Equipment{}
	}
	return out, err
}

func (s *Service) CreateEquipment(ctx context.Context, in EquipmentInput) (*Equipment, error) {
	p := authctx.Must(ctx)
	in.CategoryCode = strings.ToUpper(strings.TrimSpace(in.CategoryCode))
	if !categoryCodes[in.CategoryCode] {
		return nil, apperr.Validation("category_code harus salah satu: HVAC LIFT GEN PUMP ELEC FIRE PLMB SECU FAC OTH")
	}
	if in.CategoryName == "" {
		return nil, apperr.Validation("category_name wajib")
	}
	var out Equipment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var parent *uuid.UUID
		if in.TypeName != nil && *in.TypeName != "" {
			var pid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM equipment WHERE category_code = $1 AND type_name IS NULL AND deleted_at IS NULL`, in.CategoryCode).Scan(&pid); err == nil {
				parent = &pid
			}
		}
		err := tx.QueryRow(ctx, `INSERT INTO equipment (organization_id, category_code, category_name, type_name, parent_id, default_criticality, created_by, updated_by) VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$7)
			RETURNING id, category_code, category_name, type_name, parent_id, default_criticality, is_active, version`,
			p.OrganizationID, in.CategoryCode, in.CategoryName, derefStr(in.TypeName), parent, in.DefaultCriticality, p.UserID).
			Scan(&out.ID, &out.CategoryCode, &out.CategoryName, &out.TypeName, &out.ParentID, &out.DefaultCriticality, &out.IsActive, &out.Version)
		if err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("DUPLICATE_EQUIPMENT", "Equipment dengan kategori/tipe ini sudah ada")
			}
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "equipment", EntityID: &out.ID, EntityLabel: in.CategoryCode + "/" + derefStr(in.TypeName), After: in})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) UpdateEquipment(ctx context.Context, id uuid.UUID, in EquipmentInput) (*Equipment, error) {
	p := authctx.Must(ctx)
	var out Equipment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `UPDATE equipment SET category_name = COALESCE(NULLIF($2,''), category_name), type_name = COALESCE(NULLIF($3,''), type_name), default_criticality = COALESCE($4, default_criticality), is_active = COALESCE($5, is_active), updated_by = $6
			WHERE id = $1 AND deleted_at IS NULL RETURNING id, category_code, category_name, type_name, parent_id, default_criticality, is_active, version`,
			id, in.CategoryName, derefStr(in.TypeName), in.DefaultCriticality, in.IsActive, p.UserID).
			Scan(&out.ID, &out.CategoryCode, &out.CategoryName, &out.TypeName, &out.ParentID, &out.DefaultCriticality, &out.IsActive, &out.Version)
		if err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Equipment")
			}
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "equipment", EntityID: &id, After: in})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------- Asset ----------

type Asset struct {
	ID                uuid.UUID      `json:"id"`
	PropertyID        uuid.UUID      `json:"property_id"`
	AssetCode         string         `json:"asset_code"`
	Name              string         `json:"name"`
	EquipmentID       uuid.UUID      `json:"equipment_id"`
	CategoryCode      string         `json:"category_code"`
	CategoryName      string         `json:"category_name"`
	TypeName          *string        `json:"type_name"`
	LocationID        uuid.UUID      `json:"location_id"`
	LocationName      string         `json:"location_name"`
	LocationPath      string         `json:"location_path"`
	Status            string         `json:"status"`
	Criticality       *string        `json:"criticality"`
	Manufacturer      *string        `json:"manufacturer"`
	Model             *string        `json:"model"`
	SerialNumber      *string        `json:"serial_number"`
	InstalledAt       *time.Time     `json:"installed_at"`
	WarrantyUntil     *time.Time     `json:"warranty_until"`
	Specifications    map[string]any `json:"specifications"`
	Notes             *string        `json:"notes"`
	QRCode            *string        `json:"qr_code"`
	QRURL             *string        `json:"qr_url"`
	OpenWorkOrders    int            `json:"open_work_orders"`
	NextPMDue         *time.Time     `json:"next_pm_due"`
	LastMaintenanceAt *time.Time     `json:"last_maintenance_at"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	Version           int            `json:"version"`
}

type AssetInput struct {
	PropertyID     *uuid.UUID     `json:"property_id"`
	AssetCode      *string        `json:"asset_code"` // override manual (migrasi data OD-008)
	Name           *string        `json:"name"`
	EquipmentID    *uuid.UUID     `json:"equipment_id"`
	LocationID     *uuid.UUID     `json:"location_id"`
	Status         *string        `json:"status"`
	Criticality    *string        `json:"criticality"`
	Manufacturer   *string        `json:"manufacturer"`
	Model          *string        `json:"model"`
	SerialNumber   *string        `json:"serial_number"`
	InstalledAt    *time.Time     `json:"installed_at"`
	WarrantyUntil  *time.Time     `json:"warranty_until"`
	Specifications map[string]any `json:"specifications"`
	Notes          *string        `json:"notes"`
}

var assetStatuses = map[string]bool{"active": true, "inactive": true, "under_maintenance": true, "decommissioned": true}
var criticalities = map[string]bool{"low": true, "medium": true, "high": true, "critical": true}

func (s *Service) QRBaseURL() string { return qrBase }

var qrBase = "https://bv.link/q/"

func SetQRBaseURL(u string) { qrBase = u }

func (s *Service) CreateAsset(ctx context.Context, in AssetInput) (*Asset, error) {
	var out *Asset
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.CreateAssetTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) CreateAssetTx(ctx context.Context, tx pgx.Tx, in AssetInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return uuid.Nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	if in.EquipmentID == nil {
		return uuid.Nil, apperr.Validation("equipment_id (kategori) wajib").WithField("equipment_id", "wajib")
	}
	if in.LocationID == nil {
		return uuid.Nil, apperr.Validation("location_id wajib").WithField("location_id", "wajib")
	}
	propertyID, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
	if err != nil {
		return uuid.Nil, err
	}
	if in.PropertyID != nil && *in.PropertyID != propertyID {
		return uuid.Nil, apperr.Validation("location tidak berada di property yang diberikan")
	}
	if !p.HasOnProperty("engineering.assets.create", propertyID) {
		return uuid.Nil, apperr.Forbidden("Tidak memiliki engineering.assets.create pada property ini")
	}
	var catCode string
	if err := tx.QueryRow(ctx, `SELECT category_code FROM equipment WHERE id = $1 AND deleted_at IS NULL`, *in.EquipmentID).Scan(&catCode); err != nil {
		return uuid.Nil, apperr.Validation("equipment_id tidak ditemukan")
	}
	status := "active"
	if in.Status != nil {
		status = *in.Status
	}
	if !assetStatuses[status] {
		return uuid.Nil, apperr.Validation("status tidak valid")
	}
	if in.Criticality != nil && !criticalities[*in.Criticality] {
		return uuid.Nil, apperr.Validation("criticality tidak valid")
	}
	code := ""
	if in.AssetCode != nil && strings.TrimSpace(*in.AssetCode) != "" {
		code = strings.ToUpper(strings.TrimSpace(*in.AssetCode))
	} else {
		code, err = ids.NextCategorized(ctx, tx, p.OrganizationID, ids.PrefixAsset, catCode)
		if err != nil {
			return uuid.Nil, err
		}
	}
	spec, _ := json.Marshal(orDefault(in.Specifications))
	qr, err := ids.NewQRCode()
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO assets (organization_id, property_id, asset_code, name, equipment_id, location_id, status, criticality, manufacturer, model, serial_number, installed_at, warranty_until, specifications, notes, qr_code, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17) RETURNING id`,
		p.OrganizationID, propertyID, code, strings.TrimSpace(*in.Name), *in.EquipmentID, *in.LocationID, status, in.Criticality, in.Manufacturer, in.Model, in.SerialNumber, in.InstalledAt, in.WarrantyUntil, spec, in.Notes, qr, actorOrNil(p)).Scan(&id)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return uuid.Nil, apperr.Conflict("DUPLICATE_ASSET_CODE", "Asset ID sudah digunakan")
		}
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO qr_codes (code, organization_id, object_type, object_id) VALUES ($1,$2,'asset',$3)`, qr, p.OrganizationID, id); err != nil {
		return uuid.Nil, err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"asset_code": code, "name": *in.Name}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "asset", EntityID: &id, EntityLabel: code, After: in})
	if s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.AssetCreated, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "asset", ObjectID: id, ObjectLabel: code, ActorUserID: actorOrNil(p)})
		_ = s.Jobs.EnqueueTx(ctx, tx, jobs.SearchIndexArgs{OrganizationID: p.OrganizationID, ObjectType: "asset", ObjectID: id})
	}
	return id, nil
}

func orDefault(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func actorOrNil(p *authctx.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Service) UpdateAsset(ctx context.Context, id uuid.UUID, in AssetInput, ifVersion *int) (*Asset, error) {
	p := authctx.Must(ctx)
	var out *Asset
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.HasOnProperty("engineering.assets.update", before.PropertyID) {
			return apperr.Forbidden("")
		}
		if ifVersion != nil && *ifVersion != before.Version {
			return apperr.StaleVersion()
		}
		if in.Status != nil && !assetStatuses[*in.Status] {
			return apperr.Validation("status tidak valid")
		}
		if in.Status != nil && *in.Status == "decommissioned" && !p.HasOnProperty("engineering.assets.decommission", before.PropertyID) {
			return apperr.Forbidden("Memerlukan engineering.assets.decommission")
		}
		if in.Criticality != nil && !criticalities[*in.Criticality] {
			return apperr.Validation("criticality tidak valid")
		}
		if in.LocationID != nil {
			pid, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
			if err != nil || pid != before.PropertyID {
				return apperr.Validation("location_id harus berada di property yang sama")
			}
		}
		if in.EquipmentID != nil {
			var ok bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM equipment WHERE id = $1 AND deleted_at IS NULL)`, *in.EquipmentID).Scan(&ok)
			if !ok {
				return apperr.Validation("equipment_id tidak ditemukan")
			}
		}
		var spec []byte
		if in.Specifications != nil {
			spec, _ = json.Marshal(in.Specifications)
		}
		if _, err := tx.Exec(ctx, `UPDATE assets SET name = COALESCE(NULLIF($2,''), name), equipment_id = COALESCE($3, equipment_id), location_id = COALESCE($4, location_id), status = COALESCE(NULLIF($5,''), status),
			criticality = COALESCE($6, criticality), manufacturer = COALESCE($7, manufacturer), model = COALESCE($8, model), serial_number = COALESCE($9, serial_number), installed_at = COALESCE($10, installed_at),
			warranty_until = COALESCE($11, warranty_until), specifications = COALESCE($12, specifications), notes = COALESCE($13, notes), updated_by = $14 WHERE id = $1`,
			id, derefStr(in.Name), in.EquipmentID, in.LocationID, derefStr(in.Status), in.Criticality, in.Manufacturer, in.Model, in.SerialNumber, in.InstalledAt, in.WarrantyUntil, nullBytes(spec), in.Notes, p.UserID); err != nil {
			return err
		}
		if in.Status != nil && *in.Status != before.Status {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: id, Action: audit.ActStatusChanged, From: before.Status, To: *in.Status})
		}
		if in.LocationID != nil && *in.LocationID != before.LocationID {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: id, Action: "relocated", From: before.LocationID.String(), To: in.LocationID.String()})
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: id, Action: audit.ActUpdated})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "asset", EntityID: &id, EntityLabel: before.AssetCode, Before: before, After: in})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.AssetUpdated, OrganizationID: p.OrganizationID, PropertyID: &before.PropertyID, ObjectType: "asset", ObjectID: id, ObjectLabel: before.AssetCode, ActorUserID: &p.UserID})
			_ = s.Jobs.EnqueueTx(ctx, tx, jobs.SearchIndexArgs{OrganizationID: p.OrganizationID, ObjectType: "asset", ObjectID: id})
		}
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

const assetSelect = `SELECT a.id, a.property_id, a.asset_code, a.name, a.equipment_id, e.category_code, e.category_name, e.type_name, a.location_id, l.name,
	a.status, a.criticality, a.manufacturer, a.model, a.serial_number, a.installed_at, a.warranty_until, a.specifications, a.notes, a.qr_code, a.created_at, a.updated_at, a.version,
	(SELECT count(*) FROM work_orders w WHERE w.asset_id = a.id AND w.status NOT IN ('closed','cancelled')),
	(SELECT min(ms.due_at) FROM maintenance_schedules ms WHERE ms.asset_id = a.id AND ms.status IN ('scheduled','due','overdue')),
	(SELECT max(w.closed_at) FROM work_orders w WHERE w.asset_id = a.id AND w.work_order_type = 'maintenance' AND w.status = 'closed')
	FROM assets a JOIN equipment e ON e.id = a.equipment_id JOIN locations l ON l.id = a.location_id`

func scanAsset(row pgx.Row) (*Asset, error) {
	var a Asset
	var spec []byte
	if err := row.Scan(&a.ID, &a.PropertyID, &a.AssetCode, &a.Name, &a.EquipmentID, &a.CategoryCode, &a.CategoryName, &a.TypeName, &a.LocationID, &a.LocationName,
		&a.Status, &a.Criticality, &a.Manufacturer, &a.Model, &a.SerialNumber, &a.InstalledAt, &a.WarrantyUntil, &spec, &a.Notes, &a.QRCode, &a.CreatedAt, &a.UpdatedAt, &a.Version,
		&a.OpenWorkOrders, &a.NextPMDue, &a.LastMaintenanceAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(spec, &a.Specifications)
	if a.Specifications == nil {
		a.Specifications = map[string]any{}
	}
	if a.QRCode != nil {
		u := qrBase + *a.QRCode
		a.QRURL = &u
	}
	return &a, nil
}

func (s *Service) getTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Asset, error) {
	a, err := scanAsset(tx.QueryRow(ctx, assetSelect+` WHERE a.id = $1 AND a.deleted_at IS NULL`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Asset")
		}
		return nil, err
	}
	a.LocationPath = property.LocationPathText(ctx, tx, a.LocationID)
	return a, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Asset, error) {
	var out *Asset
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasOnProperty("engineering.assets.view", a.PropertyID) {
			return apperr.Forbidden("")
		}
		out = a
		return nil
	})
	return out, err
}

type Filter struct {
	PropertyID   *uuid.UUID
	LocationID   *uuid.UUID
	EquipmentID  *uuid.UUID
	CategoryCode string
	Statuses     []string
	Criticality  []string
	Q            string
}

func (s *Service) List(ctx context.Context, f Filter, page httpx.Page) ([]Asset, *string, error) {
	p := authctx.Must(ctx)
	var out []Asset
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE a.deleted_at IS NULL"
		if f.PropertyID != nil {
			where += " AND a.property_id = " + add(*f.PropertyID)
		} else if pids, all := p.PropertyIDsFor("engineering.assets.view"); !all {
			where += " AND a.property_id = ANY(" + add(pids) + "::uuid[])"
		}
		if f.LocationID != nil {
			where += " AND a.location_id IN (SELECT d.id FROM locations d JOIN locations r ON d.path <@ r.path WHERE r.id = " + add(*f.LocationID) + ")"
		}
		if f.EquipmentID != nil {
			where += " AND (a.equipment_id = " + add(*f.EquipmentID) + " OR e.parent_id = " + add(*f.EquipmentID) + ")"
		}
		if f.CategoryCode != "" {
			where += " AND e.category_code = " + add(strings.ToUpper(f.CategoryCode))
		}
		if len(f.Statuses) > 0 {
			where += " AND a.status = ANY(" + add(f.Statuses) + ")"
		}
		if len(f.Criticality) > 0 {
			where += " AND a.criticality = ANY(" + add(f.Criticality) + ")"
		}
		if f.Q != "" {
			q := add("%" + f.Q + "%")
			where += " AND (a.asset_code ILIKE " + q + " OR a.name ILIKE " + q + " OR a.serial_number ILIKE " + q + ")"
		}
		if page.Cursor != nil {
			where += " AND (a.asset_code, a.id) > (" + add(page.Cursor.Value) + ", " + add(page.Cursor.ID) + ")"
		}
		rows, err := tx.Query(ctx, assetSelect+where+" ORDER BY a.asset_code, a.id LIMIT "+add(page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scanAsset(rows)
			if err != nil {
				return err
			}
			out = append(out, *a)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			out = out[:page.Limit]
			c := httpx.EncodeCursor(last.AssetCode, last.ID)
			next = &c
		}
		for i := range out {
			out[i].LocationPath = property.LocationPathText(ctx, tx, out[i].LocationID)
		}
		return nil
	})
	if out == nil {
		out = []Asset{}
	}
	return out, next, err
}

// History: asset history = activities asset + WO/PM terkait (Asset Management → History; WF-001 "Asset History Updated").
type HistoryItem struct {
	Kind       string         `json:"kind"` // activity | work_order | maintenance_schedule | inspection
	OccurredAt time.Time      `json:"occurred_at"`
	ObjectType string         `json:"object_type"`
	ObjectID   uuid.UUID      `json:"object_id"`
	Label      string         `json:"label"`
	Title      string         `json:"title"`
	Status     string         `json:"status"`
	ActorName  string         `json:"actor_name"`
	Payload    map[string]any `json:"payload"`
}

func (s *Service) History(ctx context.Context, id uuid.UUID, limit int) ([]HistoryItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var out []HistoryItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasOnProperty("engineering.assets.view", a.PropertyID) {
			return apperr.Forbidden("")
		}
		rows, err := tx.Query(ctx, `
			SELECT 'work_order', COALESCE(w.closed_at, w.completed_at, w.updated_at), 'work_order', w.id, w.work_order_number, w.title, w.status, COALESCE(u.full_name,''), jsonb_build_object('type', w.work_order_type, 'created_at', w.created_at, 'resolution', w.resolution)
			FROM work_orders w LEFT JOIN users u ON u.id = w.assignee_user_id WHERE w.asset_id = $1
			UNION ALL
			SELECT 'task', COALESCE(t.completed_at, t.updated_at), 'task', t.id, t.task_number, t.title, t.status, COALESCE(u.full_name,''), jsonb_build_object('type', t.task_type)
			FROM tasks t LEFT JOIN users u ON u.id = t.assignee_user_id WHERE t.asset_id = $1
			UNION ALL
			SELECT 'maintenance_schedule', ms.due_at, 'maintenance_schedule', ms.id, mp.plan_code, mp.name, ms.status, '', jsonb_build_object('due_date', ms.due_date, 'work_order_id', ms.work_order_id)
			FROM maintenance_schedules ms JOIN maintenance_plans mp ON mp.id = ms.plan_id WHERE ms.asset_id = $1
			UNION ALL
			SELECT 'activity', ac.occurred_at, 'asset', ac.object_id, ac.action, COALESCE(ac.to_value,''), COALESCE(ac.from_value,''), COALESCE(u.full_name, 'System'), ac.payload
			FROM activities ac LEFT JOIN users u ON u.id = ac.actor_user_id WHERE ac.object_type = 'asset' AND ac.object_id = $1
			ORDER BY 2 DESC LIMIT $2`, id, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h HistoryItem
			var payload []byte
			if err := rows.Scan(&h.Kind, &h.OccurredAt, &h.ObjectType, &h.ObjectID, &h.Label, &h.Title, &h.Status, &h.ActorName, &payload); err != nil {
				return err
			}
			_ = json.Unmarshal(payload, &h.Payload)
			out = append(out, h)
		}
		return rows.Err()
	})
	if out == nil {
		out = []HistoryItem{}
	}
	return out, err
}

// ---------- QR (PRD §22) ----------

type QRResolve struct {
	ObjectType string         `json:"object_type"` // asset | checkpoint | location
	ObjectID   uuid.UUID      `json:"object_id"`
	Summary    map[string]any `json:"summary"`
	DeepLink   string         `json:"deep_link"`
}

// ResolveQR: validasi org (RLS) + permission user.
func (s *Service) ResolveQR(ctx context.Context, code string) (*QRResolve, error) {
	p := authctx.Must(ctx)
	code = strings.ToUpper(strings.TrimSpace(code))
	var out *QRResolve
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var ot string
		var oid uuid.UUID
		var revoked *time.Time
		if err := tx.QueryRow(ctx, `SELECT object_type, object_id, revoked_at FROM qr_codes WHERE code = $1`, code).Scan(&ot, &oid, &revoked); err != nil {
			return apperr.NotFound("QR code")
		}
		if revoked != nil {
			return apperr.Conflict("QR_REVOKED", "QR code sudah tidak berlaku")
		}
		out = &QRResolve{ObjectType: ot, ObjectID: oid, Summary: map[string]any{}}
		switch ot {
		case "asset":
			a, err := s.getTx(ctx, tx, oid)
			if err != nil {
				return err
			}
			if !p.HasOnProperty("engineering.assets.view", a.PropertyID) {
				return apperr.Forbidden("")
			}
			out.Summary = map[string]any{"asset_code": a.AssetCode, "name": a.Name, "status": a.Status, "location_path": a.LocationPath, "category": a.CategoryName, "open_work_orders": a.OpenWorkOrders, "property_id": a.PropertyID}
			out.DeepLink = "/assets/" + a.ID.String()
		case "checkpoint":
			var name string
			var pid, lid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT name, property_id, location_id FROM checkpoints WHERE id = $1 AND deleted_at IS NULL`, oid).Scan(&name, &pid, &lid); err != nil {
				return apperr.NotFound("Checkpoint")
			}
			if !p.HasOnProperty("security.patrol.view", pid) {
				return apperr.Forbidden("")
			}
			out.Summary = map[string]any{"name": name, "location_path": property.LocationPathText(ctx, tx, lid), "property_id": pid}
			out.DeepLink = "/security/patrol/checkpoints/" + oid.String()
		case "location":
			var name, lt string
			var pid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT name, location_type, property_id FROM locations WHERE id = $1 AND deleted_at IS NULL`, oid).Scan(&name, &lt, &pid); err != nil {
				return apperr.NotFound("Lokasi")
			}
			if !p.HasOnProperty("property.locations.view", pid) {
				return apperr.Forbidden("")
			}
			out.Summary = map[string]any{"name": name, "location_type": lt, "location_path": property.LocationPathText(ctx, tx, oid), "property_id": pid}
			out.DeepLink = "/property/locations/" + oid.String()
		}
		return nil
	})
	return out, err
}

// RotateQR: regenerasi QR (stiker rusak/ditiru).
func (s *Service) RotateQR(ctx context.Context, objectType string, objectID uuid.UUID) (string, error) {
	p := authctx.Must(ctx)
	var newCode string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pid uuid.UUID
		table := map[string]string{"asset": "assets", "checkpoint": "checkpoints", "location": "locations"}[objectType]
		if table == "" {
			return apperr.Validation("object_type harus asset|checkpoint|location")
		}
		if err := tx.QueryRow(ctx, `SELECT property_id FROM `+table+` WHERE id = $1`, objectID).Scan(&pid); err != nil {
			return apperr.NotFound(objectType)
		}
		perm := map[string]string{"asset": "engineering.assets.update", "checkpoint": "security.checkpoints.update", "location": "property.locations.update"}[objectType]
		if !p.HasOnProperty(perm, pid) {
			return apperr.Forbidden("")
		}
		code, err := ids.NewQRCode()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE qr_codes SET revoked_at = now(), rotated_at = now() WHERE object_type = $1 AND object_id = $2 AND revoked_at IS NULL`, objectType, objectID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO qr_codes (code, organization_id, object_type, object_id) VALUES ($1,$2,$3,$4)`, code, p.OrganizationID, objectType, objectID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE `+table+` SET qr_code = $2 WHERE id = $1`, objectID, code); err != nil {
			return err
		}
		newCode = code
		return audit.Log(ctx, tx, audit.AuditEntry{Action: "qr_rotated", EntityType: objectType, EntityID: &objectID})
	})
	return newCode, err
}
