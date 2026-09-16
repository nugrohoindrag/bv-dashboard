package app_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// setupTenants: dua akun tenant aktif (unit 1201 & 1202) — helper untuk test P1.3+.
func (e *env) setupTenants(t *testing.T) (tenA, tenB string) {
	t.Helper()
	e.app.TenantApp.SetRegistrationRateLimit(0)
	tr := e.login("tr.manager@demo.buildingvision.id")
	reg := func(email, name string, unit uuid.UUID) uuid.UUID {
		st, body := e.do("", http.MethodPost, "/api/v1/tenant/register", map[string]any{"organization_slug": "org-a", "full_name": name, "email": email, "password": "Tenant12345", "property_id": e.refs.PropertyID, "unit_id": unit})
		var res struct {
			TenantUserID uuid.UUID `json:"tenant_user_id"`
		}
		e.mustJSON(st, body, 201, &res)
		st, body = e.do(tr, http.MethodPost, "/api/v1/tenant-users/"+res.TenantUserID.String()+"/approve", nil)
		e.mustJSON(st, body, 200, nil)
		return res.TenantUserID
	}
	reg("dewi@tenant.test", "Dewi Lestari", e.refs.UnitA1201)
	reg("rudi@tenant.test", "Rudi Hartono", e.refs.UnitA1202)
	_, ra := e.loginTenant(t, "dewi@tenant.test", "Tenant12345")
	_, rb := e.loginTenant(t, "rudi@tenant.test", "Tenant12345")
	return ra["access_token"].(string), rb["access_token"].(string)
}

type bookingResp struct {
	ID             uuid.UUID `json:"id"`
	BookingNumber  string    `json:"booking_number"`
	Status         string    `json:"status"`
	AllowedActions []string  `json:"allowed_actions"`
}

