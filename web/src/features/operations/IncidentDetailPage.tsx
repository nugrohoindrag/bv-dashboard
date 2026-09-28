// Detail Incident (PRD §14.3; PRD P1 v2 §33; PRD P2 v2.1 §6.3): aksi start/resolve/close/reopen/cancel + Eskalasi;
// tindakan (action taken) & resolusi; investigasi; orang terlibat (data pribadi); status SLA; Buat Work Order dari
// Incident; evidence foto & video; timeline.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle, Dialog, DialogContent, DialogFooter, Field, Tabs, TabsContent, TabsList, TabsTrigger, Textarea } from "@/components/ui/primitives";
import { FlagBadges, PriorityBadge, SLAStatusBadge, SeverityBadge, StatusBadge, objectTypeLabel } from "@/components/bv/badges";
import { AsyncState, DetailSkeleton, KeyValue, LocationPath, RelativeTime, useToast } from "@/components/bv/common";
import { ActivityTimeline } from "@/components/bv/timeline";
import { PhotoEvidenceUploader } from "@/components/bv/checklist";
import { itemLink } from "@/components/bv/cards";
import { useActivities, useAttachments, useComments, useOne, useUpdate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import type { Incident } from "@/api/types";
import { EscalationBadge } from "@/features/security/shared";
import { AssignDialog, CreateWorkItemDialog, TransitionActions } from "./dialogs";
import { CommentsPanel, EvidenceSections, RelatedList } from "./WorkItemDetailPage";
import { InvestigationCard, PeopleCard, VideoEvidence } from "./IncidentExtensions";

/** Asal incident: link generated_from, atau source_type object Security P2 (Emergency Alert, pelanggaran parkir). */
function sourceOf(i: Incident): { type: string; id: string } | null {
  const l = i.links.find((x) => x.link_type === "generated_from" && x.direction === "to");
  if (l) return { type: l.object_type, id: l.object_id };
  if (i.source_type && i.source_id && ["emergency_alert", "parking_violation"].includes(i.source_type)) return { type: i.source_type, id: i.source_id };
  return null;
}

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
  const [actionOpen, setActionOpen] = useState(false);
  const attById = useMemo(() => Object.fromEntries((attachments.data ?? []).map((a) => [a.id, a])), [attachments.data]);
  // video evidence (P2-SIN-04) tampil terpisah dari foto/dokumen
  const videos = useMemo(() => (attachments.data ?? []).filter((a) => a.attachment_type === "video"), [attachments.data]);
  const nonVideo = useMemo(() => (attachments.data ?? []).filter((a) => a.attachment_type !== "video"), [attachments.data]);
  return (
    <AsyncState query={inc} skeleton={<DetailSkeleton />}>
      {(i) => {
        const src = sourceOf(i);
        return (
          <div>
            <PageHeader
              breadcrumb={<><Link to="/operations/incidents" className="hover:underline">{t("nav.incidents")}</Link> / <span className="font-mono">{i.incident_number}</span></>}
              title={<span><span className="font-mono">{i.incident_number}</span> <span className="font-normal text-muted-foreground">·</span> {i.title}</span>}
              // "Eskalasi L{n}" menggantikan flag generik "Dieskalasi" agar tidak dobel
              badges={<><StatusBadge objectType="incident" status={i.status} /><SeverityBadge severity={i.severity} /><PriorityBadge priority={i.priority} /><EscalationBadge level={i.escalation_level} /><FlagBadges item={i} flags={(i.escalation_level ?? 0) > 0 ? i.flags.filter((f) => f !== "escalated") : i.flags} /><SLAStatusBadge status={i.sla_status} /></>}
              subtitle={<>{i.incident_type} · {i.category} · dilaporkan <RelativeTime value={i.reported_at} /> oleh {i.reported_by_name ?? "system"}</>}
              actions={
                <>
                  {can("operations.work_orders.create") && !["closed", "cancelled"].includes(i.status) && <Button variant="secondary" size="sm" onClick={() => setWoOpen(true)}>{t("action.create_work_order")}</Button>}
                  <span className="hidden md:contents"><TransitionActions objectType="incident" item={i} entityLabel={i.incident_number} onAssign={() => setAssignOpen(true)} /></span>
                </>
              }
            />
            <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
              <div className="min-w-0 lg:col-span-8">
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
                    {/* PRD P1 v2 §33: tindakan yang diambil (penanganan awal/lanjutan), terpisah dari resolusi akhir */}
                    <Card><CardHeader><CardTitle>Tindakan</CardTitle>{can("operations.incidents.update") && i.allowed_actions.includes("update") && <Button variant="link" size="sm" onClick={() => setActionOpen(true)}>Edit</Button>}</CardHeader><CardContent>
                      <p className="whitespace-pre-line text-body">{i.action_taken || <span className="italic text-muted-foreground">Belum ada tindakan dicatat.</span>}</p>
                    </CardContent></Card>
                    {/* PRD P2 v2.1 P2-SIN-06 / P2-SIN-03 */}
                    <InvestigationCard incident={i} />
                    <PeopleCard incident={i} />
                    <CommentsPanel resource="incidents" id={i.id} comments={comments.data ?? []} />
                  </TabsContent>
                  <TabsContent value="evidence" className="space-y-5 pt-4">
                    {!["closed", "cancelled"].includes(i.status) && <PhotoEvidenceUploader objectType="incident" objectId={i.id} attachmentType="photo" />}
                    <EvidenceSections items={nonVideo} />
                    <VideoEvidence incident={i} videos={videos} onUploaded={() => { attachments.refetch(); inc.refetch(); }} />
                  </TabsContent>
                  <TabsContent value="activity" className="pt-4"><AsyncState query={activities}>{(acts) => <ActivityTimeline items={acts} objectLabel="Incident" attachmentsById={attById} />}</AsyncState></TabsContent>
                  <TabsContent value="related" className="pt-4"><RelatedList links={i.links} /></TabsContent>
                </Tabs>
              </div>
              <div className="min-w-0 space-y-4 lg:col-span-4">
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
                    { label: "Eskalasi", value: (i.escalation_level ?? 0) > 0 ? <>Level {i.escalation_level}<span className="block text-xs text-muted-foreground">{fmtDateTime(i.escalated_at)}</span></> : "—" },
                    { label: "Sumber", value: src ? <Link className="text-brand-600 hover:underline" to={itemLink(src.type, src.id)}>{objectTypeLabel[src.type] ?? src.type}</Link> : "—" },
                  ]} />
                </CardContent></Card>
              </div>
            </div>
            <div className="md:hidden"><TransitionActions objectType="incident" item={i} entityLabel={i.incident_number} mobileBar onAssign={() => setAssignOpen(true)} /></div>
            <AssignDialog objectType="incident" id={i.id} open={assignOpen} onOpenChange={setAssignOpen} current={i.assignee} />
            {actionOpen && <ActionTakenDialog incident={i} onClose={() => setActionOpen(false)} />}
            <CreateWorkItemDialog objectType="work_order" open={woOpen} onOpenChange={setWoOpen} defaults={{ title: i.title, location_id: i.location.id, priority: i.priority, type: "corrective", source_type: "incident", source_id: i.id, link_to: { object_type: "incident", object_id: i.id, link_type: "generated_from" } }} onCreated={(x) => nav(`/operations/work-orders/${x.id}`)} />
          </div>
        );
      }}
    </AsyncState>
  );
}

function ActionTakenDialog({ incident, onClose }: { incident: Incident; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const update = useUpdate<{ id: string; version: number; action_taken: string | null }>("incidents");
  const [text, setText] = useState(incident.action_taken ?? "");
  const submit = () => update.mutateAsync({ id: incident.id, version: incident.version, action_taken: text.trim() || null }).then(() => { toast.action("saved", incident.incident_number); onClose(); }).catch(toast.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Tindakan · ${incident.incident_number}`} description="Langkah penanganan yang sudah diambil (tercatat di Activity).">
        <Field label="Tindakan yang diambil"><Textarea rows={5} autoFocus value={text} onChange={(e) => setText(e.target.value)} /></Field>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>
          <Button loading={update.isPending} onClick={submit}>{t("action.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
