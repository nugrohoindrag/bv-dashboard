// Reports (PRD P1 v1.3 §26 Advanced Reports; NC §56/§57 KPI naming): satu halaman dengan pemilih laporan + rentang tanggal.
// Semua angka dihitung server (`GET /reports/{name}`), dibatasi property yang diizinkan; halaman hanya menyajikan.
import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { Icon } from "@buildingvision/ui";
import { MetricCard } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Button, Card, CardContent, CardHeader, CardTitle, Input, NativeSelect } from "@/components/ui/primitives";
import { AsyncState } from "@/components/bv/common";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";

interface Point { key: string; label?: string; values: Record<string, number> }
interface Report { name: string; property_id: string | null; from: string; to: string; summary: Record<string, number>; series: Point[]; breakdowns: Record<string, Point[]> }
interface CatalogItem { name: string; title: string; description: string }

const rp = (n: number) => "Rp " + new Intl.NumberFormat("id-ID").format(Math.round(n));
const num = (n: number) => new Intl.NumberFormat("id-ID", { maximumFractionDigits: 1 }).format(n);
const pct = (n: number) => `${num(n)}%`;
const hrs = (n: number) => (n >= 48 ? `${num(n / 24)} hari` : `${num(n)} jam`);

// Definisi presentasi per laporan: kartu ringkasan (label, key, format, tone) + seri yang digambar + label breakdown.
type Fmt = "int" | "pct" | "money" | "hours" | "rating" | "days";
const F: Record<Fmt, (n: number) => string> = { int: num, pct, money: rp, hours: hrs, rating: (n) => `${num(n)} / 5`, days: (n) => `${num(n)} hari` };
interface View { cards: { key: string; label: string; fmt: Fmt; tone?: (n: number) => "success" | "warning" | "error" | "info" | "primary" | "neutral" }[]; series: { key: string; label: string; color: string; fmt?: Fmt }[]; breakdowns: Record<string, { title: string; cols: { key: string; label: string; fmt: Fmt }[] }> }
const good = (t: number) => (n: number) => (n >= t ? "success" : n >= t - 15 ? "warning" : "error");
const VIEWS: Record<string, View> = {
  "service-requests": {
    cards: [
      { key: "created", label: "Ticket masuk", fmt: "int", tone: () => "primary" }, { key: "resolved", label: "Diselesaikan", fmt: "int" }, { key: "open_now", label: "Terbuka saat ini", fmt: "int", tone: (n) => (n > 0 ? "info" : "neutral") },
      { key: "sla_resolution_pct", label: "SLA compliance (resolusi)", fmt: "pct", tone: good(90) }, { key: "sla_response_pct", label: "SLA respons", fmt: "pct", tone: good(90) },
      { key: "avg_response_hours", label: "Rata-rata respons", fmt: "hours" }, { key: "avg_resolution_hours", label: "Rata-rata penyelesaian", fmt: "hours" },
      { key: "reopen_rate_pct", label: "Reopen rate", fmt: "pct", tone: (n) => (n <= 5 ? "success" : n <= 15 ? "warning" : "error") }, { key: "csat_avg", label: "CSAT", fmt: "rating", tone: (n) => (n >= 4 ? "success" : n >= 3 ? "warning" : n > 0 ? "error" : "neutral") }, { key: "tenant_app", label: "Via Tenant App", fmt: "int" },
    ],
    series: [{ key: "created", label: "Masuk", color: "var(--color-primary)" }, { key: "resolved", label: "Selesai", color: "var(--color-success)" }],
    breakdowns: { category: { title: "Per kategori", cols: [{ key: "count", label: "Ticket", fmt: "int" }, { key: "reopened", label: "Reopen", fmt: "int" }, { key: "avg_resolution_hours", label: "Rata-rata selesai", fmt: "hours" }] }, status: { title: "Per status", cols: [{ key: "count", label: "Ticket", fmt: "int" }] }, priority: { title: "Per prioritas", cols: [{ key: "count", label: "Ticket", fmt: "int" }] }, csat: { title: "Distribusi CSAT", cols: [{ key: "count", label: "Penilaian", fmt: "int" }] }, location: { title: "Lokasi berulang (>1 ticket)", cols: [{ key: "count", label: "Ticket", fmt: "int" }] } },
  },
  "work-orders": {
    cards: [{ key: "created", label: "WO dibuat", fmt: "int", tone: () => "primary" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "on_time_pct", label: "Tepat waktu", fmt: "pct", tone: good(85) }, { key: "avg_completion_hours", label: "Rata-rata durasi", fmt: "hours" }, { key: "open_now", label: "Terbuka", fmt: "int" }, { key: "overdue_now", label: "Overdue", fmt: "int", tone: (n) => (n > 0 ? "error" : "success") }, { key: "reopened", label: "Reopen", fmt: "int" }, { key: "actual_cost_total", label: "Biaya aktual", fmt: "money" }, { key: "vendor_assigned", label: "Ke vendor", fmt: "int" }],
    series: [{ key: "created", label: "Dibuat", color: "var(--color-primary)" }, { key: "completed", label: "Selesai", color: "var(--color-success)" }],
    breakdowns: { type: { title: "Per tipe", cols: [{ key: "count", label: "WO", fmt: "int" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "actual_cost_total", label: "Biaya", fmt: "money" }] }, priority: { title: "Per prioritas", cols: [{ key: "count", label: "WO", fmt: "int" }, { key: "completed_on_time", label: "Tepat waktu", fmt: "int" }] }, status: { title: "Per status", cols: [{ key: "count", label: "WO", fmt: "int" }] }, team: { title: "Per tim", cols: [{ key: "count", label: "WO", fmt: "int" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "avg_completion_hours", label: "Rata-rata durasi", fmt: "hours" }] }, asset: { title: "Aset dengan WO terbanyak", cols: [{ key: "count", label: "WO", fmt: "int" }, { key: "actual_cost_total", label: "Biaya", fmt: "money" }] } },
  },
  maintenance: {
    cards: [{ key: "scheduled", label: "Jadwal PM", fmt: "int", tone: () => "primary" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "completed_on_time", label: "Tepat waktu", fmt: "int" }, { key: "compliance_pct", label: "PM compliance", fmt: "pct", tone: good(90) }, { key: "overdue", label: "Overdue", fmt: "int", tone: (n) => (n > 0 ? "error" : "success") }, { key: "skipped", label: "Dilewati", fmt: "int" }, { key: "work_orders_created", label: "WO dibuat", fmt: "int" }],
    series: [{ key: "due", label: "Jatuh tempo", color: "var(--color-primary)" }, { key: "completed", label: "Selesai", color: "var(--color-success)" }],
    breakdowns: { plan: { title: "Per rencana PM", cols: [{ key: "scheduled", label: "Jadwal", fmt: "int" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "completed_on_time", label: "Tepat waktu", fmt: "int" }] }, status: { title: "Per status", cols: [{ key: "count", label: "Jadwal", fmt: "int" }] } },
  },
  "patrol-cleaning": {
    cards: [{ key: "patrol_completion_pct", label: "Patrol completion", fmt: "pct", tone: good(90) }, { key: "patrol_total", label: "Patrol", fmt: "int" }, { key: "cleaning_completion_pct", label: "Cleaning completion", fmt: "pct", tone: good(90) }, { key: "cleaning_total", label: "Cleaning task", fmt: "int" }, { key: "inspection_completion_pct", label: "Inspeksi selesai", fmt: "pct", tone: good(90) }, { key: "completed_late", label: "Selesai terlambat", fmt: "int", tone: (n) => (n > 0 ? "warning" : "success") }, { key: "cancelled", label: "Dibatalkan", fmt: "int" }],
    series: [{ key: "patrol_completed", label: "Patrol selesai", color: "var(--color-primary)" }, { key: "cleaning_completed", label: "Cleaning selesai", color: "var(--color-success)" }],
    breakdowns: { team: { title: "Per tim", cols: [{ key: "total", label: "Task", fmt: "int" }, { key: "completed", label: "Selesai", fmt: "int" }] }, findings: { title: "Finding per severity", cols: [{ key: "count", label: "Finding", fmt: "int" }] } },
  },
  facilities: {
    cards: [{ key: "bookings", label: "Booking", fmt: "int", tone: () => "primary" }, { key: "confirmed", label: "Dikonfirmasi", fmt: "int" }, { key: "hours_booked", label: "Jam terpakai", fmt: "hours" }, { key: "cancelled", label: "Dibatalkan", fmt: "int" }, { key: "rejected", label: "Ditolak", fmt: "int" }, { key: "no_show_pct", label: "No-show", fmt: "pct", tone: (n) => (n <= 5 ? "success" : "warning") }, { key: "tenant_app", label: "Via Tenant App", fmt: "int" }],
    series: [{ key: "bookings", label: "Booking", color: "var(--color-primary)" }, { key: "hours_booked", label: "Jam", color: "var(--color-success)" }],
    breakdowns: { facility: { title: "Utilisasi per fasilitas", cols: [{ key: "bookings", label: "Booking", fmt: "int" }, { key: "hours_booked", label: "Jam terpakai", fmt: "hours" }, { key: "hours_available", label: "Jam tersedia", fmt: "hours" }, { key: "utilization_pct", label: "Utilisasi", fmt: "pct" }, { key: "no_show", label: "No-show", fmt: "int" }] }, status: { title: "Per status", cols: [{ key: "count", label: "Booking", fmt: "int" }] } },
  },
  visitors: {
    cards: [{ key: "registered", label: "Tamu terdaftar", fmt: "int", tone: () => "primary" }, { key: "checked_in", label: "Check-in", fmt: "int" }, { key: "headcount_checked_in", label: "Orang masuk", fmt: "int" }, { key: "show_rate_pct", label: "Show rate", fmt: "pct", tone: good(80) }, { key: "avg_visit_minutes", label: "Rata-rata kunjungan (mnt)", fmt: "int" }, { key: "expired", label: "Kedaluwarsa", fmt: "int" }, { key: "denied", label: "Ditolak", fmt: "int" }, { key: "tenant_app", label: "Via Tenant App", fmt: "int" }],
    series: [{ key: "registered", label: "Terdaftar", color: "var(--color-primary)" }, { key: "checked_in", label: "Check-in", color: "var(--color-success)" }],
    breakdowns: { status: { title: "Per status", cols: [{ key: "count", label: "Tamu", fmt: "int" }] }, channel: { title: "Per kanal", cols: [{ key: "count", label: "Tamu", fmt: "int" }] }, hour: { title: "Jam kedatangan", cols: [{ key: "count", label: "Check-in", fmt: "int" }] } },
  },
  billing: {
    cards: [{ key: "issued", label: "Invoice diterbitkan", fmt: "int", tone: () => "primary" }, { key: "issued_amount", label: "Nilai diterbitkan", fmt: "money" }, { key: "collected_amount", label: "Terkumpul", fmt: "money" }, { key: "collection_rate_pct", label: "Collection rate", fmt: "pct", tone: good(90) }, { key: "paid", label: "Lunas", fmt: "int" }, { key: "overdue_now", label: "Overdue", fmt: "int", tone: (n) => (n > 0 ? "error" : "success") }, { key: "overdue_amount", label: "Nilai overdue", fmt: "money" }, { key: "outstanding_amount", label: "Outstanding", fmt: "money" }, { key: "avg_days_to_pay", label: "Rata-rata hari bayar", fmt: "days" }],
    series: [{ key: "issued_amount", label: "Diterbitkan", color: "var(--color-primary)", fmt: "money" }, { key: "collected_amount", label: "Terkumpul", color: "var(--color-success)", fmt: "money" }],
    breakdowns: { status: { title: "Per status invoice", cols: [{ key: "count", label: "Invoice", fmt: "int" }, { key: "amount", label: "Nilai", fmt: "money" }, { key: "outstanding", label: "Outstanding", fmt: "money" }] }, type: { title: "Per jenis", cols: [{ key: "count", label: "Invoice", fmt: "int" }, { key: "amount", label: "Nilai", fmt: "money" }, { key: "collected", label: "Terkumpul", fmt: "money" }] }, provider: { title: "Pembayaran per provider/metode", cols: [{ key: "count", label: "Transaksi", fmt: "int" }, { key: "paid", label: "Berhasil", fmt: "int" }, { key: "paid_amount", label: "Nilai", fmt: "money" }, { key: "failed", label: "Gagal/expired", fmt: "int" }] }, tenant_overdue: { title: "Tenant dengan tunggakan", cols: [{ key: "count", label: "Invoice", fmt: "int" }, { key: "outstanding", label: "Outstanding", fmt: "money" }] } },
  },
  vendors: {
    cards: [{ key: "vendor_work_orders", label: "WO vendor", fmt: "int", tone: () => "primary" }, { key: "vendors_active", label: "Vendor aktif", fmt: "int" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "on_time_pct", label: "Tepat waktu", fmt: "pct", tone: good(85) }, { key: "avg_completion_hours", label: "Rata-rata durasi", fmt: "hours" }, { key: "reopened", label: "Reopen", fmt: "int", tone: (n) => (n > 0 ? "warning" : "success") }, { key: "actual_cost_total", label: "Biaya aktual", fmt: "money" }],
    series: [],
    breakdowns: { vendor: { title: "Performa per vendor", cols: [{ key: "work_orders", label: "WO", fmt: "int" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "on_time_pct", label: "Tepat waktu", fmt: "pct" }, { key: "avg_completion_hours", label: "Rata-rata durasi", fmt: "hours" }, { key: "reopened", label: "Reopen", fmt: "int" }, { key: "actual_cost_total", label: "Biaya", fmt: "money" }] }, category: { title: "Per kategori layanan", cols: [{ key: "count", label: "WO", fmt: "int" }] } },
  },
  inventory: {
    cards: [{ key: "transactions", label: "Transaksi stok", fmt: "int", tone: () => "primary" }, { key: "qty_in", label: "Masuk", fmt: "int" }, { key: "qty_out", label: "Keluar", fmt: "int" }, { key: "usage_transactions", label: "Pemakaian (WO)", fmt: "int" }, { key: "usage_cost", label: "Biaya pemakaian", fmt: "money" }, { key: "adjustments", label: "Penyesuaian", fmt: "int" }, { key: "low_stock_items", label: "Item stok rendah", fmt: "int", tone: (n) => (n > 0 ? "warning" : "success") }],
    series: [{ key: "qty_in", label: "Masuk", color: "var(--color-success)" }, { key: "qty_out", label: "Keluar", color: "var(--color-error)" }],
    breakdowns: { type: { title: "Per jenis transaksi", cols: [{ key: "count", label: "Transaksi", fmt: "int" }, { key: "quantity", label: "Qty", fmt: "int" }] }, item_usage: { title: "Item paling banyak dipakai", cols: [{ key: "quantity", label: "Qty", fmt: "int" }, { key: "cost", label: "Biaya", fmt: "money" }, { key: "transactions", label: "Transaksi", fmt: "int" }] }, low_stock: { title: "Stok di bawah minimum", cols: [{ key: "quantity", label: "Stok", fmt: "int" }, { key: "min_stock", label: "Minimum", fmt: "int" }] } },
  },
};

