package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/platform/jobs"
)

// ---------- Task lifecycle, WO foundation, checklist, evidence, notification, search, export (§10–§18, US-P0-005/007) ----------

func TestP0v2TaskFoundation(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	spv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	techID := e.refs.Users["technician"]

	// ---- Checklist foundation (§12): Pass/Fail, Selection (expected), Numeric range, Signature ----
	var tpl struct {
		ID       uuid.UUID `json:"id"`
		Category *string   `json:"category"`
		Items    []struct {
			ItemType      string  `json:"item_type"`
			ExpectedValue *string `json:"expected_value"`
			Options       []struct {
				Value string `json:"value"`
			} `json:"options"`
		} `json:"items"`
	}
	st, body := e.do(admin, http.MethodPost, "/api/v1/checklist-templates", map[string]any{"name": "Inspeksi Pompa", "domain": "engineering", "category": "Pump", "applies_to": []string{"task", "work_order"},
		"items": []map[string]any{
			{"label": "Tekanan normal", "item_type": "pass_fail", "is_required": true},
			{"label": "Kondisi area", "item_type": "selection", "is_required": true, "options": []map[string]any{{"value": "clean", "label": "Bersih"}, {"value": "dirty", "label": "Kotor"}}, "expected_value": "clean"},
			{"label": "Suhu bearing", "item_type": "numeric", "numeric_unit": "°C", "numeric_min": 20, "numeric_max": 70},
			{"label": "Tanda tangan teknisi", "item_type": "signature"},
			{"label": "Catatan", "item_type": "text", "help_text": "Deskripsi temuan"},
		}})
	e.mustJSON(st, body, 201, &tpl)
	if tpl.Category == nil || *tpl.Category != "Pump" || len(tpl.Items) != 5 || tpl.Items[1].ExpectedValue == nil || len(tpl.Items[1].Options) != 2 {
		t.Fatalf("template: %s", body)
	}
	// validasi: selection < 2 opsi, expected bukan opsi
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/checklist-templates", map[string]any{"name": "Bad", "items": []map[string]any{{"label": "x", "item_type": "selection", "options": []map[string]any{{"value": "a"}}}}}); st != 400 {
		t.Fatalf("selection 1 opsi harus 400, got %d", st)
	}
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/checklist-templates", map[string]any{"name": "Bad2", "items": []map[string]any{{"label": "x", "item_type": "pass_fail", "expected_value": "maybe"}}}); st != 400 {
		t.Fatalf("expected pass_fail tidak valid harus 400, got %d", st)
	}
	if st, body := e.do(admin, http.MethodPost, "/api/v1/checklist-templates/"+tpl.ID.String()+"/publish", nil); st != 200 {
		t.Fatalf("publish: %d %s", st, body)
	}

	// ---- US-P0-005: Task dibuat, di-assign, punya due date, checklist, evidence, activity ----
	var task workItem
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "Cek pompa transfer", "location_id": e.refs.MechRoomA12, "priority": "critical",
		"assignee_user_id": techID, "due_at": "2030-01-01T10:00:00Z", "checklist_template_id": tpl.ID})
	e.mustJSON(st, body, 201, &task)
	if task.Status != "assigned" || !has(task.Flags, "critical") {
		t.Fatalf("task awal: %s", body)
	}
	if st, body := e.do(tech, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/start", map[string]any{}); st != 200 {
		t.Fatalf("start: %d %s", st, body)
	}
	var runs struct {
		Data []struct {
			Items []struct {
				ID          uuid.UUID `json:"id"`
				ItemType    string    `json:"item_type"`
				IsDeviation *bool     `json:"is_deviation"`
				FindingID   *string   `json:"finding_id"`
			} `json:"items"`
		} `json:"data"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/tasks/"+task.ID.String()+"/checklist-runs", nil)
	e.mustJSON(st, body, 200, &runs)
	if len(runs.Data) != 1 || len(runs.Data[0].Items) != 5 {
		t.Fatalf("checklist run: %s", body)
	}
	items := runs.Data[0].Items
	answer := func(itemID uuid.UUID, in map[string]any) (int, []byte) {
		return e.do(tech, http.MethodPost, "/api/v1/checklist-run-items/"+itemID.String()+"/answer", in)
	}
	if st, _ := answer(items[0].ID, map[string]any{"result_value": "ok"}); st != 400 {
		t.Fatalf("pass_fail menolak nilai lain, got %d", st)
	}
	if st, body := answer(items[0].ID, map[string]any{"result_value": "fail", "note": "tekanan drop"}); st != 200 {
		t.Fatalf("answer fail: %d %s", st, body)
	}
	if st, _ := answer(items[1].ID, map[string]any{"result_value": "wet"}); st != 400 {
		t.Fatalf("selection menolak nilai di luar opsi, got %d", st)
	}
	if st, body := answer(items[1].ID, map[string]any{"result_value": "dirty"}); st != 200 {
		t.Fatalf("answer selection: %d %s", st, body)
	}
	if st, body := answer(items[2].ID, map[string]any{"result_number": 85}); st != 200 {
		t.Fatalf("answer numeric: %d %s", st, body)
	}
	// signature: attachment bertipe signature wajib; foto biasa ditolak
	photo := e.uploadAttachment(tech, "task", task.ID, "photo", "image/png")
	if st, _ := answer(items[3].ID, map[string]any{"attachment_id": photo}); st != 400 {
		t.Fatalf("signature dengan foto biasa harus 400, got %d", st)
	}
	sig := e.uploadAttachment(tech, "task", task.ID, "signature", "image/png")
	if st, body := answer(items[3].ID, map[string]any{"attachment_id": sig}); st != 200 {
		t.Fatalf("answer signature: %d %s", st, body)
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/tasks/"+task.ID.String()+"/checklist-runs", nil)
	e.mustJSON(st, body, 200, &runs)
	dev := map[string]bool{}
	findings := 0
	for _, it := range runs.Data[0].Items {
		if it.IsDeviation != nil && *it.IsDeviation {
			dev[it.ItemType] = true
		}
		if it.FindingID != nil {
			findings++
		}
	}
	if !dev["pass_fail"] || !dev["selection"] || !dev["numeric"] || dev["signature"] {
		t.Fatalf("evaluasi expected result: %v", dev)
	}
	if findings != 3 {
		t.Fatalf("deviasi → finding otomatis: %d, want 3", findings)
	}
	if st, body := e.do(tech, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/complete", map[string]any{"completion_notes": "selesai"}); st != 200 {
		t.Fatalf("complete: %d %s", st, body)
	}
	// Reopened (§10.4): permission reopen, reopen_count, flag, activity
	var re struct {
		Status      string   `json:"status"`
		Flags       []string `json:"flags"`
		ReopenCount *int     `json:"reopen_count"`
	}
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/reopen", map[string]any{"reason": "x"}); st != 403 {
		t.Fatalf("technician tanpa operations.tasks.reopen harus 403, got %d", st)
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/reopen", map[string]any{"reason": "tekanan masih drop"})
	e.mustJSON(st, body, 200, &re)
	if re.Status != "in_progress" || re.ReopenCount == nil || *re.ReopenCount != 1 || !has(re.Flags, "reopened") {
		t.Fatalf("reopen: %s", body)
	}
	var acts struct {
		Data []struct {
			Action string `json:"action"`
		} `json:"data"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/tasks/"+task.ID.String()+"/activities", nil)
	e.mustJSON(st, body, 200, &acts)
	seen := map[string]bool{}
	for _, a := range acts.Data {
		seen[a.Action] = true
	}
	for _, want := range []string{"created", "assigned", "status_changed", "checklist_item_answered", "attachment_added", "reopened"} {
		if !seen[want] {
			t.Fatalf("activity %s tidak ada: %v", want, seen)
		}
	}

	// ---- WO dikaitkan dengan Task (§11) + equipment & notes ----
	var wo struct {
		ID     uuid.UUID `json:"id"`
		Number string    `json:"number"`
		Notes  *string   `json:"notes"`
		Links  []struct {
			ObjectType string `json:"object_type"`
			ObjectID   string `json:"object_id"`
		} `json:"links"`
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/work-orders", map[string]any{"title": "Ganti seal pompa", "notes": "butuh seal 2 inch"})
	e.mustJSON(st, body, 201, &wo)
	linked := false
	for _, l := range wo.Links {
		if l.ObjectType == "task" && l.ObjectID == task.ID.String() {
			linked = true
		}
	}
	if !linked || wo.Notes == nil || !strings.HasPrefix(wo.Number, "WO-") {
		t.Fatalf("WO dari task: %s", body)
	}

	// ---- Delete draft (§8.3): hanya draft tanpa aktivitas; yang berjalan harus Cancel ----
	var draft workItem
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "Salah input", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, &draft)
	if !has(draft.AllowedActions, "delete") {
		t.Fatalf("draft task harus punya aksi delete: %v", draft.AllowedActions)
	}
	if st, _ := e.do(spv, http.MethodDelete, "/api/v1/tasks/"+draft.ID.String(), nil); st != 400 {
		t.Fatalf("delete tanpa reason harus 400, got %d", st)
	}
	if st, body := e.do(spv, http.MethodDelete, "/api/v1/tasks/"+draft.ID.String()+"?reason=salah+input", nil); st != 204 {
		t.Fatalf("delete draft: %d %s", st, body)
	}
	if st, _ := e.do(spv, http.MethodGet, "/api/v1/tasks/"+draft.ID.String(), nil); st != 404 {
		t.Fatalf("task terhapus harus 404, got %d", st)
	}
	if st, _ := e.do(spv, http.MethodDelete, "/api/v1/tasks/"+task.ID.String()+"?reason=x", nil); st != 409 {
		t.Fatalf("delete task berjalan harus 409, got %d", st)
	}

	// ---- Attachment (§13): dokumen didukung, magic content type divalidasi, confirm hanya pengunggah ----
	e.uploadAttachment(spv, "work_order", wo.ID, "document", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	if st, _ := e.do(spv, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "work_order", "object_id": wo.ID, "attachment_type": "photo", "content_type": "application/pdf", "size_bytes": 1000}); st != 400 {
		t.Fatalf("foto dengan content PDF harus 400, got %d", st)
	}
	if st, _ := e.do(spv, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "work_order", "object_id": wo.ID, "attachment_type": "document", "content_type": "application/x-msdownload", "size_bytes": 1000}); st != 400 {
		t.Fatalf("executable harus 400, got %d", st)
	}
	var pre struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
		StorageKey   string    `json:"storage_key"`
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": "work_order", "object_id": wo.ID, "attachment_type": "document", "content_type": "application/pdf", "size_bytes": 10})
	e.mustJSON(st, body, 201, &pre)
	_ = e.store.Put(context.Background(), pre.StorageKey, "application/pdf", strings.NewReader("%PDF-1.7"), 8)
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{}); st != 403 {
		t.Fatalf("confirm oleh bukan pengunggah harus 403, got %d", st)
	}

	// ---- Notification (§14, US-P0-007): system broadcast + channel email ----
	var bc struct {
		Recipients int `json:"recipients"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/notifications/broadcast", map[string]any{"title": "Pemeliharaan sistem", "body": "Sabtu 22:00", "property_id": e.refs.PropertyID, "role_codes": []string{"technician"}, "email": true})
	e.mustJSON(st, body, 202, &bc)
	if bc.Recipients != 2 {
		t.Fatalf("broadcast ke technician: %d", bc.Recipients)
	}
	ib := e.inboxOf(t, tech)
	found := false
	for _, n := range ib.Data {
		if n.Type == "system_broadcast" && n.Title == "Pemeliharaan sistem" {
			found = true
		}
	}
	if !found {
		t.Fatalf("broadcast tidak ada di inbox technician")
	}
	emailJobs := 0
	for _, j := range e.jobs.Jobs {
		if d, ok := j.(jobs.NotificationDeliverArgs); ok && d.Channel == "email" {
			emailJobs++
		}
	}
	if emailJobs < 2 {
		t.Fatalf("job email broadcast: %d", emailJobs)
	}
	var prefs struct {
		Data []struct {
			Type           string `json:"type"`
			Email          bool   `json:"email"`
			EmailAvailable bool   `json:"email_available"`
		} `json:"data"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/notifications/preferences", nil)
	if st != 200 {
		t.Fatalf("preferences: %d %s", st, body)
	}
	if err := json.Unmarshal(body, &prefs); err != nil || len(prefs.Data) == 0 {
		var arr []struct {
			Type           string `json:"type"`
			Email          bool   `json:"email"`
			EmailAvailable bool   `json:"email_available"`
		}
		_ = json.Unmarshal(body, &arr)
		prefs.Data = arr
	}
	critEmail := false
	for _, p := range prefs.Data {
		if p.Type == "work_order_overdue" && p.EmailAvailable && p.Email {
			critEmail = true
		}
	}
	if !critEmail {
		t.Fatalf("rule kritis (work_order_overdue) harus punya channel email default: %s", body)
	}

	// ---- Search (§17.1): User Name, Vendor Name, Floor, Finding ----
	var vnd struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/vendors", map[string]any{"name": "CV Pompa Jaya Abadi", "service_categories": []string{"plumbing"}})
	e.mustJSON(st, body, 201, &vnd)
	e.createUser(admin, "rahmat.hidayat@org-a.test", []map[string]any{{"role_id": e.roleID(admin, "staff"), "property_id": e.refs.PropertyID}}, map[string]any{"full_name": "Rahmat Hidayat"})
	_, _ = e.app.Search.Reindex(context.Background(), e.refs.OrgID)
	for _, c := range []struct{ q, typ string }{{"Rahmat", "user"}, {"Pompa Jaya", "vendor"}, {"Lantai 12", "floor"}, {"Tekanan normal", "finding"}} {
		var res struct {
			Data []struct {
				ObjectType string `json:"object_type"`
				DeepLink   string `json:"deep_link"`
			} `json:"data"`
		}
		st, body := e.do(admin, http.MethodGet, "/api/v1/search?q="+strings.ReplaceAll(c.q, " ", "+"), nil)
		e.mustJSON(st, body, 200, &res)
		ok := false
		for _, r := range res.Data {
			if r.ObjectType == c.typ && r.DeepLink != "" {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("search %q tidak menemukan tipe %s: %s", c.q, c.typ, body)
		}
	}
	// technician tanpa iam.users.view tidak melihat hasil user
	var res struct {
		Data []struct {
			ObjectType string `json:"object_type"`
		} `json:"data"`
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/search?q=Rahmat", nil)
	e.mustJSON(st, body, 200, &res)
	for _, r := range res.Data {
		if r.ObjectType == "user" {
			t.Fatalf("search bocor: technician melihat hasil user")
		}
	}

	// ---- Filter & sort (§17.2–§17.3) ----
	if st, _ := e.do(spv, http.MethodGet, "/api/v1/work-orders?sort=bogus", nil); st != 400 {
		t.Fatalf("sort tidak dikenal harus 400, got %d", st)
	}
	if st, _ := e.do(admin, http.MethodGet, "/api/v1/incidents?sort=-updated_at", nil); st != 200 {
		t.Fatalf("incidents sort updated_at: %d", st)
	}
	if st, _ := e.do(spv, http.MethodGet, "/api/v1/findings?sort=priority&floor_id="+e.refs.FloorA12.String(), nil); st != 200 {
		t.Fatalf("findings sort priority + floor filter: %d", st)
	}
	if st, _ := e.do(admin, http.MethodGet, "/api/v1/incidents?location_id=bukan-uuid", nil); st != 400 {
		t.Fatalf("filter uuid tidak valid harus 400, got %d", st)
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/tasks?building_id="+e.refs.TowerB.String(), nil)
	if st != 200 || containsID(listIDs(t, body), task.ID) {
		t.Fatalf("filter building (Tower B) tidak boleh memuat task Tower A: %d", st)
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/tasks?tower_id="+e.refs.TowerA.String(), nil)
	if st != 200 || !containsID(listIDs(t, body), task.ID) {
		t.Fatalf("filter tower A harus memuat task: %d %s", st, body)
	}
	// keyset pagination + sort
	var page1 struct {
		Data       []json.RawMessage `json:"data"`
		NextCursor *string           `json:"next_cursor"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/findings?limit=1&sort=-created_at", nil)
	e.mustJSON(st, body, 200, &page1)
	if len(page1.Data) != 1 || page1.NextCursor == nil {
		t.Fatalf("cursor findings: %s", body)
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/findings?limit=1&sort=-created_at&cursor="+*page1.NextCursor, nil)
	if st != 200 || strings.Contains(string(body), string(page1.Data[0])) {
		t.Fatalf("halaman 2 findings: %d", st)
	}

	// ---- Export (§18): resource baru + permission ----
	for _, res := range []string{"users", "locations", "tenants", "vendors", "audit_logs"} {
		if st, body := e.do(admin, http.MethodPost, "/api/v1/exports", map[string]any{"resource": res, "format": "csv"}); st != 202 {
			t.Fatalf("export %s: %d %s", res, st, body)
		}
	}
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/exports", map[string]any{"resource": "audit_logs", "format": "csv"}); st != 403 {
		t.Fatalf("technician export audit_logs harus 403, got %d", st)
	}
	// jalankan job export sinkron (meniru worker) → ready
	for _, j := range e.jobs.Jobs {
		if x, ok := j.(jobs.ExportGenerateArgs); ok {
			if err := e.app.Exports.Generate(context.Background(), x.OrganizationID, x.ExportID); err != nil {
				t.Fatalf("generate export: %v", err)
			}
		}
	}
	var mine struct {
		Data []struct {
			Resource string `json:"resource"`
			Status   string `json:"status"`
			RowCount *int   `json:"row_count"`
		} `json:"data"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/exports", nil)
	e.mustJSON(st, body, 200, &mine)
	ready := map[string]bool{}
	for _, x := range mine.Data {
		if x.Status == "ready" && x.RowCount != nil && *x.RowCount > 0 {
			ready[x.Resource] = true
		}
	}
	for _, res := range []string{"users", "locations", "tenants", "vendors", "audit_logs"} {
		if !ready[res] {
			t.Fatalf("export %s tidak ready: %s", res, body)
		}
	}
}

// uploadAttachment: presign → PUT (memory storage) → confirm; mengembalikan attachment id.
func (e *env) uploadAttachment(token, objectType string, objectID uuid.UUID, attType, contentType string) uuid.UUID {
	e.t.Helper()
	var pre struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
		StorageKey   string    `json:"storage_key"`
	}
	content := "\x89PNG\r\n\x1a\n0000"
	if !strings.HasPrefix(contentType, "image/") {
		content = "PK\x03\x04docx"
	}
	st, body := e.do(token, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": objectType, "object_id": objectID, "attachment_type": attType, "content_type": contentType, "size_bytes": len(content)})
	e.mustJSON(st, body, 201, &pre)
	if err := e.store.Put(context.Background(), pre.StorageKey, contentType, strings.NewReader(content), int64(len(content))); err != nil {
		e.t.Fatal(err)
	}
	st, body = e.do(token, http.MethodPost, "/api/v1/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	return pre.AttachmentID
}

func TestP0v2ServiceRequestAndAssetFilters(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	for _, c := range []struct {
		path string
		want int
	}{
		{"/api/v1/service-requests?sort=-updated_at", 200}, {"/api/v1/service-requests?sort=priority&floor_id=" + e.refs.FloorA12.String(), 200},
		{"/api/v1/service-requests?sort=bogus", 400}, {"/api/v1/service-requests?tenant_id=x", 400},
		{"/api/v1/assets?building_id=" + e.refs.TowerA.String(), 200}, {"/api/v1/assets?floor_id=zzz", 400},
	} {
		if st, body := e.do(admin, http.MethodGet, c.path, nil); st != c.want {
			t.Errorf("%s: %d, want %d %s", c.path, st, c.want, body)
		}
	}
}
