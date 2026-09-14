// Package security: Patrol Route → Checkpoint → Patrol Schedule → Patrol Task → Checkpoint Scan → Finding → Incident/WO
// (PRD §13, AT-005, AT-006, WF-002; Naming Convention §14). Patrol Task = tasks (task_type=patrol) + patrol_tasks (extension).
package security

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/property"
)

type Service struct {
	DB   *db.DB
	Jobs jobs.Enqueuer
	Ops  *operations.Service
	// MissedPolicy (TD-007): "auto" = checkpoint pending otomatis ditandai missed saat complete; "manual" = officer wajib menandai.
	MissedPolicy string
	HorizonDays  int
}

func New(d *db.DB, j jobs.Enqueuer, ops *operations.Service) *Service {
	s := &Service{DB: d, Jobs: j, Ops: ops, MissedPolicy: "auto", HorizonDays: 7}
	ops.RegisterHook("task:patrol", patrolHook{s: s})
	return s
}

// ---------- Patrol Route & Checkpoint ----------

type Checkpoint struct {
	ID                  uuid.UUID  `json:"id"`
	PropertyID          uuid.UUID  `json:"property_id"`
	Name                string     `json:"name"`
	LocationID          uuid.UUID  `json:"location_id"`
	LocationPath        string     `json:"location_path"`
	QRCode              *string    `json:"qr_code"`
	Instructions        *string    `json:"instructions"`
	ChecklistTemplateID *uuid.UUID `json:"checklist_template_id"`
	IsActive            bool       `json:"is_active"`
	SortOrder           *int       `json:"sort_order,omitempty"` // dalam konteks route
	Version             int        `json:"version"`
}

type CheckpointInput struct {
	Name                *string    `json:"name"`
	LocationID          *uuid.UUID `json:"location_id"`
	Instructions        *string    `json:"instructions"`
	ChecklistTemplateID *uuid.UUID `json:"checklist_template_id"`
	IsActive            *bool      `json:"is_active"`
}

func (s *Service) CreateCheckpoint(ctx context.Context, in CheckpointInput) (*Checkpoint, error) {
	var out *Checkpoint
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.CreateCheckpointTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = s.getCheckpointTx(ctx, tx, id)
		return err
	})
	return out, err
}

// CreateCheckpointTx: di dalam transaksi (seed / import).
func (s *Service) CreateCheckpointTx(ctx context.Context, tx pgx.Tx, in CheckpointInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" || in.LocationID == nil {
		return uuid.Nil, apperr.Validation("name dan location_id wajib")
	}
	pid, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
	if err != nil {
		return uuid.Nil, err
	}
	if !p.HasOnProperty("security.checkpoints.create", pid) {
		return uuid.Nil, apperr.Forbidden("")
	}
	qr, err := ids.NewQRCode()
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO checkpoints (organization_id, property_id, name, location_id, qr_code, instructions, checklist_template_id, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING id`,
		p.OrganizationID, pid, strings.TrimSpace(*in.Name), *in.LocationID, qr, in.Instructions, in.ChecklistTemplateID, actorOrNil(p)).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO qr_codes (code, organization_id, object_type, object_id) VALUES ($1,$2,'checkpoint',$3)`, qr, p.OrganizationID, id); err != nil {
		return uuid.Nil, err
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "checkpoint", EntityID: &id, EntityLabel: *in.Name, After: in})
	return id, nil
}

func (s *Service) UpdateCheckpoint(ctx context.Context, id uuid.UUID, in CheckpointInput) (*Checkpoint, error) {
	p := authctx.Must(ctx)
	var out *Checkpoint
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getCheckpointTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.HasOnProperty("security.checkpoints.update", before.PropertyID) {
			return apperr.Forbidden("")
		}
		if in.LocationID != nil {
			pid, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
			if err != nil || pid != before.PropertyID {
				return apperr.Validation("location_id harus di property yang sama")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE checkpoints SET name = COALESCE(NULLIF($2,''), name), location_id = COALESCE($3, location_id), instructions = COALESCE($4, instructions), checklist_template_id = COALESCE($5, checklist_template_id), is_active = COALESCE($6, is_active), updated_by = $7 WHERE id = $1`,
			id, deref(in.Name), in.LocationID, in.Instructions, in.ChecklistTemplateID, in.IsActive, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "checkpoint", EntityID: &id, EntityLabel: before.Name, Before: before, After: in})
		out, err = s.getCheckpointTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) getCheckpointTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Checkpoint, error) {
	var c Checkpoint
	if err := tx.QueryRow(ctx, `SELECT id, property_id, name, location_id, qr_code, instructions, checklist_template_id, is_active, version FROM checkpoints WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&c.ID, &c.PropertyID, &c.Name, &c.LocationID, &c.QRCode, &c.Instructions, &c.ChecklistTemplateID, &c.IsActive, &c.Version); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Checkpoint")
		}
		return nil, err
	}
	c.LocationPath = property.LocationPathText(ctx, tx, c.LocationID)
	return &c, nil
}

