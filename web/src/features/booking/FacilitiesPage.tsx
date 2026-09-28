// Facilities — satu komponen, dua konteks:
// • Booking › Facilities (PRD P1 v1.3 §21): katalog fasilitas yang dapat dipesan tenant, jam operasional, aturan slot, approval, penutupan.
// • Property › Facilities (PRD P1 v2 §6.3): facility operasional building (lobby, lift, toilet, koridor, common area, …) dengan
//   kode, tipe, lokasi, status, deskripsi, foto (attachments object_type=facility) dan pekerjaan terbuka di lokasinya.
// Izin: property.facilities.* ATAU booking.facilities.* (server menerima salah satu).
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid, FilterBar, FilterSelect, useUrlFilters } from "@/components/bv/datagrid";
import { useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellLocation, CellText, CellTitle } from "@/components/bv/cells";
import { AttachmentGrid, PhotoEvidenceUploader } from "@/components/bv/checklist";
import { useAction, useAll, useAttachments, useUpdate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useProfile } from "@/lib/profile";
import { statusOptions } from "@/lib/status";
import type { Facility } from "@/api/types";
import { TreeView } from "@/features/property/LocationsPage";

export type { Facility } from "@/api/types";
export const FACILITY_TYPES: Record<string, string> = {
  lobby: "Lobby", lift: "Lift", toilet: "Toilet", corridor: "Koridor", common_area: "Common Area", parking: "Parkir",
  meeting_room: "Meeting Room", function_hall: "Function Hall", gym: "Gym", pool: "Kolam Renang", court: "Lapangan", bbq: "BBQ Area", coworking: "Coworking", lounge: "Lounge", other: "Lainnya",
};
/** Tipe yang secara default dapat dibooking (server: bookableTypes); facility operasional tidak. */
const BOOKABLE_TYPES = new Set(["meeting_room", "function_hall", "gym", "pool", "court", "bbq", "coworking", "lounge"]);
const DAYS = ["Min", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab"];
const perm = (can: (p: string) => boolean, a: string) => can(`property.facilities.${a}`) || can(`booking.facilities.${a}`);

export default function FacilitiesPage({ variant = "booking" }: { variant?: "booking" | "property" }) {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const toast = useToast();
  const isProperty = variant === "property";
  const f = useUrlFilters();
  // Booking: hanya facility bookable; Property: semua facility dengan filter tipe/status/bookable/pencarian di URL
  const query = isProperty ? { property_id: propertyId ?? undefined, type: f.get("type") || undefined, status: f.get("status") || undefined, bookable: f.get("bookable") || undefined, q: f.get("q") || undefined } : { property_id: propertyId ?? undefined, bookable: true };
  const list = useAll<Facility>("facilities", query);
  const [edit, setEdit] = useState<Facility | "new" | null>(null);
  const [schedFor, setSchedFor] = useState<Facility | null>(null);
  const columns = useMemo<ColumnDef<Facility, unknown>[]>(() => {
    // Tabel disederhanakan (29 Sep 2026): kode + nama digabung, satu baris per sel. Deskripsi, flag booking (property) dan
    // aturan slot/durasi (booking) ada di dialog edit.
    const title: ColumnDef<Facility, unknown> = { id: "name", header: isProperty ? "Facility" : "Fasilitas", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.facility_code} title={row.original.name} /> };
    const status: ColumnDef<Facility, unknown> = { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <StatusBadge objectType="facility" status={row.original.status ?? (row.original.is_active ? "operational" : "inactive")} />, size: 130 };
    if (isProperty)
      return [
        title,
        { id: "type", header: t("label.type"), meta: { mobile: "secondary" }, cell: ({ row }) => <CellText max={140}>{FACILITY_TYPES[row.original.facility_type] ?? row.original.facility_type}</CellText>, size: 130 },
        { id: "location", header: t("label.location"), meta: { mobile: "secondary" }, cell: ({ row }) => (row.original.location_id ? <Link to={`/property/locations/${row.original.location_id}`} onClick={(e) => e.stopPropagation()} className="hover:underline"><CellLocation path={row.original.location_path} /></Link> : <span className="text-muted-foreground">—</span>) },
        status,
        { id: "open_work", header: "Pekerjaan terbuka", meta: { mobile: "secondary" }, cell: ({ row }) => { const n = row.original.open_work ?? 0; return n > 0 && row.original.location_id ? <Link to={`/operations/tasks?location_id=${row.original.location_id}&open=true`} onClick={(e) => e.stopPropagation()} className="font-semibold tnum text-warning-text hover:underline">{n}</Link> : <span className="tnum text-muted-foreground">{n}</span>; }, size: 140 },
      ];
    return [
      title,
      { id: "hours", header: "Jam", cell: ({ row }) => <CellText max={200}>{`${row.original.open_time}–${row.original.close_time} · ${row.original.weekdays.length === 7 ? "setiap hari" : row.original.weekdays.map((d) => DAYS[d]).join(" ")}`}</CellText>, size: 200 },
      { id: "capacity", header: "Kapasitas", cell: ({ row }) => <span className="tnum">{row.original.capacity ?? "—"}</span>, size: 90 },
      { id: "approval", header: "Approval", cell: ({ row }) => <Badge tone={row.original.effective_approval ? "warning" : "success"}>{row.original.effective_approval ? "Perlu approval" : "Otomatis"}</Badge>, size: 140 },
      { id: "upcoming", header: "Mendatang", cell: ({ row }) => <span className="tnum">{row.original.upcoming_bookings}</span>, size: 90 },
      status,
    ];
  }, [t, isProperty]);
  return (
    <div>
      <PageHeader
        title="Facilities"
        subtitle={isProperty ? "Facility operasional building (lobby, lift, toilet, koridor, common area, …) beserta status dan pekerjaan terbuka." : "Fasilitas yang dapat dipesan tenant melalui Tenant App (meeting room, gym, function hall…)."}
        actions={perm(can, "create") && <Button onClick={() => setEdit("new")} disabled={!propertyId}><Icon name="add" size={16} /> Tambah Fasilitas</Button>}
      >
        {isProperty && (
          <FilterBar
            spec={{
              type: Object.entries(FACILITY_TYPES).map(([value, label]) => ({ value, label })),
              status: statusOptions("facility"),
              extra: <FilterSelect param="bookable" label="Booking" options={[{ value: "true", label: "Dapat dibooking" }, { value: "false", label: "Operasional saja" }]} className="sm:w-44" />,
            }}
          />
        )}
      </PageHeader>
      {!propertyId && <Alert variant="info" className="mb-4">Pilih property di header untuk menambah fasilitas.</Alert>}
      <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} loading={list.isLoading} error={list.error} onRetry={() => list.refetch()} isFiltered={isProperty && f.isFiltered} empty={{ icon: "meeting_room", title: "Belum ada fasilitas." }}
        onRowClick={isProperty && perm(can, "update") && propertyId ? (r) => setEdit(r) : undefined}
        rowActions={(r) => [
          ...(perm(can, "update") ? [{ label: "Edit", icon: "edit", onSelect: () => setEdit(r) }] : []),
          ...(can("booking.facilities.update") && r.is_bookable ? [{ label: "Penutupan / jadwal khusus", icon: "event_busy", onSelect: () => setSchedFor(r) }] : []),
          ...(perm(can, "delete") ? [{ label: "Hapus", icon: "delete", destructive: true, onSelect: () => api(`facilities/${r.id}`, { method: "DELETE" }).then(() => { toast.action("deleted", "Fasilitas"); list.refetch(); }).catch(toast.error) }] : []),
        ]} />
      {edit && propertyId && <FacilityDialog item={edit === "new" ? null : edit} propertyId={propertyId} variant={variant} onClose={() => setEdit(null)} />}
      {schedFor && <SchedulesDialog facility={schedFor} onClose={() => setSchedFor(null)} />}
    </div>
  );
}

