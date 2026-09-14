-- +goose Up
-- +goose StatementBegin
-- ============ TENANT SERVICE (PRD §16) ============
CREATE TABLE service_request_categories (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  code            text NOT NULL,                    -- complaint | inquiry | maintenance | facility | cleaning | security | other
  name            text NOT NULL,
  default_domain  text,                             -- engineering | security | housekeeping | management
  default_priority text NOT NULL DEFAULT 'medium' CHECK (default_priority IN ('low','medium','high','critical')),
  default_team_id uuid REFERENCES teams(id),
  is_active       boolean NOT NULL DEFAULT true,
  sort_order      int NOT NULL DEFAULT 0,
  UNIQUE (organization_id, code)
);

CREATE TABLE service_requests (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  request_number   text NOT NULL,                   -- SR-2026-000001
  category_id      uuid REFERENCES service_request_categories(id),
  category_code    text NOT NULL,
  title            text NOT NULL,
  description      text,
  tenant_id        uuid REFERENCES tenants(id),
  occupant_id      uuid REFERENCES occupants(id),
  requester_name   text,                            -- untuk public intake / walk-in
  requester_phone  text,
  requester_email  text,
  location_id      uuid REFERENCES locations(id),
  priority         text NOT NULL DEFAULT 'medium' CHECK (priority IN ('low','medium','high','critical')),
  status           text NOT NULL DEFAULT 'new' CHECK (status IN ('new','acknowledged','assigned','in_progress','waiting_for_tenant','resolved','closed','cancelled')),
  channel          text NOT NULL DEFAULT 'staff' CHECK (channel IN ('staff','phone','walk_in','public_intake','email','whatsapp')),
  assignee_user_id uuid REFERENCES users(id),
  assignee_team_id uuid REFERENCES teams(id),
  acknowledged_at  timestamptz,
  resolved_at      timestamptz,
  closed_at        timestamptz,
  cancelled_at     timestamptz,
  resolution       text,
  sla_risk_at      timestamptz,
  sla_breached_at  timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, request_number)
);
CREATE INDEX idx_sr_list ON service_requests (organization_id, property_id, status, created_at DESC);
CREATE INDEX idx_sr_tenant ON service_requests (tenant_id);
CREATE INDEX idx_sr_sla_risk ON service_requests (organization_id, property_id) WHERE sla_risk_at IS NOT NULL AND status NOT IN ('closed','cancelled','resolved');
CREATE TRIGGER trg_service_requests_upd BEFORE UPDATE ON service_requests FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ============ NOTIFICATION (PRD §17, TAD §5.14) ============
CREATE TABLE notification_rules (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid REFERENCES organizations(id),   -- NULL = default global (seed)
  event_type      text NOT NULL,                    -- work_order.assigned, task.overdue, ...
  recipient_resolver text NOT NULL,                 -- assignee | assignee_team_supervisor | property_domain_supervisor | requester | reporter
  notification_type text NOT NULL,                  -- work_order_assigned, ...
  severity        text NOT NULL DEFAULT 'info' CHECK (severity IN ('info','warning','critical','success')),
  channels        text[] NOT NULL DEFAULT '{inapp,push}',
  dedup_minutes   int NOT NULL DEFAULT 0,
  is_active       boolean NOT NULL DEFAULT true
);
CREATE INDEX idx_notification_rules_event ON notification_rules (event_type) WHERE is_active;

CREATE TABLE notifications (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  type              text NOT NULL,                  -- work_order_assigned | task_overdue | ...
  title             text NOT NULL,                  -- "Work Order Overdue"
  body              text NOT NULL,                  -- "WO-2026-001283 — AHU-03\nTower A · Floor 12"
  object_type       text,
  object_id         uuid,
  object_label      text,
  deep_link         text,
  severity          text NOT NULL DEFAULT 'info' CHECK (severity IN ('info','warning','critical','success')),
  created_at        timestamptz NOT NULL DEFAULT now(),
  read_at           timestamptz,
  delivered_push_at timestamptz,
  push_error        text
);
CREATE INDEX idx_notifications_user ON notifications (user_id, created_at DESC);
CREATE INDEX idx_notifications_unread ON notifications (user_id) WHERE read_at IS NULL;
CREATE INDEX idx_notifications_dedup ON notifications (user_id, type, object_id, created_at DESC);

