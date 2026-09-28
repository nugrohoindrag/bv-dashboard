// Billing (PRD P4 v2.1 Financial Operations) — tipe JSON (cermin struct Go api/internal/billing/*.go), label, helper hitung
// uang (cermin RoundMul/TaxOf server, P4-NFR-01), tanggal zona waktu property (B-13), dan hook data bersama halaman billing.
// Modul ini mandiri (tidak mengimpor halaman lain) — halaman billing lain boleh memakainya tanpa ketergantungan siklik.
import { useQuery } from "@tanstack/react-query";
import { format } from "date-fns";
import { id as localeID } from "date-fns/locale";
import { fromZonedTime, toZonedTime } from "date-fns-tz";
import { useCallback, useState } from "react";
import { useAll } from "@/api/hooks";
import type { Location, Tenant } from "@/api/types";
import { ApiError, api, type Problem } from "@/lib/api";
import { useAuth, type Principal } from "@/lib/auth";
import { fmtMoney, getTimezone } from "@/lib/format";

// ---------- Tipe API ----------

export interface InvoiceItem {
  id?: string;
  description: string;
  quantity: number;
  unit: string | null;
  unit_price: number;
  amount: number;
  charge_type: string | null;
  /** % — null = default invoice/pengaturan billing. */
  tax_rate: number | null;
  tax_amount: number;
  tax_exempt?: boolean;
  billing_rule_id?: string | null;
  meter_reading_id?: string | null;
  parking_permit_id?: string | null;
  source_type?: string | null;
  source_id?: string | null;
  meta?: Record<string, unknown> | null;
}

export interface Invoice {
  id: string;
  /** null = draft (nomor diberikan saat terbit, D-P4-01). */
  invoice_number: string | null;
  /** Nomor atau "Draft". */
  display_number: string;
  property_id: string;
  tenant_id: string | null;
  tenant_name: string | null;
  unit_location_id: string | null;
  unit_label: string | null;
  invoice_type: string;
  period_start: string | null;
  period_end: string | null;
  description: string | null;
  currency_code: string;
  subtotal_amount: number;
  tax_amount: number;
  tax_mode: "manual" | "computed" | string;
  total_amount: number;
  paid_amount: number;
  credited_amount: number;
  outstanding_amount: number;
  issued_at: string | null;
  due_at: string;
  /** YYYY-MM-DD di zona waktu property (B-13). */
  due_date: string | null;
  days_overdue: number;
  paid_at: string | null;
  status: string;
  source: string;
  source_id: string | null;
  billing_run_id: string | null;
  external_ref: string | null;
  notes: string | null;
  cancel_reason: string | null;
  items: InvoiceItem[];
  payment_count: number;
  penalty_accrued: number;
  created_at: string;
  created_by_name: string | null;
  allowed_actions: string[];
  version: number;
}

export interface Payment {
  id: string;
  payment_number: string;
  property_id: string;
  invoice_id: string;
  invoice_number: string;
  tenant_name: string | null;
  amount: number;
  currency_code: string;
  provider_code: string;
  method: string;
  status: string;
  provider_ref: string | null;
  checkout_url: string | null;
  va_number: string | null;
  qr_string: string | null;
  instructions: string | null;
  expires_at: string | null;
  paid_at: string | null;
  verified_at: string | null;
  verification: string | null;
  verified_by_name: string | null;
  receipt_number: string | null;
  failure_reason: string | null;
  notes: string | null;
  reference: string | null;
  external_ref: string | null;
  receipt_group: string | null;
  refunded_at: string | null;
  refund_reason: string | null;
  proof_count: number;
  tenant_user_name: string | null;
  created_at: string;
  allowed_actions: string[];
  version: number;
}

/** Bukti transfer (P4-VRF-02). */
export interface Proof {
  id: string;
  content_type: string;
  file_name: string | null;
  url: string;
  thumb_url?: string;
  uploaded_at: string;
}

export interface ReceiveResult {
  receipt_group: string;
  payments: Payment[];
  allocated_amount: number;
  credit_amount: number;
}