func (s *Service) ListCheckpoints(ctx context.Context, propertyID *uuid.UUID, q string) ([]Checkpoint, error) {
	p := authctx.Must(ctx)
	var out []Checkpoint
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{q}
		where := " WHERE deleted_at IS NULL AND ($1 = '' OR name ILIKE '%' || $1 || '%')"
		if propertyID != nil {
			args = append(args, *propertyID)
			where += " AND property_id = $2"
		} else if pids, all := p.PropertyIDsFor("security.checkpoints.view"); !all {
			args = append(args, pids)
			where += " AND property_id = ANY($2::uuid[])"
		}
		rows, err := tx.Query(ctx, `SELECT id FROM checkpoints`+where+` ORDER BY name`, args...)
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
			c, err := s.getCheckpointTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *c)
		}
		return nil
	})
	if out == nil {
		out = []Checkpoint{}
	}
	return out, err
}

type PatrolRoute struct {
	ID                  uuid.UUID    `json:"id"`
	PropertyID          uuid.UUID    `json:"property_id"`
	Name                string       `json:"name"`
	Description         *string      `json:"description"`
	EstimatedMinutes    *int         `json:"estimated_minutes"`
	ChecklistTemplateID *uuid.UUID   `json:"checklist_template_id"`
	IsActive            bool         `json:"is_active"`
	Checkpoints         []Checkpoint `json:"checkpoints"` // urut sort_order (PRD §13.2 assign checkpoint order)
	Version             int          `json:"version"`
}

type RouteCheckpointInput struct {
	CheckpointID          uuid.UUID `json:"checkpoint_id"`
	SortOrder             int       `json:"sort_order"`
	ExpectedOffsetMinutes *int      `json:"expected_offset_minutes"`
}

type RouteInput struct {
	PropertyID          *uuid.UUID              `json:"property_id"`
	Name                *string                 `json:"name"`
	Description         *string                 `json:"description"`
	EstimatedMinutes    *int                    `json:"estimated_minutes"`
	ChecklistTemplateID *uuid.UUID              `json:"checklist_template_id"`
	IsActive            *bool                   `json:"is_active"`
	Checkpoints         *[]RouteCheckpointInput `json:"checkpoints"`
}

func (s *Service) CreateRoute(ctx context.Context, in RouteInput) (*PatrolRoute, error) {
	var out *PatrolRoute
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.CreateRouteTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = s.getRouteTx(ctx, tx, id)
		return err
	})
	return out, err
}

// CreateRouteTx: di dalam transaksi (seed / import).
func (s *Service) CreateRouteTx(ctx context.Context, tx pgx.Tx, in RouteInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil || in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return uuid.Nil, apperr.Validation("property_id dan name wajib")
	}
	if !p.HasOnProperty("security.patrol_routes.create", *in.PropertyID) {
		return uuid.Nil, apperr.Forbidden("")
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO patrol_routes (organization_id, property_id, name, description, estimated_minutes, checklist_template_id, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$7) RETURNING id`,
		p.OrganizationID, *in.PropertyID, strings.TrimSpace(*in.Name), in.Description, in.EstimatedMinutes, in.ChecklistTemplateID, actorOrNil(p)).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if in.Checkpoints != nil {
		if err := s.setRouteCheckpoints(ctx, tx, id, *in.PropertyID, *in.Checkpoints); err != nil {
			return uuid.Nil, err
		}
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "patrol_route", EntityID: &id, EntityLabel: *in.Name, After: in})
	return id, nil
}

func (s *Service) setRouteCheckpoints(ctx context.Context, tx pgx.Tx, routeID, propertyID uuid.UUID, cps []RouteCheckpointInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM patrol_route_checkpoints WHERE route_id = $1`, routeID); err != nil {
		return err
	}
	for i, c := range cps {
		var pid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM checkpoints WHERE id = $1 AND deleted_at IS NULL`, c.CheckpointID).Scan(&pid); err != nil || pid != propertyID {
			return apperr.Validation("checkpoint tidak ditemukan di property ini: " + c.CheckpointID.String())
		}
		order := c.SortOrder
		if order == 0 {
			order = i + 1
		}
		if _, err := tx.Exec(ctx, `INSERT INTO patrol_route_checkpoints (route_id, checkpoint_id, sort_order, expected_offset_minutes) VALUES ($1,$2,$3,$4)`, routeID, c.CheckpointID, order, c.ExpectedOffsetMinutes); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Validation("sort_order/checkpoint duplikat")
			}
			return err
		}
	}
	return nil
}

func (s *Service) UpdateRoute(ctx context.Context, id uuid.UUID, in RouteInput) (*PatrolRoute, error) {
	p := authctx.Must(ctx)
	var out *PatrolRoute
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getRouteTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.HasOnProperty("security.patrol_routes.update", before.PropertyID) {
			return apperr.Forbidden("")
		}
		if _, err := tx.Exec(ctx, `UPDATE patrol_routes SET name = COALESCE(NULLIF($2,''), name), description = COALESCE($3, description), estimated_minutes = COALESCE($4, estimated_minutes), checklist_template_id = COALESCE($5, checklist_template_id), is_active = COALESCE($6, is_active), updated_by = $7 WHERE id = $1`,
			id, deref(in.Name), in.Description, in.EstimatedMinutes, in.ChecklistTemplateID, in.IsActive, p.UserID); err != nil {
			return err
		}
		if in.Checkpoints != nil {
			if err := s.setRouteCheckpoints(ctx, tx, id, before.PropertyID, *in.Checkpoints); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "patrol_route", EntityID: &id, EntityLabel: before.Name, Before: before, After: in})
		out, err = s.getRouteTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) getRouteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*PatrolRoute, error) {
	var r PatrolRoute
	if err := tx.QueryRow(ctx, `SELECT id, property_id, name, description, estimated_minutes, checklist_template_id, is_active, version FROM patrol_routes WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&r.ID, &r.PropertyID, &r.Name, &r.Description, &r.EstimatedMinutes, &r.ChecklistTemplateID, &r.IsActive, &r.Version); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Patrol Route")
		}
		return nil, err
	}
	r.Checkpoints = []Checkpoint{}
	rows, err := tx.Query(ctx, `SELECT rc.checkpoint_id, rc.sort_order FROM patrol_route_checkpoints rc WHERE rc.route_id = $1 ORDER BY rc.sort_order`, id)
	if err != nil {
		return nil, err
	}
	type rc struct {
		id    uuid.UUID
		order int
	}
	var list []rc
	for rows.Next() {
		var x rc
		if err := rows.Scan(&x.id, &x.order); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, x)
	}
	rows.Close()
	for _, x := range list {
		c, err := s.getCheckpointTx(ctx, tx, x.id)
		if err != nil {
			continue
		}
		o := x.order
		c.SortOrder = &o
		r.Checkpoints = append(r.Checkpoints, *c)
	}
	return &r, nil
}

