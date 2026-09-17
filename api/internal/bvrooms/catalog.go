package bvrooms

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
)

// ---------- app config (white-label) ----------

type AppConfig struct {
	Organization struct {
		ID           uuid.UUID `json:"id"`
		Slug         string    `json:"slug"`
		Name         string    `json:"name"`
		LogoURL      *string   `json:"logo_url"`
		PrimaryColor *string   `json:"primary_color"`
		WelcomeTitle string    `json:"welcome_title"`
		WelcomeBody  string    `json:"welcome_body"`
	} `json:"organization"`
	Features struct {
		PayAtProperty  bool   `json:"pay_at_property"`
		OnlinePayment  bool   `json:"online_payment"` // D2: gateway ON HOLD → false
		WebPush        bool   `json:"web_push"`
		VAPIDPublicKey string `json:"vapid_public_key,omitempty"`
		OTPProvider    string `json:"otp_provider"`
		AuthMethod     string `json:"auth_method"` // pin | otp (vendor SMS di-hold → pin)
	} `json:"features"`
	ListedCount     int     `json:"listed_count"`
	SingleProperty  *string `json:"single_property_slug"`
	DefaultCategory string  `json:"default_category"`
}

// AppConfig: branding org (organizations.settings.bvrooms) + fitur aktif; single_property_slug bila hanya satu properti listed.
func (s *Service) AppConfig(ctx context.Context, slug string) (*AppConfig, error) {
	orgID, name, err := s.ResolveOrg(ctx, slug)
	if err != nil {
		return nil, err
	}
	out := &AppConfig{}
	out.Organization.ID, out.Organization.Slug, out.Organization.Name = orgID, strings.ToLower(strings.TrimSpace(slug)), name
	out.Organization.WelcomeTitle = "Selamat datang di " + name + "!"
	out.Organization.WelcomeBody = "Pesan kamar dengan harga terpercaya, mudah, dan pelayanan terbaik."
	out.Features.OnlinePayment = false
	out.Features.OTPProvider = s.SMS.Code()
	out.Features.AuthMethod = s.Cfg.AuthMethod
	out.Features.WebPush = s.Pusher != nil && s.Cfg.VAPIDPublicKey != ""
	out.Features.VAPIDPublicKey = s.Cfg.VAPIDPublicKey
	out.DefaultCategory = "all"
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var settings []byte
		_ = tx.QueryRow(ctx, `SELECT settings FROM organizations WHERE id = $1`, orgID).Scan(&settings)
		var st struct {
			BVRooms struct {
				LogoURL      *string `json:"logo_url"`
				PrimaryColor *string `json:"primary_color"`
				WelcomeTitle string  `json:"welcome_title"`
				WelcomeBody  string  `json:"welcome_body"`
			} `json:"bvrooms"`
		}
		_ = json.Unmarshal(settings, &st)
		out.Organization.LogoURL, out.Organization.PrimaryColor = st.BVRooms.LogoURL, st.BVRooms.PrimaryColor
		if st.BVRooms.WelcomeTitle != "" {
			out.Organization.WelcomeTitle = st.BVRooms.WelcomeTitle
		}
		if st.BVRooms.WelcomeBody != "" {
			out.Organization.WelcomeBody = st.BVRooms.WelcomeBody
		}
		var single *string
		var hotels, apts int
		if err := tx.QueryRow(ctx, `SELECT count(*), bool_or(allow_pay_at_property), min(slug) FILTER (WHERE true), count(*) FILTER (WHERE listing_category = 'hotel'), count(*) FILTER (WHERE listing_category = 'apartment')
			FROM bvrooms_property_listings l JOIN properties p ON p.location_id = l.property_id WHERE l.bvrooms_listed AND p.status = 'active'`).Scan(&out.ListedCount, &out.Features.PayAtProperty, &single, &hotels, &apts); err != nil {
			return err
		}
		if out.ListedCount == 1 {
			out.SingleProperty = single
		}
		if hotels > 0 && apts == 0 {
			out.DefaultCategory = "hotel"
		} else if apts > 0 && hotels == 0 {
			out.DefaultCategory = "apartment"
		}
		return nil
	})
	return out, err
}

// ---------- daftar properti ----------

type CatalogFilter struct {
	Q        string
	Category string // all | hotel | apartment
	Lat, Lng *float64
	CheckIn  *time.Time
	CheckOut *time.Time
	Rooms    int
	Adults   int
	Children int
	Sort     string // popular | price_asc | distance
	Cursor   string
	Limit    int
}

type PropertyCard struct {
	ID            uuid.UUID `json:"id"`
	Slug          string    `json:"slug"`
	DisplayName   string    `json:"display_name"`
	Category      string    `json:"listing_category"`
	City          *string   `json:"city"`
	AddressShort  string    `json:"address_short"`
	CoverPhotoURL *string   `json:"cover_photo_url"`
	RatingAvg     float64   `json:"rating_avg"`
	RatingCount   int       `json:"rating_count"`
	RatingLabel   string    `json:"rating_label"`
	MinRate       *int64    `json:"min_rate"`
	HasPromo      bool      `json:"has_promo"`
	DistanceKm    *float64  `json:"distance_km,omitempty"`
	IsWishlisted  *bool     `json:"is_wishlisted,omitempty"`
	Lat           *float64  `json:"lat"`
	Lng           *float64  `json:"lng"`
	popularity    float64
}

