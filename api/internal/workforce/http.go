package workforce

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/httpx"
)

func httpxValidation(msg string) error { return apperr.Validation(msg) }

type Handler struct {
	Svc *Service
	IAM *iam.Service
}

// Mount: shift per domain (/security/*, /housekeeping/*), attendance saya, kompetensi, kapasitas.
func (h *Handler) Mount(r chi.Router) {
	req, any := h.IAM.Require, h.IAM.RequireAny
	for _, d := range []string{"security", "housekeeping"} {
		d := d
		view, manage := d+".shifts.view", d+".shifts.manage"
		r.With(req(view)).Get("/"+d+"/shifts", h.listShifts(d))
		r.With(req(manage)).Post("/"+d+"/shifts", h.saveShift(d, false))
		r.With(req(manage)).Patch("/"+d+"/shifts/{id}", h.saveShift(d, true))
		r.With(req(view)).Get("/"+d+"/roster", h.listRoster(d))
		r.With(req(manage)).Post("/"+d+"/roster", h.assignRoster(d))
		r.With(req(manage)).Post("/"+d+"/roster/copy-week", h.copyWeek(d))
		r.With(req(manage)).Delete("/"+d+"/roster/{id}", h.cancelRoster(d))
		r.With(any(view, "overview.dashboard.view")).Get("/"+d+"/on-duty", h.onDuty(d))
		r.With(req(view)).Get("/"+d+"/attendance", h.listAttendance(d))
		r.With(req(view)).Get("/"+d+"/staff", h.listStaff(d))
		r.With(req(view)).Get("/"+d+"/handovers", h.listHandovers(d))
		r.With(req(view)).Get("/"+d+"/handovers/draft", h.handoverDraft(d))
		r.With(req(view)).Get("/"+d+"/handovers/{id}", h.getHandover(d))
		r.With(req(view)).Post("/"+d+"/handovers", h.createHandover(d))
		r.With(req(view)).Post("/"+d+"/handovers/{id}/acknowledge", h.ackHandover(d))
	}
	// Staff App: roster & attendance saya (P2-MOB-04)
	r.Get("/me/roster", h.myRoster)
	r.Get("/me/attendance", h.myAttendance)
	r.With(req("workforce.attendance.clock")).Post("/me/attendance/clock-in", h.clockIn)
	r.With(req("workforce.attendance.clock")).Post("/me/attendance/clock-out", h.clockOut)
	// Kapasitas tim lintas domain (Overview)
	r.With(req("overview.dashboard.view")).Get("/overview/workforce-capacity", h.capacity)
}

func pid(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := httpx.PathUUID(r, chi.URLParam, name)
	if err != nil {
		httpx.WriteError(w, r, err)
		return uuid.Nil, false
	}
	return id, true
}

func propertyParam(r *http.Request) *uuid.UUID {
	v, _ := httpx.QueryUUID(r, "property_id")
	return v
}

func decodeOpt(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.ContentLength == 0 {
		return true
	}
	if err := httpx.Decode(r, v); err != nil {
		httpx.WriteError(w, r, err)
		return false
	}
	return true
}

func write(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, status, v)
}

func (h *Handler) listShifts(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := h.Svc.ListShifts(r.Context(), d, propertyParam(r))
		write(w, r, http.StatusOK, httpx.NewList(out, nil), err)
	}
}

func (h *Handler) saveShift(d string, update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id *uuid.UUID
		if update {
			v, ok := pid(w, r, "id")
			if !ok {
				return
			}
			id = &v
		}
		var in ShiftInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if update {
			in.IfVersion = httpx.IfMatchVersion(r)
		}
		out, err := h.Svc.SaveShift(r.Context(), d, id, in)
		st := http.StatusCreated
		if update {
			st = http.StatusOK
		}
		write(w, r, st, out, err)
	}
}

func (h *Handler) listRoster(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := RosterFilter{PropertyID: propertyParam(r), From: r.URL.Query().Get("from"), To: r.URL.Query().Get("to")}
		f.UserID, _ = httpx.QueryUUID(r, "user_id")
		f.ShiftID, _ = httpx.QueryUUID(r, "shift_id")
		out, err := h.Svc.ListRoster(r.Context(), d, f)
		write(w, r, http.StatusOK, httpx.NewList(out, nil), err)
	}
}

func (h *Handler) assignRoster(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in RosterAssignInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.AssignRoster(r.Context(), d, in)
		write(w, r, http.StatusCreated, out, err)
	}
}

func (h *Handler) copyWeek(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in CopyWeekInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.CopyWeek(r.Context(), d, in)
		write(w, r, http.StatusOK, out, err)
	}
}

func (h *Handler) cancelRoster(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pid(w, r, "id")
		if !ok {
			return
		}
		if err := h.Svc.CancelRoster(r.Context(), d, id); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) onDuty(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := h.Svc.OnDuty(r.Context(), d, propertyParam(r))
		write(w, r, http.StatusOK, out, err)
	}
}

func (h *Handler) listAttendance(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := h.Svc.ListAttendance(r.Context(), d, propertyParam(r), r.URL.Query().Get("date"))
		write(w, r, http.StatusOK, httpx.NewList(out, nil), err)
	}
}

func (h *Handler) listHandovers(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		out, err := h.Svc.ListHandovers(r.Context(), d, propertyParam(r), limit)
		write(w, r, http.StatusOK, httpx.NewList(out, nil), err)
	}
}

func (h *Handler) getHandover(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pid(w, r, "id")
		if !ok {
			return
		}
		out, err := h.Svc.GetHandover(r.Context(), d, id)
		write(w, r, http.StatusOK, out, err)
	}
}

func (h *Handler) handoverDraft(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := propertyParam(r)
		if p == nil {
			httpx.WriteError(w, r, httpxValidation("property_id wajib"))
			return
		}
		out, err := h.Svc.HandoverDraft(r.Context(), d, *p)
		write(w, r, http.StatusOK, out, err)
	}
}

func (h *Handler) createHandover(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in HandoverInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.CreateHandover(r.Context(), d, in)
		write(w, r, http.StatusCreated, out, err)
	}
}

func (h *Handler) ackHandover(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pid(w, r, "id")
		if !ok {
			return
		}
		out, err := h.Svc.AcknowledgeHandover(r.Context(), d, id)
		write(w, r, http.StatusOK, out, err)
	}
}

func (h *Handler) myRoster(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.MyRoster(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	write(w, r, http.StatusOK, httpx.NewList(out, nil), err)
}

func (h *Handler) myAttendance(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.MyAttendance(r.Context())
	write(w, r, http.StatusOK, out, err)
}

func (h *Handler) clockIn(w http.ResponseWriter, r *http.Request) {
	var in ClockInput
	if !decodeOpt(w, r, &in) {
		return
	}
	out, err := h.Svc.ClockIn(r.Context(), in)
	write(w, r, http.StatusCreated, out, err)
}

func (h *Handler) clockOut(w http.ResponseWriter, r *http.Request) {
	var in ClockInput
	if !decodeOpt(w, r, &in) {
		return
	}
	out, err := h.Svc.ClockOut(r.Context(), in)
	write(w, r, http.StatusOK, out, err)
}

func (h *Handler) listStaff(d string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		team, err := httpx.QueryUUID(r, "team_id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out, err := h.Svc.ListDomainStaff(r.Context(), d, propertyParam(r), team)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, httpx.NewList(out, nil))
	}
}

func (h *Handler) capacity(w http.ResponseWriter, r *http.Request) {
	out, err := h.Svc.Capacity(r.Context(), propertyParam(r))
	write(w, r, http.StatusOK, out, err)
}
