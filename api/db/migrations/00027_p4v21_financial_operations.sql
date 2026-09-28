-- +goose Up
-- +goose StatementBegin
-- PRD P4 v2.1 (Roadmap v2.1 §12, §25.2, §25.3, §25.8 Phase 4, §29 + Boundary) — Financial Operations: billing rule & tagihan
-- periodik (semua dasar tarif, D-P4-05), nomor invoice saat issue (D-P4-01), PPN terkonfigurasi (D-P4-02), meter listrik/air,
-- Sinking Fund (D-P4-06), denda & deposit, kredit/credit note/refund/alokasi, aging & penagihan, rekonsiliasi impor mutasi
-- (D-P4-04), budget vs actual + biaya manual (D-P4-07), ekspor siap-jurnal + webhook keluar (D-P4-03). Online payment tetap HOLD.
-- Uang = bigint rupiah (P4-NFR-01). Lihat ADR-019.

-- ---------- Master data: pajak tenant & tanggal hunian unit (prorata P4-BRL-04) ----------
ALTER TABLE tenants
  ADD COLUMN tax_id     text,                                   -- NPWP tenant (D-P4-02)
  ADD COLUMN tax_exempt boolean NOT NULL DEFAULT false;
ALTER TABLE units
  ADD COLUMN occupied_from  date,                               -- awal hunian/sewa tenant saat ini (prorata)
  ADD COLUMN occupied_until date;                               -- akhir hunian (move-out terjadwal)

-- ---------- §5.2 Invoice (D-P4-01 nomor saat issue; tipe lengkap; kredit) ----------
ALTER TABLE invoices ALTER COLUMN invoice_number DROP NOT NULL;
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_invoice_type_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_invoice_type_check CHECK (invoice_type IN
  ('service_charge','ipl','utility','electricity','water','parking','sinking_fund','penalty','deposit','rental','facility','additional_charge','other'));
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_source_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_source_check CHECK (source IN
  ('manual','import','rental','hotel','facility','unit_sale','billing_run','work_order','service_request','penalty'));
ALTER TABLE invoices
  ADD COLUMN credited_amount  bigint NOT NULL DEFAULT 0 CHECK (credited_amount >= 0),
  ADD COLUMN tax_mode         text NOT NULL DEFAULT 'manual' CHECK (tax_mode IN ('manual','computed')),
  ADD COLUMN billing_run_id   uuid,
  ADD COLUMN due_date         date,                             -- tanggal jatuh tempo (zona waktu property, B-13)
  ADD COLUMN reminder_stage   int,                              -- offset hari pengingat terakhir yang terkirim (P4-COL-02)
  ADD COLUMN last_reminded_at timestamptz;
ALTER TABLE invoices ADD CONSTRAINT invoices_settled_check CHECK (paid_amount + credited_amount <= total_amount);
CREATE INDEX idx_invoices_source ON invoices (organization_id, source, source_id) WHERE source_id IS NOT NULL;
CREATE INDEX idx_invoices_unit ON invoices (unit_location_id, status);

ALTER TABLE invoice_items
  ADD COLUMN charge_type       text,                            -- jenis komponen (nilai invoice_type) — revenue per tipe, sinking fund
  ADD COLUMN tax_rate          numeric(5,2) NOT NULL DEFAULT 0,
  ADD COLUMN tax_amount        bigint NOT NULL DEFAULT 0,
  ADD COLUMN billing_rule_id   uuid,
  ADD COLUMN meter_reading_id  uuid,
  ADD COLUMN parking_permit_id uuid REFERENCES parking_permits(id),
  ADD COLUMN source_type       text,                            -- work_order | service_request | penalty | …
  ADD COLUMN source_id         uuid,
  ADD COLUMN meta              jsonb NOT NULL DEFAULT '{}'::jsonb;  -- angka meter awal/akhir, prorata, dasar hitung
UPDATE invoice_items it SET charge_type = i.invoice_type FROM invoices i WHERE i.id = it.invoice_id;

