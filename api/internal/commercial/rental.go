package commercial

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
)

// ============ RENTAL LISTING (rental inventory + rate configuration daily/weekly/monthly) ============

type RentalListing struct {
	ID              uuid.UUID  `json:"id"`
	ListingCode     string     `json:"listing_code"`
	PropertyID      uuid.UUID  `json:"property_id"`
	UnitLocationID  uuid.UUID  `json:"unit_location_id"`
	UnitNumber      string     `json:"unit_number"`
	UnitName        string     `json:"unit_name"`
	FloorName       *string    `json:"floor_name"`
	OccupancyStatus string     `json:"unit_occupancy_status"`
	Title           string     `json:"title"`
	Description     *string    `json:"description"`
	RateDaily       *int64     `json:"rate_daily"`
	RateWeekly      *int64     `json:"rate_weekly"`
	RateMonthly     *int64     `json:"rate_monthly"`
	DepositAmount   int64      `json:"deposit_amount"`
	CurrencyCode    string     `json:"currency_code"`
	MinStayDays     int        `json:"min_stay_days"`
	MaxOccupants    *int       `json:"max_occupants"`
	Bedrooms        *int       `json:"bedrooms"`
	Bathrooms       *int       `json:"bathrooms"`
	AreaM2          *float64   `json:"area_m2"`
	Furnishing      *string    `json:"furnishing"`
	Features        []string   `json:"features"`
	AvailableFrom   *time.Time `json:"available_from"`
	AvailableUntil  *time.Time `json:"available_until"`
	Status          string     `json:"status"`
	Documents       []Document `json:"documents"`
	PublishedAt     *time.Time `json:"published_at"`
	ArchivedAt      *time.Time `json:"archived_at"`
	ActiveResID     *uuid.UUID `json:"active_reservation_id"` // rental active saat ini
	NextStart       *time.Time `json:"next_start_date"`
	OpenInquiries   int        `json:"open_inquiries"`
	AllowedActions  []string   `json:"allowed_actions"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Version         int        `json:"version"`
}

const rlSelect = `SELECT x.id, x.listing_code, x.property_id, x.unit_location_id, u.unit_number, l.name, pl.name, u.occupancy_status, x.title, x.description, x.rate_daily, x.rate_weekly, x.rate_monthly, x.deposit_amount, x.currency_code,
	x.min_stay_days, x.max_occupants, x.bedrooms, x.bathrooms, x.area_m2, x.furnishing, x.features, x.available_from::timestamptz, x.available_until::timestamptz, x.status, x.documents, x.published_at, x.archived_at,
	(SELECT id FROM unit_rental_reservations rr WHERE rr.listing_id = x.id AND rr.status = 'active' LIMIT 1),
	(SELECT min(start_date)::timestamptz FROM unit_rental_reservations rr WHERE rr.listing_id = x.id AND rr.status = 'reserved' AND rr.start_date >= current_date),
	(SELECT count(*) FROM unit_rental_reservations rr WHERE rr.listing_id = x.id AND rr.status = 'new'),
	x.created_at, x.updated_at, x.version
	FROM unit_rental_listings x JOIN units u ON u.location_id = x.unit_location_id JOIN locations l ON l.id = u.location_id LEFT JOIN locations pl ON pl.id = l.parent_id`

func scanRL(row pgx.Row) (*RentalListing, error) {
	var x RentalListing
	var docs []byte
	if err := row.Scan(&x.ID, &x.ListingCode, &x.PropertyID, &x.UnitLocationID, &x.UnitNumber, &x.UnitName, &x.FloorName, &x.OccupancyStatus, &x.Title, &x.Description, &x.RateDaily, &x.RateWeekly, &x.RateMonthly, &x.DepositAmount, &x.CurrencyCode,
		&x.MinStayDays, &x.MaxOccupants, &x.Bedrooms, &x.Bathrooms, &x.AreaM2, &x.Furnishing, &x.Features, &x.AvailableFrom, &x.AvailableUntil, &x.Status, &docs, &x.PublishedAt, &x.ArchivedAt,
		&x.ActiveResID, &x.NextStart, &x.OpenInquiries, &x.CreatedAt, &x.UpdatedAt, &x.Version); err != nil {
		return nil, err
	}
	x.CurrencyCode = strings.TrimSpace(x.CurrencyCode)
	x.Documents = []Document{}
	_ = json.Unmarshal(docs, &x.Documents)
	if x.Features == nil {
		x.Features = []string{}
	}
	switch x.Status {
	case "draft":
		x.AllowedActions = []string{"publish", "archive"}
	case "published":
		x.AllowedActions = []string{"unpublish", "archive"}
	case "archived":
		x.AllowedActions = []string{"publish"}
	}
	return &x, nil
}

type RentalListingInput struct {
	PropertyID     *uuid.UUID  `json:"property_id"`
	UnitLocationID *uuid.UUID  `json:"unit_location_id"`
	Title          *string     `json:"title"`
	Description    *string     `json:"description"`
	RateDaily      *int64      `json:"rate_daily"`
	RateWeekly     *int64      `json:"rate_weekly"`
	RateMonthly    *int64      `json:"rate_monthly"`
	DepositAmount  *int64      `json:"deposit_amount"`
	MinStayDays    *int        `json:"min_stay_days"`
	MaxOccupants   *int        `json:"max_occupants"`
	Bedrooms       *int        `json:"bedrooms"`
	Bathrooms      *int        `json:"bathrooms"`
	AreaM2         *float64    `json:"area_m2"`
	Furnishing     *string     `json:"furnishing"`
	Features       *[]string   `json:"features"`
	AvailableFrom  *string     `json:"available_from"`  // YYYY-MM-DD
	AvailableUntil *string     `json:"available_until"` // YYYY-MM-DD
	Documents      *[]Document `json:"documents"`
}

func validRates(d, w, m *int64) error {
	if d == nil && w == nil && m == nil {
		return apperr.Validation("minimal satu rate (rate_daily/rate_weekly/rate_monthly) wajib")
	}
	for k, v := range map[string]*int64{"rate_daily": d, "rate_weekly": w, "rate_monthly": m} {
		if v != nil && *v <= 0 {
			return apperr.Validation(k + " harus > 0").WithField(k, "harus > 0")
		}
	}
	return nil
}

func (s *Service) ListRentalListings(ctx context.Context, f ListingFilter, page httpx.Page) ([]RentalListing, *string, error) {
	p := authctx.Must(ctx)
	out := []RentalListing{}
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args, err := s.scopeWhere(ctx, p, "commercial.rental_listings.view", f.PropertyID, "x.property_id")
		if err != nil {
			return err
		}
		if f.PropertyID != nil {
			if err := s.requireRental(ctx, tx, *f.PropertyID); err != nil {
				return err
			}
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND x.status = ANY($%d)", len(args))
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (x.listing_code ILIKE $%d OR x.title ILIKE $%d OR u.unit_number ILIKE $%d)", len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (x.created_at, x.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, rlSelect+where+fmt.Sprintf(" ORDER BY x.created_at DESC, x.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanRL(rows)
			if err != nil {
				return err
			}
			out = append(out, *x)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	return out, next, err
}

func (s *Service) getRLTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*RentalListing, error) {
	x, err := scanRL(tx.QueryRow(ctx, rlSelect+` WHERE x.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Rental listing")
		}
		return nil, err
	}
	return x, nil
}

