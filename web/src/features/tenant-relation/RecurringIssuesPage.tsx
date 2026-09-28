// Isu Berulang — Recurring Issue Detection (PRD P3 v2.1 P3-TSH-07, P3-CMP-03): lokasi + kategori yang dilaporkan ≥ ambang dalam
// jendela waktu (Settings › Property Profile) menjadi sinyal Attention Required; keluhan berulang otomatis naik prioritas.
// Daftar GET /recurring-issues (property_id, status; kosong = Terbuka + Ditangani) → detail /tenant-relation/recurring-issues/:id
// (permintaan terkait → daftar Service Request `?recurring_issue_id=`) → Tangani (acknowledge) / Selesaikan (resolve).
import { useMemo, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Dialog, DialogContent, DialogFooter, Field, Textarea } from "@/components/ui/primitives";
import { DataGrid, useUrlFilters } from "@/components/bv/datagrid";
import { AsyncState, FormSkeleton, KeyValue, LocationPath, RelativeTime, useToast } from "@/components/bv/common";
import { PriorityBadge, StatusBadge } from "@/components/bv/badges";
import { CellText } from "@/components/bv/cells";
import { useAction, useAll, useList, useOne } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDate, fmtDateTime, fmtNumber } from "@/lib/format";
import type { ServiceRequest } from "@/api/types";
import type { RecurringIssue } from "./types";

const VIEWS = [
  { key: "", label: "Aktif" },
  { key: "open", label: "Terbuka" },
  { key: "acknowledged", label: "Ditangani" },
  { key: "resolved", label: "Selesai" },
];
const SR_LIST = "/operations/service-requests";
/** Path server memakai pemisah " · "; LocationPath memakai " / ". */
const slashPath = (p: string) => p.replace(/ · /g, " / ");

export default function RecurringIssuesPage() {
  const { id } = useParams();
  const nav = useNavigate();
  const { search } = useLocation();
  const { propertyId } = useAuth();
  const f = useUrlFilters();
  const status = f.get("status");
  const list = useAll<RecurringIssue>("recurring-issues", { property_id: propertyId ?? undefined, status: status || undefined });
  const rows = list.data ?? [];
  const columns = useMemo<ColumnDef<RecurringIssue, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): lokasi = nama lokasi (path lengkap di tooltip), kategori, jumlah
    // permintaan satu baris, satu status + penanda keluhan, terakhir terlihat. Rincian waktu & catatan ada di drawer detail.
    { id: "location", header: "Lokasi", meta: { mobile: "primary" }, cell: ({ row: { original: r } }) => <CellText max={220} title={r.location_path} className="font-medium text-on-surface">{r.location_name}</CellText> },
    { id: "category", header: "Kategori", meta: { mobile: "secondary" }, cell: ({ row }) => <CellText max={180}>{row.original.category_name ?? row.original.category_code}</CellText> },
    { id: "count", header: "Permintaan", meta: { mobile: "secondary" }, size: 150, cell: ({ row: { original: r } }) => (
      <span className="whitespace-nowrap text-sm" title={r.open_count > 0 ? `${fmtNumber(r.open_count)} belum selesai` : "semua selesai"}><span className="font-semibold tnum">{fmtNumber(r.request_count)}×</span> <span className="text-on-surface-variant">/ {r.window_days} hari</span></span>
    ) },
    { id: "status", header: "Status", meta: { mobile: "status" }, size: 170, cell: ({ row: { original: r } }) => (
      <div className="flex flex-wrap items-center gap-1">
        <StatusBadge objectType="recurring_issue" status={r.status} />
        {r.complaint_count > 0 && <Badge tone="warning">{fmtNumber(r.complaint_count)} keluhan</Badge>}
      </div>
    ) },
    { id: "last_seen", header: "Terakhir", meta: { mobile: "hidden" }, size: 120, cell: ({ row }) => <span className="whitespace-nowrap text-sm text-on-surface-variant"><RelativeTime value={row.original.last_seen_at} /></span> },
  ], []);
  return (
    <div>
      <PageHeader title="Isu Berulang" subtitle="Lokasi dan kategori yang dilaporkan berulang kali dalam jendela waktu tertentu. Ambang & jendela diatur di Settings › Property Profile." />
      <div className="mb-3 flex flex-wrap items-center gap-1.5" role="group" aria-label="Status isu">
        {VIEWS.map((v) => <FilterChip key={v.key || "active"} selected={status === v.key} onClick={() => f.set({ status: v.key || null })}>{v.label}</FilterChip>)}
      </div>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/tenant-relation/recurring-issues/${r.id}${search}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={!!status && status !== "resolved"}
        empty={status === "resolved"
          ? { icon: "task_alt", title: "Belum ada isu berulang yang diselesaikan" }
          : { icon: "event_repeat", title: "Tidak ada isu berulang aktif", description: "Sinyal muncul otomatis saat permintaan pada lokasi & kategori yang sama mencapai ambang dalam jendela waktu." }}
        rowClassName={(r) => (r.id === id ? "bg-primary-soft" : r.status === "open" ? "border-l-4 border-l-critical" : undefined)}
      />
      {id && <RecurringIssueDrawer id={id} onClose={() => nav(`/tenant-relation/recurring-issues${search}`)} />}
    </div>
  );
}

function RecurringIssueDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const q = useOne<RecurringIssue>("recurring-issues", id);
  const srs = useList<ServiceRequest>("service-requests", { recurring_issue_id: id, sort: "-created_at" }, { limit: 20 });
  const [dlg, setDlg] = useState<"acknowledge" | "resolve" | null>(null);
  const r = q.data;
  const srRows = srs.data?.pages.flatMap((p) => p.data) ?? [];
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={r ? `Isu berulang · ${r.location_name}` : "Isu berulang"} description={r ? `${r.category_name ?? r.category_code} · ${r.location_path}` : undefined}>
        <AsyncState query={q} skeleton={<FormSkeleton fields={6} />}>
          {(r) => (
            <div className="space-y-5">
              <div className="flex flex-wrap items-center gap-2">
                <StatusBadge objectType="recurring_issue" status={r.status} />
                {r.complaint_count > 0 && <Badge tone="warning">{fmtNumber(r.complaint_count)} keluhan · prioritas dinaikkan</Badge>}
              </div>
              {r.status === "open" && <Alert variant="warning" title={`${fmtNumber(r.request_count)} permintaan dalam ${r.window_days} hari (ambang ${r.threshold})`}>Tinjau akar masalah di lokasi ini — mis. jadwalkan inspeksi atau Work Order perbaikan menyeluruh, lalu tandai Ditangani.</Alert>}
              <KeyValue items={[
                { label: "Lokasi", value: <LocationPath pathText={slashPath(r.location_path)} locationId={r.location_id} linkTo={(lid) => `/property/locations/${lid}`} /> },
                { label: "Kategori", value: r.category_name ?? r.category_code },
                { label: "Jumlah permintaan", value: `${fmtNumber(r.request_count)} (ambang ${r.threshold} dalam ${r.window_days} hari)` },
                { label: "Belum selesai", value: fmtNumber(r.open_count) },
                { label: "Keluhan", value: fmtNumber(r.complaint_count) },
                { label: "Pertama terlihat", value: fmtDateTime(r.first_seen_at) },
                { label: "Terakhir terlihat", value: <>{fmtDateTime(r.last_seen_at)} <span className="text-on-surface-variant">(<RelativeTime value={r.last_seen_at} />)</span></> },
                ...(r.acknowledged_at ? [{ label: "Ditangani", value: <>{fmtDateTime(r.acknowledged_at)}{r.acknowledged_by_name ? ` oleh ${r.acknowledged_by_name}` : ""}</> }] : []),
                ...(r.resolved_at ? [{ label: "Selesai", value: fmtDateTime(r.resolved_at) }] : []),
                ...(r.note ? [{ label: "Catatan", value: <span className="whitespace-pre-line">{r.note}</span> }] : []),
              ]} />
              <div>
                <div className="mb-2 flex items-center justify-between gap-2">
                  <div className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Permintaan terkait ({fmtNumber(r.service_request_ids.length)})</div>
                  <Link to={`${SR_LIST}?recurring_issue_id=${r.id}`} className="inline-flex items-center gap-0.5 text-sm font-semibold text-primary hover:underline">Buka di daftar<Icon name="chevron_right" size={14} aria-hidden /></Link>
                </div>
                {srs.isLoading ? (
                  <p className="text-sm text-on-surface-variant">Memuat permintaan…</p>
                ) : srs.isError ? (
                  <p className="text-sm text-on-surface-variant">Daftar permintaan tidak dapat dimuat.</p>
                ) : srRows.length === 0 ? (
                  <p className="text-sm text-on-surface-variant">Tidak ada permintaan yang dapat ditampilkan.</p>
                ) : (
                  <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
                    {srRows.map((s) => (
                      <li key={s.id}>
                        <Link to={`${SR_LIST}/${s.id}`} className="flex items-start justify-between gap-3 px-3 py-2 hover:bg-surface-container">
                          <span className="min-w-0">
                            <span className="font-mono text-[13px] font-semibold text-primary">{s.request_number}</span>
                            <span className="block truncate text-sm">{s.title}</span>
                            <span className="text-xs text-on-surface-variant">{fmtDate(s.created_at)}{s.request_type === "complaint" ? " · Keluhan" : ""}</span>
                          </span>
                          <span className="flex shrink-0 flex-col items-end gap-1"><StatusBadge objectType="service_request" status={s.status} /><PriorityBadge priority={s.priority} /></span>
                        </Link>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
              {(r.allowed_actions.includes("acknowledge") || r.allowed_actions.includes("resolve")) && (
                <DialogFooter className="flex-wrap">
                  {r.allowed_actions.includes("acknowledge") && <Button variant="secondary" icon="how_to_reg" onClick={() => setDlg("acknowledge")}>Tangani</Button>}
                  {r.allowed_actions.includes("resolve") && <Button icon="task_alt" onClick={() => setDlg("resolve")}>Selesaikan</Button>}
                </DialogFooter>
              )}
              {dlg && <ActDialog issue={r} action={dlg} onClose={() => setDlg(null)} />}
            </div>
          )}
        </AsyncState>
      </DialogContent>
    </Dialog>
  );
}

function ActDialog({ issue, action, onClose }: { issue: RecurringIssue; action: "acknowledge" | "resolve"; onClose: () => void }) {
  const toast = useToast();
  const [note, setNote] = useState(issue.note ?? "");
  const act = useAction<{ note: string }, RecurringIssue>(() => `recurring-issues/${issue.id}/${action}`, { invalidate: ["all", "list", "one", "tr-metrics"] });
  const submit = () => act.mutateAsync({ note: note.trim() }).then(() => { toast.action(action === "acknowledge" ? "acknowledged" : "resolved", "Isu berulang"); onClose(); }).catch((e) => toast.failed(action === "acknowledge" ? "acknowledged" : "resolved", e, "Isu berulang"));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        title={action === "acknowledge" ? "Tangani isu berulang" : "Selesaikan isu berulang"}
        description={action === "acknowledge" ? "Menandai isu sedang ditangani — sinyal tetap aktif sampai diselesaikan." : "Isu ditutup; laporan baru pada lokasi & kategori yang sama akan membuat sinyal baru bila kembali mencapai ambang."}
      >
        <Field label="Catatan" help="Opsional — mis. tindakan akar masalah yang diambil."><Textarea rows={3} value={note} onChange={(e) => setNote(e.target.value)} placeholder="mis. WO penggantian kompresor AC lantai 12 dijadwalkan 2 Okt" /></Field>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon={action === "acknowledge" ? "how_to_reg" : "task_alt"} loading={act.isPending} onClick={submit}>{action === "acknowledge" ? "Tandai ditangani" : "Selesaikan"}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
