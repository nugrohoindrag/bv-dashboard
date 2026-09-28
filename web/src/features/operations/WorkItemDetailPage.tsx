// Detail Task / Work Order (DS §5.3): header (ID · judul · status · flags · aksi), tab Detail · Checklist · Evidence · Activity · Terkait,
// panel kanan: assignee, lokasi, aset, SLA, tanggal. Semua transisi lewat allowed_actions dari server.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle, Field, Input, Tabs, TabsContent, TabsList, TabsTrigger, Textarea, Dialog, DialogContent, DialogFooter, NativeSelect } from "@/components/ui/primitives";
import { FlagBadges, PriorityBadge, SLAStatusBadge, StatusBadge, categoryText, objectTypeLabel, workTypeLabel } from "@/components/bv/badges";
import { AsyncState, DetailSkeleton, KeyValue, LocationPath, RelativeTime, SLAProgress, useToast } from "@/components/bv/common";
import { ActivityTimeline } from "@/components/bv/timeline";
import { AttachmentGrid, ChecklistRunner, PhotoEvidenceUploader } from "@/components/bv/checklist";
import { itemLink } from "@/components/bv/cards";
import { useActivities, useAddComment, useAttachments, useChecklistRuns, useComments, useCreate, useTaskCategories, useTemplates, useUpdate, useWorkItem, resourceOf } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime, fmtMoney } from "@/lib/format";
import type { Attachment, Money, SLAInfo, WorkItem } from "@/api/types";
import { AssignDialog, CreateWorkItemDialog, FollowUpTaskDialog, TransitionActions, WorkOrderFromTaskDialog } from "./dialogs";
import { followUpPurposeLabel } from "./ChainTimeline";
import { CreateFindingDialog, CreateIncidentDialog } from "./FindingDialogs";
import { WorkOrderPartsPanel, WorkOrderVendorPanel } from "@/features/inventory/WorkOrderPartsPanel";
import { ConsumablesCard, RoutePositionCard } from "@/features/housekeeping/CleaningTaskPanels";
import { ChargeDialog } from "@/features/billing/ChargeDialog";

