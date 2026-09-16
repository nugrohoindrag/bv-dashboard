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
}

func New(d *db.DB, j jobs.Enqueuer, prof *profile.Service, publicURL string) *Service {
	return &Service{DB: d, Jobs: j, Profile: prof, PublicURL: publicURL}
}

// ---------- Invoice ----------

type Item struct {
	ID          uuid.UUID `json:"id,omitempty"`
	Description string    `json:"description"`
	Quantity    float64   `json:"quantity"`
	Unit        *string   `json:"unit"`
	UnitPrice   int64     `json:"unit_price"`
	Amount      int64     `json:"amount"`
}

type Invoice struct {
	ID             uuid.UUID  `json:"id"`
	InvoiceNumber  string     `json:"invoice_number"`
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
	TotalAmount    int64      `json:"total_amount"`
	PaidAmount     int64      `json:"paid_amount"`
	OutstandingAmt int64      `json:"outstanding_amount"`
	IssuedAt       *time.Time `json:"issued_at"`
	DueAt          time.Time  `json:"due_at"`
	PaidAt         *time.Time `json:"paid_at"`
	Status         string     `json:"status"`
	Source         string     `json:"source"`
	ExternalRef    *string    `json:"external_ref"`
	Notes          *string    `json:"notes"`
	CancelReason   *string    `json:"cancel_reason"`
	Items          []Item     `json:"items"`
	PaymentCount   int        `json:"payment_count"`
	CreatedAt      time.Time  `json:"created_at"`
	CreatedByName  *string    `json:"created_by_name"`
	AllowedActions []string   `json:"allowed_actions"`
	Version        int        `json:"version"`
}

const invSelect = `SELECT i.id, i.invoice_number, i.property_id, i.tenant_id, t.name, i.unit_location_id, COALESCE('Unit ' || u.unit_number, l.name), i.invoice_type, i.period_start, i.period_end, i.description,
	i.currency_code, i.subtotal_amount, i.tax_amount, i.total_amount, i.paid_amount, i.issued_at, i.due_at, i.paid_at, i.status, i.source, i.external_ref, i.notes, i.cancel_reason,
	(SELECT count(*) FROM payments p WHERE p.invoice_id = i.id AND p.status = 'paid'), i.created_at, cb.full_name, i.version
	FROM invoices i LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN locations l ON l.id = i.unit_location_id LEFT JOIN units u ON u.location_id = l.id LEFT JOIN users cb ON cb.id = i.created_by`

func scanInvoice(row pgx.Row) (*Invoice, error) {
	var v Invoice
	if err := row.Scan(&v.ID, &v.InvoiceNumber, &v.PropertyID, &v.TenantID, &v.TenantName, &v.UnitLocationID, &v.UnitLabel, &v.InvoiceType, &v.PeriodStart, &v.PeriodEnd, &v.Description,
		&v.CurrencyCode, &v.SubtotalAmount, &v.TaxAmount, &v.TotalAmount, &v.PaidAmount, &v.IssuedAt, &v.DueAt, &v.PaidAt, &v.Status, &v.Source, &v.ExternalRef, &v.Notes, &v.CancelReason,
		&v.PaymentCount, &v.CreatedAt, &v.CreatedByName, &v.Version); err != nil {
		return nil, err
	}
	v.CurrencyCode = strings.TrimSpace(v.CurrencyCode)
	v.OutstandingAmt = v.TotalAmount - v.PaidAmount
	v.Items = []Item{}
	return &v, nil
}

func loadItems(ctx context.Context, tx pgx.Tx, inv *Invoice) error {
	rows, err := tx.Query(ctx, `SELECT id, description, quantity, unit, unit_price, amount FROM invoice_items WHERE invoice_id = $1 ORDER BY sort_order`, inv.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Description, &it.Quantity, &it.Unit, &it.UnitPrice, &it.Amount); err != nil {
			return err
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
		if can("billing.payments.create") {
			inv.AllowedActions = append(inv.AllowedActions, "record_payment")
		}
		if can("billing.invoices.cancel") && inv.PaidAmount == 0 {
			inv.AllowedActions = append(inv.AllowedActions, "cancel")
		}
	}
}

