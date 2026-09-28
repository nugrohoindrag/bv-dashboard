// Reports (PRD P1 v1.3 §26 Advanced Reports; PRD P1 v2 §41 laporan operasional; NC §56/§57 KPI naming): satu halaman dengan
// pemilih laporan + rentang tanggal. Semua angka dihitung server (`GET /reports/{name}`), dibatasi property yang diizinkan;
// halaman hanya menyajikan. Export CSV = file dari server (`?format=csv`, permission reports.reports.export).
// PRD P2 v2.1: laporan `security` (P2-SDB-03) dan `team-performance` (P2-WKL-04); rentang dari URL (?from&to — drill-down
// dashboard domain); laporan tanpa definisi presentasi dirender generik (ringkasan + breakdown) dengan label ramah.
// PRD P4 v2.1 P4-FIN-03: laporan keuangan aging, collection, revenue, ipl, sinking-fund, budget-actual, operating-cost — uang
// memakai satu helper fmtMoney (P4-FIN-04), breakdown lebar & tautan drill-down per baris, grafik seri harian/bulanan.
import { useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { Icon } from "@buildingvision/ui";
import { MetricCard } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Button, Card, CardContent, CardHeader, CardTitle, Input, NativeSelect } from "@/components/ui/primitives";
import { AsyncState, useToast } from "@/components/bv/common";
import { objectTypeLabel } from "@/components/bv/badges";
import { api, downloadFile } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtMoney } from "@/lib/format";
import { cn } from "@/lib/utils";
import { chargeTypeLabel, incidentCategoryLabel } from "@/features/dashboards/dashboard";
import { MONTHS_SHORT, fmtAxisMoney, fmtSigned } from "@/features/finance/fin-utils";
import { EMERGENCY_TYPES, labelOf } from "@/features/security/p2";
import { guessFmt, initialRange, summaryLabel, type ReportFmt } from "./report-view";

interface Point { key: string; label?: string; values: Record<string, number> }
interface Report { name: string; property_id: string | null; from: string; to: string; summary: Record<string, number>; series: Point[]; breakdowns: Record<string, Point[]> }
interface CatalogItem { name: string; title: string; description: string }

const money = (n: number) => fmtMoney(Math.round(n));
const num = (n: number) => new Intl.NumberFormat("id-ID", { maximumFractionDigits: 1 }).format(n);
const pct = (n: number) => `${num(n)}%`;
const hrs = (n: number) => (n >= 48 ? `${num(n / 24)} hari` : `${num(n)} jam`);

// Definisi presentasi per laporan: kartu ringkasan (label, key, format, tone) + seri yang digambar + label breakdown.
type Fmt = ReportFmt;
const F: Record<Fmt, (n: number) => string> = { int: num, pct, money, hours: hrs, rating: (n) => `${num(n)} / 5`, days: (n) => `${num(n)} hari`, minutes: (n) => `${num(n)} mnt`, score: (n) => (n > 0 ? num(n) : "—"), signed_money: (n) => fmtSigned(Math.round(n)), year: (n) => String(Math.round(n)) };
interface Breakdown { title: string; cols: { key: string; label: string; fmt: Fmt }[]; rowLabel?: (p: Point) => string; /** tautan drill-down per baris (route SPA). */ rowLink?: (p: Point, r: Report, propertyId: string | null) => string | null; /** lebar penuh (banyak kolom). */ wide?: boolean }
interface View {
  cards: { key: string; label: string; fmt: Fmt; tone?: (n: number) => "success" | "warning" | "error" | "info" | "primary" | "neutral" }[];
  series: { key: string; label: string; color: string; fmt?: Fmt }[];
  breakdowns: Record<string, Breakdown>;
  /** operations-kpi: kartu KPI aktual vs target dari breakdowns.kpi */ kpi?: boolean;
  /** nilai turunan per titik seri (mis. total lintas domain). */ derive?: (v: Record<string, number>) => Record<string, number>;
  /** "bar" untuk seri bulanan (kunci YYYY-MM-01); default area harian. */ chart?: "area" | "bar";
  /** catatan konteks laporan (mis. titik-waktu, tahun anggaran). */ note?: (r: Report) => string | null;
  /** judul grafik seri. */ seriesTitle?: string;
}
const good = (t: number) => (n: number) => (n >= t ? "success" : n >= t - 15 ? "warning" : "error");
const low = (t: number) => (n: number) => (n <= t ? "success" : n <= t * 2 ? "warning" : "error"); // lebih kecil lebih baik
const alarm = (n: number) => (n > 0 ? "error" : "success");
const caution = (n: number) => (n > 0 ? "warning" : "success");
const otLabel = (p: Point) => objectTypeLabel[p.key] ?? p.label ?? p.key;
// kolom umum breakdown task (count · completed · on time · overdue)
const TASK_COLS: { key: string; label: string; fmt: Fmt }[] = [{ key: "count", label: "Task", fmt: "int" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "completed_on_time", label: "Tepat waktu", fmt: "int" }, { key: "overdue", label: "Overdue", fmt: "int" }];
const INC_COLS: { key: string; label: string; fmt: Fmt }[] = [{ key: "count", label: "Incident", fmt: "int" }, { key: "resolved", label: "Resolved", fmt: "int" }, { key: "avg_resolution_hours", label: "Rata-rata selesai", fmt: "hours" }];
// PRD P2 v2.1 P2-WKL-04: kolom performa per team / per staf
const TEAM_COLS: { key: string; label: string; fmt: Fmt }[] = [{ key: "completed", label: "Selesai", fmt: "int" }, { key: "on_time_pct", label: "Tepat waktu", fmt: "pct" }, { key: "overdue_now", label: "Overdue kini", fmt: "int" }, { key: "rework", label: "Rework", fmt: "int" }, { key: "sla_breached", label: "SLA breach", fmt: "int" }, { key: "avg_completion_hours", label: "Durasi rata-rata", fmt: "hours" }, { key: "avg_inspection_score", label: "Skor inspeksi", fmt: "score" }, { key: "missed_checkpoints", label: "Checkpoint terlewat", fmt: "int" }];
// PRD P4 v2.1: kolom aging (bucket umur piutang) & budget vs actual
const AGING_COLS: { key: string; label: string; fmt: Fmt }[] = [{ key: "current", label: "Belum jatuh tempo", fmt: "money" }, { key: "d1_30", label: "1–30", fmt: "money" }, { key: "d31_60", label: "31–60", fmt: "money" }, { key: "d61_90", label: "61–90", fmt: "money" }, { key: "d90_plus", label: "> 90", fmt: "money" }, { key: "total", label: "Total", fmt: "money" }, { key: "invoices", label: "Invoice", fmt: "int" }];
const BVA_COLS: { key: string; label: string; fmt: Fmt }[] = [{ key: "budget", label: "Budget", fmt: "money" }, { key: "actual", label: "Actual", fmt: "money" }, { key: "variance", label: "Variance", fmt: "signed_money" }, { key: "variance_pct", label: "Variance %", fmt: "pct" }];
const PAY_METHOD: Record<string, string> = { transfer: "Transfer", cash: "Tunai", card: "Kartu", va: "Virtual account", qris: "QRIS", ewallet: "E-wallet", check: "Cek/giro", credit: "Saldo kredit", deposit: "Deposit", other: "Lainnya" };
const COLLECTION_CHANNEL: Record<string, string> = { phone: "Telepon", whatsapp: "WhatsApp", visit: "Kunjungan", letter: "Surat", other: "Lainnya" };
const bvaLink = (kind: string, p: Point, r: Report, pid: string | null) => `/finance/budget-actual/transactions?kind=${kind}&category=${p.key}&year=${Math.round(r.summary.fiscal_year || new Date(r.to).getFullYear())}${pid ? `&property_id=${pid}` : ""}`;

