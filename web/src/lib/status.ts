// Helper label status dari kontrak (contracts/status-map.yaml) — pengganti peta status lokal per halaman.
import i18n from "./i18n";
import { statusDef, statusMap, type ObjectType } from "./status-map";

/** Label status sesuai bahasa aktif (fallback: kode status mentah). */
export function statusLabel(objectType: ObjectType, status: string | null | undefined): string {
  if (!status) return "—";
  const def = statusDef(objectType, status);
  if (!def) return status;
  return i18n.t(`status.${objectType}.${status}`, { defaultValue: i18n.language === "en" ? def.label_en : def.label_id });
}

/** Opsi <select> filter status untuk suatu object type. */
export function statusOptions(objectType: ObjectType): { value: string; label: string }[] {
  return Object.keys(statusMap[objectType] ?? {}).map((value) => ({ value, label: statusLabel(objectType, value) }));
}

export interface FlagSource {
  flags?: string[] | Record<string, boolean> | null;
  reopen_count?: number | null;
  is_overdue?: boolean | null;
  priority?: string | null;
  severity?: string | null;
}

/**
 * Flag yang ditampilkan untuk suatu item (PRD P0 §20.2 Status System): flags API + `reopened` (flags.reopened atau
 * reopen_count > 0) + `critical` (opt-in, priority/severity = critical). Urutan: overdue, SLA, critical, reopened, evidence.
 */
export function deriveFlags(src: FlagSource, opts: { critical?: boolean } = {}): string[] {
  const raw = src.flags;
  const list = Array.isArray(raw) ? [...raw] : raw ? Object.entries(raw).filter(([, v]) => v).map(([k]) => k) : [];
  const set = new Set(list);
  if (src.is_overdue) set.add("overdue");
  if ((src.reopen_count ?? 0) > 0) set.add("reopened");
  if (opts.critical && (src.priority === "critical" || src.severity === "critical")) set.add("critical");
  if (set.has("sla_breach")) set.delete("sla_risk");
  const order = ["overdue", "sla_breach", "sla_risk", "critical", "escalated", "reopened", "evidence_incomplete"];
  return [...set].sort((a, b) => (order.indexOf(a) + 1 || 99) - (order.indexOf(b) + 1 || 99));
}
