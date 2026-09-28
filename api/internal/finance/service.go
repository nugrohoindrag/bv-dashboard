// Package finance: budget vs actual, biaya operasional manual, pemetaan akun + ekspor siap-jurnal, dan webhook keluar untuk
// integrasi sistem akuntansi (PRD P4 v2.1 §8 P4-BGT-*, P4-CST-02..04, §9 P4-INT-02, P4-INT-04; D-P4-03, D-P4-07).
// Bukan general ledger (Boundary §29): tidak ada chart of accounts/laporan keuangan formal — hanya ekspor jurnal ke sistem
// akuntansi pelanggan.
package finance

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/jobs"
)

type Service struct {
	DB          *db.DB
	Jobs        jobs.Enqueuer
	Attachments *attachments.Service
	HTTPClient  *http.Client
	// AllowInsecureWebhooks: izinkan URL http/localhost (lingkungan non-produksi & test).
	AllowInsecureWebhooks bool
}

func New(d *db.DB, j jobs.Enqueuer, att *attachments.Service) *Service {
	return &Service{DB: d, Jobs: j, Attachments: att, HTTPClient: &http.Client{Timeout: 10 * time.Second}}
}

type Category struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Kategori pendapatan = tipe komponen tagihan (tanpa deposit — kewajiban, bukan pendapatan).
var RevenueCategories = []Category{{"service_charge", "Service Charge"}, {"ipl", "IPL"}, {"electricity", "Listrik"}, {"water", "Air"}, {"utility", "Utilitas"},
	{"parking", "Parkir"}, {"sinking_fund", "Sinking Fund"}, {"penalty", "Denda"}, {"rental", "Sewa"}, {"facility", "Fasilitas"}, {"additional_charge", "Biaya Tambahan"}, {"other", "Lainnya"}}

// Kategori biaya operasional (P4-CST-02).
var CostCategories = []Category{{"maintenance", "Maintenance"}, {"utility", "Utilitas"}, {"security", "Keamanan"}, {"cleaning", "Kebersihan"}, {"staff", "Staf"}, {"admin", "Administrasi"}, {"other", "Lainnya"}}

func catLabel(kind, key string) string {
	list := RevenueCategories
	if kind == "cost" {
		list = CostCategories
	}
	for _, c := range list {
		if c.Key == key {
			return c.Label
		}
	}
	return key
}

func validCat(kind, key string) bool {
	list := RevenueCategories
	if kind == "cost" {
		list = CostCategories
	} else if kind != "revenue" {
		return false
	}
	for _, c := range list {
		if c.Key == key {
			return true
		}
	}
	return false
}

func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// ---------- Budget (P4-BGT-01) ----------

type BudgetLine struct {
	Kind     string    `json:"kind"`
	Category string    `json:"category"`
	Label    string    `json:"label"`
	Months   [12]int64 `json:"months"`
	Total    int64     `json:"total"`
}

type Budget struct {
	ID             uuid.UUID    `json:"id"`
	PropertyID     uuid.UUID    `json:"property_id"`
	PropertyName   string       `json:"property_name"`
	FiscalYear     int          `json:"fiscal_year"`
	Revision       int          `json:"revision"`
	Name           *string      `json:"name"`
	Status         string       `json:"status"`
	Notes          *string      `json:"notes"`
	ApprovedByName *string      `json:"approved_by_name"`
	ApprovedAt     *time.Time   `json:"approved_at"`
	CreatedByName  *string      `json:"created_by_name"`
	CreatedAt      time.Time    `json:"created_at"`
	RevenueTotal   int64        `json:"revenue_total"`
	CostTotal      int64        `json:"cost_total"`
	Lines          []BudgetLine `json:"lines,omitempty"`
	AllowedActions []string     `json:"allowed_actions"`
	Version        int          `json:"version"`
}

const budgetSelect = `SELECT b.id, b.property_id, pl.name, b.fiscal_year, b.revision, b.name, b.status, b.notes, ab.full_name, b.approved_at, cb.full_name, b.created_at,
	COALESCE((SELECT sum(amount) FROM budget_lines x WHERE x.budget_id = b.id AND x.kind = 'revenue'),0), COALESCE((SELECT sum(amount) FROM budget_lines x WHERE x.budget_id = b.id AND x.kind = 'cost'),0), b.version
	FROM budgets b JOIN locations pl ON pl.id = b.property_id LEFT JOIN users ab ON ab.id = b.approved_by LEFT JOIN users cb ON cb.id = b.created_by`

func scanBudget(row pgx.Row) (*Budget, error) {
	var b Budget
	if err := row.Scan(&b.ID, &b.PropertyID, &b.PropertyName, &b.FiscalYear, &b.Revision, &b.Name, &b.Status, &b.Notes, &b.ApprovedByName, &b.ApprovedAt, &b.CreatedByName, &b.CreatedAt,
		&b.RevenueTotal, &b.CostTotal, &b.Version); err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Service) budgetActions(ctx context.Context, b *Budget) {
	p := authctx.Must(ctx)
	b.AllowedActions = []string{"view"}
	manage := p.HasOnProperty("billing.budgets.manage", b.PropertyID)
	switch b.Status {
	case "draft":
		if manage {
			b.AllowedActions = append(b.AllowedActions, "update")
		}
		if p.HasOnProperty("billing.budgets.approve", b.PropertyID) {
			b.AllowedActions = append(b.AllowedActions, "approve")
		}
	case "approved":
		if manage {
			b.AllowedActions = append(b.AllowedActions, "revise")
		}
	}
}

