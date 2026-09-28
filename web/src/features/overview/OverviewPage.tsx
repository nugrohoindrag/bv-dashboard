// Building Management Overview (29 Sep 2026; Roadmap v2.1 §20, §25.1–§25.3, Principle 8–11). Layout mengikuti referensi
// dashboard "Moneed" yang dipilih user (m2.css). Bukan sekadar tiket/work order: keuangan (tagihan, IPL/service charge,
// biaya vs budget, Sinking Fund), okupansi & sensus penghuni, hotel, kinerja layanan per domain, operasional, Berita & Memo,
// dan Perlu perhatian. Setiap kartu tampil sesuai permission + profile property (Apartment/Office/Hotel) dan menautkan ke
// sumbernya. Tanpa akses keuangan, baris pertama memakai kartu operasional. Pill di header = tab di halaman ini (?tab=):
// Overview (ringkasan) · Keuangan · Okupansi · Penghuni · Operasional · Laporan — masing-masing ringkasan areanya (tabs.tsx).
import { Link, useSearchParams } from "react-router-dom";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import { useOverview } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";
import { fmtNumber } from "@/lib/format";
import { fetchPropertyContext, useProfile, type ProfileCode } from "@/lib/profile";
import type { BuildingState, DomainDashboard, OccupancySummary, OverviewToday, TenantRequestsPanel } from "@/api/types";
import type { Occupancy as HotelOccupancy } from "@/features/hotel/hotel-api";
import type { Announcement, TRMetrics } from "@/features/tenant-relation/types";
import { ACCESS, checkAccess, type Access } from "@/app/navigation";
import { useAccessContext } from "@/app/access";
import { useOnboarding } from "@/lib/growth";
import { ChecklistView } from "@/features/growth/OnboardingPage";
import { kpi } from "./helpers";
import { AttentionTable, BuildingBars, CardHead, DoneToday, GoalList, Load, OpsHero, Overdue, PmPromo, ServiceGauge, SolidHead } from "./widgets";
import { CostCard, FinanceHero, HotelCard, NewsCard, OccupancyCard, OpsCard, RevenueCard, SinkingFundCard, type Residents } from "./bms";
import { FinanceTab, OccupancyTab, OperationsTab, ReportsTab, ResidentsTab } from "./tabs";
import "./m2.css";

const dateShort = new Intl.DateTimeFormat("id-ID", { weekday: "long", day: "numeric", month: "long", year: "numeric" });
type DomainKey = "engineering" | "security" | "housekeeping" | "finance";
type TabKey = "ringkasan" | "keuangan" | "okupansi" | "penghuni" | "operasional" | "laporan";

