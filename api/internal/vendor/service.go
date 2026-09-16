// Package vendor: Vendor Management (PRD P1 v1.3 §24; NC §37) — vendor master, contact, service category,
// assignment ke Work Order (Vendor Work Order), performance history dari WO. Vendor Portal = scope lanjutan (OD-P1-011).
package vendor

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
)

const EventWorkOrderVendorAssigned = "work_order.vendor_assigned"

type Service struct {
	DB   *db.DB
	Jobs jobs.Enqueuer
}

func New(d *db.DB, j jobs.Enqueuer) *Service { return &Service{DB: d, Jobs: j} }

type Contact struct {
	ID        uuid.UUID `json:"id,omitempty"`
	Name      string    `json:"name"`
	Role      *string   `json:"role"`
	Phone     *string   `json:"phone"`
	Email     *string   `json:"email"`
	IsPrimary bool      `json:"is_primary"`
}

type Performance struct {
	TotalWorkOrders     int      `json:"total_work_orders"`
	CompletedWorkOrders int      `json:"completed_work_orders"`
	OpenWorkOrders      int      `json:"open_work_orders"`
	OnTimePct           *float64 `json:"on_time_pct"`
	AvgCompletionHours  *float64 `json:"avg_completion_hours"`
	ReopenCount         int      `json:"reopen_count"`
	Last90dWorkOrders   int      `json:"last_90d_work_orders"`
}

type Vendor struct {
	ID                uuid.UUID   `json:"id"`
	VendorCode        string      `json:"vendor_code"`
	Name              string      `json:"name"`
	ServiceCategories []string    `json:"service_categories"`
	ContactName       *string     `json:"contact_name"`
	ContactPhone      *string     `json:"contact_phone"`
	ContactEmail      *string     `json:"contact_email"`
	Address           *string     `json:"address"`
	TaxID             *string     `json:"tax_id"`
	ContractRef       *string     `json:"contract_ref"`
	ContractStart     *time.Time  `json:"contract_start"`
	ContractEnd       *time.Time  `json:"contract_end"`
	Status            string      `json:"status"`
	Notes             *string     `json:"notes"`
	Contacts          []Contact   `json:"contacts"`
	Performance       Performance `json:"performance"`
	CreatedAt         time.Time   `json:"created_at"`
	Version           int         `json:"version"`
}

const vSelect = `SELECT v.id, v.vendor_code, v.name, v.service_categories, v.contact_name, v.contact_phone, v.contact_email, v.address, v.tax_id, v.contract_ref, v.contract_start, v.contract_end, v.status, v.notes, v.created_at, v.version FROM vendors v`

func scanVendor(row pgx.Row) (*Vendor, error) {
	var v Vendor
	if err := row.Scan(&v.ID, &v.VendorCode, &v.Name, &v.ServiceCategories, &v.ContactName, &v.ContactPhone, &v.ContactEmail, &v.Address, &v.TaxID, &v.ContractRef, &v.ContractStart, &v.ContractEnd, &v.Status, &v.Notes, &v.CreatedAt, &v.Version); err != nil {
		return nil, err
	}
	v.Contacts = []Contact{}
	return &v, nil
}

