package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/platform/apperr"
)

// Provider: abstraksi payment gateway (TD-P1-007; OD-P1-009). BuildingVision tidak pernah menyimpan kredensial
// pembayaran (guardrail #10); status akhir hanya dari callback terverifikasi (PRD §23, AT-P1-012).
type Provider interface {
	Code() string
	// CreateCheckout membuat transaksi di sisi provider dan mengembalikan instruksi bayar untuk tenant.
	CreateCheckout(ctx context.Context, req CheckoutRequest) (*CheckoutResult, error)
	// VerifyWebhook memvalidasi tanda tangan callback dan menormalkan event. body sudah dibaca oleh handler.
	VerifyWebhook(r *http.Request, body []byte, secret string) (*WebhookEvent, error)
}

type CheckoutRequest struct {
	PaymentID     uuid.UUID
	PaymentNumber string
	InvoiceNumber string
	Amount        int64
	Currency      string
	Method        string
	PayerName     string
	PayerEmail    string
	Config        map[string]any
	PublicURL     string
}

type CheckoutResult struct {
	ProviderRef  string
	CheckoutURL  string
	VANumber     string
	QRString     string
	Instructions string
	ExpiresAt    *time.Time
	Status       string // initiated | pending | paid (manual/cash langsung paid oleh staf)
}

// WebhookEvent: bentuk ternormalisasi dari callback provider.
type WebhookEvent struct {
	ExternalID  string // id event/transaksi di provider (idempotensi)
	ProviderRef string // referensi transaksi yang dibuat saat checkout
	Status      string // paid | failed | expired | cancelled | refunded
	Amount      int64
	PaidAt      *time.Time
	Raw         map[string]any
}

var registry = map[string]Provider{}

// Register mendaftarkan adapter provider (dipanggil saat init; adapter nyata ditambah tanpa mengubah alur).
func Register(p Provider) { registry[p.Code()] = p }

func Get(code string) (Provider, bool) {
	p, ok := registry[code]
	return p, ok
}

func init() {
	Register(manualProvider{})
	Register(mockGateway{})
}

// ---------- manual: transfer bank / tunai, diverifikasi staf (billing.payments.verify) ----------

type manualProvider struct{}

func (manualProvider) Code() string { return "manual" }

func (manualProvider) CreateCheckout(_ context.Context, req CheckoutRequest) (*CheckoutResult, error) {
	instr := "Transfer ke rekening building management, lalu unggah/berikan bukti ke Tenant Relation untuk verifikasi."
	if v, ok := req.Config["instructions"].(string); ok && v != "" {
		instr = v
	}
	if bank, ok := req.Config["bank_account"].(string); ok && bank != "" {
		instr += "\nRekening: " + bank
	}
	exp := time.Now().Add(72 * time.Hour)
	return &CheckoutResult{ProviderRef: "MAN-" + req.PaymentNumber, Instructions: instr + "\nBerita transfer: " + req.PaymentNumber, ExpiresAt: &exp, Status: "pending"}, nil
}

func (manualProvider) VerifyWebhook(*http.Request, []byte, string) (*WebhookEvent, error) {
	return nil, apperr.Validation("provider manual tidak menerima webhook")
}

// ---------- mock_gateway: simulasi gateway dengan webhook HMAC-SHA256 (X-BV-Signature) ----------

type mockGateway struct{}

func (mockGateway) Code() string { return "mock_gateway" }

func (mockGateway) CreateCheckout(_ context.Context, req CheckoutRequest) (*CheckoutResult, error) {
	ref := "MOCK-" + strings.ReplaceAll(req.PaymentID.String(), "-", "")[:16]
	exp := time.Now().Add(24 * time.Hour)
	res := &CheckoutResult{ProviderRef: ref, CheckoutURL: strings.TrimRight(req.PublicURL, "/") + "/pay/mock/" + ref, ExpiresAt: &exp, Status: "pending"}
	switch req.Method {
	case "va":
		res.VANumber = fmt.Sprintf("8808%012d", req.Amount%1000000000000)
		res.Instructions = "Bayar melalui Virtual Account " + res.VANumber + " sebelum " + exp.Format("02 Jan 2006 15:04")
	case "qris":
		res.QRString = "00020101021226mock" + ref
		res.Instructions = "Scan QRIS di aplikasi pembayaran Anda."
	default:
		res.Instructions = "Lanjutkan pembayaran melalui halaman checkout."
	}
	return res, nil
}

// Signature: hex(HMAC-SHA256(secret, rawBody)) di header X-BV-Signature. Body: {"event_id","provider_ref","status","amount","paid_at"}.
func (mockGateway) VerifyWebhook(r *http.Request, body []byte, secret string) (*WebhookEvent, error) {
	if secret == "" {
		return nil, apperr.New(503, "PROVIDER_NOT_CONFIGURED", "Provider not configured", "webhook_secret belum diatur")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	got := strings.TrimSpace(r.Header.Get("X-BV-Signature"))
	if !hmac.Equal([]byte(strings.ToLower(got)), []byte(want)) {
		return nil, apperr.New(401, "WEBHOOK_SIGNATURE_INVALID", "Invalid signature", "Tanda tangan webhook tidak valid")
	}
	var raw struct {
		EventID     string  `json:"event_id"`
		ProviderRef string  `json:"provider_ref"`
		Status      string  `json:"status"`
		Amount      int64   `json:"amount"`
		PaidAt      *string `json:"paid_at"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || raw.EventID == "" || raw.ProviderRef == "" {
		return nil, apperr.Validation("payload webhook tidak valid")
	}
	ev := &WebhookEvent{ExternalID: raw.EventID, ProviderRef: raw.ProviderRef, Status: strings.ToLower(raw.Status), Amount: raw.Amount}
	if raw.PaidAt != nil {
		if t, err := time.Parse(time.RFC3339, *raw.PaidAt); err == nil {
			ev.PaidAt = &t
		}
	}
	_ = json.Unmarshal(body, &ev.Raw)
	return ev, nil
}
