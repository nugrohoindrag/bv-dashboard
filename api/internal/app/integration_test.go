package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	migrations "github.com/buildingvision/api/db"
	"github.com/buildingvision/api/internal/app"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/config"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/storage"
	"github.com/buildingvision/api/internal/seed"
)

// Integration test: Postgres asli (TAD §5.5 AT-010 otomatis). Skip bila DB tidak tersedia.
// Env: BV_TEST_ADMIN_DATABASE_URL (owner) — default postgres lokal; app memakai role bv_app agar RLS aktif.

type env struct {
	t          *testing.T
	srv        *httptest.Server
	app        *app.App
	store      *storage.MemoryStorage
	jobs       *jobs.MemoryEnqueuer
	refs       *seed.DemoRefs
	orgB       uuid.UUID
	orgBAdmin  string
	tokens     map[string]string
	dispatched int
}

func adminURL() string {
	if v := os.Getenv("BV_TEST_ADMIN_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://postgres:postgres@localhost:5432/buildingvision_test?sslmode=disable"
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	aurl := adminURL()
	// buat database test bila belum ada
	base := aurl[:strings.LastIndex(aurl, "/")] + "/postgres?sslmode=disable"
	adminPool, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Skipf("postgres tidak tersedia: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		t.Skipf("postgres tidak tersedia: %v", err)
	}
	dbName := aurl[strings.LastIndex(aurl, "/")+1:]
	dbName = dbName[:strings.Index(dbName, "?")]
	_, _ = adminPool.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, dbName))
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, dbName)); err != nil {
		t.Fatalf("create db: %v", err)
	}
	adminPool.Close()
	if err := migrations.Up(ctx, aurl); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ownerDB, err := db.Open(ctx, aurl)
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Migrate(ctx, ownerDB.Pool); err != nil {
		t.Fatalf("river migrate: %v", err)
	}
	if err := seed.SeedGlobal(ctx, ownerDB); err != nil {
		t.Fatal(err)
	}
	iamSvc := iam.NewService(ownerDB, nil, 0)
	// Org A (demo)
	orgA, _, err := seed.EnsureOrganization(ctx, ownerDB, "Org A", "org-a")
	if err != nil {
		t.Fatal(err)
	}
	var refs *seed.DemoRefs
	if err := ownerDB.WithOrgTx(ctx, orgA, func(ctx context.Context, tx pgx.Tx) error {
		adminID, err := seed.SeedOrganization(ctx, tx, iamSvc, orgA, "admin@org-a.test", "Admin12345!")
		if err != nil {
			return err
		}
		refs, err = seed.SeedDemoRefs(ctx, tx, orgA, adminID)
		return err
	}); err != nil {
		t.Fatalf("seed org A: %v", err)
	}
	// Org B (minimal)
	orgB, _, err := seed.EnsureOrganization(ctx, ownerDB, "Org B", "org-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := ownerDB.WithOrgTx(ctx, orgB, func(ctx context.Context, tx pgx.Tx) error {
		_, err := seed.SeedOrganization(ctx, tx, iamSvc, orgB, "admin@org-b.test", "Admin12345!")
		return err
	}); err != nil {
		t.Fatalf("seed org B: %v", err)
	}
	ownerDB.Close()

	// App memakai bv_app (RLS berlaku)
	appURL := strings.Replace(aurl, "postgres:postgres@", "bv_app:bv_app_dev@", 1)
	appDB, err := db.Open(ctx, appURL)
	if err != nil {
		t.Fatalf("open as bv_app: %v", err)
	}
	t.Cleanup(appDB.Close)
	cfg, _ := config.Load()
	cfg.Env = "test"
	store := storage.NewMemory("http://storage.test")
	enq := &jobs.MemoryEnqueuer{}
	a, err := app.New(app.Options{Cfg: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), DB: appDB, Jobs: enq, Storage: store})
	if err != nil {
		t.Fatal(err)
	}
	a.IAM.SetLoginRateLimit(0)
	a.Use(app.DefaultExtensions(a)...)
	srv := httptest.NewServer(a.BuildRouter())
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, app: a, store: store, jobs: enq, refs: refs, orgB: orgB, orgBAdmin: "admin@org-b.test", tokens: map[string]string{}}
}

