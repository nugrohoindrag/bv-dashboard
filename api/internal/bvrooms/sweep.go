package bvrooms

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/authctx"
)

// Sweep (worker, tiap menit, per org): (1) booking UNPAID lewat batas bayar → EXPIRED (kecuali bukti sedang diverifikasi);
// (2) refresh min_rate_cache & popularity_score listing; (3) kirim Web Push yang tertunda.
func (s *Service) Sweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	var due []uuid.UUID
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT b.id FROM bvrooms_bookings b WHERE b.payment_status = 'unpaid' AND b.status_override IS NULL AND b.payment_deadline_at IS NOT NULL AND b.payment_deadline_at < now()
			AND NOT EXISTS (SELECT 1 FROM bvrooms_payments p WHERE p.booking_id = b.id AND p.status = 'proof_submitted') ORDER BY b.payment_deadline_at LIMIT 200`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			due = append(due, id)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range due {
		err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
			b, err := scanBooking(tx.QueryRow(ctx, bookingSelect+` WHERE b.id = $1 FOR UPDATE OF b`, id))
			if err != nil {
				return err
			}
			if b.PaymentStatus != "unpaid" || b.StatusOverride != nil {
				return nil
			}
			return s.expireBookingTx(ctx, tx, b, "Batas waktu pembayaran terlewati")
		})
		if err != nil {
			s.Log.Warn("bvrooms sweep expire", "booking", id, "err", err)
			continue
		}
		n++
	}
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.refreshListingCachesTx(ctx, tx); err != nil {
			return err
		}
		_, err := s.deliverPushTx(ctx, tx)
		return err
	})
	return n, err
}

// refreshListingCachesTx: min_rate_cache = rate termurah aktif (base_rate/hotel_rates) tipe aktif;
// popularity = booking 90 hari terakhir (bukan cancelled/expired) + bobot rating.
func (s *Service) refreshListingCachesTx(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE bvrooms_property_listings l SET
		min_rate_cache = (
			SELECT min(x.rate) FROM (
				SELECT LEAST(rt.base_rate, COALESCE((SELECT min(ra.rate_per_night) FROM hotel_rates ra WHERE ra.room_type_id = rt.id AND ra.status = 'active' AND (ra.valid_until IS NULL OR ra.valid_until >= current_date)), rt.base_rate)) AS rate
				FROM hotel_room_types rt WHERE rt.property_id = l.property_id AND rt.status = 'active' AND l.listing_category = 'hotel'
				UNION ALL
				SELECT LEAST(ut.base_rate, COALESCE((SELECT min(ra.rate_per_night) FROM hotel_rates ra WHERE ra.unit_type_id = ut.id AND ra.status = 'active' AND (ra.valid_until IS NULL OR ra.valid_until >= current_date)), ut.base_rate))
				FROM bvrooms_unit_types ut WHERE ut.property_id = l.property_id AND ut.status = 'active' AND l.listing_category = 'apartment') x),
		min_rate_cached_at = now(),
		popularity_score = (SELECT count(*) FROM bvrooms_bookings b WHERE b.property_id = l.property_id AND b.created_at > now() - interval '90 days' AND b.payment_status NOT IN ('expired') AND b.status_override IS NULL) + l.rating_avg * 2`)
	return err
}
