// Security › Emergency (PRD P2 v2.1 §6.4; NC §16): Panic Button (Staff App) atau laporan web → Emergency Alert →
// Security Response → Incident otomatis. Emergency aktif tampil paling atas (diperbarui tiap 15 detik), lalu riwayat
// dengan filter status & tanggal (cursor). Tab Kontak Darurat: kontak per property (security.emergency_contacts.*).
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button, Card, ConfirmDialog, DatePicker, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Tabs, TabsContent, TabsList, TabsTrigger, Textarea } from "@/components/ui/primitives";
import { DataGrid, useUrlFilters } from "@/components/bv/datagrid";
import { CardSkeleton, EmptyState, QueryErrorState } from "@/components/bv/states";
import { LocationPath, RelativeTime, useToast } from "@/components/bv/common";
import { LocationPicker } from "@/components/bv/pickers";
import { Fab } from "@/components/bv/mobile";
import { useAction, useAll, useInvalidate, useList } from "@/api/hooks";
import { api, uuid, type ListResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import type { EmergencyAlert, EmergencyContact } from "./types";
import { CONTACT_TYPE_ICON, CONTACT_TYPES, EMERGENCY_ACTION_LABEL, EMERGENCY_CHANNELS, EMERGENCY_REFRESH_MS, EMERGENCY_TYPE_ICON, EMERGENCY_TYPES, fmtSeconds, labelOf, nextDay, optionsOf, secondsSince, telHref } from "./p2";
import { EscalationBadge, FilterRow, PropertyField, SelectFilter } from "./shared";
import { StatusBadge } from "@/components/bv/badges";
import { CellLocation, CellText, CellTitle } from "@/components/bv/cells";
import { statusOptions } from "@/lib/status";
import { TOUCH, useNow, usePropertyChoice } from "./hooks";

export default function EmergencyPage({ tab = "alerts" }: { tab?: "alerts" | "contacts" }) {
  const nav = useNavigate();
  const { can } = useAuth();
  const [raiseOpen, setRaiseOpen] = useState(false);
  const canRaise = can("security.emergency_alerts.raise");
  // daftar kontak per property mensyaratkan security.emergency_contacts.view (manage tanpa view ditolak server)
  const canContacts = can("security.emergency_contacts.view");
  const alerts = <AlertsTab onRaise={canRaise ? () => setRaiseOpen(true) : undefined} />;
  return (
    <div>
      <PageHeader
        title="Emergency"
        subtitle="Panic Button dari Staff App atau laporan web → Emergency Alert → Security Response → Incident otomatis."
        actions={canRaise && <span className="hidden md:inline-flex"><Button variant="destructive" icon="e911_emergency" onClick={() => setRaiseOpen(true)}>Laporkan Emergency</Button></span>}
      />
      {canContacts ? (
        <Tabs value={tab} onValueChange={(v) => nav(v === "contacts" ? "/security/emergency/contacts" : "/security/emergency")}>
          <TabsList>
            <TabsTrigger value="alerts">Emergency Alert</TabsTrigger>
            <TabsTrigger value="contacts">Kontak Darurat</TabsTrigger>
          </TabsList>
          <TabsContent value="alerts">{alerts}</TabsContent>
          <TabsContent value="contacts"><ContactsTab /></TabsContent>
        </Tabs>
      ) : (
        alerts
      )}
      {canRaise && <Fab label="Laporkan" icon="e911_emergency" aria-label="Laporkan Emergency" onClick={() => setRaiseOpen(true)} />}
      {raiseOpen && <RaiseEmergencyDialog onClose={() => setRaiseOpen(false)} onRaised={(a) => nav(`/security/emergency/${a.id}`)} />}
    </div>
  );
}

// Filter "Aktif" = drill-down Security Dashboard (status=raised,acknowledged,responding; server menerima CSV).
const ACTIVE_STATUSES = "raised,acknowledged,responding";

// ---------- Tab Emergency Alert: aktif + riwayat ----------
function AlertsTab({ onRaise }: { onRaise?: () => void }) {
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  const status = f.get("status");
  const from = f.get("from");
  const to = f.get("to");
  // pelapor tanpa .view hanya boleh melihat alert miliknya: server menolak property_id tanpa mine=true (403)
  const mine = can("security.emergency_alerts.view") ? undefined : true;
  const active = useQuery({
    queryKey: ["list", "emergency-alerts", "active", propertyId, mine],
    queryFn: ({ signal }) => api<ListResponse<EmergencyAlert>>("emergency-alerts", { query: { property_id: propertyId ?? undefined, mine, active: true, limit: 100 }, signal }).then((r) => r.data),
    refetchInterval: EMERGENCY_REFRESH_MS,
    staleTime: 5_000,
  });
  const history = useList<EmergencyAlert>("emergency-alerts", { property_id: propertyId ?? undefined, mine, status: status || undefined, from: from || undefined, to: to ? nextDay(to) : undefined });
  const rows = history.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<EmergencyAlert, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026): nomor + jenis emergency, lokasi = nama terakhir, pelapor · waktu satu baris,
      // satu status + eskalasi. Kanal, GPS & timeline ada di halaman detail.
      { id: "alert", header: "Emergency", meta: { mobile: "primary" }, cell: ({ row: { original: a } }) => <CellTitle code={a.alert_number} title={a.emergency_type_label || labelOf(EMERGENCY_TYPES, a.emergency_type)} /> },
      { id: "location", header: "Lokasi", meta: { mobile: "secondary" }, cell: ({ row }) => <CellLocation path={row.original.location.path_text} /> },
      { id: "raised_at", header: "Dilaporkan", meta: { mobile: "secondary" }, size: 170, cell: ({ row: { original: a } }) => <CellText max={180} title={a.raised_by_name ?? undefined}>{a.raised_by_name ?? "—"} · <RelativeTime value={a.raised_at} className="text-on-surface-variant" /></CellText> },
      { id: "status", header: "Status", meta: { mobile: "status" }, cell: ({ row: { original: a } }) => <div className="flex items-center gap-1 whitespace-nowrap"><StatusBadge objectType="emergency_alert" status={a.status} /><EscalationBadge level={a.escalation_level} /></div> },
      { id: "response", header: "Respons", meta: { mobile: "secondary" }, size: 130, cell: ({ row: { original: a } }) => (a.ack_seconds !== null ? <span className="tnum whitespace-nowrap">Respons {fmtSeconds(a.ack_seconds)}</span> : a.status === "raised" ? <span className="whitespace-nowrap text-on-error-container">Belum direspons</span> : <span className="text-on-surface-variant">—</span>) },
      { id: "incident", header: "Incident", meta: { mobile: "hidden" }, size: 150, cell: ({ row: { original: a } }) => (a.incident_id ? <Link to={`/operations/incidents/${a.incident_id}`} onClick={(e) => e.stopPropagation()} className="whitespace-nowrap font-mono text-[13px] text-primary hover:underline">{a.incident_number ?? "Incident"}</Link> : "—") },
    ],
    [],
  );
  return (
    <div className="space-y-6">
      <ActiveAlerts query={active} onRaise={onRaise} />
      <section aria-labelledby="emg-history">
        <h2 id="emg-history" className="mb-2 text-h3 font-bold text-on-surface">Riwayat Emergency</h2>
        <FilterRow>
          <SelectFilter label="Status" value={status} onChange={(v) => f.set({ status: v })} options={[{ value: ACTIVE_STATUSES, label: "Aktif (belum selesai)" }, ...statusOptions("emergency_alert")]} />
          <span className="inline-flex w-full items-center gap-1 sm:w-auto">
            <DatePicker className="min-w-0 flex-1 sm:w-40 sm:flex-none" value={from} onChange={(v) => f.set({ from: v })} aria-label="Dari tanggal" />
            <span className="text-on-surface-variant">–</span>
            <DatePicker className="min-w-0 flex-1 sm:w-40 sm:flex-none" value={to} onChange={(v) => f.set({ to: v })} aria-label="Sampai tanggal" />
          </span>
          {f.isFiltered && <Button variant="ghost" size="sm" icon="replay" onClick={f.reset}>Reset filter</Button>}
        </FilterRow>
        <DataGrid
          columns={columns}
          rows={rows}
          rowId={(r) => r.id}
          onRowClick={(r) => `/security/emergency/${r.id}`}
          loading={history.isLoading}
          error={history.error}
          onRetry={() => history.refetch()}
          isFiltered={f.isFiltered}
          empty={{ icon: "e911_emergency", title: "Belum ada Emergency Alert", description: "Emergency dari Panic Button Staff App atau laporan web akan tercatat di sini beserta waktu responsnya." }}
          hasMore={history.hasNextPage}
          onLoadMore={() => history.fetchNextPage()}
          loadingMore={history.isFetchingNextPage}
          rowClassName={(r) => (r.active ? "border-l-4 border-l-critical" : undefined)}
        />
      </section>
    </div>
  );
}

