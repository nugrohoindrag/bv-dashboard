-- +goose Up
-- +goose StatementBegin
-- ============ TASK ENGINE (PRD §9–§11, TAD §5.8, §7.4, ADR-007) ============

CREATE TABLE sla_policies (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  property_id        uuid REFERENCES properties(location_id),   -- NULL = default organization
  object_type        text NOT NULL CHECK (object_type IN ('task','work_order','service_request','incident')),
  priority           text NOT NULL CHECK (priority IN ('low','medium','high','critical')),
  response_minutes   int,
  resolution_minutes int NOT NULL,
  risk_threshold_pct int NOT NULL DEFAULT 75,
  calendar           text NOT NULL DEFAULT 'always' CHECK (calendar IN ('always','business_hours')),
  is_active          boolean NOT NULL DEFAULT true,
  created_at         timestamptz NOT NULL DEFAULT now(),
  created_by         uuid,
  updated_at         timestamptz NOT NULL DEFAULT now(),
  updated_by         uuid,
  version            int NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX uq_sla_policies ON sla_policies (organization_id, COALESCE(property_id,'00000000-0000-0000-0000-000000000000'::uuid), object_type, priority) WHERE is_active;
CREATE TRIGGER trg_sla_policies_upd BEFORE UPDATE ON sla_policies FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE sla_tracking (
  object_type        text NOT NULL,
  object_id          uuid NOT NULL,
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  policy_id          uuid REFERENCES sla_policies(id),
  started_at         timestamptz NOT NULL DEFAULT now(),
  response_due_at    timestamptz,
  resolution_due_at  timestamptz,
  responded_at       timestamptz,
  resolved_at        timestamptz,
  risk_threshold_pct int NOT NULL DEFAULT 75,
  sla_risk_at        timestamptz,
  sla_breached_at    timestamptz,
  escalated_at       timestamptz,
  paused_at          timestamptz,
  paused_minutes     int NOT NULL DEFAULT 0,
  PRIMARY KEY (object_type, object_id)
);
CREATE INDEX idx_sla_tracking_open ON sla_tracking (organization_id, resolution_due_at) WHERE resolved_at IS NULL;

-- Checklist template (authoring: draft|published|archived; TD-001)
CREATE TABLE checklist_templates (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  code            text NOT NULL,                    -- CHK-000001
  name            text NOT NULL,
  description     text,
  domain          text,                             -- engineering | security | housekeeping | general
  applies_to      text[] NOT NULL DEFAULT '{}',     -- task | work_order | inspection | patrol | cleaning
  status          text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
  current_version int NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, code)
);
CREATE TRIGGER trg_checklist_templates_upd BEFORE UPDATE ON checklist_templates FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE checklist_template_items (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  template_id      uuid NOT NULL REFERENCES checklist_templates(id) ON DELETE CASCADE,
  template_version int NOT NULL DEFAULT 1,
  sort_order       int NOT NULL DEFAULT 0,
  section          text,
  label            text NOT NULL,
  item_type        text NOT NULL CHECK (item_type IN ('ok_notok_na','yes_no','numeric','text','photo')),
  is_required      boolean NOT NULL DEFAULT true,
  photo_required   boolean NOT NULL DEFAULT false,
  numeric_unit     text,
  numeric_min      numeric,
  numeric_max      numeric,
  help_text        text
);
CREATE INDEX idx_cti_template ON checklist_template_items (template_id, template_version, sort_order);

