package metering

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/httpx"
)

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

// Mount: meter & tarif utilitas + pembacaan (PRD P4 v2.1 §5.3).
func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("billing.meters.view")).Get("/billing/utility-tariffs", h.listTariffs)
	r.With(req("billing.meters.manage")).Post("/billing/utility-tariffs", h.saveTariff(false))
	r.With(req("billing.meters.manage")).Patch("/billing/utility-tariffs/{id}", h.saveTariff(true))
	r.With(req("billing.meters.view")).Get("/billing/meters", h.listMeters)
	r.With(req("billing.meters.manage")).Post("/billing/meters", h.saveMeter(false))
	r.With(req("billing.meters.view")).Get("/billing/meters/{id}", h.getMeter)
	r.With(req("billing.meters.manage")).Patch("/billing/meters/{id}", h.saveMeter(true))
	r.With(req("billing.meter_readings.create")).Post("/billing/meters/{id}/readings", h.record)
	r.With(req("billing.meter_readings.view")).Get("/billing/meter-readings", h.listReadings)
	r.With(req("billing.meter_readings.view")).Get("/billing/meter-readings/{id}", h.getReading)
	r.With(req("billing.meter_readings.review")).Post("/billing/meter-readings/{id}/approve", h.review("approve"))
	r.With(req("billing.meter_readings.review")).Post("/billing/meter-readings/{id}/reject", h.review("reject"))
}

func (h *Handler) listTariffs(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, err := h.Svc.ListTariffs(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) saveTariff(update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in TariffInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		st := http.StatusCreated
		if update {
			tid, err := httpx.PathUUID(r, chi.URLParam, "id")
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			out, err := h.Svc.SaveTariff(r.Context(), &tid, in)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, out)
			return
		}
		out, err := h.Svc.SaveTariff(r.Context(), nil, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, st, out)
	}
}

func (h *Handler) listMeters(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f MeterFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.LocationID, _ = httpx.QueryUUID(r, "location_id")
	f.MeterType = r.URL.Query().Get("meter_type")
	f.Status = r.URL.Query().Get("status")
	f.Unread = r.URL.Query().Get("unread") == "true"
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.ListMeters(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) getMeter(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.GetMeter(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) saveMeter(update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in MeterInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if update {
			id, err := httpx.PathUUID(r, chi.URLParam, "id")
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			out, err := h.Svc.SaveMeter(r.Context(), &id, in)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, out)
			return
		}
		out, err := h.Svc.SaveMeter(r.Context(), nil, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, out)
	}
}

func (h *Handler) record(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in ReadingInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in.Source = "web"
	out, err := h.Svc.Record(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) listReadings(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f ReadingFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.MeterID, _ = httpx.QueryUUID(r, "meter_id")
	f.Period = r.URL.Query().Get("period")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.MeterType = r.URL.Query().Get("meter_type")
	items, next, err := h.Svc.ListReadings(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) getReading(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.GetReading(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) review(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var in struct {
			Note string `json:"note"`
		}
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		out, err := h.Svc.Review(r.Context(), id, action, in.Note)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}
