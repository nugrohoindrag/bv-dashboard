package workforce

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
)

// ---------- Attendance / On-duty (PRD P2 v2.1 §8.2 P2-DTY-01..02; P2-MOB-04) ----------
// Clock-in / clock-out dari Staff App (GPS opsional, offline lewat sync). Satu sesi on-duty terbuka per user.

type Attendance struct {
	ID                uuid.UUID  `json:"id"`
	PropertyID        uuid.UUID  `json:"property_id"`
	Domain            string     `json:"domain"`
	UserID            uuid.UUID  `json:"user_id"`
	UserName          string     `json:"user_name"`
	ShiftAssignmentID *uuid.UUID `json:"shift_assignment_id"`
	ShiftName         *string    `json:"shift_name"`
	ShiftStartsAt     *time.Time `json:"shift_starts_at"`
	ShiftEndsAt       *time.Time `json:"shift_ends_at"`
	ClockInAt         time.Time  `json:"clock_in_at"`
	ClockInGPSStatus  *string    `json:"clock_in_gps_status"`
	ClockInSource     string     `json:"clock_in_source"`
	ClockOutAt        *time.Time `json:"clock_out_at"`
	ClockOutSource    *string    `json:"clock_out_source"`
	Status            string     `json:"status"` // on_duty | completed | auto_closed
	LateMinutes       int        `json:"late_minutes"`
	WorkedMinutes     int        `json:"worked_minutes"`
	Note              *string    `json:"note"`
}

const attendanceSelect = `SELECT a.id, a.property_id, a.domain, a.user_id, u.full_name, a.shift_assignment_id, sd.name, sa.starts_at, sa.ends_at, a.clock_in_at, a.clock_in_gps_status, a.clock_in_source,
	a.clock_out_at, a.clock_out_source, a.status, a.late_minutes, a.note
	FROM attendance_records a JOIN users u ON u.id = a.user_id LEFT JOIN shift_assignments sa ON sa.id = a.shift_assignment_id LEFT JOIN shift_definitions sd ON sd.id = sa.shift_id`

func scanAttendance(row pgx.Row) (*Attendance, error) {
	var a Attendance
	if err := row.Scan(&a.ID, &a.PropertyID, &a.Domain, &a.UserID, &a.UserName, &a.ShiftAssignmentID, &a.ShiftName, &a.ShiftStartsAt, &a.ShiftEndsAt, &a.ClockInAt, &a.ClockInGPSStatus, &a.ClockInSource,
		&a.ClockOutAt, &a.ClockOutSource, &a.Status, &a.LateMinutes, &a.Note); err != nil {
		return nil, err
	}
	end := time.Now()
	if a.ClockOutAt != nil {
		end = *a.ClockOutAt
	}
	a.WorkedMinutes = int(end.Sub(a.ClockInAt).Minutes())
	return &a, nil
}

// ClockInput: clock-in / clock-out (Staff App; offline lewat mutation clock_in / clock_out).
type ClockInput struct {
	ID                *uuid.UUID `json:"id"` // id klien (idempoten untuk sync)
	PropertyID        *uuid.UUID `json:"property_id"`
	Domain            string     `json:"domain"`
	ShiftAssignmentID *uuid.UUID `json:"shift_assignment_id"`
	GPSLat            *float64   `json:"gps_lat"`
	GPSLng            *float64   `json:"gps_lng"`
	GPSStatus         string     `json:"gps_status"`
	Note              *string    `json:"note"`
	ClientTime        *time.Time `json:"client_time"`
	FromSync          bool       `json:"-"`
}

func gpsStatus(in ClockInput) string {
	switch in.GPSStatus {
	case "captured", "unavailable", "denied":
		if in.GPSStatus == "captured" && (in.GPSLat == nil || in.GPSLng == nil) {
			return "unavailable"
		}
		return in.GPSStatus
	}
	if in.GPSLat != nil && in.GPSLng != nil {
		return "captured"
	}
	return "unavailable"
}

