// Package events: domain event {object}.{event} (Naming Convention §62) yang di-enqueue dalam
// transaksi yang sama dengan perubahan (outbox via River, TAD §5.10).
package events

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Event struct {
	Type           string         `json:"type"` // work_order.assigned
	OrganizationID uuid.UUID      `json:"organization_id"`
	PropertyID     *uuid.UUID     `json:"property_id,omitempty"`
	ObjectType     string         `json:"object_type"` // work_order
	ObjectID       uuid.UUID      `json:"object_id"`
	ObjectLabel    string         `json:"object_label"` // WO-2026-000123
	ActorUserID    *uuid.UUID     `json:"actor_user_id,omitempty"`
	OccurredAt     time.Time      `json:"occurred_at"`
	Payload        map[string]any `json:"payload,omitempty"`
}

// Subscriber menerima event di worker (notification rules, search indexer, domain hook).
type Subscriber interface {
	Name() string
	Handle(ctx context.Context, ev Event) error
}

// Katalog event P0 (subset; subscriber mem-filter berdasarkan prefix/tipe).
const (
	TaskCreated       = "task.created"
	TaskAssigned      = "task.assigned"
	TaskStarted       = "task.started"
	TaskCompleted     = "task.completed"
	TaskClosed        = "task.closed"
	TaskReopened      = "task.reopened"
	TaskCancelled     = "task.cancelled"
	TaskStatusChanged = "task.status_changed"
	TaskDueSoon       = "task.due_soon"
	TaskOverdue       = "task.overdue"
	TaskSLARisk       = "task.sla_risk"
	TaskSLABreached   = "task.sla_breached"

	WorkOrderCreated       = "work_order.created"
	WorkOrderAssigned      = "work_order.assigned"
	WorkOrderStarted       = "work_order.started"
	WorkOrderCompleted     = "work_order.completed"
	WorkOrderClosed        = "work_order.closed"
	WorkOrderReopened      = "work_order.reopened"
	WorkOrderCancelled     = "work_order.cancelled"
	WorkOrderStatusChanged = "work_order.status_changed"
	WorkOrderDueSoon       = "work_order.due_soon"
	WorkOrderOverdue       = "work_order.overdue"
	WorkOrderSLARisk       = "work_order.sla_risk"
	WorkOrderSLABreached   = "work_order.sla_breached"

	PatrolOverdue          = "patrol_task.overdue"
	PatrolCheckpointMissed = "patrol_task.checkpoint_missed"

	FindingCreated   = "finding.created"
	FindingResolved  = "finding.resolved"
	FindingEscalated = "finding.escalated"

	IncidentCreated   = "incident.created"
	IncidentAssigned  = "incident.assigned"
	IncidentResolved  = "incident.resolved"
	IncidentClosed    = "incident.closed"
	IncidentEscalated = "incident.escalated"

	ServiceRequestCreated       = "service_request.created"
	ServiceRequestAssigned      = "service_request.assigned"
	ServiceRequestResolved      = "service_request.resolved"
	ServiceRequestClosed        = "service_request.closed"
	ServiceRequestStatusChanged = "service_request.status_changed"
	ServiceRequestSLARisk       = "service_request.sla_risk"
	ServiceRequestSLABreached   = "service_request.sla_breached"

	MaintenanceScheduleDue     = "maintenance_schedule.due"
	MaintenanceScheduleOverdue = "maintenance_schedule.overdue"

	AssetCreated    = "asset.created"
	AssetUpdated    = "asset.updated"
	TenantCreated   = "tenant.created"
	TenantUpdated   = "tenant.updated"
	LocationCreated = "location.created"
	LocationUpdated = "location.updated"

	AttachmentConfirmed = "attachment.confirmed"
	CommentAdded        = "comment.added"
	SyncConflict        = "sync.conflict"
	ExportReady         = "export.ready"
)
