package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/jobs"
)

// ExecutionHook: extension point domain (TAD §5.8) — didaftarkan per "objectType:type" atau "objectType:*".
type ExecutionHook interface {
	// BeforeComplete: validasi tambahan sebelum complete (mis. patrol: semua checkpoint tercatat).
	BeforeComplete(ctx context.Context, tx pgx.Tx, item *WorkItem, in TransitionInput) error
	// AfterTransition: side-effect setelah status berubah (mis. engineering: update maintenance schedule; tenantservice: resolve SR).
	AfterTransition(ctx context.Context, tx pgx.Tx, item *WorkItem, action string, from, to workflow.Status) error
}

// CreateHook (opsional): dipanggil setelah work item dibuat (mis. housekeeping memastikan cleaning_tasks extension ada).
type CreateHook interface {
	AfterCreate(ctx context.Context, tx pgx.Tx, item *WorkItem) error
}

type Service struct {
	DB          *db.DB
	Jobs        jobs.Enqueuer
	Attachments *attachments.Service
	hooks       map[string][]ExecutionHook
}

func NewService(d *db.DB, j jobs.Enqueuer, att *attachments.Service) *Service {
	s := &Service{DB: d, Jobs: j, Attachments: att, hooks: map[string][]ExecutionHook{}}
	if att != nil {
		att.ObjectAccess = s.ObjectAccess
	}
	return s
}

// RegisterHook: key "task:patrol", "work_order:maintenance", "task:*", "work_order:*".
func (s *Service) RegisterHook(key string, h ExecutionHook) {
	s.hooks[key] = append(s.hooks[key], h)
}

func (s *Service) hooksFor(item *WorkItem) []ExecutionHook {
	var out []ExecutionHook
	out = append(out, s.hooks[item.ObjectType+":"+item.Type]...)
	out = append(out, s.hooks[item.ObjectType+":*"]...)
	return out
}

// ---------- table mapping ----------

type tableInfo struct {
	table, numberCol, typeCol, permObj string
	wf                                 workflow.Workflow
}

func tableFor(objectType string) (tableInfo, error) {
	switch objectType {
	case ObjTask:
		return tableInfo{"tasks", "task_number", "task_type", "tasks", workflow.Task}, nil
	case ObjWorkOrder:
		return tableInfo{"work_orders", "work_order_number", "work_order_type", "work_orders", workflow.WorkOrder}, nil
	}
	return tableInfo{}, apperr.Validation("object_type harus task|work_order")
}

func (t tableInfo) perm(action string) string { return "operations." + t.permObj + "." + action }

// ---------- load ----------

const workItemSelect = `
	SELECT %[1]s.id, %[1]s.property_id, %[1]s.%[2]s, %[1]s.%[3]s, %[1]s.title, %[1]s.description,
	  %[1]s.location_id, l.name, %[1]s.asset_id, a.asset_code, a.name, a.status,
	  %[1]s.priority, %[1]s.status, %[1]s.scheduled_start_at, %[1]s.due_at, %[1]s.started_at, %[1]s.completed_at, %[1]s.closed_at, %[1]s.cancelled_at,
	  %[1]s.checklist_template_id, %[4]s, %[1]s.completion_notes,
	  %[1]s.is_overdue, %[1]s.sla_risk_at, %[1]s.sla_breached_at, %[1]s.evidence_incomplete,
	  %[1]s.assignee_user_id, au.full_name, %[1]s.assignee_team_id, at.name,
	  %[1]s.source_type, %[1]s.source_id, %[1]s.created_at, %[1]s.created_by, cu.full_name, %[1]s.updated_at, %[1]s.version,
	  %[5]s
	FROM %[1]s
	LEFT JOIN locations l ON l.id = %[1]s.location_id
	LEFT JOIN assets a ON a.id = %[1]s.asset_id
	LEFT JOIN users au ON au.id = %[1]s.assignee_user_id
	LEFT JOIN teams at ON at.id = %[1]s.assignee_team_id
	LEFT JOIN users cu ON cu.id = %[1]s.created_by`

