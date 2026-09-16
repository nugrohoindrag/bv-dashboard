// Package reports: Advanced Reports (PRD P1 v1.3 §26; NC §56 Reporting Naming): Service Request volume, SLA compliance,
// response/resolution time, reopen rate, tenant satisfaction, Work Order performance, PM compliance, patrol/cleaning
// completion, facility utilization, visitor volume, billing/payment status, vendor performance, inventory movement.
// Semua laporan read-only, dibatasi scope property yang diizinkan (server-side) dan rentang tanggal [from, to].
package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/property"
)

type Service struct {
	DB *db.DB
}

func New(d *db.DB) *Service { return &Service{DB: d} }

// Params: rentang tanggal inklusif [From, To] (YYYY-MM-DD, zona waktu property — atau Asia/Jakarta bila lintas property),
// property opsional (kosong = seluruh property yang diizinkan).
type Params struct {
	PropertyID *uuid.UUID
	From, To   time.Time // diisi Run() dari FromDate/ToDate dalam zona waktu property
	FromDate   string
	ToDate     string
	TZ         *time.Location
}

// Point: satu baris seri (tanggal / kategori / status) dengan beberapa nilai.
type Point struct {
	Key    string             `json:"key"`
	Label  string             `json:"label,omitempty"`
	Values map[string]float64 `json:"values"`
}

// Report: hasil generik — summary (angka ringkas), series (deret waktu), breakdowns (per kategori/status/entitas).
type Report struct {
	Name       string             `json:"name"`
	PropertyID *uuid.UUID         `json:"property_id"`
	From       time.Time          `json:"from"`
	To         time.Time          `json:"to"`
	Summary    map[string]float64 `json:"summary"`
	Series     []Point            `json:"series"`
	Breakdowns map[string][]Point `json:"breakdowns"`
}

// Catalog: daftar laporan + permission (semua `reports.reports.view`).
var Catalog = []struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}{
	{"service-requests", "Service Request", "Volume, SLA compliance, waktu respons/penyelesaian, reopen rate, kepuasan tenant (CSAT)"},
	{"work-orders", "Work Order Performance", "Volume, penyelesaian tepat waktu, durasi, biaya, per tipe/prioritas"},
	{"maintenance", "PM Compliance", "Jadwal preventive maintenance: selesai tepat waktu vs overdue/skipped"},
	{"patrol-cleaning", "Patrol & Cleaning Completion", "Penyelesaian task patrol, cleaning, inspeksi"},
	{"facilities", "Facility Utilization", "Booking per fasilitas, jam terpakai, utilisasi, no-show"},
	{"visitors", "Visitor Volume", "Volume tamu per hari & status"},
	{"billing", "Billing & Payment", "Invoice diterbitkan/lunas/overdue, collection rate, pembayaran per provider"},
	{"vendors", "Vendor Performance", "Work Order per vendor: selesai, tepat waktu, durasi, reopen"},
	{"inventory", "Inventory Movement", "Pergerakan stok per jenis, pemakaian part, item stok rendah"},
}

func newReport(name string, p Params) *Report {
	return &Report{Name: name, PropertyID: p.PropertyID, From: p.From, To: p.To, Summary: map[string]float64{}, Series: []Point{}, Breakdowns: map[string][]Point{}}
}

// scope: klausa property + args (server-side authz: hanya property yang diizinkan).
func (s *Service) scope(ctx context.Context, p Params, col string) (string, []any, error) {
	pr := authctx.Must(ctx)
	// batas rentang: maksimal 366 hari
	if p.To.Before(p.From) || p.To.Sub(p.From) > 366*24*time.Hour {
		return "", nil, apperr.Validation("rentang tanggal tidak valid (maks 366 hari)")
	}
	args := []any{p.From, p.To.Add(24 * time.Hour)} // $1 from, $2 to (eksklusif)
	if p.PropertyID != nil {
		if err := iam.CanOnProperty(ctx, "reports.reports.view", *p.PropertyID); err != nil {
			return "", nil, err
		}
		args = append(args, *p.PropertyID)
		return col + " = $3", args, nil
	}
	if ids, all := pr.PropertyIDsFor("reports.reports.view"); !all {
		args = append(args, ids)
		return col + " = ANY($3)", args, nil
	}
	return "true", args, nil
}

