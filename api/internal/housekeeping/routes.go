package housekeeping

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Cleaning Route (PRD P2 v2.1 §7.3 P2-RTE-01..03, P2-SHF-04; Roadmap v2.1 §10, §39.2) ----------
// Route = urutan area (stop) per staf/shift dengan estimasi waktu. Setiap hari aktif route menghasilkan satu
// Cleaning Route Run (CRT-… + tanggal) berisi cleaning task berurutan (stop 1..n, jadwal berantai sesuai estimasi).
// Progres run (x dari y area) diperbarui oleh cleaningHook saat task berubah status — setara progres patrol.

const ObjCleaningRouteRun = "cleaning_route_run"

type RouteStop struct {
	ID                  uuid.UUID  `json:"id"`
	LocationID          uuid.UUID  `json:"location_id"`
	LocationName        string     `json:"location_name"`
	LocationPath        string     `json:"location_path"`
	SortOrder           int        `json:"sort_order"`
	EstimatedMinutes    int        `json:"estimated_minutes"`
	OffsetMinutes       int        `json:"offset_minutes"` // menit sejak start_time route
	ChecklistTemplateID *uuid.UUID `json:"checklist_template_id"`
	Notes               *string    `json:"notes"`
}

type CleaningRoute struct {
	ID                    uuid.UUID   `json:"id"`
	PropertyID            uuid.UUID   `json:"property_id"`
	RouteCode             string      `json:"route_code"`
	Name                  string      `json:"name"`
	Description           *string     `json:"description"`
	CleaningType          string      `json:"cleaning_type"`
	ShiftID               *uuid.UUID  `json:"shift_id"`
	ShiftName             *string     `json:"shift_name"`
	StartTime             string      `json:"start_time"`
	Weekdays              []int       `json:"weekdays"`
	ResponsibleTeamID     *uuid.UUID  `json:"responsible_team_id"`
	ResponsibleTeamName   *string     `json:"responsible_team_name"`
	DefaultAssigneeUserID *uuid.UUID  `json:"default_assignee_user_id"`
	DefaultAssigneeName   *string     `json:"default_assignee_name"`
	ChecklistTemplateID   *uuid.UUID  `json:"checklist_template_id"`
	RequiresPhoto         bool        `json:"requires_photo"`
	Priority              string      `json:"priority"`
	IsActive              bool        `json:"is_active"`
	TotalMinutes          int         `json:"total_minutes"`
	Stops                 []RouteStop `json:"stops"`
	TodayRun              *RouteRun   `json:"today_run,omitempty"`
	Version               int         `json:"version"`
}

type RouteStopInput struct {
	LocationID          *uuid.UUID `json:"location_id"`
	EstimatedMinutes    *int       `json:"estimated_minutes"`
	ChecklistTemplateID *uuid.UUID `json:"checklist_template_id"`
	Notes               *string    `json:"notes"`
}

type RouteInput struct {
	PropertyID            *uuid.UUID        `json:"property_id"`
	Name                  *string           `json:"name"`
	Description           *string           `json:"description"`
	CleaningType          *string           `json:"cleaning_type"`
	ShiftID               *uuid.UUID        `json:"shift_id"`
	StartTime             *string           `json:"start_time"` // HH:MM; default = jam mulai shift, lalu 08:00
	Weekdays              *[]int            `json:"weekdays"`
	ResponsibleTeamID     *uuid.UUID        `json:"responsible_team_id"`
	DefaultAssigneeUserID *uuid.UUID        `json:"default_assignee_user_id"`
	ChecklistTemplateID   *uuid.UUID        `json:"checklist_template_id"`
	RequiresPhoto         *bool             `json:"requires_photo"`
	Priority              *string           `json:"priority"`
	IsActive              *bool             `json:"is_active"`
	Stops                 *[]RouteStopInput `json:"stops"`
}

type RouteRunStop struct {
	TaskID           uuid.UUID  `json:"task_id"`
	TaskNumber       string     `json:"task_number"`
	Title            string     `json:"title"`
	Status           string     `json:"status"`
	SortOrder        int        `json:"sort_order"`
	LocationID       *uuid.UUID `json:"location_id"`
	LocationName     *string    `json:"location_name"`
	ScheduledStartAt *time.Time `json:"scheduled_start_at"`
	DueAt            *time.Time `json:"due_at"`
	CompletedAt      *time.Time `json:"completed_at"`
}

