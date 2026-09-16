// Commercial › Unit Sales Management › Listings (PRD P1 v1.3 §3.10: sales inventory, unit availability/status, listing, price
// configuration, document/reference tracking). Listing = ekstensi komersial unit milik property (bukan marketplace publik).
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { MetricCard } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useList, useOne } from "@/api/hooks";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { FURNISHING, LISTING_STATUS, fmtD, num, rp, type Doc, type Listing, type SalesSummary } from "./commercial-api";
import { DocList, DocumentsEditor, UnitSelect } from "./shared";

export function useSalesSummary(propertyId: string | null) {
  return useQuery({ queryKey: ["unit-sales-summary", propertyId], enabled: !!propertyId, queryFn: () => api<SalesSummary>("unit-sales/summary", { query: { property_id: propertyId } }) });
}

export default function UnitListingsPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const { propertyId, can } = useAuth();
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const [edit, setEdit] = useState<Listing | "new" | null>(null);
  const list = useList<Listing>("unit-sales/listings", { property_id: propertyId ?? undefined, status: status || undefined, q: q || undefined }, { enabled: !!propertyId });
  const sum = useSalesSummary(propertyId);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<Listing, unknown>[]>(() => [
    { id: "code", header: "Listing", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.listing_code}</span>, size: 150 },
    { id: "unit", header: "Unit", cell: ({ row }) => <div><div className="font-medium">{row.original.unit_number} · {row.original.title}</div><div className="text-xs text-muted-foreground">{row.original.floor_name ?? ""}{row.original.bedrooms != null ? ` · ${row.original.bedrooms} KT` : ""}{row.original.area_m2 != null ? ` · ${row.original.area_m2} m²` : ""}{row.original.furnishing ? ` · ${FURNISHING[row.original.furnishing]}` : ""}</div></div> },
    { id: "price", header: "Harga", cell: ({ row }) => <span className="tnum font-semibold">{rp(row.original.asking_price)}{row.original.price_negotiable ? <span className="ml-1 text-xs font-normal text-muted-foreground">nego</span> : null}</span>, size: 170 },
    { id: "leads", header: "Lead", cell: ({ row }) => <span className="tnum">{row.original.lead_count}</span>, size: 70 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={LISTING_STATUS[row.original.status]?.tone ?? "neutral"}>{LISTING_STATUS[row.original.status]?.label ?? row.original.status}</Badge>, size: 120 },
  ], [t]);
  if (!propertyId) return <Alert variant="info">Pilih property (profile Apartment) di header.</Alert>;
  return (
    <div>
      <PageHeader title="Unit Sales · Listings" subtitle="Inventori penjualan unit milik property: harga, ketersediaan, dokumen. Reservasi & pipeline ada di Leads." actions={<>{can("commercial.sales_leads.view") && <Link to="/commercial/sales/leads"><Button variant="secondary"><Icon name="group" size={16} /> Leads</Button></Link>}{can("commercial.unit_listings.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Listing Baru</Button>}</>}>
        <div className="flex items-center gap-2">
          <Input className="w-64" placeholder="Cari kode / judul / unit…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-44" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option>{Object.entries(LISTING_STATUS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}</NativeSelect>
        </div>
      </PageHeader>
      {sum.data && (
        <div className="mb-5 grid grid-cols-5 gap-4">
          <MetricCard label="Available" value={String(sum.data.listings.published ?? 0)} tone="success" />
          <MetricCard label="Reserved" value={String(sum.data.listings.reserved ?? 0)} tone={(sum.data.listings.reserved ?? 0) > 0 ? "warning" : "neutral"} />
          <MetricCard label="Sold" value={String(sum.data.listings.sold ?? 0)} subValue={rp(sum.data.sold_value)} tone="primary" />
          <MetricCard label="Lead terbuka" value={String((sum.data.leads.new ?? 0) + (sum.data.leads.contacted ?? 0) + (sum.data.leads.qualified ?? 0))} tone="info" />
          <MetricCard label="Follow-up jatuh tempo" value={String(sum.data.follow_ups_due)} tone={sum.data.follow_ups_due > 0 ? "warning" : "neutral"} />
        </div>
      )}
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/commercial/sales/listings/${r.id}`} loading={list.isLoading} isFiltered={!!q || !!status} empty={{ message: "Belum ada listing penjualan." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      {edit && <ListingDialog propertyId={propertyId} listing={edit === "new" ? null : edit} onClose={() => setEdit(null)} onSaved={(l) => nav(`/commercial/sales/listings/${l.id}`)} />}
      {id && <ListingDrawer id={id} onClose={() => nav("/commercial/sales/listings")} onEdit={(l) => setEdit(l)} />}
    </div>
  );
}

export function ListingDrawer({ id, onClose, onEdit }: { id: string; onClose: () => void; onEdit?: (l: Listing) => void }) {
  const toast = useToast();
  const { can } = useAuth();
  const q = useOne<Listing>("unit-sales/listings", id);
  const act = useAction<{ id: string; action: string }, Listing>((i) => `unit-sales/listings/${i.id}/${i.action}`, { body: () => ({}), invalidate: ["list", "one", "all", "unit-sales"] });
  const run = (action: string) => act.mutateAsync({ id, action }).then((l) => toast.success(`${l.listing_code}: ${LISTING_STATUS[l.status]?.label}`)).catch(toast.error);
  return (
    <Drawer open onClose={onClose} title="Unit Listing" width={640}>
      <AsyncState query={q}>
        {(x) => (
          <div className="space-y-5">
            <div className="flex items-start justify-between gap-3">
              <div><div className="font-mono text-lg font-semibold">{x.listing_code}</div><div className="text-sm text-muted-foreground">Unit {x.unit_number} · {x.title}</div></div>
              <Badge tone={LISTING_STATUS[x.status]?.tone ?? "neutral"}>{LISTING_STATUS[x.status]?.label}</Badge>
            </div>
            <div className="flex flex-wrap gap-2">
              {x.allowed_actions.includes("publish") && can("commercial.unit_listings.publish") && <Button size="sm" onClick={() => run("publish")} loading={act.isPending} icon="publish">Publikasikan</Button>}
              {x.allowed_actions.includes("unpublish") && can("commercial.unit_listings.publish") && <Button size="sm" variant="secondary" onClick={() => run("unpublish")} loading={act.isPending}>Tarik (draft)</Button>}
              {x.allowed_actions.includes("archive") && can("commercial.unit_listings.archive") && <Button size="sm" variant="ghost" onClick={() => run("archive")} loading={act.isPending}>Arsipkan</Button>}
              {x.status !== "sold" && can("commercial.unit_listings.update") && onEdit && <Button size="sm" variant="secondary" onClick={() => onEdit(x)} icon="edit">Edit</Button>}
              {x.active_reservation_id && <Link to={`/commercial/sales/reservations/${x.active_reservation_id}`}><Button size="sm" variant="secondary">Lihat reservasi</Button></Link>}
              <Link to={`/commercial/sales/leads?listing_id=${x.id}`}><Button size="sm" variant="secondary">Leads ({x.lead_count})</Button></Link>
            </div>
            <KeyValue items={[
              { label: "Harga", value: <span className="tnum font-semibold">{rp(x.asking_price)}{x.price_negotiable ? " (nego)" : ""}</span> },
              { label: "Status unit", value: x.unit_occupancy_status }, { label: "Lantai", value: x.floor_name ?? "—" },
              { label: "Kamar tidur / mandi", value: `${x.bedrooms ?? "—"} / ${x.bathrooms ?? "—"}` }, { label: "Luas", value: x.area_m2 != null ? `${x.area_m2} m²` : "—" },
              { label: "Furnishing", value: x.furnishing ? FURNISHING[x.furnishing] : "—" }, { label: "Fitur", value: x.features.length ? x.features.join(", ") : "—" },
              { label: "Deskripsi", value: x.description ?? "—" }, { label: "Dokumen / referensi", value: <DocList docs={x.documents} /> },
              { label: "Dipublikasikan", value: x.published_at ? fmtD(x.published_at) : "—" }, { label: "Terjual", value: x.sold_at ? fmtD(x.sold_at) : "—" },
              { label: "Dibuat", value: <RelativeTime value={x.created_at} /> },
            ]} />
          </div>
        )}
      </AsyncState>
    </Drawer>
  );
}

export function ListingDialog({ propertyId, listing, onClose, onSaved }: { propertyId: string; listing: Listing | null; onClose: () => void; onSaved?: (l: Listing) => void }) {
  const toast = useToast();
  const [f, setF] = useState({ unit_location_id: listing?.unit_location_id ?? "", title: listing?.title ?? "", description: listing?.description ?? "", asking_price: listing ? String(listing.asking_price) : "", price_negotiable: listing?.price_negotiable ?? false, bedrooms: listing?.bedrooms != null ? String(listing.bedrooms) : "", bathrooms: listing?.bathrooms != null ? String(listing.bathrooms) : "", area_m2: listing?.area_m2 != null ? String(listing.area_m2) : "", furnishing: listing?.furnishing ?? "", features: listing?.features.join(", ") ?? "" });
  const [docs, setDocs] = useState<Doc[]>(listing?.documents ?? []);
  const create = useAction<Record<string, unknown>, Listing>(() => "unit-sales/listings", { invalidate: ["list", "one", "unit-sales"] });
  const update = useAction<Record<string, unknown>, Listing>(() => `unit-sales/listings/${listing?.id}`, { method: "PATCH", invalidate: ["list", "one", "unit-sales"] });
  const body = () => ({ ...(listing ? {} : { property_id: propertyId, unit_location_id: f.unit_location_id }), title: f.title.trim() || null, description: f.description || null, asking_price: num(f.asking_price), price_negotiable: f.price_negotiable, bedrooms: f.bedrooms === "" ? null : Number(f.bedrooms), bathrooms: f.bathrooms === "" ? null : Number(f.bathrooms), area_m2: f.area_m2 === "" ? null : Number(f.area_m2), furnishing: f.furnishing || null, features: f.features.split(",").map((s) => s.trim()).filter(Boolean), documents: docs });
  const submit = () => (listing ? update.mutateAsync(body()) : create.mutateAsync(body())).then((l) => { toast.success(`${l.listing_code} disimpan`); onClose(); onSaved?.(l); }).catch(toast.error);
  const pending = create.isPending || update.isPending;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={listing ? `Edit ${listing.listing_code}` : "Listing Penjualan Baru"} description="Unit dalam portfolio property. Harga dalam IDR; dokumen berupa referensi (nomor/link).">
        <div className="space-y-4">
          <Field label="Unit" required><UnitSelect propertyId={propertyId} value={f.unit_location_id} onChange={(v) => setF({ ...f, unit_location_id: v })} disabled={!!listing} /></Field>
          <Field label="Judul listing"><Input placeholder="mis. 2BR Tower Emerald 1201" value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Harga (IDR)" required><Input inputMode="numeric" value={f.asking_price} onChange={(e) => setF({ ...f, asking_price: e.target.value.replace(/[^\d]/g, "") })} /></Field>
            <div className="flex items-end pb-2"><Checkbox label="Harga bisa nego" checked={f.price_negotiable} onCheckedChange={(v) => setF({ ...f, price_negotiable: v })} /></div>
            <Field label="Kamar tidur"><Input type="number" min={0} value={f.bedrooms} onChange={(e) => setF({ ...f, bedrooms: e.target.value })} /></Field>
            <Field label="Kamar mandi"><Input type="number" min={0} value={f.bathrooms} onChange={(e) => setF({ ...f, bathrooms: e.target.value })} /></Field>
            <Field label="Luas (m²)"><Input type="number" min={0} step="0.5" value={f.area_m2} onChange={(e) => setF({ ...f, area_m2: e.target.value })} /></Field>
            <Field label="Furnishing"><NativeSelect value={f.furnishing} onChange={(e) => setF({ ...f, furnishing: e.target.value })}><option value="">—</option>{Object.entries(FURNISHING).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</NativeSelect></Field>
          </div>
          <Field label="Fitur (pisahkan koma)"><Input placeholder="balcony, city view, private lift" value={f.features} onChange={(e) => setF({ ...f, features: e.target.value })} /></Field>
          <Field label="Deskripsi"><Textarea rows={3} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
          <Field label="Dokumen / referensi"><DocumentsEditor value={docs} onChange={setDocs} /></Field>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={pending} disabled={(!listing && !f.unit_location_id) || !num(f.asking_price)} onClick={submit}>{listing ? "Simpan" : "Buat Listing"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
