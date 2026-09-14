package operations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Finding (PRD §12.4, §13.2, §15.2; Naming Convention §8) ----------

type Finding struct {
	ID              uuid.UUID       `json:"id"`
	PropertyID      uuid.UUID       `json:"property_id"`
	FindingNumber   string          `json:"finding_number"`
	FindingType     string          `json:"finding_type"`
	Category        *string         `json:"category"`
	Title           string          `json:"title"`
	Description     *string         `json:"description"`
	Location        LocationRef     `json:"location"`
	Asset           AssetRef        `json:"asset"`
	Severity        string          `json:"severity"`
	Status          workflow.Status `json:"status"`
	SourceType      *string         `json:"source_type"`
	SourceID        *uuid.UUID      `json:"source_id"`
	SourceLabel     string          `json:"source_label"`
	ReportedBy      *uuid.UUID      `json:"reported_by"`
	ReportedByName  *string         `json:"reported_by_name"`
	ReportedAt      time.Time       `json:"reported_at"`
	Resolution      *string         `json:"resolution"`
	ResolvedAt      *time.Time      `json:"resolved_at"`
	ClosedAt        *time.Time      `json:"closed_at"`
	EscalatedAt     *time.Time      `json:"escalated_at"`
	Links           []ObjectLink    `json:"links"`
	AttachmentCount int             `json:"attachment_count"`
	AllowedActions  []string        `json:"allowed_actions"`
	CreatedAt       time.Time       `json:"created_at"`
	Version         int             `json:"version"`
}

type CreateFindingInput struct {
	PropertyID       *uuid.UUID `json:"property_id"`
	FindingType      string     `json:"finding_type"`
	Category         *string    `json:"category"`
	Title            string     `json:"title"`
	Description      *string    `json:"description"`
	LocationID       *uuid.UUID `json:"location_id"`
	AssetID          *uuid.UUID `json:"asset_id"`
	Severity         string     `json:"severity"`
	SourceType       *string    `json:"source_type"`
	SourceID         *uuid.UUID `json:"source_id"`
	AttachmentID     *uuid.UUID `json:"attachment_id"` // foto yang sudah di-presign untuk object sumber → di-relink ke finding
	ClientRecordedAt *time.Time `json:"client_recorded_at"`
	FromSync         bool       `json:"-"`
}

var findingTypes = map[string]bool{"general": true, "patrol": true, "inspection": true, "housekeeping": true, "checklist": true}