-- ---------------- TASK ----------------
CREATE TABLE tasks (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  property_id        uuid NOT NULL REFERENCES properties(location_id),
  task_number        text NOT NULL,                 -- TSK-2026-000001
  task_type          text NOT NULL DEFAULT 'general' CHECK (task_type IN ('general','patrol','cleaning','inspection','routine_maintenance')),
  title              text NOT NULL,
  description        text,
  location_id        uuid REFERENCES locations(id),
  asset_id           uuid REFERENCES assets(id),
  priority           text NOT NULL DEFAULT 'medium' CHECK (priority IN ('low','medium','high','critical')),
  status             text NOT NULL DEFAULT 'new' CHECK (status IN ('new','scheduled','assigned','in_progress','on_hold','completed','closed','cancelled')),
  scheduled_start_at timestamptz,
  due_at             timestamptz,
  started_at         timestamptz,
  completed_at       timestamptz,
  closed_at          timestamptz,
  cancelled_at       timestamptz,
  checklist_template_id uuid REFERENCES checklist_templates(id),
  requires_photo     boolean NOT NULL DEFAULT false,
  completion_notes   text,
  is_overdue         boolean NOT NULL DEFAULT false,
  sla_risk_at        timestamptz,
  sla_breached_at    timestamptz,
  evidence_incomplete boolean NOT NULL DEFAULT false,
  -- denormalized current assignment for list/overview
  assignee_user_id   uuid REFERENCES users(id),
  assignee_team_id   uuid REFERENCES teams(id),
  source_type        text,                          -- service_request | finding | incident | schedule | manual
  source_id          uuid,
  created_at         timestamptz NOT NULL DEFAULT now(),
  created_by         uuid,
  updated_at         timestamptz NOT NULL DEFAULT now(),
  updated_by         uuid,
  version            int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, task_number)
);
CREATE INDEX idx_tasks_list ON tasks (organization_id, property_id, status, due_at);
CREATE INDEX idx_tasks_type ON tasks (organization_id, property_id, task_type, status);
CREATE INDEX idx_tasks_overdue ON tasks (organization_id, property_id) WHERE is_overdue;
CREATE INDEX idx_tasks_sla_risk ON tasks (organization_id, property_id) WHERE sla_risk_at IS NOT NULL AND status NOT IN ('closed','cancelled');
CREATE INDEX idx_tasks_assignee ON tasks (assignee_user_id) WHERE status NOT IN ('closed','cancelled');
CREATE INDEX idx_tasks_team ON tasks (assignee_team_id) WHERE status NOT IN ('closed','cancelled');
CREATE INDEX idx_tasks_location ON tasks (location_id);
CREATE TRIGGER trg_tasks_upd BEFORE UPDATE ON tasks FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------------- WORK ORDER ----------------
CREATE TABLE work_orders (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  property_id        uuid NOT NULL REFERENCES properties(location_id),
  work_order_number  text NOT NULL,                 -- WO-2026-000001
  work_order_type    text NOT NULL CHECK (work_order_type IN ('maintenance','corrective','repair','service')),
  title              text NOT NULL,
  description        text,
  location_id        uuid NOT NULL REFERENCES locations(id),   -- FR-WO-003 wajib
  asset_id           uuid REFERENCES assets(id),
  priority           text NOT NULL DEFAULT 'medium' CHECK (priority IN ('low','medium','high','critical')),
  status             text NOT NULL DEFAULT 'new' CHECK (status IN ('new','scheduled','assigned','in_progress','on_hold','completed','closed','cancelled')),
  scheduled_start_at timestamptz,
  due_at             timestamptz,
  started_at         timestamptz,
  completed_at       timestamptz,
  closed_at          timestamptz,
  cancelled_at       timestamptz,
  checklist_template_id uuid REFERENCES checklist_templates(id),
  requires_evidence  boolean NOT NULL DEFAULT true, -- After photo wajib saat complete (PRD §11.2)
  completion_notes   text,
  resolution         text,
  estimated_cost_amount bigint,
  actual_cost_amount    bigint,
  currency_code      char(3) NOT NULL DEFAULT 'IDR',
  parts_usage        text,                          -- free text (FR-WO-017)
  vendor_reference   text,                          -- free text (PRD §6.3)
  reopen_count       int NOT NULL DEFAULT 0,
  is_overdue         boolean NOT NULL DEFAULT false,
  sla_risk_at        timestamptz,
  sla_breached_at    timestamptz,
  evidence_incomplete boolean NOT NULL DEFAULT false,
  assignee_user_id   uuid REFERENCES users(id),
  assignee_team_id   uuid REFERENCES teams(id),
  requester_user_id  uuid REFERENCES users(id),
  source_type        text,                          -- service_request | finding | incident | maintenance_schedule | manual
  source_id          uuid,
  maintenance_schedule_id uuid,                     -- FK ditambahkan di migration engineering
  created_at         timestamptz NOT NULL DEFAULT now(),
  created_by         uuid,
  updated_at         timestamptz NOT NULL DEFAULT now(),
  updated_by         uuid,
  version            int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, work_order_number)
);
CREATE INDEX idx_wo_list ON work_orders (organization_id, property_id, status, due_at);
CREATE INDEX idx_wo_type ON work_orders (organization_id, property_id, work_order_type, status);
CREATE INDEX idx_wo_overdue ON work_orders (organization_id, property_id) WHERE is_overdue;
CREATE INDEX idx_wo_sla_risk ON work_orders (organization_id, property_id) WHERE sla_risk_at IS NOT NULL AND status NOT IN ('closed','cancelled');
CREATE INDEX idx_wo_assignee ON work_orders (assignee_user_id) WHERE status NOT IN ('closed','cancelled');
CREATE INDEX idx_wo_team ON work_orders (assignee_team_id) WHERE status NOT IN ('closed','cancelled');
CREATE INDEX idx_wo_asset ON work_orders (asset_id);
CREATE INDEX idx_wo_location ON work_orders (location_id);
CREATE TRIGGER trg_work_orders_upd BEFORE UPDATE ON work_orders FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------------- INCIDENT (PRD §14) ----------------
CREATE TABLE incidents (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  incident_number  text NOT NULL,                   -- INC-2026-000001
  incident_type    text NOT NULL DEFAULT 'security' CHECK (incident_type IN ('security','safety','building')),
  category         text NOT NULL,                   -- unauthorized_access | suspicious_activity | property_damage | lost_property | fire_smoke | emergency | other
  title            text NOT NULL,
  description      text,
  location_id      uuid REFERENCES locations(id),
  severity         text NOT NULL DEFAULT 'medium' CHECK (severity IN ('low','medium','high','critical')),
  priority         text NOT NULL DEFAULT 'medium' CHECK (priority IN ('low','medium','high','critical')),
  status           text NOT NULL DEFAULT 'new' CHECK (status IN ('new','assigned','in_progress','resolved','closed','cancelled')),
  reported_by      uuid REFERENCES users(id),
  reported_at      timestamptz NOT NULL DEFAULT now(),
  occurred_at      timestamptz,
  assignee_user_id uuid REFERENCES users(id),
  assignee_team_id uuid REFERENCES teams(id),
  resolution       text,
  resolved_at      timestamptz,
  closed_at        timestamptz,
  sla_risk_at      timestamptz,
  sla_breached_at  timestamptz,
  source_type      text,                            -- finding | patrol_task | manual
  source_id        uuid,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, incident_number)
);
CREATE INDEX idx_incidents_list ON incidents (organization_id, property_id, status, severity);
CREATE TRIGGER trg_incidents_upd BEFORE UPDATE ON incidents FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------------- FINDING (PRD §12.4, §13, §15.2) ----------------
CREATE TABLE findings (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  finding_number   text NOT NULL,                   -- FND-2026-000001
  finding_type     text NOT NULL DEFAULT 'general' CHECK (finding_type IN ('general','patrol','inspection','housekeeping','checklist')),
  category         text,                            -- water_leakage | fire_exit_blocked | odor | damage | ...
  title            text NOT NULL,
  description      text,
  location_id      uuid REFERENCES locations(id),
  asset_id         uuid REFERENCES assets(id),
  severity         text NOT NULL DEFAULT 'medium' CHECK (severity IN ('low','medium','high','critical')),
  status           text NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','resolved','closed')),
  source_type      text,                            -- task | patrol_task | inspection | housekeeping_inspection | checklist_run_item
  source_id        uuid,
  reported_by      uuid REFERENCES users(id),
  reported_at      timestamptz NOT NULL DEFAULT now(),
  resolution       text,
  resolved_by      uuid,
  resolved_at      timestamptz,
  closed_at        timestamptz,
  escalated_at     timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, finding_number)
);
CREATE INDEX idx_findings_list ON findings (organization_id, property_id, status, severity);
CREATE INDEX idx_findings_source ON findings (source_type, source_id);
CREATE TRIGGER trg_findings_upd BEFORE UPDATE ON findings FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------------- SHARED SUB-ENTITIES ----------------
CREATE TABLE assignments (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  object_type      text NOT NULL,
  object_id        uuid NOT NULL,
  assignee_user_id uuid REFERENCES users(id),
  assignee_team_id uuid REFERENCES teams(id),
  assigned_by      uuid,
  assigned_at      timestamptz NOT NULL DEFAULT now(),
  unassigned_at    timestamptz,
  is_current       boolean NOT NULL DEFAULT true,
  note             text,
  CHECK (assignee_user_id IS NOT NULL OR assignee_team_id IS NOT NULL)
);
CREATE INDEX idx_assignments_object ON assignments (object_type, object_id) WHERE is_current;
CREATE INDEX idx_assignments_user ON assignments (assignee_user_id, object_type) WHERE is_current;
CREATE INDEX idx_assignments_team ON assignments (assignee_team_id, object_type) WHERE is_current;

