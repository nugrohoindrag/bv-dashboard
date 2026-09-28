-- +goose Up
-- +goose StatementBegin
-- PRD P3 v2.1 (Roadmap v2.1 §11, §19, §25.2, §25.8 Phase 3, §29, §39.2, §39.4) — Tenant Experience: push tenant (Web Push +
-- FCM, B-10), WhatsApp manual click-to-chat pengganti email (D-P3-08), wajib ganti password, nomor WhatsApp pengelola, pengumuman
-- bertarget/terjadwal/berkategori + pelacakan baca, Package, izin parkir tenant di atas entitas P2, feedback umum, deteksi isu berulang.
-- Lihat ADR-018.

-- ---------- §6.2 Push: registrasi perangkat per platform & aplikasi (B-10, P3-PSH-01..03) ----------
-- Web Push: token = endpoint subscription; kunci p256dh/auth disimpan terpisah. FCM (APK Staff/Tenant): token FCM.
ALTER TABLE device_tokens
  ADD COLUMN app            text NOT NULL DEFAULT 'staff' CHECK (app IN ('staff','tenant')),
  ADD COLUMN push_kind      text NOT NULL DEFAULT 'fcm' CHECK (push_kind IN ('fcm','webpush')),
  ADD COLUMN webpush_p256dh text,
  ADD COLUMN webpush_auth   text,
  ADD COLUMN user_agent     text;
UPDATE device_tokens dt SET app = 'tenant' WHERE EXISTS (SELECT 1 FROM tenant_users tu WHERE tu.user_id = dt.user_id);

-- notifikasi: jejak kanal per penerima untuk log komunikasi SR (P3-TRC-03)
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS push_attempted_at timestamptz;

-- ---------- §5.13 Akun: wajib ganti password (P3-ACC-03) & nomor WhatsApp pengelola (P3-WAM-05) ----------
ALTER TABLE users ADD COLUMN must_change_password boolean NOT NULL DEFAULT false;
ALTER TABLE properties ADD COLUMN whatsapp_number text;
ALTER TABLE tenant_users DROP CONSTRAINT IF EXISTS tenant_users_registration_source_check;
ALTER TABLE tenant_users ADD CONSTRAINT tenant_users_registration_source_check
  CHECK (registration_source IN ('self','staff','import','rental_onboarding','hotel_checkin','sale_handover','tenant_admin'));

-- ---------- B-06: waktu reopen SR (reopened_30d dihitung dari waktu reopen, bukan updated_at) ----------
ALTER TABLE service_requests ADD COLUMN last_reopened_at timestamptz;
UPDATE service_requests sr SET last_reopened_at = (
  SELECT max(a.occurred_at) FROM activities a WHERE a.object_type = 'service_request' AND a.object_id = sr.id AND a.action = 'reopened')
WHERE sr.reopen_count > 0;
UPDATE service_requests SET last_reopened_at = updated_at WHERE reopen_count > 0 AND last_reopened_at IS NULL;