// ListProperties: katalog publik org (hanya bvrooms_listed). N properti per org kecil → filter/sort di memori.
func (s *Service) ListProperties(ctx context.Context, f CatalogFilter) ([]PropertyCard, *string, error) {
	orgID := mustOrg(ctx)
	if f.Category == "" {
		f.Category = "all"
	}
	if f.Category != "all" && f.Category != "hotel" && f.Category != "apartment" {
		return nil, nil, apperr.Validation("category harus all|hotel|apartment")
	}
	if f.Sort == "" {
		f.Sort = "popular"
	}
	if f.Sort == "distance" && (f.Lat == nil || f.Lng == nil) {
		return nil, nil, apperr.Validation("sort=distance membutuhkan lat & lng")
	}
	if f.Limit <= 0 {
		f.Limit = 25
	}
	if (f.CheckIn == nil) != (f.CheckOut == nil) {
		return nil, nil, apperr.Validation("check_in dan check_out harus diberikan bersama")
	}
	if f.CheckIn != nil && !f.CheckOut.After(*f.CheckIn) {
		return nil, nil, apperr.Validation("check_out harus setelah check_in")
	}
	var customerID *uuid.UUID
	if p, ok := authctx.From(ctx); ok && p.Source == authctx.SourceBVRooms {
		customerID = &p.UserID
	}
	q := "%" + strings.ToLower(strings.TrimSpace(f.Q)) + "%"
	out := []PropertyCard{}
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT l.property_id, l.slug, l.display_name, l.listing_category, l.city, COALESCE(l.district,''), COALESCE(l.address_line,''), l.lat, l.lng, l.rating_avg, l.rating_count, l.min_rate_cache, l.popularity_score,
			(SELECT ph.storage_key FROM bvrooms_property_photos ph WHERE ph.property_id = l.property_id AND ph.status = 'ready' ORDER BY ph.is_cover DESC, ph.sort_order, ph.created_at LIMIT 1),
			EXISTS (SELECT 1 FROM bvrooms_promotions pr WHERE pr.is_active AND (pr.property_id IS NULL OR pr.property_id = l.property_id) AND (pr.book_from IS NULL OR pr.book_from <= now()) AND (pr.book_until IS NULL OR pr.book_until >= now())),
			($3::uuid IS NOT NULL AND EXISTS (SELECT 1 FROM bvrooms_wishlists w WHERE w.customer_id = $3 AND w.property_id = l.property_id))
			FROM bvrooms_property_listings l JOIN properties p ON p.location_id = l.property_id JOIN locations loc ON loc.id = l.property_id
			WHERE l.bvrooms_listed AND p.status = 'active' AND loc.deleted_at IS NULL
			  AND ($1 = 'all' OR l.listing_category = $1)
			  AND ($2 = '%%' OR lower(l.display_name) LIKE $2 OR lower(COALESCE(l.city,'')) LIKE $2 OR lower(COALESCE(l.district,'')) LIKE $2 OR lower(COALESCE(l.address_line,'')) LIKE $2)`,
			f.Category, q, customerID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c PropertyCard
			var district, address string
			var coverKey *string
			var wish bool
			if err := rows.Scan(&c.ID, &c.Slug, &c.DisplayName, &c.Category, &c.City, &district, &address, &c.Lat, &c.Lng, &c.RatingAvg, &c.RatingCount, &c.MinRate, &c.popularity, &coverKey, &c.HasPromo, &wish); err != nil {
				return err
			}
			c.AddressShort = strings.Trim(strings.Join(nonEmpty(district, deref(c.City)), ", "), ", ")
			if c.AddressShort == "" {
				c.AddressShort = address
			}
			c.RatingLabel = ratingLabel(c.RatingAvg, c.RatingCount)
			c.CoverPhotoURL = s.photoURL(ctx, coverKey)
			if customerID != nil {
				w := wish
				c.IsWishlisted = &w
			}
			if f.Lat != nil && f.Lng != nil && c.Lat != nil && c.Lng != nil {
				d := round1(haversineKm(*f.Lat, *f.Lng, *c.Lat, *c.Lng))
				c.DistanceKm = &d
			}
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		// harga mulai dari untuk tanggal & pax tertentu (hanya tipe yang tersedia)
		if f.CheckIn != nil {
			for i := range out {
				min, err := s.minRateForStayTx(ctx, tx, out[i].ID, out[i].Category, *f.CheckIn, *f.CheckOut, f.Rooms, f.Adults, f.Children)
				if err != nil {
					return err
				}
				out[i].MinRate = min
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch f.Sort {
		case "price_asc":
			if (a.MinRate == nil) != (b.MinRate == nil) {
				return a.MinRate != nil
			}
			if a.MinRate != nil && *a.MinRate != *b.MinRate {
				return *a.MinRate < *b.MinRate
			}
		case "distance":
			if (a.DistanceKm == nil) != (b.DistanceKm == nil) {
				return a.DistanceKm != nil
			}
			if a.DistanceKm != nil && *a.DistanceKm != *b.DistanceKm {
				return *a.DistanceKm < *b.DistanceKm
			}
		}
		if a.popularity != b.popularity {
			return a.popularity > b.popularity
		}
		if a.RatingAvg != b.RatingAvg {
			return a.RatingAvg > b.RatingAvg
		}
		return a.DisplayName < b.DisplayName
	})
	start := 0
	if f.Cursor != "" {
		n, err := strconv.Atoi(f.Cursor)
		if err != nil || n < 0 {
			return nil, nil, apperr.Validation("cursor tidak valid")
		}
		start = n
	}
	if start > len(out) {
		start = len(out)
	}
	end := start + f.Limit
	var next *string
	if end < len(out) {
		v := strconv.Itoa(end)
		next = &v
	} else {
		end = len(out)
	}
	return out[start:end], next, nil
}

func nonEmpty(xs ...string) []string {
	var out []string
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}

// ---------- detail properti ----------

type DescriptionSection struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

type RatingSummary struct {
	Avg   float64 `json:"avg"`
	Count int     `json:"count"`
	Label string  `json:"label"`
	Hist  []int   `json:"hist"` // index 0 = 1★ … 4 = 5★
}

type PhotoItem struct {
	ID       uuid.UUID `json:"id"`
	URL      string    `json:"url"`
	Category string    `json:"category"`
	TypeName *string   `json:"type_name"`
	Caption  *string   `json:"caption"`
	IsCover  bool      `json:"is_cover"`
}

type PhotoCategory struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

var photoLabels = map[string]string{"facade": "Facade", "room": "Room", "receptionist": "Receptionist", "lobby": "Lobby", "restaurant": "Restaurant", "pool": "Swimming Pool", "other": "Lainnya"}

type PaymentOption struct {
	ProviderCode string  `json:"provider_code"`
	MethodCode   string  `json:"method_code"`
	Label        string  `json:"label"`
	Group        string  `json:"group"` // transfer | on_site | virtual_account
	LogoURL      *string `json:"logo_url"`
	Enabled      bool    `json:"enabled"`
	ComingSoon   bool    `json:"coming_soon"`
}

type PropertyDetail struct {
	PropertyCard
	Tagline             *string              `json:"tagline"`
	Terminology         map[string]string    `json:"terminology"`
	AddressLine         *string              `json:"address_line"`
	District            *string              `json:"district"`
	Phone               *string              `json:"phone"`
	WhatsApp            *string              `json:"whatsapp"`
	CheckInTime         string               `json:"check_in_time"`
	CheckOutTime        string               `json:"check_out_time"`
	Timezone            string               `json:"timezone"`
	DescriptionSections []DescriptionSection `json:"description_sections"`
	Facilities          []string             `json:"facilities"`
	Policies            []string             `json:"policies"`
	CancellationPolicy  *string              `json:"cancellation_policy_md"`
	CancellationRules   json.RawMessage      `json:"cancellation_rules"`
	PaymentWindowHours  int                  `json:"payment_window_hours"`
	PhotoCount          int                  `json:"photo_count"`
	PhotosPreview       []PhotoItem          `json:"photos_preview"`
	RatingSummary       RatingSummary        `json:"rating_summary"`
	PaymentOptions      []PaymentOption      `json:"payment_options"`
	RoomTypesPreview    []TypeOffer          `json:"room_types_preview"`
}

func (s *Service) GetProperty(ctx context.Context, slug string, lat, lng *float64) (*PropertyDetail, error) {
	orgID := mustOrg(ctx)
	var customerID *uuid.UUID
	if p, ok := authctx.From(ctx); ok && p.Source == authctx.SourceBVRooms {
		customerID = &p.UserID
	}
	var out *PropertyDetail
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		l, err := s.listingBySlugTx(ctx, tx, slug, true)
		if err != nil {
			return err
		}
		d := &PropertyDetail{}
		d.PropertyCard = s.cardFromListingTx(ctx, tx, l, customerID, lat, lng)
		d.Tagline, d.AddressLine, d.District, d.Phone, d.WhatsApp = l.Tagline, l.AddressLine, l.District, l.Phone, l.WhatsApp
		d.Terminology = l.terminology()
		d.CheckInTime, d.CheckOutTime, d.Timezone = l.CheckInTime, l.CheckOutTime, l.Timezone
		d.DescriptionSections = []DescriptionSection{}
		_ = json.Unmarshal(l.Description, &d.DescriptionSections)
		d.Facilities, d.Policies = l.Facilities, l.Policies
		if d.Facilities == nil {
			d.Facilities = []string{}
		}
		if d.Policies == nil {
			d.Policies = []string{}
		}
		d.CancellationPolicy = l.CancellationMD
		d.CancellationRules = json.RawMessage(l.CancellationRules)
		d.PaymentWindowHours = l.PaymentWindowH
		d.RatingSummary = ratingSummary(l)
		photos, cats, err := s.photosTx(ctx, tx, l.PropertyID, "")
		if err != nil {
			return err
		}
		for _, c := range cats {
			d.PhotoCount += c.Count
		}
		if len(photos) > 4 {
			photos = photos[:4]
		}
		d.PhotosPreview = photos
		d.PaymentOptions = paymentOptions(l)
		d.RoomTypesPreview, err = s.typeOffersTx(ctx, tx, l, nil, nil, 1, 1, 0)
		if err != nil {
			return err
		}
		out = d
		return nil
	})
	return out, err
}

func ratingSummary(l *listingRow) RatingSummary {
	hist := make([]int, 5)
	for i := 0; i < 5 && i < len(l.RatingHist); i++ {
		hist[i] = int(l.RatingHist[i])
	}
	return RatingSummary{Avg: l.RatingAvg, Count: l.RatingCount, Label: ratingLabel(l.RatingAvg, l.RatingCount), Hist: hist}
}

func (s *Service) cardFromListingTx(ctx context.Context, tx pgx.Tx, l *listingRow, customerID *uuid.UUID, lat, lng *float64) PropertyCard {
	c := PropertyCard{ID: l.PropertyID, Slug: l.Slug, DisplayName: l.DisplayName, Category: l.Category, City: l.City, RatingAvg: l.RatingAvg, RatingCount: l.RatingCount, MinRate: l.MinRateCache, Lat: l.Lat, Lng: l.Lng, popularity: l.Popularity}
	c.RatingLabel = ratingLabel(l.RatingAvg, l.RatingCount)
	c.AddressShort = strings.Join(nonEmpty(deref(l.District), deref(l.City)), ", ")
	if c.AddressShort == "" {
		c.AddressShort = deref(l.AddressLine)
	}
	var coverKey *string
	_ = tx.QueryRow(ctx, `SELECT storage_key FROM bvrooms_property_photos WHERE property_id = $1 AND status = 'ready' ORDER BY is_cover DESC, sort_order, created_at LIMIT 1`, l.PropertyID).Scan(&coverKey)
	c.CoverPhotoURL = s.photoURL(ctx, coverKey)
	_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bvrooms_promotions pr WHERE pr.is_active AND (pr.property_id IS NULL OR pr.property_id = $1) AND (pr.book_from IS NULL OR pr.book_from <= now()) AND (pr.book_until IS NULL OR pr.book_until >= now()))`, l.PropertyID).Scan(&c.HasPromo)
	if customerID != nil {
		var w bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bvrooms_wishlists WHERE customer_id = $1 AND property_id = $2)`, *customerID, l.PropertyID).Scan(&w)
		c.IsWishlisted = &w
	}
	if lat != nil && lng != nil && l.Lat != nil && l.Lng != nil {
		d := round1(haversineKm(*lat, *lng, *l.Lat, *l.Lng))
		c.DistanceKm = &d
	}
	return c
}

// paymentOptions (D2): manual transfer dari rekening listing, pay_at_property bila diizinkan, VA hanya mockup (disabled).
func paymentOptions(l *listingRow) []PaymentOption {
	out := []PaymentOption{}
	var accounts []struct {
		Bank          string `json:"bank"`
		AccountNumber string `json:"account_number"`
		AccountName   string `json:"account_name"`
	}
	_ = json.Unmarshal(l.BankAccounts, &accounts)
	for _, a := range accounts {
		code := "transfer_" + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(a.Bank), " ", "_"))
		out = append(out, PaymentOption{ProviderCode: "manual", MethodCode: code, Label: "Transfer " + strings.TrimSpace(a.Bank), Group: "transfer", Enabled: true})
	}
	if l.AllowPayAtProp {
		out = append(out, PaymentOption{ProviderCode: "manual", MethodCode: "cash_on_site", Label: "Bayar di Tempat", Group: "on_site", Enabled: true})
	}
	for _, va := range []struct{ code, label string }{{"bca_va", "BCA Virtual Account"}, {"mandiri_va", "Mandiri Virtual Account"}, {"bni_va", "BNI Virtual Account"}, {"briva", "BRIVA"}} {
		out = append(out, PaymentOption{ProviderCode: "gateway", MethodCode: va.code, Label: va.label, Group: "virtual_account", Enabled: false, ComingSoon: true})
	}
	return out
}

// ---------- foto ----------

func (s *Service) photosTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, category string) ([]PhotoItem, []PhotoCategory, error) {
	rows, err := tx.Query(ctx, `SELECT ph.id, ph.storage_key, ph.category, COALESCE(rt.name, ut.name), ph.caption, ph.is_cover FROM bvrooms_property_photos ph
		LEFT JOIN hotel_room_types rt ON rt.id = ph.room_type_id LEFT JOIN bvrooms_unit_types ut ON ut.id = ph.unit_type_id
		WHERE ph.property_id = $1 AND ph.status = 'ready' ORDER BY ph.is_cover DESC, ph.category, ph.sort_order, ph.created_at`, propertyID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := []PhotoItem{}
	counts := map[string]int{}
	for rows.Next() {
		var it PhotoItem
		var key string
		if err := rows.Scan(&it.ID, &key, &it.Category, &it.TypeName, &it.Caption, &it.IsCover); err != nil {
			return nil, nil, err
		}
		counts[it.Category]++
		if category != "" && it.Category != category {
			continue
		}
		if u := s.photoURL(ctx, &key); u != nil {
			it.URL = *u
		}
		items = append(items, it)
	}
	cats := []PhotoCategory{}
	for _, k := range []string{"facade", "room", "receptionist", "lobby", "restaurant", "pool", "other"} {
		if counts[k] > 0 {
			cats = append(cats, PhotoCategory{Key: k, Label: photoLabels[k], Count: counts[k]})
		}
	}
	return items, cats, rows.Err()
}

type PhotoGallery struct {
	Categories []PhotoCategory `json:"categories"`
	Items      []PhotoItem     `json:"items"`
}

func (s *Service) Photos(ctx context.Context, slug, category string) (*PhotoGallery, error) {
	orgID := mustOrg(ctx)
	var out *PhotoGallery
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		l, err := s.listingBySlugTx(ctx, tx, slug, true)
		if err != nil {
			return err
		}
		items, cats, err := s.photosTx(ctx, tx, l.PropertyID, category)
		if err != nil {
			return err
		}
		out = &PhotoGallery{Categories: cats, Items: items}
		return nil
	})
	return out, err
}

// ---------- tipe kamar / unit: availability, harga, add-on, promo ----------

type AddonOffer struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	PricingUnit string    `json:"pricing_unit"`
	Price       int64     `json:"price"`
	MaxQty      int       `json:"max_qty"`
}

type PromoInfo struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	DiscountType   string    `json:"discount_type"`
	DiscountValue  int64     `json:"discount_value"`
	DiscountAmount int64     `json:"discount_amount"`
}

type TypeOffer struct {
	ID             uuid.UUID    `json:"id"`
	Kind           string       `json:"kind"` // room_type | unit_type
	Name           string       `json:"name"`
	Description    *string      `json:"description"`
	SizeM2         *float64     `json:"size_m2"`
	Bedrooms       *int         `json:"bedrooms,omitempty"`
	MaxAdults      int          `json:"max_adults"`
	MaxChildren    int          `json:"max_children"`
	BedType        *string      `json:"bed_type"`
	Amenities      []string     `json:"amenities"`
	PhotoURL       *string      `json:"photo_url"`
	RatePerNight   int64        `json:"rate_per_night"`
	Nights         int          `json:"nights"`
	LineTotal      int64        `json:"line_total"` // setelah promo
	Promo          *PromoInfo   `json:"promo"`
	TotalUnits     int          `json:"total_units"`
	AvailableCount int          `json:"available_count"`
	IsAvailable    bool         `json:"is_available"`
	Addons         []AddonOffer `json:"addons"`
	baseRate       int64
	currency       string
}

// typeRow: representasi umum hotel_room_types / bvrooms_unit_types.
type typeRow struct {
	ID          uuid.UUID
	Kind        string
	Name        string
	Description *string
	CapAdults   int
	CapChildren int
	BedType     *string
	SizeM2      *float64
	Bedrooms    *int
	Amenities   []string
	BaseRate    int64
	Currency    string
	Status      string
}

func (s *Service) typesTx(ctx context.Context, tx pgx.Tx, l *listingRow, onlyID *uuid.UUID) ([]typeRow, error) {
	var rows pgx.Rows
	var err error
	if l.Category == "apartment" {
		rows, err = tx.Query(ctx, `SELECT id, 'unit_type', name, description, capacity_adults, capacity_children, NULL::text, size_m2, bedrooms, amenities, base_rate, currency_code, status FROM bvrooms_unit_types WHERE property_id = $1 AND status = 'active' AND ($2::uuid IS NULL OR id = $2) ORDER BY base_rate, name`, l.PropertyID, onlyID)
	} else {
		rows, err = tx.Query(ctx, `SELECT id, 'room_type', name, description, capacity_adults, capacity_children, bed_type, size_m2, NULL::int, amenities, base_rate, currency_code, status FROM hotel_room_types WHERE property_id = $1 AND status = 'active' AND ($2::uuid IS NULL OR id = $2) ORDER BY base_rate, name`, l.PropertyID, onlyID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []typeRow
	for rows.Next() {
		var t typeRow
		if err := rows.Scan(&t.ID, &t.Kind, &t.Name, &t.Description, &t.CapAdults, &t.CapChildren, &t.BedType, &t.SizeM2, &t.Bedrooms, &t.Amenities, &t.BaseRate, &t.Currency, &t.Status); err != nil {
			return nil, err
		}
		t.Currency = strings.TrimSpace(t.Currency)
		if t.Amenities == nil {
			t.Amenities = []string{}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// availabilityTx: total unit aktif − maksimum reservasi aktif yang overlap per malam (setara hotel.availabilityTx, dua kind).
func (s *Service) availabilityTx(ctx context.Context, tx pgx.Tx, t *typeRow, checkIn, checkOut time.Time) (total, available int, err error) {
	if t.Kind == "unit_type" {
		err = tx.QueryRow(ctx, `SELECT count(*) FROM units u JOIN locations l ON l.id = u.location_id WHERE u.bvrooms_unit_type_id = $1 AND u.rentable_daily AND l.is_active AND l.deleted_at IS NULL`, t.ID).Scan(&total)
	} else {
		err = tx.QueryRow(ctx, `SELECT count(*) FROM hotel_rooms WHERE room_type_id = $1 AND is_active AND room_status NOT IN ('out_of_order','out_of_service')`, t.ID).Scan(&total)
	}
	if err != nil {
		return 0, 0, err
	}
	col := "room_type_id"
	if t.Kind == "unit_type" {
		col = "unit_type_id"
	}
	var maxBooked int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(c),0) FROM (
		SELECT d::date AS night, count(r.id) AS c FROM generate_series($2::date, ($3::date - 1), interval '1 day') d
		LEFT JOIN hotel_reservations r ON r.`+col+` = $1 AND r.status IN ('new','confirmed','checked_in') AND r.stay @> d::date
		GROUP BY d) x`, t.ID, checkIn, checkOut).Scan(&maxBooked); err != nil {
		return 0, 0, err
	}
	available = total - maxBooked
	if available < 0 {
		available = 0
	}
	return total, available, nil
}

