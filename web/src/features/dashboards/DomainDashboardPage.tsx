// Dashboard domain Engineering · Security · Housekeeping · Finance (PRD P2 v2.1 §5.1 P2-ENG-01..04, §6.1 P2-SDB-01..02, §7.1
// P2-HDB-01..05; PRD P4 v2.1 P4-FIN-01; Roadmap v2.1 §25.2 KPI minimum, §25.8 dashboard per domain, Principle 5/8 exception-driven,
// Principle 9 setiap KPI dapat ditelusuri). Satu komponen, parameter domain; data dari GET /dashboards/{domain} (KPI + severity +
// hint + drill-down dihitung server). Filter property · gedung/tower · periode disimpan di URL. Finance: Attention Required ada di
// respons dashboard (tidak memanggil overview/attention-required), uang KPI ringkas, pintasan layar kerja keuangan.
import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { FilterChip } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, DatePicker, NativeSelect, TBody, TD, TH, THead, TR, Table } from "@/components/ui/primitives";
import { StatusBadge, semanticStyle } from "@/components/bv/badges";
import { RelativeTime } from "@/components/bv/common";
import { CellText } from "@/components/bv/cells";
import { CardSkeleton, KpiSkeleton, QueryErrorState } from "@/components/bv/states";
import { LocationPicker } from "@/components/bv/pickers";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDate, fmtNumber } from "@/lib/format";
import { statusDef } from "@/lib/status-map";
import { statusLabel } from "@/lib/status";
import { cn } from "@/lib/utils";
import type { AttentionItem, DashboardBreakdownRow, DomainDashboard } from "@/api/types";
import { AttentionPanel } from "@/features/overview/AttentionPanel";
import { Icon } from "@buildingvision/ui";
import { KpiCard } from "./KpiCard";
import { FinanceAttentionList } from "./FinanceAttention";
import { BREAKDOWNS, DOMAIN_SUBTITLE, DOMAIN_TITLE, FINANCE_SHORTCUTS, areaCondition, attentionLink, breakdownLabel, formatCell, healthSegments, lastDays, runStatusFromCode, type BreakdownSpec, type Domain } from "./dashboard";

const PRESETS = [7, 30, 90];

