// Halaman sistem (PRD P0 §23, §24.1): 403 Akses ditolak, 404 Halaman tidak ditemukan, dan halaman pemulihan
// ErrorBoundary. ErrorRecoveryPage tidak bergantung pada Router agar tetap tampil bila shell ikut gagal.
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { Button } from "@/components/ui/primitives";
import { ForbiddenState, NotFoundState } from "@/components/bv/states";
import { cn } from "@/lib/utils";

export function ForbiddenPage({ detail }: { detail?: React.ReactNode }) {
  return (
    <div className="flex min-h-[60vh] items-center justify-center">
      <ForbiddenState detail={detail} />
    </div>
  );
}

export function NotFoundPage() {
  return (
    <div className="flex min-h-[60vh] items-center justify-center">
      <NotFoundState />
    </div>
  );
}

/** Chunk lazy gagal dimuat (biasanya setelah deploy baru) → pemulihan = muat ulang. */
function isChunkError(e: Error) {
  return /dynamically imported module|Loading chunk|Importing a module script failed/i.test(e.message);
}

export function ErrorRecoveryPage({ error, onReset, inline }: { error: Error; onReset?: () => void; inline?: boolean }) {
  const { t } = useTranslation();
  const chunk = isChunkError(error);
  return (
    <div role="alert" data-testid="error-recovery" className={cn("flex items-center justify-center px-4", inline ? "min-h-[60vh]" : "min-h-dvh bg-background text-on-background")}>
      <div className="w-full max-w-lg rounded-[var(--radius-xl)] border border-border bg-surface p-6 text-center" style={{ boxShadow: "var(--elevation-1)" }}>
        <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-error-container text-on-error-container" aria-hidden>
          <Icon name={chunk ? "refresh" : "report_problem"} size={28} />
        </div>
        <h1 className="mt-3 text-h1 font-bold text-on-surface">{chunk ? t("state.update_title") : t("state.crash_title")}</h1>
        <p className="mt-2 text-sm text-on-surface-variant">{chunk ? t("state.update_desc") : t("state.crash_desc")}</p>
        <p className="mt-2 text-sm text-on-surface-variant"><span className="font-semibold text-on-surface">{t("state.impact")}:</span> {t("state.crash_impact")}</p>
        <div className="mt-5 flex flex-wrap items-center justify-center gap-2">
          <Button icon="refresh" onClick={() => window.location.reload()}>{t("action.reload")}</Button>
          {!chunk && onReset && <Button variant="secondary" icon="replay" onClick={onReset}>{t("action.retry")}</Button>}
          <Button variant="ghost" icon="space_dashboard" onClick={() => window.location.assign("/overview")}>{t("action.back_overview")}</Button>
        </div>
        {!chunk && error.message && (
          <details className="mt-4 text-left text-caption text-on-surface-variant">
            <summary className="cursor-pointer">{t("state.technical_detail")}</summary>
            <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap rounded-[var(--radius-sm)] bg-surface-container p-2 font-mono">{error.message}</pre>
          </details>
        )}
      </div>
    </div>
  );
}
