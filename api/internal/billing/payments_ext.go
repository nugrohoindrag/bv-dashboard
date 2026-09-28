package billing

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
)

const EventPaymentRefunded = "payment.refunded"

// ---------- P4-PAY-05: satu penerimaan dialokasikan ke banyak invoice ----------

type Allocation struct {
	InvoiceID uuid.UUID `json:"invoice_id"`
	Amount    int64     `json:"amount"`
}

type ReceiveInput struct {
	PropertyID  uuid.UUID    `json:"property_id"`
	TenantID    *uuid.UUID   `json:"tenant_id"`
	UnitID      *uuid.UUID   `json:"unit_location_id"`
	Amount      int64        `json:"amount"`
	Method      string       `json:"method"`
	PaidAt      *time.Time   `json:"paid_at"`
	Reference   *string      `json:"reference"`
	ExternalRef *string      `json:"external_ref"`
	Notes       *string      `json:"notes"`
	Allocations []Allocation `json:"allocations"` // kosong = FIFO jatuh tempo terlama
}

type ReceiveResult struct {
	ReceiptGroup string    `json:"receipt_group"`
	Payments     []Payment `json:"payments"`
	Allocated    int64     `json:"allocated_amount"`
	CreditAmount int64     `json:"credit_amount"` // sisa → saldo kredit tenant
}

// Receive: Finance mencatat satu penerimaan (mis. satu transfer untuk beberapa tagihan) → satu payment per invoice dengan
// grup kwitansi RCV-…; sisa menjadi saldo kredit (P4-PAY-04).
func (s *Service) Receive(ctx context.Context, in ReceiveInput) (*ReceiveResult, error) {
	return s.receive(ctx, in, nil)
}

func (s *Service) receive(ctx context.Context, in ReceiveInput, statementLine *uuid.UUID) (*ReceiveResult, error) {
	p := authctx.Must(ctx)
	if !p.HasOnProperty("billing.payments.allocate", in.PropertyID) || !p.HasOnProperty("billing.payments.verify", in.PropertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.payments.allocate & billing.payments.verify")
	}
	if in.Amount <= 0 {
		return nil, apperr.Validation("amount harus > 0").WithField("amount", "harus > 0")
	}
	out := &ReceiveResult{Payments: []Payment{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.receiveTx(ctx, tx, in, statementLine)
		return err
	})
	return out, err
}

