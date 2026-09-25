// Tenant Relation › Feedback (PRD P1 v1.3 §18 CSAT): daftar rating 1–5 + komentar per ticket.
import { useMemo } from "react";
import { Link } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { DataGrid } from "@/components/bv/datagrid";
import { RelativeTime } from "@/components/bv/common";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";

interface FeedbackRow { service_request_id: string; request_number: string; title: string; category_code: string; category_name: string | null; rating: number; comment: string | null; tenant_name: string | null; created_at: string }

function Stars({ n }: { n: number }) {
  return <span className="inline-flex items-center" aria-label={`${n} dari 5`}>{[1, 2, 3, 4, 5].map((i) => <Icon key={i} name={i <= n ? "star" : "star_border"} size={16} color={i <= n ? "var(--color-warning)" : "var(--color-outline)"} />)}</span>;
}

export default function FeedbackPage() {
  const { propertyId } = useAuth();
  const list = useList<FeedbackRow>("tenant-relation/feedback", { property_id: propertyId ?? undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const avg = rows.length ? rows.reduce((a, r) => a + r.rating, 0) / rows.length : null;
  const columns = useMemo<ColumnDef<FeedbackRow, unknown>[]>(() => [
    { id: "rating", header: "Rating", cell: ({ row }) => <Stars n={row.original.rating} />, size: 130 },
    { id: "number", header: "Ticket", cell: ({ row }) => <Link to={`/tenant-relation/service-requests/${row.original.service_request_id}`} className="font-mono text-[13px] font-semibold text-primary hover:underline">{row.original.request_number}</Link>, size: 150 },
    { id: "title", header: "Judul", cell: ({ row }) => <div><div className="font-medium">{row.original.title}</div><div className="text-xs text-muted-foreground">{row.original.category_name ?? row.original.category_code}</div></div> },
    { id: "comment", header: "Komentar", cell: ({ row }) => <span className="text-sm">{row.original.comment ?? <span className="text-muted-foreground">—</span>}</span> },
    { id: "tenant", header: "Tenant", cell: ({ row }) => <span className="text-sm">{row.original.tenant_name ?? "—"}</span>, size: 160 },
    { id: "created_at", header: "Waktu", cell: ({ row }) => <span className="text-xs text-muted-foreground"><RelativeTime value={row.original.created_at} /></span>, size: 120 },
  ], []);
  return (
    <div>
      <PageHeader title="Feedback / CSAT" subtitle={avg != null ? `Rata-rata ${avg.toFixed(2)} dari ${rows.length} feedback (dimuat)` : "Belum ada feedback."} />
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.service_request_id} loading={list.isLoading} empty={{ message: "Belum ada feedback tenant." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
    </div>
  );
}
