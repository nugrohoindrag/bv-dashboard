package waassist

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

// Mount: POST /whatsapp/compose (pratinjau), POST /whatsapp/send (catat + tautan wa.me), GET /whatsapp/logs.
// Permission dicek per object (objectPerm) di service; akun tenant ditolak.
func (h *Handler) Mount(r chi.Router) {
	staffOnly := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authctx.Must(r.Context()).IsTenant {
				httpx.WriteError(w, r, apperr.Forbidden("Hanya untuk staf"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	r.With(staffOnly).Post("/whatsapp/compose", h.compose)
	r.With(staffOnly).Post("/whatsapp/send", h.send)
	r.With(staffOnly).Get("/whatsapp/logs", h.logs)
}

func (h *Handler) compose(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Compose(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Send(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.QueryUUID(r, "object_id")
	if err != nil || id == nil {
		httpx.WriteError(w, r, apperr.Validation("object_id wajib"))
		return
	}
	items, err := h.Svc.Logs(r.Context(), r.URL.Query().Get("object_type"), *id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}