export interface DocumentLink {
  url: string;
  expires_at: string;
  file_name?: string;
}

export interface RunLine {
  id: string;
  billing_rule_id: string;
  rule_name: string;
  unit_location_id: string;
  unit_label: string;
  tenant_id: string | null;
  tenant_name: string | null;
  charge_type: string;
  description: string;
  quantity: number;
  unit_measure: string | null;
  unit_price: number;
  amount: number;
  tax_rate: number;
  tax_amount: number;
  meter_reading_id: string | null;
  parking_permit_id: string | null;
  meta: Record<string, unknown>;
  exception: string | null;
  exception_label: string | null;
  included: boolean;
  invoice_id: string | null;
  invoice_number: string | null;
}

export interface BillingRun {
  id: string;
  property_id: string;
  property_name: string;
  run_number: string;
  period_start: string;
  period_end: string;
  period_label: string;
  status: string;
  source: string;
  rule_ids: string[];
  combine: boolean;
  invoice_type: string;
  issue_date: string;
  due_date: string;
  line_count: number;
  exception_count: number;
  subtotal_amount: number;
  tax_amount: number;
  total_amount: number;
  invoice_count: number;
  notes: string | null;
  created_by_name: string | null;
  created_at: string;
  generated_at: string | null;
  issued_at: string | null;
  exceptions: Record<string, number>;
  lines?: RunLine[];
  allowed_actions: string[];
  version: number;
}

export interface BillingRule {
  id: string;
  property_id: string;
  code: string;
  name: string;
  charge_type: string;
  basis: string;
  rate: number;
  rates: Record<string, number>;
  base_rule_id: string | null;
  base_rule_name: string | null;
  tariff_id: string | null;
  tariff_name: string | null;
  meter_type: string | null;
  frequency: string;
  issue_day: number;
  due_days: number;
  tax_rate: number | null;
  prorate: boolean;
  unit_label: string | null;
  scope_location_ids: string[];
  unit_types: string[];
  occupancy_statuses: string[];
  bill_to: string;
  auto_generate: boolean;
  is_active: boolean;
  effective_from: string | null;
  effective_until: string | null;
  description: string | null;
  version: number;
}

/** Tarif utilitas (api/internal/metering) untuk rule meter_usage. */
export interface UtilityTariff {
  id: string;
  property_id: string;
  code: string;
  name: string;
  meter_type: string;
  rate: number;
  fixed_charge: number;
  minimum_charge: number;
  unit_label: string;
  is_active: boolean;
  effective_from: string | null;
  notes: string | null;
  version: number;
}

export interface CreditNote {
  id: string;
  credit_note_number: string | null;
  property_id: string;
  invoice_id: string;
  invoice_number: string | null;
  tenant_name: string | null;
  unit_label: string | null;
  amount: number;
  reason: string;
  status: string;
  requested_by_name: string | null;
  requested_at: string;
  decided_by_name: string | null;
  decided_at: string | null;
  decision_note: string | null;
  allowed_actions: string[];
  version: number;
}

export interface PenaltyRule {
  id: string;
  property_id: string;
  property_name: string;
  name: string;
  method: "percent" | "fixed" | string;
  rate: number;
  period: "per_day" | "per_month" | "once" | string;
  grace_days: number;
  max_amount: number | null;
  max_pct: number | null;
  invoice_types: string[];
  is_active: boolean;
  /** Kalimat aturan dari server, mis. "2% per bulan dari sisa tagihan setelah 7 hari, maks. 10%". */
  summary: string;
  version: number;
}

export interface Penalty {
  id: string;
  property_id: string;
  invoice_id: string;
  invoice_number: string | null;
  invoice_status: string;
  tenant_id: string | null;
  tenant_name: string | null;
  unit_location_id: string | null;
  unit_label: string | null;
  penalty_rule_id: string;
  rule_name: string;
  accrued_amount: number;
  billed_amount: number;
  unbilled_amount: number;
  days_late: number;
  status: string;
  billed_invoice_number: string | null;
  waive_reason: string | null;
  waived_by_name: string | null;
  waived_at: string | null;
  updated_at: string;
  allowed_actions: string[];
}

