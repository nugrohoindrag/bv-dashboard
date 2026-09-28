package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Invoice (PRD P4 v2.1 §5.2) ----------
// D-P4-01 / B-19: nomor INV-{YEAR}-{SEQ6} diberikan saat issue (draft tanpa nomor, tanpa lompatan nomor).
// B-07: jumlah baris = qty × harga (server-side, pembulatan half-up) untuk invoice manual/impor; pajak per item.
// B-13 / P4-OUT-03: jatuh tempo = akhir hari tanggal jatuh tempo di zona waktu property.

// InvoiceTypes: tipe invoice lengkap (P4-INV-02); IPL = label profile Apartment untuk service charge.
var invoiceTypes = map[string]bool{"service_charge": true, "ipl": true, "utility": true, "electricity": true, "water": true, "parking": true, "sinking_fund": true,
	"penalty": true, "deposit": true, "rental": true, "facility": true, "additional_charge": true, "other": true}

// ChargeTypes: komponen tagihan (sama dengan tipe invoice).
func ValidChargeType(t string) bool { return invoiceTypes[t] }

type Item struct {
	ID              uuid.UUID      `json:"id,omitempty"`
	Description     string         `json:"description"`
	Quantity        float64        `json:"quantity"`
	Unit            *string        `json:"unit"`
	UnitPrice       int64          `json:"unit_price"`
	Amount          int64          `json:"amount"`
	ChargeType      *string        `json:"charge_type"`
	TaxRate         *float64       `json:"tax_rate"` // % — nil = default invoice/pengaturan
	TaxAmount       int64          `json:"tax_amount"`
	TaxExempt       bool           `json:"tax_exempt,omitempty"`
	BillingRuleID   *uuid.UUID     `json:"billing_rule_id,omitempty"`
	MeterReadingID  *uuid.UUID     `json:"meter_reading_id,omitempty"`
	ParkingPermitID *uuid.UUID     `json:"parking_permit_id,omitempty"`
	SourceType      *string        `json:"source_type,omitempty"`
	SourceID        *uuid.UUID     `json:"source_id,omitempty"`
	Meta            map[string]any `json:"meta,omitempty"`
}

type Invoice struct {
	ID             uuid.UUID  `json:"id"`
	InvoiceNumber  *string    `json:"invoice_number"` // null = draft (nomor saat issue, D-P4-01)
	DisplayNumber  string     `json:"display_number"` // nomor atau "Draft"
	PropertyID     uuid.UUID  `json:"property_id"`
	TenantID       *uuid.UUID `json:"tenant_id"`
	TenantName     *string    `json:"tenant_name"`
	UnitLocationID *uuid.UUID `json:"unit_location_id"`
	UnitLabel      *string    `json:"unit_label"`
	InvoiceType    string     `json:"invoice_type"`
	PeriodStart    *time.Time `json:"period_start"`
	PeriodEnd      *time.Time `json:"period_end"`
	Description    *string    `json:"description"`
	CurrencyCode   string     `json:"currency_code"`
	SubtotalAmount int64      `json:"subtotal_amount"`
	TaxAmount      int64      `json:"tax_amount"`
	TaxMode        string     `json:"tax_mode"` // manual | computed
	TotalAmount    int64      `json:"total_amount"`
	PaidAmount     int64      `json:"paid_amount"`
	CreditedAmount int64      `json:"credited_amount"`
	OutstandingAmt int64      `json:"outstanding_amount"`
	IssuedAt       *time.Time `json:"issued_at"`
	DueAt          time.Time  `json:"due_at"`
	DueDate        *string    `json:"due_date"`
	DaysOverdue    int        `json:"days_overdue"`
	PaidAt         *time.Time `json:"paid_at"`
	Status         string     `json:"status"`
	Source         string     `json:"source"`
	SourceID       *uuid.UUID `json:"source_id"`
	BillingRunID   *uuid.UUID `json:"billing_run_id"`
	ExternalRef    *string    `json:"external_ref"`
	Notes          *string    `json:"notes"`
	CancelReason   *string    `json:"cancel_reason"`
	Items          []Item     `json:"items"`
	PaymentCount   int        `json:"payment_count"`
	PenaltyAccrued int64      `json:"penalty_accrued"`
	CreatedAt      time.Time  `json:"created_at"`
	CreatedByName  *string    `json:"created_by_name"`
	AllowedActions []string   `json:"allowed_actions"`
	Version        int        `json:"version"`
}

const invSelect = `SELECT i.id, i.invoice_number, i.property_id, i.tenant_id, t.name, i.unit_location_id, COALESCE('Unit ' || u.unit_number, l.name), i.invoice_type, i.period_start, i.period_end, i.description,
	i.currency_code, i.subtotal_amount, i.tax_amount, i.tax_mode, i.total_amount, i.paid_amount, i.credited_amount, i.issued_at, i.due_at, to_char(i.due_date, 'YYYY-MM-DD'), i.paid_at, i.status, i.source, i.source_id, i.billing_run_id,
	i.external_ref, i.notes, i.cancel_reason,
	(SELECT count(*) FROM payments p WHERE p.invoice_id = i.id AND p.status = 'paid'),
	COALESCE((SELECT sum(ip.accrued_amount) FROM invoice_penalties ip WHERE ip.invoice_id = i.id AND ip.status IN ('accruing','final')),0),
	i.created_at, cb.full_name, i.version,
	COALESCE((now() AT TIME ZONE pz.timezone)::date - COALESCE(i.due_date, (i.due_at AT TIME ZONE pz.timezone)::date), 0)
	FROM invoices i LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN locations l ON l.id = i.unit_location_id LEFT JOIN units u ON u.location_id = l.id LEFT JOIN users cb ON cb.id = i.created_by
	LEFT JOIN properties pz ON pz.location_id = i.property_id`

func scanInvoice(row pgx.Row) (*Invoice, error) {
	var v Invoice
	var daysPastDue int32
	if err := row.Scan(&v.ID, &v.InvoiceNumber, &v.PropertyID, &v.TenantID, &v.TenantName, &v.UnitLocationID, &v.UnitLabel, &v.InvoiceType, &v.PeriodStart, &v.PeriodEnd, &v.Description,
		&v.CurrencyCode, &v.SubtotalAmount, &v.TaxAmount, &v.TaxMode, &v.TotalAmount, &v.PaidAmount, &v.CreditedAmount, &v.IssuedAt, &v.DueAt, &v.DueDate, &v.PaidAt, &v.Status, &v.Source, &v.SourceID, &v.BillingRunID,
		&v.ExternalRef, &v.Notes, &v.CancelReason, &v.PaymentCount, &v.PenaltyAccrued, &v.CreatedAt, &v.CreatedByName, &v.Version, &daysPastDue); err != nil {
		return nil, err
	}
	v.CurrencyCode = strings.TrimSpace(v.CurrencyCode)
	v.OutstandingAmt = v.TotalAmount - v.PaidAmount - v.CreditedAmount
	if v.Status == "cancelled" || v.Status == "draft" {
		v.OutstandingAmt = 0
		if v.Status == "draft" {
			v.OutstandingAmt = v.TotalAmount
		}
	}
	v.DisplayNumber = "Draft"
	if v.InvoiceNumber != nil {
		v.DisplayNumber = *v.InvoiceNumber
	}
	// hari kalender di timezone property — konsisten dengan bucket aging & denda
	if v.Status == "overdue" && daysPastDue > 0 {
		v.DaysOverdue = int(daysPastDue)
	}
	v.Items = []Item{}
	return &v, nil
}

