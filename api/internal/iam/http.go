package iam

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/httpx"
)

const refreshCookie = "bv_refresh"

type Handler struct {
	Svc          *Service
	CookieDomain string
	CookieSecure bool
}

// MountPublic: /auth/* (tanpa token).
func (h *Handler) MountPublic(r chi.Router) {
	r.Post("/auth/login", h.login)
	r.Post("/auth/refresh", h.refresh)
	r.Post("/auth/logout", h.logout)
}

// MountProtected: /me, /users, /roles, /permissions, /teams (dengan token).
func (h *Handler) MountProtected(r chi.Router) {
	r.Get("/me", h.me)
	r.Get("/me/permissions", h.mePermissions)
	r.Post("/me/password", h.changePassword)
	r.Post("/me/devices", h.registerDevice)
	r.Delete("/me/devices/{token}", h.unregisterDevice)

	r.With(h.Svc.Require("iam.users.view")).Get("/users", h.listUsers)
	r.With(h.Svc.Require("iam.users.create")).Post("/users", h.createUser)
	r.With(h.Svc.Require("iam.users.view")).Get("/users/{id}", h.getUser)
	r.With(h.Svc.Require("iam.users.update")).Patch("/users/{id}", h.updateUser)
	r.With(h.Svc.Require("iam.users.reset_password")).Post("/users/{id}/reset-password", h.resetPassword)

	r.With(h.Svc.Require("iam.roles.view")).Get("/roles", h.listRoles)
	r.With(h.Svc.Require("iam.roles.create")).Post("/roles", h.createRole)
	r.With(h.Svc.Require("iam.roles.update")).Patch("/roles/{id}", h.updateRole)
	r.With(h.Svc.Require("iam.roles.delete")).Delete("/roles/{id}", h.deleteRole)
	r.With(h.Svc.Require("iam.roles.view")).Get("/permissions", h.listPermissions)

	r.With(h.Svc.Require("iam.teams.view")).Get("/teams", h.listTeams)
	r.With(h.Svc.Require("iam.teams.create")).Post("/teams", h.createTeam)
	r.With(h.Svc.Require("iam.teams.view")).Get("/teams/{id}", h.getTeam)
	r.With(h.Svc.Require("iam.teams.update")).Patch("/teams/{id}", h.updateTeam)
	r.With(h.Svc.Require("iam.teams.delete")).Delete("/teams/{id}", h.deleteTeam)
}

type loginReq struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	Client     string `json:"client"`
	DeviceID   string `json:"device_id"`
}

type loginResp struct {
	TokenPair
	User meResp `json:"user"`
}

type meResp struct {
	ID              uuid.UUID       `json:"id"`
	FullName        string          `json:"full_name"`
	Organization    uuid.UUID       `json:"organization_id"`
	Roles           []string        `json:"roles"`
	TeamIDs         []uuid.UUID     `json:"team_ids"`
	LeadTeamIDs     []uuid.UUID     `json:"lead_team_ids"`
	Permissions     []string        `json:"permissions"`
	Properties      []propertyScope `json:"properties"`
	IsInternalAdmin bool            `json:"is_internal_admin"` // Website PRD §18: menu App Downloads hanya untuk admin_internal
}

type propertyScope struct {
	PropertyID  *uuid.UUID `json:"property_id"` // null = seluruh organization
	Permissions []string   `json:"permissions"`
}

// ToMe: representasi principal untuk respons login/verify (dipakai lintas package).
func ToMe(p *authctx.Principal) any { return toMe(p) }

