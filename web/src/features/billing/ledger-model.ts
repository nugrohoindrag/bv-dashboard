// Model & helper murni ledger dana (PRD P4 v2.1 §5.4 Sinking Fund P4-SCF-02..04, §5.5 Deposit P4-PND-03..04, §6.1 saldo
// kredit P4-PAY-04..05): bentuk JSON = struct Go api/internal/billing/ledgers.go. Ledger bertanda: + masuk, − keluar.
import type { Tone } from "@buildingvision/ui/bv";

export interface FundEntry {
  id: string; property_id: string; entry_type: string; amount: number; entry_date: string; description: string | null; invoice_id: string | null; invoice_number: string | null;
  payment_id: string | null; work_order_id: string | null; work_order_number: string | null; reference: string | null; created_by_name: string | null; created_at: string;
}
export interface SinkingFundSummary {
  property_id: string; balance: number; opening: number; receipts: number; usage: number; adjustments: number; receipts_period: number; usage_period: number; billed: number; outstanding: number; entries: FundEntry[];
}
export interface PartyBalance { tenant_id: string | null; tenant_name: string | null; unit_location_id: string | null; unit_label: string | null; balance: number; last_entry_at: string | null }
export interface LedgerEntry {
  id: string; entry_type: string; amount: number; entry_date: string; description: string | null; tenant_id: string | null; unit_location_id: string | null; invoice_id: string | null; invoice_number: string | null;
  payment_id: string | null; work_order_id?: string | null; created_by_name: string | null; created_at: string;
}

export const FUND_TYPES: Record<string, { label: string; tone?: Tone }> = {
  opening: { label: "Saldo awal", tone: "info" },
  receipt: { label: "Penerimaan", tone: "success" },
  reversal: { label: "Koreksi penerimaan", tone: "warning" },
  usage: { label: "Penggunaan", tone: "neutral" },
  adjustment: { label: "Penyesuaian", tone: "neutral" },
};
/** Jenis entri sinking fund manual (penerimaan otomatis dari pembayaran komponen sinking fund). */
export const FUND_INPUT_TYPES: { value: "usage" | "adjustment" | "opening"; label: string; help: string }[] = [
  { value: "usage", label: "Penggunaan", help: "Dana dipakai untuk pekerjaan besar — tautkan Work Order sebagai bukti." },
  { value: "adjustment", label: "Penyesuaian", help: "Koreksi saldo (tambah atau kurang), mis. bunga rekening dana." },
  { value: "opening", label: "Saldo awal", help: "Saldo dana sebelum BuildingVision dipakai (migrasi)." },
];

export const DEPOSIT_TYPES: Record<string, { label: string; tone?: Tone }> = {
  received: { label: "Diterima", tone: "success" },
  deducted: { label: "Dipotong", tone: "warning" },
  applied: { label: "Dipakai untuk invoice", tone: "info" },
  refunded: { label: "Dikembalikan", tone: "neutral" },
  adjustment: { label: "Penyesuaian", tone: "neutral" },
};
export const DEPOSIT_INPUT_TYPES: { value: "received" | "deducted" | "refunded" | "adjustment"; label: string; help: string }[] = [
  { value: "received", label: "Diterima", help: "Deposit diterima di luar invoice (tunai/transfer langsung)." },
  { value: "deducted", label: "Dipotong", help: "Potongan kerusakan / tunggakan saat move-out — tautkan Work Order inspeksi/perbaikan." },
  { value: "refunded", label: "Dikembalikan", help: "Pengembalian deposit ke tenant." },
  { value: "adjustment", label: "Penyesuaian", help: "Koreksi saldo deposit (tambah atau kurang)." },
];

export const CREDIT_TYPES: Record<string, { label: string; tone?: Tone }> = {
  overpayment: { label: "Kelebihan bayar", tone: "success" },
  credit_note: { label: "Credit note", tone: "success" },
  applied: { label: "Dipakai untuk invoice", tone: "info" },
  refunded: { label: "Dikembalikan", tone: "neutral" },
  adjustment: { label: "Penyesuaian", tone: "neutral" },
};

/** Nominal yang disimpan server: usage/deducted/refunded dicatat negatif; adjustment memakai tanda pilihan user. */
export function signedAmount(type: string, amount: number, decrease = false): number {
  if (type === "usage" || type === "deducted" || type === "refunded") return -Math.abs(amount);
  if (type === "adjustment") return decrease ? -Math.abs(amount) : Math.abs(amount);
  return Math.abs(amount);
}

/** Nilai `amount` untuk API (positif; adjustment boleh negatif). */
export function apiAmount(type: string, amount: number, decrease = false): number {
  return type === "adjustment" && decrease ? -Math.abs(amount) : Math.abs(amount);
}

/** Saldo setelah entri; negatif = tidak cukup (server menolak dengan 409). */
export function balanceAfter(balance: number, type: string, amount: number, decrease = false): number {
  return balance + signedAmount(type, amount, decrease);
}

/** Label pihak saldo: tenant, atau unit tanpa tenant. */
export function balanceParty(b: Pick<PartyBalance, "tenant_name" | "unit_label" | "tenant_id">): string {
  return b.tenant_name ?? b.unit_label ?? (b.tenant_id ? "Tenant" : "Unit");
}
export const balanceKey = (b: Pick<PartyBalance, "tenant_id" | "unit_location_id">) => b.tenant_id ?? b.unit_location_id ?? "";

/** Total saldo & jumlah pihak. */
export function balanceTotals(rows: PartyBalance[]): { total: number; parties: number; negative: number } {
  let total = 0;
  let negative = 0;
  for (const r of rows) {
    total += r.balance;
    if (r.balance < 0) negative++;
  }
  return { total, parties: rows.length, negative };
}
