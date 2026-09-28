// Preventive Maintenance (PRD §12): tab Jadwal (maintenance_schedules due/overdue → WO) dan Plan (CRUD, publish, generate).
// PRD P2 v2.1 P2-INS-02: plan dapat menghasilkan Work Order PM (default) atau Inspeksi terjadwal (task inspection) — jadwal
// menampilkan tautan ke output masing-masing.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Tabs, TabsContent, TabsList, TabsTrigger, Textarea } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { PriorityBadge, StatusBadge } from "@/components/bv/badges";
import { CellLocation, CellText, CellTitle } from "@/components/bv/cells";
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
/** PRD P2 v2.1 P2-INS-02: output jadwal plan. */
const OUTPUT_LABEL: Record<string, string> = { work_order: "Work Order Preventive Maintenance", inspection: "Inspeksi terjadwal" };

export default function PreventiveMaintenancePage() {
  const { t } = useTranslation();
  const { scheduleId } = useParams();
  const [tab, setTab] = useState(scheduleId ? "schedules" : "schedules");
  return (
    <div>
      <PageHeader title={t("nav.preventive_maintenance")} subtitle="Jadwal Preventive Maintenance dibuat otomatis dari plan yang dipublikasikan; Work Order dibuat lead_time_days sebelum due." />
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
  const query = useMemo(() => { const q: Record<string, string | undefined> = { ...f.all, property_id: propertyId ?? f.all.property_id ?? undefined }; delete q.cursor; if (!q.status && !q.due_within_days && !q.due_from) q.due_within_days = "30"; return q; }, [f.all, propertyId]);
  const list = useList<MaintenanceSchedule>("maintenance-schedules", query);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [skip, setSkip] = useState<MaintenanceSchedule | null>(null);
  const skipAct = useAction<{ id: string; reason: string }>((i) => `maintenance-schedules/${i.id}/skip`, { body: (i) => ({ reason: i.reason }) });
  const runDue = useAction<void, { created: number }>(() => "maintenance-schedules/run-due", { body: () => ({}) });
  const columns = useMemo<ColumnDef<MaintenanceSchedule, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026, pola halaman Tasks): aset (kode + nama), lokasi = nama terakhir, plan satu baris,
      // satu status. Output (WO/inspeksi) dibuka lewat klik baris / menu opsi; team penanggung jawab ada di form Maintenance Plan.
      { id: "asset", header: t("label.asset"), meta: { mobile: "primary" }, cell: ({ row }) => <Link to={`/assets/${row.original.asset_id}`} className="block hover:underline" onClick={(e) => e.stopPropagation()}><CellTitle code={row.original.asset_code} title={row.original.asset_name} /></Link> },
      { id: "location", header: t("label.location"), meta: { mobile: "secondary" }, cell: ({ row }) => <CellLocation path={row.original.location_path} max={150} /> },
      { id: "plan", header: "Plan", meta: { mobile: "hidden" }, cell: ({ row }) => <CellText max={200} title={`${row.original.plan_code} · ${row.original.plan_name}`}>{row.original.plan_name}</CellText> },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <StatusBadge objectType="maintenance_schedule" status={row.original.status} />, size: 120 },
      { id: "priority", header: t("label.priority"), cell: ({ row }) => <PriorityBadge priority={row.original.priority} />, size: 100 },
      { id: "due", header: t("label.due"), meta: { mobile: "secondary" }, cell: ({ row }) => <span className={cn("tnum text-sm", row.original.status === "overdue" && "font-semibold text-critical-text")}>{fmtDate(row.original.due_date)}</span>, size: 120 },
      { id: "actions", header: "", cell: ({ row }) => <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>{can("engineering.maintenance_schedules.skip") && ["scheduled", "due", "overdue"].includes(row.original.status) && !row.original.work_order_id && !row.original.task_id && <Button size="sm" variant="ghost" onClick={() => setSkip(row.original)}>{t("action.skip")}</Button>}</div>, size: 90 },
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
          extra: can("engineering.maintenance_schedules.skip") ? <Button size="sm" variant="secondary" loading={runDue.isPending} onClick={() => runDue.mutateAsync().then((r) => toast.success(`${r.created} Work Order dibuat dari jadwal due`)).catch(toast.error)}><Icon name="play_arrow" size={16} /> Buat WO jadwal due</Button> : undefined,
        }}
      />
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => (r.output_type === "inspection" && r.task_id ? `/operations/tasks/${r.task_id}` : r.work_order_id ? `/operations/work-orders/${r.work_order_id}` : undefined)} loading={list.isLoading} error={list.error} onRetry={() => list.refetch()} isFiltered={f.isFiltered} empty={{ message: "Belum ada jadwal Preventive Maintenance. Publikasikan Maintenance Plan untuk membuat jadwal." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} rowClassName={(r) => cn(r.status === "overdue" && "border-l-4 border-l-critical", r.id === highlight && "bg-brand-50")} />
      {skip && <ReasonDialog open onOpenChange={(o) => !o && setSkip(null)} title="Lewati jadwal Preventive Maintenance" label="Alasan dilewati" confirmLabel={t("action.skip")} loading={skipAct.isPending} onConfirm={(reason) => skipAct.mutateAsync({ id: skip.id, reason }).then(() => { toast.success("Jadwal dilewati"); setSkip(null); nav("/engineering/preventive-maintenance"); }).catch(toast.error)} />}
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
      // Kode + nama plan digabung; frekuensi satu baris. Lead time, output & team ada di form plan (klik baris).
      { id: "plan", header: "Plan", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.plan_code} title={row.original.name} /> },
      { id: "asset", header: t("label.asset"), meta: { mobile: "secondary" }, cell: ({ row }) => <CellText max={200} title={`${row.original.asset_code} · ${row.original.asset_name}`}><span className="font-mono text-xs">{row.original.asset_code}</span> {row.original.asset_name}</CellText> },
      { id: "frequency", header: "Frekuensi", meta: { nowrap: true }, cell: ({ row }) => <span className="text-sm">{freqLabel[row.original.frequency] ?? row.original.frequency}{row.original.interval_days ? ` (${row.original.interval_days} hari)` : ""}</span>, size: 130 },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <StatusBadge objectType="authoring" status={row.original.status} />, size: 110 },
      { id: "next", header: "Due berikutnya", meta: { nowrap: true, mobile: "secondary" }, cell: ({ row }) => <span className="tnum text-sm">{fmtDate(row.original.next_due)}</span>, size: 120 },
      { id: "count", header: "Jadwal", meta: { nowrap: true }, cell: ({ row }) => <span className="tnum">{row.original.schedule_count}</span>, size: 80 },
    ],
    [t],
  );
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <NativeSelect className="w-40" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option><option value="draft">Draft</option><option value="published">Published</option><option value="archived">Archived</option></NativeSelect>
        <span className="ml-auto">{can("engineering.maintenance_plans.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Buat Plan</Button>}</span>
      </div>
      <DataGrid
        columns={columns}
        rows={plans.data ?? []}
        rowId={(r) => r.id}
        onRowClick={(r) => { setEdit(r); }}
        loading={plans.isLoading} error={plans.error} onRetry={() => plans.refetch()}
        empty={{ message: "Belum ada Maintenance Plan." }}
        rowActions={(r) => [
          ...(r.status === "draft" && can("engineering.maintenance_plans.publish") ? [{ label: t("action.publish"), onSelect: () => setStatusAct.mutateAsync({ id: r.id, action: "publish" }).then(() => toast.success("Plan dipublikasikan & jadwal dibuat")).catch(toast.error) }] : []),
          ...(r.status === "published" && can("engineering.maintenance_plans.publish") ? [{ label: "Generate jadwal", onSelect: () => generate.mutateAsync({ id: r.id }).then((x) => toast.success(`${x.created} jadwal dibuat`)).catch(toast.error) }] : []),
          ...(r.status !== "archived" && can("engineering.maintenance_plans.archive") ? [{ label: t("action.archive"), destructive: true, onSelect: () => setStatusAct.mutateAsync({ id: r.id, action: "archive" }).then(() => toast.action("archived", "Plan")).catch(toast.error) }] : []),
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
  const [form, setForm] = useState({ name: plan?.name ?? "", asset_id: plan?.asset_id ?? null as string | null, frequency: plan?.frequency ?? "monthly", interval_days: plan?.interval_days?.toString() ?? "", start_date: plan?.start_date?.slice(0, 10) ?? new Date().toISOString().slice(0, 10), end_date: plan?.end_date?.slice(0, 10) ?? "", checklist_template_id: plan?.checklist_template_id ?? "", default_priority: plan?.default_priority ?? "medium", responsible_team_id: plan?.responsible_team_id ?? null as string | null, lead_time_days: plan?.lead_time_days?.toString() ?? "3", description: "", publish: false, output_type: plan?.output_type ?? "work_order" });
  const templates = useTemplates({ status: "published", applies_to: "work_order" });
  const create = useCreate<Record<string, unknown>, MaintenancePlan>("maintenance-plans");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("maintenance-plans");
  const publish = useAction<{ id: string }>((i) => `maintenance-plans/${i.id}/publish`, { body: () => ({}) });
  const set = (k: keyof typeof form, v: string | boolean | null) => setForm((s) => ({ ...s, [k]: v }));
  const submit = async () => {
    if (!form.name.trim() || !form.asset_id) return toast.error(new Error("Nama dan aset wajib diisi"));
    // PlanInput server tidak memiliki property_id (property diturunkan dari aset; field JSON tak dikenal → 400)
    const body: Record<string, unknown> = { name: form.name.trim(), asset_id: form.asset_id, frequency: form.frequency, interval_days: form.frequency === "custom_days" ? Number(form.interval_days || 0) : null, start_date: form.start_date, end_date: form.end_date || null, checklist_template_id: form.checklist_template_id || null, default_priority: form.default_priority, responsible_team_id: form.responsible_team_id, lead_time_days: Number(form.lead_time_days || 0), description: form.description || null, output_type: form.output_type };
    try {
      if (plan) await update.mutateAsync({ id: plan.id, version: plan.version, ...body });
      else {
        const p = await create.mutateAsync(body);
        if (form.publish) await publish.mutateAsync({ id: p.id });
      }
      toast.action("saved", "Plan");
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
          {/* PRD P2 v2.1 P2-INS-02: plan menghasilkan WO PM atau inspeksi engineering terjadwal (task inspection) */}
          <Field label="Output jadwal" help={form.output_type === "inspection" ? "Setiap jadwal jatuh tempo membuat task Inspeksi (hasil pass/fail; item Not OK → Finding)." : "Setiap jadwal jatuh tempo membuat Work Order maintenance."}>
            <NativeSelect value={form.output_type} onChange={(e) => set("output_type", e.target.value)} disabled={readOnly}>{Object.entries(OUTPUT_LABEL).map(([v, l]) => <option key={v} value={v}>{l}</option>)}</NativeSelect>
          </Field>
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
