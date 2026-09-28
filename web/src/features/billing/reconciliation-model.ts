// Model & helper murni Rekonsiliasi (PRD P4 v2.1 §6.4 P4-REC-01..03, D-P4-04 impor mutasi): bentuk JSON = struct Go
// api/internal/billing/reconciliation.go (ImportInput, StatementImport, StmtLine, MatchInput).
import type { Tone } from "@buildingvision/ui/bv";

export interface StmtLine {
  id: string; line_no: number; txn_date: string; description: string | null; reference: string | null; amount: number; balance: number | null;
  status: "unmatched" | "suggested" | "matched" | "ignored"; match_type: string | null;
  suggested_payment_id: string | null; suggested_payment_number: string | null; suggested_invoice_id: string | null; suggested_invoice_number: string | null; suggested_party: string | null; match_score: number | null;
  payment_id: string | null; payment_number: string | null; invoice_id: string | null; invoice_number: string | null; difference: number | null;
  matched_by_name: string | null; matched_at: string | null; note: string | null; allowed_actions: string[];
}
export interface StatementImport {
  id: string; property_id: string; property_name: string; bank_account_id: string | null; bank_account: string | null; file_name: string | null; format: string;
  period_from: string | null; period_to: string | null; line_count: number; credit_count: number; matched_count: number; suggested_count: number; unmatched_count: number; ignored_count: number;
  credit_total: number; matched_total: number; duplicates_skipped?: number; status: "open" | "completed" | string; imported_by_name: string | null; imported_at: string;
  detected_columns?: string[]; lines?: StmtLine[];
}
export interface BankAccount { id: string; property_id: string | null; bank_name: string; account_number: string; account_name: string; branch: string | null; is_default: boolean; is_active: boolean }
export interface MatchInput { payment_id?: string; invoice_id?: string; tenant_id?: string; unit_location_id?: string; note?: string }

/** Kolom mutasi yang dapat dipetakan manual (judul kolom atau "#nomor" mulai 1). */
export const COLUMN_KEYS: { key: string; label: string; hint: string }[] = [
  { key: "date", label: "Tanggal", hint: "wajib" },
  { key: "description", label: "Keterangan / berita", hint: "" },
  { key: "reference", label: "Referensi", hint: "" },
  { key: "credit", label: "Kredit (masuk)", hint: "atau kolom jumlah" },
  { key: "debit", label: "Debet (keluar)", hint: "" },
  { key: "amount", label: "Jumlah (satu kolom)", hint: "bila tidak ada kolom kredit/debet" },
  { key: "type", label: "Tipe CR/DB", hint: "untuk kolom jumlah" },
  { key: "balance", label: "Saldo", hint: "" },
];
export const COLUMN_LABEL: Record<string, string> = Object.fromEntries(COLUMN_KEYS.map((c) => [c.key, c.label]));

/** Format tanggal (layout Go) — kosong = deteksi otomatis. */
export const DATE_FORMATS: { value: string; label: string }[] = [
  { value: "", label: "Otomatis" },
  { value: "02/01/2006", label: "DD/MM/YYYY" },
  { value: "02-01-2006", label: "DD-MM-YYYY" },
  { value: "2006-01-02", label: "YYYY-MM-DD" },
  { value: "01/02/2006", label: "MM/DD/YYYY" },
  { value: "02/01/06", label: "DD/MM/YY" },
  { value: "02 Jan 2006", label: "DD Mon YYYY" },
];

export const MAX_FILE_BYTES = 8 * 1024 * 1024;
export const ACCEPT_EXT = [".csv", ".txt", ".xlsx", ".xlsm"];

/** Berkas yang dapat dibaca server (CSV/teks atau Excel xlsx/xlsm). */
export function acceptedFile(name: string): boolean {
  const n = name.toLowerCase();
  return ACCEPT_EXT.some((e) => n.endsWith(e));
}

/** Pemetaan manual → objek yang dikirim (hanya kolom terisi); kosong → undefined (deteksi otomatis penuh). */
export function cleanColumns(map: Record<string, string>): Record<string, string> | undefined {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(map)) if (v.trim()) out[k] = v.trim();
  return Object.keys(out).length ? out : undefined;
}

/** "date: Tanggal Transaksi" → { key: "date", label: "Tanggal", header: "Tanggal Transaksi" }. */
export function parseDetected(list: string[] | undefined): { key: string; label: string; header: string }[] {
  return (list ?? []).map((s) => {
    const i = s.indexOf(":");
    const key = i >= 0 ? s.slice(0, i).trim() : s;
    return { key, label: COLUMN_LABEL[key] ?? key, header: i >= 0 ? s.slice(i + 1).trim() : "" };
  });
}

/** Tone skor saran: ≥ 90 yakin (dapat dikonfirmasi massal), ≥ 70 cukup, sisanya periksa manual. */
export function scoreTone(score: number | null | undefined): Tone {
  if (!score) return "neutral";
  if (score >= 90) return "success";
  if (score >= 70) return "info";
  return "warning";
}

export const MATCH_TYPE: Record<string, string> = { pending_payment: "Pembayaran menunggu verifikasi", invoice: "Invoice terbuka", manual: "Dicocokkan manual" };

/** Selisih mutasi vs tagihan: positif = lebih bayar (→ saldo kredit), negatif = kurang (pembayaran sebagian). */
export function differenceText(diff: number | null | undefined): string | null {
  if (!diff) return null;
  return diff > 0 ? "lebih bayar → saldo kredit" : "kurang bayar (pembayaran sebagian)";
}

export const LINE_FILTERS: { key: string; label: string; test: (l: StmtLine) => boolean }[] = [
  { key: "open", label: "Perlu tindakan", test: (l) => l.status === "suggested" || l.status === "unmatched" },
  { key: "suggested", label: "Ada saran", test: (l) => l.status === "suggested" },
  { key: "unmatched", label: "Belum cocok", test: (l) => l.status === "unmatched" },
  { key: "matched", label: "Cocok", test: (l) => l.status === "matched" },
  { key: "ignored", label: "Diabaikan", test: (l) => l.status === "ignored" && l.amount > 0 },
  { key: "debit", label: "Debit (keluar)", test: (l) => l.amount < 0 },
  { key: "all", label: "Semua", test: () => true },
];

/** Jumlah saran yang akan dikonfirmasi massal (skor ≥ min). */
export function confirmableCount(lines: StmtLine[] | undefined, min = 90): number {
  return (lines ?? []).filter((l) => l.status === "suggested" && (l.match_score ?? 0) >= min && l.allowed_actions.includes("confirm")).length;
}

/** Progres rekonsiliasi mutasi kredit: cocok ÷ (kredit − diabaikan). */
export function matchProgress(v: Pick<StatementImport, "credit_count" | "matched_count" | "ignored_count" | "lines">): number {
  const ignoredCredit = v.lines ? v.lines.filter((l) => l.status === "ignored" && l.amount > 0).length : 0;
  const base = v.credit_count - ignoredCredit;
  return base > 0 ? Math.min(100, Math.round((v.matched_count / base) * 100)) : 100;
}
