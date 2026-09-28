// Model & helper murni Receivables (PRD P4 v2.1 §7: P4-OUT-01..02 outstanding & statement, P4-AGE-01 aging, P4-COL-03..04
// log penagihan, janji bayar, daftar kerja). Bentuk JSON = struct Go api/internal/billing/receivables.go.
import type { Tone } from "@buildingvision/ui/bv";
import type { Party } from "@/features/finance/fin-ui";

// ---------- Aging ----------
export type AgingBucket = "current" | "1_30" | "31_60" | "61_90" | "90_plus";
export type AgingGroup = "tenant" | "unit" | "property";

export interface AgingRow {
  kind: "property" | "tenant" | "unit" | "total";
  id: string | null;
  label: string;
  property_id: string;
  property_name: string;
  tenant_id: string | null;
  unit_location_id: string | null;
  current: number;
  d1_30: number;
  d31_60: number;
  d61_90: number;
  d90_plus: number;
  total: number;
  invoice_count: number;
  oldest_days_overdue: number;
}
export interface AgingReport { group_by: AgingGroup; as_of: string; buckets: { key: string; label: string }[]; rows: AgingRow[]; totals: AgingRow; currency_code: string }

export const AGING_BUCKETS: { key: AgingBucket; field: "current" | "d1_30" | "d31_60" | "d61_90" | "d90_plus"; label: string }[] = [
  { key: "current", field: "current", label: "Belum jatuh tempo" },
  { key: "1_30", field: "d1_30", label: "1–30 hari" },
  { key: "31_60", field: "d31_60", label: "31–60 hari" },
  { key: "61_90", field: "d61_90", label: "61–90 hari" },
  { key: "90_plus", field: "d90_plus", label: "> 90 hari" },
];
export const AGING_GROUPS: { value: AgingGroup; label: string }[] = [{ value: "tenant", label: "Per tenant" }, { value: "unit", label: "Per unit" }, { value: "property", label: "Per property" }];

/** Rail kartu bucket: > 90 hari kritis, 61–90 perlu perhatian (bila ada nilai). */
export function bucketTone(key: AgingBucket, amount: number): Tone | undefined {
  if (amount <= 0) return undefined;
  if (key === "90_plus") return "error";
  if (key === "61_90") return "warning";
  return undefined;
}

type PartyRef = { kind?: string; tenant_id: string | null; unit_location_id: string | null; property_id?: string | null };

/** Parameter pihak untuk filter daftar: tenant → tenant_id; unit → unit_location_id; baris property → property_id. */
export function partyParams(row: PartyRef): Record<string, string> {
  if (row.tenant_id) return { tenant_id: row.tenant_id };
  if (row.unit_location_id) return { unit_location_id: row.unit_location_id };
  if (row.kind === "property" && row.property_id) return { property_id: row.property_id };
  return {};
}

function qs(params: Record<string, string | null | undefined>): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) p.set(k, v);
  const s = p.toString();
  return s ? "?" + s : "";
}

/**
 * Drill-down aging → daftar invoice (`/billing/invoices?aging=…&tenant_id=…`); `bucket` "open" = semua tagihan terbuka pihak.
 * `propertyId` = filter property halaman (dipakai bila baris bukan baris property).
 */
export function agingInvoicesLink(row: PartyRef | null, bucket: AgingBucket | "open", propertyId?: string | null): string {
  const base: Record<string, string | null | undefined> = bucket === "open" ? { open: "true" } : { aging: bucket };
  const party = row ? partyParams(row) : {};
  return "/billing/invoices" + qs({ ...base, property_id: party.property_id ?? propertyId, ...party });
}

/** Statement of account pihak (tenant/unit) pada property. */
export function statementLink(propertyId: string | null | undefined, party: PartyRef, extra: Record<string, string> = {}): string {
  return "/billing/statement" + qs({ property_id: propertyId, tenant_id: party.tenant_id, unit_location_id: party.tenant_id ? null : party.unit_location_id, ...extra });
}