func (s *Service) enrich(ctx context.Context, tx pgx.Tx, v *Vendor) error {
	rows, err := tx.Query(ctx, `SELECT id, name, role, phone, email, is_primary FROM vendor_contacts WHERE vendor_id = $1 ORDER BY is_primary DESC, name`, v.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.Name, &c.Role, &c.Phone, &c.Email, &c.IsPrimary); err != nil {
			rows.Close()
			return err
		}
		v.Contacts = append(v.Contacts, c)
	}
	rows.Close()
	// performance dari WO (NC §56 "Vendor Performance")
	return tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status IN ('completed','closed')), count(*) FILTER (WHERE status NOT IN ('completed','closed','cancelled')),
		CASE WHEN count(*) FILTER (WHERE completed_at IS NOT NULL AND due_at IS NOT NULL) > 0 THEN (count(*) FILTER (WHERE completed_at IS NOT NULL AND due_at IS NOT NULL AND completed_at <= due_at))::float8 * 100 / count(*) FILTER (WHERE completed_at IS NOT NULL AND due_at IS NOT NULL) END,
		avg(EXTRACT(EPOCH FROM (completed_at - COALESCE(vendor_assigned_at, created_at)))/3600)::float8, COALESCE(sum(reopen_count),0), count(*) FILTER (WHERE created_at >= now() - interval '90 days')
		FROM work_orders WHERE vendor_id = $1`, v.ID).
		Scan(&v.Performance.TotalWorkOrders, &v.Performance.CompletedWorkOrders, &v.Performance.OpenWorkOrders, &v.Performance.OnTimePct, &v.Performance.AvgCompletionHours, &v.Performance.ReopenCount, &v.Performance.Last90dWorkOrders)
}

type Input struct {
	Name              *string    `json:"name"`
	ServiceCategories *[]string  `json:"service_categories"`
	ContactName       *string    `json:"contact_name"`
	ContactPhone      *string    `json:"contact_phone"`
	ContactEmail      *string    `json:"contact_email"`
	Address           *string    `json:"address"`
	TaxID             *string    `json:"tax_id"`
	ContractRef       *string    `json:"contract_ref"`
	ContractStart     *string    `json:"contract_start"`
	ContractEnd       *string    `json:"contract_end"`
	Status            *string    `json:"status"`
	Notes             *string    `json:"notes"`
	Contacts          *[]Contact `json:"contacts"`
}

var categories = map[string]bool{"hvac": true, "electrical": true, "plumbing": true, "lift": true, "cleaning": true, "security": true, "pest_control": true, "civil": true, "it": true, "landscaping": true, "fire_protection": true, "other": true}

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

func (s *Service) List(ctx context.Context, q, category, status string, page httpx.Page) ([]Vendor, *string, error) {
	var out []Vendor
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := " WHERE v.deleted_at IS NULL"
		var args []any
		if q = strings.TrimSpace(q); q != "" {
			args = append(args, "%"+q+"%")
			where += fmt.Sprintf(" AND (v.name ILIKE $%d OR v.vendor_code ILIKE $%d OR v.contact_name ILIKE $%d)", len(args), len(args), len(args))
		}
		if category != "" {
			args = append(args, category)
			where += fmt.Sprintf(" AND $%d = ANY(v.service_categories)", len(args))
		}
		if status != "" {
			args = append(args, status)
			where += fmt.Sprintf(" AND v.status = $%d", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (v.name, v.id) > ($%d, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, vSelect+where+fmt.Sprintf(" ORDER BY v.name, v.id LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		var items []Vendor
		for rows.Next() {
			v, err := scanVendor(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, *v)
		}
		rows.Close()
		if len(items) > page.Limit {
			last := items[page.Limit-1]
			c := httpx.EncodeCursor(last.Name, last.ID)
			next = &c
			items = items[:page.Limit]
		}
		for i := range items {
			if err := s.enrich(ctx, tx, &items[i]); err != nil {
				return err
			}
		}
		out = items
		return nil
	})
	if out == nil {
		out = []Vendor{}
	}
	return out, next, err
}

func (s *Service) getTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Vendor, error) {
	v, err := scanVendor(tx.QueryRow(ctx, vSelect+` WHERE v.id = $1 AND v.deleted_at IS NULL`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Vendor")
		}
		return nil, err
	}
	return v, s.enrich(ctx, tx, v)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Vendor, error) {
	var out *Vendor
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) Create(ctx context.Context, in Input) (*Vendor, error) {
	p := authctx.Must(ctx)
	name := strings.TrimSpace(deref(in.Name))
	if name == "" {
		return nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	cats := []string{}
	if in.ServiceCategories != nil {
		for _, c := range *in.ServiceCategories {
			if !categories[c] {
				return nil, apperr.Validation("service_categories tidak valid: " + c)
			}
			cats = append(cats, c)
		}
	}
	cs, err := parseDate(in.ContractStart)
	if err != nil {
		return nil, err
	}
	ce, err := parseDate(in.ContractEnd)
	if err != nil {
		return nil, err
	}
	var out *Vendor
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		code, err := ids.NextPlain(ctx, tx, p.OrganizationID, ids.PrefixVendor)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO vendors (organization_id, vendor_code, name, service_categories, contact_name, contact_phone, contact_email, address, tax_id, contract_ref, contract_start, contract_end, notes, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14) RETURNING id`, p.OrganizationID, code, name, cats, in.ContactName, in.ContactPhone, in.ContactEmail, in.Address, in.TaxID, in.ContractRef, cs, ce, in.Notes, p.UserID).Scan(&id); err != nil {
			return err
		}
		if in.Contacts != nil {
			if err := s.replaceContacts(ctx, tx, id, *in.Contacts); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "vendor", EntityID: &id, EntityLabel: code + " " + name})
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) replaceContacts(ctx context.Context, tx pgx.Tx, vendorID uuid.UUID, contacts []Contact) error {
	p := authctx.Must(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM vendor_contacts WHERE vendor_id = $1`, vendorID); err != nil {
		return err
	}
	for _, c := range contacts {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO vendor_contacts (organization_id, vendor_id, name, role, phone, email, is_primary) VALUES ($1,$2,$3,$4,$5,$6,$7)`, p.OrganizationID, vendorID, strings.TrimSpace(c.Name), c.Role, c.Phone, c.Email, c.IsPrimary); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input, ifVersion *int) (*Vendor, error) {
	p := authctx.Must(ctx)
	var out *Vendor
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		v, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != v.Version {
			return apperr.StaleVersion()
		}
		if in.Status != nil && *in.Status != "active" && *in.Status != "inactive" && *in.Status != "blacklisted" {
			return apperr.Validation("status harus active|inactive|blacklisted")
		}
		var cats any
		if in.ServiceCategories != nil {
			for _, c := range *in.ServiceCategories {
				if !categories[c] {
					return apperr.Validation("service_categories tidak valid: " + c)
				}
			}
			cats = *in.ServiceCategories
		}
		cs, err := parseDate(in.ContractStart)
		if err != nil {
			return err
		}
		ce, err := parseDate(in.ContractEnd)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE vendors SET name = COALESCE(NULLIF(TRIM($2),''), name), service_categories = COALESCE($3, service_categories), contact_name = COALESCE($4, contact_name), contact_phone = COALESCE($5, contact_phone), contact_email = COALESCE($6, contact_email),
			address = COALESCE($7, address), tax_id = COALESCE($8, tax_id), contract_ref = COALESCE($9, contract_ref), contract_start = COALESCE($10, contract_start), contract_end = COALESCE($11, contract_end), status = COALESCE($12, status), notes = COALESCE($13, notes), updated_by = $14 WHERE id = $1`,
			id, deref(in.Name), cats, in.ContactName, in.ContactPhone, in.ContactEmail, in.Address, in.TaxID, in.ContractRef, cs, ce, in.Status, in.Notes, p.UserID); err != nil {
			return err
		}
		if in.Contacts != nil {
			if err := s.replaceContacts(ctx, tx, id, *in.Contacts); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "vendor", EntityID: &id, EntityLabel: v.VendorCode, After: in})
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) Deactivate(ctx context.Context, id uuid.UUID) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		v, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if v.Performance.OpenWorkOrders > 0 {
			return apperr.Conflict("HAS_OPEN_WORK_ORDERS", fmt.Sprintf("%d Work Order vendor masih berjalan", v.Performance.OpenWorkOrders))
		}
		_, err = tx.Exec(ctx, `UPDATE vendors SET status = 'inactive', deleted_at = now(), updated_by = $2 WHERE id = $1`, id, p.UserID)
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "vendor", EntityID: &id, EntityLabel: v.VendorCode})
		return err
	})
}

// ---------- Vendor Work Order assignment (NC §37) ----------

type AssignInput struct {
	VendorID *uuid.UUID `json:"vendor_id"` // nil = lepas vendor
	Notes    *string    `json:"vendor_notes"`
}

type WOVendor struct {
	WorkOrderID      uuid.UUID  `json:"work_order_id"`
	WorkOrderNumber  string     `json:"work_order_number"`
	VendorID         *uuid.UUID `json:"vendor_id"`
	VendorName       *string    `json:"vendor_name"`
	VendorAssignedAt *time.Time `json:"vendor_assigned_at"`
	VendorNotes      *string    `json:"vendor_notes"`
}

func (s *Service) AssignWorkOrder(ctx context.Context, woID uuid.UUID, in AssignInput) (*WOVendor, error) {
	p := authctx.Must(ctx)
	var out WOVendor
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var propertyID uuid.UUID
		var status string
		if err := tx.QueryRow(ctx, `SELECT property_id, work_order_number, status FROM work_orders WHERE id = $1`, woID).Scan(&propertyID, &out.WorkOrderNumber, &status); err != nil {
			return apperr.NotFound("Work Order")
		}
		if err := iam.CanOnProperty(ctx, "vendor.assignments.create", propertyID); err != nil {
			return err
		}
		if status == "closed" || status == "cancelled" {
			return apperr.Conflict("OBJECT_TERMINAL", "Work Order sudah "+status)
		}
		if in.VendorID != nil {
			var st string
			if err := tx.QueryRow(ctx, `SELECT status FROM vendors WHERE id = $1 AND deleted_at IS NULL`, *in.VendorID).Scan(&st); err != nil {
				return apperr.Validation("vendor_id tidak ditemukan")
			}
			if st != "active" {
				return apperr.Validation("Vendor tidak aktif")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE work_orders SET vendor_id = $2, vendor_assigned_at = CASE WHEN $2::uuid IS NULL THEN NULL ELSE now() END, vendor_notes = COALESCE($3, vendor_notes), vendor_reference = COALESCE((SELECT name FROM vendors WHERE id = $2), vendor_reference), updated_by = $4 WHERE id = $1`, woID, in.VendorID, in.Notes, p.UserID); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: "work_order", ObjectID: woID, Action: "vendor_assigned", Payload: map[string]any{"vendor_id": in.VendorID}})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "work_order", EntityID: &woID, EntityLabel: out.WorkOrderNumber, After: map[string]any{"vendor_id": in.VendorID}})
		if s.Jobs != nil && in.VendorID != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: EventWorkOrderVendorAssigned, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: "work_order", ObjectID: woID, ObjectLabel: out.WorkOrderNumber, ActorUserID: &p.UserID, Payload: map[string]any{"vendor_id": *in.VendorID, "domain": "engineering"}})
		}
		out.WorkOrderID = woID
		return tx.QueryRow(ctx, `SELECT w.vendor_id, v.name, w.vendor_assigned_at, w.vendor_notes FROM work_orders w LEFT JOIN vendors v ON v.id = w.vendor_id WHERE w.id = $1`, woID).Scan(&out.VendorID, &out.VendorName, &out.VendorAssignedAt, &out.VendorNotes)
	})
	return &out, err
}