func selectFor(t tableInfo) string {
	evidenceCol := "tasks.requires_photo"
	woCols := "NULL::text, NULL::bigint, NULL::bigint, NULL::char(3), NULL::text, NULL::text, NULL::int, NULL::uuid, NULL::uuid, NULL::uuid, NULL::text, NULL::text"
	if t.table == "work_orders" {
		evidenceCol = "work_orders.requires_evidence"
		// P1: vendor_id/vendor_name/vendor_notes (Vendor Work Order, NC §37)
		woCols = "work_orders.resolution, work_orders.estimated_cost_amount, work_orders.actual_cost_amount, work_orders.currency_code, work_orders.parts_usage, work_orders.vendor_reference, work_orders.reopen_count, work_orders.requester_user_id, work_orders.maintenance_schedule_id, work_orders.vendor_id, (SELECT name FROM vendors vd WHERE vd.id = work_orders.vendor_id), work_orders.vendor_notes"
	}
	return fmt.Sprintf(workItemSelect, t.table, t.numberCol, t.typeCol, evidenceCol, woCols)
}

func scanWorkItem(row pgx.Row, objectType string) (*WorkItem, error) {
	var w WorkItem
	w.ObjectType = objectType
	var estAmt, actAmt *int64
	var cur *string
	if err := row.Scan(&w.ID, &w.PropertyID, &w.Number, &w.Type, &w.Title, &w.Description,
		&w.Location.ID, &w.Location.Name, &w.Asset.ID, &w.Asset.AssetCode, &w.Asset.Name, &w.Asset.Status,
		&w.Priority, &w.Status, &w.ScheduledStartAt, &w.DueAt, &w.StartedAt, &w.CompletedAt, &w.ClosedAt, &w.CancelledAt,
		&w.ChecklistTemplateID, &w.RequiresEvidence, &w.CompletionNotes,
		&w.IsOverdue, &w.SLARiskAt, &w.SLABreachedAt, &w.EvidenceIncomplete,
		&w.Assignee.UserID, &w.Assignee.UserName, &w.Assignee.TeamID, &w.Assignee.TeamName,
		&w.SourceType, &w.SourceID, &w.CreatedAt, &w.CreatedBy, &w.CreatedByName, &w.UpdatedAt, &w.Version,
		&w.Resolution, &estAmt, &actAmt, &cur, &w.PartsUsage, &w.VendorReference, &w.ReopenCount, &w.RequesterUserID, &w.MaintenanceScheduleID,
		&w.VendorID, &w.VendorName, &w.VendorNotes,
	); err != nil {
		return nil, err
	}
	c := "IDR"
	if cur != nil {
		c = strings.TrimSpace(*cur)
	}
	if estAmt != nil {
		w.EstimatedCost = &Money{CurrencyCode: c, Amount: *estAmt}
	}
	if actAmt != nil {
		w.ActualCost = &Money{CurrencyCode: c, Amount: *actAmt}
	}
	// flag dihitung ulang saat read (TAD §5.12)
	if !w.IsOverdue && w.DueAt != nil && w.DueAt.Before(time.Now()) && w.Status != workflow.Completed && w.Status != workflow.Closed && w.Status != workflow.Cancelled {
		w.IsOverdue = true
	}
	w.Flags = []string{}
	if w.IsOverdue {
		w.Flags = append(w.Flags, "overdue")
	}
	if w.SLABreachedAt != nil {
		w.Flags = append(w.Flags, "sla_breach")
	} else if w.SLARiskAt != nil {
		w.Flags = append(w.Flags, "sla_risk")
	}
	if w.EvidenceIncomplete {
		w.Flags = append(w.Flags, "evidence_incomplete")
	}
	w.AllowedActions = []string{}
	return &w, nil
}

// loadTx memuat work item (FOR UPDATE opsional) + cek scope property view.
func (s *Service) loadTx(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID, forUpdate bool) (*WorkItem, tableInfo, error) {
	t, err := tableFor(objectType)
	if err != nil {
		return nil, t, err
	}
	q := selectFor(t) + fmt.Sprintf(" WHERE %s.id = $1", t.table)
	if forUpdate {
		q += fmt.Sprintf(" FOR NO KEY UPDATE OF %s", t.table)
	}
	w, err := scanWorkItem(tx.QueryRow(ctx, q, id), objectType)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, t, apperr.NotFound(objectLabel(objectType))
		}
		return nil, t, err
	}
	return w, t, nil
}

