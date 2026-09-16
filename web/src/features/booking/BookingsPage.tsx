// Booking › Bookings (PRD P1 v1.3 §21, WF-P1-004): daftar booking fasilitas, approval (OD-P1-007), check-in/complete,
// pembuatan booking oleh staf atas nama tenant; konflik slot dicegah server (409 BOOKING_CONFLICT).
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useAll, useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import type { Tenant } from "@/api/types";
import { FACILITY_TYPES, type Facility } from "./FacilitiesPage";

export interface Booking {
  id: string; booking_number: string; property_id: string; facility_id: string; facility_name: string; facility_type: string; tenant_user_id: string | null; tenant_id: string | null; tenant_name: string | null;
  requester_name: string | null; requester_phone: string | null; starts_at: string; ends_at: string; attendees: number | null; purpose: string | null; notes: string | null; status: string; channel: string;
  approved_at: string | null; approved_by_name: string | null; rejection_reason: string | null; cancelled_at: string | null; cancel_reason: string | null; checked_in_at: string | null; completed_at: string | null;
  created_at: string; created_by_name: string | null; allowed_actions: string[]; version: number;
}
export const BOOKING_STATUS: Record<string, { label: string; tone: "warning" | "success" | "error" | "neutral" | "info" | "primary" }> = {
  pending: { label: "Menunggu approval", tone: "warning" }, confirmed: { label: "Confirmed", tone: "success" }, rejected: { label: "Ditolak", tone: "error" }, cancelled: { label: "Dibatalkan", tone: "neutral" },
  checked_in: { label: "Checked in", tone: "primary" }, completed: { label: "Selesai", tone: "success" }, no_show: { label: "No show", tone: "neutral" },
};
const ACTION_LABEL: Record<string, string> = { approve: "Setujui", reject: "Tolak…", cancel: "Batalkan…", check_in: "Check-in", complete: "Selesai", no_show: "No show" };

