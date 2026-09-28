// Property › Occupancy (PRD P1 v2 §8): informasi occupancy dasar — ringkasan unit (total · terisi · kosong · dipesan ·
// tidak aktif · occupancy %), rincian per building/tower dan lantai, serta daftar unit (status unit, status occupancy,
// referensi tenant/occupant). Semua angka dihitung server (GET /occupancy/summary, /occupancy/units).
import { useMemo } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { MetricCard } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Card, CardContent, CardHeader, CardTitle, Table, TBody, TD, TH, THead, TR } from "@/components/ui/primitives";
import { DataGrid, FilterBar, FilterSelect, useUrlFilters } from "@/components/bv/datagrid";
import { AsyncState } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellLocation, CellTitle } from "@/components/bv/cells";
import { useAll } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtNumber } from "@/lib/format";
import { statusOptions } from "@/lib/status";
import type { OccupancyGroup, OccupancySummary, OccupancyUnit } from "@/api/types";

export const UNIT_TYPES: Record<string, string> = { commercial: "Commercial", residential: "Residential", hotel_room: "Hotel Room" };
/** Path induk unit ("Gedung / Lantai 12 / 1201" → "Gedung / Lantai 12"). */
const parentPathText = (path: string | null | undefined): string | null => {
  const parts = (path ?? "").split(/\s*[/›>]\s*/).map((x) => x.trim()).filter(Boolean);
  return parts.length > 1 ? parts.slice(0, -1).join(" / ") : null;
};
const pct = (n: number) => `${new Intl.NumberFormat("id-ID", { maximumFractionDigits: 1 }).format(n)}%`;

