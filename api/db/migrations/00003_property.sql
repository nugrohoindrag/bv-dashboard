-- +goose Up
-- +goose StatementBegin
-- One Building Data Model (PRD §8, TAD §7.2, ADR-006)
CREATE TABLE locations (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid,                             -- root property node (self untuk property)
  location_type   text NOT NULL CHECK (location_type IN ('property','building','tower','floor','area','space','unit')),
  parent_id       uuid REFERENCES locations(id),
  name            text NOT NULL,
  code            text NOT NULL,                    -- PROP-000001 / BLD-000001 / ... (business id)
  path            ltree NOT NULL,                   -- label = replace(id,'-','_') per level
  depth           int  NOT NULL DEFAULT 0,
  sort_order      int  NOT NULL DEFAULT 0,
  is_active       boolean NOT NULL DEFAULT true,
  qr_code         text UNIQUE,                      -- Area QR (PRD §22); diisi dari qr_codes
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, code)
);
CREATE INDEX idx_locations_path ON locations USING GIST (path);
CREATE INDEX idx_locations_parent ON locations (parent_id);
CREATE INDEX idx_locations_property ON locations (organization_id, property_id, location_type);
CREATE TRIGGER trg_locations_upd BEFORE UPDATE ON locations FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE properties (
  location_id     uuid PRIMARY KEY REFERENCES locations(id),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  timezone        text NOT NULL DEFAULT 'Asia/Jakarta',
  address         text,
  city            text,
  property_type   text,                             -- office | mall | apartment | mixed | industrial
  sla_calendar    text NOT NULL DEFAULT 'always' CHECK (sla_calendar IN ('always','business_hours')),
  public_intake_key text UNIQUE,                    -- OD-001
  settings        jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE TABLE property_business_hours (              -- OD-006
  property_id uuid NOT NULL REFERENCES properties(location_id) ON DELETE CASCADE,
  weekday     int NOT NULL CHECK (weekday BETWEEN 0 AND 6),   -- 0 = Minggu
  open_time   time NOT NULL,
  close_time  time NOT NULL,
  PRIMARY KEY (property_id, weekday)
);
CREATE TABLE buildings (
  location_id     uuid PRIMARY KEY REFERENCES locations(id),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  floors_count    int,
  year_built      int,
  gross_area_m2   numeric(12,2)
);
CREATE TABLE towers (
  location_id     uuid PRIMARY KEY REFERENCES locations(id),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  floors_count    int
);
CREATE TABLE floors (
  location_id     uuid PRIMARY KEY REFERENCES locations(id),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  floor_number    int NOT NULL,
  floor_label     text                              -- "Ground Floor", "Basement 01"
);
CREATE TABLE areas (
  location_id     uuid PRIMARY KEY REFERENCES locations(id),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  area_type       text NOT NULL DEFAULT 'other'     -- lobby | toilet | corridor | mechanical_room | parking | loading_dock | garden | other
);
CREATE TABLE spaces (
  location_id     uuid PRIMARY KEY REFERENCES locations(id),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  space_type      text NOT NULL DEFAULT 'other',    -- meeting_room | office | retail | storage | other
  area_m2         numeric(10,2)
);
CREATE TABLE tenants (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  tenant_code     text NOT NULL,                    -- TEN-000001
  name            text NOT NULL,
  tenant_type     text NOT NULL DEFAULT 'company' CHECK (tenant_type IN ('company','individual')),
  contact_name    text,
  contact_phone   text,
  contact_email   text,
  status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive','moved_out')),
  notes           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, tenant_code)
);
CREATE TRIGGER trg_tenants_upd BEFORE UPDATE ON tenants FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE units (
  location_id     uuid PRIMARY KEY REFERENCES locations(id),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  unit_number     text NOT NULL,
  unit_type       text NOT NULL DEFAULT 'commercial' CHECK (unit_type IN ('commercial','residential')),
  area_m2         numeric(10,2),
  tenant_id       uuid REFERENCES tenants(id),
  occupancy_status text NOT NULL DEFAULT 'vacant' CHECK (occupancy_status IN ('vacant','occupied','reserved'))
);
CREATE TABLE occupants (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  tenant_id       uuid REFERENCES tenants(id),
  full_name       text NOT NULL,
  phone           text,
  email           text,
  is_primary_contact boolean NOT NULL DEFAULT false,
  status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_occupants_upd BEFORE UPDATE ON occupants FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
CREATE TABLE unit_occupants (
  unit_location_id uuid NOT NULL REFERENCES units(location_id) ON DELETE CASCADE,
  occupant_id      uuid NOT NULL REFERENCES occupants(id) ON DELETE CASCADE,
  moved_in_at      date,
  moved_out_at     date,
  PRIMARY KEY (unit_location_id, occupant_id)
);

ALTER TABLE teams ADD CONSTRAINT fk_teams_property FOREIGN KEY (property_id) REFERENCES properties(location_id);
ALTER TABLE user_roles ADD CONSTRAINT fk_user_roles_property FOREIGN KEY (property_id) REFERENCES properties(location_id);

-- RLS
ALTER TABLE locations ENABLE ROW LEVEL SECURITY;  ALTER TABLE locations FORCE ROW LEVEL SECURITY;
ALTER TABLE properties ENABLE ROW LEVEL SECURITY; ALTER TABLE properties FORCE ROW LEVEL SECURITY;
ALTER TABLE buildings ENABLE ROW LEVEL SECURITY;  ALTER TABLE buildings FORCE ROW LEVEL SECURITY;
ALTER TABLE towers ENABLE ROW LEVEL SECURITY;     ALTER TABLE towers FORCE ROW LEVEL SECURITY;
ALTER TABLE floors ENABLE ROW LEVEL SECURITY;     ALTER TABLE floors FORCE ROW LEVEL SECURITY;
ALTER TABLE areas ENABLE ROW LEVEL SECURITY;      ALTER TABLE areas FORCE ROW LEVEL SECURITY;
ALTER TABLE spaces ENABLE ROW LEVEL SECURITY;     ALTER TABLE spaces FORCE ROW LEVEL SECURITY;
ALTER TABLE units ENABLE ROW LEVEL SECURITY;      ALTER TABLE units FORCE ROW LEVEL SECURITY;
ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;    ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
ALTER TABLE occupants ENABLE ROW LEVEL SECURITY;  ALTER TABLE occupants FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON locations  USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON properties USING (organization_id = app_current_org() OR current_setting('app.auth_lookup', true) = 'on');
CREATE POLICY org_isolation ON buildings  USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON towers     USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON floors     USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON areas      USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON spaces     USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON units      USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON tenants    USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON occupants  USING (organization_id = app_current_org());
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS unit_occupants, occupants, units, tenants, spaces, areas, floors, towers, buildings, property_business_hours, properties, locations CASCADE;
