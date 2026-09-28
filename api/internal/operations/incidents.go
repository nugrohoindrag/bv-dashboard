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

// ---------- Incident (PRD §14; Naming Convention §14, §21, §29) ----------

var IncidentCategories = []string{"unauthorized_access", "suspicious_activity", "property_damage", "lost_property", "fire_smoke", "emergency", "safety", "other"}

// IncidentTypes (CHECK incidents.incident_type): security | safety | building.
var IncidentTypes = map[string]bool{"security": true, "safety": true, "building": true}

// IncidentTypeForCategory: tipe default dari kategori (dipakai bila klien hanya mengirim kategori).
func IncidentTypeForCategory(category string) string {
	switch category {
	case "fire_smoke", "fire", "emergency", "safety", "medical":
		return "safety"
	case "property_damage", "building":
		return "building"
	}
	return "security"
}

type Incident struct {
	ID             uuid.UUID       `json:"id"`
	PropertyID     uuid.UUID       `json:"property_id"`
	IncidentNumber string          `json:"incident_number"`
	IncidentType   string          `json:"incident_type"`
	Category       string          `json:"category"`
	Title          string          `json:"title"`
	Description    *string         `json:"description"`
	Location       LocationRef     `json:"location"`
	Severity       string          `json:"severity"`
	Priority       string          `json:"priority"`
	Status         workflow.Status `json:"status"`
	ReportedBy     *uuid.UUID      `json:"reported_by"`
	ReportedByName *string         `json:"reported_by_name"`
	ReportedAt     time.Time       `json:"reported_at"`
	OccurredAt     *time.Time      `json:"occurred_at"`
	Assignee       AssigneeRef     `json:"assignee"`
	Resolution     *string         `json:"resolution"`
	ActionTaken    *string         `json:"action_taken"` // PRD P1 v2 §33
	SLAStatus      string          `json:"sla_status"`   // on_track | at_risk | breached | completed
	SLA            *SLAInfo        `json:"sla,omitempty"`
	ResolvedAt     *time.Time      `json:"resolved_at"`
	ClosedAt       *time.Time      `json:"closed_at"`
	SLARiskAt      *time.Time      `json:"sla_risk_at"`
	SLABreachedAt  *time.Time      `json:"sla_breached_at"`
	SourceType     *string         `json:"source_type"`
	SourceID       *uuid.UUID      `json:"source_id"`
	// PRD P1 v2.1 P1-XMW-01: SR asal rantai (bila incident lahir dari pekerjaan permintaan tenant)
	OriginServiceRequestID *uuid.UUID `json:"origin_service_request_id"`
	// PRD P2 v2.1 §6.3: eskalasi, investigasi, people involved, video evidence
	EscalatedAt     *time.Time             `json:"escalated_at"`
	EscalationLevel int                    `json:"escalation_level"`
	Investigation   *IncidentInvestigation `json:"investigation,omitempty"`
	PeopleCount     int                    `json:"people_count"`
	VideoCount      int                    `json:"video_count"`
	Links           []ObjectLink           `json:"links"`
	AttachmentCount int                    `json:"attachment_count"`
	CommentCount    int                    `json:"comment_count"`
	AllowedActions  []string               `json:"allowed_actions"`
	Flags           []string               `json:"flags"`
	CreatedAt       time.Time              `json:"created_at"`
	Version         int                    `json:"version"`
}

type CreateIncidentInput struct {
	PropertyID       *uuid.UUID `json:"property_id"`
	IncidentType     string     `json:"incident_type"`
	Category         string     `json:"category"`
	Title            string     `json:"title"`
	Description      *string    `json:"description"`
	LocationID       *uuid.UUID `json:"location_id"`
	Severity         string     `json:"severity"`
	Priority         string     `json:"priority"`
	OccurredAt       *time.Time `json:"occurred_at"`
	AssigneeUserID   *uuid.UUID `json:"assignee_user_id"`
	AssigneeTeamID   *uuid.UUID `json:"assignee_team_id"`
	SourceType       *string    `json:"source_type"`
	SourceID         *uuid.UUID `json:"source_id"`
	ActionTaken      *string    `json:"action_taken"`
	ClientRecordedAt *time.Time `json:"client_recorded_at"`
	FromSync         bool       `json:"-"`
	// SuppressCritical: incident otomatis dari Emergency Alert — notifikasi kritis sudah dikirim lewat emergency_alert.raised
	SuppressCritical bool `json:"-"`
}

