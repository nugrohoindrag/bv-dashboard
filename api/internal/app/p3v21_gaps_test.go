package app_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type p3GapCtx struct {
	tenA        string
	sr3         uuid.UUID
	msgAtt      uuid.UUID
	vehicleID   uuid.UUID
	permitID    uuid.UUID
	pkgID       uuid.UUID
	memberEmail string
	memberTemp  string
	prefType    string
	fake        *fakeWebPush
}

// p3ClientGaps: celah backend yang ditemukan saat membangun Tenant App (PWA) P3/P4 v2.1 — pesan foto saja, strip foto SR,
// daftar izin parkir & detail kendaraan, nomor WhatsApp di registrasi, must_change_password ditegakkan server, kosongkan
// telepon, preferensi notifikasi parsial, tag Web Push per object, ringkasan unit per unit.
func p3ClientGaps(t *testing.T, e *env, c p3GapCtx) {
	tenA := c.tenA

	// ---- pesan foto tanpa teks (tenant & staf); tanpa teks & tanpa foto tetap 400 ----
	att2 := e.uploadTenantAttachment(tenA, "service_request", c.sr3, "photo", "image/png")
	if st, body := e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+c.sr3.String()+"/messages", map[string]any{"body": "", "attachment_ids": []uuid.UUID{att2}}); st != 201 {
		t.Fatalf("pesan foto tanpa teks harus diterima: %d %s", st, body)
	}
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/tenant/requests/"+c.sr3.String()+"/messages", map[string]any{"body": "  "}); st != 400 {
		t.Fatalf("pesan kosong tanpa foto harus 400, got %d", st)
	}
	tr := e.login("tr.manager@demo.buildingvision.id")
	if st, body := e.do(tr, http.MethodPost, "/api/v1/service-requests/"+c.sr3.String()+"/messages", map[string]any{"attachment_ids": []uuid.UUID{att2}}); st != 201 {
		t.Fatalf("pesan staf foto saja: %d %s", st, body)
	}
	// ---- lampiran pesan tidak ikut strip foto masalah SR ----
	var srd struct {
		Photos []struct {
			ID uuid.UUID `json:"id"`
		} `json:"photos"`
	}
	st, body := e.do(tenA, http.MethodGet, "/api/v1/tenant/requests/"+c.sr3.String(), nil)
	e.mustJSON(st, body, 200, &srd)
	for _, p := range srd.Photos {
		if p.ID == c.msgAtt || p.ID == att2 {
			t.Fatalf("lampiran pesan tidak boleh tampil sebagai foto SR: %s", body)
		}
	}

	// ---- daftar izin parkir tenant + detail kendaraan dengan dokumen (STNK) ----
	var pl struct {
		Data []struct {
			ID     uuid.UUID `json:"id"`
			Status string    `json:"status"`
		} `json:"data"`
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/parking-permits", nil)
	e.mustJSON(st, body, 200, &pl)
	if len(pl.Data) != 1 || pl.Data[0].ID != c.permitID || pl.Data[0].Status != "approved" {
		t.Fatalf("daftar izin parkir tenant: %s", body)
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/parking-permits?status=requested", nil)
	e.mustJSON(st, body, 200, &pl)
	if len(pl.Data) != 0 {
		t.Fatalf("filter status izin parkir: %s", body)
	}
	stnk := e.uploadTenantAttachment(tenA, "vehicle", c.vehicleID, "document", "application/pdf")
	var vd struct {
		ID        uuid.UUID `json:"id"`
		Permits   []any     `json:"permits"`
		Documents []struct {
			ID  uuid.UUID `json:"id"`
			URL string    `json:"url"`
		} `json:"documents"`
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/vehicles/"+c.vehicleID.String(), nil)
	e.mustJSON(st, body, 200, &vd)
	if vd.ID != c.vehicleID || len(vd.Permits) != 1 || len(vd.Documents) != 1 || vd.Documents[0].ID != stnk || vd.Documents[0].URL == "" {
		t.Fatalf("detail kendaraan + dokumen: %s", body)
	}
	// rudi mengganti password sementaranya di skenario reset password (Tenant99999)
	if _, lr := e.loginTenant(t, "rudi@tenant.test", "Tenant99999"); lr["access_token"] != nil {
		if st, _ := e.do(lr["access_token"].(string), http.MethodGet, "/api/v1/tenant/vehicles/"+c.vehicleID.String(), nil); st != 404 {
			t.Fatalf("kendaraan tenant lain harus 404, got %d", st)
		}
	} else {
		t.Fatalf("login rudi: %v", lr)
	}

	// ---- nomor WhatsApp pengelola di daftar property registrasi (publik) ----
	st, body = e.do("", http.MethodGet, "/api/v1/tenant/registration/properties?organization_slug=org-a", nil)
	if st != 200 || !strings.Contains(string(body), `"whatsapp_number":"628112222333"`) {
		t.Fatalf("registration/properties harus memuat whatsapp_number: %d %s", st, body)
	}

	// ---- must_change_password ditegakkan server ----
	st, lm := e.loginTenant(t, c.memberEmail, c.memberTemp)
	if st != 200 || lm["must_change_password"] != true {
		t.Fatalf("login anggota password sementara: %d %v", st, lm)
	}
	mt := lm["access_token"].(string)
	st, body = e.do(mt, http.MethodGet, "/api/v1/tenant/announcements", nil)
	if st != 403 || !strings.Contains(string(body), "PASSWORD_CHANGE_REQUIRED") {
		t.Fatalf("password sementara harus 403 PASSWORD_CHANGE_REQUIRED: %d %s", st, body)
	}
	for _, path := range []string{"/api/v1/tenant/me", "/api/v1/me", "/api/v1/push/config"} {
		if st, body := e.do(mt, http.MethodGet, path, nil); st != 200 {
			t.Fatalf("%s harus tetap terbuka saat wajib ganti password: %d %s", path, st, body)
		}
	}
	e.changeTempPassword(t, mt, c.memberTemp)
	if st, body := e.do(mt, http.MethodGet, "/api/v1/tenant/announcements", nil); st != 200 {
		t.Fatalf("setelah ganti password akses normal: %d %s", st, body)
	}

	// ---- ringkasan per unit: booking dihitung per unit (anggota lain unit 1201) ----
	pm := e.login("pm@demo.buildingvision.id")
	var fac struct {
		ID uuid.UUID `json:"id"`
	}
	st, body = e.do(pm, http.MethodPost, "/api/v1/facilities", map[string]any{"property_id": e.refs.PropertyID, "name": "Ruang Serbaguna", "facility_type": "meeting_room", "capacity": 8, "open_time": "00:00", "close_time": "23:59", "slot_minutes": 30, "min_duration_minutes": 30, "max_duration_minutes": 180})
	e.mustJSON(st, body, 201, &fac)
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	nowJ := time.Now().In(jkt)
	start := time.Date(nowJ.Year(), nowJ.Month(), nowJ.Day()+2, 10, 0, 0, 0, jkt)
	st, body = e.do(mt, http.MethodPost, "/api/v1/tenant/bookings", map[string]any{"facility_id": fac.ID, "starts_at": start, "ends_at": start.Add(time.Hour), "purpose": "Rapat anggota"})
	e.mustJSON(st, body, 201, nil)
	var units struct {
		Data []struct {
			Unit struct {
				ID uuid.UUID `json:"id"`
			} `json:"unit"`
			Counts struct {
				UpcomingBookings int `json:"upcoming_bookings"`
			} `json:"counts"`
		} `json:"data"`
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/units", nil)
	e.mustJSON(st, body, 200, &units)
	if len(units.Data) == 0 || units.Data[0].Unit.ID != e.refs.UnitA1201 || units.Data[0].Counts.UpcomingBookings != 1 {
		t.Fatalf("upcoming_bookings harus per unit (booking anggota unit 1201): %s", body)
	}

	// ---- PATCH /tenant/me: string kosong mengosongkan telepon ----
	st, body = e.do(tenA, http.MethodPatch, "/api/v1/tenant/me", map[string]any{"phone": ""})
	if st != 200 || !strings.Contains(string(body), `"phone":null`) {
		t.Fatalf("telepon harus dapat dikosongkan: %d %s", st, body)
	}

	// ---- preferensi notifikasi: kanal yang tidak dikirim tidak berubah ----
	pref := func() map[string]any {
		st, body := e.do(tenA, http.MethodGet, "/api/v1/notifications/preferences", nil)
		for _, p := range list(e.jm(st, body, 200)["data"]) {
			if p["type"] == c.prefType {
				return p
			}
		}
		t.Fatalf("preferensi %s tidak ada", c.prefType)
		return nil
	}
	if st, body := e.do(tenA, http.MethodPut, "/api/v1/notifications/preferences", map[string]any{"type": c.prefType, "push": false}); st != 204 {
		t.Fatalf("set preferensi: %d %s", st, body)
	}
	if p := pref(); p["push"] != false || p["inapp"] != true {
		t.Fatalf("hanya push yang berubah: %v", p)
	}
	if st, body := e.do(tenA, http.MethodPut, "/api/v1/notifications/preferences", map[string]any{"type": c.prefType, "inapp": false}); st != 204 {
		t.Fatalf("set preferensi: %d %s", st, body)
	}
	if p := pref(); p["push"] != false || p["inapp"] != false {
		t.Fatalf("push lama harus dipertahankan: %v", p)
	}

	// ---- tag Web Push per object (object_type:object_id) ----
	if len(c.fake.sent) == 0 || c.fake.sent[0]["tag"] != "package:"+c.pkgID.String() {
		t.Fatalf("tag web push harus object_type:object_id: %+v", c.fake.sent)
	}
}