func (s *Service) GetRoute(ctx context.Context, id uuid.UUID) (*PatrolRoute, error) {
	var out *PatrolRoute
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getRouteTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasOnProperty("security.patrol_routes.view", r.PropertyID) {
			return apperr.Forbidden("")
		}
		out = r
		return nil
	})
	return out, err
}

func (s *Service) ListRoutes(ctx context.Context, propertyID *uuid.UUID) ([]PatrolRoute, error) {
	p := authctx.Must(ctx)
	var out []PatrolRoute
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		where := " WHERE deleted_at IS NULL"
		if propertyID != nil {
			args = append(args, *propertyID)
			where += " AND property_id = $1"
		} else if pids, all := p.PropertyIDsFor("security.patrol_routes.view"); !all {
			args = append(args, pids)
			where += " AND property_id = ANY($1::uuid[])"
		}
		rows, err := tx.Query(ctx, `SELECT id FROM patrol_routes`+where+` ORDER BY name`, args...)
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
			out = append(out, *r)
		}
		return nil
	})
	if out == nil {
		out = []PatrolRoute{}
	}
	return out, err
}

// ---------- Patrol Schedule ----------

type PatrolSchedule struct {
	ID                    uuid.UUID  `json:"id"`
	PropertyID            uuid.UUID  `json:"property_id"`
	RouteID               uuid.UUID  `json:"route_id"`
	RouteName             string     `json:"route_name"`
	Name                  string     `json:"name"`
	StartTime             string     `json:"start_time"` // HH:MM
	DurationMinutes       int        `json:"duration_minutes"`
	Weekdays              []int      `json:"weekdays"`
	ResponsibleTeamID     *uuid.UUID `json:"responsible_team_id"`
	DefaultAssigneeUserID *uuid.UUID `json:"default_assignee_user_id"`
	Priority              string     `json:"priority"`
	IsActive              bool       `json:"is_active"`
	ValidFrom             *string    `json:"valid_from"`
	ValidUntil            *string    `json:"valid_until"`
	Version               int        `json:"version"`
}

type ScheduleInput struct {
	RouteID               *uuid.UUID `json:"route_id"`
	Name                  *string    `json:"name"`
	StartTime             *string    `json:"start_time"`
	DurationMinutes       *int       `json:"duration_minutes"`
	Weekdays              *[]int     `json:"weekdays"`
	ResponsibleTeamID     *uuid.UUID `json:"responsible_team_id"`
	DefaultAssigneeUserID *uuid.UUID `json:"default_assignee_user_id"`
	Priority              *string    `json:"priority"`
	IsActive              *bool      `json:"is_active"`
	ValidFrom             *string    `json:"valid_from"`
	ValidUntil            *string    `json:"valid_until"`
}

func parseHHMM(s string) (time.Time, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return time.Time{}, apperr.Validation("start_time harus HH:MM")
	}
	return t, nil
}

