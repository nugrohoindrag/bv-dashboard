// Package tenantservice: Service Request (PRD §16, AT-008, WF-004; Naming Convention §18–§19, §27).
// SR ↔ Task/Work Order bidirectional; hook: WO/Task hasil SR ditutup → SR resolved.
package tenantservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/property"
)

type Service struct {
	DB   *db.DB
	Jobs jobs.Enqueuer
	Ops  *operations.Service
}

func New(d *db.DB, j jobs.Enqueuer, ops *operations.Service) *Service {
	s := &Service{DB: d, Jobs: j, Ops: ops}
	ops.RegisterHook("work_order:*", srHook{s: s})
	ops.RegisterHook("task:*", srHook{s: s})
	return s
}

type Category struct {
	ID              uuid.UUID  `json:"id"`
	Code            string     `json:"code"`
	Name            string     `json:"name"`
	DefaultDomain   *string    `json:"default_domain"`
	DefaultPriority string     `json:"default_priority"`
	DefaultTeamID   *uuid.UUID `json:"default_team_id"`
	IsActive        bool       `json:"is_active"`
	Icon            *string    `json:"icon"`
	Profiles        []string   `json:"profiles"`
	TenantVisible   bool       `json:"tenant_visible"`
	PropertyID      *uuid.UUID `json:"property_id"`
}

// CategoryFilter: profile-aware (PRD §3.11 "service/request categories" per profile; kategori override per property).
type CategoryFilter struct {
	PropertyID *uuid.UUID
	TenantOnly bool
	IncludeAll bool // tanpa filter profile (untuk settings)
}

func (s *Service) ListCategories(ctx context.Context, f CategoryFilter) ([]Category, error) {
	var out []Category
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.ListCategoriesTx(ctx, tx, f)
		return err
	})
	if out == nil {
		out = []Category{}
	}
	return out, err
}

