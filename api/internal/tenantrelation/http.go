package tenantrelation

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/tenantapp"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	// Tenant users (validasi akun, akses unit)
	r.With(req("tenant_relation.tenant_users.view")).Get("/tenant-users", h.list)
	r.With(req("tenant_relation.tenant_users.update")).Post("/tenant-users", h.create)
	r.With(req("tenant_relation.tenant_users.view")).Get("/tenant-users/{id}", h.get)
	r.With(req("tenant_relation.tenant_users.update")).Patch("/tenant-users/{id}", h.update)
	r.With(req("tenant_relation.tenant_users.validate")).Post("/tenant-users/{id}/approve", h.decide("approve"))
	r.With(req("tenant_relation.tenant_users.validate")).Post("/tenant-users/{id}/reject", h.decide("reject"))
	r.With(req("tenant_relation.tenant_users.suspend")).Post("/tenant-users/{id}/suspend", h.decide("suspend"))
	r.With(req("tenant_relation.tenant_users.validate")).Post("/tenant-users/{id}/reactivate", h.decide("reactivate"))
	r.With(req("tenant_relation.tenant_users.update")).Post("/tenant-users/{id}/access", h.grant)
	r.With(req("tenant_relation.tenant_users.update")).Delete("/tenant-users/{id}/access/{accessId}", h.revoke)
	// Komunikasi tenant ↔ BM pada Service Request (terpisah dari komentar internal)
	r.With(req("tenant_relation.messages.view")).Get("/service-requests/{id}/messages", h.messages)
	r.With(req("tenant_relation.messages.create")).Post("/service-requests/{id}/messages", h.postMessage)
	// Metrics & feedback
	r.With(req("tenant.service_requests.view")).Get("/tenant-relation/metrics", h.metrics)
	r.With(req("tenant_relation.feedback.view")).Get("/tenant-relation/feedback", h.feedback)
	// Announcements
	r.With(req("tenant_relation.announcements.view")).Get("/announcements", h.listAnn)
	r.With(req("tenant_relation.announcements.create")).Post("/announcements", h.createAnn)
	r.With(req("tenant_relation.announcements.view")).Get("/announcements/{id}", h.getAnn)
	r.With(req("tenant_relation.announcements.update")).Patch("/announcements/{id}", h.updateAnn)
	r.With(req("tenant_relation.announcements.publish")).Post("/announcements/{id}/publish", h.annAction("publish"))
	r.With(req("tenant_relation.announcements.publish")).Post("/announcements/{id}/archive", h.annAction("archive"))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f Filter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.TenantID, _ = httpx.QueryUUID(r, "tenant_id")
	f.Status = httpx.QueryCSV(r, "status")
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.List(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Create(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in UpdateInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Update(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) decide(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var in DecisionInput
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		out, err := h.Svc.Decide(r.Context(), id, action, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *Handler) grant(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in AccessInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.GrantAccess(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	aid, err := uuid.Parse(chi.URLParam(r, "accessId"))
	if err != nil {
		httpx.WriteError(w, r, apperr.Validation("accessId tidak valid"))
		return
	}
	out, err := h.Svc.RevokeAccess(r.Context(), id, aid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// messages (sisi staf): SR harus dalam property yang boleh dilihat user.
func (h *Handler) srScope(w http.ResponseWriter, r *http.Request, id uuid.UUID) bool {
	var pid uuid.UUID
	err := h.Svc.DB.WithTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT property_id FROM service_requests WHERE id = $1`, id).Scan(&pid); err != nil {
			return apperr.NotFound("Service Request")
		}
		return iam.CanOnProperty(ctx, "tenant.service_requests.view", pid)
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return false
	}
	return true
}

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !h.srScope(w, r, id) {
		return
	}
	var items []tenantapp.Message
	err = h.Svc.DB.WithTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		items, err = tenantapp.ListMessagesTx(ctx, tx, id, false)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) postMessage(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !h.srScope(w, r, id) {
		return
	}
	var in tenantapp.MessageInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out *tenantapp.Message
	err = h.Svc.DB.WithTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = tenantapp.PostMessageTx(ctx, tx, h.Svc.Jobs, id, "staff", in)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	out, err := h.Svc.Metrics(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) feedback(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, next, err := h.Svc.ListFeedback(r.Context(), pid, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) listAnn(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, next, err := h.Svc.ListAnnouncements(r.Context(), pid, httpx.QueryCSV(r, "status"), page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) createAnn(w http.ResponseWriter, r *http.Request) {
	var in AnnouncementInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateAnnouncement(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) getAnn(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.GetAnnouncement(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) updateAnn(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in AnnouncementInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateAnnouncement(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) annAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.TransitionAnnouncement(r.Context(), id, action)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

var _ = authctx.Must
