// Commercial › Unit Rental Management › Listings (PRD P1 v1.3 §3.10: rental inventory, rental availability, rental rate
// configuration daily/weekly/monthly). Unit dalam portfolio property.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { MetricCard } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useList, useOne } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { FURNISHING, RLISTING_STATUS, fmtD, num, rp, type Doc, type RentalListing, type RentalSummary } from "./commercial-api";
import { DocList, DocumentsEditor, UnitSelect } from "./shared";

export function useRentalSummary(propertyId: string | null) {
  return useQuery({ queryKey: ["unit-rental-summary", propertyId], enabled: !!propertyId, queryFn: () => api<RentalSummary>("unit-rental/summary", { query: { property_id: propertyId } }) });
}

const rates = (l: RentalListing) => [l.rate_daily ? `${rp(l.rate_daily)}/hari` : null, l.rate_weekly ? `${rp(l.rate_weekly)}/minggu` : null, l.rate_monthly ? `${rp(l.rate_monthly)}/bulan` : null].filter(Boolean).join(" · ");

export default function RentalListingsPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const { propertyId, can } = useAuth();
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const [edit, setEdit] = useState<RentalListing | "new" | null>(null);
  const list = useList<RentalListing>("unit-rental/listings", { property_id: propertyId ?? undefined, status: status || undefined, q: q || undefined }, { enabled: !!propertyId });
  const sum = useRentalSummary(propertyId);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<RentalListing, unknown>[]>(() => [
    { id: "code", header: "Listing", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.listing_code}</span>, size: 150 },
    { id: "unit", header: "Unit", cell: ({ row }) => <div><div className="font-medium">{row.original.unit_number} · {row.original.title}</div><div className="text-xs text-muted-foreground">{row.original.floor_name ?? ""}{row.original.bedrooms != null ? ` · ${row.original.bedrooms} KT` : ""}{row.original.furnishing ? ` · ${FURNISHING[row.original.furnishing]}` : ""} · min {row.original.min_stay_days} hari</div></div> },
    { id: "rates", header: "Tarif", cell: ({ row }) => <span className="tnum text-sm">{rates(row.original)}</span>, size: 300 },
    { id: "occ", header: "Hunian", cell: ({ row }) => row.original.active_reservation_id ? <Badge tone="primary">Disewa</Badge> : row.original.next_start_date ? <span className="text-xs">Mulai {fmtD(row.original.next_start_date)}</span> : <span className="text-xs text-muted-foreground">{row.original.unit_occupancy_status}</span>, size: 120 },
    { id: "inq", header: "Inquiry", cell: ({ row }) => <span className="tnum">{row.original.open_inquiries}</span>, size: 80 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={RLISTING_STATUS[row.original.status]?.tone ?? "neutral"}>{RLISTING_STATUS[row.original.status]?.label ?? row.original.status}</Badge>, size: 110 },
  ], [t]);
  if (!propertyId) return <Alert variant="info">Pilih property (profile Apartment) di header.</Alert>;
  return (
    <div>
      <PageHeader title="Unit Rental · Listings" subtitle="Inventori sewa unit milik property dengan tarif harian / mingguan / bulanan." actions={<>{can("commercial.rental_reservations.view") && <Link to="/commercial/rental/reservations"><Button variant="secondary"><Icon name="event_available" size={16} /> Reservasi</Button></Link>}{can("commercial.rental_listings.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Listing Sewa Baru</Button>}</>}>
        <div className="flex items-center gap-2">
          <Input className="w-64" placeholder="Cari kode / judul / unit…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-40" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option>{Object.entries(RLISTING_STATUS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}</NativeSelect>
        </div>
      </PageHeader>
      {sum.data && (
        <div className="mb-5 grid grid-cols-5 gap-4">
          <MetricCard label="Published" value={String(sum.data.listings.published ?? 0)} tone="success" />
          <MetricCard label="Sewa aktif" value={String(sum.data.active_rentals)} subValue={`${sum.data.occupancy_pct.toFixed(0)}% dari listing`} tone="primary" />
          <MetricCard label="Mulai ≤ 7 hari" value={String(sum.data.upcoming_start)} tone={sum.data.upcoming_start > 0 ? "info" : "neutral"} />
          <MetricCard label="Berakhir ≤ 7 hari" value={String(sum.data.ending_soon)} tone={sum.data.ending_soon > 0 ? "warning" : "neutral"} />
          <MetricCard label="Inquiry terbuka" value={String(sum.data.open_inquiries)} tone={sum.data.open_inquiries > 0 ? "warning" : "neutral"} />
        </div>
      )}
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/commercial/rental/listings/${r.id}`} loading={list.isLoading} isFiltered={!!q || !!status} empty={{ message: "Belum ada listing sewa." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      {edit && <RentalListingDialog propertyId={propertyId} listing={edit === "new" ? null : edit} onClose={() => setEdit(null)} onSaved={(l) => nav(`/commercial/rental/listings/${l.id}`)} />}
      {id && <RentalListingDrawer id={id} onClose={() => nav("/commercial/rental/listings")} onEdit={(l) => setEdit(l)} />}
    </div>
  );
}

export function RentalListingDrawer({ id, onClose, onEdit }: { id: string; onClose: () => void; onEdit?: (l: RentalListing) => void }) {
  const toast = useToast();
  const { can } = useAuth();
  const q = useOne<RentalListing>("unit-rental/listings", id);
  const act = useAction<{ action: string }, RentalListing>((i) => `unit-rental/listings/${id}/${i.action}`, { body: () => ({}), invalidate: ["list", "one", "all", "unit-rental"] });
  const run = (action: string) => act.mutateAsync({ action }).then((l) => toast.success(`${l.listing_code}: ${RLISTING_STATUS[l.status]?.label}`)).catch(toast.error);
  return (
    <Drawer open onClose={onClose} title="Rental Listing" width={640}>
      <AsyncState query={q}>
        {(x) => (
          <div className="space-y-5">
            <div className="flex items-start justify-between gap-3">
              <div><div className="font-mono text-lg font-semibold">{x.listing_code}</div><div className="text-sm text-muted-foreground">Unit {x.unit_number} · {x.title}</div></div>
              <Badge tone={RLISTING_STATUS[x.status]?.tone ?? "neutral"}>{RLISTING_STATUS[x.status]?.label}</Badge>
            </div>
            <div className="flex flex-wrap gap-2">
              {x.allowed_actions.includes("publish") && can("commercial.rental_listings.publish") && <Button size="sm" onClick={() => run("publish")} loading={act.isPending} icon="publish">Publikasikan</Button>}
              {x.allowed_actions.includes("unpublish") && can("commercial.rental_listings.publish") && <Button size="sm" variant="secondary" onClick={() => run("unpublish")} loading={act.isPending}>Tarik (draft)</Button>}
              {x.allowed_actions.includes("archive") && can("commercial.rental_listings.archive") && <Button size="sm" variant="ghost" onClick={() => run("archive")} loading={act.isPending}>Arsipkan</Button>}
              {can("commercial.rental_listings.update") && onEdit && <Button size="sm" variant="secondary" onClick={() => onEdit(x)} icon="edit">Edit</Button>}
              {can("commercial.rental_reservations.view") && <Link to={`/commercial/rental/reservations?listing_id=${x.id}`}><Button size="sm" variant="secondary">Reservasi</Button></Link>}
              {x.active_reservation_id && <Link to={`/commercial/rental/reservations/${x.active_reservation_id}`}><Button size="sm" variant="secondary">Sewa aktif</Button></Link>}
            </div>
            <KeyValue items={[
              { label: "Tarif", value: <span className="tnum">{rates(x)}</span> }, { label: "Deposit", value: rp(x.deposit_amount) },
              { label: "Minimal sewa", value: `${x.min_stay_days} hari` }, { label: "Maks. penghuni", value: x.max_occupants ?? "—" },
              { label: "Tersedia", value: `${x.available_from ? fmtD(x.available_from) : "sekarang"} – ${x.available_until ? fmtD(x.available_until) : "tanpa batas"}` },
              { label: "Status unit", value: x.unit_occupancy_status }, { label: "Lantai", value: x.floor_name ?? "—" },
              { label: "Kamar tidur / mandi", value: `${x.bedrooms ?? "—"} / ${x.bathrooms ?? "—"}` }, { label: "Luas", value: x.area_m2 != null ? `${x.area_m2} m²` : "—" },
              { label: "Furnishing", value: x.furnishing ? FURNISHING[x.furnishing] : "—" }, { label: "Fitur", value: x.features.length ? x.features.join(", ") : "—" },
              { label: "Deskripsi", value: x.description ?? "—" }, { label: "Dokumen / referensi", value: <DocList docs={x.documents} /> },
              { label: "Inquiry terbuka", value: String(x.open_inquiries) }, { label: "Dibuat", value: <RelativeTime value={x.created_at} /> },
            ]} />
          </div>
        )}
      </AsyncState>
    </Drawer>
  );
}

export function RentalListingDialog({ propertyId, listing, onClose, onSaved }: { propertyId: string; listing: RentalListing | null; onClose: () => void; onSaved?: (l: RentalListing) => void }) {
  const toast = useToast();
  const s = (v: number | null | undefined) => (v == null ? "" : String(v));
  const [f, setF] = useState({ unit_location_id: listing?.unit_location_id ?? "", title: listing?.title ?? "", description: listing?.description ?? "", rate_daily: s(listing?.rate_daily), rate_weekly: s(listing?.rate_weekly), rate_monthly: s(listing?.rate_monthly), deposit_amount: s(listing?.deposit_amount), min_stay_days: s(listing?.min_stay_days ?? 1), max_occupants: s(listing?.max_occupants), bedrooms: s(listing?.bedrooms), bathrooms: s(listing?.bathrooms), area_m2: s(listing?.area_m2), furnishing: listing?.furnishing ?? "", features: listing?.features.join(", ") ?? "", available_from: listing?.available_from?.slice(0, 10) ?? "", available_until: listing?.available_until?.slice(0, 10) ?? "" });
  const [docs, setDocs] = useState<Doc[]>(listing?.documents ?? []);
  const create = useAction<Record<string, unknown>, RentalListing>(() => "unit-rental/listings", { invalidate: ["list", "one", "unit-rental"] });
  const update = useAction<Record<string, unknown>, RentalListing>(() => `unit-rental/listings/${listing?.id}`, { method: "PATCH", invalidate: ["list", "one", "unit-rental"] });
  const anyRate = !!(num(f.rate_daily) || num(f.rate_weekly) || num(f.rate_monthly));
  const body = () => ({ ...(listing ? {} : { property_id: propertyId, unit_location_id: f.unit_location_id }), title: f.title.trim() || null, description: f.description || null, rate_daily: num(f.rate_daily) || (listing ? 0 : null), rate_weekly: num(f.rate_weekly) || (listing ? 0 : null), rate_monthly: num(f.rate_monthly) || (listing ? 0 : null), deposit_amount: num(f.deposit_amount) ?? 0, min_stay_days: Number(f.min_stay_days) || 1, max_occupants: f.max_occupants === "" ? null : Number(f.max_occupants), bedrooms: f.bedrooms === "" ? null : Number(f.bedrooms), bathrooms: f.bathrooms === "" ? null : Number(f.bathrooms), area_m2: f.area_m2 === "" ? null : Number(f.area_m2), furnishing: f.furnishing || null, features: f.features.split(",").map((x) => x.trim()).filter(Boolean), available_from: f.available_from || (listing ? "" : null), available_until: f.available_until || (listing ? "" : null), documents: docs });
  const submit = () => (listing ? update.mutateAsync(body()) : create.mutateAsync(body())).then((l) => { toast.success(`${l.listing_code} disimpan`); onClose(); onSaved?.(l); }).catch(toast.error);
  const money = (k: "rate_daily" | "rate_weekly" | "rate_monthly" | "deposit_amount") => <Input inputMode="numeric" value={f[k]} onChange={(e) => setF({ ...f, [k]: e.target.value.replace(/[^\d]/g, "") })} />;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={listing ? `Edit ${listing.listing_code}` : "Listing Sewa Baru"} description="Rental rate configuration: isi tarif untuk periode yang ditawarkan (kosongkan yang tidak ditawarkan). Minimal satu tarif.">
        <div className="space-y-4">
          <Field label="Unit" required><UnitSelect propertyId={propertyId} value={f.unit_location_id} onChange={(v) => setF({ ...f, unit_location_id: v })} disabled={!!listing} /></Field>
          <Field label="Judul listing"><Input placeholder="mis. Studio furnished Tower Emerald" value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} /></Field>
          <div className="grid grid-cols-3 gap-3">
            <Field label="Tarif harian (IDR)">{money("rate_daily")}</Field>
            <Field label="Tarif mingguan (IDR)">{money("rate_weekly")}</Field>
            <Field label="Tarif bulanan (IDR)">{money("rate_monthly")}</Field>
            <Field label="Deposit (IDR)">{money("deposit_amount")}</Field>
            <Field label="Minimal sewa (hari)"><Input type="number" min={1} value={f.min_stay_days} onChange={(e) => setF({ ...f, min_stay_days: e.target.value })} /></Field>
            <Field label="Maks. penghuni"><Input type="number" min={1} value={f.max_occupants} onChange={(e) => setF({ ...f, max_occupants: e.target.value })} /></Field>
            <Field label="Tersedia mulai"><Input type="date" value={f.available_from} onChange={(e) => setF({ ...f, available_from: e.target.value })} /></Field>
            <Field label="Tersedia sampai"><Input type="date" value={f.available_until} onChange={(e) => setF({ ...f, available_until: e.target.value })} /></Field>
            <Field label="Furnishing"><NativeSelect value={f.furnishing} onChange={(e) => setF({ ...f, furnishing: e.target.value })}><option value="">—</option>{Object.entries(FURNISHING).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</NativeSelect></Field>
            <Field label="Kamar tidur"><Input type="number" min={0} value={f.bedrooms} onChange={(e) => setF({ ...f, bedrooms: e.target.value })} /></Field>
            <Field label="Kamar mandi"><Input type="number" min={0} value={f.bathrooms} onChange={(e) => setF({ ...f, bathrooms: e.target.value })} /></Field>
            <Field label="Luas (m²)"><Input type="number" min={0} step="0.5" value={f.area_m2} onChange={(e) => setF({ ...f, area_m2: e.target.value })} /></Field>
          </div>
          <Field label="Fitur (pisahkan koma)"><Input value={f.features} onChange={(e) => setF({ ...f, features: e.target.value })} /></Field>
          <Field label="Deskripsi"><Textarea rows={3} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
          <Field label="Dokumen / referensi"><DocumentsEditor value={docs} onChange={setDocs} /></Field>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={create.isPending || update.isPending} disabled={(!listing && !f.unit_location_id) || !anyRate} onClick={submit}>{listing ? "Simpan" : "Buat Listing"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
