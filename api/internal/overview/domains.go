package overview

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Domain Dashboards (PRD P2 v2.1 §5.1 P2-ENG-01..04, §6.1 P2-SDB-01..02, §7.1 P2-HDB-01..05;
// Roadmap v2.1 §25.2 KPI minimum, §25.8 dashboard per domain; Principle 5/8 exception-driven, 9 every KPI drills down) ----------
// Satu endpoint per domain. KPI titik-waktu ("sekarang"/"hari ini") dan KPI periode [from, to] (default 30 hari terakhir,
// zona waktu property). Setiap KPI membawa definisi singkat (hint) dan tautan drill-down ke daftar record.
// Scope: permission view domain (engineering.assets / security.incidents / housekeeping.cleaning) + Building/Tower grant,
// opsional filter property_id & location_id (subtree building/tower/floor). Downtime/MTTR/MTBF → P6 (D-P2-02).

type KPI struct {
	Key       string  `json:"key"`
	Label     string  `json:"label"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`     // count | pct | minutes | idr | score
	Severity  string  `json:"severity"` // normal | warning | critical
	Hint      string  `json:"hint"`
	DrillDown string  `json:"drill_down"`
}

type BreakdownRow struct {
	Key       string             `json:"key"`
	Label     string             `json:"label"`
	Values    map[string]float64 `json:"values"`
	DrillDown string             `json:"drill_down,omitempty"`
}

type DomainDashboard struct {
	Domain         string                    `json:"domain"`
	PropertyID     *uuid.UUID                `json:"property_id"`
	LocationID     *uuid.UUID                `json:"location_id"`
	From           string                    `json:"from"`
	To             string                    `json:"to"`
	GeneratedAt    time.Time                 `json:"generated_at"`
	KPIs           []KPI                     `json:"kpis"`
	Distribution   map[string]int            `json:"distribution,omitempty"` // engineering: health status
	Breakdowns     map[string][]BreakdownRow `json:"breakdowns"`
	Attention      []AttentionItem           `json:"attention"`
	AttentionTotal int                       `json:"attention_total"`
}

type DashboardParams struct {
	PropertyID *uuid.UUID
	LocationID *uuid.UUID // building/tower/floor — subtree
	From, To   string     // YYYY-MM-DD
}

// dctx: scope + periode satu permintaan dashboard.
type dctx struct {
	sq                          *scopeQ
	from, to, today, tomorrow   string // placeholder SQL
	fromDate, toDate, locFilter string
	link                        func(path string) string
}

func (d *dctx) add(v any) string {
	d.sq.args = append(d.sq.args, v)
	return fmt.Sprintf("$%d", len(d.sq.args))
}

// w: klausa scope + filter lokasi (subtree) untuk kolom property & ekspresi uuid lokasi object.
func (d *dctx) w(propCol, locIDExpr string) string {
	out := d.sq.where(propCol, locIDExpr)
	if d.locFilter != "" {
		if locIDExpr == "" {
			return out + " AND false"
		}
		out += " AND (SELECT fl.path FROM locations fl WHERE fl.id = " + locIDExpr + ") <@ (SELECT fr.path FROM locations fr WHERE fr.id = " + d.locFilter + ")"
	}
	return out
}

func (s *Service) newDashboard(ctx context.Context, tx pgx.Tx, domain, perm string, in DashboardParams) (*dctx, *DomainDashboard, error) {
	p := authctx.Must(ctx)
	if !p.Has(perm) {
		return nil, nil, apperr.Forbidden("Memerlukan " + perm)
	}
	loc, _ := time.LoadLocation("Asia/Jakarta")
	if in.PropertyID != nil {
		if !p.HasAnyOnProperty(perm, *in.PropertyID) {
			return nil, nil, apperr.Forbidden("")
		}
		loc = property.PropertyTimezone(ctx, tx, *in.PropertyID)
	}
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	from, to := today.AddDate(0, 0, -29), today
	var err error
	if in.From != "" {
		if from, err = time.ParseInLocation("2006-01-02", in.From, loc); err != nil {
			return nil, nil, apperr.Validation("from harus YYYY-MM-DD").WithField("from", "format tanggal")
		}
	}
	if in.To != "" {
		if to, err = time.ParseInLocation("2006-01-02", in.To, loc); err != nil {
			return nil, nil, apperr.Validation("to harus YYYY-MM-DD").WithField("to", "format tanggal")
		}
	}
	if to.Before(from) || to.Sub(from) > 366*24*time.Hour {
		return nil, nil, apperr.Validation("rentang tanggal tidak valid (maks 366 hari)")
	}
	// $1..$4 = periode (selalu terketik lewat periodCTE), lalu argumen scope
	d := &dctx{sq: &scopeQ{args: []any{from, to.AddDate(0, 0, 1), today, today.AddDate(0, 0, 1)}}, from: "$1", to: "$2", today: "$3", tomorrow: "$4"}
	if in.PropertyID != nil {
		d.sq.propPH = d.add(*in.PropertyID)
	}
	d.sq.tpl = p.ScopeSQL(perm, "{P}", "{L}", d.add)
	if in.LocationID != nil {
		var lp uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM locations WHERE id = $1 AND deleted_at IS NULL`, *in.LocationID).Scan(&lp); err != nil {
			return nil, nil, apperr.Validation("location_id tidak ditemukan").WithField("location_id", "tidak valid")
		}
		if in.PropertyID != nil && lp != *in.PropertyID {
			return nil, nil, apperr.Validation("location_id di luar property").WithField("location_id", "tidak valid")
		}
		d.locFilter = d.add(*in.LocationID)
	}
	d.fromDate, d.toDate = from.Format("2006-01-02"), to.Format("2006-01-02")
	qs := ""
	if in.PropertyID != nil {
		qs += "&property_id=" + in.PropertyID.String()
	}
	if in.LocationID != nil {
		qs += "&location_id=" + in.LocationID.String()
	}
	d.link = func(path string) string {
		if qs == "" {
			return path
		}
		sep := "?"
		for _, c := range path {
			if c == '?' {
				sep = "&"
				break
			}
		}
		return path + sep + qs[1:]
	}
	out := &DomainDashboard{Domain: domain, PropertyID: in.PropertyID, LocationID: in.LocationID, From: d.fromDate, To: d.toDate, GeneratedAt: time.Now().UTC(),
		KPIs: []KPI{}, Breakdowns: map[string][]BreakdownRow{}, Attention: []AttentionItem{}}
	return d, out, nil
}

// periodCTE: mengetik $1..$4 (periode) di setiap query dashboard — Postgres menganalisis CTE walau tidak dirujuk,
// sehingga query yang tidak memakai periode tetap valid dengan daftar argumen yang sama.
const periodCTE = `WITH _period AS (SELECT $1::timestamptz AS pf, $2::timestamptz AS pt, $3::timestamptz AS td, $4::timestamptz AS tm) `

func withPeriod(q string) string {
	if len(q) >= 5 && q[:5] == "WITH " {
		return periodCTE + ", " + q[5:]
	}
	return periodCTE + q
}

func num(ctx context.Context, tx pgx.Tx, q string, args []any) (float64, error) {
	var v *float64
	if err := tx.QueryRow(ctx, withPeriod(q), args...).Scan(&v); err != nil {
		return 0, err
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

func pct1(a, b float64) float64 {
	if b <= 0 {
		return 0
	}
	return math.Round(a/b*1000) / 10
}

// rows: breakdown dari query `SELECT key, label, v1, v2, …`.
func rows(ctx context.Context, tx pgx.Tx, q string, args []any, cols []string, drill func(key string) string) ([]BreakdownRow, error) {
	rs, err := tx.Query(ctx, withPeriod(q), args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	out := []BreakdownRow{}
	for rs.Next() {
		var key, label string
		vals := make([]float64, len(cols))
		dest := []any{&key, &label}
		for i := range vals {
			dest = append(dest, &vals[i])
		}
		if err := rs.Scan(dest...); err != nil {
			return nil, err
		}
		r := BreakdownRow{Key: key, Label: label, Values: map[string]float64{}}
		for i, c := range cols {
			r.Values[c] = vals[i]
		}
		if drill != nil {
			r.DrillDown = drill(key)
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

// buildingOf: building (atau tower teratas) yang menaungi lokasi.
func buildingOf(locExpr string) string {
	return "(SELECT b.id FROM locations b JOIN locations l0 ON l0.id = " + locExpr + " WHERE b.path @> l0.path AND b.location_type IN ('building','tower') ORDER BY b.depth LIMIT 1)"
}

func sevAbove(v, warn, crit float64) string {
	switch {
	case v >= crit && crit > 0:
		return "critical"
	case v >= warn && warn > 0:
		return "warning"
	}
	return "normal"
}

func sevBelow(v, warn, crit float64, hasData bool) string {
	if !hasData {
		return "normal"
	}
	switch {
	case v < crit:
		return "critical"
	case v < warn:
		return "warning"
	}
	return "normal"
}

func (s *Service) attachAttention(ctx context.Context, out *DomainDashboard, domain string) {
	if !authctx.Must(ctx).Has("overview.dashboard.view") {
		return
	}
	items, total, err := s.AttentionRequired(ctx, out.PropertyID, domain, 10)
	if err == nil {
		out.Attention, out.AttentionTotal = items, total
	}
	if out.Attention == nil {
		out.Attention = []AttentionItem{}
	}
}

// ---------- Engineering Dashboard ----------

func (s *Service) EngineeringDashboard(ctx context.Context, in DashboardParams) (*DomainDashboard, error) {
	var out *DomainDashboard
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		d, o, err := s.newDashboard(ctx, tx, "engineering", "engineering.assets.view", in)
		if err != nil {
			return err
		}
		out = o
		a := d.sq.args
		msW := d.w("ms.property_id", "a.location_id")
		pmDue, err := num(ctx, tx, `SELECT count(*)::float8 FROM maintenance_schedules ms JOIN assets a ON a.id = ms.asset_id WHERE ms.status IN ('scheduled','due') AND ms.due_at < now() + interval '7 days'`+msW, a)
		if err != nil {
			return err
		}
		pmOverdue, err := num(ctx, tx, `SELECT count(*)::float8 FROM maintenance_schedules ms JOIN assets a ON a.id = ms.asset_id WHERE ms.status = 'overdue'`+msW, a)
		if err != nil {
			return err
		}
		var pmScheduled, pmOnTime float64
		if err := tx.QueryRow(ctx, periodCTE+`SELECT count(*) FILTER (WHERE ms.status <> 'skipped' AND (ms.due_at <= now() OR ms.status = 'completed'))::float8,
			count(*) FILTER (WHERE ms.status = 'completed' AND ms.completed_at <= ms.due_at + interval '1 day')::float8
			FROM maintenance_schedules ms JOIN assets a ON a.id = ms.asset_id WHERE ms.due_at >= `+d.from+` AND ms.due_at < `+d.to+msW, a...).Scan(&pmScheduled, &pmOnTime); err != nil {
			return err
		}
		woW := d.w("w.property_id", "w.location_id")
		openCorrective, err := num(ctx, tx, `SELECT count(*)::float8 FROM work_orders w WHERE w.work_order_type IN ('corrective','repair') AND w.status NOT IN ('completed','closed','cancelled','draft')`+woW, a)
		if err != nil {
			return err
		}
		cost, err := num(ctx, tx, `SELECT COALESCE(sum(w.actual_cost_amount),0)::float8 FROM work_orders w WHERE w.completed_at >= `+d.from+` AND w.completed_at < `+d.to+woW, a)
		if err != nil {
			return err
		}
		assetW := d.w("a.property_id", "a.location_id")
		out.Distribution = map[string]int{"healthy": 0, "warning": 0, "critical": 0, "offline": 0, "unknown": 0}
		hr, err := tx.Query(ctx, periodCTE+`SELECT a.health_status, count(*) FROM assets a WHERE a.deleted_at IS NULL`+assetW+` GROUP BY 1`, a...)
		if err != nil {
			return err
		}
		for hr.Next() {
			var st string
			var n int
			if hr.Scan(&st, &n) == nil {
				out.Distribution[st] = n
			}
		}
		hr.Close()
		critical := float64(out.Distribution["critical"])
		atRisk := critical + float64(out.Distribution["warning"])
		compliance := pct1(pmOnTime, pmScheduled)
		out.KPIs = []KPI{
			{Key: "pm_due", Label: "PM Due (7 hari)", Value: pmDue, Unit: "count", Severity: "normal", Hint: "Jadwal PM belum selesai yang jatuh tempo ≤ 7 hari", DrillDown: d.link("/engineering/preventive-maintenance?status=scheduled,due")},
			{Key: "pm_overdue", Label: "Overdue PM", Value: pmOverdue, Unit: "count", Severity: sevAbove(pmOverdue, 1, 5), Hint: "Jadwal PM melewati jatuh tempo", DrillDown: d.link("/engineering/preventive-maintenance?status=overdue")},
			{Key: "pm_compliance", Label: "PM Compliance", Value: compliance, Unit: "pct", Severity: sevBelow(compliance, 90, 70, pmScheduled > 0), Hint: "PM selesai tepat waktu (≤ H+1) ÷ PM jatuh tempo dalam periode (tanpa skipped)", DrillDown: d.link("/reports/maintenance?from=" + d.fromDate + "&to=" + d.toDate)},
			{Key: "open_corrective_work_orders", Label: "Open Corrective WO", Value: openCorrective, Unit: "count", Severity: sevAbove(openCorrective, 10, 25), Hint: "WO corrective/repair yang belum selesai", DrillDown: d.link("/operations/work-orders?type=corrective,repair&open=true")},
			{Key: "critical_asset_count", Label: "Critical Asset", Value: critical, Unit: "count", Severity: sevAbove(critical, 1, 1), Hint: "Asset dengan health Critical (skor < 70)", DrillDown: d.link("/assets?health_status=critical")},
			{Key: "at_risk_asset_count", Label: "At-Risk Asset", Value: atRisk, Unit: "count", Severity: sevAbove(atRisk, 1, 5), Hint: "Asset dengan health Warning atau Critical (skor < 90)", DrillDown: d.link("/assets?at_risk=true")},
			{Key: "maintenance_cost", Label: "Maintenance Cost", Value: cost, Unit: "idr", Severity: "normal", Hint: "Biaya aktual WO yang selesai dalam periode (internal)", DrillDown: d.link("/reports/work-orders?from=" + d.fromDate + "&to=" + d.toDate)},
		}
		// P2-ENG-04: breakdown per building, kategori equipment, team
		if out.Breakdowns["building"], err = rows(ctx, tx, `WITH o AS (
				SELECT `+buildingOf("w.location_id")+` AS bid, 1 AS open_wo, 0 AS overdue_pm, 0 AS at_risk FROM work_orders w WHERE w.status NOT IN ('completed','closed','cancelled','draft') AND w.location_id IS NOT NULL`+woW+`
				UNION ALL SELECT `+buildingOf("a.location_id")+`, 0, 1, 0 FROM maintenance_schedules ms JOIN assets a ON a.id = ms.asset_id WHERE ms.status = 'overdue'`+msW+`
				UNION ALL SELECT `+buildingOf("a.location_id")+`, 0, 0, 1 FROM assets a WHERE a.deleted_at IS NULL AND a.health_status IN ('warning','critical')`+assetW+`)
			SELECT b.id::text, b.name, sum(o.open_wo)::float8, sum(o.overdue_pm)::float8, sum(o.at_risk)::float8 FROM o JOIN locations b ON b.id = o.bid GROUP BY b.id, b.name ORDER BY 3 DESC, 2`, a,
			[]string{"open_work_orders", "overdue_pm", "at_risk_assets"}, func(k string) string { return "/operations/work-orders?open=true&location_id=" + k }); err != nil {
			return err
		}
		if out.Breakdowns["equipment_category"], err = rows(ctx, tx, `SELECT e.category_code, COALESCE(max(e.category_name), e.category_code), count(*)::float8,
				count(*) FILTER (WHERE a.health_status IN ('warning','critical'))::float8, count(*) FILTER (WHERE a.health_status = 'critical')::float8,
				COALESCE(sum((SELECT count(*) FROM work_orders w WHERE w.asset_id = a.id AND w.status NOT IN ('completed','closed','cancelled','draft'))),0)::float8
			FROM assets a JOIN equipment e ON e.id = a.equipment_id WHERE a.deleted_at IS NULL`+assetW+` GROUP BY e.category_code ORDER BY 4 DESC, 3 DESC`, a,
			[]string{"assets", "at_risk_assets", "critical_assets", "open_work_orders"}, func(k string) string { return "/assets?category=" + k }); err != nil {
			return err
		}
		if out.Breakdowns["team"], err = rows(ctx, tx, `SELECT t.id::text, t.name, count(*)::float8, count(*) FILTER (WHERE w.due_at < now() OR w.is_overdue)::float8,
				count(*) FILTER (WHERE w.sla_breached_at IS NOT NULL)::float8
			FROM work_orders w JOIN teams t ON t.id = w.assignee_team_id WHERE w.status NOT IN ('completed','closed','cancelled','draft')`+woW+` GROUP BY t.id, t.name ORDER BY 3 DESC`, a,
			[]string{"open_work_orders", "overdue", "sla_breached"}, func(k string) string { return "/operations/work-orders?open=true&team_id=" + k }); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.attachAttention(ctx, out, "engineering")
	return out, nil
}

// ---------- Security Dashboard ----------

func (s *Service) SecurityDashboard(ctx context.Context, in DashboardParams) (*DomainDashboard, error) {
	var out *DomainDashboard
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		d, o, err := s.newDashboard(ctx, tx, "security", "security.incidents.view", in)
		if err != nil {
			return err
		}
		out = o
		a := d.sq.args
		iW := d.w("i.property_id", "i.location_id")
		var today, open, critical, unauthorized, respMin float64
		if err := tx.QueryRow(ctx, periodCTE+`SELECT count(*) FILTER (WHERE i.reported_at >= `+d.today+` AND i.reported_at < `+d.tomorrow+`)::float8,
			count(*) FILTER (WHERE i.status NOT IN ('resolved','closed','cancelled'))::float8,
			count(*) FILTER (WHERE i.status NOT IN ('resolved','closed','cancelled') AND i.severity = 'critical')::float8,
			count(*) FILTER (WHERE i.category = 'unauthorized_access' AND i.reported_at >= `+d.from+` AND i.reported_at < `+d.to+`)::float8,
			COALESCE(avg(EXTRACT(EPOCH FROM (st.responded_at - i.reported_at))/60) FILTER (WHERE st.responded_at IS NOT NULL AND i.reported_at >= `+d.from+` AND i.reported_at < `+d.to+`), 0)::float8
			FROM incidents i LEFT JOIN sla_tracking st ON st.object_type = 'incident' AND st.object_id = i.id WHERE true`+iW, a...).Scan(&today, &open, &critical, &unauthorized, &respMin); err != nil {
			return err
		}
		tW := d.w("t.property_id", "t.location_id")
		var patrolTotal, patrolDone float64
		if err := tx.QueryRow(ctx, periodCTE+`SELECT count(*) FILTER (WHERE t.status <> 'cancelled')::float8, count(*) FILTER (WHERE t.status IN ('completed','closed'))::float8
			FROM tasks t WHERE t.task_type = 'patrol' AND COALESCE(t.scheduled_start_at, t.created_at) >= `+d.from+` AND COALESCE(t.scheduled_start_at, t.created_at) < `+d.to+tW, a...).Scan(&patrolTotal, &patrolDone); err != nil {
			return err
		}
		var cpScanned, cpMissed, cpTotal float64
		if err := tx.QueryRow(ctx, periodCTE+`SELECT count(*) FILTER (WHERE cs.status = 'scanned')::float8, count(*) FILTER (WHERE cs.status = 'missed')::float8, count(*)::float8
			FROM checkpoint_scans cs JOIN tasks t ON t.id = cs.patrol_task_id
			WHERE t.status IN ('completed','closed') AND COALESCE(t.scheduled_start_at, t.created_at) >= `+d.from+` AND COALESCE(t.scheduled_start_at, t.created_at) < `+d.to+tW, a...).Scan(&cpScanned, &cpMissed, &cpTotal); err != nil {
			return err
		}
		eW := d.w("ea.property_id", "ea.location_id")
		var emgPeriod, emgActive float64
		if err := tx.QueryRow(ctx, periodCTE+`SELECT count(*) FILTER (WHERE ea.raised_at >= `+d.from+` AND ea.raised_at < `+d.to+`)::float8,
			count(*) FILTER (WHERE ea.status IN ('raised','acknowledged','responding'))::float8 FROM emergency_alerts ea WHERE true`+eW, a...).Scan(&emgPeriod, &emgActive); err != nil {
			return err
		}
		visitors, err := num(ctx, tx, `SELECT count(*)::float8 FROM visitors v WHERE v.checked_in_at >= `+d.today+` AND v.checked_in_at < `+d.tomorrow+d.w("v.property_id", "v.host_unit_location_id"), a)
		if err != nil {
			return err
		}
		patrolPct := pct1(patrolDone, patrolTotal)
		cpPct := pct1(cpScanned, cpTotal)
		emgSev := "normal"
		if emgActive > 0 {
			emgSev = "critical"
		}
		out.KPIs = []KPI{
			{Key: "incidents_today", Label: "Incidents Today", Value: today, Unit: "count", Severity: "normal", Hint: "Incident dilaporkan hari ini", DrillDown: d.link("/operations/incidents?reported=today")},
			{Key: "open_incidents", Label: "Open Incidents", Value: open, Unit: "count", Severity: sevAbove(open, 5, 15), Hint: "Incident belum resolved/closed", DrillDown: d.link("/operations/incidents?open=true")},
			{Key: "critical_incidents", Label: "Critical Incidents", Value: critical, Unit: "count", Severity: sevAbove(critical, 1, 1), Hint: "Incident terbuka dengan severity critical", DrillDown: d.link("/operations/incidents?open=true&severity=critical")},
			{Key: "patrol_completion", Label: "Patrol Completion", Value: patrolPct, Unit: "pct", Severity: sevBelow(patrolPct, 90, 70, patrolTotal > 0), Hint: "Patrol selesai ÷ patrol terjadwal dalam periode", DrillDown: d.link("/security/patrol?from=" + d.fromDate + "&to=" + d.toDate)},
			{Key: "checkpoint_compliance", Label: "Checkpoint Compliance", Value: cpPct, Unit: "pct", Severity: sevBelow(cpPct, 95, 80, cpTotal > 0), Hint: "Checkpoint discan ÷ seluruh checkpoint pada patrol yang selesai dalam periode", DrillDown: d.link("/reports/security?from=" + d.fromDate + "&to=" + d.toDate)},
			{Key: "missed_checkpoints", Label: "Missed Checkpoint", Value: cpMissed, Unit: "count", Severity: sevAbove(cpMissed, 1, 10), Hint: "Checkpoint terlewat pada patrol dalam periode", DrillDown: d.link("/reports/security?from=" + d.fromDate + "&to=" + d.toDate)},
			{Key: "security_response_time", Label: "Security Response Time", Value: math.Round(respMin*10) / 10, Unit: "minutes", Severity: sevAbove(respMin, 15, 30), Hint: "Rata-rata menit dari incident dilaporkan hingga direspons (SLA response)", DrillDown: d.link("/reports/incidents?from=" + d.fromDate + "&to=" + d.toDate)},
			{Key: "emergency_events", Label: "Emergency Events", Value: emgPeriod, Unit: "count", Severity: emgSev, Hint: fmt.Sprintf("Emergency Alert dalam periode; %d aktif sekarang", int(emgActive)), DrillDown: d.link("/security/emergency")},
			{Key: "active_emergencies", Label: "Active Emergency", Value: emgActive, Unit: "count", Severity: emgSev, Hint: "Emergency Alert belum resolved", DrillDown: d.link("/security/emergency?status=raised,acknowledged,responding")},
			{Key: "visitor_volume", Label: "Visitor Volume", Value: visitors, Unit: "count", Severity: "normal", Hint: "Tamu check-in hari ini", DrillDown: d.link("/security/visitors?date=today")},
			{Key: "unauthorized_entry", Label: "Unauthorized Entry", Value: unauthorized, Unit: "count", Severity: sevAbove(unauthorized, 1, 3), Hint: "Incident kategori unauthorized_access dalam periode", DrillDown: d.link("/operations/incidents?category=unauthorized_access&from=" + d.fromDate + "&to=" + d.toDate)},
		}
		if out.Breakdowns["route"], err = rows(ctx, tx, `SELECT r.id::text, r.name, count(DISTINCT t.id) FILTER (WHERE t.status <> 'cancelled')::float8, count(DISTINCT t.id) FILTER (WHERE t.status IN ('completed','closed'))::float8,
				count(cs.id) FILTER (WHERE cs.status = 'scanned' AND t.status IN ('completed','closed'))::float8, count(cs.id) FILTER (WHERE cs.status = 'missed')::float8
			FROM tasks t JOIN patrol_tasks pt ON pt.task_id = t.id JOIN patrol_routes r ON r.id = pt.route_id LEFT JOIN checkpoint_scans cs ON cs.patrol_task_id = t.id
			WHERE t.task_type = 'patrol' AND COALESCE(t.scheduled_start_at, t.created_at) >= `+d.from+` AND COALESCE(t.scheduled_start_at, t.created_at) < `+d.to+tW+` GROUP BY r.id, r.name ORDER BY 2`, a,
			[]string{"patrols", "completed", "checkpoints_scanned", "checkpoints_missed"}, func(k string) string { return "/security/patrol-routes/" + k }); err != nil {
			return err
		}
		if out.Breakdowns["incident_category"], err = rows(ctx, tx, `SELECT i.category, i.category, count(*)::float8, count(*) FILTER (WHERE i.status NOT IN ('resolved','closed','cancelled'))::float8,
				count(*) FILTER (WHERE i.severity = 'critical')::float8
			FROM incidents i WHERE i.reported_at >= `+d.from+` AND i.reported_at < `+d.to+iW+` GROUP BY 1 ORDER BY 3 DESC`, a,
			[]string{"reported", "open", "critical"}, func(k string) string {
				return "/operations/incidents?category=" + k + "&from=" + d.fromDate + "&to=" + d.toDate
			}); err != nil {
			return err
		}
		if out.Breakdowns["building"], err = rows(ctx, tx, `SELECT b.id::text, b.name, count(*)::float8, count(*) FILTER (WHERE x.severity = 'critical')::float8
			FROM (SELECT `+buildingOf("i.location_id")+` AS bid, i.severity FROM incidents i WHERE i.status NOT IN ('resolved','closed','cancelled') AND i.location_id IS NOT NULL`+iW+`) x
			JOIN locations b ON b.id = x.bid GROUP BY b.id, b.name ORDER BY 3 DESC`, a,
			[]string{"open_incidents", "critical"}, func(k string) string { return "/operations/incidents?open=true&location_id=" + k }); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.attachAttention(ctx, out, "security")
	return out, nil
}

// ---------- Housekeeping Dashboard ----------

func (s *Service) HousekeepingDashboard(ctx context.Context, in DashboardParams) (*DomainDashboard, error) {
	var out *DomainDashboard
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		d, o, err := s.newDashboard(ctx, tx, "housekeeping", "housekeeping.cleaning.view", in)
		if err != nil {
			return err
		}
		out = o
		a := d.sq.args
		tW := d.w("t.property_id", "t.location_id")
		var todayTotal, todayDone float64
		if err := tx.QueryRow(ctx, periodCTE+`SELECT count(*) FILTER (WHERE t.status <> 'cancelled')::float8, count(*) FILTER (WHERE t.status IN ('completed','closed'))::float8
			FROM tasks t WHERE t.task_type = 'cleaning' AND COALESCE(t.scheduled_start_at, t.created_at) >= `+d.today+` AND COALESCE(t.scheduled_start_at, t.created_at) < `+d.tomorrow+tW, a...).Scan(&todayTotal, &todayDone); err != nil {
			return err
		}
		var score, inspected float64
		if err := tx.QueryRow(ctx, periodCTE+`SELECT avg(hi.score)::float8, count(*)::float8 FROM housekeeping_inspections hi JOIN tasks t ON t.id = hi.task_id
			WHERE hi.score IS NOT NULL AND t.completed_at >= `+d.from+` AND t.completed_at < `+d.to+tW, a...).Scan(&score, &inspected); err != nil {
			// avg NULL bila tidak ada inspeksi
			score, inspected = 0, 0
		}
		var completed, onTime, withDue, reworked, inspectedTasks float64
		if err := tx.QueryRow(ctx, periodCTE+`SELECT count(*)::float8, count(*) FILTER (WHERE t.due_at IS NOT NULL AND t.completed_at <= t.due_at)::float8, count(*) FILTER (WHERE t.due_at IS NOT NULL)::float8,
			count(*) FILTER (WHERE ct.inspection_status IN ('rework_required','failed'))::float8, count(*) FILTER (WHERE ct.inspection_status IN ('passed','rework_required','failed'))::float8
			FROM tasks t JOIN cleaning_tasks ct ON ct.task_id = t.id WHERE t.task_type = 'cleaning' AND t.status IN ('completed','closed') AND t.completed_at >= `+d.from+` AND t.completed_at < `+d.to+tW, a...).
			Scan(&completed, &onTime, &withDue, &reworked, &inspectedTasks); err != nil {
			return err
		}
		findings, err := num(ctx, tx, `SELECT count(*)::float8 FROM findings f WHERE f.created_at >= `+d.from+` AND f.created_at < `+d.to+`
			AND (f.finding_type = 'housekeeping' OR (f.source_type = 'task' AND EXISTS (SELECT 1 FROM tasks st WHERE st.id = f.source_id AND st.task_type = 'cleaning'))
			     OR (f.source_type = 'task' AND EXISTS (SELECT 1 FROM housekeeping_inspections hi WHERE hi.task_id = f.source_id)))`+d.w("f.property_id", "f.location_id"), a)
		if err != nil {
			return err
		}
		// Area condition: rata-rata skor inspeksi per area dalam periode — Good ≥ 90, Fair 70–89, Poor < 70
		if out.Breakdowns["area"], err = rows(ctx, tx, `SELECT l.id::text, l.name, round(avg(hi.score))::float8, count(*)::float8,
				CASE WHEN avg(hi.score) >= 90 THEN 2 WHEN avg(hi.score) >= 70 THEN 1 ELSE 0 END::float8
			FROM housekeeping_inspections hi JOIN tasks t ON t.id = hi.task_id JOIN locations l ON l.id = t.location_id
			WHERE hi.score IS NOT NULL AND t.completed_at >= `+d.from+` AND t.completed_at < `+d.to+tW+` GROUP BY l.id, l.name ORDER BY 3, 2 LIMIT 50`, a,
			[]string{"avg_score", "inspections", "condition"}, func(k string) string { return "/housekeeping/inspections?location_id=" + k }); err != nil {
			return err
		}
		good := 0.0
		for _, r := range out.Breakdowns["area"] {
			if r.Values["condition"] == 2 {
				good++
			}
		}
		areas := float64(len(out.Breakdowns["area"]))
		onDuty, scheduled := 0.0, 0.0
		dutyKnown := false
		if s.Workforce != nil {
			if b, err := s.Workforce.OnDuty(ctx, "housekeeping", in.PropertyID); err == nil {
				onDuty, scheduled, dutyKnown = float64(b.OnDuty), float64(b.Scheduled), true
			}
		}
		completion := pct1(todayDone, todayTotal)
		scoreR := math.Round(score*10) / 10
		finding := pct1(findings, completed)
		rework := pct1(reworked, inspectedTasks)
		area := pct1(good, areas)
		sla := pct1(onTime, withDue)
		dutySev := "normal"
		if dutyKnown && onDuty < scheduled {
			dutySev = "warning"
		}
		out.KPIs = []KPI{
			{Key: "cleaning_tasks_today", Label: "Cleaning Tasks Today", Value: todayTotal, Unit: "count", Severity: "normal", Hint: "Cleaning task terjadwal hari ini (tanpa cancelled)", DrillDown: d.link("/housekeeping/cleaning?date=today")},
			{Key: "completion_rate", Label: "Completion Rate", Value: completion, Unit: "pct", Severity: "normal", Hint: "Cleaning task hari ini yang sudah selesai", DrillDown: d.link("/housekeeping/cleaning?date=today&status=completed,closed")},
			{Key: "inspection_score", Label: "Inspection Score", Value: scoreR, Unit: "score", Severity: sevBelow(scoreR, 90, 70, inspected > 0), Hint: "Rata-rata skor inspeksi housekeeping (0–100) dalam periode", DrillDown: d.link("/housekeeping/inspections?from=" + d.fromDate + "&to=" + d.toDate)},
			{Key: "finding_rate", Label: "Finding Rate", Value: finding, Unit: "pct", Severity: sevAbove(finding, 10, 25), Hint: "Finding housekeeping per 100 cleaning task selesai dalam periode", DrillDown: d.link("/findings?type=housekeeping&from=" + d.fromDate + "&to=" + d.toDate)},
			{Key: "rework_rate", Label: "Rework Rate", Value: rework, Unit: "pct", Severity: sevAbove(rework, 10, 20), Hint: "Cleaning task terinspeksi yang gagal / perlu rework dalam periode", DrillDown: d.link("/housekeeping/inspections?result=fail,partial&from=" + d.fromDate + "&to=" + d.toDate)},
			{Key: "staff_on_duty", Label: "Staff On Duty", Value: onDuty, Unit: "count", Severity: dutySev, Hint: fmt.Sprintf("Staf housekeeping on-duty sekarang dari %d terjadwal", int(scheduled)), DrillDown: d.link("/housekeeping/shifts?tab=on-duty")},
			{Key: "area_condition", Label: "Area Condition", Value: area, Unit: "pct", Severity: sevBelow(area, 80, 60, areas > 0), Hint: "Area terinspeksi dengan kondisi Good (rata-rata skor ≥ 90)", DrillDown: d.link("/housekeeping/inspections?from=" + d.fromDate + "&to=" + d.toDate)},
			{Key: "cleaning_sla_compliance", Label: "Cleaning SLA Compliance", Value: sla, Unit: "pct", Severity: sevBelow(sla, 90, 75, withDue > 0), Hint: "Cleaning task selesai sebelum tenggat ÷ selesai bertenggat dalam periode", DrillDown: d.link("/reports/patrol-cleaning?from=" + d.fromDate + "&to=" + d.toDate)},
		}
		if out.Breakdowns["team"], err = rows(ctx, tx, `SELECT tm.id::text, tm.name, count(*) FILTER (WHERE t.status <> 'cancelled')::float8, count(*) FILTER (WHERE t.status IN ('completed','closed'))::float8,
				count(*) FILTER (WHERE t.status NOT IN ('completed','closed','cancelled') AND t.due_at < now())::float8
			FROM tasks t JOIN teams tm ON tm.id = t.assignee_team_id WHERE t.task_type = 'cleaning' AND COALESCE(t.scheduled_start_at, t.created_at) >= `+d.today+` AND COALESCE(t.scheduled_start_at, t.created_at) < `+d.tomorrow+tW+`
			GROUP BY tm.id, tm.name ORDER BY 3 DESC`, a, []string{"tasks_today", "completed", "overdue"}, func(k string) string { return "/housekeeping/cleaning?date=today&team_id=" + k }); err != nil {
			return err
		}
		if out.Breakdowns["routes_today"], err = rows(ctx, tx, `SELECT rr.id::text, r.route_code || ' ' || r.name, rr.total_stops::float8, rr.completed_stops::float8,
				CASE rr.status WHEN 'completed' THEN 2 WHEN 'in_progress' THEN 1 ELSE 0 END::float8
			FROM cleaning_route_runs rr JOIN cleaning_routes r ON r.id = rr.route_id
			WHERE rr.run_date = (`+d.today+`::timestamptz AT TIME ZONE (SELECT pr.timezone FROM properties pr WHERE pr.location_id = rr.property_id))::date AND rr.status <> 'cancelled'`+d.w("rr.property_id", "")+` ORDER BY r.start_time, r.name`, a,
			[]string{"total_stops", "completed_stops", "status"}, func(k string) string { return "/housekeeping/routes?run=" + k }); err != nil {
			return err
		}
		if out.Breakdowns["consumables"], err = rows(ctx, tx, `SELECT i.id::text, i.name, sum(c.quantity)::float8, COALESCE(sum(c.total_cost),0)::float8
			FROM task_consumables c JOIN tasks t ON t.id = c.task_id JOIN inventory_items i ON i.id = c.item_id
			WHERE c.recorded_at >= `+d.from+` AND c.recorded_at < `+d.to+tW+` GROUP BY i.id, i.name ORDER BY 4 DESC LIMIT 10`, a,
			[]string{"quantity", "total_cost"}, func(k string) string { return "/inventory?item=" + k }); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.attachAttention(ctx, out, "housekeeping")
	return out, nil
}
