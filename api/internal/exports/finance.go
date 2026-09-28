package exports

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/authctx"
)

// ---------- Dataset keuangan (PRD P4 v2.1 P4-INT-01, B-17): invoice, item, pembayaran, credit note, ledger deposit/kredit,
// sinking fund — per periode (from/to, YYYY-MM-DD) & property, mengikuti scope permission billing peminta. ----------

var financeResources = map[string]string{
	"invoices": "billing.invoices.view", "invoice_items": "billing.invoices.view", "payments": "billing.payments.view",
	"credit_notes": "billing.credit_notes.view", "deposit_ledger": "billing.deposits.view", "credit_ledger": "billing.deposits.view",
	"sinking_fund": "billing.sinking_fund.view",
}

func init() {
	for k, v := range financeResources {
		resources[k] = v
	}
}

type financeDataset struct {
	headers  []string
	sql      string
	propCol  string
	dateExpr string // kolom tanggal filter from/to
}

var financeDatasets = map[string]financeDataset{
	"invoices": {
		headers: []string{"No. Invoice", "Status", "Tipe", "Property", "Tenant", "Kode Tenant", "Unit", "Periode Mulai", "Periode Akhir", "Terbit", "Jatuh Tempo", "Subtotal", "Pajak", "Total",
			"Dibayar", "Credit Note", "Sisa", "Sumber", "Ref. Akuntansi", "Keterangan"},
		sql: `SELECT COALESCE(i.invoice_number,'Draft'), i.status, i.invoice_type, pl.name, COALESCE(t.name,''), COALESCE(t.tenant_code,''), COALESCE(un.unit_number,''),
			COALESCE(to_char(i.period_start,'YYYY-MM-DD'),''), COALESCE(to_char(i.period_end,'YYYY-MM-DD'),''), COALESCE(to_char(i.issued_at AT TIME ZONE pr.timezone,'YYYY-MM-DD'),''),
			COALESCE(to_char(i.due_date,'YYYY-MM-DD'), to_char(i.due_at AT TIME ZONE pr.timezone,'YYYY-MM-DD')), i.subtotal_amount, i.tax_amount, i.total_amount, i.paid_amount, i.credited_amount,
			CASE WHEN i.status IN ('issued','partially_paid','overdue') THEN i.total_amount - i.paid_amount - i.credited_amount ELSE 0 END, i.source, COALESCE(i.external_ref,''), COALESCE(i.description,'')
			FROM invoices i JOIN properties pr ON pr.location_id = i.property_id JOIN locations pl ON pl.id = i.property_id LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN units un ON un.location_id = i.unit_location_id
			WHERE true`,
		propCol: "i.property_id", dateExpr: "COALESCE(i.issued_at, i.created_at) AT TIME ZONE pr.timezone",
	},
	"invoice_items": {
		headers: []string{"No. Invoice", "Status Invoice", "Tanggal Terbit", "Property", "Tenant", "Unit", "Uraian", "Komponen", "Qty", "Satuan", "Harga", "Jumlah", "Tarif Pajak", "Pajak", "Sumber"},
		sql: `SELECT COALESCE(i.invoice_number,'Draft'), i.status, COALESCE(to_char(i.issued_at AT TIME ZONE pr.timezone,'YYYY-MM-DD'),''), pl.name, COALESCE(t.name,''), COALESCE(un.unit_number,''),
			it.description, COALESCE(it.charge_type, i.invoice_type), it.quantity::float8, COALESCE(it.unit,''), it.unit_price, it.amount, it.tax_rate::float8, it.tax_amount, COALESCE(it.source_type,'')
			FROM invoice_items it JOIN invoices i ON i.id = it.invoice_id JOIN properties pr ON pr.location_id = i.property_id JOIN locations pl ON pl.id = i.property_id
			LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN units un ON un.location_id = i.unit_location_id WHERE true`,
		propCol: "i.property_id", dateExpr: "COALESCE(i.issued_at, i.created_at) AT TIME ZONE pr.timezone",
	},
	"payments": {
		headers: []string{"No. Pembayaran", "No. Kwitansi", "Grup Penerimaan", "Status", "Metode", "Provider", "Jumlah", "Tanggal Bayar", "No. Invoice", "Property", "Tenant", "Referensi", "Ref. Akuntansi",
			"Diverifikasi", "Refund", "Alasan Refund"},
		sql: `SELECT p.payment_number, COALESCE(p.receipt_number,''), COALESCE(p.receipt_group,''), p.status, p.method, p.provider_code, p.amount, COALESCE(to_char(p.paid_at AT TIME ZONE pr.timezone,'YYYY-MM-DD HH24:MI'),''),
			COALESCE(i.invoice_number,''), pl.name, COALESCE(t.name,''), COALESCE(p.reference,''), COALESCE(p.external_ref,''), COALESCE(to_char(p.verified_at AT TIME ZONE pr.timezone,'YYYY-MM-DD HH24:MI'),''),
			COALESCE(to_char(p.refunded_at AT TIME ZONE pr.timezone,'YYYY-MM-DD'),''), COALESCE(p.refund_reason,'')
			FROM payments p JOIN invoices i ON i.id = p.invoice_id JOIN properties pr ON pr.location_id = p.property_id JOIN locations pl ON pl.id = p.property_id LEFT JOIN tenants t ON t.id = i.tenant_id WHERE true`,
		propCol: "p.property_id", dateExpr: "COALESCE(p.paid_at, p.created_at) AT TIME ZONE pr.timezone",
	},
	"credit_notes": {
		headers: []string{"No. Credit Note", "Status", "No. Invoice", "Property", "Tenant", "Jumlah", "Alasan", "Diajukan", "Diputuskan", "Catatan"},
		sql: `SELECT COALESCE(c.credit_note_number,''), c.status, COALESCE(i.invoice_number,''), pl.name, COALESCE(t.name,''), c.amount, c.reason, to_char(c.requested_at AT TIME ZONE pr.timezone,'YYYY-MM-DD'),
			COALESCE(to_char(c.decided_at AT TIME ZONE pr.timezone,'YYYY-MM-DD'),''), COALESCE(c.decision_note,'')
			FROM credit_notes c JOIN invoices i ON i.id = c.invoice_id JOIN properties pr ON pr.location_id = c.property_id JOIN locations pl ON pl.id = c.property_id LEFT JOIN tenants t ON t.id = i.tenant_id WHERE true`,
		propCol: "c.property_id", dateExpr: "c.requested_at AT TIME ZONE pr.timezone",
	},
	"deposit_ledger": {
		headers: []string{"Tanggal", "Jenis", "Jumlah", "Property", "Tenant", "Unit", "Keterangan", "No. Invoice"},
		sql: `SELECT to_char(d.entry_date,'YYYY-MM-DD'), d.entry_type, d.amount, pl.name, COALESCE(t.name,''), COALESCE(un.unit_number,''), COALESCE(d.reason,''), COALESCE(i.invoice_number,'')
			FROM deposit_entries d JOIN locations pl ON pl.id = d.property_id LEFT JOIN tenants t ON t.id = d.tenant_id LEFT JOIN units un ON un.location_id = d.unit_location_id LEFT JOIN invoices i ON i.id = d.invoice_id WHERE true`,
		propCol: "d.property_id", dateExpr: "d.entry_date",
	},
	"credit_ledger": {
		headers: []string{"Tanggal", "Jenis", "Jumlah", "Property", "Tenant", "Unit", "Keterangan", "No. Invoice"},
		sql: `SELECT to_char(c.entry_date,'YYYY-MM-DD'), c.entry_type, c.amount, pl.name, COALESCE(t.name,''), COALESCE(un.unit_number,''), COALESCE(c.description,''), COALESCE(i.invoice_number,'')
			FROM tenant_credit_entries c JOIN locations pl ON pl.id = c.property_id LEFT JOIN tenants t ON t.id = c.tenant_id LEFT JOIN units un ON un.location_id = c.unit_location_id LEFT JOIN invoices i ON i.id = c.invoice_id WHERE true`,
		propCol: "c.property_id", dateExpr: "c.entry_date",
	},
	"sinking_fund": {
		headers: []string{"Tanggal", "Jenis", "Jumlah", "Property", "Keterangan", "No. Invoice", "Work Order", "Referensi"},
		sql: `SELECT to_char(sf.entry_date,'YYYY-MM-DD'), sf.entry_type, sf.amount, pl.name, COALESCE(sf.description,''), COALESCE(i.invoice_number,''), COALESCE(w.work_order_number,''), COALESCE(sf.reference,'')
			FROM sinking_fund_entries sf JOIN locations pl ON pl.id = sf.property_id LEFT JOIN invoices i ON i.id = sf.invoice_id LEFT JOIN work_orders w ON w.id = sf.work_order_id WHERE true`,
		propCol: "sf.property_id", dateExpr: "sf.entry_date",
	},
}

