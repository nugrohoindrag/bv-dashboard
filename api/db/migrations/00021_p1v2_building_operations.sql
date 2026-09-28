-- +goose Up
-- +goose StatementBegin
-- PRD P1 v2 (Roadmap v2.0) — Building Operations MVP: gap closure di atas P0 v2 (00020).
-- Semua perubahan backward-compatible (ADD COLUMN / CREATE TABLE / perluasan CHECK). Lihat ADR-015.

-- ---------- §6.3 Facility operasional (bukan hanya fasilitas booking) ----------
ALTER TABLE facilities DROP CONSTRAINT IF EXISTS facilities_facility_type_check;
ALTER TABLE facilities ADD CONSTRAINT facilities_facility_type_check CHECK (facility_type IN
  ('meeting_room','function_hall','gym','pool','court','bbq','coworking','lounge','parking','other',
   'lobby','lift','toilet','corridor','common_area'));
ALTER TABLE facilities
  ADD COLUMN status      text NOT NULL DEFAULT 'operational' CHECK (status IN ('operational','under_maintenance','closed','inactive')),
  ADD COLUMN is_bookable boolean NOT NULL DEFAULT true;
UPDATE facilities SET status = 'inactive' WHERE NOT is_active;
CREATE INDEX idx_facilities_location ON facilities (location_id) WHERE deleted_at IS NULL;

-- ---------- §7 Building Map / Floor Plan ----------
CREATE TABLE floor_plans (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  location_id     uuid NOT NULL REFERENCES locations(id),   -- building / tower / floor / area
  name            text NOT NULL,
  description     text,
  attachment_id   uuid REFERENCES attachments(id),           -- gambar denah (object_type floor_plan)
  image_width     int CHECK (image_width IS NULL OR image_width > 0),
  image_height    int CHECK (image_height IS NULL OR image_height > 0),
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1
);
CREATE INDEX idx_floor_plans_location ON floor_plans (organization_id, location_id) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_floor_plans_upd BEFORE UPDATE ON floor_plans FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE floor_plans ENABLE ROW LEVEL SECURITY; ALTER TABLE floor_plans FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON floor_plans USING (organization_id = app_current_org());

-- Marker: titik pada denah (persen terhadap lebar/tinggi gambar) yang menunjuk lokasi / facility / asset.
CREATE TABLE floor_plan_markers (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  floor_plan_id   uuid NOT NULL REFERENCES floor_plans(id) ON DELETE CASCADE,
  target_type     text NOT NULL CHECK (target_type IN ('location','facility','asset')),
  target_id       uuid NOT NULL,
  x_pct           numeric(6,3) NOT NULL CHECK (x_pct >= 0 AND x_pct <= 100),
  y_pct           numeric(6,3) NOT NULL CHECK (y_pct >= 0 AND y_pct <= 100),
  label           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (floor_plan_id, target_type, target_id)
);
CREATE INDEX idx_floor_plan_markers_target ON floor_plan_markers (organization_id, target_type, target_id);
CREATE TRIGGER trg_floor_plan_markers_upd BEFORE UPDATE ON floor_plan_markers FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE floor_plan_markers ENABLE ROW LEVEL SECURITY; ALTER TABLE floor_plan_markers FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON floor_plan_markers USING (organization_id = app_current_org());

-- ---------- §8 Occupancy: status Inactive ----------
ALTER TABLE units DROP CONSTRAINT IF EXISTS units_occupancy_status_check;
ALTER TABLE units ADD CONSTRAINT units_occupancy_status_check CHECK (occupancy_status IN ('vacant','occupied','reserved','inactive'));

-- ---------- §13 Task: category, escalation ----------
ALTER TABLE tasks
  ADD COLUMN category         text,
  ADD COLUMN escalated_at     timestamptz,
  ADD COLUMN escalation_level int NOT NULL DEFAULT 0;
CREATE INDEX idx_tasks_category ON tasks (organization_id, category) WHERE category IS NOT NULL;
CREATE INDEX idx_tasks_completed ON tasks (organization_id, completed_at) WHERE completed_at IS NOT NULL;

-- ---------- §22–§26 Work Order: tipe, Draft, cost breakdown, escalation ----------
ALTER TABLE work_orders DROP CONSTRAINT IF EXISTS work_orders_work_order_type_check;
ALTER TABLE work_orders ADD CONSTRAINT work_orders_work_order_type_check CHECK (work_order_type IN
  ('maintenance','corrective','preventive','inspection','repair','service','other'));
ALTER TABLE work_orders DROP CONSTRAINT IF EXISTS work_orders_status_check;
ALTER TABLE work_orders ADD CONSTRAINT work_orders_status_check CHECK (status IN
  ('draft','new','scheduled','assigned','in_progress','on_hold','completed','closed','cancelled'));
ALTER TABLE work_orders
  ADD COLUMN parts_cost_amount   bigint CHECK (parts_cost_amount IS NULL OR parts_cost_amount >= 0),
  ADD COLUMN service_cost_amount bigint CHECK (service_cost_amount IS NULL OR service_cost_amount >= 0),
  ADD COLUMN other_cost_amount   bigint CHECK (other_cost_amount IS NULL OR other_cost_amount >= 0),
  ADD COLUMN submitted_at        timestamptz,
  ADD COLUMN escalated_at        timestamptz,
  ADD COLUMN escalation_level    int NOT NULL DEFAULT 0;
