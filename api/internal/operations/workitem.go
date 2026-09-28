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
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Create ----------

// CreateTask (FR-TSK-001..005, 013).
func (s *Service) CreateTask(ctx context.Context, in CreateTaskInput) (*WorkItem, error) {
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.CreateTaskTx(ctx, tx, in)
		if err != nil {
			return err
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

// CreateTaskTx: dipakai generator (patrol/cleaning/inspection) di transaksi yang sama.
func (s *Service) CreateTaskTx(ctx context.Context, tx pgx.Tx, in CreateTaskInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return uuid.Nil, apperr.Validation("title wajib").WithField("title", "wajib")
	}
	if in.TaskType == "" {
		in.TaskType = "general"
	}
	if !TaskTypes[in.TaskType] {
		return uuid.Nil, apperr.Validation("task_type tidak valid")
	}
	if in.Priority == "" {
		in.Priority = "medium"
	}
	if !Priorities[in.Priority] {
		return uuid.Nil, apperr.Validation("priority harus low|medium|high|critical")
	}
	category, err := normalizeCategory(in.Category)
	if err != nil {
		return uuid.Nil, err
	}
	propertyID, err := s.resolveProperty(ctx, tx, in.PropertyID, in.LocationID, in.AssetID)
	if err != nil {
		return uuid.Nil, err
	}
	if !canAt(ctx, tx, "operations.tasks.create", propertyID, in.LocationID) || (p.VendorID != nil && !p.IsSystem) {
		return uuid.Nil, apperr.Forbidden("Tidak memiliki operations.tasks.create pada property ini")
	}
	if err := s.validateRefs(ctx, tx, propertyID, in.LocationID, in.AssetID, in.AssigneeUserID, in.AssigneeTeamID, in.ChecklistTemplateID); err != nil {
		return uuid.Nil, err
	}
	loc := property.PropertyTimezone(ctx, tx, propertyID)
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixTask, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	status := workflow.New
	if in.ScheduledStartAt != nil || in.DueAt != nil {
		status = workflow.Scheduled
	}
	if in.AssigneeUserID != nil || in.AssigneeTeamID != nil {
		status = workflow.Assigned
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO tasks (organization_id, property_id, task_number, task_type, title, description, location_id, asset_id, priority, status,
		  scheduled_start_at, due_at, checklist_template_id, requires_photo, assignee_user_id, assignee_team_id, source_type, source_id, created_by, updated_by, category)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$19,$20) RETURNING id`,
		p.OrganizationID, propertyID, number, in.TaskType, in.Title, in.Description, in.LocationID, in.AssetID, in.Priority, status,
		in.ScheduledStartAt, in.DueAt, in.ChecklistTemplateID, in.RequiresPhoto, in.AssigneeUserID, in.AssigneeTeamID, in.SourceType, in.SourceID, actorOrNil(p), category).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	if err := s.afterCreate(ctx, tx, ObjTask, id, number, propertyID, in.Priority, in.DueAt, in.ChecklistTemplateID, in.AssigneeUserID, in.AssigneeTeamID, in.LinkTo, in.SourceType, in.SourceID, false); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// CreateWorkOrder (FR-WO-001..007).
func (s *Service) CreateWorkOrder(ctx context.Context, in CreateWorkOrderInput) (*WorkItem, error) {
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id, err := s.CreateWorkOrderTx(ctx, tx, in)
		if err != nil {
			return err
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

func (s *Service) CreateWorkOrderTx(ctx context.Context, tx pgx.Tx, in CreateWorkOrderInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return uuid.Nil, apperr.Validation("title wajib").WithField("title", "wajib")
	}
	if !WorkOrderTypes[in.WorkOrderType] {
		return uuid.Nil, apperr.Validation("work_order_type harus corrective|preventive|inspection|repair|service|other|maintenance").WithField("work_order_type", "tidak valid")
	}
	if in.Draft && (in.AssigneeUserID != nil || in.AssigneeTeamID != nil) {
		return uuid.Nil, apperr.Validation("Draft Work Order belum dapat di-assign; submit terlebih dahulu").WithField("assignee_user_id", "tidak diizinkan untuk draft")
	}
	for f, m := range map[string]*Money{"estimated_cost": in.EstimatedCost, "service_cost": in.ServiceCost, "other_cost": in.OtherCost} {
		if m != nil && m.Amount < 0 {
			return uuid.Nil, apperr.Validation(f+" tidak boleh negatif").WithField(f, "harus ≥ 0")
		}
	}
	if in.Priority == "" {
		in.Priority = "medium"
	}
	if !Priorities[in.Priority] {
		return uuid.Nil, apperr.Validation("priority harus low|medium|high|critical")
	}
	if in.LocationID == nil {
		// FR-WO-003 lokasi wajib; boleh diturunkan dari asset
		if in.AssetID != nil {
			var lid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT location_id FROM assets WHERE id = $1 AND deleted_at IS NULL`, *in.AssetID).Scan(&lid); err == nil {
				in.LocationID = &lid
			}
		}
		if in.LocationID == nil {
			return uuid.Nil, apperr.Validation("location_id wajib untuk Work Order").WithField("location_id", "wajib")
		}
	}
	// FR-WO-004: maintenance wajib asset
	if in.WorkOrderType == "maintenance" && in.AssetID == nil {
		return uuid.Nil, apperr.Validation("Work Order maintenance wajib memiliki asset").WithField("asset_id", "wajib untuk maintenance")
	}
	propertyID, err := s.resolveProperty(ctx, tx, in.PropertyID, in.LocationID, in.AssetID)
	if err != nil {
		return uuid.Nil, err
	}
	if !canAt(ctx, tx, "operations.work_orders.create", propertyID, in.LocationID) || (p.VendorID != nil && !p.IsSystem) {
		return uuid.Nil, apperr.Forbidden("Tidak memiliki operations.work_orders.create pada property ini")
	}
	if err := s.validateRefs(ctx, tx, propertyID, in.LocationID, in.AssetID, in.AssigneeUserID, in.AssigneeTeamID, in.ChecklistTemplateID); err != nil {
		return uuid.Nil, err
	}
	loc := property.PropertyTimezone(ctx, tx, propertyID)
	number, err := ids.NextYearly(ctx, tx, p.OrganizationID, ids.PrefixWorkOrder, time.Now(), loc)
	if err != nil {
		return uuid.Nil, err
	}
	requiresEvidence := true
	if in.RequiresEvidence != nil {
		requiresEvidence = *in.RequiresEvidence
	}
	status := workflow.New
	if in.ScheduledStartAt != nil || in.DueAt != nil {
		status = workflow.Scheduled
	}
	if in.AssigneeUserID != nil || in.AssigneeTeamID != nil {
		status = workflow.Assigned
	}
	var submittedAt *time.Time
	if in.Draft {
		status = workflow.Draft
	} else {
		now := time.Now().UTC()
		submittedAt = &now
	}
	var estAmt *int64
	if in.EstimatedCost != nil {
		estAmt = &in.EstimatedCost.Amount
	}
	requester := in.RequesterUserID
	if requester == nil && !p.IsSystem {
		requester = &p.UserID
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO work_orders (organization_id, property_id, work_order_number, work_order_type, title, description, location_id, asset_id, priority, status,
		  scheduled_start_at, due_at, checklist_template_id, requires_evidence, assignee_user_id, assignee_team_id, estimated_cost_amount, vendor_reference,
		  requester_user_id, source_type, source_id, maintenance_schedule_id, notes, created_by, updated_by, service_cost_amount, other_cost_amount, submitted_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$24,$23,$23,$25,$26,$27) RETURNING id`,
		p.OrganizationID, propertyID, number, in.WorkOrderType, in.Title, in.Description, in.LocationID, in.AssetID, in.Priority, status,
		in.ScheduledStartAt, in.DueAt, in.ChecklistTemplateID, requiresEvidence, in.AssigneeUserID, in.AssigneeTeamID, estAmt, in.VendorReference,
		requester, in.SourceType, in.SourceID, in.MaintenanceScheduleID, actorOrNil(p), in.Notes, moneyAmt(in.ServiceCost), moneyAmt(in.OtherCost), submittedAt).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	if err := s.afterCreate(ctx, tx, ObjWorkOrder, id, number, propertyID, in.Priority, in.DueAt, in.ChecklistTemplateID, in.AssigneeUserID, in.AssigneeTeamID, in.LinkTo, in.SourceType, in.SourceID, in.Draft); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func actorOrNil(p *authctx.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	return &p.UserID
}

func moneyAmt(m *Money) *int64 {
	if m == nil {
		return nil
	}
	return &m.Amount
}

// normalizeCategory: kategori operasional Task (PRD P1 v2 §13.3) — teks bebas pendek, snake/kode dari master data.
func normalizeCategory(c *string) (*string, error) {
	if c == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*c)
	if v == "" {
		return nil, nil
	}
	if len(v) > 60 {
		return nil, apperr.Validation("category maksimal 60 karakter").WithField("category", "terlalu panjang")
	}
	return &v, nil
}

// DefaultTaskCategories: kategori operasional bawaan (PRD P1 v2 §13.3); kategori lain yang sudah dipakai organisasi ikut ditampilkan.
var DefaultTaskCategories = []string{"general", "engineering", "electrical", "plumbing", "hvac", "cleaning", "security", "safety", "landscaping", "inspection", "tenant_service"}

type TaskCategory struct {
	Code  string `json:"code"`
	Count int    `json:"count"` // jumlah task (org) dengan kategori ini
}

func (s *Service) TaskCategories(ctx context.Context) ([]TaskCategory, error) {
	counts := map[string]int{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT category, count(*) FROM tasks WHERE category IS NOT NULL GROUP BY category`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			var n int
			if err := rows.Scan(&c, &n); err != nil {
				return err
			}
			counts[c] = n
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	out := []TaskCategory{}
	seen := map[string]bool{}
	for _, c := range DefaultTaskCategories {
		out = append(out, TaskCategory{Code: c, Count: counts[c]})
		seen[c] = true
	}
	for c, n := range counts {
		if !seen[c] {
			out = append(out, TaskCategory{Code: c, Count: n})
		}
	}
	return out, nil
}