export interface BillPenaltiesResult {
  invoice_ids: string[];
  penalties: number;
  total_amount: number;
}

export interface BillingSettings {
  property_id: string | null;
  /** true = belum ada override property; nilai dari default organization. */
  inherited: boolean;
  tax_enabled: boolean;
  tax_name: string;
  tax_rate: number;
  seller_name: string | null;
  seller_tax_id: string | null;
  seller_address: string | null;
  invoice_footer: string | null;
  payment_instructions: string | null;
  default_due_days: number;
  reminder_offsets: number[];
  updated_at: string | null;
  version: number;
}

export interface BankAccount {
  id: string;
  property_id: string | null;
  bank_name: string;
  account_number: string;
  account_name: string;
  branch: string | null;
  is_default: boolean;
  is_active: boolean;
  notes: string | null;
  version: number;
}

export interface PartyBalance {
  tenant_id: string | null;
  tenant_name: string | null;
  unit_location_id: string | null;
  unit_label: string | null;
  balance: number;
  last_entry_at: string | null;
}

export interface ChargeDraft {
  source_type: string;
  source_id: string;
  source_number: string;
  source_title: string;
  property_id: string;
  tenant_id: string | null;
  tenant_name: string | null;
  unit_location_id: string | null;
  unit_label: string | null;
  items: InvoiceItem[];
  actual_cost_amount: number | null;
  existing_invoice_numbers: string[];
}

export interface ImportRowError {
  row: number;
  field: string;
  message: string;
}

export interface InvoiceImportResult {
  rows: number;
  invoices: number;
  total_amount: number;
  errors: ImportRowError[];
  invoice_ids: string[];
  dry_run: boolean;
}

export interface ProviderInfo {
  code: string;
  name: string;
  is_active: boolean;
  methods: string[];
  config: Record<string, unknown>;
  has_secret: boolean;
}

// ---------- Label & opsi ----------

export type Option = { value: string; label: string };
export const labelOf = (opts: Option[], v?: string | null) => (v ? opts.find((o) => o.value === v)?.label ?? v.replace(/_/g, " ") : "—");

/** Tipe invoice lengkap (P4-INV-02); IPL = label profile Apartment untuk service charge. */
export const INVOICE_TYPES: Option[] = [
  { value: "service_charge", label: "Service Charge" },
  { value: "ipl", label: "IPL" },
  { value: "utility", label: "Utilitas" },
  { value: "electricity", label: "Listrik" },
  { value: "water", label: "Air" },
  { value: "parking", label: "Parkir" },
  { value: "sinking_fund", label: "Sinking Fund" },
  { value: "penalty", label: "Denda" },
  { value: "deposit", label: "Deposit" },
  { value: "rental", label: "Sewa" },
  { value: "facility", label: "Fasilitas" },
  { value: "additional_charge", label: "Biaya Tambahan" },
  { value: "other", label: "Lainnya" },
];
export const invoiceTypeLabel = (t?: string | null) => labelOf(INVOICE_TYPES, t);

export const INVOICE_SOURCES: Option[] = [
  { value: "manual", label: "Manual" },
  { value: "import", label: "Impor CSV" },
  { value: "billing_run", label: "Billing Run" },
  { value: "penalty", label: "Denda" },
  { value: "work_order", label: "Work Order" },
  { value: "service_request", label: "Service Request" },
  { value: "rental", label: "Sewa unit" },
  { value: "hotel", label: "Hotel" },
  { value: "unit_sale", label: "Penjualan unit" },
  { value: "facility", label: "Booking fasilitas" },
];