/** Daftar kerja penagihan difilter ke satu pihak (?party = tenant_id atau unit_location_id — sama dengan drill-down dashboard). */
export function collectionsLink(propertyId: string | null | undefined, party: PartyRef): string {
  return "/billing/collections" + qs({ property_id: propertyId, party: party.tenant_id ?? party.unit_location_id });
}

/** Porsi bucket terhadap total (0–100, satu desimal). */
export function bucketShare(amount: number, total: number): number {
  return total > 0 ? Math.round((amount / total) * 1000) / 10 : 0;
}

// ---------- Statement ----------
export interface StatementEntry { date: string; kind: "invoice" | "void" | "credit_note" | "payment" | "refund" | "credit" | string; reference: string | null; description: string; debit: number; credit: number; balance: number; object_type: string; object_id: string }
export interface Statement {
  property_id: string; property_name: string; tenant_id: string | null; tenant_name: string | null; unit_location_id: string | null; unit_label: string | null;
  from: string; to: string; opening_balance: number; total_debit: number; total_credit: number; closing_balance: number; outstanding_amount: number; credit_balance: number; deposit_balance: number;
  entries: StatementEntry[]; currency_code: string; generated_at: string;
}
export interface DocumentLink { url: string; expires_at: string; file_name?: string }

export const STATEMENT_KIND: Record<string, string> = { invoice: "Invoice", void: "Pembatalan", credit_note: "Credit note", payment: "Pembayaran", refund: "Refund", credit: "Saldo kredit" };

/** Tautan object entry statement (invoice / pembayaran / credit note); entry saldo kredit → halaman saldo kredit. */
export function statementObjectLink(e: Pick<StatementEntry, "object_type" | "object_id">): string | null {
  switch (e.object_type) {
    case "invoice":
      return `/billing/invoices/${e.object_id}`;
    case "payment":
      return `/billing/payments/${e.object_id}`;
    case "credit_note":
      return `/billing/credit-notes/${e.object_id}`;
    case "credit_entry":
      return "/billing/deposits?tab=credit";
    default:
      return null;
  }
}

/** Saldo akhir: positif = terutang, negatif = kelebihan bayar (kredit). */
export function balanceText(n: number): string {
  return n > 0 ? "terutang" : n < 0 ? "kelebihan bayar" : "lunas";
}

// ---------- Log penagihan & janji bayar ----------
export interface CollectionLog {
  id: string; property_id: string; tenant_id: string | null; tenant_name: string | null; unit_location_id: string | null; unit_label: string | null;
  invoice_ids: string[]; invoice_numbers: string[]; channel: string; contact_person: string | null; outcome: string; notes: string | null;
  promise_date: string | null; promise_amount: number | null; promise_status: "open" | "kept" | "broken" | "cancelled" | null; follow_up_on: string | null;
  created_by_name: string | null; created_at: string; allowed_actions: string[]; version: number;
}
export interface CollectionLogInput {
  property_id: string; tenant_id: string | null; unit_location_id: string | null; invoice_ids: string[]; channel: string; contact_person: string | null; outcome: string;
  notes: string | null; promise_date: string | null; promise_amount: number | null; follow_up_on: string | null;
}

export const CHANNELS: { value: string; label: string; icon: string }[] = [
  { value: "phone", label: "Telepon", icon: "call" },
  { value: "whatsapp", label: "WhatsApp", icon: "chat" },
  { value: "visit", label: "Kunjungan", icon: "directions_walk" },
  { value: "letter", label: "Surat", icon: "mail" },
  { value: "other", label: "Lainnya", icon: "more_horiz" },
];
export const OUTCOMES: { value: string; label: string }[] = [
  { value: "contacted", label: "Terhubung" },
  { value: "no_answer", label: "Tidak dijawab" },
  { value: "promise_to_pay", label: "Janji bayar" },
  { value: "dispute", label: "Keberatan / sengketa" },
  { value: "paid", label: "Mengaku sudah bayar" },
  { value: "other", label: "Lainnya" },
];
export const channelLabel = (v: string) => CHANNELS.find((c) => c.value === v)?.label ?? v;
export const outcomeLabel = (v: string | null | undefined) => (v ? OUTCOMES.find((c) => c.value === v)?.label ?? v : "—");

