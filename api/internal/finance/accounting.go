package finance

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Pemetaan akun & ekspor siap-jurnal (PRD P4 v2.1 P4-INT-02, D-P4-03) ----------
// Setiap transaksi keuangan BuildingVision (invoice terbit/void, pembayaran, credit note, refund, sisa penerimaan, penggunaan
// sinking fund, mutasi deposit manual) diturunkan menjadi baris jurnal debit/kredit memakai tabel pemetaan kode akun pelanggan
// (default organization, override per property). Format CSV/XLSX umum untuk diimpor ke sistem akuntansi (Accurate, Jurnal, dsb.).

type MappingKey struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	DefaultCode string `json:"default_code"`
	DefaultName string `json:"default_name"`
}

var mappingKeys = func() []MappingKey {
	keys := []MappingKey{
		{"receivable", "Piutang usaha", "1-1300", "Piutang Usaha"},
		{"cash:transfer", "Kas & bank — transfer", "1-1102", "Bank"},
		{"cash:cash", "Kas — tunai", "1-1101", "Kas"},
		{"cash:va", "Bank — virtual account", "1-1102", "Bank"},
		{"cash:qris", "Bank — QRIS", "1-1102", "Bank"},
		{"cash:card", "Bank — kartu", "1-1102", "Bank"},
		{"cash:ewallet", "E-wallet", "1-1103", "E-wallet"},
		{"cash:check", "Cek/giro", "1-1102", "Bank"},
		{"cash:other", "Kas — lainnya", "1-1101", "Kas"},
		{"tax_output", "PPN keluaran", "2-1300", "PPN Keluaran"},
		{"customer_credit", "Kelebihan bayar / saldo kredit pelanggan", "2-1400", "Uang Muka Pelanggan"},
		{"deposit", "Deposit penyewa", "2-1500", "Deposit Penyewa"},
		{"sinking_fund", "Dana sinking fund", "2-1600", "Dana Sinking Fund"},
	}
	codes := map[string]string{"service_charge": "4-1100", "ipl": "4-1100", "electricity": "4-1200", "water": "4-1300", "utility": "4-1200", "parking": "4-1400",
		"penalty": "4-1500", "rental": "4-1600", "facility": "4-1700", "additional_charge": "4-1800", "other": "4-1900"}
	for _, c := range RevenueCategories {
		if c.Key == "sinking_fund" {
			continue
		}
		keys = append(keys, MappingKey{"revenue:" + c.Key, "Pendapatan " + c.Label, codes[c.Key], "Pendapatan " + c.Label})
	}
	return keys
}()

type Mapping struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	PropertyID  *uuid.UUID `json:"property_id"`
	AccountCode string     `json:"account_code"`
	AccountName string     `json:"account_name"`
	IsDefault   bool       `json:"is_default"` // true = belum dipetakan (nilai bawaan)
	Inherited   bool       `json:"inherited"`  // true = dari pemetaan organization
}

// mappingsTx: kunci → akun (override property → organization → bawaan).
func mappingsTx(ctx context.Context, tx pgx.Tx, propertyID *uuid.UUID) (map[string]Mapping, error) {
	out := map[string]Mapping{}
	for _, k := range mappingKeys {
		out[k.Key] = Mapping{Key: k.Key, Label: k.Label, AccountCode: k.DefaultCode, AccountName: k.DefaultName, IsDefault: true}
	}
	rows, err := tx.Query(ctx, `SELECT mapping_key, property_id, account_code, COALESCE(account_name,'') FROM account_mappings WHERE property_id IS NULL OR property_id = $1 ORDER BY property_id NULLS FIRST`, propertyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, code, name string
		var pid *uuid.UUID
		if err := rows.Scan(&key, &pid, &code, &name); err != nil {
			return nil, err
		}
		m, ok := out[key]
		if !ok {
			continue
		}
		m.AccountCode, m.AccountName, m.IsDefault, m.PropertyID = code, name, false, pid
		m.Inherited = propertyID != nil && pid == nil
		out[key] = m
	}
	return out, rows.Err()
}

func (s *Service) ListMappings(ctx context.Context, propertyID *uuid.UUID) ([]Mapping, error) {
	p := authctx.Must(ctx)
	if propertyID != nil && !p.HasAnyOnProperty("billing.accounting.view", *propertyID) {
		return nil, apperr.Forbidden("")
	}
	var out []Mapping
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		m, err := mappingsTx(ctx, tx, propertyID)
		if err != nil {
			return err
		}
		for _, k := range mappingKeys {
			out = append(out, m[k.Key])
		}
		return nil
	})
	return out, err
}

