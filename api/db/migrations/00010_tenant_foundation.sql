-- +goose Up
-- +goose StatementBegin
-- ============ TENANT FOUNDATION (PRD P1 v1.3 §7, §29, §30; TD-P1-003) ============
-- TenantUser: akun Mobile Tenant = users (IAM) + relasi tenant/occupant/property + status validasi.
CREATE TABLE tenant_users (
  id                  uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id     uuid NOT NULL REFERENCES organizations(id),
  user_id             uuid NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  property_id         uuid NOT NULL REFERENCES properties(location_id),
  tenant_id           uuid REFERENCES tenants(id),
  occupant_id         uuid REFERENCES occupants(id),
  role                text NOT NULL DEFAULT 'tenant_user' CHECK (role IN ('tenant_user','tenant_admin')),
  status              text NOT NULL DEFAULT 'pending_validation' CHECK (status IN ('pending_validation','active','rejected','suspended')),
  registration_source text NOT NULL DEFAULT 'self' CHECK (registration_source IN ('self','staff','import','rental_onboarding','hotel_checkin')),
  ownership_status    text CHECK (ownership_status IN ('owner','tenant','family','guest','employee')),
  requested_at        timestamptz NOT NULL DEFAULT now(),
  validated_at        timestamptz,
  validated_by        uuid,
  rejection_reason    text,
  suspended_at        timestamptz,
  suspended_by        uuid,
  suspension_reason   text,
  last_seen_at        timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now(),
  created_by          uuid,
  updated_at          timestamptz NOT NULL DEFAULT now(),
  updated_by          uuid,
  version             int NOT NULL DEFAULT 1
);
CREATE INDEX idx_tenant_users_property_status ON tenant_users (organization_id, property_id, status);
CREATE INDEX idx_tenant_users_tenant ON tenant_users (tenant_id);
CREATE TRIGGER trg_tenant_users_upd BEFORE UPDATE ON tenant_users FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- TenantAccess: scope akses tenant user ke unit / area (PRD §6.2–6.3; guardrail #2, #4, #11). Sumber isolasi server-side.
CREATE TABLE tenant_access (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  tenant_user_id  uuid NOT NULL REFERENCES tenant_users(id) ON DELETE CASCADE,
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  location_id     uuid NOT NULL REFERENCES locations(id),        -- unit (utama) atau area/space yang diizinkan khusus
  access_type     text NOT NULL CHECK (access_type IN ('unit','area')),
  is_primary      boolean NOT NULL DEFAULT false,
  status          text NOT NULL DEFAULT 'active' CHECK (status IN ('pending','active','revoked')),
  valid_from      date,
  valid_until     date,
  granted_by      uuid,
  granted_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz,
  revoked_by      uuid,
  UNIQUE (tenant_user_id, location_id)
);
CREATE INDEX idx_tenant_access_user ON tenant_access (tenant_user_id) WHERE status = 'active';
CREATE INDEX idx_tenant_access_location ON tenant_access (location_id) WHERE status = 'active';

-- Authorized common area (OD-P1-003): area/space yang boleh dilaporkan tenant tanpa akses khusus
ALTER TABLE locations ADD COLUMN IF NOT EXISTS tenant_reportable boolean NOT NULL DEFAULT true;

-- ServiceRequestMessage: komunikasi tenant ↔ building management, TERPISAH dari comments internal (guardrail #6, #8; TD-P1-006)
CREATE TABLE service_request_messages (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  service_request_id uuid NOT NULL REFERENCES service_requests(id) ON DELETE CASCADE,
  author_user_id     uuid REFERENCES users(id),
  author_kind        text NOT NULL CHECK (author_kind IN ('tenant','staff','system')),
  body               text NOT NULL,
  attachment_ids     uuid[] NOT NULL DEFAULT '{}',
  created_at         timestamptz NOT NULL DEFAULT now(),
  read_by_tenant_at  timestamptz,
  read_by_staff_at   timestamptz
);
CREATE INDEX idx_sr_messages_sr ON service_request_messages (service_request_id, created_at);

-- ServiceRequestFeedback: CSAT setelah Closed (PRD §18)
CREATE TABLE service_request_feedback (
  service_request_id uuid PRIMARY KEY REFERENCES service_requests(id) ON DELETE CASCADE,
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  property_id        uuid NOT NULL REFERENCES properties(location_id),
  tenant_user_id     uuid REFERENCES users(id),
  rating             int NOT NULL CHECK (rating BETWEEN 1 AND 5),
  comment            text,
  created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_sr_feedback_property ON service_request_feedback (organization_id, property_id, created_at DESC);

-- Announcement: komunikasi Tenant Relation → tenant (PRD §3.5 Communication; Home "Important Announcement")
CREATE TABLE announcements (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid REFERENCES properties(location_id),      -- NULL = seluruh organization
  title           text NOT NULL,
  excerpt         text,
  body            text NOT NULL,
  audience        text NOT NULL DEFAULT 'tenant' CHECK (audience IN ('tenant','staff','all')),
  importance      text NOT NULL DEFAULT 'normal' CHECK (importance IN ('normal','important')),
  image_attachment_id uuid,
  status          text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
  published_at    timestamptz,
  expires_at      timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1
);
CREATE INDEX idx_announcements_list ON announcements (organization_id, property_id, status, published_at DESC);
CREATE TRIGGER trg_announcements_upd BEFORE UPDATE ON announcements FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- Field opsional laporan tenant (PRD §12): contact preference, preferred visit time, additional note
ALTER TABLE service_requests
  ADD COLUMN IF NOT EXISTS contact_preference text,
  ADD COLUMN IF NOT EXISTS preferred_visit_at timestamptz,
  ADD COLUMN IF NOT EXISTS additional_note text;

-- Channel SR dari Tenant App; source activity/comment dari Tenant App
ALTER TABLE service_requests DROP CONSTRAINT IF EXISTS service_requests_channel_check;
ALTER TABLE service_requests ADD CONSTRAINT service_requests_channel_check CHECK (channel IN ('staff','phone','walk_in','public_intake','email','whatsapp','tenant_app'));
ALTER TABLE activities DROP CONSTRAINT IF EXISTS activities_source_check;
ALTER TABLE activities ADD CONSTRAINT activities_source_check CHECK (source IN ('web','mobile','system','sync','tenant_app'));
ALTER TABLE comments DROP CONSTRAINT IF EXISTS comments_source_check;
ALTER TABLE comments ADD CONSTRAINT comments_source_check CHECK (source IN ('web','mobile','sync','system','tenant_app'));

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['tenant_users','tenant_access','service_request_messages','service_request_feedback','announcements'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- tenant_users dibaca saat login (sebelum org di-set) untuk menentukan status akun
DROP POLICY org_isolation ON tenant_users;
CREATE POLICY org_isolation ON tenant_users USING (organization_id = app_current_org() OR current_setting('app.auth_lookup', true) = 'on');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS announcements, service_request_feedback, service_request_messages, tenant_access, tenant_users CASCADE;
ALTER TABLE locations DROP COLUMN IF EXISTS tenant_reportable;
ALTER TABLE service_requests DROP COLUMN IF EXISTS contact_preference, DROP COLUMN IF EXISTS preferred_visit_at, DROP COLUMN IF EXISTS additional_note;
ALTER TABLE service_requests DROP CONSTRAINT IF EXISTS service_requests_channel_check;
ALTER TABLE service_requests ADD CONSTRAINT service_requests_channel_check CHECK (channel IN ('staff','phone','walk_in','public_intake','email','whatsapp'));
ALTER TABLE activities DROP CONSTRAINT IF EXISTS activities_source_check;
ALTER TABLE activities ADD CONSTRAINT activities_source_check CHECK (source IN ('web','mobile','system','sync'));
ALTER TABLE comments DROP CONSTRAINT IF EXISTS comments_source_check;
ALTER TABLE comments ADD CONSTRAINT comments_source_check CHECK (source IN ('web','mobile','sync','system'));
-- +goose StatementEnd
