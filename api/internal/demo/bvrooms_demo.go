package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/bvrooms"
	"github.com/buildingvision/api/internal/hotel"
	"github.com/buildingvision/api/internal/platform/storage"
)

// BVRooms customer demo (§21A.2 / §21A.22): identitas deterministik; login lewat OTP mock (dev_code di respons env local/test).
var bvCustomers = []struct{ Key, Email, Name, Phone string }{
	{"c1", "bvrooms.customer@buildingvision.local", "Fransiska Wijaya", "+6281200000101"},
	{"c2", "bvrooms.customer2@buildingvision.local", "Aan Prayitno", "+6281200000102"},
	{"c3", "bvrooms.customer3@buildingvision.local", "Marco Ramones", "+6281200000103"},
	{"c4", "bvrooms.customer4@buildingvision.local", "Dina Kartika", "+6281200000104"},
	{"c5", "bvrooms.customer5@buildingvision.local", "Yoga Saputra", "+6281200000105"},
}

func (s *Service) bvCustomerIDs(ctx context.Context, env *Env) (map[string]uuid.UUID, error) {
	ids := map[string]uuid.UUID{}
	err := s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		for _, c := range bvCustomers {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO bvrooms_customers (organization_id, phone_e164, full_name, email) VALUES ($1,$2,$3,$4)
				ON CONFLICT (organization_id, phone_e164) DO UPDATE SET full_name = EXCLUDED.full_name, email = EXCLUDED.email RETURNING id`, env.OrgID, c.Phone, c.Name, c.Email).Scan(&id); err != nil {
				return err
			}
			ids[c.Key] = id
		}
		return nil
	})
	return ids, err
}

// bvPhoto: foto listing deterministik (file JPEG kecil; diunggah ke storage bila tersedia) — §21A.4.
func (s *Service) bvPhoto(ctx context.Context, env *Env, propertyID uuid.UUID, category string, roomTypeID, unitTypeID *uuid.UUID, caption string, sort int, cover bool) error {
	id := uuid.Must(uuid.NewV7())
	key := storage.ObjectKey(env.OrgID.String(), "bvrooms-photo-demo-"+id.String(), time.Now(), ".jpg")
	if s.Storage != nil {
		_ = s.Storage.Put(context.WithoutCancel(ctx), key, "image/jpeg", bytes.NewReader(demoJPEG), int64(len(demoJPEG)))
	}
	return s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO bvrooms_property_photos (id, organization_id, property_id, category, room_type_id, unit_type_id, storage_key, content_type, size_bytes, status, caption, sort_order, is_cover, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'image/jpeg',$8,'ready',$9,$10,$11,$12)`, id, env.OrgID, propertyID, category, roomTypeID, unitTypeID, key, len(demoJPEG), caption, sort, cover, env.AdminID)
		return err
	})
}

