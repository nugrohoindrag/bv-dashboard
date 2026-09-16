package app_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type capResp struct {
	PropertyID   uuid.UUID                          `json:"property_id"`
	Profile      string                             `json:"profile"`
	Capabilities []string                           `json:"capabilities"`
	Terminology  map[string]struct{ ID, EN string } `json:"terminology"`
	Config       struct {
		ExposeSLAToTenant bool `json:"expose_sla_to_tenant"`
		Version           int  `json:"version"`
	} `json:"config"`
}

// AT-P1-000 / AT-P1-000A / AC-02..AC-20 Onboarding Brief: profile melekat pada property, capability server-side,
// mandatory modules di semua profile, perubahan profile = aksi administratif.
func TestPropertyProfile(t *testing.T) {
	e := setup(t)
	pm := e.login("pm@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	admin := e.login("admin@org-a.test")

	// katalog profile
	st, body := e.do(pm, http.MethodGet, "/api/v1/profiles", nil)
	var cat struct {
		Data []struct {
			Code         string   `json:"code"`
			Capabilities []string `json:"capabilities"`
		} `json:"data"`
	}
	e.mustJSON(st, body, 200, &cat)
	if len(cat.Data) != 3 {
		t.Fatalf("profiles: %d", len(cat.Data))
	}
	for _, p := range cat.Data {
		for _, m := range []string{"housekeeping", "security", "engineering", "tenant_relation"} {
			if !has(p.Capabilities, m) {
				t.Fatalf("profile %s tanpa modul mandatory %s", p.Code, m)
			}
		}
	}

	// property demo = office (backfill/seed)
	st, body = e.do(tech, http.MethodGet, "/api/v1/properties/"+e.refs.PropertyID.String()+"/capabilities", nil)
	var c capResp
	e.mustJSON(st, body, 200, &c)
	if c.Profile != "office" || !has(c.Capabilities, "tenant_management") || has(c.Capabilities, "hotel_booking") || has(c.Capabilities, "unit_sales") {
		t.Fatalf("office capabilities salah: %+v", c)
	}
	if c.Terminology["customer"].EN != "Tenant" {
		t.Fatalf("terminology office: %+v", c.Terminology["customer"])
	}

	// AC-02: create property tanpa profile ditolak
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "property", "name": "Grand City Hotel", "details": map[string]any{"timezone": "Asia/Jakarta"}})
	if st != 400 {
		t.Fatalf("create property tanpa profile: %d %s", st, body)
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "property", "name": "Grand City Hotel", "details": map[string]any{"timezone": "Asia/Jakarta", "profile": "hotel"}})
	var loc struct {
		ID      uuid.UUID      `json:"id"`
		Version int            `json:"version"`
		Details map[string]any `json:"details"`
	}
	e.mustJSON(st, body, 201, &loc)
	if loc.Details["profile"] != "hotel" {
		t.Fatalf("details.profile: %v", loc.Details)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/properties/"+loc.ID.String()+"/capabilities", nil)
	e.mustJSON(st, body, 200, &c)
	if c.Profile != "hotel" || !has(c.Capabilities, "hotel_booking") || !has(c.Capabilities, "reception") || has(c.Capabilities, "unit_rental") {
		t.Fatalf("hotel capabilities salah: %v", c.Capabilities)
	}
	if c.Terminology["inventory_unit"].EN != "Room" || c.Terminology["customer"].ID != "Tamu" {
		t.Fatalf("terminology hotel: %+v", c.Terminology)
	}

	// PS-006: PATCH biasa tidak boleh mengubah profile
	st, body = e.do(admin, http.MethodPatch, "/api/v1/locations/"+loc.ID.String(), map[string]any{"details": map[string]any{"profile": "office"}})
	if st != 400 {
		t.Fatalf("patch profile harus ditolak: %d %s", st, body)
	}
	// technician tidak punya change_profile
	st, body = e.do(tech, http.MethodPost, "/api/v1/properties/"+loc.ID.String()+"/profile", map[string]any{"profile": "apartment", "reason": "test"})
	if st != 403 {
		t.Fatalf("technician change profile: %d %s", st, body)
	}
	// reason wajib
	st, body = e.do(admin, http.MethodPost, "/api/v1/properties/"+loc.ID.String()+"/profile", map[string]any{"profile": "apartment"})
	if st != 400 {
		t.Fatalf("change profile tanpa reason: %d %s", st, body)
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/properties/"+loc.ID.String()+"/profile", map[string]any{"profile": "apartment", "reason": "Salah pilih saat setup"})
	e.mustJSON(st, body, 200, &c)
	if c.Profile != "apartment" || !has(c.Capabilities, "unit_sales") || !has(c.Capabilities, "unit_rental") || has(c.Capabilities, "hotel_booking") {
		t.Fatalf("apartment capabilities salah: %v", c.Capabilities)
	}
	// audit log tercatat
	st, body = e.do(admin, http.MethodGet, "/api/v1/audit-logs?entity_type=property_profile", nil)
	if st != 200 || !strings.Contains(string(body), "Grand City Hotel") {
		t.Fatalf("audit profile change: %d %s", st, body)
	}

	// konfigurasi profile: terminology override + OD flags
	st, body = e.do(admin, http.MethodPatch, "/api/v1/properties/"+loc.ID.String()+"/profile-config", map[string]any{"expose_sla_to_tenant": true, "terminology": map[string]any{"customer": map[string]string{"id": "Warga", "en": "Resident"}}})
	e.mustJSON(st, body, 200, &c)
	if !c.Config.ExposeSLAToTenant || c.Terminology["customer"].ID != "Warga" || c.Terminology["customer"].EN != "Resident" {
		t.Fatalf("profile-config: %+v %+v", c.Config, c.Terminology["customer"])
	}
	st, body = e.do(admin, http.MethodPatch, "/api/v1/properties/"+loc.ID.String()+"/profile-config", map[string]any{"terminology": map[string]any{"bogus": map[string]string{"id": "x"}}})
	if st != 400 {
		t.Fatalf("terminology key tak dikenal harus 400: %d %s", st, body)
	}

	// kategori SR per profile (PRD §11): hotel punya room_service; office tidak
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "property", "name": "Hotel B", "details": map[string]any{"timezone": "Asia/Jakarta", "profile": "hotel"}})
	var hotelB struct {
		ID uuid.UUID `json:"id"`
	}
	e.mustJSON(st, body, 201, &hotelB)
	codes := func(propertyID uuid.UUID) []string {
		st, body := e.do(admin, http.MethodGet, "/api/v1/service-request-categories?property_id="+propertyID.String(), nil)
		var res struct {
			Data []struct {
				Code string  `json:"code"`
				Icon *string `json:"icon"`
			} `json:"data"`
		}
		e.mustJSON(st, body, 200, &res)
		var out []string
		for _, c := range res.Data {
			out = append(out, c.Code)
		}
		return out
	}
	hc, oc := codes(hotelB.ID), codes(e.refs.PropertyID)
	if !has(hc, "room_service") || has(oc, "room_service") || !has(oc, "plumbing") || !has(hc, "plumbing") || !has(oc, "complaint") {
		t.Fatalf("kategori per profile: hotel=%v office=%v", hc, oc)
	}

	// org B tidak melihat property org A (RLS) — AC-14
	st, _ = e.do(e.login(e.orgBAdmin), http.MethodGet, "/api/v1/properties/"+loc.ID.String()+"/capabilities", nil)
	if st != 404 && st != 403 {
		t.Fatalf("lintas org: %d", st)
	}
}
