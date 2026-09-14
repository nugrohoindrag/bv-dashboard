// Package housekeeping: Cleaning Schedule → Cleaning Task → Housekeeping Inspection → Finding → Rework/WO
// (PRD §15, AT-007, WF-003; Naming Convention §16–§17). Cleaning Task = tasks (task_type=cleaning) + cleaning_tasks.
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
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/property"
)

type Service struct {
	DB          *db.DB
	Jobs        jobs.Enqueuer
	Ops         *operations.Service
	HorizonDays int
}

func New(d *db.DB, j jobs.Enqueuer, ops *operations.Service) *Service {
	s := &Service{DB: d, Jobs: j, Ops: ops, HorizonDays: 7}
	ops.RegisterHook("task:cleaning", cleaningHook{s: s})
	return s
}

var cleaningTypes = map[string]bool{"routine": true, "periodic": true, "deep": true, "spot": true, "special": true}

// ---------- Cleaning Schedule ----------

type CleaningSchedule struct {
	ID                    uuid.UUID  `json:"id"`
	PropertyID            uuid.UUID  `json:"property_id"`
	Name                  string     `json:"name"`
	LocationID            uuid.UUID  `json:"location_id"`
	LocationPath          string     `json:"location_path"`
	CleaningType          string     `json:"cleaning_type"`
	StartTime             string     `json:"start_time"`
	DurationMinutes       int        `json:"duration_minutes"`
	Weekdays              []int      `json:"weekdays"`
	ChecklistTemplateID   *uuid.UUID `json:"checklist_template_id"`
	ResponsibleTeamID     *uuid.UUID `json:"responsible_team_id"`
	DefaultAssigneeUserID *uuid.UUID `json:"default_assignee_user_id"`
	Priority              string     `json:"priority"`
	RequiresPhoto         bool       `json:"requires_photo"`
	IsActive              bool       `json:"is_active"`
	ValidFrom             *string    `json:"valid_from"`
	ValidUntil            *string    `json:"valid_until"`
	Version               int        `json:"version"`
}

type ScheduleInput struct {
	Name                  *string    `json:"name"`
	LocationID            *uuid.UUID `json:"location_id"`
	CleaningType          *string    `json:"cleaning_type"`
	StartTime             *string    `json:"start_time"`
	DurationMinutes       *int       `json:"duration_minutes"`
	Weekdays              *[]int     `json:"weekdays"`
	ChecklistTemplateID   *uuid.UUID `json:"checklist_template_id"`
	ResponsibleTeamID     *uuid.UUID `json:"responsible_team_id"`
	DefaultAssigneeUserID *uuid.UUID `json:"default_assignee_user_id"`
	Priority              *string    `json:"priority"`
	RequiresPhoto         *bool      `json:"requires_photo"`
	IsActive              *bool      `json:"is_active"`
	ValidFrom             *string    `json:"valid_from"`
	ValidUntil            *string    `json:"valid_until"`
}

