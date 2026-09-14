// Package overview: Situation → Priority → Action (PRD §19; TAD §5.16). Satu endpoint read-only per panel,
// cache in-memory 30 detik per (org, property, panel, user-perm-hash), CTA dari allowed_actions server.
package overview

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/property"
)

type Service struct {
	DB       *db.DB
	Ops      *operations.Service
	CacheTTL time.Duration
	cache    sync.Map // key → cacheEntry
}

type cacheEntry struct {
	at  time.Time
	val any
}

func (s *Service) cached(key string, fn func() (any, error)) (any, error) {
	if s.CacheTTL > 0 {
		if v, ok := s.cache.Load(key); ok {
			ce := v.(cacheEntry)
			if time.Since(ce.at) < s.CacheTTL {
				return ce.val, nil
			}
		}
	}
	val, err := fn()
	if err != nil {
		return nil, err
	}
	if s.CacheTTL > 0 {
		s.cache.Store(key, cacheEntry{at: time.Now(), val: val})
	}
	return val, nil
}

// scope: property filter + izin.
func (s *Service) scope(ctx context.Context, propertyID *uuid.UUID) ([]uuid.UUID, bool, error) {
	p := authctx.Must(ctx)
	if propertyID != nil {
		if !p.HasOnProperty("overview.dashboard.view", *propertyID) {
			return nil, false, apperr.Forbidden("")
		}
		return []uuid.UUID{*propertyID}, false, nil
	}
	ids, all := p.PropertyIDsFor("overview.dashboard.view")
	return ids, all, nil
}

func propWhere(col string, ids []uuid.UUID, all bool, args *[]any) string {
	if all {
		return ""
	}
	*args = append(*args, ids)
	return fmt.Sprintf(" AND %s = ANY($%d::uuid[])", col, len(*args))
}

