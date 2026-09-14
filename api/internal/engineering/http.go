package engineering

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("engineering.maintenance_plans.view")).Get("/maintenance-plans", h.listPlans)
	r.With(req("engineering.maintenance_plans.create")).Post("/maintenance-plans", h.createPlan)
	r.With(req("engineering.maintenance_plans.view")).Get("/maintenance-plans/{id}", h.getPlan)
	r.With(req("engineering.maintenance_plans.update")).Patch("/maintenance-plans/{id}", h.updatePlan)
	r.With(req("engineering.maintenance_plans.publish")).Post("/maintenance-plans/{id}/publish", h.setPlanStatus("published"))
	r.With(req("engineering.maintenance_plans.archive")).Post("/maintenance-plans/{id}/archive", h.setPlanStatus("archived"))
	r.With(req("engineering.maintenance_plans.publish")).Post("/maintenance-plans/{id}/generate", h.generate)

	r.With(req("engineering.maintenance_schedules.view")).Get("/maintenance-schedules", h.listSchedules)
	r.With(req("engineering.maintenance_schedules.skip")).Post("/maintenance-schedules/{id}/skip", h.skip)
	r.With(req("engineering.maintenance_schedules.skip")).Post("/maintenance-schedules/run-due", h.runDue)

	r.With(req("engineering.inspections.create")).Post("/inspections", h.createInspection)
}

func (h *Handler) listPlans(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	aid, _ := httpx.QueryUUID(r, "asset_id")
	items, err := h.Svc.ListPlans(r.Context(), pid, aid, r.URL.Query().Get("status"), r.URL.Query().Get("q"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createPlan(w http.ResponseWriter, r *http.Request) {
	var in PlanInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	m, err := h.Svc.CreatePlan(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, m)
}

func (h *Handler) getPlan(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	m, err := h.Svc.GetPlan(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, m)
}

func (h *Handler) updatePlan(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in PlanInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	m, err := h.Svc.UpdatePlan(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, m)
}

func (h *Handler) setPlanStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		m, err := h.Svc.SetPlanStatus(r.Context(), id, status)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, m)
	}
}

func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principalOrg(r)
	n, err := h.Svc.GenerateSchedules(r.Context(), p, &id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"generated": n})
}

func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f ScheduleFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.PlanID, _ = httpx.QueryUUID(r, "plan_id")
	f.AssetID, _ = httpx.QueryUUID(r, "asset_id")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.From, _ = httpx.QueryTime(r, "due_from")
	f.To, _ = httpx.QueryTime(r, "due_to")
	if v := r.URL.Query().Get("due_within_days"); v != "" {
		n := 0
		for _, c := range v {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		f.DueWithinDays = &n
	}
	items, next, err := h.Svc.ListSchedules(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) skip(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.SkipSchedule(r.Context(), id, in.Reason); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) runDue(w http.ResponseWriter, r *http.Request) {
	n, err := h.Svc.CreateDueWorkOrders(r.Context(), principalOrg(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"created": n})
}

func (h *Handler) createInspection(w http.ResponseWriter, r *http.Request) {
	var in CreateInspectionInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	item, err := h.Svc.CreateInspection(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
