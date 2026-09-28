package billing

import (
	"context"
	"fmt"
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
	"github.com/buildingvision/api/internal/property"
)

// ---------- Credit note (PRD P4 v2.1 P4-INV-07) ----------
// Koreksi invoice terbit (dibayar sebagian/penuh) tanpa mengedit/hapus invoice: Finance Staff mengajukan (billing.credit_notes.create),
// Finance Manager menyetujui (billing.credit_notes.approve) — nomor CN-{YEAR}-{SEQ6} diberikan saat disetujui. Bila setelah koreksi
// pembayaran melebihi tagihan, kelebihannya menjadi saldo kredit tenant.

const (
	EventCreditNoteRequested = "credit_note.requested"
	EventCreditNoteApproved  = "credit_note.approved"
)

type CreditNote struct {
	ID               uuid.UUID  `json:"id"`
	CreditNoteNumber *string    `json:"credit_note_number"`
	PropertyID       uuid.UUID  `json:"property_id"`
	InvoiceID        uuid.UUID  `json:"invoice_id"`
	InvoiceNumber    *string    `json:"invoice_number"`
	TenantName       *string    `json:"tenant_name"`
	UnitLabel        *string    `json:"unit_label"`
	Amount           int64      `json:"amount"`
	Reason           string     `json:"reason"`
	Status           string     `json:"status"`
	RequestedByName  *string    `json:"requested_by_name"`
	RequestedAt      time.Time  `json:"requested_at"`
	DecidedByName    *string    `json:"decided_by_name"`
	DecidedAt        *time.Time `json:"decided_at"`
	DecisionNote     *string    `json:"decision_note"`
	AllowedActions   []string   `json:"allowed_actions"`
	Version          int        `json:"version"`
}

const cnSelect = `SELECT c.id, c.credit_note_number, c.property_id, c.invoice_id, i.invoice_number, t.name, COALESCE('Unit ' || un.unit_number, l.name), c.amount, c.reason, c.status,
	rb.full_name, c.requested_at, db.full_name, c.decided_at, c.decision_note, c.version
	FROM credit_notes c JOIN invoices i ON i.id = c.invoice_id LEFT JOIN tenants t ON t.id = c.tenant_id LEFT JOIN locations l ON l.id = c.unit_location_id
	LEFT JOIN units un ON un.location_id = c.unit_location_id LEFT JOIN users rb ON rb.id = c.requested_by LEFT JOIN users db ON db.id = c.decided_by`

func scanCN(row pgx.Row) (*CreditNote, error) {
	var c CreditNote
	if err := row.Scan(&c.ID, &c.CreditNoteNumber, &c.PropertyID, &c.InvoiceID, &c.InvoiceNumber, &c.TenantName, &c.UnitLabel, &c.Amount, &c.Reason, &c.Status,
		&c.RequestedByName, &c.RequestedAt, &c.DecidedByName, &c.DecidedAt, &c.DecisionNote, &c.Version); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Service) cnActions(ctx context.Context, c *CreditNote) {
	p := authctx.Must(ctx)
	c.AllowedActions = []string{"view"}
	if c.Status == "pending" {
		if p.HasOnProperty("billing.credit_notes.approve", c.PropertyID) {
			c.AllowedActions = append(c.AllowedActions, "approve", "reject")
		}
		if p.HasOnProperty("billing.credit_notes.create", c.PropertyID) {
			c.AllowedActions = append(c.AllowedActions, "cancel")
		}
	}
}

type CreditNoteInput struct {
	Amount int64  `json:"amount"`
	Reason string `json:"reason"`
}

