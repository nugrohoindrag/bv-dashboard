package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
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
	"github.com/buildingvision/api/internal/profile"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Generate tagihan periode (PRD P4 v2.1 §5.1 P4-BRL-02..04) ----------
// preview (baris per unit × rule, total, pengecualian) → generate (draft invoice, satu per unit bila combine) → issue massal.
// Idempoten per rule × unit × (izin parkir) × periode lewat indeks unik billing_run_lines(invoice_id NOT NULL); invoice yang
// dibatalkan melepas barisnya sehingga dapat ditagihkan ulang.

const EventBillingRunGenerated = "billing_run.generated"

// Exception labels (preview).
var exceptionLabels = map[string]string{
	"no_area": "Luas unit belum diisi", "no_rate": "Tarif tipe unit belum diatur", "no_tenant": "Unit belum memiliki tenant",
	"no_meter": "Unit belum memiliki meter", "no_reading": "Belum ada pembacaan meter periode ini", "reading_flagged": "Pembacaan meter perlu direview",
	"no_tariff": "Tarif utilitas belum diatur", "already_billed": "Sudah ditagihkan pada periode ini", "zero_amount": "Jumlah nol", "no_base": "Komponen dasar tidak ada",
}

type RunLine struct {
	ID              uuid.UUID      `json:"id"`
	RuleID          uuid.UUID      `json:"billing_rule_id"`
	RuleName        string         `json:"rule_name"`
	UnitID          uuid.UUID      `json:"unit_location_id"`
	UnitLabel       string         `json:"unit_label"`
	TenantID        *uuid.UUID     `json:"tenant_id"`
	TenantName      *string        `json:"tenant_name"`
	ChargeType      string         `json:"charge_type"`
	Description     string         `json:"description"`
	Quantity        float64        `json:"quantity"`
	UnitMeasure     *string        `json:"unit_measure"`
	UnitPrice       int64          `json:"unit_price"`
	Amount          int64          `json:"amount"`
	TaxRate         float64        `json:"tax_rate"`
	TaxAmount       int64          `json:"tax_amount"`
	MeterReadingID  *uuid.UUID     `json:"meter_reading_id"`
	ParkingPermitID *uuid.UUID     `json:"parking_permit_id"`
	Meta            map[string]any `json:"meta"`
	Exception       *string        `json:"exception"`
	ExceptionLabel  *string        `json:"exception_label"`
	Included        bool           `json:"included"`
	InvoiceID       *uuid.UUID     `json:"invoice_id"`
	InvoiceNumber   *string        `json:"invoice_number"`
}

type Run struct {
	ID             uuid.UUID      `json:"id"`
	PropertyID     uuid.UUID      `json:"property_id"`
	PropertyName   string         `json:"property_name"`
	RunNumber      string         `json:"run_number"`
	PeriodStart    string         `json:"period_start"`
	PeriodEnd      string         `json:"period_end"`
	PeriodLabel    string         `json:"period_label"`
	Status         string         `json:"status"`
	Source         string         `json:"source"`
	RuleIDs        []uuid.UUID    `json:"rule_ids"`
	Combine        bool           `json:"combine"`
	InvoiceType    string         `json:"invoice_type"`
	IssueDate      string         `json:"issue_date"`
	DueDate        string         `json:"due_date"`
	LineCount      int            `json:"line_count"`
	ExceptionCount int            `json:"exception_count"`
	SubtotalAmount int64          `json:"subtotal_amount"`
	TaxAmount      int64          `json:"tax_amount"`
	TotalAmount    int64          `json:"total_amount"`
	InvoiceCount   int            `json:"invoice_count"`
	Notes          *string        `json:"notes"`
	CreatedByName  *string        `json:"created_by_name"`
	CreatedAt      time.Time      `json:"created_at"`
	GeneratedAt    *time.Time     `json:"generated_at"`
	IssuedAt       *time.Time     `json:"issued_at"`
	Exceptions     map[string]int `json:"exceptions"`
	Lines          []RunLine      `json:"lines,omitempty"`
	AllowedActions []string       `json:"allowed_actions"`
	Version        int            `json:"version"`
}

const runSelect = `SELECT r.id, r.property_id, pl.name, r.run_number, to_char(r.period_start,'YYYY-MM-DD'), to_char(r.period_end,'YYYY-MM-DD'), r.status, r.source, r.rule_ids, r.combine, r.invoice_type,
	to_char(r.issue_date,'YYYY-MM-DD'), to_char(r.due_date,'YYYY-MM-DD'), r.line_count, r.exception_count, r.subtotal_amount, r.tax_amount, r.total_amount, r.invoice_count, r.notes, u.full_name,
	r.created_at, r.generated_at, r.issued_at, r.version
	FROM billing_runs r JOIN locations pl ON pl.id = r.property_id LEFT JOIN users u ON u.id = r.created_by`

func scanRun(row pgx.Row) (*Run, error) {
	var r Run
	if err := row.Scan(&r.ID, &r.PropertyID, &r.PropertyName, &r.RunNumber, &r.PeriodStart, &r.PeriodEnd, &r.Status, &r.Source, &r.RuleIDs, &r.Combine, &r.InvoiceType,
		&r.IssueDate, &r.DueDate, &r.LineCount, &r.ExceptionCount, &r.SubtotalAmount, &r.TaxAmount, &r.TotalAmount, &r.InvoiceCount, &r.Notes, &r.CreatedByName,
		&r.CreatedAt, &r.GeneratedAt, &r.IssuedAt, &r.Version); err != nil {
		return nil, err
	}
	if t, err := time.Parse("2006-01-02", r.PeriodStart); err == nil {
		r.PeriodLabel = monthLabel(t)
	}
	if r.RuleIDs == nil {
		r.RuleIDs = []uuid.UUID{}
	}
	r.Exceptions = map[string]int{}
	return &r, nil
}

