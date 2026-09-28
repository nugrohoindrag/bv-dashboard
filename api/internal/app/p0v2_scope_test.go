package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------- Access scope & RBAC (§8.2–§8.4, US-P0-004, §29 "RBAC permission test 100%") ----------

func TestP0v2AccessScope(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	pid := e.refs.PropertyID

	// ---- Property scope: property_id eksplisit di luar scope → 403 (regresi bug list asset/incident/finding/SR/tenant/location) ----
	var p2 struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(admin, http.MethodPost, "/api/v1/properties", map[string]any{"name": "Property Kedua", "details": map[string]any{"profile": "apartment"}})
	e.mustJSON(st, body, 201, &p2)
	tech := e.login("budi@demo.buildingvision.id") // technician ber-scope property demo
	for _, path := range []string{"/api/v1/assets", "/api/v1/incidents", "/api/v1/findings", "/api/v1/service-requests", "/api/v1/tenants", "/api/v1/locations", "/api/v1/work-orders", "/api/v1/tasks"} {
		st, body := e.do(tech, http.MethodGet, path+"?property_id="+p2.ID.String(), nil)
		if st != 403 {
			t.Errorf("SCOPE: technician GET %s?property_id=<lain> harus 403, got %d %s", path, st, body)
		}
	}

	// ---- Building/Tower scope: supervisor hanya Tower A ----
	woA := e.createWO(admin, e.refs.MechRoomA12, "WO Tower A")
	woB := e.createWO(admin, e.refs.ToiletB3, "WO Tower B")
	scoped := e.createUser(admin, "spv.towera@org-a.test", []map[string]any{{"role_id": e.roleID(admin, "supervisor"), "property_id": pid, "scope_location_id": e.refs.TowerA}}, nil)
	_ = scoped
	stok := e.login("spv.towera@org-a.test")
	st, body = e.do(stok, http.MethodGet, "/api/v1/work-orders", nil)
	ids := listIDs(t, body)
	if st != 200 || !containsID(ids, woA) || containsID(ids, woB) {
		t.Fatalf("BUILDING SCOPE list WO: %d A=%v B=%v", st, containsID(ids, woA), containsID(ids, woB))
	}
	if st, _ := e.do(stok, http.MethodGet, "/api/v1/work-orders/"+woB.String(), nil); st != 403 {
		t.Fatalf("BUILDING SCOPE: detail WO Tower B harus 403, got %d", st)
	}
	if st, body := e.do(stok, http.MethodGet, "/api/v1/work-orders/"+woA.String(), nil); st != 200 {
		t.Fatalf("BUILDING SCOPE: detail WO Tower A: %d %s", st, body)
	}
	if st, body := e.do(stok, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "x", "location_id": e.refs.ToiletB3}); st != 403 {
		t.Fatalf("BUILDING SCOPE: create WO di Tower B harus 403, got %d %s", st, body)
	}
	if st, body := e.do(stok, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "x", "location_id": e.refs.ToiletA12}); st != 201 {
		t.Fatalf("BUILDING SCOPE: create WO di Tower A: %d %s", st, body)
	}
	// lokasi: subtree Tower A + leluhur (property/building) terlihat; Tower B tidak
	st, body = e.do(stok, http.MethodGet, "/api/v1/locations", nil)
	ids = listIDs(t, body)
	if st != 200 || !containsID(ids, e.refs.FloorA12) || !containsID(ids, pid) || containsID(ids, e.refs.FloorB3) {
		t.Fatalf("BUILDING SCOPE locations: %d floorA=%v property=%v floorB=%v", st, containsID(ids, e.refs.FloorA12), containsID(ids, pid), containsID(ids, e.refs.FloorB3))
	}
	if st, _ := e.do(stok, http.MethodGet, "/api/v1/locations/"+e.refs.FloorB3.String(), nil); st != 403 {
		t.Fatalf("BUILDING SCOPE: detail Floor B harus 403, got %d", st)
	}
	// scope harus Building/Tower di property yang sama
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/users", map[string]any{"email": "bad.scope@org-a.test", "full_name": "x", "password": "Demo12345!",
		"roles": []map[string]any{{"role_id": e.roleID(admin, "supervisor"), "property_id": pid, "scope_location_id": e.refs.FloorA12}}}); st != 400 {
		t.Fatalf("scope floor harus 400, got %d", st)
	}
	// /me menyertakan scope_location_id
	st, body = e.do(stok, http.MethodGet, "/api/v1/me/permissions", nil)
	if st != 200 || !strings.Contains(string(body), e.refs.TowerA.String()) {
		t.Fatalf("/me/permissions tanpa scope_location_id: %s", body)
	}

	// ---- Resource scope: akun Vendor hanya WO vendor-nya ----
	var vnd struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/vendors", map[string]any{"name": "PT Dingin Selalu", "service_categories": []string{"hvac"}})
	e.mustJSON(st, body, 201, &vnd)
	if st, body := e.do(admin, http.MethodPost, "/api/v1/work-orders/"+woA.String()+"/vendor", map[string]any{"vendor_id": vnd.ID}); st != 200 {
		t.Fatalf("assign vendor: %d %s", st, body)
	}
	// role vendor tanpa vendor_id ditolak; vendor + role staf lain ditolak
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/users", map[string]any{"email": "v0@org-a.test", "full_name": "v", "password": "Demo12345!", "roles": []map[string]any{{"role_id": e.roleID(admin, "vendor")}}}); st != 400 {
		t.Fatalf("role vendor tanpa vendor_id harus 400, got %d", st)
	}
	e.createUser(admin, "teknisi@dingin.test", []map[string]any{{"role_id": e.roleID(admin, "vendor")}}, map[string]any{"vendor_id": vnd.ID})
	vtok := e.login("teknisi@dingin.test")
	st, body = e.do(vtok, http.MethodGet, "/api/v1/work-orders", nil)
	ids = listIDs(t, body)
	if st != 200 || len(ids) != 1 || ids[0] != woA {
		t.Fatalf("VENDOR SCOPE list: %d %v", st, ids)
	}
	if st, _ := e.do(vtok, http.MethodGet, "/api/v1/work-orders/"+woB.String(), nil); st != 403 {
		t.Fatalf("VENDOR SCOPE: WO lain harus 403, got %d", st)
	}
	if st, _ := e.do(vtok, http.MethodGet, "/api/v1/tasks", nil); st != 403 {
		t.Fatalf("VENDOR: tanpa permission tasks harus 403, got %d", st)
	}
	// WO dijadwalkan lalu dikerjakan vendor (guard assignee menerima vendor pemilik WO)
	if st, body := e.do(admin, http.MethodPost, "/api/v1/work-orders/"+woA.String()+"/schedule", map[string]any{"due_at": "2030-01-01T00:00:00Z"}); st != 200 {
		t.Fatalf("schedule WO: %d %s", st, body)
	}
	var started workItem
	st, body = e.do(vtok, http.MethodPost, "/api/v1/work-orders/"+woA.String()+"/start", map[string]any{})
	e.mustJSON(st, body, 200, &started)
	if started.Status != "in_progress" {
		t.Fatalf("vendor start WO: %s", body)
	}

	// ---- Privilege escalation guard ----
	var role struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/roles", map[string]any{"code": "user_admin", "name": "User Admin", "permissions": []string{"iam.users.*", "iam.roles.view"}})
	e.mustJSON(st, body, 201, &role)
	e.createUser(admin, "useradmin@org-a.test", []map[string]any{{"role_id": role.ID}}, nil)
	ua := e.login("useradmin@org-a.test")
	st, body = e.do(ua, http.MethodPost, "/api/v1/users", map[string]any{"email": "evil@org-a.test", "full_name": "evil", "password": "Demo12345!",
		"roles": []map[string]any{{"role_id": e.roleID(admin, "organization_admin")}}})
	if st != 403 || !strings.Contains(string(body), "ROLE_NOT_GRANTABLE") {
		t.Fatalf("ESKALASI: user admin memberi organization_admin harus 403 ROLE_NOT_GRANTABLE, got %d %s", st, body)
	}
	// role yang permission-nya dimiliki pemberi → boleh
	if st, body := e.do(ua, http.MethodPost, "/api/v1/users", map[string]any{"email": "ok@org-a.test", "full_name": "ok", "password": "Demo12345!",
		"roles": []map[string]any{{"role_id": role.ID}}}); st != 201 {
		t.Fatalf("grant role setara: %d %s", st, body)
	}

	// ---- RBAC negatif: technician tidak dapat admin / delete / reopen ----
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users"}, {http.MethodPost, "/api/v1/roles"}, {http.MethodPost, "/api/v1/portfolios"},
		{http.MethodDelete, "/api/v1/work-orders/" + woB.String()}, {http.MethodPost, "/api/v1/notifications/broadcast"},
		{http.MethodGet, "/api/v1/audit-logs"}, {http.MethodGet, "/api/v1/platform/organizations"}, {http.MethodPost, "/api/v1/users/" + scoped.String() + "/deactivate"},
	} {
		if st, _ := e.do(tech, c.method, c.path, map[string]any{}); st != 403 {
			t.Errorf("RBAC: technician %s %s harus 403, got %d", c.method, c.path, st)
		}
	}
}

