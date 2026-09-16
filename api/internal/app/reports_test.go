package app_test

import (
	"net/http"
	"testing"
	"time"
)

// P1.10 — Advanced Reports (PRD §26): seluruh laporan katalog dapat dijalankan per property & seluruh scope,
// authz server-side (teknisi tanpa reports.reports.view → 403), rentang tanggal divalidasi.
func TestReports(t *testing.T) {
	e := setup(t)
	pm := e.login("pm@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	tenA, _ := e.setupTenants(t)
	tr := e.login("tr.manager@demo.buildingvision.id")

	// data: satu ticket tenant diselesaikan + feedback (mengisi SLA/CSAT)
	var sr struct {
		ID string `json:"id"`
	}
	st, body := e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "plumbing", "title": "Keran bocor", "description": "Keran dapur bocor terus menerus sejak semalam"})
	e.mustJSON(st, body, 201, &sr)
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr.ID+"/acknowledge", nil)
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr.ID+"/resolve", map[string]any{"resolution": "Seal keran diganti"})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+sr.ID+"/confirm", nil)
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+sr.ID+"/feedback", map[string]any{"rating": 5, "comment": "Cepat"})
	e.mustJSON(st, body, 201, nil)

	var catalog struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	st, body = e.do(pm, http.MethodGet, "/api/v1/reports", nil)
	e.mustJSON(st, body, 200, &catalog)
	if len(catalog.Data) != 9 {
		t.Fatalf("katalog laporan: %+v", catalog.Data)
	}
	from := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	to := time.Now().Format("2006-01-02")
	type report struct {
		Name       string                      `json:"name"`
		Summary    map[string]float64          `json:"summary"`
		Series     []map[string]any            `json:"series"`
		Breakdowns map[string][]map[string]any `json:"breakdowns"`
	}
	for _, c := range catalog.Data {
		var r report
		st, body = e.do(pm, http.MethodGet, "/api/v1/reports/"+c.Name+"?property_id="+e.refs.PropertyID.String()+"&from="+from+"&to="+to, nil)
		if st != 200 {
			t.Fatalf("laporan %s: %d %s", c.Name, st, body)
		}
		e.mustJSON(st, body, 200, &r)
		if r.Name != c.Name || r.Summary == nil || r.Breakdowns == nil {
			t.Fatalf("laporan %s: %+v", c.Name, r)
		}
		if c.Name != "vendors" && len(r.Series) != 8 {
			t.Fatalf("laporan %s: seri harian harus 8 hari, dapat %d", c.Name, len(r.Series))
		}
		// tanpa property_id: seluruh scope
		st, body = e.do(pm, http.MethodGet, "/api/v1/reports/"+c.Name+"?from="+from+"&to="+to, nil)
		e.mustJSON(st, body, 200, &r)
	}
	var srr report
	st, body = e.do(pm, http.MethodGet, "/api/v1/reports/service-requests?property_id="+e.refs.PropertyID.String()+"&from="+from+"&to="+to, nil)
	e.mustJSON(st, body, 200, &srr)
	if srr.Summary["created"] < 1 || srr.Summary["resolved"] < 1 || srr.Summary["csat_count"] != 1 || srr.Summary["csat_avg"] != 5 || srr.Summary["sla_tracked"] < 1 {
		t.Fatalf("summary SR: %+v", srr.Summary)
	}
	if len(srr.Breakdowns["category"]) == 0 || len(srr.Breakdowns["csat"]) != 1 {
		t.Fatalf("breakdown SR: %+v", srr.Breakdowns)
	}
	// authz: teknisi tidak punya reports.reports.view
	if st, _ := e.do(tech, http.MethodGet, "/api/v1/reports/work-orders?property_id="+e.refs.PropertyID.String(), nil); st != 403 {
		t.Fatalf("teknisi harus 403: %d", st)
	}
	if st, _ := e.do(pm, http.MethodGet, "/api/v1/reports/work-orders?from=2026-01-01&to=2025-01-01", nil); st != 400 {
		t.Fatalf("rentang terbalik harus 400: %d", st)
	}
	if st, _ := e.do(pm, http.MethodGet, "/api/v1/reports/nope", nil); st != 404 {
		t.Fatalf("laporan tidak dikenal harus 404: %d", st)
	}
}
