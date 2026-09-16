package bvrooms

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/hotel"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/platform/ids"
	"github.com/buildingvision/api/internal/platform/storage"
)

// ============ Dashboard (staf; permission bvrooms.*) — Requirements v0.2 §5.6 ============

// ---------- listing ----------

type Listing struct {
	PropertyID          uuid.UUID            `json:"property_id"`
	PropertyName        string               `json:"property_name"`
	Profile             string               `json:"profile"`
	Listed              bool                 `json:"bvrooms_listed"`
	Slug                string               `json:"slug"`
	Category            string               `json:"listing_category"`
	DisplayName         string               `json:"display_name"`
	Tagline             *string              `json:"tagline"`
	AddressLine         *string              `json:"address_line"`
	District            *string              `json:"district"`
	City                *string              `json:"city"`
	Lat                 *float64             `json:"lat"`
	Lng                 *float64             `json:"lng"`
	Phone               *string              `json:"phone"`
	WhatsApp            *string              `json:"whatsapp"`
	CheckInTime         string               `json:"check_in_time"`
	CheckOutTime        string               `json:"check_out_time"`
	DescriptionSections []DescriptionSection `json:"description_sections"`
	Facilities          []string             `json:"facilities"`
	Policies            []string             `json:"policies"`
	CancellationPolicy  *string              `json:"cancellation_policy_md"`
	CancellationRules   json.RawMessage      `json:"cancellation_rules"`
	PaymentWindowHours  int                  `json:"payment_window_hours"`
	BankAccounts        json.RawMessage      `json:"bank_accounts"`
	AllowPayAtProperty  bool                 `json:"allow_pay_at_property"`
	RatingSummary       RatingSummary        `json:"rating_summary"`
	MinRateCache        *int64               `json:"min_rate_cache"`
	Photos              []AdminPhoto         `json:"photos"`
	Version             int                  `json:"version"`
}

type AdminPhoto struct {
	ID         uuid.UUID  `json:"id"`
	URL        *string    `json:"url"`
	Category   string     `json:"category"`
	RoomTypeID *uuid.UUID `json:"room_type_id"`
	UnitTypeID *uuid.UUID `json:"unit_type_id"`
	Caption    *string    `json:"caption"`
	SortOrder  int        `json:"sort_order"`
	IsCover    bool       `json:"is_cover"`
	Status     string     `json:"status"`
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(v string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(v)), "-"), "-")
	if s == "" {
		s = "property"
	}
	return s
}

func (s *Service) listingDTOTx(ctx context.Context, tx pgx.Tx, l *listingRow) (*Listing, error) {
	out := &Listing{PropertyID: l.PropertyID, Listed: l.Listed, Slug: l.Slug, Category: l.Category, DisplayName: l.DisplayName, Tagline: l.Tagline, AddressLine: l.AddressLine, District: l.District, City: l.City, Lat: l.Lat, Lng: l.Lng, Phone: l.Phone, WhatsApp: l.WhatsApp,
		CheckInTime: l.CheckInTime, CheckOutTime: l.CheckOutTime, Facilities: l.Facilities, Policies: l.Policies, CancellationPolicy: l.CancellationMD, CancellationRules: json.RawMessage(l.CancellationRules), PaymentWindowHours: l.PaymentWindowH,
		BankAccounts: json.RawMessage(l.BankAccounts), AllowPayAtProperty: l.AllowPayAtProp, RatingSummary: ratingSummary(l), MinRateCache: l.MinRateCache, Version: l.Version}
	out.DescriptionSections = []DescriptionSection{}
	_ = json.Unmarshal(l.Description, &out.DescriptionSections)
	if out.Facilities == nil {
		out.Facilities = []string{}
	}
	if out.Policies == nil {
		out.Policies = []string{}
	}
	_ = tx.QueryRow(ctx, `SELECT loc.name, p.profile FROM properties p JOIN locations loc ON loc.id = p.location_id WHERE p.location_id = $1`, l.PropertyID).Scan(&out.PropertyName, &out.Profile)
	rows, err := tx.Query(ctx, `SELECT id, storage_key, category, room_type_id, unit_type_id, caption, sort_order, is_cover, status FROM bvrooms_property_photos WHERE property_id = $1 ORDER BY is_cover DESC, sort_order, created_at`, l.PropertyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out.Photos = []AdminPhoto{}
	for rows.Next() {
		var ph AdminPhoto
		var key string
		if err := rows.Scan(&ph.ID, &key, &ph.Category, &ph.RoomTypeID, &ph.UnitTypeID, &ph.Caption, &ph.SortOrder, &ph.IsCover, &ph.Status); err != nil {
			return nil, err
		}
		if ph.Status == "ready" {
			ph.URL = s.photoURL(ctx, &key)
		}
		out.Photos = append(out.Photos, ph)
	}
	return out, rows.Err()
}

// GetListing: profil listing (dibuat draft otomatis dari data property bila belum ada).
func (s *Service) GetListing(ctx context.Context, propertyID uuid.UUID) (*Listing, error) {
	if err := iam.CanOnProperty(ctx, "bvrooms.listing.view", propertyID); err != nil {
		return nil, err
	}
	var out *Listing
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		l, err := s.ensureListingTx(ctx, tx, propertyID)
		if err != nil {
			return err
		}
		out, err = s.listingDTOTx(ctx, tx, l)
		return err
	})
	return out, err
}

func (s *Service) ensureListingTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID) (*listingRow, error) {
	l, err := s.listingByIDTx(ctx, tx, propertyID)
	if err == nil {
		return l, nil
	}
	if !apperr.Is(err, "NOT_FOUND") {
		return nil, err
	}
	p := authctx.Must(ctx)
	var name, profile string
	var address, city *string
	if err := tx.QueryRow(ctx, `SELECT loc.name, p.profile, p.address, p.city FROM properties p JOIN locations loc ON loc.id = p.location_id WHERE p.location_id = $1 AND loc.deleted_at IS NULL`, propertyID).Scan(&name, &profile, &address, &city); err != nil {
		return nil, apperr.NotFound("Property")
	}
	if profile == "office" {
		return nil, apperr.Conflict("CAPABILITY_NOT_ENABLED", "BVRooms hanya untuk property profile Hotel atau Apartment")
	}
	slug := slugify(name)
	for i := 2; ; i++ {
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bvrooms_property_listings WHERE slug = $1)`, slug).Scan(&exists)
		if !exists {
			break
		}
		slug = fmt.Sprintf("%s-%d", slugify(name), i)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_property_listings (property_id, organization_id, slug, listing_category, display_name, address_line, city, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		propertyID, p.OrganizationID, slug, profile, name, address, city, actorID(p)); err != nil {
		return nil, err
	}
	// add-on default (nonaktif sampai harga diisi) — §6
	for _, a := range []struct {
		kind, name, unit string
		max              int
	}{{"breakfast", "Breakfast", "per_guest_per_night", 4}, {"extra_bed", "Extra Bed", "per_item_per_night", 3}} {
		code, err := ids.NextYearly(ctx, tx, p.OrganizationID, "ADD", time.Now(), nil)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_addons (organization_id, property_id, addon_code, kind, name, pricing_unit, price, max_qty, is_active, sort_order) VALUES ($1,$2,$3,$4,$5,$6,0,$7,false,0)`,
			p.OrganizationID, propertyID, code, a.kind, a.name, a.unit, a.max); err != nil {
			return nil, err
		}
	}
	return s.listingByIDTx(ctx, tx, propertyID)
}

