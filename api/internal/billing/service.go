// Package billing: Invoice & Payment (PRD P1 v1.3 §23, WF-P1-006, AT-P1-012; NC §34–§36). Bukan accounting penuh (§5 Non-Goals):
// invoice dibuat staf / import / modul komersial, tenant melihat & membayar via provider (TD-P1-007), status akhir dari callback terverifikasi.
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/metering"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantscope"
)

const (
	EventInvoiceIssued    = "invoice.issued"
	EventInvoiceDueSoon   = "invoice.due_soon"
	EventInvoiceOverdue   = "invoice.overdue"
	EventInvoicePaid      = "invoice.paid"
	EventInvoiceCancelled = "invoice.cancelled"
	EventPaymentInitiated = "payment.initiated"
	EventPaymentPaid      = "payment.paid"
	EventPaymentFailed    = "payment.failed"
)

type Service struct {
	DB        *db.DB
	Jobs      jobs.Enqueuer
	Profile   *profile.Service
	PublicURL string
	// Metering: pemakaian meter untuk billing rule meter_usage (P4-UTL-05).
	Metering *metering.Service
	// DocumentSecret: kunci HMAC tautan dokumen publik (PDF invoice/kuitansi/statement).
	DocumentSecret []byte
}

func New(d *db.DB, j jobs.Enqueuer, prof *profile.Service, publicURL string) *Service {
	return &Service{DB: d, Jobs: j, Profile: prof, PublicURL: publicURL}
}

// ---------- Payment ----------

