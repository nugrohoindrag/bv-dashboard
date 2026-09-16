// Roles & Permissions (PRD §22.3): role sistem (read-only) + role kustom dengan matriks permission modul.objek.aksi.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { Button, Checkbox, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { useAll, useCreate, useInvalidate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import type { Role } from "@/api/types";

interface Perm { code: string; module: string; object: string; action: string }

export default function RolesSection() {
  const { can } = useAuth();
  const roles = useAll<Role>("roles");
  const perms = useAll<Perm>("permissions");
  const [sel, setSel] = useState<Role | null | "new">(null);
  return (
    <div className="grid grid-cols-12 gap-5">
      <div className="col-span-4 space-y-2">
        {can("iam.roles.create") && <Button className="w-full" onClick={() => setSel("new")}><Icon name="add" size={16} /> Role kustom</Button>}
        <ul className="divide-y divide-border rounded-md border border-border">
          {(roles.data ?? []).map((r) => (
            <li key={r.id}><button type="button" className={cn("flex w-full items-center justify-between px-3 py-2 text-left text-sm hover:bg-muted", sel !== "new" && sel?.id === r.id && "bg-brand-50")} onClick={() => setSel(r)}><span><span className="font-medium">{r.name}</span><span className="ml-1 text-xs text-muted-foreground">{r.is_system ? "sistem" : "kustom"}{r.domain ? ` · ${r.domain}` : ""}</span></span><span className="text-xs text-muted-foreground tnum">{r.user_count} user · {r.permissions.length} izin</span></button></li>
          ))}
        </ul>
      </div>
      <div className="col-span-8">
        {sel ? <RoleEditor role={sel === "new" ? null : sel} perms={perms.data ?? []} onClose={() => setSel(null)} /> : <p className="py-10 text-center text-sm text-muted-foreground">Pilih role untuk melihat izin.</p>}
      </div>
    </div>
  );
}

function RoleEditor({ role, perms, onClose }: { role: Role | null; perms: Perm[]; onClose: () => void }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const readOnly = !!role?.is_system || (role ? !can("iam.roles.update") : !can("iam.roles.create"));
  const [form, setForm] = useState({ code: role?.code ?? "", name: role?.name ?? "", domain: role?.domain ?? "", permissions: new Set(role?.permissions ?? []) });
  const [saving, setSaving] = useState(false);
  const [del, setDel] = useState(false);
  const create = useCreate<Record<string, unknown>>("roles");
  const grouped = useMemo(() => {
    const m = new Map<string, Map<string, Perm[]>>();
    for (const p of perms) {
      if (!m.has(p.module)) m.set(p.module, new Map());
      const o = m.get(p.module)!;
      if (!o.has(p.object)) o.set(p.object, []);
      o.get(p.object)!.push(p);
    }
    return m;
  }, [perms]);
  const has = (code: string) => form.permissions.has(code) || form.permissions.has("*") || form.permissions.has(code.split(".")[0] + ".*") || form.permissions.has(code.split(".").slice(0, 2).join(".") + ".*");
  const toggle = (code: string) => setForm((f) => { const s = new Set(f.permissions); if (s.has(code)) s.delete(code); else s.add(code); return { ...f, permissions: s }; });
  const save = async () => {
    if (!form.code.trim() || !form.name.trim()) return toast.error(new Error("Kode dan nama wajib"));
    setSaving(true);
    try {
      const body = { code: form.code.trim(), name: form.name.trim(), domain: form.domain || null, permissions: [...form.permissions] };
      if (role) await api(`roles/${role.id}`, { method: "PATCH", body });
      else await create.mutateAsync(body);
      invalidate("roles");
      toast.success("Role disimpan");
      if (!role) onClose();
    } catch (e) {
      toast.error(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <div className="space-y-4 rounded-lg border border-border bg-card p-4">
      <div className="grid grid-cols-3 gap-3">
        <Field label="Kode" required><Input value={form.code} onChange={(e) => setForm({ ...form, code: e.target.value })} disabled={!!role} /></Field>
        <Field label="Nama" required><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} disabled={readOnly} /></Field>
        <Field label="Domain"><NativeSelect value={form.domain} onChange={(e) => setForm({ ...form, domain: e.target.value })} disabled={readOnly}><option value="">—</option>{["management", "engineering", "security", "housekeeping"].map((d) => <option key={d} value={d}>{d}</option>)}</NativeSelect></Field>
      </div>
      {role?.is_system && <p className="rounded-md bg-info-soft px-3 py-2 text-sm text-info-text">Role sistem bersifat read-only. Buat role kustom untuk kombinasi izin berbeda.</p>}
      <div className="max-h-[60vh] overflow-y-auto rounded-md border border-border">
        {[...grouped.entries()].map(([mod, objs]) => (
          <div key={mod}>
            <div className="sticky top-0 bg-muted px-3 py-1.5 text-xs font-semibold uppercase text-muted-foreground">{mod}</div>
            {[...objs.entries()].map(([obj, list]) => (
              <div key={obj} className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-border px-3 py-1.5 text-sm">
                <span className="w-44 shrink-0 font-medium">{obj}</span>
                {list.map((p) => <label key={p.code} className="flex items-center gap-1.5 text-xs"><Checkbox checked={has(p.code)} disabled={readOnly} onCheckedChange={() => toggle(p.code)} /> {p.action}</label>)}
              </div>
            ))}
          </div>
        ))}
      </div>
      <div className="flex justify-end gap-2">
        {role && !role.is_system && can("iam.roles.delete") && (del ? <><span className="text-sm text-muted-foreground">Hapus role ini?</span><Button variant="destructive" onClick={() => api(`roles/${role.id}`, { method: "DELETE" }).then(() => { invalidate("roles"); toast.success("Role dihapus"); onClose(); }).catch(toast.error)}>Ya, hapus</Button></> : <Button variant="ghost" className="mr-auto" onClick={() => setDel(true)}>Hapus</Button>)}
        <Button variant="secondary" onClick={onClose}>Tutup</Button>
        {!readOnly && <Button loading={saving} onClick={save}>{t("action.save")}</Button>}
      </div>
    </div>
  );
}
