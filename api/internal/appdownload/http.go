package appdownload

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

// MountAdmin: /admin/app-downloads — seluruhnya memerlukan role admin_internal (Website PRD §43; dicek server-side §19).
// Tidak ada DELETE: deaktivasi lebih disukai daripada penghapusan (§44).
func (h *Handler) MountAdmin(r chi.Router) {
	view := h.IAM.RequireInternalAdmin("platform.app_downloads.view")
	manage := h.IAM.RequireInternalAdmin("platform.app_downloads.manage")
	r.With(view).Get("/admin/app-downloads", h.list)
	r.With(manage).Post("/admin/app-downloads", h.create)
	r.With(view).Get("/admin/app-downloads/{id}", h.get)
	r.With(manage).Patch("/admin/app-downloads/{id}", h.update)
	r.With(manage).Post("/admin/app-downloads/{id}/activate", h.setStatus("active"))
	r.With(manage).Post("/admin/app-downloads/{id}/deactivate", h.setStatus("inactive"))
}

// MountPublic: /public/app-downloads — hanya tautan aktif (§22, §47); tanpa token.
func (h *Handler) MountPublic(r chi.Router) {
	r.Get("/public/app-downloads", h.public)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.List(r.Context())
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

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.Svc.Create(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, a)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in Input
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.Svc.Update(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a)
}

func (h *Handler) setStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		a, err := h.Svc.SetStatus(r.Context(), id, status)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, a)
	}
}

func (h *Handler) public(w http.ResponseWriter, r *http.Request) {
	apps, err := h.Svc.ListPublic(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"apps": apps})
}
