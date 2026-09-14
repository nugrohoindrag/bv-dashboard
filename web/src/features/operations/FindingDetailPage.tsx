// Detail Finding (PRD §15.3): sumber (patrol/inspeksi/checklist item), evidence, aksi Buat Work Order / Incident, resolve/close.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { SeverityBadge, StatusBadge, objectTypeLabel } from "@/components/bv/badges";
import { AsyncState, DetailSkeleton, KeyValue, LocationPath, RelativeTime } from "@/components/bv/common";
import { ActivityTimeline } from "@/components/bv/timeline";
import { PhotoEvidenceUploader } from "@/components/bv/checklist";
import { itemLink } from "@/components/bv/cards";
import { useActivities, useAttachments, useComments, useOne } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import type { Finding } from "@/api/types";
import { CreateWorkItemDialog, TransitionActions } from "./dialogs";
import { CreateIncidentDialog } from "./FindingDialogs";
import { CommentsPanel, EvidenceSections, RelatedList } from "./WorkItemDetailPage";

export default function FindingDetailPage() {
  const { id } = useParams();
  const { t } = useTranslation();
  const { can } = useAuth();
  const nav = useNavigate();
  const q = useOne<Finding>("findings", id);
  const activities = useActivities("finding", id);
  const comments = useComments("findings", id);
  const attachments = useAttachments("finding", id);
  const [woOpen, setWoOpen] = useState(false);
  const [incOpen, setIncOpen] = useState(false);
  const attById = useMemo(() => Object.fromEntries((attachments.data ?? []).map((a) => [a.id, a])), [attachments.data]);
  return (
    <AsyncState query={q} skeleton={<DetailSkeleton />}>
      {(fd) => (
        <div>
          <PageHeader
            breadcrumb={<><Link to="/findings" className="hover:underline">Findings</Link> / <span className="font-mono">{fd.finding_number}</span></>}
            title={<span><span className="font-mono">{fd.finding_number}</span> <span className="font-normal text-muted-foreground">·</span> {fd.title}</span>}
            badges={<><StatusBadge objectType="finding" status={fd.status} /><SeverityBadge severity={fd.severity} /></>}
            subtitle={<>{fd.finding_type}{fd.category ? ` · ${fd.category}` : ""} · dilaporkan <RelativeTime value={fd.reported_at} /> oleh {fd.reported_by_name ?? "system"}{fd.source_type && fd.source_id && <> · dari <Link className="text-brand-600 hover:underline" to={itemLink(fd.source_type, fd.source_id)}>{fd.source_label || objectTypeLabel[fd.source_type] || fd.source_type}</Link></>}</>}
            actions={
              <>
                {can("operations.work_orders.create") && !["closed", "cancelled"].includes(fd.status) && <Button size="sm" onClick={() => setWoOpen(true)}>{t("action.create_from_finding")}</Button>}
                {can("operations.incidents.create") && !["closed", "cancelled"].includes(fd.status) && <Button size="sm" variant="secondary" onClick={() => setIncOpen(true)}>{t("action.create_incident")}</Button>}
                <TransitionActions objectType="finding" item={fd} />
              </>
            }
          />
          <div className="grid grid-cols-12 gap-5">
            <div className="col-span-8">
              <Tabs defaultValue="detail">
                <TabsList>
                  <TabsTrigger value="detail">{t("label.detail")}</TabsTrigger>
                  <TabsTrigger value="evidence">{t("label.evidence")} ({fd.attachment_count})</TabsTrigger>
                  <TabsTrigger value="activity">{t("label.activity")}</TabsTrigger>
                  <TabsTrigger value="related">{t("label.related")} ({fd.links.length})</TabsTrigger>
                </TabsList>
                <TabsContent value="detail" className="space-y-4 pt-4">
                  <Card><CardContent className="pt-4">
                    <p className="whitespace-pre-line text-body">{fd.description || <span className="italic text-muted-foreground">Tanpa deskripsi.</span>}</p>
                    {fd.resolution && <div className="mt-4"><div className="text-xs font-semibold uppercase text-muted-foreground">{t("label.resolution")}</div><p className="whitespace-pre-line text-body">{fd.resolution}</p></div>}
                  </CardContent></Card>
                  <CommentsPanel resource="findings" id={fd.id} comments={comments.data ?? []} />
                </TabsContent>
                <TabsContent value="evidence" className="space-y-4 pt-4">
                  {!["closed", "cancelled"].includes(fd.status) && <PhotoEvidenceUploader objectType="finding" objectId={fd.id} attachmentType="photo" />}
                  <EvidenceSections items={attachments.data ?? []} />
                </TabsContent>
                <TabsContent value="activity" className="pt-4"><AsyncState query={activities}>{(acts) => <ActivityTimeline items={acts} objectLabel="Finding" attachmentsById={attById} />}</AsyncState></TabsContent>
                <TabsContent value="related" className="pt-4"><RelatedList links={fd.links} /></TabsContent>
              </Tabs>
            </div>
            <div className="col-span-4 space-y-4">
              <Card><CardHeader><CardTitle>Informasi</CardTitle></CardHeader><CardContent>
                <KeyValue items={[
                  { label: t("label.location"), value: <LocationPath pathText={fd.location.path_text} locationId={fd.location.id} linkTo={(lid) => `/property/locations/${lid}`} /> },
                  { label: t("label.asset"), value: fd.asset.id ? <Link to={`/assets/${fd.asset.id}`} className="text-brand-600 hover:underline"><span className="font-mono text-xs">{fd.asset.asset_code}</span> {fd.asset.name}</Link> : "—" },
                  { label: "Dilaporkan", value: fmtDateTime(fd.reported_at) },
                  { label: "Resolved", value: fmtDateTime(fd.resolved_at) },
                ]} />
              </CardContent></Card>
            </div>
          </div>
          <CreateWorkItemDialog objectType="work_order" open={woOpen} onOpenChange={setWoOpen} defaults={{ title: fd.title, location_id: fd.location.id, asset_id: fd.asset.id, priority: fd.severity, type: "corrective", source_type: "finding", source_id: fd.id, link_to: { object_type: "finding", object_id: fd.id, link_type: "generated_from" } }} onCreated={(x) => nav(`/operations/work-orders/${x.id}`)} />
          <CreateIncidentDialog open={incOpen} onOpenChange={setIncOpen} defaults={{ title: fd.title, location_id: fd.location.id, source_type: "finding", source_id: fd.id, incident_type: fd.finding_type === "patrol" ? "security" : "general" }} onCreated={(x) => nav(`/operations/incidents/${x.id}`)} />
        </div>
      )}
    </AsyncState>
  );
}
