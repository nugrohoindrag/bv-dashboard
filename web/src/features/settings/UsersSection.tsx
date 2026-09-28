// Users (PRD §22.2; PRD P0 v2 §8, §24.1, §26.1): daftar + export, buat user (password awal atau undangan email),
// edit profil, role per property dengan scope Building/Tower opsional, user vendor, aktifkan/nonaktifkan (dengan alasan),
// reset password, kirim ulang undangan, tab Sesi (lihat & cabut). `?user=<id>` (dari Global Search) membuka drawer.
import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Alert, Badge, Button, Checkbox, ConfirmDialog, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, SearchInput, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { CellText, CellTitle } from "@/components/bv/cells";
import { ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { CardSkeleton, QueryErrorState } from "@/components/bv/states";
import { LocationPicker, VendorPicker } from "@/components/bv/pickers";
import { SessionList } from "@/components/bv/sessions";
import { ExportButton } from "@/components/bv/export";
import { Fab } from "@/components/bv/mobile";
import { useAction, useAll, useCreate, useInvalidate, useList, useUpdate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { Role, RoleAssignment, Session, Team, User } from "@/api/types";

interface InviteResult { user_id: string; email: string; expires_at: string; invite_url?: string }

export default function UsersSection() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const [q, setQ] = useState("");
  const [role, setRole] = useState("");
  const [active, setActive] = useState("");
  const list = useList<User>("users", { q: q || undefined, role: role || undefined, is_active: active || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const roles = useAll<Role>("roles");
  const [creating, setCreating] = useState(false);
  const [reset, setReset] = useState<User | null>(null);
  const [status, setStatus] = useState<{ user: User; action: "activate" | "deactivate" } | null>(null);
  const [invite, setInvite] = useState<InviteResult | null>(null);
  const openId = sp.get("user");
  const open = (id: string | null) => {
    const next = new URLSearchParams(sp);
    if (id) next.set("user", id);
    else next.delete("user");
    setSp(next, { replace: true });
  };
  const resend = useResendInvite(setInvite);
  const columns = useMemo<ColumnDef<User, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026): kode user di atas nama; role = role pertama + "+N" (daftar lengkap & vendor di
      // tooltip/drawer); team satu baris terpotong.
      { id: "name", header: "User", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.user_code} title={row.original.full_name} /> },
      { id: "email", header: "Email", meta: { mobile: "hidden" }, cell: ({ row }) => <CellText max={200} muted>{row.original.email ?? row.original.username}</CellText>, size: 200 },
      {
        id: "roles", header: "Role", meta: { mobile: "secondary" }, size: 180,
        cell: ({ row }) => {
          const labels = row.original.roles.map((r) => `${r.role_name ?? r.role_code}${r.scope_location_name ? ` · ${r.scope_location_name}` : r.property_id ? "" : ` · ${t("users.all_properties")}`}`);
          const tip = [...labels, row.original.vendor_name ? `Vendor: ${row.original.vendor_name}` : null].filter(Boolean).join("\n");
          if (!labels.length) return <span className="text-sm text-muted-foreground" title={tip || undefined}>—</span>;
          return (
            <span className="flex min-w-0 items-center gap-1 whitespace-nowrap" title={tip}>
              <span className="max-w-[150px] truncate rounded-full bg-neutral-soft px-2 py-0.5 text-xs text-neutral-text">{labels[0]}</span>
              {labels.length > 1 && <span className="text-xs text-on-surface-variant">+{labels.length - 1}</span>}
            </span>
          );
        },
      },
      { id: "teams", header: "Team", meta: { mobile: "hidden" }, cell: ({ row }) => <CellText max={160}>{row.original.teams.map((x) => x.team_name + (x.is_lead ? " (lead)" : "")).join(", ") || "—"}</CellText>, size: 160 },
      { id: "last", header: t("users.last_login"), meta: { mobile: "secondary" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm"><RelativeTime value={row.original.last_login_at} /></span>, size: 120 },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <Badge tone={row.original.is_active ? "success" : "neutral"}>{row.original.is_active ? t("users.active") : t("users.inactive")}</Badge>, size: 90 },
    ],
    [t],
  );
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="w-full sm:w-64"><SearchInput placeholder={t("users.search")} value={q} onChange={(e) => setQ(e.target.value)} /></div>
        <NativeSelect className="w-[calc(50%-4px)] sm:w-48" value={role} onChange={(e) => setRole(e.target.value)} aria-label="Role"><option value="">Role: {t("label.all")}</option>{(roles.data ?? []).map((r) => <option key={r.id} value={r.code}>{r.name}</option>)}</NativeSelect>
        <NativeSelect className="w-[calc(50%-4px)] sm:w-36" value={active} onChange={(e) => setActive(e.target.value)} aria-label={t("label.status")}><option value="">{t("label.status")}: {t("label.all")}</option><option value="true">{t("users.active")}</option><option value="false">{t("users.inactive")}</option></NativeSelect>
        <span className="ml-auto flex items-center gap-2">
          <ExportButton resource="users" filters={{ q, role, is_active: active }} />
          {can("iam.users.create") && <span className="hidden md:inline-flex"><Button icon="add" onClick={() => setCreating(true)}>{t("users.add")}</Button></span>}
        </span>
      </div>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => open(r.id)}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={!!q || !!role || !!active}
        empty={{ icon: "manage_accounts", title: t("users.empty"), description: t("users.empty_desc"), action: can("iam.users.create") ? <Button icon="add" onClick={() => setCreating(true)}>{t("users.add")}</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowActions={(r) => [
          ...(can("iam.users.update") ? [{ label: t("action.view"), icon: "edit", onSelect: () => open(r.id) }] : []),
          ...(can("iam.users.reset_password") ? [{ label: t("users.reset_password"), icon: "key", onSelect: () => setReset(r) }] : []),
          ...(r.is_active && can("iam.users.deactivate") ? [{ label: t("users.deactivate"), icon: "block", destructive: true, onSelect: () => setStatus({ user: r, action: "deactivate" }) }] : []),
          ...(!r.is_active && can("iam.users.activate") ? [{ label: t("users.activate"), icon: "check_circle", onSelect: () => setStatus({ user: r, action: "activate" }) }] : []),
          ...(r.email && !r.last_login_at && can("iam.users.create") ? [{ label: t("users.resend_invite"), icon: "send", onSelect: () => resend(r) }] : []),
        ]}
      />
      {can("iam.users.create") && <Fab label={t("action.create")} aria-label={t("users.add")} onClick={() => setCreating(true)} />}
      {creating && <UserDialog item={null} roles={roles.data ?? []} onClose={() => setCreating(false)} onInvited={setInvite} />}
      {openId && <UserDrawer id={openId} roles={roles.data ?? []} onClose={() => open(null)} onStatus={(user, action) => setStatus({ user, action })} onReset={setReset} onInvited={setInvite} />}
      {reset && <ResetPasswordDialog user={reset} onClose={() => setReset(null)} />}
      {status && <StatusDialog user={status.user} action={status.action} onClose={() => setStatus(null)} />}
      {invite && <InviteResultDialog invite={invite} onClose={() => setInvite(null)} />}
    </div>
  );
}

function useResendInvite(onInvited: (i: InviteResult) => void) {
  const { t } = useTranslation();
  const toast = useToast();
  return (u: User) =>
    api<InviteResult>(`users/${u.id}/invite`, { body: {} })
      .then((inv) => {
        toast.action("sent", t("users.invite_to", { email: inv.email }));
        if (inv.invite_url) onInvited(inv);
      })
      .catch((e) => toast.failed("sent", e, t("users.invite")));
}

/** Drawer detail: GET /users/{id} (dibuka dari baris atau ?user=). */
function UserDrawer({ id, roles, onClose, onStatus, onReset, onInvited }: { id: string; roles: Role[]; onClose: () => void; onStatus: (u: User, a: "activate" | "deactivate") => void; onReset: (u: User) => void; onInvited: (i: InviteResult) => void }) {
  const q = useQuery({ queryKey: ["one", "users", id], queryFn: () => api<User>(`users/${id}`) });
  if (q.isLoading) return <Dialog open onOpenChange={(o) => !o && onClose()}><DialogContent side="right" title="User"><CardSkeleton lines={6} /></DialogContent></Dialog>;
  if (q.isError || !q.data) return <Dialog open onOpenChange={(o) => !o && onClose()}><DialogContent side="right" title="User"><QueryErrorState error={q.error} onRetry={() => q.refetch()} compact /></DialogContent></Dialog>;
  return <UserDialog key={q.data.version} item={q.data} roles={roles} onClose={onClose} onStatus={onStatus} onReset={onReset} onInvited={onInvited} />;
}

type RoleRow = { role_id: string; property_id: string | null; scope_location_id: string | null; scope_location_name?: string | null };

function UserDialog({ item, roles, onClose, onStatus, onReset, onInvited }: { item: User | null; roles: Role[]; onClose: () => void; onStatus?: (u: User, a: "activate" | "deactivate") => void; onReset?: (u: User) => void; onInvited: (i: InviteResult) => void }) {
  const { t } = useTranslation();
  const { properties, can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const teams = useAll<Team>("teams");
  const [form, setForm] = useState({
    full_name: item?.full_name ?? "", email: item?.email ?? "", username: item?.username ?? "", phone: item?.phone ?? "", password: "", invite: !item,
    vendor_id: item?.vendor_id ?? null as string | null,
    roles: (item?.roles ?? []).map((r): RoleRow => ({ role_id: r.role_id, property_id: r.property_id, scope_location_id: r.scope_location_id ?? null, scope_location_name: r.scope_location_name })),
    team_ids: item?.teams.map((x) => x.team_id) ?? ([] as string[]),
  });
  const [err, setErr] = useState<string | null>(null);
  const create = useCreate<Record<string, unknown>, User | { user: User; invite: InviteResult }>("users");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("users");
  const [newRole, setNewRole] = useState<RoleRow>({ role_id: "", property_id: null, scope_location_id: null });
  const resend = useResendInvite(onInvited);
  const roleById = (id: string) => roles.find((r) => r.id === id);
  const vendorRole = roles.find((r) => r.code === "vendor");
  const hasVendorRole = form.roles.some((r) => r.role_id === vendorRole?.id);
  const visibleRoles = roles.filter((r) => r.code !== "platform_admin");
  const submit = async () => {
    setErr(null);
    if (!form.full_name.trim() || (!form.email && !form.username)) return setErr(t("users.err_identity"));
    if (!item && form.invite && !form.email) return setErr(t("users.err_invite_email"));
    if (!item && !form.invite && form.password.length < 10) return setErr(t("users.err_password"));
    if (hasVendorRole && !form.vendor_id) return setErr(t("users.err_vendor_required"));
    if (form.vendor_id && form.roles.some((r) => r.role_id !== vendorRole?.id)) return setErr(t("users.err_vendor_only"));
    const roleBody: RoleAssignment[] = form.roles.map((r) => ({ role_id: r.role_id, property_id: r.property_id, scope_location_id: r.property_id ? r.scope_location_id : null }));
    try {
      if (item) {
        await update.mutateAsync({ id: item.id, version: item.version, full_name: form.full_name, email: form.email || null, username: form.username || null, phone: form.phone || null, roles: roleBody, team_ids: form.team_ids, vendor_id: form.vendor_id, clear_vendor: !form.vendor_id && !!item.vendor_id });
        toast.action("saved", `User ${form.full_name}`);
      } else {
        const res = await create.mutateAsync({ full_name: form.full_name, email: form.email || null, username: form.username || null, phone: form.phone || null, ...(form.invite ? { invite: true } : { password: form.password }), roles: roleBody, team_ids: form.team_ids, vendor_id: form.vendor_id });
        if ("invite" in res && res.invite) {
          toast.action("sent", t("users.invite_to", { email: res.invite.email }));
          if (res.invite.invite_url) onInvited(res.invite);
        } else toast.action("created", `User ${form.full_name}`);
      }
      invalidate("users");
      onClose();
    } catch (e) {
      // ROLE_NOT_GRANTABLE (403) / vendor rules (400): tampilkan detail server apa adanya
      setErr((e as Error).message);
    }
  };
  const propName = (id: string | null) => (id ? properties.find((p) => p.id === id)?.name ?? id : t("users.all_properties"));
  const profile = (
    <div className="space-y-4">
      {err && <Alert variant="critical" title={t("users.save_failed")}>{err}</Alert>}
      <Field label={t("profile.name")} required><Input value={form.full_name} onChange={(e) => setForm({ ...form, full_name: e.target.value })} /></Field>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label="Email" required={!item && form.invite}><Input type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} /></Field>
        <Field label="Username" help={t("users.username_help")}><Input value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} /></Field>
        <Field label={t("profile.phone")}><Input value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} /></Field>
        {!item && !form.invite && <Field label={t("users.initial_password")} required><Input type="password" autoComplete="new-password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} /></Field>}
      </div>
      {!item && (
        <label className="flex items-start gap-2 text-sm">
          <Checkbox checked={form.invite} onCheckedChange={(v) => setForm({ ...form, invite: !!v })} />
          <span><span className="font-medium">{t("users.send_invite")}</span><span className="block text-xs text-on-surface-variant">{t("users.send_invite_help")}</span></span>
        </label>
      )}
      <Field label="Vendor" help={t("users.vendor_help")}>
        <div className="flex gap-2">
          <VendorPicker className="flex-1" value={form.vendor_id} onChange={(v) => setForm({ ...form, vendor_id: v })} placeholder={t("users.no_vendor")} />
          {form.vendor_id && <Button variant="ghost" size="sm" onClick={() => setForm({ ...form, vendor_id: null })}>{t("users.unlink_vendor")}</Button>}
        </div>
      </Field>
      <Field label={`Role (${form.roles.length})`}>
        <ul className="mb-2 divide-y divide-border rounded-md border border-border">
          {form.roles.map((r, i) => (
            <li key={i} className="flex items-center justify-between gap-2 px-3 py-1.5 text-sm">
              <span className="min-w-0">{roleById(r.role_id)?.name ?? r.role_id} <span className="text-xs text-muted-foreground">· {propName(r.property_id)}{r.scope_location_id ? ` · ${t("users.scope")}: ${r.scope_location_name ?? r.scope_location_id.slice(0, 8)}` : ""}</span></span>
              <Button size="icon-sm" variant="ghost" aria-label={t("users.remove_role")} onClick={() => setForm({ ...form, roles: form.roles.filter((_, j) => j !== i) })}>×</Button>
            </li>
          ))}
          {form.roles.length === 0 && <li className="px-3 py-2 text-xs text-muted-foreground">{t("users.no_roles")}</li>}
        </ul>
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          <NativeSelect value={newRole.role_id} onChange={(e) => setNewRole({ ...newRole, role_id: e.target.value })} aria-label="Role"><option value="">{t("users.pick_role")}</option>{visibleRoles.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}</NativeSelect>
          <NativeSelect value={newRole.property_id ?? ""} onChange={(e) => setNewRole({ ...newRole, property_id: e.target.value || null, scope_location_id: null })} aria-label="Property"><option value="">{t("users.all_properties")}</option>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect>
          <LocationPicker className="sm:col-span-2" propertyId={newRole.property_id} disabled={!newRole.property_id} allowTypes={["building", "tower"]} value={newRole.scope_location_id} onChange={(id, node) => setNewRole({ ...newRole, scope_location_id: id, scope_location_name: node?.name ?? null })} placeholder={newRole.property_id ? t("users.scope_placeholder") : t("users.scope_needs_property")} />
          <Button variant="secondary" className="sm:col-span-2" disabled={!newRole.role_id} onClick={() => { setForm({ ...form, roles: [...form.roles, newRole] }); setNewRole({ role_id: "", property_id: null, scope_location_id: null }); }}>{t("users.add_role")}</Button>
        </div>
      </Field>
      <Field label="Team">
        <div className="max-h-40 space-y-1 overflow-y-auto rounded-md border border-border p-2">
          {(teams.data ?? []).map((tm) => <label key={tm.id} className="flex items-center gap-2 text-sm"><Checkbox checked={form.team_ids.includes(tm.id)} onCheckedChange={() => setForm((f) => ({ ...f, team_ids: f.team_ids.includes(tm.id) ? f.team_ids.filter((x) => x !== tm.id) : [...f.team_ids, tm.id] }))} /> {tm.name} <span className="text-xs text-muted-foreground">{tm.domain}</span></label>)}
          {(teams.data ?? []).length === 0 && <p className="text-xs text-on-surface-variant">—</p>}
        </div>
      </Field>
    </div>
  );
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? item.full_name : t("users.add")} description={item ? `${item.user_code}${item.email ? " · " + item.email : ""}` : undefined}>
        {item && (
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <Badge tone={item.is_active ? "success" : "neutral"}>{item.is_active ? t("users.active") : t("users.inactive")}</Badge>
            <span className="ml-auto flex flex-wrap gap-2">
              {item.is_active && can("iam.users.deactivate") && <Button size="sm" variant="secondary" onClick={() => onStatus?.(item, "deactivate")}>{t("users.deactivate")}</Button>}
              {!item.is_active && can("iam.users.activate") && <Button size="sm" variant="success" onClick={() => onStatus?.(item, "activate")}>{t("users.activate")}</Button>}
              {can("iam.users.reset_password") && <Button size="sm" variant="ghost" onClick={() => onReset?.(item)}>{t("users.reset_password")}</Button>}
              {item.email && can("iam.users.create") && <Button size="sm" variant="ghost" icon="send" onClick={() => resend(item)}>{t("users.resend_invite")}</Button>}
            </span>
          </div>
        )}
        {item && can("iam.sessions.view") ? (
          <Tabs defaultValue="profile">
            <TabsList>
              <TabsTrigger value="profile">{t("users.tab_profile")}</TabsTrigger>
              <TabsTrigger value="sessions">{t("users.tab_sessions")}</TabsTrigger>
            </TabsList>
            <TabsContent value="profile">{profile}</TabsContent>
            <TabsContent value="sessions"><UserSessions user={item} /></TabsContent>
          </Tabs>
        ) : profile}
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>{(!item || can("iam.users.update")) && <Button loading={create.isPending || update.isPending} onClick={submit}>{item ? t("action.save") : form.invite ? t("users.send_invite_btn") : t("action.save")}</Button>}</DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function UserSessions({ user }: { user: User }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const q = useQuery({ queryKey: ["user-sessions", user.id], queryFn: () => api<{ data: Session[] }>(`users/${user.id}/sessions`).then((r) => r.data) });
  const [revoking, setRevoking] = useState<string | null>(null);
  const [confirmAll, setConfirmAll] = useState(false);
  const revoke = async (sessionId?: string) => {
    setRevoking(sessionId ?? "all");
    try {
      const r = await api<{ revoked: number }>(`users/${user.id}/sessions/revoke`, { body: sessionId ? { session_id: sessionId } : {} });
      toast.success(t("profile.revoked_n", { n: r.revoked }));
      await q.refetch();
    } catch (e) {
      toast.error(e);
    } finally {
      setRevoking(null);
    }
  };
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-on-surface-variant">{t("users.sessions_hint")}</p>
        {can("iam.sessions.revoke") && <Button size="sm" variant="destructive" loading={revoking === "all"} disabled={!q.data?.length} onClick={() => setConfirmAll(true)}>{t("users.revoke_all")}</Button>}
      </div>
      {q.isLoading ? <CardSkeleton lines={3} /> : q.isError ? <QueryErrorState error={q.error} onRetry={() => q.refetch()} compact /> : (
        <SessionList sessions={(q.data ?? []).map((s) => ({ ...s, current: false }))} onRevoke={can("iam.sessions.revoke") ? (s) => revoke(s.id) : undefined} revoking={revoking} />
      )}
      <ConfirmDialog open={confirmAll} onOpenChange={setConfirmAll} destructive title={t("users.revoke_all")} description={t("users.revoke_all_desc", { name: user.full_name })} confirmLabel={t("users.revoke_all")} onConfirm={() => { setConfirmAll(false); revoke(); }} />
    </div>
  );
}

