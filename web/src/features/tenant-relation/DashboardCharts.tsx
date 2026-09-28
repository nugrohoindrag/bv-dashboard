// Komponen dashboard Tenant Relation (PRD P3 v2.1 §7.1 P3-TSH-01..09; Roadmap Principle 9 "every KPI drills down"):
// kartu KPI (nilai + definisi + delta periode, seluruh kartu menautkan drill-down), tren 30 hari (masuk vs selesai; keluhan per
// hari sebagai grafik terpisah — satu sumbu per grafik), breakdown kategori/tipe/kanal (bar horizontal satu warna, baris = tautan).
// Warna seri memakai token chart DS (--color-chart-primary/secondary, tervalidasi CVD mode terang); teks memakai token teks.
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { Bar, BarChart, CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { format } from "date-fns";
import { id as localeID } from "date-fns/locale";
import { Icon } from "@buildingvision/ui";
import type { Tone } from "@buildingvision/ui/bv";
import { Badge, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Segmented } from "@/components/ui/primitives";
import { fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { CountRow, DayPoint } from "./types";
import type { PeriodDelta } from "./utils";

// ---------- Kartu KPI ----------
export interface KpiTileProps {
  label: string;
  value: string;
  /** Definisi singkat (juga tooltip). */
  hint: string;
  to: string;
  /** Rail + badge status (hanya bila nilai menandakan kondisi: berisiko / melewati SLA). */
  tone?: Tone | null;
  statusLabel?: string;
  delta?: PeriodDelta & { period: string };
  sub?: string;
  testId?: string;
}

const DELTA_ICON: Record<PeriodDelta["direction"], string> = { up: "trending_up", down: "trending_down", flat: "trending_flat" };

export function KpiTile({ label, value, hint, to, tone, statusLabel, delta, sub, testId }: KpiTileProps) {
  return (
    <Card railTone={tone ?? undefined} interactive className="min-w-0">
      <Link to={to} className="flex h-full flex-col p-4" title={hint} aria-label={`${label}: ${value}. ${hint}`} data-testid={testId}>
        <span className="text-sm font-medium text-on-surface-variant">{label}</span>
        <span className="mt-1 text-display font-extrabold leading-9 text-on-surface">{value}</span>
        {(delta || sub) && (
          <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs">
            {delta && (
              <span className={cn("inline-flex items-center gap-0.5 font-semibold", delta.type === "positive" ? "text-success-text" : delta.type === "negative" ? "text-critical-text" : "text-on-surface-variant")}>
                <Icon name={DELTA_ICON[delta.direction]} size={14} aria-hidden />
                {delta.text}
              </span>
            )}
            {delta && <span className="text-on-surface-variant">{delta.period}</span>}
            {sub && <span className="text-on-surface-variant">{sub}</span>}
          </div>
        )}
        <p className="mt-1 line-clamp-2 text-caption text-on-surface-variant">{hint}</p>
        <div className="mt-auto flex flex-wrap items-center justify-between gap-2 pt-2 text-xs">
          {tone && statusLabel ? <Badge tone={tone}>{statusLabel}</Badge> : <span />}
          <span className="inline-flex items-center gap-0.5 font-semibold text-primary">Lihat detail<Icon name="chevron_right" size={14} aria-hidden /></span>
        </div>
      </Link>
    </Card>
  );
}

// ---------- Tren 30 hari ----------
const SERIES = [
  { key: "created", label: "Masuk", color: "var(--color-chart-primary)" },
  { key: "resolved", label: "Selesai", color: "var(--color-chart-secondary)" },
] as const;

function dayLabel(date: string, pattern = "d MMM"): string {
  const d = new Date(`${date}T00:00:00`);
  return Number.isNaN(d.getTime()) ? date : format(d, pattern, { locale: localeID });
}

interface TipPayload { dataKey?: string | number; value?: number; color?: string; payload?: { date: string } }
/** Tooltip: nilai lebih menonjol dari nama seri; kunci seri berupa garis pendek (bukan kotak). */
function SeriesTooltip({ active, payload, names }: { active?: boolean; payload?: TipPayload[]; names: Record<string, string> }) {
  if (!active || !payload?.length) return null;
  const date = payload[0]?.payload?.date;
  return (
    <div className="min-w-[150px] rounded-[var(--radius-md)] border border-border bg-surface px-3 py-2 text-xs shadow-popover">
      {date && <div className="mb-1 text-on-surface-variant">{dayLabel(date, "EEEE, d MMM yyyy")}</div>}
      <ul className="space-y-0.5">
        {payload.map((p) => (
          <li key={String(p.dataKey)} className="flex items-center gap-2">
            <span aria-hidden className="inline-block h-0.5 w-3 rounded-full" style={{ backgroundColor: p.color }} />
            <span className="font-bold tnum text-on-surface">{fmtNumber(p.value ?? 0)}</span>
            <span className="text-on-surface-variant">{names[String(p.dataKey)] ?? String(p.dataKey)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

const axisTick = { fontSize: 11, fill: "var(--color-on-surface-variant)" };

export function TrendCard({ series, timezone }: { series: DayPoint[]; timezone: string }) {
  const [view, setView] = useState<"chart" | "table">("chart");
  const data = useMemo(() => series.map((p) => ({ ...p, label: dayLabel(p.date) })), [series]);
  const totals = useMemo(() => series.reduce((s, p) => ({ created: s.created + p.created, resolved: s.resolved + p.resolved, complaints: s.complaints + p.complaints }), { created: 0, resolved: 0, complaints: 0 }), [series]);
  const names: Record<string, string> = { created: "Masuk", resolved: "Selesai", complaints: "Keluhan" };
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Tren 30 hari</CardTitle>
          <CardSubtitle>Permintaan masuk vs selesai per hari, dan keluhan per hari · zona waktu {timezone}</CardSubtitle>
        </div>
        <Segmented value={view} onChange={setView} options={[{ value: "chart", label: "Grafik" }, { value: "table", label: "Tabel" }]} />
      </CardHeader>
      <CardContent>
        {series.length === 0 ? (
          <p className="py-8 text-center text-sm text-on-surface-variant">Belum ada data 30 hari terakhir.</p>
        ) : view === "table" ? (
          <div className="max-h-[296px] overflow-y-auto rounded-[var(--radius-md)] border border-border">
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-surface-container text-left text-xs text-on-surface-variant">
                <tr><th className="px-3 py-2 font-semibold">Tanggal</th><th className="px-3 py-2 text-right font-semibold">Masuk</th><th className="px-3 py-2 text-right font-semibold">Selesai</th><th className="px-3 py-2 text-right font-semibold">Keluhan</th></tr>
              </thead>
              <tbody>
                {[...series].reverse().map((p) => (
                  <tr key={p.date} className="border-t border-border">
                    <td className="px-3 py-1.5">{dayLabel(p.date, "EEE, d MMM yyyy")}</td>
                    <td className="px-3 py-1.5 text-right tnum">{fmtNumber(p.created)}</td>
                    <td className="px-3 py-1.5 text-right tnum">{fmtNumber(p.resolved)}</td>
                    <td className="px-3 py-1.5 text-right tnum">{fmtNumber(p.complaints)}</td>
                  </tr>
                ))}
              </tbody>
              <tfoot className="border-t border-border bg-surface-container-low font-semibold">
                <tr><td className="px-3 py-1.5">Total 30 hari</td><td className="px-3 py-1.5 text-right tnum">{fmtNumber(totals.created)}</td><td className="px-3 py-1.5 text-right tnum">{fmtNumber(totals.resolved)}</td><td className="px-3 py-1.5 text-right tnum">{fmtNumber(totals.complaints)}</td></tr>
              </tfoot>
            </table>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-5 lg:grid-cols-3">
            <figure className="min-w-0 lg:col-span-2" aria-label="Grafik permintaan masuk dan selesai per hari">
              <ul className="mb-2 flex flex-wrap gap-4 text-xs text-on-surface-variant" aria-label="Legenda">
                {SERIES.map((s) => (
                  <li key={s.key} className="inline-flex items-center gap-1.5">
                    <span aria-hidden className="inline-block h-0.5 w-4 rounded-full" style={{ backgroundColor: s.color }} />
                    {s.label} <span className="font-semibold text-on-surface">{fmtNumber(totals[s.key])}</span>
                  </li>
                ))}
              </ul>
              <div className="h-60">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={data} margin={{ top: 8, right: 12, bottom: 0, left: 0 }}>
                    <CartesianGrid vertical={false} stroke="var(--color-chart-grid)" />
                    <XAxis dataKey="label" tick={axisTick} tickLine={false} axisLine={{ stroke: "var(--color-outline-variant)" }} interval="preserveStartEnd" minTickGap={28} />
                    <YAxis allowDecimals={false} width={36} tick={axisTick} tickLine={false} axisLine={false} />
                    <Tooltip content={<SeriesTooltip names={names} />} cursor={{ stroke: "var(--color-outline)", strokeWidth: 1 }} />
                    {SERIES.map((s) => (
                      <Line key={s.key} type="monotone" dataKey={s.key} stroke={s.color} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" dot={false} activeDot={{ r: 4, stroke: "var(--color-surface)", strokeWidth: 2 }} isAnimationActive={false} />
                    ))}
                  </LineChart>
                </ResponsiveContainer>
              </div>
            </figure>
            <figure className="min-w-0" aria-label="Grafik keluhan per hari">
              <div className="mb-2 text-xs text-on-surface-variant">Keluhan per hari · total <span className="font-semibold text-on-surface">{fmtNumber(totals.complaints)}</span></div>
              <div className="h-60">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={data} margin={{ top: 8, right: 4, bottom: 0, left: 0 }} barCategoryGap={2}>
                    <CartesianGrid vertical={false} stroke="var(--color-chart-grid)" />
                    <XAxis dataKey="label" tick={axisTick} tickLine={false} axisLine={{ stroke: "var(--color-outline-variant)" }} interval="preserveStartEnd" minTickGap={28} />
                    <YAxis allowDecimals={false} width={28} tick={axisTick} tickLine={false} axisLine={false} />
                    <Tooltip content={<SeriesTooltip names={names} />} cursor={{ fill: "var(--color-surface-container)" }} />
                    <Bar dataKey="complaints" fill="var(--color-chart-primary)" radius={[4, 4, 0, 0]} maxBarSize={24} isAnimationActive={false} />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            </figure>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ---------- Breakdown kategori / tipe / kanal ----------
export function BreakdownCard({ title, subtitle, rows, emptyLabel = "Belum ada permintaan 30 hari terakhir." }: { title: string; subtitle?: string; rows: CountRow[]; emptyLabel?: string }) {
  const max = Math.max(1, ...rows.map((r) => r.count));
  const total = rows.reduce((s, r) => s + r.count, 0);
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>{title}</CardTitle>
          {subtitle && <CardSubtitle>{subtitle}</CardSubtitle>}
        </div>
      </CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <p className="text-sm text-on-surface-variant">{emptyLabel}</p>
        ) : (
          <ul className="-mx-2 space-y-0.5">
            {rows.map((r) => (
              <li key={r.key}>
                <Link to={r.drill_down} className="group block rounded-[var(--radius-md)] px-2 py-1.5 hover:bg-surface-container" aria-label={`${r.label}: ${r.count} permintaan (${Math.round((r.count / Math.max(1, total)) * 100)}%). Lihat daftar`}>
                  <div className="flex items-center justify-between gap-3 text-sm">
                    <span className="min-w-0 truncate text-on-surface">{r.label}</span>
                    <span className="inline-flex shrink-0 items-center gap-1 font-semibold tnum text-on-surface">
                      {fmtNumber(r.count)}
                      <Icon name="chevron_right" size={14} className="text-on-surface-variant opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100" aria-hidden />
                    </span>
                  </div>
                  <div className="mt-1 h-2" aria-hidden>
                    <div className="h-2 rounded-r-[4px]" style={{ width: `${Math.max(2, (r.count / max) * 100)}%`, backgroundColor: "var(--color-chart-primary)" }} />
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