func cacheKey(ctx context.Context, panel string, propertyID *uuid.UUID, extra string) string {
	p := authctx.Must(ctx)
	pid := "all"
	if propertyID != nil {
		pid = propertyID.String()
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s", p.OrganizationID, p.UserID, panel, pid, extra)
}

// ---------- 19.1 Today ----------

type Counter struct {
	Value     int            `json:"value"`
	Breakdown map[string]int `json:"breakdown,omitempty"`
	Link      string         `json:"link"`
}

type Today struct {
	OpenWorkOrders Counter   `json:"open_work_orders"`
	Overdue        Counter   `json:"overdue"`
	SLARisk        Counter   `json:"sla_risk"`
	PMDue          Counter   `json:"pm_due"`
	Incidents      Counter   `json:"incidents"`
	TenantRequests Counter   `json:"tenant_requests"`
	GeneratedAt    time.Time `json:"generated_at"`
}

func (s *Service) Today(ctx context.Context, propertyID *uuid.UUID) (*Today, error) {
	ids, all, err := s.scope(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	v, err := s.cached(cacheKey(ctx, "today", propertyID, ""), func() (any, error) {
		var out Today
		err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			args := []any{}
			pw := propWhere("property_id", ids, all, &args)
			q := func(sql string) int {
				var n int
				_ = tx.QueryRow(ctx, sql, args...).Scan(&n)
				return n
			}
			out.OpenWorkOrders = Counter{Value: q(`SELECT count(*) FROM work_orders WHERE status NOT IN ('closed','cancelled')` + pw), Link: "/operations/work-orders?open=true"}
			out.OpenWorkOrders.Breakdown = map[string]int{
				"overdue":  q(`SELECT count(*) FROM work_orders WHERE status NOT IN ('completed','closed','cancelled') AND (is_overdue OR due_at < now())` + pw),
				"sla_risk": q(`SELECT count(*) FROM work_orders WHERE status NOT IN ('closed','cancelled') AND sla_risk_at IS NOT NULL` + pw),
			}
			ovTask := q(`SELECT count(*) FROM tasks WHERE status NOT IN ('completed','closed','cancelled') AND (is_overdue OR due_at < now())` + pw)
			out.Overdue = Counter{Value: out.OpenWorkOrders.Breakdown["overdue"] + ovTask, Breakdown: map[string]int{"work_orders": out.OpenWorkOrders.Breakdown["overdue"], "tasks": ovTask}, Link: "/operations/work-orders?overdue=true"}
			riskTask := q(`SELECT count(*) FROM tasks WHERE status NOT IN ('closed','cancelled') AND sla_risk_at IS NOT NULL` + pw)
			riskSR := q(`SELECT count(*) FROM service_requests WHERE status NOT IN ('resolved','closed','cancelled') AND sla_risk_at IS NOT NULL` + pw)
			out.SLARisk = Counter{Value: out.OpenWorkOrders.Breakdown["sla_risk"] + riskTask + riskSR, Breakdown: map[string]int{"work_orders": out.OpenWorkOrders.Breakdown["sla_risk"], "tasks": riskTask, "service_requests": riskSR}, Link: "/operations/work-orders?sla_risk=true"}
			out.PMDue = Counter{Value: q(`SELECT count(*) FROM maintenance_schedules WHERE status IN ('scheduled','due','overdue') AND due_at <= now() + interval '7 days'` + pw),
				Breakdown: map[string]int{"overdue": q(`SELECT count(*) FROM maintenance_schedules WHERE status = 'overdue'` + pw), "today": q(`SELECT count(*) FROM maintenance_schedules ms JOIN properties pr ON pr.location_id = ms.property_id WHERE ms.status IN ('scheduled','due') AND ms.due_date = (now() AT TIME ZONE pr.timezone)::date` + strings.ReplaceAll(pw, "property_id", "ms.property_id"))},
				Link:      "/engineering/preventive-maintenance?due_within_days=7"}
			out.Incidents = Counter{Value: q(`SELECT count(*) FROM incidents WHERE status NOT IN ('closed','cancelled')` + pw),
				Breakdown: map[string]int{"critical": q(`SELECT count(*) FROM incidents WHERE status NOT IN ('closed','cancelled') AND severity = 'critical'` + pw), "new": q(`SELECT count(*) FROM incidents WHERE status = 'new'` + pw)},
				Link:      "/operations/incidents?open=true"}
			out.TenantRequests = Counter{Value: q(`SELECT count(*) FROM service_requests WHERE status NOT IN ('closed','cancelled')` + pw),
				Breakdown: map[string]int{"new_today": q(`SELECT count(*) FROM service_requests sr JOIN properties pr ON pr.location_id = sr.property_id WHERE (sr.created_at AT TIME ZONE pr.timezone)::date = (now() AT TIME ZONE pr.timezone)::date` + strings.ReplaceAll(pw, "property_id", "sr.property_id")), "sla_risk": riskSR, "new": q(`SELECT count(*) FROM service_requests WHERE status = 'new'` + pw)},
				Link:      "/operations/service-requests?open=true"}
			out.GeneratedAt = time.Now().UTC()
			return nil
		})
		return &out, err
	})
	if err != nil {
		return nil, err
	}
	return v.(*Today), nil
}

// ---------- 19.2 Attention Required ----------

type AttentionItem struct {
	Category       string     `json:"category"` // sla_breach | sla_risk | maintenance_overdue | patrol_overdue | unresolved_finding | critical_incident | sync_conflict | overdue
	Severity       string     `json:"severity"` // critical | warning
	ObjectType     string     `json:"object_type"`
	ObjectID       uuid.UUID  `json:"object_id"`
	Label          string     `json:"label"`
	Title          string     `json:"title"`
	Status         string     `json:"status"`
	Priority       *string    `json:"priority"`
	LocationPath   *string    `json:"location_path"`
	DueAt          *time.Time `json:"due_at"`
	AgeMinutes     int        `json:"age_minutes"` // umur kondisi (sejak breach/overdue/created)
	Since          time.Time  `json:"since"`
	AssigneeName   *string    `json:"assignee_name"`
	AllowedActions []string   `json:"allowed_actions"`
	DeepLink       string     `json:"deep_link"`
}

