package billing

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Denda keterlambatan (PRD P4 v2.1 §5.5 P4-PND-01..02) ----------
// Aturan per property (persen/nominal, per hari/bulan/sekali, masa tenggang, batas). Worker menghitung akrual pada invoice
// overdue (monoton naik, berhenti saat lunas → final). Denda ditagihkan lewat invoice penalty (item bersumber invoice_penalty;
// hanya selisih yang belum ditagihkan), dapat di-waive dengan alasan & izin billing.penalties.waive.

type PenaltyRule struct {
	ID           uuid.UUID `json:"id"`
	PropertyID   uuid.UUID `json:"property_id"`
	PropertyName string    `json:"property_name"`
	Name         string    `json:"name"`
	Method       string    `json:"method"` // percent | fixed
	Rate         float64   `json:"rate"`
	Period       string    `json:"period"` // per_day | per_month | once
	GraceDays    int       `json:"grace_days"`
	MaxAmount    *int64    `json:"max_amount"`
	MaxPct       *float64  `json:"max_pct"`
	InvoiceTypes []string  `json:"invoice_types"`
	IsActive     bool      `json:"is_active"`
	Summary      string    `json:"summary"`
	Version      int       `json:"version"`
}

const penaltyRuleSelect = `SELECT r.id, r.property_id, pl.name, r.name, r.method, r.rate::float8, r.period, r.grace_days, r.max_amount, r.max_pct::float8, r.invoice_types, r.is_active, r.version
	FROM penalty_rules r JOIN locations pl ON pl.id = r.property_id`

func scanPenaltyRule(row pgx.Row) (*PenaltyRule, error) {
	var r PenaltyRule
	if err := row.Scan(&r.ID, &r.PropertyID, &r.PropertyName, &r.Name, &r.Method, &r.Rate, &r.Period, &r.GraceDays, &r.MaxAmount, &r.MaxPct, &r.InvoiceTypes, &r.IsActive, &r.Version); err != nil {
		return nil, err
	}
	if r.InvoiceTypes == nil {
		r.InvoiceTypes = []string{}
	}
	r.Summary = penaltySummary(&r)
	return &r, nil
}