type ListingInput struct {
	Listed              *bool                 `json:"bvrooms_listed"`
	Slug                *string               `json:"slug"`
	Category            *string               `json:"listing_category"`
	DisplayName         *string               `json:"display_name"`
	Tagline             *string               `json:"tagline"`
	AddressLine         *string               `json:"address_line"`
	District            *string               `json:"district"`
	City                *string               `json:"city"`
	Lat                 *float64              `json:"lat"`
	Lng                 *float64              `json:"lng"`
	Phone               *string               `json:"phone"`
	WhatsApp            *string               `json:"whatsapp"`
	CheckInTime         *string               `json:"check_in_time"`
	CheckOutTime        *string               `json:"check_out_time"`
	DescriptionSections *[]DescriptionSection `json:"description_sections"`
	Facilities          *[]string             `json:"facilities"`
	Policies            *[]string             `json:"policies"`
	CancellationPolicy  *string               `json:"cancellation_policy_md"`
	CancellationRules   *json.RawMessage      `json:"cancellation_rules"`
	PaymentWindowHours  *int                  `json:"payment_window_hours"`
	BankAccounts        *json.RawMessage      `json:"bank_accounts"`
	AllowPayAtProperty  *bool                 `json:"allow_pay_at_property"`
}

var timeRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// UpdateListing: PUT /bvrooms/admin/properties/{id}/listing.
func (s *Service) UpdateListing(ctx context.Context, propertyID uuid.UUID, in ListingInput, ifVersion *int) (*Listing, error) {
	if err := iam.CanOnProperty(ctx, "bvrooms.listing.manage", propertyID); err != nil {
		return nil, err
	}
	if in.Category != nil && *in.Category != "hotel" && *in.Category != "apartment" {
		return nil, apperr.Validation("listing_category harus hotel|apartment")
	}
	if in.CheckInTime != nil && !timeRe.MatchString(*in.CheckInTime) || in.CheckOutTime != nil && !timeRe.MatchString(*in.CheckOutTime) {
		return nil, apperr.Validation("check_in_time/check_out_time harus HH:MM")
	}
	if in.PaymentWindowHours != nil && (*in.PaymentWindowHours < 1 || *in.PaymentWindowHours > 72) {
		return nil, apperr.Validation("payment_window_hours harus 1..72")
	}
	if in.BankAccounts != nil {
		var accounts []struct {
			Bank          string `json:"bank"`
			AccountNumber string `json:"account_number"`
			AccountName   string `json:"account_name"`
		}
		if err := json.Unmarshal(*in.BankAccounts, &accounts); err != nil {
			return nil, apperr.Validation("bank_accounts harus array {bank, account_number, account_name}")
		}
		for _, a := range accounts {
			if strings.TrimSpace(a.Bank) == "" || strings.TrimSpace(a.AccountNumber) == "" {
				return nil, apperr.Validation("bank_accounts: bank dan account_number wajib")
			}
		}
	}
	if in.CancellationRules != nil && !json.Valid(*in.CancellationRules) {
		return nil, apperr.Validation("cancellation_rules harus JSON")
	}
	p := authctx.Must(ctx)
	var out *Listing
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		l, err := s.ensureListingTx(ctx, tx, propertyID)
		if err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != l.Version {
			return apperr.StaleVersion()
		}
		if in.Slug != nil {
			sl := slugify(*in.Slug)
			in.Slug = &sl
		}
		var desc, rules, banks []byte
		if in.DescriptionSections != nil {
			desc = mustJSON(*in.DescriptionSections)
		}
		if in.CancellationRules != nil {
			rules = *in.CancellationRules
		}
		if in.BankAccounts != nil {
			banks = *in.BankAccounts
		}
		if in.Listed != nil && *in.Listed {
			// validasi minimum sebelum tayang: nama, alamat, dan ≥1 tipe aktif
			var nTypes int
			cat := l.Category
			if in.Category != nil {
				cat = *in.Category
			}
			if cat == "apartment" {
				_ = tx.QueryRow(ctx, `SELECT count(*) FROM bvrooms_unit_types WHERE property_id = $1 AND status = 'active'`, propertyID).Scan(&nTypes)
			} else {
				_ = tx.QueryRow(ctx, `SELECT count(*) FROM hotel_room_types WHERE property_id = $1 AND status = 'active'`, propertyID).Scan(&nTypes)
			}
			if nTypes == 0 {
				return apperr.Conflict("LISTING_INCOMPLETE", "Tambahkan minimal satu tipe kamar/unit aktif sebelum menayangkan properti")
			}
		}
		_, err = tx.Exec(ctx, `UPDATE bvrooms_property_listings SET
			bvrooms_listed = COALESCE($2, bvrooms_listed), slug = COALESCE($3, slug), listing_category = COALESCE($4, listing_category), display_name = COALESCE(NULLIF(TRIM($5),''), display_name),
			tagline = COALESCE($6, tagline), address_line = COALESCE($7, address_line), district = COALESCE($8, district), city = COALESCE($9, city), lat = COALESCE($10, lat), lng = COALESCE($11, lng),
			phone = COALESCE($12, phone), whatsapp = COALESCE($13, whatsapp), check_in_time = COALESCE($14::time, check_in_time), check_out_time = COALESCE($15::time, check_out_time),
			description_sections = COALESCE($16::jsonb, description_sections), facilities = COALESCE($17, facilities), policies = COALESCE($18, policies), cancellation_policy_md = COALESCE($19, cancellation_policy_md),
			cancellation_rules = COALESCE($20::jsonb, cancellation_rules), payment_window_hours = COALESCE($21, payment_window_hours), bank_accounts = COALESCE($22::jsonb, bank_accounts), allow_pay_at_property = COALESCE($23, allow_pay_at_property),
			updated_by = $24
			WHERE property_id = $1`,
			propertyID, in.Listed, in.Slug, in.Category, deref(in.DisplayName), in.Tagline, in.AddressLine, in.District, in.City, in.Lat, in.Lng, in.Phone, in.WhatsApp, in.CheckInTime, in.CheckOutTime,
			nilIfEmpty(desc), in.Facilities, in.Policies, in.CancellationPolicy, nilIfEmpty(rules), in.PaymentWindowHours, nilIfEmpty(banks), in.AllowPayAtProperty, actorID(p))
		if err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("SLUG_TAKEN", "slug sudah dipakai properti lain")
			}
			return err
		}
		if err := s.refreshListingCachesTx(ctx, tx); err != nil {
			return err
		}
		l2, err := s.listingByIDTx(ctx, tx, propertyID)
		if err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "bvrooms_listing", EntityID: &propertyID, EntityLabel: l2.DisplayName, Before: map[string]any{"listed": l.Listed}, After: map[string]any{"listed": l2.Listed, "category": l2.Category}})
		out, err = s.listingDTOTx(ctx, tx, l2)
		return err
	})
	return out, err
}

func nilIfEmpty(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return b
}

// ---------- foto listing (unggah langsung ke storage; presign → confirm) ----------

type PhotoPresignInput struct {
	Category    string     `json:"category"`
	RoomTypeID  *uuid.UUID `json:"room_type_id"`
	UnitTypeID  *uuid.UUID `json:"unit_type_id"`
	ContentType string     `json:"content_type"`
	SizeBytes   int64      `json:"size_bytes"`
	Caption     *string    `json:"caption"`
	IsCover     bool       `json:"is_cover"`
}

