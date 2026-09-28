// Helper murni Workforce PRD P2 v2.1 §8 (shift per domain D-P2-05, roster mingguan, on-duty, serah terima, kompetensi,
// kapasitas). Kode = nilai yang divalidasi backend (api/internal/workforce/*.go). Status object (roster_attendance,
// attendance, shift_handover, validity) memakai status map GENERATED lewat StatusBadge — tidak ada peta status lokal di sini.
import type { Tone } from "@buildingvision/ui/bv";
import type { RosterEntry, ShiftDomain } from "@/api/types";
import { WEEKDAY_SHORT } from "@/lib/weekdays";

export type LabelMap = Record<string, string>;
export const labelOf = (map: LabelMap, code: string | null | undefined): string => (code ? map[code] ?? code.replace(/_/g, " ") : "—");

export const SHIFT_DOMAIN_LABEL: Record<ShiftDomain, string> = { security: "Security", housekeeping: "Housekeeping" };
/** NC §15 "Shift Management" (Security) · NC §17 "Shift" (Housekeeping). */
export const SHIFT_PAGE_TITLE: Record<ShiftDomain, string> = { security: "Security Shift Management", housekeeping: "Housekeeping Shift" };
export const DOMAIN_LABEL: LabelMap = { engineering: "Engineering", security: "Security", housekeeping: "Housekeeping", general: "Umum", total: "Total" };

// ---------- Shift ----------
/** Warna shift disimpan sebagai nama tone token (bukan hex) agar benar di tema light & dark (DS Guideline §2.1). */
export const SHIFT_TONES: { value: Tone; label: string }[] = [
  { value: "primary", label: "Teal (brand)" },
  { value: "info", label: "Biru" },
  { value: "success", label: "Hijau" },
  { value: "warning", label: "Kuning" },
  { value: "error", label: "Merah" },
  { value: "neutral", label: "Abu-abu" },
];
/** Tone token dari kolom `color` shift; nilai lain (mis. hex dari klien lama) → undefined (ditampilkan netral). */
export function shiftTone(color: string | null | undefined): Tone | undefined {
  return SHIFT_TONES.some((t) => t.value === color) ? (color as Tone) : undefined;
}

/** Label shift ringkas: "Pagi · 07:00–15:00". */
export function shiftText(s: { name: string; start_time: string; end_time: string }): string {
  return `${s.name} · ${s.start_time}–${s.end_time}`;
}

/** Menit dari "HH:MM"; null bila format salah. */
export function clockMinutes(v: string | null | undefined): number | null {
  const m = /^(\d{1,2}):(\d{2})$/.exec((v ?? "").trim());
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (h > 23 || min > 59) return null;
  return h * 60 + min;
}
/** Cermin scanShift backend: akhir ≤ mulai = lintas tengah malam (+24 jam). null bila jam tidak valid / sama. */
export function shiftWindow(start: string, end: string): { durationMinutes: number; crossesMidnight: boolean } | null {
  const s = clockMinutes(start);
  const e = clockMinutes(end);
  if (s === null || e === null || s === e) return null;
  const crosses = e <= s;
  return { durationMinutes: (crosses ? e + 1440 : e) - s, crossesMidnight: crosses };
}
/** "8 jam" · "7 jam 30 menit" · "45 menit". */
export function fmtHours(minutes: number | null | undefined): string {
  if (minutes === null || minutes === undefined || !Number.isFinite(minutes)) return "—";
  const m = Math.max(0, Math.round(minutes));
  const h = Math.floor(m / 60);
  const r = m % 60;
  if (!h) return `${r} menit`;
  return r ? `${h} jam ${r} menit` : `${h} jam`;
}

