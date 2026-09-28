// Billing › Credit Notes (PRD P4 v2.1 P4-INV-07; D-P4-08 pemisahan wewenang): koreksi invoice terbit tanpa mengedit invoice.
// Diajukan dari detail invoice (billing.credit_notes.create); Finance Manager menyetujui/menolak (billing.credit_notes.approve,
// penolakan wajib catatan); pengaju dapat membatalkan selama menunggu. Detail = drawer /billing/credit-notes/:id.
import { useMemo, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Button, Dialog, DialogContent, DialogFooter, Drawer, Field, Textarea } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { AsyncState, DetailSkeleton, KeyValue, ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { useInvalidate, useList, useOne } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import { statusOptions } from "@/lib/status";
import { SummaryTile } from "./shared";
import { money, type CreditNote, type Invoice } from "./types";

export default function CreditNotesPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const location = useLocation();
  const { propertyId } = useAuth();
  const f = useUrlFilters();
  const list = useList<CreditNote>("billing/credit-notes", { property_id: propertyId ?? undefined, status: f.get("status") || undefined, invoice_id: f.get("invoice_id") || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];

  const columns = useMemo<ColumnDef<CreditNote, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): nomor + tenant satu kolom, alasan satu baris, satu badge status.
    // Unit, pengaju & pemutus ada di drawer detail.
    { id: "number", header: "Credit note", meta: { mobile: "primary" }, cell: ({ row: { original: c } }) => <CellTitle code={c.credit_note_number ?? "Pengajuan"} title={c.tenant_name ?? "Tanpa tenant"} /> },
    { id: "invoice", header: "Invoice", size: 150, meta: { mobile: "secondary" }, cell: ({ row: { original: c } }) => <Link to={`/billing/invoices/${c.invoice_id}`} onClick={(e) => e.stopPropagation()} className="whitespace-nowrap font-mono text-[13px] text-primary hover:underline">{c.invoice_number ?? "—"}</Link> },
    { id: "amount", header: "Nominal", size: 140, meta: { mobile: "secondary" }, cell: ({ row: { original: c } }) => <div className="tnum whitespace-nowrap text-right font-semibold">{money(c.amount)}</div> },
    { id: "reason", header: "Alasan", meta: { mobile: "hidden" }, cell: ({ row: { original: c } }) => <CellText max={240}>{c.reason}</CellText> },
    { id: "requested", header: "Diajukan", size: 120, meta: { mobile: "secondary" }, cell: ({ row: { original: c } }) => <span className="whitespace-nowrap text-sm text-on-surface-variant" title={c.requested_by_name ?? undefined}><RelativeTime value={c.requested_at} /></span> },
    { id: "status", header: t("label.status"), size: 130, meta: { mobile: "status" }, cell: ({ row: { original: c } }) => <StatusBadge objectType="credit_note" status={c.status} /> },
  ], [t]);

  return (
    <div>
      <PageHeader title={t("nav.credit_notes")} subtitle="Koreksi invoice terbit yang sudah dibayar sebagian/penuh — diajukan dari detail invoice, berlaku setelah disetujui. Kelebihan bayar setelah koreksi menjadi saldo kredit tenant.">
        <FilterBar
          spec={{
            status: statusOptions("credit_note"),
            presets: [
              { key: "all", label: "Semua", params: {} },
              { key: "pending", label: "Menunggu persetujuan", params: { status: "pending" } },
            ],
          }}
        />
      </PageHeader>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/billing/credit-notes/${r.id}${location.search}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={f.isFiltered}
        empty={{ icon: "receipt", title: "Belum ada credit note", description: "Credit note diajukan dari detail invoice terbit (aksi \"Ajukan credit note\")." }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
      />
      {id && <CreditNoteDrawer id={id} onClose={() => nav({ pathname: "/billing/credit-notes", search: location.search })} />}
    </div>
  );
}

function CreditNoteDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const q = useOne<CreditNote>("billing/credit-notes", id);
  return (
    <Drawer open onClose={onClose} title="Credit note" width={640}>
      <AsyncState query={q} skeleton={<DetailSkeleton />}>{(c) => <CreditNoteDetail c={c} />}</AsyncState>
    </Drawer>
  );
}