// userDomain: domain team staf (security/housekeeping/engineering) — dipakai bila clock-in tanpa roster.
func userDomain(ctx context.Context, tx pgx.Tx, userID uuid.UUID) string {
	var d string
	_ = tx.QueryRow(ctx, `SELECT t.domain FROM team_members tm JOIN teams t ON t.id = tm.team_id WHERE tm.user_id = $1 AND t.is_active AND t.domain IN ('security','housekeeping','engineering') ORDER BY t.property_id NULLS LAST LIMIT 1`, userID).Scan(&d)
	if d == "" {
		return "general"
	}
	return d
}

func (s *Service) ClockIn(ctx context.Context, in ClockInput) (*Attendance, error) {
	var out *Attendance
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.ClockInTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = scanAttendance(tx.QueryRow(ctx, attendanceSelect+` WHERE a.id = $1`, id))
		return err
	})
	return out, err
}

// ClockInTx: roster aktif (±2 jam sebelum mulai s/d akhir shift) dipakai otomatis; tanpa roster → domain team & property
// eksplisit/tunggal. Terlambat dihitung dari awal shift.
func (s *Service) ClockInTx(ctx context.Context, tx pgx.Tx, in ClockInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if in.ID != nil {
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attendance_records WHERE id = $1)`, *in.ID).Scan(&exists)
		if exists {
			return *in.ID, nil
		}
	}
	var openID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM attendance_records WHERE user_id = $1 AND clock_out_at IS NULL`, p.UserID).Scan(&openID); err == nil {
		return uuid.Nil, apperr.Conflict("ALREADY_ON_DUTY", "Anda sudah clock-in; clock-out dahulu sebelum shift berikutnya")
	}
	now := time.Now().UTC()
	at := now
	if in.FromSync && in.ClientTime != nil && in.ClientTime.Before(now) && now.Sub(*in.ClientTime) < 24*time.Hour {
		at = in.ClientTime.UTC() // waktu nyata di perangkat saat offline
	}
	var saID *uuid.UUID
	var pid uuid.UUID
	domain := in.Domain
	var startsAt *time.Time
	if in.ShiftAssignmentID != nil {
		var uid uuid.UUID
		var st time.Time
		if err := tx.QueryRow(ctx, `SELECT user_id, property_id, domain, starts_at FROM shift_assignments WHERE id = $1 AND status = 'scheduled'`, *in.ShiftAssignmentID).Scan(&uid, &pid, &domain, &st); err != nil || uid != p.UserID {
			return uuid.Nil, apperr.Validation("shift_assignment_id bukan jadwal Anda").WithField("shift_assignment_id", "tidak valid")
		}
		saID, startsAt = in.ShiftAssignmentID, &st
	} else {
		var id uuid.UUID
		var st time.Time
		err := tx.QueryRow(ctx, `SELECT id, property_id, domain, starts_at FROM shift_assignments WHERE user_id = $1 AND status = 'scheduled' AND $2 BETWEEN starts_at - interval '2 hours' AND ends_at
			ORDER BY abs(EXTRACT(EPOCH FROM (starts_at - $2))) LIMIT 1`, p.UserID, at).Scan(&id, &pid, &domain, &st)
		if err == nil {
			saID, startsAt = &id, &st
		} else {
			switch {
			case in.PropertyID != nil:
				pid = *in.PropertyID
			default:
				pids, all := p.PropertyIDsFor("workforce.attendance.clock")
				if all || len(pids) != 1 {
					return uuid.Nil, apperr.Validation("property_id wajib (tidak ada jadwal shift aktif)").WithField("property_id", "wajib")
				}
				pid = pids[0]
			}
			if domain == "" {
				domain = userDomain(ctx, tx, p.UserID)
			}
		}
	}
	switch domain {
	case "security", "housekeeping", "engineering", "general":
	default:
		return uuid.Nil, apperr.Validation("domain harus security|housekeeping|engineering|general").WithField("domain", "tidak valid")
	}
	if !p.HasAnyOnProperty("workforce.attendance.clock", pid) {
		return uuid.Nil, apperr.Forbidden("Memerlukan workforce.attendance.clock pada property ini")
	}
	late := 0
	if startsAt != nil && at.After(startsAt.Add(5*time.Minute)) {
		late = int(at.Sub(*startsAt).Minutes())
	}
	src := "mobile"
	if in.FromSync {
		src = "sync"
	} else if p.Source == authctx.SourceWeb {
		src = "web"
	}
	id := uuid.Must(uuid.NewV7())
	if in.ID != nil {
		id = *in.ID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO attendance_records (id, organization_id, property_id, domain, user_id, shift_assignment_id, clock_in_at, clock_in_lat, clock_in_lng, clock_in_gps_status, clock_in_source, late_minutes, note)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, id, p.OrganizationID, pid, domain, p.UserID, saID, at, in.GPSLat, in.GPSLng, gpsStatus(in), src, late, in.Note); err != nil {
		if db.IsUniqueViolation(err) {
			return uuid.Nil, apperr.Conflict("ALREADY_ON_DUTY", "Anda sudah clock-in")
		}
		return uuid.Nil, err
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: "clock_in", EntityType: "attendance", EntityID: &id, EntityLabel: domain, After: map[string]any{"late_minutes": late, "gps_status": gpsStatus(in), "source": src}})
	if s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "attendance.clocked_in", OrganizationID: p.OrganizationID, PropertyID: &pid, ObjectType: "attendance", ObjectID: id, ObjectLabel: p.FullName, ActorUserID: actorOrNil(p), Payload: map[string]any{"domain": domain, "late_minutes": late}})
	}
	return id, nil
}

