// Package audit: activities (timeline per object) dan audit_logs (jejak sistem/keamanan) — PRD §24, TAD §5.10.
// Ditulis dalam transaksi yang sama dengan perubahan.
package audit

import (
	"context"
	"encoding/json"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
)

// Action katalog activity (timeline)
const (
	ActCreated            = "created"
	ActStatusChanged      = "status_changed"
	ActAssigned           = "assigned"
	ActUnassigned         = "unassigned"
	ActCommented          = "commented"
	ActAttachmentAdded    = "attachment_added"
	ActChecklistStarted   = "checklist_started"
	ActChecklistAnswered  = "checklist_item_answered"
	ActChecklistCompleted = "checklist_completed"
	ActCheckpointScanned  = "checkpoint_scanned"
	ActCheckpointMissed   = "checkpoint_missed"
	ActFindingCreated     = "finding_created"
	ActResolved           = "resolved"
	ActReopened           = "reopened"
	ActCancelled          = "cancelled"
	ActPriorityChanged    = "priority_changed"
	ActDueChanged         = "due_changed"
	ActUpdated            = "updated"
	ActLinked             = "linked"
	ActEscalated          = "escalated"
	ActEvidenceOffline    = "evidence_recorded_offline"
	ActLateEvidence       = "late_evidence"
	ActClockSkew          = "clock_skew"
	ActOverdue            = "overdue_flagged"
	ActSLARisk            = "sla_risk_flagged"
	ActSLABreached        = "sla_breached"
)

type Activity struct {
	ID               uuid.UUID      `json:"id"`
	ObjectType       string         `json:"object_type"`
	ObjectID         uuid.UUID      `json:"object_id"`
	ActorUserID      *uuid.UUID     `json:"actor_user_id"`
	ActorName        string         `json:"actor_name"`
	Action           string         `json:"action"`
	FromValue        *string        `json:"from_value"`
	ToValue          *string        `json:"to_value"`
	Payload          map[string]any `json:"payload"`
	OccurredAt       time.Time      `json:"occurred_at"`
	ClientRecordedAt *time.Time     `json:"client_recorded_at"`
	Source           string         `json:"source"`
}

type Entry struct {
	ObjectType       string
	ObjectID         uuid.UUID
	Action           string
	From, To         string
	Payload          map[string]any
	ClientRecordedAt *time.Time
	OccurredAt       *time.Time
}

// Record menulis satu activity dari principal di ctx.
func Record(ctx context.Context, q db.Querier, e Entry) error {
	p := authctx.Must(ctx)
	var actor *uuid.UUID
	if !p.IsSystem && p.UserID != uuid.Nil {
		actor = &p.UserID
	}
	src := string(p.Source)
	if src == "" {
		src = "web"
	}
	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
	if p.FullName != "" {
		e.Payload["actor_name"] = p.FullName
	}
	payload, _ := json.Marshal(e.Payload)
	occurred := time.Now().UTC()
	if e.OccurredAt != nil {
		occurred = *e.OccurredAt
	}
	_, err := q.Exec(ctx, `
		INSERT INTO activities (organization_id, object_type, object_id, actor_user_id, action, from_value, to_value, payload, occurred_at, client_recorded_at, source)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9,$10,$11)`,
		p.OrganizationID, e.ObjectType, e.ObjectID, actor, e.Action, e.From, e.To, payload, occurred, e.ClientRecordedAt, src)
	return err
}

