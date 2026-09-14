-- +goose Up
-- +goose StatementBegin
-- ============ ENGINEERING (PRD §12) ============
CREATE TABLE maintenance_plans (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  plan_code        text NOT NULL,                   -- PM-2026-000001
  name             text NOT NULL,
  asset_id         uuid NOT NULL REFERENCES assets(id),
  frequency        text NOT NULL CHECK (frequency IN ('daily','weekly','biweekly','monthly','quarterly','semiannual','annual','custom_days')),
  interval_days    int,                             -- untuk custom_days
  start_date       date NOT NULL,
  end_date         date,
  checklist_template_id uuid REFERENCES checklist_templates(id),
  default_priority text NOT NULL DEFAULT 'medium' CHECK (default_priority IN ('low','medium','high','critical')),
  responsible_team_id uuid REFERENCES teams(id),
  lead_time_days   int NOT NULL DEFAULT 0,          -- TD-006: WO dibuat H-n sebelum due
  duration_minutes int,
  status           text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
  description      text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, plan_code)
);
CREATE INDEX idx_mp_asset ON maintenance_plans (asset_id) WHERE status = 'published';
CREATE TRIGGER trg_maintenance_plans_upd BEFORE UPDATE ON maintenance_plans FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE maintenance_schedules (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  plan_id          uuid NOT NULL REFERENCES maintenance_plans(id) ON DELETE CASCADE,
  asset_id         uuid NOT NULL REFERENCES assets(id),
  due_date         date NOT NULL,
  due_at           timestamptz NOT NULL,
  status           text NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','due','in_progress','completed','overdue','skipped','cancelled')),
  work_order_id    uuid REFERENCES work_orders(id),
  completed_at     timestamptz,
  skipped_reason   text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  version          int NOT NULL DEFAULT 1,
  UNIQUE (plan_id, due_date)                        -- idempotent generator
);
CREATE INDEX idx_ms_due ON maintenance_schedules (organization_id, property_id, status, due_date);
CREATE TRIGGER trg_maintenance_schedules_upd BEFORE UPDATE ON maintenance_schedules FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE work_orders ADD CONSTRAINT fk_wo_maintenance_schedule FOREIGN KEY (maintenance_schedule_id) REFERENCES maintenance_schedules(id);

-- Inspection = Task extension (task_type = inspection), 1:1
CREATE TABLE inspections (
  task_id          uuid PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  inspection_number text NOT NULL,                  -- INS-2026-000001
  inspection_type  text NOT NULL DEFAULT 'engineering' CHECK (inspection_type IN ('engineering','safety','housekeeping','general')),
  result           text CHECK (result IN ('pass','fail','partial')),
  result_notes     text,
  UNIQUE (organization_id, inspection_number)
);

