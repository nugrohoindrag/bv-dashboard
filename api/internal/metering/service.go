// Package metering: meter listrik & air, tarif utilitas, dan pembacaan meter (PRD P4 v2.1 §5.3 P4-UTL-01..05; Roadmap v2.1
// §12 Utility/Electricity/Water). Pembacaan dari Web atau Staff App (mutasi offline record_meter_reading) dengan foto; angka
// mundur / lonjakan pemakaian ditandai (flagged) untuk direview sebelum ditagihkan. Pemakaian → item invoice lewat billing rule
// meter_usage (package billing). Pembacaan otomatis (smart meter) = P7 (di luar scope).
package metering

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/property"
)

type Service struct {
	DB          *db.DB
	Jobs        jobs.Enqueuer
	Attachments *attachments.Service
	// SpikeFactor: pemakaian > faktor × rata-rata 3 periode sebelumnya → flagged spike (default 3).
	SpikeFactor float64
}

func New(d *db.DB, j jobs.Enqueuer, att *attachments.Service) *Service {
	return &Service{DB: d, Jobs: j, Attachments: att, SpikeFactor: 3}
}

var meterTypes = map[string]string{"electricity": "kWh", "water": "m³"}

// ---------- Tarif utilitas (P4-UTL-04) ----------

// Block: tarif blok progresif — up_to nil = sisa pemakaian.
type Block struct {
	UpTo *float64 `json:"up_to"`
	Rate float64  `json:"rate"`
}

