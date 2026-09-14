// Object cards (DS §4.2): WorkOrderCard · TaskCard · ServiceRequestCard · IncidentCard; TodayCounter (§4.3); AttentionRequiredList (§4.4)
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Clock, User2, Users2 } from "lucide-react";
import { Button, Card } from "@/components/ui/primitives";
import { FlagBadges, PriorityBadge, SeverityBadge, StatusBadge, objectTypeLabel, workTypeLabel } from "./badges";
import { LocationPath, RelativeTime } from "./common";
import { fmtDateTime, fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { AttentionItem, Incident, ServiceRequest, WorkItem } from "@/api/types";

// bar kiri 4px = flag tertinggi: Critical (overdue/breach), Warning (SLA risk); status tetap di badge
function accentClass(flags?: string[]): string {
  if (!flags?.length) return "border-l-transparent";
  if (flags.includes("overdue") || flags.includes("sla_breach")) return "border-l-critical";
  if (flags.includes("sla_risk") || flags.includes("evidence_incomplete")) return "border-l-warning";
  return "border-l-transparent";
}

export function itemLink(ot: string, id: string): string {
  return ({ task: "/operations/tasks/", work_order: "/operations/work-orders/", service_request: "/operations/service-requests/", incident: "/operations/incidents/", finding: "/findings/", maintenance_schedule: "/engineering/preventive-maintenance/", asset: "/assets/" }[ot] ?? "/") + id;
}

export function WorkItemCard({ item, compact, actions }: { item: WorkItem; compact?: boolean; actions?: React.ReactNode }) {
  const to = itemLink(item.object_type, item.id);
  const assignee = item.assignee.user_name ?? item.assignee.team_name;
  return (
    <Card className={cn("border-l-4 p-3", accentClass(item.flags))}>
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-body-strong font-semibold">
            <Link to={to} className="font-mono text-[13px] hover:underline">{item.number}</Link>
            <span className="text-sm font-normal text-muted-foreground">· {workTypeLabel[item.type] ?? item.type}</span>
          </div>
          <Link to={to} className="line-clamp-2 block text-body font-medium text-foreground hover:underline">{item.title}</Link>
        </div>
        <div className="flex shrink-0 flex-wrap justify-end gap-1">
          <StatusBadge objectType={item.object_type} status={item.status} />
          <FlagBadges flags={item.flags} />
        </div>
      </div>
      <div className={cn("mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground", compact && "mt-1")}>
        <LocationPath pathText={item.location.path_text} />
        {item.asset.asset_code && <span className="font-mono text-xs">{item.asset.asset_code}</span>}
      </div>
      {!compact && (
        <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
          <span className="inline-flex items-center gap-1">{item.assignee.team_id && !item.assignee.user_id ? <Users2 className="h-3.5 w-3.5" /> : <User2 className="h-3.5 w-3.5" />}{assignee ?? <em>belum ditugaskan</em>}</span>
          <span className={cn("inline-flex items-center gap-1 tnum", item.is_overdue && "text-critical-text")}><Clock className="h-3.5 w-3.5" />Due {fmtDateTime(item.due_at)}</span>
          <PriorityBadge priority={item.priority} />
        </div>
      )}
      {actions && <div className="mt-3 flex justify-end gap-2">{actions}</div>}
    </Card>
  );
}
export const WorkOrderCard = WorkItemCard;
export const TaskCard = WorkItemCard;

export function ServiceRequestCard({ item, compact }: { item: ServiceRequest; compact?: boolean }) {
  const to = itemLink("service_request", item.id);
  return (
    <Card className={cn("border-l-4 p-3", accentClass(item.flags))}>
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-body-strong font-semibold">
            <Link to={to} className="font-mono text-[13px] hover:underline">{item.request_number}</Link>
            <span className="text-sm font-normal text-muted-foreground">· {item.category_name ?? item.category_code}</span>
          </div>
          <Link to={to} className="line-clamp-2 block text-body font-medium hover:underline">{item.title}</Link>
        </div>
        <div className="flex shrink-0 flex-wrap justify-end gap-1">
          <StatusBadge objectType="service_request" status={item.status} />
          <FlagBadges flags={item.flags} />
        </div>
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
        {item.tenant_name && <span>{item.tenant_name}</span>}
        <LocationPath pathText={item.location.path_text} />
        {!compact && <PriorityBadge priority={item.priority} />}
        {!compact && <RelativeTime value={item.created_at} />}
      </div>
    </Card>
  );
}

export function IncidentCard({ item, compact }: { item: Incident; compact?: boolean }) {
  const to = itemLink("incident", item.id);
  return (
    <Card className={cn("border-l-4 p-3", item.severity === "critical" ? "border-l-critical" : accentClass(item.flags))}>
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-body-strong font-semibold">
            <Link to={to} className="font-mono text-[13px] hover:underline">{item.incident_number}</Link>
            <span className="text-sm font-normal text-muted-foreground">· {item.category.replace(/_/g, " ")}</span>
          </div>
          <Link to={to} className="line-clamp-2 block text-body font-medium hover:underline">{item.title}</Link>
        </div>
        <div className="flex shrink-0 flex-wrap justify-end gap-1">
          <StatusBadge objectType="incident" status={item.status} />
          <SeverityBadge severity={item.severity} />
        </div>
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
        <LocationPath pathText={item.location.path_text} />
        {!compact && <span>{item.assignee.user_name ?? item.assignee.team_name ?? <em>belum ditugaskan</em>}</span>}
        <RelativeTime value={item.reported_at} />
      </div>
    </Card>
  );
}

// ---------- TodayCounter (DS §4.3): angka display tabular + exception breakdown ----------
export function TodayCounter({ label, value, breakdown, link, loading }: { label: string; value?: number; breakdown?: { label: string; value: number; tone: "critical" | "warning" | "info" | "neutral"; link?: string }[]; link: string; loading?: boolean }) {
  return (
    <Card className="p-4">
      <Link to={link} className="block text-sm text-muted-foreground hover:underline">{label}</Link>
      <div className="mt-1 text-display font-bold tnum leading-9">{loading ? <span className="inline-block h-8 w-16 animate-pulse rounded bg-muted" /> : fmtNumber(value ?? 0)}</div>
      {breakdown && breakdown.length > 0 && (
        <div className="mt-1 flex flex-wrap gap-x-3 text-xs">
          {breakdown.map((b) => (
            <Link key={b.label} to={b.link ?? link} className={cn("tnum hover:underline", b.tone === "critical" && "text-critical-text", b.tone === "warning" && "text-warning-text", b.tone === "info" && "text-info-text", b.tone === "neutral" && "text-muted-foreground")}>
              <span className="font-semibold">{fmtNumber(b.value)}</span> {b.label}
            </Link>
          ))}
        </div>
      )}
    </Card>
  );
}

// ---------- AttentionRequiredList (DS §4.4): severity · object · location · age/due · CTA (dari allowed_actions server) ----------
const categoryLabel: Record<string, string> = { sla_breach: "SLA Breach", sla_risk: "SLA Risk", maintenance_overdue: "Maintenance Overdue", patrol_overdue: "Patrol Overdue", unresolved_finding: "Finding Belum Selesai", critical_incident: "Incident Kritis", incident: "Incident", overdue: "Overdue", sync_conflict: "Sync Conflict" };

export function AttentionRequiredList({ items, onAction, total, limit = 10, seeAllTo }: { items: AttentionItem[]; onAction?: (item: AttentionItem, action: string) => void; total?: number; limit?: number; seeAllTo?: string }) {
  const { t } = useTranslation();
  const shown = items.slice(0, limit);
  return (
    <div className="divide-y divide-border">
      {shown.map((it) => (
        <div key={it.object_type + it.object_id + it.category} className="flex items-start gap-3 py-2.5">
          <span aria-label={it.severity} className={cn("mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full", it.severity === "critical" ? "bg-critical" : "bg-warning")} />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className={cn("text-xs font-semibold uppercase tracking-wide", it.severity === "critical" ? "text-critical-text" : "text-warning-text")}>{categoryLabel[it.category] ?? it.category}</span>
              <Link to={it.deep_link} className="font-mono text-[13px] font-semibold hover:underline">{it.label}</Link>
              <span className="truncate text-body">{it.title}</span>
            </div>
            <div className="mt-0.5 flex flex-wrap items-center gap-x-3 text-sm text-muted-foreground">
              {it.location_path && <LocationPath pathText={it.location_path} />}
              <span className="text-xs">{objectTypeLabel[it.object_type] ?? it.object_type}</span>
              <span className={cn("tnum text-xs", it.severity === "critical" ? "text-critical-text" : "text-warning-text")}>
                {it.category === "overdue" || it.category === "patrol_overdue" || it.category === "maintenance_overdue" ? "due " : ""}
                <RelativeTime value={it.since} />
              </span>
              {it.assignee_name && <span className="text-xs">{it.assignee_name}</span>}
            </div>
          </div>
          <div className="flex shrink-0 gap-1">
            {it.allowed_actions.filter((a) => a !== "view").slice(0, 2).map((a) => (
              <Button key={a} size="sm" variant="secondary" onClick={() => onAction?.(it, a)}>
                {t(`action.${a}`, { defaultValue: a })}
              </Button>
            ))}
            <Button size="sm" variant="ghost" asChild>
              <Link to={it.deep_link}>{t("action.view")}</Link>
            </Button>
          </div>
        </div>
      ))}
      {total !== undefined && total > shown.length && seeAllTo && (
        <div className="pt-3 text-sm">
          <Link to={seeAllTo} className="text-brand-600 hover:underline">{t("overview.see_all", { n: total })}</Link>
        </div>
      )}
    </div>
  );
}
