package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type listingResp struct {
	ID              uuid.UUID  `json:"id"`
	ListingCode     string     `json:"listing_code"`
	Status          string     `json:"status"`
	OccupancyStatus string     `json:"unit_occupancy_status"`
	AskingPrice     int64      `json:"asking_price"`
	LeadCount       int        `json:"lead_count"`
	ActiveResID     *uuid.UUID `json:"active_reservation_id"`
	AllowedActions  []string   `json:"allowed_actions"`
	Version         int        `json:"version"`
}

type leadResp struct {
	ID             uuid.UUID  `json:"id"`
	LeadCode       string     `json:"lead_code"`
	Status         string     `json:"status"`
	ListingID      *uuid.UUID `json:"listing_id"`
	ReservationID  *uuid.UUID `json:"reservation_id"`
	AllowedActions []string   `json:"allowed_actions"`
}

type saleResResp struct {
	ID                uuid.UUID  `json:"id"`
	ReservationNumber string     `json:"reservation_number"`
	Status            string     `json:"status"`
	AgreedPrice       int64      `json:"agreed_price"`
	BookingFee        int64      `json:"booking_fee"`
	InvoiceID         *uuid.UUID `json:"invoice_id"`
	TenantID          *uuid.UUID `json:"tenant_id"`
	TenantUserID      *uuid.UUID `json:"tenant_user_id"`
	AllowedActions    []string   `json:"allowed_actions"`
}

type rentalListingResp struct {
	ID             uuid.UUID `json:"id"`
	ListingCode    string    `json:"listing_code"`
	Status         string    `json:"status"`
	RateDaily      *int64    `json:"rate_daily"`
	RateWeekly     *int64    `json:"rate_weekly"`
	RateMonthly    *int64    `json:"rate_monthly"`
	OpenInquiries  int       `json:"open_inquiries"`
	AllowedActions []string  `json:"allowed_actions"`
}

type quoteResp struct {
	Available   bool      `json:"available"`
	Reason      string    `json:"reason"`
	EndDate     time.Time `json:"end_date"`
	Days        int       `json:"days"`
	TotalAmount int64     `json:"total_amount"`
	Deposit     int64     `json:"deposit_amount"`
	Conflicts   []string  `json:"conflicts"`
}

type rentalResResp struct {
	ID                uuid.UUID  `json:"id"`
	ReservationNumber string     `json:"reservation_number"`
	Status            string     `json:"status"`
	RentalPeriod      string     `json:"rental_period"`
	StartDate         time.Time  `json:"start_date"`
	EndDate           time.Time  `json:"end_date"`
	Days              int        `json:"days"`
	TotalAmount       int64      `json:"total_amount"`
	DepositAmount     int64      `json:"deposit_amount"`
	InvoiceID         *uuid.UUID `json:"invoice_id"`
	InvoiceStatus     *string    `json:"invoice_status"`
	TenantID          *uuid.UUID `json:"tenant_id"`
	TenantCode        *string    `json:"tenant_code"`
	TenantUserID      *uuid.UUID `json:"tenant_user_id"`
	AllowedActions    []string   `json:"allowed_actions"`
}

type onboardingResp struct {
	TenantCode        string `json:"tenant_code"`
	AccountEmail      string `json:"account_email"`
	TemporaryPassword string `json:"temporary_password"`
}

