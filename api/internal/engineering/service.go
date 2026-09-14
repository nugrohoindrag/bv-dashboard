// Package engineering: Maintenance Plan → Maintenance Schedule → Maintenance Work Order (PRD §12.2, AT-004, WF-001),
// Corrective (PRD §12.3), Inspection (PRD §12.4). Extend operations lewat hook (TAD §5.2, §5.8).
package engineering

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
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/property"
)

type Service struct {
	DB   *db.DB
	Jobs jobs.Enqueuer
	Ops  *operations.Service
	// ScheduleHorizonDays: horizon generate schedule (TAD §5.11: 60 hari)
	ScheduleHorizonDays int
}

func New(d *db.DB, j jobs.Enqueuer, ops *operations.Service) *Service {
	s := &Service{DB: d, Jobs: j, Ops: ops, ScheduleHorizonDays: 60}
	ops.RegisterHook("work_order:maintenance", maintenanceHook{s: s})
	ops.RegisterHook("task:inspection", inspectionHook{s: s})
	return s
}

// ---------- Maintenance Plan (PRD §12.2 minimal: asset, frequency, start date, checklist, default priority, responsible team) ----------

type MaintenancePlan struct {
	ID                  uuid.UUID  `json:"id"`
	PropertyID          uuid.UUID  `json:"property_id"`
	PlanCode            string     `json:"plan_code"`
	Name                string     `json:"name"`
	AssetID             uuid.UUID  `json:"asset_id"`
	AssetCode           string     `json:"asset_code"`
	AssetName           string     `json:"asset_name"`
	Frequency           string     `json:"frequency"`
	IntervalDays        *int       `json:"interval_days"`
	StartDate           time.Time  `json:"start_date"`
	EndDate             *time.Time `json:"end_date"`
	ChecklistTemplateID *uuid.UUID `json:"checklist_template_id"`
	DefaultPriority     string     `json:"default_priority"`
	ResponsibleTeamID   *uuid.UUID `json:"responsible_team_id"`
	ResponsibleTeamName *string    `json:"responsible_team_name"`
	LeadTimeDays        int        `json:"lead_time_days"`
	DurationMinutes     *int       `json:"duration_minutes"`
	Status              string     `json:"status"`
	Description         *string    `json:"description"`
	NextDue             *time.Time `json:"next_due"`
	ScheduleCount       int        `json:"schedule_count"`
	Version             int        `json:"version"`
}

type PlanInput struct {
	Name                *string    `json:"name"`
	AssetID             *uuid.UUID `json:"asset_id"`
	Frequency           *string    `json:"frequency"`
	IntervalDays        *int       `json:"interval_days"`
	StartDate           *string    `json:"start_date"` // YYYY-MM-DD (tanggal lokal property)
	EndDate             *string    `json:"end_date"`
	ChecklistTemplateID *uuid.UUID `json:"checklist_template_id"`
	DefaultPriority     *string    `json:"default_priority"`
	ResponsibleTeamID   *uuid.UUID `json:"responsible_team_id"`
	LeadTimeDays        *int       `json:"lead_time_days"`
	DurationMinutes     *int       `json:"duration_minutes"`
	Description         *string    `json:"description"`
}

var frequencies = map[string]int{"daily": 1, "weekly": 7, "biweekly": 14, "monthly": 0, "quarterly": 0, "semiannual": 0, "annual": 0, "custom_days": -1}

