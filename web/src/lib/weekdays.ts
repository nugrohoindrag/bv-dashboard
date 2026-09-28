// Hari dalam minggu untuk jadwal berulang (patrol, cleaning, Cleaning Route): kode = Go time.Weekday di backend
// (0 = Minggu … 6 = Sabtu; kolom `weekdays int[]` default {0..6}). UI menampilkan Senin lebih dulu.
export const WEEKDAY_SHORT = ["Min", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab"];
/** Urutan tampil Senin-first. */
export const WEEKDAY_ORDER = [1, 2, 3, 4, 5, 6, 0];
export const ALL_WEEKDAYS = [0, 1, 2, 3, 4, 5, 6];
export const WORKDAYS = [1, 2, 3, 4, 5];

/** "Setiap hari" · "Sen Sel Rab" · "—". Nilai di luar 0–6 (data lama 1–7) dinormalisasi 7 → 0. */
export function weekdaysText(days: number[] | null | undefined): string {
  const set = new Set(normalizeWeekdays(days));
  if (set.size === 7) return "Setiap hari";
  return WEEKDAY_ORDER.filter((d) => set.has(d)).map((d) => WEEKDAY_SHORT[d]).join(" ") || "—";
}

/** 7 (Minggu versi 1–7) → 0; buang nilai tidak valid & duplikat; urut 0–6. */
export function normalizeWeekdays(days: number[] | null | undefined): number[] {
  return [...new Set((days ?? []).map((d) => (d === 7 ? 0 : d)).filter((d) => Number.isInteger(d) && d >= 0 && d <= 6))].sort((a, b) => a - b);
}

export function toggleWeekday(days: number[], d: number): number[] {
  const set = new Set(normalizeWeekdays(days));
  if (set.has(d)) set.delete(d);
  else set.add(d);
  return [...set].sort((a, b) => a - b);
}
