package reports

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

// Mount: GET /reports (katalog), GET /reports/{name}?property_id=&from=YYYY-MM-DD&to=YYYY-MM-DD (permission reports.reports.view).
func (h *Handler) Mount(r chi.Router) {
	req := h.IAM.Require
	r.With(req("reports.reports.view")).Get("/reports", h.catalog)
	r.With(req("reports.reports.view")).Get("/reports/{name}", h.run)
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(Catalog, nil))
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	var p Params
	var err error
	if p.PropertyID, err = httpx.QueryUUID(r, "property_id"); err != nil {
		httpx.WriteError(w, r, apperr.Validation("property_id tidak valid"))
		return
	}
	p.FromDate = r.URL.Query().Get("from")
	p.ToDate = r.URL.Query().Get("to")
	out, err := h.Svc.Run(r.Context(), chi.URLParam(r, "name"), p)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