func (s *Service) CreatePlan(ctx context.Context, in PlanInput) (*MaintenancePlan, error) {
	p := authctx.Must(ctx)
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" || in.AssetID == nil || in.Frequency == nil || in.StartDate == nil {
		return nil, apperr.Validation("name, asset_id, frequency, start_date wajib")
	}
	if _, ok := frequencies[*in.Frequency]; !ok {
		return nil, apperr.Validation("frequency tidak valid")
	}
	if *in.Frequency == "custom_days" && (in.IntervalDays == nil || *in.IntervalDays < 1) {
		return nil, apperr.Validation("interval_days wajib untuk custom_days")
	}
	start, err := time.Parse("2006-01-02", *in.StartDate)
	if err != nil {
		return nil, apperr.Validation("start_date harus YYYY-MM-DD")
	}
	var end *time.Time
	if in.EndDate != nil && *in.EndDate != "" {
		e, err := time.Parse("2006-01-02", *in.EndDate)
		if err != nil {
			return nil, apperr.Validation("end_date harus YYYY-MM-DD")
		}
		end = &e
	}
	prio := "medium"
	if in.DefaultPriority != nil {
		prio = *in.DefaultPriority
	}
	if !operations.Priorities[prio] {
		return nil, apperr.Validation("default_priority tidak valid")
	}
	var out *MaintenancePlan
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var propertyID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM assets WHERE id = $1 AND deleted_at IS NULL`, *in.AssetID).Scan(&propertyID); err != nil {
			return apperr.Validation("asset_id tidak ditemukan")
		}
		if !p.HasOnProperty("engineering.maintenance_plans.create", propertyID) {
			return apperr.Forbidden("")
		}
		if in.ChecklistTemplateID != nil {
			var st string
			if err := tx.QueryRow(ctx, `SELECT status FROM checklist_templates WHERE id = $1 AND deleted_at IS NULL`, *in.ChecklistTemplateID).Scan(&st); err != nil || st != "published" {
				return apperr.Validation("checklist_template_id tidak ditemukan / belum dipublikasikan")
			}
		}
		loc := property.PropertyTimezone(ctx, tx, propertyID)
		code, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixMaintenancePlan, time.Now(), loc)
		if err != nil {
			return err
		}
		lead := 0
		if in.LeadTimeDays != nil {
			lead = *in.LeadTimeDays
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO maintenance_plans (organization_id, property_id, plan_code, name, asset_id, frequency, interval_days, start_date, end_date, checklist_template_id, default_priority, responsible_team_id, lead_time_days, duration_minutes, description, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$16) RETURNING id`,
			p.OrganizationID, propertyID, code, strings.TrimSpace(*in.Name), *in.AssetID, *in.Frequency, in.IntervalDays, start, end, in.ChecklistTemplateID, prio, in.ResponsibleTeamID, lead, in.DurationMinutes, in.Description, p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "maintenance_plan", EntityID: &id, EntityLabel: code, After: in})
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: *in.AssetID, Action: "maintenance_plan_created", Payload: map[string]any{"plan_code": code, "frequency": *in.Frequency}})
		out, err = s.getPlanTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) UpdatePlan(ctx context.Context, id uuid.UUID, in PlanInput) (*MaintenancePlan, error) {
	p := authctx.Must(ctx)
	var out *MaintenancePlan
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getPlanTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.HasOnProperty("engineering.maintenance_plans.update", before.PropertyID) {
			return apperr.Forbidden("")
		}
		if before.Status == "archived" {
			return apperr.Conflict("PLAN_ARCHIVED", "Maintenance Plan sudah diarsipkan")
		}
		if in.Frequency != nil {
			if _, ok := frequencies[*in.Frequency]; !ok {
				return apperr.Validation("frequency tidak valid")
			}
		}
		var start, end *time.Time
		if in.StartDate != nil {
			t, err := time.Parse("2006-01-02", *in.StartDate)
			if err != nil {
				return apperr.Validation("start_date harus YYYY-MM-DD")
			}
			start = &t
		}
		if in.EndDate != nil {
			if *in.EndDate == "" {
				z := time.Time{}
				end = &z
			} else {
				t, err := time.Parse("2006-01-02", *in.EndDate)
				if err != nil {
					return apperr.Validation("end_date harus YYYY-MM-DD")
				}
				end = &t
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE maintenance_plans SET name = COALESCE(NULLIF($2,''), name), frequency = COALESCE(NULLIF($3,''), frequency), interval_days = COALESCE($4, interval_days),
			start_date = COALESCE($5, start_date), end_date = CASE WHEN $6::timestamptz IS NULL THEN end_date WHEN $6 = '0001-01-01'::timestamptz THEN NULL ELSE $6::date END,
			checklist_template_id = COALESCE($7, checklist_template_id), default_priority = COALESCE(NULLIF($8,''), default_priority), responsible_team_id = COALESCE($9, responsible_team_id),
			lead_time_days = COALESCE($10, lead_time_days), duration_minutes = COALESCE($11, duration_minutes), description = COALESCE($12, description), updated_by = $13 WHERE id = $1`,
			id, deref(in.Name), deref(in.Frequency), in.IntervalDays, start, end, in.ChecklistTemplateID, deref(in.DefaultPriority), in.ResponsibleTeamID, in.LeadTimeDays, in.DurationMinutes, in.Description, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "maintenance_plan", EntityID: &id, EntityLabel: before.PlanCode, Before: before, After: in})
		// schedule ke depan yang belum punya WO dihapus & digenerate ulang oleh job
		if before.Status == "published" {
			_, _ = tx.Exec(ctx, `DELETE FROM maintenance_schedules WHERE plan_id = $1 AND status = 'scheduled' AND work_order_id IS NULL AND due_date > CURRENT_DATE`, id)
			if s.Jobs != nil {
				_ = s.Jobs.EnqueueTx(ctx, tx, jobs.MaintenanceScheduleGenerateArgs{PlanID: &id, OrganizationID: &p.OrganizationID})
			}
		}
		out, err = s.getPlanTx(ctx, tx, id)
		return err
	})
	return out, err
}

// SetPlanStatus: draft → published (generate schedule), → archived.
func (s *Service) SetPlanStatus(ctx context.Context, id uuid.UUID, status string) (*MaintenancePlan, error) {
	p := authctx.Must(ctx)
	if status != "published" && status != "archived" {
		return nil, apperr.Validation("status harus published|archived")
	}
	var out *MaintenancePlan
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		plan, err := s.getPlanTx(ctx, tx, id)
		if err != nil {
			return err
		}
		perm := "engineering.maintenance_plans.publish"
		if status == "archived" {
			perm = "engineering.maintenance_plans.archive"
		}
		if !p.HasOnProperty(perm, plan.PropertyID) {
			return apperr.Forbidden("")
		}
		if _, err := tx.Exec(ctx, `UPDATE maintenance_plans SET status = $2, updated_by = $3 WHERE id = $1`, id, status, p.UserID); err != nil {
			return err
		}
		if status == "archived" {
			_, _ = tx.Exec(ctx, `UPDATE maintenance_schedules SET status = 'cancelled' WHERE plan_id = $1 AND status IN ('scheduled','due') AND work_order_id IS NULL`, id)
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "maintenance_plan", EntityID: &id, EntityLabel: plan.PlanCode, Before: map[string]any{"status": plan.Status}, After: map[string]any{"status": status}})
		if status == "published" {
			// generate langsung (sinkron) agar AT-004 terlihat seketika; job periodic menjaga horizon
			if _, err := s.generateSchedulesForPlanTx(ctx, tx, id); err != nil {
				return err
			}
		}
		out, err = s.getPlanTx(ctx, tx, id)
		return err
	})
	return out, err
}

const planSelect = `SELECT mp.id, mp.property_id, mp.plan_code, mp.name, mp.asset_id, a.asset_code, a.name, mp.frequency, mp.interval_days, mp.start_date, mp.end_date, mp.checklist_template_id,
	mp.default_priority, mp.responsible_team_id, t.name, mp.lead_time_days, mp.duration_minutes, mp.status, mp.description, mp.version,
	(SELECT min(ms.due_at) FROM maintenance_schedules ms WHERE ms.plan_id = mp.id AND ms.status IN ('scheduled','due','overdue')),
	(SELECT count(*) FROM maintenance_schedules ms WHERE ms.plan_id = mp.id)
	FROM maintenance_plans mp JOIN assets a ON a.id = mp.asset_id LEFT JOIN teams t ON t.id = mp.responsible_team_id`

func scanPlan(row pgx.Row) (*MaintenancePlan, error) {
	var m MaintenancePlan
	if err := row.Scan(&m.ID, &m.PropertyID, &m.PlanCode, &m.Name, &m.AssetID, &m.AssetCode, &m.AssetName, &m.Frequency, &m.IntervalDays, &m.StartDate, &m.EndDate, &m.ChecklistTemplateID,
		&m.DefaultPriority, &m.ResponsibleTeamID, &m.ResponsibleTeamName, &m.LeadTimeDays, &m.DurationMinutes, &m.Status, &m.Description, &m.Version, &m.NextDue, &m.ScheduleCount); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Service) getPlanTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*MaintenancePlan, error) {
	m, err := scanPlan(tx.QueryRow(ctx, planSelect+` WHERE mp.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Maintenance Plan")
		}
		return nil, err
	}
	return m, nil
}

