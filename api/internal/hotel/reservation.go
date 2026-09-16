package hotel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/housekeeping"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantrelation"
)

type Guest struct {
	ID             uuid.UUID `json:"id,omitempty"`
	FullName       string    `json:"full_name"`
	IDType         *string   `json:"id_type"`
	IDNumber       *string   `json:"id_number,omitempty"` // input saja; disimpan termasking
	IDNumberMasked *string   `json:"id_number_masked"`
	Phone          *string   `json:"phone"`
	Email          *string   `json:"email"`
	Nationality    *string   `json:"nationality"`
	IsPrimary      bool      `json:"is_primary"`
}

type Reservation struct {
	ID                 uuid.UUID  `json:"id"`
	ReservationNumber  string     `json:"reservation_number"`
	PropertyID         uuid.UUID  `json:"property_id"`
	RoomTypeID         uuid.UUID  `json:"room_type_id"`
	RoomTypeName       string     `json:"room_type_name"`
	RoomLocationID     *uuid.UUID `json:"room_location_id"`
	RoomNumber         *string    `json:"room_number"`
	RoomStatus         *string    `json:"room_status"`
	GuestName          string     `json:"guest_name"`
	GuestPhone         *string    `json:"guest_phone"`
	GuestEmail         *string    `json:"guest_email"`
	Adults             int        `json:"adults"`
	Children           int        `json:"children"`
	CheckInDate        time.Time  `json:"check_in_date"`
	CheckOutDate       time.Time  `json:"check_out_date"`
	Nights             int        `json:"nights"`
	RateID             *uuid.UUID `json:"rate_id"`
	RatePerNight       int64      `json:"rate_per_night"`
	TotalAmount        int64      `json:"total_amount"`
	CurrencyCode       string     `json:"currency_code"`
	Status             string     `json:"status"`
	StayStatus         string     `json:"stay_status"` // upcoming | arriving_today | in_house | departing_today | departed | cancelled
	Source             string     `json:"source"`
	SpecialRequests    *string    `json:"special_requests"`
	Notes              *string    `json:"notes"`
	InvoiceID          *uuid.UUID `json:"invoice_id"`
	InvoiceNumber      *string    `json:"invoice_number"`
	TenantUserID       *uuid.UUID `json:"tenant_user_id"`
	BVRoomsBookingID   *uuid.UUID `json:"bvrooms_booking_id"` // reservasi dari BVRooms (source=bvrooms)
	BVRoomsBookingCode *string    `json:"bvrooms_booking_code"`
	BVRoomsPayment     *string    `json:"bvrooms_payment_status"`
	ConfirmedAt        *time.Time `json:"confirmed_at"`
	CheckedInAt        *time.Time `json:"checked_in_at"`
	CheckedOutAt       *time.Time `json:"checked_out_at"`
	CancelledAt        *time.Time `json:"cancelled_at"`
	CancelReason       *string    `json:"cancel_reason"`
	Guests             []Guest    `json:"guests"`
	OpenRequests       int        `json:"open_requests"` // Guest Request (SR) pada kamar selama menginap
	CreatedAt          time.Time  `json:"created_at"`
	CreatedByName      *string    `json:"created_by_name"`
	AllowedActions     []string   `json:"allowed_actions"`
	Version            int        `json:"version"`
}

const resSelect = `SELECT r.id, r.reservation_number, r.property_id, r.room_type_id, rt.name, r.room_location_id, hr.room_number, hr.room_status, r.guest_name, r.guest_phone, r.guest_email, r.adults, r.children,
	r.check_in_date, r.check_out_date, r.nights, r.rate_id, r.rate_per_night, r.total_amount, r.currency_code, r.status, r.source, r.special_requests, r.notes, r.invoice_id, i.invoice_number, r.tenant_user_id,
	r.bvrooms_booking_id, bb.booking_code, bb.payment_status,
	r.confirmed_at, r.checked_in_at, r.checked_out_at, r.cancelled_at, r.cancel_reason, r.created_at, cu.full_name, r.version,
	(SELECT count(*) FROM service_requests sr WHERE sr.location_id = r.room_location_id AND sr.status NOT IN ('closed','cancelled') AND sr.created_at >= COALESCE(r.checked_in_at, r.created_at))
	FROM hotel_reservations r JOIN hotel_room_types rt ON rt.id = r.room_type_id LEFT JOIN hotel_rooms hr ON hr.location_id = r.room_location_id LEFT JOIN invoices i ON i.id = r.invoice_id LEFT JOIN users cu ON cu.id = r.created_by LEFT JOIN bvrooms_bookings bb ON bb.id = r.bvrooms_booking_id`