type Tariff struct {
	ID            uuid.UUID `json:"id"`
	PropertyID    uuid.UUID `json:"property_id"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	MeterType     string    `json:"meter_type"`
	Rate          float64   `json:"rate"`
	Blocks        []Block   `json:"blocks"`
	FixedCharge   int64     `json:"fixed_charge"`
	MinimumCharge int64     `json:"minimum_charge"`
	UnitLabel     string    `json:"unit_label"`
	IsActive      bool      `json:"is_active"`
	EffectiveFrom *string   `json:"effective_from"`
	Notes         *string   `json:"notes"`
	Version       int       `json:"version"`
}

const tariffSelect = `SELECT id, property_id, code, name, meter_type, rate::float8, blocks, fixed_charge, minimum_charge, unit_label, is_active, to_char(effective_from,'YYYY-MM-DD'), notes, version FROM utility_tariffs`

func scanTariff(row pgx.Row) (*Tariff, error) {
	var t Tariff
	var blocks []byte
	if err := row.Scan(&t.ID, &t.PropertyID, &t.Code, &t.Name, &t.MeterType, &t.Rate, &blocks, &t.FixedCharge, &t.MinimumCharge, &t.UnitLabel, &t.IsActive, &t.EffectiveFrom, &t.Notes, &t.Version); err != nil {
		return nil, err
	}
	t.Blocks = []Block{}
	_ = json.Unmarshal(blocks, &t.Blocks)
	return &t, nil
}

// UsageCharge: biaya pemakaian dengan tarif blok (jika ada) + biaya beban, minimal minimum_charge. Dibulatkan ke rupiah.
func (t *Tariff) UsageCharge(usage float64) (energy int64, fixed int64, total int64) {
	if usage < 0 {
		usage = 0
	}
	var cost float64
	if len(t.Blocks) > 0 {
		rest, prev := usage, 0.0
		for _, b := range t.Blocks {
			if rest <= 0 {
				break
			}
			span := rest
			if b.UpTo != nil {
				span = math.Min(rest, *b.UpTo-prev)
				prev = *b.UpTo
			}
			if span < 0 {
				span = 0
			}
			cost += span * b.Rate
			rest -= span
		}
	} else {
		cost = usage * t.Rate
	}
	energy = int64(math.Round(cost))
	fixed = t.FixedCharge
	total = energy + fixed
	if total < t.MinimumCharge {
		total = t.MinimumCharge
	}
	return
}

func (s *Service) TariffTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Tariff, error) {
	t, err := scanTariff(tx.QueryRow(ctx, tariffSelect+` WHERE id = $1`, id))
	if err != nil {
		return nil, apperr.NotFound("Tarif")
	}
	return t, nil
}

func (s *Service) ListTariffs(ctx context.Context, propertyID *uuid.UUID) ([]Tariff, error) {
	p := authctx.Must(ctx)
	out := []Tariff{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args := " WHERE true", []any{}
		if propertyID != nil {
			if !p.HasAnyOnProperty("billing.meters.view", *propertyID) {
				return apperr.Forbidden("")
			}
			args = append(args, *propertyID)
			where += " AND property_id = $1"
		} else if pids, all := p.PropertyIDsFor("billing.meters.view"); !all {
			args = append(args, pids)
			where += " AND property_id = ANY($1)"
		}
		rows, err := tx.Query(ctx, tariffSelect+where+` ORDER BY meter_type, code`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTariff(rows)
			if err != nil {
				return err
			}
			out = append(out, *t)
		}
		return rows.Err()
	})
	return out, err
}

type TariffInput struct {
	PropertyID    *uuid.UUID `json:"property_id"`
	Code          *string    `json:"code"`
	Name          *string    `json:"name"`
	MeterType     *string    `json:"meter_type"`
	Rate          *float64   `json:"rate"`
	Blocks        *[]Block   `json:"blocks"`
	FixedCharge   *int64     `json:"fixed_charge"`
	MinimumCharge *int64     `json:"minimum_charge"`
	UnitLabel     *string    `json:"unit_label"`
	IsActive      *bool      `json:"is_active"`
	EffectiveFrom *string    `json:"effective_from"`
	Notes         *string    `json:"notes"`
}

func validateBlocks(bs []Block) error {
	prev := 0.0
	for i, b := range bs {
		if b.Rate < 0 {
			return apperr.Validation("blocks.rate tidak boleh negatif")
		}
		if b.UpTo == nil {
			if i != len(bs)-1 {
				return apperr.Validation("blok tanpa up_to harus blok terakhir")
			}
			continue
		}
		if *b.UpTo <= prev {
			return apperr.Validation("blocks.up_to harus naik")
		}
		prev = *b.UpTo
	}
	return nil
}

func (s *Service) SaveTariff(ctx context.Context, id *uuid.UUID, in TariffInput) (*Tariff, error) {
	p := authctx.Must(ctx)
	var out *Tariff
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pid uuid.UUID
		if id != nil {
			cur, err := s.TariffTx(ctx, tx, *id)
			if err != nil {
				return err
			}
			pid = cur.PropertyID
		} else {
			if in.PropertyID == nil {
				return apperr.Validation("property_id wajib")
			}
			pid = *in.PropertyID
		}
		if !p.HasOnProperty("billing.meters.manage", pid) {
			return apperr.Forbidden("Memerlukan billing.meters.manage")
		}
		if in.MeterType != nil {
			if _, ok := meterTypes[*in.MeterType]; !ok {
				return apperr.Validation("meter_type harus electricity|water").WithField("meter_type", "tidak valid")
			}
		}
		if in.Blocks != nil {
			if err := validateBlocks(*in.Blocks); err != nil {
				return err
			}
		}
		var blocks []byte
		if in.Blocks != nil {
			blocks, _ = json.Marshal(*in.Blocks)
		}
		var tid uuid.UUID
		if id == nil {
			if strings.TrimSpace(deref(in.Code)) == "" || strings.TrimSpace(deref(in.Name)) == "" || in.MeterType == nil {
				return apperr.Validation("code, name, meter_type wajib")
			}
			unit := deref(in.UnitLabel)
			if unit == "" {
				unit = meterTypes[*in.MeterType]
			}
			if blocks == nil {
				blocks = []byte("[]")
			}
			if err := tx.QueryRow(ctx, `INSERT INTO utility_tariffs (organization_id, property_id, code, name, meter_type, rate, blocks, fixed_charge, minimum_charge, unit_label, is_active, effective_from, notes, created_by, updated_by)
				VALUES ($1,$2,$3,$4,$5,COALESCE($6,0),$7,COALESCE($8,0),COALESCE($9,0),$10,COALESCE($11,true),$12,$13,$14,$14) RETURNING id`,
				p.OrganizationID, pid, strings.TrimSpace(*in.Code), strings.TrimSpace(*in.Name), *in.MeterType, in.Rate, blocks, in.FixedCharge, in.MinimumCharge, unit, in.IsActive, dateOrNil(in.EffectiveFrom), in.Notes, p.UserID).Scan(&tid); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("TARIFF_CODE_EXISTS", "Kode tarif sudah dipakai")
				}
				return err
			}
		} else {
			tid = *id
			if _, err := tx.Exec(ctx, `UPDATE utility_tariffs SET name = COALESCE(NULLIF(TRIM($2),''), name), rate = COALESCE($3, rate), blocks = COALESCE($4, blocks), fixed_charge = COALESCE($5, fixed_charge),
				minimum_charge = COALESCE($6, minimum_charge), unit_label = COALESCE(NULLIF($7,''), unit_label), is_active = COALESCE($8, is_active), effective_from = COALESCE($9, effective_from), notes = COALESCE($10, notes), updated_by = $11 WHERE id = $1`,
				tid, deref(in.Name), in.Rate, blocks, in.FixedCharge, in.MinimumCharge, deref(in.UnitLabel), in.IsActive, dateOrNil(in.EffectiveFrom), in.Notes, p.UserID); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditConfigChange, EntityType: "utility_tariff", EntityID: &tid, EntityLabel: deref(in.Code), After: in})
		var err error
		out, err = s.TariffTx(ctx, tx, tid)
		return err
	})
	return out, err
}

// ---------- Meter (P4-UTL-01) ----------

type LastReading struct {
	ID     uuid.UUID `json:"id"`
	Value  float64   `json:"value"`
	Usage  *float64  `json:"usage"`
	Period string    `json:"period"`
	ReadAt time.Time `json:"read_at"`
	Status string    `json:"status"`
}

type Meter struct {
	ID             uuid.UUID    `json:"id"`
	PropertyID     uuid.UUID    `json:"property_id"`
	LocationID     uuid.UUID    `json:"location_id"`
	LocationName   string       `json:"location_name"`
	LocationPath   string       `json:"location_path"`
	UnitNumber     *string      `json:"unit_number"`
	TenantName     *string      `json:"tenant_name"`
	MeterType      string       `json:"meter_type"`
	MeterNumber    string       `json:"meter_number"`
	Multiplier     float64      `json:"multiplier"`
	InitialReading float64      `json:"initial_reading"`
	TariffID       *uuid.UUID   `json:"tariff_id"`
	TariffName     *string      `json:"tariff_name"`
	UnitLabel      string       `json:"unit_label"`
	Status         string       `json:"status"`
	InstalledOn    *string      `json:"installed_on"`
	Notes          *string      `json:"notes"`
	LastReading    *LastReading `json:"last_reading"`
	ReadThisPeriod bool         `json:"read_this_period"`
	Version        int          `json:"version"`
}

const meterSelect = `SELECT m.id, m.property_id, m.location_id, l.name, COALESCE((SELECT string_agg(a.name, ' · ' ORDER BY a.depth) FROM locations a WHERE a.path @> l.path AND a.depth > 0), l.name),
	un.unit_number, t.name, m.meter_type, m.meter_number, m.multiplier::float8, m.initial_reading::float8, m.tariff_id, ut.name, m.status, to_char(m.installed_on,'YYYY-MM-DD'), m.notes, m.version,
	lr.id, lr.reading_value::float8, lr.usage::float8, lr.period, lr.read_at, lr.status
	FROM meters m JOIN locations l ON l.id = m.location_id LEFT JOIN units un ON un.location_id = m.location_id LEFT JOIN tenants t ON t.id = un.tenant_id
	LEFT JOIN utility_tariffs ut ON ut.id = m.tariff_id
	LEFT JOIN LATERAL (SELECT r.id, r.reading_value, r.usage, r.period, r.read_at, r.status FROM meter_readings r WHERE r.meter_id = m.id AND r.status <> 'rejected' ORDER BY r.read_at DESC LIMIT 1) lr ON true`

func scanMeter(row pgx.Row, period string) (*Meter, error) {
	var m Meter
	var lrID *uuid.UUID
	var lrVal, lrUsage *float64
	var lrPeriod, lrStatus *string
	var lrAt *time.Time
	if err := row.Scan(&m.ID, &m.PropertyID, &m.LocationID, &m.LocationName, &m.LocationPath, &m.UnitNumber, &m.TenantName, &m.MeterType, &m.MeterNumber, &m.Multiplier, &m.InitialReading,
		&m.TariffID, &m.TariffName, &m.Status, &m.InstalledOn, &m.Notes, &m.Version, &lrID, &lrVal, &lrUsage, &lrPeriod, &lrAt, &lrStatus); err != nil {
		return nil, err
	}
	m.UnitLabel = meterTypes[m.MeterType]
	if lrID != nil {
		m.LastReading = &LastReading{ID: *lrID, Value: *lrVal, Usage: lrUsage, Period: *lrPeriod, ReadAt: *lrAt, Status: *lrStatus}
		m.ReadThisPeriod = *lrPeriod == period
	}
	return &m, nil
}

type MeterFilter struct {
	PropertyID *uuid.UUID
	LocationID *uuid.UUID // subtree (building/floor/unit)
	MeterType  string
	Status     string
	Unread     bool // belum dibaca periode berjalan
	Q          string
}

func currentPeriod(loc *time.Location) string { return time.Now().In(loc).Format("2006-01") }

func (s *Service) ListMeters(ctx context.Context, f MeterFilter, page httpx.Page) ([]Meter, *string, error) {
	p := authctx.Must(ctx)
	var out []Meter
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var args []any
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE true"
		loc, _ := time.LoadLocation("Asia/Jakarta")
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("billing.meters.view", *f.PropertyID) {
				return apperr.Forbidden("")
			}
			where += " AND m.property_id = " + add(*f.PropertyID)
			loc = property.PropertyTimezone(ctx, tx, *f.PropertyID)
		}
		where += " AND " + p.ScopeSQL("billing.meters.view", "m.property_id", "l.path", add)
		if f.LocationID != nil {
			where += " AND l.path <@ (SELECT fl.path FROM locations fl WHERE fl.id = " + add(*f.LocationID) + ")"
		}
		if f.MeterType != "" {
			where += " AND m.meter_type = " + add(f.MeterType)
		}
		if f.Status != "" {
			where += " AND m.status = " + add(f.Status)
		} else {
			where += " AND m.status = 'active'"
		}
		period := currentPeriod(loc)
		if f.Unread {
			where += " AND NOT EXISTS (SELECT 1 FROM meter_readings r WHERE r.meter_id = m.id AND r.period = " + add(period) + " AND r.status <> 'rejected')"
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			a := add("%" + q + "%")
			where += " AND (m.meter_number ILIKE " + a + " OR un.unit_number ILIKE " + a + " OR l.name ILIKE " + a + ")"
		}
		if page.Cursor != nil {
			c1, c2 := add(page.Cursor.Value), add(page.Cursor.ID)
			where += " AND (m.meter_number, m.id) > (" + c1 + ", " + c2 + ")"
		}
		rows, err := tx.Query(ctx, meterSelect+where+fmt.Sprintf(" ORDER BY m.meter_number, m.id LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMeter(rows, period)
			if err != nil {
				return err
			}
			out = append(out, *m)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.MeterNumber, last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	if out == nil {
		out = []Meter{}
	}
	return out, next, err
}

func (s *Service) meterTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Meter, error) {
	var pid uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT property_id FROM meters WHERE id = $1`, id).Scan(&pid); err != nil {
		return nil, apperr.NotFound("Meter")
	}
	// periode dihitung dulu: query timezone tidak boleh berjalan saat baris QueryRow masih terbuka (conn busy)
	period := currentPeriod(property.PropertyTimezone(ctx, tx, pid))
	m, err := scanMeter(tx.QueryRow(ctx, meterSelect+` WHERE m.id = $1`, id), period)
	if err != nil {
		return nil, apperr.NotFound("Meter")
	}
	return m, nil
}

