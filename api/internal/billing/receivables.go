package billing

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantscope"
)

// ---------- Receivable & Collection (PRD P4 v2.1 §7: P4-OUT-01..03, P4-AGE-01, P4-COL-01..04) ----------

const (
	EventInvoiceReminder         = "invoice.reminder"
	EventCollectionPromiseBroken = "collection_promise.broken"
)

// Sweep (worker, 15 menit): overdue (sekali, B-03), pengingat bertahap H-n/H+n per pengaturan (P4-COL-02), payment kedaluwarsa,
// akrual denda (P4-PND-02), janji bayar ditepati/ingkar (P4-COL-03), lalu draft tagihan periode terjadwal (P4-BRL-03).
func (s *Service) Sweep(ctx context.Context, orgID uuid.UUID) error {
	ctx = authctx.With(ctx, authctx.System(orgID))
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE invoices SET status = 'overdue', overdue_notified_at = now() WHERE status IN ('issued','partially_paid') AND due_at < now() RETURNING id, invoice_number, property_id`)
		if err != nil {
			return err
		}
		type ref struct {
			id  uuid.UUID
			num *string
			pid uuid.UUID
		}
		var overdue []ref
		justOverdue := map[uuid.UUID]bool{}
		for rows.Next() {
			var r ref
			if rows.Scan(&r.id, &r.num, &r.pid) == nil {
				overdue = append(overdue, r)
				justOverdue[r.id] = true
			}
		}
		rows.Close()
		if s.Jobs != nil {
			for _, r := range overdue {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventInvoiceOverdue, OrganizationID: orgID, PropertyID: &r.pid, ObjectType: "invoice", ObjectID: r.id, ObjectLabel: strPtrVal(r.num), Payload: map[string]any{"domain": "finance"}})
			}
		}
		if err := s.remindersTx(ctx, tx, orgID, justOverdue); err != nil {
			return err
		}
		_, _ = tx.Exec(ctx, `UPDATE payments SET status = 'expired', failure_reason = 'Batas waktu pembayaran habis' WHERE status IN ('initiated','pending') AND expires_at IS NOT NULL AND expires_at < now()`)
		if _, err := s.AccruePenaltiesTx(ctx, tx); err != nil {
			return err
		}
		return s.promisesTx(ctx, tx, orgID)
	})
	if err != nil {
		return err
	}
	_, err = s.AutoGenerateSweep(ctx, orgID)
	return err
}

func stageLabel(o int) string {
	switch {
	case o < 0:
		return fmt.Sprintf("H-%d", -o)
	case o == 0:
		return "H"
	}
	return fmt.Sprintf("H+%d", o)
}

// remindersTx: satu pengingat per tahap; tahap sebelum tanggal terbit dilewati; transisi overdue pada sweep yang sama menggantikan
// pengingat H+n pertama (tidak dobel).
func (s *Service) remindersTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, justOverdue map[uuid.UUID]bool) error {
	rows, err := tx.Query(ctx, `SELECT i.id, i.property_id, i.invoice_number, i.reminder_stage, `+daysPastDueSQL("i")+`,
		COALESCE((i.issued_at AT TIME ZONE pr.timezone)::date, (now() AT TIME ZONE pr.timezone)::date) - COALESCE(i.due_date, (i.due_at AT TIME ZONE pr.timezone)::date),
		i.total_amount - i.paid_amount - i.credited_amount
		FROM invoices i JOIN properties pr ON pr.location_id = i.property_id
		WHERE i.status IN ('issued','partially_paid','overdue') AND i.invoice_number IS NOT NULL AND i.total_amount - i.paid_amount - i.credited_amount > 0`)
	if err != nil {
		return err
	}
	type rem struct {
		id, pid        uuid.UUID
		num            string
		stage          *int32
		rel, issuedRel int
		outstanding    int64
	}
	var list []rem
	for rows.Next() {
		var r rem
		if rows.Scan(&r.id, &r.pid, &r.num, &r.stage, &r.rel, &r.issuedRel, &r.outstanding) == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	offsets := map[uuid.UUID][]int32{}
	for _, r := range list {
		offs, ok := offsets[r.pid]
		if !ok {
			pid := r.pid
			offs = s.settingsFor(ctx, tx, &pid).ReminderOffsets
			offsets[r.pid] = offs
		}
		best, found := int32(0), false
		for _, o := range offs {
			if int(o) > r.rel || int(o) < r.issuedRel || (r.stage != nil && o <= *r.stage) {
				continue
			}
			if !found || o > best {
				best, found = o, true
			}
		}
		if !found {
			continue
		}
		tag, err := tx.Exec(ctx, `INSERT INTO invoice_reminders (invoice_id, organization_id, offset_days) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, r.id, orgID, best)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE invoices SET reminder_stage = $2, last_reminded_at = now(), due_soon_notified_at = CASE WHEN $2 <= 0 THEN now() ELSE due_soon_notified_at END WHERE id = $1`, r.id, best); err != nil {
			return err
		}
		if tag.RowsAffected() == 0 || s.Jobs == nil || (best > 0 && justOverdue[r.id]) {
			continue
		}
		evType := EventInvoiceReminder
		if best <= 0 {
			evType = EventInvoiceDueSoon
		}
		pid := r.pid
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: evType, OrganizationID: orgID, PropertyID: &pid, ObjectType: "invoice", ObjectID: r.id, ObjectLabel: r.num,
			Payload: map[string]any{"domain": "finance", "offset_days": best, "stage": stageLabel(int(best)), "outstanding": r.outstanding, "days_overdue": max(r.rel, 0)}})
	}
	return nil
}

// promisesTx: janji bayar terbuka → kept bila penerimaan sejak dicatat ≥ janji (atau semua invoice terkait lunas); lewat tanggal
// tanpa terpenuhi → broken + notifikasi Finance.
func (s *Service) promisesTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT c.id, c.property_id, c.tenant_id, c.unit_location_id, c.invoice_ids, c.promise_date, c.promise_amount, c.created_at,
		c.promise_date < (now() AT TIME ZONE pr.timezone)::date, COALESCE(t.name, 'Unit ' || un.unit_number, l.name, '')
		FROM collection_logs c JOIN properties pr ON pr.location_id = c.property_id LEFT JOIN tenants t ON t.id = c.tenant_id
		LEFT JOIN locations l ON l.id = c.unit_location_id LEFT JOIN units un ON un.location_id = c.unit_location_id
		WHERE c.promise_status = 'open'`)
	if err != nil {
		return err
	}
	type pr struct {
		id, pid      uuid.UUID
		tenant, unit *uuid.UUID
		invoices     []uuid.UUID
		date         time.Time
		amount       *int64
		created      time.Time
		past         bool
		label        string
	}
	var list []pr
	for rows.Next() {
		var v pr
		if rows.Scan(&v.id, &v.pid, &v.tenant, &v.unit, &v.invoices, &v.date, &v.amount, &v.created, &v.past, &v.label) == nil {
			list = append(list, v)
		}
	}
	rows.Close()
	for _, v := range list {
		var received int64
		var openCount int
		if len(v.invoices) > 0 {
			_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(p.amount),0) FROM payments p WHERE p.invoice_id = ANY($1) AND p.status = 'paid' AND p.method <> 'credit' AND p.paid_at >= $2`, v.invoices, v.created).Scan(&received)
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM invoices WHERE id = ANY($1) AND status IN ('issued','partially_paid','overdue')`, v.invoices).Scan(&openCount)
		} else {
			_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(p.amount),0) FROM payments p JOIN invoices i ON i.id = p.invoice_id WHERE p.status = 'paid' AND p.method <> 'credit' AND p.paid_at >= $3
				AND (($1::uuid IS NOT NULL AND i.tenant_id = $1) OR ($1::uuid IS NULL AND i.unit_location_id = $2))`, v.tenant, v.unit, v.created).Scan(&received)
			openCount = 1
		}
		kept := (v.amount != nil && received >= *v.amount) || (v.amount == nil && len(v.invoices) > 0 && openCount == 0)
		switch {
		case kept:
			_, _ = tx.Exec(ctx, `UPDATE collection_logs SET promise_status = 'kept' WHERE id = $1`, v.id)
		case v.past:
			_, _ = tx.Exec(ctx, `UPDATE collection_logs SET promise_status = 'broken' WHERE id = $1`, v.id)
			if s.Jobs != nil {
				pid := v.pid
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventCollectionPromiseBroken, OrganizationID: orgID, PropertyID: &pid, ObjectType: "collection_log", ObjectID: v.id, ObjectLabel: v.label,
					Payload: map[string]any{"domain": "finance", "promise_date": v.date.Format("2006-01-02"), "promise_amount": v.amount, "received": received}})
			}
		}
	}
	return nil
}

// ---------- Aging (P4-AGE-01) & outstanding per tenant/unit/property (P4-OUT-01) ----------

type AgingRow struct {
	Kind           string     `json:"kind"` // property | tenant | unit
	ID             *uuid.UUID `json:"id"`
	Label          string     `json:"label"`
	PropertyID     uuid.UUID  `json:"property_id"`
	PropertyName   string     `json:"property_name"`
	TenantID       *uuid.UUID `json:"tenant_id"`
	UnitLocationID *uuid.UUID `json:"unit_location_id"`
	Current        int64      `json:"current"`
	D1to30         int64      `json:"d1_30"`
	D31to60        int64      `json:"d31_60"`
	D61to90        int64      `json:"d61_90"`
	D90Plus        int64      `json:"d90_plus"`
	Total          int64      `json:"total"`
	InvoiceCount   int        `json:"invoice_count"`
	OldestDays     int        `json:"oldest_days_overdue"`
}

type AgingBucket struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type AgingReport struct {
	GroupBy      string        `json:"group_by"`
	AsOf         time.Time     `json:"as_of"`
	Buckets      []AgingBucket `json:"buckets"`
	Rows         []AgingRow    `json:"rows"`
	Totals       AgingRow      `json:"totals"`
	CurrencyCode string        `json:"currency_code"`
}

var agingBuckets = []AgingBucket{{"current", "Belum jatuh tempo"}, {"1_30", "1–30 hari"}, {"31_60", "31–60 hari"}, {"61_90", "61–90 hari"}, {"90_plus", "> 90 hari"}}

func (s *Service) Aging(ctx context.Context, propertyID *uuid.UUID, groupBy string, tenantID *uuid.UUID) (*AgingReport, error) {
	p := authctx.Must(ctx)
	if groupBy == "" {
		groupBy = "tenant"
	}
	if groupBy != "tenant" && groupBy != "unit" && groupBy != "property" {
		return nil, apperr.Validation("group_by harus tenant, unit, atau property").WithField("group_by", "tidak valid")
	}
	out := &AgingReport{GroupBy: groupBy, AsOf: time.Now(), Buckets: agingBuckets, Rows: []AgingRow{}, CurrencyCode: "IDR"}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var args []any
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		sw, err := scopeInvoices(p, "billing.invoices.view", propertyID, add)
		if err != nil {
			return err
		}
		where := " WHERE i.status IN ('issued','partially_paid','overdue') AND i.total_amount - i.paid_amount - i.credited_amount > 0" + sw
		if tenantID != nil {
			where += " AND i.tenant_id = " + add(*tenantID)
		}
		var key, label, extra string
		switch groupBy {
		case "property":
			key, label, extra = "i.property_id", "pl.name", "NULL::uuid, NULL::uuid"
		case "unit":
			key, label, extra = "i.unit_location_id", "COALESCE('Unit ' || un.unit_number, l.name, 'Tanpa unit')", "NULL::uuid, i.unit_location_id"
		default:
			key = "COALESCE(i.tenant_id, i.unit_location_id)"
			label = "COALESCE(t.name, 'Unit ' || un.unit_number || ' (tanpa tenant)', l.name, 'Tanpa tenant')"
			extra = "i.tenant_id, CASE WHEN i.tenant_id IS NULL THEN i.unit_location_id END"
		}
		d := daysPastDueSQL("i")
		o := "(i.total_amount - i.paid_amount - i.credited_amount)"
		q := `SELECT ` + key + `, min(` + label + `), i.property_id, min(pl.name), ` + extra + `,
			COALESCE(sum(` + o + `) FILTER (WHERE ` + d + ` <= 0),0), COALESCE(sum(` + o + `) FILTER (WHERE ` + d + ` BETWEEN 1 AND 30),0),
			COALESCE(sum(` + o + `) FILTER (WHERE ` + d + ` BETWEEN 31 AND 60),0), COALESCE(sum(` + o + `) FILTER (WHERE ` + d + ` BETWEEN 61 AND 90),0),
			COALESCE(sum(` + o + `) FILTER (WHERE ` + d + ` > 90),0), sum(` + o + `), count(*), GREATEST(max(` + d + `),0)
			FROM invoices i JOIN properties pr ON pr.location_id = i.property_id JOIN locations pl ON pl.id = i.property_id LEFT JOIN tenants t ON t.id = i.tenant_id
			LEFT JOIN locations l ON l.id = i.unit_location_id LEFT JOIN units un ON un.location_id = i.unit_location_id` + where + `
			GROUP BY ` + key + `, i.property_id, ` + extra + ` ORDER BY sum(` + o + `) DESC LIMIT 500`
		rows, err := tx.Query(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r AgingRow
			var lbl *string
			if err := rows.Scan(&r.ID, &lbl, &r.PropertyID, &r.PropertyName, &r.TenantID, &r.UnitLocationID, &r.Current, &r.D1to30, &r.D31to60, &r.D61to90, &r.D90Plus, &r.Total, &r.InvoiceCount, &r.OldestDays); err != nil {
				return err
			}
			r.Label = strPtrVal(lbl)
			r.Kind = groupBy
			if groupBy == "tenant" && r.TenantID == nil {
				r.Kind = "unit"
			}
			out.Rows = append(out.Rows, r)
			out.Totals.Current += r.Current
			out.Totals.D1to30 += r.D1to30
			out.Totals.D31to60 += r.D31to60
			out.Totals.D61to90 += r.D61to90
			out.Totals.D90Plus += r.D90Plus
			out.Totals.Total += r.Total
			out.Totals.InvoiceCount += r.InvoiceCount
			if r.OldestDays > out.Totals.OldestDays {
				out.Totals.OldestDays = r.OldestDays
			}
		}
		out.Totals.Kind, out.Totals.Label = "total", "Total"
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---------- Statement of account (P4-OUT-02) ----------

type StatementEntry struct {
	Date        time.Time `json:"date"`
	Kind        string    `json:"kind"` // invoice | void | credit_note | payment | refund | credit
	Reference   *string   `json:"reference"`
	Description string    `json:"description"`
	Debit       int64     `json:"debit"`
	Credit      int64     `json:"credit"`
	Balance     int64     `json:"balance"`
	ObjectType  string    `json:"object_type"`
	ObjectID    uuid.UUID `json:"object_id"`
}

type Statement struct {
	PropertyID     uuid.UUID        `json:"property_id"`
	PropertyName   string           `json:"property_name"`
	TenantID       *uuid.UUID       `json:"tenant_id"`
	TenantName     *string          `json:"tenant_name"`
	UnitLocationID *uuid.UUID       `json:"unit_location_id"`
	UnitLabel      *string          `json:"unit_label"`
	From           string           `json:"from"`
	To             string           `json:"to"`
	Opening        int64            `json:"opening_balance"`
	TotalDebit     int64            `json:"total_debit"`
	TotalCredit    int64            `json:"total_credit"`
	Closing        int64            `json:"closing_balance"` // + = terutang, − = kelebihan (kredit)
	Outstanding    int64            `json:"outstanding_amount"`
	CreditBalance  int64            `json:"credit_balance"`
	DepositBalance int64            `json:"deposit_balance"`
	Entries        []StatementEntry `json:"entries"`
	CurrencyCode   string           `json:"currency_code"`
	GeneratedAt    time.Time        `json:"generated_at"`
}

// statementTx: invCond/ledgerCond = kondisi pihak (alias i untuk invoice, ce untuk ledger) dengan argumen args.
func statementTx(ctx context.Context, tx pgx.Tx, invCond, ledgerCond string, args []any, from, to time.Time, loc *time.Location) (*Statement, error) {
	n := len(args)
	q := `WITH inv AS (SELECT i.id FROM invoices i WHERE ` + invCond + `)
	SELECT i.issued_at, 'invoice', i.invoice_number, COALESCE(i.description, i.invoice_type), i.total_amount, 0::bigint, 'invoice', i.id FROM invoices i WHERE i.id IN (SELECT id FROM inv) AND i.issued_at IS NOT NULL AND i.invoice_number IS NOT NULL
	UNION ALL
	SELECT i.cancelled_at, 'void', i.invoice_number, 'Pembatalan ' || i.invoice_number || COALESCE(' — ' || i.cancel_reason, ''), 0, i.total_amount - i.credited_amount, 'invoice', i.id
	  FROM invoices i WHERE i.id IN (SELECT id FROM inv) AND i.status = 'cancelled' AND i.invoice_number IS NOT NULL AND i.cancelled_at IS NOT NULL
	UNION ALL
	SELECT c.decided_at, 'credit_note', c.credit_note_number, 'Credit note ' || COALESCE(i.invoice_number, '') || ' — ' || c.reason, 0, c.amount, 'credit_note', c.id
	  FROM credit_notes c JOIN invoices i ON i.id = c.invoice_id WHERE c.invoice_id IN (SELECT id FROM inv) AND c.status = 'approved'
	UNION ALL
	SELECT p.paid_at, 'payment', COALESCE(p.receipt_number, p.payment_number), 'Pembayaran ' || COALESCE(i.invoice_number, '') || ' (' || p.method || ')' || COALESCE(' · ' || p.reference, ''), 0, p.amount, 'payment', p.id
	  FROM payments p JOIN invoices i ON i.id = p.invoice_id WHERE p.invoice_id IN (SELECT id FROM inv) AND p.status IN ('paid','refunded') AND p.method <> 'credit' AND p.paid_at IS NOT NULL
	UNION ALL
	SELECT p.refunded_at, 'refund', p.payment_number, 'Refund ' || p.payment_number || COALESCE(' — ' || p.refund_reason, ''), p.amount, 0, 'payment', p.id
	  FROM payments p WHERE p.invoice_id IN (SELECT id FROM inv) AND p.status = 'refunded' AND p.method <> 'credit' AND p.refunded_at IS NOT NULL
	    AND NOT EXISTS (SELECT 1 FROM tenant_credit_entries x WHERE x.payment_id = p.id AND x.entry_type = 'adjustment')
	UNION ALL
	SELECT ce.created_at, 'credit', NULL, COALESCE(ce.description, 'Saldo kredit'), CASE WHEN ce.amount < 0 THEN -ce.amount ELSE 0 END, CASE WHEN ce.amount > 0 THEN ce.amount ELSE 0 END, 'credit_entry', ce.id
	  FROM tenant_credit_entries ce WHERE (` + ledgerCond + `) AND ce.payment_id IS NULL AND ce.credit_note_id IS NULL AND ce.entry_type IN ('overpayment','adjustment','refunded')
	ORDER BY 1, 2`
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	st := &Statement{Entries: []StatementEntry{}, CurrencyCode: "IDR", GeneratedAt: time.Now(), From: from.Format("2006-01-02"), To: to.Format("2006-01-02")}
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	end := time.Date(to.Year(), to.Month(), to.Day(), 23, 59, 59, 999999999, loc)
	for rows.Next() {
		var e StatementEntry
		if err := rows.Scan(&e.Date, &e.Kind, &e.Reference, &e.Description, &e.Debit, &e.Credit, &e.ObjectType, &e.ObjectID); err != nil {
			return nil, err
		}
		switch {
		case e.Date.Before(start):
			st.Opening += e.Debit - e.Credit
		case !e.Date.After(end):
			st.Entries = append(st.Entries, e)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	bal := st.Opening
	for i := range st.Entries {
		e := &st.Entries[i]
		bal += e.Debit - e.Credit
		e.Balance = bal
		st.TotalDebit += e.Debit
		st.TotalCredit += e.Credit
	}
	st.Closing = bal
	_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(i.total_amount - i.paid_amount - i.credited_amount),0) FROM invoices i WHERE (`+invCond+`) AND i.status IN ('issued','partially_paid','overdue')`, args[:n]...).Scan(&st.Outstanding)
	_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(ce.amount),0) FROM tenant_credit_entries ce WHERE `+ledgerCond, args[:n]...).Scan(&st.CreditBalance)
	_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(ce.amount),0) FROM deposit_entries ce WHERE `+ledgerCond, args[:n]...).Scan(&st.DepositBalance)
	return st, nil
}

