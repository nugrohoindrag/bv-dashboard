-- +goose Up
-- +goose StatementBegin
-- ============ BVROOMS — Customer Booking App (BVRooms-Backend-Requirements v0.2) ============
-- White-label per organization (D1): seluruh tabel org-scoped + RLS org_isolation. Hotel memakai inventori hotel_*;
-- apartemen (D4) memakai bvrooms_unit_types → units.rentable_daily. Reservasi keduanya di hotel_reservations (satu mesin status).
-- Pembayaran provider-agnostic, fase 1 manual_transfer / pay_at_property (D2). OTP disiapkan, vendor SMS di-hold (D3).

-- ---------- identitas & sesi customer ----------
CREATE TABLE bvrooms_customers (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  phone_e164      text NOT NULL,                                  -- +62812xxxx
  full_name       text NOT NULL,
  email           text,
  status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active','blocked')),
  locale          text NOT NULL DEFAULT 'id',
  last_login_at   timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, phone_e164)
);
CREATE TRIGGER trg_bvrooms_customers_upd BEFORE UPDATE ON bvrooms_customers FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE bvrooms_otp_codes (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  phone_e164      text NOT NULL,
  purpose         text NOT NULL CHECK (purpose IN ('login','register','change_phone')),
  code_hash       text NOT NULL,
  expires_at      timestamptz NOT NULL,
  attempts        int NOT NULL DEFAULT 0,
  consumed_at     timestamptz,
  ip              text,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_bvrooms_otp_phone ON bvrooms_otp_codes (organization_id, phone_e164, purpose, created_at DESC);

CREATE TABLE bvrooms_sessions (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  customer_id     uuid NOT NULL REFERENCES bvrooms_customers(id) ON DELETE CASCADE,
  device_id       text,
  refresh_hash    text NOT NULL UNIQUE,
  user_agent      text,
  ip              text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  revoked_at      timestamptz
);
CREATE INDEX idx_bvrooms_sessions_customer ON bvrooms_sessions (customer_id);

-- ---------- inventori apartemen (D4) ----------
CREATE TABLE bvrooms_unit_types (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  property_id       uuid NOT NULL REFERENCES properties(location_id),
  unit_type_code    text NOT NULL,                                -- UT-2026-000001
  name              text NOT NULL,                                -- Studio 25 m², 2BR
  description       text,
  capacity_adults   int NOT NULL DEFAULT 2,
  capacity_children int NOT NULL DEFAULT 0,
  bedrooms          int NOT NULL DEFAULT 1,
  size_m2           numeric(8,2),
  amenities         text[] NOT NULL DEFAULT '{}',
  base_rate         bigint NOT NULL DEFAULT 0,                    -- per malam (IDR)
  currency_code     char(3) NOT NULL DEFAULT 'IDR',
  status            text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
  created_at        timestamptz NOT NULL DEFAULT now(),
  created_by        uuid,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid,
  version           int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, unit_type_code)
);
CREATE INDEX idx_bvrooms_unit_types_property ON bvrooms_unit_types (organization_id, property_id, status);
CREATE TRIGGER trg_bvrooms_unit_types_upd BEFORE UPDATE ON bvrooms_unit_types FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- unit residential yang disewakan harian lewat BVRooms
ALTER TABLE units
  ADD COLUMN IF NOT EXISTS bvrooms_unit_type_id uuid REFERENCES bvrooms_unit_types(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS rentable_daily boolean NOT NULL DEFAULT false;
CREATE INDEX idx_units_bvrooms_unit_type ON units (bvrooms_unit_type_id) WHERE rentable_daily;

-- hotel_rates dapat menunjuk tipe unit apartemen
ALTER TABLE hotel_rates ALTER COLUMN room_type_id DROP NOT NULL;
ALTER TABLE hotel_rates ADD COLUMN IF NOT EXISTS unit_type_id uuid REFERENCES bvrooms_unit_types(id) ON DELETE CASCADE;
ALTER TABLE hotel_rates ADD CONSTRAINT hotel_rates_type_xor CHECK ((room_type_id IS NULL) <> (unit_type_id IS NULL));
CREATE INDEX idx_hotel_rates_unit_type ON hotel_rates (unit_type_id, status) WHERE unit_type_id IS NOT NULL;

-- ---------- profil listing (konten yang dipublikasikan) ----------
CREATE TABLE bvrooms_property_listings (
  property_id            uuid PRIMARY KEY REFERENCES properties(location_id) ON DELETE CASCADE,
  organization_id        uuid NOT NULL REFERENCES organizations(id),
  bvrooms_listed         boolean NOT NULL DEFAULT false,
  slug                   text NOT NULL,
  listing_category       text NOT NULL CHECK (listing_category IN ('hotel','apartment')),
  display_name           text NOT NULL,
  tagline                text,
  address_line           text,
  district               text,
  city                   text,
  lat                    numeric(9,6),
  lng                    numeric(9,6),
  phone                  text,                                     -- "Hubungi Hotel"
  whatsapp               text,
  check_in_time          time NOT NULL DEFAULT '14:00',
  check_out_time         time NOT NULL DEFAULT '12:00',
  description_sections   jsonb NOT NULL DEFAULT '[]'::jsonb,       -- [{key,title,body}]
  facilities             text[] NOT NULL DEFAULT '{}',
  policies               text[] NOT NULL DEFAULT '{}',
  cancellation_policy_md text,
  cancellation_rules     jsonb NOT NULL DEFAULT '{"free_until_hours_before_checkin":24,"fee_pct_after":100}'::jsonb,
  payment_window_hours   int NOT NULL DEFAULT 5 CHECK (payment_window_hours BETWEEN 1 AND 72),
  bank_accounts          jsonb NOT NULL DEFAULT '[]'::jsonb,       -- [{bank,account_number,account_name}] manual_transfer
  allow_pay_at_property  boolean NOT NULL DEFAULT false,
  popularity_score       numeric(12,2) NOT NULL DEFAULT 0,
  rating_avg             numeric(3,2) NOT NULL DEFAULT 0,
  rating_count           int NOT NULL DEFAULT 0,
  rating_hist            int[] NOT NULL DEFAULT '{0,0,0,0,0}',     -- index 0 = 1★
  min_rate_cache         bigint,
  min_rate_cached_at     timestamptz,
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  updated_by             uuid,
  version                int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, slug)
);
CREATE INDEX idx_bvrooms_listings_listed ON bvrooms_property_listings (organization_id, bvrooms_listed, listing_category);
CREATE TRIGGER trg_bvrooms_listings_upd BEFORE UPDATE ON bvrooms_property_listings FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE bvrooms_property_photos (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id) ON DELETE CASCADE,
  category        text NOT NULL DEFAULT 'other' CHECK (category IN ('facade','room','receptionist','lobby','restaurant','pool','other')),
  room_type_id    uuid REFERENCES hotel_room_types(id) ON DELETE SET NULL,
  unit_type_id    uuid REFERENCES bvrooms_unit_types(id) ON DELETE SET NULL,
  storage_key     text NOT NULL,
  content_type    text NOT NULL,
  size_bytes      bigint NOT NULL DEFAULT 0,
  status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','ready')),
  caption         text,
  sort_order      int NOT NULL DEFAULT 0,
  is_cover        boolean NOT NULL DEFAULT false,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid
);
CREATE INDEX idx_bvrooms_photos_property ON bvrooms_property_photos (property_id, status, sort_order);