func scanRes(row pgx.Row) (*Reservation, error) {
	var r Reservation
	if err := row.Scan(&r.ID, &r.ReservationNumber, &r.PropertyID, &r.RoomTypeID, &r.RoomTypeName, &r.RoomLocationID, &r.RoomNumber, &r.RoomStatus, &r.GuestName, &r.GuestPhone, &r.GuestEmail, &r.Adults, &r.Children,
		&r.CheckInDate, &r.CheckOutDate, &r.Nights, &r.RateID, &r.RatePerNight, &r.TotalAmount, &r.CurrencyCode, &r.Status, &r.Source, &r.SpecialRequests, &r.Notes, &r.InvoiceID, &r.InvoiceNumber, &r.TenantUserID,
		&r.BVRoomsBookingID, &r.BVRoomsBookingCode, &r.BVRoomsPayment,
		&r.ConfirmedAt, &r.CheckedInAt, &r.CheckedOutAt, &r.CancelledAt, &r.CancelReason, &r.CreatedAt, &r.CreatedByName, &r.Version, &r.OpenRequests); err != nil {
		return nil, err
	}
	r.CurrencyCode = strings.TrimSpace(r.CurrencyCode)
	r.Guests = []Guest{}
	return &r, nil
}

func (s *Service) enrichRes(ctx context.Context, tx pgx.Tx, r *Reservation) error {
	rows, err := tx.Query(ctx, `SELECT id, full_name, id_type, id_number_masked, phone, email, nationality, is_primary FROM hotel_reservation_guests WHERE reservation_id = $1 ORDER BY is_primary DESC, full_name`, r.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var g Guest
		if err := rows.Scan(&g.ID, &g.FullName, &g.IDType, &g.IDNumberMasked, &g.Phone, &g.Email, &g.Nationality, &g.IsPrimary); err != nil {
			return err
		}
		r.Guests = append(r.Guests, g)
	}
	loc := property.PropertyTimezone(ctx, tx, r.PropertyID)
	today := time.Now().In(loc).Format("2006-01-02")
	ci, co := r.CheckInDate.Format("2006-01-02"), r.CheckOutDate.Format("2006-01-02")
	switch r.Status {
	case "cancelled", "no_show":
		r.StayStatus = "cancelled"
	case "checked_out":
		r.StayStatus = "departed"
	case "checked_in":
		if co <= today {
			r.StayStatus = "departing_today"
		} else {
			r.StayStatus = "in_house"
		}
	default:
		if ci == today {
			r.StayStatus = "arriving_today"
		} else if ci < today {
			r.StayStatus = "arrival_overdue"
		} else {
			r.StayStatus = "upcoming"
		}
	}
	p := authctx.Must(ctx)
	can := func(perm string) bool { return p.HasOnProperty(perm, r.PropertyID) }
	r.AllowedActions = []string{"view"}
	switch r.Status {
	case "new":
		if can("hotel.reservations.confirm") {
			r.AllowedActions = append(r.AllowedActions, "confirm")
		}
		fallthrough
	case "confirmed":
		if can("hotel.reservations.assign_room") {
			r.AllowedActions = append(r.AllowedActions, "assign_room")
		}
		if can("hotel.reservations.check_in") && ci <= today {
			r.AllowedActions = append(r.AllowedActions, "check_in")
		}
		if can("hotel.reservations.cancel") {
			r.AllowedActions = append(r.AllowedActions, "cancel")
		}
		if can("hotel.reservations.no_show") && ci < today {
			r.AllowedActions = append(r.AllowedActions, "no_show")
		}
		if can("hotel.reservations.update") {
			r.AllowedActions = append(r.AllowedActions, "update")
		}
	case "checked_in":
		if can("hotel.reservations.check_out") {
			r.AllowedActions = append(r.AllowedActions, "check_out")
		}
		if can("hotel.reservations.assign_room") {
			r.AllowedActions = append(r.AllowedActions, "assign_room") // pindah kamar
		}
	}
	return rows.Err()
}

