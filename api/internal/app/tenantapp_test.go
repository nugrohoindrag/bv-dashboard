package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type tenantReq struct {
	ID             uuid.UUID `json:"id"`
	RequestNumber  string    `json:"request_number"`
	Status         string    `json:"status"`
	TenantStatus   string    `json:"tenant_status"`
	Priority       string    `json:"priority"`
	AreaScope      *string   `json:"area_scope"`
	ReopenCount    int       `json:"reopen_count"`
	AllowedActions []string  `json:"allowed_actions"`
	Location       struct {
		ID       *uuid.UUID `json:"id"`
		PathText *string    `json:"path_text"`
	} `json:"location"`
	Timeline []struct {
		Key       string `json:"key"`
		ActorKind string `json:"actor_kind"`
	} `json:"timeline"`
	MessageCount int `json:"message_count"`
	Feedback     *struct {
		Rating int `json:"rating"`
	} `json:"feedback"`
	DueEstimateAt *string `json:"due_estimate_at"`
	AssignedTeam  *string `json:"assigned_team"`
}

// loginTenant: login client tenant_app (refresh token di body).
func (e *env) loginTenant(t *testing.T, email, pass string) (int, map[string]any) {
	t.Helper()
	st, body := e.do("", http.MethodPost, "/api/v1/auth/login", map[string]any{"identifier": email, "password": pass, "client": "tenant_app"})
	var resp map[string]any
	_ = json.Unmarshal(body, &resp)
	return st, resp
}

func problemCode(resp map[string]any) string {
	c, _ := resp["code"].(string)
	return c
}