func (s *Service) CreateSchedule(ctx context.Context, in ScheduleInput) (*CleaningSchedule, error) {
	var out *CleaningSchedule
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

// CreateScheduleTx: di dalam transaksi (seed / import); langsung generate cleaning task horizon.
func (s *Service) CreateScheduleTx(ctx context.Context, tx pgx.Tx, in ScheduleInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if in.Name == nil || in.LocationID == nil || in.StartTime == nil {
		return uuid.Nil, apperr.Validation("name, location_id, start_time wajib")
	}
	st, err := time.Parse("15:04", *in.StartTime)
	if err != nil {
		return uuid.Nil, apperr.Validation("start_time harus HH:MM")
	}
	ct := "routine"
	if in.CleaningType != nil {
		ct = *in.CleaningType
	}
	if !cleaningTypes[ct] {
		return uuid.Nil, apperr.Validation("cleaning_type harus routine|periodic|deep|spot|special")
	}
	prio := "medium"
	if in.Priority != nil {
		prio = *in.Priority
	}
	if !operations.Priorities[prio] {
		return uuid.Nil, apperr.Validation("priority tidak valid")
	}
	dur := 60
	if in.DurationMinutes != nil && *in.DurationMinutes > 0 {
		dur = *in.DurationMinutes
	}
	wd := []int{0, 1, 2, 3, 4, 5, 6}
	if in.Weekdays != nil && len(*in.Weekdays) > 0 {
		wd = *in.Weekdays
	}
	reqPhoto := true
	if in.RequiresPhoto != nil {
		reqPhoto = *in.RequiresPhoto
	}
	pid, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
	if err != nil {
		return uuid.Nil, err
	}
	if !p.HasOnProperty("housekeeping.cleaning_schedules.create", pid) {
		return uuid.Nil, apperr.Forbidden("")
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO cleaning_schedules (organization_id, property_id, name, location_id, cleaning_type, start_time, duration_minutes, weekdays, checklist_template_id, responsible_team_id, default_assignee_user_id, priority, requires_photo, valid_from, valid_until, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::date,$15::date,$16,$16) RETURNING id`,
		p.OrganizationID, pid, strings.TrimSpace(*in.Name), *in.LocationID, ct, st.Format("15:04:05"), dur, wd, in.ChecklistTemplateID, in.ResponsibleTeamID, in.DefaultAssigneeUserID, prio, reqPhoto, nilIfEmpty(in.ValidFrom), nilIfEmpty(in.ValidUntil), actorOrNil(p)).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "cleaning_schedule", EntityID: &id, EntityLabel: *in.Name, After: in})
	if _, err := s.generateForScheduleTx(ctx, tx, id); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func actorOrNil(p *authctx.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}

func nilIfEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

func (s *Service) UpdateSchedule(ctx context.Context, id uuid.UUID, in ScheduleInput) (*CleaningSchedule, error) {
	p := authctx.Must(ctx)
	var out *CleaningSchedule
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getScheduleTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.HasOnProperty("housekeeping.cleaning_schedules.update", before.PropertyID) {
			return apperr.Forbidden("")
		}
		var startStr *string
		if in.StartTime != nil {
			st, err := time.Parse("15:04", *in.StartTime)
			if err != nil {
				return apperr.Validation("start_time harus HH:MM")
			}
			v := st.Format("15:04:05")
			startStr = &v
		}
		if in.CleaningType != nil && !cleaningTypes[*in.CleaningType] {
			return apperr.Validation("cleaning_type tidak valid")
		}
		if _, err := tx.Exec(ctx, `UPDATE cleaning_schedules SET name = COALESCE(NULLIF($2,''), name), location_id = COALESCE($3, location_id), cleaning_type = COALESCE(NULLIF($4,''), cleaning_type), start_time = COALESCE($5::time, start_time),
			duration_minutes = COALESCE($6, duration_minutes), weekdays = COALESCE($7, weekdays), checklist_template_id = COALESCE($8, checklist_template_id), responsible_team_id = COALESCE($9, responsible_team_id), default_assignee_user_id = COALESCE($10, default_assignee_user_id),
			priority = COALESCE(NULLIF($11,''), priority), requires_photo = COALESCE($12, requires_photo), is_active = COALESCE($13, is_active), valid_from = COALESCE($14::date, valid_from), valid_until = COALESCE($15::date, valid_until), updated_by = $16 WHERE id = $1`,
			id, deref(in.Name), in.LocationID, deref(in.CleaningType), startStr, in.DurationMinutes, in.Weekdays, in.ChecklistTemplateID, in.ResponsibleTeamID, in.DefaultAssigneeUserID, deref(in.Priority), in.RequiresPhoto, in.IsActive, nilIfEmpty(in.ValidFrom), nilIfEmpty(in.ValidUntil), p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "cleaning_schedule", EntityID: &id, EntityLabel: before.Name, Before: before, After: in})
		_, _ = tx.Exec(ctx, `UPDATE tasks t SET status = 'cancelled', cancelled_at = now() FROM cleaning_tasks ct WHERE ct.task_id = t.id AND ct.cleaning_schedule_id = $1 AND t.status IN ('new','scheduled','assigned') AND ct.schedule_date > CURRENT_DATE`, id)
		_, _ = tx.Exec(ctx, `DELETE FROM cleaning_tasks WHERE cleaning_schedule_id = $1 AND schedule_date > CURRENT_DATE AND task_id IN (SELECT id FROM tasks WHERE status = 'cancelled')`, id)
		if _, err := s.generateForScheduleTx(ctx, tx, id); err != nil {
			return err
		}
		out, err = s.getScheduleTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) getScheduleTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*CleaningSchedule, error) {
	var sc CleaningSchedule
	var st time.Time
	var vf, vu *time.Time
	if err := tx.QueryRow(ctx, `SELECT id, property_id, name, location_id, cleaning_type, start_time, duration_minutes, weekdays, checklist_template_id, responsible_team_id, default_assignee_user_id, priority, requires_photo, is_active, valid_from, valid_until, version FROM cleaning_schedules WHERE id = $1`, id).
		Scan(&sc.ID, &sc.PropertyID, &sc.Name, &sc.LocationID, &sc.CleaningType, &st, &sc.DurationMinutes, &sc.Weekdays, &sc.ChecklistTemplateID, &sc.ResponsibleTeamID, &sc.DefaultAssigneeUserID, &sc.Priority, &sc.RequiresPhoto, &sc.IsActive, &vf, &vu, &sc.Version); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Cleaning Schedule")
		}
		return nil, err
	}
	sc.StartTime = st.Format("15:04")
	sc.LocationPath = property.LocationPathText(ctx, tx, sc.LocationID)
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

func (s *Service) ListSchedules(ctx context.Context, propertyID *uuid.UUID) ([]CleaningSchedule, error) {
	p := authctx.Must(ctx)
	var out []CleaningSchedule
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		where := " WHERE 1=1"
		if propertyID != nil {
			args = append(args, *propertyID)
			where += " AND property_id = $1"
		} else if pids, all := p.PropertyIDsFor("housekeeping.cleaning_schedules.view"); !all {
			args = append(args, pids)
			where += " AND property_id = ANY($1::uuid[])"
		}
		rows, err := tx.Query(ctx, `SELECT id FROM cleaning_schedules`+where+` ORDER BY start_time, name`, args...)
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
		out = []CleaningSchedule{}
	}
	return out, err
}

// ---------- Cleaning Task generation ----------

func (s *Service) generateForScheduleTx(ctx context.Context, tx pgx.Tx, scheduleID uuid.UUID) (int, error) {
	sc, err := s.getScheduleTx(ctx, tx, scheduleID)
	if err != nil {
		return 0, err
	}
	if !sc.IsActive {
		return 0, nil
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
		ds := day.Format("2006-01-02")
		if sc.ValidFrom != nil && ds < *sc.ValidFrom {
			continue
		}
		if sc.ValidUntil != nil && ds > *sc.ValidUntil {
			continue
		}
		startAt := time.Date(day.Year(), day.Month(), day.Day(), st.Hour(), st.Minute(), 0, 0, loc)
		dueAt := startAt.Add(time.Duration(sc.DurationMinutes) * time.Minute)
		if d == 0 && dueAt.Before(now) {
			continue
		}
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cleaning_tasks WHERE cleaning_schedule_id = $1 AND schedule_date = $2)`, scheduleID, ds).Scan(&exists)
		if exists {
			continue
		}
		title := fmt.Sprintf("%s — %s", cleaningLabel(sc.CleaningType), sc.Name) // "Routine Cleaning — Toilet Lantai 12" (NC §17)
		taskID, err := s.Ops.CreateTaskTx(ctx, tx, operations.CreateTaskInput{
			PropertyID: &sc.PropertyID, TaskType: "cleaning", Title: title, LocationID: &sc.LocationID, Priority: sc.Priority,
			ScheduledStartAt: &startAt, DueAt: &dueAt, ChecklistTemplateID: sc.ChecklistTemplateID, RequiresPhoto: sc.RequiresPhoto,
			AssigneeUserID: sc.DefaultAssigneeUserID, AssigneeTeamID: sc.ResponsibleTeamID, SourceType: strPtr("cleaning_schedule"), SourceID: &scheduleID,
		})
		if err != nil {
			return n, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO cleaning_tasks (task_id, organization_id, cleaning_schedule_id, schedule_date, cleaning_type) VALUES ($1,$2,$3,$4::date,$5)
			ON CONFLICT (task_id) DO UPDATE SET cleaning_schedule_id = EXCLUDED.cleaning_schedule_id, schedule_date = EXCLUDED.schedule_date, cleaning_type = EXCLUDED.cleaning_type`, taskID, authctx.Must(ctx).OrganizationID, scheduleID, ds, sc.CleaningType); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func cleaningLabel(t string) string {
	switch t {
	case "routine":
		return "Routine Cleaning"
	case "periodic":
		return "Periodic Cleaning"
	case "deep":
		return "Deep Cleaning"
	case "spot":
		return "Spot Cleaning"
	case "special":
		return "Special Cleaning"
	}
	return "Cleaning"
}

func (s *Service) GenerateCleaningTasks(ctx context.Context, orgID uuid.UUID, scheduleID *uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	total := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var idList []uuid.UUID
		if scheduleID != nil {
			idList = []uuid.UUID{*scheduleID}
		} else {
			rows, err := tx.Query(ctx, `SELECT id FROM cleaning_schedules WHERE is_active`)
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

// CreateAdhocCleaning: cleaning task manual (spot cleaning dari SR, dsb).
type AdhocCleaningInput struct {
	operations.CreateTaskInput
	CleaningType string `json:"cleaning_type"`
}

func (s *Service) CreateAdhocCleaning(ctx context.Context, in AdhocCleaningInput) (*operations.WorkItem, error) {
	if in.CleaningType == "" {
		in.CleaningType = "spot"
	}
	if !cleaningTypes[in.CleaningType] {
		return nil, apperr.Validation("cleaning_type tidak valid")
	}
	in.TaskType = "cleaning"
	if in.Title == "" {
		in.Title = cleaningLabel(in.CleaningType)
	}
	var out *operations.WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		taskID, err := s.Ops.CreateTaskTx(ctx, tx, in.CreateTaskInput)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE cleaning_tasks SET cleaning_type = $2 WHERE task_id = $1`, taskID, in.CleaningType); err != nil {
			return err
		}
		out, err = s.Ops.GetTx(ctx, tx, operations.ObjTask, taskID)
		return err
	})
	return out, err
}