func (s *Service) getResTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Reservation, error) {
	r, err := scanRes(tx.QueryRow(ctx, resSelect+` WHERE r.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Reservation")
		}
		return nil, err
	}
	return r, s.enrichRes(ctx, tx, r)
}

type ReservationInput struct {
	PropertyID      *uuid.UUID `json:"property_id"`
	RoomTypeID      *uuid.UUID `json:"room_type_id"`
	RoomLocationID  *uuid.UUID `json:"room_location_id"`
	GuestName       *string    `json:"guest_name"`
	GuestPhone      *string    `json:"guest_phone"`
	GuestEmail      *string    `json:"guest_email"`
	Adults          *int       `json:"adults"`
	Children        *int       `json:"children"`
	CheckInDate     *string    `json:"check_in_date"`  // YYYY-MM-DD
	CheckOutDate    *string    `json:"check_out_date"` // YYYY-MM-DD
	Source          *string    `json:"source"`
	SpecialRequests *string    `json:"special_requests"`
	Notes           *string    `json:"notes"`
	Guests          *[]Guest   `json:"guests"`
	Confirm         bool       `json:"confirm"` // langsung Confirmed
}

var sources = map[string]bool{"walk_in": true, "phone": true, "email": true, "website": true, "corporate": true, "other": true}

func maskID(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	d := strings.TrimSpace(*v)
	if len(d) <= 4 {
		return &d
	}
	m := strings.Repeat("•", len(d)-4) + d[len(d)-4:]
	return &m
}

// CreateReservation (WF-P1-007): Select Dates → Room Type → Availability → Rate → Guest Details → Reservation.
func (s *Service) CreateReservation(ctx context.Context, in ReservationInput) (*Reservation, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil || in.RoomTypeID == nil {
		return nil, apperr.Validation("property_id dan room_type_id wajib")
	}
	if err := iam.CanOnProperty(ctx, "hotel.reservations.create", *in.PropertyID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(deref(in.GuestName))
	if name == "" {
		return nil, apperr.Validation("guest_name wajib").WithField("guest_name", "wajib")
	}
	ci, err := parseDate(in.CheckInDate)
	if err != nil || ci == nil {
		return nil, apperr.Validation("check_in_date wajib (YYYY-MM-DD)").WithField("check_in_date", "wajib")
	}
	co, err := parseDate(in.CheckOutDate)
	if err != nil || co == nil {
		return nil, apperr.Validation("check_out_date wajib (YYYY-MM-DD)").WithField("check_out_date", "wajib")
	}
	if !co.After(*ci) {
		return nil, apperr.Validation("check_out_date harus setelah check_in_date")
	}
	src := deref(in.Source)
	if src == "" {
		src = "walk_in"
	}
	if !sources[src] {
		return nil, apperr.Validation("source tidak valid (OTA/channel di luar scope)")
	}
	var out *Reservation
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireHotel(ctx, tx, *in.PropertyID); err != nil {
			return err
		}
		rt, err := s.getRTTx(ctx, tx, *in.RoomTypeID)
		if err != nil {
			return err
		}
		if rt.PropertyID != *in.PropertyID || rt.Status != "active" {
			return apperr.Validation("room_type_id tidak valid untuk property ini")
		}
		// serialisasi per tipe kamar agar dua reservasi paralel tidak melewati kapasitas
		if _, err := tx.Exec(ctx, `SELECT 1 FROM hotel_room_types WHERE id = $1 FOR UPDATE`, rt.ID); err != nil {
			return err
		}
		av, err := s.availabilityTx(ctx, tx, rt, *ci, *co, nil)
		if err != nil {
			return err
		}
		if av.AvailableRooms <= 0 {
			return apperr.Conflict("NO_AVAILABILITY", fmt.Sprintf("Tidak ada kamar %s tersedia untuk tanggal tersebut", rt.Name))
		}
		if in.RoomLocationID != nil {
			okRoom := false
			for _, id := range av.FreeRoomIDs {
				if id == *in.RoomLocationID {
					okRoom = true
				}
			}
			if !okRoom {
				return apperr.Conflict("ROOM_UNAVAILABLE", "Kamar yang dipilih tidak tersedia pada tanggal tersebut")
			}
		}
		adults := 1
		if in.Adults != nil {
			adults = *in.Adults
		}
		children := 0
		if in.Children != nil {
			children = *in.Children
		}
		if adults < 1 || adults > rt.CapacityAdults || children > rt.CapacityChildren {
			return apperr.Validation(fmt.Sprintf("Kapasitas %s: %d dewasa, %d anak", rt.Name, rt.CapacityAdults, rt.CapacityChildren))
		}
		loc := property.PropertyTimezone(ctx, tx, *in.PropertyID)
		number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixHotelReservation, time.Now(), loc)
		if err != nil {
			return err
		}
		status := "new"
		var confirmedAt *time.Time
		if in.Confirm {
			if err := iam.CanOnProperty(ctx, "hotel.reservations.confirm", *in.PropertyID); err != nil {
				return err
			}
			status = "confirmed"
			now := time.Now().UTC()
			confirmedAt = &now
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO hotel_reservations (organization_id, property_id, reservation_number, room_type_id, room_location_id, guest_name, guest_phone, guest_email, adults, children, check_in_date, check_out_date, rate_id, rate_per_night, total_amount, currency_code, status, source, special_requests, notes, confirmed_at, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$22) RETURNING id`,
			p.OrganizationID, *in.PropertyID, number, rt.ID, in.RoomLocationID, name, in.GuestPhone, in.GuestEmail, adults, children, *ci, *co, av.RateID(), av.RatePerNight, av.TotalAmount, rt.CurrencyCode, status, src, in.SpecialRequests, in.Notes, confirmedAt, p.UserID).Scan(&id); err != nil {
			if db.IsExclusionViolation(err) {
				return apperr.Conflict("ROOM_UNAVAILABLE", "Kamar sudah dipesan pada tanggal tersebut")
			}
			return err
		}
		guests := []Guest{{FullName: name, Phone: in.GuestPhone, Email: in.GuestEmail, IsPrimary: true}}
		if in.Guests != nil && len(*in.Guests) > 0 {
			guests = *in.Guests
		}
		for _, g := range guests {
			if strings.TrimSpace(g.FullName) == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO hotel_reservation_guests (organization_id, reservation_id, full_name, id_type, id_number_masked, phone, email, nationality, is_primary) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
				p.OrganizationID, id, strings.TrimSpace(g.FullName), g.IDType, maskID(g.IDNumber), g.Phone, g.Email, g.Nationality, g.IsPrimary); err != nil {
				return err
			}
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "hotel_reservation", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "room_type": rt.Name, "nights": av.Nights, "status": status}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "hotel_reservation", EntityID: &id, EntityLabel: number + " " + name})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventReservationCreated, OrganizationID: p.OrganizationID, PropertyID: in.PropertyID, ObjectType: "hotel_reservation", ObjectID: id, ObjectLabel: number + " · " + name, ActorUserID: &p.UserID, Payload: map[string]any{"status": status, "domain": "tenant_relation", "check_in": ci.Format("2006-01-02")}})
		}
		out, err = s.getResTx(ctx, tx, id)
		return err
	})
	return out, err
}

