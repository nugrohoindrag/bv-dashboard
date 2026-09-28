package billing

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Additional charge dari Work Order / Service Request (PRD P4 v2.1 P4-INV-09, exit gate §29) ----------
// Biaya WO (suku cadang + jasa = actual cost − suku cadang) dibebankan ke tenant sebagai invoice additional_charge yang tertaut
// ke WO/SR (invoices.source & invoice_items.source_*). Satu tagihan aktif per sumber kecuali allow_multiple.

type ChargeDraft struct {
	SourceType     string     `json:"source_type"`
	SourceID       uuid.UUID  `json:"source_id"`
	SourceNumber   string     `json:"source_number"`
	SourceTitle    string     `json:"source_title"`
	PropertyID     uuid.UUID  `json:"property_id"`
	TenantID       *uuid.UUID `json:"tenant_id"`
	TenantName     *string    `json:"tenant_name"`
	UnitLocationID *uuid.UUID `json:"unit_location_id"`
	UnitLabel      *string    `json:"unit_label"`
	Items          []Item     `json:"items"`
	ActualCost     *int64     `json:"actual_cost_amount"`
	ExistingIDs    []string   `json:"existing_invoice_numbers"`
}

// chargeDraftTx: saran item & pihak dari WO/SR.
func (s *Service) chargeDraftTx(ctx context.Context, tx pgx.Tx, sourceType string, sourceID uuid.UUID) (*ChargeDraft, error) {
	d := &ChargeDraft{SourceType: sourceType, SourceID: sourceID, Items: []Item{}, ExistingIDs: []string{}}
	var locationID *uuid.UUID
	var woIDs []uuid.UUID
	switch sourceType {
	case "work_order":
		var srcType *string
		var srcID *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id, work_order_number, title, location_id, actual_cost_amount, source_type, source_id FROM work_orders WHERE id = $1`, sourceID).
			Scan(&d.PropertyID, &d.SourceNumber, &d.SourceTitle, &locationID, &d.ActualCost, &srcType, &srcID); err != nil {
			return nil, apperr.NotFound("Work order")
		}
		woIDs = []uuid.UUID{sourceID}
		if srcType != nil && *srcType == "service_request" && srcID != nil {
			var tid *uuid.UUID
			var sl *uuid.UUID
			if tx.QueryRow(ctx, `SELECT tenant_id, location_id FROM service_requests WHERE id = $1`, *srcID).Scan(&tid, &sl) == nil {
				d.TenantID = tid
				if locationID == nil {
					locationID = sl
				}
			}
		}
	case "service_request":
		if err := tx.QueryRow(ctx, `SELECT property_id, request_number, title, location_id, tenant_id FROM service_requests WHERE id = $1`, sourceID).
			Scan(&d.PropertyID, &d.SourceNumber, &d.SourceTitle, &locationID, &d.TenantID); err != nil {
			return nil, apperr.NotFound("Service request")
		}
		rows, err := tx.Query(ctx, `SELECT id FROM work_orders WHERE source_type = 'service_request' AND source_id = $1 AND status <> 'cancelled' ORDER BY created_at`, sourceID)
		if err == nil {
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil {
					woIDs = append(woIDs, id)
				}
			}
			rows.Close()
		}
	default:
		return nil, apperr.Validation("source_type harus work_order atau service_request").WithField("source_type", "tidak valid")
	}
	// unit: lokasi WO/SR bila berupa unit (atau leluhur unit terdekat)
	if locationID != nil {
		var unit uuid.UUID
		if tx.QueryRow(ctx, `SELECT a.id FROM locations l JOIN locations a ON a.path @> l.path JOIN units un ON un.location_id = a.id WHERE l.id = $1 ORDER BY a.depth DESC LIMIT 1`, *locationID).Scan(&unit) == nil {
			d.UnitLocationID = &unit
			if d.TenantID == nil {
				var tid *uuid.UUID
				_ = tx.QueryRow(ctx, `SELECT tenant_id FROM units WHERE location_id = $1`, unit).Scan(&tid)
				d.TenantID = tid
			}
		}
	}
	if d.TenantID != nil {
		_ = tx.QueryRow(ctx, `SELECT name FROM tenants WHERE id = $1`, *d.TenantID).Scan(&d.TenantName)
	}
	if d.UnitLocationID != nil {
		_ = tx.QueryRow(ctx, `SELECT COALESCE('Unit ' || un.unit_number, l.name) FROM locations l LEFT JOIN units un ON un.location_id = l.id WHERE l.id = $1`, *d.UnitLocationID).Scan(&d.UnitLabel)
	}
	ct := "additional_charge"
	for _, wo := range woIDs {
		var woNum, woTitle string
		var actual, svcCost, otherCost *int64
		_ = tx.QueryRow(ctx, `SELECT work_order_number, title, actual_cost_amount, service_cost_amount, other_cost_amount FROM work_orders WHERE id = $1`, wo).Scan(&woNum, &woTitle, &actual, &svcCost, &otherCost)
		var partsTotal int64
		rows, err := tx.Query(ctx, `SELECT ii.name, ii.unit, wp.quantity::float8, wp.unit_cost, wp.total_cost FROM work_order_parts wp JOIN inventory_items ii ON ii.id = wp.item_id WHERE wp.work_order_id = $1 ORDER BY wp.recorded_at`, wo)
		if err == nil {
			for rows.Next() {
				var name string
				var unit *string
				var qty float64
				var unitCost, total *int64
				if rows.Scan(&name, &unit, &qty, &unitCost, &total) != nil {
					continue
				}
				price := int64(0)
				if unitCost != nil {
					price = *unitCost
				}
				amt := RoundMul(qty, price)
				desc := "Suku cadang: " + name + " (" + woNum + ")"
				if total != nil && *total > 0 && *total != amt {
					// biaya tercatat ≠ qty × harga (pembulatan/pecahan): tagihkan 1 × biaya tercatat agar jumlah saran = jumlah invoice
					u := ""
					if unit != nil {
						u = " " + *unit
					}
					desc += " — " + strconv.FormatFloat(qty, 'f', -1, 64) + u
					qty, price, amt, unit = 1, *total, *total, nil
				}
				partsTotal += amt
				st, id := "work_order", wo
				d.Items = append(d.Items, Item{Description: desc, Quantity: qty, Unit: unit, UnitPrice: price, Amount: amt, ChargeType: &ct, SourceType: &st, SourceID: &id})
			}
			rows.Close()
		}
		// rincian biaya WO (PRD P1 v2 §24): jasa & biaya lain bila diisi; bila tidak, jasa = actual − suku cadang
		st, id := "work_order", wo
		switch {
		case svcCost != nil || otherCost != nil:
			if svcCost != nil && *svcCost > 0 {
				d.Items = append(d.Items, Item{Description: "Jasa perbaikan " + woNum + ": " + woTitle, Quantity: 1, UnitPrice: *svcCost, Amount: *svcCost, ChargeType: &ct, SourceType: &st, SourceID: &id})
			}
			if otherCost != nil && *otherCost > 0 {
				d.Items = append(d.Items, Item{Description: "Biaya lain " + woNum, Quantity: 1, UnitPrice: *otherCost, Amount: *otherCost, ChargeType: &ct, SourceType: &st, SourceID: &id})
			}
		case actual != nil && *actual > partsTotal:
			svc := *actual - partsTotal
			d.Items = append(d.Items, Item{Description: "Jasa perbaikan " + woNum + ": " + woTitle, Quantity: 1, UnitPrice: svc, Amount: svc, ChargeType: &ct, SourceType: &st, SourceID: &id})
		}
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT i.invoice_number FROM invoices i LEFT JOIN invoice_items it ON it.invoice_id = i.id
		WHERE i.status <> 'cancelled' AND i.invoice_number IS NOT NULL AND ((i.source = $1 AND i.source_id = $2) OR (it.source_type = $1 AND it.source_id = $2))`, sourceType, sourceID)
	if err == nil {
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				d.ExistingIDs = append(d.ExistingIDs, n)
			}
		}
		rows.Close()
	}
	return d, nil
}

