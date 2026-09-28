package app_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// PRD P1 v2.1 (Roadmap v2.1) — penutupan gap P1: GAP-P1-03 (occupancy hotel), GAP-P1-04 (If-Match denah),
// GAP-P1-05 (scope Building facility/overview/reports), GAP-P1-07 (rantai keluhan lintas tim, Roadmap §17 contoh 1),
// GAP-P1-09 (validasi tipe incident). Lihat docs/p1-v21-readiness.md & ADR-016.

type chainView struct {
	Status    string `json:"status"`
	OpenCount int    `json:"open_count"`
	TeamCount int    `json:"team_count"`
	Items     []struct {
		ObjectType      string  `json:"object_type"`
		Number          string  `json:"number"`
		Status          string  `json:"status"`
		Depth           int     `json:"depth"`
		ParentLabel     string  `json:"parent_label"`
		TeamName        *string `json:"team_name"`
		FollowUpPurpose *string `json:"follow_up_purpose"`
		Open            bool    `json:"open"`
		DeepLink        string  `json:"deep_link"`
	} `json:"items"`
}

func (e *env) srStatus(t *testing.T, token string, id uuid.UUID) string {
	t.Helper()
	var sr struct {
		Status string `json:"status"`
	}
	st, body := e.do(token, http.MethodGet, "/api/v1/service-requests/"+id.String(), nil)
	e.mustJSON(st, body, 200, &sr)
	return sr.Status
}