func (s *Service) GetMeter(ctx context.Context, id uuid.UUID) (*Meter, error) {
	var out *Meter
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		m, err := s.meterTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasAnyOnProperty("billing.meters.view", m.PropertyID) {
			return apperr.Forbidden("")
		}
		out = m
		return nil
	})
	return out, err
}

type MeterInput struct {
	LocationID     *uuid.UUID `json:"location_id"`
	MeterType      *string    `json:"meter_type"`
	MeterNumber    *string    `json:"meter_number"`
	Multiplier     *float64   `json:"multiplier"`
	InitialReading *float64   `json:"initial_reading"`
	TariffID       *uuid.UUID `json:"tariff_id"` // UUID nol = pakai tarif billing rule
	Status         *string    `json:"status"`
	InstalledOn    *string    `json:"installed_on"`
	Notes          *string    `json:"notes"`
}

func (s *Service) SaveMeter(ctx context.Context, id *uuid.UUID, in MeterInput) (*Meter, error) {
	p := authctx.Must(ctx)
	var out *Meter
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pid uuid.UUID
		if id != nil {
			cur, err := s.meterTx(ctx, tx, *id)
			if err != nil {
				return err
			}
			pid = cur.PropertyID
		} else {
			if in.LocationID == nil || in.MeterType == nil || strings.TrimSpace(deref(in.MeterNumber)) == "" {
				return apperr.Validation("location_id, meter_type, meter_number wajib")
			}
			lp, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
			if err != nil {
				return apperr.Validation("location_id tidak ditemukan").WithField("location_id", "tidak valid")
			}
			pid = lp
		}
		if !p.HasOnProperty("billing.meters.manage", pid) {
			return apperr.Forbidden("Memerlukan billing.meters.manage")
		}
		if in.MeterType != nil {
			if _, ok := meterTypes[*in.MeterType]; !ok {
				return apperr.Validation("meter_type harus electricity|water").WithField("meter_type", "tidak valid")
			}
		}
		if in.Multiplier != nil && *in.Multiplier <= 0 {
			return apperr.Validation("multiplier harus > 0")
		}
		if in.Status != nil && *in.Status != "active" && *in.Status != "inactive" && *in.Status != "replaced" {
			return apperr.Validation("status harus active|inactive|replaced")
		}
		clearTariff := in.TariffID != nil && *in.TariffID == uuid.Nil
		tariff := in.TariffID
		if clearTariff {
			tariff = nil
		}
		if tariff != nil {
			t, err := s.TariffTx(ctx, tx, *tariff)
			if err != nil || t.PropertyID != pid {
				return apperr.Validation("tariff_id tidak ditemukan di property ini").WithField("tariff_id", "tidak valid")
			}
		}
		var mid uuid.UUID
		if id == nil {
			if err := tx.QueryRow(ctx, `INSERT INTO meters (organization_id, property_id, location_id, meter_type, meter_number, multiplier, initial_reading, tariff_id, installed_on, notes, created_by, updated_by)
				VALUES ($1,$2,$3,$4,$5,COALESCE($6,1),COALESCE($7,0),$8,$9,$10,$11,$11) RETURNING id`,
				p.OrganizationID, pid, *in.LocationID, *in.MeterType, strings.TrimSpace(*in.MeterNumber), in.Multiplier, in.InitialReading, tariff, dateOrNil(in.InstalledOn), in.Notes, p.UserID).Scan(&mid); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("METER_EXISTS", "Nomor meter sudah terdaftar di property ini")
				}
				return err
			}
		} else {
			mid = *id
			if _, err := tx.Exec(ctx, `UPDATE meters SET meter_number = COALESCE(NULLIF(TRIM($2),''), meter_number), multiplier = COALESCE($3, multiplier), initial_reading = COALESCE($4, initial_reading),
				tariff_id = CASE WHEN $5 THEN NULL ELSE COALESCE($6, tariff_id) END, status = COALESCE($7, status), installed_on = COALESCE($8, installed_on), notes = COALESCE($9, notes), updated_by = $10 WHERE id = $1`,
				mid, deref(in.MeterNumber), in.Multiplier, in.InitialReading, clearTariff, tariff, in.Status, dateOrNil(in.InstalledOn), in.Notes, p.UserID); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditConfigChange, EntityType: "meter", EntityID: &mid, EntityLabel: deref(in.MeterNumber), After: in})
		var err error
		out, err = s.meterTx(ctx, tx, mid)
		return err
	})
	return out, err
}

