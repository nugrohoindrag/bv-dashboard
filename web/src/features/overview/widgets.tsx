// Widget Building Management Overview (29 Sep 2026): kartu bergaya referensi "Moneed" (m2.css). Dipakai OverviewPage.
import { useState, type ReactNode } from "react";
import { Link, useNavigate } from "react-router-dom";
import type { UseQueryResult } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import { RelativeTime } from "@/components/bv/common";
import { attentionCategoryLabel } from "@/components/bv/cards";
import { useOverview } from "@/api/hooks";
import { fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { AttentionItem, BuildingState, Counter, DomainDashboard, OverviewToday } from "@/api/types";
import type { TRMetrics } from "@/features/tenant-relation/types";
import { kpi, shortLocation } from "./helpers";

const bd = (c: Counter | undefined, key: string) => c?.breakdown?.[key] ?? 0;
const link = (c: Counter | undefined, fallback: string) => c?.link || fallback;

export function CardHead({ icon, title, right }: { icon: string; title: string; right?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <div className="flex min-w-0 items-center gap-3">
        <span className="m2-icon-tile"><Icon name={icon} size={20} aria-hidden /></span>
        <h2 className="m2-title truncate">{title}</h2>
      </div>
      {right && <div className="flex flex-wrap items-center gap-1.5">{right}</div>}
    </div>
  );
}

export function SolidHead({ icon, tone, title, pill = "Hari ini" }: { icon: string; tone: "green" | "red"; title: string; pill?: string }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <div className="flex min-w-0 items-center gap-3">
        <span className={cn("m2-icon-solid", tone === "green" ? "m2-bg-green" : "m2-bg-red")}><Icon name={icon} size={20} aria-hidden /></span>
        <h2 className="m2-title truncate">{title}</h2>
      </div>
      <span className="m2-pill m2-pill-opt">{pill}</span>
    </div>
  );
}

export function Load<T>({ query, children, className }: { query: UseQueryResult<T>; children: (d: T) => ReactNode; className?: string }) {
  if (query.isLoading) return <div className={cn("m2-skel min-h-24", className)} />;
  if (query.error && !query.data) {
    return (
      <div className={cn("m2-caption flex items-center justify-between gap-2 py-6", className)}>
        Data belum bisa dimuat.
        <button type="button" className="m2-pill" onClick={() => void query.refetch()}>Coba lagi</button>
      </div>
    );
  }
  return query.data === undefined ? null : <div className={className}>{children(query.data)}</div>;
}

/** Heatmap piksel ala referensi: satu kolom per gedung/tower, tinggi = pekerjaan terbuka, piksel gelap = porsi terlambat. */