func (s *Service) GetRentalListing(ctx context.Context, id uuid.UUID) (*RentalListing, error) {
	var out *RentalListing
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getRLTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.rental_listings.view", x.PropertyID); err != nil {
			return err
		}
		out = x
		return nil
	})
	return out, err
}

func (s *Service) CreateRentalListing(ctx context.Context, in RentalListingInput) (*RentalListing, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil || in.UnitLocationID == nil {
		return nil, apperr.Validation("property_id dan unit_location_id wajib")
	}
	if err := iam.CanOnProperty(ctx, "commercial.rental_listings.create", *in.PropertyID); err != nil {
		return nil, err
	}
	if err := validRates(in.RateDaily, in.RateWeekly, in.RateMonthly); err != nil {
		return nil, err
	}
	if in.Furnishing != nil && !furnishings[*in.Furnishing] {
		return nil, apperr.Validation("furnishing harus unfurnished|semi_furnished|furnished")
	}
	if in.MinStayDays != nil && *in.MinStayDays <= 0 {
		return nil, apperr.Validation("min_stay_days harus > 0")
	}
	var from, until *time.Time
	if v := nilIfEmpty(in.AvailableFrom); v != nil {
		t, err := parseDate(*v, "available_from")
		if err != nil {
			return nil, err
		}
		from = &t
	}
	if v := nilIfEmpty(in.AvailableUntil); v != nil {
		t, err := parseDate(*v, "available_until")
		if err != nil {
			return nil, err
		}
		until = &t
	}
	if from != nil && until != nil && until.Before(*from) {
		return nil, apperr.Validation("available_until harus ≥ available_from")
	}
	var out *RentalListing
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireRental(ctx, tx, *in.PropertyID); err != nil {
			return err
		}
		u, err := s.unitTx(ctx, tx, *in.UnitLocationID)
		if err != nil {
			return err
		}
		if u.PropertyID != *in.PropertyID {
			return apperr.Validation("Unit bukan milik property ini").WithField("unit_location_id", "bukan milik property")
		}
		if u.UnitType == "hotel_room" {
			return apperr.Validation("Kamar hotel dikelola lewat Hotel Booking Management")
		}
		title := strings.TrimSpace(deref(in.Title))
		if title == "" {
			title = "Sewa Unit " + u.UnitNumber
		}
		docs := []Document{}
		if in.Documents != nil {
			if docs, err = normalizeDocs(*in.Documents); err != nil {
				return err
			}
		}
		feats := []string{}
		if in.Features != nil {
			feats = *in.Features
		}
		area := in.AreaM2
		if area == nil {
			area = u.AreaM2
		}
		loc := property.PropertyTimezone(ctx, tx, *in.PropertyID)
		code, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixRentalListing, time.Now(), loc)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO unit_rental_listings (organization_id, property_id, listing_code, unit_location_id, title, description, rate_daily, rate_weekly, rate_monthly, deposit_amount, min_stay_days, max_occupants, bedrooms, bathrooms, area_m2, furnishing, features, available_from, available_until, documents, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,COALESCE($10,0),COALESCE($11,1),$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$21) RETURNING id`,
			p.OrganizationID, *in.PropertyID, code, u.LocationID, title, nilIfEmpty(in.Description), in.RateDaily, in.RateWeekly, in.RateMonthly, in.DepositAmount, in.MinStayDays, in.MaxOccupants, in.Bedrooms, in.Bathrooms, area, in.Furnishing, feats, from, until, mustJSON(docs), p.UserID).Scan(&id); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("LISTING_EXISTS", "Unit "+u.UnitNumber+" sudah memiliki listing sewa aktif")
			}
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "unit_rental_listing", EntityID: &id, EntityLabel: code + " " + title})
		out, err = s.getRLTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) UpdateRentalListing(ctx context.Context, id uuid.UUID, in RentalListingInput, ifVersion *int) (*RentalListing, error) {
	p := authctx.Must(ctx)
	var out *RentalListing
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getRLTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.rental_listings.update", x.PropertyID); err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != x.Version {
			return apperr.StaleVersion()
		}
		d, w, m := x.RateDaily, x.RateWeekly, x.RateMonthly
		if in.RateDaily != nil {
			d = zeroToNil(in.RateDaily)
		}
		if in.RateWeekly != nil {
			w = zeroToNil(in.RateWeekly)
		}
		if in.RateMonthly != nil {
			m = zeroToNil(in.RateMonthly)
		}
		if err := validRates(d, w, m); err != nil {
			return err
		}
		if in.Furnishing != nil && !furnishings[*in.Furnishing] {
			return apperr.Validation("furnishing harus unfurnished|semi_furnished|furnished")
		}
		if in.MinStayDays != nil && *in.MinStayDays <= 0 {
			return apperr.Validation("min_stay_days harus > 0")
		}
		from, until := x.AvailableFrom, x.AvailableUntil
		if in.AvailableFrom != nil {
			if v := nilIfEmpty(in.AvailableFrom); v == nil {
				from = nil
			} else {
				t, err := parseDate(*v, "available_from")
				if err != nil {
					return err
				}
				from = &t
			}
		}
		if in.AvailableUntil != nil {
			if v := nilIfEmpty(in.AvailableUntil); v == nil {
				until = nil
			} else {
				t, err := parseDate(*v, "available_until")
				if err != nil {
					return err
				}
				until = &t
			}
		}
		if from != nil && until != nil && until.Before(*from) {
			return apperr.Validation("available_until harus ≥ available_from")
		}
		var docs any
		if in.Documents != nil {
			dd, err := normalizeDocs(*in.Documents)
			if err != nil {
				return err
			}
			docs = mustJSON(dd)
		}
		var feats any
		if in.Features != nil {
			feats = *in.Features
		}
		if _, err := tx.Exec(ctx, `UPDATE unit_rental_listings SET title = COALESCE(NULLIF(TRIM($2),''), title), description = COALESCE($3, description), rate_daily = $4, rate_weekly = $5, rate_monthly = $6, deposit_amount = COALESCE($7, deposit_amount),
			min_stay_days = COALESCE($8, min_stay_days), max_occupants = COALESCE($9, max_occupants), bedrooms = COALESCE($10, bedrooms), bathrooms = COALESCE($11, bathrooms), area_m2 = COALESCE($12, area_m2), furnishing = COALESCE($13, furnishing), features = COALESCE($14, features),
			available_from = $15, available_until = $16, documents = COALESCE($17, documents), updated_by = $18 WHERE id = $1`,
			id, deref(in.Title), in.Description, d, w, m, in.DepositAmount, in.MinStayDays, in.MaxOccupants, in.Bedrooms, in.Bathrooms, in.AreaM2, in.Furnishing, feats, from, until, docs, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "unit_rental_listing", EntityID: &id, EntityLabel: x.ListingCode, After: in})
		out, err = s.getRLTx(ctx, tx, id)
		return err
	})
	return out, err
}

func zeroToNil(v *int64) *int64 {
	if v == nil || *v == 0 {
		return nil
	}
	return v
}

// RentalListingAct: publish | unpublish | archive.
func (s *Service) RentalListingAct(ctx context.Context, id uuid.UUID, action string) (*RentalListing, error) {
	p := authctx.Must(ctx)
	var out *RentalListing
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getRLTx(ctx, tx, id)
		if err != nil {
			return err
		}
		perm := map[string]string{"publish": "commercial.rental_listings.publish", "unpublish": "commercial.rental_listings.publish", "archive": "commercial.rental_listings.archive"}[action]
		if perm == "" {
			return apperr.Validation("aksi tidak dikenal")
		}
		if err := iam.CanOnProperty(ctx, perm, x.PropertyID); err != nil {
			return err
		}
		if !has(x.AllowedActions, action) {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk listing berstatus %s", action, x.Status))
		}
		if err := s.requireRental(ctx, tx, x.PropertyID); err != nil {
			return err
		}
		var to string
		switch action {
		case "publish":
			to = "published"
			if _, err := tx.Exec(ctx, `UPDATE unit_rental_listings SET status = 'published', published_at = COALESCE(published_at, now()), archived_at = NULL, updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("LISTING_EXISTS", "Unit sudah memiliki listing sewa aktif lain")
				}
				return err
			}
		case "unpublish":
			to = "draft"
			if _, err := tx.Exec(ctx, `UPDATE unit_rental_listings SET status = 'draft', updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
		case "archive":
			to = "archived"
			var active int
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM unit_rental_reservations WHERE listing_id = $1 AND status IN ('reserved','active')`, id).Scan(&active)
			if active > 0 {
				return apperr.Conflict("RESERVATIONS_ACTIVE", fmt.Sprintf("%d reservasi sewa masih aktif pada listing ini", active))
			}
			if _, err := tx.Exec(ctx, `UPDATE unit_rental_listings SET status = 'archived', archived_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "unit_rental_listing", ObjectID: id, Action: audit.ActStatusChanged, From: x.Status, To: to})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "unit_rental_listing", EntityID: &id, EntityLabel: x.ListingCode, Before: map[string]any{"status": x.Status}, After: map[string]any{"status": to}})
		if action == "publish" && s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventRentalListingPublished, OrganizationID: p.OrganizationID, PropertyID: &x.PropertyID, ObjectType: "unit_rental_listing", ObjectID: id, ObjectLabel: x.ListingCode + " · " + x.Title, ActorUserID: &p.UserID, Payload: map[string]any{"domain": "management"}})
		}
		out, err = s.getRLTx(ctx, tx, id)
		return err
	})
	return out, err
}

// ============ RENTAL PERIOD / AVAILABILITY (WF-P1-009 Rental Period → Availability) ============

var rentalPeriods = map[string]bool{"daily": true, "weekly": true, "monthly": true}

// periodEnd: end_date eksklusif untuk count × periode dari start.
func periodEnd(start time.Time, period string, count int) time.Time {
	switch period {
	case "daily":
		return start.AddDate(0, 0, count)
	case "weekly":
		return start.AddDate(0, 0, 7*count)
	default:
		return start.AddDate(0, count, 0)
	}
}

func rateFor(x *RentalListing, period string) *int64 {
	switch period {
	case "daily":
		return x.RateDaily
	case "weekly":
		return x.RateWeekly
	default:
		return x.RateMonthly
	}
}

type Quote struct {
	ListingID     uuid.UUID `json:"listing_id"`
	UnitNumber    string    `json:"unit_number"`
	RentalPeriod  string    `json:"rental_period"`
	PeriodCount   int       `json:"period_count"`
	StartDate     time.Time `json:"start_date"`
	EndDate       time.Time `json:"end_date"`
	Days          int       `json:"days"`
	RateAmount    int64     `json:"rate_amount"`
	TotalAmount   int64     `json:"total_amount"`
	DepositAmount int64     `json:"deposit_amount"`
	CurrencyCode  string    `json:"currency_code"`
	Available     bool      `json:"available"`
	Reason        string    `json:"reason,omitempty"`
	Conflicts     []string  `json:"conflicts,omitempty"` // nomor reservasi yang bertabrakan
}

// quoteTx: harga + ketersediaan (reserved/active memblokir; inquiry 'new' tidak).
func (s *Service) quoteTx(ctx context.Context, tx pgx.Tx, x *RentalListing, period string, count int, start time.Time, excludeResID *uuid.UUID) (*Quote, error) {
	if !rentalPeriods[period] {
		return nil, apperr.Validation("rental_period harus daily|weekly|monthly").WithField("rental_period", "daily|weekly|monthly")
	}
	if count <= 0 {
		return nil, apperr.Validation("period_count harus > 0").WithField("period_count", "harus > 0")
	}
	rate := rateFor(x, period)
	if rate == nil {
		return nil, apperr.Validation(fmt.Sprintf("Listing tidak menawarkan sewa %s", period)).WithField("rental_period", "tidak ditawarkan")
	}
	start = dateOnly(start)
	end := periodEnd(start, period, count)
	q := &Quote{ListingID: x.ID, UnitNumber: x.UnitNumber, RentalPeriod: period, PeriodCount: count, StartDate: start, EndDate: end, Days: int(end.Sub(start).Hours() / 24), RateAmount: *rate, TotalAmount: *rate * int64(count), DepositAmount: x.DepositAmount, CurrencyCode: x.CurrencyCode, Available: true}
	if q.Days < x.MinStayDays {
		q.Available, q.Reason = false, fmt.Sprintf("Minimal sewa %d hari", x.MinStayDays)
		return q, nil
	}
	if x.Status != "published" {
		q.Available, q.Reason = false, "Listing belum dipublikasikan"
		return q, nil
	}
	if x.AvailableFrom != nil && start.Before(dateOnly(*x.AvailableFrom)) {
		q.Available, q.Reason = false, "Unit tersedia mulai "+x.AvailableFrom.Format("2006-01-02")
		return q, nil
	}
	if x.AvailableUntil != nil && end.After(dateOnly(*x.AvailableUntil).AddDate(0, 0, 1)) {
		q.Available, q.Reason = false, "Unit tersedia sampai "+x.AvailableUntil.Format("2006-01-02")
		return q, nil
	}
	rows, err := tx.Query(ctx, `SELECT reservation_number FROM unit_rental_reservations WHERE unit_location_id = $1 AND status IN ('reserved','active') AND stay && daterange($2::date, $3::date, '[)') AND ($4::uuid IS NULL OR id <> $4)`, x.UnitLocationID, start, end, excludeResID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		q.Conflicts = append(q.Conflicts, n)
	}
	if len(q.Conflicts) > 0 {
		q.Available, q.Reason = false, "Unit sudah dipesan pada periode tersebut"
	}
	return q, nil
}

func (s *Service) Availability(ctx context.Context, listingID uuid.UUID, period string, count int, start time.Time) (*Quote, error) {
	var out *Quote
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getRLTx(ctx, tx, listingID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.rental_reservations.view", x.PropertyID); err != nil {
			return err
		}
		if err := s.requireRental(ctx, tx, x.PropertyID); err != nil {
			return err
		}
		out, err = s.quoteTx(ctx, tx, x, period, count, start, nil)
		return err
	})
	return out, err
}

// ============ RENTAL RESERVATION (New → Reserved → Active → Completed | Cancelled) ============

type RentalReservation struct {
	ID                uuid.UUID  `json:"id"`
	ReservationNumber string     `json:"reservation_number"`
	PropertyID        uuid.UUID  `json:"property_id"`
	ListingID         uuid.UUID  `json:"listing_id"`
	ListingCode       string     `json:"listing_code"`
	ListingTitle      string     `json:"listing_title"`
	UnitLocationID    uuid.UUID  `json:"unit_location_id"`
	UnitNumber        string     `json:"unit_number"`
	ProspectName      string     `json:"prospect_name"`
	ProspectPhone     *string    `json:"prospect_phone"`
	ProspectEmail     *string    `json:"prospect_email"`
	Company           *string    `json:"company"`
	Occupants         int        `json:"occupants"`
	RentalPeriod      string     `json:"rental_period"`
	PeriodCount       int        `json:"period_count"`
	StartDate         time.Time  `json:"start_date"`
	EndDate           time.Time  `json:"end_date"`
	Days              int        `json:"days"`
	RateAmount        int64      `json:"rate_amount"`
	TotalAmount       int64      `json:"total_amount"`
	DepositAmount     int64      `json:"deposit_amount"`
	CurrencyCode      string     `json:"currency_code"`
	Status            string     `json:"status"`
	Source            string     `json:"source"`
	SpecialRequests   *string    `json:"special_requests"`
	Notes             *string    `json:"notes"`
	InvoiceID         *uuid.UUID `json:"invoice_id"`
	InvoiceNumber     *string    `json:"invoice_number"`
	InvoiceStatus     *string    `json:"invoice_status"`
	TenantID          *uuid.UUID `json:"tenant_id"`
	TenantCode        *string    `json:"tenant_code"`
	OccupantID        *uuid.UUID `json:"occupant_id"`
	TenantUserID      *uuid.UUID `json:"tenant_user_id"`
	ReservedAt        *time.Time `json:"reserved_at"`
	ActivatedAt       *time.Time `json:"activated_at"`
	CompletedAt       *time.Time `json:"completed_at"`
	CancelledAt       *time.Time `json:"cancelled_at"`
	CancelReason      *string    `json:"cancel_reason"`
	AllowedActions    []string   `json:"allowed_actions"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	Version           int        `json:"version"`
}

