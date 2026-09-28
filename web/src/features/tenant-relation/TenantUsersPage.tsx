// Tenant Relation › Tenant Users (PRD P1 v1.3 §7 Tenant Identity & Access; kontrak PWA §2.1 "Validasi Account"):
// validasi pendaftaran dari Tenant App, buat akun untuk occupant, kelola akses unit/area, suspend/reactivate.
// PRD P3 v2.1 (email di-hold D-P3-08 → WhatsApp manual): reset password (P3-ACC-05, password sementara tampil sekali + tombol
// WhatsApp), kabar keputusan akun lewat WhatsApp (P3-ACC-07), akun baru + password sementara (P3-ACC-03), role Tenant Admin (P3-ACC-08),
// riwayat WhatsApp manual per akun (P3-WAM-03). Filter status di URL (`?status=pending_validation` dari drill-down dashboard).
import { useMemo, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid, useUrlFilters } from "@/components/bv/datagrid";
import { AsyncState, KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { WhatsAppButton, type WhatsAppContext } from "@/components/bv/WhatsAppButton";
import { useAction, useAll, useList, useOne } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useProfile } from "@/lib/profile";
import { fmtDateTime } from "@/lib/format";
import type { Tenant } from "@/api/types";
import { TreeView } from "@/features/property/LocationsPage";
import { StatusBadge } from "@/components/bv/badges";
import { CellText } from "@/components/bv/cells";
import { statusLabel } from "@/lib/status";
import { OWNERSHIP_STATUSES, REGISTRATION_SOURCES, WA_CONTEXTS, labelOf } from "./labels";
import { useWhatsAppLogs } from "./hooks";

export interface TenantAccess { id: string; access_id: string; location_type: string; name: string; code: string; unit_number?: string | null; path_text: string; is_primary: boolean; access_type: string }
export interface TenantUser {
  id: string; user_id: string; full_name: string; email: string | null; phone: string | null; property_id: string; property_name: string;
  tenant_id: string | null; tenant_name: string | null; occupant_id: string | null; role: string; status: string; registration_source: string;
  ownership_status: string | null; requested_at: string; validated_at: string | null; validated_by_name: string | null; rejection_reason: string | null;
  suspension_reason: string | null; last_seen_at: string | null; access: TenantAccess[]; open_requests: number; allowed_actions: string[]; version: number;
}
interface PasswordResult { tenant_user: TenantUser; temporary_password?: string }

/** Konteks pesan WhatsApp sesuai status akun (kabar keputusan akun, P3-ACC-07). */
const STATUS_WA: Record<string, WhatsAppContext | undefined> = { active: "account_approved", rejected: "account_rejected", suspended: "account_suspended" };

function RoleBadge({ role }: { role: string }) {
  return role === "tenant_admin" ? <Badge tone="primary"><Icon name="admin_panel_settings" size={12} aria-hidden />Tenant Admin</Badge> : <span className="text-xs">Tenant User</span>;
}

