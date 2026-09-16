// Commercial › Hotel Booking Management › Reservations (PRD P1 v1.3 §3.9, WF-P1-007): dates → room type → availability → rate →
// guest → reservation; assign room, check-in (+ akun Guest App), check-out (kamar Dirty → Housekeeping, invoice), cancel, no-show.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useAll, useList, useOne } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import { RES_STATUS, ROOM_STATUS, STAY_LABEL, rp, type Availability, type Reservation, type Room, type RoomType } from "./hotel-api";

const fmtD = (s: string) => new Date(s).toLocaleDateString("id-ID", { day: "2-digit", month: "short", year: "numeric" });

export default function HotelReservationsPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const { propertyId, can } = useAuth();
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const list = useList<Reservation>("hotel/reservations", { property_id: propertyId ?? undefined, status: status || undefined, q: q || undefined }, { enabled: !!propertyId });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<Reservation, unknown>[]>(() => [
    { id: "number", header: "Reservasi", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.reservation_number}</span>, size: 150 },
    { id: "guest", header: "Tamu", cell: ({ row }) => <div><div className="font-medium">{row.original.guest_name}</div><div className="text-xs text-muted-foreground">{row.original.adults} dewasa{row.original.children ? `, ${row.original.children} anak` : ""} · {row.original.source}</div></div> },
    { id: "room", header: "Tipe / Kamar", cell: ({ row }) => <div><div className="text-sm">{row.original.room_type_name}</div><div className="text-xs text-muted-foreground">{row.original.room_number ? `Kamar ${row.original.room_number}` : "belum ditetapkan"}{row.original.room_status ? ` · ${ROOM_STATUS[row.original.room_status]?.label}` : ""}</div></div>, size: 190 },
    { id: "stay", header: "Menginap", cell: ({ row }) => <div className="text-sm">{fmtD(row.original.check_in_date)} → {fmtD(row.original.check_out_date)}<div className="text-xs text-muted-foreground">{row.original.nights} malam · {STAY_LABEL[row.original.stay_status] ?? row.original.stay_status}</div></div>, size: 220 },
    { id: "total", header: "Total", cell: ({ row }) => <span className="tnum font-semibold">{rp(row.original.total_amount)}</span>, size: 140 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={RES_STATUS[row.original.status]?.tone ?? "neutral"}>{RES_STATUS[row.original.status]?.label ?? row.original.status}</Badge>, size: 130 },
  ], [t]);
  if (!propertyId) return <Alert variant="info">Pilih property (profile Hotel) di header.</Alert>;
  return (
    <div>
      <PageHeader title="Hotel Reservations" subtitle="Reservasi kamar milik property (tanpa OTA/channel manager). Konflik kamar dicegah oleh server." actions={<>{can("hotel.reservations.view") && <Link to="/commercial/hotel/calendar"><Button variant="secondary"><Icon name="calendar_month" size={16} /> Kalender</Button></Link>}{can("hotel.reservations.create") && <Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> Reservasi Baru</Button>}</>}>
        <div className="flex items-center gap-2">
          <Input className="w-64" placeholder="Cari nomor / tamu / kamar…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-44" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option>{Object.entries(RES_STATUS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}</NativeSelect>
        </div>
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/commercial/hotel/reservations/${r.id}`} loading={list.isLoading} isFiltered={!!q || !!status} empty={{ message: "Belum ada reservasi." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      {createOpen && <CreateReservationDialog propertyId={propertyId} onClose={() => setCreateOpen(false)} onCreated={(r) => nav(`/commercial/hotel/reservations/${r.id}`)} />}
      {id && <ReservationDrawer id={id} onClose={() => nav("/commercial/hotel/reservations")} />}
    </div>
  );
}

export function ReservationDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const toast = useToast();
  const res = useOne<Reservation>("hotel/reservations", id);
  const act = useAction<{ id: string; action: string; body?: Record<string, unknown> }, { reservation: Reservation; temporary_password?: string; guest_account_email?: string }>((i) => `hotel/reservations/${i.id}/${i.action}`, { body: (i) => i.body ?? {}, invalidate: ["list", "one", "all", "hotel"] });
  const [dialog, setDialog] = useState<"assign" | "check_in" | "cancel" | null>(null);
  const [roomId, setRoomId] = useState("");
  const [reason, setReason] = useState("");
  const [guestAccount, setGuestAccount] = useState(false);
  const [guestEmail, setGuestEmail] = useState("");
  const [cred, setCred] = useState<{ email: string; password: string } | null>(null);
  const r = res.data;
  const avail = useQuery({ queryKey: ["hotel-avail", r?.room_type_id, r?.check_in_date, r?.check_out_date], enabled: !!r && dialog !== null && dialog !== "cancel", queryFn: () => api<{ data: Availability[] }>("hotel/availability", { query: { property_id: r!.property_id, room_type_id: r!.room_type_id, check_in: r!.check_in_date.slice(0, 10), check_out: r!.check_out_date.slice(0, 10) } }).then((x) => x.data[0]) });
  const rooms = useAll<Room>("hotel/rooms", { property_id: r?.property_id, room_type_id: r?.room_type_id }, { enabled: !!r && dialog !== null && dialog !== "cancel" });
  const freeRooms = (rooms.data ?? []).filter((x) => (avail.data?.free_room_ids ?? []).includes(x.location_id) || x.location_id === r?.room_location_id);
  const run = (action: string, body?: Record<string, unknown>) => act.mutateAsync({ id, action, body }).then((x) => {
    if (x.temporary_password) setCred({ email: x.guest_account_email ?? "", password: x.temporary_password });
    toast.success(`${x.reservation.reservation_number}: ${RES_STATUS[x.reservation.status]?.label}`);
    setDialog(null);
    setReason("");
  }).catch(toast.error);
  return (
    <Drawer open onClose={onClose} title="Reservasi" width={720}>
      <AsyncState query={res}>
        {(x) => (
          <div className="space-y-5">
            <div className="flex items-start justify-between gap-3">
              <div><div className="font-mono text-lg font-semibold">{x.reservation_number}</div><div className="text-sm text-muted-foreground">{x.guest_name} · {x.room_type_name}{x.room_number ? ` · Kamar ${x.room_number}` : ""}</div></div>
              <div className="flex items-center gap-2"><Badge tone={RES_STATUS[x.status]?.tone ?? "neutral"}>{RES_STATUS[x.status]?.label}</Badge><Badge tone="neutral">{STAY_LABEL[x.stay_status] ?? x.stay_status}</Badge></div>
            </div>
            {cred && <Alert variant="success" title="Akun Guest App dibuat (tampil sekali)">Email <code>{cred.email}</code> · Password sementara <code className="font-mono text-base">{cred.password}</code> — sampaikan ke tamu; akses berakhir saat check-out.</Alert>}
            <div className="flex flex-wrap gap-2">
              {x.allowed_actions.includes("confirm") && <Button size="sm" onClick={() => run("confirm")} loading={act.isPending}>Konfirmasi</Button>}
              {x.allowed_actions.includes("assign_room") && <Button size="sm" variant="secondary" onClick={() => { setRoomId(x.room_location_id ?? ""); setDialog("assign"); }}>{x.status === "checked_in" ? "Pindah kamar…" : "Tetapkan kamar…"}</Button>}
              {x.allowed_actions.includes("check_in") && <Button size="sm" onClick={() => { setRoomId(x.room_location_id ?? ""); setGuestEmail(x.guest_email ?? ""); setDialog("check_in"); }} icon="login">Check-in…</Button>}
              {x.allowed_actions.includes("check_out") && <Button size="sm" onClick={() => run("check_out")} loading={act.isPending} icon="logout">Check-out</Button>}
              {x.allowed_actions.includes("no_show") && <Button size="sm" variant="secondary" onClick={() => run("no_show")}>No show</Button>}
              {x.allowed_actions.includes("cancel") && <Button size="sm" variant="ghost" onClick={() => setDialog("cancel")}>Batalkan…</Button>}
            </div>
            <KeyValue items={[
              { label: "Check-in", value: fmtD(x.check_in_date) }, { label: "Check-out", value: fmtD(x.check_out_date) }, { label: "Malam", value: String(x.nights) },
              { label: "Tarif / malam", value: rp(x.rate_per_night) }, { label: "Total", value: rp(x.total_amount) }, { label: "Sumber", value: x.source },
              { label: "Tamu", value: `${x.adults} dewasa${x.children ? `, ${x.children} anak` : ""}` }, { label: "Kontak", value: `${x.guest_phone ?? "—"} · ${x.guest_email ?? "—"}` },
              { label: "Permintaan khusus", value: x.special_requests ?? "—" }, { label: "Catatan internal", value: x.notes ?? "—" },
              { label: "Invoice", value: x.invoice_id ? <Link to={`/billing/invoices/${x.invoice_id}`} className="text-primary hover:underline">{x.invoice_number}</Link> : "—" },
              { label: "Guest request terbuka", value: String(x.open_requests) },
              { label: "Checked in", value: x.checked_in_at ? fmtDateTime(x.checked_in_at) : "—" }, { label: "Checked out", value: x.checked_out_at ? fmtDateTime(x.checked_out_at) : "—" },
              { label: "Dibuat", value: <><RelativeTime value={x.created_at} /> · {x.created_by_name ?? "—"}</> },
            ]} />
            {x.guests.length > 0 && <div><div className="mb-1 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Daftar tamu</div><ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">{x.guests.map((g, i) => <li key={g.id ?? i} className="px-3 py-1.5">{g.full_name}{g.is_primary ? " (utama)" : ""} {g.id_type ? `· ${g.id_type} ${g.id_number_masked ?? ""}` : ""} {g.nationality ?? ""}</li>)}</ul></div>}
            {x.room_location_id && x.status === "checked_in" && can2(x) && <Link to={`/tenant-relation/service-requests`} className="text-sm text-primary hover:underline">Guest request pada kamar ini → Service Requests</Link>}
            {(dialog === "assign" || dialog === "check_in") && (
              <Dialog open onOpenChange={(o) => !o && setDialog(null)}>
                <DialogContent title={dialog === "assign" ? "Tetapkan kamar" : "Check-in tamu"} description={dialog === "check_in" ? "Kamar harus tersedia (bukan Occupied/OOO). Opsional: buat akun Guest App agar tamu dapat mengirim guest request dari Tenant App." : "Hanya kamar tipe yang sama dan bebas sepanjang menginap."}>
                  <div className="space-y-3">
                    <Field label="Kamar" required={dialog === "check_in"}><NativeSelect value={roomId} onChange={(e) => setRoomId(e.target.value)}><option value="">— pilih kamar —</option>{freeRooms.map((rm) => <option key={rm.location_id} value={rm.location_id}>{rm.room_number} · {ROOM_STATUS[rm.room_status]?.label}{rm.floor_name ? ` · ${rm.floor_name}` : ""}</option>)}</NativeSelect></Field>
                    {avail.data && <p className="text-xs text-muted-foreground">{avail.data.available_rooms} dari {avail.data.total_rooms} kamar {avail.data.room_type_name} tersedia untuk tanggal ini.</p>}
                    {dialog === "check_in" && <>
                      <Checkbox label="Buat akun Guest App (Tenant App profile Hotel)" checked={guestAccount} onCheckedChange={setGuestAccount} />
                      {guestAccount && <Field label="Email tamu" required><Input type="email" value={guestEmail} onChange={(e) => setGuestEmail(e.target.value)} /></Field>}
                    </>}
                  </div>
                  <DialogFooter><Button variant="secondary" onClick={() => setDialog(null)}>Batal</Button><Button loading={act.isPending} disabled={dialog === "assign" ? !roomId : (!roomId && !x.room_location_id) || (guestAccount && !guestEmail.trim())} onClick={() => run(dialog === "assign" ? "assign_room" : "check_in", dialog === "assign" ? { room_location_id: roomId } : { room_location_id: roomId || undefined, create_guest_account: guestAccount, guest_email: guestAccount ? guestEmail.trim() : undefined })}>{dialog === "assign" ? "Tetapkan" : "Check-in"}</Button></DialogFooter>
                </DialogContent>
              </Dialog>
            )}
            {dialog === "cancel" && (
              <Dialog open onOpenChange={(o) => !o && setDialog(null)}>
                <DialogContent title={`Batalkan ${x.reservation_number}`}><Field label="Alasan" required><Textarea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} /></Field>
                  <DialogFooter><Button variant="secondary" onClick={() => setDialog(null)}>Batal</Button><Button variant="destructive" disabled={!reason.trim()} loading={act.isPending} onClick={() => run("cancel", { reason: reason.trim() })}>Batalkan reservasi</Button></DialogFooter>
                </DialogContent>
              </Dialog>
            )}
          </div>
        )}
      </AsyncState>
    </Drawer>
  );
}
const can2 = (_x: Reservation) => true;

export function CreateReservationDialog({ propertyId, onClose, onCreated, defaults }: { propertyId: string; onClose: () => void; onCreated?: (r: Reservation) => void; defaults?: Partial<{ check_in_date: string; check_out_date: string }> }) {
  const toast = useToast();
  const today = new Date().toISOString().slice(0, 10);
  const [f, setF] = useState({ check_in_date: defaults?.check_in_date ?? today, check_out_date: defaults?.check_out_date ?? new Date(Date.now() + 86400000).toISOString().slice(0, 10), room_type_id: "", room_location_id: "", guest_name: "", guest_phone: "", guest_email: "", adults: "1", children: "0", source: "walk_in", special_requests: "", notes: "", confirm: true });
  const types = useAll<RoomType>("hotel/room-types", { property_id: propertyId });
  const validDates = !!f.check_in_date && !!f.check_out_date && f.check_out_date > f.check_in_date;
  const avail = useQuery({ queryKey: ["hotel-avail-all", propertyId, f.check_in_date, f.check_out_date], enabled: validDates, queryFn: () => api<{ data: Availability[] }>("hotel/availability", { query: { property_id: propertyId, check_in: f.check_in_date, check_out: f.check_out_date } }).then((x) => x.data) });
  const rooms = useAll<Room>("hotel/rooms", { property_id: propertyId, room_type_id: f.room_type_id }, { enabled: !!f.room_type_id });
  const sel = avail.data?.find((a) => a.room_type_id === f.room_type_id);
  const create = useAction<Record<string, unknown>, Reservation>(() => "hotel/reservations", { invalidate: ["list", "one", "hotel"] });
  const submit = () => create.mutateAsync({ property_id: propertyId, room_type_id: f.room_type_id, room_location_id: f.room_location_id || null, guest_name: f.guest_name.trim(), guest_phone: f.guest_phone || null, guest_email: f.guest_email || null, adults: Number(f.adults) || 1, children: Number(f.children) || 0, check_in_date: f.check_in_date, check_out_date: f.check_out_date, source: f.source, special_requests: f.special_requests || null, notes: f.notes || null, confirm: f.confirm })
    .then((r) => { toast.success(`Reservasi ${r.reservation_number} dibuat`); onClose(); onCreated?.(r); }).catch(toast.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Reservasi Baru" description="Select Dates → Room Type → Availability → Rate → Guest Details → Reservation (WF-P1-007).">
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Check-in" required><Input type="date" value={f.check_in_date} onChange={(e) => setF({ ...f, check_in_date: e.target.value })} /></Field>
            <Field label="Check-out" required><Input type="date" value={f.check_out_date} onChange={(e) => setF({ ...f, check_out_date: e.target.value })} /></Field>
          </div>
          <Field label="Tipe kamar" required>
            <div className="grid grid-cols-1 gap-2">
              {(types.data ?? []).filter((x) => x.status === "active").map((x) => {
                const a = avail.data?.find((y) => y.room_type_id === x.id);
                const selected = f.room_type_id === x.id;
                return (
                  <button key={x.id} type="button" disabled={!!a && a.available_rooms === 0} onClick={() => setF({ ...f, room_type_id: x.id, room_location_id: "" })} className={`flex items-center justify-between rounded-[var(--radius-md)] border p-3 text-left disabled:opacity-50 ${selected ? "border-primary bg-primary-soft" : "border-border hover:bg-surface-container"}`}>
                    <span><span className="font-semibold">{x.name}</span> <span className="text-xs text-muted-foreground">{x.capacity_adults} dewasa · {x.room_count} kamar</span></span>
                    <span className="text-right text-sm">{a ? <><span className="tnum font-semibold">{rp(a.total_amount)}</span><div className="text-xs text-muted-foreground">{a.available_rooms} tersedia · {rp(a.rate_per_night)}/malam</div></> : <span className="text-xs text-muted-foreground">pilih tanggal</span>}</span>
                  </button>
                );
              })}
            </div>
          </Field>
          {f.room_type_id && <Field label="Kamar (opsional; bisa ditetapkan saat check-in)"><NativeSelect value={f.room_location_id} onChange={(e) => setF({ ...f, room_location_id: e.target.value })}><option value="">— belum ditetapkan —</option>{(rooms.data ?? []).filter((rm) => (sel?.free_room_ids ?? []).includes(rm.location_id)).map((rm) => <option key={rm.location_id} value={rm.location_id}>{rm.room_number} · {ROOM_STATUS[rm.room_status]?.label}</option>)}</NativeSelect></Field>}
          <div className="grid grid-cols-2 gap-3">
            <Field label="Nama tamu" required className="col-span-2"><Input value={f.guest_name} onChange={(e) => setF({ ...f, guest_name: e.target.value })} /></Field>
            <Field label="Telepon"><Input value={f.guest_phone} onChange={(e) => setF({ ...f, guest_phone: e.target.value })} /></Field>
            <Field label="Email"><Input type="email" value={f.guest_email} onChange={(e) => setF({ ...f, guest_email: e.target.value })} /></Field>
            <Field label="Dewasa"><Input type="number" min={1} value={f.adults} onChange={(e) => setF({ ...f, adults: e.target.value })} /></Field>
            <Field label="Anak"><Input type="number" min={0} value={f.children} onChange={(e) => setF({ ...f, children: e.target.value })} /></Field>
            <Field label="Sumber"><NativeSelect value={f.source} onChange={(e) => setF({ ...f, source: e.target.value })}>{["walk_in", "phone", "email", "website", "corporate", "other"].map((s) => <option key={s} value={s}>{s}</option>)}</NativeSelect></Field>
          </div>
          <Field label="Permintaan khusus"><Textarea rows={2} value={f.special_requests} onChange={(e) => setF({ ...f, special_requests: e.target.value })} /></Field>
          <Checkbox label="Langsung Confirmed" checked={f.confirm} onCheckedChange={(v) => setF({ ...f, confirm: v })} />
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={create.isPending} disabled={!validDates || !f.room_type_id || !f.guest_name.trim()} onClick={submit}>Buat Reservasi</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