/** Bucket umur piutang (P4-AGE-01) — nilai = parameter `aging` GET /invoices. */
export const AGING_BUCKETS: Option[] = [
  { value: "current", label: "Belum jatuh tempo" },
  { value: "1_30", label: "1–30 hari" },
  { value: "31_60", label: "31–60 hari" },
  { value: "61_90", label: "61–90 hari" },
  { value: "90_plus", label: "> 90 hari" },
];

export const PAYMENT_METHODS: Option[] = [
  { value: "transfer", label: "Transfer bank" },
  { value: "cash", label: "Tunai" },
  { value: "card", label: "Kartu (EDC)" },
  { value: "va", label: "Virtual Account" },
  { value: "qris", label: "QRIS" },
  { value: "ewallet", label: "E-wallet" },
  { value: "check", label: "Cek / Giro" },
  { value: "other", label: "Lainnya" },
  { value: "credit", label: "Saldo kredit" },
  { value: "deposit", label: "Potong deposit" },
];
/** Metode yang boleh dicatat staf (server manualMethods). */
export const MANUAL_METHODS = PAYMENT_METHODS.filter((m) => m.value !== "credit" && m.value !== "deposit");
export const methodLabel = (m?: string | null) => labelOf(PAYMENT_METHODS, m);

export const PROVIDERS: Option[] = [
  { value: "manual", label: "Manual (verifikasi staf)" },
  { value: "mock_gateway", label: "Mock gateway" },
  { value: "midtrans", label: "Midtrans" },
  { value: "xendit", label: "Xendit" },
];
/** Provider tanpa adapter — pembayaran online ditunda (P4-ONL-02/03, keputusan 16 Sep 2026). */
export const PROVIDERS_ON_HOLD = ["midtrans", "xendit"];

export const VERIFICATION_LABEL: Record<string, string> = { staff_manual: "Diverifikasi staf", gateway_callback: "Callback gateway" };

/** Dasar hitung billing rule (D-P4-05: semua tersedia, dipilih per rule). */
export const RULE_BASES: (Option & { hint: string; icon: string })[] = [
  { value: "fixed_per_unit", label: "Tetap per unit", hint: "Tarif sama untuk setiap unit dalam cakupan.", icon: "apartment" },
  { value: "per_area_m2", label: "Per m² luas unit", hint: "Tarif × luas unit (units.area_m2) — service charge / IPL per m².", icon: "square_foot" },
  { value: "per_unit_type", label: "Per tipe unit", hint: "Tarif berbeda per tipe unit (komersial / residensial).", icon: "category" },
  { value: "meter_usage", label: "Pemakaian meter", hint: "Pemakaian meter listrik / air × tarif utilitas (blok, abonemen).", icon: "electric_meter" },
  { value: "percentage", label: "Persentase rule lain", hint: "Persen dari rule dasar, mis. sinking fund = 10% IPL.", icon: "percent" },
  { value: "per_vehicle", label: "Per kendaraan", hint: "Per izin parkir aktif; tarif per jenis kendaraan, tarif khusus izin diutamakan.", icon: "local_parking" },
];
export const basisLabel = (b?: string | null) => labelOf(RULE_BASES, b);

export const FREQUENCIES: Option[] = [
  { value: "monthly", label: "Bulanan" },
  { value: "quarterly", label: "Triwulan" },
  { value: "yearly", label: "Tahunan" },
];
export const UNIT_TYPES: Option[] = [
  { value: "commercial", label: "Komersial" },
  { value: "residential", label: "Residensial" },
];
export const OCCUPANCY_STATUSES: Option[] = [
  { value: "occupied", label: "Terisi" },
  { value: "vacant", label: "Kosong" },
  { value: "reserved", label: "Dipesan" },
  { value: "inactive", label: "Nonaktif" },
];
export const METER_TYPES: Option[] = [
  { value: "electricity", label: "Listrik (kWh)" },
  { value: "water", label: "Air (m³)" },
];
export const VEHICLE_TYPES: Option[] = [
  { value: "car", label: "Mobil" },
  { value: "motorcycle", label: "Motor" },
  { value: "truck", label: "Truk" },
  { value: "bicycle", label: "Sepeda" },
  { value: "other", label: "Lainnya" },
];
export const BILL_TO: Option[] = [
  { value: "tenant", label: "Tenant penghuni unit" },
  { value: "unit", label: "Unit (pemilik / tanpa tenant)" },
];