const rrSelect = `SELECT x.id, x.reservation_number, x.property_id, x.listing_id, li.listing_code, li.title, x.unit_location_id, u.unit_number, x.prospect_name, x.prospect_phone, x.prospect_email, x.company, x.occupants,
	x.rental_period, x.period_count, x.start_date::timestamptz, x.end_date::timestamptz, x.end_date - x.start_date, x.rate_amount, x.total_amount, x.deposit_amount, x.currency_code, x.status, x.source, x.special_requests, x.notes,
	x.invoice_id, inv.invoice_number, inv.status, x.tenant_id, t.tenant_code, x.occupant_id, x.tenant_user_id, x.reserved_at, x.activated_at, x.completed_at, x.cancelled_at, x.cancel_reason, x.created_at, x.updated_at, x.version
	FROM unit_rental_reservations x JOIN unit_rental_listings li ON li.id = x.listing_id JOIN units u ON u.location_id = x.unit_location_id LEFT JOIN invoices inv ON inv.id = x.invoice_id LEFT JOIN tenants t ON t.id = x.tenant_id`

func scanRR(row pgx.Row) (*RentalReservation, error) {
	var x RentalReservation
	if err := row.Scan(&x.ID, &x.ReservationNumber, &x.PropertyID, &x.ListingID, &x.ListingCode, &x.ListingTitle, &x.UnitLocationID, &x.UnitNumber, &x.ProspectName, &x.ProspectPhone, &x.ProspectEmail, &x.Company, &x.Occupants,
		&x.RentalPeriod, &x.PeriodCount, &x.StartDate, &x.EndDate, &x.Days, &x.RateAmount, &x.TotalAmount, &x.DepositAmount, &x.CurrencyCode, &x.Status, &x.Source, &x.SpecialRequests, &x.Notes,
		&x.InvoiceID, &x.InvoiceNumber, &x.InvoiceStatus, &x.TenantID, &x.TenantCode, &x.OccupantID, &x.TenantUserID, &x.ReservedAt, &x.ActivatedAt, &x.CompletedAt, &x.CancelledAt, &x.CancelReason, &x.CreatedAt, &x.UpdatedAt, &x.Version); err != nil {
		return nil, err
	}
	x.CurrencyCode = strings.TrimSpace(x.CurrencyCode)
	switch x.Status {
	case "new":
		x.AllowedActions = []string{"confirm", "cancel"}
	case "reserved":
		x.AllowedActions = []string{"activate", "cancel"}
	case "active":
		x.AllowedActions = []string{"complete"}
	default:
		x.AllowedActions = []string{}
	}
	return &x, nil
}