func (s *Service) loadBudgetLines(ctx context.Context, tx pgx.Tx, b *Budget) error {
	rows, err := tx.Query(ctx, `SELECT kind, category, month, amount FROM budget_lines WHERE budget_id = $1`, b.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	idx := map[string]*BudgetLine{}
	for rows.Next() {
		var kind, cat string
		var m int
		var amt int64
		if err := rows.Scan(&kind, &cat, &m, &amt); err != nil {
			return err
		}
		k := kind + "|" + cat
		l, ok := idx[k]
		if !ok {
			l = &BudgetLine{Kind: kind, Category: cat, Label: catLabel(kind, cat)}
			idx[k] = l
		}
		l.Months[m-1] = amt
		l.Total += amt
	}
	b.Lines = []BudgetLine{}
	for _, kind := range []string{"revenue", "cost"} {
		list := RevenueCategories
		if kind == "cost" {
			list = CostCategories
		}
		for _, c := range list {
			if l, ok := idx[kind+"|"+c.Key]; ok {
				b.Lines = append(b.Lines, *l)
			}
		}
	}
	return rows.Err()
}

func (s *Service) budgetTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*Budget, error) {
	q := budgetSelect + ` WHERE b.id = $1`
	if lock {
		q += ` FOR UPDATE OF b`
	}
	b, err := scanBudget(tx.QueryRow(ctx, q, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Budget")
		}
		return nil, err
	}
	if err := s.loadBudgetLines(ctx, tx, b); err != nil {
		return nil, err
	}
	s.budgetActions(ctx, b)
	return b, nil
}

func (s *Service) ListBudgets(ctx context.Context, propertyID *uuid.UUID, year int) ([]Budget, error) {
	p := authctx.Must(ctx)
	out := []Budget{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args := " WHERE true", []any{}
		if propertyID != nil {
			if !p.HasAnyOnProperty("billing.budgets.view", *propertyID) {
				return apperr.Forbidden("")
			}
			args = append(args, *propertyID)
			where += fmt.Sprintf(" AND b.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("billing.budgets.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND b.property_id = ANY($%d)", len(args))
		}
		if year > 0 {
			args = append(args, year)
			where += fmt.Sprintf(" AND b.fiscal_year = $%d", len(args))
		}
		rows, err := tx.Query(ctx, budgetSelect+where+" ORDER BY b.fiscal_year DESC, pl.name, b.revision DESC", args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			b, err := scanBudget(rows)
			if err != nil {
				return err
			}
			s.budgetActions(ctx, b)
			out = append(out, *b)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) GetBudget(ctx context.Context, id uuid.UUID) (*Budget, error) {
	var out *Budget
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := s.budgetTx(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasAnyOnProperty("billing.budgets.view", b.PropertyID) {
			return apperr.Forbidden("")
		}
		out = b
		return nil
	})
	return out, err
}

type BudgetLineInput struct {
	Kind     string    `json:"kind"`
	Category string    `json:"category"`
	Months   [12]int64 `json:"months"`
}

type BudgetInput struct {
	PropertyID *uuid.UUID         `json:"property_id"`
	FiscalYear int                `json:"fiscal_year"`
	Name       *string            `json:"name"`
	Notes      *string            `json:"notes"`
	CopyFromID *uuid.UUID         `json:"copy_from_id"`
	Lines      *[]BudgetLineInput `json:"lines"`
}

func (s *Service) replaceLinesTx(ctx context.Context, tx pgx.Tx, orgID, budgetID uuid.UUID, lines []BudgetLineInput) error {
	seen := map[string]bool{}
	for i, l := range lines {
		if !validCat(l.Kind, l.Category) {
			return apperr.Validation(fmt.Sprintf("lines[%d]: kategori %s/%s tidak valid", i, l.Kind, l.Category)).WithField(fmt.Sprintf("lines[%d].category", i), "tidak valid")
		}
		if seen[l.Kind+l.Category] {
			return apperr.Validation("Kategori "+l.Category+" duplikat").WithField(fmt.Sprintf("lines[%d].category", i), "duplikat")
		}
		seen[l.Kind+l.Category] = true
		for _, v := range l.Months {
			if v < 0 {
				return apperr.Validation("Nilai budget tidak boleh negatif").WithField(fmt.Sprintf("lines[%d].months", i), "negatif")
			}
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM budget_lines WHERE budget_id = $1`, budgetID); err != nil {
		return err
	}
	for _, l := range lines {
		for m, v := range l.Months {
			if v == 0 {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO budget_lines (organization_id, budget_id, kind, category, month, amount) VALUES ($1,$2,$3,$4,$5,$6)`, orgID, budgetID, l.Kind, l.Category, m+1, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) CreateBudget(ctx context.Context, in BudgetInput) (*Budget, error) {
	p := authctx.Must(ctx)
	if in.PropertyID == nil {
		return nil, apperr.Validation("property_id wajib").WithField("property_id", "wajib")
	}
	if !p.HasOnProperty("billing.budgets.manage", *in.PropertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.budgets.manage")
	}
	if in.FiscalYear < 2000 || in.FiscalYear > 2100 {
		return nil, apperr.Validation("fiscal_year tidak valid").WithField("fiscal_year", "2000..2100")
	}
	var out *Budget
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var draft int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM budgets WHERE property_id = $1 AND fiscal_year = $2 AND status = 'draft'`, *in.PropertyID, in.FiscalYear).Scan(&draft)
		if draft > 0 {
			return apperr.Conflict("BUDGET_DRAFT_EXISTS", "Sudah ada draft budget untuk tahun ini; lanjutkan draft tersebut")
		}
		var rev int
		_ = tx.QueryRow(ctx, `SELECT COALESCE(max(revision),0) + 1 FROM budgets WHERE property_id = $1 AND fiscal_year = $2`, *in.PropertyID, in.FiscalYear).Scan(&rev)
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO budgets (organization_id, property_id, fiscal_year, revision, name, notes, created_by, updated_by) VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$7) RETURNING id`,
			p.OrganizationID, *in.PropertyID, in.FiscalYear, rev, strings.TrimSpace(deref(in.Name)), deref(in.Notes), p.UserID).Scan(&id); err != nil {
			return err
		}
		switch {
		case in.Lines != nil:
			if err := s.replaceLinesTx(ctx, tx, p.OrganizationID, id, *in.Lines); err != nil {
				return err
			}
		case in.CopyFromID != nil:
			if _, err := tx.Exec(ctx, `INSERT INTO budget_lines (organization_id, budget_id, kind, category, month, amount) SELECT organization_id, $2, kind, category, month, amount FROM budget_lines WHERE budget_id = $1`, *in.CopyFromID, id); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "budget", EntityID: &id, EntityLabel: fmt.Sprintf("Budget %d rev %d", in.FiscalYear, rev), After: map[string]any{"fiscal_year": in.FiscalYear, "revision": rev}})
		var err error
		out, err = s.budgetTx(ctx, tx, id, false)
		return err
	})
	return out, err
}

