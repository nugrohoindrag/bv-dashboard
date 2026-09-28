// Helper murni sel tabel (29 Sep 2026, tabel "clean & simple" seperti halaman Tasks).
import type { Flag } from "./badges";
import { deriveFlags, type FlagSource } from "@/lib/status";

/** Nama lokasi terakhir dari path server ("Gedung / Lantai / Ruang" → "Ruang"). */
export function lastSegment(path: string | null | undefined): string {
  const parts = (path ?? "").split(/\s*[/›>]\s*/).map((x) => x.trim()).filter(Boolean);
  return parts[parts.length - 1] ?? "—";
}

const FLAG_ORDER: Flag[] = ["overdue", "sla_breach", "critical", "sla_risk", "escalated", "reopened", "evidence_incomplete"];
/** Satu flag terpenting untuk kolom Status (semua flag tetap tampil di halaman detail). */
export function topFlag(item: FlagSource, opts: { critical?: boolean } = {}): Flag | null {
  const flags = new Set(deriveFlags(item, opts));
  return FLAG_ORDER.find((f) => flags.has(f)) ?? null;
}
