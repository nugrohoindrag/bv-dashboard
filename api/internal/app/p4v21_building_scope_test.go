package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestP4v21BillingBuildingScope: PRD P4 v2.1 P4-ACL-04 — grant ber-scope Building/Tower melihat invoice, pembayaran, dan
// dokumennya hanya untuk unit di subtree-nya (daftar & detail konsisten); aksi keuangan tetap memerlukan grant property.
func TestP4v21BillingBuildingScope(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	fin := e.login("finance@demo.buildingvision.id")
	pid := e.refs.PropertyID

	type idOnly struct {
		ID uuid.UUID `json:"id"`
	}
	var unitB idOnly
	st, body := e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "unit", "parent_id": e.refs.FloorB3, "name": "Unit B-301",
		"details": map[string]any{"unit_number": "B-301", "unit_type": "residential"}})
	e.mustJSON(st, body, 201, &unitB)

	due := time.Now().Add(10 * 24 * time.Hour)
	mkInvoice := func(unit uuid.UUID, label string) (inv, pay idOnly) {
		t.Helper()
		st, body := e.do(fin, http.MethodPost, "/api/v1/invoices", map[string]any{"property_id": pid, "unit_location_id": unit, "invoice_type": "service_charge",
			"due_at": due, "issue_now": true, "items": []map[string]any{{"description": "Service charge " + label, "unit_price": 100000}}})
		e.mustJSON(st, body, 201, &inv)
		st, body = e.do(fin, http.MethodPost, "/api/v1/invoices/"+inv.ID.String()+"/payments", map[string]any{"amount": 100000, "method": "cash"})
		e.mustJSON(st, body, 201, &pay)
		return inv, pay
	}
	invA, payA := mkInvoice(e.refs.UnitA1201, "Tower A")
	invB, payB := mkInvoice(unitB.ID, "Tower B")

	e.createUser(admin, "bm.towera.billing@org-a.test", []map[string]any{{"role_id": e.roleID(admin, "building_manager"), "property_id": pid, "scope_location_id": e.refs.TowerA}}, nil)
	bmA := e.login("bm.towera.billing@org-a.test")

	for _, path := range []string{"/api/v1/invoices", "/api/v1/invoices?property_id=" + pid.String()} {
		st, body := e.do(bmA, http.MethodGet, path, nil)
		ids := listIDs(t, body)
		if st != 200 || !containsID(ids, invA.ID) || containsID(ids, invB.ID) {
			t.Fatalf("P4-ACL-04 daftar invoice %s: %d A=%v B=%v", path, st, containsID(ids, invA.ID), containsID(ids, invB.ID))
		}
	}
	for _, path := range []string{"/api/v1/payments", "/api/v1/payments?property_id=" + pid.String()} {
		st, body := e.do(bmA, http.MethodGet, path, nil)
		ids := listIDs(t, body)
		if st != 200 || !containsID(ids, payA.ID) || containsID(ids, payB.ID) {
			t.Fatalf("P4-ACL-04 daftar pembayaran %s: %d A=%v B=%v", path, st, containsID(ids, payA.ID), containsID(ids, payB.ID))
		}
	}
	// detail & dokumen: Tower A boleh, Tower B 403 (sebelumnya detail invoice Tower A pun 403 walau tampil di daftar)
	for _, c := range []struct {
		path string
		want int
	}{
		{"/api/v1/invoices/" + invA.ID.String(), 200},
		{"/api/v1/invoices/" + invB.ID.String(), 403},
		{"/api/v1/payments/" + payA.ID.String(), 200},
		{"/api/v1/payments/" + payB.ID.String(), 403},
		{"/api/v1/invoices/" + invA.ID.String() + "/pdf", 200},
		{"/api/v1/invoices/" + invB.ID.String() + "/pdf", 403},
		{"/api/v1/payments/" + payA.ID.String() + "/receipt", 200},
		{"/api/v1/payments/" + payB.ID.String() + "/receipt", 403},
		{"/api/v1/payments/" + payA.ID.String() + "/proofs", 200},
		{"/api/v1/payments/" + payB.ID.String() + "/proofs", 403},
	} {
		if st, body := e.do(bmA, http.MethodGet, c.path, nil); st != c.want {
			t.Fatalf("P4-ACL-04 GET %s: %d (want %d) %s", c.path, st, c.want, body)
		}
	}
	var detail struct {
		AllowedActions []string `json:"allowed_actions"`
	}
	st, body = e.do(bmA, http.MethodGet, "/api/v1/invoices/"+invA.ID.String(), nil)
	e.mustJSON(st, body, 200, &detail)
	for _, a := range detail.AllowedActions {
		if a != "view" && a != "download_pdf" {
			t.Fatalf("P4-ACL-04 user ber-scope baca tidak boleh mendapat aksi keuangan: %v", detail.AllowedActions)
		}
	}
	if st, _ := e.do(bmA, http.MethodPost, "/api/v1/invoices/"+invA.ID.String()+"/void", map[string]any{"reason": "uji"}); st != 403 {
		t.Fatalf("P4-ACL-04 void oleh user ber-scope tanpa izin void harus 403, got %d", st)
	}
	// tautan dokumen bertanda tangan mengikuti aturan yang sama
	if st, _ := e.do(bmA, http.MethodPost, "/api/v1/invoices/"+invA.ID.String()+"/document-link", nil); st != 200 && st != 201 {
		t.Fatalf("P4-ACL-04 tautan dokumen invoice Tower A: %d", st)
	}
	if st, _ := e.do(bmA, http.MethodPost, "/api/v1/invoices/"+invB.ID.String()+"/document-link", nil); st != 403 {
		t.Fatalf("P4-ACL-04 tautan dokumen invoice Tower B harus 403, got %d", st)
	}
	// grant property tetap melihat semuanya
	st, body = e.do(fin, http.MethodGet, "/api/v1/payments", nil)
	if ids := listIDs(t, body); st != 200 || !containsID(ids, payA.ID) || !containsID(ids, payB.ID) {
		t.Fatalf("finance (property-wide) harus melihat kedua pembayaran: %d", st)
	}
}