type Payment struct {
	ID            uuid.UUID  `json:"id"`
	PaymentNumber string     `json:"payment_number"`
	PropertyID    uuid.UUID  `json:"property_id"`
	InvoiceID     uuid.UUID  `json:"invoice_id"`
	InvoiceNumber string     `json:"invoice_number"`
	TenantName    *string    `json:"tenant_name"`
	Amount        int64      `json:"amount"`
	CurrencyCode  string     `json:"currency_code"`
	ProviderCode  string     `json:"provider_code"`
	Method        string     `json:"method"`
	Status        string     `json:"status"`
	ProviderRef   *string    `json:"provider_ref"`
	CheckoutURL   *string    `json:"checkout_url"`
	VANumber      *string    `json:"va_number"`
	QRString      *string    `json:"qr_string"`
	Instructions  *string    `json:"instructions"`
	ExpiresAt     *time.Time `json:"expires_at"`
	PaidAt        *time.Time `json:"paid_at"`
	VerifiedAt    *time.Time `json:"verified_at"`
	Verification  *string    `json:"verification"`
	VerifiedBy    *string    `json:"verified_by_name"`
	ReceiptNumber *string    `json:"receipt_number"`
	FailureReason *string    `json:"failure_reason"`
	Notes         *string    `json:"notes"`
	// PRD P4 v2.1: referensi bank, nomor dokumen akuntansi (P4-INT-03), grup penerimaan (P4-PAY-05), refund (P4-PAY-06), bukti transfer (P4-VRF-02)
	Reference      *string    `json:"reference"`
	ExternalRef    *string    `json:"external_ref"`
	ReceiptGroup   *string    `json:"receipt_group"`
	RefundedAt     *time.Time `json:"refunded_at"`
	RefundReason   *string    `json:"refund_reason"`
	ProofCount     int        `json:"proof_count"`
	TenantUserName *string    `json:"tenant_user_name"`
	TenantUserID   *uuid.UUID `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	AllowedActions []string   `json:"allowed_actions"`
	Version        int        `json:"version"`
}

const paySelect = `SELECT p.id, p.payment_number, p.property_id, p.invoice_id, COALESCE(i.invoice_number, 'Draft'), t.name, p.amount, p.currency_code, p.provider_code, p.method, p.status, p.provider_ref, p.checkout_url, p.va_number, p.qr_string, p.instructions,
	p.expires_at, p.paid_at, p.verified_at, p.verification, vb.full_name, p.receipt_number, p.failure_reason, p.notes,
	p.reference, p.external_ref, p.receipt_group, p.refunded_at, p.refund_reason,
	(SELECT count(*) FROM attachments a WHERE a.object_type = 'payment' AND a.object_id = p.id AND a.deleted_at IS NULL), tu.full_name, p.tenant_user_id, p.created_at, p.version
	FROM payments p JOIN invoices i ON i.id = p.invoice_id LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN users vb ON vb.id = p.verified_by LEFT JOIN users tu ON tu.id = p.tenant_user_id`

func scanPayment(row pgx.Row) (*Payment, error) {
	var v Payment
	if err := row.Scan(&v.ID, &v.PaymentNumber, &v.PropertyID, &v.InvoiceID, &v.InvoiceNumber, &v.TenantName, &v.Amount, &v.CurrencyCode, &v.ProviderCode, &v.Method, &v.Status, &v.ProviderRef, &v.CheckoutURL, &v.VANumber, &v.QRString, &v.Instructions,
		&v.ExpiresAt, &v.PaidAt, &v.VerifiedAt, &v.Verification, &v.VerifiedBy, &v.ReceiptNumber, &v.FailureReason, &v.Notes,
		&v.Reference, &v.ExternalRef, &v.ReceiptGroup, &v.RefundedAt, &v.RefundReason, &v.ProofCount, &v.TenantUserName, &v.TenantUserID, &v.CreatedAt, &v.Version); err != nil {
		return nil, err
	}
	v.CurrencyCode = strings.TrimSpace(v.CurrencyCode)
	v.AllowedActions = []string{"view"}
	return &v, nil
}

type ProviderInfo struct {
	Code      string         `json:"code"`
	Name      string         `json:"name"`
	IsActive  bool           `json:"is_active"`
	Methods   []string       `json:"methods"`
	Config    map[string]any `json:"config"`
	HasSecret bool           `json:"has_secret"`
}

func (s *Service) providersTx(ctx context.Context, tx pgx.Tx, activeOnly bool) ([]ProviderInfo, error) {
	rows, err := tx.Query(ctx, `SELECT code, name, is_active, methods, config, webhook_secret IS NOT NULL AND webhook_secret <> '' FROM payment_providers WHERE (NOT $1::bool OR is_active) ORDER BY code`, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderInfo{}
	for rows.Next() {
		var pi ProviderInfo
		var cfg []byte
		if err := rows.Scan(&pi.Code, &pi.Name, &pi.IsActive, &pi.Methods, &cfg, &pi.HasSecret); err != nil {
			return nil, err
		}
		pi.Config = map[string]any{}
		_ = json.Unmarshal(cfg, &pi.Config)
		out = append(out, pi)
	}
	return out, rows.Err()
}

func (s *Service) ListProviders(ctx context.Context) ([]ProviderInfo, error) {
	var out []ProviderInfo
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.providersTx(ctx, tx, false)
		return err
	})
	return out, err
}

type ProviderInput struct {
	Name          *string         `json:"name"`
	IsActive      *bool           `json:"is_active"`
	Methods       *[]string       `json:"methods"`
	Config        *map[string]any `json:"config"`
	WebhookSecret *string         `json:"webhook_secret"`
}

// UpsertProvider: konfigurasi provider (billing.settings.manage tingkat organization, B-15). Secret tidak pernah dikembalikan.
// Provider tanpa adapter (midtrans/xendit — online payment HOLD, P4-ONL-03) tidak dapat diaktifkan.
func (s *Service) UpsertProvider(ctx context.Context, code string, in ProviderInput) (*ProviderInfo, error) {
	p := authctx.Must(ctx)
	if _, all := p.PropertyIDsFor("billing.settings.manage"); !all {
		return nil, apperr.Forbidden("Memerlukan billing.settings.manage tingkat organization")
	}
	_, hasAdapter := Get(code)
	if !hasAdapter && code != "midtrans" && code != "xendit" {
		return nil, apperr.Validation("provider tidak dikenal")
	}
	if !hasAdapter && in.IsActive != nil && *in.IsActive {
		return nil, apperr.Conflict("PROVIDER_NOT_AVAILABLE", "Provider "+code+" belum tersedia (pembayaran online sedang ditunda)")
	}
	if !hasAdapter && in.IsActive == nil {
		f := false
		in.IsActive = &f
	}
	var out *ProviderInfo
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var cfg []byte
		if in.Config != nil {
			cfg, _ = json.Marshal(*in.Config)
		}
		var methods []string
		if in.Methods != nil {
			methods = *in.Methods
		}
		name := deref(in.Name)
		if name == "" {
			name = map[string]string{"manual": "Transfer / Tunai (verifikasi staf)", "mock_gateway": "Mock Gateway", "midtrans": "Midtrans", "xendit": "Xendit"}[code]
		}
		if _, err := tx.Exec(ctx, `INSERT INTO payment_providers (organization_id, code, name, is_active, methods, config, webhook_secret, updated_by)
			VALUES ($1,$2,$3,COALESCE($4,true),COALESCE($5,'{transfer}'::text[]),COALESCE($6,'{}'::jsonb),$7,$8)
			ON CONFLICT (organization_id, code) DO UPDATE SET name = COALESCE(NULLIF($3,''), payment_providers.name), is_active = COALESCE($4, payment_providers.is_active), methods = COALESCE($5, payment_providers.methods),
			  config = COALESCE($6, payment_providers.config), webhook_secret = COALESCE($7, payment_providers.webhook_secret), updated_by = $8, updated_at = now()`,
			p.OrganizationID, code, name, in.IsActive, methods, cfg, in.WebhookSecret, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "payment_provider", EntityLabel: code, After: map[string]any{"is_active": in.IsActive, "methods": in.Methods, "secret_rotated": in.WebhookSecret != nil}})
		list, err := s.providersTx(ctx, tx, false)
		if err != nil {
			return err
		}
		for i := range list {
			if list[i].Code == code {
				out = &list[i]
			}
		}
		return nil
	})
	return out, err
}

type InitiateInput struct {
	ProviderCode string `json:"provider_code"`
	Method       string `json:"method"`
	Amount       *int64 `json:"amount"` // opsional: bayar sebagian (default outstanding)
}

// initiateTx: buat Payment + checkout di provider (tenant) — atau pencatatan manual oleh staf (recordTx).
func (s *Service) initiateTx(ctx context.Context, tx pgx.Tx, inv *Invoice, in InitiateInput, sc *tenantscope.Scope) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if inv.OutstandingAmt <= 0 || !(inv.Status == "issued" || inv.Status == "partially_paid" || inv.Status == "overdue") {
		return uuid.Nil, apperr.Conflict("INVOICE_NOT_PAYABLE", "Invoice tidak dapat dibayar")
	}
	amount := inv.OutstandingAmt
	if in.Amount != nil && *in.Amount > 0 && *in.Amount < amount {
		amount = *in.Amount
	}
	var pending int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM payments WHERE invoice_id = $1 AND status IN ('initiated','pending') AND (expires_at IS NULL OR expires_at > now())`, inv.ID).Scan(&pending)
	if pending > 0 {
		return uuid.Nil, apperr.Conflict("PAYMENT_PENDING", "Masih ada pembayaran yang menunggu penyelesaian untuk invoice ini")
	}
	code := in.ProviderCode
	if code == "" {
		code = "manual"
	}
	prov, ok := Get(code)
	if !ok {
		return uuid.Nil, apperr.Validation("provider_code tidak didukung")
	}
	var name string
	var active bool
	var methods []string
	var cfg []byte
	if err := tx.QueryRow(ctx, `SELECT name, is_active, methods, config FROM payment_providers WHERE code = $1`, code).Scan(&name, &active, &methods, &cfg); err != nil || !active {
		return uuid.Nil, apperr.Validation("Provider pembayaran tidak aktif untuk organisasi ini")
	}
	method := in.Method
	if method == "" && len(methods) > 0 {
		method = methods[0]
	}
	if len(methods) > 0 && !has(methods, method) {
		return uuid.Nil, apperr.Validation("method tidak tersedia pada provider ini")
	}
	config := map[string]any{}
	_ = json.Unmarshal(cfg, &config)
	if _, ok := config["bank_account"]; !ok && code == "manual" {
		if acct := s.bankAccountText(ctx, tx, inv.PropertyID); acct != "" {
			config["bank_account"] = acct
		}
	}
	loc := property.PropertyTimezone(ctx, tx, inv.PropertyID)
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixPayment, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	var tenantUser *uuid.UUID
	payer, payerEmail := "", ""
	if sc != nil {
		tenantUser = &sc.UserID
		_ = tx.QueryRow(ctx, `SELECT full_name, COALESCE(email,'') FROM users WHERE id = $1`, sc.UserID).Scan(&payer, &payerEmail)
	}
	id := uuid.Must(uuid.NewV7())
	res, err := prov.CreateCheckout(ctx, CheckoutRequest{PaymentID: id, PaymentNumber: number, InvoiceNumber: inv.DisplayNumber, Amount: amount, Currency: inv.CurrencyCode, Method: method, PayerName: payer, PayerEmail: payerEmail, Config: config, PublicURL: s.PublicURL})
	if err != nil {
		return uuid.Nil, err
	}
	status := res.Status
	if status == "" {
		status = "pending"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO payments (id, organization_id, property_id, payment_number, invoice_id, tenant_user_id, amount, currency_code, provider_code, method, status, provider_ref, checkout_url, va_number, qr_string, instructions, expires_at, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''),NULLIF($13,''),NULLIF($14,''),NULLIF($15,''),NULLIF($16,''),$17,$18,$18)`,
		id, p.OrganizationID, inv.PropertyID, number, inv.ID, tenantUser, amount, inv.CurrencyCode, code, method, status, res.ProviderRef, res.CheckoutURL, res.VANumber, res.QRString, res.Instructions, res.ExpiresAt, actorOrNil(p)); err != nil {
		return uuid.Nil, err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "payment", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "amount": amount, "provider": code, "method": method}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "payment", EntityID: &id, EntityLabel: number})
	if s.Jobs != nil {
		payload := map[string]any{"amount": amount, "provider": code, "domain": "finance", "invoice_number": inv.InvoiceNumber}
		if tenantUser != nil {
			payload["tenant_user_id"] = *tenantUser
		}
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventPaymentInitiated, OrganizationID: p.OrganizationID, PropertyID: &inv.PropertyID, ObjectType: "payment", ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p), Payload: payload})
	}
	return id, nil
}

// FailPayment: staf menandai pembayaran manual gagal/kedaluwarsa (mis. bukti tidak valid).
func (s *Service) FailPayment(ctx context.Context, paymentID uuid.UUID, reason string) (*Payment, error) {
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
		if pay.Status != "initiated" && pay.Status != "pending" {
			return apperr.Conflict("PAYMENT_NOT_PENDING", "Payment berstatus "+pay.Status)
		}
		if _, err := tx.Exec(ctx, `UPDATE payments SET status = 'failed', failure_reason = NULLIF($2,''), updated_by = $3 WHERE id = $1`, paymentID, strings.TrimSpace(reason), p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "payment", EntityID: &paymentID, EntityLabel: pay.PaymentNumber, After: map[string]any{"status": "failed", "reason": reason}})
		out, err = s.getPaymentTx(ctx, tx, paymentID)
		return err
	})
	return out, err
}

func (s *Service) getPaymentTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Payment, error) {
	pay, err := scanPayment(tx.QueryRow(ctx, paySelect+` WHERE p.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Payment")
		}
		return nil, err
	}
	return pay, nil
}

