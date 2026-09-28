// Tenant Relation › Announcements (PRD P1 v1.3 §3.5 Communication; PRD P3 v2.1 §5.10 P3-ANN-01..06, §6.5 P3-BRC-01).
// Kategori Pengumuman · News · Alert, sasaran building/tower/lantai/unit/tenant, jadwal publish & kedaluwarsa, gambar, pelacakan
// baca & konfirmasi, dan Broadcast darurat (langsung terbit, in-app + push). Draft → Terjadwal → Terbit → Diarsipkan.
// Detail = drawer di /tenant-relation/announcements/:id (deep link notifikasi).
import { useMemo, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button } from "@/components/ui/primitives";
import { DataGrid, useUrlFilters } from "@/components/bv/datagrid";
import { RelativeTime } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText } from "@/components/bv/cells";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusOptions } from "@/lib/status";
import { fmtDateTime, fmtNumber } from "@/lib/format";
import { FilterRow, SelectFilter } from "@/features/security/shared";
import type { Announcement } from "./types";
import { ANNOUNCEMENT_AUDIENCES, ANNOUNCEMENT_CATEGORIES, ANNOUNCEMENT_CATEGORY_ICON, labelOf, optionsOf } from "./labels";
import { AnnouncementActionDialog, AnnouncementDrawer, type AnnAction } from "./AnnouncementDrawer";
import { AnnouncementFormDialog, BroadcastDialog } from "./AnnouncementForm";
import { useNow } from "./hooks";

