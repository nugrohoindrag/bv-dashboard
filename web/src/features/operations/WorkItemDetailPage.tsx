// Detail Task / Work Order (DS §5.3): header (ID · judul · status · flags · aksi), tab Detail · Checklist · Evidence · Activity · Terkait,
// panel kanan: assignee, lokasi, aset, SLA, tanggal. Semua transisi lewat allowed_actions dari server.
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle, Field, Input, Tabs, TabsContent, TabsList, TabsTrigger, Textarea, Dialog, DialogContent, DialogFooter, NativeSelect } from "@/components/ui/primitives";
import { FlagBadges, PriorityBadge, StatusBadge, objectTypeLabel, workTypeLabel } from "@/components/bv/badges";
import { AsyncState, DetailSkeleton, KeyValue, LocationPath, RelativeTime, SLAProgress, useToast } from "@/components/bv/common";
import { ActivityTimeline } from "@/components/bv/timeline";
import { AttachmentGrid, ChecklistRunner, PhotoEvidenceUploader } from "@/components/bv/checklist";
import { itemLink } from "@/components/bv/cards";
import { useActivities, useAddComment, useAttachments, useChecklistRuns, useComments, useCreate, useTemplates, useUpdate, useWorkItem, resourceOf } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime, fmtMoney } from "@/lib/format";
import type { Attachment, WorkItem } from "@/api/types";
import { AssignDialog, CreateWorkItemDialog, TransitionActions } from "./dialogs";
import { CreateFindingDialog, CreateIncidentDialog } from "./FindingDialogs";
import { WorkOrderPartsPanel, WorkOrderVendorPanel } from "@/features/inventory/WorkOrderPartsPanel";

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
  const attById = useMemo(() => Object.fromEntries((attachments.data ?? []).map((a) => [a.id, a])), [attachments.data]);
  const hkInspection = useCreate<{ cleaning_task_id: string; title: string }, WorkItem>("housekeeping-inspections");
  const toast = useToast();
  const label = objectTypeLabel[objectType];
  const canEdit = can(objectType === "task" ? "operations.tasks.update" : "operations.work_orders.update");

  return (
    <AsyncState query={item} skeleton={<DetailSkeleton />}>
      {(w) => (
        <div>
          <PageHeader
            breadcrumb={<><Link to={`/operations/${resource}`} className="hover:underline">{objectType === "task" ? t("nav.tasks") : t("nav.work_orders")}</Link> / <span className="font-mono">{w.number}</span></>}
            title={<span><span className="font-mono">{w.number}</span> <span className="font-normal text-muted-foreground">·</span> {w.title}</span>}
            badges={<><StatusBadge objectType={objectType} status={w.status} /><FlagBadges flags={w.flags} /><PriorityBadge priority={w.priority} /></>}
            subtitle={<>{workTypeLabel[w.type] ?? w.type} · {t("label.created")} <RelativeTime value={w.created_at} /> oleh {w.created_by_name ?? "system"}{w.source_type && w.source_id && <> · dari <Link className="text-brand-600 hover:underline" to={itemLink(w.source_type, w.source_id)}>{objectTypeLabel[w.source_type] ?? w.source_type}</Link></>}</>}
            actions={
              <>
                {canEdit && w.allowed_actions.includes("update") && <Button variant="secondary" size="sm" onClick={() => setEditOpen(true)}>Edit</Button>}
                <TransitionActions objectType={objectType} item={w} onAssign={() => setAssignOpen(true)} />
              </>
            }
          />
          {w.evidence_incomplete && <p className="mb-4 rounded-md border border-warning/30 bg-warning-soft px-3 py-2 text-sm text-warning-text">{t("label.evidence_incomplete")}: unggah foto evidence / lengkapi checklist sebelum menyelesaikan.</p>}
          <div className="grid grid-cols-12 gap-5">
            <div className="col-span-8">
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
                  </CardContent></Card>
                  {objectType === "work_order" && (
                    <Card><CardHeader><CardTitle>Biaya & Vendor</CardTitle></CardHeader><CardContent>
                      <KeyValue items={[
                        { label: "Estimasi biaya", value: w.estimated_cost ? fmtMoney(w.estimated_cost.amount, w.estimated_cost.currency_code) : "—" },
                        { label: "Biaya aktual", value: w.actual_cost ? fmtMoney(w.actual_cost.amount, w.actual_cost.currency_code) : "—" },
                        { label: "Parts", value: w.parts_usage ?? "—" },
                        { label: "Vendor ref", value: w.vendor_reference ?? "—" },
                        { label: "Reopen", value: w.reopen_count ?? 0 },
                      ]} />
                    </CardContent></Card>
                  )}
                  {objectType === "work_order" && <WorkOrderPartsPanel woId={w.id} propertyId={w.property_id} terminal={["closed", "cancelled"].includes(w.status)} editable={!["completed", "closed", "cancelled"].includes(w.status)} />}
                  <CommentsPanel resource={resource} id={w.id} comments={comments.data ?? []} />
                </TabsContent>
                <TabsContent value="checklist" className="pt-4">
                  <AsyncState query={runs}>
                    {(list) => list.length === 0 ? <p className="py-8 text-center text-sm text-muted-foreground">{w.checklist_template_id ? "Checklist run belum dibuat." : "Tidak ada checklist pada item ini."}</p> : (
                      <div className="space-y-6">
                        {list.map((run) => <ChecklistRunner key={run.id} run={run} editable={w.allowed_actions.includes("answer") || (canEdit && !["closed", "cancelled", "completed"].includes(w.status))} objectType={objectType} objectId={w.id} attachmentsById={attById} />)}
                      </div>
                    )}
                  </AsyncState>
                </TabsContent>
                <TabsContent value="evidence" className="space-y-4 pt-4">
                  {!["closed", "cancelled"].includes(w.status) && <PhotoEvidenceUploader objectType={objectType} objectId={w.id} attachmentType={w.status === "completed" ? "after" : "before"} label={w.status === "completed" ? "Unggah foto After" : "Unggah foto Before"} />}
                  <EvidenceSections items={attachments.data ?? []} />
                </TabsContent>
                <TabsContent value="activity" className="pt-4">
                  <AsyncState query={activities}>{(acts) => <ActivityTimeline items={acts} objectLabel={label} attachmentsById={attById} />}</AsyncState>
                </TabsContent>
                <TabsContent value="related" className="space-y-3 pt-4">
                  <div className="flex flex-wrap gap-2">
                    {can("operations.work_orders.create") && objectType === "task" && <Button variant="secondary" size="sm" onClick={() => setWoOpen(true)}>{t("action.create_work_order")}</Button>}
                    {can("operations.findings.create") && <Button variant="secondary" size="sm" onClick={() => setFindingOpen(true)}>Catat Finding</Button>}
                    {can("operations.incidents.create") && <Button variant="secondary" size="sm" onClick={() => setIncidentOpen(true)}>{t("action.report_incident")}</Button>}
                    {objectType === "task" && w.type === "cleaning" && can("housekeeping.inspections.create") && <Button variant="secondary" size="sm" loading={hkInspection.isPending} onClick={() => hkInspection.mutateAsync({ cleaning_task_id: w.id, title: `Inspeksi · ${w.title}` }).then((x) => nav(itemLink("task", x.id))).catch(toast.error)}>Buat Inspeksi Housekeeping</Button>}
                  </div>
                  <RelatedList links={w.links ?? []} />
                </TabsContent>
              </Tabs>
            </div>
            <div className="col-span-4 space-y-4">
              <Card><CardHeader><CardTitle>Penugasan</CardTitle>{w.allowed_actions.includes("assign") && <Button variant="link" size="sm" onClick={() => setAssignOpen(true)}>{t("action.assign")}</Button>}</CardHeader><CardContent>
                <KeyValue items={[{ label: t("label.team"), value: w.assignee.team_name }, { label: t("label.assignee"), value: w.assignee.user_name ?? <em className="text-muted-foreground">belum ditugaskan</em> }]} />
              </CardContent></Card>
              {objectType === "work_order" && <WorkOrderVendorPanel woId={w.id} vendorId={w.vendor_id ?? null} vendorName={w.vendor_name ?? null} vendorNotes={w.vendor_notes ?? null} terminal={["closed", "cancelled"].includes(w.status)} />}
              <Card><CardHeader><CardTitle>{t("label.location")} & {t("label.asset")}</CardTitle></CardHeader><CardContent>
                <KeyValue items={[
                  { label: t("label.location"), value: <LocationPath pathText={w.location.path_text} locationId={w.location.id} linkTo={(lid) => `/property/locations/${lid}`} /> },
                  { label: t("label.asset"), value: w.asset.id ? <Link to={`/assets/${w.asset.id}`} className="text-brand-600 hover:underline"><span className="font-mono text-xs">{w.asset.asset_code}</span> {w.asset.name}</Link> : "—" },
                ]} />
              </CardContent></Card>
              <Card><CardHeader><CardTitle>{t("label.sla")} & Jadwal</CardTitle></CardHeader><CardContent className="space-y-3">
                <SLAProgress sla={w.sla} />
                <KeyValue items={[
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
          <AssignDialog objectType={objectType} id={w.id} open={assignOpen} onOpenChange={setAssignOpen} current={w.assignee} />
          {editOpen && <EditDialog objectType={objectType} item={w} onClose={() => setEditOpen(false)} />}
          <CreateWorkItemDialog objectType="work_order" open={woOpen} onOpenChange={setWoOpen} defaults={{ title: w.title, location_id: w.location.id, asset_id: w.asset.id, priority: w.priority, type: "corrective", link_to: { object_type: objectType, object_id: w.id, link_type: "generated_from" } }} onCreated={(x) => nav(itemLink("work_order", x.id))} />
          <CreateFindingDialog open={findingOpen} onOpenChange={setFindingOpen} defaults={{ location_id: w.location.id, asset_id: w.asset.id, source_type: objectType, source_id: w.id, finding_type: objectType === "task" && w.type === "patrol" ? "security" : objectType === "task" && w.type === "cleaning" ? "housekeeping" : "engineering" }} onCreated={(x) => nav(`/findings/${x.id}`)} />
          <CreateIncidentDialog open={incidentOpen} onOpenChange={setIncidentOpen} defaults={{ location_id: w.location.id, source_type: objectType, source_id: w.id, title: w.title }} onCreated={(x) => nav(`/operations/incidents/${x.id}`)} />
        </div>
      )}
    </AsyncState>
  );
}

export function EvidenceSections({ items }: { items: Attachment[] }) {
  const groups: [string, string][] = [["before", "Before"], ["after", "After"], ["photo", "Foto"], ["checklist", "Checklist"], ["document", "Dokumen"], ["signature", "Tanda tangan"]];
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
  const [form, setForm] = useState({ title: item.title, description: item.description ?? "", priority: item.priority, due_at: item.due_at ? item.due_at.slice(0, 16) : "", scheduled_start_at: item.scheduled_start_at ? item.scheduled_start_at.slice(0, 16) : "", checklist_template_id: item.checklist_template_id ?? "", vendor_reference: item.vendor_reference ?? "", estimated_cost: item.estimated_cost?.amount?.toString() ?? "", actual_cost: item.actual_cost?.amount?.toString() ?? "", parts_usage: item.parts_usage ?? "" });
  const set = (k: keyof typeof form, v: string) => setForm((f) => ({ ...f, [k]: v }));
  const submit = () => {
    const body: Record<string, unknown> = { id: item.id, version: item.version, title: form.title, description: form.description || null, priority: form.priority, due_at: form.due_at ? new Date(form.due_at).toISOString() : null, scheduled_start_at: form.scheduled_start_at ? new Date(form.scheduled_start_at).toISOString() : null, checklist_template_id: form.checklist_template_id || null };
    if (objectType === "work_order") {
      body.vendor_reference = form.vendor_reference || null;
      body.parts_usage = form.parts_usage || null;
      if (form.estimated_cost) body.estimated_cost = { currency_code: "IDR", amount: Number(form.estimated_cost) };
      if (form.actual_cost) body.actual_cost = { currency_code: "IDR", amount: Number(form.actual_cost) };
    }
    update.mutateAsync(body as { id: string; version: number }).then(() => { toast.success("Perubahan disimpan"); onClose(); }).catch(toast.error);
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={`Edit ${item.number}`}>
        <div className="space-y-4">
          <Field label={t("label.title")} required><Input value={form.title} onChange={(e) => set("title", e.target.value)} /></Field>
          <Field label={t("label.description")}><Textarea rows={3} value={form.description} onChange={(e) => set("description", e.target.value)} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("label.priority")}><NativeSelect value={form.priority} onChange={(e) => set("priority", e.target.value)}>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
            <Field label={t("label.checklist")}><NativeSelect value={form.checklist_template_id} onChange={(e) => set("checklist_template_id", e.target.value)}><option value="">Tanpa checklist</option>{(templates.data ?? []).map((tp) => <option key={tp.id} value={tp.id}>{tp.name}</option>)}</NativeSelect></Field>
            <Field label="Jadwal mulai"><Input type="datetime-local" value={form.scheduled_start_at} onChange={(e) => set("scheduled_start_at", e.target.value)} /></Field>
            <Field label={t("label.due")}><Input type="datetime-local" value={form.due_at} onChange={(e) => set("due_at", e.target.value)} /></Field>
          </div>
          {objectType === "work_order" && (
            <div className="grid grid-cols-2 gap-3">
              <Field label="Estimasi biaya (IDR)"><Input type="number" value={form.estimated_cost} onChange={(e) => set("estimated_cost", e.target.value)} /></Field>
              <Field label="Biaya aktual (IDR)"><Input type="number" value={form.actual_cost} onChange={(e) => set("actual_cost", e.target.value)} /></Field>
              <Field label="Vendor ref"><Input value={form.vendor_reference} onChange={(e) => set("vendor_reference", e.target.value)} /></Field>
              <Field label="Parts"><Input value={form.parts_usage} onChange={(e) => set("parts_usage", e.target.value)} /></Field>
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
