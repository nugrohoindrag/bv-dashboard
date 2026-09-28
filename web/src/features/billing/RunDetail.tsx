// Detail Billing Run (/billing/runs/:id — PRD P4 v2.1 P4-BRL-02..04; desain "Buat Tagihan Bulanan"): pratinjau baris per unit
// (qty × tarif, pajak, prorata, meter awal → akhir), badge pengecualian, sertakan/kecualikan baris, total, hitung ulang →
// buat draft invoice → daftar draft → terbitkan semua; batalkan dengan alasan.
import { useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Card, Checkbox, ConfirmDialog, SearchInput, Table, TBody, TD, TH, THead, TR } from "@/components/ui/primitives";
import { AsyncState, DetailSkeleton, ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { DataGrid } from "@/components/bv/datagrid";
import { useInvalidate, useList, useOne } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { ExceptionBadge, ItemMeta, SummaryTile } from "./shared";
import { RUN_EXCEPTIONS, dueDay, fmtDay, fmtPeriod, invoiceTypeLabel, money, pct, qtyText, type BillingRun, type Invoice, type RunLine } from "./types";

type LineFilter = "all" | "included" | "excluded" | "exception";
const PAGE = 40;

export function RunDetail({ id }: { id: string }) {
  const run = useOne<BillingRun>("billing/runs", id);
  return <AsyncState query={run} skeleton={<DetailSkeleton />}>{(r) => <RunView run={r} />}</AsyncState>;
}

interface UnitGroup { unitId: string; unitLabel: string; tenantName: string | null; lines: RunLine[]; included: number; tax: number; invoiceIds: { id: string; number: string | null }[] }

function RunView({ run: r }: { run: BillingRun }) {
  const toast = useToast();
  const qc = useQueryClient();
  const invalidate = useInvalidate();
  const [filter, setFilter] = useState<LineFilter>("all");
  const [q, setQ] = useState("");
  const [limit, setLimit] = useState(PAGE);
  const [dialog, setDialog] = useState<null | "generate" | "issue" | "cancel">(null);
  const [busy, setBusy] = useState<string | null>(null);
  const inFlight = useRef(false);
  const has = (a: string) => r.allowed_actions.includes(a);
  const canToggle = has("toggle_line");
  const lines = useMemo(() => r.lines ?? [], [r.lines]);
  const drafts = useList<Invoice>("invoices", { billing_run_id: r.id }, { enabled: r.status !== "preview" });

  const groups = useMemo(() => {
    const map = new Map<string, UnitGroup>();
    for (const l of lines) {
      let g = map.get(l.unit_location_id);
      if (!g) {
        g = { unitId: l.unit_location_id, unitLabel: l.unit_label, tenantName: l.tenant_name, lines: [], included: 0, tax: 0, invoiceIds: [] };
        map.set(l.unit_location_id, g);
      }
      g.lines.push(l);
      if (!g.tenantName && l.tenant_name) g.tenantName = l.tenant_name;
      if (l.included && !l.exception) {
        g.included += l.amount;
        g.tax += l.tax_amount;
      }
      if (l.invoice_id && !g.invoiceIds.some((x) => x.id === l.invoice_id)) g.invoiceIds.push({ id: l.invoice_id, number: l.invoice_number });
    }
    return [...map.values()];
  }, [lines]);

  const needle = q.trim().toLowerCase();
  const visible = groups
    .map((g) => ({
      ...g,
      lines: g.lines.filter((l) => (filter === "all" ? true : filter === "included" ? l.included && !l.exception : filter === "excluded" ? !l.included && !l.exception : !!l.exception)),
    }))
    .filter((g) => g.lines.length > 0 && (!needle || g.unitLabel.toLowerCase().includes(needle) || (g.tenantName ?? "").toLowerCase().includes(needle)));
  const counts = {
    all: lines.length,
    included: lines.filter((l) => l.included && !l.exception).length,
    excluded: lines.filter((l) => !l.included && !l.exception).length,
    exception: lines.filter((l) => !!l.exception).length,
  };

  const setRun = (out: BillingRun) => {
    qc.setQueryData(["one", "billing/runs", r.id], out);
    invalidate("list");
  };

  const refetchRun = () => qc.invalidateQueries({ queryKey: ["one", "billing/runs", r.id] });

  const toggleLines = async (targets: RunLine[], include: boolean) => {
    const todo = targets.filter((l) => !l.exception && l.included !== include);
    if (!todo.length) return;
    setBusy(`toggle:${todo[0].id}`);
    try {
      let out: BillingRun | null = null;
      for (const l of todo) out = await api<BillingRun>(`billing/runs/${r.id}/lines/${l.id}/${include ? "include" : "exclude"}`, { method: "POST", body: {} });
      if (out) setRun(out);
    } catch (e) {
      toast.error(e);
      refetchRun();
    } finally {
      setBusy(null);
    }
  };

  const action = async (a: "refresh" | "generate" | "issue" | "cancel", reason?: string) => {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(a);
    try {
      const out = await api<BillingRun>(`billing/runs/${r.id}/${a}`, { body: reason ? { reason } : {}, idempotencyKey: uuid() });
      setRun(out);
      invalidate("list", "all");
      if (a === "refresh") toast.success(`Pratinjau dihitung ulang — ${out.line_count} baris, ${out.exception_count} pengecualian`);
      if (a === "generate") toast.success(`${out.invoice_count} draft invoice dibuat — review lalu terbitkan`);
      if (a === "issue") toast.success(`${out.invoice_count} invoice diterbitkan & dikirim ke tenant`);
      if (a === "cancel") toast.action("cancelled", `Billing run ${out.run_number}`);
      setDialog(null);
    } catch (e) {
      toast.error(e);
    } finally {
      inFlight.current = false;
      setBusy(null);
    }
  };

  const draftCols = useMemo<ColumnDef<Invoice, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): nomor + unit satu kolom, badge "Draft" hanya di kolom Status.
    { id: "number", header: "Invoice", meta: { mobile: "primary" }, cell: ({ row: { original: i } }) => <CellTitle code={i.invoice_number ?? "Belum bernomor"} title={i.unit_label ?? "—"} /> },
    { id: "party", header: "Tenant", meta: { mobile: "secondary" }, cell: ({ row: { original: i } }) => <CellText max={200} muted={!i.tenant_name}>{i.tenant_name ?? "Tanpa tenant"}</CellText> },
    { id: "items", header: "Jenis", size: 150, meta: { mobile: "hidden" }, cell: ({ row: { original: i } }) => <CellText max={150} muted>{invoiceTypeLabel(i.invoice_type)}</CellText> },
    { id: "due", header: "Jatuh tempo", size: 130, meta: { mobile: "hidden" }, cell: ({ row: { original: i } }) => <span className="tnum whitespace-nowrap text-sm">{fmtDay(dueDay(i))}</span> },
    { id: "total", header: "Total", size: 130, meta: { mobile: "secondary" }, cell: ({ row: { original: i } }) => <div className="tnum whitespace-nowrap text-right font-semibold">{money(i.total_amount, i.currency_code)}</div> },
    { id: "status", header: "Status", size: 130, meta: { mobile: "status" }, cell: ({ row: { original: i } }) => <StatusBadge objectType="invoice" status={i.status} /> },
  ], []);
  const draftRows = drafts.data?.pages.flatMap((p) => p.data) ?? [];

  return (
    <div>
      <PageHeader
        breadcrumb={<><Link to="/billing/runs" className="hover:underline">Billing Runs</Link> / <span className="font-mono">{r.run_number}</span></>}
        title={<span>Tagihan {r.period_label}</span>}
        badges={<><StatusBadge objectType="billing_run" status={r.status} />{r.source === "scheduled" && <Badge tone="info">Terjadwal</Badge>}</>}
        subtitle={<>{r.property_name} · periode {fmtPeriod(r.period_start, r.period_end)} · terbit {fmtDay(r.issue_date)} · jatuh tempo {fmtDay(r.due_date)} · {r.combine ? `digabung per unit (${invoiceTypeLabel(r.invoice_type)})` : "satu invoice per baris"} · dibuat <RelativeTime value={r.created_at} /> oleh {r.created_by_name ?? "sistem"}</>}
        actions={
          <>
            {has("refresh") && <Button variant="secondary" icon="refresh" loading={busy === "refresh"} onClick={() => action("refresh")}>Hitung ulang</Button>}
            {has("generate") && <Button icon="note_add" disabled={counts.included === 0} onClick={() => setDialog("generate")}>Buat draft invoice</Button>}
            {has("issue") && <Button icon="send" onClick={() => setDialog("issue")}>Terbitkan semua</Button>}
            {has("cancel") && <Button variant="ghost" icon="block" onClick={() => setDialog("cancel")}>Batalkan…</Button>}
          </>
        }
      />

      {r.status === "preview" && <Alert variant="info" className="mb-4">Pratinjau — belum ada invoice. Lengkapi data yang menjadi pengecualian (luas unit, pembacaan meter, tarif) lalu <b>Hitung ulang</b>, atau kecualikan baris yang tidak ingin ditagih.</Alert>}
      {r.status === "generated" && <Alert variant="warning" className="mb-4">Draft invoice sudah dibuat dan belum terlihat tenant. Review lalu <b>Terbitkan semua</b> — nomor INV diberikan saat terbit.</Alert>}
      {r.status === "cancelled" && r.notes && <Alert variant="warning" className="mb-4" title="Dibatalkan"><span className="whitespace-pre-line">{r.notes}</span></Alert>}

      <div className="mb-4 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
        <SummaryTile label="Unit" value={groups.length} sub={`${r.line_count} baris`} />
        <SummaryTile label="Ditagihkan" value={counts.included} sub={`${counts.excluded} dikecualikan`} />
        <SummaryTile label="Pengecualian" value={r.exception_count} tone={r.exception_count > 0 ? "warning" : undefined} />
        <SummaryTile label="Subtotal" value={money(r.subtotal_amount)} />
        <SummaryTile label="Pajak" value={money(r.tax_amount)} />
        <SummaryTile label="Total tagihan" value={money(r.total_amount)} tone="primary" sub={r.invoice_count ? `${r.invoice_count} invoice` : undefined} />
      </div>

      {Object.keys(r.exceptions ?? {}).length > 0 && (
        <div className="mb-4 flex flex-wrap items-center gap-1.5">
          <span className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Pengecualian:</span>
          {Object.entries(r.exceptions).map(([code, n]) => <ExceptionBadge key={code} code={code} label={`${RUN_EXCEPTIONS[code] ?? code} · ${n}`} />)}
        </div>
      )}

      {r.status !== "preview" && (
        <section className="mb-6">
          <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-h3 font-bold">Invoice dari run ini</h2>
            <Link to={`/billing/invoices?billing_run_id=${r.id}`} className="inline-flex items-center gap-1 text-sm text-primary hover:underline">Buka di Invoices <Icon name="open_in_new" size={14} /></Link>
          </div>
          <DataGrid columns={draftCols} rows={draftRows} rowId={(i) => i.id} onRowClick={(i) => `/billing/invoices/${i.id}?billing_run_id=${r.id}`} loading={drafts.isLoading} error={drafts.error} onRetry={() => drafts.refetch()} empty={{ icon: "request_quote", title: "Belum ada invoice", description: r.status === "cancelled" ? "Draft invoice run ini dibatalkan." : "Invoice run ini belum tersedia." }} hasMore={drafts.hasNextPage} onLoadMore={() => drafts.fetchNextPage()} loadingMore={drafts.isFetchingNextPage} />
        </section>
      )}

      <div className="mb-3 flex flex-wrap items-center gap-2">
        {([["all", "Semua"], ["included", "Ditagihkan"], ["excluded", "Dikecualikan"], ["exception", "Pengecualian"]] as [LineFilter, string][]).map(([k, label]) => (
          <FilterChip key={k} selected={filter === k} onClick={() => { setFilter(k); setLimit(PAGE); }}>{label} ({counts[k]})</FilterChip>
        ))}
        <span className="w-full sm:ml-auto sm:w-64"><SearchInput placeholder="Cari unit / tenant…" value={q} onChange={(e) => { setQ(e.target.value); setLimit(PAGE); }} aria-label="Cari unit atau tenant" /></span>
      </div>

      {visible.length === 0 ? (
        <p className="rounded-[var(--radius-md)] border border-border px-4 py-8 text-center text-sm text-on-surface-variant">{lines.length === 0 ? "Tidak ada unit dalam cakupan rule terpilih pada periode ini." : "Tidak ada baris untuk filter ini."}</p>
      ) : (
        <div className="space-y-3">
          {visible.slice(0, limit).map((g) => {
            const togglable = g.lines.filter((l) => !l.exception);
            const allOn = togglable.length > 0 && togglable.every((l) => l.included);
            const someOn = togglable.some((l) => l.included);
            return (
              <Card key={g.unitId} className="p-0">
                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-2.5">
                  <div className="flex min-w-0 items-center gap-3">
                    {canToggle && togglable.length > 0 && (
                      <Checkbox checked={allOn} indeterminate={!allOn && someOn} onCheckedChange={(v) => toggleLines(togglable, v)} aria-label={`Sertakan semua baris ${g.unitLabel}`} disabled={!!busy} />
                    )}
                    <div className="min-w-0">
                      <div className="font-semibold">{g.unitLabel}</div>
                      <div className="text-xs text-on-surface-variant">{g.tenantName ?? "Tanpa tenant (ditagih ke unit)"}</div>
                    </div>
                  </div>
                  <div className="flex items-center gap-3 text-right">
                    {g.invoiceIds.map((inv) => <Link key={inv.id} to={`/billing/invoices/${inv.id}?billing_run_id=${r.id}`} className="text-sm text-primary hover:underline">{inv.number ?? "Draft"}</Link>)}
                    <div><div className="text-[11px] uppercase tracking-wide text-on-surface-variant">Total unit</div><div className="tnum font-bold">{money(g.included + g.tax)}</div></div>
                  </div>
                </div>
                <Table>
                  <THead>
                    <tr>
                      {canToggle && <TH style={{ width: 40 }} aria-label="Sertakan" />}
                      <TH>Komponen</TH>
                      <TH className="bv-num">Qty × tarif</TH>
                      <TH className="bv-num">Pajak</TH>
                      <TH className="bv-num">Jumlah</TH>
                    </tr>
                  </THead>
                  <TBody>
                    {g.lines.map((l) => {
                      const off = !!l.exception || !l.included;
                      return (
                        <TR key={l.id} className={cn(off && "text-on-surface-variant")}>
                          {canToggle && (
                            <TD>{!l.exception && <Checkbox checked={l.included} onCheckedChange={(v) => toggleLines([l], v)} aria-label={`Sertakan ${l.description}`} disabled={!!busy} />}</TD>
                          )}
                          <TD>
                            <div className={cn("font-medium", off && "line-through decoration-1")}>{l.description}</div>
                            <div className="text-xs text-on-surface-variant">{l.rule_name} · {invoiceTypeLabel(l.charge_type)}</div>
                            <ItemMeta meta={l.meta} unit={l.unit_measure} />
                            <div className="mt-0.5 flex flex-wrap gap-1">
                              {l.exception && <ExceptionBadge code={l.exception} label={l.exception_label ?? RUN_EXCEPTIONS[l.exception]} />}
                              {!l.exception && !l.included && <Badge>Dikecualikan</Badge>}
                            </div>
                          </TD>
                          <TD className="bv-num whitespace-nowrap">{l.quantity ? <>{qtyText(l.quantity)} {l.unit_measure ?? ""} × {money(l.unit_price)}</> : "—"}</TD>
                          <TD className="bv-num whitespace-nowrap">{l.tax_amount > 0 ? <><span className="text-xs">{pct(l.tax_rate)}</span> {money(l.tax_amount)}</> : "—"}</TD>
                          <TD className="bv-num whitespace-nowrap font-semibold">{money(l.amount)}</TD>
                        </TR>
                      );
                    })}
                  </TBody>
                </Table>
              </Card>
            );
          })}
          {visible.length > limit && (
            <div className="text-center"><Button variant="secondary" onClick={() => setLimit((n) => n + PAGE)}>Tampilkan {Math.min(PAGE, visible.length - limit)} unit lagi ({visible.length - limit} tersisa)</Button></div>
          )}
          <div className="flex items-center justify-between rounded-[var(--radius-lg)] border border-border px-4 py-3">
            <span className="text-h3 font-bold uppercase">Total tagihan</span>
            <span className="tnum text-display font-bold">{money(r.total_amount)}</span>
          </div>
        </div>
      )}

      <ConfirmDialog open={dialog === "generate"} onOpenChange={(o) => !o && setDialog(null)} title="Buat draft invoice?" description={`${counts.included} baris ditagihkan (${money(r.total_amount)}) menjadi draft invoice ${r.combine ? "— satu per unit & pihak tagih" : "— satu per baris"}. Baris pengecualian & dikecualikan tidak ikut. Draft belum terlihat tenant.`} confirmLabel="Buat draft" onConfirm={() => action("generate")} />
      <ConfirmDialog open={dialog === "issue"} onOpenChange={(o) => !o && setDialog(null)} title={`Terbitkan ${r.invoice_count} invoice?`} description={`Nomor INV diberikan saat terbit dan setiap tenant menerima notifikasi. Total ${money(r.total_amount)} · jatuh tempo ${fmtDay(r.due_date)}. Invoice terbit tidak dapat diedit.`} confirmLabel="Terbitkan semua" onConfirm={() => action("issue")} />
      <ReasonDialog open={dialog === "cancel"} onOpenChange={(o) => !o && setDialog(null)} title={`Batalkan ${r.run_number}`} description={r.status === "generated" ? "Semua draft invoice run ini ikut dibatalkan; unit × periode dapat ditagihkan ulang lewat run baru." : "Pratinjau dibatalkan; tidak ada invoice yang dibuat."} confirmLabel="Batalkan run" destructive loading={busy === "cancel"} onConfirm={(reason) => action("cancel", reason)} />
      {r.generated_at && <p className="mt-4 text-xs text-on-surface-variant">Draft dibuat {fmtDateTime(r.generated_at)}{r.issued_at ? ` · diterbitkan ${fmtDateTime(r.issued_at)}` : ""}</p>}
    </div>
  );
}
