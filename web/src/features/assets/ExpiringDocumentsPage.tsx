// Dokumen Equipment & Warranty kedaluwarsa (PRD P2 v2.1 §5.6 P2-DOC-02; NC §76.4 Equipment Document): dokumen aktif dan
// warranty asset yang sudah/segera kedaluwarsa ≤ N hari (GET /asset-documents/expiring). Baris → Asset 360 tab Dokumen.
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, NativeSelect } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { CellText, CellTitle } from "@/components/bv/cells";
import { api, type ListResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDate } from "@/lib/format";
import { statusOptions } from "@/lib/status";
import type { ExpiringDocument } from "@/api/types";
import { ValidityBadge } from "@/features/workforce/components";
import { documentTypeLabel, internalLink } from "./asset-p2";

export default function ExpiringDocumentsPage() {
  const { propertyId } = useAuth();
  const [days, setDays] = useState("30");
  const [status, setStatus] = useState("");
  const q = useQuery({
    queryKey: ["expiring", "asset-documents", propertyId ?? null, days],
    queryFn: ({ signal }) => api<ListResponse<ExpiringDocument>>("asset-documents/expiring", { query: { property_id: propertyId ?? undefined, days }, signal }).then((r) => r.data),
  });
  const rows = useMemo(() => (q.data ?? []).filter((r) => !status || r.status === status), [q.data, status]);
  const counts = useMemo(() => ({ expired: (q.data ?? []).filter((r) => r.status === "expired").length, expiring: (q.data ?? []).filter((r) => r.status === "expiring").length }), [q.data]);
  const columns = useMemo<ColumnDef<ExpiringDocument, unknown>[]>(
    () => [
      // Pola tabel Tasks (29 Sep 2026): judul satu baris, tipe di kolom sendiri (tanpa sub-baris), aset = kode + nama.
      { id: "title", header: "Dokumen", meta: { mobile: "primary" }, cell: ({ row: { original: r } }) => <CellTitle title={r.title} /> },
      { id: "type", header: "Tipe", meta: { mobile: "hidden" }, size: 150, cell: ({ row: { original: r } }) => <CellText max={150} muted>{r.kind === "warranty" ? "Warranty asset" : documentTypeLabel(r.document_type)}</CellText> },
      { id: "asset", header: "Aset", meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <Link to={`/assets/${r.asset_id}?tab=documents`} className="block max-w-[220px] hover:underline" onClick={(e) => e.stopPropagation()}><CellTitle code={r.asset_code} title={r.asset_name} /></Link> },
      { id: "expires", header: "Berlaku s/d", meta: { mobile: "secondary", nowrap: true }, size: 130, cell: ({ row: { original: r } }) => <span className="tnum">{fmtDate(r.expires_on)}</span> },
      { id: "status", header: "Status", meta: { mobile: "status" }, size: 260, cell: ({ row: { original: r } }) => <ValidityBadge status={r.status} days={r.days_to_expire} /> },
    ],
    [],
  );
  return (
    <div>
      <PageHeader title="Dokumen & Warranty Kedaluwarsa" subtitle="Dokumen equipment aktif dan warranty asset yang sudah atau segera kedaluwarsa. Pengingat otomatis H-30, H-7, dan saat kedaluwarsa ke Engineering Supervisor.">
        <div className="flex flex-wrap items-center gap-2">
          <NativeSelect className="w-[calc(50%-4px)] sm:w-56" value={days} onChange={(e) => setDays(e.target.value)} aria-label="Rentang">
            {["30", "60", "90", "180", "365"].map((d) => <option key={d} value={d}>Kedaluwarsa ≤ {d} hari</option>)}
          </NativeSelect>
          <NativeSelect className="w-[calc(50%-4px)] sm:w-48" value={status} onChange={(e) => setStatus(e.target.value)} aria-label="Status">
            <option value="">Status: Semua</option>
            {statusOptions("validity").filter((o) => o.value === "expired" || o.value === "expiring" || o.value === "valid").map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
          </NativeSelect>
          {q.data && <span className="flex gap-1.5">{counts.expired > 0 && <Badge tone="error">{counts.expired} kedaluwarsa</Badge>}{counts.expiring > 0 && <Badge tone="warning">{counts.expiring} ≤ 30 hari</Badge>}</span>}
        </div>
      </PageHeader>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => `${r.kind}:${r.id}`}
        onRowClick={(r) => internalLink(r.deep_link) ?? `/assets/${r.asset_id}?tab=documents`}
        loading={q.isLoading}
        error={q.error}
        onRetry={() => q.refetch()}
        isFiltered={!!status}
        empty={{ icon: "verified", title: "Tidak ada dokumen yang akan kedaluwarsa", description: `Semua dokumen equipment & warranty berlaku lebih dari ${days} hari (atau tanpa masa berlaku).` }}
      />
    </div>
  );
}