export default function BookingsPage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const toast = useToast();
  const [status, setStatus] = useState("");
  const [upcoming, setUpcoming] = useState(true);
  const [q, setQ] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [reasonFor, setReasonFor] = useState<{ b: Booking; action: string } | null>(null);
  const [reason, setReason] = useState("");
  const list = useList<Booking>("bookings", { property_id: propertyId ?? undefined, status: status || undefined, upcoming: upcoming || undefined, q: q || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const act = useAction<{ id: string; action: string; reason?: string }, Booking>((i) => `bookings/${i.id}/${i.action}`, { body: (i) => ({ reason: i.reason }), invalidate: ["list", "all"] });
  const run = (b: Booking, action: string) => {
    if (action === "reject" || action === "cancel") return setReasonFor({ b, action });
    act.mutateAsync({ id: b.id, action }).then(() => toast.success(`Booking ${b.booking_number}: ${ACTION_LABEL[action]}`)).catch(toast.error);
  };
  const columns = useMemo<ColumnDef<Booking, unknown>[]>(() => [
    { id: "number", header: "Booking", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.booking_number}</span>, size: 150 },
    { id: "facility", header: "Fasilitas", cell: ({ row }) => <div><div className="font-medium">{row.original.facility_name}</div><div className="text-xs text-muted-foreground">{FACILITY_TYPES[row.original.facility_type] ?? row.original.facility_type}</div></div> },
    { id: "time", header: "Waktu", cell: ({ row }) => <span className="text-sm">{fmtDateTime(row.original.starts_at)} → {new Date(row.original.ends_at).toLocaleTimeString("id-ID", { hour: "2-digit", minute: "2-digit" })}</span>, size: 220 },
    { id: "requester", header: "Pemesan", cell: ({ row }) => <div><div className="text-sm">{row.original.requester_name ?? "—"}</div><div className="text-xs text-muted-foreground">{row.original.tenant_name ?? ""}{row.original.channel === "tenant_app" ? " · Tenant App" : ""}</div></div> },
    { id: "purpose", header: "Keperluan", cell: ({ row }) => <span className="text-sm">{row.original.purpose ?? "—"}{row.original.attendees ? ` · ${row.original.attendees} org` : ""}</span> },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={BOOKING_STATUS[row.original.status]?.tone ?? "neutral"}>{BOOKING_STATUS[row.original.status]?.label ?? row.original.status}</Badge>, size: 150 },
    { id: "created_at", header: "Dibuat", cell: ({ row }) => <span className="text-xs text-muted-foreground"><RelativeTime value={row.original.created_at} /></span>, size: 110 },
  ], [t]);
  return (
    <div>
      <PageHeader title="Bookings" subtitle="Reservasi fasilitas oleh tenant/staf. Slot ganda dicegah oleh server." actions={can("booking.bookings.create") && <Button onClick={() => setCreateOpen(true)} disabled={!propertyId}><Icon name="add" size={16} /> Buat Booking</Button>}>
        <div className="flex items-center gap-2">
          <Input className="w-64" placeholder="Cari nomor / pemesan / fasilitas…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-48" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option>{Object.entries(BOOKING_STATUS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}</NativeSelect>
          <NativeSelect className="w-40" value={upcoming ? "upcoming" : "all"} onChange={(e) => setUpcoming(e.target.value === "upcoming")}><option value="upcoming">Mendatang</option><option value="all">Semua waktu</option></NativeSelect>
        </div>
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} loading={list.isLoading} isFiltered={!!q || !!status} empty={{ message: "Belum ada booking." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage}
        rowActions={(r) => r.allowed_actions.filter((a) => a !== "view").map((a) => ({ label: ACTION_LABEL[a] ?? a, destructive: a === "reject" || a === "cancel", onSelect: () => run(r, a) }))} />
      {createOpen && propertyId && <CreateBookingDialog propertyId={propertyId} onClose={() => setCreateOpen(false)} />}
      {reasonFor && (
        <Dialog open onOpenChange={(o) => !o && setReasonFor(null)}>
          <DialogContent title={`${ACTION_LABEL[reasonFor.action].replace("…", "")} ${reasonFor.b.booking_number}`}>
            <Field label="Alasan" required><Textarea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} /></Field>
            <DialogFooter><Button variant="secondary" onClick={() => setReasonFor(null)}>Batal</Button><Button variant="destructive" disabled={!reason.trim()} loading={act.isPending} onClick={() => act.mutateAsync({ id: reasonFor.b.id, action: reasonFor.action, reason: reason.trim() }).then(() => { setReasonFor(null); setReason(""); toast.success("Tersimpan"); }).catch(toast.error)}>Konfirmasi</Button></DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function CreateBookingDialog({ propertyId, onClose }: { propertyId: string; onClose: () => void }) {
  const toast = useToast();
  const facilities = useAll<Facility>("facilities", { property_id: propertyId, active: true });
  const tenants = useAll<Tenant>("tenants", { property_id: propertyId });
  const [f, setF] = useState({ facility_id: "", date: "", start: "09:00", end: "10:00", attendees: "", purpose: "", tenant_id: "", requester_name: "", requester_phone: "" });
  const create = useAction<Record<string, unknown>, Booking>(() => "bookings", { invalidate: ["list", "all"] });
  const fac = facilities.data?.find((x) => x.id === f.facility_id);
  const submit = () => {
    const starts_at = new Date(`${f.date}T${f.start}:00`).toISOString();
    const ends_at = new Date(`${f.date}T${f.end}:00`).toISOString();
    create.mutateAsync({ facility_id: f.facility_id, starts_at, ends_at, attendees: f.attendees ? Number(f.attendees) : null, purpose: f.purpose || null, tenant_id: f.tenant_id || null, requester_name: f.requester_name || null, requester_phone: f.requester_phone || null })
      .then((b) => { toast.success(`Booking ${b.booking_number} dibuat (${BOOKING_STATUS[b.status]?.label})`); onClose(); }).catch(toast.error);
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Buat Booking (atas nama tenant)">
        <div className="space-y-4">
          <Field label="Fasilitas" required><NativeSelect value={f.facility_id} onChange={(e) => setF({ ...f, facility_id: e.target.value })}><option value="">— pilih —</option>{(facilities.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.name} ({x.open_time}–{x.close_time})</option>)}</NativeSelect></Field>
          {fac && <p className="text-xs text-muted-foreground">Slot {fac.slot_minutes} mnt · durasi {fac.min_duration_minutes}–{fac.max_duration_minutes} mnt · kapasitas {fac.capacity ?? "—"} · {fac.effective_approval ? "perlu approval" : "konfirmasi otomatis"}</p>}
          <div className="grid grid-cols-3 gap-3">
            <Field label="Tanggal" required><Input type="date" value={f.date} onChange={(e) => setF({ ...f, date: e.target.value })} /></Field>
            <Field label="Mulai" required><Input type="time" value={f.start} onChange={(e) => setF({ ...f, start: e.target.value })} /></Field>
            <Field label="Selesai" required><Input type="time" value={f.end} onChange={(e) => setF({ ...f, end: e.target.value })} /></Field>
          </div>
          <Field label="Tenant"><NativeSelect value={f.tenant_id} onChange={(e) => setF({ ...f, tenant_id: e.target.value })}><option value="">— tanpa tenant —</option>{(tenants.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}</NativeSelect></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Nama pemesan"><Input value={f.requester_name} onChange={(e) => setF({ ...f, requester_name: e.target.value })} /></Field>
            <Field label="Telepon"><Input value={f.requester_phone} onChange={(e) => setF({ ...f, requester_phone: e.target.value })} /></Field>
            <Field label="Jumlah orang"><Input type="number" value={f.attendees} onChange={(e) => setF({ ...f, attendees: e.target.value })} /></Field>
            <Field label="Keperluan"><Input value={f.purpose} onChange={(e) => setF({ ...f, purpose: e.target.value })} /></Field>
          </div>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={create.isPending} disabled={!f.facility_id || !f.date} onClick={submit}>Buat Booking</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
