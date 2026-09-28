package finance

import (
	"net/http"
	"strconv"
	"time"

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

// Mount: Finance (PRD P4 v2.1 §8–§9): budget, biaya, budget vs actual, pemetaan akun, ekspor jurnal, webhook keluar.
func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("billing.budgets.view")).Get("/finance/categories", h.categories)
	r.With(req("billing.budgets.view")).Get("/finance/budgets", h.listBudgets)
	r.With(req("billing.budgets.manage")).Post("/finance/budgets", h.createBudget)
	r.With(req("billing.budgets.view")).Get("/finance/budgets/{id}", h.getBudget)
	r.With(req("billing.budgets.manage")).Patch("/finance/budgets/{id}", h.updateBudget)
	r.With(req("billing.budgets.approve")).Post("/finance/budgets/{id}/approve", h.actBudget("approve"))
	r.With(req("billing.budgets.manage")).Post("/finance/budgets/{id}/revise", h.actBudget("revise"))
	r.With(req("billing.budgets.view")).Get("/finance/budget-actual", h.budgetActual)
	r.With(req("billing.budgets.view")).Get("/finance/budget-actual/transactions", h.actualTransactions)
	r.With(h.IAM.RequireAny("billing.costs.view", "reports.reports.view")).Get("/finance/operating-costs", h.operatingCosts)
	r.With(req("billing.costs.view")).Get("/finance/costs", h.listCosts)
	r.With(req("billing.costs.manage")).Post("/finance/costs", h.saveCost(false))
	r.With(req("billing.costs.manage")).Patch("/finance/costs/{id}", h.saveCost(true))
	r.With(req("billing.costs.manage")).Delete("/finance/costs/{id}", h.deleteCost)
	r.With(req("billing.accounting.view")).Get("/finance/account-mappings", h.listMappings)
	r.With(req("billing.accounting.manage")).Put("/finance/account-mappings", h.putMappings)
	r.With(req("billing.accounting.view")).Get("/finance/journal", h.journal)
	r.With(req("billing.accounting.export")).Get("/finance/journal/export", h.exportJournal)
	r.With(req("billing.accounting.view")).Get("/finance/webhooks", h.listEndpoints)
	r.With(req("billing.accounting.manage")).Post("/finance/webhooks", h.saveEndpoint(false))
	r.With(req("billing.accounting.manage")).Patch("/finance/webhooks/{id}", h.saveEndpoint(true))
	r.With(req("billing.accounting.manage")).Delete("/finance/webhooks/{id}", h.deleteEndpoint)
	r.With(req("billing.accounting.manage")).Post("/finance/webhooks/{id}/test", h.testEndpoint)
	r.With(req("billing.accounting.view")).Get("/finance/webhooks/{id}/deliveries", h.listDeliveries)
	r.With(req("billing.accounting.manage")).Post("/finance/webhook-deliveries/{id}/retry", h.retryDelivery)
}

func reply(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, status, v)
}

func qUUID(r *http.Request, key string) *uuid.UUID {
	v, _ := httpx.QueryUUID(r, key)
	return v
}

func qInt(r *http.Request, key string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(key))
	return n
}

func qDate(r *http.Request, key string) *time.Time {
	if t, err := time.Parse("2006-01-02", r.URL.Query().Get(key)); err == nil {
		return &t
	}
	return nil
}

func withID(fn func(w http.ResponseWriter, r *http.Request, id uuid.UUID)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		fn(w, r, id)
	}
}

func (h *Handler) categories(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"revenue": RevenueCategories, "cost": CostCategories})
}

func (h *Handler) listBudgets(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListBudgets(r.Context(), qUUID(r, "property_id"), qInt(r, "year"))
	reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
}