function ActiveAlerts({ query, onRaise }: { query: UseQueryResult<EmergencyAlert[]>; onRaise?: () => void }) {
  const items = query.data ?? [];
  return (
    <section aria-labelledby="emg-active">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <h2 id="emg-active" className="inline-flex items-center gap-2 text-h3 font-bold text-on-surface">
          <Icon name="e911_emergency" size={20} className={items.length ? "text-error" : "text-on-surface-variant"} aria-hidden />
          Emergency Aktif
          {items.length > 0 && <Badge tone="error">{items.length}</Badge>}
        </h2>
        <span className="text-xs text-on-surface-variant">Diperbarui otomatis tiap 15 detik{query.isFetching && !query.isLoading ? " · memuat…" : ""}</span>
      </div>
      {query.isLoading ? (
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2"><CardSkeleton /><CardSkeleton /></div>
      ) : query.isError && !query.data ? (
        <QueryErrorState error={query.error} onRetry={() => query.refetch()} compact />
      ) : items.length === 0 ? (
        <Card>
          <EmptyState compact title="Tidak ada Emergency aktif" description="Semua Emergency Alert sudah ditangani. Panic Button dari Staff App akan langsung muncul di sini." action={onRaise ? <Button variant="secondary" icon="e911_emergency" className={TOUCH} onClick={onRaise}>Laporkan Emergency</Button> : undefined} />
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">{items.map((a) => <ActiveAlertCard key={a.id} alert={a} />)}</div>
      )}
    </section>
  );
}

