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
	"github.com/buildingvision/api/internal/security"
	"github.com/buildingvision/api/internal/workforce"
)

type Service struct {
	DB       *db.DB
	Ops      *operations.Service
	CacheTTL time.Duration
	cache    sync.Map // key → cacheEntry
	// PRD P2 v2.1 §11: kekurangan staf on-duty per domain (opsional; nil = sinyal dilewati)
	Workforce *workforce.Service
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

// scopeQ: filter property + scope Building/Tower (PRD P0 v2 §8.4; PRD P1 v2.1 P1-DSH-09). Placeholder dialokasikan sekali
// sehingga fragmen WHERE yang sama dapat dipakai di banyak query/alias dengan satu daftar argumen.
type scopeQ struct {
	args   []any
	propPH string // placeholder property_id eksplisit ("" = semua property dalam scope)
	tpl    string // ScopeSQL dengan token {P} (kolom property) dan {L} (ekspresi ltree lokasi)
}

// where: " AND …" untuk kolom property & ekspresi uuid lokasi object (locIDExpr "" = object tanpa lokasi).
func (sq *scopeQ) where(propCol, locIDExpr string) string {
	w := ""
	if sq.propPH != "" {
		w += " AND " + propCol + " = " + sq.propPH
	}
	path := "NULL::ltree"
	if locIDExpr != "" {
		path = "(SELECT sl.path FROM locations sl WHERE sl.id = " + locIDExpr + ")"
	}
	return w + " AND " + strings.NewReplacer("{P}", propCol, "{L}", path).Replace(sq.tpl)
}

// scope: izin overview + filter scope. property_id eksplisit boleh untuk user ber-scope building di property tsb.
func (s *Service) scope(ctx context.Context, propertyID *uuid.UUID) (*scopeQ, error) {
	p := authctx.Must(ctx)
	sq := &scopeQ{}
	add := func(v any) string { sq.args = append(sq.args, v); return fmt.Sprintf("$%d", len(sq.args)) }
	if propertyID != nil {
		if !p.HasAnyOnProperty("overview.dashboard.view", *propertyID) {
			return nil, apperr.Forbidden("")
		}
		sq.propPH = add(*propertyID)
	}
	sq.tpl = p.ScopeSQL("overview.dashboard.view", "{P}", "{L}", add)
	return sq, nil
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
	OpenWorkOrders Counter `json:"open_work_orders"`
	Overdue        Counter `json:"overdue"`
	SLARisk        Counter `json:"sla_risk"`
	PMDue          Counter `json:"pm_due"`
	Incidents      Counter `json:"incidents"`
	TenantRequests Counter `json:"tenant_requests"`
	// PRD P1 v2 §10 Today's Operations: Open Tasks, Due Today, Overdue Tasks, Completed Today
	OpenTasks      Counter   `json:"open_tasks"`
	DueToday       Counter   `json:"due_today"`
	OverdueTasks   Counter   `json:"overdue_tasks"`
	CompletedToday Counter   `json:"completed_today"`
	GeneratedAt    time.Time `json:"generated_at"`
}

func (s *Service) Today(ctx context.Context, propertyID *uuid.UUID) (*Today, error) {
	sq, err := s.scope(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	v, err := s.cached(cacheKey(ctx, "today", propertyID, ""), func() (any, error) {
		var out Today
		err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			args := sq.args
			pw := sq.where("property_id", "location_id")
			q := func(sql string) int {
				var n int
				_ = tx.QueryRow(ctx, sql, args...).Scan(&n)
				return n
			}
			out.OpenWorkOrders = Counter{Value: q(`SELECT count(*) FROM work_orders WHERE status NOT IN ('closed','cancelled','draft')` + pw), Link: "/operations/work-orders?open=true"}
			out.OpenWorkOrders.Breakdown = map[string]int{
				"overdue":  q(`SELECT count(*) FROM work_orders WHERE status NOT IN ('completed','closed','cancelled','draft') AND (is_overdue OR due_at < now())` + pw),
				"sla_risk": q(`SELECT count(*) FROM work_orders WHERE status NOT IN ('completed','closed','cancelled','draft') AND sla_risk_at IS NOT NULL` + pw),
				"draft":    q(`SELECT count(*) FROM work_orders WHERE status = 'draft'` + pw),
			}
			ovTask := q(`SELECT count(*) FROM tasks WHERE status NOT IN ('completed','closed','cancelled') AND (is_overdue OR due_at < now())` + pw)
			out.Overdue = Counter{Value: out.OpenWorkOrders.Breakdown["overdue"] + ovTask, Breakdown: map[string]int{"work_orders": out.OpenWorkOrders.Breakdown["overdue"], "tasks": ovTask}, Link: "/operations/work-orders?overdue=true"}
			riskTask := q(`SELECT count(*) FROM tasks WHERE status NOT IN ('completed','closed','cancelled') AND sla_risk_at IS NOT NULL` + pw)
			riskSR := q(`SELECT count(*) FROM service_requests WHERE status NOT IN ('resolved','closed','cancelled') AND sla_risk_at IS NOT NULL` + pw)
			breached := q(`SELECT (SELECT count(*) FROM tasks WHERE status NOT IN ('completed','closed','cancelled') AND sla_breached_at IS NOT NULL` + pw + `) +
				(SELECT count(*) FROM work_orders WHERE status NOT IN ('completed','closed','cancelled','draft') AND sla_breached_at IS NOT NULL` + pw + `) +
				(SELECT count(*) FROM service_requests WHERE status NOT IN ('resolved','closed','cancelled') AND sla_breached_at IS NOT NULL` + pw + `)`)
			out.SLARisk = Counter{Value: out.OpenWorkOrders.Breakdown["sla_risk"] + riskTask + riskSR, Breakdown: map[string]int{"work_orders": out.OpenWorkOrders.Breakdown["sla_risk"], "tasks": riskTask, "service_requests": riskSR, "breached": breached}, Link: "/operations/tasks?sla_risk=true"}
			// PRD P1 v2 §10: KPI task harian (timezone property) — tiap KPI drill-down ke list terfilter (§37)
			tpw := sq.where("t.property_id", "t.location_id")
			today := func(col string) string {
				return `(t.` + col + ` AT TIME ZONE pr.timezone)::date = (now() AT TIME ZONE pr.timezone)::date`
			}
			out.OpenTasks = Counter{Value: q(`SELECT count(*) FROM tasks WHERE status NOT IN ('completed','closed','cancelled')` + pw),
				Breakdown: map[string]int{"in_progress": q(`SELECT count(*) FROM tasks WHERE status = 'in_progress'` + pw), "unassigned": q(`SELECT count(*) FROM tasks WHERE status = 'new' AND assignee_user_id IS NULL AND assignee_team_id IS NULL` + pw)},
				Link:      "/operations/tasks?open=true"}
			dueTask := q(`SELECT count(*) FROM tasks t JOIN properties pr ON pr.location_id = t.property_id WHERE t.status NOT IN ('completed','closed','cancelled') AND ` + today("due_at") + tpw)
			dueWO := q(`SELECT count(*) FROM work_orders t JOIN properties pr ON pr.location_id = t.property_id WHERE t.status NOT IN ('completed','closed','cancelled','draft') AND ` + today("due_at") + tpw)
			out.DueToday = Counter{Value: dueTask + dueWO, Breakdown: map[string]int{"tasks": dueTask, "work_orders": dueWO}, Link: "/operations/tasks?due_today=true"}
			out.OverdueTasks = Counter{Value: ovTask, Link: "/operations/tasks?overdue=true"}
			doneTask := q(`SELECT count(*) FROM tasks t JOIN properties pr ON pr.location_id = t.property_id WHERE t.completed_at IS NOT NULL AND ` + today("completed_at") + tpw)
			doneWO := q(`SELECT count(*) FROM work_orders t JOIN properties pr ON pr.location_id = t.property_id WHERE t.completed_at IS NOT NULL AND ` + today("completed_at") + tpw)
			out.CompletedToday = Counter{Value: doneTask + doneWO, Breakdown: map[string]int{"tasks": doneTask, "work_orders": doneWO}, Link: "/operations/tasks?completed_today=true"}
			// PM schedule tidak punya lokasi sendiri: lokasi = lokasi asset
			mpw := sq.where("ms.property_id", "(SELECT sa.location_id FROM assets sa WHERE sa.id = ms.asset_id)")
			out.PMDue = Counter{Value: q(`SELECT count(*) FROM maintenance_schedules ms WHERE ms.status IN ('scheduled','due','overdue') AND ms.due_at <= now() + interval '7 days'` + mpw),
				Breakdown: map[string]int{"overdue": q(`SELECT count(*) FROM maintenance_schedules ms WHERE ms.status = 'overdue'` + mpw), "today": q(`SELECT count(*) FROM maintenance_schedules ms JOIN properties pr ON pr.location_id = ms.property_id WHERE ms.status IN ('scheduled','due') AND ms.due_date = (now() AT TIME ZONE pr.timezone)::date` + mpw)},
				Link:      "/engineering/preventive-maintenance?due_within_days=7"}
			out.Incidents = Counter{Value: q(`SELECT count(*) FROM incidents WHERE status NOT IN ('closed','cancelled')` + pw),
				Breakdown: map[string]int{"critical": q(`SELECT count(*) FROM incidents WHERE status NOT IN ('closed','cancelled') AND severity = 'critical'` + pw), "new": q(`SELECT count(*) FROM incidents WHERE status = 'new'` + pw)},
				Link:      "/operations/incidents?open=true"}
			out.TenantRequests = Counter{Value: q(`SELECT count(*) FROM service_requests WHERE status NOT IN ('closed','cancelled')` + pw),
				Breakdown: map[string]int{"new_today": q(`SELECT count(*) FROM service_requests sr JOIN properties pr ON pr.location_id = sr.property_id WHERE (sr.created_at AT TIME ZONE pr.timezone)::date = (now() AT TIME ZONE pr.timezone)::date` + sq.where("sr.property_id", "sr.location_id")), "sla_risk": riskSR, "new": q(`SELECT count(*) FROM service_requests WHERE status = 'new'` + pw)},
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
	// PRD P1 v2 §11: sla_breach | critical_incident | critical_asset_issue | maintenance_overdue | patrol_overdue | overdue_task |
	// overdue_work_order | reopened_request | sla_risk | unresolved_finding | incident | sync_conflict
	Category       string     `json:"category"`
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
	sq, err := s.scope(ctx, propertyID)
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
			args := sq.args
			pw := sq.where("x.property_id", "x.location_id")
			domainFilter := ""
			switch domain {
			case "engineering":
				// dikurung: OR tanpa kurung sebelumnya melewati filter property & kondisi WHERE
				domainFilter = " AND (x.object_type IN ('work_order','maintenance_schedule') OR (x.object_type = 'task' AND x.sub_type IN ('general','inspection','routine_maintenance')))"
			case "security":
				domainFilter = " AND (x.object_type IN ('incident','emergency_alert') OR (x.object_type = 'task' AND x.sub_type IN ('patrol','patrol_missed')))"
			case "housekeeping":
				domainFilter = " AND (x.object_type = 'task' AND x.sub_type = 'cleaning')"
			}
			rows, err := tx.Query(ctx, `
				WITH x AS (
				  SELECT 'work_order' AS object_type, w.id, w.property_id, w.work_order_number AS label, w.title, w.status, w.priority, w.location_id, w.due_at, w.assignee_user_id, w.work_order_type AS sub_type,
				         w.sla_breached_at, w.sla_risk_at, w.is_overdue OR (w.due_at < now()) AS overdue, w.created_at,
				         (w.asset_id IS NOT NULL AND (w.priority = 'critical' OR EXISTS (SELECT 1 FROM assets ca WHERE ca.id = w.asset_id AND ca.criticality = 'critical'))) AS asset_critical, false AS reopened
				  FROM work_orders w WHERE w.status NOT IN ('completed','closed','cancelled','draft')
				  UNION ALL
				  SELECT 'task', t.id, t.property_id, t.task_number, t.title, t.status, t.priority, t.location_id, t.due_at, t.assignee_user_id, t.task_type,
				         t.sla_breached_at, t.sla_risk_at, t.is_overdue OR (t.due_at < now()), t.created_at, false, false
				  FROM tasks t WHERE t.status NOT IN ('completed','closed','cancelled')
				  UNION ALL
				  SELECT 'service_request', sr.id, sr.property_id, sr.request_number, sr.title, sr.status, sr.priority, sr.location_id, NULL, sr.assignee_user_id, sr.category_code,
				         sr.sla_breached_at, sr.sla_risk_at, false, sr.created_at, false, sr.reopen_count > 0
				  FROM service_requests sr WHERE sr.status NOT IN ('resolved','closed','cancelled')
				  UNION ALL
				  SELECT 'incident', i.id, i.property_id, i.incident_number, i.title, i.status, i.severity, i.location_id, NULL, i.assignee_user_id, i.category,
				         i.sla_breached_at, i.sla_risk_at, false, i.reported_at, false, false
				  FROM incidents i WHERE i.status NOT IN ('resolved','closed','cancelled')
				  UNION ALL
				  SELECT 'finding', f.id, f.property_id, f.finding_number, f.title, f.status, f.severity, f.location_id, NULL, NULL, f.finding_type,
				         NULL, NULL, false, f.reported_at,
				         (f.asset_id IS NOT NULL AND (f.severity = 'critical' OR EXISTS (SELECT 1 FROM assets ca WHERE ca.id = f.asset_id AND ca.criticality = 'critical'))), false
				  FROM findings f WHERE f.status IN ('open','in_progress')
				  UNION ALL
				  SELECT 'maintenance_schedule', ms.id, ms.property_id, mp.plan_code, mp.name || ' — ' || a.name, ms.status, mp.default_priority, a.location_id, ms.due_at, NULL, 'maintenance',
				         NULL, NULL, true, ms.due_at, false, false
				  FROM maintenance_schedules ms JOIN maintenance_plans mp ON mp.id = ms.plan_id JOIN assets a ON a.id = ms.asset_id WHERE ms.status = 'overdue'
				  UNION ALL
				  -- PRD P2 v2.1 P2-EMG-09: Emergency aktif
				  SELECT 'emergency_alert', e.id, e.property_id, e.alert_number, 'Emergency — ' || `+security.EmergencyTypeLabelSQL("e.emergency_type")+`, e.status, 'critical', e.location_id, NULL, e.responder_user_id, e.emergency_type,
				         NULL, NULL, false, e.raised_at, false, false
				  FROM emergency_alerts e WHERE e.status IN ('raised','acknowledged','responding')
				  UNION ALL
				  -- PRD P2 v2.1 §11: patrol selesai dengan checkpoint terlewat (belum diverifikasi / Close supervisor)
				  SELECT 'task', t.id, t.property_id, t.task_number, t.title || ' — ' || pt.missed_checkpoints || ' checkpoint terlewat', t.status, t.priority, t.location_id, t.due_at, t.assignee_user_id, 'patrol_missed',
				         NULL, NULL, false, COALESCE(t.completed_at, t.updated_at), false, false
				  FROM tasks t JOIN patrol_tasks pt ON pt.task_id = t.id WHERE t.status = 'completed' AND pt.missed_checkpoints > 0 AND t.completed_at > now() - interval '48 hours'
				)
				SELECT x.object_type, x.id, x.label, x.title, x.status, x.priority, x.location_id, x.due_at, u.full_name, x.sub_type,
				  CASE
				    WHEN x.object_type = 'emergency_alert' THEN 'active_emergency'
				    WHEN x.sub_type = 'patrol_missed' THEN 'checkpoint_missed'
				    WHEN x.sla_breached_at IS NOT NULL THEN 'sla_breach'
				    WHEN x.object_type = 'incident' AND x.priority = 'critical' THEN 'critical_incident'
				    WHEN x.asset_critical THEN 'critical_asset_issue'
				    WHEN x.object_type = 'maintenance_schedule' THEN 'maintenance_overdue'
				    WHEN x.object_type = 'task' AND x.sub_type = 'patrol' AND x.overdue THEN 'patrol_overdue'
				    WHEN x.object_type = 'task' AND x.overdue THEN 'overdue_task'
				    WHEN x.object_type = 'work_order' AND x.overdue THEN 'overdue_work_order'
				    WHEN x.reopened THEN 'reopened_request'
				    WHEN x.sla_risk_at IS NOT NULL THEN 'sla_risk'
				    WHEN x.object_type = 'finding' THEN 'unresolved_finding'
				    WHEN x.object_type = 'incident' THEN 'incident'
				    ELSE NULL END AS category,
				  COALESCE(x.sla_breached_at, CASE WHEN x.overdue THEN x.due_at END, x.sla_risk_at, x.created_at) AS since
				FROM x LEFT JOIN users u ON u.id = x.assignee_user_id
				WHERE (x.sla_breached_at IS NOT NULL OR x.sla_risk_at IS NOT NULL OR x.overdue OR x.asset_critical OR x.reopened OR x.object_type IN ('finding','maintenance_schedule','emergency_alert') OR x.sub_type = 'patrol_missed' OR (x.object_type = 'incident' AND x.priority IN ('critical','high')))`+pw+domainFilter+`
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
				case "active_emergency", "sla_breach", "maintenance_overdue", "patrol_overdue", "critical_incident", "critical_asset_issue", "overdue_task", "overdue_work_order":
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
		items = append(items, s.workforceSignals(ctx, p, propertyID, domain)...)
		items = append(items, s.tenantSignals(ctx, p, propertyID, domain)...)
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

// workforceSignals (PRD P2 v2.1 §11): sertifikat/lisensi staf kedaluwarsa ≤ 30 hari & kekurangan staf on-duty shift berjalan.
func (s *Service) workforceSignals(ctx context.Context, p *authctx.Principal, propertyID *uuid.UUID, domain string) []AttentionItem {
	var out []AttentionItem
	if domain == "" || domain == "security" || domain == "housekeeping" {
		if s.Workforce != nil {
			for _, d := range []string{"security", "housekeeping"} {
				if domain != "" && domain != d {
					continue
				}
				b, err := s.Workforce.OnDuty(ctx, d, propertyID)
				if err != nil {
					continue
				}
				for _, sh := range b.Shifts {
					if sh.Shortage <= 0 {
						continue
					}
					label := map[string]string{"security": "Security", "housekeeping": "Housekeeping"}[d]
					out = append(out, AttentionItem{Category: "workforce_shortage", Severity: "warning", ObjectType: "workforce", ObjectID: sh.ShiftID, Label: label + " · " + sh.Name,
						Title: fmt.Sprintf("%d dari minimal %d staf on-duty (dijadwalkan %d)", sh.OnDuty, sh.MinStaff, sh.Scheduled), Status: "shortage", Since: sh.StartsAt,
						AgeMinutes: int(time.Since(sh.StartsAt).Minutes()), AllowedActions: []string{"view"}, DeepLink: "/" + d + "/shifts?tab=on-duty"})
				}
			}
		}
	}
	// PRD P2 v2.1 P2-DOC-02: dokumen equipment & warranty kedaluwarsa (≤ 30 hari)
	if (domain == "" || domain == "engineering") && p.Has("engineering.asset_documents.view") {
		_ = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			args := []any{}
			add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
			where := ""
			if propertyID != nil {
				where += " AND a.property_id = " + add(*propertyID)
			}
			where += " AND " + p.ScopeSQL("engineering.asset_documents.view", "a.property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = a.location_id)", add)
			rows, err := tx.Query(ctx, `SELECT 'asset_document', d.id, a.id, a.asset_code || ' · ' || d.title, a.name, d.expires_on FROM asset_documents d JOIN assets a ON a.id = d.asset_id
				WHERE d.is_active AND a.deleted_at IS NULL AND d.expires_on IS NOT NULL AND d.expires_on <= CURRENT_DATE + 30`+where+`
				UNION ALL SELECT 'asset', a.id, a.id, a.asset_code || ' · Warranty', a.name, a.warranty_until::date FROM assets a
				WHERE a.deleted_at IS NULL AND a.status <> 'decommissioned' AND a.warranty_until IS NOT NULL AND a.warranty_until::date <= CURRENT_DATE + 30`+where+` ORDER BY 6 LIMIT 50`, args...)
			if err != nil {
				return nil
			}
			defer rows.Close()
			for rows.Next() {
				var ot, label, name string
				var id, assetID uuid.UUID
				var exp time.Time
				if rows.Scan(&ot, &id, &assetID, &label, &name, &exp) != nil {
					continue
				}
				sev, title := "warning", name+" — berlaku s/d "+exp.Format("02 Jan 2006")
				if exp.Before(time.Now().Truncate(24 * time.Hour)) {
					sev, title = "critical", name+" — kedaluwarsa "+exp.Format("02 Jan 2006")
				}
				out = append(out, AttentionItem{Category: "document_expiring", Severity: sev, ObjectType: ot, ObjectID: id, Label: label, Title: title, Status: "expiring",
					Since: exp, AgeMinutes: int(time.Since(exp).Minutes()), AllowedActions: []string{"view"}, DeepLink: "/assets/" + assetID.String() + "?tab=documents"})
			}
			return nil
		})
	}
	return out
}

// tenantSignals (PRD P3 v2.1 P3-TSH-07, P3-CMP-03): isu berulang (lokasi + kategori ≥ ambang dalam jendela waktu) yang
// belum diselesaikan → Attention Required; keluhan berulang = critical.
func (s *Service) tenantSignals(ctx context.Context, p *authctx.Principal, propertyID *uuid.UUID, domain string) []AttentionItem {
	var out []AttentionItem
	if domain != "" || !p.Has("tenant_relation.recurring_issues.view") {
		return nil
	}
	_ = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := ""
		if propertyID != nil {
			where += " AND ri.property_id = " + add(*propertyID)
		}
		where += " AND " + p.ScopeSQL("tenant_relation.recurring_issues.view", "ri.property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = ri.location_id)", add)
		rows, err := tx.Query(ctx, `SELECT ri.id, l.name, COALESCE(c.name, ri.category_code), ri.request_count, ri.complaint_count, ri.window_days, ri.threshold, ri.status, ri.first_seen_at, ri.location_id
			FROM recurring_issues ri JOIN locations l ON l.id = ri.location_id LEFT JOIN service_request_categories c ON c.id = ri.category_id
			WHERE ri.status IN ('open','acknowledged')`+where+` ORDER BY ri.last_seen_at DESC LIMIT 50`, args...)
		if err != nil {
			return nil
		}
		type row struct {
			it    AttentionItem
			locID uuid.UUID
		}
		var list []row
		for rows.Next() {
			var id, locID uuid.UUID
			var loc, cat, status string
			var n, complaints, window, threshold int
			var first time.Time
			if rows.Scan(&id, &loc, &cat, &n, &complaints, &window, &threshold, &status, &first, &locID) != nil {
				continue
			}
			sev := "warning"
			if complaints >= threshold {
				sev = "critical"
			}
			list = append(list, row{locID: locID, it: AttentionItem{Category: "recurring_issue", Severity: sev, ObjectType: "recurring_issue", ObjectID: id, Label: "Isu berulang · " + loc,
				Title: fmt.Sprintf("%s — %d permintaan dalam %d hari", cat, n, window), Status: status, Since: first, AgeMinutes: int(time.Since(first).Minutes()),
				AllowedActions: []string{"view"}, DeepLink: "/tenant-relation/recurring-issues/" + id.String()}})
		}
		rows.Close()
		for _, r := range list {
			pt := property.LocationPathText(ctx, tx, r.locID)
			r.it.LocationPath = &pt
			out = append(out, r.it)
		}
		return nil
	})
	return out
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func deepLink(objectType string, id uuid.UUID) string {
	route := map[string]string{"task": "/operations/tasks/", "work_order": "/operations/work-orders/", "service_request": "/operations/service-requests/", "incident": "/operations/incidents/", "finding": "/findings/", "maintenance_schedule": "/engineering/preventive-maintenance/",
		"emergency_alert": "/security/emergency/", "asset": "/assets/"}[objectType]
	return route + id.String()
}

// allowedActions: CTA minimal View / Assign / Close / Resolve (PRD §19.2) berdasarkan permission user.
func (s *Service) allowedActions(ctx context.Context, tx pgx.Tx, p *authctx.Principal, it AttentionItem) []string {
	out := []string{"view"}
	var pid uuid.UUID
	table := map[string]string{"task": "tasks", "work_order": "work_orders", "service_request": "service_requests", "incident": "incidents", "finding": "findings", "maintenance_schedule": "maintenance_schedules", "emergency_alert": "emergency_alerts"}[it.ObjectType]
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
	case "emergency_alert":
		if p.HasAnyOnProperty("security.emergency_alerts.respond", pid) {
			if it.Status == "raised" {
				out = append(out, "acknowledge")
			}
			if it.Status == "raised" || it.Status == "acknowledged" {
				out = append(out, "respond")
			}
		}
		if p.HasAnyOnProperty("security.emergency_alerts.resolve", pid) {
			out = append(out, "resolve")
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
	if _, err := s.scope(ctx, propertyID); err != nil {
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
			panel.Items = append(panel.Items, tasks...) // tetap [] (bukan null) bila kosong
		case "housekeeping":
			tasks, _, err := s.Ops.List(ctx, operations.ObjTask, operations.ListFilter{PropertyID: propertyID, ScheduledOn: &today, Types: []string{"cleaning"}, Sort: "due_at"}, page)
			if err != nil {
				return nil, err
			}
			panel.Items = append(panel.Items, tasks...) // tetap [] (bukan null) bila kosong
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
	DueToday       int        `json:"due_today"` // PRD P1 v2 §12
	Open           int        `json:"open"`      // open_tasks + open_work_orders
	SLARisk        int        `json:"sla_risk"`  // PRD P2 v2.1 P2-WKL-02: SLA risk by team
	SLABreached    int        `json:"sla_breached"`
	Members        int        `json:"members,omitempty"`
}

// TeamWorkload: group "" (per team × assignee, default) atau "team" (PRD P1 v2 §12: Team | Open | Due Today | Overdue).
func (s *Service) TeamWorkload(ctx context.Context, propertyID *uuid.UUID, group string) ([]WorkloadRow, error) {
	sq, err := s.scope(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	byTeam := group == "team"
	v, err := s.cached(cacheKey(ctx, "workload", propertyID, group), func() (any, error) {
		var out []WorkloadRow
		err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			args := sq.args
			pw := sq.where("w.property_id", "w.location_id")
			userCols, groupBy, order := "u.id, COALESCE(u.full_name, '(belum ditugaskan ke orang)')", "t.id, t.name, t.domain, u.id, u.full_name", "t.name NULLS LAST, u.full_name NULLS LAST"
			if byTeam {
				userCols, groupBy, order = "NULL::uuid, ''", "t.id, t.name, t.domain", "t.name NULLS LAST"
			}
			rows, err := tx.Query(ctx, `
				WITH w AS (
				  SELECT 'task' AS kind, t.property_id, t.location_id, t.assignee_user_id, t.assignee_team_id, t.status, (t.is_overdue OR t.due_at < now()) AS overdue,
				    (t.due_at AT TIME ZONE pr.timezone)::date = (now() AT TIME ZONE pr.timezone)::date AS due_today, t.sla_risk_at IS NOT NULL AS sla_risk, t.sla_breached_at IS NOT NULL AS sla_breached
				  FROM tasks t JOIN properties pr ON pr.location_id = t.property_id WHERE t.status NOT IN ('completed','closed','cancelled')
				  UNION ALL
				  SELECT 'work_order', o.property_id, o.location_id, o.assignee_user_id, o.assignee_team_id, o.status, (o.is_overdue OR o.due_at < now()),
				    (o.due_at AT TIME ZONE pr.timezone)::date = (now() AT TIME ZONE pr.timezone)::date, o.sla_risk_at IS NOT NULL, o.sla_breached_at IS NOT NULL
				  FROM work_orders o JOIN properties pr ON pr.location_id = o.property_id WHERE o.status NOT IN ('completed','closed','cancelled','draft')
				), members AS (
				  SELECT tm.team_id, tm.user_id FROM team_members tm
				)
				SELECT t.id, t.name, t.domain, `+userCols+`,
				  count(*) FILTER (WHERE w.kind = 'task'), count(*) FILTER (WHERE w.kind = 'work_order'), count(*) FILTER (WHERE w.overdue), count(*) FILTER (WHERE w.status = 'in_progress'),
				  count(*) FILTER (WHERE w.due_today), (SELECT count(*) FROM team_members tm WHERE tm.team_id = t.id),
				  count(*) FILTER (WHERE w.sla_risk AND NOT w.sla_breached), count(*) FILTER (WHERE w.sla_breached)
				FROM w
				LEFT JOIN users u ON u.id = w.assignee_user_id
				LEFT JOIN teams t ON t.id = COALESCE(w.assignee_team_id, (SELECT m.team_id FROM members m JOIN teams tt ON tt.id = m.team_id WHERE m.user_id = w.assignee_user_id AND (tt.property_id IS NULL OR tt.property_id = w.property_id) ORDER BY tt.property_id NULLS LAST LIMIT 1))
				WHERE (w.assignee_user_id IS NOT NULL OR w.assignee_team_id IS NOT NULL)`+pw+`
				GROUP BY `+groupBy+`
				ORDER BY `+order, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var r WorkloadRow
				var tname, tdomain *string
				if err := rows.Scan(&r.TeamID, &tname, &tdomain, &r.UserID, &r.AssigneeName, &r.OpenTasks, &r.OpenWorkOrders, &r.Overdue, &r.InProgress, &r.DueToday, &r.Members, &r.SLARisk, &r.SLABreached); err != nil {
					return err
				}
				r.Open = r.OpenTasks + r.OpenWorkOrders
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
	PropertyID   uuid.UUID `json:"property_id"`
	LocationType string    `json:"location_type"`
	Name         string    `json:"name"`
	PathText     string    `json:"path_text"`
	Open         int       `json:"open"`
	Overdue      int       `json:"overdue"`
	SLARisk      int       `json:"sla_risk"`
	Incidents    int       `json:"incidents"`
}

func (s *Service) BuildingState(ctx context.Context, propertyID *uuid.UUID) ([]BuildingState, error) {
	sq, err := s.scope(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	v, err := s.cached(cacheKey(ctx, "building_state", propertyID, ""), func() (any, error) {
		var out []BuildingState
		err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			args := sq.args
			// user ber-scope Tower hanya melihat building/tower di subtree scope-nya
			pw := sq.where("bl.property_id", "bl.id")
			rows, err := tx.Query(ctx, `
				WITH b AS (SELECT bl.id, bl.property_id, bl.location_type, bl.name, bl.path FROM locations bl WHERE bl.deleted_at IS NULL AND bl.location_type IN ('building','tower')`+pw+`),
				x AS (
				  SELECT location_id, (is_overdue OR due_at < now()) AS overdue, sla_risk_at IS NOT NULL OR sla_breached_at IS NOT NULL AS risk, false AS incident FROM work_orders WHERE status NOT IN ('completed','closed','cancelled','draft')
				  UNION ALL SELECT location_id, (is_overdue OR due_at < now()), sla_risk_at IS NOT NULL OR sla_breached_at IS NOT NULL, false FROM tasks WHERE status NOT IN ('completed','closed','cancelled')
				  UNION ALL SELECT location_id, false, sla_risk_at IS NOT NULL OR sla_breached_at IS NOT NULL, false FROM service_requests WHERE status NOT IN ('resolved','closed','cancelled')
				  UNION ALL SELECT location_id, false, false, true FROM incidents WHERE status NOT IN ('closed','cancelled')
				)
				SELECT b.id, b.property_id, b.location_type, b.name,
				  count(x.*) FILTER (WHERE NOT x.incident), count(x.*) FILTER (WHERE x.overdue), count(x.*) FILTER (WHERE x.risk), count(x.*) FILTER (WHERE x.incident)
				FROM b LEFT JOIN locations l ON l.path <@ b.path LEFT JOIN x ON x.location_id = l.id
				GROUP BY b.id, b.property_id, b.location_type, b.name, b.path ORDER BY b.path`, args...)
			if err != nil {
				return err
			}
			var idsOut []uuid.UUID
			for rows.Next() {
				var r BuildingState
				if err := rows.Scan(&r.LocationID, &r.PropertyID, &r.LocationType, &r.Name, &r.Open, &r.Overdue, &r.SLARisk, &r.Incidents); err != nil {
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
	sq, err := s.scope(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	v, err := s.cached(cacheKey(ctx, "tenant_requests", propertyID, ""), func() (any, error) {
		var out TenantRequestsPanel
		out.Recent = []RecentSR{}
		err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			args := sq.args
			pw := sq.where("sr.property_id", "sr.location_id")
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

// ---------- PRD P1 v2 §42/§44: My Work (dashboard "Limited" worker) ----------

type MyWorkSummary struct {
	Open           int                  `json:"open"`
	DueToday       int                  `json:"due_today"`
	Overdue        int                  `json:"overdue"`
	InProgress     int                  `json:"in_progress"`
	SLARisk        int                  `json:"sla_risk"`
	CompletedToday int                  `json:"completed_today"`
	Next           *operations.WorkItem `json:"next"` // pekerjaan berikut untuk CTA "Start Task" (prioritas → due)
	GeneratedAt    time.Time            `json:"generated_at"`
}

// MyWorkSummary: task & WO milik user (assignee langsung atau anggota team assignee).
func (s *Service) MyWorkSummary(ctx context.Context) (*MyWorkSummary, error) {
	p := authctx.Must(ctx)
	out := &MyWorkSummary{GeneratedAt: time.Now().UTC()}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		mine := `(x.assignee_user_id = $1 OR x.assignee_team_id = ANY($2::uuid[]))`
		teams := p.TeamIDs
		if teams == nil {
			teams = []uuid.UUID{}
		}
		return tx.QueryRow(ctx, `
			WITH x AS (
			  SELECT t.assignee_user_id, t.assignee_team_id, t.status, t.due_at, t.completed_at, t.is_overdue, t.sla_risk_at, pr.timezone FROM tasks t JOIN properties pr ON pr.location_id = t.property_id
			  UNION ALL
			  SELECT w.assignee_user_id, w.assignee_team_id, w.status, w.due_at, w.completed_at, w.is_overdue, w.sla_risk_at, pr.timezone FROM work_orders w JOIN properties pr ON pr.location_id = w.property_id WHERE w.status <> 'draft'
			)
			SELECT count(*) FILTER (WHERE x.status NOT IN ('completed','closed','cancelled')),
			  count(*) FILTER (WHERE x.status NOT IN ('completed','closed','cancelled') AND (x.due_at AT TIME ZONE x.timezone)::date = (now() AT TIME ZONE x.timezone)::date),
			  count(*) FILTER (WHERE x.status NOT IN ('completed','closed','cancelled') AND (x.is_overdue OR x.due_at < now())),
			  count(*) FILTER (WHERE x.status = 'in_progress'),
			  count(*) FILTER (WHERE x.status NOT IN ('completed','closed','cancelled') AND x.sla_risk_at IS NOT NULL),
			  count(*) FILTER (WHERE x.completed_at IS NOT NULL AND (x.completed_at AT TIME ZONE x.timezone)::date = (now() AT TIME ZONE x.timezone)::date)
			FROM x WHERE `+mine, p.UserID, teams).Scan(&out.Open, &out.DueToday, &out.Overdue, &out.InProgress, &out.SLARisk, &out.CompletedToday)
	})
	if err != nil {
		return nil, err
	}
	// pekerjaan berikut: yang sedang berjalan dulu, lalu siap dimulai (prioritas tertinggi, due terdekat)
	page := httpx.Page{Limit: 1}
	for _, st := range [][]string{{"in_progress", "on_hold"}, {"assigned", "scheduled"}} {
		for _, ot := range []string{operations.ObjTask, operations.ObjWorkOrder} {
			items, _, err := s.Ops.List(ctx, ot, operations.ListFilter{Mine: true, Statuses: st, Sort: "priority"}, page)
			if err == nil && len(items) > 0 {
				out.Next = &items[0]
				return out, nil
			}
		}
	}
	return out, nil
}
