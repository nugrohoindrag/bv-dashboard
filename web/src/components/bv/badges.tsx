// StatusBadge / PriorityBadge / SeverityBadge / AssetStatusBadge / FlagBadge (DS §2.2, §4.1)
// Warna & label dari status-map (generated) — tidak menerima warna manual.
import { useTranslation } from "react-i18next";
import { AlarmClock, AlertTriangle, ArrowDown, ArrowUp, CloudCheck, CloudOff, CloudUpload, GitMerge, ImageOff, Minus, Timer, TimerOff, type LucideIcon } from "lucide-react";
import { Badge } from "@/components/ui/primitives";
import { statusDef, type ObjectType, type Semantic, type Variant } from "@/lib/status-map";
import { cn } from "@/lib/utils";

const icons: Record<string, LucideIcon> = { "alarm-clock": AlarmClock, timer: Timer, "timer-off": TimerOff, "image-off": ImageOff, "arrow-down": ArrowDown, minus: Minus, "arrow-up": ArrowUp, "alert-triangle": AlertTriangle, "cloud-upload": CloudUpload, "cloud-check": CloudCheck, "cloud-off": CloudOff, "git-merge": GitMerge };

export function semanticClass(semantic: Semantic, variant: Variant): string {
  if (variant === "solid") return `bg-${semantic} text-white`;
  if (variant === "outline") return "border border-neutral-300 bg-transparent text-neutral-text";
  return `bg-${semantic}-soft text-${semantic}-text`;
}

export function StatusBadge({ objectType, status, className }: { objectType: ObjectType; status: string; className?: string }) {
  const { t } = useTranslation();
  const def = statusDef(objectType, status);
  if (!def) return <Badge className={cn("bg-neutral-soft text-neutral-text", className)}>{status}</Badge>;
  return (
    <Badge dot={def.variant === "solid"} className={cn(semanticClass(def.semantic, def.variant), className)}>
      {t(`status.${objectType}.${status}`, { defaultValue: def.label_id })}
    </Badge>
  );
}

function LevelBadge({ group, level, prefix, className }: { group: "priority" | "severity"; level: string; prefix?: string; className?: string }) {
  const { t } = useTranslation();
  const def = statusDef(group, level);
  if (!def) return null;
  const Icon = def.icon ? icons[def.icon] : undefined;
  return (
    <Badge className={cn(semanticClass(def.semantic, def.variant), className)} title={`${group === "priority" ? "Prioritas" : "Severity"}: ${def.label_id}`}>
      {Icon && <Icon className="h-3 w-3" aria-hidden />}
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
  const Icon = def.icon ? icons[def.icon] : undefined;
  return (
    <Badge className={cn(semanticClass(def.semantic, def.variant), className)}>
      {Icon && <Icon className="h-3 w-3" aria-hidden />}
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
  const Icon = def.icon ? icons[def.icon] : undefined;
  return (
    <Badge className={semanticClass(def.semantic, def.variant)}>
      {Icon && <Icon className="h-3 w-3" aria-hidden />}
      {def.label_id}
    </Badge>
  );
}

// Tipe object → label canonical (Naming Convention §44: object canonical tidak diterjemahkan)
export const objectTypeLabel: Record<string, string> = { task: "Task", work_order: "Work Order", service_request: "Service Request", incident: "Incident", finding: "Finding", asset: "Asset", maintenance_schedule: "Maintenance", inspection: "Inspection", tenant: "Tenant", location: "Lokasi", property: "Property", building: "Building", unit: "Unit" };
export const workTypeLabel: Record<string, string> = { general: "General", patrol: "Patrol", cleaning: "Cleaning", inspection: "Inspection", routine_maintenance: "Routine Maintenance", maintenance: "Maintenance", corrective: "Corrective", repair: "Repair", service: "Service" };