func (s *Service) ListCategoriesTx(ctx context.Context, tx pgx.Tx, f CategoryFilter) ([]Category, error) {
	prof := ""
	if f.PropertyID != nil && !f.IncludeAll {
		if err := tx.QueryRow(ctx, `SELECT profile FROM properties WHERE location_id = $1`, *f.PropertyID).Scan(&prof); err != nil {
			if db.IsNoRows(err) {
				return nil, apperr.NotFound("Property")
			}
			return nil, err
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT id, code, name, default_domain, default_priority, default_team_id, is_active, icon, profiles, tenant_visible, property_id
		FROM service_request_categories c
		WHERE is_active
		  AND ($1::uuid IS NULL OR property_id IS NULL OR property_id = $1)
		  AND ($2 = '' OR $2 = ANY(profiles))
		  AND (NOT $3::bool OR tenant_visible)
		  -- override per property mengalahkan kategori org dengan kode sama
		  AND NOT ($1::uuid IS NOT NULL AND property_id IS NULL AND EXISTS (SELECT 1 FROM service_request_categories o WHERE o.organization_id = c.organization_id AND o.property_id = $1 AND o.code = c.code))
		ORDER BY sort_order, name`, f.PropertyID, prof, f.TenantOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Code, &c.Name, &c.DefaultDomain, &c.DefaultPriority, &c.DefaultTeamID, &c.IsActive, &c.Icon, &c.Profiles, &c.TenantVisible, &c.PropertyID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type ServiceRequest struct {
	ID              uuid.UUID               `json:"id"`
	PropertyID      uuid.UUID               `json:"property_id"`
	RequestNumber   string                  `json:"request_number"`
	CategoryCode    string                  `json:"category_code"`
	CategoryName    *string                 `json:"category_name"`
	Title           string                  `json:"title"`
	Description     *string                 `json:"description"`
	TenantID        *uuid.UUID              `json:"tenant_id"`
	TenantName      *string                 `json:"tenant_name"`
	OccupantID      *uuid.UUID              `json:"occupant_id"`
	RequesterName   *string                 `json:"requester_name"`
	RequesterPhone  *string                 `json:"requester_phone"`
	RequesterEmail  *string                 `json:"requester_email"`
	Location        operations.LocationRef  `json:"location"`
	Priority        string                  `json:"priority"`
	Status          workflow.Status         `json:"status"`
	Channel         string                  `json:"channel"`
	Assignee        operations.AssigneeRef  `json:"assignee"`
	AcknowledgedAt  *time.Time              `json:"acknowledged_at"`
	ResolvedAt      *time.Time              `json:"resolved_at"`
	ClosedAt        *time.Time              `json:"closed_at"`
	Resolution      *string                 `json:"resolution"`
	SLA             *operations.SLAInfo     `json:"sla,omitempty"`
	SLARiskAt       *time.Time              `json:"sla_risk_at"`
	SLABreachedAt   *time.Time              `json:"sla_breached_at"`
	Flags           []string                `json:"flags"`
	Links           []operations.ObjectLink `json:"links"`
	AttachmentCount int                     `json:"attachment_count"`
	CommentCount    int                     `json:"comment_count"`
	AllowedActions  []string                `json:"allowed_actions"`
	CreatedAt       time.Time               `json:"created_at"`
	CreatedBy       *uuid.UUID              `json:"created_by"`
	CreatedByName   *string                 `json:"created_by_name"`
	Version         int                     `json:"version"`
	// P1 (PRD v1.3): pelapor Tenant App, lingkup lokasi, reopen, konfirmasi tenant, estimasi due, domain routing
	TenantUserID    *uuid.UUID `json:"tenant_user_id"`
	AreaScope       *string    `json:"area_scope"`
	ReopenCount     int        `json:"reopen_count"`
	ConfirmedAt     *time.Time `json:"confirmed_at"`
	DueEstimateAt   *time.Time `json:"due_estimate_at"`
	Domain          *string    `json:"domain"`
	MessageCount    int        `json:"message_count"`
	UnreadTenantMsg int        `json:"unread_tenant_messages"`
	Feedback        *Feedback  `json:"feedback,omitempty"`
}

// Feedback: CSAT tenant (PRD §18).
type Feedback struct {
	Rating    int       `json:"rating"`
	Comment   *string   `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateInput struct {
	PropertyID     *uuid.UUID `json:"property_id"`
	CategoryCode   string     `json:"category_code"`
	Title          string     `json:"title"`
	Description    *string    `json:"description"`
	TenantID       *uuid.UUID `json:"tenant_id"`
	OccupantID     *uuid.UUID `json:"occupant_id"`
	RequesterName  *string    `json:"requester_name"`
	RequesterPhone *string    `json:"requester_phone"`
	RequesterEmail *string    `json:"requester_email"`
	LocationID     *uuid.UUID `json:"location_id"`
	Priority       string     `json:"priority"`
	Channel        string     `json:"channel"`
	AssigneeUserID *uuid.UUID `json:"assignee_user_id"`
	AssigneeTeamID *uuid.UUID `json:"assignee_team_id"`
	// P1 — diisi jalur Mobile Tenant (tenantapp), bukan dari body staf
	TenantUserID *uuid.UUID `json:"-"`
	AreaScope    string     `json:"-"` // unit | common_area | other
	AsTenant     bool       `json:"-"` // lewati cek permission staf (kepemilikan diverifikasi tenantapp)
}

type UpdateInput struct {
	CategoryCode   *string    `json:"category_code"`
	Title          *string    `json:"title"`
	Description    *string    `json:"description"`
	TenantID       *uuid.UUID `json:"tenant_id"`
	LocationID     *uuid.UUID `json:"location_id"`
	Priority       *string    `json:"priority"`
	RequesterName  *string    `json:"requester_name"`
	RequesterPhone *string    `json:"requester_phone"`
}

var channels = map[string]bool{"staff": true, "phone": true, "walk_in": true, "public_intake": true, "email": true, "whatsapp": true, "tenant_app": true}

func (s *Service) Create(ctx context.Context, in CreateInput) (*ServiceRequest, error) {
	var out *ServiceRequest
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.CreateTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) CreateTx(ctx context.Context, tx pgx.Tx, in CreateInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return uuid.Nil, apperr.Validation("title wajib").WithField("title", "wajib")
	}
	if in.CategoryCode == "" {
		in.CategoryCode = "other"
	}
	if in.Channel == "" {
		in.Channel = "staff"
	}
	if !channels[in.Channel] {
		return uuid.Nil, apperr.Validation("channel tidak valid")
	}
	// property: dari input, lokasi, atau tenant
	var propertyID uuid.UUID
	switch {
	case in.PropertyID != nil:
		propertyID = *in.PropertyID
	case in.LocationID != nil:
		pid, err := property.ResolvePropertyOfLocation(ctx, tx, *in.LocationID)
		if err != nil {
			return uuid.Nil, err
		}
		propertyID = pid
	case in.TenantID != nil:
		if err := tx.QueryRow(ctx, `SELECT property_id FROM tenants WHERE id = $1 AND deleted_at IS NULL`, *in.TenantID).Scan(&propertyID); err != nil {
			return uuid.Nil, apperr.Validation("tenant_id tidak ditemukan")
		}
	default:
		return uuid.Nil, apperr.Validation("property_id, location_id, atau tenant_id wajib")
	}
	if !in.AsTenant && !p.HasOnProperty("tenant.service_requests.create", propertyID) {
		return uuid.Nil, apperr.Forbidden("Tidak memiliki tenant.service_requests.create pada property ini")
	}
	// kategori → default priority/team (override per property mengalahkan kategori org; PRD §11 "Property dapat mengatur category")
	var catID *uuid.UUID
	var catPrio string
	var catTeam *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id, default_priority, default_team_id FROM service_request_categories WHERE code = $1 AND is_active AND (property_id IS NULL OR property_id = $2) ORDER BY property_id NULLS LAST LIMIT 1`, in.CategoryCode, propertyID).Scan(&catID, &catPrio, &catTeam); err != nil {
		return uuid.Nil, apperr.Validation("category_code tidak dikenal")
	}
	if in.Priority == "" || in.AsTenant {
		// PRD §13: tenant tidak bebas menaikkan priority — default dari kategori/rule
		in.Priority = catPrio
	}
	if !operations.Priorities[in.Priority] {
		return uuid.Nil, apperr.Validation("priority tidak valid")
	}
	if in.AssigneeTeamID == nil && in.AssigneeUserID == nil && catTeam != nil {
		in.AssigneeTeamID = catTeam
	}
	// tenant harus di property yang sama; unit tenant → lokasi default
	if in.TenantID != nil {
		var tpid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM tenants WHERE id = $1 AND deleted_at IS NULL`, *in.TenantID).Scan(&tpid); err != nil || tpid != propertyID {
			return uuid.Nil, apperr.Validation("tenant_id tidak ditemukan di property ini")
		}
		if in.LocationID == nil {
			var lid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT location_id FROM units WHERE tenant_id = $1 ORDER BY unit_number LIMIT 1`, *in.TenantID).Scan(&lid); err == nil {
				in.LocationID = &lid
			}
		}
	}
	if in.LocationID != nil {
		var lpid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM locations WHERE id = $1 AND deleted_at IS NULL`, *in.LocationID).Scan(&lpid); err != nil || lpid != propertyID {
			return uuid.Nil, apperr.Validation("location_id tidak ditemukan di property ini")
		}
	}
	loc := property.PropertyTimezone(ctx, tx, propertyID)
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixServiceRequest, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	status := workflow.New
	if in.AssigneeUserID != nil || in.AssigneeTeamID != nil {
		status = workflow.Assigned
	}
	var id uuid.UUID
	var areaScope *string
	if in.AreaScope != "" {
		areaScope = &in.AreaScope
	}
	err = tx.QueryRow(ctx, `INSERT INTO service_requests (organization_id, property_id, request_number, category_id, category_code, title, description, tenant_id, occupant_id, requester_name, requester_phone, requester_email, location_id, priority, status, channel, assignee_user_id, assignee_team_id, acknowledged_at, created_by, updated_by, tenant_user_id, area_scope)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$20,$21,$22) RETURNING id`,
		p.OrganizationID, propertyID, number, catID, in.CategoryCode, in.Title, in.Description, in.TenantID, in.OccupantID, in.RequesterName, in.RequesterPhone, in.RequesterEmail, in.LocationID, in.Priority, status, in.Channel, in.AssigneeUserID, in.AssigneeTeamID, nilTimeIf(status == workflow.Assigned), actorOrNil(p), in.TenantUserID, areaScope).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	if in.AssigneeUserID != nil || in.AssigneeTeamID != nil {
		_, _ = tx.Exec(ctx, `INSERT INTO assignments (organization_id, object_type, object_id, assignee_user_id, assignee_team_id, assigned_by) VALUES ($1,'service_request',$2,$3,$4,$5)`, p.OrganizationID, id, in.AssigneeUserID, in.AssigneeTeamID, actorOrNil(p))
	}
	if err := s.Ops.ApplySLA(ctx, tx, operations.ObjServiceRequest, id, propertyID, in.Priority, time.Now().UTC()); err != nil {
		return uuid.Nil, err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "category": in.CategoryCode, "channel": in.Channel}})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: operations.ObjServiceRequest, EntityID: &id, EntityLabel: number})
	if s.Jobs != nil {
		payload := map[string]any{"category": in.CategoryCode, "priority": in.Priority, "domain": domainOf(ctx, tx, in.CategoryCode), "assignee_user_id": in.AssigneeUserID, "assignee_team_id": in.AssigneeTeamID, "channel": in.Channel}
		if in.TenantUserID != nil {
			payload["tenant_user_id"] = *in.TenantUserID
		}
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.ServiceRequestCreated, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: operations.ObjServiceRequest, ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p), Payload: payload})
		if in.AssigneeUserID != nil || in.AssigneeTeamID != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.ServiceRequestAssigned, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: operations.ObjServiceRequest, ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p),
				Payload: map[string]any{"assignee_user_id": in.AssigneeUserID, "assignee_team_id": in.AssigneeTeamID}})
		}
		_ = s.Jobs.EnqueueTx(ctx, tx, jobs.SearchIndexArgs{OrganizationID: p.OrganizationID, ObjectType: operations.ObjServiceRequest, ObjectID: id})
	}
	return id, nil
}