// ---------- Pembacaan meter (P4-UTL-02..03) ----------

type Reading struct {
	ID             uuid.UUID  `json:"id"`
	MeterID        uuid.UUID  `json:"meter_id"`
	MeterNumber    string     `json:"meter_number"`
	MeterType      string     `json:"meter_type"`
	PropertyID     uuid.UUID  `json:"property_id"`
	LocationName   string     `json:"location_name"`
	UnitNumber     *string    `json:"unit_number"`
	ReadingValue   float64    `json:"reading_value"`
	PreviousValue  *float64   `json:"previous_value"`
	Usage          *float64   `json:"usage"`
	UnitLabel      string     `json:"unit_label"`
	ReadAt         time.Time  `json:"read_at"`
	Period         string     `json:"period"`
	Source         string     `json:"source"`
	Status         string     `json:"status"`
	Anomaly        *string    `json:"anomaly"`
	Notes          *string    `json:"notes"`
	RecordedByName *string    `json:"recorded_by_name"`
	ReviewedByName *string    `json:"reviewed_by_name"`
	ReviewedAt     *time.Time `json:"reviewed_at"`
	ReviewNote     *string    `json:"review_note"`
	Billed         bool       `json:"billed"`
	PhotoURL       string     `json:"photo_url,omitempty"`
	PhotoCount     int        `json:"photo_count"`
	AllowedActions []string   `json:"allowed_actions"`
	Version        int        `json:"version"`
}

