package bvrooms

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/storage"
)

// ---------- metode pembayaran (D2: fase hold) ----------

// PaymentMethods: GET /payment-methods?property_id — VA dikembalikan enabled=false, coming_soon=true (mockup UI).
func (s *Service) PaymentMethods(ctx context.Context, propertyID uuid.UUID) ([]PaymentOption, error) {
	orgID := mustOrg(ctx)
	var out []PaymentOption
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		l, err := s.listingByIDTx(ctx, tx, propertyID)
		if err != nil {
			return err
		}
		out = paymentOptions(l)
		return nil
	})
	return out, err
}

const paymentSelect = `SELECT id, provider_code, method_code, amount, status, instructions, va_number, expires_at, proof_storage_key, proof_submitted_at, paid_at, verified_at, note, created_at FROM bvrooms_payments`

func (s *Service) scanPayment(ctx context.Context, row pgx.Row, withProof bool) (*Payment, error) {
	var p Payment
	var proofKey *string
	if err := row.Scan(&p.ID, &p.ProviderCode, &p.MethodCode, &p.Amount, &p.Status, &p.Instructions, &p.VANumber, &p.ExpiresAt, &proofKey, &p.ProofSubmitted, &p.PaidAt, &p.VerifiedAt, &p.Note, &p.CreatedAt); err != nil {
		return nil, err
	}
	p.ProofRequired = p.ProviderCode == "manual" && p.MethodCode != "cash_on_site"
	if withProof {
		p.ProofURL = s.photoURL(ctx, proofKey)
	}
	if p.Instructions == nil {
		p.Instructions = json.RawMessage(`{}`)
	}
	return &p, nil
}

func (s *Service) latestPaymentTx(ctx context.Context, tx pgx.Tx, bookingID uuid.UUID, withProof bool) (*Payment, error) {
	p, err := s.scanPayment(ctx, tx.QueryRow(ctx, paymentSelect+` WHERE booking_id = $1 ORDER BY (status IN ('pending','proof_submitted','paid')) DESC, created_at DESC LIMIT 1`, bookingID), withProof)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return p, nil
}

type CreatePaymentReq struct {
	ProviderCode string `json:"provider_code"`
	MethodCode   string `json:"method_code"`
}

// CreatePayment: pilih metode (manual transfer rekening properti / cash_on_site). Payment pending lama dibatalkan (change-method).
func (s *Service) CreatePayment(ctx context.Context, code string, in CreatePaymentReq) (*Booking, error) {
	p, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	if in.ProviderCode == "" {
		in.ProviderCode = "manual"
	}
	var out *Booking
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := s.bookingByCodeTx(ctx, tx, code)
		if err != nil {
			return err
		}
		if b.CustomerID != p.UserID {
			return apperr.NotFound("Booking")
		}
		l, err := s.listingByIDTx(ctx, tx, b.PropertyID)
		if err != nil {
			return err
		}
		st := b.deriveStatus(s.now())
		if st != StatusUnpaid {
			return apperr.Conflict("NOT_PAYABLE", "Booking tidak dalam status UNPAID")
		}
		var opt *PaymentOption
		for _, o := range paymentOptions(l) {
			if o.MethodCode == in.MethodCode && o.ProviderCode == in.ProviderCode {
				oo := o
				opt = &oo
			}
		}
		if opt == nil {
			return apperr.Validation("method_code tidak dikenal untuk properti ini")
		}
		if !opt.Enabled {
			return apperr.New(422, "METHOD_DISABLED", "Method disabled", "Metode pembayaran ini belum tersedia (segera hadir)")
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_payments SET status = 'cancelled' WHERE booking_id = $1 AND status = 'pending'`, b.ID); err != nil {
			return err
		}
		var proofSubmitted bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bvrooms_payments WHERE booking_id = $1 AND status = 'proof_submitted')`, b.ID).Scan(&proofSubmitted)
		if proofSubmitted {
			return apperr.Conflict("PROOF_PENDING", "Bukti pembayaran sedang diverifikasi; metode tidak dapat diganti")
		}
		instr := map[string]any{}
		if opt.MethodCode == "cash_on_site" {
			instr["note"] = "Pembayaran dilakukan di properti saat check-in"
			// pay_at_property: reservasi langsung confirmed, tanpa deadline
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'confirmed', confirmed_at = now() WHERE bvrooms_booking_id = $1 AND status = 'new'`, b.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE bvrooms_bookings SET payment_status = 'pay_at_property', payment_deadline_at = NULL WHERE id = $1`, b.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_payments (organization_id, booking_id, provider_code, method_code, amount, status, instructions, note) VALUES ($1,$2,'manual','cash_on_site',$3,'pending',$4,'Bayar di properti saat check-in')`,
				p.OrganizationID, b.ID, b.Total, mustJSON(instr)); err != nil {
				return err
			}
		} else {
			var accounts []map[string]string
			_ = json.Unmarshal(l.BankAccounts, &accounts)
			for _, a := range accounts {
				if "transfer_"+strings.ToLower(strings.ReplaceAll(strings.TrimSpace(a["bank"]), " ", "_")) == opt.MethodCode {
					instr["bank"], instr["account_number"], instr["account_name"] = a["bank"], a["account_number"], a["account_name"]
				}
			}
			instr["amount"] = b.Total
			instr["note"] = fmt.Sprintf("Transfer tepat Rp %s ke rekening di atas, lalu unggah bukti transfer sebelum %s.", formatIDR(b.Total), deadlineText(b.Deadline, l.location()))
			if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_payments (organization_id, booking_id, provider_code, method_code, amount, status, instructions, expires_at) VALUES ($1,$2,'manual',$3,$4,'pending',$5,$6)`,
				p.OrganizationID, b.ID, opt.MethodCode, b.Total, mustJSON(instr), b.Deadline); err != nil {
				return err
			}
		}
		_ = audit.LogAs(ctx, tx, p.OrganizationID, nil, p.IP, p.UserAgent, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "bvrooms_booking", EntityID: &b.ID, EntityLabel: b.Code, After: map[string]any{"payment_method": opt.MethodCode}})
		out, err = s.bookingTx(ctx, tx, b.ID, false)
		return err
	})
	return out, err
}

