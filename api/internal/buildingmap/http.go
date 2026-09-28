package buildingmap

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

// Mount: /floor-plans (PRD P1 v2 §7) & /occupancy (§8).
func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("property.floor_plans.view")).Get("/floor-plans", h.list)
	r.With(req("property.floor_plans.create")).Post("/floor-plans", h.create)
	r.With(req("property.floor_plans.view")).Get("/floor-plans/{id}", h.get)
	r.With(req("property.floor_plans.update")).Patch("/floor-plans/{id}", h.update)
	r.With(req("property.floor_plans.delete")).Delete("/floor-plans/{id}", h.delete)
	r.With(req("property.floor_plans.update")).Post("/floor-plans/{id}/markers", h.upsertMarker)
	r.With(req("property.floor_plans.update")).Patch("/floor-plans/{id}/markers/{markerId}", h.updateMarker)
	r.With(req("property.floor_plans.update")).Delete("/floor-plans/{id}/markers/{markerId}", h.deleteMarker)
	r.With(req("property.floor_plans.view")).Get("/floor-plan-markers", h.findTarget)

	r.With(req("property.occupancy.view")).Get("/occupancy/summary", h.occupancySummary)
	r.With(req("property.occupancy.view")).Get("/occupancy/units", h.occupancyUnits)
}

func id(r *http.Request, name string) (uuid.UUID, error) {
	return httpx.PathUUID(r, chi.URLParam, name)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	var f Filter
	var err error
	if f.PropertyID, err = httpx.QueryUUID(r, "property_id"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if f.LocationID, err = httpx.QueryLocation(r); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if v := r.URL.Query().Get("active"); v != "" {
		b := v == "true"
		f.Active = &b
	}
	out, err := h.Svc.List(r.Context(), f)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(out, nil))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in FloorPlanInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Create(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	pid, err := id(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Get(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	pid, err := id(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in FloorPlanInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Update(r.Context(), pid, in, httpx.IfMatchVersion(r)) // optimistic locking (PRD P1 v2.1 P1-BLD-06)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	pid, err := id(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.Delete(r.Context(), pid); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) upsertMarker(w http.ResponseWriter, r *http.Request) {
	pid, err := id(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in MarkerInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpsertMarker(r.Context(), pid, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) updateMarker(w http.ResponseWriter, r *http.Request) {
	pid, err := id(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	mid, err := id(r, "markerId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in MarkerInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateMarker(r.Context(), pid, mid, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteMarker(w http.ResponseWriter, r *http.Request) {
	pid, err := id(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	mid, err := id(r, "markerId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.DeleteMarker(r.Context(), pid, mid); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) findTarget(w http.ResponseWriter, r *http.Request) {
	tt := r.URL.Query().Get("target_type")
	if tt != "location" && tt != "facility" && tt != "asset" {
		httpx.WriteError(w, r, apperr.Validation("target_type harus location|facility|asset").WithField("target_type", "wajib"))
		return
	}
	tid, err := httpx.QueryUUID(r, "target_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if tid == nil {
		httpx.WriteError(w, r, apperr.Validation("target_id wajib").WithField("target_id", "wajib"))
		return
	}
	out, err := h.Svc.FindTarget(r.Context(), tt, *tid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(out, nil))
}

func occFilter(r *http.Request) (OccupancyFilter, error) {
	var f OccupancyFilter
	var err error
	if f.PropertyID, err = httpx.QueryUUID(r, "property_id"); err != nil {
		return f, err
	}
	if f.LocationID, err = httpx.QueryLocation(r); err != nil {
		return f, err
	}
	f.Statuses = httpx.QueryCSV(r, "status")
	for _, s := range f.Statuses {
		switch s {
		case "vacant", "occupied", "reserved", "inactive":
		default:
			return f, apperr.Validation("status harus vacant|occupied|reserved|inactive").WithField("status", "tidak valid")
		}
	}
	f.UnitTypes = httpx.QueryCSV(r, "unit_type")
	f.Q = r.URL.Query().Get("q")
	return f, nil
}

func (h *Handler) occupancySummary(w http.ResponseWriter, r *http.Request) {
	f, err := occFilter(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.OccupancySummary(r.Context(), f)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) occupancyUnits(w http.ResponseWriter, r *http.Request) {
	f, err := occFilter(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := h.Svc.OccupancyUnits(r.Context(), f, limit)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(out, nil))
}