func statementRange(fromS, toS *string, loc *time.Location) (time.Time, time.Time, error) {
	now := time.Now().In(loc)
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, -6, 0)
	if t, err := parseDate(fromS); err != nil {
		return from, to, err
	} else if t != nil {
		from = *t
	}
	if t, err := parseDate(toS); err != nil {
		return from, to, err
	} else if t != nil {
		to = *t
	}
	if to.Before(from) {
		return from, to, apperr.Validation("to harus setelah from").WithField("to", "sebelum from")
	}
	return from, to, nil
}

// Statement (staf): per tenant atau per unit dalam satu property.
func (s *Service) Statement(ctx context.Context, propertyID uuid.UUID, tenantID, unitID *uuid.UUID, from, to *string) (*Statement, error) {
	p := authctx.Must(ctx)
	if !p.HasAnyOnProperty("billing.invoices.view", propertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.invoices.view")
	}
	if tenantID == nil && unitID == nil {
		return nil, apperr.Validation("tenant_id atau unit_location_id wajib").WithField("tenant_id", "wajib")
	}
	var out *Statement
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.validateParties(ctx, tx, propertyID, tenantID, unitID, true); err != nil {
			return err
		}
		loc := property.PropertyTimezone(ctx, tx, propertyID)
		f, t, err := statementRange(from, to, loc)
		if err != nil {
			return err
		}
		invCond, ledgerCond := "i.property_id = $1 AND i.tenant_id = $2", "ce.property_id = $1 AND ce.tenant_id = $2"
		args := []any{propertyID, tenantID}
		if tenantID == nil {
			invCond, ledgerCond = "i.property_id = $1 AND i.unit_location_id = $2", "ce.property_id = $1 AND ce.unit_location_id = $2 AND ce.tenant_id IS NULL"
			args = []any{propertyID, unitID}
		}
		st, err := statementTx(ctx, tx, invCond, ledgerCond, args, f, t, loc)
		if err != nil {
			return err
		}
		st.PropertyID, st.TenantID, st.UnitLocationID = propertyID, tenantID, unitID
		_ = tx.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, propertyID).Scan(&st.PropertyName)
		if tenantID != nil {
			_ = tx.QueryRow(ctx, `SELECT name FROM tenants WHERE id = $1`, *tenantID).Scan(&st.TenantName)
		}
		if unitID != nil {
			_ = tx.QueryRow(ctx, `SELECT COALESCE('Unit ' || un.unit_number, l.name) FROM locations l LEFT JOIN units un ON un.location_id = l.id WHERE l.id = $1`, *unitID).Scan(&st.UnitLabel)
		}
		out = st
		return nil
	})
	return out, err
}

