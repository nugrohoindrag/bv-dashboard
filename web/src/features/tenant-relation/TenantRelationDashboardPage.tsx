// Tenant Relation › Overview — Tenant / Guest / Occupier Service Health (PRD P3 v2.1 §7.1 P3-TSH-01..09, Roadmap v2.1 §25.2,
// §25.8 Phase 3). Semua KPI dari GET /tenant-relation/metrics dan menautkan drill-down server (`drill_down`): Overdue = SLA breached
// → `sla_status=breached` (B-07), Resolved Today di zona waktu property (B-05), Reopened dari waktu reopen (B-06), kepatuhan SLA
// memperhitungkan waktu jeda (B-09). Breakdown kategori/tipe/kanal + tren 30 hari; Attention Required mencakup isu berulang & feedback.
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle } from "@/components/ui/primitives";
import { AsyncState, KpiSkeleton } from "@/components/bv/common";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useProfile } from "@/lib/profile";
import { fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { TRMetrics } from "./types";
import { complianceTone, drillLink, fmtDecimal, fmtHours, fmtPct, periodDelta } from "./utils";
import { BreakdownCard, KpiTile, TrendCard } from "./DashboardCharts";

const SR_LIST = "/operations/service-requests";

export default function TenantRelationDashboardPage() {
  const { propertyId } = useAuth();
  const prof = useProfile();
  const m = useQuery({ queryKey: ["tr-metrics", propertyId], queryFn: ({ signal }) => api<TRMetrics>("tenant-relation/metrics", { query: { property_id: propertyId ?? undefined }, signal }), refetchInterval: 60_000 });
  return (
    <div>
      <PageHeader
        title={prof.term("relation_module", "id")}
        subtitle={`Kesehatan layanan tenant: intake, triase, komunikasi, SLA, eskalasi, dan resolusi ${prof.term("request", "id").toLowerCase()}.`}
        actions={<Button variant="secondary" size="sm" icon="refresh" loading={m.isFetching && !m.isLoading} onClick={() => m.refetch()}>Muat ulang</Button>}
      />
      <AsyncState query={m} skeleton={<KpiSkeleton count={6} />}>
        {(x) => <Dashboard x={x} />}
      </AsyncState>
    </div>
  );
}

function Dashboard({ x }: { x: TRMetrics }) {
  const dd = x.drill_down;
  const link = (key: string, fallback = SR_LIST) => drillLink(dd, key, fallback);
  const reqDelta = periodDelta(x.requests_30d, x.requests_prev_30d, null);
  const cmpDelta = periodDelta(x.complaints_30d, x.complaints_prev_30d, false);
  const compTone = complianceTone(x.sla_compliance_pct);
  return (
    <div className="space-y-5">
      <section aria-labelledby="kpi-now">
        <h2 id="kpi-now" className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Saat ini</h2>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-6">
          <KpiTile testId="kpi-open" label="Open Tenant Tickets" value={fmtNumber(x.open_tickets)} hint="Permintaan yang belum ditutup atau dibatalkan." to={link("open_tickets")} />
          <KpiTile testId="kpi-sla-risk" label="SLA Risk" value={fmtNumber(x.sla_risk)} hint="Mendekati batas SLA dan belum selesai." to={link("sla_risk")} tone={x.sla_risk > 0 ? "warning" : null} statusLabel="Berisiko" />
          <KpiTile testId="kpi-overdue" label="Overdue" value={fmtNumber(x.overdue)} hint="Belum selesai dan sudah melewati batas SLA penyelesaian." to={link("overdue", `${SR_LIST}?sla_status=breached`)} tone={x.overdue > 0 ? "error" : null} statusLabel="Melewati SLA" />
          <KpiTile testId="kpi-resolved-today" label="Resolved Today" value={fmtNumber(x.resolved_today)} hint={`Selesai hari ini (zona waktu ${x.timezone}).`} to={link("resolved_today")} />
          <KpiTile testId="kpi-recurring" label="Isu berulang terbuka" value={fmtNumber(x.recurring_issues_open)} hint="Lokasi + kategori yang dilaporkan berulang di atas ambang (Terbuka / Ditangani)." to={link("recurring_issues_open", "/tenant-relation/recurring-issues")} tone={x.recurring_issues_open > 0 ? "error" : null} statusLabel="Perlu tindakan" />
          <KpiTile testId="kpi-feedback" label="Feedback umum baru" value={fmtNumber(x.general_feedback_new)} hint="Saran/keluhan umum tenant berstatus Baru atau Ditinjau." to={link("general_feedback_new", "/tenant-relation/feedback/general")} tone={x.general_feedback_new > 0 ? "info" : null} statusLabel="Perlu ditanggapi" />
        </div>
      </section>
      <section aria-labelledby="kpi-period">
        <h2 id="kpi-period" className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Kinerja periode</h2>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-6">
          <KpiTile testId="kpi-response" label="Rata-rata penyelesaian" value={fmtHours(x.avg_resolution_hours)} sub={`respons ${fmtHours(x.avg_response_hours)}`} hint="Dibuat → selesai; respons = dibuat → diterima/ditriase (30 hari terakhir)." to={link("avg_resolution_hours", "/reports/service-requests")} />
          <KpiTile testId="kpi-sla-compliance" label="Kepatuhan SLA (30 hari)" value={fmtPct(x.sla_compliance_pct)} hint="Selesai ≤ batas SLA + waktu jeda (menunggu tenant)." to={link("sla_compliance_pct", "/reports/sla")} tone={compTone === "warning" || compTone === "error" ? compTone : null} statusLabel="Di bawah 90%" />
          <KpiTile testId="kpi-csat" label="CSAT (90 hari)" value={x.csat != null ? fmtDecimal(x.csat, 2) : "—"} sub={`${fmtNumber(x.csat_count)} feedback`} hint="Rata-rata rating 1–5 setelah permintaan ditutup." to={link("csat", "/tenant-relation/feedback")} tone={x.csat != null && x.csat < 4.2 ? "warning" : null} statusLabel="Di bawah target 4,2" />
          <KpiTile testId="kpi-reopened" label="Reopened (30 hari)" value={fmtNumber(x.reopened_30d)} sub={x.reopen_rate_pct != null ? `reopen rate ${fmtPct(x.reopen_rate_pct)}` : undefined} hint="Dibuka kembali tenant dalam 30 hari (dari waktu reopen)." to={link("reopened_30d")} tone={x.reopened_30d > 0 ? "warning" : null} statusLabel="Perlu ditinjau" />
          <KpiTile testId="kpi-requests" label="Permintaan (30 hari)" value={fmtNumber(x.requests_30d)} delta={{ ...reqDelta, period: `vs 30 hari sebelumnya (${fmtNumber(x.requests_prev_30d)})` }} hint="Seluruh kanal: Tenant App, staf, WhatsApp, telepon, dll." to={link("requests_30d")} />
          <KpiTile testId="kpi-complaints" label="Keluhan (30 hari)" value={fmtNumber(x.complaints_30d)} delta={{ ...cmpDelta, period: `vs 30 hari sebelumnya (${fmtNumber(x.complaints_prev_30d)})` }} hint="Permintaan bertipe Keluhan." to={link("complaints_30d", `${SR_LIST}?request_type=complaint`)} />
        </div>
      </section>

      <TrendCard series={x.series} timezone={x.timezone} />

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-3">
        <BreakdownCard title="Per kategori" subtitle="30 hari terakhir · 12 teratas" rows={x.by_category} />
        <BreakdownCard title="Per tipe permintaan" subtitle="30 hari terakhir" rows={x.by_type} />
        <BreakdownCard title="Per kanal" subtitle="30 hari terakhir" rows={x.by_channel} />
      </div>

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
        <AttentionCard x={x} className="lg:col-span-7" />
        <Card className="lg:col-span-5">
          <CardHeader><CardTitle>Tenant App</CardTitle></CardHeader>
          <CardContent className="space-y-3 text-sm">
            <Link to={link("tenant_app_tickets_30d")} className="flex items-center justify-between rounded-[var(--radius-md)] hover:underline"><span>Ticket via Tenant App (30 hari)</span><span className="font-semibold tnum">{fmtNumber(x.tenant_app_tickets_30d)}</span></Link>
            <Link to={link("pending_accounts", "/tenant-relation/tenant-users?status=pending_validation")} className="flex items-center justify-between hover:underline"><span>Akun menunggu validasi</span><span className="font-semibold tnum">{fmtNumber(x.pending_accounts)}</span></Link>
            <div className="flex flex-wrap gap-2 pt-2">
              <Link to="/tenant-relation/tenant-users"><Button size="sm" variant="secondary">Tenant Users</Button></Link>
              <Link to="/tenant-relation/feedback"><Button size="sm" variant="secondary">Feedback / CSAT</Button></Link>
              <Link to="/tenant-relation/announcements"><Button size="sm" variant="secondary">Announcements</Button></Link>
              <Link to="/tenant-relation/recurring-issues"><Button size="sm" variant="secondary">Isu Berulang</Button></Link>
              <Link to="/tenant-relation/service-requests"><Button size="sm">Service Requests</Button></Link>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

interface AttentionRow { key: string; count: number; title: string; body?: string; to: string; tone: "critical" | "warning" | "info"; icon: string }

function AttentionCard({ x, className }: { x: TRMetrics; className?: string }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const dd = x.drill_down;
  const rows: AttentionRow[] = [
    { key: "overdue", count: x.overdue, title: `${fmtNumber(x.overdue)} ticket melewati SLA`, to: drillLink(dd, "overdue", `${SR_LIST}?sla_status=breached`), tone: "critical" as const, icon: "alarm" },
    { key: "recurring", count: x.recurring_issues_open, title: `${fmtNumber(x.recurring_issues_open)} isu berulang terbuka`, body: "Lokasi & kategori yang sama dilaporkan berulang kali — keluhan berulang otomatis naik prioritas.", to: drillLink(dd, "recurring_issues_open", "/tenant-relation/recurring-issues"), tone: "critical" as const, icon: "event_repeat" },
    { key: "sla_risk", count: x.sla_risk, title: `${fmtNumber(x.sla_risk)} ticket berisiko SLA`, to: drillLink(dd, "sla_risk", `${SR_LIST}?sla_status=at_risk`), tone: "warning" as const, icon: "schedule" },
    ...(can("tenant_relation.tenant_users.view") ? [{ key: "pending", count: x.pending_accounts, title: `${fmtNumber(x.pending_accounts)} pendaftaran Tenant App menunggu validasi`, body: "Tenant belum dapat login sampai akun disetujui.", to: drillLink(dd, "pending_accounts", "/tenant-relation/tenant-users?status=pending_validation"), tone: "warning" as const, icon: "how_to_reg" }] : []),
    { key: "unread", count: x.waiting_for_staff, title: `${fmtNumber(x.waiting_for_staff)} ticket memiliki pesan tenant yang belum dibaca`, body: 'Balas melalui panel "Pesan ke Tenant" di detail ticket.', to: `${SR_LIST}?open=true`, tone: "info" as const, icon: "forum" },
    { key: "waiting", count: x.waiting_for_tenant, title: `${fmtNumber(x.waiting_for_tenant)} ticket menunggu respons tenant`, body: 'Status tenant-facing: "Need Your Response".', to: drillLink(dd, "waiting_for_tenant", `${SR_LIST}?status=waiting_for_tenant`), tone: "info" as const, icon: "hourglass_top" },
    ...(can("tenant_relation.feedback.view") ? [{ key: "feedback", count: x.general_feedback_new, title: `${fmtNumber(x.general_feedback_new)} feedback umum belum ditanggapi`, to: drillLink(dd, "general_feedback_new", "/tenant-relation/feedback/general"), tone: "info" as const, icon: "reviews" }] : []),
  ].filter((r) => r.count > 0);
  const TONE: Record<AttentionRow["tone"], string> = { critical: "bg-critical-soft text-critical-text", warning: "bg-warning-soft text-warning-text", info: "bg-info-soft text-info-text" };
  return (
    <Card className={className}>
      <CardHeader><CardTitle>Attention Required</CardTitle></CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("empty.attention")}</p>
        ) : (
          <ul className="space-y-2">
            {rows.map((r) => (
              <li key={r.key}>
                <Link to={r.to} className={cn("flex items-start gap-3 rounded-[var(--radius-lg)] px-3 py-2.5 text-sm transition-opacity hover:opacity-90", TONE[r.tone])}>
                  <Icon name={r.icon} size={18} className="mt-0.5 shrink-0" aria-hidden />
                  <span className="min-w-0 flex-1">
                    <span className="block font-bold">{r.title}</span>
                    {r.body && <span className="block">{r.body}</span>}
                  </span>
                  <span className="inline-flex shrink-0 items-center text-xs font-semibold underline">Lihat<Icon name="chevron_right" size={14} aria-hidden /></span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