func (s *Service) GetPlan(ctx context.Context, id uuid.UUID) (*MaintenancePlan, error) {
	var out *MaintenancePlan
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		m, err := s.getPlanTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasOnProperty("engineering.maintenance_plans.view", m.PropertyID) {
			return apperr.Forbidden("")
		}
		out = m
		return nil
	})
	return out, err
}

func (s *Service) ListPlans(ctx context.Context, propertyID, assetID *uuid.UUID, status, q string) ([]MaintenancePlan, error) {
	p := authctx.Must(ctx)
	var out []MaintenancePlan
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE 1=1"
		if propertyID != nil {
			where += " AND mp.property_id = " + add(*propertyID)
		} else if pids, all := p.PropertyIDsFor("engineering.maintenance_plans.view"); !all {
			where += " AND mp.property_id = ANY(" + add(pids) + "::uuid[])"
		}
		if assetID != nil {
			where += " AND mp.asset_id = " + add(*assetID)
		}
		if status != "" {
			where += " AND mp.status = " + add(status)
		}
		if q != "" {
			qq := add("%" + q + "%")
			where += " AND (mp.name ILIKE " + qq + " OR mp.plan_code ILIKE " + qq + " OR a.name ILIKE " + qq + ")"
		}
		rows, err := tx.Query(ctx, planSelect+where+" ORDER BY mp.plan_code LIMIT 500", args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanPlan(rows)
			if err != nil {
				return err
			}
			out = append(out, *m)
		}
		return rows.Err()
	})
	if out == nil {
		out = []MaintenancePlan{}
	}
	return out, err
}

