package demo

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/httpx"
)

// Handler: Admin Internal → Demo Data (§29–§31). Hanya role admin_internal pada organization internal
// (RequireInternalAdmin, dicek server-side); role lain 403.
type Handler struct {
	Svc *Service
	IAM *iam.Service
}

func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.RequireInternalAdmin
	r.With(req("platform.demo_data.view")).Get("/admin/demo", h.status)
	r.With(req("platform.demo_data.manage")).Post("/admin/demo/seed", h.seed)
	r.With(req("platform.demo_data.manage")).Post("/admin/demo/reset", h.reset)
	r.With(req("platform.demo_data.view")).Post("/admin/demo/verify", h.verify)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.Status(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type seedReq struct {
	Profiles []string `json:"profiles"` // kosong = seluruh profile
}

func (h *Handler) seed(w http.ResponseWriter, r *http.Request) {
	var in seedReq
	if r.ContentLength != 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	out, err := h.Svc.Seed(r.Context(), in.Profiles)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type resetReq struct {
	Profiles []string `json:"profiles"` // kosong = seluruh environment; sebagian = profile lain di-seed ulang
	Reseed   bool     `json:"reseed"`   // true = "Reset & Reseed": seed ulang profile yang dihapus
	Confirm  string   `json:"confirm"`  // wajib "RESET" (guard §39)
}

func (h *Handler) reset(w http.ResponseWriter, r *http.Request) {
	var in resetReq
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if in.Confirm != "RESET" {
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{"code": "CONFIRM_REQUIRED", "detail": "confirm harus \"RESET\""})
		return
	}
	out, err := h.Svc.Reset(r.Context(), in.Profiles)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if in.Reseed {
		profiles := in.Profiles
		if len(profiles) == 0 {
			profiles = Profiles
		}
		if _, err := h.Svc.Seed(r.Context(), profiles); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out.Reseeded = append(out.Reseeded, profiles...)
		out.State, _ = h.Svc.Status(r.Context())
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.Verify(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"report": out, "text": out.Text()})
}
