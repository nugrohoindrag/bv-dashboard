package tenantapp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

// MountPublic: registrasi mandiri (tanpa token; rate-limit per IP) — PRD §7, kontrak PWA §2.1.
func (h *Handler) MountPublic(r chi.Router) {
	r.Get("/tenant/registration/properties", h.regProperties)
	r.Get("/tenant/registration/locations", h.regLocations)
	r.Post("/tenant/register", h.register)
}

// Mount: endpoint Mobile Tenant (token akun tenant; permission tenant_app.*) — PRD §31.
func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("tenant_app.profile.view")).Get("/tenant/me", h.me)
	r.With(req("tenant_app.profile.update")).Patch("/tenant/me", h.updateMe)
	r.With(req("tenant_app.requests.view")).Get("/tenant/locations", h.locations)
	r.With(req("tenant_app.requests.view")).Get("/tenant/categories", h.categories)
	r.With(req("tenant_app.requests.view")).Get("/tenant/requests", h.list)
	r.With(req("tenant_app.requests.create")).Post("/tenant/requests", h.create)
	r.With(req("tenant_app.requests.view")).Get("/tenant/requests/{id}", h.get)
	r.With(req("tenant_app.requests.cancel")).Post("/tenant/requests/{id}/cancel", h.act("cancel"))
	r.With(req("tenant_app.requests.confirm")).Post("/tenant/requests/{id}/confirm", h.act("confirm"))
	r.With(req("tenant_app.requests.reopen")).Post("/tenant/requests/{id}/reopen", h.act("reopen"))
	r.With(req("tenant_app.feedback.create")).Post("/tenant/requests/{id}/feedback", h.feedback)
	r.With(req("tenant_app.messages.view")).Get("/tenant/requests/{id}/messages", h.messages)
	r.With(req("tenant_app.messages.create")).Post("/tenant/requests/{id}/messages", h.postMessage)
	r.With(req("tenant_app.requests.create")).Post("/tenant/attachments/presign", h.presign)
	r.With(req("tenant_app.requests.create")).Post("/tenant/attachments/{id}/confirm", h.confirmAttachment)
	r.With(req("tenant_app.inbox.view")).Get("/tenant/announcements", h.announcements)
	r.With(req("tenant_app.inbox.view")).Get("/tenant/announcements/{id}", h.announcement)
	// ---- PRD P3 v2.1 ----
	r.With(req("tenant_app.inbox.view")).Post("/tenant/announcements/{id}/acknowledge", h.ackAnnouncement)         // P3-ANN-05
	r.With(req("tenant_app.units.view")).Get("/tenant/units", h.units)                                             // P3-UNT-01..04
	r.With(req("tenant_app.units.view")).Get("/tenant/units/{id}", h.unit)                                         //
	r.With(req("tenant_app.feedback.create")).Post("/tenant/feedback", h.createFeedback)                           // P3-FDB-02
	r.With(req("tenant_app.feedback.view")).Get("/tenant/feedback", h.listFeedback)                                //
	r.With(req("tenant_app.feedback.view")).Get("/tenant/feedback/{id}", h.getFeedback)                            //
	r.With(req("tenant_app.members.view")).Get("/tenant/members", h.members)                                       // P3-ACC-08 (tenant_admin)
	r.With(req("tenant_app.members.manage")).Post("/tenant/members", h.createMember)                               //
	r.With(req("tenant_app.members.manage")).Post("/tenant/members/{id}/deactivate", h.memberAction("deactivate")) //
	r.With(req("tenant_app.members.manage")).Post("/tenant/members/{id}/reactivate", h.memberAction("reactivate")) //
	r.With(req("tenant_app.members.manage")).Put("/tenant/members/{id}/units", h.memberUnits)                      //
}