func (s *Service) RequestCreditNote(ctx context.Context, invoiceID uuid.UUID, in CreditNoteInput) (*CreditNote, error) {
	p := authctx.Must(ctx)
	var out *CreditNote
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inv, err := s.getTx(ctx, tx, invoiceID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.credit_notes.create", inv.PropertyID); err != nil {
			return err
		}
		if inv.InvoiceNumber == nil || inv.Status == "cancelled" {
			return apperr.Conflict("INVOICE_NOT_CREDITABLE", "Credit note hanya untuk invoice terbit (draft diedit, invoice belum dibayar di-void)")
		}
		if strings.TrimSpace(in.Reason) == "" {
			return apperr.Validation("reason wajib").WithField("reason", "wajib")
		}
		var pendingSum int64
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM credit_notes WHERE invoice_id = $1 AND status = 'pending'`, invoiceID).Scan(&pendingSum)
		max := inv.TotalAmount - inv.CreditedAmount - pendingSum
		if in.Amount <= 0 || in.Amount > max {
			return apperr.Validation(fmt.Sprintf("amount harus 1..%d (total invoice dikurangi koreksi yang ada)", max)).WithField("amount", "melebihi batas")
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO credit_notes (organization_id, property_id, invoice_id, tenant_id, unit_location_id, amount, reason, requested_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING id`, p.OrganizationID, inv.PropertyID, invoiceID, inv.TenantID, inv.UnitLocationID, in.Amount, strings.TrimSpace(in.Reason), p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: invoiceID, Action: "credit_note_requested", Payload: map[string]any{"credit_note_id": id, "amount": in.Amount, "reason": in.Reason}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "credit_note", EntityID: &id, EntityLabel: *inv.InvoiceNumber, After: in})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventCreditNoteRequested, OrganizationID: p.OrganizationID, PropertyID: &inv.PropertyID, ObjectType: "credit_note", ObjectID: id, ObjectLabel: *inv.InvoiceNumber, ActorUserID: &p.UserID,
				Payload: map[string]any{"domain": "finance", "amount": in.Amount}})
		}
		out, err = scanCN(tx.QueryRow(ctx, cnSelect+` WHERE c.id = $1`, id))
		if err == nil {
			s.cnActions(ctx, out)
		}
		return err
	})
	return out, err
}

type CreditNoteFilter struct {
	PropertyID *uuid.UUID
	InvoiceID  *uuid.UUID
	Statuses   []string
}

func (s *Service) ListCreditNotes(ctx context.Context, f CreditNoteFilter, page httpx.Page) ([]CreditNote, *string, error) {
	p := authctx.Must(ctx)
	var out []CreditNote
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("billing.credit_notes.view", *f.PropertyID) {
				return apperr.Forbidden("")
			}
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND c.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("billing.credit_notes.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND c.property_id = ANY($%d)", len(args))
		}
		if f.InvoiceID != nil {
			args = append(args, *f.InvoiceID)
			where += fmt.Sprintf(" AND c.invoice_id = $%d", len(args))
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND c.status = ANY($%d)", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (c.requested_at, c.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, cnSelect+where+fmt.Sprintf(" ORDER BY c.requested_at DESC, c.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCN(rows)
			if err != nil {
				return err
			}
			s.cnActions(ctx, c)
			out = append(out, *c)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			cc := httpx.EncodeCursor(last.RequestedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &cc
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	if out == nil {
		out = []CreditNote{}
	}
	return out, next, err
}

func (s *Service) GetCreditNote(ctx context.Context, id uuid.UUID) (*CreditNote, error) {
	var out *CreditNote
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		c, err := scanCN(tx.QueryRow(ctx, cnSelect+` WHERE c.id = $1`, id))
		if err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Credit note")
			}
			return err
		}
		if !authctx.Must(ctx).HasAnyOnProperty("billing.credit_notes.view", c.PropertyID) {
			return apperr.Forbidden("")
		}
		s.cnActions(ctx, c)
		out = c
		return nil
	})
	return out, err
}

type CreditNoteDecision struct {
	Note string `json:"note"`
}

