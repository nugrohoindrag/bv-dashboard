package finance

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
)

// ---------- Webhook keluar (PRD P4 v2.1 P4-INT-04, D-P4-03) ----------
// Event domain keuangan dari outbox (domain_event.dispatch) → webhook_deliveries per endpoint aktif yang berlangganan →
// worker mengirim POST JSON bertanda tangan HMAC-SHA256 (X-BV-Signature: v1=hex(hmac(secret, timestamp + "." + body)))
// dengan retry backoff eksponensial (maks. 8 percobaan).

var FinanceEvents = []string{"invoice.issued", "invoice.paid", "invoice.overdue", "invoice.cancelled", "invoice.reminder", "payment.paid", "payment.refunded",
	"credit_note.approved", "billing_run.generated"}

const maxAttempts = 8

type Endpoint struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	URL            string     `json:"url"`
	EventTypes     []string   `json:"event_types"`
	IsActive       bool       `json:"is_active"`
	SecretHint     string     `json:"secret_hint"`
	Secret         string     `json:"secret,omitempty"` // hanya saat dibuat / rotasi
	LastDeliveryAt *time.Time `json:"last_delivery_at"`
	LastStatus     *string    `json:"last_status"`
	PendingCount   int        `json:"pending_count"`
	FailedCount    int        `json:"failed_count"`
	CreatedAt      time.Time  `json:"created_at"`
	Version        int        `json:"version"`
}

const endpointSelect = `SELECT e.id, e.name, e.url, e.event_types, e.is_active, right(e.secret, 4), e.last_delivery_at, e.last_status,
	(SELECT count(*) FROM webhook_deliveries d WHERE d.endpoint_id = e.id AND d.status = 'pending'), (SELECT count(*) FROM webhook_deliveries d WHERE d.endpoint_id = e.id AND d.status = 'failed'),
	e.created_at, e.version FROM webhook_endpoints e`

func scanEndpoint(row pgx.Row) (*Endpoint, error) {
	var e Endpoint
	if err := row.Scan(&e.ID, &e.Name, &e.URL, &e.EventTypes, &e.IsActive, &e.SecretHint, &e.LastDeliveryAt, &e.LastStatus, &e.PendingCount, &e.FailedCount, &e.CreatedAt, &e.Version); err != nil {
		return nil, err
	}
	if e.EventTypes == nil {
		e.EventTypes = []string{}
	}
	e.SecretHint = "••••" + e.SecretHint
	return &e, nil
}

func requireOrgManage(ctx context.Context) error {
	if _, all := authctx.Must(ctx).PropertyIDsFor("billing.accounting.manage"); !all {
		return apperr.Forbidden("Webhook keluar memerlukan billing.accounting.manage tingkat organization")
	}
	return nil
}

func (s *Service) validateURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return apperr.Validation("url tidak valid").WithField("url", "tidak valid")
	}
	if s.AllowInsecureWebhooks {
		return nil
	}
	if u.Scheme != "https" {
		return apperr.Validation("url harus https").WithField("url", "harus https")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return apperr.Validation("url tidak boleh mengarah ke jaringan internal").WithField("url", "host internal")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return apperr.Validation("url tidak boleh mengarah ke jaringan internal").WithField("url", "IP internal")
	}
	return nil
}

func newSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "whsec_" + hex.EncodeToString(b)
}