// TenantStatement: statement pihak tenant user yang login (tenant atau unit aksesnya).
func (s *Service) TenantStatement(ctx context.Context, from, to *string) (*Statement, error) {
	var out *Statement
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		loc := property.PropertyTimezone(ctx, tx, sc.PropertyID)
		f, t, err := statementRange(from, to, loc)
		if err != nil {
			return err
		}
		invCond, args := tenantInvoiceWhere(sc, 1)
		ledgerCond := "(ce.tenant_id IS NULL AND ce.unit_location_id = ANY($1))"
		if sc.TenantID != nil {
			ledgerCond = "(ce.tenant_id = $1 OR (ce.tenant_id IS NULL AND ce.unit_location_id = ANY($2)))"
		}
		st, err := statementTx(ctx, tx, invCond, ledgerCond, args, f, t, loc)
		if err != nil {
			return err
		}
		st.PropertyID, st.TenantID = sc.PropertyID, sc.TenantID
		_ = tx.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, sc.PropertyID).Scan(&st.PropertyName)
		if sc.TenantID != nil {
			_ = tx.QueryRow(ctx, `SELECT name FROM tenants WHERE id = $1`, *sc.TenantID).Scan(&st.TenantName)
		} else if u := sc.PrimaryUnit(); u != nil {
			id, name := u.ID, u.Name
			st.UnitLocationID, st.UnitLabel = &id, &name
		}
		out = st
		return nil
	})
	return out, err
}