func objectLabel(ot string) string {
	switch ot {
	case ObjTask:
		return "Task"
	case ObjWorkOrder:
		return "Work Order"
	case ObjIncident:
		return "Incident"
	case ObjFinding:
		return "Finding"
	case ObjServiceRequest:
		return "Service Request"
	}
	return ot
}

// enrich: SLA, allowed actions, checklist summary, counts, links, extension, location path.
func (s *Service) enrich(ctx context.Context, tx pgx.Tx, w *WorkItem, t tableInfo, full bool) error {
	p := authctx.Must(ctx)
	if w.Location.ID != nil {
		var pathText *string
		_ = tx.QueryRow(ctx, `SELECT string_agg(a.name, ' / ' ORDER BY a.depth) FROM locations l JOIN locations a ON a.path @> l.path AND a.depth > 0 WHERE l.id = $1`, *w.Location.ID).Scan(&pathText)
		if pathText == nil || *pathText == "" {
			pathText = w.Location.Name
		}
		w.Location.PathText = pathText
	}
	// SLA
	var sla SLAInfo
	err := tx.QueryRow(ctx, `SELECT policy_id, response_due_at, resolution_due_at, responded_at, resolved_at, sla_risk_at, sla_breached_at, escalated_at, started_at FROM sla_tracking WHERE object_type = $1 AND object_id = $2`, w.ObjectType, w.ID).
		Scan(&sla.PolicyID, &sla.ResponseDueAt, &sla.ResolutionDueAt, &sla.RespondedAt, &sla.ResolvedAt, &sla.RiskAt, &sla.BreachedAt, &sla.EscalatedAt, new(time.Time))
	if err == nil {
		if sla.ResolutionDueAt != nil {
			var started time.Time
			_ = tx.QueryRow(ctx, `SELECT started_at FROM sla_tracking WHERE object_type = $1 AND object_id = $2`, w.ObjectType, w.ID).Scan(&started)
			total := sla.ResolutionDueAt.Sub(started)
			ref := time.Now()
			if sla.ResolvedAt != nil {
				ref = *sla.ResolvedAt
			}
			if total > 0 {
				pct := int(ref.Sub(started) * 100 / total)
				sla.ElapsedPct = &pct
			}
			rem := int(sla.ResolutionDueAt.Sub(ref).Minutes())
			sla.RemainingMinutes = &rem
		}
		w.SLA = &sla
	}
	// allowed actions
	w.AllowedActions = s.allowedActions(ctx, tx, w, t)
	if !full {
		return nil
	}
	// checklist summary
	var cs ChecklistSummary
	err = tx.QueryRow(ctx, `SELECT r.id, r.status, r.total_items, r.answered_items, r.not_ok_items,
		(SELECT count(*) FROM checklist_run_items i WHERE i.run_id = r.id AND i.photo_required AND i.attachment_id IS NULL)
		FROM checklist_runs r WHERE r.object_type = $1 AND r.object_id = $2 ORDER BY r.created_at DESC LIMIT 1`, w.ObjectType, w.ID).
		Scan(&cs.RunID, &cs.Status, &cs.TotalItems, &cs.Answered, &cs.NotOK, &cs.PhotoMissing)
	if err == nil {
		w.ChecklistSummary = &cs
	}
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE object_type = $1 AND object_id = $2 AND deleted_at IS NULL`, w.ObjectType, w.ID).Scan(&w.AttachmentCount)
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM comments WHERE object_type = $1 AND object_id = $2 AND deleted_at IS NULL`, w.ObjectType, w.ID).Scan(&w.CommentCount)
	links, err := s.listLinksTx(ctx, tx, w.ObjectType, w.ID)
	if err != nil {
		return err
	}
	w.Links = links
	// extension (patrol_tasks / cleaning_tasks / inspections)
	if w.ObjectType == ObjTask {
		var ext []byte
		switch w.Type {
		case "patrol":
			_ = tx.QueryRow(ctx, `SELECT to_jsonb(pt) - 'task_id' - 'organization_id' || jsonb_build_object('route_name', r.name) FROM patrol_tasks pt JOIN patrol_routes r ON r.id = pt.route_id WHERE pt.task_id = $1`, w.ID).Scan(&ext)
		case "cleaning":
			_ = tx.QueryRow(ctx, `SELECT to_jsonb(ct) - 'task_id' - 'organization_id' FROM cleaning_tasks ct WHERE ct.task_id = $1`, w.ID).Scan(&ext)
		case "inspection":
			_ = tx.QueryRow(ctx, `SELECT COALESCE(to_jsonb(i) - 'task_id' - 'organization_id', '{}'::jsonb) || COALESCE(to_jsonb(hi) - 'task_id' - 'organization_id', '{}'::jsonb) FROM tasks t LEFT JOIN inspections i ON i.task_id = t.id LEFT JOIN housekeeping_inspections hi ON hi.task_id = t.id WHERE t.id = $1`, w.ID).Scan(&ext)
		}
		if len(ext) > 0 {
			_ = json.Unmarshal(ext, &w.Extension)
		}
	}
	_ = p
	return nil
}

