// Portfolio (PRD P0 v2 §4.1, §7): Organization → Portfolio → Property. Daftar + buat/edit (kode, nama, deskripsi, aktif),
// hapus (property dilepas, tidak ikut terhapus), drawer daftar property anggota. Perm property.portfolios.*.
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, ConfirmDialog, Dialog, DialogContent, DialogFooter, Field, Input, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { CellText, CellTitle } from "@/components/bv/cells";
import { useToast } from "@/components/bv/common";
import { CardSkeleton, QueryErrorState } from "@/components/bv/states";
import { Fab } from "@/components/bv/mobile";
import { useAll, useInvalidate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { Location, Portfolio } from "@/api/types";
import { LocationStatusBadge } from "./LocationsPage";

export default function PortfoliosPage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const list = useAll<Portfolio>("portfolios", { include_inactive: true });
  const [edit, setEdit] = useState<Portfolio | "new" | null>(null);
  const [del, setDel] = useState<Portfolio | null>(null);
  const [open, setOpen] = useState<Portfolio | null>(null);
  const toast = useToast();
  const invalidate = useInvalidate();
  const columns = useMemo<ColumnDef<Portfolio, unknown>[]>(
    () => [
      // Pola tabel Tasks (29 Sep 2026): kode kecil di atas nama satu baris; deskripsi dipotong "…" (lengkap di tooltip).
      { id: "name", header: t("portfolio.name"), meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.code} title={row.original.name} /> },
      { id: "description", header: t("label.description"), meta: { mobile: "secondary" }, cell: ({ row }) => <CellText max={320} muted>{row.original.description ?? "—"}</CellText> },
      { id: "properties", header: t("nav.properties"), meta: { mobile: "secondary", nowrap: true }, cell: ({ row }) => <span className="tnum">{row.original.property_count}</span>, size: 100 },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <Badge tone={row.original.is_active ? "success" : "neutral"}>{row.original.is_active ? "Aktif" : "Nonaktif"}</Badge>, size: 100 },
    ],
    [t],
  );
  const remove = async (p: Portfolio) => {
    try {
      await api(`portfolios/${p.id}`, { method: "DELETE" });
      invalidate("portfolios");
      toast.action("deleted", `Portfolio ${p.name}`);
    } catch (e) {
      toast.failed("deleted", e, "Portfolio");
    }
  };
  return (
    <div>
      <PageHeader title={t("nav.portfolios")} subtitle={t("portfolio.subtitle")} actions={can("property.portfolios.create") && <span className="hidden md:inline-flex"><Button icon="add" onClick={() => setEdit("new")}>{t("portfolio.create")}</Button></span>} />
      <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} onRowClick={(r) => setOpen(r)} isLoading={list.isLoading} error={list.error} onRetry={() => list.refetch()}
        empty={{ icon: "hub", title: t("portfolio.empty"), description: t("portfolio.empty_desc"), action: can("property.portfolios.create") ? <Button icon="add" onClick={() => setEdit("new")}>{t("portfolio.create")}</Button> : undefined }}
        rowActions={(r) => [
          ...(can("property.portfolios.update") ? [{ label: "Edit", icon: "edit", onSelect: () => setEdit(r) }] : []),
          ...(can("property.portfolios.delete") ? [{ label: t("loc.delete"), icon: "delete", destructive: true, onSelect: () => setDel(r) }] : []),
        ]} />
      {can("property.portfolios.create") && <Fab label={t("action.create")} aria-label={t("portfolio.create")} onClick={() => setEdit("new")} />}
      {edit && <PortfolioDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
      {open && <PortfolioDrawer portfolio={open} onClose={() => setOpen(null)} onEdit={can("property.portfolios.update") ? () => { setEdit(open); setOpen(null); } : undefined} />}
      {del && <ConfirmDialog open onOpenChange={(o) => !o && setDel(null)} destructive title={t("portfolio.delete_title", { name: del.name })} description={t("portfolio.delete_desc")} confirmLabel={t("loc.delete")} onConfirm={() => { const p = del; setDel(null); remove(p); }} />}
    </div>
  );
}

function PortfolioDialog({ item, onClose }: { item: Portfolio | null; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const invalidate = useInvalidate();
  const [form, setForm] = useState({ code: item?.code ?? "", name: item?.name ?? "", description: item?.description ?? "", is_active: item?.is_active ?? true });
  const [err, setErr] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const submit = async () => {
    setErr(null);
    if (!form.name.trim()) return setErr(t("portfolio.err_name"));
    setSaving(true);
    try {
      const body = { code: form.code.trim() || undefined, name: form.name.trim(), description: form.description.trim() || null, is_active: form.is_active };
      if (item) await api(`portfolios/${item.id}`, { method: "PATCH", body, ifMatch: item.version });
      else await api("portfolios", { body });
      invalidate("portfolios");
      toast.action(item ? "saved" : "created", `Portfolio ${form.name.trim()}`);
      onClose();
    } catch (e) {
      const c = (e as { code?: string }).code;
      setErr(c === "DUPLICATE_CODE" ? t("portfolio.err_code") : (e as Error).message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={item ? `Edit ${item.name}` : t("portfolio.create")}>
        <div className="space-y-4">
          {err && <Alert variant="critical">{err}</Alert>}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Field label={t("portfolio.code")} help={item ? undefined : t("loc.code_help")}><Input value={form.code} placeholder="auto" onChange={(e) => setForm({ ...form, code: e.target.value.toUpperCase() })} /></Field>
            <Field label={t("portfolio.name")} required className="sm:col-span-2"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></Field>
          </div>
          <Field label={t("label.description")}><Textarea rows={2} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} /></Field>
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.is_active} onCheckedChange={(v) => setForm({ ...form, is_active: !!v })} /> Aktif</label>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={saving} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PortfolioDrawer({ portfolio, onClose, onEdit }: { portfolio: Portfolio; onClose: () => void; onEdit?: () => void }) {
  const { t } = useTranslation();
  const props = useQuery({ queryKey: ["portfolio-properties", portfolio.id], queryFn: () => api<{ data: Location[] }>(`portfolios/${portfolio.id}/properties`).then((r) => r.data) });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={portfolio.name} description={`${portfolio.code}${portfolio.description ? " · " + portfolio.description : ""}`}>
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-sm font-semibold">{t("nav.properties")} ({props.data?.length ?? portfolio.property_count})</span>
            {onEdit && <Button size="sm" variant="secondary" icon="edit" onClick={onEdit}>Edit</Button>}
          </div>
          {props.isLoading ? <CardSkeleton lines={3} /> : props.isError ? <QueryErrorState error={props.error} onRetry={() => props.refetch()} compact /> : (
            <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
              {(props.data ?? []).map((p) => (
                <li key={p.id} className="flex items-center justify-between gap-2 px-3 py-2.5">
                  <Link to={`/property/locations/${p.id}`} className="min-w-0 hover:underline"><span className="font-mono text-xs text-on-surface-variant">{p.code}</span> <span className="font-medium">{p.name}</span></Link>
                  <LocationStatusBadge loc={p} />
                </li>
              ))}
              {(props.data ?? []).length === 0 && <li className="px-3 py-6 text-center text-sm text-on-surface-variant">{t("portfolio.no_properties")}</li>}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