func tenantInvoiceActions(inv *Invoice) {
	inv.AllowedActions = []string{"view"}
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
	TaxAmount      *int64     `json:"tax_amount"`
	DueAt          *time.Time `json:"due_at"`
	ExternalRef    *string    `json:"external_ref"`
	Notes          *string    `json:"notes"`
	Items          *[]Item    `json:"items"`
	IssueNow       bool       `json:"issue_now"`
}

var invoiceTypes = map[string]bool{"service_charge": true, "utility": true, "rental": true, "facility": true, "deposit": true, "other": true}

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

func sumItems(items []Item) (int64, error) {
	var sub int64
	for i := range items {
		if strings.TrimSpace(items[i].Description) == "" {
			return 0, apperr.Validation("item.description wajib")
		}
		if items[i].Quantity <= 0 {
			items[i].Quantity = 1
		}
		if items[i].Amount == 0 {
			items[i].Amount = int64(items[i].Quantity * float64(items[i].UnitPrice))
		}
		if items[i].Amount < 0 {
			return 0, apperr.Validation("item.amount tidak boleh negatif")
		}
		sub += items[i].Amount
	}
	return sub, nil
}

// CreateTx: dipakai staf & modul komersial (rental/hotel) — invoice draft atau langsung issued.
func (s *Service) CreateTx(ctx context.Context, tx pgx.Tx, in InvoiceInput, source string, sourceID *uuid.UUID) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil {
		return uuid.Nil, apperr.Validation("property_id wajib")
	}
	if err := s.Profile.RequireCapabilityTx(ctx, tx, *in.PropertyID, profile.CapBilling); err != nil {
		return uuid.Nil, err
	}
	it := deref(in.InvoiceType)
	if it == "" {
		it = "service_charge"
	}
	if !invoiceTypes[it] {
		return uuid.Nil, apperr.Validation("invoice_type tidak valid")
	}
	if in.DueAt == nil {
		return uuid.Nil, apperr.Validation("due_at wajib").WithField("due_at", "wajib")
	}
	if in.TenantID == nil && in.UnitLocationID == nil {
		return uuid.Nil, apperr.Validation("tenant_id atau unit_location_id wajib")
	}
	if in.TenantID != nil {
		var tpid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM tenants WHERE id = $1 AND deleted_at IS NULL`, *in.TenantID).Scan(&tpid); err != nil || tpid != *in.PropertyID {
			return uuid.Nil, apperr.Validation("tenant_id tidak ditemukan di property ini")
		}
	}
	if in.UnitLocationID != nil {
		pid, err := property.ResolvePropertyOfLocation(ctx, tx, *in.UnitLocationID)
		if err != nil || pid != *in.PropertyID {
			return uuid.Nil, apperr.Validation("unit_location_id tidak berada di property ini")
		}
	}
	var items []Item
	if in.Items != nil {
		items = *in.Items
	}
	if len(items) == 0 {
		return uuid.Nil, apperr.Validation("items minimal satu").WithField("items", "wajib")
	}
	sub, err := sumItems(items)
	if err != nil {
		return uuid.Nil, err
	}
	tax := int64(0)
	if in.TaxAmount != nil {
		tax = *in.TaxAmount
	}
	if tax < 0 {
		return uuid.Nil, apperr.Validation("tax_amount tidak boleh negatif")
	}
	ps, err := parseDate(in.PeriodStart)
	if err != nil {
		return uuid.Nil, err
	}
	pe, err := parseDate(in.PeriodEnd)
	if err != nil {
		return uuid.Nil, err
	}
	if source == "" {
		source = "manual"
	}
	loc := property.PropertyTimezone(ctx, tx, *in.PropertyID)
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixInvoice, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	status := "draft"
	var issuedAt *time.Time
	if in.IssueNow {
		status = "issued"
		now := time.Now().UTC()
		issuedAt = &now
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO invoices (organization_id, property_id, invoice_number, tenant_id, unit_location_id, invoice_type, period_start, period_end, description, subtotal_amount, tax_amount, total_amount, issued_at, due_at, status, source, source_id, external_ref, notes, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$20) RETURNING id`,
		p.OrganizationID, *in.PropertyID, number, in.TenantID, in.UnitLocationID, it, ps, pe, in.Description, sub, tax, sub+tax, issuedAt, *in.DueAt, status, source, sourceID, in.ExternalRef, in.Notes, actorOrNil(p)).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	for i, x := range items {
		if _, err := tx.Exec(ctx, `INSERT INTO invoice_items (organization_id, invoice_id, sort_order, description, quantity, unit, unit_price, amount) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, p.OrganizationID, id, i, strings.TrimSpace(x.Description), x.Quantity, x.Unit, x.UnitPrice, x.Amount); err != nil {
			return uuid.Nil, err
		}
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "total": sub + tax, "status": status, "source": source}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "invoice", EntityID: &id, EntityLabel: number})
	if status == "issued" {
		s.emitInvoice(ctx, tx, id, number, *in.PropertyID, EventInvoiceIssued, nil)
	}
	return id, nil
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
	PropertyID *uuid.UUID
	TenantID   *uuid.UUID
	Statuses   []string
	Type       string
	Q          string
	Overdue    bool
}

func (s *Service) List(ctx context.Context, f Filter, page httpx.Page) ([]Invoice, *string, error) {
	p := authctx.Must(ctx)
	var out []Invoice
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if f.PropertyID != nil {
			if err := iam.CanOnProperty(ctx, "billing.invoices.view", *f.PropertyID); err != nil {
				return err
			}
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND i.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("billing.invoices.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND i.property_id = ANY($%d)", len(args))
		}
		if f.TenantID != nil {
			args = append(args, *f.TenantID)
			where += fmt.Sprintf(" AND i.tenant_id = $%d", len(args))
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND i.status = ANY($%d)", len(args))
		}
		if f.Type != "" {
			args = append(args, f.Type)
			where += fmt.Sprintf(" AND i.invoice_type = $%d", len(args))
		}
		if f.Overdue {
			where += " AND i.status = 'overdue'"
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (i.invoice_number ILIKE $%d OR t.name ILIKE $%d OR i.description ILIKE $%d OR i.external_ref ILIKE $%d)", len(args), len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (i.created_at, i.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, invSelect+where+fmt.Sprintf(" ORDER BY i.created_at DESC, i.id DESC LIMIT %d", page.Limit+1), args...)
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
		if err := iam.CanOnProperty(ctx, "billing.invoices.view", inv.PropertyID); err != nil {
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

// Update: hanya draft.
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
			return apperr.Conflict("INVOICE_NOT_DRAFT", "Hanya invoice draft yang dapat diubah")
		}
		if ifVersion != nil && *ifVersion != inv.Version {
			return apperr.StaleVersion()
		}
		sub, tax := inv.SubtotalAmount, inv.TaxAmount
		if in.Items != nil {
			items := *in.Items
			if len(items) == 0 {
				return apperr.Validation("items minimal satu")
			}
			sub, err = sumItems(items)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM invoice_items WHERE invoice_id = $1`, id); err != nil {
				return err
			}
			for i, x := range items {
				if _, err := tx.Exec(ctx, `INSERT INTO invoice_items (organization_id, invoice_id, sort_order, description, quantity, unit, unit_price, amount) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, p.OrganizationID, id, i, strings.TrimSpace(x.Description), x.Quantity, x.Unit, x.UnitPrice, x.Amount); err != nil {
					return err
				}
			}
		}
		if in.TaxAmount != nil {
			tax = *in.TaxAmount
		}
		ps, err := parseDate(in.PeriodStart)
		if err != nil {
			return err
		}
		pe, err := parseDate(in.PeriodEnd)
		if err != nil {
			return err
		}
		if in.InvoiceType != nil && !invoiceTypes[*in.InvoiceType] {
			return apperr.Validation("invoice_type tidak valid")
		}
		if _, err := tx.Exec(ctx, `UPDATE invoices SET tenant_id = COALESCE($2, tenant_id), unit_location_id = COALESCE($3, unit_location_id), invoice_type = COALESCE($4, invoice_type), period_start = COALESCE($5, period_start), period_end = COALESCE($6, period_end),
			description = COALESCE($7, description), subtotal_amount = $8, tax_amount = $9, total_amount = $8 + $9, due_at = COALESCE($10, due_at), external_ref = COALESCE($11, external_ref), notes = COALESCE($12, notes), updated_by = $13 WHERE id = $1`,
			id, in.TenantID, in.UnitLocationID, in.InvoiceType, ps, pe, in.Description, sub, tax, in.DueAt, in.ExternalRef, in.Notes, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "invoice", EntityID: &id, EntityLabel: inv.InvoiceNumber, After: in})
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

// Act: issue | cancel.
func (s *Service) Act(ctx context.Context, id uuid.UUID, action string, in ActionInput) (*Invoice, error) {
	p := authctx.Must(ctx)
	var out *Invoice
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inv, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.invoices.view", inv.PropertyID); err != nil {
			return err
		}
		s.staffActions(ctx, inv)
		if !has(inv.AllowedActions, action) {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk invoice berstatus %s", action, inv.Status))
		}
		switch action {
		case "issue":
			if inv.TotalAmount <= 0 {
				return apperr.Validation("Total invoice harus > 0")
			}
			if _, err := tx.Exec(ctx, `UPDATE invoices SET status = 'issued', issued_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
			s.emitInvoice(ctx, tx, id, inv.InvoiceNumber, inv.PropertyID, EventInvoiceIssued, map[string]any{"total": inv.TotalAmount, "due_at": inv.DueAt})
		case "cancel":
			reason := strings.TrimSpace(in.Reason)
			if reason == "" {
				return apperr.Validation("reason wajib").WithField("reason", "wajib")
			}
			if _, err := tx.Exec(ctx, `UPDATE invoices SET status = 'cancelled', cancelled_at = now(), cancel_reason = $2, updated_by = $3 WHERE id = $1`, id, reason, p.UserID); err != nil {
				return err
			}
			_, _ = tx.Exec(ctx, `UPDATE payments SET status = 'cancelled', updated_by = $2 WHERE invoice_id = $1 AND status IN ('initiated','pending')`, id, p.UserID)
			s.emitInvoice(ctx, tx, id, inv.InvoiceNumber, inv.PropertyID, EventInvoiceCancelled, map[string]any{"reason": reason})
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: id, Action: audit.ActStatusChanged, From: inv.Status, To: map[string]string{"issue": "issued", "cancel": "cancelled"}[action], Payload: map[string]any{"action": action, "reason": in.Reason}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "invoice", EntityID: &id, EntityLabel: inv.InvoiceNumber, Before: map[string]any{"status": inv.Status}, After: map[string]any{"action": action}})
		out, err = s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s.staffActions(ctx, out)
		return nil
	})
	return out, err
}