func (h *Handler) ackAnnouncement(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.AcknowledgeAnnouncement(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) units(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.MyUnits(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) unit(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.MyUnit(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createFeedback(w http.ResponseWriter, r *http.Request) {
	var in GeneralFeedbackInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateGeneralFeedback(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) listFeedback(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, next, err := h.Svc.ListGeneralFeedback(r.Context(), page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) getFeedback(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) members(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.Members(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createMember(w http.ResponseWriter, r *http.Request) {
	var in MemberInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateMember(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) memberAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.MemberAction(r.Context(), id, action)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *Handler) memberUnits(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in struct {
		UnitIDs []uuid.UUID `json:"unit_ids"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.SetMemberUnits(r.Context(), id, in.UnitIDs)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) regProperties(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.RegistrationProperties(r.Context(), r.URL.Query().Get("organization_slug"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) regLocations(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil || pid == nil {
		httpx.WriteError(w, r, apperr.Validation("property_id wajib"))
		return
	}
	parent, _ := httpx.QueryUUID(r, "parent_id")
	items, err := h.Svc.RegistrationLocations(r.Context(), r.URL.Query().Get("organization_slug"), *pid, parent, r.URL.Query().Get("type"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var in RegisterInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Register(r.Context(), in, httpx.ClientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.Me(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	var in UpdateMeInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateMe(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) locations(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ReportableLocations(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) categories(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.Categories(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f ListFilter
	if v := r.URL.Query().Get("open"); v != "" {
		b := v == "true"
		f.Open = &b
	}
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.ListRequests(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateRequestInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateRequest(r.Context(), in)
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
	out, err := h.Svc.GetRequest(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) act(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var in ActionInput
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		out, err := h.Svc.Act(r.Context(), id, action, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *Handler) feedback(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in FeedbackInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Feedback(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.ListMessages(r.Context(), id)
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
	var in MessageInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.PostMessage(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

// tenantUploadPolicy: object yang boleh diberi lampiran oleh akun tenant (kepemilikan divalidasi ObjectAccess tiap object):
// permintaan (foto/dokumen/video; juga lampiran pesan), feedback umum (foto), bukti transfer pembayaran (foto/PDF, PRD P4
// v2.1 P4-VRF-02), kendaraan (foto/STNK, P3-PRK-01).
var tenantUploadPolicy = map[string]map[string]bool{
	"service_request": {"photo": true, "document": true, "video": true},
	"tenant_feedback": {"photo": true},
	"payment":         {"photo": true, "document": true},
	"vehicle":         {"photo": true, "document": true},
}

// presign: lampiran tenant — object milik tenant (ObjectAccess cabang tenant) sesuai tenantUploadPolicy.
func (h *Handler) presign(w http.ResponseWriter, r *http.Request) {
	var in attachments.PresignInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !authctx.Must(r.Context()).IsTenant {
		httpx.WriteError(w, r, apperr.Forbidden(""))
		return
	}
	allowed, ok := tenantUploadPolicy[in.ObjectType]
	if !ok {
		httpx.WriteError(w, r, apperr.Validation("object_type harus service_request|tenant_feedback|payment|vehicle"))
		return
	}
	if !allowed[in.AttachmentType] {
		in.AttachmentType = "photo"
		if !allowed["photo"] {
			httpx.WriteError(w, r, apperr.Validation("attachment_type tidak diizinkan untuk object ini"))
			return
		}
	}
	out, err := h.Svc.Attachments.Presign(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) confirmAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in attachments.ConfirmInput
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	// hanya file yang diunggah oleh tenant ini
	p := authctx.Must(r.Context())
	a, err := h.Svc.Attachments.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if a.UploadedBy != p.UserID {
		httpx.WriteError(w, r, apperr.NotFound("Attachment"))
		return
	}
	out, err := h.Svc.Attachments.Confirm(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) announcement(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Announcement(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) announcements(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, next, err := h.Svc.Announcements(r.Context(), r.URL.Query().Get("category"), page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

var _ = uuid.Nil
