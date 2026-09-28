package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/app"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/seed"
)

// ============================================================================
// PRD P0 v2 (Roadmap v2.0) — P0 QA & Security Test Suite (deliverable #19, §29 Success Metrics)
//   • API authentication coverage 100%      → TestP0v2APIAuthCoverage
//   • Tenant isolation 100%                  → TestP0v2TenantIsolationMatrix
//   • RBAC / access scope (property, building, vendor, escalation) → TestP0v2AccessScope
//   • Organization / hierarchy / portfolio / platform admin → TestP0v2OrganizationAndHierarchy
//   • Critical audit events + immutability   → TestP0v2AuditTrail
//   • Task lifecycle, WO foundation, checklist, evidence, notification, search, export → TestP0v2TaskFoundation
// ============================================================================

// publicRoutes: route yang SENGAJA tanpa token staf. Route baru tanpa autentikasi akan membuat test gagal
// sampai didaftarkan di sini secara eksplisit (review keamanan).
var publicRoutes = map[string]bool{
	"GET /health": true, "GET /ready": true, "GET /healthz": true, "GET /readyz": true,
	"POST /public/v1/service-requests": true, "GET /public/v1/service-requests/{number}": true,
	"POST /api/v1/auth/login": true, "POST /api/v1/auth/refresh": true, "POST /api/v1/auth/logout": true, "POST /api/v1/auth/accept-invite": true,
	"GET /api/v1/tenant/registration/properties": true, "GET /api/v1/tenant/registration/locations": true, "POST /api/v1/tenant/register": true,
	"POST /api/v1/webhooks/payments/{provider}": true,
	"GET /api/v1/public/documents/{token}":      true, // PRD P4 v2.1: PDF lewat tautan bertanda tangan HMAC (token = otorisasi)
	"GET /api/v1/public/app-downloads":          true,
	"POST /api/v1/public/signup":                true, "POST /api/v1/public/signup/resend": true, "POST /api/v1/public/signup/verify": true,
	"POST /api/v1/public/demo-requests": true, "POST /api/v1/public/events": true, "GET /api/v1/public/plans": true,
}

// bvroomsCustomerRoutes: diproteksi token customer BVRooms (bukan token staf) — prefix.
func isBVRoomsRoute(route string) bool { return strings.HasPrefix(route, "/api/v1/bvrooms/") }

func TestP0v2APIAuthCoverage(t *testing.T) {
	e := setup(t)
	routes := app.ProbeRoutes(e.app.Router)
	if len(routes) < 300 {
		t.Fatalf("jumlah route terlalu sedikit (%d) — probe gagal?", len(routes))
	}
	protected, public := 0, 0
	for _, ri := range routes {
		key := ri.Method + " " + ri.Route
		switch {
		case publicRoutes[key]:
			public++
			if ri.RequiresStaffAuth() {
				t.Errorf("route publik %s ternyata meminta token staf", key)
			}
		case isBVRoomsRoute(ri.Route):
			// katalog/auth BVRooms publik; endpoint customer memakai token customer (401 domain) — tidak boleh menerima tanpa token
			if ri.Status >= 200 && ri.Status < 300 && (strings.Contains(ri.Route, "/customers") || strings.Contains(ri.Route, "/bookings")) {
				t.Errorf("endpoint customer BVRooms %s dapat diakses tanpa token (%d)", key, ri.Status)
			}
		default:
			protected++
			if !ri.RequiresStaffAuth() {
				t.Errorf("route %s tidak diproteksi autentikasi (status %d, code %s)", key, ri.Status, ri.Code)
			}
		}
	}
	t.Logf("API auth coverage: %d route terproteksi, %d publik terdaftar", protected, public)

	// token kedaluwarsa/rusak ditolak seragam
	st, _ := e.do("not-a-token", http.MethodGet, "/api/v1/me", nil)
	if st != 401 {
		t.Fatalf("token rusak harus 401, got %d", st)
	}
}

// ---------- helpers ----------