CREATE TABLE bvrooms_banners (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  property_id       uuid REFERENCES properties(location_id) ON DELETE CASCADE,
  title             text NOT NULL,
  subtitle          text,
  image_url         text,
  image_storage_key text,
  cta_label         text,
  deep_link         text,
  starts_at         timestamptz,
  ends_at           timestamptz,
  sort_order        int NOT NULL DEFAULT 0,
  is_active         boolean NOT NULL DEFAULT true,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  version           int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_bvrooms_banners_upd BEFORE UPDATE ON bvrooms_banners FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- diskon otomatis tanpa kode (§10) — diatur di dashboard
CREATE TABLE bvrooms_promotions (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid REFERENCES properties(location_id) ON DELETE CASCADE,
  room_type_id     uuid REFERENCES hotel_room_types(id) ON DELETE CASCADE,
  unit_type_id     uuid REFERENCES bvrooms_unit_types(id) ON DELETE CASCADE,
  name             text NOT NULL,
  discount_type    text NOT NULL CHECK (discount_type IN ('percent','fixed')),
  discount_value   bigint NOT NULL CHECK (discount_value > 0),
  min_nights       int NOT NULL DEFAULT 1,
  stay_from        date,
  stay_until       date,
  book_from        timestamptz,
  book_until       timestamptz,
  show_as_banner   boolean NOT NULL DEFAULT true,
  banner_image_url text,
  is_active        boolean NOT NULL DEFAULT true,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  version          int NOT NULL DEFAULT 1
);
CREATE INDEX idx_bvrooms_promotions_org ON bvrooms_promotions (organization_id, is_active);
CREATE TRIGGER trg_bvrooms_promotions_upd BEFORE UPDATE ON bvrooms_promotions FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- add-on / package (D5) — harga & aturan diinput di dashboard
CREATE TABLE bvrooms_addons (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id) ON DELETE CASCADE,
  room_type_id    uuid REFERENCES hotel_room_types(id) ON DELETE CASCADE,
  unit_type_id    uuid REFERENCES bvrooms_unit_types(id) ON DELETE CASCADE,
  addon_code      text NOT NULL,                                  -- ADD-2026-000001
  kind            text NOT NULL CHECK (kind IN ('breakfast','extra_bed','other')),
  name            text NOT NULL,
  pricing_unit    text NOT NULL CHECK (pricing_unit IN ('per_guest_per_night','per_item_per_night','per_booking')),
  price           bigint NOT NULL CHECK (price >= 0),
  max_qty         int NOT NULL DEFAULT 1 CHECK (max_qty >= 1),
  is_active       boolean NOT NULL DEFAULT true,
  sort_order      int NOT NULL DEFAULT 0,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, addon_code)
);
CREATE INDEX idx_bvrooms_addons_property ON bvrooms_addons (property_id, is_active);
CREATE TRIGGER trg_bvrooms_addons_upd BEFORE UPDATE ON bvrooms_addons FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------- booking (header multi-kamar) ----------
CREATE TABLE bvrooms_bookings (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  booking_code     text NOT NULL,                                 -- 12 char base32 (H1H38Q8SMD8P)
  customer_id      uuid NOT NULL REFERENCES bvrooms_customers(id),
  guest_full_name  text NOT NULL,
  guest_email      text,
  guest_phone      text,
  check_in_date    date NOT NULL,
  check_out_date   date NOT NULL,
  nights           int GENERATED ALWAYS AS (check_out_date - check_in_date) STORED,
  rooms_count      int NOT NULL DEFAULT 1,
  adults_total     int NOT NULL DEFAULT 1,
  children_total   int NOT NULL DEFAULT 0,
  room_subtotal    bigint NOT NULL DEFAULT 0,
  addon_subtotal   bigint NOT NULL DEFAULT 0,
  discount_amount  bigint NOT NULL DEFAULT 0,
  promotion_id     uuid REFERENCES bvrooms_promotions(id) ON DELETE SET NULL,
  total_amount     bigint NOT NULL DEFAULT 0,
  currency_code    char(3) NOT NULL DEFAULT 'IDR',
  payment_status   text NOT NULL DEFAULT 'unpaid' CHECK (payment_status IN ('unpaid','paid','expired','refund_pending','refunded','pay_at_property')),
  payment_deadline_at timestamptz,
  status_override  text CHECK (status_override IN ('cancelled','expired')),
  cancelled_at     timestamptz,
  cancel_reason    text,
  cancelled_by     text CHECK (cancelled_by IN ('customer','property','system')),
  reviewed_at      timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, booking_code),
  CHECK (check_out_date > check_in_date)
);
CREATE INDEX idx_bvrooms_bookings_customer ON bvrooms_bookings (customer_id, created_at DESC);
CREATE INDEX idx_bvrooms_bookings_property ON bvrooms_bookings (organization_id, property_id, check_in_date);
CREATE INDEX idx_bvrooms_bookings_deadline ON bvrooms_bookings (payment_deadline_at) WHERE payment_status = 'unpaid';
CREATE TRIGGER trg_bvrooms_bookings_upd BEFORE UPDATE ON bvrooms_bookings FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- hotel_reservations: satu mesin status untuk kamar hotel & unit apartemen; tautan ke booking BVRooms
ALTER TABLE hotel_reservations ALTER COLUMN room_type_id DROP NOT NULL;
ALTER TABLE hotel_reservations
  ADD COLUMN IF NOT EXISTS unit_type_id       uuid REFERENCES bvrooms_unit_types(id),
  ADD COLUMN IF NOT EXISTS unit_location_id   uuid REFERENCES units(location_id),
  ADD COLUMN IF NOT EXISTS bvrooms_booking_id uuid REFERENCES bvrooms_bookings(id);
