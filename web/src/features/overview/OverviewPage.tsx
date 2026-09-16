// Overview (PRD §19, DS §5.4): Row1 TodayCounter×6 · Row2 AttentionRequired (8) + Tenant Requests (4) ·
// Row3 Today's Operations tabs · Row4 PM Due 7 hari + Team Workload + Building State. Setiap panel dimuat independen.
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Card, CardContent, CardHeader, CardTitle, NativeSelect, Skeleton, THead, TBody, TD, TH, TR, Table, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { AttentionRequiredList, TodayCounter, WorkItemCard } from "@/components/bv/cards";
import { PriorityBadge, StatusBadge } from "@/components/bv/badges";
import { AsyncState, RelativeTime } from "@/components/bv/common";
import { useOverview } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime, fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { AttentionItem, BuildingState, MaintenanceSchedule, OverviewToday, TenantRequestsPanel, TodaysOperations, WorkloadRow } from "@/api/types";
import { AssignDialog } from "@/features/operations/dialogs";
import { BuildingHero } from "@buildingvision/ui/bv";
import { useOnboarding } from "@/lib/growth";
import { ChecklistView } from "@/features/growth/OnboardingPage";

export default function OverviewPage() {
  const { t } = useTranslation();
  const { propertyId, properties, can } = useAuth();
  const q = { property_id: propertyId ?? undefined };
  const today = useOverview<OverviewToday>("today", q);
  const [domain, setDomain] = useState("");
  const attention = useOverview<{ data: AttentionItem[]; total: number }>("attention-required", { ...q, domain: domain || undefined, limit: 10 });
  const tenant = useOverview<TenantRequestsPanel>("tenant-requests", q);
  const ops = useOverview<{ data: TodaysOperations[] }>("todays-operations", q);
  const pm = useOverview<{ data: MaintenanceSchedule[] }>("pm-due", { ...q, days: 7 });
  const workload = useOverview<{ data: WorkloadRow[] }>("team-workload", q);
  const building = useOverview<{ data: BuildingState[] }>("building-state", q);
  const nav = useNavigate();
  const [assign, setAssign] = useState<AttentionItem | null>(null);
  const propName = properties.find((p) => p.id === propertyId)?.name ?? t("label.all_properties");
  const d = today.data;
  const L = today.isLoading;

  const onAction = (it: AttentionItem, action: string) => {
    if (action === "assign") setAssign(it);
    else nav(it.deep_link);
  };

  // Onboarding checklist (Website PRD §28): tampil untuk organization trial sampai selesai/disembunyikan
  const onboarding = useOnboarding(can("platform.organizations.view"));
  const ob = onboarding.data;
  const showChecklist = !!ob && ob.trial?.is_trial_org && !ob.dismissed && ob.completed < ob.total;

  const canCreateWO = can("operations.work_orders.create");
  const canCreateTask = can("operations.tasks.create");
  const canReportIncident = can("operations.incidents.create");
  const overdueTotal = d?.overdue.value ?? 0;
  const slaRiskTotal = d?.sla_risk.value ?? 0;

  return (
    <div className="space-y-6">
      {showChecklist && ob && <ChecklistView data={ob} compact onChanged={() => onboarding.refetch()} />}
      {/* Hero: banner solid primary (DS Guideline §2.3), identitas property + kondisi operasional hari ini */}
      <BuildingHero
        propertyName={propName}
        context={fmtDateTime(new Date())}
        liveLabel="Live"
        headline={L ? "…" : `${fmtNumber(d?.open_work_orders.value ?? 0)} Work Order terbuka`}
        headlineSuffix={L ? undefined : `${fmtNumber(d?.tenant_requests.value ?? 0)} Service Request · ${fmtNumber(d?.incidents.value ?? 0)} Incident`}
        badge={{ icon: overdueTotal > 0 ? "warning" : "verified", label: overdueTotal > 0 ? `${fmtNumber(overdueTotal)} Overdue` : "Tanpa overdue" }}
        stats={[
          { label: "Overdue", value: L ? "–" : fmtNumber(overdueTotal), tone: overdueTotal > 0 ? "error" : "default" },
          { label: "SLA Risk", value: L ? "–" : fmtNumber(slaRiskTotal), tone: slaRiskTotal > 0 ? "warning" : "default" },
          { label: "PM Due 7 hari", value: L ? "–" : fmtNumber(d?.pm_due.value ?? 0) },
        ]}
        actions={[
          ...(canCreateWO ? [{ label: "Buat Work Order", icon: "add_task", onClick: () => nav("/operations/work-orders?new=1") }] : []),
          ...(canCreateTask ? [{ label: "Buat Task", icon: "playlist_add", onClick: () => nav("/operations/tasks?new=1") }] : []),
          ...(canReportIncident ? [{ label: "Lapor Incident", icon: "emergency_home", onClick: () => nav("/operations/incidents?new=1") }] : []),
        ]}
      />

      {/* Row 1: TodayCounter × 6 */}
      <div className="grid grid-cols-3 gap-4 2xl:grid-cols-6">
        <TodayCounter label={t("overview.open_work_orders")} loading={L} value={d?.open_work_orders.value} link="/operations/work-orders?open=true" breakdown={[{ label: "overdue", value: d?.open_work_orders.breakdown?.overdue ?? 0, tone: "critical", link: "/operations/work-orders?overdue=true" }, { label: "SLA risk", value: d?.open_work_orders.breakdown?.sla_risk ?? 0, tone: "warning", link: "/operations/work-orders?sla_risk=true" }]} />
        <TodayCounter label={t("overview.overdue")} loading={L} value={d?.overdue.value} link="/operations/work-orders?overdue=true" breakdown={[{ label: "Work Order", value: d?.overdue.breakdown?.work_orders ?? 0, tone: "critical", link: "/operations/work-orders?overdue=true" }, { label: "Task", value: d?.overdue.breakdown?.tasks ?? 0, tone: "critical", link: "/operations/tasks?overdue=true" }]} />
        <TodayCounter label={t("overview.sla_risk")} loading={L} value={d?.sla_risk.value} link="/operations/work-orders?sla_risk=true" breakdown={[{ label: "WO", value: d?.sla_risk.breakdown?.work_orders ?? 0, tone: "warning", link: "/operations/work-orders?sla_risk=true" }, { label: "Task", value: d?.sla_risk.breakdown?.tasks ?? 0, tone: "warning", link: "/operations/tasks?sla_risk=true" }, { label: "SR", value: d?.sla_risk.breakdown?.service_requests ?? 0, tone: "warning", link: "/operations/service-requests?sla_risk=true" }]} />
        <TodayCounter label={t("overview.pm_due")} loading={L} value={d?.pm_due.value} link="/engineering/preventive-maintenance" breakdown={[{ label: "overdue", value: d?.pm_due.breakdown?.overdue ?? 0, tone: "critical" }, { label: "hari ini", value: d?.pm_due.breakdown?.today ?? 0, tone: "info" }]} />
        <TodayCounter label={t("overview.incidents")} loading={L} value={d?.incidents.value} link="/operations/incidents?open=true" breakdown={[{ label: "kritis", value: d?.incidents.breakdown?.critical ?? 0, tone: "critical", link: "/operations/incidents?severity=critical" }, { label: "baru", value: d?.incidents.breakdown?.new ?? 0, tone: "info", link: "/operations/incidents?status=new" }]} />
        <TodayCounter label={t("overview.tenant_requests")} loading={L} value={d?.tenant_requests.value} link="/operations/service-requests?open=true" breakdown={[{ label: "baru hari ini", value: d?.tenant_requests.breakdown?.new_today ?? 0, tone: "info" }, { label: "SLA risk", value: d?.tenant_requests.breakdown?.sla_risk ?? 0, tone: "warning", link: "/operations/service-requests?sla_risk=true" }]} />
      </div>

      {/* Row 2 */}
      <div className="grid grid-cols-12 gap-4">
        <Card className="col-span-8">
          <CardHeader>
            <CardTitle>{t("overview.attention_required")}</CardTitle>
            <NativeSelect className="w-40" value={domain} onChange={(e) => setDomain(e.target.value)} aria-label="Domain">
              <option value="">{t("label.all")}</option>
              <option value="engineering">Engineering</option>
              <option value="security">Security</option>
              <option value="housekeeping">Housekeeping</option>
            </NativeSelect>
          </CardHeader>
          <CardContent>
            <AsyncState query={attention} empty={{ message: t("empty.attention") }} skeleton={<PanelSkeleton rows={5} />}>
              {(res) => (res.data.length ? <AttentionRequiredList items={res.data} total={res.total} onAction={onAction} seeAllTo="/operations/work-orders?overdue=true" /> : <p className="py-8 text-center text-sm text-muted-foreground">{t("empty.attention")}</p>)}
            </AsyncState>
          </CardContent>
        </Card>
        <Card className="col-span-4">
          <CardHeader>
            <CardTitle>{t("overview.tenant_requests")}</CardTitle>
            <Link to="/operations/service-requests" className="text-sm font-semibold text-primary hover:underline">{t("action.view")}</Link>
          </CardHeader>
          <CardContent>
            <AsyncState query={tenant} skeleton={<PanelSkeleton rows={4} />}>
              {(p) => (
                <>
                  <div className="mb-3 grid grid-cols-3 gap-2 text-center">
                    <Stat label={t("overview.new_today")} value={p.new_today} tone="info" />
                    <Stat label={t("overview.open")} value={p.open} />
                    <Stat label={t("label.sla_risk")} value={p.sla_risk} tone="warning" />
                  </div>
                  <ul className="divide-y divide-border">
                    {p.recent.map((r) => (
                      <li key={r.id} className="py-2">
                        <div className="flex items-center justify-between gap-2">
                          <Link to={`/operations/service-requests/${r.id}`} className="font-mono text-[13px] font-semibold hover:underline">{r.request_number}</Link>
                          <StatusBadge objectType="service_request" status={r.status} />
                        </div>
                        <div className="truncate text-sm">{r.title}</div>
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">{r.tenant_name ?? "—"} · <RelativeTime value={r.created_at} /> <PriorityBadge priority={r.priority} /></div>
                      </li>
                    ))}
                    {p.recent.length === 0 && <li className="py-6 text-center text-sm text-muted-foreground">{t("empty.service_requests")}</li>}
                  </ul>
                </>
              )}
            </AsyncState>
          </CardContent>
        </Card>
      </div>

      {/* Row 3: Today's Operations */}
      <Card>
        <CardHeader><CardTitle>{t("overview.todays_operations")}</CardTitle></CardHeader>
        <CardContent>
          <AsyncState query={ops} skeleton={<PanelSkeleton rows={3} />}>
            {(res) => (
              <Tabs defaultValue="engineering">
                <TabsList>
                  {res.data.map((p) => (
                    <TabsTrigger key={p.domain} value={p.domain}>
                      {t(`domain.${p.domain}`)} <span className="ml-1 rounded-full bg-muted px-1.5 text-xs tnum">{p.summary.total ?? 0}</span>
                      {(p.summary.overdue ?? 0) > 0 && <span className="ml-1 rounded-full bg-critical-soft px-1.5 text-xs text-critical-text tnum">{p.summary.overdue} overdue</span>}
                    </TabsTrigger>
                  ))}
                </TabsList>
                {res.data.map((p) => (
                  <TabsContent key={p.domain} value={p.domain}>
                    {p.items.length === 0 ? <p className="py-6 text-center text-sm text-muted-foreground">Tidak ada task/work order terjadwal hari ini.</p> : (
                      <div className="grid grid-cols-2 gap-3 2xl:grid-cols-3">
                        {p.items.slice(0, 12).map((it) => <WorkItemCard key={it.id} item={it} compact />)}
                      </div>
                    )}
                  </TabsContent>
                ))}
              </Tabs>
            )}
          </AsyncState>
        </CardContent>
      </Card>

      {/* Row 4 */}
      <div className="grid grid-cols-12 gap-4">
        <Card className="col-span-5">
          <CardHeader><CardTitle>{t("overview.pm_due_7")}</CardTitle><Link to="/engineering/preventive-maintenance" className="text-sm text-brand-600 hover:underline">{t("action.view")}</Link></CardHeader>
          <CardContent className="px-0">
            <AsyncState query={pm} skeleton={<PanelSkeleton rows={4} />}>
              {(res) => res.data.length === 0 ? <p className="py-6 text-center text-sm text-muted-foreground">Tidak ada PM due 7 hari ke depan.</p> : (
                <Table>
                  <THead><tr><TH>Asset</TH><TH>Plan</TH><TH>Due</TH><TH>Status</TH></tr></THead>
                  <TBody>
                    {res.data.slice(0, 8).map((s) => (
                      <TR key={s.id} className="cursor-pointer" onClick={() => nav(s.work_order_id ? `/operations/work-orders/${s.work_order_id}` : `/engineering/preventive-maintenance`)}>
                        <TD><span className="font-mono text-xs">{s.asset_code}</span> {s.asset_name}</TD>
                        <TD className="text-muted-foreground">{s.plan_name}</TD>
                        <TD className="tnum">{fmtDateTime(s.due_at)}</TD>
                        <TD><StatusBadge objectType="maintenance_schedule" status={s.status} /></TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              )}
            </AsyncState>
          </CardContent>
        </Card>
        <Card className="col-span-4">
          <CardHeader><CardTitle>{t("overview.team_workload")}</CardTitle></CardHeader>
          <CardContent className="px-0">
            <AsyncState query={workload} skeleton={<PanelSkeleton rows={4} />}>
              {(res) => res.data.length === 0 ? <p className="py-6 text-center text-sm text-muted-foreground">Belum ada penugasan.</p> : (
                <Table>
                  <THead><tr><TH>Team / Assignee</TH><TH className="text-right">Task</TH><TH className="text-right">WO</TH><TH className="text-right">Overdue</TH></tr></THead>
                  <TBody>
                    {res.data.slice(0, 10).map((r, i) => (
                      <TR key={i}>
                        <TD><div className="text-body">{r.assignee_name}</div><div className="text-xs text-muted-foreground">{r.team_name}</div></TD>
                        <TD className="text-right tnum">{r.open_tasks}</TD>
                        <TD className="text-right tnum">{r.open_work_orders}</TD>
                        <TD className={cn("text-right tnum", r.overdue > 0 && "font-semibold text-critical-text")}>{r.overdue}</TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              )}
            </AsyncState>
          </CardContent>
        </Card>
        <Card className="col-span-3">
          <CardHeader><CardTitle>{t("overview.building_state")}</CardTitle></CardHeader>
          <CardContent>
            <AsyncState query={building} skeleton={<PanelSkeleton rows={3} />}>
              {(res) => (
                <ul className="space-y-3">
                  {res.data.map((b) => (
                    <li key={b.location_id}>
                      <div className="flex items-center justify-between text-body"><span className={cn(b.location_type === "tower" && "pl-3")}>{b.name}</span><span className="text-xs text-muted-foreground">{b.location_type}</span></div>
                      <div className="mt-1 flex gap-2 text-xs">
                        <Pill label="Open" value={b.open} tone="info" />
                        <Pill label="Overdue" value={b.overdue} tone="critical" />
                        <Pill label="SLA Risk" value={b.sla_risk} tone="warning" />
                        {b.incidents > 0 && <Pill label="Incident" value={b.incidents} tone="critical" />}
                      </div>
                    </li>
                  ))}
                  {res.data.length === 0 && <li className="text-sm text-muted-foreground">Belum ada building/tower.</li>}
                </ul>
              )}
            </AsyncState>
          </CardContent>
        </Card>
      </div>
      {assign && <AssignDialog objectType={assign.object_type as "task" | "work_order" | "service_request" | "incident"} id={assign.object_id} open onOpenChange={(o) => !o && setAssign(null)} />}
    </div>
  );
}

function Stat({ label, value, tone }: { label: string; value: number; tone?: "info" | "warning" | "critical" }) {
  return (
    <div className="rounded-md bg-muted px-2 py-2">
      <div className={cn("text-h2 font-bold tnum", tone === "warning" && value > 0 && "text-warning-text", tone === "critical" && value > 0 && "text-critical-text")}>{fmtNumber(value)}</div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  );
}
function Pill({ label, value, tone }: { label: string; value: number; tone: "info" | "warning" | "critical" }) {
  const cls = value === 0 ? "bg-neutral-soft text-neutral-text" : tone === "info" ? "bg-info-soft text-info-text" : tone === "warning" ? "bg-warning-soft text-warning-text" : "bg-critical-soft text-critical-text";
  return <span className={cn("rounded-full px-2 py-0.5 tnum", cls)}>{value} {label}</span>;
}
function PanelSkeleton({ rows }: { rows: number }) {
  return (
    <div className="space-y-2" aria-busy>
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} className="h-10 w-full" />
      ))}
    </div>
  );
}