// RateID: rate malam pertama (dipakai INSERT).
func (a *Availability) RateID() *uuid.UUID { return a.rateID }

type Filter struct {
	PropertyID  *uuid.UUID
	Statuses    []string
	RoomTypeID  *uuid.UUID
	From, To    *time.Time // overlap stay
	ArrivalDate *time.Time
	Q           string
}

func (s *Service) List(ctx context.Context, f Filter, page httpx.Page) ([]Reservation, *string, error) {
	p := authctx.Must(ctx)
	var out []Reservation
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if f.PropertyID != nil {
			if err := iam.CanOnProperty(ctx, "hotel.reservations.view", *f.PropertyID); err != nil {
				return err
			}
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND r.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("hotel.reservations.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND r.property_id = ANY($%d)", len(args))
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND r.status = ANY($%d)", len(args))
		}
		if f.RoomTypeID != nil {
			args = append(args, *f.RoomTypeID)
			where += fmt.Sprintf(" AND r.room_type_id = $%d", len(args))
		}
		if f.From != nil && f.To != nil {
			args = append(args, *f.From, *f.To)
			where += fmt.Sprintf(" AND r.stay && daterange($%d::date, $%d::date, '[]')", len(args)-1, len(args))
		}
		if f.ArrivalDate != nil {
			args = append(args, *f.ArrivalDate)
			where += fmt.Sprintf(" AND r.check_in_date = $%d::date", len(args))
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (r.reservation_number ILIKE $%d OR r.guest_name ILIKE $%d OR r.guest_phone ILIKE $%d OR hr.room_number ILIKE $%d)", len(args), len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (r.check_in_date::timestamptz, r.id) > ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, resSelect+where+fmt.Sprintf(" ORDER BY r.check_in_date, r.id LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		var items []Reservation
		for rows.Next() {
			r, err := scanRes(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, *r)
		}
		rows.Close()
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.CheckInDate.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			if err := s.enrichRes(ctx, tx, &items[i]); err != nil {
				return err
			}
		}
		out = items
		return nil
	})
	if out == nil {
		out = []Reservation{}
	}
	return out, next, err
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Reservation, error) {
	var out *Reservation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getResTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "hotel.reservations.view", r.PropertyID); err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

