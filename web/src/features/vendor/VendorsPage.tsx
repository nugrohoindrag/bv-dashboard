// Vendor Management (PRD P1 v1.3 §24; NC §37): vendor master, kontak, kategori layanan, kontrak, performance dari Work Order.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { StatusBadge } from "@/components/bv/badges";
import { AsyncState, KeyValue, useToast } from "@/components/bv/common";
import { useAction, useAll, useList, useOne, useUpdate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";

export interface VendorContact { id?: string; name: string; role: string | null; phone: string | null; email: string | null; is_primary: boolean }
export interface Vendor {
  id: string; vendor_code: string; name: string; service_categories: string[]; contact_name: string | null; contact_phone: string | null; contact_email: string | null; address: string | null; tax_id: string | null;
  contract_ref: string | null; contract_start: string | null; contract_end: string | null; status: string; notes: string | null; contacts: VendorContact[];
  performance: { total_work_orders: number; completed_work_orders: number; open_work_orders: number; on_time_pct: number | null; avg_completion_hours: number | null; reopen_count: number; last_90d_work_orders: number }; created_at: string; version: number;
}
export const VENDOR_CATEGORIES: Record<string, string> = { hvac: "HVAC", electrical: "Electrical", plumbing: "Plumbing", lift: "Lift", cleaning: "Cleaning", security: "Security", pest_control: "Pest Control", civil: "Sipil", it: "IT", landscaping: "Landscaping", fire_protection: "Fire Protection", other: "Lainnya" };

export default function VendorsPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const { can } = useAuth();
  const [q, setQ] = useState("");
  const [category, setCategory] = useState("");
  const [status, setStatus] = useState("active");
  const [edit, setEdit] = useState<Vendor | "new" | null>(null);
  const list = useList<Vendor>("vendors", { q: q || undefined, category: category || undefined, status: status || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<Vendor, unknown>[]>(() => [
    { id: "code", header: "Kode", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.vendor_code}</span>, size: 120 },
    { id: "name", header: "Vendor", cell: ({ row }) => <div><div className="font-medium">{row.original.name}</div><div className="text-xs text-muted-foreground">{row.original.contact_name ?? ""} {row.original.contact_phone ?? ""}</div></div> },
    { id: "categories", header: "Layanan", cell: ({ row }) => <div className="flex flex-wrap gap-1">{row.original.service_categories.map((c) => <Badge key={c} tone="neutral">{VENDOR_CATEGORIES[c] ?? c}</Badge>)}</div> },
    { id: "perf", header: "Performance", cell: ({ row }) => <div className="text-xs"><span className="tnum font-semibold">{row.original.performance.completed_work_orders}</span>/{row.original.performance.total_work_orders} WO selesai{row.original.performance.on_time_pct != null ? ` · on-time ${row.original.performance.on_time_pct.toFixed(0)}%` : ""}{row.original.performance.open_work_orders ? ` · ${row.original.performance.open_work_orders} berjalan` : ""}</div>, size: 220 },
    { id: "contract", header: "Kontrak", cell: ({ row }) => <span className="text-xs text-muted-foreground">{row.original.contract_ref ?? "—"}{row.original.contract_end ? ` · s/d ${row.original.contract_end.slice(0, 10)}` : ""}</span>, size: 180 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={row.original.status === "active" ? "success" : row.original.status === "blacklisted" ? "error" : "neutral"}>{row.original.status}</Badge>, size: 110 },
  ], [t]);
  return (
    <div>
      <PageHeader title="Vendors" subtitle="Vendor eksternal untuk Vendor Work Order; performance dihitung dari Work Order yang ditugaskan." actions={can("vendor.vendors.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Tambah Vendor</Button>}>
        <div className="flex items-center gap-2">
          <Input className="w-64" placeholder="Cari vendor / kontak…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-44" value={category} onChange={(e) => setCategory(e.target.value)}><option value="">Layanan: {t("label.all")}</option>{Object.entries(VENDOR_CATEGORIES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</NativeSelect>
          <NativeSelect className="w-40" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option><option value="active">active</option><option value="inactive">inactive</option><option value="blacklisted">blacklisted</option></NativeSelect>
        </div>
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/vendors/${r.id}`} loading={list.isLoading} isFiltered={!!q || !!category} empty={{ message: "Belum ada vendor." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} rowActions={(r) => (can("vendor.vendors.update") ? [{ label: "Edit", icon: "edit", onSelect: () => setEdit(r) }] : [])} />
      {edit && <VendorDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
      {id && <VendorDrawer id={id} onClose={() => nav("/vendors")} onEdit={(v) => setEdit(v)} />}
    </div>
  );
}

interface WORow { id: string; work_order_number: string; title: string; status: string; priority: string; vendor_assigned_at: string | null; due_at: string | null; completed_at: string | null; on_time: boolean | null }

function VendorDrawer({ id, onClose, onEdit }: { id: string; onClose: () => void; onEdit: (v: Vendor) => void }) {
  const { can } = useAuth();
  const toast = useToast();
  const v = useOne<Vendor>("vendors", id);
  const wos = useAll<WORow>(`vendors/${id}/work-orders`);
  return (
    <Drawer open onClose={onClose} title="Vendor" width={680}>
      <AsyncState query={v}>
        {(x) => (
          <div className="space-y-5">
            <div className="flex items-start justify-between gap-3">
              <div><div className="text-lg font-semibold">{x.name}</div><div className="font-mono text-sm text-muted-foreground">{x.vendor_code}</div></div>
              <div className="flex items-center gap-2"><Badge tone={x.status === "active" ? "success" : "neutral"}>{x.status}</Badge>{can("vendor.vendors.update") && <Button size="sm" variant="secondary" onClick={() => onEdit(x)}>Edit</Button>}{can("vendor.vendors.deactivate") && x.status === "active" && <Button size="sm" variant="ghost" onClick={() => api(`vendors/${id}`, { method: "DELETE" }).then(() => { toast.success("Vendor dinonaktifkan"); onClose(); }).catch(toast.error)}>Nonaktifkan</Button>}</div>
            </div>
            <div className="grid grid-cols-4 gap-3">
              {[{ l: "Total WO", v: x.performance.total_work_orders }, { l: "Selesai", v: x.performance.completed_work_orders }, { l: "On-time", v: x.performance.on_time_pct != null ? `${x.performance.on_time_pct.toFixed(0)}%` : "—" }, { l: "Rata-rata (jam)", v: x.performance.avg_completion_hours != null ? x.performance.avg_completion_hours.toFixed(1) : "—" }].map((m) => (
                <div key={m.l} className="rounded-[var(--radius-md)] bg-surface-container p-3"><div className="text-xs text-muted-foreground">{m.l}</div><div className="tnum text-xl font-semibold">{m.v}</div></div>
              ))}
            </div>
            <KeyValue items={[
              { label: "Layanan", value: x.service_categories.map((c) => VENDOR_CATEGORIES[c] ?? c).join(", ") || "—" },
              { label: "Kontak utama", value: `${x.contact_name ?? "—"} ${x.contact_phone ?? ""} ${x.contact_email ?? ""}` },
              { label: "Alamat", value: x.address ?? "—" }, { label: "NPWP", value: x.tax_id ?? "—" },
              { label: "Kontrak", value: x.contract_ref ? `${x.contract_ref} (${x.contract_start?.slice(0, 10) ?? "—"} – ${x.contract_end?.slice(0, 10) ?? "—"})` : "—" },
              { label: "Reopen WO", value: String(x.performance.reopen_count) }, { label: "WO 90 hari", value: String(x.performance.last_90d_work_orders) },
            ]} />
            {x.contacts.length > 0 && <div><div className="mb-1 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Kontak</div><ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">{x.contacts.map((c, i) => <li key={c.id ?? i} className="px-3 py-1.5">{c.name} {c.role ? `· ${c.role}` : ""} {c.phone ?? ""} {c.email ?? ""} {c.is_primary && <Badge tone="primary" className="ml-1">utama</Badge>}</li>)}</ul></div>}
            <div>
              <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Riwayat Vendor Work Order</div>
              <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
                {(wos.data ?? []).map((w) => <li key={w.id} className="flex items-center justify-between gap-2 px-3 py-1.5"><span><Link to={`/operations/work-orders/${w.id}`} className="font-mono text-primary hover:underline">{w.work_order_number}</Link> {w.title}</span><span className="flex items-center gap-2 text-xs text-muted-foreground">{w.on_time === true ? <Badge tone="success">on-time</Badge> : w.on_time === false ? <Badge tone="warning">terlambat</Badge> : null}{w.completed_at ? fmtDateTime(w.completed_at) : ""}<StatusBadge objectType="work_order" status={w.status} /></span></li>)}
                {(wos.data ?? []).length === 0 && <li className="px-3 py-2 text-muted-foreground">Belum ada Work Order.</li>}
              </ul>
            </div>
          </div>
        )}
      </AsyncState>
    </Drawer>
  );
}

function VendorDialog({ item, onClose }: { item: Vendor | null; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [f, setF] = useState({ name: item?.name ?? "", service_categories: item?.service_categories ?? [], contact_name: item?.contact_name ?? "", contact_phone: item?.contact_phone ?? "", contact_email: item?.contact_email ?? "", address: item?.address ?? "", tax_id: item?.tax_id ?? "", contract_ref: item?.contract_ref ?? "", contract_start: item?.contract_start?.slice(0, 10) ?? "", contract_end: item?.contract_end?.slice(0, 10) ?? "", status: item?.status ?? "active", notes: item?.notes ?? "" });
  const create = useAction<Record<string, unknown>, Vendor>(() => "vendors", { invalidate: ["list", "one"] });
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("vendors");
  const submit = async () => {
    const body: Record<string, unknown> = { name: f.name.trim(), service_categories: f.service_categories, contact_name: f.contact_name || null, contact_phone: f.contact_phone || null, contact_email: f.contact_email || null, address: f.address || null, tax_id: f.tax_id || null, contract_ref: f.contract_ref || null, contract_start: f.contract_start || null, contract_end: f.contract_end || null, notes: f.notes || null };
    try {
      if (item) await update.mutateAsync({ id: item.id, version: item.version, ...body, status: f.status });
      else await create.mutateAsync(body);
      toast.success("Vendor disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit ${item.name}` : "Tambah Vendor"}>
        <div className="space-y-4">
          <Field label="Nama vendor" required><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Kategori layanan"><div className="flex flex-wrap gap-2">{Object.entries(VENDOR_CATEGORIES).map(([k, v]) => <Checkbox key={k} label={v} checked={f.service_categories.includes(k)} onCheckedChange={(on) => setF({ ...f, service_categories: on ? [...f.service_categories, k] : f.service_categories.filter((x) => x !== k) })} />)}</div></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Nama kontak"><Input value={f.contact_name} onChange={(e) => setF({ ...f, contact_name: e.target.value })} /></Field>
            <Field label="Telepon"><Input value={f.contact_phone} onChange={(e) => setF({ ...f, contact_phone: e.target.value })} /></Field>
            <Field label="Email"><Input value={f.contact_email} onChange={(e) => setF({ ...f, contact_email: e.target.value })} /></Field>
            <Field label="NPWP"><Input value={f.tax_id} onChange={(e) => setF({ ...f, tax_id: e.target.value })} /></Field>
            <Field label="No. kontrak"><Input value={f.contract_ref} onChange={(e) => setF({ ...f, contract_ref: e.target.value })} /></Field>
            {item && <Field label="Status"><NativeSelect value={f.status} onChange={(e) => setF({ ...f, status: e.target.value })}><option value="active">active</option><option value="inactive">inactive</option><option value="blacklisted">blacklisted</option></NativeSelect></Field>}
            <Field label="Kontrak mulai"><Input type="date" value={f.contract_start} onChange={(e) => setF({ ...f, contract_start: e.target.value })} /></Field>
            <Field label="Kontrak selesai"><Input type="date" value={f.contract_end} onChange={(e) => setF({ ...f, contract_end: e.target.value })} /></Field>
          </div>
          <Field label="Alamat"><Textarea rows={2} value={f.address} onChange={(e) => setF({ ...f, address: e.target.value })} /></Field>
          <Field label="Catatan"><Textarea rows={2} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} disabled={!f.name.trim()} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