// P1.7 — Apartment Unit Sales & Rental Management (PRD §3.10, WF-P1-008/009, AT-P1-000D/E, DoD #24, #25, #40, #41).
func TestApartmentSalesAndRental(t *testing.T) {
	e := setup(t)
	admin := e.login("admin@org-a.test")
	pm := e.login("pm@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")

	// property demo = office → capability unit_sales/unit_rental tidak aktif (AC-15/18)
	if st, body := e.do(pm, http.MethodGet, "/api/v1/unit-sales/listings?property_id="+e.refs.PropertyID.String(), nil); st != 403 || !strings.Contains(string(body), "CAPABILITY_NOT_ENABLED") {
		t.Fatalf("unit-sales pada profile office harus 403 CAPABILITY_NOT_ENABLED: %d %s", st, body)
	}
	if st, body := e.do(pm, http.MethodGet, "/api/v1/unit-rental/listings?property_id="+e.refs.PropertyID.String(), nil); st != 403 || !strings.Contains(string(body), "CAPABILITY_NOT_ENABLED") {
		t.Fatalf("unit-rental pada profile office harus 403 CAPABILITY_NOT_ENABLED: %d %s", st, body)
	}

	// ---- property apartment + tower/lantai/unit ----
	var apt, bld, twr, flr, u1, u2 struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "property", "name": "Green Residence", "details": map[string]any{"timezone": "Asia/Jakarta", "profile": "apartment"}})
	e.mustJSON(st, body, 201, &apt)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "building", "parent_id": apt.ID, "name": "Green Residence Building", "details": map[string]any{}})
	e.mustJSON(st, body, 201, &bld)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "tower", "parent_id": bld.ID, "name": "Tower Emerald", "details": map[string]any{}})
	e.mustJSON(st, body, 201, &twr)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "floor", "parent_id": twr.ID, "name": "Lantai 12", "details": map[string]any{"floor_number": 12}})
	e.mustJSON(st, body, 201, &flr)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "unit", "parent_id": flr.ID, "name": "Unit E-1201", "details": map[string]any{"unit_number": "E-1201", "unit_type": "residential", "area_m2": 72}})
	e.mustJSON(st, body, 201, &u1)
	st, body = e.do(admin, http.MethodPost, "/api/v1/locations", map[string]any{"location_type": "unit", "parent_id": flr.ID, "name": "Unit E-1202", "details": map[string]any{"unit_number": "E-1202", "unit_type": "residential", "area_m2": 45}})
	e.mustJSON(st, body, 201, &u2)

	// authz: teknisi tidak punya commercial.* → 403 (server-side, bukan UI)
	if st, _ := e.do(tech, http.MethodPost, "/api/v1/unit-sales/listings", map[string]any{"property_id": apt.ID, "unit_location_id": u1.ID, "asking_price": 1}); st != 403 {
		t.Fatalf("teknisi buat listing harus 403: %d", st)
	}

	// ================= UNIT SALES (WF-P1-008) =================
	var li listingResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-sales/listings", map[string]any{"property_id": apt.ID, "unit_location_id": u1.ID, "title": "2BR Emerald 1201", "asking_price": 1_500_000_000, "bedrooms": 2, "bathrooms": 1, "furnishing": "semi_furnished", "features": []string{"balcony", "city_view"},
		"documents": []map[string]any{{"name": "Brosur", "reference": "BR-2026-01"}}})
	e.mustJSON(st, body, 201, &li)
	if !strings.HasPrefix(li.ListingCode, "LIST-") || li.Status != "draft" || !has(li.AllowedActions, "publish") {
		t.Fatalf("listing: %+v", li)
	}
	// unit yang sama tidak boleh punya dua listing aktif
	if st, body := e.do(admin, http.MethodPost, "/api/v1/unit-sales/listings", map[string]any{"property_id": apt.ID, "unit_location_id": u1.ID, "asking_price": 1}); st != 409 || !strings.Contains(string(body), "LISTING_EXISTS") {
		t.Fatalf("listing ganda harus 409 LISTING_EXISTS: %d %s", st, body)
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-sales/listings/"+li.ID.String()+"/publish", nil)
	e.mustJSON(st, body, 200, &li)
	if li.Status != "published" {
		t.Fatalf("publish: %+v", li)
	}

	// lead / inquiry → pipeline
	var ld leadResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-sales/leads", map[string]any{"property_id": apt.ID, "listing_id": li.ID, "full_name": "Andi Wijaya", "phone": "0812-1111-2222", "email": "andi@buyer.test", "source": "website", "budget_max": 1_600_000_000, "inquiry": "Tanya harga & cicilan 2BR"})
	e.mustJSON(st, body, 201, &ld)
	if !strings.HasPrefix(ld.LeadCode, "LEAD-") || ld.Status != "new" {
		t.Fatalf("lead: %+v", ld)
	}
	// qualify langsung dari new tidak dilarang; jalankan contact → qualified sesuai pipeline
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-sales/leads/"+ld.ID.String()+"/contact", map[string]any{"note": "Telepon pertama"})
	e.mustJSON(st, body, 200, &ld)
	if ld.Status != "contacted" {
		t.Fatalf("contact: %+v", ld)
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-sales/leads/"+ld.ID.String()+"/activities", map[string]any{"activity_type": "site_visit", "summary": "Kunjungan unit E-1201 bersama keluarga"})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-sales/leads/"+ld.ID.String()+"/qualify", nil)
	e.mustJSON(st, body, 200, &ld)
	if ld.Status != "qualified" || !has(ld.AllowedActions, "reserve") {
		t.Fatalf("qualify: %+v", ld)
	}
	// lose tanpa reason → 400
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/unit-sales/leads/"+ld.ID.String()+"/lose", nil); st != 400 {
		t.Fatalf("lose tanpa reason harus 400: %d", st)
	}
	var acts struct {
		Data []struct {
			ActivityType string `json:"activity_type"`
		} `json:"data"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-sales/leads/"+ld.ID.String()+"/activities", nil)
	e.mustJSON(st, body, 200, &acts)
	if len(acts.Data) < 4 { // inquiry note, status contacted, site_visit, status qualified
		t.Fatalf("aktivitas lead: %+v", acts.Data)
	}

	// unit reservation (booking fee → invoice deposit via Billing)
	var sr saleResResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-sales/reservations", map[string]any{"lead_id": ld.ID, "listing_id": li.ID, "agreed_price": 1_450_000_000, "booking_fee": 50_000_000, "reserved_until": time.Now().AddDate(0, 0, 14).Format("2006-01-02")})
	e.mustJSON(st, body, 201, &sr)
	if !strings.HasPrefix(sr.ReservationNumber, "SRES-") || sr.Status != "reserved" || sr.AgreedPrice != 1_450_000_000 || sr.InvoiceID == nil {
		t.Fatalf("sale reservation: %+v", sr)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-sales/listings/"+li.ID.String(), nil)
	e.mustJSON(st, body, 200, &li)
	if li.Status != "reserved" || li.OccupancyStatus != "reserved" || li.ActiveResID == nil {
		t.Fatalf("listing setelah reservasi: %+v", li)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-sales/leads/"+ld.ID.String(), nil)
	e.mustJSON(st, body, 200, &ld)
	if ld.Status != "reserved" || ld.ReservationID == nil || *ld.ReservationID != sr.ID {
		t.Fatalf("lead setelah reservasi: %+v", ld)
	}
	// lead lain tidak bisa mereservasi listing yang sudah reserved
	var ld2 leadResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-sales/leads", map[string]any{"property_id": apt.ID, "full_name": "Budi Santoso", "source": "walk_in"})
	e.mustJSON(st, body, 201, &ld2)
	if st, body := e.do(admin, http.MethodPost, "/api/v1/unit-sales/reservations", map[string]any{"lead_id": ld2.ID, "listing_id": li.ID}); st != 409 || !strings.Contains(string(body), "UNIT_UNAVAILABLE") {
		t.Fatalf("reservasi ganda harus 409 UNIT_UNAVAILABLE: %d %s", st, body)
	}
	// guard profile: reservasi aktif memblokir perubahan profile (AC-19/20)
	if st, body := e.do(admin, http.MethodPost, "/api/v1/properties/"+apt.ID.String()+"/profile", map[string]any{"profile": "office", "reason": "test guard"}); st != 409 || !strings.Contains(string(body), "PROFILE_CHANGE_BLOCKED") {
		t.Fatalf("ganti profile dengan reservasi aktif harus 409 PROFILE_CHANGE_BLOCKED: %d %s", st, body)
	}
	// sold → handover (pemilik menjadi Tenant/Occupant + akun Tenant App)
	var sact struct {
		Reservation saleResResp     `json:"reservation"`
		Onboarding  *onboardingResp `json:"onboarding"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-sales/reservations/"+sr.ID.String()+"/complete", map[string]any{"contract_reference": "AJB-2026-0042"})
	e.mustJSON(st, body, 200, &sact)
	if sact.Reservation.Status != "sold" || !has(sact.Reservation.AllowedActions, "handover") {
		t.Fatalf("complete: %+v", sact.Reservation)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-sales/listings/"+li.ID.String(), nil)
	e.mustJSON(st, body, 200, &li)
	if li.Status != "sold" {
		t.Fatalf("listing sold: %+v", li)
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-sales/reservations/"+sr.ID.String()+"/handover", map[string]any{"create_tenant_account": true, "owner_email": "andi@buyer.test"})
	e.mustJSON(st, body, 200, &sact)
	if sact.Reservation.Status != "handed_over" || sact.Reservation.TenantID == nil || sact.Onboarding == nil || !strings.HasPrefix(sact.Onboarding.TenantCode, "TEN-") || sact.Onboarding.TemporaryPassword == "" {
		t.Fatalf("handover: %+v %+v", sact.Reservation, sact.Onboarding)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-sales/leads/"+ld.ID.String(), nil)
	e.mustJSON(st, body, 200, &ld)
	if ld.Status != "sold" {
		t.Fatalf("lead sold: %+v", ld)
	}
	// pemilik login Tenant App → unit E-1201 dengan terminologi apartment (Penghuni)
	st, resp := e.loginTenant(t, "andi@buyer.test", sact.Onboarding.TemporaryPassword)
	if st != 200 {
		t.Fatalf("login pemilik: %d %v", st, resp)
	}
	ownerTok, _ := resp["access_token"].(string)
	var me struct {
		Property struct {
			Profile     string `json:"profile"`
			Terminology map[string]struct {
				ID string `json:"id"`
			} `json:"terminology"`
		} `json:"property"`
		PrimaryUnit *struct {
			ID uuid.UUID `json:"id"`
		} `json:"primary_unit"`
	}
	st, body = e.do(ownerTok, http.MethodGet, "/api/v1/tenant/me", nil)
	e.mustJSON(st, body, 200, &me)
	if me.Property.Profile != "apartment" || me.PrimaryUnit == nil || me.PrimaryUnit.ID != u1.ID || me.Property.Terminology["customer"].ID == "" {
		t.Fatalf("tenant/me pemilik: %+v", me)
	}
	var sum struct {
		Listings     map[string]int `json:"listings"`
		Leads        map[string]int `json:"leads"`
		Reservations map[string]int `json:"reservations"`
		SoldValue    int64          `json:"sold_value"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-sales/summary?property_id="+apt.ID.String(), nil)
	e.mustJSON(st, body, 200, &sum)
	if sum.Listings["sold"] != 1 || sum.Leads["sold"] != 1 || sum.Reservations["handed_over"] != 1 || sum.SoldValue != 1_450_000_000 {
		t.Fatalf("summary: %+v", sum)
	}

	// ================= UNIT RENTAL (WF-P1-009) =================
	var rl rentalListingResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-rental/listings", map[string]any{"property_id": apt.ID, "unit_location_id": u2.ID, "title": "Studio Emerald 1202", "rate_daily": 500_000, "rate_weekly": 3_000_000, "rate_monthly": 9_000_000, "deposit_amount": 5_000_000, "min_stay_days": 2, "max_occupants": 2, "furnishing": "furnished"})
	e.mustJSON(st, body, 201, &rl)
	if !strings.HasPrefix(rl.ListingCode, "RLIST-") || rl.Status != "draft" || rl.RateMonthly == nil || *rl.RateMonthly != 9_000_000 {
		t.Fatalf("rental listing: %+v", rl)
	}
	// tanpa rate sama sekali → 400
	if st, _ := e.do(admin, http.MethodPost, "/api/v1/unit-rental/listings", map[string]any{"property_id": apt.ID, "unit_location_id": u1.ID}); st != 400 {
		t.Fatalf("listing tanpa rate harus 400: %d", st)
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-rental/listings/"+rl.ID.String()+"/publish", nil)
	e.mustJSON(st, body, 200, &rl)
	if rl.Status != "published" {
		t.Fatalf("publish rental: %+v", rl)
	}

	start := time.Now().AddDate(0, 1, 0)
	startS := start.Format("2006-01-02")
	// availability: monthly × 3 → tersedia, total 27 jt, end = start + 3 bulan
	var q quoteResp
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-rental/availability?listing_id="+rl.ID.String()+"&rental_period=monthly&period_count=3&start_date="+startS, nil)
	e.mustJSON(st, body, 200, &q)
	wantEnd := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 3, 0)
	if !q.Available || q.TotalAmount != 27_000_000 || q.Deposit != 5_000_000 || !q.EndDate.Equal(wantEnd) {
		t.Fatalf("quote monthly: %+v (want end %s)", q, wantEnd)
	}
	// daily × 1 < min_stay 2 → tidak tersedia
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-rental/availability?listing_id="+rl.ID.String()+"&rental_period=daily&period_count=1&start_date="+startS, nil)
	e.mustJSON(st, body, 200, &q)
	if q.Available || !strings.Contains(q.Reason, "Minimal") {
		t.Fatalf("quote daily <min stay: %+v", q)
	}
	// weekly × 2 → 14 hari, 6 jt
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-rental/availability?listing_id="+rl.ID.String()+"&rental_period=weekly&period_count=2&start_date="+startS, nil)
	e.mustJSON(st, body, 200, &q)
	if !q.Available || q.Days != 14 || q.TotalAmount != 6_000_000 {
		t.Fatalf("quote weekly: %+v", q)
	}

	// inquiry (New) tidak memblokir; reservasi confirm=true pada periode sama → Reserved
	var inq, rr rentalResResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-rental/reservations", map[string]any{"listing_id": rl.ID, "prospect_name": "Citra Lestari", "prospect_phone": "0813-3333-4444", "rental_period": "monthly", "period_count": 3, "start_date": startS, "source": "phone"})
	e.mustJSON(st, body, 201, &inq)
	if !strings.HasPrefix(inq.ReservationNumber, "RRES-") || inq.Status != "new" || inq.InvoiceID != nil {
		t.Fatalf("inquiry: %+v", inq)
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-rental/reservations", map[string]any{"listing_id": rl.ID, "prospect_name": "Dewi Anggraini", "prospect_email": "dewi.rent@tenant.test", "prospect_phone": "0815-5555-6666", "rental_period": "monthly", "period_count": 3, "start_date": startS, "confirm": true})
	e.mustJSON(st, body, 201, &rr)
	if rr.Status != "reserved" || rr.TotalAmount != 27_000_000 || rr.DepositAmount != 5_000_000 || rr.InvoiceID == nil || rr.InvoiceStatus == nil || *rr.InvoiceStatus != "issued" {
		t.Fatalf("reserved: %+v", rr)
	}
	// konfirmasi inquiry pada periode yang sudah reserved → 409
	if st, body := e.do(admin, http.MethodPost, "/api/v1/unit-rental/reservations/"+inq.ID.String()+"/confirm", nil); st != 409 || !strings.Contains(string(body), "UNIT_UNAVAILABLE") {
		t.Fatalf("confirm inquiry bertabrakan harus 409 UNIT_UNAVAILABLE: %d %s", st, body)
	}
	// availability sekarang menunjukkan konflik
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-rental/availability?listing_id="+rl.ID.String()+"&rental_period=monthly&period_count=1&start_date="+startS, nil)
	e.mustJSON(st, body, 200, &q)
	if q.Available || len(q.Conflicts) != 1 || q.Conflicts[0] != rr.ReservationNumber {
		t.Fatalf("quote konflik: %+v", q)
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-rental/reservations/"+inq.ID.String()+"/cancel", map[string]any{"reason": "Pindah ke unit lain"})
	e.mustJSON(st, body, 200, nil)
	// reservasi confirm pada periode overlap langsung → 409 (EXCLUDE di DB)
	if st, body := e.do(admin, http.MethodPost, "/api/v1/unit-rental/reservations", map[string]any{"listing_id": rl.ID, "prospect_name": "Eko", "rental_period": "weekly", "period_count": 1, "start_date": start.AddDate(0, 1, 0).Format("2006-01-02"), "confirm": true}); st != 409 {
		t.Fatalf("overlap reserved harus 409: %d %s", st, body)
	}
	// periode setelahnya → boleh
	var rrNext rentalResResp
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-rental/reservations", map[string]any{"listing_id": rl.ID, "prospect_name": "Fajar", "rental_period": "daily", "period_count": 3, "start_date": wantEnd.Format("2006-01-02"), "confirm": true})
	e.mustJSON(st, body, 201, &rrNext)
	if rrNext.Status != "reserved" || rrNext.Days != 3 || rrNext.TotalAmount != 1_500_000 {
		t.Fatalf("reservasi berikutnya: %+v", rrNext)
	}

	// activate → Tenant Onboarding (Tenant/Occupant + akun Tenant App) → Active Rental
	var ract struct {
		Reservation rentalResResp   `json:"reservation"`
		Onboarding  *onboardingResp `json:"onboarding"`
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-rental/reservations/"+rr.ID.String()+"/activate", map[string]any{"create_tenant_account": true})
	e.mustJSON(st, body, 200, &ract)
	if ract.Reservation.Status != "active" || ract.Reservation.TenantID == nil || ract.Reservation.TenantCode == nil || ract.Reservation.TenantUserID == nil || ract.Onboarding == nil || ract.Onboarding.TemporaryPassword == "" || ract.Onboarding.AccountEmail != "dewi.rent@tenant.test" {
		t.Fatalf("activate: %+v %+v", ract.Reservation, ract.Onboarding)
	}
	e.dispatch(t)
	st, resp = e.loginTenant(t, "dewi.rent@tenant.test", ract.Onboarding.TemporaryPassword)
	if st != 200 {
		t.Fatalf("login penyewa: %d %v", st, resp)
	}
	renterTok, _ := resp["access_token"].(string)
	st, body = e.do(renterTok, http.MethodGet, "/api/v1/tenant/me", nil)
	e.mustJSON(st, body, 200, &me)
	if me.Property.Profile != "apartment" || me.PrimaryUnit == nil || me.PrimaryUnit.ID != u2.ID {
		t.Fatalf("tenant/me penyewa: %+v", me)
	}
	if ib := e.inboxOf(t, renterTok); !hasType(ib, "rental_activated") {
		t.Fatalf("penyewa harus menerima notifikasi rental_activated: %+v", ib.Data)
	}
	// penyewa melihat tagihan sewa di Bills (invoice ter-link ke tenant saat onboarding)
	var bills struct {
		Data []struct {
			ID     uuid.UUID `json:"id"`
			Status string    `json:"status"`
		} `json:"data"`
	}
	st, body = e.do(renterTok, http.MethodGet, "/api/v1/tenant/invoices", nil)
	e.mustJSON(st, body, 200, &bills)
	if len(bills.Data) != 1 || bills.Data[0].ID != *rr.InvoiceID {
		t.Fatalf("bills penyewa: %+v (invoice %v)", bills.Data, rr.InvoiceID)
	}
	// unit occupied oleh tenant onboarding
	var unitLoc struct {
		Details map[string]any `json:"details"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/locations/"+u2.ID.String(), nil)
	e.mustJSON(st, body, 200, &unitLoc)
	if unitLoc.Details["occupancy_status"] != "occupied" || unitLoc.Details["tenant_id"] == nil {
		t.Fatalf("unit setelah onboarding: %+v", unitLoc.Details)
	}
	// periode aktif tidak dapat diubah
	if st, _ := e.do(admin, http.MethodPatch, "/api/v1/unit-rental/reservations/"+rr.ID.String(), map[string]any{"period_count": 4}); st != 409 {
		t.Fatalf("ubah periode aktif harus 409: %d", st)
	}
	// kalender: entri untuk rr (active) + rrNext (reserved)
	var cal struct {
		Listings []any `json:"listings"`
		Entries  []struct {
			ReservationID uuid.UUID `json:"reservation_id"`
			Status        string    `json:"status"`
		} `json:"entries"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-rental/reservations/calendar?property_id="+apt.ID.String()+"&from="+startS+"&to="+wantEnd.AddDate(0, 0, 10).Format("2006-01-02"), nil)
	e.mustJSON(st, body, 200, &cal)
	if len(cal.Listings) != 1 || len(cal.Entries) != 2 {
		t.Fatalf("kalender: %d listing, %+v", len(cal.Listings), cal.Entries)
	}
	// archive listing dengan reservasi aktif → 409
	if st, body := e.do(admin, http.MethodPost, "/api/v1/unit-rental/listings/"+rl.ID.String()+"/archive", nil); st != 409 || !strings.Contains(string(body), "RESERVATIONS_ACTIVE") {
		t.Fatalf("archive dengan reservasi aktif harus 409: %d %s", st, body)
	}
	// complete → move-out: unit vacant, tenant moved_out, akses Tenant App berakhir
	st, body = e.do(admin, http.MethodPost, "/api/v1/unit-rental/reservations/"+rr.ID.String()+"/complete", map[string]any{"move_out_date": time.Now().Format("2006-01-02")})
	e.mustJSON(st, body, 200, &ract)
	if ract.Reservation.Status != "completed" {
		t.Fatalf("complete: %+v", ract.Reservation)
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/locations/"+u2.ID.String(), nil)
	e.mustJSON(st, body, 200, &unitLoc)
	if unitLoc.Details["occupancy_status"] != "vacant" || unitLoc.Details["tenant_id"] != nil {
		t.Fatalf("unit setelah move-out: %+v", unitLoc.Details)
	}
	var rsum struct {
		Reservations  map[string]int `json:"reservations"`
		ActiveRentals int            `json:"active_rentals"`
		UpcomingStart int            `json:"upcoming_start"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-rental/summary?property_id="+apt.ID.String(), nil)
	e.mustJSON(st, body, 200, &rsum)
	if rsum.Reservations["completed"] != 1 || rsum.Reservations["reserved"] != 1 || rsum.Reservations["cancelled"] != 1 || rsum.ActiveRentals != 0 {
		t.Fatalf("rental summary: %+v", rsum)
	}
	// notifikasi staf: management supervisor (admin/PM fallback) menerima inquiry & reservasi
	e.dispatch(t)
	var evTypes []string
	for _, ev := range e.jobs.Events {
		evTypes = append(evTypes, ev.Type)
	}
	for _, want := range []string{"unit_listing.published", "unit_sales_lead.created", "unit_sale_reservation.created", "unit_sale_reservation.sold", "unit_sale_reservation.handed_over", "unit_rental_listing.published", "unit_rental_reservation.created", "unit_rental_reservation.confirmed", "unit_rental_reservation.activated", "unit_rental_reservation.completed", "unit_rental_reservation.cancelled"} {
		if !has(evTypes, want) {
			t.Fatalf("event %s tidak diterbitkan; events=%v", want, evTypes)
		}
	}
	// list & filter
	var page struct {
		Data []json.RawMessage `json:"data"`
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-rental/reservations?property_id="+apt.ID.String()+"&status=reserved", nil)
	e.mustJSON(st, body, 200, &page)
	if len(page.Data) != 1 {
		t.Fatalf("filter reserved: %d", len(page.Data))
	}
	st, body = e.do(admin, http.MethodGet, "/api/v1/unit-sales/leads?property_id="+apt.ID.String()+"&q=andi", nil)
	e.mustJSON(st, body, 200, &page)
	if len(page.Data) != 1 {
		t.Fatalf("cari lead: %d", len(page.Data))
	}
}
