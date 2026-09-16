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
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
)

// ---------- status customer-facing (status-map: bvrooms_booking_customer) ----------

const (
	StatusUnpaid    = "UNPAID"
	StatusPaid      = "PAID"
	StatusCheckIn   = "CHECK IN"
	StatusCheckOut  = "CHECK OUT"
	StatusCancelled = "CANCELLED"
	StatusExpired   = "EXPIRED"
)

type bookingRow struct {
	ID             uuid.UUID
	PropertyID     uuid.UUID
	Code           string
	CustomerID     uuid.UUID
	GuestName      string
	GuestEmail     *string
	GuestPhone     *string
	CheckIn        time.Time
	CheckOut       time.Time
	Nights         int
	RoomsCount     int
	Adults         int
	Children       int
	RoomSubtotal   int64
	AddonSubtotal  int64
	Discount       int64
	PromotionID    *uuid.UUID
	Total          int64
	Currency       string
	PaymentStatus  string
	Deadline       *time.Time
	StatusOverride *string
	CancelledAt    *time.Time
	CancelReason   *string
	CancelledBy    *string
	ReviewedAt     *time.Time
	CreatedAt      time.Time
	Version        int
	resStatuses    []string
}

const bookingSelect = `SELECT b.id, b.property_id, b.booking_code, b.customer_id, b.guest_full_name, b.guest_email, b.guest_phone, b.check_in_date, b.check_out_date, b.nights, b.rooms_count, b.adults_total, b.children_total,
	b.room_subtotal, b.addon_subtotal, b.discount_amount, b.promotion_id, b.total_amount, b.currency_code, b.payment_status, b.payment_deadline_at, b.status_override, b.cancelled_at, b.cancel_reason, b.cancelled_by, b.reviewed_at, b.created_at, b.version,
	COALESCE((SELECT array_agg(r.status ORDER BY br.sort_order) FROM bvrooms_booking_rooms br JOIN hotel_reservations r ON r.id = br.reservation_id WHERE br.booking_id = b.id), '{}')
	FROM bvrooms_bookings b`

func scanBooking(row pgx.Row) (*bookingRow, error) {
	var b bookingRow
	if err := row.Scan(&b.ID, &b.PropertyID, &b.Code, &b.CustomerID, &b.GuestName, &b.GuestEmail, &b.GuestPhone, &b.CheckIn, &b.CheckOut, &b.Nights, &b.RoomsCount, &b.Adults, &b.Children,
		&b.RoomSubtotal, &b.AddonSubtotal, &b.Discount, &b.PromotionID, &b.Total, &b.Currency, &b.PaymentStatus, &b.Deadline, &b.StatusOverride, &b.CancelledAt, &b.CancelReason, &b.CancelledBy, &b.ReviewedAt, &b.CreatedAt, &b.Version, &b.resStatuses); err != nil {
		return nil, err
	}
	b.Currency = strings.TrimSpace(b.Currency)
	return &b, nil
}

// deriveStatus (§4.1): dari payment_status + status reservasi.
func (b *bookingRow) deriveStatus(now time.Time) string {
	if b.PaymentStatus == "expired" || (b.StatusOverride != nil && *b.StatusOverride == "expired") {
		return StatusExpired
	}
	if b.StatusOverride != nil && *b.StatusOverride == "cancelled" {
		return StatusCancelled
	}
	all := func(st ...string) bool {
		if len(b.resStatuses) == 0 {
			return false
		}
		for _, r := range b.resStatuses {
			ok := false
			for _, s := range st {
				if r == s {
					ok = true
				}
			}
			if !ok {
				return false
			}
		}
		return true
	}
	any := func(st string) bool {
		for _, r := range b.resStatuses {
			if r == st {
				return true
			}
		}
		return false
	}
	switch {
	case all("cancelled", "no_show"):
		return StatusCancelled
	case any("checked_in"):
		return StatusCheckIn
	case all("checked_out", "cancelled", "no_show") && any("checked_out"):
		return StatusCheckOut
	case b.PaymentStatus == "unpaid":
		if b.Deadline != nil && now.After(*b.Deadline) {
			return StatusExpired
		}
		return StatusUnpaid
	default:
		return StatusPaid
	}
}

func (b *bookingRow) checkInAt(loc *time.Location, checkInTime string) time.Time {
	h, m := 14, 0
	_, _ = fmt.Sscanf(checkInTime, "%d:%d", &h, &m)
	return time.Date(b.CheckIn.Year(), b.CheckIn.Month(), b.CheckIn.Day(), h, m, 0, 0, loc)
}

// ---------- DTO ----------

