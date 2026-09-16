// Tenant Relation › Overview (PRD P1 v1.3 §28 Building Management Tenant Dashboard): Open Tenant Tickets, SLA Risk, Overdue,
// Resolved Today, Reopened, CSAT; Attention Required: akun menunggu validasi, pesan tenant belum dibaca, ticket menunggu tenant.
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { MetricCard } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Button, Card, CardContent, CardHeader, CardTitle } from "@/components/ui/primitives";
import { AsyncState } from "@/components/bv/common";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useProfile } from "@/lib/profile";

interface Metrics {
  open_tickets: number; sla_risk: number; overdue: number; resolved_today: number; reopened_30d: number; waiting_for_tenant: number;
  waiting_for_staff: number; pending_accounts: number; csat: number | null; csat_count: number; reopen_rate_pct: number | null; tenant_app_tickets_30d: number;
}

export default function TenantRelationDashboardPage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const prof = useProfile();
  const m = useQuery({ queryKey: ["tr-metrics", propertyId], queryFn: ({ signal }) => api<Metrics>("tenant-relation/metrics", { query: { property_id: propertyId ?? undefined }, signal }), refetchInterval: 60_000 });
  const srBase = "/tenant-relation/service-requests";
  return (
    <div>
      <PageHeader title={prof.term("relation_module", "id")} subtitle={`Intake, triage, komunikasi, SLA, eskalasi, dan resolusi ${prof.term("request", "id").toLowerCase()}.`} />
      <AsyncState query={m}>
        {(x) => (
          <div className="space-y-5">
            <div className="grid grid-cols-6 gap-4">
              <MetricCard label="Open Tenant Tickets" value={String(x.open_tickets)} tone="primary" />
              <MetricCard label="SLA Risk" value={String(x.sla_risk)} tone={x.sla_risk > 0 ? "warning" : "neutral"} />
              <MetricCard label="Overdue" value={String(x.overdue)} tone={x.overdue > 0 ? "error" : "neutral"} />
              <MetricCard label="Resolved Today" value={String(x.resolved_today)} tone="success" />
              <MetricCard label="Reopened (30 hari)" value={String(x.reopened_30d)} subValue={x.reopen_rate_pct != null ? `reopen rate ${x.reopen_rate_pct.toFixed(1)}%` : undefined} tone={x.reopened_30d > 0 ? "warning" : "neutral"} />
              <MetricCard label="CSAT (90 hari)" value={x.csat != null ? x.csat.toFixed(2) : "—"} subValue={`${x.csat_count} feedback`} tone={x.csat != null && x.csat >= 4.2 ? "success" : x.csat != null ? "warning" : "neutral"} />
            </div>
            <div className="grid grid-cols-12 gap-5">
              <Card className="col-span-7">
                <CardHeader><CardTitle>Attention Required</CardTitle></CardHeader>
                <CardContent className="space-y-2">
                  {x.pending_accounts > 0 && can("tenant_relation.tenant_users.view") && (
                    <Alert variant="warning" title={`${x.pending_accounts} pendaftaran Tenant App menunggu validasi`} action={<Button size="sm" variant="secondary" onClick={() => (window.location.href = "/tenant-relation/tenant-users")}>Validasi</Button>}>Tenant belum dapat login sampai akun disetujui.</Alert>
                  )}
                  {x.waiting_for_staff > 0 && <Alert variant="info" title={`${x.waiting_for_staff} ticket memiliki pesan tenant yang belum dibaca`} action={<Link to={`${srBase}?open=true`} className="text-sm font-semibold underline">Lihat</Link>}>Balas melalui panel "Pesan ke Tenant" di detail ticket.</Alert>}
                  {x.waiting_for_tenant > 0 && <Alert variant="info" title={`${x.waiting_for_tenant} ticket menunggu respons tenant`} action={<Link to={`${srBase}?status=waiting_for_tenant`} className="text-sm font-semibold underline">Lihat</Link>}>Status tenant-facing: "Need Your Response".</Alert>}
                  {x.overdue > 0 && <Alert variant="critical" title={`${x.overdue} ticket melewati SLA`} action={<Link to={`${srBase}?sla_risk=true`} className="text-sm font-semibold underline">Lihat</Link>} />}
                  {x.sla_risk > 0 && <Alert variant="warning" title={`${x.sla_risk} ticket berisiko SLA`} action={<Link to={`${srBase}?sla_risk=true`} className="text-sm font-semibold underline">Lihat</Link>} />}
                  {x.pending_accounts + x.waiting_for_staff + x.waiting_for_tenant + x.overdue + x.sla_risk === 0 && <p className="text-sm text-muted-foreground">{t("empty.attention")}</p>}
                </CardContent>
              </Card>
              <Card className="col-span-5">
                <CardHeader><CardTitle>Tenant App</CardTitle></CardHeader>
                <CardContent className="space-y-3 text-sm">
                  <div className="flex items-center justify-between"><span>Ticket via Tenant App (30 hari)</span><span className="tnum font-semibold">{x.tenant_app_tickets_30d}</span></div>
                  <div className="flex items-center justify-between"><span>Akun menunggu validasi</span><span className="tnum font-semibold">{x.pending_accounts}</span></div>
                  <div className="flex flex-wrap gap-2 pt-2">
                    <Link to="/tenant-relation/tenant-users"><Button size="sm" variant="secondary">Tenant Users</Button></Link>
                    <Link to="/tenant-relation/feedback"><Button size="sm" variant="secondary">Feedback / CSAT</Button></Link>
                    <Link to="/tenant-relation/announcements"><Button size="sm" variant="secondary">Announcements</Button></Link>
                    <Link to={srBase}><Button size="sm">Service Requests</Button></Link>
                  </div>
                </CardContent>
              </Card>
            </div>
          </div>
        )}
      </AsyncState>
    </div>
  );
}
