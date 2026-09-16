// Package booking: Facility Booking (PRD P1 v1.3 §21, WF-P1-004, AT-P1-010). `bookings` = generic Booking (NC §72 #11);
// reservasi domain (hotel/rental/sale) berada di modul masing-masing (TD-P1-004). Konflik waktu dicegah EXCLUDE constraint DB.
package booking

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
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantscope"
)

const (
	EventBookingCreated   = "booking.created"
	EventBookingConfirmed = "booking.confirmed"
	EventBookingRejected  = "booking.rejected"
	EventBookingCancelled = "booking.cancelled"
	EventBookingCheckedIn = "booking.checked_in"
	EventBookingCompleted = "booking.completed"
	EventBookingNoShow    = "booking.no_show"
)

type Service struct {
	DB      *db.DB
	Jobs    jobs.Enqueuer
	Profile *profile.Service
}

func New(d *db.DB, j jobs.Enqueuer, prof *profile.Service) *Service {
	return &Service{DB: d, Jobs: j, Profile: prof}
}

// ---------- Facility ----------

type Facility struct {
	ID                 uuid.UUID  `json:"id"`
	PropertyID         uuid.UUID  `json:"property_id"`
	FacilityCode       string     `json:"facility_code"`
	Name               string     `json:"name"`
	Description        *string    `json:"description"`
	FacilityType       string     `json:"facility_type"`
	LocationID         *uuid.UUID `json:"location_id"`
	LocationPath       *string    `json:"location_path"`
	Capacity           *int       `json:"capacity"`
	RequiresApproval   *bool      `json:"requires_approval"`  // NULL = ikut property
	EffectiveApproval  bool       `json:"effective_approval"` // hasil resolusi
	SlotMinutes        int        `json:"slot_minutes"`
	MinDurationMinutes int        `json:"min_duration_minutes"`
	MaxDurationMinutes int        `json:"max_duration_minutes"`
	AdvanceBookingDays int        `json:"advance_booking_days"`
	OpenTime           string     `json:"open_time"`
	CloseTime          string     `json:"close_time"`
	Weekdays           []int      `json:"weekdays"`
	Rules              *string    `json:"rules"`
	ImageAttachmentID  *uuid.UUID `json:"image_attachment_id"`
	IsActive           bool       `json:"is_active"`
	UpcomingBookings   int        `json:"upcoming_bookings"`
	Version            int        `json:"version"`
}

const facSelect = `SELECT f.id, f.property_id, f.facility_code, f.name, f.description, f.facility_type, f.location_id, f.capacity, f.requires_approval, f.slot_minutes, f.min_duration_minutes, f.max_duration_minutes,
	f.advance_booking_days, to_char(f.open_time,'HH24:MI'), to_char(f.close_time,'HH24:MI'), f.weekdays, f.rules, f.image_attachment_id, f.is_active, f.version,
	(SELECT count(*) FROM bookings b WHERE b.facility_id = f.id AND b.status IN ('pending','confirmed') AND b.starts_at >= now()),
	(SELECT string_agg(a.name, ' · ' ORDER BY a.depth) FROM locations l JOIN locations a ON a.path @> l.path AND a.depth > 0 WHERE l.id = f.location_id)
	FROM facilities f`

func scanFacility(row pgx.Row) (*Facility, error) {
	var f Facility
	if err := row.Scan(&f.ID, &f.PropertyID, &f.FacilityCode, &f.Name, &f.Description, &f.FacilityType, &f.LocationID, &f.Capacity, &f.RequiresApproval, &f.SlotMinutes, &f.MinDurationMinutes, &f.MaxDurationMinutes,
		&f.AdvanceBookingDays, &f.OpenTime, &f.CloseTime, &f.Weekdays, &f.Rules, &f.ImageAttachmentID, &f.IsActive, &f.Version, &f.UpcomingBookings, &f.LocationPath); err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Service) resolveApproval(ctx context.Context, tx pgx.Tx, f *Facility) {
	if f.RequiresApproval != nil {
		f.EffectiveApproval = *f.RequiresApproval
		return
	}
	if pc, err := s.Profile.ResolveTx(ctx, tx, f.PropertyID); err == nil {
		f.EffectiveApproval = pc.Config.BookingApprovalRequired
	}
}

type FacilityInput struct {
	PropertyID         *uuid.UUID `json:"property_id"`
	Name               *string    `json:"name"`
	Description        *string    `json:"description"`
	FacilityType       *string    `json:"facility_type"`
	LocationID         *uuid.UUID `json:"location_id"`
	Capacity           *int       `json:"capacity"`
	RequiresApproval   *bool      `json:"requires_approval"`
	SlotMinutes        *int       `json:"slot_minutes"`
	MinDurationMinutes *int       `json:"min_duration_minutes"`
	MaxDurationMinutes *int       `json:"max_duration_minutes"`
	AdvanceBookingDays *int       `json:"advance_booking_days"`
	OpenTime           *string    `json:"open_time"`
	CloseTime          *string    `json:"close_time"`
	Weekdays           *[]int     `json:"weekdays"`
	Rules              *string    `json:"rules"`
	ImageAttachmentID  *uuid.UUID `json:"image_attachment_id"`
	IsActive           *bool      `json:"is_active"`
}

