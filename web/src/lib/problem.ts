// Helper problem+json (RFC 9457) untuk UX state: 403 → Akses ditolak, 404 → tidak ditemukan (PRD P0 §23, §24.1).
import type { Problem } from "./api";

/** Problem dari error apa pun (ApiError, Error biasa, atau undefined). */
export function problemOf(err: unknown): Partial<Problem> & { message?: string } {
  const e = err as { problem?: Problem; message?: string; status?: number } | null | undefined;
  if (!e) return {};
  if (e.problem) return { ...e.problem, message: e.message };
  return { status: typeof e.status === "number" ? e.status : undefined, message: e.message };
}
export const isForbidden = (err: unknown) => problemOf(err).status === 403;
export const isNotFound = (err: unknown) => problemOf(err).status === 404;

/** Error validasi per field (problem+json `errors[]`) → { field: pesan } untuk ditampilkan di bawah input. */
export function fieldErrorsOf(err: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  for (const e of problemOf(err).errors ?? []) {
    if (e?.field && !out[e.field]) out[e.field] = e.message;
  }
  return out;
}