func (s *Service) receiveTx(ctx context.Context, tx pgx.Tx, in ReceiveInput, statementLine *uuid.UUID) (*ReceiveResult, error) {
	p := authctx.Must(ctx)
	out := &ReceiveResult{Payments: []Payment{}}
	if err := s.validateParties(ctx, tx, in.PropertyID, in.TenantID, in.UnitID, true); err != nil {
		return nil, err
	}
	type open struct {
		id          uuid.UUID
		outstanding int64
	}
	var opens []open
	rows, err := tx.Query(ctx, `SELECT id, total_amount - paid_amount - credited_amount FROM invoices
		WHERE property_id = $1 AND status IN ('issued','partially_paid','overdue') AND total_amount - paid_amount - credited_amount > 0
		  AND (($2::uuid IS NOT NULL AND tenant_id = $2) OR ($2::uuid IS NULL AND tenant_id IS NULL AND unit_location_id = $3))
		ORDER BY due_at, issued_at FOR UPDATE`, in.PropertyID, in.TenantID, in.UnitID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var o open
		if rows.Scan(&o.id, &o.outstanding) == nil {
			opens = append(opens, o)
		}
	}
	rows.Close()
	outstandingOf := map[uuid.UUID]int64{}
	for _, o := range opens {
		outstandingOf[o.id] = o.outstanding
	}
	var plan []Allocation
	remaining := in.Amount
	if len(in.Allocations) > 0 {
		for _, a := range in.Allocations {
			max, ok := outstandingOf[a.InvoiceID]
			if !ok {
				return nil, apperr.Validation("allocations: invoice "+a.InvoiceID.String()+" bukan tagihan terbuka pihak ini").WithField("allocations", "invoice tidak valid")
			}
			if a.Amount <= 0 || a.Amount > max {
				return nil, apperr.Validation(fmt.Sprintf("allocations: jumlah untuk invoice harus 1..%d", max)).WithField("allocations", "jumlah tidak valid")
			}
			if a.Amount > remaining {
				return nil, apperr.Validation("total alokasi melebihi jumlah penerimaan").WithField("allocations", "melebihi amount")
			}
			remaining -= a.Amount
			plan = append(plan, a)
		}
	} else {
		for _, o := range opens {
			if remaining <= 0 {
				break
			}
			a := o.outstanding
			if a > remaining {
				a = remaining
			}
			plan = append(plan, Allocation{InvoiceID: o.id, Amount: a})
			remaining -= a
		}
	}
	loc := property.PropertyTimezone(ctx, tx, in.PropertyID)
	group, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixReceipt, time.Now(), loc)
	if err != nil {
		return nil, err
	}
	out.ReceiptGroup = group
	method := in.Method
	if method == "" {
		method = "transfer"
	}
	for _, a := range plan {
		pid, err := s.recordManualTx(ctx, tx, a.InvoiceID, RecordInput{Amount: a.Amount, Method: method, PaidAt: in.PaidAt, Notes: in.Notes, Reference: in.Reference, ExternalRef: in.ExternalRef}, group)
		if err != nil {
			return nil, err
		}
		if statementLine != nil {
			_, _ = tx.Exec(ctx, `UPDATE payments SET bank_statement_line_id = $2 WHERE id = $1`, pid, *statementLine)
		}
		pay, err := s.getPaymentTx(ctx, tx, pid)
		if err != nil {
			return nil, err
		}
		s.paymentActions(ctx, pay)
		out.Payments = append(out.Payments, *pay)
		out.Allocated += a.Amount
	}
	if remaining > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO tenant_credit_entries (organization_id, property_id, tenant_id, unit_location_id, entry_type, amount, description, created_by)
			VALUES ($1,$2,$3,$4,'overpayment',$5,$6,$7)`, p.OrganizationID, in.PropertyID, in.TenantID, in.UnitID, remaining, "Sisa penerimaan "+group+" (belum dialokasikan)", p.UserID); err != nil {
			return nil, err
		}
		out.CreditAmount = remaining
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "receipt", EntityLabel: group, After: map[string]any{"amount": in.Amount, "allocated": out.Allocated, "credit": out.CreditAmount, "invoices": len(plan)}})
	return out, nil
}

// ---------- Pakai saldo kredit / deposit untuk invoice ----------

type ApplyBalanceInput struct {
	Amount int64  `json:"amount"` // 0 = sebesar mungkin (min saldo, sisa tagihan)
	Notes  string `json:"notes"`
}

// ApplyBalance: kind credit (saldo kredit, P4-PAY-05) | deposit (potong deposit untuk tunggakan, P4-PND-04).
func (s *Service) ApplyBalance(ctx context.Context, invoiceID uuid.UUID, kind string, in ApplyBalanceInput) (*Payment, error) {
	lt, ok := ledgerTables[kind]
	if !ok {
		return nil, apperr.NotFound("Ledger")
	}
	p := authctx.Must(ctx)
	var out *Payment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inv, err := s.getTx(ctx, tx, invoiceID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.payments.allocate", inv.PropertyID); err != nil {
			return err
		}
		if kind == "deposit" {
			if err := iam.CanOnProperty(ctx, lt.managePerm, inv.PropertyID); err != nil {
				return err
			}
		}
		if inv.OutstandingAmt <= 0 {
			return apperr.Conflict("INVOICE_NOT_PAYABLE", "Invoice tidak memiliki sisa tagihan")
		}
		bal := partyBalanceTx(ctx, tx, kind, inv.TenantID, inv.UnitLocationID)
		amount := in.Amount
		if amount <= 0 {
			amount = bal
		}
		if amount > inv.OutstandingAmt {
			amount = inv.OutstandingAmt
		}
		if amount <= 0 || amount > bal {
			return apperr.Conflict("BALANCE_INSUFFICIENT", fmt.Sprintf("Saldo %s tidak cukup (saldo %d)", map[string]string{"credit": "kredit", "deposit": "deposit"}[kind], bal))
		}
		note := strings.TrimSpace(in.Notes)
		if note == "" {
			note = map[string]string{"credit": "Pemakaian saldo kredit", "deposit": "Pemotongan deposit"}[kind]
		}
		pid, err := s.recordManualTx(ctx, tx, invoiceID, RecordInput{Amount: amount, Method: kind, Notes: &note}, "")
		if err != nil {
			return err
		}
		num := inv.DisplayNumber
		if kind == "credit" {
			_, err = tx.Exec(ctx, `INSERT INTO tenant_credit_entries (organization_id, property_id, tenant_id, unit_location_id, entry_type, amount, description, invoice_id, payment_id, created_by)
				VALUES ($1,$2,$3,$4,'applied',$5,$6,$7,$8,$9)`, p.OrganizationID, inv.PropertyID, inv.TenantID, inv.UnitLocationID, -amount, "Dipakai untuk "+num, invoiceID, pid, p.UserID)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO deposit_entries (organization_id, property_id, tenant_id, unit_location_id, entry_type, amount, reason, invoice_id, payment_id, created_by)
				VALUES ($1,$2,$3,$4,'applied',$5,$6,$7,$8,$9)`, p.OrganizationID, inv.PropertyID, inv.TenantID, inv.UnitLocationID, -amount, "Dipotong untuk "+num, invoiceID, pid, p.UserID)
		}
		if err != nil {
			return err
		}
		out, err = s.getPaymentTx(ctx, tx, pid)
		if err == nil {
			s.paymentActions(ctx, out)
		}
		return err
	})
	return out, err
}

