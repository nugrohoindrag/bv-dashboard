package overview

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
	Eng *engineering.Service
}

// Endpoint TAD §5.16.
func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require("overview.dashboard.view")
	r.With(req).Get("/overview/today", h.today)
	r.With(req).Get("/overview/attention-required", h.attention)
	r.With(req).Get("/overview/todays-operations", h.todaysOps)
	r.With(req).Get("/overview/team-workload", h.workload)
	r.With(req).Get("/overview/pm-due", h.pmDue)
	r.With(req).Get("/overview/tenant-requests", h.tenantRequests)
	r.With(req).Get("/overview/building-state", h.buildingState)
}

func (h *Handler) today(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Today(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) attention(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, total, err := h.Svc.AttentionRequired(r.Context(), pid, r.URL.Query().Get("domain"), limit)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if items == nil {
		items = []AttentionItem{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": items, "total": total})
}

func (h *Handler) todaysOps(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.TodaysOperations(r.Context(), pid, r.URL.Query().Get("domain"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(out, nil))
}

func (h *Handler) workload(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.TeamWorkload(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(out, nil))
}

func (h *Handler) pmDue(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	days := 7
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 {
		days = v
	}
	items, next, err := h.Eng.ListSchedules(r.Context(), engineering.ScheduleFilter{PropertyID: pid, DueWithinDays: &days}, httpx.Page{Limit: 50})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) tenantRequests(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.TenantRequests(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) buildingState(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.BuildingState(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(out, nil))
}