// resolveRateTx: rate aktif paling spesifik per malam (hotel_rates), fallback base_rate — logika sama dengan modul hotel.
func (s *Service) resolveRateTx(ctx context.Context, tx pgx.Tx, t *typeRow, checkIn, checkOut time.Time) (firstRate *uuid.UUID, firstAmt, total int64, err error) {
	col := "room_type_id"
	if t.Kind == "unit_type" {
		col = "unit_type_id"
	}
	rows, err := tx.Query(ctx, `SELECT id, rate_per_night, valid_from, valid_until, weekdays, min_nights FROM hotel_rates WHERE `+col+` = $1 AND status = 'active' ORDER BY priority DESC, created_at`, t.ID)
	if err != nil {
		return nil, 0, 0, err
	}
	type rate struct {
		id     uuid.UUID
		amount int64
		vf, vu *time.Time
		wd     []int32
		minN   int
	}
	var rates []rate
	for rows.Next() {
		var r rate
		if err := rows.Scan(&r.id, &r.amount, &r.vf, &r.vu, &r.wd, &r.minN); err != nil {
			rows.Close()
			return nil, 0, 0, err
		}
		rates = append(rates, r)
	}
	rows.Close()
	nights := int(checkOut.Sub(checkIn).Hours() / 24)
	for i := 0; i < nights; i++ {
		day := checkIn.AddDate(0, 0, i)
		amt := t.BaseRate
		var rid *uuid.UUID
		for _, r := range rates {
			if r.minN > nights || (r.vf != nil && day.Before(*r.vf)) || (r.vu != nil && day.After(*r.vu)) {
				continue
			}
			okDay := false
			for _, w := range r.wd {
				if int(w) == int(day.Weekday()) {
					okDay = true
				}
			}
			if !okDay {
				continue
			}
			id := r.id
			amt, rid = r.amount, &id
			break
		}
		if i == 0 {
			firstRate, firstAmt = rid, amt
		}
		total += amt
	}
	return firstRate, firstAmt, total, nil
}

