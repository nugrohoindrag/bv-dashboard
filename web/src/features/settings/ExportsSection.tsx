// Riwayat Export (PRD P0 §18): ekspor milik saya 30 hari terakhir; baris pending/processing di-polling tiap 3 detik,
// tautan unduh saat siap. Ekspor baru bisa dibuat dari sini atau dari tombol Export di daftar modul.
import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Button, NativeSelect } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { StatusBadge } from "@/components/bv/badges";
import { CellTitle } from "@/components/bv/cells";
import { RelativeTime } from "@/components/bv/common";
import { api } from "@/lib/api";
import { fmtDateTime } from "@/lib/format";
import { EXPORT_RESOURCES, useExport } from "@/lib/use-export";
import type { ExportJob } from "@/api/types";

export default function ExportsSection() {
  const { t } = useTranslation();
  const exp = useExport();
  const [resource, setResource] = useState<string>("work_orders");
  const list = useQuery({
    queryKey: ["exports"],
    queryFn: () => api<{ data: ExportJob[]; resources?: string[] }>("exports"),
    refetchInterval: (q) => ((q.state.data?.data ?? []).some((x) => x.status === "pending" || x.status === "processing") ? 3000 : false),
  });
  const resources = list.data?.resources?.length ? list.data.resources : [...EXPORT_RESOURCES];
  const columns = useMemo<ColumnDef<ExportJob, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026): format (kode kecil) di atas nama data; satu baris per kolom.
      { id: "resource", header: t("export.data"), meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.format?.toUpperCase()} title={t(`export.resource.${row.original.resource}`, { defaultValue: row.original.resource })} /> },
      { id: "created_at", header: t("label.created"), meta: { mobile: "secondary" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm"><RelativeTime value={row.original.created_at} /></span>, size: 130 },
      { id: "rows", header: t("export.rows"), meta: { mobile: "secondary" }, cell: ({ row }) => <span className="tnum whitespace-nowrap">{row.original.row_count ?? "—"}</span>, size: 90 },
      { id: "expires", header: t("export.expires"), meta: { mobile: "hidden" }, cell: ({ row }) => <span className="tnum whitespace-nowrap text-sm">{fmtDateTime(row.original.expires_at)}</span>, size: 150 },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <span title={row.original.error ?? undefined}><StatusBadge objectType="export" status={row.original.status} /></span>, size: 120 },
      {
        id: "actions", header: "", cell: ({ row }) => row.original.status === "ready" ? <DownloadButton id={row.original.id} url={row.original.download_url} /> : null,
      },
    ],
    [t],
  );
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <NativeSelect className="w-full sm:w-56" value={resource} onChange={(e) => setResource(e.target.value)} aria-label={t("export.data")}>
          {resources.map((r) => <option key={r} value={r}>{t(`export.resource.${r}`, { defaultValue: r })}</option>)}
        </NativeSelect>
        <Button icon="download" loading={exp.busy} onClick={() => exp.request(resource, {})}>{t("export.new")}</Button>
        <span className="text-sm text-on-surface-variant">{t("export.retention")}</span>
      </div>
      <DataGrid
        columns={columns}
        rows={list.data?.data ?? []}
        rowId={(r) => r.id}
        isLoading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        empty={{ icon: "download", title: t("export.empty"), description: t("export.empty_desc") }}
      />
    </div>
  );
}

/** Tautan unduh berumur pendek: ambil ulang dari GET /exports/{id} saat diklik. */
function DownloadButton({ id, url }: { id: string; url: string | null }) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  return (
    <Button size="sm" variant="secondary" icon="download" loading={busy} onClick={async () => {
      setBusy(true);
      try {
        const x = await api<ExportJob>(`exports/${id}`);
        window.open(x.download_url ?? url ?? "", "_blank");
      } finally {
        setBusy(false);
      }
    }}>{t("export.download")}</Button>
  );
}