export default function OccupancyPage() {
  const { t } = useTranslation();
  const { propertyId } = useAuth();
  const f = useUrlFilters();
  const query = { property_id: propertyId ?? undefined, location_id: f.get("location_id") || undefined, status: f.get("status") || undefined, unit_type: f.get("unit_type") || undefined, q: f.get("q") || undefined };
  const summary = useQuery({ queryKey: ["occupancy-summary", query], queryFn: ({ signal }) => api<OccupancySummary>("occupancy/summary", { query, signal }), staleTime: 30_000 });
  const units = useAll<OccupancyUnit>("occupancy/units", query);
  const columns = useMemo<ColumnDef<OccupancyUnit, unknown>[]>(() => [
    // Pola tabel Tasks (29 Sep 2026): unit (kode + nomor), lokasi = induk terdekat (path di tooltip), satu status occupancy.
    // Status unit (aktif/nonaktif) & daftar occupant ada di detail lokasi (klik baris).
    { id: "unit", header: "Unit", meta: { mobile: "primary" }, cell: ({ row }) => <Link to={`/property/locations/${row.original.location_id}`} onClick={(e) => e.stopPropagation()} className="block hover:underline"><CellTitle code={row.original.unit_number && row.original.unit_number !== row.original.code ? row.original.code : undefined} title={row.original.unit_number || row.original.code} /></Link>, size: 140 },
    { id: "path", header: t("label.location"), meta: { mobile: "secondary" }, cell: ({ row }) => <CellLocation path={parentPathText(row.original.path_text)} max={160} /> },
    { id: "occupancy_status", header: "Occupancy", meta: { mobile: "status" }, cell: ({ row }) => <StatusBadge objectType="occupancy" status={row.original.occupancy_status} />, size: 130 },
    { id: "tenant", header: t("label.tenant"), meta: { mobile: "secondary" }, cell: ({ row }) => (row.original.tenant ? <Link to={`/tenant/tenants/${row.original.tenant.id}`} onClick={(e) => e.stopPropagation()} className="block max-w-[200px] truncate text-brand-600 hover:underline" title={row.original.tenant.name}>{row.original.tenant.name}</Link> : <span className="text-muted-foreground">—</span>) },
    { id: "unit_type", header: "Tipe", meta: { mobile: "hidden", nowrap: true }, cell: ({ row }) => UNIT_TYPES[row.original.unit_type] ?? (row.original.unit_type || "—"), size: 130 },
    { id: "area", header: "Luas", meta: { mobile: "hidden", nowrap: true }, cell: ({ row }) => <span className="tnum">{row.original.area_m2 != null ? `${fmtNumber(row.original.area_m2)} m²` : "—"}</span>, size: 90 },
  ], [t]);
  return (
    <div>
      <PageHeader title={t("nav.occupancy")} subtitle="Status hunian unit: Vacant · Occupied · Reserved · Inactive (PRD P1 v2 §8).">
        <FilterBar
          spec={{
            status: statusOptions("occupancy"),
            location: true,
            extra: <FilterSelect param="unit_type" label="Tipe unit" options={Object.entries(UNIT_TYPES).map(([value, label]) => ({ value, label }))} className="sm:w-44" />,
          }}
        />
      </PageHeader>
      <div className="space-y-5">
        <AsyncState query={summary}>
          {(s) => (
            <>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 xl:grid-cols-6">
                <MetricCard label="Total unit" value={fmtNumber(s.total)} tone="primary" />
                <MetricCard label="Occupied" value={fmtNumber(s.occupied)} tone="success" />
                <MetricCard label="Vacant" value={fmtNumber(s.vacant)} tone="info" />
                <MetricCard label="Reserved" value={fmtNumber(s.reserved)} tone="warning" />
                <MetricCard label="Inactive" value={fmtNumber(s.inactive)} tone="neutral" />
                <MetricCard label="Occupancy" value={pct(s.occupancy_pct)} subValue="occupied / (total − inactive)" tone={s.occupancy_pct >= 80 ? "success" : s.occupancy_pct >= 50 ? "warning" : "error"} />
              </div>
              <div className="grid grid-cols-1 gap-5 2xl:grid-cols-2">
                <GroupTable title="Per building / tower" rows={s.buildings} onPick={(id) => f.set({ location_id: id })} />
                <GroupTable title="Per lantai" rows={s.floors} onPick={(id) => f.set({ location_id: id })} />
              </div>
            </>
          )}
        </AsyncState>
        <Card>
          <CardHeader><CardTitle>Unit</CardTitle></CardHeader>
          <CardContent className="px-0">
            <DataGrid columns={columns} rows={units.data ?? []} rowId={(r) => r.location_id} onRowClick={(r) => `/property/locations/${r.location_id}`} loading={units.isLoading} error={units.error} onRetry={() => units.refetch()} isFiltered={f.isFiltered} empty={{ icon: "door_front", title: "Belum ada unit.", description: "Tambah unit di Property › Units." }} />
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function GroupTable({ title, rows, onPick }: { title: string; rows: OccupancyGroup[]; onPick: (locationId: string) => void }) {
  return (
    <Card>
      <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent className="px-0">
        {rows.length === 0 ? <p className="px-4 py-6 text-center text-sm text-muted-foreground">Tidak ada data.</p> : (
          <Table>
            <THead><tr><TH>Lokasi</TH><TH className="text-right">Total</TH><TH className="text-right">Occupied</TH><TH className="text-right">Vacant</TH><TH className="text-right">Reserved</TH><TH className="text-right">Inactive</TH><TH className="text-right">%</TH></tr></THead>
            <TBody>
              {rows.map((r) => (
                <TR key={r.location_id} className="cursor-pointer" onClick={() => onPick(r.location_id)} title="Filter unit di lokasi ini">
                  <TD><span className="block max-w-[220px] truncate" title={r.name}><span className="mr-1 text-[10px] uppercase text-muted-foreground">{r.location_type}</span>{r.name}</span></TD>
                  <TD className="text-right tnum">{r.total}</TD>
                  <TD className="text-right tnum">{r.occupied}</TD>
                  <TD className="text-right tnum">{r.vacant}</TD>
                  <TD className="text-right tnum">{r.reserved}</TD>
                  <TD className="text-right tnum">{r.inactive}</TD>
                  <TD className="text-right tnum font-semibold">{pct(r.occupancy_pct)}</TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
