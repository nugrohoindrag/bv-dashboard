-- +goose Up
-- +goose StatementBegin
-- ============ APARTMENT UNIT SALES & RENTAL MANAGEMENT (PRD P1 v1.3 §3.10, WF-P1-008/009, AT-P1-000D/E; NC §71 Commercial) ============
-- Unit dalam portfolio property (guardrail #22: bukan public multi-property marketplace). Hanya aktif pada profile Apartment
-- (capability unit_sales / unit_rental). Inventori = units (One Building Data Model); listing = ekstensi komersial per unit.

-- ---------- UNIT SALES ----------
CREATE TABLE unit_listings (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  property_id       uuid NOT NULL REFERENCES properties(location_id),
  listing_code      text NOT NULL,                                 -- LIST-2026-000001
  unit_location_id  uuid NOT NULL REFERENCES units(location_id),
  title             text NOT NULL,
  description       text,
  asking_price      bigint NOT NULL DEFAULT 0,                     -- IDR
  currency_code     char(3) NOT NULL DEFAULT 'IDR',
  price_negotiable  boolean NOT NULL DEFAULT false,
  bedrooms          int,
  bathrooms         int,
  area_m2           numeric(10,2),
  furnishing        text CHECK (furnishing IN ('unfurnished','semi_furnished','furnished')),
  features          text[] NOT NULL DEFAULT '{}',
  -- Sales inventory status (PRD §3.10 unit availability/status): Draft | Published (available) | Reserved | Sold | Archived
  status            text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','reserved','sold','archived')),
  documents         jsonb NOT NULL DEFAULT '[]',                   -- document/reference tracking [{name, reference, attachment_id, note}]
  published_at      timestamptz,
  sold_at           timestamptz,
  archived_at       timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  created_by        uuid,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid,
  version           int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, listing_code)
);
-- satu listing aktif per unit
CREATE UNIQUE INDEX uq_unit_listings_active ON unit_listings (unit_location_id) WHERE status IN ('draft','published','reserved');
CREATE INDEX idx_unit_listings_property ON unit_listings (organization_id, property_id, status);
CREATE TRIGGER trg_unit_listings_upd BEFORE UPDATE ON unit_listings FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- Prospective buyer / customer record + sales inquiry + pipeline
CREATE TABLE unit_sales_leads (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  property_id       uuid NOT NULL REFERENCES properties(location_id),
  lead_code         text NOT NULL,                                 -- LEAD-2026-000001
  listing_id        uuid REFERENCES unit_listings(id),             -- unit yang diminati (boleh kosong: inquiry umum)
  full_name         text NOT NULL,
  phone             text,
  email             text,
  company           text,
  source            text NOT NULL DEFAULT 'walk_in' CHECK (source IN ('walk_in','phone','email','website','referral','agent','other')),
  budget_min        bigint,
  budget_max        bigint,
  inquiry           text,                                          -- isi sales inquiry awal
  -- Pipeline (Onboarding Brief §16): New → Contacted → Qualified → Reserved → Sold; Lost/Cancelled
  status            text NOT NULL DEFAULT 'new' CHECK (status IN ('new','contacted','qualified','reserved','sold','lost','cancelled')),
  assigned_to       uuid REFERENCES users(id),
  next_follow_up_at timestamptz,
  last_activity_at  timestamptz,
  lost_reason       text,
  notes             text,
  created_at        timestamptz NOT NULL DEFAULT now(),
  created_by        uuid,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid,
  version           int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, lead_code)
);
CREATE INDEX idx_unit_sales_leads_property ON unit_sales_leads (organization_id, property_id, status, created_at);
CREATE INDEX idx_unit_sales_leads_listing ON unit_sales_leads (listing_id);
CREATE TRIGGER trg_unit_sales_leads_upd BEFORE UPDATE ON unit_sales_leads FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- Sales activity / history (PRD §3.10)
CREATE TABLE unit_sales_lead_activities (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  lead_id           uuid NOT NULL REFERENCES unit_sales_leads(id) ON DELETE CASCADE,
  activity_type     text NOT NULL CHECK (activity_type IN ('note','call','meeting','site_visit','email','whatsapp','status_change','document','reservation')),
  summary           text NOT NULL,
  occurred_at       timestamptz NOT NULL DEFAULT now(),
  created_by        uuid,
  created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_unit_sales_lead_activities ON unit_sales_lead_activities (lead_id, occurred_at DESC);

-- Unit Reservation (sale): WF-P1-008 Inquiry → Unit Reservation → Sales Status → Handover
CREATE TABLE unit_sale_reservations (
  id                  uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id     uuid NOT NULL REFERENCES organizations(id),
  property_id         uuid NOT NULL REFERENCES properties(location_id),
  reservation_number  text NOT NULL,                               -- SRES-2026-000001
  listing_id          uuid NOT NULL REFERENCES unit_listings(id),
  unit_location_id    uuid NOT NULL REFERENCES units(location_id),
  lead_id             uuid NOT NULL REFERENCES unit_sales_leads(id),
  agreed_price        bigint NOT NULL DEFAULT 0,
  booking_fee         bigint NOT NULL DEFAULT 0,
  currency_code       char(3) NOT NULL DEFAULT 'IDR',
  reserved_until      date,                                        -- masa berlaku reservasi (opsional)
  -- Reserved → Contract Signed (opsional) → Sold → Handed Over | Cancelled
  status              text NOT NULL DEFAULT 'reserved' CHECK (status IN ('reserved','contract_signed','sold','handed_over','cancelled')),
  contract_reference  text,
  documents           jsonb NOT NULL DEFAULT '[]',
  notes               text,
  invoice_id          uuid REFERENCES invoices(id),                -- booking fee / pelunasan (Billing linkage, opsional)
  tenant_id           uuid REFERENCES tenants(id),                 -- pemilik setelah handover (Tenant/Occupant bersama)
  occupant_id         uuid REFERENCES occupants(id),
  tenant_user_id      uuid REFERENCES users(id),                   -- akun Tenant App pemilik (opsional)
  contract_signed_at  timestamptz,
  sold_at             timestamptz,
  handed_over_at      timestamptz,
  cancelled_at        timestamptz,
  cancel_reason       text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  created_by          uuid,
  updated_at          timestamptz NOT NULL DEFAULT now(),
  updated_by          uuid,
  version             int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, reservation_number)
);
-- satu reservasi aktif per unit (DoD: unit tidak bisa direservasi ganda)
CREATE UNIQUE INDEX uq_unit_sale_res_active ON unit_sale_reservations (unit_location_id) WHERE status IN ('reserved','contract_signed','sold');
CREATE INDEX idx_unit_sale_res_property ON unit_sale_reservations (organization_id, property_id, status, created_at);
CREATE INDEX idx_unit_sale_res_lead ON unit_sale_reservations (lead_id);
CREATE TRIGGER trg_unit_sale_reservations_upd BEFORE UPDATE ON unit_sale_reservations FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------- UNIT RENTAL ----------
CREATE TABLE unit_rental_listings (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  property_id       uuid NOT NULL REFERENCES properties(location_id),
  listing_code      text NOT NULL,                                 -- RLIST-2026-000001
  unit_location_id  uuid NOT NULL REFERENCES units(location_id),
  title             text NOT NULL,
  description       text,
  -- rental rate configuration (PRD §3.10): NULL = periode tidak ditawarkan
  rate_daily        bigint,
  rate_weekly       bigint,
  rate_monthly      bigint,
  deposit_amount    bigint NOT NULL DEFAULT 0,
  currency_code     char(3) NOT NULL DEFAULT 'IDR',
  min_stay_days     int NOT NULL DEFAULT 1 CHECK (min_stay_days > 0),
  max_occupants     int,
  bedrooms          int,
  bathrooms         int,
  area_m2           numeric(10,2),
  furnishing        text CHECK (furnishing IN ('unfurnished','semi_furnished','furnished')),
  features          text[] NOT NULL DEFAULT '{}',
  available_from    date,
  available_until   date,
  status            text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
  documents         jsonb NOT NULL DEFAULT '[]',
  published_at      timestamptz,
  archived_at       timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  created_by        uuid,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid,
  version           int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, listing_code),
  CHECK (rate_daily IS NOT NULL OR rate_weekly IS NOT NULL OR rate_monthly IS NOT NULL),
  CHECK (available_until IS NULL OR available_from IS NULL OR available_until >= available_from)
);
CREATE UNIQUE INDEX uq_unit_rental_listings_active ON unit_rental_listings (unit_location_id) WHERE status IN ('draft','published');
CREATE INDEX idx_unit_rental_listings_property ON unit_rental_listings (organization_id, property_id, status);
CREATE TRIGGER trg_unit_rental_listings_upd BEFORE UPDATE ON unit_rental_listings FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- Rental Reservation (domain-specific Reservation, TD-P1-004): WF-P1-009 Rental Period → Availability → Booking → Tenant Onboarding → Active Rental
CREATE TABLE unit_rental_reservations (
  id                  uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id     uuid NOT NULL REFERENCES organizations(id),
  property_id         uuid NOT NULL REFERENCES properties(location_id),
  reservation_number  text NOT NULL,                               -- RRES-2026-000001
  listing_id          uuid NOT NULL REFERENCES unit_rental_listings(id),
  unit_location_id    uuid NOT NULL REFERENCES units(location_id),
  prospect_name       text NOT NULL,
  prospect_phone      text,
  prospect_email      text,
  company             text,
  occupants           int NOT NULL DEFAULT 1,
  rental_period       text NOT NULL CHECK (rental_period IN ('daily','weekly','monthly')),
  period_count        int NOT NULL CHECK (period_count > 0),      -- mis. 3 (bulan)
  start_date          date NOT NULL,
  end_date            date NOT NULL,                               -- dihitung service: start + count × periode
  stay                daterange GENERATED ALWAYS AS (daterange(start_date, end_date, '[)')) STORED,
  rate_amount         bigint NOT NULL DEFAULT 0,                   -- rate per periode saat dipesan (snapshot)
  total_amount        bigint NOT NULL DEFAULT 0,
  deposit_amount      bigint NOT NULL DEFAULT 0,
  currency_code       char(3) NOT NULL DEFAULT 'IDR',
  -- Onboarding Brief §16 Unit Rental: New (inquiry) → Reserved → Active → Completed | Cancelled
  status              text NOT NULL DEFAULT 'new' CHECK (status IN ('new','reserved','active','completed','cancelled')),
  source              text NOT NULL DEFAULT 'walk_in' CHECK (source IN ('walk_in','phone','email','website','referral','agent','tenant_app','other')),
  special_requests    text,
  notes               text,                                        -- internal
  invoice_id          uuid REFERENCES invoices(id),                -- Billing linkage (sewa + deposit)
  tenant_id           uuid REFERENCES tenants(id),                 -- Tenant Onboarding (Tenant/Occupant bersama)
  occupant_id         uuid REFERENCES occupants(id),
  tenant_user_id      uuid REFERENCES users(id),                   -- akun Tenant App penyewa (opsional)
  reserved_at         timestamptz,
  activated_at        timestamptz,
  completed_at        timestamptz,
  cancelled_at        timestamptz,
  cancel_reason       text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  created_by          uuid,
  updated_at          timestamptz NOT NULL DEFAULT now(),
  updated_by          uuid,
  version             int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, reservation_number),
  CHECK (end_date > start_date),
  -- Konflik periode dicegah di DB (TD-P1-009: inquiry 'new' tidak memblokir; 'reserved'/'active' memblokir)
  EXCLUDE USING gist (unit_location_id WITH =, stay WITH &&) WHERE (status IN ('reserved','active'))
);
CREATE INDEX idx_unit_rental_res_list ON unit_rental_reservations (organization_id, property_id, status, start_date);
CREATE INDEX idx_unit_rental_res_unit_stay ON unit_rental_reservations USING gist (unit_location_id, stay) WHERE status IN ('reserved','active');
CREATE TRIGGER trg_unit_rental_reservations_upd BEFORE UPDATE ON unit_rental_reservations FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- Tenant App account dari serah terima unit (pemilik) — sumber registrasi
ALTER TABLE tenant_users DROP CONSTRAINT IF EXISTS tenant_users_registration_source_check;
ALTER TABLE tenant_users ADD CONSTRAINT tenant_users_registration_source_check CHECK (registration_source IN ('self','staff','import','rental_onboarding','hotel_checkin','sale_handover'));

-- Billing linkage: sumber invoice untuk booking fee penjualan unit (rental memakai 'rental' yang sudah ada)
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_source_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_source_check CHECK (source IN ('manual','import','rental','hotel','facility','unit_sale'));

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['unit_listings','unit_sales_leads','unit_sales_lead_activities','unit_sale_reservations','unit_rental_listings','unit_rental_reservations'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS unit_rental_reservations, unit_rental_listings, unit_sale_reservations, unit_sales_lead_activities, unit_sales_leads, unit_listings CASCADE;
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_source_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_source_check CHECK (source IN ('manual','import','rental','hotel','facility'));
ALTER TABLE tenant_users DROP CONSTRAINT IF EXISTS tenant_users_registration_source_check;
ALTER TABLE tenant_users ADD CONSTRAINT tenant_users_registration_source_check CHECK (registration_source IN ('self','staff','import','rental_onboarding','hotel_checkin'));
-- +goose StatementEnd