// ---------- Log penagihan & janji bayar (P4-COL-03) ----------

var collectionChannels = map[string]bool{"phone": true, "whatsapp": true, "visit": true, "letter": true, "other": true}
var collectionOutcomes = map[string]bool{"contacted": true, "no_answer": true, "promise_to_pay": true, "dispute": true, "paid": true, "other": true}

type CollectionLog struct {
	ID             uuid.UUID   `json:"id"`
	PropertyID     uuid.UUID   `json:"property_id"`
	TenantID       *uuid.UUID  `json:"tenant_id"`
	TenantName     *string     `json:"tenant_name"`
	UnitLocationID *uuid.UUID  `json:"unit_location_id"`
	UnitLabel      *string     `json:"unit_label"`
	InvoiceIDs     []uuid.UUID `json:"invoice_ids"`
	InvoiceNumbers []string    `json:"invoice_numbers"`
	Channel        string      `json:"channel"`
	ContactPerson  *string     `json:"contact_person"`
	Outcome        string      `json:"outcome"`
	Notes          *string     `json:"notes"`
	PromiseDate    *string     `json:"promise_date"`
	PromiseAmount  *int64      `json:"promise_amount"`
	PromiseStatus  *string     `json:"promise_status"`
	FollowUpOn     *string     `json:"follow_up_on"`
	CreatedByName  *string     `json:"created_by_name"`
	CreatedAt      time.Time   `json:"created_at"`
	AllowedActions []string    `json:"allowed_actions"`
	Version        int         `json:"version"`
}