// Update: data tamu/permintaan/tanggal (new|confirmed). Perubahan tanggal memvalidasi ulang availability.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in ReservationInput, ifVersion *int) (*Reservation, error) {
	p := authctx.Must(ctx)
	var out *Reservation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getResTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "hotel.reservations.update", r.PropertyID); err != nil {
			return err
		}
		if !has(r.AllowedActions, "update") {
			return apperr.InvalidTransition("Reservasi berstatus " + r.Status + " tidak dapat diubah")
		}
		if ifVersion != nil && *ifVersion != r.Version {
			return apperr.StaleVersion()
		}
		ci, co := r.CheckInDate, r.CheckOutDate
		if d, err := parseDate(in.CheckInDate); err != nil {
			return err
		} else if d != nil {
			ci = *d
		}
		if d, err := parseDate(in.CheckOutDate); err != nil {
			return err
		} else if d != nil {
			co = *d
		}
		if !co.After(ci) {
			return apperr.Validation("check_out_date harus setelah check_in_date")
		}
		rt, err := s.getRTTx(ctx, tx, r.RoomTypeID)
		if err != nil {
			return err
		}
		rateID, rate, total := r.RateID, r.RatePerNight, r.TotalAmount
		if !ci.Equal(r.CheckInDate) || !co.Equal(r.CheckOutDate) {
			av, err := s.availabilityTx(ctx, tx, rt, ci, co, &r.ID)
			if err != nil {
				return err
			}
			if av.AvailableRooms <= 0 {
				return apperr.Conflict("NO_AVAILABILITY", "Tidak ada kamar tersedia untuk tanggal baru")
			}
			rateID, rate, total = av.rateID, av.RatePerNight, av.TotalAmount
		}
		if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET guest_name = COALESCE(NULLIF(TRIM($2),''), guest_name), guest_phone = COALESCE($3, guest_phone), guest_email = COALESCE($4, guest_email), adults = COALESCE($5, adults), children = COALESCE($6, children),
			check_in_date = $7, check_out_date = $8, rate_id = $9, rate_per_night = $10, total_amount = $11, special_requests = COALESCE($12, special_requests), notes = COALESCE($13, notes), updated_by = $14 WHERE id = $1`,
			id, deref(in.GuestName), in.GuestPhone, in.GuestEmail, in.Adults, in.Children, ci, co, rateID, rate, total, in.SpecialRequests, in.Notes, p.UserID); err != nil {
			if db.IsExclusionViolation(err) {
				return apperr.Conflict("ROOM_UNAVAILABLE", "Kamar yang ditetapkan tidak tersedia pada tanggal baru")
			}
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "hotel_reservation", EntityID: &id, EntityLabel: r.ReservationNumber, After: in})
		out, err = s.getResTx(ctx, tx, id)
		return err
	})
	return out, err
}

type ActionInput struct {
	Reason             string     `json:"reason"`
	RoomLocationID     *uuid.UUID `json:"room_location_id"`
	CreateGuestAccount bool       `json:"create_guest_account"` // check-in: akun Guest App (Tenant App profile hotel)
	GuestEmail         *string    `json:"guest_email"`
	IssueInvoice       *bool      `json:"issue_invoice"` // check-out: default true bila Billing aktif
}

type ActionResult struct {
	Reservation       *Reservation `json:"reservation"`
	TemporaryPassword string       `json:"temporary_password,omitempty"`
	GuestAccountEmail string       `json:"guest_account_email,omitempty"`
}

// Act: confirm | assign_room | check_in | check_out | cancel | no_show — dengan linkage room status & housekeeping.
func (s *Service) Act(ctx context.Context, id uuid.UUID, action string, in ActionInput) (*ActionResult, error) {
	p := authctx.Must(ctx)
	res := &ActionResult{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getResTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "hotel.reservations.view", r.PropertyID); err != nil {
			return err
		}
		if !has(r.AllowedActions, action) {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk reservasi berstatus %s", action, r.Status))
		}
		var to, ev string
		switch action {
		case "confirm":
			to, ev = "confirmed", EventReservationConfirmed
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'confirmed', confirmed_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
		case "assign_room":
			if in.RoomLocationID == nil {
				return apperr.Validation("room_location_id wajib")
			}
			room, err := s.getRoomTx(ctx, tx, *in.RoomLocationID)
			if err != nil {
				return err
			}
			if room.PropertyID != r.PropertyID || room.RoomTypeID != r.RoomTypeID {
				return apperr.Validation("Kamar harus dari tipe kamar yang sama pada property ini")
			}
			if room.RoomStatus == "out_of_order" || room.RoomStatus == "out_of_service" || !room.IsActive {
				return apperr.Conflict("ROOM_UNAVAILABLE", "Kamar "+room.RoomNumber+" "+room.RoomStatus)
			}
			if r.Status == "checked_in" {
				// pindah kamar: kamar lama → dirty, kamar baru → occupied
				if room.RoomStatus == "occupied" {
					return apperr.Conflict("ROOM_UNAVAILABLE", "Kamar sedang ditempati")
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET room_location_id = $2, updated_by = $3 WHERE id = $1`, id, *in.RoomLocationID, p.UserID); err != nil {
				if db.IsExclusionViolation(err) {
					return apperr.Conflict("ROOM_UNAVAILABLE", "Kamar sudah dipesan pada tanggal tersebut")
				}
				return err
			}
			if r.Status == "checked_in" {
				if r.RoomLocationID != nil {
					old, _ := s.getRoomTx(ctx, tx, *r.RoomLocationID)
					if old != nil {
						if err := s.setRoomStatusTx(ctx, tx, old, "dirty", "Pindah kamar "+r.ReservationNumber, "room_move"); err != nil {
							return err
						}
						if err := s.createTurnoverTaskTx(ctx, tx, old, r); err != nil {
							return err
						}
					}
				}
				if err := s.setRoomStatusTx(ctx, tx, room, "occupied", "Check-in "+r.ReservationNumber, "room_move"); err != nil {
					return err
				}
			}
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "hotel_reservation", ObjectID: id, Action: "room_assigned", Payload: map[string]any{"room": room.RoomNumber}})
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "hotel_reservation", EntityID: &id, EntityLabel: r.ReservationNumber, After: map[string]any{"room": room.RoomNumber}})
			res.Reservation, err = s.getResTx(ctx, tx, id)
			return err
		case "check_in":
			roomID := r.RoomLocationID
			if in.RoomLocationID != nil {
				roomID = in.RoomLocationID
			}
			if roomID == nil {
				return apperr.Validation("Reservasi belum memiliki kamar; tetapkan kamar (room_location_id) saat check-in")
			}
			room, err := s.getRoomTx(ctx, tx, *roomID)
			if err != nil {
				return err
			}
			if room.RoomTypeID != r.RoomTypeID || room.PropertyID != r.PropertyID {
				return apperr.Validation("Kamar harus dari tipe kamar reservasi")
			}
			if room.RoomStatus == "occupied" || room.RoomStatus == "out_of_order" || room.RoomStatus == "out_of_service" {
				return apperr.Conflict("ROOM_NOT_READY", "Kamar "+room.RoomNumber+" berstatus "+room.RoomStatus)
			}
			if room.RoomStatus == "dirty" && !p.HasOnProperty("hotel.reservations.update", r.PropertyID) {
				return apperr.Conflict("ROOM_NOT_READY", "Kamar belum dibersihkan (Dirty)")
			}
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'checked_in', checked_in_at = now(), room_location_id = $2, updated_by = $3 WHERE id = $1`, id, *roomID, p.UserID); err != nil {
				if db.IsExclusionViolation(err) {
					return apperr.Conflict("ROOM_UNAVAILABLE", "Kamar sudah dipesan pada tanggal tersebut")
				}
				return err
			}
			if err := s.setRoomStatusTx(ctx, tx, room, "occupied", "Check-in "+r.ReservationNumber, "check_in"); err != nil {
				return err
			}
			// Guest App (Tenant App profile Hotel): akun tamu dengan akses kamar sampai check-out (AC-11)
			if in.CreateGuestAccount {
				email := strings.ToLower(strings.TrimSpace(deref(in.GuestEmail)))
				if email == "" {
					email = strings.ToLower(strings.TrimSpace(deref(r.GuestEmail)))
				}
				if email == "" {
					return apperr.Validation("guest_email wajib untuk membuat akun Guest App")
				}
				cr, err := s.TenantRel.CreateTx(ctx, tx, tenantrelation.CreateInput{PropertyID: r.PropertyID, FullName: r.GuestName, Email: email, Phone: deref(r.GuestPhone), Role: "tenant_user", OwnershipStatus: "guest", UnitIDs: []uuid.UUID{*roomID}, Source: "hotel_checkin"})
				if err != nil {
					return err
				}
				_, _ = tx.Exec(ctx, `UPDATE tenant_access SET valid_until = $2 WHERE tenant_user_id = $1`, cr.TenantUser.ID, r.CheckOutDate)
				_, _ = tx.Exec(ctx, `UPDATE hotel_reservations SET tenant_user_id = $2 WHERE id = $1`, id, cr.TenantUser.UserID)
				res.TemporaryPassword, res.GuestAccountEmail = cr.TemporaryPassword, email
			}
			to, ev = "checked_in", EventReservationCheckedIn
		case "check_out":
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'checked_out', checked_out_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
			if r.RoomLocationID != nil {
				room, err := s.getRoomTx(ctx, tx, *r.RoomLocationID)
				if err == nil {
					// Reservation → Room Status (Dirty) → Housekeeping (cleaning task turnover) — PRD §3.9 linkage
					if err := s.setRoomStatusTx(ctx, tx, room, "dirty", "Check-out "+r.ReservationNumber, "check_out"); err != nil {
						return err
					}
					if err := s.createTurnoverTaskTx(ctx, tx, room, r); err != nil {
						return err
					}
				}
			}
			// akses Guest App berakhir
			if r.TenantUserID != nil {
				_, _ = tx.Exec(ctx, `UPDATE tenant_access ta SET valid_until = current_date FROM tenant_users tu WHERE tu.id = ta.tenant_user_id AND tu.user_id = $1`, *r.TenantUserID)
			}
			// Billing linkage: invoice stay (bila Billing aktif & belum ada)
			issue := in.IssueInvoice == nil || *in.IssueInvoice
			if issue && r.InvoiceID == nil && r.TotalAmount > 0 && s.Billing != nil && p.HasOnProperty("billing.invoices.create", r.PropertyID) {
				pc, _ := s.Profile.ResolveTx(ctx, tx, r.PropertyID)
				if pc != nil && pc.Has(profile.CapBilling) {
					due := time.Now().Add(24 * time.Hour)
					items := []billing.Item{{Description: fmt.Sprintf("Stay %s · %d malam × %s", r.RoomTypeName, r.Nights, r.RoomTypeName), Quantity: float64(r.Nights), Unit: strPtr("malam"), UnitPrice: r.RatePerNight, Amount: r.TotalAmount}}
					desc := "Reservasi " + r.ReservationNumber + " · " + r.GuestName
					invID, err := s.Billing.CreateTx(ctx, tx, billing.InvoiceInput{PropertyID: &r.PropertyID, UnitLocationID: r.RoomLocationID, InvoiceType: strPtr("rental"), Description: &desc, DueAt: &due, Items: &items, IssueNow: true}, "hotel", &r.ID)
					if err != nil {
						return err
					}
					_, _ = tx.Exec(ctx, `UPDATE hotel_reservations SET invoice_id = $2 WHERE id = $1`, id, invID)
				}
			}
			to, ev = "checked_out", EventReservationCheckedOut
		case "cancel":
			reason := strings.TrimSpace(in.Reason)
			if reason == "" {
				return apperr.Validation("reason wajib").WithField("reason", "wajib")
			}
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'cancelled', cancelled_at = now(), cancel_reason = $2, updated_by = $3 WHERE id = $1`, id, reason, p.UserID); err != nil {
				return err
			}
			to, ev = "cancelled", EventReservationCancelled
		case "no_show":
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'no_show', no_show_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
			to, ev = "no_show", EventReservationNoShow
		default:
			return apperr.Validation("aksi tidak dikenal")
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "hotel_reservation", ObjectID: id, Action: audit.ActStatusChanged, From: r.Status, To: to, Payload: map[string]any{"action": action, "reason": in.Reason}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "hotel_reservation", EntityID: &id, EntityLabel: r.ReservationNumber, Before: map[string]any{"status": r.Status}, After: map[string]any{"status": to, "reason": in.Reason}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: ev, OrganizationID: p.OrganizationID, PropertyID: &r.PropertyID, ObjectType: "hotel_reservation", ObjectID: id, ObjectLabel: r.ReservationNumber + " · " + r.GuestName, ActorUserID: &p.UserID, Payload: map[string]any{"from": r.Status, "to": to, "domain": "tenant_relation"}})
		}
		for _, h := range s.resHooks {
			if err := h(ctx, tx, r, action, r.Status, to); err != nil {
				return err
			}
		}
		res.Reservation, err = s.getResTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// createTurnoverTaskTx: Cleaning Task "Room turnover" pada lokasi kamar (Housekeeping) — dibuat lewat Task Engine bersama.
