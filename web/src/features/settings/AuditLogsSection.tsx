// Audit Log (PRD §23): jejak perubahan konfigurasi & aksi sensitif; filter entitas/aksi/aktor/tanggal; detail before/after.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Button, Dialog, DialogContent, DialogFooter, Input, NativeSelect } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { UserPicker } from "@/components/bv/pickers";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";

interface AuditLog { id: string; actor_user_id: string | null; actor_name: string; action: string; entity_type: string; entity_id: string | null; entity_label: string | null; before: unknown; after: unknown; ip: string | null; request_id: string | null; occurred_at: string; message: string }

const ENTITIES = ["user", "role", "team", "organization", "location", "asset", "equipment", "checklist_template", "sla_policy", "maintenance_plan", "export", "session"];

export default function AuditLogsSection() {
  const { t } = useTranslation();
  const { propertyId } = useAuth();
  const [f, setF] = useState({ entity_type: "", action: "", actor_id: null as string | null, from: "", to: "" });
  const query = useMemo(() => ({ entity_type: f.entity_type || undefined, action: f.action || undefined, actor_id: f.actor_id ?? undefined, from: f.from ? f.from + "T00:00:00Z" : undefined, to: f.to ? f.to + "T23:59:59Z" : undefined }), [f]);
  const list = useList<AuditLog>("audit-logs", query);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [sel, setSel] = useState<AuditLog | null>(null);
  const columns = useMemo<ColumnDef<AuditLog, unknown>[]>(
    () => [
      { id: "at", header: "Waktu", cell: ({ row }) => <span className="tnum text-xs">{fmtDateTime(row.original.occurred_at)}</span>, size: 150 },
      { id: "msg", header: "Peristiwa", cell: ({ row }) => <span className="text-sm">{row.original.message}</span> },
      { id: "entity", header: "Entitas", cell: ({ row }) => <span className="text-xs text-muted-foreground">{row.original.entity_type}{row.original.entity_label ? ` · ${row.original.entity_label}` : ""}</span>, size: 200 },
      { id: "action", header: "Aksi", cell: ({ row }) => <span className="font-mono text-xs">{row.original.action}</span>, size: 140 },
      { id: "ip", header: "IP", cell: ({ row }) => <span className="font-mono text-xs text-muted-foreground">{row.original.ip ?? "—"}</span>, size: 120 },
    ],
    [],
  );
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <NativeSelect className="w-44" value={f.entity_type} onChange={(e) => setF({ ...f, entity_type: e.target.value })}><option value="">Entitas: {t("label.all")}</option>{ENTITIES.map((x) => <option key={x} value={x}>{x}</option>)}</NativeSelect>
        <Input className="w-40" placeholder="Aksi (mis. user.created)" value={f.action} onChange={(e) => setF({ ...f, action: e.target.value })} />
        <UserPicker propertyId={propertyId} value={f.actor_id} onChange={(id) => setF({ ...f, actor_id: id })} placeholder="Aktor" className="w-48" />
        <Input type="date" className="w-36" value={f.from} onChange={(e) => setF({ ...f, from: e.target.value })} aria-label="Dari" />
        <span className="text-muted-foreground">–</span>
        <Input type="date" className="w-36" value={f.to} onChange={(e) => setF({ ...f, to: e.target.value })} aria-label="Sampai" />
        {(f.entity_type || f.action || f.actor_id || f.from || f.to) && <Button variant="ghost" size="sm" onClick={() => setF({ entity_type: "", action: "", actor_id: null, from: "", to: "" })}>{t("action.reset_filter")}</Button>}
      </div>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => { setSel(r); }} loading={list.isLoading} isFiltered={!!(f.entity_type || f.action || f.actor_id || f.from || f.to)} empty={{ message: "Belum ada catatan audit." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      {sel && (
        <Dialog open onOpenChange={(o) => !o && setSel(null)}>
          <DialogContent title={sel.message} description={`${fmtDateTime(sel.occurred_at)} · ${sel.actor_name}${sel.request_id ? ` · req ${sel.request_id}` : ""}`}>
            <div className="grid grid-cols-2 gap-3 text-xs">
              <div><div className="mb-1 font-semibold uppercase text-muted-foreground">Sebelum</div><pre className="max-h-80 overflow-auto rounded-md bg-muted p-2">{JSON.stringify(sel.before, null, 2)}</pre></div>
              <div><div className="mb-1 font-semibold uppercase text-muted-foreground">Sesudah</div><pre className="max-h-80 overflow-auto rounded-md bg-muted p-2">{JSON.stringify(sel.after, null, 2)}</pre></div>
            </div>
            <DialogFooter><Button variant="secondary" onClick={() => setSel(null)}>Tutup</Button></DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
