// Package buildingmap: Building Map / Floor Plan & Occupancy (PRD P1 v2 §7–§8).
// Denah = gambar (attachment object_type floor_plan) pada satu lokasi (building/tower/floor/area); marker menunjuk
// lokasi, facility, atau asset dengan koordinat persen sehingga tidak bergantung resolusi gambar. Setiap marker
// membawa ringkasan pekerjaan terbuka (task/WO/incident/SR) agar denah dapat membuka record operasional terkait.
package buildingmap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/property"
)

type Service struct {
	DB          *db.DB
	Attachments *attachments.Service
}

type FloorPlan struct {
	ID           uuid.UUID               `json:"id"`
	PropertyID   uuid.UUID               `json:"property_id"`
	LocationID   uuid.UUID               `json:"location_id"`
	LocationName string                  `json:"location_name"`
	LocationType string                  `json:"location_type"`
	LocationPath string                  `json:"location_path"`
	Name         string                  `json:"name"`
	Description  *string                 `json:"description"`
	AttachmentID *uuid.UUID              `json:"attachment_id"`
	Image        *attachments.Attachment `json:"image"`
	ImageWidth   *int                    `json:"image_width"`
	ImageHeight  *int                    `json:"image_height"`
	IsActive     bool                    `json:"is_active"`
	MarkerCount  int                     `json:"marker_count"`
	Markers      []Marker                `json:"markers,omitempty"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
	Version      int                     `json:"version"`
}

type Marker struct {
	ID          uuid.UUID   `json:"id"`
	FloorPlanID uuid.UUID   `json:"floor_plan_id"`
	TargetType  string      `json:"target_type"` // location | facility | asset
	TargetID    uuid.UUID   `json:"target_id"`
	TargetLabel string      `json:"target_label"` // kode/nama
	TargetName  string      `json:"target_name"`
	TargetKind  string      `json:"target_kind"` // location_type / facility_type / asset status
	XPct        float64     `json:"x_pct"`
	YPct        float64     `json:"y_pct"`
	Label       *string     `json:"label"`
	Work        WorkSummary `json:"work"`
	Version     int         `json:"version"`
}

// WorkSummary: pekerjaan terbuka pada target marker (lokasi subtree / lokasi facility / asset).
type WorkSummary struct {
	OpenTasks      int        `json:"open_tasks"`
	OpenWorkOrders int        `json:"open_work_orders"`
	OpenIncidents  int        `json:"open_incidents"`
	OpenRequests   int        `json:"open_requests"`
	Overdue        int        `json:"overdue"`
	Critical       int        `json:"critical"`
	Signal         string     `json:"signal"` // ok | attention | critical
	Items          []WorkLink `json:"items"`
}

type WorkLink struct {
	ObjectType string    `json:"object_type"`
	ObjectID   uuid.UUID `json:"object_id"`
	Number     string    `json:"number"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	Priority   string    `json:"priority"`
	DeepLink   string    `json:"deep_link"`
}

type FloorPlanInput struct {
	LocationID   *uuid.UUID `json:"location_id"`
	Name         *string    `json:"name"`
	Description  *string    `json:"description"`
	AttachmentID *uuid.UUID `json:"attachment_id"`
	ImageWidth   *int       `json:"image_width"`
	ImageHeight  *int       `json:"image_height"`
	IsActive     *bool      `json:"is_active"`
}

type MarkerInput struct {
	TargetType string     `json:"target_type"`
	TargetID   *uuid.UUID `json:"target_id"`
	XPct       *float64   `json:"x_pct"`
	YPct       *float64   `json:"y_pct"`
	Label      *string    `json:"label"`
}

type Filter struct {
	PropertyID *uuid.UUID
	LocationID *uuid.UUID // denah di lokasi ini, leluhurnya, atau turunannya
	Active     *bool
}

var planLocationTypes = map[string]bool{"building": true, "tower": true, "floor": true, "area": true}

const planSelect = `SELECT fp.id, fp.property_id, fp.location_id, l.name, l.location_type, fp.name, fp.description, fp.attachment_id, fp.image_width, fp.image_height, fp.is_active,
	(SELECT count(*) FROM floor_plan_markers m WHERE m.floor_plan_id = fp.id), fp.created_at, fp.updated_at, fp.version
	FROM floor_plans fp JOIN locations l ON l.id = fp.location_id`