function FacilityDialog({ item, propertyId, variant, onClose }: { item: Facility | null; propertyId: string; variant: "booking" | "property"; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const prof = useProfile();
  // setelah dibuat, dialog tetap terbuka dalam mode edit agar foto dapat diunggah (attachment butuh object_id)
  const [current, setCurrent] = useState<Facility | null>(item);
  const defaultType = variant === "property" ? "lobby" : "meeting_room";
  const [f, setF] = useState({
    name: item?.name ?? "", description: item?.description ?? "", facility_type: item?.facility_type ?? defaultType, capacity: item?.capacity?.toString() ?? "", approval: item?.requires_approval === null || item === null ? "inherit" : item.requires_approval ? "yes" : "no",
    slot_minutes: String(item?.slot_minutes ?? 60), min_duration_minutes: String(item?.min_duration_minutes ?? 60), max_duration_minutes: String(item?.max_duration_minutes ?? 240), advance_booking_days: String(item?.advance_booking_days ?? 30),
    open_time: item?.open_time ?? "08:00", close_time: item?.close_time ?? "21:00", weekdays: item?.weekdays ?? [0, 1, 2, 3, 4, 5, 6], rules: item?.rules ?? "", location_id: item?.location_id ?? null as string | null,
    status: item?.status ?? (item && !item.is_active ? "inactive" : "operational"), is_bookable: item?.is_bookable ?? (variant === "booking" || BOOKABLE_TYPES.has(defaultType)),
  });
  const canBook = prof.has("facility_booking");
  const create = useAction<Record<string, unknown>, Facility>(() => "facilities", { invalidate: ["all", "list"] });
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("facilities");
  const photos = useAttachments("facility", current?.id);
  const submit = async () => {
    const body: Record<string, unknown> = { name: f.name.trim(), description: f.description || null, facility_type: f.facility_type, location_id: f.location_id, status: f.status, is_bookable: canBook ? f.is_bookable : false };
    if (f.is_bookable && canBook)
      Object.assign(body, { capacity: f.capacity ? Number(f.capacity) : null, requires_approval: f.approval === "inherit" ? null : f.approval === "yes", slot_minutes: Number(f.slot_minutes), min_duration_minutes: Number(f.min_duration_minutes), max_duration_minutes: Number(f.max_duration_minutes), advance_booking_days: Number(f.advance_booking_days), open_time: f.open_time, close_time: f.close_time, weekdays: f.weekdays, rules: f.rules || null });
    try {
      if (current) {
        await update.mutateAsync({ id: current.id, version: current.version, ...body });
        toast.action("saved", "Fasilitas");
        onClose();
      } else {
        const created = await create.mutateAsync({ ...body, property_id: propertyId });
        toast.action("created", `Fasilitas ${created.facility_code}`);
        setCurrent(created);
      }
    } catch (e) {
      toast.error(e);
    }
  };
  const bookingFields = f.is_bookable && canBook;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={current ? `Edit ${current.name}` : "Tambah Fasilitas"}>
        <div className="space-y-4">
          {current && !item && <Alert variant="success">Fasilitas {current.facility_code} dibuat. Tambahkan foto di bawah, lalu simpan/tutup.</Alert>}
          <div className="grid grid-cols-2 gap-3">
            <Field label="Nama" required><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
            <Field label="Tipe"><NativeSelect value={f.facility_type} onChange={(e) => setF({ ...f, facility_type: e.target.value, is_bookable: current ? f.is_bookable : BOOKABLE_TYPES.has(e.target.value) })}>{Object.entries(FACILITY_TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</NativeSelect></Field>
            <Field label={t("label.status")}><NativeSelect value={f.status} onChange={(e) => setF({ ...f, status: e.target.value })}>{statusOptions("facility").map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
            {canBook && <Field label="Booking tenant"><Checkbox label="Dapat dibooking (Tenant App)" checked={f.is_bookable} onCheckedChange={(v) => setF({ ...f, is_bookable: v })} /></Field>}
          </div>
          <Field label="Lokasi"><div className="max-h-44 overflow-y-auto rounded-[var(--radius-md)] border border-border p-1"><TreeView propertyId={propertyId} selected={f.location_id} onSelect={(n) => setF({ ...f, location_id: n.id })} allowTypes={["building", "tower", "floor", "area", "space", "unit"]} /></div></Field>
          <Field label="Deskripsi"><Textarea rows={2} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
          {bookingFields && (
            <>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Kapasitas (orang)"><Input type="number" value={f.capacity} onChange={(e) => setF({ ...f, capacity: e.target.value })} /></Field>
                <Field label="Approval (OD-P1-007)"><NativeSelect value={f.approval} onChange={(e) => setF({ ...f, approval: e.target.value })}><option value="inherit">Ikut pengaturan property</option><option value="no">Konfirmasi otomatis</option><option value="yes">Perlu approval</option></NativeSelect></Field>
                <Field label="Buka"><Input type="time" value={f.open_time} onChange={(e) => setF({ ...f, open_time: e.target.value })} /></Field>
                <Field label="Tutup"><Input type="time" value={f.close_time} onChange={(e) => setF({ ...f, close_time: e.target.value })} /></Field>
                <Field label="Slot (menit)"><Input type="number" value={f.slot_minutes} onChange={(e) => setF({ ...f, slot_minutes: e.target.value })} /></Field>
                <Field label="Durasi min–max (menit)"><div className="flex gap-2"><Input type="number" value={f.min_duration_minutes} onChange={(e) => setF({ ...f, min_duration_minutes: e.target.value })} /><Input type="number" value={f.max_duration_minutes} onChange={(e) => setF({ ...f, max_duration_minutes: e.target.value })} /></div></Field>
                <Field label="Maks. hari ke depan"><Input type="number" value={f.advance_booking_days} onChange={(e) => setF({ ...f, advance_booking_days: e.target.value })} /></Field>
              </div>
              <Field label="Hari operasional"><div className="flex flex-wrap gap-2">{DAYS.map((d, i) => <Checkbox key={d} label={d} checked={f.weekdays.includes(i)} onCheckedChange={(v) => setF({ ...f, weekdays: v ? [...f.weekdays, i].sort() : f.weekdays.filter((x) => x !== i) })} />)}</div></Field>
              <Field label="Aturan penggunaan (tampil di Tenant App)"><Textarea rows={2} value={f.rules} onChange={(e) => setF({ ...f, rules: e.target.value })} /></Field>
            </>
          )}
          <Field label="Foto">
            {current ? (
              <div className="space-y-2">
                <PhotoEvidenceUploader objectType="facility" objectId={current.id} attachmentType="photo" compact label="Unggah foto facility" onUploaded={() => photos.refetch()} />
                <AttachmentGrid items={photos.data ?? []} emptyLabel="Belum ada foto." />
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">Foto dapat diunggah setelah fasilitas disimpan.</p>
            )}
          </Field>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{current && !item ? "Tutup" : t("action.discard")}</Button><Button loading={create.isPending || update.isPending} disabled={!f.name.trim()} onClick={submit}>{t("action.save")}</Button></DialogFooter>
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
          <Button loading={add.isPending} disabled={!form.starts_at || !form.ends_at} onClick={() => add.mutateAsync({ starts_at: new Date(form.starts_at).toISOString(), ends_at: new Date(form.ends_at).toISOString(), reason: form.reason }).then(() => { setForm({ starts_at: "", ends_at: "", reason: "" }); toast.action("created", "Penutupan"); }).catch(toast.error)}>Tambah penutupan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
