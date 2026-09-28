package app_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestP4v21InventoryLowStockScope: PRD P4 v2.1 B-06 — laporan inventory (low stock) mengikuti filter property; stok dihitung
// dari gudang property tersebut, bukan total organisasi.
func TestP4v21InventoryLowStockScope(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	p1 := e.refs.PropertyID
	var p2 struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(admin, http.MethodPost, "/api/v1/properties", map[string]any{"name": "Property Gudang Kedua", "details": map[string]any{"profile": "office"}})
	e.mustJSON(st, body, 201, &p2)

	type idOnly struct {
		ID uuid.UUID `json:"id"`
	}
	var loc1, loc2, itemX, itemY, itemZ idOnly
	st, body = e.do(admin, http.MethodPost, "/api/v1/inventory/stock-locations", map[string]any{"property_id": p1, "name": "Gudang P1", "is_default": true})
	e.mustJSON(st, body, 201, &loc1)
	st, body = e.do(admin, http.MethodPost, "/api/v1/inventory/stock-locations", map[string]any{"property_id": p2.ID, "name": "Gudang P2", "is_default": true})
	e.mustJSON(st, body, 201, &loc2)
	st, body = e.do(admin, http.MethodPost, "/api/v1/inventory/items", map[string]any{"name": "Lampu LED 18W", "category": "spare_part", "unit": "pcs", "min_stock": 5})
	e.mustJSON(st, body, 201, &itemX)
	st, body = e.do(admin, http.MethodPost, "/api/v1/inventory/items", map[string]any{"name": "Sekring 10A", "category": "spare_part", "unit": "pcs", "min_stock": 3})
	e.mustJSON(st, body, 201, &itemY)
	st, body = e.do(admin, http.MethodPost, "/api/v1/inventory/items", map[string]any{"name": "Kabel NYM 2x1,5", "category": "spare_part", "unit": "roll", "min_stock": 2})
	e.mustJSON(st, body, 201, &itemZ) // belum pernah distok di gudang mana pun
	for _, in := range []map[string]any{
		{"item_id": itemX.ID, "stock_location_id": loc1.ID, "quantity": 10}, // X: P1 10 (≥ 5), P2 1 (< 5), total 11
		{"item_id": itemX.ID, "stock_location_id": loc2.ID, "quantity": 1},
		{"item_id": itemY.ID, "stock_location_id": loc1.ID, "quantity": 2}, // Y: hanya P1, 2 (< 3)
	} {
		in["transaction_type"] = "in"
		if st, body := e.do(admin, http.MethodPost, "/api/v1/inventory/stock-transactions", in); st != 201 {
			t.Fatalf("stock in: %d %s", st, body)
		}
	}

	low := func(query string) map[string]float64 {
		t.Helper()
		var rep struct {
			Summary    map[string]float64 `json:"summary"`
			Breakdowns map[string][]struct {
				Key    string             `json:"key"`
				Values map[string]float64 `json:"values"`
			} `json:"breakdowns"`
		}
		st, body := e.do(admin, http.MethodGet, "/api/v1/reports/inventory"+query, nil)
		e.mustJSON(st, body, 200, &rep)
		out := map[string]float64{}
		for _, r := range rep.Breakdowns["low_stock"] {
			out[r.Key] = r.Values["quantity"]
		}
		if int(rep.Summary["low_stock_items"]) != len(out) {
			t.Fatalf("B-06 %s: summary low_stock_items=%v ≠ breakdown %d", query, rep.Summary["low_stock_items"], len(out))
		}
		return out
	}
	x, y, z := itemX.ID.String(), itemY.ID.String(), itemZ.ID.String()

	r1 := low("?property_id=" + p1.String())
	if _, ok := r1[x]; ok {
		t.Fatalf("B-06 P1: X (stok P1 10 ≥ 5) tidak boleh low stock: %v", r1)
	}
	if q, ok := r1[y]; !ok || q != 2 {
		t.Fatalf("B-06 P1: Y harus low stock dengan stok 2: %v", r1)
	}
	if _, ok := r1[z]; ok {
		t.Fatalf("B-06 P1: Z belum distok di P1 → tidak dihitung untuk property: %v", r1)
	}

	r2 := low("?property_id=" + p2.ID.String())
	if q, ok := r2[x]; !ok || q != 1 {
		t.Fatalf("B-06 P2: X harus low stock dengan stok gudang P2 = 1 (bukan total 11): %v", r2)
	}
	if _, ok := r2[y]; ok {
		t.Fatalf("B-06 P2: Y tidak disimpan di P2: %v", r2)
	}

	all := low("")
	if _, ok := all[x]; ok {
		t.Fatalf("B-06 tanpa filter: X total 11 ≥ 5 tidak boleh low stock: %v", all)
	}
	if _, ok := all[y]; !ok {
		t.Fatalf("B-06 tanpa filter: Y harus low stock: %v", all)
	}
	if _, ok := all[z]; !ok {
		t.Fatalf("B-06 tanpa filter: Z (belum pernah distok) harus low stock: %v", all)
	}
}
