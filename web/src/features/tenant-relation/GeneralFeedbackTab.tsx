// Feedback Umum tenant (PRD P3 v2.1 P3-FDB-02..03): daftar GET /tenant-relation/general-feedback (property_id, status, category)
// → drawer detail (identitas anonim disembunyikan server) → Tinjau · Tanggapi (tanggapan dikirim ke tenant sebagai notifikasi) · Tutup.
// Aksi mengikuti allowed_actions server (tenant_relation.feedback.respond).
import { useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { Alert, Badge, Button, Dialog, DialogContent, DialogFooter, Field, Textarea } from "@/components/ui/primitives";
import { DataGrid, useUrlFilters } from "@/components/bv/datagrid";
import { AsyncState, FormSkeleton, KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { AttachmentGrid } from "@/components/bv/checklist";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { useAction, useAttachments, useList, useOne } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusOptions } from "@/lib/status";
import { fmtDateTime } from "@/lib/format";
import { FilterRow, SelectFilter } from "@/features/security/shared";
import type { GeneralFeedback } from "./types";
import { FEEDBACK_CATEGORIES, FEEDBACK_CATEGORY_ICON, labelOf, optionsOf } from "./labels";

export function GeneralFeedbackTab({ selectedId }: { selectedId?: string }) {
  const { propertyId } = useAuth();
  const nav = useNavigate();
  const { search } = useLocation();
  const f = useUrlFilters();
  const status = f.get("status");
  const category = f.get("category");
  const list = useList<GeneralFeedback>("tenant-relation/general-feedback", { property_id: propertyId ?? undefined, status: status || undefined, category: category || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<GeneralFeedback, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): nomor + subjek dalam satu kolom, kategori, pengirim satu baris
    // (tenant · unit di tooltip), status, waktu kirim. Isi feedback & foto ada di drawer detail.
    { id: "feedback", header: "Feedback", meta: { mobile: "primary" }, cell: ({ row: { original: x } }) => <CellTitle code={x.feedback_number} title={x.subject || labelOf(FEEDBACK_CATEGORIES, x.category)} /> },
    { id: "category", header: "Kategori", meta: { mobile: "secondary" }, size: 130, cell: ({ row }) => <span className="inline-flex items-center gap-1.5 whitespace-nowrap text-sm"><Icon name={FEEDBACK_CATEGORY_ICON[row.original.category] ?? "chat_bubble"} size={16} className="shrink-0 text-on-surface-variant" aria-hidden />{labelOf(FEEDBACK_CATEGORIES, row.original.category)}</span> },
    { id: "sender", header: "Pengirim", meta: { mobile: "secondary" }, cell: ({ row }) => <Sender f={row.original} /> },
    { id: "status", header: "Status", meta: { mobile: "status" }, size: 130, cell: ({ row }) => <StatusBadge objectType="tenant_feedback" status={row.original.status} /> },
    { id: "created_at", header: "Dikirim", meta: { mobile: "hidden" }, size: 120, cell: ({ row }) => <span className="whitespace-nowrap text-sm text-on-surface-variant"><RelativeTime value={row.original.created_at} /></span> },
  ], []);
  const close = () => nav(`/tenant-relation/feedback/general${search}`);
  return (
    <div className="space-y-3">
      <FilterRow className="mb-0">
        <SelectFilter label="Status" value={status} onChange={(v) => f.set({ status: v })} options={statusOptions("tenant_feedback")} />
        <SelectFilter label="Kategori" value={category} onChange={(v) => f.set({ category: v })} options={optionsOf(FEEDBACK_CATEGORIES)} />
        {(status || category) && <Button variant="ghost" size="sm" icon="replay" onClick={() => f.set({ status: null, category: null })}>Reset filter</Button>}
      </FilterRow>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/tenant-relation/feedback/general/${r.id}${search}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={!!status || !!category}
        empty={{ icon: "reviews", title: "Belum ada feedback umum", description: "Saran, pujian, keluhan, atau pertanyaan yang dikirim tenant dari Tenant App tampil di sini." }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowClassName={(r) => (r.id === selectedId ? "bg-primary-soft" : r.status === "new" ? "font-semibold" : undefined)}
      />
      {selectedId && <FeedbackDrawer id={selectedId} onClose={close} />}
    </div>
  );
}

function Sender({ f }: { f: GeneralFeedback }) {
  if (f.is_anonymous) return <span className="inline-flex items-center gap-1 whitespace-nowrap text-sm text-on-surface-variant"><Icon name="visibility_off" size={14} aria-hidden />Anonim</span>;
  const where = [f.tenant_name, f.unit_label].filter(Boolean).join(" · ");
  return <CellText max={180} title={[f.sender_name, where].filter(Boolean).join(" · ") || undefined}>{f.sender_name ?? f.tenant_name ?? "—"}</CellText>;
}

type FbAction = "review" | "respond" | "close";

function FeedbackDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const toast = useToast();
  const q = useOne<GeneralFeedback>("tenant-relation/general-feedback", id);
  const photos = useAttachments("tenant_feedback", id);
  const [respondOpen, setRespondOpen] = useState(false);
  const act = useAction<{ action: FbAction; response?: string }, GeneralFeedback>((i) => `tenant-relation/general-feedback/${id}/${i.action}`, { body: (i) => (i.response ? { response: i.response } : {}), invalidate: ["list", "one", "tr-metrics"] });
  const run = (action: FbAction, response?: string) =>
    act.mutateAsync({ action, response }).then((r) => {
      toast.success(action === "review" ? `${r.feedback_number} ditandai sedang ditinjau` : action === "respond" ? `Tanggapan ${r.feedback_number} terkirim ke tenant` : `${r.feedback_number} ditutup`);
      setRespondOpen(false);
    }).catch((e) => toast.failed("updated", e, "Feedback"));
  const x = q.data;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={x ? `Feedback ${x.feedback_number}` : "Feedback umum"} description={x ? `${labelOf(FEEDBACK_CATEGORIES, x.category)} · ${x.property_name}` : undefined}>
        <AsyncState query={q} skeleton={<FormSkeleton fields={5} />}>
          {(x) => (
            <div className="space-y-5">
              <div className="flex flex-wrap items-center gap-2">
                <StatusBadge objectType="tenant_feedback" status={x.status} />
                <Badge><Icon name={FEEDBACK_CATEGORY_ICON[x.category] ?? "chat_bubble"} size={12} aria-hidden />{labelOf(FEEDBACK_CATEGORIES, x.category)}</Badge>
                {x.is_anonymous && <Badge tone="neutral"><Icon name="visibility_off" size={12} aria-hidden />Anonim</Badge>}
              </div>
              <div>
                {x.subject && <div className="text-h3 font-bold text-on-surface">{x.subject}</div>}
                <p className="mt-1 whitespace-pre-line text-body">{x.body}</p>
              </div>
              {x.is_anonymous && <Alert variant="info">Tenant mengirim feedback ini secara anonim — nama, tenant, dan unit pengirim tidak ditampilkan. Tanggapan tetap terkirim ke pengirim.</Alert>}
              <KeyValue items={[
                ...(!x.is_anonymous ? [
                  { label: "Pengirim", value: x.sender_name ?? "—" },
                  { label: "Tenant", value: x.tenant_name ?? "—" },
                  { label: "Unit", value: x.unit_label ?? "—" },
                ] : []),
                { label: "Dikirim", value: <>{fmtDateTime(x.created_at)} <span className="text-on-surface-variant">(<RelativeTime value={x.created_at} />)</span></> },
                { label: "Property", value: x.property_name },
              ]} />
              {x.photo_count > 0 && (
                <div>
                  <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Foto dari tenant ({x.photo_count})</div>
                  {photos.isLoading ? <p className="text-sm text-on-surface-variant">Memuat foto…</p> : photos.isError ? <p className="text-sm text-on-surface-variant">Foto tidak dapat dimuat.</p> : <AttachmentGrid items={photos.data ?? []} emptyLabel="Foto tidak tersedia." />}
                </div>
              )}
              <div>
                <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Tanggapan ke tenant</div>
                {x.response ? (
                  <div className="rounded-[var(--radius-md)] bg-primary-soft px-3 py-2">
                    <p className="whitespace-pre-line text-body">{x.response}</p>
                    <p className="mt-1 text-xs text-on-surface-variant">{x.responded_by_name ?? "Building Management"} · {fmtDateTime(x.responded_at)}</p>
                  </div>
                ) : (
                  <p className="text-sm text-on-surface-variant">Belum ditanggapi.</p>
                )}
              </div>
              {(x.allowed_actions.includes("review") || x.allowed_actions.includes("respond") || x.allowed_actions.includes("close")) && (
                <DialogFooter className="flex-wrap">
                  {x.allowed_actions.includes("close") && <Button variant="secondary" icon="task_alt" className="mr-auto" loading={act.isPending && act.variables?.action === "close"} onClick={() => run("close")}>Tutup</Button>}
                  {x.allowed_actions.includes("review") && <Button variant="secondary" icon="visibility" loading={act.isPending && act.variables?.action === "review"} onClick={() => run("review")}>Tandai ditinjau</Button>}
                  {x.allowed_actions.includes("respond") && <Button icon="send" onClick={() => setRespondOpen(true)}>{x.response ? "Perbarui tanggapan" : "Tanggapi"}</Button>}
                </DialogFooter>
              )}
              {respondOpen && <RespondDialog feedback={x} loading={act.isPending} onClose={() => setRespondOpen(false)} onSubmit={(text) => run("respond", text)} />}
            </div>
          )}
        </AsyncState>
      </DialogContent>
    </Dialog>
  );
}

function RespondDialog({ feedback, loading, onClose, onSubmit }: { feedback: GeneralFeedback; loading: boolean; onClose: () => void; onSubmit: (text: string) => void }) {
  const [text, setText] = useState(feedback.response ?? "");
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Tanggapi ${feedback.feedback_number}`} description="Tanggapan dikirim ke tenant sebagai notifikasi Tenant App dan status feedback menjadi Ditanggapi.">
        <Field label="Tanggapan" required><Textarea rows={5} autoFocus value={text} onChange={(e) => setText(e.target.value)} maxLength={2000} placeholder="mis. Terima kasih atas masukannya. Jadwal pembersihan lobby kami tambah menjadi dua kali sehari mulai pekan depan." /></Field>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="send" disabled={!text.trim()} loading={loading} onClick={() => onSubmit(text.trim())}>Kirim tanggapan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