var facilityTypes = map[string]bool{"meeting_room": true, "function_hall": true, "gym": true, "pool": true, "court": true, "bbq": true, "coworking": true, "lounge": true, "parking": true, "other": true}

func parseHHMM(v string) (time.Duration, error) {
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, apperr.Validation("format jam harus HH:MM")
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

func (s *Service) ListFacilities(ctx context.Context, propertyID *uuid.UUID, activeOnly bool) ([]Facility, error) {
	p := authctx.Must(ctx)
	var out []Facility
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE f.deleted_at IS NULL"
		var args []any
		if propertyID != nil {
			if err := iam.CanOnProperty(ctx, "booking.facilities.view", *propertyID); err != nil {
				return err
			}
			args = append(args, *propertyID)
			where += " AND f.property_id = $1"
		} else if pids, all := p.PropertyIDsFor("booking.facilities.view"); !all {
			args = append(args, pids)
			where += " AND f.property_id = ANY($1)"
		}
		if activeOnly {
			where += " AND f.is_active"
		}
		rows, err := tx.Query(ctx, facSelect+where+" ORDER BY f.name", args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanFacility(rows)
			if err != nil {
				return err
			}
			out = append(out, *f)
		}
		for i := range out {
			s.resolveApproval(ctx, tx, &out[i])
		}
		return rows.Err()
	})
	if out == nil {
		out = []Facility{}
	}
	return out, err
}

func (s *Service) getFacilityTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Facility, error) {
	f, err := scanFacility(tx.QueryRow(ctx, facSelect+` WHERE f.id = $1 AND f.deleted_at IS NULL`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Facility")
		}
		return nil, err
	}
	s.resolveApproval(ctx, tx, f)
	return f, nil
}

func (s *Service) GetFacility(ctx context.Context, id uuid.UUID) (*Facility, error) {
	var out *Facility
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFacilityTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "booking.facilities.view", f.PropertyID); err != nil {
			return err
		}
		out = f
		return nil
	})
	return out, err
}

