-- +goose Up
-- +goose StatementBegin
-- PRD P0 v2 (Roadmap v2.0) — Product & Platform Foundation: gap closure di atas fondasi P0 v1.0.
-- Semua perubahan backward-compatible (ADD COLUMN / CREATE TABLE / perluasan CHECK). Lihat ADR-014.

-- ---------- §6 Organization: profil, status, platform admin ----------
ALTER TABLE organizations
  ADD COLUMN status            text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive','suspended')),
  ADD COLUMN status_reason     text,
  ADD COLUMN status_changed_at timestamptz,
  ADD COLUMN legal_name        text,
  ADD COLUMN email             text,
  ADD COLUMN phone             text,
  ADD COLUMN website           text,
  ADD COLUMN address           text,
  ADD COLUMN city              text,
  ADD COLUMN province          text,
  ADD COLUMN postal_code       text,
  ADD COLUMN country           text NOT NULL DEFAULT 'ID',
  ADD COLUMN tax_id            text,
  ADD COLUMN industry          text;
UPDATE organizations SET status = CASE WHEN is_active THEN 'active' ELSE 'inactive' END;

-- ---------- §4.1 Portfolio (di atas Property) ----------
CREATE TABLE portfolios (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  code            text NOT NULL,
  name            text NOT NULL,
  description     text,
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, code)
);
CREATE TRIGGER trg_portfolios_upd BEFORE UPDATE ON portfolios FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE portfolios ENABLE ROW LEVEL SECURITY; ALTER TABLE portfolios FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON portfolios USING (organization_id = app_current_org());

-- ---------- §7 Hierarchy: kontak, metadata, status per level ----------
ALTER TABLE locations ADD COLUMN metadata jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE properties
  ADD COLUMN portfolio_id  uuid REFERENCES portfolios(id),
  ADD COLUMN contact_name  text,
  ADD COLUMN contact_phone text,
  ADD COLUMN contact_email text,
  ADD COLUMN province      text,
  ADD COLUMN postal_code   text,
  ADD COLUMN country       text NOT NULL DEFAULT 'ID',
  ADD COLUMN latitude      double precision,
  ADD COLUMN longitude     double precision;
CREATE INDEX idx_properties_portfolio ON properties (portfolio_id) WHERE portfolio_id IS NOT NULL;
-- status property selaras dengan locations.is_active (draft tetap tersedia untuk onboarding)
UPDATE properties pr SET status = CASE WHEN l.is_active THEN 'active' ELSE 'inactive' END FROM locations l WHERE l.id = pr.location_id;
ALTER TABLE buildings
  ADD COLUMN building_type text,
  ADD COLUMN address       text;

-- ---------- §8.4 Access scope: Building/Tower & Resource (vendor) ----------
-- scope_location_id: role berlaku hanya pada subtree lokasi (building/tower) di property tersebut.
ALTER TABLE user_roles ADD COLUMN scope_location_id uuid REFERENCES locations(id);
DROP INDEX uq_user_roles;
CREATE UNIQUE INDEX uq_user_roles ON user_roles (user_id, role_id,
  COALESCE(property_id, '00000000-0000-0000-0000-000000000000'::uuid),
  COALESCE(scope_location_id, '00000000-0000-0000-0000-000000000000'::uuid));
-- akun Vendor: user yang terikat ke satu vendor hanya melihat work order vendor tersebut
ALTER TABLE users ADD COLUMN vendor_id uuid REFERENCES vendors(id);

-- ---------- §10 Task lifecycle: Reopened ----------
ALTER TABLE tasks
  ADD COLUMN reopen_count     int NOT NULL DEFAULT 0,
  ADD COLUMN last_reopened_at timestamptz;
ALTER TABLE work_orders
  ADD COLUMN last_reopened_at timestamptz,
  ADD COLUMN notes            text;

-- ---------- §12 Checklist: Pass/Fail, Selection, Signature, expected result, category ----------
ALTER TABLE checklist_templates ADD COLUMN category text;
ALTER TABLE checklist_template_items DROP CONSTRAINT checklist_template_items_item_type_check;
ALTER TABLE checklist_template_items ADD CONSTRAINT checklist_template_items_item_type_check
  CHECK (item_type IN ('ok_notok_na','yes_no','pass_fail','numeric','text','photo','selection','signature'));
