// Package exports: CSV/XLSX export (PRD §23 Should; TAD §5.17) — job → object storage → notification dengan signed URL.
package exports

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/storage"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantservice"
	"github.com/buildingvision/api/internal/vendor"
)

type Service struct {
	DB          *db.DB
	Jobs        jobs.Enqueuer
	Storage     storage.Storage
	IAM         *iam.Service
	Ops         *operations.Service
	Assets      *asset.Service
	SR          *tenantservice.Service
	Property    *property.Service // PRD P0 v2 §18: locations & tenants
	Vendors     *vendor.Service
	DownloadTTL time.Duration
}

type Export struct {
	ID          uuid.UUID  `json:"id"`
	Resource    string     `json:"resource"`
	Format      string     `json:"format"`
	Status      string     `json:"status"`
	RowCount    *int       `json:"row_count"`
	Error       *string    `json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	DownloadURL *string    `json:"download_url"`
}

// resources → permission view yang wajib dimiliki peminta (dicek saat request; scope baris tetap diterapkan List service).
var resources = map[string]string{
	"tasks": "operations.tasks.view", "work_orders": "operations.work_orders.view", "service_requests": "tenant.service_requests.view",
	"incidents": "operations.incidents.view", "assets": "engineering.assets.view", "findings": "operations.findings.view",
	// PRD P0 v2 §18
	"users": "iam.users.view", "locations": "property.locations.view", "tenants": "property.tenants.view",
	"vendors": "vendor.vendors.view", "audit_logs": "platform.audit_logs.view",
}

// Resources: daftar resource yang dapat diekspor.
func Resources() []string {
	out := make([]string, 0, len(resources))
	for k := range resources {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Request: buat export request → job (audit: export selalu dicatat).
func (s *Service) Request(ctx context.Context, resource, format string, filters map[string]string) (*Export, error) {
	p := authctx.Must(ctx)
	perm, ok := resources[resource]
	if !ok {
		return nil, apperr.Validation("resource harus salah satu: " + strings.Join(Resources(), "|"))
	}
	// export mengikuti permission user (PRD P0 v2 §18): tanpa akses view resource → ditolak di depan
	if !p.Has(perm) || (p.VendorID != nil && !p.IsSystem) {
		return nil, apperr.Forbidden("Memerlukan permission " + perm)
	}
	if format != "csv" && format != "xlsx" {
		return nil, apperr.Validation("format harus csv|xlsx")
	}
	if filters == nil {
		filters = map[string]string{}
	}
	fb, _ := json.Marshal(filters)
	var out Export
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO exports (organization_id, requested_by, resource, filters, format) VALUES ($1,$2,$3,$4,$5) RETURNING id, resource, format, status, created_at`,
			p.OrganizationID, p.UserID, resource, fb, format).Scan(&out.ID, &out.Resource, &out.Format, &out.Status, &out.CreatedAt); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditExport, EntityType: "export", EntityID: &out.ID, EntityLabel: resource, After: filters})
		if s.Jobs != nil {
			return s.Jobs.EnqueueTx(ctx, tx, jobs.ExportGenerateArgs{ExportID: out.ID, OrganizationID: p.OrganizationID})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListMine: riwayat export milik user (30 hari terakhir).
func (s *Service) ListMine(ctx context.Context) ([]Export, error) {
	p := authctx.Must(ctx)
	out := []Export{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, resource, format, status, row_count, error, created_at, completed_at, expires_at FROM exports
			WHERE requested_by = $1 AND created_at > now() - interval '30 days' ORDER BY created_at DESC LIMIT 100`, p.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x Export
			if err := rows.Scan(&x.ID, &x.Resource, &x.Format, &x.Status, &x.RowCount, &x.Error, &x.CreatedAt, &x.CompletedAt, &x.ExpiresAt); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Export, error) {
	p := authctx.Must(ctx)
	var out Export
	var key *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id, resource, format, status, row_count, error, created_at, completed_at, expires_at, storage_key FROM exports WHERE id = $1 AND requested_by = $2`, id, p.UserID).
			Scan(&out.ID, &out.Resource, &out.Format, &out.Status, &out.RowCount, &out.Error, &out.CreatedAt, &out.CompletedAt, &out.ExpiresAt, &key)
	})
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Export")
		}
		return nil, err
	}
	if key != nil && out.Status == "ready" {
		if u, err := s.Storage.PresignGet(ctx, *key, s.DownloadTTL); err == nil {
			out.DownloadURL = &u
		}
	}
	return &out, nil
}