const collSelect = `SELECT c.id, c.property_id, c.tenant_id, t.name, c.unit_location_id, COALESCE('Unit ' || un.unit_number, l.name), c.invoice_ids,
	COALESCE((SELECT array_agg(x.invoice_number ORDER BY x.due_at) FROM invoices x WHERE x.id = ANY(c.invoice_ids) AND x.invoice_number IS NOT NULL), '{}'),
	c.channel, c.contact_person, c.outcome, c.notes, to_char(c.promise_date,'YYYY-MM-DD'), c.promise_amount, c.promise_status, to_char(c.follow_up_on,'YYYY-MM-DD'), u.full_name, c.created_at, c.version
	FROM collection_logs c LEFT JOIN tenants t ON t.id = c.tenant_id LEFT JOIN locations l ON l.id = c.unit_location_id LEFT JOIN units un ON un.location_id = c.unit_location_id
	LEFT JOIN users u ON u.id = c.created_by`

func scanColl(row pgx.Row) (*CollectionLog, error) {
	var v CollectionLog
	if err := row.Scan(&v.ID, &v.PropertyID, &v.TenantID, &v.TenantName, &v.UnitLocationID, &v.UnitLabel, &v.InvoiceIDs, &v.InvoiceNumbers, &v.Channel, &v.ContactPerson, &v.Outcome, &v.Notes,
		&v.PromiseDate, &v.PromiseAmount, &v.PromiseStatus, &v.FollowUpOn, &v.CreatedByName, &v.CreatedAt, &v.Version); err != nil {
		return nil, err
	}
	if v.InvoiceIDs == nil {
		v.InvoiceIDs = []uuid.UUID{}
	}
	if v.InvoiceNumbers == nil {
		v.InvoiceNumbers = []string{}
	}
	return &v, nil
}

