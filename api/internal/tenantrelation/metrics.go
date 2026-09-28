package tenantrelation

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Tenant / Guest / Occupier Service Health (PRD P3 v2.1 §7.1, Roadmap v2.1 §25.2, §25.8 Phase 3) ----------

type CountRow struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
	Link  string `json:"drill_down"`
}

type DayPoint struct {
	Date       string `json:"date"`
	Created    int    `json:"created"`
	Resolved   int    `json:"resolved"`
	Complaints int    `json:"complaints"`
}

type Metrics struct {
	OpenTickets      int      `json:"open_tickets"`
	SLARisk          int      `json:"sla_risk"`
	Overdue          int      `json:"overdue"` // P3-TSH-02 / B-07: permintaan terbuka yang melewati batas SLA penyelesaian (= sla_status breached)
	ResolvedToday    int      `json:"resolved_today"`
	Reopened30d      int      `json:"reopened_30d"`
	WaitingForTenant int      `json:"waiting_for_tenant"`
	WaitingForStaff  int      `json:"waiting_for_staff"` // pesan tenant belum dibaca staf
	PendingAccounts  int      `json:"pending_accounts"`
	CSAT             *float64 `json:"csat"`
	CSATCount        int      `json:"csat_count"`
	ReopenRatePct    *float64 `json:"reopen_rate_pct"`
	TenantAppTickets int      `json:"tenant_app_tickets_30d"`
	// PRD P3 v2.1 P3-TSH-04..09
	AvgResponseHours    *float64          `json:"avg_response_hours"`
	AvgResolutionHours  *float64          `json:"avg_resolution_hours"`
	SLACompliancePct    *float64          `json:"sla_compliance_pct"`
	Requests30d         int               `json:"requests_30d"`
	RequestsPrev30d     int               `json:"requests_prev_30d"`
	Complaints30d       int               `json:"complaints_30d"`
	ComplaintsPrev30d   int               `json:"complaints_prev_30d"`
	RecurringIssuesOpen int               `json:"recurring_issues_open"`
	FeedbackNew         int               `json:"general_feedback_new"`
	ByCategory          []CountRow        `json:"by_category"`
	ByType              []CountRow        `json:"by_type"`
	ByChannel           []CountRow        `json:"by_channel"`
	Series              []DayPoint        `json:"series"`
	DrillDown           map[string]string `json:"drill_down"`
	Timezone            string            `json:"timezone"`
}

var requestTypeLabel = map[string]string{"service_request": "Permintaan layanan", "complaint": "Keluhan", "maintenance_request": "Perbaikan", "cleaning_request": "Kebersihan", "facility_issue": "Masalah fasilitas", "other": "Lainnya"}
var channelLabel = map[string]string{"tenant_app": "Tenant App", "staff": "Staf", "whatsapp": "WhatsApp", "phone": "Telepon", "email": "Email", "walk_in": "Datang langsung", "public_intake": "Formulir publik (QR)"}