// P1.1/P1.2 — Tenant foundation & tenant ticket (AT-P1-001..009, WF-P1-001/002/003, DoD Tenant 1–10, Operations 11–16).
func TestTenantAppFlow(t *testing.T) {
	e := setup(t)
	e.app.TenantApp.SetRegistrationRateLimit(0)
	tr := e.login("tr.manager@demo.buildingvision.id")
	ops := e.login("ops@demo.buildingvision.id")
	engSpv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	budiID := e.refs.Users["technician"]

	// ---- registrasi publik (org-a) ----
	st, body := e.do("", http.MethodGet, "/api/v1/tenant/registration/properties?organization_slug=org-a", nil)
	var props struct {
		Data []struct {
			ID      uuid.UUID `json:"id"`
			Profile string    `json:"profile"`
		} `json:"data"`
	}
	e.mustJSON(st, body, 200, &props)
	if len(props.Data) == 0 || props.Data[0].ID != e.refs.PropertyID {
		t.Fatalf("registration properties: %+v", props)
	}
	st, body = e.do("", http.MethodGet, "/api/v1/tenant/registration/locations?organization_slug=org-a&type=floor&property_id="+e.refs.PropertyID.String(), nil)
	var floors struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	e.mustJSON(st, body, 200, &floors)
	if len(floors.Data) == 0 {
		t.Fatal("registration floors kosong")
	}
	st, body = e.do("", http.MethodGet, "/api/v1/tenant/registration/locations?organization_slug=org-a&type=unit&property_id="+e.refs.PropertyID.String()+"&parent_id="+e.refs.FloorA12.String(), nil)
	var units struct {
		Data []struct {
			ID   uuid.UUID `json:"id"`
			Code string    `json:"code"`
		} `json:"data"`
	}
	e.mustJSON(st, body, 200, &units)
	if len(units.Data) < 2 {
		t.Fatalf("registration units floor A12: %+v", units)
	}
	register := func(email, name string, unit uuid.UUID) uuid.UUID {
		st, body := e.do("", http.MethodPost, "/api/v1/tenant/register", map[string]any{"organization_slug": "org-a", "full_name": name, "email": email, "phone": "0812000", "password": "Tenant12345", "property_id": e.refs.PropertyID, "unit_id": unit, "ownership_status": "tenant"})
		var res struct {
			TenantUserID  uuid.UUID `json:"tenant_user_id"`
			AccountStatus string    `json:"account_status"`
		}
		e.mustJSON(st, body, 201, &res)
		if res.AccountStatus != "pending_validation" {
			t.Fatalf("register status: %s", res.AccountStatus)
		}
		return res.TenantUserID
	}
	tuA := register("dewi@tenant.test", "Dewi Lestari", e.refs.UnitA1201)
	tuB := register("rudi@tenant.test", "Rudi Hartono", e.refs.UnitA1202)
	// email duplikat
	st, _ = e.do("", http.MethodPost, "/api/v1/tenant/register", map[string]any{"organization_slug": "org-a", "full_name": "X", "email": "dewi@tenant.test", "password": "Tenant12345", "property_id": e.refs.PropertyID, "unit_id": e.refs.UnitA1201})
	if st != 409 {
		t.Fatalf("email duplikat harus 409: %d", st)
	}
	// unit di luar property → 400
	st, _ = e.do("", http.MethodPost, "/api/v1/tenant/register", map[string]any{"organization_slug": "org-a", "full_name": "X", "email": "x@tenant.test", "password": "Tenant12345", "property_id": e.refs.PropertyID, "unit_id": uuid.New()})
	if st != 400 {
		t.Fatalf("unit asing harus 400: %d", st)
	}

	// login sebelum validasi → ACCOUNT_PENDING (setelah password benar)
	st, resp := e.loginTenant(t, "dewi@tenant.test", "Tenant12345")
	if st != 403 || problemCode(resp) != "ACCOUNT_PENDING" {
		t.Fatalf("login pending: %d %v", st, resp)
	}
	st, resp = e.loginTenant(t, "dewi@tenant.test", "salah")
	if st != 401 {
		t.Fatalf("password salah harus 401 (tidak bocor status): %d %v", st, resp)
	}
	// staf tidak bisa login lewat Tenant App
	st, resp = e.loginTenant(t, "budi@demo.buildingvision.id", "Demo12345!")
	if st != 403 || problemCode(resp) != "NOT_TENANT_ACCOUNT" {
		t.Fatalf("staf via tenant_app: %d %v", st, resp)
	}

	// ---- Tenant Relation memvalidasi ----
	e.dispatch(t)
	if ib := e.inboxOf(t, tr); !hasType(ib, "tenant_account_pending") {
		t.Fatalf("notifikasi akun pending ke Tenant Relation tidak ada: %+v", ib.Data)
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/tenant-users?status=pending_validation&property_id="+e.refs.PropertyID.String(), nil)
	var tus struct {
		Data []struct {
			ID             uuid.UUID `json:"id"`
			Status         string    `json:"status"`
			AllowedActions []string  `json:"allowed_actions"`
		} `json:"data"`
	}
	e.mustJSON(st, body, 200, &tus)
	if len(tus.Data) != 2 || !has(tus.Data[0].AllowedActions, "approve") {
		t.Fatalf("pending tenant users: %+v", tus)
	}
	// technician tidak boleh memvalidasi
	st, _ = e.do(tech, http.MethodPost, "/api/v1/tenant-users/"+tuA.String()+"/approve", nil)
	if st != 403 {
		t.Fatalf("technician approve: %d", st)
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/tenant-users/"+tuA.String()+"/approve", nil)
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tr, http.MethodPost, "/api/v1/tenant-users/"+tuB.String()+"/approve", nil)
	e.mustJSON(st, body, 200, nil)

	// login tenant → refresh token di body; web client ditolak
	st, resp = e.loginTenant(t, "dewi@tenant.test", "Tenant12345")
	if st != 200 || resp["refresh_token"] == "" || resp["refresh_token"] == nil {
		t.Fatalf("login tenant: %d %v", st, resp)
	}
	tenA := resp["access_token"].(string)
	st, body = e.do("", http.MethodPost, "/api/v1/auth/login", map[string]any{"identifier": "dewi@tenant.test", "password": "Tenant12345", "client": "web"})
	if st != 403 || !strings.Contains(string(body), "TENANT_ACCOUNT_ONLY") {
		t.Fatalf("tenant via web harus 403: %d %s", st, body)
	}
	st, resp = e.loginTenant(t, "rudi@tenant.test", "Tenant12345")
	tenB := resp["access_token"].(string)
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "tenant_account_approved") {
		t.Fatalf("notifikasi approved ke tenant tidak ada: %+v", ib.Data)
	}

	// AT-P1-001: /tenant/me hanya property/unit yang diotorisasi; tidak ada permission staf
	var me struct {
		FullName string `json:"full_name"`
		Property struct {
			ID          uuid.UUID `json:"id"`
			Profile     string    `json:"profile"`
			Terminology map[string]struct {
				ID string `json:"id"`
			} `json:"terminology"`
		} `json:"property"`
		PrimaryUnit *struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		} `json:"primary_unit"`
		Units []any `json:"units"`
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/me", nil)
	e.mustJSON(st, body, 200, &me)
	if me.Property.ID != e.refs.PropertyID || me.Property.Profile != "office" || me.PrimaryUnit == nil || me.PrimaryUnit.ID != e.refs.UnitA1201 || len(me.Units) != 1 {
		t.Fatalf("tenant/me: %+v", me)
	}
	if me.Property.Terminology["customer"].ID != "Tenant" {
		t.Fatalf("terminology tenant/me: %+v", me.Property.Terminology)
	}
	for _, path := range []string{"/api/v1/work-orders", "/api/v1/service-requests", "/api/v1/tenants", "/api/v1/users", "/api/v1/tenant-users", "/api/v1/overview/today"} {
		if st, _ := e.do(tenA, http.MethodGet, path, nil); st != 403 {
			t.Fatalf("tenant akses endpoint staf %s harus 403, got %d", path, st)
		}
	}
	// staf tidak boleh memakai endpoint tenant
	if st, _ := e.do(ops, http.MethodGet, "/api/v1/tenant/me", nil); st != 403 {
		t.Fatalf("staf akses /tenant/me harus 403, got %d", st)
	}

	// kategori & lokasi
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/categories", nil)
	var cats struct {
		Data []struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	e.mustJSON(st, body, 200, &cats)
	codes := []string{}
	for _, c := range cats.Data {
		codes = append(codes, c.Code)
	}
	if !has(codes, "air_conditioning") || !has(codes, "plumbing") || has(codes, "room_service") {
		t.Fatalf("kategori tenant office: %v", codes)
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/locations", nil)
	var locs struct {
		Data []struct {
			ID    uuid.UUID `json:"id"`
			Scope string    `json:"scope"`
			Name  string    `json:"name"`
		} `json:"data"`
	}
	e.mustJSON(st, body, 200, &locs)
	var lobby uuid.UUID
	for _, l := range locs.Data {
		if l.ID == e.refs.UnitA1202 {
			t.Fatal("unit tenant lain tidak boleh muncul di lokasi laporan")
		}
		if l.ID == e.refs.LobbyA {
			lobby = l.ID
			if l.Scope != "common_area" {
				t.Fatalf("lobby scope: %s", l.Scope)
			}
		}
	}
	if lobby == uuid.Nil {
		t.Fatal("common area (Lobby) tidak tersedia untuk tenant")
	}

	// AT-P1-002: Report an Issue — My Unit (lokasi otomatis unit 1201), priority dari kategori (tenant tidak bisa override)
	var r1 tenantReq
	// field priority tidak ada di kontrak tenant (PRD §13: tenant tidak bebas menaikkan priority) → strict decode menolak
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "air_conditioning", "description": "AC tidak dingin sejak pagi, suhu ruangan 30 derajat", "priority": "critical"}); st != 400 {
		t.Fatalf("priority dari tenant harus ditolak: %d", st)
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "air_conditioning", "description": "AC tidak dingin sejak pagi, suhu ruangan 30 derajat"})
	e.mustJSON(st, body, 201, &r1)
	if !strings.HasPrefix(r1.RequestNumber, "SR-") || r1.TenantStatus != "submitted" || r1.Status != "new" || r1.Location.ID == nil || *r1.Location.ID != e.refs.UnitA1201 || r1.AreaScope == nil || *r1.AreaScope != "unit" {
		t.Fatalf("AT-P1-002: %+v", r1)
	}
	if r1.Priority != "medium" {
		t.Fatalf("PRD §13 priority tenant harus default kategori (medium), got %s", r1.Priority)
	}
	if !has(r1.AllowedActions, "cancel") || !has(r1.AllowedActions, "message") || has(r1.AllowedActions, "confirm") {
		t.Fatalf("allowed_actions baru: %v", r1.AllowedActions)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "ticket_created") {
		t.Fatalf("notifikasi Ticket Created ke tenant tidak ada: %+v", ib.Data)
	}
	if ib := e.inboxOf(t, tr); !hasType(ib, "service_request_received") {
		t.Fatalf("notifikasi SR received ke Tenant Relation tidak ada: %+v", ib.Data)
	}
	// AT-P1-003: common area Lobby Tower A
	var r2 tenantReq
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "cleanliness", "description": "Lantai lobby licin dan kotor bekas hujan", "location_id": lobby})
	e.mustJSON(st, body, 201, &r2)
	if r2.AreaScope == nil || *r2.AreaScope != "common_area" || *r2.Location.ID != lobby {
		t.Fatalf("AT-P1-003: %+v", r2)
	}
	// AT-P1-004: unit A-1202 bukan milik tenant A
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "electrical", "description": "Lampu mati di unit sebelah, tolong dicek", "location_id": e.refs.UnitA1202})
	if st != 403 || !strings.Contains(string(body), "LOCATION_NOT_AUTHORIZED") {
		t.Fatalf("AT-P1-004 create unit lain: %d %s", st, body)
	}
	// AT-P1-004: tenant B tidak bisa membaca ticket tenant A (404, bukan 403 — tidak bocor)
	if st, _ := e.do(tenB, http.MethodGet, "/api/v1/tenant/requests/"+r1.ID.String(), nil); st != 404 {
		t.Fatalf("AT-P1-004 read lintas tenant: %d", st)
	}
	if st, _ := e.do(tenB, http.MethodPost, "/api/v1/tenant/requests/"+r1.ID.String()+"/cancel", map[string]any{"reason": "x"}); st != 404 {
		t.Fatalf("AT-P1-004 act lintas tenant: %d", st)
	}
	var list struct {
		Data []tenantReq `json:"data"`
	}
	st, body = e.do(tenB, http.MethodGet, "/api/v1/tenant/requests", nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 0 {
		t.Fatalf("list tenant B harus kosong: %d", len(list.Data))
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/requests?open=true", nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 2 {
		t.Fatalf("list tenant A: %d", len(list.Data))
	}

	// ---- Operations: triage oleh Tenant Relation → Work Order ----
	var sr struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"status"`
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+r1.ID.String()+"/acknowledge", nil)
	e.mustJSON(st, body, 200, &sr)
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "ticket_status") {
		t.Fatalf("notifikasi status ke tenant tidak ada: %+v", ib.Data)
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/requests/"+r1.ID.String(), nil)
	e.mustJSON(st, body, 200, &r1)
	if r1.TenantStatus != "received" || len(r1.Timeline) < 2 || r1.Timeline[1].Key != "received" {
		t.Fatalf("tenant status/timeline setelah acknowledge: %+v", r1)
	}
	// komentar internal staf tidak pernah tampil ke tenant (AT-P1-009)
	st, _ = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+r1.ID.String()+"/comments", map[string]any{"body": "INTERNAL-NOTE vendor mahal, biaya 5jt"})
	if st != 201 {
		t.Fatalf("komentar internal: %d", st)
	}
	// pesan staf → tenant (thread terpisah)
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+r1.ID.String()+"/messages", map[string]any{"body": "Teknisi akan datang besok pagi jam 09.00"})
	e.mustJSON(st, body, 201, nil)
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "ticket_message") {
		t.Fatalf("notifikasi pesan ke tenant tidak ada: %+v", ib.Data)
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/requests/"+r1.ID.String()+"/messages", nil)
	var msgs struct {
		Data []struct {
			AuthorKind string `json:"author_kind"`
			Body       string `json:"body"`
		} `json:"data"`
	}
	e.mustJSON(st, body, 200, &msgs)
	if len(msgs.Data) != 1 || msgs.Data[0].AuthorKind != "staff" || strings.Contains(string(body), "INTERNAL-NOTE") {
		t.Fatalf("AT-P1-009 pesan tenant: %s", body)
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+r1.ID.String()+"/messages", map[string]any{"body": "Baik, saya ada di unit besok"})
	e.mustJSON(st, body, 201, nil)
	e.dispatch(t)
	if ib := e.inboxOf(t, tr); !hasType(ib, "service_request_message") {
		t.Fatalf("notifikasi pesan tenant ke Tenant Relation tidak ada")
	}
	// AT-P1-005: SR → WO (link dua arah)
	var wo workItem
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/service-requests/"+r1.ID.String()+"/work-orders", map[string]any{"work_order_type": "repair", "assignee_user_id": budiID, "requires_evidence": false, "estimated_cost": map[string]any{"amount": 5000000, "currency_code": "IDR"}})
	e.mustJSON(st, body, 201, &wo)
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/requests/"+r1.ID.String(), nil)
	e.mustJSON(st, body, 200, &r1)
	if r1.TenantStatus != "in_progress" {
		t.Fatalf("AT-P1-006 tenant status setelah WO dibuat: %s", r1.TenantStatus)
	}
	// AT-P1-009: detail tenant tidak memuat internal notes/cost/assignee/WO number
	raw := string(body)
	for _, forbidden := range []string{"INTERNAL-NOTE", "5000000", "cost", wo.Number, "Budi Santoso", "assignee_user", "links"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("AT-P1-009 detail tenant membocorkan %q: %s", forbidden, raw)
		}
	}
	// eksekusi WO → SR resolved (hook) → tenant Resolved (AT-P1-006)
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/start", nil)
	e.mustJSON(st, body, 200, &wo)
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/complete", map[string]any{"resolution": "Freon diisi ulang"})
	e.mustJSON(st, body, 200, &wo)
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/close", nil)
	e.mustJSON(st, body, 200, &wo)
	e.dispatch(t)
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/requests/"+r1.ID.String(), nil)
	e.mustJSON(st, body, 200, &r1)
	if r1.TenantStatus != "resolved" || !has(r1.AllowedActions, "confirm") || !has(r1.AllowedActions, "reopen") {
		t.Fatalf("AT-P1-006 resolved: %+v", r1)
	}
	if ib := e.inboxOf(t, tenA); !hasType(ib, "ticket_resolved") {
		t.Fatalf("notifikasi resolved ke tenant tidak ada: %+v", ib.Data)
	}
	// AT-P1-007: Confirm Resolved → Closed; feedback CSAT
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+r1.ID.String()+"/confirm", nil)
	e.mustJSON(st, body, 200, &r1)
	if r1.TenantStatus != "closed" || !has(r1.AllowedActions, "feedback") {
		t.Fatalf("AT-P1-007 confirm: %+v", r1)
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+r1.ID.String()+"/feedback", map[string]any{"rating": 5, "comment": "Cepat dan rapi"})
	e.mustJSON(st, body, 201, &r1)
	if r1.Feedback == nil || r1.Feedback.Rating != 5 {
		t.Fatalf("feedback: %+v", r1.Feedback)
	}
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+r1.ID.String()+"/feedback", map[string]any{"rating": 1}); st != 409 {
		t.Fatalf("feedback dua kali harus 409: %d", st)
	}
	// AT-P1-008: Reopen (Still Have Problem) — reason wajib, auditable
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+r2.ID.String()+"/acknowledge", nil)
	e.mustJSON(st, body, 200, &sr)
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+r2.ID.String()+"/resolve", map[string]any{"resolution": "Lobby sudah dibersihkan"})
	e.mustJSON(st, body, 200, &sr)
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+r2.ID.String()+"/reopen", map[string]any{}); st != 400 {
		t.Fatalf("reopen tanpa reason harus 400: %d", st)
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+r2.ID.String()+"/reopen", map[string]any{"reason": "Masih licin di dekat pintu masuk"})
	e.mustJSON(st, body, 200, &r2)
	if r2.TenantStatus != "in_progress" || r2.ReopenCount != 1 {
		t.Fatalf("AT-P1-008 reopen: %+v", r2)
	}
	found := false
	for _, ev := range r2.Timeline {
		if ev.Key == "reopened" && ev.ActorKind == "tenant" {
			found = true
		}
	}
	if !found {
		t.Fatalf("timeline reopen oleh tenant tidak tercatat: %+v", r2.Timeline)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tr); !hasType(ib, "service_request_reopened") {
		t.Fatalf("notifikasi reopen ke Tenant Relation tidak ada")
	}
	// audit trail reopen tercatat (guardrail #9)
	st, body = e.do(tr, http.MethodGet, "/api/v1/service-requests/"+r2.ID.String()+"/activities", nil)
	if st != 200 || !strings.Contains(string(body), "reopened") || !strings.Contains(string(body), "Masih licin") {
		t.Fatalf("audit reopen: %d %s", st, body)
	}
	// cancel oleh tenant hanya sebelum ditugaskan
	var r3 tenantReq
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "noise", "description": "Suara bising dari renovasi unit sebelah setiap malam"})
	e.mustJSON(st, body, 201, &r3)
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+r3.ID.String()+"/cancel", map[string]any{"reason": "Sudah selesai sendiri"})
	e.mustJSON(st, body, 200, &r3)
	if r3.TenantStatus != "cancelled" {
		t.Fatalf("cancel: %s", r3.TenantStatus)
	}

	// Tenant Relation metrics & feedback list
	var m struct {
		OpenTickets int      `json:"open_tickets"`
		CSAT        *float64 `json:"csat"`
		CSATCount   int      `json:"csat_count"`
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/tenant-relation/metrics?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &m)
	if m.CSATCount != 1 || m.CSAT == nil || *m.CSAT != 5 || m.OpenTickets != 1 {
		t.Fatalf("metrics: %+v", m)
	}
	st, body = e.do(tr, http.MethodGet, "/api/v1/tenant-relation/feedback", nil)
	if st != 200 || !strings.Contains(string(body), "Cepat dan rapi") {
		t.Fatalf("feedback list: %d %s", st, body)
	}

	// Announcement → tenant property (published saja)
	var ann struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/announcements", map[string]any{"property_id": e.refs.PropertyID, "title": "Pemadaman listrik terjadwal", "body": "Sabtu 08.00–10.00 untuk perawatan panel", "importance": "important"})
	e.mustJSON(st, body, 201, &ann)
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/announcements", nil)
	if st != 200 || strings.Contains(string(body), "Pemadaman") {
		t.Fatalf("draft announcement tidak boleh tampil: %d %s", st, body)
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/announcements/"+ann.ID.String()+"/publish", nil)
	e.mustJSON(st, body, 200, nil)
	e.dispatch(t)
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/announcements", nil)
	if st != 200 || !strings.Contains(string(body), "Pemadaman") {
		t.Fatalf("announcement published: %d %s", st, body)
	}
	if ib := e.inboxOf(t, tenB); !hasType(ib, "announcement") {
		t.Fatalf("notifikasi announcement ke tenant B tidak ada: %+v", ib.Data)
	}

	// suspend → sesi diputus
	st, body = e.do(tr, http.MethodPost, "/api/v1/tenant-users/"+tuB.String()+"/suspend", map[string]any{"reason": "Pindah unit"})
	e.mustJSON(st, body, 200, nil)
	if st, _ := e.do(tenB, http.MethodGet, "/api/v1/tenant/me", nil); st != 401 {
		t.Fatalf("akun suspended harus 401: %d", st)
	}
	st, resp = e.loginTenant(t, "rudi@tenant.test", "Tenant12345")
	if st != 403 || problemCode(resp) != "ACCOUNT_SUSPENDED" {
		t.Fatalf("login suspended: %d %v", st, resp)
	}
}
