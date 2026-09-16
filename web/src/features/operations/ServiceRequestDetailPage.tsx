// Detail Service Request (PRD §18.3): header + aksi (acknowledge/assign/start/wait_tenant/resolve/close), tab Detail · Evidence · Activity · Terkait,
// panel kanan: tenant/pemohon, SLA response/resolution, assignee. "Buat Work Order dari SR" → POST /service-requests/{id}/work-orders.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { FlagBadges, PriorityBadge, StatusBadge } from "@/components/bv/badges";
import { AsyncState, DetailSkeleton, KeyValue, LocationPath, RelativeTime, SLAProgress } from "@/components/bv/common";
import { ActivityTimeline } from "@/components/bv/timeline";
import { PhotoEvidenceUploader } from "@/components/bv/checklist";
import { useActivities, useAttachments, useComments, useOne } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import type { ServiceRequest } from "@/api/types";
import { AssignDialog, CreateWorkItemDialog, TransitionActions } from "./dialogs";
import { CommentsPanel, EvidenceSections, RelatedList } from "./WorkItemDetailPage";
import { TenantMessagesPanel } from "@/features/tenant-relation/TenantMessagesPanel";

export default function ServiceRequestDetailPage() {
  const { id } = useParams();
  const { t } = useTranslation();
  const { can } = useAuth();
  const nav = useNavigate();
  const sr = useOne<ServiceRequest>("service-requests", id);
  const activities = useActivities("service_request", id);
  const comments = useComments("service-requests", id);
  const attachments = useAttachments("service_request", id);
  const [assignOpen, setAssignOpen] = useState(false);
  const [woOpen, setWoOpen] = useState(false);
  const attById = useMemo(() => Object.fromEntries((attachments.data ?? []).map((a) => [a.id, a])), [attachments.data]);
  return (
    <AsyncState query={sr} skeleton={<DetailSkeleton />}>
      {(s) => (
        <div>
          <PageHeader
            breadcrumb={<><Link to="/operations/service-requests" className="hover:underline">{t("nav.service_requests")}</Link> / <span className="font-mono">{s.request_number}</span></>}
            title={<span><span className="font-mono">{s.request_number}</span> <span className="font-normal text-muted-foreground">·</span> {s.title}</span>}
            badges={<><StatusBadge objectType="service_request" status={s.status} /><FlagBadges flags={s.flags} /><PriorityBadge priority={s.priority} /></>}
            subtitle={<>{s.category_name ?? s.category_code} · via {s.channel} · {t("label.created")} <RelativeTime value={s.created_at} /> oleh {s.created_by_name ?? "tenant"}</>}
            actions={
              <>
                {can("operations.work_orders.create") && !["closed", "cancelled"].includes(s.status) && <Button variant="secondary" size="sm" onClick={() => setWoOpen(true)}>{t("action.create_work_order")}</Button>}
                <TransitionActions objectType="service_request" item={s} onAssign={() => setAssignOpen(true)} />
              </>
            }
          />
          <div className="grid grid-cols-12 gap-5">
            <div className="col-span-8">
              <Tabs defaultValue="detail">
                <TabsList>
                  <TabsTrigger value="detail">{t("label.detail")}</TabsTrigger>
                  <TabsTrigger value="evidence">{t("label.evidence")} ({s.attachment_count})</TabsTrigger>
                  <TabsTrigger value="activity">{t("label.activity")}</TabsTrigger>
                  <TabsTrigger value="related">{t("label.related")} ({s.links.length})</TabsTrigger>
                  {can("tenant_relation.messages.view") && <TabsTrigger value="messages" badge={s.unread_tenant_messages || undefined}>Pesan Tenant{s.message_count ? ` (${s.message_count})` : ""}</TabsTrigger>}
                </TabsList>
                <TabsContent value="detail" className="space-y-4 pt-4">
                  <Card><CardContent className="pt-4">
                    <p className="whitespace-pre-line text-body">{s.description || <span className="italic text-muted-foreground">Tanpa deskripsi.</span>}</p>
                    {s.resolution && <div className="mt-4"><div className="text-xs font-semibold uppercase text-muted-foreground">{t("label.resolution")}</div><p className="whitespace-pre-line text-body">{s.resolution}</p></div>}
                  </CardContent></Card>
                  <CommentsPanel resource="service-requests" id={s.id} comments={comments.data ?? []} />
                </TabsContent>
                <TabsContent value="evidence" className="space-y-4 pt-4">
                  {!["closed", "cancelled"].includes(s.status) && <PhotoEvidenceUploader objectType="service_request" objectId={s.id} attachmentType="photo" />}
                  <EvidenceSections items={attachments.data ?? []} />
                </TabsContent>
                <TabsContent value="activity" className="pt-4"><AsyncState query={activities}>{(acts) => <ActivityTimeline items={acts} objectLabel="Service Request" attachmentsById={attById} />}</AsyncState></TabsContent>
                <TabsContent value="related" className="pt-4"><RelatedList links={s.links} /></TabsContent>
                {can("tenant_relation.messages.view") && <TabsContent value="messages" className="pt-4"><TenantMessagesPanel srId={s.id} terminal={["closed", "cancelled"].includes(s.status)} channel={s.channel} /></TabsContent>}
              </Tabs>
            </div>
            <div className="col-span-4 space-y-4">
              <Card><CardHeader><CardTitle>{t("label.tenant")} / Pemohon</CardTitle></CardHeader><CardContent>
                <KeyValue items={[
                  { label: t("label.tenant"), value: s.tenant_id ? <Link to={`/tenant/tenants/${s.tenant_id}`} className="text-brand-600 hover:underline">{s.tenant_name}</Link> : "—" },
                  { label: "Pemohon", value: <>{s.requester_name ?? "—"}{s.channel === "tenant_app" && <span className="ml-1 rounded bg-primary-soft px-1.5 text-[10px] font-semibold text-primary">Tenant App</span>}</> },
                  ...(s.area_scope ? [{ label: "Lingkup", value: s.area_scope === "unit" ? "Unit tenant" : s.area_scope === "common_area" ? "Common area" : "Area lain" }] : []),
                  ...(s.reopen_count ? [{ label: "Reopen", value: `${s.reopen_count}×` }] : []),
                  ...(s.feedback ? [{ label: "CSAT", value: `${s.feedback.rating}/5${s.feedback.comment ? ` — ${s.feedback.comment}` : ""}` }] : []),
                  { label: "Telepon", value: s.requester_phone ?? "—" },
                  { label: t("label.location"), value: <LocationPath pathText={s.location.path_text} locationId={s.location.id} linkTo={(lid) => `/property/locations/${lid}`} /> },
                ]} />
              </CardContent></Card>
              <Card><CardHeader><CardTitle>Penugasan</CardTitle>{s.allowed_actions.includes("assign") && <Button variant="link" size="sm" onClick={() => setAssignOpen(true)}>{t("action.assign")}</Button>}</CardHeader><CardContent>
                <KeyValue items={[{ label: t("label.team"), value: s.assignee.team_name }, { label: t("label.assignee"), value: s.assignee.user_name ?? <em className="text-muted-foreground">belum ditugaskan</em> }]} />
              </CardContent></Card>
              <Card><CardHeader><CardTitle>{t("label.sla")}</CardTitle></CardHeader><CardContent className="space-y-3">
                <SLAProgress sla={s.sla} />
                <KeyValue items={[
                  { label: "Response due", value: fmtDateTime(s.sla?.response_due_at) },
                  { label: "Acknowledged", value: fmtDateTime(s.acknowledged_at) },
                  { label: "Resolution due", value: fmtDateTime(s.sla?.resolution_due_at) },
                  { label: "Resolved", value: fmtDateTime(s.resolved_at) },
                  { label: "Closed", value: fmtDateTime(s.closed_at) },
                ]} />
              </CardContent></Card>
            </div>
          </div>
          <AssignDialog objectType="service_request" id={s.id} open={assignOpen} onOpenChange={setAssignOpen} current={s.assignee} />
          <CreateWorkItemDialog objectType="work_order" open={woOpen} onOpenChange={setWoOpen} defaults={{ title: s.title, location_id: s.location.id, priority: s.priority, type: "service", source_type: "service_request", source_id: s.id, link_to: { object_type: "service_request", object_id: s.id, link_type: "generated_from" } }} onCreated={(x) => nav(`/operations/work-orders/${x.id}`)} />
        </div>
      )}
    </AsyncState>
  );
}
