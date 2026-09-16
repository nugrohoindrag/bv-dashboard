package app_test

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/mailer"
	"github.com/buildingvision/api/internal/seed"
)

// AT-WEB-001..005 (Website PRD v1.1 §49): self-serve signup → verifikasi email → trial workspace → onboarding checklist
// → sample data; App Downloads hanya admin_internal (server-side), public API hanya tautan aktif + audit; trial expired diblokir.
func TestGrowthSignupTrialAndAppDownloads(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	rec := &mailer.Recorder{}
	e.app.Growth.Mailer = rec
	e.app.Growth.SetRateLimit(0)

	// ---------- Signup ----------
	st, body := e.do("", http.MethodPost, "/api/v1/public/signup", map[string]any{"email": "owner@trial.test", "full_name": "Trial Owner", "password": "Trial12345!", "organization_name": "Trial Hotel Group", "source_page": "/pricing"})
	var su struct {
		SignupID uuid.UUID `json:"signup_id"`
	}
	e.mustJSON(st, body, 201, &su)
	if len(rec.Sent) != 1 || !strings.Contains(rec.Sent[0].Text, "/verify-email?token=") {
		t.Fatalf("verification email tidak terkirim: %+v", rec.Sent)
	}
	token := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`).FindStringSubmatch(rec.Sent[0].Text)[1]

	// email yang sama ditolak untuk akun yang sudah ada
	st, body = e.do("", http.MethodPost, "/api/v1/public/signup", map[string]any{"email": "admin@org-a.test", "full_name": "X", "password": "Trial12345!", "organization_name": "Y"})
	if st != 409 {
		t.Fatalf("signup email terpakai: %d %s", st, body)
	}
	// token salah
	st, body = e.do("", http.MethodPost, "/api/v1/public/signup/verify", map[string]any{"token": "bogus"})
	if st != 404 {
		t.Fatalf("verify token salah: %d %s", st, body)
	}

	// ---------- Verify → organization + trial + auto login ----------
	st, body = e.do("", http.MethodPost, "/api/v1/public/signup/verify", map[string]any{"token": token})
	var ver struct {
		AccessToken    string    `json:"access_token"`
		OrganizationID uuid.UUID `json:"organization_id"`
		Next           string    `json:"next"`
		User           struct {
			Roles []string `json:"roles"`
		} `json:"user"`
	}
	e.mustJSON(st, body, 200, &ver)
	if ver.AccessToken == "" || ver.Next != "/onboarding" || len(ver.User.Roles) != 1 || ver.User.Roles[0] != "organization_admin" {
		t.Fatalf("verify resp: %s", body)
	}
	// token tidak bisa dipakai dua kali
	st, _ = e.do("", http.MethodPost, "/api/v1/public/signup/verify", map[string]any{"token": token})
	if st != 409 {
		t.Fatalf("verify ulang: %d", st)
	}
	tok := ver.AccessToken

	st, body = e.do(tok, http.MethodGet, "/api/v1/trial", nil)
	var tr struct {
		Trial struct {
			Status   string `json:"status"`
			DaysLeft int    `json:"days_left"`
			Locked   bool   `json:"locked"`
		} `json:"trial"`
	}
	e.mustJSON(st, body, 200, &tr)
	if tr.Trial.Status != "trial" || tr.Trial.DaysLeft < 13 || tr.Trial.Locked {
		t.Fatalf("trial: %s", body)
	}
	// login biasa dengan password signup juga bisa
	st, _ = e.do("", http.MethodPost, "/api/v1/auth/login", map[string]any{"identifier": "owner@trial.test", "password": "Trial12345!", "client": "web"})
	if st != 200 {
		t.Fatalf("login akun trial: %d", st)
	}

	// ---------- Onboarding checklist sebelum property ----------
	st, body = e.do(tok, http.MethodGet, "/api/v1/onboarding", nil)
	var ob struct {
		Profile   *string `json:"profile"`
		Completed int     `json:"completed"`
		Total     int     `json:"total"`
		Activated bool    `json:"activated"`
		Items     []struct {
			Key  string `json:"key"`
			Done bool   `json:"done"`
		} `json:"items"`
	}
	e.mustJSON(st, body, 200, &ob)
	if ob.Total != 8 || ob.Completed != 0 || ob.Profile != nil {
		t.Fatalf("checklist awal: %s", body)
	}
	// sample data tanpa property → 404
	st, _ = e.do(tok, http.MethodPost, "/api/v1/onboarding/sample-data", map[string]any{"property_id": uuid.New()})
	if st != 404 {
		t.Fatalf("sample data tanpa property: %d", st)
	}

	// ---------- Create Property dengan profile (§26–§27) ----------
	st, body = e.do(tok, http.MethodPost, "/api/v1/properties", map[string]any{"name": "Grand Trial Hotel", "details": map[string]any{"profile": "hotel", "timezone": "Asia/Jakarta", "address": "Jl. Contoh 1", "city": "Jakarta"}})
	var prop struct {
		ID uuid.UUID `json:"id"`
	}
	e.mustJSON(st, body, 201, &prop)
	st, body = e.do(tok, http.MethodGet, "/api/v1/onboarding", nil)
	e.mustJSON(st, body, 200, &ob)
	if ob.Profile == nil || *ob.Profile != "hotel" || !ob.Items[0].Done || ob.Completed != 1 {
		t.Fatalf("checklist setelah property: %s", body)
	}

	// ---------- Sample data (§30) ----------
	st, body = e.do(tok, http.MethodPost, "/api/v1/onboarding/sample-data", map[string]any{"property_id": prop.ID})
	e.mustJSON(st, body, 201, &ob)
	done := map[string]bool{}
	for _, it := range ob.Items {
		done[it.Key] = it.Done
	}
	if !done["add_areas"] || !done["create_request"] || !done["create_work"] || done["complete_work"] || ob.Activated {
		t.Fatalf("checklist setelah sample data: %s", body)
	}
	st, _ = e.do(tok, http.MethodPost, "/api/v1/onboarding/sample-data", map[string]any{"property_id": prop.ID})
	if st != 409 {
		t.Fatalf("sample data dua kali: %d", st)
	}
	st, body = e.do(tok, http.MethodGet, "/api/v1/work-orders", nil)
	if st != 200 || !strings.Contains(string(body), "[Sample] AHU-01 is not cooling") {
		t.Fatalf("WO sample: %d %s", st, body)
	}
	// isolasi: org A tidak melihat WO sample org trial
	st, body = e.do(e.login("admin@org-a.test"), http.MethodGet, "/api/v1/work-orders", nil)
	if st != 200 || strings.Contains(string(body), "[Sample] AHU-01") {
		t.Fatalf("isolasi org: %d", st)
	}

	// ---------- App Downloads: organization_admin biasa (permission "*") tetap ditolak ----------
	st, _ = e.do(tok, http.MethodGet, "/api/v1/admin/app-downloads", nil)
	if st != 403 {
		t.Fatalf("app-downloads oleh org admin: %d", st)
	}
	st, _ = e.do(e.login("admin@org-a.test"), http.MethodPost, "/api/v1/admin/app-downloads", map[string]any{"name": "X", "app_type": "staff", "platform": "android", "download_url": "https://drive.google.com/file/d/abc/view"})
	if st != 403 {
		t.Fatalf("create app-download oleh org A admin: %d", st)
	}
	// public: belum ada
	st, body = e.do("", http.MethodGet, "/api/v1/public/app-downloads", nil)
	if st != 200 || strings.TrimSpace(string(body)) != `{"apps":[]}` {
		t.Fatalf("public app-downloads kosong: %d %s", st, body)
	}

	// seed organization internal + admin_internal (bvctl seed --internal)
	ownerDB, err := db.Open(ctx, adminURL())
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.SeedInternalOrganization(ctx, ownerDB, iam.NewService(ownerDB, nil, 0), "internal@bv.test", "Internal12345!"); err != nil {
		t.Fatal(err)
	}
	ownerDB.Close()
	st, body = e.do("", http.MethodPost, "/api/v1/auth/login", map[string]any{"identifier": "internal@bv.test", "password": "Internal12345!", "client": "web"})
	var li struct {
		AccessToken string `json:"access_token"`
		User        struct {
			IsInternalAdmin bool `json:"is_internal_admin"`
		} `json:"user"`
	}
	e.mustJSON(st, body, 200, &li)
	if !li.User.IsInternalAdmin {
		t.Fatalf("is_internal_admin harus true: %s", body)
	}
	itok := li.AccessToken
	// validasi URL Google Drive (§21)
	st, _ = e.do(itok, http.MethodPost, "/api/v1/admin/app-downloads", map[string]any{"name": "BuildingVision Staff App", "app_type": "staff", "platform": "android", "download_url": "https://example.com/app.apk"})
	if st != 400 && st != 422 {
		t.Fatalf("URL non-drive harus ditolak: %d", st)
	}
	st, body = e.do(itok, http.MethodPost, "/api/v1/admin/app-downloads", map[string]any{"name": "BuildingVision Staff App", "app_type": "staff", "platform": "android", "download_url": "https://drive.google.com/file/d/abc123/view"})
	var ad struct {
		ID      uuid.UUID `json:"id"`
		Status  string    `json:"status"`
		Version int       `json:"version"`
	}
	e.mustJSON(st, body, 201, &ad)
	if ad.Status != "inactive" {
		t.Fatalf("status awal harus inactive: %s", body)
	}
	st, body = e.do(itok, http.MethodPost, "/api/v1/admin/app-downloads", map[string]any{"name": "BuildingVision Tenant App", "app_type": "tenant", "platform": "android", "download_url": "https://drive.google.com/file/d/def456/view", "status": "active"})
	e.mustJSON(st, body, 201, nil)
	// inactive tidak tampil di publik; active tampil tanpa metadata internal (§22)
	st, body = e.do("", http.MethodGet, "/api/v1/public/app-downloads", nil)
	if st != 200 || strings.Contains(string(body), "Staff App") || !strings.Contains(string(body), "Tenant App") || strings.Contains(string(body), "updated_by") || strings.Contains(string(body), "\"id\"") {
		t.Fatalf("public app-downloads: %d %s", st, body)
	}
	st, body = e.do(itok, http.MethodPost, "/api/v1/admin/app-downloads/"+ad.ID.String()+"/activate", nil)
	e.mustJSON(st, body, 200, &ad)
	if ad.Status != "active" {
		t.Fatalf("activate: %s", body)
	}
	_, body = e.do("", http.MethodGet, "/api/v1/public/app-downloads", nil)
	if !strings.Contains(string(body), "Staff App") || !strings.Contains(string(body), "abc123") {
		t.Fatalf("public setelah activate: %s", body)
	}
	// ganti URL → audit previous/new url (§20)
	st, body = e.do(itok, http.MethodPatch, "/api/v1/admin/app-downloads/"+ad.ID.String(), map[string]any{"download_url": "https://drive.google.com/file/d/xyz789/view"})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(itok, http.MethodGet, "/api/v1/audit-logs?entity_type=app_download", nil)
	if st != 200 || !strings.Contains(string(body), "APP_DOWNLOAD_CREATED") || !strings.Contains(string(body), "APP_DOWNLOAD_ACTIVATED") || !strings.Contains(string(body), "APP_DOWNLOAD_UPDATED") || !strings.Contains(string(body), "previous_url") {
		t.Fatalf("audit app download: %d %s", st, body)
	}
	// admin internal tidak bisa membuat organization data biasa? (bukan scope) — cukup pastikan org biasa tetap 403 setelah data ada
	st, _ = e.do(tok, http.MethodGet, "/api/v1/admin/app-downloads", nil)
	if st != 403 {
		t.Fatalf("app-downloads oleh org admin (setelah ada data): %d", st)
	}

	// ---------- Trial lifecycle (§31): ending soon → expired → mutasi diblokir → choose plan ----------
	appDB := e.app.DB
	if _, err := appDB.Pool.Exec(ctx, `UPDATE organizations SET trial_ends_at = now() + interval '2 days' WHERE id = $1`, ver.OrganizationID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Growth.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	st, body = e.do(tok, http.MethodGet, "/api/v1/trial", nil)
	e.mustJSON(st, body, 200, &tr)
	if tr.Trial.Status != "trial_ending_soon" {
		t.Fatalf("ending soon: %s", body)
	}
	if _, err := appDB.Pool.Exec(ctx, `UPDATE organizations SET trial_ends_at = now() - interval '1 hour' WHERE id = $1`, ver.OrganizationID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Growth.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	st, body = e.do(tok, http.MethodGet, "/api/v1/trial", nil)
	e.mustJSON(st, body, 200, &tr)
	if tr.Trial.Status != "trial_expired" || !tr.Trial.Locked {
		t.Fatalf("expired: %s", body)
	}
	// email ending soon + expired ke admin
	var subjects []string
	for _, m := range rec.Sent {
		subjects = append(subjects, m.Subject)
	}
	joined := strings.Join(subjects, "|")
	if !strings.Contains(joined, "ends soon") || !strings.Contains(joined, "has expired") || !strings.Contains(joined, "Welcome") {
		t.Fatalf("email trial: %v", subjects)
	}
	// notifikasi in-app
	st, body = e.do(tok, http.MethodGet, "/api/v1/notifications", nil)
	if st != 200 || !strings.Contains(string(body), "trial_expired") {
		t.Fatalf("notifikasi trial: %d %s", st, body)
	}
	// mutasi diblokir (402), GET tetap boleh
	st, body = e.do(tok, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "blocked", "priority": "low"})
	if st != 402 {
		t.Fatalf("mutasi saat expired: %d %s", st, body)
	}
	st, _ = e.do(tok, http.MethodGet, "/api/v1/work-orders", nil)
	if st != 200 {
		t.Fatalf("GET saat expired: %d", st)
	}
	// choose a plan → converted, unlock
	st, body = e.do(tok, http.MethodPost, "/api/v1/trial/convert", map[string]any{"plan_code": "growth"})
	var ti struct {
		Status   string  `json:"status"`
		PlanCode *string `json:"plan_code"`
		Locked   bool    `json:"locked"`
	}
	e.mustJSON(st, body, 200, &ti)
	if ti.Status != "converted" || ti.PlanCode == nil || *ti.PlanCode != "growth" || ti.Locked {
		t.Fatalf("convert: %s", body)
	}
	st, body = e.do(tok, http.MethodPost, "/api/v1/tasks", map[string]any{"task_type": "general", "title": "after convert", "priority": "low", "property_id": prop.ID})
	if st != 201 {
		t.Fatalf("mutasi setelah convert: %d %s", st, body)
	}

	// ---------- Book a Demo + events (§34, §40) ----------
	st, body = e.do("", http.MethodPost, "/api/v1/public/demo-requests", map[string]any{"full_name": "Ana Demo", "email": "ana@demo.test", "company": "Demo Co", "property_profile": "office", "property_count": 3, "source_page": "/solutions/office"})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do("", http.MethodPost, "/api/v1/public/events", map[string]any{"events": []map[string]any{{"event": "app_download_clicked", "source_page": "/download", "properties": map[string]any{"app": "staff", "platform": "android"}}, {"event": "not_allowed"}}})
	if st != 202 || !strings.Contains(string(body), `"accepted":1`) {
		t.Fatalf("events: %d %s", st, body)
	}
	time.Sleep(50 * time.Millisecond) // email async
	var n int
	_ = appDB.Pool.QueryRow(ctx, `SELECT count(*) FROM growth_events WHERE event IN ('account_created','email_verified','trial_started','property_created','app_download_clicked','demo_requested','subscription_started')`).Scan(&n)
	if n < 6 {
		t.Fatalf("growth_events tercatat %d", n)
	}
	st, body = e.do("", http.MethodGet, "/api/v1/public/plans", nil)
	if st != 200 || !strings.Contains(string(body), `"trial_days":14`) {
		t.Fatalf("plans: %d %s", st, body)
	}
}