/** Label pengecualian baris billing run (cermin exceptionLabels server; server juga mengirim exception_label). */
export const RUN_EXCEPTIONS: Record<string, string> = {
  no_area: "Luas unit belum diisi", no_rate: "Tarif tipe unit belum diatur", no_tenant: "Unit belum memiliki tenant", no_meter: "Unit belum memiliki meter",
  no_reading: "Belum ada pembacaan meter periode ini", reading_flagged: "Pembacaan meter perlu direview", no_tariff: "Tarif utilitas belum diatur",
  already_billed: "Sudah ditagihkan pada periode ini", zero_amount: "Jumlah nol", no_base: "Komponen dasar tidak ada",
};

export const PENALTY_METHODS: Option[] = [
  { value: "percent", label: "Persentase dari sisa tagihan" },
  { value: "fixed", label: "Nominal tetap" },
];
export const PENALTY_PERIODS: Option[] = [
  { value: "per_day", label: "Per hari" },
  { value: "per_month", label: "Per bulan (30 hari)" },
  { value: "once", label: "Sekali" },
];

// ---------- Uang (P4-NFR-01: integer rupiah, pembulatan half-up sama dengan server) ----------

/** qty × harga dibulatkan half-up ke rupiah (qty 2 desimal) — cermin billing.RoundMul. */
export function roundMul(qty: number, price: number): number {
  const qc = Math.round((Number.isFinite(qty) ? qty : 0) * 100);
  const v = qc * Math.round(price || 0);
  return v >= 0 ? Math.floor((v + 50) / 100) : -Math.floor((-v + 50) / 100);
}

/** Pajak = jumlah × tarif% (tarif 2 desimal), half-up — cermin billing.TaxOf. */
export function taxOf(amount: number, rate: number): number {
  if (!(rate > 0) || amount === 0) return 0;
  const bp = Math.round(rate * 100);
  const v = amount * bp;
  return v >= 0 ? Math.floor((v + 5000) / 10000) : -Math.floor((-v + 5000) / 10000);
}

/** Mode pajak per item di form: ikut default invoice · bebas pajak · tarif khusus. */
export type ItemTaxMode = "default" | "exempt" | "custom";
export interface ItemDraft {
  key: string;
  description: string;
  quantity: string;
  unit: string;
  unit_price: string;
  /** "" = mengikuti tipe invoice. */
  charge_type: string;
  tax_mode: ItemTaxMode;
  tax_rate: string;
  source_type?: string | null;
  source_id?: string | null;
  meta?: Record<string, unknown> | null;
  /** Tautan item hasil billing run (rule, pembacaan meter, izin parkir) — dipertahankan saat draft diedit. */
  billing_rule_id?: string | null;
  meter_reading_id?: string | null;
  parking_permit_id?: string | null;
  /**
   * Jumlah asli item dari sumber tepercaya (billing run: prorata, tarif blok meter; denda; sewa/hotel) — server memakai `amount`
   * yang dikirim untuk sumber tersebut. Dipakai selama qty & harga tidak diubah; bila diubah, jumlah = qty × harga.
   */
  orig?: { quantity: string; unit_price: string; amount: number } | null;
}

let itemSeq = 0;
export function newItemDraft(patch: Partial<ItemDraft> = {}): ItemDraft {
  itemSeq += 1;
  return { key: `it-${itemSeq}`, description: "", quantity: "1", unit: "", unit_price: "", charge_type: "", tax_mode: "default", tax_rate: "", ...patch };
}

