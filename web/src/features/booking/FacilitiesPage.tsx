// Booking › Facilities (PRD P1 v1.3 §21): katalog fasilitas property, jam operasional, aturan slot, approval, penutupan.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { useToast } from "@/components/bv/common";
import { useAction, useAll, useUpdate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { TreeView } from "@/features/property/LocationsPage";

export interface Facility {
  id: string; property_id: string; facility_code: string; name: string; description: string | null; facility_type: string; location_id: string | null; location_path: string | null;
  capacity: number | null; requires_approval: boolean | null; effective_approval: boolean; slot_minutes: number; min_duration_minutes: number; max_duration_minutes: number; advance_booking_days: number;
  open_time: string; close_time: string; weekdays: number[]; rules: string | null; is_active: boolean; upcoming_bookings: number; version: number;
}
export const FACILITY_TYPES: Record<string, string> = { meeting_room: "Meeting Room", function_hall: "Function Hall", gym: "Gym", pool: "Kolam Renang", court: "Lapangan", bbq: "BBQ Area", coworking: "Coworking", lounge: "Lounge", parking: "Parkir", other: "Lainnya" };
const DAYS = ["Min", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab"];

export default function FacilitiesPage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const toast = useToast();
  const list = useAll<Facility>("facilities", { property_id: propertyId ?? undefined });
  const [edit, setEdit] = useState<Facility | "new" | null>(null);
  const [schedFor, setSchedFor] = useState<Facility | null>(null);
  const columns = useMemo<ColumnDef<Facility, unknown>[]>(() => [
    { id: "code", header: "Kode", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.facility_code}</span>, size: 120 },
    { id: "name", header: "Fasilitas", cell: ({ row }) => <div><div className="font-medium">{row.original.name}</div><div className="text-xs text-muted-foreground">{FACILITY_TYPES[row.original.facility_type] ?? row.original.facility_type}{row.original.location_path ? ` · ${row.original.location_path}` : ""}</div></div> },
    { id: "hours", header: "Jam", cell: ({ row }) => <span className="text-sm">{row.original.open_time}–{row.original.close_time} <span className="text-xs text-muted-foreground">{row.original.weekdays.length === 7 ? "setiap hari" : row.original.weekdays.map((d) => DAYS[d]).join(" ")}</span></span>, size: 200 },
    { id: "slot", header: "Slot / durasi", cell: ({ row }) => <span className="text-xs">{row.original.slot_minutes} mnt · {row.original.min_duration_minutes}–{row.original.max_duration_minutes} mnt · H-{row.original.advance_booking_days}</span>, size: 190 },
    { id: "capacity", header: "Kapasitas", cell: ({ row }) => <span className="tnum">{row.original.capacity ?? "—"}</span>, size: 90 },
    { id: "approval", header: "Approval", cell: ({ row }) => <Badge tone={row.original.effective_approval ? "warning" : "success"}>{row.original.effective_approval ? "Perlu approval" : "Otomatis"}{row.original.requires_approval === null ? " · property" : ""}</Badge>, size: 170 },
    { id: "upcoming", header: "Mendatang", cell: ({ row }) => <span className="tnum">{row.original.upcoming_bookings}</span>, size: 90 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={row.original.is_active ? "success" : "neutral"}>{row.original.is_active ? "Aktif" : "Nonaktif"}</Badge>, size: 90 },
  ], [t]);
  return (
    <div>
      <PageHeader title="Facilities" subtitle="Fasilitas yang dapat dipesan tenant melalui Tenant App (meeting room, gym, function hall…)." actions={can("booking.facilities.create") && <Button onClick={() => setEdit("new")} disabled={!propertyId}><Icon name="add" size={16} /> Tambah Fasilitas</Button>} />
      {!propertyId && <Alert variant="info" className="mb-4">Pilih property di header untuk menambah fasilitas.</Alert>}
      <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} loading={list.isLoading} empty={{ message: "Belum ada fasilitas." }}
        rowActions={(r) => [
          ...(can("booking.facilities.update") ? [{ label: "Edit", icon: "edit", onSelect: () => setEdit(r) }, { label: "Penutupan / jadwal khusus", icon: "event_busy", onSelect: () => setSchedFor(r) }] : []),
          ...(can("booking.facilities.delete") ? [{ label: "Hapus", icon: "delete", destructive: true, onSelect: () => api(`facilities/${r.id}`, { method: "DELETE" }).then(() => { toast.success("Fasilitas dihapus"); list.refetch(); }).catch(toast.error) }] : []),
        ]} />
      {edit && propertyId && <FacilityDialog item={edit === "new" ? null : edit} propertyId={propertyId} onClose={() => setEdit(null)} />}
      {schedFor && <SchedulesDialog facility={schedFor} onClose={() => setSchedFor(null)} />}
    </div>
  );
}