// seedBVRoomsHotel: listing Grand Vision Hotel, foto, add-on, promo, banner, 5 customer, booking seluruh status,
// multi-room, pembayaran manual/verifikasi, pay at property, kedaluwarsa (sweep), review 1–5★, wishlist, notifikasi.
func (s *Service) seedBVRoomsHotel(ctx context.Context, env *Env, p *prop, rtIDs, roomIDs map[string]uuid.UUID, logf func(string, ...any)) error {
	admin := p.admin
	desc := []bvrooms.DescriptionSection{
		{Key: "lokasi", Title: "Lokasi", Body: "Grand Vision Hotel berada di kawasan bisnis Sudirman, 5 menit dari stasiun MRT Senayan dan 20 menit dari pusat perbelanjaan."},
		{Key: "fitur_khusus", Title: "Fitur Khusus", Body: "Executive Lounge, kolam renang rooftop, dan ruang pertemuan berkapasitas 120 orang. Menerima pembayaran kartu debit/kredit."},
		{Key: "fasilitas", Title: "Fasilitas", Body: "Seluruh kamar dilengkapi AC, Wi-Fi cepat, smart TV, dan kamar mandi dengan bathtub. CCTV 24 jam di area umum."},
		{Key: "terdekat", Title: "Terdekat", Body: "Senayan City, Plaza Senayan, GBK, dan aneka kuliner di Jl. Sudirman."},
	}
	banks := json.RawMessage(`[{"bank":"BCA","account_number":"3000108765432","account_name":"PT Grand Vision Hospitality"},{"bank":"Mandiri","account_number":"1230009876543","account_name":"PT Grand Vision Hospitality"},{"bank":"BNI","account_number":"0987654321","account_name":"PT Grand Vision Hospitality"}]`)
	if _, err := s.BVRooms.UpdateListing(admin, p.ID, bvrooms.ListingInput{Listed: ptr(true), Slug: ptr("grand-vision-hotel"), Category: ptr("hotel"), DisplayName: ptr("Grand Vision Hotel"), Tagline: ptr("Menginap Mudah Tanpa Repot"),
		AddressLine: ptr("Jl. Jenderal Sudirman Kav. 52"), District: ptr("Senayan"), City: ptr("Jakarta Selatan"), Lat: ptr(-6.2250), Lng: ptr(106.8090), Phone: ptr("+62215700052"), WhatsApp: ptr("+6281100000052"),
		CheckInTime: ptr("14:00"), CheckOutTime: ptr("12:00"), DescriptionSections: &desc,
		Facilities:         ptr([]string{"wifi", "parking", "ac", "gym", "spa", "restaurant", "coffee_shop", "pool", "tv", "meeting_room", "card_payment", "no_smoking", "elevator", "cctv"}),
		Policies:           ptr([]string{"Tamu harus membawa KTP saat melakukan check in.", "Tidak diperbolehkan membawa senjata tajam.", "Tidak diizinkan membawa pulang properti hotel, seperti televisi, AC, router, modem wifi, bantal, guling, selimut dll."}),
		CancellationPolicy: ptr(cancelPolicyMD), PaymentWindowHours: ptr(5), BankAccounts: &banks, AllowPayAtProperty: ptr(true)}, nil); err != nil {
		return fmt.Errorf("bvrooms listing: %w", err)
	}
	// foto per kategori + foto tipe kamar
	photos := []struct {
		cat, caption string
		cover        bool
	}{{"facade", "Tampak depan Grand Vision Hotel", true}, {"facade", "Tampak malam", false}, {"lobby", "Lobby utama", false}, {"receptionist", "Front Office", false}, {"restaurant", "Restoran Sudirman", false}, {"pool", "Kolam renang rooftop", false}, {"other", "Executive Lounge", false}}
	for i, ph := range photos {
		if err := s.bvPhoto(ctx, env, p.ID, ph.cat, nil, nil, ph.caption, i, ph.cover); err != nil {
			return err
		}
	}
	for i, k := range []string{"standard", "deluxe", "executive", "suite"} {
		id := rtIDs[k]
		if err := s.bvPhoto(ctx, env, p.ID, "room", &id, nil, "Kamar "+k, 10+i, false); err != nil {
			return err
		}
	}
	// add-on (D5): default breakfast & extra bed diaktifkan dengan harga dashboard
	addons, err := s.BVRooms.ListAddons(admin, p.ID)
	if err != nil {
		return err
	}
	addonIDs := map[string]uuid.UUID{}
	for _, a := range addons {
		price := map[string]int64{"breakfast": 85000, "extra_bed": 250000}[a.Kind]
		maxQty := map[string]int{"breakfast": 4, "extra_bed": 3}[a.Kind]
		if _, err := s.BVRooms.UpdateAddon(admin, p.ID, a.ID, bvrooms.AddonInput{Price: ptr(price), MaxQty: ptr(maxQty), IsActive: ptr(true)}); err != nil {
			return err
		}
		addonIDs[a.Kind] = a.ID
	}
	// promo aktif (10%, min 2 malam) + promo kedaluwarsa; 2 banner
	if _, err := s.BVRooms.CreatePromotion(admin, bvrooms.PromotionInput{PropertyID: &p.ID, Name: ptr("Diskon Menginap 2 Malam"), DiscountType: ptr("percent"), DiscountValue: ptr(int64(10)), MinNights: ptr(2), ShowAsBanner: ptr(true)}); err != nil {
		return err
	}
	pastFrom, pastUntil := time.Now().AddDate(0, -2, 0), time.Now().AddDate(0, -1, 0)
	if _, err := s.BVRooms.CreatePromotion(admin, bvrooms.PromotionInput{PropertyID: &p.ID, Name: ptr("Promo Kemerdekaan (berakhir)"), DiscountType: ptr("fixed"), DiscountValue: ptr(int64(170000)), BookFrom: &pastFrom, BookUntil: &pastUntil, ShowAsBanner: ptr(false), IsActive: ptr(false)}); err != nil {
		return err
	}
	if _, err := s.BVRooms.CreateBanner(admin, bvrooms.BannerInput{Title: ptr("Menginap Mudah Tanpa Repot"), Subtitle: ptr("Pesan kamar Grand Vision Hotel langsung dari aplikasi"), CTALabel: ptr("Mulai Sekarang"), DeepLink: ptr("/property/grand-vision-hotel"), SortOrder: ptr(0)}); err != nil {
		return err
	}
	if _, err := s.BVRooms.CreateBanner(admin, bvrooms.BannerInput{PropertyID: &p.ID, Title: ptr("Executive Lounge Access"), Subtitle: ptr("Gratis untuk Executive Room & Suite"), CTALabel: ptr("Lihat Kamar"), DeepLink: ptr("/property/grand-vision-hotel"), SortOrder: ptr(1)}); err != nil {
		return err
	}
	// fully booked (§21A.8): Suite (4 kamar) pada D..D+1 — 3 reservasi Front Office + 1 booking BVRooms
	fo := p.admin
	D := today().AddDate(0, 0, 15)
	for i, g := range []string{"Corporate Block A", "Corporate Block B", "Corporate Block C"} {
		if _, err := s.Hotel.CreateReservation(fo, hotel.ReservationInput{PropertyID: &p.ID, RoomTypeID: ptr(rtIDs["suite"]), GuestName: ptr(g), GuestPhone: ptr(fmt.Sprintf("0812999900%02d", i)), Adults: ptr(2), CheckInDate: ptr(date(D)), CheckOutDate: ptr(date(D.AddDate(0, 0, 2))), Source: ptr("corporate"), Confirm: true}); err != nil {
			return fmt.Errorf("suite block: %w", err)
		}
	}

	// ---- customer & booking ----
	cust, err := s.bvCustomerIDs(ctx, env)
	if err != nil {
		return err
	}
	cctx := func(key string) context.Context {
		for _, c := range bvCustomers {
			if c.Key == key {
				return s.asCustomer(ctx, env, cust[key], c.Name)
			}
		}
		return env.sysCtx
	}
	fin := s.asEmail(ctx, env, "finance.demo@buildingvision.local")
	guest := func(key string) bvrooms.GuestInput {
		for _, c := range bvCustomers {
			if c.Key == key {
				return bvrooms.GuestInput{FullName: c.Name, Email: c.Email, Phone: c.Phone}
			}
		}
		return bvrooms.GuestInput{}
	}
	book := func(key string, ci time.Time, nights int, rooms []bvrooms.CreateRoomInput, payMethod string) (*bvrooms.Booking, error) {
		in := bvrooms.CreateBookingInput{PropertyID: p.ID, CheckIn: date(ci), CheckOut: date(ci.AddDate(0, 0, nights)), Guest: guest(key), Rooms: rooms}
		if payMethod != "" {
			in.Payment = &bvrooms.CreatePaymentInput{MethodCode: payMethod}
		}
		b, err := s.BVRooms.CreateBooking(cctx(key), in)
		if err != nil {
			return nil, fmt.Errorf("bvrooms booking %s: %w", key, err)
		}
		return b, nil
	}
	std := func(adults int, addons ...bvrooms.CreateAddonInput) bvrooms.CreateRoomInput {
		return bvrooms.CreateRoomInput{TypeID: rtIDs["standard"], Adults: adults, Addons: addons}
	}
	// pay manual: pilih transfer → bukti → verifikasi Finance → PAID
	payManual := func(key string, b *bvrooms.Booking, method string, verify bool) error {
		b2, err := s.BVRooms.CreatePayment(cctx(key), b.BookingCode, bvrooms.CreatePaymentReq{ProviderCode: "manual", MethodCode: method})
		if err != nil {
			return fmt.Errorf("payment %s: %w", b.BookingCode, err)
		}
		if err := s.submitProof(ctx, env, key, b2); err != nil {
			return err
		}
		if !verify {
			return nil
		}
		b3, err := s.BVRooms.GetBooking(cctx(key), b.BookingCode)
		if err != nil {
			return err
		}
		if _, err := s.BVRooms.VerifyPayment(fin, b3.Payment.ID, bvrooms.VerifyInput{Note: "Transfer diterima (demo)"}); err != nil {
			return fmt.Errorf("verify %s: %w", b.BookingCode, err)
		}
		return nil
	}
	// backdateBooking: geser tanggal stay & pembuatan ke masa lalu (untuk riwayat) lalu jalankan check-in/out via Front Office
	stayHistory := func(key string, b *bvrooms.Booking, ciOff int, checkout bool, room string) error {
		ci := today().AddDate(0, 0, ciOff)
		co := ci.AddDate(0, 0, b.Nights)
		if err := s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET check_in_date = $2::date, check_out_date = $3::date, created_at = $2::date - interval '6 days' WHERE bvrooms_booking_id = $1`, b.ID, ci, co); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE bvrooms_bookings SET check_in_date = $2::date, check_out_date = $3::date, created_at = $2::date - interval '6 days' WHERE id = $1`, b.ID, ci, co)
			return err
		}); err != nil {
			return fmt.Errorf("backdate booking %s: %w", b.BookingCode, err)
		}
		bk, err := s.BVRooms.GetBooking(cctx(key), b.BookingCode)
		if err != nil {
			return err
		}
		for i, r := range bk.Rooms {
			rl := roomIDs[room]
			if i > 0 || room == "" {
				rl = uuid.Nil
			}
			in := hotel.ActionInput{}
			if rl != uuid.Nil {
				in.RoomLocationID = &rl
			} else {
				// pilih kamar bebas dari tipe yang sama
				var free uuid.UUID
				_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
					return tx.QueryRow(ctx, `SELECT hr.location_id FROM hotel_rooms hr WHERE hr.room_type_id = $1 AND hr.is_active AND hr.room_status = 'available'
						AND NOT EXISTS (SELECT 1 FROM hotel_reservations x WHERE x.room_location_id = hr.location_id AND x.status IN ('new','confirmed','checked_in') AND x.stay && daterange($2::date, $3::date, '[)')) ORDER BY hr.room_number LIMIT 1`, r.TypeID, ci, co).Scan(&free)
				})
				if free == uuid.Nil {
					return fmt.Errorf("tidak ada kamar bebas untuk %s", b.BookingCode)
				}
				in.RoomLocationID = &free
			}
			if _, err := s.Hotel.Act(fo, r.ReservationID, "check_in", in); err != nil {
				return fmt.Errorf("check-in %s: %w", b.BookingCode, err)
			}
			if checkout {
				if _, err := s.Hotel.Act(fo, r.ReservationID, "check_out", hotel.ActionInput{IssueInvoice: ptr(false)}); err != nil {
					return fmt.Errorf("check-out %s: %w", b.BookingCode, err)
				}
			}
		}
		if checkout {
			_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE hotel_reservations SET checked_in_at = $2, checked_out_at = $3 WHERE bvrooms_booking_id = $1`, b.ID, at(ci, 14, 30), at(co, 11, 30))
				return err
			})
		}
		return nil
	}
	review := func(key string, b *bvrooms.Booking, stars int) error {
		_, err := s.BVRooms.CreateReview(cctx(key), b.BookingCode, bvrooms.ReviewInput{Stars: stars})
		if err != nil {
			return fmt.Errorf("review %s: %w", b.BookingCode, err)
		}
		return nil
	}
	breakfast := bvrooms.CreateAddonInput{AddonID: addonIDs["breakfast"], Qty: 2}
	extraBed := bvrooms.CreateAddonInput{AddonID: addonIDs["extra_bed"], Qty: 1}

	// C1 — SC-031 manual payment → PAID (upcoming, dengan breakfast); riwayat CHECK OUT + review 5★; wishlist
	b, err := book("c1", today().AddDate(0, 0, 7), 2, []bvrooms.CreateRoomInput{std(2, breakfast)}, "")
	if err != nil {
		return err
	}
	if err := payManual("c1", b, "transfer_bca", true); err != nil {
		return err
	}
	h1, err := book("c1", today().AddDate(0, 0, 30), 2, []bvrooms.CreateRoomInput{std(2)}, "")
	if err != nil {
		return err
	}
	if err := payManual("c1", h1, "transfer_mandiri", true); err != nil {
		return err
	}
	if err := stayHistory("c1", h1, -12, true, ""); err != nil {
		return err
	}
	if err := review("c1", h1, 5); err != nil {
		return err
	}
	if err := s.BVRooms.AddWishlist(cctx("c1"), p.ID); err != nil {
		return err
	}
	_ = s.BVRooms.MarkRead(cctx("c1"), nil) // notifikasi historis (sudah dibaca) — §21A.19

	// C2 — UNPAID upcoming (transfer dipilih, bukti belum diunggah) + riwayat CHECK OUT review 4★, wishlist kosong
	b2, err := book("c2", today().AddDate(0, 0, 3), 1, []bvrooms.CreateRoomInput{{TypeID: rtIDs["deluxe"], Adults: 2}}, "")
	if err != nil {
		return err
	}
	if _, err := s.BVRooms.CreatePayment(cctx("c2"), b2.BookingCode, bvrooms.CreatePaymentReq{ProviderCode: "manual", MethodCode: "transfer_bni"}); err != nil {
		return err
	}
	h2, err := book("c2", today().AddDate(0, 0, 32), 3, []bvrooms.CreateRoomInput{{TypeID: rtIDs["deluxe"], Adults: 2, Addons: []bvrooms.CreateAddonInput{breakfast}}}, "")
	if err != nil {
		return err
	}
	if err := payManual("c2", h2, "transfer_bca", true); err != nil {
		return err
	}
	if err := stayHistory("c2", h2, -25, true, ""); err != nil {
		return err
	}
	if err := review("c2", h2, 4); err != nil {
		return err
	}

	// C3 — SC-030 multi-room (Standard + Deluxe, breakfast + extra bed) PAID lalu CHECK IN (in-house); riwayat 3★
	b3, err := book("c3", today().AddDate(0, 0, 35), 2, []bvrooms.CreateRoomInput{std(2, breakfast), {TypeID: rtIDs["deluxe"], Adults: 2, Children: 1, Addons: []bvrooms.CreateAddonInput{breakfast, extraBed}}}, "")
	if err != nil {
		return err
	}
	if err := payManual("c3", b3, "transfer_bca", true); err != nil {
		return err
	}
	if err := stayHistory("c3", b3, -1, false, "304"); err != nil {
		return err
	}
	h3, err := book("c3", today().AddDate(0, 0, 40), 1, []bvrooms.CreateRoomInput{std(1)}, "")
	if err != nil {
		return err
	}
	if err := payManual("c3", h3, "transfer_mandiri", true); err != nil {
		return err
	}
	if err := stayHistory("c3", h3, -40, true, ""); err != nil {
		return err
	}
	if err := review("c3", h3, 3); err != nil {
		return err
	}

	// C4 — SC-033 EXPIRED (deadline lewat → sweep), CANCELLED oleh customer, riwayat 2★
	b4, err := book("c4", today().AddDate(0, 0, 10), 2, []bvrooms.CreateRoomInput{std(2, extraBed)}, "")
	if err != nil {
		return err
	}
	_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE bvrooms_bookings SET payment_deadline_at = now() - interval '2 hours', created_at = now() - interval '7 hours' WHERE id = $1`, b4.ID)
		return err
	})
	if _, err := s.BVRooms.Sweep(ctx, env.OrgID); err != nil {
		return fmt.Errorf("bvrooms sweep: %w", err)
	}
	b4c, err := book("c4", today().AddDate(0, 0, 21), 1, []bvrooms.CreateRoomInput{{TypeID: rtIDs["executive"], Adults: 2}}, "")
	if err != nil {
		return err
	}
	if _, err := s.BVRooms.CancelBooking(cctx("c4"), b4c.BookingCode, bvrooms.CancelInput{Reason: "Perubahan jadwal perjalanan"}); err != nil {
		return err
	}
	h4, err := book("c4", today().AddDate(0, 0, 45), 2, []bvrooms.CreateRoomInput{std(2)}, "")
	if err != nil {
		return err
	}
	if err := payManual("c4", h4, "transfer_bca", true); err != nil {
		return err
	}
	if err := stayHistory("c4", h4, -60, true, ""); err != nil {
		return err
	}
	if err := review("c4", h4, 2); err != nil {
		return err
	}

	// C5 — SC-032 pay at property (confirmed, tanpa deadline) untuk Suite di tanggal fully booked D; bukti transfer menunggu verifikasi; riwayat 1★
	b5, err := book("c5", D, 2, []bvrooms.CreateRoomInput{{TypeID: rtIDs["suite"], Adults: 2}}, "cash_on_site")
	if err != nil {
		return err
	}
	if b5.Status != bvrooms.StatusPaid {
		return fmt.Errorf("pay at property harus PAID, dapat %s", b5.Status)
	}
	b5p, err := book("c5", today().AddDate(0, 0, 5), 1, []bvrooms.CreateRoomInput{{TypeID: rtIDs["executive"], Adults: 1, Addons: []bvrooms.CreateAddonInput{{AddonID: addonIDs["breakfast"], Qty: 1}}}}, "")
	if err != nil {
		return err
	}
	if err := payManual("c5", b5p, "transfer_bca", false); err != nil { // proof_submitted, menunggu Finance
		return err
	}
	h5, err := book("c5", today().AddDate(0, 0, 50), 1, []bvrooms.CreateRoomInput{std(1)}, "")
	if err != nil {
		return err
	}
	if err := payManual("c5", h5, "transfer_bni", true); err != nil {
		return err
	}
	if err := stayHistory("c5", h5, -75, true, ""); err != nil {
		return err
	}
	if err := review("c5", h5, 1); err != nil {
		return err
	}
	s.flush(ctx)
	logf("hotel: BVRooms listing 'grand-vision-hotel', 11 foto, 2 add-on, 2 promo, 2 banner, 5 customer, 12 booking (UNPAID/PAID/CHECK IN/CHECK OUT/CANCELLED/EXPIRED, multi-room, pay at property), review 1–5★, wishlist")
	return nil
}