func (s *Service) paymentActions(ctx context.Context, pay *Payment) {
	p := authctx.Must(ctx)
	pay.AllowedActions = []string{"view"}
	// B-09: pending yang kedaluwarsa tetap dapat diverifikasi
	if (pay.Status == "pending" || pay.Status == "initiated" || pay.Status == "expired") && pay.ProviderCode == "manual" && p.HasOnProperty("billing.payments.verify", pay.PropertyID) {
		pay.AllowedActions = append(pay.AllowedActions, "verify")
		if pay.Status != "expired" {
			pay.AllowedActions = append(pay.AllowedActions, "fail")
		}
	}
	if pay.Status == "refunded" {
		pay.AllowedActions = append(pay.AllowedActions, "download_receipt") // kwitansi tetap tersedia (renderReceiptTx)
	}
	if pay.Status == "paid" {
		pay.AllowedActions = append(pay.AllowedActions, "download_receipt")
		if p.HasOnProperty("billing.payments.refund", pay.PropertyID) && pay.Method != "credit" && pay.Method != "deposit" {
			pay.AllowedActions = append(pay.AllowedActions, "refund")
		}
		if p.HasOnProperty("billing.payments.create", pay.PropertyID) {
			pay.AllowedActions = append(pay.AllowedActions, "update")
		}
	}
}