func (s *Service) CreateFacility(ctx context.Context, in FacilityInput) (*Facility, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil {
		return nil, apperr.Validation("property_id wajib")
	}
	if err := iam.CanOnProperty(ctx, "booking.facilities.create", *in.PropertyID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(deref(in.Name))
	if name == "" {
		return nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	ft := deref(in.FacilityType)
	if ft == "" {
		ft = "other"
	}
	if !facilityTypes[ft] {
		return nil, apperr.Validation("facility_type tidak valid")
	}
	open, closeT := "08:00", "21:00"
	if in.OpenTime != nil {
		open = *in.OpenTime
	}
	if in.CloseTime != nil {
		closeT = *in.CloseTime
	}
	if _, err := parseHHMM(open); err != nil {
		return nil, err
	}
	if _, err := parseHHMM(closeT); err != nil {
		return nil, err
	}
	slot, minD, maxD, adv := 60, 60, 240, 30
	if in.SlotMinutes != nil {
		slot = *in.SlotMinutes
	}
	if in.MinDurationMinutes != nil {
		minD = *in.MinDurationMinutes
	}
	if in.MaxDurationMinutes != nil {
		maxD = *in.MaxDurationMinutes
	}
	if in.AdvanceBookingDays != nil {
		adv = *in.AdvanceBookingDays
	}
	if slot < 15 || minD < slot || maxD < minD || adv < 0 {
		return nil, apperr.Validation("slot/min/max/advance tidak konsisten")
	}
	wd := []int{0, 1, 2, 3, 4, 5, 6}
	if in.Weekdays != nil {
		wd = *in.Weekdays
	}
	var out *Facility
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.Profile.RequireCapabilityTx(ctx, tx, *in.PropertyID, profile.CapFacilityBooking); err != nil {
			return err
		}
		if in.LocationID != nil {
			pid, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
			if err != nil || pid != *in.PropertyID {
				return apperr.Validation("location_id tidak berada di property ini")
			}
		}
		code, err := ids.NextPlain(ctx, tx, p.OrganizationID, ids.PrefixFacility)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO facilities (organization_id, property_id, facility_code, name, description, facility_type, location_id, capacity, requires_approval, slot_minutes, min_duration_minutes, max_duration_minutes, advance_booking_days, open_time, close_time, weekdays, rules, image_attachment_id, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::time,$15::time,$16,$17,$18,$19,$19) RETURNING id`,
			p.OrganizationID, *in.PropertyID, code, name, in.Description, ft, in.LocationID, in.Capacity, in.RequiresApproval, slot, minD, maxD, adv, open, closeT, wd, in.Rules, in.ImageAttachmentID, p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "facility", EntityID: &id, EntityLabel: code + " " + name})
		out, err = s.getFacilityTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) UpdateFacility(ctx context.Context, id uuid.UUID, in FacilityInput, ifVersion *int) (*Facility, error) {
	p := authctx.Must(ctx)
	var out *Facility
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFacilityTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "booking.facilities.update", f.PropertyID); err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != f.Version {
			return apperr.StaleVersion()
		}
		if in.FacilityType != nil && !facilityTypes[*in.FacilityType] {
			return apperr.Validation("facility_type tidak valid")
		}
		for _, t := range []*string{in.OpenTime, in.CloseTime} {
			if t != nil {
				if _, err := parseHHMM(*t); err != nil {
					return err
				}
			}
		}
		var wd *[]int = in.Weekdays
		var wdArg any
		if wd != nil {
			wdArg = *wd
		}
		if _, err := tx.Exec(ctx, `UPDATE facilities SET name = COALESCE(NULLIF(TRIM($2),''), name), description = COALESCE($3, description), facility_type = COALESCE($4, facility_type), location_id = COALESCE($5, location_id),
			capacity = COALESCE($6, capacity), requires_approval = CASE WHEN $7::bool IS NULL THEN requires_approval ELSE $7 END, slot_minutes = COALESCE($8, slot_minutes), min_duration_minutes = COALESCE($9, min_duration_minutes),
			max_duration_minutes = COALESCE($10, max_duration_minutes), advance_booking_days = COALESCE($11, advance_booking_days), open_time = COALESCE($12::time, open_time), close_time = COALESCE($13::time, close_time),
			weekdays = COALESCE($14, weekdays), rules = COALESCE($15, rules), image_attachment_id = COALESCE($16, image_attachment_id), is_active = COALESCE($17, is_active), updated_by = $18 WHERE id = $1`,
			id, deref(in.Name), in.Description, in.FacilityType, in.LocationID, in.Capacity, in.RequiresApproval, in.SlotMinutes, in.MinDurationMinutes, in.MaxDurationMinutes, in.AdvanceBookingDays, in.OpenTime, in.CloseTime, wdArg, in.Rules, in.ImageAttachmentID, in.IsActive, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "facility", EntityID: &id, EntityLabel: f.FacilityCode, After: in})
		out, err = s.getFacilityTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) DeleteFacility(ctx context.Context, id uuid.UUID) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFacilityTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "booking.facilities.delete", f.PropertyID); err != nil {
			return err
		}
		if f.UpcomingBookings > 0 {
			return apperr.Conflict("HAS_BOOKINGS", fmt.Sprintf("%d booking mendatang masih aktif", f.UpcomingBookings))
		}
		_, err = tx.Exec(ctx, `UPDATE facilities SET deleted_at = now(), is_active = false, updated_by = $2 WHERE id = $1`, id, p.UserID)
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "facility", EntityID: &id, EntityLabel: f.FacilityCode})
		return err
	})
}

// ---------- Schedules (closure) ----------

type Schedule struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	OpenTime  *string   `json:"open_time"`
	CloseTime *string   `json:"close_time"`
	Reason    *string   `json:"reason"`
}

type ScheduleInput struct {
	Kind      string    `json:"kind"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	OpenTime  *string   `json:"open_time"`
	CloseTime *string   `json:"close_time"`
	Reason    *string   `json:"reason"`
}

func (s *Service) ListSchedules(ctx context.Context, facilityID uuid.UUID) ([]Schedule, error) {
	out := []Schedule{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFacilityTx(ctx, tx, facilityID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "booking.facilities.view", f.PropertyID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, kind, starts_at, ends_at, to_char(open_time,'HH24:MI'), to_char(close_time,'HH24:MI'), reason FROM facility_schedules WHERE facility_id = $1 AND ends_at >= now() - interval '30 days' ORDER BY starts_at`, facilityID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sc Schedule
			if err := rows.Scan(&sc.ID, &sc.Kind, &sc.StartsAt, &sc.EndsAt, &sc.OpenTime, &sc.CloseTime, &sc.Reason); err != nil {
				return err
			}
			out = append(out, sc)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) AddSchedule(ctx context.Context, facilityID uuid.UUID, in ScheduleInput) (*Schedule, error) {
	p := authctx.Must(ctx)
	if in.Kind == "" {
		in.Kind = "closure"
	}
	if in.Kind != "closure" && in.Kind != "special_hours" {
		return nil, apperr.Validation("kind harus closure|special_hours")
	}
	if !in.EndsAt.After(in.StartsAt) {
		return nil, apperr.Validation("ends_at harus setelah starts_at")
	}
	var out Schedule
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFacilityTx(ctx, tx, facilityID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "booking.facilities.update", f.PropertyID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO facility_schedules (organization_id, facility_id, kind, starts_at, ends_at, open_time, close_time, reason, created_by) VALUES ($1,$2,$3,$4,$5,$6::time,$7::time,$8,$9) RETURNING id`,
			p.OrganizationID, facilityID, in.Kind, in.StartsAt, in.EndsAt, in.OpenTime, in.CloseTime, in.Reason, p.UserID).Scan(&out.ID); err != nil {
			return err
		}
		out.Kind, out.StartsAt, out.EndsAt, out.OpenTime, out.CloseTime, out.Reason = in.Kind, in.StartsAt, in.EndsAt, in.OpenTime, in.CloseTime, in.Reason
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "facility_schedule", EntityID: &out.ID, EntityLabel: f.FacilityCode, After: in})
		return nil
	})
	return &out, err
}