func (e *env) login(email string) string {
	if tok, ok := e.tokens[email]; ok {
		return tok
	}
	pass := "Demo12345!"
	if strings.HasPrefix(email, "admin@") {
		pass = "Admin12345!"
	}
	st, body := e.do("", http.MethodPost, "/api/v1/auth/login", map[string]any{"identifier": email, "password": pass, "client": "mobile"})
	if st != 200 {
		e.t.Fatalf("login %s: %d %s", email, st, body)
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(body, &resp)
	e.tokens[email] = resp.AccessToken
	return resp.AccessToken
}

func (e *env) do(token, method, path string, payload any) (int, []byte) {
	var rd io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (e *env) mustJSON(st int, body []byte, want int, v any) {
	e.t.Helper()
	if st != want {
		e.t.Fatalf("status %d, want %d: %s", st, want, body)
	}
	if v != nil {
		if err := json.Unmarshal(body, v); err != nil {
			e.t.Fatalf("json: %v: %s", err, body)
		}
	}
}

type workItem struct {
	ID             uuid.UUID `json:"id"`
	Number         string    `json:"number"`
	Status         string    `json:"status"`
	AllowedActions []string  `json:"allowed_actions"`
	Version        int       `json:"version"`
	Flags          []string  `json:"flags"`
	Assignee       struct {
		UserID *uuid.UUID `json:"user_id"`
	} `json:"assignee"`
	SLA *struct {
		ResolutionDueAt *time.Time `json:"resolution_due_at"`
	} `json:"sla"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Extension map[string]any `json:"extension"`
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestP0CoreWorkflows(t *testing.T) {
	e := setup(t)
	spv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	budiID := e.refs.Users["technician"]

	// ---------- AT-001: Create Work Order ----------
	var wo workItem
	st, body := e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{
		"work_order_type": "corrective", "title": "Perbaikan AHU-03 tidak dingin", "location_id": e.refs.MechRoomA12,
		"priority": "high", "assignee_user_id": budiID,
	})
	e.mustJSON(st, body, 201, &wo)
	if !strings.HasPrefix(wo.Number, "WO-") || !strings.HasSuffix(wo.Number, "-000001") {
		t.Fatalf("AT-001: nomor WO %q", wo.Number)
	}
	if wo.Status != "assigned" {
		t.Fatalf("AT-001: status awal %s, want assigned", wo.Status)
	}
	if wo.SLA == nil || wo.SLA.ResolutionDueAt == nil {
		t.Fatalf("AT-001: SLA tidak terhitung")
	}
	// activity history tercatat
	var acts struct {
		Data []struct {
			Action string `json:"action"`
		} `json:"data"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+wo.ID.String()+"/activities", nil)
	e.mustJSON(st, body, 200, &acts)
	seen := map[string]bool{}
	for _, a := range acts.Data {
		seen[a.Action] = true
	}
	if !seen["created"] || !seen["assigned"] {
		t.Fatalf("AT-001: activity created/assigned tidak ada: %v", seen)
	}
	// WO tanpa lokasi ditolak (FR-WO-003)
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "repair", "title": "x", "property_id": e.refs.PropertyID})
	if st != 400 {
		t.Fatalf("FR-WO-003: WO tanpa lokasi harus 400, got %d %s", st, body)
	}
	// nomor kedua berurutan
	var wo2 workItem
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "repair", "title": "Lampu lobby mati", "location_id": e.refs.LobbyA, "priority": "low"})
	e.mustJSON(st, body, 201, &wo2)
	if !strings.HasSuffix(wo2.Number, "-000002") || wo2.Status != "new" {
		t.Fatalf("business id/status WO kedua: %s %s", wo2.Number, wo2.Status)
	}

	// ---------- RBAC negative: technician tidak boleh close, tidak boleh buat user ----------
	st, _ = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/close", nil)
	if st != 403 && st != 409 {
		t.Fatalf("RBAC: technician close harus ditolak, got %d", st)
	}
	st, _ = e.do(tech, http.MethodPost, "/api/v1/users", map[string]any{"full_name": "X", "email": "x@x.test", "password": "Password123"})
	if st != 403 {
		t.Fatalf("RBAC: technician create user harus 403, got %d", st)
	}
	// user lain (joko) yang bukan assignee tidak boleh start
	joko := e.login("joko@demo.buildingvision.id")
	st, _ = e.do(joko, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/start", nil)
	if st != 403 {
		t.Fatalf("guard IsAssignee: non-assignee start harus 403, got %d", st)
	}

	// ---------- AT-002: Technician Start → Checklist → Photo → Complete ----------
	// engineering manager buat checklist template & publish (Settings → Checklists), supervisor pasang ke WO
	engMgr := e.login("eng.manager@demo.buildingvision.id")
	var tpl struct {
		ID    uuid.UUID `json:"id"`
		Items []struct {
			ID uuid.UUID `json:"id"`
		} `json:"items"`
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/checklist-templates", map[string]any{
		"name": "Corrective AHU", "applies_to": []string{"work_order"},
		"items": []map[string]any{
			{"label": "Filter bersih", "item_type": "ok_notok_na", "is_required": true},
			{"label": "Suhu keluaran (°C)", "item_type": "numeric", "is_required": true, "numeric_unit": "°C", "numeric_min": 12, "numeric_max": 18},
			{"label": "Catatan", "item_type": "text", "is_required": false},
		},
	})
	e.mustJSON(st, body, 201, &tpl)
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/checklist-templates/"+tpl.ID.String()+"/publish", nil)
	e.mustJSON(st, body, 200, nil)
	var run struct {
		ID    uuid.UUID `json:"id"`
		Items []struct {
			ID       uuid.UUID `json:"id"`
			ItemType string    `json:"item_type"`
		} `json:"items"`
		Status string `json:"status"`
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/checklist-runs", map[string]any{"template_id": tpl.ID})
	e.mustJSON(st, body, 201, &run)
	if len(run.Items) != 3 {
		t.Fatalf("checklist run items %d", len(run.Items))
	}
	// complete sebelum start → invalid transition
	st, _ = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/complete", nil)
	if st != 409 {
		t.Fatalf("complete dari assigned harus 409, got %d", st)
	}
	// start
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/start", map[string]any{"gps_status": "captured", "gps_lat": -6.2, "gps_lng": 106.8})
	e.mustJSON(st, body, 200, &wo)
	if wo.Status != "in_progress" {
		t.Fatalf("AT-002: status setelah start %s", wo.Status)
	}
	// complete tanpa checklist & foto → guard gagal
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/complete", nil)
	if st != 409 {
		t.Fatalf("complete tanpa evidence harus 409, got %d %s", st, body)
	}
	// isi checklist
	for _, it := range run.Items {
		var ans map[string]any
		switch it.ItemType {
		case "ok_notok_na":
			ans = map[string]any{"result_value": "ok"}
		case "numeric":
			ans = map[string]any{"result_number": 16.5}
		case "text":
			ans = map[string]any{"result_text": "Filter diganti"}
		}
		st, body = e.do(tech, http.MethodPost, "/api/v1/checklist-run-items/"+it.ID.String()+"/answer", ans)
		e.mustJSON(st, body, 200, &run)
	}
	if run.Status != "completed" {
		t.Fatalf("checklist run status %s", run.Status)
	}
	// complete masih gagal: After Photo wajib (PRD §11.2)
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/complete", nil)
	if st != 409 || !strings.Contains(string(body), "After Photo") {
		t.Fatalf("complete tanpa after photo harus 409 'After Photo': %d %s", st, body)
	}
	// presign → (simulasi PUT) → confirm
	var pre struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
		StorageKey   string    `json:"storage_key"`
		UploadURL    string    `json:"upload_url"`
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/attachments/presign", map[string]any{
		"object_type": "work_order", "object_id": wo.ID, "attachment_type": "photo_after", "content_type": "image/jpeg", "size_bytes": 12345, "client_attachment_id": "cli-1",
	})
	e.mustJSON(st, body, 201, &pre)
	if pre.UploadURL == "" {
		t.Fatal("presign tanpa upload_url")
	}
	_ = e.store.Put(context.Background(), pre.StorageKey, "image/jpeg", bytes.NewReader(make([]byte, 12345)), 12345)
	var att struct {
		Status    string `json:"status"`
		GPSStatus string `json:"gps_status"`
		URL       string `json:"url"`
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{"gps_status": "denied", "captured_at": time.Now()})
	e.mustJSON(st, body, 200, &att)
	if att.Status != "ready" || att.GPSStatus != "denied" || att.URL == "" {
		t.Fatalf("attachment confirm: %+v", att)
	}
	// presign idempotent (client_attachment_id sama)
	var pre2 struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/attachments/presign", map[string]any{
		"object_type": "work_order", "object_id": wo.ID, "attachment_type": "photo_after", "content_type": "image/jpeg", "size_bytes": 12345, "client_attachment_id": "cli-1",
	})
	e.mustJSON(st, body, 201, &pre2)
	if pre2.AttachmentID != pre.AttachmentID {
		t.Fatal("presign tidak idempotent")
	}
	// comment
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/comments", map[string]any{"body": "Filter sudah diganti, suhu normal"})
	e.mustJSON(st, body, 201, nil)
	// complete
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/complete", map[string]any{"completion_notes": "Selesai", "resolution": "Ganti filter"})
	e.mustJSON(st, body, 200, &wo)
	if wo.Status != "completed" {
		t.Fatalf("AT-002: status %s, want completed", wo.Status)
	}
	if !has(e.jobs.EventTypes(), "work_order.completed") {
		t.Fatalf("AT-002: event work_order.completed tidak di-enqueue: %v", e.jobs.EventTypes())
	}
	// technician tidak bisa close (Completed ≠ Closed)
	st, _ = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/close", nil)
	if st != 403 {
		t.Fatalf("technician close harus 403, got %d", st)
	}

	// ---------- AT-003: Supervisor Close ----------
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+wo.ID.String(), nil)
	e.mustJSON(st, body, 200, &wo)
	if !has(wo.AllowedActions, "close") {
		t.Fatalf("AT-003: allowed_actions supervisor harus memuat close: %v", wo.AllowedActions)
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/close", nil)
	e.mustJSON(st, body, 200, &wo)
	if wo.Status != "closed" {
		t.Fatalf("AT-003: status %s", wo.Status)
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+wo.ID.String()+"/activities", nil)
	e.mustJSON(st, body, 200, &acts)
	closedSeen := false
	for _, a := range acts.Data {
		if a.Action == "status_changed" {
			closedSeen = true
		}
	}
	if !closedSeen || !has(e.jobs.EventTypes(), "work_order.closed") {
		t.Fatal("AT-003: close event tidak tercatat")
	}
	// reopen wajib alasan (FR-WO-014)
	st, _ = e.do(spv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/reopen", map[string]any{})
	if st != 400 {
		t.Fatalf("reopen tanpa reason harus 400, got %d", st)
	}
	st, body = e.do(spv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/reopen", map[string]any{"reason": "Suhu naik lagi"})
	e.mustJSON(st, body, 200, &wo)
	if wo.Status != "in_progress" {
		t.Fatalf("reopen status %s", wo.Status)
	}

	// ---------- Task lifecycle + finding → WO ----------
	var task workItem
	st, body = e.do(spv, http.MethodPost, "/api/v1/tasks", map[string]any{"task_type": "inspection", "title": "Inspeksi Mechanical Room", "location_id": e.refs.MechRoomA12, "assignee_user_id": budiID, "priority": "medium"})
	e.mustJSON(st, body, 201, &task)
	if !strings.HasPrefix(task.Number, "TSK-") || task.Status != "assigned" {
		t.Fatalf("task create: %s %s", task.Number, task.Status)
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/start", nil)
	e.mustJSON(st, body, 200, &task)
	var finding struct {
		ID            uuid.UUID `json:"id"`
		FindingNumber string    `json:"finding_number"`
		Status        string    `json:"status"`
	}
	st, body = e.do(tech, http.MethodPost, "/api/v1/findings", map[string]any{"title": "Kebocoran pipa chiller", "severity": "high", "source_type": "task", "source_id": task.ID, "location_id": e.refs.MechRoomA12})
	e.mustJSON(st, body, 201, &finding)
	if !strings.HasPrefix(finding.FindingNumber, "FND-") || finding.Status != "open" {
		t.Fatalf("finding: %+v", finding)
	}
	if !has(e.jobs.EventTypes(), "finding.created") {
		t.Fatal("finding.created event")
	}
	var woF workItem
	st, body = e.do(spv, http.MethodPost, "/api/v1/findings/"+finding.ID.String()+"/work-orders", map[string]any{"assignee_user_id": budiID})
	e.mustJSON(st, body, 201, &woF)
	if woF.Status != "assigned" {
		t.Fatalf("WO dari finding: %s", woF.Status)
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/findings/"+finding.ID.String(), nil)
	e.mustJSON(st, body, 200, &finding)
	if finding.Status != "in_progress" {
		t.Fatalf("finding harus in_progress setelah WO dibuat: %s", finding.Status)
	}
	// link dua arah terlihat dari WO
	var links struct {
		Data []struct {
			ObjectType string `json:"object_type"`
			Label      string `json:"label"`
		} `json:"data"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders/"+woF.ID.String()+"/links", nil)
	e.mustJSON(st, body, 200, &links)
	if len(links.Data) != 1 || links.Data[0].ObjectType != "finding" || links.Data[0].Label != finding.FindingNumber {
		t.Fatalf("links WO↔finding: %+v", links.Data)
	}
	// task complete (inspection tanpa requires_photo) → close
	st, body = e.do(tech, http.MethodPost, "/api/v1/tasks/"+task.ID.String()+"/complete", nil)
	e.mustJSON(st, body, 200, &task)
	if task.Status != "completed" {
		t.Fatalf("task complete: %s", task.Status)
	}

	// ---------- Incident: report → assign → resolve → close, + WO ----------
	officer := e.login("wawan@demo.buildingvision.id")
	var inc struct {
		ID             uuid.UUID `json:"id"`
		IncidentNumber string    `json:"incident_number"`
		Status         string    `json:"status"`
	}
	st, body = e.do(officer, http.MethodPost, "/api/v1/incidents", map[string]any{"category": "suspicious_activity", "title": "Orang mencurigakan di parkir LG", "location_id": e.refs.ParkingLG, "severity": "high"})
	e.mustJSON(st, body, 201, &inc)
	if !strings.HasPrefix(inc.IncidentNumber, "INC-") {
		t.Fatalf("incident number %s", inc.IncidentNumber)
	}
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/incidents/"+inc.ID.String()+"/assign", map[string]any{"assignee_team_id": e.refs.Teams["security"]})
	e.mustJSON(st, body, 200, &inc)
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/incidents/"+inc.ID.String()+"/resolve", map[string]any{"resolution": "Sudah ditangani, orang diminta meninggalkan area"})
	e.mustJSON(st, body, 200, &inc)
	if inc.Status != "resolved" {
		t.Fatalf("incident resolve: %s", inc.Status)
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/incidents/"+inc.ID.String()+"/close", nil)
	e.mustJSON(st, body, 200, &inc)
	if inc.Status != "closed" {
		t.Fatalf("incident close: %s", inc.Status)
	}

	// ---------- list & filter ----------
	var list struct {
		Data []workItem `json:"data"`
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders?status=in_progress,assigned&property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 2 {
		t.Fatalf("filter status: %d", len(list.Data))
	}
	st, body = e.do(tech, http.MethodGet, "/api/v1/work-orders?mine=true", nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 2 {
		t.Fatalf("mine: %d", len(list.Data))
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders?location_id="+e.refs.TowerA.String()+"&limit=1", nil)
	var pl struct {
		Data       []workItem `json:"data"`
		NextCursor *string    `json:"next_cursor"`
	}
	e.mustJSON(st, body, 200, &pl)
	if len(pl.Data) != 1 || pl.NextCursor == nil {
		t.Fatalf("cursor pagination: %d %v", len(pl.Data), pl.NextCursor)
	}
	st, body = e.do(spv, http.MethodGet, "/api/v1/work-orders?location_id="+e.refs.TowerA.String()+"&limit=1&cursor="+*pl.NextCursor, nil)
	e.mustJSON(st, body, 200, &pl)
	if len(pl.Data) != 1 {
		t.Fatalf("cursor page 2: %d", len(pl.Data))
	}

	// ---------- AT-010: Organization isolation ----------
	adminB := e.login(e.orgBAdmin)
	st, body = e.do(adminB, http.MethodGet, "/api/v1/work-orders/"+wo.ID.String(), nil)
	if st != 404 {
		t.Fatalf("AT-010: WO org A dari org B harus 404, got %d %s", st, body)
	}
	st, body = e.do(adminB, http.MethodGet, "/api/v1/work-orders", nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 0 {
		t.Fatalf("AT-010: list org B harus kosong, got %d", len(list.Data))
	}
	st, _ = e.do(adminB, http.MethodGet, "/api/v1/locations/"+e.refs.PropertyID.String(), nil)
	if st != 404 {
		t.Fatalf("AT-010: property org A dari org B harus 404, got %d", st)
	}
	st, _ = e.do(adminB, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/close", nil)
	if st != 404 {
		t.Fatalf("AT-010: aksi lintas org harus 404, got %d", st)
	}
	st, _ = e.do(adminB, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "repair", "title": "x", "location_id": e.refs.LobbyA})
	if st != 400 && st != 404 {
		t.Fatalf("AT-010: create WO dengan lokasi org lain harus ditolak, got %d", st)
	}
	st, _ = e.do(adminB, http.MethodGet, "/api/v1/users/"+budiID.String(), nil)
	if st != 404 {
		t.Fatalf("AT-010: user org A dari org B harus 404, got %d", st)
	}
	var users struct {
		Data []struct{ Email *string } `json:"data"`
	}
	st, body = e.do(adminB, http.MethodGet, "/api/v1/users", nil)
	e.mustJSON(st, body, 200, &users)
	for _, u := range users.Data {
		if u.Email != nil && strings.Contains(*u.Email, "demo.buildingvision") {
			t.Fatal("AT-010: user org A bocor ke org B")
		}
	}

	// ---------- Audit log tersedia untuk admin ----------
	adminA := e.login("admin@org-a.test")
	var al struct {
		Data []struct {
			Action  string `json:"action"`
			Message string `json:"message"`
		} `json:"data"`
	}
	st, body = e.do(adminA, http.MethodGet, "/api/v1/audit-logs?limit=50", nil)
	e.mustJSON(st, body, 200, &al)
	found := false
	for _, l := range al.Data {
		if l.Action == "status_change" && strings.Contains(l.Message, "WO-") {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit log status_change WO tidak ditemukan (%d rows)", len(al.Data))
	}
	// unauthenticated
	st, _ = e.do("", http.MethodGet, "/api/v1/work-orders", nil)
	if st != 401 {
		t.Fatalf("tanpa token harus 401, got %d", st)
	}
}

func TestAuthRefreshRotation(t *testing.T) {
	e := setup(t)
	st, body := e.do("", http.MethodPost, "/api/v1/auth/login", map[string]any{"identifier": "budi@demo.buildingvision.id", "password": "Demo12345!", "client": "mobile"})
	var lr struct {
		RefreshToken string `json:"refresh_token"`
		AccessToken  string `json:"access_token"`
	}
	e.mustJSON(st, body, 200, &lr)
	if lr.RefreshToken == "" {
		t.Fatal("mobile login harus mengembalikan refresh_token")
	}
	st, body = e.do("", http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": lr.RefreshToken, "client": "mobile"})
	var r2 struct {
		RefreshToken string `json:"refresh_token"`
	}
	e.mustJSON(st, body, 200, &r2)
	if r2.RefreshToken == "" || r2.RefreshToken == lr.RefreshToken {
		t.Fatal("refresh token harus dirotasi")
	}
	// reuse token lama → seluruh sesi dicabut
	st, _ = e.do("", http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": lr.RefreshToken, "client": "mobile"})
	if st != 401 {
		t.Fatalf("reuse harus 401, got %d", st)
	}
	st, _ = e.do("", http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": r2.RefreshToken, "client": "mobile"})
	if st != 401 {
		t.Fatalf("setelah reuse-detection, token baru pun harus 401, got %d", st)
	}
	// password salah
	st, _ = e.do("", http.MethodPost, "/api/v1/auth/login", map[string]any{"identifier": "budi@demo.buildingvision.id", "password": "salah"})
	if st != 401 {
		t.Fatalf("password salah harus 401, got %d", st)
	}
}