export default function TenantUsersPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const { search } = useLocation();
  const { propertyId, can } = useAuth();
  const prof = useProfile();
  const f = useUrlFilters();
  const statusParam = f.get("status");
  const status = statusParam === "all" ? "" : statusParam || "pending_validation";
  const [q, setQ] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const list = useList<TenantUser>("tenant-users", { property_id: propertyId ?? undefined, status: status || undefined, q: q || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<TenantUser, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): nama satu baris (email/telepon di tooltip), unit, tenant, role, waktu
    // daftar, status. Status kepemilikan, sumber pendaftaran & kontak lengkap ada di drawer detail.
    { id: "name", header: prof.term("customer", "id") + " user", meta: { mobile: "primary" }, cell: ({ row: { original: u } }) => <CellText max={220} title={[u.full_name, u.email, u.phone].filter(Boolean).join(" · ")} className="font-medium text-on-surface">{u.full_name}</CellText> },
    { id: "unit", header: prof.term("inventory_unit", "id"), meta: { mobile: "secondary" }, cell: ({ row }) => { const units = row.original.access.filter((a) => a.access_type === "unit").map((a) => a.unit_number ?? a.name).join(", "); return <CellText max={140} muted={!units}>{units || "—"}</CellText>; } },
    { id: "tenant", header: t("label.tenant"), meta: { mobile: "secondary" }, cell: ({ row }) => <CellText max={170} muted={!row.original.tenant_name}>{row.original.tenant_name ?? "—"}</CellText> },
    { id: "role", header: "Role", meta: { mobile: "hidden" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm">{row.original.role === "tenant_admin" ? "Tenant Admin" : "Tenant User"}</span>, size: 130 },
    { id: "requested_at", header: "Didaftarkan", meta: { mobile: "hidden" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm text-muted-foreground"><RelativeTime value={row.original.requested_at} /></span>, size: 130 },
    { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <StatusBadge objectType="tenant_user" status={row.original.status} />, size: 150 },
  ], [t, prof]);
  return (
    <div>
      <PageHeader title="Tenant Users" subtitle="Akun Tenant App: validasi pendaftaran, akses unit, status akun, reset password, dan kabar lewat WhatsApp." actions={can("tenant_relation.tenant_users.update") && <Button onClick={() => setCreateOpen(true)} disabled={!propertyId}><Icon name="person_add" size={16} /> Buat Akun Tenant</Button>}>
        <div className="flex flex-wrap items-center gap-2">
          <Input className="w-full sm:w-72" placeholder="Cari nama / email / telepon…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-full sm:w-52" value={status} onChange={(e) => f.set({ status: e.target.value || "all" })} aria-label="Status akun">
            <option value="">Status: {t("label.all")}</option>
            <option value="pending_validation">Menunggu validasi</option>
            <option value="active">Aktif</option>
            <option value="suspended">Ditangguhkan</option>
            <option value="rejected">Ditolak</option>
          </NativeSelect>
        </div>
      </PageHeader>
      {!propertyId && <Alert variant="info" className="mb-4">Pilih property di header untuk membuat akun tenant.</Alert>}
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/tenant-relation/tenant-users/${r.id}${search}`} loading={list.isLoading} error={list.error} onRetry={() => list.refetch()} isFiltered={!!q || !!status} empty={{ message: status === "pending_validation" ? "Tidak ada pendaftaran yang menunggu validasi." : "Belum ada akun tenant." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} rowClassName={(r) => (r.id === id ? "bg-primary-soft" : undefined)} />
      {id && <TenantUserDrawer id={id} onClose={() => nav(`/tenant-relation/tenant-users${search}`)} />}
      {createOpen && propertyId && <CreateTenantUserDialog propertyId={propertyId} onClose={() => setCreateOpen(false)} />}
    </div>
  );
}

function TenantUserDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const { can } = useAuth();
  const tu = useOne<TenantUser>("tenant-users", id);
  const waLogs = useWhatsAppLogs("tenant_user", id);
  const decide = useAction<{ id: string; action: string; reason?: string }, TenantUser>((i) => `tenant-users/${i.id}/${i.action}`, { body: (i) => ({ reason: i.reason }), invalidate: ["list", "one", "notifications", "tr-metrics"] });
  const grant = useAction<{ id: string; location_id: string; is_primary: boolean }, TenantUser>((i) => `tenant-users/${i.id}/access`, { body: (i) => ({ location_id: i.location_id, is_primary: i.is_primary }), invalidate: ["list", "one"] });
  const setRole = useAction<{ role: string }, TenantUser>(() => `tenant-users/${id}`, { method: "PATCH", invalidate: ["list", "one"] });
  const [reasonFor, setReasonFor] = useState<"reject" | "suspend" | null>(null);
  const [reason, setReason] = useState("");
  const [pickLoc, setPickLoc] = useState<string | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);
  const [notify, setNotify] = useState<WhatsAppContext | null>(null);
  const run = (action: string, r?: string) =>
    decide.mutateAsync({ id, action, reason: r }).then(() => {
      toast.success(`Akun ${statusLabel("tenant_user", action === "approve" || action === "reactivate" ? "active" : action === "reject" ? "rejected" : "suspended").toLowerCase()}`);
      setReasonFor(null);
      setReason("");
      // email di-hold (D-P3-08): tawarkan kabar keputusan lewat WhatsApp manual
      setNotify(action === "reject" ? "account_rejected" : action === "suspend" ? "account_suspended" : "account_approved");
    }).catch(toast.error);
  const revoke = (accessId: string) => api(`tenant-users/${id}/access/${accessId}`, { method: "DELETE" }).then(() => { toast.success("Akses dicabut"); tu.refetch(); }).catch(toast.error);
  return (
    <Drawer open onClose={onClose} title="Akun Tenant" width={640}>
      <AsyncState query={tu}>
        {(u) => {
          const statusWA = STATUS_WA[u.status];
          const canReset = u.status === "active" && u.allowed_actions.includes("reset_password") && can("tenant_relation.tenant_users.reset_password", u.property_id);
          const canUpdate = u.allowed_actions.includes("update");
          return (
            <div className="space-y-5">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2 text-lg font-semibold">{u.full_name}{u.role === "tenant_admin" && <RoleBadge role={u.role} />}</div>
                  <div className="text-sm text-muted-foreground">{u.email} {u.phone ? `· ${u.phone}` : ""}</div>
                  {!u.phone && <div className="mt-0.5 inline-flex items-center gap-1 text-xs text-warning-text"><Icon name="warning_amber" size={14} aria-hidden />Nomor telepon kosong — pesan WhatsApp tidak dapat dikirim.</div>}
                </div>
                <StatusBadge objectType="tenant_user" status={u.status} />
              </div>
              {u.status === "pending_validation" && <Alert variant="warning" title="Menunggu validasi">Periksa identitas pendaftar dan unit yang diklaim sebelum menyetujui. Setelah disetujui, tenant dapat login ke Tenant App dan melihat data unit tersebut.</Alert>}
              {u.rejection_reason && <Alert variant="critical" title="Ditolak">{u.rejection_reason}</Alert>}
              {u.suspension_reason && <Alert variant="warning" title="Ditangguhkan">{u.suspension_reason}</Alert>}
              {notify && (
                <Alert variant="success" title="Kabari tenant lewat WhatsApp" action={<WhatsAppButton context={notify} objectType="tenant_user" objectId={u.id} label="Kirim via WhatsApp" variant="primary" onSent={() => { setNotify(null); waLogs.refetch(); }} />}>
                  Email ke tenant sedang di-hold — kirim kabar {notify === "account_approved" ? "persetujuan akun" : notify === "account_rejected" ? "penolakan beserta alasannya" : "penangguhan beserta alasannya"} secara manual.
                </Alert>
              )}
              <div className="flex flex-wrap gap-2">
                {u.allowed_actions.includes("approve") && <Button onClick={() => run("approve")} loading={decide.isPending} icon="check">Setujui</Button>}
                {u.allowed_actions.includes("reject") && <Button variant="secondary" onClick={() => setReasonFor("reject")}>Tolak…</Button>}
                {u.allowed_actions.includes("suspend") && <Button variant="secondary" onClick={() => setReasonFor("suspend")}>Tangguhkan…</Button>}
                {u.allowed_actions.includes("reactivate") && <Button onClick={() => run("reactivate")} loading={decide.isPending}>Aktifkan kembali</Button>}
                {canReset && <Button variant="secondary" icon="key" onClick={() => setResetOpen(true)}>Reset password…</Button>}
                {statusWA && !notify && <WhatsAppButton context={statusWA} objectType="tenant_user" objectId={u.id} size="md" label={`WhatsApp: ${labelOf(WA_CONTEXTS, statusWA).toLowerCase()}`} onSent={() => waLogs.refetch()} />}
              </div>
              <KeyValue items={[
                { label: "Property", value: u.property_name },
                { label: t("label.tenant"), value: u.tenant_name ?? "—" },
                { label: "Role", value: canUpdate ? <RoleSelect role={u.role} busy={setRole.isPending} onChange={(role) => setRole.mutateAsync({ role }).then(() => toast.success(role === "tenant_admin" ? `${u.full_name} menjadi Tenant Admin` : `${u.full_name} menjadi Tenant User`)).catch(toast.error)} /> : <RoleBadge role={u.role} /> },
                { label: "Hubungan", value: u.ownership_status ? labelOf(OWNERSHIP_STATUSES, u.ownership_status) : "—" },
                { label: "Sumber", value: labelOf(REGISTRATION_SOURCES, u.registration_source) },
                { label: "Didaftarkan", value: <RelativeTime value={u.requested_at} /> },
                { label: "Divalidasi", value: u.validated_at ? <><RelativeTime value={u.validated_at} /> oleh {u.validated_by_name}</> : "—" },
                { label: "Terakhir aktif", value: u.last_seen_at ? <RelativeTime value={u.last_seen_at} /> : "belum pernah login" },
                { label: "Ticket terbuka", value: String(u.open_requests) },
              ]} />
              {u.role === "tenant_admin" && <p className="-mt-2 text-xs text-on-surface-variant">Tenant Admin melihat seluruh permintaan tenant-nya dan dapat mengelola anggota (undang, nonaktifkan, akses unit) dari Tenant App.</p>}
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
              <div>
                <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Riwayat WhatsApp manual</div>
                {waLogs.isLoading ? (
                  <p className="text-sm text-muted-foreground">Memuat…</p>
                ) : (waLogs.data ?? []).length === 0 ? (
                  <p className="text-sm text-muted-foreground">Belum ada pesan WhatsApp yang dicatat untuk akun ini.</p>
                ) : (
                  <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
                    {(waLogs.data ?? []).map((l) => (
                      <li key={l.id} className="px-3 py-2 text-sm">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <span className="inline-flex items-center gap-1.5 font-medium"><Icon name="chat" size={14} className="text-success" aria-hidden />{labelOf(WA_CONTEXTS, l.context)}</span>
                          <span className="text-xs text-muted-foreground">{fmtDateTime(l.sent_at)}</span>
                        </div>
                        <div className="text-xs text-muted-foreground">ke {l.recipient_name ?? "—"} (+{l.phone}) · oleh {l.sent_by_name}</div>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
              {reasonFor && (
                <Dialog open onOpenChange={(o) => !o && setReasonFor(null)}>
                  <DialogContent title={reasonFor === "reject" ? "Tolak pendaftaran" : "Tangguhkan akun"} description={reasonFor === "suspend" ? "Sesi aktif tenant akan diputus. Alasan tercatat di audit log dan ditampilkan ke tenant di layar login." : "Alasan tercatat di audit log dan ditampilkan ke pendaftar di layar login."}>
                    <Field label="Alasan" required><Textarea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} /></Field>
                    <DialogFooter><Button variant="secondary" onClick={() => setReasonFor(null)}>Batal</Button><Button variant={reasonFor === "reject" ? "destructive" : "primary"} disabled={!reason.trim()} loading={decide.isPending} onClick={() => run(reasonFor, reason.trim())}>Konfirmasi</Button></DialogFooter>
                  </DialogContent>
                </Dialog>
              )}
              {resetOpen && <ResetPasswordDialog user={u} onClose={() => setResetOpen(false)} onSent={() => waLogs.refetch()} />}
            </div>
          );
        }}
      </AsyncState>
    </Drawer>
  );
}

function RoleSelect({ role, busy, onChange }: { role: string; busy: boolean; onChange: (role: string) => void }) {
  return (
    <span className="inline-flex items-center gap-2">
      <NativeSelect className="w-48" value={role} disabled={busy} onChange={(e) => e.target.value !== role && onChange(e.target.value)} aria-label="Role akun tenant">
        <option value="tenant_user">Tenant User</option>
        <option value="tenant_admin">Tenant Admin / PIC</option>
      </NativeSelect>
      {busy && <Icon name="progress_activity" size={16} className="animate-spin text-on-surface-variant" aria-hidden />}
    </span>
  );
}

/** Password sementara (tampil sekali) + salin + kirim lewat WhatsApp manual. */
function TemporaryPassword({ password }: { password: string }) {
  const toast = useToast();
  const copy = () => navigator.clipboard?.writeText(password).then(() => toast.success("Password sementara disalin")).catch(() => toast.warning("Tidak dapat menyalin — salin manual."));
  return (
    <Field label="Password sementara (tampil sekali)" help="Tenant wajib mengganti password saat login pertama.">
      <div className="flex items-center gap-2">
        <code className="block flex-1 select-all rounded bg-surface-container px-3 py-2 font-mono text-lg tracking-wide">{password}</code>
        <Button size="sm" variant="secondary" onClick={copy}>Salin</Button>
      </div>
    </Field>
  );
}

function ResetPasswordDialog({ user, onClose, onSent }: { user: TenantUser; onClose: () => void; onSent: () => void }) {
  const toast = useToast();
  const reset = useAction<void, PasswordResult>(() => `tenant-users/${user.id}/reset-password`, { body: () => ({}), invalidate: ["one"] });
  const [result, setResult] = useState<PasswordResult | null>(null);
  const run = () => reset.mutateAsync().then((r) => { setResult(r); toast.success(`Password ${user.full_name} direset`); }).catch((e) => toast.failed("updated", e, "Password"));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Reset password · ${user.full_name}`} description={result ? "Kirim password sementara ke tenant lewat WhatsApp. Password tidak dapat ditampilkan lagi setelah dialog ditutup." : "Password lama tidak berlaku, seluruh sesi Tenant App tenant diputus, dan tenant wajib mengganti password saat login berikutnya."}>
        {result?.temporary_password ? (
          <div className="space-y-4">
            <TemporaryPassword password={result.temporary_password} />
            <Alert variant="info" title="Kirim ke tenant" action={<WhatsAppButton context="password_reset" objectType="tenant_user" objectId={user.id} temporaryPassword={result.temporary_password} label="Kirim via WhatsApp" variant="primary" onSent={onSent} />}>
              Email sedang di-hold — sampaikan lewat WhatsApp ke nomor terdaftar tenant{user.phone ? ` (${user.phone})` : ""}.
            </Alert>
            <DialogFooter><Button onClick={onClose}>Selesai</Button></DialogFooter>
          </div>
        ) : (
          <DialogFooter>
            <Button variant="secondary" onClick={onClose}>Batal</Button>
            <Button variant="destructive" icon="key" loading={reset.isPending} onClick={run}>Reset password</Button>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  );
}

function CreateTenantUserDialog({ propertyId, onClose }: { propertyId: string; onClose: () => void }) {
  const toast = useToast();
  const tenants = useAll<Tenant>("tenants", { property_id: propertyId });
  const [form, setForm] = useState({ full_name: "", email: "", phone: "", role: "tenant_user", ownership_status: "tenant", tenant_id: "" });
  const [unitIds, setUnitIds] = useState<string[]>([]);
  const [result, setResult] = useState<PasswordResult | null>(null);
  const create = useAction<Record<string, unknown>, PasswordResult>(() => "tenant-users", { invalidate: ["list", "one"] });
  const tenant = tenants.data?.find((x) => x.id === form.tenant_id);
  const submit = () => create.mutateAsync({ property_id: propertyId, full_name: form.full_name.trim(), email: form.email.trim(), phone: form.phone.trim(), role: form.role, ownership_status: form.ownership_status, tenant_id: form.tenant_id || null, unit_ids: unitIds }).then(setResult).catch(toast.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Buat Akun Tenant" description="Akun langsung aktif. Password sementara ditampilkan sekali — kirim ke tenant lewat WhatsApp (email sedang di-hold).">
        {result ? (
          <div className="space-y-4">
            <Alert variant="success" title="Akun dibuat">{result.tenant_user.full_name} · {result.tenant_user.email}{result.tenant_user.role === "tenant_admin" ? " · Tenant Admin" : ""}</Alert>
            {result.temporary_password && (
              <>
                <TemporaryPassword password={result.temporary_password} />
                <div className="flex flex-wrap items-center gap-2">
                  <WhatsAppButton context="account_created" objectType="tenant_user" objectId={result.tenant_user.id} temporaryPassword={result.temporary_password} label="Kirim via WhatsApp" variant="primary" size="md" />
                  {!result.tenant_user.phone && <span className="text-xs text-warning-text">Nomor telepon kosong — isi di akun tenant agar pesan dapat dikirim.</span>}
                </div>
              </>
            )}
            <DialogFooter><Button variant="secondary" onClick={onClose}>Selesai</Button></DialogFooter>
          </div>
        ) : (
          <div className="space-y-4">
            <Field label="Tenant (opsional)"><NativeSelect value={form.tenant_id} onChange={(e) => { setForm({ ...form, tenant_id: e.target.value }); setUnitIds([]); }}><option value="">— Tanpa tenant —</option>{(tenants.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.name} ({x.tenant_code})</option>)}</NativeSelect></Field>
            {tenant && tenant.units.length > 0 && (
              <Field label="Unit yang dapat diakses">
                <div className="flex flex-wrap gap-2">{tenant.units.map((u) => <Checkbox key={u.location_id} label={`Unit ${u.unit_number}`} checked={unitIds.includes(u.location_id)} onCheckedChange={(v) => setUnitIds((s) => (v ? [...s, u.location_id] : s.filter((x) => x !== u.location_id)))} />)}</div>
              </Field>
            )}
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Nama lengkap" required><Input value={form.full_name} onChange={(e) => setForm({ ...form, full_name: e.target.value })} /></Field>
              <Field label="Email" required><Input type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} /></Field>
              <Field label="Telepon / WhatsApp" help="Dipakai untuk mengirim password sementara lewat WhatsApp."><Input type="tel" inputMode="tel" value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} placeholder="08…" /></Field>
              <Field label="Role"><NativeSelect value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })}><option value="tenant_user">Tenant User</option><option value="tenant_admin">Tenant Admin / PIC</option></NativeSelect></Field>
              <Field label="Hubungan"><NativeSelect value={form.ownership_status} onChange={(e) => setForm({ ...form, ownership_status: e.target.value })}><option value="owner">Pemilik</option><option value="tenant">Penyewa</option><option value="family">Keluarga</option><option value="employee">Karyawan</option></NativeSelect></Field>
            </div>
            {form.role === "tenant_admin" && <p className="text-xs text-on-surface-variant">Tenant Admin melihat seluruh permintaan tenant-nya dan dapat mengelola anggota tenant dari Tenant App.</p>}
            <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={create.isPending} disabled={!form.full_name.trim() || !form.email.trim()} onClick={submit}>Buat akun</Button></DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