func (s *Service) DeleteSchedule(ctx context.Context, facilityID, scheduleID uuid.UUID) error {
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFacilityTx(ctx, tx, facilityID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "booking.facilities.update", f.PropertyID); err != nil {
			return err
		}
		ct, err := tx.Exec(ctx, `DELETE FROM facility_schedules WHERE id = $1 AND facility_id = $2`, scheduleID, facilityID)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return apperr.NotFound("Schedule")
		}
		return nil
	})
}

// ---------- Availability ----------

type Slot struct {
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	Available bool      `json:"available"`
	Reason    string    `json:"reason,omitempty"` // booked | closed | past
}

// AvailabilityTx: slot per hari (timezone property) — dipakai staf & tenant.
func (s *Service) AvailabilityTx(ctx context.Context, tx pgx.Tx, f *Facility, date time.Time) ([]Slot, error) {
	loc := property.PropertyTimezone(ctx, tx, f.PropertyID)
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	openD, _ := parseHHMM(f.OpenTime)
	closeD, _ := parseHHMM(f.CloseTime)
	out := []Slot{}
	weekdayOK := false
	for _, w := range f.Weekdays {
		if w == int(day.Weekday()) {
			weekdayOK = true
		}
	}
	// booking aktif hari itu
	type rng struct{ s, e time.Time }
	var busy, closed []rng
	rows, err := tx.Query(ctx, `SELECT starts_at, ends_at FROM bookings WHERE facility_id = $1 AND status IN ('pending','confirmed','checked_in') AND starts_at < $3 AND ends_at > $2`, f.ID, day, day.Add(24*time.Hour))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r rng
		if err := rows.Scan(&r.s, &r.e); err != nil {
			rows.Close()
			return nil, err
		}
		busy = append(busy, r)
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT starts_at, ends_at FROM facility_schedules WHERE facility_id = $1 AND kind = 'closure' AND starts_at < $3 AND ends_at > $2`, f.ID, day, day.Add(24*time.Hour))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r rng
		if err := rows.Scan(&r.s, &r.e); err != nil {
			rows.Close()
			return nil, err
		}
		closed = append(closed, r)
	}
	rows.Close()
	now := time.Now()
	closeAt := day.Add(closeD)
	for t := day.Add(openD); !t.Add(time.Duration(f.SlotMinutes) * time.Minute).After(closeAt); t = t.Add(time.Duration(f.SlotMinutes) * time.Minute) {
		sl := Slot{StartsAt: t, EndsAt: t.Add(time.Duration(f.SlotMinutes) * time.Minute), Available: true}
		switch {
		case !weekdayOK:
			sl.Available, sl.Reason = false, "closed"
		case sl.EndsAt.Before(now):
			sl.Available, sl.Reason = false, "past"
		}
		if sl.Available {
			for _, r := range closed {
				if r.s.Before(sl.EndsAt) && r.e.After(sl.StartsAt) {
					sl.Available, sl.Reason = false, "closed"
				}
			}
		}
		if sl.Available {
			for _, r := range busy {
				if r.s.Before(sl.EndsAt) && r.e.After(sl.StartsAt) {
					sl.Available, sl.Reason = false, "booked"
				}
			}
		}
		out = append(out, sl)
	}
	return out, nil
}

func (s *Service) Availability(ctx context.Context, facilityID uuid.UUID, date time.Time) ([]Slot, error) {
	var out []Slot
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFacilityTx(ctx, tx, facilityID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "booking.bookings.view", f.PropertyID); err != nil {
			return err
		}
		out, err = s.AvailabilityTx(ctx, tx, f, date)
		return err
	})
	return out, err
}

// ---------- Booking ----------

type Booking struct {
	ID              uuid.UUID  `json:"id"`
	BookingNumber   string     `json:"booking_number"`
	PropertyID      uuid.UUID  `json:"property_id"`
	FacilityID      uuid.UUID  `json:"facility_id"`
	FacilityName    string     `json:"facility_name"`
	FacilityType    string     `json:"facility_type"`
	TenantUserID    *uuid.UUID `json:"tenant_user_id"`
	TenantID        *uuid.UUID `json:"tenant_id"`
	TenantName      *string    `json:"tenant_name"`
	RequesterName   *string    `json:"requester_name"`
	RequesterPhone  *string    `json:"requester_phone"`
	StartsAt        time.Time  `json:"starts_at"`
	EndsAt          time.Time  `json:"ends_at"`
	Attendees       *int       `json:"attendees"`
	Purpose         *string    `json:"purpose"`
	Notes           *string    `json:"notes"`
	Status          string     `json:"status"`
	Channel         string     `json:"channel"`
	ApprovedAt      *time.Time `json:"approved_at"`
	ApprovedByName  *string    `json:"approved_by_name"`
	RejectionReason *string    `json:"rejection_reason"`
	CancelledAt     *time.Time `json:"cancelled_at"`
	CancelReason    *string    `json:"cancel_reason"`
	CheckedInAt     *time.Time `json:"checked_in_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	CreatedByName   *string    `json:"created_by_name"`
	AllowedActions  []string   `json:"allowed_actions"`
	Version         int        `json:"version"`
}