var bulan = []string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}

func monthLabel(t time.Time) string { return bulan[t.Month()] + " " + fmt.Sprint(t.Year()) }

func (s *Service) runActions(ctx context.Context, r *Run) {
	p := authctx.Must(ctx)
	r.AllowedActions = []string{"view"}
	can := func(perm string) bool { return p.HasOnProperty(perm, r.PropertyID) }
	switch r.Status {
	case "preview":
		if can("billing.runs.generate") {
			r.AllowedActions = append(r.AllowedActions, "generate", "refresh", "cancel", "toggle_line")
		}
	case "generated":
		if can("billing.runs.issue") {
			r.AllowedActions = append(r.AllowedActions, "issue")
		}
		if can("billing.runs.generate") {
			r.AllowedActions = append(r.AllowedActions, "cancel")
		}
	}
}

func (s *Service) runTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, withLines bool) (*Run, error) {
	r, err := scanRun(tx.QueryRow(ctx, runSelect+` WHERE r.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Billing run")
		}
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT exception, count(*) FROM billing_run_lines WHERE run_id = $1 AND exception IS NOT NULL GROUP BY exception`, id)
	if err == nil {
		for rows.Next() {
			var k string
			var n int
			if rows.Scan(&k, &n) == nil {
				r.Exceptions[k] = n
			}
		}
		rows.Close()
	}
	if withLines {
		lines, err := s.runLinesTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		r.Lines = lines
	}
	s.runActions(ctx, r)
	return r, nil
}

func (s *Service) runLinesTx(ctx context.Context, tx pgx.Tx, runID uuid.UUID) ([]RunLine, error) {
	rows, err := tx.Query(ctx, `SELECT bl.id, bl.billing_rule_id, br.name, bl.unit_location_id, COALESCE('Unit ' || un.unit_number, l.name), bl.tenant_id, t.name, bl.charge_type, bl.description,
		bl.quantity::float8, bl.unit_label, bl.unit_price, bl.amount, bl.tax_rate::float8, bl.tax_amount, bl.meter_reading_id, bl.parking_permit_id, bl.meta, bl.exception, bl.included, bl.invoice_id, i.invoice_number
		FROM billing_run_lines bl JOIN billing_rules br ON br.id = bl.billing_rule_id JOIN locations l ON l.id = bl.unit_location_id LEFT JOIN units un ON un.location_id = bl.unit_location_id
		LEFT JOIN tenants t ON t.id = bl.tenant_id LEFT JOIN invoices i ON i.id = bl.invoice_id WHERE bl.run_id = $1 ORDER BY l.path, br.code`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RunLine{}
	for rows.Next() {
		var l RunLine
		var meta []byte
		if err := rows.Scan(&l.ID, &l.RuleID, &l.RuleName, &l.UnitID, &l.UnitLabel, &l.TenantID, &l.TenantName, &l.ChargeType, &l.Description, &l.Quantity, &l.UnitMeasure, &l.UnitPrice, &l.Amount,
			&l.TaxRate, &l.TaxAmount, &l.MeterReadingID, &l.ParkingPermitID, &meta, &l.Exception, &l.Included, &l.InvoiceID, &l.InvoiceNumber); err != nil {
			return nil, err
		}
		l.Meta = map[string]any{}
		_ = json.Unmarshal(meta, &l.Meta)
		if l.Exception != nil {
			lbl := exceptionLabels[*l.Exception]
			l.ExceptionLabel = &lbl
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

type RunInput struct {
	PropertyID  uuid.UUID   `json:"property_id"`
	Period      string      `json:"period"` // YYYY-MM (atau period_start + period_end)
	PeriodStart *string     `json:"period_start"`
	PeriodEnd   *string     `json:"period_end"`
	RuleIDs     []uuid.UUID `json:"rule_ids"` // kosong = seluruh rule aktif property
	Combine     *bool       `json:"combine"`  // default true: satu invoice per unit
	InvoiceType string      `json:"invoice_type"`
	IssueDate   *string     `json:"issue_date"`
	DueDate     *string     `json:"due_date"`
	Notes       *string     `json:"notes"`
	Source      string      `json:"-"`
}

type unitRow struct {
	id             uuid.UUID
	path, label    string
	unitType       string
	area           *float64
	tenantID       *uuid.UUID
	tenantName     *string
	taxExempt      bool
	occupancy      string
	occFrom, occTo *time.Time
}

// ruleDue: rule ditagihkan pada periode (frekuensi & masa berlaku).
func ruleDue(r *Rule, start, end time.Time) bool {
	if r.EffectiveFrom != nil {
		if t, err := time.Parse("2006-01-02", *r.EffectiveFrom); err == nil && t.After(end) {
			return false
		}
	}
	if r.EffectiveUntil != nil {
		if t, err := time.Parse("2006-01-02", *r.EffectiveUntil); err == nil && t.Before(start) {
			return false
		}
	}
	switch r.Frequency {
	case "quarterly":
		m := start.Month()
		return m == 1 || m == 4 || m == 7 || m == 10
	case "yearly":
		anchor := time.January
		if r.EffectiveFrom != nil {
			if t, err := time.Parse("2006-01-02", *r.EffectiveFrom); err == nil {
				anchor = t.Month()
			}
		}
		return start.Month() == anchor
	}
	return true
}

// prorateFactor: porsi hari hunian dalam periode (P4-BRL-04).
func prorateFactor(u unitRow, start, end time.Time) (float64, int, int) {
	total := int(end.Sub(start).Hours()/24) + 1
	from, to := start, end
	if u.occFrom != nil && u.occFrom.After(from) {
		from = *u.occFrom
	}
	if u.occTo != nil && u.occTo.Before(to) {
		to = *u.occTo
	}
	days := int(to.Sub(from).Hours()/24) + 1
	if days < 0 {
		days = 0
	}
	if days > total {
		days = total
	}
	return float64(days) / float64(total), days, total
}

func inScope(r *Rule, u unitRow, scopePaths []string) bool {
	if len(r.UnitTypes) > 0 && !has(r.UnitTypes, u.unitType) {
		return false
	}
	if len(r.OccupancyStatuses) > 0 && !has(r.OccupancyStatuses, u.occupancy) {
		return false
	}
	if len(scopePaths) == 0 {
		return true
	}
	for _, sp := range scopePaths {
		if u.path == sp || strings.HasPrefix(u.path, sp+".") {
			return true
		}
	}
	return false
}

// computeLinesTx: hitung seluruh baris run (tanpa menyimpan).
func (s *Service) computeLinesTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, rules []Rule, start, end time.Time) ([]RunLine, error) {
	st := s.settingsFor(ctx, tx, &propertyID)
	rows, err := tx.Query(ctx, `SELECT l.id, l.path::text, COALESCE('Unit ' || un.unit_number, l.name), un.unit_type, un.area_m2::float8, un.tenant_id, t.name, COALESCE(t.tax_exempt,false), un.occupancy_status, un.occupied_from, un.occupied_until
		FROM units un JOIN locations l ON l.id = un.location_id LEFT JOIN tenants t ON t.id = un.tenant_id
		WHERE l.property_id = $1 AND l.deleted_at IS NULL AND l.is_active AND un.unit_type <> 'hotel_room' ORDER BY l.path`, propertyID)
	if err != nil {
		return nil, err
	}
	var units []unitRow
	for rows.Next() {
		var u unitRow
		if err := rows.Scan(&u.id, &u.path, &u.label, &u.unitType, &u.area, &u.tenantID, &u.tenantName, &u.taxExempt, &u.occupancy, &u.occFrom, &u.occTo); err != nil {
			rows.Close()
			return nil, err
		}
		units = append(units, u)
	}
	rows.Close()
	period := start.Format("2006-01")
	label := monthLabel(start)
	// rule persentase dihitung setelah rule dasarnya
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Basis != "percentage" && rules[j].Basis == "percentage" })
	baseAmount := map[string]int64{} // ruleID|unitID → jumlah (termasuk rule dasar yang tidak ikut run)
	var out []RunLine
	exc := func(l *RunLine, code string) {
		if l.Exception == nil {
			c := code
			l.Exception = &c
		}
	}
	for ri := range rules {
		r := &rules[ri]
		if !r.IsActive || !ruleDue(r, start, end) {
			continue
		}
		var scopePaths []string
		if len(r.ScopeLocationIDs) > 0 {
			prow, err := tx.Query(ctx, `SELECT path::text FROM locations WHERE id = ANY($1)`, r.ScopeLocationIDs)
			if err == nil {
				for prow.Next() {
					var p string
					if prow.Scan(&p) == nil {
						scopePaths = append(scopePaths, p)
					}
				}
				prow.Close()
			}
		}
		taxRate := 0.0
		if r.TaxRate != nil {
			taxRate = *r.TaxRate
		} else if st.TaxEnabled {
			taxRate = st.TaxRate
		}
		for _, u := range units {
			if !inScope(r, u, scopePaths) {
				continue
			}
			base := RunLine{RuleID: r.ID, RuleName: r.Name, UnitID: u.id, UnitLabel: u.label, ChargeType: r.ChargeType, Included: true, Meta: map[string]any{"basis": r.Basis, "period": period}}
			if r.BillTo == "tenant" {
				base.TenantID, base.TenantName = u.tenantID, u.tenantName
			}
			rate := taxRate
			if u.taxExempt {
				rate = 0
			}
			base.TaxRate = rate
			factor, days, total := 1.0, 0, 0
			if r.Prorate {
				factor, days, total = prorateFactor(u, start, end)
				if factor < 1 {
					base.Meta["prorate_days"], base.Meta["period_days"] = days, total
				}
			}
			var lines []RunLine
			switch r.Basis {
			case "fixed_per_unit":
				l := base
				l.Description = fmt.Sprintf("%s — %s", r.Name, label)
				l.Quantity, l.UnitPrice = 1, int64(math.Round(r.Rate))
				l.Amount = int64(math.Round(r.Rate * factor))
				l.UnitMeasure = r.UnitLabel
				lines = append(lines, l)
			case "per_area_m2":
				l := base
				m2 := "m²"
				l.UnitMeasure = &m2
				l.Description = fmt.Sprintf("%s — %s", r.Name, label)
				l.UnitPrice = int64(math.Round(r.Rate))
				l.Meta["rate"] = r.Rate
				if u.area == nil || *u.area <= 0 {
					exc(&l, "no_area")
				} else {
					l.Quantity = *u.area
					l.Amount = int64(math.Round(*u.area * r.Rate * factor))
					l.Meta["area_m2"] = *u.area
				}
				lines = append(lines, l)
			case "per_unit_type":
				l := base
				l.Description = fmt.Sprintf("%s — %s", r.Name, label)
				l.Quantity = 1
				rt, ok := r.Rates[u.unitType]
				if !ok {
					exc(&l, "no_rate")
				} else {
					l.UnitPrice = int64(math.Round(rt))
					l.Amount = int64(math.Round(rt * factor))
					l.Meta["unit_type"] = u.unitType
				}
				lines = append(lines, l)
			case "meter_usage":
				meters, err := s.Metering.MetersOfUnitTx(ctx, tx, u.id, deref(r.MeterType))
				if err != nil {
					return nil, err
				}
				if len(meters) == 0 {
					l := base
					l.Description = fmt.Sprintf("%s — %s", r.Name, label)
					exc(&l, "no_meter")
					lines = append(lines, l)
				}
				for _, m := range meters {
					l := base
					l.Meta = map[string]any{"basis": r.Basis, "period": period, "meter_number": m.MeterNumber, "meter_id": m.ID}
					um := m.UnitLabel
					l.UnitMeasure = &um
					tariffID := m.TariffID
					if tariffID == nil {
						tariffID = r.TariffID
					}
					reading, _ := s.Metering.UsageForPeriodTx(ctx, tx, m.ID, period)
					kind := map[string]string{"electricity": "Listrik", "water": "Air"}[m.MeterType]
					l.Description = fmt.Sprintf("%s %s — meter %s", kind, label, m.MeterNumber)
					switch {
					case reading == nil:
						exc(&l, "no_reading")
					case reading.Status == "flagged":
						exc(&l, "reading_flagged")
						l.MeterReadingID = &reading.ID
					case tariffID == nil:
						exc(&l, "no_tariff")
						l.MeterReadingID = &reading.ID
					default:
						t, err := s.Metering.TariffTx(ctx, tx, *tariffID)
						if err != nil {
							exc(&l, "no_tariff")
							break
						}
						usage := 0.0
						if reading.Usage != nil {
							usage = *reading.Usage
						}
						energy, fixed, totalAmt := t.UsageCharge(usage)
						l.MeterReadingID = &reading.ID
						l.Quantity = math.Round(usage*100) / 100
						l.UnitPrice = int64(math.Round(t.Rate))
						l.Amount = totalAmt
						prev := 0.0
						if reading.PreviousValue != nil {
							prev = *reading.PreviousValue
						}
						l.Description = fmt.Sprintf("%s %s — meter %s: %s → %s (%s %s)", kind, label, m.MeterNumber, fmtNum(prev), fmtNum(reading.ReadingValue), fmtNum(usage), m.UnitLabel)
						l.Meta["previous"], l.Meta["current"], l.Meta["usage"] = prev, reading.ReadingValue, usage
						l.Meta["energy_charge"], l.Meta["fixed_charge"], l.Meta["tariff"] = energy, fixed, t.Name
					}
					lines = append(lines, l)
				}
			case "percentage":
				l := base
				l.Description = fmt.Sprintf("%s — %s", r.Name, label)
				l.Quantity = 1
				key := r.BaseRuleID.String() + "|" + u.id.String()
				bAmt, ok := baseAmount[key]
				if !ok {
					// rule dasar tidak ikut run → hitung virtual untuk unit ini
					if br, err := s.ruleTx(ctx, tx, *r.BaseRuleID); err == nil {
						if vl, err := s.computeLinesTx(ctx, tx, propertyID, []Rule{*br}, start, end); err == nil {
							for _, x := range vl {
								if x.UnitID == u.id && (x.Exception == nil || *x.Exception == "already_billed") {
									bAmt += x.Amount
									ok = true
								}
							}
						}
					}
				}
				if !ok || bAmt <= 0 {
					exc(&l, "no_base")
				} else {
					l.UnitPrice = int64(math.Round(float64(bAmt) * r.Rate / 100))
					l.Amount = l.UnitPrice
					l.Meta["base_amount"], l.Meta["percent"] = bAmt, r.Rate
				}
				lines = append(lines, l)
			case "per_vehicle":
				prow, err := tx.Query(ctx, `SELECT pp.id, v.plate_number, v.vehicle_type, pp.fee_amount FROM parking_permits pp JOIN vehicles v ON v.id = pp.vehicle_id
					WHERE pp.unit_location_id = $1 AND pp.status IN ('approved','expired') AND (pp.valid_from IS NULL OR pp.valid_from <= $3) AND (pp.valid_until IS NULL OR pp.valid_until >= $2)
					ORDER BY v.plate_number`, u.id, start, end)
				if err != nil {
					return nil, err
				}
				type permit struct {
					id           uuid.UUID
					plate, vtype string
					fee          *int64
				}
				var permits []permit
				for prow.Next() {
					var pm permit
					if prow.Scan(&pm.id, &pm.plate, &pm.vtype, &pm.fee) == nil {
						permits = append(permits, pm)
					}
				}
				prow.Close()
				for _, pm := range permits {
					l := base
					l.Meta = map[string]any{"basis": r.Basis, "period": period, "plate": pm.plate, "vehicle_type": pm.vtype}
					pid := pm.id
					l.ParkingPermitID = &pid
					l.Quantity = 1
					price := r.Rate
					if rt, ok := r.Rates[pm.vtype]; ok {
						price = rt
					}
					if pm.fee != nil {
						price = float64(*pm.fee)
					}
					l.UnitPrice = int64(math.Round(price))
					l.Amount = l.UnitPrice
					l.Description = fmt.Sprintf("%s %s — %s", r.Name, label, pm.plate)
					lines = append(lines, l)
				}
			}
			for i := range lines {
				l := &lines[i]
				if r.BillTo == "tenant" && u.tenantID == nil {
					exc(l, "no_tenant")
				}
				if l.Exception == nil && l.Amount <= 0 {
					exc(l, "zero_amount")
				}
				if l.Exception == nil {
					var billed bool
					_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM billing_run_lines WHERE billing_rule_id = $1 AND unit_location_id = $2 AND period_start = $3 AND invoice_id IS NOT NULL
						AND COALESCE(parking_permit_id, '00000000-0000-0000-0000-000000000000'::uuid) = COALESCE($4::uuid, '00000000-0000-0000-0000-000000000000'::uuid))`, r.ID, u.id, start, l.ParkingPermitID).Scan(&billed)
					if billed {
						exc(l, "already_billed")
					}
				}
				l.TaxAmount = TaxOf(l.Amount, l.TaxRate)
				// dasar rule persentase: termasuk baris yang sudah ditagihkan (agar komponen turunannya juga terdeteksi already_billed)
				if l.Exception == nil || *l.Exception == "already_billed" {
					baseAmount[r.ID.String()+"|"+u.id.String()] += l.Amount
				}
				if l.Exception != nil {
					l.Included = false
					lbl := exceptionLabels[*l.Exception]
					l.ExceptionLabel = &lbl
				}
				out = append(out, *l)
			}
		}
	}
	return out, nil
}

func fmtNum(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return strings.Replace(s, ".", ",", 1)
}

func (s *Service) resolvePeriod(in RunInput) (time.Time, time.Time, error) {
	if in.Period != "" {
		t, err := time.Parse("2006-01", in.Period)
		if err != nil {
			return time.Time{}, time.Time{}, apperr.Validation("period harus YYYY-MM").WithField("period", "format tidak valid")
		}
		return t, t.AddDate(0, 1, -1), nil
	}
	ps, err := parseDate(in.PeriodStart)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	pe, err := parseDate(in.PeriodEnd)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if ps == nil || pe == nil || pe.Before(*ps) {
		return time.Time{}, time.Time{}, apperr.Validation("period atau period_start/period_end wajib").WithField("period", "wajib")
	}
	if pe.Sub(*ps) > 370*24*time.Hour {
		return time.Time{}, time.Time{}, apperr.Validation("periode maksimal 1 tahun")
	}
	return *ps, *pe, nil
}

// CreateRun: preview tersimpan (status preview) — tidak membuat invoice.
func (s *Service) CreateRun(ctx context.Context, in RunInput) (*Run, error) {
	p := authctx.Must(ctx)
	if !p.HasOnProperty("billing.runs.generate", in.PropertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.runs.generate")
	}
	var out *Run
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.createRunTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = s.runTx(ctx, tx, id, true)
		return err
	})
	return out, err
}

func (s *Service) createRunTx(ctx context.Context, tx pgx.Tx, in RunInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if err := s.Profile.RequireCapabilityTx(ctx, tx, in.PropertyID, profile.CapBilling); err != nil {
		return uuid.Nil, err
	}
	start, end, err := s.resolvePeriod(in)
	if err != nil {
		return uuid.Nil, err
	}
	rules, err := s.runRulesTx(ctx, tx, in.PropertyID, in.RuleIDs)
	if err != nil {
		return uuid.Nil, err
	}
	loc := property.PropertyTimezone(ctx, tx, in.PropertyID)
	now := time.Now().In(loc)
	issue := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if in.IssueDate != nil && *in.IssueDate != "" {
		t, err := parseDate(in.IssueDate)
		if err != nil {
			return uuid.Nil, err
		}
		issue = *t
	}
	st := s.settingsFor(ctx, tx, &in.PropertyID)
	dueDays := st.DefaultDueDays
	for _, r := range rules {
		if r.DueDays < dueDays || dueDays == 0 {
			dueDays = r.DueDays
		}
	}
	due := issue.AddDate(0, 0, dueDays)
	if in.DueDate != nil && *in.DueDate != "" {
		t, err := parseDate(in.DueDate)
		if err != nil {
			return uuid.Nil, err
		}
		due = *t
	}
	if due.Before(issue) {
		return uuid.Nil, apperr.Validation("due_date harus setelah issue_date").WithField("due_date", "sebelum issue_date")
	}
	combine := in.Combine == nil || *in.Combine
	itype := in.InvoiceType
	if itype == "" {
		itype = "service_charge"
		if prof, err := s.Profile.ProfileOfTx(ctx, tx, in.PropertyID); err == nil && prof == profile.Apartment {
			itype = "ipl"
		}
	}
	if !invoiceTypes[itype] {
		return uuid.Nil, apperr.Validation("invoice_type tidak valid").WithField("invoice_type", "tidak valid")
	}
	lines, err := s.computeLinesTx(ctx, tx, in.PropertyID, rules, start, end)
	if err != nil {
		return uuid.Nil, err
	}
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixBillingRun, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	source := in.Source
	if source == "" {
		source = "manual"
	}
	ruleIDs := []uuid.UUID{}
	for _, r := range rules {
		ruleIDs = append(ruleIDs, r.ID)
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO billing_runs (organization_id, property_id, run_number, period_start, period_end, source, rule_ids, combine, invoice_type, issue_date, due_date, notes, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13) RETURNING id`, p.OrganizationID, in.PropertyID, number, start, end, source, ruleIDs, combine, itype, issue, due, in.Notes, actorOrNil(p)).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if err := s.insertLinesTx(ctx, tx, id, start, lines); err != nil {
		return uuid.Nil, err
	}
	if err := s.refreshRunTotalsTx(ctx, tx, id); err != nil {
		return uuid.Nil, err
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "billing_run", EntityID: &id, EntityLabel: number, After: map[string]any{"period": start.Format("2006-01"), "lines": len(lines), "source": source}})
	return id, nil
}

func (s *Service) runRulesTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, ruleIDs []uuid.UUID) ([]Rule, error) {
	where, args := ` WHERE r.property_id = $1 AND r.is_active`, []any{propertyID}
	if len(ruleIDs) > 0 {
		args = append(args, ruleIDs)
		where += ` AND r.id = ANY($2)`
	}
	rows, err := tx.Query(ctx, ruleSelect+where+` ORDER BY r.code`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	if len(ruleIDs) > 0 && len(out) != len(ruleIDs) {
		return nil, apperr.Validation("rule_ids harus billing rule aktif di property ini").WithField("rule_ids", "tidak valid")
	}
	if len(out) == 0 {
		return nil, apperr.Validation("Belum ada billing rule aktif untuk property ini").WithField("rule_ids", "kosong")
	}
	return out, rows.Err()
}

func (s *Service) insertLinesTx(ctx context.Context, tx pgx.Tx, runID uuid.UUID, start time.Time, lines []RunLine) error {
	p := authctx.Must(ctx)
	for _, l := range lines {
		meta, _ := json.Marshal(l.Meta)
		if _, err := tx.Exec(ctx, `INSERT INTO billing_run_lines (organization_id, run_id, billing_rule_id, unit_location_id, tenant_id, charge_type, description, quantity, unit_label, unit_price, amount, tax_rate, tax_amount,
			meter_reading_id, parking_permit_id, period_start, meta, exception, included)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`, p.OrganizationID, runID, l.RuleID, l.UnitID, l.TenantID, l.ChargeType, l.Description, l.Quantity, l.UnitMeasure, l.UnitPrice, l.Amount,
			l.TaxRate, l.TaxAmount, l.MeterReadingID, l.ParkingPermitID, start, meta, l.Exception, l.Included); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) refreshRunTotalsTx(ctx context.Context, tx pgx.Tx, runID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE billing_runs r SET line_count = x.n, exception_count = x.exc, subtotal_amount = x.sub, tax_amount = x.tax, total_amount = x.sub + x.tax,
		invoice_count = (SELECT count(DISTINCT invoice_id) FROM billing_run_lines WHERE run_id = r.id AND invoice_id IS NOT NULL)
		FROM (SELECT count(*) AS n, count(*) FILTER (WHERE exception IS NOT NULL) AS exc,
		  COALESCE(sum(amount) FILTER (WHERE exception IS NULL AND included),0) AS sub, COALESCE(sum(tax_amount) FILTER (WHERE exception IS NULL AND included),0) AS tax
		  FROM billing_run_lines WHERE run_id = $1) x WHERE r.id = $1`, runID)
	return err
}

// RefreshRun: hitung ulang preview (mis. setelah luas unit/pembacaan meter dilengkapi).
func (s *Service) RefreshRun(ctx context.Context, id uuid.UUID) (*Run, error) {
	var out *Run
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.runTx(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if !has(r.AllowedActions, "refresh") {
			return apperr.InvalidTransition("Hanya run berstatus preview yang dapat dihitung ulang")
		}
		start, _ := time.Parse("2006-01-02", r.PeriodStart)
		end, _ := time.Parse("2006-01-02", r.PeriodEnd)
		rules, err := s.runRulesTx(ctx, tx, r.PropertyID, r.RuleIDs)
		if err != nil {
			return err
		}
		lines, err := s.computeLinesTx(ctx, tx, r.PropertyID, rules, start, end)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM billing_run_lines WHERE run_id = $1`, id); err != nil {
			return err
		}
		if err := s.insertLinesTx(ctx, tx, id, start, lines); err != nil {
			return err
		}
		if err := s.refreshRunTotalsTx(ctx, tx, id); err != nil {
			return err
		}
		out, err = s.runTx(ctx, tx, id, true)
		return err
	})
	return out, err
}

// ToggleLine: sertakan/kecualikan baris pada preview (baris berpengecualian tidak dapat disertakan).
func (s *Service) ToggleLine(ctx context.Context, runID, lineID uuid.UUID, included bool) (*Run, error) {
	var out *Run
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.runTx(ctx, tx, runID, false)
		if err != nil {
			return err
		}
		if !has(r.AllowedActions, "toggle_line") {
			return apperr.InvalidTransition("Baris hanya dapat diubah saat preview")
		}
		var exc *string
		if err := tx.QueryRow(ctx, `SELECT exception FROM billing_run_lines WHERE id = $1 AND run_id = $2`, lineID, runID).Scan(&exc); err != nil {
			return apperr.NotFound("Baris")
		}
		if included && exc != nil {
			return apperr.Conflict("LINE_HAS_EXCEPTION", "Baris dengan pengecualian ("+exceptionLabels[*exc]+") tidak dapat ditagihkan")
		}
		if _, err := tx.Exec(ctx, `UPDATE billing_run_lines SET included = $3 WHERE id = $1 AND run_id = $2`, lineID, runID, included); err != nil {
			return err
		}
		if err := s.refreshRunTotalsTx(ctx, tx, runID); err != nil {
			return err
		}
		out, err = s.runTx(ctx, tx, runID, true)
		return err
	})
	return out, err
}

