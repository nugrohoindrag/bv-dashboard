// Cleaning Route (PRD P2 v2.1 §7.3 P2-RTE-01..03, P2-SHF-04; NC §76.4): urutan area (stop) per staf/shift dengan estimasi
// waktu → satu Cleaning Route Run per hari aktif berisi cleaning task berurutan (jadwal berantai). Progres run x dari y area.
// Deep link: /housekeeping/routes?run={id} membuka detail run (notifikasi & Housekeeping Dashboard).
import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Card, Checkbox, ConfirmDialog, DatePicker, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Tabs, TabsContent, TabsList, TabsTrigger, Textarea } from "@/components/ui/primitives";
import { StatusBadge } from "@/components/bv/badges";
import { DataGrid } from "@/components/bv/datagrid";
import { CellText, CellTitle } from "@/components/bv/cells";
import { AsyncState, DetailSkeleton, KeyValue, useToast } from "@/components/bv/common";
import { EmptyState, QueryErrorState, CardSkeleton } from "@/components/bv/states";
import { LocationPicker, TeamPicker, WeekdayPicker } from "@/components/bv/pickers";
import { useInvalidate, useTemplates } from "@/api/hooks";
import { api, type ListResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDate, fmtTime } from "@/lib/format";
import { fieldErrorsOf, problemOf } from "@/lib/problem";
import { ALL_WEEKDAYS, normalizeWeekdays, weekdaysText } from "@/lib/weekdays";
import { statusOptions } from "@/lib/status";
import { cn } from "@/lib/utils";
import type { CleaningRoute, CleaningRouteRun } from "@/api/types";
import { PropertyField } from "@/features/security/shared";
import { usePropertyChoice } from "@/features/security/hooks";
import { ShiftSelect, StaffPicker } from "@/features/workforce/components";
import { fmtHours, isoDay } from "@/features/workforce/workforce";
import { CLEANING_TYPES, DEFAULT_STOP_MINUTES, cleaningTypeLabel, moveItem, stopTimings } from "./routes";
import { clearableId } from "@/lib/clearable";

