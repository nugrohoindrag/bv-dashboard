package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
)

// ---------- Billing Rule (PRD P4 v2.1 §5.1 P4-BRL-01; D-P4-05: SEMUA dasar hitung tersedia, dipilih per rule) ----------
// basis:
//   fixed_per_unit — tarif tetap per unit
//   per_area_m2    — tarif × luas unit (units.area_m2) — service charge / IPL per m²
//   per_unit_type  — tarif per tipe unit (rates {"residential": …, "commercial": …})
//   meter_usage    — pemakaian meter listrik/air × tarif utilitas (blok, abonemen)
//   percentage     — persen dari rule lain (mis. sinking fund = 10% IPL)
//   per_vehicle    — per izin parkir aktif (rates per jenis kendaraan; tarif khusus izin diutamakan)

var ruleBases = map[string]bool{"fixed_per_unit": true, "per_area_m2": true, "per_unit_type": true, "meter_usage": true, "percentage": true, "per_vehicle": true}
var ruleFrequencies = map[string]bool{"monthly": true, "quarterly": true, "yearly": true}
var occupancyStatuses = map[string]bool{"vacant": true, "occupied": true, "reserved": true, "inactive": true}

type Rule struct {
	ID                uuid.UUID          `json:"id"`
	PropertyID        uuid.UUID          `json:"property_id"`
	Code              string             `json:"code"`
	Name              string             `json:"name"`
	ChargeType        string             `json:"charge_type"`
	Basis             string             `json:"basis"`
	Rate              float64            `json:"rate"`
	Rates             map[string]float64 `json:"rates"`
	BaseRuleID        *uuid.UUID         `json:"base_rule_id"`
	BaseRuleName      *string            `json:"base_rule_name"`
	TariffID          *uuid.UUID         `json:"tariff_id"`
	TariffName        *string            `json:"tariff_name"`
	MeterType         *string            `json:"meter_type"`
	Frequency         string             `json:"frequency"`
	IssueDay          int                `json:"issue_day"`
	DueDays           int                `json:"due_days"`
	TaxRate           *float64           `json:"tax_rate"`
	Prorate           bool               `json:"prorate"`
	UnitLabel         *string            `json:"unit_label"`
	ScopeLocationIDs  []uuid.UUID        `json:"scope_location_ids"`
	UnitTypes         []string           `json:"unit_types"`
	OccupancyStatuses []string           `json:"occupancy_statuses"`
	BillTo            string             `json:"bill_to"`
	AutoGenerate      bool               `json:"auto_generate"`
	IsActive          bool               `json:"is_active"`
	EffectiveFrom     *string            `json:"effective_from"`
	EffectiveUntil    *string            `json:"effective_until"`
	Description       *string            `json:"description"`
	Version           int                `json:"version"`
}

const ruleSelect = `SELECT r.id, r.property_id, r.code, r.name, r.charge_type, r.basis, r.rate::float8, r.rates, r.base_rule_id, br.name, r.tariff_id, ut.name, r.meter_type, r.frequency, r.issue_day, r.due_days,
	r.tax_rate::float8, r.prorate, r.unit_label, r.scope_location_ids, r.unit_types, r.occupancy_statuses, r.bill_to, r.auto_generate, r.is_active,
	to_char(r.effective_from,'YYYY-MM-DD'), to_char(r.effective_until,'YYYY-MM-DD'), r.description, r.version
	FROM billing_rules r LEFT JOIN billing_rules br ON br.id = r.base_rule_id LEFT JOIN utility_tariffs ut ON ut.id = r.tariff_id`