export default function OverviewPage() {
  const { propertyId, properties, can } = useAuth();
  const prof = useProfile();
  const ctx = useAccessContext();
  const allowed = (a: Access) => checkAccess(a, ctx) === "allowed";
  const pid = propertyId ?? undefined;
  const q = { property_id: pid };
  // tab disimpan di URL (?tab=) agar bisa dibagikan & tombol kembali browser berfungsi
  const [sp, setSp] = useSearchParams();

  // Onboarding checklist (Website PRD §28): tampil untuk organization trial sampai selesai/disembunyikan
  const onboarding = useOnboarding(can("platform.organizations.view"));
  const ob = onboarding.data;
  const showChecklist = !!ob && ob.trial?.is_trial_org && !ob.dismissed && ob.completed < ob.total;

  // profile tiap property dalam scope (cache sama dengan useProfile) → kartu Apartment/Hotel
  const scopeIds = propertyId ? [propertyId] : properties.map((p) => p.id);
  const ctxs = useQueries({ queries: scopeIds.map((id) => ({ queryKey: ["property-context", id], staleTime: 5 * 60_000, queryFn: ({ signal }: { signal: AbortSignal }) => fetchPropertyContext(id, signal) })) });
  const profileOf = new Map<string, ProfileCode>();
  ctxs.forEach((c) => c.data && profileOf.set(c.data.property_id, c.data.profile));
  const profiles = new Set(profileOf.values());
  const hotelIds = scopeIds.filter((id) => profileOf.get(id) === "hotel");
  const apartment = prof.profile ? prof.profile === "apartment" : profiles.has("apartment");

  // --- data ---
  const today = useOverview<OverviewToday>("today", q);
  const tenant = useOverview<TenantRequestsPanel>("tenant-requests", q);
  const building = useOverview<{ data: BuildingState[] }>("building-state", q);
  const dash = (domain: DomainKey, enabled: boolean) => ({
    queryKey: ["dashboard", domain, { property_id: pid, location_id: undefined, from: undefined, to: undefined }],
    queryFn: ({ signal }: { signal: AbortSignal }) => api<DomainDashboard>(`dashboards/${domain}`, { query: { property_id: pid }, signal }),
    enabled,
    staleTime: 30_000,
    refetchInterval: 60_000,
  });
  const finAllowed = allowed(ACCESS.financeDashboard);
  const eng = useQuery(dash("engineering", allowed(ACCESS.engineeringDashboard)));
  const sec = useQuery(dash("security", allowed(ACCESS.securityDashboard)));
  const hk = useQuery(dash("housekeeping", allowed(ACCESS.housekeepingDashboard)));
  const fin = useQuery(dash("finance", finAllowed));
  const trAllowed = allowed(ACCESS.tenantRelation);
  const tr = useQuery({ queryKey: ["tr-metrics", propertyId], enabled: trAllowed, queryFn: ({ signal }) => api<TRMetrics>("tenant-relation/metrics", { query: q, signal }), refetchInterval: 60_000 });
  const occAllowed = allowed(ACCESS.occupancy);
  const occ = useQuery({ queryKey: ["occupancy-summary", q], enabled: occAllowed, queryFn: ({ signal }) => api<OccupancySummary>("occupancy/summary", { query: q, signal }), staleTime: 30_000 });
  const censusAllowed = can("property.occupants.view");
  const residents = useQuery({ queryKey: ["overview", "residents", pid ?? null], enabled: censusAllowed, retry: false, queryFn: ({ signal }) => api<Residents>("overview/residents", { query: q, signal }), staleTime: 30_000 });
  const hotelAllowed = can("hotel.rooms.view");
  const hotels = useQueries({ queries: (hotelAllowed ? hotelIds : []).map((id) => ({ queryKey: ["hotel-occupancy", id], queryFn: () => api<HotelOccupancy>("hotel/occupancy", { query: { property_id: id } }), refetchInterval: 60_000 })) });
  const newsAllowed = allowed(ACCESS.announcements);
  const catalog = useQuery({ queryKey: ["reports", "catalog"], enabled: sp.get("tab") === "laporan" && allowed(ACCESS.reports), queryFn: ({ signal }) => api<{ data: { name: string; title: string; description: string }[] }>("reports", { signal }), staleTime: 5 * 60_000 });
  const news = useQuery({ queryKey: ["overview", "announcements", pid ?? null], enabled: newsAllowed, queryFn: ({ signal }) => api<{ data: Announcement[] }>("announcements", { query: { property_id: pid, status: "published", limit: 5 }, signal }), staleTime: 60_000 });

  const propName = properties.find((p) => p.id === propertyId)?.name ?? "Semua properti";
  const d = today.data;
  const L = today.isLoading;
  const finData = fin.data;
  const sinking = kpi(finData, "sinking_fund");
  const hotelPairs = (hotelAllowed ? hotelIds : []).map((id, i) => ({ id, data: hotels[i]?.data })).filter((x): x is { id: string; data: HotelOccupancy } => !!x.data);
  const hotelRows = hotelPairs.map((h) => h.data);
  const hotelNames = hotelPairs.map((h) => properties.find((p) => p.id === h.id)?.name ?? "Hotel");

  const occupantTerm = prof.term("occupant");
  const customerTerm = prof.term("customer");
  const occTitle = prof.profile === "hotel" ? "Okupansi kamar" : prof.profile === "office" ? `Okupansi & ${customerTerm}` : apartment ? "Okupansi & Sensus Penghuni" : "Okupansi & Penghuni";

  const tabs: { key: TabKey; label: string; icon: string; show: boolean }[] = [
    { key: "ringkasan", label: "Overview", icon: "space_dashboard", show: true },
    { key: "keuangan", label: "Keuangan", icon: "account_balance_wallet", show: finAllowed },
    { key: "okupansi", label: "Okupansi", icon: "apartment", show: occAllowed },
    { key: "penghuni", label: occupantTerm, icon: "groups", show: censusAllowed || trAllowed },
    { key: "operasional", label: "Operasional", icon: "engineering", show: true },
    { key: "laporan", label: "Laporan", icon: "summarize", show: allowed(ACCESS.reports) },
  ];
  const visibleTabs = tabs.filter((t) => t.show);
  const tab: TabKey = visibleTabs.find((t) => t.key === sp.get("tab"))?.key ?? "ringkasan";
  const selectTab = (k: TabKey) => {
    const n = new URLSearchParams(sp);
    if (k === "ringkasan") n.delete("tab");
    else n.set("tab", k);
    setSp(n);
  };
  const propertyNameOf = propertyId ? undefined : (id?: string) => properties.find((p) => p.id === id)?.name;

  return (
    <div className="space-y-4 pt-2">
      {showChecklist && ob && <ChecklistView data={ob} compact onChanged={() => onboarding.refetch()} />}
      <div className="m2">
        {/* ---------- Header ---------- */}
        <header className="mb-4 flex flex-wrap items-center justify-between gap-3 px-1">
          <div className="flex min-w-0 items-center gap-3">
            <span className="m2-icon-solid m2-bg-blue"><Icon name="apartment" size={20} aria-hidden /></span>
            <div className="min-w-0">
              <h1 className="m2-h1 truncate">Building Management Overview</h1>
              <div className="m2-caption truncate">{propName} · {dateShort.format(new Date())}</div>
            </div>
          </div>
          <div className="m2-nav" role="tablist" aria-label="Building Management Overview">
            {visibleTabs.map((t) => (
              <button key={t.key} type="button" role="tab" id={`bmo-tab-${t.key}`} aria-selected={tab === t.key} aria-controls="bmo-panel" onClick={() => selectTab(t.key)}>
                <Icon name={t.icon} size={16} aria-hidden />{t.label}
              </button>
            ))}
          </div>
        </header>

        <div id="bmo-panel" role="tabpanel" aria-labelledby={`bmo-tab-${tab}`}>
        {tab === "keuangan" && <FinanceTab fin={fin} apartment={apartment} flags={{ createInvoice: can("billing.invoices.create"), payments: allowed(ACCESS.payments), receivables: allowed(ACCESS.receivables), sinkingFund: allowed(ACCESS.sinkingFund) }} />}
        {tab === "okupansi" && <OccupancyTab occ={occ} residents={residents.data} hotels={hotelRows} hotelNames={hotelNames} title={occTitle} occupantTerm={occupantTerm} customerTerm={customerTerm} showCensus={censusAllowed && prof.profile !== "hotel"} />}
        {tab === "penghuni" && <ResidentsTab residents={censusAllowed ? residents : undefined} tr={trAllowed ? tr : undefined} news={newsAllowed ? news : undefined} occupantTerm={occupantTerm} customerTerm={customerTerm} flags={{ tenantUsers: allowed(ACCESS.tenantUsers), tenants: allowed(ACCESS.tenants), createAnnouncement: can("tenant_relation.announcements.create") }} />}
        {tab === "operasional" && <OperationsTab d={d} loading={L} building={building} propertyId={propertyId} propName={propName} propertyName={propertyNameOf} can={can} svc={{ tr: trAllowed ? tr : undefined, eng: eng.data, sec: sec.data, hk: hk.data, fin: finData, loading: eng.isLoading && sec.isLoading && hk.isLoading }} />}
        {tab === "laporan" && <ReportsTab catalog={catalog} />}
        {tab === "ringkasan" && (
        <div className="m2-grid">
          {/* ---------- Baris 1: keuangan (atau operasional bila tanpa akses keuangan) ---------- */}
          {finAllowed ? (
            finData ? (
              <>
                <FinanceHero fin={finData} canCreateInvoice={can("billing.invoices.create")} canPayments={allowed(ACCESS.payments)} canReceivables={allowed(ACCESS.receivables)} />
                <RevenueCard fin={finData} apartment={apartment} />
                <CostCard fin={finData} />
              </>
            ) : (
              <>
                <div className="m2-card m2-a-fin"><div className="m2-skel h-56" /></div>
                <div className="m2-card m2-a-cash"><div className="m2-skel h-56" /></div>
                <div className="m2-card m2-a-cost"><div className="m2-skel h-56" /></div>
              </>
            )
          ) : (
            <>
              <OpsHero d={d} loading={L} buildings={building.data?.data ?? []} propName={propName} can={can} />
              <section className="m2-card m2-a-cash flex flex-col"><SolidHead icon="arrow_downward" tone="green" title="Selesai hari ini" /><DoneToday d={d} loading={L} /></section>
              <section className="m2-card m2-a-cost flex flex-col"><Overdue d={d} loading={L} /></section>
            </>
          )}

          {/* ---------- Kinerja layanan ---------- */}
          <section className="m2-card m2-a-svc flex flex-col">
            <CardHead icon="speed" title="Kinerja layanan" right={allowed(ACCESS.reports) ? <Link to="/reports" className="m2-round" aria-label="Buka laporan"><Icon name="tune" size={16} aria-hidden /></Link> : undefined} />
            <div className="m2-sub mt-4 flex flex-1 flex-col gap-2 p-2">
              <ServiceGauge tr={trAllowed ? tr : undefined} eng={eng.data} />
              <GoalList eng={eng.data} sec={sec.data} hk={hk.data} fin={finData} loading={eng.isLoading && sec.isLoading && hk.isLoading} />
            </div>
          </section>

          {/* ---------- Okupansi & sensus penghuni (atau beban per gedung) ---------- */}
          {occAllowed ? (
            <OccupancyCard summary={occ.data} residents={residents.data} title={occTitle} occupantTerm={occupantTerm} customerTerm={customerTerm} showCensus={censusAllowed && prof.profile !== "hotel"} />
          ) : (
            <section className="m2-card m2-a-occ flex flex-col">
              <CardHead icon="bar_chart" title="Beban per gedung" right={<span className="m2-pill">Hari ini</span>} />
              <Load query={building} className="mt-4 flex-1">{(res) => <BuildingBars rows={res.data} propertyName={propertyNameOf} />}</Load>
            </section>
          )}

          {/* ---------- Operasional hari ini ---------- */}
          <OpsCard d={d} loading={L} />

          {/* ---------- Kolom kartu profile: Sinking Fund (Apartment) · Hotel · permintaan tenant · laporan ---------- */}
          <div className="m2-a-side m2-side grid min-w-0 grid-cols-1 sm:grid-cols-2">
            {apartment && allowed(ACCESS.sinkingFund) && sinking ? <SinkingFundCard value={sinking.value} to={sinking.drill_down || "/billing/sinking-fund"} /> : <PmPromo d={d} />}
            {hotelRows.length > 0 ? <HotelCard rows={hotelRows} /> : (
              <Link to="/operations/service-requests?open=true" className="m2-card m2-focus flex flex-col justify-between gap-3">
                <div className="flex items-center justify-between">
                  <span className="m2-pill m2-pill-soft">Permintaan {customerTerm.toLowerCase()}</span>
                  <span className="m2-round m2-round-sm" aria-hidden><Icon name="open_in_new" size={14} /></span>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="m2-num-sm">{tenant.data ? fmtNumber(tenant.data.open) : "–"}</span>
                  {(tenant.data?.sla_risk ?? 0) > 0 && <span className="m2-chip m2-chip-red"><span className="m2-chip-dot m2-bg-red"><Icon name="timer" size={11} aria-hidden /></span>{fmtNumber(tenant.data!.sla_risk)} SLA</span>}
                </div>
                <div className="m2-caption">{tenant.data ? `${fmtNumber(tenant.data.new_today)} baru hari ini` : " "}</div>
              </Link>
            )}
            <Link to={allowed(ACCESS.reports) ? "/reports" : "/overview"} className="m2-card m2-focus flex flex-col justify-between gap-4">
              <div className="flex items-center justify-between">
                <span className="m2-round m2-round-sm" aria-hidden><Icon name="description" size={14} /></span>
                <span className="m2-round m2-round-sm" aria-hidden><Icon name="open_in_new" size={14} /></span>
              </div>
              <div>
                <span className="m2-pill m2-pill-soft">Lihat &amp; cetak laporan</span>
                <div className="m2-title mt-2">Laporan Manajemen</div>
              </div>
            </Link>
          </div>

          {/* ---------- Berita & Memo ---------- */}
          {newsAllowed ? (
            <NewsCard items={news.data?.data ?? []} loading={news.isLoading} canCreate={can("tenant_relation.announcements.create")} />
          ) : (
            <div className="m2-a-news grid"><PmPromo d={d} /></div>
          )}

          {/* ---------- Perlu perhatian ---------- */}
          <section className="m2-card m2-a-tx">
            <AttentionTable propertyId={propertyId} />
          </section>
        </div>
        )}
        </div>
      </div>
    </div>
  );
}
