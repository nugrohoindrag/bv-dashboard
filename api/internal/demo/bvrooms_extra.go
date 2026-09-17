package demo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/bvrooms"
	"github.com/buildingvision/api/internal/hotel"
	"github.com/buildingvision/api/internal/property"
)

type extraRoomType struct {
	name, desc, bed string
	adults, kids    int
	size            float64
	rate            int64
	amen            []string
	rooms           int
	imgs            []string
}

type extraPhoto struct{ cat, caption, img string }

type extraHotel struct {
	name, slug, tagline, address, district, city, phone, wa string
	lat, lng                                                float64
	facilities                                              []string
	desc                                                    []bvrooms.DescriptionSection
	photos                                                  []extraPhoto
	types                                                   []extraRoomType
	promo                                                   string
	promoPct                                                int64
}

// extraHotels: dua hotel tambahan agar katalog BVRooms (Home "Jelajahi", Daftar Property, sort/filter, Terdekat) terisi
// wajar. Struktur ringan: property + 1 lantai + tipe kamar + kamar + listing + foto + add-on + promo — tanpa skenario operasional.
var extraHotels = []extraHotel{
	{
		name: "Vision Boutique Hotel Kemang", slug: "vision-boutique-kemang", tagline: "Butik hotel tenang di jantung Kemang", address: "Jl. Kemang Raya No. 18", district: "Kemang", city: "Jakarta Selatan",
		phone: "+62217190018", wa: "+6281100000018", lat: -6.2607, lng: 106.8130,
		facilities: []string{"wifi", "parking", "ac", "restaurant", "coffee_shop", "pool", "tv", "no_smoking", "elevator", "cctv", "card_payment"},
		desc: []bvrooms.DescriptionSection{
			{Key: "lokasi", Title: "Lokasi", Body: "Di Kemang Raya, 10 menit ke Blok M dan dekat area kuliner & galeri seni Kemang."},
			{Key: "fitur_khusus", Title: "Fitur Khusus", Body: "Desain butik 48 kamar, taman dalam, dan kafe roastery lokal."},
			{Key: "fasilitas", Title: "Fasilitas", Body: "Kolam renang outdoor, restoran, parkir luas, Wi-Fi cepat di seluruh area."},
			{Key: "terdekat", Title: "Terdekat", Body: "Kemang Village, Lippo Mall Kemang, RS Brawijaya."},
		},
		photos: []extraPhoto{{"facade", "Tampak depan", "modern-building"}, {"lobby", "Lobby & lounge", "apartment-lounge"}, {"restaurant", "Kafe roastery", "banquet-hall"}, {"pool", "Kolam renang taman", "hotel-pool"}, {"other", "Ruang meeting", "meeting-room"}},
		types: []extraRoomType{
			{"Superior Room", "Kamar 26 m² bergaya butik dengan jendela besar menghadap taman.", "queen", 2, 1, 26, 780000, []string{"wifi", "ac", "tv", "mini_fridge"}, 4, []string{"hotel-room", "clean-bathroom"}},
			{"Deluxe Garden", "Kamar 34 m² dengan teras privat ke taman dalam.", "king", 2, 1, 34, 1150000, []string{"wifi", "ac", "tv", "mini_fridge", "balcony", "breakfast"}, 3, []string{"hotel-room", "apartment-living"}},
			{"Kemang Suite", "Suite 58 m² dengan ruang tamu, pantry, dan bathtub.", "king", 3, 2, 58, 2450000, []string{"wifi", "ac", "tv", "mini_fridge", "bathtub", "kitchen", "breakfast"}, 2, []string{"hotel-suite", "apartment-kitchen"}},
		},
		promo: "Weekend Getaway Kemang", promoPct: 15,
	},
	{
		name: "Vision Airport Hotel Cengkareng", slug: "vision-airport-cengkareng", tagline: "5 menit dari Bandara Soekarno-Hatta", address: "Jl. Marsekal Suryadarma No. 1", district: "Cengkareng", city: "Tangerang",
		phone: "+62215500001", wa: "+6281100000001", lat: -6.1256, lng: 106.6558,
		facilities: []string{"wifi", "parking", "ac", "restaurant", "tv", "no_smoking", "elevator", "cctv", "card_payment", "gym"},
		desc: []bvrooms.DescriptionSection{
			{Key: "lokasi", Title: "Lokasi", Body: "Hotel transit tepat di kawasan bandara dengan shuttle gratis ke Terminal 1–3 setiap 30 menit."},
			{Key: "fitur_khusus", Title: "Fitur Khusus", Body: "Kamar kedap suara, check-in 24 jam, paket transit 6 jam."},
			{Key: "fasilitas", Title: "Fasilitas", Body: "Restoran 24 jam, gym, business center, parkir inap."},
			{Key: "terdekat", Title: "Terdekat", Body: "Bandara Soekarno-Hatta, Stasiun Kereta Bandara, Mall @ Alam Sutera."},
		},
		photos: []extraPhoto{{"facade", "Tampak depan", "hotel-exterior"}, {"lobby", "Lobby 24 jam", "office-interior"}, {"restaurant", "Restoran 24 jam", "banquet-hall"}, {"other", "Business center", "meeting-room"}},
		types: []extraRoomType{
			{"Transit Room", "Kamar 20 m² kedap suara untuk transit singkat.", "double", 2, 0, 20, 550000, []string{"wifi", "ac", "tv"}, 6, []string{"hotel-room"}},
			{"Family Room", "Kamar 38 m² dengan dua tempat tidur, cocok untuk keluarga.", "twin", 4, 2, 38, 990000, []string{"wifi", "ac", "tv", "mini_fridge", "breakfast"}, 3, []string{"hotel-room", "clean-bathroom"}},
		},
		promo: "Promo Transit Bandara", promoPct: 10,
	},
}