func domainOf(ctx context.Context, tx pgx.Tx, code string) string {
	var d *string
	_ = tx.QueryRow(ctx, `SELECT default_domain FROM service_request_categories WHERE code = $1`, code).Scan(&d)
	if d == nil {
		return "management"
	}
	return *d
}

func nilTimeIf(b bool) *time.Time {
	if !b {
		return nil
	}
	t := time.Now().UTC()
	return &t
}

func actorOrNil(p *authctx.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}

const srSelect = `SELECT sr.id, sr.property_id, sr.request_number, sr.category_code, c.name, sr.title, sr.description, sr.tenant_id, t.name, sr.occupant_id, sr.requester_name, sr.requester_phone, sr.requester_email,
	sr.location_id, l.name, sr.priority, sr.status, sr.channel, sr.assignee_user_id, au.full_name, sr.assignee_team_id, at.name, sr.acknowledged_at, sr.resolved_at, sr.closed_at, sr.resolution,
	sr.sla_risk_at, sr.sla_breached_at, sr.created_at, sr.created_by, cu.full_name, sr.version,
	(SELECT count(*) FROM attachments x WHERE x.object_type = 'service_request' AND x.object_id = sr.id AND x.deleted_at IS NULL),
	(SELECT count(*) FROM comments cm WHERE cm.object_type = 'service_request' AND cm.object_id = sr.id AND cm.deleted_at IS NULL),
	sr.tenant_user_id, sr.area_scope, sr.reopen_count, sr.confirmed_at, sr.due_estimate_at, c.default_domain,
	(SELECT count(*) FROM service_request_messages m WHERE m.service_request_id = sr.id),
	(SELECT count(*) FROM service_request_messages m WHERE m.service_request_id = sr.id AND m.author_kind = 'tenant' AND m.read_by_staff_at IS NULL),
	fb.rating, fb.comment, fb.created_at
	FROM service_requests sr LEFT JOIN service_request_feedback fb ON fb.service_request_id = sr.id LEFT JOIN service_request_categories c ON c.id = sr.category_id LEFT JOIN tenants t ON t.id = sr.tenant_id LEFT JOIN locations l ON l.id = sr.location_id
	LEFT JOIN users au ON au.id = sr.assignee_user_id LEFT JOIN teams at ON at.id = sr.assignee_team_id LEFT JOIN users cu ON cu.id = sr.created_by`

