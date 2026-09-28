package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PRD P2 v2.1 §5.5–§5.6 Engineering: dokumen equipment & warranty (P2-DOC-01..03), equipment health (P2-EQH-02..04),
// Asset 360 biaya/parts/vendor/PM compliance (P2-AST-04..06), inspeksi engineering terjadwal (P2-INS-02).
// Lihat docs/p2-v21-readiness.md & ADR-017.

type assetDoc struct {
	ID           uuid.UUID  `json:"id"`
	DocumentCode string     `json:"document_code"`
	Status       string     `json:"status"`
	DaysToExpire *int       `json:"days_to_expire"`
	AttachmentID *uuid.UUID `json:"attachment_id"`
	Version      int        `json:"version"`
}

type healthView struct {
	Score   *int   `json:"score"`
	Status  string `json:"status"`
	Factors []struct {
		Code   string `json:"code"`
		Points int    `json:"points"`
		Link   string `json:"link"`
	} `json:"factors"`
}

func (h healthView) has(code string) bool {
	for _, f := range h.Factors {
		if f.Code == code {
			return true
		}
	}
	return false
}

// doIfMatch: request dengan header If-Match (optimistic locking, NC §76).
func (e *env) doIfMatch(token, method, path string, payload any, version int) (int, []byte) {
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-Match", fmt.Sprintf(`"%d"`, version))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func ahuEquipment(t *testing.T, e *env, token string) uuid.UUID {
	t.Helper()
	var eq struct {
		Data []struct {
			ID       uuid.UUID `json:"id"`
			TypeName *string   `json:"type_name"`
		} `json:"data"`
	}
	st, body := e.do(token, http.MethodGet, "/api/v1/equipment?q=AHU", nil)
	e.mustJSON(st, body, 200, &eq)
	for _, x := range eq.Data {
		if x.TypeName != nil && *x.TypeName == "AHU" {
			return x.ID
		}
	}
	t.Fatal("equipment AHU tidak ada di seed")
	return uuid.Nil
}