type BookingRoomAddon struct {
	ID          uuid.UUID  `json:"id"`
	AddonID     *uuid.UUID `json:"addon_id"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	PricingUnit string     `json:"pricing_unit"`
	Qty         int        `json:"qty"`
	UnitPrice   int64      `json:"unit_price"`
	LineTotal   int64      `json:"line_total"`
}

type BookingRoom struct {
	ID              uuid.UUID          `json:"id"`
	ReservationID   uuid.UUID          `json:"reservation_id"`
	ReservationNo   string             `json:"reservation_number"`
	TypeID          uuid.UUID          `json:"type_id"`
	Kind            string             `json:"kind"`
	TypeName        string             `json:"type_name"`
	Adults          int                `json:"adults"`
	Children        int                `json:"children"`
	RatePerNight    int64              `json:"rate_per_night"`
	Nights          int                `json:"nights"`
	DiscountAmount  int64              `json:"discount_amount"`
	LineTotal       int64              `json:"line_total"`
	ReservationStat string             `json:"reservation_status"`
	AssignedNumber  *string            `json:"assigned_number"` // nomor kamar/unit setelah assignment
	Addons          []BookingRoomAddon `json:"addons"`
}

type BookingProperty struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	DisplayName string    `json:"display_name"`
	Category    string    `json:"listing_category"`
	City        *string   `json:"city"`
	AddressLine *string   `json:"address_line"`
	CoverURL    *string   `json:"cover_photo_url"`
	Lat         *float64  `json:"lat"`
	Lng         *float64  `json:"lng"`
	Phone       *string   `json:"phone"`
	WhatsApp    *string   `json:"whatsapp"`
	Policies    []string  `json:"policies"`
	CancelMD    *string   `json:"cancellation_policy_md"`
	Timezone    string    `json:"timezone"`
}

type Payment struct {
	ID             uuid.UUID       `json:"id"`
	ProviderCode   string          `json:"provider_code"`
	MethodCode     string          `json:"method_code"`
	Amount         int64           `json:"amount"`
	Status         string          `json:"status"`
	Instructions   json.RawMessage `json:"instructions"`
	VANumber       *string         `json:"va_number"`
	ExpiresAt      *time.Time      `json:"expires_at"`
	ProofRequired  bool            `json:"proof_upload_required"`
	ProofSubmitted *time.Time      `json:"proof_submitted_at"`
	ProofURL       *string         `json:"proof_url,omitempty"`
	PaidAt         *time.Time      `json:"paid_at"`
	VerifiedAt     *time.Time      `json:"verified_at"`
	Note           *string         `json:"note"`
	CreatedAt      time.Time       `json:"created_at"`
}

type Review struct {
	Stars       int       `json:"stars"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type Booking struct {
	ID              uuid.UUID       `json:"id"`
	BookingCode     string          `json:"booking_code"`
	Status          string          `json:"status"`
	PaymentStatus   string          `json:"payment_status"`
	PaymentDeadline *time.Time      `json:"payment_deadline_at"`
	Property        BookingProperty `json:"property"`
	Guest           GuestInput      `json:"guest"`
	CheckInDate     string          `json:"check_in_date"`
	CheckOutDate    string          `json:"check_out_date"`
	CheckInAt       time.Time       `json:"check_in_at"`
	CheckOutAt      time.Time       `json:"check_out_at"`
	Nights          int             `json:"nights"`
	RoomsCount      int             `json:"rooms_count"`
	GuestsTotal     int             `json:"guests_total"`
	Rooms           []BookingRoom   `json:"rooms"`
	Totals          BookingTotals   `json:"totals"`
	Currency        string          `json:"currency_code"`
	PrimaryAction   string          `json:"primary_action"` // pay | direct | rebook | none
	CanCancel       bool            `json:"can_cancel"`
	CanModifyGuest  bool            `json:"can_modify_guest"`
	CanReview       bool            `json:"can_review"`
	Review          *Review         `json:"review"`
	Payment         *Payment        `json:"payment"`
	CancelledAt     *time.Time      `json:"cancelled_at"`
	CancelReason    *string         `json:"cancel_reason"`
	CancelledBy     *string         `json:"cancelled_by"`
	CreatedAt       time.Time       `json:"created_at"`
	Customer        *Customer       `json:"customer,omitempty"` // hanya respons dashboard
	Version         int             `json:"version"`
}

type BookingTotals struct {
	Room     int64 `json:"room"`
	Addon    int64 `json:"addon"`
	Discount int64 `json:"discount"`
	Total    int64 `json:"total"`
}

type GuestInput struct {
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
}

type BookingCard struct {
	ID            uuid.UUID `json:"id"`
	BookingCode   string    `json:"booking_code"`
	Status        string    `json:"status"`
	PaymentStatus string    `json:"payment_status"`
	Property      struct {
		Slug        string   `json:"slug"`
		DisplayName string   `json:"display_name"`
		City        *string  `json:"city"`
		CoverURL    *string  `json:"cover_photo_url"`
		Lat         *float64 `json:"lat"`
		Lng         *float64 `json:"lng"`
	} `json:"property"`
	CheckInAt     time.Time  `json:"check_in_at"`
	CheckOutAt    time.Time  `json:"check_out_at"`
	GuestsTotal   int        `json:"guests_total"`
	RoomsCount    int        `json:"rooms_count"`
	TotalAmount   int64      `json:"total_amount"`
	PrimaryAction string     `json:"primary_action"`
	Deadline      *time.Time `json:"payment_deadline_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

func primaryAction(status string) string {
	switch status {
	case StatusUnpaid:
		return "pay"
	case StatusPaid, StatusCheckIn:
		return "direct"
	default:
		return "rebook"
	}
}

// ---------- create ----------

type CreateAddonInput struct {
	AddonID uuid.UUID `json:"addon_id"`
	Qty     int       `json:"qty"`
}

type CreateRoomInput struct {
	TypeID   uuid.UUID          `json:"type_id"`
	Kind     string             `json:"kind"` // room_type | unit_type (opsional; ditentukan dari kategori listing)
	Adults   int                `json:"adults"`
	Children int                `json:"children"`
	Addons   []CreateAddonInput `json:"addons"`
}

type CreatePaymentInput struct {
	MethodCode string `json:"method_code"` // cash_on_site → pay_at_property; transfer_* → pilih di langkah pembayaran
}

type CreateBookingInput struct {
	PropertyID uuid.UUID           `json:"property_id"`
	CheckIn    string              `json:"check_in"`
	CheckOut   string              `json:"check_out"`
	Guest      GuestInput          `json:"guest"`
	Rooms      []CreateRoomInput   `json:"rooms"`
	Payment    *CreatePaymentInput `json:"payment"`
}

// CreateBooking (§4.2): availability per tipe dalam satu transaksi → N hotel_reservations(new) → UNPAID + deadline; atau
// pay_at_property → confirmed. Serialisasi per tipe via SELECT … FOR UPDATE + exclusion constraint DB.
func (s *Service) CreateBooking(ctx context.Context, in CreateBookingInput) (*Booking, error) {
	p, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	if in.PropertyID == uuid.Nil {
		return nil, apperr.Validation("property_id wajib")
	}
	ci, err := parseDate(in.CheckIn)
	if err != nil {
		return nil, apperr.Validation("check_in wajib (YYYY-MM-DD)").WithField("check_in", "wajib")
	}
	co, err := parseDate(in.CheckOut)
	if err != nil {
		return nil, apperr.Validation("check_out wajib (YYYY-MM-DD)").WithField("check_out", "wajib")
	}
	if !co.After(ci) {
		return nil, apperr.New(422, "INVALID_DATES", "Invalid dates", "check_out harus setelah check_in")
	}
	nights := int(co.Sub(ci).Hours() / 24)
	if nights > 30 {
		return nil, apperr.New(422, "INVALID_DATES", "Invalid dates", "Maksimal 30 malam per booking")
	}
	if len(in.Rooms) == 0 || len(in.Rooms) > 5 {
		return nil, apperr.Validation("rooms harus 1..5 kamar/unit")
	}
	in.Guest.FullName = strings.TrimSpace(in.Guest.FullName)
	in.Guest.Email = strings.ToLower(strings.TrimSpace(in.Guest.Email))
	if in.Guest.FullName == "" {
		return nil, apperr.Validation("Nama tamu wajib").WithField("guest.full_name", "wajib")
	}
	if in.Guest.Email != "" && !emailRe.MatchString(in.Guest.Email) {
		return nil, apperr.Validation("email tamu tidak valid").WithField("guest.email", "tidak valid")
	}
	if in.Guest.Phone != "" {
		ph, err := NormalizePhone(in.Guest.Phone)
		if err != nil {
			return nil, err
		}
		in.Guest.Phone = ph
	}
	payAtProperty := in.Payment != nil && in.Payment.MethodCode == "cash_on_site"
	now := s.now()
	var out *Booking
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		l, err := s.listingByIDTx(ctx, tx, in.PropertyID)
		if err != nil || !l.Listed {
			return apperr.New(423, "PROPERTY_UNLISTED", "Property unlisted", "Properti tidak menerima booking saat ini")
		}
		loc := l.location()
		today := time.Now().In(loc)
		today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
		if ci.Before(today) {
			return apperr.New(422, "INVALID_DATES", "Invalid dates", "check_in tidak boleh di masa lalu")
		}
		if payAtProperty && !l.AllowPayAtProp {
			return apperr.New(422, "METHOD_DISABLED", "Method disabled", "Bayar di tempat tidak tersedia untuk properti ini")
		}
		types, err := s.typesTx(ctx, tx, l, nil)
		if err != nil {
			return err
		}
		byID := map[uuid.UUID]*typeRow{}
		for i := range types {
			byID[types[i].ID] = &types[i]
		}
		need := map[uuid.UUID]int{}
		for i, r := range in.Rooms {
			t, ok := byID[r.TypeID]
			if !ok {
				return apperr.Validation(fmt.Sprintf("rooms[%d].type_id tidak valid untuk properti ini", i))
			}
			if r.Adults < 1 {
				return apperr.Validation(fmt.Sprintf("rooms[%d].adults minimal 1", i))
			}
			if r.Adults > t.CapAdults || r.Children > t.CapChildren || r.Children < 0 {
				return apperr.New(422, "CAPACITY_EXCEEDED", "Capacity exceeded", fmt.Sprintf("Kapasitas %s: %d dewasa, %d anak", t.Name, t.CapAdults, t.CapChildren))
			}
			need[r.TypeID]++
		}
		// kunci tipe (urut id agar bebas deadlock) lalu cek availability
		typeIDs := make([]uuid.UUID, 0, len(need))
		for id := range need {
			typeIDs = append(typeIDs, id)
		}
		sortUUIDs(typeIDs)
		for _, id := range typeIDs {
			t := byID[id]
			table := "hotel_room_types"
			if t.Kind == "unit_type" {
				table = "bvrooms_unit_types"
			}
			if _, err := tx.Exec(ctx, `SELECT 1 FROM `+table+` WHERE id = $1 FOR UPDATE`, id); err != nil {
				return err
			}
			_, avail, err := s.availabilityTx(ctx, tx, t, ci, co)
			if err != nil {
				return err
			}
			if avail < need[id] {
				e := apperr.Conflict("ROOM_UNAVAILABLE", fmt.Sprintf("%s tersisa %d untuk tanggal tersebut", t.Name, avail))
				return e.WithField("type_id", id.String()).WithField("available_count", fmt.Sprint(avail))
			}
		}
		code, err := newBookingCode()
		if err != nil {
			return err
		}
		bookingID := uuid.Must(uuid.NewV7())
		paymentStatus := "unpaid"
		var deadline *time.Time
		if payAtProperty {
			paymentStatus = "pay_at_property"
		} else {
			d := now.Add(time.Duration(l.PaymentWindowH) * time.Hour)
			deadline = &d
		}
		// baris kamar + reservasi
		var roomSub, addonSub, discount int64
		var adultsT, childrenT int
		var promoID *uuid.UUID
		type roomCalc struct {
			t        *typeRow
			in       CreateRoomInput
			rateID   *uuid.UUID
			first    int64
			total    int64
			promo    *PromoInfo
			addons   []BookingRoomAddon
			addonSum int64
		}
		var calcs []roomCalc
		for _, r := range in.Rooms {
			t := byID[r.TypeID]
			rateID, first, total, err := s.resolveRateTx(ctx, tx, t, ci, co)
			if err != nil {
				return err
			}
			promo, err := s.bestPromoTx(ctx, tx, l.PropertyID, t, ci, co, total)
			if err != nil {
				return err
			}
			offers, err := s.addonsForTypeTx(ctx, tx, l.PropertyID, t)
			if err != nil {
				return err
			}
			c := roomCalc{t: t, in: r, rateID: rateID, first: first, total: total, promo: promo}
			for _, a := range r.Addons {
				var off *AddonOffer
				for i := range offers {
					if offers[i].ID == a.AddonID {
						off = &offers[i]
					}
				}
				if off == nil {
					return apperr.Validation("addon_id tidak tersedia untuk tipe ini")
				}
				if a.Qty < 1 || a.Qty > off.MaxQty {
					return apperr.New(422, "ADDON_MAX_EXCEEDED", "Addon max exceeded", fmt.Sprintf("%s maksimal %d", off.Name, off.MaxQty))
				}
				var line int64
				switch off.PricingUnit {
				case "per_guest_per_night", "per_item_per_night":
					line = off.Price * int64(a.Qty) * int64(nights)
				default:
					line = off.Price * int64(a.Qty)
				}
				c.addons = append(c.addons, BookingRoomAddon{AddonID: &off.ID, Name: off.Name, Kind: off.Kind, PricingUnit: off.PricingUnit, Qty: a.Qty, UnitPrice: off.Price, LineTotal: line})
				c.addonSum += line
			}
			calcs = append(calcs, c)
			roomSub += total
			addonSub += c.addonSum
			if promo != nil {
				discount += promo.DiscountAmount
				if promoID == nil {
					id := promo.ID
					promoID = &id
				}
			}
			adultsT += r.Adults
			childrenT += r.Children
		}
		total := roomSub + addonSub - discount
		if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_bookings (id, organization_id, property_id, booking_code, customer_id, guest_full_name, guest_email, guest_phone, check_in_date, check_out_date, rooms_count, adults_total, children_total,
			room_subtotal, addon_subtotal, discount_amount, promotion_id, total_amount, currency_code, payment_status, payment_deadline_at)
			VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
			bookingID, p.OrganizationID, l.PropertyID, code, p.UserID, in.Guest.FullName, in.Guest.Email, in.Guest.Phone, ci, co, len(in.Rooms), adultsT, childrenT,
			roomSub, addonSub, discount, promoID, total, calcs[0].t.Currency, paymentStatus, deadline); err != nil {
			return err
		}
		resStatus := "new"
		var confirmedAt *time.Time
		if payAtProperty {
			resStatus = "confirmed"
			confirmedAt = &now
		}
		for i, c := range calcs {
			number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixHotelReservation, now, loc)
			if err != nil {
				return err
			}
			var roomTypeID, unitTypeID *uuid.UUID
			if c.t.Kind == "unit_type" {
				unitTypeID = &c.t.ID
			} else {
				roomTypeID = &c.t.ID
			}
			special := addonSummary(c.addons)
			var resID uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO hotel_reservations (organization_id, property_id, reservation_number, room_type_id, unit_type_id, guest_name, guest_phone, guest_email, adults, children, check_in_date, check_out_date, rate_id, rate_per_night, total_amount, currency_code, status, source, special_requests, notes, confirmed_at, bvrooms_booking_id)
				VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9,$10,$11,$12,$13,$14,$15,$16,$17,'bvrooms',NULLIF($18,''),$19,$20,$21) RETURNING id`,
				p.OrganizationID, l.PropertyID, number, roomTypeID, unitTypeID, in.Guest.FullName, in.Guest.Phone, in.Guest.Email, c.in.Adults, c.in.Children, ci, co, c.rateID, c.first, c.total, c.t.Currency, resStatus, special, "BVRooms "+code, confirmedAt, bookingID).Scan(&resID); err != nil {
				if db.IsExclusionViolation(err) {
					return apperr.Conflict("ROOM_UNAVAILABLE", "Kamar sudah dipesan pada tanggal tersebut")
				}
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO hotel_reservation_guests (organization_id, reservation_id, full_name, phone, email, is_primary) VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),true)`,
				p.OrganizationID, resID, in.Guest.FullName, in.Guest.Phone, in.Guest.Email); err != nil {
				return err
			}
			var disc int64
			if c.promo != nil {
				disc = c.promo.DiscountAmount
			}
			var brID uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO bvrooms_booking_rooms (organization_id, booking_id, reservation_id, room_type_id, unit_type_id, type_name, adults, children, rate_per_night, nights, discount_amount, line_total, sort_order)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`,
				p.OrganizationID, bookingID, resID, roomTypeID, unitTypeID, c.t.Name, c.in.Adults, c.in.Children, c.first, nights, disc, c.total-disc+c.addonSum, i).Scan(&brID); err != nil {
				return err
			}
			for _, a := range c.addons {
				if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_booking_addons (organization_id, booking_room_id, addon_id, name, kind, pricing_unit, qty, unit_price, line_total) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
					p.OrganizationID, brID, a.AddonID, a.Name, a.Kind, a.PricingUnit, a.Qty, a.UnitPrice, a.LineTotal); err != nil {
					return err
				}
			}
		}
		if payAtProperty {
			if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_payments (organization_id, booking_id, provider_code, method_code, amount, status, instructions, note) VALUES ($1,$2,'manual','cash_on_site',$3,'pending',$4,'Bayar di properti saat check-in')`,
				p.OrganizationID, bookingID, total, `{"note":"Pembayaran dilakukan di properti saat check-in"}`); err != nil {
				return err
			}
		}
		title, body := "Pemesanan Berhasil.", "Pemesanan tempat kamu berhasil, yuk langsung dibayar agar tidak hangus!"
		if payAtProperty {
			body = "Pemesanan tempat kamu berhasil. Pembayaran dilakukan di properti saat check-in."
		}
		if err := s.notifyTx(ctx, tx, p.OrganizationID, p.UserID, "booking_created", title, body, &bookingID, "success"); err != nil {
			return err
		}
		_ = audit.LogAs(ctx, tx, p.OrganizationID, nil, p.IP, p.UserAgent, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "bvrooms_booking", EntityID: &bookingID, EntityLabel: code, After: map[string]any{"customer_id": p.UserID, "rooms": len(in.Rooms), "total": total, "payment_status": paymentStatus}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventBookingCreated, OrganizationID: p.OrganizationID, PropertyID: &l.PropertyID, ObjectType: "bvrooms_booking", ObjectID: bookingID, ObjectLabel: code + " · " + in.Guest.FullName, Payload: map[string]any{"domain": "tenant_relation", "total": total, "check_in": in.CheckIn}})
		}
		out, err = s.bookingTx(ctx, tx, bookingID, false)
		return err
	})
	return out, err
}

func addonSummary(addons []BookingRoomAddon) string {
	var parts []string
	for _, a := range addons {
		parts = append(parts, fmt.Sprintf("%s x%d", a.Name, a.Qty))
	}
	return strings.Join(parts, ", ")
}

func sortUUIDs(xs []uuid.UUID) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j].String() < xs[j-1].String(); j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// ---------- read ----------

func (s *Service) bookingTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, withCustomer bool) (*Booking, error) {
	b, err := scanBooking(tx.QueryRow(ctx, bookingSelect+` WHERE b.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Booking")
		}
		return nil, err
	}
	return s.assembleBookingTx(ctx, tx, b, withCustomer)
}