export default function WorkItemDetailPage({ objectType }: { objectType: "task" | "work_order" }) {
  const { id } = useParams();
  const { t } = useTranslation();
  const { can } = useAuth();
  const nav = useNavigate();
  const resource = resourceOf(objectType);
  const item = useWorkItem(objectType, id);
  const activities = useActivities(objectType, id);
  const comments = useComments(resource, id);
  const attachments = useAttachments(objectType, id);
  const runs = useChecklistRuns(resource, id);
  const [assignOpen, setAssignOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [woOpen, setWoOpen] = useState(false);
  const [findingOpen, setFindingOpen] = useState(false);
  const [incidentOpen, setIncidentOpen] = useState(false);
  const [woFromTask, setWoFromTask] = useState(false);
  const [costOpen, setCostOpen] = useState(false);
  const [chargeOpen, setChargeOpen] = useState(false); // PRD P4 v2.1 P4-INV-09: tagih biaya WO ke tenant
  const [followUpOpen, setFollowUpOpen] = useState(false);
  const attById = useMemo(() => Object.fromEntries((attachments.data ?? []).map((a) => [a.id, a])), [attachments.data]);
  const hkInspection = useCreate<{ cleaning_task_id: string; title: string }, WorkItem>("housekeeping-inspections");
  const toast = useToast();
  const label = objectTypeLabel[objectType];
  const canEdit = can(objectType === "task" ? "operations.tasks.update" : "operations.work_orders.update");
  const canManage = can(objectType === "task" ? "operations.tasks.manage" : "operations.work_orders.manage");

  return (
    <AsyncState query={item} skeleton={<DetailSkeleton />}>
      {(w) => (
        <div>
          <PageHeader
            breadcrumb={<><Link to={`/operations/${resource}`} className="hover:underline">{objectType === "task" ? t("nav.tasks") : t("nav.work_orders")}</Link> / <span className="font-mono">{w.number}</span></>}
            title={<span><span className="font-mono">{w.number}</span> <span className="font-normal text-muted-foreground">·</span> {w.title}</span>}
            badges={<><StatusBadge objectType={objectType} status={w.status} /><FlagBadges item={w} /><PriorityBadge priority={w.priority} /></>}
            subtitle={<>{workTypeLabel[w.type] ?? w.type}{w.category ? ` · ${categoryText(w.category)}` : ""} · {t("label.created")} <RelativeTime value={w.created_at} /> oleh {w.created_by_name ?? "system"}{w.follow_up_purpose && <> · {followUpPurposeLabel[w.follow_up_purpose] ?? w.follow_up_purpose}</>}{w.source_type && w.source_id && <> · dari <Link className="text-brand-600 hover:underline" to={itemLink(w.source_type, w.source_id)}>{objectTypeLabel[w.source_type] ?? w.source_type}</Link></>}{w.origin_service_request_id && w.source_type !== "service_request" && <> · rantai request <Link className="font-mono text-brand-600 hover:underline" to={`/operations/service-requests/${w.origin_service_request_id}`}>{w.origin_service_request_number ?? "SR"}</Link></>}</>}
            actions={
              <>
                {canEdit && w.allowed_actions.includes("update") && <Button variant="secondary" size="sm" onClick={() => setEditOpen(true)}>Edit</Button>}
                {/* PRD P1 v2.1 P1-XMW-03 / P2-XTW-01: WO selesai → Task tindak lanjut untuk team lain */}
                {w.allowed_actions.includes("create_follow_up_task") && <Button variant="secondary" size="sm" icon="assignment_return" onClick={() => setFollowUpOpen(true)}>Tindak lanjut</Button>}
                {/* < md: transisi pindah ke MobileActionBar di bawah (PRD P0 §22) */}
                <span className="hidden md:contents"><TransitionActions objectType={objectType} item={w} entityLabel={w.number} onAssign={() => setAssignOpen(true)} onDeleted={() => nav(`/operations/${resource}`)} /></span>
              </>
            }
          />
          {w.status === "draft" && <p className="mb-4 rounded-md border border-info/30 bg-info-soft px-3 py-2 text-sm text-info-text">Work Order masih Draft: belum terlihat teknisi dan SLA belum berjalan. Gunakan <strong>Submit</strong> untuk membukanya (Open).</p>}
          {w.evidence_incomplete && <p className="mb-4 rounded-md border border-warning/30 bg-warning-soft px-3 py-2 text-sm text-warning-text">{t("label.evidence_incomplete")}: unggah foto evidence / lengkapi checklist sebelum menyelesaikan.</p>}
          <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
            <div className="min-w-0 lg:col-span-8">
              <Tabs defaultValue="detail">
                <TabsList>
                  <TabsTrigger value="detail">{t("label.detail")}</TabsTrigger>
                  <TabsTrigger value="checklist">{t("label.checklist")} {w.checklist_summary ? `(${w.checklist_summary.answered_items}/${w.checklist_summary.total_items})` : ""}</TabsTrigger>
                  <TabsTrigger value="evidence">{t("label.evidence")} ({w.attachment_count})</TabsTrigger>
                  <TabsTrigger value="activity">{t("label.activity")}</TabsTrigger>
                  <TabsTrigger value="related">{t("label.related")} ({w.links?.length ?? 0})</TabsTrigger>
                </TabsList>
                <TabsContent value="detail" className="space-y-4 pt-4">
                  <Card><CardContent className="pt-4">
                    <p className="whitespace-pre-line text-body">{w.description || <span className="italic text-muted-foreground">Tanpa deskripsi.</span>}</p>
                    {w.completion_notes && <div className="mt-4"><div className="text-xs font-semibold uppercase text-muted-foreground">{t("label.completion_notes")}</div><p className="whitespace-pre-line text-body">{w.completion_notes}</p></div>}
                    {w.resolution && <div className="mt-4"><div className="text-xs font-semibold uppercase text-muted-foreground">{t("label.resolution")}</div><p className="whitespace-pre-line text-body">{w.resolution}</p></div>}
                    {w.notes && <div className="mt-4"><div className="text-xs font-semibold uppercase text-muted-foreground">{t("wo.notes")}</div><p className="whitespace-pre-line text-body">{w.notes}</p></div>}
                  </CardContent></Card>
                  {objectType === "work_order" && (
                    // PRD P1 v2 §26: estimasi · parts · jasa · lain · aktual (aktual = jumlah komponen bila tidak diisi manual)
                    <Card><CardHeader><CardTitle>Biaya & Vendor</CardTitle><span className="flex flex-wrap items-center gap-3">{w.status !== "cancelled" && can("billing.invoices.create", w.property_id) && <Button variant="link" size="sm" onClick={() => setChargeOpen(true)}>Tagih biaya ke tenant</Button>}{canEdit && w.allowed_actions.includes("update") && <Button variant="link" size="sm" onClick={() => setCostOpen(true)}>Edit biaya</Button>}</span></CardHeader><CardContent>
                      <KeyValue items={[
                        { label: "Estimasi biaya", value: moneyText(w.estimated_cost) },
                        { label: "Biaya parts", value: moneyText(w.parts_cost) },
                        { label: "Biaya jasa", value: moneyText(w.service_cost) },
                        { label: "Biaya lain", value: moneyText(w.other_cost) },
                        { label: "Biaya aktual", value: <span className="font-semibold">{moneyText(w.actual_cost)}</span> },
                        { label: "Parts", value: w.parts_usage ?? "—" },
                        { label: "Vendor ref", value: w.vendor_reference ?? "—" },
                        { label: "Reopen", value: w.reopen_count ?? 0 },
                      ]} />
                    </CardContent></Card>
                  )}
                  {objectType === "work_order" && <WorkOrderPartsPanel woId={w.id} propertyId={w.property_id} terminal={["closed", "cancelled"].includes(w.status)} editable={!["completed", "closed", "cancelled"].includes(w.status)} />}
                  {/* PRD P2 v2.1 §7.5 P2-CNS-02: consumable per cleaning task */}
                  {objectType === "task" && w.type === "cleaning" && <ConsumablesCard task={w} />}
                  <CommentsPanel resource={resource} id={w.id} comments={comments.data ?? []} />
                </TabsContent>
                <TabsContent value="checklist" className="pt-4">
                  <AsyncState query={runs}>
                    {(list) => list.length === 0 ? <p className="py-8 text-center text-sm text-muted-foreground">{w.checklist_template_id ? "Checklist run belum dibuat." : "Tidak ada checklist pada item ini."}</p> : (
                      <div className="space-y-6">
                        {/* Server hanya menerima jawaban saat In Progress (assignee) atau pemegang *.manage (on-behalf). */}
                        {w.status !== "in_progress" && !canManage && !["closed", "cancelled", "completed"].includes(w.status) && <p className="rounded-md border border-warning/30 bg-warning-soft px-3 py-2 text-sm text-warning-text">Checklist dapat diisi setelah pekerjaan dimulai (Mulai).</p>}
                        {list.map((run) => <ChecklistRunner key={run.id} run={run} editable={w.status === "in_progress" ? (canEdit || w.allowed_actions.includes("answer")) : canManage && !["closed", "cancelled"].includes(w.status)} objectType={objectType} objectId={w.id} attachmentsById={attById} />)}
                      </div>
                    )}
                  </AsyncState>
                </TabsContent>
                <TabsContent value="evidence" className="space-y-4 pt-4">
                  {/* Before saat belum dimulai; Before + During + After saat dikerjakan (PRD P1 v2 §19; guard complete WO requires_evidence
                      menuntut photo_after SEBELUM complete — After harus bisa diunggah selagi In Progress). */}
                  {!["closed", "cancelled"].includes(w.status) && (
                    <div className="grid gap-3 md:grid-cols-3">
                      {w.status !== "completed" && <PhotoEvidenceUploader objectType={objectType} objectId={w.id} attachmentType="photo_before" label="Unggah foto Before" />}
                      {["in_progress", "on_hold"].includes(w.status) && <PhotoEvidenceUploader objectType={objectType} objectId={w.id} attachmentType="photo_during" label="Unggah foto During" />}
                      {["in_progress", "on_hold", "completed"].includes(w.status) && <PhotoEvidenceUploader objectType={objectType} objectId={w.id} attachmentType="photo_after" label="Unggah foto After" />}
                    </div>
                  )}
                  <EvidenceSections items={attachments.data ?? []} />
                </TabsContent>
                <TabsContent value="activity" className="pt-4">
                  <AsyncState query={activities}>{(acts) => <ActivityTimeline items={acts} objectLabel={label} attachmentsById={attById} />}</AsyncState>
                </TabsContent>
                <TabsContent value="related" className="space-y-3 pt-4">
                  <div className="flex flex-wrap gap-2">
                    {can("operations.work_orders.create") && objectType === "task" && !w.allowed_actions.includes("create_work_order") && <Button variant="secondary" size="sm" onClick={() => setWoOpen(true)}>{t("action.create_work_order")}</Button>}
                    {objectType === "task" && w.allowed_actions.includes("create_work_order") && <Button variant="secondary" size="sm" onClick={() => setWoFromTask(true)}>{t("action.create_work_order")}</Button>}
                    {w.allowed_actions.includes("create_follow_up_task") && <Button variant="secondary" size="sm" onClick={() => setFollowUpOpen(true)}>Buat Task Tindak Lanjut</Button>}
                    {can("operations.findings.create") && <Button variant="secondary" size="sm" onClick={() => setFindingOpen(true)}>Catat Finding</Button>}
                    {can("operations.incidents.create") && <Button variant="secondary" size="sm" onClick={() => setIncidentOpen(true)}>{t("action.report_incident")}</Button>}
                    {objectType === "task" && w.type === "cleaning" && can("housekeeping.inspections.create") && <Button variant="secondary" size="sm" loading={hkInspection.isPending} onClick={() => hkInspection.mutateAsync({ cleaning_task_id: w.id, title: `Inspeksi · ${w.title}` }).then((x) => nav(itemLink("task", x.id))).catch(toast.error)}>Buat Inspeksi Housekeeping</Button>}
                  </div>
                  <RelatedList links={w.links ?? []} />
                </TabsContent>
              </Tabs>
            </div>
            <div className="min-w-0 space-y-4 lg:col-span-4">
              <Card><CardHeader><CardTitle>Penugasan</CardTitle>{w.allowed_actions.includes("assign") && <Button variant="link" size="sm" onClick={() => setAssignOpen(true)}>{t("action.assign")}</Button>}</CardHeader><CardContent>
                <KeyValue items={[{ label: t("label.team"), value: w.assignee.team_name }, { label: t("label.assignee"), value: w.assignee.user_name ?? <em className="text-muted-foreground">belum ditugaskan</em> }]} />
              </CardContent></Card>
              {/* PRD P2 v2.1 P2-TEC-03: saran teknisi sesuai kategori equipment (saran, bukan pemblokiran) */}
              {objectType === "work_order" && <WorkOrderVendorPanel woId={w.id} vendorId={w.vendor_id ?? null} vendorName={w.vendor_name ?? null} vendorNotes={w.vendor_notes ?? null} terminal={["closed", "cancelled"].includes(w.status)} />}
              {/* PRD P2 v2.1 §7.3 P2-RTE-02/03: posisi stop dalam Cleaning Route */}
              {objectType === "task" && w.type === "cleaning" && <RoutePositionCard task={w} />}
              <Card><CardHeader><CardTitle>{t("label.location")} & {t("label.asset")}</CardTitle></CardHeader><CardContent>
                <KeyValue items={[
                  { label: t("label.location"), value: <LocationPath pathText={w.location.path_text} locationId={w.location.id} linkTo={(lid) => `/property/locations/${lid}`} /> },
                  { label: t("label.asset"), value: w.asset.id ? <Link to={`/assets/${w.asset.id}`} className="text-brand-600 hover:underline"><span className="font-mono text-xs">{w.asset.asset_code}</span> {w.asset.name}</Link> : "—" },
                  ...(w.asset.equipment_name ? [{ label: "Equipment", value: w.asset.equipment_name }] : []),
                ]} />
              </CardContent></Card>
              <Card><CardHeader><CardTitle>{t("label.sla")} & Jadwal</CardTitle><SLAStatusBadge status={w.sla?.status ?? w.sla_status} /></CardHeader><CardContent className="space-y-3">
                <SLAProgress sla={w.sla} />
                <KeyValue items={[
                  ...slaRows(w.sla),
                  ...((w.escalation_level ?? 0) > 0 ? [{ label: t("label.escalated"), value: <>Level {w.escalation_level} · {fmtDateTime(w.escalated_at)}</> }] : []),
                  ...(objectType === "task" ? [{ label: t("label.category"), value: categoryText(w.category) }] : []),
                  ...(w.submitted_at ? [{ label: "Submit", value: fmtDateTime(w.submitted_at) }] : []),
                  { label: "Jadwal mulai", value: fmtDateTime(w.scheduled_start_at) },
                  { label: t("label.due"), value: <span className={w.is_overdue ? "font-semibold text-critical-text" : ""}>{fmtDateTime(w.due_at)}</span> },
                  { label: "Mulai", value: fmtDateTime(w.started_at) },
                  { label: "Selesai", value: fmtDateTime(w.completed_at) },
                  { label: "Ditutup", value: fmtDateTime(w.closed_at ?? w.cancelled_at) },
                  { label: t("label.updated"), value: fmtDateTime(w.updated_at) },
                ]} />
              </CardContent></Card>
            </div>
          </div>
          <div className="md:hidden"><TransitionActions objectType={objectType} item={w} entityLabel={w.number} mobileBar onAssign={() => setAssignOpen(true)} onDeleted={() => nav(`/operations/${resource}`)} /></div>
          <AssignDialog objectType={objectType} id={w.id} open={assignOpen} onOpenChange={setAssignOpen} current={w.assignee} />
          {editOpen && <EditDialog objectType={objectType} item={w} onClose={() => setEditOpen(false)} />}
          {costOpen && <CostDialog item={w} onClose={() => setCostOpen(false)} />}
          {chargeOpen && <ChargeDialog sourceType="work_order" sourceId={w.id} onClose={() => setChargeOpen(false)} />}
          <CreateWorkItemDialog objectType="work_order" open={woOpen} onOpenChange={setWoOpen} defaults={{ title: w.title, location_id: w.location.id, asset_id: w.asset.id, priority: w.priority, type: "corrective", link_to: { object_type: objectType, object_id: w.id, link_type: "generated_from" } }} onCreated={(x) => nav(itemLink("work_order", x.id))} />
          {woFromTask && <WorkOrderFromTaskDialog taskId={w.id} defaults={{ title: w.title, priority: w.priority }} onClose={() => setWoFromTask(false)} />}
          {followUpOpen && <FollowUpTaskDialog workOrderId={w.id} propertyId={w.property_id} defaults={{ title: w.title, priority: w.priority }} onClose={() => setFollowUpOpen(false)} />}
          <CreateFindingDialog open={findingOpen} onOpenChange={setFindingOpen} defaults={{ location_id: w.location.id, asset_id: w.asset.id, source_type: objectType, source_id: w.id, finding_type: objectType === "task" && w.type === "patrol" ? "security" : objectType === "task" && w.type === "cleaning" ? "housekeeping" : "engineering" }} onCreated={(x) => nav(`/findings/${x.id}`)} />
          <CreateIncidentDialog open={incidentOpen} onOpenChange={setIncidentOpen} defaults={{ location_id: w.location.id, source_type: objectType, source_id: w.id, title: w.title }} onCreated={(x) => nav(`/operations/incidents/${x.id}`)} />
        </div>
      )}
    </AsyncState>
  );
}

const moneyText = (m?: Money | null) => (m ? fmtMoney(m.amount, m.currency_code) : "—");
const RESPONSE_STATUS: Record<string, string> = { pending: "Menunggu respons", met: "Tepat waktu", breached: "Terlambat" };
/** Baris ringkas SLA (PRD P1 v2 §21.2): status respons + hasil akhir. */
function slaRows(sla?: SLAInfo | null): { label: string; value: React.ReactNode }[] {
  if (!sla) return [];
  const rows: { label: string; value: React.ReactNode }[] = [];
  if (sla.response_due_at) rows.push({ label: "Respons", value: <>{RESPONSE_STATUS[sla.response_status ?? ""] ?? "—"} <span className="text-xs text-muted-foreground">(due {fmtDateTime(sla.response_due_at)})</span></> });
  if (sla.met !== undefined && sla.met !== null) rows.push({ label: "Hasil SLA", value: sla.met ? "Terpenuhi" : "Tidak terpenuhi" });
  return rows;
}

export function EvidenceSections({ items }: { items: Attachment[] }) {
  // Before / During / After (PRD P1 v2 §19) + foto umum, checklist, dokumen, tanda tangan
  const groups: [string, string][] = [["photo_before", "Before"], ["photo_during", "During"], ["photo_after", "After"], ["photo", "Foto"], ["checklist_item_photo", "Checklist"], ["document", "Dokumen"], ["signature", "Tanda tangan"]];
  const known = new Set(groups.map((g) => g[0]));
  const other = items.filter((a) => !known.has(a.attachment_type));
  return (
    <div className="space-y-5">
      {groups.map(([k, label]) => {
        const list = items.filter((a) => a.attachment_type === k);
        if (!list.length) return null;
        return <div key={k}><div className="mb-2 text-xs font-semibold uppercase text-muted-foreground">{label} ({list.length})</div><AttachmentGrid items={list} /></div>;
      })}
      {other.length > 0 && <div><div className="mb-2 text-xs font-semibold uppercase text-muted-foreground">Lainnya</div><AttachmentGrid items={other} /></div>}
      {items.length === 0 && <p className="py-6 text-center text-sm text-muted-foreground">Belum ada evidence.</p>}
    </div>
  );
}

export function RelatedList({ links }: { links: { id: string; link_type: string; direction: string; object_type: string; object_id: string; label: string; title: string; status: string }[] }) {
  const linkLabel: Record<string, string> = { generated_from: "dibuat dari", related_to: "terkait", rework_of: "rework dari", escalated_to: "dieskalasi ke", resolved_by: "diselesaikan oleh" };
  if (!links.length) return <p className="py-6 text-center text-sm text-muted-foreground">Belum ada object terkait.</p>;
  return (
    <ul className="divide-y divide-border rounded-lg border border-border">
      {links.map((l) => (
        <li key={l.id} className="flex items-center justify-between gap-3 px-4 py-2.5">
          <div className="min-w-0">
            <div className="text-xs text-muted-foreground">{objectTypeLabel[l.object_type] ?? l.object_type} · {linkLabel[l.link_type] ?? l.link_type}{l.direction === "to" ? " (sumber)" : ""}</div>
            <Link to={itemLink(l.object_type, l.object_id)} className="hover:underline"><span className="font-mono text-[13px] font-semibold">{l.label}</span> {l.title}</Link>
          </div>
          <StatusBadge objectType={(["task", "work_order", "service_request", "incident", "finding", "asset", "maintenance_schedule", "inspection"].includes(l.object_type) ? l.object_type : "task") as "task"} status={l.status} />
        </li>
      ))}
    </ul>
  );
}

export function CommentsPanel({ resource, id, comments }: { resource: string; id: string; comments: { id: string; author_name: string; body: string; created_at: string; source: string }[] }) {
  const { t } = useTranslation();
  const [body, setBody] = useState("");
  const add = useAddComment(resource);
  const toast = useToast();
  return (
    <Card>
      <CardHeader><CardTitle>{t("label.comments")} ({comments.length})</CardTitle></CardHeader>
      <CardContent className="space-y-3">
        <ul className="space-y-3">
          {comments.map((c) => (
            <li key={c.id} className="rounded-md bg-muted px-3 py-2">
              <div className="flex items-center justify-between text-xs text-muted-foreground"><span className="font-medium text-foreground">{c.author_name}</span><span><RelativeTime value={c.created_at} />{c.source === "sync" ? " · offline" : ""}</span></div>
              <p className="mt-1 whitespace-pre-line text-body">{c.body}</p>
            </li>
          ))}
        </ul>
        <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); if (!body.trim()) return; add.mutateAsync({ id, body: body.trim() }).then(() => setBody("")).catch(toast.error); }}>
          <Textarea rows={2} className="flex-1" placeholder="Tulis komentar…" value={body} onChange={(e) => setBody(e.target.value)} />
          <Button type="submit" loading={add.isPending} disabled={!body.trim()}>Kirim</Button>
        </form>
      </CardContent>
    </Card>
  );
}

