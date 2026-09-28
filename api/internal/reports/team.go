package reports

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Team Performance Report (PRD P2 v2.1 P2-WKL-04; Roadmap v2.1 §10 Team Performance, §25.8 Operational Team Workload):
// per team & per staf dalam periode — selesai, tepat waktu, overdue terbuka, rework, rata-rata durasi, skor inspeksi HK,
// checkpoint terlewat (security), serta SLA breach per team (P2-WKL-02).
func (s *Service) teamPerformance(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("team-performance", p)
	sc, args, err := s.scope(ctx, p, "x.property_id", "x.location_id")
	if err != nil {
		return nil, err
	}
	// x: pekerjaan (task + WO) yang selesai dalam periode ATAU masih terbuka, dengan team (langsung atau team assignee user)
	const workX = `WITH x AS (
		SELECT 'task' AS ot, t.id, t.property_id, t.location_id, t.task_type AS sub_type, t.status, t.assignee_user_id,
		  COALESCE(t.assignee_team_id, (SELECT tm.team_id FROM team_members tm JOIN teams tt ON tt.id = tm.team_id WHERE tm.user_id = t.assignee_user_id AND tt.is_active ORDER BY tt.property_id NULLS LAST LIMIT 1)) AS team_id,
		  t.completed_at, t.started_at, t.due_at, (t.is_overdue OR t.due_at < now()) AS overdue, t.sla_breached_at, t.source_type,
		  (SELECT pt.missed_checkpoints FROM patrol_tasks pt WHERE pt.task_id = t.id) AS missed,
		  (SELECT hi.score FROM housekeeping_inspections hi WHERE hi.cleaning_task_id = t.id ORDER BY hi.task_id DESC LIMIT 1) AS hk_score
		FROM tasks t
		UNION ALL
		SELECT 'work_order', w.id, w.property_id, w.location_id, w.work_order_type, w.status, w.assignee_user_id,
		  COALESCE(w.assignee_team_id, (SELECT tm.team_id FROM team_members tm JOIN teams tt ON tt.id = tm.team_id WHERE tm.user_id = w.assignee_user_id AND tt.is_active ORDER BY tt.property_id NULLS LAST LIMIT 1)),
		  w.completed_at, w.started_at, w.due_at, (w.is_overdue OR w.due_at < now()), w.sla_breached_at, w.source_type, NULL::int, NULL::int
		FROM work_orders w WHERE w.status <> 'draft')`
	cols := []string{"completed", "completed_on_time", "on_time_pct", "open_now", "overdue_now", "rework", "sla_breached", "avg_completion_hours", "missed_checkpoints", "avg_inspection_score"}
	metrics := `count(*) FILTER (WHERE x.completed_at >= $1 AND x.completed_at < $2)::float8,
		count(*) FILTER (WHERE x.completed_at >= $1 AND x.completed_at < $2 AND (x.due_at IS NULL OR x.completed_at <= x.due_at))::float8,
		COALESCE(round(100.0 * count(*) FILTER (WHERE x.completed_at >= $1 AND x.completed_at < $2 AND (x.due_at IS NULL OR x.completed_at <= x.due_at))
		  / NULLIF(count(*) FILTER (WHERE x.completed_at >= $1 AND x.completed_at < $2), 0), 1), 0)::float8,
		count(*) FILTER (WHERE x.status NOT IN ('completed','closed','cancelled'))::float8,
		count(*) FILTER (WHERE x.status NOT IN ('completed','closed','cancelled') AND x.overdue)::float8,
		count(*) FILTER (WHERE x.ot = 'task' AND x.source_type = 'finding' AND x.sub_type = 'cleaning' AND x.completed_at >= $1 AND x.completed_at < $2)::float8,
		count(*) FILTER (WHERE x.sla_breached_at >= $1 AND x.sla_breached_at < $2)::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (x.completed_at - x.started_at))/3600) FILTER (WHERE x.completed_at >= $1 AND x.completed_at < $2 AND x.started_at IS NOT NULL), 0)::float8,
		COALESCE(sum(x.missed) FILTER (WHERE x.completed_at >= $1 AND x.completed_at < $2), 0)::float8,
		COALESCE(avg(x.hk_score) FILTER (WHERE x.completed_at >= $1 AND x.completed_at < $2), 0)::float8`
	sum, err := summary(ctx, tx, fmt.Sprintf(workX+`SELECT %s FROM x WHERE (x.completed_at >= $1 OR x.status NOT IN ('completed','closed','cancelled')) AND %s`, metrics, sc), cols, args...)
	if err != nil {
		return nil, err
	}
	r.Summary = sum
	if r.Breakdowns["team"], err = breakdown(ctx, tx, fmt.Sprintf(workX+`SELECT COALESCE(tm.id::text,'-'), COALESCE(tm.name || ' (' || tm.domain || ')', '(tanpa team)'), %s
		FROM x LEFT JOIN teams tm ON tm.id = x.team_id WHERE (x.completed_at >= $1 OR x.status NOT IN ('completed','closed','cancelled')) AND %s
		GROUP BY 1, 2 ORDER BY count(*) FILTER (WHERE x.completed_at >= $1 AND x.completed_at < $2) DESC`, metrics, sc), cols, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["staff"], err = breakdown(ctx, tx, fmt.Sprintf(workX+`SELECT COALESCE(u.id::text,'-'), COALESCE(u.full_name, '(belum ditugaskan)'), %s
		FROM x LEFT JOIN users u ON u.id = x.assignee_user_id WHERE (x.completed_at >= $1 OR x.status NOT IN ('completed','closed','cancelled')) AND %s
		GROUP BY 1, 2 ORDER BY count(*) FILTER (WHERE x.completed_at >= $1 AND x.completed_at < $2) DESC LIMIT 100`, metrics, sc), cols, args...); err != nil {
		return nil, err
	}
	// seri harian penyelesaian per domain utama
	if r.Series, err = series(ctx, tx, fmt.Sprintf(workX+`SELECT d::date,
		count(x.id) FILTER (WHERE x.ot = 'work_order' OR x.sub_type IN ('general','inspection','routine_maintenance'))::float8,
		count(x.id) FILTER (WHERE x.sub_type = 'patrol')::float8,
		count(x.id) FILTER (WHERE x.sub_type = 'cleaning')::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d
		LEFT JOIN x ON x.completed_at >= d AND x.completed_at < d + interval '1 day' AND (%s)
		GROUP BY d ORDER BY d`, sc), []string{"engineering", "security", "housekeeping"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}