const VIEWS: Record<string, View> = {
  // ---------- PRD P1 v2 §41: laporan operasional ----------
  "operations-kpi": { cards: [], series: [], breakdowns: {}, kpi: true },
  tasks: {
    cards: [
      { key: "created", label: "Task dibuat", fmt: "int", tone: () => "primary" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "completion_rate_pct", label: "Completion rate", fmt: "pct", tone: good(90) },
      { key: "on_time_pct", label: "Tepat waktu", fmt: "pct", tone: good(85) }, { key: "open_now", label: "Terbuka", fmt: "int" }, { key: "overdue_now", label: "Overdue", fmt: "int", tone: alarm },
      { key: "overdue_rate_pct", label: "Overdue rate", fmt: "pct", tone: low(10) }, { key: "avg_completion_hours", label: "Rata-rata durasi", fmt: "hours" }, { key: "reopened", label: "Reopen", fmt: "int", tone: caution }, { key: "escalated", label: "Dieskalasi", fmt: "int", tone: caution },
    ],
    series: [{ key: "created", label: "Dibuat", color: "var(--color-primary)" }, { key: "completed", label: "Selesai", color: "var(--color-success)" }],
    breakdowns: { category: { title: "Per kategori", cols: TASK_COLS }, type: { title: "Per tipe", cols: TASK_COLS }, priority: { title: "Per prioritas", cols: TASK_COLS }, team: { title: "Per team", cols: TASK_COLS }, assignee: { title: "Per assignee", cols: TASK_COLS } },
  },
  sla: {
    cards: [
      { key: "resolution_compliance_pct", label: "SLA compliance (resolusi)", fmt: "pct", tone: good(90) }, { key: "response_compliance_pct", label: "SLA respons", fmt: "pct", tone: good(90) }, { key: "resolved", label: "Selesai ber-SLA", fmt: "int", tone: () => "primary" },
      { key: "resolved_within_sla", label: "Selesai dalam SLA", fmt: "int" }, { key: "on_track_now", label: "On track (kini)", fmt: "int", tone: () => "success" }, { key: "at_risk_now", label: "At risk (kini)", fmt: "int", tone: caution },
      { key: "breached_now", label: "Breached (kini)", fmt: "int", tone: alarm }, { key: "breached_in_period", label: "Breach pada periode", fmt: "int", tone: alarm }, { key: "responded", label: "Direspons", fmt: "int" }, { key: "responded_within_sla", label: "Respons tepat waktu", fmt: "int" },
    ],
    series: [{ key: "resolved", label: "Selesai", color: "var(--color-primary)" }, { key: "resolved_within_sla", label: "Dalam SLA", color: "var(--color-success)" }, { key: "breached", label: "Breach", color: "var(--color-error)" }],
    breakdowns: {
      object_type: { title: "Per object", rowLabel: otLabel, cols: [{ key: "resolved", label: "Selesai", fmt: "int" }, { key: "resolution_compliance_pct", label: "Compliance", fmt: "pct" }, { key: "response_compliance_pct", label: "Respons", fmt: "pct" }, { key: "at_risk_now", label: "At risk", fmt: "int" }, { key: "breached_now", label: "Breached", fmt: "int" }] },
      priority: { title: "Per prioritas", cols: [{ key: "resolved", label: "Selesai", fmt: "int" }, { key: "resolved_within_sla", label: "Dalam SLA", fmt: "int" }, { key: "breached", label: "Breach", fmt: "int" }] },
    },
  },
  incidents: {
    cards: [
      { key: "reported", label: "Incident dilaporkan", fmt: "int", tone: () => "primary" }, { key: "critical", label: "Kritis", fmt: "int", tone: alarm }, { key: "high", label: "High", fmt: "int", tone: caution }, { key: "resolved", label: "Resolved", fmt: "int" },
      { key: "resolution_rate_pct", label: "Resolution rate", fmt: "pct", tone: good(90) }, { key: "open_now", label: "Terbuka", fmt: "int" }, { key: "critical_open_now", label: "Kritis terbuka", fmt: "int", tone: alarm }, { key: "avg_resolution_hours", label: "Rata-rata selesai", fmt: "hours" }, { key: "sla_breached", label: "SLA breach", fmt: "int", tone: alarm },
    ],
    series: [{ key: "reported", label: "Dilaporkan", color: "var(--color-error)" }, { key: "resolved", label: "Resolved", color: "var(--color-success)" }],
    breakdowns: { severity: { title: "Per severity", cols: INC_COLS }, category: { title: "Per kategori", cols: INC_COLS }, type: { title: "Per tipe", cols: INC_COLS }, status: { title: "Per status", cols: INC_COLS }, location: { title: "Lokasi terbanyak", cols: [{ key: "count", label: "Incident", fmt: "int" }] } },
  },
  backlog: {
    cards: [
      { key: "open", label: "Backlog terbuka", fmt: "int", tone: () => "primary" }, { key: "tasks", label: "Task", fmt: "int" }, { key: "work_orders", label: "Work Order", fmt: "int" }, { key: "service_requests", label: "Service Request", fmt: "int" }, { key: "incidents", label: "Incident", fmt: "int" },
      { key: "overdue", label: "Overdue", fmt: "int", tone: alarm }, { key: "sla_breached", label: "SLA breach", fmt: "int", tone: alarm }, { key: "unassigned", label: "Belum ditugaskan", fmt: "int", tone: caution }, { key: "critical", label: "Kritis", fmt: "int", tone: alarm }, { key: "avg_age_days", label: "Rata-rata umur", fmt: "days" }, { key: "older_than_7_days", label: "Umur > 7 hari", fmt: "int", tone: caution },
    ],
    series: [{ key: "incoming", label: "Masuk", color: "var(--color-primary)" }, { key: "completed", label: "Selesai", color: "var(--color-success)" }],
    breakdowns: {
      age: { title: "Umur backlog", cols: [{ key: "count", label: "Item", fmt: "int" }] },
      object_type: { title: "Per object", rowLabel: otLabel, cols: [{ key: "count", label: "Item", fmt: "int" }, { key: "overdue", label: "Overdue", fmt: "int" }, { key: "sla_breached", label: "SLA breach", fmt: "int" }] },
      priority: { title: "Per prioritas", cols: [{ key: "count", label: "Item", fmt: "int" }, { key: "overdue", label: "Overdue", fmt: "int" }] },
      team: { title: "Per team", cols: [{ key: "count", label: "Item", fmt: "int" }, { key: "overdue", label: "Overdue", fmt: "int" }] },
      status: { title: "Per status", rowLabel: (p) => { const [ot, st] = p.key.split(":"); return `${objectTypeLabel[ot] ?? ot} · ${st ?? p.label}`; }, cols: [{ key: "count", label: "Item", fmt: "int" }] },
    },
  },
  // ---------- PRD P2 v2.1 P2-SDB-03: Security Report ----------
  security: {
    cards: [
      { key: "patrol_completion_pct", label: "Patrol completion", fmt: "pct", tone: good(90) }, { key: "checkpoint_compliance_pct", label: "Checkpoint compliance", fmt: "pct", tone: good(95) }, { key: "checkpoints_missed", label: "Checkpoint terlewat", fmt: "int", tone: caution },
      { key: "patrol_total", label: "Patrol terjadwal", fmt: "int", tone: () => "primary" }, { key: "incidents_reported", label: "Incident dilaporkan", fmt: "int" }, { key: "incidents_critical", label: "Incident kritis", fmt: "int", tone: alarm },
      { key: "avg_response_minutes", label: "Rata-rata respons incident", fmt: "minutes", tone: (n) => (n <= 15 ? "success" : n <= 30 ? "warning" : "error") }, { key: "unauthorized_entry", label: "Unauthorized entry", fmt: "int", tone: caution },
      { key: "emergencies_raised", label: "Emergency", fmt: "int", tone: caution }, { key: "avg_emergency_ack_minutes", label: "Rata-rata respons emergency", fmt: "minutes" }, { key: "emergencies_false_alarm", label: "Alarm palsu", fmt: "int" },
      { key: "visitors_checked_in", label: "Tamu check-in", fmt: "int" }, { key: "parking_violations", label: "Pelanggaran parkir", fmt: "int", tone: caution },
    ],
    // satu satuan (checkpoint) per grafik; patrol selesai ada di kartu ringkasan
    series: [{ key: "checkpoints_scanned", label: "Checkpoint discan", color: "var(--color-success)" }, { key: "checkpoints_missed", label: "Checkpoint terlewat", color: "var(--color-error)" }],
    breakdowns: {
      route: { title: "Per rute patrol", cols: [{ key: "patrols", label: "Patrol", fmt: "int" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "checkpoint_compliance_pct", label: "Compliance", fmt: "pct" }, { key: "checkpoints_missed", label: "Terlewat", fmt: "int" }] },
      missed_checkpoint: { title: "Checkpoint paling sering terlewat", cols: [{ key: "missed", label: "Terlewat", fmt: "int" }] },
      incident_category: { title: "Incident per kategori", rowLabel: (p) => incidentCategoryLabel(p.key), cols: [{ key: "count", label: "Incident", fmt: "int" }, { key: "critical", label: "Kritis", fmt: "int" }] },
      emergency_type: { title: "Emergency per tipe", rowLabel: (p) => labelOf(EMERGENCY_TYPES, p.key), cols: [{ key: "count", label: "Emergency", fmt: "int" }, { key: "avg_ack_minutes", label: "Rata-rata respons", fmt: "minutes" }] },
    },
  },
  // ---------- PRD P2 v2.1 P2-WKL-04: Team Performance ----------
  "team-performance": {
    cards: [
      { key: "completed", label: "Pekerjaan selesai", fmt: "int", tone: () => "primary" }, { key: "on_time_pct", label: "Tepat waktu", fmt: "pct", tone: good(85) }, { key: "open_now", label: "Terbuka kini", fmt: "int" },
      { key: "overdue_now", label: "Overdue kini", fmt: "int", tone: alarm }, { key: "rework", label: "Rework (re-clean)", fmt: "int", tone: caution }, { key: "sla_breached", label: "SLA breach", fmt: "int", tone: alarm },
      { key: "avg_completion_hours", label: "Durasi rata-rata", fmt: "hours" }, { key: "avg_inspection_score", label: "Skor inspeksi HK", fmt: "score" }, { key: "missed_checkpoints", label: "Checkpoint terlewat", fmt: "int", tone: caution },
    ],
    // satu seri (total lintas domain); rincian domain di tabel per team
    series: [{ key: "total", label: "Pekerjaan selesai", color: "var(--color-primary)" }],
    derive: (v) => ({ total: (v.engineering ?? 0) + (v.security ?? 0) + (v.housekeeping ?? 0) }),
    breakdowns: { team: { title: "Per team", cols: TEAM_COLS }, staff: { title: "Per staf", cols: TEAM_COLS } },
  },
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
    cards: [{ key: "created", label: "WO dibuat", fmt: "int", tone: () => "primary" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "on_time_pct", label: "Tepat waktu", fmt: "pct", tone: good(85) }, { key: "within_sla_pct", label: "Dalam SLA", fmt: "pct", tone: good(90) }, { key: "avg_completion_hours", label: "Rata-rata durasi", fmt: "hours" }, { key: "open_now", label: "Terbuka", fmt: "int" }, { key: "overdue_now", label: "Overdue", fmt: "int", tone: (n) => (n > 0 ? "error" : "success") }, { key: "draft_now", label: "Draft", fmt: "int" }, { key: "reopened", label: "Reopen", fmt: "int" }, { key: "vendor_assigned", label: "Ke vendor", fmt: "int" },
      // PRD P1 v2 §26: komponen biaya
      { key: "estimated_cost_total", label: "Estimasi biaya", fmt: "money" }, { key: "parts_cost_total", label: "Biaya parts", fmt: "money" }, { key: "service_cost_total", label: "Biaya jasa", fmt: "money" }, { key: "other_cost_total", label: "Biaya lain", fmt: "money" }, { key: "actual_cost_total", label: "Biaya aktual", fmt: "money" }],
    series: [{ key: "created", label: "Dibuat", color: "var(--color-primary)" }, { key: "completed", label: "Selesai", color: "var(--color-success)" }],
    breakdowns: { type: { title: "Per tipe", cols: [{ key: "count", label: "WO", fmt: "int" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "actual_cost_total", label: "Biaya", fmt: "money" }] }, priority: { title: "Per prioritas", cols: [{ key: "count", label: "WO", fmt: "int" }, { key: "completed_on_time", label: "Tepat waktu", fmt: "int" }] }, status: { title: "Per status", cols: [{ key: "count", label: "WO", fmt: "int" }] }, team: { title: "Per tim", cols: [{ key: "count", label: "WO", fmt: "int" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "avg_completion_hours", label: "Rata-rata durasi", fmt: "hours" }] }, asset: { title: "Aset dengan WO terbanyak", cols: [{ key: "count", label: "WO", fmt: "int" }, { key: "actual_cost_total", label: "Biaya", fmt: "money" }] } },
  },
  maintenance: {
    cards: [{ key: "scheduled", label: "Jadwal Preventive Maintenance", fmt: "int", tone: () => "primary" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "completed_on_time", label: "Tepat waktu", fmt: "int" }, { key: "compliance_pct", label: "Kepatuhan Preventive Maintenance", fmt: "pct", tone: good(90) }, { key: "overdue", label: "Overdue", fmt: "int", tone: (n) => (n > 0 ? "error" : "success") }, { key: "skipped", label: "Dilewati", fmt: "int" }, { key: "work_orders_created", label: "WO dibuat", fmt: "int" }],
    series: [{ key: "due", label: "Jatuh tempo", color: "var(--color-primary)" }, { key: "completed", label: "Selesai", color: "var(--color-success)" }],
    breakdowns: { plan: { title: "Per rencana Preventive Maintenance", cols: [{ key: "scheduled", label: "Jadwal", fmt: "int" }, { key: "completed", label: "Selesai", fmt: "int" }, { key: "completed_on_time", label: "Tepat waktu", fmt: "int" }] }, status: { title: "Per status", cols: [{ key: "count", label: "Jadwal", fmt: "int" }] } },
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
  // ---------- PRD P4 v2.1 P4-FIN-03: laporan keuangan ----------
  aging: {
    cards: [
      { key: "total", label: "Total piutang", fmt: "money", tone: () => "primary" }, { key: "current", label: "Belum jatuh tempo", fmt: "money" }, { key: "d1_30", label: "1–30 hari", fmt: "money" },
      { key: "d31_60", label: "31–60 hari", fmt: "money", tone: caution }, { key: "d61_90", label: "61–90 hari", fmt: "money", tone: caution }, { key: "d90_plus", label: "> 90 hari", fmt: "money", tone: alarm }, { key: "invoices", label: "Invoice terbuka", fmt: "int" },
    ],
    series: [],
    note: () => "Titik-waktu (per hari ini) — rentang tanggal tidak dipakai untuk aging.",
    breakdowns: {
      tenant: { title: "Per tenant / unit (100 terbesar)", cols: AGING_COLS, wide: true, rowLink: (p, _r, pid) => `/billing/collections?party=${p.key}${pid ? `&property_id=${pid}` : ""}` },
      property: { title: "Per property", cols: AGING_COLS, wide: true, rowLink: (p) => `/billing/aging?property_id=${p.key}` },
      type: { title: "Per jenis tagihan", cols: AGING_COLS, wide: true, rowLabel: (p) => chargeTypeLabel(p.key) },
    },
  },
  collection: {
    cards: [
      { key: "collection_rate_pct", label: "Collection rate", fmt: "pct", tone: good(90) }, { key: "due_amount", label: "Tagihan jatuh tempo", fmt: "money", tone: () => "primary" }, { key: "collected_on_due", label: "Terbayar (atas jatuh tempo)", fmt: "money" },
      { key: "cash_collected", label: "Kas diterima", fmt: "money" }, { key: "on_time_rate_pct", label: "Bayar tepat waktu", fmt: "pct", tone: good(80) }, { key: "due_invoices", label: "Invoice jatuh tempo", fmt: "int" }, { key: "paid_invoices", label: "Invoice lunas", fmt: "int" },
      { key: "reminders_sent", label: "Pengingat terkirim", fmt: "int" }, { key: "collection_logs", label: "Log penagihan", fmt: "int" }, { key: "promises_open", label: "Janji bayar menunggu", fmt: "int", tone: (n) => (n > 0 ? "info" : "neutral") },
      { key: "promises_kept", label: "Janji ditepati", fmt: "int", tone: (n) => (n > 0 ? "success" : "neutral") }, { key: "promises_broken", label: "Janji ingkar", fmt: "int", tone: alarm },
    ],
    seriesTitle: "Jatuh tempo vs kas diterima (harian)",
    series: [{ key: "due_amount", label: "Jatuh tempo", color: "var(--color-chart-secondary)", fmt: "money" }, { key: "cash_collected", label: "Kas diterima", color: "var(--color-chart-primary)", fmt: "money" }],
    breakdowns: {
      tenant: { title: "Collection per tenant / unit (terendah dulu)", cols: [{ key: "due_amount", label: "Jatuh tempo", fmt: "money" }, { key: "collected", label: "Terbayar", fmt: "money" }, { key: "rate_pct", label: "Rate", fmt: "pct" }], rowLink: (p, _r, pid) => `/billing/collections?party=${p.key}${pid ? `&property_id=${pid}` : ""}` },
      method: { title: "Pembayaran per metode", rowLabel: (p) => PAY_METHOD[p.key] ?? p.key, cols: [{ key: "count", label: "Transaksi", fmt: "int" }, { key: "amount", label: "Nilai", fmt: "money" }] },
      channel: { title: "Log penagihan per kanal", rowLabel: (p) => COLLECTION_CHANNEL[p.key] ?? p.key, cols: [{ key: "count", label: "Kontak", fmt: "int" }, { key: "promises", label: "Janji bayar", fmt: "int" }] },
    },
  },
  revenue: {
    cards: [
      { key: "net_revenue", label: "Pendapatan bersih", fmt: "money", tone: () => "primary" }, { key: "billed_revenue", label: "Pendapatan ditagihkan", fmt: "money" }, { key: "credit_notes", label: "Credit note", fmt: "money", tone: caution },
      { key: "tax", label: "Pajak (PPN)", fmt: "money" }, { key: "deposits_billed", label: "Deposit ditagihkan", fmt: "money" }, { key: "invoices", label: "Invoice terbit", fmt: "int" },
    ],
    seriesTitle: "Pendapatan ditagihkan (harian)",
    series: [{ key: "billed_revenue", label: "Pendapatan", color: "var(--color-chart-primary)", fmt: "money" }],
    breakdowns: {
      charge_type: { title: "Per jenis tagihan", rowLabel: (p) => chargeTypeLabel(p.key), cols: [{ key: "amount", label: "Nilai", fmt: "money" }, { key: "tax", label: "Pajak", fmt: "money" }, { key: "items", label: "Item", fmt: "int" }] },
      property: { title: "Per property", cols: [{ key: "amount", label: "Pendapatan", fmt: "money" }] },
    },
  },
  ipl: {
    cards: [
      { key: "ipl_billed", label: "IPL ditagihkan", fmt: "money", tone: () => "primary" }, { key: "ipl_collected", label: "IPL terbayar", fmt: "money" }, { key: "collection_rate_pct", label: "Collection rate IPL", fmt: "pct", tone: good(90) },
      { key: "ipl_outstanding", label: "Tunggakan IPL", fmt: "money", tone: caution }, { key: "units_billed", label: "Unit ditagih", fmt: "int" }, { key: "units_overdue", label: "Unit menunggak", fmt: "int", tone: alarm },
    ],
    series: [],
    breakdowns: {
      unit: { title: "Per unit (tunggakan terbesar)", wide: true, cols: [{ key: "billed", label: "Ditagihkan (periode)", fmt: "money" }, { key: "outstanding", label: "Tunggakan", fmt: "money" }, { key: "oldest_days_overdue", label: "Tertua", fmt: "days" }], rowLink: (p, _r, pid) => `/billing/statement?unit_location_id=${p.key}${pid ? `&property_id=${pid}` : ""}` },
    },
  },
  "sinking-fund": {
    cards: [
      { key: "closing", label: "Saldo akhir", fmt: "money", tone: () => "primary" }, { key: "opening", label: "Saldo awal", fmt: "money" }, { key: "receipts", label: "Penerimaan", fmt: "money", tone: () => "success" },
      { key: "usage", label: "Penggunaan", fmt: "money" }, { key: "adjustments", label: "Penyesuaian & saldo awal", fmt: "signed_money" },
    ],
    seriesTitle: "Saldo dana (harian)",
    series: [{ key: "balance", label: "Saldo", color: "var(--color-chart-primary)", fmt: "money" }],
    breakdowns: {
      property: { title: "Per property", cols: [{ key: "balance", label: "Saldo", fmt: "money" }, { key: "receipts", label: "Penerimaan", fmt: "money" }, { key: "usage", label: "Penggunaan", fmt: "money" }], rowLink: (p) => `/billing/sinking-fund?property_id=${p.key}` },
      usage: { title: "Penggunaan dana", cols: [{ key: "amount", label: "Nominal", fmt: "money" }] },
    },
  },
  "budget-actual": {
    cards: [
      { key: "revenue_actual", label: "Pendapatan (actual)", fmt: "money", tone: () => "primary" }, { key: "revenue_budget", label: "Pendapatan (budget)", fmt: "money" }, { key: "cost_actual", label: "Biaya (actual)", fmt: "money" },
      { key: "cost_budget", label: "Biaya (budget)", fmt: "money" }, { key: "net_actual", label: "Selisih (actual)", fmt: "money", tone: (n) => (n < 0 ? "error" : "success") }, { key: "net_budget", label: "Selisih (budget)", fmt: "money" },
    ],
    series: [],
    note: (r) => (r.summary.fiscal_year ? `Tahun anggaran ${Math.round(r.summary.fiscal_year)} — bulan yang beririsan dengan rentang laporan; budget = revisi disetujui (atau terbaru) tiap property.` : null),
    breakdowns: {
      revenue: { title: "Pendapatan per kategori", cols: BVA_COLS, rowLink: (p, r, pid) => bvaLink("revenue", p, r, pid) },
      cost: { title: "Biaya per kategori", cols: BVA_COLS, rowLink: (p, r, pid) => bvaLink("cost", p, r, pid) },
    },
  },
  "operating-cost": {
    cards: [{ key: "total_cost", label: "Total biaya operasional", fmt: "money", tone: () => "primary" }],
    chart: "bar",
    seriesTitle: "Biaya per bulan",
    series: [{ key: "cost", label: "Biaya", color: "var(--color-chart-primary)", fmt: "money" }],
    note: (r) => (r.summary.fiscal_year ? `Tahun ${Math.round(r.summary.fiscal_year)}: work order selesai (actual cost), consumable cleaning, dan biaya manual.` : null),
    breakdowns: {
      category: { title: "Per kategori", cols: [{ key: "amount", label: "Biaya", fmt: "money" }, { key: "share_pct", label: "Porsi", fmt: "pct" }], rowLink: (p, r) => `/finance/costs?category=${p.key}&from=${r.from.slice(0, 10)}&to=${r.to.slice(0, 10)}` },
    },
  },
};