var rentalSources = map[string]bool{"walk_in": true, "phone": true, "email": true, "website": true, "referral": true, "agent": true, "tenant_app": true, "other": true}

type RentalReservationInput struct {
	ListingID       *uuid.UUID `json:"listing_id"`
	ProspectName    *string    `json:"prospect_name"`
	ProspectPhone   *string    `json:"prospect_phone"`
	ProspectEmail   *string    `json:"prospect_email"`
	Company         *string    `json:"company"`
	Occupants       *int       `json:"occupants"`
	RentalPeriod    *string    `json:"rental_period"` // daily | weekly | monthly
	PeriodCount     *int       `json:"period_count"`
	StartDate       *string    `json:"start_date"` // YYYY-MM-DD
	Source          *string    `json:"source"`
	SpecialRequests *string    `json:"special_requests"`
	Notes           *string    `json:"notes"`
	Confirm         bool       `json:"confirm"` // langsung Reserved (bukan inquiry)
}

type RentalResFilter struct {
	PropertyID *uuid.UUID
	ListingID  *uuid.UUID
	UnitID     *uuid.UUID
	Statuses   []string
	From, To   *time.Time
	Q          string
}

func (s *Service) ListRentalReservations(ctx context.Context, f RentalResFilter, page httpx.Page) ([]RentalReservation, *string, error) {
	p := authctx.Must(ctx)
	out := []RentalReservation{}
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args, err := s.scopeWhere(ctx, p, "commercial.rental_reservations.view", f.PropertyID, "x.property_id")
		if err != nil {
			return err
		}
		if f.PropertyID != nil {
			if err := s.requireRental(ctx, tx, *f.PropertyID); err != nil {
				return err
			}
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND x.status = ANY($%d)", len(args))
		}
		if f.ListingID != nil {
			args = append(args, *f.ListingID)
			where += fmt.Sprintf(" AND x.listing_id = $%d", len(args))
		}
		if f.UnitID != nil {
			args = append(args, *f.UnitID)
			where += fmt.Sprintf(" AND x.unit_location_id = $%d", len(args))
		}
		if f.From != nil && f.To != nil {
			args = append(args, *f.From, *f.To)
			where += fmt.Sprintf(" AND x.stay && daterange($%d::date, $%d::date, '[]')", len(args)-1, len(args))
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (x.reservation_number ILIKE $%d OR x.prospect_name ILIKE $%d OR x.prospect_phone ILIKE $%d OR u.unit_number ILIKE $%d)", len(args), len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (x.start_date::timestamptz, x.id) > ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, rrSelect+where+fmt.Sprintf(" ORDER BY x.start_date, x.id LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanRR(rows)
			if err != nil {
				return err
			}
			out = append(out, *x)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.StartDate.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	return out, next, err
}

func (s *Service) getRRTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*RentalReservation, error) {
	x, err := scanRR(tx.QueryRow(ctx, rrSelect+` WHERE x.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Rental reservation")
		}
		return nil, err
	}
	return x, nil
}

func (s *Service) GetRentalReservation(ctx context.Context, id uuid.UUID) (*RentalReservation, error) {
	var out *RentalReservation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getRRTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.rental_reservations.view", x.PropertyID); err != nil {
			return err
		}
		out = x
		return nil
	})
	return out, err
}

// CreateRentalReservation: inquiry (New) atau langsung Reserved (confirm=true; konflik periode → 409 UNIT_UNAVAILABLE).
func (s *Service) CreateRentalReservation(ctx context.Context, in RentalReservationInput) (*RentalReservation, error) {
	p := authctx.Must(ctx)
	if in.ListingID == nil {
		return nil, apperr.Validation("listing_id wajib")
	}
	name := strings.TrimSpace(deref(in.ProspectName))
	if name == "" {
		return nil, apperr.Validation("prospect_name wajib").WithField("prospect_name", "wajib")
	}
	if in.RentalPeriod == nil || in.PeriodCount == nil || in.StartDate == nil {
		return nil, apperr.Validation("rental_period, period_count, dan start_date wajib")
	}
	start, err := parseDate(*in.StartDate, "start_date")
	if err != nil {
		return nil, err
	}
	src := deref(in.Source)
	if src == "" {
		src = "walk_in"
	}
	if !rentalSources[src] {
		return nil, apperr.Validation("source tidak valid")
	}
	occ := 1
	if in.Occupants != nil {
		if *in.Occupants <= 0 {
			return nil, apperr.Validation("occupants harus > 0")
		}
		occ = *in.Occupants
	}
	var out *RentalReservation
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		li, err := s.getRLTx(ctx, tx, *in.ListingID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.rental_reservations.create", li.PropertyID); err != nil {
			return err
		}
		if in.Confirm && !p.HasOnProperty("commercial.rental_reservations.confirm", li.PropertyID) {
			return apperr.Forbidden("Tidak memiliki izin mengonfirmasi reservasi sewa")
		}
		if err := s.requireRental(ctx, tx, li.PropertyID); err != nil {
			return err
		}
		if li.MaxOccupants != nil && occ > *li.MaxOccupants {
			return apperr.Validation(fmt.Sprintf("Maksimal %d penghuni untuk unit ini", *li.MaxOccupants)).WithField("occupants", "melebihi kapasitas")
		}
		q, err := s.quoteTx(ctx, tx, li, *in.RentalPeriod, *in.PeriodCount, start, nil)
		if err != nil {
			return err
		}
		if in.Confirm && !q.Available {
			return apperr.Conflict("UNIT_UNAVAILABLE", q.Reason)
		}
		if q.Days < li.MinStayDays {
			return apperr.Validation(q.Reason)
		}
		status := "new"
		if in.Confirm {
			status = "reserved"
		}
		loc := property.PropertyTimezone(ctx, tx, li.PropertyID)
		num, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixRentalReservation, time.Now(), loc)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO unit_rental_reservations (organization_id, property_id, reservation_number, listing_id, unit_location_id, prospect_name, prospect_phone, prospect_email, company, occupants, rental_period, period_count, start_date, end_date, rate_amount, total_amount, deposit_amount, currency_code, status, source, special_requests, notes, reserved_at, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,CASE WHEN $19 = 'reserved' THEN now() END,$23,$23) RETURNING id`,
			p.OrganizationID, li.PropertyID, num, li.ID, li.UnitLocationID, name, nilIfEmpty(in.ProspectPhone), nilIfEmpty(in.ProspectEmail), nilIfEmpty(in.Company), occ, q.RentalPeriod, q.PeriodCount, q.StartDate, q.EndDate, q.RateAmount, q.TotalAmount, q.DepositAmount, li.CurrencyCode, status, src, nilIfEmpty(in.SpecialRequests), nilIfEmpty(in.Notes), p.UserID).Scan(&id); err != nil {
			if db.IsExclusionViolation(err) {
				return apperr.Conflict("UNIT_UNAVAILABLE", "Unit sudah dipesan pada periode tersebut")
			}
			return err
		}
		if status == "reserved" {
			if err := s.afterReserveTx(ctx, tx, p, id, li.PropertyID, li.UnitLocationID); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "unit_rental_reservation", EntityID: &id, EntityLabel: num + " " + li.UnitNumber, After: map[string]any{"period": q.RentalPeriod, "count": q.PeriodCount, "start": q.StartDate, "status": status}})
		if s.Jobs != nil {
			ev := EventRentalReservationCreated
			if status == "reserved" {
				ev = EventRentalReservationConfirmed
			}
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: ev, OrganizationID: p.OrganizationID, PropertyID: &li.PropertyID, ObjectType: "unit_rental_reservation", ObjectID: id, ObjectLabel: num + " · Unit " + li.UnitNumber + " · " + name, ActorUserID: &p.UserID, Payload: map[string]any{"to": status, "domain": "management"}})
		}
		out, err = s.getRRTx(ctx, tx, id)
		return err
	})
	return out, err
}