// bestPromoTx: promo aktif terbaik (diskon terbesar) untuk tipe & stay (§10: tanpa kode).
func (s *Service) bestPromoTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, t *typeRow, checkIn, checkOut time.Time, lineTotal int64) (*PromoInfo, error) {
	nights := int(checkOut.Sub(checkIn).Hours() / 24)
	col := "room_type_id"
	if t.Kind == "unit_type" {
		col = "unit_type_id"
	}
	rows, err := tx.Query(ctx, `SELECT id, name, discount_type, discount_value FROM bvrooms_promotions
		WHERE is_active AND (property_id IS NULL OR property_id = $1) AND (`+col+` IS NULL OR `+col+` = $2) AND (room_type_id IS NULL OR $5::text = 'room_type_id') AND (unit_type_id IS NULL OR $5::text = 'unit_type_id')
		  AND min_nights <= $3 AND (stay_from IS NULL OR stay_from <= $4::date) AND (stay_until IS NULL OR stay_until >= $4::date)
		  AND (book_from IS NULL OR book_from <= now()) AND (book_until IS NULL OR book_until >= now())`, propertyID, t.ID, nights, checkIn, col)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var best *PromoInfo
	for rows.Next() {
		var p PromoInfo
		if err := rows.Scan(&p.ID, &p.Name, &p.DiscountType, &p.DiscountValue); err != nil {
			return nil, err
		}
		if p.DiscountType == "percent" {
			p.DiscountAmount = lineTotal * p.DiscountValue / 100
		} else {
			p.DiscountAmount = p.DiscountValue
		}
		if p.DiscountAmount > lineTotal {
			p.DiscountAmount = lineTotal
		}
		if best == nil || p.DiscountAmount > best.DiscountAmount {
			pp := p
			best = &pp
		}
	}
	return best, rows.Err()
}