func (s *Service) Run(ctx context.Context, name string, p Params) (*Report, error) {
	var out *Report
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// zona waktu property: batas hari, deret harian, dan jam kedatangan dihitung dalam zona ini (SET LOCAL per transaksi)
		loc := p.TZ
		if loc == nil {
			if p.PropertyID != nil {
				loc = property.PropertyTimezone(ctx, tx, *p.PropertyID)
			} else {
				loc, _ = time.LoadLocation("Asia/Jakarta")
			}
		}
		now := time.Now().In(loc)
		p.From = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -29)
		p.To = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		var err error
		if p.FromDate != "" {
			if p.From, err = time.ParseInLocation("2006-01-02", p.FromDate, loc); err != nil {
				return apperr.Validation("from harus YYYY-MM-DD")
			}
		}
		if p.ToDate != "" {
			if p.To, err = time.ParseInLocation("2006-01-02", p.ToDate, loc); err != nil {
				return apperr.Validation("to harus YYYY-MM-DD")
			}
		}
		if _, err := tx.Exec(ctx, "SET LOCAL TIME ZONE '"+loc.String()+"'"); err != nil {
			return err
		}
		switch name {
		case "service-requests":
			out, err = s.serviceRequests(ctx, tx, p)
		case "work-orders":
			out, err = s.workOrders(ctx, tx, p)
		case "maintenance":
			out, err = s.maintenance(ctx, tx, p)
		case "patrol-cleaning":
			out, err = s.patrolCleaning(ctx, tx, p)
		case "facilities":
			out, err = s.facilities(ctx, tx, p)
		case "visitors":
			out, err = s.visitors(ctx, tx, p)
		case "billing":
			out, err = s.billing(ctx, tx, p)
		case "vendors":
			out, err = s.vendors(ctx, tx, p)
		case "inventory":
			out, err = s.inventory(ctx, tx, p)
		default:
			return apperr.NotFound("Report")
		}
		return err
	})
	return out, err
}

// ---------- helpers ----------

