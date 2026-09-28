// Kartu KPI dashboard domain (PRD P2 v2.1; Roadmap v2.1 Principle 9 "every KPI drills down"): nilai sesuai unit server,
// rail + badge dari severity server (normal tanpa rail), definisi singkat (hint) sebagai tooltip & teks, seluruh kartu
// menautkan ke drill-down server. Dipakai DomainDashboardPage dan katalog Design System. Uang tampil ringkas ("Rp 1,25 M";
// nilai penuh di tooltip & label aksesibilitas) agar muat di kartu (PRD P4 v2.1 P4-FIN-01, P4-FIN-04).
import { Link } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import { Badge, Card } from "@/components/ui/primitives";
import type { DashboardKpi } from "@/api/types";
import { cn } from "@/lib/utils";
import { SEVERITY_LABEL, formatKpiDisplay, kpiSuffix, severityTone } from "./dashboard";

export function KpiCard({ kpi }: { kpi: DashboardKpi }) {
  const tone = severityTone(kpi.severity);
  const suffix = kpiSuffix(kpi.unit);
  const { text: value, title: full } = formatKpiDisplay(kpi.unit, kpi.value, kpi.key);
  return (
    <Card railTone={tone} interactive className="min-w-0">
      <Link to={kpi.drill_down || "#"} className="flex h-full flex-col p-4" title={kpi.hint} aria-label={`${kpi.label}: ${full}${suffix ?? ""}. ${kpi.hint}`} data-testid={`kpi-${kpi.key}`} data-severity={kpi.severity}>
        <div className="flex items-start justify-between gap-2">
          <span className="text-sm font-medium text-on-surface-variant">{kpi.label}</span>
          <Icon name="info" size={16} className="shrink-0 text-on-surface-variant" aria-hidden />
        </div>
        <div className="mt-1 flex items-baseline gap-1">
          <span className={cn("font-extrabold tnum leading-9 text-on-surface", value.length > 11 ? "text-h1" : "text-display")} title={full !== value ? full : undefined}>{value}</span>
          {suffix && <span className="text-sm text-on-surface-variant">{suffix}</span>}
        </div>
        <p className="mt-1 line-clamp-2 text-caption text-on-surface-variant">{kpi.hint}</p>
        <div className="mt-auto flex flex-wrap items-center justify-between gap-2 pt-2 text-xs">
          {tone ? <Badge tone={tone}>{SEVERITY_LABEL[kpi.severity] ?? kpi.severity}</Badge> : <span />}
          <span className="inline-flex items-center gap-0.5 font-semibold text-primary">Lihat detail<Icon name="chevron_right" size={14} aria-hidden /></span>
        </div>
      </Link>
    </Card>
  );
}
