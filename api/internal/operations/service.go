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
	// listener rantai SR lintas tim (PRD P1 v2.1 §5.15) — didaftarkan tenantservice
	chainListeners []ChainListener
	// akses lampiran untuk object domain lain (PRD P2 v2.1: emergency_alert, parking_violation, lost_found_item, …)
	objectAccessors map[string]ObjectAccessor
}

// ObjectAccessor: validasi akses object domain (dipakai lampiran/evidence) — didaftarkan modul pemilik object.
type ObjectAccessor func(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, write bool) error

// RegisterObjectAccess mendaftarkan validator akses untuk object_type milik modul domain.
func (s *Service) RegisterObjectAccess(objectType string, fn ObjectAccessor) {
	if s.objectAccessors == nil {
		s.objectAccessors = map[string]ObjectAccessor{}
	}
	s.objectAccessors[objectType] = fn
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
	  %[5]s,
	  %[1]s.last_reopened_at, %[1]s.reopen_count, %[6]s, a.equipment_id, COALESCE(eq.type_name, eq.category_name),
	  %[1]s.escalated_at, %[1]s.escalation_level, %[7]s,
	  %[1]s.origin_service_request_id, (SELECT osr.request_number FROM service_requests osr WHERE osr.id = %[1]s.origin_service_request_id), %[8]s
	FROM %[1]s
	LEFT JOIN locations l ON l.id = %[1]s.location_id
	LEFT JOIN assets a ON a.id = %[1]s.asset_id
	LEFT JOIN equipment eq ON eq.id = a.equipment_id
	LEFT JOIN users au ON au.id = %[1]s.assignee_user_id
	LEFT JOIN teams at ON at.id = %[1]s.assignee_team_id
	LEFT JOIN users cu ON cu.id = %[1]s.created_by`

func selectFor(t tableInfo) string {
	evidenceCol := "tasks.requires_photo"
	woCols := "NULL::text, NULL::bigint, NULL::bigint, NULL::char(3), NULL::text, NULL::text, NULL::uuid, NULL::uuid, NULL::uuid, NULL::text, NULL::text"
	notesCol := "NULL::text"
	// PRD P1 v2: category (task) · parts/service/other cost + submitted_at (WO)
	p1Cols := "tasks.category, NULL::bigint, NULL::bigint, NULL::bigint, NULL::timestamptz"
	// PRD P1 v2.1 §5.15: tujuan Task tindak lanjut (inspeksi akhir / re-clean / verifikasi security)
	followUpCol := "tasks.follow_up_purpose"
	if t.table == "work_orders" {
		followUpCol = "NULL::text"
		p1Cols = "NULL::text, work_orders.parts_cost_amount, work_orders.service_cost_amount, work_orders.other_cost_amount, work_orders.submitted_at"
		evidenceCol = "work_orders.requires_evidence"
		// P1: vendor_id/vendor_name/vendor_notes (Vendor Work Order, NC §37)
		woCols = "work_orders.resolution, work_orders.estimated_cost_amount, work_orders.actual_cost_amount, work_orders.currency_code, work_orders.parts_usage, work_orders.vendor_reference, work_orders.requester_user_id, work_orders.maintenance_schedule_id, work_orders.vendor_id, (SELECT name FROM vendors vd WHERE vd.id = work_orders.vendor_id), work_orders.vendor_notes"
		notesCol = "work_orders.notes"
	}
	return fmt.Sprintf(workItemSelect, t.table, t.numberCol, t.typeCol, evidenceCol, woCols, notesCol, p1Cols, followUpCol)
}

func scanWorkItem(row pgx.Row, objectType string) (*WorkItem, error) {
	var w WorkItem
	w.ObjectType = objectType
	var estAmt, actAmt, partsAmt, svcAmt, otherAmt *int64
	var cur *string
	if err := row.Scan(&w.ID, &w.PropertyID, &w.Number, &w.Type, &w.Title, &w.Description,
		&w.Location.ID, &w.Location.Name, &w.Asset.ID, &w.Asset.AssetCode, &w.Asset.Name, &w.Asset.Status,
		&w.Priority, &w.Status, &w.ScheduledStartAt, &w.DueAt, &w.StartedAt, &w.CompletedAt, &w.ClosedAt, &w.CancelledAt,
		&w.ChecklistTemplateID, &w.RequiresEvidence, &w.CompletionNotes,
		&w.IsOverdue, &w.SLARiskAt, &w.SLABreachedAt, &w.EvidenceIncomplete,
		&w.Assignee.UserID, &w.Assignee.UserName, &w.Assignee.TeamID, &w.Assignee.TeamName,
		&w.SourceType, &w.SourceID, &w.CreatedAt, &w.CreatedBy, &w.CreatedByName, &w.UpdatedAt, &w.Version,
		&w.Resolution, &estAmt, &actAmt, &cur, &w.PartsUsage, &w.VendorReference, &w.RequesterUserID, &w.MaintenanceScheduleID,
		&w.VendorID, &w.VendorName, &w.VendorNotes,
		&w.LastReopenedAt, &w.ReopenCount, &w.Notes, &w.Asset.EquipmentID, &w.Asset.EquipmentName,
		&w.EscalatedAt, &w.EscalationLevel, &w.Category, &partsAmt, &svcAmt, &otherAmt, &w.SubmittedAt,
		&w.OriginServiceRequestID, &w.OriginServiceRequestNumber, &w.FollowUpPurpose,
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
	if partsAmt != nil {
		w.PartsCost = &Money{CurrencyCode: c, Amount: *partsAmt}
	}
	if svcAmt != nil {
		w.ServiceCost = &Money{CurrencyCode: c, Amount: *svcAmt}
	}
	if otherAmt != nil {
		w.OtherCost = &Money{CurrencyCode: c, Amount: *otherAmt}
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
	// PRD P0 v2 §10.4/§20.2: Reopened & Critical sebagai flag status umum
	if w.ReopenCount != nil && *w.ReopenCount > 0 {
		w.Flags = append(w.Flags, "reopened")
	}
	if w.Priority == "critical" && w.Status != workflow.Closed && w.Status != workflow.Cancelled {
		w.Flags = append(w.Flags, "critical")
	}
	// PRD P1 v2 §52 Escalation
	if w.EscalationLevel > 0 && !isDoneStatus(string(w.Status)) {
		w.Flags = append(w.Flags, "escalated")
	}
	w.SLAStatus = DeriveSLAStatus(string(w.Status), w.SLARiskAt, w.SLABreachedAt, w.DueAt != nil)
	w.AllowedActions = []string{}
	return &w, nil
}

func isDoneStatus(st string) bool {
	switch st {
	case "completed", "closed", "cancelled", "resolved":
		return true
	}
	return false
}

// DeriveSLAStatus (PRD P1 v2 §21.3): On Track / At Risk / Breached / Completed. hasTarget=false dan belum selesai → ""
// (object tanpa SLA/due date tidak punya status SLA).
func DeriveSLAStatus(status string, riskAt, breachedAt *time.Time, hasTarget bool) string {
	switch {
	case status == "cancelled" || status == "draft":
		return ""
	case isDoneStatus(status):
		return SLACompleted
	case breachedAt != nil:
		return SLABreached
	case riskAt != nil:
		return SLAAtRisk
	case hasTarget:
		return SLAOnTrack
	}
	return ""
}

// SLAStatusSQL: predikat SQL filter sla_status untuk tabel dengan kolom status/sla_risk_at/sla_breached_at.
// doneStatuses: status "selesai" object (task/WO: 'completed','closed'; SR/incident: 'resolved','closed').
// hasDue: tabel memiliki kolom due_at.
func SLAStatusSQL(tb string, statuses []string, doneStatuses string, hasDue bool) string {
	var parts []string
	open := tb + ".status NOT IN (" + doneStatuses + ",'cancelled','draft')"
	target := "EXISTS (SELECT 1 FROM sla_tracking st WHERE st.object_id = " + tb + ".id)"
	if hasDue {
		target = "(" + tb + ".due_at IS NOT NULL OR " + target + ")"
	}
	for _, st := range statuses {
		switch st {
		case SLABreached:
			parts = append(parts, "("+open+" AND "+tb+".sla_breached_at IS NOT NULL)")
		case SLAAtRisk:
			parts = append(parts, "("+open+" AND "+tb+".sla_breached_at IS NULL AND "+tb+".sla_risk_at IS NOT NULL)")
		case SLAOnTrack:
			parts = append(parts, "("+open+" AND "+tb+".sla_breached_at IS NULL AND "+tb+".sla_risk_at IS NULL AND "+target+")")
		case SLACompleted:
			parts = append(parts, tb+".status IN ("+doneStatuses+")")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " AND (" + strings.Join(parts, " OR ") + ")"
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
	err := tx.QueryRow(ctx, `SELECT policy_id, response_due_at, resolution_due_at, responded_at, resolved_at, sla_risk_at, sla_breached_at, escalated_at, started_at, response_breached_at FROM sla_tracking WHERE object_type = $1 AND object_id = $2`, w.ObjectType, w.ID).
		Scan(&sla.PolicyID, &sla.ResponseDueAt, &sla.ResolutionDueAt, &sla.RespondedAt, &sla.ResolvedAt, &sla.RiskAt, &sla.BreachedAt, &sla.EscalatedAt, new(time.Time), &sla.ResponseBreachedAt)
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
		FillSLAStatus(&sla, string(w.Status))
		w.SLA = &sla
		if w.SLAStatus == "" && sla.Status != "" {
			w.SLAStatus = sla.Status
		}
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
			// PRD P2 v2.1 P2-RTE-02/03: posisi stop & progres route run
			_ = tx.QueryRow(ctx, `SELECT to_jsonb(ct) - 'task_id' - 'organization_id' || COALESCE((SELECT jsonb_build_object('route_id', r.id, 'route_code', r.route_code, 'route_name', r.name,
				'route_total_stops', rr.total_stops, 'route_completed_stops', rr.completed_stops, 'route_run_status', rr.status)
				FROM cleaning_route_runs rr JOIN cleaning_routes r ON r.id = rr.route_id WHERE rr.id = ct.route_run_id), '{}'::jsonb)
				FROM cleaning_tasks ct WHERE ct.task_id = $1`, w.ID).Scan(&ext)
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

// FillSLAStatus melengkapi status resolusi & respons SLA (PRD P1 v2 §21.2–21.3).
func FillSLAStatus(sla *SLAInfo, status string) {
	sla.Status = DeriveSLAStatus(status, sla.RiskAt, sla.BreachedAt, sla.ResolutionDueAt != nil)
	if sla.ResolvedAt != nil && sla.ResolutionDueAt != nil {
		met := !sla.ResolvedAt.After(*sla.ResolutionDueAt) && sla.BreachedAt == nil
		sla.Met = &met
	}
	switch {
	case sla.ResponseDueAt == nil:
		sla.ResponseStatus = ""
	case sla.RespondedAt != nil:
		if sla.RespondedAt.After(*sla.ResponseDueAt) {
			sla.ResponseStatus = "breached"
		} else {
			sla.ResponseStatus = "met"
		}
	case sla.ResponseBreachedAt != nil || time.Now().After(*sla.ResponseDueAt):
		sla.ResponseStatus = "breached"
	default:
		sla.ResponseStatus = "pending"
	}
}

// guardInput menghitung fakta untuk guard state machine.
func (s *Service) guardInput(ctx context.Context, tx pgx.Tx, w *WorkItem, t tableInfo, fromSync bool) workflow.GuardInput {
	p := authctx.Must(ctx)
	gi := workflow.GuardInput{HasAssignee: w.Assignee.UserID != nil || w.Assignee.TeamID != nil || w.VendorID != nil}
	if w.Assignee.UserID != nil && *w.Assignee.UserID == p.UserID {
		gi.IsAssignee = true
	}
	if w.Assignee.TeamID != nil && p.IsMemberOfTeam(*w.Assignee.TeamID) {
		gi.IsAssignee = true
	}
	// akun Vendor mengerjakan WO yang ditugaskan ke vendor-nya (PRD P0 v2 §8.4)
	if p.VendorID != nil && w.VendorID != nil && *p.VendorID == *w.VendorID {
		gi.IsAssignee = true
	}
	gi.HasManage = canWI(ctx, tx, t.perm("manage"), w)
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
	var gi *workflow.GuardInput
	out := []string{}
	for _, tr := range t.wf.ActionsFrom(w.Status) {
		if tr.Perm != "" && !canWI(ctx, tx, tr.Perm, w) {
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
	if canWI(ctx, tx, t.perm("update"), w) && !t.wf.IsTerminal(w.Status) {
		out = append(out, "update")
	}
	if canWI(ctx, tx, "operations.comments.create", w) {
		out = append(out, "comment")
	}
	if canWI(ctx, tx, "operations.attachments.create", w) && !t.wf.IsTerminal(w.Status) {
		out = append(out, "attach")
	}
	if deletable(w) && canWI(ctx, tx, t.perm("delete"), w) {
		out = append(out, "delete")
	}
	if w.ObjectType == ObjTask && !t.wf.IsTerminal(w.Status) && canAt(ctx, tx, "operations.work_orders.create", w.PropertyID, w.Location.ID) {
		out = append(out, "create_work_order")
	}
	if escalatable(w.Status) && canWI(ctx, tx, t.perm("escalate"), w) {
		out = append(out, "escalate")
	}
	// PRD P1 v2.1 P1-XMW-03: WO selesai → Task tindak lanjut untuk team lain (inspeksi akhir, re-clean, verifikasi)
	if w.ObjectType == ObjWorkOrder && (w.Status == workflow.Completed || w.Status == workflow.Closed) && canAt(ctx, tx, "operations.tasks.create", w.PropertyID, w.Location.ID) &&
		(authctx.Must(ctx).VendorID == nil || authctx.Must(ctx).IsSystem) {
		out = append(out, "create_follow_up_task")
	}
	out = append(out, "view")
	return out
}

// ObjectAccess: dipakai attachments.Service — validasi permission view/update pada object.
func (s *Service) ObjectAccess(ctx context.Context, tx pgx.Tx, objectType string, objectID uuid.UUID, write bool) error {
	p := authctx.Must(ctx)
	var propertyID uuid.UUID
	var locationID *uuid.UUID
	var permObj string
	switch objectType {
	case ObjTask, ObjWorkOrder:
		w, t, err := s.loadTx(ctx, tx, objectType, objectID, false)
		if err != nil {
			return err
		}
		if !canWI(ctx, tx, t.perm("view"), w) {
			return apperr.Forbidden("")
		}
		if write && t.wf.IsTerminal(w.Status) && !canWI(ctx, tx, t.perm("manage"), w) {
			return apperr.Conflict("OBJECT_TERMINAL", objectLabel(objectType)+" sudah "+workflow.Label(w.Status))
		}
		return nil
	case ObjIncident:
		if err := tx.QueryRow(ctx, `SELECT property_id, location_id FROM incidents WHERE id = $1`, objectID).Scan(&propertyID, &locationID); err != nil {
			return apperr.NotFound("Incident")
		}
		permObj = "incidents"
	case ObjFinding:
		if err := tx.QueryRow(ctx, `SELECT property_id, location_id FROM findings WHERE id = $1`, objectID).Scan(&propertyID, &locationID); err != nil {
			return apperr.NotFound("Finding")
		}
		permObj = "findings"
	case ObjServiceRequest:
		if err := tx.QueryRow(ctx, `SELECT property_id, location_id FROM service_requests WHERE id = $1`, objectID).Scan(&propertyID, &locationID); err != nil {
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
		if p.VendorID != nil || !canAt(ctx, tx, "tenant.service_requests.view", propertyID, locationID) {
			return apperr.Forbidden("")
		}
		return nil
	case ObjAsset:
		if err := tx.QueryRow(ctx, `SELECT property_id, location_id FROM assets WHERE id = $1`, objectID).Scan(&propertyID, &locationID); err != nil {
			return apperr.NotFound("Asset")
		}
		if p.VendorID != nil || !canAt(ctx, tx, "engineering.assets.view", propertyID, locationID) {
			return apperr.Forbidden("")
		}
		return nil
	// PRD P1 v2 §6.3/§7: foto lokasi & facility, gambar denah (floor plan)
	case "location":
		if err := tx.QueryRow(ctx, `SELECT property_id FROM locations WHERE id = $1 AND deleted_at IS NULL`, objectID).Scan(&propertyID); err != nil {
			return apperr.NotFound("Location")
		}
		perm := "property.locations.view"
		if write {
			perm = "property.locations.update"
		}
		if p.VendorID != nil || !canAt(ctx, tx, perm, propertyID, &objectID) {
			return apperr.Forbidden("")
		}
		return nil
	case "facility":
		if err := tx.QueryRow(ctx, `SELECT property_id, location_id FROM facilities WHERE id = $1 AND deleted_at IS NULL`, objectID).Scan(&propertyID, &locationID); err != nil {
			return apperr.NotFound("Facility")
		}
		act := "view"
		if write {
			act = "update"
		}
		if p.VendorID != nil || !(canAt(ctx, tx, "property.facilities."+act, propertyID, locationID) || canAt(ctx, tx, "booking.facilities."+act, propertyID, locationID)) {
			return apperr.Forbidden("")
		}
		return nil
	case "floor_plan":
		if err := tx.QueryRow(ctx, `SELECT property_id, location_id FROM floor_plans WHERE id = $1 AND deleted_at IS NULL`, objectID).Scan(&propertyID, &locationID); err != nil {
			return apperr.NotFound("Floor plan")
		}
		perm := "property.floor_plans.view"
		if write {
			perm = "property.floor_plans.update"
		}
		if p.VendorID != nil || !canAt(ctx, tx, perm, propertyID, locationID) {
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
		if fn, ok := s.objectAccessors[objectType]; ok {
			return fn(ctx, tx, objectID, write)
		}
		return apperr.Validation("object_type tidak dikenal: " + objectType)
	}
	if p.VendorID != nil || !canAt(ctx, tx, "operations."+permObj+".view", propertyID, locationID) {
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
		if !canWI(ctx, tx, t.perm("view"), w) {
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
			if !p.HasAnyOnProperty(t.perm("view"), *f.PropertyID) {
				return apperr.Forbidden("")
			}
			where += " AND " + tb + ".property_id = " + add(*f.PropertyID)
		}
		// scope property-wide / building (PRD P0 v2 §8.4) + resource scope vendor
		where += " AND " + p.ScopeSQL(t.perm("view"), tb+".property_id", "(SELECT sl.path FROM locations sl WHERE sl.id = "+tb+".location_id)", add)
		where += vendorScopeSQL(p, objectType, tb, add)
		if f.EquipmentID != nil {
			where += " AND " + tb + ".asset_id IN (SELECT ea.id FROM assets ea WHERE ea.equipment_id = " + add(*f.EquipmentID) + ")"
		}
		if f.VendorID != nil && objectType == ObjWorkOrder {
			where += " AND " + tb + ".vendor_id = " + add(*f.VendorID)
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
				// Draft WO belum menjadi pekerjaan operasional (PRD P1 v2 §23)
				where += " AND " + tb + ".status NOT IN ('closed','cancelled','draft')"
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
		tzExpr := "COALESCE((SELECT timezone FROM properties WHERE location_id = " + tb + ".property_id), 'Asia/Jakarta')"
		if f.DueToday {
			where += " AND " + tb + ".status NOT IN ('completed','closed','cancelled','draft') AND (" + tb + ".due_at AT TIME ZONE " + tzExpr + ")::date = (now() AT TIME ZONE " + tzExpr + ")::date"
		}
		if f.CompletedToday {
			where += " AND " + tb + ".completed_at IS NOT NULL AND (" + tb + ".completed_at AT TIME ZONE " + tzExpr + ")::date = (now() AT TIME ZONE " + tzExpr + ")::date"
		}
		if f.CompletedFrom != nil {
			where += " AND " + tb + ".completed_at >= " + add(*f.CompletedFrom)
		}
		if f.CompletedTo != nil {
			where += " AND " + tb + ".completed_at <= " + add(*f.CompletedTo)
		}
		if len(f.SLAStatus) > 0 {
			where += SLAStatusSQL(tb, f.SLAStatus, "'completed','closed'", true)
		}
		if len(f.Categories) > 0 && objectType == ObjTask {
			where += " AND " + tb + ".category = ANY(" + add(f.Categories) + ")"
		}
		if f.Unassigned {
			where += " AND " + tb + ".assignee_user_id IS NULL AND " + tb + ".assignee_team_id IS NULL"
			if objectType == ObjWorkOrder {
				where += " AND " + tb + ".vendor_id IS NULL"
			}
		}
		if f.Escalated != nil {
			if *f.Escalated {
				where += " AND " + tb + ".escalation_level > 0 AND " + tb + ".status NOT IN ('completed','closed','cancelled')"
			} else {
				where += " AND " + tb + ".escalation_level = 0"
			}
		}
		if f.Undated != nil && *f.Undated {
			where += " AND " + tb + ".scheduled_start_at IS NULL AND " + tb + ".due_at IS NULL"
		}
		if f.Unscheduled != nil && *f.Unscheduled {
			where += " AND " + tb + ".scheduled_start_at IS NULL"
		}
		if len(f.InspectionResults) > 0 && objectType == ObjTask {
			where += " AND EXISTS (SELECT 1 FROM inspections ir WHERE ir.task_id = " + tb + ".id AND ir.result = ANY(" + add(f.InspectionResults) + "))"
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
			// PRD P1 v2 §40: Task ID/Title/Assignee · WO ID/Title/Asset
			where += " AND (" + tb + "." + t.numberCol + " ILIKE " + q + " OR " + tb + ".title ILIKE " + q +
				" OR au.full_name ILIKE " + q + " OR at.name ILIKE " + q + " OR a.asset_code ILIKE " + q + " OR a.name ILIKE " + q + ")"
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
