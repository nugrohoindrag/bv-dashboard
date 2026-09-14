package operations

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	// ----- Tasks -----
	r.With(req("operations.tasks.view")).Get("/tasks", h.list(ObjTask))
	r.With(req("operations.tasks.create")).Post("/tasks", h.createTask)
	r.With(req("operations.tasks.assign")).Post("/tasks/bulk-assign", h.bulkAssign(ObjTask))
	r.With(req("operations.tasks.view")).Get("/tasks/by-number/{number}", h.getByNumber(ObjTask))
	r.With(req("operations.tasks.view")).Get("/tasks/{id}", h.get(ObjTask))
	r.With(req("operations.tasks.update")).Patch("/tasks/{id}", h.update(ObjTask))
	r.With(req("operations.tasks.assign")).Post("/tasks/{id}/assign", h.assign(ObjTask))
	r.With(req("operations.tasks.view")).Get("/tasks/{id}/assignments", h.assignments(ObjTask))
	h.mountTransitions(r, "/tasks", ObjTask)
	h.mountSub(r, "/tasks", ObjTask)

	// ----- Work Orders -----
	r.With(req("operations.work_orders.view")).Get("/work-orders", h.list(ObjWorkOrder))
	r.With(req("operations.work_orders.create")).Post("/work-orders", h.createWorkOrder)
	r.With(req("operations.work_orders.assign")).Post("/work-orders/bulk-assign", h.bulkAssign(ObjWorkOrder))
	r.With(req("operations.work_orders.view")).Get("/work-orders/by-number/{number}", h.getByNumber(ObjWorkOrder))
	r.With(req("operations.work_orders.view")).Get("/work-orders/{id}", h.get(ObjWorkOrder))
	r.With(req("operations.work_orders.update")).Patch("/work-orders/{id}", h.update(ObjWorkOrder))
	r.With(req("operations.work_orders.assign")).Post("/work-orders/{id}/assign", h.assign(ObjWorkOrder))
	r.With(req("operations.work_orders.view")).Get("/work-orders/{id}/assignments", h.assignments(ObjWorkOrder))
	h.mountTransitions(r, "/work-orders", ObjWorkOrder)
	h.mountSub(r, "/work-orders", ObjWorkOrder)

	// ----- Incidents -----
	r.With(req("operations.incidents.view")).Get("/incidents", h.listIncidents)
	r.With(h.IAM.RequireAny("operations.incidents.create", "security.incidents.create")).Post("/incidents", h.createIncident)
	r.With(req("operations.incidents.view")).Get("/incidents/{id}", h.getIncident)
	r.With(req("operations.incidents.update")).Patch("/incidents/{id}", h.updateIncident)
	r.With(req("operations.incidents.assign")).Post("/incidents/{id}/assign", h.assignIncident)
	for _, a := range []string{workflow.ActStart, workflow.ActResolve, workflow.ActClose, workflow.ActReopen, workflow.ActCancel} {
		a := a
		r.Post("/incidents/{id}/"+a, h.incidentTransition(a))
	}
	r.With(req("operations.work_orders.create")).Post("/incidents/{id}/work-orders", h.woFromIncident)
	h.mountSub(r, "/incidents", ObjIncident)

	// ----- Findings -----
	r.With(req("operations.findings.view")).Get("/findings", h.listFindings)
	r.With(req("operations.findings.create")).Post("/findings", h.createFinding)
	r.With(req("operations.findings.view")).Get("/findings/{id}", h.getFinding)
	for _, a := range []string{workflow.ActStart, workflow.ActResolve, workflow.ActClose, workflow.ActReopen} {
		a := a
		r.Post("/findings/{id}/"+a, h.findingTransition(a))
	}
	r.With(req("operations.work_orders.create")).Post("/findings/{id}/work-orders", h.woFromFinding)
	r.With(req("operations.tasks.create")).Post("/findings/{id}/tasks", h.taskFromFinding)
	r.With(h.IAM.RequireAny("operations.incidents.create", "security.incidents.create")).Post("/findings/{id}/incidents", h.incidentFromFinding)
	h.mountSub(r, "/findings", ObjFinding)

	// ----- Checklist templates & runs -----
	r.With(req("operations.checklists.view")).Get("/checklist-templates", h.listTemplates)
	r.With(req("operations.checklists.create")).Post("/checklist-templates", h.createTemplate)
	r.With(req("operations.checklists.view")).Get("/checklist-templates/{id}", h.getTemplate)
	r.With(req("operations.checklists.update")).Patch("/checklist-templates/{id}", h.updateTemplate)
	r.With(req("operations.checklists.publish")).Post("/checklist-templates/{id}/publish", h.setTemplateStatus("published"))
	r.With(req("operations.checklists.archive")).Post("/checklist-templates/{id}/archive", h.setTemplateStatus("archived"))
	r.Get("/checklist-runs/{id}", h.getRun)
	r.Post("/checklist-runs/{id}/items/{itemId}/answer", h.answerItem)
	r.Post("/checklist-run-items/{itemId}/answer", h.answerItem)

	// ----- SLA -----
	r.With(req("operations.sla_policies.view")).Get("/sla-policies", h.listSLA)
	r.With(h.IAM.RequireAny("operations.sla_policies.create", "operations.sla_policies.update")).Post("/sla-policies", h.upsertSLA)

	// ----- Generic links / activities -----
	r.Post("/links", h.createLink)
	r.Get("/activities", h.listActivities)
}