func (s *Service) UpdateBudget(ctx context.Context, id uuid.UUID, in BudgetInput) (*Budget, error) {
	p := authctx.Must(ctx)
	var out *Budget
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := s.budgetTx(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if !has(b.AllowedActions, "update") {
			if !p.HasOnProperty("billing.budgets.manage", b.PropertyID) {
				return apperr.Forbidden("Memerlukan billing.budgets.manage")
			}
			return apperr.InvalidTransition("Budget berstatus " + b.Status + " tidak dapat diubah; buat revisi")
		}
		if _, err := tx.Exec(ctx, `UPDATE budgets SET name = CASE WHEN $2::text IS NULL THEN name ELSE NULLIF($2,'') END, notes = CASE WHEN $3::text IS NULL THEN notes ELSE NULLIF($3,'') END, updated_by = $4 WHERE id = $1`,
			id, in.Name, in.Notes, p.UserID); err != nil {
			return err
		}
		if in.Lines != nil {
			if err := s.replaceLinesTx(ctx, tx, p.OrganizationID, id, *in.Lines); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "budget", EntityID: &id, EntityLabel: fmt.Sprintf("Budget %d rev %d", b.FiscalYear, b.Revision), Before: map[string]any{"revenue": b.RevenueTotal, "cost": b.CostTotal}, After: in})
		out, err = s.budgetTx(ctx, tx, id, false)
		return err
	})
	return out, err
}

// ActBudget: approve (draft → approved; approved sebelumnya → superseded) | revise (salin approved → draft revisi baru).
func (s *Service) ActBudget(ctx context.Context, id uuid.UUID, action string) (*Budget, error) {
	p := authctx.Must(ctx)
	var out *Budget
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := s.budgetTx(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if !has(b.AllowedActions, action) {
			perm := map[string]string{"approve": "billing.budgets.approve", "revise": "billing.budgets.manage"}[action]
			if perm != "" && !p.HasOnProperty(perm, b.PropertyID) {
				return apperr.Forbidden("Memerlukan " + perm)
			}
			return apperr.InvalidTransition("Aksi " + action + " tidak tersedia untuk budget berstatus " + b.Status)
		}
		target := id
		switch action {
		case "approve":
			if b.RevenueTotal+b.CostTotal == 0 {
				return apperr.Validation("Budget masih kosong")
			}
			if _, err := tx.Exec(ctx, `UPDATE budgets SET status = 'superseded', updated_by = $3 WHERE property_id = $1 AND fiscal_year = $2 AND status = 'approved'`, b.PropertyID, b.FiscalYear, p.UserID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE budgets SET status = 'approved', approved_by = $2, approved_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
				return err
			}
		case "revise":
			var draft int
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM budgets WHERE property_id = $1 AND fiscal_year = $2 AND status = 'draft'`, b.PropertyID, b.FiscalYear).Scan(&draft)
			if draft > 0 {
				return apperr.Conflict("BUDGET_DRAFT_EXISTS", "Sudah ada draft revisi untuk tahun ini")
			}
			var rev int
			_ = tx.QueryRow(ctx, `SELECT max(revision) + 1 FROM budgets WHERE property_id = $1 AND fiscal_year = $2`, b.PropertyID, b.FiscalYear).Scan(&rev)
			if err := tx.QueryRow(ctx, `INSERT INTO budgets (organization_id, property_id, fiscal_year, revision, name, notes, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$7) RETURNING id`,
				p.OrganizationID, b.PropertyID, b.FiscalYear, rev, b.Name, b.Notes, p.UserID).Scan(&target); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO budget_lines (organization_id, budget_id, kind, category, month, amount) SELECT organization_id, $2, kind, category, month, amount FROM budget_lines WHERE budget_id = $1`, id, target); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "budget", EntityID: &id, EntityLabel: fmt.Sprintf("Budget %d rev %d", b.FiscalYear, b.Revision), Before: map[string]any{"status": b.Status}, After: map[string]any{"action": action}})
		out, err = s.budgetTx(ctx, tx, target, false)
		return err
	})
	return out, err
}

// ---------- Biaya operasional manual (P4-CST-03, D-P4-07 tanpa payroll) ----------

type CostEntry struct {
	ID              uuid.UUID `json:"id"`
	PropertyID      uuid.UUID `json:"property_id"`
	PropertyName    string    `json:"property_name"`
	Category        string    `json:"category"`
	CategoryLabel   string    `json:"category_label"`
	EntryDate       string    `json:"entry_date"`
	Amount          int64     `json:"amount"`
	Payee           *string   `json:"payee"`
	Description     string    `json:"description"`
	Reference       *string   `json:"reference"`
	AttachmentCount int       `json:"attachment_count"`
	CreatedByName   *string   `json:"created_by_name"`
	CreatedAt       time.Time `json:"created_at"`
	AllowedActions  []string  `json:"allowed_actions"`
	Version         int       `json:"version"`
}

const costSelect = `SELECT c.id, c.property_id, pl.name, c.category, to_char(c.entry_date,'YYYY-MM-DD'), c.amount, c.payee, c.description, c.reference,
	(SELECT count(*) FROM attachments a WHERE a.object_type = 'cost_entry' AND a.object_id = c.id AND a.deleted_at IS NULL), u.full_name, c.created_at, c.version
	FROM cost_entries c JOIN locations pl ON pl.id = c.property_id LEFT JOIN users u ON u.id = c.created_by`