func (e *env) roleID(token, code string) uuid.UUID {
	e.t.Helper()
	var roles []struct {
		ID   uuid.UUID `json:"id"`
		Code string    `json:"code"`
	}
	st, body := e.do(token, http.MethodGet, "/api/v1/roles", nil)
	var wrap struct {
		Data json.RawMessage `json:"data"`
	}
	e.mustJSON(st, body, 200, &wrap)
	_ = json.Unmarshal(wrap.Data, &roles)
	for _, r := range roles {
		if r.Code == code {
			return r.ID
		}
	}
	e.t.Fatalf("role %s tidak ditemukan", code)
	return uuid.Nil
}

func (e *env) createUser(token, email string, roles []map[string]any, extra map[string]any) uuid.UUID {
	e.t.Helper()
	in := map[string]any{"email": email, "full_name": strings.Split(email, "@")[0], "password": "Demo12345!", "roles": roles}
	for k, v := range extra {
		in[k] = v
	}
	var u struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(token, http.MethodPost, "/api/v1/users", in)
	e.mustJSON(st, body, 201, &u)
	return u.ID
}

func (e *env) tryLogin(email, pass string) (int, string, []byte) {
	st, body := e.do("", http.MethodPost, "/api/v1/auth/login", map[string]any{"identifier": email, "password": pass, "client": "mobile"})
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.Unmarshal(body, &resp)
	return st, resp.AccessToken, body
}

func listIDs(t *testing.T, body []byte) []uuid.UUID {
	t.Helper()
	var l struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &l); err != nil {
		t.Fatalf("list json: %v %s", err, body)
	}
	out := make([]uuid.UUID, 0, len(l.Data))
	for _, d := range l.Data {
		out = append(out, d.ID)
	}
	return out
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// ---------- Tenant isolation matrix (§6.2, §29 "Tenant isolation test 100%") ----------

