// Preventive Maintenance (PRD §12): tab Jadwal (maintenance_schedules due/overdue → WO) dan Plan (CRUD, publish, generate).
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Plus, Play } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Tabs, TabsContent, TabsList, TabsTrigger, Textarea } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { PriorityBadge, StatusBadge } from "@/components/bv/badges";
import { ReasonDialog, useToast } from "@/components/bv/common";
import { AssetPicker, TeamPicker } from "@/components/bv/pickers";
import { useAction, useAll, useCreate, useList, useTemplates, useUpdate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusMap } from "@/lib/status-map";
import { fmtDate, fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { MaintenancePlan, MaintenanceSchedule } from "@/api/types";

const FREQ = ["daily", "weekly", "biweekly", "monthly", "quarterly", "semiannual", "annual", "custom_days"];
const freqLabel: Record<string, string> = { daily: "Harian", weekly: "Mingguan", biweekly: "2 Mingguan", monthly: "Bulanan", quarterly: "Triwulan", semiannual: "Semesteran", annual: "Tahunan", custom_days: "Interval hari" };

export default function PreventiveMaintenancePage() {
  const { t } = useTranslation();
  const { scheduleId } = useParams();
  const [tab, setTab] = useState(scheduleId ? "schedules" : "schedules");
  return (
    <div>
      <PageHeader title={t("nav.preventive_maintenance")} subtitle="Jadwal PM dibuat otomatis dari plan yang dipublikasikan; Work Order dibuat lead_time_days sebelum due." />
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="schedules">Jadwal</TabsTrigger>
          <TabsTrigger value="plans">Maintenance Plan</TabsTrigger>
        </TabsList>
        <TabsContent value="schedules" className="pt-4"><SchedulesTab highlight={scheduleId} /></TabsContent>
        <TabsContent value="plans" className="pt-4"><PlansTab /></TabsContent>
      </Tabs>
    </div>
  );
}

function SchedulesTab({ highlight }: { highlight?: string }) {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const nav = useNavigate();
  const toast = useToast();
  const f = useUrlFilters();
  const query = useMemo(() => { const q: Record<string, string | undefined> = { ...f.all, property_id: propertyId ?? undefined }; delete q.cursor; if (!q.status && !q.due_within_days && !q.due_from) q.due_within_days = "30"; return q; }, [f.all, propertyId]);
  const list = useList<MaintenanceSchedule>("maintenance-schedules", query);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [skip, setSkip] = useState<MaintenanceSchedule | null>(null);
  const skipAct = useAction<{ id: string; reason: string }>((i) => `maintenance-schedules/${i.id}/skip`, { body: (i) => ({ reason: i.reason }) });
  const runDue = useAction<void, { created: number }>(() => "maintenance-schedules/run-due", { body: () => ({}) });
  const columns = useMemo<ColumnDef<MaintenanceSchedule, unknown>[]>(
    () => [
      { id: "due", header: t("label.due"), cell: ({ row }) => <span className={cn("tnum", row.original.status === "overdue" && "font-semibold text-critical-text")}>{fmtDate(row.original.due_date)}</span>, size: 110 },
      { id: "asset", header: t("label.asset"), cell: ({ row }) => <div><Link to={`/assets/${row.original.asset_id}`} className="hover:underline" onClick={(e) => e.stopPropagation()}><span className="font-mono text-xs">{row.original.asset_code}</span> {row.original.asset_name}</Link><div className="text-xs text-muted-foreground">{row.original.location_path}</div></div> },
      { id: "plan", header: "Plan", cell: ({ row }) => <div><div>{row.original.plan_name}</div><div className="font-mono text-xs text-muted-foreground">{row.original.plan_code}</div></div> },
      { id: "status", header: t("label.status"), cell: ({ row }) => <StatusBadge objectType="maintenance_schedule" status={row.original.status} />, size: 120 },
      { id: "priority", header: t("label.priority"), cell: ({ row }) => <PriorityBadge priority={row.original.priority} />, size: 100 },
      { id: "team", header: t("label.team"), cell: ({ row }) => row.original.team_name ?? "—" },
      { id: "wo", header: "Work Order", cell: ({ row }) => row.original.work_order_id ? <Link to={`/operations/work-orders/${row.original.work_order_id}`} className="font-mono text-xs hover:underline" onClick={(e) => e.stopPropagation()}>{row.original.work_order_number} <StatusBadge objectType="work_order" status={row.original.work_order_status ?? "new"} /></Link> : <span className="text-xs text-muted-foreground">—</span> },
      { id: "actions", header: "", cell: ({ row }) => <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>{can("engineering.maintenance_schedules.skip") && ["scheduled", "due", "overdue"].includes(row.original.status) && !row.original.work_order_id && <Button size="sm" variant="ghost" onClick={() => setSkip(row.original)}>{t("action.skip")}</Button>}</div>, size: 90 },
    ],
    [t, can],
  );
  return (
    <div className="space-y-3">
      <FilterBar
        spec={{
          status: Object.entries(statusMap.maintenance_schedule).map(([value, d]) => ({ value, label: d.label_id })),
          presets: [
            { key: "7", label: "7 hari", params: { due_within_days: "7" } },
            { key: "30", label: "30 hari", params: { due_within_days: "30" } },
            { key: "overdue", label: t("label.overdue"), params: { status: "overdue" } },
          ],
          extra: can("engineering.maintenance_schedules.skip") ? <Button size="sm" variant="secondary" loading={runDue.isPending} onClick={() => runDue.mutateAsync().then((r) => toast.success(`${r.created} Work Order dibuat dari jadwal due`)).catch(toast.error)}><Play /> Buat WO jadwal due</Button> : undefined,
        }}
      />
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => (r.work_order_id ? `/operations/work-orders/${r.work_order_id}` : undefined)} loading={list.isLoading} isFiltered={f.isFiltered} empty={{ message: "Belum ada jadwal PM. Publikasikan Maintenance Plan untuk membuat jadwal." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} rowClassName={(r) => cn(r.status === "overdue" && "border-l-4 border-l-critical", r.id === highlight && "bg-brand-50")} />
      {skip && <ReasonDialog open onOpenChange={(o) => !o && setSkip(null)} title="Lewati jadwal PM" label="Alasan dilewati" confirmLabel={t("action.skip")} loading={skipAct.isPending} onConfirm={(reason) => skipAct.mutateAsync({ id: skip.id, reason }).then(() => { toast.success("Jadwal dilewati"); setSkip(null); nav("/engineering/preventive-maintenance"); }).catch(toast.error)} />}
    </div>
  );
}

function PlansTab() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const toast = useToast();
  const [status, setStatus] = useState("");
  const plans = useAll<MaintenancePlan>("maintenance-plans", { property_id: propertyId ?? undefined, status: status || undefined });
  const [edit, setEdit] = useState<MaintenancePlan | null | "new">(null);
  const setStatusAct = useAction<{ id: string; action: "publish" | "archive" }>((i) => `maintenance-plans/${i.id}/${i.action}`, { body: () => ({}) });
  const generate = useAction<{ id: string }, { created: number }>((i) => `maintenance-plans/${i.id}/generate`, { body: () => ({}) });
  const columns = useMemo<ColumnDef<MaintenancePlan, unknown>[]>(
    () => [
      { id: "code", header: "Kode", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.plan_code}</span>, size: 130 },
      { id: "name", header: "Plan", cell: ({ row }) => <div><div className="font-medium">{row.original.name}</div><div className="text-xs text-muted-foreground">{freqLabel[row.original.frequency] ?? row.original.frequency}{row.original.interval_days ? ` (${row.original.interval_days} hari)` : ""} · lead {row.original.lead_time_days} hari</div></div> },
      { id: "asset", header: t("label.asset"), cell: ({ row }) => <span><span className="font-mono text-xs">{row.original.asset_code}</span> {row.original.asset_name}</span> },
      { id: "status", header: t("label.status"), cell: ({ row }) => <StatusBadge objectType="authoring" status={row.original.status} />, size: 110 },
      { id: "next", header: "Due berikutnya", cell: ({ row }) => fmtDate(row.original.next_due), size: 120 },
      { id: "count", header: "Jadwal", cell: ({ row }) => <span className="tnum">{row.original.schedule_count}</span>, size: 80 },
      { id: "team", header: t("label.team"), cell: ({ row }) => row.original.responsible_team_name ?? "—" },
    ],
    [t],
  );
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <NativeSelect className="w-40" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option><option value="draft">Draft</option><option value="published">Published</option><option value="archived">Archived</option></NativeSelect>
        <span className="ml-auto">{can("engineering.maintenance_plans.create") && <Button onClick={() => setEdit("new")}><Plus /> Buat Plan</Button>}</span>
      </div>
      <DataGrid
        columns={columns}
        rows={plans.data ?? []}
        rowId={(r) => r.id}
        onRowClick={(r) => { setEdit(r); }}
        loading={plans.isLoading}
        empty={{ message: "Belum ada Maintenance Plan." }}
        rowActions={(r) => [
          ...(r.status === "draft" && can("engineering.maintenance_plans.publish") ? [{ label: t("action.publish"), onSelect: () => setStatusAct.mutateAsync({ id: r.id, action: "publish" }).then(() => toast.success("Plan dipublikasikan & jadwal dibuat")).catch(toast.error) }] : []),
          ...(r.status === "published" && can("engineering.maintenance_plans.publish") ? [{ label: "Generate jadwal", onSelect: () => generate.mutateAsync({ id: r.id }).then((x) => toast.success(`${x.created} jadwal dibuat`)).catch(toast.error) }] : []),
          ...(r.status !== "archived" && can("engineering.maintenance_plans.archive") ? [{ label: t("action.archive"), destructive: true, onSelect: () => setStatusAct.mutateAsync({ id: r.id, action: "archive" }).then(() => toast.success("Plan diarsipkan")).catch(toast.error) }] : []),
        ]}
      />
      {edit && <PlanDialog plan={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

function PlanDialog({ plan, onClose }: { plan: MaintenancePlan | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId, properties } = useAuth();
  const toast = useToast();
  const [pid, setPid] = useState(plan?.property_id ?? propertyId ?? properties[0]?.id ?? "");
  const [form, setForm] = useState({ name: plan?.name ?? "", asset_id: plan?.asset_id ?? null as string | null, frequency: plan?.frequency ?? "monthly", interval_days: plan?.interval_days?.toString() ?? "", start_date: plan?.start_date ?? new Date().toISOString().slice(0, 10), end_date: plan?.end_date ?? "", checklist_template_id: plan?.checklist_template_id ?? "", default_priority: plan?.default_priority ?? "medium", responsible_team_id: plan?.responsible_team_id ?? null as string | null, lead_time_days: plan?.lead_time_days?.toString() ?? "3", description: "", publish: false });
  const templates = useTemplates({ status: "published", applies_to: "work_order" });
  const create = useCreate<Record<string, unknown>, MaintenancePlan>("maintenance-plans");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("maintenance-plans");
  const publish = useAction<{ id: string }>((i) => `maintenance-plans/${i.id}/publish`, { body: () => ({}) });
  const set = (k: keyof typeof form, v: string | boolean | null) => setForm((s) => ({ ...s, [k]: v }));
  const submit = async () => {
    if (!form.name.trim() || !form.asset_id) return toast.error(new Error("Nama dan aset wajib diisi"));
    const body: Record<string, unknown> = { property_id: pid, name: form.name.trim(), asset_id: form.asset_id, frequency: form.frequency, interval_days: form.frequency === "custom_days" ? Number(form.interval_days || 0) : null, start_date: form.start_date, end_date: form.end_date || null, checklist_template_id: form.checklist_template_id || null, default_priority: form.default_priority, responsible_team_id: form.responsible_team_id, lead_time_days: Number(form.lead_time_days || 0), description: form.description || null };
    try {
      if (plan) await update.mutateAsync({ id: plan.id, version: plan.version, ...body });
      else {
        const p = await create.mutateAsync(body);
        if (form.publish) await publish.mutateAsync({ id: p.id });
      }
      toast.success("Plan disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  const readOnly = plan?.status === "archived";
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={plan ? `${plan.plan_code} · ${plan.name}` : "Buat Maintenance Plan"}>
        <div className="space-y-4">
          {!plan && properties.length > 1 && <Field label="Property" required><NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); set("asset_id", null); }}>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>}
          <Field label="Nama plan" required><Input value={form.name} onChange={(e) => set("name", e.target.value)} disabled={readOnly} /></Field>
          <Field label={t("label.asset")} required><AssetPicker propertyId={pid} value={form.asset_id} onChange={(v) => set("asset_id", v)} disabled={!!plan} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Frekuensi" required><NativeSelect value={form.frequency} onChange={(e) => set("frequency", e.target.value)} disabled={readOnly}>{FREQ.map((x) => <option key={x} value={x}>{freqLabel[x]}</option>)}</NativeSelect></Field>
            {form.frequency === "custom_days" && <Field label="Interval (hari)" required><Input type="number" min={1} value={form.interval_days} onChange={(e) => set("interval_days", e.target.value)} /></Field>}
            <Field label="Mulai" required><Input type="date" value={form.start_date} onChange={(e) => set("start_date", e.target.value)} disabled={readOnly} /></Field>
            <Field label="Selesai"><Input type="date" value={form.end_date} onChange={(e) => set("end_date", e.target.value)} disabled={readOnly} /></Field>
            <Field label="Lead time (hari)" help="WO dibuat N hari sebelum due"><Input type="number" min={0} value={form.lead_time_days} onChange={(e) => set("lead_time_days", e.target.value)} disabled={readOnly} /></Field>
            <Field label={t("label.priority")}><NativeSelect value={form.default_priority} onChange={(e) => set("default_priority", e.target.value)} disabled={readOnly}>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
          </div>
          <Field label={t("label.checklist")}><NativeSelect value={form.checklist_template_id} onChange={(e) => set("checklist_template_id", e.target.value)} disabled={readOnly}><option value="">Tanpa checklist</option>{(templates.data ?? []).map((tp) => <option key={tp.id} value={tp.id}>{tp.name}</option>)}</NativeSelect></Field>
          <Field label="Team penanggung jawab"><TeamPicker propertyId={pid} domain="engineering" value={form.responsible_team_id} onChange={(v) => set("responsible_team_id", v)} disabled={readOnly} /></Field>
          {!plan && <Field label={t("label.description")}><Textarea rows={2} value={form.description} onChange={(e) => set("description", e.target.value)} /></Field>}
          {!plan && <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.publish} onCheckedChange={(v) => set("publish", !!v)} /> Publikasikan langsung (jadwal dibuat otomatis)</label>}
          {plan && <p className="text-xs text-muted-foreground">Status: {plan.status} · Jadwal: {plan.schedule_count} · Due berikutnya: {fmtDateTime(plan.next_due)}</p>}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>
          {!readOnly && <Button loading={create.isPending || update.isPending || publish.isPending} onClick={submit}>{t("action.save")}</Button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
