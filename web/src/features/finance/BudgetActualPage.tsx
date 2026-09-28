// Finance › Budget vs Actual (PRD P4 v2.1 P4-BGT-02..03; Roadmap §25.8): budget disetujui (atau revisi terbaru) vs actual otomatis
// — pendapatan dari item invoice terbit per jenis (tanpa pajak & deposit, dikurangi credit note), biaya dari WO selesai,
// consumable cleaning, dan biaya manual — per kategori × bulan, YTD, variance & status; drill-down ke transaksi sumber.
// Route: /finance/budget-actual · /finance/budget-actual/transactions?kind&category&year&month&property_id (tautan server).
import { useMemo, useState } from "react";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { Bar, BarChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, NativeSelect, TBody, TD, TH, THead, TR, Table } from "@/components/ui/primitives";
import { CardSkeleton, EmptyState, KpiSkeleton, QueryErrorState } from "@/components/bv/states";
import { StatusBadge } from "@/components/bv/badges";
import { CellText } from "@/components/bv/cells";
import { fmtMoney, fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import { PropertySelect, StatTile, YearSelect } from "./fin-ui";
import { MONTHS_LONG, MONTHS_SHORT, appLink, currentYear, fmtAxisMoney, fmtDay, fmtMoneyShort, fmtMoneyTile, fmtPct, useGet, usePropertyParam, yearOptions } from "./fin-utils";
import { BVA_STATUS, SOURCE_TYPE, achievement, favorable, txnQuery, type ActualTxn, type BVA, type BVARow, type Categories } from "./finance-model";

export default function BudgetActualPage() {
  const { pathname } = useLocation();
  const txn = pathname.endsWith("/transactions");
  return (
    <div>
      <PageHeader title="Budget vs Actual" subtitle="Realisasi pendapatan & biaya dibanding budget per kategori dan bulan. Actual dihitung otomatis dari invoice, credit note, work order, consumable, dan biaya manual." />
      {txn ? <Transactions /> : <Overview />}
    </div>
  );
}

function useYearParam(): [number, (y: number) => void] {
  const [sp, setSp] = useSearchParams();
  const y = Number(sp.get("year")) || currentYear();
  return [y, (v: number) => { const n = new URLSearchParams(sp); n.set("year", String(v)); setSp(n, { replace: true }); }];
}

// ---------- ringkasan ----------
function Overview() {
  const nav = useNavigate();
  const [pid, setPid] = usePropertyParam();
  const [year, setYear] = useYearParam();
  const [view, setView] = useState<"ytd" | "monthly">("ytd");
  const q = useGet<BVA>("finance/budget-actual", { property_id: pid ?? undefined, year });
  const d = q.data;
  const upTo = d?.up_to_month ?? 12;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <PropertySelect value={pid} onChange={setPid} allowAll />
        <YearSelect value={year} years={yearOptions(currentYear(), 4, 1)} onChange={(y) => setYear(y ?? currentYear())} />
        <span className="flex gap-1.5">
          <FilterChip selected={view === "ytd"} onClick={() => setView("ytd")}>Ringkas (YTD)</FilterChip>
          <FilterChip selected={view === "monthly"} onClick={() => setView("monthly")}>Bulanan</FilterChip>
        </span>
        {d?.budget_id && <Link to={`/finance/budgets/${d.budget_id}`} className="ml-auto inline-flex items-center gap-1 text-sm font-semibold text-primary hover:underline">Budget rev {d.revision}{d.budget_status && <StatusBadge objectType="budget" status={d.budget_status} />}</Link>}
      </div>
      {q.isLoading ? (
        <>
          <KpiSkeleton count={3} />
          <CardSkeleton lines={8} />
        </>
      ) : q.error && !d ? (
        <QueryErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : d ? (
        <>
          {pid && !d.budget_id && <Alert variant="warning" title={`Belum ada budget ${year}`} action={<Button size="sm" variant="secondary" onClick={() => nav(`/finance/budgets?property_id=${pid}&year=${year}`)}>Buka Budget</Button>}>Actual tetap ditampilkan; variance dihitung setelah budget dibuat.</Alert>}
          {pid && d.budget_status === "draft" && <Alert variant="info">Memakai draft budget (belum disetujui) sebagai pembanding.</Alert>}
          {!pid && <Alert variant="info">Semua property: budget = jumlah budget <b>disetujui</b> tiap property.</Alert>}
          <section aria-label="Ringkasan YTD" className="grid grid-cols-1 gap-3 md:grid-cols-3">
            <SummaryTile label={`Pendapatan YTD s/d ${MONTHS_SHORT[Math.max(0, upTo - 1)]}`} row={d.total_revenue} />
            <SummaryTile label={`Biaya YTD s/d ${MONTHS_SHORT[Math.max(0, upTo - 1)]}`} row={d.total_cost} />
            <SummaryTile label="Selisih (pendapatan − biaya)" row={d.net} />
          </section>
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            <MonthlyChart title="Pendapatan per bulan" row={d.total_revenue} />
            <MonthlyChart title="Biaya per bulan" row={d.total_cost} />
          </div>
          <BvaTable title="Pendapatan" rows={d.revenue} total={d.total_revenue} view={view} upTo={upTo} year={year} pid={pid} />
          <BvaTable title="Biaya operasional" rows={d.cost} total={d.total_cost} view={view} upTo={upTo} year={year} pid={pid} />
          <BvaTable title="Selisih" rows={[]} total={d.net} view={view} upTo={upTo} year={year} pid={pid} netOnly />
          <p className="text-xs text-on-surface-variant">Status: pendapatan di bawah budget &gt; 5% = <b>di bawah budget</b>; biaya di atas budget &gt; 5% = <b>di atas budget</b>. YTD sampai bulan berjalan untuk tahun ini.</p>
        </>
      ) : null}
    </div>
  );
}

function SummaryTile({ label, row }: { label: string; row: BVARow }) {
  const kind = row.kind === "cost" ? "cost" : "revenue";
  const pct = achievement(row.ytd);
  const good = favorable(kind, row.ytd.variance);
  return (
    <StatTile
      label={label}
      value={fmtMoneyTile(row.ytd.actual)}
      title={`Actual ${fmtMoney(row.ytd.actual)} · budget ${fmtMoney(row.ytd.budget)}`}
      sub={row.ytd.budget ? `budget ${fmtMoney(row.ytd.budget)} · ${pct !== null ? `${fmtPct(pct)} tercapai · ` : ""}selisih ${row.ytd.variance > 0 ? "+" : ""}${fmtMoney(row.ytd.variance)}` : "belum ada budget"}
      tone={row.ytd.budget ? (row.status === "on_track" ? (good ? "success" : undefined) : BVA_STATUS[row.status]?.tone) : undefined}
    />
  );
}

// grafik: dua seri (budget · actual) pada satu sumbu uang; warna token chart DS, legenda + tooltip (tidak hanya warna)
function MonthlyChart({ title, row }: { title: string; row: BVARow }) {
  const data = row.months.map((c, i) => ({ month: MONTHS_SHORT[i], budget: c.budget, actual: c.actual }));
  const empty = data.every((x) => !x.budget && !x.actual);
  return (
    <Card className="min-w-0">
      <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent>
        {empty ? <p className="py-10 text-center text-sm text-on-surface-variant">Belum ada budget maupun realisasi.</p> : (
          <div className="h-64" role="img" aria-label={`${title}: budget vs actual per bulan`}>
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={data} margin={{ left: 4, right: 8, top: 8 }} barGap={2}>
                <CartesianGrid vertical={false} stroke="var(--color-chart-grid)" />
                <XAxis dataKey="month" tick={{ fontSize: 11, fill: "var(--color-chart-axis)" }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fontSize: 11, fill: "var(--color-chart-axis)" }} width={56} axisLine={false} tickLine={false} tickFormatter={(v: number) => fmtAxisMoney(v)} />
                <Tooltip cursor={{ fill: "var(--color-surface-container)" }} formatter={(v: number, k: string) => [fmtMoney(v), k === "budget" ? "Budget" : "Actual"]} contentStyle={{ borderRadius: 8, border: "1px solid var(--color-border)", background: "var(--color-surface)", color: "var(--color-on-surface)" }} />
                <Legend formatter={(k: string) => (k === "budget" ? "Budget" : "Actual")} wrapperStyle={{ fontSize: 12 }} />
                <Bar dataKey="budget" fill="var(--color-chart-secondary)" radius={[4, 4, 0, 0]} maxBarSize={18} isAnimationActive={false} />
                <Bar dataKey="actual" fill="var(--color-chart-primary)" radius={[4, 4, 0, 0]} maxBarSize={18} isAnimationActive={false} />
              </BarChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function VarianceText({ kind, v, pct }: { kind: string; v: number; pct?: number | null }) {
  if (!v) return <span className="text-on-surface-variant">—</span>;
  const good = favorable(kind === "cost" ? "cost" : "revenue", v);
  return (
    <span className={cn("inline-flex items-center gap-0.5 whitespace-nowrap", good ? "text-on-success-container" : "text-on-error-container")}>
      <Icon name={v > 0 ? "arrow_upward" : "arrow_downward"} size={12} aria-hidden />
      {fmtMoney(Math.abs(v))}{pct !== undefined && pct !== null ? ` (${fmtPct(pct, { signed: true })})` : ""}
    </span>
  );
}

function BvaTable({ title, rows, total, view, upTo, year, pid, netOnly }: { title: string; rows: BVARow[]; total: BVARow; view: "ytd" | "monthly"; upTo: number; year: number; pid: string | null; netOnly?: boolean }) {
  const nav = useNavigate();
  const all = netOnly ? [total] : [...rows, total];
  const drill = (r: BVARow, month?: number) => (r.category === "total" || r.category === "net" ? null : txnQuery({ kind: r.kind, category: r.category, year, month, propertyId: pid }));
  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1 basis-60">
          <CardTitle>{title}</CardTitle>
          {!netOnly && <CardSubtitle>Klik baris untuk melihat transaksi sumber{view === "monthly" ? "; klik sel bulan untuk transaksi bulan itu" : ""}.</CardSubtitle>}
        </div>
      </CardHeader>
      <CardContent className="px-0 pb-0">
        {!netOnly && rows.length === 0 ? <EmptyState compact icon="query_stats" title={`Belum ada budget atau realisasi ${title.toLowerCase()}`} /> : view === "ytd" ? (
          <Table data-testid={`bva-${netOnly ? "net" : title}`}>
            <THead>
              <tr>
                <TH>Kategori</TH><TH className="bv-num">Budget YTD</TH><TH className="bv-num">Actual YTD</TH><TH className="bv-num">Variance</TH><TH>Status</TH><TH className="bv-num">Budget setahun</TH><TH className="bv-num">Actual setahun</TH>{!netOnly && <TH aria-label="Sumber" />}
              </tr>
            </THead>
            <TBody>
              {all.map((r) => {
                const to = drill(r);
                const isTotal = r.category === "total" || r.category === "net";
                return (
                  <TR key={r.kind + r.category} className={cn(to && "cursor-pointer", isTotal && "bg-surface-container-low font-semibold")} onClick={to ? () => nav(to) : undefined}>
                    <TD>{to ? <Link to={to} className="block max-w-[220px] truncate font-medium hover:underline" title={r.label} onClick={(e) => e.stopPropagation()}>{r.label}</Link> : <CellText max={220}>{r.label}</CellText>}</TD>
                    <TD className="bv-num tnum whitespace-nowrap">{fmtMoney(r.ytd.budget)}</TD>
                    <TD className="bv-num tnum whitespace-nowrap">{fmtMoney(r.ytd.actual)}</TD>
                    <TD className="bv-num tnum whitespace-nowrap"><VarianceText kind={r.kind} v={r.ytd.variance} pct={r.variance_pct} /></TD>
                    <TD>{r.ytd.budget > 0 ? <Badge tone={BVA_STATUS[r.status]?.tone}>{BVA_STATUS[r.status]?.label ?? r.status}</Badge> : <span className="whitespace-nowrap text-xs text-on-surface-variant">tanpa budget</span>}</TD>
                    <TD className="bv-num tnum whitespace-nowrap">{fmtMoney(r.total.budget)}</TD>
                    <TD className="bv-num tnum whitespace-nowrap">{fmtMoney(r.total.actual)}</TD>
                    {!netOnly && (
                      <TD className="whitespace-nowrap text-right" onClick={(e) => e.stopPropagation()}>
                        {(r.sources ?? []).map((s) => { const l = appLink(s.path); return l ? <Link key={s.path} to={l} className="ml-2 whitespace-nowrap text-xs font-semibold text-primary hover:underline">{s.label}</Link> : null; })}
                      </TD>
                    )}
                  </TR>
                );
              })}
            </TBody>
          </Table>
        ) : (
          <Table data-testid={`bva-monthly-${netOnly ? "net" : title}`}>
            <THead>
              <tr><TH className="sticky left-0 z-[1] min-w-[150px]">Kategori</TH>{MONTHS_SHORT.map((m, i) => <TH key={m} className={cn("bv-num", i + 1 > upTo && "opacity-70")}>{m}</TH>)}<TH className="bv-num">Total</TH></tr>
            </THead>
            <TBody>
              {all.map((r) => {
                const isTotal = r.category === "total" || r.category === "net";
                return (
                  <TR key={r.kind + r.category} className={cn(isTotal && "bg-surface-container-low font-semibold")}>
                    <TD className={cn("sticky left-0 z-[1]", isTotal ? "bg-surface-container-low" : "bg-surface")}><CellText max={200}>{r.label}</CellText></TD>
                    {r.months.map((c, i) => {
                      const to = c.actual || c.budget ? drill(r, i + 1) : null;
                      const body = (
                        <>
                          <div className="tnum">{fmtMoneyShort(c.actual)}</div>
                          <div className="text-[11px] tnum text-on-surface-variant">{c.budget ? `/ ${fmtMoneyShort(c.budget)}` : ""}</div>
                        </>
                      );
                      return (
                        <TD key={i} className={cn("bv-num whitespace-nowrap", c.budget > 0 && !favorable(r.kind === "cost" ? "cost" : "revenue", c.variance) && "text-on-error-container")} title={`${MONTHS_LONG[i]}: actual ${fmtMoney(c.actual)} · budget ${fmtMoney(c.budget)}`}>
                          {to ? <Link to={to} className="block hover:underline">{body}</Link> : body}
                        </TD>
                      );
                    })}
                    <TD className="bv-num whitespace-nowrap"><div className="tnum">{fmtMoneyShort(r.total.actual)}</div><div className="text-[11px] tnum text-on-surface-variant">/ {fmtMoneyShort(r.total.budget)}</div></TD>
                  </TR>
                );
              })}
            </TBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

// ---------- drill-down transaksi ----------
function Transactions() {
  const nav = useNavigate();
  const [sp, setSp] = useSearchParams();
  const kind = sp.get("kind") === "cost" ? "cost" : "revenue";
  const category = sp.get("category") ?? "";
  const year = Number(sp.get("year")) || currentYear();
  const month = Number(sp.get("month")) || 0;
  const pid = sp.get("property_id") || null;
  const cats = useGet<Categories>("finance/categories", {}, { staleTime: 10 * 60_000 });
  const label = cats.data?.[kind].find((c) => c.key === category)?.label ?? category;
  const q = useGet<{ data: ActualTxn[] }>("finance/budget-actual/transactions", { property_id: pid ?? undefined, kind, category, year, month: month || undefined }, { enabled: !!category });
  const rows = useMemo(() => q.data?.data ?? [], [q.data]);
  const total = rows.reduce((s, t) => s + t.amount, 0);
  const setMonth = (m: number) => { const n = new URLSearchParams(sp); if (m) n.set("month", String(m)); else n.delete("month"); setSp(n, { replace: true }); };
  const back = `/finance/budget-actual?year=${year}${pid ? `&property_id=${pid}` : ""}`;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Link to={back} className="inline-flex items-center gap-1 text-sm font-semibold text-primary hover:underline"><Icon name="arrow_back" size={16} aria-hidden />Budget vs Actual</Link>
        <NativeSelect className="ml-auto w-44" value={String(month)} onChange={(e) => setMonth(Number(e.target.value))} aria-label="Bulan">
          <option value="0">Setahun {year}</option>
          {MONTHS_LONG.map((m, i) => <option key={m} value={i + 1}>{m} {year}</option>)}
        </NativeSelect>
      </div>
      <div>
        <h2 className="text-h2 font-bold">{kind === "cost" ? "Biaya" : "Pendapatan"} · {label}</h2>
        <p className="text-sm text-on-surface-variant">{month ? `${MONTHS_LONG[month - 1]} ${year}` : `Tahun ${year}`} · {fmtNumber(rows.length)} transaksi · total <b className="tnum text-on-surface">{fmtMoney(total)}</b></p>
      </div>
      {!category ? <Alert variant="warning">Kategori tidak disebutkan pada tautan.</Alert> : q.isLoading ? <CardSkeleton lines={8} /> : q.error && !q.data ? <QueryErrorState error={q.error} onRetry={() => q.refetch()} /> : rows.length === 0 ? (
        <EmptyState icon="receipt_long" title="Tidak ada transaksi" description="Belum ada transaksi sumber untuk kategori & periode ini." />
      ) : (
        <Card>
          <CardContent className="px-0 pb-0 pt-2">
            <Table data-testid="bva-transactions">
              <THead><tr><TH>Tanggal</TH><TH>Sumber</TH><TH>Nomor</TH><TH>Keterangan</TH><TH className="bv-num">Nominal</TH></tr></THead>
              <TBody>
                {rows.map((t) => {
                  const to = appLink(t.link);
                  return (
                    <TR key={t.source_type + t.source_id + t.date + t.amount} className={cn(to && "cursor-pointer")} onClick={to ? () => nav(to) : undefined}>
                      <TD className="whitespace-nowrap tnum">{fmtDay(t.date)}</TD>
                      <TD className="whitespace-nowrap">{SOURCE_TYPE[t.source_type] ?? t.source_type}</TD>
                      <TD className="whitespace-nowrap font-mono text-[13px]">{to ? <Link to={to} className="text-primary hover:underline" onClick={(e) => e.stopPropagation()}>{t.number ?? "Buka"}</Link> : t.number ?? "—"}</TD>
                      <TD><CellText max={320}>{t.description}</CellText></TD>
                      <TD className={cn("bv-num tnum whitespace-nowrap font-semibold", t.amount < 0 && "text-on-error-container")}>{fmtMoney(t.amount)}</TD>
                    </TR>
                  );
                })}
                <TR className="bg-surface-container-low font-semibold"><TD colSpan={4}>Total</TD><TD className="bv-num tnum whitespace-nowrap">{fmtMoney(total)}</TD></TR>
              </TBody>
            </Table>
          </CardContent>
        </Card>
      )}
      {rows.length >= 1000 && <Alert variant="info">Menampilkan 1.000 transaksi pertama — pilih bulan untuk mempersempit.</Alert>}
    </div>
  );
}
