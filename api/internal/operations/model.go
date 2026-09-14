// Package operations: TASK ENGINE — tasks, work_orders, assignments, checklists, comments, findings,
// incidents, SLA, object_links (PRD §9–§11, §14; TAD §5.8, §7.4, ADR-007).
package operations

import (
	"time"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/operations/workflow"
)

const (
	ObjTask                = "task"
	ObjWorkOrder           = "work_order"
	ObjIncident            = "incident"
	ObjFinding             = "finding"
	ObjServiceRequest      = "service_request"
	ObjInspection          = "inspection"
	ObjAsset               = "asset"
	ObjMaintenanceSchedule = "maintenance_schedule"
)

var Priorities = map[string]bool{"low": true, "medium": true, "high": true, "critical": true}
var TaskTypes = map[string]bool{"general": true, "patrol": true, "cleaning": true, "inspection": true, "routine_maintenance": true}
var WorkOrderTypes = map[string]bool{"maintenance": true, "corrective": true, "repair": true, "service": true}

// Money (Naming Convention §40)
type Money struct {
	CurrencyCode string `json:"currency_code"`
	Amount       int64  `json:"amount"`
}

// Assignee ringkas untuk kartu/list.
type AssigneeRef struct {
	UserID   *uuid.UUID `json:"user_id"`
	UserName *string    `json:"user_name"`
	TeamID   *uuid.UUID `json:"team_id"`
	TeamName *string    `json:"team_name"`
}

type LocationRef struct {
	ID       *uuid.UUID `json:"id"`
	Name     *string    `json:"name"`
	PathText *string    `json:"path_text"`
}

type AssetRef struct {
	ID        *uuid.UUID `json:"id"`
	AssetCode *string    `json:"asset_code"`
	Name      *string    `json:"name"`
	Status    *string    `json:"status"`
}

type SLAInfo struct {
	PolicyID         *uuid.UUID `json:"policy_id"`
	ResponseDueAt    *time.Time `json:"response_due_at"`
	ResolutionDueAt  *time.Time `json:"resolution_due_at"`
	RespondedAt      *time.Time `json:"responded_at"`
	ResolvedAt       *time.Time `json:"resolved_at"`
	RiskAt           *time.Time `json:"sla_risk_at"`
	BreachedAt       *time.Time `json:"sla_breached_at"`
	EscalatedAt      *time.Time `json:"escalated_at"`
	ElapsedPct       *int       `json:"elapsed_pct"`
	RemainingMinutes *int       `json:"remaining_minutes"`
}

// WorkItem: representasi bersama Task & Work Order (field WO-only nullable).
type WorkItem struct {
	ObjectType          string          `json:"object_type"`
	ID                  uuid.UUID       `json:"id"`
	PropertyID          uuid.UUID       `json:"property_id"`
	Number              string          `json:"number"` // TSK-2026-000001 / WO-2026-000001
	Type                string          `json:"type"`   // task_type / work_order_type
	Title               string          `json:"title"`
	Description         *string         `json:"description"`
	Location            LocationRef     `json:"location"`
	Asset               AssetRef        `json:"asset"`
	Priority            string          `json:"priority"`
	Status              workflow.Status `json:"status"`
	ScheduledStartAt    *time.Time      `json:"scheduled_start_at"`
	DueAt               *time.Time      `json:"due_at"`
	StartedAt           *time.Time      `json:"started_at"`
	CompletedAt         *time.Time      `json:"completed_at"`
	ClosedAt            *time.Time      `json:"closed_at"`
	CancelledAt         *time.Time      `json:"cancelled_at"`
	ChecklistTemplateID *uuid.UUID      `json:"checklist_template_id"`
	RequiresEvidence    bool            `json:"requires_evidence"` // WO: after photo; Task: requires_photo
	CompletionNotes     *string         `json:"completion_notes"`
	IsOverdue           bool            `json:"is_overdue"`
	SLARiskAt           *time.Time      `json:"sla_risk_at"`
	SLABreachedAt       *time.Time      `json:"sla_breached_at"`
	EvidenceIncomplete  bool            `json:"evidence_incomplete"`
	Assignee            AssigneeRef     `json:"assignee"`
	SourceType          *string         `json:"source_type"`
	SourceID            *uuid.UUID      `json:"source_id"`
	CreatedAt           time.Time       `json:"created_at"`
	CreatedBy           *uuid.UUID      `json:"created_by"`
	CreatedByName       *string         `json:"created_by_name"`
	UpdatedAt           time.Time       `json:"updated_at"`
	Version             int             `json:"version"`

	// Work Order only
	Resolution            *string    `json:"resolution,omitempty"`
	EstimatedCost         *Money     `json:"estimated_cost,omitempty"`
	ActualCost            *Money     `json:"actual_cost,omitempty"`
	PartsUsage            *string    `json:"parts_usage,omitempty"`
	VendorReference       *string    `json:"vendor_reference,omitempty"`
	ReopenCount           *int       `json:"reopen_count,omitempty"`
	RequesterUserID       *uuid.UUID `json:"requester_user_id,omitempty"`
	MaintenanceScheduleID *uuid.UUID `json:"maintenance_schedule_id,omitempty"`

	// Computed
	SLA              *SLAInfo          `json:"sla,omitempty"`
	AllowedActions   []string          `json:"allowed_actions"`
	Flags            []string          `json:"flags"` // overdue | sla_risk | sla_breach | evidence_incomplete
	ChecklistSummary *ChecklistSummary `json:"checklist_summary,omitempty"`
	AttachmentCount  int               `json:"attachment_count"`
	CommentCount     int               `json:"comment_count"`
	Links            []ObjectLink      `json:"links,omitempty"`
	Extension        map[string]any    `json:"extension,omitempty"` // patrol_tasks / cleaning_tasks / inspections
}

