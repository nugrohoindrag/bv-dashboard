-- +goose Up
-- +goose StatementBegin
-- PRD P2 v2.1 (Roadmap v2.1 §8.1 Engineering, §10 Housekeeping) — dokumen equipment bertipe + kedaluwarsa (P2-DOC-*),
-- equipment health score (P2-EQH-*, keputusan D-P2-01: P2; tanpa downtime D-P2-02), inspeksi engineering terjadwal dari
-- maintenance plan (P2-INS-02), cleaning route (P2-RTE-*), consumable per cleaning task (P2-CNS-02). Lihat ADR-017.

-- ---------- §5.6 Equipment documents ----------
CREATE TABLE asset_documents (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  asset_id         uuid NOT NULL REFERENCES assets(id),
  document_code    text NOT NULL,                              -- DOC-2026-000001
  document_type    text NOT NULL DEFAULT 'other' CHECK (document_type IN ('manual','warranty','certificate','permit','inspection_report','drawing','contract','other')),
  title            text NOT NULL,
  document_number  text,
  issuer           text,
  issued_on        date,
  expires_on       date,
  attachment_id    uuid REFERENCES attachments(id),
  notes            text,
  reminder_stage   int NOT NULL DEFAULT 0,                     -- 0 belum, 1 = H-30, 2 = H-7, 3 = kedaluwarsa
  last_reminded_at timestamptz,
  is_active        boolean NOT NULL DEFAULT true,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, document_code)
);
CREATE INDEX idx_asset_documents_asset ON asset_documents (asset_id) WHERE is_active;
CREATE INDEX idx_asset_documents_expiry ON asset_documents (organization_id, expires_on) WHERE is_active AND expires_on IS NOT NULL;
CREATE TRIGGER trg_asset_documents_upd BEFORE UPDATE ON asset_documents FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
-- pengingat warranty asset (warranty_until sudah ada di assets)
ALTER TABLE assets ADD COLUMN warranty_reminder_stage int NOT NULL DEFAULT 0;

-- ---------- §5.6 Equipment health (Healthy 90–100 · Warning 70–89 · Critical < 70 · Offline · Unknown; NC §14) ----------
ALTER TABLE assets
  ADD COLUMN health_score      int CHECK (health_score IS NULL OR health_score BETWEEN 0 AND 100),
  ADD COLUMN health_status     text NOT NULL DEFAULT 'unknown' CHECK (health_status IN ('healthy','warning','critical','offline','unknown')),
  ADD COLUMN health_factors    jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN health_updated_at timestamptz;
CREATE INDEX idx_assets_health ON assets (organization_id, property_id, health_status);

-- ---------- §5.4 Inspeksi engineering terjadwal: maintenance plan dapat menghasilkan inspeksi (task) alih-alih WO ----------
ALTER TABLE maintenance_plans ADD COLUMN output_type text NOT NULL DEFAULT 'work_order' CHECK (output_type IN ('work_order','inspection'));
ALTER TABLE maintenance_schedules ADD COLUMN task_id uuid REFERENCES tasks(id);

-- ---------- §7.3 Cleaning route ----------
CREATE TABLE cleaning_routes (
  id                       uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id          uuid NOT NULL REFERENCES organizations(id),
  property_id              uuid NOT NULL REFERENCES properties(location_id),
  route_code               text NOT NULL,                     -- CRT-000001
  name                     text NOT NULL,
  description              text,
  cleaning_type            text NOT NULL DEFAULT 'routine' CHECK (cleaning_type IN ('routine','periodic','deep','spot','special')),
  shift_id                 uuid REFERENCES shift_definitions(id),  -- P2-SHF-04
  start_time               time NOT NULL DEFAULT '08:00',
  weekdays                 int[] NOT NULL DEFAULT '{0,1,2,3,4,5,6}',
  responsible_team_id      uuid REFERENCES teams(id),
  default_assignee_user_id uuid REFERENCES users(id),
  checklist_template_id    uuid REFERENCES checklist_templates(id),
  requires_photo           boolean NOT NULL DEFAULT false,
  priority                 text NOT NULL DEFAULT 'medium' CHECK (priority IN ('low','medium','high','critical')),
  is_active                boolean NOT NULL DEFAULT true,
  created_at               timestamptz NOT NULL DEFAULT now(),
  created_by               uuid,
  updated_at               timestamptz NOT NULL DEFAULT now(),
  updated_by               uuid,
  version                  int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, route_code)
);
CREATE TRIGGER trg_cleaning_routes_upd BEFORE UPDATE ON cleaning_routes FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE cleaning_route_stops (
  id                    uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id       uuid NOT NULL REFERENCES organizations(id),
  route_id              uuid NOT NULL REFERENCES cleaning_routes(id) ON DELETE CASCADE,
  location_id           uuid NOT NULL REFERENCES locations(id),
  sort_order            int NOT NULL,
  estimated_minutes     int NOT NULL DEFAULT 15 CHECK (estimated_minutes > 0),
  checklist_template_id uuid REFERENCES checklist_templates(id),
  notes                 text,
  UNIQUE (route_id, sort_order)
);

