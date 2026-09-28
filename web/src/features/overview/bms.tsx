// Widget Building Management Overview (29 Sep 2026, Roadmap v2.1 §25.2–§25.3): keuangan (tagihan, IPL/service charge,
// biaya, Sinking Fund), okupansi & sensus penghuni, hotel, operasional ringkas, serta Berita & Memo. Gaya m2.css.
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import { RelativeTime } from "@/components/bv/common";
import { fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Counter, DomainDashboard, OccupancySummary, OverviewToday } from "@/api/types";
import type { Occupancy as HotelOccupancy } from "@/features/hotel/hotel-api";
import type { Announcement } from "@/features/tenant-relation/types";
import { chargeTypeLabel } from "@/features/dashboards/dashboard";
import { fmtMoneyShort } from "@/features/finance/fin-utils";
import { kpi } from "./helpers";
import { CardHead, SolidHead } from "./widgets";

export interface Residents {
  occupants_active: number;
  units_with_occupants: number;
  moved_in_30d: number;
  moved_out_30d: number;
  tenants_active: number;
  app_users: Record<string, number>;
  pending_validation: number;
  link: string;
}

const bd = (c: Counter | undefined, key: string) => c?.breakdown?.[key] ?? 0;
const breakdown = (fin: DomainDashboard | undefined, key: string) => fin?.breakdowns?.[key] ?? [];

// ---------------------------------------------------------------------------------------------------------------------
// Keuangan
// ---------------------------------------------------------------------------------------------------------------------

const AGING_SHORT: Record<string, string> = { current: "Blm", "1_30": "30", "31_60": "60", "61_90": "90", "90_plus": ">90" };

/** Kartu utama (gaya "My balance"): sisa tagihan terbuka, porsi lewat jatuh tempo, heatmap aging, aksi Finance. */
export function FinanceHero({ fin, canCreateInvoice, canPayments, canReceivables, area = "m2-a-fin" }: { fin: DomainDashboard; canCreateInvoice: boolean; canPayments: boolean; canReceivables: boolean; area?: string }) {
  const nav = useNavigate();
  const outstanding = kpi(fin, "outstanding");
  const overdue = kpi(fin, "overdue");
  const pending = kpi(fin, "pending_verification")?.value ?? 0;
  const aging = breakdown(fin, "aging");
  return (
    <section className={cn("m2-card flex flex-col", area)}>
      <CardHead icon="request_quote" title="Tagihan & penagihan" right={<><span className="m2-pill">IDR</span><span className="m2-pill">Saat ini</span></>} />
      <div className="mt-4 flex flex-wrap items-end justify-between gap-4">
        <div className="min-w-0">
          <div className="m2-caption">Sisa tagihan terbuka</div>
          <div className="flex items-center gap-3">
            <span className="m2-hero" title={outstanding ? new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(outstanding.value) : undefined}>{fmtMoneyShort(outstanding?.value ?? 0)}</span>
            <Link to={outstanding?.drill_down || "/billing/invoices?open=true"} className="m2-round m2-round-lg" aria-label="Lihat tagihan terbuka"><Icon name="visibility" size={18} aria-hidden /></Link>
          </div>
          <div className="m2-small mt-2 flex flex-wrap items-center gap-2 m2-text-2">
            {(overdue?.value ?? 0) > 0 ? (
              <><span className="m2-chip-dot m2-bg-red"><Icon name="priority_high" size={11} aria-hidden /></span><span><b className="m2-red-text">{fmtMoneyShort(overdue!.value)}</b> lewat jatuh tempo</span></>
            ) : (
              <><span className="m2-chip-dot m2-bg-green"><Icon name="check" size={11} aria-hidden /></span><span>Tidak ada tagihan lewat jatuh tempo</span></>
            )}
          </div>
        </div>
        <AgingPixels rows={aging} />
      </div>
      <div className="mt-5 grid grid-cols-1 gap-2 sm:grid-cols-3">
        {canCreateInvoice && <button type="button" className="m2-btn m2-btn-blue" onClick={() => nav("/billing/invoices?new=1")}><Icon name="add" size={18} aria-hidden />Buat tagihan</button>}
        {canPayments && <button type="button" className="m2-btn m2-btn-dark" onClick={() => nav("/billing/payments?status=pending")}><Icon name="fact_check" size={17} aria-hidden />Verifikasi bayar{pending > 0 ? ` (${fmtNumber(pending)})` : ""}</button>}
        {canReceivables && <button type="button" className="m2-btn m2-btn-light" onClick={() => nav("/billing/collections")}><Icon name="pending_actions" size={17} aria-hidden />Tunggakan</button>}
      </div>
    </section>
  );
}