func scanRule(row pgx.Row) (*Rule, error) {
	var r Rule
	var rates []byte
	if err := row.Scan(&r.ID, &r.PropertyID, &r.Code, &r.Name, &r.ChargeType, &r.Basis, &r.Rate, &rates, &r.BaseRuleID, &r.BaseRuleName, &r.TariffID, &r.TariffName, &r.MeterType, &r.Frequency, &r.IssueDay, &r.DueDays,
		&r.TaxRate, &r.Prorate, &r.UnitLabel, &r.ScopeLocationIDs, &r.UnitTypes, &r.OccupancyStatuses, &r.BillTo, &r.AutoGenerate, &r.IsActive, &r.EffectiveFrom, &r.EffectiveUntil, &r.Description, &r.Version); err != nil {
		return nil, err
	}
	r.Rates = map[string]float64{}
	_ = json.Unmarshal(rates, &r.Rates)
	if r.ScopeLocationIDs == nil {
		r.ScopeLocationIDs = []uuid.UUID{}
	}
	if r.UnitTypes == nil {
		r.UnitTypes = []string{}
	}
	if r.OccupancyStatuses == nil {
		r.OccupancyStatuses = []string{}
	}
	return &r, nil
}

func (s *Service) ListRules(ctx context.Context, propertyID *uuid.UUID, activeOnly bool) ([]Rule, error) {
	p := authctx.Must(ctx)
	out := []Rule{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args := " WHERE true", []any{}
		if propertyID != nil {
			if !p.HasAnyOnProperty("billing.rules.view", *propertyID) {
				return apperr.Forbidden("")
			}
			args = append(args, *propertyID)
			where += " AND r.property_id = $1"
		} else if pids, all := p.PropertyIDsFor("billing.rules.view"); !all {
			args = append(args, pids)
			where += " AND r.property_id = ANY($1)"
		}
		if activeOnly {
			where += " AND r.is_active"
		}
		rows, err := tx.Query(ctx, ruleSelect+where+` ORDER BY r.property_id, (r.basis = 'percentage'), r.code`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRule(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) ruleTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Rule, error) {
	r, err := scanRule(tx.QueryRow(ctx, ruleSelect+` WHERE r.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Billing rule")
		}
		return nil, err
	}
	return r, nil
}

func (s *Service) GetRule(ctx context.Context, id uuid.UUID) (*Rule, error) {
	var out *Rule
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.ruleTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasAnyOnProperty("billing.rules.view", r.PropertyID) {
			return apperr.Forbidden("")
		}
		out = r
		return nil
	})
	return out, err
}

type RuleInput struct {
	PropertyID        *uuid.UUID          `json:"property_id"`
	Code              *string             `json:"code"`
	Name              *string             `json:"name"`
	ChargeType        *string             `json:"charge_type"`
	Basis             *string             `json:"basis"`
	Rate              *float64            `json:"rate"`
	Rates             *map[string]float64 `json:"rates"`
	BaseRuleID        *uuid.UUID          `json:"base_rule_id"` // UUID nol = kosongkan
	TariffID          *uuid.UUID          `json:"tariff_id"`    // UUID nol = kosongkan
	MeterType         *string             `json:"meter_type"`
	Frequency         *string             `json:"frequency"`
	IssueDay          *int                `json:"issue_day"`
	DueDays           *int                `json:"due_days"`
	TaxRate           *float64            `json:"tax_rate"`       // 0..100; kembali ke pengaturan billing lewat clear_tax_rate
	ClearTaxRate      bool                `json:"clear_tax_rate"` // true = kembali ke pengaturan billing
	Prorate           *bool               `json:"prorate"`
	UnitLabel         *string             `json:"unit_label"`
	ScopeLocationIDs  *[]uuid.UUID        `json:"scope_location_ids"`
	UnitTypes         *[]string           `json:"unit_types"`
	OccupancyStatuses *[]string           `json:"occupancy_statuses"`
	BillTo            *string             `json:"bill_to"`
	AutoGenerate      *bool               `json:"auto_generate"`
	IsActive          *bool               `json:"is_active"`
	EffectiveFrom     *string             `json:"effective_from"`
	EffectiveUntil    *string             `json:"effective_until"`
	Description       *string             `json:"description"`
}

// SaveRule: buat / ubah billing rule (billing.rules.manage). Rule yang sudah pernah ditagihkan tetap dapat diubah (tarif baru
// berlaku untuk periode berikutnya; invoice lama tidak berubah).
func (s *Service) SaveRule(ctx context.Context, id *uuid.UUID, in RuleInput) (*Rule, error) {
	p := authctx.Must(ctx)
	var out *Rule
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		cur := &Rule{Rates: map[string]float64{}, Frequency: "monthly", IssueDay: 1, DueDays: 14, BillTo: "tenant", OccupancyStatuses: []string{"occupied"}, IsActive: true, ScopeLocationIDs: []uuid.UUID{}, UnitTypes: []string{}}
		if id != nil {
			r, err := s.ruleTx(ctx, tx, *id)
			if err != nil {
				return err
			}
			cur = r
		} else {
			if in.PropertyID == nil {
				return apperr.Validation("property_id wajib").WithField("property_id", "wajib")
			}
			cur.PropertyID = *in.PropertyID
		}
		if !p.HasOnProperty("billing.rules.manage", cur.PropertyID) {
			return apperr.Forbidden("Memerlukan billing.rules.manage")
		}
		str := func(dst *string, v *string) {
			if v != nil {
				*dst = strings.TrimSpace(*v)
			}
		}
		str(&cur.Code, in.Code)
		str(&cur.Name, in.Name)
		str(&cur.ChargeType, in.ChargeType)
		str(&cur.Basis, in.Basis)
		str(&cur.Frequency, in.Frequency)
		str(&cur.BillTo, in.BillTo)
		if in.Rate != nil {
			cur.Rate = *in.Rate
		}
		if in.Rates != nil {
			cur.Rates = *in.Rates
		}
		if in.BaseRuleID != nil {
			cur.BaseRuleID = in.BaseRuleID
			if *in.BaseRuleID == uuid.Nil {
				cur.BaseRuleID = nil
			}
		}
		if in.TariffID != nil {
			cur.TariffID = in.TariffID
			if *in.TariffID == uuid.Nil {
				cur.TariffID = nil
			}
		}
		if in.MeterType != nil {
			mt := strings.TrimSpace(*in.MeterType)
			cur.MeterType = &mt
			if mt == "" {
				cur.MeterType = nil
			}
		}
		if in.IssueDay != nil {
			cur.IssueDay = *in.IssueDay
		}
		if in.DueDays != nil {
			cur.DueDays = *in.DueDays
		}
		if in.ClearTaxRate {
			cur.TaxRate = nil
		} else if in.TaxRate != nil {
			cur.TaxRate = in.TaxRate
		}
		if in.Prorate != nil {
			cur.Prorate = *in.Prorate
		}
		if in.UnitLabel != nil {
			cur.UnitLabel = in.UnitLabel
		}
		if in.ScopeLocationIDs != nil {
			cur.ScopeLocationIDs = *in.ScopeLocationIDs
		}
		if in.UnitTypes != nil {
			cur.UnitTypes = *in.UnitTypes
		}
		if in.OccupancyStatuses != nil {
			cur.OccupancyStatuses = *in.OccupancyStatuses
		}
		if in.AutoGenerate != nil {
			cur.AutoGenerate = *in.AutoGenerate
		}
		if in.IsActive != nil {
			cur.IsActive = *in.IsActive
		}
		if in.EffectiveFrom != nil {
			cur.EffectiveFrom = in.EffectiveFrom
			if *in.EffectiveFrom == "" {
				cur.EffectiveFrom = nil
			}
		}
		if in.EffectiveUntil != nil {
			cur.EffectiveUntil = in.EffectiveUntil
			if *in.EffectiveUntil == "" {
				cur.EffectiveUntil = nil
			}
		}
		if in.Description != nil {
			cur.Description = in.Description
		}
		// ---- validasi ----
		if cur.Code == "" || cur.Name == "" {
			return apperr.Validation("code dan name wajib").WithField("code", "wajib")
		}
		if !ruleBases[cur.Basis] {
			return apperr.Validation("basis harus fixed_per_unit|per_area_m2|per_unit_type|meter_usage|percentage|per_vehicle").WithField("basis", "tidak valid")
		}
		if cur.ChargeType == "" {
			cur.ChargeType = map[string]string{"meter_usage": "utility", "per_vehicle": "parking"}[cur.Basis]
			if cur.ChargeType == "" {
				cur.ChargeType = "service_charge"
			}
		}
		if !invoiceTypes[cur.ChargeType] || cur.ChargeType == "penalty" {
			return apperr.Validation("charge_type tidak valid").WithField("charge_type", "tidak valid")
		}
		if !ruleFrequencies[cur.Frequency] {
			return apperr.Validation("frequency harus monthly|quarterly|yearly").WithField("frequency", "tidak valid")
		}
		if cur.BillTo != "tenant" && cur.BillTo != "unit" {
			return apperr.Validation("bill_to harus tenant|unit").WithField("bill_to", "tidak valid")
		}
		if cur.IssueDay < 1 || cur.IssueDay > 28 {
			return apperr.Validation("issue_day harus 1..28").WithField("issue_day", "1..28")
		}
		if cur.DueDays < 0 || cur.DueDays > 120 {
			return apperr.Validation("due_days harus 0..120").WithField("due_days", "0..120")
		}
		if cur.TaxRate != nil && (*cur.TaxRate < 0 || *cur.TaxRate > 100) {
			return apperr.Validation("tax_rate harus 0..100").WithField("tax_rate", "0..100")
		}
		if cur.Rate < 0 {
			return apperr.Validation("rate tidak boleh negatif").WithField("rate", "negatif")
		}
		for k, v := range cur.Rates {
			if v < 0 || strings.TrimSpace(k) == "" {
				return apperr.Validation("rates tidak valid").WithField("rates", "tidak valid")
			}
		}
		for _, os := range cur.OccupancyStatuses {
			if !occupancyStatuses[os] {
				return apperr.Validation("occupancy_statuses tidak valid").WithField("occupancy_statuses", "tidak valid")
			}
		}
		switch cur.Basis {
		case "per_unit_type":
			if len(cur.Rates) == 0 {
				return apperr.Validation("per_unit_type memerlukan rates per tipe unit").WithField("rates", "wajib")
			}
		case "meter_usage":
			if cur.MeterType == nil || (*cur.MeterType != "electricity" && *cur.MeterType != "water") {
				return apperr.Validation("meter_usage memerlukan meter_type electricity|water").WithField("meter_type", "wajib")
			}
			if cur.ChargeType == "service_charge" {
				cur.ChargeType = *cur.MeterType
			}
		case "percentage":
			if cur.BaseRuleID == nil {
				return apperr.Validation("percentage memerlukan base_rule_id").WithField("base_rule_id", "wajib")
			}
			if id != nil && *cur.BaseRuleID == *id {
				return apperr.Validation("base_rule_id tidak boleh rule itu sendiri").WithField("base_rule_id", "tidak valid")
			}
			base, err := s.ruleTx(ctx, tx, *cur.BaseRuleID)
			if err != nil || base.PropertyID != cur.PropertyID {
				return apperr.Validation("base_rule_id tidak ditemukan di property ini").WithField("base_rule_id", "tidak valid")
			}
			if base.Basis == "percentage" {
				return apperr.Validation("base rule tidak boleh berbasis persentase").WithField("base_rule_id", "rantai persentase")
			}
			if cur.Rate <= 0 || cur.Rate > 100 {
				return apperr.Validation("rate persentase harus 0..100").WithField("rate", "0..100")
			}
		}
		if cur.TariffID != nil {
			var tp uuid.UUID
			var mt string
			if err := tx.QueryRow(ctx, `SELECT property_id, meter_type FROM utility_tariffs WHERE id = $1`, *cur.TariffID).Scan(&tp, &mt); err != nil || tp != cur.PropertyID {
				return apperr.Validation("tariff_id tidak ditemukan di property ini").WithField("tariff_id", "tidak valid")
			}
			if cur.MeterType != nil && *cur.MeterType != mt {
				return apperr.Validation("tariff_id tidak sesuai meter_type").WithField("tariff_id", "tipe meter berbeda")
			}
		}
		if len(cur.ScopeLocationIDs) > 0 {
			var n int
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE id = ANY($1) AND property_id = $2 AND deleted_at IS NULL`, cur.ScopeLocationIDs, cur.PropertyID).Scan(&n)
			if n != len(cur.ScopeLocationIDs) {
				return apperr.Validation("scope_location_ids harus lokasi di property ini").WithField("scope_location_ids", "tidak valid")
			}
		}
		rates, _ := json.Marshal(cur.Rates)
		ef, eu := dateArg(cur.EffectiveFrom), dateArg(cur.EffectiveUntil)
		var rid uuid.UUID
		if id == nil {
			if err := tx.QueryRow(ctx, `INSERT INTO billing_rules (organization_id, property_id, code, name, charge_type, basis, rate, rates, base_rule_id, tariff_id, meter_type, frequency, issue_day, due_days, tax_rate, prorate, unit_label,
				scope_location_ids, unit_types, occupancy_statuses, bill_to, auto_generate, is_active, effective_from, effective_until, description, created_by, updated_by)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$27) RETURNING id`,
				p.OrganizationID, cur.PropertyID, cur.Code, cur.Name, cur.ChargeType, cur.Basis, cur.Rate, rates, cur.BaseRuleID, cur.TariffID, cur.MeterType, cur.Frequency, cur.IssueDay, cur.DueDays, cur.TaxRate, cur.Prorate, cur.UnitLabel,
				cur.ScopeLocationIDs, cur.UnitTypes, cur.OccupancyStatuses, cur.BillTo, cur.AutoGenerate, cur.IsActive, ef, eu, cur.Description, p.UserID).Scan(&rid); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("RULE_CODE_EXISTS", "Kode billing rule sudah dipakai").WithField("code", "sudah dipakai")
				}
				return err
			}
		} else {
			rid = *id
			if _, err := tx.Exec(ctx, `UPDATE billing_rules SET code = $2, name = $3, charge_type = $4, basis = $5, rate = $6, rates = $7, base_rule_id = $8, tariff_id = $9, meter_type = $10, frequency = $11, issue_day = $12,
				due_days = $13, tax_rate = $14, prorate = $15, unit_label = $16, scope_location_ids = $17, unit_types = $18, occupancy_statuses = $19, bill_to = $20, auto_generate = $21, is_active = $22,
				effective_from = $23, effective_until = $24, description = $25, updated_by = $26 WHERE id = $1`,
				rid, cur.Code, cur.Name, cur.ChargeType, cur.Basis, cur.Rate, rates, cur.BaseRuleID, cur.TariffID, cur.MeterType, cur.Frequency, cur.IssueDay, cur.DueDays, cur.TaxRate, cur.Prorate, cur.UnitLabel,
				cur.ScopeLocationIDs, cur.UnitTypes, cur.OccupancyStatuses, cur.BillTo, cur.AutoGenerate, cur.IsActive, ef, eu, cur.Description, p.UserID); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("RULE_CODE_EXISTS", "Kode billing rule sudah dipakai").WithField("code", "sudah dipakai")
				}
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditConfigChange, EntityType: "billing_rule", EntityID: &rid, EntityLabel: cur.Code, After: in})
		var err error
		out, err = s.ruleTx(ctx, tx, rid)
		return err
	})
	return out, err
}

func dateArg(v *string) any {
	if v == nil || *v == "" {
		return nil
	}
	t, err := parseDate(v)
	if err != nil || t == nil {
		return nil
	}
	return *t
}

var _ = fmt.Sprintf