func (s *Service) addonsForTypeTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, t *typeRow) ([]AddonOffer, error) {
	col := "room_type_id"
	if t.Kind == "unit_type" {
		col = "unit_type_id"
	}
	rows, err := tx.Query(ctx, `SELECT id, kind, name, pricing_unit, price, max_qty FROM bvrooms_addons WHERE property_id = $1 AND is_active
		AND ((room_type_id IS NULL AND unit_type_id IS NULL) OR `+col+` = $2) ORDER BY sort_order, name`, propertyID, t.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AddonOffer{}
	for rows.Next() {
		var a AddonOffer
		if err := rows.Scan(&a.ID, &a.Kind, &a.Name, &a.PricingUnit, &a.Price, &a.MaxQty); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// typeOffersTx: tipe kamar/unit dengan harga (tanpa tanggal: rate malam ini dari base/rate aktif, 1 malam) & availability bila tanggal diberikan.
func (s *Service) typeOffersTx(ctx context.Context, tx pgx.Tx, l *listingRow, checkIn, checkOut *time.Time, rooms, adults, children int) ([]TypeOffer, error) {
	types, err := s.typesTx(ctx, tx, l, nil)
	if err != nil {
		return nil, err
	}
	out := []TypeOffer{}
	for i := range types {
		t := &types[i]
		o := TypeOffer{ID: t.ID, Kind: t.Kind, Name: t.Name, Description: t.Description, SizeM2: t.SizeM2, Bedrooms: t.Bedrooms, MaxAdults: t.CapAdults, MaxChildren: t.CapChildren, BedType: t.BedType, Amenities: t.Amenities, baseRate: t.BaseRate, currency: t.Currency}
		var key *string
		col := "room_type_id"
		if t.Kind == "unit_type" {
			col = "unit_type_id"
		}
		_ = tx.QueryRow(ctx, `SELECT storage_key FROM bvrooms_property_photos WHERE property_id = $1 AND `+col+` = $2 AND status = 'ready' ORDER BY is_cover DESC, sort_order, created_at LIMIT 1`, l.PropertyID, t.ID).Scan(&key)
		o.PhotoURL = s.photoURL(ctx, key)
		ci := time.Now().In(l.location())
		ci = time.Date(ci.Year(), ci.Month(), ci.Day(), 0, 0, 0, 0, time.UTC)
		co := ci.AddDate(0, 0, 1)
		if checkIn != nil && checkOut != nil {
			ci, co = *checkIn, *checkOut
		}
		_, first, total, err := s.resolveRateTx(ctx, tx, t, ci, co)
		if err != nil {
			return nil, err
		}
		o.RatePerNight, o.Nights, o.LineTotal = first, int(co.Sub(ci).Hours()/24), total
		if o.Nights < 1 {
			o.Nights = 1
		}
		if promo, err := s.bestPromoTx(ctx, tx, l.PropertyID, t, ci, co, total); err != nil {
			return nil, err
		} else if promo != nil {
			o.Promo = promo
			o.LineTotal = total - promo.DiscountAmount
		}
		if checkIn != nil {
			o.TotalUnits, o.AvailableCount, err = s.availabilityTx(ctx, tx, t, ci, co)
			if err != nil {
				return nil, err
			}
			need := rooms
			if need < 1 {
				need = 1
			}
			o.IsAvailable = o.AvailableCount >= need && (adults <= t.CapAdults*need) && (children <= t.CapChildren*need)
		} else {
			o.TotalUnits, o.AvailableCount, err = s.availabilityTx(ctx, tx, t, ci, co)
			if err != nil {
				return nil, err
			}
			o.IsAvailable = o.AvailableCount > 0
		}
		o.Addons, err = s.addonsForTypeTx(ctx, tx, l.PropertyID, t)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// RoomTypes: GET /catalog/properties/{slug}/room-types?check_in&check_out&rooms&adults&children
func (s *Service) RoomTypes(ctx context.Context, slug string, checkIn, checkOut *time.Time, rooms, adults, children int) ([]TypeOffer, error) {
	orgID := mustOrg(ctx)
	if (checkIn == nil) != (checkOut == nil) {
		return nil, apperr.Validation("check_in dan check_out harus diberikan bersama")
	}
	if checkIn != nil && !checkOut.After(*checkIn) {
		return nil, apperr.Validation("check_out harus setelah check_in")
	}
	var out []TypeOffer
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		l, err := s.listingBySlugTx(ctx, tx, slug, true)
		if err != nil {
			return err
		}
		out, err = s.typeOffersTx(ctx, tx, l, checkIn, checkOut, rooms, adults, children)
		return err
	})
	return out, err
}

// minRateForStayTx: harga termurah per malam di antara tipe yang tersedia untuk stay (kartu "Start from").
func (s *Service) minRateForStayTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, category string, checkIn, checkOut time.Time, rooms, adults, children int) (*int64, error) {
	l := &listingRow{PropertyID: propertyID, Category: category, Timezone: "Asia/Jakarta"}
	offers, err := s.typeOffersTx(ctx, tx, l, &checkIn, &checkOut, rooms, adults, children)
	if err != nil {
		return nil, err
	}
	var min *int64
	for _, o := range offers {
		if !o.IsAvailable {
			continue
		}
		v := o.LineTotal / int64(o.Nights)
		if min == nil || v < *min {
			vv := v
			min = &vv
		}
	}
	return min, nil
}

// ---------- kalender ketersediaan per tipe ----------

type CalendarDay struct {
	Date         string `json:"date"`
	IsAvailable  bool   `json:"is_available"`
	RatePerNight int64  `json:"rate_per_night"`
}

func (s *Service) Calendar(ctx context.Context, slug string, typeID uuid.UUID, month string) ([]CalendarDay, error) {
	orgID := mustOrg(ctx)
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, apperr.Validation("month harus YYYY-MM")
	}
	end := start.AddDate(0, 1, 0)
	out := []CalendarDay{}
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		l, err := s.listingBySlugTx(ctx, tx, slug, true)
		if err != nil {
			return err
		}
		types, err := s.typesTx(ctx, tx, l, &typeID)
		if err != nil {
			return err
		}
		if len(types) == 0 {
			return apperr.NotFound("Tipe kamar/unit")
		}
		t := &types[0]
		for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
			_, avail, err := s.availabilityTx(ctx, tx, t, d, d.AddDate(0, 0, 1))
			if err != nil {
				return err
			}
			_, first, _, err := s.resolveRateTx(ctx, tx, t, d, d.AddDate(0, 0, 1))
			if err != nil {
				return err
			}
			out = append(out, CalendarDay{Date: d.Format("2006-01-02"), IsAvailable: avail > 0, RatePerNight: first})
		}
		return nil
	})
	return out, err
}