// seedBVRoomsExtraHotels dipanggil setelah seedHotel (profile hotel) — lihat Service.Seed.
func (s *Service) seedBVRoomsExtraHotels(ctx context.Context, env *Env, logf func(string, ...any)) error {
	admin := s.asEmail(ctx, env, AdminEmail)
	banks := json.RawMessage(`[{"bank":"BCA","account_number":"3000108765432","account_name":"PT Grand Vision Hospitality"},{"bank":"Mandiri","account_number":"1230009876543","account_name":"PT Grand Vision Hospitality"}]`)
	for _, h := range extraHotels {
		loc, err := s.Property.CreateLocation(admin, property.CreateLocationInput{LocationType: property.LTProperty, Name: h.name, Details: map[string]any{"timezone": "Asia/Jakarta", "address": h.address, "city": h.city, "profile": ProfileHotel, "property_type": ProfileHotel}})
		if err != nil {
			return fmt.Errorf("extra hotel %s: %w", h.name, err)
		}
		pid := loc.ID
		_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE properties SET settings = jsonb_set(COALESCE(settings,'{}'::jsonb), '{demo_dataset}', to_jsonb($2::text), true) WHERE location_id = $1`, pid, "DEMO-HOTEL-EXTRA-V1")
			return err
		})
		bld, err := s.Property.CreateLocation(admin, property.CreateLocationInput{LocationType: property.LTBuilding, ParentID: &pid, Name: "Main Building", Details: map[string]any{"floors_count": 3.0}})
		if err != nil {
			return err
		}
		floor, err := s.Property.CreateLocation(admin, property.CreateLocationInput{LocationType: property.LTFloor, ParentID: &bld.ID, Name: "Lantai 2", Details: map[string]any{"floor_number": 2.0}})
		if err != nil {
			return err
		}
		roomNo := 201
		typeIDs := make([]uuid.UUID, 0, len(h.types))
		for _, t := range h.types {
			out, err := s.Hotel.CreateRoomType(admin, hotel.RoomTypeInput{PropertyID: &pid, Name: ptr(t.name), Description: ptr(t.desc), CapacityAdults: ptr(t.adults), CapacityChildren: ptr(t.kids), BedType: ptr(t.bed), SizeM2: ptr(t.size), Amenities: ptr(t.amen), BaseRate: ptr(t.rate)})
			if err != nil {
				return fmt.Errorf("room type %s: %w", t.name, err)
			}
			if _, err := s.Hotel.CreateRate(admin, hotel.RateInput{RoomTypeID: &out.ID, Name: ptr("Weekend Rate"), RatePerNight: ptr(t.rate * 115 / 100), Weekdays: ptr([]int{5, 6}), Priority: ptr(10)}); err != nil {
				return err
			}
			for r := 0; r < t.rooms; r++ {
				if _, err := s.Hotel.CreateRoom(admin, hotel.RoomInput{PropertyID: &pid, FloorID: ptr(floor.ID), RoomTypeID: out.ID, RoomNumber: fmt.Sprintf("%d", roomNo)}); err != nil {
					return fmt.Errorf("room %d: %w", roomNo, err)
				}
				roomNo++
			}
			typeIDs = append(typeIDs, out.ID)
		}
		desc := h.desc
		if _, err := s.BVRooms.UpdateListing(admin, pid, bvrooms.ListingInput{Listed: ptr(true), Slug: ptr(h.slug), Category: ptr("hotel"), DisplayName: ptr(h.name), Tagline: ptr(h.tagline),
			AddressLine: ptr(h.address), District: ptr(h.district), City: ptr(h.city), Lat: ptr(h.lat), Lng: ptr(h.lng), Phone: ptr(h.phone), WhatsApp: ptr(h.wa),
			CheckInTime: ptr("14:00"), CheckOutTime: ptr("12:00"), DescriptionSections: &desc, Facilities: ptr(h.facilities),
			Policies:           ptr([]string{"Tamu harus membawa KTP saat melakukan check in.", "Tidak diperbolehkan membawa senjata tajam.", "Dilarang merokok di dalam kamar."}),
			CancellationPolicy: ptr(cancelPolicyMD), PaymentWindowHours: ptr(5), BankAccounts: &banks, AllowPayAtProperty: ptr(true)}, nil); err != nil {
			return fmt.Errorf("listing %s: %w", h.slug, err)
		}
		for i, ph := range h.photos {
			if err := s.bvPhoto(ctx, env, pid, ph.cat, nil, nil, ph.caption, i, i == 0, ph.img); err != nil {
				return err
			}
		}
		for ti, t := range h.types {
			for j, img := range t.imgs {
				id := typeIDs[ti]
				if err := s.bvPhoto(ctx, env, pid, "room", &id, nil, t.name, 20+ti*4+j, false, img); err != nil {
					return err
				}
			}
		}
		addons, err := s.BVRooms.ListAddons(admin, pid)
		if err != nil {
			return err
		}
		for _, a := range addons {
			price := map[string]int64{"breakfast": 65000, "extra_bed": 200000}[a.Kind]
			maxQty := map[string]int{"breakfast": 4, "extra_bed": 2}[a.Kind]
			if _, err := s.BVRooms.UpdateAddon(admin, pid, a.ID, bvrooms.AddonInput{Price: ptr(price), MaxQty: ptr(maxQty), IsActive: ptr(true)}); err != nil {
				return err
			}
		}
		if _, err := s.BVRooms.CreatePromotion(admin, bvrooms.PromotionInput{PropertyID: &pid, Name: ptr(h.promo), DiscountType: ptr("percent"), DiscountValue: ptr(h.promoPct), MinNights: ptr(1), ShowAsBanner: ptr(true)}); err != nil {
			return err
		}
		logf("hotel: BVRooms listing tambahan '%s' (%d tipe kamar, %d foto)", h.slug, len(h.types), len(h.photos))
	}
	return nil
}