func loadItems(ctx context.Context, tx pgx.Tx, inv *Invoice) error {
	rows, err := tx.Query(ctx, `SELECT id, description, quantity::float8, unit, unit_price, amount, charge_type, tax_rate::float8, tax_amount, billing_rule_id, meter_reading_id, parking_permit_id, source_type, source_id, meta
		FROM invoice_items WHERE invoice_id = $1 ORDER BY sort_order`, inv.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var it Item
		var rate float64
		var meta []byte
		if err := rows.Scan(&it.ID, &it.Description, &it.Quantity, &it.Unit, &it.UnitPrice, &it.Amount, &it.ChargeType, &rate, &it.TaxAmount, &it.BillingRuleID, &it.MeterReadingID, &it.ParkingPermitID, &it.SourceType, &it.SourceID, &meta); err != nil {
			return err
		}
		it.TaxRate = &rate
		if len(meta) > 2 {
			_ = json.Unmarshal(meta, &it.Meta)
		}
		inv.Items = append(inv.Items, it)
	}
	return rows.Err()
}

func (s *Service) staffActions(ctx context.Context, inv *Invoice) {
	p := authctx.Must(ctx)
	can := func(perm string) bool { return p.HasOnProperty(perm, inv.PropertyID) }
	inv.AllowedActions = []string{"view"}
	switch inv.Status {
	case "draft":
		if can("billing.invoices.update") {
			inv.AllowedActions = append(inv.AllowedActions, "update")
		}
		if can("billing.invoices.issue") {
			inv.AllowedActions = append(inv.AllowedActions, "issue")
		}
		if can("billing.invoices.cancel") {
			inv.AllowedActions = append(inv.AllowedActions, "cancel")
		}
	case "issued", "partially_paid", "overdue":
		if can("billing.payments.create") && inv.OutstandingAmt > 0 {
			inv.AllowedActions = append(inv.AllowedActions, "record_payment")
		}
		if can("billing.payments.allocate") && inv.OutstandingAmt > 0 {
			inv.AllowedActions = append(inv.AllowedActions, "apply_credit")
			if can("billing.deposits.manage") {
				inv.AllowedActions = append(inv.AllowedActions, "apply_deposit")
			}
		}
		// P4-INV-07: void hanya untuk invoice terbit yang belum dibayar/dikoreksi; selebihnya lewat credit note
		if can("billing.invoices.void") && inv.PaidAmount == 0 && inv.CreditedAmount == 0 {
			inv.AllowedActions = append(inv.AllowedActions, "void")
		}
		if can("billing.credit_notes.create") {
			inv.AllowedActions = append(inv.AllowedActions, "credit_note")
		}
	case "paid":
		if can("billing.credit_notes.create") {
			inv.AllowedActions = append(inv.AllowedActions, "credit_note")
		}
	}
	if inv.InvoiceNumber != nil && inv.Status != "cancelled" {
		inv.AllowedActions = append(inv.AllowedActions, "download_pdf")
	}
}

func tenantInvoiceActions(inv *Invoice) {
	inv.AllowedActions = []string{"view", "download_pdf"}
	if (inv.Status == "issued" || inv.Status == "partially_paid" || inv.Status == "overdue") && inv.OutstandingAmt > 0 {
		inv.AllowedActions = append(inv.AllowedActions, "pay")
	}
}

type InvoiceInput struct {
	PropertyID     *uuid.UUID `json:"property_id"`
	TenantID       *uuid.UUID `json:"tenant_id"`
	UnitLocationID *uuid.UUID `json:"unit_location_id"`
	InvoiceType    *string    `json:"invoice_type"`
	PeriodStart    *string    `json:"period_start"` // YYYY-MM-DD
	PeriodEnd      *string    `json:"period_end"`
	Description    *string    `json:"description"`
	// TaxAmount: total pajak manual (mode lama). Kosong = pajak dihitung per item (P4-INV-03).
	TaxAmount *int64     `json:"tax_amount"`
	TaxRate   *float64   `json:"tax_rate"`  // tarif default item (%); kosong = pengaturan billing property
	ApplyTax  *bool      `json:"apply_tax"` // kosong = mengikuti pengaturan billing (tax_enabled)
	DueAt     *time.Time `json:"due_at"`
	DueDate   *string    `json:"due_date"` // YYYY-MM-DD zona waktu property (B-13) — diutamakan
	// ExternalRef: nomor dokumen di sistem akuntansi (P4-INT-03)
	ExternalRef *string `json:"external_ref"`
	Notes       *string `json:"notes"`
	Items       *[]Item `json:"items"`
	IssueNow    bool    `json:"issue_now"`
	// internal (modul lain / billing run)
	BillingRunID *uuid.UUID `json:"-"`
}

func parseDate(v *string) (*time.Time, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", *v)
	if err != nil {
		return nil, apperr.Validation("tanggal harus YYYY-MM-DD")
	}
	return &t, nil
}

// RoundMul: qty × harga, dibulatkan half-up ke rupiah (qty 2 desimal) — P4-NFR-01 tanpa float pada hasil.
func RoundMul(qty float64, price int64) int64 {
	qc := int64(math.Round(qty * 100))
	v := qc * price
	if v >= 0 {
		return (v + 50) / 100
	}
	return -((-v + 50) / 100)
}

// TaxOf: pajak = jumlah × tarif% (tarif 2 desimal), half-up.
func TaxOf(amount int64, rate float64) int64 {
	if rate <= 0 || amount == 0 {
		return 0
	}
	bp := int64(math.Round(rate * 100))
	v := amount * bp
	if v >= 0 {
		return (v + 5000) / 10000
	}
	return -((-v + 5000) / 10000)
}

// trustedSource: invoice dari modul internal (sewa, hotel, penjualan) — jumlah baris sudah dihitung modul sumber
// (mis. tarif malam berbeda weekend) dan tanpa PPN default.
func trustedSource(source string) bool {
	return source == "rental" || source == "hotel" || source == "unit_sale" || source == "facility"
}

type taxPlan struct {
	manual      bool
	manualTotal int64
	defaultRate float64
	exempt      bool
}

