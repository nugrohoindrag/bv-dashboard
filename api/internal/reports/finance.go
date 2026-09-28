package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/finance"
	"github.com/buildingvision/api/internal/platform/authctx"
)

// ---------- Laporan keuangan PRD P4 v2.1 P4-FIN-03 (Aging, Collection, Revenue, IPL, Sinking Fund, Budget vs Actual,
// Operating Cost). Zona waktu sesi = zona property (SET LOCAL di Run), sehingga ::date = tanggal lokal. ----------

const openInv = `i.status IN ('issued','partially_paid','overdue') AND i.total_amount - i.paid_amount - i.credited_amount > 0`
const daysPast = `(now()::date - COALESCE(i.due_date, i.due_at::date))`

// aging: titik-waktu (sekarang); rentang tanggal tidak dipakai.
func (s *Service) aging(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("aging", p)
	sc, args, err := s.scope(ctx, p, "i.property_id")
	if err != nil {
		return nil, err
	}
	o := "(i.total_amount - i.paid_amount - i.credited_amount)"
	buckets := `COALESCE(sum(` + o + `) FILTER (WHERE ` + daysPast + ` <= 0),0)::float8, COALESCE(sum(` + o + `) FILTER (WHERE ` + daysPast + ` BETWEEN 1 AND 30),0)::float8,
		COALESCE(sum(` + o + `) FILTER (WHERE ` + daysPast + ` BETWEEN 31 AND 60),0)::float8, COALESCE(sum(` + o + `) FILTER (WHERE ` + daysPast + ` BETWEEN 61 AND 90),0)::float8,
		COALESCE(sum(` + o + `) FILTER (WHERE ` + daysPast + ` > 90),0)::float8, COALESCE(sum(` + o + `),0)::float8, count(*)::float8`
	cols := []string{"current", "d1_30", "d31_60", "d61_90", "d90_plus", "total", "invoices"}
	if r.Summary, err = summary(ctx, tx, fmt.Sprintf(`SELECT `+buckets+` FROM invoices i WHERE `+openInv+` AND $1::timestamptz <= $2::timestamptz AND %s`, sc), cols, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["tenant"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT COALESCE(i.tenant_id, i.unit_location_id)::text, min(COALESCE(t.name, 'Unit ' || un.unit_number || ' (tanpa tenant)')), `+buckets+`
		FROM invoices i LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN units un ON un.location_id = i.unit_location_id
		WHERE `+openInv+` AND $1::timestamptz <= $2::timestamptz AND %s GROUP BY COALESCE(i.tenant_id, i.unit_location_id) ORDER BY sum(`+o+`) DESC LIMIT 100`, sc), cols, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["property"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT i.property_id::text, min(pl.name), `+buckets+` FROM invoices i JOIN locations pl ON pl.id = i.property_id
		WHERE `+openInv+` AND $1::timestamptz <= $2::timestamptz AND %s GROUP BY i.property_id ORDER BY 8 DESC`, sc), cols, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["type"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT i.invoice_type, i.invoice_type, `+buckets+` FROM invoices i WHERE `+openInv+` AND $1::timestamptz <= $2::timestamptz AND %s GROUP BY i.invoice_type ORDER BY 8 DESC`, sc), cols, args...); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) collection(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("collection", p)
	sc, args, err := s.scope(ctx, p, "i.property_id")
	if err != nil {
		return nil, err
	}
	pmSc := replaceCol(sc, "i.property_id", "pm.property_id")
	clSc := replaceCol(sc, "i.property_id", "c.property_id")
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT COALESCE(sum(i.total_amount - i.credited_amount),0)::float8, COALESCE(sum(LEAST(i.paid_amount, i.total_amount - i.credited_amount)),0)::float8,
		count(*)::float8, count(*) FILTER (WHERE i.status = 'paid')::float8, count(*) FILTER (WHERE i.status = 'paid' AND i.paid_at <= i.due_at)::float8
		FROM invoices i WHERE i.status NOT IN ('draft','cancelled') AND i.due_at >= $1 AND i.due_at < $2 AND %s`, sc), []string{"due_amount", "collected_on_due", "due_invoices", "paid_invoices", "paid_on_time"}, args...)
	if err != nil {
		return nil, err
	}
	sum["collection_rate_pct"] = pct(sum["collected_on_due"], sum["due_amount"])
	sum["on_time_rate_pct"] = pct(sum["paid_on_time"], sum["due_invoices"])
	var cash, reminders, logs, open, kept, broken float64
	_ = tx.QueryRow(ctx, fmt.Sprintf(`SELECT COALESCE(sum(pm.amount),0)::float8 FROM payments pm WHERE pm.status IN ('paid','refunded') AND pm.method <> 'credit' AND pm.paid_at >= $1 AND pm.paid_at < $2 AND %s`, pmSc), args...).Scan(&cash)
	_ = tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*)::float8 FROM invoice_reminders ir JOIN invoices i ON i.id = ir.invoice_id WHERE ir.sent_at >= $1 AND ir.sent_at < $2 AND %s`, sc), args...).Scan(&reminders)
	_ = tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*)::float8, count(*) FILTER (WHERE c.promise_status = 'open')::float8, count(*) FILTER (WHERE c.promise_status = 'kept')::float8, count(*) FILTER (WHERE c.promise_status = 'broken')::float8
		FROM collection_logs c WHERE c.created_at >= $1 AND c.created_at < $2 AND %s`, clSc), args...).Scan(&logs, &open, &kept, &broken)
	sum["cash_collected"], sum["reminders_sent"], sum["collection_logs"] = cash, reminders, logs
	sum["promises_open"], sum["promises_kept"], sum["promises_broken"] = open, kept, broken
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date,
		COALESCE((SELECT sum(i.total_amount - i.credited_amount) FROM invoices i WHERE i.status NOT IN ('draft','cancelled') AND i.due_at >= d AND i.due_at < d + interval '1 day' AND %s),0)::float8,
		COALESCE((SELECT sum(pm.amount) FROM payments pm WHERE pm.status IN ('paid','refunded') AND pm.method <> 'credit' AND pm.paid_at >= d AND pm.paid_at < d + interval '1 day' AND %s),0)::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d ORDER BY d`, sc, pmSc), []string{"due_amount", "cash_collected"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["method"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT pm.method, pm.method, count(*)::float8, COALESCE(sum(pm.amount),0)::float8 FROM payments pm WHERE pm.status IN ('paid','refunded') AND pm.paid_at >= $1 AND pm.paid_at < $2 AND %s GROUP BY pm.method ORDER BY 4 DESC`, pmSc), []string{"count", "amount"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["tenant"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT COALESCE(i.tenant_id, i.unit_location_id)::text, min(COALESCE(t.name, 'Unit ' || un.unit_number)), COALESCE(sum(i.total_amount - i.credited_amount),0)::float8,
		COALESCE(sum(LEAST(i.paid_amount, i.total_amount - i.credited_amount)),0)::float8, CASE WHEN sum(i.total_amount - i.credited_amount) > 0 THEN round(sum(LEAST(i.paid_amount, i.total_amount - i.credited_amount))::numeric * 100 / sum(i.total_amount - i.credited_amount), 1) ELSE 0 END::float8
		FROM invoices i LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN units un ON un.location_id = i.unit_location_id
		WHERE i.status NOT IN ('draft','cancelled') AND i.due_at >= $1 AND i.due_at < $2 AND %s GROUP BY COALESCE(i.tenant_id, i.unit_location_id) ORDER BY 5, 3 DESC LIMIT 100`, sc), []string{"due_amount", "collected", "rate_pct"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["channel"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT c.channel, c.channel, count(*)::float8, count(*) FILTER (WHERE c.outcome = 'promise_to_pay')::float8 FROM collection_logs c WHERE c.created_at >= $1 AND c.created_at < $2 AND %s GROUP BY c.channel`, clSc), []string{"count", "promises"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) revenue(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("revenue", p)
	sc, args, err := s.scope(ctx, p, "i.property_id")
	if err != nil {
		return nil, err
	}
	cnSc := replaceCol(sc, "i.property_id", "c.property_id")
	base := `FROM invoice_items it JOIN invoices i ON i.id = it.invoice_id WHERE i.status <> 'cancelled' AND i.invoice_number IS NOT NULL AND i.issued_at >= $1 AND i.issued_at < $2 AND %s`
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT COALESCE(sum(it.amount) FILTER (WHERE COALESCE(it.charge_type, i.invoice_type) <> 'deposit'),0)::float8, COALESCE(sum(it.tax_amount),0)::float8,
		COALESCE(sum(it.amount) FILTER (WHERE COALESCE(it.charge_type, i.invoice_type) = 'deposit'),0)::float8, count(DISTINCT i.id)::float8 `+base, sc), []string{"billed_revenue", "tax", "deposits_billed", "invoices"}, args...)
	if err != nil {
		return nil, err
	}
	var cn float64
	_ = tx.QueryRow(ctx, fmt.Sprintf(`SELECT COALESCE(sum(c.amount),0)::float8 FROM credit_notes c WHERE c.status = 'approved' AND c.decided_at >= $1 AND c.decided_at < $2 AND %s`, cnSc), args...).Scan(&cn)
	sum["credit_notes"] = cn
	sum["net_revenue"] = sum["billed_revenue"] - cn
	r.Summary = sum
	if r.Breakdowns["charge_type"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT COALESCE(it.charge_type, i.invoice_type), COALESCE(it.charge_type, i.invoice_type), COALESCE(sum(it.amount),0)::float8, COALESCE(sum(it.tax_amount),0)::float8, count(*)::float8 `+base+` GROUP BY 1 ORDER BY 3 DESC`, sc), []string{"amount", "tax", "items"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["property"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT i.property_id::text, min(pl.name), COALESCE(sum(it.amount) FILTER (WHERE COALESCE(it.charge_type, i.invoice_type) <> 'deposit'),0)::float8 FROM invoice_items it JOIN invoices i ON i.id = it.invoice_id JOIN locations pl ON pl.id = i.property_id
		WHERE i.status <> 'cancelled' AND i.invoice_number IS NOT NULL AND i.issued_at >= $1 AND i.issued_at < $2 AND %s GROUP BY i.property_id ORDER BY 3 DESC`, sc), []string{"amount"}, args...); err != nil {
		return nil, err
	}
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date, COALESCE((SELECT sum(it.amount) FROM invoice_items it JOIN invoices i ON i.id = it.invoice_id WHERE i.status <> 'cancelled' AND i.invoice_number IS NOT NULL
		AND COALESCE(it.charge_type, i.invoice_type) <> 'deposit' AND i.issued_at >= d AND i.issued_at < d + interval '1 day' AND %s),0)::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d ORDER BY d`, sc), []string{"billed_revenue"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// ipl: IPL/service charge (profile Apartment) — billing, collection, outstanding per unit (P4-SCF-05).
func (s *Service) ipl(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("ipl", p)
	sc, args, err := s.scope(ctx, p, "i.property_id")
	if err != nil {
		return nil, err
	}
	iplItems := `EXISTS (SELECT 1 FROM invoice_items it WHERE it.invoice_id = i.id AND COALESCE(it.charge_type, i.invoice_type) IN ('ipl','service_charge'))`
	iplAmt := `COALESCE((SELECT sum(it.amount + it.tax_amount) FROM invoice_items it WHERE it.invoice_id = i.id AND COALESCE(it.charge_type, i.invoice_type) IN ('ipl','service_charge')),0)`
	share := `CASE WHEN i.total_amount > 0 THEN ` + iplAmt + `::numeric / i.total_amount ELSE 0 END`
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT count(DISTINCT i.unit_location_id)::float8, COALESCE(sum(`+iplAmt+`) FILTER (WHERE i.issued_at >= $1 AND i.issued_at < $2),0)::float8,
		COALESCE(sum(round(LEAST(i.paid_amount + i.credited_amount, i.total_amount) * `+share+`)) FILTER (WHERE i.issued_at >= $1 AND i.issued_at < $2),0)::float8,
		COALESCE(sum(round((i.total_amount - i.paid_amount - i.credited_amount) * `+share+`)) FILTER (WHERE i.status IN ('issued','partially_paid','overdue')),0)::float8,
		count(DISTINCT i.unit_location_id) FILTER (WHERE i.status = 'overdue')::float8
		FROM invoices i WHERE i.status NOT IN ('draft','cancelled') AND i.invoice_number IS NOT NULL AND `+iplItems+` AND %s`, sc),
		[]string{"units_billed", "ipl_billed", "ipl_collected", "ipl_outstanding", "units_overdue"}, args...)
	if err != nil {
		return nil, err
	}
	sum["collection_rate_pct"] = pct(sum["ipl_collected"], sum["ipl_billed"])
	r.Summary = sum
	if r.Breakdowns["unit"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT i.unit_location_id::text, min(COALESCE('Unit ' || un.unit_number, l.name)), COALESCE(sum(`+iplAmt+`) FILTER (WHERE i.issued_at >= $1 AND i.issued_at < $2),0)::float8,
		COALESCE(sum(round((i.total_amount - i.paid_amount - i.credited_amount) * `+share+`)) FILTER (WHERE i.status IN ('issued','partially_paid','overdue')),0)::float8,
		COALESCE(max(`+daysPast+`) FILTER (WHERE i.status = 'overdue'),0)::float8
		FROM invoices i JOIN locations l ON l.id = i.unit_location_id LEFT JOIN units un ON un.location_id = i.unit_location_id
		WHERE i.status NOT IN ('draft','cancelled') AND i.invoice_number IS NOT NULL AND `+iplItems+` AND %s GROUP BY i.unit_location_id ORDER BY 4 DESC, 2 LIMIT 500`, sc), []string{"billed", "outstanding", "oldest_days_overdue"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) sinkingFund(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("sinking-fund", p)
	sc, args, err := s.scope(ctx, p, "sf.property_id")
	if err != nil {
		return nil, err
	}
	sum, err := summary(ctx, tx, fmt.Sprintf(`SELECT COALESCE(sum(sf.amount) FILTER (WHERE sf.entry_date < $1::date),0)::float8,
		COALESCE(sum(sf.amount) FILTER (WHERE sf.entry_type IN ('receipt','reversal') AND sf.entry_date >= $1::date AND sf.entry_date < $2::date),0)::float8,
		COALESCE(-sum(sf.amount) FILTER (WHERE sf.entry_type = 'usage' AND sf.entry_date >= $1::date AND sf.entry_date < $2::date),0)::float8,
		COALESCE(sum(sf.amount) FILTER (WHERE sf.entry_type IN ('adjustment','opening') AND sf.entry_date >= $1::date AND sf.entry_date < $2::date),0)::float8,
		COALESCE(sum(sf.amount) FILTER (WHERE sf.entry_date < $2::date),0)::float8
		FROM sinking_fund_entries sf WHERE %s`, sc), []string{"opening", "receipts", "usage", "adjustments", "closing"}, args...)
	if err != nil {
		return nil, err
	}
	r.Summary = sum
	if r.Series, err = series(ctx, tx, fmt.Sprintf(`SELECT d::date, COALESCE((SELECT sum(sf.amount) FROM sinking_fund_entries sf WHERE sf.entry_date <= d::date AND %s),0)::float8
		FROM generate_series($1::timestamptz, $2::timestamptz - interval '1 day', interval '1 day') d ORDER BY d`, sc), []string{"balance"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["property"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT sf.property_id::text, min(pl.name), COALESCE(sum(sf.amount),0)::float8, COALESCE(sum(sf.amount) FILTER (WHERE sf.entry_type IN ('receipt','reversal') AND sf.entry_date >= $1::date AND sf.entry_date < $2::date),0)::float8,
		COALESCE(-sum(sf.amount) FILTER (WHERE sf.entry_type = 'usage' AND sf.entry_date >= $1::date AND sf.entry_date < $2::date),0)::float8
		FROM sinking_fund_entries sf JOIN locations pl ON pl.id = sf.property_id WHERE %s GROUP BY sf.property_id ORDER BY 2`, sc), []string{"balance", "receipts", "usage"}, args...); err != nil {
		return nil, err
	}
	if r.Breakdowns["usage"], err = breakdown(ctx, tx, fmt.Sprintf(`SELECT sf.id::text, to_char(sf.entry_date,'YYYY-MM-DD') || ' · ' || COALESCE(sf.description,'') || COALESCE(' (' || w.work_order_number || ')',''), (-sf.amount)::float8
		FROM sinking_fund_entries sf LEFT JOIN work_orders w ON w.id = sf.work_order_id WHERE sf.entry_type = 'usage' AND sf.entry_date >= $1::date AND sf.entry_date < $2::date AND %s ORDER BY sf.entry_date DESC LIMIT 100`, sc), []string{"amount"}, args...); err != nil {
		return nil, err
	}
	return r, nil
}

// reportProps: daftar property untuk perhitungan finance (satu property atau seluruh scope reports).
func reportProps(ctx context.Context, p Params) ([]uuid.UUID, bool) {
	if p.PropertyID != nil {
		return []uuid.UUID{*p.PropertyID}, false
	}
	return authctx.Must(ctx).PropertyIDsFor("reports.reports.view")
}

// monthsIn: bulan (1..12) tahun `to` yang beririsan dengan rentang laporan.
func monthsIn(p Params) (int, int, int) {
	year := p.To.Year()
	from := 1
	if p.From.Year() == year {
		from = int(p.From.Month())
	}
	return year, from, int(p.To.Month())
}

func (s *Service) budgetActual(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("budget-actual", p)
	if _, _, err := s.scope(ctx, p, "x.property_id"); err != nil {
		return nil, err
	}
	pids, all := reportProps(ctx, p)
	year, m1, m2 := monthsIn(p)
	act, err := finance.ActualsTx(ctx, tx, pids, all, year)
	if err != nil {
		return nil, err
	}
	args := []any{year}
	w := ""
	if !all {
		args = append(args, pids)
		w = " AND b.property_id = ANY($2)"
	}
	rows, err := tx.Query(ctx, `SELECT bl.kind, bl.category, bl.month, sum(bl.amount)::bigint FROM budget_lines bl JOIN budgets b ON b.id = bl.budget_id
		WHERE b.fiscal_year = $1 AND b.id IN (SELECT DISTINCT ON (x.property_id) x.id FROM budgets x WHERE x.fiscal_year = $1 AND x.status <> 'superseded' ORDER BY x.property_id, (x.status = 'approved') DESC, x.revision DESC)`+w+` GROUP BY 1, 2, 3`, args...)
	if err != nil {
		return nil, err
	}
	budget := map[string]int64{}
	for rows.Next() {
		var kind, cat string
		var m int
		var v int64
		if rows.Scan(&kind, &cat, &m, &v) == nil && m >= m1 && m <= m2 {
			budget[kind+"|"+cat] += v
		}
	}
	rows.Close()
	build := func(kind string, cats []finance.Category, actual map[string]*[12]int64) []Point {
		out := []Point{}
		for _, c := range cats {
			var a int64
			if x := actual[c.Key]; x != nil {
				for m := m1; m <= m2; m++ {
					a += x[m-1]
				}
			}
			b := budget[kind+"|"+c.Key]
			if a == 0 && b == 0 {
				continue
			}
			v := map[string]float64{"budget": float64(b), "actual": float64(a), "variance": float64(a - b), "variance_pct": 0}
			if b > 0 {
				v["variance_pct"] = pct(float64(a-b), float64(b))
			}
			out = append(out, Point{Key: c.Key, Label: c.Label, Values: v})
			r.Summary[kind+"_budget"] += float64(b)
			r.Summary[kind+"_actual"] += float64(a)
		}
		return out
	}
	r.Summary["revenue_budget"], r.Summary["revenue_actual"], r.Summary["cost_budget"], r.Summary["cost_actual"] = 0, 0, 0, 0
	r.Breakdowns["revenue"] = build("revenue", finance.RevenueCategories, act.Revenue)
	r.Breakdowns["cost"] = build("cost", finance.CostCategories, act.Cost)
	r.Summary["net_budget"] = r.Summary["revenue_budget"] - r.Summary["cost_budget"]
	r.Summary["net_actual"] = r.Summary["revenue_actual"] - r.Summary["cost_actual"]
	r.Summary["fiscal_year"] = float64(year)
	return r, nil
}

func (s *Service) operatingCost(ctx context.Context, tx pgx.Tx, p Params) (*Report, error) {
	r := newReport("operating-cost", p)
	if _, _, err := s.scope(ctx, p, "x.property_id"); err != nil {
		return nil, err
	}
	pids, all := reportProps(ctx, p)
	year, m1, m2 := monthsIn(p)
	act, err := finance.ActualsTx(ctx, tx, pids, all, year)
	if err != nil {
		return nil, err
	}
	var total float64
	r.Breakdowns["category"] = []Point{}
	for _, c := range finance.CostCategories {
		x := act.Cost[c.Key]
		if x == nil {
			continue
		}
		var v int64
		for m := m1; m <= m2; m++ {
			v += x[m-1]
		}
		if v == 0 {
			continue
		}
		total += float64(v)
		r.Breakdowns["category"] = append(r.Breakdowns["category"], Point{Key: c.Key, Label: c.Label, Values: map[string]float64{"amount": float64(v)}})
	}
	for i := range r.Breakdowns["category"] {
		r.Breakdowns["category"][i].Values["share_pct"] = pct(r.Breakdowns["category"][i].Values["amount"], total)
	}
	r.Summary["total_cost"] = total
	r.Summary["fiscal_year"] = float64(year)
	for m := m1; m <= m2; m++ {
		var v int64
		for _, x := range act.Cost {
			v += x[m-1]
		}
		r.Series = append(r.Series, Point{Key: time.Date(year, time.Month(m), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), Values: map[string]float64{"cost": float64(v)}})
	}
	return r, nil
}
