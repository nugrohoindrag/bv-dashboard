// Tenant (PRD §18.1): daftar tenant + unit; detail drawer (route /tenant/tenants/:id) dengan occupant & service request tenant.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { StatusBadge } from "@/components/bv/badges";
import { AsyncState, KeyValue, useToast } from "@/components/bv/common";
import { useAll, useCreate, useList, useOne, useUpdate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import type { Location, ServiceRequest, Tenant } from "@/api/types";
import { CreateServiceRequestDialog } from "@/features/operations/FindingDialogs";

interface Occupant { id: string; tenant_id: string | null; full_name: string; phone: string | null; email: string | null; is_primary_contact: boolean; status: string; unit_ids: string[]; version: number }

export default function TenantListPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const { propertyId, can } = useAuth();
  const nav = useNavigate();
  const [q, setQ] = useState("");
  const [status, setStatus] = useState("");
  const list = useList<Tenant>("tenants", { property_id: propertyId ?? undefined, q: q || undefined, status: status || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [edit, setEdit] = useState<Tenant | null | "new">(null);
  const columns = useMemo<ColumnDef<Tenant, unknown>[]>(
    () => [
      { id: "code", header: "Kode", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.tenant_code}</span>, size: 130 },
      { id: "name", header: "Tenant", cell: ({ row }) => <div><div className="font-medium">{row.original.name}</div><div className="text-xs text-muted-foreground">{row.original.tenant_type} · {row.original.contact_name ?? "—"} {row.original.contact_phone ?? ""}</div></div> },
      { id: "units", header: "Unit", cell: ({ row }) => <span className="text-sm">{row.original.units.map((u) => u.unit_number).join(", ") || <span className="text-muted-foreground">—</span>}</span> },
      { id: "sr", header: "SR open", cell: ({ row }) => <span className={cn("tnum", row.original.open_requests > 0 && "font-semibold")}>{row.original.open_requests}</span>, size: 80 },
      { id: "status", header: t("label.status"), cell: ({ row }) => <span className={cn("text-xs", row.original.status === "active" ? "text-success-text" : "text-muted-foreground")}>{row.original.status}</span>, size: 90 },
    ],
    [t],
  );
  return (
    <div>
      <PageHeader title={t("nav.tenants")} actions={can("property.tenants.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Tambah Tenant</Button>}>
        <div className="flex items-center gap-2">
          <Input className="w-72" placeholder="Cari tenant / unit…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-40" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option><option value="active">Active</option><option value="inactive">Inactive</option><option value="moved_out">Moved out</option></NativeSelect>
        </div>
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/tenant/tenants/${r.id}`} loading={list.isLoading} isFiltered={!!q || !!status} empty={{ message: "Belum ada tenant." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} rowActions={(r) => (can("property.tenants.update") ? [{ label: "Edit", onSelect: () => setEdit(r) }] : [])} />
      {edit && <TenantDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
      {id && <TenantDrawer id={id} onClose={() => nav("/tenant/tenants")} onEdit={(tn) => setEdit(tn)} />}
    </div>
  );
}

function TenantDrawer({ id, onClose, onEdit }: { id: string; onClose: () => void; onEdit: (t: Tenant) => void }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const tenant = useOne<Tenant>("tenants", id);
  const occupants = useAll<Occupant>("occupants", { tenant_id: id });
  const srs = useAll<ServiceRequest>("service-requests", { tenant_id: id });
  const [srOpen, setSrOpen] = useState(false);
  const [occ, setOcc] = useState<Occupant | null | "new">(null);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={tenant.data ? `${tenant.data.tenant_code} · ${tenant.data.name}` : "Tenant"}>
        <AsyncState query={tenant}>
          {(tn) => (
            <div className="space-y-5">
              <KeyValue items={[{ label: "Tipe", value: tn.tenant_type }, { label: "Kontak", value: tn.contact_name }, { label: "Telepon", value: tn.contact_phone }, { label: "Email", value: tn.contact_email }, { label: "Status", value: tn.status }, { label: "Unit", value: tn.units.length ? <ul>{tn.units.map((u) => <li key={u.location_id}><Link to={`/property/locations/${u.location_id}`} className="text-brand-600 hover:underline">{u.unit_number}</Link> <span className="text-xs text-muted-foreground">{u.path_text}</span></li>)}</ul> : "—" }]} />
              <div>
                <div className="mb-2 flex items-center justify-between"><h3 className="text-h3 font-semibold">Occupant</h3>{can("property.occupants.create") && <Button size="sm" variant="secondary" onClick={() => setOcc("new")}><Icon name="add" size={16} /> Tambah</Button>}</div>
                <ul className="divide-y divide-border rounded-md border border-border">
                  {(occupants.data ?? []).map((o) => <li key={o.id} className="flex items-center justify-between px-3 py-2 text-sm"><span>{o.full_name}{o.is_primary_contact && <span className="ml-1 rounded-full bg-brand-50 px-1.5 text-[10px] text-brand-700">utama</span>}<span className="ml-2 text-xs text-muted-foreground">{o.phone ?? ""} {o.email ?? ""}</span></span>{can("property.occupants.update") && <Button size="sm" variant="ghost" onClick={() => setOcc(o)}>Edit</Button>}</li>)}
                  {(occupants.data ?? []).length === 0 && <li className="px-3 py-4 text-center text-xs text-muted-foreground">Belum ada occupant.</li>}
                </ul>
              </div>
              <div>
                <div className="mb-2 flex items-center justify-between"><h3 className="text-h3 font-semibold">Service Request</h3>{can("tenant.service_requests.create") && <Button size="sm" variant="secondary" onClick={() => setSrOpen(true)}><Icon name="add" size={16} /> {t("action.create_service_request")}</Button>}</div>
                <ul className="divide-y divide-border rounded-md border border-border">
                  {(srs.data ?? []).slice(0, 20).map((s) => <li key={s.id} className="flex items-center justify-between px-3 py-2 text-sm"><Link to={`/operations/service-requests/${s.id}`} className="hover:underline"><span className="font-mono text-xs font-semibold">{s.request_number}</span> {s.title}</Link><StatusBadge objectType="service_request" status={s.status} /></li>)}
                  {(srs.data ?? []).length === 0 && <li className="px-3 py-4 text-center text-xs text-muted-foreground">Belum ada service request.</li>}
                </ul>
              </div>
              <DialogFooter>
                {can("property.tenants.update") && <Button variant="secondary" onClick={() => onEdit(tn)}>Edit tenant</Button>}
                <Button onClick={onClose}>Tutup</Button>
              </DialogFooter>
              <CreateServiceRequestDialog open={srOpen} onOpenChange={setSrOpen} defaults={{ tenant_id: tn.id, location_id: tn.units[0]?.location_id ?? null }} />
              {occ && <OccupantDialog tenant={tn} item={occ === "new" ? null : occ} onClose={() => setOcc(null)} />}
            </div>
          )}
        </AsyncState>
      </DialogContent>
    </Dialog>
  );
}

function TenantDialog({ item, onClose }: { item: Tenant | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId, properties } = useAuth();
  const toast = useToast();
  const [pid, setPid] = useState(item?.property_id ?? propertyId ?? properties[0]?.id ?? "");
  const units = useAll<Location>("units", { property_id: pid || undefined }, { enabled: !!pid });
  const [form, setForm] = useState({ name: item?.name ?? "", tenant_type: item?.tenant_type ?? "company", contact_name: item?.contact_name ?? "", contact_phone: item?.contact_phone ?? "", contact_email: item?.contact_email ?? "", status: item?.status ?? "active", notes: "", unit_ids: item?.units.map((u) => u.location_id) ?? ([] as string[]) });
  const [uq, setUq] = useState("");
  const create = useCreate<Record<string, unknown>>("tenants");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("tenants");
  const submit = async () => {
    if (!form.name.trim()) return toast.error(new Error("Nama tenant wajib"));
    const body = { property_id: pid, name: form.name.trim(), tenant_type: form.tenant_type, contact_name: form.contact_name || null, contact_phone: form.contact_phone || null, contact_email: form.contact_email || null, status: form.status, notes: form.notes || null, unit_ids: form.unit_ids };
    try {
      if (item) await update.mutateAsync({ id: item.id, version: item.version, ...body });
      else await create.mutateAsync(body);
      toast.success("Tenant disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  const toggle = (id: string) => setForm((f) => ({ ...f, unit_ids: f.unit_ids.includes(id) ? f.unit_ids.filter((x) => x !== id) : [...f.unit_ids, id] }));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit ${item.tenant_code}` : "Tambah Tenant"}>
        <div className="space-y-4">
          {!item && properties.length > 1 && <Field label="Property" required><NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); setForm({ ...form, unit_ids: [] }); }}>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>}
          <div className="grid grid-cols-2 gap-3">
            <Field label="Nama tenant" required className="col-span-2"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></Field>
            <Field label="Tipe"><NativeSelect value={form.tenant_type} onChange={(e) => setForm({ ...form, tenant_type: e.target.value })}><option value="company">Company</option><option value="individual">Individual</option></NativeSelect></Field>
            <Field label={t("label.status")}><NativeSelect value={form.status} onChange={(e) => setForm({ ...form, status: e.target.value })}><option value="active">Active</option><option value="inactive">Inactive</option><option value="moved_out">Moved out</option></NativeSelect></Field>
            <Field label="Nama kontak"><Input value={form.contact_name} onChange={(e) => setForm({ ...form, contact_name: e.target.value })} /></Field>
            <Field label="Telepon"><Input value={form.contact_phone} onChange={(e) => setForm({ ...form, contact_phone: e.target.value })} /></Field>
            <Field label="Email" className="col-span-2"><Input type="email" value={form.contact_email} onChange={(e) => setForm({ ...form, contact_email: e.target.value })} /></Field>
          </div>
          <Field label={`Unit (${form.unit_ids.length})`}>
            <Input className="mb-2" placeholder="Filter unit…" value={uq} onChange={(e) => setUq(e.target.value)} />
            <div className="max-h-48 space-y-1 overflow-y-auto rounded-md border border-border p-2">
              {(units.data ?? []).filter((u) => !uq || u.name.toLowerCase().includes(uq.toLowerCase()) || u.path_text.toLowerCase().includes(uq.toLowerCase())).map((u) => (
                <label key={u.id} className="flex cursor-pointer items-center gap-2 text-sm"><Checkbox checked={form.unit_ids.includes(u.id)} onCheckedChange={() => toggle(u.id)} /> {u.name} <span className="text-xs text-muted-foreground">{u.path_text}</span></label>
              ))}
              {(units.data ?? []).length === 0 && <p className="text-xs text-muted-foreground">Belum ada unit di property ini.</p>}
            </div>
          </Field>
          {!item && <Field label="Catatan"><Textarea rows={2} value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} /></Field>}
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function OccupantDialog({ tenant, item, onClose }: { tenant: Tenant; item: Occupant | null; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [form, setForm] = useState({ full_name: item?.full_name ?? "", phone: item?.phone ?? "", email: item?.email ?? "", is_primary_contact: item?.is_primary_contact ?? false, status: item?.status ?? "active", unit_ids: item?.unit_ids ?? tenant.units.map((u) => u.location_id) });
  const create = useCreate<Record<string, unknown>>("occupants");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("occupants");
  const submit = async () => {
    if (!form.full_name.trim()) return toast.error(new Error("Nama wajib"));
    const body = { tenant_id: tenant.id, full_name: form.full_name.trim(), phone: form.phone || null, email: form.email || null, is_primary_contact: form.is_primary_contact, status: form.status, unit_ids: form.unit_ids };
    try {
      if (item) await update.mutateAsync({ id: item.id, version: item.version, ...body });
      else await create.mutateAsync(body);
      toast.success("Occupant disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={item ? "Edit Occupant" : "Tambah Occupant"}>
        <div className="space-y-4">
          <Field label="Nama lengkap" required><Input value={form.full_name} onChange={(e) => setForm({ ...form, full_name: e.target.value })} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Telepon"><Input value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} /></Field>
            <Field label="Email"><Input type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} /></Field>
          </div>
          <Field label="Unit">
            <div className="space-y-1">{tenant.units.map((u) => <label key={u.location_id} className="flex items-center gap-2 text-sm"><Checkbox checked={form.unit_ids.includes(u.location_id)} onCheckedChange={() => setForm((f) => ({ ...f, unit_ids: f.unit_ids.includes(u.location_id) ? f.unit_ids.filter((x) => x !== u.location_id) : [...f.unit_ids, u.location_id] }))} /> {u.unit_number}</label>)}</div>
          </Field>
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.is_primary_contact} onCheckedChange={(v) => setForm({ ...form, is_primary_contact: !!v })} /> Kontak utama</label>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