type RouteRun struct {
	ID             uuid.UUID      `json:"id"`
	PropertyID     uuid.UUID      `json:"property_id"`
	RouteID        uuid.UUID      `json:"route_id"`
	RouteCode      string         `json:"route_code"`
	RouteName      string         `json:"route_name"`
	CleaningType   string         `json:"cleaning_type"`
	ShiftName      *string        `json:"shift_name"`
	RunDate        string         `json:"run_date"`
	AssigneeUserID *uuid.UUID     `json:"assignee_user_id"`
	AssigneeName   *string        `json:"assignee_name"`
	TeamID         *uuid.UUID     `json:"team_id"`
	TeamName       *string        `json:"team_name"`
	Status         string         `json:"status"` // scheduled | in_progress | completed | cancelled
	TotalStops     int            `json:"total_stops"`
	CompletedStops int            `json:"completed_stops"`
	ProgressPct    int            `json:"progress_pct"`
	StartedAt      *time.Time     `json:"started_at"`
	CompletedAt    *time.Time     `json:"completed_at"`
	Stops          []RouteRunStop `json:"stops"`
	NextStop       *RouteRunStop  `json:"next_stop"`
}

// routePermAt: scope Building/Tower (P2-NFR-05) — izin property-wide, atau grant ber-scope yang mencakup SEMUA
// lokasi stop (menulis) / SALAH SATU lokasi stop (melihat).
func routePermAt(ctx context.Context, tx pgx.Tx, action string, propertyID uuid.UUID, locIDs []uuid.UUID, all bool) error {
	p := authctx.Must(ctx)
	perm := "housekeeping.cleaning_routes." + action
	if p.HasOnProperty(perm, propertyID) {
		return nil
	}
	ok := false
	for _, id := range locIDs {
		id := id
		in := p.HasOnPropertyAt(perm, propertyID, db.LocationPathFn(ctx, tx, &id))
		if all && !in {
			ok = false
			break
		}
		if in {
			ok = true
			if !all {
				break
			}
		}
	}
	if ok {
		return nil
	}
	return apperr.Forbidden("Memerlukan " + perm + " pada lokasi route")
}

func stopLocations(r *CleaningRoute) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(r.Stops))
	for _, st := range r.Stops {
		out = append(out, st.LocationID)
	}
	return out
}

func inputLocations(stops []RouteStopInput) []uuid.UUID {
	out := []uuid.UUID{}
	for _, st := range stops {
		if st.LocationID != nil {
			out = append(out, *st.LocationID)
		}
	}
	return out
}

// routeScopeSQL: route terlihat bila salah satu stop berada dalam scope permission view.
func routeScopeSQL(p *authctx.Principal, routeAlias string, add func(any) string) string {
	return "EXISTS (SELECT 1 FROM cleaning_route_stops ss JOIN locations sl ON sl.id = ss.location_id WHERE ss.route_id = " + routeAlias + ".id AND " +
		p.ScopeSQL("housekeeping.cleaning_routes.view", routeAlias+".property_id", "sl.path", add) + ")"
}

// shiftForRoute: shift wajib domain housekeeping di property yang sama (D-P2-05 shift per domain).
func shiftForRoute(ctx context.Context, tx pgx.Tx, shiftID, propertyID uuid.UUID) (string, error) {
	var pid uuid.UUID
	var domain string
	var start time.Time
	if err := tx.QueryRow(ctx, `SELECT property_id, domain, start_time FROM shift_definitions WHERE id = $1 AND is_active`, shiftID).Scan(&pid, &domain, &start); err != nil {
		return "", apperr.Validation("shift_id tidak ditemukan").WithField("shift_id", "tidak valid")
	}
	if pid != propertyID || domain != "housekeeping" {
		return "", apperr.Validation("shift_id harus shift Housekeeping di property yang sama").WithField("shift_id", "tidak valid")
	}
	return start.Format("15:04"), nil
}

func validWeekdays(wd []int) error {
	if len(wd) == 0 {
		return apperr.Validation("weekdays minimal satu hari").WithField("weekdays", "wajib")
	}
	for _, d := range wd {
		if d < 0 || d > 6 {
			return apperr.Validation("weekdays berisi 0 (Minggu) … 6 (Sabtu)").WithField("weekdays", "tidak valid")
		}
	}
	return nil
}

// validateStops: lokasi wajib di property route; estimasi > 0 (default 15 menit).
func validateStops(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, stops []RouteStopInput) error {
	if len(stops) == 0 {
		return apperr.Validation("stops minimal satu area").WithField("stops", "wajib")
	}
	for i, st := range stops {
		if st.LocationID == nil {
			return apperr.Validation(fmt.Sprintf("stops[%d].location_id wajib", i)).WithField("stops", "location_id wajib")
		}
		pid, err := property.ResolvePropertyOfLocation(ctx, tx, *st.LocationID)
		if err != nil || pid != propertyID {
			return apperr.Validation(fmt.Sprintf("stops[%d].location_id tidak berada di property route", i)).WithField("stops", "lokasi di luar property")
		}
		if st.EstimatedMinutes != nil && *st.EstimatedMinutes <= 0 {
			return apperr.Validation(fmt.Sprintf("stops[%d].estimated_minutes harus > 0", i)).WithField("stops", "estimasi > 0")
		}
	}
	return nil
}