func (h *Handler) createBudget(w http.ResponseWriter, r *http.Request) {
	var in BudgetInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateBudget(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

func (h *Handler) getBudget(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		out, err := h.Svc.GetBudget(r.Context(), id)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

func (h *Handler) updateBudget(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		var in BudgetInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.UpdateBudget(r.Context(), id, in)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

func (h *Handler) actBudget(action string) http.HandlerFunc {
	return withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		out, err := h.Svc.ActBudget(r.Context(), id, action)
		status := http.StatusOK
		if action == "revise" {
			status = http.StatusCreated
		}
		reply(w, r, status, out, err)
	})
}

func (h *Handler) budgetActual(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.BudgetVsActual(r.Context(), qUUID(r, "property_id"), qInt(r, "year"))
	reply(w, r, http.StatusOK, out, err)
}

func (h *Handler) actualTransactions(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ActualTransactions(r.Context(), qUUID(r, "property_id"), r.URL.Query().Get("kind"), r.URL.Query().Get("category"), qInt(r, "year"), qInt(r, "month"))
	reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
}

func (h *Handler) operatingCosts(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.OperatingCosts(r.Context(), qUUID(r, "property_id"), qInt(r, "year"))
	reply(w, r, http.StatusOK, out, err)
}

func (h *Handler) listCosts(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := CostFilter{PropertyID: qUUID(r, "property_id"), Category: r.URL.Query().Get("category"), From: qDate(r, "from"), To: qDate(r, "to"), Q: r.URL.Query().Get("q")}
	items, next, err := h.Svc.ListCosts(r.Context(), f, page)
	reply(w, r, http.StatusOK, httpx.NewList(items, next), err)
}

func (h *Handler) saveCost(update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in CostInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if !update {
			out, err := h.Svc.SaveCost(r.Context(), nil, in)
			reply(w, r, http.StatusCreated, out, err)
			return
		}
		withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
			out, err := h.Svc.SaveCost(r.Context(), &id, in)
			reply(w, r, http.StatusOK, out, err)
		})(w, r)
	}
}

func (h *Handler) deleteCost(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		if err := h.Svc.DeleteCost(r.Context(), id); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})(w, r)
}

func (h *Handler) listMappings(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListMappings(r.Context(), qUUID(r, "property_id"))
	reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
}

func (h *Handler) putMappings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mappings []MappingInput `json:"mappings"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.PutMappings(r.Context(), qUUID(r, "property_id"), in.Mappings)
	reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
}

func (h *Handler) journal(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.Journal(r.Context(), qUUID(r, "property_id"), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	reply(w, r, http.StatusOK, out, err)
}

func (h *Handler) exportJournal(w http.ResponseWriter, r *http.Request) {
	data, name, ctype, err := h.Svc.ExportJournal(r.Context(), qUUID(r, "property_id"), r.URL.Query().Get("from"), r.URL.Query().Get("to"), r.URL.Query().Get("format"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) listEndpoints(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListEndpoints(r.Context())
	reply(w, r, http.StatusOK, httpx.NewList(items, nil), err)
}

func (h *Handler) saveEndpoint(update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in EndpointInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if !update {
			out, err := h.Svc.SaveEndpoint(r.Context(), nil, in)
			reply(w, r, http.StatusCreated, out, err)
			return
		}
		withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
			out, err := h.Svc.SaveEndpoint(r.Context(), &id, in)
			reply(w, r, http.StatusOK, out, err)
		})(w, r)
	}
}

func (h *Handler) deleteEndpoint(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		if err := h.Svc.DeleteEndpoint(r.Context(), id); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})(w, r)
}

func (h *Handler) testEndpoint(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		out, err := h.Svc.TestEndpoint(r.Context(), id)
		reply(w, r, http.StatusOK, out, err)
	})(w, r)
}

func (h *Handler) listDeliveries(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		page, err := httpx.ParsePage(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		items, next, err := h.Svc.ListDeliveries(r.Context(), id, r.URL.Query().Get("status"), page)
		reply(w, r, http.StatusOK, httpx.NewList(items, next), err)
	})(w, r)
}

func (h *Handler) retryDelivery(w http.ResponseWriter, r *http.Request) {
	withID(func(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
		if err := h.Svc.RetryDelivery(r.Context(), id); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "pending"})
	})(w, r)
}

var _ = apperr.Validation