// P1.3 — Facility Booking (PRD §21, WF-P1-004, AT-P1-010, DoD #26–27) & Visitor (PRD §22, WF-P1-005, AT-P1-011, DoD #28–29).
func TestFacilityBookingAndVisitor(t *testing.T) {
	e := setup(t)
	tenA, tenB := e.setupTenants(t)
	tr := e.login("tr.manager@demo.buildingvision.id")
	pm := e.login("pm@demo.buildingvision.id")
	sec := e.login("wawan@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")

	// ---- Facility (PM membuat; 24 jam agar test tidak bergantung jam) ----
	var fac struct {
		ID                uuid.UUID `json:"id"`
		FacilityCode      string    `json:"facility_code"`
		EffectiveApproval bool      `json:"effective_approval"`
	}
	st, body := e.do(pm, http.MethodPost, "/api/v1/facilities", map[string]any{"property_id": e.refs.PropertyID, "name": "Meeting Room 1201", "facility_type": "meeting_room", "capacity": 8, "open_time": "00:00", "close_time": "23:59", "slot_minutes": 30, "min_duration_minutes": 30, "max_duration_minutes": 180})
	e.mustJSON(st, body, 201, &fac)
	if !strings.HasPrefix(fac.FacilityCode, "FCL-") || fac.EffectiveApproval {
		t.Fatalf("facility: %+v", fac)
	}
	// technician tidak punya booking.facilities.create
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/facilities", map[string]any{"property_id": e.refs.PropertyID, "name": "X"}); st != 403 {
		t.Fatalf("technician create facility: %d", st)
	}
	// tenant melihat fasilitas & availability
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/facilities", nil)
	if st != 200 || !strings.Contains(string(body), "Meeting Room 1201") {
		t.Fatalf("tenant facilities: %d %s", st, body)
	}
	// deterministik: besok 09:00 waktu property (Asia/Jakarta) agar seluruh offset test tetap dalam satu hari operasional
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	nowJ := time.Now().In(jkt)
	start := time.Date(nowJ.Year(), nowJ.Month(), nowJ.Day()+1, 9, 0, 0, 0, jkt)
	end := start.Add(60 * time.Minute)
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/facilities/"+fac.ID.String()+"/availability?date="+start.Format("2006-01-02"), nil)
	var av struct {
		Data []struct {
			Available bool `json:"available"`
		} `json:"data"`
	}
	e.mustJSON(st, body, 200, &av)
	if len(av.Data) == 0 {
		t.Fatal("availability kosong")
	}
	// ---- Booking tenant A: tanpa approval → confirmed ----
	var b1 bookingResp
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/bookings", map[string]any{"facility_id": fac.ID, "starts_at": start, "ends_at": end, "attendees": 4, "purpose": "Rapat internal"})
	e.mustJSON(st, body, 201, &b1)
	if !strings.HasPrefix(b1.BookingNumber, "BKG-") || b1.Status != "confirmed" || !has(b1.AllowedActions, "cancel") {
		t.Fatalf("booking A: %+v", b1)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "booking_confirmed") {
		t.Fatalf("notifikasi booking confirmed ke tenant tidak ada: %+v", ib.Data)
	}
	// AT-P1-010 / DoD #27: double booking (overlap) ditolak DB → 409 BOOKING_CONFLICT
	st, body = e.do(tenB, http.MethodPost, "/api/v1/tenant/bookings", map[string]any{"facility_id": fac.ID, "starts_at": start.Add(30 * time.Minute), "ends_at": end.Add(30 * time.Minute), "purpose": "Overlap"})
	if st != 409 || !strings.Contains(string(body), "BOOKING_CONFLICT") {
		t.Fatalf("AT-P1-010 double booking harus 409: %d %s", st, body)
	}
	// slot persis setelahnya boleh ([) range)
	var b2 bookingResp
	st, body = e.do(tenB, http.MethodPost, "/api/v1/tenant/bookings", map[string]any{"facility_id": fac.ID, "starts_at": end, "ends_at": end.Add(60 * time.Minute), "purpose": "Setelah A"})
	e.mustJSON(st, body, 201, &b2)
	// kapasitas
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/tenant/bookings", map[string]any{"facility_id": fac.ID, "starts_at": end.Add(2 * time.Hour), "ends_at": end.Add(3 * time.Hour), "attendees": 20}); st != 400 {
		t.Fatalf("kapasitas terlampaui harus 400: %d", st)
	}
	// durasi di atas max
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/tenant/bookings", map[string]any{"facility_id": fac.ID, "starts_at": end.Add(4 * time.Hour), "ends_at": end.Add(8 * time.Hour)}); st != 400 {
		t.Fatalf("durasi > max harus 400: %d", st)
	}
	// isolasi: tenant B tidak melihat booking A
	if st, _ := e.do(tenB, http.MethodGet, "/api/v1/tenant/bookings/"+b1.ID.String(), nil); st != 404 {
		t.Fatalf("booking lintas tenant: %d", st)
	}
	// availability sekarang menandai slot terpakai
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/facilities/"+fac.ID.String()+"/availability?date="+start.Format("2006-01-02"), nil)
	if st != 200 || !strings.Contains(string(body), `"reason":"booked"`) {
		t.Fatalf("availability booked: %d %s", st, body)
	}
	// staf melihat daftar booking property; tenant tidak boleh akses endpoint staf
	st, body = e.do(tr, http.MethodGet, "/api/v1/bookings?property_id="+e.refs.PropertyID.String()+"&upcoming=true", nil)
	var list struct {
		Data []bookingResp `json:"data"`
	}
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 2 {
		t.Fatalf("bookings staf: %d", len(list.Data))
	}
	if st, _ := e.do(tenA, http.MethodGet, "/api/v1/bookings", nil); st != 403 {
		t.Fatalf("tenant akses /bookings harus 403: %d", st)
	}
	// tenant A cancel → slot bebas lagi; staff check-in booking B nanti (status confirmed → check_in)
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/bookings/"+b1.ID.String()+"/cancel", map[string]any{"reason": "Rapat dibatalkan"})
	e.mustJSON(st, body, 200, &b1)
	if b1.Status != "cancelled" {
		t.Fatalf("cancel: %s", b1.Status)
	}
	var b3 bookingResp
	st, body = e.do(tenB, http.MethodPost, "/api/v1/tenant/bookings", map[string]any{"facility_id": fac.ID, "starts_at": start, "ends_at": end, "purpose": "Ambil slot yang dibatalkan"})
	e.mustJSON(st, body, 201, &b3)

	// ---- Approval model (OD-P1-007): property config → pending → TR approve ----
	st, body = e.do(pm, http.MethodPatch, "/api/v1/properties/"+e.refs.PropertyID.String()+"/profile-config", map[string]any{"booking_approval_required": true})
	e.mustJSON(st, body, 200, nil)
	var b4 bookingResp
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/bookings", map[string]any{"facility_id": fac.ID, "starts_at": end.Add(5 * time.Hour), "ends_at": end.Add(6 * time.Hour), "purpose": "Perlu approval"})
	e.mustJSON(st, body, 201, &b4)
	if b4.Status != "pending" {
		t.Fatalf("booking dengan approval harus pending: %s", b4.Status)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tr); !hasType(ib, "booking_received") {
		t.Fatalf("notifikasi booking pending ke Tenant Relation tidak ada: %+v", ib.Data)
	}
	// technician tidak boleh approve
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/bookings/"+b4.ID.String()+"/approve", nil); st != 403 {
		t.Fatalf("technician approve booking: %d", st)
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/bookings/"+b4.ID.String()+"/approve", nil)
	e.mustJSON(st, body, 200, &b4)
	if b4.Status != "confirmed" {
		t.Fatalf("approve: %s", b4.Status)
	}
	st, body = e.do(tr, http.MethodPost, "/api/v1/bookings/"+b4.ID.String()+"/check_in", nil)
	e.mustJSON(st, body, 200, &b4)
	st, body = e.do(tr, http.MethodPost, "/api/v1/bookings/"+b4.ID.String()+"/complete", nil)
	e.mustJSON(st, body, 200, &b4)
	if b4.Status != "completed" {
		t.Fatalf("complete: %s", b4.Status)
	}
	// closure fasilitas memblokir booking baru
	st, body = e.do(pm, http.MethodPost, "/api/v1/facilities/"+fac.ID.String()+"/schedules", map[string]any{"kind": "closure", "starts_at": end.Add(10 * time.Hour), "ends_at": end.Add(14 * time.Hour), "reason": "Maintenance AC"})
	e.mustJSON(st, body, 201, nil)
	if st, body := e.do(tenA, http.MethodPost, "/api/v1/tenant/bookings", map[string]any{"facility_id": fac.ID, "starts_at": end.Add(11 * time.Hour), "ends_at": end.Add(12 * time.Hour)}); st != 409 || !strings.Contains(string(body), "FACILITY_CLOSED") {
		t.Fatalf("booking saat closure harus 409: %d %s", st, body)
	}

	// ---- Visitor (PRD §22) ----
	var v1 struct {
		ID             uuid.UUID `json:"id"`
		VisitorNumber  string    `json:"visitor_number"`
		Status         string    `json:"status"`
		HostUnitLabel  *string   `json:"host_unit_label"`
		IDNumberMasked *string   `json:"id_number_masked"`
		Pass           *struct {
			PassCode  string `json:"pass_code"`
			QRPayload string `json:"qr_payload"`
			Status    string `json:"status"`
		} `json:"pass"`
		AllowedActions []string `json:"allowed_actions"`
	}
	exp := time.Now().Add(3 * time.Hour)
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/visitors", map[string]any{"visitor_name": "Andi Kurir", "visitor_phone": "0813", "purpose": "Antar dokumen", "vehicle_plate": "B 1234 XY", "id_number": "3171012345678901", "expected_at": exp})
	e.mustJSON(st, body, 201, &v1)
	if !strings.HasPrefix(v1.VisitorNumber, "VIS-") || v1.Status != "registered" || v1.Pass == nil || v1.Pass.Status != "active" || v1.HostUnitLabel == nil || !strings.Contains(*v1.HostUnitLabel, "1201") {
		t.Fatalf("visitor: %+v", v1)
	}
	if v1.IDNumberMasked == nil || !strings.HasSuffix(*v1.IDNumberMasked, "8901") || strings.Contains(*v1.IDNumberMasked, "3171") {
		t.Fatalf("id_number harus dimasking: %v", v1.IDNumberMasked)
	}
	// unit tenant lain sebagai host → 403
	if st, _ := e.do(tenA, http.MethodPost, "/api/v1/tenant/visitors", map[string]any{"visitor_name": "X", "expected_at": exp, "host_unit_location_id": e.refs.UnitA1202}); st != 403 {
		t.Fatalf("host unit lain harus 403: %d", st)
	}
	// isolasi lintas tenant
	if st, _ := e.do(tenB, http.MethodGet, "/api/v1/tenant/visitors/"+v1.ID.String(), nil); st != 404 {
		t.Fatalf("visitor lintas tenant: %d", st)
	}
	e.dispatch(t)
	// AT-P1-011: Security melihat & memverifikasi (resolve QR → check-in → check-out)
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	if ib := e.inboxOf(t, secSpv); !hasType(ib, "visitor_registered") {
		t.Fatalf("notifikasi visitor ke Security supervisor tidak ada: %+v", ib.Data)
	}
	st, body = e.do(sec, http.MethodGet, "/api/v1/visitors/resolve?code="+v1.Pass.QRPayload, nil)
	var vs struct {
		ID             uuid.UUID `json:"id"`
		Status         string    `json:"status"`
		AllowedActions []string  `json:"allowed_actions"`
	}
	e.mustJSON(st, body, 200, &vs)
	if vs.ID != v1.ID || !has(vs.AllowedActions, "check_in") {
		t.Fatalf("resolve pass: %+v", vs)
	}
	st, body = e.do(sec, http.MethodPost, "/api/v1/visitors/"+v1.ID.String()+"/check_in", map[string]any{"note": "KTP dicek"})
	e.mustJSON(st, body, 200, &vs)
	if vs.Status != "checked_in" {
		t.Fatalf("check_in: %s", vs.Status)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "visitor_arrived") {
		t.Fatalf("notifikasi tamu tiba ke tenant tidak ada: %+v", ib.Data)
	}
	st, body = e.do(sec, http.MethodPost, "/api/v1/visitors/"+v1.ID.String()+"/check_out", nil)
	e.mustJSON(st, body, 200, &vs)
	if vs.Status != "checked_out" {
		t.Fatalf("check_out: %s", vs.Status)
	}
	// pass sudah terpakai → tidak bisa check-in lagi (tidak ada aksi)
	st, body = e.do(sec, http.MethodGet, "/api/v1/visitors/"+v1.ID.String(), nil)
	e.mustJSON(st, body, 200, &vs)
	if has(vs.AllowedActions, "check_in") {
		t.Fatalf("checked_out tidak boleh check_in lagi: %v", vs.AllowedActions)
	}
	// technician tidak punya security.visitors.view
	if st, _ := e.do(tech, http.MethodGet, "/api/v1/visitors", nil); st != 403 {
		t.Fatalf("technician visitors: %d", st)
	}
	// approval model visitor (OD-P1-008)
	st, body = e.do(pm, http.MethodPatch, "/api/v1/properties/"+e.refs.PropertyID.String()+"/profile-config", map[string]any{"visitor_approval_required": true})
	e.mustJSON(st, body, 200, nil)
	v1.Pass = nil // struct dipakai ulang; field omitempty tidak di-reset oleh Unmarshal
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/visitors", map[string]any{"visitor_name": "Tamu Malam", "expected_at": exp.Add(2 * time.Hour)})
	e.mustJSON(st, body, 201, &v1)
	if v1.Status != "pending_approval" || v1.Pass != nil {
		t.Fatalf("visitor pending approval: %+v", v1)
	}
	// officer tidak punya manage → tidak bisa approve; supervisor bisa
	if st, _ := e.do(sec, http.MethodPost, "/api/v1/visitors/"+v1.ID.String()+"/approve", nil); st != 409 && st != 403 {
		t.Fatalf("officer approve: %d", st)
	}
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/visitors/"+v1.ID.String()+"/approve", nil)
	e.mustJSON(st, body, 200, &v1)
	if v1.Status != "registered" || v1.Pass == nil {
		t.Fatalf("approve visitor: %+v", v1)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "visitor_approved") {
		t.Fatalf("notifikasi visitor approved ke tenant tidak ada")
	}
}