const isoDay = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

export default function ReportsPage() {
  const { name = "service-requests" } = useParams();
  const nav = useNavigate();
  const [sp] = useSearchParams();
  const { propertyId, can } = useAuth();
  const toast = useToast();
  const [exporting, setExporting] = useState(false);
  // drill-down dashboard domain: /reports/{name}?from=YYYY-MM-DD&to=YYYY-MM-DD
  const [range, setRange] = useState(() => initialRange(sp.get("from"), sp.get("to")));
  const catalog = useQuery({ queryKey: ["reports-catalog"], queryFn: () => api<{ data: CatalogItem[] }>("reports").then((r) => r.data), staleTime: 60_000 });
  const report = useQuery({ queryKey: ["report", name, propertyId, range.from, range.to], queryFn: () => api<Report>(`reports/${name}`, { query: { property_id: propertyId ?? undefined, from: range.from, to: range.to } }) });
  const view = VIEWS[name] ?? (report.data ? genericView(report.data) : undefined);
  const current = catalog.data?.find((c) => c.name === name);
  const preset = (days: number) => { const to = new Date(); const from = new Date(); from.setDate(to.getDate() - (days - 1)); setRange({ from: isoDay(from), to: isoDay(to) }); };
  // CSV dari server (summary + seri + breakdown, sesuai scope property) — PRD P1 v2 §41 "Export mengikuti capability P0"
  const download = async () => {
    setExporting(true);
    try {
      await downloadFile(`reports/${name}`, { property_id: propertyId ?? undefined, from: range.from, to: range.to, format: "csv" }, `${name}_${range.from}_${range.to}.csv`);
      toast.action("exported", current?.title ?? name);
    } catch (e) {
      toast.failed("exported", e, current?.title ?? name);
    } finally {
      setExporting(false);
    }
  };
  if (!view && report.isError) return <Alert variant="critical">Laporan tidak dikenal atau gagal dimuat.</Alert>;
  return (
    <div>
      <PageHeader title="Reports" subtitle={current ? current.description : "Laporan operasional & komersial (PRD P1 §26). Angka dihitung server sesuai scope property."} actions={can("reports.reports.export") && <Button variant="secondary" onClick={download} loading={exporting} disabled={!report.data}><Icon name="download" size={16} /> Export CSV</Button>}>
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
        {(r) => !view ? null : (
          <div className="space-y-5">
            {view.kpi && <KpiGrid rows={r.breakdowns.kpi ?? []} />}
            {view.cards.length > 0 && (
              <div className="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-5">
                {view.cards.map((c) => { const v = r.summary[c.key] ?? 0; return <MetricCard key={c.key} label={c.label} value={F[c.fmt](v)} tone={c.tone ? c.tone(v) : "neutral"} />; })}
              </div>
            )}
            {view.note?.(r) && <Alert variant="info">{view.note(r)}</Alert>}
            {view.series.length > 0 && r.series.length > 0 && <SeriesChart view={view} r={r} />}
            <div className="grid grid-cols-1 gap-5 xl:grid-cols-2">
              {Object.entries(view.breakdowns).map(([k, b]) => {
                const rows = r.breakdowns[k] ?? [];
                return (
                  <Card key={k} className={cn(b.wide && "xl:col-span-2")}>
                    <CardHeader><CardTitle>{b.title}</CardTitle></CardHeader>
                    <CardContent>
                      {rows.length === 0 ? <p className="text-sm text-muted-foreground">Tidak ada data pada rentang ini.</p> : (
                        <div className="bv-table-scroll"><table className="w-full text-sm" data-testid={`breakdown-${k}`}>
                          <thead><tr className="text-left text-xs uppercase text-muted-foreground"><th className="py-1">&nbsp;</th>{b.cols.map((c) => <th key={c.key} className="whitespace-nowrap py-1 pl-3 text-right">{c.label}</th>)}</tr></thead>
                          <tbody>{rows.map((p) => {
                            const label = b.rowLabel ? b.rowLabel(p) : p.label || p.key;
                            const to = b.rowLink ? b.rowLink(p, r, propertyId) : null;
                            return <tr key={p.key} className="border-t border-border"><td className="py-1.5 pr-2">{to ? <Link to={to} className="hover:underline">{label}</Link> : label}</td>{b.cols.map((c) => <td key={c.key} className="tnum whitespace-nowrap py-1.5 pl-3 text-right">{F[c.fmt](p.values[c.key] ?? 0)}</td>)}</tr>;
                          })}</tbody>
                        </table></div>
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

// ---------- Grafik seri: satu sumbu (satuan sama), legenda + tooltip; seri bulanan (operating-cost) sebagai batang ----------
function SeriesChart({ view, r }: { view: View; r: Report }) {
  const monthly = view.chart === "bar";
  const isMoney = view.series[0]?.fmt === "money";
  const data = r.series.map((p) => ({ date: monthly ? MONTHS_SHORT[Number(p.key.slice(5, 7)) - 1] ?? p.key : p.key.slice(5), ...p.values, ...(view.derive ? view.derive(p.values) : {}) }));
  const fmtV = (v: number, k: string) => (view.series.find((s) => s.key === k)?.fmt === "money" ? money(v) : num(v));
  const labelOf = (k: string) => view.series.find((s) => s.key === k)?.label ?? k;
  const tick = { fontSize: 11 };
  const tipStyle = { borderRadius: 8, border: "1px solid var(--color-border)", background: "var(--color-surface)", color: "var(--color-on-surface)" };
  const yFmt = (v: number) => (isMoney ? fmtAxisMoney(v) : String(v));
  return (
    <Card>
      <CardHeader><CardTitle>{view.seriesTitle ?? "Tren harian"}</CardTitle></CardHeader>
      <CardContent>
        <div className="h-64" role="img" aria-label={view.seriesTitle ?? "Tren harian"}>
          <ResponsiveContainer width="100%" height="100%">
            {monthly ? (
              <BarChart data={data} margin={{ left: 8, right: 8, top: 8 }}>
                <CartesianGrid vertical={false} stroke="var(--color-outline-variant)" />
                <XAxis dataKey="date" tick={tick} axisLine={false} tickLine={false} />
                <YAxis tick={tick} width={56} axisLine={false} tickLine={false} tickFormatter={yFmt} />
                <Tooltip cursor={{ fill: "var(--color-surface-container)" }} formatter={(v: number, k: string) => [fmtV(v, k), labelOf(k)]} contentStyle={tipStyle} />
                {view.series.map((s) => <Bar key={s.key} dataKey={s.key} fill={s.color} radius={[4, 4, 0, 0]} maxBarSize={32} isAnimationActive={false} />)}
              </BarChart>
            ) : (
              <AreaChart data={data} margin={{ left: 8, right: 8, top: 8 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--color-outline-variant)" />
                <XAxis dataKey="date" tick={tick} />
                <YAxis tick={tick} width={56} tickFormatter={yFmt} />
                <Tooltip formatter={(v: number, k: string) => [fmtV(v, k), labelOf(k)]} contentStyle={tipStyle} />
                {view.series.map((s) => <Area key={s.key} type="monotone" dataKey={s.key} stroke={s.color} fill={s.color} fillOpacity={0.15} strokeWidth={2} />)}
              </AreaChart>
            )}
          </ResponsiveContainer>
        </div>
        {view.series.length > 1 && <div className="mt-2 flex gap-4 text-xs text-muted-foreground">{view.series.map((s) => <span key={s.key} className="flex items-center gap-1"><span className="inline-block h-2 w-2 rounded-full" style={{ background: s.color }} /> {s.label}</span>)}</div>}
      </CardContent>
    </Card>
  );
}

// ---------- Tampilan generik ----------
/** Laporan tanpa definisi presentasi: semua ringkasan + semua breakdown (kolom dari nilai baris pertama). */
function genericView(r: Report): View {
  const breakdowns: View["breakdowns"] = {};
  for (const [k, rows] of Object.entries(r.breakdowns ?? {})) {
    const keys = Object.keys(rows[0]?.values ?? {});
    breakdowns[k] = { title: summaryLabel(k), cols: keys.map((c) => ({ key: c, label: summaryLabel(c), fmt: guessFmt(c) })) };
  }
  return { cards: Object.keys(r.summary ?? {}).map((k) => ({ key: k, label: summaryLabel(k), fmt: guessFmt(k) })), series: [], breakdowns };
}

// ---------- Operations KPI (PRD P1 v2 §51): aktual vs target; overdue_task_rate = lebih kecil lebih baik ----------
function KpiGrid({ rows }: { rows: Point[] }) {
  if (!rows.length) return <p className="text-sm text-muted-foreground">Tidak ada data KPI pada rentang ini.</p>;
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {rows.map((k) => {
        const v = k.values;
        const lower = v.lower_is_better === 1;
        const noData = !v.basis;
        const met = v.met === 1;
        return (
          <Card key={k.key} className="p-4" railTone={noData ? undefined : met ? "success" : "error"}>
            <div className="text-sm text-on-surface-variant">{k.label || k.key}</div>
            <div className="mt-1 flex items-baseline gap-2">
              <span className="text-display font-extrabold tnum leading-9">{noData ? "—" : pct(v.actual_pct ?? 0)}</span>
              <span className="text-sm text-on-surface-variant">target {lower ? "<" : ">"} {pct(v.target_pct ?? 0)}</span>
            </div>
            <div className="mt-2 flex items-center justify-between text-xs">
              <span className={cn("rounded-full px-2 py-0.5 font-semibold", noData ? "bg-neutral-soft text-neutral-text" : met ? "bg-success-soft text-success-text" : "bg-critical-soft text-critical-text")}>{noData ? "Belum ada data" : met ? "Tercapai" : "Belum tercapai"}</span>
              <span className="tnum text-muted-foreground">basis {num(v.basis ?? 0)}{lower ? " · lebih kecil lebih baik" : ""}</span>
            </div>
          </Card>
        );
      })}
    </div>
  );
}
