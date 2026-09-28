package security

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/platform/httpx"
)

// mountP2: PRD P2 v2.1 §6.4 Emergency, §6.6 Parking, §6.7 Lost & Found.
func (h *Handler) mountP2(r chi.Router) {
	req, any := h.IAM.Require, h.IAM.RequireAny
	// Emergency (pelapor boleh melihat alert-nya sendiri walau tanpa .view → dicek di service)
	r.With(any("security.emergency_alerts.view", "security.emergency_alerts.raise")).Get("/emergency-alerts", h.listEmergencies)
	r.With(req("security.emergency_alerts.raise")).Post("/emergency-alerts", h.raiseEmergency)
	r.With(any("security.emergency_alerts.view", "security.emergency_alerts.raise")).Get("/emergency-alerts/{id}", h.getEmergency)
	for _, a := range []string{"acknowledge", "respond", "note", "resolve", "cancel"} {
		a := a
		r.With(any("security.emergency_alerts.respond", "security.emergency_alerts.resolve")).Post("/emergency-alerts/{id}/"+a, h.actEmergency(a))
	}
	r.With(any("security.emergency_contacts.view", "security.emergency_alerts.raise")).Get("/emergency-contacts", h.listContacts)
	r.With(req("security.emergency_contacts.manage")).Post("/emergency-contacts", h.saveContact(false))
	r.With(req("security.emergency_contacts.manage")).Patch("/emergency-contacts/{id}", h.saveContact(true))
	r.With(req("security.emergency_contacts.manage")).Delete("/emergency-contacts/{id}", h.deleteContact)

	// Parking
	r.With(req("security.parking.view")).Get("/parking-areas", h.listParkingAreas)
	r.With(req("security.parking.manage")).Post("/parking-areas", h.saveParkingArea(false))
	r.With(req("security.parking.manage")).Patch("/parking-areas/{id}", h.saveParkingArea(true))
	r.With(req("security.parking.view")).Get("/vehicles", h.listVehicles)
	r.With(req("security.parking.manage")).Post("/vehicles", h.saveVehicle(false))
	r.With(req("security.parking.manage")).Patch("/vehicles/{id}", h.saveVehicle(true))
	r.With(req("security.parking.view")).Get("/parking-logs", h.listParkingLogs)
	r.With(req("security.parking.record")).Post("/parking-logs", h.parkingEntry)
	r.With(req("security.parking.record")).Post("/parking-logs/exit", h.parkingExitByPlate)
	r.With(req("security.parking.record")).Post("/parking-logs/{id}/exit", h.parkingExit)
	r.With(req("security.parking.view")).Get("/parking-violations", h.listViolations)
	r.With(req("security.parking.record")).Post("/parking-violations", h.createViolation)
	r.With(req("security.parking.view")).Get("/parking-violations/{id}", h.getViolation)
	for _, a := range []string{"update", "resolve", "create_incident"} {
		a := a
		path := "/parking-violations/{id}/" + strings.ReplaceAll(a, "_", "-")
		r.With(req("security.parking.view")).Post(path, h.actViolation(a))
	}
	// PRD P3 v2.1 §5.8: izin parkir tenant (persetujuan Security/BM) + layanan Tenant App di atas entitas P2
	r.With(req("security.parking_permits.view")).Get("/parking-permits", h.listPermits)
	r.With(req("security.parking_permits.view")).Get("/parking-permits/{id}", h.getPermit)
	for _, a := range []string{"approve", "reject", "revoke", "update"} {
		r.With(req("security.parking_permits.approve")).Post("/parking-permits/{id}/"+a, h.actPermit(a))
	}
	r.With(req("tenant_app.parking.view")).Get("/tenant/vehicles", h.tenantVehicles)
	r.With(req("tenant_app.parking.create")).Post("/tenant/vehicles", h.tenantRegisterVehicle)
	r.With(req("tenant_app.parking.view")).Get("/tenant/vehicles/{id}", h.tenantVehicle)
	r.With(req("tenant_app.parking.create")).Patch("/tenant/vehicles/{id}", h.tenantUpdateVehicle)
	r.With(req("tenant_app.parking.cancel")).Delete("/tenant/vehicles/{id}", h.tenantRemoveVehicle)
	r.With(req("tenant_app.parking.view")).Get("/tenant/parking-areas", h.tenantParkingAreas)
	r.With(req("tenant_app.parking.view")).Get("/tenant/parking-permits", h.tenantPermits)
	r.With(req("tenant_app.parking.create")).Post("/tenant/parking-permits", h.tenantRequestPermit)
	r.With(req("tenant_app.parking.view")).Get("/tenant/parking-permits/{id}", h.tenantPermit)
	r.With(req("tenant_app.parking.cancel")).Post("/tenant/parking-permits/{id}/cancel", h.tenantCancelPermit)

	// Lost & Found
	r.With(req("security.lost_found.view")).Get("/lost-found/items", h.listLostFound)
	r.With(req("security.lost_found.create")).Post("/lost-found/items", h.createLostFound)
	r.With(req("security.lost_found.view")).Get("/lost-found/items/{id}", h.getLostFound)
	for _, a := range []string{"update", "return", "match", "dispose"} {
		a := a
		r.With(req("security.lost_found.view")).Post("/lost-found/items/{id}/"+a, h.actLostFound(a))
	}
	r.With(req("security.lost_found.view")).Get("/lost-found/reports", h.listLostReports)
	r.With(req("security.lost_found.create")).Post("/lost-found/reports", h.createLostReport)
	r.With(req("security.lost_found.view")).Get("/lost-found/reports/{id}/matches", h.lostFoundMatches)
}