ALTER TABLE hotel_reservations ADD CONSTRAINT hotel_reservations_type_xor CHECK ((room_type_id IS NULL) <> (unit_type_id IS NULL));
ALTER TABLE hotel_reservations DROP CONSTRAINT IF EXISTS hotel_reservations_source_check;
ALTER TABLE hotel_reservations ADD CONSTRAINT hotel_reservations_source_check CHECK (source IN ('walk_in','phone','email','website','corporate','bvrooms','other'));
-- satu unit apartemen tidak boleh disewa ganda pada malam yang sama (setara DoD #42 untuk kamar)
ALTER TABLE hotel_reservations ADD CONSTRAINT excl_hotel_res_unit_stay
  EXCLUDE USING gist (unit_location_id WITH =, stay WITH &&) WHERE (unit_location_id IS NOT NULL AND status IN ('new','confirmed','checked_in'));
CREATE INDEX idx_hotel_res_unit_type_stay ON hotel_reservations USING gist (unit_type_id, stay) WHERE unit_type_id IS NOT NULL AND status IN ('new','confirmed','checked_in');
CREATE INDEX idx_hotel_res_bvrooms_booking ON hotel_reservations (bvrooms_booking_id) WHERE bvrooms_booking_id IS NOT NULL;

CREATE TABLE bvrooms_booking_rooms (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  booking_id      uuid NOT NULL REFERENCES bvrooms_bookings(id) ON DELETE CASCADE,
  reservation_id  uuid NOT NULL UNIQUE REFERENCES hotel_reservations(id),
  room_type_id    uuid REFERENCES hotel_room_types(id),
  unit_type_id    uuid REFERENCES bvrooms_unit_types(id),
  type_name       text NOT NULL,
  adults          int NOT NULL DEFAULT 1,
  children        int NOT NULL DEFAULT 0,
  rate_per_night  bigint NOT NULL DEFAULT 0,
  nights          int NOT NULL DEFAULT 1,
  discount_amount bigint NOT NULL DEFAULT 0,
  line_total      bigint NOT NULL DEFAULT 0,
  sort_order      int NOT NULL DEFAULT 0
);
CREATE INDEX idx_bvrooms_booking_rooms_booking ON bvrooms_booking_rooms (booking_id);

CREATE TABLE bvrooms_booking_addons (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  booking_room_id uuid NOT NULL REFERENCES bvrooms_booking_rooms(id) ON DELETE CASCADE,
  addon_id        uuid REFERENCES bvrooms_addons(id) ON DELETE SET NULL,
  name            text NOT NULL,
  kind            text NOT NULL,
  pricing_unit    text NOT NULL,
  qty             int NOT NULL DEFAULT 1,
  unit_price      bigint NOT NULL DEFAULT 0,
  line_total      bigint NOT NULL DEFAULT 0
);
CREATE INDEX idx_bvrooms_booking_addons_room ON bvrooms_booking_addons (booking_room_id);

-- ---------- pembayaran (provider-agnostic; fase hold: manual / pay_at_property) ----------
CREATE TABLE bvrooms_payments (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  booking_id         uuid NOT NULL REFERENCES bvrooms_bookings(id) ON DELETE CASCADE,
  provider_code      text NOT NULL,                               -- manual | mock_gateway | (midtrans|xendit)
  method_code        text NOT NULL,                               -- transfer_bca | cash_on_site | bca_va ...
  amount             bigint NOT NULL,
  status             text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','proof_submitted','paid','expired','failed','cancelled')),
  instructions       jsonb NOT NULL DEFAULT '{}'::jsonb,
  external_ref       text,
  va_number          text,
  proof_storage_key  text,
  proof_content_type text,
  proof_submitted_at timestamptz,
  verified_by        uuid,
  verified_at        timestamptz,
  paid_at            timestamptz,
  expires_at         timestamptz,
  note               text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  version            int NOT NULL DEFAULT 1
);
CREATE INDEX idx_bvrooms_payments_booking ON bvrooms_payments (booking_id, created_at DESC);
CREATE INDEX idx_bvrooms_payments_pending ON bvrooms_payments (organization_id, status) WHERE status IN ('pending','proof_submitted');
CREATE TRIGGER trg_bvrooms_payments_upd BEFORE UPDATE ON bvrooms_payments FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------- review (bintang saja), wishlist, inbox, push ----------
CREATE TABLE bvrooms_reviews (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id) ON DELETE CASCADE,
  booking_id      uuid NOT NULL UNIQUE REFERENCES bvrooms_bookings(id) ON DELETE CASCADE,
  customer_id     uuid NOT NULL REFERENCES bvrooms_customers(id),
  stars           smallint NOT NULL CHECK (stars BETWEEN 1 AND 5),
  display_name    text NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_bvrooms_reviews_property ON bvrooms_reviews (property_id, created_at DESC);

CREATE TABLE bvrooms_wishlists (
  organization_id uuid NOT NULL REFERENCES organizations(id),
  customer_id     uuid NOT NULL REFERENCES bvrooms_customers(id) ON DELETE CASCADE,
  property_id     uuid NOT NULL REFERENCES properties(location_id) ON DELETE CASCADE,
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (customer_id, property_id)
);

CREATE TABLE bvrooms_notifications (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  customer_id       uuid NOT NULL REFERENCES bvrooms_customers(id) ON DELETE CASCADE,
  type              text NOT NULL,                                -- booking_created | payment_received | payment_expired | ...
  title             text NOT NULL,
  body              text NOT NULL,
  booking_id        uuid REFERENCES bvrooms_bookings(id) ON DELETE SET NULL,
  severity          text NOT NULL DEFAULT 'info' CHECK (severity IN ('success','danger','info')),
  created_at        timestamptz NOT NULL DEFAULT now(),
  read_at           timestamptz,
  deleted_at        timestamptz,
  delivered_push_at timestamptz
);
CREATE INDEX idx_bvrooms_notifications_customer ON bvrooms_notifications (customer_id, created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE bvrooms_push_subscriptions (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  customer_id     uuid NOT NULL REFERENCES bvrooms_customers(id) ON DELETE CASCADE,
  endpoint        text NOT NULL UNIQUE,
  p256dh          text NOT NULL,
  auth            text NOT NULL,
  user_agent      text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz
);
CREATE INDEX idx_bvrooms_push_customer ON bvrooms_push_subscriptions (customer_id) WHERE revoked_at IS NULL;

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['bvrooms_customers','bvrooms_otp_codes','bvrooms_sessions','bvrooms_unit_types','bvrooms_property_listings','bvrooms_property_photos',
    'bvrooms_banners','bvrooms_promotions','bvrooms_addons','bvrooms_bookings','bvrooms_booking_rooms','bvrooms_booking_addons','bvrooms_payments',
    'bvrooms_reviews','bvrooms_wishlists','bvrooms_notifications','bvrooms_push_subscriptions'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE hotel_reservations DROP CONSTRAINT IF EXISTS excl_hotel_res_unit_stay;
ALTER TABLE hotel_reservations DROP CONSTRAINT IF EXISTS hotel_reservations_type_xor;
DROP INDEX IF EXISTS idx_hotel_res_unit_type_stay;
DROP INDEX IF EXISTS idx_hotel_res_bvrooms_booking;
DROP TABLE IF EXISTS bvrooms_push_subscriptions, bvrooms_notifications, bvrooms_wishlists, bvrooms_reviews, bvrooms_payments,
  bvrooms_booking_addons, bvrooms_booking_rooms CASCADE;
ALTER TABLE hotel_reservations DROP COLUMN IF EXISTS bvrooms_booking_id, DROP COLUMN IF EXISTS unit_location_id, DROP COLUMN IF EXISTS unit_type_id;
DELETE FROM hotel_reservations WHERE room_type_id IS NULL;
ALTER TABLE hotel_reservations ALTER COLUMN room_type_id SET NOT NULL;
ALTER TABLE hotel_reservations DROP CONSTRAINT IF EXISTS hotel_reservations_source_check;
ALTER TABLE hotel_reservations ADD CONSTRAINT hotel_reservations_source_check CHECK (source IN ('walk_in','phone','email','website','corporate','other'));
DROP TABLE IF EXISTS bvrooms_bookings, bvrooms_addons, bvrooms_promotions, bvrooms_banners, bvrooms_property_photos, bvrooms_property_listings CASCADE;
ALTER TABLE hotel_rates DROP CONSTRAINT IF EXISTS hotel_rates_type_xor;
ALTER TABLE hotel_rates DROP COLUMN IF EXISTS unit_type_id;
DELETE FROM hotel_rates WHERE room_type_id IS NULL;
ALTER TABLE hotel_rates ALTER COLUMN room_type_id SET NOT NULL;
ALTER TABLE units DROP COLUMN IF EXISTS bvrooms_unit_type_id, DROP COLUMN IF EXISTS rentable_daily;
DROP TABLE IF EXISTS bvrooms_unit_types, bvrooms_sessions, bvrooms_otp_codes, bvrooms_customers CASCADE;
-- +goose StatementEnd
