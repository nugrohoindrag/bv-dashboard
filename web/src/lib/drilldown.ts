// Drill-down dashboard → parameter API daftar (PRD P2 v2.1 Principle 9 "every KPI drills down"). Server dashboard
// (api/internal/overview/domains.go) menautkan KPI ke URL daftar dengan parameter ringkas (date=today, reported=today,
// from/to, result=fail,partial); fungsi di sini menerjemahkannya ke filter yang diterima endpoint daftar (parseListFilter,
// incidents, findings) tanpa mengubah URL — chip filter tetap menampilkan parameter asal. Parameter yang belum didukung
// server dikembalikan sebagai `notes` agar halaman dapat memberi tahu pengguna.

export type Params = Record<string, string | undefined>;
export interface Translated { query: Params; notes: string[] }

/** YYYY-MM-DD kalender lokal (browser ≈ zona waktu property). */
export function localDay(d: Date = new Date()): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}
const DAY = /^\d{4}-\d{2}-\d{2}$/;
/** "today" | YYYY-MM-DD → YYYY-MM-DD; lainnya → null. */
export function resolveDay(v: string | null | undefined, now: Date = new Date()): string | null {
  if (!v) return null;
  if (v === "today") return localDay(now);
  return DAY.test(v) ? v : null;
}
/** Awal hari lokal (inklusif) sebagai RFC3339 UTC. */
export function startOfDayISO(day: string): string {
  const [y, m, d] = day.split("-").map(Number);
  return new Date(y, m - 1, d, 0, 0, 0, 0).toISOString();
}
/** Akhir hari lokal (inklusif, 23:59:59.999) sebagai RFC3339 UTC. */
export function endOfDayISO(day: string): string {
  const [y, m, d] = day.split("-").map(Number);
  return new Date(y, m - 1, d, 23, 59, 59, 999).toISOString();
}

type RangeTarget = "completed" | "due" | "created";

/**
 * Task / Work Order (parseListFilter): `date` → `scheduled_on` (tanggal jadwal di zona waktu property);
 * `from`/`to` → rentang `completed_*` (inspeksi selesai), `due_*` (patrol), atau `created_*`; `result` (hasil inspeksi
 * pass|fail|partial) diteruskan apa adanya.
 */
export function workItemQuery(params: Params, range: RangeTarget = "completed", now: Date = new Date()): Translated {
  const q: Params = { ...params };
  const notes: string[] = [];
  const day = resolveDay(q.date, now);
  if (q.date !== undefined) {
    delete q.date;
    if (day) q.scheduled_on = `${day}T00:00:00Z`;
  }
  const from = resolveDay(q.from, now);
  const to = resolveDay(q.to, now);
  if (q.from !== undefined || q.to !== undefined) {
    delete q.from;
    delete q.to;
    if (from) q[`${range}_from`] = startOfDayISO(from);
    if (to) q[`${range}_to`] = endOfDayISO(to);
  }
  return { query: q, notes };
}

/** Incident: `reported=today|YYYY-MM-DD` dan `from`/`to` → `created_from`/`created_to` (server memfilter reported_at). */
export function incidentQuery(params: Params, now: Date = new Date()): Translated {
  const q: Params = { ...params };
  const reported = resolveDay(q.reported, now);
  if (q.reported !== undefined) {
    delete q.reported;
    if (reported) {
      q.created_from = startOfDayISO(reported);
      q.created_to = endOfDayISO(reported);
    }
  }
  const from = resolveDay(q.from, now);
  const to = resolveDay(q.to, now);
  if (q.from !== undefined || q.to !== undefined) {
    delete q.from;
    delete q.to;
    if (from) q.created_from = startOfDayISO(from);
    if (to) q.created_to = endOfDayISO(to);
  }
  return { query: q, notes: [] };
}

/** Finding: `from`/`to` → `created_from`/`created_to`; `asset_id` diteruskan apa adanya. */
export function findingQuery(params: Params, now: Date = new Date()): Translated {
  const q: Params = { ...params };
  const from = resolveDay(q.from, now);
  const to = resolveDay(q.to, now);
  if (q.from !== undefined || q.to !== undefined) {
    delete q.from;
    delete q.to;
    if (from) q.created_from = startOfDayISO(from);
    if (to) q.created_to = endOfDayISO(to);
  }
  return { query: q, notes: [] };
}

/** Visitor: `date=today|YYYY-MM-DD` → tanggal (default hari ini). */
export function visitorDate(v: string | null | undefined, now: Date = new Date()): string {
  return resolveDay(v, now) ?? localDay(now);
}