func (s *Service) bookingByCodeTx(ctx context.Context, tx pgx.Tx, code string) (*bookingRow, error) {
	b, err := scanBooking(tx.QueryRow(ctx, bookingSelect+` WHERE b.booking_code = $1`, strings.ToUpper(strings.TrimSpace(code))))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Booking")
		}
		return nil, err
	}
	return b, nil
}

func (s *Service) assembleBookingTx(ctx context.Context, tx pgx.Tx, b *bookingRow, withCustomer bool) (*Booking, error) {
	l, err := s.listingByIDTx(ctx, tx, b.PropertyID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	loc := l.location()
	st := b.deriveStatus(now)
	out := &Booking{ID: b.ID, BookingCode: b.Code, Status: st, PaymentStatus: b.PaymentStatus, PaymentDeadline: b.Deadline,
		Guest:       GuestInput{FullName: b.GuestName, Email: deref(b.GuestEmail), Phone: deref(b.GuestPhone)},
		CheckInDate: b.CheckIn.Format("2006-01-02"), CheckOutDate: b.CheckOut.Format("2006-01-02"), Nights: b.Nights, RoomsCount: b.RoomsCount, GuestsTotal: b.Adults + b.Children,
		Totals: BookingTotals{Room: b.RoomSubtotal, Addon: b.AddonSubtotal, Discount: b.Discount, Total: b.Total}, Currency: b.Currency,
		CancelledAt: b.CancelledAt, CancelReason: b.CancelReason, CancelledBy: b.CancelledBy, CreatedAt: b.CreatedAt, Version: b.Version}
	out.CheckInAt = b.checkInAt(loc, l.CheckInTime)
	co := time.Date(b.CheckOut.Year(), b.CheckOut.Month(), b.CheckOut.Day(), 0, 0, 0, 0, loc)
	h, m := 12, 0
	_, _ = fmt.Sscanf(l.CheckOutTime, "%d:%d", &h, &m)
	out.CheckOutAt = co.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	out.PrimaryAction = primaryAction(st)
	out.CanCancel = (st == StatusUnpaid || st == StatusPaid) && now.Before(out.CheckInAt)
	out.CanModifyGuest = st == StatusUnpaid || st == StatusPaid
	out.CanReview = st == StatusCheckOut && b.ReviewedAt == nil
	var coverKey *string
	_ = tx.QueryRow(ctx, `SELECT storage_key FROM bvrooms_property_photos WHERE property_id = $1 AND status = 'ready' ORDER BY is_cover DESC, sort_order, created_at LIMIT 1`, l.PropertyID).Scan(&coverKey)
	out.Property = BookingProperty{ID: l.PropertyID, Slug: l.Slug, DisplayName: l.DisplayName, Category: l.Category, City: l.City, AddressLine: l.AddressLine, CoverURL: s.photoURL(ctx, coverKey), Lat: l.Lat, Lng: l.Lng, Phone: l.Phone, WhatsApp: l.WhatsApp, Policies: l.Policies, CancelMD: l.CancellationMD, Timezone: l.Timezone}
	if out.Property.Policies == nil {
		out.Property.Policies = []string{}
	}
	rows, err := tx.Query(ctx, `SELECT br.id, br.reservation_id, r.reservation_number, COALESCE(br.room_type_id, br.unit_type_id), CASE WHEN br.unit_type_id IS NULL THEN 'room_type' ELSE 'unit_type' END, br.type_name, br.adults, br.children, br.rate_per_night, br.nights, br.discount_amount, br.line_total, r.status,
		COALESCE(hr.room_number, u.unit_number)
		FROM bvrooms_booking_rooms br JOIN hotel_reservations r ON r.id = br.reservation_id LEFT JOIN hotel_rooms hr ON hr.location_id = r.room_location_id LEFT JOIN units u ON u.location_id = r.unit_location_id
		WHERE br.booking_id = $1 ORDER BY br.sort_order`, b.ID)
	if err != nil {
		return nil, err
	}
	out.Rooms = []BookingRoom{}
	for rows.Next() {
		var r BookingRoom
		if err := rows.Scan(&r.ID, &r.ReservationID, &r.ReservationNo, &r.TypeID, &r.Kind, &r.TypeName, &r.Adults, &r.Children, &r.RatePerNight, &r.Nights, &r.DiscountAmount, &r.LineTotal, &r.ReservationStat, &r.AssignedNumber); err != nil {
			rows.Close()
			return nil, err
		}
		r.Addons = []BookingRoomAddon{}
		out.Rooms = append(out.Rooms, r)
	}
	rows.Close()
	for i := range out.Rooms {
		ar, err := tx.Query(ctx, `SELECT id, addon_id, name, kind, pricing_unit, qty, unit_price, line_total FROM bvrooms_booking_addons WHERE booking_room_id = $1 ORDER BY name`, out.Rooms[i].ID)
		if err != nil {
			return nil, err
		}
		for ar.Next() {
			var a BookingRoomAddon
			if err := ar.Scan(&a.ID, &a.AddonID, &a.Name, &a.Kind, &a.PricingUnit, &a.Qty, &a.UnitPrice, &a.LineTotal); err != nil {
				ar.Close()
				return nil, err
			}
			out.Rooms[i].Addons = append(out.Rooms[i].Addons, a)
		}
		ar.Close()
	}
	var rv Review
	if err := tx.QueryRow(ctx, `SELECT stars, display_name, created_at FROM bvrooms_reviews WHERE booking_id = $1`, b.ID).Scan(&rv.Stars, &rv.DisplayName, &rv.CreatedAt); err == nil {
		out.Review = &rv
	}
	out.Payment, _ = s.latestPaymentTx(ctx, tx, b.ID, withCustomer)
	if withCustomer {
		out.Customer, _ = s.customerByIDTx(ctx, tx, b.CustomerID)
	}
	return out, nil
}

func (s *Service) GetBooking(ctx context.Context, code string) (*Booking, error) {
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
		out, err = s.assembleBookingTx(ctx, tx, b, false)
		return err
	})
	return out, err
}

