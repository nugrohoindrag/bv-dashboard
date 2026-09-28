// Dialog & aksi bersama modul Operations: AssignDialog, CreateWorkItemDialog, TransitionActions (allowed_actions → tombol),
// useExport (POST /exports → polling → link unduh). Nama aksi mengikuti Naming Convention §42–§44.
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { RowActionMenu } from "@buildingvision/ui/bv";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { ReasonDialog, useToast } from "@/components/bv/common";
import { MobileActionBar } from "@/components/bv/mobile";
import { AssetPicker, LocationPicker, TeamPicker, UserPicker } from "@/components/bv/pickers";
import { workTypeLabel } from "@/components/bv/badges";
import { useAssign, useCreate, useInvalidate, useTaskCategories, useTemplates, useTransition } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { WorkItem } from "@/api/types";

export type ActionObjectType = "task" | "work_order" | "service_request" | "incident" | "finding";
export const resourceFor: Record<ActionObjectType, string> = { task: "tasks", work_order: "work-orders", service_request: "service-requests", incident: "incidents", finding: "findings" };

export function errMessage(e: unknown): string {
  const err = e as { problem?: { detail?: string; title?: string }; message?: string };
  return err?.problem?.detail ?? err?.problem?.title ?? err?.message ?? "Terjadi kesalahan";
}