func (s *Service) taxPlanTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, tenantID *uuid.UUID, in InvoiceInput, source string) (taxPlan, error) {
	if in.TaxAmount != nil {
		if *in.TaxAmount < 0 {
			return taxPlan{}, apperr.Validation("tax_amount tidak boleh negatif")
		}
		return taxPlan{manual: true, manualTotal: *in.TaxAmount}, nil
	}
	st := s.settingsFor(ctx, tx, &propertyID)
	plan := taxPlan{}
	apply := st.TaxEnabled && !trustedSource(source)
	if in.ApplyTax != nil {
		apply = *in.ApplyTax
	}
	if apply {
		plan.defaultRate = st.TaxRate
	}
	if in.TaxRate != nil {
		if *in.TaxRate < 0 || *in.TaxRate > 100 {
			return taxPlan{}, apperr.Validation("tax_rate harus 0..100")
		}
		plan.defaultRate = *in.TaxRate
	}
	if tenantID != nil {
		_ = tx.QueryRow(ctx, `SELECT tax_exempt FROM tenants WHERE id = $1`, *tenantID).Scan(&plan.exempt)
	}
	return plan, nil
}

// computeItems: normalisasi item (qty, jumlah, pajak per item) → subtotal & pajak total.
// mergeItems: item kiriman klien yang membawa `id` item lama mewarisi tautan sumber (rule, pembacaan meter, izin parkir,
// source, meta, charge_type) bila tidak dikirim; jumlah lama (prorata, tarif meter, denda) dipertahankan bila kuantitas & harga
// tidak berubah dan klien tidak mengirim jumlah — jumlah hanya dipakai untuk invoice bersumber tepercaya (computeItems).
func mergeItems(old, in []Item) []Item {
	byID := make(map[uuid.UUID]Item, len(old))
	for _, it := range old {
		byID[it.ID] = it
	}
	for i := range in {
		n := &in[i]
		o, ok := byID[n.ID]
		if n.ID == uuid.Nil || !ok {
			continue
		}
		if n.BillingRuleID == nil {
			n.BillingRuleID = o.BillingRuleID
		}
		if n.MeterReadingID == nil {
			n.MeterReadingID = o.MeterReadingID
		}
		if n.ParkingPermitID == nil {
			n.ParkingPermitID = o.ParkingPermitID
		}
		if n.SourceType == nil && n.SourceID == nil {
			n.SourceType, n.SourceID = o.SourceType, o.SourceID
		}
		if n.Meta == nil {
			n.Meta = o.Meta
		}
		if n.ChargeType == nil {
			n.ChargeType = o.ChargeType
		}
		if n.Amount == 0 && n.Quantity == o.Quantity && n.UnitPrice == o.UnitPrice {
			n.Amount = o.Amount
		}
	}
	return in
}

func computeItems(items []Item, invoiceType string, trust bool, plan taxPlan) (sub, tax int64, err error) {
	for i := range items {
		it := &items[i]
		it.Description = strings.TrimSpace(it.Description)
		if it.Description == "" {
			return 0, 0, apperr.Validation("item.description wajib").WithField(fmt.Sprintf("items[%d].description", i), "wajib")
		}
		if it.Quantity <= 0 {
			it.Quantity = 1
		}
		if it.UnitPrice < 0 {
			return 0, 0, apperr.Validation("item.unit_price tidak boleh negatif").WithField(fmt.Sprintf("items[%d].unit_price", i), "negatif")
		}
		if !trust || it.Amount == 0 {
			it.Amount = RoundMul(it.Quantity, it.UnitPrice)
		}
		if it.Amount < 0 {
			return 0, 0, apperr.Validation("item.amount tidak boleh negatif")
		}
		if it.ChargeType == nil || *it.ChargeType == "" {
			ct := invoiceType
			it.ChargeType = &ct
		} else if !invoiceTypes[*it.ChargeType] {
			return 0, 0, apperr.Validation("item.charge_type tidak valid").WithField(fmt.Sprintf("items[%d].charge_type", i), "tidak valid")
		}
		rate := 0.0
		switch {
		case plan.manual, plan.exempt, it.TaxExempt:
		case it.TaxRate != nil:
			rate = *it.TaxRate
		default:
			rate = plan.defaultRate
		}
		if rate < 0 || rate > 100 {
			return 0, 0, apperr.Validation("item.tax_rate harus 0..100")
		}
		it.TaxRate = &rate
		it.TaxAmount = TaxOf(it.Amount, rate)
		sub += it.Amount
		tax += it.TaxAmount
	}
	if plan.manual {
		tax = plan.manualTotal
	}
	return sub, tax, nil
}

