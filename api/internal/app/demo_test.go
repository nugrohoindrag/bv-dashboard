package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/buildingvision/api/internal/demo"
)

// Demo Seed Database (Development Instruction v1.1): seed tiga environment (Hotel, Apartment, Office) lewat service layer,
// verifikasi coverage (§40), login akun demo (dashboard, Tenant App, BVRooms OTP mock), otorisasi Admin Internal (§38),
// idempotensi & reset (§32).
func TestDemoSeed(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	d := e.app.Demo

	res, err := d.Seed(ctx, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, l := range res.Log {
		t.Log(l)
	}
	st, err := d.Status(ctx)
	if err != nil || !st.Present || st.State != "ready" || len(st.Profiles) != 3 {
		t.Fatalf("status setelah seed: %+v err=%v", st, err)
	}
	for _, p := range demo.Profiles {
		if ps := st.Profiles[p]; ps.PropertyID == nil || ps.RecordCount < 50 {
			t.Fatalf("profile %s belum lengkap: %+v", p, ps)
		}
	}

	// ---- verifikasi coverage (§40) ----
	rep, err := d.Verify(ctx)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	t.Log("\n" + rep.Text())
	if !rep.Pass {
		t.Fatalf("verifikasi demo gagal: %d/%d", rep.Failed, rep.Total)
	}

	// ---- login akun demo (§6) ----
	for _, email := range []string{demo.AdminEmail, "hotel.manager@buildingvision.local", "apartment.manager@buildingvision.local", "office.manager@buildingvision.local", "engineering.demo@buildingvision.local", "security.demo@buildingvision.local", "housekeeping.demo@buildingvision.local", "tenantrelation.demo@buildingvision.local"} {
		if st, body := e.do("", http.MethodPost, "/api/v1/auth/login", map[string]any{"identifier": email, "password": demo.Password, "client": "web"}); st != 200 {
			t.Fatalf("login %s: %d %s", email, st, body)
		}
	}
	for _, email := range []string{"hotel.guest@buildingvision.local", "apartment.tenant@buildingvision.local", "office.tenant@buildingvision.local", "office.tenant2@buildingvision.local"} {
		st, body := e.loginTenant(t, email, demo.Password)
		if st != 200 {
			t.Fatalf("login tenant %s: %d %v", email, st, body)
		}
	}
	// prospect: pending validation → login ditolak sampai divalidasi Tenant Relation
	if st, _ := e.loginTenant(t, "apartment.prospect@buildingvision.local", demo.Password); st == 200 {
		t.Fatalf("prospect pending validation tidak boleh login")
	}
	// BVRooms customer: OTP mock → sesi → booking history hanya miliknya
	var otp struct {
		DevCode string `json:"dev_code"`
	}
	st2, body := e.do("", http.MethodPost, "/api/v1/bvrooms/auth/otp/request", map[string]any{"organization_slug": demo.OrgSlug, "phone": "+6281200000103", "purpose": "login"})
	e.mustJSON(st2, body, 200, &otp)
	var sess struct {
		AccessToken string `json:"access_token"`
	}
	st2, body = e.do("", http.MethodPost, "/api/v1/bvrooms/auth/otp/verify", map[string]any{"organization_slug": demo.OrgSlug, "phone": "+6281200000103", "purpose": "login", "code": otp.DevCode, "device_id": "t"})
	e.mustJSON(st2, body, 200, &sess)
	var upcoming, history struct {
		Data []struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	st2, body = e.do(sess.AccessToken, http.MethodGet, "/api/v1/bvrooms/bookings?scope=upcoming", nil)
	e.mustJSON(st2, body, 200, &upcoming)
	st2, body = e.do(sess.AccessToken, http.MethodGet, "/api/v1/bvrooms/bookings?scope=history", nil)
	e.mustJSON(st2, body, 200, &history)
	if len(upcoming.Data) == 0 || len(history.Data) == 0 || upcoming.Data[0].Status != "CHECK IN" || history.Data[0].Status != "CHECK OUT" {
		t.Fatalf("customer 3 harus punya upcoming CHECK IN (multi-room) & history CHECK OUT: %+v | %+v", upcoming, history)
	}
	// katalog publik org demo: 2 listing (hotel + apartment); staff JWT ditolak endpoint customer
	st2, body = e.do("", http.MethodGet, "/api/v1/bvrooms/catalog/properties?organization_slug=demo", nil)
	if st2 != 200 || !strings.Contains(string(body), "grand-vision-hotel") || !strings.Contains(string(body), "vision-residence") {
		t.Fatalf("katalog demo: %d %s", st2, body)
	}
	admin := e.login("admin@org-a.test")
	if st, _ := e.do(admin, http.MethodGet, "/api/v1/bvrooms/customers/me", nil); st != 401 {
		t.Fatalf("staff JWT di endpoint customer harus 401: %d", st)
	}

	// ---- otorisasi Demo Data: non admin_internal → 403 (§38) ----
	if st, body := e.do(admin, http.MethodGet, "/api/v1/admin/demo", nil); st != 403 {
		t.Fatalf("organization_admin tidak boleh akses Demo Data: %d %s", st, body)
	}
	demoAdmin := e.loginAs(t, demo.AdminEmail, demo.Password)
	if st, _ := e.do(demoAdmin, http.MethodPost, "/api/v1/admin/demo/seed", map[string]any{}); st != 403 {
		t.Fatalf("admin demo (organization_admin) tidak boleh seed lewat API: %d", st)
	}

	// ---- idempotent: seed ulang tidak menggandakan ----
	before := st.Profiles[demo.ProfileHotel].RecordCount
	if _, err := d.Seed(ctx, []string{demo.ProfileHotel}); err != nil {
		t.Fatalf("seed ulang: %v", err)
	}
	st, _ = d.Status(ctx)
	if st.Profiles[demo.ProfileHotel].RecordCount != before {
		t.Fatalf("seed ulang menggandakan data: %d → %d", before, st.Profiles[demo.ProfileHotel].RecordCount)
	}

	// ---- reset sebagian: hotel dihapus, apartment & office di-seed ulang; lalu reset total ----
	rr, err := d.Reset(ctx, []string{demo.ProfileHotel})
	if err != nil {
		t.Fatalf("reset hotel: %v", err)
	}
	if !rr.DeletedOrg || len(rr.Reseeded) != 2 {
		t.Fatalf("reset sebagian: %+v", rr)
	}
	st, _ = d.Status(ctx)
	if st.Profiles[demo.ProfileHotel].PropertyID != nil || st.Profiles[demo.ProfileApartment].PropertyID == nil {
		t.Fatalf("status setelah reset sebagian: %+v", st.Profiles)
	}
	if _, err := d.Reset(ctx, nil); err != nil {
		t.Fatalf("reset total: %v", err)
	}
	st, _ = d.Status(ctx)
	if st.Present {
		t.Fatalf("organization demo masih ada setelah reset")
	}
	// data organization lain tidak terpengaruh
	if st, body := e.do(admin, http.MethodGet, "/api/v1/locations/"+e.refs.PropertyID.String(), nil); st != 200 {
		t.Fatalf("property org-a hilang setelah reset demo: %d %s", st, body)
	}
}

func (e *env) loginAs(t *testing.T, email, pass string) string {
	t.Helper()
	st, body := e.do("", http.MethodPost, "/api/v1/auth/login", map[string]any{"identifier": email, "password": pass, "client": "web"})
	if st != 200 {
		t.Fatalf("login %s: %d %s", email, st, body)
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(body, &resp)
	return resp.AccessToken
}
