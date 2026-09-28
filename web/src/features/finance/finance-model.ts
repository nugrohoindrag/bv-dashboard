// Model & helper murni Finance (PRD P4 v2.1 §8 P4-BGT-01..03, P4-CST-02..04; §9 P4-INT-02, P4-INT-04): bentuk JSON = struct Go
// api/internal/finance (service.go, accounting.go, webhooks.go). Bukan general ledger — hanya budget, biaya, ekspor jurnal &
// webhook keluar untuk sistem akuntansi pelanggan.
import type { Tone } from "@buildingvision/ui/bv";

// ---------- kategori ----------
export interface Category { key: string; label: string }
export interface Categories { revenue: Category[]; cost: Category[] }
export type Kind = "revenue" | "cost";

/** Cadangan bila GET /finance/categories gagal (sama dengan finance.RevenueCategories / CostCategories). */
export const FALLBACK_CATEGORIES: Categories = {
  revenue: [
    { key: "service_charge", label: "Service Charge" }, { key: "ipl", label: "IPL" }, { key: "electricity", label: "Listrik" }, { key: "water", label: "Air" }, { key: "utility", label: "Utilitas" },
    { key: "parking", label: "Parkir" }, { key: "sinking_fund", label: "Sinking Fund" }, { key: "penalty", label: "Denda" }, { key: "rental", label: "Sewa" }, { key: "facility", label: "Fasilitas" },
    { key: "additional_charge", label: "Biaya Tambahan" }, { key: "other", label: "Lainnya" },
  ],
  cost: [{ key: "maintenance", label: "Maintenance" }, { key: "utility", label: "Utilitas" }, { key: "security", label: "Keamanan" }, { key: "cleaning", label: "Kebersihan" }, { key: "staff", label: "Staf" }, { key: "admin", label: "Administrasi" }, { key: "other", label: "Lainnya" }],
};

// ---------- budget ----------
export interface BudgetLine { kind: Kind; category: string; label: string; months: number[]; total: number }
export interface Budget {
  id: string; property_id: string; property_name: string; fiscal_year: number; revision: number; name: string | null; status: "draft" | "approved" | "superseded" | string; notes: string | null;
  approved_by_name: string | null; approved_at: string | null; created_by_name: string | null; created_at: string; revenue_total: number; cost_total: number; lines?: BudgetLine[]; allowed_actions: string[]; version: number;
}
/** Matriks editor: kind|category → 12 nilai bulanan. */
export type Matrix = Record<string, number[]>;
export const mkey = (kind: Kind, category: string) => `${kind}|${category}`;
export const zeros = () => Array.from({ length: 12 }, () => 0);

export function matrixFromLines(lines: BudgetLine[] | undefined): Matrix {
  const m: Matrix = {};
  for (const l of lines ?? []) m[mkey(l.kind, l.category)] = [...l.months].slice(0, 12).concat(zeros()).slice(0, 12);
  return m;
}

/** Baris yang dikirim ke server (kategori bernilai nol seluruhnya tidak dikirim). */
export function linesFromMatrix(m: Matrix): { kind: Kind; category: string; months: number[] }[] {
  const out: { kind: Kind; category: string; months: number[] }[] = [];
  for (const [k, months] of Object.entries(m)) {
    if (!months.some((v) => v)) continue;
    const [kind, category] = k.split("|") as [Kind, string];
    out.push({ kind, category, months: months.map((v) => Math.max(0, Math.round(v || 0))) });
  }
  return out;
}

export const sum = (xs: number[]) => xs.reduce((a, b) => a + (b || 0), 0);

/** Total per bulan untuk satu jenis. */
export function monthTotals(m: Matrix, kind: Kind): number[] {
  const t = zeros();
  for (const [k, months] of Object.entries(m)) if (k.startsWith(kind + "|")) months.forEach((v, i) => (t[i] += v || 0));
  return t;
}

/** Bagi nilai tahunan rata ke 12 bulan (dibulatkan ke bawah; sisa ke Desember). */
export function spreadAnnual(total: number): number[] {
  const t = Math.max(0, Math.round(total));
  const base = Math.floor(t / 12);
  const out = Array.from({ length: 12 }, () => base);
  out[11] += t - base * 12;
  return out;
}

export function matrixEqual(a: Matrix, b: Matrix): boolean {
  const la = linesFromMatrix(a);
  const lb = linesFromMatrix(b);
  if (la.length !== lb.length) return false;
  const idx = new Map(lb.map((l) => [mkey(l.kind, l.category), l.months.join(",")]));
  return la.every((l) => idx.get(mkey(l.kind, l.category)) === l.months.join(","));
}

// ---------- budget vs actual ----------
export interface Cell { budget: number; actual: number; variance: number }
export interface DrillSrc { label: string; path: string }
export interface BVARow { kind: string; category: string; label: string; months: Cell[]; total: Cell; ytd: Cell; variance_pct: number | null; status: "on_track" | "over" | "under" | string; drill_down: string; sources: DrillSrc[] | null }
export interface BVA {
  property_id: string | null; fiscal_year: number; up_to_month: number; budget_id: string | null; budget_status: string | null; revision: number | null;
  revenue: BVARow[]; cost: BVARow[]; total_revenue: BVARow; total_cost: BVARow; net: BVARow; currency_code: string;
}
export interface ActualTxn { date: string; source_type: string; source_id: string; number: string | null; description: string; amount: number; link: string }

export const BVA_STATUS: Record<string, { label: string; tone: Tone }> = {
  on_track: { label: "Sesuai budget", tone: "success" },
  over: { label: "Di atas budget", tone: "error" },
  under: { label: "Di bawah budget", tone: "warning" },
};
export const SOURCE_TYPE: Record<string, string> = { invoice_item: "Item invoice", credit_note: "Credit note", work_order: "Work Order", task_consumable: "Consumable cleaning", cost_entry: "Biaya manual" };