// Metrics: definisi kanonis — Resolved Today dalam zona waktu property (B-05), Reopened 30 hari dari waktu reopen (B-06),
// Overdue = lewat SLA (B-07), kepatuhan SLA memperhitungkan waktu jeda (B-09), setiap KPI membawa tautan drill-down (P3-TSH-09).
func (s *Service) Metrics(ctx context.Context, propertyID *uuid.UUID) (*Metrics, error) {
	p := authctx.Must(ctx)
	m := Metrics{DrillDown: map[string]string{}, ByCategory: []CountRow{}, ByType: []CountRow{}, ByChannel: []CountRow{}, Series: []DayPoint{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pids []uuid.UUID
		all := true
		if propertyID != nil {
			if err := iam.CanOnProperty(ctx, "tenant.service_requests.view", *propertyID); err != nil {
				return err
			}
			pids, all = []uuid.UUID{*propertyID}, false
		} else {
			pids, all = p.PropertyIDsFor("tenant.service_requests.view")
		}
		scope := func(col string) string {
			if all {
				return "true"
			}
			return col + " = ANY($1)"
		}
		args := []any{}
		if !all {
			args = append(args, pids)
		}
		loc, _ := time.LoadLocation("Asia/Jakarta")
		if propertyID != nil {
			loc = property.PropertyTimezone(ctx, tx, *propertyID)
		}
		m.Timezone = loc.String()
		q := `SELECT
			count(*) FILTER (WHERE sr.status NOT IN ('closed','cancelled')),
			count(*) FILTER (WHERE sr.sla_risk_at IS NOT NULL AND sr.sla_breached_at IS NULL AND sr.status NOT IN ('resolved','closed','cancelled')),
			count(*) FILTER (WHERE sr.sla_breached_at IS NOT NULL AND sr.status NOT IN ('resolved','closed','cancelled')),
			count(*) FILTER (WHERE sr.resolved_at >= (date_trunc('day', now() AT TIME ZONE pr.timezone) AT TIME ZONE pr.timezone)),
			count(*) FILTER (WHERE sr.last_reopened_at >= now() - interval '30 days'),
			count(*) FILTER (WHERE sr.status = 'waiting_for_tenant'),
			count(*) FILTER (WHERE sr.channel = 'tenant_app' AND sr.created_at >= now() - interval '30 days'),
			count(*) FILTER (WHERE sr.closed_at >= now() - interval '30 days'),
			count(*) FILTER (WHERE sr.closed_at >= now() - interval '30 days' AND sr.reopen_count > 0),
			count(*) FILTER (WHERE sr.created_at >= now() - interval '30 days'),
			count(*) FILTER (WHERE sr.created_at >= now() - interval '60 days' AND sr.created_at < now() - interval '30 days'),
			count(*) FILTER (WHERE sr.request_type = 'complaint' AND sr.created_at >= now() - interval '30 days'),
			count(*) FILTER (WHERE sr.request_type = 'complaint' AND sr.created_at >= now() - interval '60 days' AND sr.created_at < now() - interval '30 days'),
			avg(EXTRACT(EPOCH FROM (sr.acknowledged_at - sr.created_at))/3600) FILTER (WHERE sr.acknowledged_at IS NOT NULL AND sr.created_at >= now() - interval '30 days'),
			avg(EXTRACT(EPOCH FROM (sr.resolved_at - sr.created_at))/3600) FILTER (WHERE sr.resolved_at IS NOT NULL AND sr.resolved_at >= now() - interval '30 days')
			FROM service_requests sr JOIN properties pr ON pr.location_id = sr.property_id WHERE ` + scope("sr.property_id")
		var closed30, reopened30 int
		if err := tx.QueryRow(ctx, q, args...).Scan(&m.OpenTickets, &m.SLARisk, &m.Overdue, &m.ResolvedToday, &m.Reopened30d, &m.WaitingForTenant, &m.TenantAppTickets, &closed30, &reopened30,
			&m.Requests30d, &m.RequestsPrev30d, &m.Complaints30d, &m.ComplaintsPrev30d, &m.AvgResponseHours, &m.AvgResolutionHours); err != nil {
			return err
		}
		if closed30 > 0 {
			r := float64(reopened30) * 100 / float64(closed30)
			m.ReopenRatePct = &r
		}
		// kepatuhan SLA 30 hari: selesai ≤ batas + waktu jeda (menunggu tenant) — sama dengan laporan sla/operations-kpi (B-09)
		var tracked, onTime int
		_ = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE st.resolution_due_at IS NOT NULL),
			count(*) FILTER (WHERE st.resolution_due_at IS NOT NULL AND st.resolved_at <= st.resolution_due_at + (st.paused_minutes || ' minutes')::interval)
			FROM service_requests sr JOIN sla_tracking st ON st.object_type = 'service_request' AND st.object_id = sr.id
			WHERE sr.resolved_at >= now() - interval '30 days' AND `+scope("sr.property_id"), args...).Scan(&tracked, &onTime)
		if tracked > 0 {
			v := float64(onTime) * 100 / float64(tracked)
			m.SLACompliancePct = &v
		}
		_ = tx.QueryRow(ctx, `SELECT count(DISTINCT m.service_request_id) FROM service_request_messages m JOIN service_requests sr ON sr.id = m.service_request_id WHERE m.author_kind = 'tenant' AND m.read_by_staff_at IS NULL AND `+scope("sr.property_id"), args...).Scan(&m.WaitingForStaff)
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM tenant_users tu WHERE tu.status = 'pending_validation' AND `+scope("tu.property_id"), args...).Scan(&m.PendingAccounts)
		var avg *float64
		_ = tx.QueryRow(ctx, `SELECT avg(rating)::float8, count(*) FROM service_request_feedback fb WHERE fb.created_at >= now() - interval '90 days' AND `+scope("fb.property_id"), args...).Scan(&avg, &m.CSATCount)
		m.CSAT = avg
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM recurring_issues ri WHERE ri.status IN ('open','acknowledged') AND `+scope("ri.property_id"), args...).Scan(&m.RecurringIssuesOpen)
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM tenant_feedback f WHERE f.status IN ('new','in_review') AND `+scope("f.property_id"), args...).Scan(&m.FeedbackNew)

		// breakdown 30 hari: kategori, tipe, kanal (P3-TSH-06)
		from30 := time.Now().Add(-30 * 24 * time.Hour).In(loc)
		q30 := url.QueryEscape(from30.Format(time.RFC3339))
		propQ := ""
		if propertyID != nil {
			propQ = "&property_id=" + propertyID.String()
		}
		rows, err := tx.Query(ctx, `SELECT sr.category_code, COALESCE(max(c.name), sr.category_code), count(*) FROM service_requests sr LEFT JOIN service_request_categories c ON c.id = sr.category_id
			WHERE sr.created_at >= now() - interval '30 days' AND `+scope("sr.property_id")+` GROUP BY sr.category_code ORDER BY count(*) DESC LIMIT 12`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r CountRow
			if err := rows.Scan(&r.Key, &r.Label, &r.Count); err != nil {
				rows.Close()
				return err
			}
			r.Link = "/operations/service-requests?category=" + url.QueryEscape(r.Key) + "&created_from=" + q30 + propQ
			m.ByCategory = append(m.ByCategory, r)
		}
		rows.Close()
		for _, dim := range []struct {
			col    string
			param  string
			labels map[string]string
			dst    *[]CountRow
		}{{"request_type", "request_type", requestTypeLabel, &m.ByType}, {"channel", "channel", channelLabel, &m.ByChannel}} {
			rows, err := tx.Query(ctx, `SELECT sr.`+dim.col+`, count(*) FROM service_requests sr WHERE sr.created_at >= now() - interval '30 days' AND `+scope("sr.property_id")+` GROUP BY 1 ORDER BY 2 DESC`, args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var r CountRow
				if err := rows.Scan(&r.Key, &r.Count); err != nil {
					rows.Close()
					return err
				}
				r.Label = dim.labels[r.Key]
				if r.Label == "" {
					r.Label = r.Key
				}
				r.Link = "/operations/service-requests?" + dim.param + "=" + url.QueryEscape(r.Key) + "&created_from=" + q30 + propQ
				*dim.dst = append(*dim.dst, r)
			}
			rows.Close()
		}
		// tren harian 30 hari (zona waktu property / Asia/Jakarta lintas property)
		srows, err := tx.Query(ctx, `SELECT d::date,
			(SELECT count(*) FROM service_requests sr WHERE sr.created_at >= (d AT TIME ZONE $`+fmt.Sprint(len(args)+1)+`) AND sr.created_at < ((d + interval '1 day') AT TIME ZONE $`+fmt.Sprint(len(args)+1)+`) AND `+scope("sr.property_id")+`),
			(SELECT count(*) FROM service_requests sr WHERE sr.resolved_at >= (d AT TIME ZONE $`+fmt.Sprint(len(args)+1)+`) AND sr.resolved_at < ((d + interval '1 day') AT TIME ZONE $`+fmt.Sprint(len(args)+1)+`) AND `+scope("sr.property_id")+`),
			(SELECT count(*) FROM service_requests sr WHERE sr.request_type = 'complaint' AND sr.created_at >= (d AT TIME ZONE $`+fmt.Sprint(len(args)+1)+`) AND sr.created_at < ((d + interval '1 day') AT TIME ZONE $`+fmt.Sprint(len(args)+1)+`) AND `+scope("sr.property_id")+`)
			FROM generate_series((now() AT TIME ZONE $`+fmt.Sprint(len(args)+1)+`)::date - 29, (now() AT TIME ZONE $`+fmt.Sprint(len(args)+1)+`)::date, interval '1 day') d ORDER BY 1`, append(args, loc.String())...)
		if err != nil {
			return err
		}
		for srows.Next() {
			var d time.Time
			var pt DayPoint
			if err := srows.Scan(&d, &pt.Created, &pt.Resolved, &pt.Complaints); err != nil {
				srows.Close()
				return err
			}
			pt.Date = d.Format("2006-01-02")
			m.Series = append(m.Series, pt)
		}
		srows.Close()

		// tautan drill-down (P3-TSH-09) — filter daftar Service Request Web
		now := time.Now().In(loc)
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		base := "/operations/service-requests?"
		pq := ""
		if propertyID != nil {
			pq = "&property_id=" + propertyID.String()
		}
		m.DrillDown["open_tickets"] = base + "open=true" + pq
		m.DrillDown["sla_risk"] = base + "sla_status=at_risk" + pq
		m.DrillDown["overdue"] = base + "sla_status=breached" + pq
		m.DrillDown["resolved_today"] = base + "resolved_from=" + url.QueryEscape(today.Format(time.RFC3339)) + pq
		m.DrillDown["reopened_30d"] = base + "reopened_from=" + q30 + pq
		m.DrillDown["waiting_for_tenant"] = base + "status=waiting_for_tenant" + pq
		m.DrillDown["complaints_30d"] = base + "request_type=complaint&created_from=" + q30 + pq
		m.DrillDown["requests_30d"] = base + "created_from=" + q30 + pq
		m.DrillDown["tenant_app_tickets_30d"] = base + "channel=tenant_app&created_from=" + q30 + pq
		m.DrillDown["csat"] = "/tenant-relation/feedback"
		m.DrillDown["pending_accounts"] = "/tenant-relation/tenant-users?status=pending_validation"
		m.DrillDown["recurring_issues_open"] = "/tenant-relation/recurring-issues"
		m.DrillDown["general_feedback_new"] = "/tenant-relation/feedback?tab=general"
		m.DrillDown["avg_response_hours"] = "/reports/service-requests"
		m.DrillDown["avg_resolution_hours"] = "/reports/service-requests"
		m.DrillDown["sla_compliance_pct"] = "/reports/sla"
		return nil
	})
	return &m, err
}