const QUICK_ACTIONS = ["acknowledge", "respond"];

function ActiveAlertCard({ alert: a }: { alert: EmergencyAlert }) {
  const nav = useNavigate();
  const toast = useToast();
  const act = useAction<{ action: string }, EmergencyAlert>((i) => `emergency-alerts/${a.id}/${i.action}`, { body: () => ({}) });
  const quick = QUICK_ACTIONS.filter((x) => a.allowed_actions.includes(x));
  const now = useNow(a.acknowledged_at ? 0 : 5_000); // detik "menunggu respons" berjalan sampai diterima
  const waiting = secondsSince(a.raised_at, now);
  const run = (action: string) =>
    act
      .mutateAsync({ action })
      .then(() => (action === "respond" ? toast.success(`${a.alert_number}: security menangani di lokasi`) : toast.transition(action, a.alert_number)))
      .catch((e) => toast.failed("updated", e, a.alert_number));
  return (
    <Card railTone={a.status === "raised" ? "error" : a.status === "acknowledged" ? "warning" : "info"} className="p-4">
      <div className="flex items-start gap-3">
        <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-error-container text-on-error-container" aria-hidden>
          <Icon name={EMERGENCY_TYPE_ICON[a.emergency_type] ?? "warning"} size={22} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Link to={`/security/emergency/${a.id}`} className="min-w-0 hover:underline">
              <span className="font-mono text-[13px] font-semibold">{a.alert_number}</span> <span className="text-body font-semibold">{a.emergency_type_label || labelOf(EMERGENCY_TYPES, a.emergency_type)}</span>
            </Link>
            <span className="flex flex-wrap gap-1"><StatusBadge objectType="emergency_alert" status={a.status} /><EscalationBadge level={a.escalation_level} /></span>
          </div>
          <div className="mt-1"><LocationPath pathText={a.location.path_text} /></div>
          <div className="mt-1 text-sm text-on-surface-variant">
            {a.raised_by_name ?? "—"} · <RelativeTime value={a.raised_at} /> · {labelOf(EMERGENCY_CHANNELS, a.channel)}
          </div>
          <div className={cn("mt-1 text-sm font-semibold tnum", a.acknowledged_at ? "text-on-surface" : "text-on-error-container")}>
            {a.acknowledged_at ? `${a.acknowledged_by_name ? `Diterima ${a.acknowledged_by_name}` : "Diterima"} · respons ${fmtSeconds(a.ack_seconds)}` : `Menunggu respons · ${fmtSeconds(waiting)}`}
            {a.responding_at && " · security di lokasi"}
          </div>
          {a.description && <p className="mt-1 line-clamp-2 text-sm">{a.description}</p>}
          <div className="mt-3 flex flex-wrap items-center gap-2">
            {quick.map((x, i) => (
              <Button key={x} size="md" variant={i === 0 ? "primary" : "secondary"} className={TOUCH} loading={act.isPending && act.variables?.action === x} onClick={() => run(x)}>
                {EMERGENCY_ACTION_LABEL[x]}
              </Button>
            ))}
            <Button size="md" variant="ghost" icon="chevron_right" className={TOUCH} onClick={() => nav(`/security/emergency/${a.id}`)}>Buka detail</Button>
            {a.incident_id && <Link to={`/operations/incidents/${a.incident_id}`} className="ml-auto text-sm text-primary hover:underline">Incident {a.incident_number}</Link>}
          </div>
        </div>
      </div>
    </Card>
  );
}

