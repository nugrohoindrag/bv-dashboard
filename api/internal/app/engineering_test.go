package app_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// AT-004: PM Generation — Maintenance Plan aktif → Maintenance Schedule → Maintenance Work Order; hook → schedule completed; asset history.
func TestEngineeringPreventiveMaintenance(t *testing.T) {
	e := setup(t)
	engMgr := e.login("eng.manager@demo.buildingvision.id")
	spv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")

	var eq struct {
		Data []struct {
			ID           uuid.UUID `json:"id"`
			CategoryCode string    `json:"category_code"`
			TypeName     *string   `json:"type_name"`
		} `json:"data"`
	}
	st, body := e.do(engMgr, http.MethodGet, "/api/v1/equipment?q=AHU", nil)
	e.mustJSON(st, body, 200, &eq)
	var ahuID uuid.UUID
	for _, x := range eq.Data {
		if x.TypeName != nil && *x.TypeName == "AHU" {
			ahuID = x.ID
		}
	}
	if ahuID == uuid.Nil {
		t.Fatal("equipment AHU tidak ada di seed")
	}
	var asset struct {
		ID           uuid.UUID `json:"id"`
		AssetCode    string    `json:"asset_code"`
		QRCode       *string   `json:"qr_code"`
		LocationPath string    `json:"location_path"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/assets", map[string]any{"name": "AHU-03", "equipment_id": ahuID, "location_id": e.refs.MechRoomA12, "criticality": "high", "manufacturer": "Daikin"})
	e.mustJSON(st, body, 201, &asset)
	if asset.AssetCode != "AST-HVAC-000001" || asset.QRCode == nil {
		t.Fatalf("asset: %+v", asset)
	}
	if !strings.Contains(asset.LocationPath, "Mechanical Room") {
		t.Fatalf("asset location path: %s", asset.LocationPath)
	}
	// QR resolve (PRD §22)
	var qr struct {
		ObjectType string    `json:"object_type"`
		ObjectID   uuid.UUID `json:"object_id"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/qr/"+*asset.QRCode+"/resolve", nil)
	e.mustJSON(st, body, 200, &qr)
	if qr.ObjectType != "asset" || qr.ObjectID != asset.ID {
		t.Fatalf("qr resolve: %+v", qr)
	}
	adminB := e.login(e.orgBAdmin)
	st, _ = e.do(adminB, http.MethodGet, "/api/v1/qr/"+*asset.QRCode+"/resolve", nil)
	if st != 404 {
		t.Fatalf("AT-010 QR lintas org harus 404, got %d", st)
	}

	var plan struct {
		ID            uuid.UUID `json:"id"`
		PlanCode      string    `json:"plan_code"`
		Status        string    `json:"status"`
		ScheduleCount int       `json:"schedule_count"`
	}
	today := time.Now().In(mustLoc()).Format("2006-01-02")
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/maintenance-plans", map[string]any{"name": "PM Harian AHU", "asset_id": asset.ID, "frequency": "daily", "start_date": today, "default_priority": "medium", "responsible_team_id": e.refs.Teams["engineering"], "lead_time_days": 0})
	e.mustJSON(st, body, 201, &plan)
	if !strings.HasPrefix(plan.PlanCode, "PM-") || plan.Status != "draft" {
		t.Fatalf("plan: %+v", plan)
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/maintenance-plans/"+plan.ID.String()+"/publish", nil)
	e.mustJSON(st, body, 200, &plan)
	if plan.Status != "published" || plan.ScheduleCount < 60 {
		t.Fatalf("AT-004: schedule harus ter-generate >=60 (horizon), got %d status %s", plan.ScheduleCount, plan.Status)
	}
	var gen struct {
		Generated int `json:"generated"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/maintenance-plans/"+plan.ID.String()+"/generate", nil)
	e.mustJSON(st, body, 200, &gen)
	if gen.Generated != 0 {
		t.Fatalf("AT-004: generator harus idempotent, got %d baru", gen.Generated)
	}
	var run struct {
		Created int `json:"created"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/maintenance-schedules/run-due", nil)
	e.mustJSON(st, body, 200, &run)
	if run.Created != 1 {
		t.Fatalf("AT-004: WO maintenance dibuat %d, want 1", run.Created)
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/maintenance-schedules/run-due", nil)
	e.mustJSON(st, body, 200, &run)
	if run.Created != 0 {
		t.Fatalf("AT-004: run-due kedua harus 0 (idempotent), got %d", run.Created)
	}
	var sch struct {
		Data []struct {
			ID          uuid.UUID  `json:"id"`
			Status      string     `json:"status"`
			WorkOrderID *uuid.UUID `json:"work_order_id"`
		} `json:"data"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/maintenance-schedules?plan_id="+plan.ID.String()+"&status=due,overdue,in_progress", nil)
	e.mustJSON(st, body, 200, &sch)
	if len(sch.Data) != 1 || sch.Data[0].WorkOrderID == nil {
		t.Fatalf("schedule due: %+v", sch.Data)
	}
	woID := *sch.Data[0].WorkOrderID
	var wo workItem
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+woID.String(), nil)
	e.mustJSON(st, body, 200, &wo)
	if wo.Status != "assigned" {
		t.Fatalf("maintenance WO status %s", wo.Status)
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+woID.String()+"/start", nil)
	e.mustJSON(st, body, 200, &wo)
	var pre struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
		StorageKey   string    `json:"storage_key"`
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "work_order", "object_id": woID, "attachment_type": "photo_after", "content_type": "image/jpeg", "size_bytes": 100})
	e.mustJSON(st, body, 201, &pre)
	_ = e.store.Put(context.Background(), pre.StorageKey, "image/jpeg", bytes.NewReader(make([]byte, 100)), 100)
	st, body = e.do(tech, http.MethodPost, "/api/v1/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+woID.String()+"/complete", map[string]any{"completion_notes": "PM selesai"})
	e.mustJSON(st, body, 200, &wo)
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders/"+woID.String()+"/close", nil)
	e.mustJSON(st, body, 200, &wo)
	st, body = e.do(spv, http.MethodGet, "/api/v1/maintenance-schedules?plan_id="+plan.ID.String()+"&status=completed", nil)
	e.mustJSON(st, body, 200, &sch)
	if len(sch.Data) != 1 {
		t.Fatalf("WF-001: schedule harus completed setelah WO close, got %d", len(sch.Data))
	}
	var hist struct {
		Data []struct {
			Kind  string `json:"kind"`
			Label string `json:"label"`
		} `json:"data"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/assets/"+asset.ID.String()+"/history", nil)
	e.mustJSON(st, body, 200, &hist)
	foundWO, foundAct := false, false
	for _, h := range hist.Data {
		if h.Kind == "work_order" && h.Label == wo.Number {
			foundWO = true
		}
		if h.Kind == "activity" && h.Label == "maintenance_closed" {
			foundAct = true
		}
	}
	if !foundWO || !foundAct {
		t.Fatalf("asset history: wo=%v act=%v (%d rows)", foundWO, foundAct, len(hist.Data))
	}
	var insp struct {
		Number    string         `json:"number"`
		Extension map[string]any `json:"extension"`
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/inspections", map[string]any{"title": "Inspeksi AHU-03", "asset_id": asset.ID, "location_id": e.refs.MechRoomA12, "inspection_type": "engineering"})
	e.mustJSON(st, body, 201, &insp)
	if !strings.HasPrefix(insp.Number, "TSK-") || insp.Extension == nil || !strings.HasPrefix(fmt.Sprint(insp.Extension["inspection_number"]), "INS-") {
		t.Fatalf("inspection: %+v", insp)
	}
}

func mustLoc() *time.Location {
	l, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.UTC
	}
	return l
}