const bkSelect = `SELECT b.id, b.booking_number, b.property_id, b.facility_id, f.name, f.facility_type, b.tenant_user_id, b.tenant_id, t.name, b.requester_name, b.requester_phone, b.starts_at, b.ends_at, b.attendees, b.purpose, b.notes,
	b.status, b.channel, b.approved_at, ab.full_name, b.rejection_reason, b.cancelled_at, b.cancel_reason, b.checked_in_at, b.completed_at, b.created_at, cb.full_name, b.version
	FROM bookings b JOIN facilities f ON f.id = b.facility_id LEFT JOIN tenants t ON t.id = b.tenant_id LEFT JOIN users ab ON ab.id = b.approved_by LEFT JOIN users cb ON cb.id = b.created_by`

func scanBooking(row pgx.Row) (*Booking, error) {
	var b Booking
	if err := row.Scan(&b.ID, &b.BookingNumber, &b.PropertyID, &b.FacilityID, &b.FacilityName, &b.FacilityType, &b.TenantUserID, &b.TenantID, &b.TenantName, &b.RequesterName, &b.RequesterPhone, &b.StartsAt, &b.EndsAt, &b.Attendees, &b.Purpose, &b.Notes,
		&b.Status, &b.Channel, &b.ApprovedAt, &b.ApprovedByName, &b.RejectionReason, &b.CancelledAt, &b.CancelReason, &b.CheckedInAt, &b.CompletedAt, &b.CreatedAt, &b.CreatedByName, &b.Version); err != nil {
		return nil, err
	}
	return &b, nil
}

// staffActions: aksi staf berdasarkan status & permission.
func (s *Service) staffActions(ctx context.Context, b *Booking) {
	p := authctx.Must(ctx)
	can := func(perm string) bool { return p.HasOnProperty(perm, b.PropertyID) }
	b.AllowedActions = []string{"view"}
	switch b.Status {
	case "pending":
		if can("booking.bookings.approve") {
			b.AllowedActions = append(b.AllowedActions, "approve")
		}
		if can("booking.bookings.reject") {
			b.AllowedActions = append(b.AllowedActions, "reject")
		}
		if can("booking.bookings.cancel") {
			b.AllowedActions = append(b.AllowedActions, "cancel")
		}
	case "confirmed":
		if can("booking.bookings.check_in") {
			b.AllowedActions = append(b.AllowedActions, "check_in", "no_show")
		}
		if can("booking.bookings.cancel") {
			b.AllowedActions = append(b.AllowedActions, "cancel")
		}
	case "checked_in":
		if can("booking.bookings.check_in") {
			b.AllowedActions = append(b.AllowedActions, "complete")
		}
	}
}

// tenantActions: tenant hanya dapat membatalkan booking miliknya sebelum mulai (PRD §21 "cancel sesuai policy").
func tenantActions(b *Booking) {
	b.AllowedActions = []string{"view"}
	if (b.Status == "pending" || b.Status == "confirmed") && b.StartsAt.After(time.Now()) {
		b.AllowedActions = append(b.AllowedActions, "cancel")
	}
}

type CreateBookingInput struct {
	PropertyID     *uuid.UUID `json:"property_id"`
	FacilityID     uuid.UUID  `json:"facility_id"`
	StartsAt       time.Time  `json:"starts_at"`
	EndsAt         time.Time  `json:"ends_at"`
	Attendees      *int       `json:"attendees"`
	Purpose        *string    `json:"purpose"`
	Notes          *string    `json:"notes"`
	TenantID       *uuid.UUID `json:"tenant_id"`
	RequesterName  *string    `json:"requester_name"`
	RequesterPhone *string    `json:"requester_phone"`
}

