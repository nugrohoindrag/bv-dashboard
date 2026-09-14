package notification

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
	req := h.IAM.Require("notification.inbox.view")
	r.With(req).Get("/notifications", h.list)
	r.With(req).Post("/notifications/{id}/read", h.read)
	r.With(req).Post("/notifications/read-all", h.readAll)
	r.With(req).Get("/notifications/preferences", h.prefs)
	r.With(req).Put("/notifications/preferences", h.setPref)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, next, unread, err := h.Svc.List(r.Context(), r.URL.Query().Get("unread") == "true", page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": items, "next_cursor": next, "unread_count": unread})
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.MarkRead(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) readAll(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.MarkAllRead(r.Context()); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) prefs(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListPreferences(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) setPref(w http.ResponseWriter, r *http.Request) {
	var in Preference
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.SetPreference(r.Context(), in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
