package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/hotel"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/operations"
)

// seedHotel: Grand Vision Hotel (§3.1, §21, §21A) — 4 tipe kamar, 30 kamar (301–310, 401–410, 501–510), rate standar /
// akhir pekan / peak, reservasi lintas status, Guest App, tiket E2E-02 (housekeeping) & engineering, BVRooms lengkap.
func (s *Service) seedHotel(ctx context.Context, env *Env, logf func(string, ...any)) (uuid.UUID, error) {
	p, err := s.seedBase(ctx, env, propSpec{Name: "Grand Vision Hotel", Profile: ProfileHotel, Prefix: "hotel", Address: "Jl. Jenderal Sudirman Kav. 52, Senayan", City: "Jakarta Selatan", Timezone: "Asia/Jakarta", Building: "Main Tower", FloorFrom: 0, FloorTo: 10, Lat: -6.2250, Lng: 106.8090}, logf)
	if err != nil {
		return uuid.Nil, err
	}
	admin := p.admin
	fo := admin // Front Office (admin org: reservasi + pembuatan akun Guest App butuh tenant_relation.*)

	// ---- room types + rates ----
	type rtDef struct {
		key, name, desc, bed string
		adults, children     int
		size                 float64
		rate                 int64
		amenities            []string
		floor                int
	}
	rts := []rtDef{
		{"standard", "Standard Room", "Kamar nyaman 24 m² dengan tempat tidur queen, cocok untuk perjalanan bisnis.", "queen", 2, 1, 24, 850000, []string{"wifi", "ac", "tv", "bathtub"}, 3},
		{"deluxe", "Deluxe Room", "Kamar 32 m² dengan pemandangan kota dan area kerja.", "king", 2, 1, 32, 1250000, []string{"wifi", "ac", "tv", "bathtub", "mini_fridge", "breakfast"}, 4},
		{"executive", "Executive Room", "Kamar 40 m² akses Executive Lounge, sarapan termasuk.", "king", 3, 1, 40, 1850000, []string{"wifi", "ac", "tv", "bathtub", "mini_fridge", "breakfast", "coffee_shop"}, 5},
		{"suite", "Suite", "Suite 65 m² dengan ruang tamu terpisah dan pantry.", "king", 3, 2, 65, 3200000, []string{"wifi", "ac", "tv", "bathtub", "mini_fridge", "breakfast", "kitchen", "balcony"}, 5},
	}
	rtIDs := map[string]uuid.UUID{}
	roomIDs := map[string]uuid.UUID{} // nomor → location id
	roomType := map[string]string{}   // nomor → key
	for _, rt := range rts {
		out, err := s.Hotel.CreateRoomType(admin, hotel.RoomTypeInput{PropertyID: &p.ID, Name: ptr(rt.name), Description: ptr(rt.desc), CapacityAdults: ptr(rt.adults), CapacityChildren: ptr(rt.children), BedType: ptr(rt.bed), SizeM2: ptr(rt.size), Amenities: ptr(rt.amenities), BaseRate: ptr(rt.rate)})
		if err != nil {
			return uuid.Nil, fmt.Errorf("room type %s: %w", rt.name, err)
		}
		rtIDs[rt.key] = out.ID
		if _, err := s.Hotel.CreateRate(admin, hotel.RateInput{RoomTypeID: &out.ID, Name: ptr("Weekend Rate"), RatePerNight: ptr(rt.rate * 120 / 100), Weekdays: ptr([]int{5, 6}), Priority: ptr(10)}); err != nil {
			return uuid.Nil, err
		}
		peakFrom := today().AddDate(0, 0, 20)
		if _, err := s.Hotel.CreateRate(admin, hotel.RateInput{RoomTypeID: &out.ID, Name: ptr("Peak Season Rate"), RatePerNight: ptr(rt.rate * 150 / 100), ValidFrom: ptr(date(peakFrom)), ValidUntil: ptr(date(peakFrom.AddDate(0, 0, 10))), Priority: ptr(20)}); err != nil {
			return uuid.Nil, err
		}
	}
	// 30 kamar: lantai 3 = Standard (301–310), lantai 4 = Deluxe (401–410), lantai 5 = Executive 501–506 + Suite 507–510
	for f := 3; f <= 5; f++ {
		for n := 1; n <= 10; n++ {
			num := fmt.Sprintf("%d%02d", f, n)
			key := map[int]string{3: "standard", 4: "deluxe", 5: "executive"}[f]
			if f == 5 && n >= 7 {
				key = "suite"
			}
			room, err := s.Hotel.CreateRoom(admin, hotel.RoomInput{PropertyID: &p.ID, FloorID: ptr(p.Floors[f]), RoomTypeID: rtIDs[key], RoomNumber: num})
			if err != nil {
				return uuid.Nil, fmt.Errorf("room %s: %w", num, err)
			}
			roomIDs[num] = room.LocationID
			roomType[num] = key
		}
	}
	// status kamar realistis: dirty (cleaning), clean, inspected, out_of_order (maintenance), out_of_service
	hkSpv := s.asEmail(ctx, env, "housekeeping.demo@buildingvision.local")
	for num, st := range map[string][]string{"306": {"dirty"}, "307": {"dirty", "clean"}, "308": {"dirty", "clean", "inspected"}, "409": {"out_of_order"}, "410": {"out_of_service"}, "506": {"dirty"}} {
		for _, to := range st {
			if _, err := s.Hotel.SetRoomStatus(hkSpv, roomIDs[num], hotel.RoomStatusInput{RoomStatus: to, Note: ptr("Status demo")}); err != nil {
				return uuid.Nil, fmt.Errorf("room status %s→%s: %w", num, to, err)
			}
		}
	}
	logf("hotel: 4 tipe kamar, 30 kamar, rate standar/weekend/peak, status kamar bervariasi")

	// ---- reservasi Front Office (§7 Hotel, §21): 10 tamu ----
	type resDef struct {
		guest, phone, email, rt, room string
		ciOff, nights                 int
		stage                         string // upcoming | confirmed | checked_in | checked_out | cancelled
		adults                        int
		guestApp                      string // email akun Guest App saat check-in
	}
	resDefs := []resDef{
		{"Andi Prasetyo", "081211110001", "andi.prasetyo@guest.test", "executive", "501", -3, 5, "checked_in", 2, "hotel.guest@buildingvision.local"},
		{"Maria Chandra", "081211110002", "maria.chandra@guest.test", "deluxe", "401", -1, 3, "checked_in", 2, ""},
		{"Robert Tanaka", "081211110003", "robert.tanaka@guest.test", "suite", "507", -2, 4, "checked_in", 3, ""},
		{"Sinta Dewi", "081211110004", "sinta.dewi@guest.test", "standard", "302", 2, 2, "confirmed", 1, ""},
		{"Hendra Gunawan", "081211110005", "hendra.gunawan@guest.test", "standard", "301", 5, 3, "confirmed", 2, ""},
		{"Yuki Sato", "081211110006", "yuki.sato@guest.test", "deluxe", "", 9, 2, "upcoming", 2, ""},
		{"Putri Ayu", "081211110007", "putri.ayu@guest.test", "standard", "303", -10, 2, "checked_out", 2, ""},
		{"Daniel Wong", "081211110008", "daniel.wong@guest.test", "executive", "502", -7, 3, "checked_out", 1, ""},
		{"Lestari Handayani", "081211110009", "lestari.h@guest.test", "deluxe", "402", -20, 4, "checked_out", 2, ""},
		{"Kevin Lim", "081211110010", "kevin.lim@guest.test", "suite", "", 12, 2, "cancelled", 2, ""},
		{"Nadia Rahma", "081211110011", "nadia.rahma@guest.test", "standard", "", 4, 1, "cancelled", 1, ""},
	}
	var activeStayTenant *tenantRef
	for _, r := range resDefs {
		ci := today().AddDate(0, 0, r.ciOff)
		co := ci.AddDate(0, 0, r.nights)
		var roomLoc *uuid.UUID
		if r.room != "" {
			id := roomIDs[r.room]
			roomLoc = &id
		}
		res, err := s.Hotel.CreateReservation(fo, hotel.ReservationInput{PropertyID: &p.ID, RoomTypeID: ptr(rtIDs[r.rt]), RoomLocationID: roomLoc, GuestName: ptr(r.guest), GuestPhone: ptr(r.phone), GuestEmail: ptr(r.email), Adults: ptr(r.adults), CheckInDate: ptr(date(ci)), CheckOutDate: ptr(date(co)), Source: ptr("phone"), SpecialRequests: ptr("Lantai tinggi bila memungkinkan")})
		if err != nil {
			return uuid.Nil, fmt.Errorf("reservation %s: %w", r.guest, err)
		}
		act := func(a string, in hotel.ActionInput) (*hotel.ActionResult, error) {
			out, err := s.Hotel.Act(fo, res.ID, a, in)
			if err != nil {
				return nil, fmt.Errorf("reservation %s %s: %w", r.guest, a, err)
			}
			return out, nil
		}
		switch r.stage {
		case "confirmed", "checked_in", "checked_out":
			if _, err := act("confirm", hotel.ActionInput{}); err != nil {
				return uuid.Nil, err
			}
		case "cancelled":
			if _, err := act("cancel", hotel.ActionInput{Reason: "Perubahan rencana perjalanan"}); err != nil {
				return uuid.Nil, err
			}
		}
		if r.stage == "checked_in" || r.stage == "checked_out" {
			in := hotel.ActionInput{RoomLocationID: roomLoc}
			if r.guestApp != "" {
				in.CreateGuestAccount, in.GuestEmail = true, ptr(r.guestApp)
			}
			out, err := act("check_in", in)
			if err != nil {
				return uuid.Nil, err
			}
			if r.guestApp != "" {
				// password deterministik untuk akun Guest App demo
				hash, _ := iam.HashPassword(Password)
				var tenantID uuid.UUID
				_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
					if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, is_active = true WHERE organization_id = $1 AND lower(email) = $3`, env.OrgID, hash, r.guestApp); err != nil {
						return err
					}
					var tid *uuid.UUID
					if err := tx.QueryRow(ctx, `SELECT tu.tenant_id FROM tenant_users tu JOIN users u ON u.id = tu.user_id WHERE lower(u.email) = $1 AND tu.status = 'active'`, r.guestApp).Scan(&tid); err != nil {
						return err
					}
					if tid == nil {
						// akun Guest App belum punya entitas tenant → buat tenant individual (tamu) & tautkan (dipakai booking/visitor/billing)
						if err := tx.QueryRow(ctx, `INSERT INTO tenants (organization_id, property_id, tenant_code, name, tenant_type, contact_name, contact_phone, contact_email, status, created_by) VALUES ($1,$2,'TEN-HOTEL-0001',$3,'individual',$3,$4,$5,'active',$6) RETURNING id`, env.OrgID, p.ID, r.guest, "+62"+r.phone[1:], r.email, env.AdminID).Scan(&tenantID); err != nil {
							return err
						}
						_, err := tx.Exec(ctx, `UPDATE tenant_users tu SET tenant_id = $2 FROM users u WHERE u.id = tu.user_id AND lower(u.email) = $1`, r.guestApp, tenantID)
						return err
					}
					tenantID = *tid
					return nil
				})
				_ = out
				activeStayTenant = &tenantRef{TenantID: tenantID, Name: r.guest, UnitID: *roomLoc, UnitLabel: "Room " + r.room, AppEmail: r.guestApp}
				s.IAM.InvalidateUser(env.Users[r.guestApp])
			}
			_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE hotel_reservations SET checked_in_at = $2 WHERE id = $1`, res.ID, at(ci, 14, 30))
				return err
			})
		}
		if r.stage == "checked_out" {
			if _, err := act("check_out", hotel.ActionInput{IssueInvoice: ptr(true)}); err != nil {
				return uuid.Nil, err
			}
			_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE hotel_reservations SET checked_out_at = $2 WHERE id = $1`, res.ID, at(co, 11, 45))
				return err
			})
		}
		_ = s.backdate(ctx, env, "hotel_reservations", res.ID, ci.AddDate(0, 0, -7), nil)
	}
	s.flush(ctx)
	// kamar hasil check-out lama: turnover selesai → available (kecuali 303 tetap dirty untuk demo housekeeping)
	for _, num := range []string{"502", "402"} {
		for _, to := range []string{"clean", "inspected", "available"} {
			_, _ = s.Hotel.SetRoomStatus(hkSpv, roomIDs[num], hotel.RoomStatusInput{RoomStatus: to})
		}
	}
	logf("hotel: reservasi %d (3 in-house, 2 confirmed, 1 upcoming, 3 checked-out + invoice, 2 cancelled); Guest App hotel.guest@buildingvision.local", len(resDefs))

	// guest tenant untuk skenario (tenant dari akun Guest App)
	if activeStayTenant == nil {
		return uuid.Nil, fmt.Errorf("guest app tenant tidak terbentuk")
	}
	// tenant tambahan: tamu in-house lain sebagai tenant (tanpa akun) untuk booking/visitor
	var secondTenant tenantRef
	_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		var tid uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO tenants (organization_id, property_id, tenant_code, name, tenant_type, contact_name, contact_phone, contact_email, status, created_by) VALUES ($1,$2,'TEN-HOTEL-0002','Maria Chandra','individual','Maria Chandra','+6281211110002','maria.chandra@guest.test','active',$3) RETURNING id`, env.OrgID, p.ID, env.AdminID).Scan(&tid); err != nil {
			return err
		}
		secondTenant = tenantRef{TenantID: tid, Name: "Maria Chandra", UnitID: roomIDs["401"], UnitLabel: "Room 401"}
		return nil
	})
	p.Tenants = []tenantRef{*activeStayTenant, secondTenant}

	// ---- tiket & E2E (§8, §9): E2E-02 housekeeping Room 501 + engineering AC + security + tenant relation ----
	guest := activeStayTenant
	tickets := []srSpec{
		// E2E-02 — Hotel Room 501 Cleaning Request → HK task → checklist → evidence → completed → notifikasi tamu
		{Category: "cleaning", Title: "Room cleaning request — Room 501", Desc: "Mohon dibersihkan sore ini, ada tumpahan kopi di karpet.", Priority: "medium", Location: ptr(roomIDs["501"]), TenantApp: guest.AppEmail, Age: 26 * time.Hour, Stage: "closed", AssignTo: p.Staff["hk"], Rating: 5,
			WO: &woSpec{Kind: "task", Domain: "housekeeping", Type: "cleaning", Title: "Spot cleaning karpet Room 501", Priority: "medium", Assignee: p.Staff["hk"], Stage: "closed", Template: "cleaning_room", Evidence: true, DueIn: 4 * time.Hour}},
		// Engineering — AC kamar tidak dingin (in-house guest) → WO → parts → resolved → konfirmasi tamu → CSAT
		{Category: "maintenance", Title: "AC kamar tidak dingin — Room 501", Desc: "AC hanya mengeluarkan angin, tidak dingin sejak semalam.", Priority: "high", Location: ptr(roomIDs["501"]), TenantApp: guest.AppEmail, Age: 30 * time.Hour, Stage: "closed", AssignTo: p.Staff["tech"], Rating: 4,
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Perbaikan AC Room 501 (ganti filter & isi refrigerant)", Priority: "high", Assignee: p.Staff["tech"], Stage: "closed", Template: "pm_ac", Evidence: true, DueIn: 6 * time.Hour, Parts: []partSpec{{"ac_filter", 1}, {"refrigerant", 0.5}}}},
		{Category: "maintenance", Title: "Lampu kamar mandi mati — Room 401", Desc: "Lampu kamar mandi berkedip lalu mati.", Priority: "medium", Location: ptr(roomIDs["401"]), Tenant: &secondTenant.TenantID, Channel: "phone", Age: 5 * time.Hour, Stage: "in_progress", AssignTo: p.Staff["tech2"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Ganti lampu kamar mandi Room 401", Priority: "medium", Assignee: p.Staff["tech2"], Stage: "in_progress", Evidence: true, DueIn: 4 * time.Hour, Parts: []partSpec{{"led_lamp", 1}}}},
		{Category: "maintenance", Title: "Plumbing leak — wastafel bocor Room 507", Desc: "Air menetes dari bawah wastafel.", Priority: "high", Location: ptr(roomIDs["507"]), Channel: "walk_in", Age: 20 * time.Hour, Stage: "resolved", AssignTo: p.Staff["tech"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Perbaikan kebocoran wastafel Room 507", Priority: "high", Assignee: p.Staff["tech"], Stage: "completed", Evidence: true, DueIn: 3 * time.Hour, Parts: []partSpec{{"pipe_connector", 2}, {"water_valve", 1}}, Vendor: "civil"}},
		{Category: "facility", Title: "Lift issue — lift 2 berhenti di lantai 4", Desc: "Lift 2 sempat berhenti 2 menit.", Priority: "critical", Location: ptr(p.Areas["lift_lobby"]), Channel: "walk_in", Age: 2 * time.Hour, Stage: "assigned", AssignTo: p.Staff["tech"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Inspeksi darurat Lift 2", Priority: "critical", Assignee: p.Staff["tech"], Asset: ptr(p.Assets["lift2"]), Stage: "assigned", DueIn: 1 * time.Hour, Vendor: "civil"}},
		{Category: "maintenance", Title: "Electrical issue — stop kontak Room 302 tidak berfungsi", Desc: "", Priority: "medium", Location: ptr(roomIDs["302"]), Channel: "phone", Age: 3 * 24 * time.Hour, Stage: "closed", AssignTo: p.Staff["tech2"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Perbaikan stop kontak Room 302", Priority: "medium", Assignee: p.Staff["tech2"], Stage: "closed", Evidence: true, DueIn: 8 * time.Hour, Parts: []partSpec{{"cable", 3}}}},
		{Category: "cleaning", Title: "Common area cleaning — lobby lift kotor", Desc: "Bekas hujan, lantai licin.", Priority: "medium", Location: ptr(p.Areas["lift_lobby"]), Channel: "walk_in", Age: 90 * time.Minute, Stage: "assigned", AssignTo: p.Staff["hk2"],
			WO: &woSpec{Kind: "task", Domain: "housekeeping", Type: "cleaning", Title: "Cleaning lobby lift", Priority: "medium", Assignee: p.Staff["hk2"], Stage: "assigned", DueIn: 2 * time.Hour}},
		{Category: "cleaning", Title: "Additional cleaning — tambahan handuk & amenities Room 401", Desc: "", Priority: "low", Location: ptr(roomIDs["401"]), Tenant: &secondTenant.TenantID, Channel: "phone", Age: 40 * time.Minute, Stage: "acknowledged"},
		{Category: "security", Title: "Lost item — dompet tertinggal di restoran", Desc: "Dompet warna cokelat.", Priority: "high", Location: ptr(p.Areas["lobby"]), TenantApp: guest.AppEmail, Age: 8 * time.Hour, Stage: "resolved", AssignTo: p.Staff["sec"],
			WO: &woSpec{Kind: "task", Domain: "security", Type: "general", Title: "Pencarian barang tertinggal (CCTV & lost & found)", Priority: "high", Assignee: p.Staff["sec"], Stage: "completed", Evidence: true, DueIn: 4 * time.Hour}},
		{Category: "security", Title: "Access issue — kunci kamar tidak bisa dibuka", Desc: "Key card Room 507 tidak terbaca.", Priority: "high", Location: ptr(roomIDs["507"]), Channel: "phone", Age: 4 * time.Hour, Stage: "waiting_for_tenant", AssignTo: p.Staff["sec2"], Message: "Kami sudah mengganti key card; mohon konfirmasi apakah sudah bisa digunakan."},
		{Category: "complaint", Title: "Complaint — suara bising renovasi lantai 6", Desc: "Renovasi mulai pukul 07:00, mengganggu istirahat.", Priority: "high", TenantApp: guest.AppEmail, Age: 3 * 24 * time.Hour, Stage: "closed", AssignTo: p.Staff["tr"], Message: "Mohon maaf atas ketidaknyamanan. Jam kerja renovasi kami geser mulai pukul 09:00.", Rating: 3, Reopen: true},
		{Category: "inquiry", Title: "Information request — jam operasional kolam renang", Desc: "", Priority: "low", TenantApp: guest.AppEmail, Age: 6 * time.Hour, Stage: "resolved", AssignTo: p.Staff["tr"], Message: "Kolam renang buka 06:00–21:00 setiap hari."},
		{Category: "other", Title: "Follow-up request — permintaan late check-out", Desc: "Mohon late check-out sampai 15:00.", Priority: "low", TenantApp: guest.AppEmail, Age: 15 * time.Minute, Stage: "new"},
		{Category: "complaint", Title: "Complaint — sarapan habis sebelum jam 09:30", Desc: "", Priority: "medium", Channel: "walk_in", Age: 2 * 24 * time.Hour, Stage: "cancelled"},
		{Category: "maintenance", Title: "AC tidak dingin — Room 402 (overdue)", Desc: "", Priority: "high", Location: ptr(roomIDs["402"]), Channel: "phone", Age: 2 * 24 * time.Hour, Stage: "assigned", AssignTo: p.Staff["tech"],
			WO: &woSpec{Kind: "work_order", Domain: "engineering", Title: "Perbaikan AC Room 402", Priority: "high", Assignee: p.Staff["tech"], Stage: "assigned", DueIn: -12 * time.Hour}},
	}
	for _, t := range tickets {
		if _, err := s.runSR(ctx, env, p.ID, t, &p.StockLoc); err != nil {
			return uuid.Nil, err
		}
	}
	logf("hotel: %d tiket lintas domain & status (E2E-02 housekeeping Room 501, engineering AC + parts usage, security, tenant relation, reopen, CSAT)", len(tickets))

	// ---- fasilitas, visitor, billing, pengumuman ----
	if err := s.seedFacilities(ctx, env, p, []facilitySpec{{"Swimming Pool", "pool", 30, false}, {"Gym", "gym", 15, false}, {"Meeting Room Sudirman", "meeting_room", 12, true}, {"Executive Lounge", "lounge", 20, false}}, p.Tenants, logf); err != nil {
		return uuid.Nil, err
	}
	if err := s.seedVisitors(ctx, env, p, p.Tenants, logf); err != nil {
		return uuid.Nil, err
	}
	if err := s.seedBilling(ctx, env, p, p.Tenants, "other", 450000, logf); err != nil {
		return uuid.Nil, err
	}
	for _, a := range []struct{ title, excerpt, body, imp string }{
		{"Maintenance Notice: Pembersihan kolam renang", "Kolam renang ditutup Rabu 08:00–12:00", "Kolam renang akan ditutup sementara untuk pembersihan rutin. Mohon maaf atas ketidaknyamanannya.", "normal"},
		{"Facility Closure: Executive Lounge", "Lounge ditutup untuk renovasi 2 hari", "Executive Lounge ditutup sementara; layanan sarapan dipindahkan ke restoran lantai 1.", "important"},
		{"Service Update: Late check-out gratis akhir pekan", "Berlaku Sabtu–Minggu sampai 14:00", "Nikmati late check-out gratis hingga pukul 14:00 setiap akhir pekan (tergantung ketersediaan).", "normal"},
		{"Emergency Notice: Simulasi evakuasi kebakaran", "Jumat 10:00 — alarm akan berbunyi", "Akan dilaksanakan simulasi evakuasi. Ikuti arahan petugas keamanan.", "important"},
	} {
		if err := s.announce(ctx, env, p.ID, a.title, a.excerpt, a.body, a.imp, 14*24*time.Hour); err != nil {
			return uuid.Nil, err
		}
	}

	// ---- BVRooms (§21A) ----
	if err := s.seedBVRoomsHotel(ctx, env, p, rtIDs, roomIDs, logf); err != nil {
		return uuid.Nil, err
	}
	return p.ID, nil
}

var _ = operations.ObjTask
