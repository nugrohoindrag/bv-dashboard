-- +goose Up
-- +goose StatementBegin
-- ============ BILLING & PAYMENT (PRD P1 v1.3 §23, WF-P1-006, AT-P1-012; NC §34–§36; TD-P1-007) ============
CREATE TABLE invoices (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  invoice_number   text NOT NULL,                                  -- INV-2026-000001
  tenant_id        uuid REFERENCES tenants(id),
  unit_location_id uuid REFERENCES locations(id),
  invoice_type     text NOT NULL DEFAULT 'service_charge' CHECK (invoice_type IN ('service_charge','utility','rental','facility','deposit','other')),
  period_start     date,
  period_end       date,
  description      text,
  currency_code    char(3) NOT NULL DEFAULT 'IDR',
  subtotal_amount  bigint NOT NULL DEFAULT 0,
  tax_amount       bigint NOT NULL DEFAULT 0,
  total_amount     bigint NOT NULL DEFAULT 0,
  paid_amount      bigint NOT NULL DEFAULT 0,
  issued_at        timestamptz,
  due_at           timestamptz NOT NULL,
  paid_at          timestamptz,
  status           text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','issued','partially_paid','paid','overdue','cancelled')),
  source           text NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','import','rental','hotel','facility')),
  source_id        uuid,
  external_ref     text,                                           -- referensi sistem akuntansi (OD-P1-010)
  notes            text,
  cancelled_at     timestamptz,
  cancel_reason    text,
  due_soon_notified_at timestamptz,
  overdue_notified_at  timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, invoice_number),
  CHECK (paid_amount >= 0 AND paid_amount <= total_amount)
);
CREATE INDEX idx_invoices_list ON invoices (organization_id, property_id, status, due_at);
CREATE INDEX idx_invoices_tenant ON invoices (tenant_id, status);
CREATE TRIGGER trg_invoices_upd BEFORE UPDATE ON invoices FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE invoice_items (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  invoice_id      uuid NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  sort_order      int NOT NULL DEFAULT 0,
  description     text NOT NULL,
  quantity        numeric(12,2) NOT NULL DEFAULT 1,
  unit            text,                                            -- m², kWh, bulan (NC §42)
  unit_price      bigint NOT NULL DEFAULT 0,
  amount          bigint NOT NULL DEFAULT 0
);
CREATE INDEX idx_invoice_items ON invoice_items (invoice_id, sort_order);

-- Provider pembayaran per organization (TD-P1-007): manual (transfer/cash diverifikasi staf), mock_gateway (webhook HMAC), adapter nyata (OD-P1-009)
CREATE TABLE payment_providers (
  organization_id uuid NOT NULL REFERENCES organizations(id),
  code            text NOT NULL CHECK (code IN ('manual','mock_gateway','midtrans','xendit')),
  name            text NOT NULL,
  is_active       boolean NOT NULL DEFAULT true,
  methods         text[] NOT NULL DEFAULT '{transfer}',            -- transfer | va | qris | ewallet | card | cash
  config          jsonb NOT NULL DEFAULT '{}'::jsonb,              -- publik (instruksi transfer, merchant id)
  webhook_secret  text,                                            -- rahasia; tidak pernah dikembalikan API
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  PRIMARY KEY (organization_id, code)
);

CREATE TABLE payments (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  payment_number   text NOT NULL,                                  -- PAY-2026-000001
  invoice_id       uuid NOT NULL REFERENCES invoices(id),
  tenant_user_id   uuid REFERENCES users(id),
  amount           bigint NOT NULL CHECK (amount > 0),
  currency_code    char(3) NOT NULL DEFAULT 'IDR',
  provider_code    text NOT NULL,
  method           text NOT NULL DEFAULT 'transfer',
  status           text NOT NULL DEFAULT 'initiated' CHECK (status IN ('initiated','pending','paid','failed','expired','cancelled','refunded')),
  provider_ref     text,                                           -- id transaksi di gateway
  checkout_url     text,
  va_number        text,
  qr_string        text,
  instructions     text,
  expires_at       timestamptz,
  paid_at          timestamptz,
  verified_at      timestamptz,
  verified_by      uuid,                                           -- staf (manual) / NULL (callback gateway)
  verification     text CHECK (verification IN ('gateway_callback','staff_manual')),
  receipt_number   text,
  failure_reason   text,
  callback_payload jsonb,
  notes            text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, payment_number)
);
CREATE INDEX idx_payments_invoice ON payments (invoice_id, status);
CREATE INDEX idx_payments_list ON payments (organization_id, property_id, status, created_at DESC);
CREATE UNIQUE INDEX uq_payments_provider_ref ON payments (provider_code, provider_ref) WHERE provider_ref IS NOT NULL;
CREATE TRIGGER trg_payments_upd BEFORE UPDATE ON payments FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- Idempotensi callback gateway: satu event eksternal diproses sekali
CREATE TABLE payment_webhook_events (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  provider_code   text NOT NULL,
  external_id     text NOT NULL,
  signature_valid boolean NOT NULL,
  payload         jsonb NOT NULL,
  received_at     timestamptz NOT NULL DEFAULT now(),
  processed_at    timestamptz,
  result          text,
  UNIQUE (provider_code, external_id)
);

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['invoices','invoice_items','payment_providers','payments','payment_webhook_events'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- webhook gateway tiba tanpa konteks org: lookup provider_ref/secret memakai auth_lookup (seperti login)
DROP POLICY org_isolation ON payments;
CREATE POLICY org_isolation ON payments USING (organization_id = app_current_org() OR current_setting('app.auth_lookup', true) = 'on');
DROP POLICY org_isolation ON payment_providers;
CREATE POLICY org_isolation ON payment_providers USING (organization_id = app_current_org() OR current_setting('app.auth_lookup', true) = 'on');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS payment_webhook_events, payments, payment_providers, invoice_items, invoices CASCADE;
-- +goose StatementEnd