// ---------- Housekeeping Inspection (PRD §15.2, AT-007) ----------

type CreateInspectionInput struct {
	CleaningTaskID      uuid.UUID  `json:"cleaning_task_id"`
	ChecklistTemplateID *uuid.UUID `json:"checklist_template_id"`
	InspectorUserID     *uuid.UUID `json:"inspector_user_id"` // default: user saat ini
	Title               *string    `json:"title"`
}

// CreateInspection: task inspection (task_type=inspection) yang menunjuk cleaning task Completed; assignee = inspector.
func (s *Service) CreateInspection(ctx context.Context, in CreateInspectionInput) (*operations.WorkItem, error) {
	p := authctx.Must(ctx)
	var out *operations.WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		ct, err := s.Ops.GetTx(ctx, tx, operations.ObjTask, in.CleaningTaskID)
		if err != nil {
			return err
		}
		if ct.Type != "cleaning" {
			return apperr.Validation("cleaning_task_id bukan cleaning task")
		}
		if !p.HasOnProperty("housekeeping.inspections.create", ct.PropertyID) {
			return apperr.Forbidden("")
		}
		if ct.Status != workflow.Completed && ct.Status != workflow.Closed {
			return apperr.Conflict("WORKFLOW_GUARD_FAILED", "Inspection hanya untuk Cleaning Task yang sudah Completed")
		}
		inspector := p.UserID
		if in.InspectorUserID != nil {
			inspector = *in.InspectorUserID
		}
		title := "Inspeksi: " + ct.Title
		if in.Title != nil && *in.Title != "" {
			title = *in.Title
		}
		taskID, err := s.Ops.CreateTaskTx(ctx, tx, operations.CreateTaskInput{
			PropertyID: &ct.PropertyID, TaskType: "inspection", Title: title, LocationID: ct.Location.ID, Priority: ct.Priority,
			ChecklistTemplateID: in.ChecklistTemplateID, AssigneeUserID: &inspector, SourceType: strPtr(operations.ObjTask), SourceID: &ct.ID,
			LinkTo: &operations.LinkRef{ObjectType: operations.ObjTask, ObjectID: ct.ID, LinkType: "generated_from"},
		})
		if err != nil {
			return err
		}
		loc := property.PropertyTimezone(ctx, tx, ct.PropertyID)
		number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixInspection, time.Now(), loc)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inspections (task_id, organization_id, inspection_number, inspection_type) VALUES ($1,$2,$3,'housekeeping')`, taskID, p.OrganizationID, number); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO housekeeping_inspections (task_id, organization_id, cleaning_task_id, inspector_user_id) VALUES ($1,$2,$3,$4)`, taskID, p.OrganizationID, ct.ID, inspector); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjTask, ObjectID: ct.ID, Action: "inspection_created", Payload: map[string]any{"inspection_task_id": taskID, "inspection_number": number}})
		out, err = s.Ops.GetTx(ctx, tx, operations.ObjTask, taskID)
		return err
	})
	return out, err
}

// ---------- Hook: cleaning complete → inspection_status not_inspected (siap diinspeksi) ----------

type cleaningHook struct{ s *Service }

// AfterCreate: cleaning task yang dibuat lewat jalur generik (rework dari finding, /tasks) tetap punya extension row.
func (h cleaningHook) AfterCreate(ctx context.Context, tx pgx.Tx, item *operations.WorkItem) error {
	_, err := tx.Exec(ctx, `INSERT INTO cleaning_tasks (task_id, organization_id, cleaning_type) VALUES ($1,$2,'spot') ON CONFLICT (task_id) DO NOTHING`, item.ID, authctx.Must(ctx).OrganizationID)
	return err
}

func (h cleaningHook) BeforeComplete(context.Context, pgx.Tx, *operations.WorkItem, operations.TransitionInput) error {
	return nil
}
func (h cleaningHook) AfterTransition(ctx context.Context, tx pgx.Tx, item *operations.WorkItem, action string, from, to workflow.Status) error {
	if action == workflow.ActReopen {
		_, _ = tx.Exec(ctx, `UPDATE cleaning_tasks SET inspection_status = 'not_inspected' WHERE task_id = $1`, item.ID)
	}
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
