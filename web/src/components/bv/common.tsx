// LocationPath · AsyncState · ReasonDialog · Toast (+katalog pesan) · SLAProgress · relative time (DS §4, §5.5; PRD P0 §23)
import * as React from "react";
import { createContext, useCallback, useContext, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { Button, Dialog, DialogContent, DialogFooter, Field, Textarea, Tooltip } from "@/components/ui/primitives";
import { EmptyState, ListSkeleton, QueryErrorState } from "./states";
import { TRANSITION_TOAST, isToastAction, toastFailure, toastMessage, type ToastAction } from "@/lib/messages";
import { fmtDateTime, fmtRelative, fmtMinutes } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { SLAInfo } from "@/api/types";

// ---------- LocationPath: "Tower A / Lantai 12 / Mechanical Room" (level terakhir bold; klik → filter subtree) ----------
export function LocationPath({ path, pathText, locationId, className, linkTo }: { path?: { id: string; name: string }[]; pathText?: string | null; locationId?: string | null; className?: string; linkTo?: (id: string) => string }) {
  const parts = path?.length ? path.slice(path.length > 1 ? 1 : 0) : (pathText ?? "").split(" / ").filter(Boolean).map((name, i) => ({ id: String(i), name }));
  if (!parts.length) return <span className={cn("text-muted-foreground", className)}>—</span>;
  return (
    <span className={cn("inline text-sm leading-5 text-on-surface-variant [&>*]:inline [&>*]:align-middle", className)} title={pathText ?? undefined}>
      {parts.map((p, i) => {
        const last = i === parts.length - 1;
        const el = <span className={cn("whitespace-nowrap", last && "font-semibold text-on-surface")}>{p.name}</span>;
        return (
          <React.Fragment key={p.id + i}>
            {i > 0 && <Icon name="chevron_right" size={12} className="mx-0.5 opacity-60" aria-hidden />}
            {last && locationId && linkTo ? (
              <Link to={linkTo(locationId)} className="hover:underline">
                {el}
              </Link>
            ) : (
              el
            )}
          </React.Fragment>
        );
      })}
    </span>
  );
}

// ---------- AsyncState: Loading (skeleton) / Error (ErrorState; 403 → Akses ditolak) / Empty (+aksi) ----------
export interface AsyncEmpty { title?: string; description?: string; action?: React.ReactNode; icon?: string; /** @deprecated */ message?: string; /** @deprecated */ cta?: React.ReactNode }
export function AsyncState<T>({ query, children, empty, emptyFilter, isFiltered, skeleton }: { query: { isLoading: boolean; isError: boolean; error?: unknown; refetch: () => unknown; data?: T }; children: (data: T) => React.ReactNode; empty?: AsyncEmpty; emptyFilter?: string; isFiltered?: boolean; skeleton?: React.ReactNode }) {
  const { t } = useTranslation();
  if (query.isLoading) return <>{skeleton ?? <ListSkeleton />}</>;
  if (query.isError) return <QueryErrorState error={query.error} onRetry={() => query.refetch()} />;
  const data = query.data as T;
  const isEmpty = Array.isArray(data) ? data.length === 0 : data === null || data === undefined;
  if (isEmpty) {
    if (isFiltered) return <EmptyState compact title={emptyFilter ?? t("state.empty_filter")} description={t("state.empty_filter_desc")} />;
    return <EmptyState icon={empty?.icon} title={empty?.title ?? empty?.message ?? t("empty.generic")} description={empty?.description} action={empty?.action ?? empty?.cta} />;
  }
  return <>{children(data)}</>;
}

// Komponen state dipindah ke ./states (PRD P0 §23); diekspor ulang agar impor lama tetap berlaku.
export { EmptyState, ErrorState, QueryErrorState, ForbiddenState, NotFoundState, ListSkeleton, DetailSkeleton, TableSkeleton, CardSkeleton, FormSkeleton, KpiSkeleton, PageSkeleton } from "./states";

export function OfflineBanner() {
  const [online, setOnline] = useState(typeof navigator === "undefined" ? true : navigator.onLine);
  const { t } = useTranslation();
  React.useEffect(() => {
    const on = () => setOnline(true);
    const off = () => setOnline(false);
    window.addEventListener("online", on);
    window.addEventListener("offline", off);
    return () => {
      window.removeEventListener("online", on);
      window.removeEventListener("offline", off);
    };
  }, []);
  if (online) return null;
  return (
    <div className="flex items-center gap-2 bg-warning-soft px-4 py-2 text-sm text-warning-text md:px-8">
      <Icon name="wifi_off" size={16} /> {t("state.offline")}
    </div>
  );
}

// ---------- Relative time dengan tooltip absolut (DS §6) ----------
export function RelativeTime({ value, className }: { value?: string | null; className?: string }) {
  if (!value) return <span className={className}>—</span>;
  return (
    <Tooltip content={fmtDateTime(value)}>
      <span className={className}>{fmtRelative(value)}</span>
    </Tooltip>
  );
}

// ---------- SLA progress (DS §5.2): Info < 75%, Warning ≥ 75%, Critical setelah breach ----------
export function SLAProgress({ sla }: { sla?: SLAInfo | null }) {
  if (!sla?.resolution_due_at) return <span className="text-sm text-muted-foreground">Tanpa SLA</span>;
  const pct = Math.min(100, Math.max(0, sla.elapsed_pct ?? 0));
  const breached = !!sla.sla_breached_at || (sla.remaining_minutes ?? 0) < 0;
  const risk = !!sla.sla_risk_at || pct >= 75;
  const tone = breached ? "bg-critical" : risk ? "bg-warning" : "bg-info";
  const label = sla.resolved_at ? "Selesai dalam SLA" : breached ? `Breach ${fmtMinutes(Math.abs(sla.remaining_minutes ?? 0))} lalu` : `Sisa ${fmtMinutes(sla.remaining_minutes)}`;
  return (
    <div>
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <span>{label}</span>
        <span className="tnum">Due {fmtDateTime(sla.resolution_due_at)}</span>
      </div>
      <div className="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
        <div className={cn("h-full", tone)} style={{ width: `${sla.resolved_at ? 100 : pct}%` }} />
      </div>
    </div>
  );
}

// ---------- ReasonDialog: cancel / reopen / hold / resolve (reason wajib, TAD §5.8) ----------
export function ReasonDialog({ open, onOpenChange, title, label = "Alasan", confirmLabel = "Lanjutkan", onConfirm, loading, destructive, description }: { open: boolean; onOpenChange: (o: boolean) => void; title: string; label?: string; confirmLabel?: string; onConfirm: (reason: string) => void; loading?: boolean; destructive?: boolean; description?: string }) {
  const [reason, setReason] = useState("");
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={(o) => { onOpenChange(o); if (!o) setReason(""); }}>
      <DialogContent title={title} description={description}>
        <Field label={label} required>
          <Textarea value={reason} onChange={(e) => setReason(e.target.value)} autoFocus rows={3} />
        </Field>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>{t("action.discard")}</Button>
          <Button variant={destructive ? "destructive" : "primary"} disabled={!reason.trim()} loading={loading} onClick={() => onConfirm(reason.trim())}>
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Toast (Success 4 detik dengan tautan ke object — DS §5.5) ----------
interface ToastItem { id: number; variant: "success" | "critical" | "info" | "warning"; message: string; link?: { to: string; label: string } }
const ToastCtx = createContext<{ push: (t: Omit<ToastItem, "id">) => void } | null>(null);
export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const push = useCallback((t: Omit<ToastItem, "id">) => {
    const id = Date.now() + Math.random();
    setItems((s) => [...s, { ...t, id }]);
    setTimeout(() => setItems((s) => s.filter((x) => x.id !== id)), t.variant === "critical" ? 8000 : 4000);
  }, []);
  const value = useMemo(() => ({ push }), [push]);
  return (
    <ToastCtx.Provider value={value}>
      {children}
      <div className="pointer-events-none fixed inset-x-4 bottom-[calc(88px+env(safe-area-inset-bottom))] z-[1200] flex flex-col gap-2 md:inset-x-auto md:bottom-4 md:right-4 md:w-[360px]" aria-live="polite">
        {items.map((it) => (
          <div key={it.id} className={cn("pointer-events-auto flex items-start gap-3 rounded-lg border px-4 py-3 text-sm shadow-popover", it.variant === "success" && "border-success/30 bg-success-soft text-success-text", it.variant === "critical" && "border-critical/30 bg-critical-soft text-critical-text", it.variant === "warning" && "border-warning/30 bg-warning-soft text-warning-text", it.variant === "info" && "border-info/30 bg-info-soft text-info-text")}>
            {it.variant === "critical" && <Icon name="error" size={16} className="mt-0.5 shrink-0" />}
            <div className="flex-1">
              {it.message}
              {it.link && (
                <Link to={it.link.to} className="ml-2 font-semibold underline">
                  {it.link.label}
                </Link>
              )}
            </div>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  );
}
export function useToast() {
  const c = useContext(ToastCtx);
  if (!c) throw new Error("useToast di luar ToastProvider");
  const { t } = useTranslation();
  return {
    success: (message: string, link?: ToastItem["link"]) => c.push({ variant: "success", message, link }),
    error: (err: unknown) => c.push({ variant: "critical", message: (err as { message?: string })?.message || t("state.error") }),
    info: (message: string) => c.push({ variant: "info", message }),
    warning: (message: string) => c.push({ variant: "warning", message }),
    /** Konfirmasi standar dari katalog (lib/messages): `toast.action("created", "Work Order WO-1")`. */
    action: (action: ToastAction, entity?: string | null, link?: ToastItem["link"]) => c.push({ variant: "success", message: toastMessage(action, entity), link }),
    /** Konfirmasi untuk nama aksi transisi server (start/complete/close/…); aksi tak dikenal → "<Label> berhasil". */
    transition: (action: string, entity?: string | null) => {
      const kind = TRANSITION_TOAST[action] ?? (isToastAction(action) ? action : null);
      c.push({ variant: "success", message: kind ? toastMessage(kind, entity) : t("toast.transition_generic", { action: t(`action.${action}`, { defaultValue: action }) }) });
    },
    /** Error standar: "<Entity> gagal <kata kerja>. <detail problem+json>". */
    failed: (action: ToastAction, err: unknown, entity?: string | null) => {
      const e = err as { problem?: { detail?: string; title?: string }; message?: string };
      c.push({ variant: "critical", message: toastFailure(action, entity, e?.problem?.detail ?? e?.problem?.title ?? e?.message) });
    },
  };
}

export function KeyValue({ items, className }: { items: { label: string; value: React.ReactNode }[]; className?: string }) {
  return (
    <dl className={cn("grid grid-cols-[minmax(110px,auto)_1fr] gap-x-4 gap-y-2 text-sm", className)}>
      {items.map((it) => (
        <React.Fragment key={it.label}>
          <dt className="text-muted-foreground">{it.label}</dt>
          <dd className="min-w-0 break-words text-foreground">{it.value ?? "—"}</dd>
        </React.Fragment>
      ))}
    </dl>
  );
}