type PhotoPresignResult struct {
	PhotoID   uuid.UUID         `json:"photo_id"`
	UploadURL string            `json:"upload_url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

var photoCategories = map[string]bool{"facade": true, "room": true, "receptionist": true, "lobby": true, "restaurant": true, "pool": true, "other": true}

func (s *Service) PhotoPresign(ctx context.Context, propertyID uuid.UUID, in PhotoPresignInput) (*PhotoPresignResult, error) {
	if err := iam.CanOnProperty(ctx, "bvrooms.listing.manage", propertyID); err != nil {
		return nil, err
	}
	if in.Category == "" {
		in.Category = "other"
	}
	if !photoCategories[in.Category] {
		return nil, apperr.Validation("category tidak valid")
	}
	ext, ok := proofContent[in.ContentType]
	if !ok || in.ContentType == "application/pdf" {
		return nil, apperr.Validation("content_type harus image/jpeg|image/png|image/webp")
	}
	if in.SizeBytes <= 0 || in.SizeBytes > 10<<20 {
		return nil, apperr.Validation("size_bytes harus 1..10MB")
	}
	p := authctx.Must(ctx)
	var out *PhotoPresignResult
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := s.ensureListingTx(ctx, tx, propertyID); err != nil {
			return err
		}
		id := uuid.Must(uuid.NewV7())
		key := storage.ObjectKey(p.OrganizationID.String(), "bvrooms-photo-"+id.String(), s.now(), ext)
		if _, err := tx.Exec(ctx, `INSERT INTO bvrooms_property_photos (id, organization_id, property_id, category, room_type_id, unit_type_id, storage_key, content_type, size_bytes, caption, is_cover, created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			id, p.OrganizationID, propertyID, in.Category, in.RoomTypeID, in.UnitTypeID, key, in.ContentType, in.SizeBytes, in.Caption, in.IsCover, actorID(p)); err != nil {
			return err
		}
		url, err := s.Storage.PresignPut(ctx, key, in.ContentType, in.SizeBytes, s.uploadTTL)
		if err != nil {
			return err
		}
		out = &PhotoPresignResult{PhotoID: id, UploadURL: url, Method: "PUT", Headers: map[string]string{"Content-Type": in.ContentType}, ExpiresAt: s.now().Add(s.uploadTTL)}
		return nil
	})
	return out, err
}

func (s *Service) PhotoConfirm(ctx context.Context, propertyID, photoID uuid.UUID) (*AdminPhoto, error) {
	if err := iam.CanOnProperty(ctx, "bvrooms.listing.manage", propertyID); err != nil {
		return nil, err
	}
	var out *AdminPhoto
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var key string
		var isCover bool
		if err := tx.QueryRow(ctx, `SELECT storage_key, is_cover FROM bvrooms_property_photos WHERE id = $1 AND property_id = $2`, photoID, propertyID).Scan(&key, &isCover); err != nil {
			return apperr.NotFound("Foto")
		}
		size, _, err := s.Storage.Head(ctx, key)
		if err != nil {
			return apperr.Conflict("UPLOAD_NOT_FOUND", "File belum diunggah ke storage")
		}
		if isCover {
			_, _ = tx.Exec(ctx, `UPDATE bvrooms_property_photos SET is_cover = false WHERE property_id = $1 AND id <> $2`, propertyID, photoID)
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_property_photos SET status = 'ready', size_bytes = $2 WHERE id = $1`, photoID, size); err != nil {
			return err
		}
		out, err = s.adminPhotoTx(ctx, tx, photoID)
		return err
	})
	return out, err
}

func (s *Service) adminPhotoTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*AdminPhoto, error) {
	var ph AdminPhoto
	var key string
	if err := tx.QueryRow(ctx, `SELECT id, storage_key, category, room_type_id, unit_type_id, caption, sort_order, is_cover, status FROM bvrooms_property_photos WHERE id = $1`, id).Scan(&ph.ID, &key, &ph.Category, &ph.RoomTypeID, &ph.UnitTypeID, &ph.Caption, &ph.SortOrder, &ph.IsCover, &ph.Status); err != nil {
		return nil, apperr.NotFound("Foto")
	}
	if ph.Status == "ready" {
		ph.URL = s.photoURL(ctx, &key)
	}
	return &ph, nil
}

type PhotoPatchInput struct {
	Category   *string    `json:"category"`
	RoomTypeID *uuid.UUID `json:"room_type_id"`
	UnitTypeID *uuid.UUID `json:"unit_type_id"`
	Caption    *string    `json:"caption"`
	SortOrder  *int       `json:"sort_order"`
	IsCover    *bool      `json:"is_cover"`
}

func (s *Service) PhotoPatch(ctx context.Context, propertyID, photoID uuid.UUID, in PhotoPatchInput) (*AdminPhoto, error) {
	if err := iam.CanOnProperty(ctx, "bvrooms.listing.manage", propertyID); err != nil {
		return nil, err
	}
	if in.Category != nil && !photoCategories[*in.Category] {
		return nil, apperr.Validation("category tidak valid")
	}
	var out *AdminPhoto
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if in.IsCover != nil && *in.IsCover {
			_, _ = tx.Exec(ctx, `UPDATE bvrooms_property_photos SET is_cover = false WHERE property_id = $1`, propertyID)
		}
		tag, err := tx.Exec(ctx, `UPDATE bvrooms_property_photos SET category = COALESCE($3, category), room_type_id = COALESCE($4, room_type_id), unit_type_id = COALESCE($5, unit_type_id), caption = COALESCE($6, caption), sort_order = COALESCE($7, sort_order), is_cover = COALESCE($8, is_cover) WHERE id = $1 AND property_id = $2`,
			photoID, propertyID, in.Category, in.RoomTypeID, in.UnitTypeID, in.Caption, in.SortOrder, in.IsCover)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Foto")
		}
		out, err = s.adminPhotoTx(ctx, tx, photoID)
		return err
	})
	return out, err
}

func (s *Service) PhotoDelete(ctx context.Context, propertyID, photoID uuid.UUID) error {
	if err := iam.CanOnProperty(ctx, "bvrooms.listing.manage", propertyID); err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var key string
		if err := tx.QueryRow(ctx, `DELETE FROM bvrooms_property_photos WHERE id = $1 AND property_id = $2 RETURNING storage_key`, photoID, propertyID).Scan(&key); err != nil {
			return apperr.NotFound("Foto")
		}
		_ = s.Storage.Delete(ctx, key)
		return nil
	})
}

// ---------- add-on (D5) ----------

type Addon struct {
	ID          uuid.UUID  `json:"id"`
	PropertyID  uuid.UUID  `json:"property_id"`
	AddonCode   string     `json:"addon_code"`
	RoomTypeID  *uuid.UUID `json:"room_type_id"`
	UnitTypeID  *uuid.UUID `json:"unit_type_id"`
	Kind        string     `json:"kind"`
	Name        string     `json:"name"`
	PricingUnit string     `json:"pricing_unit"`
	Price       int64      `json:"price"`
	MaxQty      int        `json:"max_qty"`
	IsActive    bool       `json:"is_active"`
	SortOrder   int        `json:"sort_order"`
	Version     int        `json:"version"`
}

type AddonInput struct {
	RoomTypeID  *uuid.UUID `json:"room_type_id"`
	UnitTypeID  *uuid.UUID `json:"unit_type_id"`
	Kind        *string    `json:"kind"`
	Name        *string    `json:"name"`
	PricingUnit *string    `json:"pricing_unit"`
	Price       *int64     `json:"price"`
	MaxQty      *int       `json:"max_qty"`
	IsActive    *bool      `json:"is_active"`
	SortOrder   *int       `json:"sort_order"`
}

const addonSelect = `SELECT id, property_id, addon_code, room_type_id, unit_type_id, kind, name, pricing_unit, price, max_qty, is_active, sort_order, version FROM bvrooms_addons`

func scanAddon(row pgx.Row) (*Addon, error) {
	var a Addon
	if err := row.Scan(&a.ID, &a.PropertyID, &a.AddonCode, &a.RoomTypeID, &a.UnitTypeID, &a.Kind, &a.Name, &a.PricingUnit, &a.Price, &a.MaxQty, &a.IsActive, &a.SortOrder, &a.Version); err != nil {
		return nil, err
	}
	return &a, nil
}

var addonKinds = map[string]bool{"breakfast": true, "extra_bed": true, "other": true}
var pricingUnits = map[string]bool{"per_guest_per_night": true, "per_item_per_night": true, "per_booking": true}

func (s *Service) ListAddons(ctx context.Context, propertyID uuid.UUID) ([]Addon, error) {
	if err := iam.CanOnProperty(ctx, "bvrooms.addons.view", propertyID); err != nil {
		return nil, err
	}
	out := []Addon{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, addonSelect+` WHERE property_id = $1 ORDER BY sort_order, name`, propertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scanAddon(rows)
			if err != nil {
				return err
			}
			out = append(out, *a)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) CreateAddon(ctx context.Context, propertyID uuid.UUID, in AddonInput) (*Addon, error) {
	if err := iam.CanOnProperty(ctx, "bvrooms.addons.manage", propertyID); err != nil {
		return nil, err
	}
	if in.Kind == nil || !addonKinds[*in.Kind] {
		return nil, apperr.Validation("kind harus breakfast|extra_bed|other")
	}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	if in.PricingUnit == nil || !pricingUnits[*in.PricingUnit] {
		return nil, apperr.Validation("pricing_unit harus per_guest_per_night|per_item_per_night|per_booking")
	}
	if in.Price == nil || *in.Price < 0 {
		return nil, apperr.Validation("price wajib ≥ 0")
	}
	if in.RoomTypeID != nil && in.UnitTypeID != nil {
		return nil, apperr.Validation("room_type_id dan unit_type_id tidak boleh keduanya")
	}
	p := authctx.Must(ctx)
	var out *Addon
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := s.ensureListingTx(ctx, tx, propertyID); err != nil {
			return err
		}
		code, err := ids.NextYearly(ctx, tx, p.OrganizationID, "ADD", time.Now(), nil)
		if err != nil {
			return err
		}
		maxQty, active, sort := 1, true, 0
		if in.MaxQty != nil {
			maxQty = *in.MaxQty
		}
		if in.IsActive != nil {
			active = *in.IsActive
		}
		if in.SortOrder != nil {
			sort = *in.SortOrder
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO bvrooms_addons (organization_id, property_id, room_type_id, unit_type_id, addon_code, kind, name, pricing_unit, price, max_qty, is_active, sort_order) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
			p.OrganizationID, propertyID, in.RoomTypeID, in.UnitTypeID, code, *in.Kind, strings.TrimSpace(*in.Name), *in.PricingUnit, *in.Price, maxQty, active, sort).Scan(&id); err != nil {
			return err
		}
		out, err = scanAddon(tx.QueryRow(ctx, addonSelect+` WHERE id = $1`, id))
		return err
	})
	return out, err
}

func (s *Service) UpdateAddon(ctx context.Context, propertyID, id uuid.UUID, in AddonInput) (*Addon, error) {
	if err := iam.CanOnProperty(ctx, "bvrooms.addons.manage", propertyID); err != nil {
		return nil, err
	}
	if in.Kind != nil && !addonKinds[*in.Kind] {
		return nil, apperr.Validation("kind harus breakfast|extra_bed|other")
	}
	if in.PricingUnit != nil && !pricingUnits[*in.PricingUnit] {
		return nil, apperr.Validation("pricing_unit tidak valid")
	}
	if in.Price != nil && *in.Price < 0 {
		return nil, apperr.Validation("price harus ≥ 0")
	}
	if in.MaxQty != nil && *in.MaxQty < 1 {
		return nil, apperr.Validation("max_qty minimal 1")
	}
	var out *Addon
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE bvrooms_addons SET room_type_id = COALESCE($3, room_type_id), unit_type_id = COALESCE($4, unit_type_id), kind = COALESCE($5, kind), name = COALESCE(NULLIF(TRIM($6),''), name), pricing_unit = COALESCE($7, pricing_unit), price = COALESCE($8, price), max_qty = COALESCE($9, max_qty), is_active = COALESCE($10, is_active), sort_order = COALESCE($11, sort_order) WHERE id = $1 AND property_id = $2`,
			id, propertyID, in.RoomTypeID, in.UnitTypeID, in.Kind, deref(in.Name), in.PricingUnit, in.Price, in.MaxQty, in.IsActive, in.SortOrder)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Add-on")
		}
		out, err = scanAddon(tx.QueryRow(ctx, addonSelect+` WHERE id = $1`, id))
		return err
	})
	return out, err
}