/** Persen realisasi (actual ÷ budget) — null bila budget 0. */
export function achievement(c: Cell): number | null {
  return c.budget > 0 ? Math.round((c.actual / c.budget) * 1000) / 10 : null;
}

/** Selisih yang "baik" untuk jenisnya: pendapatan di atas budget / biaya di bawah budget. */
export function favorable(kind: string, variance: number): boolean {
  return kind === "cost" ? variance <= 0 : variance >= 0;
}

/** Query drill-down transaksi dari halaman (kind, category, year, month, property). */
export function txnQuery(p: { kind: string; category: string; year: number; month?: number; propertyId?: string | null }): string {
  const q = new URLSearchParams({ kind: p.kind, category: p.category, year: String(p.year) });
  if (p.month) q.set("month", String(p.month));
  if (p.propertyId) q.set("property_id", p.propertyId);
  return `/finance/budget-actual/transactions?${q.toString()}`;
}

// ---------- biaya operasional ----------
export interface CostEntry {
  id: string; property_id: string; property_name: string; category: string; category_label: string; entry_date: string; amount: number; payee: string | null; description: string;
  reference: string | null; attachment_count: number; created_by_name: string | null; created_at: string; allowed_actions: string[]; version: number;
}
export interface OperatingCostRow { category: string; label: string; months: number[]; total: number; share_pct: number }
export interface OperatingCost { year: number; categories: OperatingCostRow[]; monthly: number[]; total: number; sources: Record<string, string[]> }

// ---------- akuntansi ----------
export interface Mapping { key: string; label: string; property_id: string | null; account_code: string; account_name: string; is_default: boolean; inherited: boolean }
export interface JournalLine { date: string; journal_no: string; account_code: string; account_name: string; debit: number; credit: number; description: string; reference: string; property: string; party: string; source_type: string }
export interface Journal { from: string; to: string; lines: JournalLine[]; total_debit: number; total_credit: number; entries: number; truncated?: boolean }

export const JOURNAL_SOURCE: Record<string, string> = {
  invoice: "Invoice", invoice_void: "Pembatalan invoice", payment: "Pembayaran", refund: "Refund", credit_note: "Credit note", credit_entry: "Saldo kredit", sinking_fund: "Sinking fund", deposit: "Deposit",
};

/** Kelompok kunci pemetaan untuk tampilan. */
export function mappingGroup(key: string): string {
  if (key.startsWith("revenue:")) return "Pendapatan";
  if (key.startsWith("cash:")) return "Kas & bank";
  return "Neraca";
}

/** Sumber nilai pemetaan: bawaan sistem · organization · override property. */
export function mappingSource(m: Mapping, scopedToProperty: boolean): { label: string; tone: Tone } {
  if (m.is_default) return { label: "Bawaan sistem", tone: "neutral" };
  if (m.inherited) return { label: "Dari organization", tone: "info" };
  return scopedToProperty ? { label: "Override property", tone: "primary" } : { label: "Organization", tone: "primary" };
}

// ---------- webhook ----------
export interface WebhookEndpoint {
  id: string; name: string; url: string; event_types: string[]; is_active: boolean; secret_hint: string; secret?: string; last_delivery_at: string | null; last_status: string | null;
  pending_count: number; failed_count: number; created_at: string; version: number;
}
export interface WebhookDelivery {
  id: string; endpoint_id: string; event_type: string; object_type: string | null; object_id: string | null; status: "pending" | "delivered" | "failed" | string; attempts: number;
  next_attempt_at: string; response_code: number | null; error: string | null; created_at: string; delivered_at: string | null; payload?: Record<string, unknown>;
}

export const FINANCE_EVENTS: { value: string; label: string }[] = [
  { value: "invoice.issued", label: "Invoice diterbitkan" },
  { value: "invoice.paid", label: "Invoice lunas" },
  { value: "invoice.overdue", label: "Invoice overdue" },
  { value: "invoice.cancelled", label: "Invoice dibatalkan" },
  { value: "invoice.reminder", label: "Pengingat tagihan" },
  { value: "payment.paid", label: "Pembayaran diterima" },
  { value: "payment.refunded", label: "Pembayaran di-refund" },
  { value: "credit_note.approved", label: "Credit note disetujui" },
  { value: "billing_run.generated", label: "Draft tagihan periode dibuat" },
];
export const eventLabel = (v: string) => (v === "webhook.test" ? "Tes webhook" : FINANCE_EVENTS.find((e) => e.value === v)?.label ?? v);

/** Validasi bentuk URL webhook (http/https). Null = valid; server produksi menolak http & host internal. */
export function webhookUrlError(raw: string): string | null {
  const s = raw.trim();
  if (!s) return "URL wajib";
  let u: URL;
  try {
    u = new URL(s);
  } catch {
    return "URL tidak valid";
  }
  if (u.protocol !== "https:" && u.protocol !== "http:") return "URL harus http(s)";
  return null;
}

/** Peringatan (bukan blokir): produksi hanya menerima https ke host publik. */
export function webhookUrlWarning(raw: string): string | null {
  try {
    const u = new URL(raw.trim());
    const host = u.hostname.toLowerCase();
    if (u.protocol === "http:") return "Produksi hanya menerima https — http ditolak kecuali lingkungan non-produksi.";
    if (host === "localhost" || host.endsWith(".local") || host.endsWith(".internal") || /^(10|127|192\.168|172\.(1[6-9]|2\d|3[01]))\./.test(host)) return "Host internal/privat ditolak server produksi.";
  } catch {
    return null;
  }
  return null;
}