func scanPlan(row pgx.Row) (*FloorPlan, error) {
	var f FloorPlan
	if err := row.Scan(&f.ID, &f.PropertyID, &f.LocationID, &f.LocationName, &f.LocationType, &f.Name, &f.Description, &f.AttachmentID, &f.ImageWidth, &f.ImageHeight, &f.IsActive,
		&f.MarkerCount, &f.CreatedAt, &f.UpdatedAt, &f.Version); err != nil {
		return nil, err
	}
	return &f, nil
}

func can(ctx context.Context, tx pgx.Tx, perm string, propertyID uuid.UUID, locationID *uuid.UUID) bool {
	return operations.CanAt(ctx, tx, perm, propertyID, locationID)
}

func (s *Service) List(ctx context.Context, f Filter) ([]FloorPlan, error) {
	p := authctx.Must(ctx)
	out := []FloorPlan{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE fp.deleted_at IS NULL"
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("property.floor_plans.view", *f.PropertyID) {
				return apperr.Forbidden("")
			}
			where += " AND fp.property_id = " + add(*f.PropertyID)
		}
		where += " AND " + p.ScopeSQL("property.floor_plans.view", "fp.property_id", "l.path", add)
		if f.LocationID != nil {
			// denah lokasi itu sendiri, leluhur (mis. denah building saat membuka floor), atau turunan
			where += " AND EXISTS (SELECT 1 FROM locations r WHERE r.id = " + add(*f.LocationID) + " AND (l.path @> r.path OR l.path <@ r.path))"
		}
		if f.Active != nil {
			where += " AND fp.is_active = " + add(*f.Active)
		}
		rows, err := tx.Query(ctx, planSelect+where+" ORDER BY l.path, fp.name", args...)
		if err != nil {
			return err
		}
		var list []*FloorPlan
		for rows.Next() {
			fp, err := scanPlan(rows)
			if err != nil {
				rows.Close()
				return err
			}
			list = append(list, fp)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, fp := range list {
			fp.LocationPath = property.LocationPathText(ctx, tx, fp.LocationID)
			s.fillImage(ctx, tx, fp)
			out = append(out, *fp)
		}
		return nil
	})
	return out, err
}