// ---------- tipe unit apartemen (D4) ----------

type UnitType struct {
	ID               uuid.UUID `json:"id"`
	PropertyID       uuid.UUID `json:"property_id"`
	UnitTypeCode     string    `json:"unit_type_code"`
	Name             string    `json:"name"`
	Description      *string   `json:"description"`
	CapacityAdults   int       `json:"capacity_adults"`
	CapacityChildren int       `json:"capacity_children"`
	Bedrooms         int       `json:"bedrooms"`
	SizeM2           *float64  `json:"size_m2"`
	Amenities        []string  `json:"amenities"`
	BaseRate         int64     `json:"base_rate"`
	CurrencyCode     string    `json:"currency_code"`
	Status           string    `json:"status"`
	UnitCount        int       `json:"unit_count"`
	Version          int       `json:"version"`
}

type UnitTypeInput struct {
	PropertyID       *uuid.UUID `json:"property_id"`
	Name             *string    `json:"name"`
	Description      *string    `json:"description"`
	CapacityAdults   *int       `json:"capacity_adults"`
	CapacityChildren *int       `json:"capacity_children"`
	Bedrooms         *int       `json:"bedrooms"`
	SizeM2           *float64   `json:"size_m2"`
	Amenities        *[]string  `json:"amenities"`
	BaseRate         *int64     `json:"base_rate"`
	Status           *string    `json:"status"`
}

const utSelect = `SELECT ut.id, ut.property_id, ut.unit_type_code, ut.name, ut.description, ut.capacity_adults, ut.capacity_children, ut.bedrooms, ut.size_m2, ut.amenities, ut.base_rate, ut.currency_code, ut.status, ut.version,
	(SELECT count(*) FROM units u WHERE u.bvrooms_unit_type_id = ut.id AND u.rentable_daily) FROM bvrooms_unit_types ut`

func scanUT(row pgx.Row) (*UnitType, error) {
	var t UnitType
	if err := row.Scan(&t.ID, &t.PropertyID, &t.UnitTypeCode, &t.Name, &t.Description, &t.CapacityAdults, &t.CapacityChildren, &t.Bedrooms, &t.SizeM2, &t.Amenities, &t.BaseRate, &t.CurrencyCode, &t.Status, &t.Version, &t.UnitCount); err != nil {
		return nil, err
	}
	t.CurrencyCode = strings.TrimSpace(t.CurrencyCode)
	if t.Amenities == nil {
		t.Amenities = []string{}
	}
	return &t, nil
}

