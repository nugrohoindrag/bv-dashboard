// Tombol Export standar (PRD P0 §18): tampil hanya bila user punya platform.exports.create.
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/primitives";
import { useAuth } from "@/lib/auth";
import { useExport, type ExportResource } from "@/lib/use-export";

export function ExportButton({ resource, filters = {}, className }: { resource: ExportResource; filters?: Record<string, string | undefined | null>; className?: string }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const exp = useExport();
  if (!can("platform.exports.create")) return null;
  const clean = Object.fromEntries(Object.entries(filters).filter(([, v]) => v !== undefined && v !== null && v !== "")) as Record<string, string>;
  return (
    <Button variant="secondary" size="sm" icon="download" className={className} loading={exp.busy} onClick={() => exp.request(resource, clean)}>
      {t("action.export")}
    </Button>
  );
}
