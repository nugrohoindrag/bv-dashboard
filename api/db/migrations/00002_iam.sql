-- +goose Up
-- +goose StatementBegin
CREATE TABLE users (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  user_code       text NOT NULL,                       -- USR-000001
  email           text,
  username        text,
  full_name       text NOT NULL,
  phone           text,
  password_hash   text NOT NULL,
  is_active       boolean NOT NULL DEFAULT true,
  permission_version int NOT NULL DEFAULT 1,           -- claim `ver`
  failed_login_count int NOT NULL DEFAULT 0,
  locked_until    timestamptz,
  last_login_at   timestamptz,
  preferred_locale text NOT NULL DEFAULT 'id',
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, user_code),
  CHECK (email IS NOT NULL OR username IS NOT NULL)
);
CREATE UNIQUE INDEX uq_users_email ON users (organization_id, lower(email)) WHERE email IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX uq_users_username ON users (organization_id, lower(username)) WHERE username IS NOT NULL AND deleted_at IS NULL;
-- login global by email (lintas org): email harus unik global agar login tanpa org slug.
CREATE UNIQUE INDEX uq_users_email_global ON users (lower(email)) WHERE email IS NOT NULL AND deleted_at IS NULL;
CREATE TRIGGER trg_users_upd BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE identities (           -- disiapkan untuk SSO/OIDC fase lanjut
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  user_id         uuid NOT NULL REFERENCES users(id),
  provider        text NOT NULL,        -- password | oidc:<name>
  subject         text NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (provider, subject)
);

CREATE TABLE sessions (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  user_id            uuid NOT NULL REFERENCES users(id),
  refresh_token_hash text NOT NULL UNIQUE,
  previous_token_hash text,                        -- reuse detection
  client             text NOT NULL,                -- web | mobile
  device_id          text,
  ip                 inet,
  user_agent         text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  last_used_at       timestamptz NOT NULL DEFAULT now(),
  expires_at         timestamptz NOT NULL,
  revoked_at         timestamptz
);
CREATE INDEX idx_sessions_user ON sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE password_reset_tokens (
  token_hash      text PRIMARY KEY,
  organization_id uuid NOT NULL,
  user_id         uuid NOT NULL REFERENCES users(id),
  expires_at      timestamptz NOT NULL,
  used_at         timestamptz
);

CREATE TABLE permissions (
  code        text PRIMARY KEY,               -- {module}.{object}.{action}
  module      text NOT NULL,
  object      text NOT NULL,
  action      text NOT NULL,
  description text
);

CREATE TABLE roles (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  code            text NOT NULL,              -- technician, engineering_supervisor, ...
  name            text NOT NULL,
  is_system       boolean NOT NULL DEFAULT false,
  domain          text,                       -- engineering | security | housekeeping | management | admin
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, code)
);
CREATE TRIGGER trg_roles_upd BEFORE UPDATE ON roles FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE role_permissions (
  role_id         uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  permission_code text NOT NULL REFERENCES permissions(code),
  PRIMARY KEY (role_id, permission_code)
);

CREATE TABLE user_roles (
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role_id     uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  property_id uuid,                                -- NULL = seluruh property di organization
  granted_by  uuid,
  granted_at  timestamptz NOT NULL DEFAULT now()
);
-- property_id NULL = seluruh org; unique index dengan coalesce
CREATE UNIQUE INDEX uq_user_roles ON user_roles (user_id, role_id, COALESCE(property_id, '00000000-0000-0000-0000-000000000000'::uuid));

CREATE TABLE teams (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid,                             -- FK ditambahkan setelah properties dibuat
  name            text NOT NULL,                    -- Engineering Team
  domain          text NOT NULL,                    -- engineering | security | housekeeping | management
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_teams_upd BEFORE UPDATE ON teams FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE team_members (
  team_id   uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  is_lead   boolean NOT NULL DEFAULT false,         -- supervisor team
  joined_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, user_id)
);

CREATE TABLE device_tokens (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform        text NOT NULL CHECK (platform IN ('android','ios','web')),
  token           text NOT NULL UNIQUE,
  device_id       text,
  app_version     text,
  last_seen_at    timestamptz NOT NULL DEFAULT now(),
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_device_tokens_user ON device_tokens (user_id);

-- RLS
ALTER TABLE users ENABLE ROW LEVEL SECURITY;         ALTER TABLE users FORCE ROW LEVEL SECURITY;
ALTER TABLE roles ENABLE ROW LEVEL SECURITY;         ALTER TABLE roles FORCE ROW LEVEL SECURITY;
ALTER TABLE teams ENABLE ROW LEVEL SECURITY;         ALTER TABLE teams FORCE ROW LEVEL SECURITY;
ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;      ALTER TABLE sessions FORCE ROW LEVEL SECURITY;
ALTER TABLE device_tokens ENABLE ROW LEVEL SECURITY; ALTER TABLE device_tokens FORCE ROW LEVEL SECURITY;
ALTER TABLE identities ENABLE ROW LEVEL SECURITY;    ALTER TABLE identities FORCE ROW LEVEL SECURITY;

-- Login perlu membaca user lintas org sebelum org diketahui: policy mengizinkan bila app.organization_id belum di-set
-- DAN session var app.bypass_rls_for_login = 'on' (di-set hanya oleh service login).
CREATE POLICY org_isolation ON users USING (organization_id = app_current_org() OR current_setting('app.auth_lookup', true) = 'on');
CREATE POLICY org_isolation ON roles USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON teams USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON sessions USING (organization_id = app_current_org() OR current_setting('app.auth_lookup', true) = 'on');
CREATE POLICY org_isolation ON device_tokens USING (organization_id = app_current_org());
CREATE POLICY org_isolation ON identities USING (organization_id = app_current_org() OR current_setting('app.auth_lookup', true) = 'on');
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS device_tokens, team_members, teams, user_roles, role_permissions, roles, permissions, password_reset_tokens, sessions, identities, users CASCADE;
