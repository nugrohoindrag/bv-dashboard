// Tenant Relation › Tenant Users (PRD P1 v1.3 §7 Tenant Identity & Access; kontrak PWA §2.1 "Validasi Account"):
// validasi pendaftaran dari Tenant App, buat akun untuk occupant, kelola akses unit/area, suspend/reactivate.
import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useAll, useList, useOne } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useProfile } from "@/lib/profile";
import type { Tenant } from "@/api/types";
import { TreeView } from "@/features/property/LocationsPage";

export interface TenantAccess { id: string; access_id: string; location_type: string; name: string; code: string; unit_number?: string | null; path_text: string; is_primary: boolean; access_type: string }
export interface TenantUser {
  id: string; user_id: string; full_name: string; email: string | null; phone: string | null; property_id: string; property_name: string;
  tenant_id: string | null; tenant_name: string | null; occupant_id: string | null; role: string; status: string; registration_source: string;
  ownership_status: string | null; requested_at: string; validated_at: string | null; validated_by_name: string | null; rejection_reason: string | null;
  suspension_reason: string | null; last_seen_at: string | null; access: TenantAccess[]; open_requests: number; allowed_actions: string[]; version: number;
}

const STATUS_TONE: Record<string, "warning" | "success" | "error" | "neutral"> = { pending_validation: "warning", active: "success", rejected: "error", suspended: "neutral" };
const STATUS_LABEL: Record<string, string> = { pending_validation: "Menunggu validasi", active: "Aktif", rejected: "Ditolak", suspended: "Ditangguhkan" };