-- ---------- Pengaturan billing per organization (property_id NULL) / override per property (D-P4-02) ----------
CREATE TABLE billing_settings (
  id                   uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id      uuid NOT NULL REFERENCES organizations(id),
  property_id          uuid REFERENCES properties(location_id),
  tax_enabled          boolean NOT NULL DEFAULT false,
  tax_name             text NOT NULL DEFAULT 'PPN',
  tax_rate             numeric(5,2) NOT NULL DEFAULT 11 CHECK (tax_rate >= 0 AND tax_rate <= 100),
  seller_name          text,                                    -- nama badan hukum pengelola pada invoice
  seller_tax_id        text,                                    -- NPWP pengelola
  seller_address       text,
  invoice_footer       text,
  payment_instructions text,
  default_due_days     int NOT NULL DEFAULT 14 CHECK (default_due_days BETWEEN 0 AND 120),
  reminder_offsets     int[] NOT NULL DEFAULT '{-3,1,7,14,30}',  -- H-3, H+1, H+7, H+14, H+30 (P4-COL-02)
  updated_at           timestamptz NOT NULL DEFAULT now(),
  updated_by           uuid,
  version              int NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX uq_billing_settings ON billing_settings (organization_id, COALESCE(property_id, '00000000-0000-0000-0000-000000000000'::uuid));
CREATE TRIGGER trg_billing_settings_upd BEFORE UPDATE ON billing_settings FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE bank_accounts (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid REFERENCES properties(location_id),      -- NULL = rekening organization
  bank_name       text NOT NULL,
  account_number  text NOT NULL,
  account_name    text NOT NULL,
  branch          text,
  is_default      boolean NOT NULL DEFAULT false,
  is_active       boolean NOT NULL DEFAULT true,
  notes           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, account_number)
);
CREATE TRIGGER trg_bank_accounts_upd BEFORE UPDATE ON bank_accounts FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------- §5.3 Utility & metering (P4-UTL-01..05) ----------
CREATE TABLE utility_tariffs (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  code            text NOT NULL,
  name            text NOT NULL,
  meter_type      text NOT NULL CHECK (meter_type IN ('electricity','water')),
  rate            numeric(14,2) NOT NULL DEFAULT 0,             -- Rp per kWh / m³ (tanpa blok)
  blocks          jsonb NOT NULL DEFAULT '[]'::jsonb,           -- [{"up_to": 10, "rate": 5000}, {"up_to": null, "rate": 7500}]
  fixed_charge    bigint NOT NULL DEFAULT 0,                    -- abonemen / biaya beban per periode
  minimum_charge  bigint NOT NULL DEFAULT 0,
  unit_label      text NOT NULL DEFAULT 'kWh',
  is_active       boolean NOT NULL DEFAULT true,
  effective_from  date,
  notes           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, property_id, code)
);
CREATE TRIGGER trg_utility_tariffs_upd BEFORE UPDATE ON utility_tariffs FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE meters (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  location_id     uuid NOT NULL REFERENCES locations(id),       -- unit / area
  meter_type      text NOT NULL CHECK (meter_type IN ('electricity','water')),
  meter_number    text NOT NULL,
  multiplier      numeric(10,3) NOT NULL DEFAULT 1 CHECK (multiplier > 0),
  initial_reading numeric(14,3) NOT NULL DEFAULT 0,
  tariff_id       uuid REFERENCES utility_tariffs(id),          -- NULL = tarif dari billing rule
  status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive','replaced')),
  installed_on    date,
  notes           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, property_id, meter_type, meter_number)
);
CREATE INDEX idx_meters_location ON meters (location_id, meter_type) WHERE status = 'active';
CREATE TRIGGER trg_meters_upd BEFORE UPDATE ON meters FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE meter_readings (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  meter_id        uuid NOT NULL REFERENCES meters(id),
  reading_value   numeric(14,3) NOT NULL CHECK (reading_value >= 0),
  previous_value  numeric(14,3),
  usage           numeric(14,3),                                -- (value − previous) × multiplier
  read_at         timestamptz NOT NULL DEFAULT now(),
  period          text NOT NULL,                                -- YYYY-MM (zona waktu property)
  source          text NOT NULL DEFAULT 'web' CHECK (source IN ('web','staff_app','import')),
  status          text NOT NULL DEFAULT 'recorded' CHECK (status IN ('recorded','flagged','approved','rejected')),
  anomaly         text CHECK (anomaly IN ('rollback','spike')),
  notes           text,
  recorded_by     uuid REFERENCES users(id),
  reviewed_by     uuid REFERENCES users(id),
  reviewed_at     timestamptz,
  review_note     text,
  client_ref      uuid,                                         -- idempotensi mutasi offline Staff App
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         int NOT NULL DEFAULT 1
);
CREATE INDEX idx_meter_readings_meter ON meter_readings (meter_id, read_at DESC);
CREATE INDEX idx_meter_readings_list ON meter_readings (organization_id, property_id, period, status);
CREATE UNIQUE INDEX uq_meter_readings_client ON meter_readings (organization_id, client_ref) WHERE client_ref IS NOT NULL;
CREATE TRIGGER trg_meter_readings_upd BEFORE UPDATE ON meter_readings FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE invoice_items ADD CONSTRAINT fk_invoice_items_meter_reading FOREIGN KEY (meter_reading_id) REFERENCES meter_readings(id);