// escalatable: eskalasi hanya untuk pekerjaan yang masih berjalan (bukan draft/selesai/batal).
func escalatable(st workflow.Status) bool {
	switch st {
	case workflow.New, workflow.Scheduled, workflow.Assigned, workflow.InProgress, workflow.OnHold:
		return true
	}
	return false
}

// afterCreate: assignment row, checklist run, SLA tracking, link, activity, audit, event.
// draft=true (WO Draft, PRD P1 v2 §23): SLA belum berjalan — diterapkan saat submit.
func (s *Service) afterCreate(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID, number string, propertyID uuid.UUID, priority string, dueAt *time.Time,
	templateID, assigneeUser, assigneeTeam *uuid.UUID, link *LinkRef, sourceType *string, sourceID *uuid.UUID, draft bool) error {
	p := authctx.Must(ctx)
	// PRD P1 v2.1 P1-XMW-01: SR asal rantai ikut diturunkan dari object sumber
	if err := setOriginTx(ctx, tx, objectType, id, sourceType, sourceID); err != nil {
		return err
	}
	if assigneeUser != nil || assigneeTeam != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO assignments (organization_id, object_type, object_id, assignee_user_id, assignee_team_id, assigned_by) VALUES ($1,$2,$3,$4,$5,$6)`,
			p.OrganizationID, objectType, id, assigneeUser, assigneeTeam, actorOrNil(p)); err != nil {
			return err
		}
	}
	if templateID != nil {
		if _, err := s.startChecklistRunTx(ctx, tx, objectType, id, *templateID); err != nil {
			return err
		}
	}
	if !draft {
		if err := s.startSLATx(ctx, tx, objectType, id, propertyID, priority, dueAt == nil); err != nil {
			return err
		}
	}
	if link != nil {
		if _, err := s.linkTx(ctx, tx, objectType, id, link.ObjectType, link.ObjectID, link.LinkType); err != nil {
			return err
		}
	} else if sourceType != nil && sourceID != nil && isLinkable(*sourceType) {
		if _, err := s.linkTx(ctx, tx, objectType, id, *sourceType, *sourceID, "generated_from"); err != nil {
			return err
		}
	}
	// hook AfterCreate (domain extension)
	if w, _, err := s.loadTx(ctx, tx, objectType, id, false); err == nil {
		for _, h := range s.hooksFor(w) {
			if ch, ok := h.(CreateHook); ok {
				if err := ch.AfterCreate(ctx, tx, w); err != nil {
					return err
				}
			}
		}
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: objectType, ObjectID: id, Action: audit.ActCreated, Payload: map[string]any{"number": number, "priority": priority}})
	if assigneeUser != nil || assigneeTeam != nil {
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: objectType, ObjectID: id, Action: audit.ActAssigned, Payload: assigneePayload(ctx, tx, assigneeUser, assigneeTeam)})
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: objectType, EntityID: &id, EntityLabel: number})
	if s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: objectType + ".created", OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: objectType, ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p)})
		if assigneeUser != nil || assigneeTeam != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: objectType + ".assigned", OrganizationID: p.OrganizationID, PropertyID: &propertyID, ObjectType: objectType, ObjectID: id, ObjectLabel: number, ActorUserID: actorOrNil(p),
				Payload: map[string]any{"assignee_user_id": assigneeUser, "assignee_team_id": assigneeTeam}})
		}
		_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(p.OrganizationID, objectType, id))
	}
	return nil
}

// startSLATx: SLA tracking mulai sekarang; due_at default = SLA resolution due bila belum diisi.
func (s *Service) startSLATx(ctx context.Context, tx pgx.Tx, objectType string, id, propertyID uuid.UUID, priority string, setDue bool) error {
	if err := s.applySLATx(ctx, tx, objectType, id, propertyID, priority, time.Now().UTC()); err != nil {
		return err
	}
	if setDue {
		t, _ := tableFor(objectType)
		_, _ = tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET due_at = (SELECT resolution_due_at FROM sla_tracking WHERE object_type = $2 AND object_id = $1) WHERE id = $1 AND due_at IS NULL`, t.table), id, objectType)
	}
	return nil
}