// createTx: validasi aturan fasilitas lalu INSERT; konflik → 409 BOOKING_CONFLICT (EXCLUDE constraint).
func (s *Service) createTx(ctx context.Context, tx pgx.Tx, in CreateBookingInput, sc *tenantscope.Scope) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	f, err := s.getFacilityTx(ctx, tx, in.FacilityID)
	if err != nil {
		return uuid.Nil, err
	}
	if !f.IsActive {
		return uuid.Nil, apperr.Validation("Fasilitas tidak aktif")
	}
	if sc != nil && f.PropertyID != sc.PropertyID {
		return uuid.Nil, apperr.NotFound("Facility") // tidak bocor lintas property
	}
	if err := s.Profile.RequireCapabilityTx(ctx, tx, f.PropertyID, profile.CapFacilityBooking); err != nil {
		return uuid.Nil, err
	}
	if !in.EndsAt.After(in.StartsAt) {
		return uuid.Nil, apperr.Validation("ends_at harus setelah starts_at")
	}
	dur := in.EndsAt.Sub(in.StartsAt)
	if dur < time.Duration(f.MinDurationMinutes)*time.Minute || dur > time.Duration(f.MaxDurationMinutes)*time.Minute {
		return uuid.Nil, apperr.Validation(fmt.Sprintf("Durasi harus %d–%d menit", f.MinDurationMinutes, f.MaxDurationMinutes))
	}
	if in.StartsAt.Before(time.Now().Add(-5 * time.Minute)) {
		return uuid.Nil, apperr.Validation("Waktu mulai sudah lewat")
	}
	if in.StartsAt.After(time.Now().Add(time.Duration(f.AdvanceBookingDays) * 24 * time.Hour)) {
		return uuid.Nil, apperr.Validation(fmt.Sprintf("Booking maksimal %d hari ke depan", f.AdvanceBookingDays))
	}
	loc := property.PropertyTimezone(ctx, tx, f.PropertyID)
	ls, le := in.StartsAt.In(loc), in.EndsAt.In(loc)
	openD, _ := parseHHMM(f.OpenTime)
	closeD, _ := parseHHMM(f.CloseTime)
	dayStart := time.Date(ls.Year(), ls.Month(), ls.Day(), 0, 0, 0, 0, loc)
	if ls.Before(dayStart.Add(openD)) || le.After(dayStart.Add(closeD)) {
		return uuid.Nil, apperr.Validation(fmt.Sprintf("Di luar jam operasional fasilitas (%s–%s)", f.OpenTime, f.CloseTime))
	}
	okDay := false
	for _, w := range f.Weekdays {
		if w == int(ls.Weekday()) {
			okDay = true
		}
	}
	if !okDay {
		return uuid.Nil, apperr.Validation("Fasilitas tidak beroperasi pada hari tersebut")
	}
	if f.Capacity != nil && in.Attendees != nil && *in.Attendees > *f.Capacity {
		return uuid.Nil, apperr.Validation(fmt.Sprintf("Kapasitas maksimal %d orang", *f.Capacity))
	}
	var closedN int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM facility_schedules WHERE facility_id = $1 AND kind = 'closure' AND starts_at < $3 AND ends_at > $2`, f.ID, in.StartsAt, in.EndsAt).Scan(&closedN)
	if closedN > 0 {
		return uuid.Nil, apperr.Conflict("FACILITY_CLOSED", "Fasilitas ditutup pada waktu tersebut")
	}
	status := "confirmed"
	if f.EffectiveApproval {
		status = "pending"
	}
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixBooking, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	channel := "staff"
	var tenantUser, tenantID, occupantID *uuid.UUID
	reqName, reqPhone := in.RequesterName, in.RequesterPhone
	if sc != nil {
		channel = "tenant_app"
		tenantUser, tenantID, occupantID = &sc.UserID, sc.TenantID, sc.OccupantID
		_ = tx.QueryRow(ctx, `SELECT full_name, phone FROM users WHERE id = $1`, sc.UserID).Scan(&reqName, &reqPhone)
	} else {
		tenantID = in.TenantID
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO bookings (organization_id, property_id, booking_number, facility_id, tenant_user_id, tenant_id, occupant_id, requester_name, requester_phone, starts_at, ends_at, attendees, purpose, notes, status, channel, created_by, updated_by, approved_at, approved_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17, CASE WHEN $15::text = 'confirmed' THEN now() END, CASE WHEN $15::text = 'confirmed' AND $16::text = 'staff' THEN $17::uuid END) RETURNING id`,
		p.OrganizationID, f.PropertyID, number, f.ID, tenantUser, tenantID, occupantID, reqName, reqPhone, in.StartsAt, in.EndsAt, in.Attendees, in.Purpose, in.Notes, status, channel, p.UserID).Scan(&id)
	if err != nil {
		if db.IsExclusionViolation(err) {
			return uuid.Nil, apperr.Conflict("BOOKING_CONFLICT", "Slot waktu sudah dipesan")
		}
		return uuid.Nil, err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "booking", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "facility": f.Name, "status": status, "channel": channel}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "booking", EntityID: &id, EntityLabel: number})
	if s.Jobs != nil {
		payload := map[string]any{"status": status, "facility": f.Name, "domain": "tenant_relation", "channel": channel}
		if tenantUser != nil {
			payload["tenant_user_id"] = *tenantUser
		}
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventBookingCreated, OrganizationID: p.OrganizationID, PropertyID: &f.PropertyID, ObjectType: "booking", ObjectID: id, ObjectLabel: number + " · " + f.Name, ActorUserID: &p.UserID, Payload: payload})
		if status == "confirmed" {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventBookingConfirmed, OrganizationID: p.OrganizationID, PropertyID: &f.PropertyID, ObjectType: "booking", ObjectID: id, ObjectLabel: number + " · " + f.Name, ActorUserID: &p.UserID, Payload: payload})
		}
	}
	return id, nil
}