type UpdateIncidentInput struct {
	Category    *string    `json:"category"`
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	LocationID  *uuid.UUID `json:"location_id"`
	Severity    *string    `json:"severity"`
	Priority    *string    `json:"priority"`
	OccurredAt  *time.Time `json:"occurred_at"`
	ActionTaken *string    `json:"action_taken"` // tindakan yang sudah diambil (PRD P1 v2 §33)
	// PRD P2 v2.1 P2-SIN-06: investigasi
	InvestigationStatus   *string    `json:"investigation_status"`
	InvestigatorUserID    *uuid.UUID `json:"investigator_user_id"`
	InvestigationFindings *string    `json:"investigation_findings"`
	RootCause             *string    `json:"root_cause"`
	CorrectiveAction      *string    `json:"corrective_action"`
	CorrectiveOwnerUserID *uuid.UUID `json:"corrective_owner_user_id"`
}

func (s *Service) CreateIncident(ctx context.Context, in CreateIncidentInput) (*Incident, error) {
	var out *Incident
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.CreateIncidentTx(ctx, tx, in)
		if err != nil {
			return err
		}
		out, err = s.getIncidentTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) CreateIncidentTx(ctx context.Context, tx pgx.Tx, in CreateIncidentInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return uuid.Nil, apperr.Validation("title wajib")
	}
	if in.Category == "" {
		in.Category = "other"
	}
	if in.IncidentType == "" {
		in.IncidentType = IncidentTypeForCategory(in.Category)
	}
	// PRD P1 v2.1 GAP-P1-09: tipe incident divalidasi (sebelumnya nilai kategori lolos ke CHECK DB → error insert 500)
	if !IncidentTypes[in.IncidentType] {
		return uuid.Nil, apperr.Validation("incident_type harus security|safety|building (kategori dikirim di field category)").WithField("incident_type", "tidak valid")
	}
	if len(in.Category) > 60 {
		return uuid.Nil, apperr.Validation("category maksimal 60 karakter").WithField("category", "terlalu panjang")
	}
	if in.Severity == "" {
		in.Severity = "medium"
	}
	if !Priorities[in.Severity] {
		return uuid.Nil, apperr.Validation("severity tidak valid")
	}
	if in.Priority == "" {
		in.Priority = in.Severity // default priority mengikuti severity
	}
	if !Priorities[in.Priority] {
		return uuid.Nil, apperr.Validation("priority tidak valid")
	}
	propertyID, err := s.resolveProperty(ctx, tx, in.PropertyID, in.LocationID, nil)
	if err != nil {
		return uuid.Nil, err
	}
	if !p.HasOnProperty("operations.incidents.create", propertyID) && !p.HasOnProperty("security.incidents.create", propertyID) {
		return uuid.Nil, apperr.Forbidden("Tidak memiliki izin Report Incident pada property ini")
	}
	if err := s.validateRefs(ctx, tx, propertyID, in.LocationID, nil, in.AssigneeUserID, in.AssigneeTeamID, nil); err != nil {
		return uuid.Nil, err
	}
	loc := property.PropertyTimezone(ctx, tx, propertyID)
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixIncident, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	status := workflow.New
	if in.AssigneeUserID != nil || in.AssigneeTeamID != nil {
		status = workflow.Assigned
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO incidents (organization_id, property_id, incident_number, incident_type, category, title, description, location_id, severity, priority, status, reported_by, occurred_at, assignee_user_id, assignee_team_id, source_type, source_id, created_by, updated_by, action_taken)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$12,$12,$18) RETURNING id`,
		p.OrganizationID, propertyID, number, in.IncidentType, in.Category, in.Title, in.Description, in.LocationID, in.Severity, in.Priority, status, actorOrNil(p), in.OccurredAt, in.AssigneeUserID, in.AssigneeTeamID, in.SourceType, in.SourceID, in.ActionTaken).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	if err := setOriginTx(ctx, tx, ObjIncident, id, in.SourceType, in.SourceID); err != nil {
		return uuid.Nil, err
	}
	if in.AssigneeUserID != nil || in.AssigneeTeamID != nil {
		_, _ = tx.Exec(ctx, `INSERT INTO assignments (organization_id, object_type, object_id, assignee_user_id, assignee_team_id, assigned_by) VALUES ($1,'incident',$2,$3,$4,$5)`, p.OrganizationID, id, in.AssigneeUserID, in.AssigneeTeamID, actorOrNil(p))
	}
	if err := s.applySLATx(ctx, tx, ObjIncident, id, propertyID, in.Priority, time.Now().UTC()); err != nil {
		return uuid.Nil, err
	}
	if in.SourceType != nil && in.SourceID != nil && isLinkable(*in.SourceType) {
		if _, err := s.linkTx(ctx, tx, ObjIncident, id, *in.SourceType, *in.SourceID, "escalated_to"); err != nil {
			return uuid.Nil, err
		}
	}
	ctxAct := ctx
	if in.FromSync {
		ctxAct = authctx.With(ctx, withSource(p, authctx.SourceSync))
	}
	_ = audit.Record(ctxAct, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "severity": in.Severity, "category": in.Category}, ClientRecordedAt: in.ClientRecordedAt})
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: ObjIncident, EntityID: &id, EntityLabel: number})
	if s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.IncidentCreated, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: ObjIncident, ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p),
			Payload: map[string]any{"severity": in.Severity, "category": in.Category, "assignee_user_id": in.AssigneeUserID, "assignee_team_id": in.AssigneeTeamID}})
		if in.AssigneeUserID != nil || in.AssigneeTeamID != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.IncidentAssigned, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: ObjIncident, ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p),
				Payload: map[string]any{"assignee_user_id": in.AssigneeUserID, "assignee_team_id": in.AssigneeTeamID}})
		}
		if in.Severity == "critical" && !in.SuppressCritical {
			// PRD P1 v2 §35/§49: Critical Incident → manager, supervisor, worker (team security)
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.IncidentCritical, OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: ObjIncident, ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p),
				Payload: map[string]any{"severity": in.Severity, "category": in.Category, "assignee_user_id": in.AssigneeUserID, "assignee_team_id": in.AssigneeTeamID, "domain": "security"}})
		}
		_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(p.OrganizationID, ObjIncident, id))
	}
	return id, nil
}

func (s *Service) getIncidentTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Incident, error) {
	var i Incident
	err := tx.QueryRow(ctx, `SELECT i.id, i.property_id, i.incident_number, i.incident_type, i.category, i.title, i.description, i.location_id, l.name, i.severity, i.priority, i.status,
		i.reported_by, u.full_name, i.reported_at, i.occurred_at, i.assignee_user_id, au.full_name, i.assignee_team_id, t.name, i.resolution, i.resolved_at, i.closed_at, i.sla_risk_at, i.sla_breached_at,
		i.source_type, i.source_id, i.created_at, i.version, i.action_taken, i.origin_service_request_id,
		(SELECT count(*) FROM attachments x WHERE x.object_type = 'incident' AND x.object_id = i.id AND x.deleted_at IS NULL),
		(SELECT count(*) FROM comments c WHERE c.object_type = 'incident' AND c.object_id = i.id AND c.deleted_at IS NULL)
		FROM incidents i LEFT JOIN locations l ON l.id = i.location_id LEFT JOIN users u ON u.id = i.reported_by LEFT JOIN users au ON au.id = i.assignee_user_id LEFT JOIN teams t ON t.id = i.assignee_team_id WHERE i.id = $1`, id).
		Scan(&i.ID, &i.PropertyID, &i.IncidentNumber, &i.IncidentType, &i.Category, &i.Title, &i.Description, &i.Location.ID, &i.Location.Name, &i.Severity, &i.Priority, &i.Status,
			&i.ReportedBy, &i.ReportedByName, &i.ReportedAt, &i.OccurredAt, &i.Assignee.UserID, &i.Assignee.UserName, &i.Assignee.TeamID, &i.Assignee.TeamName, &i.Resolution, &i.ResolvedAt, &i.ClosedAt, &i.SLARiskAt, &i.SLABreachedAt,
			&i.SourceType, &i.SourceID, &i.CreatedAt, &i.Version, &i.ActionTaken, &i.OriginServiceRequestID, &i.AttachmentCount, &i.CommentCount)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Incident")
		}
		return nil, err
	}
	if i.Location.ID != nil {
		pt := property.LocationPathText(ctx, tx, *i.Location.ID)
		i.Location.PathText = &pt
	}
	i.Links, _ = s.listLinksTx(ctx, tx, ObjIncident, id)
	i.Flags = []string{}
	if i.SLABreachedAt != nil {
		i.Flags = append(i.Flags, "sla_breach")
	} else if i.SLARiskAt != nil {
		i.Flags = append(i.Flags, "sla_risk")
	}
	if i.Severity == "critical" && !isDoneStatus(string(i.Status)) {
		i.Flags = append(i.Flags, "critical")
	}
	var sla SLAInfo
	var started time.Time
	if err := tx.QueryRow(ctx, `SELECT policy_id, response_due_at, resolution_due_at, responded_at, resolved_at, sla_risk_at, sla_breached_at, escalated_at, started_at, response_breached_at FROM sla_tracking WHERE object_type = 'incident' AND object_id = $1`, id).
		Scan(&sla.PolicyID, &sla.ResponseDueAt, &sla.ResolutionDueAt, &sla.RespondedAt, &sla.ResolvedAt, &sla.RiskAt, &sla.BreachedAt, &sla.EscalatedAt, &started, &sla.ResponseBreachedAt); err == nil {
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
		FillSLAStatus(&sla, string(i.Status))
		i.SLA = &sla
	}
	i.SLAStatus = DeriveSLAStatus(string(i.Status), i.SLARiskAt, i.SLABreachedAt, i.SLA != nil)
	p := authctx.Must(ctx)
	i.AllowedActions = []string{"view"}
	isAssignee := (i.Assignee.UserID != nil && *i.Assignee.UserID == p.UserID) || (i.Assignee.TeamID != nil && p.IsMemberOfTeam(*i.Assignee.TeamID))
	for _, tr := range workflow.Incident.ActionsFrom(i.Status) {
		if !p.HasOnProperty(tr.Perm, i.PropertyID) {
			continue
		}
		if tr.Guard != nil && !isAssignee && !tr.AllowPermBypassGuard {
			continue
		}
		i.AllowedActions = append(i.AllowedActions, tr.Action)
	}
	if !workflow.Incident.IsTerminal(i.Status) && p.HasOnProperty("operations.incidents.update", i.PropertyID) {
		i.AllowedActions = append(i.AllowedActions, "update")
	}
	if !workflow.Incident.IsTerminal(i.Status) && p.HasOnProperty("operations.work_orders.create", i.PropertyID) {
		i.AllowedActions = append(i.AllowedActions, "create_work_order")
	}
	if p.HasOnProperty("operations.comments.create", i.PropertyID) {
		i.AllowedActions = append(i.AllowedActions, "comment")
	}
	if p.HasOnProperty("operations.attachments.create", i.PropertyID) {
		i.AllowedActions = append(i.AllowedActions, "attach")
	}
	s.loadIncidentExtTx(ctx, tx, &i)
	return &i, nil
}

func (s *Service) GetIncident(ctx context.Context, id uuid.UUID) (*Incident, error) {
	var out *Incident
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		i, err := s.getIncidentTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasOnProperty("operations.incidents.view", i.PropertyID) {
			return apperr.Forbidden("")
		}
		out = i
		return nil
	})
	return out, err
}

type IncidentFilter struct {
	PropertyID *uuid.UUID
	Statuses   []string
	Severities []string
	Categories []string
	LocationID *uuid.UUID
	AssigneeID *uuid.UUID
	TeamID     *uuid.UUID
	Open       *bool
	Q          string
	From, To   *time.Time
	SLAStatus  []string // PRD P1 v2 §39
	SortCol    SortCol  // PRD P0 v2 §17.3
	SortDesc   bool
}

func (s *Service) ListIncidents(ctx context.Context, f IncidentFilter, page httpx.Page) ([]Incident, *string, error) {
	p := authctx.Must(ctx)
	var out []Incident
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE 1=1"
		if f.PropertyID != nil {
			// PRD P0 v2 §24.1: property_id eksplisit tetap wajib dalam scope user
			if !p.HasAnyOnProperty("operations.incidents.view", *f.PropertyID) {
				return apperr.Forbidden("")
			}
			where += " AND i.property_id = " + add(*f.PropertyID)
		}
		where += " AND " + locationScopeSQL(p, "operations.incidents.view", "i", add)
		if len(f.Statuses) > 0 {
			where += " AND i.status = ANY(" + add(f.Statuses) + ")"
		}
		if len(f.Severities) > 0 {
			where += " AND i.severity = ANY(" + add(f.Severities) + ")"
		}
		if len(f.Categories) > 0 {
			where += " AND i.category = ANY(" + add(f.Categories) + ")"
		}
		if f.LocationID != nil {
			where += " AND i.location_id IN (SELECT d.id FROM locations d JOIN locations r ON d.path <@ r.path WHERE r.id = " + add(*f.LocationID) + ")"
		}
		if f.AssigneeID != nil {
			where += " AND i.assignee_user_id = " + add(*f.AssigneeID)
		}
		if f.TeamID != nil {
			where += " AND i.assignee_team_id = " + add(*f.TeamID)
		}
		if f.Open != nil && *f.Open {
			where += " AND i.status NOT IN ('closed','cancelled')"
		}
		if len(f.SLAStatus) > 0 {
			where += SLAStatusSQL("i", f.SLAStatus, "'resolved','closed'", false)
		}
		if f.From != nil {
			where += " AND i.reported_at >= " + add(*f.From)
		}
		if f.To != nil {
			where += " AND i.reported_at <= " + add(*f.To)
		}
		if f.Q != "" {
			q := add("%" + f.Q + "%")
			where += " AND (i.incident_number ILIKE " + q + " OR i.title ILIKE " + q + ")"
		}
		col := f.SortCol
		if col.Expr == "" {
			col, f.SortDesc = IncidentSortCols["created_at"], true
		}
		idList, nx, err := KeysetIDs(ctx, tx, KeysetQuery{From: "FROM incidents i", Where: where, IDCol: "i.id", Args: args}, col, f.SortDesc, page)
		if err != nil {
			return err
		}
		next = nx
		for _, id := range idList {
			it, err := s.getIncidentTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *it)
		}
		return nil
	})
	if out == nil {
		out = []Incident{}
	}
	return out, next, err
}

func (s *Service) UpdateIncident(ctx context.Context, id uuid.UUID, in UpdateIncidentInput, ifVersion *int) (*Incident, error) {
	p := authctx.Must(ctx)
	var out *Incident
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getIncidentTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.HasOnProperty("operations.incidents.update", before.PropertyID) {
			return apperr.Forbidden("")
		}
		if ifVersion != nil && *ifVersion != before.Version {
			return apperr.StaleVersion()
		}
		if workflow.Incident.IsTerminal(before.Status) {
			return apperr.Conflict("OBJECT_TERMINAL", "Incident sudah "+workflow.Label(before.Status))
		}
		if in.Severity != nil && !Priorities[*in.Severity] {
			return apperr.Validation("severity tidak valid")
		}
		if in.Priority != nil && !Priorities[*in.Priority] {
			return apperr.Validation("priority tidak valid")
		}
		if err := s.validateRefs(ctx, tx, before.PropertyID, in.LocationID, nil, nil, nil, nil); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE incidents SET category = COALESCE(NULLIF($2,''), category), title = COALESCE(NULLIF($3,''), title), description = COALESCE($4, description), location_id = COALESCE($5, location_id),
			severity = COALESCE(NULLIF($6,''), severity), priority = COALESCE(NULLIF($7,''), priority), occurred_at = COALESCE($8, occurred_at), updated_by = $9, action_taken = COALESCE($10, action_taken) WHERE id = $1`,
			id, derefStr(in.Category), derefStr(in.Title), in.Description, in.LocationID, derefStr(in.Severity), derefStr(in.Priority), in.OccurredAt, p.UserID, in.ActionTaken); err != nil {
			return err
		}
		if in.Severity != nil && *in.Severity == "critical" && before.Severity != "critical" && s.Jobs != nil {
			// eskalasi severity → Critical Incident (PRD P1 v2 §35)
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.IncidentCritical, OrganizationID: p.OrganizationID, PropertyID: &before.PropertyID, ObjectType: ObjIncident, ObjectID: id, ObjectLabel: before.IncidentNumber, ActorUserID: actorOrNil(p),
				Payload: map[string]any{"severity": "critical", "from": before.Severity, "assignee_user_id": before.Assignee.UserID, "assignee_team_id": before.Assignee.TeamID, "domain": "security"}})
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: id, Action: audit.ActEscalated, From: before.Severity, To: "critical", Payload: map[string]any{"reason": "severity critical"}})
		}
		if err := s.updateInvestigationTx(ctx, tx, before, IncidentInvestigationInput{InvestigationStatus: in.InvestigationStatus, InvestigatorUserID: in.InvestigatorUserID,
			InvestigationFindings: in.InvestigationFindings, RootCause: in.RootCause, CorrectiveAction: in.CorrectiveAction, CorrectiveOwnerUserID: in.CorrectiveOwnerUserID}); err != nil {
			return err
		}
		if in.ActionTaken != nil && derefStr(before.ActionTaken) != *in.ActionTaken {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: id, Action: audit.ActUpdated, Payload: map[string]any{"action_taken": *in.ActionTaken}})
		}
		if in.Priority != nil && *in.Priority != before.Priority {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: id, Action: audit.ActPriorityChanged, From: before.Priority, To: *in.Priority})
			_ = s.applySLATx(ctx, tx, ObjIncident, id, before.PropertyID, *in.Priority, before.CreatedAt)
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: id, Action: audit.ActUpdated})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: ObjIncident, EntityID: &id, EntityLabel: before.IncidentNumber, Before: before, After: in})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(p.OrganizationID, ObjIncident, id))
		}
		out, err = s.getIncidentTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) AssignIncident(ctx context.Context, id uuid.UUID, in AssignInput) (*Incident, error) {
	p := authctx.Must(ctx)
	var out *Incident
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inc, err := s.getIncidentTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.HasOnProperty("operations.incidents.assign", inc.PropertyID) {
			return apperr.Forbidden("")
		}
		if workflow.Incident.IsTerminal(inc.Status) || inc.Status == workflow.Resolved {
			return apperr.InvalidTransition("Incident tidak dapat di-assign dari status " + workflow.Label(inc.Status))
		}
		if in.AssigneeUserID == nil && in.AssigneeTeamID == nil {
			return apperr.Validation("assignee_user_id atau assignee_team_id wajib")
		}
		if err := s.validateRefs(ctx, tx, inc.PropertyID, nil, nil, in.AssigneeUserID, in.AssigneeTeamID, nil); err != nil {
			return err
		}
		_, _ = tx.Exec(ctx, `UPDATE assignments SET is_current = false, unassigned_at = now() WHERE object_type = 'incident' AND object_id = $1 AND is_current`, id)
		if _, err := tx.Exec(ctx, `INSERT INTO assignments (organization_id, object_type, object_id, assignee_user_id, assignee_team_id, assigned_by, note) VALUES ($1,'incident',$2,$3,$4,$5,$6)`, p.OrganizationID, id, in.AssigneeUserID, in.AssigneeTeamID, p.UserID, in.Note); err != nil {
			return err
		}
		newStatus := inc.Status
		if inc.Status == workflow.New {
			newStatus = workflow.Assigned
		}
		if _, err := tx.Exec(ctx, `UPDATE incidents SET assignee_user_id = $2, assignee_team_id = $3, status = $4, updated_by = $5 WHERE id = $1`, id, in.AssigneeUserID, in.AssigneeTeamID, newStatus, p.UserID); err != nil {
			return err
		}
		pl := assigneePayload(ctx, tx, in.AssigneeUserID, in.AssigneeTeamID)
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: id, Action: audit.ActAssigned, Payload: pl})
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: "assigned", EntityType: ObjIncident, EntityID: &id, EntityLabel: inc.IncidentNumber, After: pl})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.IncidentAssigned, OrganizationID: p.OrganizationID, PropertyID: &inc.PropertyID, ObjectType: ObjIncident, ObjectID: id, ObjectLabel: inc.IncidentNumber, ActorUserID: &p.UserID,
				Payload: map[string]any{"assignee_user_id": in.AssigneeUserID, "assignee_team_id": in.AssigneeTeamID}})
		}
		out, err = s.getIncidentTx(ctx, tx, id)
		return err
	})
	return out, err
}