// ---------- Laporkan Emergency (web; idempoten lewat id klien) ----------
export function RaiseEmergencyDialog({ onClose, onRaised }: { onClose: () => void; onRaised?: (a: EmergencyAlert) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [clientId] = useState(() => uuid());
  const [pid, setPid] = usePropertyChoice();
  const [type, setType] = useState("");
  const [locationId, setLocationId] = useState<string | null>(null);
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!type) return;
    setBusy(true);
    try {
      // id klien = kunci idempoten (kirim ulang tidak membuat alert ganda)
      const a = await api<EmergencyAlert>("emergency-alerts", {
        body: { id: clientId, property_id: pid || undefined, emergency_type: type, location_id: locationId, description: description.trim() || null, channel: "web", client_raised_at: new Date().toISOString() },
        idempotencyKey: clientId,
      });
      invalidate("list", "overview", "notifications");
      toast.success(`Emergency ${a.alert_number} dilaporkan${a.incident_number ? ` · Incident ${a.incident_number} dibuat` : ""}`, { to: `/security/emergency/${a.id}`, label: "Buka" });
      onClose();
      onRaised?.(a);
    } catch (e) {
      toast.failed("created", e, "Emergency Alert");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Laporkan Emergency" description="Security on-duty, supervisor, dan manager property langsung menerima notifikasi. Incident kritis dibuat otomatis.">
        <div className="space-y-4">
          <PropertyField value={pid} onChange={(v) => { setPid(v); setLocationId(null); }} />
          <Field label="Jenis emergency" required>
            <NativeSelect value={type} onChange={(e) => setType(e.target.value)} autoFocus>
              <option value="">Pilih jenis emergency…</option>
              {optionsOf(EMERGENCY_TYPES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
            </NativeSelect>
          </Field>
          <Field label="Lokasi" help="Pilih lokasi sedetail mungkin agar security cepat tiba.">
            <LocationPicker propertyId={pid || null} value={locationId} onChange={(id) => setLocationId(id)} />
          </Field>
          <Field label="Keterangan">
            <Textarea rows={3} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Apa yang terjadi, jumlah korban, kondisi saat ini…" />
          </Field>
          <p className="text-xs text-on-surface-variant">Gunakan hanya untuk keadaan darurat nyata. Alarm palsu dapat dibatalkan security/supervisor dengan alasan.</p>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button variant="destructive" icon="e911_emergency" disabled={!type} loading={busy} onClick={submit}>Laporkan Emergency</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Tab Kontak Darurat (P2-EMG-07) ----------
function ContactsTab() {
  const { propertyId, properties, can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const canManage = can("security.emergency_contacts.manage");
  const list = useAll<EmergencyContact>("emergency-contacts", { property_id: propertyId ?? undefined });
  const [edit, setEdit] = useState<EmergencyContact | "new" | null>(null);
  const [del, setDel] = useState<EmergencyContact | null>(null);
  const setActive = (c: EmergencyContact) =>
    api(`emergency-contacts/${c.id}`, { method: "PATCH", body: { is_active: !c.is_active } })
      .then(() => { invalidate("all"); toast.action("updated", `Kontak ${c.name}`); })
      .catch(toast.error);
  const remove = (c: EmergencyContact) =>
    api(`emergency-contacts/${c.id}`, { method: "DELETE" })
      .then(() => { invalidate("all"); toast.action("deleted", `Kontak ${c.name}`); setDel(null); })
      .catch(toast.error);
  const columns = useMemo<ColumnDef<EmergencyContact, unknown>[]>(() => {
    const cols: ColumnDef<EmergencyContact, unknown>[] = [
      // catatan kontak di tooltip & dialog ubah kontak
      { id: "name", header: "Kontak", meta: { mobile: "primary" }, cell: ({ row: { original: c } }) => <CellText max={240} title={c.notes ? `${c.name} — ${c.notes}` : c.name} className="font-medium">{c.name}</CellText> },
      { id: "type", header: "Jenis", meta: { mobile: "secondary" }, size: 180, cell: ({ row: { original: c } }) => <span className="inline-flex items-center gap-1.5 whitespace-nowrap text-sm"><Icon name={CONTACT_TYPE_ICON[c.contact_type] ?? "call"} size={16} className="text-on-surface-variant" aria-hidden />{labelOf(CONTACT_TYPES, c.contact_type)}</span> },
      { id: "phone", header: "Telepon", meta: { mobile: "secondary", nowrap: true }, size: 170, cell: ({ row: { original: c } }) => <a href={telHref(c.phone)} onClick={(e) => e.stopPropagation()} className="inline-flex min-h-11 items-center gap-1 font-semibold tnum text-primary hover:underline md:min-h-0"><Icon name="call" size={14} aria-hidden />{c.phone}</a> },
    ];
    // tanpa property terpilih (semua property) → tampilkan kolom property
    if (!propertyId) cols.push({ id: "property", header: "Property", meta: { mobile: "hidden" }, cell: ({ row }) => <CellText max={160}>{properties.find((p) => p.id === row.original.property_id)?.name ?? "—"}</CellText> });
    cols.push(
      { id: "sort_order", header: "Urutan", meta: { mobile: "hidden" }, size: 80, cell: ({ row }) => <span className="tnum">{row.original.sort_order}</span> },
      { id: "status", header: "Status", meta: { mobile: "status" }, size: 110, cell: ({ row }) => (row.original.is_active ? <Badge tone="success">Aktif</Badge> : <Badge tone="neutral">Nonaktif</Badge>) },
    );
    return cols;
  }, [propertyId, properties]);
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-on-surface-variant">Kontak aktif tampil di detail Emergency dan tersimpan offline di Staff App.</p>
        {canManage && <Button icon="add" className={TOUCH} onClick={() => setEdit("new")}>Tambah Kontak</Button>}
      </div>
      <DataGrid
        columns={columns}
        rows={list.data ?? []}
        rowId={(r) => r.id}
        onRowClick={canManage ? (r) => { setEdit(r); } : undefined}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        empty={{ icon: "call", title: "Belum ada kontak darurat", description: "Tambahkan nomor pemadam kebakaran, ambulans, polisi, PLN/PDAM, dan kontak internal agar cepat dihubungi saat Emergency.", action: canManage ? <Button icon="add" onClick={() => setEdit("new")}>Tambah Kontak</Button> : undefined }}
        rowActions={canManage ? (c) => [
          { label: "Edit", icon: "edit", onSelect: () => setEdit(c) },
          { label: c.is_active ? "Nonaktifkan" : "Aktifkan", icon: c.is_active ? "block" : "check_circle", onSelect: () => setActive(c) },
          { label: "Hapus", icon: "delete", destructive: true, onSelect: () => setDel(c) },
        ] : undefined}
      />
      {edit && <ContactDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
      {del && <ConfirmDialog open onOpenChange={(o) => !o && setDel(null)} title={`Hapus kontak ${del.name}?`} description="Kontak dihapus permanen dari daftar kontak darurat property ini." confirmLabel="Hapus" destructive onConfirm={() => remove(del)} />}
    </div>
  );
}

function ContactDialog({ item, onClose }: { item: EmergencyContact | null; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [pid, setPid] = usePropertyChoice(item?.property_id);
  const [f, setF] = useState({ name: item?.name ?? "", contact_type: item?.contact_type ?? "fire", phone: item?.phone ?? "", notes: item?.notes ?? "", sort_order: String(item?.sort_order ?? 0) });
  const [busy, setBusy] = useState(false);
  const valid = !!f.name.trim() && !!f.phone.trim() && (!!item || !!pid);
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    try {
      const body = { name: f.name.trim(), contact_type: f.contact_type, phone: f.phone.trim(), sort_order: Number(f.sort_order) || 0 };
      if (item) await api(`emergency-contacts/${item.id}`, { method: "PATCH", body: { ...body, notes: f.notes.trim() } });
      else await api("emergency-contacts", { body: { ...body, property_id: pid, notes: f.notes.trim() || null }, idempotencyKey: uuid() });
      invalidate("all");
      toast.action(item ? "saved" : "created", "Kontak darurat");
      onClose();
    } catch (e) {
      toast.failed(item ? "saved" : "created", e, "Kontak darurat");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={item ? `Edit kontak · ${item.name}` : "Tambah Kontak Darurat"}>
        <div className="space-y-4">
          {!item && <PropertyField value={pid} onChange={setPid} />}
          <Field label="Nama" required><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="mis. Damkar Jakarta Selatan" autoFocus /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Jenis" required><NativeSelect value={f.contact_type} onChange={(e) => setF({ ...f, contact_type: e.target.value })}>{optionsOf(CONTACT_TYPES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
            <Field label="Telepon" required><Input type="tel" inputMode="tel" value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value })} placeholder="mis. 113 / 021-555-0101" /></Field>
          </div>
          <Field label="Urutan tampil" help="Angka kecil tampil lebih dulu."><Input type="number" min={0} value={f.sort_order} onChange={(e) => setF({ ...f, sort_order: e.target.value })} /></Field>
          <Field label="Catatan"><Textarea rows={2} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
