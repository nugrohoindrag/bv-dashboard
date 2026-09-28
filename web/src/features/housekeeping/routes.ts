// Helper murni Housekeeping PRD P2 v2.1 (§7.3 Cleaning Route, §7.5 consumable): label tipe cleaning (NC §18), jadwal
// berantai stop route (offset = jumlah estimasi stop sebelumnya — cermin getRouteTx backend), urutan stop, pesan error.

/** NC §18 Cleaning Type Naming. */
export const CLEANING_TYPE_LABEL: Record<string, string> = {
  routine: "Routine Cleaning", periodic: "Periodic Cleaning", deep: "Deep Cleaning", spot: "Spot Cleaning", special: "Special Cleaning",
};
export const CLEANING_TYPES = Object.keys(CLEANING_TYPE_LABEL);
export const cleaningTypeLabel = (c: string | null | undefined) => (c ? CLEANING_TYPE_LABEL[c] ?? c : "—");

/** Estimasi default per stop (menit) — sama dengan default server. */
export const DEFAULT_STOP_MINUTES = 15;

function toMinutes(hhmm: string): number | null {
  const m = /^(\d{1,2}):(\d{2})/.exec((hhmm ?? "").trim());
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  return h > 23 || min > 59 ? null : h * 60 + min;
}
function toClock(total: number): string {
  const t = ((total % 1440) + 1440) % 1440;
  return `${String(Math.floor(t / 60)).padStart(2, "0")}:${String(t % 60).padStart(2, "0")}`;
}

export interface StopTiming {
  offset: number;
  minutes: number;
  start: string | null;
  end: string | null;
  /** Melewati tengah malam relatif terhadap mulai route. */
  nextDay: boolean;
}

/**
 * Jadwal berantai stop: stop ke-i mulai pada start_time + Σ estimasi stop sebelumnya (offset_minutes backend).
 * Estimasi kosong/≤0 memakai default 15 menit. start_time kosong/tidak valid → jam null (offset tetap dihitung).
 */
export function stopTimings(startTime: string | null | undefined, estimates: (number | string | null | undefined)[]): { stops: StopTiming[]; totalMinutes: number; end: string | null } {
  const base = startTime ? toMinutes(startTime) : null;
  let offset = 0;
  const stops = estimates.map((e) => {
    const n = Number(e);
    const minutes = Number.isFinite(n) && n > 0 ? Math.round(n) : DEFAULT_STOP_MINUTES;
    const s: StopTiming = { offset, minutes, start: base === null ? null : toClock(base + offset), end: base === null ? null : toClock(base + offset + minutes), nextDay: base !== null && base + offset + minutes > 1440 };
    offset += minutes;
    return s;
  });
  return { stops, totalMinutes: offset, end: base === null ? null : toClock(base + offset) };
}

/** Pindahkan elemen i satu langkah (dir -1 naik, +1 turun); di luar batas → salinan tanpa perubahan. */
export function moveItem<T>(list: T[], i: number, dir: -1 | 1): T[] {
  const j = i + dir;
  if (i < 0 || i >= list.length || j < 0 || j >= list.length) return [...list];
  const next = [...list];
  [next[i], next[j]] = [next[j], next[i]];
  return next;
}

/** Pesan error pemakaian consumable dari kode problem+json; null = pakai detail server. */
export function consumableUsageError(code: string | null | undefined): string | null {
  switch (code) {
    case "INSUFFICIENT_STOCK":
      return "Stok tidak mencukupi di gudang property ini.";
    case "OBJECT_TERMINAL":
      return "Task sudah selesai/ditutup; consumable tidak dapat diubah.";
    default:
      return null;
  }
}
