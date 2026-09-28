package billing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/tenantscope"
)

// ---------- Ledger dana (PRD P4 v2.1 §5.4 Sinking Fund D-P4-06, §5.5 Deposit, §6.1 saldo kredit tenant) ----------
// Posting dari pembayaran bersifat kumulatif & dapat dihitung ulang: porsi komponen = total komponen × (dibayar ÷ total invoice),
// dibulatkan ke bawah; invoice lunas → porsi penuh. Selisih dengan yang sudah diposting dicatat sebagai entry baru (receipt /
// reversal), sehingga refund & credit note otomatis mengoreksi saldo (P4-NFR-03 audit, tanpa ubah entry lama).

// postFundsTx: posting sinking fund & deposit untuk satu invoice (dipanggil setelah paid_amount berubah).
func (s *Service) postFundsTx(ctx context.Context, tx pgx.Tx, invoiceID uuid.UUID, paymentID *uuid.UUID) error {
	p := authctx.Must(ctx)
	var propertyID uuid.UUID
	var total, paid int64
	var status, source, itype string
	var number *string
	var tenantID, unitID *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT property_id, total_amount, paid_amount, status, source, invoice_type, invoice_number, tenant_id, unit_location_id FROM invoices WHERE id = $1`, invoiceID).
		Scan(&propertyID, &total, &paid, &status, &source, &itype, &number, &tenantID, &unitID); err != nil {
		return err
	}
	if total <= 0 {
		return nil
	}
	share := func(component int64) int64 {
		if component <= 0 {
			return 0
		}
		if paid >= total {
			return component
		}
		return component * paid / total
	}
	label := ""
	if number != nil {
		label = *number
	}
	// Sinking Fund: item charge_type sinking_fund (termasuk pajaknya, umumnya 0)
	var sf int64
	_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount + tax_amount),0) FROM invoice_items WHERE invoice_id = $1 AND COALESCE(charge_type, $2) = 'sinking_fund'`, invoiceID, itype).Scan(&sf)
	if sf > 0 {
		var posted int64
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM sinking_fund_entries WHERE invoice_id = $1 AND entry_type IN ('receipt','reversal')`, invoiceID).Scan(&posted)
		if delta := share(sf) - posted; delta != 0 {
			et, desc := "receipt", "Penerimaan sinking fund "+label
			if delta < 0 {
				et, desc = "reversal", "Koreksi sinking fund "+label
			}
			if _, err := tx.Exec(ctx, `INSERT INTO sinking_fund_entries (organization_id, property_id, entry_type, amount, entry_date, description, invoice_id, payment_id, created_by)
				VALUES ($1,$2,$3,$4,current_date,$5,$6,$7,$8)`, p.OrganizationID, propertyID, et, delta, desc, invoiceID, paymentID, actorOrNil(p)); err != nil {
				return err
			}
		}
	}
	// Deposit: item charge_type deposit (booking fee penjualan unit bukan deposit jaminan → dikecualikan)
	if source != "unit_sale" && (tenantID != nil || unitID != nil) {
		var dep int64
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount + tax_amount),0) FROM invoice_items WHERE invoice_id = $1 AND COALESCE(charge_type, $2) = 'deposit'`, invoiceID, itype).Scan(&dep)
		if dep > 0 {
			var posted int64
			_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM deposit_entries WHERE invoice_id = $1 AND entry_type IN ('received','adjustment') AND reason LIKE 'Deposit diterima%'`, invoiceID).Scan(&posted)
			if delta := share(dep) - posted; delta != 0 {
				et := "received"
				if delta < 0 {
					et = "adjustment"
				}
				if _, err := tx.Exec(ctx, `INSERT INTO deposit_entries (organization_id, property_id, tenant_id, unit_location_id, entry_type, amount, entry_date, reason, invoice_id, payment_id, created_by)
					VALUES ($1,$2,$3,$4,$5,$6,current_date,$7,$8,$9,$10)`, p.OrganizationID, propertyID, tenantID, unitID, et, delta, "Deposit diterima "+label, invoiceID, paymentID, actorOrNil(p)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ---------- Sinking Fund (P4-SCF-02..04) ----------

type FundEntry struct {
	ID            uuid.UUID  `json:"id"`
	PropertyID    uuid.UUID  `json:"property_id"`
	EntryType     string     `json:"entry_type"`
	Amount        int64      `json:"amount"`
	EntryDate     string     `json:"entry_date"`
	Description   *string    `json:"description"`
	InvoiceID     *uuid.UUID `json:"invoice_id"`
	InvoiceNumber *string    `json:"invoice_number"`
	PaymentID     *uuid.UUID `json:"payment_id"`
	WorkOrderID   *uuid.UUID `json:"work_order_id"`
	WorkOrderNo   *string    `json:"work_order_number"`
	Reference     *string    `json:"reference"`
	CreatedByName *string    `json:"created_by_name"`
	CreatedAt     time.Time  `json:"created_at"`
}

type SinkingFundSummary struct {
	PropertyID     uuid.UUID   `json:"property_id"`
	Balance        int64       `json:"balance"`
	Opening        int64       `json:"opening"`
	Receipts       int64       `json:"receipts"`
	Usage          int64       `json:"usage"`
	Adjustments    int64       `json:"adjustments"`
	ReceiptsPeriod int64       `json:"receipts_period"`
	UsagePeriod    int64       `json:"usage_period"`
	Billed         int64       `json:"billed"`      // komponen sinking fund yang ditagihkan (invoice terbit)
	Outstanding    int64       `json:"outstanding"` // porsi sinking fund yang belum dibayar
	Entries        []FundEntry `json:"entries"`
}

func (s *Service) SinkingFund(ctx context.Context, propertyID uuid.UUID, from, to *time.Time) (*SinkingFundSummary, error) {
	p := authctx.Must(ctx)
	if !p.HasAnyOnProperty("billing.sinking_fund.view", propertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.sinking_fund.view")
	}
	out := &SinkingFundSummary{PropertyID: propertyID, Entries: []FundEntry{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0), COALESCE(sum(amount) FILTER (WHERE entry_type = 'opening'),0),
			COALESCE(sum(amount) FILTER (WHERE entry_type IN ('receipt','reversal')),0), COALESCE(-sum(amount) FILTER (WHERE entry_type = 'usage'),0),
			COALESCE(sum(amount) FILTER (WHERE entry_type = 'adjustment'),0),
			COALESCE(sum(amount) FILTER (WHERE entry_type IN ('receipt','reversal') AND ($2::date IS NULL OR entry_date >= $2) AND ($3::date IS NULL OR entry_date <= $3)),0),
			COALESCE(-sum(amount) FILTER (WHERE entry_type = 'usage' AND ($2::date IS NULL OR entry_date >= $2) AND ($3::date IS NULL OR entry_date <= $3)),0)
			FROM sinking_fund_entries WHERE property_id = $1`, propertyID, from, to).Scan(&out.Balance, &out.Opening, &out.Receipts, &out.Usage, &out.Adjustments, &out.ReceiptsPeriod, &out.UsagePeriod); err != nil {
			return err
		}
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(it.amount + it.tax_amount),0) FROM invoice_items it JOIN invoices i ON i.id = it.invoice_id
			WHERE i.property_id = $1 AND i.status NOT IN ('draft','cancelled') AND COALESCE(it.charge_type, i.invoice_type) = 'sinking_fund'`, propertyID).Scan(&out.Billed)
		out.Outstanding = out.Billed - out.Receipts
		if out.Outstanding < 0 {
			out.Outstanding = 0
		}
		rows, err := tx.Query(ctx, `SELECT e.id, e.property_id, e.entry_type, e.amount, to_char(e.entry_date,'YYYY-MM-DD'), e.description, e.invoice_id, i.invoice_number, e.payment_id, e.work_order_id, w.work_order_number, e.reference, u.full_name, e.created_at
			FROM sinking_fund_entries e LEFT JOIN invoices i ON i.id = e.invoice_id LEFT JOIN work_orders w ON w.id = e.work_order_id LEFT JOIN users u ON u.id = e.created_by
			WHERE e.property_id = $1 AND ($2::date IS NULL OR e.entry_date >= $2) AND ($3::date IS NULL OR e.entry_date <= $3) ORDER BY e.entry_date DESC, e.created_at DESC LIMIT 500`, propertyID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e FundEntry
			if err := rows.Scan(&e.ID, &e.PropertyID, &e.EntryType, &e.Amount, &e.EntryDate, &e.Description, &e.InvoiceID, &e.InvoiceNumber, &e.PaymentID, &e.WorkOrderID, &e.WorkOrderNo, &e.Reference, &e.CreatedByName, &e.CreatedAt); err != nil {
				return err
			}
			out.Entries = append(out.Entries, e)
		}
		return rows.Err()
	})
	return out, err
}

type FundEntryInput struct {
	PropertyID  uuid.UUID  `json:"property_id"`
	EntryType   string     `json:"entry_type"` // opening | usage | adjustment
	Amount      int64      `json:"amount"`     // positif; usage dicatat negatif; adjustment boleh negatif
	EntryDate   *string    `json:"entry_date"`
	Description string     `json:"description"`
	WorkOrderID *uuid.UUID `json:"work_order_id"` // P4-SCF-04: penggunaan tertaut WO sebagai bukti
	Reference   *string    `json:"reference"`
}

func (s *Service) AddSinkingFundEntry(ctx context.Context, in FundEntryInput) (*FundEntry, error) {
	p := authctx.Must(ctx)
	if !p.HasOnProperty("billing.sinking_fund.manage", in.PropertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.sinking_fund.manage")
	}
	if in.EntryType != "opening" && in.EntryType != "usage" && in.EntryType != "adjustment" {
		return nil, apperr.Validation("entry_type harus opening|usage|adjustment").WithField("entry_type", "tidak valid")
	}
	if in.Amount == 0 || (in.EntryType != "adjustment" && in.Amount < 0) {
		return nil, apperr.Validation("amount harus > 0").WithField("amount", "harus > 0")
	}
	if strings.TrimSpace(in.Description) == "" {
		return nil, apperr.Validation("description wajib").WithField("description", "wajib")
	}
	amount := in.Amount
	if in.EntryType == "usage" {
		amount = -in.Amount
	}
	d, err := parseDate(in.EntryDate)
	if err != nil {
		return nil, err
	}
	var out *FundEntry
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if in.WorkOrderID != nil {
			var wp uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT property_id FROM work_orders WHERE id = $1`, *in.WorkOrderID).Scan(&wp); err != nil || wp != in.PropertyID {
				return apperr.Validation("work_order_id tidak ditemukan di property ini").WithField("work_order_id", "tidak valid")
			}
		}
		if in.EntryType == "usage" {
			var bal int64
			_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM sinking_fund_entries WHERE property_id = $1`, in.PropertyID).Scan(&bal)
			if bal+amount < 0 {
				return apperr.Conflict("SINKING_FUND_INSUFFICIENT", fmt.Sprintf("Saldo sinking fund tidak cukup (saldo %d)", bal))
			}
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO sinking_fund_entries (organization_id, property_id, entry_type, amount, entry_date, description, work_order_id, reference, created_by)
			VALUES ($1,$2,$3,$4,COALESCE($5, current_date),$6,$7,NULLIF($8,''),$9) RETURNING id`, p.OrganizationID, in.PropertyID, in.EntryType, amount, d, strings.TrimSpace(in.Description), in.WorkOrderID, deref(in.Reference), p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "sinking_fund_entry", EntityID: &id, EntityLabel: in.EntryType, After: in})
		var e FundEntry
		if err := tx.QueryRow(ctx, `SELECT e.id, e.property_id, e.entry_type, e.amount, to_char(e.entry_date,'YYYY-MM-DD'), e.description, e.invoice_id, NULL::text, e.payment_id, e.work_order_id, w.work_order_number, e.reference, u.full_name, e.created_at
			FROM sinking_fund_entries e LEFT JOIN work_orders w ON w.id = e.work_order_id LEFT JOIN users u ON u.id = e.created_by WHERE e.id = $1`, id).
			Scan(&e.ID, &e.PropertyID, &e.EntryType, &e.Amount, &e.EntryDate, &e.Description, &e.InvoiceID, &e.InvoiceNumber, &e.PaymentID, &e.WorkOrderID, &e.WorkOrderNo, &e.Reference, &e.CreatedByName, &e.CreatedAt); err != nil {
			return err
		}
		out = &e
		return nil
	})
	return out, err
}

