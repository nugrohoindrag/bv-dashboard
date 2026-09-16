package growth

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc          *Service
	IAM          *iam.Service
	CookieDomain string
	CookieSecure bool
}

// MountPublic: endpoint tanpa token untuk website & alur signup (Website PRD §25, §33–§34, §40).
func (h *Handler) MountPublic(r chi.Router) {
	r.Post("/public/signup", h.signup)
	r.Post("/public/signup/resend", h.resend)
	r.Post("/public/signup/verify", h.verify)
	r.Post("/public/demo-requests", h.demo)
	r.Post("/public/events", h.publicEvents)
	r.Get("/public/plans", h.plans)
}

// Mount: endpoint terautentikasi (onboarding, trial, event dashboard).
func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("platform.organizations.view")).Get("/onboarding", h.onboarding)
	r.With(req("platform.organizations.update")).Post("/onboarding/dismiss", h.dismiss)
	r.With(req("platform.organizations.update")).Post("/onboarding/sample-data", h.sampleData)
	r.With(req("platform.organizations.view")).Get("/trial", h.trial)
	r.With(req("platform.organizations.update")).Post("/trial/convert", h.convert)
	r.With(req("platform.organizations.update")).Post("/trial/cancel", h.cancel)
	r.Post("/growth/events", h.userEvents)
}

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	var in SignupInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.Svc.Signup(r.Context(), in, httpx.ClientIP(r), r.UserAgent())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, res)
}

func (h *Handler) resend(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.Resend(r.Context(), in.Email, httpx.ClientIP(r)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "sent"})
}

type verifyResp struct {
	iam.TokenPair
	RefreshToken   string    `json:"refresh_token,omitempty"`
	User           any       `json:"user"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Next           string    `json:"next"` // /onboarding
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.Svc.Verify(r.Context(), in.Token, httpx.ClientIP(r), r.UserAgent())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// web: refresh token di cookie HttpOnly (sama dengan login)
	iam.WriteRefreshCookie(w, res.Tokens.RefreshToken, res.Tokens.RefreshExpiresAt, h.CookieDomain, h.CookieSecure)
	out := verifyResp{TokenPair: *res.Tokens, User: iam.ToMe(res.Principal), OrganizationID: res.OrganizationID, Next: "/onboarding"}
	out.TokenPair.RefreshToken = ""
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) demo(w http.ResponseWriter, r *http.Request) {
	var in DemoRequestInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := h.Svc.RequestDemo(r.Context(), in, httpx.ClientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "received"})
}

type eventsReq struct {
	Events []EventInput `json:"events"`
}

func (h *Handler) publicEvents(w http.ResponseWriter, r *http.Request) {
	var in eventsReq
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	n, err := h.Svc.TrackPublic(r.Context(), in.Events, httpx.ClientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"accepted": n})
}

func (h *Handler) userEvents(w http.ResponseWriter, r *http.Request) {
	var in eventsReq
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	n, err := h.Svc.TrackUser(r.Context(), in.Events)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"accepted": n})
}

func (h *Handler) plans(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"plans": Plans, "trial_days": h.Svc.Cfg.TrialDays})
}

func (h *Handler) onboarding(w http.ResponseWriter, r *http.Request) {
	o, err := h.Svc.Checklist(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, o)
}

func (h *Handler) dismiss(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Dismissed bool `json:"dismissed"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.Dismiss(r.Context(), in.Dismissed); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) sampleData(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PropertyID uuid.UUID `json:"property_id"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	o, err := h.Svc.SampleData(r.Context(), in.PropertyID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, o)
}

func (h *Handler) trial(w http.ResponseWriter, r *http.Request) {
	t, err := h.Svc.Trial(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"trial": t, "plans": Plans})
}

func (h *Handler) convert(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PlanCode string `json:"plan_code"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.Svc.Convert(r.Context(), in.PlanCode)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, t)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.Svc.Cancel(r.Context(), in.Reason)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, t)
}