// ---------- Payment ----------

type Payment struct {
	ID             uuid.UUID  `json:"id"`
	PaymentNumber  string     `json:"payment_number"`
	PropertyID     uuid.UUID  `json:"property_id"`
	InvoiceID      uuid.UUID  `json:"invoice_id"`
	InvoiceNumber  string     `json:"invoice_number"`
	TenantName     *string    `json:"tenant_name"`
	Amount         int64      `json:"amount"`
	CurrencyCode   string     `json:"currency_code"`
	ProviderCode   string     `json:"provider_code"`
	Method         string     `json:"method"`
	Status         string     `json:"status"`
	ProviderRef    *string    `json:"provider_ref"`
	CheckoutURL    *string    `json:"checkout_url"`
	VANumber       *string    `json:"va_number"`
	QRString       *string    `json:"qr_string"`
	Instructions   *string    `json:"instructions"`
	ExpiresAt      *time.Time `json:"expires_at"`
	PaidAt         *time.Time `json:"paid_at"`
	VerifiedAt     *time.Time `json:"verified_at"`
	Verification   *string    `json:"verification"`
	VerifiedBy     *string    `json:"verified_by_name"`
	ReceiptNumber  *string    `json:"receipt_number"`
	FailureReason  *string    `json:"failure_reason"`
	Notes          *string    `json:"notes"`
	CreatedAt      time.Time  `json:"created_at"`
	AllowedActions []string   `json:"allowed_actions"`
	Version        int        `json:"version"`
}

