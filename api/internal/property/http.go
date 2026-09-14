package property

import (
	"net/http"

	"github.com/go-chi/chi/v5"

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
	r.With(req("platform.organizations.view")).Get("/organizations/me", h.getOrg)
	r.With(req("platform.organizations.update")).Patch("/organizations/me", h.updateOrg)

	// Generic location endpoints
	r.With(req("property.locations.view")).Get("/locations", h.listLocations)
	r.With(req("property.locations.view")).Get("/locations/tree", h.tree)
	r.With(req("property.locations.view")).Get("/locations/{id}", h.getLocation)
	r.With(req("property.locations.view")).Get("/locations/{id}/path", h.getPath)
	r.With(h.IAM.RequireAny("property.locations.create", "property.properties.create")).Post("/locations", h.createLocation)
	r.With(req("property.locations.update")).Patch("/locations/{id}", h.updateLocation)
	r.With(req("property.locations.delete")).Delete("/locations/{id}", h.deleteLocation)

	// Typed aliases (Naming Convention §47): /properties /buildings /towers /floors /areas /spaces /units
	for _, lt := range []LocationType{LTProperty, LTBuilding, LTTower, LTFloor, LTArea, LTSpace, LTUnit} {
		path := "/" + pluralOf(lt)
		lt := lt
		r.With(req("property.locations.view")).Get(path, h.listTyped(lt))
		r.With(h.IAM.RequireAny("property.locations.create", "property.properties.create")).Post(path, h.createTyped(lt))
		r.With(req("property.locations.view")).Get(path+"/{id}", h.getLocation)
		r.With(req("property.locations.update")).Patch(path+"/{id}", h.updateLocation)
	}
	r.With(req("property.locations.view")).Get("/buildings/{id}/floors", h.childrenOf(LTFloor))
	r.With(req("property.locations.view")).Get("/floors/{id}/areas", h.childrenOf(LTArea))
	r.With(req("property.locations.view")).Get("/floors/{id}/units", h.childrenOf(LTUnit))

	r.With(req("property.tenants.view")).Get("/tenants", h.listTenants)
	r.With(req("property.tenants.create")).Post("/tenants", h.createTenant)
	r.With(req("property.tenants.view")).Get("/tenants/{id}", h.getTenant)
	r.With(req("property.tenants.update")).Patch("/tenants/{id}", h.updateTenant)

	r.With(req("property.occupants.view")).Get("/occupants", h.listOccupants)
	r.With(req("property.occupants.create")).Post("/occupants", h.createOccupant)
	r.With(req("property.occupants.update")).Patch("/occupants/{id}", h.updateOccupant)
}

func pluralOf(lt LocationType) string {
	switch lt {
	case LTProperty:
		return "properties"
	default:
		return string(lt) + "s"
	}
}

func (h *Handler) getOrg(w http.ResponseWriter, r *http.Request) {
	o, err := h.Svc.GetOrganization(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, o)
}

func (h *Handler) updateOrg(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     *string        `json:"name"`
		Settings map[string]any `json:"settings"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	o, err := h.Svc.UpdateOrganization(r.Context(), in.Name, in.Settings)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, o)
}

func (h *Handler) parseFilter(r *http.Request) (LocationFilter, error) {
	var f LocationFilter
	var err error
	if f.PropertyID, err = httpx.QueryUUID(r, "property_id"); err != nil {
		return f, err
	}
	if f.ParentID, err = httpx.QueryUUID(r, "parent_id"); err != nil {
		return f, err
	}
	f.LocationType = LocationType(r.URL.Query().Get("location_type"))
	f.Q = r.URL.Query().Get("q")
	f.IncludeInactive = r.URL.Query().Get("include_inactive") == "true"
	return f, nil
}

func (h *Handler) listLocations(w http.ResponseWriter, r *http.Request) {
	f, err := h.parseFilter(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.ListLocations(r.Context(), f)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) listTyped(lt LocationType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := h.parseFilter(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		f.LocationType = lt
		if lt == LTProperty {
			f.IncludeInactive = true
		}
		items, err := h.Svc.ListLocations(r.Context(), f)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
	}
}

func (h *Handler) childrenOf(lt LocationType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.PathUUID(r, chi.URLParam, "id")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		items, err := h.Svc.ListLocations(r.Context(), LocationFilter{ParentID: &id, LocationType: lt})
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
	}
}

func (h *Handler) tree(w http.ResponseWriter, r *http.Request) {
	pid, err := httpx.QueryUUID(r, "property_id")
	if err != nil || pid == nil {
		httpx.WriteError(w, r, errOr(err, "property_id wajib"))
		return
	}
	t, err := h.Svc.Tree(r.Context(), *pid)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, t)
}

func (h *Handler) getLocation(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	l, err := h.Svc.GetLocation(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, l)
}

func (h *Handler) getPath(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	l, err := h.Svc.GetLocation(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"path": l.Path, "path_text": l.PathText})
}

func (h *Handler) createLocation(w http.ResponseWriter, r *http.Request) {
	var in CreateLocationInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	l, err := h.Svc.CreateLocation(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, l)
}

func (h *Handler) createTyped(lt LocationType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in CreateLocationInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		in.LocationType = lt
		l, err := h.Svc.CreateLocation(r.Context(), in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, l)
	}
}

func (h *Handler) updateLocation(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in UpdateLocationInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	l, err := h.Svc.UpdateLocation(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, l)
}

func (h *Handler) deleteLocation(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.Svc.DeleteLocation(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----- tenants -----

func (h *Handler) listTenants(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f TenantFilter
	if f.PropertyID, err = httpx.QueryUUID(r, "property_id"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f.Status = r.URL.Query().Get("status")
	f.Q = r.URL.Query().Get("q")
	items, next, err := h.Svc.ListTenants(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, next))
}

func (h *Handler) createTenant(w http.ResponseWriter, r *http.Request) {
	var in TenantInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.Svc.CreateTenant(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, t)
}

func (h *Handler) getTenant(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.Svc.GetTenant(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, t)
}

func (h *Handler) updateTenant(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in TenantInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, err := h.Svc.UpdateTenant(r.Context(), id, in, httpx.IfMatchVersion(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, t)
}

// ----- occupants -----

func (h *Handler) listOccupants(w http.ResponseWriter, r *http.Request) {
	tid, err := httpx.QueryUUID(r, "tenant_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	uid, err := httpx.QueryUUID(r, "unit_id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := h.Svc.ListOccupants(r.Context(), tid, uid, r.URL.Query().Get("q"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, nil))
}

func (h *Handler) createOccupant(w http.ResponseWriter, r *http.Request) {
	var in OccupantInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	o, err := h.Svc.CreateOccupant(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, o)
}

func (h *Handler) updateOccupant(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathUUID(r, chi.URLParam, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in OccupantInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	o, err := h.Svc.UpdateOccupant(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, o)
}

func errOr(err error, msg string) error {
	if err != nil {
		return err
	}
	return httpxValidation(msg)
}

func httpxValidation(msg string) error { return apperr.Validation(msg) }