func (s *Service) AttentionRequired(ctx context.Context, propertyID *uuid.UUID, domain string, limit int) ([]AttentionItem, int, error) {
	ids, all, err := s.scope(ctx, propertyID)
	if err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 10
	}
	p := authctx.Must(ctx)
	type res struct {
		items []AttentionItem
		total int
	}
	v, err := s.cached(cacheKey(ctx, "attention", propertyID, domain), func() (any, error) {
		var items []AttentionItem
		err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			args := []any{}
			pw := propWhere("x.property_id", ids, all, &args)
			domainFilter := ""
			switch domain {
			case "engineering":
				domainFilter = " AND x.object_type IN ('work_order','maintenance_schedule') OR (x.object_type = 'task' AND x.sub_type IN ('general','inspection','routine_maintenance'))"
			case "security":
				domainFilter = " AND (x.object_type = 'incident' OR (x.object_type = 'task' AND x.sub_type = 'patrol'))"
			case "housekeeping":
				domainFilter = " AND (x.object_type = 'task' AND x.sub_type = 'cleaning')"
			}
			rows, err := tx.Query(ctx, `
				WITH x AS (
				  SELECT 'work_order' AS object_type, w.id, w.property_id, w.work_order_number AS label, w.title, w.status, w.priority, w.location_id, w.due_at, w.assignee_user_id, w.work_order_type AS sub_type,
				         w.sla_breached_at, w.sla_risk_at, w.is_overdue OR (w.due_at < now()) AS overdue, w.created_at
				  FROM work_orders w WHERE w.status NOT IN ('completed','closed','cancelled')
				  UNION ALL
				  SELECT 'task', t.id, t.property_id, t.task_number, t.title, t.status, t.priority, t.location_id, t.due_at, t.assignee_user_id, t.task_type,
				         t.sla_breached_at, t.sla_risk_at, t.is_overdue OR (t.due_at < now()), t.created_at
				  FROM tasks t WHERE t.status NOT IN ('completed','closed','cancelled')
				  UNION ALL
				  SELECT 'service_request', sr.id, sr.property_id, sr.request_number, sr.title, sr.status, sr.priority, sr.location_id, NULL, sr.assignee_user_id, sr.category_code,
				         sr.sla_breached_at, sr.sla_risk_at, false, sr.created_at
				  FROM service_requests sr WHERE sr.status NOT IN ('resolved','closed','cancelled')
				  UNION ALL
				  SELECT 'incident', i.id, i.property_id, i.incident_number, i.title, i.status, i.severity, i.location_id, NULL, i.assignee_user_id, i.category,
				         i.sla_breached_at, i.sla_risk_at, false, i.reported_at
				  FROM incidents i WHERE i.status NOT IN ('resolved','closed','cancelled')
				  UNION ALL
				  SELECT 'finding', f.id, f.property_id, f.finding_number, f.title, f.status, f.severity, f.location_id, NULL, NULL, f.finding_type,
				         NULL, NULL, false, f.reported_at
				  FROM findings f WHERE f.status IN ('open','in_progress')
				  UNION ALL
				  SELECT 'maintenance_schedule', ms.id, ms.property_id, mp.plan_code, mp.name || ' — ' || a.name, ms.status, mp.default_priority, a.location_id, ms.due_at, NULL, 'maintenance',
				         NULL, NULL, true, ms.due_at
				  FROM maintenance_schedules ms JOIN maintenance_plans mp ON mp.id = ms.plan_id JOIN assets a ON a.id = ms.asset_id WHERE ms.status = 'overdue'
				)
				SELECT x.object_type, x.id, x.label, x.title, x.status, x.priority, x.location_id, x.due_at, u.full_name, x.sub_type,
				  CASE
				    WHEN x.sla_breached_at IS NOT NULL THEN 'sla_breach'
				    WHEN x.object_type = 'maintenance_schedule' THEN 'maintenance_overdue'
				    WHEN x.object_type = 'task' AND x.sub_type = 'patrol' AND x.overdue THEN 'patrol_overdue'
				    WHEN x.object_type = 'incident' AND x.priority = 'critical' THEN 'critical_incident'
				    WHEN x.overdue THEN 'overdue'
				    WHEN x.sla_risk_at IS NOT NULL THEN 'sla_risk'
				    WHEN x.object_type = 'finding' THEN 'unresolved_finding'
				    WHEN x.object_type = 'incident' THEN 'incident'
				    ELSE NULL END AS category,
				  COALESCE(x.sla_breached_at, CASE WHEN x.overdue THEN x.due_at END, x.sla_risk_at, x.created_at) AS since
				FROM x LEFT JOIN users u ON u.id = x.assignee_user_id
				WHERE (x.sla_breached_at IS NOT NULL OR x.sla_risk_at IS NOT NULL OR x.overdue OR x.object_type IN ('finding','maintenance_schedule') OR (x.object_type = 'incident' AND x.priority IN ('critical','high')))`+pw+domainFilter+`
				ORDER BY 1 LIMIT 500`, args...)
			if err != nil {
				return err
			}
			var locs []*uuid.UUID
			for rows.Next() {
				var it AttentionItem
				var cat *string
				var locID *uuid.UUID
				var subType string
				if err := rows.Scan(&it.ObjectType, &it.ObjectID, &it.Label, &it.Title, &it.Status, &it.Priority, &locID, &it.DueAt, &it.AssigneeName, &subType, &cat, &it.Since); err != nil {
					return err
				}
				if cat == nil {
					continue
				}
				it.Category = *cat
				switch it.Category {
				case "sla_breach", "maintenance_overdue", "patrol_overdue", "critical_incident", "overdue":
					it.Severity = "critical"
				default:
					it.Severity = "warning"
				}
				it.AgeMinutes = int(time.Since(it.Since).Minutes())
				it.DeepLink = deepLink(it.ObjectType, it.ObjectID)
				items = append(items, it)
				locs = append(locs, locID)
			}
			rows.Close()
			for i := range items {
				if locs[i] != nil {
					pt := property.LocationPathText(ctx, tx, *locs[i])
					items[i].LocationPath = &pt
				}
				items[i].AllowedActions = s.allowedActions(ctx, tx, p, items[i])
			}
			// sync conflicts (OD-004) → warning
			if p.Has("sync.conflicts.view") {
				crows, err := tx.Query(ctx, `SELECT sm.client_mutation_id, sm.object_type, sm.object_id, sm.action, sm.received_at, u.full_name, sm.reason_code
					FROM sync_mutations sm JOIN users u ON u.id = sm.user_id WHERE sm.result = 'conflict' AND sm.acknowledged_at IS NULL ORDER BY sm.received_at DESC LIMIT 50`)
				if err != nil {
					return err
				}
				type crow struct {
					mid    uuid.UUID
					it     AttentionItem
					action string
					worker string
					reason *string
				}
				var cl []crow
				for crows.Next() {
					var c crow
					if err := crows.Scan(&c.mid, &c.it.ObjectType, &c.it.ObjectID, &c.action, &c.it.Since, &c.worker, &c.reason); err != nil {
						crows.Close()
						return err
					}
					cl = append(cl, c)
				}
				crows.Close()
				for _, c := range cl {
					it := c.it
					it.Category, it.Severity = "sync_conflict", "warning"
					it.Label, it.Title, it.Status = operations.DescribeObject(ctx, tx, it.ObjectType, it.ObjectID)
					it.Title = fmt.Sprintf("%s · %s ditolak (%s)", c.worker, c.action, derefStr(c.reason))
					it.AgeMinutes = int(time.Since(it.Since).Minutes())
					it.AllowedActions = []string{"view", "acknowledge_conflict"}
					it.DeepLink = "/sync/conflicts/" + c.mid.String()
					items = append(items, it)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		// urut: severity → umur (server-side, DS §4.4)
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Severity != items[j].Severity {
				return items[i].Severity == "critical"
			}
			return items[i].AgeMinutes > items[j].AgeMinutes
		})
		return res{items: items, total: len(items)}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	r := v.(res)
	items := r.items
	if len(items) > limit {
		items = items[:limit]
	}
	return items, r.total, nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func deepLink(objectType string, id uuid.UUID) string {
	route := map[string]string{"task": "/operations/tasks/", "work_order": "/operations/work-orders/", "service_request": "/operations/service-requests/", "incident": "/operations/incidents/", "finding": "/findings/", "maintenance_schedule": "/engineering/preventive-maintenance/"}[objectType]
	return route + id.String()
}

// allowedActions: CTA minimal View / Assign / Close / Resolve (PRD §19.2) berdasarkan permission user.
func (s *Service) allowedActions(ctx context.Context, tx pgx.Tx, p *authctx.Principal, it AttentionItem) []string {
	out := []string{"view"}
	var pid uuid.UUID
	table := map[string]string{"task": "tasks", "work_order": "work_orders", "service_request": "service_requests", "incident": "incidents", "finding": "findings", "maintenance_schedule": "maintenance_schedules"}[it.ObjectType]
	if table == "" {
		return out
	}
	_ = tx.QueryRow(ctx, `SELECT property_id FROM `+table+` WHERE id = $1`, it.ObjectID).Scan(&pid)
	permObj := map[string]string{"task": "operations.tasks", "work_order": "operations.work_orders", "service_request": "tenant.service_requests", "incident": "operations.incidents", "finding": "operations.findings"}[it.ObjectType]
	switch it.ObjectType {
	case "task", "work_order", "service_request", "incident":
		if p.HasOnProperty(permObj+".assign", pid) && it.Status != "completed" && it.Status != "resolved" {
			out = append(out, "assign")
		}
		if (it.Status == "completed" || it.Status == "resolved") && p.HasOnProperty(permObj+".close", pid) {
			out = append(out, "close")
		}
		if (it.ObjectType == "service_request" || it.ObjectType == "incident") && it.Status != "resolved" && p.HasOnProperty(permObj+".resolve", pid) {
			out = append(out, "resolve")
		}
	case "finding":
		if p.HasOnProperty("operations.findings.resolve", pid) {
			out = append(out, "resolve")
		}
		if p.HasOnProperty("operations.work_orders.create", pid) {
			out = append(out, "create_work_order")
		}
	case "maintenance_schedule":
		if p.HasOnProperty("engineering.maintenance_schedules.skip", pid) {
			out = append(out, "skip")
		}
	}
	return out
}

// ---------- 19.3 Today's Operations ----------

type TodaysOperations struct {
	Domain  string                `json:"domain"`
	Items   []operations.WorkItem `json:"items"`
	Summary map[string]int        `json:"summary"` // by status
}

func (s *Service) TodaysOperations(ctx context.Context, propertyID *uuid.UUID, domain string) ([]TodaysOperations, error) {
	if _, _, err := s.scope(ctx, propertyID); err != nil {
		return nil, err
	}
	domains := []string{"engineering", "security", "housekeeping"}
	if domain != "" {
		domains = []string{domain}
	}
	today := time.Now()
	var out []TodaysOperations
	for _, d := range domains {
		panel := TodaysOperations{Domain: d, Items: []operations.WorkItem{}, Summary: map[string]int{}}
		page := httpx.Page{Limit: 100}
		switch d {
		case "engineering":
			wos, _, err := s.Ops.List(ctx, operations.ObjWorkOrder, operations.ListFilter{PropertyID: propertyID, ScheduledOn: &today, Sort: "due_at"}, page)
			if err != nil {
				return nil, err
			}
			panel.Items = append(panel.Items, wos...)
			tasks, _, err := s.Ops.List(ctx, operations.ObjTask, operations.ListFilter{PropertyID: propertyID, ScheduledOn: &today, Types: []string{"general", "inspection", "routine_maintenance"}, Sort: "due_at"}, page)
			if err != nil {
				return nil, err
			}
			panel.Items = append(panel.Items, tasks...)
		case "security":
			tasks, _, err := s.Ops.List(ctx, operations.ObjTask, operations.ListFilter{PropertyID: propertyID, ScheduledOn: &today, Types: []string{"patrol"}, Sort: "due_at"}, page)
			if err != nil {
				return nil, err
			}
			panel.Items = tasks
		case "housekeeping":
			tasks, _, err := s.Ops.List(ctx, operations.ObjTask, operations.ListFilter{PropertyID: propertyID, ScheduledOn: &today, Types: []string{"cleaning"}, Sort: "due_at"}, page)
			if err != nil {
				return nil, err
			}
			panel.Items = tasks
		}
		for _, it := range panel.Items {
			panel.Summary[string(it.Status)]++
			if it.IsOverdue {
				panel.Summary["overdue"]++
			}
		}
		panel.Summary["total"] = len(panel.Items)
		out = append(out, panel)
	}
	return out, nil
}

// ---------- 19.4 Team Workload (Should) ----------

type WorkloadRow struct {
	TeamID         *uuid.UUID `json:"team_id"`
	TeamName       string     `json:"team_name"`
	Domain         string     `json:"domain"`
	UserID         *uuid.UUID `json:"user_id"`
	AssigneeName   string     `json:"assignee_name"`
	OpenTasks      int        `json:"open_tasks"`
	OpenWorkOrders int        `json:"open_work_orders"`
	Overdue        int        `json:"overdue"`
	InProgress     int        `json:"in_progress"`
}

func (s *Service) TeamWorkload(ctx context.Context, propertyID *uuid.UUID) ([]WorkloadRow, error) {
	ids, all, err := s.scope(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	v, err := s.cached(cacheKey(ctx, "workload", propertyID, ""), func() (any, error) {
		var out []WorkloadRow
		err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			args := []any{}
			pw := propWhere("w.property_id", ids, all, &args)
			rows, err := tx.Query(ctx, `
				WITH w AS (
				  SELECT 'task' AS kind, property_id, assignee_user_id, assignee_team_id, status, (is_overdue OR due_at < now()) AS overdue FROM tasks WHERE status NOT IN ('completed','closed','cancelled')
				  UNION ALL
				  SELECT 'work_order', property_id, assignee_user_id, assignee_team_id, status, (is_overdue OR due_at < now()) FROM work_orders WHERE status NOT IN ('completed','closed','cancelled')
				), members AS (
				  SELECT tm.team_id, tm.user_id FROM team_members tm
				)
				SELECT t.id, t.name, t.domain, u.id, COALESCE(u.full_name, '(belum ditugaskan ke orang)'),
				  count(*) FILTER (WHERE w.kind = 'task'), count(*) FILTER (WHERE w.kind = 'work_order'), count(*) FILTER (WHERE w.overdue), count(*) FILTER (WHERE w.status = 'in_progress')
				FROM w
				LEFT JOIN users u ON u.id = w.assignee_user_id
				LEFT JOIN teams t ON t.id = COALESCE(w.assignee_team_id, (SELECT m.team_id FROM members m JOIN teams tt ON tt.id = m.team_id WHERE m.user_id = w.assignee_user_id AND (tt.property_id IS NULL OR tt.property_id = w.property_id) ORDER BY tt.property_id NULLS LAST LIMIT 1))
				WHERE (w.assignee_user_id IS NOT NULL OR w.assignee_team_id IS NOT NULL)`+pw+`
				GROUP BY t.id, t.name, t.domain, u.id, u.full_name
				ORDER BY t.name NULLS LAST, u.full_name NULLS LAST`, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var r WorkloadRow
				var tname, tdomain *string
				if err := rows.Scan(&r.TeamID, &tname, &tdomain, &r.UserID, &r.AssigneeName, &r.OpenTasks, &r.OpenWorkOrders, &r.Overdue, &r.InProgress); err != nil {
					return err
				}
				r.TeamName, r.Domain = derefStr(tname), derefStr(tdomain)
				if r.TeamName == "" {
					r.TeamName = "(tanpa team)"
				}
				out = append(out, r)
			}
			return rows.Err()
		})
		if out == nil {
			out = []WorkloadRow{}
		}
		return out, err
	})
	if err != nil {
		return nil, err
	}
	return v.([]WorkloadRow), nil
}

// ---------- 19.7 Building State per Building/Tower ----------

type BuildingState struct {
	LocationID   uuid.UUID `json:"location_id"`
	LocationType string    `json:"location_type"`
	Name         string    `json:"name"`
	PathText     string    `json:"path_text"`
	Open         int       `json:"open"`
	Overdue      int       `json:"overdue"`
	SLARisk      int       `json:"sla_risk"`
	Incidents    int       `json:"incidents"`
}

func (s *Service) BuildingState(ctx context.Context, propertyID *uuid.UUID) ([]BuildingState, error) {
	ids, all, err := s.scope(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	v, err := s.cached(cacheKey(ctx, "building_state", propertyID, ""), func() (any, error) {
		var out []BuildingState
		err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			args := []any{}
			pw := propWhere("b.property_id", ids, all, &args)
			rows, err := tx.Query(ctx, `
				WITH b AS (SELECT id, property_id, location_type, name, path FROM locations WHERE deleted_at IS NULL AND location_type IN ('building','tower')`+strings.ReplaceAll(pw, "b.property_id", "property_id")+`),
				x AS (
				  SELECT location_id, (is_overdue OR due_at < now()) AS overdue, sla_risk_at IS NOT NULL OR sla_breached_at IS NOT NULL AS risk, false AS incident FROM work_orders WHERE status NOT IN ('completed','closed','cancelled')
				  UNION ALL SELECT location_id, (is_overdue OR due_at < now()), sla_risk_at IS NOT NULL OR sla_breached_at IS NOT NULL, false FROM tasks WHERE status NOT IN ('completed','closed','cancelled')
				  UNION ALL SELECT location_id, false, sla_risk_at IS NOT NULL OR sla_breached_at IS NOT NULL, false FROM service_requests WHERE status NOT IN ('resolved','closed','cancelled')
				  UNION ALL SELECT location_id, false, false, true FROM incidents WHERE status NOT IN ('closed','cancelled')
				)
				SELECT b.id, b.location_type, b.name,
				  count(x.*) FILTER (WHERE NOT x.incident), count(x.*) FILTER (WHERE x.overdue), count(x.*) FILTER (WHERE x.risk), count(x.*) FILTER (WHERE x.incident)
				FROM b LEFT JOIN locations l ON l.path <@ b.path LEFT JOIN x ON x.location_id = l.id
				GROUP BY b.id, b.location_type, b.name, b.path ORDER BY b.path`, args...)
			if err != nil {
				return err
			}
			var idsOut []uuid.UUID
			for rows.Next() {
				var r BuildingState
				if err := rows.Scan(&r.LocationID, &r.LocationType, &r.Name, &r.Open, &r.Overdue, &r.SLARisk, &r.Incidents); err != nil {
					rows.Close()
					return err
				}
				out = append(out, r)
				idsOut = append(idsOut, r.LocationID)
			}
			rows.Close()
			// tower di bawah building: hindari double count — tampilkan keduanya (building total, tower detail)
			for i := range out {
				out[i].PathText = property.LocationPathText(ctx, tx, idsOut[i])
			}
			return nil
		})
		if out == nil {
			out = []BuildingState{}
		}
		return out, err
	})
	if err != nil {
		return nil, err
	}
	return v.([]BuildingState), nil
}

