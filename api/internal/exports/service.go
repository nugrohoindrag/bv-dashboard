// Package exports: CSV/XLSX export (PRD §23 Should; TAD §5.17) — job → object storage → notification dengan signed URL.
package exports

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
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
	"github.com/buildingvision/api/internal/tenantservice"
)

type Service struct {
	DB          *db.DB
	Jobs        jobs.Enqueuer
	Storage     storage.Storage
	IAM         *iam.Service
	Ops         *operations.Service
	Assets      *asset.Service
	SR          *tenantservice.Service
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

var resources = map[string]bool{"tasks": true, "work_orders": true, "service_requests": true, "incidents": true, "assets": true, "findings": true}

// Request: buat export request → job (audit: export selalu dicatat).
func (s *Service) Request(ctx context.Context, resource, format string, filters map[string]string) (*Export, error) {
	p := authctx.Must(ctx)
	if !resources[resource] {
		return nil, apperr.Validation("resource harus tasks|work_orders|service_requests|incidents|assets|findings")
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
	switch resource {
	case "tasks", "work_orders":
		ot := operations.ObjTask
		if resource == "work_orders" {
			ot = operations.ObjWorkOrder
		}
		f := listFilterFromQuery(q)
		headers := []string{"Number", "Type", "Title", "Status", "Priority", "Location", "Asset", "Assignee", "Team", "Due", "Overdue", "SLA Risk", "Created", "Completed", "Closed"}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			items, next, err := s.Ops.List(ctx, ot, f, page)
			if err != nil {
				return nil, nil, err
			}
			for _, w := range items {
				rows = append(rows, []any{w.Number, w.Type, w.Title, string(w.Status), w.Priority, deref(w.Location.PathText), deref(w.Asset.AssetCode), deref(w.Assignee.UserName), deref(w.Assignee.TeamName), fmtTime(w.DueAt), w.IsOverdue, w.SLARiskAt != nil, fmtTime(&w.CreatedAt), fmtTime(w.CompletedAt), fmtTime(w.ClosedAt)})
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
		headers := []string{"Number", "Category", "Title", "Tenant", "Status", "Priority", "Location", "Assignee", "Team", "Created", "Resolved", "Closed"}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			items, next, err := s.SR.List(ctx, f, page)
			if err != nil {
				return nil, nil, err
			}
			for _, r := range items {
				rows = append(rows, []any{r.RequestNumber, r.CategoryCode, r.Title, deref(r.TenantName), string(r.Status), r.Priority, deref(r.Location.PathText), deref(r.Assignee.UserName), deref(r.Assignee.TeamName), fmtTime(&r.CreatedAt), fmtTime(r.ResolvedAt), fmtTime(r.ClosedAt)})
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
		headers := []string{"Number", "Category", "Title", "Severity", "Priority", "Status", "Location", "Reported By", "Reported At", "Resolved", "Closed"}
		var rows [][]any
		page := httpx.Page{Limit: 200}
		for {
			items, next, err := s.Ops.ListIncidents(ctx, f, page)
			if err != nil {
				return nil, nil, err
			}
			for _, i := range items {
				rows = append(rows, []any{i.IncidentNumber, i.Category, i.Title, i.Severity, i.Priority, string(i.Status), deref(i.Location.PathText), deref(i.ReportedByName), fmtTime(&i.ReportedAt), fmtTime(i.ResolvedAt), fmtTime(i.ClosedAt)})
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
	}
	return nil, nil, fmt.Errorf("resource tidak dikenal")
}

func listFilterFromQuery(q url.Values) operations.ListFilter {
	var f operations.ListFilter
	f.PropertyID = parseUUID(q.Get("property_id"))
	f.Types = splitCSV(q.Get("type"))
	f.Statuses = splitCSV(q.Get("status"))
	f.Priorities = splitCSV(q.Get("priority"))
	f.LocationID = parseUUID(q.Get("location_id"))
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
	f.Q = q.Get("q")
	f.Sort = q.Get("sort")
	return f
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