func insertStopsTx(ctx context.Context, tx pgx.Tx, orgID, routeID uuid.UUID, stops []RouteStopInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM cleaning_route_stops WHERE route_id = $1`, routeID); err != nil {
		return err
	}
	for i, st := range stops {
		est := 15
		if st.EstimatedMinutes != nil {
			est = *st.EstimatedMinutes
		}
		if _, err := tx.Exec(ctx, `INSERT INTO cleaning_route_stops (organization_id, route_id, location_id, sort_order, estimated_minutes, checklist_template_id, notes) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			orgID, routeID, *st.LocationID, i+1, est, st.ChecklistTemplateID, nilIfEmpty(st.Notes)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CreateRoute(ctx context.Context, in RouteInput) (*CleaningRoute, error) {
	p := authctx.Must(ctx)
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	if in.Stops == nil || len(*in.Stops) == 0 {
		return nil, apperr.Validation("stops minimal satu area").WithField("stops", "wajib")
	}
	ct := "routine"
	if in.CleaningType != nil {
		ct = *in.CleaningType
	}
	if !cleaningTypes[ct] {
		return nil, apperr.Validation("cleaning_type harus routine|periodic|deep|spot|special").WithField("cleaning_type", "tidak valid")
	}
	prio := "medium"
	if in.Priority != nil {
		prio = *in.Priority
	}
	if !operations.Priorities[prio] {
		return nil, apperr.Validation("priority tidak valid").WithField("priority", "tidak valid")
	}
	wd := []int{0, 1, 2, 3, 4, 5, 6}
	if in.Weekdays != nil {
		wd = *in.Weekdays
	}
	if err := validWeekdays(wd); err != nil {
		return nil, err
	}
	var out *CleaningRoute
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var propertyID uuid.UUID
		if in.PropertyID != nil {
			propertyID = *in.PropertyID
		} else if first := (*in.Stops)[0]; first.LocationID != nil {
			pid, err := property.ResolvePropertyOfLocation(ctx, tx, *first.LocationID)
			if err != nil {
				return err
			}
			propertyID = pid
		} else {
			return apperr.Validation("property_id atau stops[0].location_id wajib")
		}
		if err := routePermAt(ctx, tx, "create", propertyID, inputLocations(*in.Stops), true); err != nil {
			return err
		}
		if err := validateStops(ctx, tx, propertyID, *in.Stops); err != nil {
			return err
		}
		start := "08:00"
		if in.ShiftID != nil {
			shiftStart, err := shiftForRoute(ctx, tx, *in.ShiftID, propertyID)
			if err != nil {
				return err
			}
			start = shiftStart
		}
		if in.StartTime != nil && *in.StartTime != "" {
			if _, err := time.Parse("15:04", *in.StartTime); err != nil {
				return apperr.Validation("start_time harus HH:MM").WithField("start_time", "format HH:MM")
			}
			start = *in.StartTime
		}
		reqPhoto := false
		if in.RequiresPhoto != nil {
			reqPhoto = *in.RequiresPhoto
		}
		code, err := ids.NextPlain(ctx, tx, p.OrganizationID, ids.PrefixCleaningRoute)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO cleaning_routes (organization_id, property_id, route_code, name, description, cleaning_type, shift_id, start_time, weekdays, responsible_team_id, default_assignee_user_id, checklist_template_id, requires_photo, priority, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::time,$9,$10,$11,$12,$13,$14,$15,$15) RETURNING id`,
			p.OrganizationID, propertyID, code, strings.TrimSpace(*in.Name), nilIfEmpty(in.Description), ct, in.ShiftID, start, wd, in.ResponsibleTeamID, in.DefaultAssigneeUserID, in.ChecklistTemplateID, reqPhoto, prio, actorOrNil(p)).Scan(&id); err != nil {
			return err
		}
		if err := insertStopsTx(ctx, tx, p.OrganizationID, id, *in.Stops); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "cleaning_route", EntityID: &id, EntityLabel: code + " " + strings.TrimSpace(*in.Name), After: in})
		if _, err := s.generateRunsForRouteTx(ctx, tx, id); err != nil {
			return err
		}
		out, err = s.getRouteTx(ctx, tx, id)
		return err
	})
	return out, err
}

// cancelFutureRunsTx: run yang belum dimulai (tanggal > hari ini) dibatalkan & dihapus agar dapat dibuat ulang
// sesuai definisi route terbaru (pola yang sama dengan UpdateSchedule cleaning).
func cancelFutureRunsTx(ctx context.Context, tx pgx.Tx, routeID uuid.UUID, includeToday bool) error {
	cond := "rr.run_date > CURRENT_DATE"
	if includeToday {
		cond = "rr.run_date >= CURRENT_DATE"
	}
	rows, err := tx.Query(ctx, `SELECT rr.id FROM cleaning_route_runs rr WHERE rr.route_id = $1 AND rr.status = 'scheduled' AND `+cond, routeID)
	if err != nil {
		return err
	}
	var runIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			runIDs = append(runIDs, id)
		}
	}
	rows.Close()
	for _, runID := range runIDs {
		if _, err := tx.Exec(ctx, `UPDATE tasks t SET status = 'cancelled', cancelled_at = now() FROM cleaning_tasks ct WHERE ct.task_id = t.id AND ct.route_run_id = $1 AND t.status IN ('new','scheduled','assigned')`, runID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM cleaning_tasks WHERE route_run_id = $1 AND task_id IN (SELECT id FROM tasks WHERE status = 'cancelled')`, runID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM cleaning_route_runs rr WHERE rr.id = $1 AND NOT EXISTS (SELECT 1 FROM cleaning_tasks ct WHERE ct.route_run_id = rr.id)`, runID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) UpdateRoute(ctx context.Context, id uuid.UUID, in RouteInput, ifVersion *int) (*CleaningRoute, error) {
	p := authctx.Must(ctx)
	if in.CleaningType != nil && !cleaningTypes[*in.CleaningType] {
		return nil, apperr.Validation("cleaning_type tidak valid").WithField("cleaning_type", "tidak valid")
	}
	if in.Priority != nil && !operations.Priorities[*in.Priority] {
		return nil, apperr.Validation("priority tidak valid").WithField("priority", "tidak valid")
	}
	if in.Weekdays != nil {
		if err := validWeekdays(*in.Weekdays); err != nil {
			return nil, err
		}
	}
	if in.StartTime != nil && *in.StartTime != "" {
		if _, err := time.Parse("15:04", *in.StartTime); err != nil {
			return nil, apperr.Validation("start_time harus HH:MM").WithField("start_time", "format HH:MM")
		}
	}
	var out *CleaningRoute
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getRouteTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := routePermAt(ctx, tx, "update", before.PropertyID, stopLocations(before), true); err != nil {
			return err
		}
		if in.Stops != nil {
			if err := routePermAt(ctx, tx, "update", before.PropertyID, inputLocations(*in.Stops), true); err != nil {
				return err
			}
		}
		if ifVersion != nil && *ifVersion != before.Version {
			return apperr.StaleVersion()
		}
		start := in.StartTime
		if in.ShiftID != nil && *in.ShiftID != uuid.Nil {
			shiftStart, err := shiftForRoute(ctx, tx, *in.ShiftID, before.PropertyID)
			if err != nil {
				return err
			}
			if start == nil || *start == "" {
				start = &shiftStart
			}
		}
		if in.Stops != nil {
			if err := validateStops(ctx, tx, before.PropertyID, *in.Stops); err != nil {
				return err
			}
		}
		// UUID nol mengosongkan referensi opsional; string kosong mengosongkan deskripsi
		if _, err := tx.Exec(ctx, `UPDATE cleaning_routes SET name = COALESCE(NULLIF($2,''), name), description = CASE WHEN $3::text IS NULL THEN description ELSE NULLIF(btrim($3::text), '') END, cleaning_type = COALESCE(NULLIF($4,''), cleaning_type),
			shift_id = CASE WHEN $5::uuid IS NULL THEN shift_id WHEN $5::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL ELSE $5::uuid END, start_time = COALESCE(NULLIF($6,'')::time, start_time), weekdays = COALESCE($7, weekdays), responsible_team_id = CASE WHEN $8::uuid IS NULL THEN responsible_team_id WHEN $8::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL ELSE $8::uuid END,
			default_assignee_user_id = CASE WHEN $9::uuid IS NULL THEN default_assignee_user_id WHEN $9::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL ELSE $9::uuid END, checklist_template_id = CASE WHEN $10::uuid IS NULL THEN checklist_template_id WHEN $10::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL ELSE $10::uuid END, requires_photo = COALESCE($11, requires_photo),
			priority = COALESCE(NULLIF($12,''), priority), is_active = COALESCE($13, is_active), updated_by = $14 WHERE id = $1`,
			id, deref(in.Name), in.Description, deref(in.CleaningType), in.ShiftID, deref(start), in.Weekdays, in.ResponsibleTeamID, in.DefaultAssigneeUserID, in.ChecklistTemplateID, in.RequiresPhoto,
			deref(in.Priority), in.IsActive, p.UserID); err != nil {
			return err
		}
		if in.Stops != nil {
			if err := insertStopsTx(ctx, tx, p.OrganizationID, id, *in.Stops); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "cleaning_route", EntityID: &id, EntityLabel: before.RouteCode + " " + before.Name, Before: before, After: in})
		if err := cancelFutureRunsTx(ctx, tx, id, false); err != nil {
			return err
		}
		if _, err := s.generateRunsForRouteTx(ctx, tx, id); err != nil {
			return err
		}
		out, err = s.getRouteTx(ctx, tx, id)
		return err
	})
	return out, err
}

// DeactivateRoute: route nonaktif; run yang belum dimulai (termasuk hari ini) dibatalkan. Riwayat run tetap tersimpan.
func (s *Service) DeactivateRoute(ctx context.Context, id uuid.UUID) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getRouteTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := routePermAt(ctx, tx, "delete", r.PropertyID, stopLocations(r), true); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE cleaning_routes SET is_active = false, updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
			return err
		}
		if err := cancelFutureRunsTx(ctx, tx, id, true); err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "cleaning_route", EntityID: &id, EntityLabel: r.RouteCode + " " + r.Name})
	})
}

const routeSelect = `SELECT r.id, r.property_id, r.route_code, r.name, r.description, r.cleaning_type, r.shift_id, sd.name, r.start_time, r.weekdays, r.responsible_team_id, t.name,
	r.default_assignee_user_id, u.full_name, r.checklist_template_id, r.requires_photo, r.priority, r.is_active, r.version
	FROM cleaning_routes r LEFT JOIN shift_definitions sd ON sd.id = r.shift_id LEFT JOIN teams t ON t.id = r.responsible_team_id LEFT JOIN users u ON u.id = r.default_assignee_user_id`

func (s *Service) getRouteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*CleaningRoute, error) {
	var r CleaningRoute
	var st time.Time
	if err := tx.QueryRow(ctx, routeSelect+` WHERE r.id = $1`, id).Scan(&r.ID, &r.PropertyID, &r.RouteCode, &r.Name, &r.Description, &r.CleaningType, &r.ShiftID, &r.ShiftName, &st, &r.Weekdays,
		&r.ResponsibleTeamID, &r.ResponsibleTeamName, &r.DefaultAssigneeUserID, &r.DefaultAssigneeName, &r.ChecklistTemplateID, &r.RequiresPhoto, &r.Priority, &r.IsActive, &r.Version); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Cleaning Route")
		}
		return nil, err
	}
	r.StartTime = st.Format("15:04")
	rows, err := tx.Query(ctx, `SELECT s.id, s.location_id, l.name, s.sort_order, s.estimated_minutes, s.checklist_template_id, s.notes FROM cleaning_route_stops s JOIN locations l ON l.id = s.location_id WHERE s.route_id = $1 ORDER BY s.sort_order`, id)
	if err != nil {
		return nil, err
	}
	r.Stops = []RouteStop{}
	for rows.Next() {
		var x RouteStop
		if err := rows.Scan(&x.ID, &x.LocationID, &x.LocationName, &x.SortOrder, &x.EstimatedMinutes, &x.ChecklistTemplateID, &x.Notes); err != nil {
			rows.Close()
			return nil, err
		}
		x.OffsetMinutes = r.TotalMinutes
		r.TotalMinutes += x.EstimatedMinutes
		r.Stops = append(r.Stops, x)
	}
	rows.Close()
	for i := range r.Stops {
		r.Stops[i].LocationPath = property.LocationPathText(ctx, tx, r.Stops[i].LocationID)
	}
	return &r, nil
}

func (s *Service) GetRoute(ctx context.Context, id uuid.UUID) (*CleaningRoute, error) {
	var out *CleaningRoute
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getRouteTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := routePermAt(ctx, tx, "view", r.PropertyID, stopLocations(r), false); err != nil {
			return err
		}
		r.TodayRun = s.todayRunTx(ctx, tx, r)
		out = r
		return nil
	})
	return out, err
}

func (s *Service) ListRoutes(ctx context.Context, propertyID *uuid.UUID) ([]CleaningRoute, error) {
	p := authctx.Must(ctx)
	out := []CleaningRoute{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE 1=1"
		if propertyID != nil {
			if !p.HasAnyOnProperty("housekeeping.cleaning_routes.view", *propertyID) {
				return apperr.Forbidden("")
			}
			where += " AND r.property_id = " + add(*propertyID)
		}
		where += " AND " + routeScopeSQL(p, "r", add)
		rows, err := tx.Query(ctx, `SELECT r.id FROM cleaning_routes r`+where+` ORDER BY r.is_active DESC, r.start_time, r.name`, args...)
		if err != nil {
			return err
		}
		var idList []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			idList = append(idList, id)
		}
		rows.Close()
		for _, id := range idList {
			r, err := s.getRouteTx(ctx, tx, id)
			if err != nil {
				return err
			}
			r.TodayRun = s.todayRunTx(ctx, tx, r)
			out = append(out, *r)
		}
		return nil
	})
	return out, err
}

// todayRunTx: run hari ini (zona waktu property) beserta progres, nil bila tidak ada.
func (s *Service) todayRunTx(ctx context.Context, tx pgx.Tx, r *CleaningRoute) *RouteRun {
	loc := property.PropertyTimezone(ctx, tx, r.PropertyID)
	var runID uuid.UUID
	if tx.QueryRow(ctx, `SELECT id FROM cleaning_route_runs WHERE route_id = $1 AND run_date = $2::date`, r.ID, time.Now().In(loc).Format("2006-01-02")).Scan(&runID) != nil {
		return nil
	}
	run, err := s.getRunTx(ctx, tx, runID)
	if err != nil {
		return nil
	}
	return run
}

// ---------- Route run generation ----------

// generateRunsForRouteTx: horizon HorizonDays; satu run per hari aktif (idempotent: UNIQUE route_id, run_date).
func (s *Service) generateRunsForRouteTx(ctx context.Context, tx pgx.Tx, routeID uuid.UUID) (int, error) {
	r, err := s.getRouteTx(ctx, tx, routeID)
	if err != nil {
		return 0, err
	}
	if !r.IsActive || len(r.Stops) == 0 {
		return 0, nil
	}
	orgID := authctx.Must(ctx).OrganizationID
	loc := property.PropertyTimezone(ctx, tx, r.PropertyID)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	st, _ := time.Parse("15:04", r.StartTime)
	n := 0
	for d := 0; d < s.HorizonDays; d++ {
		day := today.AddDate(0, 0, d)
		if !containsInt(r.Weekdays, int(day.Weekday())) {
			continue
		}
		start := time.Date(day.Year(), day.Month(), day.Day(), st.Hour(), st.Minute(), 0, 0, loc)
		if d == 0 && start.Add(time.Duration(r.TotalMinutes)*time.Minute).Before(now) {
			continue // seluruh route hari ini sudah lewat
		}
		ds := day.Format("2006-01-02")
		var runID uuid.UUID
		err := tx.QueryRow(ctx, `INSERT INTO cleaning_route_runs (organization_id, property_id, route_id, run_date, assignee_user_id, team_id, total_stops, created_by)
			VALUES ($1,$2,$3,$4::date,$5,$6,$7,$8) ON CONFLICT (route_id, run_date) DO NOTHING RETURNING id`,
			orgID, r.PropertyID, r.ID, ds, r.DefaultAssigneeUserID, r.ResponsibleTeamID, len(r.Stops), actorOrNil(authctx.Must(ctx))).Scan(&runID)
		if err != nil {
			if db.IsNoRows(err) {
				continue // sudah ada
			}
			return n, err
		}
		for i, stop := range r.Stops {
			startAt := start.Add(time.Duration(stop.OffsetMinutes) * time.Minute)
			dueAt := startAt.Add(time.Duration(stop.EstimatedMinutes) * time.Minute)
			tpl := stop.ChecklistTemplateID
			if tpl == nil {
				tpl = r.ChecklistTemplateID
			}
			// NC §17: "Routine Cleaning — Route Lantai 12 · 2/5 Toilet Pria"
			title := fmt.Sprintf("%s — %s · %d/%d %s", cleaningLabel(r.CleaningType), r.Name, i+1, len(r.Stops), stop.LocationName)
			locID := stop.LocationID
			taskID, err := s.Ops.CreateTaskTx(ctx, tx, operations.CreateTaskInput{
				PropertyID: &r.PropertyID, TaskType: "cleaning", Title: title, LocationID: &locID, Priority: r.Priority,
				ScheduledStartAt: &startAt, DueAt: &dueAt, ChecklistTemplateID: tpl, RequiresPhoto: r.RequiresPhoto,
				AssigneeUserID: r.DefaultAssigneeUserID, AssigneeTeamID: r.ResponsibleTeamID, SourceType: strPtr(ObjCleaningRouteRun), SourceID: &runID,
			})
			if err != nil {
				return n, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO cleaning_tasks (task_id, organization_id, schedule_date, cleaning_type, route_run_id, route_stop_order) VALUES ($1,$2,$3::date,$4,$5,$6)
				ON CONFLICT (task_id) DO UPDATE SET schedule_date = EXCLUDED.schedule_date, cleaning_type = EXCLUDED.cleaning_type, route_run_id = EXCLUDED.route_run_id, route_stop_order = EXCLUDED.route_stop_order`,
				taskID, orgID, ds, r.CleaningType, runID, stop.SortOrder); err != nil {
				return n, err
			}
		}
		n++
	}
	return n, nil
}

