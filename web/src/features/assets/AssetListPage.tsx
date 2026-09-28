// Asset Register (PRD §13; PRD P2 v2.1 P2-EQH-04): daftar aset per property/lokasi/kategori + health (status map asset_health,
// skor) & filter health/at-risk (drill-down Engineering Dashboard: ?health_status=, ?at_risk=true, ?category=); penanda
// dokumen/warranty kedaluwarsa; QR; buat/edit aset; export.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, FilterSelect, useUrlFilters } from "@/components/bv/datagrid";
import { AssetStatusBadge, PriorityBadge } from "@/components/bv/badges";
import { CellLocation, CellTitle } from "@/components/bv/cells";
import { useAll, useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDate } from "@/lib/format";
import { statusOptions } from "@/lib/status";
import type { Asset, Equipment } from "@/api/types";
import { useExport } from "@/features/operations/dialogs";
import { AssetDialog } from "./AssetDialog";
import { ExpiringDocsBadge, HealthBadge } from "./AssetP2Tabs";

export default function AssetListPage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  // `category` (drill-down dashboard) dan `type` (alias lama) sama-sama kode kategori equipment
  const query = useMemo(() => { const q: Record<string, string | undefined> = { ...f.all, property_id: propertyId ?? f.all.property_id ?? undefined }; if (q.type) { q.category = q.type; delete q.type; } delete q.cursor; return q; }, [f.all, propertyId]);
  const list = useList<Asset>("assets", query);
  const equipment = useAll<Equipment>("equipment");
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [createOpen, setCreateOpen] = useState(false);
  const exp = useExport();
  const categories = useMemo(() => Array.from(new Map((equipment.data ?? []).map((e) => [e.category_code, e.category_name])).entries()).map(([value, label]) => ({ value, label })), [equipment.data]);
  const columns = useMemo<ColumnDef<Asset, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026, pola halaman Tasks): kode + nama satu baris, lokasi = nama lokasi terakhir, satu
      // status + satu penanda (dokumen kedaluwarsa). Kategori/tipe, WO open & maintenance terakhir ada di Asset 360.
      { id: "asset", header: "Aset", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.asset_code} title={row.original.name} /> },
      { id: "location", header: t("label.location"), meta: { mobile: "secondary" }, cell: ({ row }) => <CellLocation path={row.original.location_path} max={150} /> },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <span className="inline-flex items-center gap-1"><AssetStatusBadge status={row.original.status} /><ExpiringDocsBadge count={row.original.expiring_documents} /></span>, size: 130 },
      { id: "health", header: "Health", meta: { nowrap: true, mobile: "secondary" }, cell: ({ row }) => <HealthBadge status={row.original.health_status} score={row.original.health_score} />, size: 140 },
      { id: "crit", header: "Kritikalitas", meta: { nowrap: true }, cell: ({ row }) => row.original.criticality ? <PriorityBadge priority={row.original.criticality} /> : "—", size: 110 },
      { id: "next_pm", header: "Preventive Maintenance berikutnya", meta: { nowrap: true }, cell: ({ row }) => <span className="tnum text-sm">{fmtDate(row.original.next_pm_due)}</span>, size: 150 },
    ],
    [t],
  );
  return (
    <div>
      <PageHeader title={t("nav.assets")} actions={can("engineering.assets.create") && <Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> Tambah Aset</Button>}>
        <FilterBar
          spec={{
            status: [{ value: "active", label: "Active" }, { value: "inactive", label: "Inactive" }, { value: "under_maintenance", label: "Under Maintenance" }, { value: "decommissioned", label: "Decommissioned" }],
            location: true,
            extra: (
              <>
                <FilterSelect param="category" label="Kategori" options={categories} />
                <FilterSelect param="health_status" label="Health" options={statusOptions("asset_health")} />
              </>
            ),
            presets: [
              { key: "critical", label: "Kritikal", params: { criticality: "critical" } },
              { key: "um", label: "Under maintenance", params: { status: "under_maintenance" } },
              // PRD P2 v2.1 P2-EQH-04: at-risk = health warning + critical
              { key: "at_risk", label: "At-risk (health)", params: { at_risk: "true" } },
            ],
          }}
          onExport={can("platform.exports.create") ? () => exp.request("assets", Object.fromEntries(Object.entries(query).filter(([, v]) => v) as [string, string][])) : undefined}
        />
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/assets/${r.id}`} loading={list.isLoading} error={list.error} onRetry={() => list.refetch()} isFiltered={f.isFiltered} empty={{ message: t("empty.assets") }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      {createOpen && <AssetDialog asset={null} onClose={() => setCreateOpen(false)} />}
    </div>
  );
}
