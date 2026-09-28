package app_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// PRD P2 v2.1 §8 Workforce (D-P2-05 shift per domain): shift, roster, clock-in/out & on-duty, serah terima, kompetensi,
// saran teknisi, kapasitas, laporan Team Performance. Lihat docs/p2-v21-readiness.md & ADR-017.

type rosterEntry struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	Attendance string    `json:"attendance"`
}

func TestP2v21ShiftAndOnDuty(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	officer := e.login("wawan@demo.buildingvision.id")
	hkSpv := e.login("hk.spv@demo.buildingvision.id")
	siti := e.login("siti@demo.buildingvision.id")
	bm := e.login("bm@demo.buildingvision.id")
	pid := e.refs.PropertyID
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(jkt)
	today := now.Format("2006-01-02")

	// officer kedua di team security (belum clock-in) — tidak boleh menerima emergency selama ada staf on-duty
	off2 := e.createUser(admin, "officer2@org-a.test", []map[string]any{{"role_id": e.roleID(admin, "security_officer"), "property_id": pid}}, nil)
	var team struct {
		Members []struct {
			UserID uuid.UUID `json:"user_id"`
			IsLead bool      `json:"is_lead"`
		} `json:"members"`
	}
	secTeam := e.refs.Teams["security"]
	st, body := e.do(admin, http.MethodGet, "/api/v1/teams/"+secTeam.String(), nil)
	e.mustJSON(st, body, 200, &team)
	members := []map[string]any{}
	for _, m := range team.Members {
		members = append(members, map[string]any{"user_id": m.UserID, "is_lead": m.IsLead})
	}
	members = append(members, map[string]any{"user_id": off2, "is_lead": false})
	st, body = e.do(admin, http.MethodPatch, "/api/v1/teams/"+secTeam.String(), map[string]any{"members": members})
	e.mustJSON(st, body, 200, nil)

	// ---- Shift definition per domain (P2-SHF-01) ----
	start := now.Add(-time.Hour)
	end := now.Add(7 * time.Hour)
	var cur struct {
		ID              uuid.UUID `json:"id"`
		CrossesMidnight bool      `json:"crosses_midnight"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/security/shifts", map[string]any{"property_id": pid, "code": "cur", "name": "Shift Berjalan", "start_time": start.Format("15:04"), "end_time": end.Format("15:04"), "min_staff": 2})
	e.mustJSON(st, body, 201, &cur)
	var night struct {
		CrossesMidnight bool `json:"crosses_midnight"`
		DurationMinutes int  `json:"duration_minutes"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/security/shifts", map[string]any{"property_id": pid, "code": "MLM", "name": "Malam", "start_time": "23:00", "end_time": "07:00"})
	e.mustJSON(st, body, 201, &night)
	if !night.CrossesMidnight || night.DurationMinutes != 480 {
		t.Fatalf("shift malam lintas hari: %s", body)
	}
	if st, _ := e.do(officer, http.MethodPost, "/api/v1/security/shifts", map[string]any{"property_id": pid, "code": "x", "name": "x", "start_time": "08:00", "end_time": "16:00"}); st != 403 {
		t.Fatalf("officer tidak boleh mengelola shift: %d", st)
	}
	if st, _ := e.do(officer, http.MethodGet, "/api/v1/security/shifts?property_id="+pid.String(), nil); st != 200 {
		t.Fatalf("officer boleh melihat shift security: %d", st)
	}
	if st, _ := e.do(siti, http.MethodGet, "/api/v1/security/shifts?property_id="+pid.String(), nil); st != 403 {
		t.Fatalf("staf housekeeping tidak boleh melihat shift security (per domain): %d", st)
	}
	var hkShift struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/housekeeping/shifts", map[string]any{"property_id": pid, "code": "cur", "name": "HK Berjalan", "start_time": start.Format("15:04"), "end_time": end.Format("15:04"), "min_staff": 1})
	e.mustJSON(st, body, 201, &hkShift)

	// ---- Roster (P2-SHF-02) ----
	var res struct {
		Created int `json:"created"`
		Skipped int `json:"skipped"`
	}
	assign := map[string]any{"shift_id": cur.ID, "dates": []string{today}, "user_ids": []uuid.UUID{e.refs.Users["security_officer"], off2}, "post": "Pos Lobby", "team_id": secTeam}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/security/roster", assign)
	e.mustJSON(st, body, 201, &res)
	if res.Created != 2 {
		t.Fatalf("roster: %s", body)
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/security/roster", assign)
	e.mustJSON(st, body, 201, &res)
	if res.Created != 0 || res.Skipped != 2 {
		t.Fatalf("roster ulang harus dilewati: %s", body)
	}
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/housekeeping/roster", map[string]any{"shift_id": hkShift.ID, "dates": []string{today}, "user_ids": []uuid.UUID{e.refs.Users["housekeeping_staff"]}})
	e.mustJSON(st, body, 201, &res)
	var my struct {
		Data []rosterEntry `json:"data"`
	}
	st, body = e.do(officer, http.MethodGet, "/api/v1/me/roster", nil)
	e.mustJSON(st, body, 200, &my)
	if len(my.Data) != 1 {
		t.Fatalf("roster saya: %s", body)
	}

	// ---- Clock-in & on-duty (P2-DTY-01..02) ----
	var att struct {
		ID                uuid.UUID  `json:"id"`
		ShiftAssignmentID *uuid.UUID `json:"shift_assignment_id"`
		Domain            string     `json:"domain"`
		Status            string     `json:"status"`
		LateMinutes       int        `json:"late_minutes"`
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/me/attendance/clock-in", map[string]any{"gps_lat": -6.2, "gps_lng": 106.8})
	e.mustJSON(st, body, 201, &att)
	if att.ShiftAssignmentID == nil || att.Domain != "security" || att.Status != "on_duty" || att.LateMinutes < 50 {
		t.Fatalf("clock-in harus terhubung roster berjalan (terlambat ±60 menit): %s", body)
	}
	if st, _ := e.do(officer, http.MethodPost, "/api/v1/me/attendance/clock-in", map[string]any{}); st != 409 {
		t.Fatalf("clock-in ganda harus 409: %d", st)
	}
	var board struct {
		Scheduled int           `json:"scheduled"`
		OnDuty    int           `json:"on_duty"`
		Absent    int           `json:"absent"`
		Shortage  int           `json:"shortage"`
		Staff     []rosterEntry `json:"staff"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/security/on-duty?property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &board)
	if board.Scheduled != 2 || board.OnDuty != 1 || board.Absent != 1 || board.Shortage != 1 {
		t.Fatalf("P2-DTY-02 papan on-duty: %s", body)
	}
	// Attention Required: kekurangan staf on-duty
	var attn struct {
		Data []struct {
			Category string `json:"category"`
			DeepLink string `json:"deep_link"`
		} `json:"data"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/overview/attention-required?limit=50&property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &attn)
	okShort := false
	for _, it := range attn.Data {
		if it.Category == "workforce_shortage" && strings.HasPrefix(it.DeepLink, "/security/shifts") {
			okShort = true
		}
	}
	if !okShort {
		t.Fatalf("attention workforce_shortage: %s", body)
	}
	// P2-DTY-03: emergency → staf on-duty (bukan seluruh team)
	st, body = e.do(e.login("budi@demo.buildingvision.id"), http.MethodPost, "/api/v1/emergency-alerts", map[string]any{"emergency_type": "medical", "location_id": e.refs.LobbyA, "channel": "panic_button"})
	e.mustJSON(st, body, 201, nil)
	e.dispatch(t)
	if ib := e.inboxOf(t, officer); !hasType(ib, "emergency_raised") {
		t.Fatalf("officer on-duty harus menerima emergency: %+v", ib.Data)
	}
	if ib := e.inboxOf(t, e.login("officer2@org-a.test")); hasType(ib, "emergency_raised") {
		t.Fatalf("officer yang belum clock-in tidak menerima emergency selama ada staf on-duty: %+v", ib.Data)
	}
	// kapasitas lintas domain (P2-WKL-03)
	var cap struct {
		Domains []struct {
			Domain          string `json:"domain"`
			OnDuty          int    `json:"on_duty"`
			CapacityMinutes int    `json:"capacity_minutes"`
			Status          string `json:"status"`
		} `json:"domains"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/overview/workforce-capacity?property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &cap)
	okCap := false
	for _, d := range cap.Domains {
		if d.Domain == "security" && d.OnDuty == 1 && d.CapacityMinutes > 300 {
			okCap = true
		}
	}
	if len(cap.Domains) != 3 || !okCap {
		t.Fatalf("workforce capacity: %s", body)
	}

	// ---- Serah terima shift (P2-SHF-03) ----
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/incidents", map[string]any{"title": "Pintu samping rusak", "category": "property_damage", "severity": "medium", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, nil)
	var draft struct {
		Counts map[string]int `json:"counts"`
	}
	st, body = e.do(officer, http.MethodGet, "/api/v1/security/handovers/draft?property_id="+pid.String(), nil)
	e.mustJSON(st, body, 200, &draft)
	if draft.Counts["active_incidents"] < 2 || draft.Counts["active_emergencies"] != 1 {
		t.Fatalf("draft serah terima: %s", body)
	}
	var hov struct {
		ID             uuid.UUID `json:"id"`
		HandoverNumber string    `json:"handover_number"`
		Status         string    `json:"status"`
		AllowedActions []string  `json:"allowed_actions"`
	}
	if st, _ := e.do(officer, http.MethodPost, "/api/v1/security/handovers", map[string]any{"property_id": pid}); st != 400 {
		t.Fatalf("serah terima tanpa catatan harus 400: %d", st)
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/security/handovers", map[string]any{"property_id": pid, "shift_id": cur.ID, "shift_date": today, "received_by": off2, "post": "Pos Lobby", "notes": "Emergency medis di lobby masih ditangani; pintu samping rusak menunggu teknisi."})
	e.mustJSON(st, body, 201, &hov)
	if !strings.HasPrefix(hov.HandoverNumber, "HOV-") || hov.Status != "submitted" || has(hov.AllowedActions, "acknowledge") {
		t.Fatalf("serah terima: %s", body)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, e.login("officer2@org-a.test")); !hasType(ib, "shift_handover") {
		t.Fatalf("penerima harus menerima notifikasi serah terima: %+v", ib.Data)
	}
	if st, _ := e.do(officer, http.MethodPost, "/api/v1/security/handovers/"+hov.ID.String()+"/acknowledge", nil); st != 403 {
		t.Fatalf("pembuat tidak boleh meng-acknowledge serah terima sendiri: %d", st)
	}
	st, body = e.do(e.login("officer2@org-a.test"), http.MethodPost, "/api/v1/security/handovers/"+hov.ID.String()+"/acknowledge", nil)
	e.mustJSON(st, body, 200, &hov)
	if hov.Status != "acknowledged" {
		t.Fatalf("acknowledge serah terima: %s", body)
	}
	// ---- Clock-out ----
	st, body = e.do(officer, http.MethodPost, "/api/v1/me/attendance/clock-out", map[string]any{"note": "Selesai"})
	e.mustJSON(st, body, 200, &att)
	if att.Status != "completed" {
		t.Fatalf("clock-out: %s", body)
	}
	var mine struct {
		Current *struct{}  `json:"current"`
		Recent  []struct{} `json:"recent"`
	}
	st, body = e.do(officer, http.MethodGet, "/api/v1/me/attendance", nil)
	e.mustJSON(st, body, 200, &mine)
	if mine.Current != nil || len(mine.Recent) != 1 {
		t.Fatalf("attendance saya: %s", body)
	}

	// ---- Clock-in/out offline via sync (P2-NFR-02) ----
	sessID := uuid.New()
	var syncRes struct {
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}
	st, body = e.do(siti, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-hk", "mutations": []map[string]any{
		mut(uuid.New(), "attendance", sessID, "clock_in", 1, map[string]any{"gps_status": "denied"}, time.Now().Add(-30*time.Minute)),
		mut(uuid.New(), "attendance", sessID, "clock_out", 2, map[string]any{}, time.Now().Add(-5*time.Minute)),
	}})
	e.mustJSON(st, body, 200, &syncRes)
	if len(syncRes.Results) != 2 || syncRes.Results[0].Status != "applied" || syncRes.Results[1].Status != "applied" {
		t.Fatalf("clock-in/out sync: %s", body)
	}
	var hkAtt struct {
		Data []struct {
			Status        string `json:"status"`
			ClockInSource string `json:"clock_in_source"`
		} `json:"data"`
	}
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/housekeeping/attendance?property_id="+pid.String()+"&date="+today, nil)
	e.mustJSON(st, body, 200, &hkAtt)
	if len(hkAtt.Data) != 1 || hkAtt.Data[0].Status != "completed" || hkAtt.Data[0].ClockInSource != "sync" {
		t.Fatalf("rekap kehadiran housekeeping: %s", body)
	}

	// ---- Salin roster mingguan & batal (P2-SHF-02) ----
	weekStart := now.AddDate(0, 0, -int(now.Weekday())).Format("2006-01-02")
	nextWeek := now.AddDate(0, 0, 7-int(now.Weekday())).Format("2006-01-02")
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/security/roster/copy-week", map[string]any{"property_id": pid, "from_week_start": weekStart, "to_week_start": nextWeek})
	e.mustJSON(st, body, 200, &res)
	if res.Created != 2 {
		t.Fatalf("salin minggu: %s", body)
	}
	var roster struct {
		Data []rosterEntry `json:"data"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/security/roster?property_id="+pid.String()+"&from="+today+"&to="+today, nil)
	e.mustJSON(st, body, 200, &roster)
	for _, r := range roster.Data {
		if r.UserID == e.refs.Users["security_officer"] {
			if st, _ := e.do(secSpv, http.MethodDelete, "/api/v1/security/roster/"+r.ID.String(), nil); st != 409 {
				t.Fatalf("roster dengan kehadiran tidak boleh dibatalkan: %d", st)
			}
		} else if st, _ := e.do(secSpv, http.MethodDelete, "/api/v1/security/roster/"+r.ID.String(), nil); st != 204 {
			t.Fatalf("batal roster: %d", st)
		}
	}
}

// P2-WKL-02..04: laporan Team Performance, SLA per team, team workload dengan SLA risk.
// (Kompetensi staf P2-TEC/P2-SPN dihapus dari scope atas keputusan user 2026-09-27.)
func TestP2v21TeamPerformance(t *testing.T) {
	e := setup(t)
	engSpv := e.login("eng.spv@demo.buildingvision.id")
	bm := e.login("bm@demo.buildingvision.id")
	st, body := e.do(engSpv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "AHU-07 bocor", "location_id": e.refs.MechRoomA12, "assignee_team_id": e.refs.Teams["engineering"]})
	e.mustJSON(st, body, 201, nil)
	// laporan Team Performance (P2-WKL-04) & SLA per team (P2-WKL-02)
	var rep struct {
		Breakdowns map[string][]struct {
			Label string `json:"label"`
		} `json:"breakdowns"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/reports/team-performance?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &rep)
	if _, ok := rep.Breakdowns["team"]; !ok {
		t.Fatalf("team performance: %s", body)
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/reports/sla?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &rep)
	if _, ok := rep.Breakdowns["team"]; !ok {
		t.Fatalf("SLA per team: %s", body)
	}
	var wl struct {
		Data []struct {
			SLARisk *int `json:"sla_risk"`
		} `json:"data"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/overview/team-workload?group=team", nil)
	e.mustJSON(st, body, 200, &wl)
	if len(wl.Data) == 0 || wl.Data[0].SLARisk == nil {
		t.Fatalf("team workload sla_risk: %s", body)
	}
}
