package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/property"
)

// SeedBVRoomsDemo (Requirements v0.2 §9.1): properti hotel & apartemen demo yang tayang di BVRooms —
// tipe kamar/unit + inventori, listing lengkap (rekening transfer, bayar di tempat), add-on (D5), promo & banner (§10),
// serta satu customer demo (+62 812-0000-0001, OTP mock). Dipanggil dari SeedDemoRefs withOps=true.
func SeedBVRoomsDemo(ctx context.Context, tx pgx.Tx, refs *DemoRefs) error {
	orgID, adminID := refs.OrgID, refs.AdminID
	mk := func(lt property.LocationType, parent *uuid.UUID, name string, details map[string]any) (uuid.UUID, error) {
		return createLocationTx(ctx, tx, orgID, adminID, lt, parent, name, details)
	}
	now := time.Now()
	jkt, _ := time.LoadLocation("Asia/Jakarta")

	// ---------- Hotel: Graha Pangeran Hotel ----------
	hotelID, err := mk(property.LTProperty, nil, "Graha Pangeran Hotel", map[string]any{"timezone": "Asia/Jakarta", "address": "Jl. Fatmawati No. 88, Gandaria Utara", "city": "Jakarta Selatan", "property_type": "hotel", "profile": "hotel"})
	if err != nil {
		return err
	}
	hBld, err := mk(property.LTBuilding, &hotelID, "Main Wing", map[string]any{"floors_count": 8.0})
	if err != nil {
		return err
	}
	hFloor, err := mk(property.LTFloor, &hBld, "Floor 5", map[string]any{"floor_number": 5.0})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO teams (organization_id, property_id, name, domain, created_by) VALUES ($1,$2,'Housekeeping Hotel','housekeeping',$3)`, orgID, hotelID, adminID); err != nil {
		return err
	}
	type rtDef struct {
		name, bed   string
		adults      int
		children    int
		size        float64
		rate        int64
		amenities   []string
		roomNums    []string
		description string
	}
	rtDefs := []rtDef{
		{"Single Bed Studio", "single", 1, 0, 24, 1220000, []string{"wifi", "ac", "tv", "bathtub", "single_bed"}, []string{"501", "502"}, "Studio nyaman untuk perjalanan bisnis."},
		{"Double Bed Studio", "queen", 3, 1, 32, 2560000, []string{"wifi", "ac", "tv", "bathtub", "queen_bed", "breakfast"}, []string{"503", "504", "505"}, "Studio luas dengan pemandangan kota."},
		{"Luxury Bed Studio", "king", 3, 2, 45, 3560000, []string{"wifi", "ac", "tv", "bathtub", "king_bed", "breakfast", "mini_fridge"}, []string{"506"}, "Suite premium dengan ruang tamu terpisah."},
	}
	rtIDs := map[string]uuid.UUID{}
	for _, d := range rtDefs {
		code, err := ids.NextYearly(ctx, tx, orgID, ids.PrefixHotelRoomType, now, jkt)
		if err != nil {
			return err
		}
		var rtID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO hotel_room_types (organization_id, property_id, room_type_code, name, description, capacity_adults, capacity_children, bed_type, size_m2, amenities, base_rate, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12) RETURNING id`,
			orgID, hotelID, code, d.name, d.description, d.adults, d.children, d.bed, d.size, d.amenities, d.rate, adminID).Scan(&rtID); err != nil {
			return err
		}
		rtIDs[d.name] = rtID
		for _, num := range d.roomNums {
			locID, err := mk(property.LTUnit, &hFloor, "Room "+num, map[string]any{"unit_number": num, "unit_type": "hotel_room", "area_m2": d.size})
			if err != nil {
				return err
			}
			rcode, err := ids.NextYearly(ctx, tx, orgID, ids.PrefixHotelRoom, now, jkt)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO hotel_rooms (location_id, organization_id, property_id, room_code, room_type_id, room_number, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$7)`, locID, orgID, hotelID, rcode, rtID, num, adminID); err != nil {
				return err
			}
		}
		// rate akhir pekan lebih tinggi (Jumat–Sabtu)
		rateCode, err := ids.NextYearly(ctx, tx, orgID, ids.PrefixHotelRate, now, jkt)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO hotel_rates (organization_id, property_id, rate_code, room_type_id, name, rate_per_night, weekdays, priority, created_by, updated_by) VALUES ($1,$2,$3,$4,'Weekend',$5,'{5,6}',10,$6,$6)`, orgID, hotelID, rateCode, rtID, d.rate*115/100, adminID); err != nil {
			return err
		}
	}
	hotelDesc := `[{"key":"lokasi","title":"Lokasi","body":"Hotel yang terletak di Gandaria Utara, Jakarta Selatan, dekat ITC Fatmawati. Cocok untuk perjalanan bisnis maupun liburan bersama keluarga."},{"key":"fitur_khusus","title":"Fitur Khusus","body":"Desain interior sederhana dan elegan, jendela dengan pencahayaan cukup, genset dan parkir luas. Menerima pembayaran kartu debit/kredit."},{"key":"fasilitas","title":"Fasilitas","body":"Kamar dilengkapi AC, wifi, dan TV. Kamar mandi dalam yang bersih. CCTV 24 jam di area umum."},{"key":"terdekat","title":"Terdekat","body":"Rekomendasi kuliner: Oishi Yatai, Pecel Lele Mas Amir, Waroenk Iga & Steak HW, DeenCo, dan Royal Cafe."}]`
	if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_property_listings (property_id, organization_id, bvrooms_listed, slug, listing_category, display_name, tagline, address_line, district, city, lat, lng, phone, whatsapp, description_sections, facilities, policies, cancellation_policy_md, bank_accounts, allow_pay_at_property, updated_by)
		VALUES ($1,$2,true,'graha-pangeran-hotel','hotel','Graha Pangeran Hotel','Menginap Mudah Tanpa Repot','Jl. Fatmawati No. 88','Gandaria Utara','Jakarta Selatan',-6.2653,106.7925,'+622175000088','+6281100000088',$3::jsonb,
			'{wifi,parking,ac,gym,spa,restaurant,coffee_shop,pool,tv,meeting_room,card_payment,no_smoking}',
			'{"Tamu harus membawa KTP saat melakukan check in.","Tidak diperbolehkan membawa senjata tajam.","Tidak diizinkan membawa pulang properti hotel, seperti televisi, AC, router, modem wifi, bantal, guling, selimut dll."}',
			$4, '[{"bank":"BCA","account_number":"3000108765432","account_name":"PT Graha Pangeran Property"},{"bank":"Mandiri","account_number":"1230009876543","account_name":"PT Graha Pangeran Property"}]'::jsonb, true, $5)`,
		hotelID, orgID, hotelDesc, cancelPolicyMD, adminID); err != nil {
		return err
	}
	for _, a := range []struct {
		kind, name, unit string
		price            int64
		max              int
		rt               string
	}{{"breakfast", "Breakfast", "per_guest_per_night", 25000, 4, ""}, {"extra_bed", "Extra Bed", "per_item_per_night", 150000, 3, ""}} {
		code, err := ids.NextYearly(ctx, tx, orgID, "ADD", now, jkt)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_addons (organization_id, property_id, addon_code, kind, name, pricing_unit, price, max_qty, is_active, sort_order) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,true,0)`, orgID, hotelID, code, a.kind, a.name, a.unit, a.price, a.max); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_promotions (organization_id, property_id, name, discount_type, discount_value, min_nights, show_as_banner, is_active) VALUES ($1,$2,'Pemesanan Pertama Potongan Harga','percent',15,1,true,true)`, orgID, hotelID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_banners (organization_id, title, subtitle, cta_label, deep_link, sort_order) VALUES ($1,'Menginap Mudah Tanpa Repot','Pesan kamar hotel & unit apartemen dalam hitungan menit','Mulai Sekarang','/catalog',0)`, orgID); err != nil {
		return err
	}

	// ---------- Apartemen: Graha Pangeran Residence ----------
	aptID, err := mk(property.LTProperty, nil, "Graha Pangeran Residence", map[string]any{"timezone": "Asia/Jakarta", "address": "Jl. Pangeran No. 3", "city": "Jakarta Selatan", "property_type": "apartment", "profile": "apartment"})
	if err != nil {
		return err
	}
	aBld, err := mk(property.LTBuilding, &aptID, "Residence Building", map[string]any{"floors_count": 20.0})
	if err != nil {
		return err
	}
	aTwr, err := mk(property.LTTower, &aBld, "Tower Emerald", map[string]any{"floors_count": 20.0})
	if err != nil {
		return err
	}
	aFloor, err := mk(property.LTFloor, &aTwr, "Lantai 15", map[string]any{"floor_number": 15.0})
	if err != nil {
		return err
	}
	utCode, err := ids.NextYearly(ctx, tx, orgID, "UT", now, jkt)
	if err != nil {
		return err
	}
	var studioID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO bvrooms_unit_types (organization_id, property_id, unit_type_code, name, description, capacity_adults, capacity_children, bedrooms, size_m2, amenities, base_rate, created_by, updated_by) VALUES ($1,$2,$3,'Studio 36 m²','Unit studio furnished dengan dapur kecil dan balkon.',2,1,1,36,'{wifi,ac,tv,kitchen,washing_machine,balcony,elevator,cctv}',650000,$4,$4) RETURNING id`, orgID, aptID, utCode, adminID).Scan(&studioID); err != nil {
		return err
	}
	for _, num := range []string{"E-1501", "E-1502", "E-1503"} {
		locID, err := mk(property.LTUnit, &aFloor, "Unit "+num, map[string]any{"unit_number": num, "unit_type": "residential", "area_m2": 36.0})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE units SET bvrooms_unit_type_id = $2, rentable_daily = true WHERE location_id = $1`, locID, studioID); err != nil {
			return err
		}
	}
	aptDesc := `[{"key":"lokasi","title":"Lokasi","body":"Apartemen di kawasan Pangeran, Jakarta Selatan; 10 menit ke stasiun MRT."},{"key":"fasilitas","title":"Fasilitas","body":"Kolam renang, gym, minimarket, dan keamanan 24 jam."}]`
	if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_property_listings (property_id, organization_id, bvrooms_listed, slug, listing_category, display_name, tagline, address_line, district, city, lat, lng, phone, description_sections, facilities, policies, cancellation_policy_md, bank_accounts, allow_pay_at_property, check_in_time, updated_by)
		VALUES ($1,$2,true,'graha-pangeran-residence','apartment','Graha Pangeran Residence','Sewa harian unit furnished','Jl. Pangeran No. 3','Kebayoran Baru','Jakarta Selatan',-6.2431,106.8011,'+622175000099',$3::jsonb,
			'{wifi,parking,pool,gym,elevator,cctv,no_smoking}', '{"Penghuni wajib menunjukkan KTP saat check in.","Dilarang membawa hewan peliharaan."}', $4,
			'[{"bank":"BCA","account_number":"3000108765432","account_name":"PT Graha Pangeran Property"}]'::jsonb, false, '15:00', $5)`,
		aptID, orgID, aptDesc, cancelPolicyMD, adminID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE bvrooms_property_listings l SET min_rate_cache = (SELECT min(base_rate) FROM hotel_room_types WHERE property_id = l.property_id AND status = 'active'), min_rate_cached_at = now() WHERE l.property_id = $1`, hotelID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE bvrooms_property_listings l SET min_rate_cache = (SELECT min(base_rate) FROM bvrooms_unit_types WHERE property_id = l.property_id AND status = 'active'), min_rate_cached_at = now() WHERE l.property_id = $1`, aptID); err != nil {
		return err
	}
	// customer demo (login lewat OTP mock; env local mengembalikan dev_code)
	if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_customers (organization_id, phone_e164, full_name, email) VALUES ($1,'+6281200000001','Fransiska Demo','fransiska.demo@customer.test') ON CONFLICT DO NOTHING`, orgID); err != nil {
		return err
	}
	fmt.Printf("seed bvrooms: hotel %s (3 tipe kamar, 6 kamar), residence %s (1 tipe unit, 3 unit), customer demo +6281200000001\n", hotelID, aptID)
	return nil
}

const cancelPolicyMD = `**Pembatalan**

Dengan melakukan pemesanan, Anda menyetujui kebijakan tidak-datang (No-Show) dan pembatalan berikut.

- **Sebelum check-in:** pembatalan hingga 24 jam sebelum tanggal check-in dikembalikan sepenuhnya. Pembatalan kurang dari 24 jam dikenakan biaya satu malam.
- **Di tanggal check-in:** pembatalan setelah waktu check-in atau tidak datang dikenakan biaya penuh malam pertama.
- **Saat menginap:** malam yang tersisa tidak dapat dikembalikan.`