func (s *Service) collActions(ctx context.Context, v *CollectionLog) {
	p := authctx.Must(ctx)
	v.AllowedActions = []string{"view"}
	if p.HasOnProperty("billing.collections.manage", v.PropertyID) {
		v.AllowedActions = append(v.AllowedActions, "update")
		if v.PromiseStatus != nil && *v.PromiseStatus == "open" {
			v.AllowedActions = append(v.AllowedActions, "mark_kept", "mark_broken", "cancel_promise")
		}
	}
}

type CollectionLogInput struct {
	PropertyID     uuid.UUID   `json:"property_id"`
	TenantID       *uuid.UUID  `json:"tenant_id"`
	UnitLocationID *uuid.UUID  `json:"unit_location_id"`
	InvoiceIDs     []uuid.UUID `json:"invoice_ids"`
	Channel        string      `json:"channel"`
	ContactPerson  *string     `json:"contact_person"`
	Outcome        string      `json:"outcome"`
	Notes          *string     `json:"notes"`
	PromiseDate    *string     `json:"promise_date"`
	PromiseAmount  *int64      `json:"promise_amount"`
	FollowUpOn     *string     `json:"follow_up_on"`
}

func (s *Service) CreateCollectionLog(ctx context.Context, in CollectionLogInput) (*CollectionLog, error) {
	p := authctx.Must(ctx)
	if !p.HasOnProperty("billing.collections.manage", in.PropertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.collections.manage")
	}
	if !collectionChannels[in.Channel] {
		return nil, apperr.Validation("channel harus phone, whatsapp, visit, letter, atau other").WithField("channel", "tidak valid")
	}
	if in.Outcome == "" {
		in.Outcome = "contacted"
	}
	if !collectionOutcomes[in.Outcome] {
		return nil, apperr.Validation("outcome tidak valid").WithField("outcome", "tidak valid")
	}
	var out *CollectionLog
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.validateParties(ctx, tx, in.PropertyID, in.TenantID, in.UnitLocationID, true); err != nil {
			return err
		}
		if len(in.InvoiceIDs) > 0 {
			var n int
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM invoices WHERE id = ANY($1) AND property_id = $2 AND invoice_number IS NOT NULL
				AND (($3::uuid IS NOT NULL AND tenant_id = $3) OR ($3::uuid IS NULL AND unit_location_id = $4))`, in.InvoiceIDs, in.PropertyID, in.TenantID, in.UnitLocationID).Scan(&n)
			if n != len(in.InvoiceIDs) {
				return apperr.Validation("invoice_ids harus invoice terbit milik pihak ini").WithField("invoice_ids", "tidak valid")
			}
		}
		loc := property.PropertyTimezone(ctx, tx, in.PropertyID)
		today := time.Now().In(loc).Format("2006-01-02")
		var promiseStatus *string
		if in.Outcome == "promise_to_pay" {
			if in.PromiseDate == nil || *in.PromiseDate == "" {
				return apperr.Validation("promise_date wajib untuk janji bayar").WithField("promise_date", "wajib")
			}
			if _, err := parseDate(in.PromiseDate); err != nil {
				return err
			}
			if *in.PromiseDate < today {
				return apperr.Validation("promise_date tidak boleh di masa lalu").WithField("promise_date", "masa lalu")
			}
			if in.PromiseAmount != nil && *in.PromiseAmount <= 0 {
				return apperr.Validation("promise_amount harus > 0").WithField("promise_amount", "harus > 0")
			}
			open := "open"
			promiseStatus = &open
		} else {
			in.PromiseDate, in.PromiseAmount = nil, nil
		}
		if _, err := parseDate(in.FollowUpOn); err != nil {
			return err
		}
		ids := in.InvoiceIDs
		if ids == nil {
			ids = []uuid.UUID{}
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO collection_logs (organization_id, property_id, tenant_id, unit_location_id, invoice_ids, channel, contact_person, outcome, notes, promise_date, promise_amount, promise_status, follow_up_on, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,NULLIF($9,''),$10,$11,$12,$13,$14,$14) RETURNING id`, p.OrganizationID, in.PropertyID, in.TenantID, in.UnitLocationID, ids, in.Channel, deref(in.ContactPerson), in.Outcome, deref(in.Notes),
			dateArg(in.PromiseDate), in.PromiseAmount, promiseStatus, dateArg(in.FollowUpOn), p.UserID).Scan(&id); err != nil {
			return err
		}
		for _, iid := range ids {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: iid, Action: "collection_logged", Payload: map[string]any{"channel": in.Channel, "outcome": in.Outcome, "promise_date": in.PromiseDate, "promise_amount": in.PromiseAmount}})
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "collection_log", EntityID: &id, EntityLabel: in.Channel + " · " + in.Outcome, After: in})
		var err error
		out, err = scanColl(tx.QueryRow(ctx, collSelect+` WHERE c.id = $1`, id))
		if err == nil {
			s.collActions(ctx, out)
		}
		return err
	})
	return out, err
}

type CollectionLogUpdate struct {
	Action     string  `json:"action"` // mark_kept | mark_broken | cancel_promise | update
	Notes      *string `json:"notes"`
	FollowUpOn *string `json:"follow_up_on"`
}