func (s *Service) createTurnoverTaskTx(ctx context.Context, tx pgx.Tx, room *Room, r *Reservation) error {
	var teamID *uuid.UUID
	_ = tx.QueryRow(ctx, `SELECT id FROM teams WHERE property_id = $1 AND domain = 'housekeeping' AND is_active ORDER BY created_at LIMIT 1`, r.PropertyID).Scan(&teamID)
	due := time.Now().Add(3 * time.Hour)
	taskID, err := s.Ops.CreateTaskTx(ctx, tx, operations.CreateTaskInput{
		PropertyID: &r.PropertyID, TaskType: "cleaning", Title: fmt.Sprintf("Room turnover %s", room.RoomNumber), Description: strPtr("Check-out reservasi " + r.ReservationNumber + " — bersihkan dan siapkan kamar"),
		LocationID: &room.LocationID, Priority: "high", DueAt: &due, AssigneeTeamID: teamID, RequiresPhoto: true, SourceType: strPtr("hotel_reservation"), SourceID: &r.ID,
	})
	if err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `UPDATE cleaning_tasks SET cleaning_type = 'routine' WHERE task_id = $1`, taskID)
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "hotel_room", ObjectID: room.LocationID, Action: "turnover_task_created", Payload: map[string]any{"task_id": taskID, "reservation": r.ReservationNumber}})
	return nil
}