func (s *Service) ListUnitTypes(ctx context.Context, propertyID uuid.UUID) ([]UnitType, error) {
	if err := iam.CanOnProperty(ctx, "bvrooms.inventory.view", propertyID); err != nil {
		return nil, err
	}
	out := []UnitType{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, utSelect+` WHERE ut.property_id = $1 ORDER BY ut.name`, propertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanUT(rows)
			if err != nil {
				return err
			}
			out = append(out, *t)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) CreateUnitType(ctx context.Context, in UnitTypeInput) (*UnitType, error) {
	if in.PropertyID == nil || in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return nil, apperr.Validation("property_id dan name wajib")
	}
	if err := iam.CanOnProperty(ctx, "bvrooms.inventory.manage", *in.PropertyID); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	var out *UnitType
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var profile string
		if err := tx.QueryRow(ctx, `SELECT profile FROM properties WHERE location_id = $1`, *in.PropertyID).Scan(&profile); err != nil {
			return apperr.NotFound("Property")
		}
		if profile != "apartment" {
			return apperr.Conflict("CAPABILITY_NOT_ENABLED", "Tipe unit hanya untuk property profile Apartment; hotel memakai tipe kamar (hotel.room_types)")
		}
		code, err := ids.NextYearly(ctx, tx, p.OrganizationID, "UT", time.Now(), nil)
		if err != nil {
			return err
		}
		ca, cc, br := 2, 0, 1
		if in.CapacityAdults != nil {
			ca = *in.CapacityAdults
		}
		if in.CapacityChildren != nil {
			cc = *in.CapacityChildren
		}
		if in.Bedrooms != nil {
			br = *in.Bedrooms
		}
		var rate int64
		if in.BaseRate != nil {
			rate = *in.BaseRate
		}
		am := []string{}
		if in.Amenities != nil {
			am = *in.Amenities
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO bvrooms_unit_types (organization_id, property_id, unit_type_code, name, description, capacity_adults, capacity_children, bedrooms, size_m2, amenities, base_rate, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12) RETURNING id`,
			p.OrganizationID, *in.PropertyID, code, strings.TrimSpace(*in.Name), in.Description, ca, cc, br, in.SizeM2, am, rate, actorID(p)).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "bvrooms_unit_type", EntityID: &id, EntityLabel: code + " " + *in.Name})
		out, err = scanUT(tx.QueryRow(ctx, utSelect+` WHERE ut.id = $1`, id))
		return err
	})
	return out, err
}

func (s *Service) UpdateUnitType(ctx context.Context, id uuid.UUID, in UnitTypeInput) (*UnitType, error) {
	p := authctx.Must(ctx)
	if in.Status != nil && *in.Status != "active" && *in.Status != "archived" {
		return nil, apperr.Validation("status harus active|archived")
	}
	var out *UnitType
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		cur, err := scanUT(tx.QueryRow(ctx, utSelect+` WHERE ut.id = $1`, id))
		if err != nil {
			return apperr.NotFound("Tipe unit")
		}
		if err := iam.CanOnProperty(ctx, "bvrooms.inventory.manage", cur.PropertyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_unit_types SET name = COALESCE(NULLIF(TRIM($2),''), name), description = COALESCE($3, description), capacity_adults = COALESCE($4, capacity_adults), capacity_children = COALESCE($5, capacity_children), bedrooms = COALESCE($6, bedrooms), size_m2 = COALESCE($7, size_m2), amenities = COALESCE($8, amenities), base_rate = COALESCE($9, base_rate), status = COALESCE($10, status), updated_by = $11 WHERE id = $1`,
			id, deref(in.Name), in.Description, in.CapacityAdults, in.CapacityChildren, in.Bedrooms, in.SizeM2, in.Amenities, in.BaseRate, in.Status, actorID(p)); err != nil {
			return err
		}
		out, err = scanUT(tx.QueryRow(ctx, utSelect+` WHERE ut.id = $1`, id))
		return err
	})
	return out, err
}

type UnitRentalInput struct {
	UnitTypeID    *uuid.UUID `json:"unit_type_id"`
	RentableDaily *bool      `json:"rentable_daily"`
}

// SetUnitRental: tandai unit residential sebagai inventori sewa harian BVRooms (PATCH /bvrooms/admin/units/{id}).
func (s *Service) SetUnitRental(ctx context.Context, unitID uuid.UUID, in UnitRentalInput) (map[string]any, error) {
	var out map[string]any
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var propertyID uuid.UUID
		var unitType, number string
		if err := tx.QueryRow(ctx, `SELECT l.property_id, u.unit_type, u.unit_number FROM units u JOIN locations l ON l.id = u.location_id WHERE u.location_id = $1 AND l.deleted_at IS NULL`, unitID).Scan(&propertyID, &unitType, &number); err != nil {
			return apperr.NotFound("Unit")
		}
		if err := iam.CanOnProperty(ctx, "bvrooms.inventory.manage", propertyID); err != nil {
			return err
		}
		if unitType == "hotel_room" {
			return apperr.Validation("Kamar hotel dikelola lewat hotel.rooms, bukan sewa unit")
		}
		if in.UnitTypeID != nil {
			var tp uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT property_id FROM bvrooms_unit_types WHERE id = $1`, *in.UnitTypeID).Scan(&tp); err != nil || tp != propertyID {
				return apperr.Validation("unit_type_id tidak valid untuk property ini")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE units SET bvrooms_unit_type_id = COALESCE($2, bvrooms_unit_type_id), rentable_daily = COALESCE($3, rentable_daily) WHERE location_id = $1`, unitID, in.UnitTypeID, in.RentableDaily); err != nil {
			return err
		}
		var utID *uuid.UUID
		var rentable bool
		_ = tx.QueryRow(ctx, `SELECT bvrooms_unit_type_id, rentable_daily FROM units WHERE location_id = $1`, unitID).Scan(&utID, &rentable)
		if rentable && utID == nil {
			return apperr.Validation("unit_type_id wajib bila rentable_daily = true")
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "unit", EntityID: &unitID, EntityLabel: number, After: map[string]any{"rentable_daily": rentable, "bvrooms_unit_type_id": utID}})
		out = map[string]any{"location_id": unitID, "unit_number": number, "property_id": propertyID, "unit_type_id": utID, "rentable_daily": rentable}
		return nil
	})
	return out, err
}

// ---------- promo & banner (§10) ----------

type Promotion struct {
	ID             uuid.UUID  `json:"id"`
	PropertyID     *uuid.UUID `json:"property_id"`
	RoomTypeID     *uuid.UUID `json:"room_type_id"`
	UnitTypeID     *uuid.UUID `json:"unit_type_id"`
	Name           string     `json:"name"`
	DiscountType   string     `json:"discount_type"`
	DiscountValue  int64      `json:"discount_value"`
	MinNights      int        `json:"min_nights"`
	StayFrom       *string    `json:"stay_from"`
	StayUntil      *string    `json:"stay_until"`
	BookFrom       *time.Time `json:"book_from"`
	BookUntil      *time.Time `json:"book_until"`
	ShowAsBanner   bool       `json:"show_as_banner"`
	BannerImageURL *string    `json:"banner_image_url"`
	IsActive       bool       `json:"is_active"`
	Version        int        `json:"version"`
}

type PromotionInput struct {
	PropertyID     *uuid.UUID `json:"property_id"`
	RoomTypeID     *uuid.UUID `json:"room_type_id"`
	UnitTypeID     *uuid.UUID `json:"unit_type_id"`
	Name           *string    `json:"name"`
	DiscountType   *string    `json:"discount_type"`
	DiscountValue  *int64     `json:"discount_value"`
	MinNights      *int       `json:"min_nights"`
	StayFrom       *string    `json:"stay_from"`
	StayUntil      *string    `json:"stay_until"`
	BookFrom       *time.Time `json:"book_from"`
	BookUntil      *time.Time `json:"book_until"`
	ShowAsBanner   *bool      `json:"show_as_banner"`
	BannerImageURL *string    `json:"banner_image_url"`
	IsActive       *bool      `json:"is_active"`
}

const promoSelect = `SELECT id, property_id, room_type_id, unit_type_id, name, discount_type, discount_value, min_nights, to_char(stay_from,'YYYY-MM-DD'), to_char(stay_until,'YYYY-MM-DD'), book_from, book_until, show_as_banner, banner_image_url, is_active, version FROM bvrooms_promotions`

func scanPromo(row pgx.Row) (*Promotion, error) {
	var p Promotion
	if err := row.Scan(&p.ID, &p.PropertyID, &p.RoomTypeID, &p.UnitTypeID, &p.Name, &p.DiscountType, &p.DiscountValue, &p.MinNights, &p.StayFrom, &p.StayUntil, &p.BookFrom, &p.BookUntil, &p.ShowAsBanner, &p.BannerImageURL, &p.IsActive, &p.Version); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Service) marketingPerm(ctx context.Context, action string, propertyID *uuid.UUID) error {
	p := authctx.Must(ctx)
	perm := "bvrooms.marketing." + action
	if propertyID != nil {
		return iam.CanOnProperty(ctx, perm, *propertyID)
	}
	if !p.Has(perm) {
		return apperr.Forbidden("")
	}
	return nil
}