// WorkOrders: riwayat WO vendor (performance history).
type WORow struct {
	ID              uuid.UUID  `json:"id"`
	WorkOrderNumber string     `json:"work_order_number"`
	Title           string     `json:"title"`
	Status          string     `json:"status"`
	Priority        string     `json:"priority"`
	PropertyID      uuid.UUID  `json:"property_id"`
	AssignedAt      *time.Time `json:"vendor_assigned_at"`
	DueAt           *time.Time `json:"due_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	OnTime          *bool      `json:"on_time"`
}

func (s *Service) WorkOrders(ctx context.Context, vendorID uuid.UUID, page httpx.Page) ([]WORow, *string, error) {
	p := authctx.Must(ctx)
	var out []WORow
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{vendorID}
		where := " WHERE w.vendor_id = $1"
		if pids, all := p.PropertyIDsFor("operations.work_orders.view"); !all {
			args = append(args, pids)
			where += " AND w.property_id = ANY($2)"
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (w.created_at, w.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, `SELECT w.id, w.work_order_number, w.title, w.status, w.priority, w.property_id, w.vendor_assigned_at, w.due_at, w.completed_at, w.created_at FROM work_orders w`+where+fmt.Sprintf(" ORDER BY w.created_at DESC, w.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var items []WORow
		var lastCreated time.Time
		for rows.Next() {
			var r WORow
			var created time.Time
			if err := rows.Scan(&r.ID, &r.WorkOrderNumber, &r.Title, &r.Status, &r.Priority, &r.PropertyID, &r.AssignedAt, &r.DueAt, &r.CompletedAt, &created); err != nil {
				return err
			}
			if r.CompletedAt != nil && r.DueAt != nil {
				ok := !r.CompletedAt.After(*r.DueAt)
				r.OnTime = &ok
			}
			items = append(items, r)
			lastCreated = created
		}
		if len(items) > page.Limit {
			c := httpx.EncodeCursor(lastCreated.UTC().Format(time.RFC3339Nano), items[page.Limit-1].ID)
			next = &c
			items = items[:page.Limit]
		}
		out = items
		return rows.Err()
	})
	if out == nil {
		out = []WORow{}
	}
	return out, next, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
