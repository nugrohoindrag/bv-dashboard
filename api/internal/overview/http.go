package overview

import (
	"context"
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
	// Building Management Overview: sensus penghuni (permission property.occupants.view dicek di service)
	r.With(req).Get("/overview/residents", h.residents)
	// PRD P1 v2 §44: dashboard "Limited" untuk worker — ringkasan pekerjaan milik sendiri
	r.With(h.IAM.RequireAny("operations.tasks.view", "operations.work_orders.view")).Get("/me/work-summary", h.myWork)
	// PRD P2 v2.1 GAP-P2-09: dashboard per domain (permission domain dicek di service)
	r.With(req).Get("/dashboards/engineering", h.domainDashboard(h.Svc.EngineeringDashboard))
	r.With(req).Get("/dashboards/security", h.domainDashboard(h.Svc.SecurityDashboard))
	r.With(req).Get("/dashboards/housekeeping", h.domainDashboard(h.Svc.HousekeepingDashboard))
	// PRD P4 v2.1 P4-FIN-01: dashboard Finance (permission billing.invoices.view dicek di service)
	r.With(h.IAM.RequireAny("overview.dashboard.view", "billing.invoices.view")).Get("/dashboards/finance", h.domainDashboard(h.Svc.FinanceDashboard))
}

func (h *Handler) domainDashboard(fn func(context.Context, DashboardParams) (*DomainDashboard, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in DashboardParams
		var err error
		if in.PropertyID, err = httpx.QueryUUID(r, "property_id"); err == nil {
			in.LocationID, err = httpx.QueryUUID(r, "location_id")
		}
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		in.From, in.To = r.URL.Query().Get("from"), r.URL.Query().Get("to")
		out, err := fn(r.Context(), in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
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
	out, err := h.Svc.TeamWorkload(r.Context(), pid, r.URL.Query().Get("group"))
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

func (h *Handler) residents(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Residents(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) myWork(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.MyWorkSummary(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