func (e *env) createWO(token string, loc uuid.UUID, title string) uuid.UUID {
	e.t.Helper()
	var w workItem
	st, body := e.do(token, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": title, "location_id": loc})
	e.mustJSON(st, body, 201, &w)
	return w.ID
}

// ---------- Audit trail (§16, US-P0-006, §29 "Critical audit events captured 100%") ----------

func TestP0v2AuditTrail(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	techID := e.refs.Users["technician"]

	e.createUser(admin, "audit.new@org-a.test", []map[string]any{{"role_id": e.roleID(admin, "staff"), "property_id": e.refs.PropertyID}}, nil)
	// login gagal (password salah) & akun nonaktif tercatat
	_, _, _ = e.tryLogin("budi@demo.buildingvision.id", "salah-password")
	st, body := e.do(admin, http.MethodPost, "/api/v1/users/"+techID.String()+"/deactivate", map[string]any{"reason": "cuti panjang"})
	if st != 200 {
		t.Fatalf("deactivate user: %d %s", st, body)
	}
	if st, _, _ := e.tryLogin("budi@demo.buildingvision.id", "Demo12345!"); st != 401 {
		t.Fatalf("login user nonaktif harus 401, got %d", st)
	}
	if st, body := e.do(admin, http.MethodPost, "/api/v1/users/"+techID.String()+"/activate", nil); st != 200 {
		t.Fatalf("activate user: %d %s", st, body)
	}
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/users/"+e.refs.AdminID.String()+"/deactivate", map[string]any{"reason": "x"}); st != 400 {
		t.Fatalf("menonaktifkan akun sendiri harus 400, got %d", st)
	}
	// login + logout (cookie/refresh saja, tanpa access token)
	st, tok, body := e.tryLogin("budi@demo.buildingvision.id", "Demo12345!")
	if st != 200 {
		t.Fatalf("login: %d %s", st, body)
	}
	var pair struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.Unmarshal(body, &pair)
	if st, _ := e.do("", http.MethodPost, "/api/v1/auth/logout", map[string]any{"refresh_token": pair.RefreshToken}); st != 204 {
		t.Fatalf("logout: %d", st)
	}
	_ = tok
	// permission change + konfigurasi
	spvRole := e.roleID(admin, "engineering_supervisor")
	if st, body := e.do(admin, http.MethodPatch, "/api/v1/roles/"+spvRole.String(), map[string]any{"name": "Engineering Supervisor", "permissions": []string{"operations.*", "engineering.*.view"}}); st != 200 {
		t.Fatalf("update role: %d %s", st, body)
	}
	if st, body := e.do(admin, http.MethodPut, "/api/v1/notifications/preferences", map[string]any{"type": "work_order_assigned", "inapp": true, "push": false, "email": false}); st != 204 && st != 200 {
		t.Fatalf("set preference: %d %s", st, body)
	}
	if st, body := e.do(admin, http.MethodPatch, "/api/v1/organizations/me", map[string]any{"phone": "+62210001"}); st != 200 {
		t.Fatalf("update org: %d %s", st, body)
	}

	var logs struct {
		Data []struct {
			Action     string          `json:"action"`
			EntityType string          `json:"entity_type"`
			ActorName  string          `json:"actor_name"`
			Before     json.RawMessage `json:"before"`
			After      json.RawMessage `json:"after"`
			IP         *string         `json:"ip"`
			UserAgent  *string         `json:"user_agent"`
		} `json:"data"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/audit-logs?limit=200", nil)
	e.mustJSON(st, body, 200, &logs)
	seen := map[string]bool{}
	for _, l := range logs.Data {
		seen[l.Action+":"+l.EntityType] = true
		if l.Action == "status_change" && l.EntityType == "user" && (string(l.Before) == "null" || string(l.After) == "null") {
			t.Errorf("status_change user tanpa previous/new value")
		}
	}
	for _, want := range []string{"login:user", "login_failed:user", "logout:user", "status_change:user", "permission_change:role", "update:role", "config_change:notification_preference", "update:organization", "create:user"} {
		if !seen[want] {
			t.Errorf("AUDIT: event %s tidak tercatat (seen=%v)", want, seen)
		}
	}

	// immutability: operational DB role (bv_app) tidak dapat UPDATE/DELETE audit_logs
	ctx := context.Background()
	err := e.app.DB.WithOrgTx(ctx, e.refs.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE audit_logs SET action = 'tampered'`)
		return err
	})
	if err == nil {
		t.Fatal("AUDIT IMMUTABLE: UPDATE audit_logs oleh bv_app harus gagal")
	}
	err = e.app.DB.WithOrgTx(ctx, e.refs.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM audit_logs`)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("AUDIT IMMUTABLE: DELETE audit_logs harus ditolak trigger, got %v", err)
	}
}

// ---------- Session management (§24.1) ----------

func TestP0v2Sessions(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	_, t1, _ := e.tryLogin("budi@demo.buildingvision.id", "Demo12345!")
	_, t2, _ := e.tryLogin("budi@demo.buildingvision.id", "Demo12345!")
	var sessions struct {
		Data []struct {
			ID      uuid.UUID `json:"id"`
			Current bool      `json:"current"`
		} `json:"data"`
	}
	st, body := e.do(t1, http.MethodGet, "/api/v1/me/sessions", nil)
	e.mustJSON(st, body, 200, &sessions)
	if len(sessions.Data) < 2 {
		t.Fatalf("sesi aktif: %s", body)
	}
	cur := 0
	for _, s := range sessions.Data {
		if s.Current {
			cur++
		}
	}
	if cur != 1 {
		t.Fatalf("tepat satu sesi current, got %d", cur)
	}
	// revoke sesi lain → token t2 ditolak (setelah version bump), t1 perlu refresh (TOKEN_STALE) — keduanya bukan 200
	if st, body := e.do(t1, http.MethodPost, "/api/v1/me/sessions/revoke-others", nil); st != 200 {
		t.Fatalf("revoke-others: %d %s", st, body)
	}
	if st, _ := e.do(t2, http.MethodGet, "/api/v1/me", nil); st != 401 {
		t.Fatalf("token sesi yang dicabut harus 401, got %d", st)
	}
	// admin: lihat & cabut seluruh sesi user
	st, body = e.do(admin, http.MethodGet, "/api/v1/users/"+e.refs.Users["technician"].String()+"/sessions", nil)
	if st != 200 {
		t.Fatalf("admin list sessions: %d %s", st, body)
	}
	if st, body := e.do(admin, http.MethodPost, "/api/v1/users/"+e.refs.Users["technician"].String()+"/sessions/revoke", nil); st != 200 {
		t.Fatalf("admin revoke: %d %s", st, body)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/users/"+e.refs.Users["technician"].String()+"/sessions", nil)
	e.mustJSON(st, body, 200, &sessions)
	if len(sessions.Data) != 0 {
		t.Fatalf("setelah revoke semua, sesi tersisa %d", len(sessions.Data))
	}
	// logout-all
	_, t3, _ := e.tryLogin("siti@demo.buildingvision.id", "Demo12345!")
	if st, _ := e.do(t3, http.MethodPost, "/api/v1/auth/logout-all", nil); st != 200 {
		t.Fatalf("logout-all: %d", st)
	}
	if st, _ := e.do(t3, http.MethodGet, "/api/v1/me", nil); st != 401 {
		t.Fatalf("setelah logout-all token harus 401, got %d", st)
	}
}

// ---------- Invite Users (§26.1 Organization Setup journey) ----------

func TestP0v2InviteUser(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	var resp struct {
		User struct {
			ID uuid.UUID `json:"id"`
		} `json:"user"`
		Invite struct {
			InviteURL *string `json:"invite_url"`
		} `json:"invite"`
	}
	st, body := e.do(admin, http.MethodPost, "/api/v1/users", map[string]any{"email": "baru@org-a.test", "full_name": "User Baru", "invite": true,
		"roles": []map[string]any{{"role_id": e.roleID(admin, "staff"), "property_id": e.refs.PropertyID}}})
	e.mustJSON(st, body, 201, &resp)
	if resp.Invite.InviteURL == nil || !strings.Contains(*resp.Invite.InviteURL, "/accept-invite?token=") {
		t.Fatalf("invite url: %s", body)
	}
	token := (*resp.Invite.InviteURL)[strings.Index(*resp.Invite.InviteURL, "token=")+6:]
	if st, _ := e.do("", http.MethodPost, "/api/v1/auth/accept-invite", map[string]any{"token": token, "password": "pendek"}); st != 400 {
		t.Fatalf("password pendek harus 400, got %d", st)
	}
	if st, body := e.do("", http.MethodPost, "/api/v1/auth/accept-invite", map[string]any{"token": token, "password": "Rahasia12345!"}); st != 204 {
		t.Fatalf("accept invite: %d %s", st, body)
	}
	if st, _, body := e.tryLogin("baru@org-a.test", "Rahasia12345!"); st != 200 {
		t.Fatalf("login setelah accept: %d %s", st, body)
	}
	if st, body := e.do("", http.MethodPost, "/api/v1/auth/accept-invite", map[string]any{"token": token, "password": "Lagi12345!"}); st != 400 || !strings.Contains(string(body), "INVITE_EXPIRED") {
		t.Fatalf("token sekali pakai: %d %s", st, body)
	}
	// kirim ulang undangan membatalkan token lama
	if st, body := e.do(admin, http.MethodPost, "/api/v1/users/"+resp.User.ID.String()+"/invite", nil); st != 200 {
		t.Fatalf("re-invite: %d %s", st, body)
	}
}