// afterReserveTx: unit → reserved (bila vacant) + invoice sewa+deposit (Billing aktif).
func (s *Service) afterReserveTx(ctx context.Context, tx pgx.Tx, p *authctx.Principal, id, propertyID, unitID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE units SET occupancy_status = 'reserved' WHERE location_id = $1 AND occupancy_status = 'vacant'`, unitID); err != nil {
		return err
	}
	x, err := s.getRRTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if x.InvoiceID == nil && s.Billing != nil && x.TotalAmount+x.DepositAmount > 0 && p.HasOnProperty("billing.invoices.create", propertyID) {
		pc, _ := s.Profile.ResolveTx(ctx, tx, propertyID)
		if pc != nil && pc.Has(profile.CapBilling) {
			periodLabel := map[string]string{"daily": "hari", "weekly": "minggu", "monthly": "bulan"}[x.RentalPeriod]
			items := []billing.Item{{Description: fmt.Sprintf("Sewa unit %s · %d %s (%s – %s)", x.UnitNumber, x.PeriodCount, periodLabel, x.StartDate.Format("02 Jan 2006"), x.EndDate.AddDate(0, 0, -1).Format("02 Jan 2006")), Quantity: float64(x.PeriodCount), Unit: strPtr(periodLabel), UnitPrice: x.RateAmount, Amount: x.TotalAmount}}
			if x.DepositAmount > 0 {
				items = append(items, billing.Item{Description: "Deposit sewa unit " + x.UnitNumber, Quantity: 1, UnitPrice: x.DepositAmount, Amount: x.DepositAmount})
			}
			due := x.StartDate
			if due.Before(time.Now()) {
				due = time.Now().Add(24 * time.Hour)
			}
			desc := "Reservasi sewa " + x.ReservationNumber + " · " + x.ProspectName
			ps, pe := x.StartDate.Format("2006-01-02"), x.EndDate.AddDate(0, 0, -1).Format("2006-01-02")
			invID, err := s.Billing.CreateTx(ctx, tx, billing.InvoiceInput{PropertyID: &propertyID, UnitLocationID: &unitID, InvoiceType: strPtr("rental"), PeriodStart: &ps, PeriodEnd: &pe, Description: &desc, DueAt: &due, Items: &items, IssueNow: true}, "rental", &id)
			if err != nil {
				return err
			}
			_, _ = tx.Exec(ctx, `UPDATE unit_rental_reservations SET invoice_id = $2 WHERE id = $1`, id, invID)
		}
	}
	return nil
}

func (s *Service) UpdateRentalReservation(ctx context.Context, id uuid.UUID, in RentalReservationInput, ifVersion *int) (*RentalReservation, error) {
	p := authctx.Must(ctx)
	var out *RentalReservation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getRRTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "commercial.rental_reservations.update", x.PropertyID); err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != x.Version {
			return apperr.StaleVersion()
		}
		if x.Status == "completed" || x.Status == "cancelled" {
			return apperr.InvalidTransition("Reservasi berstatus " + x.Status + " tidak dapat diubah")
		}
		if in.Source != nil && !rentalSources[*in.Source] {
			return apperr.Validation("source tidak valid")
		}
		// perubahan periode hanya saat new/reserved (active: periode terkunci)
		period, count, start := x.RentalPeriod, x.PeriodCount, x.StartDate
		changed := false
		if in.RentalPeriod != nil && *in.RentalPeriod != period {
			period, changed = *in.RentalPeriod, true
		}
		if in.PeriodCount != nil && *in.PeriodCount != count {
			count, changed = *in.PeriodCount, true
		}
		if in.StartDate != nil {
			t, err := parseDate(*in.StartDate, "start_date")
			if err != nil {
				return err
			}
			if !t.Equal(dateOnly(start)) {
				start, changed = t, true
			}
		}
		if changed {
			if x.Status == "active" {
				return apperr.InvalidTransition("Periode sewa aktif tidak dapat diubah; selesaikan lalu buat reservasi baru")
			}
			li, err := s.getRLTx(ctx, tx, x.ListingID)
			if err != nil {
				return err
			}
			q, err := s.quoteTx(ctx, tx, li, period, count, start, &x.ID)
			if err != nil {
				return err
			}
			if x.Status == "reserved" && !q.Available {
				return apperr.Conflict("UNIT_UNAVAILABLE", q.Reason)
			}
			if _, err := tx.Exec(ctx, `UPDATE unit_rental_reservations SET rental_period = $2, period_count = $3, start_date = $4, end_date = $5, rate_amount = $6, total_amount = $7, updated_by = $8 WHERE id = $1`, id, q.RentalPeriod, q.PeriodCount, q.StartDate, q.EndDate, q.RateAmount, q.TotalAmount, p.UserID); err != nil {
				if db.IsExclusionViolation(err) {
					return apperr.Conflict("UNIT_UNAVAILABLE", "Unit sudah dipesan pada periode tersebut")
				}
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE unit_rental_reservations SET prospect_name = COALESCE(NULLIF(TRIM($2),''), prospect_name), prospect_phone = COALESCE($3, prospect_phone), prospect_email = COALESCE($4, prospect_email), company = COALESCE($5, company), occupants = COALESCE($6, occupants), source = COALESCE($7, source), special_requests = COALESCE($8, special_requests), notes = COALESCE($9, notes), updated_by = $10 WHERE id = $1`,
			id, deref(in.ProspectName), in.ProspectPhone, in.ProspectEmail, in.Company, in.Occupants, in.Source, in.SpecialRequests, in.Notes, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "unit_rental_reservation", EntityID: &id, EntityLabel: x.ReservationNumber, After: in})
		out, err = s.getRRTx(ctx, tx, id)
		return err
	})
	return out, err
}