func (s *Service) ClockOut(ctx context.Context, in ClockInput) (*Attendance, error) {
	var out *Attendance
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.ClockOutTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = scanAttendance(tx.QueryRow(ctx, attendanceSelect+` WHERE a.id = $1`, id))
		return err
	})
	return out, err
}

func (s *Service) ClockOutTx(ctx context.Context, tx pgx.Tx, in ClockInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	var id uuid.UUID
	var clockIn time.Time
	if err := tx.QueryRow(ctx, `SELECT id, clock_in_at FROM attendance_records WHERE user_id = $1 AND clock_out_at IS NULL FOR UPDATE`, p.UserID).Scan(&id, &clockIn); err != nil {
		if in.FromSync && in.ID != nil {
			// idempoten: sesi yang sama sudah ditutup (mutation diulang)
			var closed bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attendance_records WHERE id = $1 AND clock_out_at IS NOT NULL)`, *in.ID).Scan(&closed)
			if closed {
				return *in.ID, nil
			}
		}
		return uuid.Nil, apperr.Conflict("NOT_ON_DUTY", "Anda belum clock-in")
	}
	at := time.Now().UTC()
	if in.FromSync && in.ClientTime != nil && in.ClientTime.After(clockIn) && in.ClientTime.Before(at) {
		at = in.ClientTime.UTC()
	}
	src := "mobile"
	if in.FromSync {
		src = "sync"
	} else if p.Source == authctx.SourceWeb {
		src = "web"
	}
	if _, err := tx.Exec(ctx, `UPDATE attendance_records SET clock_out_at = $2, clock_out_lat = $3, clock_out_lng = $4, clock_out_gps_status = $5, clock_out_source = $6, status = 'completed', note = COALESCE($7, note) WHERE id = $1`,
		id, at, in.GPSLat, in.GPSLng, gpsStatus(in), src, in.Note); err != nil {
		return uuid.Nil, err
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: "clock_out", EntityType: "attendance", EntityID: &id, EntityLabel: p.FullName, After: map[string]any{"source": src}})
	return id, nil
}

// MyAttendance: sesi on-duty aktif + riwayat 14 hari.
type MyAttendance struct {
	Current *Attendance  `json:"current"`
	Recent  []Attendance `json:"recent"`
	Next    *RosterEntry `json:"next_shift"`
}

func (s *Service) MyAttendance(ctx context.Context) (*MyAttendance, error) {
	p := authctx.Must(ctx)
	out := &MyAttendance{Recent: []Attendance{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, attendanceSelect+` WHERE a.user_id = $1 AND a.clock_in_at > now() - interval '14 days' ORDER BY a.clock_in_at DESC LIMIT 30`, p.UserID)
		if err != nil {
			return err
		}
		for rows.Next() {
			a, err := scanAttendance(rows)
			if err != nil {
				rows.Close()
				return err
			}
			if a.ClockOutAt == nil && out.Current == nil {
				c := *a
				out.Current = &c
			}
			out.Recent = append(out.Recent, *a)
		}
		rows.Close()
		if r, err := scanRoster(tx.QueryRow(ctx, rosterSelect+` WHERE sa.user_id = $1 AND sa.status = 'scheduled' AND sa.ends_at > now() ORDER BY sa.starts_at LIMIT 1`, p.UserID)); err == nil {
			out.Next = r
		}
		return nil
	})
	return out, err
}

// ListAttendance: rekap kehadiran domain (supervisor) per tanggal.
func (s *Service) ListAttendance(ctx context.Context, domain string, propertyID *uuid.UUID, date string) ([]Attendance, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	out := []Attendance{}
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, apperr.Validation("date harus YYYY-MM-DD")
	}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{domain, date}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE a.domain = $1 AND (a.clock_in_at AT TIME ZONE pr.timezone)::date = $2::date"
		if propertyID != nil {
			if !p.HasAnyOnProperty(shiftPerm(domain, "view"), *propertyID) {
				return apperr.Forbidden("")
			}
			where += " AND a.property_id = " + add(*propertyID)
		}
		where += " AND " + p.ScopeSQL(shiftPerm(domain, "view"), "a.property_id", "", add)
		rows, err := tx.Query(ctx, attendanceSelect+` JOIN properties pr ON pr.location_id = a.property_id`+where+` ORDER BY a.clock_in_at`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scanAttendance(rows)
			if err != nil {
				return err
			}
			out = append(out, *a)
		}
		return rows.Err()
	})
	return out, err
}

// ---------- On-duty board (P2-DTY-02) ----------

type OnDutyTeam struct {
	TeamID    *uuid.UUID `json:"team_id"`
	TeamName  string     `json:"team_name"`
	Scheduled int        `json:"scheduled"`
	OnDuty    int        `json:"on_duty"`
	Absent    int        `json:"absent"`
}

type OnDutyShift struct {
	ShiftID   uuid.UUID `json:"shift_id"`
	Name      string    `json:"name"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	MinStaff  int       `json:"min_staff"`
	Scheduled int       `json:"scheduled"`
	OnDuty    int       `json:"on_duty"`
	Shortage  int       `json:"shortage"` // min_staff − on_duty (≥ 0)
}

type OnDutyBoard struct {
	Domain      string        `json:"domain"`
	PropertyID  *uuid.UUID    `json:"property_id"`
	GeneratedAt time.Time     `json:"generated_at"`
	Scheduled   int           `json:"scheduled"` // roster shift yang sedang berjalan
	OnDuty      int           `json:"on_duty"`   // clock-in aktif
	Absent      int           `json:"absent"`    // dijadwalkan, lewat 15 menit belum clock-in
	Shortage    int           `json:"shortage"`
	Shifts      []OnDutyShift `json:"shifts"`
	Teams       []OnDutyTeam  `json:"teams"`
	Staff       []RosterEntry `json:"staff"`       // roster shift berjalan + status kehadiran
	Unscheduled []Attendance  `json:"unscheduled"` // on-duty tanpa roster
	Link        string        `json:"link"`
}

// OnDuty: papan on-duty domain saat ini — dijadwalkan vs on-duty vs absent per shift & team.
func (s *Service) OnDuty(ctx context.Context, domain string, propertyID *uuid.UUID) (*OnDutyBoard, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	out := &OnDutyBoard{Domain: domain, PropertyID: propertyID, GeneratedAt: time.Now().UTC(), Shifts: []OnDutyShift{}, Teams: []OnDutyTeam{}, Staff: []RosterEntry{}, Unscheduled: []Attendance{},
		Link: "/" + domain + "/shifts?tab=on-duty"}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{domain}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE sa.domain = $1 AND sa.status = 'scheduled' AND now() BETWEEN sa.starts_at AND sa.ends_at"
		if propertyID != nil {
			if !p.HasAnyOnProperty(shiftPerm(domain, "view"), *propertyID) && !p.HasAnyOnProperty("overview.dashboard.view", *propertyID) {
				return apperr.Forbidden("")
			}
			where += " AND sa.property_id = " + add(*propertyID)
		} else {
			where += " AND (" + p.ScopeSQL(shiftPerm(domain, "view"), "sa.property_id", "", add) + " OR " + p.ScopeSQL("overview.dashboard.view", "sa.property_id", "", add) + ")"
		}
		rows, err := tx.Query(ctx, rosterSelect+where+" ORDER BY sa.starts_at, u.full_name", args...)
		if err != nil {
			return err
		}
		shiftIdx := map[uuid.UUID]int{}
		teamIdx := map[string]int{}
		for rows.Next() {
			r, err := scanRoster(rows)
			if err != nil {
				rows.Close()
				return err
			}
			out.Staff = append(out.Staff, *r)
		}
		rows.Close()
		for _, r := range out.Staff {
			if _, ok := shiftIdx[r.ShiftID]; !ok {
				var minStaff int
				_ = tx.QueryRow(ctx, `SELECT min_staff FROM shift_definitions WHERE id = $1`, r.ShiftID).Scan(&minStaff)
				shiftIdx[r.ShiftID] = len(out.Shifts)
				out.Shifts = append(out.Shifts, OnDutyShift{ShiftID: r.ShiftID, Name: r.ShiftName, StartsAt: r.StartsAt, EndsAt: r.EndsAt, MinStaff: minStaff})
			}
			sh := &out.Shifts[shiftIdx[r.ShiftID]]
			sh.Scheduled++
			key, name := "-", "(tanpa team)"
			if r.TeamID != nil {
				key, name = r.TeamID.String(), deref(r.TeamName)
			}
			if _, ok := teamIdx[key]; !ok {
				teamIdx[key] = len(out.Teams)
				out.Teams = append(out.Teams, OnDutyTeam{TeamID: r.TeamID, TeamName: name})
			}
			tm := &out.Teams[teamIdx[key]]
			tm.Scheduled++
			out.Scheduled++
			switch r.Attendance {
			case "on_duty", "late":
				sh.OnDuty++
				tm.OnDuty++
			case "absent":
				tm.Absent++
				out.Absent++
			}
		}
		// on-duty total = semua sesi terbuka domain (termasuk tanpa roster)
		args2 := []any{domain}
		add2 := func(v any) string { args2 = append(args2, v); return fmt.Sprintf("$%d", len(args2)) }
		w2 := " WHERE a.domain = $1 AND a.clock_out_at IS NULL"
		if propertyID != nil {
			w2 += " AND a.property_id = " + add2(*propertyID)
		} else {
			w2 += " AND (" + p.ScopeSQL(shiftPerm(domain, "view"), "a.property_id", "", add2) + " OR " + p.ScopeSQL("overview.dashboard.view", "a.property_id", "", add2) + ")"
		}
		arows, err := tx.Query(ctx, attendanceSelect+w2+" ORDER BY a.clock_in_at", args2...)
		if err != nil {
			return err
		}
		for arows.Next() {
			a, err := scanAttendance(arows)
			if err != nil {
				arows.Close()
				return err
			}
			out.OnDuty++
			if a.ShiftAssignmentID == nil {
				out.Unscheduled = append(out.Unscheduled, *a)
			}
		}
		arows.Close()
		for i := range out.Shifts {
			if d := out.Shifts[i].MinStaff - out.Shifts[i].OnDuty; d > 0 {
				out.Shifts[i].Shortage = d
				out.Shortage += d
			}
		}
		return nil
	})
	return out, err
}

// AttendanceSweep: sesi on-duty yang lupa clock-out ditutup otomatis (akhir shift + 4 jam, atau 16 jam tanpa roster).
func (s *Service) AttendanceSweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	n := 0
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE attendance_records a SET clock_out_at = COALESCE(sa.ends_at, a.clock_in_at + interval '12 hours'), clock_out_source = 'system', status = 'auto_closed'
			FROM attendance_records a2 LEFT JOIN shift_assignments sa ON sa.id = a2.shift_assignment_id
			WHERE a.id = a2.id AND a.clock_out_at IS NULL AND ((sa.id IS NOT NULL AND now() > sa.ends_at + interval '4 hours') OR (sa.id IS NULL AND now() > a.clock_in_at + interval '16 hours'))`)
		if err != nil {
			return err
		}
		n = int(tag.RowsAffected())
		return nil
	})
	return n, err
}