function CreditNoteDetail({ c }: { c: CreditNote }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const inv = useOne<Invoice>("invoices", c.invoice_id);
  const [dialog, setDialog] = useState<null | "approve" | "reject" | "cancel">(null);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const has = (a: string) => c.allowed_actions.includes(a);
  const decide = async (action: "approve" | "reject" | "cancel", n: string) => {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    try {
      const out = await api<CreditNote>(`billing/credit-notes/${c.id}/${action}`, { body: n ? { note: n } : {}, idempotencyKey: uuid() });
      invalidate("list", "one", "all");
      if (action === "approve") toast.success(`Credit note ${out.credit_note_number ?? ""} disetujui — invoice ${c.invoice_number ?? ""} dikoreksi ${money(c.amount)}`);
      else toast.action(action === "reject" ? "rejected" : "cancelled", "Credit note");
      setDialog(null);
      setNote("");
    } catch (e) {
      toast.error(e);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  };
  const v = inv.data;
  const overflow = v ? Math.max(0, v.paid_amount + v.credited_amount + c.amount - v.total_amount) : 0;
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-h2 font-bold">{c.credit_note_number ?? "Pengajuan credit note"}</span>
            <StatusBadge objectType="credit_note" status={c.status} />
          </div>
          <div className="mt-0.5 text-sm text-on-surface-variant">{c.tenant_name ?? "Tanpa tenant"}{c.unit_label ? ` · ${c.unit_label}` : ""} · Invoice <Link className="font-mono text-primary hover:underline" to={`/billing/invoices/${c.invoice_id}`}>{c.invoice_number}</Link></div>
        </div>
        <div className="tnum text-h2 font-bold">{money(c.amount)}</div>
      </div>
      {(has("approve") || has("reject") || has("cancel")) && (
        <div className="flex flex-wrap gap-2">
          {has("approve") && <Button icon="check_circle" onClick={() => setDialog("approve")}>Setujui</Button>}
          {has("reject") && <Button variant="secondary" icon="cancel" onClick={() => setDialog("reject")}>Tolak…</Button>}
          {has("cancel") && <Button variant="ghost" icon="undo" onClick={() => setDialog("cancel")}>Batalkan pengajuan</Button>}
        </div>
      )}
      <Alert variant="info" title="Alasan">{c.reason}</Alert>
      {c.decision_note && <Alert variant={c.status === "rejected" ? "critical" : "success"} title="Catatan keputusan">{c.decision_note}</Alert>}
      {v && (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          <SummaryTile label="Total invoice" value={money(v.total_amount)} />
          <SummaryTile label="Dibayar" value={money(v.paid_amount)} />
          <SummaryTile label="Sudah dikoreksi" value={money(v.credited_amount)} />
          <SummaryTile label="Sisa invoice" value={money(["issued", "partially_paid", "overdue"].includes(v.status) ? v.outstanding_amount : 0)} sub={<StatusBadge objectType="invoice" status={v.status} />} />
        </div>
      )}
      <KeyValue items={[
        { label: "Diajukan", value: <>{c.requested_by_name ?? "—"} · {fmtDateTime(c.requested_at)}</> },
        { label: "Diputuskan", value: c.decided_at ? <>{c.decided_by_name ?? "—"} · {fmtDateTime(c.decided_at)}</> : "—" },
      ]} />

      {dialog === "approve" && (
        <Dialog open onOpenChange={(o) => !o && setDialog(null)}>
          <DialogContent title="Setujui credit note?" description={`Invoice ${c.invoice_number ?? ""} dikoreksi ${money(c.amount)} dan nomor CN diberikan. Tindakan tercatat di audit.`}>
            {c.status === "pending" && overflow > 0 && <Alert variant="info" className="mb-3">Pembayaran akan melebihi tagihan bersih sebesar {money(overflow)} — kelebihan menjadi saldo kredit tenant.</Alert>}
            <Field label="Catatan (opsional)"><Textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} /></Field>
            <DialogFooter>
              <Button variant="secondary" onClick={() => setDialog(null)}>Batal</Button>
              <Button icon="check_circle" loading={busy} onClick={() => decide("approve", note.trim())}>Setujui</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
      <ReasonDialog open={dialog === "reject"} onOpenChange={(o) => !o && setDialog(null)} title="Tolak credit note" label="Catatan penolakan" description="Catatan wajib dan terlihat oleh pengaju." confirmLabel="Tolak" destructive loading={busy} onConfirm={(n) => decide("reject", n)} />
      {dialog === "cancel" && (
        <Dialog open onOpenChange={(o) => !o && setDialog(null)}>
          <DialogContent title="Batalkan pengajuan credit note?">
            <Field label="Catatan (opsional)"><Textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} /></Field>
            <DialogFooter>
              <Button variant="secondary" onClick={() => setDialog(null)}>Kembali</Button>
              <Button variant="destructive" loading={busy} onClick={() => decide("cancel", note.trim())}>Batalkan pengajuan</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