type ChecklistSummary struct {
	RunID        uuid.UUID `json:"run_id"`
	Status       string    `json:"status"`
	TotalItems   int       `json:"total_items"`
	Answered     int       `json:"answered_items"`
	NotOK        int       `json:"not_ok_items"`
	PhotoMissing int       `json:"photo_missing"`
}

type ObjectLink struct {
	ID         uuid.UUID `json:"id"`
	LinkType   string    `json:"link_type"`
	Direction  string    `json:"direction"` // from | to
	ObjectType string    `json:"object_type"`
	ObjectID   uuid.UUID `json:"object_id"`
	Label      string    `json:"label"` // business id
	Title      string    `json:"title"`
	Status     string    `json:"status"`
}

type Assignment struct {
	ID             uuid.UUID   `json:"id"`
	Assignee       AssigneeRef `json:"assignee"`
	AssignedBy     *uuid.UUID  `json:"assigned_by"`
	AssignedByName string      `json:"assigned_by_name"`
	AssignedAt     time.Time   `json:"assigned_at"`
	UnassignedAt   *time.Time  `json:"unassigned_at"`
	IsCurrent      bool        `json:"is_current"`
	Note           *string     `json:"note"`
}

type Comment struct {
	ID         uuid.UUID  `json:"id"`
	ObjectType string     `json:"object_type"`
	ObjectID   uuid.UUID  `json:"object_id"`
	AuthorID   uuid.UUID  `json:"author_id"`
	AuthorName string     `json:"author_name"`
	Body       string     `json:"body"`
	Source     string     `json:"source"`
	CreatedAt  time.Time  `json:"created_at"`
	EditedAt   *time.Time `json:"edited_at"`
}

// ---------- Inputs ----------

type CreateTaskInput struct {
	PropertyID          *uuid.UUID `json:"property_id"`
	TaskType            string     `json:"task_type"`
	Title               string     `json:"title"`
	Description         *string    `json:"description"`
	LocationID          *uuid.UUID `json:"location_id"`
	AssetID             *uuid.UUID `json:"asset_id"`
	Priority            string     `json:"priority"`
	ScheduledStartAt    *time.Time `json:"scheduled_start_at"`
	DueAt               *time.Time `json:"due_at"`
	ChecklistTemplateID *uuid.UUID `json:"checklist_template_id"`
	RequiresPhoto       bool       `json:"requires_photo"`
	AssigneeUserID      *uuid.UUID `json:"assignee_user_id"`
	AssigneeTeamID      *uuid.UUID `json:"assignee_team_id"`
	SourceType          *string    `json:"source_type"`
	SourceID            *uuid.UUID `json:"source_id"`
	LinkTo              *LinkRef   `json:"link_to"` // FR-TSK-013
}

