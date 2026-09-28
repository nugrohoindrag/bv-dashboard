package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/authctx"
)

// PRD P1 v2 (Roadmap v2.0) — Building Operations MVP. Lihat docs/p1-v2-readiness.md & ADR-015.

type p1Item struct {
	ID              uuid.UUID `json:"id"`
	Number          string    `json:"number"`
	Type            string    `json:"type"`
	Status          string    `json:"status"`
	Priority        string    `json:"priority"`
	Category        *string   `json:"category"`
	SLAStatus       string    `json:"sla_status"`
	EscalationLevel int       `json:"escalation_level"`
	Flags           []string  `json:"flags"`
	AllowedActions  []string  `json:"allowed_actions"`
	SubmittedAt     *string   `json:"submitted_at"`
	CompletionNotes *string   `json:"completion_notes"`
	SLA             *struct {
		Status          string  `json:"status"`
		ResolutionDueAt *string `json:"resolution_due_at"`
	} `json:"sla"`
	ActualCost *struct {
		Amount int64 `json:"amount"`
	} `json:"actual_cost"`
	ServiceCost *struct {
		Amount int64 `json:"amount"`
	} `json:"service_cost"`
}

type p1List struct {
	Data []p1Item `json:"data"`
}

func (l p1List) has(id uuid.UUID) bool {
	for _, x := range l.Data {
		if x.ID == id {
			return true
		}
	}
	return false
}

func (e *env) p1List(t *testing.T, token, path string) p1List {
	t.Helper()
	var l p1List
	st, body := e.do(token, http.MethodGet, path, nil)
	e.mustJSON(st, body, 200, &l)
	return l
}

func (e *env) orgTx(t *testing.T, fn func(ctx context.Context, tx pgx.Tx) error) {
	t.Helper()
	ctx := authctx.With(context.Background(), authctx.System(e.refs.OrgID))
	if err := e.app.DB.WithOrgTx(ctx, e.refs.OrgID, fn); err != nil {
		t.Fatal(err)
	}
}

