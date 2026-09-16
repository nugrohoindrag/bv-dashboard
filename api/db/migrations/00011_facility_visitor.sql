-- +goose Up
-- +goose StatementBegin
-- ============ FACILITY BOOKING (PRD P1 v1.3 §21, WF-P1-004, AT-P1-010; NC §72 #11 Booking = generic) ============
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE facilities (
  id                   uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id      uuid NOT NULL REFERENCES organizations(id),
  property_id          uuid NOT NULL REFERENCES properties(location_id),
  facility_code        text NOT NULL,                              -- FCL-000001
  name                 text NOT NULL,
  description          text,
  facility_type        text NOT NULL DEFAULT 'other' CHECK (facility_type IN ('meeting_room','function_hall','gym','pool','court','bbq','coworking','lounge','parking','other')),
  location_id          uuid REFERENCES locations(id),              -- ruang/area fisik (One Building Data Model)
  capacity             int,
  requires_approval    boolean,                                    -- NULL = ikut property_profile_configs.booking_approval_required (OD-P1-007)
  slot_minutes         int NOT NULL DEFAULT 60 CHECK (slot_minutes BETWEEN 15 AND 1440),
  min_duration_minutes int NOT NULL DEFAULT 60,
  max_duration_minutes int NOT NULL DEFAULT 240,
  advance_booking_days int NOT NULL DEFAULT 30,
  open_time            time NOT NULL DEFAULT '08:00',
  close_time           time NOT NULL DEFAULT '21:00',
  weekdays             int[] NOT NULL DEFAULT '{0,1,2,3,4,5,6}',
  rules                text,
  image_attachment_id  uuid,
  is_active            boolean NOT NULL DEFAULT true,
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by           uuid,
  updated_at           timestamptz NOT NULL DEFAULT now(),
  updated_by           uuid,
  deleted_at           timestamptz,
  version              int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, facility_code)
);
CREATE INDEX idx_facilities_property ON facilities (organization_id, property_id) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_facilities_upd BEFORE UPDATE ON facilities FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- FacilitySchedule: penutupan / jam khusus (maintenance, event internal)
CREATE TABLE facility_schedules (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  facility_id     uuid NOT NULL REFERENCES facilities(id) ON DELETE CASCADE,
  kind            text NOT NULL DEFAULT 'closure' CHECK (kind IN ('closure','special_hours')),
  starts_at       timestamptz NOT NULL,
  ends_at         timestamptz NOT NULL,
  open_time       time,
  close_time      time,
  reason          text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  CHECK (ends_at > starts_at)
);
CREATE INDEX idx_facility_schedules ON facility_schedules (facility_id, starts_at, ends_at);

CREATE TABLE bookings (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  booking_number   text NOT NULL,                                  -- BKG-2026-000001
  facility_id      uuid NOT NULL REFERENCES facilities(id),
  tenant_user_id   uuid REFERENCES users(id),                      -- pemesan dari Tenant App
  tenant_id        uuid REFERENCES tenants(id),
  occupant_id      uuid REFERENCES occupants(id),
  requester_name   text,
  requester_phone  text,
  starts_at        timestamptz NOT NULL,
  ends_at          timestamptz NOT NULL,
  period           tstzrange GENERATED ALWAYS AS (tstzrange(starts_at, ends_at, '[)')) STORED,
  attendees        int,
  purpose          text,
  notes            text,
  status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','rejected','cancelled','checked_in','completed','no_show')),
  channel          text NOT NULL DEFAULT 'staff' CHECK (channel IN ('tenant_app','staff')),
  approved_by      uuid,
  approved_at      timestamptz,
  rejection_reason text,
  cancelled_at     timestamptz,
  cancelled_by     uuid,
  cancel_reason    text,
  checked_in_at    timestamptz,
  completed_at     timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, booking_number),
  CHECK (ends_at > starts_at),
  -- AT-P1-010 / DoD #27: double booking dicegah oleh database, bukan aplikasi
  EXCLUDE USING gist (facility_id WITH =, period WITH &&) WHERE (status IN ('pending','confirmed','checked_in'))
);
CREATE INDEX idx_bookings_list ON bookings (organization_id, property_id, status, starts_at);
CREATE INDEX idx_bookings_tenant_user ON bookings (tenant_user_id, starts_at DESC);
CREATE INDEX idx_bookings_facility_time ON bookings (facility_id, starts_at);
CREATE TRIGGER trg_bookings_upd BEFORE UPDATE ON bookings FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ============ VISITOR MANAGEMENT (PRD §22, WF-P1-005, AT-P1-011) ============
CREATE TABLE visitors (
  id                    uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id       uuid NOT NULL REFERENCES organizations(id),
  property_id           uuid NOT NULL REFERENCES properties(location_id),
  visitor_number        text NOT NULL,                             -- VIS-2026-000001
  tenant_user_id        uuid REFERENCES users(id),                 -- host dari Tenant App
  tenant_id             uuid REFERENCES tenants(id),
  host_name             text,
  host_unit_location_id uuid REFERENCES locations(id),
  visitor_name          text NOT NULL,
  visitor_phone         text,
  visitor_company       text,
  id_number_masked      text,                                      -- hanya 4 digit terakhir disimpan (privasi)
  purpose               text,
  vehicle_plate         text,
  headcount             int NOT NULL DEFAULT 1 CHECK (headcount BETWEEN 1 AND 100),
  expected_at           timestamptz NOT NULL,
  expected_until        timestamptz,
  status                text NOT NULL DEFAULT 'registered' CHECK (status IN ('pending_approval','registered','checked_in','checked_out','cancelled','expired','denied')),
  channel               text NOT NULL DEFAULT 'staff' CHECK (channel IN ('tenant_app','staff','walk_in')),
  approved_by           uuid,
  approved_at           timestamptz,
  denied_reason         text,
  verified_by           uuid,
  checked_in_at         timestamptz,
  checked_out_at        timestamptz,
  checkin_note          text,
  cancelled_at          timestamptz,
  created_at            timestamptz NOT NULL DEFAULT now(),
  created_by            uuid,
  updated_at            timestamptz NOT NULL DEFAULT now(),
  updated_by            uuid,
  version               int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, visitor_number)
);
CREATE INDEX idx_visitors_list ON visitors (organization_id, property_id, status, expected_at);
CREATE INDEX idx_visitors_tenant_user ON visitors (tenant_user_id, expected_at DESC);
CREATE TRIGGER trg_visitors_upd BEFORE UPDATE ON visitors FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- VisitorPass: kode/QR yang diverifikasi Security saat masuk
CREATE TABLE visitor_passes (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  visitor_id      uuid NOT NULL REFERENCES visitors(id) ON DELETE CASCADE,
  pass_code       text NOT NULL UNIQUE,                            -- 12 char base32 opaque (QR payload)
  valid_from      timestamptz NOT NULL,
  valid_until     timestamptz NOT NULL,
  status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active','used','revoked','expired')),
  issued_at       timestamptz NOT NULL DEFAULT now(),
  used_at         timestamptz,
  revoked_at      timestamptz
);
CREATE INDEX idx_visitor_passes_visitor ON visitor_passes (visitor_id);

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['facilities','facility_schedules','bookings','visitors','visitor_passes'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS visitor_passes, visitors, bookings, facility_schedules, facilities CASCADE;
-- +goose StatementEnd
