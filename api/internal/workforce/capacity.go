package workforce

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
)

// ---------- Workforce capacity vs workload (PRD P2 v2.1 P2-WKL-03; Roadmap v2.1 §25.8 "Workforce Capacity") ----------
// Kapasitas = staf on-duty × sisa jam shift (tanpa roster: 4 jam). Beban = pekerjaan terbuka hari ini (jatuh tempo hari ini,
// overdue, atau sedang dikerjakan) × estimasi durasi (jadwal → selisih mulai–tenggat, dibatasi 15–240 menit; default task 60,
// WO 120). Overview lintas tim menjumlahkan hasil per domain (D-P2-05).

type DomainCapacity struct {
	Domain          string  `json:"domain"`
	Scheduled       int     `json:"scheduled"`
	OnDuty          int     `json:"on_duty"`
	Absent          int     `json:"absent"`
	MinStaff        int     `json:"min_staff"`
	Shortage        int     `json:"shortage"`
	CapacityMinutes int     `json:"capacity_minutes"`
	WorkloadMinutes int     `json:"workload_minutes"`
	OpenItems       int     `json:"open_items"`
	LoadRatio       float64 `json:"load_ratio"` // beban / kapasitas
	Status          string  `json:"status"`     // ok | tight | over | no_staff | idle
	Link            string  `json:"link"`
}

type CapacitySummary struct {
	PropertyID  *uuid.UUID       `json:"property_id"`
	GeneratedAt time.Time        `json:"generated_at"`
	Domains     []DomainCapacity `json:"domains"`
	Total       DomainCapacity   `json:"total"`
}

// taskDomainSQL: domain pekerjaan (task/WO) — patrol → security; cleaning & inspeksi HK → housekeeping; lainnya → team assignee
// atau engineering.
const workloadSQL = `
WITH w AS (
  SELECT CASE WHEN t.task_type = 'patrol' THEN 'security'
              WHEN t.task_type = 'cleaning' OR EXISTS (SELECT 1 FROM housekeeping_inspections hi WHERE hi.task_id = t.id) THEN 'housekeeping'
              ELSE COALESCE((SELECT tm.domain FROM teams tm WHERE tm.id = t.assignee_team_id AND tm.domain IN ('security','housekeeping','engineering')), 'engineering') END AS domain,
         LEAST(240, GREATEST(15, COALESCE(EXTRACT(EPOCH FROM (t.due_at - t.scheduled_start_at))/60, 60)))::int AS minutes
  FROM tasks t JOIN properties pr ON pr.location_id = t.property_id
  WHERE t.status NOT IN ('completed','closed','cancelled') AND ($1::uuid IS NULL OR t.property_id = $1) AND %s
    AND (t.status IN ('in_progress','on_hold') OR t.due_at < now() OR (t.due_at AT TIME ZONE pr.timezone)::date = (now() AT TIME ZONE pr.timezone)::date
         OR (t.scheduled_start_at AT TIME ZONE pr.timezone)::date = (now() AT TIME ZONE pr.timezone)::date)
  UNION ALL
  SELECT 'engineering', LEAST(480, GREATEST(30, COALESCE(EXTRACT(EPOCH FROM (w.due_at - w.scheduled_start_at))/60, 120)))::int
  FROM work_orders w JOIN properties pr ON pr.location_id = w.property_id
  WHERE w.status NOT IN ('completed','closed','cancelled','draft') AND ($1::uuid IS NULL OR w.property_id = $1) AND %s
    AND (w.status IN ('in_progress','on_hold') OR w.due_at < now() OR (w.due_at AT TIME ZONE pr.timezone)::date = (now() AT TIME ZONE pr.timezone)::date)
)
SELECT domain, count(*), COALESCE(sum(minutes),0) FROM w GROUP BY domain`