/** Heatmap aging piutang: satu kolom per rentang umur, tinggi = nominal, makin tua makin gelap. */
function AgingPixels({ rows }: { rows: { key: string; label: string; values: Record<string, number> }[] }) {
  if (!rows.length) return null;
  const max = Math.max(1, ...rows.map((r) => r.values.amount ?? 0));
  const shade = ["m2-pixel-1", "m2-pixel-1", "m2-pixel-2", "m2-pixel-3", "m2-pixel-3"];
  return (
    <div aria-label="Aging piutang">
      <div className="m2-pixels m2-pixels-wide">
        {rows.flatMap((r, ci) => {
          const v = r.values.amount ?? 0;
          const h = v > 0 ? Math.max(1, Math.round((v / max) * 6)) : 0;
          return Array.from({ length: 6 }).map((_, ri) => {
            const fromBottom = 5 - ri;
            const cls = v === 0 ? (fromBottom === 0 ? "m2-pixel-l" : "") : fromBottom < h ? shade[ci] ?? "m2-pixel-3" : "";
            return <span key={`${r.key}-${ri}`} className={cn("m2-pixel", cls)} title={ri === 5 ? `${r.label}: ${fmtMoneyShort(v)}` : undefined} />;
          });
        })}
      </div>
      <div className="m2-pixel-axis">{rows.map((r) => <span key={r.key}>{AGING_SHORT[r.key] ?? r.label}</span>)}</div>
    </div>
  );
}

/** Kartu gaya "Income": pendapatan periode + collection rate + dua jenis tagihan terbesar (IPL/service charge, sewa, …). */
export function RevenueCard({ fin, apartment, area = "m2-a-cash" }: { fin: DomainDashboard; apartment: boolean; area?: string }) {
  const revenue = kpi(fin, "revenue");
  const rate = kpi(fin, "collection_rate");
  const cash = kpi(fin, "cash_collected");
  const types = breakdown(fin, "revenue_type").slice(0, 2);
  const label = (code: string) => (code === "service_charge" && apartment ? "IPL" : chargeTypeLabel(code));
  const colors = ["var(--m2-blue)", "var(--m2-lime)"];
  return (
    <section className={cn("m2-card flex flex-col", area)}>
      <SolidHead icon="arrow_downward" tone="green" title="Pendapatan" pill="30 hari" />
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <span className="m2-num">{fmtMoneyShort(revenue?.value ?? 0)}</span>
        {rate && <span className={cn("m2-chip", rate.value >= 90 ? "m2-chip-green" : "m2-chip-red")}><span className={cn("m2-chip-dot", rate.value >= 90 ? "m2-bg-green" : "m2-bg-red")}><Icon name={rate.value >= 90 ? "trending_up" : "trending_down"} size={11} aria-hidden /></span>{Math.round(rate.value)}%</span>}
      </div>
      <p className="m2-small m2-sub mt-3 inline-block self-start px-3 py-1.5 m2-text-2">
        Kas diterima <b className="m2-green-text">{fmtMoneyShort(cash?.value ?? 0)}</b> · collection rate {rate ? `${Math.round(rate.value)}%` : "–"}
      </p>
      <div className="mt-auto flex flex-wrap gap-x-6 gap-y-3 pt-4">
        {types.map((t, i) => (
          <Link key={t.key} to={t.drill_down || "/billing/invoices"} className="m2-focus flex gap-2.5 rounded-md">
            <span className="m2-split-bar" style={{ background: colors[i] }} />
            <span>
              <span className="m2-caption block">{label(t.key)}</span>
              <span className="block text-lg font-semibold tabular-nums">{fmtMoneyShort(t.values.amount ?? 0)}</span>
            </span>
          </Link>
        ))}
        {!types.length && <span className="m2-caption">Belum ada tagihan terbit dalam 30 hari.</span>}
      </div>
    </section>
  );
}

