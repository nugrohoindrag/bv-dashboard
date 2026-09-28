package security

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Parking sisi security (PRD P2 v2.1 §6.6, D-P2-06) ----------
// Area parkir (kapasitas, tipe), registri kendaraan, log keluar-masuk manual oleh security, pelanggaran parkir → incident.
// Permohonan izin parkir dari Tenant App = P3 (memakai entitas yang sama).

var plateClean = regexp.MustCompile(`[^A-Z0-9]`)

// NormalizePlate: "b 1234 xyz" → "B1234XYZ" (kunci pencarian & keunikan per property).
func NormalizePlate(p string) string { return plateClean.ReplaceAllString(strings.ToUpper(p), "") }

var parkingAreaTypes = map[string]bool{"tenant": true, "visitor": true, "staff": true, "public": true, "loading": true, "mixed": true}
var vehicleTypes = map[string]bool{"car": true, "motorcycle": true, "truck": true, "bicycle": true, "other": true}
var vehicleOwnerTypes = map[string]bool{"tenant": true, "staff": true, "visitor": true, "other": true}
var violationTypes = map[string]bool{"illegal_parking": true, "no_permit": true, "blocking": true, "overstay": true, "reserved_spot": true, "other": true}
var violationActions = map[string]bool{"none": true, "warning": true, "sticker": true, "wheel_lock": true, "towed": true, "reported": true}

type ParkingArea struct {
	ID           uuid.UUID  `json:"id"`
	PropertyID   uuid.UUID  `json:"property_id"`
	LocationID   *uuid.UUID `json:"location_id"`
	LocationPath *string    `json:"location_path"`
	Code         string     `json:"code"`
	Name         string     `json:"name"`
	AreaType     string     `json:"area_type"`
	Capacity     int        `json:"capacity"`
	Occupied     int        `json:"occupied"` // kendaraan tercatat masuk & belum keluar
	Available    int        `json:"available"`
	IsActive     bool       `json:"is_active"`
	Notes        *string    `json:"notes"`
	Version      int        `json:"version"`
}

type ParkingAreaInput struct {
	PropertyID *uuid.UUID `json:"property_id"`
	LocationID *uuid.UUID `json:"location_id"`
	Code       *string    `json:"code"`
	Name       *string    `json:"name"`
	AreaType   *string    `json:"area_type"`
	Capacity   *int       `json:"capacity"`
	IsActive   *bool      `json:"is_active"`
	Notes      *string    `json:"notes"`
}

func parkingPerm(ctx context.Context, action string, propertyID uuid.UUID) error {
	if authctx.Must(ctx).HasAnyOnProperty("security.parking."+action, propertyID) {
		return nil
	}
	return apperr.Forbidden("Memerlukan security.parking." + action)
}

func (s *Service) parkingAreasTx(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]ParkingArea, error) {
	rows, err := tx.Query(ctx, `SELECT a.id, a.property_id, a.location_id, a.code, a.name, a.area_type, a.capacity, a.is_active, a.notes, a.version,
		(SELECT count(*) FROM parking_logs pl WHERE pl.parking_area_id = a.id AND pl.exited_at IS NULL)
		FROM parking_areas a WHERE `+where+` ORDER BY a.code`, args...)
	if err != nil {
		return nil, err
	}
	out := []ParkingArea{}
	for rows.Next() {
		var a ParkingArea
		if err := rows.Scan(&a.ID, &a.PropertyID, &a.LocationID, &a.Code, &a.Name, &a.AreaType, &a.Capacity, &a.IsActive, &a.Notes, &a.Version, &a.Occupied); err != nil {
			rows.Close()
			return nil, err
		}
		a.Available = a.Capacity - a.Occupied
		if a.Available < 0 {
			a.Available = 0
		}
		out = append(out, a)
	}
	rows.Close()
	for i := range out {
		if out[i].LocationID != nil {
			pt := property.LocationPathText(ctx, tx, *out[i].LocationID)
			out[i].LocationPath = &pt
		}
	}
	return out, nil
}

func (s *Service) ListParkingAreas(ctx context.Context, propertyID *uuid.UUID) ([]ParkingArea, error) {
	p := authctx.Must(ctx)
	var out []ParkingArea
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := "TRUE"
		if propertyID != nil {
			if err := parkingPerm(ctx, "view", *propertyID); err != nil {
				return err
			}
			where += " AND a.property_id = " + add(*propertyID)
		}
		where += " AND " + p.ScopeSQL("security.parking.view", "a.property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = a.location_id)", add)
		var err error
		out, err = s.parkingAreasTx(ctx, tx, where, args...)
		return err
	})
	return out, err
}