func TestP2v21AssetDocumentsAndHealth(t *testing.T) {
	e := setup(t)
	engMgr := e.login("eng.manager@demo.buildingvision.id")
	engSpv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	loc := mustLoc()
	day := func(offset int) string { return time.Now().In(loc).AddDate(0, 0, offset).Format("2006-01-02") }

	warranty := time.Now().AddDate(0, 0, 5)
	var asset struct {
		ID           uuid.UUID `json:"id"`
		HealthStatus string    `json:"health_status"`
	}
	st, body := e.do(engMgr, http.MethodPost, "/api/v1/assets", map[string]any{"name": "AHU-21", "equipment_id": ahuEquipment(t, e, engMgr), "location_id": e.refs.MechRoomA12, "criticality": "high", "warranty_until": warranty})
	e.mustJSON(st, body, 201, &asset)
	if asset.HealthStatus != "unknown" {
		t.Fatalf("asset baru: health unknown, got %s", asset.HealthStatus)
	}

	// ---- Dokumen equipment (P2-DOC-01) ----
	aid := asset.ID.String()
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/assets/"+aid+"/documents", map[string]any{"document_type": "manual", "title": "Manual"}); st != 403 {
		t.Fatalf("teknisi tidak boleh menambah dokumen: %d", st)
	}
	if st, _ := e.do(engSpv, http.MethodPost, "/api/v1/assets/"+aid+"/documents", map[string]any{"document_type": "brosur", "title": "X"}); st != 400 {
		t.Fatalf("document_type tidak valid harus 400: %d", st)
	}
	if st, _ := e.do(engSpv, http.MethodPost, "/api/v1/assets/"+aid+"/documents", map[string]any{"document_type": "permit", "title": "X", "issued_on": day(0), "expires_on": day(-10)}); st != 400 {
		t.Fatalf("expires_on sebelum issued_on harus 400: %d", st)
	}
	// file diunggah ke asset lalu dipindah ke dokumen (object_type asset_document)
	var pre struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
		StorageKey   string    `json:"storage_key"`
	}
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "asset", "object_id": asset.ID, "attachment_type": "document", "content_type": "application/pdf", "size_bytes": 64, "original_filename": "sertifikat-k3.pdf"})
	e.mustJSON(st, body, 201, &pre)
	_ = e.store.Put(context.Background(), pre.StorageKey, "application/pdf", bytes.NewReader(append([]byte("%PDF-1.4\n"), make([]byte, 55)...)), 64)
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	var cert, permit, manual assetDoc
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/assets/"+aid+"/documents", map[string]any{"document_type": "certificate", "title": "Sertifikat K3 AHU", "document_number": "K3/2026/01", "issuer": "Disnaker", "issued_on": day(-355), "expires_on": day(10), "attachment_id": pre.AttachmentID})
	e.mustJSON(st, body, 201, &cert)
	if cert.Status != "expiring" || cert.DaysToExpire == nil || cert.AttachmentID == nil || cert.DocumentCode == "" {
		t.Fatalf("dokumen sertifikat: %s", body)
	}
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/assets/"+aid+"/documents", map[string]any{"document_type": "permit", "title": "Izin operasi", "expires_on": day(-1)})
	e.mustJSON(st, body, 201, &permit)
	if permit.Status != "expired" {
		t.Fatalf("izin kedaluwarsa: %s", body)
	}
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/assets/"+aid+"/documents", map[string]any{"document_type": "manual", "title": "Manual operasi"})
	e.mustJSON(st, body, 201, &manual)
	if manual.Status != "no_expiry" {
		t.Fatalf("manual tanpa kedaluwarsa: %s", body)
	}
	// file dokumen dapat dibuka teknisi (view), validasi akses lewat registry object asset_document
	var atts struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/attachments?object_type=asset_document&object_id="+cert.ID.String(), nil)
	e.mustJSON(st, body, 200, &atts)
	if len(atts.Data) != 1 || atts.Data[0].ID != pre.AttachmentID {
		t.Fatalf("file dokumen: %s", body)
	}
	var docs struct {
		Data []assetDoc `json:"data"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/assets/"+aid+"/documents", nil)
	e.mustJSON(st, body, 200, &docs)
	if len(docs.Data) != 3 {
		t.Fatalf("daftar dokumen: %s", body)
	}
	// If-Match basi → 409 (NC §76 konsistensi)
	if st, _ := e.doIfMatch(engSpv, http.MethodPatch, "/api/v1/asset-documents/"+manual.ID.String(), map[string]any{"title": "Manual v2"}, manual.Version+5); st != 409 {
		t.Fatalf("If-Match basi harus 409: %d", st)
	}
	st, body = e.doIfMatch(engSpv, http.MethodPatch, "/api/v1/asset-documents/"+manual.ID.String(), map[string]any{"title": "Manual v2"}, manual.Version)
	e.mustJSON(st, body, 200, nil)

	var ast struct {
		DocumentCount     int `json:"document_count"`
		ExpiringDocuments int `json:"expiring_documents"`
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/assets/"+aid, nil)
	e.mustJSON(st, body, 200, &ast)
	if ast.DocumentCount != 3 || ast.ExpiringDocuments != 2 {
		t.Fatalf("ringkasan dokumen di asset: %s", body)
	}

	// ---- Kedaluwarsa + pengingat (P2-DOC-02) ----
	var exp struct {
		Data []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"data"`
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/asset-documents/expiring?days=30", nil)
	e.mustJSON(st, body, 200, &exp)
	kinds := map[string]int{}
	for _, x := range exp.Data {
		kinds[x.Kind]++
	}
	if kinds["document"] != 2 || kinds["warranty"] != 1 {
		t.Fatalf("dokumen & warranty kedaluwarsa: %s", body)
	}
	if n, err := e.app.Asset.DocumentSweep(context.Background(), e.refs.OrgID); err != nil || n != 3 {
		t.Fatalf("document sweep: n=%d err=%v", n, err)
	}
	if n, _ := e.app.Asset.DocumentSweep(context.Background(), e.refs.OrgID); n != 0 {
		t.Fatalf("pengingat tidak boleh berulang pada tahap yang sama: %d", n)
	}
	e.dispatch(t)
	ib := e.inboxOf(t, engSpv)
	if !hasType(ib, "document_expiring") || !hasType(ib, "document_expired") {
		t.Fatalf("Engineering Supervisor harus diingatkan (H-30/H-7/expired): %+v", ib.Data)
	}
	var attn struct {
		Data []struct {
			Category string `json:"category"`
			DeepLink string `json:"deep_link"`
		} `json:"data"`
	}
	st, body = e.do(engMgr, http.MethodGet, "/api/v1/overview/attention-required?domain=engineering&limit=50", nil)
	e.mustJSON(st, body, 200, &attn)
	docAttn := 0
	for _, it := range attn.Data {
		if it.Category == "document_expiring" {
			docAttn++
		}
	}
	if docAttn != 3 {
		t.Fatalf("attention document_expiring (2 dokumen + warranty): %s", body)
	}

	// ---- Equipment health (P2-EQH-02..04) ----
	var wo workItem
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "AHU-21 bocor", "asset_id": asset.ID, "priority": "critical", "assignee_user_id": e.refs.Users["technician"]})
	e.mustJSON(st, body, 201, &wo)
	var h healthView
	st, body = e.do(tech, http.MethodGet, "/api/v1/assets/"+aid+"/health", nil)
	e.mustJSON(st, body, 200, &h)
	if h.Score == nil || *h.Score != 75 || h.Status != "warning" || !h.has("open_corrective") || !h.has("critical_work_order") {
		t.Fatalf("health 100-10-15 = 75 warning: %s", body)
	}
	for _, f := range h.Factors {
		if f.Link == "" || f.Points >= 0 {
			t.Fatalf("faktor health harus dapat ditelusuri (link + pengurang): %s", body)
		}
	}
	var fnd struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/findings", map[string]any{"title": "Vibrasi tinggi AHU-21", "asset_id": asset.ID, "severity": "high", "finding_type": "inspection"})
	e.mustJSON(st, body, 201, &fnd)
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/assets/"+aid+"/health/recompute", nil); st != 403 {
		t.Fatalf("teknisi tidak boleh memaksa hitung ulang: %d", st)
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/assets/"+aid+"/health/recompute", nil)
	e.mustJSON(st, body, 200, &h)
	if *h.Score != 65 || h.Status != "critical" || !h.has("open_findings") {
		t.Fatalf("health 75-10 = 65 critical: %s", body)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, engSpv); !hasType(ib, "asset_health_critical") {
		t.Fatalf("health turun ke critical harus memberi tahu Engineering Supervisor: %+v", ib.Data)
	}
	var list struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/assets?at_risk=true", nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 1 || list.Data[0].ID != asset.ID {
		t.Fatalf("filter at_risk: %s", body)
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/assets?health_status=healthy", nil)
	e.mustJSON(st, body, 200, &list)
	for _, x := range list.Data {
		if x.ID == asset.ID {
			t.Fatalf("filter health_status=healthy tidak boleh memuat asset critical: %s", body)
		}
	}
	// perbaikan selesai → health diperbarui otomatis oleh hook (Roadmap §17 contoh 3)
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/start", nil)
	e.mustJSON(st, body, 200, &wo)
	st, body = e.do(tech, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "work_order", "object_id": wo.ID, "attachment_type": "photo_after", "content_type": "image/jpeg", "size_bytes": 100})
	e.mustJSON(st, body, 201, &pre)
	_ = e.store.Put(context.Background(), pre.StorageKey, "image/jpeg", bytes.NewReader(make([]byte, 100)), 100)
	st, body = e.do(tech, http.MethodPost, "/api/v1/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/complete", map[string]any{"completion_notes": "Seal diganti"})
	e.mustJSON(st, body, 200, &wo)
	var after struct {
		HealthScore  *int   `json:"health_score"`
		HealthStatus string `json:"health_status"`
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/assets/"+aid, nil)
	e.mustJSON(st, body, 200, &after)
	if after.HealthScore == nil || *after.HealthScore != 90 || after.HealthStatus != "healthy" {
		t.Fatalf("health setelah WO selesai (hanya finding terbuka -10): %s", body)
	}
	var hist struct {
		Data []struct {
			Kind  string `json:"kind"`
			Label string `json:"label"`
		} `json:"data"`
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/assets/"+aid+"/history?limit=100", nil)
	e.mustJSON(st, body, 200, &hist)
	changed := 0
	for _, x := range hist.Data {
		if x.Kind == "activity" && x.Label == "health_changed" {
			changed++
		}
	}
	if changed < 3 {
		t.Fatalf("riwayat asset harus mencatat perubahan health (unknown→warning→critical→healthy): %s", body)
	}

	// ---- Asset 360: biaya, parts, vendor, PM compliance (P2-AST-04..06) ----
	var ins struct {
		Total struct {
			WorkOrders int `json:"work_orders"`
		} `json:"total"`
		ByType []struct {
			Key string `json:"key"`
		} `json:"by_type"`
		ByMonth []struct {
			Key string `json:"key"`
		} `json:"by_month"`
		PM struct {
			Scheduled int `json:"scheduled"`
		} `json:"pm"`
		Health *healthView `json:"health"`
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/assets/"+aid+"/insight", nil)
	e.mustJSON(st, body, 200, &ins)
	if ins.Total.WorkOrders != 1 || len(ins.ByType) != 1 || ins.ByType[0].Key != "corrective" || len(ins.ByMonth) != 1 || ins.Health == nil {
		t.Fatalf("insight asset: %s", body)
	}
	if st, _ := e.do(engSpv, http.MethodGet, "/api/v1/assets/"+aid+"/insight?from=kemarin", nil); st != 400 {
		t.Fatalf("from tidak valid harus 400: %d", st)
	}
}

func TestP2v21ScheduledInspection(t *testing.T) {
	e := setup(t)
	engMgr := e.login("eng.manager@demo.buildingvision.id")
	engSpv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	today := time.Now().In(mustLoc()).Format("2006-01-02")

	var asset struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(engMgr, http.MethodPost, "/api/v1/assets", map[string]any{"name": "AHU-22", "equipment_id": ahuEquipment(t, e, engMgr), "location_id": e.refs.MechRoomA12, "criticality": "medium"})
	e.mustJSON(st, body, 201, &asset)
	if st, _ := e.do(engMgr, http.MethodPost, "/api/v1/maintenance-plans", map[string]any{"name": "X", "asset_id": asset.ID, "frequency": "daily", "start_date": today, "output_type": "laporan"}); st != 400 {
		t.Fatalf("output_type tidak valid harus 400: %d", st)
	}
	var plan struct {
		ID         uuid.UUID `json:"id"`
		OutputType string    `json:"output_type"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/maintenance-plans", map[string]any{"name": "Inspeksi Harian AHU", "asset_id": asset.ID, "frequency": "daily", "start_date": today, "default_priority": "medium",
		"responsible_team_id": e.refs.Teams["engineering"], "lead_time_days": 0, "output_type": "inspection"})
	e.mustJSON(st, body, 201, &plan)
	if plan.OutputType != "inspection" {
		t.Fatalf("plan inspeksi: %s", body)
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/maintenance-plans/"+plan.ID.String()+"/publish", nil)
	e.mustJSON(st, body, 200, nil)
	var run struct {
		Created int `json:"created"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/maintenance-schedules/run-due", nil)
	e.mustJSON(st, body, 200, &run)
	if run.Created != 1 {
		t.Fatalf("P2-INS-02: inspeksi terjadwal dibuat %d, want 1", run.Created)
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/maintenance-schedules/run-due", nil)
	e.mustJSON(st, body, 200, &run)
	if run.Created != 0 {
		t.Fatalf("run-due kedua harus idempotent: %d", run.Created)
	}
	type schedRow struct {
		ID          uuid.UUID  `json:"id"`
		Status      string     `json:"status"`
		OutputType  string     `json:"output_type"`
		WorkOrderID *uuid.UUID `json:"work_order_id"`
		TaskID      *uuid.UUID `json:"task_id"`
		TaskNumber  *string    `json:"task_number"`
	}
	var sch struct {
		Data []schedRow `json:"data"`
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/maintenance-schedules?plan_id="+plan.ID.String()+"&status=due,overdue,in_progress", nil)
	e.mustJSON(st, body, 200, &sch)
	if len(sch.Data) != 1 || sch.Data[0].TaskID == nil || sch.Data[0].WorkOrderID != nil || sch.Data[0].OutputType != "inspection" || sch.Data[0].TaskNumber == nil {
		t.Fatalf("schedule inspeksi harus menunjuk task (bukan WO): %s", body)
	}
	taskID := *sch.Data[0].TaskID
	var task workItem
	st, body = e.do(tech, http.MethodGet, "/api/v1/tasks/"+taskID.String(), nil)
	e.mustJSON(st, body, 200, &task)
	if task.Type != "inspection" || task.Status != "assigned" {
		t.Fatalf("task inspeksi terjadwal (team engineering): %s", body)
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/tasks/"+taskID.String()+"/start", nil)
	e.mustJSON(st, body, 200, &task)
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/maintenance-schedules?plan_id="+plan.ID.String()+"&status=in_progress", nil)
	e.mustJSON(st, body, 200, &sch)
	if len(sch.Data) != 1 {
		t.Fatalf("schedule mengikuti task: in_progress: %s", body)
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/tasks/"+taskID.String()+"/complete", map[string]any{"completion_notes": "Normal"})
	e.mustJSON(st, body, 200, &task)
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/maintenance-schedules?plan_id="+plan.ID.String()+"&status=completed", nil)
	e.mustJSON(st, body, 200, &sch)
	if len(sch.Data) != 1 || sch.Data[0].ID == uuid.Nil {
		t.Fatalf("schedule completed setelah inspeksi selesai: %s", body)
	}
	var result string
	var score *int
	err := e.app.DB.WithOrgTx(context.Background(), e.refs.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT result, score FROM inspections WHERE task_id = $1`, taskID).Scan(&result, &score)
	})
	if err != nil || result != "pass" || score == nil || *score != 100 {
		t.Fatalf("hasil inspeksi terjadwal: result=%s score=%v err=%v", result, score, err)
	}
	// PM compliance di Asset 360 menghitung jadwal inspeksi yang selesai tepat waktu
	var ins struct {
		PM struct {
			Scheduled       int `json:"scheduled"`
			CompletedOnTime int `json:"completed_on_time"`
		} `json:"pm"`
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/assets/"+asset.ID.String()+"/insight", nil)
	e.mustJSON(st, body, 200, &ins)
	if ins.PM.CompletedOnTime < 1 {
		t.Fatalf("PM compliance: %s", body)
	}
	// skip jadwal inspeksi berikutnya → task dibatalkan, schedule skipped
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/maintenance-schedules?plan_id="+plan.ID.String()+"&status=scheduled&limit=1", nil)
	e.mustJSON(st, body, 200, &sch)
	if len(sch.Data) != 1 {
		t.Fatalf("schedule berikutnya: %s", body)
	}
	if st, body := e.do(engSpv, http.MethodPost, "/api/v1/maintenance-schedules/"+sch.Data[0].ID.String()+"/skip", map[string]any{"reason": "Libur"}); st != 200 && st != 204 {
		t.Fatalf("skip schedule: %d %s", st, body)
	}
}

// P2-PAT-08: temuan patroli terhubung ke checkpoint.
func TestP2v21FindingCheckpoint(t *testing.T) {
	e := setup(t)
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	officer := e.login("wawan@demo.buildingvision.id")
	var cp struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(secSpv, http.MethodPost, "/api/v1/checkpoints", map[string]any{"name": "CP-21 Lobby", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, &cp)
	var f struct {
		ID             uuid.UUID  `json:"id"`
		CheckpointID   *uuid.UUID `json:"checkpoint_id"`
		CheckpointName *string    `json:"checkpoint_name"`
		Location       struct {
			ID *uuid.UUID `json:"id"`
		} `json:"location"`
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/findings", map[string]any{"title": "Pintu darurat terganjal", "finding_type": "patrol", "severity": "medium", "checkpoint_id": cp.ID, "property_id": e.refs.PropertyID})
	e.mustJSON(st, body, 201, &f)
	if f.CheckpointID == nil || *f.CheckpointID != cp.ID || f.CheckpointName == nil || f.Location.ID == nil || *f.Location.ID != e.refs.LobbyA {
		t.Fatalf("finding checkpoint (lokasi default = lokasi checkpoint): %s", body)
	}
	var list struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/findings?checkpoint_id="+cp.ID.String(), nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 1 || list.Data[0].ID != f.ID {
		t.Fatalf("filter finding per checkpoint: %s", body)
	}
	if st, _ := e.do(officer, http.MethodPost, "/api/v1/findings", map[string]any{"title": "X", "checkpoint_id": uuid.New(), "property_id": e.refs.PropertyID}); st != 400 {
		t.Fatalf("checkpoint tidak dikenal harus 400: %d", st)
	}
	// offline: attach_photo lalu add_finding dengan client_attachment_id → foto dipindah ke finding
	var task workItem
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/tasks", map[string]any{"task_type": "general", "title": "Cek pintu darurat", "location_id": e.refs.LobbyA, "assignee_user_id": e.refs.Users["security_officer"]})
	e.mustJSON(st, body, 201, &task)
	var res struct {
		Results []struct {
			Status   string         `json:"status"`
			Response map[string]any `json:"response"`
		} `json:"results"`
	}
	now := time.Now()
	st, body = e.do(officer, http.MethodPost, "/api/v1/sync/mutations", map[string]any{"device_id": "dev-fnd", "mutations": []map[string]any{
		mut(uuid.New(), "task", task.ID, "attach_photo", 1, map[string]any{"client_attachment_id": "fnd-photo-1", "attachment_type": "photo", "content_type": "image/jpeg", "size_bytes": 1000, "gps_status": "denied"}, now),
		mut(uuid.New(), "task", task.ID, "add_finding", 2, map[string]any{"title": "Engsel pintu darurat patah", "severity": "medium", "finding_type": "patrol", "checkpoint_id": cp.ID, "client_attachment_id": "fnd-photo-1"}, now),
	}})
	e.mustJSON(st, body, 200, &res)
	if len(res.Results) != 2 || res.Results[1].Status != "applied" || res.Results[1].Response["finding_id"] == nil {
		t.Fatalf("add_finding offline: %s", body)
	}
	var fo struct {
		AttachmentCount int        `json:"attachment_count"`
		CheckpointID    *uuid.UUID `json:"checkpoint_id"`
	}
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/findings/"+res.Results[1].Response["finding_id"].(string), nil)
	e.mustJSON(st, body, 200, &fo)
	if fo.AttachmentCount != 1 || fo.CheckpointID == nil || *fo.CheckpointID != cp.ID {
		t.Fatalf("foto offline harus pindah ke finding + checkpoint tersimpan: %s", body)
	}
}