func scanSR(row pgx.Row) (*ServiceRequest, error) {
	var r ServiceRequest
	var fbRating *int
	var fbComment *string
	var fbAt *time.Time
	if err := row.Scan(&r.ID, &r.PropertyID, &r.RequestNumber, &r.CategoryCode, &r.CategoryName, &r.Title, &r.Description, &r.TenantID, &r.TenantName, &r.OccupantID, &r.RequesterName, &r.RequesterPhone, &r.RequesterEmail,
		&r.Location.ID, &r.Location.Name, &r.Priority, &r.Status, &r.Channel, &r.Assignee.UserID, &r.Assignee.UserName, &r.Assignee.TeamID, &r.Assignee.TeamName, &r.AcknowledgedAt, &r.ResolvedAt, &r.ClosedAt, &r.Resolution,
		&r.SLARiskAt, &r.SLABreachedAt, &r.CreatedAt, &r.CreatedBy, &r.CreatedByName, &r.Version, &r.AttachmentCount, &r.CommentCount,
		&r.TenantUserID, &r.AreaScope, &r.ReopenCount, &r.ConfirmedAt, &r.DueEstimateAt, &r.Domain, &r.MessageCount, &r.UnreadTenantMsg, &fbRating, &fbComment, &fbAt); err != nil {
		return nil, err
	}
	if fbRating != nil {
		r.Feedback = &Feedback{Rating: *fbRating, Comment: fbComment, CreatedAt: *fbAt}
	}
	r.Flags = []string{}
	if r.SLABreachedAt != nil {
		r.Flags = append(r.Flags, "sla_breach")
	} else if r.SLARiskAt != nil {
		r.Flags = append(r.Flags, "sla_risk")
	}
	return &r, nil
}