// fillImage: gambar denah = attachment_id eksplisit, atau foto ready terakhir yang diunggah ke denah.
func (s *Service) fillImage(ctx context.Context, tx pgx.Tx, fp *FloorPlan) {
	if s.Attachments == nil {
		return
	}
	id := fp.AttachmentID
	if id == nil {
		var aid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM attachments WHERE object_type = 'floor_plan' AND object_id = $1 AND status = 'ready' AND deleted_at IS NULL AND content_type LIKE 'image/%' ORDER BY uploaded_at DESC LIMIT 1`, fp.ID).Scan(&aid); err != nil {
			return
		}
		id = &aid
	}
	a, err := s.Attachments.GetTx(ctx, tx, *id)
	if err != nil {
		return
	}
	list := []attachments.Attachment{*a}
	s.Attachments.FillURLs(ctx, list)
	fp.Image = &list[0]
}

func (s *Service) loadTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, perm string) (*FloorPlan, error) {
	fp, err := scanPlan(tx.QueryRow(ctx, planSelect+` WHERE fp.id = $1 AND fp.deleted_at IS NULL`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Floor plan")
		}
		return nil, err
	}
	if !can(ctx, tx, perm, fp.PropertyID, &fp.LocationID) {
		if perm == "property.floor_plans.view" {
			return nil, apperr.NotFound("Floor plan")
		}
		return nil, apperr.Forbidden("Memerlukan " + perm)
	}
	return fp, nil
}

// Get: denah + marker + ringkasan pekerjaan per marker.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*FloorPlan, error) {
	var out *FloorPlan
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		fp, err := s.loadTx(ctx, tx, id, "property.floor_plans.view")
		if err != nil {
			return err
		}
		fp.LocationPath = property.LocationPathText(ctx, tx, fp.LocationID)
		s.fillImage(ctx, tx, fp)
		fp.Markers, err = s.markersTx(ctx, tx, fp.ID)
		if err != nil {
			return err
		}
		out = fp
		return nil
	})
	return out, err
}

func (s *Service) Create(ctx context.Context, in FloorPlanInput) (*FloorPlan, error) {
	p := authctx.Must(ctx)
	if in.LocationID == nil {
		return nil, apperr.Validation("location_id wajib").WithField("location_id", "wajib")
	}
	name := strings.TrimSpace(deref(in.Name))
	if name == "" {
		return nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	if err := validDims(in); err != nil {
		return nil, err
	}
	var out *FloorPlan
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var propertyID uuid.UUID
		var lt string
		if err := tx.QueryRow(ctx, `SELECT property_id, location_type FROM locations WHERE id = $1 AND deleted_at IS NULL`, *in.LocationID).Scan(&propertyID, &lt); err != nil {
			return apperr.Validation("location_id tidak ditemukan").WithField("location_id", "tidak valid")
		}
		if !planLocationTypes[lt] {
			return apperr.Validation("Denah hanya untuk building, tower, floor, atau area").WithField("location_id", "tipe lokasi tidak didukung")
		}
		if !can(ctx, tx, "property.floor_plans.create", propertyID, in.LocationID) {
			return apperr.Forbidden("Memerlukan property.floor_plans.create")
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO floor_plans (organization_id, property_id, location_id, name, description, image_width, image_height, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING id`, p.OrganizationID, propertyID, *in.LocationID, name, in.Description, in.ImageWidth, in.ImageHeight, p.UserID).Scan(&id); err != nil {
			return err
		}
		if in.AttachmentID != nil {
			if err := s.checkAttachment(ctx, tx, id, *in.AttachmentID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE floor_plans SET attachment_id = $2 WHERE id = $1`, id, *in.AttachmentID); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "floor_plan", EntityID: &id, EntityLabel: name})
		fp, err := s.loadTx(ctx, tx, id, "property.floor_plans.view")
		if err != nil {
			return err
		}
		fp.LocationPath = property.LocationPathText(ctx, tx, fp.LocationID)
		out = fp
		return nil
	})
	return out, err
}

func validDims(in FloorPlanInput) error {
	for f, v := range map[string]*int{"image_width": in.ImageWidth, "image_height": in.ImageHeight} {
		if v != nil && *v <= 0 {
			return apperr.Validation(f+" harus > 0").WithField(f, "harus > 0")
		}
	}
	return nil
}

// checkAttachment: gambar denah harus attachment gambar milik denah ini (object_type floor_plan).
func (s *Service) checkAttachment(ctx context.Context, tx pgx.Tx, planID, attID uuid.UUID) error {
	var ot, ct string
	var oid uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT object_type, object_id, content_type FROM attachments WHERE id = $1 AND deleted_at IS NULL`, attID).Scan(&ot, &oid, &ct); err != nil {
		return apperr.Validation("attachment_id tidak ditemukan").WithField("attachment_id", "tidak valid")
	}
	if ot != "floor_plan" || oid != planID {
		return apperr.Validation("attachment_id harus diunggah ke denah ini (object_type floor_plan)").WithField("attachment_id", "bukan milik denah")
	}
	if !attachments.IsImage(ct) {
		return apperr.Validation("Gambar denah harus berupa image (PNG/JPEG/WebP)").WithField("attachment_id", "bukan gambar")
	}
	return nil
}

// Update: If-Match (version) opsional — bila dikirim dan tidak cocok → 409 STALE_VERSION (PRD P1 v2.1 P1-BLD-06).
func (s *Service) Update(ctx context.Context, id uuid.UUID, in FloorPlanInput, ifVersion *int) (*FloorPlan, error) {
	p := authctx.Must(ctx)
	if err := validDims(in); err != nil {
		return nil, err
	}
	var out *FloorPlan
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.loadTx(ctx, tx, id, "property.floor_plans.update")
		if err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != before.Version {
			return apperr.StaleVersion()
		}
		if in.AttachmentID != nil {
			if err := s.checkAttachment(ctx, tx, id, *in.AttachmentID); err != nil {
				return err
			}
		}
		if in.LocationID != nil && *in.LocationID != before.LocationID {
			var pid uuid.UUID
			var lt string
			if err := tx.QueryRow(ctx, `SELECT property_id, location_type FROM locations WHERE id = $1 AND deleted_at IS NULL`, *in.LocationID).Scan(&pid, &lt); err != nil || pid != before.PropertyID || !planLocationTypes[lt] {
				return apperr.Validation("location_id tidak valid untuk denah").WithField("location_id", "tidak valid")
			}
		}
		if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
			return apperr.Validation("name tidak boleh kosong").WithField("name", "wajib")
		}
		if _, err := tx.Exec(ctx, `UPDATE floor_plans SET name = COALESCE(NULLIF(TRIM($2),''), name), description = COALESCE($3, description), attachment_id = COALESCE($4, attachment_id),
			image_width = COALESCE($5, image_width), image_height = COALESCE($6, image_height), is_active = COALESCE($7, is_active), location_id = COALESCE($8, location_id), updated_by = $9 WHERE id = $1`,
			id, deref(in.Name), in.Description, in.AttachmentID, in.ImageWidth, in.ImageHeight, in.IsActive, in.LocationID, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "floor_plan", EntityID: &id, EntityLabel: before.Name, Before: before, After: in})
		fp, err := s.loadTx(ctx, tx, id, "property.floor_plans.view")
		if err != nil {
			return err
		}
		fp.LocationPath = property.LocationPathText(ctx, tx, fp.LocationID)
		s.fillImage(ctx, tx, fp)
		fp.Markers, err = s.markersTx(ctx, tx, id)
		out = fp
		return err
	})
	return out, err
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		fp, err := s.loadTx(ctx, tx, id, "property.floor_plans.delete")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE floor_plans SET deleted_at = now(), is_active = false, updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "floor_plan", EntityID: &id, EntityLabel: fp.Name})
		return nil
	})
}