export default function DomainDashboardPage({ domain }: { domain: Domain }) {
  const { propertyId, properties, can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const pid = sp.has("property_id") ? sp.get("property_id") || null : propertyId ?? null;
  const locationId = sp.get("location_id") || null;
  const from = sp.get("from") ?? "";
  const to = sp.get("to") ?? "";
  const set = (patch: Record<string, string | null>) => {
    const n = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (v === null) n.delete(k);
      else n.set(k, v);
    }
    setSp(n, { replace: true });
  };
  const params = { property_id: pid ?? undefined, location_id: locationId ?? undefined, from: from || undefined, to: to || undefined };
  const q = useQuery({
    queryKey: ["dashboard", domain, params],
    queryFn: ({ signal }) => api<DomainDashboard>(`dashboards/${domain}`, { query: params, signal }),
    refetchInterval: 60_000,
    staleTime: 30_000,
  });
  const [expanded, setExpanded] = useState(false);
  const finance = domain === "finance";
  // finance: seluruh item attention sudah ada di respons dashboard (maks. 5 per kategori)
  const fullAttention = useQuery({
    queryKey: ["overview", "attention-required", "dashboard", domain, pid],
    enabled: expanded && !finance,
    queryFn: ({ signal }) => api<{ data: AttentionItem[]; total: number }>("overview/attention-required", { query: { property_id: pid ?? undefined, domain, limit: 50 }, signal }),
  });
  const d = q.data;
  const activePreset = PRESETS.find((n) => { const r = lastDays(n); return (from || d?.from) === r.from && (to || d?.to) === r.to; });
  const filtered = sp.has("property_id") || !!locationId || !!from || !!to;

  return (
    <div className="space-y-5">
      <PageHeader
        title={DOMAIN_TITLE[domain]}
        subtitle={d ? <>Periode {fmtDate(d.from)} – {fmtDate(d.to)} · diperbarui <RelativeTime value={d.generated_at} /></> : DOMAIN_SUBTITLE[domain]}
        actions={<Button variant="secondary" size="sm" icon="refresh" loading={q.isFetching && !q.isLoading} onClick={() => void q.refetch()}>Muat ulang</Button>}
      >
        <div className="flex flex-wrap items-center gap-2">
          {properties.length > 1 && (
            <NativeSelect className="w-full sm:w-56" value={pid ?? ""} onChange={(e) => set({ property_id: e.target.value, location_id: null })} aria-label="Property">
              <option value="">Semua property</option>
              {properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </NativeSelect>
          )}
          <LocationPicker propertyId={pid} allowTypes={["building", "tower", "floor"]} value={locationId} onChange={(id) => set({ location_id: id })} placeholder={pid ? "Gedung / tower: semua" : "Pilih property untuk filter gedung"} className="w-full sm:w-64" />
          <span className="inline-flex w-full items-center gap-1 sm:w-auto">
            <DatePicker className="min-w-0 flex-1 sm:w-40 sm:flex-none" value={from || d?.from || ""} onChange={(v) => set({ from: v || null })} aria-label="Dari tanggal" />
            <span className="text-on-surface-variant">–</span>
            <DatePicker className="min-w-0 flex-1 sm:w-40 sm:flex-none" value={to || d?.to || ""} onChange={(v) => set({ to: v || null })} aria-label="Sampai tanggal" />
          </span>
          <span className="flex flex-wrap gap-1.5">
            {PRESETS.map((n) => <FilterChip key={n} selected={activePreset === n} onClick={() => { const r = lastDays(n); set({ from: r.from, to: r.to }); }}>{n} hari</FilterChip>)}
          </span>
          {filtered && <Button variant="ghost" size="sm" icon="replay" onClick={() => setSp(new URLSearchParams(), { replace: true })}>Reset filter</Button>}
        </div>
      </PageHeader>

      {q.isLoading ? (
        <>
          <KpiSkeleton count={8} />
          <CardSkeleton lines={5} />
        </>
      ) : q.error && !d ? (
        <QueryErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : d ? (
        <>
          <section aria-label="KPI" className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-4">
            {d.kpis.map((k) => <KpiCard key={k.key} kpi={k} />)}
          </section>
          {finance && (
            <nav aria-label="Pintasan keuangan" className="flex flex-wrap gap-2">
              {FINANCE_SHORTCUTS.filter((x) => can(x.perm, pid ?? undefined)).map((x) => (
                <Link key={x.to} to={pid ? `${x.to}?property_id=${pid}` : x.to} className="inline-flex h-9 items-center gap-1.5 rounded-full border border-border bg-surface px-3 text-sm font-semibold text-on-surface hover:bg-surface-container">
                  <Icon name={x.icon} size={16} aria-hidden className="text-on-surface-variant" />{x.label}
                </Link>
              ))}
            </nav>
          )}
          {domain === "engineering" && d.distribution && <HealthDistribution dist={d.distribution} pid={pid} locationId={locationId} />}
          <Card>
            <CardHeader>
              <div className="min-w-0 flex-1 basis-60">
                <CardTitle>Attention Required{d.attention_total ? ` (${fmtNumber(d.attention_total)})` : ""}</CardTitle>
                <CardSubtitle>Pengecualian yang perlu tindakan: kritis lebih dulu, lalu umur kondisi.</CardSubtitle>
              </div>
            </CardHeader>
            <CardContent>
              {finance ? (
                <FinanceAttentionList items={d.attention ?? []} total={d.attention_total} limit={expanded ? 50 : 10} onSeeAll={expanded ? undefined : () => setExpanded(true)} />
              ) : (
                <AttentionPanel
                  items={expanded && fullAttention.data ? fullAttention.data.data : d.attention}
                  total={d.attention_total}
                  limit={expanded ? 50 : 10}
                  onSeeAll={expanded ? undefined : () => setExpanded(true)}
                  emptyText="Tidak ada yang perlu perhatian di domain ini. Semua terkendali."
                />
              )}
            </CardContent>
          </Card>
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            {BREAKDOWNS[domain].map((spec) => <BreakdownCard key={spec.key} spec={spec} rows={d.breakdowns?.[spec.key] ?? []} />)}
          </div>
        </>
      ) : null}
    </div>
  );
}

function HealthDistribution({ dist, pid, locationId }: { dist: NonNullable<DomainDashboard["distribution"]>; pid: string | null; locationId: string | null }) {
  const segs = healthSegments(dist);
  const total = segs.reduce((n, s) => n + s.count, 0);
  const scope = `${pid ? `&property_id=${pid}` : ""}${locationId ? `&location_id=${locationId}` : ""}`;
  const color = (status: string) => {
    const def = statusDef("asset_health", status);
    if (!def) return undefined;
    return (def.semantic === "neutral" ? semanticStyle("neutral", "soft") : semanticStyle(def.semantic, "solid")).backgroundColor;
  };
  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1 basis-60">
          <CardTitle>Distribusi health equipment</CardTitle>
          <CardSubtitle>Healthy 90–100 · Warning 70–89 · Critical &lt; 70 (NC §14); skor dari aturan yang dapat ditelusuri — tanpa data downtime.</CardSubtitle>
        </div>
        <span className="text-sm text-on-surface-variant tnum">{fmtNumber(total)} aset</span>
      </CardHeader>
      <CardContent className="space-y-3">
        {/* batang bertumpuk: celah 2px warna surface antar segmen (dataviz: surface gap); identitas lewat legenda berlabel */}
        <div className="flex h-3 w-full gap-[2px] overflow-hidden rounded-full bg-surface" role="img" aria-label={segs.map((s) => `${statusLabel("asset_health", s.status)} ${s.count}`).join(", ")}>
          {total === 0 && <div className="h-full w-full bg-surface-container-high" />}
          {segs.filter((s) => s.count > 0).map((s) => <div key={s.status} className="h-full min-w-[4px]" style={{ flexGrow: s.count, flexBasis: 0, backgroundColor: color(s.status) }} title={`${statusLabel("asset_health", s.status)}: ${s.count}`} />)}
        </div>
        <ul className="flex flex-wrap gap-x-5 gap-y-2">
          {segs.map((s) => (
            <li key={s.status}>
              <Link to={`/assets?health_status=${s.status}${scope}`} className="inline-flex items-center gap-2 text-sm hover:underline" data-testid={`health-${s.status}`}>
                <StatusBadge objectType="asset_health" status={s.status} />
                <span className="font-semibold tnum">{fmtNumber(s.count)}</span>
                <span className="text-xs text-on-surface-variant tnum">{s.pct}%</span>
              </Link>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}

function BreakdownCard({ spec, rows }: { spec: BreakdownSpec; rows: DashboardBreakdownRow[] }) {
  const nav = useNavigate();
  const label = (r: DashboardBreakdownRow) => breakdownLabel(spec.key, r);
  const numeric = (fmt: string) => fmt === "count" || fmt === "idr" || fmt === "score";
  return (
    <Card className="min-w-0">
      <CardHeader><CardTitle>{spec.title}</CardTitle></CardHeader>
      <CardContent className="px-0 pb-0">
        {rows.length === 0 ? (
          <p className="px-5 pb-5 text-sm text-on-surface-variant">{spec.empty}</p>
        ) : (
          <Table data-testid={`breakdown-${spec.key}`}>
            <THead><tr><TH>{spec.rowTitle}</TH>{spec.cols.map((c) => <TH key={c.key} className={cn(numeric(c.fmt) && "bv-num")}>{c.label}</TH>)}</tr></THead>
            <TBody>
              {rows.map((r) => {
                const to = r.drill_down ? attentionLink(r.drill_down) : null;
                return (
                <TR key={r.key} className={cn(to && "cursor-pointer")} onClick={to ? () => nav(to) : undefined}>
                  <TD>{to ? <Link to={to} className="block max-w-[240px] truncate font-medium hover:underline" title={label(r)} onClick={(e) => e.stopPropagation()}>{label(r)}</Link> : <CellText max={240} className="font-medium">{label(r)}</CellText>}</TD>
                  {spec.cols.map((c) => <TD key={c.key} className={cn(numeric(c.fmt) && "bv-num tnum whitespace-nowrap")}><Cell fmt={c.fmt} row={r} col={c.key} /></TD>)}
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

function Cell({ fmt, row, col }: { fmt: string; row: DashboardBreakdownRow; col: string }) {
  const v = row.values?.[col];
  if (fmt === "condition") {
    const c = areaCondition(v);
    return <Badge tone={c.tone}>{c.label}</Badge>;
  }
  if (fmt === "run_status") return <StatusBadge objectType="cleaning_route_run" status={runStatusFromCode(v)} />;
  if (fmt === "progress") {
    const total = row.values?.total_stops ?? 0;
    const done = row.values?.completed_stops ?? 0;
    const pct = total > 0 ? Math.round((done / total) * 100) : 0;
    return (
      <div className="min-w-24">
        <div className="text-xs tnum">{fmtNumber(done)}/{fmtNumber(total)} area</div>
        <div className="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-surface-container-high"><div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} /></div>
      </div>
    );
  }
  return <>{formatCell(fmt as "count", v)}</>;
}