func (h *Handler) mountTransitions(r chi.Router, base, objectType string) {
	for _, a := range []string{workflow.ActSchedule, workflow.ActStart, workflow.ActHold, workflow.ActResume, workflow.ActComplete, workflow.ActClose, workflow.ActReopen, workflow.ActCancel, workflow.ActUnassign} {
		a := a
		r.Post(base+"/{id}/"+a, h.transition(objectType, a))
	}
}

// mountSub: sub-resource bersama (checklist-runs, comments, attachments handled elsewhere, activities, links).
func (h *Handler) mountSub(r chi.Router, base, objectType string) {
	r.Get(base+"/{id}/checklist-runs", h.listRuns(objectType))
	r.Post(base+"/{id}/checklist-runs", h.attachChecklist(objectType))
	r.Get(base+"/{id}/comments", h.listComments(objectType))
	r.With(h.IAM.Require("operations.comments.create")).Post(base+"/{id}/comments", h.addComment(objectType))
	r.Get(base+"/{id}/activities", h.activities(objectType))
	r.Get(base+"/{id}/links", h.links(objectType))
	r.Post(base+"/{id}/links", h.addLink(objectType))
}

// ---------- helpers ----------

func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return uuid.Nil, false
	}
	return id, true
}

func parseListFilter(r *http.Request) (ListFilter, error) {
	q := r.URL.Query()
	var f ListFilter
	var err error
	if f.PropertyID, err = httpx.QueryUUID(r, "property_id"); err != nil {
		return f, err
	}
	f.Types = httpx.QueryCSV(r, "type")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Priorities = httpx.QueryCSV(r, "priority")
	if f.LocationID, err = httpx.QueryUUID(r, "location_id"); err != nil {
		return f, err
	}
	if f.AssigneeID, err = httpx.QueryUUID(r, "assignee_id"); err != nil {
		return f, err
	}
	if f.TeamID, err = httpx.QueryUUID(r, "team_id"); err != nil {
		return f, err
	}
	if f.AssetID, err = httpx.QueryUUID(r, "asset_id"); err != nil {
		return f, err
	}
	if f.SourceID, err = httpx.QueryUUID(r, "source_id"); err != nil {
		return f, err
	}
	f.SourceType = q.Get("source_type")
	f.Mine = q.Get("mine") == "true"
	if v := q.Get("overdue"); v != "" {
		b := v == "true"
		f.Overdue = &b
	}
	if v := q.Get("sla_risk"); v != "" {
		b := v == "true"
		f.SLARisk = &b
	}
	if v := q.Get("open"); v != "" {
		b := v == "true"
		f.Open = &b
	}
	if f.DueFrom, err = httpx.QueryTime(r, "due_from"); err != nil {
		return f, err
	}
	if f.DueTo, err = httpx.QueryTime(r, "due_to"); err != nil {
		return f, err
	}
	if f.CreatedFrom, err = httpx.QueryTime(r, "created_from"); err != nil {
		return f, err
	}
	if f.CreatedTo, err = httpx.QueryTime(r, "created_to"); err != nil {
		return f, err
	}
	if f.ScheduledOn, err = httpx.QueryTime(r, "scheduled_on"); err != nil {
		return f, err
	}
	f.Q = q.Get("q")
	f.Sort = q.Get("sort")
	return f, nil
}

