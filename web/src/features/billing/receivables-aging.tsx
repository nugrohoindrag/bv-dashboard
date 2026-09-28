// Receivables › Aging (PRD P4 v2.1 P4-AGE-01, P4-OUT-01): umur piutang per tenant/unit/property dengan drill-down setiap sel
// ke daftar invoice (filter aging + pihak). Titik-waktu (sekarang); dihitung server di zona waktu property.
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { RowActionMenu } from "@buildingvision/ui/bv";
import { Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Segmented, TBody, TD, TH, THead, TR, Table } from "@/components/ui/primitives";
import { CardSkeleton, EmptyState, KpiSkeleton, QueryErrorState } from "@/components/bv/states";
import { RelativeTime } from "@/components/bv/common";
import { useAuth } from "@/lib/auth";
import { fmtMoney, fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import { PropertySelect, StatTile } from "@/features/finance/fin-ui";
import { fmtMoneyTile, useGet, usePropertyParam } from "@/features/finance/fin-utils";
import { AGING_BUCKETS, AGING_GROUPS, agingInvoicesLink, bucketShare, bucketTone, collectionsLink, statementLink, type AgingGroup, type AgingReport, type AgingRow } from "./receivables-model";

export function AgingTab() {
  const nav = useNavigate();
  const { can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const [pid, setPid] = usePropertyParam();
  const group = (AGING_GROUPS.some((g) => g.value === sp.get("group_by")) ? sp.get("group_by") : "tenant") as AgingGroup;
  const q = useGet<AgingReport>("billing/aging", { property_id: pid ?? undefined, group_by: group });
  const setGroup = (g: AgingGroup) => {
    const n = new URLSearchParams(sp);
    n.set("group_by", g);
    setSp(n, { replace: true });
  };
  const r = q.data;
  const canCollect = can("billing.collections.view");
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <PropertySelect value={pid} onChange={setPid} allowAll />
        <Segmented<AgingGroup> value={group} onChange={setGroup} options={AGING_GROUPS} />
        {r && <span className="text-sm text-on-surface-variant">Per <RelativeTime value={r.as_of} /> · hanya invoice terbit dengan sisa tagihan</span>}
        <Button variant="ghost" size="sm" icon="refresh" className="ml-auto" loading={q.isFetching && !q.isLoading} onClick={() => void q.refetch()}>Muat ulang</Button>
      </div>
      {q.isLoading ? (
        <>
          <KpiSkeleton count={6} />
          <CardSkeleton lines={6} />
        </>
      ) : q.error && !r ? (
        <QueryErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : r ? (
        <>
          <section aria-label="Ringkasan aging" className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
            {AGING_BUCKETS.map((b) => {
              const v = r.totals[b.field];
              return <StatTile key={b.key} label={b.label} value={fmtMoneyTile(v)} title={fmtMoney(v)} sub={`${bucketShare(v, r.totals.total)}% dari piutang`} tone={bucketTone(b.key, v)} to={v > 0 ? agingInvoicesLink(null, b.key, pid) : undefined} testId={`aging-${b.key}`} />;
            })}
            <StatTile label="Total piutang" value={fmtMoneyTile(r.totals.total)} title={fmtMoney(r.totals.total)} sub={`${fmtNumber(r.totals.invoice_count)} invoice · tertua ${fmtNumber(r.totals.oldest_days_overdue)} hari`} tone="primary" to={r.totals.total > 0 ? agingInvoicesLink(null, "open", pid) : undefined} />
          </section>
          <Card>
            <CardHeader>
              <div className="min-w-0 flex-1 basis-60">
                <CardTitle>Aging {AGING_GROUPS.find((g) => g.value === group)?.label.toLowerCase()}</CardTitle>
                <CardSubtitle>Klik nilai untuk membuka daftar invoice pada bucket tersebut. Maks. 500 baris, diurutkan dari piutang terbesar.</CardSubtitle>
              </div>
            </CardHeader>
            <CardContent className="px-0 pb-0">
              {r.rows.length === 0 ? (
                <EmptyState icon="task_alt" title="Tidak ada piutang terbuka" description="Semua invoice terbit sudah lunas atau belum ada invoice pada scope ini." />
              ) : (
                <Table data-testid="aging-table">
                  <THead>
                    <tr>
                      <TH>{group === "property" ? "Property" : group === "unit" ? "Unit" : "Tenant / unit"}</TH>
                      {AGING_BUCKETS.map((b) => <TH key={b.key} className="bv-num">{b.label}</TH>)}
                      <TH className="bv-num">Total</TH>
                      <TH className="bv-num">Invoice</TH>
                      <TH className="bv-num">Tertua</TH>
                      <TH aria-label="Aksi" />
                    </tr>
                  </THead>
                  <TBody>
                    {r.rows.map((row, i) => (
                      <TR key={(row.id ?? "x") + row.property_id + i}>
                        <TD>
                          <PartyCell row={row} group={group} />
                        </TD>
                        {AGING_BUCKETS.map((b) => <MoneyCell key={b.key} amount={row[b.field]} to={agingInvoicesLink(row, b.key, pid)} strong={b.key === "90_plus"} />)}
                        <MoneyCell amount={row.total} to={agingInvoicesLink(row, "open", pid)} strong />
                        <TD className="bv-num tnum whitespace-nowrap">{fmtNumber(row.invoice_count)}</TD>
                        <TD className="bv-num tnum whitespace-nowrap">{row.oldest_days_overdue > 0 ? <Badge tone={row.oldest_days_overdue > 90 ? "error" : row.oldest_days_overdue > 60 ? "warning" : "neutral"}>{row.oldest_days_overdue} hari</Badge> : "—"}</TD>
                        <TD className="text-right">
                          {row.kind !== "property" && (
                            <RowActionMenu label="Aksi" items={[
                              { id: "st", label: "Statement of account", icon: "receipt_long", onClick: () => nav(statementLink(row.property_id, row)) },
                              ...(canCollect ? [{ id: "col", label: "Daftar kerja penagihan", icon: "pending_actions", onClick: () => nav(collectionsLink(row.property_id, row)) }] : []),
                              { id: "inv", label: "Semua invoice terbuka", icon: "request_quote", onClick: () => nav(agingInvoicesLink(row, "open", pid)) },
                            ]} />
                          )}
                        </TD>
                      </TR>
                    ))}
                    <TotalsRow row={r.totals} pid={pid} />
                  </TBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </>
      ) : null}
    </div>
  );
}

// Nama pihak satu baris (29 Sep 2026): label tebal + property/keterangan redup, dipotong "…" dengan teks lengkap di tooltip.
function PartyCell({ row, group }: { row: AgingRow; group: AgingGroup }) {
  const sub = [row.kind === "unit" && group === "tenant" ? "Unit tanpa tenant" : null, group !== "property" ? row.property_name : null].filter(Boolean).join(" · ");
  return (
    <span className="block max-w-[260px] truncate" title={[row.label, sub].filter(Boolean).join(" · ") || undefined}>
      <span className="font-medium">{row.label || "—"}</span>
      {sub && <span className="text-xs text-on-surface-variant"> · {sub}</span>}
    </span>
  );
}

function MoneyCell({ amount, to, strong }: { amount: number; to: string; strong?: boolean }) {
  if (!amount) return <TD className="bv-num tnum text-on-surface-variant">—</TD>;
  return (
    <TD className="bv-num tnum whitespace-nowrap">
      <Link to={to} className={cn("hover:underline", strong && "font-semibold")} title="Buka daftar invoice">{fmtMoney(amount)}</Link>
    </TD>
  );
}

function TotalsRow({ row, pid }: { row: AgingRow; pid: string | null }) {
  return (
    <TR className="bg-surface-container-low font-semibold">
      <TD>Total</TD>
      {AGING_BUCKETS.map((b) => <MoneyCell key={b.key} amount={row[b.field]} to={agingInvoicesLink(null, b.key, pid)} strong />)}
      <MoneyCell amount={row.total} to={agingInvoicesLink(null, "open", pid)} strong />
      <TD className="bv-num tnum whitespace-nowrap">{fmtNumber(row.invoice_count)}</TD>
      <TD className="bv-num tnum whitespace-nowrap">{row.oldest_days_overdue > 0 ? `${row.oldest_days_overdue} hari` : "—"}</TD>
      <TD />
    </TR>
  );
}
