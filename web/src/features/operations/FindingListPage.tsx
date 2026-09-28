// Findings (PRD §15): daftar temuan dari patrol/inspeksi/checklist; aksi Buat Work Order dari Finding; resolve/close.
// PRD P2 v2.1 P2-PAT-08: filter & kolom checkpoint asal temuan patroli (?checkpoint_id=).
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, FilterSelect, useUrlFilters } from "@/components/bv/datagrid";
import { SeverityBadge } from "@/components/bv/badges";
import { CellLocation, CellStatus, CellTitle } from "@/components/bv/cells";
import { RelativeTime } from "@/components/bv/common";
import { useAll, useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusMap } from "@/lib/status-map";
import type { Checkpoint, Finding } from "@/api/types";
import { findingQuery } from "@/lib/drilldown";
import { CreateWorkItemDialog, TransitionActions, useExport } from "./dialogs";
import { CreateFindingDialog, FINDING_TYPES } from "./FindingDialogs";
import { CASE_SORTS, safeSort } from "@/lib/sort";

export default function FindingListPage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const nav = useNavigate();
  const f = useUrlFilters();
  const drill = useMemo(() => findingQuery(f.all), [f.all]);
  const query = useMemo(() => { const q: Record<string, string | undefined> = { ...drill.query, property_id: propertyId ?? drill.query.property_id ?? undefined, sort: safeSort(f.all.sort, CASE_SORTS) }; delete q.cursor; return q; }, [drill.query, f.all.sort, propertyId]);
  const checkpoints = useAll<Checkpoint>("checkpoints", { property_id: propertyId ?? undefined }, { enabled: can("security.checkpoints.view") });
  const list = useList<Finding>("findings", query);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [createOpen, setCreateOpen] = useState(false);
  const [wo, setWo] = useState<Finding | null>(null);
  const exp = useExport();
  const columns = useMemo<ColumnDef<Finding, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026, pola Tasks): nomor + judul, lokasi terakhir, severity, status, waktu lapor.
      // Tipe/kategori, sumber & checkpoint, kode aset, pelapor dan Work Order terkait ada di halaman detail.
      { id: "title", header: "Finding", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.finding_number} title={row.original.title} /> },
      { id: "location", header: t("label.location"), meta: { mobile: "secondary" }, cell: ({ row }) => <CellLocation path={row.original.location.path_text} max={150} /> },
      { id: "severity", header: t("label.severity"), meta: { mobile: "secondary" }, cell: ({ row }) => <SeverityBadge severity={row.original.severity} />, size: 110 },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <CellStatus objectType="finding" status={row.original.status} />, size: 120 },
      { id: "reported", header: "Dilaporkan", meta: { mobile: "secondary" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm"><RelativeTime value={row.original.reported_at} /></span>, size: 120 },
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
            extra: can("security.checkpoints.view") ? <FilterSelect param="checkpoint_id" label="Checkpoint" options={(checkpoints.data ?? []).map((c) => ({ value: c.id, label: c.name }))} className="sm:w-48" /> : undefined,
            presets: [
              { key: "unresolved", label: "Belum selesai", params: { unresolved: "true" } },
              { key: "critical", label: "Kritis", params: { severity: "critical" } },
            ],
          }}
          onExport={can("platform.exports.create") ? () => exp.request("findings", Object.fromEntries(Object.entries(query).filter(([, v]) => v) as [string, string][])) : undefined}
        />
      </PageHeader>
      {drill.notes.map((n) => <Alert key={n} variant="info" className="mb-3">{n}</Alert>)}
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/findings/${r.id}`} loading={list.isLoading} error={list.error} onRetry={() => list.refetch()} isFiltered={f.isFiltered} empty={{ message: t("empty.findings") }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      <CreateFindingDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={(x) => nav(`/findings/${x.id}`)} />
      {wo && <CreateWorkItemDialog objectType="work_order" open onOpenChange={(o) => !o && setWo(null)} defaults={{ title: wo.title, location_id: wo.location.id, asset_id: wo.asset.id, priority: wo.severity, type: "corrective", source_type: "finding", source_id: wo.id, link_to: { object_type: "finding", object_id: wo.id, link_type: "generated_from" } }} onCreated={(x) => nav(`/operations/work-orders/${x.id}`)} />}
    </div>
  );
}
