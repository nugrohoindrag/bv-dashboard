-- +goose Up
-- +goose StatementBegin
-- Activity timeline per object (PRD §24, TAD §5.10)
CREATE TABLE activities (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  object_type        text NOT NULL,
  object_id          uuid NOT NULL,
  actor_user_id      uuid,                          -- NULL = system
  action             text NOT NULL,                 -- created | status_changed | assigned | commented | attachment_added | checklist_item_answered | finding_created | resolved | closed | reopened | evidence_recorded_offline | late_evidence | ...
  from_value         text,
  to_value           text,
  payload            jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at        timestamptz NOT NULL DEFAULT now(),
  client_recorded_at timestamptz,
  source             text NOT NULL DEFAULT 'web' CHECK (source IN ('web','mobile','system','sync'))
);
CREATE INDEX idx_activities_object ON activities (organization_id, object_type, object_id, occurred_at DESC);
CREATE INDEX idx_activities_actor ON activities (organization_id, actor_user_id, occurred_at DESC);

-- Audit log sistem/keamanan
CREATE TABLE audit_logs (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  actor_user_id   uuid,
  action          text NOT NULL,                    -- create | update | delete | status_change | login | login_failed | role_change | permission_change | export | access_denied
  entity_type     text NOT NULL,
  entity_id       uuid,
  entity_label    text,                             -- business id untuk render {User} {Action} {Object}
  before          jsonb,
  after           jsonb,
  ip              inet,
  user_agent      text,
  request_id      text,
  occurred_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_logs_org_time ON audit_logs (organization_id, occurred_at DESC);
CREATE INDEX idx_audit_logs_entity ON audit_logs (organization_id, entity_type, entity_id);

-- Attachments / Evidence (PRD §11.2, TAD §5.13)
CREATE TABLE attachments (
  id                   uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id      uuid NOT NULL REFERENCES organizations(id),
  object_type          text NOT NULL,
  object_id            uuid NOT NULL,
  attachment_type      text NOT NULL CHECK (attachment_type IN ('photo','photo_before','photo_after','checklist_item_photo','document','signature')),
  storage_key          text NOT NULL,
  original_filename    text,
  content_type         text NOT NULL,
  size_bytes           bigint NOT NULL,
  width                int,
  height               int,
  sha256               text,
  captured_at          timestamptz,
  uploaded_by          uuid NOT NULL,
  uploaded_at          timestamptz NOT NULL DEFAULT now(),
  gps_lat              double precision,
  gps_lng              double precision,
  gps_accuracy_m       double precision,
  gps_status           text NOT NULL DEFAULT 'unavailable' CHECK (gps_status IN ('captured','unavailable','denied')),
  device_id            text,
  client_attachment_id text,
  status               text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','ready','failed')),
  thumb_320_key        text,
  thumb_1280_key       text,
  caption              text,
  deleted_at           timestamptz
);
CREATE INDEX idx_attachments_object ON attachments (organization_id, object_type, object_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_attachments_client ON attachments (organization_id, uploaded_by, client_attachment_id) WHERE client_attachment_id IS NOT NULL;

CREATE TABLE exports (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  requested_by    uuid NOT NULL,
  resource        text NOT NULL,
  filters         jsonb NOT NULL DEFAULT '{}'::jsonb,
  format          text NOT NULL CHECK (format IN ('csv','xlsx')),
  status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','ready','failed')),
  storage_key     text,
  row_count       int,
  error           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  completed_at    timestamptz,
  expires_at      timestamptz
);

ALTER TABLE activities  ENABLE ROW LEVEL SECURITY; ALTER TABLE activities  FORCE ROW LEVEL SECURITY;
ALTER TABLE audit_logs  ENABLE ROW LEVEL SECURITY; ALTER TABLE audit_logs  FORCE ROW LEVEL SECURITY;
ALTER TABLE attachments ENABLE ROW LEVEL SECURITY; ALTER TABLE attachments FORCE ROW LEVEL SECURITY;
ALTER TABLE exports     ENABLE ROW LEVEL SECURITY; ALTER TABLE exports     FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON activities  USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON audit_logs  USING (organization_id = app_current_org() OR current_setting('app.auth_lookup', true) = 'on');
CREATE POLICY org_isolation ON attachments USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON exports     USING (organization_id = app_current_org());
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS exports, attachments, audit_logs, activities CASCADE;
