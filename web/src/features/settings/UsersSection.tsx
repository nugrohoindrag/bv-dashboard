// Users (PRD §22.2): daftar, buat/edit user, role per property, team, aktif/nonaktif, reset password.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useAll, useCreate, useList, useUpdate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import type { Role, Team, User } from "@/api/types";

export default function UsersSection() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const [q, setQ] = useState("");
  const [role, setRole] = useState("");
  const [active, setActive] = useState("");
  const list = useList<User>("users", { q: q || undefined, role: role || undefined, is_active: active || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const roles = useAll<Role>("roles");
  const [edit, setEdit] = useState<User | null | "new">(null);
  const [reset, setReset] = useState<User | null>(null);
  const columns = useMemo<ColumnDef<User, unknown>[]>(
    () => [
      { id: "name", header: "User", cell: ({ row }) => <div><div className="font-medium">{row.original.full_name}</div><div className="text-xs text-muted-foreground">{row.original.email ?? row.original.username} · <span className="font-mono">{row.original.user_code}</span></div></div> },
      { id: "roles", header: "Role", cell: ({ row }) => <div className="flex flex-wrap gap-1">{row.original.roles.map((r, i) => <span key={i} className="rounded-full bg-neutral-soft px-2 py-0.5 text-xs text-neutral-text">{r.role_name ?? r.role_code}{r.property_id ? "" : " · semua"}</span>)}</div> },
      { id: "teams", header: "Team", cell: ({ row }) => <span className="text-xs">{row.original.teams.map((x) => x.team_name + (x.is_lead ? " (lead)" : "")).join(", ") || "—"}</span> },
      { id: "last", header: "Login terakhir", cell: ({ row }) => <RelativeTime value={row.original.last_login_at} />, size: 120 },
      { id: "active", header: "Aktif", cell: ({ row }) => <span className={cn("text-xs", row.original.is_active ? "text-success-text" : "text-muted-foreground")}>{row.original.is_active ? "Aktif" : "Nonaktif"}</span>, size: 80 },
    ],
    [],
  );
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Input className="w-64" placeholder="Cari nama / email…" value={q} onChange={(e) => setQ(e.target.value)} />
        <NativeSelect className="w-48" value={role} onChange={(e) => setRole(e.target.value)}><option value="">Role: {t("label.all")}</option>{(roles.data ?? []).map((r) => <option key={r.id} value={r.code}>{r.name}</option>)}</NativeSelect>
        <NativeSelect className="w-36" value={active} onChange={(e) => setActive(e.target.value)}><option value="">Status: {t("label.all")}</option><option value="true">Aktif</option><option value="false">Nonaktif</option></NativeSelect>
        <span className="ml-auto">{can("iam.users.create") && <Button onClick={() => setEdit("new")}><Plus /> Tambah User</Button>}</span>
      </div>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => { if (can("iam.users.update")) setEdit(r); }} loading={list.isLoading} isFiltered={!!q || !!role || !!active} empty={{ message: "Belum ada user." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} rowActions={(r) => [...(can("iam.users.update") ? [{ label: "Edit", onSelect: () => setEdit(r) }] : []), ...(can("iam.users.reset_password") ? [{ label: "Reset password", onSelect: () => setReset(r) }] : [])]} />
      {edit && <UserDialog item={edit === "new" ? null : edit} roles={roles.data ?? []} onClose={() => setEdit(null)} />}
      {reset && <ResetPasswordDialog user={reset} onClose={() => setReset(null)} />}
    </div>
  );
}

