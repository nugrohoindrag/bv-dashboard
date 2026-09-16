package commercial

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

// Mount: /unit-sales/* dan /unit-rental/* (NC §48 kebab-case; permission module commercial).
func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	// Unit Sales Management
	r.With(req("commercial.unit_listings.view")).Get("/unit-sales/listings", h.listListings)
	r.With(req("commercial.unit_listings.create")).Post("/unit-sales/listings", h.createListing)
	r.With(req("commercial.unit_listings.view")).Get("/unit-sales/listings/{id}", h.getListing)
	r.With(req("commercial.unit_listings.update")).Patch("/unit-sales/listings/{id}", h.updateListing)
	for _, a := range []string{"publish", "unpublish", "archive"} {
		r.With(req("commercial.unit_listings.view")).Post("/unit-sales/listings/{id}/"+a, h.listingAct(a)) // izin per aksi di service
	}
	r.With(req("commercial.sales_leads.view")).Get("/unit-sales/leads", h.listLeads)
	r.With(req("commercial.sales_leads.create")).Post("/unit-sales/leads", h.createLead)
	r.With(req("commercial.sales_leads.view")).Get("/unit-sales/leads/{id}", h.getLead)
	r.With(req("commercial.sales_leads.update")).Patch("/unit-sales/leads/{id}", h.updateLead)
	for _, a := range []string{"contact", "qualify", "lose", "cancel", "reopen"} {
		r.With(req("commercial.sales_leads.update")).Post("/unit-sales/leads/{id}/"+a, h.leadAct(a))
	}
	r.With(req("commercial.sales_leads.view")).Get("/unit-sales/leads/{id}/activities", h.listActivities)
	r.With(req("commercial.sales_leads.update")).Post("/unit-sales/leads/{id}/activities", h.addActivity)
	r.With(req("commercial.sale_reservations.view")).Get("/unit-sales/reservations", h.listSaleRes)
	r.With(req("commercial.sale_reservations.create")).Post("/unit-sales/reservations", h.createSaleRes)
	r.With(req("commercial.sale_reservations.view")).Get("/unit-sales/reservations/{id}", h.getSaleRes)
	r.With(req("commercial.sale_reservations.update")).Patch("/unit-sales/reservations/{id}", h.updateSaleRes)
	for _, a := range []string{"sign_contract", "complete", "handover", "cancel"} {
		r.With(req("commercial.sale_reservations.view")).Post("/unit-sales/reservations/{id}/"+a, h.saleAct(a))
	}
	r.With(req("commercial.sales_leads.view")).Get("/unit-sales/summary", h.salesSummary)
	// Unit Rental Management
	r.With(req("commercial.rental_listings.view")).Get("/unit-rental/listings", h.listRL)
	r.With(req("commercial.rental_listings.create")).Post("/unit-rental/listings", h.createRL)
	r.With(req("commercial.rental_listings.view")).Get("/unit-rental/listings/{id}", h.getRL)
	r.With(req("commercial.rental_listings.update")).Patch("/unit-rental/listings/{id}", h.updateRL)
	for _, a := range []string{"publish", "unpublish", "archive"} {
		r.With(req("commercial.rental_listings.view")).Post("/unit-rental/listings/{id}/"+a, h.rlAct(a))
	}
	r.With(req("commercial.rental_reservations.view")).Get("/unit-rental/availability", h.availability)
	r.With(req("commercial.rental_reservations.view")).Get("/unit-rental/reservations", h.listRR)
	r.With(req("commercial.rental_reservations.create")).Post("/unit-rental/reservations", h.createRR)
	r.With(req("commercial.rental_reservations.view")).Get("/unit-rental/reservations/calendar", h.rentalCalendar)
	r.With(req("commercial.rental_reservations.view")).Get("/unit-rental/reservations/{id}", h.getRR)
	r.With(req("commercial.rental_reservations.update")).Patch("/unit-rental/reservations/{id}", h.updateRR)
	for _, a := range []string{"confirm", "activate", "complete", "cancel"} {
		r.With(req("commercial.rental_reservations.view")).Post("/unit-rental/reservations/{id}/"+a, h.rentalAct(a))
	}
	r.With(req("commercial.rental_reservations.view")).Get("/unit-rental/summary", h.rentalSummary)
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
	return parseDate(v, key)
}

func optDateQ(r *http.Request, key string) *time.Time {
	if v := r.URL.Query().Get(key); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			return &t
		}
	}
	return nil
}

func idParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return uuid.Nil, false
	}
	return id, true
}

func respond(w http.ResponseWriter, r *http.Request, status int, out any, err error) {
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, status, out)
}

// ---------- listings ----------

func (h *Handler) listListings(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f ListingFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.ListListings(r.Context(), f, page)
	respond(w, r, http.StatusOK, httpx.NewList(items, next), err)
}

func (h *Handler) createListing(w http.ResponseWriter, r *http.Request) {
	var in ListingInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateListing(r.Context(), in)
	respond(w, r, http.StatusCreated, out, err)
}

func (h *Handler) getListing(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	out, err := h.Svc.GetListing(r.Context(), id)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) updateListing(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var in ListingInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateListing(r.Context(), id, in, httpx.IfMatchVersion(r))
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) listingAct(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		out, err := h.Svc.ListingAct(r.Context(), id, action)
		respond(w, r, http.StatusOK, out, err)
	}
}

// ---------- leads ----------

func (h *Handler) listLeads(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f LeadFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.ListingID, _ = httpx.QueryUUID(r, "listing_id")
	f.AssignedTo, _ = httpx.QueryUUID(r, "assigned_to")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.ListLeads(r.Context(), f, page)
	respond(w, r, http.StatusOK, httpx.NewList(items, next), err)
}