CREATE TABLE notification_preferences (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  type    text NOT NULL,
  inapp   boolean NOT NULL DEFAULT true,
  push    boolean NOT NULL DEFAULT true,
  PRIMARY KEY (user_id, type)
);

-- ============ SEARCH (TAD §5.15) ============
CREATE TABLE search_documents (
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid,
  object_type     text NOT NULL,                    -- property | building | unit | tenant | asset | task | work_order | service_request | incident
  object_id       uuid NOT NULL,
  business_id     text,
  title           text NOT NULL,
  subtitle        text,
  location_path   text,
  status          text,
  tsv             tsvector GENERATED ALWAYS AS (
                    setweight(to_tsvector('simple', coalesce(business_id,'')), 'A') ||
                    setweight(to_tsvector('simple', coalesce(title,'')), 'A') ||
                    setweight(to_tsvector('simple', coalesce(subtitle,'')), 'B') ||
                    setweight(to_tsvector('simple', coalesce(location_path,'')), 'C')
                  ) STORED,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (object_type, object_id)
);
CREATE INDEX idx_search_tsv ON search_documents USING GIN (tsv);
CREATE INDEX idx_search_bid_trgm ON search_documents USING GIN (business_id gin_trgm_ops);
CREATE INDEX idx_search_title_trgm ON search_documents USING GIN (title gin_trgm_ops);
CREATE INDEX idx_search_org ON search_documents (organization_id, property_id);

-- ============ SYNC (TAD §6.5–6.6) ============
CREATE TABLE sync_mutations (
  client_mutation_id uuid PRIMARY KEY,
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  device_id          text NOT NULL,
  user_id            uuid NOT NULL REFERENCES users(id),
  object_type        text NOT NULL,
  object_id          uuid NOT NULL,
  action             text NOT NULL,
  seq                bigint NOT NULL,
  payload            jsonb NOT NULL DEFAULT '{}'::jsonb,
  client_time        timestamptz,
  received_at        timestamptz NOT NULL DEFAULT now(),
  result             text NOT NULL CHECK (result IN ('applied','duplicate','rejected','conflict')),
  reason_code        text,
  reason_detail      text,
  server_version     int,
  response           jsonb,
  acknowledged_by    uuid,
  acknowledged_at    timestamptz
);
CREATE INDEX idx_sync_mutations_conflicts ON sync_mutations (organization_id, received_at DESC) WHERE result = 'conflict' AND acknowledged_at IS NULL;
CREATE INDEX idx_sync_mutations_device_seq ON sync_mutations (device_id, object_id, seq DESC);

CREATE TABLE sync_cursors (                          -- untuk delta work-bundle (removed tracking)
  user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id text NOT NULL,
  cursor_at timestamptz NOT NULL,
  bundle_object_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  PRIMARY KEY (user_id, device_id)
);

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['service_request_categories','service_requests','notifications','search_documents','sync_mutations'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
ALTER TABLE notification_rules ENABLE ROW LEVEL SECURITY; ALTER TABLE notification_rules FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON notification_rules USING (organization_id IS NULL OR organization_id = app_current_org());

-- Grants untuk role aplikasi
GRANT USAGE ON SCHEMA public TO bv_app, bv_worker;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO bv_app, bv_worker;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO bv_app, bv_worker;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO bv_app, bv_worker;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO bv_app, bv_worker;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS sync_cursors, sync_mutations, search_documents, notification_preferences, notifications, notification_rules, service_requests, service_request_categories CASCADE;
