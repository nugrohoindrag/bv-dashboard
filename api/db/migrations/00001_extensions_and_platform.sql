-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS ltree;
CREATE EXTENSION IF NOT EXISTS btree_gin;

-- UUID v7 (time-ordered) generator; fallback implementasi murni SQL agar tidak bergantung pada PG18.
CREATE OR REPLACE FUNCTION uuid_generate_v7() RETURNS uuid AS $$
DECLARE
  unix_ts_ms bytea;
  uuid_bytes bytea;
BEGIN
  unix_ts_ms = substring(int8send(floor(extract(epoch from clock_timestamp()) * 1000)::bigint) from 3);
  uuid_bytes = unix_ts_ms || gen_random_bytes(10);
  uuid_bytes = set_byte(uuid_bytes, 6, (b'0111' || get_byte(uuid_bytes, 6)::bit(4))::bit(8)::int);
  uuid_bytes = set_byte(uuid_bytes, 8, (b'10' || get_byte(uuid_bytes, 8)::bit(6))::bit(8)::int);
  RETURN encode(uuid_bytes, 'hex')::uuid;
END
$$ LANGUAGE plpgsql VOLATILE;

-- Helper: organization_id dari session (RLS). NULL bila tidak di-set.
CREATE OR REPLACE FUNCTION app_current_org() RETURNS uuid AS $$
  SELECT NULLIF(current_setting('app.organization_id', true), '')::uuid
$$ LANGUAGE sql STABLE;

-- Trigger updated_at + version bump
CREATE OR REPLACE FUNCTION set_updated_at_and_version() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  IF TG_OP = 'UPDATE' THEN
    NEW.version = OLD.version + 1;
  END IF;
  RETURN NEW;
END
$$ LANGUAGE plpgsql;

-- Role aplikasi (bukan owner) agar RLS berlaku. Password diganti oleh deploy script.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'bv_app') THEN
    CREATE ROLE bv_app LOGIN PASSWORD 'bv_app_dev';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'bv_worker') THEN
    CREATE ROLE bv_worker LOGIN PASSWORD 'bv_worker_dev';
  END IF;
END
$$;

CREATE TABLE organizations (
  id            uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  code          text NOT NULL UNIQUE,            -- ORG-xxxx (business id)
  slug          text NOT NULL UNIQUE,            -- untuk public intake
  name          text NOT NULL,
  timezone      text NOT NULL DEFAULT 'Asia/Jakarta',
  is_active     boolean NOT NULL DEFAULT true,
  settings      jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at    timestamptz NOT NULL DEFAULT now(),
  created_by    uuid,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  updated_by    uuid,
  version       int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_organizations_upd BEFORE UPDATE ON organizations FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE business_id_sequences (
  organization_id uuid NOT NULL REFERENCES organizations(id),
  prefix          text NOT NULL,
  year            int  NOT NULL,          -- 0 untuk prefix tanpa tahun (mis. AST-HVAC)
  last_value      int  NOT NULL DEFAULT 0,
  PRIMARY KEY (organization_id, prefix, year)
);

CREATE TABLE feature_flags (
  organization_id uuid NOT NULL REFERENCES organizations(id),
  flag            text NOT NULL,
  enabled         boolean NOT NULL DEFAULT false,
  config          jsonb NOT NULL DEFAULT '{}'::jsonb,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, flag)
);

CREATE TABLE idempotency_keys (
  organization_id uuid NOT NULL,
  user_id         uuid NOT NULL,
  key             text NOT NULL,
  request_hash    text NOT NULL,
  status_code     int  NOT NULL,
  response_body   jsonb,
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL DEFAULT now() + interval '24 hours',
  PRIMARY KEY (organization_id, user_id, key)
);
CREATE INDEX idx_idempotency_expires ON idempotency_keys (expires_at);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS feature_flags;
DROP TABLE IF EXISTS business_id_sequences;
DROP TABLE IF EXISTS organizations;
DROP FUNCTION IF EXISTS set_updated_at_and_version();
DROP FUNCTION IF EXISTS app_current_org();
DROP FUNCTION IF EXISTS uuid_generate_v7();
