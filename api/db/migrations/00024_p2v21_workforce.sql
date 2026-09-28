-- +goose Up
-- +goose StatementBegin
-- PRD P2 v2.1 (Roadmap v2.1 §9 Shift Management, §10 Shift, §25.2 Workforce KPI, §25.8 Workforce Capacity) — keputusan
-- D-P2-05: shift PER DOMAIN (Security Shift Management & Housekeeping Shift). Satu model tabel dengan kolom domain agar field
-- inti (shift, roster, clock-in, handover) dan format event seragam; dikelola dari modul domain masing-masing (permission
-- security.shifts.* / housekeeping.shifts.*). Kompetensi staf (skill + sertifikat/lisensi) satu model untuk teknisi &
-- personel security (P2-TEC-01, P2-SPN-01). Tanpa payroll/cuti (Roadmap §2). Lihat ADR-017.

CREATE TABLE shift_definitions (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  domain          text NOT NULL CHECK (domain IN ('security','housekeeping')),
  code            text NOT NULL,
  name            text NOT NULL,
  start_time      time NOT NULL,
  end_time        time NOT NULL,                              -- end ≤ start = lintas tengah malam
  break_minutes   int NOT NULL DEFAULT 0 CHECK (break_minutes >= 0),
  min_staff       int NOT NULL DEFAULT 1 CHECK (min_staff >= 0), -- kebutuhan minimum staf on-duty (kapasitas)
  color           text,
  sort_order      int NOT NULL DEFAULT 0,
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, property_id, domain, code)
);
CREATE TRIGGER trg_shift_definitions_upd BEFORE UPDATE ON shift_definitions FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- roster: penugasan staf ke shift per tanggal (P2-SHF-02)
CREATE TABLE shift_assignments (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  domain          text NOT NULL CHECK (domain IN ('security','housekeeping')),
  shift_id        uuid NOT NULL REFERENCES shift_definitions(id),
  shift_date      date NOT NULL,
  user_id         uuid NOT NULL REFERENCES users(id),
  team_id         uuid REFERENCES teams(id),
  post            text,                                        -- pos jaga (security) / zona (housekeeping)
  starts_at       timestamptz NOT NULL,                        -- dihitung dari shift_date + start_time (timezone property)
  ends_at         timestamptz NOT NULL,
  status          text NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','cancelled')),
  note            text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (shift_id, shift_date, user_id)
);
CREATE INDEX idx_shift_assignments_date ON shift_assignments (organization_id, property_id, domain, shift_date);
CREATE INDEX idx_shift_assignments_user ON shift_assignments (user_id, starts_at);
CREATE TRIGGER trg_shift_assignments_upd BEFORE UPDATE ON shift_assignments FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- attendance / on-duty (P2-DTY-01..02): clock-in/out dari Staff App (GPS opsional, offline via sync)
CREATE TABLE attendance_records (
  id                  uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id     uuid NOT NULL REFERENCES organizations(id),
  property_id         uuid NOT NULL REFERENCES properties(location_id),
  domain              text NOT NULL CHECK (domain IN ('security','housekeeping','engineering','general')),
  user_id             uuid NOT NULL REFERENCES users(id),
  shift_assignment_id uuid REFERENCES shift_assignments(id),
  clock_in_at         timestamptz NOT NULL,
  clock_in_lat        numeric(9,6),
  clock_in_lng        numeric(9,6),
  clock_in_gps_status text CHECK (clock_in_gps_status IS NULL OR clock_in_gps_status IN ('captured','unavailable','denied')),
  clock_in_source     text NOT NULL DEFAULT 'mobile' CHECK (clock_in_source IN ('web','mobile','sync','system')),
  clock_out_at        timestamptz,
  clock_out_lat       numeric(9,6),
  clock_out_lng       numeric(9,6),
  clock_out_gps_status text CHECK (clock_out_gps_status IS NULL OR clock_out_gps_status IN ('captured','unavailable','denied')),
  clock_out_source    text CHECK (clock_out_source IS NULL OR clock_out_source IN ('web','mobile','sync','system')),
  status              text NOT NULL DEFAULT 'on_duty' CHECK (status IN ('on_duty','completed','auto_closed')),
  late_minutes        int NOT NULL DEFAULT 0,
  note                text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             int NOT NULL DEFAULT 1
);
-- satu sesi on-duty terbuka per user
CREATE UNIQUE INDEX uq_attendance_open ON attendance_records (user_id) WHERE clock_out_at IS NULL;
CREATE INDEX idx_attendance_onduty ON attendance_records (organization_id, property_id, domain) WHERE clock_out_at IS NULL;
CREATE INDEX idx_attendance_user ON attendance_records (user_id, clock_in_at DESC);
CREATE TRIGGER trg_attendance_records_upd BEFORE UPDATE ON attendance_records FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- serah terima shift (P2-SHF-03): catatan + snapshot item terbuka (incident aktif, patrol tertunda, area belum selesai, temuan)
CREATE TABLE shift_handovers (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  domain           text NOT NULL CHECK (domain IN ('security','housekeeping')),
  handover_number  text NOT NULL,                             -- HOV-2026-000001
  shift_id         uuid REFERENCES shift_definitions(id),
  shift_date       date,
  handed_over_by   uuid NOT NULL REFERENCES users(id),
  received_by      uuid REFERENCES users(id),
  post             text,
  notes            text NOT NULL,
  open_items       jsonb NOT NULL DEFAULT '{}'::jsonb,
  status           text NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted','acknowledged')),
  handed_over_at   timestamptz NOT NULL DEFAULT now(),
  acknowledged_at  timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, handover_number)
);
CREATE INDEX idx_shift_handovers ON shift_handovers (organization_id, property_id, domain, handed_over_at DESC);
CREATE TRIGGER trg_shift_handovers_upd BEFORE UPDATE ON shift_handovers FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- patrol schedule & cleaning schedule dapat dikaitkan ke shift (P2-SHF-04)
ALTER TABLE patrol_schedules ADD COLUMN shift_id uuid REFERENCES shift_definitions(id);
ALTER TABLE cleaning_schedules ADD COLUMN shift_id uuid REFERENCES shift_definitions(id);

DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['shift_definitions','shift_assignments','attendance_records','shift_handovers'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cleaning_schedules DROP COLUMN IF EXISTS shift_id;
ALTER TABLE patrol_schedules DROP COLUMN IF EXISTS shift_id;
DROP TABLE IF EXISTS shift_handovers;
DROP TABLE IF EXISTS attendance_records;
DROP TABLE IF EXISTS shift_assignments;
DROP TABLE IF EXISTS shift_definitions;
-- +goose StatementEnd