-- ============ SECURITY (PRD §13) ============
CREATE TABLE patrol_routes (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  name            text NOT NULL,
  description     text,
  estimated_minutes int,
  checklist_template_id uuid REFERENCES checklist_templates(id),
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_patrol_routes_upd BEFORE UPDATE ON patrol_routes FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE checkpoints (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  name            text NOT NULL,
  location_id     uuid NOT NULL REFERENCES locations(id),
  qr_code         text UNIQUE,
  instructions    text,
  checklist_template_id uuid REFERENCES checklist_templates(id),
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_checkpoints_upd BEFORE UPDATE ON checkpoints FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE patrol_route_checkpoints (
  route_id       uuid NOT NULL REFERENCES patrol_routes(id) ON DELETE CASCADE,
  checkpoint_id  uuid NOT NULL REFERENCES checkpoints(id) ON DELETE CASCADE,
  sort_order     int NOT NULL,
  expected_offset_minutes int,                      -- menit dari mulai patrol
  PRIMARY KEY (route_id, checkpoint_id),
  UNIQUE (route_id, sort_order)
);

CREATE TABLE patrol_schedules (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  route_id        uuid NOT NULL REFERENCES patrol_routes(id),
  name            text NOT NULL,
  start_time      time NOT NULL,                    -- waktu lokal property
  duration_minutes int NOT NULL DEFAULT 60,         -- due = start + duration
  weekdays        int[] NOT NULL DEFAULT '{0,1,2,3,4,5,6}',
  responsible_team_id uuid REFERENCES teams(id),
  default_assignee_user_id uuid REFERENCES users(id),
  priority        text NOT NULL DEFAULT 'medium' CHECK (priority IN ('low','medium','high','critical')),
  is_active       boolean NOT NULL DEFAULT true,
  valid_from      date,
  valid_until     date,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_patrol_schedules_upd BEFORE UPDATE ON patrol_schedules FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- Patrol Task = Task extension (task_type = patrol), 1:1
CREATE TABLE patrol_tasks (
  task_id          uuid PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  route_id         uuid NOT NULL REFERENCES patrol_routes(id),
  patrol_schedule_id uuid REFERENCES patrol_schedules(id),
  schedule_date    date,
  total_checkpoints int NOT NULL DEFAULT 0,
  scanned_checkpoints int NOT NULL DEFAULT 0,
  missed_checkpoints int NOT NULL DEFAULT 0,
  UNIQUE (patrol_schedule_id, schedule_date)        -- idempotent generator
);

CREATE TABLE checkpoint_scans (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  patrol_task_id   uuid NOT NULL REFERENCES patrol_tasks(task_id) ON DELETE CASCADE,
  checkpoint_id    uuid NOT NULL REFERENCES checkpoints(id),
  sort_order       int NOT NULL,
  status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','scanned','missed')),
  scanned_at       timestamptz,
  scanned_by       uuid,
  scan_method      text CHECK (scan_method IN ('qr','manual')),
  gps_lat          double precision,
  gps_lng          double precision,
  gps_status       text CHECK (gps_status IN ('captured','unavailable','denied')),
  missed_reason    text,
  note             text,
  client_scan_id   text,
  UNIQUE (patrol_task_id, checkpoint_id)
);
CREATE UNIQUE INDEX uq_checkpoint_scans_client ON checkpoint_scans (organization_id, client_scan_id) WHERE client_scan_id IS NOT NULL;

-- ============ HOUSEKEEPING (PRD §15) ============
CREATE TABLE cleaning_schedules (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  name            text NOT NULL,
  location_id     uuid NOT NULL REFERENCES locations(id),
  cleaning_type   text NOT NULL DEFAULT 'routine' CHECK (cleaning_type IN ('routine','periodic','deep','spot','special')),
  start_time      time NOT NULL,
  duration_minutes int NOT NULL DEFAULT 60,
  weekdays        int[] NOT NULL DEFAULT '{0,1,2,3,4,5,6}',
  checklist_template_id uuid REFERENCES checklist_templates(id),
  responsible_team_id uuid REFERENCES teams(id),
  default_assignee_user_id uuid REFERENCES users(id),
  priority        text NOT NULL DEFAULT 'medium' CHECK (priority IN ('low','medium','high','critical')),
  requires_photo  boolean NOT NULL DEFAULT true,
  is_active       boolean NOT NULL DEFAULT true,
  valid_from      date,
  valid_until     date,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_cleaning_schedules_upd BEFORE UPDATE ON cleaning_schedules FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- Cleaning Task = Task extension (task_type = cleaning), 1:1
CREATE TABLE cleaning_tasks (
  task_id             uuid PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
  organization_id     uuid NOT NULL REFERENCES organizations(id),
  cleaning_schedule_id uuid REFERENCES cleaning_schedules(id),
  schedule_date       date,
  cleaning_type       text NOT NULL DEFAULT 'routine' CHECK (cleaning_type IN ('routine','periodic','deep','spot','special')),
  inspection_status   text NOT NULL DEFAULT 'not_inspected' CHECK (inspection_status IN ('not_inspected','passed','failed','rework_required')),
  UNIQUE (cleaning_schedule_id, schedule_date)
);

-- Housekeeping Inspection = Task extension (task_type = inspection) yang menunjuk cleaning task
CREATE TABLE housekeeping_inspections (
  task_id           uuid PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  cleaning_task_id  uuid NOT NULL REFERENCES cleaning_tasks(task_id),
  inspector_user_id uuid REFERENCES users(id),
  result            text CHECK (result IN ('pass','fail','partial')),
  score             int CHECK (score BETWEEN 0 AND 100),
  result_notes      text
);

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['maintenance_plans','maintenance_schedules','inspections','patrol_routes','checkpoints','patrol_schedules','patrol_tasks','checkpoint_scans','cleaning_schedules','cleaning_tasks','housekeeping_inspections'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS housekeeping_inspections, cleaning_tasks, cleaning_schedules, checkpoint_scans, patrol_tasks, patrol_schedules, patrol_route_checkpoints, checkpoints, patrol_routes, inspections, maintenance_schedules, maintenance_plans CASCADE;