// ---------- P4-PAY-06: refund ----------

type RefundInput struct {
	Reason   string `json:"reason"`
	ToCredit bool   `json:"to_credit"` // true = dikembalikan sebagai saldo kredit (bukan uang keluar)
}

// Refund: pembayaran lunas → refunded; invoice dikoreksi (paid berkurang, status dihitung ulang), sinking fund/deposit ikut
// terkoreksi lewat postFundsTx.
func (s *Service) Refund(ctx context.Context, paymentID uuid.UUID, in RefundInput) (*Payment, error) {
	var out *Payment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pay, err := s.getPaymentTx(ctx, tx, paymentID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.payments.refund", pay.PropertyID); err != nil {
			return err
		}
		if strings.TrimSpace(in.Reason) == "" {
			return apperr.Validation("reason wajib").WithField("reason", "wajib")
		}
		if pay.Method == "credit" || pay.Method == "deposit" {
			return apperr.Conflict("REFUND_NOT_ALLOWED", "Pemakaian saldo dikoreksi lewat credit note, bukan refund")
		}
		if err := s.refundTx(ctx, tx, paymentID, strings.TrimSpace(in.Reason), in.ToCredit); err != nil {
			return err
		}
		out, err = s.getPaymentTx(ctx, tx, paymentID)
		if err == nil {
			s.paymentActions(ctx, out)
		}
		return err
	})
	return out, err
}

