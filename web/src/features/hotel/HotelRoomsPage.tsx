// Commercial › Hotel Booking Management › Rooms, Room Types, Rates (PRD P1 v1.3 §3.9). Room = Unit (TD-P1-005); status kamar
// mengikuti PRD §16 (Available/Occupied/Dirty/Clean/Inspected/OOO/OOS) dan ter-link ke Housekeeping.
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Tabs, TabsContent, TabsList, TabsTrigger, Textarea } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { useAction, useAll, useLocationTree, useUpdate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { ROOM_STATUS, rp, type Rate, type Room, type RoomType } from "./hotel-api";
import type { TreeNode } from "@/api/types";

const DAYS = ["Min", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab"];

export default function HotelRoomsPage() {
  const { propertyId } = useAuth();
  if (!propertyId) return <Alert variant="info">Pilih property (profile Hotel) di header.</Alert>;
  return (
    <div>
      <PageHeader title="Rooms & Rates" subtitle="Inventori kamar property (Room = Unit), tipe kamar, dan tarif per malam." />
      <Tabs defaultValue="rooms">
        <TabsList><TabsTrigger value="rooms">Rooms</TabsTrigger><TabsTrigger value="types">Room Types</TabsTrigger><TabsTrigger value="rates">Rates</TabsTrigger></TabsList>
        <TabsContent value="rooms" className="pt-4"><RoomsBoard propertyId={propertyId} /></TabsContent>
        <TabsContent value="types" className="pt-4"><RoomTypesTab propertyId={propertyId} /></TabsContent>
        <TabsContent value="rates" className="pt-4"><RatesTab propertyId={propertyId} /></TabsContent>
      </Tabs>
    </div>
  );
}

function RoomsBoard({ propertyId }: { propertyId: string }) {
  const { can } = useAuth();
  const toast = useToast();
  const rooms = useAll<Room>("hotel/rooms", { property_id: propertyId });
  const [createOpen, setCreateOpen] = useState(false);
  const [statusFor, setStatusFor] = useState<Room | null>(null);
  const [newStatus, setNewStatus] = useState("");
  const [note, setNote] = useState("");
  const setStatus = useAction<{ id: string; room_status: string; note: string }, Room>((i) => `hotel/rooms/${i.id}/status`, { body: (i) => ({ room_status: i.room_status, note: i.note }), invalidate: ["all", "hotel"] });
  const list = rooms.data ?? [];
  const groups = Object.keys(ROOM_STATUS).map((k) => ({ key: k, items: list.filter((r) => r.room_status === k) }));
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex flex-wrap gap-2">{groups.map((g) => <Badge key={g.key} tone={ROOM_STATUS[g.key].tone}>{ROOM_STATUS[g.key].label}: {g.items.length}</Badge>)}</div>
        {can("hotel.rooms.create") && <Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> Tambah Kamar</Button>}
      </div>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6">
        {list.map((r) => (
          <button key={r.location_id} type="button" onClick={() => { setStatusFor(r); setNewStatus(""); setNote(""); }} className="rounded-[var(--radius-md)] border border-border bg-surface p-3 text-left hover:bg-surface-container">
            <div className="flex items-center justify-between"><span className="text-lg font-semibold">{r.room_number}</span><Badge tone={ROOM_STATUS[r.room_status]?.tone ?? "neutral"}>{ROOM_STATUS[r.room_status]?.label}</Badge></div>
            <div className="text-xs text-muted-foreground">{r.room_type_name}{r.floor_name ? ` · ${r.floor_name}` : ""}</div>
            {r.current_guest && <div className="mt-1 truncate text-xs"><Icon name="person" size={12} className="mr-1 align-middle" />{r.current_guest}</div>}
            {r.open_cleaning_tasks > 0 && <div className="mt-1 text-xs text-warning"><Icon name="cleaning_services" size={12} className="mr-1 align-middle" />{r.open_cleaning_tasks} cleaning task</div>}
            {r.status_note && <div className="mt-1 truncate text-xs text-muted-foreground">{r.status_note}</div>}
          </button>
        ))}
        {list.length === 0 && <p className="col-span-full text-sm text-muted-foreground">Belum ada kamar. Buat tipe kamar lalu tambah kamar per lantai.</p>}
      </div>
      {createOpen && <CreateRoomDialog propertyId={propertyId} onClose={() => setCreateOpen(false)} />}
      {statusFor && (
        <Dialog open onOpenChange={(o) => !o && setStatusFor(null)}>
          <DialogContent title={`Kamar ${statusFor.room_number} · ${ROOM_STATUS[statusFor.room_status]?.label}`} description="Occupied hanya melalui check-in; Dirty otomatis saat check-out; Clean/Inspected otomatis dari Housekeeping. Perubahan manual untuk OOO/OOS dan koreksi.">
            <div className="space-y-3 text-sm">
              <div>{statusFor.room_type_name} · {statusFor.room_code}{statusFor.current_guest ? ` · tamu: ${statusFor.current_guest}` : ""}{statusFor.next_arrival ? ` · kedatangan berikutnya ${new Date(statusFor.next_arrival).toLocaleDateString("id-ID")}` : ""}</div>
              {can("hotel.rooms.set_status") && statusFor.allowed_statuses.length > 0 && <>
                <Field label="Ubah status ke"><NativeSelect value={newStatus} onChange={(e) => setNewStatus(e.target.value)}><option value="">—</option>{statusFor.allowed_statuses.map((s) => <option key={s} value={s}>{ROOM_STATUS[s]?.label ?? s}</option>)}</NativeSelect></Field>
                <Field label="Catatan"><Input value={note} onChange={(e) => setNote(e.target.value)} placeholder="mis. AC rusak, renovasi" /></Field>
              </>}
            </div>
            <DialogFooter><Button variant="secondary" onClick={() => setStatusFor(null)}>Tutup</Button>{newStatus && <Button loading={setStatus.isPending} onClick={() => setStatus.mutateAsync({ id: statusFor.location_id, room_status: newStatus, note }).then(() => { toast.success("Status kamar diperbarui"); setStatusFor(null); }).catch(toast.error)}>Simpan</Button>}</DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function CreateRoomDialog({ propertyId, onClose }: { propertyId: string; onClose: () => void }) {
  const toast = useToast();
  const types = useAll<RoomType>("hotel/room-types", { property_id: propertyId });
  const tree = useLocationTree(propertyId);
  const floors: TreeNode[] = [];
  const walk = (n?: TreeNode) => { if (!n) return; if (n.location_type === "floor") floors.push(n); n.children?.forEach(walk); };
  walk(tree.data);
  const [f, setF] = useState({ floor_id: "", room_type_id: "", room_numbers: "", size_m2: "" });
  const create = useAction<Record<string, unknown>, Room>(() => "hotel/rooms", { invalidate: ["all", "hotel", "tree"] });
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    const nums = f.room_numbers.split(/[,\s]+/).map((x) => x.trim()).filter(Boolean);
    let ok = 0;
    for (const n of nums) {
      try { await create.mutateAsync({ property_id: propertyId, floor_id: f.floor_id, room_type_id: f.room_type_id, room_number: n, size_m2: f.size_m2 ? Number(f.size_m2) : null }); ok++; } catch (e) { toast.error(e); }
    }
    setBusy(false);
    if (ok) { toast.success(`${ok} kamar ditambahkan`); onClose(); }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Tambah Kamar" description="Setiap kamar dibuat sebagai Unit (unit_type hotel_room) di lantai terpilih, sehingga Service Request/Work Order/cleaning task menunjuk lokasi yang sama.">
        <div className="space-y-3">
          <Field label="Lantai" required><NativeSelect value={f.floor_id} onChange={(e) => setF({ ...f, floor_id: e.target.value })}><option value="">— pilih —</option>{floors.map((fl) => <option key={fl.id} value={fl.id}>{fl.path_text ?? fl.name}</option>)}</NativeSelect></Field>
          <Field label="Tipe kamar" required><NativeSelect value={f.room_type_id} onChange={(e) => setF({ ...f, room_type_id: e.target.value })}><option value="">— pilih —</option>{(types.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}</NativeSelect></Field>
          <Field label="Nomor kamar (pisahkan koma untuk banyak)" required><Input value={f.room_numbers} onChange={(e) => setF({ ...f, room_numbers: e.target.value })} placeholder="501, 502, 503" /></Field>
          <Field label="Luas (m²)"><Input type="number" value={f.size_m2} onChange={(e) => setF({ ...f, size_m2: e.target.value })} /></Field>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={busy} disabled={!f.floor_id || !f.room_type_id || !f.room_numbers.trim()} onClick={submit}>Tambah</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RoomTypesTab({ propertyId }: { propertyId: string }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const types = useAll<RoomType>("hotel/room-types", { property_id: propertyId });
  const [edit, setEdit] = useState<RoomType | "new" | null>(null);
  const [f, setF] = useState({ name: "", description: "", capacity_adults: "2", capacity_children: "0", bed_type: "", size_m2: "", amenities: "", base_rate: "0", status: "active" });
  const create = useAction<Record<string, unknown>, RoomType>(() => "hotel/room-types", { invalidate: ["all", "hotel"] });
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("hotel/room-types");
  const open = (x: RoomType | "new") => { setEdit(x); if (x !== "new") setF({ name: x.name, description: x.description ?? "", capacity_adults: String(x.capacity_adults), capacity_children: String(x.capacity_children), bed_type: x.bed_type ?? "", size_m2: x.size_m2?.toString() ?? "", amenities: x.amenities.join(", "), base_rate: String(x.base_rate), status: x.status }); else setF({ name: "", description: "", capacity_adults: "2", capacity_children: "0", bed_type: "", size_m2: "", amenities: "", base_rate: "0", status: "active" }); };
  const submit = async () => {
    const body: Record<string, unknown> = { name: f.name.trim(), description: f.description || null, capacity_adults: Number(f.capacity_adults) || 1, capacity_children: Number(f.capacity_children) || 0, bed_type: f.bed_type || null, size_m2: f.size_m2 ? Number(f.size_m2) : null, amenities: f.amenities.split(",").map((x) => x.trim()).filter(Boolean), base_rate: Number(f.base_rate) || 0 };
    try {
      if (edit && edit !== "new") await update.mutateAsync({ id: edit.id, version: edit.version, ...body, status: f.status });
      else await create.mutateAsync({ ...body, property_id: propertyId });
      toast.success("Tipe kamar disimpan");
      setEdit(null);
    } catch (e) { toast.error(e); }
  };
  return (
    <div className="space-y-3">
      {can("hotel.room_types.create") && <Button onClick={() => open("new")}><Icon name="add" size={16} /> Tambah Tipe Kamar</Button>}
      <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
        {(types.data ?? []).map((x) => <li key={x.id} className="flex items-center justify-between px-3 py-2"><span><span className="font-semibold">{x.name}</span> <span className="font-mono text-xs text-muted-foreground">{x.room_type_code}</span> · {x.capacity_adults} dewasa{x.capacity_children ? `, ${x.capacity_children} anak` : ""} · {x.room_count} kamar{x.amenities.length ? ` · ${x.amenities.join(", ")}` : ""}</span><span className="flex items-center gap-3"><span className="tnum">{rp(x.base_rate)}/malam</span><Badge tone={x.status === "active" ? "success" : "neutral"}>{x.status}</Badge>{can("hotel.room_types.update") && <Button size="sm" variant="ghost" onClick={() => open(x)}>Edit</Button>}</span></li>)}
        {(types.data ?? []).length === 0 && <li className="px-3 py-2 text-muted-foreground">Belum ada tipe kamar.</li>}
      </ul>
      {edit && (
        <Dialog open onOpenChange={(o) => !o && setEdit(null)}>
          <DialogContent title={edit === "new" ? "Tambah Tipe Kamar" : `Edit ${edit.name}`}>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Nama" required className="col-span-2"><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
              <Field label="Kapasitas dewasa"><Input type="number" value={f.capacity_adults} onChange={(e) => setF({ ...f, capacity_adults: e.target.value })} /></Field>
              <Field label="Kapasitas anak"><Input type="number" value={f.capacity_children} onChange={(e) => setF({ ...f, capacity_children: e.target.value })} /></Field>
              <Field label="Tipe tempat tidur"><Input value={f.bed_type} onChange={(e) => setF({ ...f, bed_type: e.target.value })} /></Field>
              <Field label="Luas (m²)"><Input type="number" value={f.size_m2} onChange={(e) => setF({ ...f, size_m2: e.target.value })} /></Field>
              <Field label="Tarif dasar / malam (Rp)"><Input type="number" value={f.base_rate} onChange={(e) => setF({ ...f, base_rate: e.target.value })} /></Field>
              {edit !== "new" && <Field label="Status"><NativeSelect value={f.status} onChange={(e) => setF({ ...f, status: e.target.value })}><option value="active">active</option><option value="archived">archived</option></NativeSelect></Field>}
              <Field label="Amenities (pisahkan koma)" className="col-span-2"><Input value={f.amenities} onChange={(e) => setF({ ...f, amenities: e.target.value })} /></Field>
              <Field label="Deskripsi" className="col-span-2"><Textarea rows={2} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
            </div>
            <DialogFooter><Button variant="secondary" onClick={() => setEdit(null)}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} disabled={!f.name.trim()} onClick={submit}>{t("action.save")}</Button></DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function RatesTab({ propertyId }: { propertyId: string }) {
  const { can } = useAuth();
  const toast = useToast();
  const types = useAll<RoomType>("hotel/room-types", { property_id: propertyId });
  const rates = useAll<Rate>("hotel/rates", { property_id: propertyId });
  const [open, setOpen] = useState(false);
  const [f, setF] = useState({ room_type_id: "", name: "", rate_per_night: "", valid_from: "", valid_until: "", weekdays: [0, 1, 2, 3, 4, 5, 6], min_nights: "1", priority: "0" });
  const create = useAction<Record<string, unknown>, Rate>(() => "hotel/rates", { invalidate: ["all", "hotel"] });
  const archive = useUpdate<{ id: string; version: number; status: string }>("hotel/rates");
  return (
    <div className="space-y-3">
      {can("hotel.rates.create") && <Button onClick={() => setOpen(true)}><Icon name="add" size={16} /> Tambah Rate</Button>}
      <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
        {(rates.data ?? []).map((r) => <li key={r.id} className="flex items-center justify-between px-3 py-2"><span><span className="font-semibold">{r.name}</span> · {r.room_type_name} <span className="font-mono text-xs text-muted-foreground">{r.rate_code}</span> · {r.weekdays.length === 7 ? "setiap hari" : r.weekdays.map((d) => DAYS[d]).join(" ")}{r.valid_from ? ` · ${r.valid_from.slice(0, 10)} – ${r.valid_until?.slice(0, 10) ?? "…"}` : ""} · min {r.min_nights} malam · prioritas {r.priority}</span><span className="flex items-center gap-3"><span className="tnum font-semibold">{rp(r.rate_per_night)}</span><Badge tone={r.status === "active" ? "success" : "neutral"}>{r.status}</Badge>{can("hotel.rates.update") && r.status === "active" && <Button size="sm" variant="ghost" onClick={() => archive.mutateAsync({ id: r.id, version: r.version, status: "archived" }).then(() => toast.success("Rate diarsipkan")).catch(toast.error)}>Arsipkan</Button>}</span></li>)}
        {(rates.data ?? []).length === 0 && <li className="px-3 py-2 text-muted-foreground">Belum ada rate khusus — tarif dasar tipe kamar dipakai.</li>}
      </ul>
      {open && (
        <Dialog open onOpenChange={(o) => !o && setOpen(false)}>
          <DialogContent title="Tambah Rate" description="Rate dengan prioritas lebih tinggi menang; malam yang tidak tercakup memakai tarif dasar tipe kamar.">
            <div className="grid grid-cols-2 gap-3">
              <Field label="Tipe kamar" required className="col-span-2"><NativeSelect value={f.room_type_id} onChange={(e) => setF({ ...f, room_type_id: e.target.value })}><option value="">— pilih —</option>{(types.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}</NativeSelect></Field>
              <Field label="Nama" required><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="Weekend / Long Stay" /></Field>
              <Field label="Tarif / malam (Rp)" required><Input type="number" value={f.rate_per_night} onChange={(e) => setF({ ...f, rate_per_night: e.target.value })} /></Field>
              <Field label="Berlaku dari"><Input type="date" value={f.valid_from} onChange={(e) => setF({ ...f, valid_from: e.target.value })} /></Field>
              <Field label="Sampai"><Input type="date" value={f.valid_until} onChange={(e) => setF({ ...f, valid_until: e.target.value })} /></Field>
              <Field label="Min. malam"><Input type="number" value={f.min_nights} onChange={(e) => setF({ ...f, min_nights: e.target.value })} /></Field>
              <Field label="Prioritas"><Input type="number" value={f.priority} onChange={(e) => setF({ ...f, priority: e.target.value })} /></Field>
              <Field label="Hari" className="col-span-2"><div className="flex flex-wrap gap-2">{DAYS.map((d, i) => <Checkbox key={d} label={d} checked={f.weekdays.includes(i)} onCheckedChange={(v) => setF({ ...f, weekdays: v ? [...f.weekdays, i].sort() : f.weekdays.filter((x) => x !== i) })} />)}</div></Field>
            </div>
            <DialogFooter><Button variant="secondary" onClick={() => setOpen(false)}>Batal</Button><Button loading={create.isPending} disabled={!f.room_type_id || !f.name.trim() || !f.rate_per_night} onClick={() => create.mutateAsync({ room_type_id: f.room_type_id, name: f.name.trim(), rate_per_night: Number(f.rate_per_night), valid_from: f.valid_from || null, valid_until: f.valid_until || null, weekdays: f.weekdays, min_nights: Number(f.min_nights) || 1, priority: Number(f.priority) || 0 }).then(() => { toast.success("Rate ditambahkan"); setOpen(false); }).catch(toast.error)}>Simpan</Button></DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
