package tenantservice

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
	Ops *operations.Handler
}

func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("tenant.service_requests.view")).Get("/service-request-categories", h.categories)
	r.With(req("tenant.service_requests.view")).Get("/service-requests", h.list)
	r.With(req("tenant.service_requests.create")).Post("/service-requests", h.create)
	r.With(req("tenant.service_requests.view")).Get("/service-requests/{id}", h.get)
	// PRD P1 v2.1 P1-XMW-04: timeline lintas tim (Task → Finding → WO → tindak lanjut) — staf saja
	r.With(req("tenant.service_requests.view")).Get("/service-requests/{id}/chain", h.chain)
	// PRD P3 v2.1 P3-TRC-03: log komunikasi ke tenant (in-app, push + status kirim, pesan, WhatsApp manual)
	r.With(req("tenant.service_requests.view")).Get("/service-requests/{id}/communications", h.communications)
	// PRD P3 v2.1 P3-TSH-07: Recurring Issue Detection
	r.With(req("tenant_relation.recurring_issues.view")).Get("/recurring-issues", h.recurringIssues)
	r.With(req("tenant_relation.recurring_issues.view")).Get("/recurring-issues/{id}", h.recurringIssue)
	r.With(req("tenant_relation.recurring_issues.manage")).Post("/recurring-issues/{id}/acknowledge", h.recurringAct("acknowledge"))
	r.With(req("tenant_relation.recurring_issues.manage")).Post("/recurring-issues/{id}/resolve", h.recurringAct("resolve"))
	r.With(req("tenant.service_requests.update")).Patch("/service-requests/{id}", h.update)
	r.With(req("tenant.service_requests.assign")).Post("/service-requests/{id}/assign", h.assign)
	for _, a := range []string{workflow.ActAcknowledge, workflow.ActStart, workflow.ActWaitTenant, workflow.ActResolve, workflow.ActClose, workflow.ActReopen, workflow.ActCancel} {
		a := a
		r.Post("/service-requests/{id}/"+a, h.transition(a))
	}
	r.With(req("operations.work_orders.create")).Post("/service-requests/{id}/work-orders", h.woFromSR)
	r.With(req("operations.tasks.create")).Post("/service-requests/{id}/tasks", h.taskFromSR)
	r.With(req("platform.organizations.update")).Post("/service-requests/public-intake", h.enableIntake)
	// sub-resource bersama via operations handler (comments, activities, links, attachments generic)
	h.Ops.MountSubResources(r, "/service-requests", operations.ObjServiceRequest)
}

func (h *Handler) communications(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.Communications(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) recurringIssues(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, err := h.Svc.ListRecurringIssues(r.Context(), pid, httpx.QueryCSV(r, "status"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) recurringIssue(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.GetRecurringIssue(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) recurringAct(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var in struct {
			Note string `json:"note"`
		}
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		out, err := h.Svc.ActRecurringIssue(r.Context(), id, action, in.Note)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *Handler) categories(w http.ResponseWriter, r *http.Request) {
	var f CategoryFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.IncludeAll = r.URL.Query().Get("all") == "true"
	f.TenantOnly = r.URL.Query().Get("tenant_visible") == "true"
	items, err := h.Svc.ListCategories(r.Context(), f)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	q := r.URL.Query()
	var f Filter
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Priorities = httpx.QueryCSV(r, "priority")
	f.Categories = httpx.QueryCSV(r, "category")
	f.Sort = q.Get("sort")
	// PRD P0 v2 §17.2: filter tidak valid → 400 (bukan diabaikan); lokasi seragam building/floor/area/unit
	for _, x := range []struct {
		dst **uuid.UUID
		key string
	}{{&f.PropertyID, "property_id"}, {&f.TenantID, "tenant_id"}, {&f.AssigneeID, "assignee_id"}, {&f.TeamID, "team_id"}} {
		if *x.dst, err = httpx.QueryUUID(r, x.key); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	if f.LocationID, err = httpx.QueryLocation(r); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f.Mine = q.Get("mine") == "true"
	if v := q.Get("open"); v != "" {
		b := v == "true"
		f.Open = &b
	}
	if v := q.Get("sla_risk"); v != "" {
		b := v == "true"
		f.SLARisk = &b
	}
	// PRD P1 v2 §27.2, §38–§39
	f.RequestTypes = httpx.QueryCSV(r, "request_type")
	f.SLAStatus = httpx.QueryCSV(r, "sla_status")
	if v := q.Get("reopened"); v != "" {
		b := v == "true"
		f.Reopened = &b
	}
	f.CreatedFrom, _ = httpx.QueryTime(r, "created_from")
	f.CreatedTo, _ = httpx.QueryTime(r, "created_to")
	f.Channels = httpx.QueryCSV(r, "channel")
	f.ResolvedFrom, _ = httpx.QueryTime(r, "resolved_from")
	f.ResolvedTo, _ = httpx.QueryTime(r, "resolved_to")
	f.ReopenedFrom, _ = httpx.QueryTime(r, "reopened_from")
	if f.RecurringIssueID, err = httpx.QueryUUID(r, "recurring_issue_id"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f.Q = q.Get("q")
	items, next, err := h.Svc.List(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sr, err := h.Svc.Create(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, sr)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sr, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sr)
}

func (h *Handler) chain(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	cv, err := h.Svc.Chain(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cv)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in UpdateInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sr, err := h.Svc.Update(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sr)
}

func (h *Handler) assign(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in operations.AssignInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sr, err := h.Svc.Assign(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sr)
}

func (h *Handler) transition(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var in TransitionInput
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		sr, err := h.Svc.Transition(r.Context(), id, action, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, sr)
	}
}

func (h *Handler) woFromSR(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in operations.CreateWorkOrderInput
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	item, err := h.Svc.CreateWorkOrderFromSR(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) enableIntake(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PropertyID uuid.UUID `json:"property_id"`
		Enabled    bool      `json:"enabled"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	key, err := h.Svc.EnablePublicIntake(r.Context(), in.PropertyID, in.Enabled)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"intake_key": key, "enabled": in.Enabled})
}

func (h *Handler) taskFromSR(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in operations.CreateTaskInput
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	item, err := h.Svc.CreateTaskFromSR(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
