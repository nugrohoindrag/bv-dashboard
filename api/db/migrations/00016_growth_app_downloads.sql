-- +goose Up
-- +goose StatementBegin
-- ============ WEBSITE, SELF-SERVE ONBOARDING, FREE TRIAL & APP DOWNLOADS (Website PRD v1.1 §15–§22, §24–§32, §42) ============

-- ---------- Organization: trial lifecycle (§31) + organization internal BuildingVision (§18 admin-internal) ----------
ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS is_internal        boolean NOT NULL DEFAULT false,      -- organization internal BuildingVision (role admin_internal hanya di sini)
  ADD COLUMN IF NOT EXISTS trial_status       text NOT NULL DEFAULT 'none' CHECK (trial_status IN ('none','trial','trial_ending_soon','trial_expired','converted','cancelled')),
  ADD COLUMN IF NOT EXISTS trial_started_at   timestamptz,
  ADD COLUMN IF NOT EXISTS trial_ends_at      timestamptz,
  ADD COLUMN IF NOT EXISTS trial_converted_at timestamptz,
  ADD COLUMN IF NOT EXISTS trial_cancelled_at timestamptz,
  ADD COLUMN IF NOT EXISTS plan_code          text,                                -- plan yang dipilih saat convert (starter|growth|enterprise)
  ADD COLUMN IF NOT EXISTS signup_source      text;                                -- website | sales | seed
CREATE INDEX IF NOT EXISTS idx_organizations_trial ON organizations (trial_status, trial_ends_at) WHERE trial_status IN ('trial','trial_ending_soon');

-- ---------- Email verification untuk user (§25 Verify Email; §41) ----------
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified_at timestamptz;

-- ---------- Pending signup (§25): akun dibuat setelah email terverifikasi ----------
-- Global (tanpa organization) karena organization baru dibuat saat verifikasi. Tidak ber-RLS; hanya diakses service growth.
CREATE TABLE signups (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  email             text NOT NULL,
  full_name         text NOT NULL,
  password_hash     text NOT NULL,
  organization_name text NOT NULL,
  phone             text,
  token_hash        text NOT NULL UNIQUE,                 -- sha256(token verifikasi)
  expires_at        timestamptz NOT NULL,
  verified_at       timestamptz,
  organization_id   uuid REFERENCES organizations(id),
  user_id           uuid,
  source_page       text,
  ip                inet,
  user_agent        text,
  resend_count      int NOT NULL DEFAULT 0,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_signups_email ON signups (lower(email), created_at DESC);
CREATE INDEX idx_signups_expires ON signups (expires_at) WHERE verified_at IS NULL;

-- ---------- App Downloads (§15–§22, §42–§44): konfigurasi global (bukan per organization) ----------
CREATE TABLE app_downloads (
  id            uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  name          text NOT NULL,                                                  -- BuildingVision Staff App
  app_type      text NOT NULL CHECK (app_type IN ('staff','tenant')),
  platform      text NOT NULL CHECK (platform IN ('android','ios','other')),
  download_url  text NOT NULL,                                                  -- Google Drive URL (divalidasi di service)
  status        text NOT NULL DEFAULT 'inactive' CHECK (status IN ('active','inactive')),
  notes         text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  created_by    uuid,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  updated_by    uuid,
  version       int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_app_downloads_upd BEFORE UPDATE ON app_downloads FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
CREATE INDEX idx_app_downloads_active ON app_downloads (app_type, platform) WHERE status = 'active';

-- ---------- Demo requests (§34 Book a Demo) ----------
CREATE TABLE demo_requests (
  id            uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  full_name     text NOT NULL,
  email         text NOT NULL,
  company       text,
  phone         text,
  property_profile text CHECK (property_profile IN ('hotel','apartment','office')),
  property_count   int,
  message       text,
  source_page   text,
  status        text NOT NULL DEFAULT 'new' CHECK (status IN ('new','contacted','scheduled','closed')),
  ip            inet,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_demo_requests_created ON demo_requests (created_at DESC);

-- ---------- Funnel analytics (§40): event ringan tanpa data pribadi ----------
CREATE TABLE growth_events (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  event           text NOT NULL,                       -- page_viewed | start_trial_clicked | app_download_clicked | ...
  organization_id uuid,                                -- diisi untuk event pasca-signup
  user_id         uuid,
  anonymous_id    text,                                -- id acak sisi klien (bukan cookie pihak ketiga)
  source_page     text,
  properties      jsonb NOT NULL DEFAULT '{}'::jsonb,  -- {app, platform, solution, plan, ...}
  occurred_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_growth_events_event_time ON growth_events (event, occurred_at DESC);
CREATE INDEX idx_growth_events_org ON growth_events (organization_id, occurred_at DESC) WHERE organization_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS growth_events, demo_requests, app_downloads, signups CASCADE;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
ALTER TABLE organizations
  DROP COLUMN IF EXISTS is_internal, DROP COLUMN IF EXISTS trial_status, DROP COLUMN IF EXISTS trial_started_at, DROP COLUMN IF EXISTS trial_ends_at,
  DROP COLUMN IF EXISTS trial_converted_at, DROP COLUMN IF EXISTS trial_cancelled_at, DROP COLUMN IF EXISTS plan_code, DROP COLUMN IF EXISTS signup_source;
-- +goose StatementEnd
