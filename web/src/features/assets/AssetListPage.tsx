// Asset Register (PRD §13): daftar aset per property/lokasi/kategori; QR; buat/edit aset; export.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { AssetStatusBadge, PriorityBadge } from "@/components/bv/badges";
import { useAll, useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDate } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Asset, Equipment } from "@/api/types";
import { useExport } from "@/features/operations/dialogs";
import { AssetDialog } from "./AssetDialog";

export default function AssetListPage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  const query = useMemo(() => { const q: Record<string, string | undefined> = { ...f.all, property_id: propertyId ?? undefined }; if (q.type) { q.category = q.type; delete q.type; } delete q.cursor; return q; }, [f.all, propertyId]);
  const list = useList<Asset>("assets", query);
  const equipment = useAll<Equipment>("equipment");
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [createOpen, setCreateOpen] = useState(false);
  const exp = useExport();
  const categories = useMemo(() => Array.from(new Map((equipment.data ?? []).map((e) => [e.category_code, e.category_name])).entries()).map(([value, label]) => ({ value, label })), [equipment.data]);
  const columns = useMemo<ColumnDef<Asset, unknown>[]>(
    () => [
      { id: "code", header: "Kode", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.asset_code}</span>, size: 140 },
      { id: "name", header: "Nama", cell: ({ row }) => <div><div className="font-medium">{row.original.name}</div><div className="text-xs text-muted-foreground">{row.original.category_name}{row.original.type_name ? ` · ${row.original.type_name}` : ""}</div></div> },
      { id: "location", header: t("label.location"), cell: ({ row }) => <span className="text-sm text-muted-foreground">{row.original.location_path}</span> },
      { id: "status", header: t("label.status"), cell: ({ row }) => <AssetStatusBadge status={row.original.status} />, size: 130 },
      { id: "crit", header: "Kritikalitas", cell: ({ row }) => row.original.criticality ? <PriorityBadge priority={row.original.criticality} /> : "—", size: 110 },
      { id: "wo", header: "WO open", cell: ({ row }) => <span className={cn("tnum", row.original.open_work_orders > 0 && "font-semibold")}>{row.original.open_work_orders}</span>, size: 80 },
      { id: "pm", header: "PM berikutnya", cell: ({ row }) => fmtDate(row.original.next_pm_due), size: 120 },
      { id: "last", header: "Maintenance terakhir", cell: ({ row }) => fmtDate(row.original.last_maintenance_at), size: 140 },
    ],
    [t],
  );
  return (
    <div>
      <PageHeader title={t("nav.assets")} actions={can("engineering.assets.create") && <Button onClick={() => setCreateOpen(true)}><Plus /> Tambah Aset</Button>}>
        <FilterBar
          spec={{
            status: [{ value: "active", label: "Active" }, { value: "inactive", label: "Inactive" }, { value: "under_maintenance", label: "Under Maintenance" }, { value: "decommissioned", label: "Decommissioned" }],
            type: categories,
            location: true,
            presets: [{ key: "critical", label: "Kritikal", params: { criticality: "critical" } }, { key: "um", label: "Under maintenance", params: { status: "under_maintenance" } }],
          }}
          onExport={can("platform.exports.create") ? () => exp.request("assets", Object.fromEntries(Object.entries(query).filter(([, v]) => v) as [string, string][])) : undefined}
        />
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/assets/${r.id}`} loading={list.isLoading} isFiltered={f.isFiltered} empty={{ message: t("empty.assets") }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      {createOpen && <AssetDialog asset={null} onClose={() => setCreateOpen(false)} />}
    </div>
  );
}
