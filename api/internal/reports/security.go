package reports

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Security Report (PRD P2 v2.1 P2-SDB-03; Roadmap v2.1 §9, §25.2): patrol & checkpoint compliance, checkpoint terlewat,
// incident (respons), visitor, emergency per periode. Checkpoint compliance = checkpoint discan ÷ seluruh checkpoint pada
// patrol yang selesai; respons incident dari SLA response (sla_tracking.responded_at); emergency ack = raised → acknowledged.
func (s *Service) security(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("security", p)
	sc, args, err := s.scope(ctx, p, "t.property_id", "t.location_id")
	if err != nil {
		return nil, err
	}
	const inPeriod = `COALESCE(t.scheduled_start_at, t.created_at) >= $1 AND COALESCE(t.scheduled_start_at, t.created_at) < $2`
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(DISTINCT t.id) FILTER (WHERE t.status <> 'cancelled')::float8,
		count(DISTINCT t.id) FILTER (WHERE t.status IN ('completed','closed'))::float8,
		count(cs.id) FILTER (WHERE t.status IN ('completed','closed'))::float8,
		count(cs.id) FILTER (WHERE cs.status = 'scanned' AND t.status IN ('completed','closed'))::float8,
		count(cs.id) FILTER (WHERE cs.status = 'missed')::float8
		FROM tasks t LEFT JOIN checkpoint_scans cs ON cs.patrol_task_id = t.id WHERE t.task_type = 'patrol' AND `+inPeriod+` AND %s`, sc),
		[]string{"patrol_total", "patrol_completed", "checkpoints_total", "checkpoints_scanned", "checkpoints_missed"}, args...)
	if err != nil {
		return nil, err
	}
	sum["patrol_completion_pct"] = pct(sum["patrol_completed"], sum["patrol_total"])
	sum["checkpoint_compliance_pct"] = pct(sum["checkpoints_scanned"], sum["checkpoints_total"])

	isc, iargs, err := s.scope(ctx, p, "i.property_id", "i.location_id")
	if err != nil {
		return nil, err
	}
	inc, err := summary(ctx, tx, fmt.Sprintf(`SELECT count(*)::float8, count(*) FILTER (WHERE i.severity = 'critical')::float8, count(*) FILTER (WHERE i.resolved_at IS NOT NULL)::float8,
		count(*) FILTER (WHERE i.category = 'unauthorized_access')::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (st.responded_at - i.reported_at))/60) FILTER (WHERE st.responded_at IS NOT NULL), 0)::float8
		FROM incidents i LEFT JOIN sla_tracking st ON st.object_type = 'incident' AND st.object_id = i.id WHERE i.reported_at >= $1 AND i.reported_at < $2 AND %s`, isc),
		[]string{"incidents_reported", "incidents_critical", "incidents_resolved", "unauthorized_entry", "avg_response_minutes"}, iargs...)
	if err != nil {
		return nil, err
	}
	esc, eargs, err := s.scope(ctx, p, "ea.property_id", "ea.location_id")
	if err != nil {
		return nil, err
	}
	emg, err := summary(ctx, tx, fmt.Sprintf(`SELECT count(*)::float8, count(*) FILTER (WHERE ea.status = 'resolved')::float8, count(*) FILTER (WHERE ea.status = 'cancelled')::float8,
		count(*) FILTER (WHERE ea.escalation_level > 0)::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (ea.acknowledged_at - ea.raised_at))/60) FILTER (WHERE ea.acknowledged_at IS NOT NULL), 0)::float8
		FROM emergency_alerts ea WHERE ea.raised_at >= $1 AND ea.raised_at < $2 AND %s`, esc),
		[]string{"emergencies_raised", "emergencies_resolved", "emergencies_false_alarm", "emergencies_escalated", "avg_emergency_ack_minutes"}, eargs...)
	if err != nil {
		return nil, err
	}
	vsc, vargs, err := s.scope(ctx, p, "v.property_id", "v.host_unit_location_id")
	if err != nil {
		return nil, err
	}
	vis, err := summary(ctx, tx, fmt.Sprintf(`SELECT count(*)::float8 FROM visitors v WHERE v.checked_in_at >= $1 AND v.checked_in_at < $2 AND %s`, vsc), []string{"visitors_checked_in"}, vargs...)
	if err != nil {
		return nil, err
	}
	psc, pargs, err := s.scope(ctx, p, "pv.property_id", "pv.location_id")
	if err != nil {
		return nil, err
	}
	pv, err := summary(ctx, tx, fmt.Sprintf(`SELECT count(*)::float8 FROM parking_violations pv WHERE pv.recorded_at >= $1 AND pv.recorded_at < $2 AND %s`, psc), []string{"parking_violations"}, pargs...)
	if err != nil {
		return nil, err
	}
	for _, m := range []map[string]float64{inc, emg, vis, pv} {
		for k, v := range m {
			sum[k] = v
		}
	}
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date,
		count(DISTINCT t.id) FILTER (WHERE t.status IN ('completed','closed'))::float8,
		count(cs.id) FILTER (WHERE cs.status = 'scanned')::float8,
		count(cs.id) FILTER (WHERE cs.status = 'missed')::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d
		LEFT JOIN tasks t ON (%s) AND t.task_type = 'patrol' AND COALESCE(t.scheduled_start_at, t.created_at) >= d AND COALESCE(t.scheduled_start_at, t.created_at) < d + interval '1 day'
		LEFT JOIN checkpoint_scans cs ON cs.patrol_task_id = t.id
		GROUP BY d ORDER BY d`, sc), []string{"patrol_completed", "checkpoints_scanned", "checkpoints_missed"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["route"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT pr.id::text, pr.name, count(DISTINCT t.id) FILTER (WHERE t.status <> 'cancelled')::float8, count(DISTINCT t.id) FILTER (WHERE t.status IN ('completed','closed'))::float8,
		count(cs.id) FILTER (WHERE cs.status = 'scanned' AND t.status IN ('completed','closed'))::float8, count(cs.id) FILTER (WHERE t.status IN ('completed','closed'))::float8,
		count(cs.id) FILTER (WHERE cs.status = 'missed')::float8
		FROM tasks t JOIN patrol_tasks pt ON pt.task_id = t.id JOIN patrol_routes pr ON pr.id = pt.route_id LEFT JOIN checkpoint_scans cs ON cs.patrol_task_id = t.id
		WHERE t.task_type = 'patrol' AND `+inPeriod+` AND %s GROUP BY pr.id, pr.name ORDER BY pr.name`, sc),
		[]string{"patrols", "completed", "checkpoints_scanned", "checkpoints_total", "checkpoints_missed"}, args...); err != nil {
		return nil, err
	}
	for i := range r.Breakdowns["route"] {
		v := r.Breakdowns["route"][i].Values
		v["checkpoint_compliance_pct"] = pct(v["checkpoints_scanned"], v["checkpoints_total"])
	}
	if r.Breakdowns["missed_checkpoint"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT c.id::text, c.name, count(*)::float8
		FROM checkpoint_scans cs JOIN checkpoints c ON c.id = cs.checkpoint_id JOIN tasks t ON t.id = cs.patrol_task_id
		WHERE cs.status = 'missed' AND `+inPeriod+` AND %s GROUP BY c.id, c.name ORDER BY 3 DESC LIMIT 10`, sc), []string{"missed"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["incident_category"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT i.category, i.category, count(*)::float8, count(*) FILTER (WHERE i.severity = 'critical')::float8
		FROM incidents i WHERE i.reported_at >= $1 AND i.reported_at < $2 AND %s GROUP BY 1 ORDER BY 3 DESC`, isc), []string{"count", "critical"}, iargs...); err != nil {
		return nil, err
	}
	if r.Breakdowns["emergency_type"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT ea.emergency_type, ea.emergency_type, count(*)::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (ea.acknowledged_at - ea.raised_at))/60) FILTER (WHERE ea.acknowledged_at IS NOT NULL), 0)::float8
		FROM emergency_alerts ea WHERE ea.raised_at >= $1 AND ea.raised_at < $2 AND %s GROUP BY 1 ORDER BY 3 DESC`, esc), []string{"count", "avg_ack_minutes"}, eargs...); err != nil {
		return nil, err
	}
	return r, nil
}
