-- +goose Up
-- +goose StatementBegin
-- Equipment = kategori/tipe asset (Naming Convention §11–12)
CREATE TABLE equipment (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  category_code   text NOT NULL,                    -- HVAC | LIFT | GEN | PUMP | ELEC | FIRE | PLMB | SECU | FAC | OTH
  category_name   text NOT NULL,                    -- HVAC, Lift, Generator, ...
  type_name       text,                             -- AHU, Chiller, Passenger Lift (NULL = category row)
  parent_id       uuid REFERENCES equipment(id),
  default_criticality text CHECK (default_criticality IN ('low','medium','high','critical')),
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX uq_equipment_cat_type ON equipment (organization_id, category_code, COALESCE(type_name, ''));
CREATE TRIGGER trg_equipment_upd BEFORE UPDATE ON equipment FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE assets (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  asset_code      text NOT NULL,                    -- AST-HVAC-000001
  name            text NOT NULL,
  equipment_id    uuid NOT NULL REFERENCES equipment(id),
  location_id     uuid NOT NULL REFERENCES locations(id),
  status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive','under_maintenance','decommissioned')),
  criticality     text CHECK (criticality IN ('low','medium','high','critical')),
  manufacturer    text,
  model           text,
  serial_number   text,
  installed_at    date,
  warranty_until  date,
  specifications  jsonb NOT NULL DEFAULT '{}'::jsonb,   -- {value, unit} pairs (NC §41)
  notes           text,
  qr_code         text UNIQUE,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, asset_code)
);
CREATE INDEX idx_assets_property ON assets (organization_id, property_id, status);
CREATE INDEX idx_assets_location ON assets (location_id);
CREATE INDEX idx_assets_name_trgm ON assets USING GIN (name gin_trgm_ops);
CREATE TRIGGER trg_assets_upd BEFORE UPDATE ON assets FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- QR code registry (PRD §22, TAD §8.6): satu format untuk Asset, Checkpoint, Area
CREATE TABLE qr_codes (
  code            text PRIMARY KEY,                 -- 12 char base32, opaque, unik global
  organization_id uuid NOT NULL REFERENCES organizations(id),
  object_type     text NOT NULL CHECK (object_type IN ('asset','checkpoint','location')),
  object_id       uuid NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  rotated_at      timestamptz,
  revoked_at      timestamptz
);
CREATE INDEX idx_qr_codes_object ON qr_codes (object_type, object_id) WHERE revoked_at IS NULL;

ALTER TABLE equipment ENABLE ROW LEVEL SECURITY; ALTER TABLE equipment FORCE ROW LEVEL SECURITY;
ALTER TABLE assets    ENABLE ROW LEVEL SECURITY; ALTER TABLE assets    FORCE ROW LEVEL SECURITY;
ALTER TABLE qr_codes  ENABLE ROW LEVEL SECURITY; ALTER TABLE qr_codes  FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON equipment USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON assets    USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON qr_codes  USING (organization_id = app_current_org());
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS qr_codes, assets, equipment CASCADE;