export default function CleaningRoutesPage() {
  const { can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const runId = sp.get("run");
  const tab = sp.get("tab") === "runs" ? "runs" : "routes";
  const setParam = (k: string, v: string | null) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n, { replace: true }); };
  const [edit, setEdit] = useState<CleaningRoute | "new" | null>(null);
  return (
    <div>
      <PageHeader
        title="Cleaning Route"
        subtitle="Urutan area per staf/shift dengan estimasi waktu; setiap hari aktif menghasilkan run berisi cleaning task berurutan."
        actions={can("housekeeping.cleaning_routes.create") && <Button icon="add" onClick={() => setEdit("new")}>Buat Route</Button>}
      />
      <Tabs value={tab} onValueChange={(v) => setParam("tab", v === "routes" ? null : v)}>
        <TabsList>
          <TabsTrigger value="routes" icon="route">Route</TabsTrigger>
          <TabsTrigger value="runs" icon="calendar_today">Run harian</TabsTrigger>
        </TabsList>
        <TabsContent value="routes"><RoutesList onOpen={setEdit} onOpenRun={(id) => setParam("run", id)} /></TabsContent>
        <TabsContent value="runs"><RunsList onOpenRun={(id) => setParam("run", id)} /></TabsContent>
      </Tabs>
      {edit && <RouteEditor route={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
      {runId && <RunDrawer id={runId} onClose={() => setParam("run", null)} />}
    </div>
  );
}

function RoutesList({ onOpen, onOpenRun }: { onOpen: (r: CleaningRoute) => void; onOpenRun: (id: string) => void }) {
  const { propertyId } = useAuth();
  const list = useQuery({
    queryKey: ["cleaning-routes", propertyId ?? null],
    queryFn: ({ signal }) => api<ListResponse<CleaningRoute>>("cleaning-routes", { query: { property_id: propertyId ?? undefined }, signal }).then((r) => r.data),
  });
  // today_run dari GET /cleaning-routes; daftar run hari ini sebagai cadangan
  const todayRuns = useQuery({
    queryKey: ["cleaning-route-runs", propertyId ?? null, "today"],
    queryFn: ({ signal }) => api<ListResponse<CleaningRouteRun>>("cleaning-route-runs", { query: { property_id: propertyId ?? undefined }, signal }).then((r) => r.data),
    refetchInterval: 60_000,
  });
  const runOf = useMemo(() => {
    const m = new Map<string, CleaningRouteRun>();
    for (const r of todayRuns.data ?? []) m.set(r.route_id, r);
    return (route: CleaningRoute) => route.today_run ?? m.get(route.id) ?? null;
  }, [todayRuns.data]);
  const columns = useMemo<ColumnDef<CleaningRoute, unknown>[]>(
    () => [
      // Pola tabel Tasks (29 Sep 2026): kode + nama satu baris, jadwal satu baris (shift di tooltip); tipe cleaning & wajib foto
      // ada di form route (klik baris).
      { id: "name", header: "Route", meta: { mobile: "primary" }, cell: ({ row: { original: r } }) => <CellTitle code={r.route_code} title={r.name} /> },
      { id: "schedule", header: "Jadwal", meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <CellText max={200} title={[r.start_time, r.shift_name, weekdaysText(r.weekdays)].filter(Boolean).join(" · ")} className="tnum">{r.start_time} · {weekdaysText(r.weekdays)}</CellText> },
      { id: "stops", header: "Area", meta: { mobile: "secondary", nowrap: true }, size: 150, cell: ({ row: { original: r } }) => <span className="tnum text-sm">{r.stops.length} area · {fmtHours(r.total_minutes)}</span> },
      { id: "assignee", header: "Petugas", meta: { mobile: "hidden" }, cell: ({ row: { original: r } }) => <CellText max={160} muted={!r.default_assignee_name && !r.responsible_team_name}>{r.default_assignee_name ?? r.responsible_team_name ?? "—"}</CellText> },
      {
        id: "today", header: "Hari ini", meta: { mobile: "secondary" }, size: 170,
        cell: ({ row: { original: r } }) => {
          const today = runOf(r);
          return today ? (
            <button type="button" className="inline-flex items-center gap-1.5 whitespace-nowrap text-left" onClick={(e) => { e.stopPropagation(); onOpenRun(today.id); }} aria-label={`Buka run hari ini ${r.route_code}`}>
              <StatusBadge objectType="cleaning_route_run" status={today.status} />
              <span className="text-xs text-on-surface-variant tnum">{today.completed_stops}/{today.total_stops} area</span>
            </button>
          ) : <span className="text-xs text-on-surface-variant">Tidak ada run</span>;
        },
      },
      { id: "status", header: "Status", meta: { mobile: "status" }, size: 100, cell: ({ row: { original: r } }) => (r.is_active ? <Badge tone="success">Aktif</Badge> : <Badge>Nonaktif</Badge>) },
    ],
    [onOpenRun, runOf],
  );
  return (
    <DataGrid
      columns={columns}
      rows={list.data ?? []}
      rowId={(r) => r.id}
      onRowClick={(r) => { onOpen(r); }}
      loading={list.isLoading}
      error={list.error}
      onRetry={() => list.refetch()}
      empty={{ icon: "route", title: "Belum ada Cleaning Route", description: "Susun urutan area (mis. Toilet Lt. 12 → Pantry → Koridor) untuk satu staf/shift; cleaning task dibuat berurutan setiap hari aktif." }}
    />
  );
}

function RunsList({ onOpenRun }: { onOpenRun: (id: string) => void }) {
  const { propertyId } = useAuth();
  const [date, setDate] = useState(() => isoDay(new Date()));
  const [status, setStatus] = useState("");
  const [mine, setMine] = useState(false);
  const runs = useQuery({
    queryKey: ["cleaning-route-runs", propertyId ?? null, date, status, mine],
    // mine=true: run milik saya / team saya (server mengabaikan property_id scope route untuk mine)
    queryFn: ({ signal }) => api<ListResponse<CleaningRouteRun>>("cleaning-route-runs", { query: { property_id: propertyId ?? undefined, date, status: status || undefined, mine: mine || undefined }, signal }).then((r) => r.data),
    refetchInterval: 60_000,
  });
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <DatePicker className="w-[calc(50%-4px)] sm:w-44" value={date} onChange={(v) => setDate(v || isoDay(new Date()))} aria-label="Tanggal run" />
        <NativeSelect className="w-[calc(50%-4px)] sm:w-48" value={status} onChange={(e) => setStatus(e.target.value)} aria-label="Status run">
          <option value="">Status: Semua</option>
          {statusOptions("cleaning_route_run").map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </NativeSelect>
        <Checkbox label="Milik saya / team saya" checked={mine} onCheckedChange={setMine} />
        <span className="text-sm text-on-surface-variant">Progres run diperbarui saat cleaning task berubah status.</span>
      </div>
      {runs.isLoading ? (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">{[0, 1, 2].map((i) => <CardSkeleton key={i} lines={3} />)}</div>
      ) : runs.error && !runs.data ? (
        <QueryErrorState error={runs.error} onRetry={() => runs.refetch()} />
      ) : (runs.data ?? []).length === 0 ? (
        <EmptyState icon="route" title="Tidak ada run pada tanggal ini" description="Run dibuat otomatis untuk route aktif pada hari yang dipilih (horizon beberapa hari ke depan)." />
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {(runs.data ?? []).map((r) => <RunCard key={r.id} run={r} onOpen={() => onOpenRun(r.id)} />)}
        </div>
      )}
    </div>
  );
}

function RunProgress({ run }: { run: Pick<CleaningRouteRun, "completed_stops" | "total_stops" | "progress_pct"> }) {
  return (
    <div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-surface-container-high" role="progressbar" aria-valuenow={run.progress_pct} aria-valuemin={0} aria-valuemax={100} aria-label="Progres run">
        <div className="h-full rounded-full bg-primary" style={{ width: `${run.progress_pct}%` }} />
      </div>
      <div className="mt-1 text-xs text-on-surface-variant tnum">{run.completed_stops} dari {run.total_stops} area selesai</div>
    </div>
  );
}

function RunCard({ run, onOpen }: { run: CleaningRouteRun; onOpen: () => void }) {
  return (
    <Card className="p-3" interactive>
      <button type="button" onClick={onOpen} className="block w-full text-left">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <div className="font-medium"><span className="font-mono text-[13px]">{run.route_code}</span> · {run.route_name}</div>
            <div className="text-xs text-on-surface-variant">{run.assignee_name ?? run.team_name ?? "Belum ditugaskan"}{run.shift_name ? ` · ${run.shift_name}` : ""}</div>
          </div>
          <StatusBadge objectType="cleaning_route_run" status={run.status} />
        </div>
        <div className="mt-2"><RunProgress run={run} /></div>
        {run.next_stop && <div className="mt-1 text-xs"><span className="text-on-surface-variant">Berikutnya:</span> {run.next_stop.sort_order}. {run.next_stop.location_name ?? run.next_stop.title}{run.next_stop.scheduled_start_at ? ` · ${fmtTime(run.next_stop.scheduled_start_at)}` : ""}</div>}
      </button>
    </Card>
  );
}

function RunDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const q = useQuery({ queryKey: ["cleaning-route-run", id], queryFn: ({ signal }) => api<CleaningRouteRun>(`cleaning-route-runs/${id}`, { signal }), refetchInterval: 60_000 });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Cleaning Route Run" description={q.data ? `${q.data.route_code} · ${q.data.route_name} · ${fmtDate(q.data.run_date)}` : undefined}>
        <AsyncState query={q} skeleton={<DetailSkeleton />}>
          {(run) => (
            <div className="space-y-4" data-testid="route-run-detail">
              <div className="flex flex-wrap items-center gap-2"><StatusBadge objectType="cleaning_route_run" status={run.status} /><span className="text-sm text-on-surface-variant">{cleaningTypeLabel(run.cleaning_type)}</span></div>
              <KeyValue items={[
                { label: "Petugas", value: run.assignee_name ?? "—" },
                { label: "Team", value: run.team_name ?? "—" },
                { label: "Shift", value: run.shift_name ?? "—" },
                { label: "Mulai", value: run.started_at ? fmtTime(run.started_at) : "—" },
                { label: "Selesai", value: run.completed_at ? fmtTime(run.completed_at) : "—" },
              ]} />
              <RunProgress run={run} />
              <ol className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
                {run.stops.map((s) => {
                  const next = run.next_stop?.task_id === s.task_id;
                  return (
                    <li key={s.task_id} className={cn("flex items-start justify-between gap-2 px-3 py-2", next && "bg-primary-soft")}>
                      <div className="min-w-0">
                        <div className="text-sm font-medium">{s.sort_order}. {s.location_name ?? s.title}{next && <span className="ml-1 text-xs font-semibold text-primary">· berikutnya</span>}</div>
                        <Link to={`/operations/tasks/${s.task_id}`} className="font-mono text-xs text-primary hover:underline">{s.task_number}</Link>
                        <span className="text-xs text-on-surface-variant tnum"> · {fmtTime(s.scheduled_start_at)}–{fmtTime(s.due_at)}{s.completed_at ? ` · selesai ${fmtTime(s.completed_at)}` : ""}</span>
                      </div>
                      <StatusBadge objectType="task" status={s.status} />
                    </li>
                  );
                })}
              </ol>
            </div>
          )}
        </AsyncState>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Tutup</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface StopDraft { key: string; location_id: string | null; estimated_minutes: string; checklist_template_id: string; notes: string }
let stopSeq = 0;
const newStop = (s?: Partial<StopDraft>): StopDraft => ({ key: `s${++stopSeq}`, location_id: null, estimated_minutes: String(DEFAULT_STOP_MINUTES), checklist_template_id: "", notes: "", ...s });

function RouteEditor({ route, onClose }: { route: CleaningRoute | null; onClose: () => void }) {
  const { can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const canSave = route ? can("housekeeping.cleaning_routes.update") : can("housekeeping.cleaning_routes.create");
  const [pid, setPid] = usePropertyChoice(route?.property_id);
  const templates = useTemplates({ status: "published", domain: "housekeeping" });
  const [f, setF] = useState({
    name: route?.name ?? "", description: route?.description ?? "", cleaning_type: route?.cleaning_type ?? "routine", priority: (route?.priority ?? "medium") as string,
    shift_id: route?.shift_id ?? "", start_time: route?.start_time ?? "08:00", weekdays: normalizeWeekdays(route?.weekdays ?? ALL_WEEKDAYS),
    responsible_team_id: route?.responsible_team_id ?? null as string | null, default_assignee_user_id: route?.default_assignee_user_id ?? null as string | null,
    checklist_template_id: route?.checklist_template_id ?? "", requires_photo: route?.requires_photo ?? false, is_active: route?.is_active ?? true,
  });
  const [shiftStart, setShiftStart] = useState<string | null>(null);
  const [stops, setStops] = useState<StopDraft[]>(() => (route?.stops.length ? route.stops.map((s) => newStop({ location_id: s.location_id, estimated_minutes: String(s.estimated_minutes), checklist_template_id: s.checklist_template_id ?? "", notes: s.notes ?? "" })) : [newStop()]));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [confirmOff, setConfirmOff] = useState(false);
  const effectiveStart = f.start_time || shiftStart || (route?.shift_id === f.shift_id ? route?.start_time : null) || null;
  const timing = stopTimings(effectiveStart, stops.map((s) => s.estimated_minutes));
  const setStop = (i: number, patch: Partial<StopDraft>) => setStops((list) => list.map((s, j) => (j === i ? { ...s, ...patch } : s)));
  const readOnly = !canSave;

  const submit = async () => {
    const e: Record<string, string> = {};
    if (!f.name.trim()) e.name = "Nama route wajib";
    if (!stops.length || stops.some((s) => !s.location_id)) e.stops = "Setiap stop wajib memilih area/lokasi";
    if (!f.weekdays.length) e.weekdays = "Pilih minimal satu hari";
    if (!f.shift_id && !f.start_time) e.start_time = "Isi jam mulai atau pilih shift";
    if (!route && !pid) e.property_id = "Pilih property";
    setErrors(e);
    if (Object.keys(e).length) return;
    const body: Record<string, unknown> = {
      name: f.name.trim(), description: f.description.trim() || (route ? "" : null), cleaning_type: f.cleaning_type, priority: f.priority,
      shift_id: clearableId(f.shift_id, route?.shift_id), start_time: f.start_time || null, weekdays: f.weekdays, responsible_team_id: clearableId(f.responsible_team_id, route?.responsible_team_id), default_assignee_user_id: clearableId(f.default_assignee_user_id, route?.default_assignee_user_id),
      checklist_template_id: clearableId(f.checklist_template_id, route?.checklist_template_id), requires_photo: f.requires_photo,
      stops: stops.map((s) => ({ location_id: s.location_id, estimated_minutes: Number(s.estimated_minutes) > 0 ? Math.round(Number(s.estimated_minutes)) : DEFAULT_STOP_MINUTES, checklist_template_id: s.checklist_template_id || null, notes: s.notes.trim() || null })),
    };
    setBusy(true);
    try {
      if (route) await api(`cleaning-routes/${route.id}`, { method: "PATCH", body: { ...body, is_active: f.is_active }, ifMatch: route.version });
      else await api("cleaning-routes", { body: { ...body, property_id: pid } });
      invalidate("cleaning-route", "dashboard");
      toast.action(route ? "updated" : "created", `Cleaning Route ${f.name.trim()}`);
      onClose();
    } catch (err) {
      setErrors(fieldErrorsOf(err));
      if (problemOf(err).code === "STALE_VERSION") toast.warning("Route sudah diubah pengguna lain. Tutup lalu buka kembali untuk memuat versi terbaru.");
      else toast.failed(route ? "updated" : "created", err, "Cleaning Route");
    } finally {
      setBusy(false);
    }
  };
  const generate = async () => {
    if (!route) return;
    setBusy(true);
    try {
      const r = await api<{ generated: number }>(`cleaning-routes/${route.id}/generate`, { body: {} });
      invalidate("cleaning-route", "dashboard");
      toast.success(r.generated ? `${r.generated} run dibuat` : "Run hari aktif sudah lengkap");
    } catch (err) {
      toast.failed("created", err, "Run");
    } finally {
      setBusy(false);
    }
  };
  const deactivate = async () => {
    if (!route) return;
    setBusy(true);
    try {
      await api(`cleaning-routes/${route.id}`, { method: "DELETE" });
      invalidate("cleaning-route", "dashboard");
      toast.action("updated", `Cleaning Route ${route.route_code} dinonaktifkan`);
      setConfirmOff(false);
      onClose();
    } catch (err) {
      toast.failed("updated", err, "Cleaning Route");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" maxWidth="720px" title={route ? `${route.route_code} · ${route.name}` : "Buat Cleaning Route"} description={readOnly ? "Mode lihat — Anda tidak memiliki izin mengubah route." : undefined}>
        <div className="space-y-4">
          {!route && <PropertyField value={pid} onChange={(v) => { setPid(v); setF((s) => ({ ...s, shift_id: "", responsible_team_id: null, default_assignee_user_id: null })); setStops([newStop()]); }} />}
          {errors.property_id && <p className="text-sm text-on-error-container">{errors.property_id}</p>}
          <Field label="Nama route" required error={errors.name}><Input value={f.name} disabled={readOnly} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="mis. Route Lantai 12 pagi" /></Field>
          <Field label="Deskripsi"><Textarea rows={2} value={f.description} disabled={readOnly} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Tipe cleaning" error={errors.cleaning_type}><NativeSelect value={f.cleaning_type} disabled={readOnly} onChange={(e) => setF({ ...f, cleaning_type: e.target.value })}>{CLEANING_TYPES.map((c) => <option key={c} value={c}>{cleaningTypeLabel(c)}</option>)}</NativeSelect></Field>
            <Field label="Prioritas" error={errors.priority}><NativeSelect value={f.priority} disabled={readOnly} onChange={(e) => setF({ ...f, priority: e.target.value })}>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{p === "low" ? "Rendah" : p === "medium" ? "Sedang" : p === "high" ? "Tinggi" : "Kritis"}</option>)}</NativeSelect></Field>
            <Field label="Shift Housekeeping" help={route?.shift_id ? "Kosongkan untuk melepas shift (isi jam mulai)." : "Opsional — jam mulai mengikuti shift bila jam dikosongkan."} error={errors.shift_id}>
              <ShiftSelect domain="housekeeping" propertyId={pid} value={f.shift_id} disabled={readOnly} onChange={(v, s) => { setF({ ...f, shift_id: v }); setShiftStart(s?.start_time ?? null); }} />
            </Field>
            <Field label="Jam mulai" required={!f.shift_id} help={f.shift_id ? "Kosongkan untuk mengikuti jam mulai shift." : undefined} error={errors.start_time}>
              <Input type="time" value={f.start_time} disabled={readOnly} onChange={(e) => setF({ ...f, start_time: e.target.value })} placeholder={shiftStart ?? undefined} />
            </Field>
          </div>
          <Field label="Hari aktif" required error={errors.weekdays}><WeekdayPicker value={f.weekdays} disabled={readOnly} onChange={(v) => setF({ ...f, weekdays: v })} /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {can("iam.teams.view") && <Field label="Team"><TeamPicker propertyId={pid} domain="housekeeping" value={f.responsible_team_id} disabled={readOnly} onChange={(v) => setF({ ...f, responsible_team_id: v })} /></Field>}
            <Field label="Petugas default" help="Kosong = run ditugaskan ke team"><StaffPicker domain="housekeeping" propertyId={pid} teamId={f.responsible_team_id} value={f.default_assignee_user_id} disabled={readOnly} onChange={(v) => setF({ ...f, default_assignee_user_id: v })} /></Field>
            <Field label="Checklist default"><NativeSelect value={f.checklist_template_id} disabled={readOnly} onChange={(e) => setF({ ...f, checklist_template_id: e.target.value })}><option value="">Tanpa checklist</option>{(templates.data ?? []).map((tp) => <option key={tp.id} value={tp.id}>{tp.name}</option>)}</NativeSelect></Field>
          </div>
          <div className="flex flex-wrap gap-4">
            <Checkbox label="Wajib foto before/after" checked={f.requires_photo} disabled={readOnly} onCheckedChange={(v) => setF({ ...f, requires_photo: v })} />
            {route && <Checkbox label="Aktif" checked={f.is_active} disabled={readOnly} onCheckedChange={(v) => setF({ ...f, is_active: v })} />}
          </div>

          <section aria-label="Urutan stop">
            <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
              <h3 className="text-h3 font-bold">Urutan area ({stops.length})</h3>
              <span className="text-sm text-on-surface-variant tnum" data-testid="route-total">Total {fmtHours(timing.totalMinutes)}{effectiveStart && timing.end ? ` · ${effectiveStart}–${timing.end}` : ""}</span>
            </div>
            {errors.stops && <Alert variant="critical" className="mb-2">{errors.stops}</Alert>}
            <ol className="space-y-2">
              {stops.map((s, i) => {
                const t = timing.stops[i];
                return (
                  <li key={s.key} className="rounded-[var(--radius-md)] border border-border p-2.5">
                    <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                      <span className="text-sm font-semibold">Stop {i + 1}{t?.start ? <span className="ml-1 font-normal text-on-surface-variant tnum">· {t.start}–{t.end}{t.nextDay ? " (+1 hari)" : ""}</span> : <span className="ml-1 font-normal text-on-surface-variant tnum">· +{t?.offset ?? 0} mnt</span>}</span>
                      {!readOnly && (
                        <span className="flex gap-1">
                          <Button size="icon-sm" variant="ghost" aria-label={`Naikkan stop ${i + 1}`} disabled={i === 0} onClick={() => setStops((l) => moveItem(l, i, -1))}><Icon name="arrow_upward" size={16} /></Button>
                          <Button size="icon-sm" variant="ghost" aria-label={`Turunkan stop ${i + 1}`} disabled={i === stops.length - 1} onClick={() => setStops((l) => moveItem(l, i, 1))}><Icon name="arrow_downward" size={16} /></Button>
                          <Button size="icon-sm" variant="ghost" aria-label={`Hapus stop ${i + 1}`} disabled={stops.length === 1} onClick={() => setStops((l) => l.filter((_, j) => j !== i))}><Icon name="delete" size={16} /></Button>
                        </span>
                      )}
                    </div>
                    <div className="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_120px]">
                      <LocationPicker propertyId={pid} value={s.location_id} disabled={readOnly} onChange={(id) => setStop(i, { location_id: id })} placeholder="Pilih area…" />
                      <Input type="number" min={1} aria-label={`Estimasi menit stop ${i + 1}`} value={s.estimated_minutes} disabled={readOnly} onChange={(e) => setStop(i, { estimated_minutes: e.target.value })} />
                      <NativeSelect aria-label={`Checklist stop ${i + 1}`} value={s.checklist_template_id} disabled={readOnly} onChange={(e) => setStop(i, { checklist_template_id: e.target.value })}>
                        <option value="">Checklist: ikut route</option>
                        {(templates.data ?? []).map((tp) => <option key={tp.id} value={tp.id}>{tp.name}</option>)}
                      </NativeSelect>
                      <Input aria-label={`Catatan stop ${i + 1}`} placeholder="Catatan" value={s.notes} disabled={readOnly} onChange={(e) => setStop(i, { notes: e.target.value })} />
                    </div>
                  </li>
                );
              })}
            </ol>
            {!readOnly && <Button size="sm" variant="secondary" icon="add_location_alt" className="mt-2" onClick={() => setStops((l) => [...l, newStop()])}>Tambah stop</Button>}
          </section>
          {route && <p className="text-xs text-on-surface-variant">Menyimpan perubahan membuat ulang run yang belum dimulai (mulai besok); run hari ini tetap berjalan.</p>}
        </div>
        <DialogFooter className="flex-wrap">
          {route && can("housekeeping.cleaning_routes.delete") && route.is_active && <Button variant="ghost" className="mr-auto" onClick={() => setConfirmOff(true)}>Nonaktifkan</Button>}
          {route && can("housekeeping.cleaning_routes.update") && route.is_active && <Button variant="secondary" icon="play_arrow" loading={busy} onClick={() => void generate()}>Generate run</Button>}
          <Button variant="secondary" onClick={onClose}>{readOnly ? "Tutup" : "Batal"}</Button>
          {!readOnly && <Button loading={busy} onClick={() => void submit()}>Simpan</Button>}
        </DialogFooter>
        <ConfirmDialog open={confirmOff} onOpenChange={setConfirmOff} title={`Nonaktifkan ${route?.route_code ?? "route"}?`} description="Run yang belum dimulai (termasuk hari ini) dibatalkan; riwayat run tetap tersimpan." confirmLabel="Nonaktifkan" destructive loading={busy} onConfirm={() => void deactivate()} />
      </DialogContent>
    </Dialog>
  );
}
