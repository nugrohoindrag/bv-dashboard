// Helper murni Dashboard domain (PRD P2 v2.1 §5.1 P2-ENG-01..04, §6.1 P2-SDB-01..02, §7.1 P2-HDB-01..05; Roadmap v2.1 §25.2,
// §25.8). Server (api/internal/overview/domains.go) menghitung KPI, severity, hint, dan tautan drill-down; halaman hanya
// menyajikan. Severity KPI & kondisi area adalah kondisi metrik (bukan status object di status map).
import type { Tone } from "@buildingvision/ui/bv";
import { fmtMoney, fmtNumber } from "@/lib/format";
import { appLink, fmtMoneyShort } from "@/features/finance/fin-utils";

export type Domain = "engineering" | "security" | "housekeeping" | "finance";
export const DOMAINS: Domain[] = ["engineering", "security", "housekeeping", "finance"];
export const DOMAIN_TITLE: Record<Domain, string> = { engineering: "Engineering Dashboard", security: "Security Dashboard", housekeeping: "Housekeeping Dashboard", finance: "Finance Dashboard" };
export const DOMAIN_SUBTITLE: Record<Domain, string> = {
  engineering: "Kesehatan equipment, preventive & corrective maintenance, biaya — setiap KPI dapat ditelusuri ke daftar record.",
  security: "Incident, patrol & checkpoint, emergency, dan visitor — setiap KPI dapat ditelusuri ke daftar record.",
  housekeeping: "Cleaning hari ini, kualitas inspeksi, finding & rework, staf on-duty, route, dan consumable.",
  // PRD P4 v2.1 P4-FIN-01 (Roadmap v2.1 §25.2 Financial Health, §25.8 Phase 4)
  finance: "Collection rate, outstanding, aging, revenue, operating cost, budget vs actual, dan sinking fund — setiap KPI dapat ditelusuri.",
};

const num1 = (v: number) => new Intl.NumberFormat("id-ID", { maximumFractionDigits: 1 }).format(v);

/** Nilai KPI sesuai unit server: count | pct | minutes | idr | score. */
export function formatKpiValue(unit: string, value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return "—";
  switch (unit) {
    case "pct":
      return `${num1(value)}%`;
    case "minutes":
      return `${num1(value)} mnt`;
    case "idr":
      return fmtMoney(value, "IDR");
    case "score":
      return num1(value);
    default:
      return fmtNumber(Math.round(value));
  }
}
/**
 * Tampilan KPI di kartu: uang ≥ Rp 100 juta diringkas ("Rp 350 jt", "Rp 1,25 M"; nilai penuh di `title`), persen variance
 * bertanda ("+5,2%"); unit lain sama dengan `formatKpiValue`.
 */
export function formatKpiDisplay(unit: string, value: number | null | undefined, key = ""): { text: string; title: string } {
  const full = formatKpiValue(unit, value);
  if (value === null || value === undefined || !Number.isFinite(value)) return { text: full, title: full };
  if (unit === "idr") return { text: Math.abs(value) >= 1e8 ? fmtMoneyShort(value) : full, title: full };
  if (unit === "pct" && /variance/.test(key) && value > 0) return { text: `+${full}`, title: full };
  return { text: full, title: full };
}

/** Akhiran kecil di samping angka (skor inspeksi 0–100). */
export const kpiSuffix = (unit: string): string | undefined => (unit === "score" ? "/100" : undefined);

/** Tone rail kartu KPI dari severity server (normal → tanpa rail). */
export function severityTone(severity: string | null | undefined): Tone | undefined {
  return severity === "critical" ? "error" : severity === "warning" ? "warning" : undefined;
}
export const SEVERITY_LABEL: Record<string, string> = { critical: "Kritis", warning: "Perlu perhatian" };

/** Rentang default dashboard = 30 hari terakhir termasuk hari ini (sama dengan default server). */
export function lastDays(days: number, today: Date = new Date()): { from: string; to: string } {
  const to = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  const from = new Date(to);
  from.setDate(to.getDate() - (days - 1));
  return { from: iso(from), to: iso(to) };
}
const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

// ---------- Distribusi health equipment (NC §14) ----------
export const HEALTH_ORDER = ["healthy", "warning", "critical", "offline", "unknown"] as const;
export function healthSegments(dist: Partial<Record<string, number>> | null | undefined): { status: string; count: number; pct: number }[] {
  const counts = HEALTH_ORDER.map((s) => ({ status: s as string, count: Math.max(0, dist?.[s] ?? 0) }));
  const total = counts.reduce((n, c) => n + c.count, 0);
  return counts.map((c) => ({ ...c, pct: total ? Math.round((c.count / total) * 1000) / 10 : 0 }));
}

