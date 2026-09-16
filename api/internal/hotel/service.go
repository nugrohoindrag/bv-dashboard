// Package hotel: Hotel Booking Management (PRD P1 v1.3 §3.9, WF-P1-007, AT-P1-000C; NC §71 Commercial › Hotel Booking Management).
// Inventori kamar milik property (TD-P1-005: HotelRoom = ekstensi Unit); reservasi ↔ room status ↔ Housekeeping;
// OTA/channel manager di luar scope (guardrail #21). Hanya aktif pada profile Hotel (capability hotel_booking).
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
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantrelation"
)

const (
	EventReservationCreated    = "hotel_reservation.created"
	EventReservationConfirmed  = "hotel_reservation.confirmed"
	EventReservationCheckedIn  = "hotel_reservation.checked_in"
	EventReservationCheckedOut = "hotel_reservation.checked_out"
	EventReservationCancelled  = "hotel_reservation.cancelled"
	EventReservationNoShow     = "hotel_reservation.no_show"
	EventRoomStatusChanged     = "hotel_room.status_changed"
)

type Service struct {
	DB           *db.DB
	Jobs         jobs.Enqueuer
	Profile      *profile.Service
	Property     *property.Service
	Housekeeping *housekeeping.Service
	Billing      *billing.Service
	TenantRel    *tenantrelation.Service
	Ops          *operations.Service

	resHooks []ReservationHook
}

// ReservationHook dipanggil di dalam transaksi Act setelah transisi status reservasi (from → to).
// Dipakai BVRooms untuk notifikasi customer & sinkronisasi status booking (Requirements v0.2 §4.2).
type ReservationHook func(ctx context.Context, tx pgx.Tx, r *Reservation, action, from, to string) error

func (s *Service) RegisterReservationHook(h ReservationHook) { s.resHooks = append(s.resHooks, h) }

