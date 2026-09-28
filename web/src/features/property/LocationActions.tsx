// Aksi status lokasi (PRD P0 v2 §7): Activate / Deactivate di semua level (POST /locations/{id}/activate|deactivate {reason})
// dan hapus typed (DELETE /areas|/units|… /{id}; property tidak dapat dihapus — nonaktifkan saja).
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ConfirmDialog } from "@/components/ui/primitives";
import { ReasonDialog, useToast } from "@/components/bv/common";
import { useInvalidate } from "@/api/hooks";
import { api } from "@/lib/api";
import type { Location } from "@/api/types";
import { TYPED_PATH } from "./location-meta";

export function LocationStatusDialog({ loc, action, onClose, onDone }: { loc: Location; action: "activate" | "deactivate"; onClose: () => void; onDone?: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const invalidate = useInvalidate();
  const [busy, setBusy] = useState(false);
  const run = async (reason: string) => {
    setBusy(true);
    try {
      await api(`locations/${loc.id}/${action}`, { body: { reason } });
      invalidate("list", "all", "one", "tree");
      toast.action(action === "activate" ? "approved" : "cancelled", `${loc.code} · ${loc.name}`);
      onDone?.();
      onClose();
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <ReasonDialog open onOpenChange={(o) => !o && onClose()} loading={busy} destructive={action === "deactivate"}
      title={t(action === "activate" ? "loc.activate_title" : "loc.deactivate_title", { name: loc.name })}
      description={t(action === "activate" ? "loc.activate_desc" : "loc.deactivate_desc")}
      confirmLabel={t(action === "activate" ? "loc.activate" : "loc.deactivate")} onConfirm={run} />
  );
}

export function DeleteLocationDialog({ loc, onClose, onDone }: { loc: Location; onClose: () => void; onDone?: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const invalidate = useInvalidate();
  const run = async () => {
    try {
      await api(`${TYPED_PATH[loc.location_type]}/${loc.id}`, { method: "DELETE" });
      invalidate("list", "all", "one", "tree");
      toast.action("deleted", `${loc.code} · ${loc.name}`);
      onDone?.();
    } catch (e) {
      toast.error(e);
    } finally {
      onClose();
    }
  };
  return <ConfirmDialog open onOpenChange={(o) => !o && onClose()} destructive title={t("loc.delete_title", { name: loc.name })} description={t("loc.delete_desc")} confirmLabel={t("loc.delete")} onConfirm={run} />;
}
