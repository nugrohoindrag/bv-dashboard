package app_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestP4v21BillingFilters: umpan balik UI billing — filter tanggal YYYY-MM-DD inklusif di timezone property, days_overdue = hari
// kalender (sama dengan aging), PATCH draft dapat mengosongkan field, tagihan WO dapat dibuat tanpa tenant (UUID nol).
func TestP4v21BillingFilters(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.setupTenants(t)
	fin := e.login("finance@demo.buildingvision.id")
	pm := e.login("pm@demo.buildingvision.id")
	pid := e.refs.PropertyID
	loc, _ := time.LoadLocation("Asia/Jakarta")
	today := time.Now().In(loc)
	day := func(d int) string { return today.AddDate(0, 0, d).Format("2006-01-02") }

	mk := func(due string, extra map[string]any) map[string]any {
		t.Helper()
		in := map[string]any{"property_id": pid, "unit_location_id": e.refs.UnitA1201, "invoice_type": "other", "due_date": due, "issue_now": true, "apply_tax": false,
			"items": []map[string]any{{"description": "Biaya " + due, "unit_price": 100000}}}
		for k, v := range extra {
			in[k] = v
		}
		st, body := e.do(fin, http.MethodPost, "/api/v1/invoices", in)
		return e.jm(st, body, 201)
	}
	invToday := mk(day(0), nil)
	invOld := mk(day(-3), nil)

	// ---- filter jatuh tempo/terbit tanggal lokal inklusif ----
	has := func(path string, id any) bool {
		t.Helper()
		st, body := e.do(fin, http.MethodGet, path, nil)
		if st != 200 {
			t.Fatalf("GET %s: %d %s", path, st, body)
		}
		return containsID(listIDs(t, body), uuid.MustParse(str(id)))
	}
	if !has("/api/v1/invoices?due_from="+day(0)+"&due_to="+day(0), invToday["id"]) || has("/api/v1/invoices?due_from="+day(0)+"&due_to="+day(0), invOld["id"]) {
		t.Fatal("filter due_from/due_to = hari ini harus memuat invoice jatuh tempo hari ini saja")
	}
	if has("/api/v1/invoices?due_to="+day(-1), invToday["id"]) || !has("/api/v1/invoices?due_to="+day(-1), invOld["id"]) {
		t.Fatal("filter due_to kemarin")
	}
	if !has("/api/v1/invoices?issued_from="+day(0)+"&issued_to="+day(0), invToday["id"]) {
		t.Fatal("filter issued_to = hari ini harus memuat invoice yang terbit hari ini (inklusif)")
	}

	// ---- days_overdue = hari kalender property (sama dengan bucket aging) ----
	if err := e.app.Billing.Sweep(ctx, e.refs.OrgID); err != nil {
		t.Fatal(err)
	}
	st, body := e.do(fin, http.MethodGet, "/api/v1/invoices/"+str(invOld["id"]), nil)
	if v := e.jm(st, body, 200); v["status"] != "overdue" || num(v["days_overdue"]) != 3 {
		t.Fatalf("days_overdue harus 3 hari kalender: %v %v", v["status"], v["days_overdue"])
	}
	if !has("/api/v1/invoices?aging=1_30", invOld["id"]) {
		t.Fatal("invoice 3 hari lewat jatuh tempo harus di bucket aging 1–30")
	}

	// ---- filter tanggal bayar lokal inklusif ----
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices/"+str(invToday["id"])+"/payments", map[string]any{"amount": 100000, "method": "cash"})
	pay := e.jm(st, body, 201)
	if !has("/api/v1/payments?paid_from="+day(0)+"&paid_to="+day(0), pay["id"]) || has("/api/v1/payments?paid_to="+day(-1), pay["id"]) {
		t.Fatal("filter paid_from/paid_to tanggal lokal inklusif")
	}

	// ---- PATCH draft: string kosong mengosongkan periode, deskripsi, catatan, external_ref ----
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices", map[string]any{"property_id": pid, "unit_location_id": e.refs.UnitA1201, "invoice_type": "other", "due_date": day(10),
		"period_start": day(0), "period_end": day(29), "description": "Draft uji", "notes": "catatan", "external_ref": "EXT-1",
		"items": []map[string]any{{"description": "Biaya", "unit_price": 1000}}})
	draft := e.jm(st, body, 201)
	st, body = e.do(fin, http.MethodPatch, "/api/v1/invoices/"+str(draft["id"]), map[string]any{"period_start": "", "period_end": "", "description": "", "notes": "", "external_ref": ""})
	if v := e.jm(st, body, 200); v["period_start"] != nil || v["period_end"] != nil || v["description"] != nil || v["notes"] != nil || v["external_ref"] != nil {
		t.Fatalf("PATCH pengosongan: %v", v)
	}
	st, body = e.do(fin, http.MethodPatch, "/api/v1/invoices/"+str(draft["id"]), map[string]any{"notes": "baru"})
	if v := e.jm(st, body, 200); v["notes"] != "baru" || v["external_ref"] != nil {
		t.Fatalf("PATCH sebagian tidak mengubah field lain: %v", v)
	}

	// ---- tagihan WO: UUID nol = tagih unit tanpa tenant yang disarankan ----
	st, body = e.do(pm, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "Ganti kran unit 1201", "priority": "low", "location_id": e.refs.UnitA1201})
	woID := uuid.MustParse(str(e.jm(st, body, 201)["id"]))
	if err := e.app.DB.WithOrgTx(ctx, e.refs.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE work_orders SET service_cost_amount = 75000, actual_cost_amount = 75000 WHERE id = $1`, woID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/billing/charges/draft?source_type=work_order&source_id="+woID.String(), nil)
	if d := e.jm(st, body, 200); d["tenant_id"] == nil {
		t.Fatalf("saran tagihan WO unit 1201 harus membawa tenant: %v", d)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/charges", map[string]any{"source_type": "work_order", "source_id": woID, "tenant_id": uuid.Nil})
	if v := e.jm(st, body, 201); v["tenant_id"] != nil || str(v["unit_location_id"]) != e.refs.UnitA1201.String() {
		t.Fatalf("tagihan WO tanpa tenant: tenant=%v unit=%v", v["tenant_id"], v["unit_location_id"])
	}
}