// ---------- Tanggal (lokal browser; roster memakai tanggal kalender property) ----------
export function isoDay(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}
/** "2026-09-27" → Date lokal 00:00. */
export function parseDay(day: string): Date {
  const [y, m, d] = day.split("-").map(Number);
  return new Date(y, (m ?? 1) - 1, d ?? 1);
}
export function addDays(day: string, n: number): string {
  const d = parseDay(day);
  d.setDate(d.getDate() + n);
  return isoDay(d);
}
/** Senin minggu berjalan (roster mingguan dimulai Senin). */
export function weekStart(day: string): string {
  const d = parseDay(day);
  const dow = d.getDay(); // 0 = Minggu
  d.setDate(d.getDate() - (dow === 0 ? 6 : dow - 1));
  return isoDay(d);
}
export function weekDays(start: string): string[] {
  return Array.from({ length: 7 }, (_, i) => addDays(start, i));
}
/** Label kolom roster: "Sen 28/9". */
export function dayLabel(day: string): string {
  const d = parseDay(day);
  return `${WEEKDAY_SHORT[d.getDay()]} ${d.getDate()}/${d.getMonth() + 1}`;
}


/** Roster dikelompokkan per shift × tanggal untuk grid mingguan. */
export function groupRoster(entries: RosterEntry[]): Map<string, RosterEntry[]> {
  const out = new Map<string, RosterEntry[]>();
  for (const e of entries) {
    const k = `${e.shift_id}|${e.shift_date}`;
    const list = out.get(k);
    if (list) list.push(e);
    else out.set(k, [e]);
  }
  return out;
}

// ---------- Kehadiran / serah terima ----------
export const CLOCK_SOURCE: LabelMap = { web: "Web", mobile: "Staff App", sync: "Staff App (offline)", system: "Sistem" };
export const GPS_LABEL: LabelMap = { captured: "GPS tercatat", unavailable: "GPS tidak tersedia", denied: "Izin GPS ditolak" };
export const HANDOVER_GROUP_LABEL: LabelMap = {
  active_emergencies: "Emergency aktif", active_incidents: "Incident aktif", pending_patrols: "Patrol tertunda", open_findings: "Finding terbuka",
  unfinished_cleaning: "Area belum selesai", rework_tasks: "Rework (re-clean)",
};
/** Urutan grup snapshot serah terima per domain (server hanya mengisi grup domainnya). */
export const HANDOVER_GROUPS: Record<ShiftDomain, string[]> = {
  security: ["active_emergencies", "active_incidents", "pending_patrols", "open_findings"],
  housekeeping: ["unfinished_cleaning", "rework_tasks", "open_findings"],
};

// ---------- Kompetensi ----------
export const SKILL_LEVELS: LabelMap = { basic: "Dasar", intermediate: "Menengah", advanced: "Mahir", expert: "Ahli" };
export const CERT_TYPES: LabelMap = { license: "Lisensi", certificate: "Sertifikat", training: "Pelatihan" };
/** Teks masa berlaku dari days_to_expire server (validity: valid | expiring | expired | no_expiry). */
export function validityText(status: string | null | undefined, days: number | null | undefined): string {
  if (status === "no_expiry" || days === null || days === undefined) return "Tanpa masa berlaku";
  if (days < 0) return `Kedaluwarsa ${Math.abs(days)} hari lalu`;
  if (days === 0) return "Kedaluwarsa hari ini";
  return `Berlaku ${days} hari lagi`;
}

// ---------- Kapasitas (P2-WKL-03) ----------
// Status kapasitas adalah kondisi metrik (bukan status object di status map): ok | tight | over | no_staff | idle.
export const CAPACITY_LABEL: LabelMap = { ok: "Aman", tight: "Ketat", over: "Melebihi kapasitas", no_staff: "Tanpa staf on-duty", idle: "Tidak ada beban" };
export function capacityTone(status: string): Tone {
  switch (status) {
    case "ok":
      return "success";
    case "tight":
      return "warning";
    case "over":
    case "no_staff":
      return "error";
    default:
      return "neutral";
  }
}
/** Persentase bar beban (0–100, dibatasi) dari load_ratio server. */
export function loadPct(ratio: number | null | undefined): number {
  if (!ratio || !Number.isFinite(ratio) || ratio < 0) return 0;
  return Math.min(100, Math.round(ratio * 100));
}