// ---------- Calendar & Occupancy (PRD §3.3 Hotel Reservation Operations) ----------

type CalendarEntry struct {
	ReservationID     uuid.UUID  `json:"reservation_id"`
	ReservationNumber string     `json:"reservation_number"`
	GuestName         string     `json:"guest_name"`
	RoomTypeID        uuid.UUID  `json:"room_type_id"`
	RoomTypeName      string     `json:"room_type_name"`
	RoomLocationID    *uuid.UUID `json:"room_location_id"`
	RoomNumber        *string    `json:"room_number"`
	CheckInDate       time.Time  `json:"check_in_date"`
	CheckOutDate      time.Time  `json:"check_out_date"`
	Status            string     `json:"status"`
}

type Calendar struct {
	From    time.Time       `json:"from"`
	To      time.Time       `json:"to"`
	Rooms   []Room          `json:"rooms"`
	Entries []CalendarEntry `json:"entries"`
}

func (s *Service) CalendarView(ctx context.Context, propertyID uuid.UUID, from, to time.Time) (*Calendar, error) {
	if err := iam.CanOnProperty(ctx, "hotel.reservations.view", propertyID); err != nil {
		return nil, err
	}
	if !to.After(from) || to.Sub(from) > 62*24*time.Hour {
		return nil, apperr.Validation("rentang kalender 1–62 hari")
	}
	out := &Calendar{From: from, To: to, Rooms: []Room{}, Entries: []CalendarEntry{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireHotel(ctx, tx, propertyID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, roomSelect+` WHERE r.property_id = $1 AND r.is_active ORDER BY rt.name, r.room_number`, propertyID)
		if err != nil {
			return err
		}
		for rows.Next() {
			r, err := scanRoom(rows)
			if err != nil {
				rows.Close()
				return err
			}
			out.Rooms = append(out.Rooms, *r)
		}
		rows.Close()
		rows, err = tx.Query(ctx, `SELECT r.id, r.reservation_number, r.guest_name, r.room_type_id, rt.name, r.room_location_id, hr.room_number, r.check_in_date, r.check_out_date, r.status
			FROM hotel_reservations r JOIN hotel_room_types rt ON rt.id = r.room_type_id LEFT JOIN hotel_rooms hr ON hr.location_id = r.room_location_id
			WHERE r.property_id = $1 AND r.status IN ('new','confirmed','checked_in','checked_out') AND r.stay && daterange($2::date, $3::date, '[]') ORDER BY r.check_in_date`, propertyID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e CalendarEntry
			if err := rows.Scan(&e.ReservationID, &e.ReservationNumber, &e.GuestName, &e.RoomTypeID, &e.RoomTypeName, &e.RoomLocationID, &e.RoomNumber, &e.CheckInDate, &e.CheckOutDate, &e.Status); err != nil {
				return err
			}
			out.Entries = append(out.Entries, e)
		}
		return rows.Err()
	})
	return out, err
}

