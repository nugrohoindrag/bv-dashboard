// Aksi cepat Attention Required (PRD P1 v2 §11; PRD P2 v2.1 P2-EMG-09): handler + dialog dipakai AttentionPanel dan daftar
// Overview agar perilaku CTA sama — Assign lewat dialog, Emergency Alert di-acknowledge/respond langsung, resolve Emergency butuh
// teks penyelesaian; aksi lain membuka deep link server.
import { useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { ReasonDialog, useToast } from "@/components/bv/common";
import { useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import type { AttentionItem } from "@/api/types";
import { AssignDialog } from "@/features/operations/dialogs";
import { emergencyAttentionTitle } from "@/features/security/p2";

/** Judul Emergency dari server: format lama berkode ("EMERGENCY — fire") diberi label; format baru dibiarkan. */
export function localizeAttention(it: AttentionItem): AttentionItem {
  return it.object_type === "emergency_alert" ? { ...it, title: emergencyAttentionTitle(it.title) } : it;
}

/** Handler aksi Attention Required + dialog-nya; dipakai AttentionPanel dan daftar Overview agar perilaku CTA sama. */
export function useAttentionActions(): { onAction: (it: AttentionItem, action: string) => void; dialogs: ReactNode } {
  const nav = useNavigate();
  const toast = useToast();
  const invalidate = useInvalidate();
  const [assign, setAssign] = useState<AttentionItem | null>(null);
  const [resolveFor, setResolveFor] = useState<AttentionItem | null>(null);
  const [busy, setBusy] = useState(false);

  const emergencyAction = (it: AttentionItem, action: string, body: Record<string, unknown> = {}) => {
    setBusy(true);
    return api(`emergency-alerts/${it.object_id}/${action}`, { body, idempotencyKey: uuid() })
      .then(() => {
        invalidate("overview", "dashboard", "list", "one", "notifications");
        if (action === "respond") toast.success(`${it.label}: security menangani di lokasi`);
        else toast.transition(action, it.label);
        setResolveFor(null);
      })
      .catch((e) => toast.failed("updated", e, it.label))
      .finally(() => setBusy(false));
  };

  const onAction = (it: AttentionItem, action: string) => {
    if (it.object_type === "emergency_alert") {
      if (action === "acknowledge" || action === "respond") void emergencyAction(it, action);
      else if (action === "resolve") setResolveFor(it);
      else nav(it.deep_link);
      return;
    }
    if (action === "assign") setAssign(it);
    else nav(it.deep_link);
  };

  const dialogs = (
    <>
      {assign && <AssignDialog objectType={assign.object_type as "task" | "work_order" | "service_request" | "incident"} id={assign.object_id} open onOpenChange={(o) => !o && setAssign(null)} />}
      {resolveFor && (
        <ReasonDialog
          open
          onOpenChange={(o) => !o && setResolveFor(null)}
          title={`Selesaikan Emergency ${resolveFor.label}`}
          label="Penyelesaian"
          confirmLabel="Selesaikan"
          description="Ringkasan penanganan; disalin ke tindakan Incident terkait."
          loading={busy}
          onConfirm={(text) => void emergencyAction(resolveFor, "resolve", { resolution: text })}
        />
      )}
    </>
  );
  return { onAction, dialogs };
}