func scanCost(row pgx.Row) (*CostEntry, error) {
	var c CostEntry
	if err := row.Scan(&c.ID, &c.PropertyID, &c.PropertyName, &c.Category, &c.EntryDate, &c.Amount, &c.Payee, &c.Description, &c.Reference, &c.AttachmentCount, &c.CreatedByName, &c.CreatedAt, &c.Version); err != nil {
		return nil, err
	}
	c.CategoryLabel = catLabel("cost", c.Category)
	return &c, nil
}

func (s *Service) costActions(ctx context.Context, c *CostEntry) {
	c.AllowedActions = []string{"view"}
	if authctx.Must(ctx).HasOnProperty("billing.costs.manage", c.PropertyID) {
		c.AllowedActions = append(c.AllowedActions, "update", "delete", "attach")
	}
}

type CostFilter struct {
	PropertyID *uuid.UUID
	Category   string
	From, To   *time.Time
	Q          string
}

func (s *Service) ListCosts(ctx context.Context, f CostFilter, page httpx.Page) ([]CostEntry, *string, error) {
	p := authctx.Must(ctx)
	var out []CostEntry
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var args []any
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE c.deleted_at IS NULL"
		if f.PropertyID != nil {
			if !p.HasAnyOnProperty("billing.costs.view", *f.PropertyID) {
				return apperr.Forbidden("")
			}
			where += " AND c.property_id = " + add(*f.PropertyID)
		} else if pids, all := p.PropertyIDsFor("billing.costs.view"); !all {
			where += " AND c.property_id = ANY(" + add(pids) + ")"
		}
		if f.Category != "" {
			where += " AND c.category = " + add(f.Category)
		}
		if f.From != nil {
			where += " AND c.entry_date >= " + add(*f.From)
		}
		if f.To != nil {
			where += " AND c.entry_date <= " + add(*f.To)
		}
		if q := strings.TrimSpace(f.Q); q != "" {
			a := add("%" + q + "%")
			where += " AND (c.description ILIKE " + a + " OR c.payee ILIKE " + a + " OR c.reference ILIKE " + a + ")"
		}
		if page.Cursor != nil {
			where += fmt.Sprintf(" AND (c.created_at, c.id) < (%s::timestamptz, %s)", add(page.Cursor.Value), add(page.Cursor.ID))
		}
		rows, err := tx.Query(ctx, costSelect+where+fmt.Sprintf(" ORDER BY c.created_at DESC, c.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCost(rows)
			if err != nil {
				return err
			}
			s.costActions(ctx, c)
			out = append(out, *c)
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
		out = []CostEntry{}
	}
	return out, next, err
}

type CostInput struct {
	PropertyID  *uuid.UUID `json:"property_id"`
	Category    *string    `json:"category"`
	EntryDate   *string    `json:"entry_date"`
	Amount      *int64     `json:"amount"`
	Payee       *string    `json:"payee"`
	Description *string    `json:"description"`
	Reference   *string    `json:"reference"`
}

func (s *Service) SaveCost(ctx context.Context, id *uuid.UUID, in CostInput) (*CostEntry, error) {
	p := authctx.Must(ctx)
	var out *CostEntry
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		cur := &CostEntry{}
		if id != nil {
			c, err := scanCost(tx.QueryRow(ctx, costSelect+` WHERE c.id = $1 AND c.deleted_at IS NULL FOR UPDATE OF c`, *id))
			if err != nil {
				return apperr.NotFound("Biaya")
			}
			cur = c
		} else {
			if in.PropertyID == nil {
				return apperr.Validation("property_id wajib").WithField("property_id", "wajib")
			}
			cur.PropertyID = *in.PropertyID
			cur.EntryDate = time.Now().Format("2006-01-02")
		}
		if !p.HasOnProperty("billing.costs.manage", cur.PropertyID) {
			return apperr.Forbidden("Memerlukan billing.costs.manage")
		}
		if in.Category != nil {
			cur.Category = *in.Category
		}
		if in.EntryDate != nil {
			if _, err := time.Parse("2006-01-02", *in.EntryDate); err != nil {
				return apperr.Validation("entry_date harus YYYY-MM-DD").WithField("entry_date", "format")
			}
			cur.EntryDate = *in.EntryDate
		}
		if in.Amount != nil {
			cur.Amount = *in.Amount
		}
		if in.Payee != nil {
			cur.Payee = in.Payee
		}
		if in.Description != nil {
			cur.Description = strings.TrimSpace(*in.Description)
		}
		if in.Reference != nil {
			cur.Reference = in.Reference
		}
		switch {
		case !validCat("cost", cur.Category):
			return apperr.Validation("category tidak valid").WithField("category", "tidak valid")
		case cur.Amount <= 0:
			return apperr.Validation("amount harus > 0").WithField("amount", "harus > 0")
		case cur.Description == "":
			return apperr.Validation("description wajib").WithField("description", "wajib")
		}
		var cid uuid.UUID
		if id == nil {
			if err := tx.QueryRow(ctx, `INSERT INTO cost_entries (organization_id, property_id, category, entry_date, amount, payee, description, reference, created_by, updated_by)
				VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,NULLIF($8,''),$9,$9) RETURNING id`, p.OrganizationID, cur.PropertyID, cur.Category, cur.EntryDate, cur.Amount, deref(cur.Payee), cur.Description, deref(cur.Reference), p.UserID).Scan(&cid); err != nil {
				return err
			}
		} else {
			cid = *id
			if _, err := tx.Exec(ctx, `UPDATE cost_entries SET category=$2, entry_date=$3, amount=$4, payee=NULLIF($5,''), description=$6, reference=NULLIF($7,''), updated_by=$8 WHERE id = $1`,
				cid, cur.Category, cur.EntryDate, cur.Amount, deref(cur.Payee), cur.Description, deref(cur.Reference), p.UserID); err != nil {
				return err
			}
		}
		act := audit.AuditCreate
		if id != nil {
			act = audit.AuditUpdate
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: act, EntityType: "cost_entry", EntityID: &cid, EntityLabel: cur.Description, After: in})
		var err error
		out, err = scanCost(tx.QueryRow(ctx, costSelect+` WHERE c.id = $1`, cid))
		if err == nil {
			s.costActions(ctx, out)
		}
		return err
	})
	return out, err
}