func (s *Service) refundTx(ctx context.Context, tx pgx.Tx, paymentID uuid.UUID, reason string, toCredit bool) error {
	p := authctx.Must(ctx)
	var invID, propertyID uuid.UUID
	var amount int64
	var status, number string
	if err := tx.QueryRow(ctx, `SELECT invoice_id, property_id, amount, status, payment_number FROM payments WHERE id = $1 FOR UPDATE`, paymentID).Scan(&invID, &propertyID, &amount, &status, &number); err != nil {
		return apperr.NotFound("Payment")
	}
	if status != "paid" {
		return apperr.Conflict("PAYMENT_NOT_REFUNDABLE", "Hanya pembayaran lunas yang dapat direfund")
	}
	var total, paid, credited int64
	var due time.Time
	var tenantID, unitID *uuid.UUID
	var invNumber *string
	if err := tx.QueryRow(ctx, `SELECT total_amount, paid_amount, credited_amount, due_at, tenant_id, unit_location_id, invoice_number FROM invoices WHERE id = $1 FOR UPDATE`, invID).
		Scan(&total, &paid, &credited, &due, &tenantID, &unitID, &invNumber); err != nil {
		return err
	}
	// porsi pembayaran yang diterapkan ke invoice (kelebihan sudah menjadi kredit)
	var excess int64
	_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM tenant_credit_entries WHERE payment_id = $1 AND entry_type = 'overpayment'`, paymentID).Scan(&excess)
	applied := amount - excess
	if applied > paid {
		applied = paid
	}
	if excess > 0 && !toCredit {
		// refund tunai mengembalikan kelebihan bayar yang tersimpan sebagai saldo kredit — saldo harus masih cukup
		if bal := partyBalanceTx(ctx, tx, "credit", tenantID, unitID); bal < excess {
			return apperr.Conflict("CREDIT_ALREADY_USED", fmt.Sprintf("Kelebihan bayar %d dari pembayaran ini sudah dipakai (saldo kredit %d); refund ke saldo kredit atau koreksi manual", excess, bal))
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE payments SET status = 'refunded', refunded_at = now(), refunded_by = $2, refund_reason = $3, updated_by = $2 WHERE id = $1`, paymentID, actorOrNil(p), reason); err != nil {
		return err
	}
	newStatus := invoiceStatusFor(total, paid-applied, credited, due)
	if _, err := tx.Exec(ctx, `UPDATE invoices SET paid_amount = paid_amount - $2, status = $3, paid_at = CASE WHEN $3 = 'paid' THEN paid_at ELSE NULL END, updated_by = $4 WHERE id = $1`, invID, applied, newStatus, actorOrNil(p)); err != nil {
		return err
	}
	if excess > 0 && !toCredit {
		// refund tunai: kelebihan bayar dari pembayaran ini ikut dikembalikan (refund ke saldo kredit: kelebihan tetap menjadi kredit)
		_, _ = tx.Exec(ctx, `INSERT INTO tenant_credit_entries (organization_id, property_id, tenant_id, unit_location_id, entry_type, amount, description, payment_id, created_by)
			VALUES ($1,$2,$3,$4,'refunded',$5,$6,$7,$8)`, p.OrganizationID, propertyID, tenantID, unitID, -excess, "Refund kelebihan bayar "+number, paymentID, actorOrNil(p))
	}
	if toCredit && applied > 0 {
		_, _ = tx.Exec(ctx, `INSERT INTO tenant_credit_entries (organization_id, property_id, tenant_id, unit_location_id, entry_type, amount, description, invoice_id, payment_id, created_by)
			VALUES ($1,$2,$3,$4,'adjustment',$5,$6,$7,$8,$9)`, p.OrganizationID, propertyID, tenantID, unitID, applied, "Refund "+number+" ke saldo kredit", invID, paymentID, actorOrNil(p))
	}
	if err := s.postFundsTx(ctx, tx, invID, &paymentID); err != nil {
		return err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "payment", ObjectID: paymentID, Action: audit.ActStatusChanged, From: "paid", To: "refunded", Payload: map[string]any{"reason": reason, "to_credit": toCredit}})
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: invID, Action: "payment_refunded", Payload: map[string]any{"payment": number, "amount": applied}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "payment", EntityID: &paymentID, EntityLabel: number, Before: map[string]any{"status": "paid"}, After: map[string]any{"status": "refunded", "reason": reason}})
	if s.Jobs != nil {
		n := ""
		if invNumber != nil {
			n = *invNumber
		}
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventPaymentRefunded, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "payment", ObjectID: paymentID, ObjectLabel: number, ActorUserID: actorOrNil(p),
			Payload: map[string]any{"domain": "finance", "invoice_number": n, "amount": applied, "reason": reason}})
	}
	return nil
}

