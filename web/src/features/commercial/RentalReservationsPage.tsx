// Commercial › Unit Rental Management › Reservations (PRD P1 v1.3 §3.10: rental inquiry, reservation/booking, rental status,
// tenant onboarding, rental history; WF-P1-009 Rental Period → Availability → Booking → Tenant Onboarding → Active Rental).
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, KeyValue, ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useAll, useList, useOne } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import { PERIOD_LABEL, PERIOD_UNIT, RRES_STATUS, SOURCES, fmtD, rp, type Onboarding, type Quote, type RentalListing, type RentalReservation } from "./commercial-api";
import { OnboardingCard } from "./shared";

const endIncl = (s: string) => { const d = new Date(s); d.setDate(d.getDate() - 1); return fmtD(d.toISOString()); };

export default function RentalReservationsPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const [sp] = useSearchParams();
  const nav = useNavigate();
  const { propertyId, can } = useAuth();
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const listingId = sp.get("listing_id") ?? undefined;
  const list = useList<RentalReservation>("unit-rental/reservations", { property_id: propertyId ?? undefined, status: status || undefined, q: q || undefined, listing_id: listingId }, { enabled: !!propertyId });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<RentalReservation, unknown>[]>(() => [
    { id: "number", header: "Reservasi", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.reservation_number}</span>, size: 150 },
    { id: "prospect", header: "Penyewa", cell: ({ row }) => <div><div className="font-medium">{row.original.prospect_name}{row.original.company ? <span className="text-xs text-muted-foreground"> · {row.original.company}</span> : null}</div><div className="text-xs text-muted-foreground">{row.original.prospect_phone ?? "—"} · {row.original.occupants} penghuni · {row.original.source}</div></div> },
    { id: "unit", header: "Unit", cell: ({ row }) => <div className="text-sm">{row.original.unit_number}<div className="text-xs text-muted-foreground">{row.original.listing_title}</div></div>, size: 170 },
    { id: "period", header: "Periode", cell: ({ row }) => <div className="text-sm">{fmtD(row.original.start_date)} → {endIncl(row.original.end_date)}<div className="text-xs text-muted-foreground">{PERIOD_LABEL[row.original.rental_period]} × {row.original.period_count} · {row.original.days} hari</div></div>, size: 230 },
    { id: "total", header: "Total", cell: ({ row }) => <div className="tnum text-sm font-semibold">{rp(row.original.total_amount)}<div className="text-xs font-normal text-muted-foreground">{row.original.invoice_number ? `${row.original.invoice_number} · ${row.original.invoice_status}` : "belum ditagih"}</div></div>, size: 170 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={RRES_STATUS[row.original.status]?.tone ?? "neutral"}>{RRES_STATUS[row.original.status]?.label ?? row.original.status}</Badge>, size: 130 },
  ], [t]);
  if (!propertyId) return <Alert variant="info">Pilih property (profile Apartment) di header.</Alert>;
  return (
    <div>
      <PageHeader title="Unit Rental · Reservations" subtitle="Inquiry (New) → Reserved → Active (tenant onboarding) → Completed. Konflik periode per unit dicegah oleh server." actions={<>{can("commercial.rental_reservations.view") && <Link to="/commercial/rental/calendar"><Button variant="secondary"><Icon name="calendar_month" size={16} /> Kalender</Button></Link>}{can("commercial.rental_reservations.create") && <Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> Reservasi / Inquiry</Button>}</>}>
        <div className="flex items-center gap-2">
          <Input className="w-64" placeholder="Cari nomor / penyewa / unit…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-44" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option>{Object.entries(RRES_STATUS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}</NativeSelect>
          {listingId && <Badge tone="info">Filter listing aktif</Badge>}
        </div>
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/commercial/rental/reservations/${r.id}`} loading={list.isLoading} isFiltered={!!q || !!status || !!listingId} empty={{ message: "Belum ada reservasi sewa." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      {createOpen && <CreateRentalReservationDialog propertyId={propertyId} defaultListingId={listingId} onClose={() => setCreateOpen(false)} onCreated={(r) => nav(`/commercial/rental/reservations/${r.id}`)} />}
      {id && <RentalReservationDrawer id={id} onClose={() => nav("/commercial/rental/reservations")} />}
    </div>
  );
}

export function RentalReservationDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const toast = useToast();
  const { can } = useAuth();
  const q = useOne<RentalReservation>("unit-rental/reservations", id);
  const act = useAction<{ action: string; body?: Record<string, unknown> }, { reservation: RentalReservation; onboarding?: Onboarding }>((i) => `unit-rental/reservations/${id}/${i.action}`, { body: (i) => i.body ?? {}, invalidate: ["list", "one", "all", "unit-rental"] });
  const [dialog, setDialog] = useState<"cancel" | "activate" | "complete" | null>(null);
  const [ob, setOb] = useState<Onboarding | null>(null);
  const [a, setA] = useState({ tenant_name: "", tenant_phone: "", tenant_email: "", company: "", tenant_type: "individual", create_tenant_account: true, move_in_date: "" });
  const [moveOut, setMoveOut] = useState(new Date().toISOString().slice(0, 10));
  const run = (action: string, body?: Record<string, unknown>) => act.mutateAsync({ action, body }).then((x) => { if (x.onboarding) setOb(x.onboarding); toast.success(`${x.reservation.reservation_number}: ${RRES_STATUS[x.reservation.status]?.label}`); setDialog(null); }).catch(toast.error);
  return (
    <Drawer open onClose={onClose} title="Rental Reservation" width={700}>
      <AsyncState query={q}>
        {(x) => (
          <div className="space-y-5">
            <div className="flex items-start justify-between gap-3">
              <div><div className="font-mono text-lg font-semibold">{x.reservation_number}</div><div className="text-sm text-muted-foreground">{x.prospect_name} · Unit {x.unit_number} · {PERIOD_LABEL[x.rental_period]} × {x.period_count}</div></div>
              <Badge tone={RRES_STATUS[x.status]?.tone ?? "neutral"}>{RRES_STATUS[x.status]?.label}</Badge>
            </div>
            {ob && <OnboardingCard ob={ob} />}
            <div className="flex flex-wrap gap-2">
              {x.allowed_actions.includes("confirm") && can("commercial.rental_reservations.confirm") && <Button size="sm" onClick={() => run("confirm")} loading={act.isPending} icon="event_available">Konfirmasi (Reserved)</Button>}
              {x.allowed_actions.includes("activate") && can("commercial.rental_reservations.activate") && <Button size="sm" onClick={() => { setA({ ...a, tenant_name: x.prospect_name, tenant_phone: x.prospect_phone ?? "", tenant_email: x.prospect_email ?? "", company: x.company ?? "", move_in_date: x.start_date.slice(0, 10) }); setDialog("activate"); }} icon="key">Aktifkan & onboarding…</Button>}
              {x.allowed_actions.includes("complete") && can("commercial.rental_reservations.complete") && <Button size="sm" variant="secondary" onClick={() => setDialog("complete")} icon="logout">Selesai (move-out)…</Button>}
              {x.allowed_actions.includes("cancel") && can("commercial.rental_reservations.cancel") && <Button size="sm" variant="ghost" onClick={() => setDialog("cancel")}>Batalkan…</Button>}
            </div>
            <KeyValue items={[
              { label: "Periode", value: `${fmtD(x.start_date)} → ${endIncl(x.end_date)} (${x.days} hari)` }, { label: "Tarif", value: `${rp(x.rate_amount)} / ${PERIOD_UNIT[x.rental_period]}` },
              { label: "Total sewa", value: <span className="tnum font-semibold">{rp(x.total_amount)}</span> }, { label: "Deposit", value: rp(x.deposit_amount) },
              { label: "Invoice", value: x.invoice_id ? <Link to={`/billing/invoices/${x.invoice_id}`} className="text-primary hover:underline">{x.invoice_number} · {x.invoice_status}</Link> : "—" },
              { label: "Listing", value: <Link to={`/commercial/rental/listings/${x.listing_id}`} className="text-primary hover:underline">{x.listing_code} · {x.listing_title}</Link> },
              { label: "Kontak", value: `${x.prospect_phone ?? "—"} · ${x.prospect_email ?? "—"}` }, { label: "Penghuni", value: `${x.occupants} orang${x.company ? ` · ${x.company}` : ""}` },
              { label: "Sumber", value: x.source }, { label: "Permintaan khusus", value: x.special_requests ?? "—" }, { label: "Catatan internal", value: x.notes ?? "—" },
              { label: "Tenant (onboarding)", value: x.tenant_id ? <Link to={`/tenant/tenants/${x.tenant_id}`} className="text-primary hover:underline">{x.tenant_code}</Link> : "—" }, { label: "Akun Tenant App", value: x.tenant_user_id ? "Ya" : "—" },
              { label: "Reserved", value: x.reserved_at ? fmtDateTime(x.reserved_at) : "—" }, { label: "Aktif", value: x.activated_at ? fmtDateTime(x.activated_at) : "—" },
              { label: "Selesai", value: x.completed_at ? fmtDateTime(x.completed_at) : "—" }, { label: "Dibatalkan", value: x.cancelled_at ? `${fmtDateTime(x.cancelled_at)} · ${x.cancel_reason ?? ""}` : "—" },
              { label: "Dibuat", value: <RelativeTime value={x.created_at} /> },
            ]} />
            {dialog === "cancel" && <ReasonDialog open onOpenChange={(o) => !o && setDialog(null)} title={`Batalkan ${x.reservation_number}`} description="Invoice sewa dibatalkan bila belum dibayar; unit kembali tersedia untuk periode ini." confirmLabel="Batalkan reservasi" destructive loading={act.isPending} onConfirm={(reason) => run("cancel", { reason })} />}
            {dialog === "activate" && (
              <Dialog open onOpenChange={(o) => !o && setDialog(null)}>
                <DialogContent side="right" title={`Tenant onboarding · Unit ${x.unit_number}`} description="Penyewa dicatat sebagai Tenant + Occupant unit (model Tenant/Occupant bersama), unit → Occupied. Opsional: akun Tenant App dengan akses sampai akhir sewa (AC-11).">
                  <div className="space-y-3">
                    <div className="grid grid-cols-2 gap-3">
                      <Field label="Nama penyewa" required className="col-span-2"><Input value={a.tenant_name} onChange={(e) => setA({ ...a, tenant_name: e.target.value })} /></Field>
                      <Field label="Telepon"><Input value={a.tenant_phone} onChange={(e) => setA({ ...a, tenant_phone: e.target.value })} /></Field>
                      <Field label="Email"><Input type="email" value={a.tenant_email} onChange={(e) => setA({ ...a, tenant_email: e.target.value })} /></Field>
                      <Field label="Jenis"><NativeSelect value={a.tenant_type} onChange={(e) => setA({ ...a, tenant_type: e.target.value })}><option value="individual">Perorangan</option><option value="company">Perusahaan</option></NativeSelect></Field>
                      {a.tenant_type === "company" && <Field label="Nama perusahaan"><Input value={a.company} onChange={(e) => setA({ ...a, company: e.target.value })} /></Field>}
                      <Field label="Tanggal masuk"><Input type="date" value={a.move_in_date} onChange={(e) => setA({ ...a, move_in_date: e.target.value })} /></Field>
                    </div>
                    <Checkbox label="Buat akun Tenant App untuk penyewa (butuh email)" checked={a.create_tenant_account} onCheckedChange={(v) => setA({ ...a, create_tenant_account: v })} />
                  </div>
                  <DialogFooter><Button variant="secondary" onClick={() => setDialog(null)}>Batal</Button><Button loading={act.isPending} disabled={!a.tenant_name.trim() || (a.create_tenant_account && !a.tenant_email.trim())} onClick={() => run("activate", { tenant_name: a.tenant_name.trim(), tenant_phone: a.tenant_phone || null, tenant_email: a.tenant_email || null, company: a.company || null, tenant_type: a.tenant_type, create_tenant_account: a.create_tenant_account, move_in_date: a.move_in_date || null })}>Aktifkan sewa</Button></DialogFooter>
                </DialogContent>
              </Dialog>
            )}
            {dialog === "complete" && (
              <Dialog open onOpenChange={(o) => !o && setDialog(null)}>
                <DialogContent title={`Selesaikan sewa ${x.reservation_number}`} description="Move-out: occupant dinonaktifkan, unit → Vacant, akses Tenant App penyewa berakhir. Riwayat sewa tetap tersimpan.">
                  <Field label="Tanggal keluar"><Input type="date" value={moveOut} onChange={(e) => setMoveOut(e.target.value)} /></Field>
                  <DialogFooter><Button variant="secondary" onClick={() => setDialog(null)}>Batal</Button><Button loading={act.isPending} onClick={() => run("complete", { move_out_date: moveOut || null })}>Selesaikan</Button></DialogFooter>
                </DialogContent>
              </Dialog>
            )}
          </div>
        )}
      </AsyncState>
    </Drawer>
  );
}

/** Rental Period → Availability → Booking (WF-P1-009): kuotasi harga & ketersediaan real-time dari server. */
export function CreateRentalReservationDialog({ propertyId, defaultListingId, onClose, onCreated }: { propertyId: string; defaultListingId?: string; onClose: () => void; onCreated?: (r: RentalReservation) => void }) {
  const toast = useToast();
  const { can } = useAuth();
  const listings = useAll<RentalListing>("unit-rental/listings", { property_id: propertyId, status: "published" });
  const [f, setF] = useState({ listing_id: defaultListingId ?? "", rental_period: "monthly", period_count: "1", start_date: new Date().toISOString().slice(0, 10), prospect_name: "", prospect_phone: "", prospect_email: "", company: "", occupants: "1", source: "walk_in", special_requests: "", notes: "", confirm: can("commercial.rental_reservations.confirm") });
  const sel = (listings.data ?? []).find((l) => l.id === f.listing_id);
  const offered = sel ? (["daily", "weekly", "monthly"] as const).filter((p) => (p === "daily" ? sel.rate_daily : p === "weekly" ? sel.rate_weekly : sel.rate_monthly)) : [];
  const period = offered.includes(f.rental_period as never) ? f.rental_period : offered[0] ?? f.rental_period;
  const quote = useQuery({ queryKey: ["unit-rental-quote", f.listing_id, period, f.period_count, f.start_date], enabled: !!f.listing_id && !!f.start_date && Number(f.period_count) > 0, queryFn: () => api<Quote>("unit-rental/availability", { query: { listing_id: f.listing_id, rental_period: period, period_count: f.period_count, start_date: f.start_date } }) });
  const create = useAction<Record<string, unknown>, RentalReservation>(() => "unit-rental/reservations", { invalidate: ["list", "one", "all", "unit-rental"] });
  const submit = () => create.mutateAsync({ listing_id: f.listing_id, prospect_name: f.prospect_name.trim(), prospect_phone: f.prospect_phone || null, prospect_email: f.prospect_email || null, company: f.company || null, occupants: Number(f.occupants) || 1, rental_period: period, period_count: Number(f.period_count) || 1, start_date: f.start_date, source: f.source, special_requests: f.special_requests || null, notes: f.notes || null, confirm: f.confirm })
    .then((r) => { toast.success(`${r.reservation_number} dibuat (${RRES_STATUS[r.status]?.label})`); onClose(); onCreated?.(r); }).catch(toast.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Reservasi / Inquiry Sewa" description="Prospective Tenant → Unit Listing → Rental Period (Daily / Weekly / Monthly) → Availability → Booking.">
        <div className="space-y-4">
          <Field label="Listing" required><NativeSelect value={f.listing_id} onChange={(e) => setF({ ...f, listing_id: e.target.value })}><option value="">— pilih unit —</option>{(listings.data ?? []).map((l) => <option key={l.id} value={l.id}>{l.unit_number} · {l.title}</option>)}</NativeSelect></Field>
          <div className="grid grid-cols-3 gap-3">
            <Field label="Periode" required><NativeSelect value={period} onChange={(e) => setF({ ...f, rental_period: e.target.value })}>{(offered.length ? offered : (["daily", "weekly", "monthly"] as const)).map((p) => <option key={p} value={p}>{PERIOD_LABEL[p]}</option>)}</NativeSelect></Field>
            <Field label={`Jumlah ${PERIOD_UNIT[period] ?? "periode"}`} required><Input type="number" min={1} value={f.period_count} onChange={(e) => setF({ ...f, period_count: e.target.value })} /></Field>
            <Field label="Mulai" required><Input type="date" value={f.start_date} onChange={(e) => setF({ ...f, start_date: e.target.value })} /></Field>
          </div>
          {quote.data && (
            <Alert variant={quote.data.available ? "success" : "warning"} title={quote.data.available ? `Tersedia · ${rp(quote.data.total_amount)}` : `Tidak tersedia · ${quote.data.reason ?? ""}`}>
              {fmtD(quote.data.start_date)} → {endIncl(quote.data.end_date)} ({quote.data.days} hari) · {rp(quote.data.rate_amount)}/{PERIOD_UNIT[quote.data.rental_period]} · deposit {rp(quote.data.deposit_amount)}{quote.data.conflicts?.length ? ` · bentrok: ${quote.data.conflicts.join(", ")}` : ""}
            </Alert>
          )}
          <div className="grid grid-cols-2 gap-3">
            <Field label="Nama penyewa" required className="col-span-2"><Input value={f.prospect_name} onChange={(e) => setF({ ...f, prospect_name: e.target.value })} /></Field>
            <Field label="Telepon"><Input value={f.prospect_phone} onChange={(e) => setF({ ...f, prospect_phone: e.target.value })} /></Field>
            <Field label="Email"><Input type="email" value={f.prospect_email} onChange={(e) => setF({ ...f, prospect_email: e.target.value })} /></Field>
            <Field label="Perusahaan"><Input value={f.company} onChange={(e) => setF({ ...f, company: e.target.value })} /></Field>
            <Field label="Jumlah penghuni"><Input type="number" min={1} value={f.occupants} onChange={(e) => setF({ ...f, occupants: e.target.value })} /></Field>
            <Field label="Sumber"><NativeSelect value={f.source} onChange={(e) => setF({ ...f, source: e.target.value })}>{SOURCES.map((s) => <option key={s} value={s}>{s}</option>)}</NativeSelect></Field>
          </div>
          <Field label="Permintaan khusus"><Textarea rows={2} value={f.special_requests} onChange={(e) => setF({ ...f, special_requests: e.target.value })} /></Field>
          <Field label="Catatan internal"><Input value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
          {can("commercial.rental_reservations.confirm") && <Checkbox label="Langsung Reserved (blokir periode; invoice sewa + deposit diterbitkan bila Billing aktif)" checked={f.confirm} onCheckedChange={(v) => setF({ ...f, confirm: v })} />}
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={create.isPending} disabled={!f.listing_id || !f.prospect_name.trim() || !f.start_date || (f.confirm && quote.data ? !quote.data.available : false)} onClick={submit}>{f.confirm ? "Reservasi" : "Simpan Inquiry"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