func (s *Service) getTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*ServiceRequest, error) {
	r, err := scanSR(tx.QueryRow(ctx, srSelect+` WHERE sr.id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Service Request")
		}
		return nil, err
	}
	return r, s.enrich(ctx, tx, r)
}

func (s *Service) enrich(ctx context.Context, tx pgx.Tx, r *ServiceRequest) error {
	p := authctx.Must(ctx)
	if r.Location.ID != nil {
		pt := property.LocationPathText(ctx, tx, *r.Location.ID)
		r.Location.PathText = &pt
	}
	var sla operations.SLAInfo
	var started time.Time
	if err := tx.QueryRow(ctx, `SELECT policy_id, response_due_at, resolution_due_at, responded_at, resolved_at, sla_risk_at, sla_breached_at, escalated_at, started_at FROM sla_tracking WHERE object_type = 'service_request' AND object_id = $1`, r.ID).
		Scan(&sla.PolicyID, &sla.ResponseDueAt, &sla.ResolutionDueAt, &sla.RespondedAt, &sla.ResolvedAt, &sla.RiskAt, &sla.BreachedAt, &sla.EscalatedAt, &started); err == nil {
		if sla.ResolutionDueAt != nil {
			ref := time.Now()
			if sla.ResolvedAt != nil {
				ref = *sla.ResolvedAt
			}
			if total := sla.ResolutionDueAt.Sub(started); total > 0 {
				pct := int(ref.Sub(started) * 100 / total)
				sla.ElapsedPct = &pct
			}
			rem := int(sla.ResolutionDueAt.Sub(ref).Minutes())
			sla.RemainingMinutes = &rem
		}
		r.SLA = &sla
	}
	r.Links, _ = s.Ops.ListLinksTx(ctx, tx, operations.ObjServiceRequest, r.ID)
	r.AllowedActions = []string{"view"}
	for _, tr := range workflow.ServiceRequest.ActionsFrom(r.Status) {
		if p.HasOnProperty(tr.Perm, r.PropertyID) {
			r.AllowedActions = append(r.AllowedActions, tr.Action)
		}
	}
	if !workflow.ServiceRequest.IsTerminal(r.Status) {
		if p.HasOnProperty("tenant.service_requests.update", r.PropertyID) {
			r.AllowedActions = append(r.AllowedActions, "update")
		}
		if r.Status != workflow.Resolved && p.HasOnProperty("operations.work_orders.create", r.PropertyID) {
			r.AllowedActions = append(r.AllowedActions, "create_work_order")
		}
		if r.Status != workflow.Resolved && p.HasOnProperty("operations.tasks.create", r.PropertyID) {
			r.AllowedActions = append(r.AllowedActions, "create_task")
		}
	}
	if p.HasOnProperty("operations.comments.create", r.PropertyID) {
		r.AllowedActions = append(r.AllowedActions, "comment")
	}
	if p.HasOnProperty("operations.attachments.create", r.PropertyID) {
		r.AllowedActions = append(r.AllowedActions, "attach")
	}
	return nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*ServiceRequest, error) {
	var out *ServiceRequest
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasOnProperty("tenant.service_requests.view", r.PropertyID) {
			return apperr.Forbidden("")
		}
		out = r
		return nil
	})
	return out, err
}

type Filter struct {
	PropertyID             *uuid.UUID
	Statuses               []string
	Priorities             []string
	Categories             []string
	TenantID               *uuid.UUID
	LocationID             *uuid.UUID
	AssigneeID             *uuid.UUID
	TeamID                 *uuid.UUID
	Open                   *bool
	SLARisk                *bool
	Mine                   bool
	CreatedFrom, CreatedTo *time.Time
	Q                      string
}

func (s *Service) List(ctx context.Context, f Filter, page httpx.Page) ([]ServiceRequest, *string, error) {
	p := authctx.Must(ctx)
	var out []ServiceRequest
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE 1=1"
		if f.PropertyID != nil {
			where += " AND sr.property_id = " + add(*f.PropertyID)
		} else if pids, all := p.PropertyIDsFor("tenant.service_requests.view"); !all {
			where += " AND sr.property_id = ANY(" + add(pids) + "::uuid[])"
		}
		if len(f.Statuses) > 0 {
			where += " AND sr.status = ANY(" + add(f.Statuses) + ")"
		}
		if len(f.Priorities) > 0 {
			where += " AND sr.priority = ANY(" + add(f.Priorities) + ")"
		}
		if len(f.Categories) > 0 {
			where += " AND sr.category_code = ANY(" + add(f.Categories) + ")"
		}
		if f.TenantID != nil {
			where += " AND sr.tenant_id = " + add(*f.TenantID)
		}
		if f.LocationID != nil {
			where += " AND sr.location_id IN (SELECT d.id FROM locations d JOIN locations r ON d.path <@ r.path WHERE r.id = " + add(*f.LocationID) + ")"
		}
		if f.AssigneeID != nil {
			where += " AND sr.assignee_user_id = " + add(*f.AssigneeID)
		}
		if f.TeamID != nil {
			where += " AND sr.assignee_team_id = " + add(*f.TeamID)
		}
		if f.Mine {
			where += " AND (sr.assignee_user_id = " + add(p.UserID)
			if len(p.TeamIDs) > 0 {
				where += " OR sr.assignee_team_id = ANY(" + add(p.TeamIDs) + "::uuid[])"
			}
			where += ")"
		}
		if f.Open != nil && *f.Open {
			where += " AND sr.status NOT IN ('closed','cancelled')"
		}
		if f.SLARisk != nil && *f.SLARisk {
			where += " AND sr.sla_risk_at IS NOT NULL AND sr.status NOT IN ('resolved','closed','cancelled')"
		}
		if f.CreatedFrom != nil {
			where += " AND sr.created_at >= " + add(*f.CreatedFrom)
		}
		if f.CreatedTo != nil {
			where += " AND sr.created_at <= " + add(*f.CreatedTo)
		}
		if f.Q != "" {
			q := add("%" + f.Q + "%")
			where += " AND (sr.request_number ILIKE " + q + " OR sr.title ILIKE " + q + " OR t.name ILIKE " + q + ")"
		}
		if page.Cursor != nil {
			where += " AND (sr.created_at, sr.id) < (" + add(page.Cursor.Value) + "::timestamptz, " + add(page.Cursor.ID) + ")"
		}
		rows, err := tx.Query(ctx, srSelect+where+" ORDER BY sr.created_at DESC, sr.id DESC LIMIT "+add(page.Limit+1), args...)
		if err != nil {
			return err
		}
		var items []*ServiceRequest
		for rows.Next() {
			r, err := scanSR(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, r)
		}
		rows.Close()
		hasMore := len(items) > page.Limit
		if hasMore {
			items = items[:page.Limit]
		}
		for _, r := range items {
			if err := s.enrich(ctx, tx, r); err != nil {
				return err
			}
			out = append(out, *r)
		}
		if hasMore && len(out) > 0 {
			last := out[len(out)-1]
			c := httpx.EncodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
		}
		return nil
	})
	if out == nil {
		out = []ServiceRequest{}
	}
	return out, next, err
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput, ifVersion *int) (*ServiceRequest, error) {
	p := authctx.Must(ctx)
	var out *ServiceRequest
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.HasOnProperty("tenant.service_requests.update", before.PropertyID) {
			return apperr.Forbidden("")
		}
		if ifVersion != nil && *ifVersion != before.Version {
			return apperr.StaleVersion()
		}
		if workflow.ServiceRequest.IsTerminal(before.Status) {
			return apperr.Conflict("OBJECT_TERMINAL", "Service Request sudah "+workflow.Label(before.Status))
		}
		if in.Priority != nil && !operations.Priorities[*in.Priority] {
			return apperr.Validation("priority tidak valid")
		}
		var catID *uuid.UUID
		if in.CategoryCode != nil {
			var cid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM service_request_categories WHERE code = $1 AND is_active`, *in.CategoryCode).Scan(&cid); err != nil {
				return apperr.Validation("category_code tidak dikenal")
			}
			catID = &cid
		}
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET category_id = COALESCE($2, category_id), category_code = COALESCE(NULLIF($3,''), category_code), title = COALESCE(NULLIF($4,''), title), description = COALESCE($5, description),
			tenant_id = COALESCE($6, tenant_id), location_id = COALESCE($7, location_id), priority = COALESCE(NULLIF($8,''), priority), requester_name = COALESCE($9, requester_name), requester_phone = COALESCE($10, requester_phone), updated_by = $11 WHERE id = $1`,
			id, catID, deref(in.CategoryCode), deref(in.Title), in.Description, in.TenantID, in.LocationID, deref(in.Priority), in.RequesterName, in.RequesterPhone, p.UserID); err != nil {
			return err
		}
		if in.Priority != nil && *in.Priority != before.Priority {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: id, Action: audit.ActPriorityChanged, From: before.Priority, To: *in.Priority})
			_ = s.Ops.ApplySLA(ctx, tx, operations.ObjServiceRequest, id, before.PropertyID, *in.Priority, before.CreatedAt)
			_ = audit.Log(ctx, tx, audit.AuditEntry{Action: "changed_priority", EntityType: operations.ObjServiceRequest, EntityID: &id, EntityLabel: fmt.Sprintf("%s from %s to %s", before.RequestNumber, capitalize(before.Priority), capitalize(*in.Priority))})
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: id, Action: audit.ActUpdated})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: operations.ObjServiceRequest, EntityID: &id, EntityLabel: before.RequestNumber, Before: before, After: in})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueTx(ctx, tx, jobs.SearchIndexArgs{OrganizationID: p.OrganizationID, ObjectType: operations.ObjServiceRequest, ObjectID: id})
		}
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Assign (triage → assign team/user; status → assigned).
func (s *Service) Assign(ctx context.Context, id uuid.UUID, in operations.AssignInput) (*ServiceRequest, error) {
	p := authctx.Must(ctx)
	var out *ServiceRequest
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.HasOnProperty("tenant.service_requests.assign", r.PropertyID) {
			return apperr.Forbidden("")
		}
		if workflow.ServiceRequest.IsTerminal(r.Status) || r.Status == workflow.Resolved {
			return apperr.InvalidTransition("Service Request tidak dapat di-assign dari status " + workflow.Label(r.Status))
		}
		if in.AssigneeUserID == nil && in.AssigneeTeamID == nil {
			return apperr.Validation("assignee_user_id atau assignee_team_id wajib")
		}
		_, _ = tx.Exec(ctx, `UPDATE assignments SET is_current = false, unassigned_at = now() WHERE object_type = 'service_request' AND object_id = $1 AND is_current`, id)
		if _, err := tx.Exec(ctx, `INSERT INTO assignments (organization_id, object_type, object_id, assignee_user_id, assignee_team_id, assigned_by, note) VALUES ($1,'service_request',$2,$3,$4,$5,$6)`, p.OrganizationID, id, in.AssigneeUserID, in.AssigneeTeamID, p.UserID, in.Note); err != nil {
			return err
		}
		newStatus := r.Status
		if r.Status == workflow.New || r.Status == workflow.Acknowledged {
			newStatus = workflow.Assigned
		}
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET assignee_user_id = $2, assignee_team_id = $3, status = $4, acknowledged_at = COALESCE(acknowledged_at, now()), updated_by = $5 WHERE id = $1`, id, in.AssigneeUserID, in.AssigneeTeamID, newStatus, p.UserID); err != nil {
			return err
		}
		_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET responded_at = COALESCE(responded_at, now()) WHERE object_type = 'service_request' AND object_id = $1`, id)
		pl := map[string]any{"assignee_user_id": in.AssigneeUserID, "assignee_team_id": in.AssigneeTeamID}
		if in.AssigneeUserID != nil {
			var n string
			_ = tx.QueryRow(ctx, `SELECT full_name FROM users WHERE id = $1`, *in.AssigneeUserID).Scan(&n)
			pl["assignee_name"] = n
		}
		if in.AssigneeTeamID != nil {
			var n string
			_ = tx.QueryRow(ctx, `SELECT name FROM teams WHERE id = $1`, *in.AssigneeTeamID).Scan(&n)
			pl["assignee_team_name"] = n
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: id, Action: audit.ActAssigned, Payload: pl})
		if newStatus != r.Status {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: id, Action: audit.ActStatusChanged, From: string(r.Status), To: string(newStatus)})
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: "assigned", EntityType: operations.ObjServiceRequest, EntityID: &id, EntityLabel: r.RequestNumber, After: pl})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.ServiceRequestAssigned, OrganizationID: p.OrganizationID, PropertyID: &r.PropertyID, ObjectType: operations.ObjServiceRequest, ObjectID: id, ObjectLabel: r.RequestNumber, ActorUserID: &p.UserID, Payload: pl})
		}
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

type TransitionInput struct {
	Reason     string `json:"reason"`
	Resolution string `json:"resolution"`
}

func (s *Service) Transition(ctx context.Context, id uuid.UUID, action string, in TransitionInput) (*ServiceRequest, error) {
	var out *ServiceRequest
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.TransitionTx(ctx, tx, id, action, in, false); err != nil {
			return err
		}
		var err error
		out, err = s.getTx(ctx, tx, id)
		return err
	})
	return out, err
}

// TransitionTx: transisi SR di dalam tx. asTenant=true dipakai jalur Mobile Tenant (P1): kepemilikan & aksi yang
// diizinkan sudah diverifikasi oleh tenantapp, sehingga cek permission staf dilewati (guardrail #9: reopen auditable).
func (s *Service) TransitionTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, action string, in TransitionInput, asTenant bool) error {
	p := authctx.Must(ctx)
	{
		r, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		tr, ok := workflow.ServiceRequest.Find(r.Status, action)
		if !ok {
			return apperr.InvalidTransition(fmt.Sprintf("Service Request %s cannot be %s from status %s", r.RequestNumber, action, r.Status))
		}
		if !asTenant && !p.HasOnProperty(tr.Perm, r.PropertyID) {
			return apperr.Forbidden("Memerlukan " + tr.Perm)
		}
		reason := strings.TrimSpace(in.Reason)
		if reason == "" {
			reason = strings.TrimSpace(in.Resolution)
		}
		if tr.RequireReason && reason == "" {
			return apperr.Validation("reason/resolution wajib").WithField("reason", "wajib")
		}
		if action == workflow.ActResolve {
			// SR tidak boleh resolved bila masih ada Task/WO terkait yang open (WF-004: resolution mengikuti operational work)
			var openWork int
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM object_links ol
				WHERE ((ol.from_type = 'service_request' AND ol.from_id = $1) OR (ol.to_type = 'service_request' AND ol.to_id = $1))
				AND ( (ol.to_type = 'work_order' AND EXISTS (SELECT 1 FROM work_orders w WHERE w.id = ol.to_id AND w.status NOT IN ('completed','closed','cancelled')))
				   OR (ol.from_type = 'work_order' AND EXISTS (SELECT 1 FROM work_orders w WHERE w.id = ol.from_id AND w.status NOT IN ('completed','closed','cancelled')))
				   OR (ol.to_type = 'task' AND EXISTS (SELECT 1 FROM tasks t WHERE t.id = ol.to_id AND t.status NOT IN ('completed','closed','cancelled')))
				   OR (ol.from_type = 'task' AND EXISTS (SELECT 1 FROM tasks t WHERE t.id = ol.from_id AND t.status NOT IN ('completed','closed','cancelled'))) )`, id).Scan(&openWork)
			if openWork > 0 && !p.HasOnProperty("tenant.service_requests.manage", r.PropertyID) {
				return apperr.Conflict("WORKFLOW_GUARD_FAILED", fmt.Sprintf("%d Task/Work Order terkait masih berjalan", openWork))
			}
		}
		sets := "status = $2, updated_by = $3"
		args := []any{id, tr.To, p.UserID}
		now := time.Now().UTC()
		switch action {
		case workflow.ActAcknowledge:
			sets += ", acknowledged_at = COALESCE(acknowledged_at, now())"
			_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET responded_at = COALESCE(responded_at, $2) WHERE object_type = 'service_request' AND object_id = $1`, id, now)
		case workflow.ActResolve:
			args = append(args, reason)
			sets += ", resolution = $4, resolved_at = now()"
			_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET resolved_at = $2 WHERE object_type = 'service_request' AND object_id = $1`, id, now)
		case workflow.ActClose:
			sets += ", closed_at = now()"
			if asTenant {
				sets += ", confirmed_at = now()" // PRD §17: tenant Confirm Resolved
			}
		case workflow.ActReopen:
			sets += ", resolved_at = NULL, closed_at = NULL, confirmed_at = NULL, reopen_count = reopen_count + 1"
			_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET resolved_at = NULL WHERE object_type = 'service_request' AND object_id = $1`, id)
		case workflow.ActCancel:
			sets += ", cancelled_at = now()"
		case workflow.ActWaitTenant:
			_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET paused_at = $2 WHERE object_type = 'service_request' AND object_id = $1`, id, now)
		case workflow.ActStart:
			_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET paused_minutes = paused_minutes + COALESCE(GREATEST(0, EXTRACT(EPOCH FROM ($2 - paused_at))/60)::int, 0), paused_at = NULL WHERE object_type = 'service_request' AND object_id = $1 AND paused_at IS NOT NULL`, id, now)
		}
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET `+sets+` WHERE id = $1`, args...); err != nil {
			return err
		}
		actorKind := "staff"
		if asTenant {
			actorKind = "tenant"
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: id, Action: audit.ActStatusChanged, From: string(r.Status), To: string(tr.To), Payload: map[string]any{"action": action, "reason": reason, "actor_kind": actorKind}})
		if action == workflow.ActResolve {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: id, Action: audit.ActResolved, Payload: map[string]any{"resolution": reason}})
		}
		if action == workflow.ActReopen {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: id, Action: audit.ActReopened, Payload: map[string]any{"reason": reason, "actor_kind": actorKind}})
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: operations.ObjServiceRequest, EntityID: &id, EntityLabel: r.RequestNumber, Before: map[string]any{"status": r.Status}, After: map[string]any{"status": tr.To, "reason": reason}})
		if s.Jobs != nil {
			verb := map[string]string{workflow.ActAcknowledge: "acknowledged", workflow.ActStart: "started", workflow.ActResolve: "resolved", workflow.ActClose: "closed", workflow.ActReopen: "reopened", workflow.ActCancel: "cancelled", workflow.ActWaitTenant: "waiting_for_tenant"}[action]
			payload := map[string]any{"from": r.Status, "to": tr.To, "assignee_user_id": r.Assignee.UserID, "assignee_team_id": r.Assignee.TeamID, "requester_user_id": r.CreatedBy, "actor_kind": actorKind, "domain": deref(r.Domain)}
			if r.TenantUserID != nil {
				payload["tenant_user_id"] = *r.TenantUserID
			}
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "service_request." + verb, OrganizationID: p.OrganizationID, PropertyID: &r.PropertyID, ObjectType: operations.ObjServiceRequest, ObjectID: id, ObjectLabel: r.RequestNumber, ActorUserID: &p.UserID, Payload: payload})
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.ServiceRequestStatusChanged, OrganizationID: p.OrganizationID, PropertyID: &r.PropertyID, ObjectType: operations.ObjServiceRequest, ObjectID: id, ObjectLabel: r.RequestNumber, ActorUserID: &p.UserID, Payload: payload})
			_ = s.Jobs.EnqueueTx(ctx, tx, jobs.SearchIndexArgs{OrganizationID: p.OrganizationID, ObjectType: operations.ObjServiceRequest, ObjectID: id})
		}
	}
	return nil
}