func (s *Service) Capacity(ctx context.Context, propertyID *uuid.UUID) (*CapacitySummary, error) {
	p := authctx.Must(ctx)
	if propertyID != nil && !p.HasAnyOnProperty("overview.dashboard.view", *propertyID) {
		return nil, apperr.Forbidden("")
	}
	out := &CapacitySummary{PropertyID: propertyID, GeneratedAt: time.Now().UTC()}
	byDomain := map[string]*DomainCapacity{}
	for _, d := range []string{"engineering", "security", "housekeeping"} {
		dc := &DomainCapacity{Domain: d, Link: map[string]string{"engineering": "/engineering", "security": "/security/shifts", "housekeeping": "/housekeeping/shifts"}[d]}
		byDomain[d] = dc
	}
	// papan on-duty per domain shift (security, housekeeping)
	for _, d := range []string{"security", "housekeeping"} {
		b, err := s.OnDuty(ctx, d, propertyID)
		if err != nil {
			return nil, err
		}
		dc := byDomain[d]
		dc.Scheduled, dc.OnDuty, dc.Absent, dc.Shortage = b.Scheduled, b.OnDuty, b.Absent, b.Shortage
		for _, sh := range b.Shifts {
			dc.MinStaff += sh.MinStaff
		}
	}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{propertyID}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		scopeT := p.ScopeSQL("overview.dashboard.view", "t.property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = t.location_id)", add)
		scopeW := p.ScopeSQL("overview.dashboard.view", "w.property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = w.location_id)", add)
		rows, err := tx.Query(ctx, fmt.Sprintf(workloadSQL, scopeT, scopeW), args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var d string
			var n, minutes int
			if err := rows.Scan(&d, &n, &minutes); err != nil {
				rows.Close()
				return err
			}
			if dc, ok := byDomain[d]; ok {
				dc.OpenItems, dc.WorkloadMinutes = n, minutes
			}
		}
		rows.Close()
		// kapasitas: sisa menit shift staf on-duty (roster) atau 240 menit tanpa roster
		args2 := []any{propertyID}
		add2 := func(v any) string { args2 = append(args2, v); return fmt.Sprintf("$%d", len(args2)) }
		scopeA := p.ScopeSQL("overview.dashboard.view", "a.property_id", "", add2)
		crow, err := tx.Query(ctx, `SELECT a.domain, count(*), COALESCE(sum(CASE WHEN sa.ends_at IS NOT NULL THEN GREATEST(0, EXTRACT(EPOCH FROM (sa.ends_at - now()))/60) ELSE 240 END),0)::int
			FROM attendance_records a LEFT JOIN shift_assignments sa ON sa.id = a.shift_assignment_id
			WHERE a.clock_out_at IS NULL AND ($1::uuid IS NULL OR a.property_id = $1) AND `+scopeA+` GROUP BY a.domain`, args2...)
		if err != nil {
			return err
		}
		for crow.Next() {
			var d string
			var n, minutes int
			if err := crow.Scan(&d, &n, &minutes); err != nil {
				crow.Close()
				return err
			}
			if dc, ok := byDomain[d]; ok {
				dc.CapacityMinutes = minutes
				if d == "engineering" {
					dc.OnDuty = n
				}
			}
		}
		crow.Close()
		return nil
	})
	if err != nil {
		return nil, err
	}
	total := DomainCapacity{Domain: "total", Link: "/overview"}
	for _, d := range []string{"engineering", "security", "housekeeping"} {
		dc := byDomain[d]
		switch {
		case dc.CapacityMinutes == 0 && dc.WorkloadMinutes == 0:
			dc.Status = "idle"
		case dc.CapacityMinutes == 0:
			dc.Status = "no_staff"
		default:
			dc.LoadRatio = math.Round(float64(dc.WorkloadMinutes)/float64(dc.CapacityMinutes)*100) / 100
			switch {
			case dc.LoadRatio > 1:
				dc.Status = "over"
			case dc.LoadRatio >= 0.8:
				dc.Status = "tight"
			default:
				dc.Status = "ok"
			}
		}
		if dc.Shortage > 0 && dc.Status == "ok" {
			dc.Status = "tight"
		}
		out.Domains = append(out.Domains, *dc)
		total.Scheduled += dc.Scheduled
		total.OnDuty += dc.OnDuty
		total.Absent += dc.Absent
		total.MinStaff += dc.MinStaff
		total.Shortage += dc.Shortage
		total.CapacityMinutes += dc.CapacityMinutes
		total.WorkloadMinutes += dc.WorkloadMinutes
		total.OpenItems += dc.OpenItems
	}
	if total.CapacityMinutes > 0 {
		total.LoadRatio = math.Round(float64(total.WorkloadMinutes)/float64(total.CapacityMinutes)*100) / 100
	}
	out.Total = total
	return out, nil
}