-- ---------- §6.4 WhatsApp manual: log setiap klik "Kirim via WhatsApp" (P3-WAM-03) ----------
CREATE TABLE manual_whatsapp_logs (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  property_id       uuid REFERENCES properties(location_id),
  object_type       text NOT NULL,
  object_id         uuid NOT NULL,
  context           text NOT NULL,                             -- account_approved | service_request | invoice | package | …
  recipient_user_id uuid REFERENCES users(id),
  recipient_name    text,
  phone             text NOT NULL,                             -- 62xxxxxxxxxx (ternormalisasi)
  message_preview   text,                                      -- tanpa password sementara
  sent_by           uuid NOT NULL REFERENCES users(id),
  sent_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_manual_whatsapp_object ON manual_whatsapp_logs (organization_id, object_type, object_id, sent_at DESC);

-- ---------- §5.10 Pengumuman: kategori News/Alert, target, jadwal, wajib konfirmasi baca (P3-ANN-02..06, P3-BRC-01) ----------
ALTER TABLE announcements
  ADD COLUMN category            text NOT NULL DEFAULT 'announcement' CHECK (category IN ('announcement','news','alert')),
  ADD COLUMN severity            text NOT NULL DEFAULT 'info' CHECK (severity IN ('info','warning','critical')),
  ADD COLUMN publish_at          timestamptz,
  ADD COLUMN requires_ack        boolean NOT NULL DEFAULT false,
  ADD COLUMN target_location_ids uuid[] NOT NULL DEFAULT '{}',   -- building/tower/floor/unit (subtree); kosong = seluruh property
  ADD COLUMN target_tenant_ids   uuid[] NOT NULL DEFAULT '{}',   -- tenant tertentu
  ADD COLUMN recipients_count    int;
ALTER TABLE announcements DROP CONSTRAINT IF EXISTS announcements_status_check;
ALTER TABLE announcements ADD CONSTRAINT announcements_status_check CHECK (status IN ('draft','scheduled','published','archived'));
CREATE INDEX idx_announcements_scheduled ON announcements (publish_at) WHERE status = 'scheduled';

CREATE TABLE announcement_reads (
  announcement_id uuid NOT NULL REFERENCES announcements(id) ON DELETE CASCADE,
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  organization_id uuid NOT NULL REFERENCES organizations(id),
  read_at         timestamptz NOT NULL DEFAULT now(),
  acknowledged_at timestamptz,
  PRIMARY KEY (announcement_id, user_id)
);

-- ---------- §5.9 Package (P3-PKG-01..04) ----------
CREATE TABLE packages (
  id                   uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id      uuid NOT NULL REFERENCES organizations(id),
  property_id          uuid NOT NULL REFERENCES properties(location_id),
  package_number       text NOT NULL,                          -- PKG-2026-000001
  unit_location_id     uuid REFERENCES locations(id),
  tenant_id            uuid REFERENCES tenants(id),
  recipient_user_id    uuid REFERENCES users(id),               -- akun tenant penerima (opsional)
  recipient_name       text NOT NULL,
  package_type         text NOT NULL DEFAULT 'parcel' CHECK (package_type IN ('document','parcel','food','large','other')),
  courier              text,
  tracking_number      text,
  description          text,
  storage_location     text,
  status               text NOT NULL DEFAULT 'received' CHECK (status IN ('received','notified','picked_up','returned')),
  received_at          timestamptz NOT NULL DEFAULT now(),
  received_by          uuid REFERENCES users(id),
  notified_at          timestamptz,
  reminder_count       int NOT NULL DEFAULT 0,
  last_reminded_at     timestamptz,
  picked_up_at         timestamptz,
  picked_up_by_name    text,
  picked_up_by_user_id uuid REFERENCES users(id),
  handed_over_by       uuid REFERENCES users(id),
  handover_note        text,
  returned_at          timestamptz,
  returned_by          uuid REFERENCES users(id),
  return_reason        text,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  updated_by           uuid,
  version              int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, package_number)
);
CREATE INDEX idx_packages_list ON packages (organization_id, property_id, status, received_at DESC);
CREATE INDEX idx_packages_unit ON packages (unit_location_id, status);
CREATE INDEX idx_packages_tenant ON packages (tenant_id, status);
CREATE TRIGGER trg_packages_upd BEFORE UPDATE ON packages FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------- §5.8 Parking tenant di atas entitas P2 (P3-PRK-01..03, D-P2-06) ----------
ALTER TABLE vehicles ADD COLUMN registered_by_tenant boolean NOT NULL DEFAULT false;
CREATE TABLE parking_permits (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  permit_number    text NOT NULL,                              -- PRM-2026-000001
  vehicle_id       uuid NOT NULL REFERENCES vehicles(id),
  tenant_id        uuid REFERENCES tenants(id),
  unit_location_id uuid REFERENCES locations(id),
  requested_by     uuid REFERENCES users(id),
  parking_area_id  uuid REFERENCES parking_areas(id),
  permit_type      text NOT NULL DEFAULT 'monthly' CHECK (permit_type IN ('monthly','annual','temporary')),
  status           text NOT NULL DEFAULT 'requested' CHECK (status IN ('requested','approved','rejected','cancelled','expired','revoked')),
  valid_from       date,
  valid_until      date,
  sticker_number   text,
  fee_amount       bigint,                                     -- tarif khusus izin (P4 tagihan parkir); NULL = tarif billing rule
  notes            text,
  decision_reason  text,
  decided_by       uuid REFERENCES users(id),
  decided_at       timestamptz,
  requested_at     timestamptz NOT NULL DEFAULT now(),
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, permit_number)
);
CREATE INDEX idx_parking_permits_list ON parking_permits (organization_id, property_id, status, requested_at DESC);
CREATE INDEX idx_parking_permits_vehicle ON parking_permits (vehicle_id, status);
CREATE TRIGGER trg_parking_permits_upd BEFORE UPDATE ON parking_permits FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------- §5.12 Feedback umum tenant → Tenant Relation (P3-FDB-02..03) ----------
CREATE TABLE tenant_feedback (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  feedback_number  text NOT NULL,                              -- FDB-2026-000001
  tenant_user_id   uuid NOT NULL REFERENCES users(id),         -- pengirim (disembunyikan dari staf bila anonim)
  tenant_id        uuid REFERENCES tenants(id),
  unit_location_id uuid REFERENCES locations(id),
  category         text NOT NULL DEFAULT 'suggestion' CHECK (category IN ('suggestion','compliment','complaint','question','other')),
  subject          text,
  body             text NOT NULL,
  is_anonymous     boolean NOT NULL DEFAULT false,
  status           text NOT NULL DEFAULT 'new' CHECK (status IN ('new','in_review','responded','closed')),
  response         text,
  responded_by     uuid REFERENCES users(id),
  responded_at     timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, feedback_number)
);
CREATE INDEX idx_tenant_feedback_list ON tenant_feedback (organization_id, property_id, status, created_at DESC);
CREATE TRIGGER trg_tenant_feedback_upd BEFORE UPDATE ON tenant_feedback FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------- §7.1 Recurring Issue Detection (P3-TSH-07) & sinyal keluhan berulang (P3-CMP-03) ----------
ALTER TABLE property_profile_configs
  ADD COLUMN recurring_issue_threshold   int NOT NULL DEFAULT 3 CHECK (recurring_issue_threshold >= 2),
  ADD COLUMN recurring_issue_window_days int NOT NULL DEFAULT 30 CHECK (recurring_issue_window_days BETWEEN 1 AND 365),
  ADD COLUMN package_reminder_days       int NOT NULL DEFAULT 3 CHECK (package_reminder_days BETWEEN 0 AND 60);