const paySelect = `SELECT p.id, p.payment_number, p.property_id, p.invoice_id, i.invoice_number, t.name, p.amount, p.currency_code, p.provider_code, p.method, p.status, p.provider_ref, p.checkout_url, p.va_number, p.qr_string, p.instructions,
	p.expires_at, p.paid_at, p.verified_at, p.verification, vb.full_name, p.receipt_number, p.failure_reason, p.notes, p.created_at, p.version
	FROM payments p JOIN invoices i ON i.id = p.invoice_id LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN users vb ON vb.id = p.verified_by`

func scanPayment(row pgx.Row) (*Payment, error) {
	var v Payment
	if err := row.Scan(&v.ID, &v.PaymentNumber, &v.PropertyID, &v.InvoiceID, &v.InvoiceNumber, &v.TenantName, &v.Amount, &v.CurrencyCode, &v.ProviderCode, &v.Method, &v.Status, &v.ProviderRef, &v.CheckoutURL, &v.VANumber, &v.QRString, &v.Instructions,
		&v.ExpiresAt, &v.PaidAt, &v.VerifiedAt, &v.Verification, &v.VerifiedBy, &v.ReceiptNumber, &v.FailureReason, &v.Notes, &v.CreatedAt, &v.Version); err != nil {
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

// UpsertProvider: konfigurasi provider (platform.organizations.update). Secret tidak pernah dikembalikan.
func (s *Service) UpsertProvider(ctx context.Context, code string, in ProviderInput) (*ProviderInfo, error) {
	p := authctx.Must(ctx)
	if _, ok := Get(code); !ok && code != "midtrans" && code != "xendit" {
		return nil, apperr.Validation("provider tidak dikenal")
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
	res, err := prov.CreateCheckout(ctx, CheckoutRequest{PaymentID: id, PaymentNumber: number, InvoiceNumber: inv.InvoiceNumber, Amount: amount, Currency: inv.CurrencyCode, Method: method, PayerName: payer, PayerEmail: payerEmail, Config: config, PublicURL: s.PublicURL})
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

// settleTx: tandai payment paid + update invoice (paid_amount, status) + event. verification: gateway_callback | staff_manual.
func (s *Service) settleTx(ctx context.Context, tx pgx.Tx, paymentID uuid.UUID, paidAt time.Time, verification string, verifiedBy *uuid.UUID, callback []byte) error {
	p := authctx.Must(ctx)
	var invID uuid.UUID
	var amount int64
	var status, number string
	var propertyID uuid.UUID
	var tenantUser *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT invoice_id, amount, status, payment_number, property_id, tenant_user_id FROM payments WHERE id = $1 FOR UPDATE`, paymentID).Scan(&invID, &amount, &status, &number, &propertyID, &tenantUser); err != nil {
		return apperr.NotFound("Payment")
	}
	if status == "paid" {
		return nil // idempotent
	}
	if status != "initiated" && status != "pending" {
		return apperr.Conflict("PAYMENT_NOT_SETTLEABLE", "Payment berstatus "+status)
	}
	if _, err := tx.Exec(ctx, `UPDATE payments SET status = 'paid', paid_at = $2, verified_at = now(), verification = $3, verified_by = $4, receipt_number = payment_number, callback_payload = COALESCE($5::jsonb, callback_payload), updated_by = $6 WHERE id = $1`,
		paymentID, paidAt, verification, verifiedBy, nullJSON(callback), actorOrNil(p)); err != nil {
		return err
	}
	var total, paid int64
	var invNumber string
	if err := tx.QueryRow(ctx, `UPDATE invoices SET paid_amount = LEAST(total_amount, paid_amount + $2), updated_by = $3 WHERE id = $1 RETURNING total_amount, paid_amount, invoice_number`, invID, amount, actorOrNil(p)).Scan(&total, &paid, &invNumber); err != nil {
		return err
	}
	newStatus := "partially_paid"
	if paid >= total {
		newStatus = "paid"
	}
	if _, err := tx.Exec(ctx, `UPDATE invoices SET status = $2, paid_at = CASE WHEN $2 = 'paid' THEN $3 ELSE paid_at END WHERE id = $1`, invID, newStatus, paidAt); err != nil {
		return err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "payment", ObjectID: paymentID, Action: audit.ActStatusChanged, From: status, To: "paid", Payload: map[string]any{"verification": verification}})
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: invID, Action: "payment_received", Payload: map[string]any{"payment": number, "amount": amount, "status": newStatus}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "payment", EntityID: &paymentID, EntityLabel: number, After: map[string]any{"status": "paid", "verification": verification}})
	if s.Jobs != nil {
		payload := map[string]any{"amount": amount, "domain": "finance", "invoice_number": invNumber, "receipt_number": number}
		if tenantUser != nil {
			payload["tenant_user_id"] = *tenantUser
		}
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventPaymentPaid, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "payment", ObjectID: paymentID, ObjectLabel: number, ActorUserID: actorOrNil(p), Payload: payload})
		if newStatus == "paid" {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventInvoicePaid, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "invoice", ObjectID: invID, ObjectLabel: invNumber, ActorUserID: actorOrNil(p), Payload: payload})
		}
	}
	return nil
}

func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// RecordManual: staf mencatat pembayaran tunai/transfer yang sudah diterima (verifikasi langsung).
type RecordInput struct {
	Amount int64      `json:"amount"`
	Method string     `json:"method"`
	PaidAt *time.Time `json:"paid_at"`
	Notes  *string    `json:"notes"`
}

func (s *Service) RecordManual(ctx context.Context, invoiceID uuid.UUID, in RecordInput) (*Payment, error) {
	p := authctx.Must(ctx)
	var out *Payment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inv, err := s.getTx(ctx, tx, invoiceID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.payments.create", inv.PropertyID); err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.payments.verify", inv.PropertyID); err != nil {
			return err
		}
		if inv.OutstandingAmt <= 0 || !(inv.Status == "issued" || inv.Status == "partially_paid" || inv.Status == "overdue") {
			return apperr.Conflict("INVOICE_NOT_PAYABLE", "Invoice tidak dapat dibayar")
		}
		if in.Amount <= 0 || in.Amount > inv.OutstandingAmt {
			return apperr.Validation(fmt.Sprintf("amount harus 1..%d", inv.OutstandingAmt))
		}
		method := in.Method
		if method == "" {
			method = "transfer"
		}
		loc := property.PropertyTimezone(ctx, tx, inv.PropertyID)
		number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixPayment, time.Now(), loc)
		if err != nil {
			return err
		}
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO payments (id, organization_id, property_id, payment_number, invoice_id, amount, currency_code, provider_code, method, status, notes, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,'manual',$8,'pending',$9,$10,$10)`,
			id, p.OrganizationID, inv.PropertyID, number, inv.ID, in.Amount, inv.CurrencyCode, method, in.Notes, p.UserID); err != nil {
			return err
		}
		paidAt := time.Now().UTC()
		if in.PaidAt != nil {
			paidAt = *in.PaidAt
		}
		if err := s.settleTx(ctx, tx, id, paidAt, "staff_manual", &p.UserID, nil); err != nil {
			return err
		}
		out, err = s.getPaymentTx(ctx, tx, id)
		return err
	})
	return out, err
}

// VerifyPending: staf memverifikasi pembayaran manual yang diinisiasi tenant (bukti transfer diterima).
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
		if err := s.settleTx(ctx, tx, paymentID, paidAt, "staff_manual", &p.UserID, nil); err != nil {
			return err
		}
		if in.Notes != nil {
			_, _ = tx.Exec(ctx, `UPDATE payments SET notes = $2 WHERE id = $1`, paymentID, *in.Notes)
		}
		out, err = s.getPaymentTx(ctx, tx, paymentID)
		return err
	})
	return out, err
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
	if (pay.Status == "pending" || pay.Status == "initiated") && pay.ProviderCode == "manual" && p.HasOnProperty("billing.payments.verify", pay.PropertyID) {
		pay.AllowedActions = append(pay.AllowedActions, "verify", "fail")
	}
}