// ---------- Breakdown (P2-ENG-04, P2-SDB-02, P2-HDB-*) ----------
export type ColFmt = "count" | "idr" | "score" | "condition" | "run_status" | "progress";
export interface BreakdownCol { key: string; label: string; fmt: ColFmt }
export interface BreakdownSpec { key: string; title: string; rowTitle: string; cols: BreakdownCol[]; empty: string }

const c = (key: string, label: string, fmt: ColFmt = "count"): BreakdownCol => ({ key, label, fmt });
export const BREAKDOWNS: Record<Domain, BreakdownSpec[]> = {
  engineering: [
    { key: "building", title: "Per gedung", rowTitle: "Gedung", cols: [c("open_work_orders", "WO terbuka"), c("overdue_pm", "Preventive Maintenance terlambat"), c("at_risk_assets", "Aset at-risk")], empty: "Tidak ada WO terbuka, Preventive Maintenance terlambat, atau aset at-risk." },
    { key: "equipment_category", title: "Per kategori equipment", rowTitle: "Kategori", cols: [c("assets", "Aset"), c("at_risk_assets", "At-risk"), c("critical_assets", "Kritis"), c("open_work_orders", "WO terbuka")], empty: "Belum ada aset ber-equipment." },
    { key: "team", title: "Per team", rowTitle: "Team", cols: [c("open_work_orders", "WO terbuka"), c("overdue", "Overdue"), c("sla_breached", "SLA breach")], empty: "Tidak ada WO terbuka yang ditugaskan ke team." },
  ],
  security: [
    { key: "route", title: "Patrol per rute", rowTitle: "Rute", cols: [c("patrols", "Patrol"), c("completed", "Selesai"), c("checkpoints_scanned", "Checkpoint discan"), c("checkpoints_missed", "Terlewat")], empty: "Belum ada patrol pada periode ini." },
    { key: "incident_category", title: "Incident per kategori", rowTitle: "Kategori", cols: [c("reported", "Dilaporkan"), c("open", "Terbuka"), c("critical", "Kritis")], empty: "Tidak ada incident pada periode ini." },
    { key: "building", title: "Incident terbuka per gedung", rowTitle: "Gedung", cols: [c("open_incidents", "Terbuka"), c("critical", "Kritis")], empty: "Tidak ada incident terbuka." },
  ],
  housekeeping: [
    { key: "routes_today", title: "Cleaning Route hari ini", rowTitle: "Route", cols: [c("progress", "Progres", "progress"), c("status", "Status", "run_status")], empty: "Tidak ada run Cleaning Route hari ini." },
    { key: "team", title: "Per team (hari ini)", rowTitle: "Team", cols: [c("tasks_today", "Task"), c("completed", "Selesai"), c("overdue", "Overdue")], empty: "Tidak ada cleaning task hari ini yang ditugaskan ke team." },
    { key: "area", title: "Kondisi area (inspeksi)", rowTitle: "Area", cols: [c("avg_score", "Skor rata-rata", "score"), c("inspections", "Inspeksi"), c("condition", "Kondisi", "condition")], empty: "Belum ada inspeksi housekeeping berskor pada periode ini." },
    { key: "consumables", title: "Consumable terbanyak", rowTitle: "Item", cols: [c("quantity", "Qty"), c("total_cost", "Biaya", "idr")], empty: "Belum ada pemakaian consumable pada periode ini." },
  ],
  finance: [
    { key: "aging", title: "Aging piutang", rowTitle: "Umur", cols: [c("amount", "Sisa tagihan", "idr")], empty: "Tidak ada piutang terbuka." },
    { key: "overdue_tenant", title: "Tunggakan terbesar", rowTitle: "Tenant / unit", cols: [c("outstanding", "Tunggakan", "idr"), c("invoices", "Invoice"), c("oldest_days", "Hari terlama")], empty: "Tidak ada tunggakan." },
    { key: "revenue_type", title: "Revenue per jenis tagihan", rowTitle: "Jenis", cols: [c("amount", "Ditagihkan", "idr")], empty: "Belum ada tagihan terbit pada periode ini." },
    { key: "cost_category", title: "Biaya operasional YTD", rowTitle: "Kategori", cols: [c("ytd", "YTD", "idr")], empty: "Belum ada biaya tercatat tahun ini." },
  ],
};