// resolveDue: tanggal jatuh tempo → akhir hari di zona waktu property. due_at tengah malam UTC (input tanggal lama dari Web)
// diperlakukan sebagai tanggal (B-13).
func resolveDue(loc *time.Location, dueAt *time.Time, dueDate *string) (time.Time, time.Time, error) {
	endOf := func(y int, m time.Month, d int) (time.Time, time.Time) {
		return time.Date(y, m, d, 23, 59, 59, 0, loc), time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	if dueDate != nil && *dueDate != "" {
		t, err := time.Parse("2006-01-02", *dueDate)
		if err != nil {
			return time.Time{}, time.Time{}, apperr.Validation("due_date harus YYYY-MM-DD").WithField("due_date", "format tidak valid")
		}
		a, b := endOf(t.Year(), t.Month(), t.Day())
		return a, b, nil
	}
	if dueAt == nil {
		return time.Time{}, time.Time{}, apperr.Validation("due_date wajib").WithField("due_date", "wajib")
	}
	u := dueAt.UTC()
	if u.Hour() == 0 && u.Minute() == 0 && u.Second() == 0 {
		a, b := endOf(u.Year(), u.Month(), u.Day())
		return a, b, nil
	}
	l := dueAt.In(loc)
	_, b := endOf(l.Year(), l.Month(), l.Day())
	return *dueAt, b, nil
}

func insertItemsTx(ctx context.Context, tx pgx.Tx, orgID, invoiceID uuid.UUID, items []Item) error {
	for i, x := range items {
		var meta []byte
		if len(x.Meta) > 0 {
			meta, _ = json.Marshal(x.Meta)
		} else {
			meta = []byte("{}")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO invoice_items (organization_id, invoice_id, sort_order, description, quantity, unit, unit_price, amount, charge_type, tax_rate, tax_amount, billing_rule_id, meter_reading_id, parking_permit_id, source_type, source_id, meta)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, orgID, invoiceID, i, x.Description, x.Quantity, x.Unit, x.UnitPrice, x.Amount, x.ChargeType, *x.TaxRate, x.TaxAmount,
			x.BillingRuleID, x.MeterReadingID, x.ParkingPermitID, x.SourceType, x.SourceID, meta); err != nil {
			return err
		}
	}
	return nil
}

// CreateTx: dipakai staf, impor, billing run & modul komersial (rental/hotel) — invoice draft atau langsung issued.
func (s *Service) CreateTx(ctx context.Context, tx pgx.Tx, in InvoiceInput, source string, sourceID *uuid.UUID) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil {
		return uuid.Nil, apperr.Validation("property_id wajib")
	}
	if err := s.Profile.RequireCapabilityTx(ctx, tx, *in.PropertyID, profile.CapBilling); err != nil {
		return uuid.Nil, err
	}
	if source == "" {
		source = "manual"
	}
	it := deref(in.InvoiceType)
	if it == "" {
		it = "service_charge"
	}
	if !invoiceTypes[it] {
		return uuid.Nil, apperr.Validation("invoice_type tidak valid").WithField("invoice_type", "tidak valid")
	}
	if err := s.validateParties(ctx, tx, *in.PropertyID, in.TenantID, in.UnitLocationID, true); err != nil {
		return uuid.Nil, err
	}
	var items []Item
	if in.Items != nil {
		items = *in.Items
	}
	if len(items) == 0 {
		return uuid.Nil, apperr.Validation("items minimal satu").WithField("items", "wajib")
	}
	plan, err := s.taxPlanTx(ctx, tx, *in.PropertyID, in.TenantID, in, source)
	if err != nil {
		return uuid.Nil, err
	}
	sub, tax, err := computeItems(items, it, trustedSource(source) || source == "billing_run" || source == "penalty", plan)
	if err != nil {
		return uuid.Nil, err
	}
	ps, err := parseDate(in.PeriodStart)
	if err != nil {
		return uuid.Nil, err
	}
	pe, err := parseDate(in.PeriodEnd)
	if err != nil {
		return uuid.Nil, err
	}
	if ps != nil && pe != nil && pe.Before(*ps) {
		return uuid.Nil, apperr.Validation("period_end harus setelah period_start").WithField("period_end", "sebelum period_start")
	}
	loc := property.PropertyTimezone(ctx, tx, *in.PropertyID)
	dueAt, dueDate, err := resolveDue(loc, in.DueAt, in.DueDate)
	if err != nil {
		return uuid.Nil, err
	}
	taxMode := "computed"
	if plan.manual {
		taxMode = "manual"
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO invoices (organization_id, property_id, tenant_id, unit_location_id, invoice_type, period_start, period_end, description, subtotal_amount, tax_amount, tax_mode, total_amount, due_at, due_date, status, source, source_id, billing_run_id, external_ref, notes, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'draft',$15,$16,$17,$18,$19,$20,$20) RETURNING id`,
		p.OrganizationID, *in.PropertyID, in.TenantID, in.UnitLocationID, it, ps, pe, in.Description, sub, tax, taxMode, sub+tax, dueAt, dueDate, source, sourceID, in.BillingRunID, in.ExternalRef, in.Notes, actorOrNil(p)).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if err := insertItemsTx(ctx, tx, p.OrganizationID, id, items); err != nil {
		return uuid.Nil, err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"total": sub + tax, "status": "draft", "source": source}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "invoice", EntityID: &id, EntityLabel: "Draft " + it})
	if in.IssueNow {
		if _, err := s.issueTx(ctx, tx, id); err != nil {
			return uuid.Nil, err
		}
	}
	return id, nil
}

// validateParties: tenant & unit harus berada di property invoice (juga saat update draft, B-07).
func (s *Service) validateParties(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, tenantID, unitID *uuid.UUID, requireOne bool) error {
	if requireOne && tenantID == nil && unitID == nil {
		return apperr.Validation("tenant_id atau unit_location_id wajib").WithField("tenant_id", "tenant atau unit wajib")
	}
	if tenantID != nil {
		var tpid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM tenants WHERE id = $1 AND deleted_at IS NULL`, *tenantID).Scan(&tpid); err != nil || tpid != propertyID {
			return apperr.Validation("tenant_id tidak ditemukan di property ini").WithField("tenant_id", "tidak valid")
		}
	}
	if unitID != nil {
		pid, err := property.ResolvePropertyOfLocation(ctx, tx, *unitID)
		if err != nil || pid != propertyID {
			return apperr.Validation("unit_location_id tidak berada di property ini").WithField("unit_location_id", "tidak valid")
		}
	}
	return nil
}

// issueTx: draft → issued dengan nomor INV saat terbit (D-P4-01) + event invoice.issued. Mengembalikan nomor.
func (s *Service) issueTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (string, error) {
	p := authctx.Must(ctx)
	var status string
	var number *string
	var propertyID uuid.UUID
	var total int64
	var due time.Time
	if err := tx.QueryRow(ctx, `SELECT status, invoice_number, property_id, total_amount, due_at FROM invoices WHERE id = $1 FOR UPDATE`, id).Scan(&status, &number, &propertyID, &total, &due); err != nil {
		return "", apperr.NotFound("Invoice")
	}
	if status != "draft" {
		return "", apperr.InvalidTransition("Hanya invoice draft yang dapat diterbitkan")
	}
	if total <= 0 {
		return "", apperr.Validation("Total invoice harus > 0")
	}
	num := ""
	if number != nil {
		num = *number
	} else {
		n, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixInvoice, time.Now(), property.PropertyTimezone(ctx, tx, propertyID))
		if err != nil {
			return "", err
		}
		num = n
	}
	// selalu issued; bila sudah lewat jatuh tempo (mis. impor), sweep berikutnya menandai overdue + notifikasi (sekali)
	st := "issued"
	if _, err := tx.Exec(ctx, `UPDATE invoices SET invoice_number = $2, status = $3, issued_at = now(), updated_by = $4 WHERE id = $1`, id, num, st, actorOrNil(p)); err != nil {
		return "", err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: id, Action: audit.ActStatusChanged, From: "draft", To: st, Payload: map[string]any{"number": num}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "invoice", EntityID: &id, EntityLabel: num, Before: map[string]any{"status": "draft"}, After: map[string]any{"status": st}})
	s.emitInvoice(ctx, tx, id, num, propertyID, EventInvoiceIssued, map[string]any{"total": total, "due_at": due})
	return num, nil
}

func (s *Service) emitInvoice(ctx context.Context, tx pgx.Tx, id uuid.UUID, number string, propertyID uuid.UUID, evType string, extra map[string]any) {
	if s.Jobs == nil {
		return
	}
	p := authctx.Must(ctx)
	payload := map[string]any{"domain": "finance"}
	for k, v := range extra {
		payload[k] = v
	}
	_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: evType, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "invoice", ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p), Payload: payload})
}