/** Item API → baris form (edit draft / saran tagihan WO). */
export function itemToDraft(it: InvoiceItem, invoiceType?: string): ItemDraft {
  const exempt = !!it.tax_exempt;
  return newItemDraft({
    description: it.description,
    quantity: String(it.quantity ?? 1),
    unit: it.unit ?? "",
    unit_price: String(it.unit_price ?? 0),
    charge_type: it.charge_type && it.charge_type !== invoiceType ? it.charge_type : "",
    tax_mode: exempt ? "exempt" : "default",
    tax_rate: "",
    source_type: it.source_type ?? null,
    source_id: it.source_id ?? null,
    meta: it.meta ?? null,
    billing_rule_id: it.billing_rule_id ?? null,
    meter_reading_id: it.meter_reading_id ?? null,
    parking_permit_id: it.parking_permit_id ?? null,
    orig: { quantity: String(it.quantity ?? 1), unit_price: String(it.unit_price ?? 0), amount: it.amount },
  });
}

/** Jumlah item: jumlah asli bila qty & harga tidak diubah, selain itu qty × harga (half-up). */
export function itemAmount(it: ItemDraft): number {
  if (it.orig && it.orig.quantity === it.quantity && it.orig.unit_price === it.unit_price && it.orig.amount > 0) return it.orig.amount;
  const qty = parseNum(it.quantity) > 0 ? parseNum(it.quantity) : 1;
  return roundMul(qty, Math.round(parseNum(it.unit_price)));
}

export const parseNum = (v: string | number | null | undefined): number => {
  if (typeof v === "number") return Number.isFinite(v) ? v : 0;
  const n = Number(String(v ?? "").trim().replace(",", "."));
  return Number.isFinite(n) ? n : 0;
};

export interface ItemTotals { amount: number; rate: number; tax: number }
export interface InvoiceTotals { lines: ItemTotals[]; subtotal: number; tax: number; total: number }

/**
 * Pratinjau subtotal/pajak/total (cermin computeItems server): jumlah = qty × harga (half-up); pajak per item = jumlah × tarif —
 * tarif khusus item diutamakan, bebas pajak = 0, selain itu tarif default bila pajak diterapkan. Nilai final tetap dari server
 * (mis. tenant bebas pajak).
 */
export function computeTotals(items: ItemDraft[], applyTax: boolean, defaultRate: number): InvoiceTotals {
  const lines = items.map((it) => {
    const amount = itemAmount(it);
    const rate = it.tax_mode === "exempt" ? 0 : it.tax_mode === "custom" ? parseNum(it.tax_rate) : applyTax ? defaultRate : 0;
    return { amount, rate, tax: taxOf(amount, rate) };
  });
  const subtotal = lines.reduce((a, l) => a + l.amount, 0);
  const tax = lines.reduce((a, l) => a + l.tax, 0);
  return { lines, subtotal, tax, total: subtotal + tax };
}

/** Baris form → body item API (`amount` hanya untuk item asli yang tidak diubah — B-07: server menghitung qty × harga). */
export function draftToItem(it: ItemDraft): Record<string, unknown> {
  const body: Record<string, unknown> = {
    description: it.description.trim(),
    quantity: parseNum(it.quantity) > 0 ? parseNum(it.quantity) : 1,
    unit: it.unit.trim() || null,
    unit_price: Math.round(parseNum(it.unit_price)),
    charge_type: it.charge_type || null,
  };
  if (it.tax_mode === "exempt") body.tax_exempt = true;
  if (it.tax_mode === "custom") body.tax_rate = parseNum(it.tax_rate);
  // jumlah asli sumber tepercaya dipertahankan (server menghitung ulang qty × harga untuk invoice manual/impor)
  const amount = itemAmount(it);
  if (it.orig && amount === it.orig.amount) body.amount = amount;
  if (it.source_type) body.source_type = it.source_type;
  if (it.source_id) body.source_id = it.source_id;
  if (it.billing_rule_id) body.billing_rule_id = it.billing_rule_id;
  if (it.meter_reading_id) body.meter_reading_id = it.meter_reading_id;
  if (it.parking_permit_id) body.parking_permit_id = it.parking_permit_id;
  if (it.meta && Object.keys(it.meta).length) body.meta = it.meta;
  return body;
}