function UserDialog({ item, roles, onClose }: { item: User | null; roles: Role[]; onClose: () => void }) {
  const { t } = useTranslation();
  const { properties } = useAuth();
  const toast = useToast();
  const teams = useAll<Team>("teams");
  const [form, setForm] = useState({ full_name: item?.full_name ?? "", email: item?.email ?? "", username: item?.username ?? "", phone: item?.phone ?? "", password: "", is_active: item?.is_active ?? true, roles: item?.roles.map((r) => ({ role_id: r.role_id, property_id: r.property_id })) ?? ([] as { role_id: string; property_id: string | null }[]), team_ids: item?.teams.map((x) => x.team_id) ?? ([] as string[]) });
  const create = useCreate<Record<string, unknown>>("users");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("users");
  const [newRole, setNewRole] = useState({ role_id: "", property_id: "" });
  const submit = async () => {
    if (!form.full_name.trim() || (!form.email && !form.username)) return toast.error(new Error("Nama dan email/username wajib"));
    if (!item && form.password.length < 10) return toast.error(new Error("Password minimal 10 karakter"));
    try {
      if (item) await update.mutateAsync({ id: item.id, version: item.version, full_name: form.full_name, email: form.email || null, username: form.username || null, phone: form.phone || null, is_active: form.is_active, roles: form.roles, team_ids: form.team_ids });
      else await create.mutateAsync({ full_name: form.full_name, email: form.email || null, username: form.username || null, phone: form.phone || null, password: form.password, roles: form.roles, team_ids: form.team_ids });
      toast.success("User disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  const roleName = (id: string) => roles.find((r) => r.id === id)?.name ?? id;
  const propName = (id: string | null) => (id ? properties.find((p) => p.id === id)?.name ?? id : "Semua property");
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit ${item.full_name}` : "Tambah User"}>
        <div className="space-y-4">
          <Field label="Nama lengkap" required><Input value={form.full_name} onChange={(e) => setForm({ ...form, full_name: e.target.value })} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Email"><Input type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} /></Field>
            <Field label="Username" help="Untuk petugas tanpa email"><Input value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} /></Field>
            <Field label="Telepon"><Input value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} /></Field>
            {!item && <Field label="Password awal" required><Input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} /></Field>}
          </div>
          <Field label={`Role (${form.roles.length})`}>
            <ul className="mb-2 divide-y divide-border rounded-md border border-border">
              {form.roles.map((r, i) => <li key={i} className="flex items-center justify-between px-3 py-1.5 text-sm"><span>{roleName(r.role_id)} <span className="text-xs text-muted-foreground">· {propName(r.property_id)}</span></span><Button size="icon-sm" variant="ghost" aria-label="Hapus" onClick={() => setForm({ ...form, roles: form.roles.filter((_, j) => j !== i) })}>×</Button></li>)}
              {form.roles.length === 0 && <li className="px-3 py-2 text-xs text-muted-foreground">Belum ada role.</li>}
            </ul>
            <div className="flex gap-2">
              <NativeSelect value={newRole.role_id} onChange={(e) => setNewRole({ ...newRole, role_id: e.target.value })}><option value="">Pilih role…</option>{roles.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}</NativeSelect>
              <NativeSelect value={newRole.property_id} onChange={(e) => setNewRole({ ...newRole, property_id: e.target.value })}><option value="">Semua property</option>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect>
              <Button variant="secondary" disabled={!newRole.role_id} onClick={() => { setForm({ ...form, roles: [...form.roles, { role_id: newRole.role_id, property_id: newRole.property_id || null }] }); setNewRole({ role_id: "", property_id: "" }); }}>Tambah</Button>
            </div>
          </Field>
          <Field label="Team">
            <div className="max-h-40 space-y-1 overflow-y-auto rounded-md border border-border p-2">
              {(teams.data ?? []).map((tm) => <label key={tm.id} className="flex items-center gap-2 text-sm"><Checkbox checked={form.team_ids.includes(tm.id)} onCheckedChange={() => setForm((f) => ({ ...f, team_ids: f.team_ids.includes(tm.id) ? f.team_ids.filter((x) => x !== tm.id) : [...f.team_ids, tm.id] }))} /> {tm.name} <span className="text-xs text-muted-foreground">{tm.domain}</span></label>)}
            </div>
          </Field>
          {item && <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.is_active} onCheckedChange={(v) => setForm({ ...form, is_active: !!v })} /> Aktif (nonaktif = tidak bisa login, sesi dicabut)</label>}
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ResetPasswordDialog({ user, onClose }: { user: User; onClose: () => void }) {
  const toast = useToast();
  const [pw, setPw] = useState("");
  const reset = useAction<{ new_password: string }>(() => `users/${user.id}/reset-password`);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Reset password · ${user.full_name}`} description="Semua sesi user akan dicabut.">
        <Field label="Password baru" required><Input type="password" value={pw} onChange={(e) => setPw(e.target.value)} /></Field>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={reset.isPending} disabled={pw.length < 10} onClick={() => reset.mutateAsync({ new_password: pw }).then(() => { toast.success("Password direset"); onClose(); }).catch(toast.error)}>Reset</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