func (s *Service) DeleteCost(ctx context.Context, id uuid.UUID) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		c, err := scanCost(tx.QueryRow(ctx, costSelect+` WHERE c.id = $1 AND c.deleted_at IS NULL FOR UPDATE OF c`, id))
		if err != nil {
			return apperr.NotFound("Biaya")
		}
		if !p.HasOnProperty("billing.costs.manage", c.PropertyID) {
			return apperr.Forbidden("Memerlukan billing.costs.manage")
		}
		if _, err := tx.Exec(ctx, `UPDATE cost_entries SET deleted_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "cost_entry", EntityID: &id, EntityLabel: c.Description, Before: map[string]any{"amount": c.Amount, "category": c.Category, "entry_date": c.EntryDate}})
		return nil
	})
}

// CostAccess: akses lampiran bukti biaya (object_type cost_entry).
func CostAccess(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, write bool) error {
	var pid uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT property_id FROM cost_entries WHERE id = $1 AND deleted_at IS NULL`, objectID).Scan(&pid); err != nil {
		return apperr.NotFound("Biaya")
	}
	perm := "billing.costs.view"
	if write {
		perm = "billing.costs.manage"
	}
	if !authctx.Must(ctx).HasOnProperty(perm, pid) {
		return apperr.Forbidden("")
	}
	return nil
}

// ---------- Actual (P4-BGT-02) ----------

// Actuals: nilai per kategori × bulan (1..12) untuk satu tahun, zona waktu property.
type Actuals struct {
	Revenue map[string]*[12]int64
	Cost    map[string]*[12]int64
}

func addTo(m map[string]*[12]int64, cat string, month int, v int64) {
	if month < 1 || month > 12 {
		return
	}
	a, ok := m[cat]
	if !ok {
		a = &[12]int64{}
		m[cat] = a
	}
	a[month-1] += v
}

// propWhere: filter property (satu, daftar, atau semua) untuk alias kolom property.
func propWhere(col string, propertyIDs []uuid.UUID, all bool, args *[]any) string {
	if all {
		return ""
	}
	*args = append(*args, propertyIDs)
	return fmt.Sprintf(" AND %s = ANY($%d)", col, len(*args))
}

// ActualsTx: pendapatan = item invoice terbit per charge_type (tanpa pajak & deposit) menurut bulan terbit, dikurangi credit note
// disetujui; biaya = WO selesai (actual cost), consumable cleaning, dan biaya manual.
func ActualsTx(ctx context.Context, tx pgx.Tx, propertyIDs []uuid.UUID, all bool, year int) (*Actuals, error) {
	out := &Actuals{Revenue: map[string]*[12]int64{}, Cost: map[string]*[12]int64{}}
	type q struct {
		kind, sql string
	}
	// argumen sama untuk setiap query: $1 tahun, $2 daftar property (bila dibatasi)
	args := []any{year}
	if !all {
		args = append(args, propertyIDs)
	}
	pw := func(col string) string {
		if all {
			return ""
		}
		return " AND " + col + " = ANY($2)"
	}
	qs := []q{
		{"revenue", `SELECT COALESCE(it.charge_type, i.invoice_type), extract(month FROM i.issued_at AT TIME ZONE pr.timezone)::int, sum(it.amount)::bigint
			FROM invoice_items it JOIN invoices i ON i.id = it.invoice_id JOIN properties pr ON pr.location_id = i.property_id
			WHERE i.issued_at IS NOT NULL AND i.status <> 'cancelled' AND i.invoice_number IS NOT NULL AND extract(year FROM i.issued_at AT TIME ZONE pr.timezone) = $1
			  AND COALESCE(it.charge_type, i.invoice_type) <> 'deposit'` + pw("i.property_id") + ` GROUP BY 1, 2`},
		{"revenue_cn", `SELECT i.invoice_type, extract(month FROM c.decided_at AT TIME ZONE pr.timezone)::int, -sum(c.amount)::bigint
			FROM credit_notes c JOIN invoices i ON i.id = c.invoice_id JOIN properties pr ON pr.location_id = c.property_id
			WHERE c.status = 'approved' AND extract(year FROM c.decided_at AT TIME ZONE pr.timezone) = $1 AND i.invoice_type <> 'deposit'` + pw("c.property_id") + ` GROUP BY 1, 2`},
		{"cost", `SELECT 'maintenance', extract(month FROM w.completed_at AT TIME ZONE pr.timezone)::int,
			sum(COALESCE(w.actual_cost_amount, COALESCE(w.parts_cost_amount,0) + COALESCE(w.service_cost_amount,0) + COALESCE(w.other_cost_amount,0)))::bigint
			FROM work_orders w JOIN properties pr ON pr.location_id = w.property_id
			WHERE w.status IN ('completed','closed') AND w.completed_at IS NOT NULL AND extract(year FROM w.completed_at AT TIME ZONE pr.timezone) = $1` + pw("w.property_id") + ` GROUP BY 1, 2`},
		{"cost", `SELECT 'cleaning', extract(month FROM tc.recorded_at AT TIME ZONE pr.timezone)::int, sum(COALESCE(tc.total_cost, round(tc.quantity * COALESCE(tc.unit_cost,0))))::bigint
			FROM task_consumables tc JOIN tasks t ON t.id = tc.task_id JOIN properties pr ON pr.location_id = t.property_id
			WHERE extract(year FROM tc.recorded_at AT TIME ZONE pr.timezone) = $1` + pw("t.property_id") + ` GROUP BY 1, 2`},
		{"cost", `SELECT c.category, extract(month FROM c.entry_date)::int, sum(c.amount)::bigint FROM cost_entries c
			WHERE c.deleted_at IS NULL AND extract(year FROM c.entry_date) = $1` + pw("c.property_id") + ` GROUP BY 1, 2`},
	}
	for _, x := range qs {
		rows, err := tx.Query(ctx, x.sql, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var cat string
			var m int
			var v *int64
			if err := rows.Scan(&cat, &m, &v); err != nil {
				rows.Close()
				return nil, err
			}
			if v == nil {
				continue
			}
			if x.kind == "cost" {
				addTo(out.Cost, cat, m, *v)
			} else {
				addTo(out.Revenue, cat, m, *v)
			}
		}
		rows.Close()
	}
	return out, nil
}