type CreateWorkOrderInput struct {
	PropertyID            *uuid.UUID `json:"property_id"`
	WorkOrderType         string     `json:"work_order_type"`
	Title                 string     `json:"title"`
	Description           *string    `json:"description"`
	LocationID            *uuid.UUID `json:"location_id"`
	AssetID               *uuid.UUID `json:"asset_id"`
	Priority              string     `json:"priority"`
	ScheduledStartAt      *time.Time `json:"scheduled_start_at"`
	DueAt                 *time.Time `json:"due_at"`
	ChecklistTemplateID   *uuid.UUID `json:"checklist_template_id"`
	RequiresEvidence      *bool      `json:"requires_evidence"`
	AssigneeUserID        *uuid.UUID `json:"assignee_user_id"`
	AssigneeTeamID        *uuid.UUID `json:"assignee_team_id"`
	EstimatedCost         *Money     `json:"estimated_cost"`
	VendorReference       *string    `json:"vendor_reference"`
	RequesterUserID       *uuid.UUID `json:"requester_user_id"`
	SourceType            *string    `json:"source_type"`
	SourceID              *uuid.UUID `json:"source_id"`
	MaintenanceScheduleID *uuid.UUID `json:"maintenance_schedule_id"`
	LinkTo                *LinkRef   `json:"link_to"`
}

type LinkRef struct {
	ObjectType string    `json:"object_type"`
	ObjectID   uuid.UUID `json:"object_id"`
	LinkType   string    `json:"link_type"` // generated_from | related_to | rework_of | escalated_to
}

type UpdateWorkItemInput struct {
	Title               *string    `json:"title"`
	Description         *string    `json:"description"`
	LocationID          *uuid.UUID `json:"location_id"`
	AssetID             *uuid.UUID `json:"asset_id"`
	Priority            *string    `json:"priority"`
	ScheduledStartAt    *time.Time `json:"scheduled_start_at"`
	DueAt               *time.Time `json:"due_at"`
	ChecklistTemplateID *uuid.UUID `json:"checklist_template_id"`
	RequiresEvidence    *bool      `json:"requires_evidence"`
	EstimatedCost       *Money     `json:"estimated_cost"`
	ActualCost          *Money     `json:"actual_cost"`
	PartsUsage          *string    `json:"parts_usage"`
	VendorReference     *string    `json:"vendor_reference"`
	Resolution          *string    `json:"resolution"`
}

type AssignInput struct {
	AssigneeUserID *uuid.UUID `json:"assignee_user_id"`
	AssigneeTeamID *uuid.UUID `json:"assignee_team_id"`
	Note           *string    `json:"note"`
}

type TransitionInput struct {
	Reason           string     `json:"reason"`
	CompletionNotes  *string    `json:"completion_notes"`
	Resolution       *string    `json:"resolution"`
	ActualCost       *Money     `json:"actual_cost"`
	PartsUsage       *string    `json:"parts_usage"`
	ScheduledStartAt *time.Time `json:"scheduled_start_at"`
	DueAt            *time.Time `json:"due_at"`
	GPSLat           *float64   `json:"gps_lat"`
	GPSLng           *float64   `json:"gps_lng"`
	GPSStatus        string     `json:"gps_status"`
	ClientRecordedAt *time.Time `json:"client_recorded_at"`
	// FromSync: guard evidence menerima attachment pending (TAD §6.5)
	FromSync bool `json:"-"`
}

type ListFilter struct {
	PropertyID             *uuid.UUID
	Types                  []string
	Statuses               []string
	Priorities             []string
	LocationID             *uuid.UUID // subtree
	AssigneeID             *uuid.UUID
	TeamID                 *uuid.UUID
	Mine                   bool
	Overdue                *bool
	SLARisk                *bool
	Open                   *bool // status not in closed/cancelled
	DueFrom, DueTo         *time.Time
	CreatedFrom, CreatedTo *time.Time
	ScheduledOn            *time.Time // tanggal (timezone property) — Today's Operations
	AssetID                *uuid.UUID
	SourceType             string
	SourceID               *uuid.UUID
	Q                      string
	Sort                   string // -due_at,priority,created_at
}
