// Helper murni Tenant Relation PRD P3 v2.1: konversi waktu input ↔ RFC3339 di zona waktu property, format KPI, delta periode.
import { formatInTimeZone, fromZonedTime } from "date-fns-tz";
import { getTimezone } from "@/lib/format";

/** `<input type="datetime-local">` (waktu di zona waktu property) → RFC3339 UTC; kosong → "". */
export function localInputToISO(value: string | null | undefined, tz: string = getTimezone()): string {
  if (!value) return "";
  const d = fromZonedTime(value.length === 16 ? `${value}:00` : value, tz);
  return Number.isNaN(d.getTime()) ? "" : d.toISOString();
}

/** RFC3339 → nilai `<input type="datetime-local">` di zona waktu property; kosong → "". */
export function isoToLocalInput(iso: string | null | undefined, tz: string = getTimezone()): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return formatInTimeZone(d, tz, "yyyy-MM-dd'T'HH:mm");
}

/** Waktu sekarang sebagai nilai datetime-local di zona waktu property. */
export function nowLocalInput(tz: string = getTimezone(), now: Date = new Date()): string {
  return formatInTimeZone(now, tz, "yyyy-MM-dd'T'HH:mm");
}

/** Hari ini (YYYY-MM-DD) di zona waktu property. */
export function todayInTz(tz: string = getTimezone(), now: Date = new Date()): string {
  return formatInTimeZone(now, tz, "yyyy-MM-dd");
}

/** Angka desimal format Indonesia ("4,35"); null → "—". */
export function fmtDecimal(n: number | null | undefined, digits = 1): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  return new Intl.NumberFormat("id-ID", { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(n);
}

/** Persentase format Indonesia ("92,5%"); null → "—". */
export function fmtPct(n: number | null | undefined, digits = 1): string {
  const s = fmtDecimal(n, digits);
  return s === "—" ? s : `${s}%`;
}

/** Durasi rata-rata (jam desimal dari server) → "45 m" / "3 j 12 m"; null → "—". */
export function fmtHours(h: number | null | undefined): string {
  if (h === null || h === undefined || !Number.isFinite(h)) return "—";
  const min = Math.max(0, Math.round(h * 60));
  const d = Math.floor(min / (60 * 24));
  if (d >= 2) {
    const rest = Math.round((min - d * 60 * 24) / 60);
    return rest ? `${d} hr ${rest} j` : `${d} hr`;
  }
  const hh = Math.floor(min / 60);
  const mm = min % 60;
  if (hh === 0) return `${mm} m`;
  return mm ? `${hh} j ${mm} m` : `${hh} j`;
}

export type DeltaType = "positive" | "negative" | "neutral";
export interface PeriodDelta {
  /** "+12%" / "−8%" / "baru" / "0%" */
  text: string;
  direction: "up" | "down" | "flat";
  /** Warna delta = arah × apakah naik itu baik (upIsGood null = netral). */
  type: DeltaType;
}

/** Perubahan periode ini vs periode sebelumnya. `upIsGood`: true (naik baik), false (naik buruk), null (netral). */
export function periodDelta(current: number, previous: number, upIsGood: boolean | null): PeriodDelta {
  const direction = current > previous ? "up" : current < previous ? "down" : "flat";
  const type: DeltaType = direction === "flat" || upIsGood === null ? "neutral" : (direction === "up") === upIsGood ? "positive" : "negative";
  if (previous <= 0) return { text: current > 0 ? "baru" : "0%", direction, type };
  const pct = Math.round(((current - previous) / previous) * 100);
  const text = pct === 0 ? "0%" : `${pct > 0 ? "+" : "−"}${Math.abs(pct)}%`;
  return { text, direction, type };
}

/** Tautan drill-down server (`drill_down[key]`) dengan fallback. */
export function drillLink(map: Record<string, string> | undefined, key: string, fallback: string): string {
  const v = map?.[key];
  return v && v.startsWith("/") ? v : fallback;
}

/** Tone kepatuhan SLA: ≥ 90% sukses, ≥ 75% peringatan, selain itu kritis. */
export function complianceTone(pct: number | null | undefined): "success" | "warning" | "error" | "neutral" {
  if (pct === null || pct === undefined) return "neutral";
  return pct >= 90 ? "success" : pct >= 75 ? "warning" : "error";
}