function EditDialog({ objectType, item, onClose }: { objectType: "task" | "work_order"; item: WorkItem; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>(resourceOf(objectType));
  const templates = useTemplates({ status: "published", applies_to: objectType });
  const categories = useTaskCategories();
  const [form, setForm] = useState({ title: item.title, description: item.description ?? "", priority: item.priority, due_at: item.due_at ? item.due_at.slice(0, 16) : "", scheduled_start_at: item.scheduled_start_at ? item.scheduled_start_at.slice(0, 16) : "", checklist_template_id: item.checklist_template_id ?? "", category: item.category ?? "", vendor_reference: item.vendor_reference ?? "", parts_usage: item.parts_usage ?? "", notes: item.notes ?? "" });
  const set = (k: keyof typeof form, v: string) => setForm((f) => ({ ...f, [k]: v }));
  const submit = () => {
    const body: Record<string, unknown> = { id: item.id, version: item.version, title: form.title, description: form.description || null, priority: form.priority, due_at: form.due_at ? new Date(form.due_at).toISOString() : null, scheduled_start_at: form.scheduled_start_at ? new Date(form.scheduled_start_at).toISOString() : null, checklist_template_id: form.checklist_template_id || null };
    if (objectType === "task") body.category = form.category || null;
    if (objectType === "work_order") {
      // biaya diubah lewat "Edit biaya" (CostDialog)
      body.vendor_reference = form.vendor_reference || null;
      body.parts_usage = form.parts_usage || null;
      body.notes = form.notes || null;
    }
    update.mutateAsync(body as { id: string; version: number }).then(() => { toast.action("saved", item.number); onClose(); }).catch(toast.error);
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={`Edit ${item.number}`}>
        <div className="space-y-4">
          <Field label={t("label.title")} required><Input value={form.title} onChange={(e) => set("title", e.target.value)} /></Field>
          <Field label={t("label.description")}><Textarea rows={3} value={form.description} onChange={(e) => set("description", e.target.value)} /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={t("label.priority")}><NativeSelect value={form.priority} onChange={(e) => set("priority", e.target.value)}>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
            <Field label={t("label.checklist")}><NativeSelect value={form.checklist_template_id} onChange={(e) => set("checklist_template_id", e.target.value)}><option value="">Tanpa checklist</option>{(templates.data ?? []).map((tp) => <option key={tp.id} value={tp.id}>{tp.name}</option>)}</NativeSelect></Field>
            {objectType === "task" && <Field label={t("label.category")} className="sm:col-span-2"><NativeSelect value={form.category} onChange={(e) => set("category", e.target.value)}><option value="">Tanpa kategori</option>{[...categories, ...(form.category && !categories.some((c) => c.value === form.category) ? [{ value: form.category, label: categoryText(form.category) }] : [])].map((c) => <option key={c.value} value={c.value}>{c.label}</option>)}</NativeSelect></Field>}
            <Field label="Jadwal mulai"><Input type="datetime-local" value={form.scheduled_start_at} onChange={(e) => set("scheduled_start_at", e.target.value)} /></Field>
            <Field label={t("label.due")}><Input type="datetime-local" value={form.due_at} onChange={(e) => set("due_at", e.target.value)} /></Field>
          </div>
          {objectType === "work_order" && (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Vendor ref"><Input value={form.vendor_reference} onChange={(e) => set("vendor_reference", e.target.value)} /></Field>
              <Field label="Parts"><Input value={form.parts_usage} onChange={(e) => set("parts_usage", e.target.value)} /></Field>
              <Field label={t("wo.notes")} className="sm:col-span-2"><Textarea rows={2} value={form.notes} onChange={(e) => set("notes", e.target.value)} /></Field>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>
          <Button loading={update.isPending} onClick={submit}>{t("action.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Biaya Work Order (PRD P1 v2 §26): estimasi · parts · jasa · lain · aktual ----------
function CostDialog({ item, onClose }: { item: WorkItem; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("work-orders");
  const amt = (m?: Money | null) => (m ? String(m.amount) : "");
  const [f, setF] = useState({ estimated_cost: amt(item.estimated_cost), parts_cost: amt(item.parts_cost), service_cost: amt(item.service_cost), other_cost: amt(item.other_cost), actual_cost: "" });
  const cur = item.actual_cost?.currency_code ?? item.estimated_cost?.currency_code ?? "IDR";
  const money = (v: string) => (v.trim() === "" ? undefined : { currency_code: cur, amount: Number(v) });
  const sum = ["parts_cost", "service_cost", "other_cost"].reduce((n, k) => n + (Number(f[k as keyof typeof f]) || 0), 0);
  const submit = () => {
    const body: Record<string, unknown> = { id: item.id, version: item.version, estimated_cost: money(f.estimated_cost), parts_cost: money(f.parts_cost), service_cost: money(f.service_cost), other_cost: money(f.other_cost) };
    if (f.actual_cost.trim()) body.actual_cost = money(f.actual_cost); // kosong → server: aktual = parts + jasa + lain
    update.mutateAsync(body as { id: string; version: number }).then(() => { toast.action("saved", `Biaya ${item.number}`); onClose(); }).catch(toast.error);
  };
  const field = (k: keyof typeof f, label: string, help?: string) => <Field label={label} help={help}><Input type="number" min={0} value={f[k]} onChange={(e) => setF({ ...f, [k]: e.target.value })} /></Field>;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Biaya · ${item.number}`} description={`Mata uang ${cur}. Biaya aktual saat ini: ${moneyText(item.actual_cost)}.`}>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {field("estimated_cost", "Estimasi biaya")}
          {field("parts_cost", "Biaya parts")}
          {field("service_cost", "Biaya jasa")}
          {field("other_cost", "Biaya lain")}
          {field("actual_cost", "Biaya aktual (override)", `Kosongkan: aktual = parts + jasa + lain (${fmtMoney(sum, cur)}).`)}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>
          <Button loading={update.isPending} onClick={submit}>{t("action.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