// CreateWorkOrderFromSR (AT-008): WO ter-link generated_from SR; SR → in_progress.
func (s *Service) CreateWorkOrderFromSR(ctx context.Context, srID uuid.UUID, in operations.CreateWorkOrderInput) (*operations.WorkItem, error) {
	var out *operations.WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getTx(ctx, tx, srID)
		if err != nil {
			return err
		}
		if workflow.ServiceRequest.IsTerminal(r.Status) {
			return apperr.Conflict("OBJECT_TERMINAL", "Service Request sudah "+workflow.Label(r.Status))
		}
		if in.WorkOrderType == "" {
			in.WorkOrderType = "service"
		}
		if in.Title == "" {
			in.Title = r.Title
		}
		if in.Description == nil {
			in.Description = r.Description
		}
		if in.LocationID == nil {
			in.LocationID = r.Location.ID
		}
		if in.Priority == "" {
			in.Priority = r.Priority
		}
		if in.AssigneeUserID == nil && in.AssigneeTeamID == nil {
			in.AssigneeUserID, in.AssigneeTeamID = r.Assignee.UserID, r.Assignee.TeamID
		}
		in.PropertyID = &r.PropertyID
		in.RequesterUserID = r.CreatedBy
		in.SourceType = strPtr(operations.ObjServiceRequest)
		in.SourceID = &srID
		in.LinkTo = &operations.LinkRef{ObjectType: operations.ObjServiceRequest, ObjectID: srID, LinkType: "generated_from"}
		id, err := s.Ops.CreateWorkOrderTx(ctx, tx, in)
		if err != nil {
			return err
		}
		s.markInProgress(ctx, tx, r, "work_order", id)
		out, err = s.Ops.GetTx(ctx, tx, operations.ObjWorkOrder, id)
		return err
	})
	return out, err
}