func isLinkable(t string) bool {
	switch t {
	case ObjServiceRequest, ObjFinding, ObjIncident, ObjTask, ObjWorkOrder, ObjInspection, ObjMaintenanceSchedule:
		return true
	}
	return false
}

func assigneePayload(ctx context.Context, tx pgx.Tx, userID, teamID *uuid.UUID) map[string]any {
	pl := map[string]any{}
	if userID != nil {
		var name string
		_ = tx.QueryRow(ctx, `SELECT full_name FROM users WHERE id = $1`, *userID).Scan(&name)
		pl["assignee_user_id"] = *userID
		pl["assignee_name"] = name
	}
	if teamID != nil {
		var name string
		_ = tx.QueryRow(ctx, `SELECT name FROM teams WHERE id = $1`, *teamID).Scan(&name)
		pl["assignee_team_id"] = *teamID
		pl["assignee_team_name"] = name
	}
	return pl
}

// resolveProperty: property dari input, atau dari location/asset.
func (s *Service) resolveProperty(ctx context.Context, tx pgx.Tx, propertyID, locationID, assetID *uuid.UUID) (uuid.UUID, error) {
	if propertyID != nil {
		return *propertyID, nil
	}
	if locationID != nil {
		return property.ResolvePropertyOfLocation(ctx, tx, *locationID)
	}
	if assetID != nil {
		var pid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM assets WHERE id = $1`, *assetID).Scan(&pid); err == nil {
			return pid, nil
		}
	}
	return uuid.Nil, apperr.Validation("property_id atau location_id wajib")
}

// validateRefs: lokasi/asset harus di property yang sama; assignee user/team ada; template published.
func (s *Service) validateRefs(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, locationID, assetID, userID, teamID, templateID *uuid.UUID) error {
	if locationID != nil {
		var pid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM locations WHERE id = $1 AND deleted_at IS NULL`, *locationID).Scan(&pid); err != nil || pid != propertyID {
			return apperr.Validation("location_id tidak ditemukan di property ini").WithField("location_id", "tidak valid")
		}
	}
	if assetID != nil {
		var pid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT property_id FROM assets WHERE id = $1 AND deleted_at IS NULL`, *assetID).Scan(&pid); err != nil || pid != propertyID {
			return apperr.Validation("asset_id tidak ditemukan di property ini").WithField("asset_id", "tidak valid")
		}
	}
	if userID != nil {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND is_active AND deleted_at IS NULL)`, *userID).Scan(&ok); err != nil || !ok {
			return apperr.Validation("assignee_user_id tidak ditemukan").WithField("assignee_user_id", "tidak valid")
		}
	}
	if teamID != nil {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM teams WHERE id = $1 AND is_active AND deleted_at IS NULL AND (property_id IS NULL OR property_id = $2))`, *teamID, propertyID).Scan(&ok); err != nil || !ok {
			return apperr.Validation("assignee_team_id tidak ditemukan di property ini").WithField("assignee_team_id", "tidak valid")
		}
	}
	if templateID != nil {
		var st string
		if err := tx.QueryRow(ctx, `SELECT status FROM checklist_templates WHERE id = $1 AND deleted_at IS NULL`, *templateID).Scan(&st); err != nil {
			return apperr.Validation("checklist_template_id tidak ditemukan")
		}
		if st != "published" {
			return apperr.Validation("checklist template belum dipublikasikan")
		}
	}
	return nil
}

// ---------- Update ----------

func (s *Service) Update(ctx context.Context, objectType string, id uuid.UUID, in UpdateWorkItemInput, ifVersion *int) (*WorkItem, error) {
	p := authctx.Must(ctx)
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		w, t, err := s.loadTx(ctx, tx, objectType, id, true)
		if err != nil {
			return err
		}
		if !canWI(ctx, tx, t.perm("update"), w) {
			return apperr.Forbidden("")
		}
		if ifVersion != nil && *ifVersion != w.Version {
			return apperr.StaleVersion()
		}
		if t.wf.IsTerminal(w.Status) && !canWI(ctx, tx, t.perm("manage"), w) {
			return apperr.Conflict("OBJECT_TERMINAL", objectLabel(objectType)+" sudah "+workflow.Label(w.Status))
		}
		if in.Priority != nil && !Priorities[*in.Priority] {
			return apperr.Validation("priority tidak valid")
		}
		for f, m := range map[string]*Money{"estimated_cost": in.EstimatedCost, "actual_cost": in.ActualCost, "parts_cost": in.PartsCost, "service_cost": in.ServiceCost, "other_cost": in.OtherCost} {
			if m != nil && m.Amount < 0 {
				return apperr.Validation(f+" tidak boleh negatif").WithField(f, "harus ≥ 0")
			}
		}
		category, err := normalizeCategory(in.Category)
		if err != nil {
			return err
		}
		if err := s.validateRefs(ctx, tx, w.PropertyID, in.LocationID, in.AssetID, nil, nil, in.ChecklistTemplateID); err != nil {
			return err
		}
		if objectType == ObjWorkOrder && in.LocationID == nil && w.Location.ID == nil {
			return apperr.Validation("location_id wajib untuk Work Order")
		}
		sets := []string{"updated_by = $2"}
		args := []any{id, actorOrNil(p)}
		add := func(col string, v any) {
			args = append(args, v)
			sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
		}
		if in.Title != nil && strings.TrimSpace(*in.Title) != "" {
			add("title", strings.TrimSpace(*in.Title))
		}
		if in.Description != nil {
			add("description", *in.Description)
		}
		if in.LocationID != nil {
			add("location_id", *in.LocationID)
		}
		if in.AssetID != nil {
			add("asset_id", *in.AssetID)
		}
		if in.Priority != nil {
			add("priority", *in.Priority)
		}
		if in.ScheduledStartAt != nil {
			add("scheduled_start_at", *in.ScheduledStartAt)
		}
		if in.DueAt != nil {
			add("due_at", *in.DueAt)
		}
		if in.ChecklistTemplateID != nil {
			add("checklist_template_id", *in.ChecklistTemplateID)
		}
		if objectType == ObjTask && in.Category != nil {
			add("category", category)
		}
		if objectType == ObjWorkOrder {
			if in.RequiresEvidence != nil {
				add("requires_evidence", *in.RequiresEvidence)
			}
			if in.EstimatedCost != nil {
				add("estimated_cost_amount", in.EstimatedCost.Amount)
			}
			if in.ActualCost != nil {
				add("actual_cost_amount", in.ActualCost.Amount)
			}
			// PRD P1 v2 §26: rincian biaya parts / jasa / lain-lain
			if in.PartsCost != nil {
				add("parts_cost_amount", in.PartsCost.Amount)
			}
			if in.ServiceCost != nil {
				add("service_cost_amount", in.ServiceCost.Amount)
			}
			if in.OtherCost != nil {
				add("other_cost_amount", in.OtherCost.Amount)
			}
			if in.PartsUsage != nil {
				add("parts_usage", *in.PartsUsage)
			}
			if in.VendorReference != nil {
				add("vendor_reference", *in.VendorReference)
			}
			if in.Resolution != nil {
				add("resolution", *in.Resolution)
			}
			if in.Notes != nil {
				add("notes", *in.Notes)
			}
		} else if in.RequiresEvidence != nil {
			add("requires_photo", *in.RequiresEvidence)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s WHERE id = $1`, t.table, strings.Join(sets, ", ")), args...); err != nil {
			return err
		}
		if objectType == ObjWorkOrder && in.ActualCost == nil && (in.PartsCost != nil || in.ServiceCost != nil || in.OtherCost != nil) {
			if err := syncActualCost(ctx, tx, id); err != nil {
				return err
			}
		}
		if in.ChecklistTemplateID != nil && (w.ChecklistTemplateID == nil || *w.ChecklistTemplateID != *in.ChecklistTemplateID) {
			if _, err := s.startChecklistRunTx(ctx, tx, objectType, id, *in.ChecklistTemplateID); err != nil {
				return err
			}
		}
		if in.Priority != nil && *in.Priority != w.Priority {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: objectType, ObjectID: id, Action: audit.ActPriorityChanged, From: w.Priority, To: *in.Priority})
			if err := s.applySLATx(ctx, tx, objectType, id, w.PropertyID, *in.Priority, w.CreatedAt); err != nil {
				return err
			}
		}
		if in.DueAt != nil && (w.DueAt == nil || !w.DueAt.Equal(*in.DueAt)) {
			from := ""
			if w.DueAt != nil {
				from = w.DueAt.Format(time.RFC3339)
			}
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: objectType, ObjectID: id, Action: audit.ActDueChanged, From: from, To: in.DueAt.Format(time.RFC3339)})
			// due dimajukan/dimundurkan: reset flag overdue agar sweep hitung ulang
			_, _ = tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET is_overdue = false WHERE id = $1 AND due_at > now()`, t.table), id)
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: objectType, ObjectID: id, Action: audit.ActUpdated})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(p.OrganizationID, objectType, id))
		}
		nw, _, err := s.loadTx(ctx, tx, objectType, id, false)
		if err != nil {
			return err
		}
		// audit menyimpan kondisi sebelum & sesudah (bukan hanya patch input) — PRD P0 v2 §16
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: objectType, EntityID: &id, EntityLabel: w.Number, Before: w, After: nw})
		out = nw
		return s.enrich(ctx, tx, nw, t, true)
	})
	return out, err
}

// ---------- Assign (FR-TSK-004, FR-WO-006) ----------

func (s *Service) Assign(ctx context.Context, objectType string, id uuid.UUID, in AssignInput) (*WorkItem, error) {
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		w, err := s.AssignTx(ctx, tx, objectType, id, in)
		if err != nil {
			return err
		}
		out = w
		return nil
	})
	return out, err
}

func (s *Service) AssignTx(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID, in AssignInput) (*WorkItem, error) {
	p := authctx.Must(ctx)
	w, t, err := s.loadTx(ctx, tx, objectType, id, true)
	if err != nil {
		return nil, err
	}
	if !canWI(ctx, tx, t.perm("assign"), w) {
		return nil, apperr.Forbidden("Memerlukan " + t.perm("assign"))
	}
	if t.wf.IsTerminal(w.Status) || w.Status == workflow.Completed || w.Status == workflow.Draft {
		// Draft WO harus di-submit (Open) sebelum di-assign (PRD P1 v2 §23)
		return nil, apperr.InvalidTransition(fmt.Sprintf("%s %s tidak dapat di-assign dari status %s", objectLabel(objectType), w.Number, workflow.Label(w.Status)))
	}
	if in.AssigneeUserID == nil && in.AssigneeTeamID == nil {
		return nil, apperr.Validation("assignee_user_id atau assignee_team_id wajib")
	}
	if err := s.validateRefs(ctx, tx, w.PropertyID, nil, nil, in.AssigneeUserID, in.AssigneeTeamID, nil); err != nil {
		return nil, err
	}
	same := ptrEq(w.Assignee.UserID, in.AssigneeUserID) && ptrEq(w.Assignee.TeamID, in.AssigneeTeamID)
	if same {
		return w, s.enrich(ctx, tx, w, t, true)
	}
	if _, err := tx.Exec(ctx, `UPDATE assignments SET is_current = false, unassigned_at = now() WHERE object_type = $1 AND object_id = $2 AND is_current`, objectType, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO assignments (organization_id, object_type, object_id, assignee_user_id, assignee_team_id, assigned_by, note) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		p.OrganizationID, objectType, id, in.AssigneeUserID, in.AssigneeTeamID, actorOrNil(p), in.Note); err != nil {
		return nil, err
	}
	newStatus := w.Status
	if w.Status == workflow.New || w.Status == workflow.Scheduled {
		newStatus = workflow.Assigned
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET assignee_user_id = $2, assignee_team_id = $3, status = $4, updated_by = $5 WHERE id = $1`, t.table), id, in.AssigneeUserID, in.AssigneeTeamID, newStatus, actorOrNil(p)); err != nil {
		return nil, err
	}
	pl := assigneePayload(ctx, tx, in.AssigneeUserID, in.AssigneeTeamID)
	if in.Note != nil {
		pl["note"] = *in.Note
	}
	if w.Assignee.UserID != nil || w.Assignee.TeamID != nil {
		pl["previous"] = assigneePayload(ctx, tx, w.Assignee.UserID, w.Assignee.TeamID)
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: objectType, ObjectID: id, Action: audit.ActAssigned, Payload: pl})
	if newStatus != w.Status {
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: objectType, ObjectID: id, Action: audit.ActStatusChanged, From: string(w.Status), To: string(newStatus)})
	}
	label := fmt.Sprintf("%s to %s", w.Number, firstNonEmpty(pl["assignee_name"], pl["assignee_team_name"]))
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: "assigned", EntityType: objectType, EntityID: &id, EntityLabel: label, After: pl})
	if s.Jobs != nil {
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: objectType + ".assigned", OrganizationID: p.OrganizationID, PropertyID: &w.PropertyID, ObjectType: objectType, ObjectID: id, ObjectLabel: w.Number, ActorUserID: actorOrNil(p),
			Payload: map[string]any{"assignee_user_id": in.AssigneeUserID, "assignee_team_id": in.AssigneeTeamID, "previous_user_id": w.Assignee.UserID, "previous_team_id": w.Assignee.TeamID}})
		_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(p.OrganizationID, objectType, id))
	}
	nw, _, err := s.loadTx(ctx, tx, objectType, id, false)
	if err != nil {
		return nil, err
	}
	for _, h := range s.hooksFor(nw) {
		if err := h.AfterTransition(ctx, tx, nw, workflow.ActAssign, w.Status, newStatus); err != nil {
			return nil, err
		}
	}
	return nw, s.enrich(ctx, tx, nw, t, true)
}