// collectFinance: dataset keuangan dengan filter property_id, from, to, status (opsional).
func (s *Service) collectFinance(ctx context.Context, resource string, q url.Values) ([]string, [][]any, bool, error) {
	ds, ok := financeDatasets[resource]
	if !ok {
		return nil, nil, false, nil
	}
	p := authctx.Must(ctx)
	perm := financeResources[resource]
	var rows [][]any
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var args []any
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := ""
		if pid := parseUUID(q.Get("property_id")); pid != nil {
			if !p.HasAnyOnProperty(perm, *pid) {
				return nil
			}
			where += " AND " + ds.propCol + " = " + add(*pid)
		} else if pids, all := p.PropertyIDsFor(perm); !all {
			where += " AND " + ds.propCol + " = ANY(" + add(pids) + ")"
		}
		if v := q.Get("from"); v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				where += " AND (" + ds.dateExpr + ")::date >= " + add(t) + "::date"
			}
		}
		if v := q.Get("to"); v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				where += " AND (" + ds.dateExpr + ")::date <= " + add(t) + "::date"
			}
		}
		if v := q.Get("status"); v != "" && (resource == "invoices" || resource == "payments" || resource == "credit_notes") {
			col := map[string]string{"invoices": "i.status", "payments": "p.status", "credit_notes": "c.status"}[resource]
			where += " AND " + col + " = " + add(v)
		}
		rs, err := tx.Query(ctx, ds.sql+where+" ORDER BY 1 LIMIT 50000", args...)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			vals, err := rs.Values()
			if err != nil {
				return err
			}
			rows = append(rows, vals)
		}
		return rs.Err()
	})
	return ds.headers, rows, true, err
}