func (h *Handler) createLead(w http.ResponseWriter, r *http.Request) {
	var in LeadInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateLead(r.Context(), in)
	respond(w, r, http.StatusCreated, out, err)
}

func (h *Handler) getLead(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	out, err := h.Svc.GetLead(r.Context(), id)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) updateLead(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var in LeadInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateLead(r.Context(), id, in, httpx.IfMatchVersion(r))
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) leadAct(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		var in LeadActionInput
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		out, err := h.Svc.LeadAct(r.Context(), id, action, in)
		respond(w, r, http.StatusOK, out, err)
	}
}

func (h *Handler) listActivities(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	out, err := h.Svc.ListActivities(r.Context(), id)
	respond(w, r, http.StatusOK, httpx.NewList(out, nil), err)
}

func (h *Handler) addActivity(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var in ActivityInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.AddActivity(r.Context(), id, in)
	respond(w, r, http.StatusCreated, out, err)
}

// ---------- sale reservations ----------

func (h *Handler) listSaleRes(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f SaleResFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.ListingID, _ = httpx.QueryUUID(r, "listing_id")
	f.LeadID, _ = httpx.QueryUUID(r, "lead_id")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.ListSaleReservations(r.Context(), f, page)
	respond(w, r, http.StatusOK, httpx.NewList(items, next), err)
}

func (h *Handler) createSaleRes(w http.ResponseWriter, r *http.Request) {
	var in SaleReservationInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateSaleReservation(r.Context(), in)
	respond(w, r, http.StatusCreated, out, err)
}

func (h *Handler) getSaleRes(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	out, err := h.Svc.GetSaleReservation(r.Context(), id)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) updateSaleRes(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var in SaleReservationInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateSaleReservation(r.Context(), id, in, httpx.IfMatchVersion(r))
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) saleAct(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		var in SaleActionInput
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		out, err := h.Svc.SaleAct(r.Context(), id, action, in)
		respond(w, r, http.StatusOK, out, err)
	}
}

func (h *Handler) salesSummary(w http.ResponseWriter, r *http.Request) {
	pid, err := propertyParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.SalesSummaryView(r.Context(), pid)
	respond(w, r, http.StatusOK, out, err)
}

// ---------- rental listings ----------

func (h *Handler) listRL(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f ListingFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.ListRentalListings(r.Context(), f, page)
	respond(w, r, http.StatusOK, httpx.NewList(items, next), err)
}

func (h *Handler) createRL(w http.ResponseWriter, r *http.Request) {
	var in RentalListingInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateRentalListing(r.Context(), in)
	respond(w, r, http.StatusCreated, out, err)
}

func (h *Handler) getRL(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	out, err := h.Svc.GetRentalListing(r.Context(), id)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) updateRL(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var in RentalListingInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateRentalListing(r.Context(), id, in, httpx.IfMatchVersion(r))
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) rlAct(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		out, err := h.Svc.RentalListingAct(r.Context(), id, action)
		respond(w, r, http.StatusOK, out, err)
	}
}

// ---------- availability / reservations ----------

func (h *Handler) availability(w http.ResponseWriter, r *http.Request) {
	lid, err := httpx.QueryUUID(r, "listing_id")
	if err != nil || lid == nil {
		httpx.WriteError(w, r, apperr.Validation("listing_id wajib"))
		return
	}
	start, err := dateQ(r, "start_date")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	count, _ := strconv.Atoi(r.URL.Query().Get("period_count"))
	if count == 0 {
		count = 1
	}
	out, err := h.Svc.Availability(r.Context(), *lid, r.URL.Query().Get("rental_period"), count, start)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) listRR(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f RentalResFilter
	f.PropertyID, _ = httpx.QueryUUID(r, "property_id")
	f.ListingID, _ = httpx.QueryUUID(r, "listing_id")
	f.UnitID, _ = httpx.QueryUUID(r, "unit_location_id")
	f.Statuses = httpx.QueryCSV(r, "status")
	f.From, f.To = optDateQ(r, "from"), optDateQ(r, "to")
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.ListRentalReservations(r.Context(), f, page)
	respond(w, r, http.StatusOK, httpx.NewList(items, next), err)
}

func (h *Handler) createRR(w http.ResponseWriter, r *http.Request) {
	var in RentalReservationInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.CreateRentalReservation(r.Context(), in)
	respond(w, r, http.StatusCreated, out, err)
}

func (h *Handler) getRR(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	out, err := h.Svc.GetRentalReservation(r.Context(), id)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) updateRR(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var in RentalReservationInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.UpdateRentalReservation(r.Context(), id, in, httpx.IfMatchVersion(r))
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) rentalAct(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		var in RentalActionInput
		if r.ContentLength > 0 {
			if err := httpx.Decode(r, &in); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		out, err := h.Svc.RentalAct(r.Context(), id, action, in)
		respond(w, r, http.StatusOK, out, err)
	}
}

func (h *Handler) rentalCalendar(w http.ResponseWriter, r *http.Request) {
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
	if to.Before(from) || to.Sub(from) > 120*24*time.Hour {
		httpx.WriteError(w, r, apperr.Validation("rentang kalender maksimal 120 hari"))
		return
	}
	out, err := h.Svc.RentalCalendarView(r.Context(), pid, from, to)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) rentalSummary(w http.ResponseWriter, r *http.Request) {
	pid, err := propertyParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.Svc.RentalSummaryView(r.Context(), pid)
	respond(w, r, http.StatusOK, out, err)
}
