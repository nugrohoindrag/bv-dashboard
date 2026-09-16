package billing

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

// MountPublic: callback gateway (tanpa token user; verifikasi signature per provider).
func (h *Handler) MountPublic(r chi.Router) {
	r.Post("/webhooks/payments/{provider}", h.webhook)
}

// Mount: Billing (dashboard) + Mobile Tenant (`/tenant/invoices`, `/tenant/payments`).
func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("billing.invoices.view")).Get("/invoices", h.list)
	r.With(req("billing.invoices.create")).Post("/invoices", h.create)
	r.With(req("billing.invoices.view")).Get("/invoices/{id}", h.get)
	r.With(req("billing.invoices.update")).Patch("/invoices/{id}", h.update)
	r.With(req("billing.invoices.issue")).Post("/invoices/{id}/issue", h.act("issue"))
	r.With(req("billing.invoices.cancel")).Post("/invoices/{id}/cancel", h.act("cancel"))
	r.With(req("billing.payments.create")).Post("/invoices/{id}/payments", h.recordManual)
	r.With(req("billing.payments.view")).Get("/payments", h.listPayments)
	r.With(req("billing.payments.view")).Get("/payments/{id}", h.getPayment)
	r.With(req("billing.payments.verify")).Post("/payments/{id}/verify", h.verify)
	r.With(req("billing.payments.verify")).Post("/payments/{id}/fail", h.fail)
	r.With(req("billing.payments.view")).Get("/payment-providers", h.providers)
	r.With(req("platform.organizations.update")).Put("/payment-providers/{code}", h.upsertProvider)
	// Mobile Tenant
	r.With(req("tenant_app.invoices.view")).Get("/tenant/invoices/summary", h.tenantSummary)
	r.With(req("tenant_app.invoices.view")).Get("/tenant/invoices", h.tenantInvoices)
	r.With(req("tenant_app.invoices.view")).Get("/tenant/invoices/{id}", h.tenantInvoice)
	r.With(req("tenant_app.payments.create")).Post("/tenant/invoices/{id}/payments", h.tenantPay)
	r.With(req("tenant_app.payments.view")).Get("/tenant/payments", h.tenantPayments)
	r.With(req("tenant_app.payments.view")).Get("/tenant/payments/{id}", h.tenantPayment)
	r.With(req("tenant_app.payments.view")).Get("/tenant/payment-providers", h.tenantProviders)
}

func pathID(r *http.Request) (uuid.UUID, error) { return httpx.PathUUID(r, chi.URLParam, "id") }

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f Filter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.TenantID, _ = httpx.QueryUUID(r, "tenant_id")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Type = r.URL.Query().Get("type")
	f.Q = r.URL.Query().Get("q")
	f.Overdue = r.URL.Query().Get("overdue") == "true"
	items, next, err := h.Svc.List(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in InvoiceInput
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
	id, err := pathID(r)
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
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in InvoiceInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Update(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) act(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
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

func (h *Handler) recordManual(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in RecordInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.RecordManual(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) listPayments(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f PaymentFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.InvoiceID, _ = httpx.QueryUUID(r, "invoice_id")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Provider = r.URL.Query().Get("provider")
	items, next, err := h.Svc.ListPayments(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) getPayment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.GetPayment(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in RecordInput
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	out, err := h.Svc.VerifyPending(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
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
	out, err := h.Svc.FailPayment(r.Context(), id, in.Reason)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) providers(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListProviders(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) upsertProvider(w http.ResponseWriter, r *http.Request) {
	var in ProviderInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpsertProvider(r.Context(), chi.URLParam(r, "code"), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// webhook: body dibaca mentah untuk verifikasi HMAC; respons 200 untuk duplikat (idempoten), 401 signature salah.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteError(w, r, apperr.Validation("body tidak terbaca"))
		return
	}
	out, err := h.Svc.HandleWebhook(r.Context(), chi.URLParam(r, "provider"), r, body)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ---- tenant ----

func (h *Handler) tenantSummary(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.TenantSummary(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) tenantInvoices(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var open *bool
	if v := r.URL.Query().Get("open"); v != "" {
		b := v == "true"
		open = &b
	}
	items, next, err := h.Svc.TenantInvoices(r.Context(), open, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) tenantInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.TenantInvoice(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) tenantPay(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in InitiateInput
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	out, err := h.Svc.TenantPay(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) tenantPayments(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, next, err := h.Svc.TenantPayments(r.Context(), page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) tenantPayment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.TenantPayment(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) tenantProviders(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.TenantProviders(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}