func deadlineText(t *time.Time, loc *time.Location) string {
	if t == nil {
		return "-"
	}
	return t.In(loc).Format("Mon, 02 Jan 2006 15:04")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ---------- bukti transfer ----------

type ProofPresignInput struct {
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type ProofPresignResult struct {
	PaymentID uuid.UUID         `json:"payment_id"`
	UploadURL string            `json:"upload_url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

var proofContent = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "application/pdf": ".pdf"}

// ProofPresign: URL unggah bukti transfer untuk payment pending manual (≤ 5 MB).
func (s *Service) ProofPresign(ctx context.Context, code string, in ProofPresignInput) (*ProofPresignResult, error) {
	p, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	ext, ok := proofContent[in.ContentType]
	if !ok {
		return nil, apperr.Validation("content_type harus image/jpeg|image/png|image/webp|application/pdf")
	}
	if in.SizeBytes <= 0 || in.SizeBytes > 5<<20 {
		return nil, apperr.Validation("size_bytes harus 1..5MB")
	}
	var out *ProofPresignResult
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := s.bookingByCodeTx(ctx, tx, code)
		if err != nil {
			return err
		}
		if b.CustomerID != p.UserID {
			return apperr.NotFound("Booking")
		}
		var payID uuid.UUID
		var method string
		if err := tx.QueryRow(ctx, `SELECT id, method_code FROM bvrooms_payments WHERE booking_id = $1 AND provider_code = 'manual' AND status IN ('pending','proof_submitted') ORDER BY created_at DESC LIMIT 1`, b.ID).Scan(&payID, &method); err != nil {
			return apperr.Conflict("NO_PENDING_PAYMENT", "Pilih metode pembayaran terlebih dahulu")
		}
		if method == "cash_on_site" {
			return apperr.Conflict("PROOF_NOT_REQUIRED", "Bayar di tempat tidak memerlukan bukti transfer")
		}
		key := storage.ObjectKey(p.OrganizationID.String(), "bvrooms-proof-"+payID.String(), s.now(), ext)
		url, err := s.Storage.PresignPut(ctx, key, in.ContentType, in.SizeBytes, s.uploadTTL)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_payments SET proof_storage_key = $2, proof_content_type = $3 WHERE id = $1`, payID, key, in.ContentType); err != nil {
			return err
		}
		out = &ProofPresignResult{PaymentID: payID, UploadURL: url, Method: "PUT", Headers: map[string]string{"Content-Type": in.ContentType}, ExpiresAt: s.now().Add(s.uploadTTL)}
		return nil
	})
	return out, err
}