// ListActivities: timeline per object, terbaru dulu.
func ListActivities(ctx context.Context, q db.Querier, objectType string, objectID uuid.UUID, limit int) ([]Activity, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := q.Query(ctx, `
		SELECT a.id, a.object_type, a.object_id, a.actor_user_id, COALESCE(u.full_name, a.payload->>'actor_name', 'System'),
		       a.action, a.from_value, a.to_value, a.payload, a.occurred_at, a.client_recorded_at, a.source
		FROM activities a LEFT JOIN users u ON u.id = a.actor_user_id
		WHERE a.object_type = $1 AND a.object_id = $2
		ORDER BY a.occurred_at DESC, a.id DESC LIMIT $3`, objectType, objectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Activity
	for rows.Next() {
		var a Activity
		var payload []byte
		if err := rows.Scan(&a.ID, &a.ObjectType, &a.ObjectID, &a.ActorUserID, &a.ActorName, &a.Action, &a.FromValue, &a.ToValue, &payload, &a.OccurredAt, &a.ClientRecordedAt, &a.Source); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payload, &a.Payload)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------- Audit log ----------

const (
	AuditCreate           = "create"
	AuditUpdate           = "update"
	AuditDelete           = "delete"
	AuditStatusChange     = "status_change"
	AuditLogin            = "login"
	AuditLoginFailed      = "login_failed"
	AuditLogout           = "logout"
	AuditTokenReuse       = "refresh_token_reuse"
	AuditRoleChange       = "role_change"
	AuditPermissionChange = "permission_change"
	AuditPasswordReset    = "password_reset"
	AuditExport           = "export"
	AuditAccessDenied     = "access_denied"
	AuditSyncConflict     = "sync_conflict"
)

type AuditEntry struct {
	Action      string
	EntityType  string
	EntityID    *uuid.UUID
	EntityLabel string
	Before      any
	After       any
}

func Log(ctx context.Context, q db.Querier, e AuditEntry) error {
	p := authctx.Must(ctx)
	return LogAs(ctx, q, p.OrganizationID, actorOf(p), p.IP, p.UserAgent, e)
}

func actorOf(p *authctx.Principal) *uuid.UUID {
	if p.IsSystem || p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}

// LogAs dipakai saat principal belum ada di ctx (login gagal).
func LogAs(ctx context.Context, q db.Querier, orgID uuid.UUID, actor *uuid.UUID, ip, ua string, e AuditEntry) error {
	var before, after []byte
	if e.Before != nil {
		before, _ = json.Marshal(e.Before)
	}
	if e.After != nil {
		after, _ = json.Marshal(e.After)
	}
	var ipAddr *netip.Addr
	if a, err := netip.ParseAddr(ip); err == nil {
		ipAddr = &a
	}
	_, err := q.Exec(ctx, `
		INSERT INTO audit_logs (organization_id, actor_user_id, action, entity_type, entity_id, entity_label, before, after, ip, user_agent, request_id)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,NULLIF($10,''),NULLIF($11,''))`,
		orgID, actor, e.Action, e.EntityType, e.EntityID, e.EntityLabel, nullJSON(before), nullJSON(after), ipAddr, ua, httpx.RequestID(ctx))
	return err
}

func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

type AuditLog struct {
	ID          uuid.UUID       `json:"id"`
	ActorUserID *uuid.UUID      `json:"actor_user_id"`
	ActorName   string          `json:"actor_name"`
	Action      string          `json:"action"`
	EntityType  string          `json:"entity_type"`
	EntityID    *uuid.UUID      `json:"entity_id"`
	EntityLabel *string         `json:"entity_label"`
	Before      json.RawMessage `json:"before"`
	After       json.RawMessage `json:"after"`
	IP          *string         `json:"ip"`
	RequestID   *string         `json:"request_id"`
	OccurredAt  time.Time       `json:"occurred_at"`
	Message     string          `json:"message"` // {User} {Action} {Object}
}

type AuditFilter struct {
	EntityType string
	EntityID   *uuid.UUID
	Action     string
	ActorID    *uuid.UUID
	From, To   *time.Time
}

func ListAuditLogs(ctx context.Context, q db.Querier, f AuditFilter, page httpx.Page) ([]AuditLog, *string, error) {
	args := []any{}
	where := "WHERE 1=1"
	add := func(cond string, v any) {
		args = append(args, v)
		where += " AND " + cond + "$" + itoa(len(args))
	}
	if f.EntityType != "" {
		add("a.entity_type = ", f.EntityType)
	}
	if f.EntityID != nil {
		add("a.entity_id = ", *f.EntityID)
	}
	if f.Action != "" {
		add("a.action = ", f.Action)
	}
	if f.ActorID != nil {
		add("a.actor_user_id = ", *f.ActorID)
	}
	if f.From != nil {
		add("a.occurred_at >= ", *f.From)
	}
	if f.To != nil {
		add("a.occurred_at <= ", *f.To)
	}
	if page.Cursor != nil {
		t, _ := time.Parse(time.RFC3339Nano, page.Cursor.Value)
		args = append(args, t, page.Cursor.ID)
		where += " AND (a.occurred_at, a.id) < ($" + itoa(len(args)-1) + ", $" + itoa(len(args)) + ")"
	}
	args = append(args, page.Limit+1)
	rows, err := q.Query(ctx, `
		SELECT a.id, a.actor_user_id, COALESCE(u.full_name,'System'), a.action, a.entity_type, a.entity_id, a.entity_label,
		       COALESCE(a.before,'null'::jsonb), COALESCE(a.after,'null'::jsonb), host(a.ip), a.request_id, a.occurred_at
		FROM audit_logs a LEFT JOIN users u ON u.id = a.actor_user_id `+where+`
		ORDER BY a.occurred_at DESC, a.id DESC LIMIT $`+itoa(len(args)), args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.ActorUserID, &l.ActorName, &l.Action, &l.EntityType, &l.EntityID, &l.EntityLabel, &l.Before, &l.After, &l.IP, &l.RequestID, &l.OccurredAt); err != nil {
			return nil, nil, err
		}
		label := l.EntityType
		if l.EntityLabel != nil {
			label = *l.EntityLabel
		}
		l.Message = l.ActorName + " " + l.Action + " " + label
		out = append(out, l)
	}
	var next *string
	if len(out) > page.Limit {
		last := out[page.Limit-1]
		out = out[:page.Limit]
		c := httpx.EncodeCursor(last.OccurredAt.Format(time.RFC3339Nano), last.ID)
		next = &c
	}
	return out, next, rows.Err()
}

func itoa(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = digits[n%10]
		n /= 10
	}
	return string(b[i:])
}
