// Receivables › Statement of account (PRD P4 v2.1 P4-OUT-02): per tenant (seluruh unitnya) atau per unit tanpa tenant — saldo
// awal, mutasi invoice/pembatalan/credit note/pembayaran/refund/saldo kredit dengan saldo berjalan, saldo akhir, outstanding,
// saldo kredit & deposit. Unduh PDF (format=pdf) dan tautan berbagi bertanda tangan (berlaku 72 jam).
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, DatePicker, Dialog, DialogContent, DialogFooter, TBody, TD, TH, THead, TR, Table } from "@/components/ui/primitives";
import { CardSkeleton, EmptyState, KpiSkeleton, QueryErrorState } from "@/components/bv/states";
import { useToast } from "@/components/bv/common";
import { CellText } from "@/components/bv/cells";
import { api, downloadFile, uuid } from "@/lib/api";
import { fmtDateTime, fmtMoney } from "@/lib/format";
import { cn } from "@/lib/utils";
import { CopyField, NeedProperty, PartyPicker, PropertySelect, StatTile, type Party } from "@/features/finance/fin-ui";
import { fmtDay, fmtMoneyTile, useGet, usePropertyParam } from "@/features/finance/fin-utils";
import { STATEMENT_KIND, balanceText, statementObjectLink, type DocumentLink, type Statement } from "./receivables-model";