// GenerateRouteRuns: job berkala (bersama generator cleaning schedule) atau manual per route.
func (s *Service) GenerateRouteRuns(ctx context.Context, orgID uuid.UUID, routeID *uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	total := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var idList []uuid.UUID
		if routeID != nil {
			idList = []uuid.UUID{*routeID}
		} else {
			rows, err := tx.Query(ctx, `SELECT id FROM cleaning_routes WHERE is_active`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id uuid.UUID
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				idList = append(idList, id)
			}
			rows.Close()
		}
		for _, id := range idList {
			n, err := s.generateRunsForRouteTx(ctx, tx, id)
			if err != nil {
				return err
			}
			total += n
		}
		return nil
	})
	return total, err
}

// GenerateRouteRunsFor: generate manual satu route oleh user (permission update).
func (s *Service) GenerateRouteRunsFor(ctx context.Context, routeID uuid.UUID) (int, error) {
	n := 0
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getRouteTx(ctx, tx, routeID)
		if err != nil {
			return err
		}
		if err := routePermAt(ctx, tx, "update", r.PropertyID, stopLocations(r), true); err != nil {
			return err
		}
		n, err = s.generateRunsForRouteTx(ctx, tx, routeID)
		return err
	})
	return n, err
}

// ---------- Route run (progres x dari y area) ----------

