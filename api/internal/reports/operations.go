package reports

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ---------- PRD P1 v2 §41 Operational Reporting & §51 Success Metrics ----------
// tasks: Task Completion Report · sla: SLA Report (task, WO, SR, incident) · incidents: Incident Report ·
// backlog: Operational Backlog Report · operations-kpi: KPI P1 (target roadmap).

// Task Completion Report: dibuat/selesai/tepat waktu, completion rate, overdue rate, per kategori/tipe/team/assignee.
func (s *Service) tasks(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("tasks", p)
	sc, args, err := s.scope(ctx, p, "t.property_id", "t.location_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*) FILTER (WHERE t.created_at >= $1 AND t.created_at < $2)::float8,
		count(*) FILTER (WHERE t.completed_at >= $1 AND t.completed_at < $2)::float8,
		count(*) FILTER (WHERE t.completed_at >= $1 AND t.completed_at < $2 AND (t.due_at IS NULL OR t.completed_at <= t.due_at))::float8,
		count(*) FILTER (WHERE t.created_at >= $1 AND t.created_at < $2 AND t.status IN ('completed','closed'))::float8,
		count(*) FILTER (WHERE t.created_at >= $1 AND t.created_at < $2 AND t.status = 'cancelled')::float8,
		count(*) FILTER (WHERE t.status NOT IN ('completed','closed','cancelled'))::float8,
		count(*) FILTER (WHERE t.status NOT IN ('completed','closed','cancelled') AND (t.is_overdue OR t.due_at < now()))::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (t.completed_at - COALESCE(t.started_at, t.created_at)))/3600) FILTER (WHERE t.completed_at >= $1 AND t.completed_at < $2), 0)::float8,
		count(*) FILTER (WHERE t.completed_at >= $1 AND t.completed_at < $2 AND t.reopen_count > 0)::float8,
		count(*) FILTER (WHERE t.created_at >= $1 AND t.created_at < $2 AND t.escalation_level > 0)::float8
		FROM tasks t WHERE %s`, sc), []string{"created", "completed", "completed_on_time", "created_completed", "created_cancelled", "open_now", "overdue_now", "avg_completion_hours", "reopened", "escalated"}, args...)
	if err != nil {
		return nil, err
	}
	// completion rate: task yang dibuat pada periode dan sudah selesai (tanpa cancelled)
	sum["completion_rate_pct"] = pct(sum["created_completed"], sum["created"]-sum["created_cancelled"])
	sum["on_time_pct"] = pct(sum["completed_on_time"], sum["completed"])
	sum["overdue_rate_pct"] = pct(sum["overdue_now"], sum["open_now"])
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date, count(t.id) FILTER (WHERE t.created_at >= d AND t.created_at < d + interval '1 day')::float8, count(t.id) FILTER (WHERE t.completed_at >= d AND t.completed_at < d + interval '1 day')::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d LEFT JOIN tasks t ON (%s) AND ((t.created_at >= d AND t.created_at < d + interval '1 day') OR (t.completed_at >= d AND t.completed_at < d + interval '1 day'))
		GROUP BY d ORDER BY d`, sc), []string{"created", "completed"}, args...); err != nil {
		return nil, err
	}
	bd := func(key, keyExpr, labelExpr, join string) error {
		var err error
		r.Breakdowns[key], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT %s, %s, count(*)::float8, count(*) FILTER (WHERE t.status IN ('completed','closed'))::float8,
			count(*) FILTER (WHERE t.completed_at IS NOT NULL AND (t.due_at IS NULL OR t.completed_at <= t.due_at))::float8,
			count(*) FILTER (WHERE t.status NOT IN ('completed','closed','cancelled') AND (t.is_overdue OR t.due_at < now()))::float8
			FROM tasks t %s WHERE t.created_at >= $1 AND t.created_at < $2 AND %s GROUP BY 1, 2 ORDER BY count(*) DESC LIMIT 20`, keyExpr, labelExpr, join, sc), []string{"count", "completed", "completed_on_time", "overdue"}, args...)
		return err
	}
	if err := bd("category", "COALESCE(t.category,'(none)')", "COALESCE(t.category,'(tanpa kategori)')", ""); err != nil {
		return nil, err
	}
	if err := bd("type", "t.task_type", "t.task_type", ""); err != nil {
		return nil, err
	}
	if err := bd("priority", "t.priority", "t.priority", ""); err != nil {
		return nil, err
	}
	if err := bd("team", "tm.id::text", "tm.name", "JOIN teams tm ON tm.id = t.assignee_team_id"); err != nil {
		return nil, err
	}
	if err := bd("assignee", "u.id::text", "u.full_name", "JOIN users u ON u.id = t.assignee_user_id"); err != nil {
		return nil, err
	}
	return r, nil
}

// slaObjects: object ber-SLA (PRD §21) — tabel, kolom nomor, status selesai.
var slaObjects = []struct{ ot, table, done string }{
	{"task", "tasks", "'completed','closed'"},
	{"work_order", "work_orders", "'completed','closed'"},
	{"service_request", "service_requests", "'resolved','closed'"},
	{"incident", "incidents", "'resolved','closed'"},
}

// SLA Report: per object type — selesai dalam SLA (resolution), respons tepat waktu, at risk/breached saat ini.
func (s *Service) sla(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("sla", p)
	sc, args, err := s.scope(ctx, p, "x.property_id", "x.location_id")
	if err != nil {
		return nil, err
	}
	total := map[string]float64{}
	var byObj []Point
	for _, o := range slaObjects {
		v, err := summary(ctx, tx, fmt.Sprintf(`SELECT
			count(*) FILTER (WHERE st.resolved_at >= $1 AND st.resolved_at < $2 AND st.resolution_due_at IS NOT NULL)::float8,
			count(*) FILTER (WHERE st.resolved_at >= $1 AND st.resolved_at < $2 AND st.resolution_due_at IS NOT NULL AND st.resolved_at <= st.resolution_due_at + (st.paused_minutes || ' minutes')::interval)::float8,
			count(*) FILTER (WHERE st.started_at >= $1 AND st.started_at < $2 AND st.response_due_at IS NOT NULL AND st.responded_at IS NOT NULL)::float8,
			count(*) FILTER (WHERE st.started_at >= $1 AND st.started_at < $2 AND st.response_due_at IS NOT NULL AND st.responded_at IS NOT NULL AND st.responded_at <= st.response_due_at)::float8,
			count(*) FILTER (WHERE x.status NOT IN (%[2]s,'cancelled') AND x.sla_breached_at IS NULL AND x.sla_risk_at IS NOT NULL)::float8,
			count(*) FILTER (WHERE x.status NOT IN (%[2]s,'cancelled') AND x.sla_breached_at IS NOT NULL)::float8,
			count(*) FILTER (WHERE x.status NOT IN (%[2]s,'cancelled') AND x.sla_breached_at IS NULL AND x.sla_risk_at IS NULL)::float8,
			count(*) FILTER (WHERE st.sla_breached_at >= $1 AND st.sla_breached_at < $2)::float8
			FROM sla_tracking st JOIN %[1]s x ON x.id = st.object_id WHERE st.object_type = '%[3]s' AND %[4]s`, o.table, o.done, o.ot, sc),
			[]string{"resolved", "resolved_within_sla", "responded", "responded_within_sla", "at_risk_now", "breached_now", "on_track_now", "breached_in_period"}, args...)
		if err != nil {
			return nil, err
		}
		v["resolution_compliance_pct"] = pct(v["resolved_within_sla"], v["resolved"])
		v["response_compliance_pct"] = pct(v["responded_within_sla"], v["responded"])
		byObj = append(byObj, Point{Key: o.ot, Label: o.ot, Values: v})
		for k, val := range v {
			total[k] += val
		}
	}
	total["resolution_compliance_pct"] = pct(total["resolved_within_sla"], total["resolved"])
	total["response_compliance_pct"] = pct(total["responded_within_sla"], total["responded"])
	r.Summary = total
	r.Breakdowns["object_type"] = byObj
	// per prioritas (semua object)
	prio, err := breakdown(ctx, tx, fmt.Sprintf(`WITH x AS (
		SELECT 'task' AS ot, id, property_id, location_id, priority FROM tasks UNION ALL SELECT 'work_order', id, property_id, location_id, priority FROM work_orders
		UNION ALL SELECT 'service_request', id, property_id, location_id, priority FROM service_requests UNION ALL SELECT 'incident', id, property_id, location_id, priority FROM incidents)
		SELECT x.priority, x.priority, count(*)::float8,
		  count(*) FILTER (WHERE st.resolved_at <= st.resolution_due_at + (st.paused_minutes || ' minutes')::interval)::float8,
		  count(*) FILTER (WHERE st.sla_breached_at IS NOT NULL)::float8
		FROM sla_tracking st JOIN x ON x.id = st.object_id AND x.ot = st.object_type
		WHERE st.resolved_at >= $1 AND st.resolved_at < $2 AND st.resolution_due_at IS NOT NULL AND %s GROUP BY x.priority
		ORDER BY CASE x.priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END`, sc), []string{"resolved", "resolved_within_sla", "breached"}, args...)
	if err != nil {
		return nil, err
	}
	r.Breakdowns["priority"] = prio
	// PRD P2 v2.1 P2-WKL-02: SLA per team (task & WO)
	if r.Breakdowns["team"], err = breakdown(ctx, tx, fmt.Sprintf(`WITH x AS (
		SELECT 'task' AS ot, id, property_id, location_id, assignee_team_id FROM tasks UNION ALL SELECT 'work_order', id, property_id, location_id, assignee_team_id FROM work_orders)
		SELECT COALESCE(tm.id::text,'-'), COALESCE(tm.name,'(tanpa team)'), count(*)::float8,
		  count(*) FILTER (WHERE st.resolved_at <= st.resolution_due_at + (st.paused_minutes || ' minutes')::interval)::float8,
		  count(*) FILTER (WHERE st.sla_breached_at IS NOT NULL)::float8
		FROM sla_tracking st JOIN x ON x.id = st.object_id AND x.ot = st.object_type LEFT JOIN teams tm ON tm.id = x.assignee_team_id
		WHERE st.resolved_at >= $1 AND st.resolved_at < $2 AND st.resolution_due_at IS NOT NULL AND %s GROUP BY 1, 2 ORDER BY count(*) DESC`, sc), []string{"resolved", "resolved_within_sla", "breached"}, args...); err != nil {
		return nil, err
	}
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`WITH x AS (
		SELECT id, property_id, location_id FROM tasks UNION ALL SELECT id, property_id, location_id FROM work_orders UNION ALL SELECT id, property_id, location_id FROM service_requests UNION ALL SELECT id, property_id, location_id FROM incidents)
		SELECT d::date, count(st.object_id) FILTER (WHERE st.resolved_at >= d AND st.resolved_at < d + interval '1 day')::float8,
		  count(st.object_id) FILTER (WHERE st.resolved_at >= d AND st.resolved_at < d + interval '1 day' AND st.resolved_at <= st.resolution_due_at + (st.paused_minutes || ' minutes')::interval)::float8,
		  count(st.object_id) FILTER (WHERE st.sla_breached_at >= d AND st.sla_breached_at < d + interval '1 day')::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d
		LEFT JOIN (sla_tracking st JOIN x ON x.id = st.object_id AND (%s)) ON ((st.resolved_at >= d AND st.resolved_at < d + interval '1 day') OR (st.sla_breached_at >= d AND st.sla_breached_at < d + interval '1 day'))
		GROUP BY d ORDER BY d`, sc), []string{"resolved", "resolved_within_sla", "breached"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// Incident Report: volume, severity, kategori, waktu penyelesaian, SLA breach.
func (s *Service) incidents(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("incidents", p)
	sc, args, err := s.scope(ctx, p, "i.property_id", "i.location_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*) FILTER (WHERE i.reported_at >= $1 AND i.reported_at < $2)::float8,
		count(*) FILTER (WHERE i.reported_at >= $1 AND i.reported_at < $2 AND i.severity = 'critical')::float8,
		count(*) FILTER (WHERE i.reported_at >= $1 AND i.reported_at < $2 AND i.severity = 'high')::float8,
		count(*) FILTER (WHERE i.resolved_at >= $1 AND i.resolved_at < $2)::float8,
		count(*) FILTER (WHERE i.status NOT IN ('resolved','closed','cancelled'))::float8,
		count(*) FILTER (WHERE i.status NOT IN ('resolved','closed','cancelled') AND i.severity = 'critical')::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (i.resolved_at - i.reported_at))/3600) FILTER (WHERE i.resolved_at >= $1 AND i.resolved_at < $2), 0)::float8,
		count(*) FILTER (WHERE i.reported_at >= $1 AND i.reported_at < $2 AND i.sla_breached_at IS NOT NULL)::float8
		FROM incidents i WHERE %s`, sc), []string{"reported", "critical", "high", "resolved", "open_now", "critical_open_now", "avg_resolution_hours", "sla_breached"}, args...)
	if err != nil {
		return nil, err
	}
	sum["resolution_rate_pct"] = pct(sum["resolved"], sum["reported"])
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date, count(i.id) FILTER (WHERE i.reported_at >= d AND i.reported_at < d + interval '1 day')::float8, count(i.id) FILTER (WHERE i.resolved_at >= d AND i.resolved_at < d + interval '1 day')::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d LEFT JOIN incidents i ON (%s) AND ((i.reported_at >= d AND i.reported_at < d + interval '1 day') OR (i.resolved_at >= d AND i.resolved_at < d + interval '1 day'))
		GROUP BY d ORDER BY d`, sc), []string{"reported", "resolved"}, args...); err != nil {
		return nil, err
	}
	for key, col := range map[string]string{"severity": "i.severity", "category": "i.category", "type": "i.incident_type", "status": "i.status"} {
		if r.Breakdowns[key], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT %[1]s, %[1]s, count(*)::float8, count(*) FILTER (WHERE i.resolved_at IS NOT NULL)::float8,
			COALESCE(avg(EXTRACT(EPOCH FROM (i.resolved_at - i.reported_at))/3600) FILTER (WHERE i.resolved_at IS NOT NULL), 0)::float8
			FROM incidents i WHERE i.reported_at >= $1 AND i.reported_at < $2 AND %[2]s GROUP BY 1 ORDER BY count(*) DESC`, col, sc), []string{"count", "resolved", "avg_resolution_hours"}, args...); err != nil {
			return nil, err
		}
	}
	if r.Breakdowns["location"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT l.id::text, l.name, count(*)::float8 FROM incidents i JOIN locations l ON l.id = i.location_id
		WHERE i.reported_at >= $1 AND i.reported_at < $2 AND %s GROUP BY l.id, l.name ORDER BY count(*) DESC LIMIT 10`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// Operational Backlog Report: pekerjaan terbuka saat ini (task, WO, SR, incident) per umur, prioritas, team, status.
// Snapshot "sekarang"; from/to hanya dipakai untuk seri masuk vs keluar (arus backlog).
func (s *Service) backlog(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("backlog", p)
	sc, args, err := s.scope(ctx, p, "x.property_id", "x.location_id")
	if err != nil {
		return nil, err
	}
	const openX = `WITH x AS (
		SELECT 'task' AS ot, id, property_id, location_id, priority, status, created_at, due_at, assignee_team_id, assignee_user_id, (is_overdue OR due_at < now()) AS overdue, sla_breached_at FROM tasks WHERE status NOT IN ('completed','closed','cancelled')
		UNION ALL SELECT 'work_order', id, property_id, location_id, priority, status, created_at, due_at, assignee_team_id, assignee_user_id, (is_overdue OR due_at < now()), sla_breached_at FROM work_orders WHERE status NOT IN ('completed','closed','cancelled','draft')
		UNION ALL SELECT 'service_request', id, property_id, location_id, priority, status, created_at, NULL, assignee_team_id, assignee_user_id, false, sla_breached_at FROM service_requests WHERE status NOT IN ('resolved','closed','cancelled')
		UNION ALL SELECT 'incident', id, property_id, location_id, severity, status, reported_at, NULL, assignee_team_id, assignee_user_id, false, sla_breached_at FROM incidents WHERE status NOT IN ('resolved','closed','cancelled'))`
	sum, err := summary(ctx, tx, fmt.Sprintf(openX+`SELECT count(*)::float8,
		count(*) FILTER (WHERE x.ot = 'task')::float8, count(*) FILTER (WHERE x.ot = 'work_order')::float8,
		count(*) FILTER (WHERE x.ot = 'service_request')::float8, count(*) FILTER (WHERE x.ot = 'incident')::float8,
		count(*) FILTER (WHERE x.overdue)::float8, count(*) FILTER (WHERE x.sla_breached_at IS NOT NULL)::float8,
		count(*) FILTER (WHERE x.assignee_team_id IS NULL AND x.assignee_user_id IS NULL)::float8,
		count(*) FILTER (WHERE x.priority = 'critical')::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (now() - x.created_at))/86400), 0)::float8,
		count(*) FILTER (WHERE x.created_at < now() - interval '7 days')::float8
		FROM x WHERE $1::timestamptz IS NOT NULL AND $2::timestamptz IS NOT NULL AND %s`, sc),
		[]string{"open", "tasks", "work_orders", "service_requests", "incidents", "overdue", "sla_breached", "unassigned", "critical", "avg_age_days", "older_than_7_days"}, args...)
	if err != nil {
		return nil, err
	}
	r.Summary = sum
	if r.Breakdowns["age"], err = breakdown(ctx, tx, fmt.Sprintf(openX+`SELECT b.k, b.k, count(x.id)::float8 FROM (VALUES ('0-1d',0,1),('1-3d',1,3),('3-7d',3,7),('7-30d',7,30),('>30d',30,100000)) b(k,lo,hi)
		LEFT JOIN x ON EXTRACT(EPOCH FROM (now() - x.created_at))/86400 >= b.lo AND EXTRACT(EPOCH FROM (now() - x.created_at))/86400 < b.hi AND $1::timestamptz IS NOT NULL AND $2::timestamptz IS NOT NULL AND %s
		GROUP BY b.k, b.lo ORDER BY b.lo`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["object_type"], err = breakdown(ctx, tx, fmt.Sprintf(openX+`SELECT x.ot, x.ot, count(*)::float8, count(*) FILTER (WHERE x.overdue)::float8, count(*) FILTER (WHERE x.sla_breached_at IS NOT NULL)::float8
		FROM x WHERE $1::timestamptz IS NOT NULL AND $2::timestamptz IS NOT NULL AND %s GROUP BY x.ot ORDER BY count(*) DESC`, sc), []string{"count", "overdue", "sla_breached"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["priority"], err = breakdown(ctx, tx, fmt.Sprintf(openX+`SELECT x.priority, x.priority, count(*)::float8, count(*) FILTER (WHERE x.overdue)::float8
		FROM x WHERE $1::timestamptz IS NOT NULL AND $2::timestamptz IS NOT NULL AND %s GROUP BY x.priority ORDER BY CASE x.priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END`, sc), []string{"count", "overdue"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["team"], err = breakdown(ctx, tx, fmt.Sprintf(openX+`SELECT COALESCE(tm.id::text,'-'), COALESCE(tm.name,'(tanpa team)'), count(*)::float8, count(*) FILTER (WHERE x.overdue)::float8
		FROM x LEFT JOIN teams tm ON tm.id = x.assignee_team_id WHERE $1::timestamptz IS NOT NULL AND $2::timestamptz IS NOT NULL AND %s GROUP BY 1, 2 ORDER BY count(*) DESC`, sc), []string{"count", "overdue"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["status"], err = breakdown(ctx, tx, fmt.Sprintf(openX+`SELECT x.ot || ':' || x.status, x.status, count(*)::float8 FROM x WHERE $1::timestamptz IS NOT NULL AND $2::timestamptz IS NOT NULL AND %s GROUP BY x.ot, x.status ORDER BY count(*) DESC`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	// arus backlog harian: masuk (dibuat) vs keluar (selesai) task + WO + SR
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`WITH y AS (
		SELECT property_id, location_id, created_at, completed_at AS done_at FROM tasks UNION ALL SELECT property_id, location_id, created_at, completed_at FROM work_orders WHERE status <> 'draft'
		UNION ALL SELECT property_id, location_id, created_at, resolved_at FROM service_requests UNION ALL SELECT property_id, location_id, reported_at, resolved_at FROM incidents)
		SELECT d::date, count(*) FILTER (WHERE y.created_at >= d AND y.created_at < d + interval '1 day')::float8, count(*) FILTER (WHERE y.done_at >= d AND y.done_at < d + interval '1 day')::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d LEFT JOIN y ON (%s) AND ((y.created_at >= d AND y.created_at < d + interval '1 day') OR (y.done_at >= d AND y.done_at < d + interval '1 day'))
		GROUP BY d ORDER BY d`, swapAlias(sc, "x", "y")), []string{"incoming", "completed"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// Operations KPI (PRD P1 v2 §51): nilai aktual periode vs target roadmap.
// Target: WO within SLA >90%, Task Completion Rate >90%, Overdue Task Rate <10%, Tenant Request within SLA >90%,
// Mobile Task Completion >80%, Operational Data Completeness >90%.
func (s *Service) operationsKPI(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("operations-kpi", p)
	sct, args, err := s.scope(ctx, p, "t.property_id", "t.location_id")
	if err != nil {
		return nil, err
	}
	scw := swapAlias(sct, "t", "w")
	scr := swapAlias(sct, "t", "sr")
	v := map[string]float64{}
	add := func(m map[string]float64) {
		for k, x := range m {
			v[k] = x
		}
	}
	m, err := summary(ctx, tx, fmt.Sprintf(`SELECT count(*) FILTER (WHERE st.resolution_due_at IS NOT NULL)::float8,
		count(*) FILTER (WHERE st.resolution_due_at IS NOT NULL AND st.resolved_at <= st.resolution_due_at + (st.paused_minutes || ' minutes')::interval)::float8
		FROM work_orders w JOIN sla_tracking st ON st.object_type = 'work_order' AND st.object_id = w.id WHERE w.completed_at >= $1 AND w.completed_at < $2 AND %s`, scw), []string{"wo_completed_with_sla", "wo_completed_within_sla"}, args...)
	if err != nil {
		return nil, err
	}
	add(m)
	m, err = summary(ctx, tx, fmt.Sprintf(`SELECT count(*) FILTER (WHERE t.status <> 'cancelled')::float8, count(*) FILTER (WHERE t.status IN ('completed','closed'))::float8,
		count(*) FILTER (WHERE t.status NOT IN ('cancelled') AND (t.is_overdue OR (t.due_at < now() AND t.status NOT IN ('completed','closed')) OR (t.completed_at > t.due_at)))::float8,
		count(*) FILTER (WHERE t.location_id IS NOT NULL AND (t.assignee_user_id IS NOT NULL OR t.assignee_team_id IS NOT NULL) AND t.due_at IS NOT NULL AND t.category IS NOT NULL)::float8,
		count(*)::float8
		FROM tasks t WHERE t.created_at >= $1 AND t.created_at < $2 AND %s`, sct), []string{"tasks_created", "tasks_completed", "tasks_overdue", "tasks_complete_data", "tasks_all"}, args...)
	if err != nil {
		return nil, err
	}
	add(m)
	m, err = summary(ctx, tx, fmt.Sprintf(`SELECT count(*) FILTER (WHERE st.resolution_due_at IS NOT NULL)::float8,
		count(*) FILTER (WHERE st.resolution_due_at IS NOT NULL AND st.resolved_at <= st.resolution_due_at + (st.paused_minutes || ' minutes')::interval)::float8
		FROM service_requests sr JOIN sla_tracking st ON st.object_type = 'service_request' AND st.object_id = sr.id WHERE sr.resolved_at >= $1 AND sr.resolved_at < $2 AND %s`, scr), []string{"sr_resolved_with_sla", "sr_resolved_within_sla"}, args...)
	if err != nil {
		return nil, err
	}
	add(m)
	// completion task lewat mobile: aktivitas complete ber-source mobile/sync
	m, err = summary(ctx, tx, fmt.Sprintf(`SELECT count(DISTINCT a.object_id)::float8, count(DISTINCT a.object_id) FILTER (WHERE a.source IN ('mobile','sync'))::float8
		FROM activities a JOIN tasks t ON t.id = a.object_id WHERE a.object_type = 'task' AND a.action = 'status_changed' AND a.to_value = 'completed'
		AND a.occurred_at >= $1 AND a.occurred_at < $2 AND %s`, sct), []string{"task_completions", "task_completions_mobile"}, args...)
	if err != nil {
		return nil, err
	}
	add(m)
	m, err = summary(ctx, tx, fmt.Sprintf(`SELECT count(*)::float8, count(*) FILTER (WHERE w.asset_id IS NOT NULL AND (w.assignee_user_id IS NOT NULL OR w.assignee_team_id IS NOT NULL OR w.vendor_id IS NOT NULL) AND w.due_at IS NOT NULL
		AND (w.status NOT IN ('completed','closed') OR (w.completion_notes IS NOT NULL OR w.resolution IS NOT NULL)))::float8
		FROM work_orders w WHERE w.created_at >= $1 AND w.created_at < $2 AND w.status <> 'draft' AND %s`, scw), []string{"wo_all", "wo_complete_data"}, args...)
	if err != nil {
		return nil, err
	}
	add(m)
	type kpi struct {
		key, label  string
		actual      float64
		target      float64
		lowerBetter bool
		basis       float64
	}
	kpis := []kpi{
		{"wo_within_sla", "Work Orders completed within SLA", pct(v["wo_completed_within_sla"], v["wo_completed_with_sla"]), 90, false, v["wo_completed_with_sla"]},
		{"task_completion_rate", "Task Completion Rate", pct(v["tasks_completed"], v["tasks_created"]), 90, false, v["tasks_created"]},
		{"overdue_task_rate", "Overdue Task Rate", pct(v["tasks_overdue"], v["tasks_created"]), 10, true, v["tasks_created"]},
		{"request_within_sla", "Tenant Request Resolution within SLA", pct(v["sr_resolved_within_sla"], v["sr_resolved_with_sla"]), 90, false, v["sr_resolved_with_sla"]},
		{"mobile_task_completion", "Mobile Task Completion", pct(v["task_completions_mobile"], v["task_completions"]), 80, false, v["task_completions"]},
		{"data_completeness", "Operational Data Completeness", pct(v["tasks_complete_data"]+v["wo_complete_data"], v["tasks_all"]+v["wo_all"]), 90, false, v["tasks_all"] + v["wo_all"]},
	}
	pts := make([]Point, 0, len(kpis))
	for _, k := range kpis {
		met := 0.0
		if k.basis > 0 && ((k.lowerBetter && k.actual < k.target) || (!k.lowerBetter && k.actual > k.target)) {
			met = 1
		}
		lb := 0.0
		if k.lowerBetter {
			lb = 1
		}
		pts = append(pts, Point{Key: k.key, Label: k.label, Values: map[string]float64{"actual_pct": k.actual, "target_pct": k.target, "met": met, "lower_is_better": lb, "basis": k.basis}})
		v[k.key+"_pct"] = k.actual
	}
	r.Summary = v
	r.Breakdowns["kpi"] = pts
	// seri harian: task dibuat vs selesai, WO selesai (tren penyelesaian)
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date,
		(SELECT count(*) FROM tasks t WHERE t.created_at >= d AND t.created_at < d + interval '1 day' AND %[1]s)::float8,
		(SELECT count(*) FROM tasks t WHERE t.completed_at >= d AND t.completed_at < d + interval '1 day' AND %[1]s)::float8,
		(SELECT count(*) FROM work_orders w WHERE w.completed_at >= d AND w.completed_at < d + interval '1 day' AND %[2]s)::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d ORDER BY d`, sct, scw), []string{"tasks_created", "tasks_completed", "work_orders_completed"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}