// ---------- Schedule generation (idempotent per (plan, due_date); horizon 60 hari) ----------

func nextDueDates(freq string, interval int, start time.Time, from, until time.Time) []time.Time {
	var out []time.Time
	d := start
	step := func(t time.Time) time.Time {
		switch freq {
		case "daily":
			return t.AddDate(0, 0, 1)
		case "weekly":
			return t.AddDate(0, 0, 7)
		case "biweekly":
			return t.AddDate(0, 0, 14)
		case "monthly":
			return t.AddDate(0, 1, 0)
		case "quarterly":
			return t.AddDate(0, 3, 0)
		case "semiannual":
			return t.AddDate(0, 6, 0)
		case "annual":
			return t.AddDate(1, 0, 0)
		default:
			if interval < 1 {
				interval = 30
			}
			return t.AddDate(0, 0, interval)
		}
	}
	for guard := 0; guard < 5000 && !d.After(until); guard++ {
		if !d.Before(from) {
			out = append(out, d)
		}
		d = step(d)
	}
	return out
}

// generateSchedulesForPlanTx: schedule dari hari ini s/d horizon; insert ON CONFLICT DO NOTHING (idempotent).
func (s *Service) generateSchedulesForPlanTx(ctx context.Context, tx pgx.Tx, planID uuid.UUID) (int, error) {
	p := authctx.Must(ctx)
	plan, err := s.getPlanTx(ctx, tx, planID)
	if err != nil {
		return 0, err
	}
	if plan.Status != "published" {
		return 0, nil
	}
	loc := property.PropertyTimezone(ctx, tx, plan.PropertyID)
	today := time.Now().In(loc)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	until := today.AddDate(0, 0, s.ScheduleHorizonDays)
	if plan.EndDate != nil && plan.EndDate.Before(until) {
		until = *plan.EndDate
	}
	start := time.Date(plan.StartDate.Year(), plan.StartDate.Month(), plan.StartDate.Day(), 0, 0, 0, 0, loc)
	interval := 0
	if plan.IntervalDays != nil {
		interval = *plan.IntervalDays
	}
	n := 0
	for _, d := range nextDueDates(plan.Frequency, interval, start, today, until) {
		dueAt := time.Date(d.Year(), d.Month(), d.Day(), 17, 0, 0, 0, loc) // due akhir hari kerja lokal
		tag, err := tx.Exec(ctx, `INSERT INTO maintenance_schedules (organization_id, property_id, plan_id, asset_id, due_date, due_at, status) VALUES ($1,$2,$3,$4,$5,$6,'scheduled') ON CONFLICT (plan_id, due_date) DO NOTHING`,
			p.OrganizationID, plan.PropertyID, plan.ID, plan.AssetID, d.Format("2006-01-02"), dueAt.UTC())
		if err != nil {
			return n, err
		}
		n += int(tag.RowsAffected())
	}
	return n, nil
}