// §6–§8 Building Management: facility operasional, floor plan + marker, occupancy.
func TestP1v2BuildingManagement(t *testing.T) {
	e := setup(t)
	bm := e.login("bm@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	spv := e.login("eng.spv@demo.buildingvision.id")

	// ---- §6.3 Facility: lobby/lift/toilet (bukan booking) ----
	type facility struct {
		ID         uuid.UUID `json:"id"`
		Type       string    `json:"facility_type"`
		Status     string    `json:"status"`
		IsBookable bool      `json:"is_bookable"`
		IsActive   bool      `json:"is_active"`
		OpenWork   int       `json:"open_work"`
	}
	var lobby facility
	st, body := e.do(bm, http.MethodPost, "/api/v1/facilities", map[string]any{"property_id": e.refs.PropertyID, "name": "Lobby Utama", "facility_type": "lobby", "location_id": e.refs.LobbyA, "description": "Lobby Tower A"})
	e.mustJSON(st, body, 201, &lobby)
	if lobby.Status != "operational" || lobby.IsBookable || !lobby.IsActive {
		t.Fatalf("facility lobby: %s", body)
	}
	for _, ft := range []string{"lift", "toilet", "corridor", "common_area", "parking", "gym", "pool", "meeting_room"} {
		if st, body := e.do(bm, http.MethodPost, "/api/v1/facilities", map[string]any{"property_id": e.refs.PropertyID, "name": "F " + ft, "facility_type": ft}); st != 201 {
			t.Fatalf("facility %s: %d %s", ft, st, body)
		}
	}
	if st, _ := e.do(bm, http.MethodPost, "/api/v1/facilities", map[string]any{"property_id": e.refs.PropertyID, "name": "X", "facility_type": "spaceship"}); st != 400 {
		t.Fatalf("tipe facility tidak valid harus 400: %d", st)
	}
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/facilities", map[string]any{"property_id": e.refs.PropertyID, "name": "X", "facility_type": "lobby"}); st != 403 {
		t.Fatalf("technician membuat facility harus 403: %d", st)
	}
	st, body = e.do(bm, http.MethodPatch, "/api/v1/facilities/"+lobby.ID.String(), map[string]any{"status": "under_maintenance"})
	e.mustJSON(st, body, 200, &lobby)
	if lobby.Status != "under_maintenance" || !lobby.IsActive {
		t.Fatalf("status facility: %s", body)
	}
	var facs struct {
		Data []facility `json:"data"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/facilities?type=lobby,lift&status=under_maintenance", nil)
	e.mustJSON(st, body, 200, &facs)
	if len(facs.Data) != 1 || facs.Data[0].ID != lobby.ID {
		t.Fatalf("filter facility: %s", body)
	}
	// foto facility (§6.3 Photo) — attachment object_type facility
	e.uploadAttachment(bm, "facility", lobby.ID, "photo", "image/png")
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "facility", "object_id": lobby.ID, "attachment_type": "photo", "content_type": "image/png", "size_bytes": 10}); st != 403 {
		t.Fatalf("technician unggah foto facility harus 403: %d", st)
	}
	// facility non-bookable tidak dapat dibooking
	if st, _ := e.do(bm, http.MethodPost, "/api/v1/bookings", map[string]any{"facility_id": lobby.ID, "starts_at": time.Now().Add(48 * time.Hour).Format(time.RFC3339), "ends_at": time.Now().Add(49 * time.Hour).Format(time.RFC3339)}); st < 400 {
		t.Fatalf("booking facility operasional harus ditolak: %d", st)
	}

	// ---- §7 Floor plan ----
	type plan struct {
		ID          uuid.UUID `json:"id"`
		Name        string    `json:"name"`
		MarkerCount int       `json:"marker_count"`
		Image       *struct {
			ID  uuid.UUID `json:"id"`
			URL string    `json:"url"`
		} `json:"image"`
		Markers []struct {
			ID          uuid.UUID `json:"id"`
			TargetType  string    `json:"target_type"`
			TargetID    uuid.UUID `json:"target_id"`
			TargetLabel string    `json:"target_label"`
			XPct        float64   `json:"x_pct"`
			Work        struct {
				OpenTasks      int    `json:"open_tasks"`
				OpenWorkOrders int    `json:"open_work_orders"`
				Critical       int    `json:"critical"`
				Signal         string `json:"signal"`
				Items          []struct {
					Number   string `json:"number"`
					DeepLink string `json:"deep_link"`
				} `json:"items"`
			} `json:"work"`
		} `json:"markers"`
	}
	var fp plan
	st, body = e.do(bm, http.MethodPost, "/api/v1/floor-plans", map[string]any{"location_id": e.refs.FloorA12, "name": "Denah Lantai 12", "image_width": 1600, "image_height": 900})
	e.mustJSON(st, body, 201, &fp)
	if st, _ := e.do(bm, http.MethodPost, "/api/v1/floor-plans", map[string]any{"location_id": e.refs.UnitA1201, "name": "Unit"}); st != 400 {
		t.Fatalf("denah di unit harus 400: %d", st)
	}
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/floor-plans", map[string]any{"location_id": e.refs.FloorA12, "name": "X"}); st != 403 {
		t.Fatalf("technician membuat denah harus 403: %d", st)
	}
	img := e.uploadAttachment(bm, "floor_plan", fp.ID, "photo", "image/png")
	// gambar harus milik denah ini
	other := e.uploadAttachment(bm, "facility", lobby.ID, "photo", "image/png")
	if st, _ := e.do(bm, http.MethodPatch, "/api/v1/floor-plans/"+fp.ID.String(), map[string]any{"attachment_id": other}); st != 400 {
		t.Fatalf("attachment bukan milik denah harus 400: %d", st)
	}
	st, body = e.do(bm, http.MethodPatch, "/api/v1/floor-plans/"+fp.ID.String(), map[string]any{"attachment_id": img})
	e.mustJSON(st, body, 200, &fp)
	if fp.Image == nil || fp.Image.ID != img || fp.Image.URL == "" {
		t.Fatalf("gambar denah: %s", body)
	}
	// pekerjaan kritis di Mechanical Room → marker menunjukkan sinyal critical + tautan task
	var task p1Item
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "Cek panel listrik", "location_id": e.refs.MechRoomA12, "priority": "critical", "category": "electrical"})
	e.mustJSON(st, body, 201, &task)
	st, body = e.do(bm, http.MethodPost, "/api/v1/floor-plans/"+fp.ID.String()+"/markers", map[string]any{"target_type": "location", "target_id": e.refs.MechRoomA12, "x_pct": 42.5, "y_pct": 60})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(bm, http.MethodPost, "/api/v1/floor-plans/"+fp.ID.String()+"/markers", map[string]any{"target_type": "facility", "target_id": lobby.ID, "x_pct": 10, "y_pct": 10, "label": "Lobby"})
	e.mustJSON(st, body, 200, nil)
	// upsert: target yang sama memindahkan marker, bukan menduplikasi
	st, body = e.do(bm, http.MethodPost, "/api/v1/floor-plans/"+fp.ID.String()+"/markers", map[string]any{"target_type": "location", "target_id": e.refs.MechRoomA12, "x_pct": 45, "y_pct": 61})
	e.mustJSON(st, body, 200, nil)
	for _, bad := range []map[string]any{
		{"target_type": "location", "target_id": e.refs.MechRoomA12, "x_pct": 120, "y_pct": 10},
		{"target_type": "planet", "target_id": e.refs.MechRoomA12, "x_pct": 1, "y_pct": 1},
		{"target_type": "location", "target_id": uuid.New(), "x_pct": 1, "y_pct": 1},
	} {
		if st, _ := e.do(bm, http.MethodPost, "/api/v1/floor-plans/"+fp.ID.String()+"/markers", bad); st != 400 {
			t.Fatalf("marker tidak valid %v harus 400: %d", bad, st)
		}
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/floor-plans/"+fp.ID.String(), nil)
	e.mustJSON(st, body, 200, &fp)
	if len(fp.Markers) != 2 {
		t.Fatalf("marker: %s", body)
	}
	for _, m := range fp.Markers {
		if m.TargetID == e.refs.MechRoomA12 {
			if m.XPct != 45 || m.Work.OpenTasks != 1 || m.Work.Critical != 1 || m.Work.Signal != "critical" || len(m.Work.Items) != 1 || !strings.HasPrefix(m.Work.Items[0].DeepLink, "/operations/tasks/") {
				t.Fatalf("ringkasan kerja marker: %+v", m)
			}
		}
	}
	// denah ditemukan dari lokasi turunan (unit di lantai 12) & reverse lookup target
	var plans struct {
		Data []plan `json:"data"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/floor-plans?location_id="+e.refs.UnitA1201.String(), nil)
	e.mustJSON(st, body, 200, &plans)
	if len(plans.Data) != 1 || plans.Data[0].MarkerCount != 2 {
		t.Fatalf("denah via lokasi turunan: %s", body)
	}
	var refs struct {
		Data []struct {
			FloorPlanID uuid.UUID `json:"floor_plan_id"`
		} `json:"data"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/floor-plan-markers?target_type=facility&target_id="+lobby.ID.String(), nil)
	e.mustJSON(st, body, 200, &refs)
	if len(refs.Data) != 1 || refs.Data[0].FloorPlanID != fp.ID {
		t.Fatalf("reverse lookup marker: %s", body)
	}
	// isolasi tenant: org lain tidak melihat denah
	orgB := e.login(e.orgBAdmin)
	if st, _ := e.do(orgB, http.MethodGet, "/api/v1/floor-plans/"+fp.ID.String(), nil); st != 404 {
		t.Fatalf("denah lintas organization harus 404: %d", st)
	}

	// ---- §8 Occupancy ----
	pm := e.login("pm@demo.buildingvision.id")
	if st, body := e.do(pm, http.MethodPatch, "/api/v1/units/"+e.refs.UnitA1202.String(), map[string]any{"details": map[string]any{"occupancy_status": "inactive"}}); st != 200 {
		t.Fatalf("occupancy inactive: %d %s", st, body)
	}
	if st, _ := e.do(pm, http.MethodPatch, "/api/v1/units/"+e.refs.UnitA1202.String(), map[string]any{"details": map[string]any{"occupancy_status": "haunted"}}); st != 400 {
		t.Fatalf("occupancy tidak valid harus 400: %d", st)
	}
	var occ struct {
		Total        int     `json:"total"`
		Occupied     int     `json:"occupied"`
		Inactive     int     `json:"inactive"`
		OccupancyPct float64 `json:"occupancy_pct"`
		Floors       []struct {
			LocationID uuid.UUID `json:"location_id"`
			Total      int       `json:"total"`
		} `json:"floors"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/occupancy/summary?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &occ)
	if occ.Total < 2 || occ.Inactive != 1 || len(occ.Floors) == 0 {
		t.Fatalf("occupancy summary: %s", body)
	}
	var units struct {
		Data []struct {
			LocationID      uuid.UUID `json:"location_id"`
			OccupancyStatus string    `json:"occupancy_status"`
			Tenant          *struct {
				Name string `json:"name"`
			} `json:"tenant"`
		} `json:"data"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/occupancy/units?floor_id="+e.refs.FloorA12.String()+"&status=inactive", nil)
	e.mustJSON(st, body, 200, &units)
	if len(units.Data) != 1 || units.Data[0].LocationID != e.refs.UnitA1202 {
		t.Fatalf("occupancy units: %s", body)
	}
	if st, _ := e.do(bm, http.MethodGet, "/api/v1/occupancy/units?status=zzz", nil); st != 400 {
		t.Fatalf("status occupancy tidak valid harus 400: %d", st)
	}
}

// §13–§26 Task & Work Order: kategori, due today, eskalasi, SLA status, Draft → Open, tipe WO, cost, evidence During.
func TestP1v2TaskAndWorkOrder(t *testing.T) {
	e := setup(t)
	spv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	bm := e.login("bm@demo.buildingvision.id")
	techID := e.refs.Users["technician"]
	tech2ID := e.refs.Users["technician_2"]

	// ---- Task: kategori + due hari ini + assign/reassign ----
	loc, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(loc)
	dueToday := time.Date(now.Year(), now.Month(), now.Day(), 23, 30, 0, 0, loc)
	var task p1Item
	st, body := e.do(spv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "Cek AHU lantai 12", "location_id": e.refs.MechRoomA12, "priority": "high", "category": "hvac",
		"assignee_team_id": e.refs.Teams["engineering"], "due_at": dueToday.UTC().Format(time.RFC3339), "scheduled_start_at": now.UTC().Format(time.RFC3339)})
	e.mustJSON(st, body, 201, &task)
	if task.Category == nil || *task.Category != "hvac" || task.SLAStatus != "on_track" {
		t.Fatalf("task awal: %s", body)
	}
	if st, _ := e.do(spv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "x", "location_id": e.refs.MechRoomA12, "category": strings.Repeat("x", 61)}); st != 400 {
		t.Fatalf("kategori > 60 karakter harus 400: %d", st)
	}
	for _, a := range []uuid.UUID{techID, tech2ID} {
		st, body = e.do(spv, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/assign", map[string]any{"assignee_user_id": a, "assignee_team_id": e.refs.Teams["engineering"]})
		e.mustJSON(st, body, 200, nil)
	}
	var hist struct {
		Data []struct {
			IsCurrent bool `json:"is_current"`
		} `json:"data"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/tasks/"+task.ID.String()+"/assignments", nil)
	e.mustJSON(st, body, 200, &hist)
	if len(hist.Data) != 3 || !hist.Data[0].IsCurrent {
		t.Fatalf("assignment history (team → A → B): %s", body)
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/assign", map[string]any{"assignee_user_id": techID, "assignee_team_id": e.refs.Teams["engineering"]})
	e.mustJSON(st, body, 200, nil)
	if l := e.p1List(t, spv, "/api/v1/tasks?due_today=true"); !l.has(task.ID) {
		t.Fatal("filter due_today")
	}
	if l := e.p1List(t, spv, "/api/v1/tasks?category=hvac,plumbing"); !l.has(task.ID) || len(l.Data) != 1 {
		t.Fatal("filter category")
	}
	if l := e.p1List(t, spv, "/api/v1/tasks?sla_status=on_track"); !l.has(task.ID) {
		t.Fatal("filter sla_status=on_track")
	}
	if st, _ := e.do(spv, http.MethodGet, "/api/v1/tasks?sla_status=bogus", nil); st != 400 {
		t.Fatalf("sla_status tidak valid harus 400: %d", st)
	}
	// §40 search: assignee
	if l := e.p1List(t, spv, "/api/v1/tasks?q=Budi"); !l.has(task.ID) {
		t.Fatal("search task by assignee")
	}
	var cats struct {
		Data []struct {
			Code  string `json:"code"`
			Count int    `json:"count"`
		} `json:"data"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/task-categories", nil)
	e.mustJSON(st, body, 200, &cats)
	found := false
	for _, c := range cats.Data {
		if c.Code == "hvac" && c.Count == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("task categories: %s", body)
	}

	// ---- Escalation (§52) ----
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/escalate", map[string]any{"reason": "x"}); st != 403 {
		t.Fatalf("technician eskalasi harus 403: %d", st)
	}
	if st, _ := e.do(spv, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/escalate", map[string]any{}); st != 400 {
		t.Fatalf("eskalasi tanpa reason harus 400: %d", st)
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/escalate", map[string]any{"reason": "Butuh keputusan manajer", "raise_priority": true, "escalate_to_user_id": e.refs.Users["building_manager"]})
	e.mustJSON(st, body, 200, &task)
	if task.EscalationLevel != 1 || task.Priority != "critical" || !has(task.Flags, "escalated") {
		t.Fatalf("eskalasi: %s", body)
	}
	if l := e.p1List(t, spv, "/api/v1/tasks?escalated=true"); !l.has(task.ID) {
		t.Fatal("filter escalated")
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, bm); !hasType(ib, "task_escalated") {
		t.Fatalf("manager harus menerima task_escalated: %+v", ib.Data)
	}

	// ---- Execution: start → evidence before/during/after → complete dengan catatan ----
	st, body = e.do(tech, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/start", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	for _, at := range []string{"photo_before", "photo_during", "photo_after", "document"} {
		ct := "image/png"
		if at == "document" {
			ct = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		}
		e.uploadAttachment(tech, "task", task.ID, at, ct)
	}
	var atts struct {
		Data []struct {
			AttachmentType string `json:"attachment_type"`
			UploadedBy     string `json:"uploaded_by"`
			UploadedAt     string `json:"uploaded_at"`
		} `json:"data"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/attachments?object_type=task&object_id="+task.ID.String(), nil)
	e.mustJSON(st, body, 200, &atts)
	gotDuring := false
	for _, a := range atts.Data {
		if a.AttachmentType == "photo_during" && a.UploadedBy != "" && a.UploadedAt != "" {
			gotDuring = true
		}
	}
	if !gotDuring || len(atts.Data) != 4 {
		t.Fatalf("evidence before/during/after/document: %s", body)
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/complete", map[string]any{"completion_notes": "Filter AHU diganti"})
	e.mustJSON(st, body, 200, &task)
	if task.CompletionNotes == nil || task.SLAStatus != "completed" || has(task.Flags, "escalated") {
		t.Fatalf("complete: %s", body)
	}
	if l := e.p1List(t, spv, "/api/v1/tasks?completed_today=true"); !l.has(task.ID) {
		t.Fatal("filter completed_today")
	}

	// ---- Work Order: tipe baru, Draft → Open, cost breakdown ----
	for _, typ := range []string{"preventive", "inspection", "other", "corrective"} {
		if st, body := e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": typ, "title": "WO " + typ, "location_id": e.refs.MechRoomA12}); st != 201 {
			t.Fatalf("WO %s: %d %s", typ, st, body)
		}
	}
	if st, _ := e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "general", "title": "x", "location_id": e.refs.MechRoomA12}); st != 400 {
		t.Fatalf("tipe WO tidak valid harus 400: %d", st)
	}
	if st, _ := e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "repair", "title": "x", "location_id": e.refs.MechRoomA12, "draft": true, "assignee_user_id": techID}); st != 400 {
		t.Fatalf("draft ber-assignee harus 400: %d", st)
	}
	var wo p1Item
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "repair", "title": "Perbaikan pompa", "location_id": e.refs.MechRoomA12, "priority": "high", "draft": true,
		"estimated_cost": map[string]any{"currency_code": "IDR", "amount": 1500000}})
	e.mustJSON(st, body, 201, &wo)
	if wo.Status != "draft" || wo.SLA != nil || wo.SubmittedAt != nil || !has(wo.AllowedActions, "submit") {
		t.Fatalf("WO draft: %s", body)
	}
	if st, _ := e.do(spv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/assign", map[string]any{"assignee_user_id": techID}); st != 409 && st != 422 {
		t.Fatalf("assign draft harus ditolak: %d", st)
	}
	if l := e.p1List(t, spv, "/api/v1/work-orders?open=true"); l.has(wo.ID) {
		t.Fatal("draft tidak boleh muncul di open=true")
	}
	if l := e.p1List(t, spv, "/api/v1/work-orders?status=draft"); !l.has(wo.ID) {
		t.Fatal("filter status=draft")
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/submit", map[string]any{})
	e.mustJSON(st, body, 200, &wo)
	if wo.Status != "new" || wo.SubmittedAt == nil || wo.SLA == nil || wo.SLA.ResolutionDueAt == nil {
		t.Fatalf("submit draft → Open + SLA: %s", body)
	}
	// draft kedua dihapus (delete draft PRD P0 v2)
	var wo2 p1Item
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "service", "title": "Salah input", "location_id": e.refs.MechRoomA12, "draft": true})
	e.mustJSON(st, body, 201, &wo2)
	if st, body := e.do(spv, http.MethodDelete, "/api/v1/work-orders/"+wo2.ID.String(), map[string]any{"reason": "salah input"}); st != 204 {
		t.Fatalf("hapus draft: %d %s", st, body)
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/assign", map[string]any{"assignee_user_id": techID})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(spv, http.MethodPatch, "/api/v1/work-orders/"+wo.ID.String(), map[string]any{"service_cost": map[string]any{"amount": 500000}, "other_cost": map[string]any{"amount": 100000}})
	e.mustJSON(st, body, 200, &wo)
	if wo.ActualCost == nil || wo.ActualCost.Amount != 600000 || wo.ServiceCost == nil {
		t.Fatalf("actual cost = parts+jasa+lain: %s", body)
	}
	if st, _ := e.do(spv, http.MethodPatch, "/api/v1/work-orders/"+wo.ID.String(), map[string]any{"other_cost": map[string]any{"amount": -1}}); st != 400 {
		t.Fatalf("biaya negatif harus 400: %d", st)
	}

	// ---- SLA risk + breach dalam satu sweep (perbaikan bug) & auto-escalation ----
	e.orgTx(t, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE sla_tracking SET started_at = now() - interval '10 hours', resolution_due_at = now() - interval '1 hour', response_due_at = now() - interval '9 hours' WHERE object_type = 'work_order' AND object_id = $1`, wo.ID)
		return err
	})
	if _, err := e.app.Operations.SLASweep(context.Background(), e.refs.OrgID); err != nil {
		t.Fatal(err)
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+wo.ID.String(), nil)
	e.mustJSON(st, body, 200, &wo)
	if wo.SLAStatus != "breached" || wo.EscalationLevel != 1 || !has(wo.Flags, "sla_breach") {
		t.Fatalf("breach + auto-escalation: %s", body)
	}
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	if sla, _ := raw["sla"].(map[string]any); sla == nil || sla["response_status"] != "breached" || sla["status"] != "breached" {
		t.Fatalf("SLA response/resolution status: %s", body)
	}
	if l := e.p1List(t, spv, "/api/v1/work-orders?sla_risk=true"); !l.has(wo.ID) {
		t.Fatal("breach harus ikut sla_risk=true (sla_risk_at terisi)")
	}
	if l := e.p1List(t, spv, "/api/v1/work-orders?sla_status=breached"); !l.has(wo.ID) {
		t.Fatal("filter sla_status=breached")
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, bm); !hasType(ib, "work_order_sla_breached") {
		t.Fatalf("manager harus menerima work_order_sla_breached: %+v", ib.Data)
	}
}

// §27–§33 Tenant Service Request (request type, Request → Task/WO, notifikasi, reopen, rating) & Incident.
func TestP1v2ServiceRequestAndIncident(t *testing.T) {
	e := setup(t)
	tenA, _ := e.setupTenants(t)
	tr := e.login("tr.manager@demo.buildingvision.id")
	spv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	bm := e.login("bm@demo.buildingvision.id")
	techID := e.refs.Users["technician"]

	var sr struct {
		ID           uuid.UUID `json:"id"`
		RequestType  string    `json:"request_type"`
		TenantStatus string    `json:"tenant_status"`
		ReopenCount  int       `json:"reopen_count"`
	}
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "air_conditioning", "request_type": "wish", "description": "AC tidak dingin sejak pagi"}); st != 400 {
		t.Fatalf("request_type tidak valid harus 400: %d", st)
	}
	st, body := e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "air_conditioning", "request_type": "complaint", "title": "AC tidak dingin", "description": "AC ruang rapat tidak dingin sejak pagi"})
	e.mustJSON(st, body, 201, &sr)
	if sr.RequestType != "complaint" {
		t.Fatalf("request type: %s", body)
	}
	// default dari kategori
	var sr2 struct {
		ID          uuid.UUID `json:"id"`
		RequestType string    `json:"request_type"`
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "cleanliness", "description": "Koridor depan unit kotor sekali"})
	e.mustJSON(st, body, 201, &sr2)
	if sr2.RequestType != "cleaning_request" {
		t.Fatalf("request type default kategori: %s", body)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, bm); !hasType(ib, "service_request_received") {
		t.Fatalf("manager harus menerima request baru: %+v", ib.Data)
	}
	var srs struct {
		Data []struct {
			ID          uuid.UUID `json:"id"`
			RequestType string    `json:"request_type"`
			SLAStatus   string    `json:"sla_status"`
			Flags       []string  `json:"flags"`
		} `json:"data"`
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/service-requests?request_type=complaint", nil)
	e.mustJSON(st, body, 200, &srs)
	if len(srs.Data) != 1 || srs.Data[0].ID != sr.ID || srs.Data[0].SLAStatus != "on_track" {
		t.Fatalf("filter request_type + sla_status: %s", body)
	}

	// ---- Request → Task (triage → task → team → execution → resolution → tenant notification) ----
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr.ID.String()+"/acknowledge", nil)
	e.mustJSON(st, body, 200, nil)
	var task p1Item
	st, body = e.do(spv, http.MethodPost, "/api/v1/service-requests/"+sr.ID.String()+"/tasks", map[string]any{"assignee_user_id": techID, "category": "hvac"})
	e.mustJSON(st, body, 201, &task)
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/requests/"+sr.ID.String(), nil)
	e.mustJSON(st, body, 200, &sr)
	if sr.TenantStatus != "in_progress" {
		t.Fatalf("tenant status setelah task dibuat: %s", body)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "ticket_status") {
		t.Fatalf("tenant harus menerima update in progress: %+v", ib.Data)
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/start", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tech, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/complete", map[string]any{"completion_notes": "Freon diisi ulang"})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/requests/"+sr.ID.String(), nil)
	e.mustJSON(st, body, 200, &sr)
	if sr.TenantStatus != "resolved" {
		t.Fatalf("request resolved setelah task selesai: %s", body)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "ticket_resolved") {
		t.Fatalf("tenant harus menerima ticket_resolved: %+v", ib.Data)
	}
	if ib := e.inboxOf(t, bm); !hasType(ib, "service_request_resolved") {
		t.Fatalf("manager harus menerima service_request_resolved: %+v", ib.Data)
	}
	// tenant reopen → flag reopened + Attention Required "reopened_request"
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+sr.ID.String()+"/reopen", map[string]any{"reason": "AC masih tidak dingin"})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tr, http.MethodGet, "/api/v1/service-requests?reopened=true", nil)
	e.mustJSON(st, body, 200, &srs)
	if len(srs.Data) != 1 || !has(srs.Data[0].Flags, "reopened") {
		t.Fatalf("filter reopened: %s", body)
	}
	var att struct {
		Data []struct {
			Category string    `json:"category"`
			ObjectID uuid.UUID `json:"object_id"`
			DeepLink string    `json:"deep_link"`
		} `json:"data"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/overview/attention-required?limit=50&property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &att)
	okReopen := false
	for _, a := range att.Data {
		if a.Category == "reopened_request" && a.ObjectID == sr.ID && strings.HasPrefix(a.DeepLink, "/operations/service-requests/") {
			okReopen = true
		}
	}
	if !okReopen {
		t.Fatalf("attention reopened_request: %s", body)
	}
	// staf me-reopen request lain → tenant menerima ticket_reopened
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr2.ID.String()+"/acknowledge", nil)
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr2.ID.String()+"/resolve", map[string]any{"resolution": "Sudah dibersihkan"})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr2.ID.String()+"/reopen", map[string]any{"reason": "Inspeksi: masih kotor"})
	e.mustJSON(st, body, 200, nil)
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "ticket_reopened") {
		t.Fatalf("tenant harus menerima ticket_reopened: %+v", ib.Data)
	}

	// ---- §33 Incident: action taken + Critical Incident notification ----
	sec := e.login("sec.spv@demo.buildingvision.id")
	secMgr := e.login("sec.manager@demo.buildingvision.id")
	officer := e.login("wawan@demo.buildingvision.id")
	var inc struct {
		ID          uuid.UUID `json:"id"`
		ActionTaken *string   `json:"action_taken"`
		Flags       []string  `json:"flags"`
		SLAStatus   string    `json:"sla_status"`
		SLA         *struct {
			Status string `json:"status"`
		} `json:"sla"`
	}
	st, body = e.do(secMgr, http.MethodPost, "/api/v1/incidents", map[string]any{"title": "Asap di ruang panel", "category": "fire_smoke", "severity": "critical", "location_id": e.refs.MechRoomA12, "action_taken": "Area dikosongkan, APAR disiapkan"})
	e.mustJSON(st, body, 201, &inc)
	if inc.ActionTaken == nil || !has(inc.Flags, "critical") || inc.SLA == nil || inc.SLAStatus != "on_track" {
		t.Fatalf("incident: %s", body)
	}
	st, body = e.do(secMgr, http.MethodPatch, "/api/v1/incidents/"+inc.ID.String(), map[string]any{"action_taken": "Damkar gedung di lokasi"})
	e.mustJSON(st, body, 200, &inc)
	if inc.ActionTaken == nil || *inc.ActionTaken != "Damkar gedung di lokasi" {
		t.Fatalf("update action taken: %s", body)
	}
	e.dispatch(t)
	for name, tok := range map[string]string{"manager": bm, "supervisor": sec, "worker": officer} {
		if ib := e.inboxOf(t, tok); !hasType(ib, "incident_critical") {
			t.Fatalf("%s harus menerima incident_critical: %+v", name, ib.Data)
		}
	}
	// non-critical tidak memicu incident_critical untuk manager
	before := len(e.inboxOf(t, bm).Data)
	st, body = e.do(sec, http.MethodPost, "/api/v1/incidents", map[string]any{"title": "Lampu parkir mati", "severity": "low", "location_id": e.refs.ParkingLG})
	e.mustJSON(st, body, 201, nil)
	e.dispatch(t)
	if after := len(e.inboxOf(t, bm).Data); after != before {
		t.Fatalf("incident low tidak boleh menotifikasi manager: %d → %d", before, after)
	}
}