func TestP0v2TenantIsolationMatrix(t *testing.T) {
	e := setup(t)
	adminA := e.login("admin@org-a.test")
	adminB := e.login(e.orgBAdmin)
	// object org A
	var wo, task workItem
	st, body := e.do(adminA, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "Isolasi WO", "location_id": e.refs.MechRoomA12})
	e.mustJSON(st, body, 201, &wo)
	st, body = e.do(adminA, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "Isolasi Task", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, &task)
	var pf struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(adminA, http.MethodPost, "/api/v1/portfolios", map[string]any{"name": "Portfolio A"})
	e.mustJSON(st, body, 201, &pf)

	// list di org B tidak memuat data org A
	for _, path := range []string{"/api/v1/work-orders", "/api/v1/tasks", "/api/v1/properties", "/api/v1/buildings", "/api/v1/users", "/api/v1/teams",
		"/api/v1/assets", "/api/v1/incidents", "/api/v1/findings", "/api/v1/service-requests", "/api/v1/tenants", "/api/v1/portfolios", "/api/v1/audit-logs", "/api/v1/notifications"} {
		st, body := e.do(adminB, http.MethodGet, path, nil)
		if st != 200 {
			t.Fatalf("org B GET %s: %d %s", path, st, body)
		}
		for _, id := range []uuid.UUID{wo.ID, task.ID, e.refs.PropertyID, pf.ID, e.refs.Users["technician"]} {
			if strings.Contains(string(body), id.String()) {
				t.Fatalf("ISOLASI BOCOR: %s org B memuat id org A %s", path, id)
			}
		}
	}
	// akses langsung ke object org A → 404 (RLS), bukan 403 yang membocorkan keberadaan
	for _, path := range []string{"/api/v1/work-orders/" + wo.ID.String(), "/api/v1/tasks/" + task.ID.String(), "/api/v1/locations/" + e.refs.PropertyID.String(),
		"/api/v1/portfolios/" + pf.ID.String(), "/api/v1/users/" + e.refs.Users["technician"].String()} {
		if st, _ := e.do(adminB, http.MethodGet, path, nil); st != 404 {
			t.Fatalf("org B GET %s harus 404, got %d", path, st)
		}
	}
	// mutasi lintas org ditolak
	if st, _ := e.do(adminB, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/cancel", map[string]any{"reason": "x"}); st != 404 {
		t.Fatalf("org B cancel WO org A harus 404, got %d", st)
	}
	if st, _ := e.do(adminB, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "x", "location_id": e.refs.MechRoomA12}); st != 400 && st != 404 {
		t.Fatalf("org B membuat WO di lokasi org A harus ditolak, got %d", st)
	}
	// RLS di level DB (bv_app): tabel anak tanpa organization_id juga terisolasi (00020)
	ctx := context.Background()
	err := e.app.DB.WithOrgTx(ctx, e.orgB, func(ctx context.Context, tx pgx.Tx) error {
		for _, q := range []string{`SELECT count(*) FROM user_roles WHERE user_id = $1`, `SELECT count(*) FROM team_members WHERE user_id = $1`} {
			var n int
			if err := tx.QueryRow(ctx, q, e.refs.Users["technician"]).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				t.Fatalf("RLS bocor pada %q: %d baris", q, n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---------- Organization, hierarchy, portfolio, platform admin (§6, §7, US-P0-001..003) ----------

func TestP0v2OrganizationAndHierarchy(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")

	// US-P0-001: profil organization
	var org struct {
		Name      string  `json:"name"`
		LegalName *string `json:"legal_name"`
		Status    string  `json:"status"`
		Country   string  `json:"country"`
		Email     *string `json:"email"`
	}
	st, body := e.do(admin, http.MethodPatch, "/api/v1/organizations/me", map[string]any{"legal_name": "PT Org A Tbk", "email": "ops@org-a.test", "phone": "+6221000", "country": "id", "address": "Jl. Sudirman 1"})
	e.mustJSON(st, body, 200, &org)
	if org.LegalName == nil || *org.LegalName != "PT Org A Tbk" || org.Country != "ID" || org.Status != "active" {
		t.Fatalf("profil organization: %+v", org)
	}
	if st, _ := e.do(admin, http.MethodPatch, "/api/v1/organizations/me", map[string]any{"email": "bukan-email"}); st != 400 {
		t.Fatalf("email organization tidak valid harus 400, got %d", st)
	}

	// Portfolio → Property
	var pf struct {
		ID            uuid.UUID `json:"id"`
		Code          string    `json:"code"`
		PropertyCount int       `json:"property_count"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/portfolios", map[string]any{"name": "Jakarta Office Portfolio"})
	e.mustJSON(st, body, 201, &pf)
	if !strings.HasPrefix(pf.Code, "PF-") {
		t.Fatalf("kode portfolio %s", pf.Code)
	}

	// US-P0-002: property baru dengan kontak lengkap + portfolio
	var prop struct {
		ID       uuid.UUID      `json:"id"`
		Code     string         `json:"code"`
		IsActive bool           `json:"is_active"`
		Details  map[string]any `json:"details"`
		Metadata map[string]any `json:"metadata"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/properties", map[string]any{"name": "Menara Dua", "code": "MND", "metadata": map[string]any{"nop": "31.71"},
		"details": map[string]any{"profile": "office", "address": "Jl. Thamrin 2", "city": "Jakarta", "contact_name": "Pak Budi", "contact_phone": "+62811", "contact_email": "pm@menara.test",
			"postal_code": "10230", "country": "ID", "portfolio_id": pf.ID.String()}})
	e.mustJSON(st, body, 201, &prop)
	if prop.Code != "MND" || prop.Details["contact_email"] != "pm@menara.test" || prop.Details["portfolio_id"] != pf.ID.String() || prop.Metadata["nop"] != "31.71" {
		t.Fatalf("property: %s", body)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/portfolios/"+pf.ID.String()+"/properties", nil)
	if st != 200 || !containsID(listIDs(t, body), prop.ID) {
		t.Fatalf("portfolio properties: %d %s", st, body)
	}
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/properties", map[string]any{"name": "Duplikat", "code": "MND", "details": map[string]any{"profile": "office"}}); st != 409 {
		t.Fatalf("kode lokasi duplikat harus 409, got %d", st)
	}

	// US-P0-003: Building → Tower → Floor → Area / Unit
	mk := func(path string, in map[string]any) uuid.UUID {
		var l struct {
			ID uuid.UUID `json:"id"`
		}
		st, body := e.do(admin, http.MethodPost, path, in)
		e.mustJSON(st, body, 201, &l)
		return l.ID
	}
	bld := mk("/api/v1/buildings", map[string]any{"name": "Gedung Utama", "parent_id": prop.ID, "details": map[string]any{"building_type": "office_tower", "address": "Blok A"}})
	twr := mk("/api/v1/towers", map[string]any{"name": "Tower Utara", "parent_id": bld})
	flTower := mk("/api/v1/floors", map[string]any{"name": "Lantai 5", "parent_id": twr, "details": map[string]any{"floor_number": 5.0}})
	flDirect := mk("/api/v1/floors", map[string]any{"name": "Lantai 1", "parent_id": bld, "details": map[string]any{"floor_number": 1.0}})
	area := mk("/api/v1/areas", map[string]any{"name": "Lobby", "parent_id": flDirect, "details": map[string]any{"area_type": "lobby"}})
	unit := mk("/api/v1/units", map[string]any{"name": "Unit 501", "parent_id": flTower, "details": map[string]any{"unit_number": "501", "unit_type": "commercial"}})
	var detail struct {
		Details map[string]any `json:"details"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/units/"+unit.String(), nil)
	e.mustJSON(st, body, 200, &detail)
	if detail.Details["occupancy_status"] != "vacant" {
		t.Fatalf("unit occupancy default: %v", detail.Details)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/buildings/"+bld.String(), nil)
	e.mustJSON(st, body, 200, &detail)
	if detail.Details["building_type"] != "office_tower" {
		t.Fatalf("building_type: %v", detail.Details)
	}
	// floor di bawah tower tetap muncul di /buildings/{id}/floors
	st, body = e.do(admin, http.MethodGet, "/api/v1/buildings/"+bld.String()+"/floors", nil)
	ids := listIDs(t, body)
	if st != 200 || !containsID(ids, flTower) || !containsID(ids, flDirect) {
		t.Fatalf("buildings/floors subtree: %d %s", st, body)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/buildings/"+bld.String()+"/towers", nil)
	if st != 200 || !containsID(listIDs(t, body), twr) {
		t.Fatalf("buildings/towers: %d %s", st, body)
	}
	// Unit tidak boleh langsung di bawah Building
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/units", map[string]any{"name": "X", "parent_id": bld, "details": map[string]any{"unit_number": "X"}}); st != 400 {
		t.Fatalf("unit di bawah building harus 400, got %d", st)
	}
	// activate/deactivate property → status property ikut
	var loc struct {
		IsActive bool           `json:"is_active"`
		Details  map[string]any `json:"details"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/properties/"+prop.ID.String()+"/deactivate", map[string]any{"reason": "renovasi"})
	e.mustJSON(st, body, 200, &loc)
	if loc.IsActive || loc.Details["status"] != "inactive" {
		t.Fatalf("deactivate property: %s", body)
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/properties/"+prop.ID.String()+"/activate", nil)
	e.mustJSON(st, body, 200, &loc)
	if !loc.IsActive || loc.Details["status"] != "active" {
		t.Fatalf("activate property: %s", body)
	}
	// typed DELETE (area tanpa pemakaian)
	if st, body := e.do(admin, http.MethodDelete, "/api/v1/areas/"+area.String(), nil); st != 204 {
		t.Fatalf("delete area: %d %s", st, body)
	}

	// ---------- Platform Admin (§8.2): registry organization lintas tenant ----------
	ctx := context.Background()
	ownerless := e.app.DB
	if err := seed.SeedInternalOrganization(ctx, ownerless, iam.NewService(ownerless, nil, 0), "platform@bv.test", "Platform12345!"); err != nil {
		t.Fatalf("seed internal org: %v", err)
	}
	st, pa, body := e.tryLogin("platform@bv.test", "Platform12345!")
	if st != 200 {
		t.Fatalf("login platform admin: %d %s", st, body)
	}
	// organization_admin pelanggan ("*") tetap tidak boleh mengakses registry
	if st, _ := e.do(admin, http.MethodGet, "/api/v1/platform/organizations", nil); st != 403 {
		t.Fatalf("org admin pelanggan ke registry harus 403, got %d", st)
	}
	var created struct {
		ID        uuid.UUID `json:"id"`
		Slug      string    `json:"slug"`
		UserCount int       `json:"user_count"`
		Status    string    `json:"status"`
	}
	st, body = e.do(pa, http.MethodPost, "/api/v1/platform/organizations", map[string]any{"name": "Org C Properti", "legal_name": "PT Org C",
		"admin": map[string]any{"full_name": "Admin C", "email": "admin@org-c.test", "password": "AdminC12345!"}})
	e.mustJSON(st, body, 201, &created)
	if created.Slug != "org-c-properti" || created.UserCount != 1 || created.Status != "active" {
		t.Fatalf("create org: %s", body)
	}
	st, tokC, body := e.tryLogin("admin@org-c.test", "AdminC12345!")
	if st != 200 {
		t.Fatalf("login admin org C: %d %s", st, body)
	}
	if st, _ := e.do(tokC, http.MethodGet, "/api/v1/me", nil); st != 200 {
		t.Fatalf("admin C /me: %d", st)
	}
	var orgs struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	st, body = e.do(pa, http.MethodGet, "/api/v1/platform/organizations?q=Org", nil)
	e.mustJSON(st, body, 200, &orgs)
	if len(orgs.Data) < 3 {
		t.Fatalf("registry list: %s", body)
	}
	// deactivate: login ditolak & token lama tidak berlaku
	if st, _ := e.do(pa, http.MethodPost, "/api/v1/platform/organizations/"+created.ID.String()+"/deactivate", map[string]any{}); st != 400 {
		t.Fatalf("deactivate tanpa reason harus 400, got %d", st)
	}
	st, body = e.do(pa, http.MethodPost, "/api/v1/platform/organizations/"+created.ID.String()+"/suspend", map[string]any{"reason": "tunggakan"})
	e.mustJSON(st, body, 200, &created)
	if created.Status != "suspended" {
		t.Fatalf("suspend: %s", body)
	}
	if st, body := e.do(tokC, http.MethodGet, "/api/v1/me", nil); st != 401 && st != 403 {
		t.Fatalf("token org suspended harus ditolak, got %d %s", st, body)
	}
	st, _, body = e.tryLogin("admin@org-c.test", "AdminC12345!")
	if st != 403 || !strings.Contains(string(body), "ORGANIZATION_SUSPENDED") {
		t.Fatalf("login org suspended: %d %s", st, body)
	}
	st, body = e.do(pa, http.MethodPost, "/api/v1/platform/organizations/"+created.ID.String()+"/activate", nil)
	e.mustJSON(st, body, 200, &created)
	if st, _, _ := e.tryLogin("admin@org-c.test", "AdminC12345!"); st != 200 {
		t.Fatalf("login setelah reactivate: %d", st)
	}
	// organization internal tidak dapat dinonaktifkan
	var me struct {
		Principal struct {
			Organization uuid.UUID `json:"organization_id"`
		} `json:"principal"`
	}
	st, body = e.do(pa, http.MethodGet, "/api/v1/me", nil)
	e.mustJSON(st, body, 200, &me)
	if st, _ := e.do(pa, http.MethodPost, "/api/v1/platform/organizations/"+me.Principal.Organization.String()+"/deactivate", map[string]any{"reason": "x"}); st != 403 {
		t.Fatalf("deactivate org internal harus 403, got %d", st)
	}
}