func (s *Service) UpdateCollectionLog(ctx context.Context, id uuid.UUID, in CollectionLogUpdate) (*CollectionLog, error) {
	var out *CollectionLog
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		v, err := scanColl(tx.QueryRow(ctx, collSelect+` WHERE c.id = $1 FOR UPDATE OF c`, id))
		if err != nil {
			return apperr.NotFound("Log penagihan")
		}
		s.collActions(ctx, v)
		if in.Action == "" {
			in.Action = "update"
		}
		if !has(v.AllowedActions, in.Action) {
			if !has(v.AllowedActions, "update") {
				return apperr.Forbidden("Memerlukan billing.collections.manage")
			}
			return apperr.InvalidTransition("Aksi " + in.Action + " tidak tersedia")
		}
		if _, err := parseDate(in.FollowUpOn); err != nil {
			return err
		}
		p := authctx.Must(ctx)
		st := map[string]string{"mark_kept": "kept", "mark_broken": "broken", "cancel_promise": "cancelled"}[in.Action]
		if _, err := tx.Exec(ctx, `UPDATE collection_logs SET promise_status = COALESCE(NULLIF($2,''), promise_status),
			notes = CASE WHEN $3::text IS NULL THEN notes ELSE NULLIF($3,'') END,
			follow_up_on = CASE WHEN $4::text IS NULL THEN follow_up_on WHEN $4 = '' THEN NULL ELSE $4::date END, updated_by = $5 WHERE id = $1`, id, st, in.Notes, in.FollowUpOn, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "collection_log", EntityID: &id, EntityLabel: v.Channel, After: in})
		out, err = scanColl(tx.QueryRow(ctx, collSelect+` WHERE c.id = $1`, id))
		if err == nil {
			s.collActions(ctx, out)
		}
		return err
	})
	return out, err
}

type CollectionFilter struct {
	PropertyID    *uuid.UUID
	TenantID      *uuid.UUID
	UnitID        *uuid.UUID
	InvoiceID     *uuid.UUID
	PromiseStatus string
	FollowUpDue   bool
}

