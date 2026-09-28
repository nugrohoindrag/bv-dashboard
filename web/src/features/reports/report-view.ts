// Helper murni halaman Reports: rentang awal dari URL (drill-down dashboard domain PRD P2 v2.1: /reports/{name}?from&to),
// label ramah kunci ringkasan/breakdown (termasuk laporan baru `security` P2-SDB-03, `team-performance` P2-WKL-04, dan laporan
// keuangan PRD P4 v2.1 P4-FIN-03), dan tebakan format untuk laporan tanpa definisi presentasi.

export type ReportFmt = "int" | "pct" | "money" | "hours" | "rating" | "days" | "minutes" | "score" | "signed_money" | "year";

const DAY_RE = /^\d{4}-\d{2}-\d{2}$/;
const isoDay = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

/** Rentang awal: ?from&to valid dari URL atau 30 hari terakhir (termasuk hari ini). */
export function initialRange(from: string | null, to: string | null, today: Date = new Date()): { from: string; to: string } {
  if (from && to && DAY_RE.test(from) && DAY_RE.test(to) && from <= to) return { from, to };
  const t = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  const f = new Date(t);
  f.setDate(t.getDate() - 29);
  return { from: isoDay(f), to: isoDay(t) };
}

const SUMMARY_LABEL: Record<string, string> = {
  // Security Report (P2-SDB-03)
  patrol_total: "Patrol terjadwal", patrol_completed: "Patrol selesai", patrol_completion_pct: "Patrol completion", checkpoints_total: "Checkpoint", checkpoints_scanned: "Checkpoint discan",
  checkpoints_missed: "Checkpoint terlewat", checkpoint_compliance_pct: "Checkpoint compliance", incidents_reported: "Incident dilaporkan", incidents_critical: "Incident kritis",
  incidents_resolved: "Incident selesai", unauthorized_entry: "Unauthorized entry", avg_response_minutes: "Rata-rata respons", emergencies_raised: "Emergency", emergencies_resolved: "Emergency selesai",
  emergencies_false_alarm: "Alarm palsu", emergencies_escalated: "Emergency dieskalasi", avg_emergency_ack_minutes: "Rata-rata respons emergency", visitors_checked_in: "Tamu check-in",
  parking_violations: "Pelanggaran parkir", missed_checkpoint: "Checkpoint terlewat", incident_category: "Incident per kategori", emergency_type: "Emergency per tipe", route: "Per rute patrol",
  // Team Performance (P2-WKL-04)
  completed: "Selesai", completed_on_time: "Selesai tepat waktu", on_time_pct: "Tepat waktu", open_now: "Terbuka kini", overdue_now: "Overdue kini", rework: "Rework",
  sla_breached: "SLA breach", avg_completion_hours: "Durasi rata-rata", missed_checkpoints: "Checkpoint terlewat", avg_inspection_score: "Skor inspeksi", team: "Per team", staff: "Per staf",
  // Laporan keuangan (P4-FIN-03)
  d1_30: "1–30 hari", d31_60: "31–60 hari", d61_90: "61–90 hari", d90_plus: "> 90 hari", due_amount: "Tagihan jatuh tempo", collected_on_due: "Terbayar atas jatuh tempo",
  cash_collected: "Kas diterima", billed_revenue: "Pendapatan ditagihkan", net_revenue: "Pendapatan bersih", credit_notes: "Credit note", fiscal_year: "Tahun anggaran",
  total_cost: "Total biaya", ipl_billed: "IPL ditagihkan", ipl_collected: "IPL terbayar", ipl_outstanding: "Tunggakan IPL", receipts: "Penerimaan", usage: "Penggunaan",
};

/** Label ramah kunci laporan; kunci tak dikenal → kalimat dari snake_case ("_pct" → " (%)"). */
export function summaryLabel(key: string): string {
  return SUMMARY_LABEL[key] ?? key.replace(/_pct$/, " (%)").replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());
}

/** Format dari akhiran kunci server. */
export function guessFmt(key: string): ReportFmt {
  if (key === "fiscal_year" || key === "year") return "year";
  if (key.endsWith("_pct")) return "pct";
  if (key.endsWith("_minutes")) return "minutes";
  if (key.endsWith("_hours")) return "hours";
  if (key.endsWith("_days")) return "days";
  if (/(cost|amount|revenue|budget|actual|outstanding|balance|billed|collected|receipts)/.test(key) && !/(count|invoices|units)/.test(key)) return "money";
  if (key.endsWith("_score")) return "score";
  return "int";
}