export const PROMISE_FILTERS: { key: string; label: string; params: Record<string, string> }[] = [
  { key: "all", label: "Semua log", params: {} },
  { key: "open", label: "Janji menunggu", params: { promise_status: "open" } },
  { key: "broken", label: "Janji ingkar", params: { promise_status: "broken" } },
  { key: "kept", label: "Janji ditepati", params: { promise_status: "kept" } },
  { key: "follow_up", label: "Tindak lanjut jatuh tempo", params: { follow_up_due: "true" } },
];

export const LOG_ACTIONS: Record<string, { label: string; icon: string; destructive?: boolean; done: string }> = {
  mark_kept: { label: "Tandai janji ditepati", icon: "task_alt", done: "Janji bayar ditandai ditepati" },
  mark_broken: { label: "Tandai janji ingkar", icon: "block", destructive: true, done: "Janji bayar ditandai ingkar" },
  cancel_promise: { label: "Batalkan janji", icon: "cancel", destructive: true, done: "Janji bayar dibatalkan" },
};

/** Label pihak log/worklist: tenant, unit (tanpa tenant), atau "—". */
export function partyLabel(p: { tenant_name?: string | null; unit_label?: string | null; label?: string | null }): string {
  return p.tenant_name || p.label || p.unit_label || "—";
}

// ---------- Daftar kerja penagihan ----------
export interface WorkItem {
  kind: "tenant" | "unit"; tenant_id: string | null; unit_location_id: string | null; label: string; property_id: string; property_name: string;
  outstanding_amount: number; overdue_amount: number; overdue_count: number; oldest_days_overdue: number; last_contact_at: string | null; last_outcome: string | null;
  promise_date: string | null; promise_amount: number | null; promise_status: string | null; follow_up_on: string | null; priority: "high" | "medium" | "low"; reasons: string[]; contact_phone: string | null;
}

export const PRIORITY: Record<WorkItem["priority"], { label: string; tone: Tone }> = { high: { label: "Tinggi", tone: "error" }, medium: { label: "Sedang", tone: "warning" }, low: { label: "Rendah", tone: "neutral" } };

/** Kunci pihak worklist/log (tenant atau unit tanpa tenant) — dipakai filter `?party=`. */
export const partyKey = (p: Party) => p.tenant_id ?? p.unit_location_id ?? "";

/** Filter daftar kerja: prioritas, teks (nama/unit/property), dan pihak dari drill-down. */
export function filterWorklist(items: WorkItem[], f: { priority?: string; q?: string; party?: string }): WorkItem[] {
  const q = (f.q ?? "").trim().toLowerCase();
  return items.filter((w) =>
    (!f.priority || w.priority === f.priority) &&
    (!f.party || w.tenant_id === f.party || w.unit_location_id === f.party) &&
    (!q || w.label.toLowerCase().includes(q) || w.property_name.toLowerCase().includes(q)),
  );
}

/** Ringkasan daftar kerja: jumlah per prioritas + total tunggakan. */
export function worklistSummary(items: WorkItem[]): { high: number; medium: number; low: number; overdue: number; promisesDue: number } {
  const out = { high: 0, medium: 0, low: 0, overdue: 0, promisesDue: 0 };
  for (const w of items) {
    out[w.priority]++;
    out.overdue += w.overdue_amount;
    if (w.promise_date) out.promisesDue++;
  }
  return out;
}

// ---------- invoice terbuka pihak (pilihan invoice pada log) ----------
export interface InvoiceLite { id: string; invoice_number: string | null; invoice_type: string; description: string | null; total_amount: number; outstanding_amount: number; due_at: string; status: string; tenant_name: string | null; unit_label: string | null }
