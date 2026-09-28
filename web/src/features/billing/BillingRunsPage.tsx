// Billing › Billing Runs (PRD P4 v2.1 P4-BRL-02..04; NC §34 "Service Charges"): tagihan periode untuk semua unit dalam cakupan
// billing rule — pratinjau (baris, total, pengecualian) → draft invoice → terbit massal. Run terjadwal dibuat job generate
// otomatis (P4-BRL-03). Detail = /billing/runs/:id (deep link notifikasi billing_run.generated).
import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { RelativeTime } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusOptions } from "@/lib/status";
import { RunDetail } from "./RunDetail";
import { RunWizard } from "./RunWizard";
import { money, type BillingRun } from "./types";

export default function BillingRunsPage() {
  const { id } = useParams();
  if (id) return <RunDetail key={id} id={id} />;
  return <RunList />;
}

function RunList() {
  const { t } = useTranslation();
  const { propertyId, properties, can } = useAuth();
  const f = useUrlFilters();
  const [wizard, setWizard] = useState(false);
  const list = useList<BillingRun>("billing/runs", { property_id: propertyId ?? undefined, status: f.get("status") || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const canGenerate = can("billing.runs.generate");
  const multiProperty = !propertyId && properties.length > 1;

  const columns = useMemo<ColumnDef<BillingRun, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): nomor run + periode satu kolom, satu badge status + satu flag
    // (pengecualian). Sumber terjadwal & pembuat ada di halaman detail run.
    { id: "period", header: "Run", meta: { mobile: "primary" }, cell: ({ row: { original: r } }) => <CellTitle code={r.run_number} title={r.period_label} /> },
    ...(multiProperty ? [{ id: "property", header: "Property", meta: { mobile: "hidden" as const }, cell: ({ row: { original: r } }: { row: { original: BillingRun } }) => <CellText max={160}>{r.property_name}</CellText> }] : []),
    { id: "lines", header: "Baris", size: 100, meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <span className="tnum whitespace-nowrap text-sm">{r.line_count} baris</span> },
    { id: "total", header: "Total", size: 150, meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <div className="tnum whitespace-nowrap text-right font-semibold">{money(r.total_amount)}</div> },
    { id: "invoices", header: "Invoice", size: 90, meta: { mobile: "hidden" }, cell: ({ row: { original: r } }) => <span className="tnum whitespace-nowrap">{r.invoice_count || "—"}</span> },
    { id: "status", header: t("label.status"), size: 190, meta: { mobile: "status" }, cell: ({ row: { original: r } }) => <div className="flex flex-wrap items-center gap-1"><StatusBadge objectType="billing_run" status={r.status} />{r.exception_count > 0 && <Badge tone="warning">{r.exception_count} pengecualian</Badge>}</div> },
    { id: "created", header: "Dibuat", size: 120, meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <span className="whitespace-nowrap text-sm text-on-surface-variant" title={r.created_by_name ?? "sistem"}><RelativeTime value={r.created_at} /></span> },
  ], [t, multiProperty]);

  return (
    <div>
      <PageHeader
        title={t("nav.billing_runs")}
        subtitle="Tagihan bulanan (service charge / IPL, sinking fund, utilitas, parkir) dari billing rule: pratinjau dengan pengecualian → draft invoice → terbitkan massal."
        actions={canGenerate && <Button icon="event_repeat" onClick={() => setWizard(true)}>Buat Tagihan Bulanan</Button>}
      >
        <FilterBar spec={{ status: statusOptions("billing_run") }} />
      </PageHeader>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/billing/runs/${r.id}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={f.isFiltered}
        empty={{ icon: "event_repeat", title: "Belum ada billing run", description: "Buat tagihan bulanan dari billing rule aktif. Run terjadwal muncul otomatis untuk rule dengan generate otomatis.", action: canGenerate ? <Button icon="event_repeat" onClick={() => setWizard(true)}>Buat Tagihan Bulanan</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
      />
      {wizard && <RunWizard onClose={() => setWizard(false)} />}
    </div>
  );
}