export function StatementTab() {
  const toast = useToast();
  const [sp, setSp] = useSearchParams();
  const [pid, setPid] = usePropertyParam(true);
  const party: Party = { tenant_id: sp.get("tenant_id") || null, unit_location_id: sp.get("tenant_id") ? null : sp.get("unit_location_id") || null };
  const from = sp.get("from") ?? "";
  const to = sp.get("to") ?? "";
  const set = (patch: Record<string, string | null>) => {
    const n = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (v) n.set(k, v);
      else n.delete(k);
    }
    setSp(n, { replace: true });
  };
  const hasParty = !!(party.tenant_id || party.unit_location_id);
  const params = { property_id: pid ?? undefined, tenant_id: party.tenant_id ?? undefined, unit_location_id: party.unit_location_id ?? undefined, from: from || undefined, to: to || undefined };
  const q = useGet<Statement>("billing/statement", params, { enabled: !!pid && hasParty });
  const [busy, setBusy] = useState<"pdf" | "link" | null>(null);
  const [link, setLink] = useState<DocumentLink | null>(null);
  const st = q.data;

  const downloadPdf = async () => {
    setBusy("pdf");
    try {
      await downloadFile("billing/statement", { ...params, format: "pdf", download: 1 }, "statement.pdf");
      toast.action("exported", "Statement of account");
    } catch (e) {
      toast.failed("exported", e, "Statement of account");
    } finally {
      setBusy(null);
    }
  };
  const shareLink = async () => {
    setBusy("link");
    try {
      const out = await api<DocumentLink>("billing/statement/link", { body: { property_id: pid, tenant_id: party.tenant_id, unit_location_id: party.unit_location_id, from: from || st?.from || null, to: to || st?.to || null }, idempotencyKey: uuid() });
      setLink(out);
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="grid grid-cols-1 gap-3 pt-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)_auto]">
          <div className="space-y-2">
            <div className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Property</div>
            <PropertySelect value={pid} onChange={(id) => setPid(id, { tenant_id: null, unit_location_id: null })} className="sm:w-full" />
            {!pid && <NeedProperty what="statement" />}
          </div>
          <div className="space-y-2">
            <div className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Pihak</div>
            <PartyPicker key={pid ?? "none"} propertyId={pid} value={party} onChange={(p) => set({ tenant_id: p.tenant_id, unit_location_id: p.unit_location_id })} />
          </div>
          <div className="space-y-2">
            <div className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Periode</div>
            <div className="flex items-center gap-1">
              <DatePicker className="w-40" value={from || st?.from || ""} onChange={(v) => set({ from: v || null, to: v ? to || st?.to || null : to || null })} aria-label="Dari tanggal" />
              <span className="text-on-surface-variant">–</span>
              <DatePicker className="w-40" value={to || st?.to || ""} onChange={(v) => set({ to: v || null, from: v ? from || st?.from || null : from || null })} aria-label="Sampai tanggal" />
            </div>
            <p className="text-xs text-on-surface-variant">Default 6 bulan terakhir s/d hari ini.</p>
          </div>
        </CardContent>
      </Card>

      {!hasParty ? (
        <EmptyState icon="receipt_long" title="Pilih tenant atau unit" description="Statement of account menampilkan tagihan, pembayaran, credit note, dan saldo kredit pihak dengan saldo berjalan." />
      ) : q.isLoading ? (
        <>
          <KpiSkeleton count={4} />
          <CardSkeleton lines={8} />
        </>
      ) : q.error && !st ? (
        <QueryErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : st ? (
        <>
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <div className="text-h2 font-bold">{st.tenant_name ?? st.unit_label ?? "—"}</div>
              <div className="text-sm text-on-surface-variant">{st.property_name}{st.tenant_name && st.unit_label ? ` · ${st.unit_label}` : ""} · {fmtDay(st.from)} – {fmtDay(st.to)} · dibuat {fmtDateTime(st.generated_at)}</div>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" icon="picture_as_pdf" loading={busy === "pdf"} onClick={downloadPdf}>Unduh PDF</Button>
              <Button variant="secondary" icon="link" loading={busy === "link"} onClick={shareLink}>Tautan berbagi</Button>
            </div>
          </div>
          <section aria-label="Ringkasan statement" className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
            <StatTile label="Saldo awal" value={fmtMoneyTile(st.opening_balance)} title={fmtMoney(st.opening_balance)} sub={`per ${fmtDay(st.from)}`} />
            <StatTile label="Tagihan (debit)" value={fmtMoneyTile(st.total_debit)} title={fmtMoney(st.total_debit)} sub="invoice & refund periode" />
            <StatTile label="Pembayaran & kredit" value={fmtMoneyTile(st.total_credit)} title={fmtMoney(st.total_credit)} sub="pembayaran, credit note, pembatalan" />
            <StatTile label="Saldo akhir" value={fmtMoneyTile(Math.abs(st.closing_balance))} title={fmtMoney(st.closing_balance)} sub={balanceText(st.closing_balance)} tone={st.closing_balance > 0 ? "warning" : "success"} />
            <StatTile label="Outstanding saat ini" value={fmtMoneyTile(st.outstanding_amount)} title={fmtMoney(st.outstanding_amount)} sub="sisa invoice terbuka" tone={st.outstanding_amount > 0 ? "warning" : undefined} />
            <StatTile label="Saldo kredit · deposit" value={fmtMoneyTile(st.credit_balance)} title={`Kredit ${fmtMoney(st.credit_balance)} · Deposit ${fmtMoney(st.deposit_balance)}`} sub={`Deposit ${fmtMoney(st.deposit_balance)}`} />
          </section>
          <Card>
            <CardHeader>
              <div className="min-w-0 flex-1 basis-60">
                <CardTitle>Mutasi</CardTitle>
                <CardSubtitle>Debit menambah saldo terutang; kredit menguranginya. Saldo negatif = kelebihan bayar.</CardSubtitle>
              </div>
            </CardHeader>
            <CardContent className="px-0 pb-0">
              <Table data-testid="statement-table">
                <THead>
                  <tr>
                    <TH>Tanggal</TH>
                    <TH>Jenis</TH>
                    <TH>Referensi</TH>
                    <TH>Keterangan</TH>
                    <TH className="bv-num">Debit</TH>
                    <TH className="bv-num">Kredit</TH>
                    <TH className="bv-num">Saldo</TH>
                  </tr>
                </THead>
                <TBody>
                  <TR className="bg-surface-container-low">
                    <TD className="whitespace-nowrap">{fmtDay(st.from)}</TD>
                    <TD colSpan={3} className="font-medium">Saldo awal</TD>
                    <TD />
                    <TD />
                    <TD className="bv-num tnum whitespace-nowrap font-semibold">{fmtMoney(st.opening_balance)}</TD>
                  </TR>
                  {st.entries.length === 0 && (
                    <TR><TD colSpan={7} className="py-6 text-center text-sm text-on-surface-variant">Tidak ada mutasi pada periode ini.</TD></TR>
                  )}
                  {st.entries.map((e, i) => {
                    const to = statementObjectLink(e);
                    return (
                      <TR key={e.object_id + e.kind + i}>
                        <TD className="whitespace-nowrap">{fmtDay(e.date)}</TD>
                        <TD className="whitespace-nowrap">{STATEMENT_KIND[e.kind] ?? e.kind}</TD>
                        <TD className="whitespace-nowrap font-mono text-[13px]">{e.reference ? to ? <Link to={to} className="text-primary hover:underline">{e.reference}</Link> : e.reference : "—"}</TD>
                        <TD><CellText max={280}>{e.description}</CellText></TD>
                        <TD className="bv-num tnum whitespace-nowrap">{e.debit ? fmtMoney(e.debit) : ""}</TD>
                        <TD className="bv-num tnum whitespace-nowrap">{e.credit ? fmtMoney(e.credit) : ""}</TD>
                        <TD className={cn("bv-num tnum whitespace-nowrap", e.balance < 0 && "text-on-success-container")}>{fmtMoney(e.balance)}</TD>
                      </TR>
                    );
                  })}
                  <TR className="bg-surface-container-low font-semibold">
                    <TD className="whitespace-nowrap">{fmtDay(st.to)}</TD>
                    <TD colSpan={3}>Saldo akhir ({balanceText(st.closing_balance)})</TD>
                    <TD className="bv-num tnum whitespace-nowrap">{fmtMoney(st.total_debit)}</TD>
                    <TD className="bv-num tnum whitespace-nowrap">{fmtMoney(st.total_credit)}</TD>
                    <TD className="bv-num tnum whitespace-nowrap">{fmtMoney(st.closing_balance)}</TD>
                  </TR>
                </TBody>
              </Table>
            </CardContent>
          </Card>
        </>
      ) : null}

      {link && (
        <Dialog open onOpenChange={(o) => !o && setLink(null)}>
          <DialogContent title="Tautan statement" description="Tautan PDF bertanda tangan yang dapat dibagikan ke tenant (mis. lewat WhatsApp). Siapa pun yang memegang tautan dapat membukanya sampai kedaluwarsa.">
            <CopyField label="Tautan" value={link.url} help={`Berlaku sampai ${fmtDateTime(link.expires_at)}`} />
            <DialogFooter>
              <Button variant="secondary" onClick={() => setLink(null)}>Tutup</Button>
              <Button icon="open_in_new" onClick={() => window.open(link.url, "_blank", "noopener")}>Buka</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