/** Jenis tagihan / charge type (PRD P4 v2.1 P4-INV-02) → label breakdown revenue. */
export const CHARGE_TYPE_LABEL: Record<string, string> = {
  service_charge: "Service Charge", ipl: "IPL", utility: "Utilitas", electricity: "Listrik", water: "Air", parking: "Parkir", sinking_fund: "Sinking Fund", penalty: "Denda",
  deposit: "Deposit", rental: "Sewa", facility: "Fasilitas", additional_charge: "Biaya Tambahan", other: "Lainnya",
};
export const chargeTypeLabel = (code: string) => CHARGE_TYPE_LABEL[code] ?? code.replace(/_/g, " ");

/** Label baris breakdown: incident per kategori & revenue per jenis tagihan diberi label; lainnya label server. */
export function breakdownLabel(specKey: string, row: { key: string; label?: string | null }): string {
  if (specKey === "incident_category") return incidentCategoryLabel(row.key);
  if (specKey === "revenue_type") return chargeTypeLabel(row.key);
  return row.label || row.key;
}

// ---------- Attention Required domain finance (overview/finance.go — item ada di respons dashboard) ----------
export const FINANCE_ATTENTION: Record<string, { label: string; icon: string }> = {
  payment_pending: { label: "Verifikasi pembayaran", icon: "pending" },
  promise_broken: { label: "Janji bayar ingkar", icon: "event_busy" },
  billing_run_ready: { label: "Siap diterbitkan", icon: "event_repeat" },
  statement_unmatched: { label: "Mutasi belum cocok", icon: "account_balance" },
  meter_flagged: { label: "Meter perlu review", icon: "electric_meter" },
};
export const FINANCE_OBJECT_LABEL: Record<string, string> = { payment: "Pembayaran", collection_log: "Log penagihan", billing_run: "Billing run", bank_statement_import: "Impor mutasi", meter_reading: "Pembacaan meter" };

/** Deep link item attention → route SPA (tautan server yang tidak cocok route web dinormalkan). */
export function attentionLink(link: string | null | undefined): string {
  return appLink(link) ?? "#";
}

/** Pintasan dashboard finance ke layar kerja utama. */
export const FINANCE_SHORTCUTS: { to: string; label: string; icon: string; perm: string }[] = [
  { to: "/billing/aging", label: "Aging piutang", icon: "hourglass_bottom", perm: "billing.invoices.view" },
  { to: "/billing/collections", label: "Penagihan", icon: "pending_actions", perm: "billing.collections.view" },
  { to: "/billing/reconciliation", label: "Rekonsiliasi", icon: "account_balance", perm: "billing.reconciliation.view" },
  { to: "/finance/budget-actual", label: "Budget vs Actual", icon: "compare_arrows", perm: "billing.budgets.view" },
  { to: "/finance/costs", label: "Operating cost", icon: "price_check", perm: "billing.costs.view" },
  { to: "/reports/aging", label: "Laporan keuangan", icon: "summarize", perm: "reports.reports.view" },
];

/** Kondisi area dari rata-rata skor inspeksi (server: 2 = Good ≥ 90, 1 = Fair 70–89, 0 = Poor < 70). */
export function areaCondition(code: number | null | undefined): { label: string; tone: Tone } {
  if (code === 2) return { label: "Baik", tone: "success" };
  if (code === 1) return { label: "Cukup", tone: "warning" };
  return { label: "Buruk", tone: "error" };
}
/** Status run route dari kode breakdown server (2 completed · 1 in_progress · 0 scheduled) → status map cleaning_route_run. */
export function runStatusFromCode(code: number | null | undefined): "completed" | "in_progress" | "scheduled" {
  return code === 2 ? "completed" : code === 1 ? "in_progress" : "scheduled";
}

/** Kategori incident (NC §15) → label; kode lain → spasi. */
export const INCIDENT_CATEGORY_LABEL: Record<string, string> = {
  unauthorized_access: "Akses tidak sah", suspicious: "Aktivitas mencurigakan", suspicious_activity: "Aktivitas mencurigakan", theft: "Pencurian", intrusion: "Penyusupan",
  property_damage: "Kerusakan properti", vandalism: "Vandalisme", lost_property: "Barang hilang", fire: "Kebakaran", fire_smoke: "Api / asap", medical: "Medis",
  accident: "Kecelakaan", flood: "Banjir", power_outage: "Listrik padam", emergency: "Emergency", parking_violation: "Pelanggaran parkir", other: "Lainnya",
};
export const incidentCategoryLabel = (code: string) => INCIDENT_CATEGORY_LABEL[code] ?? code.replace(/_/g, " ");

/** Nilai sel breakdown non-khusus. */
export function formatCell(fmt: ColFmt, v: number | null | undefined): string {
  if (fmt === "idr") return fmtMoney(v ?? 0, "IDR");
  if (fmt === "score") return v === null || v === undefined ? "—" : num1(v);
  return fmtNumber(Math.round(v ?? 0));
}
