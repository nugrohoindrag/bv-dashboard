// Patrol (PRD §16): tab Patrol Tasks (hari ini/riwayat, checkpoint scan status), Rute, Checkpoint, Jadwal.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Tabs, TabsContent, TabsList, TabsTrigger, Textarea } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { FlagBadges, PriorityBadge, StatusBadge } from "@/components/bv/badges";
import { AsyncState, ReasonDialog, useToast } from "@/components/bv/common";
import { LocationPicker, TeamPicker, UserPicker } from "@/components/bv/pickers";
import { useAction, useAll, useCreate, useList, useTemplates, useUpdate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { statusMap } from "@/lib/status-map";
import { fmtDateTime, fmtTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Checkpoint, CheckpointScan, PatrolRoute, PatrolSchedule, WorkItem } from "@/api/types";
import { AssignDialog, TransitionActions } from "@/features/operations/dialogs";

const WEEKDAYS = ["Sen", "Sel", "Rab", "Kam", "Jum", "Sab", "Min"];

export default function PatrolPage() {
  const { t } = useTranslation();
  const { tab = "tasks" } = useParams();
  const nav = useNavigate();
  return (
    <div>
      <PageHeader title={t("nav.patrol")} subtitle="Patrol Task dibuat otomatis dari jadwal; checkpoint di-scan dari mobile (QR) — web memantau & menugaskan." />
      <Tabs value={tab} onValueChange={(v) => nav(`/security/patrol/${v}`)}>
        <TabsList>
          <TabsTrigger value="tasks">Patrol Tasks</TabsTrigger>
          <TabsTrigger value="routes">Rute</TabsTrigger>
          <TabsTrigger value="checkpoints">Checkpoint</TabsTrigger>
          <TabsTrigger value="schedules">Jadwal</TabsTrigger>
        </TabsList>
        <TabsContent value="tasks" className="pt-4"><PatrolTasksTab /></TabsContent>
        <TabsContent value="routes" className="pt-4"><RoutesTab /></TabsContent>
        <TabsContent value="checkpoints" className="pt-4"><CheckpointsTab /></TabsContent>
        <TabsContent value="schedules" className="pt-4"><SchedulesTab /></TabsContent>
      </Tabs>
    </div>
  );
}

function PatrolTasksTab() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const toast = useToast();
  const f = useUrlFilters();
  const query = useMemo(() => { const q: Record<string, string | undefined> = { ...f.all, property_id: propertyId ?? undefined }; delete q.cursor; return q; }, [f.all, propertyId]);
  const list = useList<WorkItem>("patrol-tasks", query);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [assign, setAssign] = useState<WorkItem | null>(null);
  const [selected, setSelected] = useState<WorkItem | null>(null);
  const generate = useAction<void, { generated: number }>(() => "patrol-schedules/generate", { body: () => ({}) });
  const columns = useMemo<ColumnDef<WorkItem, unknown>[]>(
    () => [
      { id: "number", header: "ID", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.number}</span>, size: 150 },
      { id: "title", header: "Rute / Judul", cell: ({ row }) => <div className="truncate font-medium">{row.original.title}</div> },
      { id: "sched", header: "Jadwal", cell: ({ row }) => <span className="tnum">{fmtDateTime(row.original.scheduled_start_at)}</span>, size: 150 },
      { id: "status", header: t("label.status"), cell: ({ row }) => <div className="flex flex-wrap gap-1"><StatusBadge objectType="task" status={row.original.status} /><FlagBadges flags={row.original.flags} /></div> },
      { id: "cp", header: "Checkpoint", cell: ({ row }) => { const ex = row.original.extension as { checkpoints_total?: number; checkpoints_scanned?: number; checkpoints_missed?: number } | undefined; return ex ? <span className="tnum">{ex.checkpoints_scanned ?? 0}/{ex.checkpoints_total ?? 0}{(ex.checkpoints_missed ?? 0) > 0 && <span className="text-critical-text"> · {ex.checkpoints_missed} missed</span>}</span> : "—"; }, size: 130 },
      { id: "assignee", header: t("label.assignee"), cell: ({ row }) => row.original.assignee.user_name ?? row.original.assignee.team_name ?? <em className="text-muted-foreground">belum ditugaskan</em> },
      { id: "actions", header: "", cell: ({ row }) => <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}><Button size="sm" variant="ghost" onClick={() => setSelected(row.original)}>Checkpoint</Button><TransitionActions objectType="task" item={row.original} compact onAssign={() => setAssign(row.original)} /></div> },
    ],
    [t],
  );
  return (
    <div className="space-y-3">
      <FilterBar
        spec={{
          status: Object.entries(statusMap.task).map(([value, d]) => ({ value, label: d.label_id })),
          assignee: true,
          team: true,
          dateRange: true,
          presets: [
            { key: "today", label: t("label.today"), params: { scheduled_on: new Date().toISOString().slice(0, 10) + "T00:00:00Z" } },
            { key: "open", label: "Open", params: { open: "true" } },
            { key: "overdue", label: t("label.overdue"), params: { overdue: "true" } },
          ],
          extra: can("security.patrol.manage") ? <Button size="sm" variant="secondary" loading={generate.isPending} onClick={() => generate.mutateAsync().then((r) => toast.success(`${r.generated} patrol task dibuat dari jadwal`)).catch(toast.error)}><Icon name="play_arrow" size={16} /> Generate dari jadwal</Button> : undefined,
        }}
      />
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/operations/tasks/${r.id}`} loading={list.isLoading} isFiltered={f.isFiltered} empty={{ message: "Belum ada Patrol Task. Buat jadwal patrol lalu generate." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} rowClassName={(r) => (r.is_overdue ? "border-l-4 border-l-critical" : undefined)} />
      {assign && <AssignDialog objectType="task" id={assign.id} open onOpenChange={(o) => !o && setAssign(null)} current={assign.assignee} domain="security" />}
      {selected && <CheckpointScansDialog task={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}

function CheckpointScansDialog({ task, onClose }: { task: WorkItem; onClose: () => void }) {
  const { can } = useAuth();
  const toast = useToast();
  const scans = useQuery({ queryKey: ["patrol-scans", task.id], queryFn: () => api<{ data: CheckpointScan[] }>(`patrol-tasks/${task.id}/scans`).then((r) => r.data) });
  const missed = useAction<{ cpId: string; reason: string }>((i) => `patrol-tasks/${task.id}/checkpoints/${i.cpId}/missed`, { body: (i) => ({ reason: i.reason }), invalidate: ["patrol-scans", "list", "one"] });
  const manual = useAction<{ cpId: string }>(() => `patrol-tasks/${task.id}/scans`, { body: (i) => ({ checkpoint_id: i.cpId, scan_method: "manual", gps_status: "unavailable", note: "Ditandai manual dari web" }), invalidate: ["patrol-scans", "list", "one"] });
  const [missOf, setMissOf] = useState<CheckpointScan | null>(null);
  const active = ["assigned", "in_progress", "scheduled", "new"].includes(task.status);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={`Checkpoint · ${task.number}`} description={task.title}>
        <AsyncState query={scans}>
          {(items) => (
            <ol className="divide-y divide-border rounded-lg border border-border">
              {items.map((s) => (
                <li key={s.id} className="flex items-center justify-between gap-3 px-3 py-2.5">
                  <div className="min-w-0">
                    <div className="text-body font-medium">{s.sort_order}. {s.checkpoint_name}</div>
                    <div className="truncate text-xs text-muted-foreground">{s.location_path}{s.scanned_at ? ` · ${fmtDateTime(s.scanned_at)} · ${s.scan_method}${s.gps_status ? ` · GPS ${s.gps_status}` : ""}` : ""}{s.missed_reason ? ` · ${s.missed_reason}` : ""}</div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <StatusBadge objectType="checkpoint" status={s.status} />
                    {s.status === "pending" && active && can("security.patrol.manage") && (
                      <>
                        <Button size="sm" variant="ghost" onClick={() => manual.mutateAsync({ cpId: s.checkpoint_id }).catch(toast.error)}>Manual</Button>
                        <Button size="sm" variant="ghost" onClick={() => setMissOf(s)}>Missed</Button>
                      </>
                    )}
                  </div>
                </li>
              ))}
              {items.length === 0 && <li className="px-3 py-6 text-center text-sm text-muted-foreground">Rute tanpa checkpoint.</li>}
            </ol>
          )}
        </AsyncState>
        <DialogFooter><Link to={`/operations/tasks/${task.id}`} className="mr-auto text-sm text-brand-600 hover:underline">Buka detail task →</Link><Button variant="secondary" onClick={onClose}>Tutup</Button></DialogFooter>
        {missOf && <ReasonDialog open onOpenChange={(o) => !o && setMissOf(null)} title={`Tandai missed: ${missOf.checkpoint_name}`} label="Alasan" confirmLabel="Tandai missed" destructive loading={missed.isPending} onConfirm={(reason) => missed.mutateAsync({ cpId: missOf.checkpoint_id, reason }).then(() => setMissOf(null)).catch(toast.error)} />}
      </DialogContent>
    </Dialog>
  );
}

function CheckpointsTab() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const list = useAll<Checkpoint>("checkpoints", { property_id: propertyId ?? undefined });
  const [edit, setEdit] = useState<Checkpoint | null | "new">(null);
  const columns = useMemo<ColumnDef<Checkpoint, unknown>[]>(
    () => [
      { id: "name", header: "Checkpoint", cell: ({ row }) => <span className="font-medium">{row.original.name}</span> },
      { id: "loc", header: t("label.location"), cell: ({ row }) => <span className="text-muted-foreground">{row.original.location_path}</span> },
      { id: "qr", header: "QR", cell: ({ row }) => <span className="font-mono text-xs">{row.original.qr_code ?? "—"}</span>, size: 200 },
      { id: "active", header: "Aktif", cell: ({ row }) => <span className={cn("text-xs", row.original.is_active ? "text-success-text" : "text-muted-foreground")}>{row.original.is_active ? "Aktif" : "Nonaktif"}</span>, size: 80 },
    ],
    [t],
  );
  return (
    <div className="space-y-3">
      <div className="flex justify-end">{can("security.checkpoints.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Tambah Checkpoint</Button>}</div>
      <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} onRowClick={(r) => { if (can("security.checkpoints.update")) setEdit(r); }} loading={list.isLoading} empty={{ message: "Belum ada checkpoint." }} />
      {edit && <CheckpointDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

function CheckpointDialog({ item, onClose }: { item: Checkpoint | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId, properties } = useAuth();
  const toast = useToast();
  const [pid, setPid] = useState(item?.property_id ?? propertyId ?? properties[0]?.id ?? "");
  const [form, setForm] = useState({ name: item?.name ?? "", location_id: item?.location_id ?? null as string | null, instructions: item?.instructions ?? "", is_active: item?.is_active ?? true });
  const create = useCreate<Record<string, unknown>>("checkpoints");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("checkpoints");
  const submit = async () => {
    if (!form.name.trim() || !form.location_id) return toast.error(new Error("Nama dan lokasi wajib"));
    try {
      if (item) await update.mutateAsync({ id: item.id, version: 0, name: form.name, location_id: form.location_id, instructions: form.instructions || null, is_active: form.is_active });
      else await create.mutateAsync({ property_id: pid, name: form.name, location_id: form.location_id, instructions: form.instructions || null, is_active: form.is_active });
      toast.success("Checkpoint disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={item ? "Edit Checkpoint" : "Tambah Checkpoint"}>
        <div className="space-y-4">
          {!item && properties.length > 1 && <Field label="Property" required><NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); setForm({ ...form, location_id: null }); }}>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>}
          <Field label="Nama" required><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></Field>
          <Field label={t("label.location")} required><LocationPicker propertyId={pid} value={form.location_id} onChange={(id) => setForm({ ...form, location_id: id })} /></Field>
          <Field label="Instruksi"><Textarea rows={2} value={form.instructions} onChange={(e) => setForm({ ...form, instructions: e.target.value })} /></Field>
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.is_active} onCheckedChange={(v) => setForm({ ...form, is_active: !!v })} /> Aktif</label>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RoutesTab() {
  const { propertyId, can } = useAuth();
  const list = useAll<PatrolRoute>("patrol-routes", { property_id: propertyId ?? undefined });
  const [edit, setEdit] = useState<PatrolRoute | null | "new">(null);
  const columns = useMemo<ColumnDef<PatrolRoute, unknown>[]>(
    () => [
      { id: "name", header: "Rute", cell: ({ row }) => <div><div className="font-medium">{row.original.name}</div><div className="text-xs text-muted-foreground">{row.original.description}</div></div> },
      { id: "cp", header: "Checkpoint", cell: ({ row }) => <span className="tnum">{row.original.checkpoints.length}</span>, size: 100 },
      { id: "est", header: "Estimasi", cell: ({ row }) => row.original.estimated_minutes ? `${row.original.estimated_minutes} menit` : "—", size: 100 },
      { id: "active", header: "Aktif", cell: ({ row }) => <span className={cn("text-xs", row.original.is_active ? "text-success-text" : "text-muted-foreground")}>{row.original.is_active ? "Aktif" : "Nonaktif"}</span>, size: 80 },
    ],
    [],
  );
  return (
    <div className="space-y-3">
      <div className="flex justify-end">{can("security.patrol_routes.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Buat Rute</Button>}</div>
      <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} onRowClick={(r) => { setEdit(r); }} loading={list.isLoading} empty={{ message: "Belum ada rute patrol." }} />
      {edit && <RouteDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

function RouteDialog({ item, onClose }: { item: PatrolRoute | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId, properties, can } = useAuth();
  const toast = useToast();
  const [pid, setPid] = useState(item?.property_id ?? propertyId ?? properties[0]?.id ?? "");
  const checkpoints = useAll<Checkpoint>("checkpoints", { property_id: pid || undefined }, { enabled: !!pid });
  const templates = useTemplates({ status: "published" });
  const [form, setForm] = useState({ name: item?.name ?? "", description: item?.description ?? "", estimated_minutes: item?.estimated_minutes?.toString() ?? "", checklist_template_id: item?.checklist_template_id ?? "", is_active: item?.is_active ?? true, cps: item?.checkpoints.map((c) => c.id) ?? ([] as string[]) });
  const create = useCreate<Record<string, unknown>>("patrol-routes");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("patrol-routes");
  const adhoc = useCreate<Record<string, unknown>, WorkItem>(`patrol-routes/${item?.id}/patrol-tasks`);
  const toggle = (id: string) => setForm((f) => ({ ...f, cps: f.cps.includes(id) ? f.cps.filter((x) => x !== id) : [...f.cps, id] }));
  const move = (id: string, dir: -1 | 1) => setForm((f) => { const i = f.cps.indexOf(id); const j = i + dir; if (i < 0 || j < 0 || j >= f.cps.length) return f; const next = [...f.cps]; [next[i], next[j]] = [next[j], next[i]]; return { ...f, cps: next }; });
  const submit = async () => {
    if (!form.name.trim() || form.cps.length === 0) return toast.error(new Error("Nama dan minimal 1 checkpoint wajib"));
    const body = { property_id: pid, name: form.name.trim(), description: form.description || null, estimated_minutes: form.estimated_minutes ? Number(form.estimated_minutes) : null, checklist_template_id: form.checklist_template_id || null, is_active: form.is_active, checkpoints: form.cps.map((id, i) => ({ checkpoint_id: id, sort_order: i + 1 })) };
    try {
      if (item) await update.mutateAsync({ id: item.id, version: 0, ...body });
      else await create.mutateAsync(body);
      toast.success("Rute disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  const byId = new Map((checkpoints.data ?? []).map((c) => [c.id, c]));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Rute · ${item.name}` : "Buat Rute Patrol"}>
        <div className="space-y-4">
          {!item && properties.length > 1 && <Field label="Property" required><NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); setForm({ ...form, cps: [] }); }}>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>}
          <Field label="Nama rute" required><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></Field>
          <Field label={t("label.description")}><Textarea rows={2} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Estimasi (menit)"><Input type="number" value={form.estimated_minutes} onChange={(e) => setForm({ ...form, estimated_minutes: e.target.value })} /></Field>
            <Field label={t("label.checklist")}><NativeSelect value={form.checklist_template_id} onChange={(e) => setForm({ ...form, checklist_template_id: e.target.value })}><option value="">Tanpa checklist</option>{(templates.data ?? []).map((tp) => <option key={tp.id} value={tp.id}>{tp.name}</option>)}</NativeSelect></Field>
          </div>
          <Field label={`Urutan checkpoint (${form.cps.length})`}>
            <ol className="mb-2 divide-y divide-border rounded-md border border-border">
              {form.cps.map((id, i) => (
                <li key={id} className="flex items-center justify-between px-3 py-1.5 text-sm"><span>{i + 1}. {byId.get(id)?.name ?? id}</span><span className="flex gap-1"><Button size="icon-sm" variant="ghost" onClick={() => move(id, -1)} aria-label="Naik">↑</Button><Button size="icon-sm" variant="ghost" onClick={() => move(id, 1)} aria-label="Turun">↓</Button><Button size="icon-sm" variant="ghost" onClick={() => toggle(id)} aria-label="Hapus">×</Button></span></li>
              ))}
              {form.cps.length === 0 && <li className="px-3 py-3 text-center text-xs text-muted-foreground">Pilih checkpoint di bawah.</li>}
            </ol>
            <div className="max-h-40 space-y-1 overflow-y-auto rounded-md border border-border p-2">
              {(checkpoints.data ?? []).filter((c) => c.is_active && !form.cps.includes(c.id)).map((c) => (
                <label key={c.id} className="flex cursor-pointer items-center gap-2 text-sm"><Checkbox checked={false} onCheckedChange={() => toggle(c.id)} /> {c.name} <span className="text-xs text-muted-foreground">{c.location_path}</span></label>
              ))}
            </div>
          </Field>
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.is_active} onCheckedChange={(v) => setForm({ ...form, is_active: !!v })} /> Aktif</label>
        </div>
        <DialogFooter>
          {item && can("security.patrol.manage") && <Button variant="secondary" className="mr-auto" loading={adhoc.isPending} onClick={() => adhoc.mutateAsync({ title: `Patrol ad-hoc · ${item.name}`, priority: "medium", task_type: "patrol" }).then((x) => toast.success(`${x.number} dibuat`, { to: `/operations/tasks/${x.id}`, label: "Buka" })).catch(toast.error)}>Patrol sekarang (ad-hoc)</Button>}
          <Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>
          <Button loading={create.isPending || update.isPending} onClick={submit}>{t("action.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SchedulesTab() {
  const { propertyId, can } = useAuth();
  const list = useAll<PatrolSchedule>("patrol-schedules", { property_id: propertyId ?? undefined });
  const [edit, setEdit] = useState<PatrolSchedule | null | "new">(null);
  const columns = useMemo<ColumnDef<PatrolSchedule, unknown>[]>(
    () => [
      { id: "name", header: "Jadwal", cell: ({ row }) => <div><div className="font-medium">{row.original.name}</div><div className="text-xs text-muted-foreground">{row.original.route_name}</div></div> },
      { id: "time", header: "Mulai", cell: ({ row }) => <span className="tnum">{row.original.start_time.slice(0, 5)} · {row.original.duration_minutes} mnt</span>, size: 130 },
      { id: "days", header: "Hari", cell: ({ row }) => <span className="text-xs">{row.original.weekdays.map((d) => WEEKDAYS[d - 1] ?? d).join(" ")}</span> },
      { id: "prio", header: "Prioritas", cell: ({ row }) => <PriorityBadge priority={row.original.priority} />, size: 100 },
      { id: "active", header: "Aktif", cell: ({ row }) => <span className={cn("text-xs", row.original.is_active ? "text-success-text" : "text-muted-foreground")}>{row.original.is_active ? "Aktif" : "Nonaktif"}</span>, size: 80 },
    ],
    [],
  );
  return (
    <div className="space-y-3">
      <div className="flex justify-end">{can("security.patrol.manage") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Buat Jadwal</Button>}</div>
      <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} onRowClick={(r) => { if (can("security.patrol.manage")) setEdit(r); }} loading={list.isLoading} empty={{ message: "Belum ada jadwal patrol." }} />
      {edit && <PatrolScheduleDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

export function WeekdayPicker({ value, onChange }: { value: number[]; onChange: (v: number[]) => void }) {
  return (
    <div className="flex gap-1">
      {WEEKDAYS.map((d, i) => {
        const n = i + 1;
        const on = value.includes(n);
        return <button key={n} type="button" onClick={() => onChange(on ? value.filter((x) => x !== n) : [...value, n].sort())} className={cn("h-8 w-10 rounded-md border text-xs", on ? "border-brand-600 bg-brand-600 text-white" : "border-border bg-card text-muted-foreground")}>{d}</button>;
      })}
    </div>
  );
}

function PatrolScheduleDialog({ item, onClose }: { item: PatrolSchedule | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId, properties } = useAuth();
  const toast = useToast();
  const [pid, setPid] = useState(item?.property_id ?? propertyId ?? properties[0]?.id ?? "");
  const routes = useAll<PatrolRoute>("patrol-routes", { property_id: pid || undefined }, { enabled: !!pid });
  const [form, setForm] = useState({ route_id: item?.route_id ?? "", name: item?.name ?? "", start_time: item?.start_time.slice(0, 5) ?? "20:00", duration_minutes: item?.duration_minutes?.toString() ?? "60", weekdays: item?.weekdays ?? [1, 2, 3, 4, 5, 6, 7], responsible_team_id: item?.responsible_team_id ?? null as string | null, default_assignee_user_id: item?.default_assignee_user_id ?? null as string | null, priority: (item?.priority ?? "medium") as string, is_active: item?.is_active ?? true });
  const create = useCreate<Record<string, unknown>>("patrol-schedules");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("patrol-schedules");
  const submit = async () => {
    if (!form.route_id || !form.name.trim() || form.weekdays.length === 0) return toast.error(new Error("Rute, nama, dan hari wajib"));
    const body = { property_id: pid, route_id: form.route_id, name: form.name.trim(), start_time: form.start_time, duration_minutes: Number(form.duration_minutes), weekdays: form.weekdays, responsible_team_id: form.responsible_team_id, default_assignee_user_id: form.default_assignee_user_id, priority: form.priority, is_active: form.is_active };
    try {
      if (item) await update.mutateAsync({ id: item.id, version: 0, ...body });
      else await create.mutateAsync(body);
      toast.success("Jadwal disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Jadwal · ${item.name}` : "Buat Jadwal Patrol"}>
        <div className="space-y-4">
          {!item && properties.length > 1 && <Field label="Property" required><NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); setForm({ ...form, route_id: "" }); }}>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>}
          <Field label="Rute" required><NativeSelect value={form.route_id} onChange={(e) => setForm({ ...form, route_id: e.target.value })} disabled={!!item}><option value="">Pilih rute…</option>{(routes.data ?? []).filter((r) => r.is_active).map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}</NativeSelect></Field>
          <Field label="Nama jadwal" required><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="mis. Patrol malam 20:00" /></Field>
          <div className="grid grid-cols-3 gap-3">
            <Field label="Mulai" required><Input type="time" value={form.start_time} onChange={(e) => setForm({ ...form, start_time: e.target.value })} /></Field>
            <Field label="Durasi (menit)"><Input type="number" value={form.duration_minutes} onChange={(e) => setForm({ ...form, duration_minutes: e.target.value })} /></Field>
            <Field label={t("label.priority")}><NativeSelect value={form.priority} onChange={(e) => setForm({ ...form, priority: e.target.value })}>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
          </div>
          <Field label="Hari" required><WeekdayPicker value={form.weekdays} onChange={(v) => setForm({ ...form, weekdays: v })} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("label.team")}><TeamPicker propertyId={pid} domain="security" value={form.responsible_team_id} onChange={(v) => setForm({ ...form, responsible_team_id: v, default_assignee_user_id: null })} /></Field>
            <Field label="Assignee default"><UserPicker propertyId={pid} teamId={form.responsible_team_id} value={form.default_assignee_user_id} onChange={(v) => setForm({ ...form, default_assignee_user_id: v })} /></Field>
          </div>
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.is_active} onCheckedChange={(v) => setForm({ ...form, is_active: !!v })} /> Aktif</label>
          <p className="text-xs text-muted-foreground">Patrol Task dibuat otomatis oleh worker setiap hari (H-1) untuk jadwal aktif. Jam mengikuti timezone property{item ? "" : ` · ${fmtTime(new Date())}`}.</p>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