// series: deret harian dari query `SELECT day::date, v1, v2, ... GROUP BY day` (kolom pertama tanggal).
func series(ctx context.Context, tx pgx.Tx, q string, cols []string, args ...any) ([]Point, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var day time.Time
		vals := make([]float64, len(cols))
		dest := make([]any, 0, len(cols)+1)
		dest = append(dest, &day)
		for i := range vals {
			dest = append(dest, &vals[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		pt := Point{Key: day.Format("2006-01-02"), Values: map[string]float64{}}
		for i, c := range cols {
			pt.Values[c] = vals[i]
		}
		out = append(out, pt)
	}
	return out, rows.Err()
}

// breakdown: `SELECT key, label, v1, v2, ... GROUP BY key`.
func breakdown(ctx context.Context, tx pgx.Tx, q string, cols []string, args ...any) ([]Point, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var key, label *string
		vals := make([]float64, len(cols))
		dest := make([]any, 0, len(cols)+2)
		dest = append(dest, &key, &label)
		for i := range vals {
			dest = append(dest, &vals[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		pt := Point{Key: deref(key), Label: deref(label), Values: map[string]float64{}}
		for i, c := range cols {
			pt.Values[c] = vals[i]
		}
		out = append(out, pt)
	}
	return out, rows.Err()
}

func summary(ctx context.Context, tx pgx.Tx, q string, cols []string, args ...any) (map[string]float64, error) {
	vals := make([]float64, len(cols))
	dest := make([]any, len(cols))
	for i := range vals {
		dest[i] = &vals[i]
	}
	if err := tx.QueryRow(ctx, q, args...).Scan(dest...); err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for i, c := range cols {
		out[c] = vals[i]
	}
	return out, nil
}

func pct(num, den float64) float64 {
	if den <= 0 {
		return 0
	}
	return float64(int(num*10000/den+0.5)) / 100
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------- Service Request ----------

func (s *Service) serviceRequests(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("service-requests", p)
	sc, args, err := s.scope(ctx, p, "sr.property_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*) FILTER (WHERE sr.created_at >= $1 AND sr.created_at < $2)::float8,
		count(*) FILTER (WHERE sr.resolved_at >= $1 AND sr.resolved_at < $2)::float8,
		count(*) FILTER (WHERE sr.closed_at >= $1 AND sr.closed_at < $2)::float8,
		count(*) FILTER (WHERE sr.status NOT IN ('closed','cancelled'))::float8,
		count(*) FILTER (WHERE sr.closed_at >= $1 AND sr.closed_at < $2 AND sr.reopen_count > 0)::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (sr.acknowledged_at - sr.created_at))/3600) FILTER (WHERE sr.acknowledged_at IS NOT NULL AND sr.created_at >= $1 AND sr.created_at < $2), 0)::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (sr.resolved_at - sr.created_at))/3600) FILTER (WHERE sr.resolved_at IS NOT NULL AND sr.resolved_at >= $1 AND sr.resolved_at < $2), 0)::float8,
		count(*) FILTER (WHERE sr.channel = 'tenant_app' AND sr.created_at >= $1 AND sr.created_at < $2)::float8
		FROM service_requests sr WHERE %s`, sc), []string{"created", "resolved", "closed", "open_now", "reopened", "avg_response_hours", "avg_resolution_hours", "tenant_app"}, args...)
	if err != nil {
		return nil, err
	}
	// SLA compliance: resolved dalam resolution_due_at (sla_tracking)
	sla, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*) FILTER (WHERE st.resolution_due_at IS NOT NULL)::float8,
		count(*) FILTER (WHERE st.resolution_due_at IS NOT NULL AND st.resolved_at IS NOT NULL AND st.resolved_at <= st.resolution_due_at)::float8,
		count(*) FILTER (WHERE st.response_due_at IS NOT NULL)::float8,
		count(*) FILTER (WHERE st.response_due_at IS NOT NULL AND st.responded_at IS NOT NULL AND st.responded_at <= st.response_due_at)::float8,
		count(*) FILTER (WHERE st.sla_breached_at IS NOT NULL)::float8
		FROM service_requests sr JOIN sla_tracking st ON st.object_type = 'service_request' AND st.object_id = sr.id
		WHERE sr.resolved_at >= $1 AND sr.resolved_at < $2 AND %s`, sc), []string{"sla_tracked", "sla_resolved_on_time", "sla_response_tracked", "sla_responded_on_time", "sla_breached"}, args...)
	if err != nil {
		return nil, err
	}
	for k, v := range sla {
		sum[k] = v
	}
	sum["reopen_rate_pct"] = pct(sum["reopened"], sum["closed"])
	sum["sla_resolution_pct"] = pct(sum["sla_resolved_on_time"], sum["sla_tracked"])
	sum["sla_response_pct"] = pct(sum["sla_responded_on_time"], sum["sla_response_tracked"])
	csat, err := summary(ctx, tx, fmt.Sprintf(`SELECT COALESCE(avg(fb.rating),0)::float8, count(*)::float8, count(*) FILTER (WHERE fb.rating >= 4)::float8
		FROM service_request_feedback fb JOIN service_requests sr ON sr.id = fb.service_request_id WHERE fb.created_at >= $1 AND fb.created_at < $2 AND %s`, sc), []string{"csat_avg", "csat_count", "csat_satisfied"}, args...)
	if err != nil {
		return nil, err
	}
	for k, v := range csat {
		sum[k] = v
	}
	sum["csat_satisfied_pct"] = pct(sum["csat_satisfied"], sum["csat_count"])
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date, count(sr.id) FILTER (WHERE sr.created_at >= d AND sr.created_at < d + interval '1 day')::float8, count(sr.id) FILTER (WHERE sr.resolved_at >= d AND sr.resolved_at < d + interval '1 day')::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d LEFT JOIN service_requests sr ON (%s) AND ((sr.created_at >= d AND sr.created_at < d + interval '1 day') OR (sr.resolved_at >= d AND sr.resolved_at < d + interval '1 day'))
		GROUP BY d ORDER BY d`, sc), []string{"created", "resolved"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["category"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT sr.category_code, COALESCE(c.name, sr.category_code), count(*)::float8, count(*) FILTER (WHERE sr.reopen_count > 0)::float8, COALESCE(avg(EXTRACT(EPOCH FROM (sr.resolved_at - sr.created_at))/3600) FILTER (WHERE sr.resolved_at IS NOT NULL),0)::float8
		FROM service_requests sr LEFT JOIN service_request_categories c ON c.id = sr.category_id WHERE sr.created_at >= $1 AND sr.created_at < $2 AND %s GROUP BY sr.category_code, c.name ORDER BY count(*) DESC LIMIT 20`, sc), []string{"count", "reopened", "avg_resolution_hours"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["status"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT sr.status, sr.status, count(*)::float8 FROM service_requests sr WHERE sr.created_at >= $1 AND sr.created_at < $2 AND %s GROUP BY sr.status ORDER BY count(*) DESC`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["priority"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT sr.priority, sr.priority, count(*)::float8 FROM service_requests sr WHERE sr.created_at >= $1 AND sr.created_at < $2 AND %s GROUP BY sr.priority`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["csat"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT fb.rating::text, fb.rating::text, count(*)::float8 FROM service_request_feedback fb JOIN service_requests sr ON sr.id = fb.service_request_id WHERE fb.created_at >= $1 AND fb.created_at < $2 AND %s GROUP BY fb.rating ORDER BY fb.rating`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["location"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT l.id::text, l.name, count(*)::float8 FROM service_requests sr JOIN locations l ON l.id = sr.location_id WHERE sr.created_at >= $1 AND sr.created_at < $2 AND %s GROUP BY l.id, l.name HAVING count(*) > 1 ORDER BY count(*) DESC LIMIT 10`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// ---------- Work Order ----------

func (s *Service) workOrders(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("work-orders", p)
	sc, args, err := s.scope(ctx, p, "w.property_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*) FILTER (WHERE w.created_at >= $1 AND w.created_at < $2)::float8,
		count(*) FILTER (WHERE w.completed_at >= $1 AND w.completed_at < $2)::float8,
		count(*) FILTER (WHERE w.completed_at >= $1 AND w.completed_at < $2 AND (w.due_at IS NULL OR w.completed_at <= w.due_at))::float8,
		count(*) FILTER (WHERE w.status NOT IN ('completed','closed','cancelled'))::float8,
		count(*) FILTER (WHERE w.status NOT IN ('completed','closed','cancelled') AND w.due_at < now())::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (w.completed_at - COALESCE(w.started_at, w.created_at)))/3600) FILTER (WHERE w.completed_at >= $1 AND w.completed_at < $2), 0)::float8,
		COALESCE(sum(w.actual_cost_amount) FILTER (WHERE w.completed_at >= $1 AND w.completed_at < $2), 0)::float8,
		count(*) FILTER (WHERE w.completed_at >= $1 AND w.completed_at < $2 AND w.reopen_count > 0)::float8,
		count(*) FILTER (WHERE w.created_at >= $1 AND w.created_at < $2 AND w.vendor_id IS NOT NULL)::float8
		FROM work_orders w WHERE %s`, sc), []string{"created", "completed", "completed_on_time", "open_now", "overdue_now", "avg_completion_hours", "actual_cost_total", "reopened", "vendor_assigned"}, args...)
	if err != nil {
		return nil, err
	}
	sum["on_time_pct"] = pct(sum["completed_on_time"], sum["completed"])
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date, count(w.id) FILTER (WHERE w.created_at >= d AND w.created_at < d + interval '1 day')::float8, count(w.id) FILTER (WHERE w.completed_at >= d AND w.completed_at < d + interval '1 day')::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d LEFT JOIN work_orders w ON (%s) AND ((w.created_at >= d AND w.created_at < d + interval '1 day') OR (w.completed_at >= d AND w.completed_at < d + interval '1 day'))
		GROUP BY d ORDER BY d`, sc), []string{"created", "completed"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["type"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT w.work_order_type, w.work_order_type, count(*)::float8, count(*) FILTER (WHERE w.completed_at IS NOT NULL)::float8, COALESCE(sum(w.actual_cost_amount),0)::float8 FROM work_orders w WHERE w.created_at >= $1 AND w.created_at < $2 AND %s GROUP BY w.work_order_type`, sc), []string{"count", "completed", "actual_cost_total"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["priority"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT w.priority, w.priority, count(*)::float8, count(*) FILTER (WHERE w.completed_at IS NOT NULL AND (w.due_at IS NULL OR w.completed_at <= w.due_at))::float8 FROM work_orders w WHERE w.created_at >= $1 AND w.created_at < $2 AND %s GROUP BY w.priority`, sc), []string{"count", "completed_on_time"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["status"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT w.status, w.status, count(*)::float8 FROM work_orders w WHERE w.created_at >= $1 AND w.created_at < $2 AND %s GROUP BY w.status`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["asset"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT a.id::text, a.asset_code || ' ' || a.name, count(*)::float8, COALESCE(sum(w.actual_cost_amount),0)::float8 FROM work_orders w JOIN assets a ON a.id = w.asset_id WHERE w.created_at >= $1 AND w.created_at < $2 AND %s GROUP BY a.id, a.asset_code, a.name ORDER BY count(*) DESC LIMIT 10`, sc), []string{"count", "actual_cost_total"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["team"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT t.id::text, t.name, count(*)::float8, count(*) FILTER (WHERE w.completed_at IS NOT NULL)::float8, COALESCE(avg(EXTRACT(EPOCH FROM (w.completed_at - COALESCE(w.started_at, w.created_at)))/3600) FILTER (WHERE w.completed_at IS NOT NULL),0)::float8 FROM work_orders w JOIN teams t ON t.id = w.assignee_team_id WHERE w.created_at >= $1 AND w.created_at < $2 AND %s GROUP BY t.id, t.name ORDER BY count(*) DESC`, sc), []string{"count", "completed", "avg_completion_hours"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// ---------- PM compliance ----------

func (s *Service) maintenance(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("maintenance", p)
	sc, args, err := s.scope(ctx, p, "ms.property_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*)::float8,
		count(*) FILTER (WHERE ms.status = 'completed')::float8,
		count(*) FILTER (WHERE ms.status = 'completed' AND ms.completed_at <= ms.due_at)::float8,
		count(*) FILTER (WHERE ms.status = 'overdue' OR (ms.status IN ('scheduled','due','in_progress') AND ms.due_at < now()))::float8,
		count(*) FILTER (WHERE ms.status = 'skipped')::float8,
		count(*) FILTER (WHERE ms.work_order_id IS NOT NULL)::float8
		FROM maintenance_schedules ms WHERE ms.due_at >= $1 AND ms.due_at < $2 AND %s`, sc), []string{"scheduled", "completed", "completed_on_time", "overdue", "skipped", "work_orders_created"}, args...)
	if err != nil {
		return nil, err
	}
	sum["compliance_pct"] = pct(sum["completed_on_time"], sum["scheduled"]-sum["skipped"])
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date, count(ms.id) FILTER (WHERE ms.due_at >= d AND ms.due_at < d + interval '1 day')::float8, count(ms.id) FILTER (WHERE ms.completed_at >= d AND ms.completed_at < d + interval '1 day')::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d LEFT JOIN maintenance_schedules ms ON (%s) AND ((ms.due_at >= d AND ms.due_at < d + interval '1 day') OR (ms.completed_at >= d AND ms.completed_at < d + interval '1 day'))
		GROUP BY d ORDER BY d`, sc), []string{"due", "completed"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["plan"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT mp.id::text, mp.name, count(*)::float8, count(*) FILTER (WHERE ms.status = 'completed')::float8, count(*) FILTER (WHERE ms.status = 'completed' AND ms.completed_at <= ms.due_at)::float8
		FROM maintenance_schedules ms JOIN maintenance_plans mp ON mp.id = ms.plan_id WHERE ms.due_at >= $1 AND ms.due_at < $2 AND %s GROUP BY mp.id, mp.name ORDER BY count(*) DESC LIMIT 20`, sc), []string{"scheduled", "completed", "completed_on_time"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["status"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT ms.status, ms.status, count(*)::float8 FROM maintenance_schedules ms WHERE ms.due_at >= $1 AND ms.due_at < $2 AND %s GROUP BY ms.status`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// ---------- Patrol & cleaning ----------

func (s *Service) patrolCleaning(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("patrol-cleaning", p)
	sc, args, err := s.scope(ctx, p, "t.property_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*) FILTER (WHERE t.task_type = 'patrol')::float8,
		count(*) FILTER (WHERE t.task_type = 'patrol' AND t.status IN ('completed','closed'))::float8,
		count(*) FILTER (WHERE t.task_type = 'cleaning')::float8,
		count(*) FILTER (WHERE t.task_type = 'cleaning' AND t.status IN ('completed','closed'))::float8,
		count(*) FILTER (WHERE t.task_type = 'inspection')::float8,
		count(*) FILTER (WHERE t.task_type = 'inspection' AND t.status IN ('completed','closed'))::float8,
		count(*) FILTER (WHERE t.status IN ('completed','closed') AND t.due_at IS NOT NULL AND t.completed_at > t.due_at)::float8,
		count(*) FILTER (WHERE t.status = 'cancelled')::float8
		FROM tasks t WHERE t.task_type IN ('patrol','cleaning','inspection') AND COALESCE(t.scheduled_start_at, t.created_at) >= $1 AND COALESCE(t.scheduled_start_at, t.created_at) < $2 AND %s`, sc),
		[]string{"patrol_total", "patrol_completed", "cleaning_total", "cleaning_completed", "inspection_total", "inspection_completed", "completed_late", "cancelled"}, args...)
	if err != nil {
		return nil, err
	}
	sum["patrol_completion_pct"] = pct(sum["patrol_completed"], sum["patrol_total"])
	sum["cleaning_completion_pct"] = pct(sum["cleaning_completed"], sum["cleaning_total"])
	sum["inspection_completion_pct"] = pct(sum["inspection_completed"], sum["inspection_total"])
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date,
		count(t.id) FILTER (WHERE t.task_type = 'patrol')::float8, count(t.id) FILTER (WHERE t.task_type = 'patrol' AND t.status IN ('completed','closed'))::float8,
		count(t.id) FILTER (WHERE t.task_type = 'cleaning')::float8, count(t.id) FILTER (WHERE t.task_type = 'cleaning' AND t.status IN ('completed','closed'))::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d LEFT JOIN tasks t ON (%s) AND t.task_type IN ('patrol','cleaning') AND COALESCE(t.scheduled_start_at, t.created_at) >= d AND COALESCE(t.scheduled_start_at, t.created_at) < d + interval '1 day'
		GROUP BY d ORDER BY d`, sc), []string{"patrol_total", "patrol_completed", "cleaning_total", "cleaning_completed"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["team"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT tm.id::text, tm.name, count(*)::float8, count(*) FILTER (WHERE t.status IN ('completed','closed'))::float8 FROM tasks t JOIN teams tm ON tm.id = t.assignee_team_id WHERE t.task_type IN ('patrol','cleaning','inspection') AND COALESCE(t.scheduled_start_at, t.created_at) >= $1 AND COALESCE(t.scheduled_start_at, t.created_at) < $2 AND %s GROUP BY tm.id, tm.name ORDER BY count(*) DESC`, sc), []string{"total", "completed"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["findings"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT f.severity, f.severity, count(*)::float8 FROM findings f WHERE f.source_type IN ('task','patrol_task','inspection','housekeeping_inspection','checklist_run_item') AND f.created_at >= $1 AND f.created_at < $2 AND %s GROUP BY f.severity`, replaceCol(sc, "t.property_id", "f.property_id")), []string{"count"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// ---------- Facilities ----------

func (s *Service) facilities(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("facilities", p)
	sc, args, err := s.scope(ctx, p, "b.property_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*)::float8,
		count(*) FILTER (WHERE b.status IN ('confirmed','checked_in','completed'))::float8,
		count(*) FILTER (WHERE b.status = 'cancelled')::float8,
		count(*) FILTER (WHERE b.status = 'rejected')::float8,
		count(*) FILTER (WHERE b.status = 'no_show')::float8,
		COALESCE(sum(EXTRACT(EPOCH FROM (b.ends_at - b.starts_at))/3600) FILTER (WHERE b.status IN ('confirmed','checked_in','completed')), 0)::float8,
		count(*) FILTER (WHERE b.channel = 'tenant_app')::float8
		FROM bookings b WHERE b.starts_at >= $1 AND b.starts_at < $2 AND %s`, sc), []string{"bookings", "confirmed", "cancelled", "rejected", "no_show", "hours_booked", "tenant_app"}, args...)
	if err != nil {
		return nil, err
	}
	sum["no_show_pct"] = pct(sum["no_show"], sum["confirmed"]+sum["no_show"])
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date, count(b.id)::float8, COALESCE(sum(EXTRACT(EPOCH FROM (b.ends_at - b.starts_at))/3600) FILTER (WHERE b.status IN ('confirmed','checked_in','completed')),0)::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d LEFT JOIN bookings b ON (%s) AND b.starts_at >= d AND b.starts_at < d + interval '1 day'
		GROUP BY d ORDER BY d`, sc), []string{"bookings", "hours_booked"}, args...); err != nil {
		return nil, err
	}
	// utilisasi per fasilitas = jam terpakai / (jam buka per hari × hari operasional dalam rentang)
	days := int(p.To.Sub(p.From).Hours()/24) + 1
	if r.Breakdowns["facility"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT f.id::text, f.name, count(b.id)::float8,
		COALESCE(sum(EXTRACT(EPOCH FROM (b.ends_at - b.starts_at))/3600) FILTER (WHERE b.status IN ('confirmed','checked_in','completed')),0)::float8,
		(EXTRACT(EPOCH FROM (f.close_time - f.open_time))/3600 * cardinality(f.weekdays) / 7.0 * %d)::float8,
		count(b.id) FILTER (WHERE b.status = 'no_show')::float8
		FROM facilities f LEFT JOIN bookings b ON b.facility_id = f.id AND b.starts_at >= $1 AND b.starts_at < $2 WHERE f.is_active AND %s GROUP BY f.id, f.name, f.open_time, f.close_time, f.weekdays ORDER BY count(b.id) DESC`, days, replaceCol(sc, "b.property_id", "f.property_id")), []string{"bookings", "hours_booked", "hours_available", "no_show"}, args...); err != nil {
		return nil, err
	}
	for i := range r.Breakdowns["facility"] {
		v := r.Breakdowns["facility"][i].Values
		v["utilization_pct"] = pct(v["hours_booked"], v["hours_available"])
	}
	if r.Breakdowns["status"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT b.status, b.status, count(*)::float8 FROM bookings b WHERE b.starts_at >= $1 AND b.starts_at < $2 AND %s GROUP BY b.status`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

func replaceCol(clause, from, to string) string {
	out := ""
	for i := 0; i < len(clause); {
		if len(clause)-i >= len(from) && clause[i:i+len(from)] == from {
			out += to
			i += len(from)
			continue
		}
		out += string(clause[i])
		i++
	}
	return out
}

// ---------- Visitors ----------

func (s *Service) visitors(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("visitors", p)
	sc, args, err := s.scope(ctx, p, "v.property_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*)::float8,
		count(*) FILTER (WHERE v.checked_in_at IS NOT NULL)::float8,
		count(*) FILTER (WHERE v.status = 'expired')::float8,
		count(*) FILTER (WHERE v.status = 'denied')::float8,
		count(*) FILTER (WHERE v.status = 'cancelled')::float8,
		COALESCE(sum(v.headcount) FILTER (WHERE v.checked_in_at IS NOT NULL), 0)::float8,
		count(*) FILTER (WHERE v.channel = 'tenant_app')::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (v.checked_out_at - v.checked_in_at))/60) FILTER (WHERE v.checked_out_at IS NOT NULL), 0)::float8
		FROM visitors v WHERE v.expected_at >= $1 AND v.expected_at < $2 AND %s`, sc), []string{"registered", "checked_in", "expired", "denied", "cancelled", "headcount_checked_in", "tenant_app", "avg_visit_minutes"}, args...)
	if err != nil {
		return nil, err
	}
	sum["show_rate_pct"] = pct(sum["checked_in"], sum["registered"]-sum["cancelled"]-sum["denied"])
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date, count(v.id)::float8, count(v.id) FILTER (WHERE v.checked_in_at IS NOT NULL)::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d LEFT JOIN visitors v ON (%s) AND v.expected_at >= d AND v.expected_at < d + interval '1 day'
		GROUP BY d ORDER BY d`, sc), []string{"registered", "checked_in"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["status"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT v.status, v.status, count(*)::float8 FROM visitors v WHERE v.expected_at >= $1 AND v.expected_at < $2 AND %s GROUP BY v.status`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["channel"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT v.channel, v.channel, count(*)::float8 FROM visitors v WHERE v.expected_at >= $1 AND v.expected_at < $2 AND %s GROUP BY v.channel`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["hour"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT lpad(EXTRACT(HOUR FROM v.checked_in_at)::int::text, 2, '0'), lpad(EXTRACT(HOUR FROM v.checked_in_at)::int::text, 2, '0') || ':00', count(*)::float8 FROM visitors v WHERE v.checked_in_at >= $1 AND v.checked_in_at < $2 AND %s GROUP BY 1 ORDER BY 1`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// ---------- Billing ----------

func (s *Service) billing(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("billing", p)
	sc, args, err := s.scope(ctx, p, "i.property_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*) FILTER (WHERE i.issued_at >= $1 AND i.issued_at < $2)::float8,
		COALESCE(sum(i.total_amount) FILTER (WHERE i.issued_at >= $1 AND i.issued_at < $2), 0)::float8,
		COALESCE(sum(i.paid_amount) FILTER (WHERE i.issued_at >= $1 AND i.issued_at < $2), 0)::float8,
		count(*) FILTER (WHERE i.status = 'paid' AND i.paid_at >= $1 AND i.paid_at < $2)::float8,
		count(*) FILTER (WHERE i.status = 'overdue')::float8,
		COALESCE(sum(i.total_amount - i.paid_amount) FILTER (WHERE i.status = 'overdue'), 0)::float8,
		COALESCE(sum(i.total_amount - i.paid_amount) FILTER (WHERE i.status IN ('issued','partially_paid','overdue')), 0)::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (i.paid_at - i.issued_at))/86400) FILTER (WHERE i.paid_at IS NOT NULL AND i.paid_at >= $1 AND i.paid_at < $2), 0)::float8
		FROM invoices i WHERE %s`, sc), []string{"issued", "issued_amount", "collected_amount", "paid", "overdue_now", "overdue_amount", "outstanding_amount", "avg_days_to_pay"}, args...)
	if err != nil {
		return nil, err
	}
	sum["collection_rate_pct"] = pct(sum["collected_amount"], sum["issued_amount"])
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date, COALESCE(sum(i.total_amount) FILTER (WHERE i.issued_at >= d AND i.issued_at < d + interval '1 day'),0)::float8, COALESCE(sum(pm.amount) FILTER (WHERE pm.paid_at >= d AND pm.paid_at < d + interval '1 day'),0)::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d
		LEFT JOIN invoices i ON (%s) AND i.issued_at >= d AND i.issued_at < d + interval '1 day'
		LEFT JOIN payments pm ON pm.status = 'paid' AND pm.paid_at >= d AND pm.paid_at < d + interval '1 day' AND %s
		GROUP BY d ORDER BY d`, sc, replaceCol(sc, "i.property_id", "pm.property_id")), []string{"issued_amount", "collected_amount"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["status"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT i.status, i.status, count(*)::float8, COALESCE(sum(i.total_amount),0)::float8, COALESCE(sum(i.total_amount - i.paid_amount),0)::float8 FROM invoices i WHERE i.issued_at >= $1 AND i.issued_at < $2 AND %s GROUP BY i.status`, sc), []string{"count", "amount", "outstanding"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["type"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT i.invoice_type, i.invoice_type, count(*)::float8, COALESCE(sum(i.total_amount),0)::float8, COALESCE(sum(i.paid_amount),0)::float8 FROM invoices i WHERE i.issued_at >= $1 AND i.issued_at < $2 AND %s GROUP BY i.invoice_type`, sc), []string{"count", "amount", "collected"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["provider"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT pm.provider_code || '/' || pm.method, pm.provider_code || ' · ' || pm.method, count(*)::float8, count(*) FILTER (WHERE pm.status = 'paid')::float8, COALESCE(sum(pm.amount) FILTER (WHERE pm.status = 'paid'),0)::float8, count(*) FILTER (WHERE pm.status IN ('failed','expired'))::float8
		FROM payments pm WHERE pm.created_at >= $1 AND pm.created_at < $2 AND %s GROUP BY pm.provider_code, pm.method ORDER BY count(*) DESC`, replaceCol(sc, "i.property_id", "pm.property_id")), []string{"count", "paid", "paid_amount", "failed"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["tenant_overdue"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT t.id::text, t.name, count(*)::float8, COALESCE(sum(i.total_amount - i.paid_amount),0)::float8 FROM invoices i JOIN tenants t ON t.id = i.tenant_id WHERE i.status = 'overdue' AND $1::timestamptz <= $2::timestamptz AND %s GROUP BY t.id, t.name ORDER BY sum(i.total_amount - i.paid_amount) DESC LIMIT 10`, sc), []string{"count", "outstanding"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// ---------- Vendors ----------

func (s *Service) vendors(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("vendors", p)
	sc, args, err := s.scope(ctx, p, "w.property_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*)::float8,
		count(*) FILTER (WHERE w.completed_at IS NOT NULL)::float8,
		count(*) FILTER (WHERE w.completed_at IS NOT NULL AND (w.due_at IS NULL OR w.completed_at <= w.due_at))::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (w.completed_at - COALESCE(w.vendor_assigned_at, w.created_at)))/3600) FILTER (WHERE w.completed_at IS NOT NULL), 0)::float8,
		count(*) FILTER (WHERE w.reopen_count > 0)::float8,
		COALESCE(sum(w.actual_cost_amount), 0)::float8,
		count(DISTINCT w.vendor_id)::float8
		FROM work_orders w WHERE w.vendor_id IS NOT NULL AND COALESCE(w.vendor_assigned_at, w.created_at) >= $1 AND COALESCE(w.vendor_assigned_at, w.created_at) < $2 AND %s`, sc), []string{"vendor_work_orders", "completed", "completed_on_time", "avg_completion_hours", "reopened", "actual_cost_total", "vendors_active"}, args...)
	if err != nil {
		return nil, err
	}
	sum["on_time_pct"] = pct(sum["completed_on_time"], sum["completed"])
	r.Summary = sum
	if r.Breakdowns["vendor"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT v.id::text, v.name, count(*)::float8, count(*) FILTER (WHERE w.completed_at IS NOT NULL)::float8, count(*) FILTER (WHERE w.completed_at IS NOT NULL AND (w.due_at IS NULL OR w.completed_at <= w.due_at))::float8,
		COALESCE(avg(EXTRACT(EPOCH FROM (w.completed_at - COALESCE(w.vendor_assigned_at, w.created_at)))/3600) FILTER (WHERE w.completed_at IS NOT NULL),0)::float8, count(*) FILTER (WHERE w.reopen_count > 0)::float8, COALESCE(sum(w.actual_cost_amount),0)::float8
		FROM work_orders w JOIN vendors v ON v.id = w.vendor_id WHERE COALESCE(w.vendor_assigned_at, w.created_at) >= $1 AND COALESCE(w.vendor_assigned_at, w.created_at) < $2 AND %s GROUP BY v.id, v.name ORDER BY count(*) DESC`, sc), []string{"work_orders", "completed", "completed_on_time", "avg_completion_hours", "reopened", "actual_cost_total"}, args...); err != nil {
		return nil, err
	}
	for i := range r.Breakdowns["vendor"] {
		v := r.Breakdowns["vendor"][i].Values
		v["on_time_pct"] = pct(v["completed_on_time"], v["completed"])
	}
	if r.Breakdowns["category"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT c, c, count(*)::float8 FROM work_orders w JOIN vendors v ON v.id = w.vendor_id, unnest(v.service_categories) c WHERE COALESCE(w.vendor_assigned_at, w.created_at) >= $1 AND COALESCE(w.vendor_assigned_at, w.created_at) < $2 AND %s GROUP BY c ORDER BY count(*) DESC`, sc), []string{"count"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// ---------- Inventory ----------

func (s *Service) inventory(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("inventory", p)
	sc, args, err := s.scope(ctx, p, "st.property_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT
		count(*)::float8,
		COALESCE(sum(st.quantity) FILTER (WHERE st.quantity > 0), 0)::float8,
		COALESCE(-sum(st.quantity) FILTER (WHERE st.quantity < 0), 0)::float8,
		count(*) FILTER (WHERE st.transaction_type = 'usage')::float8,
		COALESCE(-sum(st.quantity * COALESCE(st.unit_cost,0)) FILTER (WHERE st.transaction_type = 'usage'), 0)::float8,
		count(*) FILTER (WHERE st.transaction_type = 'adjustment')::float8
		FROM stock_transactions st WHERE st.performed_at >= $1 AND st.performed_at < $2 AND %s`, sc), []string{"transactions", "qty_in", "qty_out", "usage_transactions", "usage_cost", "adjustments"}, args...)
	if err != nil {
		return nil, err
	}
	low, err := summary(ctx, tx, `SELECT count(*)::float8 FROM inventory_items i WHERE i.is_active AND i.min_stock > 0 AND COALESCE((SELECT sum(quantity) FROM stock_levels sl WHERE sl.item_id = i.id), 0) <= i.min_stock`, []string{"low_stock_items"})
	if err != nil {
		return nil, err
	}
	sum["low_stock_items"] = low["low_stock_items"]
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date, COALESCE(sum(st.quantity) FILTER (WHERE st.quantity > 0),0)::float8, COALESCE(-sum(st.quantity) FILTER (WHERE st.quantity < 0),0)::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d LEFT JOIN stock_transactions st ON (%s) AND st.performed_at >= d AND st.performed_at < d + interval '1 day'
		GROUP BY d ORDER BY d`, sc), []string{"qty_in", "qty_out"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["type"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT st.transaction_type, st.transaction_type, count(*)::float8, COALESCE(sum(abs(st.quantity)),0)::float8 FROM stock_transactions st WHERE st.performed_at >= $1 AND st.performed_at < $2 AND %s GROUP BY st.transaction_type`, sc), []string{"count", "quantity"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["item_usage"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT i.id::text, i.item_code || ' ' || i.name, COALESCE(-sum(st.quantity),0)::float8, COALESCE(-sum(st.quantity * COALESCE(st.unit_cost,0)),0)::float8, count(*)::float8
		FROM stock_transactions st JOIN inventory_items i ON i.id = st.item_id WHERE st.transaction_type = 'usage' AND st.performed_at >= $1 AND st.performed_at < $2 AND %s GROUP BY i.id, i.item_code, i.name ORDER BY -sum(st.quantity) DESC LIMIT 15`, sc), []string{"quantity", "cost", "transactions"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["low_stock"], err = breakdown(ctx, tx, `SELECT i.id::text, i.item_code || ' ' || i.name, COALESCE((SELECT sum(quantity) FROM stock_levels sl WHERE sl.item_id = i.id), 0)::float8, i.min_stock::float8
		FROM inventory_items i WHERE i.is_active AND i.min_stock > 0 AND COALESCE((SELECT sum(quantity) FROM stock_levels sl WHERE sl.item_id = i.id), 0) <= i.min_stock ORDER BY 3 LIMIT 20`, []string{"quantity", "min_stock"}); err != nil {
		return nil, err
	}
	return r, nil
}