type PaymentFilter struct {
	PropertyID   *uuid.UUID
	InvoiceID    *uuid.UUID
	TenantID     *uuid.UUID
	Statuses     []string
	Provider     string
	Method       string
	ReceiptGroup string
	PaidFrom     *time.Time
	PaidTo       *time.Time
	Q            string

	// tanggal kalender property (YYYY-MM-DD, inklusif)
	PaidFromDate *string
	PaidToDate   *string
}

func (s *Service) ListPayments(ctx context.Context, f PaymentFilter, page httpx.Page) ([]Payment, *string, error) {
	p := authctx.Must(ctx)
	var out []Payment
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("billing.payments.view", *f.PropertyID) {
				return apperr.Forbidden("Tidak memiliki billing.payments.view pada property ini")
			}
			where += " AND p.property_id = " + add(*f.PropertyID)
		}
		// P4-ACL-04: grant ber-scope Building/Tower melihat pembayaran invoice unit di subtree-nya
		where += " AND " + p.ScopeSQL("billing.payments.view", "p.property_id", "(SELECT sl.path FROM invoices si JOIN locations sl ON sl.id = si.unit_location_id WHERE si.id = p.invoice_id)", add)
		if f.InvoiceID != nil {
			args = append(args, *f.InvoiceID)
			where += fmt.Sprintf(" AND p.invoice_id = $%d", len(args))
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND p.status = ANY($%d)", len(args))
		}
		if f.Provider != "" {
			args = append(args, f.Provider)
			where += fmt.Sprintf(" AND p.provider_code = $%d", len(args))
		}
		if f.TenantID != nil {
			args = append(args, *f.TenantID)
			where += fmt.Sprintf(" AND i.tenant_id = $%d", len(args))
		}
		if f.Method != "" {
			args = append(args, f.Method)
			where += fmt.Sprintf(" AND p.method = $%d", len(args))
		}
		if f.ReceiptGroup != "" {
			args = append(args, f.ReceiptGroup)
			where += fmt.Sprintf(" AND p.receipt_group = $%d", len(args))
		}
		if f.PaidFrom != nil {
			args = append(args, *f.PaidFrom)
			where += fmt.Sprintf(" AND p.paid_at >= $%d", len(args))
		}
		if f.PaidTo != nil {
			args = append(args, *f.PaidTo)
			where += fmt.Sprintf(" AND p.paid_at <= $%d", len(args))
		}
		localPaid := "(p.paid_at AT TIME ZONE (SELECT pz.timezone FROM properties pz WHERE pz.location_id = p.property_id))::date"
		if f.PaidFromDate != nil {
			where += " AND " + localPaid + " >= " + add(*f.PaidFromDate) + "::date"
		}
		if f.PaidToDate != nil {
			where += " AND " + localPaid + " <= " + add(*f.PaidToDate) + "::date"
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (p.payment_number ILIKE $%[1]d OR i.invoice_number ILIKE $%[1]d OR t.name ILIKE $%[1]d OR p.reference ILIKE $%[1]d OR p.external_ref ILIKE $%[1]d OR p.receipt_group ILIKE $%[1]d)", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (p.created_at, p.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, paySelect+where+fmt.Sprintf(" ORDER BY p.created_at DESC, p.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var items []Payment
		for rows.Next() {
			v, err := scanPayment(rows)
			if err != nil {
				return err
			}
			s.paymentActions(ctx, v)
			items = append(items, *v)
		}
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		out = items
		return rows.Err()
	})
	if out == nil {
		out = []Payment{}
	}
	return out, next, err
}