ALTER TABLE checklist_template_items
  ADD COLUMN options        jsonb,   -- selection: [{"value":"clean","label":"Bersih"}, ...]
  ADD COLUMN expected_value text;    -- yes_no: yes|no · pass_fail: pass|fail · selection: value (CSV = salah satu)
ALTER TABLE checklist_run_items DROP CONSTRAINT checklist_run_items_item_type_check;
ALTER TABLE checklist_run_items ADD CONSTRAINT checklist_run_items_item_type_check
  CHECK (item_type IN ('ok_notok_na','yes_no','pass_fail','numeric','text','photo','selection','signature'));
ALTER TABLE checklist_run_items
  ADD COLUMN options        jsonb,
  ADD COLUMN expected_value text,
  ADD COLUMN is_deviation   boolean;  -- NULL = belum dijawab / tidak dievaluasi
-- backfill jawaban lama: Not OK / No / numeric di luar rentang
UPDATE checklist_run_items SET is_deviation = CASE
    WHEN item_type IN ('ok_notok_na','yes_no') THEN result_value IN ('not_ok','no')
    WHEN item_type = 'numeric' AND result_number IS NOT NULL THEN (numeric_min IS NOT NULL AND result_number < numeric_min) OR (numeric_max IS NOT NULL AND result_number > numeric_max)
    ELSE NULL END
  WHERE answered_at IS NOT NULL;

-- ---------- §14 Notification: channel email ----------
ALTER TABLE notification_preferences ADD COLUMN email boolean; -- NULL = mengikuti channel rule (email aktif untuk rule kritis)

-- ---------- §16 Audit trail immutable ----------
-- Operational user (bv_app/bv_worker) tidak dapat mengubah audit log. DELETE hanya diizinkan pada jalur purge
-- organization (demo reset / retensi) yang menyetel bv.org_purge = organization_id di transaksi yang sama.
REVOKE UPDATE, TRUNCATE ON audit_logs FROM bv_app, bv_worker;
CREATE OR REPLACE FUNCTION audit_logs_immutable() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
  IF TG_OP = 'DELETE' AND current_setting('bv.org_purge', true) = OLD.organization_id::text THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'audit_logs bersifat immutable (%)', TG_OP USING ERRCODE = 'insufficient_privilege';
END
$fn$;
CREATE TRIGGER trg_audit_logs_immutable BEFORE UPDATE OR DELETE ON audit_logs FOR EACH ROW EXECUTE FUNCTION audit_logs_immutable();