// DecideCreditNote: approve | reject | cancel.
func (s *Service) DecideCreditNote(ctx context.Context, id uuid.UUID, action string, in CreditNoteDecision) (*CreditNote, error) {
	p := authctx.Must(ctx)
	var out *CreditNote
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		c, err := scanCN(tx.QueryRow(ctx, cnSelect+` WHERE c.id = $1 FOR UPDATE OF c`, id))
		if err != nil {
			return apperr.NotFound("Credit note")
		}
		s.cnActions(ctx, c)
		if !has(c.AllowedActions, action) {
			return apperr.InvalidTransition("Aksi " + action + " tidak tersedia untuk credit note berstatus " + c.Status)
		}
		note := strings.TrimSpace(in.Note)
		switch action {
		case "approve":
			var total, paid, credited int64
			var due time.Time
			var tenantID, unitID *uuid.UUID
			var invNumber *string
			if err := tx.QueryRow(ctx, `SELECT total_amount, paid_amount, credited_amount, due_at, tenant_id, unit_location_id, invoice_number FROM invoices WHERE id = $1 FOR UPDATE`, c.InvoiceID).
				Scan(&total, &paid, &credited, &due, &tenantID, &unitID, &invNumber); err != nil {
				return err
			}
			if credited+c.Amount > total {
				return apperr.Conflict("CREDIT_EXCEEDS_INVOICE", "Koreksi melebihi total invoice")
			}
			newCredited := credited + c.Amount
			// pembayaran yang kini melebihi tagihan bersih → saldo kredit tenant
			overflow := paid + newCredited - total
			if overflow < 0 {
				overflow = 0
			}
			number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixCreditNote, time.Now(), property.PropertyTimezone(ctx, tx, c.PropertyID))
			if err != nil {
				return err
			}
			st := invoiceStatusFor(total, paid-overflow, newCredited, due)
			if _, err := tx.Exec(ctx, `UPDATE invoices SET credited_amount = $2, paid_amount = paid_amount - $3, status = $4, paid_at = CASE WHEN $4 = 'paid' THEN COALESCE(paid_at, now()) ELSE paid_at END, updated_by = $5 WHERE id = $1`,
				c.InvoiceID, newCredited, overflow, st, p.UserID); err != nil {
				return err
			}
			if overflow > 0 {
				if _, err := tx.Exec(ctx, `INSERT INTO tenant_credit_entries (organization_id, property_id, tenant_id, unit_location_id, entry_type, amount, description, invoice_id, credit_note_id, created_by)
					VALUES ($1,$2,$3,$4,'credit_note',$5,$6,$7,$8,$9)`, p.OrganizationID, c.PropertyID, tenantID, unitID, overflow, "Credit note "+number, c.InvoiceID, id, p.UserID); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE credit_notes SET status = 'approved', credit_note_number = $2, decided_by = $3, decided_at = now(), decision_note = NULLIF($4,''), updated_by = $3 WHERE id = $1`, id, number, p.UserID, note); err != nil {
				return err
			}
			if st == "paid" {
				_, _ = tx.Exec(ctx, `UPDATE invoice_penalties SET status = 'final' WHERE invoice_id = $1 AND status = 'accruing'`, c.InvoiceID)
			}
			if err := s.postFundsTx(ctx, tx, c.InvoiceID, nil); err != nil {
				return err
			}
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: c.InvoiceID, Action: "credit_note_approved", Payload: map[string]any{"credit_note": number, "amount": c.Amount, "to_credit_balance": overflow}})
			if s.Jobs != nil {
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventCreditNoteApproved, OrganizationID: p.OrganizationID, PropertyID: &c.PropertyID, ObjectType: "credit_note", ObjectID: id, ObjectLabel: number, ActorUserID: &p.UserID,
					Payload: map[string]any{"domain": "finance", "amount": c.Amount, "invoice_number": strPtrVal(invNumber)}})
			}
		case "reject", "cancel":
			if action == "reject" && note == "" {
				return apperr.Validation("note wajib untuk penolakan").WithField("note", "wajib")
			}
			st := map[string]string{"reject": "rejected", "cancel": "cancelled"}[action]
			if _, err := tx.Exec(ctx, `UPDATE credit_notes SET status = $2, decided_by = $3, decided_at = now(), decision_note = NULLIF($4,''), updated_by = $3 WHERE id = $1`, id, st, p.UserID, note); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "credit_note", EntityID: &id, EntityLabel: strPtrVal(c.InvoiceNumber), Before: map[string]any{"status": c.Status}, After: map[string]any{"action": action, "note": note}})
		out, err = scanCN(tx.QueryRow(ctx, cnSelect+` WHERE c.id = $1`, id))
		if err == nil {
			s.cnActions(ctx, out)
		}
		return err
	})
	return out, err
}

func strPtrVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