const readingSelect = `SELECT r.id, r.meter_id, m.meter_number, m.meter_type, r.property_id, l.name, un.unit_number, r.reading_value::float8, r.previous_value::float8, r.usage::float8,
	r.read_at, r.period, r.source, r.status, r.anomaly, r.notes, rb.full_name, vb.full_name, r.reviewed_at, r.review_note,
	EXISTS (SELECT 1 FROM invoice_items it JOIN invoices i ON i.id = it.invoice_id WHERE it.meter_reading_id = r.id AND i.status <> 'cancelled'),
	(SELECT count(*) FROM attachments a WHERE a.object_type = 'meter_reading' AND a.object_id = r.id AND a.deleted_at IS NULL), r.version
	FROM meter_readings r JOIN meters m ON m.id = r.meter_id JOIN locations l ON l.id = m.location_id LEFT JOIN units un ON un.location_id = m.location_id
	LEFT JOIN users rb ON rb.id = r.recorded_by LEFT JOIN users vb ON vb.id = r.reviewed_by`

func scanReading(row pgx.Row) (*Reading, error) {
	var r Reading
	if err := row.Scan(&r.ID, &r.MeterID, &r.MeterNumber, &r.MeterType, &r.PropertyID, &r.LocationName, &r.UnitNumber, &r.ReadingValue, &r.PreviousValue, &r.Usage,
		&r.ReadAt, &r.Period, &r.Source, &r.Status, &r.Anomaly, &r.Notes, &r.RecordedByName, &r.ReviewedByName, &r.ReviewedAt, &r.ReviewNote, &r.Billed, &r.PhotoCount, &r.Version); err != nil {
		return nil, err
	}
	r.UnitLabel = meterTypes[r.MeterType]
	return &r, nil
}