// guardInput menghitung fakta untuk guard state machine.
func (s *Service) guardInput(ctx context.Context, tx pgx.Tx, w *WorkItem, t tableInfo, fromSync bool) workflow.GuardInput {
	p := authctx.Must(ctx)
	gi := workflow.GuardInput{HasAssignee: w.Assignee.UserID != nil || w.Assignee.TeamID != nil}
	if w.Assignee.UserID != nil && *w.Assignee.UserID == p.UserID {
		gi.IsAssignee = true
	}
	if w.Assignee.TeamID != nil && p.IsMemberOfTeam(*w.Assignee.TeamID) {
		gi.IsAssignee = true
	}
	gi.HasManage = p.HasOnProperty(t.perm("manage"), w.PropertyID)
	gi.EvidenceSatisfied, gi.EvidenceReason = s.evidenceSatisfied(ctx, tx, w, fromSync)
	return gi
}

// evidenceSatisfied (PRD §11.2, TAD §5.8): checklist wajib selesai; photo_after wajib untuk WO requires_evidence;
// photo wajib untuk Task requires_photo. Sync menerima attachment pending.
func (s *Service) evidenceSatisfied(ctx context.Context, tx pgx.Tx, w *WorkItem, fromSync bool) (bool, string) {
	var unanswered int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM checklist_run_items i JOIN checklist_runs r ON r.id = i.run_id
		WHERE r.object_type = $1 AND r.object_id = $2 AND i.is_required AND i.answered_at IS NULL`, w.ObjectType, w.ID).Scan(&unanswered)
	if unanswered > 0 {
		return false, fmt.Sprintf("%d item checklist wajib belum dijawab", unanswered)
	}
	var photoMissing int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM checklist_run_items i JOIN checklist_runs r ON r.id = i.run_id
		WHERE r.object_type = $1 AND r.object_id = $2 AND i.photo_required AND i.attachment_id IS NULL`, w.ObjectType, w.ID).Scan(&photoMissing)
	if photoMissing > 0 {
		return false, fmt.Sprintf("%d item checklist memerlukan foto", photoMissing)
	}
	if w.RequiresEvidence {
		var n int
		label := "After Photo"
		if w.ObjectType == ObjTask {
			// task: foto tipe apa pun diterima (photo / before / after / checklist)
			label = "Photo evidence"
			n, _ = attachments.CountPhotos(ctx, tx, w.ObjectType, w.ID, fromSync)
		} else {
			n, _ = attachments.CountByType(ctx, tx, w.ObjectType, w.ID, "photo_after", fromSync)
		}
		if n == 0 {
			return false, label + " wajib sebelum complete"
		}
	}
	return true, ""
}

