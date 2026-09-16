package booking

import (
	"net/http"
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

// Mount: staf (dashboard) + tenant (`/tenant/facilities`, `/tenant/bookings`).
func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("booking.facilities.view")).Get("/facilities", h.listFacilities)
	r.With(req("booking.facilities.create")).Post("/facilities", h.createFacility)
	r.With(req("booking.facilities.view")).Get("/facilities/{id}", h.getFacility)
	r.With(req("booking.facilities.update")).Patch("/facilities/{id}", h.updateFacility)
	r.With(req("booking.facilities.delete")).Delete("/facilities/{id}", h.deleteFacility)
	r.With(req("booking.facilities.view")).Get("/facilities/{id}/schedules", h.listSchedules)
	r.With(req("booking.facilities.update")).Post("/facilities/{id}/schedules", h.addSchedule)
	r.With(req("booking.facilities.update")).Delete("/facilities/{id}/schedules/{sid}", h.deleteSchedule)
	r.With(req("booking.bookings.view")).Get("/facilities/{id}/availability", h.availability)
	r.With(req("booking.bookings.view")).Get("/bookings", h.list)
	r.With(req("booking.bookings.create")).Post("/bookings", h.create)
	r.With(req("booking.bookings.view")).Get("/bookings/{id}", h.get)
	for _, a := range []string{"approve", "reject", "cancel", "check_in", "complete", "no_show"} {
		r.Post("/bookings/{id}/"+a, h.act(a)) // permission per aksi dicek di service (allowed_actions)
	}
	// Mobile Tenant
	r.With(req("tenant_app.facilities.view")).Get("/tenant/facilities", h.tenantFacilities)
	r.With(req("tenant_app.facilities.view")).Get("/tenant/facilities/{id}/availability", h.tenantAvailability)
	r.With(req("tenant_app.bookings.view")).Get("/tenant/bookings", h.tenantList)
	r.With(req("tenant_app.bookings.create")).Post("/tenant/bookings", h.tenantCreate)
	r.With(req("tenant_app.bookings.view")).Get("/tenant/bookings/{id}", h.tenantGet)
	r.With(req("tenant_app.bookings.cancel")).Post("/tenant/bookings/{id}/cancel", h.tenantCancel)
}

func pathID(r *http.Request) (uuid.UUID, error) { return httpx.PathUUID(r, chi.URLParam, "id") }

func dateParam(r *http.Request) (time.Time, error) {
	v := r.URL.Query().Get("date")
	if v == "" {
		return time.Now(), nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return time.Time{}, apperr.Validation("date harus YYYY-MM-DD")
	}
	return t, nil
}

func (h *Handler) listFacilities(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, err := h.Svc.ListFacilities(r.Context(), pid, r.URL.Query().Get("active") == "true")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createFacility(w http.ResponseWriter, r *http.Request) {
	var in FacilityInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateFacility(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) getFacility(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.GetFacility(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) updateFacility(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in FacilityInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateFacility(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteFacility(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.DeleteFacility(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.ListSchedules(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) addSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in ScheduleInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.AddSchedule(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sid, err := uuid.Parse(chi.URLParam(r, "sid"))
	if err != nil {
		httpx.WriteError(w, r, apperr.Validation("sid tidak valid"))
		return
	}
	if err := h.Svc.DeleteSchedule(r.Context(), id, sid); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) availability(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	d, err := dateParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.Availability(r.Context(), id, d)
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
	var f Filter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.FacilityID, _ = httpx.QueryUUID(r, "facility_id")
	f.TenantID, _ = httpx.QueryUUID(r, "tenant_id")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.From, _ = httpx.QueryTime(r, "from")
	f.To, _ = httpx.QueryTime(r, "to")
	f.Upcoming = r.URL.Query().Get("upcoming") == "true"
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.List(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateBookingInput
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

// ---- tenant ----

func (h *Handler) tenantFacilities(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.TenantFacilities(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) tenantAvailability(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	d, err := dateParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.TenantAvailability(r.Context(), id, d)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) tenantList(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var upcoming *bool
	if v := r.URL.Query().Get("upcoming"); v != "" {
		b := v == "true"
		upcoming = &b
	}
	items, next, err := h.Svc.TenantList(r.Context(), upcoming, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) tenantCreate(w http.ResponseWriter, r *http.Request) {
	var in CreateBookingInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.TenantCreate(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) tenantGet(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.TenantGet(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) tenantCancel(w http.ResponseWriter, r *http.Request) {
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
	out, err := h.Svc.TenantCancel(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
