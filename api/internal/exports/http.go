package exports

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
	r.With(h.IAM.Require("platform.exports.create")).Post("/exports", h.create)
	r.With(h.IAM.Require("platform.exports.create")).Get("/exports/{id}", h.get)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Resource string            `json:"resource"`
		Format   string            `json:"format"`
		Filters  map[string]string `json:"filters"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ex, err := h.Svc.Request(r.Context(), in.Resource, in.Format, in.Filters)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, ex)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ex, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ex)
}
