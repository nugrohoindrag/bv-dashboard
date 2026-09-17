package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// BVRooms — Customer Booking App (BVRooms-Backend-Requirements v0.2): white-label per org (D1), hotel & apartemen (D4),
// OTP mock (D3), pembayaran fase hold manual_transfer / pay_at_property (D2), add-on dari dashboard (D5), review bintang (§10).

type bvBooking struct {
	ID              uuid.UUID  `json:"id"`
	BookingCode     string     `json:"booking_code"`
	Status          string     `json:"status"`
	PaymentStatus   string     `json:"payment_status"`
	PaymentDeadline *time.Time `json:"payment_deadline_at"`
	Nights          int        `json:"nights"`
	RoomsCount      int        `json:"rooms_count"`
	PrimaryAction   string     `json:"primary_action"`
	CanCancel       bool       `json:"can_cancel"`
	CanReview       bool       `json:"can_review"`
	Totals          struct {
		Room, Addon, Discount, Total int64
	} `json:"totals"`
	Rooms []struct {
		ReservationID  uuid.UUID `json:"reservation_id"`
		ReservationNo  string    `json:"reservation_number"`
		Kind           string    `json:"kind"`
		ReservationSt  string    `json:"reservation_status"`
		AssignedNumber *string   `json:"assigned_number"`
		LineTotal      int64     `json:"line_total"`
		Addons         []struct {
			Name      string `json:"name"`
			Qty       int    `json:"qty"`
			LineTotal int64  `json:"line_total"`
		} `json:"addons"`
	} `json:"rooms"`
	Payment *struct {
		ID            uuid.UUID       `json:"id"`
		MethodCode    string          `json:"method_code"`
		Status        string          `json:"status"`
		Instructions  json.RawMessage `json:"instructions"`
		ProofRequired bool            `json:"proof_upload_required"`
	} `json:"payment"`
	Review *struct {
		Stars       int    `json:"stars"`
		DisplayName string `json:"display_name"`
	} `json:"review"`
}

// bvCustomer: registrasi customer lewat OTP mock (dev_code) → access token.
func (e *env) bvCustomer(t *testing.T, org, phone, name, email string) string {
	t.Helper()
	var otp struct {
		DevCode string `json:"dev_code"`
	}
	st, body := e.do("", http.MethodPost, "/api/v1/bvrooms/auth/otp/request", map[string]any{"organization_slug": org, "phone": phone, "purpose": "register"})
	e.mustJSON(st, body, 200, &otp)
	if len(otp.DevCode) != 4 {
		t.Fatalf("dev_code OTP harus 4 digit (env test, provider mock): %s", body)
	}
	var ver struct {
		OTPToken string `json:"otp_token"`
	}
	st, body = e.do("", http.MethodPost, "/api/v1/bvrooms/auth/otp/verify", map[string]any{"organization_slug": org, "phone": phone, "purpose": "register", "code": otp.DevCode, "device_id": "test-device"})
	e.mustJSON(st, body, 200, &ver)
	var reg struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Customer     struct {
			Phone string `json:"phone"`
		} `json:"customer"`
	}
	st, body = e.do("", http.MethodPost, "/api/v1/bvrooms/auth/register", map[string]any{"organization_slug": org, "otp_token": ver.OTPToken, "full_name": name, "email": email})
	e.mustJSON(st, body, 201, &reg)
	if !strings.HasPrefix(reg.Customer.Phone, "+62") {
		t.Fatalf("phone harus E.164: %+v", reg.Customer)
	}
	return reg.AccessToken
}

func (e *env) bvInbox(t *testing.T, tok string) []string {
	t.Helper()
	var inbox struct {
		Data []struct {
			Type  string `json:"type"`
			Title string `json:"title"`
		} `json:"data"`
	}
	st, body := e.do(tok, http.MethodGet, "/api/v1/bvrooms/customers/me/notifications", nil)
	e.mustJSON(st, body, 200, &inbox)
	var types []string
	for _, n := range inbox.Data {
		types = append(types, n.Type)
	}
	return types
}