export const money = (n?: number | null, currency?: string) => fmtMoney(n ?? 0, currency || "IDR");
/** @deprecated Helper lama halaman Inventory (`rp` dari InvoicesPage) — sama dengan fmtMoney (P4-FIN-04: satu helper format uang). */
export const rp = (n: number) => fmtMoney(n);
export const pct = (n?: number | null) => (n === null || n === undefined ? "—" : `${new Intl.NumberFormat("id-ID", { maximumFractionDigits: 2 }).format(n)}%`);
export const qtyText = (n?: number | null) => (n === null || n === undefined ? "—" : new Intl.NumberFormat("id-ID", { maximumFractionDigits: 2 }).format(n));

// ---------- Tanggal (zona waktu property, B-13 / P4-OUT-03) ----------

/** Hari ini (YYYY-MM-DD) di zona waktu property aktif. */
export function todayISO(): string {
  return format(toZonedTime(new Date(), getTimezone()), "yyyy-MM-dd");
}
export function addDaysISO(d: string, n: number): string {
  const [y, m, day] = d.split("-").map(Number);
  if (!y || !m || !day) return d;
  return new Date(Date.UTC(y, m - 1, day + n)).toISOString().slice(0, 10);
}
/** YYYY-MM-DD dari tanggal ISO/timestamp (period_start dikirim server sebagai timestamp tengah malam UTC). */
export const dateOnly = (v?: string | null) => (v ? v.slice(0, 10) : "");
/** Tanggal kalender (tanpa jam) → "14 Sep 2026" — tidak digeser zona waktu. */
export function fmtDay(v?: string | null): string {
  const d = dateOnly(v);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(d)) return "—";
  const [y, m, day] = d.split("-").map(Number);
  return format(new Date(y, m - 1, day), "d MMM yyyy", { locale: localeID });
}
export function fmtPeriod(start?: string | null, end?: string | null): string {
  if (!start && !end) return "—";
  return `${fmtDay(start)} – ${fmtDay(end)}`;
}
/** Jatuh tempo invoice (due_date diutamakan; fallback due_at di zona waktu property). */
export function dueDay(inv: { due_date: string | null; due_at: string }): string {
  if (inv.due_date) return inv.due_date;
  return format(toZonedTime(new Date(inv.due_at), getTimezone()), "yyyy-MM-dd");
}

/**
 * Parameter tanggal filter → ISO: tanggal saja (dari date picker / deep link) = awal (from) atau akhir (to) hari di zona waktu
 * property agar rentang inklusif; nilai RFC3339 diteruskan apa adanya.
 */
export function rangeParam(v: string | null | undefined, edge: "from" | "to"): string | undefined {
  if (!v) return undefined;
  if (!/^\d{4}-\d{2}-\d{2}$/.test(v)) return v;
  return fromZonedTime(`${v}T${edge === "from" ? "00:00:00" : "23:59:59"}`, getTimezone()).toISOString();
}

/** Tanggal bayar (YYYY-MM-DD) → timestamp: hari ini = sekarang; tanggal lain = 12:00 waktu property. */
export function paidAtISO(d: string): string | undefined {
  if (!d) return undefined;
  if (d === todayISO()) return new Date().toISOString();
  return fromZonedTime(`${d}T12:00:00`, getTimezone()).toISOString();
}

// ---------- Error API ----------

export const errCode = (e: unknown) => (e instanceof ApiError ? e.code : undefined);
export function errMeta(e: unknown): Record<string, unknown> {
  if (!(e instanceof ApiError)) return {};
  return ((e.problem as Problem & { meta?: Record<string, unknown> }).meta ?? {}) as Record<string, unknown>;
}
export function errFields(e: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (e instanceof ApiError) for (const f of e.problem.errors ?? []) if (f.field && !out[f.field]) out[f.field] = f.message;
  return out;
}

// ---------- Izin ----------