func (s *Service) readingActions(ctx context.Context, r *Reading) {
	p := authctx.Must(ctx)
	r.AllowedActions = []string{"view"}
	if !r.Billed && p.HasOnProperty("billing.meter_readings.review", r.PropertyID) {
		switch r.Status {
		case "flagged", "recorded":
			r.AllowedActions = append(r.AllowedActions, "approve", "reject")
		case "approved":
			r.AllowedActions = append(r.AllowedActions, "reject")
		}
	}
}

type ReadingInput struct {
	ReadingValue float64    `json:"reading_value"`
	ReadAt       *time.Time `json:"read_at"`
	Notes        *string    `json:"notes"`
	// ClientRef: id mutasi offline (idempotensi Staff App); ClientAttachmentID: foto offline yang dipindah ke pembacaan
	ClientRef          *uuid.UUID `json:"client_ref"`
	ClientAttachmentID *string    `json:"client_attachment_id"`
	Source             string     `json:"-"`
}

// RecordTx: catat pembacaan — pemakaian = (angka − angka sebelumnya) × pengali; angka mundur = flagged rollback, pemakaian >
// SpikeFactor × rata-rata 3 pembacaan sebelumnya = flagged spike (P4-UTL-02). Idempoten atas client_ref.
func (s *Service) RecordTx(ctx context.Context, tx pgx.Tx, meterID uuid.UUID, in ReadingInput) (*Reading, bool, error) {
	p := authctx.Must(ctx)
	if in.ClientRef != nil {
		var existing uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM meter_readings WHERE client_ref = $1`, *in.ClientRef).Scan(&existing); err == nil {
			r, err := s.readingTx(ctx, tx, existing)
			return r, false, err
		}
	}
	m, err := s.meterTx(ctx, tx, meterID)
	if err != nil {
		return nil, false, err
	}
	if !p.HasAnyOnProperty("billing.meter_readings.create", m.PropertyID) {
		return nil, false, apperr.Forbidden("Memerlukan billing.meter_readings.create")
	}
	if m.Status != "active" {
		return nil, false, apperr.Conflict("METER_INACTIVE", "Meter tidak aktif")
	}
	if in.ReadingValue < 0 || math.IsNaN(in.ReadingValue) || math.IsInf(in.ReadingValue, 0) {
		return nil, false, apperr.Validation("reading_value harus ≥ 0").WithField("reading_value", "tidak valid")
	}
	readAt := time.Now().UTC()
	if in.ReadAt != nil {
		if in.ReadAt.After(time.Now().Add(10 * time.Minute)) {
			return nil, false, apperr.Validation("read_at tidak boleh di masa depan").WithField("read_at", "masa depan")
		}
		readAt = in.ReadAt.UTC()
	}
	loc := property.PropertyTimezone(ctx, tx, m.PropertyID)
	period := readAt.In(loc).Format("2006-01")
	prev := m.InitialReading
	var prevVal *float64
	if err := tx.QueryRow(ctx, `SELECT reading_value::float8 FROM meter_readings WHERE meter_id = $1 AND status <> 'rejected' AND read_at < $2 ORDER BY read_at DESC LIMIT 1`, meterID, readAt).Scan(&prevVal); err == nil && prevVal != nil {
		prev = *prevVal
	}
	usage := math.Round((in.ReadingValue-prev)*m.Multiplier*1000) / 1000
	status, anomaly := "recorded", ""
	if in.ReadingValue < prev {
		status, anomaly = "flagged", "rollback"
		usage = 0
	} else {
		var avg *float64
		_ = tx.QueryRow(ctx, `SELECT avg(usage)::float8 FROM (SELECT usage FROM meter_readings WHERE meter_id = $1 AND status IN ('recorded','approved') AND usage IS NOT NULL AND usage > 0 AND read_at < $2 ORDER BY read_at DESC LIMIT 3) x`, meterID, readAt).Scan(&avg)
		if avg != nil && *avg > 0 && usage > s.SpikeFactor**avg {
			status, anomaly = "flagged", "spike"
		}
	}
	source := in.Source
	if source == "" {
		source = "web"
	}
	var id uuid.UUID
	var an any
	if anomaly != "" {
		an = anomaly
	}
	if err := tx.QueryRow(ctx, `INSERT INTO meter_readings (organization_id, property_id, meter_id, reading_value, previous_value, usage, read_at, period, source, status, anomaly, notes, recorded_by, client_ref)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, p.OrganizationID, m.PropertyID, meterID, in.ReadingValue, prev, usage, readAt, period, source, status, an, in.Notes, actorOrNil(p), in.ClientRef).Scan(&id); err != nil {
		return nil, false, err
	}
	// foto offline Staff App: dipasang ke object meter → dipindah ke pembacaan (pola add_finding P2)
	if in.ClientAttachmentID != nil && *in.ClientAttachmentID != "" {
		_, _ = tx.Exec(ctx, `UPDATE attachments SET object_type = 'meter_reading', object_id = $1 WHERE client_attachment_id = $2 AND uploaded_by = $3 AND object_type IN ('meter','meter_reading') AND deleted_at IS NULL`, id, *in.ClientAttachmentID, p.UserID)
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "meter", ObjectID: meterID, Action: "reading_recorded", Payload: map[string]any{"reading_id": id, "value": in.ReadingValue, "usage": usage, "status": status, "anomaly": anomaly}})
	if status == "flagged" && s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "meter_reading.flagged", OrganizationID: p.OrganizationID, PropertyID: &m.PropertyID, ObjectType: "meter_reading", ObjectID: id, ObjectLabel: m.MeterNumber,
			Payload: map[string]any{"domain": "finance", "anomaly": anomaly, "location": m.LocationPath}})
	}
	r, err := s.readingTx(ctx, tx, id)
	return r, true, err
}