type Occupancy struct {
	Date              string         `json:"date"`
	TotalRooms        int            `json:"total_rooms"`
	Occupied          int            `json:"occupied"`
	OccupancyPct      float64        `json:"occupancy_pct"`
	ByStatus          map[string]int `json:"by_status"`
	ArrivalsToday     int            `json:"arrivals_today"`
	DeparturesToday   int            `json:"departures_today"`
	InHouse           int            `json:"in_house"`
	PendingArrival    int            `json:"pending_arrival"` // belum check-in padahal tanggal tiba sudah lewat
	OpenGuestRequests int            `json:"open_guest_requests"`
	DirtyRooms        int            `json:"dirty_rooms"`
}

func (s *Service) OccupancyView(ctx context.Context, propertyID uuid.UUID) (*Occupancy, error) {
	if err := iam.CanOnProperty(ctx, "hotel.rooms.view", propertyID); err != nil {
		return nil, err
	}
	out := &Occupancy{ByStatus: map[string]int{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireHotel(ctx, tx, propertyID); err != nil {
			return err
		}
		loc := property.PropertyTimezone(ctx, tx, propertyID)
		today := time.Now().In(loc).Format("2006-01-02")
		out.Date = today
		rows, err := tx.Query(ctx, `SELECT room_status, count(*) FROM hotel_rooms WHERE property_id = $1 AND is_active GROUP BY room_status`, propertyID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var st string
			var n int
			if rows.Scan(&st, &n) == nil {
				out.ByStatus[st] = n
				out.TotalRooms += n
			}
		}
		rows.Close()
		out.Occupied = out.ByStatus["occupied"]
		out.DirtyRooms = out.ByStatus["dirty"]
		if out.TotalRooms > 0 {
			out.OccupancyPct = float64(out.Occupied) * 100 / float64(out.TotalRooms)
		}
		return tx.QueryRow(ctx, `SELECT
			count(*) FILTER (WHERE check_in_date = $2::date AND status IN ('new','confirmed')),
			count(*) FILTER (WHERE check_out_date = $2::date AND status = 'checked_in'),
			count(*) FILTER (WHERE status = 'checked_in'),
			count(*) FILTER (WHERE check_in_date < $2::date AND status IN ('new','confirmed')),
			(SELECT count(*) FROM service_requests sr JOIN hotel_rooms hr ON hr.location_id = sr.location_id WHERE hr.property_id = $1 AND sr.status NOT IN ('closed','cancelled'))
			FROM hotel_reservations WHERE property_id = $1`, propertyID, today).Scan(&out.ArrivalsToday, &out.DeparturesToday, &out.InHouse, &out.PendingArrival, &out.OpenGuestRequests)
	})
	return out, err
}

// ---------- Hook: Housekeeping → Room status (Dirty → Clean → Inspected → Available) ----------

type roomHook struct{ s *Service }

func (h roomHook) BeforeComplete(context.Context, pgx.Tx, *operations.WorkItem, operations.TransitionInput) error {
	return nil
}

func (h roomHook) AfterTransition(ctx context.Context, tx pgx.Tx, item *operations.WorkItem, action string, from, to workflow.Status) error {
	if action != workflow.ActComplete || item.Location.ID == nil {
		return nil
	}
	var room *Room
	room, err := h.s.getRoomTx(ctx, tx, *item.Location.ID)
	if err != nil {
		return nil // bukan kamar hotel
	}
	switch item.Type {
	case "cleaning":
		if room.RoomStatus == "dirty" {
			return h.s.setRoomStatusTx(ctx, tx, room, "clean", "Cleaning "+item.Number+" selesai", "housekeeping")
		}
	case "inspection":
		var isHK bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM housekeeping_inspections WHERE task_id = $1)`, item.ID).Scan(&isHK)
		if !isHK {
			return nil
		}
		// hasil inspeksi: dihitung dari checklist (hook engineering menulis hasil; hitung ulang agar tidak bergantung urutan hook)
		var notOK, findings int
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(not_ok_items),0) FROM checklist_runs WHERE object_type = 'task' AND object_id = $1`, item.ID).Scan(&notOK)
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM findings WHERE source_type = 'task' AND source_id = $1`, item.ID).Scan(&findings)
		if notOK == 0 && findings == 0 && (room.RoomStatus == "clean" || room.RoomStatus == "dirty") {
			if err := h.s.setRoomStatusTx(ctx, tx, room, "inspected", "Inspeksi "+item.Number+" lulus", "housekeeping"); err != nil {
				return err
			}
			return h.s.setRoomStatusTx(ctx, tx, room, "available", "Siap dijual", "housekeeping")
		}
		if (notOK > 0 || findings > 0) && room.RoomStatus == "clean" {
			return h.s.setRoomStatusTx(ctx, tx, room, "dirty", "Inspeksi "+item.Number+" gagal — rework", "housekeeping")
		}
	}
	return nil
}

var _ = housekeeping.AdhocCleaningInput{}

func strPtr(s string) *string { return &s }