// ---------- Budget vs Actual (P4-BGT-03) ----------

type Cell struct {
	Budget   int64 `json:"budget"`
	Actual   int64 `json:"actual"`
	Variance int64 `json:"variance"` // actual − budget
}

type BVARow struct {
	Kind        string     `json:"kind"`
	Category    string     `json:"category"`
	Label       string     `json:"label"`
	Months      [12]Cell   `json:"months"`
	Total       Cell       `json:"total"`
	YTD         Cell       `json:"ytd"`
	VariancePct *float64   `json:"variance_pct"` // YTD
	Status      string     `json:"status"`       // on_track | over | under
	DrillDown   string     `json:"drill_down"`
	Sources     []DrillSrc `json:"sources"`
}

type DrillSrc struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

type BVA struct {
	PropertyID   *uuid.UUID `json:"property_id"`
	FiscalYear   int        `json:"fiscal_year"`
	UpToMonth    int        `json:"up_to_month"`
	BudgetID     *uuid.UUID `json:"budget_id"`
	BudgetStatus *string    `json:"budget_status"`
	Revision     *int       `json:"revision"`
	Revenue      []BVARow   `json:"revenue"`
	Cost         []BVARow   `json:"cost"`
	TotalRevenue BVARow     `json:"total_revenue"`
	TotalCost    BVARow     `json:"total_cost"`
	Net          BVARow     `json:"net"`
	CurrencyCode string     `json:"currency_code"`
}