// §9–§12 Operations Dashboard, §37 drill-down, §41 reports, §44 worker "Limited" dashboard.
func TestP1v2DashboardAndReports(t *testing.T) {
	e := setup(t)
	bm := e.login("bm@demo.buildingvision.id")
	spv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	techID := e.refs.Users["technician"]

	// asset kritis + WO → Critical Asset Issue
	var eq struct {
		Data []struct {
			ID       uuid.UUID `json:"id"`
			TypeName *string   `json:"type_name"`
		} `json:"data"`
	}
	st, body := e.do(bm, http.MethodGet, "/api/v1/equipment?q=AHU", nil)
	e.mustJSON(st, body, 200, &eq)
	var asset struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(e.login("eng.manager@demo.buildingvision.id"), http.MethodPost, "/api/v1/assets", map[string]any{"name": "Chiller-01", "equipment_id": eq.Data[0].ID, "location_id": e.refs.MechRoomA12, "criticality": "critical"})
	e.mustJSON(st, body, 201, &asset)
	var wo p1Item
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "Chiller trip", "asset_id": asset.ID, "priority": "high", "assignee_user_id": techID})
	e.mustJSON(st, body, 201, &wo)
	if l := e.p1List(t, spv, "/api/v1/work-orders?q=Chiller-01"); !l.has(wo.ID) {
		t.Fatal("search WO by asset")
	}
	// task overdue + task due today + task selesai hari ini
	var overdue, done p1Item
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "Task overdue", "location_id": e.refs.LobbyA, "assignee_user_id": techID, "due_at": time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)})
	e.mustJSON(st, body, 201, &overdue)
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "Task selesai", "location_id": e.refs.LobbyA, "assignee_user_id": techID, "category": "cleaning", "due_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	e.mustJSON(st, body, 201, &done)
	for _, a := range []string{"start", "complete"} {
		st, body = e.do(tech, http.MethodPost, "/api/v1/tasks/"+done.ID.String()+"/"+a, map[string]any{})
		e.mustJSON(st, body, 200, nil)
	}

	var unassigned p1Item
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "Belum ditugaskan", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, &unassigned)
	if l := e.p1List(t, bm, "/api/v1/tasks?unassigned=true&open=true"); !l.has(unassigned.ID) || l.has(overdue.ID) {
		t.Fatal("filter unassigned")
	}

	// ---- §10 Today's Operations ----
	var today map[string]struct {
		Value     int            `json:"value"`
		Breakdown map[string]int `json:"breakdown"`
		Link      string         `json:"link"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/overview/today?property_id="+e.refs.PropertyID.String(), nil)
	var rawToday map[string]json.RawMessage
	e.mustJSON(st, body, 200, &rawToday)
	delete(rawToday, "generated_at")
	b, _ := json.Marshal(rawToday)
	_ = json.Unmarshal(b, &today)
	for _, k := range []string{"open_tasks", "due_today", "overdue", "overdue_tasks", "sla_risk", "completed_today", "open_work_orders", "incidents", "tenant_requests", "pm_due"} {
		c, ok := today[k]
		if !ok || c.Link == "" {
			t.Fatalf("KPI %s tidak ada / tanpa drill-down link: %s", k, body)
		}
	}
	if today["overdue_tasks"].Value != 1 || today["completed_today"].Value != 1 || today["open_tasks"].Value != 2 || today["open_tasks"].Breakdown["unassigned"] != 1 {
		t.Fatalf("nilai KPI: %s", body)
	}
	// drill-down → list terfilter
	if l := e.p1List(t, bm, "/api/v1"+strings.Replace(today["overdue_tasks"].Link, "/operations", "", 1)); !l.has(overdue.ID) {
		t.Fatalf("drill-down overdue tasks %s", today["overdue_tasks"].Link)
	}
	if l := e.p1List(t, bm, "/api/v1"+strings.Replace(today["completed_today"].Link, "/operations", "", 1)); !l.has(done.ID) {
		t.Fatalf("drill-down completed today %s", today["completed_today"].Link)
	}

	// ---- §11 Attention Required ----
	var att struct {
		Data []struct {
			Category   string    `json:"category"`
			ObjectType string    `json:"object_type"`
			ObjectID   uuid.UUID `json:"object_id"`
			DeepLink   string    `json:"deep_link"`
		} `json:"data"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/overview/attention-required?limit=50", nil)
	e.mustJSON(st, body, 200, &att)
	cats := map[string]uuid.UUID{}
	for _, a := range att.Data {
		cats[a.Category] = a.ObjectID
		if a.DeepLink == "" {
			t.Fatalf("attention tanpa deep link: %+v", a)
		}
	}
	if cats["overdue_task"] != overdue.ID || cats["critical_asset_issue"] != wo.ID {
		t.Fatalf("attention categories: %s", body)
	}
	// perbaikan bug filter domain: engineering tidak memuat item domain lain
	st, body = e.do(bm, http.MethodGet, "/api/v1/overview/attention-required?domain=security&limit=50", nil)
	e.mustJSON(st, body, 200, &att)
	for _, a := range att.Data {
		if a.ObjectType == "work_order" {
			t.Fatalf("domain security memuat WO: %s", body)
		}
	}

	// ---- §12 Team Workload ----
	var wl struct {
		Data []struct {
			TeamName string `json:"team_name"`
			Open     int    `json:"open"`
			DueToday int    `json:"due_today"`
			Overdue  int    `json:"overdue"`
		} `json:"data"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/overview/team-workload?group=team", nil)
	e.mustJSON(st, body, 200, &wl)
	okTeam := false
	for _, r := range wl.Data {
		if r.TeamName == "Engineering Team" && r.Open >= 2 && r.Overdue >= 1 {
			okTeam = true
		}
	}
	if !okTeam {
		t.Fatalf("team workload per team: %s", body)
	}

	// ---- §44 Worker dashboard "Limited" ----
	if st, _ := e.do(tech, http.MethodGet, "/api/v1/overview/today", nil); st != 403 {
		t.Fatalf("worker tidak punya dashboard manajemen: %d", st)
	}
	var mine struct {
		Open           int     `json:"open"`
		Overdue        int     `json:"overdue"`
		CompletedToday int     `json:"completed_today"`
		Next           *p1Item `json:"next"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/me/work-summary", nil)
	e.mustJSON(st, body, 200, &mine)
	if mine.Open < 2 || mine.Overdue < 1 || mine.CompletedToday != 1 || mine.Next == nil || !has(mine.Next.AllowedActions, "start") {
		t.Fatalf("my work summary: %s", body)
	}

	// ---- §41 Reports + §51 KPI ----
	for _, name := range []string{"tasks", "sla", "incidents", "backlog", "operations-kpi", "work-orders", "service-requests"} {
		var rep struct {
			Name       string                     `json:"name"`
			Summary    map[string]float64         `json:"summary"`
			Breakdowns map[string]json.RawMessage `json:"breakdowns"`
		}
		st, body := e.do(bm, http.MethodGet, "/api/v1/reports/"+name+"?property_id="+e.refs.PropertyID.String(), nil)
		e.mustJSON(st, body, 200, &rep)
		if rep.Name != name || len(rep.Summary) == 0 {
			t.Fatalf("report %s: %s", name, body)
		}
	}
	var kpi struct {
		Breakdowns struct {
			KPI []struct {
				Key    string             `json:"key"`
				Values map[string]float64 `json:"values"`
			} `json:"kpi"`
		} `json:"breakdowns"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/reports/operations-kpi", nil)
	e.mustJSON(st, body, 200, &kpi)
	if len(kpi.Breakdowns.KPI) != 6 {
		t.Fatalf("6 KPI P1: %s", body)
	}
	var backlog struct {
		Summary map[string]float64 `json:"summary"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/reports/backlog?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &backlog)
	if backlog.Summary["open"] < 2 || backlog.Summary["overdue"] < 1 {
		t.Fatalf("backlog: %s", body)
	}
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/api/v1/reports/tasks?format=csv", nil)
	req.Header.Set("Authorization", "Bearer "+bm)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("export CSV report: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if st, _ := e.do(tech, http.MethodGet, "/api/v1/reports/tasks", nil); st != 403 {
		t.Fatalf("worker tanpa reports.reports.view harus 403: %d", st)
	}
}