func (s *Service) getTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Invoice, error) {
	inv, err := scanInvoice(tx.QueryRow(ctx, invSelect+` WHERE i.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Invoice")
		}
		return nil, err
	}
	return inv, loadItems(ctx, tx, inv)
}

type Filter struct {
	PropertyID   *uuid.UUID
	TenantID     *uuid.UUID
	UnitID       *uuid.UUID
	Statuses     []string
	Type         string
	Source       string
	SourceID     *uuid.UUID
	BillingRunID *uuid.UUID
	Aging        string // current | 1_30 | 31_60 | 61_90 | 90_plus (P4-AGE-01 drill-down)
	DueFrom      *time.Time
	DueTo        *time.Time
	IssuedFrom   *time.Time
	IssuedTo     *time.Time
	Q            string
	Overdue      bool
	Open         bool

	// tanggal kalender property (YYYY-MM-DD, inklusif)
	DueFromDate    *string
	DueToDate      *string
	IssuedFromDate *string
	IssuedToDate   *string
}

// daysPastDueSQL: hari kalender lewat jatuh tempo di zona waktu property (butuh join properties pr) — B-13.
func daysPastDueSQL(alias string) string {
	return "((now() AT TIME ZONE pr.timezone)::date - COALESCE(" + alias + ".due_date, (" + alias + ".due_at AT TIME ZONE pr.timezone)::date))"
}

// agingSQL: bucket umur piutang dari jatuh tempo (hari kalender zona waktu property).
func agingSQL(alias, bucket string) string {
	days := daysPastDueSQL(alias)
	open := alias + ".status IN ('issued','partially_paid','overdue') AND " + alias + ".total_amount - " + alias + ".paid_amount - " + alias + ".credited_amount > 0"
	switch bucket {
	case "current":
		return open + " AND " + days + " <= 0"
	case "1_30":
		return open + " AND " + days + " BETWEEN 1 AND 30"
	case "31_60":
		return open + " AND " + days + " BETWEEN 31 AND 60"
	case "61_90":
		return open + " AND " + days + " BETWEEN 61 AND 90"
	case "90_plus":
		return open + " AND " + days + " > 90"
	}
	return "true"
}

// scopeInvoices: property + scope Building/Tower (P4-ACL-04) via lokasi unit invoice.
func scopeInvoices(p *authctx.Principal, perm string, propertyID *uuid.UUID, add func(any) string) (string, error) {
	where := ""
	if propertyID != nil {
		if !p.HasAnyOnProperty(perm, *propertyID) {
			return "", apperr.Forbidden("Memerlukan " + perm)
		}
		where += " AND i.property_id = " + add(*propertyID)
	}
	where += " AND " + p.ScopeSQL(perm, "i.property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = i.unit_location_id)", add)
	return where, nil
}

// canViewAt: izin baca pada property, atau grant ber-scope Building/Tower yang memuat lokasi unit invoice (P4-ACL-04).
// Aksi keuangan (terbitkan, void, catat/verifikasi pembayaran, refund) tetap memerlukan grant property.
func canViewAt(ctx context.Context, tx pgx.Tx, perm string, propertyID uuid.UUID, unitID *uuid.UUID) error {
	p := authctx.Must(ctx)
	if p.HasOnProperty(perm, propertyID) {
		return nil
	}
	if unitID != nil {
		var path string
		if err := tx.QueryRow(ctx, `SELECT path::text FROM locations WHERE id = $1`, *unitID).Scan(&path); err == nil && p.HasOnLocation(perm, propertyID, path) {
			return nil
		}
	}
	return apperr.Forbidden("Tidak memiliki " + perm + " pada property ini")
}

// invoiceUnitTx: lokasi unit invoice sebuah pembayaran (untuk scope Building).
func invoiceUnitTx(ctx context.Context, tx pgx.Tx, invoiceID uuid.UUID) *uuid.UUID {
	var unit *uuid.UUID
	_ = tx.QueryRow(ctx, `SELECT unit_location_id FROM invoices WHERE id = $1`, invoiceID).Scan(&unit)
	return unit
}

func (s *Service) List(ctx context.Context, f Filter, page httpx.Page) ([]Invoice, *string, error) {
	p := authctx.Must(ctx)
	var out []Invoice
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var args []any
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		sw, err := scopeInvoices(p, "billing.invoices.view", f.PropertyID, add)
		if err != nil {
			return err
		}
		where := " WHERE true" + sw
		if f.TenantID != nil {
			where += " AND i.tenant_id = " + add(*f.TenantID)
		}
		if f.UnitID != nil {
			where += " AND i.unit_location_id = " + add(*f.UnitID)
		}
		if len(f.Statuses) > 0 {
			where += " AND i.status = ANY(" + add(f.Statuses) + ")"
		}
		if f.Type != "" {
			where += " AND i.invoice_type = " + add(f.Type)
		}
		if f.Source != "" {
			where += " AND i.source = " + add(f.Source)
		}
		if f.SourceID != nil {
			where += " AND i.source_id = " + add(*f.SourceID)
		}
		if f.BillingRunID != nil {
			where += " AND i.billing_run_id = " + add(*f.BillingRunID)
		}
		if f.Overdue {
			where += " AND i.status = 'overdue'"
		}
		if f.Open {
			where += " AND i.status IN ('issued','partially_paid','overdue')"
		}
		if f.Aging != "" {
			where += " AND " + agingSQL("i", f.Aging)
		}
		if f.DueFrom != nil {
			where += " AND i.due_at >= " + add(*f.DueFrom)
		}
		if f.DueTo != nil {
			where += " AND i.due_at <= " + add(*f.DueTo)
		}
		if f.IssuedFrom != nil {
			where += " AND i.issued_at >= " + add(*f.IssuedFrom)
		}
		if f.IssuedTo != nil {
			where += " AND i.issued_at <= " + add(*f.IssuedTo)
		}
		localDue := "COALESCE(i.due_date, (i.due_at AT TIME ZONE pr.timezone)::date)"
		if f.DueFromDate != nil {
			where += " AND " + localDue + " >= " + add(*f.DueFromDate) + "::date"
		}
		if f.DueToDate != nil {
			where += " AND " + localDue + " <= " + add(*f.DueToDate) + "::date"
		}
		if f.IssuedFromDate != nil {
			where += " AND (i.issued_at AT TIME ZONE pr.timezone)::date >= " + add(*f.IssuedFromDate) + "::date"
		}
		if f.IssuedToDate != nil {
			where += " AND (i.issued_at AT TIME ZONE pr.timezone)::date <= " + add(*f.IssuedToDate) + "::date"
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			a := add("%" + q + "%")
			where += " AND (i.invoice_number ILIKE " + a + " OR t.name ILIKE " + a + " OR i.description ILIKE " + a + " OR i.external_ref ILIKE " + a + " OR u.unit_number ILIKE " + a + ")"
		}
		if page.Cursor != nil {
			c1, c2 := add(page.Cursor.Value), add(page.Cursor.ID)
			where += " AND (i.created_at, i.id) < (" + c1 + "::timestamptz, " + c2 + ")"
		}
		q := strings.Replace(invSelect, "FROM invoices i ", "FROM invoices i JOIN properties pr ON pr.location_id = i.property_id ", 1)
		rows, err := tx.Query(ctx, q+where+fmt.Sprintf(" ORDER BY i.created_at DESC, i.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		var items []Invoice
		for rows.Next() {
			v, err := scanInvoice(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, *v)
		}
		rows.Close()
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			s.staffActions(ctx, &items[i])
		}
		out = items
		return nil
	})
	if out == nil {
		out = []Invoice{}
	}
	return out, next, err
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	var out *Invoice
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inv, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := canViewAt(ctx, tx, "billing.invoices.view", inv.PropertyID, inv.UnitLocationID); err != nil {
			return err
		}
		s.staffActions(ctx, inv)
		out = inv
		return nil
	})
	return out, err
}

func (s *Service) Create(ctx context.Context, in InvoiceInput) (*Invoice, error) {
	if in.PropertyID == nil {
		return nil, apperr.Validation("property_id wajib")
	}
	if err := iam.CanOnProperty(ctx, "billing.invoices.create", *in.PropertyID); err != nil {
		return nil, err
	}
	if in.IssueNow {
		if err := iam.CanOnProperty(ctx, "billing.invoices.issue", *in.PropertyID); err != nil {
			return nil, err
		}
	}
	var out *Invoice
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.CreateTx(ctx, tx, in, "manual", nil)
		if err != nil {
			return err
		}
		out, err = s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s.staffActions(ctx, out)
		return nil
	})
	return out, err
}

// Update: hanya draft; validasi sama dengan create (B-07): pihak, pajak, jumlah baris.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in InvoiceInput, ifVersion *int) (*Invoice, error) {
	p := authctx.Must(ctx)
	var out *Invoice
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inv, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.invoices.update", inv.PropertyID); err != nil {
			return err
		}
		if inv.Status != "draft" {
			return apperr.Conflict("INVOICE_NOT_DRAFT", "Hanya invoice draft yang dapat diubah; invoice terbit dikoreksi lewat void atau credit note")
		}
		if ifVersion != nil && *ifVersion != inv.Version {
			return apperr.StaleVersion()
		}
		tenantID, unitID := inv.TenantID, inv.UnitLocationID
		if in.TenantID != nil {
			tenantID = in.TenantID
			if *in.TenantID == uuid.Nil {
				tenantID = nil
			}
		}
		if in.UnitLocationID != nil {
			unitID = in.UnitLocationID
			if *in.UnitLocationID == uuid.Nil {
				unitID = nil
			}
		}
		if err := s.validateParties(ctx, tx, inv.PropertyID, tenantID, unitID, true); err != nil {
			return err
		}
		itype := inv.InvoiceType
		if in.InvoiceType != nil {
			if !invoiceTypes[*in.InvoiceType] {
				return apperr.Validation("invoice_type tidak valid").WithField("invoice_type", "tidak valid")
			}
			itype = *in.InvoiceType
		}
		items := inv.Items
		if in.Items != nil {
			items = mergeItems(inv.Items, *in.Items)
			if len(items) == 0 {
				return apperr.Validation("items minimal satu").WithField("items", "wajib")
			}
		} else {
			// item lama: hitung ulang pajak dengan pengaturan terbaru bila mode computed
			for i := range items {
				items[i].TaxRate = nil
				if in.TaxAmount == nil && inv.TaxMode == "computed" {
					r := 0.0
					if items[i].TaxAmount > 0 && items[i].Amount > 0 {
						r = math.Round(float64(items[i].TaxAmount)*10000/float64(items[i].Amount)) / 100
					}
					items[i].TaxRate = &r
				}
			}
		}
		plan, err := s.taxPlanTx(ctx, tx, inv.PropertyID, tenantID, in, inv.Source)
		if err != nil {
			return err
		}
		if in.TaxAmount == nil && inv.TaxMode == "manual" && in.Items == nil && in.TaxRate == nil && in.ApplyTax == nil {
			plan = taxPlan{manual: true, manualTotal: inv.TaxAmount}
		}
		sub, tax, err := computeItems(items, itype, trustedSource(inv.Source) || inv.Source == "billing_run" || inv.Source == "penalty", plan)
		if err != nil {
			return err
		}
		ps, err := parseDate(in.PeriodStart)
		if err != nil {
			return err
		}
		pe, err := parseDate(in.PeriodEnd)
		if err != nil {
			return err
		}
		dueAt, dueDate := inv.DueAt, (*time.Time)(nil)
		if in.DueAt != nil || in.DueDate != nil {
			a, b, err := resolveDue(property.PropertyTimezone(ctx, tx, inv.PropertyID), in.DueAt, in.DueDate)
			if err != nil {
				return err
			}
			dueAt, dueDate = a, &b
		}
		taxMode := "computed"
		if plan.manual {
			taxMode = "manual"
		}
		if _, err := tx.Exec(ctx, `DELETE FROM invoice_items WHERE invoice_id = $1`, id); err != nil {
			return err
		}
		if err := insertItemsTx(ctx, tx, p.OrganizationID, id, items); err != nil {
			return err
		}
		// konvensi pengosongan (web `lib/clearable.ts`): string kosong = kosongkan; field tidak dikirim = tetap
		clearPS, clearPE := in.PeriodStart != nil && *in.PeriodStart == "", in.PeriodEnd != nil && *in.PeriodEnd == ""
		if _, err := tx.Exec(ctx, `UPDATE invoices SET tenant_id = $2, unit_location_id = $3, invoice_type = $4,
			period_start = CASE WHEN $16 THEN NULL ELSE COALESCE($5, period_start) END, period_end = CASE WHEN $17 THEN NULL ELSE COALESCE($6, period_end) END,
			description = CASE WHEN $7::text IS NULL THEN description ELSE NULLIF(btrim($7), '') END,
			subtotal_amount = $8, tax_amount = $9, tax_mode = $10, total_amount = $8::bigint + $9::bigint, due_at = $11, due_date = COALESCE($12, due_date),
			external_ref = CASE WHEN $13::text IS NULL THEN external_ref ELSE NULLIF(btrim($13), '') END,
			notes = CASE WHEN $14::text IS NULL THEN notes ELSE NULLIF(btrim($14), '') END, updated_by = $15 WHERE id = $1`,
			id, tenantID, unitID, itype, ps, pe, in.Description, sub, tax, taxMode, dueAt, dueDate, in.ExternalRef, in.Notes, p.UserID, clearPS, clearPE); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "invoice", EntityID: &id, EntityLabel: inv.DisplayNumber, After: in})
		out, err = s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s.staffActions(ctx, out)
		return nil
	})
	return out, err
}

type ActionInput struct {
	Reason string `json:"reason"`
}

// Act: issue | cancel (draft) | void (terbit belum dibayar). Permission aksi diperiksa pada property invoice (B-05).
func (s *Service) Act(ctx context.Context, id uuid.UUID, action string, in ActionInput) (*Invoice, error) {
	var out *Invoice
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inv, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.invoices.view", inv.PropertyID); err != nil {
			return err
		}
		// kompatibilitas: POST /cancel pada invoice terbit = void
		if action == "cancel" && inv.Status != "draft" {
			action = "void"
		}
		s.staffActions(ctx, inv)
		if !has(inv.AllowedActions, action) {
			perm := map[string]string{"issue": "billing.invoices.issue", "cancel": "billing.invoices.cancel", "void": "billing.invoices.void"}[action]
			if perm != "" && !authctx.Must(ctx).HasOnProperty(perm, inv.PropertyID) {
				return apperr.Forbidden("Memerlukan " + perm)
			}
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk invoice berstatus %s", action, inv.Status))
		}
		switch action {
		case "issue":
			if _, err := s.issueTx(ctx, tx, id); err != nil {
				return err
			}
		case "cancel", "void":
			if err := s.cancelTx(ctx, tx, inv, strings.TrimSpace(in.Reason), action); err != nil {
				return err
			}
		}
		out, err = s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s.staffActions(ctx, out)
		return nil
	})
	return out, err
}

// cancelTx: batalkan draft / void invoice terbit (tanpa hapus; nomor tetap tercatat, P4-NFR-03).
func (s *Service) cancelTx(ctx context.Context, tx pgx.Tx, inv *Invoice, reason, action string) error {
	p := authctx.Must(ctx)
	if reason == "" {
		return apperr.Validation("reason wajib").WithField("reason", "wajib")
	}
	if _, err := tx.Exec(ctx, `UPDATE invoices SET status = 'cancelled', cancelled_at = now(), cancel_reason = $2, updated_by = $3 WHERE id = $1`, inv.ID, reason, actorOrNil(p)); err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `UPDATE payments SET status = 'cancelled', updated_by = $2 WHERE invoice_id = $1 AND status IN ('initiated','pending')`, inv.ID, actorOrNil(p))
	// baris billing run dilepas agar unit × periode dapat ditagihkan ulang (idempotensi P4-BRL-03)
	_, _ = tx.Exec(ctx, `UPDATE billing_run_lines SET invoice_id = NULL WHERE invoice_id = $1`, inv.ID)
	// invoice denda dibatalkan → denda kembali belum ditagihkan; denda milik invoice ini berhenti
	_, _ = tx.Exec(ctx, `UPDATE invoice_penalties ip SET status = CASE WHEN ip.status = 'billed' THEN 'final' ELSE ip.status END,
		billed_invoice_id = CASE WHEN ip.billed_invoice_id = $1 THEN NULL ELSE ip.billed_invoice_id END
		WHERE EXISTS (SELECT 1 FROM invoice_items it WHERE it.invoice_id = $1 AND it.source_type = 'invoice_penalty' AND it.source_id = ip.id)`, inv.ID)
	_, _ = tx.Exec(ctx, `UPDATE invoice_penalties SET status = 'waived', waive_reason = COALESCE(waive_reason, 'Invoice dibatalkan'), waived_at = now() WHERE invoice_id = $1 AND status IN ('accruing','final')`, inv.ID)
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: inv.ID, Action: audit.ActStatusChanged, From: inv.Status, To: "cancelled", Payload: map[string]any{"action": action, "reason": reason}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "invoice", EntityID: &inv.ID, EntityLabel: inv.DisplayNumber, Before: map[string]any{"status": inv.Status}, After: map[string]any{"action": action, "reason": reason}})
	if inv.InvoiceNumber != nil {
		// B-18: tenant diberi tahu pembatalan invoice terbit
		s.emitInvoice(ctx, tx, inv.ID, *inv.InvoiceNumber, inv.PropertyID, EventInvoiceCancelled, map[string]any{"reason": reason})
	}
	return nil
}

// invoiceStatusFor: status invoice terbit dari jumlah terbayar/terkoreksi & jatuh tempo. Lewat jatuh tempo tetap overdue walau
// dibayar sebagian (B-03) — tidak bolak-balik partially_paid ↔ overdue sehingga notifikasi overdue tidak berulang.
func invoiceStatusFor(total, paid, credited int64, due time.Time) string {
	switch {
	case total > 0 && paid+credited >= total:
		return "paid"
	case due.Before(time.Now()):
		return "overdue"
	case paid+credited > 0:
		return "partially_paid"
	}
	return "issued"
}

// SettleResult: hasil penerapan pembayaran ke invoice.
type SettleResult struct {
	Applied int64 `json:"applied_amount"`
	Excess  int64 `json:"excess_amount"` // kelebihan → saldo kredit tenant (P4-PAY-04)
}

// settleTx: tandai payment paid, terapkan ke invoice (terkunci), kelebihan bayar → saldo kredit (B-04), posting sinking fund &
// deposit, event. received (opsional) = jumlah yang benar-benar diterima saat verifikasi Finance (B-09).
func (s *Service) settleTx(ctx context.Context, tx pgx.Tx, paymentID uuid.UUID, paidAt time.Time, verification string, verifiedBy *uuid.UUID, callback []byte, received *int64) (*SettleResult, error) {
	p := authctx.Must(ctx)
	var invID, propertyID uuid.UUID
	var amount int64
	var status, number, provider string
	var tenantUser *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT invoice_id, amount, status, payment_number, property_id, tenant_user_id, provider_code FROM payments WHERE id = $1 FOR UPDATE`, paymentID).
		Scan(&invID, &amount, &status, &number, &propertyID, &tenantUser, &provider); err != nil {
		return nil, apperr.NotFound("Payment")
	}
	if status == "paid" {
		return &SettleResult{}, nil // idempotent
	}
	settleable := status == "initiated" || status == "pending" || (status == "expired" && verification == "staff_manual")
	if !settleable {
		return nil, apperr.Conflict("PAYMENT_NOT_SETTLEABLE", "Payment berstatus "+status)
	}
	if received != nil {
		if *received <= 0 {
			return nil, apperr.Validation("amount diterima harus > 0").WithField("amount", "harus > 0")
		}
		amount = *received
	}
	var total, paid, credited int64
	var invStatus string
	var invNumber *string
	var due time.Time
	var tenantID, unitID *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT total_amount, paid_amount, credited_amount, status, invoice_number, due_at, tenant_id, unit_location_id FROM invoices WHERE id = $1 FOR UPDATE`, invID).
		Scan(&total, &paid, &credited, &invStatus, &invNumber, &due, &tenantID, &unitID); err != nil {
		return nil, err
	}
	if invStatus == "cancelled" || invStatus == "draft" {
		return nil, apperr.Conflict("INVOICE_NOT_PAYABLE", "Invoice berstatus "+invStatus)
	}
	outstanding := total - paid - credited
	if outstanding < 0 {
		outstanding = 0
	}
	applied := amount
	if applied > outstanding {
		applied = outstanding
	}
	excess := amount - applied
	if _, err := tx.Exec(ctx, `UPDATE payments SET status = 'paid', amount = $7, paid_at = $2, verified_at = now(), verification = $3, verified_by = $4, receipt_number = payment_number,
		callback_payload = COALESCE($5::jsonb, callback_payload), updated_by = $6 WHERE id = $1`,
		paymentID, paidAt, verification, verifiedBy, nullJSON(callback), actorOrNil(p), amount); err != nil {
		return nil, err
	}
	newStatus := invoiceStatusFor(total, paid+applied, credited, due)
	if _, err := tx.Exec(ctx, `UPDATE invoices SET paid_amount = paid_amount + $2, status = $3, paid_at = CASE WHEN $3 = 'paid' THEN $4 ELSE paid_at END, updated_by = $5 WHERE id = $1`,
		invID, applied, newStatus, paidAt, actorOrNil(p)); err != nil {
		return nil, err
	}
	if newStatus == "paid" {
		// denda berhenti bertambah saat invoice lunas
		_, _ = tx.Exec(ctx, `UPDATE invoice_penalties SET status = 'final' WHERE invoice_id = $1 AND status = 'accruing'`, invID)
	}
	if excess > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO tenant_credit_entries (organization_id, property_id, tenant_id, unit_location_id, entry_type, amount, description, invoice_id, payment_id, created_by)
			VALUES ($1,$2,$3,$4,'overpayment',$5,$6,$7,$8,$9)`, p.OrganizationID, propertyID, tenantID, unitID, excess, "Kelebihan bayar "+number, invID, paymentID, actorOrNil(p)); err != nil {
			return nil, err
		}
	}
	if err := s.postFundsTx(ctx, tx, invID, &paymentID); err != nil {
		return nil, err
	}
	num := ""
	if invNumber != nil {
		num = *invNumber
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "payment", ObjectID: paymentID, Action: audit.ActStatusChanged, From: status, To: "paid", Payload: map[string]any{"verification": verification, "amount": amount}})
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: invID, Action: "payment_received", Payload: map[string]any{"payment": number, "amount": amount, "applied": applied, "excess": excess, "status": newStatus}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "payment", EntityID: &paymentID, EntityLabel: number, After: map[string]any{"status": "paid", "verification": verification, "applied": applied, "excess": excess}})
	if s.Jobs != nil {
		payload := map[string]any{"amount": amount, "domain": "finance", "invoice_number": num, "receipt_number": number, "applied": applied, "excess": excess}
		if tenantUser != nil {
			payload["tenant_user_id"] = *tenantUser
		}
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventPaymentPaid, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "payment", ObjectID: paymentID, ObjectLabel: number, ActorUserID: actorOrNil(p), Payload: payload})
		if newStatus == "paid" {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventInvoicePaid, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "invoice", ObjectID: invID, ObjectLabel: num, ActorUserID: actorOrNil(p), Payload: payload})
		}
	}
	return &SettleResult{Applied: applied, Excess: excess}, nil
}

