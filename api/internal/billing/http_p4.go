package billing

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/httpx"
)

// ---------- HTTP PRD P4 v2.1 (Financial Operations) ----------

func (h *Handler) mountP4(r chi.Router) {
	req := h.IAM.Require
	// invoice lanjutan
	r.With(req("billing.invoices.void")).Post("/invoices/{id}/void", h.act("void"))
	r.With(req("billing.invoices.view")).Get("/invoices/{id}/pdf", h.invoicePDF)
	r.With(req("billing.invoices.view")).Post("/invoices/{id}/document-link", h.docLink("invoice", false))
	r.With(req("billing.payments.allocate")).Post("/invoices/{id}/apply-credit", h.applyBalance("credit"))
	r.With(req("billing.payments.allocate")).Post("/invoices/{id}/apply-deposit", h.applyBalance("deposit"))
	r.With(req("billing.credit_notes.create")).Post("/invoices/{id}/credit-notes", h.requestCreditNote)
	r.With(req("billing.invoices.import")).Post("/invoices/import", h.importInvoices)
	r.With(req("billing.invoices.create")).Get("/billing/charges/draft", h.chargeDraft)
	r.With(req("billing.invoices.create")).Post("/billing/charges", h.createCharge)
	// payment lanjutan
	r.With(req("billing.payments.allocate")).Post("/payments/receive", h.receive)
	r.With(req("billing.payments.refund")).Post("/payments/{id}/refund", h.refund)
	r.With(req("billing.payments.create")).Patch("/payments/{id}", h.updatePayment)
	r.With(req("billing.payments.view")).Get("/payments/{id}/proofs", h.proofs)
	r.With(req("billing.payments.view")).Get("/payments/{id}/receipt", h.receiptPDF)
	r.With(req("billing.payments.view")).Post("/payments/{id}/document-link", h.docLink("receipt", false))
	// credit note
	r.With(req("billing.credit_notes.view")).Get("/billing/credit-notes", h.listCreditNotes)
	r.With(req("billing.credit_notes.view")).Get("/billing/credit-notes/{id}", h.getCreditNote)
	r.With(req("billing.credit_notes.approve")).Post("/billing/credit-notes/{id}/approve", h.decideCreditNote("approve"))
	r.With(req("billing.credit_notes.approve")).Post("/billing/credit-notes/{id}/reject", h.decideCreditNote("reject"))
	r.With(req("billing.credit_notes.create")).Post("/billing/credit-notes/{id}/cancel", h.decideCreditNote("cancel"))
	// pengaturan & rekening
	r.With(req("billing.settings.view")).Get("/billing/settings", h.getSettings)
	r.With(req("billing.settings.manage")).Put("/billing/settings", h.putSettings)
	r.With(req("billing.settings.view")).Get("/billing/bank-accounts", h.listBankAccounts)
	r.With(req("billing.settings.manage")).Post("/billing/bank-accounts", h.saveBankAccount(false))
	r.With(req("billing.settings.manage")).Patch("/billing/bank-accounts/{id}", h.saveBankAccount(true))
	// billing rule & run
	r.With(req("billing.rules.view")).Get("/billing/rules", h.listRules)
	r.With(req("billing.rules.manage")).Post("/billing/rules", h.saveRule(false))
	r.With(req("billing.rules.view")).Get("/billing/rules/{id}", h.getRule)
	r.With(req("billing.rules.manage")).Patch("/billing/rules/{id}", h.saveRule(true))
	r.With(req("billing.runs.view")).Get("/billing/runs", h.listRuns)
	r.With(req("billing.runs.generate")).Post("/billing/runs", h.createRun)
	r.With(req("billing.runs.view")).Get("/billing/runs/{id}", h.getRun)
	r.With(req("billing.runs.generate")).Post("/billing/runs/{id}/refresh", h.runAction("refresh"))
	r.With(req("billing.runs.generate")).Post("/billing/runs/{id}/generate", h.runAction("generate"))
	r.With(req("billing.runs.issue")).Post("/billing/runs/{id}/issue", h.runAction("issue"))
	r.With(req("billing.runs.generate")).Post("/billing/runs/{id}/cancel", h.runAction("cancel"))
	r.With(req("billing.runs.generate")).Post("/billing/runs/{id}/lines/{lineId}/include", h.toggleLine(true))
	r.With(req("billing.runs.generate")).Post("/billing/runs/{id}/lines/{lineId}/exclude", h.toggleLine(false))
	// sinking fund, deposit, kredit
	r.With(req("billing.sinking_fund.view")).Get("/billing/sinking-fund", h.sinkingFund)
	r.With(req("billing.sinking_fund.manage")).Post("/billing/sinking-fund/entries", h.addSinkingFund)
	r.With(req("billing.deposits.view")).Get("/billing/balances", h.balances)
	r.With(req("billing.deposits.view")).Get("/billing/ledger-entries", h.ledgerEntries)
	r.With(req("billing.deposits.manage")).Post("/billing/deposits/entries", h.addDeposit)
	// denda
	r.With(req("billing.penalties.view")).Get("/billing/penalty-rules", h.listPenaltyRules)
	r.With(req("billing.penalties.manage")).Post("/billing/penalty-rules", h.savePenaltyRule(false))
	r.With(req("billing.penalties.manage")).Patch("/billing/penalty-rules/{id}", h.savePenaltyRule(true))
	r.With(req("billing.penalties.view")).Get("/billing/penalties", h.listPenalties)
	r.With(req("billing.penalties.manage")).Post("/billing/penalties/bill", h.billPenalties)
	r.With(req("billing.penalties.waive")).Post("/billing/penalties/{id}/waive", h.waivePenalty)
	// piutang & penagihan
	r.With(req("billing.invoices.view")).Get("/billing/aging", h.aging)
	r.With(req("billing.invoices.view")).Get("/billing/statement", h.statement)
	r.With(req("billing.invoices.view")).Post("/billing/statement/link", h.statementLink(false))
	r.With(req("billing.collections.view")).Get("/billing/collections/worklist", h.worklist)
	r.With(req("billing.collections.view")).Get("/billing/collection-logs", h.listCollectionLogs)
	r.With(req("billing.collections.manage")).Post("/billing/collection-logs", h.createCollectionLog)
	r.With(req("billing.collections.manage")).Patch("/billing/collection-logs/{id}", h.updateCollectionLog)
	// rekonsiliasi
	r.With(req("billing.reconciliation.view")).Get("/billing/bank-statements", h.listImports)
	r.With(req("billing.reconciliation.manage")).Post("/billing/bank-statements", h.importStatement)
	r.With(req("billing.reconciliation.view")).Get("/billing/bank-statements/{id}", h.getImport)
	r.With(req("billing.reconciliation.manage")).Post("/billing/bank-statements/{id}/confirm-suggestions", h.confirmSuggestions)
	r.With(req("billing.reconciliation.manage")).Post("/billing/bank-statement-lines/{id}/match", h.matchLine)
	r.With(req("billing.reconciliation.manage")).Post("/billing/bank-statement-lines/{id}/ignore", h.ignoreLine(true))
	r.With(req("billing.reconciliation.manage")).Post("/billing/bank-statement-lines/{id}/restore", h.ignoreLine(false))
	// Mobile Tenant: dokumen, statement, saldo (P4-TNT-03, P4-TNT-06)
	r.With(req("tenant_app.invoices.view")).Get("/tenant/invoices/{id}/pdf", h.tenantInvoicePDF)
	r.With(req("tenant_app.invoices.view")).Post("/tenant/invoices/{id}/document-link", h.docLink("invoice", true))
	r.With(req("tenant_app.payments.view")).Get("/tenant/payments/{id}/receipt", h.tenantReceiptPDF)
	r.With(req("tenant_app.payments.view")).Post("/tenant/payments/{id}/document-link", h.docLink("receipt", true))
	r.With(req("tenant_app.invoices.view")).Get("/tenant/statement", h.tenantStatement)
	r.With(req("tenant_app.invoices.view")).Post("/tenant/statement/link", h.statementLink(true))
	r.With(req("tenant_app.invoices.view")).Get("/tenant/balances", h.tenantBalances)
	r.With(req("tenant_app.payments.view")).Get("/tenant/payments/{id}/proofs", h.tenantProofs)
}