CREATE TABLE cleaning_route_runs (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  route_id         uuid NOT NULL REFERENCES cleaning_routes(id),
  run_date         date NOT NULL,
  assignee_user_id uuid REFERENCES users(id),
  team_id          uuid REFERENCES teams(id),
  status           text NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','in_progress','completed','cancelled')),
  total_stops      int NOT NULL DEFAULT 0,
  completed_stops  int NOT NULL DEFAULT 0,
  started_at       timestamptz,
  completed_at     timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  version          int NOT NULL DEFAULT 1,
  UNIQUE (route_id, run_date)
);
CREATE INDEX idx_cleaning_route_runs ON cleaning_route_runs (organization_id, property_id, run_date);
CREATE TRIGGER trg_cleaning_route_runs_upd BEFORE UPDATE ON cleaning_route_runs FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE cleaning_tasks ADD COLUMN route_run_id uuid REFERENCES cleaning_route_runs(id), ADD COLUMN route_stop_order int;
CREATE INDEX idx_cleaning_tasks_route_run ON cleaning_tasks (route_run_id) WHERE route_run_id IS NOT NULL;

-- ---------- §7.5 Consumable per cleaning task ----------
CREATE TABLE task_consumables (
  id                   uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id      uuid NOT NULL REFERENCES organizations(id),
  task_id              uuid NOT NULL REFERENCES tasks(id),
  item_id              uuid NOT NULL REFERENCES inventory_items(id),
  stock_location_id    uuid NOT NULL REFERENCES stock_locations(id),
  quantity             numeric(12,3) NOT NULL CHECK (quantity > 0),
  unit_cost            bigint,
  total_cost           bigint,
  stock_transaction_id uuid REFERENCES stock_transactions(id),
  client_ref           text,                                   -- idempoten (sync / client_mutation_id)
  note                 text,
  recorded_by          uuid REFERENCES users(id),
  recorded_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_task_consumables_task ON task_consumables (task_id);
CREATE UNIQUE INDEX uq_task_consumables_client ON task_consumables (organization_id, client_ref) WHERE client_ref IS NOT NULL;

DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['asset_documents','cleaning_routes','cleaning_route_stops','cleaning_route_runs','task_consumables'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS task_consumables;
ALTER TABLE cleaning_tasks DROP COLUMN IF EXISTS route_stop_order, DROP COLUMN IF EXISTS route_run_id;
DROP TABLE IF EXISTS cleaning_route_runs;
DROP TABLE IF EXISTS cleaning_route_stops;
DROP TABLE IF EXISTS cleaning_routes;
ALTER TABLE maintenance_schedules DROP COLUMN IF EXISTS task_id;
ALTER TABLE maintenance_plans DROP COLUMN IF EXISTS output_type;
DROP INDEX IF EXISTS idx_assets_health;
ALTER TABLE assets DROP COLUMN IF EXISTS health_updated_at, DROP COLUMN IF EXISTS health_factors, DROP COLUMN IF EXISTS health_status, DROP COLUMN IF EXISTS health_score,
  DROP COLUMN IF EXISTS warranty_reminder_stage;
DROP TABLE IF EXISTS asset_documents;
-- +goose StatementEnd
