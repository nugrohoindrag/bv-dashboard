// Commercial › Unit Sales Management › Leads (PRD P1 v1.3 §3.10: prospective buyer/customer record, sales inquiry, sales
// pipeline/status, sales activity/history; WF-P1-008 Prospective Buyer → Unit Listing → Inquiry → Unit Reservation).
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
import { ACTIVITY_TYPES, LEAD_ACTION_LABEL, LEAD_STATUS, LISTING_STATUS, SOURCES, num, rp, type Activity, type Lead, type Listing, type SaleReservation } from "./commercial-api";
import { useSalesSummary } from "./UnitListingsPage";

const PIPELINE = ["new", "contacted", "qualified", "reserved", "sold"];

export default function SalesLeadsPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const [sp] = useSearchParams();
  const nav = useNavigate();
  const { propertyId, can } = useAuth();
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const listingId = sp.get("listing_id") ?? undefined;
  const list = useList<Lead>("unit-sales/leads", { property_id: propertyId ?? undefined, status: status || undefined, q: q || undefined, listing_id: listingId }, { enabled: !!propertyId });
  const sum = useSalesSummary(propertyId);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<Lead, unknown>[]>(() => [
    { id: "code", header: "Lead", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.lead_code}</span>, size: 150 },
    { id: "name", header: "Prospek", cell: ({ row }) => <div><div className="font-medium">{row.original.full_name}{row.original.company ? <span className="text-xs text-muted-foreground"> · {row.original.company}</span> : null}</div><div className="text-xs text-muted-foreground">{row.original.phone ?? "—"} · {row.original.email ?? "—"} · {row.original.source}</div></div> },
    { id: "unit", header: "Unit diminati", cell: ({ row }) => row.original.unit_number ? <div className="text-sm">{row.original.unit_number}<div className="text-xs text-muted-foreground">{row.original.listing_title}</div></div> : <span className="text-muted-foreground">—</span>, size: 180 },
    { id: "budget", header: "Budget", cell: ({ row }) => <span className="tnum text-sm">{row.original.budget_max ? rp(row.original.budget_max) : "—"}</span>, size: 150 },
    { id: "follow", header: "Follow-up", cell: ({ row }) => row.original.next_follow_up_at ? <span className={`text-sm ${new Date(row.original.next_follow_up_at) < new Date() && ["new", "contacted", "qualified"].includes(row.original.status) ? "text-error" : ""}`}>{fmtDateTime(row.original.next_follow_up_at)}</span> : <span className="text-muted-foreground">—</span>, size: 160 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={LEAD_STATUS[row.original.status]?.tone ?? "neutral"}>{LEAD_STATUS[row.original.status]?.label ?? row.original.status}</Badge>, size: 120 },
  ], [t]);
  if (!propertyId) return <Alert variant="info">Pilih property (profile Apartment) di header.</Alert>;
  return (
    <div>
      <PageHeader title="Unit Sales · Leads" subtitle="Prospective buyer, inquiry, dan pipeline penjualan: New → Contacted → Qualified → Reserved → Sold." actions={<>{can("commercial.sale_reservations.view") && <Link to="/commercial/sales/reservations"><Button variant="secondary"><Icon name="verified" size={16} /> Reservasi</Button></Link>}{can("commercial.sales_leads.create") && <Button onClick={() => setCreateOpen(true)}><Icon name="person_add" size={16} /> Lead Baru</Button>}</>}>
        <div className="flex items-center gap-2">
          <Input className="w-64" placeholder="Cari kode / nama / telepon…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-44" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option>{Object.entries(LEAD_STATUS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}</NativeSelect>
          {listingId && <Badge tone="info">Filter listing aktif</Badge>}
        </div>
      </PageHeader>
      {sum.data && (
        <div className="mb-5 flex flex-wrap gap-2">
          {PIPELINE.map((s, i) => (
            <button key={s} type="button" onClick={() => setStatus(status === s ? "" : s)} className={`flex items-center gap-2 rounded-full border px-3 py-1 text-sm ${status === s ? "border-primary bg-primary-soft" : "border-border hover:bg-surface-container"}`}>
              <span className="text-xs text-muted-foreground">{i + 1}</span><span>{LEAD_STATUS[s].label}</span><span className="tnum font-semibold">{sum.data.leads[s] ?? 0}</span>
            </button>
          ))}
          <span className="ml-auto text-sm text-muted-foreground">Lost {sum.data.leads.lost ?? 0} · Cancelled {sum.data.leads.cancelled ?? 0}</span>
        </div>
      )}
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/commercial/sales/leads/${r.id}`} loading={list.isLoading} isFiltered={!!q || !!status || !!listingId} empty={{ message: "Belum ada lead / inquiry." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      {createOpen && <LeadDialog propertyId={propertyId} lead={null} defaultListingId={listingId} onClose={() => setCreateOpen(false)} onSaved={(l) => nav(`/commercial/sales/leads/${l.id}`)} />}
      {id && <LeadDrawer id={id} propertyId={propertyId} onClose={() => nav("/commercial/sales/leads")} />}
    </div>
  );
}

export function LeadDrawer({ id, propertyId, onClose }: { id: string; propertyId: string; onClose: () => void }) {
  const toast = useToast();
  const { can } = useAuth();
  const lead = useOne<Lead>("unit-sales/leads", id);
  const acts = useQuery({ queryKey: ["lead-activities", id], queryFn: () => api<{ data: Activity[] }>(`unit-sales/leads/${id}/activities`).then((x) => x.data) });
  const act = useAction<{ action: string; body?: Record<string, unknown> }, Lead>((i) => `unit-sales/leads/${id}/${i.action}`, { body: (i) => i.body ?? {}, invalidate: ["list", "one", "unit-sales", "lead-activities"] });
  const addAct = useAction<Record<string, unknown>, Lead>(() => `unit-sales/leads/${id}/activities`, { invalidate: ["one", "list", "lead-activities", "unit-sales"] });
  const [dialog, setDialog] = useState<"lose" | "cancel" | "reserve" | "edit" | null>(null);
  const [a, setA] = useState({ activity_type: "call", summary: "", next_follow_up_at: "" });
  const run = (action: string, body?: Record<string, unknown>) => act.mutateAsync({ action, body }).then((l) => { toast.success(`${l.lead_code}: ${LEAD_STATUS[l.status]?.label}`); setDialog(null); }).catch(toast.error);
  const canUpdate = can("commercial.sales_leads.update");
  return (
    <Drawer open onClose={onClose} title="Sales Lead" width={720}>
      <AsyncState query={lead}>
        {(x) => (
          <div className="space-y-5">
            <div className="flex items-start justify-between gap-3">
              <div><div className="font-mono text-lg font-semibold">{x.lead_code}</div><div className="text-sm text-muted-foreground">{x.full_name}{x.company ? ` · ${x.company}` : ""}{x.unit_number ? ` · Unit ${x.unit_number}` : ""}</div></div>
              <Badge tone={LEAD_STATUS[x.status]?.tone ?? "neutral"}>{LEAD_STATUS[x.status]?.label}</Badge>
            </div>
            <div className="flex flex-wrap gap-2">
              {canUpdate && x.allowed_actions.map((ac) => {
                if (ac === "reserve") return can("commercial.sale_reservations.create") ? <Button key={ac} size="sm" onClick={() => setDialog("reserve")} icon="verified">{LEAD_ACTION_LABEL[ac]}</Button> : null;
                if (ac === "lose" || ac === "cancel") return <Button key={ac} size="sm" variant="ghost" onClick={() => setDialog(ac)}>{LEAD_ACTION_LABEL[ac]}</Button>;
                return <Button key={ac} size="sm" variant={ac === "qualify" ? "primary" : "secondary"} loading={act.isPending} onClick={() => run(ac)}>{LEAD_ACTION_LABEL[ac] ?? ac}</Button>;
              })}
              {canUpdate && <Button size="sm" variant="secondary" onClick={() => setDialog("edit")} icon="edit">Edit</Button>}
              {x.reservation_id && <Link to={`/commercial/sales/reservations/${x.reservation_id}`}><Button size="sm" variant="secondary">Lihat reservasi</Button></Link>}
            </div>
            <KeyValue items={[
              { label: "Kontak", value: `${x.phone ?? "—"} · ${x.email ?? "—"}` }, { label: "Sumber", value: x.source },
              { label: "Budget", value: x.budget_min || x.budget_max ? `${x.budget_min ? rp(x.budget_min) : "—"} – ${x.budget_max ? rp(x.budget_max) : "—"}` : "—" },
              { label: "Listing diminati", value: x.listing_id ? <Link to={`/commercial/sales/listings/${x.listing_id}`} className="text-primary hover:underline">{x.listing_code} · {x.unit_number}</Link> : "—" },
              { label: "Inquiry", value: x.inquiry ?? "—" }, { label: "PIC", value: x.assigned_name ?? "—" },
              { label: "Follow-up berikutnya", value: x.next_follow_up_at ? fmtDateTime(x.next_follow_up_at) : "—" }, { label: "Aktivitas terakhir", value: x.last_activity_at ? <RelativeTime value={x.last_activity_at} /> : "—" },
              { label: "Alasan lost/batal", value: x.lost_reason ?? "—" }, { label: "Catatan", value: x.notes ?? "—" },
            ]} />
            <div>
              <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Aktivitas & riwayat</div>
              {canUpdate && ["new", "contacted", "qualified", "reserved"].includes(x.status) && (
                <div className="mb-3 grid grid-cols-12 gap-2 rounded-[var(--radius-md)] border border-border p-3">
                  <Field label="Jenis" className="col-span-3"><NativeSelect value={a.activity_type} onChange={(e) => setA({ ...a, activity_type: e.target.value })}>{["call", "meeting", "site_visit", "email", "whatsapp", "note", "document"].map((k) => <option key={k} value={k}>{ACTIVITY_TYPES[k]}</option>)}</NativeSelect></Field>
                  <Field label="Ringkasan" className="col-span-6"><Input value={a.summary} onChange={(e) => setA({ ...a, summary: e.target.value })} placeholder="mis. Telepon: minat unit 1201, minta simulasi KPR" /></Field>
                  <Field label="Follow-up" className="col-span-3"><Input type="datetime-local" value={a.next_follow_up_at} onChange={(e) => setA({ ...a, next_follow_up_at: e.target.value })} /></Field>
                  <div className="col-span-12 flex justify-end"><Button size="sm" disabled={!a.summary.trim()} loading={addAct.isPending} onClick={() => addAct.mutateAsync({ activity_type: a.activity_type, summary: a.summary.trim(), next_follow_up_at: a.next_follow_up_at ? new Date(a.next_follow_up_at).toISOString() : null }).then(() => { setA({ ...a, summary: "", next_follow_up_at: "" }); toast.success("Aktivitas dicatat"); }).catch(toast.error)}>Catat aktivitas</Button></div>
                </div>
              )}
              <AsyncState query={acts}>
                {(items) => items.length === 0 ? <p className="text-sm text-muted-foreground">Belum ada aktivitas.</p> : (
                  <ol className="relative ml-2 space-y-3 border-l border-border pl-4">
                    {items.map((it) => (
                      <li key={it.id} className="text-sm">
                        <span className="absolute -left-[5px] mt-1.5 h-2 w-2 rounded-full bg-primary" />
                        <div><Badge tone="neutral">{ACTIVITY_TYPES[it.activity_type] ?? it.activity_type}</Badge> <span className="ml-1">{it.summary}</span></div>
                        <div className="text-xs text-muted-foreground">{fmtDateTime(it.occurred_at)} · {it.created_by_name ?? "sistem"}</div>
                      </li>
                    ))}
                  </ol>
                )}
              </AsyncState>
            </div>
            {(dialog === "lose" || dialog === "cancel") && <ReasonDialog open onOpenChange={(o) => !o && setDialog(null)} title={dialog === "lose" ? `Tandai ${x.lead_code} sebagai Lost` : `Batalkan ${x.lead_code}`} confirmLabel={dialog === "lose" ? "Lost" : "Batalkan"} destructive loading={act.isPending} onConfirm={(reason) => run(dialog, { reason })} />}
            {dialog === "reserve" && <ReserveUnitDialog propertyId={propertyId} lead={x} onClose={() => setDialog(null)} onCreated={(r) => { setDialog(null); toast.success(`Reservasi ${r.reservation_number} dibuat`); }} />}
            {dialog === "edit" && <LeadDialog propertyId={propertyId} lead={x} onClose={() => setDialog(null)} />}
          </div>
        )}
      </AsyncState>
    </Drawer>
  );
}

export function LeadDialog({ propertyId, lead, defaultListingId, onClose, onSaved }: { propertyId: string; lead: Lead | null; defaultListingId?: string; onClose: () => void; onSaved?: (l: Lead) => void }) {
  const toast = useToast();
  const listings = useAll<Listing>("unit-sales/listings", { property_id: propertyId, status: "published,reserved" });
  const [f, setF] = useState({ listing_id: lead?.listing_id ?? defaultListingId ?? "", full_name: lead?.full_name ?? "", phone: lead?.phone ?? "", email: lead?.email ?? "", company: lead?.company ?? "", source: lead?.source ?? "walk_in", budget_min: lead?.budget_min ? String(lead.budget_min) : "", budget_max: lead?.budget_max ? String(lead.budget_max) : "", inquiry: lead?.inquiry ?? "", notes: lead?.notes ?? "", next_follow_up_at: lead?.next_follow_up_at ? lead.next_follow_up_at.slice(0, 16) : "" });
  const create = useAction<Record<string, unknown>, Lead>(() => "unit-sales/leads", { invalidate: ["list", "one", "unit-sales"] });
  const update = useAction<Record<string, unknown>, Lead>(() => `unit-sales/leads/${lead?.id}`, { method: "PATCH", invalidate: ["list", "one", "unit-sales"] });
  const body = () => ({ ...(lead ? {} : { property_id: propertyId }), listing_id: f.listing_id || null, full_name: f.full_name.trim(), phone: f.phone || null, email: f.email || null, company: f.company || null, source: f.source, budget_min: num(f.budget_min), budget_max: num(f.budget_max), inquiry: f.inquiry || null, notes: f.notes || null, next_follow_up_at: f.next_follow_up_at ? new Date(f.next_follow_up_at).toISOString() : null });
  const submit = () => (lead ? update.mutateAsync(body()) : create.mutateAsync(body())).then((l) => { toast.success(`${l.lead_code} disimpan`); onClose(); onSaved?.(l); }).catch(toast.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={lead ? `Edit ${lead.lead_code}` : "Lead / Inquiry Baru"} description="Prospective buyer record + sales inquiry. Lead masuk pipeline sebagai New.">
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Nama prospek" required className="col-span-2"><Input value={f.full_name} onChange={(e) => setF({ ...f, full_name: e.target.value })} /></Field>
            <Field label="Telepon"><Input value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value })} /></Field>
            <Field label="Email"><Input type="email" value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} /></Field>
            <Field label="Perusahaan"><Input value={f.company} onChange={(e) => setF({ ...f, company: e.target.value })} /></Field>
            <Field label="Sumber"><NativeSelect value={f.source} onChange={(e) => setF({ ...f, source: e.target.value })}>{SOURCES.map((s) => <option key={s} value={s}>{s}</option>)}</NativeSelect></Field>
            <Field label="Budget min (IDR)"><Input inputMode="numeric" value={f.budget_min} onChange={(e) => setF({ ...f, budget_min: e.target.value.replace(/[^\d]/g, "") })} /></Field>
            <Field label="Budget max (IDR)"><Input inputMode="numeric" value={f.budget_max} onChange={(e) => setF({ ...f, budget_max: e.target.value.replace(/[^\d]/g, "") })} /></Field>
          </div>
          <Field label="Listing yang diminati"><NativeSelect value={f.listing_id} onChange={(e) => setF({ ...f, listing_id: e.target.value })}><option value="">— belum ada —</option>{(listings.data ?? []).map((l) => <option key={l.id} value={l.id}>{l.unit_number} · {l.title} · {rp(l.asking_price)} ({LISTING_STATUS[l.status]?.label})</option>)}</NativeSelect></Field>
          <Field label="Inquiry"><Textarea rows={2} value={f.inquiry} onChange={(e) => setF({ ...f, inquiry: e.target.value })} placeholder="Pertanyaan / kebutuhan prospek" /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Follow-up berikutnya"><Input type="datetime-local" value={f.next_follow_up_at} onChange={(e) => setF({ ...f, next_follow_up_at: e.target.value })} /></Field>
            <Field label="Catatan internal"><Input value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
          </div>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={create.isPending || update.isPending} disabled={!f.full_name.trim()} onClick={submit}>{lead ? "Simpan" : "Buat Lead"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Unit Reservation dari lead (WF-P1-008): listing published → reserved; booking fee → invoice (Billing). */
export function ReserveUnitDialog({ propertyId, lead, onClose, onCreated }: { propertyId: string; lead: Lead; onClose: () => void; onCreated?: (r: SaleReservation) => void }) {
  const toast = useToast();
  const listings = useAll<Listing>("unit-sales/listings", { property_id: propertyId, status: "published" });
  const [f, setF] = useState({ listing_id: lead.listing_id ?? "", agreed_price: "", booking_fee: "", reserved_until: "", contract_reference: "", notes: "", issue_invoice: true });
  const sel = (listings.data ?? []).find((l) => l.id === f.listing_id);
  const create = useAction<Record<string, unknown>, SaleReservation>(() => "unit-sales/reservations", { invalidate: ["list", "one", "all", "unit-sales", "lead-activities"] });
  const submit = () => create.mutateAsync({ lead_id: lead.id, listing_id: f.listing_id, agreed_price: num(f.agreed_price) || undefined, booking_fee: num(f.booking_fee) ?? 0, reserved_until: f.reserved_until || null, contract_reference: f.contract_reference || null, notes: f.notes || null, issue_invoice: f.issue_invoice }).then((r) => { onClose(); onCreated?.(r); }).catch(toast.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Reservasi unit untuk ${lead.full_name}`} description="Hanya listing Available yang dapat direservasi. Unit → Reserved; booking fee dapat ditagihkan sebagai invoice.">
        <div className="space-y-3">
          <Field label="Listing" required><NativeSelect value={f.listing_id} onChange={(e) => setF({ ...f, listing_id: e.target.value })}><option value="">— pilih —</option>{(listings.data ?? []).map((l) => <option key={l.id} value={l.id}>{l.unit_number} · {l.title} · {rp(l.asking_price)}</option>)}</NativeSelect></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Harga disepakati (IDR)" help={sel ? `Harga listing ${rp(sel.asking_price)}` : undefined}><Input inputMode="numeric" placeholder={sel ? String(sel.asking_price) : ""} value={f.agreed_price} onChange={(e) => setF({ ...f, agreed_price: e.target.value.replace(/[^\d]/g, "") })} /></Field>
            <Field label="Booking fee (IDR)"><Input inputMode="numeric" value={f.booking_fee} onChange={(e) => setF({ ...f, booking_fee: e.target.value.replace(/[^\d]/g, "") })} /></Field>
            <Field label="Berlaku sampai"><Input type="date" value={f.reserved_until} onChange={(e) => setF({ ...f, reserved_until: e.target.value })} /></Field>
            <Field label="Referensi kontrak"><Input value={f.contract_reference} onChange={(e) => setF({ ...f, contract_reference: e.target.value })} /></Field>
          </div>
          <Field label="Catatan"><Textarea rows={2} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
          <Checkbox label="Terbitkan invoice booking fee (bila Billing aktif)" checked={f.issue_invoice} onCheckedChange={(v) => setF({ ...f, issue_invoice: v })} />
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={create.isPending} disabled={!f.listing_id} onClick={submit}>Reservasi Unit</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