// allowedActions: CTA ditentukan server (TAD §5.16).
func (s *Service) allowedActions(ctx context.Context, tx pgx.Tx, w *WorkItem, t tableInfo) []string {
	p := authctx.Must(ctx)
	var gi *workflow.GuardInput
	out := []string{}
	for _, tr := range t.wf.ActionsFrom(w.Status) {
		if tr.Perm != "" && !p.HasOnProperty(tr.Perm, w.PropertyID) {
			continue
		}
		if tr.Guard != nil {
			if gi == nil {
				g := s.guardInput(ctx, tx, w, t, false)
				gi = &g
			}
			// evidence tidak memblokir tampilan tombol complete (UI menampilkan alasan); assignee wajib
			probe := *gi
			probe.EvidenceSatisfied = true
			if err := tr.Guard(probe); err != nil {
				continue
			}
		}
		out = append(out, tr.Action)
	}
	if p.HasOnProperty(t.perm("update"), w.PropertyID) && !t.wf.IsTerminal(w.Status) {
		out = append(out, "update")
	}
	if p.HasOnProperty("operations.comments.create", w.PropertyID) {
		out = append(out, "comment")
	}
	if p.HasOnProperty("operations.attachments.create", w.PropertyID) && !t.wf.IsTerminal(w.Status) {
		out = append(out, "attach")
	}
	out = append(out, "view")
	return out
}

// ObjectAccess: dipakai attachments.Service — validasi permission view/update pada object.
func (s *Service) ObjectAccess(ctx context.Context, tx pgx.Tx, objectType string, objectID uuid.UUID, write bool) error {
	p := authctx.Must(ctx)
	var propertyID uuid.UUID
	var permObj string
	switch objectType {
	case ObjTask, ObjWorkOrder:
		w, t, err := s.loadTx(ctx, tx, objectType, objectID, false)
		if err != nil {
			return err
		}
		propertyID = w.PropertyID
		permObj = t.permObj
		if write && t.wf.IsTerminal(w.Status) && !p.HasOnProperty(t.perm("manage"), propertyID) {
			return apperr.Conflict("OBJECT_TERMINAL", objectLabel(objectType)+" sudah "+workflow.Label(w.Status))
		}
	case ObjIncident:
		if err := tx.QueryRow(ctx, `SELECT property_id FROM incidents WHERE id = $1`, objectID).Scan(&propertyID); err != nil {
			return apperr.NotFound("Incident")
		}
		permObj = "incidents"
	case ObjFinding:
		if err := tx.QueryRow(ctx, `SELECT property_id FROM findings WHERE id = $1`, objectID).Scan(&propertyID); err != nil {
			return apperr.NotFound("Finding")
		}
		permObj = "findings"
	case ObjServiceRequest:
		if err := tx.QueryRow(ctx, `SELECT property_id FROM service_requests WHERE id = $1`, objectID).Scan(&propertyID); err != nil {
			return apperr.NotFound("Service Request")
		}
		if p.IsTenant {
			// P1 (guardrail #4, #11): akun Mobile Tenant hanya menyentuh SR miliknya (tenant_admin: seluruh SR tenant-nya)
			var ok bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS (
				SELECT 1 FROM service_requests sr JOIN tenant_users tu ON tu.user_id = $2 AND tu.status = 'active'
				WHERE sr.id = $1 AND (sr.tenant_user_id = $2 OR (tu.role = 'tenant_admin' AND tu.tenant_id IS NOT NULL AND sr.tenant_id = tu.tenant_id)))`, objectID, p.UserID).Scan(&ok)
			if !ok {
				return apperr.NotFound("Service Request")
			}
			if write && !p.Has("tenant_app.requests.create") {
				return apperr.Forbidden("")
			}
			return nil
		}
		if !p.HasOnProperty("tenant.service_requests.view", propertyID) {
			return apperr.Forbidden("")
		}
		return nil
	case ObjAsset:
		if err := tx.QueryRow(ctx, `SELECT property_id FROM assets WHERE id = $1`, objectID).Scan(&propertyID); err != nil {
			return apperr.NotFound("Asset")
		}
		if !p.HasOnProperty("engineering.assets.view", propertyID) {
			return apperr.Forbidden("")
		}
		return nil
	case "checklist_run_item":
		var ot string
		var oid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT r.object_type, r.object_id FROM checklist_run_items i JOIN checklist_runs r ON r.id = i.run_id WHERE i.id = $1`, objectID).Scan(&ot, &oid); err != nil {
			return apperr.NotFound("Checklist item")
		}
		return s.ObjectAccess(ctx, tx, ot, oid, write)
	default:
		return apperr.Validation("object_type tidak dikenal: " + objectType)
	}
	if !p.HasOnProperty("operations."+permObj+".view", propertyID) {
		return apperr.Forbidden("")
	}
	return nil
}