// submitProof: unggah bukti transfer demo; bila storage tidak tersedia, status proof_submitted di-set langsung.
func (s *Service) submitProof(ctx context.Context, env *Env, key string, b *bvrooms.Booking) error {
	var name string
	for _, c := range bvCustomers {
		if c.Key == key {
			name = c.Name
		}
	}
	cust, _ := s.bvCustomerIDs(ctx, env)
	cctx := s.asCustomer(ctx, env, cust[key], name)
	pre, err := s.BVRooms.ProofPresign(cctx, b.BookingCode, bvrooms.ProofPresignInput{ContentType: "image/jpeg", SizeBytes: int64(len(demoJPEG))})
	if err != nil {
		return fmt.Errorf("proof presign %s: %w", b.BookingCode, err)
	}
	var keyPath string
	_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT proof_storage_key FROM bvrooms_payments WHERE id = $1`, pre.PaymentID).Scan(&keyPath)
	})
	if s.Storage != nil && keyPath != "" {
		_ = s.Storage.Put(context.WithoutCancel(ctx), keyPath, "image/jpeg", bytes.NewReader(demoJPEG), int64(len(demoJPEG)))
	}
	if _, err := s.BVRooms.ProofConfirm(cctx, b.BookingCode); err != nil {
		// storage tidak dapat diakses (mis. MinIO lokal mati) → tandai langsung agar alur verifikasi tetap dapat didemokan
		return s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE bvrooms_payments SET status = 'proof_submitted', proof_submitted_at = now() WHERE id = $1 AND status = 'pending'`, pre.PaymentID)
			return err
		})
	}
	return nil
}

const cancelPolicyMD = `**Kebijakan Pembatalan**

- **Sebelum check-in:** pembatalan hingga 24 jam sebelum tanggal check-in dikembalikan sepenuhnya. Kurang dari 24 jam dikenakan biaya satu malam.
- **Di tanggal check-in:** pembatalan setelah waktu check-in atau tidak datang (no-show) dikenakan biaya penuh malam pertama.
- **Saat menginap:** malam yang tersisa tidak dapat dikembalikan.`
