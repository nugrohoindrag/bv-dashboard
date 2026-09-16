package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/bvrooms"
	"github.com/buildingvision/api/internal/commercial"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantapp"
	"github.com/buildingvision/api/internal/tenantrelation"
)

// seedApartment: Vision Residence (§3.2, §22) — Tower A & B (10 lantai/tower), 41 unit, 10 tenant + occupant,
// unit sales (listing → lead → reservasi → kontrak → handover) & unit rental (daily/weekly/monthly), E2E-01 (AC A-1208),
// BVRooms apartemen (sewa harian unit studio).
func (s *Service) seedApartment(ctx context.Context, env *Env, logf func(string, ...any)) (uuid.UUID, error) {
	p, err := s.seedBase(ctx, env, propSpec{Name: "Vision Residence", Profile: ProfileApartment, Prefix: "apartment", Address: "Jl. Kemang Raya No. 88", City: "Jakarta Selatan", Timezone: "Asia/Jakarta", Building: "Podium Vision Residence", FloorFrom: 0, FloorTo: 2, Lat: -6.2607, Lng: 106.8135}, logf)
	if err != nil {
		return uuid.Nil, err
	}
	admin := p.admin
	mk := func(lt property.LocationType, parent uuid.UUID, name string, details map[string]any) (uuid.UUID, error) {
		l, err := s.Property.CreateLocation(admin, property.CreateLocationInput{LocationType: lt, ParentID: &parent, Name: name, Details: details})
		if err != nil {
			return uuid.Nil, fmt.Errorf("location %s: %w", name, err)
		}
		return l.ID, nil
	}
	// tower A & B, lantai 3–12, 2 unit per lantai (+ A-1208 untuk E2E-01) → 41 unit residential
	units := map[string]uuid.UUID{}
	towerFloors := map[string]map[int]uuid.UUID{}
	for _, tw := range []string{"A", "B"} {
		twID, err := mk(property.LTTower, p.Building, "Tower "+tw, map[string]any{"floors_count": 12.0})
		if err != nil {
			return uuid.Nil, err
		}
		towerFloors[tw] = map[int]uuid.UUID{}
		for f := 3; f <= 12; f++ {
			fl, err := mk(property.LTFloor, twID, fmt.Sprintf("Tower %s Lantai %d", tw, f), map[string]any{"floor_number": float64(f)})
			if err != nil {
				return uuid.Nil, err
			}
			towerFloors[tw][f] = fl
			nums := []string{"01", "02"}
			if tw == "A" && f == 12 {
				nums = append(nums, "08")
			}
			for _, n := range nums {
				code := fmt.Sprintf("%s-%02d%s", tw, f, n)
				area := 36.0
				if n == "02" {
					area = 58.0
				}
				if n == "08" {
					area = 72.0
				}
				id, err := mk(property.LTUnit, fl, "Unit "+code, map[string]any{"unit_number": code, "unit_type": "residential", "area_m2": area})
				if err != nil {
					return uuid.Nil, err
				}
				units[code] = id
			}
		}
	}
	// area fasilitas
	for _, a := range []struct{ key, name, typ string }{{"pool", "Kolam Renang", "other"}, {"gym", "Fitness Center", "other"}, {"function", "Function Room", "other"}, {"bbq", "BBQ Area Rooftop", "other"}} {
		id, err := mk(property.LTArea, p.Floors[2], a.name, map[string]any{"area_type": a.typ})
		if err != nil {
			return uuid.Nil, err
		}
		p.Areas[a.key] = id
	}
	if err := s.markReportable(ctx, env, p.ID); err != nil {
		return uuid.Nil, err
	}

	// ---- tenant & occupant (§7 Apartment): 10 tenant (pemilik/penyewa), beberapa dengan >1 occupant, akun Tenant App ----
	tr := s.asEmail(ctx, env, "tenantrelation.demo@buildingvision.local")
	type tenDef struct {
		unit, name, phone, email string
		occupants                []string
		app                      string // email akun tenant app (occupant utama)
		owner                    string // owner | tenant
	}
	tens := []tenDef{
		{"A-1208", "Keluarga Wibowo", "081233330001", "apartment.tenant@buildingvision.local", []string{"Bapak Hendra Wibowo", "Ibu Ratna Wibowo", "Kevin Wibowo"}, "apartment.tenant@buildingvision.local", "owner"},
		{"A-0301", "Dian Puspita", "081233330002", "dian.puspita@resident.test", []string{"Dian Puspita"}, "dian.puspita@resident.test", "tenant"},
		{"A-0302", "Keluarga Halim", "081233330003", "halim@resident.test", []string{"Ronald Halim", "Susan Halim"}, "", "owner"},
		{"A-0401", "Bagus Wicaksono", "081233330004", "bagus.w@resident.test", []string{"Bagus Wicaksono"}, "", "tenant"},
		{"A-0501", "Keluarga Santoso", "081233330005", "santoso@resident.test", []string{"Irwan Santoso", "Mega Santoso", "Alya Santoso"}, "irwan.santoso@resident.test", "owner"},
		{"B-0301", "Nurul Hidayah", "081233330006", "nurul.h@resident.test", []string{"Nurul Hidayah"}, "", "tenant"},
		{"B-0302", "Keluarga Tan", "081233330007", "tan@resident.test", []string{"Michael Tan", "Jessica Tan"}, "", "owner"},
		{"B-0401", "Rafi Ramadhan", "081233330008", "rafi.r@resident.test", []string{"Rafi Ramadhan"}, "", "tenant"},
		{"B-0501", "Keluarga Kusnadi", "081233330009", "kusnadi@resident.test", []string{"Anton Kusnadi", "Lina Kusnadi"}, "", "owner"},
		{"A-0601", "Vera Anggraeni", "081233330010", "vera.a@resident.test", []string{"Vera Anggraeni"}, "", "tenant"},
	}
	var tenants []tenantRef
	for _, t := range tens {
		ten, err := s.Property.CreateTenant(admin, property.TenantInput{PropertyID: &p.ID, Name: ptr(t.name), TenantType: ptr("individual"), ContactName: ptr(t.occupants[0]), ContactPhone: ptr("+62" + t.phone[1:]), ContactEmail: ptr(t.email), Status: ptr("active"), UnitIDs: ptr([]uuid.UUID{units[t.unit]})})
		if err != nil {
			return uuid.Nil, fmt.Errorf("tenant %s: %w", t.name, err)
		}
		ref := tenantRef{TenantID: ten.ID, Name: t.name, UnitID: units[t.unit], UnitLabel: "Unit " + t.unit, AppEmail: t.app}
		for i, o := range t.occupants {
			var email *string
			if i == 0 {
				email = ptr(t.email)
			}
			occ, err := s.Property.CreateOccupant(admin, property.OccupantInput{TenantID: &ten.ID, FullName: ptr(o), Phone: ptr("+62" + t.phone[1:]), Email: email, IsPrimaryContact: ptr(i == 0), UnitIDs: ptr([]uuid.UUID{units[t.unit]})})
			if err != nil {
				return uuid.Nil, fmt.Errorf("occupant %s: %w", o, err)
			}
			if i == 0 {
				ref.OccupantID = &occ.ID
			}
		}
		if t.app != "" {
			if _, err := s.TR.Create(tr, tenantrelation.CreateInput{PropertyID: p.ID, TenantID: &ten.ID, OccupantID: ref.OccupantID, FullName: t.occupants[0], Email: t.app, Phone: "+62" + t.phone[1:], Role: "tenant_admin", OwnershipStatus: t.owner, UnitIDs: []uuid.UUID{units[t.unit]}, Password: Password}); err != nil {
				return uuid.Nil, fmt.Errorf("tenant app %s: %w", t.app, err)
			}
		}
		tenants = append(tenants, ref)
	}
	p.Tenants = tenants
	// prospect (§6): pendaftaran mandiri dari Tenant App → pending validation di Tenant Relation
	if _, err := s.TenantApp.Register(ctx, tenantapp.RegisterInput{OrganizationSlug: OrgSlug, FullName: "Prospect Demo (Calon Penyewa)", Phone: "+6281233330099", Email: "apartment.prospect@buildingvision.local", Password: Password, PropertyID: p.ID, UnitID: units["B-1201"], OwnershipStatus: "tenant"}, "127.0.0.1"); err != nil {
		return uuid.Nil, fmt.Errorf("prospect register: %w", err)
	}
	s.flush(ctx)
	logf("apartment: %d unit (Tower A/B), %d tenant, occupant multi, Tenant App apartment.tenant@ + 2 penghuni lain, prospect pending validation", len(units), len(tens))

	// ---- tiket & E2E-01 (AC tidak dingin A-1208 → WO → parts → resolved → konfirmasi → closed → CSAT) ----
	t0 := tenants[0]
	tickets := []srSpec{
		{Category: "maintenance", Title: "AC tidak dingin — Unit A-1208", Desc: "AC kamar utama tidak dingin sejak 2 hari, sudah dibersihkan filter sendiri.", Priority: "high", Location: ptr(units["A-1208"]), TenantApp: t0.AppEmail, Age: 48 * time.Hour, Stage: "closed", AssignTo: p.Staff["tech"], Rating: 5,
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Perbaikan AC Unit A-1208 (isi refrigerant, ganti filter)", Priority: "high", Assignee: p.Staff["tech"], Asset: ptr(p.Assets["ac"]), Stage: "closed", Template: "pm_ac", Evidence: true, DueIn: 8 * time.Hour, Parts: []partSpec{{"ac_filter", 1}, {"refrigerant", 1}}, Vendor: "hvac"}},
		{Category: "maintenance", Title: "Lampu koridor lantai 3 Tower A mati", Desc: "", Priority: "medium", Location: ptr(p.Areas["corridor"]), TenantApp: "dian.puspita@resident.test", Age: 6 * time.Hour, Stage: "in_progress", AssignTo: p.Staff["tech2"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Ganti lampu koridor lantai 3 Tower A", Priority: "medium", Assignee: p.Staff["tech2"], Stage: "in_progress", Evidence: true, DueIn: 6 * time.Hour, Parts: []partSpec{{"led_lamp", 2}}}},
		{Category: "maintenance", Title: "Plumbing leak — pipa dapur bocor A-0501", Desc: "Air merembes ke unit bawah.", Priority: "critical", Location: ptr(units["A-0501"]), TenantApp: "irwan.santoso@resident.test", Age: 26 * time.Hour, Stage: "resolved", AssignTo: p.Staff["tech"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Perbaikan pipa dapur A-0501", Priority: "critical", Assignee: p.Staff["tech"], Stage: "completed", Evidence: true, DueIn: 4 * time.Hour, Parts: []partSpec{{"pipe", 2}, {"pipe_connector", 3}}}},
		{Category: "facility", Title: "Lift issue — lift Tower B sering berhenti", Desc: "", Priority: "high", Location: ptr(p.Areas["lift_lobby"]), Tenant: &tenants[5].TenantID, Channel: "phone", Age: 3 * time.Hour, Stage: "assigned", AssignTo: p.Staff["tech"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Inspeksi lift Tower B", Priority: "high", Assignee: p.Staff["tech"], Asset: ptr(p.Assets["lift"]), Stage: "assigned", DueIn: 2 * time.Hour, Vendor: "civil"}},
		{Category: "maintenance", Title: "Electrical issue — MCB unit B-0401 sering trip", Desc: "", Priority: "high", Location: ptr(units["B-0401"]), Tenant: &tenants[7].TenantID, Channel: "whatsapp", Age: 5 * 24 * time.Hour, Stage: "closed", AssignTo: p.Staff["tech2"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Pemeriksaan MCB & instalasi B-0401", Priority: "high", Assignee: p.Staff["tech2"], Stage: "closed", Evidence: true, DueIn: 12 * time.Hour, Parts: []partSpec{{"cable", 5}}, Vendor: "electrical"}},
		{Category: "cleaning", Title: "Unit cleaning request — A-0301 sebelum tamu datang", Desc: "", Priority: "medium", Location: ptr(units["A-0301"]), TenantApp: "dian.puspita@resident.test", Age: 30 * time.Hour, Stage: "closed", AssignTo: p.Staff["hk"], Rating: 4,
			WO: &woSpec{Kind: "task", Domain: "housekeeping", Type: "cleaning", Title: "Unit cleaning A-0301", Priority: "medium", Assignee: p.Staff["hk"], Stage: "closed", Template: "cleaning_room", Evidence: true, DueIn: 6 * time.Hour}},
		{Category: "cleaning", Title: "Common area cleaning — tumpahan di lobby Tower A", Desc: "", Priority: "medium", Location: ptr(p.Areas["lobby"]), Channel: "walk_in", Age: 45 * time.Minute, Stage: "assigned", AssignTo: p.Staff["hk2"],
			WO: &woSpec{Kind: "task", Domain: "housekeeping", Type: "cleaning", Title: "Spot cleaning lobby Tower A", Priority: "medium", Assignee: p.Staff["hk2"], Stage: "assigned", DueIn: 2 * time.Hour}},
		{Category: "security", Title: "Suspicious activity — orang asing mencoba masuk lantai 5", Desc: "Terlihat di CCTV koridor.", Priority: "high", Location: ptr(p.Areas["lift_lobby"]), TenantApp: "irwan.santoso@resident.test", Age: 10 * time.Hour, Stage: "resolved", AssignTo: p.Staff["sec"],
			WO: &woSpec{Kind: "task", Domain: "security", Type: "general", Title: "Pemeriksaan CCTV & patroli lantai 5", Priority: "high", Assignee: p.Staff["sec"], Stage: "completed", Evidence: true, DueIn: 2 * time.Hour}},
		{Category: "security", Title: "Access issue — kartu akses lift tidak berfungsi", Desc: "", Priority: "medium", Location: ptr(units["A-0302"]), Tenant: &tenants[2].TenantID, Channel: "phone", Age: 4 * time.Hour, Stage: "waiting_for_tenant", AssignTo: p.Staff["sec2"], Message: "Kartu pengganti sudah siap di resepsionis; mohon konfirmasi pengambilan."},
		{Category: "complaint", Title: "Complaint — kebisingan renovasi unit tetangga", Desc: "Renovasi di luar jam yang diizinkan.", Priority: "high", TenantApp: t0.AppEmail, Age: 4 * 24 * time.Hour, Stage: "closed", AssignTo: p.Staff["tr"], Message: "Kami telah menegur pemilik unit; renovasi dibatasi 09:00–17:00 hari kerja.", Rating: 3, Reopen: true},
		{Category: "inquiry", Title: "Information request — prosedur perpanjangan sewa", Desc: "", Priority: "low", TenantApp: "dian.puspita@resident.test", Age: 8 * time.Hour, Stage: "resolved", AssignTo: p.Staff["tr"], Message: "Perpanjangan dapat diajukan H-30 melalui Tenant Relation."},
		{Category: "other", Title: "Follow-up request — permintaan slot parkir tambahan", Desc: "", Priority: "low", TenantApp: t0.AppEmail, Age: 20 * time.Minute, Stage: "new"},
		{Category: "complaint", Title: "Complaint — air PAM kecil (dibatalkan)", Desc: "", Priority: "medium", Channel: "phone", Age: 3 * 24 * time.Hour, Stage: "cancelled"},
		{Category: "maintenance", Title: "AC ruang tamu bocor — B-0501 (overdue)", Desc: "", Priority: "high", Location: ptr(units["B-0501"]), Tenant: &tenants[8].TenantID, Channel: "phone", Age: 3 * 24 * time.Hour, Stage: "assigned", AssignTo: p.Staff["tech"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Perbaikan AC bocor B-0501", Priority: "high", Assignee: p.Staff["tech"], Stage: "assigned", DueIn: -24 * time.Hour}},
	}
	for _, t := range tickets {
		if _, err := s.runSR(ctx, env, p.ID, t, &p.StockLoc); err != nil {
			return uuid.Nil, err
		}
	}
	logf("apartment: %d tiket (E2E-01 AC A-1208 + parts usage + CSAT, plumbing, lift, electrical, cleaning, security, complaint reopen)", len(tickets))

	// ---- fasilitas, visitor, billing, pengumuman ----
	if err := s.seedFacilities(ctx, env, p, []facilitySpec{{"Swimming Pool", "pool", 40, false}, {"Gym", "gym", 20, false}, {"Function Room", "function_hall", 80, true}, {"BBQ Area", "bbq", 20, true}, {"Meeting Room", "meeting_room", 10, false}}, tenants, logf); err != nil {
		return uuid.Nil, err
	}
	if err := s.seedVisitors(ctx, env, p, tenants, logf); err != nil {
		return uuid.Nil, err
	}
	if err := s.seedBilling(ctx, env, p, tenants, "service_charge", 1250000, logf); err != nil {
		return uuid.Nil, err
	}
	for _, a := range []struct{ title, excerpt, body, imp string }{
		{"Maintenance Notice: Pengurasan tangki air", "Sabtu 09:00–13:00, air mati sementara", "Pengurasan tangki air Tower A & B. Mohon menampung air secukupnya.", "important"},
		{"Facility Closure: Kolam renang", "Ditutup Senin untuk perawatan", "Kolam renang ditutup untuk pembersihan filter dan pengecekan pompa.", "normal"},
		{"Building Announcement: Rapat warga bulanan", "Function Room, Minggu 10:00", "Agenda: laporan pengelolaan, rencana pengecatan fasad, dan sesi tanya jawab.", "normal"},
		{"Service Update: Jam operasional gym diperpanjang", "Kini buka 05:00–23:00", "Menanggapi masukan warga, gym kini beroperasi lebih lama.", "normal"},
	} {
		if err := s.announce(ctx, env, p.ID, a.title, a.excerpt, a.body, a.imp, 21*24*time.Hour); err != nil {
			return uuid.Nil, err
		}
	}

	// ---- unit sales (§22): listing → lead pipeline → reservasi → kontrak → handover ----
	sales := admin
	saleListings := map[string]uuid.UUID{}
	for _, l := range []struct {
		unit, title string
		price       int64
		br          int
	}{{"A-1101", "2BR Tower A 1101 — City View", 1_650_000_000, 2}, {"A-1102", "Studio Tower A 1102", 850_000_000, 1}, {"B-1101", "2BR Tower B 1101 — Pool View", 1_700_000_000, 2}, {"B-1102", "Studio Tower B 1102", 820_000_000, 1}} {
		li, err := s.Commercial.CreateListing(sales, commercial.ListingInput{PropertyID: &p.ID, UnitLocationID: ptr(units[l.unit]), Title: ptr(l.title), AskingPrice: ptr(l.price), Bedrooms: ptr(l.br), Bathrooms: ptr(1), Furnishing: ptr("semi_furnished"), Features: ptr([]string{"balcony", "city_view"}), Description: ptr("Unit siap huni dengan sertifikat strata title.")})
		if err != nil {
			return uuid.Nil, fmt.Errorf("sale listing %s: %w", l.unit, err)
		}
		if _, err := s.Commercial.ListingAct(sales, li.ID, "publish"); err != nil {
			return uuid.Nil, err
		}
		saleListings[l.unit] = li.ID
	}
	// lead 1: pipeline penuh → reservasi → sign contract → complete → handover (pemilik baru + akun Tenant App)
	ld1, err := s.Commercial.CreateLead(sales, commercial.LeadInput{PropertyID: &p.ID, ListingID: ptr(saleListings["A-1101"]), FullName: ptr("Andi Kurniawan"), Phone: ptr("081244440001"), Email: ptr("andi.buyer@prospect.test"), Source: ptr("website"), BudgetMax: ptr(int64(1_700_000_000)), Inquiry: ptr("Tanya harga & skema cicilan 2BR")})
	if err != nil {
		return uuid.Nil, fmt.Errorf("lead: %w", err)
	}
	if _, err := s.Commercial.LeadAct(sales, ld1.ID, "contact", commercial.LeadActionInput{Note: "Telepon pertama, tertarik site visit"}); err != nil {
		return uuid.Nil, err
	}
	if _, err := s.Commercial.AddActivity(sales, ld1.ID, commercial.ActivityInput{ActivityType: "site_visit", Summary: "Site visit unit A-1101 bersama keluarga"}); err != nil {
		return uuid.Nil, err
	}
	if _, err := s.Commercial.LeadAct(sales, ld1.ID, "qualify", commercial.LeadActionInput{}); err != nil {
		return uuid.Nil, err
	}
	sr1, err := s.Commercial.CreateSaleReservation(sales, commercial.SaleReservationInput{LeadID: &ld1.ID, ListingID: ptr(saleListings["A-1101"]), AgreedPrice: ptr(int64(1_600_000_000)), BookingFee: ptr(int64(50_000_000)), ReservedUntil: ptr(date(today().AddDate(0, 0, 14)))})
	if err != nil {
		return uuid.Nil, fmt.Errorf("sale reservation: %w", err)
	}
	if _, err := s.Commercial.SaleAct(sales, sr1.ID, "sign_contract", commercial.SaleActionInput{ContractReference: ptr("PPJB-2026-0007")}); err != nil {
		return uuid.Nil, err
	}
	if _, err := s.Commercial.SaleAct(sales, sr1.ID, "complete", commercial.SaleActionInput{ContractReference: ptr("AJB-2026-0007")}); err != nil {
		return uuid.Nil, err
	}
	if _, err := s.Commercial.SaleAct(sales, sr1.ID, "handover", commercial.SaleActionInput{OwnerName: ptr("Andi Kurniawan"), OwnerPhone: ptr("081244440001"), OwnerEmail: ptr("andi.buyer@prospect.test"), CreateAccount: true, Password: Password, HandoverDate: ptr(date(today().AddDate(0, 0, -2)))}); err != nil {
		return uuid.Nil, fmt.Errorf("handover: %w", err)
	}
	// lead 2: reserved (unit reserved), lead 3: contacted, lead 4: lost
	ld2, err := s.Commercial.CreateLead(sales, commercial.LeadInput{PropertyID: &p.ID, ListingID: ptr(saleListings["B-1101"]), FullName: ptr("Citra Maharani"), Phone: ptr("081244440002"), Source: ptr("walk_in"), Inquiry: ptr("Minat 2BR pool view")})
	if err != nil {
		return uuid.Nil, err
	}
	_, _ = s.Commercial.LeadAct(sales, ld2.ID, "contact", commercial.LeadActionInput{Note: "Kunjungan galeri"})
	_, _ = s.Commercial.LeadAct(sales, ld2.ID, "qualify", commercial.LeadActionInput{})
	if _, err := s.Commercial.CreateSaleReservation(sales, commercial.SaleReservationInput{LeadID: &ld2.ID, ListingID: ptr(saleListings["B-1101"]), AgreedPrice: ptr(int64(1_680_000_000)), BookingFee: ptr(int64(25_000_000)), ReservedUntil: ptr(date(today().AddDate(0, 0, 21))), IssueInvoice: ptr(true)}); err != nil {
		return uuid.Nil, fmt.Errorf("sale reservation 2: %w", err)
	}
	ld3, _ := s.Commercial.CreateLead(sales, commercial.LeadInput{PropertyID: &p.ID, ListingID: ptr(saleListings["A-1102"]), FullName: ptr("Reza Firmansyah"), Phone: ptr("081244440003"), Source: ptr("referral"), Inquiry: ptr("Studio untuk investasi")})
	if ld3 != nil {
		_, _ = s.Commercial.LeadAct(sales, ld3.ID, "contact", commercial.LeadActionInput{Note: "Follow-up minggu depan"})
	}
	ld4, _ := s.Commercial.CreateLead(sales, commercial.LeadInput{PropertyID: &p.ID, ListingID: ptr(saleListings["B-1102"]), FullName: ptr("Tono Sudarto"), Phone: ptr("081244440004"), Source: ptr("website")})
	if ld4 != nil {
		_, _ = s.Commercial.LeadAct(sales, ld4.ID, "contact", commercial.LeadActionInput{Note: "Menghubungi via WhatsApp"})
		_, _ = s.Commercial.LeadAct(sales, ld4.ID, "lose", commercial.LeadActionInput{Reason: "Memilih properti lain"})
	}

	// ---- unit rental (§22): listing daily/weekly/monthly → reservasi daily selesai, weekly aktif (onboarding), monthly upcoming, inquiry ----
	rentalListings := map[string]uuid.UUID{}
	for _, l := range []struct {
		unit, title string
		d, w, m     int64
	}{{"A-0701", "Studio Furnished A-0701 (harian/mingguan/bulanan)", 550_000, 3_300_000, 9_500_000}, {"A-0702", "2BR Furnished A-0702", 900_000, 5_400_000, 15_000_000}, {"B-0701", "Studio Furnished B-0701", 500_000, 3_000_000, 9_000_000}} {
		rl, err := s.Commercial.CreateRentalListing(sales, commercial.RentalListingInput{PropertyID: &p.ID, UnitLocationID: ptr(units[l.unit]), Title: ptr(l.title), RateDaily: ptr(l.d), RateWeekly: ptr(l.w), RateMonthly: ptr(l.m), DepositAmount: ptr(l.m / 2), MinStayDays: ptr(2), MaxOccupants: ptr(3), Bedrooms: ptr(1), Bathrooms: ptr(1), Furnishing: ptr("furnished"), Features: ptr([]string{"wifi", "ac", "kitchen"}), AvailableFrom: ptr(date(today().AddDate(0, -3, 0)))})
		if err != nil {
			return uuid.Nil, fmt.Errorf("rental listing %s: %w", l.unit, err)
		}
		if _, err := s.Commercial.RentalListingAct(sales, rl.ID, "publish"); err != nil {
			return uuid.Nil, err
		}
		rentalListings[l.unit] = rl.ID
	}
	// daily (selesai): 3 hari, 2 minggu lalu — reserved → active → completed
	rrD, err := s.Commercial.CreateRentalReservation(sales, commercial.RentalReservationInput{ListingID: ptr(rentalListings["A-0701"]), ProspectName: ptr("Fajar Nugraha"), ProspectPhone: ptr("081255550001"), ProspectEmail: ptr("fajar.daily@rent.test"), RentalPeriod: ptr("daily"), PeriodCount: ptr(3), StartDate: ptr(date(today().AddDate(0, 0, -14))), Source: ptr("phone"), Confirm: true})
	if err != nil {
		return uuid.Nil, fmt.Errorf("rental daily: %w", err)
	}
	if _, err := s.Commercial.RentalAct(sales, rrD.ID, "activate", commercial.RentalActionInput{TenantName: ptr("Fajar Nugraha"), TenantPhone: ptr("081255550001"), TenantType: ptr("individual"), MoveInDate: ptr(date(today().AddDate(0, 0, -14)))}); err != nil {
		return uuid.Nil, fmt.Errorf("rental daily activate: %w", err)
	}
	if _, err := s.Commercial.RentalAct(sales, rrD.ID, "complete", commercial.RentalActionInput{MoveOutDate: ptr(date(today().AddDate(0, 0, -11)))}); err != nil {
		return uuid.Nil, fmt.Errorf("rental daily complete: %w", err)
	}
	// weekly (aktif): 2 minggu, mulai 3 hari lalu — onboarding tenant + akun Tenant App
	rrW, err := s.Commercial.CreateRentalReservation(sales, commercial.RentalReservationInput{ListingID: ptr(rentalListings["A-0702"]), ProspectName: ptr("Gita Savitri"), ProspectPhone: ptr("081255550002"), ProspectEmail: ptr("gita.weekly@rent.test"), Occupants: ptr(2), RentalPeriod: ptr("weekly"), PeriodCount: ptr(2), StartDate: ptr(date(today().AddDate(0, 0, -3))), Source: ptr("website"), Confirm: true})
	if err != nil {
		return uuid.Nil, fmt.Errorf("rental weekly: %w", err)
	}
	if _, err := s.Commercial.RentalAct(sales, rrW.ID, "activate", commercial.RentalActionInput{TenantName: ptr("Gita Savitri"), TenantPhone: ptr("081255550002"), TenantEmail: ptr("gita.weekly@rent.test"), TenantType: ptr("individual"), CreateAccount: true, Password: Password, MoveInDate: ptr(date(today().AddDate(0, 0, -3)))}); err != nil {
		return uuid.Nil, fmt.Errorf("rental weekly activate: %w", err)
	}
	// monthly (confirmed, mulai 10 hari lagi) + inquiry baru (belum memblokir) + inquiry dibatalkan
	if _, err := s.Commercial.CreateRentalReservation(sales, commercial.RentalReservationInput{ListingID: ptr(rentalListings["B-0701"]), ProspectName: ptr("Hasan Basri"), ProspectPhone: ptr("081255550003"), RentalPeriod: ptr("monthly"), PeriodCount: ptr(6), StartDate: ptr(date(today().AddDate(0, 0, 10))), Source: ptr("phone"), Confirm: true}); err != nil {
		return uuid.Nil, fmt.Errorf("rental monthly: %w", err)
	}
	if _, err := s.Commercial.CreateRentalReservation(sales, commercial.RentalReservationInput{ListingID: ptr(rentalListings["A-0701"]), ProspectName: ptr("Intan Permata"), ProspectPhone: ptr("081255550004"), RentalPeriod: ptr("monthly"), PeriodCount: ptr(1), StartDate: ptr(date(today().AddDate(0, 1, 0))), Source: ptr("website"), SpecialRequests: ptr("Butuh parkir mobil")}); err != nil {
		return uuid.Nil, fmt.Errorf("rental inquiry: %w", err)
	}
	inqC, err := s.Commercial.CreateRentalReservation(sales, commercial.RentalReservationInput{ListingID: ptr(rentalListings["B-0701"]), ProspectName: ptr("Joni Iskandar"), ProspectPhone: ptr("081255550005"), RentalPeriod: ptr("weekly"), PeriodCount: ptr(1), StartDate: ptr(date(today().AddDate(0, 2, 0)))})
	if err == nil {
		_, _ = s.Commercial.RentalAct(sales, inqC.ID, "cancel", commercial.RentalActionInput{Reason: "Pindah ke unit lain"})
	}
	s.flush(ctx)
	logf("apartment: unit sales 4 listing, 4 lead (sold+handover, reserved, contacted, lost); unit rental 3 listing, daily selesai / weekly aktif / monthly upcoming / inquiry")

	// ---- BVRooms apartemen (sewa harian unit studio) ----
	if err := s.seedBVRoomsApartment(ctx, env, p, units, logf); err != nil {
		return uuid.Nil, err
	}
	return p.ID, nil
}

// seedBVRoomsApartment: listing 'vision-residence' kategori apartment, tipe unit Studio (3 unit rentable), booking pay-at-property
// (PAID → check-in unit otomatis) dan booking UNPAID.
func (s *Service) seedBVRoomsApartment(ctx context.Context, env *Env, p *prop, units map[string]uuid.UUID, logf func(string, ...any)) error {
	admin := p.admin
	ut, err := s.BVRooms.CreateUnitType(admin, bvrooms.UnitTypeInput{PropertyID: &p.ID, Name: ptr("Studio Furnished 36 m²"), Description: ptr("Unit studio furnished dengan dapur kecil dan balkon; sewa harian."), CapacityAdults: ptr(2), CapacityChildren: ptr(1), Bedrooms: ptr(1), SizeM2: ptr(36.0), Amenities: ptr([]string{"wifi", "ac", "tv", "kitchen", "washing_machine", "balcony"}), BaseRate: ptr(int64(650000))})
	if err != nil {
		return fmt.Errorf("unit type: %w", err)
	}
	for _, code := range []string{"A-0901", "A-1001", "B-0901"} {
		if _, err := s.BVRooms.SetUnitRental(admin, units[code], bvrooms.UnitRentalInput{UnitTypeID: &ut.ID, RentableDaily: ptr(true)}); err != nil {
			return fmt.Errorf("unit rental %s: %w", code, err)
		}
	}
	desc := []bvrooms.DescriptionSection{{Key: "lokasi", Title: "Lokasi", Body: "Vision Residence di Kemang, 10 menit ke stasiun MRT dan pusat kuliner."}, {Key: "fasilitas", Title: "Fasilitas", Body: "Kolam renang, gym, function room, BBQ area, dan keamanan 24 jam."}}
	if _, err := s.BVRooms.UpdateListing(admin, p.ID, bvrooms.ListingInput{Listed: ptr(true), Slug: ptr("vision-residence"), Category: ptr("apartment"), DisplayName: ptr("Vision Residence"), Tagline: ptr("Sewa harian unit furnished"), AddressLine: ptr("Jl. Kemang Raya No. 88"), District: ptr("Kemang"), City: ptr("Jakarta Selatan"), Lat: ptr(-6.2607), Lng: ptr(106.8135), Phone: ptr("+62217190088"), CheckInTime: ptr("15:00"), CheckOutTime: ptr("12:00"),
		DescriptionSections: &desc, Facilities: ptr([]string{"wifi", "parking", "pool", "gym", "elevator", "cctv", "no_smoking"}), Policies: ptr([]string{"Penghuni wajib menunjukkan KTP saat check in.", "Dilarang membawa hewan peliharaan."}), CancellationPolicy: ptr(cancelPolicyMD), PaymentWindowHours: ptr(6), AllowPayAtProperty: ptr(true), BankAccounts: ptr(jsonRaw(`[{"bank":"BCA","account_number":"3000108765499","account_name":"PT Vision Residence Management"}]`))}, nil); err != nil {
		return fmt.Errorf("bvrooms apt listing: %w", err)
	}
	if err := s.bvPhoto(ctx, env, p.ID, "facade", nil, nil, "Vision Residence Tower A & B", 0, true); err != nil {
		return err
	}
	if err := s.bvPhoto(ctx, env, p.ID, "room", nil, &ut.ID, "Studio furnished", 1, false); err != nil {
		return err
	}
	if err := s.bvPhoto(ctx, env, p.ID, "pool", nil, nil, "Kolam renang podium", 2, false); err != nil {
		return err
	}
	cust, err := s.bvCustomerIDs(ctx, env)
	if err != nil {
		return err
	}
	c2 := s.asCustomer(ctx, env, cust["c2"], "Aan Prayitno")
	// pay at property → PAID; Front Office (dashboard BVRooms) check-in dengan unit bebas otomatis
	b, err := s.BVRooms.CreateBooking(c2, bvrooms.CreateBookingInput{PropertyID: p.ID, CheckIn: date(today()), CheckOut: date(today().AddDate(0, 0, 3)), Guest: bvrooms.GuestInput{FullName: "Aan Prayitno", Email: "bvrooms.customer2@buildingvision.local", Phone: "+6281200000102"}, Rooms: []bvrooms.CreateRoomInput{{TypeID: ut.ID, Adults: 2}}, Payment: &bvrooms.CreatePaymentInput{MethodCode: "cash_on_site"}})
	if err != nil {
		return fmt.Errorf("bvrooms apt booking: %w", err)
	}
	if _, err := s.BVRooms.AdminBookingAction(admin, b.ID, "check_in", bvrooms.AdminActionInput{}); err != nil {
		return fmt.Errorf("bvrooms apt check-in: %w", err)
	}
	// UNPAID (transfer dipilih)
	c4 := s.asCustomer(ctx, env, cust["c4"], "Dina Kartika")
	b2, err := s.BVRooms.CreateBooking(c4, bvrooms.CreateBookingInput{PropertyID: p.ID, CheckIn: date(today().AddDate(0, 0, 8)), CheckOut: date(today().AddDate(0, 0, 10)), Guest: bvrooms.GuestInput{FullName: "Dina Kartika", Email: "bvrooms.customer4@buildingvision.local", Phone: "+6281200000104"}, Rooms: []bvrooms.CreateRoomInput{{TypeID: ut.ID, Adults: 1}}})
	if err != nil {
		return err
	}
	if _, err := s.BVRooms.CreatePayment(c4, b2.BookingCode, bvrooms.CreatePaymentReq{ProviderCode: "manual", MethodCode: "transfer_bca"}); err != nil {
		return err
	}
	s.flush(ctx)
	logf("apartment: BVRooms listing 'vision-residence' (tipe Studio, 3 unit sewa harian), booking pay-at-property CHECK IN + UNPAID")
	return nil
}

var _ = operations.ObjTask
