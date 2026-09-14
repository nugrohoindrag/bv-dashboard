// Detail Incident (PRD §14.3): aksi start/resolve/close/reopen/cancel; Buat Work Order dari Incident; evidence & timeline.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { FlagBadges, PriorityBadge, SeverityBadge, StatusBadge, objectTypeLabel } from "@/components/bv/badges";
import { AsyncState, DetailSkeleton, KeyValue, LocationPath, RelativeTime } from "@/components/bv/common";
import { ActivityTimeline } from "@/components/bv/timeline";
import { PhotoEvidenceUploader } from "@/components/bv/checklist";
import { itemLink } from "@/components/bv/cards";
import { useActivities, useAttachments, useComments, useOne } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import type { Incident } from "@/api/types";
import { AssignDialog, CreateWorkItemDialog, TransitionActions } from "./dialogs";
import { CommentsPanel, EvidenceSections, RelatedList } from "./WorkItemDetailPage";

export default function IncidentDetailPage() {
  const { id } = useParams();
  const { t } = useTranslation();
  const { can } = useAuth();
  const nav = useNavigate();
  const inc = useOne<Incident>("incidents", id);
  const activities = useActivities("incident", id);
  const comments = useComments("incidents", id);
  const attachments = useAttachments("incident", id);
  const [assignOpen, setAssignOpen] = useState(false);
  const [woOpen, setWoOpen] = useState(false);
  const attById = useMemo(() => Object.fromEntries((attachments.data ?? []).map((a) => [a.id, a])), [attachments.data]);
  return (
    <AsyncState query={inc} skeleton={<DetailSkeleton />}>
      {(i) => (
        <div>
          <PageHeader
            breadcrumb={<><Link to="/operations/incidents" className="hover:underline">{t("nav.incidents")}</Link> / <span className="font-mono">{i.incident_number}</span></>}
            title={<span><span className="font-mono">{i.incident_number}</span> <span className="font-normal text-muted-foreground">·</span> {i.title}</span>}
            badges={<><StatusBadge objectType="incident" status={i.status} /><SeverityBadge severity={i.severity} /><PriorityBadge priority={i.priority} /><FlagBadges flags={i.flags} /></>}
            subtitle={<>{i.incident_type} · {i.category} · dilaporkan <RelativeTime value={i.reported_at} /> oleh {i.reported_by_name ?? "system"}</>}
            actions={
              <>
                {can("operations.work_orders.create") && !["closed", "cancelled"].includes(i.status) && <Button variant="secondary" size="sm" onClick={() => setWoOpen(true)}>{t("action.create_work_order")}</Button>}
                <TransitionActions objectType="incident" item={i} onAssign={() => setAssignOpen(true)} />
              </>
            }
          />
          <div className="grid grid-cols-12 gap-5">
            <div className="col-span-8">
              <Tabs defaultValue="detail">
                <TabsList>
                  <TabsTrigger value="detail">{t("label.detail")}</TabsTrigger>
                  <TabsTrigger value="evidence">{t("label.evidence")} ({i.attachment_count})</TabsTrigger>
                  <TabsTrigger value="activity">{t("label.activity")}</TabsTrigger>
                  <TabsTrigger value="related">{t("label.related")} ({i.links.length})</TabsTrigger>
                </TabsList>
                <TabsContent value="detail" className="space-y-4 pt-4">
                  <Card><CardContent className="pt-4">
                    <p className="whitespace-pre-line text-body">{i.description || <span className="italic text-muted-foreground">Tanpa deskripsi.</span>}</p>
                    {i.resolution && <div className="mt-4"><div className="text-xs font-semibold uppercase text-muted-foreground">{t("label.resolution")}</div><p className="whitespace-pre-line text-body">{i.resolution}</p></div>}
                  </CardContent></Card>
                  <CommentsPanel resource="incidents" id={i.id} comments={comments.data ?? []} />
                </TabsContent>
                <TabsContent value="evidence" className="space-y-4 pt-4">
                  {!["closed", "cancelled"].includes(i.status) && <PhotoEvidenceUploader objectType="incident" objectId={i.id} attachmentType="photo" />}
                  <EvidenceSections items={attachments.data ?? []} />
                </TabsContent>
                <TabsContent value="activity" className="pt-4"><AsyncState query={activities}>{(acts) => <ActivityTimeline items={acts} objectLabel="Incident" attachmentsById={attById} />}</AsyncState></TabsContent>
                <TabsContent value="related" className="pt-4"><RelatedList links={i.links} /></TabsContent>
              </Tabs>
            </div>
            <div className="col-span-4 space-y-4">
              <Card><CardHeader><CardTitle>Penugasan</CardTitle>{i.allowed_actions.includes("assign") && <Button variant="link" size="sm" onClick={() => setAssignOpen(true)}>{t("action.assign")}</Button>}</CardHeader><CardContent>
                <KeyValue items={[{ label: t("label.team"), value: i.assignee.team_name }, { label: t("label.assignee"), value: i.assignee.user_name ?? <em className="text-muted-foreground">belum ditugaskan</em> }]} />
              </CardContent></Card>
              <Card><CardHeader><CardTitle>Informasi</CardTitle></CardHeader><CardContent>
                <KeyValue items={[
                  { label: t("label.location"), value: <LocationPath pathText={i.location.path_text} locationId={i.location.id} linkTo={(lid) => `/property/locations/${lid}`} /> },
                  { label: "Terjadi", value: fmtDateTime(i.occurred_at) },
                  { label: "Dilaporkan", value: fmtDateTime(i.reported_at) },
                  { label: "Resolved", value: fmtDateTime(i.resolved_at) },
                  { label: "Closed", value: fmtDateTime(i.closed_at) },
                  { label: "Sumber", value: i.links.find((l) => l.link_type === "generated_from" && l.direction === "to") ? <Link className="text-brand-600 hover:underline" to={itemLink(i.links.find((l) => l.link_type === "generated_from" && l.direction === "to")!.object_type, i.links.find((l) => l.link_type === "generated_from" && l.direction === "to")!.object_id)}>{objectTypeLabel[i.links.find((l) => l.link_type === "generated_from" && l.direction === "to")!.object_type]}</Link> : "—" },
                ]} />
              </CardContent></Card>
            </div>
          </div>
          <AssignDialog objectType="incident" id={i.id} open={assignOpen} onOpenChange={setAssignOpen} current={i.assignee} />
          <CreateWorkItemDialog objectType="work_order" open={woOpen} onOpenChange={setWoOpen} defaults={{ title: i.title, location_id: i.location.id, priority: i.priority, type: "corrective", source_type: "incident", source_id: i.id, link_to: { object_type: "incident", object_id: i.id, link_type: "generated_from" } }} onCreated={(x) => nav(`/operations/work-orders/${x.id}`)} />
        </div>
      )}
    </AsyncState>
  );
}