function StatusDialog({ user, action, onClose }: { user: User; action: "activate" | "deactivate"; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const invalidate = useInvalidate();
  const [busy, setBusy] = useState(false);
  const run = async (reason: string) => {
    setBusy(true);
    try {
      await api(`users/${user.id}/${action}`, { body: { reason } });
      invalidate("users");
      toast.action(action === "activate" ? "approved" : "cancelled", `User ${user.full_name}`);
      onClose();
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <ReasonDialog open onOpenChange={(o) => !o && onClose()} loading={busy} destructive={action === "deactivate"}
      title={action === "deactivate" ? t("users.deactivate_title", { name: user.full_name }) : t("users.activate_title", { name: user.full_name })}
      description={action === "deactivate" ? t("users.deactivate_desc") : t("users.activate_desc")}
      confirmLabel={action === "deactivate" ? t("users.deactivate") : t("users.activate")} onConfirm={run} />
  );
}

function InviteResultDialog({ invite, onClose }: { invite: InviteResult; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={t("users.invite_sent_title")} description={t("users.invite_sent_desc", { email: invite.email })}>
        {invite.invite_url && (
          <div className="space-y-2">
            <Alert variant="warning">{t("users.invite_url_dev")}</Alert>
            <div className="flex gap-2">
              <Input readOnly value={invite.invite_url} aria-label="Invite URL" onFocus={(e) => e.currentTarget.select()} />
              <Button variant="secondary" icon="link" onClick={() => navigator.clipboard?.writeText(invite.invite_url!).then(() => toast.info(t("users.copied"))).catch(() => undefined)}>{t("users.copy")}</Button>
            </div>
          </div>
        )}
        <DialogFooter><Button onClick={onClose}>{t("action.close")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ResetPasswordDialog({ user, onClose }: { user: User; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [pw, setPw] = useState("");
  const reset = useAction<{ new_password: string }>(() => `users/${user.id}/reset-password`);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`${t("users.reset_password")} · ${user.full_name}`} description={t("users.reset_desc")}>
        <Field label={t("profile.new_password")} required><Input type="password" autoComplete="new-password" value={pw} onChange={(e) => setPw(e.target.value)} /></Field>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={reset.isPending} disabled={pw.length < 10} onClick={() => reset.mutateAsync({ new_password: pw }).then(() => { toast.action("updated", "Password"); onClose(); }).catch(toast.error)}>{t("users.reset_password")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
