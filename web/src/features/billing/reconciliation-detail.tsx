// Rekonsiliasi › detail impor (PRD P4 v2.1 P4-REC-02..03): baris mutasi dengan status (belum cocok / ada saran / cocok /
// diabaikan), saran server (skor 0–100 + selisih), konfirmasi satu per satu atau massal skor ≥ 90, cocokkan manual, abaikan
// dengan alasan (bunga bank, transfer internal), pulihkan. Setiap aksi mengembalikan impor terbaru (dipakai langsung).
import { useMemo, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { Alert, Badge, Button, Card, CardContent, ConfirmDialog } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { DetailSkeleton } from "@/components/bv/states";
import { useInvalidate, useOne } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime, fmtMoney, fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import { StatTile } from "@/features/finance/fin-ui";
import { fmtDay, fmtMoneyTile, fmtSigned } from "@/features/finance/fin-utils";
import { MatchDialog } from "./reconciliation-match";
import { LINE_FILTERS, MATCH_TYPE, confirmableCount, differenceText, matchProgress, parseDetected, scoreTone, type StatementImport, type StmtLine } from "./reconciliation-model";

export function ImportDetail({ id }: { id: string }) {
  const q = useOne<StatementImport>("billing/bank-statements", id);
  return (
    <AsyncState query={q} skeleton={<DetailSkeleton />}>
      {(imp) => <ImportView imp={imp} />}
    </AsyncState>
  );
}

function ImportView({ imp }: { imp: StatementImport }) {
  const toast = useToast();
  const qc = useQueryClient();
  const invalidate = useInvalidate();
  const loc = useLocation();
  const nav = useNavigate();
  const { can } = useAuth();
  const canManage = can("billing.reconciliation.manage", imp.property_id);
  const fresh = loc.state as { detected_columns?: string[]; duplicates_skipped?: number } | null;
  const lines = useMemo(() => imp.lines ?? [], [imp.lines]);
  const [filter, setFilter] = useState(() => (lines.some((l) => l.status === "suggested" || l.status === "unmatched") ? "open" : "all"));
  const [matchFor, setMatchFor] = useState<StmtLine | null>(null);
  const [ignoreFor, setIgnoreFor] = useState<StmtLine | null>(null);
  const [bulk, setBulk] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const bulkN = confirmableCount(lines);
  const apply = (out: StatementImport) => {
    qc.setQueryData(["one", "billing/bank-statements", imp.id], out);
    invalidate("list", "all", "overview", "dashboard");
  };
  const run = async (key: string, fn: () => Promise<StatementImport>, done: string) => {
    setBusy(key);
    try {
      apply(await fn());
      toast.success(done);
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(null);
    }
  };
  const confirmLine = (l: StmtLine) => run(l.id, () => api<StatementImport>(`billing/bank-statement-lines/${l.id}/match`, { body: {}, idempotencyKey: uuid() }), `Saran dikonfirmasi — pembayaran ${fmtMoney(l.amount)} tercatat`);
  const restoreLine = (l: StmtLine) => run(l.id, () => api<StatementImport>(`billing/bank-statement-lines/${l.id}/restore`, { body: {}, idempotencyKey: uuid() }), "Mutasi dipulihkan & dicocokkan ulang");
  const confirmBulk = async () => {
    setBulk(false);
    setBusy("bulk");
    try {
      const out = await api<{ confirmed: number; import: StatementImport }>(`billing/bank-statements/${imp.id}/confirm-suggestions`, { body: { min_score: 90 }, idempotencyKey: uuid() });
      apply(out.import);
      toast.success(`${fmtNumber(out.confirmed)} saran dikonfirmasi`);
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(null);
    }
  };
  const active = LINE_FILTERS.find((f) => f.key === filter) ?? LINE_FILTERS[LINE_FILTERS.length - 1];
  const rows = useMemo(() => lines.filter(active.test), [lines, active]);
  const progress = matchProgress(imp);
  const detected = parseDetected(fresh?.detected_columns);
  const columns: ColumnDef<StmtLine, unknown>[] = [
    // Tabel disederhanakan (29 Sep 2026): satu baris per informasi — nomor baris, saldo, jenis kecocokan & rincian saran/hasil
    // (pihak, selisih, pencocok) dipindah ke tooltip.
    { id: "txn_date", header: "Tanggal", size: 110, meta: { mobile: "secondary" }, cell: ({ row: { original: l } }) => <span className="whitespace-nowrap text-sm tnum" title={`Baris ${l.line_no}`}>{fmtDay(l.txn_date)}</span> },
    { id: "desc", header: "Keterangan", meta: { mobile: "primary" }, cell: ({ row: { original: l } }) => <CellTitle code={l.reference} title={l.description} /> },
    { id: "amount", header: "Nominal", size: 150, meta: { mobile: "secondary" }, cell: ({ row: { original: l } }) => <span className={cn("block whitespace-nowrap text-right tnum font-semibold", l.amount < 0 && "text-on-surface-variant")} title={l.balance !== null ? `Saldo ${fmtMoney(l.balance)}` : undefined}>{fmtSigned(l.amount)}</span> },
    { id: "status", header: "Status", size: 150, meta: { mobile: "status" }, cell: ({ row: { original: l } }) => <span title={l.match_type ? MATCH_TYPE[l.match_type] ?? l.match_type : undefined}><StatusBadge objectType="bank_statement_line" status={l.status} /></span> },
    { id: "result", header: "Saran / hasil", meta: { mobile: "secondary" }, cell: ({ row: { original: l } }) => <LineResult line={l} /> },
    {
      id: "actions", header: "", size: 130,
      cell: ({ row: { original: l } }) => (
        <span className="inline-flex justify-end" onClick={(e) => e.stopPropagation()}>
          {l.allowed_actions.includes("confirm") ? <Button size="sm" variant={(l.match_score ?? 0) >= 90 ? "primary" : "secondary"} icon="check" loading={busy === l.id} onClick={() => confirmLine(l)} title={(l.match_score ?? 0) >= 90 ? undefined : "Skor rendah — periksa berita transfer sebelum konfirmasi"}>Konfirmasi</Button>
            : l.allowed_actions.includes("match") ? <Button size="sm" variant="secondary" icon="link" onClick={() => setMatchFor(l)}>Cocokkan</Button>
            : l.allowed_actions.includes("restore") ? <Button size="sm" variant="ghost" icon="undo" loading={busy === l.id} onClick={() => restoreLine(l)}>Pulihkan</Button> : null}
        </span>
      ),
    },
  ];
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <Link to="/billing/reconciliation" className="inline-flex items-center gap-1 text-sm font-semibold text-primary hover:underline"><Icon name="arrow_back" size={16} aria-hidden />Semua impor</Link>
          <div className="mt-1 flex flex-wrap items-center gap-2">
            <h2 className="text-h2 font-bold">{imp.file_name ?? "Impor mutasi"}</h2>
            <Badge>{imp.format.toUpperCase()}</Badge>
            <Badge tone={imp.status === "completed" ? "success" : "warning"}>{imp.status === "completed" ? "Selesai" : "Terbuka"}</Badge>
          </div>
          <div className="text-sm text-on-surface-variant">{imp.property_name}{imp.bank_account ? ` · ${imp.bank_account}` : ""} · periode {fmtDay(imp.period_from)} – {fmtDay(imp.period_to)} · diimpor <RelativeTime value={imp.imported_at} /> oleh {imp.imported_by_name ?? "—"}</div>
        </div>
        {canManage && bulkN > 0 && <Button icon="done_all" loading={busy === "bulk"} onClick={() => setBulk(true)}>Konfirmasi saran ≥ 90 ({bulkN})</Button>}
      </div>
      {fresh && (detected.length > 0 || !!fresh.duplicates_skipped) && (
        <Alert variant="info" title="Hasil impor">
          {detected.length > 0 && <div className="mt-1 flex flex-wrap items-center gap-1.5">Kolom terdeteksi: {detected.map((d) => <Badge key={d.key} tone="info">{d.label}{d.header ? ` ← ${d.header}` : ""}</Badge>)}</div>}
          {!!fresh.duplicates_skipped && <div className="mt-1">{fmtNumber(fresh.duplicates_skipped)} baris duplikat (sudah pernah diimpor) dilewati.</div>}
        </Alert>
      )}
      <section aria-label="Ringkasan impor" className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <StatTile label="Mutasi" value={fmtNumber(imp.line_count)} sub={`${fmtNumber(imp.credit_count)} kredit · ${fmtNumber(imp.line_count - imp.credit_count)} debit`} />
        <StatTile label="Total kredit" value={fmtMoneyTile(imp.credit_total)} title={fmtMoney(imp.credit_total)} sub="uang masuk" />
        <StatTile label="Cocok" value={fmtNumber(imp.matched_count)} sub={fmtMoney(imp.matched_total)} tone={imp.matched_count > 0 ? "success" : undefined} />
        <StatTile label="Ada saran" value={fmtNumber(imp.suggested_count)} sub="perlu dikonfirmasi" tone={imp.suggested_count > 0 ? "info" : undefined} />
        <StatTile label="Belum cocok" value={fmtNumber(imp.unmatched_count)} sub="cocokkan manual / abaikan" tone={imp.unmatched_count > 0 ? "warning" : undefined} />
        <StatTile label="Diabaikan" value={fmtNumber(imp.ignored_count)} sub="termasuk mutasi debit" />
      </section>
      <Card>
        <CardContent className="pt-4">
          <div className="flex items-center justify-between text-sm"><span className="font-medium">Progres rekonsiliasi kredit</span><span className="tnum text-on-surface-variant">{progress}%</span></div>
          <div className="mt-1 h-2 w-full overflow-hidden rounded-full bg-surface-container-high" role="progressbar" aria-valuenow={progress} aria-valuemin={0} aria-valuemax={100} aria-label="Progres rekonsiliasi">
            <div className="h-full rounded-full bg-primary" style={{ width: `${progress}%` }} />
          </div>
        </CardContent>
      </Card>
      <span className="flex flex-wrap gap-1.5">
        {LINE_FILTERS.map((f) => <FilterChip key={f.key} selected={filter === f.key} onClick={() => setFilter(f.key)}>{f.label} ({fmtNumber(lines.filter(f.test).length)})</FilterChip>)}
      </span>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        layout="auto"
        isFiltered={filter !== "all"}
        empty={{ icon: "account_balance", title: "Tidak ada mutasi" }}
        rowActions={(l) => [
          ...(l.allowed_actions.includes("match") ? [{ label: "Cocokkan manual…", icon: "link", onSelect: () => setMatchFor(l) }] : []),
          ...(l.allowed_actions.includes("ignore") ? [{ label: "Abaikan…", icon: "block", destructive: true, onSelect: () => setIgnoreFor(l) }] : []),
          ...(l.payment_id ? [{ label: `Buka pembayaran ${l.payment_number ?? ""}`, icon: "payments", onSelect: () => nav(`/billing/payments/${l.payment_id}`) }] : []),
        ]}
        rowClassName={(l) => (l.status === "suggested" && (l.match_score ?? 0) >= 90 ? "border-l-4 border-l-success" : undefined)}
      />
      {matchFor && <MatchDialog line={matchFor} propertyId={imp.property_id} onClose={() => setMatchFor(null)} onDone={apply} />}
      {ignoreFor && (
        <ReasonDialog
          open
          onOpenChange={(o) => !o && setIgnoreFor(null)}
          title={`Abaikan mutasi ${fmtMoney(ignoreFor.amount)}`}
          label="Alasan diabaikan"
          description="Untuk mutasi yang bukan pembayaran tenant (bunga bank, transfer internal, setoran lain). Dapat dipulihkan kemudian."
          confirmLabel="Abaikan"
          destructive
          loading={busy === ignoreFor.id}
          onConfirm={(note) => { const l = ignoreFor; setIgnoreFor(null); void run(l.id, () => api<StatementImport>(`billing/bank-statement-lines/${l.id}/ignore`, { body: { note }, idempotencyKey: uuid() }), "Mutasi diabaikan"); }}
        />
      )}
      {bulk && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setBulk(false)}
          title={`Konfirmasi ${bulkN} saran`}
          description="Semua saran dengan skor ≥ 90 dikonfirmasi: pembayaran dibuat/diverifikasi sesuai saran. Periksa selisih terlebih dahulu bila ada."
          confirmLabel="Konfirmasi semua"
          onConfirm={() => void confirmBulk()}
        />
      )}
    </div>
  );
}

