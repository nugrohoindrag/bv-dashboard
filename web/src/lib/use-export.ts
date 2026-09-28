// Export Foundation (PRD P0 §18): POST /exports → polling GET /exports/{id} → buka download_url.
// Riwayat ekspor milik user (30 hari) ada di Settings › Exports.
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useToast } from "@/components/bv/common";
import { api } from "./api";
import i18n from "./i18n";
import type { ExportJob } from "@/api/types";

export const EXPORT_RESOURCES = ["tasks", "work_orders", "service_requests", "incidents", "assets", "findings", "users", "locations", "tenants", "vendors", "audit_logs"] as const;
export type ExportResource = (typeof EXPORT_RESOURCES)[number];

export function useExport() {
  const toast = useToast();
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const request = async (resource: string, filters: Record<string, string>, format: "xlsx" | "csv" = "xlsx") => {
    setBusy(true);
    try {
      const ex = await api<ExportJob>("exports", { body: { resource, format, filters } });
      qc.invalidateQueries({ queryKey: ["exports"] });
      toast.info(i18n.t("export.processing"));
      for (let i = 0; i < 40; i++) {
        await new Promise((r) => setTimeout(r, 1500));
        const st = await api<ExportJob>(`exports/${ex.id}`);
        if (st.status === "ready" && st.download_url) {
          window.open(st.download_url, "_blank");
          toast.action("exported", i18n.t(`export.resource.${resource}`, { defaultValue: resource }), { to: "/settings/exports", label: i18n.t("export.history") });
          qc.invalidateQueries({ queryKey: ["exports"] });
          return;
        }
        if (st.status === "failed") throw new Error(st.error ?? i18n.t("export.failed"));
      }
      toast.info(i18n.t("export.still_processing"));
    } catch (e) {
      toast.failed("exported", e);
    } finally {
      setBusy(false);
    }
  };
  return { request, busy };
}