func TestBVRoomsHotelFlow(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	d := func(tm time.Time) string { return tm.Format("2006-01-02") }

	// ---- inventori hotel (modul hotel existing) ----
	var prop, bld, floor, rt struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "property", "name": "BV Grand Hotel", "details": map[string]any{"timezone": "Asia/Jakarta", "profile": "hotel"}})
	e.mustJSON(st, body, 201, &prop)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "building", "parent_id": prop.ID, "name": "Main", "details": map[string]any{}})
	e.mustJSON(st, body, 201, &bld)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "floor", "parent_id": bld.ID, "name": "Floor 3", "details": map[string]any{"floor_number": 3}})
	e.mustJSON(st, body, 201, &floor)
	st, body = e.do(admin, http.MethodPost, "/api/v1/teams", map[string]any{"property_id": prop.ID, "name": "HK Hotel", "domain": "housekeeping"})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/room-types", map[string]any{"property_id": prop.ID, "name": "Double Bed Studio", "capacity_adults": 3, "capacity_children": 1, "base_rate": 1280000, "amenities": []string{"wifi", "ac", "tv"}})
	e.mustJSON(st, body, 201, &rt)
	var r301, r302 roomResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/rooms", map[string]any{"property_id": prop.ID, "floor_id": floor.ID, "room_type_id": rt.ID, "room_number": "301"})
	e.mustJSON(st, body, 201, &r301)
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/rooms", map[string]any{"property_id": prop.ID, "floor_id": floor.ID, "room_type_id": rt.ID, "room_number": "302"})
	e.mustJSON(st, body, 201, &r302)

	// ---- listing (dashboard): draft otomatis → lengkapi → tayang ----
	var listing struct {
		Slug     string `json:"slug"`
		Listed   bool   `json:"bvrooms_listed"`
		Category string `json:"listing_category"`
		Version  int    `json:"version"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/bvrooms/admin/properties/"+prop.ID.String()+"/listing", nil)
	e.mustJSON(st, body, 200, &listing)
	if listing.Slug != "bv-grand-hotel" || listing.Listed || listing.Category != "hotel" {
		t.Fatalf("listing draft: %+v", listing)
	}
	// belum tayang → katalog publik kosong
	var cat struct {
		Data []struct {
			Slug     string   `json:"slug"`
			MinRate  *int64   `json:"min_rate"`
			HasPromo bool     `json:"has_promo"`
			Distance *float64 `json:"distance_km"`
		} `json:"data"`
	}
	st, body = e.do("", http.MethodGet, "/api/v1/bvrooms/catalog/properties?organization_slug=org-a", nil)
	e.mustJSON(st, body, 200, &cat)
	if len(cat.Data) != 0 {
		t.Fatalf("properti belum listed tidak boleh tampil: %s", body)
	}
	st, body = e.do(admin, http.MethodPut, "/api/v1/bvrooms/admin/properties/"+prop.ID.String()+"/listing", map[string]any{
		"bvrooms_listed": true, "display_name": "BV Grand Hotel Jakarta", "address_line": "Jl. Fatmawati 88", "district": "Gandaria Utara", "city": "Jakarta Selatan", "lat": -6.26, "lng": 106.79,
		"phone": "+622112345", "check_in_time": "14:00", "check_out_time": "12:00", "facilities": []string{"wifi", "parking", "pool"}, "policies": []string{"Tamu harus membawa KTP saat check in."},
		"description_sections": []map[string]string{{"key": "lokasi", "title": "Lokasi", "body": "Dekat ITC Fatmawati"}},
		"bank_accounts":        []map[string]string{{"bank": "BCA", "account_number": "1234567890", "account_name": "PT BV Hotel"}}, "allow_pay_at_property": true, "payment_window_hours": 5,
	})
	e.mustJSON(st, body, 200, &listing)
	if !listing.Listed {
		t.Fatalf("listing harus tayang: %s", body)
	}
	// add-on default (breakfast, extra bed) dibuat nonaktif → aktifkan breakfast Rp 25.000/tamu/malam
	var addons struct {
		Data []struct {
			ID   uuid.UUID `json:"id"`
			Kind string    `json:"kind"`
		} `json:"data"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/bvrooms/admin/properties/"+prop.ID.String()+"/addons", nil)
	e.mustJSON(st, body, 200, &addons)
	var breakfastID uuid.UUID
	for _, a := range addons.Data {
		if a.Kind == "breakfast" {
			breakfastID = a.ID
		}
	}
	if breakfastID == uuid.Nil {
		t.Fatalf("add-on default breakfast harus ada: %s", body)
	}
	st, body = e.do(admin, http.MethodPatch, "/api/v1/bvrooms/admin/properties/"+prop.ID.String()+"/addons/"+breakfastID.String(), map[string]any{"price": 25000, "max_qty": 3, "is_active": true})
	e.mustJSON(st, body, 200, nil)
	// promo 10% otomatis (§10) + banner
	st, body = e.do(admin, http.MethodPost, "/api/v1/bvrooms/admin/promotions", map[string]any{"property_id": prop.ID, "name": "Diskon Pembuka", "discount_type": "percent", "discount_value": 10})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do(admin, http.MethodPost, "/api/v1/bvrooms/admin/banners", map[string]any{"title": "Menginap Mudah Tanpa Repot", "cta_label": "Mulai Sekarang"})
	e.mustJSON(st, body, 201, nil)

	// ---- publik: app-config, katalog, detail, tipe kamar ----
	var cfg struct {
		Organization struct {
			Name string `json:"name"`
		} `json:"organization"`
		Features struct {
			OnlinePayment bool `json:"online_payment"`
			PayAtProperty bool `json:"pay_at_property"`
		} `json:"features"`
		ListedCount    int     `json:"listed_count"`
		SingleProperty *string `json:"single_property_slug"`
	}
	st, body = e.do("", http.MethodGet, "/api/v1/bvrooms/app-config?organization_slug=org-a", nil)
	e.mustJSON(st, body, 200, &cfg)
	if cfg.Features.OnlinePayment || !cfg.Features.PayAtProperty || cfg.ListedCount != 1 || cfg.SingleProperty == nil || *cfg.SingleProperty != "bv-grand-hotel" {
		t.Fatalf("app-config: %s", body)
	}
	st, body = e.do("", http.MethodGet, "/api/v1/bvrooms/catalog/properties?organization_slug=org-a&lat=-6.2&lng=106.8&sort=distance", nil)
	e.mustJSON(st, body, 200, &cat)
	if len(cat.Data) != 1 || cat.Data[0].Slug != "bv-grand-hotel" || cat.Data[0].MinRate == nil || *cat.Data[0].MinRate != 1280000 || !cat.Data[0].HasPromo || cat.Data[0].Distance == nil {
		t.Fatalf("katalog: %s", body)
	}
	// org lain tidak melihat listing org-a (D1: white-label per org)
	st, body = e.do("", http.MethodGet, "/api/v1/bvrooms/catalog/properties?organization_slug=org-b", nil)
	e.mustJSON(st, body, 200, &cat)
	if len(cat.Data) != 0 {
		t.Fatalf("isolasi org: %s", body)
	}
	var detail struct {
		Terminology    map[string]string `json:"terminology"`
		PaymentOptions []struct {
			MethodCode string `json:"method_code"`
			Enabled    bool   `json:"enabled"`
			ComingSoon bool   `json:"coming_soon"`
		} `json:"payment_options"`
		RatingSummary struct {
			Label string `json:"label"`
		} `json:"rating_summary"`
	}
	st, body = e.do("", http.MethodGet, "/api/v1/bvrooms/catalog/properties/bv-grand-hotel?organization_slug=org-a", nil)
	e.mustJSON(st, body, 200, &detail)
	if detail.Terminology["unit_label"] != "Kamar" || detail.RatingSummary.Label != "No Review Yet" {
		t.Fatalf("detail: %s", body)
	}
	var vaMock, transfer, cash bool
	for _, o := range detail.PaymentOptions {
		switch o.MethodCode {
		case "bca_va":
			vaMock = !o.Enabled && o.ComingSoon
		case "transfer_bca":
			transfer = o.Enabled
		case "cash_on_site":
			cash = o.Enabled
		}
	}
	if !vaMock || !transfer || !cash {
		t.Fatalf("payment options (D2: VA mockup disabled): %s", body)
	}
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(jkt)
	// check-in hari ini agar Front Office boleh check-in (hotel: check_in hanya bila tanggal ≤ hari ini)
	ci := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jkt)
	co := ci.AddDate(0, 0, 2)
	var types struct {
		Data []struct {
			ID             uuid.UUID `json:"id"`
			Kind           string    `json:"kind"`
			RatePerNight   int64     `json:"rate_per_night"`
			LineTotal      int64     `json:"line_total"`
			AvailableCount int       `json:"available_count"`
			IsAvailable    bool      `json:"is_available"`
			Promo          *struct {
				DiscountAmount int64 `json:"discount_amount"`
			} `json:"promo"`
			Addons []struct {
				ID   uuid.UUID `json:"id"`
				Kind string    `json:"kind"`
			} `json:"addons"`
		} `json:"data"`
	}
	st, body = e.do("", http.MethodGet, "/api/v1/bvrooms/catalog/properties/bv-grand-hotel/room-types?organization_slug=org-a&check_in="+d(ci)+"&check_out="+d(co)+"&rooms=2&adults=4", nil)
	e.mustJSON(st, body, 200, &types)
	if len(types.Data) != 1 || types.Data[0].Kind != "room_type" || types.Data[0].AvailableCount != 2 || !types.Data[0].IsAvailable || types.Data[0].Promo == nil || types.Data[0].Promo.DiscountAmount != 256000 || types.Data[0].LineTotal != 2304000 || len(types.Data[0].Addons) != 1 {
		t.Fatalf("room-types: %s", body)
	}

	// ---- customer: OTP register/login ----
	cust := e.bvCustomer(t, "org-a", "0812 3456 789", "Annahl Prayitno", "annahl@guest.test")
	// login: nomor belum terdaftar → 404 PHONE_NOT_REGISTERED; nomor terdaftar → OTP → token
	if st, body := e.do("", http.MethodPost, "/api/v1/bvrooms/auth/otp/request", map[string]any{"organization_slug": "org-a", "phone": "0899000000", "purpose": "login"}); st != 404 || !strings.Contains(string(body), "PHONE_NOT_REGISTERED") {
		t.Fatalf("login nomor tak terdaftar harus 404: %d %s", st, body)
	}
	if st, body := e.do("", http.MethodPost, "/api/v1/bvrooms/auth/otp/request", map[string]any{"organization_slug": "org-a", "phone": "+628123456789", "purpose": "login"}); st != 429 {
		t.Fatalf("resend < 60 detik harus 429: %d %s", st, body)
	}
	// ---- customer: PIN (pengganti OTP selama vendor SMS di-hold) ----
	// akun lama (register via OTP, pin_hash NULL) → PIN default 1234, pin_is_default=true
	var pinLogin struct {
		AccessToken  string `json:"access_token"`
		PINIsDefault bool   `json:"pin_is_default"`
	}
	st, body = e.do("", http.MethodPost, "/api/v1/bvrooms/auth/pin/login", map[string]any{"organization_slug": "org-a", "phone": "0812 3456 789", "pin": "1234", "device_id": "pin-dev"})
	e.mustJSON(st, body, 200, &pinLogin)
	if pinLogin.AccessToken == "" || !pinLogin.PINIsDefault {
		t.Fatalf("login PIN default harus sukses + pin_is_default: %s", body)
	}
	if st, body := e.do("", http.MethodPost, "/api/v1/bvrooms/auth/pin/login", map[string]any{"organization_slug": "org-a", "phone": "0812 3456 789", "pin": "9999"}); st != 401 || !strings.Contains(string(body), "PIN_INVALID") {
		t.Fatalf("PIN salah harus 401 PIN_INVALID: %d %s", st, body)
	}
	if st, body := e.do("", http.MethodPost, "/api/v1/bvrooms/auth/pin/login", map[string]any{"organization_slug": "org-a", "phone": "0899000000", "pin": "1234"}); st != 404 || !strings.Contains(string(body), "PHONE_NOT_REGISTERED") {
		t.Fatalf("login PIN nomor tak terdaftar harus 404: %d %s", st, body)
	}
	// ganti PIN: lama = default → baru 4321; default tidak berlaku lagi
	if st, body := e.do(pinLogin.AccessToken, http.MethodPost, "/api/v1/bvrooms/auth/pin/change", map[string]any{"current_pin": "1234", "new_pin": "4321"}); st != 200 {
		t.Fatalf("ganti PIN: %d %s", st, body)
	}
	if st, _ := e.do("", http.MethodPost, "/api/v1/bvrooms/auth/pin/login", map[string]any{"organization_slug": "org-a", "phone": "0812 3456 789", "pin": "1234"}); st != 401 {
		t.Fatalf("PIN default setelah diganti harus 401: %d", st)
	}
	pinLogin.PINIsDefault = false
	st, body = e.do("", http.MethodPost, "/api/v1/bvrooms/auth/pin/login", map[string]any{"organization_slug": "org-a", "phone": "0812 3456 789", "pin": "4321"})
	e.mustJSON(st, body, 200, &pinLogin)
	if pinLogin.PINIsDefault {
		t.Fatalf("setelah ganti PIN pin_is_default harus false: %s", body)
	}
	// register langsung dengan PIN (tanpa OTP) → 201 + sesi; nomor sama → 409; PIN bukan 4 digit → 422
	var pinReg struct {
		AccessToken string `json:"access_token"`
		Customer    struct {
			Phone string `json:"phone"`
		} `json:"customer"`
	}
	st, body = e.do("", http.MethodPost, "/api/v1/bvrooms/auth/pin/register", map[string]any{"organization_slug": "org-a", "phone": "0813 0000 111", "full_name": "Dina Putri", "email": "dina@guest.test", "pin": "2468", "device_id": "pin-dev"})
	e.mustJSON(st, body, 201, &pinReg)
	if pinReg.AccessToken == "" || pinReg.Customer.Phone != "+628130000111" {
		t.Fatalf("register PIN: %s", body)
	}
	if st, body := e.do("", http.MethodPost, "/api/v1/bvrooms/auth/pin/register", map[string]any{"organization_slug": "org-a", "phone": "0813 0000 111", "full_name": "Dina Putri", "email": "dina@guest.test", "pin": "2468"}); st != 409 || !strings.Contains(string(body), "PHONE_EXISTS") {
		t.Fatalf("register PIN nomor sama harus 409: %d %s", st, body)
	}
	if st, _ := e.do("", http.MethodPost, "/api/v1/bvrooms/auth/pin/register", map[string]any{"organization_slug": "org-a", "phone": "0813 0000 222", "full_name": "X", "email": "x@guest.test", "pin": "12"}); st != 422 && st != 400 {
		t.Fatalf("PIN bukan 4 digit harus validation error: %d", st)
	}
	// ganti nomor HP lewat PATCH customers/me + pin
	if st, body := e.do(pinReg.AccessToken, http.MethodPatch, "/api/v1/bvrooms/customers/me", map[string]any{"phone": "0813 0000 333", "pin": "0000"}); st != 401 {
		t.Fatalf("ganti nomor dengan PIN salah harus 401: %d %s", st, body)
	}
	if st, body := e.do(pinReg.AccessToken, http.MethodPatch, "/api/v1/bvrooms/customers/me", map[string]any{"phone": "0813 0000 333", "pin": "2468"}); st != 200 || !strings.Contains(string(body), "+628130000333") {
		t.Fatalf("ganti nomor dengan PIN: %d %s", st, body)
	}
	// app-config mengumumkan metode auth
	if st, body := e.do("", http.MethodGet, "/api/v1/bvrooms/app-config?organization_slug=org-a", nil); st != 200 || !strings.Contains(string(body), `"auth_method":"pin"`) {
		t.Fatalf("app-config auth_method: %d %s", st, body)
	}

	// token customer tidak diterima endpoint staf, dan sebaliknya
	if st, _ := e.do(cust, http.MethodGet, "/api/v1/hotel/reservations?property_id="+prop.ID.String(), nil); st != 401 {
		t.Fatalf("token customer di endpoint staf harus 401: %d", st)
	}
	if st, _ := e.do(admin, http.MethodGet, "/api/v1/bvrooms/customers/me", nil); st != 401 {
		t.Fatalf("token staf di endpoint customer harus 401: %d", st)
	}

	// ---- booking 2 kamar + breakfast (Idempotency-Key) ----
	create := map[string]any{"property_id": prop.ID, "check_in": d(ci), "check_out": d(co), "guest": map[string]any{"full_name": "Annahl Prayitno", "email": "annahl@guest.test", "phone": "081244445555"},
		"rooms": []map[string]any{{"type_id": rt.ID, "adults": 2, "addons": []map[string]any{{"addon_id": breakfastID, "qty": 2}}}, {"type_id": rt.ID, "adults": 1}}}
	key := uuid.NewString()
	req := func() (int, []byte) {
		b, _ := json.Marshal(create)
		r, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/v1/bvrooms/bookings", bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+cust)
		r.Header.Set("Idempotency-Key", key)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.Bytes()
	}
	var bk bvBooking
	st, body = req()
	e.mustJSON(st, body, 201, &bk)
	if bk.Status != "UNPAID" || bk.RoomsCount != 2 || bk.Nights != 2 || len(bk.BookingCode) != 12 || bk.PaymentDeadline == nil || bk.PrimaryAction != "pay" {
		t.Fatalf("booking: %s", body)
	}
	// total: 2 kamar × 2 malam × 1.280.000 = 5.120.000; breakfast 25.000 × 2 tamu × 2 malam = 100.000; diskon 10% kamar = 512.000 → 4.708.000
	if bk.Totals.Room != 5120000 || bk.Totals.Addon != 100000 || bk.Totals.Discount != 512000 || bk.Totals.Total != 4708000 {
		t.Fatalf("totals: %+v", bk.Totals)
	}
	if len(bk.Rooms) != 2 || bk.Rooms[0].ReservationSt != "new" || len(bk.Rooms[0].Addons) != 1 || !strings.HasPrefix(bk.Rooms[0].ReservationNo, "RES-") {
		t.Fatalf("rooms: %s", body)
	}
	// retry dengan key sama → replay (bukan booking baru)
	var bk2 bvBooking
	st, body = req()
	e.mustJSON(st, body, 201, &bk2)
	if bk2.BookingCode != bk.BookingCode {
		t.Fatalf("idempotency: %s vs %s", bk.BookingCode, bk2.BookingCode)
	}
	// kapasitas tipe habis (2 kamar terpakai) → 409 ROOM_UNAVAILABLE; reservasi tampil di Front Office (source bvrooms)
	if st, body := e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings", map[string]any{"property_id": prop.ID, "check_in": d(ci), "check_out": d(co), "guest": map[string]any{"full_name": "X"}, "rooms": []map[string]any{{"type_id": rt.ID, "adults": 1}}}); st != 409 || !strings.Contains(string(body), "ROOM_UNAVAILABLE") {
		t.Fatalf("over-capacity harus 409: %d %s", st, body)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/hotel/reservations?property_id="+prop.ID.String(), nil)
	if st != 200 || !strings.Contains(string(body), `"source":"bvrooms"`) || !strings.Contains(string(body), `"bvrooms_booking_code":"`+bk.BookingCode+`"`) {
		t.Fatalf("reservasi BVRooms harus tampil di Front Office: %d %s", st, body)
	}
	// list upcoming & inbox
	var list struct {
		Data []struct {
			BookingCode   string `json:"booking_code"`
			Status        string `json:"status"`
			PrimaryAction string `json:"primary_action"`
		} `json:"data"`
	}
	st, body = e.do(cust, http.MethodGet, "/api/v1/bvrooms/bookings?scope=upcoming", nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 1 || list.Data[0].BookingCode != bk.BookingCode || list.Data[0].PrimaryAction != "pay" {
		t.Fatalf("upcoming: %s", body)
	}
	if got := e.bvInbox(t, cust); len(got) != 1 || got[0] != "booking_created" {
		t.Fatalf("inbox: %v", got)
	}

	// ---- pembayaran manual: transfer → bukti → verifikasi Finance ----
	if st, body := e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings/"+bk.BookingCode+"/payment", map[string]any{"method_code": "bca_va", "provider_code": "gateway"}); st != 422 || !strings.Contains(string(body), "METHOD_DISABLED") {
		t.Fatalf("VA hanya mockup (D2) harus 422: %d %s", st, body)
	}
	st, body = e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings/"+bk.BookingCode+"/payment", map[string]any{"method_code": "transfer_bca"})
	e.mustJSON(st, body, 200, &bk)
	if bk.Payment == nil || bk.Payment.Status != "pending" || !bk.Payment.ProofRequired || !strings.Contains(string(bk.Payment.Instructions), "1234567890") {
		t.Fatalf("payment transfer: %s", body)
	}
	var pre struct {
		UploadURL string `json:"upload_url"`
	}
	st, body = e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings/"+bk.BookingCode+"/payment/proof/presign", map[string]any{"content_type": "image/jpeg", "size_bytes": 1234})
	e.mustJSON(st, body, 201, &pre)
	// belum diunggah → 409
	if st, body := e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings/"+bk.BookingCode+"/payment/proof", nil); st != 409 {
		t.Fatalf("proof tanpa upload harus 409: %d %s", st, body)
	}
	storageKey := strings.TrimPrefix(pre.UploadURL, "http://storage.test/_dev/upload/")
	_ = e.store.Put(context.Background(), storageKey, "image/jpeg", strings.NewReader("jpegdata"), 8)
	st, body = e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings/"+bk.BookingCode+"/payment/proof", nil)
	e.mustJSON(st, body, 200, &bk)
	if bk.Payment.Status != "proof_submitted" || bk.Status != "UNPAID" {
		t.Fatalf("proof submitted: %s", body)
	}
	// Finance verifikasi (admin org punya bvrooms.payments.verify) → PAID, reservasi confirmed
	st, body = e.do(admin, http.MethodPost, "/api/v1/bvrooms/admin/payments/"+bk.Payment.ID.String()+"/verify", map[string]any{"note": "Transfer diterima"})
	e.mustJSON(st, body, 200, &bk)
	if bk.Status != "PAID" || bk.PaymentStatus != "paid" || bk.Rooms[0].ReservationSt != "confirmed" || bk.PrimaryAction != "direct" {
		t.Fatalf("verify: %s", body)
	}
	if got := e.bvInbox(t, cust); len(got) != 3 || got[0] != "payment_received" {
		t.Fatalf("inbox setelah verify: %v", got)
	}
	// ubah data tamu saat PAID → OK
	st, body = e.do(cust, http.MethodPatch, "/api/v1/bvrooms/bookings/"+bk.BookingCode+"/guest", map[string]any{"full_name": "Aan Prayitno", "email": "aanpray@gmail.com", "phone": "08123456789"})
	e.mustJSON(st, body, 200, nil)

	// ---- Front Office: check-in kamar 301 & 302 (hotel.Act) → CHECK IN → check-out → CHECK OUT → review ----
	st, body = e.do(admin, http.MethodPost, "/api/v1/hotel/reservations/"+bk.Rooms[0].ReservationID.String()+"/check_in", map[string]any{"room_location_id": r301.LocationID})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(cust, http.MethodGet, "/api/v1/bvrooms/bookings/"+bk.BookingCode, nil)
	e.mustJSON(st, body, 200, &bk)
	if bk.Status != "CHECK IN" || bk.Rooms[0].AssignedNumber == nil || *bk.Rooms[0].AssignedNumber != "301" || bk.CanCancel {
		t.Fatalf("check-in: %s", body)
	}
	// aksi dashboard BVRooms untuk kamar kedua (delegasi ke hotel.Act)
	st, body = e.do(admin, http.MethodPost, "/api/v1/bvrooms/admin/bookings/"+bk.ID.String()+"/check_in", map[string]any{"reservation_id": bk.Rooms[1].ReservationID, "room_location_id": r302.LocationID})
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(admin, http.MethodPost, "/api/v1/bvrooms/admin/bookings/"+bk.ID.String()+"/check_out", nil)
	e.mustJSON(st, body, 200, &bk)
	if bk.Status != "CHECK OUT" || !bk.CanReview || bk.PrimaryAction != "rebook" {
		t.Fatalf("check-out: %s", body)
	}
	got := e.bvInbox(t, cust)
	if len(got) < 5 || got[0] != "checked_out" || got[1] != "checked_in" {
		t.Fatalf("inbox check-in/out (dedupe per booking): %v", got)
	}
	// review bintang saja (§10), satu kali
	st, body = e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings/"+bk.BookingCode+"/review", map[string]any{"stars": 4})
	e.mustJSON(st, body, 201, nil)
	if st, body := e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings/"+bk.BookingCode+"/review", map[string]any{"stars": 5}); st != 409 {
		t.Fatalf("review ganda harus 409: %d %s", st, body)
	}
	var sum struct {
		Avg   float64 `json:"avg"`
		Count int     `json:"count"`
		Label string  `json:"label"`
		Hist  []int   `json:"hist"`
	}
	st, body = e.do("", http.MethodGet, "/api/v1/bvrooms/catalog/properties/bv-grand-hotel/reviews/summary?organization_slug=org-a", nil)
	e.mustJSON(st, body, 200, &sum)
	if sum.Avg != 4 || sum.Count != 1 || sum.Label != "Very Good" || sum.Hist[3] != 1 {
		t.Fatalf("summary: %s", body)
	}
	st, body = e.do(cust, http.MethodGet, "/api/v1/bvrooms/bookings?scope=history", nil)
	e.mustJSON(st, body, 200, &list)
	if len(list.Data) != 1 || list.Data[0].Status != "CHECK OUT" {
		t.Fatalf("history: %s", body)
	}

	// ---- cancel oleh customer (booking baru, UNPAID) ----
	ci2, co2 := co.AddDate(0, 0, 5), co.AddDate(0, 0, 6)
	var bk3 bvBooking
	st, body = e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings", map[string]any{"property_id": prop.ID, "check_in": d(ci2), "check_out": d(co2), "guest": map[string]any{"full_name": "Annahl"}, "rooms": []map[string]any{{"type_id": rt.ID, "adults": 1}}})
	e.mustJSON(st, body, 201, &bk3)
	if !bk3.CanCancel {
		t.Fatalf("booking mendatang harus bisa dibatalkan: %s", body)
	}
	st, body = e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings/"+bk3.BookingCode+"/cancel", map[string]any{"reason": "Berubah rencana"})
	e.mustJSON(st, body, 200, &bk3)
	if bk3.Status != "CANCELLED" || bk3.Rooms[0].ReservationSt != "cancelled" {
		t.Fatalf("cancel: %s", body)
	}
	if st, _ := e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings/"+bk3.BookingCode+"/cancel", map[string]any{}); st != 409 {
		t.Fatalf("cancel ganda harus 409: %d", st)
	}

	// ---- sweep: UNPAID lewat deadline → EXPIRED ("Pemesanan Hangus!") ----
	var bk4 bvBooking
	st, body = e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings", map[string]any{"property_id": prop.ID, "check_in": d(ci2), "check_out": d(co2), "guest": map[string]any{"full_name": "Annahl"}, "rooms": []map[string]any{{"type_id": rt.ID, "adults": 1}}})
	e.mustJSON(st, body, 201, &bk4)
	orgA := e.refs.OrgID
	if err := e.app.DB.WithOrgTx(context.Background(), orgA, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE bvrooms_bookings SET payment_deadline_at = now() - interval '1 minute' WHERE id = $1`, bk4.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := e.app.BVRooms.Sweep(context.Background(), orgA); err != nil || n != 1 {
		t.Fatalf("sweep: n=%d err=%v", n, err)
	}
	st, body = e.do(cust, http.MethodGet, "/api/v1/bvrooms/bookings/"+bk4.BookingCode, nil)
	e.mustJSON(st, body, 200, &bk4)
	if bk4.Status != "EXPIRED" || bk4.PaymentStatus != "expired" {
		t.Fatalf("expired: %s", body)
	}
	if got := e.bvInbox(t, cust); got[0] != "payment_expired" {
		t.Fatalf("inbox expired: %v", got)
	}
	// wishlist + hapus notifikasi
	st, body = e.do(cust, http.MethodPost, "/api/v1/bvrooms/customers/me/wishlist", map[string]any{"property_id": prop.ID})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do(cust, http.MethodGet, "/api/v1/bvrooms/catalog/properties?organization_slug=org-a", nil)
	if st != 200 || !strings.Contains(string(body), `"is_wishlisted":true`) {
		t.Fatalf("is_wishlisted: %d %s", st, body)
	}
	// dashboard: daftar booking & customer
	st, body = e.do(admin, http.MethodGet, "/api/v1/bvrooms/admin/bookings?property_id="+prop.ID.String()+"&status=EXPIRED", nil)
	if st != 200 || !strings.Contains(string(body), bk4.BookingCode) || strings.Contains(string(body), bk.BookingCode) {
		t.Fatalf("admin bookings filter: %d %s", st, body)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/bvrooms/admin/customers?q=annahl", nil)
	if st != 200 || !strings.Contains(string(body), `"booking_count":3`) {
		t.Fatalf("admin customers: %d %s", st, body)
	}
}

func TestBVRoomsApartmentFlow(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	d := func(tm time.Time) string { return tm.Format("2006-01-02") }
	var prop, bld, flr, u1, u2, ut struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "property", "name": "BV Residence", "details": map[string]any{"timezone": "Asia/Jakarta", "profile": "apartment"}})
	e.mustJSON(st, body, 201, &prop)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "building", "parent_id": prop.ID, "name": "Tower E", "details": map[string]any{}})
	e.mustJSON(st, body, 201, &bld)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "floor", "parent_id": bld.ID, "name": "Floor 12", "details": map[string]any{"floor_number": 12}})
	e.mustJSON(st, body, 201, &flr)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "unit", "parent_id": flr.ID, "name": "Unit E-1201", "details": map[string]any{"unit_number": "E-1201", "unit_type": "residential", "area_m2": 45}})
	e.mustJSON(st, body, 201, &u1)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "unit", "parent_id": flr.ID, "name": "Unit E-1202", "details": map[string]any{"unit_number": "E-1202", "unit_type": "residential", "area_m2": 45}})
	e.mustJSON(st, body, 201, &u2)
	// tipe unit (D4) + tandai unit sewa harian
	st, body = e.do(admin, http.MethodPost, "/api/v1/bvrooms/admin/unit-types", map[string]any{"property_id": prop.ID, "name": "Studio 45 m²", "capacity_adults": 2, "bedrooms": 1, "base_rate": 450000, "amenities": []string{"wifi", "kitchen"}})
	e.mustJSON(st, body, 201, &ut)
	// listed sebelum ada tipe/unit → tipe sudah ada; unit belum ditandai → availability 0
	for _, u := range []uuid.UUID{u1.ID, u2.ID} {
		st, body = e.do(admin, http.MethodPatch, "/api/v1/bvrooms/admin/units/"+u.String(), map[string]any{"unit_type_id": ut.ID, "rentable_daily": true})
		e.mustJSON(st, body, 200, nil)
	}
	var listing struct {
		Category string `json:"listing_category"`
		Slug     string `json:"slug"`
	}
	st, body = e.do(admin, http.MethodPut, "/api/v1/bvrooms/admin/properties/"+prop.ID.String()+"/listing", map[string]any{"bvrooms_listed": true, "allow_pay_at_property": true, "city": "Jakarta Selatan"})
	e.mustJSON(st, body, 200, &listing)
	if listing.Category != "apartment" {
		t.Fatalf("kategori apartemen dari profile: %s", body)
	}
	// katalog: filter kategori & terminologi Unit
	st, body = e.do("", http.MethodGet, "/api/v1/bvrooms/catalog/properties?organization_slug=org-a&category=hotel", nil)
	if st != 200 || strings.Contains(string(body), listing.Slug) {
		t.Fatalf("filter hotel tidak boleh memuat apartemen: %d %s", st, body)
	}
	st, body = e.do("", http.MethodGet, "/api/v1/bvrooms/catalog/properties/"+listing.Slug+"?organization_slug=org-a", nil)
	if st != 200 || !strings.Contains(string(body), `"unit_label":"Unit"`) {
		t.Fatalf("terminologi apartemen: %d %s", st, body)
	}
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(jkt)
	ci := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jkt)
	co := ci.AddDate(0, 0, 3)
	var types struct {
		Data []struct {
			Kind           string `json:"kind"`
			AvailableCount int    `json:"available_count"`
			LineTotal      int64  `json:"line_total"`
		} `json:"data"`
	}
	st, body = e.do("", http.MethodGet, "/api/v1/bvrooms/catalog/properties/"+listing.Slug+"/room-types?organization_slug=org-a&check_in="+d(ci)+"&check_out="+d(co), nil)
	e.mustJSON(st, body, 200, &types)
	if len(types.Data) != 1 || types.Data[0].Kind != "unit_type" || types.Data[0].AvailableCount != 2 || types.Data[0].LineTotal != 1350000 {
		t.Fatalf("unit types: %s", body)
	}
	// booking pay_at_property → langsung PAID (confirmed), tanpa deadline
	cust := e.bvCustomer(t, "org-a", "081377778888", "Fransiska", "fransiska@guest.test")
	var bk bvBooking
	st, body = e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings", map[string]any{"property_id": prop.ID, "check_in": d(ci), "check_out": d(co), "guest": map[string]any{"full_name": "Fransiska", "phone": "081377778888"}, "rooms": []map[string]any{{"type_id": ut.ID, "adults": 2}}, "payment": map[string]any{"method_code": "cash_on_site"}})
	e.mustJSON(st, body, 201, &bk)
	if bk.Status != "PAID" || bk.PaymentStatus != "pay_at_property" || bk.PaymentDeadline != nil || bk.Rooms[0].Kind != "unit_type" || bk.Rooms[0].ReservationSt != "confirmed" {
		t.Fatalf("booking apartemen: %s", body)
	}
	// reservasi unit tidak muncul di daftar hotel (room_type NULL) tapi dikelola lewat admin BVRooms: check-in otomatis pilih unit bebas
	st, body = e.do(admin, http.MethodPost, "/api/v1/bvrooms/admin/bookings/"+bk.ID.String()+"/check_in", nil)
	e.mustJSON(st, body, 200, &bk)
	if bk.Status != "CHECK IN" || bk.Rooms[0].AssignedNumber == nil {
		t.Fatalf("check-in unit: %s", body)
	}
	// unit kedua masih bisa disewa pada tanggal sama; unit ketiga tidak
	var bk2 bvBooking
	st, body = e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings", map[string]any{"property_id": prop.ID, "check_in": d(ci), "check_out": d(co), "guest": map[string]any{"full_name": "Fransiska"}, "rooms": []map[string]any{{"type_id": ut.ID, "adults": 1}}})
	e.mustJSON(st, body, 201, &bk2)
	if st, body := e.do(cust, http.MethodPost, "/api/v1/bvrooms/bookings", map[string]any{"property_id": prop.ID, "check_in": d(ci), "check_out": d(co), "guest": map[string]any{"full_name": "F"}, "rooms": []map[string]any{{"type_id": ut.ID, "adults": 1}}}); st != 409 {
		t.Fatalf("unit habis harus 409: %d %s", st, body)
	}
	// check-out → CHECK OUT; pay_at_property → paid
	st, body = e.do(admin, http.MethodPost, "/api/v1/bvrooms/admin/bookings/"+bk.ID.String()+"/check_out", nil)
	e.mustJSON(st, body, 200, &bk)
	if bk.Status != "CHECK OUT" || bk.PaymentStatus != "paid" || bk.Payment == nil || bk.Payment.Status != "paid" {
		t.Fatalf("check-out apartemen: %s", body)
	}
	// cancel oleh properti (dashboard) untuk booking kedua → CANCELLED + notifikasi
	st, body = e.do(admin, http.MethodPost, "/api/v1/bvrooms/admin/bookings/"+bk2.ID.String()+"/cancel", map[string]any{"reason": "Unit dalam perbaikan"})
	e.mustJSON(st, body, 200, &bk2)
	if bk2.Status != "CANCELLED" {
		t.Fatalf("cancel by property: %s", body)
	}
	got := e.bvInbox(t, cust)
	if got[0] != "booking_cancelled" {
		t.Fatalf("inbox: %v", got)
	}
	// hotel.room_types tidak boleh dipakai apartemen; tipe unit tidak boleh dibuat di hotel
	if st, body := e.do(admin, http.MethodPost, "/api/v1/bvrooms/admin/unit-types", map[string]any{"property_id": e.refs.PropertyID, "name": "X"}); st != 409 {
		t.Fatalf("unit type di property office harus 409: %d %s", st, body)
	}
}