func (s *Service) CreateSchedule(ctx context.Context, in ScheduleInput) (*PatrolSchedule, error) {
	var out *PatrolSchedule
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.CreateScheduleTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = s.getScheduleTx(ctx, tx, id)
		return err
	})
	return out, err
}

// CreateScheduleTx: di dalam transaksi (seed / import); langsung generate patrol task horizon.
func (s *Service) CreateScheduleTx(ctx context.Context, tx pgx.Tx, in ScheduleInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if in.RouteID == nil || in.Name == nil || in.StartTime == nil {
		return uuid.Nil, apperr.Validation("route_id, name, start_time wajib")
	}
	st, err := parseHHMM(*in.StartTime)
	if err != nil {
		return uuid.Nil, err
	}
	dur := 60
	if in.DurationMinutes != nil && *in.DurationMinutes > 0 {
		dur = *in.DurationMinutes
	}
	wd := []int{0, 1, 2, 3, 4, 5, 6}
	if in.Weekdays != nil && len(*in.Weekdays) > 0 {
		wd = *in.Weekdays
	}
	prio := "medium"
	if in.Priority != nil {
		prio = *in.Priority
	}
	if !operations.Priorities[prio] {
		return uuid.Nil, apperr.Validation("priority tidak valid")
	}
	route, err := s.getRouteTx(ctx, tx, *in.RouteID)
	if err != nil {
		return uuid.Nil, err
	}
	if !p.HasOnProperty("security.patrol.manage", route.PropertyID) {
		return uuid.Nil, apperr.Forbidden("")
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO patrol_schedules (organization_id, property_id, route_id, name, start_time, duration_minutes, weekdays, responsible_team_id, default_assignee_user_id, priority, valid_from, valid_until, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::date,$12::date,$13,$13) RETURNING id`,
		p.OrganizationID, route.PropertyID, *in.RouteID, *in.Name, st.Format("15:04:05"), dur, wd, in.ResponsibleTeamID, in.DefaultAssigneeUserID, prio, nilIfEmpty(in.ValidFrom), nilIfEmpty(in.ValidUntil), actorOrNil(p)).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "patrol_schedule", EntityID: &id, EntityLabel: *in.Name, After: in})
	if _, err := s.generateForScheduleTx(ctx, tx, id); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func nilIfEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

func (s *Service) UpdateSchedule(ctx context.Context, id uuid.UUID, in ScheduleInput) (*PatrolSchedule, error) {
	p := authctx.Must(ctx)
	var out *PatrolSchedule
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getScheduleTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.HasOnProperty("security.patrol.manage", before.PropertyID) {
			return apperr.Forbidden("")
		}
		var startStr *string
		if in.StartTime != nil {
			st, err := parseHHMM(*in.StartTime)
			if err != nil {
				return err
			}
			v := st.Format("15:04:05")
			startStr = &v
		}
		if _, err := tx.Exec(ctx, `UPDATE patrol_schedules SET route_id = COALESCE($2, route_id), name = COALESCE(NULLIF($3,''), name), start_time = COALESCE($4::time, start_time), duration_minutes = COALESCE($5, duration_minutes),
			weekdays = COALESCE($6, weekdays), responsible_team_id = COALESCE($7, responsible_team_id), default_assignee_user_id = COALESCE($8, default_assignee_user_id), priority = COALESCE(NULLIF($9,''), priority),
			is_active = COALESCE($10, is_active), valid_from = COALESCE($11::date, valid_from), valid_until = COALESCE($12::date, valid_until), updated_by = $13 WHERE id = $1`,
			id, in.RouteID, deref(in.Name), startStr, in.DurationMinutes, in.Weekdays, in.ResponsibleTeamID, in.DefaultAssigneeUserID, deref(in.Priority), in.IsActive, nilIfEmpty(in.ValidFrom), nilIfEmpty(in.ValidUntil), p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "patrol_schedule", EntityID: &id, EntityLabel: before.Name, Before: before, After: in})
		// patrol task ke depan yang belum dimulai dibatalkan & digenerate ulang
		_, _ = tx.Exec(ctx, `UPDATE tasks t SET status = 'cancelled', cancelled_at = now() FROM patrol_tasks pt WHERE pt.task_id = t.id AND pt.patrol_schedule_id = $1 AND t.status IN ('new','scheduled','assigned') AND pt.schedule_date > CURRENT_DATE`, id)
		_, _ = tx.Exec(ctx, `DELETE FROM patrol_tasks WHERE patrol_schedule_id = $1 AND schedule_date > CURRENT_DATE AND task_id IN (SELECT id FROM tasks WHERE status = 'cancelled')`, id)
		if _, err := s.generateForScheduleTx(ctx, tx, id); err != nil {
			return err
		}
		out, err = s.getScheduleTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) getScheduleTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*PatrolSchedule, error) {
	var sc PatrolSchedule
	var st time.Time
	var vf, vu *time.Time
	if err := tx.QueryRow(ctx, `SELECT ps.id, ps.property_id, ps.route_id, r.name, ps.name, ps.start_time, ps.duration_minutes, ps.weekdays, ps.responsible_team_id, ps.default_assignee_user_id, ps.priority, ps.is_active, ps.valid_from, ps.valid_until, ps.version
		FROM patrol_schedules ps JOIN patrol_routes r ON r.id = ps.route_id WHERE ps.id = $1`, id).
		Scan(&sc.ID, &sc.PropertyID, &sc.RouteID, &sc.RouteName, &sc.Name, &st, &sc.DurationMinutes, &sc.Weekdays, &sc.ResponsibleTeamID, &sc.DefaultAssigneeUserID, &sc.Priority, &sc.IsActive, &vf, &vu, &sc.Version); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Patrol Schedule")
		}
		return nil, err
	}
	sc.StartTime = st.Format("15:04")
	if vf != nil {
		v := vf.Format("2006-01-02")
		sc.ValidFrom = &v
	}
	if vu != nil {
		v := vu.Format("2006-01-02")
		sc.ValidUntil = &v
	}
	return &sc, nil
}

func (s *Service) ListSchedules(ctx context.Context, propertyID *uuid.UUID) ([]PatrolSchedule, error) {
	p := authctx.Must(ctx)
	var out []PatrolSchedule
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		where := " WHERE 1=1"
		if propertyID != nil {
			args = append(args, *propertyID)
			where += " AND property_id = $1"
		} else if pids, all := p.PropertyIDsFor("security.patrol.view"); !all {
			args = append(args, pids)
			where += " AND property_id = ANY($1::uuid[])"
		}
		rows, err := tx.Query(ctx, `SELECT id FROM patrol_schedules`+where+` ORDER BY start_time, name`, args...)
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
			sc, err := s.getScheduleTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *sc)
		}
		return nil
	})
	if out == nil {
		out = []PatrolSchedule{}
	}
	return out, err
}

// ---------- Patrol Task generation (horizon 7 hari; idempotent per (schedule, date)) ----------

func (s *Service) generateForScheduleTx(ctx context.Context, tx pgx.Tx, scheduleID uuid.UUID) (int, error) {
	sc, err := s.getScheduleTx(ctx, tx, scheduleID)
	if err != nil {
		return 0, err
	}
	if !sc.IsActive {
		return 0, nil
	}
	route, err := s.getRouteTx(ctx, tx, sc.RouteID)
	if err != nil {
		return 0, err
	}
	loc := property.PropertyTimezone(ctx, tx, sc.PropertyID)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	st, _ := time.Parse("15:04", sc.StartTime)
	n := 0
	for d := 0; d < s.HorizonDays; d++ {
		day := today.AddDate(0, 0, d)
		if !containsInt(sc.Weekdays, int(day.Weekday())) {
			continue
		}
		if sc.ValidFrom != nil && day.Format("2006-01-02") < *sc.ValidFrom {
			continue
		}
		if sc.ValidUntil != nil && day.Format("2006-01-02") > *sc.ValidUntil {
			continue
		}
		startAt := time.Date(day.Year(), day.Month(), day.Day(), st.Hour(), st.Minute(), 0, 0, loc)
		dueAt := startAt.Add(time.Duration(sc.DurationMinutes) * time.Minute)
		if d == 0 && dueAt.Before(now) {
			continue // slot hari ini sudah lewat
		}
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patrol_tasks WHERE patrol_schedule_id = $1 AND schedule_date = $2)`, scheduleID, day.Format("2006-01-02")).Scan(&exists)
		if exists {
			continue
		}
		title := fmt.Sprintf("Patrol %s — %s", sc.RouteName, startAt.Format("02 Jan 15:04"))
		var locID *uuid.UUID
		if len(route.Checkpoints) > 0 {
			locID = &route.Checkpoints[0].LocationID
		}
		taskID, err := s.Ops.CreateTaskTx(ctx, tx, operations.CreateTaskInput{
			PropertyID: &sc.PropertyID, TaskType: "patrol", Title: title, LocationID: locID, Priority: sc.Priority,
			ScheduledStartAt: &startAt, DueAt: &dueAt, ChecklistTemplateID: route.ChecklistTemplateID,
			AssigneeUserID: sc.DefaultAssigneeUserID, AssigneeTeamID: sc.ResponsibleTeamID, SourceType: strPtr("patrol_schedule"), SourceID: &scheduleID,
		})
		if err != nil {
			return n, err
		}
		if err := s.createPatrolExtensionTx(ctx, tx, taskID, route, &scheduleID, day.Format("2006-01-02")); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Service) createPatrolExtensionTx(ctx context.Context, tx pgx.Tx, taskID uuid.UUID, route *PatrolRoute, scheduleID *uuid.UUID, date string) error {
	p := authctx.Must(ctx)
	var dateArg any
	if date != "" {
		dateArg = date
	}
	if _, err := tx.Exec(ctx, `INSERT INTO patrol_tasks (task_id, organization_id, route_id, patrol_schedule_id, schedule_date, total_checkpoints) VALUES ($1,$2,$3,$4,$5::date,$6)`,
		taskID, p.OrganizationID, route.ID, scheduleID, dateArg, len(route.Checkpoints)); err != nil {
		return err
	}
	for _, c := range route.Checkpoints {
		order := 0
		if c.SortOrder != nil {
			order = *c.SortOrder
		}
		if _, err := tx.Exec(ctx, `INSERT INTO checkpoint_scans (organization_id, patrol_task_id, checkpoint_id, sort_order, status) VALUES ($1,$2,$3,$4,'pending')`, p.OrganizationID, taskID, c.ID, order); err != nil {
			return err
		}
	}
	return nil
}