// ---------- Markers ----------

// resolveTarget: target harus berada di property denah; mengembalikan label, nama, kind.
func resolveTarget(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, targetType string, targetID uuid.UUID) (string, string, string, error) {
	var pid uuid.UUID
	var label, name, kind string
	var err error
	switch targetType {
	case "location":
		err = tx.QueryRow(ctx, `SELECT property_id, code, name, location_type FROM locations WHERE id = $1 AND deleted_at IS NULL`, targetID).Scan(&pid, &label, &name, &kind)
	case "facility":
		err = tx.QueryRow(ctx, `SELECT property_id, facility_code, name, facility_type FROM facilities WHERE id = $1 AND deleted_at IS NULL`, targetID).Scan(&pid, &label, &name, &kind)
	case "asset":
		err = tx.QueryRow(ctx, `SELECT property_id, asset_code, name, status FROM assets WHERE id = $1 AND deleted_at IS NULL`, targetID).Scan(&pid, &label, &name, &kind)
	default:
		return "", "", "", apperr.Validation("target_type harus location|facility|asset").WithField("target_type", "tidak valid")
	}
	if err != nil || pid != propertyID {
		return "", "", "", apperr.Validation("target_id tidak ditemukan di property denah").WithField("target_id", "tidak valid")
	}
	return label, name, kind, nil
}

func validPct(v *float64, field string) error {
	if v == nil {
		return apperr.Validation(field+" wajib").WithField(field, "wajib")
	}
	if *v < 0 || *v > 100 {
		return apperr.Validation(field+" harus 0–100").WithField(field, "0–100")
	}
	return nil
}