-- ---------- §5.1 Billing rule & tagihan berulang (P4-BRL-01..05, D-P4-05) ----------
CREATE TABLE billing_rules (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  property_id        uuid NOT NULL REFERENCES properties(location_id),
  code               text NOT NULL,
  name               text NOT NULL,
  charge_type        text NOT NULL CHECK (charge_type IN
    ('service_charge','ipl','utility','electricity','water','parking','sinking_fund','deposit','rental','facility','additional_charge','other')),
  basis              text NOT NULL CHECK (basis IN ('fixed_per_unit','per_area_m2','per_unit_type','meter_usage','percentage','per_vehicle')),
  rate               numeric(14,2) NOT NULL DEFAULT 0,          -- Rp/unit, Rp/m², atau persen (basis percentage)
  rates              jsonb NOT NULL DEFAULT '{}'::jsonb,        -- per_unit_type {"residential":250000}; per_vehicle {"car":350000,"motorcycle":100000}
  base_rule_id       uuid REFERENCES billing_rules(id),         -- percentage: % dari rule lain (mis. sinking fund = 10% IPL)
  tariff_id          uuid REFERENCES utility_tariffs(id),       -- meter_usage
  meter_type         text CHECK (meter_type IN ('electricity','water')),
  frequency          text NOT NULL DEFAULT 'monthly' CHECK (frequency IN ('monthly','quarterly','yearly')),
  issue_day          int NOT NULL DEFAULT 1 CHECK (issue_day BETWEEN 1 AND 28),
  due_days           int NOT NULL DEFAULT 14 CHECK (due_days BETWEEN 0 AND 120),
  tax_rate           numeric(5,2),                              -- NULL = ikut billing_settings; 0 = tanpa pajak
  prorate            boolean NOT NULL DEFAULT false,
  unit_label         text,
  scope_location_ids uuid[] NOT NULL DEFAULT '{}',              -- subtree building/tower/floor; kosong = seluruh property
  unit_types         text[] NOT NULL DEFAULT '{}',              -- kosong = semua tipe unit
  occupancy_statuses text[] NOT NULL DEFAULT '{occupied}',
  bill_to            text NOT NULL DEFAULT 'tenant' CHECK (bill_to IN ('tenant','unit')),
  auto_generate      boolean NOT NULL DEFAULT false,            -- job periode membuat draft otomatis (P4-BRL-03)
  is_active          boolean NOT NULL DEFAULT true,
  effective_from     date,
  effective_until    date,
  description        text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  created_by         uuid,
  updated_at         timestamptz NOT NULL DEFAULT now(),
  updated_by         uuid,
  version            int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, property_id, code)
);
CREATE TRIGGER trg_billing_rules_upd BEFORE UPDATE ON billing_rules FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE invoice_items ADD CONSTRAINT fk_invoice_items_rule FOREIGN KEY (billing_rule_id) REFERENCES billing_rules(id);