func (s *Service) SaveParkingArea(ctx context.Context, id *uuid.UUID, in ParkingAreaInput) (*ParkingArea, error) {
	p := authctx.Must(ctx)
	var out *ParkingArea
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if in.AreaType != nil && !parkingAreaTypes[*in.AreaType] {
			return apperr.Validation("area_type harus tenant|visitor|staff|public|loading|mixed").WithField("area_type", "tidak valid")
		}
		if in.Capacity != nil && *in.Capacity < 0 {
			return apperr.Validation("capacity tidak boleh negatif").WithField("capacity", "≥ 0")
		}
		var aid, pid uuid.UUID
		if id == nil {
			if in.PropertyID == nil || in.Name == nil || strings.TrimSpace(*in.Name) == "" || in.Code == nil || strings.TrimSpace(*in.Code) == "" {
				return apperr.Validation("property_id, code, name wajib")
			}
			pid = *in.PropertyID
		} else {
			if err := tx.QueryRow(ctx, `SELECT property_id FROM parking_areas WHERE id = $1`, *id).Scan(&pid); err != nil {
				return apperr.NotFound("Parking area")
			}
		}
		if !p.HasOnProperty("security.parking.manage", pid) {
			return apperr.Forbidden("Memerlukan security.parking.manage")
		}
		if in.LocationID != nil && *in.LocationID != uuid.Nil {
			lp, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
			if err != nil || lp != pid {
				return apperr.Validation("location_id tidak berada di property ini").WithField("location_id", "tidak valid")
			}
		}
		if id == nil {
			at := "mixed"
			if in.AreaType != nil {
				at = *in.AreaType
			}
			capacity := 0
			if in.Capacity != nil {
				capacity = *in.Capacity
			}
			if err := tx.QueryRow(ctx, `INSERT INTO parking_areas (organization_id, property_id, location_id, code, name, area_type, capacity, notes, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9) RETURNING id`,
				p.OrganizationID, pid, in.LocationID, strings.ToUpper(strings.TrimSpace(*in.Code)), strings.TrimSpace(*in.Name), at, capacity, in.Notes, p.UserID).Scan(&aid); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("DUPLICATE_CODE", "Kode area parkir sudah dipakai di property ini")
				}
				return err
			}
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "parking_area", EntityID: &aid, EntityLabel: *in.Name, After: in})
		} else {
			aid = *id
			// location_id UUID nol = kosongkan
			if _, err := tx.Exec(ctx, `UPDATE parking_areas SET location_id = CASE WHEN $2::uuid IS NULL THEN location_id WHEN $2::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL ELSE $2::uuid END, code = COALESCE(NULLIF(UPPER(TRIM($3)),''), code), name = COALESCE(NULLIF(TRIM($4),''), name),
				area_type = COALESCE($5, area_type), capacity = COALESCE($6, capacity), is_active = COALESCE($7, is_active), notes = COALESCE($8, notes), updated_by = $9 WHERE id = $1`,
				aid, in.LocationID, deref(in.Code), deref(in.Name), in.AreaType, in.Capacity, in.IsActive, in.Notes, p.UserID); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("DUPLICATE_CODE", "Kode area parkir sudah dipakai di property ini")
				}
				return err
			}
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "parking_area", EntityID: &aid, EntityLabel: deref(in.Name), After: in})
		}
		list, err := s.parkingAreasTx(ctx, tx, "a.id = $1", aid)
		if err == nil && len(list) == 1 {
			out = &list[0]
		}
		return err
	})
	return out, err
}

// ---------- Vehicles ----------

type Vehicle struct {
	ID              uuid.UUID  `json:"id"`
	PropertyID      uuid.UUID  `json:"property_id"`
	PlateNumber     string     `json:"plate_number"`
	VehicleType     string     `json:"vehicle_type"`
	Brand           *string    `json:"brand"`
	Color           *string    `json:"color"`
	OwnerType       string     `json:"owner_type"`
	TenantID        *uuid.UUID `json:"tenant_id"`
	TenantName      *string    `json:"tenant_name"`
	UnitLocationID  *uuid.UUID `json:"unit_location_id"`
	UnitName        *string    `json:"unit_name"`
	UserID          *uuid.UUID `json:"user_id"`
	UserName        *string    `json:"user_name"`
	VisitorID       *uuid.UUID `json:"visitor_id"`
	OwnerName       *string    `json:"owner_name"`
	OwnerPhone      *string    `json:"owner_phone"`
	ParkingAreaID   *uuid.UUID `json:"parking_area_id"`
	ParkingAreaName *string    `json:"parking_area_name"`
	PermitUntil     *string    `json:"permit_until"`
	PermitValid     bool       `json:"permit_valid"`
	Status          string     `json:"status"`
	Notes           *string    `json:"notes"`
	OpenViolations  int        `json:"open_violations"`
	Inside          bool       `json:"inside"` // tercatat masuk & belum keluar
	Version         int        `json:"version"`
}

type VehicleInput struct {
	PropertyID     *uuid.UUID `json:"property_id"`
	PlateNumber    *string    `json:"plate_number"`
	VehicleType    *string    `json:"vehicle_type"`
	Brand          *string    `json:"brand"`
	Color          *string    `json:"color"`
	OwnerType      *string    `json:"owner_type"`
	TenantID       *uuid.UUID `json:"tenant_id"`
	UnitLocationID *uuid.UUID `json:"unit_location_id"`
	UserID         *uuid.UUID `json:"user_id"`
	VisitorID      *uuid.UUID `json:"visitor_id"`
	OwnerName      *string    `json:"owner_name"`
	OwnerPhone     *string    `json:"owner_phone"`
	ParkingAreaID  *uuid.UUID `json:"parking_area_id"`
	PermitUntil    *string    `json:"permit_until"`
	Status         *string    `json:"status"`
	Notes          *string    `json:"notes"`
}

