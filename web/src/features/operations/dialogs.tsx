// Dialog & aksi bersama modul Operations: AssignDialog, CreateWorkItemDialog, TransitionActions (allowed_actions → tombol),
// useExport (POST /exports → polling → link unduh). Nama aksi mengikuti Naming Convention §42–§44.
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { RowActionMenu } from "@buildingvision/ui/bv";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { ReasonDialog, useToast } from "@/components/bv/common";
import { AssetPicker, LocationPicker, TeamPicker, UserPicker } from "@/components/bv/pickers";
import { useAssign, useCreate, useTemplates, useTransition } from "@/api/hooks";
import { api } from "@/lib/api";
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
      toast.success("Penugasan disimpan");
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
const TASK_TYPES = ["general", "inspection", "patrol", "cleaning", "routine_maintenance"];
const WO_TYPES = ["maintenance", "corrective", "repair", "service", "general"];

export function CreateWorkItemDialog({ objectType, open, onOpenChange, defaults, onCreated }: { objectType: "task" | "work_order"; open: boolean; onOpenChange: (o: boolean) => void; defaults?: Partial<{ type: string; title: string; location_id: string | null; asset_id: string | null; priority: string; source_type: string; source_id: string; inspection_type: string; link_to: { object_type: string; object_id: string; link_type: string } }>; onCreated?: (item: WorkItem) => void }) {
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
  const [teamId, setTeamId] = useState<string | null>(null);
  const [userId, setUserId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
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
      setError(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const submit = async () => {
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
    if (objectType === "task") body.requires_photo = requiresEvidence;
    else body.requires_evidence = requiresEvidence;
    try {
      const isInspection = objectType === "task" && type === "inspection";
      if (isInspection) body.inspection_type = defaults?.inspection_type ?? "engineering";
      const item = await (isInspection ? createInspection : create).mutateAsync(body);
      toast.success(`${item.number} dibuat`);
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
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("label.type")} required>
              <NativeSelect value={type} onChange={(e) => setType(e.target.value)}>
                {types.map((x) => <option key={x} value={x}>{x}</option>)}
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
          <Field label={t("label.description")}>
            <Textarea rows={3} value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
          <Field label={t("label.location")} required>
            <LocationPicker propertyId={pid} value={locationId} onChange={(id) => setLocationId(id)} />
          </Field>
          <Field label={t("label.asset")}>
            <AssetPicker propertyId={pid} locationId={locationId} value={assetId} onChange={setAssetId} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
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
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={requiresEvidence} onCheckedChange={(v) => setRequiresEvidence(!!v)} /> Wajib foto evidence sebelum selesai
          </label>
          <div className="grid grid-cols-2 gap-3">
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
          <Button onClick={submit} loading={create.isPending || createInspection.isPending}>{t("action.save")}</Button>
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
};
const PRIMARY: Record<string, true> = { start: true, complete: true, resolve: true, acknowledge: true, resume: true };

const ACTION_ICON: Record<string, string> = { start: "play_arrow", resume: "play_arrow", complete: "check_circle", close: "task_alt", verify: "verified", hold: "pause_circle", reopen: "replay", cancel: "cancel", schedule: "event", acknowledge: "mark_email_read", resolve: "done_all", wait_tenant: "hourglass_top", escalate: "priority_high", skip: "skip_next" };

const NON_TRANSITION = new Set(["assign", "unassign", "update", "answer", "scan", "comment", "attach", "view", "create_work_order", "create_incident", "acknowledge_conflict"]);

export function TransitionActions({ objectType, item, onAssign, compact, size = "sm" }: { objectType: ActionObjectType; item: { id: string; allowed_actions: string[]; status: string; assignee?: { user_id: string | null; team_id: string | null } }; onAssign?: () => void; compact?: boolean; size?: "sm" | "md" }) {
  const { t } = useTranslation();
  const toast = useToast();
  const transition = useTransition(resourceFor[objectType]);
  const [pending, setPending] = useState<string | null>(null);
  const [scheduleOpen, setScheduleOpen] = useState(false);
  const [schedule, setSchedule] = useState({ start: "", due: "" });
  // Hanya transisi status yang menjadi tombol. `allowed_actions` server juga memuat aksi non-transisi
  // (comment/attach/view/update/create_*) — sebelumnya ikut dirender sebagai tombol "comment"/"attach"/"Lihat"
  // yang memanggil POST /{resource}/{id}/comment dan gagal.
  const actions = item.allowed_actions.filter((a) => !NON_TRANSITION.has(a));
  const run = async (action: string, body: Record<string, unknown> = {}) => {
    try {
      await transition.mutateAsync({ id: item.id, action, body });
      toast.success(`${t(`action.${action}`, { defaultValue: action })} berhasil`);
      setPending(null);
      setScheduleOpen(false);
    } catch (e) {
      toast.error(e);
    }
  };
  const click = (action: string) => {
    if (REASON_ACTIONS[action]) setPending(action);
    else if (action === "schedule") setScheduleOpen(true);
    else run(action);
  };
  // Baris tabel (compact): satu aksi utama sebagai tombol + sisanya di menu baris (DS: hanya kolom yang dibutuhkan, bukan deretan tombol).
  const canAssign = item.allowed_actions.includes("assign") && !!onAssign;
  const primaryAction = compact ? (actions.find((a) => PRIMARY[a]) ?? (canAssign ? null : actions[0] ?? null)) : null;
  const menuActions = compact ? actions.filter((a) => a !== primaryAction) : [];
  return (
    <>
      {compact ? (
        <>
          {canAssign && !primaryAction && (
            <Button size={size} variant="secondary" onClick={onAssign}>{t("action.assign")}</Button>
          )}
          {primaryAction && (
            <Button size={size} variant="primary" onClick={() => click(primaryAction)} loading={transition.isPending && transition.variables?.action === primaryAction}>
              {t(`action.${primaryAction}`, { defaultValue: primaryAction })}
            </Button>
          )}
          {(menuActions.length > 0 || (canAssign && primaryAction)) && (
            <RowActionMenu
              label="Aksi lainnya"
              items={[
                ...(canAssign && primaryAction ? [{ id: "assign", label: t("action.assign"), icon: "person_add", onClick: onAssign }] : []),
                ...menuActions.map((a) => ({ id: a, label: t(`action.${a}`, { defaultValue: a }), icon: ACTION_ICON[a], onClick: () => click(a), danger: a === "cancel" })),
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
            <Button key={a} size={size} variant={PRIMARY[a] ? "primary" : a === "cancel" ? "destructive" : "secondary"} onClick={() => click(a)} loading={transition.isPending && transition.variables?.action === a}>
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
          loading={transition.isPending}
          description={REASON_ACTIONS[pending].required ? "Wajib diisi. Alasan dicatat di Activity." : "Opsional."}
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
      <Dialog open={scheduleOpen} onOpenChange={setScheduleOpen}>
        <DialogContent title={t("action.schedule")}>
          <div className="grid grid-cols-2 gap-3">
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

// ---------- Export (FR-RPT / PRD §21) ----------
export function useExport() {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const request = async (resource: string, filters: Record<string, string>, format: "xlsx" | "csv" = "xlsx") => {
    setBusy(true);
    try {
      const ex = await api<{ id: string }>("exports", { body: { resource, format, filters } });
      toast.info("Export diproses… tautan unduh akan muncul di sini dan di Inbox.");
      for (let i = 0; i < 40; i++) {
        await new Promise((r) => setTimeout(r, 1500));
        const st = await api<{ status: string; download_url: string | null; error: string | null }>(`exports/${ex.id}`);
        if (st.status === "ready" && st.download_url) {
          window.open(st.download_url, "_blank");
          toast.success("Export siap diunduh");
          return;
        }
        if (st.status === "failed") throw new Error(st.error ?? "Export gagal");
      }
      toast.info("Export masih diproses. Cek Inbox untuk tautan unduh.");
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };
  return { request, busy };
}