func (s *Service) ChargeDraft(ctx context.Context, sourceType string, sourceID uuid.UUID) (*ChargeDraft, error) {
	var out *ChargeDraft
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		d, err := s.chargeDraftTx(ctx, tx, sourceType, sourceID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.invoices.create", d.PropertyID); err != nil {
			return err
		}
		out = d
		return nil
	})
	return out, err
}

type ChargeInput struct {
	SourceType     string     `json:"source_type"`
	SourceID       uuid.UUID  `json:"source_id"`
	TenantID       *uuid.UUID `json:"tenant_id"`        // null = saran WO/SR; UUID nol = tanpa tenant
	UnitLocationID *uuid.UUID `json:"unit_location_id"` // null = saran WO/SR; UUID nol = tanpa unit
	Items          *[]Item    `json:"items"`            // kosong = saran dari WO/SR
	Description    *string    `json:"description"`
	DueDate        *string    `json:"due_date"`
	IssueNow       bool       `json:"issue_now"`
	AllowMultiple  bool       `json:"allow_multiple"`
	Notes          *string    `json:"notes"`
}

// CreateCharge: invoice additional_charge tertaut ke WO/SR.
func (s *Service) CreateCharge(ctx context.Context, in ChargeInput) (*Invoice, error) {
	var out *Invoice
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		d, err := s.chargeDraftTx(ctx, tx, in.SourceType, in.SourceID)
		if err != nil {
			return err
		}
		if err := iam.CanOnProperty(ctx, "billing.invoices.create", d.PropertyID); err != nil {
			return err
		}
		if in.IssueNow {
			if err := iam.CanOnProperty(ctx, "billing.invoices.issue", d.PropertyID); err != nil {
				return err
			}
		}
		if len(d.ExistingIDs) > 0 && !in.AllowMultiple {
			return apperr.Conflict("ALREADY_CHARGED", "Sumber ini sudah ditagihkan ("+strings.Join(d.ExistingIDs, ", ")+"); batalkan invoice lama atau centang tagih ulang").WithMeta("invoice_numbers", d.ExistingIDs)
		}
		// null = pakai saran dari WO/SR; UUID nol = kosongkan (mis. tagih unit saja tanpa tenant)
		tenant, unit := d.TenantID, d.UnitLocationID
		if in.TenantID != nil {
			tenant = in.TenantID
			if *in.TenantID == uuid.Nil {
				tenant = nil
			}
		}
		if in.UnitLocationID != nil {
			unit = in.UnitLocationID
			if *in.UnitLocationID == uuid.Nil {
				unit = nil
			}
		}
		items := d.Items
		if in.Items != nil {
			items = *in.Items
			st := in.SourceType
			sid := in.SourceID
			for i := range items {
				if items[i].SourceType == nil {
					items[i].SourceType, items[i].SourceID = &st, &sid
				}
			}
		}
		if len(items) == 0 {
			return apperr.Validation("Belum ada biaya pada sumber ini; isi item tagihan").WithField("items", "wajib")
		}
		desc := in.Description
		if desc == nil || *desc == "" {
			x := fmt.Sprintf("Biaya tambahan %s — %s", d.SourceNumber, d.SourceTitle)
			desc = &x
		}
		due := in.DueDate
		if due == nil || *due == "" {
			loc := property.PropertyTimezone(ctx, tx, d.PropertyID)
			st := s.settingsFor(ctx, tx, &d.PropertyID)
			x := time.Now().In(loc).AddDate(0, 0, st.DefaultDueDays).Format("2006-01-02")
			due = &x
		}
		itype := "additional_charge"
		id, err := s.CreateTx(ctx, tx, InvoiceInput{PropertyID: &d.PropertyID, TenantID: tenant, UnitLocationID: unit, InvoiceType: &itype, Description: desc, DueDate: due, Items: &items, Notes: in.Notes, IssueNow: in.IssueNow}, in.SourceType, &in.SourceID)
		if err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: in.SourceType, ObjectID: in.SourceID, Action: "charged_to_tenant", Payload: map[string]any{"invoice_id": id}})
		out, err = s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := loadItems(ctx, tx, out); err != nil {
			return err
		}
		s.staffActions(ctx, out)
		return nil
	})
	return out, err
}

