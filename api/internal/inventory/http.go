package inventory

import (
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

func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("inventory.items.view")).Get("/inventory/items", h.listItems)
	r.With(req("inventory.items.create")).Post("/inventory/items", h.createItem)
	r.With(req("inventory.items.view")).Get("/inventory/items/{id}", h.getItem)
	r.With(req("inventory.items.update")).Patch("/inventory/items/{id}", h.updateItem)
	r.With(req("inventory.stock_locations.view")).Get("/inventory/stock-locations", h.listLocations)
	r.With(req("inventory.stock_locations.create")).Post("/inventory/stock-locations", h.createLocation)
	r.With(req("inventory.stock_locations.update")).Patch("/inventory/stock-locations/{id}", h.updateLocation)
	r.With(req("inventory.transactions.view")).Get("/inventory/stock-transactions", h.listTx)
	r.With(h.IAM.RequireAny("inventory.transactions.create", "inventory.stock.adjust")).Post("/inventory/stock-transactions", h.createTx)
	// Parts Usage against Work Order (Mobile Staff & dashboard)
	r.With(req("operations.work_orders.view")).Get("/work-orders/{id}/parts", h.listParts)
	r.With(req("inventory.parts_usage.create")).Post("/work-orders/{id}/parts", h.addPart)
	r.With(req("inventory.parts_usage.create")).Delete("/work-orders/{id}/parts/{partId}", h.removePart)
}

func (h *Handler) listItems(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	q := r.URL.Query()
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, next, err := h.Svc.ListItems(r.Context(), q.Get("q"), q.Get("category"), pid, q.Get("low_stock") == "true", page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) createItem(w http.ResponseWriter, r *http.Request) {
	var in ItemInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateItem(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) getItem(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.GetItem(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) updateItem(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in ItemInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateItem(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listLocations(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, err := h.Svc.ListStockLocations(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createLocation(w http.ResponseWriter, r *http.Request) {
	var in StockLocationInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateStockLocation(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateLocation(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in StockLocationInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateStockLocation(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listTx(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f TxFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.ItemID, _ = httpx.QueryUUID(r, "item_id")
	f.StockLocationID, _ = httpx.QueryUUID(r, "stock_location_id")
	f.Types = httpx.QueryCSV(r, "type")
	f.From, _ = httpx.QueryTime(r, "from")
	f.To, _ = httpx.QueryTime(r, "to")
	items, next, err := h.Svc.ListTransactions(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) createTx(w http.ResponseWriter, r *http.Request) {
	var in TransactionInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateTransaction(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) listParts(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, total, err := h.Svc.ListParts(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": items, "total_cost": total, "next_cursor": nil})
}

func (h *Handler) addPart(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in PartUsageInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.AddPart(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) removePart(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pid, err := uuid.Parse(chi.URLParam(r, "partId"))
	if err != nil {
		httpx.WriteError(w, r, apperr.Validation("partId tidak valid"))
		return
	}
	if err := h.Svc.RemovePart(r.Context(), id, pid); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