// ---------- Ubah catatan pembayaran (P4-INT-03 external_ref) ----------

type PaymentUpdateInput struct {
	ExternalRef *string `json:"external_ref"`
	Reference   *string `json:"reference"`
	Notes       *string `json:"notes"`
}

func (s *Service) UpdatePayment(ctx context.Context, id uuid.UUID, in PaymentUpdateInput) (*Payment, error) {
	p := authctx.Must(ctx)
	var out *Payment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pay, err := s.getPaymentTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.payments.create", pay.PropertyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE payments SET external_ref = COALESCE($2, external_ref), reference = COALESCE($3, reference), notes = COALESCE($4, notes), updated_by = $5 WHERE id = $1`,
			id, in.ExternalRef, in.Reference, in.Notes, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "payment", EntityID: &id, EntityLabel: pay.PaymentNumber, After: in})
		out, err = s.getPaymentTx(ctx, tx, id)
		if err == nil {
			s.paymentActions(ctx, out)
		}
		return err
	})
	return out, err
}

// ---------- P4-VRF-02: bukti transfer (lampiran object payment) ----------

// PaymentAccess: tenant pemilik pembayaran pending (unggah) / tenant yang melihat invoice (baca); staf billing.payments.view|verify.
func PaymentAccess(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, write bool) error {
	p := authctx.Must(ctx)
	var propertyID, invoiceID uuid.UUID
	var status, provider string
	var tenantUser *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT property_id, invoice_id, status, provider_code, tenant_user_id FROM payments WHERE id = $1`, objectID).Scan(&propertyID, &invoiceID, &status, &provider, &tenantUser); err != nil {
		return apperr.NotFound("Payment")
	}
	if p.IsTenant {
		if tenantUser == nil || *tenantUser != p.UserID {
			return apperr.NotFound("Payment")
		}
		if write && !((status == "pending" || status == "initiated") && provider == "manual") {
			return apperr.Conflict("PAYMENT_NOT_PENDING", "Bukti hanya dapat diunggah untuk pembayaran yang menunggu verifikasi")
		}
		return nil
	}
	if write {
		if !p.HasOnProperty("billing.payments.verify", propertyID) {
			return apperr.Forbidden("")
		}
		return nil
	}
	return canViewAt(ctx, tx, "billing.payments.view", propertyID, invoiceUnitTx(ctx, tx, invoiceID))
}

type Proof struct {
	ID          uuid.UUID `json:"id"`
	ContentType string    `json:"content_type"`
	FileName    *string   `json:"file_name"`
	URL         string    `json:"url"`
	ThumbURL    string    `json:"thumb_url,omitempty"`
	UploadedAt  time.Time `json:"uploaded_at"`
}

// Proofs: bukti transfer pembayaran (dilihat Finance saat verifikasi & tenant pemilik).
func (s *Service) Proofs(ctx context.Context, paymentID uuid.UUID, att *attachments.Service) ([]Proof, error) {
	out := []Proof{}
	if att == nil {
		return out, nil
	}
	list, err := att.ListForObject(ctx, "payment", paymentID)
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].UploadedAt.Before(list[j].UploadedAt) })
	for _, a := range list {
		out = append(out, Proof{ID: a.ID, ContentType: a.ContentType, FileName: a.OriginalFilename, URL: a.URL, ThumbURL: a.ThumbURL, UploadedAt: a.UploadedAt})
	}
	return out, nil
}