func (s *Service) getTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Booking, error) {
	b, err := scanBooking(tx.QueryRow(ctx, bkSelect+` WHERE b.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Booking")
		}
		return nil, err
	}
	return b, nil
}

// ---- staff API ----

type Filter struct {
	PropertyID *uuid.UUID
	FacilityID *uuid.UUID
	Statuses   []string
	From, To   *time.Time
	Upcoming   bool
	TenantID   *uuid.UUID
	Q          string
}

func (s *Service) List(ctx context.Context, f Filter, page httpx.Page) ([]Booking, *string, error) {
	p := authctx.Must(ctx)
	var out []Booking
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE true"
		var args []any
		if f.PropertyID != nil {
			if err := iam.CanOnProperty(ctx, "booking.bookings.view", *f.PropertyID); err != nil {
				return err
			}
			args = append(args, *f.PropertyID)
			where += fmt.Sprintf(" AND b.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("booking.bookings.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND b.property_id = ANY($%d)", len(args))
		}
		if f.FacilityID != nil {
			args = append(args, *f.FacilityID)
			where += fmt.Sprintf(" AND b.facility_id = $%d", len(args))
		}
		if f.TenantID != nil {
			args = append(args, *f.TenantID)
			where += fmt.Sprintf(" AND b.tenant_id = $%d", len(args))
		}
		if len(f.Statuses) > 0 {
			args = append(args, f.Statuses)
			where += fmt.Sprintf(" AND b.status = ANY($%d)", len(args))
		}
		if f.From != nil {
			args = append(args, *f.From)
			where += fmt.Sprintf(" AND b.ends_at >= $%d", len(args))
		}
		if f.To != nil {
			args = append(args, *f.To)
			where += fmt.Sprintf(" AND b.starts_at <= $%d", len(args))
		}
		if f.Upcoming {
			where += " AND b.ends_at >= now() AND b.status IN ('pending','confirmed','checked_in')"
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (b.booking_number ILIKE $%d OR b.requester_name ILIKE $%d OR f.name ILIKE $%d OR b.purpose ILIKE $%d)", len(args), len(args), len(args), len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (b.starts_at, b.id) > ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, bkSelect+where+fmt.Sprintf(" ORDER BY b.starts_at, b.id LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var items []Booking
		for rows.Next() {
			b, err := scanBooking(rows)
			if err != nil {
				return err
			}
			items = append(items, *b)
		}
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.StartsAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			s.staffActions(ctx, &items[i])
		}
		out = items
		return rows.Err()
	})
	if out == nil {
		out = []Booking{}
	}
	return out, next, err
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Booking, error) {
	var out *Booking
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "booking.bookings.view", b.PropertyID); err != nil {
			return err
		}
		s.staffActions(ctx, b)
		out = b
		return nil
	})
	return out, err
}

func (s *Service) Create(ctx context.Context, in CreateBookingInput) (*Booking, error) {
	var out *Booking
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFacilityTx(ctx, tx, in.FacilityID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "booking.bookings.create", f.PropertyID); err != nil {
			return err
		}
		if in.TenantID != nil {
			var tpid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT property_id FROM tenants WHERE id = $1 AND deleted_at IS NULL`, *in.TenantID).Scan(&tpid); err != nil || tpid != f.PropertyID {
				return apperr.Validation("tenant_id tidak ditemukan di property ini")
			}
		}
		id, err := s.createTx(ctx, tx, in, nil)
		if err != nil {
			return err
		}
		out, err = s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s.staffActions(ctx, out)
		return nil
	})
	return out, err
}

type ActionInput struct {
	Reason string `json:"reason"`
}

// Act: approve | reject | cancel | check_in | complete | no_show (staf).
func (s *Service) Act(ctx context.Context, id uuid.UUID, action string, in ActionInput) (*Booking, error) {
	p := authctx.Must(ctx)
	var out *Booking
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "booking.bookings.view", b.PropertyID); err != nil {
			return err
		}
		s.staffActions(ctx, b)
		if !has(b.AllowedActions, action) {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk booking berstatus %s", action, b.Status))
		}
		if err := s.applyTx(ctx, tx, b, action, strings.TrimSpace(in.Reason), "staff", p.UserID); err != nil {
			return err
		}
		out, err = s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s.staffActions(ctx, out)
		return nil
	})
	return out, err
}

func (s *Service) applyTx(ctx context.Context, tx pgx.Tx, b *Booking, action, reason, actorKind string, actor uuid.UUID) error {
	p := authctx.Must(ctx)
	var to, ev, sets string
	args := []any{b.ID, actor}
	switch action {
	case "approve":
		to, ev, sets = "confirmed", EventBookingConfirmed, "approved_at = now(), approved_by = $2"
	case "reject":
		if reason == "" {
			return apperr.Validation("reason wajib").WithField("reason", "wajib")
		}
		args = append(args, reason)
		to, ev, sets = "rejected", EventBookingRejected, "rejection_reason = $3"
	case "cancel":
		if reason == "" {
			if actorKind == "tenant" {
				reason = "Dibatalkan oleh tenant"
			} else {
				return apperr.Validation("reason wajib").WithField("reason", "wajib")
			}
		}
		args = append(args, reason)
		to, ev, sets = "cancelled", EventBookingCancelled, "cancelled_at = now(), cancelled_by = $2, cancel_reason = $3"
	case "check_in":
		to, ev, sets = "checked_in", EventBookingCheckedIn, "checked_in_at = now()"
	case "complete":
		to, ev, sets = "completed", EventBookingCompleted, "completed_at = now()"
	case "no_show":
		to, ev, sets = "no_show", EventBookingNoShow, "completed_at = now()"
	default:
		return apperr.Validation("aksi tidak dikenal")
	}
	if _, err := tx.Exec(ctx, `UPDATE bookings SET status = '`+to+`', updated_by = $2, `+sets+` WHERE id = $1`, args...); err != nil {
		return err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "booking", ObjectID: b.ID, Action: audit.ActStatusChanged, From: b.Status, To: to, Payload: map[string]any{"action": action, "reason": reason, "actor_kind": actorKind}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "booking", EntityID: &b.ID, EntityLabel: b.BookingNumber, Before: map[string]any{"status": b.Status}, After: map[string]any{"status": to, "reason": reason}})
	if s.Jobs != nil {
		payload := map[string]any{"from": b.Status, "to": to, "reason": reason, "facility": b.FacilityName, "domain": "tenant_relation", "actor_kind": actorKind}
		if b.TenantUserID != nil {
			payload["tenant_user_id"] = *b.TenantUserID
		}
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: ev, OrganizationID: p.OrganizationID, PropertyID: &b.PropertyID, ObjectType: "booking", ObjectID: b.ID, ObjectLabel: b.BookingNumber + " · " + b.FacilityName, ActorUserID: &p.UserID, Payload: payload})
	}
	return nil
}

