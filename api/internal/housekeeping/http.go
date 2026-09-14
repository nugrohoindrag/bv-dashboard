package housekeeping

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
	Ops *operations.Handler
}

func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("housekeeping.cleaning_schedules.view")).Get("/cleaning-schedules", h.listSchedules)
	r.With(req("housekeeping.cleaning_schedules.create")).Post("/cleaning-schedules", h.createSchedule)
	r.With(req("housekeeping.cleaning_schedules.update")).Patch("/cleaning-schedules/{id}", h.updateSchedule)
	r.With(req("housekeeping.cleaning_schedules.update")).Post("/cleaning-schedules/generate", h.generate)

	r.With(req("housekeeping.cleaning.view")).Get("/cleaning-tasks", h.listCleaningTasks)
	r.With(req("housekeeping.cleaning.manage")).Post("/cleaning-tasks", h.createAdhoc)
	r.With(req("housekeeping.cleaning.view")).Get("/cleaning-tasks/{id}", h.getCleaningTask)

	r.With(req("housekeeping.inspections.create")).Post("/housekeeping-inspections", h.createInspection)
	r.With(req("housekeeping.inspections.view")).Get("/housekeeping-inspections", h.listInspections)
}

func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, err := h.Svc.ListSchedules(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createSchedule(w http.ResponseWriter, r *http.Request) {
	var in ScheduleInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sc, err := h.Svc.CreateSchedule(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, sc)
}

func (h *Handler) updateSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in ScheduleInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sc, err := h.Svc.UpdateSchedule(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sc)
}

func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	n, err := h.Svc.GenerateCleaningTasks(r.Context(), authctx.Must(r.Context()).OrganizationID, nil)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"generated": n})
}

func (h *Handler) listCleaningTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("type", "cleaning")
	r.URL.RawQuery = q.Encode()
	h.Ops.ListHandler(operations.ObjTask)(w, r)
}

func (h *Handler) getCleaningTask(w http.ResponseWriter, r *http.Request) {
	h.Ops.GetHandler(operations.ObjTask)(w, r)
}

func (h *Handler) createAdhoc(w http.ResponseWriter, r *http.Request) {
	var in AdhocCleaningInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	item, err := h.Svc.CreateAdhocCleaning(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
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

func (h *Handler) listInspections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("type", "inspection")
	r.URL.RawQuery = q.Encode()
	h.Ops.ListHandler(operations.ObjTask)(w, r)
}
