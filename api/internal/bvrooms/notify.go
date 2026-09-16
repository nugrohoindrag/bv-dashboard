package bvrooms

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/hotel"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/httpx"
)

// ---------- inbox customer ----------

type Notification struct {
	ID          uuid.UUID  `json:"id"`
	Type        string     `json:"type"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Severity    string     `json:"severity"`
	BookingCode *string    `json:"booking_code"`
	CreatedAt   time.Time  `json:"created_at"`
	ReadAt      *time.Time `json:"read_at"`
}

// notifyTx: satu notifikasi inbox (dedupe: tipe sama untuk booking sama dalam 1 menit tidak digandakan — booking multi-kamar).
func (s *Service) notifyTx(ctx context.Context, tx pgx.Tx, orgID, customerID uuid.UUID, typ, title, body string, bookingID *uuid.UUID, severity string) error {
	if bookingID != nil {
		var dup bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bvrooms_notifications WHERE customer_id = $1 AND booking_id = $2 AND type = $3 AND created_at > now() - interval '1 minute')`, customerID, *bookingID, typ).Scan(&dup)
		if dup {
			return nil
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO bvrooms_notifications (organization_id, customer_id, type, title, body, booking_id, severity) VALUES ($1,$2,$3,$4,$5,$6,$7)`, orgID, customerID, typ, title, body, bookingID, severity)
	return err
}

type Inbox struct {
	httpx.ListResponse[Notification]
	UnreadCount int `json:"unread_count"`
}

func (s *Service) Notifications(ctx context.Context, page httpx.Page) (*Inbox, error) {
	p, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	out := &Inbox{}
	items := []Notification{}
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var cursorAt *time.Time
		var cursorID *uuid.UUID
		if page.Cursor != nil {
			t, err := time.Parse(time.RFC3339Nano, page.Cursor.Value)
			if err != nil {
				return apperr.Validation("cursor tidak valid")
			}
			cursorAt, cursorID = &t, &page.Cursor.ID
		}
		rows, err := tx.Query(ctx, `SELECT n.id, n.type, n.title, n.body, n.severity, b.booking_code, n.created_at, n.read_at FROM bvrooms_notifications n LEFT JOIN bvrooms_bookings b ON b.id = n.booking_id
			WHERE n.customer_id = $1 AND n.deleted_at IS NULL AND ($2::timestamptz IS NULL OR (n.created_at, n.id) < ($2, $3)) ORDER BY n.created_at DESC, n.id DESC LIMIT $4`, p.UserID, cursorAt, cursorID, page.Limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n Notification
			if err := rows.Scan(&n.ID, &n.Type, &n.Title, &n.Body, &n.Severity, &n.BookingCode, &n.CreatedAt, &n.ReadAt); err != nil {
				return err
			}
			items = append(items, n)
		}
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM bvrooms_notifications WHERE customer_id = $1 AND deleted_at IS NULL AND read_at IS NULL`, p.UserID).Scan(&out.UnreadCount)
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	var next *string
	if len(items) > page.Limit {
		items = items[:page.Limit]
		c := httpx.EncodeCursor(items[len(items)-1].CreatedAt.Format(time.RFC3339Nano), items[len(items)-1].ID)
		next = &c
	}
	out.ListResponse = httpx.NewList(items, next)
	return out, nil
}

func (s *Service) MarkRead(ctx context.Context, id *uuid.UUID) error {
	p, err := customer(ctx)
	if err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE bvrooms_notifications SET read_at = now() WHERE customer_id = $1 AND read_at IS NULL AND deleted_at IS NULL AND ($2::uuid IS NULL OR id = $2)`, p.UserID, id)
		return err
	})
}

func (s *Service) DeleteNotification(ctx context.Context, id uuid.UUID) error {
	p, err := customer(ctx)
	if err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE bvrooms_notifications SET deleted_at = now() WHERE customer_id = $1 AND id = $2 AND deleted_at IS NULL`, p.UserID, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Notifikasi")
		}
		return nil
	})
}

// ---------- Web Push subscription ----------

type PushSubscriptionInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (s *Service) SubscribePush(ctx context.Context, in PushSubscriptionInput) error {
	p, err := customer(ctx)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(in.Endpoint, "https://") || in.Keys.P256dh == "" || in.Keys.Auth == "" {
		return apperr.Validation("endpoint https, keys.p256dh, keys.auth wajib")
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO bvrooms_push_subscriptions (organization_id, customer_id, endpoint, p256dh, auth, user_agent)
			VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (endpoint) DO UPDATE SET customer_id = EXCLUDED.customer_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth, revoked_at = NULL`,
			p.OrganizationID, p.UserID, in.Endpoint, in.Keys.P256dh, in.Keys.Auth, p.UserAgent)
		return err
	})
}

func (s *Service) UnsubscribePush(ctx context.Context, endpoint string) error {
	p, err := customer(ctx)
	if err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE bvrooms_push_subscriptions SET revoked_at = now() WHERE customer_id = $1 AND ($2 = '' OR endpoint = $2)`, p.UserID, endpoint)
		return err
	})
}