func New(d *db.DB, j jobs.Enqueuer, prof *profile.Service, prop *property.Service, hk *housekeeping.Service, bill *billing.Service, tr *tenantrelation.Service, ops *operations.Service) *Service {
	s := &Service{DB: d, Jobs: j, Profile: prof, Property: prop, Housekeeping: hk, Billing: bill, TenantRel: tr, Ops: ops}
	// Reservation ↔ Room Status ↔ Housekeeping (PRD §3.9): cleaning selesai → Clean; inspeksi HK pass → Inspected → Available
	ops.RegisterHook("task:cleaning", roomHook{s: s})
	ops.RegisterHook("task:inspection", roomHook{s: s})
	// Guard perubahan profile (Onboarding Brief §19): reservasi aktif memblokir pindah dari Hotel
	prof.RegisterChangeGuard(func(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, from, to profile.Profile) (string, error) {
		if from != profile.Hotel {
			return "", nil
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM hotel_reservations WHERE property_id = $1 AND status IN ('new','confirmed','checked_in')`, propertyID).Scan(&n); err != nil {
			return "", err
		}
		if n > 0 {
			return fmt.Sprintf("%d reservasi hotel masih aktif", n), nil
		}
		return "", nil
	})
	return s
}

func (s *Service) requireHotel(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID) error {
	return s.Profile.RequireCapabilityTx(ctx, tx, propertyID, profile.CapHotelBooking)
}

// ---------- Room Type ----------

type RoomType struct {
	ID               uuid.UUID `json:"id"`
	PropertyID       uuid.UUID `json:"property_id"`
	RoomTypeCode     string    `json:"room_type_code"`
	Name             string    `json:"name"`
	Description      *string   `json:"description"`
	CapacityAdults   int       `json:"capacity_adults"`
	CapacityChildren int       `json:"capacity_children"`
	BedType          *string   `json:"bed_type"`
	SizeM2           *float64  `json:"size_m2"`
	Amenities        []string  `json:"amenities"`
	BaseRate         int64     `json:"base_rate"`
	CurrencyCode     string    `json:"currency_code"`
	Status           string    `json:"status"`
	RoomCount        int       `json:"room_count"`
	Version          int       `json:"version"`
}

const rtSelect = `SELECT rt.id, rt.property_id, rt.room_type_code, rt.name, rt.description, rt.capacity_adults, rt.capacity_children, rt.bed_type, rt.size_m2, rt.amenities, rt.base_rate, rt.currency_code, rt.status, rt.version,
	(SELECT count(*) FROM hotel_rooms r WHERE r.room_type_id = rt.id AND r.is_active) FROM hotel_room_types rt`

func scanRT(row pgx.Row) (*RoomType, error) {
	var t RoomType
	if err := row.Scan(&t.ID, &t.PropertyID, &t.RoomTypeCode, &t.Name, &t.Description, &t.CapacityAdults, &t.CapacityChildren, &t.BedType, &t.SizeM2, &t.Amenities, &t.BaseRate, &t.CurrencyCode, &t.Status, &t.Version, &t.RoomCount); err != nil {
		return nil, err
	}
	t.CurrencyCode = strings.TrimSpace(t.CurrencyCode)
	return &t, nil
}

type RoomTypeInput struct {
	PropertyID       *uuid.UUID `json:"property_id"`
	Name             *string    `json:"name"`
	Description      *string    `json:"description"`
	CapacityAdults   *int       `json:"capacity_adults"`
	CapacityChildren *int       `json:"capacity_children"`
	BedType          *string    `json:"bed_type"`
	SizeM2           *float64   `json:"size_m2"`
	Amenities        *[]string  `json:"amenities"`
	BaseRate         *int64     `json:"base_rate"`
	Status           *string    `json:"status"`
}

func (s *Service) ListRoomTypes(ctx context.Context, propertyID uuid.UUID) ([]RoomType, error) {
	if err := iam.CanOnProperty(ctx, "hotel.room_types.view", propertyID); err != nil {
		return nil, err
	}
	out := []RoomType{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireHotel(ctx, tx, propertyID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, rtSelect+` WHERE rt.property_id = $1 ORDER BY rt.name`, propertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanRT(rows)
			if err != nil {
				return err
			}
			out = append(out, *t)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) getRTTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*RoomType, error) {
	t, err := scanRT(tx.QueryRow(ctx, rtSelect+` WHERE rt.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Room type")
		}
		return nil, err
	}
	return t, nil
}

func (s *Service) CreateRoomType(ctx context.Context, in RoomTypeInput) (*RoomType, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil {
		return nil, apperr.Validation("property_id wajib")
	}
	if err := iam.CanOnProperty(ctx, "hotel.room_types.create", *in.PropertyID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(deref(in.Name))
	if name == "" {
		return nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	var out *RoomType
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireHotel(ctx, tx, *in.PropertyID); err != nil {
			return err
		}
		loc := property.PropertyTimezone(ctx, tx, *in.PropertyID)
		code, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixHotelRoomType, time.Now(), loc)
		if err != nil {
			return err
		}
		amen := []string{}
		if in.Amenities != nil {
			amen = *in.Amenities
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO hotel_room_types (organization_id, property_id, room_type_code, name, description, capacity_adults, capacity_children, bed_type, size_m2, amenities, base_rate, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,COALESCE($6,2),COALESCE($7,0),$8,$9,$10,COALESCE($11,0),$12,$12) RETURNING id`,
			p.OrganizationID, *in.PropertyID, code, name, in.Description, in.CapacityAdults, in.CapacityChildren, in.BedType, in.SizeM2, amen, in.BaseRate, p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "hotel_room_type", EntityID: &id, EntityLabel: code + " " + name})
		out, err = s.getRTTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) UpdateRoomType(ctx context.Context, id uuid.UUID, in RoomTypeInput, ifVersion *int) (*RoomType, error) {
	p := authctx.Must(ctx)
	var out *RoomType
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := s.getRTTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "hotel.room_types.update", t.PropertyID); err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != t.Version {
			return apperr.StaleVersion()
		}
		if in.Status != nil && *in.Status != "active" && *in.Status != "archived" {
			return apperr.Validation("status harus active|archived")
		}
		var amen any
		if in.Amenities != nil {
			amen = *in.Amenities
		}
		if _, err := tx.Exec(ctx, `UPDATE hotel_room_types SET name = COALESCE(NULLIF(TRIM($2),''), name), description = COALESCE($3, description), capacity_adults = COALESCE($4, capacity_adults), capacity_children = COALESCE($5, capacity_children), bed_type = COALESCE($6, bed_type), size_m2 = COALESCE($7, size_m2), amenities = COALESCE($8, amenities), base_rate = COALESCE($9, base_rate), status = COALESCE($10, status), updated_by = $11 WHERE id = $1`,
			id, deref(in.Name), in.Description, in.CapacityAdults, in.CapacityChildren, in.BedType, in.SizeM2, amen, in.BaseRate, in.Status, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "hotel_room_type", EntityID: &id, EntityLabel: t.RoomTypeCode, After: in})
		out, err = s.getRTTx(ctx, tx, id)
		return err
	})
	return out, err
}

// ---------- Room (= Unit) ----------

type Room struct {
	LocationID      uuid.UUID  `json:"location_id"`
	PropertyID      uuid.UUID  `json:"property_id"`
	RoomCode        string     `json:"room_code"`
	RoomNumber      string     `json:"room_number"`
	Name            string     `json:"name"`
	RoomTypeID      uuid.UUID  `json:"room_type_id"`
	RoomTypeName    string     `json:"room_type_name"`
	FloorID         *uuid.UUID `json:"floor_id"`
	FloorName       *string    `json:"floor_name"`
	RoomStatus      string     `json:"room_status"`
	StatusNote      *string    `json:"status_note"`
	StatusChangedAt time.Time  `json:"status_changed_at"`
	IsActive        bool       `json:"is_active"`
	CurrentGuest    *string    `json:"current_guest"`
	CurrentResID    *uuid.UUID `json:"current_reservation_id"`
	NextArrival     *time.Time `json:"next_arrival"`
	OpenCleaning    int        `json:"open_cleaning_tasks"`
	AllowedStatuses []string   `json:"allowed_statuses"`
	Version         int        `json:"version"`
}

const roomSelect = `SELECT r.location_id, r.property_id, r.room_code, r.room_number, l.name, r.room_type_id, rt.name, l.parent_id, pl.name, r.room_status, r.status_note, r.status_changed_at, r.is_active, r.version,
	(SELECT guest_name FROM hotel_reservations x WHERE x.room_location_id = r.location_id AND x.status = 'checked_in' LIMIT 1),
	(SELECT id FROM hotel_reservations x WHERE x.room_location_id = r.location_id AND x.status = 'checked_in' LIMIT 1),
	(SELECT min(check_in_date)::timestamptz FROM hotel_reservations x WHERE x.room_location_id = r.location_id AND x.status IN ('new','confirmed') AND x.check_in_date >= current_date),
	(SELECT count(*) FROM tasks t WHERE t.location_id = r.location_id AND t.task_type = 'cleaning' AND t.status NOT IN ('completed','closed','cancelled'))
	FROM hotel_rooms r JOIN locations l ON l.id = r.location_id JOIN hotel_room_types rt ON rt.id = r.room_type_id LEFT JOIN locations pl ON pl.id = l.parent_id`

func scanRoom(row pgx.Row) (*Room, error) {
	var r Room
	if err := row.Scan(&r.LocationID, &r.PropertyID, &r.RoomCode, &r.RoomNumber, &r.Name, &r.RoomTypeID, &r.RoomTypeName, &r.FloorID, &r.FloorName, &r.RoomStatus, &r.StatusNote, &r.StatusChangedAt, &r.IsActive, &r.Version, &r.CurrentGuest, &r.CurrentResID, &r.NextArrival, &r.OpenCleaning); err != nil {
		return nil, err
	}
	r.AllowedStatuses = allowedRoomStatuses(r.RoomStatus)
	return &r, nil
}

// allowedRoomStatuses: transisi manual yang diizinkan (occupied hanya lewat check-in; dirty lewat check-out/manual).
func allowedRoomStatuses(cur string) []string {
	switch cur {
	case "occupied":
		return []string{"out_of_order"}
	case "dirty":
		return []string{"clean", "inspected", "out_of_order", "out_of_service"}
	case "clean":
		return []string{"inspected", "available", "dirty", "out_of_order", "out_of_service"}
	case "inspected":
		return []string{"available", "dirty", "out_of_order", "out_of_service"}
	case "available":
		return []string{"dirty", "out_of_order", "out_of_service"}
	case "out_of_order", "out_of_service":
		return []string{"dirty", "available"}
	}
	return nil
}

type RoomInput struct {
	PropertyID *uuid.UUID `json:"property_id"`
	FloorID    *uuid.UUID `json:"floor_id"`    // parent location (floor) untuk unit baru
	LocationID *uuid.UUID `json:"location_id"` // atau: unit yang sudah ada dijadikan kamar
	RoomTypeID uuid.UUID  `json:"room_type_id"`
	RoomNumber string     `json:"room_number"`
	Name       *string    `json:"name"`
	SizeM2     *float64   `json:"size_m2"`
}

// CreateRoom: kamar = unit (location_type unit, unit_type hotel_room) + ekstensi hotel_rooms.
func (s *Service) CreateRoom(ctx context.Context, in RoomInput) (*Room, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil {
		return nil, apperr.Validation("property_id wajib")
	}
	if err := iam.CanOnProperty(ctx, "hotel.rooms.create", *in.PropertyID); err != nil {
		return nil, err
	}
	in.RoomNumber = strings.TrimSpace(in.RoomNumber)
	if in.RoomNumber == "" {
		return nil, apperr.Validation("room_number wajib").WithField("room_number", "wajib")
	}
	var out *Room
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireHotel(ctx, tx, *in.PropertyID); err != nil {
			return err
		}
		rt, err := s.getRTTx(ctx, tx, in.RoomTypeID)
		if err != nil {
			return err
		}
		if rt.PropertyID != *in.PropertyID {
			return apperr.Validation("room_type_id bukan milik property ini")
		}
		var locID uuid.UUID
		if in.LocationID != nil {
			var lt string
			var pid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT location_type, property_id FROM locations WHERE id = $1 AND deleted_at IS NULL`, *in.LocationID).Scan(&lt, &pid); err != nil || lt != "unit" || pid != *in.PropertyID {
				return apperr.Validation("location_id harus unit di property ini")
			}
			locID = *in.LocationID
			_, _ = tx.Exec(ctx, `UPDATE units SET unit_type = 'hotel_room' WHERE location_id = $1`, locID)
		} else {
			if in.FloorID == nil {
				return apperr.Validation("floor_id wajib untuk kamar baru")
			}
			name := deref(in.Name)
			if name == "" {
				name = "Room " + in.RoomNumber
			}
			details := map[string]any{"unit_number": in.RoomNumber, "unit_type": "hotel_room"}
			if in.SizeM2 != nil {
				details["area_m2"] = *in.SizeM2
			}
			locID, err = s.Property.CreateLocationInTx(ctx, tx, property.CreateLocationInput{LocationType: property.LTUnit, ParentID: in.FloorID, Name: name, Details: details})
			if err != nil {
				return err
			}
		}
		loc := property.PropertyTimezone(ctx, tx, *in.PropertyID)
		code, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixHotelRoom, time.Now(), loc)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO hotel_rooms (location_id, organization_id, property_id, room_code, room_type_id, room_number, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$7)`, locID, p.OrganizationID, *in.PropertyID, code, in.RoomTypeID, in.RoomNumber, p.UserID); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("ROOM_EXISTS", "Unit ini sudah terdaftar sebagai kamar")
			}
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "hotel_room", EntityID: &locID, EntityLabel: code + " " + in.RoomNumber})
		out, err = s.getRoomTx(ctx, tx, locID)
		return err
	})
	return out, err
}

func (s *Service) getRoomTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Room, error) {
	r, err := scanRoom(tx.QueryRow(ctx, roomSelect+` WHERE r.location_id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Room")
		}
		return nil, err
	}
	return r, nil
}

