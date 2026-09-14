package operations

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/property"
)

// ---------- SLA Policy (Naming Convention §30; TAD §5.12) ----------

type SLAPolicy struct {
	ID                uuid.UUID  `json:"id"`
	PropertyID        *uuid.UUID `json:"property_id"`
	ObjectType        string     `json:"object_type"`
	Priority          string     `json:"priority"`
	ResponseMinutes   *int       `json:"response_minutes"`
	ResolutionMinutes int        `json:"resolution_minutes"`
	RiskThresholdPct  int        `json:"risk_threshold_pct"`
	Calendar          string     `json:"calendar"`
	IsActive          bool       `json:"is_active"`
	Version           int        `json:"version"`
}

type SLAPolicyInput struct {
	PropertyID        *uuid.UUID `json:"property_id"`
	ObjectType        string     `json:"object_type"`
	Priority          string     `json:"priority"`
	ResponseMinutes   *int       `json:"response_minutes"`
	ResolutionMinutes int        `json:"resolution_minutes"`
	RiskThresholdPct  *int       `json:"risk_threshold_pct"`
	Calendar          string     `json:"calendar"`
	IsActive          *bool      `json:"is_active"`
}

func (s *Service) ListSLAPolicies(ctx context.Context, propertyID *uuid.UUID) ([]SLAPolicy, error) {
	var out []SLAPolicy
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, property_id, object_type, priority, response_minutes, resolution_minutes, risk_threshold_pct, calendar, is_active, version
			FROM sla_policies WHERE ($1::uuid IS NULL OR property_id IS NULL OR property_id = $1) ORDER BY property_id NULLS FIRST, object_type, CASE priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END`, propertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p SLAPolicy
			if err := rows.Scan(&p.ID, &p.PropertyID, &p.ObjectType, &p.Priority, &p.ResponseMinutes, &p.ResolutionMinutes, &p.RiskThresholdPct, &p.Calendar, &p.IsActive, &p.Version); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	if out == nil {
		out = []SLAPolicy{}
	}
	return out, err
}

func (s *Service) UpsertSLAPolicy(ctx context.Context, in SLAPolicyInput) (*SLAPolicy, error) {
	p := authctx.Must(ctx)
	if !Priorities[in.Priority] {
		return nil, apperr.Validation("priority tidak valid")
	}
	switch in.ObjectType {
	case ObjTask, ObjWorkOrder, ObjServiceRequest, ObjIncident:
	default:
		return nil, apperr.Validation("object_type tidak valid")
	}
	if in.ResolutionMinutes <= 0 {
		return nil, apperr.Validation("resolution_minutes harus > 0")
	}
	if in.Calendar == "" {
		in.Calendar = "always"
	}
	if in.Calendar != "always" && in.Calendar != "business_hours" {
		return nil, apperr.Validation("calendar harus always|business_hours")
	}
	risk := 75
	if in.RiskThresholdPct != nil {
		risk = *in.RiskThresholdPct
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	var out SLAPolicy
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// nonaktifkan policy lama dengan key sama, lalu insert baru (history tetap ada)
		if _, err := tx.Exec(ctx, `UPDATE sla_policies SET is_active = false WHERE organization_id = $1 AND COALESCE(property_id,'00000000-0000-0000-0000-000000000000'::uuid) = COALESCE($2,'00000000-0000-0000-0000-000000000000'::uuid) AND object_type = $3 AND priority = $4 AND is_active`,
			p.OrganizationID, in.PropertyID, in.ObjectType, in.Priority); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `INSERT INTO sla_policies (organization_id, property_id, object_type, priority, response_minutes, resolution_minutes, risk_threshold_pct, calendar, is_active, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10) RETURNING id, property_id, object_type, priority, response_minutes, resolution_minutes, risk_threshold_pct, calendar, is_active, version`,
			p.OrganizationID, in.PropertyID, in.ObjectType, in.Priority, in.ResponseMinutes, in.ResolutionMinutes, risk, in.Calendar, active, p.UserID).
			Scan(&out.ID, &out.PropertyID, &out.ObjectType, &out.Priority, &out.ResponseMinutes, &out.ResolutionMinutes, &out.RiskThresholdPct, &out.Calendar, &out.IsActive, &out.Version)
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "sla_policy", EntityID: &out.ID, EntityLabel: in.ObjectType + "/" + in.Priority, After: in})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// applySLATx: hitung response/resolution due dari policy (property-specific > org default) + kalender + timezone.
func (s *Service) applySLATx(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID, propertyID uuid.UUID, priority string, startedAt time.Time) error {
	p := authctx.Must(ctx)
	var policyID uuid.UUID
	var respMin *int
	var resMin, risk int
	var calendar string
	err := tx.QueryRow(ctx, `SELECT id, response_minutes, resolution_minutes, risk_threshold_pct, calendar FROM sla_policies
		WHERE organization_id = $1 AND object_type = $2 AND priority = $3 AND is_active AND (property_id = $4 OR property_id IS NULL)
		ORDER BY property_id NULLS LAST LIMIT 1`, p.OrganizationID, objectType, priority, propertyID).Scan(&policyID, &respMin, &resMin, &risk, &calendar)
	if err != nil {
		if db.IsNoRows(err) {
			return nil // tanpa policy → tanpa SLA (due_at manual saja)
		}
		return err
	}
	var propCal string
	_ = tx.QueryRow(ctx, `SELECT sla_calendar FROM properties WHERE location_id = $1`, propertyID).Scan(&propCal)
	if propCal == "business_hours" {
		calendar = "business_hours"
	}
	loc := property.PropertyTimezone(ctx, tx, propertyID)
	var hours []businessHour
	if calendar == "business_hours" {
		hours = loadBusinessHours(ctx, tx, propertyID)
	}
	resolutionDue := addWorkingMinutes(startedAt, resMin, loc, hours)
	var responseDue *time.Time
	if respMin != nil {
		t := addWorkingMinutes(startedAt, *respMin, loc, hours)
		responseDue = &t
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO sla_tracking (object_type, object_id, organization_id, policy_id, started_at, response_due_at, resolution_due_at, risk_threshold_pct)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (object_type, object_id) DO UPDATE SET policy_id = EXCLUDED.policy_id, response_due_at = EXCLUDED.response_due_at, resolution_due_at = EXCLUDED.resolution_due_at,
		  risk_threshold_pct = EXCLUDED.risk_threshold_pct, sla_risk_at = NULL, sla_breached_at = NULL, escalated_at = NULL`,
		objectType, id, p.OrganizationID, policyID, startedAt, responseDue, resolutionDue, risk)
	if err != nil {
		return err
	}
	// reset flag di object (priority berubah → hitung ulang)
	if t, e := tableFor(objectType); e == nil {
		_, _ = tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET sla_risk_at = NULL, sla_breached_at = NULL WHERE id = $1`, t.table), id)
	} else if objectType == ObjServiceRequest {
		_, _ = tx.Exec(ctx, `UPDATE service_requests SET sla_risk_at = NULL, sla_breached_at = NULL WHERE id = $1`, id)
	} else if objectType == ObjIncident {
		_, _ = tx.Exec(ctx, `UPDATE incidents SET sla_risk_at = NULL, sla_breached_at = NULL WHERE id = $1`, id)
	}
	return nil
}

// ApplySLA diekspor untuk modul lain (tenantservice, incidents).
func (s *Service) ApplySLA(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID, propertyID uuid.UUID, priority string, startedAt time.Time) error {
	return s.applySLATx(ctx, tx, objectType, id, propertyID, priority, startedAt)
}

type businessHour struct {
	weekday int
	open    time.Duration
	close   time.Duration
}

func loadBusinessHours(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID) []businessHour {
	rows, err := tx.Query(ctx, `SELECT weekday, open_time, close_time FROM property_business_hours WHERE property_id = $1`, propertyID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []businessHour
	for rows.Next() {
		var wd int
		var o, c time.Time
		if err := rows.Scan(&wd, &o, &c); err != nil {
			continue
		}
		out = append(out, businessHour{weekday: wd, open: time.Duration(o.Hour())*time.Hour + time.Duration(o.Minute())*time.Minute, close: time.Duration(c.Hour())*time.Hour + time.Duration(c.Minute())*time.Minute})
	}
	if len(out) == 0 {
		// default Senin–Jumat 08:00–17:00 (OD-006)
		for wd := 1; wd <= 5; wd++ {
			out = append(out, businessHour{weekday: wd, open: 8 * time.Hour, close: 17 * time.Hour})
		}
	}
	return out
}

// addWorkingMinutes: 24/7 bila hours nil; jika tidak, hanya menghitung menit dalam jam operasional (OD-006).
func addWorkingMinutes(start time.Time, minutes int, loc *time.Location, hours []businessHour) time.Time {
	if len(hours) == 0 {
		return start.Add(time.Duration(minutes) * time.Minute)
	}
	remaining := time.Duration(minutes) * time.Minute
	cur := start.In(loc)
	for guard := 0; guard < 400; guard++ { // maks ~1 tahun hari kerja
		var bh *businessHour
		for i := range hours {
			if hours[i].weekday == int(cur.Weekday()) {
				bh = &hours[i]
				break
			}
		}
		dayStart := time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, loc)
		if bh != nil {
			open := dayStart.Add(bh.open)
			closeT := dayStart.Add(bh.close)
			if cur.Before(open) {
				cur = open
			}
			if cur.Before(closeT) {
				avail := closeT.Sub(cur)
				if avail >= remaining {
					return cur.Add(remaining).UTC()
				}
				remaining -= avail
			}
		}
		cur = dayStart.Add(24 * time.Hour)
	}
	return cur.UTC()
}

// ---------- Sweeps (worker) ----------

// OverdueSweep: set is_overdue untuk task/WO yang lewat due_at & belum selesai; emit event sekali (TAD §5.11).
func (s *Service) OverdueSweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	total := 0
	sys := authctx.System(orgID)
	ctx = authctx.With(ctx, sys)
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		for _, ot := range []string{ObjTask, ObjWorkOrder} {
			t, _ := tableFor(ot)
			rows, err := tx.Query(ctx, fmt.Sprintf(`UPDATE %s SET is_overdue = true WHERE organization_id = $1 AND NOT is_overdue AND due_at < now() AND status NOT IN ('completed','closed','cancelled')
				RETURNING id, %s, property_id, assignee_user_id, assignee_team_id, %s`, t.table, t.numberCol, t.typeCol), orgID)
			if err != nil {
				return err
			}
			type row struct {
				id     uuid.UUID
				number string
				prop   uuid.UUID
				user   *uuid.UUID
				team   *uuid.UUID
				typ    string
			}
			var rs []row
			for rows.Next() {
				var r row
				if err := rows.Scan(&r.id, &r.number, &r.prop, &r.user, &r.team, &r.typ); err != nil {
					rows.Close()
					return err
				}
				rs = append(rs, r)
			}
			rows.Close()
			for _, r := range rs {
				total++
				_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ot, ObjectID: r.id, Action: audit.ActOverdue})
				if s.Jobs != nil {
					evType := ot + ".overdue"
					if ot == ObjTask && r.typ == "patrol" {
						evType = events.PatrolOverdue
					}
					_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: evType, OrganizationID: orgID, PropertyID: &r.prop, ObjectType: ot, ObjectID: r.id, ObjectLabel: r.number,
						Payload: map[string]any{"assignee_user_id": r.user, "assignee_team_id": r.team, "type": r.typ}})
					if evType != ot+".overdue" {
						_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: ot + ".overdue", OrganizationID: orgID, PropertyID: &r.prop, ObjectType: ot, ObjectID: r.id, ObjectLabel: r.number,
							Payload: map[string]any{"assignee_user_id": r.user, "assignee_team_id": r.team, "type": r.typ}})
					}
					_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(orgID, ot, r.id))
				}
			}
		}
		// maintenance_schedules: status overdue (Naming Convention §26)
		mrows, err := tx.Query(ctx, `UPDATE maintenance_schedules SET status = 'overdue' WHERE organization_id = $1 AND status IN ('scheduled','due') AND due_at < now() RETURNING id, property_id, asset_id`, orgID)
		if err != nil {
			return err
		}
		type mr struct{ id, prop, asset uuid.UUID }
		var ms []mr
		for mrows.Next() {
			var m mr
			if err := mrows.Scan(&m.id, &m.prop, &m.asset); err != nil {
				mrows.Close()
				return err
			}
			ms = append(ms, m)
		}
		mrows.Close()
		for _, m := range ms {
			total++
			if s.Jobs != nil {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.MaintenanceScheduleOverdue, OrganizationID: orgID, PropertyID: &m.prop, ObjectType: ObjMaintenanceSchedule, ObjectID: m.id, Payload: map[string]any{"asset_id": m.asset}})
			}
		}
		return nil
	})
	return total, err
}

// SLASweep: tandai risk (elapsed ≥ threshold) & breach (lewat due) untuk task/WO/SR/incident; escalation ke supervisor.
func (s *Service) SLASweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	total := 0
	ctx = authctx.With(ctx, authctx.System(orgID))
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		type row struct {
			ot   string
			id   uuid.UUID
			kind string // risk | breach
		}
		rows, err := tx.Query(ctx, `
			WITH open AS (
			  SELECT st.object_type, st.object_id, st.started_at, st.resolution_due_at, st.risk_threshold_pct, st.sla_risk_at, st.sla_breached_at, st.paused_at, st.paused_minutes
			  FROM sla_tracking st WHERE st.organization_id = $1 AND st.resolved_at IS NULL AND st.resolution_due_at IS NOT NULL AND st.paused_at IS NULL
			), marked AS (
			  UPDATE sla_tracking st SET
			    sla_risk_at = CASE WHEN st.sla_risk_at IS NULL AND now() >= o.started_at + (o.resolution_due_at - o.started_at) * (o.risk_threshold_pct / 100.0) + (o.paused_minutes || ' minutes')::interval THEN now() ELSE st.sla_risk_at END,
			    sla_breached_at = CASE WHEN st.sla_breached_at IS NULL AND now() >= o.resolution_due_at + (o.paused_minutes || ' minutes')::interval THEN now() ELSE st.sla_breached_at END
			  FROM open o WHERE st.object_type = o.object_type AND st.object_id = o.object_id
			  RETURNING st.object_type, st.object_id,
			    (o.sla_risk_at IS NULL AND st.sla_risk_at IS NOT NULL) AS new_risk,
			    (o.sla_breached_at IS NULL AND st.sla_breached_at IS NOT NULL) AS new_breach
			)
			SELECT object_type, object_id, new_risk, new_breach FROM marked WHERE new_risk OR new_breach`, orgID)
		if err != nil {
			return err
		}
		var rs []row
		for rows.Next() {
			var ot string
			var id uuid.UUID
			var risk, breach bool
			if err := rows.Scan(&ot, &id, &risk, &breach); err != nil {
				rows.Close()
				return err
			}
			if breach {
				rs = append(rs, row{ot, id, "breach"})
			} else if risk {
				rs = append(rs, row{ot, id, "risk"})
			}
		}
		rows.Close()
		for _, r := range rs {
			total++
			table := map[string]string{ObjTask: "tasks", ObjWorkOrder: "work_orders", ObjServiceRequest: "service_requests", ObjIncident: "incidents"}[r.ot]
			if table == "" {
				continue
			}
			numberCol := map[string]string{ObjTask: "task_number", ObjWorkOrder: "work_order_number", ObjServiceRequest: "request_number", ObjIncident: "incident_number"}[r.ot]
			var number string
			var prop uuid.UUID
			var user, team *uuid.UUID
			var status string
			_ = tx.QueryRow(ctx, fmt.Sprintf(`SELECT %s, property_id, assignee_user_id, assignee_team_id, status FROM %s WHERE id = $1`, numberCol, table), r.id).Scan(&number, &prop, &user, &team, &status)
			if status == "closed" || status == "cancelled" || status == "completed" || status == "resolved" {
				continue
			}
			col := "sla_risk_at"
			act := audit.ActSLARisk
			evSuffix := ".sla_risk"
			if r.kind == "breach" {
				col = "sla_breached_at"
				act = audit.ActSLABreached
				evSuffix = ".sla_breached"
				_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET escalated_at = COALESCE(escalated_at, now()) WHERE object_type = $1 AND object_id = $2`, r.ot, r.id)
			}
			_, _ = tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s = now() WHERE id = $1 AND %s IS NULL`, table, col, col), r.id)
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: r.ot, ObjectID: r.id, Action: act})
			if s.Jobs != nil {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: r.ot + evSuffix, OrganizationID: orgID, PropertyID: &prop, ObjectType: r.ot, ObjectID: r.id, ObjectLabel: number,
					Payload: map[string]any{"assignee_user_id": user, "assignee_team_id": team, "escalate": r.kind == "breach"}})
				_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(orgID, r.ot, r.id))
			}
		}
		return nil
	})
	return total, err
}

// DueSoonNotify: event due_soon untuk task/WO yang due dalam window (dedup di notification rules).
func (s *Service) DueSoonNotify(ctx context.Context, orgID uuid.UUID, window time.Duration) (int, error) {
	total := 0
	ctx = authctx.With(ctx, authctx.System(orgID))
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		for _, ot := range []string{ObjTask, ObjWorkOrder} {
			t, _ := tableFor(ot)
			rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT id, %s, property_id, assignee_user_id, assignee_team_id FROM %s
				WHERE organization_id = $1 AND status IN ('scheduled','assigned','in_progress','on_hold') AND due_at BETWEEN now() AND now() + $2::interval AND NOT is_overdue`, t.numberCol, t.table), orgID, fmt.Sprintf("%d minutes", int(window.Minutes())))
			if err != nil {
				return err
			}
			for rows.Next() {
				var id, prop uuid.UUID
				var number string
				var user, team *uuid.UUID
				if err := rows.Scan(&id, &number, &prop, &user, &team); err != nil {
					rows.Close()
					return err
				}
				total++
				if s.Jobs != nil {
					_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: ot + ".due_soon", OrganizationID: orgID, PropertyID: &prop, ObjectType: ot, ObjectID: id, ObjectLabel: number,
						Payload: map[string]any{"assignee_user_id": user, "assignee_team_id": team}})
				}
			}
			rows.Close()
		}
		return nil
	})
	return total, err
}

func searchIndexArgs(orgID uuid.UUID, objectType string, id uuid.UUID) jobs.SearchIndexArgs {
	return jobs.SearchIndexArgs{OrganizationID: orgID, ObjectType: objectType, ObjectID: id}
}
