package hotel

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

// Mount: /hotel/* (NC §48; brief §17) — room-types, rooms, rates, availability, reservations, calendar, occupancy.
func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("hotel.room_types.view")).Get("/hotel/room-types", h.listRoomTypes)
	r.With(req("hotel.room_types.create")).Post("/hotel/room-types", h.createRoomType)
	r.With(req("hotel.room_types.update")).Patch("/hotel/room-types/{id}", h.updateRoomType)
	r.With(req("hotel.rooms.view")).Get("/hotel/rooms", h.listRooms)
	r.With(req("hotel.rooms.create")).Post("/hotel/rooms", h.createRoom)
	r.With(req("hotel.rooms.set_status")).Post("/hotel/rooms/{id}/status", h.setRoomStatus)
	r.With(req("hotel.rates.view")).Get("/hotel/rates", h.listRates)
	r.With(req("hotel.rates.create")).Post("/hotel/rates", h.createRate)
	r.With(req("hotel.rates.update")).Patch("/hotel/rates/{id}", h.updateRate)
	r.With(req("hotel.reservations.view")).Get("/hotel/availability", h.availability)
	r.With(req("hotel.reservations.view")).Get("/hotel/reservations", h.list)
	r.With(req("hotel.reservations.create")).Post("/hotel/reservations", h.create)
	r.With(req("hotel.reservations.view")).Get("/hotel/reservations/calendar", h.calendar)
	r.With(req("hotel.reservations.view")).Get("/hotel/reservations/{id}", h.get)
	r.With(req("hotel.reservations.update")).Patch("/hotel/reservations/{id}", h.update)
	for _, a := range []string{"confirm", "assign_room", "check_in", "check_out", "cancel", "no_show"} {
		r.With(req("hotel.reservations.view")).Post("/hotel/reservations/{id}/"+a, h.act(a)) // permission per aksi via allowed_actions
	}
	r.With(req("hotel.rooms.view")).Get("/hotel/occupancy", h.occupancy)
}

func propertyParam(r *http.Request) (uuid.UUID, error) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil || pid == nil {
		return uuid.Nil, apperr.Validation("property_id wajib")
	}
	return *pid, nil
}

func dateQ(r *http.Request, key string) (time.Time, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return time.Time{}, apperr.Validation(key + " wajib (YYYY-MM-DD)")
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return time.Time{}, apperr.Validation(key + " harus YYYY-MM-DD")
	}
	return t, nil
}

func (h *Handler) listRoomTypes(w http.ResponseWriter, r *http.Request) {
	pid, err := propertyParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.ListRoomTypes(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createRoomType(w http.ResponseWriter, r *http.Request) {
	var in RoomTypeInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateRoomType(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateRoomType(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in RoomTypeInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateRoomType(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listRooms(w http.ResponseWriter, r *http.Request) {
	pid, err := propertyParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	rt, _ := httpx.QueryUUID(r, "room_type_id")
	items, err := h.Svc.ListRooms(r.Context(), pid, rt, httpx.QueryCSV(r, "status"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createRoom(w http.ResponseWriter, r *http.Request) {
	var in RoomInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateRoom(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) setRoomStatus(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in RoomStatusInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.SetRoomStatus(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listRates(w http.ResponseWriter, r *http.Request) {
	pid, err := propertyParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	rt, _ := httpx.QueryUUID(r, "room_type_id")
	items, err := h.Svc.ListRates(r.Context(), pid, rt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createRate(w http.ResponseWriter, r *http.Request) {
	var in RateInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateRate(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateRate(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in RateInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateRate(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) availability(w http.ResponseWriter, r *http.Request) {
	pid, err := propertyParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ci, err := dateQ(r, "check_in")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	co, err := dateQ(r, "check_out")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	rt, _ := httpx.QueryUUID(r, "room_type_id")
	items, err := h.Svc.Availability(r.Context(), pid, ci, co, rt)
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
	f.RoomTypeID, _ = httpx.QueryUUID(r, "room_type_id")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Q = r.URL.Query().Get("q")
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.From = &t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.To = &t
		}
	}
	if v := r.URL.Query().Get("arrival_date"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.ArrivalDate = &t
		}
	}
	items, next, err := h.Svc.List(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in ReservationInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateReservation(r.Context(), in)
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
	var in ReservationInput
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

func (h *Handler) calendar(w http.ResponseWriter, r *http.Request) {
	pid, err := propertyParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	from, err := dateQ(r, "from")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	to, err := dateQ(r, "to")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CalendarView(r.Context(), pid, from, to)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) occupancy(w http.ResponseWriter, r *http.Request) {
	pid, err := propertyParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.OccupancyView(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
