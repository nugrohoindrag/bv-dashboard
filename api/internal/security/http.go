package security

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
	r.With(req("security.checkpoints.view")).Get("/checkpoints", h.listCheckpoints)
	r.With(req("security.checkpoints.create")).Post("/checkpoints", h.createCheckpoint)
	r.With(req("security.checkpoints.update")).Patch("/checkpoints/{id}", h.updateCheckpoint)

	r.With(req("security.patrol_routes.view")).Get("/patrol-routes", h.listRoutes)
	r.With(req("security.patrol_routes.create")).Post("/patrol-routes", h.createRoute)
	r.With(req("security.patrol_routes.view")).Get("/patrol-routes/{id}", h.getRoute)
	r.With(req("security.patrol_routes.update")).Patch("/patrol-routes/{id}", h.updateRoute)
	r.With(req("security.patrol.manage")).Post("/patrol-routes/{id}/patrol-tasks", h.adhocPatrol)

	r.With(req("security.patrol.view")).Get("/patrol-schedules", h.listSchedules)
	r.With(req("security.patrol.manage")).Post("/patrol-schedules", h.createSchedule)
	r.With(req("security.patrol.manage")).Patch("/patrol-schedules/{id}", h.updateSchedule)
	r.With(req("security.patrol.manage")).Post("/patrol-schedules/generate", h.generate)

	// Patrol tasks = tasks?type=patrol; endpoint khusus untuk scan
	r.With(req("security.patrol.view")).Get("/patrol-tasks", h.listPatrolTasks)
	r.With(req("security.patrol.view")).Get("/patrol-tasks/{id}", h.getPatrolTask)
	r.With(req("security.patrol.view")).Get("/patrol-tasks/{id}/scans", h.listScans)
	r.With(h.IAM.RequireAny("security.patrol.start", "security.patrol.manage")).Post("/patrol-tasks/{id}/scans", h.scan)
	r.With(h.IAM.RequireAny("security.patrol.start", "security.patrol.manage")).Post("/patrol-tasks/{id}/checkpoints/{cpId}/missed", h.missed)
}

func (h *Handler) listCheckpoints(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, err := h.Svc.ListCheckpoints(r.Context(), pid, r.URL.Query().Get("q"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createCheckpoint(w http.ResponseWriter, r *http.Request) {
	var in CheckpointInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	c, err := h.Svc.CreateCheckpoint(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, c)
}

func (h *Handler) updateCheckpoint(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in CheckpointInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	c, err := h.Svc.UpdateCheckpoint(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *Handler) listRoutes(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, err := h.Svc.ListRoutes(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createRoute(w http.ResponseWriter, r *http.Request) {
	var in RouteInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	rt, err := h.Svc.CreateRoute(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, rt)
}

func (h *Handler) getRoute(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	rt, err := h.Svc.GetRoute(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rt)
}

func (h *Handler) updateRoute(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in RouteInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	rt, err := h.Svc.UpdateRoute(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rt)
}

func (h *Handler) adhocPatrol(w http.ResponseWriter, r *http.Request) {
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
	item, err := h.Svc.CreateAdhocPatrol(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
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
	n, err := h.Svc.GeneratePatrolTasks(r.Context(), authctx.Must(r.Context()).OrganizationID, nil)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"generated": n})
}

func (h *Handler) listPatrolTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("type", "patrol")
	r.URL.RawQuery = q.Encode()
	h.Ops.ListHandler(operations.ObjTask)(w, r)
}

func (h *Handler) getPatrolTask(w http.ResponseWriter, r *http.Request) {
	h.Ops.GetHandler(operations.ObjTask)(w, r)
}

func (h *Handler) listScans(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.ListScans(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) scan(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in ScanInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.ScanCheckpoint(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) missed(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	cp, err := httpx.PathUUID(r, chi.URLParam, "cpId")
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
	items, err := h.Svc.MarkMissed(r.Context(), id, cp, in.Reason)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}