func (s *Service) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	if _, all := authctx.Must(ctx).PropertyIDsFor("billing.accounting.view"); !all {
		return nil, apperr.Forbidden("Memerlukan billing.accounting.view tingkat organization")
	}
	out := []Endpoint{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, endpointSelect+` ORDER BY e.created_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEndpoint(rows)
			if err != nil {
				return err
			}
			out = append(out, *e)
		}
		return rows.Err()
	})
	return out, err
}

type EndpointInput struct {
	Name         *string   `json:"name"`
	URL          *string   `json:"url"`
	EventTypes   *[]string `json:"event_types"`
	IsActive     *bool     `json:"is_active"`
	RotateSecret bool      `json:"rotate_secret"`
}

func (s *Service) SaveEndpoint(ctx context.Context, id *uuid.UUID, in EndpointInput) (*Endpoint, error) {
	if err := requireOrgManage(ctx); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	if in.URL != nil {
		if err := s.validateURL(*in.URL); err != nil {
			return nil, err
		}
	}
	if in.EventTypes != nil {
		for _, t := range *in.EventTypes {
			if !has(FinanceEvents, t) {
				return nil, apperr.Validation("event_types: "+t+" tidak didukung").WithField("event_types", t)
			}
		}
	}
	var out *Endpoint
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		secret := ""
		var eid uuid.UUID
		if id == nil {
			if in.Name == nil || strings.TrimSpace(*in.Name) == "" || in.URL == nil {
				return apperr.Validation("name dan url wajib").WithField("url", "wajib")
			}
			types := []string{}
			if in.EventTypes != nil {
				types = *in.EventTypes
			}
			active := true
			if in.IsActive != nil {
				active = *in.IsActive
			}
			secret = newSecret()
			if err := tx.QueryRow(ctx, `INSERT INTO webhook_endpoints (organization_id, name, url, secret, event_types, is_active, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$7) RETURNING id`,
				p.OrganizationID, strings.TrimSpace(*in.Name), strings.TrimSpace(*in.URL), secret, types, active, p.UserID).Scan(&eid); err != nil {
				return err
			}
		} else {
			eid = *id
			if in.RotateSecret {
				secret = newSecret()
			}
			tag, err := tx.Exec(ctx, `UPDATE webhook_endpoints SET name = COALESCE(NULLIF(trim($2),''), name), url = COALESCE(NULLIF(trim($3),''), url), event_types = COALESCE($4, event_types),
				is_active = COALESCE($5, is_active), secret = COALESCE(NULLIF($6,''), secret), updated_by = $7 WHERE id = $1`, eid, in.Name, in.URL, in.EventTypes, in.IsActive, secret, p.UserID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return apperr.NotFound("Webhook")
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditConfigChange, EntityType: "webhook_endpoint", EntityID: &eid, EntityLabel: deref(in.Name), After: map[string]any{"url": in.URL, "event_types": in.EventTypes, "is_active": in.IsActive, "secret_rotated": secret != ""}})
		var err error
		out, err = scanEndpoint(tx.QueryRow(ctx, endpointSelect+` WHERE e.id = $1`, eid))
		if err == nil {
			out.Secret = secret
		}
		return err
	})
	return out, err
}

func (s *Service) DeleteEndpoint(ctx context.Context, id uuid.UUID) error {
	if err := requireOrgManage(ctx); err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM webhook_endpoints WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Webhook")
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "webhook_endpoint", EntityID: &id})
		return nil
	})
}

type Delivery struct {
	ID            uuid.UUID      `json:"id"`
	EndpointID    uuid.UUID      `json:"endpoint_id"`
	EventType     string         `json:"event_type"`
	ObjectType    *string        `json:"object_type"`
	ObjectID      *uuid.UUID     `json:"object_id"`
	Status        string         `json:"status"`
	Attempts      int            `json:"attempts"`
	NextAttemptAt time.Time      `json:"next_attempt_at"`
	ResponseCode  *int           `json:"response_code"`
	Error         *string        `json:"error"`
	CreatedAt     time.Time      `json:"created_at"`
	DeliveredAt   *time.Time     `json:"delivered_at"`
	Payload       map[string]any `json:"payload,omitempty"`
}

func (s *Service) ListDeliveries(ctx context.Context, endpointID uuid.UUID, status string, page httpx.Page) ([]Delivery, *string, error) {
	if _, all := authctx.Must(ctx).PropertyIDsFor("billing.accounting.view"); !all {
		return nil, nil, apperr.Forbidden("Memerlukan billing.accounting.view tingkat organization")
	}
	var out []Delivery
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{endpointID}
		where := " WHERE endpoint_id = $1"
		if status != "" {
			args = append(args, status)
			where += fmt.Sprintf(" AND status = $%d", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (created_at, id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, `SELECT id, endpoint_id, event_type, object_type, object_id, status, attempts, next_attempt_at, response_code, error, created_at, delivered_at, payload FROM webhook_deliveries`+where+
			fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d Delivery
			var payload []byte
			if err := rows.Scan(&d.ID, &d.EndpointID, &d.EventType, &d.ObjectType, &d.ObjectID, &d.Status, &d.Attempts, &d.NextAttemptAt, &d.ResponseCode, &d.Error, &d.CreatedAt, &d.DeliveredAt, &payload); err != nil {
				return err
			}
			_ = json.Unmarshal(payload, &d.Payload)
			out = append(out, d)
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
		out = []Delivery{}
	}
	return out, next, err
}

// RetryDelivery: kirim ulang delivery gagal (reset percobaan).
func (s *Service) RetryDelivery(ctx context.Context, id uuid.UUID) error {
	if err := requireOrgManage(ctx); err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET status = 'pending', attempts = 0, next_attempt_at = now(), error = NULL WHERE id = $1 AND status <> 'delivered'`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.InvalidTransition("Delivery sudah terkirim atau tidak ditemukan")
		}
		return nil
	})
}

