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
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Priorities = httpx.QueryCSV(r, "priority")
	f.Categories = httpx.QueryCSV(r, "category")
	f.TenantID, _ = httpx.QueryUUID(r, "tenant_id")
	f.LocationID, _ = httpx.QueryUUID(r, "location_id")
	f.AssigneeID, _ = httpx.QueryUUID(r, "assignee_id")
	f.TeamID, _ = httpx.QueryUUID(r, "team_id")
	f.Mine = q.Get("mine") == "true"
	if v := q.Get("open"); v != "" {
		b := v == "true"
		f.Open = &b
	}
	if v := q.Get("sla_risk"); v != "" {
		b := v == "true"
		f.SLARisk = &b
	}
	f.CreatedFrom, _ = httpx.QueryTime(r, "created_from")
	f.CreatedTo, _ = httpx.QueryTime(r, "created_to")
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