func (s *Service) readingTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Reading, error) {
	r, err := scanReading(tx.QueryRow(ctx, readingSelect+` WHERE r.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Pembacaan meter")
		}
		return nil, err
	}
	s.readingActions(ctx, r)
	return r, nil
}

func (s *Service) Record(ctx context.Context, meterID uuid.UUID, in ReadingInput) (*Reading, error) {
	var out *Reading
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, _, err = s.RecordTx(ctx, tx, meterID, in)
		return err
	})
	return out, err
}

type ReadingFilter struct {
	PropertyID *uuid.UUID
	MeterID    *uuid.UUID
	Period     string
	Statuses   []string
	MeterType  string
}

func (s *Service) ListReadings(ctx context.Context, f ReadingFilter, page httpx.Page) ([]Reading, *string, error) {
	p := authctx.Must(ctx)
	var out []Reading
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var args []any
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE true"
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("billing.meter_readings.view", *f.PropertyID) {
				return apperr.Forbidden("")
			}
			where += " AND r.property_id = " + add(*f.PropertyID)
		}
		where += " AND " + p.ScopeSQL("billing.meter_readings.view", "r.property_id", "l.path", add)
		if f.MeterID != nil {
			where += " AND r.meter_id = " + add(*f.MeterID)
		}
		if f.Period != "" {
			where += " AND r.period = " + add(f.Period)
		}
		if len(f.Statuses) > 0 {
			where += " AND r.status = ANY(" + add(f.Statuses) + ")"
		}
		if f.MeterType != "" {
			where += " AND m.meter_type = " + add(f.MeterType)
		}
		if page.Cursor != nil {
			c1, c2 := add(page.Cursor.Value), add(page.Cursor.ID)
			where += " AND (r.read_at, r.id) < (" + c1 + "::timestamptz, " + c2 + ")"
		}
		rows, err := tx.Query(ctx, readingSelect+where+fmt.Sprintf(" ORDER BY r.read_at DESC, r.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanReading(rows)
			if err != nil {
				return err
			}
			s.readingActions(ctx, r)
			out = append(out, *r)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.ReadAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	if out == nil {
		out = []Reading{}
	}
	return out, next, err
}

func (s *Service) GetReading(ctx context.Context, id uuid.UUID) (*Reading, error) {
	var out *Reading
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.readingTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasAnyOnProperty("billing.meter_readings.view", r.PropertyID) {
			return apperr.Forbidden("")
		}
		if s.Attachments != nil {
			if list, err := attachments.ListForObjectTx(ctx, tx, "meter_reading", id); err == nil && len(list) > 0 {
				s.Attachments.FillURLs(ctx, list)
				r.PhotoURL = list[0].URL
			}
		}
		out = r
		return nil
	})
	return out, err
}

// Review: approve | reject (anomali diverifikasi; pembacaan ditolak tidak dipakai sebagai angka awal/tagihan).
func (s *Service) Review(ctx context.Context, id uuid.UUID, action, note string) (*Reading, error) {
	p := authctx.Must(ctx)
	var out *Reading
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.readingTx(ctx, tx, id)
		if err != nil {
			return err
		}
		ok := false
		for _, a := range r.AllowedActions {
			ok = ok || a == action
		}
		if !ok {
			return apperr.InvalidTransition("Aksi " + action + " tidak tersedia untuk pembacaan berstatus " + r.Status)
		}
		st := map[string]string{"approve": "approved", "reject": "rejected"}[action]
		if action == "reject" && strings.TrimSpace(note) == "" {
			return apperr.Validation("note wajib saat menolak").WithField("note", "wajib")
		}
		if _, err := tx.Exec(ctx, `UPDATE meter_readings SET status = $2, reviewed_by = $3, reviewed_at = now(), review_note = NULLIF($4,'') WHERE id = $1`, id, st, p.UserID, strings.TrimSpace(note)); err != nil {
			return err
		}
		// angka mundur yang disetujui = meter diganti/reset → pemakaian dihitung dari 0 bila catatan menyebut reset
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "meter_reading", EntityID: &id, EntityLabel: r.MeterNumber, Before: map[string]any{"status": r.Status}, After: map[string]any{"status": st, "note": note}})
		out, err = s.readingTx(ctx, tx, id)
		return err
	})
	return out, err
}

// UsageForPeriodTx: pembacaan terakhir (tidak ditolak) meter pada periode YYYY-MM untuk billing run. flagged = belum direview.
func (s *Service) UsageForPeriodTx(ctx context.Context, tx pgx.Tx, meterID uuid.UUID, period string) (*Reading, error) {
	r, err := scanReading(tx.QueryRow(ctx, readingSelect+` WHERE r.meter_id = $1 AND r.period = $2 AND r.status <> 'rejected' ORDER BY r.read_at DESC LIMIT 1`, meterID, period))
	if err != nil {
		return nil, nil
	}
	return r, nil
}

// MetersOfUnitTx: meter aktif pada unit & tipe (billing rule meter_usage).
func (s *Service) MetersOfUnitTx(ctx context.Context, tx pgx.Tx, unitID uuid.UUID, meterType string) ([]Meter, error) {
	rows, err := tx.Query(ctx, meterSelect+` WHERE m.location_id = $1 AND m.meter_type = $2 AND m.status = 'active' ORDER BY m.meter_number`, unitID, meterType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Meter
	for rows.Next() {
		m, err := scanMeter(rows, "")
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// ---------- Staff App: bundle meter (P4-UTL-03) ----------

type BundleMeter struct {
	ID           uuid.UUID    `json:"id"`
	PropertyID   uuid.UUID    `json:"property_id"`
	LocationID   uuid.UUID    `json:"location_id"`
	LocationName string       `json:"location_name"`
	LocationPath string       `json:"location_path"`
	UnitNumber   *string      `json:"unit_number"`
	MeterType    string       `json:"meter_type"`
	MeterNumber  string       `json:"meter_number"`
	Multiplier   float64      `json:"multiplier"`
	UnitLabel    string       `json:"unit_label"`
	LastReading  *LastReading `json:"last_reading"`
	ReadPeriod   bool         `json:"read_this_period"`
}

// BundleMetersTx: meter aktif pada property yang dapat dicatat user (maks. 2.000) untuk rute pencatatan offline.
func (s *Service) BundleMetersTx(ctx context.Context, tx pgx.Tx) ([]BundleMeter, error) {
	p := authctx.Must(ctx)
	out := []BundleMeter{}
	if !p.Has("billing.meter_readings.create") {
		return out, nil
	}
	var args []any
	add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	where := " WHERE m.status = 'active' AND " + p.ScopeSQL("billing.meter_readings.create", "m.property_id", "l.path", add)
	rows, err := tx.Query(ctx, meterSelect+where+` ORDER BY l.path, m.meter_type, m.meter_number LIMIT 2000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMeter(rows, time.Now().In(jakarta()).Format("2006-01"))
		if err != nil {
			return nil, err
		}
		out = append(out, BundleMeter{ID: m.ID, PropertyID: m.PropertyID, LocationID: m.LocationID, LocationName: m.LocationName, LocationPath: m.LocationPath, UnitNumber: m.UnitNumber,
			MeterType: m.MeterType, MeterNumber: m.MeterNumber, Multiplier: m.Multiplier, UnitLabel: m.UnitLabel, LastReading: m.LastReading, ReadPeriod: m.ReadThisPeriod})
	}
	return out, rows.Err()
}

// Access: lampiran object meter (foto offline sebelum pembacaan tercipta) & meter_reading.
func (s *Service) Access(objectType string) func(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, write bool) error {
	return func(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, write bool) error {
		p := authctx.Must(ctx)
		var pid uuid.UUID
		q := `SELECT property_id FROM meters WHERE id = $1`
		if objectType == "meter_reading" {
			q = `SELECT property_id FROM meter_readings WHERE id = $1`
		}
		if err := tx.QueryRow(ctx, q, objectID).Scan(&pid); err != nil {
			return apperr.NotFound("Meter")
		}
		perm := "billing.meter_readings.view"
		if write {
			perm = "billing.meter_readings.create"
		}
		if p.IsTenant || !p.HasAnyOnProperty(perm, pid) {
			return apperr.Forbidden("")
		}
		return nil
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func dateOrNil(v *string) any {
	if v == nil || *v == "" {
		return nil
	}
	if t, err := time.Parse("2006-01-02", *v); err == nil {
		return t
	}
	return nil
}

func actorOrNil(p *authctx.Principal) *uuid.UUID {
	if p.IsSystem || p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}

var jkt *time.Location

func jakarta() *time.Location {
	if jkt == nil {
		l, err := time.LoadLocation("Asia/Jakarta")
		if err != nil {
			return time.FixedZone("WIB", 7*3600)
		}
		jkt = l
	}
	return jkt
}
