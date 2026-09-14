package app_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// AT-005 Patrol, AT-006 Patrol Finding (WF-002)
func TestSecurityPatrol(t *testing.T) {
	e := setup(t)
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	officer := e.login("wawan@demo.buildingvision.id")
	officerID := e.refs.Users["security_officer"]

	// checkpoints
	var cp1, cp2 struct {
		ID     uuid.UUID `json:"id"`
		QRCode *string   `json:"qr_code"`
	}
	st, body := e.do(secSpv, http.MethodPost, "/api/v1/checkpoints", map[string]any{"name": "CP-01 Lobby", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, &cp1)
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/checkpoints", map[string]any{"name": "CP-02 Parkir LG", "location_id": e.refs.ParkingLG})
	e.mustJSON(st, body, 201, &cp2)
	if cp1.QRCode == nil || cp2.QRCode == nil {
		t.Fatal("checkpoint QR harus dibuat")
	}
	// route dengan urutan checkpoint (PRD §13.2)
	var route struct {
		ID          uuid.UUID `json:"id"`
		Checkpoints []struct {
			ID        uuid.UUID `json:"id"`
			SortOrder int       `json:"sort_order"`
		} `json:"checkpoints"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/patrol-routes", map[string]any{"property_id": e.refs.PropertyID, "name": "Rute A Malam",
		"checkpoints": []map[string]any{{"checkpoint_id": cp1.ID, "sort_order": 1}, {"checkpoint_id": cp2.ID, "sort_order": 2}}})
	e.mustJSON(st, body, 201, &route)
	if len(route.Checkpoints) != 2 || route.Checkpoints[0].ID != cp1.ID {
		t.Fatalf("route checkpoints: %+v", route.Checkpoints)
	}
	// adhoc patrol task assigned ke officer
	var pt workItem
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/patrol-routes/"+route.ID.String()+"/patrol-tasks", map[string]any{"assignee_user_id": officerID})
	e.mustJSON(st, body, 201, &pt)
	if pt.Status != "assigned" || !strings.HasPrefix(pt.Number, "TSK-") {
		t.Fatalf("patrol task: %+v", pt)
	}
	// scan sebelum start → guard
	st, _ = e.do(officer, http.MethodPost, "/api/v1/patrol-tasks/"+pt.ID.String()+"/scans", map[string]any{"qr_code": *cp1.QRCode})
	if st != 409 {
		t.Fatalf("scan sebelum start harus 409, got %d", st)
	}
	// Start Patrol
	st, body = e.do(officer, http.MethodPost, "/api/v1/tasks/"+pt.ID.String()+"/start", map[string]any{"gps_status": "captured", "gps_lat": -6.2, "gps_lng": 106.8})
	e.mustJSON(st, body, 200, &pt)
	// scan CP-01 via QR, CP-02 via QR URL penuh; idempotent client_scan_id
	var scans struct {
		Data []struct {
			CheckpointID uuid.UUID `json:"checkpoint_id"`
			Status       string    `json:"status"`
		} `json:"data"`
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/patrol-tasks/"+pt.ID.String()+"/scans", map[string]any{"qr_code": *cp1.QRCode, "client_scan_id": "s1", "gps_status": "captured", "gps_lat": -6.2, "gps_lng": 106.8})
	e.mustJSON(st, body, 200, &scans)
	st, body = e.do(officer, http.MethodPost, "/api/v1/patrol-tasks/"+pt.ID.String()+"/scans", map[string]any{"qr_code": *cp1.QRCode, "client_scan_id": "s1"})
	e.mustJSON(st, body, 200, &scans)
	// complete dengan 1 checkpoint pending → policy auto: missed (TD-007) — tapi kita scan dulu agar AT-005 semua scanned
	st, body = e.do(officer, http.MethodPost, "/api/v1/patrol-tasks/"+pt.ID.String()+"/scans", map[string]any{"qr_code": "https://bv.link/q/" + *cp2.QRCode})
	e.mustJSON(st, body, 200, &scans)
	scanned := 0
	for _, s := range scans.Data {
		if s.Status == "scanned" {
			scanned++
		}
	}
	if scanned != 2 {
		t.Fatalf("AT-005: checkpoint scanned %d, want 2", scanned)
	}
	// QR org lain / bukan checkpoint route → ditolak
	st, _ = e.do(officer, http.MethodPost, "/api/v1/patrol-tasks/"+pt.ID.String()+"/scans", map[string]any{"qr_code": "ZZZZZZZZZZZZ"})
	if st != 404 {
		t.Fatalf("QR tidak dikenal harus 404, got %d", st)
	}

	// AT-006: Finding dengan foto saat patrol
	var pre struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
		StorageKey   string    `json:"storage_key"`
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "task", "object_id": pt.ID, "attachment_type": "photo", "content_type": "image/jpeg", "size_bytes": 50})
	e.mustJSON(st, body, 201, &pre)
	_ = e.store.Put(context.Background(), pre.StorageKey, "image/jpeg", bytes.NewReader(make([]byte, 50)), 50)
	st, body = e.do(officer, http.MethodPost, "/api/v1/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{"gps_status": "captured", "gps_lat": -6.2, "gps_lng": 106.8})
	e.mustJSON(st, body, 200, nil)
	var finding struct {
		ID              uuid.UUID `json:"id"`
		FindingNumber   string    `json:"finding_number"`
		FindingType     string    `json:"finding_type"`
		AttachmentCount int       `json:"attachment_count"`
		AllowedActions  []string  `json:"allowed_actions"`
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/findings", map[string]any{"title": "Pintu darurat terganjal", "severity": "high", "finding_type": "patrol", "source_type": "task", "source_id": pt.ID, "location_id": e.refs.ParkingLG, "attachment_id": pre.AttachmentID})
	e.mustJSON(st, body, 201, &finding)
	if finding.AttachmentCount != 1 || finding.FindingType != "patrol" {
		t.Fatalf("AT-006: finding %+v", finding)
	}
	if !has(e.jobs.EventTypes(), "finding.created") {
		t.Fatal("AT-006: event finding.created (notifikasi supervisor)")
	}
	// Finding → Incident (WF-002) oleh supervisor
	var inc struct {
		IncidentNumber string `json:"incident_number"`
		Status         string `json:"status"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/findings/"+finding.ID.String()+"/incidents", map[string]any{"category": "safety", "severity": "high"})
	e.mustJSON(st, body, 201, &inc)
	if !strings.HasPrefix(inc.IncidentNumber, "INC-") {
		t.Fatalf("incident dari finding: %+v", inc)
	}
	// Complete Patrol
	st, body = e.do(officer, http.MethodPost, "/api/v1/tasks/"+pt.ID.String()+"/complete", nil)
	e.mustJSON(st, body, 200, &pt)
	if pt.Status != "completed" {
		t.Fatalf("AT-005: patrol complete: %s", pt.Status)
	}
	// Patrol schedule harian → generate 7 hari
	var sch struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/patrol-schedules", map[string]any{"route_id": route.ID, "name": "Patrol Malam", "start_time": "23:00", "duration_minutes": 60, "responsible_team_id": e.refs.Teams["security"]})
	e.mustJSON(st, body, 201, &sch)
	var list struct {
		Data []workItem `json:"data"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/patrol-tasks?source_type=patrol_schedule&source_id="+sch.ID.String()+"&limit=50", nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) < 6 {
		t.Fatalf("patrol schedule harus generate ~7 patrol task, got %d", len(list.Data))
	}
	// generate ulang idempotent
	var gen struct {
		Generated int `json:"generated"`
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/patrol-schedules/generate", nil)
	e.mustJSON(st, body, 200, &gen)
	if gen.Generated != 0 {
		t.Fatalf("patrol generator harus idempotent, got %d", gen.Generated)
	}
	// TD-007 auto missed: patrol kedua, scan 1 dari 2 lalu complete → 1 missed
	var pt2 workItem
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/patrol-routes/"+route.ID.String()+"/patrol-tasks", map[string]any{"assignee_user_id": officerID})
	e.mustJSON(st, body, 201, &pt2)
	st, body = e.do(officer, http.MethodPost, "/api/v1/tasks/"+pt2.ID.String()+"/start", nil)
	e.mustJSON(st, body, 200, &pt2)
	st, body = e.do(officer, http.MethodPost, "/api/v1/patrol-tasks/"+pt2.ID.String()+"/scans", map[string]any{"checkpoint_id": cp1.ID, "scan_method": "manual"})
	e.mustJSON(st, body, 200, &scans)
	st, body = e.do(officer, http.MethodPost, "/api/v1/tasks/"+pt2.ID.String()+"/complete", nil)
	e.mustJSON(st, body, 200, &pt2)
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/patrol-tasks/"+pt2.ID.String()+"/scans", nil)
	e.mustJSON(st, body, 200, &scans)
	missed := 0
	for _, s := range scans.Data {
		if s.Status == "missed" {
			missed++
		}
	}
	if missed != 1 || !has(e.jobs.EventTypes(), "patrol_task.checkpoint_missed") {
		t.Fatalf("TD-007 auto missed: %d, events %v", missed, e.jobs.EventTypes())
	}
}

// AT-007 Cleaning Inspection (WF-003)
func TestHousekeepingCleaningInspection(t *testing.T) {
	e := setup(t)
	hkMgr := e.login("hk.manager@demo.buildingvision.id")
	hkSpv := e.login("hk.spv@demo.buildingvision.id")
	staff := e.login("siti@demo.buildingvision.id")
	staffID := e.refs.Users["housekeeping_staff"]

	// checklist cleaning (manager)
	var tpl struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(hkMgr, http.MethodPost, "/api/v1/checklist-templates", map[string]any{"name": "Cleaning Toilet", "domain": "housekeeping", "applies_to": []string{"cleaning", "inspection"},
		"items": []map[string]any{{"label": "Lantai bersih", "item_type": "ok_notok_na"}, {"label": "Tidak ada bau", "item_type": "yes_no"}}})
	e.mustJSON(st, body, 201, &tpl)
	st, body = e.do(hkMgr, http.MethodPost, "/api/v1/checklist-templates/"+tpl.ID.String()+"/publish", nil)
	e.mustJSON(st, body, 200, nil)

	// cleaning schedule → task hari ini/besok
	var sch struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-schedules", map[string]any{"name": "Toilet Lantai 12", "location_id": e.refs.ToiletA12, "cleaning_type": "routine", "start_time": "23:30", "duration_minutes": 30, "checklist_template_id": tpl.ID, "default_assignee_user_id": staffID, "requires_photo": true})
	e.mustJSON(st, body, 201, &sch)
	var list struct {
		Data []workItem `json:"data"`
	}
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/cleaning-tasks?source_id="+sch.ID.String()+"&sort=due_at&limit=50", nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) < 6 {
		t.Fatalf("cleaning schedule harus generate ~7 task, got %d", len(list.Data))
	}
	ct := list.Data[0]
	if !strings.HasPrefix(ct.Number, "TSK-") || ct.Status != "assigned" {
		t.Fatalf("cleaning task: %+v", ct)
	}
	// Staff: Start Cleaning → Checklist → Photo → Complete
	st, body = e.do(staff, http.MethodPost, "/api/v1/tasks/"+ct.ID.String()+"/start", nil)
	e.mustJSON(st, body, 200, &ct)
	var runs struct {
		Data []struct {
			Items []struct {
				ID       uuid.UUID `json:"id"`
				ItemType string    `json:"item_type"`
			} `json:"items"`
		} `json:"data"`
	}
	st, body = e.do(staff, http.MethodGet, "/api/v1/tasks/"+ct.ID.String()+"/checklist-runs", nil)
	e.mustJSON(st, body, 200, &runs)
	if len(runs.Data) != 1 || len(runs.Data[0].Items) != 2 {
		t.Fatalf("checklist run cleaning: %+v", runs.Data)
	}
	for _, it := range runs.Data[0].Items {
		ans := map[string]any{"result_value": "ok"}
		if it.ItemType == "yes_no" {
			ans = map[string]any{"result_value": "yes"}
		}
		st, body = e.do(staff, http.MethodPost, "/api/v1/checklist-run-items/"+it.ID.String()+"/answer", ans)
		e.mustJSON(st, body, 200, nil)
	}
	st, body = e.do(staff, http.MethodPost, "/api/v1/tasks/"+ct.ID.String()+"/complete", nil)
	if st != 409 {
		t.Fatalf("cleaning complete tanpa foto (requires_photo) harus 409: %d %s", st, body)
	}
	var pre struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
		StorageKey   string    `json:"storage_key"`
	}
	st, body = e.do(staff, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "task", "object_id": ct.ID, "attachment_type": "photo", "content_type": "image/jpeg", "size_bytes": 50})
	e.mustJSON(st, body, 201, &pre)
	_ = e.store.Put(context.Background(), pre.StorageKey, "image/jpeg", bytes.NewReader(make([]byte, 50)), 50)
	st, body = e.do(staff, http.MethodPost, "/api/v1/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(staff, http.MethodPost, "/api/v1/tasks/"+ct.ID.String()+"/complete", nil)
	e.mustJSON(st, body, 200, &ct)
	if ct.Status != "completed" {
		t.Fatalf("WF-003 cleaning complete: %s", ct.Status)
	}

	// AT-007: Inspector (supervisor) buka inspection → checklist Not OK → Finding otomatis → rework task
	var insp workItem
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/housekeeping-inspections", map[string]any{"cleaning_task_id": ct.ID, "checklist_template_id": tpl.ID})
	e.mustJSON(st, body, 201, &insp)
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/tasks/"+insp.ID.String()+"/start", nil)
	e.mustJSON(st, body, 200, &insp)
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/tasks/"+insp.ID.String()+"/checklist-runs", nil)
	e.mustJSON(st, body, 200, &runs)
	var run struct {
		Items []struct {
			ID        uuid.UUID  `json:"id"`
			FindingID *uuid.UUID `json:"finding_id"`
		} `json:"items"`
	}
	for i, it := range runs.Data[0].Items {
		ans := map[string]any{"result_value": "ok"}
		if i == 0 {
			ans = map[string]any{"result_value": "not_ok", "note": "Masih ada noda di lantai", "finding_severity": "medium"}
		}
		if it.ItemType == "yes_no" {
			ans = map[string]any{"result_value": "yes"}
		}
		st, body = e.do(hkSpv, http.MethodPost, "/api/v1/checklist-run-items/"+it.ID.String()+"/answer", ans)
		e.mustJSON(st, body, 200, &run)
	}
	var findingID *uuid.UUID
	for _, it := range run.Items {
		if it.FindingID != nil {
			findingID = it.FindingID
		}
	}
	if findingID == nil {
		t.Fatal("AT-007: Not OK harus membuat Finding")
	}
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/tasks/"+insp.ID.String()+"/complete", nil)
	e.mustJSON(st, body, 200, &insp)
	if insp.Extension == nil || insp.Extension["result"] != "fail" && insp.Extension["result"] != "partial" {
		t.Fatalf("AT-007: inspection result harus fail/partial: %+v", insp.Extension)
	}
	// cleaning task inspection_status rework_required
	var ct2 workItem
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/cleaning-tasks/"+ct.ID.String(), nil)
	e.mustJSON(st, body, 200, &ct2)
	if ct2.Extension["inspection_status"] != "rework_required" {
		t.Fatalf("cleaning inspection_status: %v", ct2.Extension["inspection_status"])
	}
	// Rework task dari finding (PRD §15.2)
	var rework workItem
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/findings/"+findingID.String()+"/tasks", map[string]any{"assignee_user_id": staffID})
	e.mustJSON(st, body, 201, &rework)
	if rework.Type != "cleaning" || !strings.HasPrefix(rework.Title, "Rework") {
		t.Fatalf("rework task: %+v", rework)
	}
}