type RentalActionInput struct {
	Reason string `json:"reason"`
	// activate: Tenant Onboarding (Tenant/Occupant bersama) + akun Tenant App opsional (AC-11)
	TenantName    *string `json:"tenant_name"`
	TenantPhone   *string `json:"tenant_phone"`
	TenantEmail   *string `json:"tenant_email"`
	Company       *string `json:"company"`
	TenantType    *string `json:"tenant_type"` // individual | company
	CreateAccount bool    `json:"create_tenant_account"`
	Password      string  `json:"password"`
	MoveInDate    *string `json:"move_in_date"`  // YYYY-MM-DD (default start_date)
	MoveOutDate   *string `json:"move_out_date"` // complete: default hari ini
}

type RentalActionResult struct {
	Reservation *RentalReservation `json:"reservation"`
	Onboarding  *OnboardingResult  `json:"onboarding,omitempty"`
}

// RentalAct: confirm (New→Reserved) | activate (Reserved→Active + Tenant Onboarding) | complete (Active→Completed + move-out) | cancel.
func (s *Service) RentalAct(ctx context.Context, id uuid.UUID, action string, in RentalActionInput) (*RentalActionResult, error) {
	p := authctx.Must(ctx)
	res := &RentalActionResult{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		x, err := s.getRRTx(ctx, tx, id)
		if err != nil {
			return err
		}
		perm := map[string]string{"confirm": "commercial.rental_reservations.confirm", "activate": "commercial.rental_reservations.activate", "complete": "commercial.rental_reservations.complete", "cancel": "commercial.rental_reservations.cancel"}[action]
		if perm == "" {
			return apperr.Validation("aksi tidak dikenal")
		}
		if err := iam.CanOnProperty(ctx, perm, x.PropertyID); err != nil {
			return err
		}
		if !has(x.AllowedActions, action) {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk reservasi berstatus %s", action, x.Status))
		}
		if err := s.requireRental(ctx, tx, x.PropertyID); err != nil {
			return err
		}
		var to, ev string
		switch action {
		case "confirm":
			to, ev = "reserved", EventRentalReservationConfirmed
			li, err := s.getRLTx(ctx, tx, x.ListingID)
			if err != nil {
				return err
			}
			q, err := s.quoteTx(ctx, tx, li, x.RentalPeriod, x.PeriodCount, x.StartDate, &x.ID)
			if err != nil {
				return err
			}
			if !q.Available {
				return apperr.Conflict("UNIT_UNAVAILABLE", q.Reason)
			}
			if _, err := tx.Exec(ctx, `UPDATE unit_rental_reservations SET status = 'reserved', reserved_at = now(), rate_amount = $2, total_amount = $3, deposit_amount = $4, updated_by = $5 WHERE id = $1`, id, q.RateAmount, q.TotalAmount, q.DepositAmount, p.UserID); err != nil {
				if db.IsExclusionViolation(err) {
					return apperr.Conflict("UNIT_UNAVAILABLE", "Unit sudah dipesan pada periode tersebut")
				}
				return err
			}
			if err := s.afterReserveTx(ctx, tx, p, id, x.PropertyID, x.UnitLocationID); err != nil {
				return err
			}
		case "activate":
			to, ev = "active", EventRentalReservationActivated
			moveIn := x.StartDate
			if v := nilIfEmpty(in.MoveInDate); v != nil {
				if moveIn, err = parseDate(*v, "move_in_date"); err != nil {
					return err
				}
			}
			var occupied int
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM unit_rental_reservations WHERE unit_location_id = $1 AND status = 'active' AND id <> $2`, x.UnitLocationID, id).Scan(&occupied)
			if occupied > 0 {
				return apperr.Conflict("UNIT_OCCUPIED", "Unit masih ditempati penyewa aktif lain; selesaikan sewa sebelumnya")
			}
			name := deref(nilIfEmpty(in.TenantName))
			if name == "" {
				name = x.ProspectName
			}
			phone, email := deref(nilIfEmpty(in.TenantPhone)), deref(nilIfEmpty(in.TenantEmail))
			if phone == "" {
				phone = deref(x.ProspectPhone)
			}
			if email == "" {
				email = deref(x.ProspectEmail)
			}
			company := deref(nilIfEmpty(in.Company))
			if company == "" {
				company = deref(x.Company)
			}
			until := x.EndDate
			ob, err := s.onboardTx(ctx, tx, x.PropertyID, x.UnitLocationID, OnboardingInput{FullName: name, Phone: phone, Email: email, Company: company, TenantType: deref(in.TenantType), OwnershipStatus: "tenant", MovedInAt: dateOnly(moveIn),
				CreateAccount: in.CreateAccount, Password: in.Password, Source: "rental_onboarding", Notes: fmt.Sprintf("Sewa %s · %s – %s", x.ReservationNumber, x.StartDate.Format("2006-01-02"), x.EndDate.AddDate(0, 0, -1).Format("2006-01-02"))}, &until)
			if err != nil {
				return err
			}
			res.Onboarding = ob
			if _, err := tx.Exec(ctx, `UPDATE unit_rental_reservations SET status = 'active', activated_at = now(), tenant_id = $2, occupant_id = $3, tenant_user_id = $4, updated_by = $5 WHERE id = $1`, id, ob.TenantID, ob.OccupantID, ob.TenantUserID, p.UserID); err != nil {
				return err
			}
			if x.InvoiceID != nil && ob.TenantID != uuid.Nil {
				_, _ = tx.Exec(ctx, `UPDATE invoices SET tenant_id = $2 WHERE id = $1 AND tenant_id IS NULL`, *x.InvoiceID, ob.TenantID)
			}
		case "complete":
			to, ev = "completed", EventRentalReservationCompleted
			moveOut := dateOnly(time.Now())
			if v := nilIfEmpty(in.MoveOutDate); v != nil {
				if moveOut, err = parseDate(*v, "move_out_date"); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE unit_rental_reservations SET status = 'completed', completed_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
			if err := s.offboardTx(ctx, tx, x.UnitLocationID, x.TenantID, x.OccupantID, x.TenantUserID, moveOut); err != nil {
				return err
			}
		case "cancel":
			to, ev = "cancelled", EventRentalReservationCancelled
			reason := strings.TrimSpace(in.Reason)
			if reason == "" {
				return apperr.Validation("reason wajib").WithField("reason", "wajib")
			}
			if _, err := tx.Exec(ctx, `UPDATE unit_rental_reservations SET status = 'cancelled', cancelled_at = now(), cancel_reason = $2, updated_by = $3 WHERE id = $1`, id, reason, p.UserID); err != nil {
				return err
			}
			if x.Status == "reserved" {
				// unit kembali vacant bila tidak ada reservasi lain yang menahannya
				_, _ = tx.Exec(ctx, `UPDATE units SET occupancy_status = 'vacant' WHERE location_id = $1 AND occupancy_status = 'reserved' AND tenant_id IS NULL AND NOT EXISTS (SELECT 1 FROM unit_rental_reservations r WHERE r.unit_location_id = $1 AND r.status = 'reserved' AND r.id <> $2) AND NOT EXISTS (SELECT 1 FROM unit_sale_reservations sr WHERE sr.unit_location_id = $1 AND sr.status IN ('reserved','contract_signed','sold'))`, x.UnitLocationID, id)
			}
			if x.InvoiceID != nil {
				_, _ = tx.Exec(ctx, `UPDATE invoices SET status = 'cancelled', updated_by = $2 WHERE id = $1 AND status IN ('draft','issued')`, *x.InvoiceID, p.UserID)
			}
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "unit_rental_reservation", ObjectID: id, Action: audit.ActStatusChanged, From: x.Status, To: to, Payload: map[string]any{"action": action, "reason": in.Reason}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "unit_rental_reservation", EntityID: &id, EntityLabel: x.ReservationNumber, Before: map[string]any{"status": x.Status}, After: map[string]any{"status": to, "reason": in.Reason}})
		if s.Jobs != nil {
			payload := map[string]any{"from": x.Status, "to": to, "reason": in.Reason, "domain": "management"}
			if res.Onboarding != nil && res.Onboarding.TenantUserID != nil {
				payload["tenant_user_id"] = res.Onboarding.TenantUserID.String()
			} else if x.TenantUserID != nil {
				payload["tenant_user_id"] = x.TenantUserID.String()
			}
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: ev, OrganizationID: p.OrganizationID, PropertyID: &x.PropertyID, ObjectType: "unit_rental_reservation", ObjectID: id, ObjectLabel: x.ReservationNumber + " · Unit " + x.UnitNumber + " · " + x.ProspectName, ActorUserID: &p.UserID, Payload: payload})
		}
		res.Reservation, err = s.getRRTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ---------- Rental calendar (unit × tanggal) ----------

type RentalCalendarEntry struct {
	ReservationID     uuid.UUID `json:"reservation_id"`
	ReservationNumber string    `json:"reservation_number"`
	ProspectName      string    `json:"prospect_name"`
	UnitLocationID    uuid.UUID `json:"unit_location_id"`
	UnitNumber        string    `json:"unit_number"`
	RentalPeriod      string    `json:"rental_period"`
	StartDate         time.Time `json:"start_date"`
	EndDate           time.Time `json:"end_date"`
	Status            string    `json:"status"`
}

type RentalCalendar struct {
	From     time.Time             `json:"from"`
	To       time.Time             `json:"to"`
	Listings []RentalListing       `json:"listings"`
	Entries  []RentalCalendarEntry `json:"entries"`
}

func (s *Service) RentalCalendarView(ctx context.Context, propertyID uuid.UUID, from, to time.Time) (*RentalCalendar, error) {
	if err := iam.CanOnProperty(ctx, "commercial.rental_reservations.view", propertyID); err != nil {
		return nil, err
	}
	out := &RentalCalendar{From: from, To: to, Listings: []RentalListing{}, Entries: []RentalCalendarEntry{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireRental(ctx, tx, propertyID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, rlSelect+` WHERE x.property_id = $1 AND x.status <> 'archived' ORDER BY u.unit_number`, propertyID)
		if err != nil {
			return err
		}
		for rows.Next() {
			x, err := scanRL(rows)
			if err != nil {
				rows.Close()
				return err
			}
			out.Listings = append(out.Listings, *x)
		}
		rows.Close()
		rows, err = tx.Query(ctx, `SELECT x.id, x.reservation_number, x.prospect_name, x.unit_location_id, u.unit_number, x.rental_period, x.start_date::timestamptz, x.end_date::timestamptz, x.status
			FROM unit_rental_reservations x JOIN units u ON u.location_id = x.unit_location_id WHERE x.property_id = $1 AND x.status IN ('new','reserved','active') AND x.stay && daterange($2::date, $3::date, '[]') ORDER BY x.start_date`, propertyID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e RentalCalendarEntry
			if err := rows.Scan(&e.ReservationID, &e.ReservationNumber, &e.ProspectName, &e.UnitLocationID, &e.UnitNumber, &e.RentalPeriod, &e.StartDate, &e.EndDate, &e.Status); err != nil {
				return err
			}
			out.Entries = append(out.Entries, e)
		}
		return rows.Err()
	})
	return out, err
}

// ---------- Rental summary ----------

type RentalSummary struct {
	Listings      map[string]int `json:"listings"`
	Reservations  map[string]int `json:"reservations"`
	ActiveRentals int            `json:"active_rentals"`
	EndingSoon    int            `json:"ending_soon"` // active, end_date ≤ 7 hari
	UpcomingStart int            `json:"upcoming_start"` // reserved, start ≤ 7 hari
	OpenInquiries int            `json:"open_inquiries"`
	OccupancyPct  float64        `json:"occupancy_pct"` // unit dengan listing sewa yang aktif disewa
}

func (s *Service) RentalSummaryView(ctx context.Context, propertyID uuid.UUID) (*RentalSummary, error) {
	if err := iam.CanOnProperty(ctx, "commercial.rental_reservations.view", propertyID); err != nil {
		return nil, err
	}
	out := &RentalSummary{Listings: map[string]int{}, Reservations: map[string]int{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireRental(ctx, tx, propertyID); err != nil {
			return err
		}
		if err := countBy(ctx, tx, `SELECT status, count(*) FROM unit_rental_listings WHERE property_id = $1 GROUP BY status`, propertyID, out.Listings); err != nil {
			return err
		}
		if err := countBy(ctx, tx, `SELECT status, count(*) FROM unit_rental_reservations WHERE property_id = $1 GROUP BY status`, propertyID, out.Reservations); err != nil {
			return err
		}
		var published int
		if err := tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM unit_rental_reservations WHERE property_id = $1 AND status = 'active'),
			(SELECT count(*) FROM unit_rental_reservations WHERE property_id = $1 AND status = 'active' AND end_date <= current_date + 7),
			(SELECT count(*) FROM unit_rental_reservations WHERE property_id = $1 AND status = 'reserved' AND start_date <= current_date + 7),
			(SELECT count(*) FROM unit_rental_reservations WHERE property_id = $1 AND status = 'new'),
			(SELECT count(*) FROM unit_rental_listings WHERE property_id = $1 AND status = 'published')`, propertyID).Scan(&out.ActiveRentals, &out.EndingSoon, &out.UpcomingStart, &out.OpenInquiries, &published); err != nil {
			return err
		}
		if published > 0 {
			out.OccupancyPct = float64(out.ActiveRentals) / float64(published) * 100
		}
		return nil
	})
	return out, err
}
