package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// PRD P2 v2.1 — umpan balik UI (web & Staff App): field opsional dapat dikosongkan (UUID nol / string kosong),
// If-Match pada shift & jadwal, filter findings (asset, periode), filter hasil inspeksi, today_run di daftar route,
// GET satu serah terima.
func TestP2v21UIFeedback(t *testing.T) {
	e := setup(t)
	hkSpv := e.login("hk.spv@demo.buildingvision.id")
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	engSpv := e.login("eng.spv@demo.buildingvision.id")
	engMgr := e.login("eng.manager@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	pid := e.refs.PropertyID
	now := time.Now().In(mustLoc())
	nilID := uuid.Nil.String()

	// ---- shift: If-Match & warna dapat dikosongkan ----
	var shift struct {
		ID      uuid.UUID `json:"id"`
		Color   *string   `json:"color"`
		Version int       `json:"version"`
	}
	st, body := e.do(hkSpv, http.MethodPost, "/api/v1/housekeeping/shifts", map[string]any{"property_id": pid, "code": "PG", "name": "Pagi", "start_time": now.Add(-10 * time.Minute).Format("15:04"), "end_time": now.Add(6 * time.Hour).Format("15:04"), "color": "teal"})
	e.mustJSON(st, body, 201, &shift)
	if st, _ := e.doIfMatch(hkSpv, http.MethodPatch, "/api/v1/housekeeping/shifts/"+shift.ID.String(), map[string]any{"name": "Pagi 1"}, shift.Version+2); st != 409 {
		t.Fatalf("If-Match basi pada shift harus 409: %d", st)
	}
	st, body = e.doIfMatch(hkSpv, http.MethodPatch, "/api/v1/housekeeping/shifts/"+shift.ID.String(), map[string]any{"color": ""}, shift.Version)
	e.mustJSON(st, body, 200, &shift)
	if shift.Color != nil {
		t.Fatalf("warna shift harus dapat dikosongkan: %s", body)
	}

	// ---- cleaning route: today_run di daftar, shift & team dapat dilepas ----
	var route struct {
		ID                uuid.UUID  `json:"id"`
		ShiftID           *uuid.UUID `json:"shift_id"`
		ResponsibleTeamID *uuid.UUID `json:"responsible_team_id"`
		Version           int        `json:"version"`
	}
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-routes", map[string]any{"name": "Route Uji", "shift_id": shift.ID, "responsible_team_id": e.refs.Teams["housekeeping"],
		"stops": []map[string]any{{"location_id": e.refs.LobbyA, "estimated_minutes": 60}, {"location_id": e.refs.ToiletA12, "estimated_minutes": 60}}})
	e.mustJSON(st, body, 201, &route)
	var routes struct {
		Data []struct {
			ID       uuid.UUID `json:"id"`
			TodayRun *struct {
				TotalStops int `json:"total_stops"`
			} `json:"today_run"`
		} `json:"data"`
	}
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/cleaning-routes", nil)
	e.mustJSON(st, body, 200, &routes)
	if len(routes.Data) != 1 || routes.Data[0].TodayRun == nil || routes.Data[0].TodayRun.TotalStops != 2 {
		t.Fatalf("daftar route harus memuat today_run: %s", body)
	}
	st, body = e.doIfMatch(hkSpv, http.MethodPatch, "/api/v1/cleaning-routes/"+route.ID.String(), map[string]any{"shift_id": nilID, "responsible_team_id": nilID}, route.Version)
	e.mustJSON(st, body, 200, &route)
	if route.ShiftID != nil || route.ResponsibleTeamID != nil {
		t.Fatalf("shift & team route harus dapat dilepas (UUID nol): %s", body)
	}

	// ---- cleaning schedule: shift dapat dilepas + If-Match ----
	var cs struct {
		ID      uuid.UUID  `json:"id"`
		ShiftID *uuid.UUID `json:"shift_id"`
		Version int        `json:"version"`
	}
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-schedules", map[string]any{"name": "Lobby", "location_id": e.refs.LobbyA, "shift_id": shift.ID, "requires_photo": false})
	e.mustJSON(st, body, 201, &cs)
	if st, _ := e.doIfMatch(hkSpv, http.MethodPatch, "/api/v1/cleaning-schedules/"+cs.ID.String(), map[string]any{"name": "Lobby 2"}, cs.Version+3); st != 409 {
		t.Fatalf("If-Match basi pada jadwal cleaning harus 409: %d", st)
	}
	st, body = e.doIfMatch(hkSpv, http.MethodPatch, "/api/v1/cleaning-schedules/"+cs.ID.String(), map[string]any{"shift_id": nilID}, cs.Version)
	e.mustJSON(st, body, 200, &cs)
	if cs.ShiftID != nil {
		t.Fatalf("shift jadwal cleaning harus dapat dilepas: %s", body)
	}

	budi := e.refs.Users["technician"]

	// ---- dokumen equipment: kedaluwarsa dapat dikosongkan ----
	var asset struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/assets", map[string]any{"name": "AHU-41", "equipment_id": ahuEquipment(t, e, engMgr), "location_id": e.refs.MechRoomA12, "criticality": "high"})
	e.mustJSON(st, body, 201, &asset)
	var doc assetDoc
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/assets/"+asset.ID.String()+"/documents", map[string]any{"document_type": "permit", "title": "Izin", "document_number": "IZ-1", "expires_on": now.AddDate(0, 0, 5).Format("2006-01-02")})
	e.mustJSON(st, body, 201, &doc)
	st, body = e.doIfMatch(engSpv, http.MethodPatch, "/api/v1/asset-documents/"+doc.ID.String(), map[string]any{"expires_on": "", "document_number": ""}, doc.Version)
	e.mustJSON(st, body, 200, &doc)
	if doc.Status != "no_expiry" {
		t.Fatalf("masa berlaku dokumen harus dapat dikosongkan: %s", body)
	}

	// ---- findings: filter asset & periode ----
	st, body = e.do(tech, http.MethodPost, "/api/v1/findings", map[string]any{"title": "Bocor", "asset_id": asset.ID, "severity": "medium"})
	e.mustJSON(st, body, 201, nil)
	var fl struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/findings?asset_id="+asset.ID.String(), nil)
	e.mustJSON(st, body, 200, &fl)
	if len(fl.Data) != 1 {
		t.Fatalf("filter findings asset_id: %s", body)
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/findings?asset_id="+asset.ID.String()+"&created_from="+now.AddDate(0, 0, 1).Format("2006-01-02"), nil)
	e.mustJSON(st, body, 200, &fl)
	if len(fl.Data) != 0 {
		t.Fatalf("filter findings created_from: %s", body)
	}

	// ---- task list: filter hasil inspeksi ----
	var ins workItem
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/inspections", map[string]any{"title": "Inspeksi AHU-41", "asset_id": asset.ID, "assignee_user_id": budi, "priority": "medium"})
	e.mustJSON(st, body, 201, &ins)
	st, body = e.do(tech, http.MethodPost, "/api/v1/tasks/"+ins.ID.String()+"/start", nil)
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tech, http.MethodPost, "/api/v1/tasks/"+ins.ID.String()+"/complete", map[string]any{"completion_notes": "Normal"})
	e.mustJSON(st, body, 200, nil)
	var tl struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/tasks?type=inspection&result=pass", nil)
	e.mustJSON(st, body, 200, &tl)
	found := false
	for _, x := range tl.Data {
		if x.ID == ins.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("filter result=pass harus memuat inspeksi lulus: %s", body)
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/tasks?type=inspection&result=fail", nil)
	e.mustJSON(st, body, 200, &tl)
	for _, x := range tl.Data {
		if x.ID == ins.ID {
			t.Fatalf("filter result=fail tidak boleh memuat inspeksi lulus: %s", body)
		}
	}
	if st, _ := e.do(engSpv, http.MethodGet, "/api/v1/tasks?result=gagal", nil); st != 400 {
		t.Fatalf("result tidak valid harus 400: %d", st)
	}

	// ---- staf domain (pilihan roster/serah terima tanpa iam.users.view) ----
	var staff struct {
		Data []struct {
			UserID    uuid.UUID `json:"user_id"`
			TeamNames []string  `json:"team_names"`
		} `json:"data"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/security/staff?property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &staff)
	foundOfficer := false
	for _, x := range staff.Data {
		if x.UserID == e.refs.Users["security_officer"] && len(x.TeamNames) > 0 {
			foundOfficer = true
		}
	}
	if !foundOfficer {
		t.Fatalf("staf domain security harus memuat officer anggota team security: %s", body)
	}
	if st, _ := e.do(tech, http.MethodGet, "/api/v1/security/staff", nil); st != 403 {
		t.Fatalf("teknisi tidak boleh melihat staf security: %d", st)
	}

	// ---- serah terima: GET satu data ----
	var ho struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/security/handovers", map[string]any{"property_id": pid, "notes": "Kunci panel di pos"})
	e.mustJSON(st, body, 201, &ho)
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/security/handovers/"+ho.ID.String(), nil)
	e.mustJSON(st, body, 200, nil)
	if st, _ := e.do(hkSpv, http.MethodGet, "/api/v1/housekeeping/handovers/"+ho.ID.String(), nil); st != 404 {
		t.Fatalf("serah terima domain lain harus 404: %d", st)
	}
}