// ---- tenant API (Mobile Tenant) ----

func (s *Service) TenantFacilities(ctx context.Context) ([]Facility, error) {
	var out []Facility
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		if err := s.Profile.RequireCapabilityTx(ctx, tx, sc.PropertyID, profile.CapFacilityBooking); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, facSelect+` WHERE f.property_id = $1 AND f.is_active AND f.deleted_at IS NULL ORDER BY f.name`, sc.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanFacility(rows)
			if err != nil {
				return err
			}
			out = append(out, *f)
		}
		for i := range out {
			s.resolveApproval(ctx, tx, &out[i])
		}
		return rows.Err()
	})
	if out == nil {
		out = []Facility{}
	}
	return out, err
}

func (s *Service) TenantAvailability(ctx context.Context, facilityID uuid.UUID, date time.Time) ([]Slot, error) {
	var out []Slot
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		f, err := s.getFacilityTx(ctx, tx, facilityID)
		if err != nil || f.PropertyID != sc.PropertyID {
			return apperr.NotFound("Facility")
		}
		out, err = s.AvailabilityTx(ctx, tx, f, date)
		return err
	})
	return out, err
}

func (s *Service) TenantCreate(ctx context.Context, in CreateBookingInput) (*Booking, error) {
	var out *Booking
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		in.TenantID, in.RequesterName, in.RequesterPhone = nil, nil, nil
		id, err := s.createTx(ctx, tx, in, sc)
		if err != nil {
			return err
		}
		out, err = s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		tenantActions(out)
		return nil
	})
	return out, err
}

func tenantOwnership(sc *tenantscope.Scope, argIdx int) (string, []any) {
	if sc.Role == "tenant_admin" && sc.TenantID != nil {
		return fmt.Sprintf("(b.tenant_user_id = $%d OR b.tenant_id = $%d)", argIdx, argIdx+1), []any{sc.UserID, *sc.TenantID}
	}
	return fmt.Sprintf("b.tenant_user_id = $%d", argIdx), []any{sc.UserID}
}

func (s *Service) TenantList(ctx context.Context, upcoming *bool, page httpx.Page) ([]Booking, *string, error) {
	var out []Booking
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		where, args := tenantOwnership(sc, 1)
		where = " WHERE " + where
		order := " ORDER BY b.starts_at DESC, b.id DESC"
		cmp := "<"
		if upcoming != nil && *upcoming {
			where += " AND b.ends_at >= now() AND b.status IN ('pending','confirmed','checked_in')"
			order = " ORDER BY b.starts_at, b.id"
			cmp = ">"
		} else if upcoming != nil {
			where += " AND (b.ends_at < now() OR b.status IN ('cancelled','rejected','completed','no_show'))"
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (b.starts_at, b.id) %s ($%d::timestamptz, $%d)", cmp, len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, bkSelect+where+order+fmt.Sprintf(" LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var items []Booking
		for rows.Next() {
			b, err := scanBooking(rows)
			if err != nil {
				return err
			}
			tenantActions(b)
			items = append(items, *b)
		}
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.StartsAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			items = items[:page.Limit]
		}
		out = items
		return rows.Err()
	})
	if out == nil {
		out = []Booking{}
	}
	return out, next, err
}

func (s *Service) tenantGetTx(ctx context.Context, tx pgx.Tx, sc *tenantscope.Scope, id uuid.UUID) (*Booking, error) {
	where, args := tenantOwnership(sc, 2)
	b, err := scanBooking(tx.QueryRow(ctx, bkSelect+` WHERE b.id = $1 AND `+where, append([]any{id}, args...)...))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Booking")
		}
		return nil, err
	}
	tenantActions(b)
	return b, nil
}

func (s *Service) TenantGet(ctx context.Context, id uuid.UUID) (*Booking, error) {
	var out *Booking
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		out, err = s.tenantGetTx(ctx, tx, sc, id)
		return err
	})
	return out, err
}

func (s *Service) TenantCancel(ctx context.Context, id uuid.UUID, in ActionInput) (*Booking, error) {
	var out *Booking
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		b, err := s.tenantGetTx(ctx, tx, sc, id)
		if err != nil {
			return err
		}
		if !has(b.AllowedActions, "cancel") {
			return apperr.InvalidTransition("Booking tidak dapat dibatalkan")
		}
		if err := s.applyTx(ctx, tx, b, "cancel", strings.TrimSpace(in.Reason), "tenant", sc.UserID); err != nil {
			return err
		}
		out, err = s.tenantGetTx(ctx, tx, sc, id)
		return err
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
