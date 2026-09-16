-- +goose Up
-- +goose StatementBegin
-- ============ HOTEL BOOKING MANAGEMENT (PRD P1 v1.3 §3.9, WF-P1-007, AT-P1-000C; NC §71 Commercial; TD-P1-005: Room = Unit) ============
-- Inventori kamar milik property. OTA/channel manager di luar scope (guardrail #18, #21).

CREATE TABLE hotel_room_types (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  property_id       uuid NOT NULL REFERENCES properties(location_id),
  room_type_code    text NOT NULL,                                 -- RT-2026-000001
  name              text NOT NULL,                                 -- Deluxe, Suite
  description       text,
  capacity_adults   int NOT NULL DEFAULT 2,
  capacity_children int NOT NULL DEFAULT 0,
  bed_type          text,
  size_m2           numeric(8,2),
  amenities         text[] NOT NULL DEFAULT '{}',
  base_rate         bigint NOT NULL DEFAULT 0,                     -- per malam (IDR) bila tidak ada rate khusus
  currency_code     char(3) NOT NULL DEFAULT 'IDR',
  status            text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
  created_at        timestamptz NOT NULL DEFAULT now(),
  created_by        uuid,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid,
  version           int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, room_type_code)
);
CREATE INDEX idx_hotel_room_types_property ON hotel_room_types (organization_id, property_id, status);
CREATE TRIGGER trg_hotel_room_types_upd BEFORE UPDATE ON hotel_room_types FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- HotelRoom = ekstensi 1:1 unit (One Building Data Model): SR/WO/cleaning task menunjuk lokasi kamar yang sama
CREATE TABLE hotel_rooms (
  location_id     uuid PRIMARY KEY REFERENCES units(location_id) ON DELETE CASCADE,
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  room_code       text NOT NULL,                                   -- ROOM-2026-000001
  room_type_id    uuid NOT NULL REFERENCES hotel_room_types(id),
  room_number     text NOT NULL,
  -- PRD §16 Hotel Room: Available | Occupied | Dirty | Clean | Inspected | Out of Order | Out of Service
  room_status     text NOT NULL DEFAULT 'available' CHECK (room_status IN ('available','occupied','dirty','clean','inspected','out_of_order','out_of_service')),
  status_note     text,
  status_changed_at timestamptz NOT NULL DEFAULT now(),
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, room_code)
);
CREATE INDEX idx_hotel_rooms_property ON hotel_rooms (organization_id, property_id, room_status);
CREATE INDEX idx_hotel_rooms_type ON hotel_rooms (room_type_id);
CREATE TRIGGER trg_hotel_rooms_upd BEFORE UPDATE ON hotel_rooms FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE hotel_rates (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  rate_code       text NOT NULL,                                   -- RATE-2026-000001
  room_type_id    uuid NOT NULL REFERENCES hotel_room_types(id) ON DELETE CASCADE,
  name            text NOT NULL,                                   -- Weekday, Weekend, Long Stay
  rate_per_night  bigint NOT NULL CHECK (rate_per_night >= 0),
  currency_code   char(3) NOT NULL DEFAULT 'IDR',
  valid_from      date,
  valid_until     date,
  weekdays        int[] NOT NULL DEFAULT '{0,1,2,3,4,5,6}',
  min_nights      int NOT NULL DEFAULT 1,
  priority        int NOT NULL DEFAULT 0,                          -- rate lebih spesifik menang
  status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, rate_code)
);
CREATE INDEX idx_hotel_rates_type ON hotel_rates (room_type_id, status);
CREATE TRIGGER trg_hotel_rates_upd BEFORE UPDATE ON hotel_rates FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE hotel_reservations (
  id                   uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id      uuid NOT NULL REFERENCES organizations(id),
  property_id          uuid NOT NULL REFERENCES properties(location_id),
  reservation_number   text NOT NULL,                              -- RES-2026-000001
  room_type_id         uuid NOT NULL REFERENCES hotel_room_types(id),
  room_location_id     uuid REFERENCES hotel_rooms(location_id),   -- room assignment (boleh kosong sampai check-in)
  guest_name           text NOT NULL,
  guest_phone          text,
  guest_email          text,
  adults               int NOT NULL DEFAULT 1,
  children             int NOT NULL DEFAULT 0,
  check_in_date        date NOT NULL,
  check_out_date       date NOT NULL,
  stay                 daterange GENERATED ALWAYS AS (daterange(check_in_date, check_out_date, '[)')) STORED,
  nights               int GENERATED ALWAYS AS (check_out_date - check_in_date) STORED,
  rate_id              uuid REFERENCES hotel_rates(id),
  rate_per_night       bigint NOT NULL DEFAULT 0,
  total_amount         bigint NOT NULL DEFAULT 0,
  currency_code        char(3) NOT NULL DEFAULT 'IDR',
  -- PRD §16 Hotel Reservation: New | Confirmed | Checked In | Checked Out | Cancelled | No Show
  status               text NOT NULL DEFAULT 'new' CHECK (status IN ('new','confirmed','checked_in','checked_out','cancelled','no_show')),
  source               text NOT NULL DEFAULT 'walk_in' CHECK (source IN ('walk_in','phone','email','website','corporate','other')),
  special_requests     text,
  notes                text,                                       -- internal
  invoice_id           uuid REFERENCES invoices(id),               -- Billing linkage (check-out)
  tenant_user_id       uuid REFERENCES users(id),                  -- akun Guest App (opsional, dibuat saat check-in)
  confirmed_at         timestamptz,
  checked_in_at        timestamptz,
  checked_out_at       timestamptz,
  cancelled_at         timestamptz,
  cancel_reason        text,
  no_show_at           timestamptz,
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by           uuid,
  updated_at           timestamptz NOT NULL DEFAULT now(),
  updated_by           uuid,
  version              int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, reservation_number),
  CHECK (check_out_date > check_in_date),
  -- DoD #42: satu kamar tidak boleh dipesan ganda pada malam yang sama (ditegakkan DB)
  EXCLUDE USING gist (room_location_id WITH =, stay WITH &&) WHERE (room_location_id IS NOT NULL AND status IN ('new','confirmed','checked_in'))
);
CREATE INDEX idx_hotel_res_list ON hotel_reservations (organization_id, property_id, status, check_in_date);
CREATE INDEX idx_hotel_res_type_stay ON hotel_reservations USING gist (room_type_id, stay) WHERE status IN ('new','confirmed','checked_in');
CREATE INDEX idx_hotel_res_room ON hotel_reservations (room_location_id);
CREATE TRIGGER trg_hotel_reservations_upd BEFORE UPDATE ON hotel_reservations FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE hotel_reservation_guests (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  reservation_id   uuid NOT NULL REFERENCES hotel_reservations(id) ON DELETE CASCADE,
  full_name        text NOT NULL,
  id_type          text,                                           -- ktp | passport | sim
  id_number_masked text,
  phone            text,
  email            text,
  nationality      text,
  is_primary       boolean NOT NULL DEFAULT false,
  created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_hotel_res_guests ON hotel_reservation_guests (reservation_id);

-- unit_type kamar hotel (units.unit_type CHECK lama: commercial|residential)
ALTER TABLE units DROP CONSTRAINT IF EXISTS units_unit_type_check;
ALTER TABLE units ADD CONSTRAINT units_unit_type_check CHECK (unit_type IN ('commercial','residential','hotel_room'));

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['hotel_room_types','hotel_rooms','hotel_rates','hotel_reservations','hotel_reservation_guests'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS hotel_reservation_guests, hotel_reservations, hotel_rates, hotel_rooms, hotel_room_types CASCADE;
ALTER TABLE units DROP CONSTRAINT IF EXISTS units_unit_type_check;
ALTER TABLE units ADD CONSTRAINT units_unit_type_check CHECK (unit_type IN ('commercial','residential'));
-- +goose StatementEnd
