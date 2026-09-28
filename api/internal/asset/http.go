package asset

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("engineering.equipment.view")).Get("/equipment", h.listEquipment)
	r.With(req("engineering.equipment.create")).Post("/equipment", h.createEquipment)
	r.With(req("engineering.equipment.update")).Patch("/equipment/{id}", h.updateEquipment)

	r.With(req("engineering.assets.view")).Get("/assets", h.list)
	r.With(req("engineering.assets.create")).Post("/assets", h.create)
	r.With(req("engineering.assets.view")).Get("/assets/{id}", h.get)
	r.With(req("engineering.assets.update")).Patch("/assets/{id}", h.update)
	r.With(req("engineering.assets.view")).Get("/assets/{id}/history", h.history)
	r.With(req("engineering.assets.update")).Post("/assets/{id}/qr/rotate", h.rotateQR("asset"))
	// PRD P2 v2.1 §5.5–§5.6: health, Asset 360 (biaya/parts/vendor/PM compliance), dokumen equipment
	r.With(req("engineering.assets.view")).Get("/assets/{id}/health", h.health(false))
	r.With(req("engineering.assets.update")).Post("/assets/{id}/health/recompute", h.health(true))
	r.With(req("engineering.assets.view")).Get("/assets/{id}/insight", h.insight)
	r.With(req("engineering.asset_documents.view")).Get("/assets/{id}/documents", h.listDocuments)
	r.With(req("engineering.asset_documents.create")).Post("/assets/{id}/documents", h.createDocument)
	r.With(req("engineering.asset_documents.update")).Patch("/asset-documents/{id}", h.updateDocument)
	r.With(req("engineering.asset_documents.delete")).Delete("/asset-documents/{id}", h.deleteDocument)
	r.With(req("engineering.asset_documents.view")).Get("/asset-documents/expiring", h.expiringDocuments)

	r.Get("/qr/{code}/resolve", h.resolveQR)
}

func (h *Handler) listEquipment(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListEquipment(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createEquipment(w http.ResponseWriter, r *http.Request) {
	var in EquipmentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	e, err := h.Svc.CreateEquipment(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, e)
}

func (h *Handler) updateEquipment(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in EquipmentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	e, err := h.Svc.UpdateEquipment(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, e)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f Filter
	// PRD P0 v2 §17.2: filter tidak valid → 400; lokasi seragam (building/tower/floor/area/unit, subtree)
	if f.PropertyID, err = httpx.QueryUUID(r, "property_id"); err == nil {
		if f.LocationID, err = httpx.QueryLocation(r); err == nil {
			f.EquipmentID, err = httpx.QueryUUID(r, "equipment_id")
		}
	}
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f.CategoryCode = r.URL.Query().Get("category")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Criticality = httpx.QueryCSV(r, "criticality")
	f.HealthStatuses = httpx.QueryCSV(r, "health_status")
	f.AtRisk = r.URL.Query().Get("at_risk") == "true"
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.List(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in AssetInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.Svc.CreateAsset(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, a)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in AssetInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.Svc.UpdateAsset(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a)
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.History(r.Context(), id, 200)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) rotateQR(objectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		code, err := h.Svc.RotateQR(r.Context(), objectType, id)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"qr_code": code, "qr_url": qrBase + code})
	}
}

func (h *Handler) resolveQR(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	// terima juga URL penuh https://bv.link/q/{code}
	if i := strings.LastIndex(code, "/"); i >= 0 {
		code = code[i+1:]
	}
	out, err := h.Svc.ResolveQR(r.Context(), code)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

var _ = uuid.Nil

func (h *Handler) health(force bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.Health(r.Context(), id, force)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *Handler) insight(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.Insight(r.Context(), id, r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listDocuments(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.ListDocuments(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(out, nil))
}

func (h *Handler) createDocument(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in AssetDocumentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateDocument(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateDocument(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in AssetDocumentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateDocument(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteDocument(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.DeleteDocument(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) expiringDocuments(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	out, err := h.Svc.ExpiringDocuments(r.Context(), pid, days)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(out, nil))
}