CREATE TABLE billing_runs (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  run_number      text NOT NULL,                                -- BRN-2026-000001
  period_start    date NOT NULL,
  period_end      date NOT NULL,
  status          text NOT NULL DEFAULT 'preview' CHECK (status IN ('preview','generated','issued','cancelled')),
  source          text NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','scheduled')),
  rule_ids        uuid[] NOT NULL DEFAULT '{}',
  combine         boolean NOT NULL DEFAULT true,               -- satu invoice per unit (gabungan komponen)
  invoice_type    text NOT NULL DEFAULT 'service_charge',
  issue_date      date NOT NULL,
  due_date        date NOT NULL,
  line_count      int NOT NULL DEFAULT 0,
  exception_count int NOT NULL DEFAULT 0,
  subtotal_amount bigint NOT NULL DEFAULT 0,
  tax_amount      bigint NOT NULL DEFAULT 0,
  total_amount    bigint NOT NULL DEFAULT 0,
  invoice_count   int NOT NULL DEFAULT 0,
  notes           text,
  created_by      uuid,
  created_at      timestamptz NOT NULL DEFAULT now(),
  generated_at    timestamptz,
  issued_at       timestamptz,
  cancelled_at    timestamptz,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, run_number)
);
CREATE INDEX idx_billing_runs_list ON billing_runs (organization_id, property_id, period_start DESC);
CREATE TRIGGER trg_billing_runs_upd BEFORE UPDATE ON billing_runs FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();
ALTER TABLE invoices ADD CONSTRAINT fk_invoices_billing_run FOREIGN KEY (billing_run_id) REFERENCES billing_runs(id);

CREATE TABLE billing_run_lines (
  id                uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id   uuid NOT NULL REFERENCES organizations(id),
  run_id            uuid NOT NULL REFERENCES billing_runs(id) ON DELETE CASCADE,
  billing_rule_id   uuid NOT NULL REFERENCES billing_rules(id),
  unit_location_id  uuid NOT NULL REFERENCES locations(id),
  tenant_id         uuid REFERENCES tenants(id),
  charge_type       text NOT NULL,
  description       text NOT NULL,
  quantity          numeric(14,3) NOT NULL DEFAULT 1,
  unit_label        text,
  unit_price        bigint NOT NULL DEFAULT 0,
  amount            bigint NOT NULL DEFAULT 0,
  tax_rate          numeric(5,2) NOT NULL DEFAULT 0,
  tax_amount        bigint NOT NULL DEFAULT 0,
  meter_reading_id  uuid REFERENCES meter_readings(id),
  parking_permit_id uuid REFERENCES parking_permits(id),
  period_start      date NOT NULL,
  meta              jsonb NOT NULL DEFAULT '{}'::jsonb,
  exception         text,                                       -- NULL = siap ditagihkan; kode: no_area | no_tenant | no_reading | already_billed | zero_amount | …
  included          boolean NOT NULL DEFAULT true,
  invoice_id        uuid REFERENCES invoices(id)
);
CREATE INDEX idx_billing_run_lines_run ON billing_run_lines (run_id);
-- idempoten per unit × periode × rule (× izin parkir): hanya satu baris yang sudah menjadi invoice aktif (P4-BRL-03)
CREATE UNIQUE INDEX uq_billing_run_lines_billed ON billing_run_lines (organization_id, billing_rule_id, unit_location_id,
  COALESCE(parking_permit_id, '00000000-0000-0000-0000-000000000000'::uuid), period_start) WHERE invoice_id IS NOT NULL;