type MappingInput struct {
	Key         string `json:"key"`
	AccountCode string `json:"account_code"` // kosong = hapus pemetaan (kembali ke bawaan/organization)
	AccountName string `json:"account_name"`
}

func (s *Service) PutMappings(ctx context.Context, propertyID *uuid.UUID, in []MappingInput) ([]Mapping, error) {
	p := authctx.Must(ctx)
	if propertyID != nil {
		if !p.HasOnProperty("billing.accounting.manage", *propertyID) {
			return nil, apperr.Forbidden("Memerlukan billing.accounting.manage")
		}
	} else if _, all := p.PropertyIDsFor("billing.accounting.manage"); !all {
		return nil, apperr.Forbidden("Pemetaan organization memerlukan billing.accounting.manage tingkat organization")
	}
	valid := map[string]bool{}
	for _, k := range mappingKeys {
		valid[k.Key] = true
	}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for i, m := range in {
			if !valid[m.Key] {
				return apperr.Validation("key "+m.Key+" tidak dikenal").WithField(fmt.Sprintf("[%d].key", i), "tidak valid")
			}
			code := strings.TrimSpace(m.AccountCode)
			if code == "" {
				if _, err := tx.Exec(ctx, `DELETE FROM account_mappings WHERE mapping_key = $1 AND property_id IS NOT DISTINCT FROM $2`, m.Key, propertyID); err != nil {
					return err
				}
				continue
			}
			if len(code) > 40 {
				return apperr.Validation("account_code maks. 40 karakter").WithField(fmt.Sprintf("[%d].account_code", i), "terlalu panjang")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO account_mappings (organization_id, property_id, mapping_key, account_code, account_name, updated_by) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6)
				ON CONFLICT (organization_id, COALESCE(property_id, '00000000-0000-0000-0000-000000000000'::uuid), mapping_key) DO UPDATE SET account_code = EXCLUDED.account_code, account_name = EXCLUDED.account_name, updated_by = EXCLUDED.updated_by`,
				p.OrganizationID, propertyID, m.Key, code, strings.TrimSpace(m.AccountName), p.UserID); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditConfigChange, EntityType: "account_mapping", EntityLabel: "Pemetaan akun", After: in})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.ListMappings(ctx, propertyID)
}

// ---------- Jurnal ----------

type JournalLine struct {
	Date        string `json:"date"`
	JournalNo   string `json:"journal_no"`
	AccountCode string `json:"account_code"`
	AccountName string `json:"account_name"`
	Debit       int64  `json:"debit"`
	Credit      int64  `json:"credit"`
	Description string `json:"description"`
	Reference   string `json:"reference"`
	Property    string `json:"property"`
	Party       string `json:"party"`
	SourceType  string `json:"source_type"`
}

type Journal struct {
	From        string        `json:"from"`
	To          string        `json:"to"`
	Lines       []JournalLine `json:"lines"`
	TotalDebit  int64         `json:"total_debit"`
	TotalCredit int64         `json:"total_credit"`
	Entries     int           `json:"entries"`
	Truncated   bool          `json:"truncated,omitempty"`
}

type jb struct {
	lines []JournalLine
	maps  map[uuid.UUID]map[string]Mapping
	tx    pgx.Tx
	ctx   context.Context
	n     int
}

func (b *jb) acct(pid uuid.UUID, key string) (string, string) {
	m, ok := b.maps[pid]
	if !ok {
		p := pid
		m, _ = mappingsTx(b.ctx, b.tx, &p)
		b.maps[pid] = m
	}
	x, ok := m[key]
	if !ok {
		if strings.HasPrefix(key, "cash:") {
			x = m["cash:other"]
		} else if strings.HasPrefix(key, "revenue:") {
			x = m["revenue:other"]
		}
	}
	return x.AccountCode, x.AccountName
}

// entry: satu jurnal seimbang (debit = kredit); baris nol dibuang.
func (b *jb) entry(pid uuid.UUID, date, no, desc, ref, prop, party, src string, legs ...[3]any) {
	var dsum, csum int64
	for _, l := range legs {
		dsum += l[1].(int64)
		csum += l[2].(int64)
	}
	if dsum == 0 && csum == 0 {
		return
	}
	b.n++
	for _, l := range legs {
		d, c := l[1].(int64), l[2].(int64)
		if d == 0 && c == 0 {
			continue
		}
		code, name := b.acct(pid, l[0].(string))
		b.lines = append(b.lines, JournalLine{Date: date, JournalNo: no, AccountCode: code, AccountName: name, Debit: d, Credit: c, Description: desc, Reference: ref, Property: prop, Party: party, SourceType: src})
	}
}

func leg(key string, debit, credit int64) [3]any { return [3]any{key, debit, credit} }

// JournalTx: turunkan jurnal untuk property (atau semua dalam scope) pada rentang tanggal (zona waktu property).
func (s *Service) JournalTx(ctx context.Context, tx pgx.Tx, pids []uuid.UUID, all bool, from, to time.Time) (*Journal, error) {
	b := &jb{maps: map[uuid.UUID]map[string]Mapping{}, tx: tx, ctx: ctx}
	// argumen sama untuk setiap query: $1 dari, $2 sampai, $3 daftar property (bila dibatasi)
	args := []any{from.Format("2006-01-02"), to.Format("2006-01-02")}
	if !all {
		args = append(args, pids)
	}
	pw := func(col string) string {
		if all {
			return ""
		}
		return " AND " + col + " = ANY($3)"
	}
	inRange := func(expr string) string {
		return " AND (" + expr + ")::date BETWEEN $1::date AND $2::date"
	}
	// 1) invoice terbit: Dr piutang total; Cr pendapatan per charge_type (sinking fund/deposit → kewajiban); Cr PPN
	type invRow struct {
		id, pid                   uuid.UUID
		date, no, prop, party, it string
		total, tax                int64
		ext                       *string
	}
	var invs []invRow
	rows, err := tx.Query(ctx, `SELECT i.id, i.property_id, to_char(i.issued_at AT TIME ZONE pr.timezone, 'YYYY-MM-DD'), i.invoice_number, pl.name, COALESCE(t.name, 'Unit ' || un.unit_number, ''), i.invoice_type,
		i.total_amount, i.tax_amount, i.external_ref
		FROM invoices i JOIN properties pr ON pr.location_id = i.property_id JOIN locations pl ON pl.id = i.property_id LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN units un ON un.location_id = i.unit_location_id
		WHERE i.invoice_number IS NOT NULL AND i.issued_at IS NOT NULL`+inRange("i.issued_at AT TIME ZONE pr.timezone")+pw("i.property_id")+` ORDER BY i.issued_at`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r invRow
		if rows.Scan(&r.id, &r.pid, &r.date, &r.no, &r.prop, &r.party, &r.it, &r.total, &r.tax, &r.ext) == nil {
			invs = append(invs, r)
		}
	}
	rows.Close()
	for _, r := range invs {
		legs := []([3]any){leg("receivable", r.total, 0)}
		irows, err := tx.Query(ctx, `SELECT COALESCE(charge_type, $2), sum(amount)::bigint FROM invoice_items WHERE invoice_id = $1 GROUP BY 1`, r.id, r.it)
		if err != nil {
			return nil, err
		}
		var itemSum int64
		for irows.Next() {
			var ct string
			var amt int64
			if irows.Scan(&ct, &amt) == nil {
				key := "revenue:" + ct
				if ct == "deposit" || ct == "sinking_fund" {
					key = ct
				}
				legs = append(legs, leg(key, 0, amt))
				itemSum += amt
			}
		}
		irows.Close()
		legs = append(legs, leg("tax_output", 0, r.tax))
		if diff := r.total - itemSum - r.tax; diff != 0 { // pembulatan / pajak manual
			legs = append(legs, leg("revenue:other", 0, diff))
		}
		b.entry(r.pid, r.date, r.no, "Invoice "+r.no, derefS(r.ext), r.prop, r.party, "invoice", legs...)
	}
	// 2) void: balik jurnal invoice (tanpa bagian credit note)
	rows, err = tx.Query(ctx, `SELECT i.id, i.property_id, to_char(i.cancelled_at AT TIME ZONE pr.timezone, 'YYYY-MM-DD'), i.invoice_number, pl.name, COALESCE(t.name, 'Unit ' || un.unit_number, ''), i.invoice_type,
		i.total_amount - i.credited_amount, i.tax_amount, i.external_ref
		FROM invoices i JOIN properties pr ON pr.location_id = i.property_id JOIN locations pl ON pl.id = i.property_id LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN units un ON un.location_id = i.unit_location_id
		WHERE i.status = 'cancelled' AND i.invoice_number IS NOT NULL AND i.cancelled_at IS NOT NULL`+inRange("i.cancelled_at AT TIME ZONE pr.timezone")+pw("i.property_id"), args...)
	if err != nil {
		return nil, err
	}
	var voids []invRow
	for rows.Next() {
		var r invRow
		if rows.Scan(&r.id, &r.pid, &r.date, &r.no, &r.prop, &r.party, &r.it, &r.total, &r.tax, &r.ext) == nil {
			voids = append(voids, r)
		}
	}
	rows.Close()
	for _, r := range voids {
		key := "revenue:" + r.it
		if r.it == "deposit" || r.it == "sinking_fund" {
			key = r.it
		}
		b.entry(r.pid, r.date, r.no+"-VOID", "Pembatalan invoice "+r.no, derefS(r.ext), r.prop, r.party, "invoice_void",
			leg(key, r.total-r.tax, 0), leg("tax_output", r.tax, 0), leg("receivable", 0, r.total))
	}
	// 3) pembayaran lunas: Dr kas/bank (atau saldo kredit / deposit); Cr piutang (diterapkan) + Cr saldo kredit (kelebihan)
	rows, err = tx.Query(ctx, `SELECT p.property_id, to_char(p.paid_at AT TIME ZONE pr.timezone, 'YYYY-MM-DD'), COALESCE(p.receipt_number, p.payment_number), p.method, p.amount,
		COALESCE((SELECT sum(ce.amount) FROM tenant_credit_entries ce WHERE ce.payment_id = p.id AND ce.entry_type = 'overpayment'),0), pl.name, COALESCE(t.name, 'Unit ' || un.unit_number, ''),
		COALESCE(i.invoice_number,''), COALESCE(p.external_ref, p.reference, '')
		FROM payments p JOIN invoices i ON i.id = p.invoice_id JOIN properties pr ON pr.location_id = p.property_id JOIN locations pl ON pl.id = p.property_id
		LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN units un ON un.location_id = i.unit_location_id
		WHERE p.status IN ('paid','refunded') AND p.paid_at IS NOT NULL`+inRange("p.paid_at AT TIME ZONE pr.timezone")+pw("p.property_id")+` ORDER BY p.paid_at`, args...)
	if err != nil {
		return nil, err
	}
	type payRow struct {
		pid                   uuid.UUID
		date, no, method      string
		amount, excess        int64
		prop, party, inv, ref string
	}
	var pays []payRow
	for rows.Next() {
		var r payRow
		if rows.Scan(&r.pid, &r.date, &r.no, &r.method, &r.amount, &r.excess, &r.prop, &r.party, &r.inv, &r.ref) == nil {
			pays = append(pays, r)
		}
	}
	rows.Close()
	for _, r := range pays {
		dr := "cash:" + r.method
		switch r.method {
		case "credit":
			dr = "customer_credit"
		case "deposit":
			dr = "deposit"
		}
		b.entry(r.pid, r.date, r.no, "Pembayaran "+r.inv, r.ref, r.prop, r.party, "payment",
			leg(dr, r.amount, 0), leg("receivable", 0, r.amount-r.excess), leg("customer_credit", 0, r.excess))
	}
	// 4) refund: tunai → Dr piutang (diterapkan) + Dr saldo kredit (kelebihan), Cr kas; ke saldo kredit → Dr piutang, Cr saldo kredit
	rows, err = tx.Query(ctx, `SELECT p.property_id, to_char(p.refunded_at AT TIME ZONE pr.timezone, 'YYYY-MM-DD'), p.payment_number, p.method, p.amount,
		COALESCE((SELECT sum(ce.amount) FROM tenant_credit_entries ce WHERE ce.payment_id = p.id AND ce.entry_type = 'overpayment'),0),
		EXISTS (SELECT 1 FROM tenant_credit_entries ce WHERE ce.payment_id = p.id AND ce.entry_type = 'adjustment'), pl.name, COALESCE(t.name, 'Unit ' || un.unit_number, ''), COALESCE(p.refund_reason,'')
		FROM payments p JOIN invoices i ON i.id = p.invoice_id JOIN properties pr ON pr.location_id = p.property_id JOIN locations pl ON pl.id = p.property_id
		LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN units un ON un.location_id = i.unit_location_id
		WHERE p.status = 'refunded' AND p.refunded_at IS NOT NULL AND p.method NOT IN ('credit','deposit')`+inRange("p.refunded_at AT TIME ZONE pr.timezone")+pw("p.property_id"), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid uuid.UUID
		var date, no, method, prop, party, reason string
		var amount, excess int64
		var toCredit bool
		if rows.Scan(&pid, &date, &no, &method, &amount, &excess, &toCredit, &prop, &party, &reason) != nil {
			continue
		}
		if toCredit {
			b.entry(pid, date, no+"-RF", "Refund ke saldo kredit "+no, reason, prop, party, "refund", leg("receivable", amount-excess, 0), leg("customer_credit", 0, amount-excess))
		} else {
			b.entry(pid, date, no+"-RF", "Refund "+no, reason, prop, party, "refund", leg("receivable", amount-excess, 0), leg("customer_credit", excess, 0), leg("cash:"+method, 0, amount))
		}
	}
	rows.Close()
	// 5) credit note: Dr pendapatan; Cr piutang (+ Cr saldo kredit untuk bagian yang sudah dibayar)
	rows, err = tx.Query(ctx, `SELECT c.property_id, to_char(c.decided_at AT TIME ZONE pr.timezone, 'YYYY-MM-DD'), c.credit_note_number, i.invoice_type, c.amount,
		COALESCE((SELECT sum(ce.amount) FROM tenant_credit_entries ce WHERE ce.credit_note_id = c.id),0), pl.name, COALESCE(t.name, 'Unit ' || un.unit_number, ''), COALESCE(i.invoice_number,''), c.reason
		FROM credit_notes c JOIN invoices i ON i.id = c.invoice_id JOIN properties pr ON pr.location_id = c.property_id JOIN locations pl ON pl.id = c.property_id
		LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN units un ON un.location_id = i.unit_location_id
		WHERE c.status = 'approved' AND c.decided_at IS NOT NULL`+inRange("c.decided_at AT TIME ZONE pr.timezone")+pw("c.property_id"), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid uuid.UUID
		var date, no, it, prop, party, inv, reason string
		var amount, overflow int64
		if rows.Scan(&pid, &date, &no, &it, &amount, &overflow, &prop, &party, &inv, &reason) != nil {
			continue
		}
		key := "revenue:" + it
		if it == "deposit" || it == "sinking_fund" {
			key = it
		}
		b.entry(pid, date, no, "Credit note "+inv+" — "+reason, inv, prop, party, "credit_note", leg(key, amount, 0), leg("receivable", 0, amount-overflow), leg("customer_credit", 0, overflow))
	}
	rows.Close()
	// 6) sisa penerimaan tanpa invoice (Receive) → Dr kas, Cr saldo kredit; saldo kredit dikembalikan manual → kebalikannya
	rows, err = tx.Query(ctx, `SELECT ce.property_id, to_char(ce.created_at AT TIME ZONE pr.timezone, 'YYYY-MM-DD'), ce.entry_type, ce.amount, COALESCE(ce.description,''), pl.name, COALESCE(t.name, 'Unit ' || un.unit_number, '')
		FROM tenant_credit_entries ce JOIN properties pr ON pr.location_id = ce.property_id JOIN locations pl ON pl.id = ce.property_id LEFT JOIN tenants t ON t.id = ce.tenant_id LEFT JOIN units un ON un.location_id = ce.unit_location_id
		WHERE ce.payment_id IS NULL AND ce.credit_note_id IS NULL AND ce.entry_type IN ('overpayment','refunded','adjustment')`+inRange("ce.created_at AT TIME ZONE pr.timezone")+pw("ce.property_id"), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid uuid.UUID
		var date, et, desc, prop, party string
		var amount int64
		if rows.Scan(&pid, &date, &et, &amount, &desc, &prop, &party) != nil {
			continue
		}
		if amount > 0 {
			b.entry(pid, date, "CR-"+date, desc, "", prop, party, "credit_entry", leg("cash:transfer", amount, 0), leg("customer_credit", 0, amount))
		} else {
			b.entry(pid, date, "CR-"+date, desc, "", prop, party, "credit_entry", leg("customer_credit", -amount, 0), leg("cash:transfer", 0, -amount))
		}
	}
	rows.Close()
	// 7) sinking fund: penggunaan/penyesuaian manual (penerimaan sudah lewat invoice)
	rows, err = tx.Query(ctx, `SELECT sf.property_id, to_char(sf.entry_date, 'YYYY-MM-DD'), sf.entry_type, sf.amount, COALESCE(sf.description,''), COALESCE(sf.reference, w.work_order_number, ''), pl.name
		FROM sinking_fund_entries sf JOIN locations pl ON pl.id = sf.property_id LEFT JOIN work_orders w ON w.id = sf.work_order_id
		WHERE sf.entry_type IN ('usage','adjustment','opening') AND sf.entry_date BETWEEN $1::date AND $2::date`+pw("sf.property_id"), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid uuid.UUID
		var date, et, desc, ref, prop string
		var amount int64
		if rows.Scan(&pid, &date, &et, &amount, &desc, &ref, &prop) != nil {
			continue
		}
		if amount < 0 {
			b.entry(pid, date, "SF-"+date, desc, ref, prop, "", "sinking_fund", leg("sinking_fund", -amount, 0), leg("cash:transfer", 0, -amount))
		} else {
			b.entry(pid, date, "SF-"+date, desc, ref, prop, "", "sinking_fund", leg("cash:transfer", amount, 0), leg("sinking_fund", 0, amount))
		}
	}
	rows.Close()
	// 8) deposit manual: diterima di luar invoice / dikembalikan / dipotong (kerusakan → pendapatan lain)
	rows, err = tx.Query(ctx, `SELECT d.property_id, to_char(d.entry_date, 'YYYY-MM-DD'), d.entry_type, d.amount, COALESCE(d.reason,''), pl.name, COALESCE(t.name, 'Unit ' || un.unit_number, '')
		FROM deposit_entries d JOIN locations pl ON pl.id = d.property_id LEFT JOIN tenants t ON t.id = d.tenant_id LEFT JOIN units un ON un.location_id = d.unit_location_id
		WHERE d.invoice_id IS NULL AND d.payment_id IS NULL AND d.entry_date BETWEEN $1::date AND $2::date`+pw("d.property_id"), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid uuid.UUID
		var date, et, reason, prop, party string
		var amount int64
		if rows.Scan(&pid, &date, &et, &amount, &reason, &prop, &party) != nil {
			continue
		}
		switch {
		case et == "deducted":
			b.entry(pid, date, "DP-"+date, "Potongan deposit — "+reason, "", prop, party, "deposit", leg("deposit", -amount, 0), leg("revenue:other", 0, -amount))
		case amount < 0:
			b.entry(pid, date, "DP-"+date, "Pengembalian deposit — "+reason, "", prop, party, "deposit", leg("deposit", -amount, 0), leg("cash:transfer", 0, -amount))
		default:
			b.entry(pid, date, "DP-"+date, "Deposit diterima — "+reason, "", prop, party, "deposit", leg("cash:transfer", amount, 0), leg("deposit", 0, amount))
		}
	}
	rows.Close()
	sort.SliceStable(b.lines, func(i, j int) bool {
		if b.lines[i].Date != b.lines[j].Date {
			return b.lines[i].Date < b.lines[j].Date
		}
		return b.lines[i].JournalNo < b.lines[j].JournalNo
	})
	j := &Journal{From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Lines: b.lines, Entries: b.n}
	for _, l := range b.lines {
		j.TotalDebit += l.Debit
		j.TotalCredit += l.Credit
	}
	if j.Lines == nil {
		j.Lines = []JournalLine{}
	}
	return j, nil
}

func derefS(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Service) journalRange(ctx context.Context, tx pgx.Tx, propertyID *uuid.UUID, fromS, toS string) (time.Time, time.Time, error) {
	loc := time.UTC
	if propertyID != nil {
		loc = property.PropertyTimezone(ctx, tx, *propertyID)
	}
	now := time.Now().In(loc)
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, -1)
	var err error
	if fromS != "" {
		if from, err = time.Parse("2006-01-02", fromS); err != nil {
			return from, to, apperr.Validation("from harus YYYY-MM-DD").WithField("from", "format")
		}
	}
	if toS != "" {
		if to, err = time.Parse("2006-01-02", toS); err != nil {
			return from, to, apperr.Validation("to harus YYYY-MM-DD").WithField("to", "format")
		}
	}
	if to.Before(from) || to.Sub(from) > 370*24*time.Hour {
		return from, to, apperr.Validation("rentang tanggal tidak valid (maks. 1 tahun)")
	}
	return from, to, nil
}

// Journal: pratinjau (maks. 500 baris) untuk layar ekspor.
func (s *Service) Journal(ctx context.Context, propertyID *uuid.UUID, from, to string) (*Journal, error) {
	var out *Journal
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pids, all, err := scopeFor(ctx, "billing.accounting.view", propertyID)
		if err != nil {
			return err
		}
		f, t, err := s.journalRange(ctx, tx, propertyID, from, to)
		if err != nil {
			return err
		}
		out, err = s.JournalTx(ctx, tx, pids, all, f, t)
		if err == nil && len(out.Lines) > 500 {
			out.Lines, out.Truncated = out.Lines[:500], true
		}
		return err
	})
	return out, err
}

// ExportJournal: file CSV/XLSX siap impor (billing.accounting.export) — tercatat di audit (P4-NFR-03).
func (s *Service) ExportJournal(ctx context.Context, propertyID *uuid.UUID, from, to, format string) ([]byte, string, string, error) {
	var data []byte
	var name, ctype string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pids, all, err := scopeFor(ctx, "billing.accounting.export", propertyID)
		if err != nil {
			return err
		}
		f, t, err := s.journalRange(ctx, tx, propertyID, from, to)
		if err != nil {
			return err
		}
		j, err := s.JournalTx(ctx, tx, pids, all, f, t)
		if err != nil {
			return err
		}
		head := []string{"Tanggal", "No. Jurnal", "Kode Akun", "Nama Akun", "Debit", "Kredit", "Keterangan", "Referensi", "Property", "Pihak", "Sumber"}
		row := func(l JournalLine) []string {
			return []string{l.Date, l.JournalNo, l.AccountCode, l.AccountName, fmt.Sprint(l.Debit), fmt.Sprint(l.Credit), l.Description, l.Reference, l.Property, l.Party, l.SourceType}
		}
		name = fmt.Sprintf("jurnal-%s-%s", j.From, j.To)
		if format == "xlsx" {
			x := excelize.NewFile()
			sh := "Jurnal"
			x.SetSheetName("Sheet1", sh)
			for c, h := range head {
				cell, _ := excelize.CoordinatesToCellName(c+1, 1)
				_ = x.SetCellValue(sh, cell, h)
			}
			for r, l := range j.Lines {
				vals := row(l)
				for c, v := range vals {
					cell, _ := excelize.CoordinatesToCellName(c+1, r+2)
					if c == 4 || c == 5 {
						_ = x.SetCellValue(sh, cell, []int64{l.Debit, l.Credit}[c-4])
						continue
					}
					_ = x.SetCellValue(sh, cell, v)
				}
			}
			var buf bytes.Buffer
			if err := x.Write(&buf); err != nil {
				return err
			}
			data, name, ctype = buf.Bytes(), name+".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		} else {
			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			_ = w.Write(head)
			for _, l := range j.Lines {
				_ = w.Write(row(l))
			}
			w.Flush()
			data, name, ctype = buf.Bytes(), name+".csv", "text/csv; charset=utf-8"
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditExport, EntityType: "journal_export", EntityLabel: name, After: map[string]any{"property_id": propertyID, "from": j.From, "to": j.To, "lines": len(j.Lines), "entries": j.Entries}})
		return nil
	})
	return data, name, ctype, err
}

// scopeFor: property tunggal (cek izin) atau semua property dalam scope permission.
func scopeFor(ctx context.Context, perm string, propertyID *uuid.UUID) ([]uuid.UUID, bool, error) {
	p := authctx.Must(ctx)
	if propertyID != nil {
		if !p.HasAnyOnProperty(perm, *propertyID) {
			return nil, false, apperr.Forbidden("Memerlukan " + perm)
		}
		return []uuid.UUID{*propertyID}, false, nil
	}
	pids, all := p.PropertyIDsFor(perm)
	return pids, all, nil
}