/** Kartu gaya "Expense": biaya operasional vs budget + komposisi kategori biaya (YTD). */
export function CostCard({ fin, area = "m2-a-cost" }: { fin: DomainDashboard; area?: string }) {
  const cost = kpi(fin, "operating_cost");
  const variance = kpi(fin, "budget_cost_variance");
  const cats = breakdown(fin, "cost_category").map((c) => ({ key: c.key, label: c.label, value: c.values.ytd ?? 0 })).sort((a, b) => b.value - a.value);
  const top = cats.slice(0, 2);
  const rest = cats.slice(2).reduce((n, c) => n + c.value, 0);
  const segs = [
    ...top.map((c, i) => ({ ...c, cls: i === 0 ? "m2-seg-dark" : "m2-seg-blue", dot: i === 0 ? "var(--m2-dark)" : "var(--m2-blue)" })),
    ...(rest > 0 ? [{ key: "rest", label: "Lainnya", value: rest, cls: "m2-seg-lime", dot: "var(--m2-lime)" }] : []),
  ];
  const sum = Math.max(1, segs.reduce((n, s) => n + s.value, 0));
  const over = (variance?.value ?? 0) > 0;
  return (
    <section className={cn("m2-card flex flex-col", area)}>
      <div className="m2-sub -mx-1 -mt-1 p-3 pb-4">
        <SolidHead icon="arrow_upward" tone="red" title="Biaya operasional" pill="30 hari" />
        <div className="mt-4"><span className="m2-num">{fmtMoneyShort(cost?.value ?? 0)}</span></div>
        {variance && (
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <span className={cn("m2-chip", over ? "m2-chip-red" : "m2-chip-green")}><span className={cn("m2-chip-dot", over ? "m2-bg-red" : "m2-bg-green")}><Icon name={over ? "trending_up" : "trending_down"} size={11} aria-hidden /></span>{Math.abs(Math.round(variance.value))}%</span>
            <Link to={variance.drill_down || "/finance/budget-actual"} className="m2-caption hover:underline">{over ? "di atas budget YTD" : "di bawah budget YTD"}</Link>
          </div>
        )}
      </div>
      <div className="mt-auto pt-5">
        {segs.length ? (
          <>
            <div className="mb-2 flex justify-between m2-caption tabular-nums">
              <span>{Math.round((segs[0].value / sum) * 100)}% {segs[0].label}</span>
              {segs.length > 1 && <span>{Math.round((segs[segs.length - 1].value / sum) * 100)}%</span>}
            </div>
            <div className="m2-seg" aria-label="Komposisi biaya YTD">
              {segs.map((s) => <span key={s.key} className={s.cls} style={{ flexGrow: Math.max(s.value, 0.001), flexBasis: 0 }} title={`${s.label}: ${fmtMoneyShort(s.value)}`} />)}
            </div>
            <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 m2-small m2-text-2">
              {segs.map((s) => <span key={s.key} className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: s.dot }} />{s.label}</span>)}
            </div>
          </>
        ) : <span className="m2-caption">Belum ada biaya tercatat tahun ini.</span>}
      </div>
    </section>
  );
}