// GeneratePatrolTasks: job periodic per org (atau satu schedule).
func (s *Service) GeneratePatrolTasks(ctx context.Context, orgID uuid.UUID, scheduleID *uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	total := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var idList []uuid.UUID
		if scheduleID != nil {
			idList = []uuid.UUID{*scheduleID}
		} else {
			rows, err := tx.Query(ctx, `SELECT id FROM patrol_schedules WHERE is_active`)
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
			n, err := s.generateForScheduleTx(ctx, tx, id)
			if err != nil {
				return err
			}
			total += n
		}
		return nil
	})
	return total, err
}

// CreateAdhocPatrol: patrol task manual dari route (tanpa schedule).
func (s *Service) CreateAdhocPatrol(ctx context.Context, routeID uuid.UUID, in operations.CreateTaskInput) (*operations.WorkItem, error) {
	var out *operations.WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		route, err := s.getRouteTx(ctx, tx, routeID)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasOnProperty("security.patrol.manage", route.PropertyID) {
			return apperr.Forbidden("")
		}
		in.TaskType = "patrol"
		in.PropertyID = &route.PropertyID
		if in.Title == "" {
			in.Title = "Patrol " + route.Name
		}
		if in.LocationID == nil && len(route.Checkpoints) > 0 {
			in.LocationID = &route.Checkpoints[0].LocationID
		}
		if in.ChecklistTemplateID == nil {
			in.ChecklistTemplateID = route.ChecklistTemplateID
		}
		taskID, err := s.Ops.CreateTaskTx(ctx, tx, in)
		if err != nil {
			return err
		}
		if err := s.createPatrolExtensionTx(ctx, tx, taskID, route, nil, ""); err != nil {
			return err
		}
		out, err = s.Ops.GetTx(ctx, tx, operations.ObjTask, taskID)
		return err
	})
	return out, err
}