/** Gauge setengah lingkaran (kartu gelap "Swiss Holiday"): SLA layanan tenant; fallback Preventive Maintenance compliance. */
export function ServiceGauge({ tr, eng }: { tr?: UseQueryResult<TRMetrics>; eng?: DomainDashboard }) {
  const [open, setOpen] = useState(true);
  const sla = tr?.data?.sla_compliance_pct;
  const pmc = kpi(eng, "pm_compliance")?.value;
  const useTenant = sla !== null && sla !== undefined;
  const value = useTenant ? sla : pmc ?? null;
  const title = useTenant ? "Layanan Tenant" : "Preventive Maintenance";
  const target = 95;
  const pct = value === null ? 0 : Math.max(0, Math.min(100, value));
  const R = 80;
  const len = Math.PI * R;
  const angle = Math.PI * (1 - pct / 100);
  const kx = 100 + R * Math.cos(angle);
  const ky = 100 - R * Math.sin(angle);
  return (
    <div className="m2-dark-card p-4">
      <div className="flex items-center justify-between gap-2">
        <span className="m2-title">{title}</span>
        <button type="button" className="m2-round m2-round-sm" style={{ background: "var(--m2-on-dark)", color: "var(--m2-dark)", borderColor: "transparent" }} aria-expanded={open} aria-label={open ? "Ciutkan" : "Buka"} onClick={() => setOpen((o) => !o)}>
          <Icon name={open ? "expand_less" : "expand_more"} size={16} aria-hidden />
        </button>
      </div>
      {open && (
        <div className="m2-gauge mx-auto mt-2 w-full">
          <svg viewBox="0 0 200 112" className="w-full" role="img" aria-label={`${title}: ${value === null ? "belum ada data" : `${Math.round(value)}%`} dari target ${target}%`}>
            <path d="M 20 100 A 80 80 0 0 1 180 100" fill="none" stroke="var(--m2-dark-2)" strokeWidth="16" strokeLinecap="round" opacity="0.35" />
            <path d="M 20 100 A 80 80 0 0 1 180 100" fill="none" stroke="var(--m2-blue)" strokeWidth="16" strokeLinecap="round" strokeDasharray={`${(len * pct) / 100} ${len}`} />
            {value !== null && <circle cx={kx} cy={ky} r="7" fill="#fff" stroke="var(--m2-blue)" strokeWidth="3" />}
            <text x="100" y="62" textAnchor="middle" className="m2-gauge-label">{useTenant ? "SLA tercapai" : "Tepat waktu"}</text>
            <text x="100" y="96" textAnchor="middle" className="m2-gauge-value">{value === null ? "–" : `${Math.round(value)}%`}</text>
          </svg>
          <div className="m2-small -mt-1 flex items-center justify-center gap-1 opacity-70">
            Target {target}% {value !== null && value >= target && <Icon name="check_circle" size={13} className="m2-green-text" aria-hidden />}
          </div>
        </div>
      )}
      {open && useTenant && tr?.data && (
        <div className="m2-small mt-3 flex justify-between opacity-80 tabular-nums">
          <span>CSAT {tr.data.csat === null ? "–" : tr.data.csat.toFixed(1)}</span>
          <span>Reopen {tr.data.reopen_rate_pct === null ? "–" : `${Math.round(tr.data.reopen_rate_pct)}%`}</span>
          <Link to="/tenant-relation" className="underline-offset-2 hover:underline">Detail</Link>
        </div>
      )}
    </div>
  );
}