function FacilityDialog({ item, propertyId, onClose }: { item: Facility | null; propertyId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [f, setF] = useState({
    name: item?.name ?? "", description: item?.description ?? "", facility_type: item?.facility_type ?? "meeting_room", capacity: item?.capacity?.toString() ?? "", approval: item?.requires_approval === null || item === null ? "inherit" : item.requires_approval ? "yes" : "no",
    slot_minutes: String(item?.slot_minutes ?? 60), min_duration_minutes: String(item?.min_duration_minutes ?? 60), max_duration_minutes: String(item?.max_duration_minutes ?? 240), advance_booking_days: String(item?.advance_booking_days ?? 30),
    open_time: item?.open_time ?? "08:00", close_time: item?.close_time ?? "21:00", weekdays: item?.weekdays ?? [0, 1, 2, 3, 4, 5, 6], rules: item?.rules ?? "", is_active: item?.is_active ?? true, location_id: item?.location_id ?? null as string | null,
  });
  const create = useAction<Record<string, unknown>, Facility>(() => "facilities", { invalidate: ["all", "list"] });
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("facilities");
  const submit = async () => {
    const body: Record<string, unknown> = { name: f.name.trim(), description: f.description || null, facility_type: f.facility_type, capacity: f.capacity ? Number(f.capacity) : null, requires_approval: f.approval === "inherit" ? null : f.approval === "yes",
      slot_minutes: Number(f.slot_minutes), min_duration_minutes: Number(f.min_duration_minutes), max_duration_minutes: Number(f.max_duration_minutes), advance_booking_days: Number(f.advance_booking_days), open_time: f.open_time, close_time: f.close_time, weekdays: f.weekdays, rules: f.rules || null, is_active: f.is_active, location_id: f.location_id };
    try {
      if (item) await update.mutateAsync({ id: item.id, version: item.version, ...body });
      else await create.mutateAsync({ ...body, property_id: propertyId });
      toast.success("Fasilitas disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit ${item.name}` : "Tambah Fasilitas"}>
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Nama" required><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
            <Field label="Tipe"><NativeSelect value={f.facility_type} onChange={(e) => setF({ ...f, facility_type: e.target.value })}>{Object.entries(FACILITY_TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</NativeSelect></Field>
            <Field label="Kapasitas (orang)"><Input type="number" value={f.capacity} onChange={(e) => setF({ ...f, capacity: e.target.value })} /></Field>
            <Field label="Approval (OD-P1-007)"><NativeSelect value={f.approval} onChange={(e) => setF({ ...f, approval: e.target.value })}><option value="inherit">Ikut pengaturan property</option><option value="no">Konfirmasi otomatis</option><option value="yes">Perlu approval</option></NativeSelect></Field>
            <Field label="Buka"><Input type="time" value={f.open_time} onChange={(e) => setF({ ...f, open_time: e.target.value })} /></Field>
            <Field label="Tutup"><Input type="time" value={f.close_time} onChange={(e) => setF({ ...f, close_time: e.target.value })} /></Field>
            <Field label="Slot (menit)"><Input type="number" value={f.slot_minutes} onChange={(e) => setF({ ...f, slot_minutes: e.target.value })} /></Field>
            <Field label="Durasi min–max (menit)"><div className="flex gap-2"><Input type="number" value={f.min_duration_minutes} onChange={(e) => setF({ ...f, min_duration_minutes: e.target.value })} /><Input type="number" value={f.max_duration_minutes} onChange={(e) => setF({ ...f, max_duration_minutes: e.target.value })} /></div></Field>
            <Field label="Maks. hari ke depan"><Input type="number" value={f.advance_booking_days} onChange={(e) => setF({ ...f, advance_booking_days: e.target.value })} /></Field>
          </div>
          <Field label="Hari operasional"><div className="flex flex-wrap gap-2">{DAYS.map((d, i) => <Checkbox key={d} label={d} checked={f.weekdays.includes(i)} onCheckedChange={(v) => setF({ ...f, weekdays: v ? [...f.weekdays, i].sort() : f.weekdays.filter((x) => x !== i) })} />)}</div></Field>
          <Field label="Lokasi fisik (opsional)"><div className="max-h-44 overflow-y-auto rounded-[var(--radius-md)] border border-border p-1"><TreeView propertyId={propertyId} selected={f.location_id} onSelect={(n) => setF({ ...f, location_id: n.id })} allowTypes={["space", "area", "unit"]} /></div></Field>
          <Field label="Deskripsi"><Textarea rows={2} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
          <Field label="Aturan penggunaan (tampil di Tenant App)"><Textarea rows={2} value={f.rules} onChange={(e) => setF({ ...f, rules: e.target.value })} /></Field>
          {item && <Checkbox label="Aktif (dapat dipesan)" checked={f.is_active} onCheckedChange={(v) => setF({ ...f, is_active: v })} />}
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} disabled={!f.name.trim()} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface Schedule { id: string; kind: string; starts_at: string; ends_at: string; reason: string | null }

function SchedulesDialog({ facility, onClose }: { facility: Facility; onClose: () => void }) {
  const toast = useToast();
  const list = useAll<Schedule>(`facilities/${facility.id}/schedules`);
  const add = useAction<{ starts_at: string; ends_at: string; reason: string }, Schedule>(() => `facilities/${facility.id}/schedules`, { invalidate: ["all"] });
  const [form, setForm] = useState({ starts_at: "", ends_at: "", reason: "" });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Penutupan · ${facility.name}`} description="Selama penutupan, slot tidak dapat dipesan (booking baru ditolak).">
        <div className="space-y-3">
          <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
            {(list.data ?? []).map((s) => (
              <li key={s.id} className="flex items-center justify-between px-3 py-2">
                <span>{new Date(s.starts_at).toLocaleString("id-ID")} → {new Date(s.ends_at).toLocaleString("id-ID")} <span className="text-muted-foreground">{s.reason}</span></span>
                <Button size="sm" variant="ghost" onClick={() => api(`facilities/${facility.id}/schedules/${s.id}`, { method: "DELETE" }).then(() => list.refetch()).catch(toast.error)}>Hapus</Button>
              </li>
            ))}
            {(list.data ?? []).length === 0 && <li className="px-3 py-2 text-muted-foreground">Tidak ada penutupan terjadwal.</li>}
          </ul>
          <div className="grid grid-cols-3 gap-2">
            <Field label="Mulai"><Input type="datetime-local" value={form.starts_at} onChange={(e) => setForm({ ...form, starts_at: e.target.value })} /></Field>
            <Field label="Selesai"><Input type="datetime-local" value={form.ends_at} onChange={(e) => setForm({ ...form, ends_at: e.target.value })} /></Field>
            <Field label="Alasan"><Input value={form.reason} onChange={(e) => setForm({ ...form, reason: e.target.value })} /></Field>
          </div>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Tutup</Button>
          <Button loading={add.isPending} disabled={!form.starts_at || !form.ends_at} onClick={() => add.mutateAsync({ starts_at: new Date(form.starts_at).toISOString(), ends_at: new Date(form.ends_at).toISOString(), reason: form.reason }).then(() => { setForm({ starts_at: "", ends_at: "", reason: "" }); toast.success("Penutupan ditambahkan"); }).catch(toast.error)}>Tambah penutupan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
