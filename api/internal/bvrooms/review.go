package bvrooms

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
)

// ---------- review (bintang saja, §10) ----------

type ReviewInput struct {
	Stars int `json:"stars"`
}

// CreateReview: hanya saat CHECK OUT, satu kali per booking; agregat listing diperbarui dalam transaksi yang sama.
func (s *Service) CreateReview(ctx context.Context, code string, in ReviewInput) (*Review, error) {
	p, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	if in.Stars < 1 || in.Stars > 5 {
		return nil, apperr.Validation("stars harus 1..5").WithField("stars", "1..5")
	}
	var out *Review
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := s.bookingByCodeTx(ctx, tx, code)
		if err != nil {
			return err
		}
		if b.CustomerID != p.UserID {
			return apperr.NotFound("Booking")
		}
		if b.deriveStatus(s.now()) != StatusCheckOut {
			return apperr.Conflict("NOT_CHECKED_OUT", "Rating hanya bisa diberikan setelah check-out")
		}
		if b.ReviewedAt != nil {
			return apperr.Conflict("ALREADY_REVIEWED", "Booking ini sudah diberi rating")
		}
		name := firstName(p.FullName)
		var created time.Time
		if err := tx.QueryRow(ctx, `INSERT INTO bvrooms_reviews (organization_id, property_id, booking_id, customer_id, stars, display_name) VALUES ($1,$2,$3,$4,$5,$6) RETURNING created_at`,
			p.OrganizationID, b.PropertyID, b.ID, p.UserID, in.Stars, name).Scan(&created); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("ALREADY_REVIEWED", "Booking ini sudah diberi rating")
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_bookings SET reviewed_at = now() WHERE id = $1`, b.ID); err != nil {
			return err
		}
		if err := s.refreshRatingTx(ctx, tx, b.PropertyID); err != nil {
			return err
		}
		_ = audit.LogAs(ctx, tx, p.OrganizationID, nil, p.IP, p.UserAgent, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "bvrooms_review", EntityID: &b.ID, EntityLabel: b.Code, After: map[string]any{"stars": in.Stars}})
		out = &Review{Stars: in.Stars, DisplayName: name, CreatedAt: created}
		return nil
	})
	return out, err
}

func firstName(full string) string {
	parts := strings.Fields(strings.TrimSpace(full))
	if len(parts) == 0 {
		return "Tamu"
	}
	return parts[0]
}

// refreshRatingTx: rating_avg/count/hist di listing dari seluruh review properti.
func (s *Service) refreshRatingTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE bvrooms_property_listings l SET
		rating_avg = COALESCE((SELECT round(avg(stars)::numeric, 2) FROM bvrooms_reviews r WHERE r.property_id = l.property_id), 0),
		rating_count = (SELECT count(*) FROM bvrooms_reviews r WHERE r.property_id = l.property_id),
		rating_hist = ARRAY[
			(SELECT count(*) FROM bvrooms_reviews r WHERE r.property_id = l.property_id AND stars = 1),
			(SELECT count(*) FROM bvrooms_reviews r WHERE r.property_id = l.property_id AND stars = 2),
			(SELECT count(*) FROM bvrooms_reviews r WHERE r.property_id = l.property_id AND stars = 3),
			(SELECT count(*) FROM bvrooms_reviews r WHERE r.property_id = l.property_id AND stars = 4),
			(SELECT count(*) FROM bvrooms_reviews r WHERE r.property_id = l.property_id AND stars = 5)]::int[]
		WHERE l.property_id = $1`, propertyID)
	return err
}

// ---------- wishlist ----------

func (s *Service) Wishlist(ctx context.Context, q string) ([]PropertyCard, error) {
	p, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	q = "%" + strings.ToLower(strings.TrimSpace(q)) + "%"
	out := []PropertyCard{}
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, listingSelect+` JOIN bvrooms_wishlists w ON w.property_id = l.property_id AND w.customer_id = $1
			WHERE l.bvrooms_listed AND p.status = 'active' AND loc.deleted_at IS NULL
			  AND ($2 = '%%' OR lower(l.display_name) LIKE $2 OR lower(COALESCE(l.city,'')) LIKE $2 OR lower(COALESCE(l.address_line,'')) LIKE $2)
			ORDER BY w.created_at DESC`, p.UserID, q)
		if err != nil {
			return err
		}
		var ls []*listingRow
		for rows.Next() {
			l, err := scanListing(rows)
			if err != nil {
				rows.Close()
				return err
			}
			ls = append(ls, l)
		}
		rows.Close()
		for _, l := range ls {
			out = append(out, s.cardFromListingTx(ctx, tx, l, &p.UserID, nil, nil))
		}
		return nil
	})
	return out, err
}

func (s *Service) AddWishlist(ctx context.Context, propertyID uuid.UUID) error {
	p, err := customer(ctx)
	if err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		l, err := s.listingByIDTx(ctx, tx, propertyID)
		if err != nil || !l.Listed {
			return apperr.NotFound("Property")
		}
		_, err = tx.Exec(ctx, `INSERT INTO bvrooms_wishlists (organization_id, customer_id, property_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, p.OrganizationID, p.UserID, propertyID)
		return err
	})
}

func (s *Service) RemoveWishlist(ctx context.Context, propertyID uuid.UUID) error {
	p, err := customer(ctx)
	if err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM bvrooms_wishlists WHERE customer_id = $1 AND property_id = $2`, p.UserID, propertyID)
		return err
	})
}

var _ = authctx.From
