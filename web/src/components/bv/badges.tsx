// StatusBadge / PriorityBadge / SeverityBadge / AssetStatusBadge / FlagBadge (DS §2.2, §4.1)
// Warna & label dari status-map (generated) — tidak menerima warna manual.
import type * as React from "react";
import { cn } from "@/lib/utils";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { toneColor, toneContainer, toneOnColor, toneOnContainer, type Tone } from "@buildingvision/ui/bv";
import { Badge } from "@/components/ui/primitives";
import { statusDef, type ObjectType, type Semantic, type Variant } from "@/lib/status-map";

// nama ikon status-map (lucide) → Material Symbols Rounded (satu keluarga ikon, DS Guideline §2.6)
const icons: Record<string, string> = { "alarm-clock": "alarm", timer: "timer", "timer-off": "timer_off", "image-off": "hide_image", "arrow-down": "arrow_downward", minus: "remove", "arrow-up": "arrow_upward", "alert-triangle": "warning", "cloud-upload": "cloud_upload", "cloud-check": "cloud_done", "cloud-off": "cloud_off", "git-merge": "merge_type" };

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
    <Badge dot={def.variant === "solid"} className={className} style={semanticStyle(def.semantic, def.variant)}>
      {t(`status.${objectType}.${status}`, { defaultValue: def.label_id })}
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

export type Flag = "overdue" | "sla_risk" | "sla_breach" | "evidence_incomplete";
export function FlagBadge({ flag, className }: { flag: Flag; className?: string }) {
  const def = statusDef("flags", flag);
  if (!def) return null;
  const icon = def.icon ? icons[def.icon] : undefined;
  return (
    <Badge className={className} style={semanticStyle(def.semantic, def.variant)}>
      {icon && <Icon name={icon} size={12} aria-hidden />}
      {def.label_id}
    </Badge>
  );
}
export function FlagBadges({ flags, className }: { flags?: string[]; className?: string }) {
  if (!flags?.length) return null;
  const ordered = flags.filter((f) => f !== "sla_risk" || !flags.includes("sla_breach")) as Flag[];
  return (
    <span className={cn("inline-flex flex-wrap gap-1", className)}>
      {ordered.map((f) => (
        <FlagBadge key={f} flag={f} />
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
export const objectTypeLabel: Record<string, string> = { task: "Task", work_order: "Work Order", service_request: "Service Request", incident: "Incident", finding: "Finding", asset: "Asset", maintenance_schedule: "Maintenance", inspection: "Inspection", tenant: "Tenant", location: "Lokasi", property: "Property", building: "Building", unit: "Unit" };
export const workTypeLabel: Record<string, string> = { general: "General", patrol: "Patrol", cleaning: "Cleaning", inspection: "Inspection", routine_maintenance: "Routine Maintenance", maintenance: "Maintenance", corrective: "Corrective", repair: "Repair", service: "Service" };