func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return uuid.Nil, false
	}
	return id, true
}

func decodeOr(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.ContentLength == 0 {
		return true
	}
	if err := httpx.Decode(r, v); err != nil {
		httpx.WriteError(w, r, err)
		return false
	}
	return true
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

func boolParam(r *http.Request, name string) *bool {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil
	}
	b := v == "true" || v == "1"
	return &b
}

// ---------- Emergency ----------

func (h *Handler) listEmergencies(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f EmergencyFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.Statuses = splitCSV(r.URL.Query().Get("status"))
	f.Active = boolParam(r, "active")
	f.Mine = r.URL.Query().Get("mine") == "true"
	f.From, _ = httpx.QueryTime(r, "from")
	f.To, _ = httpx.QueryTime(r, "to")
	items, next, err := h.Svc.ListEmergencies(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) raiseEmergency(w http.ResponseWriter, r *http.Request) {
	var in RaiseEmergencyInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.Svc.RaiseEmergency(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, a)
}

func (h *Handler) getEmergency(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a, err := h.Svc.GetEmergency(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a)
}

func (h *Handler) actEmergency(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in EmergencyActionInput
		if !decodeOr(w, r, &in) {
			return
		}
		a, err := h.Svc.ActEmergency(r.Context(), id, action, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, a)
	}
}