func (s *Service) CreateFinding(ctx context.Context, in CreateFindingInput) (*Finding, error) {
	var out *Finding
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.CreateFindingTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = s.getFindingTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) CreateFindingTx(ctx context.Context, tx pgx.Tx, in CreateFindingInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return uuid.Nil, apperr.Validation("title wajib")
	}
	if in.FindingType == "" {
		in.FindingType = "general"
	}
	if !findingTypes[in.FindingType] {
		return uuid.Nil, apperr.Validation("finding_type tidak valid")
	}
	if in.Severity == "" {
		in.Severity = "medium"
	}
	if !Priorities[in.Severity] {
		return uuid.Nil, apperr.Validation("severity tidak valid")
	}
	propertyID, err := s.resolveProperty(ctx, tx, in.PropertyID, in.LocationID, in.AssetID)
	if err != nil {
		// coba dari source
		if in.SourceType != nil && in.SourceID != nil {
			if t, e := tableFor(*in.SourceType); e == nil {
				if e2 := tx.QueryRow(ctx, `SELECT property_id FROM `+t.table+` WHERE id = $1`, *in.SourceID).Scan(&propertyID); e2 == nil {
					err = nil
				}
			}
		}
		if err != nil {
			return uuid.Nil, err
		}
	}
	if !p.HasOnProperty("operations.findings.create", propertyID) {
		return uuid.Nil, apperr.Forbidden("Tidak memiliki operations.findings.create pada property ini")
	}
	if err := s.validateRefs(ctx, tx, propertyID, in.LocationID, in.AssetID, nil, nil, nil); err != nil {
		return uuid.Nil, err
	}
	loc := property.PropertyTimezone(ctx, tx, propertyID)
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixFinding, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO findings (organization_id, property_id, finding_number, finding_type, category, title, description, location_id, asset_id, severity, source_type, source_id, reported_by, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13,$13) RETURNING id`,
		p.OrganizationID, propertyID, number, in.FindingType, in.Category, in.Title, in.Description, in.LocationID, in.AssetID, in.Severity, in.SourceType, in.SourceID, actorOrNil(p)).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	if in.AttachmentID != nil {
		// relink foto ke finding (foto tetap terlihat di object sumber lewat activity)
		_, _ = tx.Exec(ctx, `UPDATE attachments SET object_type = 'finding', object_id = $2 WHERE id = $1`, *in.AttachmentID, id)
	}
	if in.SourceType != nil && in.SourceID != nil && isLinkable(*in.SourceType) {
		if _, err := s.linkTx(ctx, tx, ObjFinding, id, *in.SourceType, *in.SourceID, "generated_from"); err != nil {
			return uuid.Nil, err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: *in.SourceType, ObjectID: *in.SourceID, Action: audit.ActFindingCreated, Payload: map[string]any{"finding_id": id, "finding_number": number, "severity": in.Severity, "title": in.Title}, ClientRecordedAt: in.ClientRecordedAt})
	}
	ctxAct := ctx
	if in.FromSync {
		ctxAct = authctx.With(ctx, withSource(p, authctx.SourceSync))
	}
	_ = audit.Record(ctxAct, tx, audit.Entry{ObjectType: ObjFinding, ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "severity": in.Severity}, ClientRecordedAt: in.ClientRecordedAt})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: ObjFinding, EntityID: &id, EntityLabel: number})
	if s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.FindingCreated, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: ObjFinding, ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p),
			Payload: map[string]any{"severity": in.Severity, "finding_type": in.FindingType, "source_type": in.SourceType, "source_id": in.SourceID}})
		if in.Severity == "critical" || in.Severity == "high" {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.FindingEscalated, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: ObjFinding, ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p), Payload: map[string]any{"severity": in.Severity}})
			_, _ = tx.Exec(ctx, `UPDATE findings SET escalated_at = now() WHERE id = $1`, id)
		}
	}
	return id, nil
}

func (s *Service) getFindingTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Finding, error) {
	var f Finding
	err := tx.QueryRow(ctx, `SELECT f.id, f.property_id, f.finding_number, f.finding_type, f.category, f.title, f.description, f.location_id, l.name, f.asset_id, a.asset_code, a.name, a.status,
		f.severity, f.status, f.source_type, f.source_id, f.reported_by, u.full_name, f.reported_at, f.resolution, f.resolved_at, f.closed_at, f.escalated_at, f.created_at, f.version,
		(SELECT count(*) FROM attachments x WHERE x.object_type = 'finding' AND x.object_id = f.id AND x.deleted_at IS NULL)
		FROM findings f LEFT JOIN locations l ON l.id = f.location_id LEFT JOIN assets a ON a.id = f.asset_id LEFT JOIN users u ON u.id = f.reported_by WHERE f.id = $1`, id).
		Scan(&f.ID, &f.PropertyID, &f.FindingNumber, &f.FindingType, &f.Category, &f.Title, &f.Description, &f.Location.ID, &f.Location.Name, &f.Asset.ID, &f.Asset.AssetCode, &f.Asset.Name, &f.Asset.Status,
			&f.Severity, &f.Status, &f.SourceType, &f.SourceID, &f.ReportedBy, &f.ReportedByName, &f.ReportedAt, &f.Resolution, &f.ResolvedAt, &f.ClosedAt, &f.EscalatedAt, &f.CreatedAt, &f.Version, &f.AttachmentCount)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Finding")
		}
		return nil, err
	}
	if f.Location.ID != nil {
		pt := property.LocationPathText(ctx, tx, *f.Location.ID)
		f.Location.PathText = &pt
	}
	if f.SourceType != nil && f.SourceID != nil {
		f.SourceLabel, _, _ = describeObject(ctx, tx, *f.SourceType, *f.SourceID)
	}
	f.Links, _ = s.listLinksTx(ctx, tx, ObjFinding, id)
	p := authctx.Must(ctx)
	f.AllowedActions = []string{"view"}
	for _, tr := range workflow.Finding.ActionsFrom(f.Status) {
		if p.HasOnProperty(tr.Perm, f.PropertyID) {
			f.AllowedActions = append(f.AllowedActions, tr.Action)
		}
	}
	if f.Status != workflow.Closed && p.HasOnProperty("operations.work_orders.create", f.PropertyID) {
		f.AllowedActions = append(f.AllowedActions, "create_work_order")
	}
	if f.Status != workflow.Closed && p.HasOnProperty("operations.incidents.create", f.PropertyID) {
		f.AllowedActions = append(f.AllowedActions, "create_incident")
	}
	return &f, nil
}

func (s *Service) GetFinding(ctx context.Context, id uuid.UUID) (*Finding, error) {
	var out *Finding
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFindingTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasOnProperty("operations.findings.view", f.PropertyID) {
			return apperr.Forbidden("")
		}
		out = f
		return nil
	})
	return out, err
}

type FindingFilter struct {
	PropertyID *uuid.UUID
	Statuses   []string
	Severities []string
	Types      []string
	LocationID *uuid.UUID
	SourceType string
	SourceID   *uuid.UUID
	Unresolved bool
	Q          string
}

func (s *Service) ListFindings(ctx context.Context, f FindingFilter, page httpx.Page) ([]Finding, *string, error) {
	p := authctx.Must(ctx)
	var out []Finding
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE 1=1"
		if f.PropertyID != nil {
			where += " AND f.property_id = " + add(*f.PropertyID)
		} else if pids, all := p.PropertyIDsFor("operations.findings.view"); !all {
			where += " AND f.property_id = ANY(" + add(pids) + "::uuid[])"
		}
		if len(f.Statuses) > 0 {
			where += " AND f.status = ANY(" + add(f.Statuses) + ")"
		}
		if len(f.Severities) > 0 {
			where += " AND f.severity = ANY(" + add(f.Severities) + ")"
		}
		if len(f.Types) > 0 {
			where += " AND f.finding_type = ANY(" + add(f.Types) + ")"
		}
		if f.LocationID != nil {
			where += " AND f.location_id IN (SELECT d.id FROM locations d JOIN locations r ON d.path <@ r.path WHERE r.id = " + add(*f.LocationID) + ")"
		}
		if f.SourceType != "" {
			where += " AND f.source_type = " + add(f.SourceType)
		}
		if f.SourceID != nil {
			where += " AND f.source_id = " + add(*f.SourceID)
		}
		if f.Unresolved {
			where += " AND f.status IN ('open','in_progress')"
		}
		if f.Q != "" {
			q := add("%" + f.Q + "%")
			where += " AND (f.finding_number ILIKE " + q + " OR f.title ILIKE " + q + ")"
		}
		if page.Cursor != nil {
			where += " AND (f.reported_at, f.id) < (" + add(page.Cursor.Value) + "::timestamptz, " + add(page.Cursor.ID) + ")"
		}
		rows, err := tx.Query(ctx, `SELECT f.id FROM findings f`+where+` ORDER BY f.reported_at DESC, f.id DESC LIMIT `+add(page.Limit+1), args...)
		if err != nil {
			return err
		}
		var idList []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			idList = append(idList, id)
		}
		rows.Close()
		hasMore := len(idList) > page.Limit
		if hasMore {
			idList = idList[:page.Limit]
		}
		for _, id := range idList {
			fd, err := s.getFindingTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *fd)
		}
		if hasMore && len(out) > 0 {
			last := out[len(out)-1]
			c := httpx.EncodeCursor(last.ReportedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
		}
		return nil
	})
	if out == nil {
		out = []Finding{}
	}
	return out, next, err
}

type FindingTransitionInput struct {
	Reason     string `json:"reason"`
	Resolution string `json:"resolution"`
}

func (s *Service) TransitionFinding(ctx context.Context, id uuid.UUID, action string, in FindingTransitionInput) (*Finding, error) {
	p := authctx.Must(ctx)
	var out *Finding
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFindingTx(ctx, tx, id)
		if err != nil {
			return err
		}
		tr, ok := workflow.Finding.Find(f.Status, action)
		if !ok {
			return apperr.InvalidTransition(fmt.Sprintf("Finding %s cannot be %s from status %s", f.FindingNumber, action, f.Status))
		}
		if !p.HasOnProperty(tr.Perm, f.PropertyID) {
			return apperr.Forbidden("Memerlukan " + tr.Perm)
		}
		reason := strings.TrimSpace(in.Reason)
		if reason == "" {
			reason = strings.TrimSpace(in.Resolution)
		}
		if tr.RequireReason && reason == "" {
			return apperr.Validation("reason/resolution wajib").WithField("reason", "wajib")
		}
		sets := "status = $2, updated_by = $3"
		args := []any{id, tr.To, p.UserID}
		switch action {
		case workflow.ActResolve:
			args = append(args, reason, p.UserID)
			sets += ", resolution = $4, resolved_at = now(), resolved_by = $5"
		case workflow.ActClose:
			sets += ", closed_at = now()"
		case workflow.ActReopen:
			sets += ", resolved_at = NULL, closed_at = NULL"
		}
		if _, err := tx.Exec(ctx, `UPDATE findings SET `+sets+` WHERE id = $1`, args...); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjFinding, ObjectID: id, Action: audit.ActStatusChanged, From: string(f.Status), To: string(tr.To), Payload: map[string]any{"action": action, "reason": reason}})
		if action == workflow.ActResolve {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjFinding, ObjectID: id, Action: audit.ActResolved, Payload: map[string]any{"resolution": reason}})
			// propagate ke source (timeline)
			if f.SourceType != nil && f.SourceID != nil {
				_ = audit.Record(ctx, tx, audit.Entry{ObjectType: *f.SourceType, ObjectID: *f.SourceID, Action: audit.ActResolved, Payload: map[string]any{"finding_number": f.FindingNumber, "resolution": reason}})
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: ObjFinding, EntityID: &id, EntityLabel: f.FindingNumber, Before: map[string]any{"status": f.Status}, After: map[string]any{"status": tr.To}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "finding." + eventVerb(action), OrganizationID: p.OrganizationID, PropertyID: &f.PropertyID, ObjectType: ObjFinding, ObjectID: id, ObjectLabel: f.FindingNumber, ActorUserID: &p.UserID, Payload: map[string]any{"from": f.Status, "to": tr.To}})
		}
		out, err = s.getFindingTx(ctx, tx, id)
		return err
	})
	return out, err
}

// CreateWorkOrderFromFinding (Release Criteria #13): WO corrective ter-link generated_from finding; finding → in_progress.
func (s *Service) CreateWorkOrderFromFinding(ctx context.Context, findingID uuid.UUID, in CreateWorkOrderInput) (*WorkItem, error) {
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFindingTx(ctx, tx, findingID)
		if err != nil {
			return err
		}
		if f.Status == workflow.Closed {
			return apperr.Conflict("OBJECT_TERMINAL", "Finding sudah ditutup")
		}
		if in.WorkOrderType == "" {
			in.WorkOrderType = "corrective"
		}
		if in.Title == "" {
			in.Title = f.Title
		}
		if in.Description == nil {
			in.Description = f.Description
		}
		if in.LocationID == nil {
			in.LocationID = f.Location.ID
		}
		if in.AssetID == nil {
			in.AssetID = f.Asset.ID
		}
		if in.Priority == "" {
			in.Priority = f.Severity
		}
		in.PropertyID = &f.PropertyID
		in.SourceType = strPtr(ObjFinding)
		in.SourceID = &findingID
		in.LinkTo = &LinkRef{ObjectType: ObjFinding, ObjectID: findingID, LinkType: "generated_from"}
		id, err := s.CreateWorkOrderTx(ctx, tx, in)
		if err != nil {
			return err
		}
		if f.Status == workflow.Open {
			_, _ = tx.Exec(ctx, `UPDATE findings SET status = 'in_progress' WHERE id = $1`, findingID)
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjFinding, ObjectID: findingID, Action: audit.ActStatusChanged, From: "open", To: "in_progress", Payload: map[string]any{"work_order_id": id}})
		}
		w, t, err := s.loadTx(ctx, tx, ObjWorkOrder, id, false)
		if err != nil {
			return err
		}
		out = w
		return s.enrich(ctx, tx, w, t, true)
	})
	return out, err
}

// CreateTaskFromFinding: rework task (PRD §15.2 Rework).
func (s *Service) CreateTaskFromFinding(ctx context.Context, findingID uuid.UUID, in CreateTaskInput) (*WorkItem, error) {
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFindingTx(ctx, tx, findingID)
		if err != nil {
			return err
		}
		if in.Title == "" {
			in.Title = "Rework: " + f.Title
		}
		if in.LocationID == nil {
			in.LocationID = f.Location.ID
		}
		if in.Priority == "" {
			in.Priority = f.Severity
		}
		if in.TaskType == "" {
			if f.FindingType == "housekeeping" {
				in.TaskType = "cleaning"
			} else {
				in.TaskType = "general"
			}
		}
		in.PropertyID = &f.PropertyID
		in.SourceType = strPtr(ObjFinding)
		in.SourceID = &findingID
		in.LinkTo = &LinkRef{ObjectType: ObjFinding, ObjectID: findingID, LinkType: "rework_of"}
		id, err := s.CreateTaskTx(ctx, tx, in)
		if err != nil {
			return err
		}
		if f.Status == workflow.Open {
			_, _ = tx.Exec(ctx, `UPDATE findings SET status = 'in_progress' WHERE id = $1`, findingID)
		}
		w, t, err := s.loadTx(ctx, tx, ObjTask, id, false)
		if err != nil {
			return err
		}
		out = w
		return s.enrich(ctx, tx, w, t, true)
	})
	return out, err
}

// FindingResolutionHook: saat WO/Task hasil finding ditutup → finding resolved otomatis (P2 Finding/Resolution).
type findingHook struct{ s *Service }

func (h findingHook) BeforeComplete(context.Context, pgx.Tx, *WorkItem, TransitionInput) error {
	return nil
}
func (h findingHook) AfterTransition(ctx context.Context, tx pgx.Tx, item *WorkItem, action string, from, to workflow.Status) error {
	if action != workflow.ActClose || item.SourceType == nil || *item.SourceType != ObjFinding || item.SourceID == nil {
		return nil
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM findings WHERE id = $1`, *item.SourceID).Scan(&status); err != nil {
		return nil
	}
	if status == "open" || status == "in_progress" {
		res := "Diselesaikan melalui " + item.Number
		_, _ = tx.Exec(ctx, `UPDATE findings SET status = 'resolved', resolution = $2, resolved_at = now() WHERE id = $1`, *item.SourceID, res)
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjFinding, ObjectID: *item.SourceID, Action: audit.ActResolved, Payload: map[string]any{"via": item.Number}})
	}
	return nil
}

// FindingHook diekspor untuk registrasi di app.
func FindingHook(s *Service) ExecutionHook { return findingHook{s: s} }