-- ---------- §6.2 Tenant isolation: RLS untuk tabel anak tanpa organization_id ----------
-- Isolasi mengikuti parent yang sudah ber-RLS (subquery ke parent tunduk pada policy parent).
ALTER TABLE user_roles ENABLE ROW LEVEL SECURITY; ALTER TABLE user_roles FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON user_roles USING (EXISTS (SELECT 1 FROM users u WHERE u.id = user_roles.user_id));
ALTER TABLE team_members ENABLE ROW LEVEL SECURITY; ALTER TABLE team_members FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON team_members USING (EXISTS (SELECT 1 FROM teams t WHERE t.id = team_members.team_id));
ALTER TABLE unit_occupants ENABLE ROW LEVEL SECURITY; ALTER TABLE unit_occupants FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON unit_occupants USING (EXISTS (SELECT 1 FROM units u WHERE u.location_id = unit_occupants.unit_location_id));
ALTER TABLE patrol_route_checkpoints ENABLE ROW LEVEL SECURITY; ALTER TABLE patrol_route_checkpoints FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON patrol_route_checkpoints USING (EXISTS (SELECT 1 FROM patrol_routes r WHERE r.id = patrol_route_checkpoints.route_id));
ALTER TABLE property_business_hours ENABLE ROW LEVEL SECURITY; ALTER TABLE property_business_hours FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON property_business_hours USING (EXISTS (SELECT 1 FROM properties p WHERE p.location_id = property_business_hours.property_id));
ALTER TABLE notification_preferences ENABLE ROW LEVEL SECURITY; ALTER TABLE notification_preferences FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON notification_preferences USING (EXISTS (SELECT 1 FROM users u WHERE u.id = notification_preferences.user_id));
ALTER TABLE sync_cursors ENABLE ROW LEVEL SECURITY; ALTER TABLE sync_cursors FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON sync_cursors USING (EXISTS (SELECT 1 FROM users u WHERE u.id = sync_cursors.user_id));
ALTER TABLE role_permissions ENABLE ROW LEVEL SECURITY; ALTER TABLE role_permissions FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON role_permissions USING (EXISTS (SELECT 1 FROM roles r WHERE r.id = role_permissions.role_id));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP POLICY IF EXISTS org_isolation ON role_permissions; ALTER TABLE role_permissions DISABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS org_isolation ON sync_cursors; ALTER TABLE sync_cursors DISABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS org_isolation ON notification_preferences; ALTER TABLE notification_preferences DISABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS org_isolation ON property_business_hours; ALTER TABLE property_business_hours DISABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS org_isolation ON patrol_route_checkpoints; ALTER TABLE patrol_route_checkpoints DISABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS org_isolation ON unit_occupants; ALTER TABLE unit_occupants DISABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS org_isolation ON team_members; ALTER TABLE team_members DISABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS org_isolation ON user_roles; ALTER TABLE user_roles DISABLE ROW LEVEL SECURITY;
DROP TRIGGER IF EXISTS trg_audit_logs_immutable ON audit_logs;
DROP FUNCTION IF EXISTS audit_logs_immutable();
GRANT UPDATE ON audit_logs TO bv_app, bv_worker;
ALTER TABLE notification_preferences DROP COLUMN IF EXISTS email;
ALTER TABLE checklist_run_items DROP COLUMN IF EXISTS is_deviation, DROP COLUMN IF EXISTS expected_value, DROP COLUMN IF EXISTS options;
ALTER TABLE checklist_template_items DROP COLUMN IF EXISTS expected_value, DROP COLUMN IF EXISTS options;
ALTER TABLE checklist_templates DROP COLUMN IF EXISTS category;
ALTER TABLE work_orders DROP COLUMN IF EXISTS notes, DROP COLUMN IF EXISTS last_reopened_at;
ALTER TABLE tasks DROP COLUMN IF EXISTS last_reopened_at, DROP COLUMN IF EXISTS reopen_count;
ALTER TABLE users DROP COLUMN IF EXISTS vendor_id;
DROP INDEX IF EXISTS uq_user_roles;
ALTER TABLE user_roles DROP COLUMN IF EXISTS scope_location_id;
CREATE UNIQUE INDEX uq_user_roles ON user_roles (user_id, role_id, COALESCE(property_id, '00000000-0000-0000-0000-000000000000'::uuid));
ALTER TABLE buildings DROP COLUMN IF EXISTS address, DROP COLUMN IF EXISTS building_type;
ALTER TABLE properties DROP COLUMN IF EXISTS longitude, DROP COLUMN IF EXISTS latitude, DROP COLUMN IF EXISTS country, DROP COLUMN IF EXISTS postal_code,
  DROP COLUMN IF EXISTS province, DROP COLUMN IF EXISTS contact_email, DROP COLUMN IF EXISTS contact_phone, DROP COLUMN IF EXISTS contact_name, DROP COLUMN IF EXISTS portfolio_id;
ALTER TABLE locations DROP COLUMN IF EXISTS metadata;
DROP TABLE IF EXISTS portfolios;
ALTER TABLE organizations DROP COLUMN IF EXISTS industry, DROP COLUMN IF EXISTS tax_id, DROP COLUMN IF EXISTS country, DROP COLUMN IF EXISTS postal_code,
  DROP COLUMN IF EXISTS province, DROP COLUMN IF EXISTS city, DROP COLUMN IF EXISTS address, DROP COLUMN IF EXISTS website, DROP COLUMN IF EXISTS phone,
  DROP COLUMN IF EXISTS email, DROP COLUMN IF EXISTS legal_name, DROP COLUMN IF EXISTS status_changed_at, DROP COLUMN IF EXISTS status_reason, DROP COLUMN IF EXISTS status;
-- +goose StatementEnd