// ---------- Impor invoice dari CSV (P4-BRL-05) ----------
// Kolom (judul, tidak peka huruf): tenant_code | unit (nomor unit) — minimal satu; invoice_type; description; quantity; unit_price
// atau amount; due_date; period_start; period_end; external_ref; group (baris dengan group sama → satu invoice banyak item).

type InvoiceImportInput struct {
	PropertyID    uuid.UUID `json:"property_id"`
	FileName      string    `json:"file_name"`
	ContentBase64 string    `json:"content_base64"`
	Content       string    `json:"content"`
	DryRun        bool      `json:"dry_run"`
	IssueNow      bool      `json:"issue_now"`
}

type ImportRowError struct {
	Row     int    `json:"row"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type InvoiceImportResult struct {
	Rows       int              `json:"rows"`
	Invoices   int              `json:"invoices"`
	Total      int64            `json:"total_amount"`
	Errors     []ImportRowError `json:"errors"`
	InvoiceIDs []uuid.UUID      `json:"invoice_ids"`
	DryRun     bool             `json:"dry_run"`
}

func (s *Service) ImportInvoices(ctx context.Context, in InvoiceImportInput) (*InvoiceImportResult, error) {
	p := authctx.Must(ctx)
	if !p.HasOnProperty("billing.invoices.import", in.PropertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.invoices.import")
	}
	if in.IssueNow && !p.HasOnProperty("billing.invoices.issue", in.PropertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.invoices.issue")
	}
	data := []byte(in.Content)
	if in.ContentBase64 != "" {
		b, err := base64.StdEncoding.DecodeString(in.ContentBase64)
		if err != nil {
			return nil, apperr.Validation("content_base64 tidak valid")
		}
		data = b
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, apperr.Validation("File wajib").WithField("content_base64", "wajib")
	}
	rows, _, err := readRows(in.FileName, data)
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, apperr.Validation("File tidak berisi baris data")
	}
	idx := map[string]int{}
	for i, h := range rows[0] {
		idx[strings.ReplaceAll(normHeader(h), " ", "_")] = i
	}
	col := func(r []string, k string) string {
		i, ok := idx[k]
		if !ok || i >= len(r) {
			return ""
		}
		return strings.TrimSpace(r[i])
	}
	if _, ok := idx["tenant_code"]; !ok {
		if _, ok := idx["unit"]; !ok {
			return nil, apperr.Validation("Kolom tenant_code atau unit wajib ada")
		}
	}
	res := &InvoiceImportResult{Errors: []ImportRowError{}, InvoiceIDs: []uuid.UUID{}, DryRun: in.DryRun}
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		type group struct {
			tenant, unit *uuid.UUID
			itype        string
			desc         string
			due          string
			ps, pe       string
			ext          string
			items        []Item
		}
		groups := map[string]*group{}
		var order []string
		year := time.Now().Year()
		normDate := func(v string) (string, bool) {
			if v == "" {
				return "", true
			}
			t, ok := parseTxnDate(v, "", year)
			if !ok {
				return "", false
			}
			return t.Format("2006-01-02"), true
		}
		for ri, r := range rows[1:] {
			rowNo := ri + 2
			if strings.TrimSpace(strings.Join(r, "")) == "" {
				continue
			}
			res.Rows++
			fail := func(field, msg string) {
				res.Errors = append(res.Errors, ImportRowError{Row: rowNo, Field: field, Message: msg})
			}
			var tenantID, unitID *uuid.UUID
			if code := col(r, "tenant_code"); code != "" {
				var id uuid.UUID
				if err := tx.QueryRow(ctx, `SELECT id FROM tenants WHERE property_id = $1 AND tenant_code = $2 AND deleted_at IS NULL`, in.PropertyID, code).Scan(&id); err != nil {
					fail("tenant_code", "Tenant "+code+" tidak ditemukan")
					continue
				}
				tenantID = &id
			}
			if un := col(r, "unit"); un != "" {
				var id uuid.UUID
				if err := tx.QueryRow(ctx, `SELECT un.location_id FROM units un JOIN locations l ON l.id = un.location_id WHERE l.property_id = $1 AND upper(un.unit_number) = upper($2) AND l.deleted_at IS NULL`, in.PropertyID, un).Scan(&id); err != nil {
					fail("unit", "Unit "+un+" tidak ditemukan")
					continue
				}
				unitID = &id
			}
			if tenantID == nil && unitID == nil {
				fail("tenant_code", "tenant_code atau unit wajib")
				continue
			}
			itype := strings.ToLower(col(r, "invoice_type"))
			if itype == "" {
				itype = "service_charge"
			}
			if !invoiceTypes[itype] {
				fail("invoice_type", "Tipe "+itype+" tidak valid")
				continue
			}
			desc := col(r, "description")
			if desc == "" {
				fail("description", "Wajib")
				continue
			}
			qty := 1.0
			if q := col(r, "quantity"); q != "" {
				v, err := strconv.ParseFloat(strings.ReplaceAll(q, ",", "."), 64)
				if err != nil || v <= 0 {
					fail("quantity", "Harus angka > 0")
					continue
				}
				qty = v
			}
			var price int64
			if v := col(r, "unit_price"); v != "" {
				a, _, ok := ParseAmount(v)
				if !ok || a < 0 {
					fail("unit_price", "Tidak valid")
					continue
				}
				price = a
			} else if v := col(r, "amount"); v != "" {
				a, _, ok := ParseAmount(v)
				if !ok || a <= 0 {
					fail("amount", "Tidak valid")
					continue
				}
				price, qty = a, 1
			} else {
				fail("amount", "unit_price atau amount wajib")
				continue
			}
			due, ok := normDate(col(r, "due_date"))
			if !ok || due == "" {
				fail("due_date", "Wajib, format YYYY-MM-DD atau DD/MM/YYYY")
				continue
			}
			ps, ok1 := normDate(col(r, "period_start"))
			pe, ok2 := normDate(col(r, "period_end"))
			if !ok1 || !ok2 {
				fail("period_start", "Format tanggal tidak valid")
				continue
			}
			key := col(r, "group")
			if key == "" {
				key = fmt.Sprintf("row-%d", rowNo)
			}
			g, ok := groups[key]
			if !ok {
				g = &group{tenant: tenantID, unit: unitID, itype: itype, desc: desc, due: due, ps: ps, pe: pe, ext: col(r, "external_ref")}
				groups[key] = g
				order = append(order, key)
			} else if (g.tenant == nil) != (tenantID == nil) || (g.tenant != nil && *g.tenant != *tenantID) {
				fail("group", "Baris dalam satu group harus untuk tenant/unit yang sama")
				continue
			}
			ct := itype
			g.items = append(g.items, Item{Description: desc, Quantity: qty, UnitPrice: price, ChargeType: &ct})
			res.Total += RoundMul(qty, price)
		}
		res.Invoices = len(order)
		if len(res.Errors) > 0 || in.DryRun {
			return nil
		}
		for _, key := range order {
			g := groups[key]
			items := g.items
			inv := InvoiceInput{PropertyID: &in.PropertyID, TenantID: g.tenant, UnitLocationID: g.unit, InvoiceType: &g.itype, Description: &g.desc, DueDate: &g.due, Items: &items, IssueNow: in.IssueNow}
			if g.ps != "" {
				inv.PeriodStart = &g.ps
			}
			if g.pe != "" {
				inv.PeriodEnd = &g.pe
			}
			if g.ext != "" {
				inv.ExternalRef = &g.ext
			}
			id, err := s.CreateTx(ctx, tx, inv, "import", nil)
			if err != nil {
				return fmt.Errorf("group %s: %w", key, err)
			}
			res.InvoiceIDs = append(res.InvoiceIDs, id)
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "invoice_import", EntityLabel: in.FileName, After: map[string]any{"rows": res.Rows, "invoices": res.Invoices, "total": res.Total, "issued": in.IssueNow}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}