const vehicleSelect = `SELECT v.id, v.property_id, v.plate_number, v.vehicle_type, v.brand, v.color, v.owner_type, v.tenant_id, t.name, v.unit_location_id, ul.name, v.user_id, u.full_name, v.visitor_id,
	v.owner_name, v.owner_phone, v.parking_area_id, pa.name, v.permit_until::text, v.status, v.notes, v.version,
	(SELECT count(*) FROM parking_violations pv WHERE pv.vehicle_id = v.id AND pv.status = 'open'),
	EXISTS (SELECT 1 FROM parking_logs pl WHERE pl.vehicle_id = v.id AND pl.exited_at IS NULL)
	FROM vehicles v LEFT JOIN tenants t ON t.id = v.tenant_id LEFT JOIN locations ul ON ul.id = v.unit_location_id LEFT JOIN users u ON u.id = v.user_id LEFT JOIN parking_areas pa ON pa.id = v.parking_area_id`

func scanVehicle(row pgx.Row) (*Vehicle, error) {
	var v Vehicle
	if err := row.Scan(&v.ID, &v.PropertyID, &v.PlateNumber, &v.VehicleType, &v.Brand, &v.Color, &v.OwnerType, &v.TenantID, &v.TenantName, &v.UnitLocationID, &v.UnitName, &v.UserID, &v.UserName, &v.VisitorID,
		&v.OwnerName, &v.OwnerPhone, &v.ParkingAreaID, &v.ParkingAreaName, &v.PermitUntil, &v.Status, &v.Notes, &v.Version, &v.OpenViolations, &v.Inside); err != nil {
		return nil, err
	}
	v.PermitValid = v.Status == "active" && (v.PermitUntil == nil || *v.PermitUntil >= time.Now().Format("2006-01-02"))
	return &v, nil
}

type VehicleFilter struct {
	PropertyID *uuid.UUID
	Q          string // plat / pemilik
	OwnerType  string
	Status     string
}

