// Findings (PRD §15): daftar temuan dari patrol/inspeksi/checklist; aksi Buat Work Order dari Finding; resolve/close.
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { SeverityBadge, StatusBadge, objectTypeLabel } from "@/components/bv/badges";
import { LocationPath, RelativeTime } from "@/components/bv/common";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusMap } from "@/lib/status-map";
import type { Finding } from "@/api/types";
import { CreateWorkItemDialog, TransitionActions, useExport } from "./dialogs";
import { CreateFindingDialog, FINDING_TYPES } from "./FindingDialogs";

export default function FindingListPage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const nav = useNavigate();
  const f = useUrlFilters();
  const query = useMemo(() => { const q: Record<string, string | undefined> = { ...f.all, property_id: propertyId ?? undefined }; delete q.cursor; return q; }, [f.all, propertyId]);
  const list = useList<Finding>("findings", query);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [createOpen, setCreateOpen] = useState(false);
  const [wo, setWo] = useState<Finding | null>(null);
  const exp = useExport();
  const columns = useMemo<ColumnDef<Finding, unknown>[]>(
    () => [
      { id: "number", header: "ID", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.finding_number}</span>, size: 150 },
      { id: "title", header: t("label.title"), cell: ({ row }) => <div className="min-w-0"><div className="truncate font-medium">{row.original.title}</div><div className="text-xs text-muted-foreground">{row.original.finding_type}{row.original.category ? ` · ${row.original.category}` : ""} · dari {row.original.source_label || (row.original.source_type ? objectTypeLabel[row.original.source_type] : "manual")}</div></div> },
      { id: "location", header: t("label.location"), cell: ({ row }) => <div><LocationPath pathText={row.original.location.path_text} className="max-w-[240px]" />{row.original.asset.asset_code && <div className="font-mono text-xs text-muted-foreground">{row.original.asset.asset_code}</div>}</div> },
      { id: "severity", header: t("label.severity"), cell: ({ row }) => <SeverityBadge severity={row.original.severity} />, size: 120 },
      { id: "status", header: t("label.status"), cell: ({ row }) => <StatusBadge objectType="finding" status={row.original.status} />, size: 120 },
      { id: "reported", header: "Dilaporkan", cell: ({ row }) => <div><RelativeTime value={row.original.reported_at} /><div className="text-xs text-muted-foreground">{row.original.reported_by_name}</div></div>, size: 130 },
      { id: "wo", header: "Work Order", cell: ({ row }) => { const l = row.original.links.find((x) => x.object_type === "work_order"); return l ? <span className="font-mono text-xs">{l.label}</span> : <span className="text-xs text-muted-foreground">—</span>; }, size: 130 },
      { id: "actions", header: "", cell: ({ row }) => <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}>{can("operations.work_orders.create") && row.original.status === "open" && <Button size="sm" variant="secondary" onClick={() => setWo(row.original)}>Buat WO</Button>}<TransitionActions objectType="finding" item={row.original} compact /></div> },
    ],
    [t, can],
  );
  return (
    <div>
      <PageHeader title="Findings" actions={can("operations.findings.create") && <Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> Catat Finding</Button>}>
        <FilterBar
          spec={{
            status: Object.entries(statusMap.finding).map(([value, d]) => ({ value, label: d.label_id })),
            severity: true,
            location: true,
            type: FINDING_TYPES.map((x) => ({ value: x, label: x })),
            presets: [
              { key: "unresolved", label: "Belum selesai", params: { unresolved: "true" } },
              { key: "critical", label: "Kritis", params: { severity: "critical" } },
            ],
          }}
          onExport={can("platform.exports.create") ? () => exp.request("findings", Object.fromEntries(Object.entries(query).filter(([, v]) => v) as [string, string][])) : undefined}
        />
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/findings/${r.id}`} loading={list.isLoading} isFiltered={f.isFiltered} empty={{ message: t("empty.findings") }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      <CreateFindingDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={(x) => nav(`/findings/${x.id}`)} />
      {wo && <CreateWorkItemDialog objectType="work_order" open onOpenChange={(o) => !o && setWo(null)} defaults={{ title: wo.title, location_id: wo.location.id, asset_id: wo.asset.id, priority: wo.severity, type: "corrective", source_type: "finding", source_id: wo.id, link_to: { object_type: "finding", object_id: wo.id, link_type: "generated_from" } }} onCreated={(x) => nav(`/operations/work-orders/${x.id}`)} />}
    </div>
  );
}
