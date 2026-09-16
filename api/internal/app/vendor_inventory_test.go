package app_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// P1.5 — Vendor Management (PRD §24) & Inventory / Spare Parts + Parts Usage vs Work Order (PRD §25; Mobile Staff "Parts Usage").
func TestVendorAndInventory(t *testing.T) {
	e := setup(t)
	engMgr := e.login("eng.manager@demo.buildingvision.id")
	engSpv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	joko := e.login("joko@demo.buildingvision.id")
	budiID := e.refs.Users["technician"]

	// ---- Vendor ----
	var vnd struct {
		ID          uuid.UUID `json:"id"`
		VendorCode  string    `json:"vendor_code"`
		Status      string    `json:"status"`
		Contacts    []any     `json:"contacts"`
		Performance struct {
			TotalWorkOrders int `json:"total_work_orders"`
			OpenWorkOrders  int `json:"open_work_orders"`
		} `json:"performance"`
	}
	st, body := e.do(engMgr, http.MethodPost, "/api/v1/vendors", map[string]any{"name": "PT Dingin Sejahtera", "service_categories": []string{"hvac", "electrical"}, "contact_name": "Pak Heri", "contact_phone": "0811", "contacts": []map[string]any{{"name": "Heri", "role": "Sales", "phone": "0811", "is_primary": true}}})
	e.mustJSON(st, body, 201, &vnd)
	if !strings.HasPrefix(vnd.VendorCode, "VND-") || vnd.Status != "active" || len(vnd.Contacts) != 1 {
		t.Fatalf("vendor: %+v", vnd)
	}
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/vendors", map[string]any{"name": "X"}); st != 403 {
		t.Fatalf("technician create vendor: %d", st)
	}
	if st, _ := e.do(engMgr, http.MethodPost, "/api/v1/vendors", map[string]any{"name": "Y", "service_categories": []string{"unknown"}}); st != 400 {
		t.Fatalf("kategori vendor tak dikenal harus 400: %d", st)
	}
	// WO → assign vendor (Vendor Work Order)
	var wo workItem
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/work-orders", map[string]any{"property_id": e.refs.PropertyID, "work_order_type": "repair", "title": "Servis chiller", "location_id": e.refs.MechRoomA12, "assignee_user_id": budiID, "requires_evidence": false})
	e.mustJSON(st, body, 201, &wo)
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/vendor", map[string]any{"vendor_id": vnd.ID, "vendor_notes": "SPK 12/2026"})
	var wv struct {
		VendorID   *uuid.UUID `json:"vendor_id"`
		VendorName *string    `json:"vendor_name"`
	}
	e.mustJSON(st, body, 200, &wv)
	if wv.VendorID == nil || *wv.VendorID != vnd.ID || wv.VendorName == nil {
		t.Fatalf("assign vendor: %+v", wv)
	}
	st, body = e.do(engMgr, http.MethodGet, "/api/v1/vendors/"+vnd.ID.String(), nil)
	e.mustJSON(st, body, 200, &vnd)
	if vnd.Performance.TotalWorkOrders != 1 || vnd.Performance.OpenWorkOrders != 1 {
		t.Fatalf("performance vendor: %+v", vnd.Performance)
	}
	// vendor dengan WO terbuka tidak bisa dinonaktifkan
	if st, _ := e.do(engMgr, http.MethodDelete, "/api/v1/vendors/"+vnd.ID.String(), nil); st != 409 {
		t.Fatalf("deactivate vendor dengan WO open harus 409: %d", st)
	}
	st, body = e.do(engMgr, http.MethodGet, "/api/v1/vendors/"+vnd.ID.String()+"/work-orders", nil)
	if st != 200 || !strings.Contains(string(body), wo.Number) {
		t.Fatalf("vendor work orders: %d %s", st, body)
	}

	// ---- Inventory: stock location, item, stock in ----
	var loc struct {
		ID        uuid.UUID `json:"id"`
		IsDefault bool      `json:"is_default"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/inventory/stock-locations", map[string]any{"property_id": e.refs.PropertyID, "name": "Gudang Teknik B1", "is_default": true})
	e.mustJSON(st, body, 201, &loc)
	var item struct {
		ID            uuid.UUID `json:"id"`
		ItemCode      string    `json:"item_code"`
		TotalQuantity float64   `json:"total_quantity"`
		LowStock      bool      `json:"low_stock"`
		Levels        []struct {
			Quantity float64 `json:"quantity"`
		} `json:"levels"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/inventory/items", map[string]any{"name": "Filter AHU 24x24", "category": "spare_part", "equipment_category_code": "HVAC", "unit": "pcs", "min_stock": 5, "unit_cost": 150000})
	e.mustJSON(st, body, 201, &item)
	if !strings.HasPrefix(item.ItemCode, "ITM-") || !item.LowStock {
		t.Fatalf("item: %+v", item)
	}
	// stok masuk 10
	var trx struct {
		TransactionNumber string  `json:"transaction_number"`
		Quantity          float64 `json:"quantity"`
		BalanceAfter      float64 `json:"balance_after"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/inventory/stock-transactions", map[string]any{"item_id": item.ID, "stock_location_id": loc.ID, "transaction_type": "in", "quantity": 10, "unit_cost": 150000, "note": "PO-001"})
	e.mustJSON(st, body, 201, &trx)
	if !strings.HasPrefix(trx.TransactionNumber, "STK-") || trx.BalanceAfter != 10 {
		t.Fatalf("stock in: %+v", trx)
	}
	// technician tidak boleh stock in/adjust
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/inventory/stock-transactions", map[string]any{"item_id": item.ID, "stock_location_id": loc.ID, "transaction_type": "in", "quantity": 1}); st != 403 {
		t.Fatalf("technician stock in: %d", st)
	}
	// ---- Parts Usage vs WO (assignee budi) ----
	var part struct {
		ID        uuid.UUID `json:"id"`
		Quantity  float64   `json:"quantity"`
		TotalCost int64     `json:"total_cost"`
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/parts", map[string]any{"item_id": item.ID, "quantity": 2, "client_part_id": "c-1"})
	e.mustJSON(st, body, 201, &part)
	if part.Quantity != 2 || part.TotalCost != 300000 {
		t.Fatalf("parts usage: %+v", part)
	}
	// idempoten (client_part_id sama) → tidak mengurangi stok lagi
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/parts", map[string]any{"item_id": item.ID, "quantity": 2, "client_part_id": "c-1"})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do(engMgr, http.MethodGet, "/api/v1/inventory/items/"+item.ID.String(), nil)
	e.mustJSON(st, body, 200, &item)
	if item.TotalQuantity != 8 {
		t.Fatalf("stok setelah usage harus 8, got %v", item.TotalQuantity)
	}
	// teknisi lain (bukan assignee) tidak boleh mencatat parts pada WO ini
	if st, _ := e.do(joko, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/parts", map[string]any{"item_id": item.ID, "quantity": 1}); st != 403 {
		t.Fatalf("non-assignee parts usage: %d", st)
	}
	// stok tidak cukup → 409 INSUFFICIENT_STOCK (stok tidak pernah negatif)
	if st, body := e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/parts", map[string]any{"item_id": item.ID, "quantity": 50}); st != 409 || !strings.Contains(string(body), "INSUFFICIENT_STOCK") {
		t.Fatalf("stok kurang harus 409: %d %s", st, body)
	}
	// low stock alert: pakai 4 lagi → sisa 4 < min 5 → notifikasi supervisor engineering
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/parts", map[string]any{"item_id": item.ID, "quantity": 4})
	e.mustJSON(st, body, 201, &part)
	e.dispatch(t)
	if ib := e.inboxOf(t, engSpv); !hasType(ib, "inventory_low_stock") {
		t.Fatalf("notifikasi low stock ke supervisor engineering tidak ada: %+v", ib.Data)
	}
	// daftar parts WO + total biaya; WO actual_cost & parts_usage terisi (internal)
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/work-orders/"+wo.ID.String()+"/parts", nil)
	if st != 200 || !strings.Contains(string(body), `"total_cost":900000`) {
		t.Fatalf("parts list: %d %s", st, body)
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/work-orders/"+wo.ID.String(), nil)
	if st != 200 || !strings.Contains(string(body), "Filter AHU 24x24 × 2") || !strings.Contains(string(body), `"amount":900000`) {
		t.Fatalf("WO parts_usage/actual_cost: %d %s", st, body)
	}
	// koreksi: hapus pemakaian 4 → stok kembali 8
	st, _ = e.do(tech, http.MethodDelete, "/api/v1/work-orders/"+wo.ID.String()+"/parts/"+part.ID.String(), nil)
	if st != 204 {
		t.Fatalf("remove part: %d", st)
	}
	st, body = e.do(engMgr, http.MethodGet, "/api/v1/inventory/items/"+item.ID.String(), nil)
	e.mustJSON(st, body, 200, &item)
	if item.TotalQuantity != 8 {
		t.Fatalf("stok setelah koreksi harus 8, got %v", item.TotalQuantity)
	}
	// riwayat transaksi memuat usage/in dengan referensi WO
	st, body = e.do(engMgr, http.MethodGet, "/api/v1/inventory/stock-transactions?item_id="+item.ID.String(), nil)
	if st != 200 || !strings.Contains(string(body), `"transaction_type":"usage"`) || !strings.Contains(string(body), wo.Number) {
		t.Fatalf("stock transactions: %d %s", st, body)
	}
	// adjustment (stock opname) ke 6
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/inventory/stock-transactions", map[string]any{"item_id": item.ID, "stock_location_id": loc.ID, "transaction_type": "adjustment", "quantity": 6, "note": "Stock opname"})
	e.mustJSON(st, body, 201, &trx)
	if trx.Quantity != -2 || trx.BalanceAfter != 6 {
		t.Fatalf("adjustment: %+v", trx)
	}
	// low stock filter
	st, body = e.do(engMgr, http.MethodGet, "/api/v1/inventory/items?low_stock=true", nil)
	if st != 200 || strings.Contains(string(body), item.ItemCode) {
		t.Fatalf("low stock filter (6 >= 5 → tidak low): %d", st)
	}
}