func (s *Service) CreateTaskFromSR(ctx context.Context, srID uuid.UUID, in operations.CreateTaskInput) (*operations.WorkItem, error) {
	var out *operations.WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getTx(ctx, tx, srID)
		if err != nil {
			return err
		}
		if workflow.ServiceRequest.IsTerminal(r.Status) {
			return apperr.Conflict("OBJECT_TERMINAL", "Service Request sudah "+workflow.Label(r.Status))
		}
		if in.Title == "" {
			in.Title = r.Title
		}
		if in.LocationID == nil {
			in.LocationID = r.Location.ID
		}
		if in.Priority == "" {
			in.Priority = r.Priority
		}
		if in.AssigneeUserID == nil && in.AssigneeTeamID == nil {
			in.AssigneeUserID, in.AssigneeTeamID = r.Assignee.UserID, r.Assignee.TeamID
		}
		in.PropertyID = &r.PropertyID
		in.SourceType = strPtr(operations.ObjServiceRequest)
		in.SourceID = &srID
		in.LinkTo = &operations.LinkRef{ObjectType: operations.ObjServiceRequest, ObjectID: srID, LinkType: "generated_from"}
		id, err := s.Ops.CreateTaskTx(ctx, tx, in)
		if err != nil {
			return err
		}
		s.markInProgress(ctx, tx, r, "task", id)
		out, err = s.Ops.GetTx(ctx, tx, operations.ObjTask, id)
		return err
	})
	return out, err
}