// ---------- Deposit & saldo kredit per tenant/unit (P4-PND-03..04, P4-PAY-04..05) ----------

type PartyBalance struct {
	TenantID    *uuid.UUID `json:"tenant_id"`
	TenantName  *string    `json:"tenant_name"`
	UnitID      *uuid.UUID `json:"unit_location_id"`
	UnitLabel   *string    `json:"unit_label"`
	Balance     int64      `json:"balance"`
	LastEntryAt *time.Time `json:"last_entry_at"`
}

type LedgerEntry struct {
	ID            uuid.UUID  `json:"id"`
	EntryType     string     `json:"entry_type"`
	Amount        int64      `json:"amount"`
	EntryDate     string     `json:"entry_date"`
	Description   *string    `json:"description"`
	TenantID      *uuid.UUID `json:"tenant_id"`
	UnitID        *uuid.UUID `json:"unit_location_id"`
	InvoiceID     *uuid.UUID `json:"invoice_id"`
	InvoiceNumber *string    `json:"invoice_number"`
	PaymentID     *uuid.UUID `json:"payment_id"`
	WorkOrderID   *uuid.UUID `json:"work_order_id,omitempty"`
	CreatedByName *string    `json:"created_by_name"`
	CreatedAt     time.Time  `json:"created_at"`
}