function LineResult({ line: l }: { line: StmtLine }) {
  const diff = l.difference ? `Selisih ${fmtSigned(l.difference)} · ${differenceText(l.difference)}` : null;
  if (l.status === "matched") {
    const tip = [diff, [l.matched_by_name, l.matched_at ? fmtDateTime(l.matched_at) : null].filter(Boolean).join(" · ")].filter(Boolean).join("\n");
    return (
      <div className="flex max-w-[240px] items-center gap-1.5 whitespace-nowrap text-sm" title={tip || undefined}>
        {l.payment_id && <Link to={`/billing/payments/${l.payment_id}`} className="truncate font-mono text-[13px] text-primary hover:underline">{l.payment_number ?? "Pembayaran"}</Link>}
        {l.invoice_id && <><Icon name="arrow_forward" size={12} aria-hidden /><Link to={`/billing/invoices/${l.invoice_id}`} className="truncate font-mono text-[13px] text-primary hover:underline">{l.invoice_number ?? "Invoice"}</Link></>}
        {diff && <Icon name="info" size={14} className="shrink-0 text-on-surface-variant" aria-label={diff} />}
      </div>
    );
  }
  if (l.status === "suggested") {
    const num = l.suggested_payment_number ?? l.suggested_invoice_number;
    const tip = [l.suggested_payment_number && l.suggested_invoice_number ? `Invoice ${l.suggested_invoice_number}` : null, l.suggested_party, diff].filter(Boolean).join("\n");
    return (
      <div className="flex max-w-[240px] items-center gap-1.5 whitespace-nowrap text-sm" title={tip || undefined}>
        <span className="truncate font-mono text-[13px] font-semibold">{num ?? "—"}</span>
        <Badge tone={scoreTone(l.match_score)} title="Skor keyakinan saran (0–100)">skor {l.match_score ?? 0}</Badge>
      </div>
    );
  }
  if (l.status === "ignored") return <CellText muted max={220}>{l.note ?? "Diabaikan"}</CellText>;
  return <CellText muted max={220}>Tidak ada saran otomatis</CellText>;
}
