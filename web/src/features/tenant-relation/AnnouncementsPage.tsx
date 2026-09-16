// Tenant Relation › Announcements (PRD P1 v1.3 §3.5 Communication; Tenant App Home "Important Announcement").
// Draft → Published (notifikasi ke seluruh tenant user property) → Archived.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useList, useUpdate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";

interface Announcement { id: string; property_id: string | null; property_name: string | null; title: string; excerpt: string | null; body: string; audience: string; importance: string; status: string; published_at: string | null; expires_at: string | null; created_at: string; created_by_name: string | null; version: number; allowed_actions: string[] }

export default function AnnouncementsPage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const toast = useToast();
  const [status, setStatus] = useState("");
  const [edit, setEdit] = useState<Announcement | "new" | null>(null);
  const list = useList<Announcement>("announcements", { property_id: propertyId ?? undefined, status: status || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const act = useAction<{ id: string; action: string }, Announcement>((i) => `announcements/${i.id}/${i.action}`, { body: () => ({}), invalidate: ["list", "one"] });
  const columns = useMemo<ColumnDef<Announcement, unknown>[]>(() => [
    { id: "title", header: "Judul", cell: ({ row }) => <div><div className="font-medium">{row.original.importance === "important" && <Icon name="priority_high" size={14} className="mr-1 align-middle text-warning" />}{row.original.title}</div><div className="line-clamp-1 text-xs text-muted-foreground">{row.original.excerpt ?? row.original.body}</div></div> },
    { id: "scope", header: "Lingkup", cell: ({ row }) => <span className="text-xs">{row.original.property_name ?? "Seluruh organisasi"} · {row.original.audience}</span>, size: 200 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={row.original.status === "published" ? "success" : row.original.status === "draft" ? "neutral" : "neutral"}>{row.original.status}</Badge>, size: 110 },
    { id: "published_at", header: "Dipublikasikan", cell: ({ row }) => <span className="text-xs text-muted-foreground">{row.original.published_at ? <RelativeTime value={row.original.published_at} /> : "—"}</span>, size: 130 },
    { id: "created_at", header: "Dibuat", cell: ({ row }) => <span className="text-xs text-muted-foreground"><RelativeTime value={row.original.created_at} /> · {row.original.created_by_name}</span>, size: 170 },
  ], [t]);
  return (
    <div>
      <PageHeader title="Announcements" subtitle="Informasi dari building management ke tenant (tampil di Tenant App)." actions={can("tenant_relation.announcements.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Buat Announcement</Button>}>
        <NativeSelect className="w-44" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option><option value="draft">Draft</option><option value="published">Published</option><option value="archived">Archived</option></NativeSelect>
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => { setEdit(r); }} loading={list.isLoading} isFiltered={!!status} empty={{ message: "Belum ada announcement." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage}
        rowActions={(r) => [
          ...(r.allowed_actions.includes("publish") ? [{ label: t("action.publish"), icon: "publish", onSelect: () => act.mutateAsync({ id: r.id, action: "publish" }).then(() => toast.success("Dipublikasikan ke tenant")).catch(toast.error) }] : []),
          ...(r.allowed_actions.includes("archive") ? [{ label: t("action.archive"), icon: "archive", onSelect: () => act.mutateAsync({ id: r.id, action: "archive" }).then(() => toast.success("Diarsipkan")).catch(toast.error) }] : []),
        ]} />
      {edit && <AnnouncementDialog item={edit === "new" ? null : edit} propertyId={propertyId} onClose={() => setEdit(null)} />}
    </div>
  );
}

function AnnouncementDialog({ item, propertyId, onClose }: { item: Announcement | null; propertyId: string | null; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [form, setForm] = useState({ title: item?.title ?? "", excerpt: item?.excerpt ?? "", body: item?.body ?? "", audience: item?.audience ?? "tenant", importance: item?.importance ?? "normal", orgWide: item ? item.property_id === null : false });
  const create = useAction<Record<string, unknown>, Announcement>(() => "announcements", { invalidate: ["list"] });
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("announcements");
  const readOnly = !!item && !item.allowed_actions.includes("update");
  const submit = async () => {
    try {
      const body = { title: form.title.trim(), excerpt: form.excerpt.trim() || null, body: form.body.trim(), audience: form.audience, importance: form.importance };
      if (item) await update.mutateAsync({ id: item.id, version: item.version, ...body });
      else await create.mutateAsync({ ...body, property_id: form.orgWide ? null : propertyId });
      toast.success("Announcement disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Announcement · ${item.status}` : "Buat Announcement"} description="Disimpan sebagai draft; publikasikan dari daftar untuk mengirim notifikasi ke tenant.">
        <div className="space-y-4">
          <Field label="Judul" required><Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} disabled={readOnly} /></Field>
          <Field label="Ringkasan (tampil di kartu Home)"><Input value={form.excerpt} onChange={(e) => setForm({ ...form, excerpt: e.target.value })} disabled={readOnly} /></Field>
          <Field label="Isi" required><Textarea rows={8} value={form.body} onChange={(e) => setForm({ ...form, body: e.target.value })} disabled={readOnly} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Audiens"><NativeSelect value={form.audience} onChange={(e) => setForm({ ...form, audience: e.target.value })} disabled={readOnly}><option value="tenant">Tenant</option><option value="staff">Staf</option><option value="all">Semua</option></NativeSelect></Field>
            <Field label="Prioritas"><NativeSelect value={form.importance} onChange={(e) => setForm({ ...form, importance: e.target.value })} disabled={readOnly}><option value="normal">Normal</option><option value="important">Penting (Important Announcement)</option></NativeSelect></Field>
          </div>
          {!item && <Checkbox label="Berlaku untuk seluruh property organisasi" checked={form.orgWide} onCheckedChange={(v) => setForm({ ...form, orgWide: v })} disabled={!propertyId ? true : false} />}
          {!propertyId && !item && <p className="text-xs text-muted-foreground">Tidak ada property terpilih — announcement dibuat untuk seluruh organisasi.</p>}
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>{!readOnly && <Button loading={create.isPending || update.isPending} disabled={!form.title.trim() || !form.body.trim()} onClick={submit}>{t("action.save")}</Button>}</DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