func (h *Handler) listContacts(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, err := h.Svc.ListEmergencyContacts(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) saveContact(update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id *uuid.UUID
		if update {
			v, ok := pathID(w, r)
			if !ok {
				return
			}
			id = &v
		}
		var in EmergencyContactInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		c, err := h.Svc.SaveEmergencyContact(r.Context(), id, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		st := http.StatusCreated
		if update {
			st = http.StatusOK
		}
		httpx.WriteJSON(w, st, c)
	}
}

func (h *Handler) deleteContact(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.Svc.DeleteEmergencyContact(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Parking ----------

func (h *Handler) listParkingAreas(w http.ResponseWriter, r *http.Request) {
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, err := h.Svc.ListParkingAreas(r.Context(), pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) saveParkingArea(update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id *uuid.UUID
		if update {
			v, ok := pathID(w, r)
			if !ok {
				return
			}
			id = &v
		}
		var in ParkingAreaInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		a, err := h.Svc.SaveParkingArea(r.Context(), id, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		st := http.StatusCreated
		if update {
			st = http.StatusOK
		}
		httpx.WriteJSON(w, st, a)
	}
}

func (h *Handler) listVehicles(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f VehicleFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.Q = r.URL.Query().Get("q")
	f.OwnerType = r.URL.Query().Get("owner_type")
	f.Status = r.URL.Query().Get("status")
	items, next, err := h.Svc.ListVehicles(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) saveVehicle(update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id *uuid.UUID
		if update {
			v, ok := pathID(w, r)
			if !ok {
				return
			}
			id = &v
		}
		var in VehicleInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		v, err := h.Svc.SaveVehicle(r.Context(), id, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		st := http.StatusCreated
		if update {
			st = http.StatusOK
		}
		httpx.WriteJSON(w, st, v)
	}
}

func (h *Handler) listParkingLogs(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f ParkingLogFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.ParkingAreaID, _ = httpx.QueryUUID(r, "parking_area_id")
	f.Inside = boolParam(r, "inside")
	f.Plate = r.URL.Query().Get("plate")
	f.From, _ = httpx.QueryTime(r, "from")
	f.To, _ = httpx.QueryTime(r, "to")
	items, next, err := h.Svc.ListParkingLogs(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) parkingEntry(w http.ResponseWriter, r *http.Request) {
	var in ParkingEntryInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	l, err := h.Svc.RecordParkingEntry(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, l)
}

func (h *Handler) parkingExit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Note *string `json:"note"`
	}
	if !decodeOr(w, r, &in) {
		return
	}
	l, err := h.Svc.RecordParkingExit(r.Context(), &id, nil, "", in.Note)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, l)
}

func (h *Handler) parkingExitByPlate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PropertyID  *uuid.UUID `json:"property_id"`
		PlateNumber string     `json:"plate_number"`
		Note        *string    `json:"note"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	l, err := h.Svc.RecordParkingExit(r.Context(), nil, in.PropertyID, in.PlateNumber, in.Note)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, l)
}

func (h *Handler) listViolations(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f ViolationFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.Statuses = splitCSV(r.URL.Query().Get("status"))
	f.Plate = r.URL.Query().Get("plate")
	f.From, _ = httpx.QueryTime(r, "from")
	f.To, _ = httpx.QueryTime(r, "to")
	items, next, err := h.Svc.ListParkingViolations(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) createViolation(w http.ResponseWriter, r *http.Request) {
	var in ParkingViolationInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	v, err := h.Svc.CreateParkingViolation(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, v)
}

func (h *Handler) getViolation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := h.Svc.GetParkingViolation(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (h *Handler) actViolation(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in ViolationActionInput
		if !decodeOr(w, r, &in) {
			return
		}
		v, err := h.Svc.ActParkingViolation(r.Context(), id, action, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, v)
	}
}

// ---------- Lost & Found ----------

func (h *Handler) listLostFound(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f LostFoundFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.Statuses = splitCSV(r.URL.Query().Get("status"))
	f.Category = r.URL.Query().Get("category")
	f.DisposalDue = r.URL.Query().Get("disposal_due") == "true"
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.ListLostFoundItems(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) createLostFound(w http.ResponseWriter, r *http.Request) {
	var in LostFoundItemInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	x, err := h.Svc.CreateLostFoundItem(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, x)
}

func (h *Handler) getLostFound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	x, err := h.Svc.GetLostFoundItem(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, x)
}

func (h *Handler) actLostFound(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var in LostFoundActionInput
		if !decodeOr(w, r, &in) {
			return
		}
		x, err := h.Svc.ActLostFoundItem(r.Context(), id, action, in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, x)
	}
}

func (h *Handler) listLostReports(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pid, _ := httpx.QueryUUID(r, "property_id")
	items, next, err := h.Svc.ListLostReports(r.Context(), pid, splitCSV(r.URL.Query().Get("status")), page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) createLostReport(w http.ResponseWriter, r *http.Request) {
	var in LostReportInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	x, err := h.Svc.CreateLostReport(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, x)
}

func (h *Handler) lostFoundMatches(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	items, err := h.Svc.LostFoundMatches(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}