// P1-XMW-01..08: SR → inspeksi HK → finding → WO Engineering → perbaikan → inspeksi akhir HK → SR Resolved → konfirmasi → CSAT.
func TestP1v21TenantComplaintChain(t *testing.T) {
	e := setup(t)
	tenA, _ := e.setupTenants(t)
	tr := e.login("tr.manager@demo.buildingvision.id")
	hkSpv := e.login("hk.spv@demo.buildingvision.id")
	siti := e.login("siti@demo.buildingvision.id")
	engSpv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	hkTeam, engTeam := e.refs.Teams["housekeeping"], e.refs.Teams["engineering"]

	var sr struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "cleanliness", "request_type": "complaint", "title": "Toilet bocor & bau", "description": "Air merembes dari wastafel toilet lantai 12"})
	e.mustJSON(st, body, 201, &sr)

	// 1) TRO → Task inspeksi ke team Housekeeping (SR → In Progress)
	var insp workItem
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr.ID.String()+"/tasks", map[string]any{"title": "Inspeksi toilet", "task_type": "inspection", "location_id": e.refs.ToiletA12, "assignee_team_id": hkTeam})
	e.mustJSON(st, body, 201, &insp)
	if got := e.srStatus(t, tr, sr.ID); got != "in_progress" {
		t.Fatalf("SR setelah task inspeksi: %s", got)
	}
	// 2) HK inspeksi → finding (kebocoran) → task selesai. SR TIDAK boleh Resolved (finding masih terbuka).
	st, body = e.do(siti, http.MethodPost, "/api/v1/tasks/"+insp.ID.String()+"/start", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	var fnd struct {
		ID                         uuid.UUID  `json:"id"`
		OriginServiceRequestID     *uuid.UUID `json:"origin_service_request_id"`
		OriginServiceRequestNumber *string    `json:"origin_service_request_number"`
	}
	st, body = e.do(siti, http.MethodPost, "/api/v1/findings", map[string]any{"title": "Pipa wastafel bocor", "finding_type": "inspection", "severity": "high", "location_id": e.refs.ToiletA12, "source_type": "task", "source_id": insp.ID})
	e.mustJSON(st, body, 201, &fnd)
	if fnd.OriginServiceRequestID == nil || *fnd.OriginServiceRequestID != sr.ID || fnd.OriginServiceRequestNumber == nil {
		t.Fatalf("P1-XMW-01: finding harus tertaut ke SR asal: %s", body)
	}
	st, body = e.do(siti, http.MethodPost, "/api/v1/tasks/"+insp.ID.String()+"/complete", map[string]any{"completion_notes": "Ditemukan kebocoran, perlu Engineering"})
	e.mustJSON(st, body, 200, nil)
	if got := e.srStatus(t, tr, sr.ID); got != "in_progress" {
		t.Fatalf("P1-XMW-02: SR tidak boleh Resolved selama finding terbuka, got %s", got)
	}
	// 3) Engineering WO dari finding → teknisi memperbaiki. SR masih menunggu inspeksi akhir.
	var wo struct {
		ID                     uuid.UUID  `json:"id"`
		Number                 string     `json:"number"`
		AllowedActions         []string   `json:"allowed_actions"`
		OriginServiceRequestID *uuid.UUID `json:"origin_service_request_id"`
	}
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/findings/"+fnd.ID.String()+"/work-orders", map[string]any{"assignee_user_id": e.refs.Users["technician"], "assignee_team_id": engTeam, "requires_evidence": false})
	e.mustJSON(st, body, 201, &wo)
	if wo.OriginServiceRequestID == nil || *wo.OriginServiceRequestID != sr.ID {
		t.Fatalf("P1-XMW-01: WO harus tertaut ke SR asal: %s", body)
	}
	// tindak lanjut belum boleh selama WO belum selesai
	if st, body := e.do(engSpv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/tasks", map[string]any{"purpose": "final_inspection"}); st != 409 {
		t.Fatalf("P1-XMW-03: follow-up dari WO belum selesai harus 409, got %d %s", st, body)
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/start", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/complete", map[string]any{"completion_notes": "Seal pipa diganti", "resolution": "Pipa wastafel diperbaiki"})
	e.mustJSON(st, body, 200, &wo)
	if has(wo.AllowedActions, "create_follow_up_task") {
		t.Fatalf("teknisi tanpa operations.tasks.create tidak boleh ditawari create_follow_up_task: %v", wo.AllowedActions)
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/work-orders/"+wo.ID.String(), nil)
	e.mustJSON(st, body, 200, &wo)
	if got := e.srStatus(t, tr, sr.ID); got != "in_progress" {
		t.Fatalf("P1-XMW-02: SR tidak boleh Resolved sebelum inspeksi akhir, got %s", got)
	}
	if !has(wo.AllowedActions, "create_follow_up_task") {
		t.Fatalf("WO completed harus menawarkan create_follow_up_task: %v", wo.AllowedActions)
	}
	// purpose tidak valid → 400; teknisi (tanpa tasks.create) → 403
	if st, _ := e.do(engSpv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/tasks", map[string]any{"purpose": "party"}); st != 400 {
		t.Fatalf("purpose tidak valid harus 400: %d", st)
	}
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/tasks", map[string]any{"purpose": "final_inspection"}); st != 403 {
		t.Fatalf("teknisi membuat follow-up harus 403: %d", st)
	}
	// 4) Inspeksi akhir untuk team Housekeeping (serah terima lintas tim → notifikasi team)
	var final struct {
		ID                     uuid.UUID  `json:"id"`
		Type                   string     `json:"type"`
		Title                  string     `json:"title"`
		FollowUpPurpose        *string    `json:"follow_up_purpose"`
		OriginServiceRequestID *uuid.UUID `json:"origin_service_request_id"`
		Links                  []struct {
			LinkType string `json:"link_type"`
			Label    string `json:"label"`
		} `json:"links"`
	}
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/tasks", map[string]any{"purpose": "final_inspection", "assignee_team_id": hkTeam})
	e.mustJSON(st, body, 201, &final)
	if final.Type != "inspection" || final.FollowUpPurpose == nil || *final.FollowUpPurpose != "final_inspection" || final.OriginServiceRequestID == nil || *final.OriginServiceRequestID != sr.ID || !strings.HasPrefix(final.Title, "Inspeksi akhir") {
		t.Fatalf("P1-XMW-03: task inspeksi akhir: %s", body)
	}
	okLink := false
	for _, l := range final.Links {
		if l.LinkType == "follow_up_of" && l.Label == wo.Number {
			okLink = true
		}
	}
	if !okLink {
		t.Fatalf("task inspeksi akhir harus tertaut follow_up_of WO: %s", body)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, siti); !hasType(ib, "task_assigned") {
		t.Fatalf("P1-XMW-07: team Housekeeping harus menerima penugasan inspeksi akhir: %+v", ib.Data)
	}
	// 5) Timeline lintas tim di detail SR (P1-XMW-04)
	var cv chainView
	st, body = e.do(tr, http.MethodGet, "/api/v1/service-requests/"+sr.ID.String()+"/chain", nil)
	e.mustJSON(st, body, 200, &cv)
	if len(cv.Items) != 4 || cv.OpenCount != 2 || cv.TeamCount < 2 {
		t.Fatalf("P1-XMW-04 timeline: %s", body)
	}
	order := []string{"task", "finding", "work_order", "task"}
	for i, it := range cv.Items {
		if it.ObjectType != order[i] || it.Depth != i+1 || it.DeepLink == "" {
			t.Fatalf("P1-XMW-04 urutan/kedalaman rantai [%d]: %+v", i, it)
		}
	}
	if cv.Items[3].ParentLabel != wo.Number {
		t.Fatalf("P1-XMW-04 parent inspeksi akhir: %+v", cv.Items[3])
	}
	// tenant tidak melihat detail internal (endpoint staf) — P1-XMW-05
	if st, _ := e.do(tenA, http.MethodGet, "/api/v1/service-requests/"+sr.ID.String()+"/chain", nil); st != 403 {
		t.Fatalf("P1-XMW-05: tenant tidak boleh mengakses timeline internal: %d", st)
	}
	// 6) HK menyelesaikan inspeksi akhir → finding resolved otomatis → SR Resolved (seluruh rantai selesai)
	st, body = e.do(siti, http.MethodPost, "/api/v1/tasks/"+final.ID.String()+"/start", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(siti, http.MethodPost, "/api/v1/tasks/"+final.ID.String()+"/complete", map[string]any{"completion_notes": "Tidak bocor lagi, area bersih"})
	e.mustJSON(st, body, 200, nil)
	var f2 struct {
		Status     string  `json:"status"`
		Resolution *string `json:"resolution"`
	}
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/findings/"+fnd.ID.String(), nil)
	e.mustJSON(st, body, 200, &f2)
	if f2.Status != "resolved" || f2.Resolution == nil || !strings.Contains(*f2.Resolution, "Diverifikasi") {
		t.Fatalf("P2-XTW-02: finding resolved setelah inspeksi akhir: %s", body)
	}
	if got := e.srStatus(t, tr, sr.ID); got != "resolved" {
		t.Fatalf("P1-XMW-02: SR harus Resolved setelah seluruh rantai selesai, got %s", got)
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/service-requests/"+sr.ID.String()+"/chain", nil)
	e.mustJSON(st, body, 200, &cv)
	if cv.OpenCount != 0 || cv.Status != "resolved" {
		t.Fatalf("timeline setelah selesai: %s", body)
	}
	// 7) Tenant konfirmasi → Closed → CSAT (P1-XMW-06)
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+sr.ID.String()+"/confirm", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	if got := e.srStatus(t, tr, sr.ID); got != "closed" {
		t.Fatalf("SR setelah konfirmasi tenant: %s", got)
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+sr.ID.String()+"/feedback", map[string]any{"rating": 5, "comment": "Cepat dan rapi"})
	e.mustJSON(st, body, 201, nil)

	// Regresi: SR → Task tunggal tetap Resolved saat task selesai (rantai satu langkah)
	var sr2 struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "cleanliness", "description": "Lantai koridor lengket"})
	e.mustJSON(st, body, 201, &sr2)
	var t2 workItem
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr2.ID.String()+"/tasks", map[string]any{"assignee_user_id": e.refs.Users["housekeeping_staff"]})
	e.mustJSON(st, body, 201, &t2)
	e.do(siti, http.MethodPost, "/api/v1/tasks/"+t2.ID.String()+"/start", map[string]any{})
	st, body = e.do(siti, http.MethodPost, "/api/v1/tasks/"+t2.ID.String()+"/complete", map[string]any{"completion_notes": "Sudah dipel"})
	e.mustJSON(st, body, 200, nil)
	if got := e.srStatus(t, tr, sr2.ID); got != "resolved" {
		t.Fatalf("regresi rantai satu langkah: SR harus Resolved, got %s", got)
	}
}