export default function AnnouncementsPage() {
  const { id } = useParams();
  const nav = useNavigate();
  const { search } = useLocation();
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  const status = f.get("status");
  const category = f.get("category");
  const list = useList<Announcement>("announcements", { property_id: propertyId ?? undefined, status: status || undefined, category: category || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [edit, setEdit] = useState<Announcement | "new" | null>(null);
  const [broadcastOpen, setBroadcastOpen] = useState(false);
  const [pending, setPending] = useState<{ a: Announcement; action: AnnAction } | null>(null);
  const now = useNow();
  const columns = useMemo<ColumnDef<Announcement, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): judul satu baris (ikon kategori), sasaran satu baris, satu status (+ satu
    // penanda kedaluwarsa), dibaca, terbit/jadwal. Ringkasan isi, badge kategori/penting/wajib konfirmasi, rincian target,
    // konfirmasi baca & pembuat ada di drawer detail.
    { id: "title", header: "Judul", meta: { mobile: "primary" }, cell: ({ row: { original: a } }) => (
      <div className="flex min-w-0 items-center gap-1.5 font-medium text-on-surface">
        <Icon name={ANNOUNCEMENT_CATEGORY_ICON[a.category] ?? "campaign"} size={16} className="shrink-0 text-on-surface-variant" aria-label={labelOf(ANNOUNCEMENT_CATEGORIES, a.category)} />
        <span className="truncate" title={a.title}>{a.title}</span>
      </div>
    ) },
    { id: "scope", header: "Sasaran", meta: { mobile: "secondary" }, cell: ({ row: { original: a } }) => {
      const locs = a.targets.filter((t) => t.kind === "location").length;
      const tens = a.targets.filter((t) => t.kind === "tenant").length;
      const who = locs || tens ? [locs ? `${locs} lokasi` : "", tens ? `${tens} tenant` : ""].filter(Boolean).join(" · ") : "Semua tenant property";
      const text = `${a.property_name ?? "Seluruh organisasi"} · ${labelOf(ANNOUNCEMENT_AUDIENCES, a.audience)}`;
      return <CellText max={200} title={`${text} · ${who}`}>{text}</CellText>;
    } },
    { id: "status", header: "Status", meta: { mobile: "status" }, size: 150, cell: ({ row: { original: a } }) => (
      <div className="flex flex-wrap items-center gap-1">
        <StatusBadge objectType="announcement" status={a.status} />
        {a.status === "published" && a.expires_at && new Date(a.expires_at).getTime() < now && <Badge tone="neutral">Kedaluwarsa</Badge>}
      </div>
    ) },
    { id: "reads", header: "Dibaca", meta: { mobile: "secondary" }, size: 110, cell: ({ row: { original: a } }) => (a.status === "published" || a.status === "archived") && a.recipients_count != null ? (
      <span className="tnum whitespace-nowrap text-sm" title={a.requires_ack ? `${fmtNumber(a.ack_count)} dikonfirmasi` : undefined}><span className="font-semibold text-on-surface">{fmtNumber(a.read_count)}</span><span className="text-on-surface-variant"> / {fmtNumber(a.recipients_count)}</span></span>
    ) : <span className="text-sm text-on-surface-variant">—</span> },
    { id: "published_at", header: "Terbit", meta: { mobile: "hidden" }, size: 140, cell: ({ row: { original: a } }) => (
      <span className="whitespace-nowrap text-sm text-on-surface-variant">{a.published_at ? <RelativeTime value={a.published_at} /> : a.status === "scheduled" && a.publish_at ? fmtDateTime(a.publish_at) : "—"}</span>
    ) },
  ], [now]);
  const close = () => nav(`/tenant-relation/announcements${search}`);
  const rowActions = (a: Announcement) => [
    ...(a.allowed_actions.includes("update") ? [{ label: "Edit", icon: "edit", onSelect: () => setEdit(a) }] : []),
    ...(a.allowed_actions.includes("publish") ? [{ label: "Publikasikan sekarang", icon: "publish", onSelect: () => setPending({ a, action: "publish" }) }] : []),
    ...(a.allowed_actions.includes("schedule") && a.publish_at && new Date(a.publish_at).getTime() > now ? [{ label: "Jadwalkan", icon: "schedule", onSelect: () => setPending({ a, action: "schedule" }) }] : []),
    ...(a.allowed_actions.includes("unschedule") ? [{ label: "Batalkan jadwal", icon: "event_busy", onSelect: () => setPending({ a, action: "unschedule" }) }] : []),
    ...(a.allowed_actions.includes("archive") ? [{ label: "Arsipkan", icon: "archive", destructive: true, onSelect: () => setPending({ a, action: "archive" }) }] : []),
  ];
  return (
    <div>
      <PageHeader
        title="Announcements"
        subtitle="Pengumuman, News, dan Alert dari building management ke tenant (tampil di Tenant App) — bertarget, terjadwal, dan terlacak bacanya."
        actions={
          <>
            {can("tenant_relation.announcements.broadcast") && <Button variant="secondary" icon="emergency_home" onClick={() => setBroadcastOpen(true)}>Broadcast darurat</Button>}
            {can("tenant_relation.announcements.create") && <Button icon="add" onClick={() => setEdit("new")}>Buat Announcement</Button>}
          </>
        }
      />
      <div className="mb-3 flex flex-wrap items-center gap-1.5" role="group" aria-label="Kategori">
        <FilterChip selected={!category} onClick={() => f.set({ category: null })}>Semua</FilterChip>
        {optionsOf(ANNOUNCEMENT_CATEGORIES).map((c) => <FilterChip key={c.value} selected={category === c.value} onClick={() => f.set({ category: category === c.value ? null : c.value })}>{c.label}</FilterChip>)}
      </div>
      <FilterRow>
        <SelectFilter label="Status" value={status} onChange={(v) => f.set({ status: v })} options={statusOptions("announcement")} />
        {(status || category) && <Button variant="ghost" size="sm" icon="replay" onClick={() => f.set({ status: null, category: null })}>Reset filter</Button>}
      </FilterRow>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/tenant-relation/announcements/${r.id}${search}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={!!status || !!category}
        empty={{ icon: "campaign", title: "Belum ada announcement.", description: "Buat pengumuman, News, atau Alert untuk tenant — dapat ditargetkan ke building, lantai, unit, atau tenant tertentu.", action: can("tenant_relation.announcements.create") ? <Button icon="add" onClick={() => setEdit("new")}>Buat Announcement</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowActions={rowActions}
        rowClassName={(r) => (r.id === id ? "bg-primary-soft" : r.category === "alert" && r.status === "published" ? "border-l-4 border-l-critical" : undefined)}
      />
      {id && <AnnouncementDrawer id={id} onClose={close} onEdit={(a) => setEdit(a)} />}
      {edit && <AnnouncementFormDialog item={edit === "new" ? null : edit} propertyId={propertyId} onClose={() => setEdit(null)} onSaved={(a) => { if (edit === "new") nav(`/tenant-relation/announcements/${a.id}${search}`); }} />}
      {broadcastOpen && <BroadcastDialog onClose={() => setBroadcastOpen(false)} onSent={(a) => nav(`/tenant-relation/announcements/${a.id}${search}`)} />}
      {pending && <AnnouncementActionDialog a={pending.a} action={pending.action} onClose={() => setPending(null)} />}
    </div>
  );
}