const runSelect = `SELECT rr.id, rr.property_id, rr.route_id, r.route_code, r.name, r.cleaning_type, sd.name, rr.run_date::text, rr.assignee_user_id, u.full_name, rr.team_id, t.name,
	rr.status, rr.total_stops, rr.completed_stops, rr.started_at, rr.completed_at
	FROM cleaning_route_runs rr JOIN cleaning_routes r ON r.id = rr.route_id LEFT JOIN shift_definitions sd ON sd.id = r.shift_id
	LEFT JOIN users u ON u.id = rr.assignee_user_id LEFT JOIN teams t ON t.id = rr.team_id`

func (s *Service) getRunTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*RouteRun, error) {
	var x RouteRun
	if err := tx.QueryRow(ctx, runSelect+` WHERE rr.id = $1`, id).Scan(&x.ID, &x.PropertyID, &x.RouteID, &x.RouteCode, &x.RouteName, &x.CleaningType, &x.ShiftName, &x.RunDate, &x.AssigneeUserID, &x.AssigneeName,
		&x.TeamID, &x.TeamName, &x.Status, &x.TotalStops, &x.CompletedStops, &x.StartedAt, &x.CompletedAt); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Cleaning Route Run")
		}
		return nil, err
	}
	if x.TotalStops > 0 {
		x.ProgressPct = x.CompletedStops * 100 / x.TotalStops
	}
	rows, err := tx.Query(ctx, `SELECT t.id, t.task_number, t.title, t.status, ct.route_stop_order, t.location_id, l.name, t.scheduled_start_at, t.due_at, t.completed_at
		FROM cleaning_tasks ct JOIN tasks t ON t.id = ct.task_id LEFT JOIN locations l ON l.id = t.location_id WHERE ct.route_run_id = $1 ORDER BY ct.route_stop_order`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	x.Stops = []RouteRunStop{}
	for rows.Next() {
		var st RouteRunStop
		if err := rows.Scan(&st.TaskID, &st.TaskNumber, &st.Title, &st.Status, &st.SortOrder, &st.LocationID, &st.LocationName, &st.ScheduledStartAt, &st.DueAt, &st.CompletedAt); err != nil {
			return nil, err
		}
		x.Stops = append(x.Stops, st)
	}
	for i := range x.Stops {
		switch x.Stops[i].Status {
		case "completed", "closed", "cancelled":
		default:
			st := x.Stops[i]
			x.NextStop = &st
		}
		if x.NextStop != nil {
			break
		}
	}
	return &x, rows.Err()
}

