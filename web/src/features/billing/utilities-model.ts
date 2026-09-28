// Model & helper murni Utilities (PRD P4 v2.1 §5.3 P4-UTL-01..05; NC "Utilities"): meter listrik & air, pembacaan (angka
// mundur / lonjakan → flagged untuk direview), tarif flat atau blok progresif + biaya beban + minimum. Bentuk JSON = struct Go
// api/internal/metering/service.go. `usageCharge` meniru Tariff.UsageCharge server (pratinjau tagihan di form tarif).

export interface TariffBlock { up_to: number | null; rate: number }
export interface Tariff {
  id: string; property_id: string; code: string; name: string; meter_type: string; rate: number; blocks: TariffBlock[]; fixed_charge: number; minimum_charge: number;
  unit_label: string; is_active: boolean; effective_from: string | null; notes: string | null; version: number;
}
export interface LastReading { id: string; value: number; usage: number | null; period: string; read_at: string; status: string }
export interface Meter {
  id: string; property_id: string; location_id: string; location_name: string; location_path: string; unit_number: string | null; tenant_name: string | null;
  meter_type: string; meter_number: string; multiplier: number; initial_reading: number; tariff_id: string | null; tariff_name: string | null; unit_label: string;
  status: string; installed_on: string | null; notes: string | null; last_reading: LastReading | null; read_this_period: boolean; version: number;
}
export interface Reading {
  id: string; meter_id: string; meter_number: string; meter_type: string; property_id: string; location_name: string; unit_number: string | null;
  reading_value: number; previous_value: number | null; usage: number | null; unit_label: string; read_at: string; period: string; source: string; status: string;
  anomaly: string | null; notes: string | null; recorded_by_name: string | null; reviewed_by_name: string | null; reviewed_at: string | null; review_note: string | null;
  billed: boolean; photo_url?: string; photo_count: number; allowed_actions: string[]; version: number;
}

export const METER_TYPES: Record<string, { label: string; unit: string; icon: string }> = {
  electricity: { label: "Listrik", unit: "kWh", icon: "bolt" },
  water: { label: "Air", unit: "m³", icon: "water_drop" },
};
export const meterTypeLabel = (t: string) => METER_TYPES[t]?.label ?? t;
export const METER_STATUS: Record<string, { label: string; tone: "success" | "neutral" | "warning" }> = {
  active: { label: "Aktif", tone: "success" },
  inactive: { label: "Nonaktif", tone: "neutral" },
  replaced: { label: "Diganti", tone: "warning" },
};
export const ANOMALY: Record<string, string> = { rollback: "Angka mundur", spike: "Lonjakan pemakaian" };
export const ANOMALY_HELP: Record<string, string> = {
  rollback: "Angka lebih kecil dari pembacaan sebelumnya (meter reset/diganti atau salah ketik). Pemakaian dihitung 0 sampai direview.",
  spike: "Pemakaian lebih dari 3× rata-rata 3 periode sebelumnya. Periksa foto meter sebelum menyetujui.",
};
export const SOURCE: Record<string, string> = { web: "Web", staff_app: "Staff App", mobile: "Staff App", offline: "Staff App (offline)", import: "Impor" };

/** Pratinjau pemakaian: (angka − angka sebelumnya) × pengali; angka mundur → pemakaian 0 & ditandai. */
export function estimateUsage(previous: number | null | undefined, value: number, multiplier: number): { usage: number; rollback: boolean } {
  const prev = previous ?? 0;
  if (value < prev) return { usage: 0, rollback: true };
  return { usage: Math.round((value - prev) * (multiplier || 1) * 1000) / 1000, rollback: false };
}

/** Biaya pemakaian = blok progresif (bila ada) atau pemakaian × tarif, dibulatkan; + biaya beban; minimal biaya minimum. */
export function usageCharge(t: Pick<Tariff, "rate" | "blocks" | "fixed_charge" | "minimum_charge">, usage: number): { energy: number; fixed: number; total: number; parts: { span: number; rate: number; cost: number }[] } {
  const u = Math.max(0, usage || 0);
  const parts: { span: number; rate: number; cost: number }[] = [];
  let cost = 0;
  if (t.blocks.length > 0) {
    let rest = u;
    let prev = 0;
    for (const b of t.blocks) {
      if (rest <= 0) break;
      let span = rest;
      if (b.up_to !== null && b.up_to !== undefined) {
        span = Math.min(rest, b.up_to - prev);
        prev = b.up_to;
      }
      if (span < 0) span = 0;
      parts.push({ span, rate: b.rate, cost: span * b.rate });
      cost += span * b.rate;
      rest -= span;
    }
  } else {
    cost = u * t.rate;
    parts.push({ span: u, rate: t.rate, cost });
  }
  const energy = Math.round(cost);
  const fixed = t.fixed_charge || 0;
  const total = Math.max(energy + fixed, t.minimum_charge || 0);
  return { energy, fixed, total, parts };
}

/** Validasi blok (sama dengan server): tarif ≥ 0, up_to naik, blok tanpa batas hanya di akhir. Null = valid. */
export function validateBlocks(blocks: TariffBlock[]): string | null {
  let prev = 0;
  for (let i = 0; i < blocks.length; i++) {
    const b = blocks[i];
    if (!Number.isFinite(b.rate) || b.rate < 0) return `Blok ${i + 1}: tarif tidak boleh negatif`;
    if (b.up_to === null) {
      if (i !== blocks.length - 1) return `Blok ${i + 1}: blok tanpa batas atas harus blok terakhir`;
      continue;
    }
    if (!(b.up_to > prev)) return `Blok ${i + 1}: batas atas harus lebih besar dari ${prev}`;
    prev = b.up_to;
  }
  return null;
}

/** Ringkasan tarif untuk tabel: "Rp 1.444,7/kWh" atau "Progresif 3 blok (Rp 1.000 – Rp 1.700/kWh)". */
export function tariffSummary(t: Pick<Tariff, "rate" | "blocks" | "unit_label">, fmtRate: (n: number) => string): string {
  if (t.blocks.length > 0) {
    const rates = t.blocks.map((b) => b.rate);
    const lo = Math.min(...rates);
    const hi = Math.max(...rates);
    return `Progresif ${t.blocks.length} blok (${fmtRate(lo)}${hi !== lo ? ` – ${fmtRate(hi)}` : ""}/${t.unit_label})`;
  }
  return `${fmtRate(t.rate)}/${t.unit_label}`;
}

/** Tarif per satuan (boleh desimal): "Rp 1.444,7". */
export function fmtRate(n: number): string {
  return "Rp " + new Intl.NumberFormat("id-ID", { maximumFractionDigits: 2 }).format(n);
}

/** Angka form → number (koma desimal diterima); kosong/tidak valid → null. */
export function parseDecimal(text: string): number | null {
  const t = text.trim().replace(/\s/g, "");
  if (!t) return null;
  // "1.234,5" (id) → 1234.5; "1234.5" → 1234.5
  const norm = t.includes(",") ? t.replace(/\./g, "").replace(",", ".") : t;
  const n = Number(norm);
  return Number.isFinite(n) ? n : null;
}

export const TABS = ["meters", "readings", "tariffs"] as const;
export type UtilTab = (typeof TABS)[number];