// ---------- Checkpoint scan (AT-005) ----------

type Scan struct {
	ID                  uuid.UUID  `json:"id"`
	CheckpointID        uuid.UUID  `json:"checkpoint_id"`
	Name                string     `json:"checkpoint_name"`
	LocationPath        string     `json:"location_path"`
	QRCode              *string    `json:"qr_code"`
	SortOrder           int        `json:"sort_order"`
	Status              string     `json:"status"`
	ScannedAt           *time.Time `json:"scanned_at"`
	ScannedBy           *uuid.UUID `json:"scanned_by"`
	ScanMethod          *string    `json:"scan_method"`
	GPSStatus           *string    `json:"gps_status"`
	MissedReason        *string    `json:"missed_reason"`
	Note                *string    `json:"note"`
	Instructions        *string    `json:"instructions"`
	ChecklistTemplateID *uuid.UUID `json:"checklist_template_id"`
}

func (s *Service) ListScans(ctx context.Context, taskID uuid.UUID) ([]Scan, error) {
	var out []Scan
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.Ops.ObjectAccess(ctx, tx, operations.ObjTask, taskID, false); err != nil {
			return err
		}
		var err error
		out, err = s.listScansTx(ctx, tx, taskID)
		return err
	})
	if out == nil {
		out = []Scan{}
	}
	return out, err
}

func (s *Service) listScansTx(ctx context.Context, tx pgx.Tx, taskID uuid.UUID) ([]Scan, error) {
	rows, err := tx.Query(ctx, `SELECT cs.id, cs.checkpoint_id, c.name, c.location_id, c.qr_code, cs.sort_order, cs.status, cs.scanned_at, cs.scanned_by, cs.scan_method, cs.gps_status, cs.missed_reason, cs.note, c.instructions, c.checklist_template_id
		FROM checkpoint_scans cs JOIN checkpoints c ON c.id = cs.checkpoint_id WHERE cs.patrol_task_id = $1 ORDER BY cs.sort_order`, taskID)
	if err != nil {
		return nil, err
	}
	var out []Scan
	var locs []uuid.UUID
	for rows.Next() {
		var sc Scan
		var loc uuid.UUID
		if err := rows.Scan(&sc.ID, &sc.CheckpointID, &sc.Name, &loc, &sc.QRCode, &sc.SortOrder, &sc.Status, &sc.ScannedAt, &sc.ScannedBy, &sc.ScanMethod, &sc.GPSStatus, &sc.MissedReason, &sc.Note, &sc.Instructions, &sc.ChecklistTemplateID); err != nil {
			rows.Close()
			return nil, err
		}
		locs = append(locs, loc)
		out = append(out, sc)
	}
	rows.Close()
	for i := range out {
		out[i].LocationPath = property.LocationPathText(ctx, tx, locs[i])
	}
	return out, nil
}

