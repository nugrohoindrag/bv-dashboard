-- +goose Up
-- +goose StatementBegin
-- ============ PROPERTY PROFILE (PRD P1 v1.3 §3, Onboarding Brief §7/§20, NC v2.0 §7; TD-P1-001) ============
-- Profile melekat pada Property (tepat satu profile aktif). property_type lama dipertahankan sebagai deskriptor.
ALTER TABLE properties
  ADD COLUMN IF NOT EXISTS profile            text NOT NULL DEFAULT 'office' CHECK (profile IN ('hotel','apartment','office')),
  ADD COLUMN IF NOT EXISTS status             text NOT NULL DEFAULT 'active' CHECK (status IN ('draft','active','inactive')),
  ADD COLUMN IF NOT EXISTS profile_changed_at timestamptz,
  ADD COLUMN IF NOT EXISTS profile_changed_by uuid;

-- Backfill dari property_type lama (PS-002: tidak boleh null setelah aktif)
UPDATE properties SET profile = CASE
  WHEN lower(coalesce(property_type,'')) IN ('hotel','serviced_hotel','boutique_hotel') THEN 'hotel'
  WHEN lower(coalesce(property_type,'')) IN ('apartment','residential','serviced_apartment','residence') THEN 'apartment'
  ELSE 'office' END;

CREATE INDEX IF NOT EXISTS idx_properties_profile ON properties (organization_id, profile);

-- Konfigurasi per property (profile hanya mengubah terminologi/aturan, bukan canonical entity — PRD §3.11)
CREATE TABLE property_profile_configs (
  property_id                  uuid PRIMARY KEY REFERENCES properties(location_id) ON DELETE CASCADE,
  organization_id              uuid NOT NULL REFERENCES organizations(id),
  terminology                  jsonb NOT NULL DEFAULT '{}'::jsonb,   -- override label: {"customer": {"id": "...", "en": "..."}}
  expose_sla_to_tenant         boolean NOT NULL DEFAULT false,       -- OD-P1-004
  tenant_confirmation_required boolean NOT NULL DEFAULT false,       -- OD-P1-005: tenant wajib confirm sebelum Close
  csat_enabled                 boolean NOT NULL DEFAULT true,        -- OD-P1-006
  booking_approval_required    boolean NOT NULL DEFAULT false,       -- OD-P1-007
  visitor_approval_required    boolean NOT NULL DEFAULT false,       -- OD-P1-008
  tenant_self_registration     boolean NOT NULL DEFAULT true,
  auto_close_resolved_hours    int NOT NULL DEFAULT 72,              -- SR resolved tanpa respons tenant → closed
  settings                     jsonb NOT NULL DEFAULT '{}'::jsonb,   -- ekstensi (dashboard widget, KPI, dsb.)
  created_at                   timestamptz NOT NULL DEFAULT now(),
  updated_at                   timestamptz NOT NULL DEFAULT now(),
  updated_by                   uuid,
  version                      int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_property_profile_configs_upd BEFORE UPDATE ON property_profile_configs FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE property_profile_configs ENABLE ROW LEVEL SECURITY; ALTER TABLE property_profile_configs FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON property_profile_configs USING (organization_id = app_current_org());

-- Config default untuk property yang sudah ada
INSERT INTO property_profile_configs (property_id, organization_id)
SELECT location_id, organization_id FROM properties
ON CONFLICT (property_id) DO NOTHING;

-- ============ KATEGORI SERVICE REQUEST PER PROFILE (PRD §11) ============
-- Kategori tetap milik organization; `profiles` menyaring per profile property; `property_id` = override khusus property.
ALTER TABLE service_request_categories
  ADD COLUMN IF NOT EXISTS property_id    uuid REFERENCES properties(location_id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS icon           text,
  ADD COLUMN IF NOT EXISTS profiles       text[] NOT NULL DEFAULT '{hotel,apartment,office}',
  ADD COLUMN IF NOT EXISTS tenant_visible boolean NOT NULL DEFAULT true;
ALTER TABLE service_request_categories DROP CONSTRAINT IF EXISTS service_request_categories_organization_id_code_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_sr_categories ON service_request_categories (organization_id, COALESCE(property_id,'00000000-0000-0000-0000-000000000000'::uuid), code);

-- Service request: dukung pelapor tenant (P1.1) — kolom disiapkan agar kompatibel ke belakang
ALTER TABLE service_requests
  ADD COLUMN IF NOT EXISTS tenant_user_id     uuid REFERENCES users(id),
  ADD COLUMN IF NOT EXISTS area_scope         text CHECK (area_scope IN ('unit','common_area','other')),
  ADD COLUMN IF NOT EXISTS reopen_count       int NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS confirmed_at       timestamptz,
  ADD COLUMN IF NOT EXISTS due_estimate_at    timestamptz,
  ADD COLUMN IF NOT EXISTS priority_override_reason text;
CREATE INDEX IF NOT EXISTS idx_sr_tenant_user ON service_requests (tenant_user_id) WHERE tenant_user_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_sr_tenant_user;
ALTER TABLE service_requests DROP COLUMN IF EXISTS tenant_user_id, DROP COLUMN IF EXISTS area_scope, DROP COLUMN IF EXISTS reopen_count, DROP COLUMN IF EXISTS confirmed_at, DROP COLUMN IF EXISTS due_estimate_at, DROP COLUMN IF EXISTS priority_override_reason;
DROP INDEX IF EXISTS uq_sr_categories;
ALTER TABLE service_request_categories ADD CONSTRAINT service_request_categories_organization_id_code_key UNIQUE (organization_id, code);
ALTER TABLE service_request_categories DROP COLUMN IF EXISTS property_id, DROP COLUMN IF EXISTS icon, DROP COLUMN IF EXISTS profiles, DROP COLUMN IF EXISTS tenant_visible;
DROP TABLE IF EXISTS property_profile_configs;
DROP INDEX IF EXISTS idx_properties_profile;
ALTER TABLE properties DROP COLUMN IF EXISTS profile, DROP COLUMN IF EXISTS status, DROP COLUMN IF EXISTS profile_changed_at, DROP COLUMN IF EXISTS profile_changed_by;
-- +goose StatementEnd