// ---------- Assign ----------
export function AssignDialog({ objectType, id, open, onOpenChange, current, domain, onDone }: { objectType: ActionObjectType; id: string; open: boolean; onOpenChange: (o: boolean) => void; current?: { user_id: string | null; team_id: string | null }; domain?: string; onDone?: () => void }) {
  const { t } = useTranslation();
  const { propertyId } = useAuth();
  const toast = useToast();
  const [teamId, setTeamId] = useState<string | null>(current?.team_id ?? null);
  const [userId, setUserId] = useState<string | null>(current?.user_id ?? null);
  const [note, setNote] = useState("");
  const assign = useAssign(resourceFor[objectType]);
  useEffect(() => {
    if (open) {
      setTeamId(current?.team_id ?? null);
      setUserId(current?.user_id ?? null);
      setNote("");
    }
  }, [open, current?.team_id, current?.user_id]);
  const submit = async () => {
    try {
      await assign.mutateAsync({ id, assignee_team_id: teamId, assignee_user_id: userId, note: note || undefined });
      toast.action("assigned");
      onOpenChange(false);
      onDone?.();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title={t("action.assign")} description="Pilih team dan/atau user. Assignee user harus anggota team bila keduanya diisi.">
        <div className="space-y-4">
          <Field label={t("label.team")}>
            <TeamPicker propertyId={propertyId} domain={domain} value={teamId} onChange={(v) => { setTeamId(v); setUserId(null); }} />
          </Field>
          <Field label={t("label.assignee")}>
            <UserPicker propertyId={propertyId} teamId={teamId} value={userId} onChange={setUserId} />
          </Field>
          <Field label="Catatan">
            <Textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>{t("action.discard")}</Button>
          <Button onClick={submit} loading={assign.isPending} disabled={!teamId && !userId}>{t("action.assign")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Create Task / Work Order (FR-TSK-001, FR-WO-001) ----------
// Tipe valid per object (server menolak tipe lain). WO: PRD P1 v2 §22.2 (+ maintenance, wajib asset).
export const TASK_TYPES = ["general", "inspection", "patrol", "cleaning", "routine_maintenance"];
export const WO_TYPES = ["corrective", "preventive", "inspection", "repair", "service", "other", "maintenance"];
const typeText = (x: string) => workTypeLabel[x] ?? x;
const money = (v: string) => (v.trim() === "" ? undefined : { currency_code: "IDR", amount: Number(v) });

export function CreateWorkItemDialog({ objectType, open, onOpenChange, defaults, onCreated }: { objectType: "task" | "work_order"; open: boolean; onOpenChange: (o: boolean) => void; defaults?: Partial<{ type: string; title: string; location_id: string | null; asset_id: string | null; priority: string; category: string; source_type: string; source_id: string; inspection_type: string; link_to: { object_type: string; object_id: string; link_type: string } }>; onCreated?: (item: WorkItem) => void }) {
  const { t } = useTranslation();
  const { propertyId, properties } = useAuth();
  const toast = useToast();
  const types = objectType === "task" ? TASK_TYPES : WO_TYPES;
  const [pid, setPid] = useState(propertyId ?? properties[0]?.id ?? "");
  const [type, setType] = useState(defaults?.type ?? types[0]);
  const [title, setTitle] = useState(defaults?.title ?? "");
  const [description, setDescription] = useState("");
  const [locationId, setLocationId] = useState<string | null>(defaults?.location_id ?? null);
  const [assetId, setAssetId] = useState<string | null>(defaults?.asset_id ?? null);
  const [priority, setPriority] = useState(defaults?.priority ?? "medium");
  const [dueAt, setDueAt] = useState("");
  const [scheduledAt, setScheduledAt] = useState("");
  const [templateId, setTemplateId] = useState("");
  const [requiresEvidence, setRequiresEvidence] = useState(objectType === "work_order");
  const [notes, setNotes] = useState("");
  const [teamId, setTeamId] = useState<string | null>(null);
  const [userId, setUserId] = useState<string | null>(null);
  const [category, setCategory] = useState(defaults?.category ?? "");
  const [error, setError] = useState<string | null>(null);
  const categories = useTaskCategories();
  const templates = useTemplates({ status: "published", applies_to: objectType });
  const create = useCreate<Record<string, unknown>, WorkItem>(resourceFor[objectType]);
  const createInspection = useCreate<Record<string, unknown>, WorkItem>("inspections"); // Task inspection → INS- extension (PRD §12.4)
  useEffect(() => {
    if (open) {
      setPid(propertyId ?? properties[0]?.id ?? "");
      setType(defaults?.type ?? types[0]);
      setTitle(defaults?.title ?? "");
      setLocationId(defaults?.location_id ?? null);
      setAssetId(defaults?.asset_id ?? null);
      setPriority(defaults?.priority ?? "medium");
      setCategory(defaults?.category ?? "");
      setError(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  // draft (WO saja, PRD P1 v2 §23): tanpa assignee, SLA belum berjalan sampai Submit
  const submit = async (draft = false) => {
    if (!title.trim() || !locationId) {
      setError("Judul dan lokasi wajib diisi.");
      return;
    }
    const body: Record<string, unknown> = {
      property_id: pid,
      [objectType === "task" ? "task_type" : "work_order_type"]: type,
      title: title.trim(),
      description: description || null,
      location_id: locationId,
      asset_id: assetId,
      priority,
      due_at: dueAt ? new Date(dueAt).toISOString() : null,
      scheduled_start_at: scheduledAt ? new Date(scheduledAt).toISOString() : null,
      checklist_template_id: templateId || null,
      assignee_team_id: teamId,
      assignee_user_id: userId,
      source_type: defaults?.source_type ?? null,
      source_id: defaults?.source_id ?? null,
      link_to: defaults?.link_to ?? null,
    };
    if (objectType === "task") {
      body.requires_photo = requiresEvidence;
      body.category = category || null; // PRD P1 v2 §13.3
    } else {
      body.requires_evidence = requiresEvidence;
      body.notes = notes.trim() || null; // PRD P0 v2 §11: catatan Work Order
      if (draft) {
        body.draft = true;
        body.assignee_team_id = null;
        body.assignee_user_id = null;
      }
    }
    try {
      const isInspection = objectType === "task" && type === "inspection";
      if (isInspection) body.inspection_type = defaults?.inspection_type ?? "engineering";
      const item = await (isInspection ? createInspection : create).mutateAsync(body);
      toast.action("created", `${objectType === "task" ? "Task" : "Work Order"} ${item.number}${draft ? " (Draft)" : ""}`);
      onOpenChange(false);
      onCreated?.(item);
    } catch (e) {
      setError(errMessage(e));
    }
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent side="right" title={objectType === "task" ? t("action.create_task") : t("action.create_work_order")}>
        <div className="space-y-4">
          {error && <p className="rounded-md bg-critical-soft px-3 py-2 text-sm text-critical-text">{error}</p>}
          {properties.length > 1 && (
            <Field label="Property" required>
              <NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); setLocationId(null); setAssetId(null); }}>
                {properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </NativeSelect>
            </Field>
          )}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={t("label.type")} required>
              <NativeSelect value={type} onChange={(e) => setType(e.target.value)}>
                {types.map((x) => <option key={x} value={x}>{typeText(x)}</option>)}
              </NativeSelect>
            </Field>
            <Field label={t("label.priority")} required>
              <NativeSelect value={priority} onChange={(e) => setPriority(e.target.value)}>
                {["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}
              </NativeSelect>
            </Field>
          </div>
          <Field label={t("label.title")} required>
            <Input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={200} />
          </Field>
          {objectType === "task" && (
            <Field label={t("label.category")}>
              <NativeSelect value={category} onChange={(e) => setCategory(e.target.value)}>
                <option value="">Tanpa kategori</option>
                {categories.map((c) => <option key={c.value} value={c.value}>{c.label}</option>)}
              </NativeSelect>
            </Field>
          )}
          <Field label={t("label.description")}>
            <Textarea rows={3} value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
          <Field label={t("label.location")} required>
            <LocationPicker propertyId={pid} value={locationId} onChange={(id) => setLocationId(id)} />
          </Field>
          <Field label={t("label.asset")}>
            <AssetPicker propertyId={pid} locationId={locationId} value={assetId} onChange={setAssetId} />
          </Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Jadwal mulai">
              <Input type="datetime-local" value={scheduledAt} onChange={(e) => setScheduledAt(e.target.value)} />
            </Field>
            <Field label={t("label.due")}>
              <Input type="datetime-local" value={dueAt} onChange={(e) => setDueAt(e.target.value)} />
            </Field>
          </div>
          <Field label={t("label.checklist")}>
            <NativeSelect value={templateId} onChange={(e) => setTemplateId(e.target.value)}>
              <option value="">Tanpa checklist</option>
              {(templates.data ?? []).map((tp) => <option key={tp.id} value={tp.id}>{tp.name} (v{tp.current_version})</option>)}
            </NativeSelect>
          </Field>
          {objectType === "work_order" && (
            <Field label={t("wo.notes")} help={t("wo.notes_help")}>
              <Textarea rows={2} value={notes} onChange={(e) => setNotes(e.target.value)} />
            </Field>
          )}
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={requiresEvidence} onCheckedChange={(v) => setRequiresEvidence(!!v)} /> Wajib foto evidence sebelum selesai
          </label>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={t("label.team")}>
              <TeamPicker propertyId={pid} value={teamId} onChange={(v) => { setTeamId(v); setUserId(null); }} />
            </Field>
            <Field label={t("label.assignee")}>
              <UserPicker propertyId={pid} teamId={teamId} value={userId} onChange={setUserId} />
            </Field>
          </div>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>{t("action.discard")}</Button>
          {objectType === "work_order" && <Button variant="secondary" onClick={() => submit(true)} loading={create.isPending && create.variables?.draft === true}>{t("action.save_draft")}</Button>}
          <Button onClick={() => submit()} loading={(create.isPending && create.variables?.draft !== true) || createInspection.isPending}>{t("action.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Transition actions ----------
// Aksi yang butuh alasan/catatan (PRD §9.4): cancel, hold, reopen wajib reason; complete/resolve punya catatan opsional; schedule butuh tanggal.
const REASON_ACTIONS: Record<string, { title: string; label: string; required: boolean; destructive?: boolean; field: "reason" | "completion_notes" | "resolution" }> = {
  cancel: { title: "Batalkan", label: "Alasan pembatalan", required: true, destructive: true, field: "reason" },
  hold: { title: "Tunda", label: "Alasan penundaan", required: true, field: "reason" },
  reopen: { title: "Buka Kembali", label: "Alasan dibuka kembali", required: true, field: "reason" },
  complete: { title: "Selesaikan", label: "Catatan penyelesaian", required: false, field: "completion_notes" },
  resolve: { title: "Resolve", label: "Resolusi", required: false, field: "resolution" },
  close: { title: "Tutup", label: "Catatan penutupan", required: false, field: "reason" },
  wait_tenant: { title: "Menunggu Tenant", label: "Alasan menunggu tenant", required: true, field: "reason" },
  delete: { title: "Hapus", label: "Alasan penghapusan", required: true, destructive: true, field: "reason" }, // hanya draft (PRD P0 v2 §10.4)
};
const PRIMARY: Record<string, true> = { start: true, complete: true, resolve: true, acknowledge: true, resume: true, submit: true };

const ACTION_ICON: Record<string, string> = { delete: "delete", create_work_order: "construction", start: "play_arrow", resume: "play_arrow", complete: "check_circle", close: "task_alt", verify: "verified", hold: "pause_circle", reopen: "replay", cancel: "cancel", schedule: "event", acknowledge: "mark_email_read", resolve: "done_all", wait_tenant: "hourglass_top", escalate: "priority_high", skip: "skip_next", submit: "send" };

// create_task/create_work_order pada Service Request = tombol tersendiri di detail SR (POST /service-requests/{id}/tasks|work-orders)
// PRD P2 v2.1 §6.3: investigate / view_people / manage_people = kartu di detail Incident, bukan transisi.
const NON_TRANSITION = new Set(["assign", "unassign", "update", "answer", "scan", "comment", "attach", "view", "create_work_order", "create_task", "create_incident", "acknowledge_conflict", "investigate", "view_people", "manage_people"]);

/**
 * Tombol transisi dari `allowed_actions`. Mode: default (deret tombol, header detail), `compact` (baris tabel: 1 aksi utama +
 * menu), `mobileBar` (PRD P0 §22: MobileActionBar — aksi utama pertama yang diizinkan, mis. "Mulai", + overflow).
 */
export function TransitionActions({ objectType, item, onAssign, compact, mobileBar, entityLabel, onDeleted, size = "sm", detailTo }: { objectType: ActionObjectType; item: { id: string; allowed_actions: string[]; status: string; title?: string; priority?: string; severity?: string; assignee?: { user_id: string | null; team_id: string | null } }; onAssign?: () => void; compact?: boolean; mobileBar?: boolean; entityLabel?: string; onDeleted?: () => void; size?: "sm" | "md"; /** Baris tabel: item "Lihat detail" di menu opsi. */ detailTo?: string }) {
  const { t } = useTranslation();
  const nav = useNavigate();
  const toast = useToast();
  const invalidate = useInvalidate();
  const transition = useTransition(resourceFor[objectType]);
  const [deleting, setDeleting] = useState(false);
  const [woOpen, setWoOpen] = useState(false);
  const [pending, setPending] = useState<string | null>(null);
  const [scheduleOpen, setScheduleOpen] = useState(false);
  const [schedule, setSchedule] = useState({ start: "", due: "" });
  const [escalateOpen, setEscalateOpen] = useState(false);
  const [completeOpen, setCompleteOpen] = useState(false);
  const isWorkItem = objectType === "task" || objectType === "work_order";
  // eskalasi manual dengan alasan: Task/WO (PRD P1 v2 §52) dan Incident (PRD P2 v2.1 P2-SIN-05)
  const canEscalateDialog = isWorkItem || objectType === "incident";
  // Hanya transisi status yang menjadi tombol. `allowed_actions` server juga memuat aksi non-transisi
  // (comment/attach/view/update/create_*) — sebelumnya ikut dirender sebagai tombol "comment"/"attach"/"Lihat"
  // yang memanggil POST /{resource}/{id}/comment dan gagal.
  // "create_work_order" pada Task = aksi tersendiri (POST /tasks/{id}/work-orders); modul lain punya alurnya sendiri.
  const actions = item.allowed_actions.filter((a) => !NON_TRANSITION.has(a) || (a === "create_work_order" && objectType === "task"));
  const run = async (action: string, body: Record<string, unknown> = {}) => {
    if (action === "delete") {
      setDeleting(true);
      try {
        await api(`${resourceFor[objectType]}/${item.id}`, { method: "DELETE", body });
        invalidate("list", "overview");
        toast.action("deleted", entityLabel);
        setPending(null);
        onDeleted?.();
      } catch (e) {
        toast.error(e); // 409 NOT_DELETABLE → detail server
      } finally {
        setDeleting(false);
      }
      return;
    }
    try {
      await transition.mutateAsync({ id: item.id, action, body });
      toast.transition(action, entityLabel);
      setPending(null);
      setScheduleOpen(false);
    } catch (e) {
      toast.error(e);
    }
  };
  const click = (action: string) => {
    if (action === "create_work_order") setWoOpen(true);
    else if (action === "escalate" && canEscalateDialog) setEscalateOpen(true); // PRD P1 v2 §52 / P2 v2.1 P2-SIN-05: alasan wajib
    else if (action === "complete" && objectType === "work_order") setCompleteOpen(true); // catatan + biaya jasa/lain (§26)
    else if (REASON_ACTIONS[action]) setPending(action);
    else if (action === "schedule") setScheduleOpen(true);
    else run(action);
  };
  // Baris tabel (compact): satu aksi utama sebagai tombol + sisanya di menu baris (DS: hanya kolom yang dibutuhkan, bukan deretan tombol).
  const canAssign = item.allowed_actions.includes("assign") && !!onAssign;
  const primaryAction = compact ? (actions.find((a) => PRIMARY[a]) ?? (canAssign ? null : actions[0] ?? null)) : null;
  const menuActions = compact ? actions.filter((a) => a !== primaryAction) : [];
  const label = (a: string) => t(`action.${a}`, { defaultValue: a });
  const barPrimary = mobileBar ? (actions.find((a) => PRIMARY[a]) ?? actions.find((a) => a !== "cancel" && a !== "delete") ?? null) : null;
  return (
    <>
      {mobileBar ? (
        <MobileActionBar
          primary={barPrimary ? { label: label(barPrimary), icon: ACTION_ICON[barPrimary], onSelect: () => click(barPrimary), loading: transition.isPending && transition.variables?.action === barPrimary } : canAssign ? { label: t("action.assign"), icon: "person_add", onSelect: onAssign! } : null}
          secondary={[
            ...(canAssign && barPrimary ? [{ label: t("action.assign"), icon: "person_add", onSelect: onAssign! }] : []),
            ...actions.filter((a) => a !== barPrimary).map((a) => ({ label: label(a), icon: ACTION_ICON[a], onSelect: () => click(a), destructive: a === "cancel" || a === "delete" })),
          ]}
        />
      ) : compact ? (
        <>
          {canAssign && !primaryAction && (
            <Button size={size} variant="secondary" onClick={onAssign}>{t("action.assign")}</Button>
          )}
          {primaryAction && (
            <Button size={size} variant="primary" onClick={() => click(primaryAction)} loading={transition.isPending && transition.variables?.action === primaryAction}>
              {t(`action.${primaryAction}`, { defaultValue: primaryAction }).replace(/\s*\(.*\)$/, "")}
            </Button>
          )}
          {(menuActions.length > 0 || (canAssign && primaryAction) || !!detailTo) && (
            <RowActionMenu
              label="Aksi lainnya"
              items={[
                ...(detailTo ? [{ id: "detail", label: "Lihat detail", icon: "visibility", onClick: () => nav(detailTo) }] : []),
                ...(canAssign && primaryAction ? [{ id: "assign", label: t("action.assign"), icon: "person_add", onClick: onAssign }] : []),
                ...menuActions.map((a) => ({ id: a, label: t(`action.${a}`, { defaultValue: a }), icon: ACTION_ICON[a], onClick: () => click(a), danger: a === "cancel" || a === "delete" })),
              ]}
            />
          )}
        </>
      ) : (
        <>
          {canAssign && (
            <Button size={size} variant="secondary" onClick={onAssign}>{t("action.assign")}</Button>
          )}
          {actions.map((a) => (
            <Button key={a} size={size} variant={PRIMARY[a] ? "primary" : a === "cancel" || a === "delete" ? "destructive" : "secondary"} onClick={() => click(a)} loading={(transition.isPending && transition.variables?.action === a) || (a === "delete" && deleting)}>
              {t(`action.${a}`, { defaultValue: a })}
            </Button>
          ))}
        </>
      )}
      {pending && (
        <ReasonDialog
          open
          onOpenChange={(o) => !o && setPending(null)}
          title={REASON_ACTIONS[pending].title}
          label={REASON_ACTIONS[pending].label}
          confirmLabel={REASON_ACTIONS[pending].title}
          destructive={REASON_ACTIONS[pending].destructive}
          loading={transition.isPending || deleting}
          description={pending === "delete" ? t("wo.delete_desc") : REASON_ACTIONS[pending].required ? "Wajib diisi. Alasan dicatat di Activity." : "Opsional."}
          onConfirm={(reason) => {
            if (REASON_ACTIONS[pending].required && !reason.trim()) {
              toast.error(new Error("Alasan wajib diisi"));
              return;
            }
            const f = REASON_ACTIONS[pending].field;
            run(pending, f === "reason" ? { reason } : { [f]: reason || null, reason });
          }}
        />
      )}
      {escalateOpen && canEscalateDialog && <EscalateDialog objectType={objectType as EscalateObjectType} item={item} entityLabel={entityLabel} onClose={() => setEscalateOpen(false)} />}
      {completeOpen && objectType === "work_order" && <CompleteWorkOrderDialog item={item} entityLabel={entityLabel} onClose={() => setCompleteOpen(false)} />}
      {woOpen && objectType === "task" && <WorkOrderFromTaskDialog taskId={item.id} defaults={{ title: item.title, priority: item.priority }} onClose={() => setWoOpen(false)} />}
      <Dialog open={scheduleOpen} onOpenChange={setScheduleOpen}>
        <DialogContent title={t("action.schedule")}>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Jadwal mulai" required><Input type="datetime-local" value={schedule.start} onChange={(e) => setSchedule({ ...schedule, start: e.target.value })} /></Field>
            <Field label={t("label.due")}><Input type="datetime-local" value={schedule.due} onChange={(e) => setSchedule({ ...schedule, due: e.target.value })} /></Field>
          </div>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setScheduleOpen(false)}>{t("action.discard")}</Button>
            <Button loading={transition.isPending} disabled={!schedule.start} onClick={() => run("schedule", { scheduled_start_at: new Date(schedule.start).toISOString(), due_at: schedule.due ? new Date(schedule.due).toISOString() : null })}>{t("action.schedule")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

// ---------- Task → Work Order (PRD P0 v2 §11: POST /tasks/{id}/work-orders) ----------
export function WorkOrderFromTaskDialog({ taskId, defaults, onClose }: { taskId: string; defaults?: { title?: string; priority?: string }; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const nav = useNavigate();
  const invalidate = useInvalidate();
  const [form, setForm] = useState({ title: defaults?.title ?? "", work_order_type: "corrective", priority: defaults?.priority ?? "medium", notes: "" });
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    try {
      const wo = await api<WorkItem>(`tasks/${taskId}/work-orders`, { body: { title: form.title.trim() || undefined, work_order_type: form.work_order_type, priority: form.priority, notes: form.notes.trim() || undefined } });
      invalidate("list", "one", "activities");
      toast.action("created", `Work Order ${wo.number}`, { to: `/operations/work-orders/${wo.id}`, label: t("action.view") });
      onClose();
      nav(`/operations/work-orders/${wo.id}`);
    } catch (e) {
      toast.failed("created", e, "Work Order");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={t("action.create_work_order")} description={t("wo.from_task_desc")}>
        <div className="space-y-4">
          <Field label={t("label.title")} help={t("wo.title_default")}><Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} maxLength={200} /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={t("label.type")}><NativeSelect value={form.work_order_type} onChange={(e) => setForm({ ...form, work_order_type: e.target.value })}>{WO_TYPES.map((x) => <option key={x} value={x}>{typeText(x)}</option>)}</NativeSelect></Field>
            <Field label={t("label.priority")}><NativeSelect value={form.priority} onChange={(e) => setForm({ ...form, priority: e.target.value })}>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
          </div>
          <Field label={t("wo.notes")}><Textarea rows={3} value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} /></Field>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={busy} onClick={submit}>{t("action.create_work_order")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Escalate (PRD P1 v2 §4.2, §52; P2 v2.1 P2-SIN-05): alasan wajib, tujuan opsional, naikkan prioritas/severity ----------
const objectLabel = (ot: string) => (ot === "task" ? "Task" : ot === "incident" ? "Incident" : "Work Order");
export type EscalateObjectType = "task" | "work_order" | "incident";

export function EscalateDialog({ objectType, item, entityLabel, onClose }: { objectType: EscalateObjectType; item: { id: string; priority?: string; severity?: string }; entityLabel?: string; onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId } = useAuth();
  const toast = useToast();
  const transition = useTransition(resourceFor[objectType]);
  const [reason, setReason] = useState("");
  const [to, setTo] = useState<string | null>(null);
  const [raise, setRaise] = useState(false);
  const isIncident = objectType === "incident";
  // Incident: POST /incidents/{id}/escalate {reason, escalate_to_user_id, raise_severity} — level eskalasi +1
  const body = isIncident ? { reason: reason.trim(), escalate_to_user_id: to, raise_severity: raise } : { reason: reason.trim(), escalate_to_user_id: to, raise_priority: raise };
  const submit = () =>
    transition
      .mutateAsync({ id: item.id, action: "escalate", body })
      .then(() => { toast.success(`${entityLabel ?? objectLabel(objectType)} dieskalasi`); onClose(); })
      .catch((e) => toast.failed("updated", e, entityLabel));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={t("action.escalate")} description={isIncident ? "Level eskalasi naik satu. Security Supervisor, manager property, dan tujuan eskalasi menerima notifikasi; alasan dicatat di Activity." : "Supervisor team & manager property menerima notifikasi. Alasan dicatat di Activity."}>
        <div className="space-y-4">
          <Field label="Alasan eskalasi" required><Textarea rows={3} autoFocus value={reason} onChange={(e) => setReason(e.target.value)} /></Field>
          <Field label="Eskalasi ke (opsional)" help={isIncident ? "Kosongkan untuk memberi tahu Security Supervisor & manager property." : "Kosongkan untuk memberi tahu supervisor team & manager property."}><UserPicker propertyId={propertyId} value={to} onChange={setTo} placeholder="Pilih supervisor/manager…" /></Field>
          {isIncident ? (
            <Checkbox label="Naikkan severity & prioritas satu tingkat" checked={raise} disabled={item.severity === "critical" && item.priority === "critical"} onCheckedChange={(v) => setRaise(!!v)} />
          ) : (
            <Checkbox label="Naikkan prioritas satu tingkat" checked={raise} disabled={item.priority === "critical"} onCheckedChange={(v) => setRaise(!!v)} />
          )}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>
          <Button disabled={!reason.trim()} loading={transition.isPending} onClick={submit}>{t("action.escalate")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Complete Work Order (PRD P1 v2 §24, §26): catatan penyelesaian + biaya jasa/lain ----------
function CompleteWorkOrderDialog({ item, entityLabel, onClose }: { item: { id: string }; entityLabel?: string; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const transition = useTransition("work-orders");
  const [f, setF] = useState({ completion_notes: "", resolution: "", service_cost: "", other_cost: "" });
  const submit = () =>
    transition
      .mutateAsync({ id: item.id, action: "complete", body: { completion_notes: f.completion_notes.trim() || null, resolution: f.resolution.trim() || undefined, service_cost: money(f.service_cost), other_cost: money(f.other_cost) } })
      .then(() => { toast.transition("complete", entityLabel); onClose(); })
      .catch(toast.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Selesaikan Work Order" description="Biaya aktual dihitung server dari parts + jasa + lain bila tidak diisi manual.">
        <div className="space-y-4">
          <Field label={t("label.completion_notes")}><Textarea rows={3} autoFocus value={f.completion_notes} onChange={(e) => setF({ ...f, completion_notes: e.target.value })} /></Field>
          <Field label={t("label.resolution")}><Textarea rows={2} value={f.resolution} onChange={(e) => setF({ ...f, resolution: e.target.value })} /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Biaya jasa (IDR)"><Input type="number" min={0} value={f.service_cost} onChange={(e) => setF({ ...f, service_cost: e.target.value })} /></Field>
            <Field label="Biaya lain (IDR)"><Input type="number" min={0} value={f.other_cost} onChange={(e) => setF({ ...f, other_cost: e.target.value })} /></Field>
          </div>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>
          <Button loading={transition.isPending} onClick={submit}>{t("action.complete")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Service Request → Task / Work Order (PRD P1 v2 §30: POST /service-requests/{id}/tasks | work-orders) ----------
// Server mengisi default dari SR (judul, lokasi, prioritas, assignee), menautkan (generated_from), memindahkan SR → In Progress
// dan memberi notifikasi tenant.
export function WorkItemFromSRDialog({ srId, kind, defaults, onClose }: { srId: string; kind: "task" | "work_order"; defaults?: { title?: string; priority?: string }; onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId } = useAuth();
  const toast = useToast();
  const nav = useNavigate();
  const invalidate = useInvalidate();
  const categories = useTaskCategories();
  const isTask = kind === "task";
  const types = isTask ? ["general", "cleaning", "routine_maintenance"] : WO_TYPES;
  const [f, setF] = useState({ title: defaults?.title ?? "", type: isTask ? "general" : "service", priority: defaults?.priority ?? "medium", category: "", due_at: "", notes: "" });
  const [teamId, setTeamId] = useState<string | null>(null);
  const [userId, setUserId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    try {
      const body: Record<string, unknown> = { title: f.title.trim() || undefined, priority: f.priority, due_at: f.due_at ? new Date(f.due_at).toISOString() : undefined, assignee_team_id: teamId ?? undefined, assignee_user_id: userId ?? undefined };
      if (isTask) Object.assign(body, { task_type: f.type, category: f.category || undefined });
      else Object.assign(body, { work_order_type: f.type, notes: f.notes.trim() || undefined });
      const item = await api<WorkItem>(`service-requests/${srId}/${isTask ? "tasks" : "work-orders"}`, { body, idempotencyKey: uuid() });
      invalidate("list", "one", "activities", "overview");
      const path = `/operations/${isTask ? "tasks" : "work-orders"}/${item.id}`;
      toast.action("created", `${objectLabel(kind)} ${item.number}`, { to: path, label: t("action.view") });
      onClose();
      nav(path);
    } catch (e) {
      toast.failed("created", e, objectLabel(kind));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={isTask ? t("action.create_task") : t("action.create_work_order")} description="Dibuat dari Service Request ini: lokasi mengikuti SR, SR otomatis In Progress dan tenant diberi notifikasi.">
        <div className="space-y-4">
          <Field label={t("label.title")} help="Kosongkan untuk memakai judul Service Request."><Input value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} maxLength={200} /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={t("label.type")}><NativeSelect value={f.type} onChange={(e) => setF({ ...f, type: e.target.value })}>{types.map((x) => <option key={x} value={x}>{typeText(x)}</option>)}</NativeSelect></Field>
            <Field label={t("label.priority")}><NativeSelect value={f.priority} onChange={(e) => setF({ ...f, priority: e.target.value })}>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
            {isTask && <Field label={t("label.category")}><NativeSelect value={f.category} onChange={(e) => setF({ ...f, category: e.target.value })}><option value="">Tanpa kategori</option>{categories.map((c) => <option key={c.value} value={c.value}>{c.label}</option>)}</NativeSelect></Field>}
            <Field label={t("label.due")}><Input type="datetime-local" value={f.due_at} onChange={(e) => setF({ ...f, due_at: e.target.value })} /></Field>
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={t("label.team")}><TeamPicker propertyId={propertyId} value={teamId} onChange={(v) => { setTeamId(v); setUserId(null); }} /></Field>
            <Field label={t("label.assignee")}><UserPicker propertyId={propertyId} teamId={teamId} value={userId} onChange={setUserId} /></Field>
          </div>
          <p className="text-xs text-on-surface-variant">Tanpa team/assignee, penugasan mengikuti assignee Service Request.</p>
          {!isTask && <Field label={t("wo.notes")}><Textarea rows={2} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>}
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={busy} onClick={submit}>{isTask ? t("action.create_task") : t("action.create_work_order")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// PRD P1 v2.1 P1-XMW-03 / P2 v2.1 P2-XTW-01: Task tindak lanjut lintas tim dari WO yang sudah selesai (inspeksi akhir,
// re-clean, verifikasi security). Task tertaut follow_up_of ke WO dan ikut rantai Service Request asal.
const FOLLOW_UP_PURPOSES: { value: string; label: string; help: string }[] = [
  { value: "final_inspection", label: "Inspeksi akhir", help: "Mis. Housekeeping memeriksa area setelah perbaikan sebelum request tenant diselesaikan." },
  { value: "re_clean", label: "Re-clean", help: "Housekeeping membersihkan ulang area setelah perbaikan." },
  { value: "security_verification", label: "Verifikasi security", help: "Security memverifikasi hasil perbaikan; incident/finding asal baru dapat ditutup setelah ini selesai." },
  { value: "inspection", label: "Inspeksi", help: "Inspeksi umum atas hasil pekerjaan." },
  { value: "follow_up", label: "Tindak lanjut lain", help: "Pekerjaan lanjutan untuk team lain." },
];

export function FollowUpTaskDialog({ workOrderId, propertyId, defaults, onClose }: { workOrderId: string; propertyId: string; defaults?: { title?: string; priority?: string }; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const nav = useNavigate();
  const invalidate = useInvalidate();
  const [f, setF] = useState({ purpose: "final_inspection", title: "", priority: defaults?.priority ?? "medium", due_at: "", description: "" });
  const [teamId, setTeamId] = useState<string | null>(null);
  const [userId, setUserId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const purpose = FOLLOW_UP_PURPOSES.find((p) => p.value === f.purpose) ?? FOLLOW_UP_PURPOSES[0];
  const submit = async () => {
    if (!teamId && !userId) return toast.error(new Error("Pilih team atau petugas penerima tindak lanjut"));
    setBusy(true);
    try {
      const body: Record<string, unknown> = {
        purpose: f.purpose, title: f.title.trim() || undefined, priority: f.priority, description: f.description.trim() || undefined,
        due_at: f.due_at ? new Date(f.due_at).toISOString() : undefined, assignee_team_id: teamId ?? undefined, assignee_user_id: userId ?? undefined,
      };
      const item = await api<WorkItem>(`work-orders/${workOrderId}/tasks`, { body, idempotencyKey: uuid() });
      invalidate("list", "one", "activities", "overview");
      const path = `/operations/tasks/${item.id}`;
      toast.action("created", `Task ${item.number}`, { to: path, label: t("action.view") });
      onClose();
      nav(path);
    } catch (e) {
      toast.failed("created", e, "Task tindak lanjut");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Buat Task Tindak Lanjut" description="Serah terima ke team lain setelah Work Order selesai. Task tertaut ke Work Order ini dan ikut rantai request tenant (bila ada).">
        <div className="space-y-4">
          <Field label="Tujuan" help={purpose.help}><NativeSelect value={f.purpose} onChange={(e) => setF({ ...f, purpose: e.target.value })}>{FOLLOW_UP_PURPOSES.map((p) => <option key={p.value} value={p.value}>{p.label}</option>)}</NativeSelect></Field>
          <Field label={t("label.title")} help={`Kosongkan untuk "${purpose.label} — ${defaults?.title ?? "judul Work Order"}".`}><Input value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} maxLength={200} /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={t("label.priority")}><NativeSelect value={f.priority} onChange={(e) => setF({ ...f, priority: e.target.value })}>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
            <Field label={t("label.due")}><Input type="datetime-local" value={f.due_at} onChange={(e) => setF({ ...f, due_at: e.target.value })} /></Field>
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={t("label.team")} required><TeamPicker propertyId={propertyId} value={teamId} onChange={(v) => { setTeamId(v); setUserId(null); }} /></Field>
            <Field label={t("label.assignee")}><UserPicker propertyId={propertyId} teamId={teamId} value={userId} onChange={setUserId} /></Field>
          </div>
          <Field label={t("label.description")}><Textarea rows={3} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} placeholder="Kosongkan untuk ringkasan otomatis dari Work Order & resolusinya." /></Field>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={busy} onClick={submit}>Buat Task</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// Export dipindah ke lib/use-export.ts (dipakai lintas modul); diekspor ulang untuk impor lama.
export { useExport } from "@/lib/use-export";