/** Sinking Fund (Apartment) — kartu sorotan gaya "Get Premium Feature". */
export function SinkingFundCard({ value, to }: { value: number; to: string }) {
  return (
    <section className="m2-card m2-promo sm:col-span-2 flex flex-col justify-between">
      <PromoLines />
      <div className="relative flex items-center justify-between gap-2">
        <span className="m2-round m2-round-sm" aria-hidden><Icon name="savings" size={14} /></span>
        <span className="flex items-center gap-1.5">
          <Link to={to} className="m2-pill m2-pill-dark">Lihat mutasi</Link>
          <Link to={to} className="m2-round m2-round-sm" aria-label="Buka Sinking Fund"><Icon name="open_in_new" size={14} aria-hidden /></Link>
        </span>
      </div>
      <div className="relative">
        <span className="m2-pill m2-pill-blue">Saldo Sinking Fund</span>
        <div className="m2-promo-title mt-2">{fmtMoneyShort(value)}</div>
        <div className="m2-caption">Dana cadangan pemeliharaan besar bagian bersama</div>
      </div>
    </section>
  );
}

function PromoLines() {
  return (
    <svg className="m2-promo-lines" viewBox="0 0 400 160" preserveAspectRatio="none" aria-hidden>
      <path d="M150 0 L150 60 L230 60 L230 160 M260 0 L260 40 L340 40 L340 160 M300 0 L300 20 L400 20 M180 160 L180 110 L120 110" fill="none" stroke="currentColor" strokeWidth="14" strokeLinejoin="round" />
    </svg>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Okupansi & sensus penghuni
// ---------------------------------------------------------------------------------------------------------------------

const OWNERSHIP_LABEL: Record<string, string> = { owner: "Pemilik", tenant: "Penyewa", family: "Keluarga", guest: "Tamu", employee: "Karyawan" };

/**
 * Okupansi unit per gedung (grafik batang, % terisi) + panel sensus: penghuni aktif, komposisi kepemilikan akun Tenant App,
 * menunggu validasi, dan mutasi 30 hari. Judul & istilah mengikuti profile (Apartment: Sensus Penghuni; Office: Tenant).
 */
export function OccupancyCard({ summary, residents, title, occupantTerm, customerTerm, showCensus, area = "m2-a-occ" }: { summary?: OccupancySummary; residents?: Residents; title: string; occupantTerm: string; customerTerm: string; showCensus: boolean; area?: string }) {
  const buildings = summary?.buildings ?? [];
  return (
    <section className={cn("m2-card flex flex-col", area)}>
      <CardHead icon="apartment" title={title} right={<><span className="m2-pill">{summary ? `${Math.round(summary.occupancy_pct)}% terisi` : "–"}</span><Link to="/property/occupancy" className="m2-round" aria-label="Buka okupansi"><Icon name="open_in_new" size={15} aria-hidden /></Link></>} />
      <div className="m2-occ-grid mt-4 flex-1">
        <div className="min-w-0">
          {summary ? <OccupancyBars rows={buildings} /> : <div className="m2-skel h-48" />}
        </div>
        <div className="m2-sub flex flex-col gap-3 p-4">
          <div>
            <div className="m2-caption">Unit terisi</div>
            <div className="flex items-baseline gap-1.5"><span className="m2-num-sm">{summary ? fmtNumber(summary.occupied) : "–"}</span><span className="m2-caption">dari {summary ? fmtNumber(summary.total - summary.inactive) : "–"} unit aktif</span></div>
            {summary && <div className="m2-caption mt-0.5">{fmtNumber(summary.vacant)} kosong · {fmtNumber(summary.reserved)} dipesan</div>}
          </div>
          {showCensus && residents && (
            <>
              <div className="border-t pt-3" style={{ borderColor: "var(--m2-line)" }}>
                <div className="m2-caption">{occupantTerm} aktif</div>
                <div className="flex items-baseline gap-1.5"><span className="m2-num-sm">{fmtNumber(residents.occupants_active)}</span><span className="m2-caption">di {fmtNumber(residents.units_with_occupants)} unit · {fmtNumber(residents.tenants_active)} {customerTerm.toLowerCase()}</span></div>
              </div>
              <ul className="flex flex-wrap gap-1.5">
                {Object.entries(residents.app_users).sort((a, b) => b[1] - a[1]).map(([k, v]) => (
                  <li key={k} className="m2-pill m2-pill-card">{OWNERSHIP_LABEL[k] ?? "Lainnya"} <b className="tabular-nums">{fmtNumber(v)}</b></li>
                ))}
              </ul>
              <div className="m2-small m2-text-2 flex flex-wrap items-center gap-x-3 gap-y-1">
                <span className="tabular-nums">30 hari: {fmtNumber(residents.moved_in_30d)} masuk · {fmtNumber(residents.moved_out_30d)} keluar</span>
              </div>
              {residents.pending_validation > 0 && (
                <Link to="/tenant-relation/tenant-users" className="m2-chip m2-chip-red self-start"><span className="m2-chip-dot m2-bg-red"><Icon name="how_to_reg" size={11} aria-hidden /></span>{fmtNumber(residents.pending_validation)} menunggu validasi</Link>
              )}
            </>
          )}
        </div>
      </div>
    </section>
  );
}

export function OccupancyBars({ rows }: { rows: OccupancySummary["buildings"] }) {
  const [active, setActive] = useState<number | null>(null);
  if (!rows.length) return <p className="m2-caption py-10 text-center">Belum ada unit terdaftar.</p>;
  const n = rows.length;
  const cols = { gridTemplateColumns: `repeat(${n}, minmax(0, 1fr))` };
  const tip = active !== null ? rows[active] : null;
  const rightSide = active !== null && active < n / 2;
  const edgePct = active === null ? 0 : ((rightSide ? active + 1 : active) / n) * 100;
  return (
    <div>
      <div className="m2-caption mb-2 flex flex-wrap items-center gap-x-3 gap-y-1">
        <span>Okupansi per gedung · {n} gedung/tower</span>
        <span className="flex-1 border-t border-dashed" style={{ borderColor: "var(--m2-line)" }} />
        <span className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: "var(--m2-blue)" }} />Terisi</span>
        <span className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: "var(--m2-lime)" }} />Dipesan</span>
      </div>
      <div className="m2-chart-scroll">
        <div style={{ minWidth: n * 64 }}>
          <div className="m2-bars" style={cols} onMouseLeave={() => setActive(null)}>
            {rows.map((b, i) => {
              const base = Math.max(1, b.total - b.inactive);
              const occ = (b.occupied / base) * 82;
              const res = (b.reserved / base) * 82;
              return (
                <button key={b.location_id} type="button" className="m2-bar m2-focus" onMouseEnter={() => setActive(i)} onFocus={() => setActive(i)} onBlur={() => setActive(null)} aria-label={`${b.name}: ${Math.round(b.occupancy_pct)}% terisi, ${b.occupied} dari ${base} unit`}>
                  <span className="m2-bar-bg" style={{ height: "82%" }} />
                  {(b.occupied > 0 || b.reserved > 0) && (
                    <span className="m2-bar-stack" style={{ height: `${Math.max(occ + res, 6)}%` }}>
                      {b.reserved > 0 && <span style={{ flex: `${b.reserved} 1 0`, background: "var(--m2-lime)" }} />}
                      {b.occupied > 0 && <span style={{ flex: `${b.occupied} 1 0`, background: "var(--m2-blue)" }} />}
                    </span>
                  )}
                  <span className="m2-bar-val" style={{ bottom: "calc(82% + 4px)" }}>{Math.round(b.occupancy_pct)}%</span>
                </button>
              );
            })}
            {tip && (
              <div className="m2-tooltip" style={rightSide ? { left: `calc(${edgePct}% + 6px)`, top: 4 } : { right: `calc(${100 - edgePct}% + 6px)`, top: 4 }}>
                <span className="m2-tooltip-chip">{tip.name}</span>
                <div className="m2-small mt-2 flex justify-between gap-2"><span className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: "var(--m2-blue)" }} />Terisi</span><b className="tabular-nums">{fmtNumber(tip.occupied)}</b></div>
                <div className="m2-small mt-1 flex justify-between gap-2"><span className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: "var(--m2-lime)" }} />Dipesan</span><b className="tabular-nums">{fmtNumber(tip.reserved)}</b></div>
                <div className="m2-small mt-1 flex justify-between gap-2 opacity-80"><span>Kosong</span><b className="tabular-nums">{fmtNumber(tip.vacant)}</b></div>
              </div>
            )}
          </div>
          <div className="m2-bar-labels" style={cols}>
            {rows.map((b) => (
              <span key={b.location_id} className="min-w-0" title={b.path_text || b.name}>
                <span className="m2-bar-name">{b.name}</span>
                <span className="m2-bar-prop">{fmtNumber(b.total - b.inactive)} unit</span>
              </span>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Hotel · Operasional · Berita & Memo
// ---------------------------------------------------------------------------------------------------------------------

/** Hotel hari ini (gabungan seluruh property hotel dalam scope). */
export function HotelCard({ rows }: { rows: HotelOccupancy[] }) {
  const total = rows.reduce((n, r) => n + r.total_rooms, 0);
  const occ = rows.reduce((n, r) => n + r.occupied, 0);
  const pct = total ? Math.round((occ / total) * 100) : 0;
  const sum = (k: keyof HotelOccupancy) => rows.reduce((n, r) => n + (Number(r[k]) || 0), 0);
  return (
    <Link to="/reception" className="m2-card m2-focus flex flex-col justify-between gap-3">
      <div className="flex items-center justify-between">
        <span className="m2-pill m2-pill-soft">Okupansi kamar hari ini</span>
        <span className="m2-round m2-round-sm" aria-hidden><Icon name="open_in_new" size={14} /></span>
      </div>
      <div className="flex items-baseline gap-1.5"><span className="m2-num-sm">{pct}%</span><span className="m2-caption">{fmtNumber(occ)} dari {fmtNumber(total)} kamar</span></div>
      <div className="m2-small m2-text-2 flex flex-wrap gap-x-3 gap-y-1 tabular-nums">
        <span>{fmtNumber(sum("arrivals_today"))} tiba</span><span>{fmtNumber(sum("departures_today"))} berangkat</span><span>{fmtNumber(sum("in_house"))} menginap</span>
        {sum("dirty_rooms") > 0 && <span className="m2-red-text">{fmtNumber(sum("dirty_rooms"))} kamar kotor</span>}
      </div>
    </Link>
  );
}

/** Operasional hari ini — ringkasan tiket/pekerjaan (bukan fokus utama Building Management Overview). */
export function OpsCard({ d, loading, area = "m2-a-ops" }: { d?: OverviewToday; loading: boolean; area?: string }) {
  const items = [
    { label: "Terlambat", value: d?.overdue.value ?? 0, sub: `${fmtNumber(bd(d?.overdue, "work_orders"))} work order · ${fmtNumber(bd(d?.overdue, "tasks"))} task`, to: d?.overdue.link || "/operations/work-orders?overdue=true", tone: "red" },
    { label: "Masalah SLA", value: d?.sla_risk.value ?? 0, sub: `${fmtNumber(bd(d?.sla_risk, "breached"))} lewat batas`, to: d?.sla_risk.link || "/operations/tasks?sla_risk=true", tone: "amber" },
    { label: "Permintaan tenant", value: d?.tenant_requests.value ?? 0, sub: `${fmtNumber(bd(d?.tenant_requests, "new_today"))} baru hari ini`, to: d?.tenant_requests.link || "/operations/service-requests?open=true", tone: "" },
    { label: "Incident aktif", value: d?.incidents.value ?? 0, sub: `${fmtNumber(bd(d?.incidents, "critical"))} kritis`, to: d?.incidents.link || "/operations/incidents?open=true", tone: bd(d?.incidents, "critical") > 0 ? "red" : "" },
  ];
  return (
    <section className={cn("m2-card flex flex-col", area)}>
      <CardHead icon="engineering" title="Operasional hari ini" right={<Link to="/operations/work-orders" className="m2-round" aria-label="Buka operasional"><Icon name="open_in_new" size={15} aria-hidden /></Link>} />
      <div className="mt-4 grid flex-1 grid-cols-2 gap-2">
        {items.map((it) => (
          <Link key={it.label} to={it.to} className="m2-sub m2-focus flex flex-col justify-between gap-1 p-3 transition-colors hover:brightness-95">
            <span className="m2-caption">{it.label}</span>
            <span className={cn("m2-num-sm", it.value > 0 && it.tone === "red" && "m2-red-text", it.value > 0 && it.tone === "amber" && "m2-amber-text")}>{loading ? "–" : fmtNumber(it.value)}</span>
            <span className="m2-caption truncate">{loading ? " " : it.sub}</span>
          </Link>
        ))}
      </div>
    </section>
  );
}

const CATEGORY: Record<string, { label: string; icon: string; cls: string }> = {
  news: { label: "Berita", icon: "article", cls: "m2-app-blue" },
  announcement: { label: "Pengumuman", icon: "campaign", cls: "m2-app-amber" },
  alert: { label: "Peringatan", icon: "warning", cls: "m2-app-red" },
};
const AUDIENCE: Record<string, string> = { staff: "Memo internal", tenant: "Untuk tenant", all: "Semua" };

/** Berita, pengumuman, dan memo internal (audience staff) yang sedang tayang. */
export function NewsCard({ items, loading, canCreate, area = "m2-a-news" }: { items: Announcement[]; loading: boolean; canCreate: boolean; area?: string }) {
  return (
    <section className={cn("m2-card flex flex-col", area)}>
      <CardHead icon="campaign" title="Berita & Memo" right={canCreate ? <Link to="/tenant-relation/announcements" className="m2-round" aria-label="Buat pengumuman"><Icon name="add" size={16} aria-hidden /></Link> : undefined} />
      <div className="mt-3 flex-1">
        {loading ? <div className="m2-skel h-40" /> : items.length === 0 ? (
          <p className="m2-caption py-8 text-center">Belum ada berita, pengumuman, atau memo yang tayang.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {items.slice(0, 5).map((a) => {
              const c = CATEGORY[a.category] ?? CATEGORY.announcement;
              return (
                <li key={a.id}>
                  <Link to="/tenant-relation/announcements" className="m2-list-item m2-focus items-start">
                    <span className={cn("m2-app-icon mt-0.5", c.cls)}><Icon name={c.icon} size={15} aria-hidden /></span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium">{a.title}</span>
                      <span className="m2-caption flex flex-wrap items-center gap-x-1.5">
                        <span>{a.audience === "staff" ? AUDIENCE.staff : c.label}</span>·<RelativeTime value={a.published_at ?? a.publish_at ?? a.created_at} />
                        {a.recipients_count ? <>·<span className="tabular-nums">{fmtNumber(a.read_count)}/{fmtNumber(a.recipients_count)} dibaca</span></> : null}
                      </span>
                    </span>
                    {a.importance === "important" && <span className="m2-status m2-status-amber m2-status-sm">Penting</span>}
                  </Link>
                </li>
              );
            })}
          </ul>
        )}
      </div>
      <Link to="/tenant-relation/announcements" className="m2-pill mt-3 self-start">Lihat semua</Link>
    </section>
  );
}
