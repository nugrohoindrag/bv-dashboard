package app_test

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// jm: decode JSON object (angka → float64) dengan status yang diharapkan.
func (e *env) jm(st int, body []byte, want int) map[string]any {
	e.t.Helper()
	var m map[string]any
	e.mustJSON(st, body, want, &m)
	return m
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func list(v any) []map[string]any {
	arr, _ := v.([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, x := range arr {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func strs(v any) []string {
	arr, _ := v.([]any)
	out := []string{}
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// rawDo: permintaan tanpa decode JSON (PDF / CSV / tautan publik).
func (e *env) rawDo(token, method, path string) (int, http.Header, []byte) {
	req, _ := http.NewRequest(method, e.srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

// TestP4v21FinancialOperations: PRD P4 v2.1 — billing rule semua dasar tarif + generate periode (preview/pengecualian/prorata/
// idempoten), PPN terkonfigurasi, nomor saat issue, meter & tarif + pembacaan offline Staff App, pembayaran bukti transfer,
// penerimaan multi-invoice + saldo kredit, refund, credit note, void (SoD), denda + waive, pengingat bertahap, aging, statement,
// PDF + tautan bertanda tangan, rekonsiliasi impor mutasi, additional charge WO, impor CSV, sinking fund, deposit, budget vs
// actual, biaya manual, ekspor jurnal, webhook keluar, dashboard Finance, ekspor dataset, bug B-01..B-19.
func TestP4v21FinancialOperations(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	tenA, tenB := e.setupTenants(t)
	pm := e.login("pm@demo.buildingvision.id")       // property_manager: billing.* (termasuk void, approve, waive)
	fin := e.login("finance@demo.buildingvision.id") // finance_staff: operasional + laporan (D-P4-08)
	tech := e.login("budi@demo.buildingvision.id")   // technician: pencatat meter
	admin := e.login("admin@org-a.test")             // organization_admin: grant tingkat organization
	pid := e.refs.PropertyID.String()
	loc, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(loc)
	curPeriod := now.Format("2006-01")
	nextStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	nextPeriod := nextStart.Format("2006-01")

	tenantOf := func(tok string) uuid.UUID {
		var me struct {
			Tenant *struct {
				ID uuid.UUID `json:"id"`
			} `json:"tenant"`
		}
		st, body := e.do(tok, http.MethodGet, "/api/v1/tenant/me", nil)
		e.mustJSON(st, body, 200, &me)
		if me.Tenant == nil {
			t.Fatal("tenant record tidak ada")
		}
		return me.Tenant.ID
	}
	tenantA, tenantB := tenantOf(tenA), tenantOf(tenB)
	// unit 1201 → tenant A (penuh), 1202 → tenant B (masuk pertengahan periode berikutnya → prorata)
	occFrom := nextStart.AddDate(0, 0, 15).Format("2006-01-02")
	st, body := e.do(pm, http.MethodPatch, "/api/v1/locations/"+e.refs.UnitA1201.String(), map[string]any{"details": map[string]any{"tenant_id": tenantA.String(), "occupancy_status": "occupied"}})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(pm, http.MethodPatch, "/api/v1/locations/"+e.refs.UnitA1202.String(), map[string]any{"details": map[string]any{"tenant_id": tenantB.String(), "occupancy_status": "occupied", "occupied_from": occFrom}})
	e.mustJSON(st, body, 200, nil)
	// unit 1203 dihuni tetapi tanpa luas & tanpa tenant → pengecualian preview (rule default hanya unit occupied)
	st, body = e.do(pm, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "unit", "parent_id": e.refs.FloorA12, "name": "Office Unit 1203", "details": map[string]any{"unit_number": "1203", "unit_type": "commercial", "occupancy_status": "occupied"}})
	unit1203 := e.jm(st, body, 201)
	_ = unit1203

	// ---- B-15 / pengaturan billing (PPN, rekening, pengingat) ----
	if st, _ := e.do(fin, http.MethodPut, "/api/v1/billing/settings?property_id="+pid, map[string]any{"tax_enabled": true}); st != 403 {
		t.Fatalf("finance_staff ubah pengaturan harus 403: %d", st)
	}
	st, body = e.do(pm, http.MethodPut, "/api/v1/billing/settings?property_id="+pid, map[string]any{"tax_enabled": true, "tax_rate": 11, "default_due_days": 14,
		"reminder_offsets": []int{-3, 1, 7}, "seller_name": "PT Graha Pangeran Kelola", "payment_instructions": "Transfer sebelum jatuh tempo."})
	sett := e.jm(st, body, 200)
	if sett["tax_enabled"] != true || num(sett["tax_rate"]) != 11 || sett["inherited"] != false {
		t.Fatalf("settings: %v", sett)
	}
	st, body = e.do(pm, http.MethodPost, "/api/v1/billing/bank-accounts", map[string]any{"property_id": pid, "bank_name": "BCA", "account_number": "1234567890", "account_name": "PT Graha Pangeran Kelola", "is_default": true})
	e.mustJSON(st, body, 201, nil)
	if st, body := e.do(admin, http.MethodPut, "/api/v1/payment-providers/midtrans", map[string]any{"is_active": true}); st != 409 || !strings.Contains(string(body), "PROVIDER_NOT_AVAILABLE") {
		t.Fatalf("aktivasi midtrans tanpa adapter harus 409: %d %s", st, body)
	}
	if st, _ := e.do(fin, http.MethodPut, "/api/v1/payment-providers/manual", map[string]any{"is_active": true}); st != 403 {
		t.Fatalf("finance_staff ubah provider harus 403: %d", st)
	}

	// ---- billing rule: per m² (prorata) + sinking fund % dari service charge (P4-BRL-01, P4-SCF-02) ----
	if st, _ := e.do(fin, http.MethodPost, "/api/v1/billing/rules", map[string]any{"property_id": pid, "code": "X", "name": "X", "charge_type": "service_charge", "basis": "fixed_per_unit", "rate": 1}); st != 403 {
		t.Fatalf("finance_staff buat rule harus 403: %d", st)
	}
	st, body = e.do(pm, http.MethodPost, "/api/v1/billing/rules", map[string]any{"property_id": pid, "code": "SC", "name": "Service Charge", "charge_type": "service_charge", "basis": "per_area_m2", "rate": 12500, "prorate": true, "due_days": 14, "issue_day": 1})
	scRule := e.jm(st, body, 201)
	st, body = e.do(pm, http.MethodPost, "/api/v1/billing/rules", map[string]any{"property_id": pid, "code": "SF", "name": "Sinking Fund", "charge_type": "sinking_fund", "basis": "percentage", "rate": 10, "base_rule_id": scRule["id"], "tax_rate": 0})
	sfRule := e.jm(st, body, 201)
	if st, _ := e.do(pm, http.MethodPost, "/api/v1/billing/rules", map[string]any{"property_id": pid, "code": "BAD", "name": "Bad", "charge_type": "sinking_fund", "basis": "percentage", "rate": 10}); st != 400 {
		t.Fatalf("rule persentase tanpa base harus 400: %d", st)
	}

	// ---- run periode berikutnya: preview → pengecualian → generate → issue (P4-BRL-02..04, D-P4-01) ----
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/runs", map[string]any{"property_id": pid, "period": nextPeriod, "rule_ids": []any{scRule["id"], sfRule["id"]}})
	run := e.jm(st, body, 201)
	lines := list(run["lines"])
	if run["status"] != "preview" || !strings.HasPrefix(str(run["run_number"]), "BRN-") || len(lines) != 6 || num(run["exception_count"]) != 2 {
		t.Fatalf("preview run: %v", run)
	}
	var sc1201, sc1202 map[string]any
	for _, l := range lines {
		switch {
		case str(l["unit_label"]) == "Unit 1201" && str(l["charge_type"]) == "service_charge":
			sc1201 = l
		case str(l["unit_label"]) == "Unit 1202" && str(l["charge_type"]) == "service_charge":
			sc1202 = l
		case str(l["unit_label"]) == "Unit 1203":
			if l["exception"] == nil || l["included"] != false {
				t.Fatalf("unit 1203 harus pengecualian: %v", l)
			}
		}
	}
	if sc1201 == nil || num(sc1201["amount"]) != 2250000 || num(sc1201["tax_amount"]) != 247500 {
		t.Fatalf("service charge 1201 (180 m² × 12.500, PPN 11%%): %v", sc1201)
	}
	meta, _ := sc1202["meta"].(map[string]any)
	if sc1202 == nil || num(sc1202["amount"]) >= 1500000 || num(sc1202["amount"]) <= 0 || meta["prorate_days"] == nil {
		t.Fatalf("prorata 1202: %v", sc1202)
	}
	for _, l := range lines {
		if str(l["unit_label"]) == "Unit 1201" && str(l["charge_type"]) == "sinking_fund" && (num(l["amount"]) != 225000 || num(l["tax_amount"]) != 0) {
			t.Fatalf("sinking fund 10%% tanpa pajak: %v", l)
		}
	}
	// kecualikan & sertakan kembali satu baris; baris pengecualian tidak dapat disertakan
	runID := str(run["id"])
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/runs/"+runID+"/lines/"+str(sc1202["id"])+"/exclude", nil)
	run = e.jm(st, body, 200)
	total1 := num(run["total_amount"])
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/runs/"+runID+"/lines/"+str(sc1202["id"])+"/include", nil)
	run = e.jm(st, body, 200)
	if num(run["total_amount"]) <= total1 {
		t.Fatalf("toggle line total: %v → %v", total1, run["total_amount"])
	}
	for _, l := range list(run["lines"]) {
		if l["exception"] != nil {
			if st, body := e.do(fin, http.MethodPost, "/api/v1/billing/runs/"+runID+"/lines/"+str(l["id"])+"/include", nil); st != 409 {
				t.Fatalf("baris pengecualian disertakan harus 409: %d %s", st, body)
			}
			break
		}
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/runs/"+runID+"/generate", nil)
	run = e.jm(st, body, 200)
	if run["status"] != "generated" || num(run["invoice_count"]) != 2 {
		t.Fatalf("generate: %v", run)
	}
	var drafts struct {
		Data []struct {
			ID            uuid.UUID `json:"id"`
			InvoiceNumber *string   `json:"invoice_number"`
			Status        string    `json:"status"`
			TotalAmount   int64     `json:"total_amount"`
			TenantID      *string   `json:"tenant_id"`
			UnitLabel     *string   `json:"unit_label"`
		} `json:"data"`
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/invoices?billing_run_id="+runID, nil)
	e.mustJSON(st, body, 200, &drafts)
	var inv1201 uuid.UUID
	var total1201 int64
	for _, d := range drafts.Data {
		if d.InvoiceNumber != nil || d.Status != "draft" {
			t.Fatalf("draft run harus tanpa nomor: %+v", d)
		}
		if d.UnitLabel != nil && *d.UnitLabel == "Unit 1201" {
			inv1201, total1201 = d.ID, d.TotalAmount
		}
	}
	if total1201 != 2250000+247500+225000 {
		t.Fatalf("total invoice 1201: %d", total1201)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/runs/"+runID+"/issue", nil)
	run = e.jm(st, body, 200)
	if run["status"] != "issued" {
		t.Fatalf("issue run: %v", run)
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/invoices/"+inv1201.String(), nil)
	invA := e.jm(st, body, 200)
	if !strings.HasPrefix(str(invA["invoice_number"]), "INV-") || invA["status"] != "issued" || invA["tax_mode"] != "computed" {
		t.Fatalf("invoice terbit: %v", invA)
	}
	// idempoten per rule × unit × periode: run kedua → semua baris pengecualian "sudah ditagihkan" → generate 409
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/runs", map[string]any{"property_id": pid, "period": nextPeriod, "rule_ids": []any{scRule["id"], sfRule["id"]}})
	run2 := e.jm(st, body, 201)
	billed := 0
	for _, l := range list(run2["lines"]) {
		if str(l["exception"]) == "already_billed" {
			billed++
		}
	}
	if billed != 4 {
		t.Fatalf("run kedua harus 4 baris already_billed: %v", run2["lines"])
	}
	if st, body := e.do(fin, http.MethodPost, "/api/v1/billing/runs/"+str(run2["id"])+"/generate", nil); st != 409 {
		t.Fatalf("generate run kosong harus 409: %d %s", st, body)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/runs/"+str(run2["id"])+"/cancel", map[string]any{"reason": "duplikat"})
	if r := e.jm(st, body, 200); r["status"] != "cancelled" {
		t.Fatalf("cancel run: %v", r)
	}

	// ---- meter & tarif + pembacaan offline Staff App (P4-UTL-01..05) ----
	st, body = e.do(pm, http.MethodPost, "/api/v1/billing/utility-tariffs", map[string]any{"property_id": pid, "code": "PLN-B2", "name": "Listrik B2", "meter_type": "electricity", "rate": 1500, "fixed_charge": 50000})
	tariff := e.jm(st, body, 201)
	st, body = e.do(pm, http.MethodPost, "/api/v1/billing/meters", map[string]any{"location_id": e.refs.UnitA1201, "meter_type": "electricity", "meter_number": "E-1201", "initial_reading": 1000, "tariff_id": tariff["id"]})
	meter := e.jm(st, body, 201)
	meterID := uuid.MustParse(str(meter["id"]))
	st, body = e.do(tech, http.MethodGet, "/api/v1/sync/work-bundle?device_id=dev-meter", nil)
	bundle := e.jm(st, body, 200)
	if len(list(bundle["meters"])) != 1 {
		t.Fatalf("bundle meters: %v", bundle["meters"])
	}
	var syncRes struct {
		Results []struct {
			Status   string         `json:"status"`
			Response map[string]any `json:"response"`
			Detail   string         `json:"detail"`
		} `json:"results"`
	}
	m1 := mut(uuid.New(), "meter", meterID, "record_meter_reading", 1, map[string]any{"reading_value": 1250}, time.Now())
	st, body = e.do(tech, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-meter", "mutations": []map[string]any{m1}})
	e.mustJSON(st, body, 200, &syncRes)
	if len(syncRes.Results) != 1 || syncRes.Results[0].Status != "applied" || num(syncRes.Results[0].Response["usage"]) != 250 {
		t.Fatalf("record_meter_reading sync: %s", body)
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-meter", "mutations": []map[string]any{m1}})
	e.mustJSON(st, body, 200, &syncRes)
	if syncRes.Results[0].Status != "duplicate" {
		t.Fatalf("pembacaan ulang harus idempoten: %s", body)
	}
	// angka mundur → flagged (rollback) → Finance menolak
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/meters/"+meterID.String()+"/readings", map[string]any{"reading_value": 1200})
	rb := e.jm(st, body, 201)
	if rb["status"] != "flagged" || rb["anomaly"] != "rollback" {
		t.Fatalf("pembacaan mundur harus flagged: %v", rb)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/meter-readings/"+str(rb["id"])+"/reject", map[string]any{"note": "salah baca"})
	if r := e.jm(st, body, 200); r["status"] != "rejected" {
		t.Fatalf("reject reading: %v", r)
	}
	st, body = e.do(pm, http.MethodPost, "/api/v1/billing/rules", map[string]any{"property_id": pid, "code": "EL", "name": "Listrik", "charge_type": "electricity", "basis": "meter_usage", "meter_type": "electricity", "tariff_id": tariff["id"]})
	elRule := e.jm(st, body, 201)
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/runs", map[string]any{"property_id": pid, "period": curPeriod, "rule_ids": []any{elRule["id"]}, "combine": false})
	elRun := e.jm(st, body, 201)
	var el1201 map[string]any
	for _, l := range list(elRun["lines"]) {
		if str(l["unit_label"]) == "Unit 1201" {
			el1201 = l
		} else if str(l["unit_label"]) == "Unit 1202" && str(l["exception"]) != "no_meter" {
			t.Fatalf("unit tanpa meter harus no_meter: %v", l)
		}
	}
	if el1201 == nil || num(el1201["amount"]) != 250*1500+50000 || el1201["meter_reading_id"] == nil {
		t.Fatalf("tagihan listrik dari pemakaian: %v", el1201)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/runs/"+str(elRun["id"])+"/generate", nil)
	e.jm(st, body, 200)
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/runs/"+str(elRun["id"])+"/issue", nil)
	e.jm(st, body, 200)
	st, body = e.do(fin, http.MethodGet, "/api/v1/invoices?billing_run_id="+str(elRun["id"]), nil)
	elList := e.jm(st, body, 200)
	elInv := list(elList["data"])
	if len(elInv) != 1 || str(elInv[0]["invoice_type"]) != "electricity" {
		t.Fatalf("invoice listrik: %v", elList)
	}
	elInvID := str(elInv[0]["id"])
	// pembacaan yang sudah ditagihkan tidak dapat ditolak
	st, body = e.do(fin, http.MethodGet, "/api/v1/billing/meter-readings/"+str(el1201["meter_reading_id"]), nil)
	if r := e.jm(st, body, 200); r["billed"] != true || has(strs(r["allowed_actions"]), "reject") {
		t.Fatalf("pembacaan tertagih: %v", r)
	}

	p4Part2(t, e, ctx, p4ctx{pm: pm, fin: fin, tech: tech, admin: admin, tenA: tenA, tenB: tenB, tenantA: tenantA, tenantB: tenantB, pid: pid,
		inv1201: inv1201.String(), total1201: total1201, elInv: elInvID, loc: loc})
}

type p4ctx struct {
	pm, fin, tech, admin, tenA, tenB string
	tenantA, tenantB                 uuid.UUID
	pid                              string
	inv1201                          string
	total1201                        int64
	elInv                            string
	loc                              *time.Location
}