func (s *Service) ListPromotions(ctx context.Context) ([]Promotion, error) {
	if err := s.marketingPerm(ctx, "view", nil); err != nil {
		return nil, err
	}
	out := []Promotion{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, promoSelect+` ORDER BY created_at DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanPromo(rows)
			if err != nil {
				return err
			}
			out = append(out, *p)
		}
		return rows.Err()
	})
	return out, err
}

func validatePromo(in PromotionInput, create bool) error {
	if create && (in.Name == nil || strings.TrimSpace(*in.Name) == "" || in.DiscountType == nil || in.DiscountValue == nil) {
		return apperr.Validation("name, discount_type, discount_value wajib")
	}
	if in.DiscountType != nil && *in.DiscountType != "percent" && *in.DiscountType != "fixed" {
		return apperr.Validation("discount_type harus percent|fixed")
	}
	if in.DiscountValue != nil && (*in.DiscountValue <= 0 || (in.DiscountType != nil && *in.DiscountType == "percent" && *in.DiscountValue > 100)) {
		return apperr.Validation("discount_value tidak valid")
	}
	for _, d := range []*string{in.StayFrom, in.StayUntil} {
		if d != nil && *d != "" {
			if _, err := parseDate(*d); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) CreatePromotion(ctx context.Context, in PromotionInput) (*Promotion, error) {
	if err := s.marketingPerm(ctx, "manage", in.PropertyID); err != nil {
		return nil, err
	}
	if err := validatePromo(in, true); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	var out *Promotion
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		minN, show, active := 1, true, true
		if in.MinNights != nil {
			minN = *in.MinNights
		}
		if in.ShowAsBanner != nil {
			show = *in.ShowAsBanner
		}
		if in.IsActive != nil {
			active = *in.IsActive
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO bvrooms_promotions (organization_id, property_id, room_type_id, unit_type_id, name, discount_type, discount_value, min_nights, stay_from, stay_until, book_from, book_until, show_as_banner, banner_image_url, is_active)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::date,NULLIF($10,'')::date,$11,$12,$13,$14,$15) RETURNING id`,
			p.OrganizationID, in.PropertyID, in.RoomTypeID, in.UnitTypeID, strings.TrimSpace(*in.Name), *in.DiscountType, *in.DiscountValue, minN, deref(in.StayFrom), deref(in.StayUntil), in.BookFrom, in.BookUntil, show, in.BannerImageURL, active).Scan(&id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "bvrooms_promotion", EntityID: &id, EntityLabel: *in.Name})
		var err error
		out, err = scanPromo(tx.QueryRow(ctx, promoSelect+` WHERE id = $1`, id))
		return err
	})
	return out, err
}

func (s *Service) UpdatePromotion(ctx context.Context, id uuid.UUID, in PromotionInput) (*Promotion, error) {
	if err := validatePromo(in, false); err != nil {
		return nil, err
	}
	var out *Promotion
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		cur, err := scanPromo(tx.QueryRow(ctx, promoSelect+` WHERE id = $1`, id))
		if err != nil {
			return apperr.NotFound("Promo")
		}
		if err := s.marketingPerm(ctx, "manage", cur.PropertyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_promotions SET property_id = COALESCE($2, property_id), room_type_id = COALESCE($3, room_type_id), unit_type_id = COALESCE($4, unit_type_id), name = COALESCE(NULLIF(TRIM($5),''), name), discount_type = COALESCE($6, discount_type), discount_value = COALESCE($7, discount_value), min_nights = COALESCE($8, min_nights),
			stay_from = COALESCE(NULLIF($9,'')::date, stay_from), stay_until = COALESCE(NULLIF($10,'')::date, stay_until), book_from = COALESCE($11, book_from), book_until = COALESCE($12, book_until), show_as_banner = COALESCE($13, show_as_banner), banner_image_url = COALESCE($14, banner_image_url), is_active = COALESCE($15, is_active) WHERE id = $1`,
			id, in.PropertyID, in.RoomTypeID, in.UnitTypeID, deref(in.Name), in.DiscountType, in.DiscountValue, in.MinNights, deref(in.StayFrom), deref(in.StayUntil), in.BookFrom, in.BookUntil, in.ShowAsBanner, in.BannerImageURL, in.IsActive); err != nil {
			return err
		}
		out, err = scanPromo(tx.QueryRow(ctx, promoSelect+` WHERE id = $1`, id))
		return err
	})
	return out, err
}

type BannerInput struct {
	PropertyID *uuid.UUID `json:"property_id"`
	Title      *string    `json:"title"`
	Subtitle   *string    `json:"subtitle"`
	ImageURL   *string    `json:"image_url"`
	CTALabel   *string    `json:"cta_label"`
	DeepLink   *string    `json:"deep_link"`
	StartsAt   *time.Time `json:"starts_at"`
	EndsAt     *time.Time `json:"ends_at"`
	SortOrder  *int       `json:"sort_order"`
	IsActive   *bool      `json:"is_active"`
}

type AdminBanner struct {
	ID         uuid.UUID  `json:"id"`
	PropertyID *uuid.UUID `json:"property_id"`
	Title      string     `json:"title"`
	Subtitle   *string    `json:"subtitle"`
	ImageURL   *string    `json:"image_url"`
	CTALabel   *string    `json:"cta_label"`
	DeepLink   *string    `json:"deep_link"`
	StartsAt   *time.Time `json:"starts_at"`
	EndsAt     *time.Time `json:"ends_at"`
	SortOrder  int        `json:"sort_order"`
	IsActive   bool       `json:"is_active"`
	Version    int        `json:"version"`
}

const bannerSelect = `SELECT id, property_id, title, subtitle, image_url, cta_label, deep_link, starts_at, ends_at, sort_order, is_active, version FROM bvrooms_banners`

func scanBanner(row pgx.Row) (*AdminBanner, error) {
	var b AdminBanner
	if err := row.Scan(&b.ID, &b.PropertyID, &b.Title, &b.Subtitle, &b.ImageURL, &b.CTALabel, &b.DeepLink, &b.StartsAt, &b.EndsAt, &b.SortOrder, &b.IsActive, &b.Version); err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Service) ListBanners(ctx context.Context) ([]AdminBanner, error) {
	if err := s.marketingPerm(ctx, "view", nil); err != nil {
		return nil, err
	}
	out := []AdminBanner{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, bannerSelect+` ORDER BY sort_order, created_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			b, err := scanBanner(rows)
			if err != nil {
				return err
			}
			out = append(out, *b)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) CreateBanner(ctx context.Context, in BannerInput) (*AdminBanner, error) {
	if err := s.marketingPerm(ctx, "manage", in.PropertyID); err != nil {
		return nil, err
	}
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		return nil, apperr.Validation("title wajib").WithField("title", "wajib")
	}
	p := authctx.Must(ctx)
	var out *AdminBanner
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sort, active := 0, true
		if in.SortOrder != nil {
			sort = *in.SortOrder
		}
		if in.IsActive != nil {
			active = *in.IsActive
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO bvrooms_banners (organization_id, property_id, title, subtitle, image_url, cta_label, deep_link, starts_at, ends_at, sort_order, is_active) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
			p.OrganizationID, in.PropertyID, strings.TrimSpace(*in.Title), in.Subtitle, in.ImageURL, in.CTALabel, in.DeepLink, in.StartsAt, in.EndsAt, sort, active).Scan(&id); err != nil {
			return err
		}
		var err error
		out, err = scanBanner(tx.QueryRow(ctx, bannerSelect+` WHERE id = $1`, id))
		return err
	})
	return out, err
}