var ledgerTables = map[string]struct{ table, descCol, viewPerm, managePerm string }{
	"deposit": {"deposit_entries", "reason", "billing.deposits.view", "billing.deposits.manage"},
	"credit":  {"tenant_credit_entries", "description", "billing.payments.view", "billing.payments.allocate"},
}

// Balances: saldo deposit / kredit per tenant+unit pada property (hanya yang ≠ 0).
func (s *Service) Balances(ctx context.Context, kind string, propertyID uuid.UUID, tenantID *uuid.UUID) ([]PartyBalance, error) {
	lt, ok := ledgerTables[kind]
	if !ok {
		return nil, apperr.NotFound("Ledger")
	}
	p := authctx.Must(ctx)
	if !p.HasAnyOnProperty(lt.viewPerm, propertyID) {
		return nil, apperr.Forbidden("Memerlukan " + lt.viewPerm)
	}
	out := []PartyBalance{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// pihak = tenant (seluruh unit-nya, sama dengan partyBalanceTx); unit hanya untuk entry tanpa tenant
		rows, err := tx.Query(ctx, `SELECT e.tenant_id, min(t.name), CASE WHEN e.tenant_id IS NULL THEN e.unit_location_id END,
			min(CASE WHEN e.tenant_id IS NULL THEN COALESCE('Unit ' || un.unit_number, l.name) END), sum(e.amount), max(e.created_at)
			FROM `+lt.table+` e LEFT JOIN tenants t ON t.id = e.tenant_id LEFT JOIN locations l ON l.id = e.unit_location_id LEFT JOIN units un ON un.location_id = e.unit_location_id
			WHERE e.property_id = $1 AND ($2::uuid IS NULL OR e.tenant_id = $2)
			GROUP BY e.tenant_id, CASE WHEN e.tenant_id IS NULL THEN e.unit_location_id END HAVING sum(e.amount) <> 0 ORDER BY sum(e.amount) DESC`, propertyID, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b PartyBalance
			if err := rows.Scan(&b.TenantID, &b.TenantName, &b.UnitID, &b.UnitLabel, &b.Balance, &b.LastEntryAt); err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

func ledgerEntriesTx(ctx context.Context, tx pgx.Tx, kind string, propertyID uuid.UUID, tenantID, unitID *uuid.UUID) ([]LedgerEntry, error) {
	lt := ledgerTables[kind]
	woCol := "NULL::uuid"
	if kind == "deposit" {
		woCol = "e.work_order_id"
	}
	rows, err := tx.Query(ctx, `SELECT e.id, e.entry_type, e.amount, to_char(e.entry_date,'YYYY-MM-DD'), e.`+lt.descCol+`, e.tenant_id, e.unit_location_id, e.invoice_id, i.invoice_number, e.payment_id, `+woCol+`, u.full_name, e.created_at
		FROM `+lt.table+` e LEFT JOIN invoices i ON i.id = e.invoice_id LEFT JOIN users u ON u.id = e.created_by
		WHERE e.property_id = $1 AND ($2::uuid IS NULL OR e.tenant_id = $2) AND ($3::uuid IS NULL OR e.unit_location_id = $3) ORDER BY e.created_at DESC LIMIT 500`, propertyID, tenantID, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LedgerEntry{}
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.EntryType, &e.Amount, &e.EntryDate, &e.Description, &e.TenantID, &e.UnitID, &e.InvoiceID, &e.InvoiceNumber, &e.PaymentID, &e.WorkOrderID, &e.CreatedByName, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Service) LedgerEntries(ctx context.Context, kind string, propertyID uuid.UUID, tenantID, unitID *uuid.UUID) ([]LedgerEntry, error) {
	lt, ok := ledgerTables[kind]
	if !ok {
		return nil, apperr.NotFound("Ledger")
	}
	if !authctx.Must(ctx).HasAnyOnProperty(lt.viewPerm, propertyID) {
		return nil, apperr.Forbidden("Memerlukan " + lt.viewPerm)
	}
	var out []LedgerEntry
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = ledgerEntriesTx(ctx, tx, kind, propertyID, tenantID, unitID)
		return err
	})
	return out, err
}

func partyBalanceTx(ctx context.Context, tx pgx.Tx, kind string, tenantID, unitID *uuid.UUID) int64 {
	lt := ledgerTables[kind]
	var bal int64
	// saldo tenant: seluruh entry tenant; tanpa tenant: per unit
	if tenantID != nil {
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM `+lt.table+` WHERE tenant_id = $1`, *tenantID).Scan(&bal)
	} else if unitID != nil {
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM `+lt.table+` WHERE tenant_id IS NULL AND unit_location_id = $1`, *unitID).Scan(&bal)
	}
	return bal
}

type DepositEntryInput struct {
	PropertyID  uuid.UUID  `json:"property_id"`
	TenantID    *uuid.UUID `json:"tenant_id"`
	UnitID      *uuid.UUID `json:"unit_location_id"`
	EntryType   string     `json:"entry_type"` // received | deducted | refunded | adjustment
	Amount      int64      `json:"amount"`     // positif (deducted/refunded dicatat negatif)
	EntryDate   *string    `json:"entry_date"`
	Reason      string     `json:"reason"`
	WorkOrderID *uuid.UUID `json:"work_order_id"` // P4-PND-04: potongan tertaut kerusakan/inspeksi
}

// AddDepositEntry: ledger deposit manual — diterima di luar invoice, dipotong (kerusakan), dikembalikan saat move-out.
func (s *Service) AddDepositEntry(ctx context.Context, in DepositEntryInput) (*LedgerEntry, error) {
	p := authctx.Must(ctx)
	if !p.HasOnProperty("billing.deposits.manage", in.PropertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.deposits.manage")
	}
	valid := map[string]bool{"received": true, "deducted": true, "refunded": true, "adjustment": true}
	if !valid[in.EntryType] {
		return nil, apperr.Validation("entry_type harus received|deducted|refunded|adjustment").WithField("entry_type", "tidak valid")
	}
	if in.Amount == 0 || (in.EntryType != "adjustment" && in.Amount < 0) {
		return nil, apperr.Validation("amount harus > 0").WithField("amount", "harus > 0")
	}
	if strings.TrimSpace(in.Reason) == "" {
		return nil, apperr.Validation("reason wajib").WithField("reason", "wajib")
	}
	amount := in.Amount
	if in.EntryType == "deducted" || in.EntryType == "refunded" {
		amount = -in.Amount
	}
	d, err := parseDate(in.EntryDate)
	if err != nil {
		return nil, err
	}
	var out *LedgerEntry
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.validateParties(ctx, tx, in.PropertyID, in.TenantID, in.UnitID, true); err != nil {
			return err
		}
		if amount < 0 {
			if bal := partyBalanceTx(ctx, tx, "deposit", in.TenantID, in.UnitID); bal+amount < 0 {
				return apperr.Conflict("DEPOSIT_INSUFFICIENT", fmt.Sprintf("Saldo deposit tidak cukup (saldo %d)", bal))
			}
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO deposit_entries (organization_id, property_id, tenant_id, unit_location_id, entry_type, amount, entry_date, reason, work_order_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,COALESCE($7, current_date),$8,$9,$10) RETURNING id`, p.OrganizationID, in.PropertyID, in.TenantID, in.UnitID, in.EntryType, amount, d, strings.TrimSpace(in.Reason), in.WorkOrderID, p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "deposit_entry", EntityID: &id, EntityLabel: in.EntryType, After: in})
		list, err := ledgerEntriesTx(ctx, tx, "deposit", in.PropertyID, in.TenantID, in.UnitID)
		if err != nil {
			return err
		}
		for i := range list {
			if list[i].ID == id {
				out = &list[i]
			}
		}
		return nil
	})
	return out, err
}

// ---------- Tenant App: saldo deposit & kredit (P4-TNT-06) ----------

type TenantBalances struct {
	DepositBalance int64  `json:"deposit_balance"`
	CreditBalance  int64  `json:"credit_balance"`
	Outstanding    int64  `json:"outstanding_amount"`
	CurrencyCode   string `json:"currency_code"`
}

func (s *Service) TenantBalances(ctx context.Context) (*TenantBalances, error) {
	out := &TenantBalances{CurrencyCode: "IDR"}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		units := unitIDs(sc)
		for _, x := range []struct {
			table string
			dst   *int64
		}{{"deposit_entries", &out.DepositBalance}, {"tenant_credit_entries", &out.CreditBalance}} {
			_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM `+x.table+` WHERE ($1::uuid IS NOT NULL AND tenant_id = $1) OR (tenant_id IS NULL AND unit_location_id = ANY($2))`, sc.TenantID, units).Scan(x.dst)
		}
		where, args := tenantInvoiceWhere(sc, 1)
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(i.total_amount - i.paid_amount - i.credited_amount),0) FROM invoices i WHERE `+where+` AND i.status IN ('issued','partially_paid','overdue')`, args...).Scan(&out.Outstanding)
		return nil
	})
	return out, err
}

var _ = httpx.Page{}