func toMe(p *authctx.Principal) meResp {
	m := meResp{ID: p.UserID, FullName: p.FullName, Organization: p.OrganizationID, Roles: p.RoleCodes, TeamIDs: p.TeamIDs, LeadTeamIDs: p.LeadTeamIDs, Permissions: p.AllPermissions(), IsInternalAdmin: p.IsInternalAdmin}
	if m.Roles == nil {
		m.Roles = []string{}
	}
	if m.TeamIDs == nil {
		m.TeamIDs = []uuid.UUID{}
	}
	if m.LeadTeamIDs == nil {
		m.LeadTeamIDs = []uuid.UUID{}
	}
	m.Properties = []propertyScope{}
	for _, g := range p.Grants {
		ps := propertyScope{PropertyID: g.PropertyID, Permissions: []string{}}
		for k := range g.Permissions {
			ps.Permissions = append(ps.Permissions, k)
		}
		m.Properties = append(m.Properties, ps)
	}
	return m
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ident := req.Identifier
	if ident == "" {
		ident = req.Email
	}
	if ident == "" {
		ident = req.Username
	}
	pair, p, err := h.Svc.Login(r.Context(), LoginInput{Identifier: ident, Password: req.Password, Client: req.Client, DeviceID: req.DeviceID, IP: httpx.ClientIP(r), UserAgent: r.UserAgent()})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := loginResp{TokenPair: *pair, User: toMe(p)}
	if req.Client != "mobile" && req.Client != authctx.ClientTenantApp {
		// web: refresh token di cookie HttpOnly; tidak dikirim di body (mobile & tenant_app: di body)
		h.setRefreshCookie(w, pair.RefreshToken, pair.RefreshExpiresAt)
		resp.RefreshToken = ""
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token"`
	Client       string `json:"client"`
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshReq
	if r.ContentLength > 0 {
		_ = httpx.Decode(r, &req)
	}
	raw := req.RefreshToken
	fromCookie := false
	if raw == "" {
		if c, err := r.Cookie(refreshCookie); err == nil {
			raw = c.Value
			fromCookie = true
		}
	}
	pair, err := h.Svc.Refresh(r.Context(), raw, req.Client, httpx.ClientIP(r), r.UserAgent())
	if err != nil {
		if fromCookie {
			h.clearRefreshCookie(w)
		}
		httpx.WriteError(w, r, err)
		return
	}
	if fromCookie || (req.Client != "mobile" && req.Client != authctx.ClientTenantApp) {
		h.setRefreshCookie(w, pair.RefreshToken, pair.RefreshExpiresAt)
		if fromCookie {
			pair.RefreshToken = ""
		}
	}
	httpx.WriteJSON(w, http.StatusOK, pair)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshReq
	if r.ContentLength > 0 {
		_ = httpx.Decode(r, &req)
	}
	raw := req.RefreshToken
	if raw == "" {
		if c, err := r.Cookie(refreshCookie); err == nil {
			raw = c.Value
		}
	}
	// principal opsional (token mungkin sudah expired)
	ctx := r.Context()
	if a := r.Header.Get("Authorization"); len(a) > 7 {
		if claims, err := h.Svc.Signer.Verify(a[7:]); err == nil {
			if p, err := h.Svc.PrincipalFromClaims(ctx, claims); err == nil {
				p.IP = httpx.ClientIP(r)
				p.UserAgent = r.UserAgent()
				ctx = authctx.With(ctx, p)
			}
		}
	}
	_ = h.Svc.Logout(ctx, raw)
	h.clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setRefreshCookie(w http.ResponseWriter, token string, exp time.Time) {
	WriteRefreshCookie(w, token, exp, h.CookieDomain, h.CookieSecure)
}

// WriteRefreshCookie: cookie refresh token web (HttpOnly, path /api/v1/auth) — dipakai juga oleh alur verifikasi signup (growth).
func WriteRefreshCookie(w http.ResponseWriter, token string, exp time.Time, domain string, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: refreshCookie, Value: token, Path: "/api/v1/auth", Domain: domain, Expires: exp, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
}
func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: refreshCookie, Value: "", Path: "/api/v1/auth", Domain: h.CookieDomain, MaxAge: -1, HttpOnly: true, Secure: h.CookieSecure, SameSite: http.SameSiteStrictMode})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	p := authctx.Must(r.Context())
	u, err := h.Svc.GetUser(r.Context(), p.UserID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"principal": toMe(p), "user": u})
}

func (h *Handler) mePermissions(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, toMe(authctx.Must(r.Context())))
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.ChangeOwnPassword(r.Context(), req.CurrentPassword, req.NewPassword); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) registerDevice(w http.ResponseWriter, r *http.Request) {
	var req DeviceInput
	if err := httpx.Decode(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.RegisterDevice(r.Context(), req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unregisterDevice(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.UnregisterDevice(r.Context(), chi.URLParam(r, "token")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----- users -----

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f UserFilter
	f.Q = r.URL.Query().Get("q")
	f.RoleCode = r.URL.Query().Get("role")
	if v := r.URL.Query().Get("is_active"); v != "" {
		b := v == "true"
		f.IsActive = &b
	}
	if f.TeamID, err = httpx.QueryUUID(r, "team_id"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if f.PropertyID, err = httpx.QueryUUID(r, "property_id"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, next, err := h.Svc.ListUsers(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var in CreateUserInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u, err := h.Svc.CreateUser(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, u)
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u, err := h.Svc.GetUser(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in UpdateUserInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u, err := h.Svc.UpdateUser(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req struct {
		NewPassword string `json:"new_password"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.ResetPassword(r.Context(), id, req.NewPassword); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----- roles -----

func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.Svc.ListRoles(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(roles, nil))
}

func (h *Handler) createRole(w http.ResponseWriter, r *http.Request) {
	var in RoleInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	role, err := h.Svc.CreateRole(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, role)
}

func (h *Handler) updateRole(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in RoleInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	role, err := h.Svc.UpdateRole(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, role)
}

func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.DeleteRole(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listPermissions(w http.ResponseWriter, r *http.Request) {
	type permOut struct {
		Code   string `json:"code"`
		Module string `json:"module"`
		Object string `json:"object"`
		Action string `json:"action"`
	}
	out := make([]permOut, 0, len(h.Svc.Catalog.Permissions))
	for _, p := range h.Svc.Catalog.Permissions {
		out = append(out, permOut{p.Code, p.Module, p.Object, p.Action})
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(out, nil))
}

// ----- teams -----

func (h *Handler) listTeams(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	teams, err := h.Svc.ListTeams(r.Context(), pid, r.URL.Query().Get("domain"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(teams, nil))
}

func (h *Handler) createTeam(w http.ResponseWriter, r *http.Request) {
	var in TeamInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.Svc.CreateTeam(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, t)
}

func (h *Handler) getTeam(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.Svc.GetTeam(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, t)
}

func (h *Handler) updateTeam(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in TeamInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.Svc.UpdateTeam(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, t)
}

func (h *Handler) deleteTeam(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.DeleteTeam(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var _ = apperr.NotFound