type ScanInput struct {
	CheckpointID     *uuid.UUID `json:"checkpoint_id"`
	QRCode           *string    `json:"qr_code"`
	ScanMethod       string     `json:"scan_method"` // qr | manual
	GPSLat           *float64   `json:"gps_lat"`
	GPSLng           *float64   `json:"gps_lng"`
	GPSStatus        string     `json:"gps_status"`
	Note             *string    `json:"note"`
	ClientScanID     *string    `json:"client_scan_id"`
	ClientRecordedAt *time.Time `json:"client_recorded_at"`
	FromSync         bool       `json:"-"`
}

// ScanCheckpoint: validasi checkpoint milik patrol task, patrol in_progress, officer = assignee; idempotent via client_scan_id.
func (s *Service) ScanCheckpoint(ctx context.Context, taskID uuid.UUID, in ScanInput) ([]Scan, error) {
	var out []Scan
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.ScanCheckpointTx(ctx, tx, taskID, in); err != nil {
			return err
		}
		var err error
		out, err = s.listScansTx(ctx, tx, taskID)
		return err
	})
	return out, err
}

func (s *Service) ScanCheckpointTx(ctx context.Context, tx pgx.Tx, taskID uuid.UUID, in ScanInput) error {
	p := authctx.Must(ctx)
	item, err := s.Ops.GetTx(ctx, tx, operations.ObjTask, taskID)
	if err != nil {
		return err
	}
	if item.Type != "patrol" {
		return apperr.Validation("task bukan patrol")
	}
	isAssignee := (item.Assignee.UserID != nil && *item.Assignee.UserID == p.UserID) || (item.Assignee.TeamID != nil && p.IsMemberOfTeam(*item.Assignee.TeamID))
	if !isAssignee && !p.HasOnProperty("security.patrol.manage", item.PropertyID) {
		return apperr.Forbidden("Hanya officer yang ditugaskan yang dapat scan checkpoint")
	}
	if item.Status != workflow.InProgress {
		return apperr.Conflict("WORKFLOW_GUARD_FAILED", "Patrol belum dimulai (Start Patrol dulu)")
	}
	if in.ClientScanID != nil && *in.ClientScanID != "" {
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM checkpoint_scans WHERE client_scan_id = $1)`, *in.ClientScanID).Scan(&exists)
		if exists {
			return nil // duplicate → idempotent
		}
	}
	var cpID uuid.UUID
	if in.CheckpointID != nil {
		cpID = *in.CheckpointID
	} else if in.QRCode != nil && *in.QRCode != "" {
		code := strings.ToUpper(strings.TrimSpace(*in.QRCode))
		if i := strings.LastIndex(code, "/"); i >= 0 {
			code = code[i+1:]
		}
		if err := tx.QueryRow(ctx, `SELECT object_id FROM qr_codes WHERE code = $1 AND object_type = 'checkpoint' AND revoked_at IS NULL`, code).Scan(&cpID); err != nil {
			return apperr.NotFound("QR checkpoint")
		}
	} else {
		return apperr.Validation("checkpoint_id atau qr_code wajib")
	}
	if in.ScanMethod == "" {
		in.ScanMethod = "qr"
		if in.CheckpointID != nil && in.QRCode == nil {
			in.ScanMethod = "manual"
		}
	}
	if in.GPSStatus == "" {
		in.GPSStatus = "unavailable"
	}
	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `UPDATE checkpoint_scans SET status = 'scanned', scanned_at = $3, scanned_by = $4, scan_method = $5, gps_lat = $6, gps_lng = $7, gps_status = $8, note = COALESCE($9, note), client_scan_id = $10
		WHERE patrol_task_id = $1 AND checkpoint_id = $2`, taskID, cpID, now, p.UserID, in.ScanMethod, in.GPSLat, in.GPSLng, in.GPSStatus, in.Note, in.ClientScanID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.Validation("checkpoint tidak termasuk dalam rute patrol ini")
	}
	if _, err := tx.Exec(ctx, `UPDATE patrol_tasks SET scanned_checkpoints = (SELECT count(*) FROM checkpoint_scans WHERE patrol_task_id = $1 AND status = 'scanned') WHERE task_id = $1`, taskID); err != nil {
		return err
	}
	var cpName string
	_ = tx.QueryRow(ctx, `SELECT name FROM checkpoints WHERE id = $1`, cpID).Scan(&cpName)
	ctxAct := ctx
	if in.FromSync {
		ctxAct = authctx.With(ctx, sourceSync(p))
	}
	_ = audit.Record(ctxAct, tx, audit.Entry{ObjectType: operations.ObjTask, ObjectID: taskID, Action: audit.ActCheckpointScanned, Payload: map[string]any{"checkpoint_id": cpID, "checkpoint_name": cpName, "method": in.ScanMethod, "gps_status": in.GPSStatus}, ClientRecordedAt: in.ClientRecordedAt})
	return nil
}

func sourceSync(p *authctx.Principal) *authctx.Principal {
	cl := *p
	cl.Source = authctx.SourceSync
	return &cl
}

// MarkMissed: officer/supervisor menandai checkpoint missed dengan alasan (TD-007 manual).
func (s *Service) MarkMissed(ctx context.Context, taskID, checkpointID uuid.UUID, reason string) ([]Scan, error) {
	p := authctx.Must(ctx)
	if strings.TrimSpace(reason) == "" {
		return nil, apperr.Validation("reason wajib")
	}
	var out []Scan
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.Ops.ObjectAccess(ctx, tx, operations.ObjTask, taskID, true); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE checkpoint_scans SET status = 'missed', missed_reason = $3, scanned_by = $4, scanned_at = now() WHERE patrol_task_id = $1 AND checkpoint_id = $2 AND status = 'pending'`, taskID, checkpointID, reason, p.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.Conflict("CHECKPOINT_NOT_PENDING", "Checkpoint tidak dalam status pending")
		}
		_, _ = tx.Exec(ctx, `UPDATE patrol_tasks SET missed_checkpoints = (SELECT count(*) FROM checkpoint_scans WHERE patrol_task_id = $1 AND status = 'missed') WHERE task_id = $1`, taskID)
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjTask, ObjectID: taskID, Action: audit.ActCheckpointMissed, Payload: map[string]any{"checkpoint_id": checkpointID, "reason": reason}})
		s.emitMissed(ctx, tx, taskID, checkpointID, reason)
		out, err = s.listScansTx(ctx, tx, taskID)
		return err
	})
	return out, err
}

