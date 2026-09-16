package app_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type invoiceResp struct {
	ID             uuid.UUID `json:"id"`
	InvoiceNumber  string    `json:"invoice_number"`
	Status         string    `json:"status"`
	TotalAmount    int64     `json:"total_amount"`
	PaidAmount     int64     `json:"paid_amount"`
	Outstanding    int64     `json:"outstanding_amount"`
	AllowedActions []string  `json:"allowed_actions"`
	Items          []struct {
		Description string `json:"description"`
		Amount      int64  `json:"amount"`
	} `json:"items"`
	Version int `json:"version"`
}

type paymentResp struct {
	ID            uuid.UUID `json:"id"`
	PaymentNumber string    `json:"payment_number"`
	Status        string    `json:"status"`
	ProviderCode  string    `json:"provider_code"`
	ProviderRef   *string   `json:"provider_ref"`
	CheckoutURL   *string   `json:"checkout_url"`
	VANumber      *string   `json:"va_number"`
	Instructions  *string   `json:"instructions"`
	ReceiptNumber *string   `json:"receipt_number"`
	Verification  *string   `json:"verification"`
	Amount        int64     `json:"amount"`
}

// P1.4 — Billing & Payment (PRD §23, WF-P1-006, AT-P1-012, DoD #30–33; TD-P1-007).
func TestBillingAndPayment(t *testing.T) {
	e := setup(t)
	tenA, tenB := e.setupTenants(t)
	fin := e.login("finance@demo.buildingvision.id")
	admin := e.login("admin@org-a.test")
	tech := e.login("budi@demo.buildingvision.id")

	// tenant A → tenant record (dibuat saat registrasi karena unit 1201 belum punya tenant)
	var me struct {
		Tenant *struct {
			ID uuid.UUID `json:"id"`
		} `json:"tenant"`
	}
	st, body := e.do(tenA, http.MethodGet, "/api/v1/tenant/me", nil)
	e.mustJSON(st, body, 200, &me)
	if me.Tenant == nil {
		t.Fatal("tenant A tanpa tenant record")
	}
	tenantID := me.Tenant.ID

	// ---- invoice draft → issue ----
	due := time.Now().Add(10 * 24 * time.Hour)
	var inv invoiceResp
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices", map[string]any{"property_id": e.refs.PropertyID, "tenant_id": tenantID, "unit_location_id": e.refs.UnitA1201, "invoice_type": "service_charge", "period_start": "2026-09-01", "period_end": "2026-09-30", "due_at": due, "tax_amount": 250000,
		"items": []map[string]any{{"description": "Service charge September", "quantity": 180, "unit": "m²", "unit_price": 12500}, {"description": "Parkir", "quantity": 1, "unit_price": 250000}}})
	e.mustJSON(st, body, 201, &inv)
	if !strings.HasPrefix(inv.InvoiceNumber, "INV-") || inv.Status != "draft" || inv.TotalAmount != 180*12500+250000+250000 || len(inv.Items) != 2 || !has(inv.AllowedActions, "issue") {
		t.Fatalf("invoice draft: %+v", inv)
	}
	// technician tidak boleh melihat invoice; tenant belum melihat draft
	if st, _ := e.do(tech, http.MethodGet, "/api/v1/invoices", nil); st != 403 {
		t.Fatalf("technician invoices: %d", st)
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/invoices", nil)
	var tl struct {
		Data []invoiceResp `json:"data"`
	}
	e.mustJSON(st, body, 200, &tl)
	if len(tl.Data) != 0 {
		t.Fatalf("draft tidak boleh tampil ke tenant: %d", len(tl.Data))
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices/"+inv.ID.String()+"/issue", nil)
	e.mustJSON(st, body, 200, &inv)
	if inv.Status != "issued" || !has(inv.AllowedActions, "record_payment") {
		t.Fatalf("issue: %+v", inv)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "invoice_issued") {
		t.Fatalf("notifikasi invoice ke tenant tidak ada: %+v", ib.Data)
	}
	// tenant A melihat; tenant B tidak (isolasi)
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/invoices/summary", nil)
	var sum struct {
		OutstandingAmount int64 `json:"outstanding_amount"`
		UnpaidCount       int   `json:"unpaid_count"`
	}
	e.mustJSON(st, body, 200, &sum)
	if sum.UnpaidCount != 1 || sum.OutstandingAmount != inv.TotalAmount {
		t.Fatalf("summary: %+v", sum)
	}
	if st, _ := e.do(tenB, http.MethodGet, "/api/v1/tenant/invoices/"+inv.ID.String(), nil); st != 404 {
		t.Fatalf("invoice lintas tenant: %d", st)
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/invoices/"+inv.ID.String(), nil)
	var tinv invoiceResp
	e.mustJSON(st, body, 200, &tinv)
	if !has(tinv.AllowedActions, "pay") {
		t.Fatalf("tenant allowed pay: %v", tinv.AllowedActions)
	}

	// ---- pembayaran manual (transfer) diinisiasi tenant → pending → verifikasi Finance ----
	var pay paymentResp
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/invoices/"+inv.ID.String()+"/payments", map[string]any{"provider_code": "manual", "method": "transfer", "amount": 500000})
	e.mustJSON(st, body, 201, &pay)
	if !strings.HasPrefix(pay.PaymentNumber, "PAY-") || pay.Status != "pending" || pay.Instructions == nil {
		t.Fatalf("payment manual: %+v", pay)
	}
	// dua pembayaran pending sekaligus ditolak
	if st, body := e.do(tenA, http.MethodPost, "/api/v1/tenant/invoices/"+inv.ID.String()+"/payments", map[string]any{"provider_code": "manual"}); st != 409 || !strings.Contains(string(body), "PAYMENT_PENDING") {
		t.Fatalf("pending ganda harus 409: %d %s", st, body)
	}
	// technician tidak boleh verifikasi
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/payments/"+pay.ID.String()+"/verify", nil); st != 403 {
		t.Fatalf("technician verify: %d", st)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/payments/"+pay.ID.String()+"/verify", map[string]any{"notes": "Bukti transfer BCA"})
	e.mustJSON(st, body, 200, &pay)
	if pay.Status != "paid" || pay.Verification == nil || *pay.Verification != "staff_manual" || pay.ReceiptNumber == nil {
		t.Fatalf("verify manual: %+v", pay)
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/invoices/"+inv.ID.String(), nil)
	e.mustJSON(st, body, 200, &inv)
	if inv.Status != "partially_paid" || inv.PaidAmount != 500000 {
		t.Fatalf("invoice setelah bayar sebagian: %+v", inv)
	}

	// ---- gateway (mock) dengan callback HMAC (AT-P1-012 / DoD #32–33) ----
	secret := "test-secret-123"
	st, body = e.do(admin, http.MethodPut, "/api/v1/payment-providers/mock_gateway", map[string]any{"is_active": true, "webhook_secret": secret, "methods": []string{"va", "qris"}})
	e.mustJSON(st, body, 200, nil)
	if strings.Contains(string(body), secret) {
		t.Fatal("webhook_secret tidak boleh dikembalikan API")
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/invoices/"+inv.ID.String()+"/payments", map[string]any{"provider_code": "mock_gateway", "method": "va"})
	e.mustJSON(st, body, 201, &pay)
	if pay.Status != "pending" || pay.ProviderRef == nil || pay.VANumber == nil || pay.Amount != inv.TotalAmount-500000 {
		t.Fatalf("payment gateway: %+v", pay)
	}
	webhook := func(sig bool, payload map[string]any) (int, []byte) {
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/v1/webhooks/payments/mock_gateway", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if sig {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(raw)
			req.Header.Set("X-BV-Signature", hex.EncodeToString(mac.Sum(nil)))
		} else {
			req.Header.Set("X-BV-Signature", "deadbeef")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	// signature salah → 401, payment tetap pending
	if st, body := webhook(false, map[string]any{"event_id": "evt-bad", "provider_ref": *pay.ProviderRef, "status": "paid", "amount": pay.Amount}); st != 401 {
		t.Fatalf("webhook signature salah harus 401: %d %s", st, body)
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/payments/"+pay.ID.String(), nil)
	e.mustJSON(st, body, 200, &pay)
	if pay.Status != "pending" {
		t.Fatalf("payment harus tetap pending setelah callback tidak valid: %s", pay.Status)
	}
	// nominal tidak sesuai → tidak paid
	if st, body := webhook(true, map[string]any{"event_id": "evt-mismatch", "provider_ref": *pay.ProviderRef, "status": "paid", "amount": 1}); st != 200 || !strings.Contains(string(body), "amount_mismatch") {
		t.Fatalf("amount mismatch: %d %s", st, body)
	}
	// callback valid → paid; invoice paid; receipt
	st, body = webhook(true, map[string]any{"event_id": "evt-ok-1", "provider_ref": *pay.ProviderRef, "status": "paid", "amount": pay.Amount, "paid_at": time.Now().UTC().Format(time.RFC3339)})
	if st != 200 || !strings.Contains(string(body), `"status":"paid"`) {
		t.Fatalf("webhook valid: %d %s", st, body)
	}
	// duplikat event → idempoten
	st, body = webhook(true, map[string]any{"event_id": "evt-ok-1", "provider_ref": *pay.ProviderRef, "status": "paid", "amount": pay.Amount})
	if st != 200 || !strings.Contains(string(body), `"duplicate":true`) {
		t.Fatalf("webhook duplikat: %d %s", st, body)
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/payments/"+pay.ID.String(), nil)
	e.mustJSON(st, body, 200, &pay)
	if pay.Status != "paid" || pay.Verification == nil || *pay.Verification != "gateway_callback" || pay.ReceiptNumber == nil {
		t.Fatalf("payment setelah callback: %+v", pay)
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/invoices/"+inv.ID.String(), nil)
	e.mustJSON(st, body, 200, &tinv)
	if tinv.Status != "paid" || tinv.Outstanding != 0 || has(tinv.AllowedActions, "pay") {
		t.Fatalf("invoice lunas: %+v", tinv)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "payment_received") || !hasType(ib, "invoice_paid") {
		t.Fatalf("notifikasi pembayaran ke tenant tidak ada: %+v", ib.Data)
	}
	if ib := e.inboxOf(t, fin); !hasType(ib, "payment_received") {
		t.Fatalf("notifikasi pembayaran ke Finance tidak ada: %+v", ib.Data)
	}
	// invoice lunas tidak bisa dibayar lagi
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/tenant/invoices/"+inv.ID.String()+"/payments", map[string]any{"provider_code": "manual"}); st != 409 {
		t.Fatalf("bayar invoice lunas harus 409: %d", st)
	}
	// summary kembali nol; riwayat pembayaran tenant 2 (1 manual + 1 gateway)
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/invoices/summary", nil)
	e.mustJSON(st, body, 200, &sum)
	if sum.UnpaidCount != 0 {
		t.Fatalf("summary setelah lunas: %+v", sum)
	}
	var pays struct {
		Data []paymentResp `json:"data"`
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/payments", nil)
	e.mustJSON(st, body, 200, &pays)
	if len(pays.Data) != 2 {
		t.Fatalf("riwayat pembayaran: %d", len(pays.Data))
	}
	// ---- pencatatan manual oleh staf + cancel invoice draft ----
	var inv2 invoiceResp
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices", map[string]any{"property_id": e.refs.PropertyID, "tenant_id": tenantID, "invoice_type": "utility", "due_at": due, "issue_now": true, "items": []map[string]any{{"description": "Listrik", "quantity": 120, "unit": "kWh", "unit_price": 1500}}})
	e.mustJSON(st, body, 201, &inv2)
	if inv2.Status != "issued" {
		t.Fatalf("issue_now: %s", inv2.Status)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices/"+inv2.ID.String()+"/payments", map[string]any{"amount": 180000, "method": "cash"})
	e.mustJSON(st, body, 201, &pay)
	if pay.Status != "paid" {
		t.Fatalf("record manual: %+v", pay)
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/invoices/"+inv2.ID.String(), nil)
	e.mustJSON(st, body, 200, &inv2)
	if inv2.Status != "paid" {
		t.Fatalf("invoice utility: %s", inv2.Status)
	}
	// sweep overdue: invoice due kemarin → overdue + notifikasi
	var inv3 invoiceResp
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices", map[string]any{"property_id": e.refs.PropertyID, "tenant_id": tenantID, "due_at": time.Now().Add(-24 * time.Hour), "issue_now": true, "items": []map[string]any{{"description": "Denda", "unit_price": 50000}}})
	e.mustJSON(st, body, 201, &inv3)
	if err := e.app.Billing.Sweep(t.Context(), e.refs.OrgID); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/invoices/"+inv3.ID.String(), nil)
	e.mustJSON(st, body, 200, &inv3)
	if inv3.Status != "overdue" {
		t.Fatalf("overdue sweep: %s", inv3.Status)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "invoice_overdue") {
		t.Fatalf("notifikasi overdue ke tenant tidak ada")
	}
}