// Get: detail lengkap.
func (s *Service) Get(ctx context.Context, objectType string, id uuid.UUID) (*WorkItem, error) {
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		w, t, err := s.loadTx(ctx, tx, objectType, id, false)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasOnProperty(t.perm("view"), w.PropertyID) {
			return apperr.Forbidden("")
		}
		out = w
		return s.enrich(ctx, tx, w, t, true)
	})
	return out, err
}

// GetByNumber: lookup business ID (deep link, search).
func (s *Service) GetByNumber(ctx context.Context, objectType, number string) (*WorkItem, error) {
	t, err := tableFor(objectType)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, fmt.Sprintf(`SELECT id FROM %s WHERE %s = $1`, t.table, t.numberCol), number).Scan(&id)
	})
	if err != nil {
		return nil, apperr.NotFound(objectLabel(objectType))
	}
	return s.Get(ctx, objectType, id)
}

// List dengan filter standar PRD §23 + cursor (TAD §6.3).
func (s *Service) List(ctx context.Context, objectType string, f ListFilter, page httpx.Page) ([]WorkItem, *string, error) {
	p := authctx.Must(ctx)
	t, err := tableFor(objectType)
	if err != nil {
		return nil, nil, err
	}
	tb := t.table
	var out []WorkItem
	var next *string
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE 1=1"
		if f.PropertyID != nil {
			if !p.HasOnProperty(t.perm("view"), *f.PropertyID) {
				return apperr.Forbidden("")
			}
			where += " AND " + tb + ".property_id = " + add(*f.PropertyID)
		} else if pids, all := p.PropertyIDsFor(t.perm("view")); !all {
			where += " AND " + tb + ".property_id = ANY(" + add(pids) + "::uuid[])"
		}
		if len(f.Types) > 0 {
			where += " AND " + tb + "." + t.typeCol + " = ANY(" + add(f.Types) + ")"
		}
		if len(f.Statuses) > 0 {
			where += " AND " + tb + ".status = ANY(" + add(f.Statuses) + ")"
		}
		if len(f.Priorities) > 0 {
			where += " AND " + tb + ".priority = ANY(" + add(f.Priorities) + ")"
		}
		if f.Open != nil {
			if *f.Open {
				where += " AND " + tb + ".status NOT IN ('closed','cancelled')"
			} else {
				where += " AND " + tb + ".status IN ('closed','cancelled')"
			}
		}
		if f.LocationID != nil {
			where += " AND " + tb + ".location_id IN (SELECT d.id FROM locations d JOIN locations r ON d.path <@ r.path WHERE r.id = " + add(*f.LocationID) + ")"
		}
		if f.AssigneeID != nil {
			where += " AND " + tb + ".assignee_user_id = " + add(*f.AssigneeID)
		}
		if f.TeamID != nil {
			where += " AND " + tb + ".assignee_team_id = " + add(*f.TeamID)
		}
		if f.Mine {
			where += " AND (" + tb + ".assignee_user_id = " + add(p.UserID)
			if len(p.TeamIDs) > 0 {
				where += " OR " + tb + ".assignee_team_id = ANY(" + add(p.TeamIDs) + "::uuid[])"
			}
			where += ")"
		}
		if f.Overdue != nil && *f.Overdue {
			where += " AND (" + tb + ".is_overdue OR (" + tb + ".due_at < now() AND " + tb + ".status NOT IN ('completed','closed','cancelled')))"
		}
		if f.SLARisk != nil && *f.SLARisk {
			where += " AND " + tb + ".sla_risk_at IS NOT NULL AND " + tb + ".status NOT IN ('closed','cancelled')"
		}
		if f.DueFrom != nil {
			where += " AND " + tb + ".due_at >= " + add(*f.DueFrom)
		}
		if f.DueTo != nil {
			where += " AND " + tb + ".due_at <= " + add(*f.DueTo)
		}
		if f.CreatedFrom != nil {
			where += " AND " + tb + ".created_at >= " + add(*f.CreatedFrom)
		}
		if f.CreatedTo != nil {
			where += " AND " + tb + ".created_at <= " + add(*f.CreatedTo)
		}
		if f.ScheduledOn != nil {
			d := add(f.ScheduledOn.Format("2006-01-02"))
			where += " AND ((COALESCE(" + tb + ".scheduled_start_at, " + tb + ".due_at) AT TIME ZONE COALESCE((SELECT timezone FROM properties WHERE location_id = " + tb + ".property_id), 'Asia/Jakarta'))::date = " + d + "::date)"
		}
		if f.Undated != nil && *f.Undated {
			where += " AND " + tb + ".scheduled_start_at IS NULL AND " + tb + ".due_at IS NULL"
		}
		if f.AssetID != nil {
			where += " AND " + tb + ".asset_id = " + add(*f.AssetID)
		}
		if f.SourceType != "" {
			where += " AND " + tb + ".source_type = " + add(f.SourceType)
		}
		if f.SourceID != nil {
			where += " AND " + tb + ".source_id = " + add(*f.SourceID)
		}
		if f.Q != "" {
			q := add("%" + f.Q + "%")
			where += " AND (" + tb + "." + t.numberCol + " ILIKE " + q + " OR " + tb + ".title ILIKE " + q + ")"
		}
		// sort & cursor
		sortCol, dir := "created_at", "DESC"
		switch strings.TrimPrefix(f.Sort, "-") {
		case "due_at", "priority", "created_at", "updated_at", "status", "title":
			sortCol = strings.TrimPrefix(f.Sort, "-")
			if strings.HasPrefix(f.Sort, "-") {
				dir = "DESC"
			} else {
				dir = "ASC"
			}
		}
		sortExpr := tb + "." + sortCol
		if sortCol == "priority" {
			sortExpr = "CASE " + tb + ".priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END"
		}
		if sortCol == "due_at" {
			sortExpr = "COALESCE(" + tb + ".due_at, 'infinity'::timestamptz)"
		}
		if page.Cursor != nil {
			cmp := "<"
			if dir == "ASC" {
				cmp = ">"
			}
			cast := map[string]string{"due_at": "timestamptz", "created_at": "timestamptz", "updated_at": "timestamptz", "priority": "int", "status": "text", "title": "text"}[sortCol]
			where += " AND (" + sortExpr + ", " + tb + ".id) " + cmp + " (" + add(page.Cursor.Value) + "::" + cast + ", " + add(page.Cursor.ID) + ")"
		}
		lim := add(page.Limit + 1)
		rows, err := tx.Query(ctx, selectFor(t)+where+" ORDER BY "+sortExpr+" "+dir+", "+tb+".id "+dir+" LIMIT "+lim, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var items []*WorkItem
		for rows.Next() {
			w, err := scanWorkItem(rows, objectType)
			if err != nil {
				return err
			}
			items = append(items, w)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		hasMore := len(items) > page.Limit
		if hasMore {
			items = items[:page.Limit]
		}
		for _, w := range items {
			if err := s.enrich(ctx, tx, w, t, false); err != nil {
				return err
			}
			out = append(out, *w)
		}
		if hasMore && len(items) > 0 {
			last := items[len(items)-1]
			var sv string
			switch sortCol {
			case "due_at":
				if last.DueAt != nil {
					sv = last.DueAt.UTC().Format(time.RFC3339Nano)
				} else {
					sv = "infinity"
				}
			case "priority":
				sv = map[string]string{"critical": "0", "high": "1", "medium": "2", "low": "3"}[last.Priority]
			case "created_at":
				sv = last.CreatedAt.UTC().Format(time.RFC3339Nano)
			case "updated_at":
				sv = last.UpdatedAt.UTC().Format(time.RFC3339Nano)
			case "status":
				sv = string(last.Status)
			case "title":
				sv = last.Title
			}
			c := httpx.EncodeCursor(sv, last.ID)
			next = &c
		}
		return nil
	})
	return out, next, err
}

// GetTx: detail lengkap di dalam transaksi yang sudah ada (dipakai modul domain).
func (s *Service) GetTx(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID) (*WorkItem, error) {
	w, t, err := s.loadTx(ctx, tx, objectType, id, false)
	if err != nil {
		return nil, err
	}
	if err := s.enrich(ctx, tx, w, t, true); err != nil {
		return nil, err
	}
	return w, nil
}