func (s *Service) UpdateBanner(ctx context.Context, id uuid.UUID, in BannerInput) (*AdminBanner, error) {
	var out *AdminBanner
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		cur, err := scanBanner(tx.QueryRow(ctx, bannerSelect+` WHERE id = $1`, id))
		if err != nil {
			return apperr.NotFound("Banner")
		}
		if err := s.marketingPerm(ctx, "manage", cur.PropertyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE bvrooms_banners SET property_id = COALESCE($2, property_id), title = COALESCE(NULLIF(TRIM($3),''), title), subtitle = COALESCE($4, subtitle), image_url = COALESCE($5, image_url), cta_label = COALESCE($6, cta_label), deep_link = COALESCE($7, deep_link), starts_at = COALESCE($8, starts_at), ends_at = COALESCE($9, ends_at), sort_order = COALESCE($10, sort_order), is_active = COALESCE($11, is_active) WHERE id = $1`,
			id, in.PropertyID, deref(in.Title), in.Subtitle, in.ImageURL, in.CTALabel, in.DeepLink, in.StartsAt, in.EndsAt, in.SortOrder, in.IsActive); err != nil {
			return err
		}
		out, err = scanBanner(tx.QueryRow(ctx, bannerSelect+` WHERE id = $1`, id))
		return err
	})
	return out, err
}

// ---------- booking (dashboard) ----------

type AdminBookingFilter struct {
	PropertyID    *uuid.UUID
	Status        string // customer-facing: UNPAID|PAID|CHECK IN|CHECK OUT|CANCELLED|EXPIRED
	PaymentStatus string
	From, To      *time.Time
	Q             string
}

func (s *Service) AdminListBookings(ctx context.Context, f AdminBookingFilter, page httpx.Page) ([]Booking, *string, error) {
	p, err := staff(ctx)
	if err != nil {
		return nil, nil, err
	}
	propIDs, all := p.PropertyIDsFor("bvrooms.bookings.view")
	if !all && len(propIDs) == 0 {
		return nil, nil, apperr.Forbidden("")
	}
	if f.PropertyID != nil && !p.HasOnProperty("bvrooms.bookings.view", *f.PropertyID) {
		return nil, nil, apperr.Forbidden("")
	}
	out := []Booking{}
	var next *string
	q := "%" + strings.ToLower(strings.TrimSpace(f.Q)) + "%"
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var cursorAt *time.Time
		var cursorID *uuid.UUID
		if page.Cursor != nil {
			t, err := time.Parse(time.RFC3339Nano, page.Cursor.Value)
			if err != nil {
				return apperr.Validation("cursor tidak valid")
			}
			cursorAt, cursorID = &t, &page.Cursor.ID
		}
		rows, err := tx.Query(ctx, bookingSelect+` WHERE ($1::uuid IS NULL OR b.property_id = $1) AND ($2::uuid[] IS NULL OR b.property_id = ANY($2))
			AND ($3 = '' OR b.payment_status = $3) AND ($4::date IS NULL OR b.check_in_date >= $4) AND ($5::date IS NULL OR b.check_in_date <= $5)
			AND ($6 = '%%' OR lower(b.booking_code) LIKE $6 OR lower(b.guest_full_name) LIKE $6 OR lower(COALESCE(b.guest_phone,'')) LIKE $6)
			AND ($7::timestamptz IS NULL OR (b.created_at, b.id) < ($7, $8)) ORDER BY b.created_at DESC, b.id DESC LIMIT $9`,
			f.PropertyID, nilIfAll(propIDs, all), f.PaymentStatus, f.From, f.To, q, cursorAt, cursorID, page.Limit*3+1)
		if err != nil {
			return err
		}
		var rowsB []*bookingRow
		for rows.Next() {
			b, err := scanBooking(rows)
			if err != nil {
				rows.Close()
				return err
			}
			rowsB = append(rowsB, b)
		}
		rows.Close()
		now := s.now()
		for _, b := range rowsB {
			if f.Status != "" && b.deriveStatus(now) != f.Status {
				continue
			}
			if len(out) >= page.Limit {
				c := httpx.EncodeCursor(out[len(out)-1].CreatedAt.Format(time.RFC3339Nano), out[len(out)-1].ID)
				next = &c
				break
			}
			bk, err := s.assembleBookingTx(ctx, tx, b, true)
			if err != nil {
				return err
			}
			out = append(out, *bk)
		}
		return nil
	})
	return out, next, err
}

func nilIfAll(ids []uuid.UUID, all bool) []uuid.UUID {
	if all {
		return nil
	}
	return ids
}

func (s *Service) AdminGetBooking(ctx context.Context, id uuid.UUID) (*Booking, error) {
	p, err := staff(ctx)
	if err != nil {
		return nil, err
	}
	var out *Booking
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := scanBooking(tx.QueryRow(ctx, bookingSelect+` WHERE b.id = $1`, id))
		if err != nil {
			return apperr.NotFound("Booking")
		}
		if !p.HasOnProperty("bvrooms.bookings.view", b.PropertyID) {
			return apperr.Forbidden("")
		}
		out, err = s.assembleBookingTx(ctx, tx, b, true)
		return err
	})
	return out, err
}

type AdminActionInput struct {
	ReservationID  *uuid.UUID `json:"reservation_id"`   // opsional: satu kamar/unit; default semua baris booking
	RoomLocationID *uuid.UUID `json:"room_location_id"` // hotel: kamar (assign/check-in)
	UnitLocationID *uuid.UUID `json:"unit_location_id"` // apartemen: unit
	Reason         string     `json:"reason"`
}

var adminActions = map[string]bool{"confirm": true, "check_in": true, "check_out": true, "cancel": true, "no_show": true}

// AdminBookingAction: confirm | check_in | check_out | cancel | no_show. Hotel → hotel.Act (room status & housekeeping);
// apartemen → transisi langsung pada hotel_reservations (unit_location_id). Notifikasi customer lewat hook/lokal.
func (s *Service) AdminBookingAction(ctx context.Context, bookingID uuid.UUID, action string, in AdminActionInput) (*Booking, error) {
	p, err := staff(ctx)
	if err != nil {
		return nil, err
	}
	if !adminActions[action] {
		return nil, apperr.Validation("aksi tidak dikenal")
	}
	type line struct {
		resID  uuid.UUID
		kind   string
		status string
	}
	var lines []line
	var b *bookingRow
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		b, err = scanBooking(tx.QueryRow(ctx, bookingSelect+` WHERE b.id = $1`, bookingID))
		if err != nil {
			return apperr.NotFound("Booking")
		}
		if !p.HasOnProperty("bvrooms.bookings.manage", b.PropertyID) {
			return apperr.Forbidden("")
		}
		rows, err := tx.Query(ctx, `SELECT br.reservation_id, CASE WHEN br.unit_type_id IS NULL THEN 'room_type' ELSE 'unit_type' END, r.status FROM bvrooms_booking_rooms br JOIN hotel_reservations r ON r.id = br.reservation_id WHERE br.booking_id = $1 AND ($2::uuid IS NULL OR br.reservation_id = $2) ORDER BY br.sort_order`, bookingID, in.ReservationID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l line
			if err := rows.Scan(&l.resID, &l.kind, &l.status); err != nil {
				return err
			}
			lines = append(lines, l)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, apperr.NotFound("Reservasi")
	}
	if action == "cancel" && strings.TrimSpace(in.Reason) == "" {
		return nil, apperr.Validation("reason wajib").WithField("reason", "wajib")
	}
	for _, l := range lines {
		if l.kind == "room_type" {
			if s.Hotel == nil {
				return nil, apperr.Internal(fmt.Errorf("hotel service tidak tersedia"))
			}
			if _, err := s.Hotel.Act(ctx, l.resID, action, hotel.ActionInput{Reason: in.Reason, RoomLocationID: in.RoomLocationID}); err != nil {
				return nil, err
			}
			continue
		}
		if err := s.unitReservationActTx(ctx, l.resID, action, in); err != nil {
			return nil, err
		}
	}
	// pay_at_property: check-in oleh Front Office menandai kas diterima saat check-out (hook); cancel → sinkron booking (hook)
	var out *Booking
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.bookingTx(ctx, tx, bookingID, true)
		return err
	})
	return out, err
}

var unitTransitions = map[string]map[string]string{
	"confirm":   {"new": "confirmed"},
	"check_in":  {"confirmed": "checked_in", "new": "checked_in"},
	"check_out": {"checked_in": "checked_out"},
	"cancel":    {"new": "cancelled", "confirmed": "cancelled"},
	"no_show":   {"confirmed": "no_show", "new": "no_show"},
}

// unitReservationActTx: transisi reservasi unit apartemen (tanpa room status housekeeping) + hook notifikasi.
func (s *Service) unitReservationActTx(ctx context.Context, resID uuid.UUID, action string, in AdminActionInput) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var r hotel.Reservation
		var unitLoc *uuid.UUID
		var unitTypeID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id, reservation_number, property_id, status, guest_name, check_in_date, check_out_date, unit_location_id, unit_type_id, bvrooms_booking_id, cancel_reason FROM hotel_reservations WHERE id = $1 FOR UPDATE`, resID).Scan(&r.ID, &r.ReservationNumber, &r.PropertyID, &r.Status, &r.GuestName, &r.CheckInDate, &r.CheckOutDate, &unitLoc, &unitTypeID, &r.BVRoomsBookingID, &r.CancelReason); err != nil {
			return apperr.NotFound("Reservasi")
		}
		to, ok := unitTransitions[action][r.Status]
		if !ok {
			return apperr.InvalidTransition(fmt.Sprintf("Aksi %s tidak tersedia untuk reservasi berstatus %s", action, r.Status))
		}
		switch action {
		case "confirm":
			_, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'confirmed', confirmed_at = now(), updated_by = $2 WHERE id = $1`, resID, p.UserID)
			if err != nil {
				return err
			}
		case "check_in":
			unit := unitLoc
			if in.UnitLocationID != nil {
				unit = in.UnitLocationID
			}
			if unit == nil {
				// pilih unit bebas pertama dari tipe yang sama
				var free uuid.UUID
				if err := tx.QueryRow(ctx, `SELECT u.location_id FROM units u JOIN locations l ON l.id = u.location_id WHERE u.bvrooms_unit_type_id = $1 AND u.rentable_daily AND l.is_active AND l.deleted_at IS NULL
					AND NOT EXISTS (SELECT 1 FROM hotel_reservations x WHERE x.unit_location_id = u.location_id AND x.status IN ('new','confirmed','checked_in') AND x.stay && daterange($2::date, $3::date, '[)') AND x.id <> $4) ORDER BY u.unit_number LIMIT 1`,
					unitTypeID, r.CheckInDate, r.CheckOutDate, resID).Scan(&free); err != nil {
					return apperr.Conflict("ROOM_UNAVAILABLE", "Tidak ada unit bebas untuk tipe ini; tetapkan unit_location_id")
				}
				unit = &free
			}
			var ok bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM units u JOIN locations l ON l.id = u.location_id WHERE u.location_id = $1 AND u.bvrooms_unit_type_id = $2 AND u.rentable_daily AND l.property_id = $3)`, *unit, unitTypeID, r.PropertyID).Scan(&ok)
			if !ok {
				return apperr.Validation("unit_location_id harus unit sewa harian dari tipe yang sama")
			}
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'checked_in', checked_in_at = now(), unit_location_id = $2, updated_by = $3 WHERE id = $1`, resID, *unit, p.UserID); err != nil {
				if db.IsExclusionViolation(err) {
					return apperr.Conflict("ROOM_UNAVAILABLE", "Unit sudah disewa pada tanggal tersebut")
				}
				return err
			}
			_, _ = tx.Exec(ctx, `UPDATE units SET occupancy_status = 'occupied' WHERE location_id = $1`, *unit)
		case "check_out":
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'checked_out', checked_out_at = now(), updated_by = $2 WHERE id = $1`, resID, p.UserID); err != nil {
				return err
			}
			if unitLoc != nil {
				_, _ = tx.Exec(ctx, `UPDATE units SET occupancy_status = 'vacant' WHERE location_id = $1`, *unitLoc)
			}
		case "cancel":
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'cancelled', cancelled_at = now(), cancel_reason = $2, updated_by = $3 WHERE id = $1`, resID, in.Reason, p.UserID); err != nil {
				return err
			}
			r.CancelReason = strPtr(in.Reason)
		case "no_show":
			if _, err := tx.Exec(ctx, `UPDATE hotel_reservations SET status = 'no_show', no_show_at = now(), updated_by = $2 WHERE id = $1`, resID, p.UserID); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "hotel_reservation", EntityID: &resID, EntityLabel: r.ReservationNumber, Before: map[string]any{"status": r.Status}, After: map[string]any{"status": to, "reason": in.Reason, "kind": "unit"}})
		from := r.Status
		r.Status = to
		return s.onReservationTransition(ctx, tx, &r, action, from, to)
	})
}

// ---------- customer (dashboard) ----------

type AdminCustomer struct {
	Customer
	BookingCount int `json:"booking_count"`
}

func (s *Service) AdminListCustomers(ctx context.Context, q string, page httpx.Page) ([]AdminCustomer, *string, error) {
	p, err := staff(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !p.Has("bvrooms.customers.view") {
		return nil, nil, apperr.Forbidden("")
	}
	q = "%" + strings.ToLower(strings.TrimSpace(q)) + "%"
	out := []AdminCustomer{}
	var next *string
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var cursorAt *time.Time
		var cursorID *uuid.UUID
		if page.Cursor != nil {
			t, err := time.Parse(time.RFC3339Nano, page.Cursor.Value)
			if err != nil {
				return apperr.Validation("cursor tidak valid")
			}
			cursorAt, cursorID = &t, &page.Cursor.ID
		}
		rows, err := tx.Query(ctx, `SELECT c.id, c.full_name, c.phone_e164, c.email, c.status, c.locale, c.created_at, c.last_login_at, (SELECT count(*) FROM bvrooms_bookings b WHERE b.customer_id = c.id)
			FROM bvrooms_customers c WHERE ($1 = '%%' OR lower(c.full_name) LIKE $1 OR c.phone_e164 LIKE $1 OR lower(COALESCE(c.email,'')) LIKE $1)
			AND ($2::timestamptz IS NULL OR (c.created_at, c.id) < ($2, $3)) ORDER BY c.created_at DESC, c.id DESC LIMIT $4`, q, cursorAt, cursorID, page.Limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c AdminCustomer
			if err := rows.Scan(&c.ID, &c.FullName, &c.Phone, &c.Email, &c.Status, &c.Locale, &c.CreatedAt, &c.LastLogin, &c.BookingCount); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	if len(out) > page.Limit {
		out = out[:page.Limit]
		c := httpx.EncodeCursor(out[len(out)-1].CreatedAt.Format(time.RFC3339Nano), out[len(out)-1].ID)
		next = &c
	}
	return out, next, nil
}

func (s *Service) AdminSetCustomerStatus(ctx context.Context, id uuid.UUID, status string) (*Customer, error) {
	p, err := staff(ctx)
	if err != nil {
		return nil, err
	}
	if !p.Has("bvrooms.customers.manage") {
		return nil, apperr.Forbidden("")
	}
	if status != "active" && status != "blocked" {
		return nil, apperr.Validation("status harus active|blocked")
	}
	var out *Customer
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE bvrooms_customers SET status = $2 WHERE id = $1`, id, status)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Customer")
		}
		if status == "blocked" {
			_, _ = tx.Exec(ctx, `UPDATE bvrooms_sessions SET revoked_at = now() WHERE customer_id = $1 AND revoked_at IS NULL`, id)
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "bvrooms_customer", EntityID: &id, After: map[string]any{"status": status}})
		out, err = s.customerByIDTx(ctx, tx, id)
		return err
	})
	return out, err
}