// P2-XTW-02 (Roadmap §17 contoh 2): Patrol finding → Incident → WO → verifikasi security → Incident baru boleh Closed.
func TestP1v21SecurityVerificationChain(t *testing.T) {
	e := setup(t)
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	engSpv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	officer := e.login("wawan@demo.buildingvision.id")

	var fnd struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(officer, http.MethodPost, "/api/v1/findings", map[string]any{"title": "Pintu darurat tidak menutup", "finding_type": "patrol", "severity": "high", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, &fnd)
	var inc struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"status"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/findings/"+fnd.ID.String()+"/incidents", map[string]any{"title": "Pintu darurat rusak", "category": "property_damage", "severity": "high"})
	e.mustJSON(st, body, 201, &inc)
	var wo workItem
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/incidents/"+inc.ID.String()+"/work-orders", map[string]any{"assignee_user_id": e.refs.Users["technician"], "requires_evidence": false})
	e.mustJSON(st, body, 201, &wo)
	e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/start", map[string]any{})
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/complete", map[string]any{"completion_notes": "Door closer diganti"})
	e.mustJSON(st, body, 200, nil)
	var ver workItem
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/tasks", map[string]any{"purpose": "security_verification", "assignee_user_id": e.refs.Users["security_officer"]})
	e.mustJSON(st, body, 201, &ver)
	// incident belum boleh Closed: resolve dulu lalu close → 409 CHAIN_OPEN_WORK selama verifikasi terbuka
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/incidents/"+inc.ID.String()+"/resolve", map[string]any{"resolution": "WO selesai"})
	e.mustJSON(st, body, 200, &inc)
	if st, body := e.do(secSpv, http.MethodPost, "/api/v1/incidents/"+inc.ID.String()+"/close", map[string]any{}); st != 409 || !strings.Contains(string(body), "CHAIN_OPEN_WORK") {
		t.Fatalf("P2-XTW-02: incident close sebelum verifikasi harus 409 CHAIN_OPEN_WORK, got %d %s", st, body)
	}
	// verifikasi security selesai → incident boleh Closed; finding asal resolved otomatis
	e.do(officer, http.MethodPost, "/api/v1/tasks/"+ver.ID.String()+"/start", map[string]any{})
	st, body = e.do(officer, http.MethodPost, "/api/v1/tasks/"+ver.ID.String()+"/complete", map[string]any{"completion_notes": "Pintu menutup rapat, alarm normal"})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/incidents/"+inc.ID.String()+"/close", map[string]any{})
	e.mustJSON(st, body, 200, &inc)
	if inc.Status != "closed" {
		t.Fatalf("incident closed: %s", body)
	}
	var f struct {
		Status string `json:"status"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/findings/"+fnd.ID.String(), nil)
	e.mustJSON(st, body, 200, &f)
	if f.Status != "resolved" {
		t.Fatalf("finding patrol asal harus resolved setelah verifikasi: %s", body)
	}
}

// P1-BLD-08 (D-P1-02): occupancy hotel mengikuti status kamar.
func TestP1v21HotelOccupancySync(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	var prop, bld, floor struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "property", "name": "Hotel Occupancy", "details": map[string]any{"timezone": "Asia/Jakarta", "profile": "hotel"}})
	e.mustJSON(st, body, 201, &prop)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "building", "parent_id": prop.ID, "name": "Wing A", "details": map[string]any{}})
	e.mustJSON(st, body, 201, &bld)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "floor", "parent_id": bld.ID, "name": "Floor 3", "details": map[string]any{"floor_number": 3}})
	e.mustJSON(st, body, 201, &floor)
	var rt struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/room-types", map[string]any{"property_id": prop.ID, "name": "Superior", "capacity_adults": 2, "base_rate": 700000})
	e.mustJSON(st, body, 201, &rt)
	rooms := map[string]roomResp{}
	for _, n := range []string{"301", "302", "303"} {
		var r roomResp
		st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/rooms", map[string]any{"property_id": prop.ID, "floor_id": floor.ID, "room_type_id": rt.ID, "room_number": n})
		e.mustJSON(st, body, 201, &r)
		rooms[n] = r
	}
	occ := func() (vacant, occupied, inactive int) {
		var s struct {
			Vacant   int `json:"vacant"`
			Occupied int `json:"occupied"`
			Inactive int `json:"inactive"`
		}
		st, body := e.do(admin, http.MethodGet, "/api/v1/occupancy/summary?property_id="+prop.ID.String(), nil)
		e.mustJSON(st, body, 200, &s)
		return s.Vacant, s.Occupied, s.Inactive
	}
	if v, o, i := occ(); v != 3 || o != 0 || i != 0 {
		t.Fatalf("occupancy awal: vacant=%d occupied=%d inactive=%d", v, o, i)
	}
	// kamar 303 Out of Order → unit Inactive
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/rooms/"+rooms["303"].LocationID.String()+"/status", map[string]any{"room_status": "out_of_order", "note": "AC rusak"})
	e.mustJSON(st, body, 200, nil)
	// check-in reservasi walk-in ke kamar 301 → unit Occupied
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	today := time.Now().In(jkt)
	var res resResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations", map[string]any{"property_id": prop.ID, "room_type_id": rt.ID, "room_location_id": rooms["301"].LocationID, "guest_name": "Tamu Occupancy",
		"check_in_date": today.Format("2006-01-02"), "check_out_date": today.AddDate(0, 0, 1).Format("2006-01-02"), "confirm": true})
	e.mustJSON(st, body, 201, &res)
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations/"+res.ID.String()+"/check_in", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	if v, o, i := occ(); v != 1 || o != 1 || i != 1 {
		t.Fatalf("P1-BLD-08 occupancy setelah check-in & out of order: vacant=%d occupied=%d inactive=%d", v, o, i)
	}
	// check-out → kamar Dirty → unit Vacant
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations/"+res.ID.String()+"/check_out", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	if v, o, i := occ(); v != 2 || o != 0 || i != 1 {
		t.Fatalf("P1-BLD-08 occupancy setelah check-out: vacant=%d occupied=%d inactive=%d", v, o, i)
	}
}

// P1-BLD-06 / P1-NFR-01: optimistic locking If-Match pada denah & marker.
func TestP1v21FloorPlanIfMatch(t *testing.T) {
	e := setup(t)
	bm := e.login("bm@demo.buildingvision.id")
	var fp struct {
		ID      uuid.UUID `json:"id"`
		Version int       `json:"version"`
	}
	st, body := e.do(bm, http.MethodPost, "/api/v1/floor-plans", map[string]any{"location_id": e.refs.FloorA12, "name": "Denah L12"})
	e.mustJSON(st, body, 201, &fp)
	patch := func(path string, version int, payload map[string]any) (int, []byte) {
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPatch, e.srv.URL+path, strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bm)
		req.Header.Set("If-Match", fmt.Sprintf(`"%d"`, version))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}
	if st, body := patch("/api/v1/floor-plans/"+fp.ID.String(), fp.Version+5, map[string]any{"name": "Basi"}); st != 409 || !strings.Contains(string(body), "STALE") {
		t.Fatalf("If-Match basi pada denah harus 409 STALE_VERSION, got %d %s", st, body)
	}
	st, body = patch("/api/v1/floor-plans/"+fp.ID.String(), fp.Version, map[string]any{"name": "Denah Lantai 12"})
	e.mustJSON(st, body, 200, &fp)
	var m struct {
		ID      uuid.UUID `json:"id"`
		Version int       `json:"version"`
	}
	st, body = e.do(bm, http.MethodPost, "/api/v1/floor-plans/"+fp.ID.String()+"/markers", map[string]any{"target_type": "location", "target_id": e.refs.MechRoomA12, "x_pct": 20, "y_pct": 30})
	e.mustJSON(st, body, 200, &m)
	if st, body := patch("/api/v1/floor-plans/"+fp.ID.String()+"/markers/"+m.ID.String(), m.Version+1, map[string]any{"x_pct": 25}); st != 409 {
		t.Fatalf("If-Match basi pada marker harus 409, got %d %s", st, body)
	}
	st, body = patch("/api/v1/floor-plans/"+fp.ID.String()+"/markers/"+m.ID.String(), m.Version, map[string]any{"x_pct": 25})
	e.mustJSON(st, body, 200, nil)
	// tanpa If-Match tetap diterima (kompatibel)
	st, body = e.do(bm, http.MethodPatch, "/api/v1/floor-plans/"+fp.ID.String(), map[string]any{"description": "Denah utama"})
	e.mustJSON(st, body, 200, nil)
}

// P1-BLD-09 / P1-DSH-09 / P1-RPT-05: scope Building/Tower di facility, overview, dan laporan.
func TestP1v21BuildingScope(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	pid := e.refs.PropertyID
	// facility di Tower A & Tower B
	var facA, facB struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(admin, http.MethodPost, "/api/v1/facilities", map[string]any{"property_id": pid, "name": "Lift A", "facility_type": "lift", "location_id": e.refs.LobbyA, "is_bookable": false})
	e.mustJSON(st, body, 201, &facA)
	st, body = e.do(admin, http.MethodPost, "/api/v1/facilities", map[string]any{"property_id": pid, "name": "Toilet B3", "facility_type": "toilet", "location_id": e.refs.ToiletB3, "is_bookable": false})
	e.mustJSON(st, body, 201, &facB)
	woA := e.createWO(admin, e.refs.MechRoomA12, "WO scope Tower A")
	woB := e.createWO(admin, e.refs.ToiletB3, "WO scope Tower B")
	_, _ = woA, woB
	// Building Manager ber-scope Tower A
	e.createUser(admin, "bm.towera@org-a.test", []map[string]any{{"role_id": e.roleID(admin, "building_manager"), "property_id": pid, "scope_location_id": e.refs.TowerA}}, nil)
	bmA := e.login("bm.towera@org-a.test")
	bmAll := e.login("bm@demo.buildingvision.id")

	// ---- facility ----
	for _, path := range []string{"/api/v1/facilities", "/api/v1/facilities?property_id=" + pid.String()} {
		st, body = e.do(bmA, http.MethodGet, path, nil)
		ids := listIDs(t, body)
		if st != 200 || !containsID(ids, facA.ID) || containsID(ids, facB.ID) {
			t.Fatalf("P1-BLD-09 list facility %s: %d A=%v B=%v", path, st, containsID(ids, facA.ID), containsID(ids, facB.ID))
		}
	}
	if st, _ := e.do(bmA, http.MethodGet, "/api/v1/facilities/"+facB.ID.String(), nil); st != 403 {
		t.Fatalf("P1-BLD-09 detail facility Tower B harus 403: %d", st)
	}
	if st, body := e.do(bmA, http.MethodPatch, "/api/v1/facilities/"+facA.ID.String(), map[string]any{"status": "under_maintenance"}); st != 200 {
		t.Fatalf("P1-BLD-09 update facility Tower A: %d %s", st, body)
	}
	if st, _ := e.do(bmA, http.MethodPost, "/api/v1/facilities", map[string]any{"property_id": pid, "name": "Koridor B", "facility_type": "corridor", "location_id": e.refs.FloorB3, "is_bookable": false}); st != 403 {
		t.Fatalf("P1-BLD-09 create facility di Tower B harus 403: %d", st)
	}

	// ---- overview ----
	type counter struct {
		Value int `json:"value"`
	}
	var todayA, todayAll struct {
		OpenWorkOrders counter `json:"open_work_orders"`
	}
	st, body = e.do(bmA, http.MethodGet, "/api/v1/overview/today?property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &todayA)
	st, body = e.do(bmAll, http.MethodGet, "/api/v1/overview/today?property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &todayAll)
	if todayA.OpenWorkOrders.Value != 1 || todayAll.OpenWorkOrders.Value != 2 {
		t.Fatalf("P1-DSH-09 open WO scoped=%d property=%d", todayA.OpenWorkOrders.Value, todayAll.OpenWorkOrders.Value)
	}
	var bs struct {
		Data []struct {
			LocationID uuid.UUID `json:"location_id"`
		} `json:"data"`
	}
	st, body = e.do(bmA, http.MethodGet, "/api/v1/overview/building-state", nil)
	e.mustJSON(st, body, 200, &bs)
	for _, b := range bs.Data {
		if b.LocationID == e.refs.TowerB {
			t.Fatalf("P1-DSH-09 building state tidak boleh memuat Tower B: %s", body)
		}
	}
	for _, path := range []string{"/api/v1/overview/attention-required", "/api/v1/overview/team-workload", "/api/v1/overview/tenant-requests", "/api/v1/overview/pm-due"} {
		if st, body := e.do(bmA, http.MethodGet, path+"?property_id="+pid.String(), nil); st != 200 {
			t.Fatalf("P1-DSH-09 %s: %d %s", path, st, body)
		}
	}

	// ---- reports ----
	var repA, repAll struct {
		Summary map[string]float64 `json:"summary"`
	}
	st, body = e.do(bmA, http.MethodGet, "/api/v1/reports/work-orders?property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &repA)
	st, body = e.do(bmAll, http.MethodGet, "/api/v1/reports/work-orders?property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &repAll)
	if repA.Summary["created"] != 1 || repAll.Summary["created"] != 2 {
		t.Fatalf("P1-RPT-05 report WO scoped=%v property=%v", repA.Summary["created"], repAll.Summary["created"])
	}
	for _, name := range []string{"operations-kpi", "tasks", "sla", "incidents", "backlog", "service-requests", "maintenance", "patrol-cleaning"} {
		if st, body := e.do(bmA, http.MethodGet, "/api/v1/reports/"+name+"?property_id="+pid.String(), nil); st != 200 {
			t.Fatalf("P1-RPT-05 laporan %s untuk user ber-scope: %d %s", name, st, body)
		}
	}
	// laporan tanpa lokasi object (billing) tetap property-wide: user ber-scope ditolak saat property eksplisit
	if st, _ := e.do(bmA, http.MethodGet, "/api/v1/reports/billing?property_id="+pid.String(), nil); st != 403 {
		t.Fatalf("laporan billing untuk user ber-scope harus 403: %d", st)
	}
}

// GAP-P1-09: tipe incident divalidasi (400 jelas, bukan error insert) — online & sync.
func TestP1v21IncidentTypeValidation(t *testing.T) {
	e := setup(t)
	officer := e.login("wawan@demo.buildingvision.id")
	var inc struct {
		IncidentType string `json:"incident_type"`
		Category     string `json:"category"`
	}
	st, body := e.do(officer, http.MethodPost, "/api/v1/incidents", map[string]any{"title": "Kebakaran kecil", "incident_type": "fire", "category": "fire_smoke", "location_id": e.refs.LobbyA})
	if st != 400 || !strings.Contains(string(body), "incident_type") {
		t.Fatalf("incident_type tidak valid harus 400 dengan field incident_type: %d %s", st, body)
	}
	// hanya kategori → tipe diturunkan
	st, body = e.do(officer, http.MethodPost, "/api/v1/incidents", map[string]any{"title": "Asap di pantry", "category": "fire_smoke", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, &inc)
	if inc.IncidentType != "safety" || inc.Category != "fire_smoke" {
		t.Fatalf("tipe dari kategori: %s", body)
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/incidents", map[string]any{"title": "Kaca lobby retak", "incident_type": "building", "category": "property_damage", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, &inc)
	if inc.IncidentType != "building" {
		t.Fatalf("tipe building: %s", body)
	}
	// sync: report_incident dengan tipe tidak valid → rejected VALIDATION_ERROR (bukan internal)
	spv := e.login("sec.spv@demo.buildingvision.id")
	var route struct {
		ID uuid.UUID `json:"id"`
	}
	var cp struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/checkpoints", map[string]any{"name": "CP Lobby", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, &cp)
	st, body = e.do(spv, http.MethodPost, "/api/v1/patrol-routes", map[string]any{"property_id": e.refs.PropertyID, "name": "Rute lobby", "checkpoints": []map[string]any{{"checkpoint_id": cp.ID, "sort_order": 1}}})
	e.mustJSON(st, body, 201, &route)
	var patrol workItem
	st, body = e.do(spv, http.MethodPost, "/api/v1/patrol-routes/"+route.ID.String()+"/patrol-tasks", map[string]any{"assignee_user_id": e.refs.Users["security_officer"]})
	e.mustJSON(st, body, 201, &patrol)
	var res struct {
		Results []struct {
			Status     string `json:"status"`
			ReasonCode string `json:"reason_code"`
			Detail     string `json:"detail"`
		} `json:"results"`
	}
	now := time.Now().UTC()
	st, body = e.do(officer, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-inc", "mutations": []map[string]any{
		mut(uuid.New(), "task", patrol.ID, "report_incident", 1, map[string]any{"title": "Orang mencurigakan", "incident_type": "medical", "category": "suspicious_activity", "location_id": e.refs.LobbyA}, now),
		mut(uuid.New(), "task", patrol.ID, "report_incident", 2, map[string]any{"title": "Orang mencurigakan", "incident_type": "security", "category": "suspicious_activity", "location_id": e.refs.LobbyA}, now),
	}})
	e.mustJSON(st, body, 200, &res)
	if len(res.Results) != 2 || res.Results[0].Status != "rejected" || res.Results[0].ReasonCode != "VALIDATION_ERROR" || strings.Contains(res.Results[0].Detail, "internal") {
		t.Fatalf("sync tipe tidak valid harus rejected VALIDATION_ERROR: %s", body)
	}
	if res.Results[1].Status != "applied" {
		t.Fatalf("sync tipe valid harus applied: %s", body)
	}
	// master data sync memuat tipe incident & tipe default per kategori
	var bundle struct {
		Master struct {
			IncidentTypes         []string          `json:"incident_types"`
			IncidentCategoryTypes map[string]string `json:"incident_category_types"`
		} `json:"master"`
	}
	st, body = e.do(officer, http.MethodGet, "/api/v1/sync/work-bundle?device_id=dev-inc", nil)
	e.mustJSON(st, body, 200, &bundle)
	if len(bundle.Master.IncidentTypes) != 3 || bundle.Master.IncidentCategoryTypes["fire_smoke"] != "safety" {
		t.Fatalf("master incident types: %+v", bundle.Master)
	}
}