func (s *Service) ListCollectionLogs(ctx context.Context, f CollectionFilter, page httpx.Page) ([]CollectionLog, *string, error) {
	p := authctx.Must(ctx)
	var out []CollectionLog
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var args []any
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE true"
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("billing.collections.view", *f.PropertyID) {
				return apperr.Forbidden("")
			}
			where += " AND c.property_id = " + add(*f.PropertyID)
		} else if pids, all := p.PropertyIDsFor("billing.collections.view"); !all {
			where += " AND c.property_id = ANY(" + add(pids) + ")"
		}
		if f.TenantID != nil {
			where += " AND c.tenant_id = " + add(*f.TenantID)
		}
		if f.UnitID != nil {
			where += " AND c.unit_location_id = " + add(*f.UnitID)
		}
		if f.InvoiceID != nil {
			where += " AND " + add(*f.InvoiceID) + " = ANY(c.invoice_ids)"
		}
		if f.PromiseStatus != "" {
			where += " AND c.promise_status = " + add(f.PromiseStatus)
		}
		if f.FollowUpDue {
			where += " AND c.follow_up_on <= current_date"
		}
		if page.Cursor != nil {
			where += fmt.Sprintf(" AND (c.created_at, c.id) < (%s::timestamptz, %s)", add(page.Cursor.Value), add(page.Cursor.ID))
		}
		rows, err := tx.Query(ctx, collSelect+where+fmt.Sprintf(" ORDER BY c.created_at DESC, c.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanColl(rows)
			if err != nil {
				return err
			}
			s.collActions(ctx, v)
			out = append(out, *v)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	if out == nil {
		out = []CollectionLog{}
	}
	return out, next, err
}

// ---------- Daftar kerja penagihan (P4-COL-04) ----------

type WorkItem struct {
	Kind           string     `json:"kind"` // tenant | unit
	TenantID       *uuid.UUID `json:"tenant_id"`
	UnitLocationID *uuid.UUID `json:"unit_location_id"`
	Label          string     `json:"label"`
	PropertyID     uuid.UUID  `json:"property_id"`
	PropertyName   string     `json:"property_name"`
	Outstanding    int64      `json:"outstanding_amount"`
	OverdueAmount  int64      `json:"overdue_amount"`
	OverdueCount   int        `json:"overdue_count"`
	OldestDays     int        `json:"oldest_days_overdue"`
	LastContactAt  *time.Time `json:"last_contact_at"`
	LastOutcome    *string    `json:"last_outcome"`
	PromiseDate    *string    `json:"promise_date"`
	PromiseAmount  *int64     `json:"promise_amount"`
	PromiseStatus  *string    `json:"promise_status"`
	FollowUpOn     *string    `json:"follow_up_on"`
	Priority       string     `json:"priority"` // high | medium | low
	Reasons        []string   `json:"reasons"`
	ContactPhone   *string    `json:"contact_phone"`
}

func (s *Service) CollectionWorklist(ctx context.Context, propertyID *uuid.UUID) ([]WorkItem, error) {
	p := authctx.Must(ctx)
	out := []WorkItem{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var args []any
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		sw, err := scopeInvoices(p, "billing.collections.view", propertyID, add)
		if err != nil {
			return err
		}
		d := daysPastDueSQL("i")
		o := "(i.total_amount - i.paid_amount - i.credited_amount)"
		rows, err := tx.Query(ctx, `WITH party AS (
			SELECT i.property_id, i.tenant_id, CASE WHEN i.tenant_id IS NULL THEN i.unit_location_id END AS unit_id, min(pl.name) AS property_name,
			  min(COALESCE(t.name, 'Unit ' || un.unit_number, l.name, '')) AS label, min(t.contact_phone) AS phone,
			  sum(`+o+`) AS outstanding, COALESCE(sum(`+o+`) FILTER (WHERE i.status = 'overdue'),0) AS overdue_amt, count(*) FILTER (WHERE i.status = 'overdue') AS overdue_n,
			  GREATEST(COALESCE(max(`+d+`) FILTER (WHERE i.status = 'overdue'),0),0) AS oldest
			FROM invoices i JOIN properties pr ON pr.location_id = i.property_id JOIN locations pl ON pl.id = i.property_id LEFT JOIN tenants t ON t.id = i.tenant_id
			LEFT JOIN locations l ON l.id = i.unit_location_id LEFT JOIN units un ON un.location_id = i.unit_location_id
			WHERE i.status IN ('issued','partially_paid','overdue') AND `+o+` > 0`+sw+`
			GROUP BY i.property_id, i.tenant_id, CASE WHEN i.tenant_id IS NULL THEN i.unit_location_id END)
			SELECT x.property_id, x.property_name, x.tenant_id, x.unit_id, x.label, x.phone, x.outstanding, x.overdue_amt, x.overdue_n, x.oldest,
			  c.created_at, c.outcome, to_char(pm.promise_date,'YYYY-MM-DD'), pm.promise_amount, pm.promise_status, to_char(fu.follow_up_on,'YYYY-MM-DD'),
			  (pm.promise_date <= (now() AT TIME ZONE pr.timezone)::date), (fu.follow_up_on <= (now() AT TIME ZONE pr.timezone)::date),
			  EXISTS (SELECT 1 FROM collection_logs b WHERE b.property_id = x.property_id AND b.promise_status = 'broken' AND b.updated_at > now() - interval '30 days'
			    AND ((x.tenant_id IS NOT NULL AND b.tenant_id = x.tenant_id) OR (x.tenant_id IS NULL AND b.unit_location_id = x.unit_id)))
			FROM party x JOIN properties pr ON pr.location_id = x.property_id
			LEFT JOIN LATERAL (SELECT c.created_at, c.outcome FROM collection_logs c WHERE c.property_id = x.property_id
			  AND ((x.tenant_id IS NOT NULL AND c.tenant_id = x.tenant_id) OR (x.tenant_id IS NULL AND c.unit_location_id = x.unit_id)) ORDER BY c.created_at DESC LIMIT 1) c ON true
			LEFT JOIN LATERAL (SELECT c.promise_date, c.promise_amount, c.promise_status FROM collection_logs c WHERE c.property_id = x.property_id AND c.promise_status = 'open'
			  AND ((x.tenant_id IS NOT NULL AND c.tenant_id = x.tenant_id) OR (x.tenant_id IS NULL AND c.unit_location_id = x.unit_id)) ORDER BY c.promise_date LIMIT 1) pm ON true
			LEFT JOIN LATERAL (SELECT c.follow_up_on FROM collection_logs c WHERE c.property_id = x.property_id AND c.follow_up_on IS NOT NULL
			  AND ((x.tenant_id IS NOT NULL AND c.tenant_id = x.tenant_id) OR (x.tenant_id IS NULL AND c.unit_location_id = x.unit_id)) ORDER BY c.created_at DESC LIMIT 1) fu ON true
			WHERE x.overdue_n > 0 OR pm.promise_date IS NOT NULL
			LIMIT 500`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var w WorkItem
			var promiseDue, followDue *bool
			var broken bool
			if err := rows.Scan(&w.PropertyID, &w.PropertyName, &w.TenantID, &w.UnitLocationID, &w.Label, &w.ContactPhone, &w.Outstanding, &w.OverdueAmount, &w.OverdueCount, &w.OldestDays,
				&w.LastContactAt, &w.LastOutcome, &w.PromiseDate, &w.PromiseAmount, &w.PromiseStatus, &w.FollowUpOn, &promiseDue, &followDue, &broken); err != nil {
				return err
			}
			w.Kind = "tenant"
			if w.TenantID == nil {
				w.Kind = "unit"
			}
			w.Reasons = []string{}
			score := 0
			if w.OldestDays > 60 {
				score += 3
				w.Reasons = append(w.Reasons, fmt.Sprintf("Tunggakan tertua %d hari", w.OldestDays))
			} else if w.OldestDays > 30 {
				score += 2
				w.Reasons = append(w.Reasons, fmt.Sprintf("Tunggakan tertua %d hari", w.OldestDays))
			} else if w.OverdueCount > 0 {
				score++
			}
			if broken {
				score += 3
				w.Reasons = append(w.Reasons, "Janji bayar ingkar (30 hari terakhir)")
			}
			if promiseDue != nil && *promiseDue {
				score += 2
				w.Reasons = append(w.Reasons, "Janji bayar jatuh tempo "+*w.PromiseDate)
			}
			if followDue != nil && *followDue {
				score++
				w.Reasons = append(w.Reasons, "Tindak lanjut "+*w.FollowUpOn)
			}
			if w.LastContactAt == nil && w.OverdueCount > 0 {
				score++
				w.Reasons = append(w.Reasons, "Belum pernah dihubungi")
			}
			switch {
			case score >= 3:
				w.Priority = "high"
			case score >= 2:
				w.Priority = "medium"
			default:
				w.Priority = "low"
			}
			out = append(out, w)
		}
		return rows.Err()
	})
	rank := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Priority] != rank[out[j].Priority] {
			return rank[out[i].Priority] < rank[out[j].Priority]
		}
		if out[i].OldestDays != out[j].OldestDays {
			return out[i].OldestDays > out[j].OldestDays
		}
		return out[i].OverdueAmount > out[j].OverdueAmount
	})
	return out, err
}

var _ = strings.TrimSpace