-- ---------- §5.4 Sinking Fund, §5.5 Deposit, §6.1 saldo kredit tenant (ledger bertanda) ----------
CREATE TABLE sinking_fund_entries (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  entry_type      text NOT NULL CHECK (entry_type IN ('opening','receipt','usage','adjustment','reversal')),
  amount          bigint NOT NULL,                              -- + masuk, − keluar
  entry_date      date NOT NULL DEFAULT current_date,
  description     text,
  invoice_id      uuid REFERENCES invoices(id),
  payment_id      uuid REFERENCES payments(id),
  work_order_id   uuid REFERENCES work_orders(id),              -- bukti penggunaan (P4-SCF-04)
  reference       text,
  created_by      uuid,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_sinking_fund_entries ON sinking_fund_entries (organization_id, property_id, entry_date DESC);

CREATE TABLE deposit_entries (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  tenant_id        uuid REFERENCES tenants(id),
  unit_location_id uuid REFERENCES locations(id),
  entry_type       text NOT NULL CHECK (entry_type IN ('received','deducted','applied','refunded','adjustment')),
  amount           bigint NOT NULL,                             -- + diterima/ditahan, − dipotong/dipakai/dikembalikan
  entry_date       date NOT NULL DEFAULT current_date,
  reason           text,
  invoice_id       uuid REFERENCES invoices(id),
  payment_id       uuid REFERENCES payments(id),
  work_order_id    uuid REFERENCES work_orders(id),
  created_by       uuid,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CHECK (tenant_id IS NOT NULL OR unit_location_id IS NOT NULL)
);
CREATE INDEX idx_deposit_entries ON deposit_entries (organization_id, property_id, tenant_id, unit_location_id);

CREATE TABLE credit_notes (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  property_id        uuid NOT NULL REFERENCES properties(location_id),
  credit_note_number text,                                      -- CN-2026-000001 (saat disetujui)
  invoice_id         uuid NOT NULL REFERENCES invoices(id),
  tenant_id          uuid REFERENCES tenants(id),
  unit_location_id   uuid REFERENCES locations(id),
  amount             bigint NOT NULL CHECK (amount > 0),
  reason             text NOT NULL,
  status             text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','cancelled')),
  requested_by       uuid,
  requested_at       timestamptz NOT NULL DEFAULT now(),
  decided_by         uuid,
  decided_at         timestamptz,
  decision_note      text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  updated_by         uuid,
  version            int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, credit_note_number)
);
CREATE INDEX idx_credit_notes_invoice ON credit_notes (invoice_id, status);
CREATE TRIGGER trg_credit_notes_upd BEFORE UPDATE ON credit_notes FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE tenant_credit_entries (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  tenant_id        uuid REFERENCES tenants(id),
  unit_location_id uuid REFERENCES locations(id),
  entry_type       text NOT NULL CHECK (entry_type IN ('overpayment','credit_note','applied','refunded','adjustment')),
  amount           bigint NOT NULL,                             -- + kredit bertambah, − kredit dipakai/dikembalikan
  entry_date       date NOT NULL DEFAULT current_date,
  description      text,
  invoice_id       uuid REFERENCES invoices(id),
  payment_id       uuid REFERENCES payments(id),
  credit_note_id   uuid REFERENCES credit_notes(id),
  created_by       uuid,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CHECK (tenant_id IS NOT NULL OR unit_location_id IS NOT NULL)
);
CREATE INDEX idx_tenant_credit_entries ON tenant_credit_entries (organization_id, property_id, tenant_id, unit_location_id);

-- ---------- §6 Payment lanjutan: bukti transfer (attachments object payment), alokasi, refund, rekonsiliasi ----------
ALTER TABLE payments
  ADD COLUMN external_ref           text,                       -- nomor dokumen di sistem akuntansi (P4-INT-03)
  ADD COLUMN receipt_group          text,                       -- RCV-2026-000001: satu penerimaan dialokasikan ke banyak invoice (P4-PAY-05)
  ADD COLUMN reference              text,                       -- referensi bank / berita transfer
  ADD COLUMN refunded_at            timestamptz,
  ADD COLUMN refunded_by            uuid,
  ADD COLUMN refund_reason          text,
  ADD COLUMN bank_statement_line_id uuid;
CREATE INDEX idx_payments_receipt_group ON payments (organization_id, receipt_group) WHERE receipt_group IS NOT NULL;

