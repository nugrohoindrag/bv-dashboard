package sync

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

func (h *Handler) Mount(r chi.Router) {
	r.With(h.IAM.Require("sync.work_bundle.view")).Get("/sync/work-bundle", h.bundle)
	r.With(h.IAM.Require("sync.mutations.create")).Post("/sync/mutations", h.push)
	r.With(h.IAM.Require("sync.conflicts.view")).Get("/sync/conflicts", h.conflicts)
	r.With(h.IAM.Require("sync.conflicts.acknowledge")).Post("/sync/conflicts/{id}/acknowledge", h.ack)
}

func (h *Handler) bundle(w http.ResponseWriter, r *http.Request) {
	device := r.URL.Query().Get("device_id")
	if device == "" {
		device = r.Header.Get("X-Device-Id")
	}
	if device == "" {
		httpx.WriteError(w, r, apperr.Validation("device_id wajib"))
		return
	}
	b, err := h.Svc.WorkBundle(r.Context(), device, r.URL.Query().Get("since"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, b)
}

func (h *Handler) push(w http.ResponseWriter, r *http.Request) {
	var in PushInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Push(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) conflicts(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.ListConflicts(r.Context(), pid, r.URL.Query().Get("include_acknowledged") == "true")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) ack(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.AcknowledgeConflict(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