// ---------- banner (dashboard) + promo show_as_banner ----------

type Banner struct {
	ID       uuid.UUID `json:"id"`
	Kind     string    `json:"kind"` // banner | promotion
	Title    string    `json:"title"`
	Subtitle *string   `json:"subtitle"`
	ImageURL *string   `json:"image_url"`
	CTALabel *string   `json:"cta_label"`
	DeepLink *string   `json:"deep_link"`
}

func (s *Service) Banners(ctx context.Context, propertyID *uuid.UUID) ([]Banner, error) {
	orgID := mustOrg(ctx)
	out := []Banner{}
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, title, subtitle, image_url, image_storage_key, cta_label, deep_link FROM bvrooms_banners
			WHERE is_active AND (starts_at IS NULL OR starts_at <= now()) AND (ends_at IS NULL OR ends_at >= now()) AND (property_id IS NULL OR $1::uuid IS NULL OR property_id = $1) ORDER BY sort_order, created_at`, propertyID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var b Banner
			var key *string
			if err := rows.Scan(&b.ID, &b.Title, &b.Subtitle, &b.ImageURL, &key, &b.CTALabel, &b.DeepLink); err != nil {
				rows.Close()
				return err
			}
			b.Kind = "banner"
			if b.ImageURL == nil {
				b.ImageURL = s.photoURL(ctx, key)
			}
			out = append(out, b)
		}
		rows.Close()
		rows, err = tx.Query(ctx, `SELECT pr.id, pr.name, pr.discount_type, pr.discount_value, pr.banner_image_url, l.slug FROM bvrooms_promotions pr LEFT JOIN bvrooms_property_listings l ON l.property_id = pr.property_id
			WHERE pr.is_active AND pr.show_as_banner AND (pr.book_from IS NULL OR pr.book_from <= now()) AND (pr.book_until IS NULL OR pr.book_until >= now()) AND (pr.property_id IS NULL OR $1::uuid IS NULL OR pr.property_id = $1) ORDER BY pr.created_at DESC`, propertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b Banner
			var dt string
			var dv int64
			var slug *string
			if err := rows.Scan(&b.ID, &b.Title, &dt, &dv, &b.ImageURL, &slug); err != nil {
				return err
			}
			b.Kind = "promotion"
			sub := "Potongan Rp " + formatIDR(dv)
			if dt == "percent" {
				sub = "Potongan Harga " + strconv.FormatInt(dv, 10) + "%"
			}
			b.Subtitle = &sub
			b.CTALabel = strPtr("Booking Sekarang")
			if slug != nil {
				b.DeepLink = strPtr("/property/" + *slug)
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

func formatIDR(v int64) string {
	s := strconv.FormatInt(v, 10)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, c)
	}
	return string(out)
}

// ---------- review summary publik ----------

func (s *Service) ReviewSummary(ctx context.Context, slug string) (*RatingSummary, error) {
	orgID := mustOrg(ctx)
	var out *RatingSummary
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		l, err := s.listingBySlugTx(ctx, tx, slug, true)
		if err != nil {
			return err
		}
		r := ratingSummary(l)
		out = &r
		return nil
	})
	return out, err
}