-- ---------- §5.5 Denda keterlambatan (P4-PND-01..02) ----------
CREATE TABLE penalty_rules (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  name            text NOT NULL,
  method          text NOT NULL DEFAULT 'percent' CHECK (method IN ('percent','fixed')),
  rate            numeric(12,4) NOT NULL CHECK (rate > 0),     -- % dari sisa tagihan atau nominal Rp
  period          text NOT NULL DEFAULT 'per_month' CHECK (period IN ('per_day','per_month','once')),
  grace_days      int NOT NULL DEFAULT 0 CHECK (grace_days >= 0),
  max_amount      bigint,                                       -- batas nominal per invoice
  max_pct         numeric(5,2),                                 -- batas % dari total invoice
  invoice_types   text[] NOT NULL DEFAULT '{}',                 -- kosong = semua tipe (kecuali penalty)
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_penalty_rules_upd BEFORE UPDATE ON penalty_rules FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE invoice_penalties (
  id                 uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id    uuid NOT NULL REFERENCES organizations(id),
  property_id        uuid NOT NULL REFERENCES properties(location_id),
  invoice_id         uuid NOT NULL REFERENCES invoices(id),
  penalty_rule_id    uuid NOT NULL REFERENCES penalty_rules(id),
  accrued_amount     bigint NOT NULL DEFAULT 0,
  days_late          int NOT NULL DEFAULT 0,
  status             text NOT NULL DEFAULT 'accruing' CHECK (status IN ('accruing','final','billed','waived')),
  last_calculated_at timestamptz,
  billed_invoice_id  uuid REFERENCES invoices(id),
  waived_by          uuid,
  waived_at          timestamptz,
  waive_reason       text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  version            int NOT NULL DEFAULT 1,
  UNIQUE (invoice_id, penalty_rule_id)
);
CREATE INDEX idx_invoice_penalties ON invoice_penalties (organization_id, property_id, status);
CREATE TRIGGER trg_invoice_penalties_upd BEFORE UPDATE ON invoice_penalties FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------- §7.3 Collection tracking (P4-COL-02..04) ----------
CREATE TABLE invoice_reminders (
  invoice_id      uuid NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  organization_id uuid NOT NULL REFERENCES organizations(id),
  offset_days     int NOT NULL,                                 -- −3 = H-3, 7 = H+7
  sent_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (invoice_id, offset_days)
);

CREATE TABLE collection_logs (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  property_id      uuid NOT NULL REFERENCES properties(location_id),
  tenant_id        uuid REFERENCES tenants(id),
  unit_location_id uuid REFERENCES locations(id),
  invoice_ids      uuid[] NOT NULL DEFAULT '{}',
  channel          text NOT NULL CHECK (channel IN ('phone','whatsapp','visit','letter','other')),
  contact_person   text,
  outcome          text NOT NULL DEFAULT 'contacted' CHECK (outcome IN ('contacted','no_answer','promise_to_pay','dispute','paid','other')),
  notes            text,
  promise_date     date,
  promise_amount   bigint,
  promise_status   text CHECK (promise_status IN ('open','kept','broken','cancelled')),
  follow_up_on     date,
  created_by       uuid,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1,
  CHECK (tenant_id IS NOT NULL OR unit_location_id IS NOT NULL)
);
CREATE INDEX idx_collection_logs ON collection_logs (organization_id, property_id, tenant_id, created_at DESC);
CREATE INDEX idx_collection_promises ON collection_logs (organization_id, promise_status, promise_date) WHERE promise_status = 'open';
CREATE TRIGGER trg_collection_logs_upd BEFORE UPDATE ON collection_logs FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------- §6.4 Reconciliation: impor mutasi rekening (P4-REC-01..03, D-P4-04) ----------
CREATE TABLE bank_statement_imports (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  bank_account_id uuid REFERENCES bank_accounts(id),
  file_name       text,
  format          text NOT NULL DEFAULT 'csv',
  period_from     date,
  period_to       date,
  line_count      int NOT NULL DEFAULT 0,
  credit_count    int NOT NULL DEFAULT 0,
  matched_count   int NOT NULL DEFAULT 0,
  status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open','completed')),
  imported_by     uuid,
  imported_at     timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_bank_statement_imports_upd BEFORE UPDATE ON bank_statement_imports FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE bank_statement_lines (
  id                   uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id      uuid NOT NULL REFERENCES organizations(id),
  import_id            uuid NOT NULL REFERENCES bank_statement_imports(id) ON DELETE CASCADE,
  property_id          uuid NOT NULL REFERENCES properties(location_id),
  line_no              int NOT NULL,
  txn_date             date NOT NULL,
  description          text,
  reference            text,
  amount               bigint NOT NULL,                         -- + kredit (uang masuk), − debit
  balance              bigint,
  status               text NOT NULL DEFAULT 'unmatched' CHECK (status IN ('unmatched','suggested','matched','ignored')),
  match_type           text CHECK (match_type IN ('pending_payment','invoice','manual')),
  suggested_payment_id uuid REFERENCES payments(id),
  suggested_invoice_id uuid REFERENCES invoices(id),
  match_score          int,
  payment_id           uuid REFERENCES payments(id),
  invoice_id           uuid REFERENCES invoices(id),
  difference           bigint,
  matched_by           uuid,
  matched_at           timestamptz,
  note                 text,
  UNIQUE (import_id, line_no)
);
CREATE INDEX idx_bank_statement_lines ON bank_statement_lines (organization_id, import_id, status);
ALTER TABLE payments ADD CONSTRAINT fk_payments_statement_line FOREIGN KEY (bank_statement_line_id) REFERENCES bank_statement_lines(id);

-- ---------- §8 Budget / Actual & biaya operasional (P4-BGT-*, P4-CST-02..04, D-P4-07) ----------
CREATE TABLE budgets (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  fiscal_year     int NOT NULL CHECK (fiscal_year BETWEEN 2000 AND 2100),
  revision        int NOT NULL DEFAULT 1,
  name            text,
  status          text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','superseded')),
  notes           text,
  approved_by     uuid,
  approved_at     timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1,
  UNIQUE (organization_id, property_id, fiscal_year, revision)
);
CREATE TRIGGER trg_budgets_upd BEFORE UPDATE ON budgets FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE budget_lines (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  budget_id       uuid NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
  kind            text NOT NULL CHECK (kind IN ('revenue','cost')),
  category        text NOT NULL,
  month           int NOT NULL CHECK (month BETWEEN 1 AND 12),
  amount          bigint NOT NULL DEFAULT 0,
  UNIQUE (budget_id, kind, category, month)
);

CREATE TABLE cost_entries (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid NOT NULL REFERENCES properties(location_id),
  category        text NOT NULL CHECK (category IN ('maintenance','utility','security','cleaning','staff','admin','other')),
  entry_date      date NOT NULL,
  amount          bigint NOT NULL CHECK (amount > 0),
  payee           text,
  description     text NOT NULL,
  reference       text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  created_by      uuid,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  deleted_at      timestamptz,
  version         int NOT NULL DEFAULT 1
);
CREATE INDEX idx_cost_entries ON cost_entries (organization_id, property_id, entry_date) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_cost_entries_upd BEFORE UPDATE ON cost_entries FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

-- ---------- §9 Integrasi akuntansi: pemetaan akun & webhook keluar (P4-INT-02, P4-INT-04, D-P4-03) ----------
CREATE TABLE account_mappings (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  property_id     uuid REFERENCES properties(location_id),
  mapping_key     text NOT NULL,                                -- receivable | revenue:ipl | tax_output | cash:transfer | sinking_fund | deposit | customer_credit
  account_code    text NOT NULL,
  account_name    text,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      uuid,
  version         int NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX uq_account_mappings ON account_mappings (organization_id, COALESCE(property_id, '00000000-0000-0000-0000-000000000000'::uuid), mapping_key);
CREATE TRIGGER trg_account_mappings_upd BEFORE UPDATE ON account_mappings FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE webhook_endpoints (
  id               uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id  uuid NOT NULL REFERENCES organizations(id),
  name             text NOT NULL,
  url              text NOT NULL,
  secret           text NOT NULL,
  event_types      text[] NOT NULL DEFAULT '{}',               -- kosong = semua event keuangan
  is_active        boolean NOT NULL DEFAULT true,
  last_delivery_at timestamptz,
  last_status      text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  created_by       uuid,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       uuid,
  version          int NOT NULL DEFAULT 1
);
CREATE TRIGGER trg_webhook_endpoints_upd BEFORE UPDATE ON webhook_endpoints FOR EACH ROW EXECUTE FUNCTION set_updated_at_and_version();

CREATE TABLE webhook_deliveries (
  id              uuid PRIMARY KEY DEFAULT uuid_generate_v7(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  endpoint_id     uuid NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
  event_type      text NOT NULL,
  object_type     text,
  object_id       uuid,
  payload         jsonb NOT NULL,
  status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','delivered','failed')),
  attempts        int NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),           -- retry backoff eksponensial (maks. 8 percobaan)
  response_code   int,
  error           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  delivered_at    timestamptz
);
CREATE INDEX idx_webhook_deliveries ON webhook_deliveries (organization_id, endpoint_id, created_at DESC);
CREATE INDEX idx_webhook_deliveries_pending ON webhook_deliveries (next_attempt_at) WHERE status = 'pending';

-- RLS
DO $$ DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['billing_settings','bank_accounts','utility_tariffs','meters','meter_readings','billing_rules','billing_runs','billing_run_lines',
    'sinking_fund_entries','deposit_entries','credit_notes','tenant_credit_entries','penalty_rules','invoice_penalties','invoice_reminders','collection_logs',
    'bank_statement_imports','bank_statement_lines','budgets','budget_lines','cost_entries','account_mappings','webhook_endpoints','webhook_deliveries'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY org_isolation ON %I USING (organization_id = app_current_org())', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS webhook_deliveries, webhook_endpoints, account_mappings, cost_entries, budget_lines, budgets CASCADE;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS fk_payments_statement_line;
DROP TABLE IF EXISTS bank_statement_lines, bank_statement_imports, collection_logs, invoice_reminders, invoice_penalties, penalty_rules CASCADE;
ALTER TABLE payments DROP COLUMN IF EXISTS bank_statement_line_id, DROP COLUMN IF EXISTS refund_reason, DROP COLUMN IF EXISTS refunded_by,
  DROP COLUMN IF EXISTS refunded_at, DROP COLUMN IF EXISTS reference, DROP COLUMN IF EXISTS receipt_group, DROP COLUMN IF EXISTS external_ref;
DROP TABLE IF EXISTS tenant_credit_entries, credit_notes, deposit_entries, sinking_fund_entries CASCADE;
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS fk_invoices_billing_run;
ALTER TABLE invoice_items DROP CONSTRAINT IF EXISTS fk_invoice_items_rule, DROP CONSTRAINT IF EXISTS fk_invoice_items_meter_reading;
DROP TABLE IF EXISTS billing_run_lines, billing_runs, billing_rules, meter_readings, meters, utility_tariffs, bank_accounts, billing_settings CASCADE;
ALTER TABLE invoice_items DROP COLUMN IF EXISTS meta, DROP COLUMN IF EXISTS source_id, DROP COLUMN IF EXISTS source_type, DROP COLUMN IF EXISTS parking_permit_id,
  DROP COLUMN IF EXISTS meter_reading_id, DROP COLUMN IF EXISTS billing_rule_id, DROP COLUMN IF EXISTS tax_amount, DROP COLUMN IF EXISTS tax_rate, DROP COLUMN IF EXISTS charge_type;
DROP INDEX IF EXISTS idx_invoices_unit;
DROP INDEX IF EXISTS idx_invoices_source;
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_settled_check;
ALTER TABLE invoices DROP COLUMN IF EXISTS last_reminded_at, DROP COLUMN IF EXISTS reminder_stage, DROP COLUMN IF EXISTS due_date, DROP COLUMN IF EXISTS billing_run_id,
  DROP COLUMN IF EXISTS tax_mode, DROP COLUMN IF EXISTS credited_amount;
UPDATE invoices SET invoice_type = 'other' WHERE invoice_type NOT IN ('service_charge','utility','rental','facility','deposit','other');
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_invoice_type_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_invoice_type_check CHECK (invoice_type IN ('service_charge','utility','rental','facility','deposit','other'));
UPDATE invoices SET source = 'manual' WHERE source NOT IN ('manual','import','rental','hotel','facility','unit_sale');
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_source_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_source_check CHECK (source IN ('manual','import','rental','hotel','facility','unit_sale'));
DELETE FROM invoices WHERE invoice_number IS NULL;
ALTER TABLE invoices ALTER COLUMN invoice_number SET NOT NULL;
ALTER TABLE units DROP COLUMN IF EXISTS occupied_until, DROP COLUMN IF EXISTS occupied_from;
ALTER TABLE tenants DROP COLUMN IF EXISTS tax_exempt, DROP COLUMN IF EXISTS tax_id;
-- +goose StatementEnd