func (s *Service) emitMissed(ctx context.Context, tx pgx.Tx, taskID, checkpointID uuid.UUID, reason string) {
	if s.Jobs == nil {
		return
	}
	p := authctx.Must(ctx)
	var number string
	var prop uuid.UUID
	_ = tx.QueryRow(ctx, `SELECT task_number, property_id FROM tasks WHERE id = $1`, taskID).Scan(&number, &prop)
	_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.PatrolCheckpointMissed, OrganizationID: p.OrganizationID, PropertyID: &prop, ObjectType: operations.ObjTask, ObjectID: taskID, ObjectLabel: number, ActorUserID: actorOrNil(p), Payload: map[string]any{"checkpoint_id": checkpointID, "reason": reason}})
}

func actorOrNil(p *authctx.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}

// ---------- Hook: complete patrol (PRD §13.2 Record checkpoint completion; TD-007) ----------

type patrolHook struct{ s *Service }

// BeforeComplete: semua checkpoint harus scanned atau missed-recorded. Policy auto: pending → missed otomatis.
func (h patrolHook) BeforeComplete(ctx context.Context, tx pgx.Tx, item *operations.WorkItem, in operations.TransitionInput) error {
	var pending int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM checkpoint_scans WHERE patrol_task_id = $1 AND status = 'pending'`, item.ID).Scan(&pending); err != nil {
		return err
	}
	if pending == 0 {
		return nil
	}
	if h.s.MissedPolicy == "manual" {
		return apperr.Conflict("WORKFLOW_GUARD_FAILED", fmt.Sprintf("%d checkpoint belum di-scan atau ditandai missed", pending))
	}
	rows, err := tx.Query(ctx, `UPDATE checkpoint_scans SET status = 'missed', missed_reason = 'Tidak di-scan saat patrol selesai (auto)' WHERE patrol_task_id = $1 AND status = 'pending' RETURNING checkpoint_id`, item.ID)
	if err != nil {
		return err
	}
	var cps []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		cps = append(cps, id)
	}
	rows.Close()
	_, _ = tx.Exec(ctx, `UPDATE patrol_tasks SET missed_checkpoints = (SELECT count(*) FROM checkpoint_scans WHERE patrol_task_id = $1 AND status = 'missed') WHERE task_id = $1`, item.ID)
	for _, cp := range cps {
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjTask, ObjectID: item.ID, Action: audit.ActCheckpointMissed, Payload: map[string]any{"checkpoint_id": cp, "auto": true}})
		h.s.emitMissed(ctx, tx, item.ID, cp, "auto")
	}
	return nil
}

func (h patrolHook) AfterTransition(ctx context.Context, tx pgx.Tx, item *operations.WorkItem, action string, from, to workflow.Status) error {
	return nil
}

func strPtr(s string) *string { return &s }
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