func (s *Service) ListRooms(ctx context.Context, propertyID uuid.UUID, roomTypeID *uuid.UUID, statuses []string) ([]Room, error) {
	if err := iam.CanOnProperty(ctx, "hotel.rooms.view", propertyID); err != nil {
		return nil, err
	}
	out := []Room{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireHotel(ctx, tx, propertyID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, roomSelect+` WHERE r.property_id = $1 AND r.is_active AND ($2::uuid IS NULL OR r.room_type_id = $2) AND ($3::text[] IS NULL OR cardinality($3::text[]) = 0 OR r.room_status = ANY($3)) ORDER BY pl.sort_order, r.room_number`, propertyID, roomTypeID, statuses)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRoom(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}

type RoomStatusInput struct {
	RoomStatus string  `json:"room_status"`
	Note       *string `json:"note"`
}

// SetRoomStatus: perubahan manual (Housekeeping/Reception/Engineering) — occupied hanya via check-in.
func (s *Service) SetRoomStatus(ctx context.Context, id uuid.UUID, in RoomStatusInput) (*Room, error) {
	var out *Room
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getRoomTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "hotel.rooms.set_status", r.PropertyID); err != nil {
			return err
		}
		if !has(r.AllowedStatuses, in.RoomStatus) {
			return apperr.InvalidTransition(fmt.Sprintf("Room %s: %s → %s tidak diizinkan", r.RoomNumber, r.RoomStatus, in.RoomStatus))
		}
		if err := s.setRoomStatusTx(ctx, tx, r, in.RoomStatus, deref(in.Note), "manual"); err != nil {
			return err
		}
		out, err = s.getRoomTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) setRoomStatusTx(ctx context.Context, tx pgx.Tx, r *Room, to, note, reason string) error {
	p := authctx.Must(ctx)
	if r.RoomStatus == to {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE hotel_rooms SET room_status = $2, status_note = NULLIF($3,''), status_changed_at = now(), updated_by = $4 WHERE location_id = $1`, r.LocationID, to, note, actorOrNil(p)); err != nil {
		return err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "hotel_room", ObjectID: r.LocationID, Action: audit.ActStatusChanged, From: r.RoomStatus, To: to, Payload: map[string]any{"reason": reason, "note": note}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "hotel_room", EntityID: &r.LocationID, EntityLabel: r.RoomCode + " " + r.RoomNumber, Before: map[string]any{"room_status": r.RoomStatus}, After: map[string]any{"room_status": to, "reason": reason}})
	if s.Jobs != nil && (to == "out_of_order" || to == "dirty") {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventRoomStatusChanged, OrganizationID: p.OrganizationID, PropertyID: &r.PropertyID, ObjectType: "hotel_room", ObjectID: r.LocationID, ObjectLabel: "Room " + r.RoomNumber, ActorUserID: actorOrNil(p), Payload: map[string]any{"from": r.RoomStatus, "to": to, "domain": map[string]string{"out_of_order": "engineering", "dirty": "housekeeping"}[to]}})
	}
	r.RoomStatus = to
	return nil
}

// ---------- Rate ----------

type Rate struct {
	ID           uuid.UUID  `json:"id"`
	PropertyID   uuid.UUID  `json:"property_id"`
	RateCode     string     `json:"rate_code"`
	RoomTypeID   uuid.UUID  `json:"room_type_id"`
	RoomTypeName string     `json:"room_type_name"`
	Name         string     `json:"name"`
	RatePerNight int64      `json:"rate_per_night"`
	CurrencyCode string     `json:"currency_code"`
	ValidFrom    *time.Time `json:"valid_from"`
	ValidUntil   *time.Time `json:"valid_until"`
	Weekdays     []int      `json:"weekdays"`
	MinNights    int        `json:"min_nights"`
	Priority     int        `json:"priority"`
	Status       string     `json:"status"`
	Version      int        `json:"version"`
}

const rateSelect = `SELECT ra.id, ra.property_id, ra.rate_code, ra.room_type_id, rt.name, ra.name, ra.rate_per_night, ra.currency_code, ra.valid_from, ra.valid_until, ra.weekdays, ra.min_nights, ra.priority, ra.status, ra.version FROM hotel_rates ra JOIN hotel_room_types rt ON rt.id = ra.room_type_id`

func scanRate(row pgx.Row) (*Rate, error) {
	var r Rate
	if err := row.Scan(&r.ID, &r.PropertyID, &r.RateCode, &r.RoomTypeID, &r.RoomTypeName, &r.Name, &r.RatePerNight, &r.CurrencyCode, &r.ValidFrom, &r.ValidUntil, &r.Weekdays, &r.MinNights, &r.Priority, &r.Status, &r.Version); err != nil {
		return nil, err
	}
	r.CurrencyCode = strings.TrimSpace(r.CurrencyCode)
	return &r, nil
}

type RateInput struct {
	RoomTypeID   *uuid.UUID `json:"room_type_id"`
	Name         *string    `json:"name"`
	RatePerNight *int64     `json:"rate_per_night"`
	ValidFrom    *string    `json:"valid_from"`
	ValidUntil   *string    `json:"valid_until"`
	Weekdays     *[]int     `json:"weekdays"`
	MinNights    *int       `json:"min_nights"`
	Priority     *int       `json:"priority"`
	Status       *string    `json:"status"`
}

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

func (s *Service) ListRates(ctx context.Context, propertyID uuid.UUID, roomTypeID *uuid.UUID) ([]Rate, error) {
	if err := iam.CanOnProperty(ctx, "hotel.rates.view", propertyID); err != nil {
		return nil, err
	}
	out := []Rate{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, rateSelect+` WHERE ra.property_id = $1 AND ($2::uuid IS NULL OR ra.room_type_id = $2) ORDER BY rt.name, ra.priority DESC, ra.name`, propertyID, roomTypeID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRate(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) CreateRate(ctx context.Context, in RateInput) (*Rate, error) {
	p := authctx.Must(ctx)
	if in.RoomTypeID == nil || in.RatePerNight == nil || strings.TrimSpace(deref(in.Name)) == "" {
		return nil, apperr.Validation("room_type_id, name, rate_per_night wajib")
	}
	vf, err := parseDate(in.ValidFrom)
	if err != nil {
		return nil, err
	}
	vu, err := parseDate(in.ValidUntil)
	if err != nil {
		return nil, err
	}
	var out *Rate
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rt, err := s.getRTTx(ctx, tx, *in.RoomTypeID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "hotel.rates.create", rt.PropertyID); err != nil {
			return err
		}
		loc := property.PropertyTimezone(ctx, tx, rt.PropertyID)
		code, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixHotelRate, time.Now(), loc)
		if err != nil {
			return err
		}
		wd := []int{0, 1, 2, 3, 4, 5, 6}
		if in.Weekdays != nil {
			wd = *in.Weekdays
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO hotel_rates (organization_id, property_id, rate_code, room_type_id, name, rate_per_night, valid_from, valid_until, weekdays, min_nights, priority, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,COALESCE($10,1),COALESCE($11,0),$12,$12) RETURNING id`, p.OrganizationID, rt.PropertyID, code, rt.ID, strings.TrimSpace(*in.Name), *in.RatePerNight, vf, vu, wd, in.MinNights, in.Priority, p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "hotel_rate", EntityID: &id, EntityLabel: code})
		out, err = scanRate(tx.QueryRow(ctx, rateSelect+` WHERE ra.id = $1`, id))
		return err
	})
	return out, err
}