// GenerateRun: preview → draft invoice (satu per unit+pihak tagih bila combine).
func (s *Service) GenerateRun(ctx context.Context, id uuid.UUID) (*Run, error) {
	var out *Run
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.generateRunTx(ctx, tx, id); err != nil {
			return err
		}
		var err error
		out, err = s.runTx(ctx, tx, id, true)
		return err
	})
	return out, err
}

func (s *Service) generateRunTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	p := authctx.Must(ctx)
	r, err := s.runTx(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if !p.HasOnProperty("billing.runs.generate", r.PropertyID) {
		return apperr.Forbidden("Memerlukan billing.runs.generate")
	}
	if r.Status != "preview" {
		return apperr.InvalidTransition("Run berstatus " + r.Status + " tidak dapat digenerate")
	}
	type group struct {
		unit     uuid.UUID
		tenant   *uuid.UUID
		charge   string
		lines    []RunLine
		firstIdx int
	}
	groups := map[string]*group{}
	var order []string
	for i, l := range r.Lines {
		if l.Exception != nil || !l.Included {
			continue
		}
		key := l.UnitID.String() + "|"
		if l.TenantID != nil {
			key += l.TenantID.String()
		}
		if !r.Combine {
			key = l.ID.String()
		}
		g, ok := groups[key]
		if !ok {
			g = &group{unit: l.UnitID, tenant: l.TenantID, charge: l.ChargeType, firstIdx: i}
			groups[key] = g
			order = append(order, key)
		}
		g.lines = append(g.lines, l)
	}
	if len(order) == 0 {
		return apperr.Conflict("RUN_EMPTY", "Tidak ada baris yang dapat ditagihkan")
	}
	start, _ := time.Parse("2006-01-02", r.PeriodStart)
	for _, key := range order {
		g := groups[key]
		items := make([]Item, 0, len(g.lines))
		for _, l := range g.lines {
			rate := l.TaxRate
			ct := l.ChargeType
			ruleID := l.RuleID
			st := "billing_run_line"
			lid := l.ID
			items = append(items, Item{Description: l.Description, Quantity: l.Quantity, Unit: l.UnitMeasure, UnitPrice: l.UnitPrice, Amount: l.Amount, ChargeType: &ct, TaxRate: &rate,
				BillingRuleID: &ruleID, MeterReadingID: l.MeterReadingID, ParkingPermitID: l.ParkingPermitID, SourceType: &st, SourceID: &lid, Meta: l.Meta})
		}
		itype := r.InvoiceType
		if !r.Combine {
			itype = g.charge
		}
		desc := "Tagihan periode " + monthLabel(start)
		unit := g.unit
		invID, err := s.CreateTx(ctx, tx, InvoiceInput{PropertyID: &r.PropertyID, TenantID: g.tenant, UnitLocationID: &unit, InvoiceType: &itype, PeriodStart: &r.PeriodStart, PeriodEnd: &r.PeriodEnd,
			Description: &desc, DueDate: &r.DueDate, Items: &items, BillingRunID: &r.ID}, "billing_run", &r.ID)
		if err != nil {
			return err
		}
		lineIDs := []uuid.UUID{}
		for _, l := range g.lines {
			lineIDs = append(lineIDs, l.ID)
		}
		if _, err := tx.Exec(ctx, `UPDATE billing_run_lines SET invoice_id = $1 WHERE id = ANY($2)`, invID, lineIDs); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("ALREADY_BILLED", "Sebagian unit sudah ditagihkan pada periode ini oleh run lain; hitung ulang preview")
			}
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE billing_runs SET status = 'generated', generated_at = now(), updated_by = $2 WHERE id = $1`, id, actorOrNil(p)); err != nil {
		return err
	}
	if err := s.refreshRunTotalsTx(ctx, tx, id); err != nil {
		return err
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "billing_run", EntityID: &id, EntityLabel: r.RunNumber, Before: map[string]any{"status": "preview"}, After: map[string]any{"status": "generated", "invoices": len(order)}})
	if s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventBillingRunGenerated, OrganizationID: p.OrganizationID, PropertyID: &r.PropertyID, ObjectType: "billing_run", ObjectID: id, ObjectLabel: r.RunNumber,
			Payload: map[string]any{"domain": "finance", "invoices": len(order), "period": r.PeriodLabel}})
	}
	return nil
}

// IssueRun: terbitkan seluruh draft invoice run (nomor diberikan saat terbit, D-P4-01).
func (s *Service) IssueRun(ctx context.Context, id uuid.UUID) (*Run, error) {
	p := authctx.Must(ctx)
	var out *Run
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.runTx(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if !has(r.AllowedActions, "issue") {
			return apperr.InvalidTransition("Run berstatus " + r.Status + " tidak dapat diterbitkan")
		}
		rows, err := tx.Query(ctx, `SELECT id FROM invoices WHERE billing_run_id = $1 AND status = 'draft' ORDER BY created_at, id`, id)
		if err != nil {
			return err
		}
		var invIDs []uuid.UUID
		for rows.Next() {
			var iid uuid.UUID
			if rows.Scan(&iid) == nil {
				invIDs = append(invIDs, iid)
			}
		}
		rows.Close()
		for _, iid := range invIDs {
			if _, err := s.issueTx(ctx, tx, iid); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE billing_runs SET status = 'issued', issued_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "billing_run", EntityID: &id, EntityLabel: r.RunNumber, Before: map[string]any{"status": r.Status}, After: map[string]any{"status": "issued", "invoices": len(invIDs)}})
		out, err = s.runTx(ctx, tx, id, true)
		return err
	})
	return out, err
}

// CancelRun: preview → cancelled; generated → draft invoice dibatalkan (baris dilepas).
func (s *Service) CancelRun(ctx context.Context, id uuid.UUID, reason string) (*Run, error) {
	p := authctx.Must(ctx)
	var out *Run
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.runTx(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if !has(r.AllowedActions, "cancel") {
			return apperr.InvalidTransition("Run berstatus " + r.Status + " tidak dapat dibatalkan")
		}
		if strings.TrimSpace(reason) == "" {
			reason = "Billing run dibatalkan"
		}
		rows, err := tx.Query(ctx, `SELECT id FROM invoices WHERE billing_run_id = $1 AND status = 'draft'`, id)
		if err != nil {
			return err
		}
		var invIDs []uuid.UUID
		for rows.Next() {
			var iid uuid.UUID
			if rows.Scan(&iid) == nil {
				invIDs = append(invIDs, iid)
			}
		}
		rows.Close()
		for _, iid := range invIDs {
			inv, err := s.getTx(ctx, tx, iid)
			if err != nil {
				return err
			}
			if err := s.cancelTx(ctx, tx, inv, reason, "cancel"); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE billing_runs SET status = 'cancelled', cancelled_at = now(), notes = COALESCE(notes || E'\n', '') || $2, updated_by = $3 WHERE id = $1`, id, reason, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "billing_run", EntityID: &id, EntityLabel: r.RunNumber, Before: map[string]any{"status": r.Status}, After: map[string]any{"status": "cancelled", "reason": reason}})
		out, err = s.runTx(ctx, tx, id, true)
		return err
	})
	return out, err
}