// UpsertMarker: satu target hanya satu marker per denah (posisi diperbarui bila sudah ada).
func (s *Service) UpsertMarker(ctx context.Context, planID uuid.UUID, in MarkerInput) (*Marker, error) {
	p := authctx.Must(ctx)
	if in.TargetID == nil {
		return nil, apperr.Validation("target_id wajib").WithField("target_id", "wajib")
	}
	if err := validPct(in.XPct, "x_pct"); err != nil {
		return nil, err
	}
	if err := validPct(in.YPct, "y_pct"); err != nil {
		return nil, err
	}
	var out *Marker
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		fp, err := s.loadTx(ctx, tx, planID, "property.floor_plans.update")
		if err != nil {
			return err
		}
		if _, _, _, err := resolveTarget(ctx, tx, fp.PropertyID, in.TargetType, *in.TargetID); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO floor_plan_markers (organization_id, floor_plan_id, target_type, target_id, x_pct, y_pct, label, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)
			ON CONFLICT (floor_plan_id, target_type, target_id) DO UPDATE SET x_pct = EXCLUDED.x_pct, y_pct = EXCLUDED.y_pct, label = COALESCE(EXCLUDED.label, floor_plan_markers.label), updated_by = EXCLUDED.updated_by
			RETURNING id`, p.OrganizationID, planID, in.TargetType, *in.TargetID, *in.XPct, *in.YPct, in.Label, p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "floor_plan", EntityID: &planID, EntityLabel: fp.Name, After: map[string]any{"marker": in}})
		ms, err := s.markersTx(ctx, tx, planID)
		if err != nil {
			return err
		}
		for i := range ms {
			if ms[i].ID == id {
				out = &ms[i]
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) UpdateMarker(ctx context.Context, planID, markerID uuid.UUID, in MarkerInput, ifVersion *int) (*Marker, error) {
	p := authctx.Must(ctx)
	for f, v := range map[string]*float64{"x_pct": in.XPct, "y_pct": in.YPct} {
		if v != nil {
			if err := validPct(v, f); err != nil {
				return nil, err
			}
		}
	}
	var out *Marker
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := s.loadTx(ctx, tx, planID, "property.floor_plans.update"); err != nil {
			return err
		}
		if ifVersion != nil {
			var v int
			if err := tx.QueryRow(ctx, `SELECT version FROM floor_plan_markers WHERE id = $2 AND floor_plan_id = $1 FOR UPDATE`, planID, markerID).Scan(&v); err != nil {
				return apperr.NotFound("Marker")
			}
			if v != *ifVersion {
				return apperr.StaleVersion()
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE floor_plan_markers SET x_pct = COALESCE($3, x_pct), y_pct = COALESCE($4, y_pct), label = COALESCE($5, label), updated_by = $6 WHERE id = $2 AND floor_plan_id = $1`,
			planID, markerID, in.XPct, in.YPct, in.Label, p.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Marker")
		}
		ms, err := s.markersTx(ctx, tx, planID)
		for i := range ms {
			if ms[i].ID == markerID {
				out = &ms[i]
			}
		}
		return err
	})
	return out, err
}

func (s *Service) DeleteMarker(ctx context.Context, planID, markerID uuid.UUID) error {
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := s.loadTx(ctx, tx, planID, "property.floor_plans.update"); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM floor_plan_markers WHERE id = $2 AND floor_plan_id = $1`, planID, markerID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Marker")
		}
		return nil
	})
}

// MarkerRef: posisi target pada denah (reverse lookup: "tampilkan di denah" dari asset/lokasi/facility).
type MarkerRef struct {
	FloorPlanID   uuid.UUID `json:"floor_plan_id"`
	FloorPlanName string    `json:"floor_plan_name"`
	MarkerID      uuid.UUID `json:"marker_id"`
	XPct          float64   `json:"x_pct"`
	YPct          float64   `json:"y_pct"`
}

func (s *Service) FindTarget(ctx context.Context, targetType string, targetID uuid.UUID) ([]MarkerRef, error) {
	out := []MarkerRef{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT fp.id, fp.name, m.id, m.x_pct, m.y_pct, fp.property_id, fp.location_id FROM floor_plan_markers m JOIN floor_plans fp ON fp.id = m.floor_plan_id
			WHERE m.target_type = $1 AND m.target_id = $2 AND fp.deleted_at IS NULL AND fp.is_active ORDER BY fp.name`, targetType, targetID)
		if err != nil {
			return err
		}
		type rr struct {
			r    MarkerRef
			prop uuid.UUID
			loc  uuid.UUID
		}
		var list []rr
		for rows.Next() {
			var x rr
			if err := rows.Scan(&x.r.FloorPlanID, &x.r.FloorPlanName, &x.r.MarkerID, &x.r.XPct, &x.r.YPct, &x.prop, &x.loc); err != nil {
				rows.Close()
				return err
			}
			list = append(list, x)
		}
		rows.Close()
		for _, x := range list {
			if can(ctx, tx, "property.floor_plans.view", x.prop, &x.loc) {
				out = append(out, x.r)
			}
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) markersTx(ctx context.Context, tx pgx.Tx, planID uuid.UUID) ([]Marker, error) {
	rows, err := tx.Query(ctx, `SELECT m.id, m.floor_plan_id, m.target_type, m.target_id, m.x_pct::float8, m.y_pct::float8, m.label, m.version, fp.property_id
		FROM floor_plan_markers m JOIN floor_plans fp ON fp.id = m.floor_plan_id WHERE m.floor_plan_id = $1 ORDER BY m.created_at`, planID)
	if err != nil {
		return nil, err
	}
	var out []Marker
	var prop uuid.UUID
	for rows.Next() {
		var m Marker
		if err := rows.Scan(&m.ID, &m.FloorPlanID, &m.TargetType, &m.TargetID, &m.XPct, &m.YPct, &m.Label, &m.Version, &prop); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		m := &out[i]
		label, name, kind, err := resolveTarget(ctx, tx, prop, m.TargetType, m.TargetID)
		if err != nil {
			// target dihapus: tampilkan apa adanya
			label, name = "—", "(target tidak tersedia)"
		}
		m.TargetLabel, m.TargetName, m.TargetKind = label, name, kind
		m.Work = s.workSummaryTx(ctx, tx, m.TargetType, m.TargetID)
	}
	if out == nil {
		out = []Marker{}
	}
	return out, nil
}

