// Commercial › Unit Sales Management › Reservations (PRD P1 v1.3 §3.10 unit reservation, sales pipeline/status;
// WF-P1-008 Unit Reservation → Sales Status → Handover). Handover = pemilik menjadi Tenant/Occupant unit (model bersama) + akun Tenant App opsional.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, KeyValue, ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useList, useOne } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import { SRES_STATUS, fmtD, rp, type Doc, type Onboarding, type SaleReservation } from "./commercial-api";
import { DocList, DocumentsEditor, OnboardingCard } from "./shared";

export default function SaleReservationsPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const { propertyId } = useAuth();
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const list = useList<SaleReservation>("unit-sales/reservations", { property_id: propertyId ?? undefined, status: status || undefined, q: q || undefined }, { enabled: !!propertyId });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<SaleReservation, unknown>[]>(() => [
    { id: "number", header: "Reservasi", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.reservation_number}</span>, size: 150 },
    { id: "unit", header: "Unit", cell: ({ row }) => <div><div className="font-medium">{row.original.unit_number}</div><div className="text-xs text-muted-foreground">{row.original.listing_title}</div></div>, size: 180 },
    { id: "buyer", header: "Pembeli", cell: ({ row }) => <div><div className="font-medium">{row.original.buyer_name}</div><div className="text-xs text-muted-foreground">{row.original.lead_code} · {row.original.buyer_phone ?? "—"}</div></div> },
    { id: "price", header: "Harga", cell: ({ row }) => <div className="tnum text-sm font-semibold">{rp(row.original.agreed_price)}<div className="text-xs font-normal text-muted-foreground">booking fee {rp(row.original.booking_fee)}</div></div>, size: 170 },
    { id: "until", header: "Berlaku s/d", cell: ({ row }) => row.original.reserved_until ? fmtD(row.original.reserved_until) : "—", size: 120 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={SRES_STATUS[row.original.status]?.tone ?? "neutral"}>{SRES_STATUS[row.original.status]?.label ?? row.original.status}</Badge>, size: 140 },
  ], [t]);
  if (!propertyId) return <Alert variant="info">Pilih property (profile Apartment) di header.</Alert>;
  return (
    <div>
      <PageHeader title="Unit Sales · Reservations" subtitle="Reservasi unit → Contract Signed → Sold → Handed Over. Reservasi baru dibuat dari Lead (Reservasi unit…).">
        <div className="flex items-center gap-2">
          <Input className="w-64" placeholder="Cari nomor / pembeli / unit…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-44" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option>{Object.entries(SRES_STATUS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}</NativeSelect>
        </div>
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/commercial/sales/reservations/${r.id}`} loading={list.isLoading} isFiltered={!!q || !!status} empty={{ message: "Belum ada reservasi penjualan." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      {id && <SaleReservationDrawer id={id} onClose={() => nav("/commercial/sales/reservations")} />}
    </div>
  );
}

export function SaleReservationDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const toast = useToast();
  const { can } = useAuth();
  const q = useOne<SaleReservation>("unit-sales/reservations", id);
  const act = useAction<{ action: string; body?: Record<string, unknown> }, { reservation: SaleReservation; onboarding?: Onboarding }>((i) => `unit-sales/reservations/${id}/${i.action}`, { body: (i) => i.body ?? {}, invalidate: ["list", "one", "all", "unit-sales", "lead-activities"] });
  const update = useAction<Record<string, unknown>, SaleReservation>(() => `unit-sales/reservations/${id}`, { method: "PATCH", invalidate: ["list", "one", "unit-sales"] });
  const [dialog, setDialog] = useState<"cancel" | "handover" | "contract" | "docs" | null>(null);
  const [ob, setOb] = useState<Onboarding | null>(null);
  const [contractRef, setContractRef] = useState("");
  const [docs, setDocs] = useState<Doc[]>([]);
  const [h, setH] = useState({ owner_name: "", owner_phone: "", owner_email: "", company: "", tenant_type: "individual", create_tenant_account: true, handover_date: new Date().toISOString().slice(0, 10) });
  const run = (action: string, body?: Record<string, unknown>) => act.mutateAsync({ action, body }).then((x) => { if (x.onboarding) setOb(x.onboarding); toast.success(`${x.reservation.reservation_number}: ${SRES_STATUS[x.reservation.status]?.label}`); setDialog(null); }).catch(toast.error);
  return (
    <Drawer open onClose={onClose} title="Unit Sale Reservation" width={680}>
      <AsyncState query={q}>
        {(x) => (
          <div className="space-y-5">
            <div className="flex items-start justify-between gap-3">
              <div><div className="font-mono text-lg font-semibold">{x.reservation_number}</div><div className="text-sm text-muted-foreground">Unit {x.unit_number} · {x.buyer_name}</div></div>
              <Badge tone={SRES_STATUS[x.status]?.tone ?? "neutral"}>{SRES_STATUS[x.status]?.label}</Badge>
            </div>
            {ob && <OnboardingCard ob={ob} />}
            <div className="flex flex-wrap gap-2">
              {x.allowed_actions.includes("sign_contract") && can("commercial.sale_reservations.update") && <Button size="sm" variant="secondary" onClick={() => { setContractRef(x.contract_reference ?? ""); setDialog("contract"); }} icon="draw">Kontrak ditandatangani…</Button>}
              {x.allowed_actions.includes("complete") && can("commercial.sale_reservations.complete") && <Button size="sm" onClick={() => run("complete")} loading={act.isPending} icon="check_circle">Tandai Sold</Button>}
              {x.allowed_actions.includes("handover") && can("commercial.sale_reservations.complete") && <Button size="sm" onClick={() => { setH({ ...h, owner_name: x.buyer_name, owner_phone: x.buyer_phone ?? "", owner_email: x.buyer_email ?? "" }); setDialog("handover"); }} icon="key">Serah terima…</Button>}
              {x.allowed_actions.includes("cancel") && can("commercial.sale_reservations.cancel") && <Button size="sm" variant="ghost" onClick={() => setDialog("cancel")}>Batalkan…</Button>}
              {!["cancelled", "handed_over"].includes(x.status) && can("commercial.sale_reservations.update") && <Button size="sm" variant="secondary" onClick={() => { setDocs(x.documents); setDialog("docs"); }} icon="folder">Dokumen…</Button>}
            </div>
            <KeyValue items={[
              { label: "Listing", value: <Link to={`/commercial/sales/listings/${x.listing_id}`} className="text-primary hover:underline">{x.listing_code} · {x.listing_title}</Link> },
              { label: "Lead", value: <Link to={`/commercial/sales/leads/${x.lead_id}`} className="text-primary hover:underline">{x.lead_code} · {x.buyer_name}</Link> },
              { label: "Kontak pembeli", value: `${x.buyer_phone ?? "—"} · ${x.buyer_email ?? "—"}` },
              { label: "Harga disepakati", value: <span className="tnum font-semibold">{rp(x.agreed_price)}</span> }, { label: "Booking fee", value: rp(x.booking_fee) },
              { label: "Invoice booking fee", value: x.invoice_id ? <Link to={`/billing/invoices/${x.invoice_id}`} className="text-primary hover:underline">Lihat invoice</Link> : "—" },
              { label: "Berlaku sampai", value: x.reserved_until ? fmtD(x.reserved_until) : "—" }, { label: "Referensi kontrak", value: x.contract_reference ?? "—" },
              { label: "Dokumen / referensi", value: <DocList docs={x.documents} /> }, { label: "Catatan", value: x.notes ?? "—" },
              { label: "Kontrak", value: x.contract_signed_at ? fmtDateTime(x.contract_signed_at) : "—" }, { label: "Sold", value: x.sold_at ? fmtDateTime(x.sold_at) : "—" },
              { label: "Serah terima", value: x.handed_over_at ? fmtDateTime(x.handed_over_at) : "—" },
              { label: "Pemilik (Tenant)", value: x.tenant_id ? <Link to={`/tenant/tenants/${x.tenant_id}`} className="text-primary hover:underline">Lihat tenant</Link> : "—" },
              { label: "Dibatalkan", value: x.cancelled_at ? `${fmtDateTime(x.cancelled_at)} · ${x.cancel_reason ?? ""}` : "—" },
              { label: "Dibuat", value: <RelativeTime value={x.created_at} /> },
            ]} />
            {dialog === "cancel" && <ReasonDialog open onOpenChange={(o) => !o && setDialog(null)} title={`Batalkan ${x.reservation_number}`} description="Listing kembali Available; lead kembali Qualified; invoice booking fee dibatalkan bila belum dibayar." confirmLabel="Batalkan reservasi" destructive loading={act.isPending} onConfirm={(reason) => run("cancel", { reason })} />}
            {dialog === "contract" && (
              <Dialog open onOpenChange={(o) => !o && setDialog(null)}>
                <DialogContent title="Kontrak ditandatangani"><Field label="Referensi kontrak (PPJB/AJB)"><Input value={contractRef} onChange={(e) => setContractRef(e.target.value)} /></Field>
                  <DialogFooter><Button variant="secondary" onClick={() => setDialog(null)}>Batal</Button><Button loading={act.isPending} onClick={() => run("sign_contract", { contract_reference: contractRef || null })}>Simpan</Button></DialogFooter>
                </DialogContent>
              </Dialog>
            )}
            {dialog === "docs" && (
              <Dialog open onOpenChange={(o) => !o && setDialog(null)}>
                <DialogContent title="Dokumen / referensi" description="Nomor referensi dokumen transaksi (PPJB, AJB, KTP, bukti bayar). Isi dokumen sensitif tidak disimpan di sini.">
                  <DocumentsEditor value={docs} onChange={setDocs} />
                  <DialogFooter><Button variant="secondary" onClick={() => setDialog(null)}>Batal</Button><Button loading={update.isPending} onClick={() => update.mutateAsync({ documents: docs }).then(() => { toast.success("Dokumen disimpan"); setDialog(null); }).catch(toast.error)}>Simpan</Button></DialogFooter>
                </DialogContent>
              </Dialog>
            )}
            {dialog === "handover" && (
              <Dialog open onOpenChange={(o) => !o && setDialog(null)}>
                <DialogContent side="right" title={`Serah terima unit ${x.unit_number}`} description="Pemilik dicatat sebagai Tenant + Occupant unit (model Tenant/Occupant bersama). Opsional: buat akun Tenant App (Resident App) untuk pemilik.">
                  <div className="space-y-3">
                    <div className="grid grid-cols-2 gap-3">
                      <Field label="Nama pemilik" required className="col-span-2"><Input value={h.owner_name} onChange={(e) => setH({ ...h, owner_name: e.target.value })} /></Field>
                      <Field label="Telepon"><Input value={h.owner_phone} onChange={(e) => setH({ ...h, owner_phone: e.target.value })} /></Field>
                      <Field label="Email"><Input type="email" value={h.owner_email} onChange={(e) => setH({ ...h, owner_email: e.target.value })} /></Field>
                      <Field label="Jenis"><NativeSelect value={h.tenant_type} onChange={(e) => setH({ ...h, tenant_type: e.target.value })}><option value="individual">Perorangan</option><option value="company">Perusahaan</option></NativeSelect></Field>
                      {h.tenant_type === "company" && <Field label="Nama perusahaan"><Input value={h.company} onChange={(e) => setH({ ...h, company: e.target.value })} /></Field>}
                      <Field label="Tanggal serah terima"><Input type="date" value={h.handover_date} onChange={(e) => setH({ ...h, handover_date: e.target.value })} /></Field>
                    </div>
                    <Checkbox label="Buat akun Tenant App untuk pemilik (butuh email)" checked={h.create_tenant_account} onCheckedChange={(v) => setH({ ...h, create_tenant_account: v })} />
                  </div>
                  <DialogFooter><Button variant="secondary" onClick={() => setDialog(null)}>Batal</Button><Button loading={act.isPending} disabled={!h.owner_name.trim() || (h.create_tenant_account && !h.owner_email.trim())} onClick={() => run("handover", { owner_name: h.owner_name.trim(), owner_phone: h.owner_phone || null, owner_email: h.owner_email || null, company: h.company || null, tenant_type: h.tenant_type, create_tenant_account: h.create_tenant_account, handover_date: h.handover_date || null })}>Serah terima</Button></DialogFooter>
                </DialogContent>
              </Dialog>
            )}
          </div>
        )}
      </AsyncState>
    </Drawer>
  );
}