// TestEndpoint: antrekan event webhook.test ke endpoint (dikirim sweep berikutnya / langsung bila deliverNow).
func (s *Service) TestEndpoint(ctx context.Context, id uuid.UUID) (*Delivery, error) {
	if err := requireOrgManage(ctx); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	var did uuid.UUID
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM webhook_endpoints WHERE id = $1)`, id).Scan(&exists)
		if !exists {
			return apperr.NotFound("Webhook")
		}
		payload, _ := json.Marshal(map[string]any{"type": "webhook.test", "occurred_at": time.Now().UTC(), "organization_id": p.OrganizationID, "data": map[string]any{"message": "Tes webhook BuildingVision"}})
		return tx.QueryRow(ctx, `INSERT INTO webhook_deliveries (organization_id, endpoint_id, event_type, payload) VALUES ($1,$2,'webhook.test',$3) RETURNING id`, p.OrganizationID, id, payload).Scan(&did)
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.DeliverSweep(ctx, p.OrganizationID); err != nil {
		return nil, err
	}
	var out *Delivery
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var d Delivery
		if err := tx.QueryRow(ctx, `SELECT id, endpoint_id, event_type, status, attempts, next_attempt_at, response_code, error, created_at, delivered_at FROM webhook_deliveries WHERE id = $1`, did).
			Scan(&d.ID, &d.EndpointID, &d.EventType, &d.Status, &d.Attempts, &d.NextAttemptAt, &d.ResponseCode, &d.Error, &d.CreatedAt, &d.DeliveredAt); err != nil {
			return err
		}
		out = &d
		return nil
	})
	return out, err
}

// ---------- subscriber (domain_event.dispatch) ----------

func (s *Service) Name() string { return "finance_webhooks" }

// Handle: event keuangan → delivery untuk setiap endpoint aktif yang berlangganan.
func (s *Service) Handle(ctx context.Context, ev events.Event) error {
	if !has(FinanceEvents, ev.Type) {
		return nil
	}
	ctx = authctx.With(ctx, authctx.System(ev.OrganizationID))
	return s.DB.WithOrgTx(ctx, ev.OrganizationID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM webhook_endpoints WHERE is_active AND (cardinality(event_types) = 0 OR $1 = ANY(event_types))`, ev.Type)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		if len(ids) == 0 {
			return nil
		}
		data := map[string]any{}
		for k, v := range ev.Payload {
			if k != "domain" && k != "tenant_user_id" {
				data[k] = v
			}
		}
		if snap := snapshotTx(ctx, tx, ev.ObjectType, ev.ObjectID); snap != nil {
			data["object"] = snap
		}
		for _, id := range ids {
			did := uuid.Must(uuid.NewV7())
			payload, _ := json.Marshal(map[string]any{"id": did, "type": ev.Type, "occurred_at": ev.OccurredAt, "organization_id": ev.OrganizationID, "property_id": ev.PropertyID,
				"object_type": ev.ObjectType, "object_id": ev.ObjectID, "object_label": ev.ObjectLabel, "data": data})
			if _, err := tx.Exec(ctx, `INSERT INTO webhook_deliveries (id, organization_id, endpoint_id, event_type, object_type, object_id, payload) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
				did, ev.OrganizationID, id, ev.Type, ev.ObjectType, ev.ObjectID, payload); err != nil {
				return err
			}
		}
		return nil
	})
}

// snapshotTx: ringkasan object saat event (tanpa data pribadi berlebih) untuk integrasi akuntansi.
func snapshotTx(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID) map[string]any {
	var q string
	switch objectType {
	case "invoice":
		q = `SELECT json_build_object('invoice_number', i.invoice_number, 'invoice_type', i.invoice_type, 'status', i.status, 'currency_code', trim(i.currency_code),
			'subtotal_amount', i.subtotal_amount, 'tax_amount', i.tax_amount, 'total_amount', i.total_amount, 'paid_amount', i.paid_amount, 'credited_amount', i.credited_amount,
			'outstanding_amount', i.total_amount - i.paid_amount - i.credited_amount, 'issued_at', i.issued_at, 'due_date', i.due_date, 'external_ref', i.external_ref,
			'property', pl.name, 'tenant_code', t.tenant_code, 'tenant_name', t.name, 'unit', un.unit_number, 'period_start', i.period_start, 'period_end', i.period_end)
			FROM invoices i JOIN locations pl ON pl.id = i.property_id LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN units un ON un.location_id = i.unit_location_id WHERE i.id = $1`
	case "payment":
		q = `SELECT json_build_object('payment_number', p.payment_number, 'receipt_number', p.receipt_number, 'receipt_group', p.receipt_group, 'status', p.status, 'method', p.method,
			'amount', p.amount, 'currency_code', trim(p.currency_code), 'paid_at', p.paid_at, 'refunded_at', p.refunded_at, 'reference', p.reference, 'external_ref', p.external_ref,
			'invoice_number', i.invoice_number, 'invoice_external_ref', i.external_ref, 'property', pl.name, 'tenant_code', t.tenant_code, 'tenant_name', t.name)
			FROM payments p JOIN invoices i ON i.id = p.invoice_id JOIN locations pl ON pl.id = p.property_id LEFT JOIN tenants t ON t.id = i.tenant_id WHERE p.id = $1`
	case "credit_note":
		q = `SELECT json_build_object('credit_note_number', c.credit_note_number, 'status', c.status, 'amount', c.amount, 'reason', c.reason, 'decided_at', c.decided_at,
			'invoice_number', i.invoice_number, 'invoice_external_ref', i.external_ref, 'property', pl.name)
			FROM credit_notes c JOIN invoices i ON i.id = c.invoice_id JOIN locations pl ON pl.id = c.property_id WHERE c.id = $1`
	case "billing_run":
		q = `SELECT json_build_object('run_number', r.run_number, 'status', r.status, 'period_start', r.period_start, 'period_end', r.period_end, 'invoice_count', r.invoice_count,
			'total_amount', r.total_amount, 'property', pl.name) FROM billing_runs r JOIN locations pl ON pl.id = r.property_id WHERE r.id = $1`
	default:
		return nil
	}
	var raw []byte
	if err := tx.QueryRow(ctx, q, id).Scan(&raw); err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

// ---------- pengiriman (worker) ----------

// Sign: tanda tangan webhook keluar (didokumentasikan untuk penerima).
func Sign(secret string, ts int64, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	m.Write(body)
	return "v1=" + hex.EncodeToString(m.Sum(nil))
}

func backoff(attempt int) time.Duration {
	d := time.Minute << uint(attempt-1) // 1, 2, 4, … menit
	if d > 12*time.Hour {
		d = 12 * time.Hour
	}
	return d
}

// DeliverSweep: kirim delivery pending yang jatuh tempo (maks. 50 per putaran per organization).
func (s *Service) DeliverSweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	type job struct {
		id, endpoint uuid.UUID
		url, secret  string
		event        string
		payload      []byte
		attempts     int
	}
	var jobs []job
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT d.id, d.endpoint_id, e.url, e.secret, d.event_type, d.payload, d.attempts FROM webhook_deliveries d JOIN webhook_endpoints e ON e.id = d.endpoint_id
			WHERE d.status = 'pending' AND d.next_attempt_at <= now() AND (e.is_active OR d.event_type = 'webhook.test') ORDER BY d.created_at LIMIT 50 FOR UPDATE OF d SKIP LOCKED`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var j job
			if rows.Scan(&j.id, &j.endpoint, &j.url, &j.secret, &j.event, &j.payload, &j.attempts) == nil {
				jobs = append(jobs, j)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		// klaim: jadwalkan ulang agar sweep paralel tidak mengirim ganda
		for _, j := range jobs {
			if _, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET next_attempt_at = now() + interval '5 minutes' WHERE id = $1`, j.id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil || len(jobs) == 0 {
		return 0, err
	}
	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if !s.AllowInsecureWebhooks {
		client = safeClient(client.Timeout)
	}
	n := 0
	for _, j := range jobs {
		ts := time.Now().Unix()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.url, bytes.NewReader(j.payload))
		var code int
		var derr string
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "BuildingVision-Webhooks/1.0")
			req.Header.Set("X-BV-Event", j.event)
			req.Header.Set("X-BV-Delivery", j.id.String())
			req.Header.Set("X-BV-Timestamp", strconv.FormatInt(ts, 10))
			req.Header.Set("X-BV-Signature", Sign(j.secret, ts, j.payload))
			resp, err := client.Do(req)
			if err != nil {
				derr = err.Error()
			} else {
				code = resp.StatusCode
				_ = resp.Body.Close()
				if code < 200 || code >= 300 {
					derr = "HTTP " + strconv.Itoa(code)
				}
			}
		} else {
			derr = err.Error()
		}
		attempts := j.attempts + 1
		_ = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
			var codeArg *int
			if code != 0 {
				codeArg = &code
			}
			if derr == "" {
				n++
				_, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET status = 'delivered', attempts = $2, response_code = $3, error = NULL, delivered_at = now() WHERE id = $1`, j.id, attempts, codeArg)
				_, _ = tx.Exec(ctx, `UPDATE webhook_endpoints SET last_delivery_at = now(), last_status = 'delivered' WHERE id = $1`, j.endpoint)
				return err
			}
			if len(derr) > 500 {
				derr = derr[:500]
			}
			st := "pending"
			if attempts >= maxAttempts || j.event == "webhook.test" {
				st = "failed"
			}
			_, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET status = $2, attempts = $3, response_code = $4, error = $5, next_attempt_at = now() + $6::interval WHERE id = $1`,
				j.id, st, attempts, codeArg, derr, fmt.Sprintf("%d seconds", int(backoff(attempts).Seconds())))
			_, _ = tx.Exec(ctx, `UPDATE webhook_endpoints SET last_delivery_at = now(), last_status = $2 WHERE id = $1`, j.endpoint, "error: "+derr)
			return err
		})
	}
	return n, nil
}

// safeClient: klien produksi yang menolak koneksi ke alamat internal pada saat dial (setelah resolusi DNS) — mencegah SSRF
// lewat hostname yang me-resolve ke IP privat/loopback (DNS rebinding); redirect tidak diikuti.
func safeClient(timeout time.Duration) *http.Client {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return fmt.Errorf("alamat tujuan webhook tidak diizinkan: %s", host)
		}
		return nil
	}}
	tr := &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: timeout, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second}
	return &http.Client{Timeout: timeout, Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