func (s *Service) ListVehicles(ctx context.Context, f VehicleFilter, page httpx.Page) ([]Vehicle, *string, error) {
	p := authctx.Must(ctx)
	out := []Vehicle{}
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE TRUE"
		if f.PropertyID != nil {
			if err := parkingPerm(ctx, "view", *f.PropertyID); err != nil {
				return err
			}
			where += " AND v.property_id = " + add(*f.PropertyID)
		}
		where += " AND " + p.ScopeSQL("security.parking.view", "v.property_id", "", add)
		if q := strings.TrimSpace(f.Q); q != "" {
			np := NormalizePlate(q)
			qa := add("%" + q + "%")
			if np != "" {
				where += " AND (v.plate_number LIKE " + add("%"+np+"%") + " OR v.owner_name ILIKE " + qa + ")"
			} else {
				where += " AND v.owner_name ILIKE " + qa
			}
		}
		if f.OwnerType != "" {
			where += " AND v.owner_type = " + add(f.OwnerType)
		}
		if f.Status != "" {
			where += " AND v.status = " + add(f.Status)
		}
		if page.Cursor != nil {
			where += " AND (v.plate_number, v.id) > (" + add(page.Cursor.Value) + ", " + add(page.Cursor.ID) + ")"
		}
		rows, err := tx.Query(ctx, vehicleSelect+where+" ORDER BY v.plate_number, v.id LIMIT "+add(page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanVehicle(rows)
			if err != nil {
				return err
			}
			out = append(out, *v)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.PlateNumber, last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	return out, next, err
}

func (s *Service) SaveVehicle(ctx context.Context, id *uuid.UUID, in VehicleInput) (*Vehicle, error) {
	p := authctx.Must(ctx)
	var out *Vehicle
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if in.VehicleType != nil && !vehicleTypes[*in.VehicleType] {
			return apperr.Validation("vehicle_type harus car|motorcycle|truck|bicycle|other").WithField("vehicle_type", "tidak valid")
		}
		if in.OwnerType != nil && !vehicleOwnerTypes[*in.OwnerType] {
			return apperr.Validation("owner_type harus tenant|staff|visitor|other").WithField("owner_type", "tidak valid")
		}
		if in.Status != nil && *in.Status != "active" && *in.Status != "inactive" && *in.Status != "blacklisted" {
			return apperr.Validation("status harus active|inactive|blacklisted").WithField("status", "tidak valid")
		}
		if in.PermitUntil != nil && *in.PermitUntil != "" {
			if _, err := time.Parse("2006-01-02", *in.PermitUntil); err != nil {
				return apperr.Validation("permit_until harus YYYY-MM-DD").WithField("permit_until", "format tanggal")
			}
		}
		var vid, pid uuid.UUID
		if id == nil {
			if in.PropertyID == nil || in.PlateNumber == nil || NormalizePlate(*in.PlateNumber) == "" {
				return apperr.Validation("property_id dan plate_number wajib")
			}
			pid = *in.PropertyID
		} else if err := tx.QueryRow(ctx, `SELECT property_id FROM vehicles WHERE id = $1`, *id).Scan(&pid); err != nil {
			return apperr.NotFound("Vehicle")
		}
		if !p.HasOnProperty("security.parking.manage", pid) {
			return apperr.Forbidden("Memerlukan security.parking.manage")
		}
		plate := ""
		if in.PlateNumber != nil {
			plate = NormalizePlate(*in.PlateNumber)
		}
		if in.ParkingAreaID != nil && *in.ParkingAreaID != uuid.Nil {
			var ap uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT property_id FROM parking_areas WHERE id = $1`, *in.ParkingAreaID).Scan(&ap); err != nil || ap != pid {
				return apperr.Validation("parking_area_id tidak berada di property ini").WithField("parking_area_id", "tidak valid")
			}
		}
		permit := nilIfEmpty(in.PermitUntil)
		if id == nil {
			vt, ot := "car", "tenant"
			if in.VehicleType != nil {
				vt = *in.VehicleType
			}
			if in.OwnerType != nil {
				ot = *in.OwnerType
			}
			if err := tx.QueryRow(ctx, `INSERT INTO vehicles (organization_id, property_id, plate_number, vehicle_type, brand, color, owner_type, tenant_id, unit_location_id, user_id, visitor_id, owner_name, owner_phone, parking_area_id, permit_until, notes, created_by, updated_by)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::date,$16,$17,$17) RETURNING id`,
				p.OrganizationID, pid, plate, vt, in.Brand, in.Color, ot, in.TenantID, in.UnitLocationID, in.UserID, in.VisitorID, in.OwnerName, in.OwnerPhone, in.ParkingAreaID, permit, in.Notes, p.UserID).Scan(&vid); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("DUPLICATE_PLATE", "Plat "+plate+" sudah terdaftar di property ini")
				}
				return err
			}
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "vehicle", EntityID: &vid, EntityLabel: plate})
		} else {
			vid = *id
			if _, err := tx.Exec(ctx, `UPDATE vehicles SET plate_number = COALESCE(NULLIF($2,''), plate_number), vehicle_type = COALESCE($3, vehicle_type), brand = COALESCE($4, brand), color = COALESCE($5, color),
				owner_type = COALESCE($6, owner_type), tenant_id = COALESCE($7, tenant_id), unit_location_id = COALESCE($8, unit_location_id), user_id = COALESCE($9, user_id), visitor_id = COALESCE($10, visitor_id),
				owner_name = COALESCE($11, owner_name), owner_phone = COALESCE($12, owner_phone),
				parking_area_id = CASE WHEN $13::uuid IS NULL THEN parking_area_id WHEN $13::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL ELSE $13::uuid END,
				permit_until = CASE WHEN $14::text IS NULL THEN permit_until WHEN $14::text = '' THEN NULL ELSE $14::date END,
				status = COALESCE($15, status), notes = COALESCE($16, notes), updated_by = $17 WHERE id = $1`,
				vid, plate, in.VehicleType, in.Brand, in.Color, in.OwnerType, in.TenantID, in.UnitLocationID, in.UserID, in.VisitorID, in.OwnerName, in.OwnerPhone, in.ParkingAreaID, in.PermitUntil, in.Status, in.Notes, p.UserID); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("DUPLICATE_PLATE", "Plat "+plate+" sudah terdaftar di property ini")
				}
				return err
			}
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "vehicle", EntityID: &vid, EntityLabel: plate, After: in})
		}
		v, err := scanVehicle(tx.QueryRow(ctx, vehicleSelect+` WHERE v.id = $1`, vid))
		out = v
		return err
	})
	return out, err
}

// ---------- Parking logs (keluar-masuk manual oleh security) ----------

type ParkingLog struct {
	ID              uuid.UUID  `json:"id"`
	PropertyID      uuid.UUID  `json:"property_id"`
	ParkingAreaID   *uuid.UUID `json:"parking_area_id"`
	ParkingAreaName *string    `json:"parking_area_name"`
	VehicleID       *uuid.UUID `json:"vehicle_id"`
	PlateNumber     string     `json:"plate_number"`
	OwnerType       *string    `json:"owner_type"`
	Registered      bool       `json:"registered"`
	EnteredAt       time.Time  `json:"entered_at"`
	ExitedAt        *time.Time `json:"exited_at"`
	DurationMinutes int        `json:"duration_minutes"`
	Gate            *string    `json:"gate"`
	EntryByName     *string    `json:"entry_by_name"`
	Note            *string    `json:"note"`
}

type ParkingEntryInput struct {
	PropertyID    *uuid.UUID `json:"property_id"`
	ParkingAreaID *uuid.UUID `json:"parking_area_id"`
	PlateNumber   string     `json:"plate_number"`
	Gate          *string    `json:"gate"`
	Note          *string    `json:"note"`
}

const parkingLogSelect = `SELECT pl.id, pl.property_id, pl.parking_area_id, pa.name, pl.vehicle_id, pl.plate_number, v.owner_type, pl.entered_at, pl.exited_at, pl.gate, u.full_name, pl.note
	FROM parking_logs pl LEFT JOIN parking_areas pa ON pa.id = pl.parking_area_id LEFT JOIN vehicles v ON v.id = pl.vehicle_id LEFT JOIN users u ON u.id = pl.entry_by`

func scanParkingLog(row pgx.Row) (*ParkingLog, error) {
	var l ParkingLog
	if err := row.Scan(&l.ID, &l.PropertyID, &l.ParkingAreaID, &l.ParkingAreaName, &l.VehicleID, &l.PlateNumber, &l.OwnerType, &l.EnteredAt, &l.ExitedAt, &l.Gate, &l.EntryByName, &l.Note); err != nil {
		return nil, err
	}
	l.Registered = l.VehicleID != nil
	end := time.Now()
	if l.ExitedAt != nil {
		end = *l.ExitedAt
	}
	l.DurationMinutes = int(end.Sub(l.EnteredAt).Minutes())
	return &l, nil
}

func (s *Service) RecordParkingEntry(ctx context.Context, in ParkingEntryInput) (*ParkingLog, error) {
	p := authctx.Must(ctx)
	plate := NormalizePlate(in.PlateNumber)
	if plate == "" {
		return nil, apperr.Validation("plate_number wajib").WithField("plate_number", "wajib")
	}
	var out *ParkingLog
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pid uuid.UUID
		switch {
		case in.ParkingAreaID != nil:
			if err := tx.QueryRow(ctx, `SELECT property_id FROM parking_areas WHERE id = $1 AND is_active`, *in.ParkingAreaID).Scan(&pid); err != nil {
				return apperr.Validation("parking_area_id tidak ditemukan / nonaktif").WithField("parking_area_id", "tidak valid")
			}
		case in.PropertyID != nil:
			pid = *in.PropertyID
		default:
			return apperr.Validation("property_id atau parking_area_id wajib")
		}
		if err := parkingPerm(ctx, "record", pid); err != nil {
			return err
		}
		var open bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM parking_logs WHERE property_id = $1 AND plate_number = $2 AND exited_at IS NULL)`, pid, plate).Scan(&open)
		if open {
			return apperr.Conflict("ALREADY_INSIDE", "Kendaraan "+plate+" masih tercatat di dalam; catat keluar dahulu")
		}
		var vid *uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT id FROM vehicles WHERE property_id = $1 AND plate_number = $2`, pid, plate).Scan(&vid)
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO parking_logs (organization_id, property_id, parking_area_id, vehicle_id, plate_number, gate, entry_by, note) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			p.OrganizationID, pid, in.ParkingAreaID, vid, plate, in.Gate, p.UserID, in.Note).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: "parking_entry", EntityType: "parking_log", EntityID: &id, EntityLabel: plate})
		l, err := scanParkingLog(tx.QueryRow(ctx, parkingLogSelect+` WHERE pl.id = $1`, id))
		out = l
		return err
	})
	return out, err
}

// RecordParkingExit: menutup log terbuka (by id atau plat).
func (s *Service) RecordParkingExit(ctx context.Context, logID *uuid.UUID, propertyID *uuid.UUID, plateNumber string, note *string) (*ParkingLog, error) {
	p := authctx.Must(ctx)
	var out *ParkingLog
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var id, pid uuid.UUID
		var exited *time.Time
		if logID != nil {
			if err := tx.QueryRow(ctx, `SELECT id, property_id, exited_at FROM parking_logs WHERE id = $1 FOR UPDATE`, *logID).Scan(&id, &pid, &exited); err != nil {
				return apperr.NotFound("Parking log")
			}
		} else {
			plate := NormalizePlate(plateNumber)
			if propertyID == nil || plate == "" {
				return apperr.Validation("property_id dan plate_number wajib")
			}
			if err := tx.QueryRow(ctx, `SELECT id, property_id, exited_at FROM parking_logs WHERE property_id = $1 AND plate_number = $2 AND exited_at IS NULL ORDER BY entered_at DESC LIMIT 1 FOR UPDATE`, *propertyID, plate).Scan(&id, &pid, &exited); err != nil {
				return apperr.NotFound("Kendaraan " + plate + " tidak tercatat di dalam")
			}
		}
		if err := parkingPerm(ctx, "record", pid); err != nil {
			return err
		}
		if exited != nil {
			return apperr.Conflict("ALREADY_EXITED", "Kendaraan sudah tercatat keluar")
		}
		if _, err := tx.Exec(ctx, `UPDATE parking_logs SET exited_at = now(), exit_by = $2, note = COALESCE($3, note) WHERE id = $1`, id, p.UserID, note); err != nil {
			return err
		}
		l, err := scanParkingLog(tx.QueryRow(ctx, parkingLogSelect+` WHERE pl.id = $1`, id))
		out = l
		return err
	})
	return out, err
}

type ParkingLogFilter struct {
	PropertyID    *uuid.UUID
	ParkingAreaID *uuid.UUID
	Inside        *bool
	Plate         string
	From, To      *time.Time
}

func (s *Service) ListParkingLogs(ctx context.Context, f ParkingLogFilter, page httpx.Page) ([]ParkingLog, *string, error) {
	p := authctx.Must(ctx)
	out := []ParkingLog{}
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE TRUE"
		if f.PropertyID != nil {
			if err := parkingPerm(ctx, "view", *f.PropertyID); err != nil {
				return err
			}
			where += " AND pl.property_id = " + add(*f.PropertyID)
		}
		where += " AND " + p.ScopeSQL("security.parking.view", "pl.property_id", "", add)
		if f.ParkingAreaID != nil {
			where += " AND pl.parking_area_id = " + add(*f.ParkingAreaID)
		}
		if f.Inside != nil {
			if *f.Inside {
				where += " AND pl.exited_at IS NULL"
			} else {
				where += " AND pl.exited_at IS NOT NULL"
			}
		}
		if np := NormalizePlate(f.Plate); np != "" {
			where += " AND pl.plate_number LIKE " + add("%"+np+"%")
		}
		if f.From != nil {
			where += " AND pl.entered_at >= " + add(*f.From)
		}
		if f.To != nil {
			where += " AND pl.entered_at < " + add(*f.To)
		}
		if page.Cursor != nil {
			where += " AND (pl.entered_at, pl.id) < (" + add(page.Cursor.Value) + "::timestamptz, " + add(page.Cursor.ID) + ")"
		}
		rows, err := tx.Query(ctx, parkingLogSelect+where+" ORDER BY pl.entered_at DESC, pl.id DESC LIMIT "+add(page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			l, err := scanParkingLog(rows)
			if err != nil {
				return err
			}
			out = append(out, *l)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.EnteredAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	return out, next, err
}

// ---------- Parking violations ----------

type ParkingViolation struct {
	ID              uuid.UUID              `json:"id"`
	PropertyID      uuid.UUID              `json:"property_id"`
	ViolationNumber string                 `json:"violation_number"`
	ParkingAreaID   *uuid.UUID             `json:"parking_area_id"`
	ParkingAreaName *string                `json:"parking_area_name"`
	Location        operations.LocationRef `json:"location"`
	VehicleID       *uuid.UUID             `json:"vehicle_id"`
	PlateNumber     string                 `json:"plate_number"`
	OwnerName       *string                `json:"owner_name"`
	ViolationType   string                 `json:"violation_type"`
	Description     *string                `json:"description"`
	ActionTaken     string                 `json:"action_taken"`
	Status          string                 `json:"status"` // open | resolved | escalated
	IncidentID      *uuid.UUID             `json:"incident_id"`
	IncidentNumber  *string                `json:"incident_number"`
	RecordedBy      *uuid.UUID             `json:"recorded_by"`
	RecordedByName  *string                `json:"recorded_by_name"`
	RecordedAt      time.Time              `json:"recorded_at"`
	ResolvedAt      *time.Time             `json:"resolved_at"`
	Resolution      *string                `json:"resolution"`
	AttachmentCount int                    `json:"attachment_count"`
	AllowedActions  []string               `json:"allowed_actions"`
	Version         int                    `json:"version"`
}

type ParkingViolationInput struct {
	PropertyID    *uuid.UUID `json:"property_id"`
	ParkingAreaID *uuid.UUID `json:"parking_area_id"`
	LocationID    *uuid.UUID `json:"location_id"`
	PlateNumber   *string    `json:"plate_number"`
	ViolationType *string    `json:"violation_type"`
	Description   *string    `json:"description"`
	ActionTaken   *string    `json:"action_taken"`
}

const violationSelect = `SELECT pv.id, pv.property_id, pv.violation_number, pv.parking_area_id, pa.name, pv.location_id, l.name, pv.vehicle_id, pv.plate_number, v.owner_name, pv.violation_type, pv.description,
	pv.action_taken, pv.status, pv.incident_id, i.incident_number, pv.recorded_by, u.full_name, pv.recorded_at, pv.resolved_at, pv.resolution,
	(SELECT count(*) FROM attachments x WHERE x.object_type = 'parking_violation' AND x.object_id = pv.id AND x.deleted_at IS NULL), pv.version
	FROM parking_violations pv LEFT JOIN parking_areas pa ON pa.id = pv.parking_area_id LEFT JOIN locations l ON l.id = pv.location_id LEFT JOIN vehicles v ON v.id = pv.vehicle_id
	LEFT JOIN incidents i ON i.id = pv.incident_id LEFT JOIN users u ON u.id = pv.recorded_by`

func (s *Service) scanViolation(ctx context.Context, tx pgx.Tx, row pgx.Row) (*ParkingViolation, error) {
	var v ParkingViolation
	if err := row.Scan(&v.ID, &v.PropertyID, &v.ViolationNumber, &v.ParkingAreaID, &v.ParkingAreaName, &v.Location.ID, &v.Location.Name, &v.VehicleID, &v.PlateNumber, &v.OwnerName, &v.ViolationType, &v.Description,
		&v.ActionTaken, &v.Status, &v.IncidentID, &v.IncidentNumber, &v.RecordedBy, &v.RecordedByName, &v.RecordedAt, &v.ResolvedAt, &v.Resolution, &v.AttachmentCount, &v.Version); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	v.AllowedActions = []string{"view"}
	if v.Status == "open" && p.HasAnyOnProperty("security.parking.manage", v.PropertyID) {
		v.AllowedActions = append(v.AllowedActions, "resolve", "update")
	}
	if v.Status == "open" && v.IncidentID == nil && (p.HasAnyOnProperty("operations.incidents.create", v.PropertyID) || p.HasAnyOnProperty("security.incidents.create", v.PropertyID)) {
		v.AllowedActions = append(v.AllowedActions, "create_incident")
	}
	if p.HasAnyOnProperty("operations.attachments.create", v.PropertyID) {
		v.AllowedActions = append(v.AllowedActions, "attach")
	}
	return &v, nil
}

func (s *Service) getViolationTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*ParkingViolation, error) {
	v, err := s.scanViolation(ctx, tx, tx.QueryRow(ctx, violationSelect+` WHERE pv.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Parking violation")
		}
		return nil, err
	}
	if v.Location.ID != nil {
		pt := property.LocationPathText(ctx, tx, *v.Location.ID)
		v.Location.PathText = &pt
	}
	return v, nil
}

