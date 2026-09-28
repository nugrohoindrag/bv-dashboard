package reports

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
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
	if r.URL.Query().Get("format") == "csv" {
		// PRD P1 v2 §41: laporan dapat diekspor (mengikuti permission reports.reports.export)
		if !authctx.Must(r.Context()).Has("reports.reports.export") {
			httpx.WriteError(w, r, apperr.Forbidden("Memerlukan reports.reports.export"))
			return
		}
		writeCSV(w, out)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// writeCSV: section,key,label,metric,value — summary, series harian, dan setiap breakdown.
func writeCSV(w http.ResponseWriter, rep *Report) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report-%s-%s-%s.csv"`, rep.Name, rep.From.Format("20060102"), rep.To.Format("20060102")))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"section", "key", "label", "metric", "value"})
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	keys := func(m map[string]float64) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	for _, k := range keys(rep.Summary) {
		_ = cw.Write([]string{"summary", "", "", k, num(rep.Summary[k])})
	}
	for _, pt := range rep.Series {
		for _, k := range keys(pt.Values) {
			_ = cw.Write([]string{"series", pt.Key, pt.Label, k, num(pt.Values[k])})
		}
	}
	bks := make([]string, 0, len(rep.Breakdowns))
	for k := range rep.Breakdowns {
		bks = append(bks, k)
	}
	sort.Strings(bks)
	for _, b := range bks {
		for _, pt := range rep.Breakdowns[b] {
			for _, k := range keys(pt.Values) {
				_ = cw.Write([]string{"breakdown:" + b, pt.Key, pt.Label, k, num(pt.Values[k])})
			}
		}
	}
	cw.Flush()
}
