// StatusBadge / PriorityBadge / SeverityBadge / AssetStatusBadge / FlagBadge (DS §2.2, §4.1)
// Warna & label dari status-map (generated) — tidak menerima warna manual.
import type * as React from "react";
import { cn } from "@/lib/utils";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { toneColor, toneContainer, toneOnColor, toneOnContainer, type Tone } from "@buildingvision/ui/bv";
import { Badge } from "@/components/ui/primitives";
import { statusDef, type ObjectType, type Semantic, type Variant } from "@/lib/status-map";
import { statusCategory } from "@/lib/status-category";
import { deriveFlags, type FlagSource } from "@/lib/status";

// nama ikon status-map (lucide) → Material Symbols Rounded (satu keluarga ikon, DS Guideline §2.6)
const icons: Record<string, string> = { "alarm-clock": "alarm", timer: "timer", "timer-off": "timer_off", "image-off": "hide_image", "arrow-down": "arrow_downward", minus: "remove", "arrow-up": "arrow_upward", "alert-triangle": "warning", "cloud-upload": "cloud_upload", "cloud-check": "cloud_done", "cloud-off": "cloud_off", "git-merge": "merge_type", "rotate-ccw": "replay", circle: "radio_button_unchecked", play: "play_arrow", pause: "pause", check: "check", "check-check": "done_all", "x-circle": "cancel" };

const semanticTone: Record<Semantic, Tone> = { success: "success", warning: "warning", critical: "error", info: "info", neutral: "neutral" };

/** Pasangan warna solid (container + on-container) per semantic; `solid` memakai fill penuh + on-color (status aktif). */
export function semanticStyle(semantic: Semantic, variant: Variant): React.CSSProperties {
  const tone = semanticTone[semantic];
  if (variant === "solid") return { backgroundColor: toneColor[tone], color: toneOnColor[tone] };
  if (variant === "outline") return { backgroundColor: "transparent", color: "var(--color-on-surface-variant)", boxShadow: "inset 0 0 0 1px var(--color-border)" };
  return { backgroundColor: toneContainer[tone], color: toneOnContainer[tone] };
}
/** Kompat: kelas Tailwind (alias token) untuk kode yang masih memakai className. */
export function semanticClass(semantic: Semantic, variant: Variant): string {
  const t = semanticTone[semantic];
  if (variant === "solid") return t === "neutral" ? "bg-outline text-surface" : `bg-${t} text-on-${t}`;
  if (variant === "outline") return "border border-border bg-transparent text-on-surface-variant";
  return t === "neutral" ? "bg-surface-container-high text-on-surface-variant" : `bg-${t}-container text-on-${t}-container`;
}

export function StatusBadge({ objectType, status, className }: { objectType: ObjectType; status: string; className?: string }) {
  const { t } = useTranslation();
  const def = statusDef(objectType, status);
  if (!def) return <Badge className={className}>{status}</Badge>;
  return (
    <Badge dot={def.variant === "solid"} className={className} style={semanticStyle(def.semantic, def.variant)} data-status-category={statusCategory(objectType, status)}>
      {t(`status.${objectType}.${status}`, { defaultValue: def.label_id })}
    </Badge>
  );
}

/** Status indicator kategori umum PRD P0 §20.2 (New · In Progress · Pending · Completed · Closed · Cancelled · Overdue · Critical). */
export function CommonStatusBadge({ category, className }: { category: string; className?: string }) {
  const { t } = useTranslation();
  const def = statusDef("common", category);
  if (!def) return <Badge className={className}>{category}</Badge>;
  const icon = def.icon ? icons[def.icon] : undefined;
  return (
    <Badge className={className} style={semanticStyle(def.semantic, def.variant)}>
      {icon && <Icon name={icon} size={12} aria-hidden />}
      {t(`status.common.${category}`, { defaultValue: def.label_id })}
    </Badge>
  );
}

function LevelBadge({ group, level, prefix, className }: { group: "priority" | "severity"; level: string; prefix?: string; className?: string }) {
  const { t } = useTranslation();
  const def = statusDef(group, level);
  if (!def) return null;
  const icon = def.icon ? icons[def.icon] : undefined;
  return (
    <Badge className={className} style={semanticStyle(def.semantic, def.variant)} title={`${group === "priority" ? "Prioritas" : "Severity"}: ${def.label_id}`}>
      {icon && <Icon name={icon} size={12} aria-hidden />}
      {prefix}
      {t(`status.${group}.${level}`, { defaultValue: def.label_id })}
    </Badge>
  );
}
export const PriorityBadge = ({ priority, className }: { priority: string; className?: string }) => <LevelBadge group="priority" level={priority} className={className} />;
export const SeverityBadge = ({ severity, className }: { severity: string; className?: string }) => <LevelBadge group="severity" level={severity} prefix="Severity " className={className} />;
export const AssetStatusBadge = ({ status, className }: { status: string; className?: string }) => <StatusBadge objectType="asset" status={status} className={className} />;
/** Status SLA (PRD P1 v2 §21.3): on_track · at_risk · breached · completed; kosong (tanpa SLA/due) → tidak dirender. */
export const SLAStatusBadge = ({ status, className }: { status?: string | null; className?: string }) => (status ? <StatusBadge objectType="sla_status" status={status} className={className} /> : null);