func firstNonEmpty(vals ...any) string {
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func ptrEq(a, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// BulkAssign (FR-TSK-014 Should).
func (s *Service) BulkAssign(ctx context.Context, objectType string, idList []uuid.UUID, in AssignInput) (map[uuid.UUID]string, error) {
	results := map[uuid.UUID]string{}
	for _, id := range idList {
		_, err := s.Assign(ctx, objectType, id, in)
		if err != nil {
			results[id] = apperr.From(err).Detail
		} else {
			results[id] = "ok"
		}
	}
	return results, nil
}

// ---------- Transition (start/hold/resume/complete/close/reopen/cancel/schedule/unassign) ----------

func (s *Service) Transition(ctx context.Context, objectType string, id uuid.UUID, action string, in TransitionInput) (*WorkItem, error) {
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		w, err := s.TransitionTx(ctx, tx, objectType, id, action, in)
		if err != nil {
			return err
		}
		out = w
		return nil
	})
	return out, err
}

// TransitionTx: satu transisi state machine dalam transaksi (dipakai HTTP & sync).
func (s *Service) TransitionTx(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID, action string, in TransitionInput) (*WorkItem, error) {
	p := authctx.Must(ctx)
	w, t, err := s.loadTx(ctx, tx, objectType, id, true)
	if err != nil {
		return nil, err
	}
	if t.wf.IsTerminal(w.Status) && action != workflow.ActReopen {
		return nil, apperr.Conflict("OBJECT_TERMINAL", fmt.Sprintf("%s %s sudah %s", objectLabel(objectType), w.Number, workflow.Label(w.Status)))
	}
	tr, ok := t.wf.Find(w.Status, action)
	if !ok {
		return nil, apperr.InvalidTransition(fmt.Sprintf("%s %s cannot be %s from status %s", objectLabel(objectType), w.Number, action, w.Status))
	}
	if tr.Perm != "" && !canWI(ctx, tx, tr.Perm, w) {
		return nil, apperr.Forbidden("Memerlukan permission " + tr.Perm)
	}
	if tr.RequireReason && strings.TrimSpace(in.Reason) == "" {
		return nil, apperr.Validation("reason wajib untuk aksi "+action).WithField("reason", "wajib")
	}
	if tr.Guard != nil {
		gi := s.guardInput(ctx, tx, w, t, in.FromSync)
		if err := tr.Guard(gi); err != nil {
			code := "WORKFLOW_GUARD_FAILED"
			if !gi.IsAssignee && !gi.HasManage {
				return nil, apperr.Forbidden(err.Error())
			}
			return nil, apperr.Conflict(code, err.Error())
		}
	}
	if action == workflow.ActComplete {
		for _, h := range s.hooksFor(w) {
			if err := h.BeforeComplete(ctx, tx, w, in); err != nil {
				return nil, err
			}
		}
	}
	now := time.Now().UTC()
	sets := []string{"status = $2", "updated_by = $3"}
	args := []any{id, tr.To, actorOrNil(p)}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	switch action {
	case workflow.ActStart, workflow.ActResume:
		if w.StartedAt == nil {
			add("started_at", now)
		}
	case workflow.ActComplete:
		add("completed_at", now)
		if in.CompletionNotes != nil {
			add("completion_notes", *in.CompletionNotes)
		}
		if objectType == ObjWorkOrder {
			if in.Resolution != nil {
				add("resolution", *in.Resolution)
			}
			if in.ActualCost != nil {
				add("actual_cost_amount", in.ActualCost.Amount)
			}
			if in.ServiceCost != nil {
				add("service_cost_amount", in.ServiceCost.Amount)
			}
			if in.OtherCost != nil {
				add("other_cost_amount", in.OtherCost.Amount)
			}
			if in.PartsUsage != nil {
				add("parts_usage", *in.PartsUsage)
			}
		}
		add("is_overdue", false)
		add("evidence_incomplete", s.hasPendingEvidence(ctx, tx, objectType, id))
	case workflow.ActClose:
		add("closed_at", now)
	case workflow.ActReopen:
		add("completed_at", nil)
		add("closed_at", nil)
		add("reopen_count", ptrInt(w.ReopenCount)+1)
		add("last_reopened_at", now)
	case workflow.ActCancel:
		add("cancelled_at", now)
		add("is_overdue", false)
	case workflow.ActSchedule:
		if in.ScheduledStartAt != nil {
			add("scheduled_start_at", *in.ScheduledStartAt)
		}
		if in.DueAt != nil {
			add("due_at", *in.DueAt)
		}
	case workflow.ActUnassign:
		add("assignee_user_id", nil)
		add("assignee_team_id", nil)
		if _, err := tx.Exec(ctx, `UPDATE assignments SET is_current = false, unassigned_at = now() WHERE object_type = $1 AND object_id = $2 AND is_current`, objectType, id); err != nil {
			return nil, err
		}
	case workflow.ActSubmit:
		add("submitted_at", now)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s WHERE id = $1`, t.table, strings.Join(sets, ", ")), args...); err != nil {
		return nil, err
	}
	if action == workflow.ActComplete && objectType == ObjWorkOrder && in.ActualCost == nil {
		// actual cost = parts + jasa + lain-lain bila belum diisi eksplisit (PRD P1 v2 §26)
		if _, err := tx.Exec(ctx, `UPDATE work_orders SET actual_cost_amount = COALESCE(parts_cost_amount,0) + COALESCE(service_cost_amount,0) + COALESCE(other_cost_amount,0)
			WHERE id = $1 AND actual_cost_amount IS NULL AND (parts_cost_amount IS NOT NULL OR service_cost_amount IS NOT NULL OR other_cost_amount IS NOT NULL)`, id); err != nil {
			return nil, err
		}
	}
	if action == workflow.ActSubmit {
		// Draft → Open: SLA mulai berjalan sejak submit (PRD P1 v2 §23)
		if err := s.startSLATx(ctx, tx, objectType, id, w.PropertyID, w.Priority, w.DueAt == nil); err != nil {
			return nil, err
		}
	}
	// SLA tracking
	switch action {
	case workflow.ActStart:
		_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET responded_at = COALESCE(responded_at, $3) WHERE object_type = $1 AND object_id = $2`, objectType, id, now)
	case workflow.ActComplete:
		_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET resolved_at = $3 WHERE object_type = $1 AND object_id = $2`, objectType, id, now)
	case workflow.ActReopen:
		_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET resolved_at = NULL WHERE object_type = $1 AND object_id = $2`, objectType, id)
	case workflow.ActHold:
		_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET paused_at = $3 WHERE object_type = $1 AND object_id = $2`, objectType, id, now)
	case workflow.ActResume:
		_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET paused_minutes = paused_minutes + GREATEST(0, EXTRACT(EPOCH FROM ($3 - paused_at))/60)::int, paused_at = NULL WHERE object_type = $1 AND object_id = $2 AND paused_at IS NOT NULL`, objectType, id, now)
	}
	// activity + audit + event
	payload := map[string]any{"action": action}
	if in.Reason != "" {
		payload["reason"] = in.Reason
	}
	if in.CompletionNotes != nil {
		payload["completion_notes"] = *in.CompletionNotes
	}
	if in.GPSStatus != "" {
		payload["gps_status"] = in.GPSStatus
		if in.GPSLat != nil && in.GPSLng != nil {
			payload["gps_lat"] = *in.GPSLat
			payload["gps_lng"] = *in.GPSLng
		}
	}
	src := p.Source
	if in.FromSync {
		src = authctx.SourceSync
	}
	ctxAct := authctx.With(ctx, withSource(p, src))
	_ = audit.Record(ctxAct, tx, audit.Entry{ObjectType: objectType, ObjectID: id, Action: audit.ActStatusChanged, From: string(w.Status), To: string(tr.To), Payload: payload, ClientRecordedAt: in.ClientRecordedAt})
	if action == workflow.ActReopen {
		// PRD P0 v2 §10.4: Reopened tercatat eksplisit di activity history
		_ = audit.Record(ctxAct, tx, audit.Entry{ObjectType: objectType, ObjectID: id, Action: audit.ActReopened, From: string(w.Status), To: string(tr.To), Payload: map[string]any{"reason": in.Reason, "reopen_count": ptrInt(w.ReopenCount) + 1}})
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: objectType, EntityID: &id, EntityLabel: w.Number, Before: map[string]any{"status": w.Status}, After: map[string]any{"status": tr.To, "action": action, "reason": in.Reason}})
	if s.Jobs != nil {
		evType := objectType + "." + eventVerb(action)
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: evType, OrganizationID: p.OrganizationID, PropertyID: &w.PropertyID, ObjectType: objectType, ObjectID: id, ObjectLabel: w.Number, ActorUserID: actorOrNil(p),
			Payload: map[string]any{"from": w.Status, "to": tr.To, "reason": in.Reason, "assignee_user_id": w.Assignee.UserID, "assignee_team_id": w.Assignee.TeamID, "requester_user_id": w.RequesterUserID}})
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: objectType + ".status_changed", OrganizationID: p.OrganizationID, PropertyID: &w.PropertyID, ObjectType: objectType, ObjectID: id, ObjectLabel: w.Number, ActorUserID: actorOrNil(p),
			Payload: map[string]any{"from": w.Status, "to": tr.To, "action": action}})
		_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(p.OrganizationID, objectType, id))
	}
	nw, _, err := s.loadTx(ctx, tx, objectType, id, false)
	if err != nil {
		return nil, err
	}
	for _, h := range s.hooksFor(nw) {
		if err := h.AfterTransition(ctx, tx, nw, action, w.Status, tr.To); err != nil {
			return nil, err
		}
	}
	// PRD P1 v2.1 §5.15 / P2 v2.1 §10: rantai lintas tim — setelah semua hook (finding hook sudah menyelesaikan finding)
	if action == workflow.ActComplete || action == workflow.ActClose {
		if nw.FollowUpPurpose != nil {
			if err := s.resolveAncestorsAfterFollowUp(ctx, tx, nw); err != nil {
				return nil, err
			}
		}
		if err := s.chainProgressTx(ctx, tx, objectType, id, nw.Number, nw.Resolution); err != nil {
			return nil, err
		}
	}
	return nw, s.enrich(ctx, tx, nw, t, true)
}

