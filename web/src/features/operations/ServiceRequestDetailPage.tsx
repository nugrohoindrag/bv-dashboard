// Detail Service Request (PRD §18.3): header + aksi (acknowledge/assign/start/wait_tenant/resolve/close), tab Detail · Evidence · Activity · Terkait,
// panel kanan: tenant/pemohon, SLA response/resolution, assignee. "Buat Task/Work Order dari SR" → POST /service-requests/{id}/tasks |
// /work-orders (PRD P1 v2 §30), tampil sesuai allowed_actions create_task / create_work_order.
// PRD P3 v2.1: tab Komunikasi (P3-TRC-03 log in-app/push/pesan/WhatsApp manual), tombol WhatsApp manual (P3-WAM-01),
// lampiran foto di pesan tenant (P3-SRQ-05). Tab dapat dibuka langsung lewat `?tab=communications|messages|…`.
import { useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { FlagBadges, PriorityBadge, SLAStatusBadge, StatusBadge, requestTypeLabel } from "@/components/bv/badges";
import { AsyncState, DetailSkeleton, KeyValue, LocationPath, RelativeTime, SLAProgress } from "@/components/bv/common";
import { ActivityTimeline } from "@/components/bv/timeline";
import { PhotoEvidenceUploader } from "@/components/bv/checklist";
import { useActivities, useAttachments, useComments, useOne } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import type { ServiceRequest } from "@/api/types";
import { AssignDialog, TransitionActions, WorkItemFromSRDialog } from "./dialogs";
import { CommentsPanel, EvidenceSections, RelatedList } from "./WorkItemDetailPage";
import { TenantMessagesPanel } from "@/features/tenant-relation/TenantMessagesPanel";
import { CommunicationsPanel } from "@/features/tenant-relation/CommunicationsPanel";
import { WhatsAppButton } from "@/components/bv/WhatsAppButton";
import { ServiceRequestChainCard } from "./ChainTimeline";

export default function ServiceRequestDetailPage() {
  const { id } = useParams();
  const [sp] = useSearchParams();
  const qc = useQueryClient();
  const { t } = useTranslation();
  const { can } = useAuth();
  const sr = useOne<ServiceRequest>("service-requests", id);
  const activities = useActivities("service_request", id);
  const comments = useComments("service-requests", id);
  const attachments = useAttachments("service_request", id);
  const [assignOpen, setAssignOpen] = useState(false);
  const [fromSR, setFromSR] = useState<"task" | "work_order" | null>(null);
  const attById = useMemo(() => Object.fromEntries((attachments.data ?? []).map((a) => [a.id, a])), [attachments.data]);
  return (
    <AsyncState query={sr} skeleton={<DetailSkeleton />}>
      {(s) => (
        <div>
          <PageHeader
            breadcrumb={<><Link to="/operations/service-requests" className="hover:underline">{t("nav.service_requests")}</Link> / <span className="font-mono">{s.request_number}</span></>}
            title={<span><span className="font-mono">{s.request_number}</span> <span className="font-normal text-muted-foreground">·</span> {s.title}</span>}
            badges={<><StatusBadge objectType="service_request" status={s.status} /><FlagBadges item={s} /><PriorityBadge priority={s.priority} /><SLAStatusBadge status={s.sla_status} /></>}
            subtitle={<>{s.request_type ? `${requestTypeLabel[s.request_type] ?? s.request_type} · ` : ""}{s.category_name ?? s.category_code} · via {s.channel} · {t("label.created")} <RelativeTime value={s.created_at} /> oleh {s.created_by_name ?? "tenant"}</>}
            actions={
              <>
                <WhatsAppButton context="service_request" objectType="service_request" objectId={s.id} label="WhatsApp" onSent={() => qc.invalidateQueries({ queryKey: ["sr-communications", s.id] })} />
                {s.allowed_actions.includes("create_task") && <Button variant="secondary" size="sm" icon="task_alt" onClick={() => setFromSR("task")}>{t("action.create_task")}</Button>}
                {s.allowed_actions.includes("create_work_order") && <Button variant="secondary" size="sm" icon="construction" onClick={() => setFromSR("work_order")}>{t("action.create_work_order")}</Button>}
                <span className="hidden md:contents"><TransitionActions objectType="service_request" item={s} entityLabel={s.request_number} onAssign={() => setAssignOpen(true)} /></span>
              </>
            }
          />
          <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
            <div className="min-w-0 lg:col-span-8">
              <Tabs defaultValue={sp.get("tab") ?? "detail"}>
                <TabsList>
                  <TabsTrigger value="detail">{t("label.detail")}</TabsTrigger>
                  <TabsTrigger value="evidence">{t("label.evidence")} ({s.attachment_count})</TabsTrigger>
                  <TabsTrigger value="activity">{t("label.activity")}</TabsTrigger>
                  <TabsTrigger value="related">{t("label.related")} ({s.links.length})</TabsTrigger>
                  {can("tenant_relation.messages.view") && <TabsTrigger value="messages" badge={s.unread_tenant_messages || undefined}>Pesan Tenant{s.message_count ? ` (${s.message_count})` : ""}</TabsTrigger>}
                  <TabsTrigger value="communications">Komunikasi</TabsTrigger>
                </TabsList>
                <TabsContent value="detail" className="space-y-4 pt-4">
                  <Card><CardContent className="pt-4">
                    <p className="whitespace-pre-line text-body">{s.description || <span className="italic text-muted-foreground">Tanpa deskripsi.</span>}</p>
                    {s.resolution && <div className="mt-4"><div className="text-xs font-semibold uppercase text-muted-foreground">{t("label.resolution")}</div><p className="whitespace-pre-line text-body">{s.resolution}</p></div>}
                  </CardContent></Card>
                  {/* PRD P1 v2.1 P1-XMW-04: timeline lintas tim (Task → Finding → WO → tindak lanjut) */}
                  <ServiceRequestChainCard srId={s.id} />
                  <CommentsPanel resource="service-requests" id={s.id} comments={comments.data ?? []} />
                </TabsContent>
                <TabsContent value="evidence" className="space-y-4 pt-4">
                  {!["closed", "cancelled"].includes(s.status) && <PhotoEvidenceUploader objectType="service_request" objectId={s.id} attachmentType="photo" />}
                  <EvidenceSections items={attachments.data ?? []} />
                </TabsContent>
                <TabsContent value="activity" className="pt-4"><AsyncState query={activities}>{(acts) => <ActivityTimeline items={acts} objectLabel="Service Request" attachmentsById={attById} />}</AsyncState></TabsContent>
                <TabsContent value="related" className="pt-4"><RelatedList links={s.links} /></TabsContent>
                {can("tenant_relation.messages.view") && <TabsContent value="messages" className="pt-4"><TenantMessagesPanel srId={s.id} terminal={["closed", "cancelled"].includes(s.status)} channel={s.channel} /></TabsContent>}
                <TabsContent value="communications" className="pt-4"><CommunicationsPanel srId={s.id} /></TabsContent>
              </Tabs>
            </div>
            <div className="min-w-0 space-y-4 lg:col-span-4">
              <Card><CardHeader><CardTitle>{t("label.tenant")} / Pemohon</CardTitle></CardHeader><CardContent>
                <KeyValue items={[
                  ...(s.request_type ? [{ label: t("label.request_type"), value: requestTypeLabel[s.request_type] ?? s.request_type }] : []),
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
              <Card><CardHeader><CardTitle>{t("label.sla")}</CardTitle><SLAStatusBadge status={s.sla?.status ?? s.sla_status} /></CardHeader><CardContent className="space-y-3">
                <SLAProgress sla={s.sla} />
                <KeyValue items={[
                  { label: "Response due", value: <>{fmtDateTime(s.sla?.response_due_at)}{s.sla?.response_status === "breached" && <span className="ml-1 text-xs font-semibold text-critical-text">terlambat</span>}{s.sla?.response_status === "met" && <span className="ml-1 text-xs text-success-text">tepat waktu</span>}</> },
                  { label: "Acknowledged", value: fmtDateTime(s.acknowledged_at) },
                  { label: "Resolution due", value: fmtDateTime(s.sla?.resolution_due_at) },
                  { label: "Resolved", value: fmtDateTime(s.resolved_at) },
                  { label: "Closed", value: fmtDateTime(s.closed_at) },
                ]} />
              </CardContent></Card>
            </div>
          </div>
          <div className="md:hidden"><TransitionActions objectType="service_request" item={s} entityLabel={s.request_number} mobileBar onAssign={() => setAssignOpen(true)} /></div>
          <AssignDialog objectType="service_request" id={s.id} open={assignOpen} onOpenChange={setAssignOpen} current={s.assignee} />
          {fromSR && <WorkItemFromSRDialog srId={s.id} kind={fromSR} defaults={{ title: s.title, priority: s.priority }} onClose={() => setFromSR(null)} />}
        </div>
      )}
    </AsyncState>
  );
}