func (s *Service) CreateParkingViolation(ctx context.Context, in ParkingViolationInput) (*ParkingViolation, error) {
	p := authctx.Must(ctx)
	if in.PlateNumber == nil || NormalizePlate(*in.PlateNumber) == "" {
		return nil, apperr.Validation("plate_number wajib").WithField("plate_number", "wajib")
	}
	if in.ViolationType != nil && !violationTypes[*in.ViolationType] {
		return nil, apperr.Validation("violation_type tidak valid").WithField("violation_type", "tidak valid")
	}
	if in.ActionTaken != nil && !violationActions[*in.ActionTaken] {
		return nil, apperr.Validation("action_taken harus none|warning|sticker|wheel_lock|towed|reported").WithField("action_taken", "tidak valid")
	}
	var out *ParkingViolation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pid uuid.UUID
		switch {
		case in.ParkingAreaID != nil:
			if err := tx.QueryRow(ctx, `SELECT property_id FROM parking_areas WHERE id = $1`, *in.ParkingAreaID).Scan(&pid); err != nil {
				return apperr.Validation("parking_area_id tidak ditemukan").WithField("parking_area_id", "tidak valid")
			}
		case in.LocationID != nil:
			lp, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
			if err != nil {
				return apperr.Validation("location_id tidak ditemukan").WithField("location_id", "tidak valid")
			}
			pid = lp
		case in.PropertyID != nil:
			pid = *in.PropertyID
		default:
			return apperr.Validation("property_id wajib")
		}
		if err := parkingPerm(ctx, "record", pid); err != nil {
			return err
		}
		plate := NormalizePlate(*in.PlateNumber)
		var vid *uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT id FROM vehicles WHERE property_id = $1 AND plate_number = $2`, pid, plate).Scan(&vid)
		loc := property.PropertyTimezone(ctx, tx, pid)
		number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixParkingViolation, time.Now(), loc)
		if err != nil {
			return err
		}
		vt, act := "other", "none"
		if in.ViolationType != nil {
			vt = *in.ViolationType
		}
		if in.ActionTaken != nil {
			act = *in.ActionTaken
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO parking_violations (organization_id, property_id, violation_number, parking_area_id, location_id, vehicle_id, plate_number, violation_type, description, action_taken, recorded_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11) RETURNING id`, p.OrganizationID, pid, number, in.ParkingAreaID, in.LocationID, vid, plate, vt, in.Description, act, p.UserID).Scan(&id); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "parking_violation", ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "plate": plate, "type": vt}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "parking_violation", EntityID: &id, EntityLabel: number + " " + plate})
		// PRD P3 v2.1 P3-PRK-03: pelanggaran kendaraan terdaftar → notifikasi pemilik (tenant) lewat resolver tenant_user
		if vid != nil && s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "parking_violation.recorded", OrganizationID: p.OrganizationID, PropertyID: &pid, ObjectType: "parking_violation", ObjectID: id, ObjectLabel: number, ActorUserID: &p.UserID,
				Payload: map[string]any{"plate": plate, "violation_type": vt, "domain": "security"}})
		}
		out, err = s.getViolationTx(ctx, tx, id)
		return err
	})
	return out, err
}

