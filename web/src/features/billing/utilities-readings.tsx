// Utilities › Pembacaan (PRD P4 v2.1 P4-UTL-02..03, P4-UTL-05): pembacaan per periode (YYYY-MM) dari Web / Staff App, status
// Tercatat · Perlu Dicek (angka mundur / lonjakan) · Disetujui · Ditolak, review setuju/tolak (tolak wajib catatan; pembacaan
// yang sudah ditagihkan tidak dapat ditolak), detail + foto meter. Deep link notifikasi: /billing/meters/readings/:id.
import { useMemo, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { FilterChip } from "@buildingvision/ui/bv";
import { Alert, Badge, Button, DatePicker, Dialog, DialogContent, DialogFooter, Drawer, Field, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, KeyValue, ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellTitle } from "@/components/bv/cells";
import { AttachmentGrid, PhotoEvidenceUploader } from "@/components/bv/checklist";
import { DetailSkeleton } from "@/components/bv/states";
import { useAttachments, useInvalidate, useList, useOne } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime, fmtNumber } from "@/lib/format";
import { statusOptions } from "@/lib/status";
import { PropertySelect } from "@/features/finance/fin-ui";
import { currentPeriod, fmtQty, periodLabel, shiftPeriod, usePropertyParam } from "@/features/finance/fin-utils";
import { ANOMALY, ANOMALY_HELP, METER_TYPES, SOURCE, meterTypeLabel, type Reading } from "./utilities-model";

