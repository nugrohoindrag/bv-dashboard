// Package workforce: lapisan pengelolaan tim P2 (PRD P2 v2.1 §8; Roadmap v2.1 §9 Shift Management, §10 Shift, §25.2 Workforce,
// §25.8 Workforce Capacity). Keputusan D-P2-05: shift PER DOMAIN — Security Shift Management & Housekeeping Shift dikelola dari
// modul domainnya (route /security/* & /housekeeping/*, permission security.shifts.* / housekeeping.shifts.*) di atas satu
// model tabel (kolom domain) agar field inti & format event seragam. Tanpa payroll/cuti (Roadmap §2). Kompetensi staf
// (skill + sertifikat) dihapus dari scope atas keputusan user 2026-09-27.
package workforce

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/property"
)

type Service struct {
	DB   *db.DB
	Jobs jobs.Enqueuer
}

func New(d *db.DB, j jobs.Enqueuer) *Service { return &Service{DB: d, Jobs: j} }

// ShiftDomains: domain dengan shift (D-P2-05).
var ShiftDomains = map[string]bool{"security": true, "housekeeping": true}

func shiftPerm(domain, action string) string { return domain + ".shifts." + action }

func validDomain(domain string) error {
	if !ShiftDomains[domain] {
		return apperr.NotFound("Domain shift")
	}
	return nil
}

func actorOrNil(p *authctx.Principal) *uuid.UUID {
	if p.IsSystem || p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------- Shift definitions (P2-SHF-01) ----------

type ShiftDefinition struct {
	ID              uuid.UUID `json:"id"`
	PropertyID      uuid.UUID `json:"property_id"`
	Domain          string    `json:"domain"`
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	StartTime       string    `json:"start_time"` // HH:MM
	EndTime         string    `json:"end_time"`
	CrossesMidnight bool      `json:"crosses_midnight"`
	DurationMinutes int       `json:"duration_minutes"`
	BreakMinutes    int       `json:"break_minutes"`
	MinStaff        int       `json:"min_staff"`
	Color           *string   `json:"color"`
	SortOrder       int       `json:"sort_order"`
	IsActive        bool      `json:"is_active"`
	Version         int       `json:"version"`
}

type ShiftInput struct {
	PropertyID   *uuid.UUID `json:"property_id"`
	Code         *string    `json:"code"`
	Name         *string    `json:"name"`
	StartTime    *string    `json:"start_time"`
	EndTime      *string    `json:"end_time"`
	BreakMinutes *int       `json:"break_minutes"`
	MinStaff     *int       `json:"min_staff"`
	Color        *string    `json:"color"`
	SortOrder    *int       `json:"sort_order"`
	IsActive     *bool      `json:"is_active"`
	IfVersion    *int       `json:"-"` // If-Match (PATCH)
}

const shiftSelect = `SELECT id, property_id, domain, code, name, to_char(start_time,'HH24:MI'), to_char(end_time,'HH24:MI'), break_minutes, min_staff, color, sort_order, is_active, version FROM shift_definitions`

func scanShift(row pgx.Row) (*ShiftDefinition, error) {
	var s ShiftDefinition
	if err := row.Scan(&s.ID, &s.PropertyID, &s.Domain, &s.Code, &s.Name, &s.StartTime, &s.EndTime, &s.BreakMinutes, &s.MinStaff, &s.Color, &s.SortOrder, &s.IsActive, &s.Version); err != nil {
		return nil, err
	}
	st, _ := time.Parse("15:04", s.StartTime)
	et, _ := time.Parse("15:04", s.EndTime)
	d := et.Sub(st)
	if d <= 0 {
		s.CrossesMidnight = true
		d += 24 * time.Hour
	}
	s.DurationMinutes = int(d.Minutes())
	return &s, nil
}

func parseClock(v, field string) (time.Time, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(v))
	if err != nil {
		return time.Time{}, apperr.Validation(field+" harus HH:MM").WithField(field, "format HH:MM")
	}
	return t, nil
}