func (s *Service) UpdateRate(ctx context.Context, id uuid.UUID, in RateInput) (*Rate, error) {
	p := authctx.Must(ctx)
	var out *Rate
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := scanRate(tx.QueryRow(ctx, rateSelect+` WHERE ra.id = $1`, id))
		if err != nil {
			return apperr.NotFound("Rate")
		}
		if err := iam.CanOnProperty(ctx, "hotel.rates.update", r.PropertyID); err != nil {
			return err
		}
		vf, err := parseDate(in.ValidFrom)
		if err != nil {
			return err
		}
		vu, err := parseDate(in.ValidUntil)
		if err != nil {
			return err
		}
		var wd any
		if in.Weekdays != nil {
			wd = *in.Weekdays
		}
		if _, err := tx.Exec(ctx, `UPDATE hotel_rates SET name = COALESCE(NULLIF(TRIM($2),''), name), rate_per_night = COALESCE($3, rate_per_night), valid_from = COALESCE($4, valid_from), valid_until = COALESCE($5, valid_until), weekdays = COALESCE($6, weekdays), min_nights = COALESCE($7, min_nights), priority = COALESCE($8, priority), status = COALESCE($9, status), updated_by = $10 WHERE id = $1`,
			id, deref(in.Name), in.RatePerNight, vf, vu, wd, in.MinNights, in.Priority, in.Status, p.UserID); err != nil {
			return err
		}
		out, err = scanRate(tx.QueryRow(ctx, rateSelect+` WHERE ra.id = $1`, id))
		return err
	})
	return out, err
}