// Generate (worker): jalankan list dengan principal si peminta (permission tetap berlaku), tulis file, simpan, notifikasi.
func (s *Service) Generate(ctx context.Context, orgID, exportID uuid.UUID) error {
	var resource, format string
	var filters map[string]string
	var requestedBy uuid.UUID
	if err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var fb []byte
		if err := tx.QueryRow(ctx, `UPDATE exports SET status = 'processing' WHERE id = $1 AND status = 'pending' RETURNING resource, format, filters, requested_by`, exportID).Scan(&resource, &format, &fb, &requestedBy); err != nil {
			return err
		}
		return json.Unmarshal(fb, &filters)
	}); err != nil {
		if db.IsNoRows(err) {
			return nil
		}
		return err
	}
	principal, err := s.IAM.LoadPrincipal(ctx, requestedBy, orgID)
	if err != nil {
		return s.fail(ctx, orgID, exportID, err)
	}
	ctx = authctx.With(ctx, principal)
	headers, rows, err := s.collect(ctx, resource, filters)
	if err != nil {
		return s.fail(ctx, orgID, exportID, err)
	}
	var buf bytes.Buffer
	ct := "text/csv"
	ext := ".csv"
	if format == "xlsx" {
		f := excelize.NewFile()
		sheet := "Sheet1"
		_ = f.SetSheetRow(sheet, "A1", &headers)
		for i, r := range rows {
			cell, _ := excelize.CoordinatesToCellName(1, i+2)
			_ = f.SetSheetRow(sheet, cell, &r)
		}
		if err := f.Write(&buf); err != nil {
			return s.fail(ctx, orgID, exportID, err)
		}
		ct = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		ext = ".xlsx"
	} else {
		buf.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM agar Excel membaca aksen dengan benar
		w := csv.NewWriter(&buf)
		_ = w.Write(headers)
		for _, r := range rows {
			strs := make([]string, len(r))
			for i, v := range r {
				strs[i] = fmt.Sprint(v)
			}
			_ = w.Write(strs)
		}
		w.Flush()
	}
	key := fmt.Sprintf("org/%s/exports/%s/%s%s", orgID, time.Now().UTC().Format("2006/01"), exportID, ext)
	if err := s.Storage.Put(ctx, key, ct, bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
		return s.fail(ctx, orgID, exportID, err)
	}
	return s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE exports SET status = 'ready', storage_key = $2, row_count = $3, completed_at = now(), expires_at = now() + interval '24 hours' WHERE id = $1`, exportID, key, len(rows)); err != nil {
			return err
		}
		if s.Jobs != nil {
			return s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.ExportReady, OrganizationID: orgID, ObjectType: "export", ObjectID: exportID, ObjectLabel: resource + ext, Payload: map[string]any{"requester_user_id": requestedBy}})
		}
		return nil
	})
}

func (s *Service) fail(ctx context.Context, orgID, exportID uuid.UUID, cause error) error {
	_ = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE exports SET status = 'failed', error = $2, completed_at = now() WHERE id = $1`, exportID, cause.Error())
		return err
	})
	return nil
}

