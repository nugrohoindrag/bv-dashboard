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
	// PRD P3 v2.1 P3-ACC-05: reset password (password sementara dikirim staf lewat tombol WhatsApp)
	r.With(req("tenant_relation.tenant_users.reset_password")).Post("/tenant-users/{id}/reset-password", h.resetPassword)
	// PRD P3 v2.1 P3-FDB-03: feedback umum tenant
	r.With(req("tenant_relation.feedback.view")).Get("/tenant-relation/general-feedback", h.listGeneralFeedback)
	r.With(req("tenant_relation.feedback.view")).Get("/tenant-relation/general-feedback/{id}", h.getGeneralFeedback)
	for _, a := range []string{"review", "respond", "close"} {
		r.With(req("tenant_relation.feedback.respond")).Post("/tenant-relation/general-feedback/{id}/"+a, h.actGeneralFeedback(a))
	}
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
	// PRD P3 v2.1 P3-ANN-03, P3-ANN-05, P3-BRC-01: jadwal publish, pelacakan baca, broadcast darurat
	r.With(req("tenant_relation.announcements.publish")).Post("/announcements/{id}/schedule", h.annAction("schedule"))
	r.With(req("tenant_relation.announcements.publish")).Post("/announcements/{id}/unschedule", h.annAction("unschedule"))
	r.With(req("tenant_relation.announcements.view")).Get("/announcements/{id}/reads", h.annReads)
	r.With(req("tenant_relation.announcements.broadcast")).Post("/announcements/broadcast", h.broadcast)
	// News Staff App: baca pengumuman audience staff|all (semua staf terautentikasi, bukan tenant)
	r.Get("/staff/announcements", h.staffAnnouncements)
	r.Get("/staff/announcements/{id}", h.staffAnnouncement)
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.ResetPassword(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listGeneralFeedback(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, next, err := h.Svc.ListGeneralFeedback(r.Context(), pid, httpx.QueryCSV(r, "status"), r.URL.Query().Get("category"), page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) getGeneralFeedback(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.GetGeneralFeedback(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) actGeneralFeedback(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var in FeedbackActionInput
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		out, err := h.Svc.ActGeneralFeedback(r.Context(), id, action, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *Handler) annReads(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.AnnouncementReads(r.Context(), id, r.URL.Query().Get("unread") == "true")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) broadcast(w http.ResponseWriter, r *http.Request) {
	var in BroadcastInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Broadcast(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) staffAnnouncements(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, next, err := h.Svc.StaffAnnouncements(r.Context(), page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) staffAnnouncement(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.StaffAnnouncement(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
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
		tenantapp.FillMessageAttachments(ctx, tx, h.Svc.Attachments, items)
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
		// P3-SRQ-05: lampiran pesan staf harus file pada SR yang sama
		if err := tenantapp.ValidateStaffMessageAttachments(ctx, tx, id, in.AttachmentIDs); err != nil {
			return err
		}
		out, err = tenantapp.PostMessageTx(ctx, tx, h.Svc.Jobs, id, "staff", in)
		if err == nil {
			list := []tenantapp.Message{*out}
			tenantapp.FillMessageAttachments(ctx, tx, h.Svc.Attachments, list)
			out = &list[0]
		}
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
	items, next, err := h.Svc.ListAnnouncementsFiltered(r.Context(), pid, httpx.QueryCSV(r, "status"), r.URL.Query().Get("category"), page)
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