// GenerateSchedules: job harian (semua plan published di org) atau satu plan.
func (s *Service) GenerateSchedules(ctx context.Context, orgID uuid.UUID, planID *uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	total := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var idList []uuid.UUID
		if planID != nil {
			idList = []uuid.UUID{*planID}
		} else {
			rows, err := tx.Query(ctx, `SELECT id FROM maintenance_plans WHERE status = 'published'`)
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
			n, err := s.generateSchedulesForPlanTx(ctx, tx, id)
			if err != nil {
				return err
			}
			total += n
		}
		return nil
	})
	return total, err
}

// CreateDueWorkOrders (AT-004): schedule yang due_date <= today + lead_time → status due + Maintenance WO (idempotent: work_order_id).
func (s *Service) CreateDueWorkOrders(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	created := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT ms.id, ms.plan_id, ms.property_id, ms.asset_id, ms.due_at, mp.name, mp.checklist_template_id, mp.default_priority, mp.responsible_team_id, mp.plan_code, mp.duration_minutes, a.location_id, a.name
			FROM maintenance_schedules ms JOIN maintenance_plans mp ON mp.id = ms.plan_id JOIN assets a ON a.id = ms.asset_id JOIN properties pr ON pr.location_id = ms.property_id
			WHERE ms.status IN ('scheduled','due','overdue') AND ms.work_order_id IS NULL AND mp.status = 'published'
			  AND ms.due_date <= ((now() AT TIME ZONE pr.timezone)::date + mp.lead_time_days)
			ORDER BY ms.due_at`)
		if err != nil {
			return err
		}
		type row struct {
			id, planID, propID, assetID uuid.UUID
			dueAt                       time.Time
			name                        string
			tplID                       *uuid.UUID
			prio                        string
			teamID                      *uuid.UUID
			planCode                    string
			duration                    *int
			locID                       uuid.UUID
			assetName                   string
		}
		var rs []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.planID, &r.propID, &r.assetID, &r.dueAt, &r.name, &r.tplID, &r.prio, &r.teamID, &r.planCode, &r.duration, &r.locID, &r.assetName); err != nil {
				rows.Close()
				return err
			}
			rs = append(rs, r)
		}
		rows.Close()
		for _, r := range rs {
			loc := property.PropertyTimezone(ctx, tx, r.propID)
			title := fmt.Sprintf("PM %s — %s (%s)", r.name, r.assetName, r.dueAt.In(loc).Format("02 Jan 2006"))
			desc := fmt.Sprintf("Preventive Maintenance dari %s", r.planCode)
			woID, err := s.Ops.CreateWorkOrderTx(ctx, tx, operations.CreateWorkOrderInput{
				PropertyID: &r.propID, WorkOrderType: "maintenance", Title: title, Description: &desc, LocationID: &r.locID, AssetID: &r.assetID,
				Priority: r.prio, DueAt: &r.dueAt, ChecklistTemplateID: r.tplID, AssigneeTeamID: r.teamID,
				SourceType: strPtr(operations.ObjMaintenanceSchedule), SourceID: &r.id, MaintenanceScheduleID: &r.id,
			})
			if err != nil {
				return fmt.Errorf("create maintenance WO for schedule %s: %w", r.id, err)
			}
			newStatus := "due"
			if r.dueAt.Before(time.Now()) {
				newStatus = "overdue"
			}
			if _, err := tx.Exec(ctx, `UPDATE maintenance_schedules SET work_order_id = $2, status = $3 WHERE id = $1`, r.id, woID, newStatus); err != nil {
				return err
			}
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: r.assetID, Action: "maintenance_work_order_created", Payload: map[string]any{"work_order_id": woID, "schedule_id": r.id, "plan_code": r.planCode}})
			if s.Jobs != nil {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.MaintenanceScheduleDue, OrganizationID: orgID, PropertyID: &r.propID, ObjectType: operations.ObjMaintenanceSchedule, ObjectID: r.id, ObjectLabel: r.planCode, Payload: map[string]any{"work_order_id": woID, "asset_id": r.assetID}})
			}
			created++
		}
		return nil
	})
	return created, err
}

func strPtr(s string) *string { return &s }
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------- Maintenance Schedule list / skip ----------

type Schedule struct {
	ID              uuid.UUID  `json:"id"`
	PropertyID      uuid.UUID  `json:"property_id"`
	PlanID          uuid.UUID  `json:"plan_id"`
	PlanCode        string     `json:"plan_code"`
	PlanName        string     `json:"plan_name"`
	AssetID         uuid.UUID  `json:"asset_id"`
	AssetCode       string     `json:"asset_code"`
	AssetName       string     `json:"asset_name"`
	LocationPath    string     `json:"location_path"`
	DueDate         string     `json:"due_date"`
	DueAt           time.Time  `json:"due_at"`
	Status          string     `json:"status"`
	WorkOrderID     *uuid.UUID `json:"work_order_id"`
	WorkOrderNumber *string    `json:"work_order_number"`
	WorkOrderStatus *string    `json:"work_order_status"`
	CompletedAt     *time.Time `json:"completed_at"`
	SkippedReason   *string    `json:"skipped_reason"`
	Priority        string     `json:"priority"`
	TeamName        *string    `json:"team_name"`
}

type ScheduleFilter struct {
	PropertyID    *uuid.UUID
	PlanID        *uuid.UUID
	AssetID       *uuid.UUID
	Statuses      []string
	From, To      *time.Time
	DueWithinDays *int
}

func (s *Service) ListSchedules(ctx context.Context, f ScheduleFilter, page httpx.Page) ([]Schedule, *string, error) {
	p := authctx.Must(ctx)
	var out []Schedule
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE 1=1"
		if f.PropertyID != nil {
			where += " AND ms.property_id = " + add(*f.PropertyID)
		} else if pids, all := p.PropertyIDsFor("engineering.maintenance_schedules.view"); !all {
			where += " AND ms.property_id = ANY(" + add(pids) + "::uuid[])"
		}
		if f.PlanID != nil {
			where += " AND ms.plan_id = " + add(*f.PlanID)
		}
		if f.AssetID != nil {
			where += " AND ms.asset_id = " + add(*f.AssetID)
		}
		if len(f.Statuses) > 0 {
			where += " AND ms.status = ANY(" + add(f.Statuses) + ")"
		}
		if f.From != nil {
			where += " AND ms.due_at >= " + add(*f.From)
		}
		if f.To != nil {
			where += " AND ms.due_at <= " + add(*f.To)
		}
		if f.DueWithinDays != nil {
			where += " AND ms.due_at <= now() + make_interval(days => " + add(*f.DueWithinDays) + ") AND ms.status IN ('scheduled','due','overdue')"
		}
		if page.Cursor != nil {
			where += " AND (ms.due_at, ms.id) > (" + add(page.Cursor.Value) + "::timestamptz, " + add(page.Cursor.ID) + ")"
		}
		rows, err := tx.Query(ctx, `SELECT ms.id, ms.property_id, ms.plan_id, mp.plan_code, mp.name, ms.asset_id, a.asset_code, a.name, a.location_id, ms.due_date::text, ms.due_at, ms.status, ms.work_order_id, w.work_order_number, w.status, ms.completed_at, ms.skipped_reason, mp.default_priority, t.name
			FROM maintenance_schedules ms JOIN maintenance_plans mp ON mp.id = ms.plan_id JOIN assets a ON a.id = ms.asset_id LEFT JOIN work_orders w ON w.id = ms.work_order_id LEFT JOIN teams t ON t.id = mp.responsible_team_id`+
			where+` ORDER BY ms.due_at, ms.id LIMIT `+add(page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var locIDs []uuid.UUID
		for rows.Next() {
			var sc Schedule
			var locID uuid.UUID
			if err := rows.Scan(&sc.ID, &sc.PropertyID, &sc.PlanID, &sc.PlanCode, &sc.PlanName, &sc.AssetID, &sc.AssetCode, &sc.AssetName, &locID, &sc.DueDate, &sc.DueAt, &sc.Status, &sc.WorkOrderID, &sc.WorkOrderNumber, &sc.WorkOrderStatus, &sc.CompletedAt, &sc.SkippedReason, &sc.Priority, &sc.TeamName); err != nil {
				return err
			}
			locIDs = append(locIDs, locID)
			out = append(out, sc)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		for i := range out {
			out[i].LocationPath = property.LocationPathText(ctx, tx, locIDs[i])
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			out = out[:page.Limit]
			c := httpx.EncodeCursor(last.DueAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
		}
		return nil
	})
	if out == nil {
		out = []Schedule{}
	}
	return out, next, err
}

func (s *Service) SkipSchedule(ctx context.Context, id uuid.UUID, reason string) error {
	p := authctx.Must(ctx)
	if strings.TrimSpace(reason) == "" {
		return apperr.Validation("reason wajib")
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pid uuid.UUID
		var status string
		var woID *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id, status, work_order_id FROM maintenance_schedules WHERE id = $1`, id).Scan(&pid, &status, &woID); err != nil {
			return apperr.NotFound("Maintenance Schedule")
		}
		if !p.HasOnProperty("engineering.maintenance_schedules.skip", pid) {
			return apperr.Forbidden("")
		}
		if status == "completed" || status == "in_progress" {
			return apperr.InvalidTransition("Schedule " + status + " tidak dapat di-skip")
		}
		if woID != nil {
			if _, err := s.Ops.TransitionTx(ctx, tx, operations.ObjWorkOrder, *woID, workflow.ActCancel, operations.TransitionInput{Reason: "PM skipped: " + reason}); err != nil && !apperr.Is(err, "WORKFLOW_INVALID_TRANSITION") && !apperr.Is(err, "OBJECT_TERMINAL") {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE maintenance_schedules SET status = 'skipped', skipped_reason = $2 WHERE id = $1`, id, reason); err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "maintenance_schedule", EntityID: &id, After: map[string]any{"status": "skipped", "reason": reason}})
	})
}

// ---------- Hooks ----------

// maintenanceHook: WO maintenance ↔ schedule status; asset history (WF-001 "Asset History Updated").
type maintenanceHook struct{ s *Service }

func (h maintenanceHook) BeforeComplete(context.Context, pgx.Tx, *operations.WorkItem, operations.TransitionInput) error {
	return nil
}

func (h maintenanceHook) AfterTransition(ctx context.Context, tx pgx.Tx, item *operations.WorkItem, action string, from, to workflow.Status) error {
	if item.MaintenanceScheduleID != nil {
		var st string
		switch to {
		case workflow.InProgress:
			st = "in_progress"
		case workflow.Completed, workflow.Closed:
			st = "completed"
		case workflow.Cancelled:
			st = "cancelled"
		}
		if st != "" {
			if st == "completed" {
				_, _ = tx.Exec(ctx, `UPDATE maintenance_schedules SET status = 'completed', completed_at = COALESCE(completed_at, now()) WHERE id = $1`, *item.MaintenanceScheduleID)
			} else {
				_, _ = tx.Exec(ctx, `UPDATE maintenance_schedules SET status = $2 WHERE id = $1 AND status <> 'completed'`, *item.MaintenanceScheduleID, st)
			}
		}
		if to == workflow.InProgress && from != workflow.OnHold && action != workflow.ActReopen {
			_, _ = tx.Exec(ctx, `UPDATE maintenance_schedules SET status = 'in_progress' WHERE id = $1 AND status <> 'completed'`, *item.MaintenanceScheduleID)
		}
	}
	if item.Asset.ID != nil {
		switch action {
		case workflow.ActClose:
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: *item.Asset.ID, Action: "maintenance_closed", Payload: map[string]any{"work_order_number": item.Number, "type": item.Type, "resolution": item.Resolution}})
		case workflow.ActComplete:
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: *item.Asset.ID, Action: "maintenance_completed", Payload: map[string]any{"work_order_number": item.Number, "type": item.Type}})
		}
	}
	return nil
}

// ---------- Inspection (PRD §12.4): Task type inspection + extension INS- ----------

type CreateInspectionInput struct {
	operations.CreateTaskInput
	InspectionType string `json:"inspection_type"` // engineering | safety | general
}

func (s *Service) CreateInspection(ctx context.Context, in CreateInspectionInput) (*operations.WorkItem, error) {
	p := authctx.Must(ctx)
	if in.InspectionType == "" {
		in.InspectionType = "engineering"
	}
	in.TaskType = "inspection"
	var out *operations.WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.Ops.CreateTaskTx(ctx, tx, in.CreateTaskInput)
		if err != nil {
			return err
		}
		var propID uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT property_id FROM tasks WHERE id = $1`, id).Scan(&propID)
		loc := property.PropertyTimezone(ctx, tx, propID)
		number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixInspection, time.Now(), loc)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inspections (task_id, organization_id, inspection_number, inspection_type) VALUES ($1,$2,$3,$4)`, id, p.OrganizationID, number, in.InspectionType); err != nil {
			return err
		}
		out, err = s.Ops.GetTx(ctx, tx, operations.ObjTask, id)
		return err
	})
	return out, err
}

// inspectionHook: saat inspection complete → result pass/fail dari checklist (Not OK → fail).
type inspectionHook struct{ s *Service }

func (h inspectionHook) BeforeComplete(context.Context, pgx.Tx, *operations.WorkItem, operations.TransitionInput) error {
	return nil
}
func (h inspectionHook) AfterTransition(ctx context.Context, tx pgx.Tx, item *operations.WorkItem, action string, from, to workflow.Status) error {
	if action != workflow.ActComplete {
		return nil
	}
	var notOK, total int
	_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(not_ok_items),0), COALESCE(sum(total_items),0) FROM checklist_runs WHERE object_type = 'task' AND object_id = $1`, item.ID).Scan(&notOK, &total)
	var findings int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM findings WHERE source_type = 'task' AND source_id = $1`, item.ID).Scan(&findings)
	result := "pass"
	if notOK > 0 || findings > 0 {
		result = "fail"
		if total > 0 && notOK < total/2 && findings == 0 {
			result = "partial"
		}
	}
	_, _ = tx.Exec(ctx, `UPDATE inspections SET result = $2, result_notes = $3 WHERE task_id = $1`, item.ID, result, item.CompletionNotes)
	// housekeeping inspection: propagate ke cleaning task
	_, _ = tx.Exec(ctx, `UPDATE housekeeping_inspections SET result = $2, result_notes = $3 WHERE task_id = $1`, item.ID, result, item.CompletionNotes)
	_, _ = tx.Exec(ctx, `UPDATE cleaning_tasks ct SET inspection_status = CASE WHEN $2 = 'pass' THEN 'passed' WHEN $3 > 0 THEN 'rework_required' ELSE 'failed' END
		FROM housekeeping_inspections hi WHERE hi.task_id = $1 AND ct.task_id = hi.cleaning_task_id`, item.ID, result, findings)
	if item.Asset.ID != nil {
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "asset", ObjectID: *item.Asset.ID, Action: "inspection_completed", Payload: map[string]any{"task_number": item.Number, "result": result}})
	}
	return nil
}