type ViolationActionInput struct {
	Resolution  string  `json:"resolution"`
	ActionTaken *string `json:"action_taken"`
	Description *string `json:"description"`
	// create_incident
	Severity *string `json:"severity"`
	Title    *string `json:"title"`
}

// ActParkingViolation: update | resolve | create_incident (pelanggaran → incident, P2-PRK-04).
func (s *Service) ActParkingViolation(ctx context.Context, id uuid.UUID, action string, in ViolationActionInput) (*ParkingViolation, error) {
	p := authctx.Must(ctx)
	var out *ParkingViolation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		v, err := s.getViolationTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !contains(v.AllowedActions, action) {
			if v.Status != "open" {
				return apperr.Conflict("OBJECT_TERMINAL", "Pelanggaran sudah "+v.Status)
			}
			return apperr.Forbidden("Aksi " + action + " tidak diizinkan")
		}
		switch action {
		case "update":
			if in.ActionTaken != nil && !violationActions[*in.ActionTaken] {
				return apperr.Validation("action_taken tidak valid").WithField("action_taken", "tidak valid")
			}
			if _, err := tx.Exec(ctx, `UPDATE parking_violations SET action_taken = COALESCE($2, action_taken), description = COALESCE($3, description), updated_by = $4 WHERE id = $1`, id, in.ActionTaken, in.Description, p.UserID); err != nil {
				return err
			}
		case "resolve":
			if strings.TrimSpace(in.Resolution) == "" {
				return apperr.Validation("resolution wajib").WithField("resolution", "wajib")
			}
			if _, err := tx.Exec(ctx, `UPDATE parking_violations SET status = 'resolved', resolution = $2, resolved_at = now(), action_taken = COALESCE($3, action_taken), updated_by = $4 WHERE id = $1`, id, in.Resolution, in.ActionTaken, p.UserID); err != nil {
				return err
			}
		case "create_incident":
			sev := "low"
			if in.Severity != nil {
				sev = *in.Severity
			}
			title := "Pelanggaran parkir " + v.PlateNumber
			if in.Title != nil && strings.TrimSpace(*in.Title) != "" {
				title = *in.Title
			}
			desc := fmt.Sprintf("Dari pelanggaran parkir %s (%s).", v.ViolationNumber, v.ViolationType)
			if v.Description != nil {
				desc += " " + *v.Description
			}
			st := "parking_violation"
			incID, err := s.Ops.CreateIncidentTx(ctx, tx, operations.CreateIncidentInput{PropertyID: &v.PropertyID, IncidentType: "security", Category: "unauthorized_access", Title: title, Description: &desc,
				LocationID: v.Location.ID, Severity: sev, SourceType: &st, SourceID: &id})
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE parking_violations SET status = 'escalated', incident_id = $2, updated_by = $3 WHERE id = $1`, id, incID, p.UserID); err != nil {
				return err
			}
		default:
			return apperr.Validation("aksi tidak dikenal")
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "parking_violation", ObjectID: id, Action: audit.ActStatusChanged, From: v.Status, To: action, Payload: map[string]any{"action": action, "resolution": in.Resolution}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "parking_violation", EntityID: &id, EntityLabel: v.ViolationNumber, After: map[string]any{"action": action}})
		out, err = s.getViolationTx(ctx, tx, id)
		return err
	})
	return out, err
}

type ViolationFilter struct {
	PropertyID *uuid.UUID
	Statuses   []string
	Plate      string
	From, To   *time.Time
}

func (s *Service) ListParkingViolations(ctx context.Context, f ViolationFilter, page httpx.Page) ([]ParkingViolation, *string, error) {
	p := authctx.Must(ctx)
	out := []ParkingViolation{}
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE TRUE"
		if f.PropertyID != nil {
			if err := parkingPerm(ctx, "view", *f.PropertyID); err != nil {
				return err
			}
			where += " AND pv.property_id = " + add(*f.PropertyID)
		}
		where += " AND " + p.ScopeSQL("security.parking.view", "pv.property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = pv.location_id)", add)
		if len(f.Statuses) > 0 {
			where += " AND pv.status = ANY(" + add(f.Statuses) + ")"
		}
		if np := NormalizePlate(f.Plate); np != "" {
			where += " AND pv.plate_number LIKE " + add("%"+np+"%")
		}
		if f.From != nil {
			where += " AND pv.recorded_at >= " + add(*f.From)
		}
		if f.To != nil {
			where += " AND pv.recorded_at < " + add(*f.To)
		}
		if page.Cursor != nil {
			where += " AND (pv.recorded_at, pv.id) < (" + add(page.Cursor.Value) + "::timestamptz, " + add(page.Cursor.ID) + ")"
		}
		rows, err := tx.Query(ctx, violationSelect+where+" ORDER BY pv.recorded_at DESC, pv.id DESC LIMIT "+add(page.Limit+1), args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			v, err := s.scanViolation(ctx, tx, rows)
			if err != nil {
				rows.Close()
				return err
			}
			out = append(out, *v)
		}
		rows.Close()
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.RecordedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return nil
	})
	return out, next, err
}

func (s *Service) GetParkingViolation(ctx context.Context, id uuid.UUID) (*ParkingViolation, error) {
	var out *ParkingViolation
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		v, err := s.getViolationTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := parkingPerm(ctx, "view", v.PropertyID); err != nil {
			return err
		}
		out = v
		return nil
	})
	return out, err
}
