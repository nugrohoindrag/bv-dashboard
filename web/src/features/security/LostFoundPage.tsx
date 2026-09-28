// Security › Lost & Found (PRD P2 v2.1 §6.7): barang temuan (foto, lokasi, penemu, lokasi simpan, masa simpan) →
// pencocokan dengan laporan kehilangan → serah terima ke pemilik (identitas + tanda tangan) atau disposal setelah masa
// simpan. Tab: /security/lost-found (barang temuan) · /security/lost-found/reports (laporan kehilangan).
// Detail barang: /security/lost-found/:id (LostFoundDetailPage; deep link notifikasi).
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Tabs, TabsContent, TabsList, TabsTrigger, Textarea } from "@/components/ui/primitives";
import { DataGrid, useUrlFilters } from "@/components/bv/datagrid";
import { AsyncState, LocationPath, RelativeTime, useToast } from "@/components/bv/common";
import { LocationPicker } from "@/components/bv/pickers";
import { Fab } from "@/components/bv/mobile";
import { useAction, useInvalidate, useList } from "@/api/hooks";
import { api, uuid, type ListResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDate, fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { LostFoundItem, LostReport } from "./types";
import { LOST_FOUND_CATEGORIES, labelOf, localDateTimeInput, optionsOf } from "./p2";
import { DisposalDueBadge, FilterRow, PropertyField, SearchBox, SelectFilter } from "./shared";
import { StatusBadge } from "@/components/bv/badges";
import { CellLocation, CellText, CellTitle } from "@/components/bv/cells";
import { lastSegment } from "@/components/bv/cell-helpers";
import { statusOptions } from "@/lib/status";
import { TOUCH, usePropertyChoice } from "./hooks";

export default function LostFoundPage({ tab = "items" }: { tab?: "items" | "reports" }) {
  const nav = useNavigate();
  const { can } = useAuth();
  const canCreate = can("security.lost_found.create");
  const [itemOpen, setItemOpen] = useState(false);
  const [reportOpen, setReportOpen] = useState(false);
  const [matchFor, setMatchFor] = useState<LostReport | null>(null);
  const openCreate = () => (tab === "reports" ? setReportOpen(true) : setItemOpen(true));
  return (
    <div>
      <PageHeader
        title="Lost & Found"
        subtitle="Barang temuan disimpan dengan masa simpan, dicocokkan dengan laporan kehilangan, lalu diserahkan ke pemilik atau di-disposal."
        actions={canCreate && (
          <span className="hidden md:inline-flex">
            {tab === "reports" ? <Button icon="add" onClick={() => setReportOpen(true)}>Catat Laporan Kehilangan</Button> : <Button icon="add" onClick={() => setItemOpen(true)}>Catat Barang Temuan</Button>}
          </span>
        )}
      />
      <Tabs value={tab} onValueChange={(v) => nav(v === "reports" ? "/security/lost-found/reports" : "/security/lost-found")}>
        <TabsList>
          <TabsTrigger value="items">Barang Temuan</TabsTrigger>
          <TabsTrigger value="reports">Laporan Kehilangan</TabsTrigger>
        </TabsList>
        <TabsContent value="items"><ItemsTab onCreate={canCreate ? () => setItemOpen(true) : undefined} /></TabsContent>
        <TabsContent value="reports"><ReportsTab onCreate={canCreate ? () => setReportOpen(true) : undefined} onMatches={setMatchFor} /></TabsContent>
      </Tabs>
      {canCreate && <Fab label="Catat" aria-label={tab === "reports" ? "Catat Laporan Kehilangan" : "Catat Barang Temuan"} onClick={openCreate} />}
      {itemOpen && <ItemCreateDialog onClose={() => setItemOpen(false)} onCreated={(x) => nav(`/security/lost-found/${x.id}`)} />}
      {reportOpen && <ReportCreateDialog onClose={() => setReportOpen(false)} onCreated={(r) => setMatchFor(r)} />}
      {matchFor && <MatchesDialog report={matchFor} onClose={() => setMatchFor(null)} />}
    </div>
  );
}

// ---------- Barang temuan ----------
function ItemsTab({ onCreate }: { onCreate?: () => void }) {
  const { propertyId } = useAuth();
  const f = useUrlFilters();
  const due = f.get("disposal_due") === "true";
  const list = useList<LostFoundItem>("lost-found/items", { property_id: propertyId ?? undefined, status: f.get("status") || undefined, category: f.get("category") || undefined, disposal_due: due || undefined, q: f.get("q") || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<LostFoundItem, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026): nomor + deskripsi, lokasi temuan = nama terakhir, waktu temuan, lokasi simpan,
      // masa simpan (+ badge lewat masa simpan), satu status. Kategori, penemu & laporan yang cocok ada di halaman detail.
      { id: "item", header: "Barang", meta: { mobile: "primary" }, cell: ({ row: { original: x } }) => <CellTitle code={x.item_number} title={x.description || labelOf(LOST_FOUND_CATEGORIES, x.category)} /> },
      { id: "found_location", header: "Lokasi temuan", meta: { mobile: "secondary" }, cell: ({ row }) => <CellLocation path={row.original.found_location.path_text} max={150} /> },
      { id: "found", header: "Ditemukan", meta: { mobile: "secondary" }, size: 150, cell: ({ row }) => <span className="tnum whitespace-nowrap text-sm">{fmtDateTime(row.original.found_at)}</span> },
      { id: "storage", header: "Lokasi simpan", meta: { mobile: "hidden" }, size: 150, cell: ({ row }) => <CellText max={150}>{row.original.storage_location || "—"}</CellText> },
      { id: "retention", header: "Masa simpan", meta: { mobile: "secondary" }, size: 170, cell: ({ row: { original: x } }) => <div className="flex items-center gap-1 whitespace-nowrap"><span className="text-sm tnum">{fmtDate(x.retention_until)}</span><DisposalDueBadge due={x.disposal_due} /></div> },
      { id: "status", header: "Status", meta: { mobile: "status" }, size: 150, cell: ({ row: { original: x } }) => <StatusBadge objectType="lost_found_item" status={x.status} /> },
    ],
    [],
  );
  return (
    <div className="space-y-3">
      <FilterRow className="mb-0">
        <SearchBox value={f.get("q")} onSubmit={(q) => f.set({ q })} placeholder="Cari nomor / deskripsi…" />
        <SelectFilter label="Status" value={f.get("status")} onChange={(v) => f.set({ status: v })} options={statusOptions("lost_found_item")} />
        <SelectFilter label="Kategori" value={f.get("category")} onChange={(v) => f.set({ category: v })} options={optionsOf(LOST_FOUND_CATEGORIES)} />
        <FilterChip selected={due} onClick={() => f.set({ disposal_due: due ? null : "true" })}>Lewat masa simpan</FilterChip>
        {f.isFiltered && <Button variant="ghost" size="sm" icon="replay" onClick={f.reset}>Reset filter</Button>}
      </FilterRow>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/security/lost-found/${r.id}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={f.isFiltered}
        empty={{ icon: "inventory_2", title: "Belum ada barang temuan", description: "Barang yang ditemukan staf/tenant dicatat di sini dengan foto, lokasi temuan, dan lokasi penyimpanannya.", action: onCreate ? <Button icon="add" onClick={onCreate}>Catat Barang Temuan</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowClassName={(r) => (r.disposal_due ? "border-l-4 border-l-warning" : undefined)}
      />
    </div>
  );
}

function ItemCreateDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (x: LostFoundItem) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [pid, setPid] = usePropertyChoice();
  const [f, setF] = useState({ category: "other", description: "", found_location_id: null as string | null, found_at: localDateTimeInput(), finder_name: "", storage_location: "" });
  const [busy, setBusy] = useState(false);
  const valid = !!f.description.trim() && (!!pid || !!f.found_location_id);
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    try {
      const x = await api<LostFoundItem>("lost-found/items", {
        body: { property_id: pid || undefined, category: f.category, description: f.description.trim(), found_location_id: f.found_location_id, found_at: f.found_at ? new Date(f.found_at).toISOString() : undefined, finder_name: f.finder_name.trim() || null, storage_location: f.storage_location.trim() || null },
        idempotencyKey: uuid(),
      });
      invalidate("list");
      toast.success(`Barang ${x.item_number} dicatat — tambahkan foto di detail.`, { to: `/security/lost-found/${x.id}`, label: "Buka" });
      onClose();
      onCreated(x);
    } catch (e) {
      toast.failed("created", e, "Barang temuan");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Catat Barang Temuan" description="Masa simpan default 90 hari sejak ditemukan; foto diunggah di detail setelah tersimpan.">
        <div className="space-y-4">
          <PropertyField value={pid} onChange={(v) => { setPid(v); setF({ ...f, found_location_id: null }); }} />
          <Field label="Kategori" required><NativeSelect value={f.category} onChange={(e) => setF({ ...f, category: e.target.value })}>{optionsOf(LOST_FOUND_CATEGORIES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
          <Field label="Deskripsi barang" required><Textarea rows={3} autoFocus value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} placeholder="mis. Dompet kulit coklat berisi kartu, tanpa uang tunai" /></Field>
          <Field label="Lokasi ditemukan"><LocationPicker propertyId={pid || null} value={f.found_location_id} onChange={(id) => setF({ ...f, found_location_id: id })} /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Waktu ditemukan"><Input type="datetime-local" value={f.found_at} onChange={(e) => setF({ ...f, found_at: e.target.value })} /></Field>
            <Field label="Nama penemu" help="Kosongkan bila ditemukan oleh Anda."><Input value={f.finder_name} onChange={(e) => setF({ ...f, finder_name: e.target.value })} /></Field>
          </div>
          <Field label="Lokasi penyimpanan" help="mis. Loker Security Pos 1, rak B-2."><Input value={f.storage_location} onChange={(e) => setF({ ...f, storage_location: e.target.value })} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Laporan kehilangan ----------
function ReportsTab({ onCreate, onMatches }: { onCreate?: () => void; onMatches: (r: LostReport) => void }) {
  const { propertyId } = useAuth();
  const f = useUrlFilters();
  const list = useList<LostReport>("lost-found/reports", { property_id: propertyId ?? undefined, status: f.get("status") || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<LostReport, unknown>[]>(
    () => [
      // Satu baris per sel (29 Sep 2026): tenant/kontak pelapor & waktu hilang tetap tersedia di tooltip (laporan tanpa halaman detail).
      { id: "report", header: "Laporan", meta: { mobile: "primary" }, cell: ({ row: { original: r } }) => <CellTitle code={r.report_number} title={r.description || labelOf(LOST_FOUND_CATEGORIES, r.category)} /> },
      { id: "reporter", header: "Pelapor", meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <CellText max={170} title={[r.reporter_name, r.tenant_name, r.reporter_contact].filter(Boolean).join(" · ")}>{r.reporter_name}</CellText> },
      { id: "lost", header: "Hilang di", meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <CellText max={160} title={`${r.lost_location.path_text ?? "—"} · ${r.lost_at ? fmtDateTime(r.lost_at) : "Waktu tidak diketahui"}`}>{r.lost_location.path_text ? lastSegment(r.lost_location.path_text) : "—"}</CellText> },
      { id: "status", header: "Status", meta: { mobile: "status" }, size: 160, cell: ({ row: { original: r } }) => <div className="flex items-center gap-1 whitespace-nowrap"><StatusBadge objectType="lost_report" status={r.status} />{r.matched_item_id && <Link to={`/security/lost-found/${r.matched_item_id}`} onClick={(e) => e.stopPropagation()} className="font-mono text-xs text-primary hover:underline">{r.matched_item_number ?? "Barang"}</Link>}</div> },
      { id: "created_at", header: "Dilaporkan", meta: { mobile: "hidden" }, size: 130, cell: ({ row }) => <RelativeTime value={row.original.created_at} className="whitespace-nowrap text-sm" /> },
      { id: "actions", header: "", size: 170, cell: ({ row: { original: r } }) => (r.status === "open" ? <Button size="sm" variant="secondary" icon="search" className={TOUCH} onClick={(e) => { e.stopPropagation(); onMatches(r); }}>Cari kecocokan</Button> : null) },
    ],
    [onMatches],
  );
  return (
    <div className="space-y-3">
      <FilterRow className="mb-0">
        <SelectFilter label="Status" value={f.get("status")} onChange={(v) => f.set({ status: v })} options={statusOptions("lost_report")} />
        {f.isFiltered && <Button variant="ghost" size="sm" icon="replay" onClick={f.reset}>Reset filter</Button>}
      </FilterRow>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => { if (r.status === "open") onMatches(r); else if (r.matched_item_id) return `/security/lost-found/${r.matched_item_id}`; }}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={f.isFiltered}
        empty={{ icon: "search", title: "Belum ada laporan kehilangan", description: "Laporan dari tenant/tamu yang kehilangan barang dicatat di sini lalu dicocokkan dengan barang temuan.", action: onCreate ? <Button icon="add" onClick={onCreate}>Catat Laporan Kehilangan</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
      />
    </div>
  );
}

function ReportCreateDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (r: LostReport) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [pid, setPid] = usePropertyChoice();
  const [f, setF] = useState({ reporter_name: "", reporter_contact: "", category: "other", description: "", lost_location_id: null as string | null, lost_at: "" });
  const [busy, setBusy] = useState(false);
  const valid = !!f.reporter_name.trim() && !!f.description.trim() && (!!pid || !!f.lost_location_id);
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    try {
      const r = await api<LostReport>("lost-found/reports", {
        body: { property_id: pid || undefined, reporter_name: f.reporter_name.trim(), reporter_contact: f.reporter_contact.trim() || null, category: f.category, description: f.description.trim(), lost_location_id: f.lost_location_id, lost_at: f.lost_at ? new Date(f.lost_at).toISOString() : null },
        idempotencyKey: uuid(),
      });
      invalidate("list");
      toast.action("created", `Laporan ${r.report_number}`);
      onClose();
      onCreated(r);
    } catch (e) {
      toast.failed("created", e, "Laporan kehilangan");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Catat Laporan Kehilangan" description="Setelah disimpan, kandidat barang temuan yang cocok langsung ditampilkan.">
        <div className="space-y-4">
          <PropertyField value={pid} onChange={(v) => { setPid(v); setF({ ...f, lost_location_id: null }); }} />
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Nama pelapor" required><Input value={f.reporter_name} autoFocus onChange={(e) => setF({ ...f, reporter_name: e.target.value })} /></Field>
            <Field label="Kontak pelapor"><Input type="tel" inputMode="tel" value={f.reporter_contact} onChange={(e) => setF({ ...f, reporter_contact: e.target.value })} placeholder="No. HP / unit" /></Field>
          </div>
          <Field label="Kategori" required><NativeSelect value={f.category} onChange={(e) => setF({ ...f, category: e.target.value })}>{optionsOf(LOST_FOUND_CATEGORIES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
          <Field label="Deskripsi barang" required><Textarea rows={3} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} placeholder="Ciri-ciri: warna, merek, isi, tanda khusus" /></Field>
          <Field label="Lokasi hilang (perkiraan)"><LocationPicker propertyId={pid || null} value={f.lost_location_id} onChange={(id) => setF({ ...f, lost_location_id: id })} /></Field>
          <Field label="Waktu hilang (perkiraan)"><Input type="datetime-local" value={f.lost_at} onChange={(e) => setF({ ...f, lost_at: e.target.value })} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Kandidat barang temuan untuk laporan kehilangan (GET /lost-found/reports/{id}/matches) + aksi Cocokkan. */
function MatchesDialog({ report, onClose }: { report: LostReport; onClose: () => void }) {
  const toast = useToast();
  const q = useQuery({ queryKey: ["lost-found-matches", report.id], queryFn: ({ signal }) => api<ListResponse<LostFoundItem>>(`lost-found/reports/${report.id}/matches`, { signal }).then((r) => r.data) });
  const match = useAction<{ itemId: string }, LostFoundItem>((i) => `lost-found/items/${i.itemId}/match`, { body: () => ({ report_id: report.id }), invalidate: ["list", "one", "lost-found-matches"] });
  const [matched, setMatched] = useState<LostFoundItem | null>(null);
  const run = (x: LostFoundItem) => match.mutateAsync({ itemId: x.id }).then((r) => { setMatched(r); toast.success(`${report.report_number} dicocokkan dengan ${r.item_number}`, { to: `/security/lost-found/${r.id}`, label: "Buka barang" }); }).catch((e) => toast.failed("updated", e, report.report_number));
  const open = report.status === "open" && !matched;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={`Kecocokan · ${report.report_number}`} description={`${labelOf(LOST_FOUND_CATEGORIES, report.category)} · ${report.description}`}>
        {matched && <p className="mb-3 rounded-[var(--radius-md)] bg-success-container px-3 py-2 text-sm text-on-success-container">Dicocokkan dengan {matched.item_number}. Lanjutkan serah terima di <Link to={`/security/lost-found/${matched.id}`} className="font-semibold underline">detail barang</Link>.</p>}
        <AsyncState query={q} empty={{ icon: "search", title: "Belum ada barang temuan yang cocok", description: "Kandidat = kategori sama, masih disimpan, dan ditemukan sejak sehari sebelum waktu hilang. Coba lagi setelah ada barang temuan baru." }}>
          {(items) => (
            <ul className="space-y-2">
              {items.map((x) => (
                <li key={x.id}>
                  <Card className={cn("p-3", matched?.id === x.id && "ring-2 ring-primary")}>
                    <div className="flex flex-wrap items-start justify-between gap-2">
                      <div className="min-w-0">
                        <Link to={`/security/lost-found/${x.id}`} className="font-mono text-[13px] font-semibold hover:underline">{x.item_number}</Link>
                        <p className="line-clamp-2 text-sm">{x.description}</p>
                        <div className="mt-1 text-xs text-on-surface-variant"><LocationPath pathText={x.found_location.path_text} className="text-xs" /> · {fmtDateTime(x.found_at)}{x.storage_location ? ` · disimpan di ${x.storage_location}` : ""}</div>
                        {x.attachment_count > 0 && <div className="mt-1 inline-flex items-center gap-1 text-xs text-on-surface-variant"><Icon name="photo_camera" size={12} aria-hidden />{x.attachment_count} foto</div>}
                      </div>
                      {open && x.allowed_actions.includes("match") && <Button size="sm" icon="link" className={TOUCH} loading={match.isPending && match.variables?.itemId === x.id} onClick={() => run(x)}>Cocokkan</Button>}
                    </div>
                  </Card>
                </li>
              ))}
            </ul>
          )}
        </AsyncState>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Tutup</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
