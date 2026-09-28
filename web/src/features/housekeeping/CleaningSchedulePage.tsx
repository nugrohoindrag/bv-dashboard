// Jadwal Cleaning (PRD §17): jadwal berulang per lokasi → cleaning task otomatis; cleaning ad-hoc; inspeksi housekeeping dibuat dari detail cleaning task.
// PRD P2 v2.1 P2-SHF-04: jadwal dapat dikaitkan ke Housekeeping Shift (jam mulai default = jam mulai shift).
// Hari = Go time.Weekday (0 = Minggu … 6 = Sabtu).
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { PriorityBadge } from "@/components/bv/badges";
import { CellLocation, CellText, CellTitle } from "@/components/bv/cells";
import { useToast } from "@/components/bv/common";
import { LocationPicker, TeamPicker, UserPicker, WeekdayPicker } from "@/components/bv/pickers";
import { useAction, useAll, useCreate, useTemplates, useUpdate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import type { CleaningSchedule, WorkItem } from "@/api/types";
import { WORKDAYS, normalizeWeekdays, weekdaysText } from "@/lib/weekdays";
import { ShiftSelect } from "@/features/workforce/components";
import { CLEANING_TYPES, cleaningTypeLabel } from "./routes";
import { clearableId } from "@/lib/clearable";

export default function CleaningSchedulePage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const toast = useToast();
  const list = useAll<CleaningSchedule>("cleaning-schedules", { property_id: propertyId ?? undefined });
  const [edit, setEdit] = useState<CleaningSchedule | null | "new">(null);
  const [adhoc, setAdhoc] = useState(false);
  const generate = useAction<void, { generated: number }>(() => "cleaning-schedules/generate", { body: () => ({}) });
  const columns = useMemo<ColumnDef<CleaningSchedule, unknown>[]>(
    () => [
      // Pola tabel Tasks (29 Sep 2026): nama satu baris, lokasi = nama terakhir (path di tooltip), jam satu baris (shift di
      // tooltip). Tipe cleaning & wajib foto ada di form jadwal (klik baris).
      { id: "name", header: "Jadwal", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle title={row.original.name} /> },
      { id: "loc", header: t("label.location"), meta: { mobile: "secondary" }, cell: ({ row }) => <CellLocation path={row.original.location_path} max={150} /> },
      { id: "time", header: "Mulai", meta: { nowrap: true, mobile: "secondary" }, cell: ({ row }) => <span className="tnum text-sm" title={row.original.shift_name ?? undefined}>{row.original.start_time.slice(0, 5)} · {row.original.duration_minutes} mnt</span>, size: 130 },
      { id: "days", header: "Hari", cell: ({ row }) => <CellText max={180}>{weekdaysText(row.original.weekdays)}</CellText> },
      { id: "prio", header: t("label.priority"), meta: { nowrap: true }, cell: ({ row }) => <PriorityBadge priority={row.original.priority} />, size: 100 },
      { id: "active", header: "Aktif", meta: { nowrap: true, mobile: "status" }, cell: ({ row }) => <span className={cn("text-xs", row.original.is_active ? "text-success-text" : "text-muted-foreground")}>{row.original.is_active ? "Aktif" : "Nonaktif"}</span>, size: 80 },
    ],
    [t],
  );
  return (
    <div>
      <PageHeader
        title={`${t("nav.housekeeping")} · ${t("nav.schedule")}`}
        subtitle="Cleaning task dibuat otomatis (H-1) dari jadwal aktif; jam mengikuti timezone property."
        actions={
          <>
            {can("housekeeping.cleaning.manage") && <Button variant="secondary" onClick={() => setAdhoc(true)}><Icon name="cleaning_services" size={16} /> Cleaning ad-hoc</Button>}
            {can("housekeeping.cleaning_schedules.update") && <Button variant="secondary" loading={generate.isPending} onClick={() => generate.mutateAsync().then((r) => toast.success(`${r.generated} cleaning task dibuat`)).catch(toast.error)}><Icon name="play_arrow" size={16} /> Generate</Button>}
            {can("housekeeping.cleaning_schedules.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Buat Jadwal</Button>}
          </>
        }
      />
      <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} onRowClick={(r) => { if (can("housekeeping.cleaning_schedules.update")) setEdit(r); }} loading={list.isLoading} error={list.error} onRetry={() => list.refetch()} empty={{ message: "Belum ada jadwal cleaning." }} />
      {edit && <CleaningScheduleDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
      {adhoc && <AdhocCleaningDialog onClose={() => setAdhoc(false)} />}
    </div>
  );
}

function CleaningScheduleDialog({ item, onClose }: { item: CleaningSchedule | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId, properties } = useAuth();
  const toast = useToast();
  const [pid, setPid] = useState(item?.property_id ?? propertyId ?? properties[0]?.id ?? "");
  const templates = useTemplates({ status: "published", domain: "housekeeping" });
  const [form, setForm] = useState({ name: item?.name ?? "", location_id: item?.location_id ?? null as string | null, cleaning_type: item?.cleaning_type ?? "routine", start_time: item?.start_time.slice(0, 5) ?? "07:00", duration_minutes: item?.duration_minutes?.toString() ?? "60", weekdays: normalizeWeekdays(item?.weekdays ?? WORKDAYS), shift_id: item?.shift_id ?? "", checklist_template_id: item?.checklist_template_id ?? "", responsible_team_id: item?.responsible_team_id ?? null as string | null, default_assignee_user_id: item?.default_assignee_user_id ?? null as string | null, priority: (item?.priority ?? "medium") as string, requires_photo: item?.requires_photo ?? true, is_active: item?.is_active ?? true });
  const create = useCreate<Record<string, unknown>>("cleaning-schedules");
  const update = useUpdate<{ id: string; version?: number } & Record<string, unknown>>("cleaning-schedules");
  const submit = async () => {
    if (!form.name.trim() || !form.location_id || form.weekdays.length === 0) return toast.error(new Error("Nama, lokasi, dan hari wajib"));
    if (!form.start_time && !form.shift_id) return toast.error(new Error("Isi jam mulai atau pilih shift"));
    // ScheduleInput server tidak memiliki property_id (property dari lokasi; field JSON tak dikenal → 400)
    const body = { name: form.name.trim(), location_id: form.location_id, cleaning_type: form.cleaning_type, start_time: form.start_time || null, shift_id: clearableId(form.shift_id, item?.shift_id), duration_minutes: Number(form.duration_minutes), weekdays: form.weekdays, checklist_template_id: form.checklist_template_id || null, responsible_team_id: form.responsible_team_id, default_assignee_user_id: form.default_assignee_user_id, priority: form.priority, requires_photo: form.requires_photo, is_active: form.is_active };
    try {
      if (item) await update.mutateAsync({ id: item.id, ...body });
      else await create.mutateAsync(body);
      toast.action("saved", "Jadwal");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Jadwal · ${item.name}` : "Buat Jadwal Cleaning"}>
        <div className="space-y-4">
          {!item && properties.length > 1 && <Field label="Property" required><NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); setForm({ ...form, location_id: null, shift_id: "" }); }}>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>}
          <Field label="Nama jadwal" required><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="mis. Lobby pagi" /></Field>
          <Field label={t("label.location")} required><LocationPicker propertyId={pid} value={form.location_id} onChange={(id) => setForm({ ...form, location_id: id })} /></Field>
          <div className="grid grid-cols-3 gap-3">
            <Field label="Tipe"><NativeSelect value={form.cleaning_type} onChange={(e) => setForm({ ...form, cleaning_type: e.target.value })}>{CLEANING_TYPES.map((x) => <option key={x} value={x}>{cleaningTypeLabel(x)}</option>)}</NativeSelect></Field>
            <Field label="Mulai" required={!form.shift_id} help={form.shift_id ? "Kosong = jam mulai shift" : undefined}><Input type="time" value={form.start_time} onChange={(e) => setForm({ ...form, start_time: e.target.value })} /></Field>
            <Field label="Durasi (menit)"><Input type="number" value={form.duration_minutes} onChange={(e) => setForm({ ...form, duration_minutes: e.target.value })} /></Field>
          </div>
          <Field label="Shift Housekeeping" help={item?.shift_id ? "Kosongkan untuk melepas shift (isi jam mulai)." : "Opsional — jam mulai mengikuti shift bila jam dikosongkan."}><ShiftSelect domain="housekeeping" propertyId={pid} value={form.shift_id} onChange={(v) => setForm({ ...form, shift_id: v })} /></Field>
          <Field label="Hari" required><WeekdayPicker value={form.weekdays} onChange={(v) => setForm({ ...form, weekdays: v })} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("label.checklist")}><NativeSelect value={form.checklist_template_id} onChange={(e) => setForm({ ...form, checklist_template_id: e.target.value })}><option value="">Tanpa checklist</option>{(templates.data ?? []).map((tp) => <option key={tp.id} value={tp.id}>{tp.name}</option>)}</NativeSelect></Field>
            <Field label={t("label.priority")}><NativeSelect value={form.priority} onChange={(e) => setForm({ ...form, priority: e.target.value })}>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
            <Field label={t("label.team")}><TeamPicker propertyId={pid} domain="housekeeping" value={form.responsible_team_id} onChange={(v) => setForm({ ...form, responsible_team_id: v, default_assignee_user_id: null })} /></Field>
            <Field label="Assignee default"><UserPicker propertyId={pid} teamId={form.responsible_team_id} value={form.default_assignee_user_id} onChange={(v) => setForm({ ...form, default_assignee_user_id: v })} /></Field>
          </div>
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.requires_photo} onCheckedChange={(v) => setForm({ ...form, requires_photo: !!v })} /> Wajib foto before/after</label>
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.is_active} onCheckedChange={(v) => setForm({ ...form, is_active: !!v })} /> Aktif</label>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function AdhocCleaningDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId, properties } = useAuth();
  const toast = useToast();
  const nav = useNavigate();
  const [pid, setPid] = useState(propertyId ?? properties[0]?.id ?? "");
  const [form, setForm] = useState({ title: "", location_id: null as string | null, cleaning_type: "spot", priority: "medium", due_at: "", assignee_team_id: null as string | null, assignee_user_id: null as string | null, requires_photo: true });
  const create = useCreate<Record<string, unknown>, WorkItem>("cleaning-tasks");
  const submit = async () => {
    if (!form.title.trim() || !form.location_id) return toast.error(new Error("Judul dan lokasi wajib"));
    try {
      const x = await create.mutateAsync({ property_id: pid, task_type: "cleaning", cleaning_type: form.cleaning_type, title: form.title.trim(), location_id: form.location_id, priority: form.priority, due_at: form.due_at ? new Date(form.due_at).toISOString() : null, assignee_team_id: form.assignee_team_id, assignee_user_id: form.assignee_user_id, requires_photo: form.requires_photo });
      toast.success(`${x.number} dibuat`);
      onClose();
      nav(`/operations/tasks/${x.id}`);
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Cleaning ad-hoc">
        <div className="space-y-4">
          {properties.length > 1 && <Field label="Property" required><NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); setForm({ ...form, location_id: null }); }}>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>}
          <Field label={t("label.title")} required><Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder="mis. Tumpahan di lobby" /></Field>
          <Field label={t("label.location")} required><LocationPicker propertyId={pid} value={form.location_id} onChange={(id) => setForm({ ...form, location_id: id })} /></Field>
          <div className="grid grid-cols-3 gap-3">
            <Field label="Tipe"><NativeSelect value={form.cleaning_type} onChange={(e) => setForm({ ...form, cleaning_type: e.target.value })}>{CLEANING_TYPES.map((x) => <option key={x} value={x}>{cleaningTypeLabel(x)}</option>)}</NativeSelect></Field>
            <Field label={t("label.priority")}><NativeSelect value={form.priority} onChange={(e) => setForm({ ...form, priority: e.target.value })}>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
            <Field label={t("label.due")}><Input type="datetime-local" value={form.due_at} onChange={(e) => setForm({ ...form, due_at: e.target.value })} /></Field>
            <Field label={t("label.team")}><TeamPicker propertyId={pid} domain="housekeeping" value={form.assignee_team_id} onChange={(v) => setForm({ ...form, assignee_team_id: v, assignee_user_id: null })} /></Field>
            <Field label={t("label.assignee")} className="col-span-2"><UserPicker propertyId={pid} teamId={form.assignee_team_id} value={form.assignee_user_id} onChange={(v) => setForm({ ...form, assignee_user_id: v })} /></Field>
          </div>
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.requires_photo} onCheckedChange={(v) => setForm({ ...form, requires_photo: !!v })} /> Wajib foto before/after</label>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending} onClick={submit}>Buat</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
