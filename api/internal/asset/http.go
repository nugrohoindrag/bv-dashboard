package asset

import (
	"net/http"
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
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.LocationID, _ = httpx.QueryUUID(r, "location_id")
	f.EquipmentID, _ = httpx.QueryUUID(r, "equipment_id")
	f.CategoryCode = r.URL.Query().Get("category")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Criticality = httpx.QueryCSV(r, "criticality")
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