// resolveRateTx: rate aktif paling spesifik untuk tipe kamar & malam pertama; fallback base_rate. Total = Σ per malam.
func (s *Service) resolveRateTx(ctx context.Context, tx pgx.Tx, rt *RoomType, checkIn, checkOut time.Time) (*uuid.UUID, int64, int64, error) {
	rows, err := tx.Query(ctx, `SELECT id, rate_per_night, valid_from, valid_until, weekdays, min_nights FROM hotel_rates WHERE room_type_id = $1 AND status = 'active' ORDER BY priority DESC, created_at`, rt.ID)
	if err != nil {
		return nil, 0, 0, err
	}
	type rate struct {
		id     uuid.UUID
		amount int64
		vf, vu *time.Time
		wd     []int
		minN   int
	}
	var rates []rate
	for rows.Next() {
		var r rate
		if err := rows.Scan(&r.id, &r.amount, &r.vf, &r.vu, &r.wd, &r.minN); err != nil {
			rows.Close()
			return nil, 0, 0, err
		}
		rates = append(rates, r)
	}
	rows.Close()
	nights := int(checkOut.Sub(checkIn).Hours() / 24)
	var total int64
	var firstRate *uuid.UUID
	var firstAmt int64
	for i := 0; i < nights; i++ {
		day := checkIn.AddDate(0, 0, i)
		amt := rt.BaseRate
		var rid *uuid.UUID
		for _, r := range rates {
			if r.minN > nights || (r.vf != nil && day.Before(*r.vf)) || (r.vu != nil && day.After(*r.vu)) {
				continue
			}
			okDay := false
			for _, w := range r.wd {
				if w == int(day.Weekday()) {
					okDay = true
				}
			}
			if !okDay {
				continue
			}
			amt, rid = r.amount, &r.id
			break
		}
		if i == 0 {
			firstRate, firstAmt = rid, amt
		}
		total += amt
	}
	return firstRate, firstAmt, total, nil
}