func (h *Handler) list(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := httpx.ParsePage(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		f, err := parseListFilter(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		items, next, err := h.Svc.List(r.Context(), objectType, f, page)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
	}
}

func (h *Handler) get(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		item, err := h.Svc.Get(r.Context(), objectType, id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+itoa(item.Version)+`"`)
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}

func (h *Handler) getByNumber(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := h.Svc.GetByNumber(r.Context(), objectType, strings.ToUpper(chi.URLParam(r, "number")))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	var in CreateTaskInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	item, err := h.Svc.CreateTask(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) createWorkOrder(w http.ResponseWriter, r *http.Request) {
	var in CreateWorkOrderInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	item, err := h.Svc.CreateWorkOrder(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) update(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in UpdateWorkItemInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		item, err := h.Svc.Update(r.Context(), objectType, id, in, httpx.IfMatchVersion(r))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}

func (h *Handler) assign(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in AssignInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		item, err := h.Svc.Assign(r.Context(), objectType, id, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}

func (h *Handler) bulkAssign(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs []uuid.UUID `json:"ids"`
			AssignInput
		}
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if len(in.IDs) == 0 || len(in.IDs) > 100 {
			httpx.WriteError(w, r, apperr.Validation("ids 1..100"))
			return
		}
		res, err := h.Svc.BulkAssign(r.Context(), objectType, in.IDs, in.AssignInput)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": res})
	}
}

func (h *Handler) assignments(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		items, err := h.Svc.ListAssignments(r.Context(), objectType, id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
	}
}

func (h *Handler) transition(objectType, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in TransitionInput
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		item, err := h.Svc.Transition(r.Context(), objectType, id, action, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}

// ----- sub-resources -----

func (h *Handler) listRuns(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		runs, err := h.Svc.ListRuns(r.Context(), objectType, id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, httpx.NewList(runs, nil))
	}
}

func (h *Handler) attachChecklist(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in struct {
			TemplateID uuid.UUID `json:"template_id"`
		}
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		run, err := h.Svc.AttachChecklist(r.Context(), objectType, id, in.TemplateID)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, run)
	}
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	run, err := h.Svc.GetRun(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, run)
}

func (h *Handler) answerItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := httpx.PathUUID(r, chi.URLParam, "itemId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in AnswerInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	run, err := h.Svc.AnswerItem(r.Context(), itemID, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, run)
}

func (h *Handler) listComments(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		items, err := h.Svc.ListComments(r.Context(), objectType, id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
	}
}

func (h *Handler) addComment(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in struct {
			Body             string     `json:"body"`
			ClientCommentID  *string    `json:"client_comment_id"`
			ClientRecordedAt *time.Time `json:"client_recorded_at"`
		}
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		c, err := h.Svc.AddComment(r.Context(), objectType, id, in.Body, in.ClientCommentID, in.ClientRecordedAt, false)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, c)
	}
}

func (h *Handler) activities(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var items []audit.Activity
		err := h.Svc.DB.WithTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
			if err := h.Svc.ObjectAccess(ctx, tx, objectType, id, false); err != nil {
				return err
			}
			var err error
			items, err = audit.ListActivities(ctx, tx, objectType, id, 300)
			return err
		})
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
	}
}

func (h *Handler) listActivities(w http.ResponseWriter, r *http.Request) {
	ot := r.URL.Query().Get("object_type")
	oid, err := httpx.QueryUUID(r, "object_id")
	if err != nil || ot == "" || oid == nil {
		httpx.WriteError(w, r, apperr.Validation("object_type dan object_id wajib"))
		return
	}
	var items []audit.Activity
	err = h.Svc.DB.WithTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if err := h.Svc.ObjectAccess(ctx, tx, ot, *oid, false); err != nil {
			return err
		}
		var err error
		items, err = audit.ListActivities(ctx, tx, ot, *oid, 300)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) links(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var items []ObjectLink
		err := h.Svc.DB.WithTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
			if err := h.Svc.ObjectAccess(ctx, tx, objectType, id, false); err != nil {
				return err
			}
			var err error
			items, err = h.Svc.listLinksTx(ctx, tx, objectType, id)
			return err
		})
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
	}
}

func (h *Handler) addLink(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in LinkRef
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		items, err := h.Svc.Link(r.Context(), objectType, id, in.ObjectType, in.ObjectID, in.LinkType)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, httpx.NewList(items, nil))
	}
}

