package overview

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/finance"
	"github.com/buildingvision/api/internal/platform/authctx"
)

// ---------- Finance Dashboard (PRD P4 v2.1 P4-FIN-01; Roadmap v2.1 §25.2 Financial Health, §25.8 Phase 4) ----------
// Collection Rate (kanonis P4-COL-05), Outstanding, Overdue, Aging > 90, Revenue, Operating Cost, Budget vs Actual (YTD),
// Sinking Fund balance (Apartment), kas diterima, pembayaran menunggu verifikasi — setiap KPI drill-down ke daftar/laporan.

func (s *Service) FinanceDashboard(ctx context.Context, in DashboardParams) (*DomainDashboard, error) {
	var out *DomainDashboard
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		d, o, err := s.newDashboard(ctx, tx, "finance", "billing.invoices.view", in)
		if err != nil {
			return err
		}
		out = o
		a := d.sq.args
		iW := d.w("i.property_id", "i.unit_location_id")
		pmW := d.w("pm.property_id", "(SELECT x.unit_location_id FROM invoices x WHERE x.id = pm.invoice_id)")
		days := "((now() AT TIME ZONE pr.timezone)::date - COALESCE(i.due_date, (i.due_at AT TIME ZONE pr.timezone)::date))"
		var dueAmt, collectedOnDue, outstanding, overdue, overdueN, aged90, revenue, cash float64
		if err := tx.QueryRow(ctx, periodCTE+`SELECT COALESCE(sum(i.total_amount - i.credited_amount) FILTER (WHERE i.due_at >= `+d.from+` AND i.due_at < `+d.to+`),0)::float8,
			COALESCE(sum(LEAST(i.paid_amount, i.total_amount - i.credited_amount)) FILTER (WHERE i.due_at >= `+d.from+` AND i.due_at < `+d.to+`),0)::float8,
			COALESCE(sum(i.total_amount - i.paid_amount - i.credited_amount) FILTER (WHERE i.status IN ('issued','partially_paid','overdue')),0)::float8,
			COALESCE(sum(i.total_amount - i.paid_amount - i.credited_amount) FILTER (WHERE i.status = 'overdue'),0)::float8,
			count(*) FILTER (WHERE i.status = 'overdue')::float8,
			COALESCE(sum(i.total_amount - i.paid_amount - i.credited_amount) FILTER (WHERE i.status = 'overdue' AND `+days+` > 90),0)::float8
			FROM invoices i JOIN properties pr ON pr.location_id = i.property_id WHERE i.status NOT IN ('draft','cancelled') AND i.invoice_number IS NOT NULL`+iW, a...).
			Scan(&dueAmt, &collectedOnDue, &outstanding, &overdue, &overdueN, &aged90); err != nil {
			return err
		}
		if revenue, err = num(ctx, tx, `SELECT COALESCE(sum(it.amount),0)::float8 FROM invoice_items it JOIN invoices i ON i.id = it.invoice_id WHERE i.status <> 'cancelled' AND i.invoice_number IS NOT NULL
			AND COALESCE(it.charge_type, i.invoice_type) <> 'deposit' AND i.issued_at >= `+d.from+` AND i.issued_at < `+d.to+iW, a); err != nil {
			return err
		}
		if cash, err = num(ctx, tx, `SELECT COALESCE(sum(pm.amount),0)::float8 FROM payments pm WHERE pm.status IN ('paid','refunded') AND pm.method <> 'credit' AND pm.paid_at >= `+d.from+` AND pm.paid_at < `+d.to+pmW, a); err != nil {
			return err
		}
		pending, err := num(ctx, tx, `SELECT count(*)::float8 FROM payments pm WHERE pm.status = 'pending' AND pm.provider_code = 'manual'`+pmW, a)
		if err != nil {
			return err
		}
		// operating cost periode & budget vs actual YTD via modul finance (sumber actual yang sama dengan laporan)
		p := authctx.Must(ctx)
		var pids []uuid.UUID
		all := false
		if in.PropertyID != nil {
			pids = []uuid.UUID{*in.PropertyID}
		} else {
			pids, all = p.PropertyIDsFor("billing.invoices.view")
		}
		now := time.Now()
		act, err := finance.ActualsTx(ctx, tx, pids, all, now.Year())
		if err != nil {
			return err
		}
		var opCost, ytdCost, ytdRevenue float64
		for _, x := range act.Cost {
			for m := 1; m <= int(now.Month()); m++ {
				ytdCost += float64(x[m-1])
			}
		}
		for _, x := range act.Revenue {
			for m := 1; m <= int(now.Month()); m++ {
				ytdRevenue += float64(x[m-1])
			}
		}
		if opCost, err = num(ctx, tx, `SELECT (COALESCE((SELECT sum(COALESCE(w.actual_cost_amount, COALESCE(w.parts_cost_amount,0) + COALESCE(w.service_cost_amount,0) + COALESCE(w.other_cost_amount,0)))
				FROM work_orders w WHERE w.status IN ('completed','closed') AND w.completed_at >= `+d.from+` AND w.completed_at < `+d.to+d.w("w.property_id", "w.location_id")+`),0)
			+ COALESCE((SELECT sum(COALESCE(tc.total_cost, round(tc.quantity * COALESCE(tc.unit_cost,0)))) FROM task_consumables tc JOIN tasks t ON t.id = tc.task_id
				WHERE tc.recorded_at >= `+d.from+` AND tc.recorded_at < `+d.to+d.w("t.property_id", "t.location_id")+`),0)
			+ COALESCE((SELECT sum(c.amount) FROM cost_entries c WHERE c.deleted_at IS NULL AND c.entry_date >= '`+d.fromDate+`'::date AND c.entry_date <= '`+d.toDate+`'::date`+d.w("c.property_id", "")+`),0))::float8`, a); err != nil {
			return err
		}
		budgetArgs := []any{now.Year()}
		bw := ""
		if !all {
			budgetArgs = append(budgetArgs, pids)
			bw = " AND b.property_id = ANY($2)"
		}
		var budgetCostYTD, budgetRevYTD float64
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(bl.amount) FILTER (WHERE bl.kind = 'cost'),0)::float8, COALESCE(sum(bl.amount) FILTER (WHERE bl.kind = 'revenue'),0)::float8
			FROM budget_lines bl JOIN budgets b ON b.id = bl.budget_id WHERE b.fiscal_year = $1 AND b.status = 'approved' AND bl.month <= `+fmt.Sprint(int(now.Month()))+bw, budgetArgs...).Scan(&budgetCostYTD, &budgetRevYTD)
		sfArgs := []any{}
		sfW := ""
		if !all {
			sfArgs = append(sfArgs, pids)
			sfW = " WHERE property_id = ANY($1)"
		}
		var sinking float64
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0)::float8 FROM sinking_fund_entries`+sfW, sfArgs...).Scan(&sinking)
		rate := pct1(collectedOnDue, dueAmt)
		costVar := 0.0
		if budgetCostYTD > 0 {
			costVar = pct1(ytdCost-budgetCostYTD, budgetCostYTD)
		}
		costSev := "normal"
		switch {
		case budgetCostYTD > 0 && costVar > 15:
			costSev = "critical"
		case budgetCostYTD > 0 && costVar > 5:
			costSev = "warning"
		}
		agedSev := "normal"
		if aged90 > 0 {
			agedSev = "warning"
		}
		range_ := "&from=" + d.fromDate + "&to=" + d.toDate
		out.KPIs = []KPI{
			{Key: "collection_rate", Label: "Collection Rate", Value: rate, Unit: "pct", Severity: sevBelow(rate, 90, 75, dueAmt > 0), Hint: "Pembayaran atas tagihan jatuh tempo dalam periode ÷ tagihan jatuh tempo (tanpa draft & batal)", DrillDown: d.link("/reports/collection?" + range_[1:])},
			{Key: "outstanding", Label: "Outstanding", Value: outstanding, Unit: "idr", Severity: "normal", Hint: "Sisa tagihan terbuka saat ini", DrillDown: d.link("/billing/invoices?open=true")},
			{Key: "overdue", Label: "Overdue", Value: overdue, Unit: "idr", Severity: sevAbove(overdueN, 1, 20), Hint: fmt.Sprintf("Sisa tagihan lewat jatuh tempo (%d invoice)", int(overdueN)), DrillDown: d.link("/billing/invoices?status=overdue")},
			{Key: "aging_90", Label: "Aging > 90 Hari", Value: aged90, Unit: "idr", Severity: agedSev, Hint: "Piutang lewat jatuh tempo lebih dari 90 hari", DrillDown: d.link("/billing/aging")},
			{Key: "revenue", Label: "Revenue", Value: revenue, Unit: "idr", Severity: "normal", Hint: "Pendapatan ditagihkan dalam periode (tanpa pajak & deposit)", DrillDown: d.link("/reports/revenue?" + range_[1:])},
			{Key: "cash_collected", Label: "Kas Diterima", Value: cash, Unit: "idr", Severity: "normal", Hint: "Pembayaran lunas dalam periode (tanpa pemakaian saldo kredit)", DrillDown: d.link("/billing/payments?status=paid&paid_from=" + d.fromDate + "&paid_to=" + d.toDate)},
			{Key: "operating_cost", Label: "Operating Cost", Value: opCost, Unit: "idr", Severity: "normal", Hint: "Biaya WO selesai + consumable cleaning + biaya manual dalam periode", DrillDown: d.link("/reports/operating-cost?" + range_[1:])},
			{Key: "budget_cost_variance", Label: "Budget vs Actual (Biaya YTD)", Value: costVar, Unit: "pct", Severity: costSev, Hint: fmt.Sprintf("Realisasi biaya YTD %s vs budget %s", rupiahF(ytdCost), rupiahF(budgetCostYTD)), DrillDown: d.link("/finance/budget-actual")},
			{Key: "revenue_ytd", Label: "Revenue YTD vs Budget", Value: pct1(ytdRevenue, budgetRevYTD), Unit: "pct", Severity: sevBelow(pct1(ytdRevenue, budgetRevYTD), 95, 85, budgetRevYTD > 0), Hint: fmt.Sprintf("Pendapatan YTD %s dari budget %s", rupiahF(ytdRevenue), rupiahF(budgetRevYTD)), DrillDown: d.link("/finance/budget-actual")},
			{Key: "sinking_fund", Label: "Sinking Fund", Value: sinking, Unit: "idr", Severity: "normal", Hint: "Saldo dana sinking fund (Apartment)", DrillDown: d.link("/billing/sinking-fund")},
			{Key: "pending_verification", Label: "Menunggu Verifikasi", Value: pending, Unit: "count", Severity: sevAbove(pending, 1, 10), Hint: "Pembayaran transfer tenant yang menunggu verifikasi Finance", DrillDown: d.link("/billing/payments?status=pending")},
		}
		o6 := "(i.total_amount - i.paid_amount - i.credited_amount)"
		if out.Breakdowns["aging"], err = rows(ctx, tx, `SELECT b.k, b.l, COALESCE(sum(`+o6+`) FILTER (WHERE CASE b.k WHEN 'current' THEN `+days+` <= 0 WHEN '1_30' THEN `+days+` BETWEEN 1 AND 30
				WHEN '31_60' THEN `+days+` BETWEEN 31 AND 60 WHEN '61_90' THEN `+days+` BETWEEN 61 AND 90 ELSE `+days+` > 90 END),0)::float8
			FROM (VALUES ('current','Belum jatuh tempo',1),('1_30','1–30 hari',2),('31_60','31–60 hari',3),('61_90','61–90 hari',4),('90_plus','> 90 hari',5)) b(k,l,o)
			LEFT JOIN invoices i ON i.status IN ('issued','partially_paid','overdue') AND `+o6+` > 0`+iW+`
			LEFT JOIN properties pr ON pr.location_id = i.property_id
			GROUP BY b.k, b.l, b.o ORDER BY b.o`, a, []string{"amount"}, func(k string) string { return d.link("/billing/invoices?aging=" + k) }); err != nil {
			return err
		}
		if out.Breakdowns["revenue_type"], err = rows(ctx, tx, `SELECT COALESCE(it.charge_type, i.invoice_type), COALESCE(it.charge_type, i.invoice_type), COALESCE(sum(it.amount),0)::float8
			FROM invoice_items it JOIN invoices i ON i.id = it.invoice_id WHERE i.status <> 'cancelled' AND i.invoice_number IS NOT NULL AND COALESCE(it.charge_type, i.invoice_type) <> 'deposit'
			AND i.issued_at >= `+d.from+` AND i.issued_at < `+d.to+iW+` GROUP BY 1 ORDER BY 3 DESC`, a, []string{"amount"}, func(k string) string {
			return d.link("/billing/invoices?type=" + k + "&issued_from=" + d.fromDate + "&issued_to=" + d.toDate)
		}); err != nil {
			return err
		}
		if out.Breakdowns["overdue_tenant"], err = rows(ctx, tx, `SELECT COALESCE(i.tenant_id, i.unit_location_id)::text, min(COALESCE(t.name, 'Unit ' || un.unit_number, '')), COALESCE(sum(`+o6+`),0)::float8, count(*)::float8,
			max(`+days+`)::float8
			FROM invoices i JOIN properties pr ON pr.location_id = i.property_id LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN units un ON un.location_id = i.unit_location_id
			WHERE i.status = 'overdue'`+iW+` GROUP BY COALESCE(i.tenant_id, i.unit_location_id) ORDER BY 3 DESC LIMIT 10`, a, []string{"outstanding", "invoices", "oldest_days"}, func(k string) string {
			return d.link("/billing/collections?party=" + k)
		}); err != nil {
			return err
		}
		out.Breakdowns["cost_category"] = []BreakdownRow{}
		for _, c := range finance.CostCategories {
			x := act.Cost[c.Key]
			if x == nil {
				continue
			}
			var v int64
			for m := 1; m <= int(now.Month()); m++ {
				v += x[m-1]
			}
			if v == 0 {
				continue
			}
			out.Breakdowns["cost_category"] = append(out.Breakdowns["cost_category"], BreakdownRow{Key: c.Key, Label: c.Label, Values: map[string]float64{"ytd": float64(v)},
				DrillDown: d.link("/finance/budget-actual")})
		}
		// Attention: pembayaran menunggu verifikasi, janji bayar ingkar, run siap terbit, mutasi belum cocok, pembacaan meter flagged
		type att struct {
			cat, sev, otype, title, link string
			q                            string
		}
		items := []att{
			{"payment_pending", "warning", "payment", "Pembayaran menunggu verifikasi", "/billing/payments/", `SELECT pm.id, pm.payment_number, COALESCE(i.invoice_number,'') || ' · ' || COALESCE(t.name,''), pm.created_at FROM payments pm JOIN invoices i ON i.id = pm.invoice_id LEFT JOIN tenants t ON t.id = i.tenant_id
				WHERE pm.status = 'pending' AND pm.provider_code = 'manual'` + pmW + ` ORDER BY pm.created_at LIMIT 5`},
			{"promise_broken", "warning", "collection_log", "Janji bayar tidak ditepati", "/billing/collections?log=", `SELECT c.id, COALESCE(t.name, 'Unit'), 'Janji ' || to_char(c.promise_date,'DD/MM/YYYY'), c.updated_at FROM collection_logs c LEFT JOIN tenants t ON t.id = c.tenant_id
				WHERE c.promise_status = 'broken' AND c.updated_at > now() - interval '14 days'` + d.w("c.property_id", "c.unit_location_id") + ` ORDER BY c.updated_at DESC LIMIT 5`},
			{"billing_run_ready", "warning", "billing_run", "Draft tagihan periode siap diterbitkan", "/billing/runs/", `SELECT r.id, r.run_number, r.invoice_count::text || ' draft invoice', r.generated_at FROM billing_runs r WHERE r.status = 'generated'` + d.w("r.property_id", "") + ` ORDER BY r.generated_at LIMIT 5`},
			{"statement_unmatched", "warning", "bank_statement_import", "Mutasi bank belum dicocokkan", "/billing/reconciliation/", `SELECT bi.id, COALESCE(bi.file_name,'Impor mutasi'), (SELECT count(*) FROM bank_statement_lines x WHERE x.import_id = bi.id AND x.status IN ('unmatched','suggested'))::text || ' mutasi', bi.imported_at
				FROM bank_statement_imports bi WHERE bi.status = 'open'` + d.w("bi.property_id", "") + ` ORDER BY bi.imported_at LIMIT 5`},
			{"meter_flagged", "warning", "meter_reading", "Pembacaan meter perlu review", "/billing/meter-readings/", `SELECT r.id, m.meter_number, COALESCE(r.anomaly,'flagged'), r.read_at FROM meter_readings r JOIN meters m ON m.id = r.meter_id WHERE r.status = 'flagged'` + d.w("r.property_id", "m.location_id") + ` ORDER BY r.read_at LIMIT 5`},
		}
		for _, x := range items {
			rs, err := tx.Query(ctx, withPeriod(x.q), a...)
			if err != nil {
				return err
			}
			for rs.Next() {
				var id uuid.UUID
				var label, title string
				var since time.Time
				if rs.Scan(&id, &label, &title, &since) != nil {
					continue
				}
				out.Attention = append(out.Attention, AttentionItem{Category: x.cat, Severity: x.sev, ObjectType: x.otype, ObjectID: id, Label: label, Title: x.title + " — " + title,
					Since: since, AgeMinutes: int(time.Since(since).Minutes()), AllowedActions: []string{"view"}, DeepLink: x.link + id.String()})
			}
			rs.Close()
		}
		out.AttentionTotal = len(out.Attention)
		return nil
	})
	return out, err
}

func rupiahF(v float64) string {
	n := int64(v)
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprint(n)
	var b []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b = append(b, '.')
		}
		b = append(b, s[i])
	}
	if neg {
		return "-Rp" + string(b)
	}
	return "Rp" + string(b)
}