// ListBookings: scope upcoming (UNPAID/PAID/CHECK IN) | history (CHECK OUT/CANCELLED/EXPIRED).
func (s *Service) ListBookings(ctx context.Context, scope string, page httpx.Page) ([]BookingCard, *string, error) {
	p, err := customer(ctx)
	if err != nil {
		return nil, nil, err
	}
	if scope == "" {
		scope = "upcoming"
	}
	if scope != "upcoming" && scope != "history" {
		return nil, nil, apperr.Validation("scope harus upcoming|history")
	}
	now := s.now()
	out := []BookingCard{}
	var next *string
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, bookingSelect+` WHERE b.customer_id = $1 ORDER BY b.created_at DESC, b.id DESC`, p.UserID)
		if err != nil {
			return err
		}
		var all []*bookingRow
		for rows.Next() {
			b, err := scanBooking(rows)
			if err != nil {
				rows.Close()
				return err
			}
			all = append(all, b)
		}
		rows.Close()
		type propInfo struct {
			l   *listingRow
			key *string
		}
		props := map[uuid.UUID]propInfo{}
		skip := page.Cursor != nil
		for _, b := range all {
			st := b.deriveStatus(now)
			upcoming := st == StatusUnpaid || st == StatusPaid || st == StatusCheckIn
			if (scope == "upcoming") != upcoming {
				continue
			}
			if skip {
				if b.ID == page.Cursor.ID {
					skip = false
				}
				continue
			}
			if len(out) >= page.Limit {
				c := httpx.EncodeCursor(out[len(out)-1].CreatedAt.Format(time.RFC3339Nano), out[len(out)-1].ID)
				next = &c
				break
			}
			pi, ok := props[b.PropertyID]
			if !ok {
				l, err := s.listingByIDTx(ctx, tx, b.PropertyID)
				if err != nil {
					return err
				}
				var key *string
				_ = tx.QueryRow(ctx, `SELECT storage_key FROM bvrooms_property_photos WHERE property_id = $1 AND status = 'ready' ORDER BY is_cover DESC, sort_order, created_at LIMIT 1`, l.PropertyID).Scan(&key)
				pi = propInfo{l: l, key: key}
				props[b.PropertyID] = pi
			}
			loc := pi.l.location()
			card := BookingCard{ID: b.ID, BookingCode: b.Code, Status: st, PaymentStatus: b.PaymentStatus, GuestsTotal: b.Adults + b.Children, RoomsCount: b.RoomsCount, TotalAmount: b.Total, PrimaryAction: primaryAction(st), Deadline: b.Deadline, CreatedAt: b.CreatedAt}
			card.Property.Slug, card.Property.DisplayName, card.Property.City, card.Property.Lat, card.Property.Lng = pi.l.Slug, pi.l.DisplayName, pi.l.City, pi.l.Lat, pi.l.Lng
			card.Property.CoverURL = s.photoURL(ctx, pi.key)
			card.CheckInAt = b.checkInAt(loc, pi.l.CheckInTime)
			h, m := 12, 0
			_, _ = fmt.Sscanf(pi.l.CheckOutTime, "%d:%d", &h, &m)
			card.CheckOutAt = time.Date(b.CheckOut.Year(), b.CheckOut.Month(), b.CheckOut.Day(), h, m, 0, 0, loc)
			out = append(out, card)
		}
		return nil
	})
	return out, next, err
}