func withSource(p *authctx.Principal, src authctx.Source) *authctx.Principal {
	cl := *p
	cl.Source = src
	return &cl
}

func eventVerb(action string) string {
	switch action {
	case workflow.ActStart, workflow.ActResume:
		return "started"
	case workflow.ActComplete:
		return "completed"
	case workflow.ActClose:
		return "closed"
	case workflow.ActReopen:
		return "reopened"
	case workflow.ActCancel:
		return "cancelled"
	case workflow.ActHold:
		return "held"
	case workflow.ActSchedule:
		return "scheduled"
	case workflow.ActUnassign:
		return "unassigned"
	case workflow.ActResolve:
		return "resolved"
	case workflow.ActAcknowledge:
		return "acknowledged"
	case workflow.ActSubmit:
		return "submitted"
	}
	return action
}

// syncActualCost: actual = parts + jasa + lain-lain (dipakai saat rincian biaya berubah tanpa actual eksplisit).
func syncActualCost(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE work_orders SET actual_cost_amount = COALESCE(parts_cost_amount,0) + COALESCE(service_cost_amount,0) + COALESCE(other_cost_amount,0) WHERE id = $1`, id)
	return err
}

// ---------- Escalation (PRD P1 v2 §4.2, §52) ----------

// Escalate: tandai task/WO tereskalasi (level +1), opsional naikkan prioritas & tujuan eskalasi; activity + audit + event
// `{object}.escalated` → notifikasi supervisor team & manager property.
func (s *Service) Escalate(ctx context.Context, objectType string, id uuid.UUID, in EscalateInput) (*WorkItem, error) {
	p := authctx.Must(ctx)
	var out *WorkItem
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		w, t, err := s.loadTx(ctx, tx, objectType, id, true)
		if err != nil {
			return err
		}
		if !canWI(ctx, tx, t.perm("escalate"), w) {
			return apperr.Forbidden("Memerlukan " + t.perm("escalate"))
		}
		if !escalatable(w.Status) {
			return apperr.InvalidTransition(fmt.Sprintf("%s %s tidak dapat dieskalasi dari status %s", objectLabel(objectType), w.Number, workflow.Label(w.Status)))
		}
		in.Reason = strings.TrimSpace(in.Reason)
		if in.Reason == "" {
			return apperr.Validation("reason wajib untuk eskalasi").WithField("reason", "wajib")
		}
		if in.EscalateToUser != nil {
			if err := s.validateRefs(ctx, tx, w.PropertyID, nil, nil, in.EscalateToUser, nil, nil); err != nil {
				return apperr.Validation("escalate_to_user_id tidak valid").WithField("escalate_to_user_id", "tidak valid")
			}
		}
		newPriority := w.Priority
		if in.RaisePriority {
			newPriority = map[string]string{"low": "medium", "medium": "high", "high": "critical", "critical": "critical"}[w.Priority]
		}
		level := w.EscalationLevel + 1
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET escalated_at = now(), escalation_level = $2, priority = $3, updated_by = $4 WHERE id = $1`, t.table), id, level, newPriority, actorOrNil(p)); err != nil {
			return err
		}
		_, _ = tx.Exec(ctx, `UPDATE sla_tracking SET escalated_at = COALESCE(escalated_at, now()) WHERE object_type = $1 AND object_id = $2`, objectType, id)
		pl := map[string]any{"reason": in.Reason, "level": level}
		if in.EscalateToUser != nil {
			pl["escalate_to"] = assigneePayload(ctx, tx, in.EscalateToUser, nil)
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: objectType, ObjectID: id, Action: audit.ActEscalated, Payload: pl})
		if newPriority != w.Priority {
			_ = audit.Record(ctx, tx, audit.Entry{ObjectType: objectType, ObjectID: id, Action: audit.ActPriorityChanged, From: w.Priority, To: newPriority})
			if err := s.applySLATx(ctx, tx, objectType, id, w.PropertyID, newPriority, w.CreatedAt); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: "escalated", EntityType: objectType, EntityID: &id, EntityLabel: w.Number, Before: map[string]any{"escalation_level": w.EscalationLevel, "priority": w.Priority}, After: map[string]any{"escalation_level": level, "priority": newPriority, "reason": in.Reason}})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: objectType + ".escalated", OrganizationID: p.OrganizationID, PropertyID: &w.PropertyID, ObjectType: objectType, ObjectID: id, ObjectLabel: w.Number, ActorUserID: actorOrNil(p),
				Payload: map[string]any{"reason": in.Reason, "level": level, "assignee_user_id": w.Assignee.UserID, "assignee_team_id": w.Assignee.TeamID, "escalate_to_user_id": in.EscalateToUser}})
			_ = s.Jobs.EnqueueTx(ctx, tx, searchIndexArgs(p.OrganizationID, objectType, id))
		}
		nw, _, err := s.loadTx(ctx, tx, objectType, id, false)
		if err != nil {
			return err
		}
		out = nw
		return s.enrich(ctx, tx, nw, t, true)
	})
	return out, err
}