// ---- helpers ----

func reply(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, status, v)
}

// decodeOpt: body JSON opsional (kosong = zero value).
func decodeOpt(r *http.Request, dst any) error {
	if r.ContentLength == 0 {
		return nil
	}
	return httpx.Decode(r, dst)
}

// decodeLarge: body besar (file base64) sampai 12 MB.
func decodeLarge(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 12<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return apperr.Validation("body kosong")
		}
		return apperr.Validation("JSON tidak valid: " + err.Error())
	}
	return nil
}

func qUUID(r *http.Request, key string) *uuid.UUID {
	v, _ := httpx.QueryUUID(r, key)
	return v
}

func qStr(r *http.Request, key string) *string {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil
	}
	return &v
}

// qTimeOrDate: RFC3339 → timestamp; YYYY-MM-DD → tanggal kalender property (dibandingkan inklusif di SQL, bukan 00:00 UTC).
func qTimeOrDate(r *http.Request, key string) (*time.Time, *string) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return &t, nil
	}
	if _, err := time.Parse("2006-01-02", v); err == nil {
		return nil, &v
	}
	return nil, nil
}

func qTime(r *http.Request, key string) *time.Time {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return &t
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return &t
	}
	return nil
}

func writePDF(w http.ResponseWriter, r *http.Request, d *Document, err error) {
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	disp := "inline"
	if r.URL.Query().Get("download") == "1" {
		disp = "attachment"
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", disp+`; filename="`+d.FileName+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(d.Bytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(d.Bytes)
}

func withID(fn func(w http.ResponseWriter, r *http.Request, id uuid.UUID)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		fn(w, r, id)
	}
}

// ---- dokumen ----

func (h *Handler) invoicePDF(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		d, err := h.Svc.InvoicePDF(r.Context(), id)
		writePDF(w, r, d, err)
	})(w, r)
}

func (h *Handler) receiptPDF(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		d, err := h.Svc.ReceiptPDF(r.Context(), id)
		writePDF(w, r, d, err)
	})(w, r)
}

func (h *Handler) tenantInvoicePDF(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		d, err := h.Svc.TenantInvoicePDF(r.Context(), id)
		writePDF(w, r, d, err)
	})(w, r)
}

func (h *Handler) tenantReceiptPDF(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		d, err := h.Svc.TenantReceiptPDF(r.Context(), id)
		writePDF(w, r, d, err)
	})(w, r)
}

func (h *Handler) docLink(kind string, tenant bool) http.HandlerFunc {
	return withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		out, err := h.Svc.DocumentLinkFor(r.Context(), kind, id, tenant)
		reply(w, r, http.StatusOK, out, err)
	})
}

func (h *Handler) publicDocument(w http.ResponseWriter, r *http.Request) {
	d, err := h.Svc.RenderSigned(r.Context(), chi.URLParam(r, "token"))
	writePDF(w, r, d, err)
}

func (h *Handler) statement(w http.ResponseWriter, r *http.Request) {
	pid := qUUID(r, "property_id")
	if pid == nil {
		httpx.WriteError(w, r, apperr.Validation("property_id wajib").WithField("property_id", "wajib"))
		return
	}
	tid, uid := qUUID(r, "tenant_id"), qUUID(r, "unit_location_id")
	if r.URL.Query().Get("format") == "pdf" {
		d, err := h.Svc.StatementPDF(r.Context(), *pid, tid, uid, qStr(r, "from"), qStr(r, "to"))
		writePDF(w, r, d, err)
		return
	}
	out, err := h.Svc.Statement(r.Context(), *pid, tid, uid, qStr(r, "from"), qStr(r, "to"))
	reply(w, r, http.StatusOK, out, err)
}

func (h *Handler) tenantStatement(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("format") == "pdf" {
		d, err := h.Svc.TenantStatementPDF(r.Context(), qStr(r, "from"), qStr(r, "to"))
		writePDF(w, r, d, err)
		return
	}
	out, err := h.Svc.TenantStatement(r.Context(), qStr(r, "from"), qStr(r, "to"))
	reply(w, r, http.StatusOK, out, err)
}

func (h *Handler) statementLink(tenant bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PropertyID *uuid.UUID `json:"property_id"`
			TenantID   *uuid.UUID `json:"tenant_id"`
			UnitID     *uuid.UUID `json:"unit_location_id"`
			From       *string    `json:"from"`
			To         *string    `json:"to"`
		}
		if err := decodeOpt(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.StatementLink(r.Context(), in.PropertyID, in.TenantID, in.UnitID, in.From, in.To, tenant)
		reply(w, r, http.StatusOK, out, err)
	}
}

func (h *Handler) tenantBalances(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.TenantBalances(r.Context())
	reply(w, r, http.StatusOK, out, err)
}

func (h *Handler) tenantProofs(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		if _, err := h.Svc.TenantPayment(r.Context(), id); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		items, err := h.Svc.Proofs(r.Context(), id, h.Att)
		reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
	})(w, r)
}

// ---- invoice & payment ----

func (h *Handler) applyBalance(kind string) http.HandlerFunc {
	return withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in ApplyBalanceInput
		if err := decodeOpt(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.ApplyBalance(r.Context(), id, kind, in)
		reply(w, r, http.StatusCreated, out, err)
	})
}

func (h *Handler) requestCreditNote(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in CreditNoteInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.RequestCreditNote(r.Context(), id, in)
		reply(w, r, http.StatusCreated, out, err)
	})(w, r)
}

func (h *Handler) importInvoices(w http.ResponseWriter, r *http.Request) {
	var in InvoiceImportInput
	if err := decodeLarge(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.ImportInvoices(r.Context(), in)
	status := http.StatusCreated
	if in.DryRun || (out != nil && len(out.Errors) > 0) {
		status = http.StatusOK
	}
	reply(w, r, status, out, err)
}

func (h *Handler) chargeDraft(w http.ResponseWriter, r *http.Request) {
	sid := qUUID(r, "source_id")
	if sid == nil {
		httpx.WriteError(w, r, apperr.Validation("source_id wajib").WithField("source_id", "wajib"))
		return
	}
	out, err := h.Svc.ChargeDraft(r.Context(), r.URL.Query().Get("source_type"), *sid)
	reply(w, r, http.StatusOK, out, err)
}

func (h *Handler) createCharge(w http.ResponseWriter, r *http.Request) {
	var in ChargeInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateCharge(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

func (h *Handler) receive(w http.ResponseWriter, r *http.Request) {
	var in ReceiveInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Receive(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

func (h *Handler) refund(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in RefundInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.Refund(r.Context(), id, in)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

func (h *Handler) updatePayment(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in PaymentUpdateInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.UpdatePayment(r.Context(), id, in)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

func (h *Handler) proofs(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		if _, err := h.Svc.GetPayment(r.Context(), id); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		items, err := h.Svc.Proofs(r.Context(), id, h.Att)
		reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
	})(w, r)
}

// ---- credit note ----

func (h *Handler) listCreditNotes(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := CreditNoteFilter{PropertyID: qUUID(r, "property_id"), InvoiceID: qUUID(r, "invoice_id"), Statuses: httpx.QueryCSV(r, "status")}
	items, next, err := h.Svc.ListCreditNotes(r.Context(), f, page)
	reply(w, r, http.StatusOK, httpx.NewList(items, next), err)
}

func (h *Handler) getCreditNote(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		out, err := h.Svc.GetCreditNote(r.Context(), id)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

func (h *Handler) decideCreditNote(action string) http.HandlerFunc {
	return withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in CreditNoteDecision
		if err := decodeOpt(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.DecideCreditNote(r.Context(), id, action, in)
		reply(w, r, http.StatusOK, out, err)
	})
}

// ---- pengaturan ----

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.GetSettings(r.Context(), qUUID(r, "property_id"))
	reply(w, r, http.StatusOK, out, err)
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) {
	var in SettingsInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.PutSettings(r.Context(), qUUID(r, "property_id"), in)
	reply(w, r, http.StatusOK, out, err)
}

func (h *Handler) listBankAccounts(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListBankAccounts(r.Context(), qUUID(r, "property_id"))
	reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
}

func (h *Handler) saveBankAccount(update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in BankAccountInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if !update {
			out, err := h.Svc.SaveBankAccount(r.Context(), nil, in)
			reply(w, r, http.StatusCreated, out, err)
			return
		}
		withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
			out, err := h.Svc.SaveBankAccount(r.Context(), &id, in)
			reply(w, r, http.StatusOK, out, err)
		})(w, r)
	}
}

// ---- rule & run ----

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListRules(r.Context(), qUUID(r, "property_id"), r.URL.Query().Get("active") == "true")
	reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		out, err := h.Svc.GetRule(r.Context(), id)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

func (h *Handler) saveRule(update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in RuleInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if !update {
			out, err := h.Svc.SaveRule(r.Context(), nil, in)
			reply(w, r, http.StatusCreated, out, err)
			return
		}
		withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
			out, err := h.Svc.SaveRule(r.Context(), &id, in)
			reply(w, r, http.StatusOK, out, err)
		})(w, r)
	}
}

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, next, err := h.Svc.ListRuns(r.Context(), qUUID(r, "property_id"), httpx.QueryCSV(r, "status"), page)
	reply(w, r, http.StatusOK, httpx.NewList(items, next), err)
}

func (h *Handler) createRun(w http.ResponseWriter, r *http.Request) {
	var in RunInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateRun(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		out, err := h.Svc.GetRun(r.Context(), id)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

func (h *Handler) runAction(action string) http.HandlerFunc {
	return withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in ActionInput
		if err := decodeOpt(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var out *Run
		var err error
		switch action {
		case "refresh":
			out, err = h.Svc.RefreshRun(r.Context(), id)
		case "generate":
			out, err = h.Svc.GenerateRun(r.Context(), id)
		case "issue":
			out, err = h.Svc.IssueRun(r.Context(), id)
		case "cancel":
			out, err = h.Svc.CancelRun(r.Context(), id, in.Reason)
		}
		reply(w, r, http.StatusOK, out, err)
	})
}

func (h *Handler) toggleLine(include bool) http.HandlerFunc {
	return withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		lineID, err := httpx.PathUUID(r, chi.URLParam, "lineId")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.ToggleLine(r.Context(), id, lineID, include)
		reply(w, r, http.StatusOK, out, err)
	})
}

// ---- ledger ----

func (h *Handler) sinkingFund(w http.ResponseWriter, r *http.Request) {
	pid := qUUID(r, "property_id")
	if pid == nil {
		httpx.WriteError(w, r, apperr.Validation("property_id wajib").WithField("property_id", "wajib"))
		return
	}
	out, err := h.Svc.SinkingFund(r.Context(), *pid, qTime(r, "from"), qTime(r, "to"))
	reply(w, r, http.StatusOK, out, err)
}

func (h *Handler) addSinkingFund(w http.ResponseWriter, r *http.Request) {
	var in FundEntryInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.AddSinkingFundEntry(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

func (h *Handler) balances(w http.ResponseWriter, r *http.Request) {
	pid := qUUID(r, "property_id")
	if pid == nil {
		httpx.WriteError(w, r, apperr.Validation("property_id wajib").WithField("property_id", "wajib"))
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "deposit"
	}
	items, err := h.Svc.Balances(r.Context(), kind, *pid, qUUID(r, "tenant_id"))
	reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
}

func (h *Handler) ledgerEntries(w http.ResponseWriter, r *http.Request) {
	pid := qUUID(r, "property_id")
	if pid == nil {
		httpx.WriteError(w, r, apperr.Validation("property_id wajib").WithField("property_id", "wajib"))
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "deposit"
	}
	items, err := h.Svc.LedgerEntries(r.Context(), kind, *pid, qUUID(r, "tenant_id"), qUUID(r, "unit_location_id"))
	reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
}

func (h *Handler) addDeposit(w http.ResponseWriter, r *http.Request) {
	var in DepositEntryInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.AddDepositEntry(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

// ---- denda ----

func (h *Handler) listPenaltyRules(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListPenaltyRules(r.Context(), qUUID(r, "property_id"))
	reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
}

func (h *Handler) savePenaltyRule(update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in PenaltyRuleInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if !update {
			out, err := h.Svc.SavePenaltyRule(r.Context(), nil, in)
			reply(w, r, http.StatusCreated, out, err)
			return
		}
		withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
			out, err := h.Svc.SavePenaltyRule(r.Context(), &id, in)
			reply(w, r, http.StatusOK, out, err)
		})(w, r)
	}
}

func (h *Handler) listPenalties(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := PenaltyFilter{PropertyID: qUUID(r, "property_id"), InvoiceID: qUUID(r, "invoice_id"), TenantID: qUUID(r, "tenant_id"), Statuses: httpx.QueryCSV(r, "status"), Unbilled: r.URL.Query().Get("unbilled") == "true"}
	items, next, err := h.Svc.ListPenalties(r.Context(), f, page)
	reply(w, r, http.StatusOK, httpx.NewList(items, next), err)
}

func (h *Handler) billPenalties(w http.ResponseWriter, r *http.Request) {
	var in BillPenaltiesInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.BillPenalties(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

func (h *Handler) waivePenalty(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in ActionInput
		if err := decodeOpt(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.WaivePenalty(r.Context(), id, in.Reason)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

// ---- piutang ----

func (h *Handler) aging(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.Aging(r.Context(), qUUID(r, "property_id"), r.URL.Query().Get("group_by"), qUUID(r, "tenant_id"))
	reply(w, r, http.StatusOK, out, err)
}

func (h *Handler) worklist(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.CollectionWorklist(r.Context(), qUUID(r, "property_id"))
	reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
}

func (h *Handler) listCollectionLogs(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := CollectionFilter{PropertyID: qUUID(r, "property_id"), TenantID: qUUID(r, "tenant_id"), UnitID: qUUID(r, "unit_location_id"), InvoiceID: qUUID(r, "invoice_id"),
		PromiseStatus: r.URL.Query().Get("promise_status"), FollowUpDue: r.URL.Query().Get("follow_up_due") == "true"}
	items, next, err := h.Svc.ListCollectionLogs(r.Context(), f, page)
	reply(w, r, http.StatusOK, httpx.NewList(items, next), err)
}

func (h *Handler) createCollectionLog(w http.ResponseWriter, r *http.Request) {
	var in CollectionLogInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateCollectionLog(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

func (h *Handler) updateCollectionLog(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in CollectionLogUpdate
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.UpdateCollectionLog(r.Context(), id, in)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

// ---- rekonsiliasi ----

func (h *Handler) listImports(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, next, err := h.Svc.ListImports(r.Context(), qUUID(r, "property_id"), page)
	reply(w, r, http.StatusOK, httpx.NewList(items, next), err)
}

func (h *Handler) importStatement(w http.ResponseWriter, r *http.Request) {
	var in ImportInput
	if err := decodeLarge(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.ImportStatement(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

func (h *Handler) getImport(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		out, err := h.Svc.GetImport(r.Context(), id)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

func (h *Handler) confirmSuggestions(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in struct {
			MinScore int `json:"min_score"`
		}
		if err := decodeOpt(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, n, err := h.Svc.ConfirmSuggestions(r.Context(), id, in.MinScore)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"confirmed": n, "import": out})
	})(w, r)
}

func (h *Handler) matchLine(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in MatchInput
		if err := decodeOpt(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.MatchLine(r.Context(), id, in)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

func (h *Handler) ignoreLine(ignore bool) http.HandlerFunc {
	return withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in struct {
			Note string `json:"note"`
		}
		if err := decodeOpt(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.SetLineIgnored(r.Context(), id, ignore, in.Note)
		reply(w, r, http.StatusOK, out, err)
	})
}