type RunFilter struct {
	PropertyID *uuid.UUID
	RouteID    *uuid.UUID
	Date       string // YYYY-MM-DD; kosong = hari ini (zona waktu property)
	Mine       bool   // assignee saya, atau run tanpa assignee milik team saya
	Statuses   []string
}

func (s *Service) ListRuns(ctx context.Context, f RunFilter) ([]RouteRun, error) {
	p := authctx.Must(ctx)
	if f.Date != "" {
		if _, err := time.Parse("2006-01-02", f.Date); err != nil {
			return nil, apperr.Validation("date harus YYYY-MM-DD").WithField("date", "format tanggal")
		}
	}
	out := []RouteRun{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE 1=1"
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("housekeeping.cleaning_routes.view", *f.PropertyID) {
				return apperr.Forbidden("")
			}
			where += " AND rr.property_id = " + add(*f.PropertyID)
		}
		if !f.Mine {
			where += " AND " + routeScopeSQL(p, "r", add)
		}
		if f.RouteID != nil {
			where += " AND rr.route_id = " + add(*f.RouteID)
		}
		if f.Date != "" {
			where += " AND rr.run_date = " + add(f.Date) + "::date"
		} else {
			where += " AND rr.run_date = (now() AT TIME ZONE (SELECT pr.timezone FROM properties pr WHERE pr.location_id = rr.property_id))::date"
		}
		if f.Mine {
			where += " AND (rr.assignee_user_id = " + add(p.UserID) + " OR (rr.assignee_user_id IS NULL AND rr.team_id IN (SELECT tm.team_id FROM team_members tm WHERE tm.user_id = " + add(p.UserID) + ")))"
		}
		if len(f.Statuses) > 0 {
			where += " AND rr.status = ANY(" + add(f.Statuses) + ")"
		}
		rows, err := tx.Query(ctx, `SELECT rr.id FROM cleaning_route_runs rr JOIN cleaning_routes r ON r.id = rr.route_id`+where+` ORDER BY rr.run_date, r.start_time, r.name LIMIT 200`, args...)
		if err != nil {
			return err
		}
		var idList []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			idList = append(idList, id)
		}
		rows.Close()
		for _, id := range idList {
			x, err := s.getRunTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *x)
		}
		return nil
	})
	return out, err
}

