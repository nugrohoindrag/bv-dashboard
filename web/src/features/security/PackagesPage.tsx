// Security › Packages — paket masuk, pemberitahuan tenant, serah terima (PRD P3 v2.1 §5.9 P3-PKG-01..04; Roadmap v2.1 §11, §39.2).
// Daftar GET /packages (property_id, status, waiting, q) dengan tampilan "Menunggu diambil" (default) / "Semua"; catat paket masuk
// (security.packages.record); detail di /security/packages/:id (deep link notifikasi) — serah terima, pengingat, retur, WhatsApp.
import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { FilterChip } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button } from "@/components/ui/primitives";
import { DataGrid, useUrlFilters } from "@/components/bv/datagrid";
import { RelativeTime } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { Fab } from "@/components/bv/mobile";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { useProfile } from "@/lib/profile";
import { statusOptions } from "@/lib/status";
import { FilterRow, SearchBox, SelectFilter } from "./shared";
import { PackageTypeLabel, RecordPackageDialog, type Package } from "./package-dialogs";
import { PackageDetail } from "./package-detail";

export default function PackagesPage() {
  const { id } = useParams();
  if (id) return <PackageDetail />;
  return <PackagesList />;
}

const VIEWS = [
  { key: "", label: "Menunggu diambil" },
  { key: "all", label: "Semua" },
];

function PackagesList() {
  const nav = useNavigate();
  const { propertyId, can } = useAuth();
  const prof = useProfile();
  const f = useUrlFilters();
  const view = f.get("view");
  const status = f.get("status");
  const q = f.get("q");
  const list = useList<Package>("packages", { property_id: propertyId ?? undefined, waiting: view === "all" ? undefined : true, status: status || undefined, q: q || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [createOpen, setCreateOpen] = useState(false);
  const canRecord = can("security.packages.record");
  // ambang pengingat (Settings › Property Profile; default 3 hari) untuk menandai paket yang lama belum diambil
  const reminderDays = Number((prof.context?.config as { package_reminder_days?: number } | undefined)?.package_reminder_days ?? 3) || 3;
  const columns = useMemo<ColumnDef<Package, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): no. paket + penerima satu kolom, unit/tenant satu baris, jenis · kurir satu baris
    // (resi di tooltip), satu status + badge lama menunggu. Petugas penerima & waktu serah terima ada di halaman detail.
    { id: "recipient", header: "Paket", meta: { mobile: "primary" }, cell: ({ row: { original: p } }) => <CellTitle code={p.package_number} title={p.recipient_name} /> },
    { id: "unit", header: "Unit / tenant", meta: { mobile: "secondary" }, cell: ({ row: { original: p } }) => <CellText max={170}>{[p.unit_name, p.tenant_name].filter(Boolean).join(" · ") || "—"}</CellText> },
    { id: "courier", header: "Kurir", meta: { mobile: "secondary" }, cell: ({ row: { original: p } }) => (
      <CellText max={170} title={[p.courier, p.tracking_number].filter(Boolean).join(" · ") || undefined}><PackageTypeLabel type={p.package_type} withIcon />{p.courier ? ` · ${p.courier}` : ""}</CellText>
    ) },
    { id: "storage", header: "Lokasi simpan", meta: { mobile: "hidden" }, size: 150, cell: ({ row }) => <CellText max={150} muted={!row.original.storage_location}>{row.original.storage_location || "—"}</CellText> },
    { id: "status", header: "Status", meta: { mobile: "status" }, size: 170, cell: ({ row: { original: p } }) => {
      const waiting = p.status === "received" || p.status === "notified";
      return (
        <div className="flex items-center gap-1 whitespace-nowrap">
          <StatusBadge objectType="package" status={p.status} />
          {waiting && p.days_waiting >= reminderDays && <Badge tone="warning">{p.days_waiting} hari</Badge>}
        </div>
      );
    } },
    { id: "received_at", header: "Diterima", meta: { mobile: "secondary" }, size: 150, cell: ({ row }) => <RelativeTime value={row.original.received_at} className="whitespace-nowrap text-sm" /> },
  ], [reminderDays]);
  const isFiltered = !!status || !!q;
  return (
    <div>
      <PageHeader
        title="Packages"
        subtitle="Paket masuk untuk tenant: dicatat resepsionis/security, tenant diberi tahu lewat Tenant App, serah terima dengan tanda tangan atau retur."
        actions={canRecord && <span className="hidden md:inline-flex"><Button icon="add" onClick={() => setCreateOpen(true)}>Catat Paket</Button></span>}
      />
      <div className="mb-3 flex flex-wrap items-center gap-1.5" role="group" aria-label="Tampilan">
        {VIEWS.map((v) => <FilterChip key={v.key || "waiting"} selected={view === v.key} onClick={() => f.set({ view: v.key || null })}>{v.label}</FilterChip>)}
      </div>
      <FilterRow>
        <SearchBox value={q} onSubmit={(v) => f.set({ q: v })} placeholder="Cari no. paket / penerima / resi / unit…" />
        <SelectFilter label="Status" value={status} onChange={(v) => f.set({ status: v })} options={statusOptions("package").filter((o) => view === "all" || o.value === "received" || o.value === "notified")} />
        {isFiltered && <Button variant="ghost" size="sm" icon="replay" onClick={() => f.set({ status: null, q: null })}>Reset filter</Button>}
      </FilterRow>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/security/packages/${r.id}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={isFiltered}
        empty={view === "all"
          ? { icon: "inventory_2", title: "Belum ada paket tercatat", description: "Catat paket yang diterima resepsionis/security agar tenant diberi tahu.", action: canRecord ? <Button icon="add" onClick={() => setCreateOpen(true)}>Catat Paket</Button> : undefined }
          : { icon: "inventory_2", title: "Tidak ada paket yang menunggu diambil", description: "Paket yang sudah diambil atau diretur ada di tampilan Semua.", action: canRecord ? <Button icon="add" onClick={() => setCreateOpen(true)}>Catat Paket</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowClassName={(r) => ((r.status === "received" || r.status === "notified") && r.days_waiting >= reminderDays ? "border-l-4 border-l-warning" : undefined)}
      />
      {canRecord && <Fab label="Catat" aria-label="Catat paket" onClick={() => setCreateOpen(true)} />}
      {createOpen && <RecordPackageDialog onClose={() => setCreateOpen(false)} onCreated={(p) => nav(`/security/packages/${p.id}`)} />}
    </div>
  );
}
