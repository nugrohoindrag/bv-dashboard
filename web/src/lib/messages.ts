// Katalog pesan Success/Error State (PRD P0 §23): konfirmasi setelah Create · Update · Assign · Complete · Delete, dst.
// Satu sumber kalimat (i18n `toast.*`) agar setiap modul memakai pola yang sama: "<Entity> berhasil <kata kerja>".
import i18n from "./i18n";

export const TOAST_ACTIONS = [
  "created", "updated", "saved", "assigned", "scheduled", "started", "held", "resumed", "completed", "resolved", "closed", "reopened",
  "cancelled", "deleted", "exported", "approved", "rejected", "verified", "published", "archived", "submitted", "sent", "uploaded", "acknowledged",
] as const;
export type ToastAction = (typeof TOAST_ACTIONS)[number];

/** Nama aksi transisi server (allowed_actions) → kategori pesan. */
export const TRANSITION_TOAST: Record<string, ToastAction> = {
  start: "started", hold: "held", resume: "resumed", complete: "completed", resolve: "resolved", close: "closed", reopen: "reopened", cancel: "cancelled",
  assign: "assigned", schedule: "scheduled", approve: "approved", reject: "rejected", deny: "rejected", verify: "verified", publish: "published",
  archive: "archived", acknowledge: "acknowledged", submit: "submitted",
};

export function isToastAction(a: string): a is ToastAction {
  return (TOAST_ACTIONS as readonly string[]).includes(a);
}

const verb = (action: ToastAction) => i18n.t(`toast.verb.${action}`);

/** "Work Order WO-001 berhasil dibuat" · tanpa entity: "Berhasil dibuat". */
export function toastMessage(action: ToastAction, entity?: string | null): string {
  return entity ? i18n.t("toast.done", { entity, verb: verb(action) }) : i18n.t("toast.done_generic", { verb: verb(action) });
}

/** "Work Order gagal dibuat." + detail problem+json bila ada. */
export function toastFailure(action: ToastAction, entity?: string | null, detail?: string | null): string {
  const head = entity ? i18n.t("toast.failed", { entity, verb: verb(action) }) : i18n.t("toast.failed_generic", { verb: verb(action) });
  return detail ? `${head} ${detail}` : head;
}
