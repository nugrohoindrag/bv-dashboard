// Billing › Reconciliation (PRD P4 v2.1 §6.4 P4-REC-01..03; D-P4-04 bank = impor mutasi, bukan API bank): daftar impor mutasi
// rekening per property, impor CSV/XLSX, dan detail impor (/billing/reconciliation/:id — deep link notifikasi & Attention
// "Mutasi bank belum dicocokkan") untuk mengonfirmasi saran, mencocokkan manual, atau mengabaikan mutasi.
import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { RelativeTime } from "@/components/bv/common";
import { CellTitle } from "@/components/bv/cells";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtMoney, fmtNumber } from "@/lib/format";
import { PropertySelect } from "@/features/finance/fin-ui";
import { fmtDay, usePropertyParam } from "@/features/finance/fin-utils";
import { ImportDetail } from "./reconciliation-detail";
import { ImportDialog } from "./reconciliation-import";
import { matchProgress, type StatementImport } from "./reconciliation-model";

export default function ReconciliationPage() {
  const { id } = useParams();
  return (
    <div>
      <PageHeader title="Reconciliation" subtitle="Impor mutasi rekening (CSV/Excel internet banking), cocokkan otomatis ke pembayaran menunggu verifikasi & invoice terbuka, lalu konfirmasi. Setiap langkah tercatat di audit." />
      {id ? <ImportDetail id={id} /> : <ImportList />}
    </div>
  );
}

function ImportList() {
  const { can } = useAuth();
  const [pid, setPid] = usePropertyParam();
  const [open, setOpen] = useState(false);
  const list = useList<StatementImport>("billing/bank-statements", { property_id: pid ?? undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const canManage = can("billing.reconciliation.manage", pid ?? undefined);
  const columns = useMemo<ColumnDef<StatementImport, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): berkas (format · rekening di atas nama berkas), periode, jumlah mutasi, progres
    // kecocokan satu baris (rincian di tooltip), total kredit, status, waktu impor. Pengimpor & total cocok ada di detail impor.
    { id: "file", header: "Berkas", meta: { mobile: "primary" }, cell: ({ row: { original: v } }) => <CellTitle code={[v.format.toUpperCase(), v.bank_account, pid ? null : v.property_name].filter(Boolean).join(" · ")} title={v.file_name ?? "Impor mutasi"} /> },
    { id: "period", header: "Periode", size: 190, meta: { mobile: "secondary" }, cell: ({ row: { original: v } }) => <span className="whitespace-nowrap text-sm tnum">{fmtDay(v.period_from)} – {fmtDay(v.period_to)}</span> },
    { id: "lines", header: "Mutasi", size: 90, meta: { mobile: "hidden" }, cell: ({ row: { original: v } }) => <span className="block whitespace-nowrap text-right text-sm tnum" title={`${fmtNumber(v.credit_count)} kredit`}>{fmtNumber(v.line_count)}</span> },
    {
      id: "progress", header: "Kecocokan", size: 170, meta: { mobile: "secondary" },
      cell: ({ row: { original: v } }) => {
        const pct = matchProgress(v);
        return (
          <div className="flex min-w-[120px] items-center gap-2" title={`${fmtNumber(v.matched_count)} cocok · ${fmtNumber(v.suggested_count)} saran · ${fmtNumber(v.unmatched_count)} belum`}>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-surface-container-high" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={`Kecocokan ${pct}%`}><div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} /></div>
            <span className="whitespace-nowrap text-xs tnum text-on-surface-variant">{pct}%</span>
          </div>
        );
      },
    },
    { id: "amount", header: "Total kredit", size: 150, meta: { mobile: "hidden" }, cell: ({ row: { original: v } }) => <span className="block whitespace-nowrap text-right text-sm tnum" title={`Cocok ${fmtMoney(v.matched_total)}`}>{fmtMoney(v.credit_total)}</span> },
    { id: "status", header: "Status", size: 110, meta: { mobile: "status" }, cell: ({ row: { original: v } }) => <Badge tone={v.status === "completed" ? "success" : "warning"}>{v.status === "completed" ? "Selesai" : "Terbuka"}</Badge> },
    { id: "imported_at", header: "Diimpor", size: 130, meta: { mobile: "hidden" }, cell: ({ row: { original: v } }) => <span className="whitespace-nowrap text-sm" title={v.imported_by_name ?? undefined}><RelativeTime value={v.imported_at} /></span> },
  ], [pid]);
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <PropertySelect value={pid} onChange={setPid} allowAll />
        {canManage && <Button icon="upload_file" className="ml-auto" onClick={() => setOpen(true)}>Impor mutasi</Button>}
      </div>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/billing/reconciliation/${r.id}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        empty={{ icon: "account_balance", title: "Belum ada impor mutasi", description: "Unduh mutasi rekening dari internet banking (CSV/Excel), lalu impor di sini untuk mencocokkan pembayaran tenant.", action: canManage ? <Button icon="upload_file" onClick={() => setOpen(true)}>Impor mutasi</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
      />
      {open && <ImportDialog propertyId={pid} onClose={() => setOpen(false)} />}
    </div>
  );
}