func (s *Service) collect(ctx context.Context, resource string, filters map[string]string) ([]string, [][]any, error) {
	q := url.Values{}
	for k, v := range filters {
		q.Set(k, v)
	}
	fmtTime := func(t *time.Time) any {
		if t == nil {
			return ""
		}
		return t.In(jakarta()).Format("02 Jan 2006, 15:04")
	}
	deref := func(s *string) any {
		if s == nil {
			return ""
		}
		return *s
	}
	if h, rows, ok, err := s.collectFinance(ctx, resource, q); ok {
		return h, rows, err
	}
	switch resource {
	case "tasks", "work_orders":
		ot := operations.ObjTask
		if resource == "work_orders" {
			ot = operations.ObjWorkOrder
		}
		f := listFilterFromQuery(q)
		headers := []string{"Number", "Type", "Title", "Status", "Priority", "Location", "Asset", "Assignee", "Team", "Due", "Overdue", "SLA Risk", "Created", "Completed", "Closed",
			"Category", "SLA Status", "Escalation Level", "Completion Notes", "Estimated Cost", "Actual Cost", "Parts Cost", "Service Cost", "Other Cost"}
		money := func(m *operations.Money) any {
			if m == nil {
				return ""
			}
			return m.Amount
		}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			items, next, err := s.Ops.List(ctx, ot, f, page)
			if err != nil {
				return nil, nil, err
			}
			for _, w := range items {
				rows = append(rows, []any{w.Number, w.Type, w.Title, string(w.Status), w.Priority, deref(w.Location.PathText), deref(w.Asset.AssetCode), deref(w.Assignee.UserName), deref(w.Assignee.TeamName), fmtTime(w.DueAt), w.IsOverdue, w.SLARiskAt != nil, fmtTime(&w.CreatedAt), fmtTime(w.CompletedAt), fmtTime(w.ClosedAt),
					deref(w.Category), w.SLAStatus, w.EscalationLevel, deref(w.CompletionNotes), money(w.EstimatedCost), money(w.ActualCost), money(w.PartsCost), money(w.ServiceCost), money(w.OtherCost)})
			}
			if next == nil || len(rows) >= 50000 {
				break
			}
			c, _ := httpx.DecodeCursor(*next)
			page.Cursor = c
		}
		return headers, rows, nil
	case "service_requests":
		var f tenantservice.Filter
		f.PropertyID = parseUUID(q.Get("property_id"))
		f.Statuses = splitCSV(q.Get("status"))
		f.Priorities = splitCSV(q.Get("priority"))
		f.Q = q.Get("q")
		f.RequestTypes = splitCSV(q.Get("request_type"))
		f.SLAStatus = splitCSV(q.Get("sla_status"))
		if q.Get("open") == "true" {
			b := true
			f.Open = &b
		}
		headers := []string{"Number", "Category", "Title", "Tenant", "Status", "Priority", "Location", "Assignee", "Team", "Created", "Resolved", "Closed", "Request Type", "SLA Status", "Reopen Count", "Rating"}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			items, next, err := s.SR.List(ctx, f, page)
			if err != nil {
				return nil, nil, err
			}
			for _, r := range items {
				rating := any("")
				if r.Feedback != nil {
					rating = r.Feedback.Rating
				}
				rows = append(rows, []any{r.RequestNumber, r.CategoryCode, r.Title, deref(r.TenantName), string(r.Status), r.Priority, deref(r.Location.PathText), deref(r.Assignee.UserName), deref(r.Assignee.TeamName), fmtTime(&r.CreatedAt), fmtTime(r.ResolvedAt), fmtTime(r.ClosedAt),
					r.RequestType, r.SLAStatus, r.ReopenCount, rating})
			}
			if next == nil || len(rows) >= 50000 {
				break
			}
			c, _ := httpx.DecodeCursor(*next)
			page.Cursor = c
		}
		return headers, rows, nil
	case "incidents":
		var f operations.IncidentFilter
		f.PropertyID = parseUUID(q.Get("property_id"))
		f.Statuses = splitCSV(q.Get("status"))
		f.Severities = splitCSV(q.Get("severity"))
		f.SLAStatus = splitCSV(q.Get("sla_status"))
		headers := []string{"Number", "Category", "Title", "Severity", "Priority", "Status", "Location", "Reported By", "Reported At", "Resolved", "Closed", "Action Taken", "Resolution", "SLA Status"}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			items, next, err := s.Ops.ListIncidents(ctx, f, page)
			if err != nil {
				return nil, nil, err
			}
			for _, i := range items {
				rows = append(rows, []any{i.IncidentNumber, i.Category, i.Title, i.Severity, i.Priority, string(i.Status), deref(i.Location.PathText), deref(i.ReportedByName), fmtTime(&i.ReportedAt), fmtTime(i.ResolvedAt), fmtTime(i.ClosedAt),
					deref(i.ActionTaken), deref(i.Resolution), i.SLAStatus})
			}
			if next == nil || len(rows) >= 50000 {
				break
			}
			c, _ := httpx.DecodeCursor(*next)
			page.Cursor = c
		}
		return headers, rows, nil
	case "findings":
		var f operations.FindingFilter
		f.PropertyID = parseUUID(q.Get("property_id"))
		f.Statuses = splitCSV(q.Get("status"))
		f.Severities = splitCSV(q.Get("severity"))
		headers := []string{"Number", "Type", "Title", "Severity", "Status", "Location", "Source", "Reported By", "Reported At", "Resolved"}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			items, next, err := s.Ops.ListFindings(ctx, f, page)
			if err != nil {
				return nil, nil, err
			}
			for _, x := range items {
				rows = append(rows, []any{x.FindingNumber, x.FindingType, x.Title, x.Severity, string(x.Status), deref(x.Location.PathText), x.SourceLabel, deref(x.ReportedByName), fmtTime(&x.ReportedAt), fmtTime(x.ResolvedAt)})
			}
			if next == nil || len(rows) >= 50000 {
				break
			}
			c, _ := httpx.DecodeCursor(*next)
			page.Cursor = c
		}
		return headers, rows, nil
	case "assets":
		var f asset.Filter
		f.PropertyID = parseUUID(q.Get("property_id"))
		f.Statuses = splitCSV(q.Get("status"))
		f.CategoryCode = q.Get("category")
		f.Q = q.Get("q")
		headers := []string{"Asset ID", "Name", "Category", "Type", "Location", "Status", "Criticality", "Manufacturer", "Model", "Serial", "Next PM Due"}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			items, next, err := s.Assets.List(ctx, f, page)
			if err != nil {
				return nil, nil, err
			}
			for _, a := range items {
				rows = append(rows, []any{a.AssetCode, a.Name, a.CategoryName, deref(a.TypeName), a.LocationPath, a.Status, deref(a.Criticality), deref(a.Manufacturer), deref(a.Model), deref(a.SerialNumber), fmtTime(a.NextPMDue)})
			}
			if next == nil || len(rows) >= 50000 {
				break
			}
			c, _ := httpx.DecodeCursor(*next)
			page.Cursor = c
		}
		return headers, rows, nil
	case "users":
		f := iam.UserFilter{Q: q.Get("q"), RoleCode: q.Get("role"), PropertyID: parseUUID(q.Get("property_id"))}
		if v := q.Get("is_active"); v != "" {
			b := v == "true"
			f.IsActive = &b
		}
		headers := []string{"User ID", "Name", "Email", "Username", "Phone", "Active", "Roles", "Teams", "Last Login"}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			items, next, err := s.IAM.ListUsers(ctx, f, page)
			if err != nil {
				return nil, nil, err
			}
			for _, u := range items {
				roles := make([]string, 0, len(u.Roles))
				for _, r := range u.Roles {
					roles = append(roles, r.RoleName)
				}
				teams := make([]string, 0, len(u.Teams))
				for _, t := range u.Teams {
					teams = append(teams, t.TeamName)
				}
				rows = append(rows, []any{u.UserCode, u.FullName, deref(u.Email), deref(u.Username), deref(u.Phone), yesNo(u.IsActive), strings.Join(roles, ", "), strings.Join(teams, ", "), fmtTime(u.LastLoginAt)})
			}
			if next == nil || len(rows) >= 50000 {
				break
			}
			c, _ := httpx.DecodeCursor(*next)
			page.Cursor = c
		}
		return headers, rows, nil
	case "locations":
		f := property.LocationFilter{PropertyID: parseUUID(q.Get("property_id")), AncestorID: parseUUID(q.Get("ancestor_id")), PortfolioID: parseUUID(q.Get("portfolio_id")), LocationType: property.LocationType(q.Get("location_type")), Q: q.Get("q"), IncludeInactive: q.Get("include_inactive") == "true"}
		items, err := s.Property.ListLocations(ctx, f)
		if err != nil {
			return nil, nil, err
		}
		headers := []string{"Code", "Name", "Type", "Path", "Active"}
		rows := make([][]any, 0, len(items))
		for _, l := range items {
			rows = append(rows, []any{l.Code, l.Name, string(l.LocationType), l.PathText, yesNo(l.IsActive)})
		}
		return headers, rows, nil
	case "tenants":
		f := property.TenantFilter{PropertyID: parseUUID(q.Get("property_id")), Status: q.Get("status"), Q: q.Get("q")}
		headers := []string{"Tenant ID", "Name", "Type", "Contact", "Phone", "Email", "Status", "Units", "Open Requests"}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			items, next, err := s.Property.ListTenants(ctx, f, page)
			if err != nil {
				return nil, nil, err
			}
			for _, t := range items {
				units := make([]string, 0, len(t.Units))
				for _, u := range t.Units {
					units = append(units, u.UnitNumber)
				}
				rows = append(rows, []any{t.TenantCode, t.Name, t.TenantType, deref(t.ContactName), deref(t.ContactPhone), deref(t.ContactEmail), t.Status, strings.Join(units, ", "), t.OpenRequests})
			}
			if next == nil || len(rows) >= 50000 {
				break
			}
			c, _ := httpx.DecodeCursor(*next)
			page.Cursor = c
		}
		return headers, rows, nil
	case "vendors":
		headers := []string{"Vendor ID", "Name", "Categories", "Contact", "Phone", "Email", "Status", "Contract", "Contract End"}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			items, next, err := s.Vendors.List(ctx, q.Get("q"), q.Get("category"), q.Get("status"), page)
			if err != nil {
				return nil, nil, err
			}
			for _, v := range items {
				end := ""
				if v.ContractEnd != nil {
					end = v.ContractEnd.Format("2006-01-02")
				}
				rows = append(rows, []any{v.VendorCode, v.Name, strings.Join(v.ServiceCategories, ", "), deref(v.ContactName), deref(v.ContactPhone), deref(v.ContactEmail), v.Status, deref(v.ContractRef), end})
			}
			if next == nil || len(rows) >= 50000 {
				break
			}
			c, _ := httpx.DecodeCursor(*next)
			page.Cursor = c
		}
		return headers, rows, nil
	case "audit_logs":
		var f audit.AuditFilter
		f.EntityType = q.Get("entity_type")
		f.Action = q.Get("action")
		f.EntityID = parseUUID(q.Get("entity_id"))
		f.ActorID = parseUUID(q.Get("actor_id"))
		if t, err := time.Parse(time.RFC3339, q.Get("from")); err == nil {
			f.From = &t
		}
		if t, err := time.Parse(time.RFC3339, q.Get("to")); err == nil {
			f.To = &t
		}
		headers := []string{"Time", "Actor", "Action", "Entity", "Label", "IP", "Request ID", "Before", "After"}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			var items []audit.AuditLog
			var next *string
			if err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				var err error
				items, next, err = audit.ListAuditLogs(ctx, tx, f, page)
				return err
			}); err != nil {
				return nil, nil, err
			}
			for _, l := range items {
				t := l.OccurredAt
				rows = append(rows, []any{fmtTime(&t), l.ActorName, l.Action, l.EntityType, deref(l.EntityLabel), deref(l.IP), deref(l.RequestID), string(l.Before), string(l.After)})
			}
			if next == nil || len(rows) >= 50000 {
				break
			}
			c, _ := httpx.DecodeCursor(*next)
			page.Cursor = c
		}
		return headers, rows, nil
	}
	return nil, nil, fmt.Errorf("resource tidak dikenal")
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func listFilterFromQuery(q url.Values) operations.ListFilter {
	var f operations.ListFilter
	f.PropertyID = parseUUID(q.Get("property_id"))
	f.Types = splitCSV(q.Get("type"))
	f.Statuses = splitCSV(q.Get("status"))
	f.Priorities = splitCSV(q.Get("priority"))
	f.LocationID = parseUUID(q.Get("location_id"))
	for _, k := range []string{"unit_id", "area_id", "floor_id", "tower_id", "building_id"} {
		if f.LocationID == nil {
			f.LocationID = parseUUID(q.Get(k))
		}
	}
	f.AssetID = parseUUID(q.Get("asset_id"))
	f.EquipmentID = parseUUID(q.Get("equipment_id"))
	f.VendorID = parseUUID(q.Get("vendor_id"))
	f.DueFrom = parseTime(q.Get("due_from"))
	f.DueTo = parseTime(q.Get("due_to"))
	f.CreatedFrom = parseTime(q.Get("created_from"))
	f.CreatedTo = parseTime(q.Get("created_to"))
	f.Mine = q.Get("mine") == "true"
	if q.Get("sla_risk") == "true" {
		b := true
		f.SLARisk = &b
	}
	f.AssigneeID = parseUUID(q.Get("assignee_id"))
	f.TeamID = parseUUID(q.Get("team_id"))
	if v := q.Get("overdue"); v == "true" {
		b := true
		f.Overdue = &b
	}
	if v := q.Get("open"); v == "true" {
		b := true
		f.Open = &b
	}
	// PRD P1 v2 §38–§39: filter state list yang sama dipakai untuk export
	f.DueToday = q.Get("due_today") == "true"
	f.CompletedToday = q.Get("completed_today") == "true"
	f.CompletedFrom = parseTime(q.Get("completed_from"))
	f.CompletedTo = parseTime(q.Get("completed_to"))
	f.SLAStatus = splitCSV(q.Get("sla_status"))
	f.Categories = splitCSV(q.Get("category"))
	if v := q.Get("escalated"); v != "" {
		b := v == "true"
		f.Escalated = &b
	}
	f.Q = q.Get("q")
	f.Sort = q.Get("sort")
	return f
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

func parseUUID(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func jakarta() *time.Location {
	l, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.UTC
	}
	return l
}
