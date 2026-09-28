// Checklist Templates (PRD §11; PRD P0 v2 §12): builder item (ok_notok_na · yes_no · pass_fail · numeric · text · photo ·
// selection · signature), deskripsi, opsi (selection), expected result, rentang angka, section, wajib/foto,
// kategori bebas + domain, publish → versi baru, archive. Validasi spesifikasi = lib/checklist-spec (cermin server).
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { Alert, Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { useToast } from "@/components/bv/common";
import { useAction, useAll, useCreate, useInvalidate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { EXPECTED_TYPES, ITEM_TYPES, expectedHint, normalizeItem, validateItemSpec, type SpecError } from "@/lib/checklist-spec";
import type { ChecklistItemType, ChecklistTemplate, ChecklistTemplateItem } from "@/api/types";

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
      // Tabel disederhanakan (29 Sep 2026): kode + nama template; domain & "berlaku untuk" di tooltip kategori dan dialog detail.
      { id: "name", header: "Template", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.code} title={row.original.name} /> },
      { id: "category", header: t("checklist.category"), meta: { mobile: "hidden" }, cell: ({ row }) => <CellText max={160} muted title={[row.original.category, row.original.domain ?? "umum", row.original.applies_to.join(", ")].filter(Boolean).join(" · ")}>{row.original.category || row.original.domain || "umum"}</CellText>, size: 160 },
      { id: "items", header: "Item", meta: { mobile: "secondary" }, cell: ({ row }) => <span className="tnum">{row.original.items.length}</span>, size: 70 },
      { id: "ver", header: "Versi", meta: { mobile: "secondary" }, cell: ({ row }) => <span className="tnum">v{row.original.current_version}</span>, size: 70 },
      { id: "usage", header: "Dipakai", meta: { mobile: "hidden" }, cell: ({ row }) => <span className="tnum">{row.original.usage_count}</span>, size: 80 },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <StatusBadge objectType="authoring" status={row.original.status} />, size: 110 },
    ],
    [t],
  );
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <NativeSelect className="w-[calc(50%-4px)] sm:w-40" value={status} onChange={(e) => setStatus(e.target.value)} aria-label={t("label.status")}><option value="">Status: {t("label.all")}</option><option value="draft">Draft</option><option value="published">Published</option><option value="archived">Archived</option></NativeSelect>
        <NativeSelect className="w-[calc(50%-4px)] sm:w-40" value={domain} onChange={(e) => setDomain(e.target.value)} aria-label="Domain"><option value="">Domain: {t("label.all")}</option>{["engineering", "security", "housekeeping"].map((d) => <option key={d} value={d}>{d}</option>)}</NativeSelect>
        <span className="ml-auto">{can("operations.checklists.create") && <Button icon="add" onClick={() => setEdit("new")}>{t("checklist.create")}</Button>}</span>
      </div>
      <DataGrid
        columns={columns}
        rows={list.data ?? []}
        rowId={(r) => r.id}
        onRowClick={(r) => setEdit(r)}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={!!status || !!domain}
        empty={{ icon: "checklist", title: t("checklist.empty"), description: t("checklist.empty_desc"), action: can("operations.checklists.create") ? <Button icon="add" onClick={() => setEdit("new")}>{t("checklist.create")}</Button> : undefined }}
        rowActions={(r) => [
          ...(r.status !== "archived" && can("operations.checklists.publish") ? [{ label: r.status === "draft" ? t("action.publish") : t("checklist.publish_new"), icon: "publish", onSelect: () => act.mutateAsync({ id: r.id, action: "publish" }).then(() => toast.action("published", `Template ${r.name}`)).catch((e) => toast.failed("published", e, "Template")) }] : []),
          ...(r.status !== "archived" && can("operations.checklists.archive") ? [{ label: t("action.archive"), icon: "archive", destructive: true, onSelect: () => act.mutateAsync({ id: r.id, action: "archive" }).then(() => toast.action("archived", `Template ${r.name}`)).catch((e) => toast.failed("archived", e, "Template")) }] : []),
        ]}
      />
      {edit && <TemplateDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

const blankItem = (sort: number, section: string | null): ChecklistTemplateItem => ({ sort_order: sort, section, label: "", item_type: "ok_notok_na", is_required: true, photo_required: false, numeric_unit: null, numeric_min: null, numeric_max: null, help_text: null, options: [], expected_value: null });

function TemplateDialog({ item, onClose }: { item: ChecklistTemplate | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const readOnly = item ? item.status === "archived" || !can("operations.checklists.update") : !can("operations.checklists.create");
  const [form, setForm] = useState({ name: item?.name ?? "", description: item?.description ?? "", domain: item?.domain ?? "", category: item?.category ?? "", applies_to: item?.applies_to ?? ["task"], items: (item?.items ?? []).map((x) => ({ ...x, options: x.options ?? [], expected_value: x.expected_value ?? null })) as ChecklistTemplateItem[] });
  const create = useCreate<Record<string, unknown>>("checklist-templates");
  const [saving, setSaving] = useState(false);
  const [touched, setTouched] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const errors = useMemo(() => form.items.map((x) => validateItemSpec(x)), [form.items]);
  const addItem = () => setForm((f) => ({ ...f, items: [...f.items, blankItem(f.items.length + 1, f.items[f.items.length - 1]?.section ?? null)] }));
  const setItem = (i: number, patch: Partial<ChecklistTemplateItem>) => setForm((f) => ({ ...f, items: f.items.map((x, j) => (j === i ? { ...x, ...patch } : x)) }));
  const move = (i: number, d: -1 | 1) => setForm((f) => { const j = i + d; if (j < 0 || j >= f.items.length) return f; const next = [...f.items]; [next[i], next[j]] = [next[j], next[i]]; return { ...f, items: next.map((x, k) => ({ ...x, sort_order: k + 1 })) }; });
  const submit = async () => {
    setTouched(true);
    setErr(null);
    if (!form.name.trim()) return setErr(t("checklist.err_name"));
    if (form.items.length === 0) return setErr(t("checklist.err_no_items"));
    if (errors.some((e) => e.length)) return setErr(t("checklist.err_fix_items"));
    setSaving(true);
    try {
      const body = { name: form.name.trim(), description: form.description || null, domain: form.domain || null, category: form.category.trim() || null, applies_to: form.applies_to, items: form.items.map((x, i) => normalizeItem({ ...x, sort_order: i + 1 })) };
      if (item) await api(`checklist-templates/${item.id}`, { method: "PATCH", body, ifMatch: item.version });
      else await create.mutateAsync(body);
      invalidate("checklist-templates");
      if (item?.status === "published") toast.info(t("checklist.saved_as_draft"));
      else toast.action("saved", `Template ${form.name.trim()}`);
      onClose();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" className="sm:max-w-3xl" title={item ? `${item.code} · ${item.name} (v${item.current_version})` : t("checklist.create")}>
        <div className="space-y-4">
          {err && <Alert variant="critical">{err}</Alert>}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Field label={t("checklist.name")} required className="sm:col-span-3"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} disabled={readOnly} /></Field>
            <Field label="Domain"><NativeSelect value={form.domain} onChange={(e) => setForm({ ...form, domain: e.target.value })} disabled={readOnly}><option value="">{t("checklist.general")}</option>{["engineering", "security", "housekeeping"].map((d) => <option key={d} value={d}>{d}</option>)}</NativeSelect></Field>
            <Field label={t("checklist.category")} help={t("checklist.category_help")} className="sm:col-span-2"><Input value={form.category} onChange={(e) => setForm({ ...form, category: e.target.value })} placeholder="HVAC, Kebersihan toilet, …" disabled={readOnly} /></Field>
          </div>
          <Field label={t("label.description")}><Textarea rows={2} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} disabled={readOnly} /></Field>
          <Field label={t("checklist.applies_to")}><div className="flex flex-wrap gap-3">{APPLIES.map((a) => <label key={a} className="flex items-center gap-1.5 text-sm"><Checkbox checked={form.applies_to.includes(a)} disabled={readOnly} onCheckedChange={() => setForm((f) => ({ ...f, applies_to: f.applies_to.includes(a) ? f.applies_to.filter((x) => x !== a) : [...f.applies_to, a] }))} /> {a}</label>)}</div></Field>
          <div>
            <div className="mb-2 flex items-center justify-between"><span className="text-sm font-medium">Item ({form.items.length})</span>{!readOnly && <Button size="sm" variant="secondary" icon="add" onClick={addItem}>{t("checklist.add_item")}</Button>}</div>
            <div className="space-y-2">
              {form.items.map((x, i) => (
                <ChecklistItemEditor key={i} item={x} index={i} errors={touched ? errors[i] : []} readOnly={readOnly} onChange={(p) => setItem(i, p)} onMove={(d) => move(i, d)} onRemove={() => setForm((f) => ({ ...f, items: f.items.filter((_, j) => j !== i) }))} />
              ))}
              {form.items.length === 0 && <p className="py-4 text-center text-xs text-muted-foreground">{t("checklist.no_items")}</p>}
            </div>
          </div>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>{!readOnly && <Button loading={saving || create.isPending} onClick={submit}>{t("action.save")}</Button>}</DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Editor satu item checklist (dipakai builder dan test). */
export function ChecklistItemEditor({ item: x, index, errors = [], readOnly, onChange, onMove, onRemove }: { item: ChecklistTemplateItem; index: number; errors?: SpecError[]; readOnly?: boolean; onChange: (p: Partial<ChecklistTemplateItem>) => void; onMove?: (d: -1 | 1) => void; onRemove?: () => void }) {
  const { t } = useTranslation();
  const errOf = (f: SpecError["field"]) => errors.filter((e) => e.field === f).map((e) => t(e.key, e.params)).join(" ");
  const opts = x.options ?? [];
  const expectedChoices: Record<string, { value: string; label: string }[]> = {
    ok_notok_na: [{ value: "ok", label: "OK" }],
    yes_no: [{ value: "yes", label: t("checklist.yes") }, { value: "no", label: t("checklist.no") }],
    pass_fail: [{ value: "pass", label: t("checklist.pass") }, { value: "fail", label: t("checklist.fail") }],
  };
  const expSel = (x.expected_value ?? "").split(",").map((v) => v.trim()).filter(Boolean);
  const hint = expectedHint(x);
  return (
    <div className="rounded-md border border-border p-2" data-testid={`item-editor-${index}`}>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-12">
        <Input className="sm:col-span-2" placeholder="Section" aria-label="Section" value={x.section ?? ""} onChange={(e) => onChange({ section: e.target.value || null })} disabled={readOnly} />
        <Input className="sm:col-span-5" placeholder={t("checklist.item_label")} aria-label={t("checklist.item_label")} aria-invalid={errOf("label") ? true : undefined} value={x.label} onChange={(e) => onChange({ label: e.target.value })} disabled={readOnly} />
        <NativeSelect className="sm:col-span-3" aria-label={t("checklist.item_type")} value={x.item_type} onChange={(e) => onChange({ item_type: e.target.value as ChecklistItemType, expected_value: null })} disabled={readOnly}>
          {ITEM_TYPES.map((v) => <option key={v} value={v}>{t(`checklist.type.${v}`)}</option>)}
        </NativeSelect>
        <div className="flex justify-end gap-0.5 sm:col-span-2">
          <Button size="icon-sm" variant="ghost" aria-label={t("checklist.move_up")} disabled={readOnly} onClick={() => onMove?.(-1)}><Icon name="arrow_upward" size={16} /></Button>
          <Button size="icon-sm" variant="ghost" aria-label={t("checklist.move_down")} disabled={readOnly} onClick={() => onMove?.(1)}><Icon name="arrow_downward" size={16} /></Button>
          <Button size="icon-sm" variant="ghost" aria-label={t("checklist.remove")} disabled={readOnly} onClick={onRemove}><Icon name="delete" size={16} /></Button>
        </div>
      </div>
      {errOf("label") && <p className="mt-1 text-xs text-error">{errOf("label")}</p>}
      <Textarea className="mt-2 min-h-[40px]" rows={1} placeholder={t("checklist.description_placeholder")} aria-label={t("checklist.item_description")} value={x.help_text ?? ""} onChange={(e) => onChange({ help_text: e.target.value || null })} disabled={readOnly} />
      {x.item_type === "selection" && (
        <div className="mt-2 space-y-1.5" data-testid="options-editor">
          <div className="text-label uppercase tracking-wide text-on-surface-variant">{t("checklist.options")}</div>
          {opts.map((o, j) => (
            <div key={j} className="flex gap-2">
              <Input className="w-32" placeholder={t("checklist.option_value")} aria-label={`${t("checklist.option_value")} ${j + 1}`} value={o.value} onChange={(e) => onChange({ options: opts.map((p, k) => (k === j ? { ...p, value: e.target.value } : p)) })} disabled={readOnly} />
              <Input className="flex-1" placeholder={t("checklist.option_label")} aria-label={`${t("checklist.option_label")} ${j + 1}`} value={o.label} onChange={(e) => onChange({ options: opts.map((p, k) => (k === j ? { ...p, label: e.target.value } : p)) })} disabled={readOnly} />
              <Button size="icon-sm" variant="ghost" aria-label={t("checklist.remove_option")} disabled={readOnly} onClick={() => onChange({ options: opts.filter((_, k) => k !== j), expected_value: expSel.filter((v) => v !== o.value).join(",") || null })}><Icon name="close" size={16} /></Button>
            </div>
          ))}
          {!readOnly && <Button size="sm" variant="ghost" icon="add" onClick={() => onChange({ options: [...opts, { value: `opsi_${opts.length + 1}`, label: "" }] })}>{t("checklist.add_option")}</Button>}
          {errOf("options") && <p className="text-xs text-error">{errOf("options")}</p>}
        </div>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs">
        <label className="flex items-center gap-1.5"><Checkbox checked={x.is_required} disabled={readOnly} onCheckedChange={(v) => onChange({ is_required: !!v })} /> {t("checklist.required")}</label>
        {x.item_type !== "photo" && x.item_type !== "signature" && <label className="flex items-center gap-1.5"><Checkbox checked={x.photo_required} disabled={readOnly} onCheckedChange={(v) => onChange({ photo_required: !!v })} /> {t("checklist.photo_required")}</label>}
        {x.item_type === "numeric" && (
          <>
            <Input className="h-8 w-20" placeholder="Unit" aria-label="Unit" value={x.numeric_unit ?? ""} onChange={(e) => onChange({ numeric_unit: e.target.value || null })} disabled={readOnly} />
            <Input className="h-8 w-20" type="number" placeholder="Min" aria-label="Min" value={x.numeric_min ?? ""} onChange={(e) => onChange({ numeric_min: e.target.value === "" ? null : Number(e.target.value) })} disabled={readOnly} />
            <Input className="h-8 w-20" type="number" placeholder="Max" aria-label="Max" value={x.numeric_max ?? ""} onChange={(e) => onChange({ numeric_max: e.target.value === "" ? null : Number(e.target.value) })} disabled={readOnly} />
          </>
        )}
        {EXPECTED_TYPES.includes(x.item_type) && (
          <span className="flex flex-wrap items-center gap-2">
            <span className="text-on-surface-variant">{t("checklist.expected_label")}:</span>
            {x.item_type === "selection" ? (
              opts.filter((o) => o.value.trim()).map((o) => (
                <label key={o.value} className="flex items-center gap-1"><Checkbox checked={expSel.includes(o.value)} disabled={readOnly} onCheckedChange={() => onChange({ expected_value: (expSel.includes(o.value) ? expSel.filter((v) => v !== o.value) : [...expSel, o.value]).join(",") || null })} /> {o.label || o.value}</label>
              ))
            ) : (
              <NativeSelect className="h-8 w-36" aria-label={t("checklist.expected_label")} value={x.expected_value ?? ""} onChange={(e) => onChange({ expected_value: e.target.value || null })} disabled={readOnly}>
                <option value="">{t("checklist.no_expected")}</option>
                {(expectedChoices[x.item_type] ?? []).map((c) => <option key={c.value} value={c.value}>{c.label}</option>)}
              </NativeSelect>
            )}
          </span>
        )}
      </div>
      {(errOf("expected_value") || errOf("numeric")) && <p className="mt-1 text-xs text-error">{[errOf("expected_value"), errOf("numeric")].filter(Boolean).join(" ")}</p>}
      {hint && <p className="mt-1 text-caption text-on-surface-variant">{hint}</p>}
    </div>
  );
}