func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// RecordInput: pencatatan pembayaran manual oleh Finance / verifikasi dengan jumlah diterima (B-09).
type RecordInput struct {
	Amount      int64      `json:"amount"`
	Method      string     `json:"method"`
	PaidAt      *time.Time `json:"paid_at"`
	Notes       *string    `json:"notes"`
	Reference   *string    `json:"reference"`
	ExternalRef *string    `json:"external_ref"`
}

var manualMethods = map[string]bool{"transfer": true, "cash": true, "card": true, "va": true, "qris": true, "ewallet": true, "check": true, "other": true}

// RecordManual: staf mencatat pembayaran yang sudah diterima (verifikasi langsung). Kelebihan → saldo kredit (P4-PAY-04);
// penerimaan ganda dicegah: bila tenant sudah mengajukan pembayaran yang menunggu verifikasi, verifikasi itu (B-04).
func (s *Service) RecordManual(ctx context.Context, invoiceID uuid.UUID, in RecordInput) (*Payment, error) {
	var out *Payment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.recordManualTx(ctx, tx, invoiceID, in, "")
		if err != nil {
			return err
		}
		out, err = s.getPaymentTx(ctx, tx, id)
		if err == nil {
			s.paymentActions(ctx, out)
		}
		return err
	})
	return out, err
}

