package attachments

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
	r.With(h.IAM.Require("operations.attachments.create")).Post("/attachments/presign", h.presign)
	r.With(h.IAM.Require("operations.attachments.create")).Post("/attachments/{id}/confirm", h.confirm)
	r.Get("/attachments", h.list)
	r.Get("/attachments/{id}", h.get)
	r.Delete("/attachments/{id}", h.del)
}

func (h *Handler) presign(w http.ResponseWriter, r *http.Request) {
	var in PresignInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Presign(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in ConfirmInput
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	a, err := h.Svc.Confirm(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.Svc.fillURLs(r.Context(), a)
	httpx.WriteJSON(w, http.StatusOK, a)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ot := r.URL.Query().Get("object_type")
	oid, err := httpx.QueryUUID(r, "object_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if ot == "" || oid == nil {
		httpx.WriteError(w, r, apperr.Validation("object_type dan object_id wajib"))
		return
	}
	items, err := h.Svc.ListForObject(r.Context(), ot, *oid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a)
}

func (h *Handler) del(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.Delete(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
