-- +goose Up
-- +goose StatementBegin
-- ============ VENDOR MANAGEMENT (PRD P1 v1.3 §24; NC §37) ============
CREATE TABLE vendors (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  vendor_code        text NOT NULL,                                -- VND-000001
  name               text NOT NULL,
  service_categories text[] NOT NULL DEFAULT '{}',                 -- hvac | electrical | plumbing | lift | cleaning | security | pest_control | civil | it | landscaping | other
  contact_name       text,
  contact_phone      text,
  contact_email      text,
  address            text,
  tax_id             text,
  contract_ref       text,
  contract_start     date,
  contract_end       date,
  status             text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive','blacklisted')),
  notes              text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  created_by         uuid,
  updated_at         timestamptz NOT NULL DEFAULT now(),
  updated_by         uuid,
  deleted_at         timestamptz,
  version            int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, vendor_code)
);
CREATE INDEX idx_vendors_name_trgm ON vendors USING GIN (name gin_trgm_ops);
CREATE TRIGGER trg_vendors_upd BEFORE UPDATE ON vendors FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE vendor_contacts (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  vendor_id       uuid NOT NULL REFERENCES vendors(id) ON DELETE CASCADE,
  name            text NOT NULL,
  role            text,
  phone           text,
  email           text,
  is_primary      boolean NOT NULL DEFAULT false,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_vendor_contacts ON vendor_contacts (vendor_id);

-- Vendor Work Order (NC §37): WO ditugaskan ke vendor; performance diturunkan dari WO (bukan tabel terpisah)
ALTER TABLE work_orders
  ADD COLUMN IF NOT EXISTS vendor_id          uuid REFERENCES vendors(id),
  ADD COLUMN IF NOT EXISTS vendor_assigned_at timestamptz,
  ADD COLUMN IF NOT EXISTS vendor_notes       text;
CREATE INDEX IF NOT EXISTS idx_wo_vendor ON work_orders (vendor_id) WHERE vendor_id IS NOT NULL;

-- ============ INVENTORY / SPARE PARTS (PRD P1 v1.3 §25; NC §38) ============
CREATE TABLE inventory_items (
  id                      uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id         uuid NOT NULL REFERENCES organizations(id),
  item_code               text NOT NULL,                           -- ITM-000001
  name                    text NOT NULL,
  description             text,
  category                text NOT NULL DEFAULT 'spare_part' CHECK (category IN ('spare_part','consumable','tool','other')),
  equipment_category_code text,                                    -- HVAC | LIFT | ... (NC §13) untuk spare part
  unit                    text NOT NULL DEFAULT 'pcs',
  min_stock               numeric(12,2) NOT NULL DEFAULT 0,
  unit_cost               bigint NOT NULL DEFAULT 0,               -- IDR (internal; tidak pernah ke tenant)
  barcode                 text,
  is_active               boolean NOT NULL DEFAULT true,
  created_at              timestamptz NOT NULL DEFAULT now(),
  created_by              uuid,
  updated_at              timestamptz NOT NULL DEFAULT now(),
  updated_by              uuid,
  deleted_at              timestamptz,
  version                 int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, item_code)
);
CREATE INDEX idx_items_name_trgm ON inventory_items USING GIN (name gin_trgm_ops);
CREATE TRIGGER trg_inventory_items_upd BEFORE UPDATE ON inventory_items FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE stock_locations (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  name            text NOT NULL,                                   -- Gudang Teknik B1
  location_id     uuid REFERENCES locations(id),
  is_default      boolean NOT NULL DEFAULT false,
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1
);
CREATE INDEX idx_stock_locations_property ON stock_locations (organization_id, property_id);
CREATE TRIGGER trg_stock_locations_upd BEFORE UPDATE ON stock_locations FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE stock_levels (
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  item_id           uuid NOT NULL REFERENCES inventory_items(id) ON DELETE CASCADE,
  stock_location_id uuid NOT NULL REFERENCES stock_locations(id) ON DELETE CASCADE,
  quantity          numeric(12,2) NOT NULL DEFAULT 0 CHECK (quantity >= 0),   -- stok tidak boleh negatif (ditegakkan DB)
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (item_id, stock_location_id)
);

CREATE TABLE stock_transactions (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  property_id        uuid NOT NULL REFERENCES properties(location_id),
  transaction_number text NOT NULL,                                -- STK-2026-000001
  item_id            uuid NOT NULL REFERENCES inventory_items(id),
  stock_location_id  uuid NOT NULL REFERENCES stock_locations(id),
  transaction_type   text NOT NULL CHECK (transaction_type IN ('in','out','adjustment','usage','transfer_in','transfer_out')),
  quantity           numeric(12,2) NOT NULL,                       -- delta bertanda (+ masuk / − keluar)
  balance_after      numeric(12,2) NOT NULL,
  unit_cost          bigint,
  reference_type     text,                                         -- work_order | purchase | manual | transfer
  reference_id       uuid,
  note               text,
  performed_by       uuid,
  performed_at       timestamptz NOT NULL DEFAULT now(),
  created_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (organization_id, transaction_number)
);
CREATE INDEX idx_stock_tx_item ON stock_transactions (item_id, performed_at DESC);
CREATE INDEX idx_stock_tx_ref ON stock_transactions (reference_type, reference_id);

-- Parts Usage against Work Order (PRD §25: Asset → Work Order → Parts Usage → Inventory)
CREATE TABLE work_order_parts (
  id                   uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id      uuid NOT NULL REFERENCES organizations(id),
  work_order_id        uuid NOT NULL REFERENCES work_orders(id) ON DELETE CASCADE,
  item_id              uuid NOT NULL REFERENCES inventory_items(id),
  stock_location_id    uuid NOT NULL REFERENCES stock_locations(id),
  quantity             numeric(12,2) NOT NULL CHECK (quantity > 0),
  unit_cost            bigint NOT NULL DEFAULT 0,
  total_cost           bigint NOT NULL DEFAULT 0,
  stock_transaction_id uuid REFERENCES stock_transactions(id),
  note                 text,
  recorded_by          uuid,
  recorded_at          timestamptz NOT NULL DEFAULT now(),
  client_part_id       text
);
CREATE INDEX idx_wo_parts ON work_order_parts (work_order_id);
CREATE UNIQUE INDEX uq_wo_parts_client ON work_order_parts (organization_id, client_part_id) WHERE client_part_id IS NOT NULL;

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['vendors','vendor_contacts','inventory_items','stock_locations','stock_levels','stock_transactions','work_order_parts'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS work_order_parts, stock_transactions, stock_levels, stock_locations, inventory_items, vendor_contacts CASCADE;
ALTER TABLE work_orders DROP COLUMN IF EXISTS vendor_id, DROP COLUMN IF EXISTS vendor_assigned_at, DROP COLUMN IF EXISTS vendor_notes;
DROP TABLE IF EXISTS vendors CASCADE;
-- +goose StatementEnd