type IncidentTransitionInput struct {
	Reason     string `json:"reason"`
	Resolution string `json:"resolution"`
}

func (s *Service) TransitionIncident(ctx context.Context, id uuid.UUID, action string, in IncidentTransitionInput) (*Incident, error) {
	p := authctx.Must(ctx)
	var out *Incident
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inc, err := s.getIncidentTx(ctx, tx, id)
		if err != nil {
			return err
		}
		tr, ok := workflow.Incident.Find(inc.Status, action)
		if !ok {
			return apperr.InvalidTransition(fmt.Sprintf("Incident %s cannot be %s from status %s", inc.IncidentNumber, action, inc.Status))
		}
		if !p.HasOnProperty(tr.Perm, inc.PropertyID) {
			return apperr.Forbidden("Memerlukan " + tr.Perm)
		}
		reason := strings.TrimSpace(in.Reason)
		if reason == "" {
			reason = strings.TrimSpace(in.Resolution)
		}
		if tr.RequireReason && reason == "" {
			return apperr.Validation("reason/resolution wajib").WithField("reason", "wajib")
		}
		if action == workflow.ActClose {
			// P2-XTW-02 (Roadmap §17 contoh 2): incident baru Closed setelah WO & verifikasi security selesai
			if err := guardChainClose(ctx, tx, ObjIncident, id); err != nil {
				return err
			}
		}
		sets := "status = $2, updated_by = $3"
		args := []any{id, tr.To, p.UserID}
		now := time.Now().UTC()
		switch action {
		case workflow.ActResolve:
			args = append(args, reason)
			sets += ", resolution = $4, resolved_at = now()"
			_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET resolved_at = $3 WHERE object_type = 'incident' AND object_id = $1 AND organization_id = $2`, id, p.OrganizationID, now)
		case workflow.ActClose:
			sets += ", closed_at = now()"
		case workflow.ActReopen:
			sets += ", resolved_at = NULL, closed_at = NULL"
			_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET resolved_at = NULL WHERE object_type = 'incident' AND object_id = $1`, id)
		case workflow.ActStart:
			_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET responded_at = COALESCE(responded_at, $2) WHERE object_type = 'incident' AND object_id = $1`, id, now)
		}
		if _, err := tx.Exec(ctx, `UPDATE incidents SET `+sets+` WHERE id = $1`, args...); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: id, Action: audit.ActStatusChanged, From: string(inc.Status), To: string(tr.To), Payload: map[string]any{"action": action, "reason": reason}})
		if action == workflow.ActResolve {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: id, Action: audit.ActResolved, Payload: map[string]any{"resolution": reason}})
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: ObjIncident, EntityID: &id, EntityLabel: inc.IncidentNumber, Before: map[string]any{"status": inc.Status}, After: map[string]any{"status": tr.To, "reason": reason}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "incident." + eventVerb(action), OrganizationID: p.OrganizationID, PropertyID: &inc.PropertyID, ObjectType: ObjIncident, ObjectID: id, ObjectLabel: inc.IncidentNumber, ActorUserID: &p.UserID,
				Payload: map[string]any{"from": inc.Status, "to": tr.To, "assignee_user_id": inc.Assignee.UserID, "assignee_team_id": inc.Assignee.TeamID, "reporter_user_id": inc.ReportedBy}})
			_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(p.OrganizationID, ObjIncident, id))
		}
		if action == workflow.ActResolve || action == workflow.ActClose {
			if err := s.chainProgressTx(ctx, tx, ObjIncident, id, inc.IncidentNumber, &reason); err != nil {
				return err
			}
		}
		out, err = s.getIncidentTx(ctx, tx, id)
		return err
	})
	return out, err
}

// CreateWorkOrderFromIncident (PRD §14: Incident dapat menghasilkan Work Order).
func (s *Service) CreateWorkOrderFromIncident(ctx context.Context, incidentID uuid.UUID, in CreateWorkOrderInput) (*WorkItem, error) {
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inc, err := s.getIncidentTx(ctx, tx, incidentID)
		if err != nil {
			return err
		}
		if workflow.Incident.IsTerminal(inc.Status) {
			return apperr.Conflict("OBJECT_TERMINAL", "Incident sudah "+workflow.Label(inc.Status))
		}
		if in.WorkOrderType == "" {
			in.WorkOrderType = "repair"
		}
		if in.Title == "" {
			in.Title = inc.Title
		}
		if in.Description == nil {
			in.Description = inc.Description
		}
		if in.LocationID == nil {
			in.LocationID = inc.Location.ID
		}
		if in.Priority == "" {
			in.Priority = inc.Priority
		}
		in.PropertyID = &inc.PropertyID
		in.SourceType = strPtr(ObjIncident)
		in.SourceID = &incidentID
		in.LinkTo = &LinkRef{ObjectType: ObjIncident, ObjectID: incidentID, LinkType: "generated_from"}
		id, err := s.CreateWorkOrderTx(ctx, tx, in)
		if err != nil {
			return err
		}
		if inc.Status == workflow.New || inc.Status == workflow.Assigned {
			_, _ = tx.Exec(ctx, `UPDATE incidents SET status = 'in_progress' WHERE id = $1`, incidentID)
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjIncident, ObjectID: incidentID, Action: audit.ActStatusChanged, From: string(inc.Status), To: "in_progress", Payload: map[string]any{"work_order_id": id}})
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

// CreateIncidentFromFinding (WF-002: Finding → Incident).
func (s *Service) CreateIncidentFromFinding(ctx context.Context, findingID uuid.UUID, in CreateIncidentInput) (*Incident, error) {
	var out *Incident
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := s.getFindingTx(ctx, tx, findingID)
		if err != nil {
			return err
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
		if in.Severity == "" {
			in.Severity = f.Severity
		}
		in.PropertyID = &f.PropertyID
		in.SourceType = strPtr(ObjFinding)
		in.SourceID = &findingID
		id, err := s.CreateIncidentTx(ctx, tx, in)
		if err != nil {
			return err
		}
		if f.Status == workflow.Open {
			_, _ = tx.Exec(ctx, `UPDATE findings SET status = 'in_progress', escalated_at = COALESCE(escalated_at, now()) WHERE id = $1`, findingID)
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: ObjFinding, ObjectID: findingID, Action: audit.ActEscalated, Payload: map[string]any{"incident_id": id}})
		}
		out, err = s.getIncidentTx(ctx, tx, id)
		return err
	})
	return out, err
}