func (s *Service) GetPayment(ctx context.Context, id uuid.UUID) (*Payment, error) {
	var out *Payment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pay, err := s.getPaymentTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := canViewAt(ctx, tx, "billing.payments.view", pay.PropertyID, invoiceUnitTx(ctx, tx, pay.InvoiceID)); err != nil {
			return err
		}
		s.paymentActions(ctx, pay)
		out = pay
		return nil
	})
	return out, err
}

// ---------- Webhook (gateway callback; AT-P1-012 / DoD #32) ----------

type WebhookResult struct {
	PaymentID uuid.UUID `json:"payment_id"`
	Status    string    `json:"status"`
	Duplicate bool      `json:"duplicate"`
}

// HandleWebhook: tanpa auth user; provider_ref → payment → org; signature diverifikasi dengan secret org; idempoten per external_id.
func (s *Service) HandleWebhook(ctx context.Context, providerCode string, r *http.Request, body []byte) (*WebhookResult, error) {
	prov, ok := Get(providerCode)
	if !ok {
		return nil, apperr.NotFound("Provider")
	}
	// pre-parse untuk mendapatkan provider_ref → org (RLS auth_lookup)
	var probe struct {
		ProviderRef string `json:"provider_ref"`
	}
	_ = json.Unmarshal(body, &probe)
	if probe.ProviderRef == "" {
		return nil, apperr.Validation("provider_ref wajib")
	}
	var paymentID, orgID uuid.UUID
	var secret *string
	if err := s.DB.WithAuthLookupTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT p.id, p.organization_id FROM payments p WHERE p.provider_code = $1 AND p.provider_ref = $2`, providerCode, probe.ProviderRef).Scan(&paymentID, &orgID); err != nil {
			return apperr.NotFound("Payment")
		}
		_ = tx.QueryRow(ctx, `SELECT webhook_secret FROM payment_providers WHERE organization_id = $1 AND code = $2`, orgID, providerCode).Scan(&secret)
		return nil
	}); err != nil {
		return nil, err
	}
	ev, verr := prov.VerifyWebhook(r, body, deref(secret))
	out := &WebhookResult{PaymentID: paymentID}
	var sigErr error
	ctx = authctx.With(ctx, authctx.System(orgID))
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		extID := probe.ProviderRef
		if ev != nil {
			extID = ev.ExternalID
		}
		var raw map[string]any
		_ = json.Unmarshal(body, &raw)
		rawJSON, _ := json.Marshal(raw)
		ct, err := tx.Exec(ctx, `INSERT INTO payment_webhook_events (organization_id, provider_code, external_id, signature_valid, payload) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (provider_code, external_id) DO NOTHING`, orgID, providerCode, extID, verr == nil, rawJSON)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			out.Duplicate = true
			_ = tx.QueryRow(ctx, `SELECT status FROM payments WHERE id = $1`, paymentID).Scan(&out.Status)
			return nil
		}
		if verr != nil {
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditAccessDenied, EntityType: "payment_webhook", EntityID: &paymentID, EntityLabel: providerCode, After: map[string]any{"error": verr.Error()}})
			_, _ = tx.Exec(ctx, `UPDATE payment_webhook_events SET processed_at = now(), result = 'signature_invalid' WHERE provider_code = $1 AND external_id = $2`, providerCode, extID)
			sigErr = verr
			return nil // B-11: commit agar jejak event bertanda tangan invalid tidak hilang; error dikembalikan setelah commit
		}
		var amount int64
		_ = tx.QueryRow(ctx, `SELECT amount FROM payments WHERE id = $1`, paymentID).Scan(&amount)
		result := "ignored"
		switch ev.Status {
		case "paid", "settlement", "success", "succeeded":
			if ev.Amount != 0 && ev.Amount != amount {
				result = "amount_mismatch"
				_, _ = tx.Exec(ctx, `UPDATE payments SET failure_reason = 'Nominal callback tidak sesuai', callback_payload = $2 WHERE id = $1`, paymentID, rawJSON)
				out.Status = "amount_mismatch"
				break
			}
			paidAt := time.Now().UTC()
			if ev.PaidAt != nil {
				paidAt = *ev.PaidAt
			}
			if _, err := s.settleTx(ctx, tx, paymentID, paidAt, "gateway_callback", nil, rawJSON, nil); err != nil {
				return err
			}
			result, out.Status = "paid", "paid"
		case "refunded", "refund":
			// B-12: refund dari gateway → status refunded + koreksi invoice (alur sama dengan refund staf)
			if err := s.refundTx(ctx, tx, paymentID, "Refund dari provider: "+ev.Status, false); err != nil {
				return err
			}
			result, out.Status = "refunded", "refunded"
		case "failed", "expired", "cancelled", "deny", "cancel", "expire":
			st := map[string]string{"failed": "failed", "deny": "failed", "expired": "expired", "expire": "expired", "cancelled": "cancelled", "cancel": "cancelled"}[ev.Status]
			_, _ = tx.Exec(ctx, `UPDATE payments SET status = $2, failure_reason = COALESCE(failure_reason, $3), callback_payload = $4 WHERE id = $1 AND status IN ('initiated','pending')`, paymentID, st, "Callback provider: "+ev.Status, rawJSON)
			result, out.Status = st, st
			if s.Jobs != nil {
				var num string
				var pid uuid.UUID
				var tu *uuid.UUID
				_ = tx.QueryRow(ctx, `SELECT payment_number, property_id, tenant_user_id FROM payments WHERE id = $1`, paymentID).Scan(&num, &pid, &tu)
				payload := map[string]any{"status": st, "domain": "finance"}
				if tu != nil {
					payload["tenant_user_id"] = *tu
				}
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventPaymentFailed, OrganizationID: orgID, PropertyID: &pid, ObjectType: "payment", ObjectID: paymentID, ObjectLabel: num, Payload: payload})
			}
		default:
			out.Status = "pending"
		}
		_, _ = tx.Exec(ctx, `UPDATE payment_webhook_events SET processed_at = now(), result = $3 WHERE provider_code = $1 AND external_id = $2`, providerCode, extID, result)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if sigErr != nil {
		return nil, sigErr
	}
	return out, nil
}

// ---------- Tenant API (Mobile Tenant) ----------

type Summary struct {
	OutstandingAmount int64      `json:"outstanding_amount"`
	UnpaidCount       int        `json:"unpaid_count"`
	OverdueCount      int        `json:"overdue_count"`
	NextDueAt         *time.Time `json:"next_due_at"`
	CurrencyCode      string     `json:"currency_code"`
}

func tenantInvoiceWhere(sc *tenantscope.Scope, argIdx int) (string, []any) {
	// tagihan milik tenant (tenants.id) — tenant_user tanpa tenant hanya melihat invoice unit primernya.
	// Invoice reservasi (rental/unit_sale/hotel) milik prospek lain pada unit yang sama tidak boleh terlihat lewat scope unit:
	// hanya lewat tenant_id (di-link saat onboarding).
	unitScope := "(i.unit_location_id = ANY($%d) AND (i.tenant_id IS NOT NULL OR i.source NOT IN ('rental','unit_sale','hotel')))"
	if sc.TenantID != nil {
		return fmt.Sprintf("(i.tenant_id = $%d OR "+unitScope+")", argIdx, argIdx+1), []any{*sc.TenantID, unitIDs(sc)}
	}
	return fmt.Sprintf(unitScope, argIdx), []any{unitIDs(sc)}
}

func unitIDs(sc *tenantscope.Scope) []uuid.UUID {
	out := []uuid.UUID{}
	for _, u := range sc.Units {
		out = append(out, u.ID)
	}
	return out
}

func (s *Service) TenantSummary(ctx context.Context) (*Summary, error) {
	var out Summary
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		where, args := tenantInvoiceWhere(sc, 1)
		out.CurrencyCode = "IDR"
		return tx.QueryRow(ctx, `SELECT COALESCE(sum(i.total_amount - i.paid_amount - i.credited_amount),0), count(*), count(*) FILTER (WHERE i.status = 'overdue'), min(i.due_at) FROM invoices i WHERE `+where+` AND i.status IN ('issued','partially_paid','overdue')`, args...).
			Scan(&out.OutstandingAmount, &out.UnpaidCount, &out.OverdueCount, &out.NextDueAt)
	})
	return &out, err
}

func (s *Service) TenantInvoices(ctx context.Context, open *bool, page httpx.Page) ([]Invoice, *string, error) {
	var out []Invoice
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		where, args := tenantInvoiceWhere(sc, 1)
		where = " WHERE " + where + " AND i.status <> 'draft'"
		if open != nil && *open {
			where += " AND i.status IN ('issued','partially_paid','overdue')"
		} else if open != nil {
			where += " AND i.status IN ('paid','cancelled')"
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (i.due_at, i.id) > ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, invSelect+where+fmt.Sprintf(" ORDER BY i.due_at, i.id LIMIT %d", page.Limit+1), args...)
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
			tenantInvoiceActions(v)
			items = append(items, *v)
		}
		rows.Close()
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.DueAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			if err := loadItems(ctx, tx, &items[i]); err != nil {
				return err
			}
		}
		out = items
		return nil
	})
	if out == nil {
		out = []Invoice{}
	}
	return out, next, err
}

func (s *Service) tenantInvoiceTx(ctx context.Context, tx pgx.Tx, sc *tenantscope.Scope, id uuid.UUID) (*Invoice, error) {
	where, args := tenantInvoiceWhere(sc, 2)
	inv, err := scanInvoice(tx.QueryRow(ctx, invSelect+` WHERE i.id = $1 AND i.status <> 'draft' AND `+where, append([]any{id}, args...)...))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Invoice")
		}
		return nil, err
	}
	tenantInvoiceActions(inv)
	return inv, loadItems(ctx, tx, inv)
}

func (s *Service) TenantInvoice(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	var out *Invoice
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		out, err = s.tenantInvoiceTx(ctx, tx, sc, id)
		return err
	})
	return out, err
}

func (s *Service) TenantProviders(ctx context.Context) ([]ProviderInfo, error) {
	var out []ProviderInfo
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tenantscope.LoadScopeTx(ctx, tx); err != nil {
			return err
		}
		var err error
		out, err = s.providersTx(ctx, tx, true)
		return err
	})
	return out, err
}

// TenantPay: inisiasi pembayaran (WF-P1-006: Tenant → Bills → Invoice → Pay → Gateway).
func (s *Service) TenantPay(ctx context.Context, invoiceID uuid.UUID, in InitiateInput) (*Payment, error) {
	var out *Payment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		inv, err := s.tenantInvoiceTx(ctx, tx, sc, invoiceID)
		if err != nil {
			return err
		}
		id, err := s.initiateTx(ctx, tx, inv, in, sc)
		if err != nil {
			return err
		}
		out, err = s.getPaymentTx(ctx, tx, id)
		if err != nil {
			return err
		}
		tenantPaymentActions(out, sc.UserID)
		return nil
	})
	return out, err
}

func (s *Service) TenantPayments(ctx context.Context, invoiceID *uuid.UUID, page httpx.Page) ([]Payment, *string, error) {
	var out []Payment
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		where, args := tenantInvoiceWhere(sc, 1)
		where = " WHERE " + where
		if invoiceID != nil {
			// B-14: seluruh pembayaran satu invoice difilter server (bukan 50 baris pertama di browser)
			args = append(args, *invoiceID)
			where += fmt.Sprintf(" AND p.invoice_id = $%d", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (p.created_at, p.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, paySelect+where+fmt.Sprintf(" ORDER BY p.created_at DESC, p.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var items []Payment
		for rows.Next() {
			v, err := scanPayment(rows)
			if err != nil {
				return err
			}
			tenantPaymentActions(v, sc.UserID)
			items = append(items, *v)
		}
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		out = items
		return rows.Err()
	})
	if out == nil {
		out = []Payment{}
	}
	return out, next, err
}

// tenantPaymentActions: kwitansi untuk pembayaran lunas; unggah bukti transfer untuk pembayaran manual milik sendiri yang menunggu (P4-VRF-02).
func tenantPaymentActions(v *Payment, userID uuid.UUID) {
	v.AllowedActions = []string{"view"}
	if v.Status == "paid" || v.Status == "refunded" {
		v.AllowedActions = append(v.AllowedActions, "download_receipt")
	}
	if (v.Status == "pending" || v.Status == "initiated") && v.ProviderCode == "manual" && v.TenantUserID != nil && *v.TenantUserID == userID {
		v.AllowedActions = append(v.AllowedActions, "upload_proof")
	}
}

func (s *Service) TenantPayment(ctx context.Context, id uuid.UUID) (*Payment, error) {
	var out *Payment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		where, args := tenantInvoiceWhere(sc, 2)
		pay, err := scanPayment(tx.QueryRow(ctx, paySelect+` WHERE p.id = $1 AND `+where, append([]any{id}, args...)...))
		if err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Payment")
			}
			return err
		}
		tenantPaymentActions(pay, sc.UserID)
		out = pay
		return nil
	})
	return out, err
}

func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func actorOrNil(p *authctx.Principal) *uuid.UUID {
	if p.IsSystem || p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}