func monthRange(year, m int) (string, string) {
	a := time.Date(year, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
	return a.Format("2006-01-02"), a.AddDate(0, 1, -1).Format("2006-01-02")
}

func drillFor(kind, cat string, propertyID *uuid.UUID, year int) (string, []DrillSrc) {
	qs := fmt.Sprintf("kind=%s&category=%s&year=%d", kind, cat, year)
	if propertyID != nil {
		qs += "&property_id=" + propertyID.String()
	}
	from, _ := monthRange(year, 1)
	_, to := monthRange(year, 12)
	pq := ""
	if propertyID != nil {
		pq = "&property_id=" + propertyID.String()
	}
	var src []DrillSrc
	if kind == "revenue" {
		src = append(src, DrillSrc{"Invoice", "/billing/invoices?type=" + cat + "&issued_from=" + from + "&issued_to=" + to + pq})
	} else {
		switch cat {
		case "maintenance":
			src = append(src, DrillSrc{"Work order selesai", "/work-orders?status=completed,closed" + pq})
		case "cleaning":
			src = append(src, DrillSrc{"Consumable cleaning", "/reports/inventory?tab=consumables&from=" + from + "&to=" + to + pq})
		}
		src = append(src, DrillSrc{"Biaya manual", "/finance/costs?category=" + cat + "&from=" + from + "&to=" + to + pq})
	}
	return "/finance/budget-actual/transactions?" + qs, src
}

// BudgetVsActual: budget approved (atau revisi terbaru) vs actual; YTD sampai bulan berjalan (tahun berjalan) atau Desember.
func (s *Service) BudgetVsActual(ctx context.Context, propertyID *uuid.UUID, year int) (*BVA, error) {
	p := authctx.Must(ctx)
	if year == 0 {
		year = time.Now().Year()
	}
	out := &BVA{PropertyID: propertyID, FiscalYear: year, UpToMonth: 12, Revenue: []BVARow{}, Cost: []BVARow{}, CurrencyCode: "IDR"}
	now := time.Now()
	if year == now.Year() {
		out.UpToMonth = int(now.Month())
	} else if year > now.Year() {
		out.UpToMonth = 0
	}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pids []uuid.UUID
		all := false
		if propertyID != nil {
			if !p.HasAnyOnProperty("billing.budgets.view", *propertyID) {
				return apperr.Forbidden("")
			}
			pids = []uuid.UUID{*propertyID}
		} else {
			pids, all = p.PropertyIDsFor("billing.budgets.view")
		}
		budget := map[string]*[12]int64{}
		if propertyID != nil {
			var bid uuid.UUID
			var st string
			var rev int
			err := tx.QueryRow(ctx, `SELECT id, status, revision FROM budgets WHERE property_id = $1 AND fiscal_year = $2 AND status <> 'superseded' ORDER BY (status = 'approved') DESC, revision DESC LIMIT 1`, *propertyID, year).Scan(&bid, &st, &rev)
			if err == nil {
				out.BudgetID, out.BudgetStatus, out.Revision = &bid, &st, &rev
				rows, err := tx.Query(ctx, `SELECT kind, category, month, amount FROM budget_lines WHERE budget_id = $1`, bid)
				if err != nil {
					return err
				}
				for rows.Next() {
					var kind, cat string
					var m int
					var v int64
					if rows.Scan(&kind, &cat, &m, &v) == nil {
						addTo(budget, kind+"|"+cat, m, v)
					}
				}
				rows.Close()
			}
		} else {
			// agregat lintas property: budget approved tiap property
			args := []any{year}
			w := propWhere("b.property_id", pids, all, &args)
			rows, err := tx.Query(ctx, `SELECT bl.kind, bl.category, bl.month, sum(bl.amount)::bigint FROM budget_lines bl JOIN budgets b ON b.id = bl.budget_id
				WHERE b.fiscal_year = $1 AND b.status = 'approved'`+w+` GROUP BY 1, 2, 3`, args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var kind, cat string
				var m int
				var v int64
				if rows.Scan(&kind, &cat, &m, &v) == nil {
					addTo(budget, kind+"|"+cat, m, v)
				}
			}
			rows.Close()
		}
		act, err := ActualsTx(ctx, tx, pids, all, year)
		if err != nil {
			return err
		}
		build := func(kind string, cats []Category, actual map[string]*[12]int64) []BVARow {
			var list []BVARow
			for _, c := range cats {
				b := budget[kind+"|"+c.Key]
				a := actual[c.Key]
				if b == nil && a == nil {
					continue
				}
				r := BVARow{Kind: kind, Category: c.Key, Label: c.Label}
				for m := 0; m < 12; m++ {
					var bv, av int64
					if b != nil {
						bv = b[m]
					}
					if a != nil {
						av = a[m]
					}
					r.Months[m] = Cell{Budget: bv, Actual: av, Variance: av - bv}
					r.Total.Budget += bv
					r.Total.Actual += av
					if m < out.UpToMonth {
						r.YTD.Budget += bv
						r.YTD.Actual += av
					}
				}
				r.Total.Variance = r.Total.Actual - r.Total.Budget
				r.YTD.Variance = r.YTD.Actual - r.YTD.Budget
				r.Status = status(kind, r.YTD)
				if r.YTD.Budget > 0 {
					v := float64(r.YTD.Variance) / float64(r.YTD.Budget) * 100
					v = float64(int64(v*10)) / 10
					r.VariancePct = &v
				}
				r.DrillDown, r.Sources = drillFor(kind, c.Key, propertyID, year)
				list = append(list, r)
			}
			return list
		}
		out.Revenue = build("revenue", RevenueCategories, act.Revenue)
		out.Cost = build("cost", CostCategories, act.Cost)
		sum := func(kind string, rows []BVARow) BVARow {
			t := BVARow{Kind: kind, Category: "total", Label: "Total"}
			for _, r := range rows {
				for m := 0; m < 12; m++ {
					t.Months[m].Budget += r.Months[m].Budget
					t.Months[m].Actual += r.Months[m].Actual
					t.Months[m].Variance += r.Months[m].Variance
				}
				t.Total.Budget += r.Total.Budget
				t.Total.Actual += r.Total.Actual
				t.YTD.Budget += r.YTD.Budget
				t.YTD.Actual += r.YTD.Actual
			}
			t.Total.Variance = t.Total.Actual - t.Total.Budget
			t.YTD.Variance = t.YTD.Actual - t.YTD.Budget
			t.Status = status(kind, t.YTD)
			if t.YTD.Budget > 0 {
				v := float64(int64(float64(t.YTD.Variance)/float64(t.YTD.Budget)*1000)) / 10
				t.VariancePct = &v
			}
			return t
		}
		out.TotalRevenue = sum("revenue", out.Revenue)
		out.TotalCost = sum("cost", out.Cost)
		net := BVARow{Kind: "net", Category: "net", Label: "Selisih pendapatan − biaya"}
		for m := 0; m < 12; m++ {
			net.Months[m].Budget = out.TotalRevenue.Months[m].Budget - out.TotalCost.Months[m].Budget
			net.Months[m].Actual = out.TotalRevenue.Months[m].Actual - out.TotalCost.Months[m].Actual
			net.Months[m].Variance = net.Months[m].Actual - net.Months[m].Budget
		}
		net.Total = Cell{Budget: out.TotalRevenue.Total.Budget - out.TotalCost.Total.Budget, Actual: out.TotalRevenue.Total.Actual - out.TotalCost.Total.Actual}
		net.Total.Variance = net.Total.Actual - net.Total.Budget
		net.YTD = Cell{Budget: out.TotalRevenue.YTD.Budget - out.TotalCost.YTD.Budget, Actual: out.TotalRevenue.YTD.Actual - out.TotalCost.YTD.Actual}
		net.YTD.Variance = net.YTD.Actual - net.YTD.Budget
		net.Status = status("revenue", net.YTD)
		out.Net = net
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// status: pendapatan di bawah budget > 5% = under; biaya di atas budget > 5% = over.
func status(kind string, c Cell) string {
	if c.Budget <= 0 {
		return "on_track"
	}
	pct := float64(c.Variance) / float64(c.Budget) * 100
	switch {
	case kind == "cost" && pct > 5:
		return "over"
	case kind != "cost" && pct < -5:
		return "under"
	}
	return "on_track"
}

// ---------- Drill-down transaksi actual ----------

type ActualTxn struct {
	Date        string    `json:"date"`
	SourceType  string    `json:"source_type"` // invoice_item | credit_note | work_order | task_consumable | cost_entry
	SourceID    uuid.UUID `json:"source_id"`
	Number      *string   `json:"number"`
	Description string    `json:"description"`
	Amount      int64     `json:"amount"`
	Link        string    `json:"link"`
}

func (s *Service) ActualTransactions(ctx context.Context, propertyID *uuid.UUID, kind, category string, year, month int) ([]ActualTxn, error) {
	p := authctx.Must(ctx)
	if !validCat(kind, category) {
		return nil, apperr.Validation("kind/category tidak valid").WithField("category", "tidak valid")
	}
	if year == 0 {
		year = time.Now().Year()
	}
	out := []ActualTxn{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pids []uuid.UUID
		all := false
		if propertyID != nil {
			if !p.HasAnyOnProperty("billing.budgets.view", *propertyID) {
				return apperr.Forbidden("")
			}
			pids = []uuid.UUID{*propertyID}
		} else {
			pids, all = p.PropertyIDsFor("billing.budgets.view")
		}
		args := []any{year, month, category}
		pw := func(col string) string { return propWhere(col, pids, all, &args) }
		mw := func(expr string) string {
			return " AND extract(year FROM " + expr + ") = $1 AND ($2 = 0 OR extract(month FROM " + expr + ") = $2)"
		}
		var q string
		if kind == "revenue" {
			q = `SELECT to_char(i.issued_at AT TIME ZONE pr.timezone, 'YYYY-MM-DD'), 'invoice_item', i.id, i.invoice_number, it.description, it.amount, '/billing/invoices/' || i.id
				FROM invoice_items it JOIN invoices i ON i.id = it.invoice_id JOIN properties pr ON pr.location_id = i.property_id
				WHERE i.issued_at IS NOT NULL AND i.status <> 'cancelled' AND i.invoice_number IS NOT NULL AND COALESCE(it.charge_type, i.invoice_type) = $3` + mw("i.issued_at AT TIME ZONE pr.timezone") + pw("i.property_id") + `
				UNION ALL
				SELECT to_char(c.decided_at AT TIME ZONE pr.timezone, 'YYYY-MM-DD'), 'credit_note', c.id, c.credit_note_number, 'Credit note ' || COALESCE(i.invoice_number,'') || ' — ' || c.reason, -c.amount, '/billing/credit-notes/' || c.id
				FROM credit_notes c JOIN invoices i ON i.id = c.invoice_id JOIN properties pr ON pr.location_id = c.property_id
				WHERE c.status = 'approved' AND i.invoice_type = $3` + mw("c.decided_at AT TIME ZONE pr.timezone") + pw("c.property_id")
		} else {
			q = `SELECT to_char(c.entry_date, 'YYYY-MM-DD'), 'cost_entry', c.id, c.reference, c.description || COALESCE(' · ' || c.payee, ''), c.amount, '/finance/costs/' || c.id
				FROM cost_entries c WHERE c.deleted_at IS NULL AND c.category = $3` + mw("c.entry_date") + pw("c.property_id")
			switch category {
			case "maintenance":
				q += ` UNION ALL SELECT to_char(w.completed_at AT TIME ZONE pr.timezone, 'YYYY-MM-DD'), 'work_order', w.id, w.work_order_number, w.title,
					COALESCE(w.actual_cost_amount, COALESCE(w.parts_cost_amount,0) + COALESCE(w.service_cost_amount,0) + COALESCE(w.other_cost_amount,0)), '/work-orders/' || w.id
					FROM work_orders w JOIN properties pr ON pr.location_id = w.property_id WHERE w.status IN ('completed','closed') AND w.completed_at IS NOT NULL AND $3 = 'maintenance'
					  AND COALESCE(w.actual_cost_amount, COALESCE(w.parts_cost_amount,0) + COALESCE(w.service_cost_amount,0) + COALESCE(w.other_cost_amount,0)) > 0` + mw("w.completed_at AT TIME ZONE pr.timezone") + pw("w.property_id")
			case "cleaning":
				q += ` UNION ALL SELECT to_char(tc.recorded_at AT TIME ZONE pr.timezone, 'YYYY-MM-DD'), 'task_consumable', tc.id, t.task_number, ii.name || ' × ' || tc.quantity::text,
					COALESCE(tc.total_cost, round(tc.quantity * COALESCE(tc.unit_cost,0)))::bigint, '/tasks/' || t.id
					FROM task_consumables tc JOIN tasks t ON t.id = tc.task_id JOIN inventory_items ii ON ii.id = tc.item_id JOIN properties pr ON pr.location_id = t.property_id
					WHERE $3 = 'cleaning'` + mw("tc.recorded_at AT TIME ZONE pr.timezone") + pw("t.property_id")
			}
		}
		rows, err := tx.Query(ctx, `SELECT * FROM (`+q+`) x ORDER BY 1, 4 LIMIT 1000`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t ActualTxn
			if err := rows.Scan(&t.Date, &t.SourceType, &t.SourceID, &t.Number, &t.Description, &t.Amount, &t.Link); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// ---------- Operating cost (P4-CST-02, laporan & dashboard) ----------

type OperatingCost struct {
	Year       int                 `json:"year"`
	Categories []OperatingCostRow  `json:"categories"`
	Monthly    [12]int64           `json:"monthly"`
	Total      int64               `json:"total"`
	Sources    map[string][]string `json:"sources"`
}

type OperatingCostRow struct {
	Category string    `json:"category"`
	Label    string    `json:"label"`
	Months   [12]int64 `json:"months"`
	Total    int64     `json:"total"`
	SharePct float64   `json:"share_pct"`
}

func (s *Service) OperatingCosts(ctx context.Context, propertyID *uuid.UUID, year int) (*OperatingCost, error) {
	p := authctx.Must(ctx)
	if year == 0 {
		year = time.Now().Year()
	}
	out := &OperatingCost{Year: year, Categories: []OperatingCostRow{}, Sources: map[string][]string{
		"maintenance": {"Work order selesai (actual cost)", "Biaya manual"}, "cleaning": {"Consumable per cleaning task", "Biaya manual"}}}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var pids []uuid.UUID
		all := false
		perm := "billing.costs.view"
		if !p.Has(perm) {
			perm = "reports.reports.view"
		}
		if propertyID != nil {
			if !p.HasAnyOnProperty(perm, *propertyID) {
				return apperr.Forbidden("")
			}
			pids = []uuid.UUID{*propertyID}
		} else {
			pids, all = p.PropertyIDsFor(perm)
		}
		act, err := ActualsTx(ctx, tx, pids, all, year)
		if err != nil {
			return err
		}
		for _, c := range CostCategories {
			a := act.Cost[c.Key]
			if a == nil {
				continue
			}
			r := OperatingCostRow{Category: c.Key, Label: c.Label, Months: *a}
			for m, v := range a {
				r.Total += v
				out.Monthly[m] += v
			}
			out.Total += r.Total
			out.Categories = append(out.Categories, r)
		}
		for i := range out.Categories {
			if out.Total > 0 {
				out.Categories[i].SharePct = float64(int64(float64(out.Categories[i].Total)/float64(out.Total)*1000)) / 10
			}
		}
		sort.SliceStable(out.Categories, func(i, j int) bool { return out.Categories[i].Total > out.Categories[j].Total })
		return nil
	})
	return out, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