// ---------- 19.6 Tenant Requests panel ----------

type TenantRequestsPanel struct {
	NewToday int        `json:"new_today"`
	Open     int        `json:"open"`
	SLARisk  int        `json:"sla_risk"`
	Recent   []RecentSR `json:"recent"`
}

type RecentSR struct {
	ID            uuid.UUID `json:"id"`
	RequestNumber string    `json:"request_number"`
	Title         string    `json:"title"`
	Status        string    `json:"status"`
	Priority      string    `json:"priority"`
	TenantName    *string   `json:"tenant_name"`
	CreatedAt     time.Time `json:"created_at"`
	SLARisk       bool      `json:"sla_risk"`
}

func (s *Service) TenantRequests(ctx context.Context, propertyID *uuid.UUID) (*TenantRequestsPanel, error) {
	ids, all, err := s.scope(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	v, err := s.cached(cacheKey(ctx, "tenant_requests", propertyID, ""), func() (any, error) {
		var out TenantRequestsPanel
		out.Recent = []RecentSR{}
		err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			args := []any{}
			pw := propWhere("sr.property_id", ids, all, &args)
			_ = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE (sr.created_at AT TIME ZONE pr.timezone)::date = (now() AT TIME ZONE pr.timezone)::date),
				count(*) FILTER (WHERE sr.status NOT IN ('closed','cancelled')), count(*) FILTER (WHERE sr.status NOT IN ('resolved','closed','cancelled') AND sr.sla_risk_at IS NOT NULL)
				FROM service_requests sr JOIN properties pr ON pr.location_id = sr.property_id WHERE 1=1`+pw, args...).Scan(&out.NewToday, &out.Open, &out.SLARisk)
			rows, err := tx.Query(ctx, `SELECT sr.id, sr.request_number, sr.title, sr.status, sr.priority, t.name, sr.created_at, sr.sla_risk_at IS NOT NULL FROM service_requests sr LEFT JOIN tenants t ON t.id = sr.tenant_id
				WHERE sr.status NOT IN ('closed','cancelled')`+pw+` ORDER BY sr.created_at DESC LIMIT 8`, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var r RecentSR
				if err := rows.Scan(&r.ID, &r.RequestNumber, &r.Title, &r.Status, &r.Priority, &r.TenantName, &r.CreatedAt, &r.SLARisk); err != nil {
					return err
				}
				out.Recent = append(out.Recent, r)
			}
			return rows.Err()
		})
		return &out, err
	})
	if err != nil {
		return nil, err
	}
	return v.(*TenantRequestsPanel), nil
}
