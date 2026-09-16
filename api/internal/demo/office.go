package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantrelation"
)

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

// seedOffice: Vision Business Center (§3.3, §23) — Main Tower 10 lantai, 30 office unit, 10 tenant company + PIC +
// authorized users pada unit berbeda (isolasi lintas unit), common area, E2E-03 (security visitor access) & E2E-04 (tenant relation).
func (s *Service) seedOffice(ctx context.Context, env *Env, logf func(string, ...any)) (uuid.UUID, error) {
	p, err := s.seedBase(ctx, env, propSpec{Name: "Vision Business Center", Profile: ProfileOffice, Prefix: "office", Address: "Jl. HR Rasuna Said Kav. X-5", City: "Jakarta Selatan", Timezone: "Asia/Jakarta", Building: "Main Tower", FloorFrom: 0, FloorTo: 10, Lat: -6.2200, Lng: 106.8320}, logf)
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
	// 30 office unit: lantai 1–10 × 3 unit (x01, x02, x03)
	units := map[string]uuid.UUID{}
	for f := 1; f <= 10; f++ {
		for n := 1; n <= 3; n++ {
			code := fmt.Sprintf("%d%02d", f, n)
			area := []float64{120, 180, 240}[n-1]
			id, err := mk(property.LTUnit, p.Floors[f], "Office Unit "+code, map[string]any{"unit_number": code, "unit_type": "commercial", "area_m2": area})
			if err != nil {
				return uuid.Nil, err
			}
			units[code] = id
		}
	}
	// common area, meeting room, facility, visitor & security area
	for _, a := range []struct {
		key, name, typ string
		floor          int
		space          bool
	}{
		{"mr_a", "Meeting Room A", "meeting_room", 2, true}, {"mr_b", "Meeting Room B", "meeting_room", 2, true}, {"conf", "Conference Room", "meeting_room", 3, true}, {"training", "Training Room", "meeting_room", 3, true}, {"lounge", "Tenant Lounge", "lounge", 1, true},
		{"reception", "Visitor Reception & Registration", "lobby", 0, false}, {"security_post", "Pos Keamanan Utama", "other", 0, false}, {"pantry", "Pantry Bersama Lantai 5", "other", 5, false}, {"prayer", "Musholla", "other", 1, false},
	} {
		var id uuid.UUID
		var err error
		if a.space {
			id, err = mk(property.LTSpace, p.Floors[a.floor], a.name, map[string]any{"space_type": a.typ, "area_m2": 40.0})
		} else {
			id, err = mk(property.LTArea, p.Floors[a.floor], a.name, map[string]any{"area_type": a.typ})
		}
		if err != nil {
			return uuid.Nil, err
		}
		p.Areas[a.key] = id
	}
	if err := s.markReportable(ctx, env, p.ID); err != nil {
		return uuid.Nil, err
	}

	// ---- 10 tenant company + PIC + authorized users (§7 Office) ----
	tr := s.asEmail(ctx, env, "tenantrelation.demo@buildingvision.local")
	type cDef struct {
		name, units0, units1, pic, phone, email string
		app                                     []string // email akun Tenant App (index 0 = PIC/tenant_admin)
	}
	cos := []cDef{
		{"PT Nusantara Digital", "501", "502", "Rina Marlina", "081266660001", "office.tenant@buildingvision.local", []string{"office.tenant@buildingvision.local", "office.tenant2@buildingvision.local"}},
		{"PT Sinar Logistik", "101", "", "Bambang Sutrisno", "081266660002", "bambang@sinarlogistik.test", []string{"bambang@sinarlogistik.test"}},
		{"CV Kreatif Media", "201", "", "Laras Ayu", "081266660003", "laras@kreatifmedia.test", []string{"laras@kreatifmedia.test"}},
		{"PT Bank Vision Syariah", "301", "302", "Ahmad Fauzi", "081266660004", "ahmad@bankvision.test", nil},
		{"PT Konsultan Prima", "401", "", "Melati Sari", "081266660005", "melati@konsultanprima.test", []string{"melati@konsultanprima.test"}},
		{"PT Global Insurance", "601", "602", "Yohanes Purba", "081266660006", "yohanes@globalins.test", nil},
		{"PT Teknologi Maju", "701", "", "Sari Utami", "081266660007", "sari@teknomaju.test", nil},
		{"Kantor Hukum Adi & Rekan", "801", "", "Adi Nugroho", "081266660008", "adi@adirekan.test", nil},
		{"PT Ekspor Pangan", "901", "902", "Dewi Lestari", "081266660009", "dewi@eksporpangan.test", nil},
		{"PT Startup Vision", "1001", "", "Gilang Ramadhan", "081266660010", "gilang@startupvision.test", []string{"gilang@startupvision.test"}},
	}
	var tenants []tenantRef
	for _, c := range cos {
		uids := []uuid.UUID{units[c.units0]}
		if c.units1 != "" {
			uids = append(uids, units[c.units1])
		}
		ten, err := s.Property.CreateTenant(admin, property.TenantInput{PropertyID: &p.ID, Name: ptr(c.name), TenantType: ptr("company"), ContactName: ptr(c.pic), ContactPhone: ptr("+62" + c.phone[1:]), ContactEmail: ptr(c.email), Status: ptr("active"), UnitIDs: &uids})
		if err != nil {
			return uuid.Nil, fmt.Errorf("tenant company %s: %w", c.name, err)
		}
		occ, err := s.Property.CreateOccupant(admin, property.OccupantInput{TenantID: &ten.ID, FullName: ptr(c.pic), Phone: ptr("+62" + c.phone[1:]), Email: ptr(c.email), IsPrimaryContact: ptr(true), UnitIDs: &uids})
		if err != nil {
			return uuid.Nil, fmt.Errorf("pic %s: %w", c.pic, err)
		}
		ref := tenantRef{TenantID: ten.ID, Name: c.name, UnitID: units[c.units0], UnitLabel: "Office Unit " + c.units0, OccupantID: &occ.ID}
		for i, email := range c.app {
			role, name, unitIDs := "tenant_admin", c.pic, uids
			occID := &occ.ID
			if i > 0 {
				// authorized user kedua: hanya unit kedua (isolasi lintas unit dapat dites)
				role, name, occID = "tenant_user", "Staff "+c.name, nil
				if c.units1 != "" {
					unitIDs = []uuid.UUID{units[c.units1]}
				}
			} else {
				ref.AppEmail = email
			}
			if _, err := s.TR.Create(tr, tenantrelation.CreateInput{PropertyID: p.ID, TenantID: &ten.ID, OccupantID: occID, FullName: name, Email: email, Phone: "+62" + c.phone[1:], Role: role, OwnershipStatus: "employee", UnitIDs: unitIDs, Password: Password}); err != nil {
				return uuid.Nil, fmt.Errorf("tenant app %s: %w", email, err)
			}
		}
		tenants = append(tenants, ref)
	}
	p.Tenants = tenants
	s.flush(ctx)
	logf("office: %d office unit, %d tenant company + PIC, Tenant App office.tenant@ (admin unit 501/502) & office.tenant2@ (user unit 502) + 4 PIC lain", len(units), len(cos))

	// ---- tiket & E2E-03 (security visitor access issue) / E2E-04 (tenant relation complaint) ----
	t0 := tenants[0]
	tickets := []srSpec{
		// E2E-03 — Security: tamu tenant ditolak akses lift → Security Task → verifikasi → resolusi → notifikasi tenant
		{Category: "security", Title: "Visitor access issue — tamu tidak bisa masuk lift lantai 5", Desc: "Tamu sudah terdaftar tetapi kartu visitor tidak bisa akses lift.", Priority: "high", Location: ptr(p.Areas["reception"]), TenantApp: t0.AppEmail, Age: 5 * time.Hour, Stage: "closed", AssignTo: p.Staff["sec"], Rating: 5,
			WO: &woSpec{Kind: "task", Domain: "security", Type: "general", Title: "Verifikasi visitor pass & reset akses lift lantai 5", Priority: "high", Assignee: p.Staff["sec"], Stage: "closed", Evidence: true, DueIn: 1 * time.Hour}},
		// E2E-04 — Tenant Relation: complaint → triage → komunikasi → follow-up → resolusi → feedback
		{Category: "complaint", Title: "Complaint — AC lobby lantai 5 terlalu dingin & pantry kotor", Desc: "Beberapa karyawan mengeluhkan suhu lobby lift lantai 5 dan kondisi pantry bersama.", Priority: "medium", Location: ptr(p.Areas["pantry"]), TenantApp: t0.AppEmail, Age: 3 * 24 * time.Hour, Stage: "closed", AssignTo: p.Staff["tr"], Message: "Terima kasih atas laporannya. Suhu AC lobby kami sesuaikan ke 24°C dan jadwal cleaning pantry ditambah menjadi 2x sehari.", Rating: 4,
			WO: &woSpec{Kind: "task", Domain: "housekeeping", Type: "cleaning", Title: "Cleaning tambahan pantry lantai 5", Priority: "medium", Assignee: p.Staff["hk"], Stage: "closed", Template: "cleaning_common", Evidence: true, DueIn: 4 * time.Hour}},
		{Category: "maintenance", Title: "AC tidak dingin — Office Unit 501", Desc: "Ruang rapat internal panas sejak pagi.", Priority: "high", Location: ptr(units["501"]), TenantApp: t0.AppEmail, Age: 28 * time.Hour, Stage: "closed", AssignTo: p.Staff["tech"], Rating: 5,
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Perbaikan FCU Unit 501", Priority: "high", Assignee: p.Staff["tech"], Stage: "closed", Template: "pm_ac", Evidence: true, DueIn: 6 * time.Hour, Parts: []partSpec{{"ac_filter", 2}}}},
		{Category: "maintenance", Title: "Lampu mati — koridor lantai 1", Desc: "", Priority: "medium", Location: ptr(p.Areas["corridor"]), TenantApp: "laras@kreatifmedia.test", Age: 7 * time.Hour, Stage: "in_progress", AssignTo: p.Staff["tech2"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Ganti lampu koridor lantai 2", Priority: "medium", Assignee: p.Staff["tech2"], Stage: "in_progress", Evidence: true, DueIn: 6 * time.Hour, Parts: []partSpec{{"led_lamp", 3}}}},
		{Category: "maintenance", Title: "Plumbing leak — toilet lantai 1 bocor", Desc: "", Priority: "high", Location: ptr(p.Areas["toilet"]), Channel: "walk_in", Age: 22 * time.Hour, Stage: "resolved", AssignTo: p.Staff["tech"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Perbaikan kebocoran toilet lantai 1", Priority: "high", Assignee: p.Staff["tech"], Stage: "completed", Evidence: true, DueIn: 4 * time.Hour, Parts: []partSpec{{"water_valve", 1}, {"pipe_connector", 2}}}},
		{Category: "facility", Title: "Lift issue — lift 1 pintu tidak menutup sempurna", Desc: "", Priority: "critical", Location: ptr(p.Areas["lift_lobby"]), Tenant: &tenants[3].TenantID, Channel: "phone", Age: 2 * time.Hour, Stage: "assigned", AssignTo: p.Staff["tech"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Inspeksi darurat Lift 1", Priority: "critical", Assignee: p.Staff["tech"], Asset: ptr(p.Assets["lift"]), Stage: "assigned", DueIn: 1 * time.Hour, Vendor: "civil"}},
		{Category: "maintenance", Title: "Electrical issue — listrik unit 801 sering padam", Desc: "", Priority: "high", Location: ptr(units["801"]), Tenant: &tenants[7].TenantID, Channel: "email", Age: 4 * 24 * time.Hour, Stage: "closed", AssignTo: p.Staff["tech2"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Pemeriksaan panel unit 801", Priority: "high", Assignee: p.Staff["tech2"], Stage: "closed", Evidence: true, DueIn: 8 * time.Hour, Vendor: "electrical"}},
		{Category: "cleaning", Title: "Cleaning request — karpet unit 401 kotor", Desc: "", Priority: "low", Location: ptr(units["401"]), TenantApp: "melati@konsultanprima.test", Age: 50 * time.Minute, Stage: "assigned", AssignTo: p.Staff["hk2"],
			WO: &woSpec{Kind: "task", Domain: "housekeeping", Type: "cleaning", Title: "Cleaning karpet unit 401", Priority: "low", Assignee: p.Staff["hk2"], Stage: "assigned", DueIn: 3 * time.Hour}},
		{Category: "cleaning", Title: "Spill — tumpahan kopi di lounge", Desc: "", Priority: "medium", Location: ptr(p.Areas["lounge"]), Channel: "walk_in", Age: 3 * time.Hour, Stage: "resolved", AssignTo: p.Staff["hk"],
			WO: &woSpec{Kind: "task", Domain: "housekeeping", Type: "cleaning", Title: "Spot cleaning lounge", Priority: "medium", Assignee: p.Staff["hk"], Stage: "completed", Evidence: true, DueIn: 1 * time.Hour}},
		{Category: "security", Title: "Lost item — laptop tertinggal di Meeting Room A", Desc: "", Priority: "high", Location: ptr(p.Areas["mr_a"]), TenantApp: "gilang@startupvision.test", Age: 9 * time.Hour, Stage: "resolved", AssignTo: p.Staff["sec2"],
			WO: &woSpec{Kind: "task", Domain: "security", Type: "general", Title: "Penelusuran barang tertinggal Meeting Room A", Priority: "high", Assignee: p.Staff["sec2"], Stage: "completed", Evidence: true, DueIn: 2 * time.Hour}},
		{Category: "security", Title: "Security incident — pintu darurat lantai 7 terbuka", Desc: "", Priority: "high", Location: ptr(p.Floors[7]), Channel: "walk_in", Age: 6 * time.Hour, Stage: "waiting_for_tenant", AssignTo: p.Staff["sec"], Message: "Apakah ada staf Anda yang menggunakan pintu darurat pagi ini?"},
		{Category: "inquiry", Title: "Information request — prosedur akses lembur akhir pekan", Desc: "", Priority: "low", TenantApp: "bambang@sinarlogistik.test", Age: 5 * time.Hour, Stage: "resolved", AssignTo: p.Staff["tr"], Message: "Ajukan form lembur H-1 ke Tenant Relation; akses lift diaktifkan sesuai jadwal."},
		{Category: "other", Title: "Follow-up request — penambahan kartu akses karyawan baru", Desc: "", Priority: "low", TenantApp: t0.AppEmail, Age: 25 * time.Minute, Stage: "new"},
		{Category: "complaint", Title: "Complaint — parkir penuh (dibatalkan)", Desc: "", Priority: "medium", Channel: "phone", Age: 2 * 24 * time.Hour, Stage: "cancelled"},
		{Category: "maintenance", Title: "AC unit 601 bocor (overdue)", Desc: "", Priority: "high", Location: ptr(units["601"]), Tenant: &tenants[5].TenantID, Channel: "phone", Age: 3 * 24 * time.Hour, Stage: "assigned", AssignTo: p.Staff["tech"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Perbaikan AC bocor unit 601", Priority: "high", Assignee: p.Staff["tech"], Stage: "assigned", DueIn: -20 * time.Hour}},
	}
	for _, t := range tickets {
		if _, err := s.runSR(ctx, env, p.ID, t, &p.StockLoc); err != nil {
			return uuid.Nil, err
		}
	}
	logf("office: %d tiket (E2E-03 security visitor access, E2E-04 tenant relation complaint, engineering, housekeeping)", len(tickets))

	// ---- fasilitas (meeting room A/B, conference, training, lounge), visitor, billing, pengumuman ----
	if err := s.seedFacilities(ctx, env, p, []facilitySpec{{"Meeting Room A", "meeting_room", 8, false}, {"Meeting Room B", "meeting_room", 12, false}, {"Conference Room", "meeting_room", 40, true}, {"Training Room", "meeting_room", 30, true}, {"Tenant Lounge", "lounge", 25, false}}, tenants, logf); err != nil {
		return uuid.Nil, err
	}
	if err := s.seedVisitors(ctx, env, p, tenants, logf); err != nil {
		return uuid.Nil, err
	}
	if err := s.seedBilling(ctx, env, p, tenants, "service_charge", 18500000, logf); err != nil {
		return uuid.Nil, err
	}
	for _, a := range []struct{ title, excerpt, body, imp string }{
		{"Maintenance Notice: Pemadaman listrik terjadwal", "Sabtu 22:00–02:00 untuk perawatan LVMDP", "Genset akan menopang beban penting. Mohon matikan perangkat sensitif.", "important"},
		{"Facility Closure: Conference Room", "Renovasi audio-visual 3 hari", "Conference Room ditutup; gunakan Training Room sebagai alternatif.", "normal"},
		{"Building Announcement: Simulasi evakuasi", "Kamis 09:00 — seluruh tenant wajib ikut", "Ikuti arahan floor warden; titik kumpul di area parkir LG.", "important"},
		{"Service Update: Jam operasional lounge", "Kini buka 07:00–21:00", "Tenant Lounge kini beroperasi lebih lama pada hari kerja.", "normal"},
	} {
		if err := s.announce(ctx, env, p.ID, a.title, a.excerpt, a.body, a.imp, 21*24*time.Hour); err != nil {
			return uuid.Nil, err
		}
	}
	return p.ID, nil
}