// ProofConfirm: file sudah diunggah → proof_submitted; deadline booking diperpanjang 24 jam menunggu verifikasi Finance.
func (s *Service) ProofConfirm(ctx context.Context, code string) (*Booking, error) {
	p, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	var out *Booking
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := s.bookingByCodeTx(ctx, tx, code)
		if err != nil {
			return err
		}
		if b.CustomerID != p.UserID {
			return apperr.NotFound("Booking")
		}
		var payID uuid.UUID
		var key *string
		if err := tx.QueryRow(ctx, `SELECT id, proof_storage_key FROM bvrooms_payments WHERE booking_id = $1 AND provider_code = 'manual' AND status IN ('pending','proof_submitted') ORDER BY created_at DESC LIMIT 1`, b.ID).Scan(&payID, &key); err != nil {
			return apperr.Conflict("NO_PENDING_PAYMENT", "Pilih metode pembayaran terlebih dahulu")
		}
		if key == nil {
			return apperr.Conflict("UPLOAD_NOT_FOUND", "Minta URL unggah (presign) terlebih dahulu")
		}
		if _, _, err := s.Storage.Head(ctx, *key); err != nil {
			return apperr.Conflict("UPLOAD_NOT_FOUND", "File bukti belum diunggah ke storage")
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_payments SET status = 'proof_submitted', proof_submitted_at = now() WHERE id = $1`, payID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_bookings SET payment_deadline_at = GREATEST(payment_deadline_at, now() + interval '24 hours') WHERE id = $1 AND payment_status = 'unpaid'`, b.ID); err != nil {
			return err
		}
		if err := s.notifyTx(ctx, tx, p.OrganizationID, p.UserID, "proof_submitted", "Bukti Pembayaran Diterima.", "Bukti transfer kamu sedang diverifikasi. Kami akan memberi kabar setelah pembayaran dikonfirmasi.", &b.ID, "info"); err != nil {
			return err
		}
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventPaymentProof, OrganizationID: p.OrganizationID, PropertyID: &b.PropertyID, ObjectType: "bvrooms_booking", ObjectID: b.ID, ObjectLabel: b.Code + " · " + b.GuestName, Payload: map[string]any{"domain": "billing", "amount": b.Total}})
		}
		out, err = s.bookingTx(ctx, tx, b.ID, false)
		return err
	})
	return out, err
}

// ---------- verifikasi (dashboard, Finance) ----------

type VerifyInput struct {
	Note string `json:"note"`
}

// VerifyPayment (staf bvrooms.payments.verify): UNPAID → PAID; reservasi confirmed; notif customer.
func (s *Service) VerifyPayment(ctx context.Context, paymentID uuid.UUID, in VerifyInput) (*Booking, error) {
	p, err := staff(ctx)
	if err != nil {
		return nil, err
	}
	var out *Booking
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var bookingID uuid.UUID
		var status string
		if err := tx.QueryRow(ctx, `SELECT booking_id, status FROM bvrooms_payments WHERE id = $1 FOR UPDATE`, paymentID).Scan(&bookingID, &status); err != nil {
			return apperr.NotFound("Payment")
		}
		b, err := scanBooking(tx.QueryRow(ctx, bookingSelect+` WHERE b.id = $1`, bookingID))
		if err != nil {
			return err
		}
		if !p.HasOnProperty("bvrooms.payments.verify", b.PropertyID) {
			return apperr.Forbidden("")
		}
		if status != "pending" && status != "proof_submitted" {
			return apperr.InvalidTransition("Payment berstatus " + status)
		}
		if b.PaymentStatus != "unpaid" && b.PaymentStatus != "pay_at_property" {
			return apperr.InvalidTransition("Booking berstatus pembayaran " + b.PaymentStatus)
		}
		if st := b.deriveStatus(s.now()); st == StatusCancelled || st == StatusExpired {
			return apperr.InvalidTransition("Booking sudah " + st)
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_payments SET status = 'paid', paid_at = now(), verified_by = $2, verified_at = now(), note = NULLIF($3,'') WHERE id = $1`, paymentID, p.UserID, in.Note); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_bookings SET payment_status = 'paid', payment_deadline_at = NULL WHERE id = $1`, b.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'confirmed', confirmed_at = COALESCE(confirmed_at, now()), updated_by = $2 WHERE bvrooms_booking_id = $1 AND status = 'new'`, b.ID, p.UserID); err != nil {
			return err
		}
		if err := s.notifyTx(ctx, tx, p.OrganizationID, b.CustomerID, "payment_received", "Pembayaran Diterima.", "Pembayaran kamu sudah kami terima. Sampai jumpa saat check-in!", &b.ID, "success"); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "bvrooms_booking", EntityID: &b.ID, EntityLabel: b.Code, Before: map[string]any{"payment_status": b.PaymentStatus}, After: map[string]any{"payment_status": "paid", "payment_id": paymentID}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventBookingPaid, OrganizationID: p.OrganizationID, PropertyID: &b.PropertyID, ObjectType: "bvrooms_booking", ObjectID: b.ID, ObjectLabel: b.Code + " · " + b.GuestName, ActorUserID: &p.UserID, Payload: map[string]any{"domain": "tenant_relation", "amount": b.Total}})
		}
		out, err = s.bookingTx(ctx, tx, b.ID, true)
		return err
	})
	return out, err
}

// RejectPayment (staf): bukti ditolak → payment failed; booking hangus (EXPIRED) — customer dapat booking ulang.
func (s *Service) RejectPayment(ctx context.Context, paymentID uuid.UUID, in VerifyInput) (*Booking, error) {
	p, err := staff(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Note) == "" {
		return nil, apperr.Validation("note (alasan penolakan) wajib").WithField("note", "wajib")
	}
	var out *Booking
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var bookingID uuid.UUID
		var status string
		if err := tx.QueryRow(ctx, `SELECT booking_id, status FROM bvrooms_payments WHERE id = $1 FOR UPDATE`, paymentID).Scan(&bookingID, &status); err != nil {
			return apperr.NotFound("Payment")
		}
		b, err := scanBooking(tx.QueryRow(ctx, bookingSelect+` WHERE b.id = $1`, bookingID))
		if err != nil {
			return err
		}
		if !p.HasOnProperty("bvrooms.payments.verify", b.PropertyID) {
			return apperr.Forbidden("")
		}
		if status != "pending" && status != "proof_submitted" {
			return apperr.InvalidTransition("Payment berstatus " + status)
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_payments SET status = 'failed', verified_by = $2, verified_at = now(), note = $3 WHERE id = $1`, paymentID, p.UserID, in.Note); err != nil {
			return err
		}
		if b.PaymentStatus == "unpaid" {
			if err := s.expireBookingTx(ctx, tx, b, "Bukti pembayaran ditolak: "+in.Note); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "bvrooms_payment", EntityID: &paymentID, EntityLabel: b.Code, Before: map[string]any{"status": status}, After: map[string]any{"status": "failed", "note": in.Note}})
		out, err = s.bookingTx(ctx, tx, b.ID, true)
		return err
	})
	return out, err
}