CREATE TABLE checklist_runs (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  object_type      text NOT NULL,
  object_id        uuid NOT NULL,
  template_id      uuid NOT NULL REFERENCES checklist_templates(id),
  template_version int NOT NULL,
  template_name    text NOT NULL,                   -- snapshot
  status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','in_progress','completed')),
  total_items      int NOT NULL DEFAULT 0,
  answered_items   int NOT NULL DEFAULT 0,
  not_ok_items     int NOT NULL DEFAULT 0,
  started_at       timestamptz,
  completed_at     timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_checklist_runs_object ON checklist_runs (object_type, object_id);

CREATE TABLE checklist_run_items (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  run_id           uuid NOT NULL REFERENCES checklist_runs(id) ON DELETE CASCADE,
  template_item_id uuid,
  sort_order       int NOT NULL DEFAULT 0,
  section          text,
  label            text NOT NULL,                   -- snapshot
  item_type        text NOT NULL CHECK (item_type IN ('ok_notok_na','yes_no','numeric','text','photo')),
  is_required      boolean NOT NULL DEFAULT true,
  photo_required   boolean NOT NULL DEFAULT false,
  numeric_unit     text,
  numeric_min      numeric,
  numeric_max      numeric,
  result_value     text,                            -- ok | not_ok | na | yes | no
  result_number    numeric,
  result_text      text,
  attachment_id    uuid REFERENCES attachments(id),
  note             text,
  answered_by      uuid,
  answered_at      timestamptz,
  answered_source  text CHECK (answered_source IN ('web','mobile','sync')),
  finding_id       uuid REFERENCES findings(id)
);
CREATE INDEX idx_cri_run ON checklist_run_items (run_id, sort_order);

CREATE TABLE comments (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  object_type     text NOT NULL,
  object_id       uuid NOT NULL,
  author_id       uuid NOT NULL REFERENCES users(id),
  body            text NOT NULL,
  is_internal     boolean NOT NULL DEFAULT true,
  source          text NOT NULL DEFAULT 'web' CHECK (source IN ('web','mobile','sync','system')),
  client_comment_id text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  edited_at       timestamptz,
  deleted_at      timestamptz
);
CREATE INDEX idx_comments_object ON comments (object_type, object_id, created_at);

CREATE TABLE object_links (
  id         uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  from_type  text NOT NULL,
  from_id    uuid NOT NULL,
  to_type    text NOT NULL,
  to_id      uuid NOT NULL,
  link_type  text NOT NULL DEFAULT 'related_to' CHECK (link_type IN ('generated_from','related_to','rework_of','escalated_to')),
  created_by uuid,
  created_at timestamptz NOT NULL DEFAULT now()
);
-- Simpan sekali, baca dua arah; cegah duplikat pasangan tanpa peduli arah
CREATE UNIQUE INDEX uq_object_links ON object_links (
  LEAST(from_type || ':' || from_id::text, to_type || ':' || to_id::text),
  GREATEST(from_type || ':' || from_id::text, to_type || ':' || to_id::text),
  link_type
);
CREATE INDEX idx_object_links_from ON object_links (from_type, from_id);
CREATE INDEX idx_object_links_to ON object_links (to_type, to_id);

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['sla_policies','sla_tracking','checklist_templates','checklist_template_items','tasks','work_orders','incidents','findings','assignments','checklist_runs','checklist_run_items','comments','object_links'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS object_links, comments, checklist_run_items, checklist_runs, assignments, findings, incidents, work_orders, tasks, checklist_template_items, checklist_templates, sla_tracking, sla_policies CASCADE;