// window: [starts_at, ends_at] shift pada tanggal tertentu dalam timezone property.
func (d *ShiftDefinition) window(date time.Time, loc *time.Location) (time.Time, time.Time) {
	st, _ := time.Parse("15:04", d.StartTime)
	start := time.Date(date.Year(), date.Month(), date.Day(), st.Hour(), st.Minute(), 0, 0, loc)
	return start, start.Add(time.Duration(d.DurationMinutes) * time.Minute)
}

func (s *Service) getShiftTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*ShiftDefinition, error) {
	sd, err := scanShift(tx.QueryRow(ctx, shiftSelect+` WHERE id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Shift")
		}
		return nil, err
	}
	return sd, nil
}

func (s *Service) ListShifts(ctx context.Context, domain string, propertyID *uuid.UUID) ([]ShiftDefinition, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	out := []ShiftDefinition{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{domain}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE domain = $1"
		if propertyID != nil {
			if !p.HasAnyOnProperty(shiftPerm(domain, "view"), *propertyID) {
				return apperr.Forbidden("")
			}
			where += " AND property_id = " + add(*propertyID)
		}
		where += " AND " + p.ScopeSQL(shiftPerm(domain, "view"), "property_id", "", add)
		rows, err := tx.Query(ctx, shiftSelect+where+" ORDER BY sort_order, start_time", args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			sd, err := scanShift(rows)
			if err != nil {
				return err
			}
			out = append(out, *sd)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) SaveShift(ctx context.Context, domain string, id *uuid.UUID, in ShiftInput) (*ShiftDefinition, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	var out *ShiftDefinition
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for f, v := range map[string]*string{"start_time": in.StartTime, "end_time": in.EndTime} {
			if v != nil {
				if _, err := parseClock(*v, f); err != nil {
					return err
				}
			}
		}
		if in.StartTime != nil && in.EndTime != nil && strings.TrimSpace(*in.StartTime) == strings.TrimSpace(*in.EndTime) {
			return apperr.Validation("start_time dan end_time tidak boleh sama").WithField("end_time", "berbeda dari start_time")
		}
		if (in.MinStaff != nil && *in.MinStaff < 0) || (in.BreakMinutes != nil && *in.BreakMinutes < 0) {
			return apperr.Validation("min_staff/break_minutes tidak boleh negatif")
		}
		var sid, pid uuid.UUID
		if id == nil {
			if in.PropertyID == nil || in.Code == nil || strings.TrimSpace(*in.Code) == "" || in.Name == nil || strings.TrimSpace(*in.Name) == "" || in.StartTime == nil || in.EndTime == nil {
				return apperr.Validation("property_id, code, name, start_time, end_time wajib")
			}
			pid = *in.PropertyID
		} else {
			cur, err := s.getShiftTx(ctx, tx, *id)
			if err != nil || cur.Domain != domain {
				return apperr.NotFound("Shift")
			}
			if in.IfVersion != nil && *in.IfVersion != cur.Version {
				return apperr.StaleVersion()
			}
			pid = cur.PropertyID
		}
		if !p.HasOnProperty(shiftPerm(domain, "manage"), pid) {
			return apperr.Forbidden("Memerlukan " + shiftPerm(domain, "manage"))
		}
		if id == nil {
			brk, minStaff, order := 0, 1, 0
			if in.BreakMinutes != nil {
				brk = *in.BreakMinutes
			}
			if in.MinStaff != nil {
				minStaff = *in.MinStaff
			}
			if in.SortOrder != nil {
				order = *in.SortOrder
			}
			if err := tx.QueryRow(ctx, `INSERT INTO shift_definitions (organization_id, property_id, domain, code, name, start_time, end_time, break_minutes, min_staff, color, sort_order, created_by, updated_by)
				VALUES ($1,$2,$3,$4,$5,$6::time,$7::time,$8,$9,$10,$11,$12,$12) RETURNING id`,
				p.OrganizationID, pid, domain, strings.ToUpper(strings.TrimSpace(*in.Code)), strings.TrimSpace(*in.Name), *in.StartTime, *in.EndTime, brk, minStaff, in.Color, order, p.UserID).Scan(&sid); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("DUPLICATE_CODE", "Kode shift sudah dipakai")
				}
				return err
			}
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "shift_definition", EntityID: &sid, EntityLabel: domain + " " + *in.Name, After: in})
		} else {
			sid = *id
			if _, err := tx.Exec(ctx, `UPDATE shift_definitions SET code = COALESCE(NULLIF(UPPER(TRIM($2)),''), code), name = COALESCE(NULLIF(TRIM($3),''), name), start_time = COALESCE($4::time, start_time),
				end_time = COALESCE($5::time, end_time), break_minutes = COALESCE($6, break_minutes), min_staff = COALESCE($7, min_staff), color = CASE WHEN $8::text IS NULL THEN color ELSE NULLIF(btrim($8::text), '') END, sort_order = COALESCE($9, sort_order),
				is_active = COALESCE($10, is_active), updated_by = $11 WHERE id = $1`,
				sid, deref(in.Code), deref(in.Name), in.StartTime, in.EndTime, in.BreakMinutes, in.MinStaff, in.Color, in.SortOrder, in.IsActive, p.UserID); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("DUPLICATE_CODE", "Kode shift sudah dipakai")
				}
				return err
			}
			// jam shift berubah → jadwal roster ke depan ikut dihitung ulang
			if in.StartTime != nil || in.EndTime != nil {
				sd, err := s.getShiftTx(ctx, tx, sid)
				if err != nil {
					return err
				}
				loc := property.PropertyTimezone(ctx, tx, pid)
				rows, err := tx.Query(ctx, `SELECT id, shift_date FROM shift_assignments WHERE shift_id = $1 AND shift_date >= CURRENT_DATE`, sid)
				if err != nil {
					return err
				}
				type ra struct {
					id uuid.UUID
					d  time.Time
				}
				var list []ra
				for rows.Next() {
					var x ra
					if rows.Scan(&x.id, &x.d) == nil {
						list = append(list, x)
					}
				}
				rows.Close()
				for _, x := range list {
					st, en := sd.window(x.d, loc)
					_, _ = tx.Exec(ctx, `UPDATE shift_assignments SET starts_at = $2, ends_at = $3 WHERE id = $1`, x.id, st, en)
				}
			}
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "shift_definition", EntityID: &sid, EntityLabel: domain, After: in})
		}
		var err error
		out, err = s.getShiftTx(ctx, tx, sid)
		return err
	})
	return out, err
}

// ---------- Roster (P2-SHF-02) ----------

type RosterEntry struct {
	ID         uuid.UUID  `json:"id"`
	PropertyID uuid.UUID  `json:"property_id"`
	Domain     string     `json:"domain"`
	ShiftID    uuid.UUID  `json:"shift_id"`
	ShiftCode  string     `json:"shift_code"`
	ShiftName  string     `json:"shift_name"`
	ShiftDate  string     `json:"shift_date"`
	UserID     uuid.UUID  `json:"user_id"`
	UserName   string     `json:"user_name"`
	TeamID     *uuid.UUID `json:"team_id"`
	TeamName   *string    `json:"team_name"`
	Post       *string    `json:"post"`
	StartsAt   time.Time  `json:"starts_at"`
	EndsAt     time.Time  `json:"ends_at"`
	Status     string     `json:"status"`
	Note       *string    `json:"note"`
	// attendance untuk tanggal ini
	ClockInAt   *time.Time `json:"clock_in_at"`
	ClockOutAt  *time.Time `json:"clock_out_at"`
	LateMinutes *int       `json:"late_minutes"`
	Attendance  string     `json:"attendance"` // upcoming | on_duty | completed | late | absent
}

const rosterSelect = `SELECT sa.id, sa.property_id, sa.domain, sa.shift_id, sd.code, sd.name, sa.shift_date::text, sa.user_id, u.full_name, sa.team_id, t.name, sa.post, sa.starts_at, sa.ends_at, sa.status, sa.note,
	ar.clock_in_at, ar.clock_out_at, ar.late_minutes
	FROM shift_assignments sa JOIN shift_definitions sd ON sd.id = sa.shift_id JOIN users u ON u.id = sa.user_id LEFT JOIN teams t ON t.id = sa.team_id
	LEFT JOIN LATERAL (SELECT a.clock_in_at, a.clock_out_at, a.late_minutes FROM attendance_records a WHERE a.shift_assignment_id = sa.id ORDER BY a.clock_in_at DESC LIMIT 1) ar ON true`

// absentGrace: roster dianggap absent bila belum clock-in lewat batas ini dari awal shift.
const absentGrace = 15 * time.Minute

func scanRoster(row pgx.Row) (*RosterEntry, error) {
	var r RosterEntry
	if err := row.Scan(&r.ID, &r.PropertyID, &r.Domain, &r.ShiftID, &r.ShiftCode, &r.ShiftName, &r.ShiftDate, &r.UserID, &r.UserName, &r.TeamID, &r.TeamName, &r.Post, &r.StartsAt, &r.EndsAt, &r.Status, &r.Note,
		&r.ClockInAt, &r.ClockOutAt, &r.LateMinutes); err != nil {
		return nil, err
	}
	now := time.Now()
	switch {
	case r.Status == "cancelled":
		r.Attendance = "cancelled"
	case r.ClockInAt != nil && r.ClockOutAt == nil:
		r.Attendance = "on_duty"
		if r.LateMinutes != nil && *r.LateMinutes > 0 {
			r.Attendance = "late"
		}
	case r.ClockInAt != nil:
		r.Attendance = "completed"
	case now.After(r.StartsAt.Add(absentGrace)):
		r.Attendance = "absent"
	default:
		r.Attendance = "upcoming"
	}
	return &r, nil
}

type RosterFilter struct {
	PropertyID *uuid.UUID
	From, To   string // YYYY-MM-DD
	UserID     *uuid.UUID
	ShiftID    *uuid.UUID
}

func (s *Service) ListRoster(ctx context.Context, domain string, f RosterFilter) ([]RosterEntry, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	out := []RosterEntry{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{domain}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE sa.domain = $1 AND sa.status = 'scheduled'"
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty(shiftPerm(domain, "view"), *f.PropertyID) {
				return apperr.Forbidden("")
			}
			where += " AND sa.property_id = " + add(*f.PropertyID)
		}
		where += " AND " + p.ScopeSQL(shiftPerm(domain, "view"), "sa.property_id", "", add)
		from, to := f.From, f.To
		if from == "" {
			from = time.Now().Format("2006-01-02")
		}
		if to == "" {
			t, _ := time.Parse("2006-01-02", from)
			to = t.AddDate(0, 0, 6).Format("2006-01-02")
		}
		if _, err := time.Parse("2006-01-02", from); err != nil {
			return apperr.Validation("from harus YYYY-MM-DD")
		}
		if _, err := time.Parse("2006-01-02", to); err != nil {
			return apperr.Validation("to harus YYYY-MM-DD")
		}
		where += " AND sa.shift_date BETWEEN " + add(from) + "::date AND " + add(to) + "::date"
		if f.UserID != nil {
			where += " AND sa.user_id = " + add(*f.UserID)
		}
		if f.ShiftID != nil {
			where += " AND sa.shift_id = " + add(*f.ShiftID)
		}
		rows, err := tx.Query(ctx, rosterSelect+where+" ORDER BY sa.shift_date, sa.starts_at, u.full_name", args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRoster(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}

// RosterAssignInput: penugasan massal — shift × tanggal × staf (P2-SHF-02).
type RosterAssignInput struct {
	ShiftID uuid.UUID   `json:"shift_id"`
	Dates   []string    `json:"dates"` // YYYY-MM-DD
	UserIDs []uuid.UUID `json:"user_ids"`
	TeamID  *uuid.UUID  `json:"team_id"`
	Post    *string     `json:"post"`
	Note    *string     `json:"note"`
}

type RosterAssignResult struct {
	Created int           `json:"created"`
	Skipped int           `json:"skipped"` // sudah ada
	Entries []RosterEntry `json:"entries"`
}

func (s *Service) AssignRoster(ctx context.Context, domain string, in RosterAssignInput) (*RosterAssignResult, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	if len(in.Dates) == 0 || len(in.UserIDs) == 0 {
		return nil, apperr.Validation("dates dan user_ids wajib")
	}
	if len(in.Dates)*len(in.UserIDs) > 1000 {
		return nil, apperr.Validation("maksimal 1000 penugasan per permintaan")
	}
	p := authctx.Must(ctx)
	out := &RosterAssignResult{Entries: []RosterEntry{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sd, err := s.getShiftTx(ctx, tx, in.ShiftID)
		if err != nil || sd.Domain != domain {
			return apperr.Validation("shift_id tidak ditemukan di domain ini").WithField("shift_id", "tidak valid")
		}
		if !sd.IsActive {
			return apperr.Validation("shift nonaktif").WithField("shift_id", "nonaktif")
		}
		if !p.HasOnProperty(shiftPerm(domain, "manage"), sd.PropertyID) {
			return apperr.Forbidden("Memerlukan " + shiftPerm(domain, "manage"))
		}
		loc := property.PropertyTimezone(ctx, tx, sd.PropertyID)
		for _, uid := range in.UserIDs {
			var active bool
			if err := tx.QueryRow(ctx, `SELECT is_active FROM users WHERE id = $1 AND deleted_at IS NULL`, uid).Scan(&active); err != nil || !active {
				return apperr.Validation("user tidak ditemukan / nonaktif: "+uid.String()).WithField("user_ids", "tidak valid")
			}
		}
		var ids []uuid.UUID
		for _, ds := range in.Dates {
			d, err := time.ParseInLocation("2006-01-02", ds, loc)
			if err != nil {
				return apperr.Validation("dates harus YYYY-MM-DD").WithField("dates", "format tanggal")
			}
			st, en := sd.window(d, loc)
			for _, uid := range in.UserIDs {
				var id uuid.UUID
				err := tx.QueryRow(ctx, `INSERT INTO shift_assignments (organization_id, property_id, domain, shift_id, shift_date, user_id, team_id, post, starts_at, ends_at, note, created_by, updated_by)
					VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11,$12,$12)
					ON CONFLICT (shift_id, shift_date, user_id) DO UPDATE SET status = 'scheduled', post = COALESCE(EXCLUDED.post, shift_assignments.post), updated_by = EXCLUDED.updated_by
					WHERE shift_assignments.status = 'cancelled' RETURNING id`,
					p.OrganizationID, sd.PropertyID, domain, sd.ID, ds, uid, in.TeamID, in.Post, st, en, in.Note, p.UserID).Scan(&id)
				if db.IsNoRows(err) {
					out.Skipped++
					continue
				}
				if err != nil {
					return err
				}
				out.Created++
				ids = append(ids, id)
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "shift_roster", EntityLabel: fmt.Sprintf("%s %s: %d penugasan", domain, sd.Name, out.Created), After: in})
		for _, id := range ids {
			if r, err := scanRoster(tx.QueryRow(ctx, rosterSelect+` WHERE sa.id = $1`, id)); err == nil {
				out.Entries = append(out.Entries, *r)
			}
		}
		return nil
	})
	return out, err
}

// CancelRoster: batalkan penugasan (bukan hapus — riwayat tetap).
func (s *Service) CancelRoster(ctx context.Context, domain string, id uuid.UUID) error {
	if err := validDomain(domain); err != nil {
		return err
	}
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pid uuid.UUID
		var d string
		if err := tx.QueryRow(ctx, `SELECT property_id, domain FROM shift_assignments WHERE id = $1`, id).Scan(&pid, &d); err != nil || d != domain {
			return apperr.NotFound("Roster")
		}
		if !p.HasOnProperty(shiftPerm(domain, "manage"), pid) {
			return apperr.Forbidden("Memerlukan " + shiftPerm(domain, "manage"))
		}
		var hasAttendance bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attendance_records WHERE shift_assignment_id = $1)`, id).Scan(&hasAttendance)
		if hasAttendance {
			return apperr.Conflict("ROSTER_IN_USE", "Staf sudah clock-in pada shift ini; penugasan tidak dapat dibatalkan")
		}
		if _, err := tx.Exec(ctx, `UPDATE shift_assignments SET status = 'cancelled', updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "shift_roster", EntityID: &id, EntityLabel: "cancel"})
	})
}

// CopyWeek: salin roster satu minggu ke minggu lain (P2-SHF-02 "salin mingguan").
type CopyWeekInput struct {
	PropertyID    uuid.UUID `json:"property_id"`
	FromWeekStart string    `json:"from_week_start"` // YYYY-MM-DD (hari pertama minggu sumber)
	ToWeekStart   string    `json:"to_week_start"`
}

func (s *Service) CopyWeek(ctx context.Context, domain string, in CopyWeekInput) (*RosterAssignResult, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	from, err1 := time.Parse("2006-01-02", in.FromWeekStart)
	to, err2 := time.Parse("2006-01-02", in.ToWeekStart)
	if err1 != nil || err2 != nil {
		return nil, apperr.Validation("from_week_start/to_week_start harus YYYY-MM-DD")
	}
	if from.Equal(to) {
		return nil, apperr.Validation("minggu tujuan harus berbeda")
	}
	out := &RosterAssignResult{Entries: []RosterEntry{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if !p.HasOnProperty(shiftPerm(domain, "manage"), in.PropertyID) {
			return apperr.Forbidden("Memerlukan " + shiftPerm(domain, "manage"))
		}
		loc := property.PropertyTimezone(ctx, tx, in.PropertyID)
		rows, err := tx.Query(ctx, `SELECT sa.shift_id, sa.shift_date, sa.user_id, sa.team_id, sa.post FROM shift_assignments sa JOIN shift_definitions sd ON sd.id = sa.shift_id
			WHERE sa.property_id = $1 AND sa.domain = $2 AND sa.status = 'scheduled' AND sd.is_active AND sa.shift_date >= $3::date AND sa.shift_date < $3::date + 7`, in.PropertyID, domain, in.FromWeekStart)
		if err != nil {
			return err
		}
		type src struct {
			shift, user uuid.UUID
			date        time.Time
			team        *uuid.UUID
			post        *string
		}
		var list []src
		for rows.Next() {
			var x src
			if err := rows.Scan(&x.shift, &x.date, &x.user, &x.team, &x.post); err != nil {
				rows.Close()
				return err
			}
			list = append(list, x)
		}
		rows.Close()
		shifts := map[uuid.UUID]*ShiftDefinition{}
		offset := int(to.Sub(from).Hours() / 24)
		for _, x := range list {
			sd, ok := shifts[x.shift]
			if !ok {
				if sd, err = s.getShiftTx(ctx, tx, x.shift); err != nil {
					return err
				}
				shifts[x.shift] = sd
			}
			d := x.date.AddDate(0, 0, offset)
			ld := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
			st, en := sd.window(ld, loc)
			tag, err := tx.Exec(ctx, `INSERT INTO shift_assignments (organization_id, property_id, domain, shift_id, shift_date, user_id, team_id, post, starts_at, ends_at, created_by, updated_by)
				VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11,$11) ON CONFLICT (shift_id, shift_date, user_id) DO NOTHING`,
				p.OrganizationID, in.PropertyID, domain, x.shift, ld.Format("2006-01-02"), x.user, x.team, x.post, st, en, p.UserID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() > 0 {
				out.Created++
			} else {
				out.Skipped++
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "shift_roster", EntityLabel: fmt.Sprintf("%s salin %s → %s: %d", domain, in.FromWeekStart, in.ToWeekStart, out.Created), After: in})
		return nil
	})
	return out, err
}

// MyRoster: jadwal shift user login (semua domain) — Staff App "roster saya" (P2-MOB-04).
func (s *Service) MyRoster(ctx context.Context, from, to string) ([]RosterEntry, error) {
	p := authctx.Must(ctx)
	out := []RosterEntry{}
	if from == "" {
		from = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	}
	if to == "" {
		t, _ := time.Parse("2006-01-02", from)
		to = t.AddDate(0, 0, 14).Format("2006-01-02")
	}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, rosterSelect+` WHERE sa.user_id = $1 AND sa.status = 'scheduled' AND sa.shift_date BETWEEN $2::date AND $3::date ORDER BY sa.starts_at`, p.UserID, from, to)
		if err != nil {
			return apperr.Validation("from/to harus YYYY-MM-DD")
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRoster(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}