CREATE TABLE recurring_issues (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  location_id     uuid NOT NULL REFERENCES locations(id),
  category_code   text NOT NULL,
  category_id     uuid REFERENCES service_request_categories(id),
  request_count   int NOT NULL DEFAULT 0,
  complaint_count int NOT NULL DEFAULT 0,
  window_days     int NOT NULL,
  threshold       int NOT NULL,
  first_seen_at   timestamptz NOT NULL,
  last_seen_at    timestamptz NOT NULL,
  service_request_ids uuid[] NOT NULL DEFAULT '{}',
  status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open','acknowledged','resolved')),
  acknowledged_by uuid REFERENCES users(id),
  acknowledged_at timestamptz,
  resolved_by     uuid REFERENCES users(id),
  resolved_at     timestamptz,
  note            text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         int NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX uq_recurring_issues_open ON recurring_issues (organization_id, location_id, category_code) WHERE status <> 'resolved';
CREATE INDEX idx_recurring_issues_list ON recurring_issues (organization_id, property_id, status, last_seen_at DESC);
CREATE TRIGGER trg_recurring_issues_upd BEFORE UPDATE ON recurring_issues FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE service_requests ADD COLUMN recurring_issue_id uuid REFERENCES recurring_issues(id);

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['manual_whatsapp_logs','announcement_reads','packages','parking_permits','tenant_feedback','recurring_issues'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE service_requests DROP COLUMN IF EXISTS recurring_issue_id;
DROP TABLE IF EXISTS recurring_issues;
ALTER TABLE property_profile_configs DROP COLUMN IF EXISTS package_reminder_days, DROP COLUMN IF EXISTS recurring_issue_window_days, DROP COLUMN IF EXISTS recurring_issue_threshold;
DROP TABLE IF EXISTS tenant_feedback;
DROP TABLE IF EXISTS parking_permits;
ALTER TABLE vehicles DROP COLUMN IF EXISTS registered_by_tenant;
DROP TABLE IF EXISTS packages;
DROP TABLE IF EXISTS announcement_reads;
DROP INDEX IF EXISTS idx_announcements_scheduled;
UPDATE announcements SET status = 'draft' WHERE status = 'scheduled';
ALTER TABLE announcements DROP CONSTRAINT IF EXISTS announcements_status_check;
ALTER TABLE announcements ADD CONSTRAINT announcements_status_check CHECK (status IN ('draft','published','archived'));
ALTER TABLE announcements DROP COLUMN IF EXISTS recipients_count, DROP COLUMN IF EXISTS target_tenant_ids, DROP COLUMN IF EXISTS target_location_ids,
  DROP COLUMN IF EXISTS requires_ack, DROP COLUMN IF EXISTS publish_at, DROP COLUMN IF EXISTS severity, DROP COLUMN IF EXISTS category;
DROP TABLE IF EXISTS manual_whatsapp_logs;
ALTER TABLE service_requests DROP COLUMN IF EXISTS last_reopened_at;
ALTER TABLE tenant_users DROP CONSTRAINT IF EXISTS tenant_users_registration_source_check;
ALTER TABLE tenant_users ADD CONSTRAINT tenant_users_registration_source_check
  CHECK (registration_source IN ('self','staff','import','rental_onboarding','hotel_checkin','sale_handover'));
ALTER TABLE properties DROP COLUMN IF EXISTS whatsapp_number;
ALTER TABLE users DROP COLUMN IF EXISTS must_change_password;
ALTER TABLE notifications DROP COLUMN IF EXISTS push_attempted_at;
ALTER TABLE device_tokens DROP COLUMN IF EXISTS user_agent, DROP COLUMN IF EXISTS webpush_auth, DROP COLUMN IF EXISTS webpush_p256dh,
  DROP COLUMN IF EXISTS push_kind, DROP COLUMN IF EXISTS app;
-- +goose StatementEnd