func (h *Handler) createLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FromType string    `json:"from_type"`
		FromID   uuid.UUID `json:"from_id"`
		ToType   string    `json:"to_type"`
		ToID     uuid.UUID `json:"to_id"`
		LinkType string    `json:"link_type"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.Link(r.Context(), in.FromType, in.FromID, in.ToType, in.ToID, in.LinkType)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, httpx.NewList(items, nil))
}

// ----- incidents -----

func (h *Handler) listIncidents(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f IncidentFilter
	if f.PropertyID, err = httpx.QueryUUID(r, "property_id"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Severities = httpx.QueryCSV(r, "severity")
	f.Categories = httpx.QueryCSV(r, "category")
	f.LocationID, _ = httpx.QueryUUID(r, "location_id")
	f.AssigneeID, _ = httpx.QueryUUID(r, "assignee_id")
	f.TeamID, _ = httpx.QueryUUID(r, "team_id")
	if v := r.URL.Query().Get("open"); v != "" {
		b := v == "true"
		f.Open = &b
	}
	f.From, _ = httpx.QueryTime(r, "created_from")
	f.To, _ = httpx.QueryTime(r, "created_to")
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.ListIncidents(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) createIncident(w http.ResponseWriter, r *http.Request) {
	var in CreateIncidentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	inc, err := h.Svc.CreateIncident(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, inc)
}

func (h *Handler) getIncident(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	inc, err := h.Svc.GetIncident(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, inc)
}

func (h *Handler) updateIncident(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in UpdateIncidentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	inc, err := h.Svc.UpdateIncident(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, inc)
}

func (h *Handler) assignIncident(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in AssignInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	inc, err := h.Svc.AssignIncident(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, inc)
}

func (h *Handler) incidentTransition(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in IncidentTransitionInput
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		inc, err := h.Svc.TransitionIncident(r.Context(), id, action, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, inc)
	}
}

func (h *Handler) woFromIncident(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in CreateWorkOrderInput
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	item, err := h.Svc.CreateWorkOrderFromIncident(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

// ----- findings -----

func (h *Handler) listFindings(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f FindingFilter
	if f.PropertyID, err = httpx.QueryUUID(r, "property_id"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Severities = httpx.QueryCSV(r, "severity")
	f.Types = httpx.QueryCSV(r, "type")
	f.LocationID, _ = httpx.QueryUUID(r, "location_id")
	f.SourceType = r.URL.Query().Get("source_type")
	f.SourceID, _ = httpx.QueryUUID(r, "source_id")
	f.Unresolved = r.URL.Query().Get("unresolved") == "true"
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.ListFindings(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) createFinding(w http.ResponseWriter, r *http.Request) {
	var in CreateFindingInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f, err := h.Svc.CreateFinding(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, f)
}

func (h *Handler) getFinding(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	f, err := h.Svc.GetFinding(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, f)
}

func (h *Handler) findingTransition(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in FindingTransitionInput
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		f, err := h.Svc.TransitionFinding(r.Context(), id, action, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, f)
	}
}

func (h *Handler) woFromFinding(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in CreateWorkOrderInput
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	item, err := h.Svc.CreateWorkOrderFromFinding(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) taskFromFinding(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in CreateTaskInput
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	item, err := h.Svc.CreateTaskFromFinding(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) incidentFromFinding(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in CreateIncidentInput
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	inc, err := h.Svc.CreateIncidentFromFinding(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, inc)
}

// ----- checklist templates -----

func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, err := h.Svc.ListChecklistTemplates(r.Context(), q.Get("status"), q.Get("domain"), q.Get("applies_to"), q.Get("q"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createTemplate(w http.ResponseWriter, r *http.Request) {
	var in ChecklistTemplateInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.Svc.CreateChecklistTemplate(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, t)
}

func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, err := h.Svc.GetChecklistTemplate(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, t)
}

func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in ChecklistTemplateInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.Svc.UpdateChecklistTemplate(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, t)
}

func (h *Handler) setTemplateStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		t, err := h.Svc.SetChecklistTemplateStatus(r.Context(), id, status)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, t)
	}
}

// ----- SLA -----

func (h *Handler) listSLA(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.ListSLAPolicies(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) upsertSLA(w http.ResponseWriter, r *http.Request) {
	var in SLAPolicyInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pol, err := h.Svc.UpsertSLAPolicy(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, pol)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ListHandler / GetHandler diekspor untuk modul domain (patrol-tasks, cleaning-tasks alias).
func (h *Handler) ListHandler(objectType string) http.HandlerFunc { return h.list(objectType) }
func (h *Handler) GetHandler(objectType string) http.HandlerFunc  { return h.get(objectType) }
