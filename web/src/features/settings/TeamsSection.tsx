// Teams (PRD §22.4): team per domain & property, anggota + lead.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { useToast } from "@/components/bv/common";
import { useAll, useCreate, useInvalidate, useUsers } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import type { Team } from "@/api/types";

export default function TeamsSection() {
  const { propertyId, can } = useAuth();
  const list = useAll<Team>("teams", { property_id: propertyId ?? undefined });
  const [edit, setEdit] = useState<Team | null | "new">(null);
  const columns = useMemo<ColumnDef<Team, unknown>[]>(
    () => [
      { id: "name", header: "Team", cell: ({ row }) => <span className="font-medium">{row.original.name}</span> },
      { id: "domain", header: "Domain", cell: ({ row }) => <span className="rounded-full bg-neutral-soft px-2 py-0.5 text-xs text-neutral-text">{row.original.domain}</span>, size: 120 },
      { id: "members", header: "Anggota", cell: ({ row }) => <span className="text-xs">{row.original.members.map((m) => m.full_name + (m.is_lead ? " (lead)" : "")).join(", ") || "—"}</span> },
      { id: "active", header: "Aktif", cell: ({ row }) => <span className={cn("text-xs", row.original.is_active ? "text-success-text" : "text-muted-foreground")}>{row.original.is_active ? "Aktif" : "Nonaktif"}</span>, size: 80 },
    ],
    [],
  );
  return (
    <div className="space-y-3">
      <div className="flex justify-end">{can("iam.teams.create") && <Button onClick={() => setEdit("new")}><Plus /> Buat Team</Button>}</div>
      <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} onRowClick={(r) => { if (can("iam.teams.update")) setEdit(r); }} loading={list.isLoading} empty={{ message: "Belum ada team." }} />
      {edit && <TeamDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

function TeamDialog({ item, onClose }: { item: Team | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId, properties, can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const users = useUsers({ is_active: true });
  const [uq, setUq] = useState("");
  const [form, setForm] = useState({ property_id: item?.property_id ?? propertyId ?? "", name: item?.name ?? "", domain: item?.domain ?? "engineering", is_active: item?.is_active ?? true, members: item?.members.map((m) => ({ user_id: m.user_id, is_lead: m.is_lead })) ?? ([] as { user_id: string; is_lead: boolean }[]) });
  const create = useCreate<Record<string, unknown>>("teams");
  const [saving, setSaving] = useState(false);
  const setMember = (uid: string, on: boolean, lead?: boolean) => setForm((f) => { const others = f.members.filter((m) => m.user_id !== uid); return { ...f, members: on ? [...others, { user_id: uid, is_lead: lead ?? f.members.find((m) => m.user_id === uid)?.is_lead ?? false }] : others }; });
  const submit = async () => {
    if (!form.name.trim()) return toast.error(new Error("Nama team wajib"));
    setSaving(true);
    try {
      const body = { property_id: form.property_id || null, name: form.name.trim(), domain: form.domain, is_active: form.is_active, members: form.members };
      if (item) await api(`teams/${item.id}`, { method: "PATCH", body });
      else await create.mutateAsync(body);
      invalidate("teams", "users");
      toast.success("Team disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Team · ${item.name}` : "Buat Team"}>
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Nama" required className="col-span-2"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></Field>
            <Field label="Domain" required><NativeSelect value={form.domain} onChange={(e) => setForm({ ...form, domain: e.target.value })}>{["engineering", "security", "housekeeping", "management"].map((d) => <option key={d} value={d}>{d}</option>)}</NativeSelect></Field>
            <Field label="Property"><NativeSelect value={form.property_id} onChange={(e) => setForm({ ...form, property_id: e.target.value })}><option value="">Lintas property</option>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>
          </div>
          <Field label={`Anggota (${form.members.length})`}>
            <Input className="mb-2" placeholder="Cari user…" value={uq} onChange={(e) => setUq(e.target.value)} />
            <div className="max-h-64 divide-y divide-border overflow-y-auto rounded-md border border-border">
              {(users.data ?? []).filter((u) => !uq || u.full_name.toLowerCase().includes(uq.toLowerCase())).map((u) => {
                const m = form.members.find((x) => x.user_id === u.id);
                return (
                  <div key={u.id} className="flex items-center justify-between px-3 py-1.5 text-sm">
                    <label className="flex items-center gap-2"><Checkbox checked={!!m} onCheckedChange={(v) => setMember(u.id, !!v)} /> {u.full_name} <span className="text-xs text-muted-foreground">{u.roles.map((r) => r.role_name).join(", ")}</span></label>
                    {m && <label className="flex items-center gap-1 text-xs"><Checkbox checked={m.is_lead} onCheckedChange={(v) => setMember(u.id, true, !!v)} /> Lead</label>}
                  </div>
                );
              })}
            </div>
          </Field>
          {item && <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.is_active} onCheckedChange={(v) => setForm({ ...form, is_active: !!v })} /> Aktif</label>}
        </div>
        <DialogFooter>
          {item && can("iam.teams.delete") && <Button variant="ghost" className="mr-auto" onClick={() => api(`teams/${item.id}`, { method: "DELETE" }).then(() => { invalidate("teams"); toast.success("Team dihapus"); onClose(); }).catch(toast.error)}>Hapus</Button>}
          <Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>
          <Button loading={saving || create.isPending} onClick={submit}>{t("action.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