// deliverPushTx: kirim notifikasi yang belum terkirim (dipanggil sweep per org).
func (s *Service) deliverPushTx(ctx context.Context, tx pgx.Tx) (int, error) {
	if s.Pusher == nil {
		return 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT n.id, n.title, n.body, n.type, b.booking_code, n.customer_id FROM bvrooms_notifications n LEFT JOIN bvrooms_bookings b ON b.id = n.booking_id
		WHERE n.delivered_push_at IS NULL AND n.deleted_at IS NULL AND n.created_at > now() - interval '1 day' ORDER BY n.created_at LIMIT 200`)
	if err != nil {
		return 0, err
	}
	type item struct {
		id       uuid.UUID
		payload  []byte
		customer uuid.UUID
	}
	var items []item
	for rows.Next() {
		var id, cid uuid.UUID
		var title, body, typ string
		var code *string
		if err := rows.Scan(&id, &title, &body, &typ, &code, &cid); err != nil {
			rows.Close()
			return 0, err
		}
		payload, _ := json.Marshal(map[string]any{"title": title, "body": body, "type": typ, "booking_code": code, "notification_id": id})
		items = append(items, item{id: id, payload: payload, customer: cid})
	}
	rows.Close()
	n := 0
	for _, it := range items {
		subs, err := tx.Query(ctx, `SELECT endpoint, p256dh, auth FROM bvrooms_push_subscriptions WHERE customer_id = $1 AND revoked_at IS NULL`, it.customer)
		if err != nil {
			return n, err
		}
		var list []PushSubscription
		for subs.Next() {
			var sub PushSubscription
			if err := subs.Scan(&sub.Endpoint, &sub.P256dh, &sub.Auth); err != nil {
				subs.Close()
				return n, err
			}
			list = append(list, sub)
		}
		subs.Close()
		for _, sub := range list {
			if err := s.Pusher.Send(ctx, sub, it.payload); err != nil {
				if errors.Is(err, ErrSubscriptionGone) {
					_, _ = tx.Exec(ctx, `UPDATE bvrooms_push_subscriptions SET revoked_at = now() WHERE endpoint = $1`, sub.Endpoint)
					continue
				}
				s.Log.Warn("bvrooms push", "err", err)
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_notifications SET delivered_push_at = now() WHERE id = $1`, it.id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ---------- hook transisi reservasi dari Front Office (hotel.Act) ----------

// onReservationTransition: reservasi milik booking BVRooms berubah status oleh staf → notifikasi customer & sinkron payment.
func (s *Service) onReservationTransition(ctx context.Context, tx pgx.Tx, r *hotel.Reservation, action, from, to string) error {
	if r.BVRoomsBookingID == nil {
		return nil
	}
	b, err := scanBooking(tx.QueryRow(ctx, bookingSelect+` WHERE b.id = $1`, *r.BVRoomsBookingID))
	if err != nil {
		return nil // booking tidak ditemukan (RLS/org lain) → abaikan
	}
	p, _ := authctx.From(ctx)
	orgID := b.orgID(ctx)
	switch to {
	case "confirmed":
		if b.PaymentStatus == "unpaid" {
			// Front Office mengonfirmasi tanpa verifikasi Finance (mis. bayar di tempat) → anggap pay_at_property
			_, _ = tx.Exec(ctx, `UPDATE bvrooms_bookings SET payment_status = 'pay_at_property', payment_deadline_at = NULL WHERE id = $1 AND payment_status = 'unpaid'`, b.ID)
		}
		return s.notifyTx(ctx, tx, orgID, b.CustomerID, "booking_confirmed", "Pemesanan Dikonfirmasi.", "Pemesanan kamu telah dikonfirmasi oleh pihak properti.", &b.ID, "success")
	case "checked_in":
		return s.notifyTx(ctx, tx, orgID, b.CustomerID, "checked_in", "Check In Berhasil.", "Nah, kamu sudah berhasil Check In, selamat menikmati malam Anda.", &b.ID, "success")
	case "checked_out":
		if b.PaymentStatus == "pay_at_property" {
			_, _ = tx.Exec(ctx, `UPDATE bvrooms_bookings SET payment_status = 'paid' WHERE id = $1`, b.ID)
			_, _ = tx.Exec(ctx, `UPDATE bvrooms_payments SET status = 'paid', paid_at = now(), verified_by = $2, verified_at = now() WHERE booking_id = $1 AND method_code = 'cash_on_site' AND status = 'pending'`, b.ID, actorID(p))
		}
		return s.notifyTx(ctx, tx, orgID, b.CustomerID, "checked_out", "Check Out Berhasil.", "Check Out sudah berhasil, sampai berjumpa kembali. Berhati-hati dalam berkendara.", &b.ID, "success")
	case "cancelled":
		var remaining int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM hotel_reservations WHERE bvrooms_booking_id = $1 AND status IN ('new','confirmed','checked_in') AND id <> $2`, b.ID, r.ID).Scan(&remaining)
		if remaining == 0 && b.StatusOverride == nil {
			paySt := b.PaymentStatus
			if paySt == "paid" {
				paySt = "refund_pending"
			} else if paySt == "pay_at_property" {
				paySt = "unpaid"
			}
			_, _ = tx.Exec(ctx, `UPDATE bvrooms_bookings SET status_override = 'cancelled', cancelled_at = now(), cancel_reason = $2, cancelled_by = 'property', payment_status = $3, payment_deadline_at = NULL WHERE id = $1`, b.ID, deref(r.CancelReason), paySt)
			_, _ = tx.Exec(ctx, `UPDATE bvrooms_payments SET status = 'cancelled' WHERE booking_id = $1 AND status IN ('pending','proof_submitted')`, b.ID)
		}
		return s.notifyTx(ctx, tx, orgID, b.CustomerID, "booking_cancelled", "Pemesanan Dibatalkan!", "Bookingan Kamu dibatalkan oleh pihak properti. Hubungi properti untuk informasi lebih lanjut.", &b.ID, "danger")
	case "no_show":
		return s.notifyTx(ctx, tx, orgID, b.CustomerID, "no_show", "Tidak Hadir.", "Kamu tercatat tidak hadir pada tanggal check-in. Booking ditutup oleh pihak properti.", &b.ID, "danger")
	}
	return nil
}

func actorID(p *authctx.Principal) *uuid.UUID {
	if p == nil || p.IsSystem || p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}
