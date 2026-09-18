package app_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

type pushResult struct {
	Results []struct {
		ClientMutationID uuid.UUID       `json:"client_mutation_id"`
		Status           string          `json:"status"`
		ReasonCode       string          `json:"reason_code"`
		Detail           string          `json:"detail"`
		Response         json.RawMessage `json:"response"`
	} `json:"results"`
}

func mut(id uuid.UUID, ot string, oid uuid.UUID, action string, seq int64, payload map[string]any, clientTime time.Time) map[string]any {
	return map[string]any{"client_mutation_id": id, "object_type": ot, "object_id": oid, "action": action, "seq": seq, "payload": payload, "client_time": clientTime}
}

// AT-009 Offline: work bundle → mutation queue → auto sync; aturan konflik OD-004 C1–C10.
func TestOfflineSync(t *testing.T) {
	e := setup(t)
	spv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	budiID := e.refs.Users["technician"]
	jokoID := e.refs.Users["technician_2"]
	now := time.Now()

	// dua WO hari ini untuk budi (requires_evidence=false agar complete tanpa foto)
	var wo1, wo2 workItem
	st, body := e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "repair", "title": "Lampu koridor mati", "location_id": e.refs.MechRoomA12, "assignee_user_id": budiID, "requires_evidence": false, "scheduled_start_at": now, "due_at": now.Add(4 * time.Hour)})
	e.mustJSON(st, body, 201, &wo1)
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "repair", "title": "Pintu toilet rusak", "location_id": e.refs.ToiletA12, "assignee_user_id": budiID, "requires_evidence": false, "scheduled_start_at": now, "due_at": now.Add(4 * time.Hour)})
	e.mustJSON(st, body, 201, &wo2)

	// 1) Pull work bundle (cache today's assigned)
	var bundle struct {
		WorkOrders []workItem               `json:"work_orders"`
		Locations  []struct{ ID uuid.UUID } `json:"locations"`
		Cursor     string                   `json:"cursor"`
		Me         struct {
			UserID uuid.UUID `json:"user_id"`
		} `json:"me"`
		Removed []any `json:"removed"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/sync/work-bundle?device_id=dev-1", nil)
	e.mustJSON(st, body, 200, &bundle)
	if len(bundle.WorkOrders) != 2 || len(bundle.Locations) != 2 || bundle.Me.UserID != budiID {
		t.Fatalf("AT-009 bundle: wo=%d loc=%d", len(bundle.WorkOrders), len(bundle.Locations))
	}

	// 2) Offline: start → comment → checklist? → complete pada wo1; push berurutan (C1 applied)
	m1, m2, m3, m4 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	var res pushResult
	st, body = e.do(tech, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-1", "mutations": []map[string]any{
		mut(m1, "work_order", wo1.ID, "start", 1, map[string]any{"gps_status": "captured", "gps_lat": -6.2, "gps_lng": 106.8}, now.Add(-30*time.Minute)),
		mut(m2, "work_order", wo1.ID, "add_comment", 2, map[string]any{"body": "Lampu diganti (offline)"}, now.Add(-20*time.Minute)),
		mut(m3, "work_order", wo1.ID, "attach_photo", 3, map[string]any{"client_attachment_id": "photo-1", "attachment_type": "photo_after", "content_type": "image/jpeg", "size_bytes": 1000, "gps_status": "denied"}, now.Add(-15*time.Minute)),
		mut(m4, "work_order", wo1.ID, "complete", 4, map[string]any{"completion_notes": "Selesai offline", "resolution": "Ganti lampu"}, now.Add(-10*time.Minute)),
	}})
	e.mustJSON(st, body, 200, &res)
	for i, r := range res.Results {
		if r.Status != "applied" {
			t.Fatalf("C1: mutation %d harus applied, got %s %s %s", i, r.Status, r.ReasonCode, r.Detail)
		}
	}
	var photoResp struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
		UploadURL    string    `json:"upload_url"`
	}
	_ = json.Unmarshal(res.Results[2].Response, &photoResp)
	if photoResp.UploadURL == "" {
		t.Fatal("attach_photo harus mengembalikan upload_url")
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+wo1.ID.String(), nil)
	e.mustJSON(st, body, 200, &wo1)
	if wo1.Status != "completed" {
		t.Fatalf("AT-009: WO harus completed setelah sync, got %s", wo1.Status)
	}
	// activity source=sync tercatat
	var acts struct {
		Data []struct {
			Action string `json:"action"`
			Source string `json:"source"`
		} `json:"data"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+wo1.ID.String()+"/activities", nil)
	e.mustJSON(st, body, 200, &acts)
	syncSeen := false
	for _, a := range acts.Data {
		if a.Source == "sync" && a.Action == "status_changed" {
			syncSeen = true
		}
	}
	if !syncSeen {
		t.Fatal("activity harus bertanda source=sync")
	}

	// C6 duplicate: kirim ulang m1..m4 → duplicate, state tidak berubah
	st, body = e.do(tech, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-1", "mutations": []map[string]any{
		mut(m1, "work_order", wo1.ID, "start", 1, nil, now), mut(m4, "work_order", wo1.ID, "complete", 4, nil, now),
	}})
	e.mustJSON(st, body, 200, &res)
	if res.Results[0].Status != "duplicate" || res.Results[1].Status != "duplicate" {
		t.Fatalf("C6: harus duplicate, got %+v", res.Results)
	}

	// C4 REASSIGNED: supervisor reassign wo2 ke joko saat budi offline; budi push start → conflict REASSIGNED, evidence (comment) tetap tersimpan
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders/"+wo2.ID.String()+"/assign", map[string]any{"assignee_user_id": jokoID})
	e.mustJSON(st, body, 200, &wo2)
	m5, m6 := uuid.New(), uuid.New()
	st, body = e.do(tech, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-1", "mutations": []map[string]any{
		mut(m5, "work_order", wo2.ID, "start", 1, nil, now),
		mut(m6, "work_order", wo2.ID, "add_comment", 2, map[string]any{"body": "Catatan dari budi saat offline"}, now),
	}})
	e.mustJSON(st, body, 200, &res)
	if res.Results[0].Status != "conflict" || res.Results[0].ReasonCode != "REASSIGNED" {
		t.Fatalf("C4: harus conflict REASSIGNED, got %+v", res.Results[0])
	}
	// komentar (evidence) tetap tersimpan (applied — komentar tidak butuh assignee)
	var comments struct {
		Data []struct {
			Body   string `json:"body"`
			Source string `json:"source"`
		} `json:"data"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+wo2.ID.String()+"/comments", nil)
	e.mustJSON(st, body, 200, &comments)
	if len(comments.Data) != 1 || comments.Data[0].Source != "sync" {
		t.Fatalf("evidence komentar harus tersimpan dari sync: %+v", comments.Data)
	}
	if !has(e.jobs.EventTypes(), "sync.conflict") {
		t.Fatal("event sync.conflict untuk notifikasi supervisor")
	}
	// konflik muncul di daftar supervisor + Attention Required; acknowledge
	var conflicts struct {
		Data []struct {
			ClientMutationID uuid.UUID `json:"client_mutation_id"`
			ReasonCode       string    `json:"reason_code"`
			WorkerName       string    `json:"worker_name"`
		} `json:"data"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/sync/conflicts", nil)
	e.mustJSON(st, body, 200, &conflicts)
	if len(conflicts.Data) != 1 || conflicts.Data[0].ReasonCode != "REASSIGNED" {
		t.Fatalf("conflicts list: %+v", conflicts.Data)
	}
	var att struct {
		Data []struct {
			Category string `json:"category"`
		} `json:"data"`
		Total int `json:"total"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/overview/attention-required?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &att)
	foundConflict := false
	for _, a := range att.Data {
		if a.Category == "sync_conflict" {
			foundConflict = true
		}
	}
	if !foundConflict {
		t.Fatalf("Attention Required harus memuat sync_conflict: %+v", att.Data)
	}
	st, _ = e.do(spv, http.MethodPost, "/api/v1/sync/conflicts/"+conflicts.Data[0].ClientMutationID.String()+"/acknowledge", nil)
	if st != 204 {
		t.Fatalf("acknowledge: %d", st)
	}

	// C3 OBJECT_TERMINAL: supervisor close wo1; budi push add_comment lama → conflict OBJECT_TERMINAL, late_evidence tercatat
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders/"+wo1.ID.String()+"/close", nil)
	e.mustJSON(st, body, 200, &wo1)
	m7 := uuid.New()
	st, body = e.do(tech, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-1", "mutations": []map[string]any{
		mut(m7, "work_order", wo1.ID, "complete", 5, map[string]any{"completion_notes": "terlambat"}, now),
	}})
	e.mustJSON(st, body, 200, &res)
	if res.Results[0].Status != "conflict" || res.Results[0].ReasonCode != "OBJECT_TERMINAL" {
		t.Fatalf("C3: harus conflict OBJECT_TERMINAL, got %+v", res.Results[0])
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+wo1.ID.String()+"/activities", nil)
	e.mustJSON(st, body, 200, &acts)
	late := false
	for _, a := range acts.Data {
		if a.Action == "late_evidence" {
			late = true
		}
	}
	if !late {
		t.Fatal("C3: activity late_evidence harus tercatat")
	}

	// C9 SEQ_GAP: seq tidak monoton → rejected, mutation berikutnya untuk object sama tidak diproses
	var wo3 workItem
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "repair", "title": "Seq test", "location_id": e.refs.LobbyA, "assignee_user_id": budiID, "requires_evidence": false})
	e.mustJSON(st, body, 201, &wo3)
	m8, m9 := uuid.New(), uuid.New()
	st, body = e.do(tech, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-1", "mutations": []map[string]any{
		mut(m8, "work_order", wo3.ID, "start", 2, nil, now),
		mut(m9, "work_order", wo3.ID, "start", 1, nil, now),
	}})
	e.mustJSON(st, body, 200, &res)
	if res.Results[0].Status != "applied" || res.Results[1].Status != "rejected" || res.Results[1].ReasonCode != "SEQ_GAP" {
		t.Fatalf("C9: %+v", res.Results)
	}
	// C10 rejected: object tidak dikenal
	st, body = e.do(tech, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-1", "mutations": []map[string]any{
		mut(uuid.New(), "work_order", uuid.New(), "start", 1, nil, now),
	}})
	e.mustJSON(st, body, 200, &res)
	if res.Results[0].Status != "rejected" || res.Results[0].ReasonCode != "NOT_FOUND" {
		t.Fatalf("C10: %+v", res.Results[0])
	}
	// C8 clock skew: client_time 1 jam lalu → tetap applied, activity clock_skew
	m10 := uuid.New()
	st, body = e.do(tech, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-1", "mutations": []map[string]any{
		mut(m10, "work_order", wo3.ID, "add_comment", 3, map[string]any{"body": "jam device melenceng"}, now.Add(-2*time.Hour)),
	}})
	e.mustJSON(st, body, 200, &res)
	if res.Results[0].Status != "applied" {
		t.Fatalf("C8: %+v", res.Results[0])
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+wo3.ID.String()+"/activities", nil)
	e.mustJSON(st, body, 200, &acts)
	skew := false
	for _, a := range acts.Data {
		if a.Action == "clock_skew" {
			skew = true
		}
	}
	if !skew {
		t.Fatal("C8: activity clock_skew")
	}
	// bundle berikutnya: wo1 closed → removed
	st, body = e.do(tech, http.MethodGet, "/api/v1/sync/work-bundle?device_id=dev-1", nil)
	e.mustJSON(st, body, 200, &bundle)
	if len(bundle.Removed) == 0 {
		t.Fatal("bundle.removed harus memuat WO yang sudah closed/reassigned")
	}

	// Bundle harus memuat: WO ad-hoc tanpa tanggal (assigned) dan WO in_progress yang jadwalnya bukan hari ini.
	var woUndated, woFuture workItem
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "repair", "title": "AC bocor (ad-hoc)", "location_id": e.refs.MechRoomA12, "assignee_user_id": budiID, "requires_evidence": false})
	e.mustJSON(st, body, 201, &woUndated)
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "repair", "title": "Pompa (mulai kemarin, tempo minggu depan)", "location_id": e.refs.MechRoomA12, "assignee_user_id": budiID, "requires_evidence": false, "scheduled_start_at": now.Add(-48 * time.Hour), "due_at": now.Add(7 * 24 * time.Hour)})
	e.mustJSON(st, body, 201, &woFuture)
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+woFuture.ID.String()+"/start", map[string]any{"gps_status": "denied"})
	e.mustJSON(st, body, 200, &woFuture)
	st, body = e.do(tech, http.MethodGet, "/api/v1/sync/work-bundle?device_id=dev-1", nil)
	e.mustJSON(st, body, 200, &bundle)
	inBundle := func(id uuid.UUID) bool {
		for _, w := range bundle.WorkOrders {
			if w.ID == id {
				return true
			}
		}
		return false
	}
	if !inBundle(woUndated.ID) {
		t.Fatal("bundle harus memuat WO assigned tanpa tanggal")
	}
	if !inBundle(woFuture.ID) {
		t.Fatal("bundle harus memuat WO in_progress walau jadwal bukan hari ini")
	}

	// Checklist item photo_required: mobile mengirim attach_photo (seq n) + checklist_item_result dengan
	// client_attachment_id (seq n+1) → server menautkan attachment_id sehingga complete tidak diblokir.
	engMgr := e.login("eng.manager@demo.buildingvision.id")
	var tpl struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/checklist-templates", map[string]any{
		"name": "Foto panel", "applies_to": []string{"work_order"},
		"items": []map[string]any{{"label": "Kondisi panel", "item_type": "ok_notok_na", "is_required": true, "photo_required": true}},
	})
	e.mustJSON(st, body, 201, &tpl)
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/checklist-templates/"+tpl.ID.String()+"/publish", nil)
	e.mustJSON(st, body, 200, nil)
	var run struct {
		ID    uuid.UUID `json:"id"`
		Items []struct {
			ID           uuid.UUID  `json:"id"`
			AttachmentID *uuid.UUID `json:"attachment_id"`
		} `json:"items"`
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders/"+woUndated.ID.String()+"/checklist-runs", map[string]any{"template_id": tpl.ID})
	e.mustJSON(st, body, 201, &run)
	m8, m9, m10 = uuid.New(), uuid.New(), uuid.New()
	st, body = e.do(tech, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-1", "mutations": []map[string]any{
		mut(uuid.New(), "work_order", woUndated.ID, "start", 1, map[string]any{"gps_status": "denied"}, now),
		mut(m8, "work_order", woUndated.ID, "attach_photo", 2, map[string]any{"client_attachment_id": "cl-photo-1", "attachment_type": "checklist", "content_type": "image/jpeg", "size_bytes": 1000, "gps_status": "denied", "checklist_item_id": run.Items[0].ID}, now),
		mut(m9, "work_order", woUndated.ID, "checklist_item_result", 3, map[string]any{"item_id": run.Items[0].ID, "run_id": run.ID, "result_value": "ok", "client_attachment_id": "cl-photo-1"}, now),
		mut(m10, "work_order", woUndated.ID, "complete", 4, map[string]any{"completion_notes": "ok"}, now),
	}})
	e.mustJSON(st, body, 200, &res)
	for i, r := range res.Results {
		if r.Status != "applied" {
			t.Fatalf("checklist foto via sync: mutation %d harus applied, got %s %s %s", i, r.Status, r.ReasonCode, r.Detail)
		}
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+woUndated.ID.String()+"/checklist-runs", nil)
	var runs struct {
		Data []struct {
			Items []struct {
				AttachmentID *uuid.UUID `json:"attachment_id"`
			} `json:"items"`
		} `json:"data"`
	}
	e.mustJSON(st, body, 200, &runs)
	if len(runs.Data) == 0 || len(runs.Data[0].Items) == 0 || runs.Data[0].Items[0].AttachmentID == nil {
		t.Fatalf("attachment_id item checklist harus tertaut dari client_attachment_id: %s", body)
	}

	// Task requires_evidence: foto tipe apa pun (mobile mengirim alias `before`) memenuhi guard complete.
	var task workItem
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks", map[string]any{"task_type": "inspection", "title": "Cek APAR lantai 12", "location_id": e.refs.MechRoomA12, "assignee_user_id": budiID, "requires_photo": true, "scheduled_start_at": now, "due_at": now.Add(2 * time.Hour)})
	e.mustJSON(st, body, 201, &task)
	st, body = e.do(tech, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-1", "mutations": []map[string]any{
		mut(uuid.New(), "task", task.ID, "start", 1, map[string]any{"gps_status": "denied"}, now),
		mut(uuid.New(), "task", task.ID, "attach_photo", 2, map[string]any{"client_attachment_id": "cl-photo-task-1", "attachment_type": "before", "content_type": "image/jpeg", "size_bytes": 1000, "gps_status": "denied"}, now),
		mut(uuid.New(), "task", task.ID, "complete", 3, map[string]any{"completion_notes": "ok"}, now),
	}})
	e.mustJSON(st, body, 200, &res)
	for i, r := range res.Results {
		if r.Status != "applied" {
			t.Fatalf("task evidence foto before via sync: mutation %d harus applied, got %s %s %s", i, r.Status, r.ReasonCode, r.Detail)
		}
	}
}

// PRD §19 Overview: Today counters, Attention Required CTA, Today's Operations, Building State, Tenant Requests, PM Due, Team Workload.
func TestOverview(t *testing.T) {
	e := setup(t)
	pm := e.login("pm@demo.buildingvision.id")
	spv := e.login("eng.spv@demo.buildingvision.id")
	ops := e.login("ops@demo.buildingvision.id")
	budiID := e.refs.Users["technician"]
	now := time.Now()

	// data: 1 WO overdue (due kemarin), 1 WO hari ini, 1 incident critical, 1 SR
	var wo workItem
	st, body := e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "Lift 2 tidak beroperasi", "location_id": e.refs.LobbyA, "assignee_user_id": budiID, "priority": "critical", "due_at": now.Add(-2 * time.Hour)})
	e.mustJSON(st, body, 201, &wo)
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "repair", "title": "Perbaikan pintu", "location_id": e.refs.ToiletB3, "assignee_user_id": budiID, "scheduled_start_at": now, "due_at": now.Add(3 * time.Hour)})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do(ops, http.MethodPost, "/api/v1/incidents", map[string]any{"category": "fire_smoke", "title": "Asap di basement", "location_id": e.refs.ParkingLG, "severity": "critical"})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do(ops, http.MethodPost, "/api/v1/service-requests", map[string]any{"category_code": "complaint", "title": "AC bocor", "location_id": e.refs.UnitA1201})
	e.mustJSON(st, body, 201, nil)
	// sweep (worker) menandai overdue
	if _, err := e.app.Operations.OverdueSweep(t.Context(), e.refs.OrgID); err != nil {
		t.Fatal(err)
	}

	var today struct {
		OpenWorkOrders struct {
			Value     int            `json:"value"`
			Breakdown map[string]int `json:"breakdown"`
		} `json:"open_work_orders"`
		Overdue   struct{ Value int } `json:"overdue"`
		Incidents struct {
			Value     int            `json:"value"`
			Breakdown map[string]int `json:"breakdown"`
		} `json:"incidents"`
		TenantRequests struct {
			Value     int            `json:"value"`
			Breakdown map[string]int `json:"breakdown"`
		} `json:"tenant_requests"`
	}
	st, body = e.do(pm, http.MethodGet, "/api/v1/overview/today?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &today)
	if today.OpenWorkOrders.Value != 2 || today.Overdue.Value != 1 || today.Incidents.Value != 1 || today.Incidents.Breakdown["critical"] != 1 || today.TenantRequests.Value != 1 || today.TenantRequests.Breakdown["new_today"] != 1 {
		t.Fatalf("Today counters: %+v", today)
	}
	var att struct {
		Data []struct {
			Category       string   `json:"category"`
			Severity       string   `json:"severity"`
			Label          string   `json:"label"`
			LocationPath   *string  `json:"location_path"`
			AllowedActions []string `json:"allowed_actions"`
			AgeMinutes     int      `json:"age_minutes"`
		} `json:"data"`
		Total int `json:"total"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/overview/attention-required?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &att)
	cats := map[string]bool{}
	for _, a := range att.Data {
		cats[a.Category] = true
		if len(a.AllowedActions) == 0 || a.AllowedActions[0] != "view" {
			t.Fatalf("setiap item harus punya CTA (view minimal): %+v", a)
		}
		if a.Category == "overdue" && a.Label == wo.Number {
			if a.Severity != "critical" || a.LocationPath == nil || !has(a.AllowedActions, "assign") {
				t.Fatalf("overdue WO item: %+v", a)
			}
		}
	}
	if !cats["overdue"] || !cats["critical_incident"] {
		t.Fatalf("Attention Required kategori: %v", cats)
	}
	if att.Data[0].Severity != "critical" {
		t.Fatal("urutan: critical dulu")
	}
	var todays struct {
		Data []struct {
			Domain  string         `json:"domain"`
			Items   []workItem     `json:"items"`
			Summary map[string]int `json:"summary"`
		} `json:"data"`
	}
	st, body = e.do(pm, http.MethodGet, "/api/v1/overview/todays-operations?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &todays)
	if len(todays.Data) != 3 || todays.Data[0].Domain != "engineering" || todays.Data[0].Summary["total"] < 1 {
		t.Fatalf("Today's Operations: %+v", todays.Data)
	}
	var bs struct {
		Data []struct {
			Name    string `json:"name"`
			Open    int    `json:"open"`
			Overdue int    `json:"overdue"`
		} `json:"data"`
	}
	st, body = e.do(pm, http.MethodGet, "/api/v1/overview/building-state?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &bs)
	var towerA, towerB *struct {
		Name    string `json:"name"`
		Open    int    `json:"open"`
		Overdue int    `json:"overdue"`
	}
	for i := range bs.Data {
		if bs.Data[i].Name == "Tower A" {
			towerA = &bs.Data[i]
		}
		if bs.Data[i].Name == "Tower B" {
			towerB = &bs.Data[i]
		}
	}
	if towerA == nil || towerB == nil || towerA.Overdue != 1 || towerB.Open != 1 {
		t.Fatalf("Building State: %+v", bs.Data)
	}
	var tr struct {
		NewToday int                              `json:"new_today"`
		Open     int                              `json:"open"`
		Recent   []struct{ RequestNumber string } `json:"recent"`
	}
	st, body = e.do(pm, http.MethodGet, "/api/v1/overview/tenant-requests?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &tr)
	if tr.NewToday != 1 || tr.Open != 1 || len(tr.Recent) != 1 {
		t.Fatalf("Tenant Requests panel: %+v", tr)
	}
	var wl struct {
		Data []struct {
			AssigneeName   string `json:"assignee_name"`
			OpenWorkOrders int    `json:"open_work_orders"`
			Overdue        int    `json:"overdue"`
		} `json:"data"`
	}
	st, body = e.do(pm, http.MethodGet, "/api/v1/overview/team-workload?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &wl)
	okWL := false
	for _, r := range wl.Data {
		if r.AssigneeName == "Budi Santoso" && r.OpenWorkOrders == 2 && r.Overdue == 1 {
			okWL = true
		}
	}
	if !okWL {
		t.Fatalf("Team Workload: %+v", wl.Data)
	}
	st, body = e.do(pm, http.MethodGet, "/api/v1/overview/pm-due?property_id="+e.refs.PropertyID.String()+"&days=7", nil)
	e.mustJSON(st, body, 200, nil)
	// technician tidak punya overview.dashboard.view
	tech := e.login("budi@demo.buildingvision.id")
	st, _ = e.do(tech, http.MethodGet, "/api/v1/overview/today", nil)
	if st != 403 {
		t.Fatalf("technician overview harus 403, got %d", st)
	}
}