type PaymentFilter struct {
	PropertyID *uuid.UUID
	InvoiceID  *uuid.UUID
	Statuses   []string
	Provider   string
}

func (s *Service) ListPayments(ctx context.Context, f PaymentFilter, page httpx.Page) ([]Payment, *string, error) {
	p := authctx.Must(ctx)
	var out []Payment
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if f.PropertyID != nil {
			if err := iam.CanOnProperty(ctx, "billing.payments.view", *f.PropertyID); err != nil {
				return err
			}
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND p.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("billing.payments.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND p.property_id = ANY($%d)", len(args))
		}
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
		if err := iam.CanOnProperty(ctx, "billing.payments.view", pay.PropertyID); err != nil {
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
			return verr
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
			if err := s.settleTx(ctx, tx, paymentID, paidAt, "gateway_callback", nil, rawJSON); err != nil {
				return err
			}
			result, out.Status = "paid", "paid"
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
		return tx.QueryRow(ctx, `SELECT COALESCE(sum(i.total_amount - i.paid_amount),0), count(*), count(*) FILTER (WHERE i.status = 'overdue'), min(i.due_at) FROM invoices i WHERE `+where+` AND i.status IN ('issued','partially_paid','overdue')`, args...).
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
		return err
	})
	return out, err
}

