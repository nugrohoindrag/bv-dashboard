package app_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// PRD P2 v2.1 §7 Housekeeping: Cleaning Route (P2-RTE-01..03), shift pada jadwal (P2-SHF-04),
// consumable per cleaning task Web & Staff App (P2-CNS-02, P2-MOB-05). Lihat docs/p2-v21-readiness.md & ADR-017.

type routeRun struct {
	ID             uuid.UUID `json:"id"`
	RunDate        string    `json:"run_date"`
	Status         string    `json:"status"`
	TotalStops     int       `json:"total_stops"`
	CompletedStops int       `json:"completed_stops"`
	ProgressPct    int       `json:"progress_pct"`
	Stops          []struct {
		TaskID    uuid.UUID `json:"task_id"`
		SortOrder int       `json:"sort_order"`
		Status    string    `json:"status"`
	} `json:"stops"`
	NextStop *struct {
		SortOrder int `json:"sort_order"`
	} `json:"next_stop"`
}

func TestP2v21CleaningRoute(t *testing.T) {
	e := setup(t)
	hkSpv := e.login("hk.spv@demo.buildingvision.id")
	siti := e.login("siti@demo.buildingvision.id")
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	sitiID := e.refs.Users["housekeeping_staff"]
	pid := e.refs.PropertyID
	jkt := mustLoc()
	now := time.Now().In(jkt)
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")

	// shift per domain (D-P2-05): route hanya boleh memakai shift Housekeeping
	var hkShift, secShift struct {
		ID        uuid.UUID `json:"id"`
		StartTime string    `json:"start_time"`
	}
	start := now.Add(-10 * time.Minute).Format("15:04")
	st, body := e.do(hkSpv, http.MethodPost, "/api/v1/housekeeping/shifts", map[string]any{"property_id": pid, "code": "PG", "name": "Pagi", "start_time": start, "end_time": now.Add(8 * time.Hour).Format("15:04")})
	e.mustJSON(st, body, 201, &hkShift)
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/security/shifts", map[string]any{"property_id": pid, "code": "PG", "name": "Pagi", "start_time": "07:00", "end_time": "15:00"})
	e.mustJSON(st, body, 201, &secShift)

	stops := []map[string]any{
		{"location_id": e.refs.LobbyA, "estimated_minutes": 30},
		{"location_id": e.refs.ToiletA12, "estimated_minutes": 30, "notes": "Cek sabun"},
		{"location_id": e.refs.ToiletB3, "estimated_minutes": 30},
	}
	// ---- validasi ----
	if st, _ := e.do(siti, http.MethodPost, "/api/v1/cleaning-routes", map[string]any{"name": "X", "stops": stops}); st != 403 {
		t.Fatalf("staf tidak boleh menyusun route: %d", st)
	}
	if st, _ := e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-routes", map[string]any{"name": "X", "property_id": pid, "stops": []map[string]any{}}); st != 400 {
		t.Fatalf("route tanpa stop harus 400: %d", st)
	}
	if st, _ := e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-routes", map[string]any{"name": "X", "property_id": pid, "stops": []map[string]any{{"location_id": uuid.New()}}}); st != 400 {
		t.Fatalf("stop lokasi tak dikenal harus 400: %d", st)
	}
	if st, _ := e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-routes", map[string]any{"name": "X", "stops": stops, "shift_id": secShift.ID}); st != 400 {
		t.Fatalf("shift Security pada route cleaning harus 400: %d", st)
	}

	// ---- P2-RTE-01: route per staf/shift dengan estimasi ----
	var route struct {
		ID           uuid.UUID `json:"id"`
		RouteCode    string    `json:"route_code"`
		StartTime    string    `json:"start_time"`
		ShiftName    *string   `json:"shift_name"`
		TotalMinutes int       `json:"total_minutes"`
		Version      int       `json:"version"`
		Stops        []struct {
			SortOrder     int `json:"sort_order"`
			OffsetMinutes int `json:"offset_minutes"`
		} `json:"stops"`
		TodayRun *routeRun `json:"today_run"`
	}
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-routes", map[string]any{"name": "Route Tower A Pagi", "stops": stops, "shift_id": hkShift.ID,
		"default_assignee_user_id": sitiID, "responsible_team_id": e.refs.Teams["housekeeping"], "cleaning_type": "routine"})
	e.mustJSON(st, body, 201, &route)
	if route.RouteCode != "CRT-000001" || route.StartTime != start || route.ShiftName == nil || route.TotalMinutes != 90 || len(route.Stops) != 3 || route.Stops[2].OffsetMinutes != 60 {
		t.Fatalf("route (start_time default = jam mulai shift, offset berantai): %s", body)
	}

	// ---- P2-RTE-02: run harian → cleaning task berurutan ----
	var runs struct {
		Data []routeRun `json:"data"`
	}
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/cleaning-route-runs?route_id="+route.ID.String()+"&date="+today, nil)
	e.mustJSON(st, body, 200, &runs)
	if len(runs.Data) != 1 || runs.Data[0].TotalStops != 3 || len(runs.Data[0].Stops) != 3 || runs.Data[0].Status != "scheduled" || runs.Data[0].NextStop == nil || runs.Data[0].NextStop.SortOrder != 1 {
		t.Fatalf("run hari ini: %s", body)
	}
	run := runs.Data[0]
	var gen struct {
		Generated int `json:"generated"`
	}
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-routes/"+route.ID.String()+"/generate", nil)
	e.mustJSON(st, body, 200, &gen)
	if gen.Generated != 0 {
		t.Fatalf("generator run harus idempotent: %s", body)
	}
	var task struct {
		ID        uuid.UUID      `json:"id"`
		Status    string         `json:"status"`
		Title     string         `json:"title"`
		Extension map[string]any `json:"extension"`
		Assignee  struct {
			UserID *uuid.UUID `json:"user_id"`
		} `json:"assignee"`
	}
	st, body = e.do(siti, http.MethodGet, "/api/v1/tasks/"+run.Stops[1].TaskID.String(), nil)
	e.mustJSON(st, body, 200, &task)
	if task.Assignee.UserID == nil || *task.Assignee.UserID != sitiID || task.Extension["route_code"] != "CRT-000001" || task.Extension["route_stop_order"] != float64(2) || task.Extension["route_total_stops"] != float64(3) {
		t.Fatalf("cleaning task stop 2 (posisi & route di extension): %s", body)
	}
	// Staff App: run saya + urutan area di work bundle
	st, body = e.do(siti, http.MethodGet, "/api/v1/cleaning-route-runs?mine=true", nil)
	e.mustJSON(st, body, 200, &runs)
	if len(runs.Data) != 1 || runs.Data[0].ID != run.ID {
		t.Fatalf("run saya hari ini: %s", body)
	}
	var bundle struct {
		CleaningRouteRuns []routeRun `json:"cleaning_route_runs"`
		CleaningTasks     []struct {
			ID uuid.UUID `json:"id"`
		} `json:"cleaning_tasks"`
	}
	st, body = e.do(siti, http.MethodGet, "/api/v1/sync/work-bundle?device_id=dev-route", nil)
	e.mustJSON(st, body, 200, &bundle)
	if len(bundle.CleaningRouteRuns) != 1 || len(bundle.CleaningRouteRuns[0].Stops) != 3 || len(bundle.CleaningTasks) < 3 {
		t.Fatalf("work bundle route run: %s", body)
	}

	// ---- P2-RTE-03: progres x dari y area ----
	for i, stop := range run.Stops {
		st, body = e.do(siti, http.MethodPost, "/api/v1/tasks/"+stop.TaskID.String()+"/start", nil)
		e.mustJSON(st, body, 200, nil)
		if i == 0 {
			var r routeRun
			st, body = e.do(siti, http.MethodGet, "/api/v1/cleaning-route-runs/"+run.ID.String(), nil)
			e.mustJSON(st, body, 200, &r)
			if r.Status != "in_progress" || r.CompletedStops != 0 {
				t.Fatalf("run in_progress setelah stop pertama dimulai: %s", body)
			}
		}
		st, body = e.do(siti, http.MethodPost, "/api/v1/tasks/"+stop.TaskID.String()+"/complete", map[string]any{"completion_notes": "Bersih"})
		e.mustJSON(st, body, 200, nil)
		var r routeRun
		st, body = e.do(siti, http.MethodGet, "/api/v1/cleaning-route-runs/"+run.ID.String(), nil)
		e.mustJSON(st, body, 200, &r)
		if r.CompletedStops != i+1 {
			t.Fatalf("progres %d dari 3: %s", i+1, body)
		}
		if i == 0 && r.ProgressPct != 33 {
			t.Fatalf("progress_pct 1/3: %s", body)
		}
		if i == 2 && (r.Status != "completed" || r.NextStop != nil) {
			t.Fatalf("run selesai setelah seluruh area: %s", body)
		}
	}

	// ---- ubah route (If-Match) → run ke depan dibuat ulang; run hari ini tetap ----
	if st, _ := e.doIfMatch(hkSpv, http.MethodPatch, "/api/v1/cleaning-routes/"+route.ID.String(), map[string]any{"name": "Route Tower A"}, route.Version+3); st != 409 {
		t.Fatalf("If-Match basi harus 409: %d", st)
	}
	st, body = e.doIfMatch(hkSpv, http.MethodPatch, "/api/v1/cleaning-routes/"+route.ID.String(), map[string]any{"stops": stops[:2]}, route.Version)
	e.mustJSON(st, body, 200, &route)
	if len(route.Stops) != 2 {
		t.Fatalf("route 2 stop: %s", body)
	}
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/cleaning-route-runs?route_id="+route.ID.String()+"&date="+tomorrow, nil)
	e.mustJSON(st, body, 200, &runs)
	if len(runs.Data) != 1 || runs.Data[0].TotalStops != 2 {
		t.Fatalf("run besok mengikuti definisi route baru: %s", body)
	}
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/cleaning-route-runs?route_id="+route.ID.String()+"&date="+today, nil)
	e.mustJSON(st, body, 200, &runs)
	if len(runs.Data) != 1 || runs.Data[0].TotalStops != 3 || runs.Data[0].Status != "completed" {
		t.Fatalf("run hari ini (selesai) tidak boleh berubah: %s", body)
	}
	// nonaktifkan → run yang belum dimulai dibatalkan
	st, _ = e.do(hkSpv, http.MethodDelete, "/api/v1/cleaning-routes/"+route.ID.String(), nil)
	if st != 204 {
		t.Fatalf("nonaktifkan route: %d", st)
	}
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/cleaning-route-runs?route_id="+route.ID.String()+"&date="+tomorrow, nil)
	e.mustJSON(st, body, 200, &runs)
	if len(runs.Data) != 0 {
		t.Fatalf("run besok harus dibatalkan setelah route nonaktif: %s", body)
	}

	// ---- P2-SHF-04: cleaning schedule & patrol schedule terkait shift ----
	var cs struct {
		StartTime string     `json:"start_time"`
		ShiftID   *uuid.UUID `json:"shift_id"`
		ShiftName *string    `json:"shift_name"`
	}
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-schedules", map[string]any{"name": "Lobby pagi", "location_id": e.refs.LobbyA, "shift_id": hkShift.ID, "requires_photo": false})
	e.mustJSON(st, body, 201, &cs)
	if cs.ShiftID == nil || *cs.ShiftID != hkShift.ID || cs.StartTime != start || cs.ShiftName == nil {
		t.Fatalf("cleaning schedule dengan shift (start_time default jam mulai shift): %s", body)
	}
	if st, _ := e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-schedules", map[string]any{"name": "X", "location_id": e.refs.LobbyA, "shift_id": secShift.ID}); st != 400 {
		t.Fatalf("shift Security pada jadwal cleaning harus 400: %d", st)
	}
	var cp struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/checkpoints", map[string]any{"name": "CP Lobby", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, &cp)
	var pr struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/patrol-routes", map[string]any{"property_id": pid, "name": "Rute Pagi", "checkpoints": []map[string]any{{"checkpoint_id": cp.ID, "sort_order": 1}}})
	e.mustJSON(st, body, 201, &pr)
	var ps struct {
		StartTime string     `json:"start_time"`
		ShiftID   *uuid.UUID `json:"shift_id"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/patrol-schedules", map[string]any{"route_id": pr.ID, "name": "Patroli pagi", "shift_id": secShift.ID})
	e.mustJSON(st, body, 201, &ps)
	if ps.ShiftID == nil || ps.StartTime != "07:00" {
		t.Fatalf("patrol schedule dengan shift: %s", body)
	}
	if st, _ := e.do(secSpv, http.MethodPost, "/api/v1/patrol-schedules", map[string]any{"route_id": pr.ID, "name": "X", "shift_id": hkShift.ID}); st != 400 {
		t.Fatalf("shift Housekeeping pada jadwal patroli harus 400: %d", st)
	}
}

func TestP2v21Consumables(t *testing.T) {
	e := setup(t)
	hkSpv := e.login("hk.spv@demo.buildingvision.id")
	siti := e.login("siti@demo.buildingvision.id")
	admin := e.login("admin@org-a.test")
	tech := e.login("budi@demo.buildingvision.id")
	sitiID := e.refs.Users["housekeeping_staff"]
	pid := e.refs.PropertyID

	var loc struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(admin, http.MethodPost, "/api/v1/inventory/stock-locations", map[string]any{"property_id": pid, "name": "Gudang HK", "is_default": true})
	e.mustJSON(st, body, 201, &loc)
	var soap, filter struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/inventory/items", map[string]any{"name": "Sabun cair 5L", "category": "consumable", "unit": "liter", "min_stock": 8, "unit_cost": 20000})
	e.mustJSON(st, body, 201, &soap)
	st, body = e.do(admin, http.MethodPost, "/api/v1/inventory/items", map[string]any{"name": "Filter AHU", "category": "spare_part", "unit": "pcs", "unit_cost": 150000})
	e.mustJSON(st, body, 201, &filter)
	for _, it := range []uuid.UUID{soap.ID, filter.ID} {
		st, body = e.do(admin, http.MethodPost, "/api/v1/inventory/stock-transactions", map[string]any{"item_id": it, "stock_location_id": loc.ID, "transaction_type": "in", "quantity": 10})
		e.mustJSON(st, body, 201, nil)
	}
	var task workItem
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-tasks", map[string]any{"title": "Spot cleaning toilet", "location_id": e.refs.ToiletA12, "assignee_user_id": sitiID, "cleaning_type": "spot"})
	e.mustJSON(st, body, 201, &task)
	tid := task.ID.String()

	// ---- P2-CNS-02 (Web): pemakaian mengurangi stok ----
	if st, _ := e.do(siti, http.MethodPost, "/api/v1/tasks/"+tid+"/consumables", map[string]any{"item_id": filter.ID, "quantity": 1}); st != 400 {
		t.Fatalf("spare part bukan consumable harus 400: %d", st)
	}
	if st, _ := e.do(siti, http.MethodPost, "/api/v1/tasks/"+tid+"/consumables", map[string]any{"item_id": soap.ID, "quantity": 0}); st != 400 {
		t.Fatalf("quantity 0 harus 400: %d", st)
	}
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/tasks/"+tid+"/consumables", map[string]any{"item_id": soap.ID, "quantity": 1}); st != 403 {
		t.Fatalf("teknisi tidak boleh mencatat consumable HK: %d", st)
	}
	if st, _ := e.do(siti, http.MethodPost, "/api/v1/tasks/"+tid+"/consumables", map[string]any{"item_id": soap.ID, "quantity": 50}); st != 409 {
		t.Fatalf("stok kurang harus 409 INSUFFICIENT_STOCK: %d", st)
	}
	var use struct {
		ID        uuid.UUID `json:"id"`
		Quantity  float64   `json:"quantity"`
		TotalCost *int64    `json:"total_cost"`
	}
	st, body = e.do(siti, http.MethodPost, "/api/v1/tasks/"+tid+"/consumables", map[string]any{"item_id": soap.ID, "quantity": 1.5, "note": "Isi dispenser"})
	e.mustJSON(st, body, 201, &use)
	if use.Quantity != 1.5 || use.TotalCost == nil || *use.TotalCost != 30000 {
		t.Fatalf("pemakaian consumable: %s", body)
	}
	var item struct {
		TotalQuantity float64 `json:"total_quantity"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/inventory/items/"+soap.ID.String(), nil)
	e.mustJSON(st, body, 200, &item)
	if item.TotalQuantity != 8.5 {
		t.Fatalf("stok berkurang 1.5: %s", body)
	}
	// koreksi oleh pencatat → stok kembali
	if st, _ := e.do(siti, http.MethodDelete, "/api/v1/tasks/"+tid+"/consumables/"+use.ID.String(), nil); st != 204 {
		t.Fatalf("koreksi consumable: %d", st)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/inventory/items/"+soap.ID.String(), nil)
	e.mustJSON(st, body, 200, &item)
	if item.TotalQuantity != 10 {
		t.Fatalf("stok kembali setelah koreksi: %s", body)
	}

	// ---- P2-MOB-05 (Staff App offline): mutation use_consumable idempoten + katalog di bundle ----
	var bundle struct {
		ConsumableItems []struct {
			ID        uuid.UUID `json:"id"`
			Available float64   `json:"available"`
		} `json:"consumable_items"`
		Properties []struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		} `json:"properties"`
	}
	st, body = e.do(siti, http.MethodGet, "/api/v1/sync/work-bundle?device_id=dev-cns", nil)
	e.mustJSON(st, body, 200, &bundle)
	if len(bundle.ConsumableItems) != 1 || bundle.ConsumableItems[0].ID != soap.ID || bundle.ConsumableItems[0].Available != 10 {
		t.Fatalf("katalog consumable di bundle (tanpa spare part): %s", body)
	}
	if len(bundle.Properties) != 1 || bundle.Properties[0].ID != pid || bundle.Properties[0].Name == "" {
		t.Fatalf("bundle.properties (pilihan property Panic/clock-in): %s", body)
	}
	var res struct {
		Results []struct {
			Status   string         `json:"status"`
			Response map[string]any `json:"response"`
		} `json:"results"`
	}
	mid := uuid.New()
	m := mut(mid, "task", task.ID, "use_consumable", 1, map[string]any{"item_id": soap.ID, "quantity": 3}, time.Now())
	st, body = e.do(siti, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-cns", "mutations": []map[string]any{m}})
	e.mustJSON(st, body, 200, &res)
	if len(res.Results) != 1 || res.Results[0].Status != "applied" || res.Results[0].Response["consumable_usage_id"] == nil {
		t.Fatalf("use_consumable sync: %s", body)
	}
	st, body = e.do(siti, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-cns", "mutations": []map[string]any{m}})
	e.mustJSON(st, body, 200, &res)
	if res.Results[0].Status != "duplicate" && res.Results[0].Status != "applied" {
		t.Fatalf("mutation ulang harus idempoten: %s", body)
	}
	// aturan domain 409 lewat sync → rejected dengan kode asli (bukan VALIDATION_ERROR "internal: …")
	var rej struct {
		Results []struct {
			Status     string `json:"status"`
			ReasonCode string `json:"reason_code"`
			Detail     string `json:"detail"`
		} `json:"results"`
	}
	st, body = e.do(siti, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-cns", "mutations": []map[string]any{
		mut(uuid.New(), "task", task.ID, "use_consumable", 2, map[string]any{"item_id": soap.ID, "quantity": 1000}, time.Now())}})
	e.mustJSON(st, body, 200, &rej)
	if len(rej.Results) != 1 || rej.Results[0].Status != "rejected" || rej.Results[0].ReasonCode != "INSUFFICIENT_STOCK" || strings.HasPrefix(rej.Results[0].Detail, "internal") {
		t.Fatalf("stok kurang via sync: %s", body)
	}
	var list struct {
		Data      []struct{ ID uuid.UUID } `json:"data"`
		TotalCost int64                    `json:"total_cost"`
	}
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/tasks/"+tid+"/consumables", nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 1 || list.TotalCost != 60000 {
		t.Fatalf("daftar consumable task (sekali walau mutation dikirim ulang): %s", body)
	}
	// stok 7 < min 8 → low stock ke Housekeeping Supervisor (P2-CNS-03)
	e.dispatch(t)
	if ib := e.inboxOf(t, hkSpv); !hasType(ib, "inventory_low_stock") {
		t.Fatalf("low stock consumable harus ke Housekeeping Supervisor: %+v", ib.Data)
	}
	// task closed → tidak bisa dicatat lagi
	st, body = e.do(siti, http.MethodPost, "/api/v1/tasks/"+tid+"/start", nil)
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(siti, http.MethodPost, "/api/v1/tasks/"+tid+"/complete", map[string]any{"completion_notes": "ok"})
	e.mustJSON(st, body, 200, nil)
	if st, _ := e.do(siti, http.MethodDelete, "/api/v1/tasks/"+tid+"/consumables/"+list.Data[0].ID.String(), nil); st != 409 {
		t.Fatalf("koreksi setelah task selesai harus 409: %d", st)
	}
}