// ---------- modify guest / cancel ----------

func (s *Service) ModifyGuest(ctx context.Context, code string, in GuestInput) (*Booking, error) {
	p, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	in.FullName = strings.TrimSpace(in.FullName)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.FullName == "" {
		return nil, apperr.Validation("Nama lengkap tidak boleh kosong!").WithField("full_name", "wajib")
	}
	if in.Email == "" || !emailRe.MatchString(in.Email) {
		return nil, apperr.Validation("Email tidak valid").WithField("email", "tidak valid")
	}
	ph, err := NormalizePhone(in.Phone)
	if err != nil {
		return nil, err
	}
	in.Phone = ph
	var out *Booking
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := s.bookingByCodeTx(ctx, tx, code)
		if err != nil {
			return err
		}
		if b.CustomerID != p.UserID {
			return apperr.NotFound("Booking")
		}
		st := b.deriveStatus(s.now())
		if st != StatusUnpaid && st != StatusPaid {
			return apperr.Conflict("GUEST_LOCKED", "Data tamu tidak dapat diubah pada status "+st)
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_bookings SET guest_full_name = $2, guest_email = $3, guest_phone = $4 WHERE id = $1`, b.ID, in.FullName, in.Email, in.Phone); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET guest_name = $2, guest_email = $3, guest_phone = $4 WHERE bvrooms_booking_id = $1`, b.ID, in.FullName, in.Email, in.Phone); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE hotel_reservation_guests g SET full_name = $2, email = $3, phone = $4 FROM hotel_reservations r WHERE r.id = g.reservation_id AND r.bvrooms_booking_id = $1 AND g.is_primary`, b.ID, in.FullName, in.Email, in.Phone); err != nil {
			return err
		}
		_ = audit.LogAs(ctx, tx, p.OrganizationID, nil, p.IP, p.UserAgent, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "bvrooms_booking", EntityID: &b.ID, EntityLabel: b.Code, Before: map[string]any{"guest": b.GuestName}, After: map[string]any{"guest": in.FullName}})
		out, err = s.bookingTx(ctx, tx, b.ID, false)
		return err
	})
	return out, err
}

type CancelInput struct {
	Reason string `json:"reason"`
}

// CancelBooking (customer): UNPAID/PAID sebelum jam check-in. paid → refund_pending (refund manual Finance).
func (s *Service) CancelBooking(ctx context.Context, code string, in CancelInput) (*Booking, error) {
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
		l, err := s.listingByIDTx(ctx, tx, b.PropertyID)
		if err != nil {
			return err
		}
		now := s.now()
		st := b.deriveStatus(now)
		if (st != StatusUnpaid && st != StatusPaid) || !now.Before(b.checkInAt(l.location(), l.CheckInTime)) {
			return apperr.Conflict("CANNOT_CANCEL", "Booking tidak dapat dibatalkan pada status "+st)
		}
		if err := s.cancelBookingTx(ctx, tx, b, "customer", strings.TrimSpace(in.Reason), "Dibatalkan oleh customer"); err != nil {
			return err
		}
		_ = audit.LogAs(ctx, tx, p.OrganizationID, nil, p.IP, p.UserAgent, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "bvrooms_booking", EntityID: &b.ID, EntityLabel: b.Code, Before: map[string]any{"status": st}, After: map[string]any{"status": StatusCancelled, "reason": in.Reason}})
		out, err = s.bookingTx(ctx, tx, b.ID, false)
		return err
	})
	return out, err
}

// cancelBookingTx: reservasi → cancelled, payment pending → cancelled, payment_status paid → refund_pending, notif.
func (s *Service) cancelBookingTx(ctx context.Context, tx pgx.Tx, b *bookingRow, by, reason, defaultReason string) error {
	if reason == "" {
		reason = defaultReason
	}
	if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'cancelled', cancelled_at = now(), cancel_reason = $2 WHERE bvrooms_booking_id = $1 AND status IN ('new','confirmed')`, b.ID, reason); err != nil {
		return err
	}
	paySt := b.PaymentStatus
	if paySt == "paid" {
		paySt = "refund_pending"
	} else if paySt == "pay_at_property" {
		paySt = "unpaid"
	}
	if _, err := tx.Exec(ctx, `UPDATE bvrooms_bookings SET status_override = 'cancelled', cancelled_at = now(), cancel_reason = $2, cancelled_by = $3, payment_status = $4, payment_deadline_at = NULL WHERE id = $1`, b.ID, reason, by, paySt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE bvrooms_payments SET status = 'cancelled' WHERE booking_id = $1 AND status IN ('pending','proof_submitted')`, b.ID); err != nil {
		return err
	}
	body := "Bookingan Kamu sudah berhasil dibatalkan."
	if paySt == "refund_pending" {
		body = "Bookingan Kamu sudah dibatalkan. Pengembalian dana diproses sesuai kebijakan pembatalan."
	}
	if err := s.notifyTx(ctx, tx, b.orgID(ctx), b.CustomerID, "booking_cancelled", "Pemesanan Dibatalkan!", body, &b.ID, "danger"); err != nil {
		return err
	}
	if s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventBookingCancelled, OrganizationID: b.orgID(ctx), PropertyID: &b.PropertyID, ObjectType: "bvrooms_booking", ObjectID: b.ID, ObjectLabel: b.Code + " · " + b.GuestName, Payload: map[string]any{"domain": "tenant_relation", "by": by, "reason": reason, "payment_status": paySt}})
	}
	return nil
}

func (b *bookingRow) orgID(ctx context.Context) uuid.UUID { return mustOrg(ctx) }

var _ = authctx.From