// RefundDone (staf): refund_pending → refunded (refund manual di luar sistem).
func (s *Service) RefundDone(ctx context.Context, bookingID uuid.UUID, in VerifyInput) (*Booking, error) {
	p, err := staff(ctx)
	if err != nil {
		return nil, err
	}
	var out *Booking
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := scanBooking(tx.QueryRow(ctx, bookingSelect+` WHERE b.id = $1 FOR UPDATE OF b`, bookingID))
		if err != nil {
			return apperr.NotFound("Booking")
		}
		if !p.HasOnProperty("bvrooms.payments.verify", b.PropertyID) {
			return apperr.Forbidden("")
		}
		if b.PaymentStatus != "refund_pending" {
			return apperr.InvalidTransition("Booking tidak menunggu refund")
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_bookings SET payment_status = 'refunded' WHERE id = $1`, b.ID); err != nil {
			return err
		}
		if err := s.notifyTx(ctx, tx, p.OrganizationID, b.CustomerID, "refunded", "Pengembalian Dana Selesai.", "Dana pembatalan booking "+b.Code+" sudah dikembalikan.", &b.ID, "success"); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "bvrooms_booking", EntityID: &b.ID, EntityLabel: b.Code, Before: map[string]any{"payment_status": "refund_pending"}, After: map[string]any{"payment_status": "refunded", "note": in.Note}})
		out, err = s.bookingTx(ctx, tx, b.ID, true)
		return err
	})
	return out, err
}

// expireBookingTx: UNPAID → EXPIRED ("Pemesanan Hangus!"): reservasi cancelled, payment pending expired.
func (s *Service) expireBookingTx(ctx context.Context, tx pgx.Tx, b *bookingRow, reason string) error {
	if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'cancelled', cancelled_at = now(), cancel_reason = $2 WHERE bvrooms_booking_id = $1 AND status IN ('new','confirmed')`, b.ID, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE bvrooms_bookings SET payment_status = 'expired', status_override = 'expired', cancelled_at = now(), cancel_reason = $2, cancelled_by = 'system' WHERE id = $1`, b.ID, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE bvrooms_payments SET status = 'expired' WHERE booking_id = $1 AND status IN ('pending','proof_submitted')`, b.ID); err != nil {
		return err
	}
	if err := s.notifyTx(ctx, tx, mustOrg(ctx), b.CustomerID, "payment_expired", "Pemesanan Hangus!", "Yah! tiket booking kamu hangus karena telah melebihi batas pembayaran :( Silahkan pesan kembali.", &b.ID, "danger"); err != nil {
		return err
	}
	if s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventBookingExpired, OrganizationID: mustOrg(ctx), PropertyID: &b.PropertyID, ObjectType: "bvrooms_booking", ObjectID: b.ID, ObjectLabel: b.Code + " · " + b.GuestName, Payload: map[string]any{"domain": "tenant_relation", "reason": reason}})
	}
	return nil
}