func (s *Service) GetRun(ctx context.Context, id uuid.UUID) (*Run, error) {
	var out *Run
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.runTx(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasAnyOnProperty("billing.runs.view", r.PropertyID) {
			return apperr.Forbidden("")
		}
		out = r
		return nil
	})
	return out, err
}

func (s *Service) ListRuns(ctx context.Context, propertyID *uuid.UUID, statuses []string, page httpx.Page) ([]Run, *string, error) {
	p := authctx.Must(ctx)
	var out []Run
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args := " WHERE true", []any{}
		if propertyID != nil {
			if !p.HasAnyOnProperty("billing.runs.view", *propertyID) {
				return apperr.Forbidden("")
			}
			args = append(args, *propertyID)
			where += fmt.Sprintf(" AND r.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("billing.runs.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND r.property_id = ANY($%d)", len(args))
		}
		if len(statuses) > 0 {
			args = append(args, statuses)
			where += fmt.Sprintf(" AND r.status = ANY($%d)", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (r.created_at, r.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, runSelect+where+fmt.Sprintf(" ORDER BY r.created_at DESC, r.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRun(rows)
			if err != nil {
				return err
			}
			s.runActions(ctx, r)
			out = append(out, *r)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	if out == nil {
		out = []Run{}
	}
	return out, next, err
}

// AutoGenerateSweep (worker, harian/6 jam): rule auto_generate → run terjadwal periode berjalan pada/ setelah issue_day,
// langsung menjadi draft invoice untuk direview Finance (P4-BRL-03). Satu run terjadwal per property × periode.
func (s *Service) AutoGenerateSweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	type prop struct {
		id       uuid.UUID
		issueDay int
		rules    []uuid.UUID
	}
	var props []prop
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT property_id, min(issue_day), array_agg(id) FROM billing_rules WHERE is_active AND auto_generate GROUP BY property_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p prop
			if rows.Scan(&p.id, &p.issueDay, &p.rules) == nil {
				props = append(props, p)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, pr := range props {
		err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
			loc := property.PropertyTimezone(ctx, tx, pr.id)
			now := time.Now().In(loc)
			if now.Day() < pr.issueDay {
				return nil
			}
			period := now.Format("2006-01")
			start, _ := time.Parse("2006-01", period)
			var exists bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM billing_runs WHERE property_id = $1 AND period_start = $2 AND source = 'scheduled' AND status <> 'cancelled')`, pr.id, start).Scan(&exists)
			if exists {
				return nil
			}
			id, err := s.createRunTx(ctx, tx, RunInput{PropertyID: pr.id, Period: period, RuleIDs: pr.rules, Source: "scheduled"})
			if err != nil {
				return err
			}
			var lines int
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM billing_run_lines WHERE run_id = $1 AND exception IS NULL AND included`, id).Scan(&lines)
			if lines > 0 {
				if err := s.generateRunTx(ctx, tx, id); err != nil {
					return err
				}
			}
			n++
			return nil
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