export type Flag = "overdue" | "sla_risk" | "sla_breach" | "evidence_incomplete" | "reopened" | "critical" | "escalated";
export function FlagBadge({ flag, className }: { flag: Flag; className?: string }) {
  const { t } = useTranslation();
  const def = statusDef("flags", flag);
  if (!def) return null;
  const icon = def.icon ? icons[def.icon] : undefined;
  return (
    <Badge className={className} style={semanticStyle(def.semantic, def.variant)}>
      {icon && <Icon name={icon} size={12} aria-hidden />}
      {t(`status.flags.${flag}`, { defaultValue: def.label_id })}
    </Badge>
  );
}
/**
 * Flag item: `flags` dari API, atau `item` (flags + reopen_count + priority/severity). `reopened` tampil bila
 * flags.reopened / reopen_count > 0; `critical` hanya bila `showCritical` (hindari dobel dengan PriorityBadge).
 */
export function FlagBadges({ flags, item, showCritical, className }: { flags?: string[]; item?: FlagSource; showCritical?: boolean; className?: string }) {
  const list = deriveFlags({ ...item, flags: flags ?? item?.flags }, { critical: showCritical });
  if (!list.length) return null;
  return (
    <span className={cn("inline-flex flex-wrap gap-1", className)}>
      {list.map((f) => (
        <FlagBadge key={f} flag={f as Flag} />
      ))}
    </span>
  );
}

export function SyncStateBadge({ state }: { state: "pending" | "synced" | "failed" | "conflict" }) {
  const def = statusDef("sync_state", state);
  if (!def) return null;
  const icon = def.icon ? icons[def.icon] : undefined;
  return (
    <Badge style={semanticStyle(def.semantic, def.variant)}>
      {icon && <Icon name={icon} size={12} aria-hidden />}
      {def.label_id}
    </Badge>
  );
}

// Tipe object → label canonical (Naming Convention §44: object canonical tidak diterjemahkan)
export const objectTypeLabel: Record<string, string> = { task: "Task", work_order: "Work Order", service_request: "Service Request", incident: "Incident", finding: "Finding", asset: "Asset", maintenance_schedule: "Maintenance", inspection: "Inspection", tenant: "Tenant", location: "Lokasi", property: "Property", building: "Building", tower: "Tower", floor: "Floor", area: "Area", space: "Space", unit: "Unit", user: "User", vendor: "Vendor", system: "Sistem", emergency_alert: "Emergency Alert", parking_violation: "Parking Violation", lost_found_item: "Lost & Found", recurring_issue: "Isu Berulang",
  // PRD P2 v2.1 (Workforce Operations): object Attention Required / notifikasi
  workforce: "Workforce", asset_document: "Dokumen Equipment", shift_handover: "Serah Terima Shift", cleaning_route_run: "Cleaning Route Run", inventory_item: "Item Inventory" };
export const workTypeLabel: Record<string, string> = { general: "General", patrol: "Patrol", cleaning: "Cleaning", inspection: "Inspection", routine_maintenance: "Routine Maintenance", maintenance: "Maintenance", corrective: "Corrective", preventive: "Preventive", repair: "Repair", service: "Service", other: "Other" };
/** Tipe Service Request (PRD P1 v2 §27.2). */
export const requestTypeLabel: Record<string, string> = { service_request: "Service Request", complaint: "Complaint", maintenance_request: "Maintenance Request", cleaning_request: "Cleaning Request", facility_issue: "Facility Issue", other: "Other" };
/** Kategori task (PRD P1 v2 §13.3) — saran; server menerima kode bebas ≤60 karakter. */
export const taskCategoryLabel: Record<string, string> = { general: "General", engineering: "Engineering", electrical: "Electrical", plumbing: "Plumbing", hvac: "HVAC", cleaning: "Cleaning", security: "Security", safety: "Safety", landscaping: "Landscaping", inspection: "Inspection", tenant_service: "Tenant Service" };
export const categoryText = (c?: string | null) => (c ? taskCategoryLabel[c] ?? c.replace(/_/g, " ") : "—");