const isoDay = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

export default function ReportsPage() {
  const { name = "service-requests" } = useParams();
  const nav = useNavigate();
  const { propertyId } = useAuth();
  const [range, setRange] = useState(() => { const to = new Date(); const from = new Date(); from.setDate(to.getDate() - 29); return { from: isoDay(from), to: isoDay(to) }; });
  const catalog = useQuery({ queryKey: ["reports-catalog"], queryFn: () => api<{ data: CatalogItem[] }>("reports").then((r) => r.data), staleTime: 60_000 });
  const report = useQuery({ queryKey: ["report", name, propertyId, range.from, range.to], queryFn: () => api<Report>(`reports/${name}`, { query: { property_id: propertyId ?? undefined, from: range.from, to: range.to } }) });
  const view = VIEWS[name];
  const current = catalog.data?.find((c) => c.name === name);
  const preset = (days: number) => { const to = new Date(); const from = new Date(); from.setDate(to.getDate() - (days - 1)); setRange({ from: isoDay(from), to: isoDay(to) }); };
  const csv = useMemo(() => {
    if (!report.data || !view) return "";
    const lines: string[] = [`report,${name}`, `from,${range.from}`, `to,${range.to}`, "", "summary"];
    for (const c of view.cards) lines.push(`${c.label},${report.data.summary[c.key] ?? 0}`);
    if (report.data.series.length) { lines.push("", `date,${view.series.map((s) => s.label).join(",")}`); for (const p of report.data.series) lines.push(`${p.key},${view.series.map((s) => p.values[s.key] ?? 0).join(",")}`); }
    for (const [k, b] of Object.entries(view.breakdowns)) { const rows = report.data.breakdowns[k] ?? []; if (!rows.length) continue; lines.push("", `${b.title},${b.cols.map((c) => c.label).join(",")}`); for (const p of rows) lines.push(`"${(p.label || p.key).replace(/"/g, '""')}",${b.cols.map((c) => p.values[c.key] ?? 0).join(",")}`); }
    return lines.join("\n");
  }, [report.data, view, name, range]);
  const download = () => { const blob = new Blob([csv], { type: "text/csv;charset=utf-8" }); const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = `${name}_${range.from}_${range.to}.csv`; a.click(); URL.revokeObjectURL(a.href); };
  if (!view) return <Alert variant="critical">Laporan tidak dikenal.</Alert>;
  return (
    <div>
      <PageHeader title="Reports" subtitle={current ? current.description : "Laporan operasional & komersial (PRD P1 §26). Angka dihitung server sesuai scope property."} actions={<Button variant="secondary" onClick={download} disabled={!report.data}><Icon name="download" size={16} /> Export CSV</Button>}>
        <div className="flex flex-wrap items-center gap-2">
          <NativeSelect className="w-64" value={name} onChange={(e) => nav(`/reports/${e.target.value}`)}>{(catalog.data ?? Object.keys(VIEWS).map((n) => ({ name: n, title: n, description: "" }))).map((c) => <option key={c.name} value={c.name}>{c.title}</option>)}</NativeSelect>
          <Input type="date" className="w-40" value={range.from} onChange={(e) => setRange({ ...range, from: e.target.value })} />
          <span className="text-muted-foreground">→</span>
          <Input type="date" className="w-40" value={range.to} onChange={(e) => setRange({ ...range, to: e.target.value })} />
          {[7, 30, 90].map((d) => <Button key={d} size="sm" variant="ghost" onClick={() => preset(d)}>{d} hari</Button>)}
          {!propertyId && <span className="text-xs text-muted-foreground">Semua property yang diizinkan</span>}
        </div>
      </PageHeader>
      <AsyncState query={report}>
        {(r) => (
          <div className="space-y-5">
            <div className="grid grid-cols-5 gap-4">
              {view.cards.map((c) => { const v = r.summary[c.key] ?? 0; return <MetricCard key={c.key} label={c.label} value={F[c.fmt](v)} tone={c.tone ? c.tone(v) : "neutral"} />; })}
            </div>
            {view.series.length > 0 && (
              <Card>
                <CardHeader><CardTitle>Tren harian</CardTitle></CardHeader>
                <CardContent>
                  <div className="h-64">
                    <ResponsiveContainer width="100%" height="100%">
                      <AreaChart data={r.series.map((p) => ({ date: p.key.slice(5), ...p.values }))} margin={{ left: 8, right: 8, top: 8 }}>
                        <CartesianGrid strokeDasharray="3 3" stroke="var(--color-outline-variant)" />
                        <XAxis dataKey="date" tick={{ fontSize: 11 }} /><YAxis tick={{ fontSize: 11 }} width={48} tickFormatter={(v: number) => (view.series[0]?.fmt === "money" ? `${Math.round(v / 1e6)}jt` : String(v))} />
                        <Tooltip formatter={(v: number, k: string) => [view.series.find((s) => s.key === k)?.fmt === "money" ? rp(v) : num(v), view.series.find((s) => s.key === k)?.label ?? k]} />
                        {view.series.map((s) => <Area key={s.key} type="monotone" dataKey={s.key} stroke={s.color} fill={s.color} fillOpacity={0.15} strokeWidth={2} />)}
                      </AreaChart>
                    </ResponsiveContainer>
                  </div>
                  <div className="mt-2 flex gap-4 text-xs text-muted-foreground">{view.series.map((s) => <span key={s.key} className="flex items-center gap-1"><span className="inline-block h-2 w-2 rounded-full" style={{ background: s.color }} /> {s.label}</span>)}</div>
                </CardContent>
              </Card>
            )}
            <div className="grid grid-cols-2 gap-5">
              {Object.entries(view.breakdowns).map(([k, b]) => {
                const rows = r.breakdowns[k] ?? [];
                return (
                  <Card key={k}>
                    <CardHeader><CardTitle>{b.title}</CardTitle></CardHeader>
                    <CardContent>
                      {rows.length === 0 ? <p className="text-sm text-muted-foreground">Tidak ada data pada rentang ini.</p> : (
                        <table className="w-full text-sm">
                          <thead><tr className="text-left text-xs uppercase text-muted-foreground"><th className="py-1">&nbsp;</th>{b.cols.map((c) => <th key={c.key} className="py-1 text-right">{c.label}</th>)}</tr></thead>
                          <tbody>{rows.map((p) => <tr key={p.key} className="border-t border-border"><td className="py-1.5 pr-2">{p.label || p.key}</td>{b.cols.map((c) => <td key={c.key} className="tnum py-1.5 text-right">{F[c.fmt](p.values[c.key] ?? 0)}</td>)}</tr>)}</tbody>
                        </table>
                      )}
                    </CardContent>
                  </Card>
                );
              })}
            </div>
          </div>
        )}
      </AsyncState>
    </div>
  );
}