func (s *Service) recordManualTx(ctx context.Context, tx pgx.Tx, invoiceID uuid.UUID, in RecordInput, receiptGroup string) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	inv, err := s.getTx(ctx, tx, invoiceID)
	if err != nil {
		return uuid.Nil, err
	}
	if err := iam.CanOnProperty(ctx, "billing.payments.create", inv.PropertyID); err != nil {
		return uuid.Nil, err
	}
	if err := iam.CanOnProperty(ctx, "billing.payments.verify", inv.PropertyID); err != nil {
		return uuid.Nil, err
	}
	if inv.OutstandingAmt <= 0 || !(inv.Status == "issued" || inv.Status == "partially_paid" || inv.Status == "overdue") {
		return uuid.Nil, apperr.Conflict("INVOICE_NOT_PAYABLE", "Invoice tidak dapat dibayar")
	}
	if in.Amount <= 0 {
		return uuid.Nil, apperr.Validation("amount harus > 0").WithField("amount", "harus > 0")
	}
	var pendingNum *string
	_ = tx.QueryRow(ctx, `SELECT payment_number FROM payments WHERE invoice_id = $1 AND status IN ('initiated','pending') ORDER BY created_at DESC LIMIT 1`, inv.ID).Scan(&pendingNum)
	if pendingNum != nil && in.Method != "credit" && in.Method != "deposit" {
		return uuid.Nil, apperr.Conflict("PAYMENT_PENDING", "Ada pembayaran tenant "+*pendingNum+" yang menunggu verifikasi; verifikasi pembayaran tersebut agar tidak tercatat ganda")
	}
	method := in.Method
	if method == "" {
		method = "transfer"
	}
	if !manualMethods[method] && method != "credit" && method != "deposit" {
		return uuid.Nil, apperr.Validation("method tidak valid").WithField("method", "tidak valid")
	}
	loc := property.PropertyTimezone(ctx, tx, inv.PropertyID)
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixPayment, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO payments (id, organization_id, property_id, payment_number, invoice_id, amount, currency_code, provider_code, method, status, notes, reference, external_ref, receipt_group, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'manual',$8,'pending',$9,NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),$13,$13)`,
		id, p.OrganizationID, inv.PropertyID, number, inv.ID, in.Amount, inv.CurrencyCode, method, in.Notes, deref(in.Reference), deref(in.ExternalRef), receiptGroup, p.UserID); err != nil {
		return uuid.Nil, err
	}
	paidAt := time.Now().UTC()
	if in.PaidAt != nil {
		paidAt = *in.PaidAt
	}
	if _, err := s.settleTx(ctx, tx, id, paidAt, "staff_manual", &p.UserID, nil, nil); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// VerifyPending: Finance memverifikasi pembayaran manual yang diinisiasi tenant (bukti transfer). Jumlah diterima dapat
// disesuaikan dan pembayaran pending yang sudah kedaluwarsa tetap dapat diverifikasi (B-09).
func (s *Service) VerifyPending(ctx context.Context, paymentID uuid.UUID, in RecordInput) (*Payment, error) {
	p := authctx.Must(ctx)
	var out *Payment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pay, err := s.getPaymentTx(ctx, tx, paymentID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.payments.verify", pay.PropertyID); err != nil {
			return err
		}
		if pay.ProviderCode != "manual" {
			return apperr.Conflict("GATEWAY_PAYMENT", "Pembayaran gateway hanya diverifikasi melalui callback provider")
		}
		paidAt := time.Now().UTC()
		if in.PaidAt != nil {
			paidAt = *in.PaidAt
		}
		var received *int64
		if in.Amount > 0 {
			received = &in.Amount
		}
		if _, err := s.settleTx(ctx, tx, paymentID, paidAt, "staff_manual", &p.UserID, nil, received); err != nil {
			return err
		}
		if in.Notes != nil || in.Reference != nil || in.ExternalRef != nil {
			_, _ = tx.Exec(ctx, `UPDATE payments SET notes = COALESCE($2, notes), reference = COALESCE(NULLIF($3,''), reference), external_ref = COALESCE(NULLIF($4,''), external_ref) WHERE id = $1`,
				paymentID, in.Notes, deref(in.Reference), deref(in.ExternalRef))
		}
		out, err = s.getPaymentTx(ctx, tx, paymentID)
		if err == nil {
			s.paymentActions(ctx, out)
		}
		return err
	})
	return out, err
}