// ---------- Availability (per tipe kamar) ----------

type Availability struct {
	RoomTypeID     uuid.UUID   `json:"room_type_id"`
	RoomTypeName   string      `json:"room_type_name"`
	TotalRooms     int         `json:"total_rooms"`
	AvailableRooms int         `json:"available_rooms"` // minimum di seluruh malam
	RatePerNight   int64       `json:"rate_per_night"`
	TotalAmount    int64       `json:"total_amount"`
	Nights         int         `json:"nights"`
	FreeRoomIDs    []uuid.UUID `json:"free_room_ids"` // kamar yang bebas sepanjang stay (untuk assignment)
	rateID         *uuid.UUID
}

// availabilityTx: total kamar aktif (bukan OOO/OOS) − reservasi aktif yang overlap per malam.
func (s *Service) availabilityTx(ctx context.Context, tx pgx.Tx, rt *RoomType, checkIn, checkOut time.Time, excludeRes *uuid.UUID) (*Availability, error) {
	out := &Availability{RoomTypeID: rt.ID, RoomTypeName: rt.Name, Nights: int(checkOut.Sub(checkIn).Hours() / 24)}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM hotel_rooms WHERE room_type_id = $1 AND is_active AND room_status NOT IN ('out_of_order','out_of_service')`, rt.ID).Scan(&out.TotalRooms); err != nil {
		return nil, err
	}
	// maksimum reservasi aktif yang overlap pada satu malam dalam rentang
	var maxBooked int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(c),0) FROM (
		SELECT d::date AS night, count(r.id) AS c FROM generate_series($2::date, ($3::date - 1), interval '1 day') d
		LEFT JOIN hotel_reservations r ON r.room_type_id = $1 AND r.status IN ('new','confirmed','checked_in') AND r.stay @> d::date AND ($4::uuid IS NULL OR r.id <> $4)
		GROUP BY d) x`, rt.ID, checkIn, checkOut, excludeRes).Scan(&maxBooked); err != nil {
		return nil, err
	}
	out.AvailableRooms = out.TotalRooms - maxBooked
	if out.AvailableRooms < 0 {
		out.AvailableRooms = 0
	}
	rows, err := tx.Query(ctx, `SELECT r.location_id FROM hotel_rooms r WHERE r.room_type_id = $1 AND r.is_active AND r.room_status NOT IN ('out_of_order','out_of_service')
		AND NOT EXISTS (SELECT 1 FROM hotel_reservations x WHERE x.room_location_id = r.location_id AND x.status IN ('new','confirmed','checked_in') AND x.stay && daterange($2::date, $3::date, '[)') AND ($4::uuid IS NULL OR x.id <> $4))
		ORDER BY r.room_number`, rt.ID, checkIn, checkOut, excludeRes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out.FreeRoomIDs = []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out.FreeRoomIDs = append(out.FreeRoomIDs, id)
	}
	out.rateID, out.RatePerNight, out.TotalAmount, err = s.resolveRateTx(ctx, tx, rt, checkIn, checkOut)
	return out, err
}

func (s *Service) Availability(ctx context.Context, propertyID uuid.UUID, checkIn, checkOut time.Time, roomTypeID *uuid.UUID) ([]Availability, error) {
	if err := iam.CanOnProperty(ctx, "hotel.reservations.view", propertyID); err != nil {
		return nil, err
	}
	if !checkOut.After(checkIn) {
		return nil, apperr.Validation("check_out harus setelah check_in")
	}
	out := []Availability{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.requireHotel(ctx, tx, propertyID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, rtSelect+` WHERE rt.property_id = $1 AND rt.status = 'active' AND ($2::uuid IS NULL OR rt.id = $2) ORDER BY rt.name`, propertyID, roomTypeID)
		if err != nil {
			return err
		}
		var types []RoomType
		for rows.Next() {
			t, err := scanRT(rows)
			if err != nil {
				rows.Close()
				return err
			}
			types = append(types, *t)
		}
		rows.Close()
		for i := range types {
			a, err := s.availabilityTx(ctx, tx, &types[i], checkIn, checkOut, nil)
			if err != nil {
				return err
			}
			out = append(out, *a)
		}
		return nil
	})
	return out, err
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

var _ = workflow.New
var _ = httpx.NewList[int]