CREATE INDEX idx_wo_completed ON work_orders (organization_id, completed_at) WHERE completed_at IS NOT NULL;
-- parts cost awal dari parts usage yang sudah tercatat (inventory P1 v1.3)
UPDATE work_orders w SET parts_cost_amount = s.total
  FROM (SELECT work_order_id, sum(total_cost)::bigint AS total FROM work_order_parts GROUP BY work_order_id) s
  WHERE s.work_order_id = w.id;

-- ---------- §19 Evidence: During photo ----------
ALTER TABLE attachments DROP CONSTRAINT IF EXISTS attachments_attachment_type_check;
ALTER TABLE attachments ADD CONSTRAINT attachments_attachment_type_check CHECK (attachment_type IN
  ('photo','photo_before','photo_during','photo_after','checklist_item_photo','document','signature'));

-- ---------- §21 SLA: response target breach ----------
ALTER TABLE sla_tracking ADD COLUMN response_breached_at timestamptz;

-- ---------- §27 Tenant Service Request: request type ----------
ALTER TABLE service_request_categories
  ADD COLUMN request_type text CHECK (request_type IS NULL OR request_type IN
    ('service_request','complaint','maintenance_request','cleaning_request','facility_issue','other'));
UPDATE service_request_categories SET request_type = CASE
  WHEN code = 'complaint' OR code = 'noise' THEN 'complaint'
  WHEN code IN ('maintenance','plumbing','electrical','air_conditioning','lift','building_damage','renovation') THEN 'maintenance_request'
  WHEN code IN ('cleaning','cleanliness','pest','gardening') THEN 'cleaning_request'
  WHEN code IN ('facility') THEN 'facility_issue'
  WHEN code = 'other' THEN 'other'
  ELSE 'service_request' END;
ALTER TABLE service_requests
  ADD COLUMN request_type text NOT NULL DEFAULT 'service_request' CHECK (request_type IN
    ('service_request','complaint','maintenance_request','cleaning_request','facility_issue','other'));
UPDATE service_requests sr SET request_type = COALESCE(c.request_type, 'service_request')
  FROM service_request_categories c WHERE c.id = sr.category_id;
CREATE INDEX idx_sr_request_type ON service_requests (organization_id, request_type);

-- ---------- §33 Incident: action taken ----------
ALTER TABLE incidents ADD COLUMN action_taken text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE incidents DROP COLUMN IF EXISTS action_taken;
DROP INDEX IF EXISTS idx_sr_request_type;
ALTER TABLE service_requests DROP COLUMN IF EXISTS request_type;
ALTER TABLE service_request_categories DROP COLUMN IF EXISTS request_type;
ALTER TABLE sla_tracking DROP COLUMN IF EXISTS response_breached_at;
UPDATE attachments SET attachment_type = 'photo' WHERE attachment_type = 'photo_during';
ALTER TABLE attachments DROP CONSTRAINT IF EXISTS attachments_attachment_type_check;
ALTER TABLE attachments ADD CONSTRAINT attachments_attachment_type_check CHECK (attachment_type IN ('photo','photo_before','photo_after','checklist_item_photo','document','signature'));
DROP INDEX IF EXISTS idx_wo_completed;
ALTER TABLE work_orders DROP COLUMN IF EXISTS escalation_level, DROP COLUMN IF EXISTS escalated_at, DROP COLUMN IF EXISTS submitted_at,
  DROP COLUMN IF EXISTS other_cost_amount, DROP COLUMN IF EXISTS service_cost_amount, DROP COLUMN IF EXISTS parts_cost_amount;
UPDATE work_orders SET status = 'cancelled', cancelled_at = now() WHERE status = 'draft';
ALTER TABLE work_orders DROP CONSTRAINT IF EXISTS work_orders_status_check;
ALTER TABLE work_orders ADD CONSTRAINT work_orders_status_check CHECK (status IN ('new','scheduled','assigned','in_progress','on_hold','completed','closed','cancelled'));
UPDATE work_orders SET work_order_type = 'corrective' WHERE work_order_type IN ('preventive','inspection','other');
ALTER TABLE work_orders DROP CONSTRAINT IF EXISTS work_orders_work_order_type_check;
ALTER TABLE work_orders ADD CONSTRAINT work_orders_work_order_type_check CHECK (work_order_type IN ('maintenance','corrective','repair','service'));
DROP INDEX IF EXISTS idx_tasks_completed;
DROP INDEX IF EXISTS idx_tasks_category;
ALTER TABLE tasks DROP COLUMN IF EXISTS escalation_level, DROP COLUMN IF EXISTS escalated_at, DROP COLUMN IF EXISTS category;
UPDATE units SET occupancy_status = 'vacant' WHERE occupancy_status = 'inactive';
ALTER TABLE units DROP CONSTRAINT IF EXISTS units_occupancy_status_check;
ALTER TABLE units ADD CONSTRAINT units_occupancy_status_check CHECK (occupancy_status IN ('vacant','occupied','reserved'));
DROP TABLE IF EXISTS floor_plan_markers;
DROP TABLE IF EXISTS floor_plans;
DROP INDEX IF EXISTS idx_facilities_location;
ALTER TABLE facilities DROP COLUMN IF EXISTS is_bookable, DROP COLUMN IF EXISTS status;
UPDATE facilities SET facility_type = 'other' WHERE facility_type IN ('lobby','lift','toilet','corridor','common_area');
ALTER TABLE facilities DROP CONSTRAINT IF EXISTS facilities_facility_type_check;
ALTER TABLE facilities ADD CONSTRAINT facilities_facility_type_check CHECK (facility_type IN ('meeting_room','function_hall','gym','pool','court','bbq','coworking','lounge','parking','other'));
-- +goose StatementEnd
