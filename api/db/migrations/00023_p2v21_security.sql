-- +goose Up
-- +goose StatementBegin
-- PRD P2 v2.1 (Roadmap v2.1 §9, §29) — Security Operations: Emergency Alert (Panic Button → Emergency Alert → Security
-- Response → Incident), incident lengkap (people involved, video, eskalasi, investigasi), Parking sisi security,
-- Lost & Found, finding tertaut checkpoint, skor inspeksi. Backward-compatible. Lihat ADR-017.

-- ---------- §6.4 Emergency (NC §16: Emergency · Panic Button · Emergency Alert) ----------
CREATE TABLE emergency_contacts (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  name            text NOT NULL,
  contact_type    text NOT NULL DEFAULT 'other' CHECK (contact_type IN ('fire','ambulance','police','electricity','water','internal','other')),
  phone           text NOT NULL,
  notes           text,
  sort_order      int NOT NULL DEFAULT 0,
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1
);
CREATE INDEX idx_emergency_contacts_property ON emergency_contacts (organization_id, property_id, sort_order);
CREATE TRIGGER trg_emergency_contacts_upd BEFORE UPDATE ON emergency_contacts FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE emergency_alerts (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  property_id       uuid NOT NULL REFERENCES properties(location_id),
  alert_number      text NOT NULL,                              -- EMG-2026-000001
  emergency_type    text NOT NULL DEFAULT 'other' CHECK (emergency_type IN ('fire','medical','security_threat','intrusion','natural_disaster','evacuation','utility_failure','other')),
  status            text NOT NULL DEFAULT 'raised' CHECK (status IN ('raised','acknowledged','responding','resolved','cancelled')),
  location_id       uuid REFERENCES locations(id),
  description       text,
  gps_lat           numeric(9,6),
  gps_lng           numeric(9,6),
  gps_status        text CHECK (gps_status IS NULL OR gps_status IN ('captured','unavailable','denied')),
  channel           text NOT NULL DEFAULT 'web' CHECK (channel IN ('panic_button','mobile','web','sync')),
  raised_by         uuid REFERENCES users(id),
  raised_at         timestamptz NOT NULL DEFAULT now(),
  client_raised_at  timestamptz,                                -- waktu tombol ditekan di perangkat (offline)
  acknowledged_by   uuid REFERENCES users(id),
  acknowledged_at   timestamptz,
  responder_user_id uuid REFERENCES users(id),
  responding_at     timestamptz,                                -- responder tiba / menangani di lokasi
  resolved_by       uuid REFERENCES users(id),
  resolved_at       timestamptz,
  resolution        text,
  cancelled_at      timestamptz,
  cancel_reason     text,
  escalation_level  int NOT NULL DEFAULT 0,
  escalated_at      timestamptz,
  incident_id       uuid REFERENCES incidents(id),
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid,
  version           int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, alert_number)
);
CREATE INDEX idx_emergency_alerts_active ON emergency_alerts (organization_id, property_id, status, raised_at DESC);
CREATE TRIGGER trg_emergency_alerts_upd BEFORE UPDATE ON emergency_alerts FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- timeline emergency (P2-EMG-06): raised → acknowledged → responding (tiba) → tindakan → resolved
CREATE TABLE emergency_alert_events (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  alert_id        uuid NOT NULL REFERENCES emergency_alerts(id) ON DELETE CASCADE,
  event_type      text NOT NULL CHECK (event_type IN ('raised','acknowledged','responding','action','escalated','incident_created','resolved','cancelled','note')),
  note            text,
  actor_user_id   uuid REFERENCES users(id),
  payload         jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_emergency_alert_events ON emergency_alert_events (alert_id, occurred_at);

-- ---------- §6.3 Incident lengkap: people involved, eskalasi, investigasi ----------
CREATE TABLE incident_people (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  incident_id     uuid NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  person_role     text NOT NULL CHECK (person_role IN ('reporter','victim','witness','suspect','other')),
  name            text NOT NULL,
  contact         text,
  identity_number text,                                        -- data pribadi: hanya role Security/Management (P2-NFR-04)
  tenant_id       uuid REFERENCES tenants(id),
  notes           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1
);
CREATE INDEX idx_incident_people ON incident_people (incident_id);
CREATE TRIGGER trg_incident_people_upd BEFORE UPDATE ON incident_people FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE incidents
  ADD COLUMN escalated_at             timestamptz,
  ADD COLUMN escalation_level         int NOT NULL DEFAULT 0,
  ADD COLUMN investigation_status     text NOT NULL DEFAULT 'not_started' CHECK (investigation_status IN ('not_started','in_progress','completed')),
  ADD COLUMN investigator_user_id     uuid REFERENCES users(id),
  ADD COLUMN investigation_findings   text,
  ADD COLUMN root_cause               text,
  ADD COLUMN corrective_action        text,
  ADD COLUMN corrective_owner_user_id uuid REFERENCES users(id),
  ADD COLUMN investigated_at          timestamptz;

-- video sebagai evidence (P2-SIN-04)
ALTER TABLE attachments DROP CONSTRAINT IF EXISTS attachments_attachment_type_check;
ALTER TABLE attachments ADD CONSTRAINT attachments_attachment_type_check CHECK (attachment_type IN
  ('photo','photo_before','photo_during','photo_after','checklist_item_photo','document','signature','video'));

-- finding menyimpan checkpoint asal (P2-PAT-08)
ALTER TABLE findings ADD COLUMN checkpoint_id uuid REFERENCES checkpoints(id);

-- skor inspeksi engineering (setara housekeeping_inspections.score) — P2-HKI-03
ALTER TABLE inspections ADD COLUMN score int CHECK (score IS NULL OR score BETWEEN 0 AND 100);

-- ---------- §6.6 Parking sisi security (D-P2-06; tenant-facing di P3) ----------
CREATE TABLE parking_areas (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  location_id     uuid REFERENCES locations(id),
  code            text NOT NULL,
  name            text NOT NULL,
  area_type       text NOT NULL DEFAULT 'mixed' CHECK (area_type IN ('tenant','visitor','staff','public','loading','mixed')),
  capacity        int NOT NULL DEFAULT 0 CHECK (capacity >= 0),
  is_active       boolean NOT NULL DEFAULT true,
  notes           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, property_id, code)
);
CREATE TRIGGER trg_parking_areas_upd BEFORE UPDATE ON parking_areas FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE vehicles (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  plate_number     text NOT NULL,                              -- dinormalisasi: huruf besar tanpa spasi
  vehicle_type     text NOT NULL DEFAULT 'car' CHECK (vehicle_type IN ('car','motorcycle','truck','bicycle','other')),
  brand            text,
  color            text,
  owner_type       text NOT NULL DEFAULT 'tenant' CHECK (owner_type IN ('tenant','staff','visitor','other')),
  tenant_id        uuid REFERENCES tenants(id),
  unit_location_id uuid REFERENCES locations(id),
  user_id          uuid REFERENCES users(id),
  visitor_id       uuid REFERENCES visitors(id),
  owner_name       text,
  owner_phone      text,
  parking_area_id  uuid REFERENCES parking_areas(id),
  permit_until     date,
  status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive','blacklisted')),
  notes            text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, property_id, plate_number)
);
CREATE TRIGGER trg_vehicles_upd BEFORE UPDATE ON vehicles FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE parking_logs (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  parking_area_id uuid REFERENCES parking_areas(id),
  vehicle_id      uuid REFERENCES vehicles(id),
  plate_number    text NOT NULL,
  entered_at      timestamptz NOT NULL DEFAULT now(),
  exited_at       timestamptz,
  gate            text,
  entry_by        uuid REFERENCES users(id),
  exit_by         uuid REFERENCES users(id),
  note            text,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_parking_logs_open ON parking_logs (organization_id, property_id, exited_at, entered_at DESC);
CREATE INDEX idx_parking_logs_plate ON parking_logs (organization_id, plate_number);

CREATE TABLE parking_violations (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  violation_number text NOT NULL,                              -- PKV-2026-000001
  parking_area_id  uuid REFERENCES parking_areas(id),
  location_id      uuid REFERENCES locations(id),
  vehicle_id       uuid REFERENCES vehicles(id),
  plate_number     text NOT NULL,
  violation_type   text NOT NULL DEFAULT 'other' CHECK (violation_type IN ('illegal_parking','no_permit','blocking','overstay','reserved_spot','other')),
  description      text,
  action_taken     text NOT NULL DEFAULT 'none' CHECK (action_taken IN ('none','warning','sticker','wheel_lock','towed','reported')),
  status           text NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','escalated')),
  incident_id      uuid REFERENCES incidents(id),
  recorded_by      uuid REFERENCES users(id),
  recorded_at      timestamptz NOT NULL DEFAULT now(),
  resolved_at      timestamptz,
  resolution       text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, violation_number)
);
CREATE INDEX idx_parking_violations ON parking_violations (organization_id, property_id, status, recorded_at DESC);
CREATE TRIGGER trg_parking_violations_upd BEFORE UPDATE ON parking_violations FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------- §6.7 Lost & Found ----------
CREATE TABLE lost_reports (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  property_id       uuid NOT NULL REFERENCES properties(location_id),
  report_number     text NOT NULL,                             -- LST-2026-000001
  category          text NOT NULL DEFAULT 'other' CHECK (category IN ('electronics','wallet','document','keys','jewelry','bag','clothing','other')),
  description       text NOT NULL,
  lost_location_id  uuid REFERENCES locations(id),
  lost_at           timestamptz,
  reporter_name     text NOT NULL,
  reporter_contact  text,
  tenant_id         uuid REFERENCES tenants(id),
  status            text NOT NULL DEFAULT 'open' CHECK (status IN ('open','matched','closed','cancelled')),
  matched_item_id   uuid,
  created_at        timestamptz NOT NULL DEFAULT now(),
  created_by        uuid,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid,
  version           int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, report_number)
);
CREATE TRIGGER trg_lost_reports_upd BEFORE UPDATE ON lost_reports FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE lost_found_items (
  id                    uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id       uuid NOT NULL REFERENCES organizations(id),
  property_id           uuid NOT NULL REFERENCES properties(location_id),
  item_number           text NOT NULL,                         -- LNF-2026-000001
  category              text NOT NULL DEFAULT 'other' CHECK (category IN ('electronics','wallet','document','keys','jewelry','bag','clothing','other')),
  description           text NOT NULL,
  found_location_id     uuid REFERENCES locations(id),
  found_at              timestamptz NOT NULL DEFAULT now(),
  found_by_user_id      uuid REFERENCES users(id),
  finder_name           text,                                  -- penemu bukan staf (tenant/tamu)
  storage_location      text,
  status                text NOT NULL DEFAULT 'stored' CHECK (status IN ('stored','returned','disposed')),
  retention_until       date NOT NULL,
  matched_report_id     uuid REFERENCES lost_reports(id),
  claimant_name         text,
  claimant_contact      text,
  claimant_identity     text,                                  -- jenis + nomor identitas (data pribadi)
  returned_at           timestamptz,
  returned_by           uuid REFERENCES users(id),
  signature_attachment_id uuid REFERENCES attachments(id),
  disposed_at           timestamptz,
  disposal_method       text CHECK (disposal_method IS NULL OR disposal_method IN ('donated','destroyed','handed_to_police','auctioned','other')),
  disposal_note         text,
  created_at            timestamptz NOT NULL DEFAULT now(),
  created_by            uuid,
  updated_at            timestamptz NOT NULL DEFAULT now(),
  updated_by            uuid,
  version               int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, item_number)
);
CREATE INDEX idx_lost_found_items ON lost_found_items (organization_id, property_id, status, found_at DESC);
CREATE TRIGGER trg_lost_found_items_upd BEFORE UPDATE ON lost_found_items FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE lost_reports ADD CONSTRAINT lost_reports_matched_item_fk FOREIGN KEY (matched_item_id) REFERENCES lost_found_items(id);

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['emergency_contacts','emergency_alerts','emergency_alert_events','incident_people','parking_areas','vehicles','parking_logs','parking_violations','lost_reports','lost_found_items'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS lost_found_items CASCADE;
DROP TABLE IF EXISTS lost_reports CASCADE;
DROP TABLE IF EXISTS parking_violations;
DROP TABLE IF EXISTS parking_logs;
DROP TABLE IF EXISTS vehicles;
DROP TABLE IF EXISTS parking_areas;
ALTER TABLE inspections DROP COLUMN IF EXISTS score;
ALTER TABLE findings DROP COLUMN IF EXISTS checkpoint_id;
UPDATE attachments SET attachment_type = 'document' WHERE attachment_type = 'video';
ALTER TABLE attachments DROP CONSTRAINT IF EXISTS attachments_attachment_type_check;
ALTER TABLE attachments ADD CONSTRAINT attachments_attachment_type_check CHECK (attachment_type IN ('photo','photo_before','photo_during','photo_after','checklist_item_photo','document','signature'));
ALTER TABLE incidents DROP COLUMN IF EXISTS investigated_at, DROP COLUMN IF EXISTS corrective_owner_user_id, DROP COLUMN IF EXISTS corrective_action,
  DROP COLUMN IF EXISTS root_cause, DROP COLUMN IF EXISTS investigation_findings, DROP COLUMN IF EXISTS investigator_user_id,
  DROP COLUMN IF EXISTS investigation_status, DROP COLUMN IF EXISTS escalation_level, DROP COLUMN IF EXISTS escalated_at;
DROP TABLE IF EXISTS incident_people;
DROP TABLE IF EXISTS emergency_alert_events;
DROP TABLE IF EXISTS emergency_alerts;
DROP TABLE IF EXISTS emergency_contacts;
-- +goose StatementEnd