func (s *Service) markInProgress(ctx context.Context, tx pgx.Tx, r *ServiceRequest, objType string, objID uuid.UUID) {
	if r.Status == workflow.New || r.Status == workflow.Acknowledged || r.Status == workflow.Assigned || r.Status == workflow.WaitingForTenant {
		_, _ = tx.Exec(ctx, `UPDATE service_requests SET status = 'in_progress', acknowledged_at = COALESCE(acknowledged_at, now()) WHERE id = $1`, r.ID)
		_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET responded_at = COALESCE(responded_at, now()) WHERE object_type = 'service_request' AND object_id = $1`, r.ID)
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: r.ID, Action: audit.ActStatusChanged, From: string(r.Status), To: "in_progress", Payload: map[string]any{objType + "_id": objID}})
	}
}

// srHook: WO/Task hasil SR → Completed/Closed ⇒ SR resolved (WF-004 "Resolution → Service Request Resolved").
type srHook struct{ s *Service }

func (h srHook) BeforeComplete(context.Context, pgx.Tx, *operations.WorkItem, operations.TransitionInput) error {
	return nil
}
func (h srHook) AfterTransition(ctx context.Context, tx pgx.Tx, item *operations.WorkItem, action string, from, to workflow.Status) error {
	if item.SourceType == nil || *item.SourceType != operations.ObjServiceRequest || item.SourceID == nil {
		return nil
	}
	if action != workflow.ActClose && action != workflow.ActComplete {
		return nil
	}
	// resolved bila semua work terkait selesai
	var openWork int
	_ = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM work_orders w WHERE w.source_type = 'service_request' AND w.source_id = $1 AND w.status NOT IN ('completed','closed','cancelled'))
		+ (SELECT count(*) FROM tasks t WHERE t.source_type = 'service_request' AND t.source_id = $1 AND t.status NOT IN ('completed','closed','cancelled'))`, *item.SourceID).Scan(&openWork)
	if openWork > 0 {
		return nil
	}
	var status string
	var number, prop string
	if err := tx.QueryRow(ctx, `SELECT status, request_number, property_id::text FROM service_requests WHERE id = $1`, *item.SourceID).Scan(&status, &number, &prop); err != nil {
		return nil
	}
	if status == "resolved" || status == "closed" || status == "cancelled" {
		return nil
	}
	res := "Diselesaikan melalui " + item.Number
	if item.Resolution != nil && *item.Resolution != "" {
		res = *item.Resolution
	}
	if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'resolved', resolution = $2, resolved_at = now() WHERE id = $1`, *item.SourceID, res); err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET resolved_at = now() WHERE object_type = 'service_request' AND object_id = $1`, *item.SourceID)
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: *item.SourceID, Action: audit.ActStatusChanged, From: status, To: "resolved", Payload: map[string]any{"via": item.Number}})
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: operations.ObjServiceRequest, ObjectID: *item.SourceID, Action: audit.ActResolved, Payload: map[string]any{"resolution": res, "via": item.Number}})
	if h.s.Jobs != nil {
		p := authctx.Must(ctx)
		pid, _ := uuid.Parse(prop)
		_ = h.s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.ServiceRequestResolved, OrganizationID: p.OrganizationID, PropertyID: &pid, ObjectType: operations.ObjServiceRequest, ObjectID: *item.SourceID, ObjectLabel: number, ActorUserID: actorOrNil(p), Payload: map[string]any{"via": item.Number}})
		_ = h.s.Jobs.EnqueueTx(ctx, tx, jobs.SearchIndexArgs{OrganizationID: p.OrganizationID, ObjectType: operations.ObjServiceRequest, ObjectID: *item.SourceID})
	}
	return nil
}

func strPtr(s string) *string { return &s }
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