// workSummaryTx menghitung pekerjaan terbuka untuk target marker; item diurut prioritas (maks 5).
func (s *Service) workSummaryTx(ctx context.Context, tx pgx.Tx, targetType string, targetID uuid.UUID) WorkSummary {
	ws := WorkSummary{Items: []WorkLink{}, Signal: "ok"}
	// predikat lokasi (subtree) atau asset
	var locPred, assetPred string
	switch targetType {
	case "location":
		locPred = `x.location_id IN (SELECT d.id FROM locations d JOIN locations r ON d.path <@ r.path WHERE r.id = $1)`
	case "facility":
		locPred = `x.location_id IN (SELECT d.id FROM locations d JOIN locations r ON d.path <@ r.path JOIN facilities f ON f.location_id = r.id WHERE f.id = $1)`
	case "asset":
		assetPred = `x.asset_id = $1`
	default:
		return ws
	}
	pred := locPred
	if assetPred != "" {
		pred = assetPred
	}
	q := `WITH x AS (
		SELECT 'task' AS ot, id, task_number AS num, title, status, priority, location_id, asset_id, (is_overdue OR due_at < now()) AS overdue FROM tasks WHERE status NOT IN ('completed','closed','cancelled')
		UNION ALL SELECT 'work_order', id, work_order_number, title, status, priority, location_id, asset_id, (is_overdue OR due_at < now()) FROM work_orders WHERE status NOT IN ('completed','closed','cancelled','draft')
		UNION ALL SELECT 'incident', id, incident_number, title, status, severity, location_id, NULL::uuid, false FROM incidents WHERE status NOT IN ('resolved','closed','cancelled')
		UNION ALL SELECT 'service_request', id, request_number, title, status, priority, location_id, NULL::uuid, false FROM service_requests WHERE status NOT IN ('resolved','closed','cancelled')
	) SELECT ot, id, num, title, status, priority, overdue FROM x WHERE ` + pred + `
	ORDER BY CASE priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END, overdue DESC LIMIT 200`
	rows, err := tx.Query(ctx, q, targetID)
	if err != nil {
		return ws
	}
	defer rows.Close()
	for rows.Next() {
		var l WorkLink
		var overdue bool
		if err := rows.Scan(&l.ObjectType, &l.ObjectID, &l.Number, &l.Title, &l.Status, &l.Priority, &overdue); err != nil {
			return ws
		}
		switch l.ObjectType {
		case "task":
			ws.OpenTasks++
		case "work_order":
			ws.OpenWorkOrders++
		case "incident":
			ws.OpenIncidents++
		case "service_request":
			ws.OpenRequests++
		}
		if overdue {
			ws.Overdue++
		}
		if l.Priority == "critical" {
			ws.Critical++
		}
		if len(ws.Items) < 5 {
			l.DeepLink = deepLink(l.ObjectType, l.ObjectID)
			ws.Items = append(ws.Items, l)
		}
	}
	switch {
	case ws.Critical > 0 || ws.Overdue > 0:
		ws.Signal = "critical"
	case ws.OpenTasks+ws.OpenWorkOrders+ws.OpenIncidents+ws.OpenRequests > 0:
		ws.Signal = "attention"
	}
	return ws
}

func deepLink(objectType string, id uuid.UUID) string {
	route := map[string]string{"task": "/operations/tasks/", "work_order": "/operations/work-orders/", "service_request": "/operations/service-requests/", "incident": "/operations/incidents/"}[objectType]
	return route + id.String()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