function matchPerm(set: string[], perm: string): boolean {
  if (set.includes(perm) || set.includes("*")) return true;
  const [m, o, a] = perm.split(".");
  return set.includes(`${m}.*`) || set.includes(`${m}.${o}.*`) || set.includes(`${m}.*.${a}`);
}
/** Grant tingkat organization (property_id null, tanpa scope building) — mis. default settings & payment provider (B-15). */
export function hasOrgGrant(principal: Principal | null, perm: string): boolean {
  return !!principal?.properties.some((s) => s.property_id === null && !s.scope_location_id && matchPerm(s.permissions, perm));
}

// ---------- File ----------

/** Isi file → base64 (tanpa prefix data URL) untuk impor CSV/XLSX. */
export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => {
      const s = String(r.result ?? "");
      resolve(s.slice(s.indexOf(",") + 1));
    };
    r.onerror = () => reject(r.error ?? new Error("File tidak dapat dibaca"));
    r.readAsDataURL(file);
  });
}

// ---------- Hook data ----------

/** Pengaturan billing efektif (override property → default organization → default sistem). */
export function useBillingSettings(propertyId?: string | null, enabled = true) {
  const { can } = useAuth();
  return useQuery({
    queryKey: ["billing-settings", propertyId ?? "org"],
    enabled: enabled && can("billing.settings.view", propertyId ?? null),
    queryFn: ({ signal }) => api<BillingSettings>("billing/settings", { query: { property_id: propertyId ?? undefined }, signal }),
    // ganti property di form: nilai lama dipertahankan sampai pengaturan property baru termuat (form tidak di-reset)
    placeholderData: (prev) => prev,
    staleTime: 60_000,
  });
}

/** Tenant & unit property (untuk pemilih pihak tagih). */
export function useParties(propertyId?: string | null) {
  const tenants = useAll<Tenant>("tenants", { property_id: propertyId ?? undefined }, { enabled: !!propertyId });
  const units = useAll<Location>("units", { property_id: propertyId ?? undefined }, { enabled: !!propertyId });
  return { tenants, units };
}

/** Property untuk form create: property aktif di header → property pertama yang dapat diakses. */
export function usePropertyChoice(initial?: string | null) {
  const { propertyId, properties } = useAuth();
  return useState<string>(initial ?? propertyId ?? properties[0]?.id ?? "");
}

/** Nama property dari id (daftar property user). */
export function usePropertyName() {
  const { properties } = useAuth();
  return useCallback((id?: string | null) => (id ? properties.find((p) => p.id === id)?.name ?? "—" : "Default organization"), [properties]);
}

/** Ringkasan tarif billing rule per dasar hitung (daftar rule & wizard billing run). */
export function ruleRateSummary(r: BillingRule): string {
  switch (r.basis) {
    case "fixed_per_unit":
      return `${money(r.rate)} / unit${r.unit_label ? ` (${r.unit_label})` : ""}`;
    case "per_area_m2":
      return `${money(r.rate)} / m²`;
    case "per_unit_type":
      return Object.entries(r.rates).map(([k, v]) => `${labelOf(UNIT_TYPES, k)} ${money(v)}`).join(" · ") || "—";
    case "meter_usage":
      return `${r.meter_type === "water" ? "Air" : "Listrik"} · ${r.tariff_name ?? "tarif meter"}`;
    case "percentage":
      return `${pct(r.rate)} dari ${r.base_rule_name ?? "rule dasar"}`;
    case "per_vehicle": {
      const extra = Object.entries(r.rates).map(([k, v]) => `${labelOf(VEHICLE_TYPES, k)} ${money(v)}`);
      return [r.rate > 0 ? `${money(r.rate)} / kendaraan` : null, ...extra].filter(Boolean).join(" · ") || "—";
    }
  }
  return "—";
}

/** Label unit dari detail location (units API). */
export function unitLabelOf(u: Location): string {
  const d = (u.details ?? {}) as Record<string, unknown>;
  return d.unit_number ? `Unit ${String(d.unit_number)}` : u.name;
}