export default function TenantUsersPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const { propertyId, can } = useAuth();
  const prof = useProfile();
  const [status, setStatus] = useState("pending_validation");
  const [q, setQ] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const list = useList<TenantUser>("tenant-users", { property_id: propertyId ?? undefined, status: status || undefined, q: q || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<TenantUser, unknown>[]>(() => [
    { id: "name", header: prof.term("customer", "id") + " user", cell: ({ row }) => <div><div className="font-medium">{row.original.full_name}</div><div className="text-xs text-muted-foreground">{row.original.email} {row.original.phone ? `· ${row.original.phone}` : ""}</div></div> },
    { id: "unit", header: prof.term("inventory_unit", "id"), cell: ({ row }) => <span className="text-sm">{row.original.access.filter((a) => a.access_type === "unit").map((a) => a.unit_number ?? a.name).join(", ") || <span className="text-muted-foreground">—</span>}</span> },
    { id: "tenant", header: t("label.tenant"), cell: ({ row }) => <span className="text-sm">{row.original.tenant_name ?? <span className="text-muted-foreground">—</span>}</span> },
    { id: "role", header: "Role", cell: ({ row }) => <span className="text-xs">{row.original.role === "tenant_admin" ? "Tenant Admin" : "Tenant User"}{row.original.ownership_status ? ` · ${row.original.ownership_status}` : ""}</span>, size: 150 },
    { id: "requested_at", header: "Didaftarkan", cell: ({ row }) => <span className="text-xs text-muted-foreground"><RelativeTime value={row.original.requested_at} /> · {row.original.registration_source}</span>, size: 160 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={STATUS_TONE[row.original.status] ?? "neutral"}>{STATUS_LABEL[row.original.status] ?? row.original.status}</Badge>, size: 150 },
  ], [t, prof]);
  return (
    <div>
      <PageHeader title="Tenant Users" subtitle="Akun Tenant App: validasi pendaftaran, akses unit, status akun." actions={can("tenant_relation.tenant_users.update") && <Button onClick={() => setCreateOpen(true)} disabled={!propertyId}><Icon name="person_add" size={16} /> Buat Akun Tenant</Button>}>
        <div className="flex items-center gap-2">
          <Input className="w-72" placeholder="Cari nama / email / telepon…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-52" value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">Status: {t("label.all")}</option>
            <option value="pending_validation">Menunggu validasi</option>
            <option value="active">Aktif</option>
            <option value="suspended">Ditangguhkan</option>
            <option value="rejected">Ditolak</option>
          </NativeSelect>
        </div>
      </PageHeader>
      {!propertyId && <Alert variant="info" className="mb-4">Pilih property di header untuk membuat akun tenant.</Alert>}
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/tenant-relation/tenant-users/${r.id}`} loading={list.isLoading} isFiltered={!!q || !!status} empty={{ message: status === "pending_validation" ? "Tidak ada pendaftaran yang menunggu validasi." : "Belum ada akun tenant." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      {id && <TenantUserDrawer id={id} onClose={() => nav("/tenant-relation/tenant-users")} />}
      {createOpen && propertyId && <CreateTenantUserDialog propertyId={propertyId} onClose={() => setCreateOpen(false)} />}
    </div>
  );
}

function TenantUserDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const tu = useOne<TenantUser>("tenant-users", id);
  const decide = useAction<{ id: string; action: string; reason?: string }, TenantUser>((i) => `tenant-users/${i.id}/${i.action}`, { body: (i) => ({ reason: i.reason }), invalidate: ["list", "one", "notifications"] });
  const grant = useAction<{ id: string; location_id: string; is_primary: boolean }, TenantUser>((i) => `tenant-users/${i.id}/access`, { body: (i) => ({ location_id: i.location_id, is_primary: i.is_primary }), invalidate: ["list", "one"] });
  const [reasonFor, setReasonFor] = useState<"reject" | "suspend" | null>(null);
  const [reason, setReason] = useState("");
  const [pickLoc, setPickLoc] = useState<string | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  const run = (action: string, r?: string) => decide.mutateAsync({ id, action, reason: r }).then(() => { toast.success(`Akun ${STATUS_LABEL[action === "approve" || action === "reactivate" ? "active" : action === "reject" ? "rejected" : "suspended"].toLowerCase()}`); setReasonFor(null); setReason(""); }).catch(toast.error);
  const revoke = (accessId: string) => api(`tenant-users/${id}/access/${accessId}`, { method: "DELETE" }).then(() => { toast.success("Akses dicabut"); tu.refetch(); }).catch(toast.error);
  return (
    <Drawer open onClose={onClose} title="Akun Tenant" width={640}>
      <AsyncState query={tu}>
        {(u) => (
          <div className="space-y-5">
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="text-lg font-semibold">{u.full_name}</div>
                <div className="text-sm text-muted-foreground">{u.email} {u.phone ? `· ${u.phone}` : ""}</div>
              </div>
              <Badge tone={STATUS_TONE[u.status] ?? "neutral"}>{STATUS_LABEL[u.status] ?? u.status}</Badge>
            </div>
            {u.status === "pending_validation" && <Alert variant="warning" title="Menunggu validasi">Periksa identitas pendaftar dan unit yang diklaim sebelum menyetujui. Setelah disetujui, tenant dapat login ke Tenant App dan melihat data unit tersebut.</Alert>}
            {u.rejection_reason && <Alert variant="critical" title="Ditolak">{u.rejection_reason}</Alert>}
            {u.suspension_reason && <Alert variant="warning" title="Ditangguhkan">{u.suspension_reason}</Alert>}
            <div className="flex flex-wrap gap-2">
              {u.allowed_actions.includes("approve") && <Button onClick={() => run("approve")} loading={decide.isPending} icon="check">Setujui</Button>}
              {u.allowed_actions.includes("reject") && <Button variant="secondary" onClick={() => setReasonFor("reject")}>Tolak…</Button>}
              {u.allowed_actions.includes("suspend") && <Button variant="secondary" onClick={() => setReasonFor("suspend")}>Tangguhkan…</Button>}
              {u.allowed_actions.includes("reactivate") && <Button onClick={() => run("reactivate")} loading={decide.isPending}>Aktifkan kembali</Button>}
            </div>
            <KeyValue items={[
              { label: "Property", value: u.property_name },
              { label: t("label.tenant"), value: u.tenant_name ?? "—" },
              { label: "Role", value: u.role === "tenant_admin" ? "Tenant Admin" : "Tenant User" },
              { label: "Hubungan", value: u.ownership_status ?? "—" },
              { label: "Sumber", value: u.registration_source },
              { label: "Didaftarkan", value: <RelativeTime value={u.requested_at} /> },
              { label: "Divalidasi", value: u.validated_at ? <><RelativeTime value={u.validated_at} /> oleh {u.validated_by_name}</> : "—" },
              { label: "Terakhir aktif", value: u.last_seen_at ? <RelativeTime value={u.last_seen_at} /> : "belum pernah login" },
              { label: "Ticket terbuka", value: String(u.open_requests) },
            ]} />
            <div>
              <div className="mb-2 flex items-center justify-between">
                <div className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Akses unit / area (server-side isolation)</div>
                {u.allowed_actions.includes("manage_access") && <Button size="sm" variant="secondary" onClick={() => setAddOpen((o) => !o)}><Icon name="add" size={14} /> Tambah akses</Button>}
              </div>
              <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
                {u.access.map((a) => (
                  <li key={a.access_id} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
                    <span><span className="font-medium">{a.unit_number ? `Unit ${a.unit_number}` : a.name}</span> <span className="text-xs text-muted-foreground">{a.path_text}</span> {a.is_primary && <Badge tone="primary" className="ml-1">utama</Badge>}</span>
                    {u.allowed_actions.includes("manage_access") && <Button size="sm" variant="ghost" onClick={() => revoke(a.access_id)}>Cabut</Button>}
                  </li>
                ))}
                {u.access.length === 0 && <li className="px-3 py-2 text-sm text-muted-foreground">Belum ada akses unit — tenant tidak dapat membuat ticket unit.</li>}
              </ul>
              {addOpen && (
                <div className="mt-2 space-y-2 rounded-[var(--radius-md)] border border-border p-3">
                  <div className="max-h-56 overflow-y-auto"><TreeView propertyId={u.property_id} selected={pickLoc} onSelect={(n) => setPickLoc(n.id)} allowTypes={["unit", "area", "space"]} /></div>
                  <div className="flex justify-end gap-2">
                    <Button size="sm" variant="secondary" onClick={() => setAddOpen(false)}>Batal</Button>
                    <Button size="sm" disabled={!pickLoc} loading={grant.isPending} onClick={() => grant.mutateAsync({ id, location_id: pickLoc!, is_primary: u.access.length === 0 }).then(() => { setAddOpen(false); setPickLoc(null); toast.success("Akses ditambahkan"); }).catch(toast.error)}>Beri akses</Button>
                  </div>
                </div>
              )}
            </div>
            {reasonFor && (
              <Dialog open onOpenChange={(o) => !o && setReasonFor(null)}>
                <DialogContent title={reasonFor === "reject" ? "Tolak pendaftaran" : "Tangguhkan akun"} description={reasonFor === "suspend" ? "Sesi aktif tenant akan diputus. Alasan tercatat di audit log dan dikirim ke tenant." : "Alasan dikirim ke pendaftar dan tercatat di audit log."}>
                  <Field label="Alasan" required><Textarea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} /></Field>
                  <DialogFooter><Button variant="secondary" onClick={() => setReasonFor(null)}>Batal</Button><Button variant={reasonFor === "reject" ? "destructive" : "primary"} disabled={!reason.trim()} loading={decide.isPending} onClick={() => run(reasonFor, reason.trim())}>Konfirmasi</Button></DialogFooter>
                </DialogContent>
              </Dialog>
            )}
          </div>
        )}
      </AsyncState>
    </Drawer>
  );
}

function CreateTenantUserDialog({ propertyId, onClose }: { propertyId: string; onClose: () => void }) {
  const toast = useToast();
  const tenants = useAll<Tenant>("tenants", { property_id: propertyId });
  const [form, setForm] = useState({ full_name: "", email: "", phone: "", role: "tenant_user", ownership_status: "tenant", tenant_id: "" });
  const [unitIds, setUnitIds] = useState<string[]>([]);
  const [result, setResult] = useState<{ tenant_user: TenantUser; temporary_password?: string } | null>(null);
  const create = useAction<Record<string, unknown>, { tenant_user: TenantUser; temporary_password?: string }>(() => "tenant-users", { invalidate: ["list", "one"] });
  const tenant = tenants.data?.find((x) => x.id === form.tenant_id);
  const submit = () => create.mutateAsync({ property_id: propertyId, full_name: form.full_name.trim(), email: form.email.trim(), phone: form.phone.trim(), role: form.role, ownership_status: form.ownership_status, tenant_id: form.tenant_id || null, unit_ids: unitIds }).then(setResult).catch(toast.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Buat Akun Tenant" description="Akun langsung aktif. Password sementara ditampilkan sekali — sampaikan ke tenant melalui kanal aman.">
        {result ? (
          <div className="space-y-3">
            <Alert variant="success" title="Akun dibuat">{result.tenant_user.full_name} · {result.tenant_user.email}</Alert>
            {result.temporary_password && <Field label="Password sementara (tampil sekali)"><code className="block rounded bg-surface-container px-3 py-2 font-mono text-lg">{result.temporary_password}</code></Field>}
            <DialogFooter><Button onClick={onClose}>Selesai</Button></DialogFooter>
          </div>
        ) : (
          <div className="space-y-4">
            <Field label="Tenant (opsional)"><NativeSelect value={form.tenant_id} onChange={(e) => { setForm({ ...form, tenant_id: e.target.value }); setUnitIds([]); }}><option value="">— Tanpa tenant —</option>{(tenants.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.name} ({x.tenant_code})</option>)}</NativeSelect></Field>
            {tenant && tenant.units.length > 0 && (
              <Field label="Unit yang dapat diakses">
                <div className="flex flex-wrap gap-2">{tenant.units.map((u) => <Checkbox key={u.location_id} label={`Unit ${u.unit_number}`} checked={unitIds.includes(u.location_id)} onCheckedChange={(v) => setUnitIds((s) => (v ? [...s, u.location_id] : s.filter((x) => x !== u.location_id)))} />)}</div>
              </Field>
            )}
            <div className="grid grid-cols-2 gap-3">
              <Field label="Nama lengkap" required><Input value={form.full_name} onChange={(e) => setForm({ ...form, full_name: e.target.value })} /></Field>
              <Field label="Email" required><Input type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} /></Field>
              <Field label="Telepon"><Input value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} /></Field>
              <Field label="Role"><NativeSelect value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })}><option value="tenant_user">Tenant User</option><option value="tenant_admin">Tenant Admin / PIC</option></NativeSelect></Field>
              <Field label="Hubungan"><NativeSelect value={form.ownership_status} onChange={(e) => setForm({ ...form, ownership_status: e.target.value })}><option value="owner">Pemilik</option><option value="tenant">Penyewa</option><option value="family">Keluarga</option><option value="employee">Karyawan</option></NativeSelect></Field>
            </div>
            <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={create.isPending} disabled={!form.full_name.trim() || !form.email.trim()} onClick={submit}>Buat akun</Button></DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