func (s *Service) TenantPayments(ctx context.Context, page httpx.Page) ([]Payment, *string, error) {
	var out []Payment
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		where, args := tenantInvoiceWhere(sc, 1)
		where = " WHERE " + where
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
		out = pay
		return nil
	})
	return out, err
}

// ---------- Sweep (worker): due soon H-3, overdue, payment expired ----------

func (s *Service) Sweep(ctx context.Context, orgID uuid.UUID) error {
	ctx = authctx.With(ctx, authctx.System(orgID))
	return s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE invoices SET status = 'overdue', overdue_notified_at = now() WHERE status IN ('issued','partially_paid') AND due_at < now() RETURNING id, invoice_number, property_id`)
		if err != nil {
			return err
		}
		type ref struct {
			id  uuid.UUID
			num string
			pid uuid.UUID
		}
		var overdue, dueSoon []ref
		for rows.Next() {
			var r ref
			if rows.Scan(&r.id, &r.num, &r.pid) == nil {
				overdue = append(overdue, r)
			}
		}
		rows.Close()
		rows, err = tx.Query(ctx, `UPDATE invoices SET due_soon_notified_at = now() WHERE status IN ('issued','partially_paid') AND due_soon_notified_at IS NULL AND due_at BETWEEN now() AND now() + interval '3 days' RETURNING id, invoice_number, property_id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r ref
			if rows.Scan(&r.id, &r.num, &r.pid) == nil {
				dueSoon = append(dueSoon, r)
			}
		}
		rows.Close()
		_, _ = tx.Exec(ctx, `UPDATE payments SET status = 'expired', failure_reason = 'Batas waktu pembayaran habis' WHERE status IN ('initiated','pending') AND expires_at IS NOT NULL AND expires_at < now()`)
		if s.Jobs != nil {
			for _, r := range overdue {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventInvoiceOverdue, OrganizationID: orgID, PropertyID: &r.pid, ObjectType: "invoice", ObjectID: r.id, ObjectLabel: r.num, Payload: map[string]any{"domain": "finance"}})
			}
			for _, r := range dueSoon {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventInvoiceDueSoon, OrganizationID: orgID, PropertyID: &r.pid, ObjectType: "invoice", ObjectID: r.id, ObjectLabel: r.num, Payload: map[string]any{"domain": "finance"}})
			}
		}
		return nil
	})
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
