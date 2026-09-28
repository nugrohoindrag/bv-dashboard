// Tab Building Management Overview (29 Sep 2026): ringkasan per area di dalam halaman Overview — Keuangan, Okupansi,
// Penghuni, Operasional, Laporan. Tiap tab memakai kartu gaya m2 (grid .m2-g12) dan menautkan ke halaman detail.
import type { ReactNode } from "react";
import { Link, useNavigate } from "react-router-dom";
import type { UseQueryResult } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import { fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { BuildingState, DomainDashboard, OccupancySummary, OverviewToday } from "@/api/types";
import type { Occupancy as HotelOccupancy } from "@/features/hotel/hotel-api";
import type { Announcement, TRMetrics } from "@/features/tenant-relation/types";
import { chargeTypeLabel } from "@/features/dashboards/dashboard";
import { fmtMoneyShort } from "@/features/finance/fin-utils";
import { kpi } from "./helpers";
import { AttentionTable, BuildingBars, CardHead, DoneToday, GoalList, Load, OpsHero, Overdue, ServiceGauge, SolidHead } from "./widgets";
import { CostCard, FinanceHero, NewsCard, OccupancyCard, OpsCard, RevenueCard, SinkingFundCard, type Residents } from "./bms";

function Empty({ children }: { children: ReactNode }) {
  return <div className="m2-card m2-s12 py-12 text-center m2-caption">{children}</div>;
}

function Stat({ label, value, sub, tone, to }: { label: string; value: ReactNode; sub?: ReactNode; tone?: "red" | "amber" | "green"; to?: string }) {
  const body = (
    <>
      <span className="m2-caption">{label}</span>
      <span className={cn("m2-num-sm", tone === "red" && "m2-red-text", tone === "amber" && "m2-amber-text", tone === "green" && "m2-green-text")}>{value}</span>
      {sub && <span className="m2-caption truncate">{sub}</span>}
    </>
  );
  return to ? <Link to={to} className="m2-stat m2-focus hover:brightness-95">{body}</Link> : <div className="m2-stat">{body}</div>;
}

/** Daftar batang horizontal (label · nilai · bar) untuk rincian kategori. */
function HBars({ rows, format = fmtNumber, color }: { rows: { key: string; label: string; value: number; to?: string }[]; format?: (n: number) => string; color?: string }) {
  const max = Math.max(1, ...rows.map((r) => r.value));
  if (!rows.length) return <p className="m2-caption py-6 text-center">Belum ada data.</p>;
  return (
    <ul className="flex flex-col gap-3">
      {rows.map((r) => {
        const inner = (
          <>
            <div className="flex items-baseline justify-between gap-3"><span className="truncate text-sm font-medium">{r.label}</span><span className="m2-small m2-text-2 tabular-nums">{format(r.value)}</span></div>
            <div className="m2-hbar mt-1.5"><span style={{ width: `${Math.max((r.value / max) * 100, r.value > 0 ? 3 : 0)}%`, background: color }} /></div>
          </>
        );
        return <li key={r.key}>{r.to ? <Link to={r.to} className="m2-focus block rounded-md">{inner}</Link> : inner}</li>;
      })}
    </ul>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Keuangan
// ---------------------------------------------------------------------------------------------------------------------

export function FinanceTab({ fin, apartment, flags }: { fin: UseQueryResult<DomainDashboard>; apartment: boolean; flags: { createInvoice: boolean; payments: boolean; receivables: boolean; sinkingFund: boolean } }) {
  if (fin.isLoading) return <div className="m2-g12"><div className="m2-card m2-s12"><div className="m2-skel h-64" /></div></div>;
  const f = fin.data;
  if (!f) return <div className="m2-g12"><Empty>Data keuangan belum bisa dimuat.</Empty></div>;
  const aging = (f.breakdowns?.aging ?? []).map((r) => ({ key: r.key, label: r.label, value: r.values.amount ?? 0, to: r.drill_down }));
  const types = (f.breakdowns?.revenue_type ?? []).map((r) => ({ key: r.key, label: r.key === "service_charge" && apartment ? "IPL" : chargeTypeLabel(r.key), value: r.values.amount ?? 0, to: r.drill_down }));
  const debtors = f.breakdowns?.overdue_tenant ?? [];
  const sinking = kpi(f, "sinking_fund");
  const ytd = kpi(f, "revenue_ytd");
  const costVar = kpi(f, "budget_cost_variance");
  const pending = kpi(f, "pending_verification");
  const aged = kpi(f, "aging_90");
  return (
    <div className="m2-g12">
      <FinanceHero fin={f} canCreateInvoice={flags.createInvoice} canPayments={flags.payments} canReceivables={flags.receivables} area="m2-s6" />
      <RevenueCard fin={f} apartment={apartment} area="m2-s3" />
      <CostCard fin={f} area="m2-s3" />

      <section className="m2-card m2-s6">
        <CardHead icon="hourglass_bottom" title="Aging piutang" right={aged && aged.value > 0 ? <span className="m2-chip m2-chip-red"><span className="m2-chip-dot m2-bg-red"><Icon name="priority_high" size={11} aria-hidden /></span>{fmtMoneyShort(aged.value)} &gt; 90 hari</span> : <span className="m2-pill">Saat ini</span>} />
        <div className="mt-4"><HBars rows={aging} format={fmtMoneyShort} /></div>
      </section>
      <section className="m2-card m2-s6">
        <CardHead icon="receipt_long" title="Pendapatan per jenis tagihan" right={<span className="m2-pill">30 hari</span>} />
        <div className="mt-4"><HBars rows={types} format={fmtMoneyShort} color="var(--m2-green)" /></div>
      </section>

      <section className="m2-card m2-s8">
        <CardHead icon="pending_actions" title="Tunggakan terbesar" right={flags.receivables ? <Link to="/billing/collections" className="m2-pill">Semua tunggakan</Link> : undefined} />
        {debtors.length === 0 ? <p className="m2-caption py-8 text-center">Tidak ada tunggakan lewat jatuh tempo.</p> : (
          <div className="mt-3 overflow-x-auto">
            <table className="m2-table m2-table-auto">
              <thead><tr><th className="m2-th">Tenant / unit</th><th className="m2-th text-right">Invoice</th><th className="m2-th text-right">Tertua</th><th className="m2-th text-right">Tunggakan</th></tr></thead>
              <tbody>
                {debtors.slice(0, 8).map((r) => (
                  <tr key={r.key}>
                    <td className="font-medium">{r.drill_down ? <Link to={r.drill_down} className="hover:underline">{r.label || "—"}</Link> : r.label || "—"}</td>
                    <td className="text-right tabular-nums">{fmtNumber(r.values.invoices ?? 0)}</td>
                    <td className={cn("text-right tabular-nums", (r.values.oldest_days ?? 0) > 60 && "m2-red-text")}>{fmtNumber(r.values.oldest_days ?? 0)} hari</td>
                    <td className="text-right font-semibold tabular-nums">{fmtMoneyShort(r.values.outstanding ?? 0)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <div className="m2-s4 m2-side grid min-w-0 grid-cols-1 sm:grid-cols-2">
        {apartment && flags.sinkingFund && sinking ? <SinkingFundCard value={sinking.value} to={sinking.drill_down || "/billing/sinking-fund"} /> : null}
        <section className="m2-card flex flex-col gap-2">
          <span className="m2-pill m2-pill-soft self-start">Budget YTD</span>
          <Stat label="Pendapatan vs budget" value={ytd ? `${Math.round(ytd.value)}%` : "–"} tone={ytd && ytd.value < 85 ? "red" : undefined} to={ytd?.drill_down || "/finance/budget-actual"} />
          <Stat label="Biaya vs budget" value={costVar ? `${costVar.value > 0 ? "+" : ""}${Math.round(costVar.value)}%` : "–"} tone={costVar && costVar.value > 5 ? "red" : "green"} to={costVar?.drill_down || "/finance/budget-actual"} />
        </section>
        <Link to={pending?.drill_down || "/billing/payments?status=pending"} className="m2-card m2-focus flex flex-col justify-between gap-3">
          <span className="m2-pill m2-pill-soft self-start">Pembayaran</span>
          <div><div className="m2-num-sm">{fmtNumber(pending?.value ?? 0)}</div><div className="m2-caption">menunggu verifikasi Finance</div></div>
          <span className="m2-round m2-round-dark self-end" aria-hidden><Icon name="open_in_new" size={15} /></span>
        </Link>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Okupansi
// ---------------------------------------------------------------------------------------------------------------------

const UNIT_TYPE_LABEL: Record<string, string> = { residential: "Hunian", commercial: "Komersial / kantor", hotel_room: "Kamar hotel", office: "Kantor", retail: "Retail", parking: "Parkir", storage: "Gudang" };

export function OccupancyTab({ occ, residents, hotels, title, occupantTerm, customerTerm, showCensus, hotelNames }: { occ: UseQueryResult<OccupancySummary>; residents?: Residents; hotels: HotelOccupancy[]; title: string; occupantTerm: string; customerTerm: string; showCensus: boolean; hotelNames: string[] }) {
  const s = occ.data;
  const floors = [...(s?.floors ?? [])].filter((f) => f.total - f.inactive > 0).sort((a, b) => b.vacant - a.vacant).slice(0, 8);
  const types = Object.entries(s?.by_unit_type ?? {}).map(([k, v]) => ({ key: k, label: UNIT_TYPE_LABEL[k] ?? k.replace(/_/g, " "), value: v })).sort((a, b) => b.value - a.value);
  return (
    <div className="m2-g12">
      <section className="m2-card m2-s12">
        <CardHead icon="apartment" title="Ringkasan okupansi unit" right={<Link to="/property/occupancy" className="m2-pill">Buka okupansi</Link>} />
        <div className="mt-4 grid grid-cols-2 gap-2 md:grid-cols-5">
          <Stat label="Okupansi" value={s ? `${Math.round(s.occupancy_pct)}%` : "–"} sub="terisi ÷ unit aktif" />
          <Stat label="Unit aktif" value={s ? fmtNumber(s.total - s.inactive) : "–"} sub={s ? `${fmtNumber(s.inactive)} nonaktif` : undefined} />
          <Stat label="Terisi" value={s ? fmtNumber(s.occupied) : "–"} to="/property/occupancy?status=occupied" />
          <Stat label="Kosong" value={s ? fmtNumber(s.vacant) : "–"} to="/property/occupancy?status=vacant" />
          <Stat label="Dipesan" value={s ? fmtNumber(s.reserved) : "–"} to="/property/occupancy?status=reserved" />
        </div>
      </section>
      <OccupancyCard summary={s} residents={residents} title={title} occupantTerm={occupantTerm} customerTerm={customerTerm} showCensus={showCensus} area="m2-s8" />
      <section className="m2-card m2-s4">
        <CardHead icon="category" title="Unit per jenis" />
        <div className="mt-4"><HBars rows={types} /></div>
      </section>
      <section className={cn("m2-card", hotels.length ? "m2-s7" : "m2-s12")}>
        <CardHead icon="layers" title="Lantai dengan unit kosong terbanyak" />
        {floors.length === 0 ? <p className="m2-caption py-8 text-center">Belum ada data lantai.</p> : (
          <div className="mt-3 overflow-x-auto">
            <table className="m2-table m2-table-auto">
              <thead><tr><th className="m2-th">Lantai</th><th className="m2-th text-right">Unit</th><th className="m2-th text-right">Terisi</th><th className="m2-th text-right">Kosong</th><th className="m2-th text-right">Okupansi</th></tr></thead>
              <tbody>
                {floors.map((f) => (
                  <tr key={f.location_id}>
                    <td className="font-medium">{f.path_text || f.name}</td>
                    <td className="text-right tabular-nums">{fmtNumber(f.total - f.inactive)}</td>
                    <td className="text-right tabular-nums">{fmtNumber(f.occupied)}</td>
                    <td className="text-right font-semibold tabular-nums">{fmtNumber(f.vacant)}</td>
                    <td className="text-right tabular-nums">{Math.round(f.occupancy_pct)}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      {hotels.length > 0 && (
        <section className="m2-card m2-s5">
          <CardHead icon="hotel" title="Okupansi kamar hotel hari ini" right={<Link to="/reception" className="m2-pill">Reception</Link>} />
          <ul className="mt-3 flex flex-col gap-2">
            {hotels.map((h, i) => (
              <li key={i} className="m2-list-item flex-col items-stretch gap-2">
                <div className="flex items-baseline justify-between gap-2"><span className="truncate text-sm font-medium">{hotelNames[i] ?? "Hotel"}</span><span className="m2-num-sm">{Math.round(h.occupancy_pct)}%</span></div>
                <div className="m2-hbar"><span style={{ width: `${Math.max(h.occupancy_pct, 2)}%` }} /></div>
                <div className="m2-caption flex flex-wrap gap-x-3 tabular-nums">
                  <span>{fmtNumber(h.occupied)}/{fmtNumber(h.total_rooms)} kamar</span><span>{fmtNumber(h.arrivals_today)} tiba</span><span>{fmtNumber(h.departures_today)} berangkat</span>
                  {h.dirty_rooms > 0 && <span className="m2-red-text">{fmtNumber(h.dirty_rooms)} kotor</span>}
                </div>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Penghuni
// ---------------------------------------------------------------------------------------------------------------------

const OWNERSHIP_LABEL: Record<string, string> = { owner: "Pemilik", tenant: "Penyewa", family: "Keluarga", guest: "Tamu", employee: "Karyawan" };

export function ResidentsTab({ residents, tr, news, occupantTerm, customerTerm, flags }: { residents?: UseQueryResult<Residents>; tr?: UseQueryResult<TRMetrics>; news?: UseQueryResult<{ data: Announcement[] }>; occupantTerm: string; customerTerm: string; flags: { tenantUsers: boolean; tenants: boolean; createAnnouncement: boolean } }) {
  const nav = useNavigate();
  const r = residents?.data;
  const m = tr?.data;
  const series = (m?.series ?? []).slice(-30);
  const maxDay = Math.max(1, ...series.map((p) => Math.max(p.created, p.resolved)));
  return (
    <div className="m2-g12">
      <section className="m2-card m2-s6 flex flex-col">
        <CardHead icon="groups" title={`Sensus ${occupantTerm.toLowerCase()}`} right={<span className="m2-pill">Saat ini</span>} />
        {residents?.isLoading ? <div className="m2-skel mt-4 h-40" /> : !r ? <p className="m2-caption py-8 text-center">Data sensus belum tersedia.</p> : (
          <>
            <div className="mt-4 flex flex-wrap items-baseline gap-3">
              <span className="m2-hero">{fmtNumber(r.occupants_active)}</span>
              <span className="m2-caption">{occupantTerm.toLowerCase()} aktif di {fmtNumber(r.units_with_occupants)} unit · {fmtNumber(r.tenants_active)} {customerTerm.toLowerCase()}</span>
            </div>
            <ul className="mt-3 flex flex-wrap gap-1.5">
              {Object.entries(r.app_users).sort((a, b) => b[1] - a[1]).map(([k, v]) => <li key={k} className="m2-pill m2-pill-card">{OWNERSHIP_LABEL[k] ?? "Lainnya"} <b className="tabular-nums">{fmtNumber(v)}</b></li>)}
            </ul>
            <div className="m2-small m2-text-2 mt-2 tabular-nums">30 hari: {fmtNumber(r.moved_in_30d)} masuk · {fmtNumber(r.moved_out_30d)} keluar</div>
            <div className="mt-auto grid grid-cols-1 gap-2 pt-5 sm:grid-cols-3">
              {flags.tenantUsers && <button type="button" className="m2-btn m2-btn-blue" onClick={() => nav("/tenant-relation/tenant-users")}><Icon name="how_to_reg" size={17} aria-hidden />Validasi{r.pending_validation ? ` (${fmtNumber(r.pending_validation)})` : ""}</button>}
              {flags.tenants && <button type="button" className="m2-btn m2-btn-dark" onClick={() => nav("/tenant/tenants")}><Icon name="groups" size={17} aria-hidden />Daftar {customerTerm.toLowerCase()}</button>}
              {flags.createAnnouncement && <button type="button" className="m2-btn m2-btn-light" onClick={() => nav("/tenant-relation/announcements")}><Icon name="campaign" size={17} aria-hidden />Pengumuman</button>}
            </div>
          </>
        )}
      </section>
      <section className="m2-card m2-s6">
        <CardHead icon="support_agent" title="Layanan tenant" right={<Link to="/tenant-relation" className="m2-pill">Dashboard layanan</Link>} />
        {tr?.isLoading ? <div className="m2-skel mt-4 h-40" /> : !m ? <p className="m2-caption py-8 text-center">Data layanan tenant belum tersedia.</p> : (
          <div className="mt-4 grid grid-cols-2 gap-2 md:grid-cols-3">
            <Stat label="Tiket terbuka" value={fmtNumber(m.open_tickets)} to={m.drill_down?.open_tickets} />
            <Stat label="Berisiko SLA" value={fmtNumber(m.sla_risk)} tone={m.sla_risk > 0 ? "amber" : undefined} to={m.drill_down?.sla_risk} />
            <Stat label="Melewati SLA" value={fmtNumber(m.overdue)} tone={m.overdue > 0 ? "red" : undefined} to={m.drill_down?.overdue} />
            <Stat label="Selesai hari ini" value={fmtNumber(m.resolved_today)} to={m.drill_down?.resolved_today} />
            <Stat label="CSAT" value={m.csat === null ? "–" : m.csat.toFixed(1)} sub={`${fmtNumber(m.csat_count)} penilaian`} />
            <Stat label="Reopen" value={m.reopen_rate_pct === null ? "–" : `${Math.round(m.reopen_rate_pct)}%`} sub={m.avg_resolution_hours === null ? undefined : `selesai rata-rata ${Math.round(m.avg_resolution_hours)} jam`} />
          </div>
        )}
      </section>

      <section className="m2-card m2-s7">
        <CardHead icon="bar_chart" title="Permintaan 30 hari" right={m ? <span className="m2-pill">{fmtNumber(m.requests_30d)} permintaan</span> : undefined} />
        {!series.length ? <p className="m2-caption py-8 text-center">Belum ada data harian.</p> : (
          <>
            <div className="m2-caption mt-3 flex gap-3"><span className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: "var(--m2-blue)" }} />Masuk</span><span className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: "var(--m2-lime)" }} />Selesai</span></div>
            <div className="m2-daybars mt-2" aria-label="Permintaan masuk vs selesai per hari">
              {series.map((p) => (
                <div key={p.date} className="m2-daybar" title={`${p.date}: ${p.created} masuk · ${p.resolved} selesai`}>
                  <span style={{ height: `${(p.created / maxDay) * 100}%`, background: "var(--m2-blue)" }} />
                  <span style={{ height: `${(p.resolved / maxDay) * 100}%`, background: "var(--m2-lime)" }} />
                </div>
              ))}
            </div>
            <div className="m2-caption mt-1 flex justify-between"><span>{series[0]?.date.slice(5)}</span><span>{series[series.length - 1]?.date.slice(5)}</span></div>
          </>
        )}
      </section>
      <section className="m2-card m2-s5">
        <CardHead icon="category" title="Permintaan per kategori" />
        <div className="mt-4"><HBars rows={(m?.by_category ?? []).slice(0, 6).map((c) => ({ key: c.key, label: c.label, value: c.count }))} /></div>
        {m && (m.recurring_issues_open > 0 || m.general_feedback_new > 0) && (
          <div className="mt-4 flex flex-wrap gap-2">
            {m.recurring_issues_open > 0 && <Link to="/tenant-relation/recurring-issues" className="m2-chip m2-chip-red"><span className="m2-chip-dot m2-bg-red"><Icon name="repeat" size={11} aria-hidden /></span>{fmtNumber(m.recurring_issues_open)} isu berulang</Link>}
            {m.general_feedback_new > 0 && <Link to="/tenant-relation/feedback/general" className="m2-pill m2-pill-soft">{fmtNumber(m.general_feedback_new)} feedback baru</Link>}
          </div>
        )}
      </section>
      {news && <NewsCard items={news.data?.data ?? []} loading={news.isLoading} canCreate={flags.createAnnouncement} area="m2-s12" />}
    </div>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Operasional
// ---------------------------------------------------------------------------------------------------------------------

export function OperationsTab({ d, loading, building, propertyId, propName, propertyName, can, svc }: { d?: OverviewToday; loading: boolean; building: UseQueryResult<{ data: BuildingState[] }>; propertyId: string | null; propName: string; propertyName?: (id?: string) => string | undefined; can: (p: string) => boolean; svc: { tr?: UseQueryResult<TRMetrics>; eng?: DomainDashboard; sec?: DomainDashboard; hk?: DomainDashboard; fin?: DomainDashboard; loading: boolean } }) {
  return (
    <div className="m2-g12">
      <OpsHero d={d} loading={loading} buildings={building.data?.data ?? []} propName={propName} can={can} area="m2-s6" />
      <section className="m2-card m2-s3 flex flex-col"><SolidHead icon="arrow_downward" tone="green" title="Selesai hari ini" /><DoneToday d={d} loading={loading} /></section>
      <section className="m2-card m2-s3 flex flex-col"><Overdue d={d} loading={loading} /></section>
      <section className="m2-card m2-s4 flex flex-col">
        <CardHead icon="speed" title="Kinerja layanan" />
        <div className="m2-sub mt-4 flex flex-1 flex-col gap-2 p-2">
          <ServiceGauge tr={svc.tr} eng={svc.eng} />
          <GoalList eng={svc.eng} sec={svc.sec} hk={svc.hk} fin={svc.fin} loading={svc.loading} />
        </div>
      </section>
      <div className="m2-s8 m2-side grid min-w-0">
        <section className="m2-card flex flex-col">
          <CardHead icon="bar_chart" title="Beban per gedung" right={<span className="m2-pill">Hari ini</span>} />
          <Load query={building} className="mt-4 flex-1">{(res) => <BuildingBars rows={res.data} propertyName={propertyName} />}</Load>
        </section>
        <OpsCard d={d} loading={loading} area="" />
      </div>
      <section className="m2-card m2-s12"><AttentionTable propertyId={propertyId} /></section>
    </div>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Laporan
// ---------------------------------------------------------------------------------------------------------------------

const REPORT_GROUPS: { key: string; title: string; icon: string; names: string[] }[] = [
  { key: "finance", title: "Keuangan", icon: "account_balance_wallet", names: ["billing", "aging", "collection", "revenue", "ipl", "sinking-fund", "budget-actual", "operating-cost"] },
  { key: "ops", title: "Operasional", icon: "engineering", names: ["operations-kpi", "tasks", "sla", "backlog", "work-orders", "maintenance", "patrol-cleaning", "team-performance"] },
  { key: "service", title: "Layanan tenant & fasilitas", icon: "support_agent", names: ["service-requests", "facilities", "visitors"] },
  { key: "safety", title: "Keamanan & insiden", icon: "shield", names: ["security", "incidents"] },
  { key: "supply", title: "Vendor & inventori", icon: "inventory_2", names: ["vendors", "inventory"] },
];
// Istilah UI: "Preventive Maintenance" tidak disingkat (NC / keputusan user 29 Sep 2026)
const REPORT_TITLE: Record<string, string> = { maintenance: "Preventive Maintenance Compliance" };

export function ReportsTab({ catalog }: { catalog: UseQueryResult<{ data: { name: string; title: string; description: string }[] }> }) {
  if (catalog.isLoading) return <div className="m2-g12"><div className="m2-card m2-s12"><div className="m2-skel h-64" /></div></div>;
  const list = catalog.data?.data ?? [];
  if (!list.length) return <div className="m2-g12"><Empty>Katalog laporan belum tersedia.</Empty></div>;
  const byName = new Map(list.map((r) => [r.name, r]));
  const grouped = new Set(REPORT_GROUPS.flatMap((g) => g.names));
  const groups = [...REPORT_GROUPS, { key: "other", title: "Lainnya", icon: "description", names: list.map((r) => r.name).filter((n) => !grouped.has(n)) }]
    .map((g) => ({ ...g, items: g.names.map((n) => byName.get(n)).filter((r): r is NonNullable<typeof r> => !!r) }))
    .filter((g) => g.items.length)
    // grup ≤ 4 laporan setengah lebar; bila jumlah grup setengah-lebar berurutan ganjil, yang terakhir dibuat penuh
    .map((g) => ({ ...g, wide: g.items.length > 4 }));
  for (let i = 0; i < groups.length; ) {
    if (groups[i].wide) { i++; continue; }
    let j = i;
    while (j < groups.length && !groups[j].wide) j++;
    if ((j - i) % 2 === 1) groups[j - 1].wide = true;
    i = j;
  }
  return (
    <div className="m2-g12">
      {groups.map((g) => (
        <section key={g.key} className={cn("m2-card", g.wide ? "m2-s12" : "m2-s6")}>
          <CardHead icon={g.icon} title={g.title} right={<span className="m2-pill">{g.items.length} laporan</span>} />
          <div className={cn("mt-4 grid grid-cols-1 gap-2 sm:grid-cols-2", g.wide && "xl:grid-cols-4")}>
            {g.items.map((r) => (
              <Link key={r.name} to={`/reports/${r.name}`} className="m2-report m2-focus">
                <span className="flex items-center justify-between gap-2"><span className="text-sm font-semibold">{REPORT_TITLE[r.name] ?? r.title}</span><Icon name="open_in_new" size={15} className="m2-muted" aria-hidden /></span>
                <span className="m2-caption line-clamp-2">{r.description.replace(/\bPM\b/g, "Preventive Maintenance")}</span>
              </Link>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}
