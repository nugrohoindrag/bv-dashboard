// Tenant Relation › Feedback: tab CSAT (PRD P1 v1.3 §18 — rating 1–5 + komentar per permintaan) dan Feedback Umum (PRD P3 v2.1
// P3-FDB-02..03 — saran/pujian/keluhan/pertanyaan tenant di luar permintaan, anonim opsional → tinjau · tanggapi · tutup).
// Rute: /tenant-relation/feedback (CSAT; `?tab=general` dari drill-down KPI), /tenant-relation/feedback/:tab (csat | general),
// /tenant-relation/feedback/general/:id (deep link notifikasi → drawer detail).
import { useMemo } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { CellText, CellTitle } from "@/components/bv/cells";
import { RelativeTime } from "@/components/bv/common";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDecimal } from "./utils";
import { GeneralFeedbackTab } from "./GeneralFeedbackTab";

interface FeedbackRow { service_request_id: string; request_number: string; title: string; category_code: string; category_name: string | null; rating: number; comment: string | null; tenant_name: string | null; created_at: string }

function Stars({ n }: { n: number }) {
  return <span className="inline-flex items-center" aria-label={`${n} dari 5`}>{[1, 2, 3, 4, 5].map((i) => <Icon key={i} name={i <= n ? "star" : "star_border"} size={16} color={i <= n ? "var(--color-warning)" : "var(--color-outline)"} />)}</span>;
}

type Tab = "csat" | "general";

export default function FeedbackPage() {
  const params = useParams();
  const [sp] = useSearchParams();
  const nav = useNavigate();
  const generalId = params.id;
  const tab: Tab = generalId || params.tab === "general" || (!params.tab && sp.get("tab") === "general") ? "general" : "csat";
  return (
    <div>
      <PageHeader title="Feedback / CSAT" subtitle={tab === "csat" ? "Rating kepuasan tenant per permintaan setelah ditutup." : "Saran, pujian, keluhan, dan pertanyaan umum tenant dari Tenant App — di luar permintaan."} />
      <Tabs value={tab} onValueChange={(v) => nav(`/tenant-relation/feedback/${v}`)}>
        <TabsList>
          <TabsTrigger value="csat">CSAT Permintaan</TabsTrigger>
          <TabsTrigger value="general">Feedback Umum</TabsTrigger>
        </TabsList>
        <TabsContent value="csat"><CsatTab /></TabsContent>
        <TabsContent value="general"><GeneralFeedbackTab selectedId={generalId} /></TabsContent>
      </Tabs>
    </div>
  );
}

function CsatTab() {
  const { propertyId } = useAuth();
  const list = useList<FeedbackRow>("tenant-relation/feedback", { property_id: propertyId ?? undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const avg = rows.length ? rows.reduce((a, r) => a + r.rating, 0) / rows.length : null;
  const columns = useMemo<ColumnDef<FeedbackRow, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): nomor + judul permintaan dalam satu kolom, rating, komentar & tenant satu
    // baris (teks lengkap di tooltip), waktu. Kategori ada di halaman detail permintaan.
    { id: "title", header: "Permintaan", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.request_number} title={row.original.title} /> },
    { id: "rating", header: "Rating", meta: { mobile: "status" }, cell: ({ row }) => <Stars n={row.original.rating} />, size: 110 },
    { id: "comment", header: "Komentar", meta: { mobile: "secondary" }, cell: ({ row }) => <CellText max={260} muted={!row.original.comment}>{row.original.comment ?? "—"}</CellText> },
    { id: "tenant", header: "Tenant", meta: { mobile: "secondary" }, cell: ({ row }) => <CellText max={160}>{row.original.tenant_name ?? "—"}</CellText> },
    { id: "created_at", header: "Waktu", meta: { mobile: "hidden" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm text-muted-foreground"><RelativeTime value={row.original.created_at} /></span>, size: 120 },
  ], []);
  return (
    <div className="space-y-3">
      {!list.isLoading && !list.isError && <p className="text-sm text-on-surface-variant">{avg != null ? `Rata-rata ${fmtDecimal(avg, 2)} dari ${rows.length} feedback (dimuat).` : "Belum ada feedback."}</p>}
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.service_request_id} onRowClick={(r) => `/tenant-relation/service-requests/${r.service_request_id}`} loading={list.isLoading} error={list.error} onRetry={() => list.refetch()} empty={{ icon: "reviews", title: "Belum ada feedback tenant.", description: "Rating muncul setelah tenant menilai permintaan yang sudah ditutup." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
    </div>
  );
}