// penaltySummary: kalimat aturan untuk UI, mis. "2% per bulan dari sisa tagihan setelah 7 hari, maks. 10%".
func penaltySummary(r *PenaltyRule) string {
	var b strings.Builder
	if r.Method == "percent" {
		b.WriteString(strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", r.Rate), "0"), ".") + "% ")
	} else {
		b.WriteString(rupiahText(int64(r.Rate)) + " ")
	}
	b.WriteString(map[string]string{"per_day": "per hari", "per_month": "per bulan", "once": "sekali"}[r.Period])
	if r.Method == "percent" {
		b.WriteString(" dari sisa tagihan")
	}
	if r.GraceDays > 0 {
		fmt.Fprintf(&b, " setelah %d hari", r.GraceDays)
	}
	if r.MaxPct != nil {
		fmt.Fprintf(&b, ", maks. %s%%", strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", *r.MaxPct), "0"), "."))
	}
	if r.MaxAmount != nil {
		b.WriteString(", maks. " + rupiahText(*r.MaxAmount))
	}
	return b.String()
}

func rupiahText(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprint(v)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-Rp" + string(out)
	}
	return "Rp" + string(out)
}

func (s *Service) ListPenaltyRules(ctx context.Context, propertyID *uuid.UUID) ([]PenaltyRule, error) {
	p := authctx.Must(ctx)
	out := []PenaltyRule{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args := " WHERE true", []any{}
		if propertyID != nil {
			if !p.HasAnyOnProperty("billing.penalties.view", *propertyID) {
				return apperr.Forbidden("")
			}
			args = append(args, *propertyID)
			where += " AND r.property_id = $1"
		} else if pids, all := p.PropertyIDsFor("billing.penalties.view"); !all {
			args = append(args, pids)
			where += " AND r.property_id = ANY($1)"
		}
		rows, err := tx.Query(ctx, penaltyRuleSelect+where+" ORDER BY pl.name, r.name", args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanPenaltyRule(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}

type PenaltyRuleInput struct {
	PropertyID   *uuid.UUID `json:"property_id"`
	Name         *string    `json:"name"`
	Method       *string    `json:"method"`
	Rate         *float64   `json:"rate"`
	Period       *string    `json:"period"`
	GraceDays    *int       `json:"grace_days"`
	MaxAmount    *int64     `json:"max_amount"`
	MaxPct       *float64   `json:"max_pct"`
	ClearMax     bool       `json:"clear_max"`
	InvoiceTypes *[]string  `json:"invoice_types"`
	IsActive     *bool      `json:"is_active"`
}

func (s *Service) SavePenaltyRule(ctx context.Context, id *uuid.UUID, in PenaltyRuleInput) (*PenaltyRule, error) {
	p := authctx.Must(ctx)
	var out *PenaltyRule
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		cur := &PenaltyRule{Method: "percent", Period: "per_month", IsActive: true, InvoiceTypes: []string{}}
		if id != nil {
			c, err := scanPenaltyRule(tx.QueryRow(ctx, penaltyRuleSelect+` WHERE r.id = $1 FOR UPDATE OF r`, *id))
			if err != nil {
				return apperr.NotFound("Aturan denda")
			}
			cur = c
		} else {
			if in.PropertyID == nil {
				return apperr.Validation("property_id wajib").WithField("property_id", "wajib")
			}
			cur.PropertyID = *in.PropertyID
		}
		if !p.HasOnProperty("billing.penalties.manage", cur.PropertyID) {
			return apperr.Forbidden("Memerlukan billing.penalties.manage")
		}
		if in.Name != nil {
			cur.Name = strings.TrimSpace(*in.Name)
		}
		if in.Method != nil {
			cur.Method = *in.Method
		}
		if in.Rate != nil {
			cur.Rate = *in.Rate
		}
		if in.Period != nil {
			cur.Period = *in.Period
		}
		if in.GraceDays != nil {
			cur.GraceDays = *in.GraceDays
		}
		if in.ClearMax {
			cur.MaxAmount, cur.MaxPct = nil, nil
		}
		if in.MaxAmount != nil {
			cur.MaxAmount = in.MaxAmount
		}
		if in.MaxPct != nil {
			cur.MaxPct = in.MaxPct
		}
		if in.InvoiceTypes != nil {
			cur.InvoiceTypes = *in.InvoiceTypes
		}
		if in.IsActive != nil {
			cur.IsActive = *in.IsActive
		}
		switch {
		case cur.Name == "":
			return apperr.Validation("name wajib").WithField("name", "wajib")
		case cur.Method != "percent" && cur.Method != "fixed":
			return apperr.Validation("method harus percent atau fixed").WithField("method", "tidak valid")
		case cur.Period != "per_day" && cur.Period != "per_month" && cur.Period != "once":
			return apperr.Validation("period harus per_day, per_month, atau once").WithField("period", "tidak valid")
		case cur.Rate <= 0 || (cur.Method == "percent" && cur.Rate > 100):
			return apperr.Validation("rate harus > 0 (persen maks. 100)").WithField("rate", "tidak valid")
		case cur.GraceDays < 0 || cur.GraceDays > 365:
			return apperr.Validation("grace_days harus 0..365").WithField("grace_days", "tidak valid")
		case cur.MaxAmount != nil && *cur.MaxAmount <= 0:
			return apperr.Validation("max_amount harus > 0").WithField("max_amount", "tidak valid")
		case cur.MaxPct != nil && (*cur.MaxPct <= 0 || *cur.MaxPct > 100):
			return apperr.Validation("max_pct harus 0..100").WithField("max_pct", "tidak valid")
		}
		for _, t := range cur.InvoiceTypes {
			if !invoiceTypes[t] || t == "penalty" {
				return apperr.Validation("invoice_types tidak valid").WithField("invoice_types", t)
			}
		}
		var rid uuid.UUID
		if id == nil {
			if err := tx.QueryRow(ctx, `INSERT INTO penalty_rules (organization_id, property_id, name, method, rate, period, grace_days, max_amount, max_pct, invoice_types, is_active, created_by, updated_by)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12) RETURNING id`, p.OrganizationID, cur.PropertyID, cur.Name, cur.Method, cur.Rate, cur.Period, cur.GraceDays, cur.MaxAmount, cur.MaxPct, cur.InvoiceTypes, cur.IsActive, p.UserID).Scan(&rid); err != nil {
				return err
			}
		} else {
			rid = *id
			if _, err := tx.Exec(ctx, `UPDATE penalty_rules SET name=$2, method=$3, rate=$4, period=$5, grace_days=$6, max_amount=$7, max_pct=$8, invoice_types=$9, is_active=$10, updated_by=$11 WHERE id = $1`,
				rid, cur.Name, cur.Method, cur.Rate, cur.Period, cur.GraceDays, cur.MaxAmount, cur.MaxPct, cur.InvoiceTypes, cur.IsActive, p.UserID); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditConfigChange, EntityType: "penalty_rule", EntityID: &rid, EntityLabel: cur.Name, After: in})
		var err error
		out, err = scanPenaltyRule(tx.QueryRow(ctx, penaltyRuleSelect+` WHERE r.id = $1`, rid))
		return err
	})
	return out, err
}

// PenaltyAmount: denda untuk sisa tagihan `base`, total invoice `total`, hari terlambat setelah masa tenggang `days`.
func PenaltyAmount(r *PenaltyRule, base, total int64, days int) int64 {
	if days <= 0 || base <= 0 {
		return 0
	}
	units := 1.0
	switch r.Period {
	case "per_day":
		units = float64(days)
	case "per_month":
		units = math.Ceil(float64(days) / 30)
	}
	var amt float64
	if r.Method == "percent" {
		amt = float64(base) * r.Rate / 100 * units
	} else {
		amt = r.Rate * units
	}
	v := int64(math.Round(amt))
	if r.MaxAmount != nil && v > *r.MaxAmount {
		v = *r.MaxAmount
	}
	if r.MaxPct != nil {
		if c := int64(math.Round(float64(total) * *r.MaxPct / 100)); v > c {
			v = c
		}
	}
	return v
}

// AccruePenaltiesTx (worker): hitung akrual denda semua invoice overdue (zona waktu property).
func (s *Service) AccruePenaltiesTx(ctx context.Context, tx pgx.Tx) (int, error) {
	rows, err := tx.Query(ctx, penaltyRuleSelect+` WHERE r.is_active`)
	if err != nil {
		return 0, err
	}
	rules := map[uuid.UUID][]PenaltyRule{}
	for rows.Next() {
		r, err := scanPenaltyRule(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		rules[r.PropertyID] = append(rules[r.PropertyID], *r)
	}
	rows.Close()
	if len(rules) == 0 {
		return 0, nil
	}
	type inv struct {
		id, propertyID uuid.UUID
		itype          string
		total, settled int64
		daysLate       int
	}
	var invs []inv
	rows, err = tx.Query(ctx, `SELECT i.id, i.property_id, i.invoice_type, i.total_amount, i.paid_amount + i.credited_amount,
		(now() AT TIME ZONE pr.timezone)::date - COALESCE(i.due_date, (i.due_at AT TIME ZONE pr.timezone)::date)
		FROM invoices i JOIN properties pr ON pr.location_id = i.property_id WHERE i.status = 'overdue' AND i.invoice_type <> 'penalty'`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var v inv
		if rows.Scan(&v.id, &v.propertyID, &v.itype, &v.total, &v.settled, &v.daysLate) == nil {
			invs = append(invs, v)
		}
	}
	rows.Close()
	p := authctx.Must(ctx)
	n := 0
	for _, v := range invs {
		for i := range rules[v.propertyID] {
			r := &rules[v.propertyID][i]
			if len(r.InvoiceTypes) > 0 && !has(r.InvoiceTypes, v.itype) {
				continue
			}
			days := v.daysLate - r.GraceDays
			amt := PenaltyAmount(r, v.total-v.settled, v.total, days)
			if amt <= 0 {
				continue
			}
			tag, err := tx.Exec(ctx, `INSERT INTO invoice_penalties (organization_id, property_id, invoice_id, penalty_rule_id, accrued_amount, days_late, last_calculated_at)
				VALUES ($1,$2,$3,$4,$5,$6,now())
				ON CONFLICT (invoice_id, penalty_rule_id) DO UPDATE SET accrued_amount = GREATEST(invoice_penalties.accrued_amount, EXCLUDED.accrued_amount), days_late = EXCLUDED.days_late, last_calculated_at = now()
				WHERE invoice_penalties.status = 'accruing' AND (invoice_penalties.accrued_amount < EXCLUDED.accrued_amount OR invoice_penalties.days_late <> EXCLUDED.days_late)`,
				p.OrganizationID, v.propertyID, v.id, r.ID, amt, v.daysLate)
			if err != nil {
				return n, err
			}
			n += int(tag.RowsAffected())
		}
	}
	return n, nil
}

type Penalty struct {
	ID             uuid.UUID  `json:"id"`
	PropertyID     uuid.UUID  `json:"property_id"`
	InvoiceID      uuid.UUID  `json:"invoice_id"`
	InvoiceNumber  *string    `json:"invoice_number"`
	InvoiceStatus  string     `json:"invoice_status"`
	TenantID       *uuid.UUID `json:"tenant_id"`
	TenantName     *string    `json:"tenant_name"`
	UnitLocationID *uuid.UUID `json:"unit_location_id"`
	UnitLabel      *string    `json:"unit_label"`
	RuleID         uuid.UUID  `json:"penalty_rule_id"`
	RuleName       string     `json:"rule_name"`
	AccruedAmount  int64      `json:"accrued_amount"`
	BilledAmount   int64      `json:"billed_amount"`
	UnbilledAmount int64      `json:"unbilled_amount"`
	DaysLate       int        `json:"days_late"`
	Status         string     `json:"status"`
	BilledInvoice  *string    `json:"billed_invoice_number"`
	WaiveReason    *string    `json:"waive_reason"`
	WaivedByName   *string    `json:"waived_by_name"`
	WaivedAt       *time.Time `json:"waived_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	AllowedActions []string   `json:"allowed_actions"`
}

const penaltyBilledSQL = `COALESCE((SELECT sum(it.amount) FROM invoice_items it JOIN invoices bi ON bi.id = it.invoice_id WHERE it.source_type = 'invoice_penalty' AND it.source_id = ip.id AND bi.status <> 'cancelled'),0)`

const penaltySelect = `SELECT ip.id, ip.property_id, ip.invoice_id, i.invoice_number, i.status, i.tenant_id, t.name, i.unit_location_id, COALESCE('Unit ' || un.unit_number, l.name), ip.penalty_rule_id, r.name,
	ip.accrued_amount, ` + penaltyBilledSQL + `, ip.days_late, ip.status, bi.invoice_number, ip.waive_reason, wb.full_name, ip.waived_at, ip.updated_at
	FROM invoice_penalties ip JOIN invoices i ON i.id = ip.invoice_id JOIN penalty_rules r ON r.id = ip.penalty_rule_id LEFT JOIN tenants t ON t.id = i.tenant_id
	LEFT JOIN locations l ON l.id = i.unit_location_id LEFT JOIN units un ON un.location_id = i.unit_location_id LEFT JOIN invoices bi ON bi.id = ip.billed_invoice_id LEFT JOIN users wb ON wb.id = ip.waived_by`

func scanPenalty(row pgx.Row) (*Penalty, error) {
	var v Penalty
	if err := row.Scan(&v.ID, &v.PropertyID, &v.InvoiceID, &v.InvoiceNumber, &v.InvoiceStatus, &v.TenantID, &v.TenantName, &v.UnitLocationID, &v.UnitLabel, &v.RuleID, &v.RuleName,
		&v.AccruedAmount, &v.BilledAmount, &v.DaysLate, &v.Status, &v.BilledInvoice, &v.WaiveReason, &v.WaivedByName, &v.WaivedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	v.UnbilledAmount = v.AccruedAmount - v.BilledAmount
	if v.UnbilledAmount < 0 || v.Status == "waived" {
		v.UnbilledAmount = 0
	}
	return &v, nil
}

func (s *Service) penaltyActions(ctx context.Context, v *Penalty) {
	p := authctx.Must(ctx)
	v.AllowedActions = []string{"view"}
	if v.Status == "accruing" || v.Status == "final" {
		if v.UnbilledAmount > 0 && p.HasOnProperty("billing.penalties.manage", v.PropertyID) {
			v.AllowedActions = append(v.AllowedActions, "bill")
		}
		if p.HasOnProperty("billing.penalties.waive", v.PropertyID) {
			v.AllowedActions = append(v.AllowedActions, "waive")
		}
	}
}

type PenaltyFilter struct {
	PropertyID *uuid.UUID
	InvoiceID  *uuid.UUID
	TenantID   *uuid.UUID
	Statuses   []string
	Unbilled   bool
}

func (s *Service) ListPenalties(ctx context.Context, f PenaltyFilter, page httpx.Page) ([]Penalty, *string, error) {
	p := authctx.Must(ctx)
	var out []Penalty
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var args []any
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE true"
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("billing.penalties.view", *f.PropertyID) {
				return apperr.Forbidden("")
			}
			where += " AND ip.property_id = " + add(*f.PropertyID)
		} else if pids, all := p.PropertyIDsFor("billing.penalties.view"); !all {
			where += " AND ip.property_id = ANY(" + add(pids) + ")"
		}
		if f.InvoiceID != nil {
			where += " AND ip.invoice_id = " + add(*f.InvoiceID)
		}
		if f.TenantID != nil {
			where += " AND i.tenant_id = " + add(*f.TenantID)
		}
		if len(f.Statuses) > 0 {
			where += " AND ip.status = ANY(" + add(f.Statuses) + ")"
		}
		if f.Unbilled {
			where += " AND ip.status IN ('accruing','final') AND ip.accrued_amount > " + penaltyBilledSQL
		}
		if page.Cursor != nil {
			where += fmt.Sprintf(" AND (ip.updated_at, ip.id) < (%s::timestamptz, %s)", add(page.Cursor.Value), add(page.Cursor.ID))
		}
		rows, err := tx.Query(ctx, penaltySelect+where+fmt.Sprintf(" ORDER BY ip.updated_at DESC, ip.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanPenalty(rows)
			if err != nil {
				return err
			}
			s.penaltyActions(ctx, v)
			out = append(out, *v)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.UpdatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	if out == nil {
		out = []Penalty{}
	}
	return out, next, err
}

func (s *Service) WaivePenalty(ctx context.Context, id uuid.UUID, reason string) (*Penalty, error) {
	p := authctx.Must(ctx)
	var out *Penalty
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		v, err := scanPenalty(tx.QueryRow(ctx, penaltySelect+` WHERE ip.id = $1 FOR UPDATE OF ip`, id))
		if err != nil {
			return apperr.NotFound("Denda")
		}
		s.penaltyActions(ctx, v)
		if !has(v.AllowedActions, "waive") {
			if !p.HasOnProperty("billing.penalties.waive", v.PropertyID) {
				return apperr.Forbidden("Memerlukan billing.penalties.waive")
			}
			return apperr.InvalidTransition("Denda berstatus " + v.Status + " tidak dapat dihapus")
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return apperr.Validation("reason wajib").WithField("reason", "wajib")
		}
		if _, err := tx.Exec(ctx, `UPDATE invoice_penalties SET status = 'waived', waive_reason = $2, waived_by = $3, waived_at = now() WHERE id = $1`, id, reason, p.UserID); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "invoice", ObjectID: v.InvoiceID, Action: "penalty_waived", Payload: map[string]any{"penalty_id": id, "amount": v.UnbilledAmount, "reason": reason}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "invoice_penalty", EntityID: &id, EntityLabel: strPtrVal(v.InvoiceNumber), Before: map[string]any{"status": v.Status, "unbilled": v.UnbilledAmount}, After: map[string]any{"status": "waived", "reason": reason}})
		out, err = scanPenalty(tx.QueryRow(ctx, penaltySelect+` WHERE ip.id = $1`, id))
		if err == nil {
			s.penaltyActions(ctx, out)
		}
		return err
	})
	return out, err
}

type BillPenaltiesInput struct {
	PropertyID uuid.UUID   `json:"property_id"`
	PenaltyIDs []uuid.UUID `json:"penalty_ids"` // kosong = semua denda belum ditagihkan di property
	TenantID   *uuid.UUID  `json:"tenant_id"`
	DueDate    *string     `json:"due_date"`
	IssueNow   bool        `json:"issue_now"`
}

type BillPenaltiesResult struct {
	InvoiceIDs []uuid.UUID `json:"invoice_ids"`
	Penalties  int         `json:"penalties"`
	Total      int64       `json:"total_amount"`
}

// BillPenalties: tagihkan selisih denda belum ditagihkan → satu invoice penalty per tenant/unit (P4-PND-02).
func (s *Service) BillPenalties(ctx context.Context, in BillPenaltiesInput) (*BillPenaltiesResult, error) {
	p := authctx.Must(ctx)
	if !p.HasOnProperty("billing.penalties.manage", in.PropertyID) || !p.HasOnProperty("billing.invoices.create", in.PropertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.penalties.manage dan billing.invoices.create")
	}
	if in.IssueNow && !p.HasOnProperty("billing.invoices.issue", in.PropertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.invoices.issue")
	}
	out := &BillPenaltiesResult{InvoiceIDs: []uuid.UUID{}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args := ` WHERE ip.property_id = $1 AND ip.status IN ('accruing','final') AND ip.accrued_amount > `+penaltyBilledSQL, []any{in.PropertyID}
		if len(in.PenaltyIDs) > 0 {
			args = append(args, in.PenaltyIDs)
			where += fmt.Sprintf(" AND ip.id = ANY($%d)", len(args))
		}
		if in.TenantID != nil {
			args = append(args, *in.TenantID)
			where += fmt.Sprintf(" AND i.tenant_id = $%d", len(args))
		}
		rows, err := tx.Query(ctx, penaltySelect+where+` ORDER BY i.tenant_id, i.unit_location_id, i.due_at FOR UPDATE OF ip`, args...)
		if err != nil {
			return err
		}
		var list []Penalty
		for rows.Next() {
			v, err := scanPenalty(rows)
			if err != nil {
				rows.Close()
				return err
			}
			list = append(list, *v)
		}
		rows.Close()
		if len(list) == 0 {
			return apperr.Conflict("NOTHING_TO_BILL", "Tidak ada denda yang belum ditagihkan")
		}
		type grp struct {
			tenant, unit *uuid.UUID
			items        []Item
			ids          []uuid.UUID
		}
		groups := map[string]*grp{}
		var order []string
		for _, v := range list {
			key := ""
			if v.TenantID != nil {
				key += v.TenantID.String()
			}
			key += "|"
			if v.UnitLocationID != nil {
				key += v.UnitLocationID.String()
			}
			g, ok := groups[key]
			if !ok {
				g = &grp{tenant: v.TenantID, unit: v.UnitLocationID}
				groups[key] = g
				order = append(order, key)
			}
			zero := 0.0
			ct := "penalty"
			st := "invoice_penalty"
			pid := v.ID
			g.items = append(g.items, Item{Description: fmt.Sprintf("Denda keterlambatan %s (%d hari) — %s", strPtrVal(v.InvoiceNumber), v.DaysLate, v.RuleName), Quantity: 1,
				UnitPrice: v.UnbilledAmount, Amount: v.UnbilledAmount, ChargeType: &ct, TaxRate: &zero, SourceType: &st, SourceID: &pid,
				Meta: map[string]any{"invoice_id": v.InvoiceID, "invoice_number": strPtrVal(v.InvoiceNumber), "days_late": v.DaysLate, "accrued": v.AccruedAmount, "previously_billed": v.BilledAmount}})
			g.ids = append(g.ids, v.ID)
			out.Penalties++
			out.Total += v.UnbilledAmount
		}
		due := in.DueDate
		if due == nil || *due == "" {
			loc := property.PropertyTimezone(ctx, tx, in.PropertyID)
			st := s.settingsFor(ctx, tx, &in.PropertyID)
			d := time.Now().In(loc).AddDate(0, 0, st.DefaultDueDays).Format("2006-01-02")
			due = &d
		}
		itype := "penalty"
		desc := "Denda keterlambatan pembayaran"
		f := false
		for _, key := range order {
			g := groups[key]
			items := g.items
			invID, err := s.CreateTx(ctx, tx, InvoiceInput{PropertyID: &in.PropertyID, TenantID: g.tenant, UnitLocationID: g.unit, InvoiceType: &itype, Description: &desc, DueDate: due, Items: &items, ApplyTax: &f, IssueNow: in.IssueNow}, "penalty", nil)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE invoice_penalties SET billed_invoice_id = $2, status = CASE WHEN status = 'final' THEN 'billed' ELSE status END WHERE id = ANY($1)`, g.ids, invID); err != nil {
				return err
			}
			out.InvoiceIDs = append(out.InvoiceIDs, invID)
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "invoice_penalty", EntityLabel: "Tagih denda", After: map[string]any{"penalties": out.Penalties, "invoices": len(out.InvoiceIDs), "total": out.Total}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

var _ = db.IsNoRows