export function GoalList({ eng, sec, hk, fin, loading }: { eng?: DomainDashboard; sec?: DomainDashboard; hk?: DomainDashboard; fin?: DomainDashboard; loading: boolean }) {
  const items = [
    { key: "pm", label: "Preventive Maintenance", k: kpi(eng, "pm_compliance"), to: "/engineering" },
    { key: "patrol", label: "Patroli selesai", k: kpi(sec, "patrol_completion"), to: "/security" },
    { key: "checkpoint", label: "Kepatuhan checkpoint", k: kpi(sec, "checkpoint_compliance"), to: "/security" },
    { key: "clean", label: "Kebersihan selesai", k: kpi(hk, "completion_rate"), to: "/housekeeping" },
    { key: "clean_sla", label: "SLA kebersihan", k: kpi(hk, "cleaning_sla_compliance"), to: "/housekeeping" },
    { key: "collect", label: "Penagihan", k: kpi(fin, "collection_rate"), to: "/finance" },
  ].filter((i) => i.k);
  if (loading) return <div className="m2-skel h-28" />;
  if (!items.length) return null;
  return (
    <ul className="flex flex-col gap-2">
      {items.map((i) => {
        const v = Math.max(0, Math.min(100, Math.round(i.k!.value)));
        const fill = v >= 90 ? "m2-fill-green" : v >= 70 ? "m2-fill-amber" : "m2-fill-red";
        return (
          <li key={i.key}>
            <Link to={i.k!.drill_down || i.to} className="m2-list-item m2-focus" title={i.k!.hint}>
              <span className="min-w-0 flex-1">
                <span className="flex items-baseline justify-between gap-2">
                  <span className="m2-goal-label">{i.label}</span>
                  <span className="m2-small m2-text-2 tabular-nums">{v}%</span>
                </span>
                <span className="m2-track mt-2 block"><span className={fill} style={{ width: `${Math.max(v, 3)}%` }} /></span>
              </span>
              <span className="m2-round m2-round-sm" aria-hidden><Icon name="chevron_right" size={14} /></span>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}


const ROW_ICON: Record<string, string> = {
  sla_breach: "timer_off", sla_risk: "timer", critical_incident: "emergency_home", critical_asset_issue: "precision_manufacturing", maintenance_overdue: "event_repeat",
  patrol_overdue: "directions_walk", overdue_task: "task_alt", overdue_work_order: "construction", reopened_request: "replay", unresolved_finding: "report_problem",
  active_emergency: "e911_emergency", checkpoint_missed: "location_off", document_expiring: "description", workforce_shortage: "group_off", recurring_issue: "repeat",
};
const DOMAIN_FILTER = [
  { value: "", label: "Semua domain" },
  { value: "engineering", label: "Engineering" },
  { value: "security", label: "Security" },
  { value: "housekeeping", label: "Housekeeping" },
];

export function AttentionTable({ propertyId }: { propertyId: string | null }) {
  const [search, setSearch] = useState("");
  const [domain, setDomain] = useState("");
  const [menu, setMenu] = useState(false);
  const [limit, setLimit] = useState(6);
  // filter domain di server (sama dengan Overview); pencarian di klien atas hasilnya
  const query = useOverview<{ data: AttentionItem[]; total: number }>("attention-required", { property_id: propertyId ?? undefined, domain: domain || undefined, limit: 50 });
  const allProperties = !propertyId;
  const rows = (query.data?.data ?? []).filter((it) => {
    if (!search.trim()) return true;
    const s = search.trim().toLowerCase();
    return `${it.title} ${it.label} ${it.location_path ?? ""}`.toLowerCase().includes(s);
  });
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <span className="m2-icon-tile"><Icon name="history" size={20} aria-hidden /></span>
          <h2 className="m2-title">Perlu perhatian {query.data && <span className="m2-muted font-normal">({fmtNumber(query.data.total)})</span>}</h2>
        </div>
        <div className="relative flex items-center gap-2">
          <label className="m2-search w-full sm:w-64">
            <Icon name="search" size={16} aria-hidden />
            <input value={search} onChange={(e) => { setSearch(e.target.value); setLimit(6); }} placeholder="Cari" aria-label="Cari item perlu perhatian" />
          </label>
          <button type="button" className="m2-round" aria-label="Filter domain" aria-haspopup="menu" aria-expanded={menu} onClick={() => setMenu((m) => !m)}>
            <Icon name="tune" size={16} aria-hidden />{domain && <span className="m2-badge-dot" />}
          </button>
          {menu && (
            <div className="m2-card m2-menu absolute right-0 top-10 z-20 w-48" role="menu">
              {DOMAIN_FILTER.map((o) => (
                <button key={o.value} type="button" role="menuitemradio" aria-checked={domain === o.value} className={cn("m2-menu-item", domain === o.value && "font-semibold")} onClick={() => { setDomain(o.value); setMenu(false); setLimit(6); }}>
                  {o.label}{domain === o.value && <Icon name="check" size={16} aria-hidden />}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
      <div className="mt-3 overflow-x-auto">
        {query.isLoading ? <div className="m2-skel h-48" /> : query.error && !query.data ? <p className="m2-caption py-8 text-center">Data belum bisa dimuat.</p> : rows.length === 0 ? (
          <p className="m2-caption py-10 text-center">{search || domain ? "Tidak ada item yang cocok." : "Tidak ada yang perlu ditindaklanjuti. Semua aman."}</p>
        ) : (
          <table className="m2-table">
            <colgroup><col className="m2-col-name" /><col className="m2-col-cat" /><col className="m2-col-loc" /><col className="m2-col-since" /><col className="m2-col-status" /><col className="m2-col-open" /></colgroup>
            <thead>
              <tr><th className="m2-th">Nama</th><th className="m2-th">Kategori</th><th className="m2-th">Lokasi</th><th className="m2-th">Sejak</th><th className="m2-th text-center">Status</th><th className="m2-th"><span className="sr-only">Buka</span></th></tr>
            </thead>
            <tbody>
              {rows.slice(0, limit).map((it) => {
                const critical = it.severity === "critical";
                return (
                  <tr key={it.object_type + it.object_id + it.category}>
                    <td>
                      <Link to={it.deep_link} className="m2-focus flex min-w-0 items-center gap-2.5 rounded-md">
                        <span className={cn("m2-app-icon", critical ? "m2-app-red" : "m2-app-amber")}><Icon name={ROW_ICON[it.category] ?? "warning"} size={15} aria-hidden /></span>
                        <span className="min-w-0">
                          <span className="block truncate font-medium">{it.title || it.label}</span>
                          <span className="m2-caption block truncate">{it.label}</span>
                        </span>
                      </Link>
                    </td>
                    <td className="m2-text-2 truncate">{attentionCategoryLabel[it.category] ?? it.category.replace(/_/g, " ")}</td>
                    <td className="m2-text-2 truncate">{shortLocation(it.location_path, allProperties) || "—"}</td>
                    <td className="m2-text-2 whitespace-nowrap"><RelativeTime value={it.since} /></td>
                    <td className="text-center"><span className={cn("m2-status", critical ? "m2-status-red" : "m2-status-amber")}>{critical ? "Kritis" : "Perhatian"}</span></td>
                    <td className="w-10 text-right"><Link to={it.deep_link} className="m2-muted inline-flex rounded-full hover:opacity-80" aria-label={`Buka ${it.label}`}><Icon name="info" size={18} aria-hidden /></Link></td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
      {rows.length > limit && <button type="button" className="m2-pill mt-3" onClick={() => setLimit(50)}>Tampilkan semua ({fmtNumber(rows.length)})</button>}
    </>
  );
}

/** Heatmap piksel ala referensi: satu kolom per gedung/tower, tinggi = pekerjaan terbuka, piksel gelap = porsi terlambat. */
export function PixelGrid({ rows }: { rows: BuildingState[] }) {
  const cols = rows.slice(0, 9);
  if (!cols.length) return null;
  const max = Math.max(1, ...cols.map((b) => b.open));
  return (
    <div className="m2-pixels" aria-label="Pekerjaan terbuka per gedung">
      {cols.flatMap((b) => {
        const h = b.open > 0 ? Math.max(1, Math.round((b.open / max) * 6)) : 0;
        const late = b.open > 0 ? Math.round((b.overdue / b.open) * h) : 0;
        const sla = b.open > 0 ? Math.min(h - late, Math.round((b.sla_risk / b.open) * h)) : 0;
        // baris 0 = atas; isi dari bawah: terlambat (biru tua) → masalah SLA (biru) → sisanya (biru muda)
        return Array.from({ length: 6 }).map((_, r) => {
          const fromBottom = 5 - r;
          // gedung tanpa pekerjaan terbuka: satu sel lime di dasar (aman)
          const cls = b.open === 0 ? (fromBottom === 0 ? "m2-pixel-l" : "") : fromBottom >= h ? "" : fromBottom < late ? "m2-pixel-3" : fromBottom < late + sla ? "m2-pixel-2" : "m2-pixel-1";
          return <span key={`${b.location_id}-${r}`} className={cn("m2-pixel", cls)} title={r === 5 ? `${b.name}: ${b.open} terbuka · ${b.overdue} terlambat` : undefined} />;
        });
      })}
    </div>
  );
}

export function DoneToday({ d, loading }: { d?: OverviewToday; loading: boolean }) {
  const done = d?.completed_today?.value ?? 0;
  const due = d?.due_today?.value ?? 0;
  const pct = done + due > 0 ? Math.round((done / (done + due)) * 100) : 0;
  return (
    <>
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <span className="m2-num">{loading ? "–" : fmtNumber(done)}<span className="m2-unit"> pekerjaan</span></span>
        {!loading && <span className="m2-chip m2-chip-green"><span className="m2-chip-dot m2-bg-green"><Icon name="trending_up" size={11} aria-hidden /></span>{pct}%</span>}
      </div>
      <p className="m2-small m2-sub mt-3 inline-block self-start px-3 py-1.5 m2-text-2">
        {due > 0 ? <>Masih <b className="m2-green-text">{fmtNumber(due)}</b> jatuh tempo hari ini</> : "Tidak ada lagi yang jatuh tempo hari ini"}
      </p>
      <div className="mt-auto flex gap-6 pt-4">
        <Split color="var(--m2-blue)" label="Task" value={bd(d?.completed_today, "tasks")} to={link(d?.completed_today, "/operations/tasks?completed_today=true")} />
        <Split color="var(--m2-lime)" label="Work Order" value={bd(d?.completed_today, "work_orders")} to="/operations/work-orders?completed_today=true" />
      </div>
    </>
  );
}

function Split({ color, label, value, to }: { color: string; label: string; value: number; to: string }) {
  return (
    <Link to={to} className="m2-focus flex gap-2.5 rounded-md">
      <span className="m2-split-bar" style={{ background: color }} />
      <span>
        <span className="m2-caption block">{label}</span>
        <span className="block text-lg font-semibold tabular-nums">{fmtNumber(value)}</span>
      </span>
    </Link>
  );
}

export function Overdue({ d, loading }: { d?: OverviewToday; loading: boolean }) {
  const total = d?.overdue.value ?? 0;
  const wo = bd(d?.overdue, "work_orders");
  const task = bd(d?.overdue, "tasks");
  const pm = bd(d?.pm_due, "overdue");
  const crit = bd(d?.incidents, "critical");
  const segs = [
    { key: "wo", label: "Work Order", value: wo, cls: "m2-seg-dark", dot: "var(--m2-dark)" },
    { key: "task", label: "Task", value: task, cls: "m2-seg-blue", dot: "var(--m2-blue)" },
    { key: "pm", label: "Preventive Maintenance", value: pm, cls: "m2-seg-lime", dot: "var(--m2-lime)" },
  ];
  const sum = Math.max(1, wo + task + pm);
  const top = segs.reduce((a, b) => (b.value > a.value ? b : a), segs[0]);
  return (
    <>
      <div className="m2-sub -mx-1 -mt-1 p-3 pb-4">
        <SolidHead icon="arrow_upward" tone="red" title="Terlambat" />
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <span className="m2-num">{loading ? "–" : fmtNumber(total)}<span className="m2-unit"> pekerjaan</span></span>
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <span className="m2-chip m2-chip-red"><span className="m2-chip-dot m2-bg-red"><Icon name="emergency_home" size={11} aria-hidden /></span>{fmtNumber(crit)} incident kritis</span>
          <Link to={link(d?.overdue, "/operations/work-orders?overdue=true")} className="m2-caption hover:underline">lihat daftar</Link>
        </div>
      </div>
      <div className="mt-auto pt-5">
        <div className="relative mb-2 flex justify-between m2-caption tabular-nums">
          <span>{Math.round((segs[0].value / sum) * 100)}%</span>
          {top.value > 0 && <span className="m2-bubble absolute left-1/2 -translate-x-1/2 -top-1">{fmtNumber(top.value)} {top.label === "Preventive Maintenance" ? "Preventive" : top.label}</span>}
          <span>{Math.round((segs[2].value / sum) * 100)}%</span>
        </div>
        <div className="m2-seg" aria-label="Komposisi pekerjaan terlambat">
          {segs.map((s) => <span key={s.key} className={s.cls} style={{ flexGrow: Math.max(s.value, 0.001), flexBasis: 0 }} title={`${s.label}: ${s.value}`} />)}
        </div>
        <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 m2-small m2-text-2">
          {segs.map((s) => <span key={s.key} className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: s.dot }} />{s.label}</span>)}
        </div>
      </div>
    </>
  );
}

/** Cadangan kartu utama bila user tidak punya akses keuangan: pekerjaan terbuka + heatmap per gedung + aksi cepat. */
export function OpsHero({ d, loading: L, buildings, propName, can, area = "m2-a-fin" }: { d?: OverviewToday; loading: boolean; buildings: BuildingState[]; propName: string; can: (p: string) => boolean; area?: string }) {
  const nav = useNavigate();
  return (
        <section className={cn("m2-card flex flex-col", area)}>
          <CardHead icon="assignment" title="Pekerjaan terbuka" right={<><span className="m2-pill"><Icon name="apartment" size={14} aria-hidden />{propName.length > 18 ? `${propName.slice(0, 16)}…` : propName}</span><span className="m2-pill">Hari ini</span></>} />
          <div className="mt-4 flex flex-wrap items-end justify-between gap-4">
            <div className="min-w-0">
              <div className="flex items-center gap-3">
                <span className="m2-hero">{L ? <span className="m2-skel inline-block h-12 w-40 align-middle" /> : fmtNumber((d?.open_tasks?.value ?? 0) + (d?.open_work_orders.value ?? 0))}</span>
                <Link to={link(d?.open_tasks, "/operations/tasks?open=true")} className="m2-round m2-round-lg" aria-label="Lihat daftar pekerjaan terbuka"><Icon name="visibility" size={18} aria-hidden /></Link>
              </div>
              <div className="m2-small mt-2 flex flex-wrap items-center gap-2 m2-text-2">
                <span className="m2-chip-dot m2-bg-green"><Icon name="open_in_new" size={11} aria-hidden /></span>
                <span><b className="m2-green-text">{fmtNumber(d?.completed_today?.value ?? 0)} selesai</b> hari ini · {fmtNumber(d?.open_tasks?.value ?? 0)} task · {fmtNumber(d?.open_work_orders.value ?? 0)} work order</span>
              </div>
            </div>
            <PixelGrid rows={buildings} />
          </div>
          <div className="mt-5 grid grid-cols-1 gap-2 sm:grid-cols-3">
            {can("operations.work_orders.create") && <button type="button" className="m2-btn m2-btn-blue" onClick={() => nav("/operations/work-orders?new=1")}><Icon name="add" size={18} aria-hidden />Buat Work Order</button>}
            {can("operations.tasks.create") && <button type="button" className="m2-btn m2-btn-dark" onClick={() => nav("/operations/tasks?new=1")}><Icon name="playlist_add" size={18} aria-hidden />Buat Task</button>}
            {can("operations.incidents.create") && <button type="button" className="m2-btn m2-btn-light" onClick={() => nav("/operations/incidents?new=1")}><Icon name="emergency_home" size={16} aria-hidden />Lapor Incident</button>}
          </div>
        </section>
  );
}

/**
 * Grafik batang ala "Cashflow chart": seluruh building/tower ditampilkan (bergulir horizontal bila banyak), angka di atas
 * tiap batang, batang tersibuk berwarna (biru = terbuka, lime = terlambat), tooltip gelap saat disorot/difokus.
 * Pada "Semua properti" nama properti tampil di bawah nama gedung agar nama kembar bisa dibedakan.
 */
export function BuildingBars({ rows, propertyName }: { rows: BuildingState[]; propertyName?: (id?: string) => string | undefined }) {
  const [active, setActive] = useState<number | null>(null);
  if (!rows.length) return <p className="m2-caption py-10 text-center">Belum ada gedung/tower.</p>;
  const n = rows.length;
  const max = Math.max(1, ...rows.map((b) => b.open));
  const busiest = rows.reduce((a, b, i) => (b.open > rows[a].open ? i : a), 0);
  const colored = active ?? busiest;
  const cols = { gridTemplateColumns: `repeat(${n}, minmax(0, 1fr))` };
  const hPct = (v: number) => (v > 0 ? Math.max(6, (v / max) * 82) : 0); // sisakan ruang di atas untuk angka
  const tip = active !== null ? rows[active] : null;
  const rightSide = active !== null && active < n / 2;
  const edgePct = active === null ? 0 : ((rightSide ? active + 1 : active) / n) * 100;
  return (
    <div>
      <div className="m2-caption mb-2 flex flex-wrap items-center gap-x-3 gap-y-1">
        <span>Pekerjaan terbuka · {n} gedung/tower</span>
        <span className="flex-1 border-t border-dashed" style={{ borderColor: "var(--m2-line)" }} />
        <span className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: "var(--m2-blue)" }} />Terbuka</span>
        <span className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: "var(--m2-lime)" }} />Terlambat</span>
      </div>
      <div className="m2-chart-scroll">
        <div style={{ minWidth: n * 64 }}>
          <div className="m2-bars" style={cols} onMouseLeave={() => setActive(null)}>
            {rows.map((b, i) => {
              const on = i === colored;
              const h = hPct(b.open);
              const late = b.open ? Math.min(b.overdue, b.open) : 0;
              return (
                <button key={b.location_id} type="button" className="m2-bar m2-focus" onMouseEnter={() => setActive(i)} onFocus={() => setActive(i)} onBlur={() => setActive(null)} aria-label={`${b.name}: ${b.open} terbuka, ${b.overdue} terlambat, ${b.sla_risk} masalah SLA`}>
                  {b.open === 0 ? (
                    <span className="m2-bar-stub m2-hatch" style={{ background: "var(--m2-sub-2)" }} />
                  ) : on ? (
                    <span className="m2-bar-stack" style={{ height: `${h}%` }}>
                      <span style={{ flex: `${Math.max(b.open - late, 0.0001)} 1 0`, background: "var(--m2-blue)" }} />
                      {late > 0 && <span style={{ flex: `${late} 1 0`, background: "var(--m2-lime)" }} />}
                    </span>
                  ) : (
                    <span className="m2-bar-bg" style={{ height: `${h}%` }} />
                  )}
                  <span className="m2-bar-val" style={{ bottom: `calc(${b.open === 0 ? 8 : h}% + 4px)` }}>{fmtNumber(b.open)}</span>
                </button>
              );
            })}
            {tip && (
              <div className="m2-tooltip" style={rightSide ? { left: `calc(${edgePct}% + 6px)`, top: 4 } : { right: `calc(${100 - edgePct}% + 6px)`, top: 4 }}>
                <span className="m2-tooltip-chip">{tip.name}</span>
                <div className="m2-small mt-2 flex items-center justify-between gap-2"><span className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: "var(--m2-blue)" }} />Terbuka</span><b className="tabular-nums">{fmtNumber(tip.open)}</b></div>
                <div className="m2-small mt-1 flex items-center justify-between gap-2"><span className="inline-flex items-center gap-1.5"><span className="m2-dot" style={{ background: "var(--m2-lime)" }} />Terlambat</span><b className="tabular-nums">{fmtNumber(tip.overdue)}</b></div>
                <div className="m2-small mt-1 flex items-center justify-between gap-2 opacity-80"><span>Masalah SLA</span><b className="tabular-nums">{fmtNumber(tip.sla_risk)}</b></div>
              </div>
            )}
          </div>
          <div className="m2-bar-labels" style={cols}>
            {rows.map((b) => {
              const prop = propertyName?.(b.property_id);
              return (
                <span key={b.location_id} className="min-w-0" title={prop ? `${b.name} · ${prop}` : b.name}>
                  <span className="m2-bar-name">{b.name}</span>
                  {prop && <span className="m2-bar-prop">{prop}</span>}
                </span>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

export function PmPromo({ d }: { d?: OverviewToday }) {
  const due = d?.pm_due.value ?? 0;
  const late = bd(d?.pm_due, "overdue");
  return (
    <section className="m2-card m2-promo sm:col-span-2 flex flex-col justify-between">
      <svg className="m2-promo-lines" viewBox="0 0 400 160" preserveAspectRatio="none" aria-hidden>
        <path d="M150 0 L150 60 L230 60 L230 160 M260 0 L260 40 L340 40 L340 160 M300 0 L300 20 L400 20 M180 160 L180 110 L120 110" fill="none" stroke="currentColor" strokeWidth="14" strokeLinejoin="round" />
      </svg>
      <div className="relative flex items-center justify-between gap-2">
        <span className="m2-round m2-round-sm" aria-hidden><Icon name="event_repeat" size={14} /></span>
        <span className="flex items-center gap-1.5">
          <Link to="/engineering/preventive-maintenance" className="m2-pill m2-pill-dark">Lihat jadwal</Link>
          <Link to="/engineering/preventive-maintenance?due_within_days=7" className="m2-round m2-round-sm" aria-label="Buka Preventive Maintenance 7 hari"><Icon name="open_in_new" size={14} aria-hidden /></Link>
        </span>
      </div>
      <div className="relative">
        <span className="m2-pill m2-pill-blue">{fmtNumber(due)} jatuh tempo 7 hari{late > 0 ? ` · ${fmtNumber(late)} terlambat` : ""}</span>
        <div className="m2-promo-title mt-2">Preventive Maintenance minggu ini</div>
      </div>
    </section>
  );
}