func (s *Service) hasPendingEvidence(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID) bool {
	var n int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE object_type = $1 AND object_id = $2 AND status = 'pending' AND deleted_at IS NULL`, objectType, id).Scan(&n)
	return n > 0
}

func ptrInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// ---------- Assignments history ----------

func (s *Service) ListAssignments(ctx context.Context, objectType string, id uuid.UUID) ([]Assignment, error) {
	var out []Assignment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.ObjectAccess(ctx, tx, objectType, id, false); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT a.id, a.assignee_user_id, u.full_name, a.assignee_team_id, t.name, a.assigned_by, COALESCE(b.full_name,'System'), a.assigned_at, a.unassigned_at, a.is_current, a.note
			FROM assignments a LEFT JOIN users u ON u.id = a.assignee_user_id LEFT JOIN teams t ON t.id = a.assignee_team_id LEFT JOIN users b ON b.id = a.assigned_by
			WHERE a.object_type = $1 AND a.object_id = $2 ORDER BY a.assigned_at DESC`, objectType, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a Assignment
			if err := rows.Scan(&a.ID, &a.Assignee.UserID, &a.Assignee.UserName, &a.Assignee.TeamID, &a.Assignee.TeamName, &a.AssignedBy, &a.AssignedByName, &a.AssignedAt, &a.UnassignedAt, &a.IsCurrent, &a.Note); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	if out == nil {
		out = []Assignment{}
	}
	return out, err
}