export function ReadingsTab({ selectedId }: { selectedId?: string }) {
  const nav = useNavigate();
  const { search } = useLocation();
  const toast = useToast();
  const invalidate = useInvalidate();
  const [sp, setSp] = useSearchParams();
  const [pid, setPid] = usePropertyParam();
  // ?period= kosong = semua periode; tanpa parameter = periode berjalan
  const period = sp.has("period") ? sp.get("period") ?? "" : currentPeriod();
  const status = sp.get("status") ?? "";
  const meterType = sp.get("meter_type") ?? "";
  const meterId = sp.get("meter_id") ?? "";
  const set = (patch: Record<string, string | null>) => {
    const n = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (v === null) n.delete(k);
      else n.set(k, v);
    }
    setSp(n, { replace: true });
  };
  const list = useList<Reading>("billing/meter-readings", { property_id: pid ?? undefined, period: period || undefined, status: status || undefined, meter_type: meterType || undefined, meter_id: meterId || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [rejectFor, setRejectFor] = useState<Reading | null>(null);
  const [busy, setBusy] = useState(false);
  const review = async (r: Reading, action: "approve" | "reject", note?: string) => {
    await api<Reading>(`billing/meter-readings/${r.id}/${action}`, { body: note ? { note } : {}, idempotencyKey: uuid() });
  };
  const approveOne = (r: Reading) => review(r, "approve").then(() => { invalidate("list", "one", "dashboard"); toast.action("approved", `Pembacaan ${r.meter_number}`); }).catch((e) => toast.failed("approved", e, "Pembacaan"));
  const approveMany = async (ids: string[], clear: () => void) => {
    const targets = rows.filter((r) => ids.includes(r.id) && r.allowed_actions.includes("approve"));
    if (!targets.length) {
      toast.info("Tidak ada pembacaan terpilih yang dapat disetujui.");
      return;
    }
    setBusy(true);
    let ok = 0;
    for (const r of targets) {
      try {
        await review(r, "approve");
        ok++;
      } catch (e) {
        toast.failed("approved", e, `Pembacaan ${r.meter_number}`);
      }
    }
    setBusy(false);
    clear();
    invalidate("list", "one", "dashboard");
    if (ok) toast.success(`${fmtNumber(ok)} pembacaan disetujui`);
  };
  const columns = useMemo<ColumnDef<Reading, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): nomor meter di atas unit/lokasi, angka & pemakaian satu baris (angka sebelumnya di
    // tooltip), waktu baca (pencatat & sumber di tooltip), status + satu flag terpenting (anomali, lalu "Ditagihkan").
    // Foto, catatan & riwayat review ada di drawer detail.
    { id: "meter", header: "Meter", meta: { mobile: "primary" }, cell: ({ row: { original: r } }) => <CellTitle code={r.meter_number} title={r.unit_number ? `Unit ${r.unit_number}` : r.location_name} /> },
    { id: "period", header: "Periode", size: 140, meta: { mobile: "hidden" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm">{periodLabel(row.original.period)}</span> },
    { id: "value", header: "Angka", size: 130, meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <span className="block whitespace-nowrap text-right tnum font-semibold" title={`Sebelumnya ${fmtQty(r.previous_value)}`}>{fmtQty(r.reading_value)}</span> },
    { id: "usage", header: "Pemakaian", size: 130, meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <span className="block whitespace-nowrap text-right tnum text-sm font-semibold">{r.usage !== null ? `${fmtQty(r.usage)} ${r.unit_label}` : "—"}</span> },
    { id: "read_at", header: "Dibaca", size: 140, meta: { mobile: "hidden" }, cell: ({ row: { original: r } }) => <span className="whitespace-nowrap text-sm" title={`${r.recorded_by_name ?? "—"} · ${SOURCE[r.source] ?? r.source}`}><RelativeTime value={r.read_at} /></span> },
    {
      id: "status", header: "Status", size: 200, meta: { mobile: "status" },
      cell: ({ row: { original: r } }) => (
        <div className="flex flex-wrap items-center gap-1">
          <StatusBadge objectType="meter_reading" status={r.status} />
          {r.anomaly ? <Badge tone="warning">{ANOMALY[r.anomaly] ?? r.anomaly}</Badge> : r.billed ? <Badge tone="info">Ditagihkan</Badge> : null}
        </div>
      ),
    },
  ], []);
  const filtered = !!status || !!meterType || !!meterId || period !== currentPeriod();
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <PropertySelect value={pid} onChange={setPid} allowAll />
        <span className="inline-flex items-center gap-1">
          <Button size="icon-sm" variant="ghost" icon="chevron_left" aria-label="Periode sebelumnya" disabled={!period} onClick={() => set({ period: shiftPeriod(period, -1) })} />
          <DatePicker type="month" className="w-40" value={period} onChange={(v) => set({ period: v })} aria-label="Periode" />
          <Button size="icon-sm" variant="ghost" icon="chevron_right" aria-label="Periode berikutnya" disabled={!period} onClick={() => set({ period: shiftPeriod(period, 1) })} />
        </span>
        <FilterChip selected={!period} onClick={() => set({ period: period ? "" : null })}>Semua periode</FilterChip>
        <NativeSelect className="w-[calc(50%-4px)] sm:w-36" value={meterType} onChange={(e) => set({ meter_type: e.target.value || null })} aria-label="Jenis meter">
          <option value="">Jenis: Semua</option>
          {Object.entries(METER_TYPES).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}
        </NativeSelect>
        {meterId && <Button size="sm" variant="secondary" icon="close" onClick={() => set({ meter_id: null })}>Satu meter</Button>}
        {filtered && <Button variant="ghost" size="sm" icon="replay" onClick={() => set({ period: null, status: null, meter_type: null, meter_id: null })}>Reset filter</Button>}
      </div>
      <span className="flex flex-wrap gap-1.5">
        <FilterChip selected={!status} onClick={() => set({ status: null })}>Semua status</FilterChip>
        {statusOptions("meter_reading").map((o) => <FilterChip key={o.value} selected={status === o.value} onClick={() => set({ status: status === o.value ? null : o.value })}>{o.label}</FilterChip>)}
      </span>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/billing/meters/readings/${r.id}${search}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={filtered}
        empty={{ icon: "electric_meter", title: period ? `Belum ada pembacaan ${periodLabel(period)}` : "Belum ada pembacaan", description: "Pembacaan dicatat dari Staff App (rute pencatatan, offline) atau dari tab Meter." }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        selectable
        bulkActions={(ids, clear) => <Button size="sm" icon="done_all" loading={busy} onClick={() => void approveMany(ids, clear)}>Setujui terpilih</Button>}
        rowActions={(r) => [
          ...(r.allowed_actions.includes("approve") ? [{ label: "Setujui", icon: "task_alt", onSelect: () => void approveOne(r) }] : []),
          ...(r.allowed_actions.includes("reject") ? [{ label: "Tolak…", icon: "block", destructive: true, onSelect: () => setRejectFor(r) }] : []),
          { label: "Detail & foto", icon: "visibility", onSelect: () => nav(`/billing/meters/readings/${r.id}${search}`) },
        ]}
        rowClassName={(r) => (r.id === selectedId ? "bg-primary-soft" : r.status === "flagged" ? "border-l-4 border-l-warning" : undefined)}
      />
      {rejectFor && <RejectDialog reading={rejectFor} onClose={() => setRejectFor(null)} />}
      {selectedId && <ReadingDrawer id={selectedId} onClose={() => nav(`/billing/meters/readings${search}`)} />}
    </div>
  );
}

function RejectDialog({ reading, onClose }: { reading: Reading; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [busy, setBusy] = useState(false);
  return (
    <ReasonDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`Tolak pembacaan ${reading.meter_number}`}
      label="Alasan penolakan"
      description="Pembacaan yang ditolak tidak dipakai sebagai angka sebelumnya maupun dasar tagihan; catat ulang angka yang benar."
      confirmLabel="Tolak"
      destructive
      loading={busy}
      onConfirm={(note) => {
        setBusy(true);
        api<Reading>(`billing/meter-readings/${reading.id}/reject`, { body: { note }, idempotencyKey: uuid() })
          .then(() => { invalidate("list", "one", "dashboard"); toast.action("rejected", `Pembacaan ${reading.meter_number}`); onClose(); })
          .catch((e) => toast.failed("rejected", e, "Pembacaan"))
          .finally(() => setBusy(false));
      }}
    />
  );
}

function ReadingDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { can } = useAuth();
  const q = useOne<Reading>("billing/meter-readings", id);
  const photos = useAttachments("meter_reading", id);
  const [approveOpen, setApproveOpen] = useState(false);
  const [note, setNote] = useState("");
  const [rejectOpen, setRejectOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const approve = () => {
    setBusy(true);
    api<Reading>(`billing/meter-readings/${id}/approve`, { body: note.trim() ? { note: note.trim() } : {}, idempotencyKey: uuid() })
      .then((r) => { invalidate("list", "one", "dashboard"); toast.action("approved", `Pembacaan ${r.meter_number}`); setApproveOpen(false); setNote(""); })
      .catch((e) => toast.failed("approved", e, "Pembacaan"))
      .finally(() => setBusy(false));
  };
  const r = q.data;
  return (
    <Drawer open onClose={onClose} title={r ? `Pembacaan ${r.meter_number}` : "Pembacaan meter"} description={r ? `${meterTypeLabel(r.meter_type)} · ${r.unit_number ? `Unit ${r.unit_number}` : r.location_name} · ${periodLabel(r.period)}` : undefined} width={640}>
      <AsyncState query={q} skeleton={<DetailSkeleton />}>
        {(r) => (
          <div className="space-y-5" data-testid="reading-drawer">
            <div className="flex flex-wrap items-center gap-2">
              <StatusBadge objectType="meter_reading" status={r.status} />
              {r.anomaly && <Badge tone="warning">{ANOMALY[r.anomaly] ?? r.anomaly}</Badge>}
              {r.billed && <Badge tone="info">Sudah ditagihkan</Badge>}
            </div>
            {r.anomaly && r.status === "flagged" && <Alert variant="warning" title={ANOMALY[r.anomaly] ?? "Perlu dicek"}>{ANOMALY_HELP[r.anomaly] ?? "Periksa angka & foto meter sebelum menyetujui."}</Alert>}
            {r.billed && <Alert variant="info">Pembacaan ini sudah menjadi item invoice utilitas — tidak dapat ditolak. Koreksi lewat credit note pada invoice terkait.</Alert>}
            <div className="grid grid-cols-3 gap-3 rounded-[var(--radius-lg)] border border-border p-3 text-center">
              <div><div className="text-xs text-on-surface-variant">Sebelumnya</div><div className="tnum text-h3 font-bold">{fmtQty(r.previous_value)}</div></div>
              <div><div className="text-xs text-on-surface-variant">Angka baca</div><div className="tnum text-h3 font-bold">{fmtQty(r.reading_value)}</div></div>
              <div><div className="text-xs text-on-surface-variant">Pemakaian</div><div className="tnum text-h3 font-bold">{r.usage !== null ? fmtQty(r.usage) : "—"}<span className="ml-1 text-xs font-normal text-on-surface-variant">{r.unit_label}</span></div></div>
            </div>
            <KeyValue items={[
              { label: "Waktu baca", value: fmtDateTime(r.read_at) },
              { label: "Dicatat", value: <>{r.recorded_by_name ?? "—"} · {SOURCE[r.source] ?? r.source}</> },
              { label: "Catatan", value: r.notes ?? "—" },
              ...(r.reviewed_at ? [{ label: "Direview", value: <>{r.reviewed_by_name ?? "—"} · {fmtDateTime(r.reviewed_at)}{r.review_note && <span className="block whitespace-pre-line text-on-surface-variant">{r.review_note}</span>}</> }] : []),
            ]} />
            <div>
              <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Foto meter ({(photos.data ?? []).length || r.photo_count})</div>
              <div className="space-y-2">
                {can("billing.meter_readings.create", r.property_id) && r.status !== "rejected" && <PhotoEvidenceUploader objectType="meter_reading" objectId={r.id} attachmentType="photo" compact label="Unggah foto meter" onUploaded={() => { void photos.refetch(); void q.refetch(); }} />}
                {photos.isError && r.photo_url ? <a href={r.photo_url} target="_blank" rel="noreferrer"><img src={r.photo_url} alt={`Foto meter ${r.meter_number}`} className="max-h-72 rounded-[var(--radius-md)] border border-border object-contain" /></a> : <AttachmentGrid items={photos.data ?? []} emptyLabel="Belum ada foto meter." />}
              </div>
            </div>
            {(r.allowed_actions.includes("approve") || r.allowed_actions.includes("reject")) && (
              <DialogFooter className="flex-wrap">
                {r.allowed_actions.includes("reject") && <Button variant="secondary" icon="block" onClick={() => setRejectOpen(true)}>Tolak…</Button>}
                {r.allowed_actions.includes("approve") && <Button icon="task_alt" onClick={() => setApproveOpen(true)}>Setujui</Button>}
              </DialogFooter>
            )}
            {approveOpen && (
              <Dialog open onOpenChange={(o) => !o && setApproveOpen(false)}>
                <DialogContent title={`Setujui pembacaan ${r.meter_number}`} description={r.status === "flagged" ? "Menyetujui pembacaan yang ditandai: pemakaian ini dipakai untuk tagihan periode." : undefined}>
                  <Field label="Catatan review (opsional)"><Textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} placeholder={r.anomaly === "rollback" ? "mis. meter diganti, angka reset" : "mis. sesuai foto"} /></Field>
                  <DialogFooter><Button variant="secondary" onClick={() => setApproveOpen(false)}>Batal</Button><Button loading={busy} onClick={approve}>Setujui</Button></DialogFooter>
                </DialogContent>
              </Dialog>
            )}
            {rejectOpen && <RejectDialog reading={r} onClose={() => setRejectOpen(false)} />}
          </div>
        )}
      </AsyncState>
    </Drawer>
  );
}

