// Checklist Templates (PRD §11): builder item (ok_notok_na · yes_no · numeric · text · photo), section, wajib/foto, publish → versi baru, archive.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { StatusBadge } from "@/components/bv/badges";
import { useToast } from "@/components/bv/common";
import { useAction, useAll, useCreate, useInvalidate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { ChecklistTemplate, ChecklistTemplateItem } from "@/api/types";

const ITEM_TYPES: { value: ChecklistTemplateItem["item_type"]; label: string }[] = [{ value: "ok_notok_na", label: "OK / Not OK / N/A" }, { value: "yes_no", label: "Ya / Tidak" }, { value: "numeric", label: "Angka" }, { value: "text", label: "Teks" }, { value: "photo", label: "Foto" }];
const APPLIES = ["task", "work_order", "inspection", "patrol", "cleaning", "maintenance_plan"];

export default function ChecklistsSection() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const [status, setStatus] = useState("");
  const [domain, setDomain] = useState("");
  const list = useAll<ChecklistTemplate>("checklist-templates", { status: status || undefined, domain: domain || undefined });
  const [edit, setEdit] = useState<ChecklistTemplate | null | "new">(null);
  const act = useAction<{ id: string; action: "publish" | "archive" }>((i) => `checklist-templates/${i.id}/${i.action}`, { body: () => ({}) });
  const columns = useMemo<ColumnDef<ChecklistTemplate, unknown>[]>(
    () => [
      { id: "code", header: "Kode", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.code}</span>, size: 130 },
      { id: "name", header: "Template", cell: ({ row }) => <div><div className="font-medium">{row.original.name}</div><div className="text-xs text-muted-foreground">{row.original.domain ?? "umum"} · {row.original.applies_to.join(", ")}</div></div> },
      { id: "items", header: "Item", cell: ({ row }) => <span className="tnum">{row.original.items.length}</span>, size: 70 },
      { id: "ver", header: "Versi", cell: ({ row }) => <span className="tnum">v{row.original.current_version}</span>, size: 70 },
      { id: "usage", header: "Dipakai", cell: ({ row }) => <span className="tnum">{row.original.usage_count}</span>, size: 80 },
      { id: "status", header: t("label.status"), cell: ({ row }) => <StatusBadge objectType="authoring" status={row.original.status} />, size: 110 },
    ],
    [t],
  );
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <NativeSelect className="w-40" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option><option value="draft">Draft</option><option value="published">Published</option><option value="archived">Archived</option></NativeSelect>
        <NativeSelect className="w-40" value={domain} onChange={(e) => setDomain(e.target.value)}><option value="">Domain: {t("label.all")}</option>{["engineering", "security", "housekeeping"].map((d) => <option key={d} value={d}>{d}</option>)}</NativeSelect>
        <span className="ml-auto">{can("operations.checklists.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Buat Template</Button>}</span>
      </div>
      <DataGrid
        columns={columns}
        rows={list.data ?? []}
        rowId={(r) => r.id}
        onRowClick={(r) => setEdit(r)}
        loading={list.isLoading}
        empty={{ message: "Belum ada checklist template." }}
        rowActions={(r) => [
          ...(r.status !== "archived" && can("operations.checklists.publish") ? [{ label: r.status === "draft" ? t("action.publish") : "Publikasikan versi baru", onSelect: () => act.mutateAsync({ id: r.id, action: "publish" }).then(() => toast.success("Template dipublikasikan")).catch(toast.error) }] : []),
          ...(r.status !== "archived" && can("operations.checklists.archive") ? [{ label: t("action.archive"), destructive: true, onSelect: () => act.mutateAsync({ id: r.id, action: "archive" }).then(() => toast.success("Template diarsipkan")).catch(toast.error) }] : []),
        ]}
      />
      {edit && <TemplateDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

function TemplateDialog({ item, onClose }: { item: ChecklistTemplate | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const readOnly = item ? item.status === "archived" || !can("operations.checklists.update") : !can("operations.checklists.create");
  const [form, setForm] = useState({ name: item?.name ?? "", description: item?.description ?? "", domain: item?.domain ?? "", applies_to: item?.applies_to ?? ["task"], items: (item?.items ?? []).map((x) => ({ ...x })) as ChecklistTemplateItem[] });
  const create = useCreate<Record<string, unknown>>("checklist-templates");
  const [saving, setSaving] = useState(false);
  const addItem = () => setForm((f) => ({ ...f, items: [...f.items, { sort_order: f.items.length + 1, section: f.items[f.items.length - 1]?.section ?? null, label: "", item_type: "ok_notok_na", is_required: true, photo_required: false, numeric_unit: null, numeric_min: null, numeric_max: null, help_text: null }] }));
  const setItem = (i: number, patch: Partial<ChecklistTemplateItem>) => setForm((f) => ({ ...f, items: f.items.map((x, j) => (j === i ? { ...x, ...patch } : x)) }));
  const move = (i: number, d: -1 | 1) => setForm((f) => { const j = i + d; if (j < 0 || j >= f.items.length) return f; const next = [...f.items]; [next[i], next[j]] = [next[j], next[i]]; return { ...f, items: next.map((x, k) => ({ ...x, sort_order: k + 1 })) }; });
  const submit = async () => {
    if (!form.name.trim()) return toast.error(new Error("Nama wajib"));
    if (form.items.length === 0 || form.items.some((x) => !x.label.trim())) return toast.error(new Error("Minimal 1 item dan semua label terisi"));
    setSaving(true);
    try {
      const body = { name: form.name.trim(), description: form.description || null, domain: form.domain || null, applies_to: form.applies_to, items: form.items.map((x, i) => ({ ...x, sort_order: i + 1, numeric_min: x.numeric_min === null || x.numeric_min === undefined || (x.numeric_min as unknown) === "" ? null : Number(x.numeric_min), numeric_max: x.numeric_max === null || x.numeric_max === undefined || (x.numeric_max as unknown) === "" ? null : Number(x.numeric_max) })) };
      if (item) await api(`checklist-templates/${item.id}`, { method: "PATCH", body, ifMatch: item.version });
      else await create.mutateAsync(body);
      invalidate("checklist-templates");
      toast.success(item?.status === "published" ? "Perubahan disimpan sebagai draft versi berikutnya. Publikasikan untuk dipakai." : "Template disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" className="sm:max-w-3xl" title={item ? `${item.code} · ${item.name} (v${item.current_version})` : "Buat Checklist Template"}>
        <div className="space-y-4">
          <div className="grid grid-cols-3 gap-3">
            <Field label="Nama" required className="col-span-2"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} disabled={readOnly} /></Field>
            <Field label="Domain"><NativeSelect value={form.domain} onChange={(e) => setForm({ ...form, domain: e.target.value })} disabled={readOnly}><option value="">Umum</option>{["engineering", "security", "housekeeping"].map((d) => <option key={d} value={d}>{d}</option>)}</NativeSelect></Field>
          </div>
          <Field label={t("label.description")}><Textarea rows={2} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} disabled={readOnly} /></Field>
          <Field label="Berlaku untuk"><div className="flex flex-wrap gap-3">{APPLIES.map((a) => <label key={a} className="flex items-center gap-1.5 text-sm"><Checkbox checked={form.applies_to.includes(a)} disabled={readOnly} onCheckedChange={() => setForm((f) => ({ ...f, applies_to: f.applies_to.includes(a) ? f.applies_to.filter((x) => x !== a) : [...f.applies_to, a] }))} /> {a}</label>)}</div></Field>
          <div>
            <div className="mb-2 flex items-center justify-between"><span className="text-sm font-medium">Item ({form.items.length})</span>{!readOnly && <Button size="sm" variant="secondary" onClick={addItem}><Icon name="add" size={16} /> Tambah item</Button>}</div>
            <div className="space-y-2">
              {form.items.map((x, i) => (
                <div key={i} className="rounded-md border border-border p-2">
                  <div className="grid grid-cols-12 gap-2">
                    <Input className="col-span-2" placeholder="Section" value={x.section ?? ""} onChange={(e) => setItem(i, { section: e.target.value || null })} disabled={readOnly} />
                    <Input className="col-span-5" placeholder="Label item" value={x.label} onChange={(e) => setItem(i, { label: e.target.value })} disabled={readOnly} />
                    <NativeSelect className="col-span-3" value={x.item_type} onChange={(e) => setItem(i, { item_type: e.target.value as ChecklistTemplateItem["item_type"] })} disabled={readOnly}>{ITEM_TYPES.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect>
                    <div className="col-span-2 flex justify-end gap-0.5">
                      <Button size="icon-sm" variant="ghost" aria-label="Naik" disabled={readOnly} onClick={() => move(i, -1)}><Icon name="arrow_upward" size={16} /></Button>
                      <Button size="icon-sm" variant="ghost" aria-label="Turun" disabled={readOnly} onClick={() => move(i, 1)}><Icon name="arrow_downward" size={16} /></Button>
                      <Button size="icon-sm" variant="ghost" aria-label="Hapus" disabled={readOnly} onClick={() => setForm((f) => ({ ...f, items: f.items.filter((_, j) => j !== i) }))}><Icon name="delete" size={16} /></Button>
                    </div>
                  </div>
                  <div className="mt-2 flex flex-wrap items-center gap-4 text-xs">
                    <label className="flex items-center gap-1.5"><Checkbox checked={x.is_required} disabled={readOnly} onCheckedChange={(v) => setItem(i, { is_required: !!v })} /> Wajib</label>
                    <label className="flex items-center gap-1.5"><Checkbox checked={x.photo_required} disabled={readOnly} onCheckedChange={(v) => setItem(i, { photo_required: !!v })} /> Wajib foto</label>
                    {x.item_type === "numeric" && (
                      <>
                        <Input className="h-7 w-20" placeholder="Unit" value={x.numeric_unit ?? ""} onChange={(e) => setItem(i, { numeric_unit: e.target.value || null })} disabled={readOnly} />
                        <Input className="h-7 w-20" type="number" placeholder="Min" value={x.numeric_min ?? ""} onChange={(e) => setItem(i, { numeric_min: e.target.value === "" ? null : Number(e.target.value) })} disabled={readOnly} />
                        <Input className="h-7 w-20" type="number" placeholder="Max" value={x.numeric_max ?? ""} onChange={(e) => setItem(i, { numeric_max: e.target.value === "" ? null : Number(e.target.value) })} disabled={readOnly} />
                      </>
                    )}
                    <Input className="h-7 flex-1" placeholder="Petunjuk (opsional)" value={x.help_text ?? ""} onChange={(e) => setItem(i, { help_text: e.target.value || null })} disabled={readOnly} />
                  </div>
                </div>
              ))}
              {form.items.length === 0 && <p className="py-4 text-center text-xs text-muted-foreground">Belum ada item.</p>}
            </div>
          </div>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>{!readOnly && <Button loading={saving || create.isPending} onClick={submit}>{t("action.save")}</Button>}</DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