func (s *Service) GetRun(ctx context.Context, id uuid.UUID) (*RouteRun, error) {
	var out *RouteRun
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getRunTx(ctx, tx, id)
		if err != nil {
			return err
		}
		p := authctx.Must(ctx)
		mine := x.AssigneeUserID != nil && *x.AssigneeUserID == p.UserID || (x.AssigneeUserID == nil && x.TeamID != nil && p.IsMemberOfTeam(*x.TeamID))
		if !mine {
			locs := []uuid.UUID{}
			for _, st := range x.Stops {
				if st.LocationID != nil {
					locs = append(locs, *st.LocationID)
				}
			}
			if err := routePermAt(ctx, tx, "view", x.PropertyID, locs, false); err != nil {
				return err
			}
		}
		out = x
		return nil
	})
	return out, err
}

// refreshRunProgressTx: dipanggil cleaningHook setiap transisi cleaning task yang menjadi bagian route run.
func refreshRunProgressTx(ctx context.Context, tx pgx.Tx, taskID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE cleaning_route_runs rr SET
		completed_stops = sub.done,
		status = CASE WHEN sub.cancelled >= rr.total_stops THEN 'cancelled'
		              WHEN sub.done + sub.cancelled >= rr.total_stops THEN 'completed'
		              WHEN sub.started > 0 OR sub.done > 0 THEN 'in_progress'
		              ELSE 'scheduled' END,
		started_at = CASE WHEN sub.started > 0 OR sub.done > 0 THEN COALESCE(rr.started_at, now()) ELSE rr.started_at END,
		completed_at = CASE WHEN sub.done + sub.cancelled >= rr.total_stops AND sub.cancelled < rr.total_stops THEN COALESCE(rr.completed_at, now()) ELSE NULL END
		FROM (SELECT ct.route_run_id,
		             count(*) FILTER (WHERE t.status IN ('completed','closed')) AS done,
		             count(*) FILTER (WHERE t.status = 'cancelled') AS cancelled,
		             count(*) FILTER (WHERE t.status NOT IN ('new','scheduled','assigned','cancelled','completed','closed')) AS started
		      FROM cleaning_tasks ct JOIN tasks t ON t.id = ct.task_id
		      WHERE ct.route_run_id = (SELECT route_run_id FROM cleaning_tasks WHERE task_id = $1)
		      GROUP BY ct.route_run_id) sub
		WHERE rr.id = sub.route_run_id`, taskID)
	return err
}
