// Global UX States (PRD P0 §23): Empty (apa · mengapa · aksi berikutnya), Error (apa yang terjadi · dampak · pemulihan),
// Loading (page · table · card · form · KPI), Forbidden 403 & Not Found 404. Semua dari token (DS Guideline §2.1).
import * as React from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon, SkeletonChart, SkeletonKPI } from "@buildingvision/ui";
import { Button, Skeleton } from "@/components/ui/primitives";
import { problemOf } from "@/lib/problem";
import { cn } from "@/lib/utils";

function StateIcon({ icon, tone = "neutral" }: { icon: React.ReactNode | string; tone?: "neutral" | "error" | "warning" | "primary" }) {
  const box = { neutral: "bg-surface-container-high text-on-surface-variant", error: "bg-error-container text-on-error-container", warning: "bg-warning-container text-on-warning-container", primary: "bg-primary-container text-on-primary-container" }[tone];
  return <div className={cn("flex h-14 w-14 shrink-0 items-center justify-center rounded-full", box)} aria-hidden>{typeof icon === "string" ? <Icon name={icon} size={28} /> : icon}</div>;
}

// ---------- Empty State ----------
export interface EmptyStateProps {
  /** Apa yang kosong ("Belum ada Work Order"). */
  title?: React.ReactNode;
  /** Mengapa kosong / apa artinya. */
  description?: React.ReactNode;
  /** Aksi berikutnya (tombol/tautan). */
  action?: React.ReactNode;
  icon?: React.ReactNode | string;
  compact?: boolean;
  className?: string;
  /** @deprecated pakai `title` (+ `description`). */
  message?: string;
  /** @deprecated pakai `action`. */
  cta?: React.ReactNode;
}
export function EmptyState({ title, description, action, icon, compact, className, message, cta }: EmptyStateProps) {
  const heading = title ?? message;
  const act = action ?? cta;
  return (
    <div role="status" className={cn("flex flex-col items-center justify-center gap-2 px-4 text-center", compact ? "py-8" : "py-12", className)}>
      {!compact && <StateIcon icon={icon ?? "inbox"} />}
      {heading && <p className={cn("text-on-surface", compact ? "text-body font-semibold" : "mt-1 text-h3 font-bold")}>{heading}</p>}
      {description && <p className="max-w-md text-sm text-on-surface-variant">{description}</p>}
      {act && <div className="mt-2 flex flex-wrap items-center justify-center gap-2">{act}</div>}
    </div>
  );
}

// ---------- Error State ----------
export interface ErrorStateProps {
  /** Apa yang terjadi. */
  title?: React.ReactNode;
  /** Detail teknis/penyebab (mis. detail problem+json). */
  description?: React.ReactNode;
  /** Dampak bagi pengguna. */
  impact?: React.ReactNode;
  /** Aksi pemulihan utama (Coba lagi). */
  onRetry?: () => void;
  retryLabel?: string;
  /** Aksi pemulihan tambahan (tautan kembali, dsb.). */
  actions?: React.ReactNode;
  icon?: string;
  tone?: "error" | "warning";
  requestId?: string;
  compact?: boolean;
  className?: string;
}
export function ErrorState({ title, description, impact, onRetry, retryLabel, actions, icon = "error", tone = "error", requestId, compact, className }: ErrorStateProps) {
  const { t } = useTranslation();
  return (
    <div role="alert" className={cn("flex flex-col items-center justify-center gap-2 rounded-[var(--radius-xl)] border px-4 text-center", tone === "error" ? "border-error-container" : "border-warning-container", compact ? "py-6" : "py-12", className)}>
      {!compact && <StateIcon icon={icon} tone={tone} />}
      <p className={cn("font-bold text-on-surface", compact ? "text-body" : "mt-1 text-h3")}>{title ?? t("state.error_title")}</p>
      {description && <p className="max-w-lg text-sm text-on-surface-variant">{description}</p>}
      {impact !== null && <p className="max-w-lg text-sm text-on-surface-variant"><span className="font-semibold text-on-surface">{t("state.impact")}:</span> {impact ?? t("state.error_impact")}</p>}
      {(onRetry || actions) && (
        <div className="mt-2 flex flex-wrap items-center justify-center gap-2">
          {onRetry && <Button size="sm" icon="refresh" onClick={onRetry}>{retryLabel ?? t("action.retry")}</Button>}
          {actions}
        </div>
      )}
      {requestId && <p className="text-caption text-on-surface-variant">Request ID: <span className="font-mono">{requestId}</span></p>}
    </div>
  );
}

// ---------- 403 / 404 ----------
function HomeButton() {
  const { t } = useTranslation();
  const nav = useNavigate();
  return <Button size="sm" icon="space_dashboard" onClick={() => nav("/")}>{t("action.back_home")}</Button>;
}
function LoginButton() {
  const { t } = useTranslation();
  const nav = useNavigate();
  return <Button size="sm" variant="secondary" onClick={() => nav("/login")}>{t("auth.login")}</Button>;
}
export function ForbiddenState({ compact, className, action, detail }: { compact?: boolean; className?: string; action?: React.ReactNode; detail?: React.ReactNode }) {
  const { t } = useTranslation();
  return (
    <div role="alert" data-testid="forbidden-state" className={cn("flex flex-col items-center justify-center gap-2 px-4 text-center", compact ? "py-8" : "py-16", className)}>
      <StateIcon icon="lock" tone="warning" />
      <p className={cn("mt-1 font-bold text-on-surface", compact ? "text-h3" : "text-h1")}>{t("state.forbidden_title")}</p>
      <p className="max-w-md text-sm text-on-surface-variant">{detail ?? t("state.forbidden_desc")}</p>
      <p className="max-w-md text-sm text-on-surface-variant"><span className="font-semibold text-on-surface">{t("state.impact")}:</span> {t("state.forbidden_impact")}</p>
      <div className="mt-2 flex flex-wrap items-center justify-center gap-2">
        {action ?? <HomeButton />}
      </div>
    </div>
  );
}

export function NotFoundState({ compact, className, title, description, action }: { compact?: boolean; className?: string; title?: React.ReactNode; description?: React.ReactNode; action?: React.ReactNode }) {
  const { t } = useTranslation();
  return (
    <div role="status" data-testid="not-found-state" className={cn("flex flex-col items-center justify-center gap-2 px-4 text-center", compact ? "py-8" : "py-16", className)}>
      <StateIcon icon="explore" />
      <p className={cn("mt-1 font-bold text-on-surface", compact ? "text-h3" : "text-h1")}>{title ?? t("state.not_found_title")}</p>
      <p className="max-w-md text-sm text-on-surface-variant">{description ?? t("state.not_found_desc")}</p>
      <div className="mt-2 flex flex-wrap items-center justify-center gap-2">
        {action ?? <HomeButton />}
      </div>
    </div>
  );
}

/** Error query → state yang tepat: 403 → Akses ditolak, 404 → tidak ditemukan, offline, 5xx, lainnya. */
export function QueryErrorState({ error, onRetry, compact, className }: { error: unknown; onRetry?: () => void; compact?: boolean; className?: string }) {
  const { t } = useTranslation();
  const p = problemOf(error);
  const online = typeof navigator === "undefined" ? true : navigator.onLine;
  if (p.status === 403) return <ForbiddenState compact className={className} detail={p.detail || undefined} />;
  if (p.status === 404) return <NotFoundState compact className={className} title={t("state.not_found_data")} description={p.detail || t("state.not_found_data_desc")} />;
  if (!online) return <ErrorState tone="warning" icon="wifi_off" title={t("state.offline_title")} impact={t("state.offline_impact")} onRetry={onRetry} compact={compact} className={className} />;
  if (p.status === 401) return <ErrorState icon="lock" title={t("state.session_title")} impact={t("state.session_impact")} actions={<LoginButton />} compact={compact} className={className} />;
  const server = (p.status ?? 0) >= 500;
  return (
    <ErrorState
      icon={server ? "dns" : "error"}
      title={server ? t("state.server_title") : t("state.error_title")}
      description={p.detail || p.message || undefined}
      impact={server ? t("state.server_impact") : undefined}
      onRetry={onRetry}
      requestId={p.request_id}
      compact={compact}
      className={className}
    />
  );
}

// ---------- Loading: skeleton page / table / card / form / KPI ----------
export function ListSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="space-y-2 py-2" aria-busy>
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex items-center gap-3">
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-4 flex-1" />
          <Skeleton className="hidden h-4 w-24 sm:block" />
          <Skeleton className="h-5 w-20 rounded-full" />
        </div>
      ))}
    </div>
  );
}

/** Area tabel: bar header (solid, seperti .bv-table) + baris; di layar sempit tampil sebagai kartu. */
export function TableSkeleton({ rows = 6, columns = 5 }: { rows?: number; columns?: number }) {
  return (
    <div aria-busy data-testid="table-skeleton">
      <div className="hidden sm:block">
        <Skeleton className="h-10 w-full rounded-b-none rounded-t-[var(--radius-md)]" />
        {Array.from({ length: rows }).map((_, r) => (
          <div key={r} className="flex items-center gap-4 border-b border-border px-3 py-3">
            {Array.from({ length: columns }).map((__, c) => (
              <Skeleton key={c} className={cn("h-4", c === 1 ? "flex-[2]" : "flex-1")} />
            ))}
          </div>
        ))}
      </div>
      <div className="space-y-2 sm:hidden">
        {Array.from({ length: Math.min(rows, 4) }).map((_, r) => <CardSkeleton key={r} lines={2} />)}
      </div>
    </div>
  );
}

export function CardSkeleton({ lines = 3, className }: { lines?: number; className?: string }) {
  return (
    <div aria-busy className={cn("space-y-3 rounded-[var(--radius-xl)] border border-border bg-surface p-4", className)}>
      <div className="flex items-center justify-between gap-3">
        <Skeleton className="h-4 w-1/2" />
        <Skeleton className="h-5 w-16 rounded-full" />
      </div>
      {Array.from({ length: lines }).map((_, i) => <Skeleton key={i} className={cn("h-3", i % 2 ? "w-2/3" : "w-full")} />)}
    </div>
  );
}

export function FormSkeleton({ fields = 4, className }: { fields?: number; className?: string }) {
  return (
    <div aria-busy data-testid="form-skeleton" className={cn("space-y-4", className)}>
      {Array.from({ length: fields }).map((_, i) => (
        <div key={i} className="space-y-1.5">
          <Skeleton className="h-3 w-28" />
          <Skeleton className="h-10 w-full rounded-[var(--radius-input)]" />
        </div>
      ))}
      <div className="flex justify-end gap-2 pt-2">
        <Skeleton className="h-9 w-20" />
        <Skeleton className="h-9 w-24" />
      </div>
    </div>
  );
}

/** Kartu metrik dashboard (reuse SkeletonKPI DS). */
export function KpiSkeleton({ count = 4 }: { count?: number }) {
  return (
    <div aria-busy className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
      {Array.from({ length: count }).map((_, i) => <SkeletonKPI key={i} />)}
    </div>
  );
}
export const ChartSkeleton = SkeletonChart;

/** Halaman daftar: header + filter + tabel. */
export function PageSkeleton() {
  return (
    <div aria-busy data-testid="page-skeleton" className="space-y-5">
      <div className="space-y-2">
        <Skeleton className="h-3 w-32" />
        <Skeleton className="h-7 w-64 max-w-full" />
      </div>
      <div className="flex flex-wrap gap-2">
        <Skeleton className="h-10 w-64 max-w-full" />
        <Skeleton className="h-10 w-40" />
        <Skeleton className="h-10 w-36" />
      </div>
      <TableSkeleton />
    </div>
  );
}

/** Halaman detail: kolom utama + panel samping; menumpuk di bawah lg. */
export function DetailSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-6 lg:grid-cols-12" aria-busy data-testid="detail-skeleton">
      <div className="space-y-4 lg:col-span-8">
        <Skeleton className="h-8 w-2/3" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
      <div className="space-y-3 lg:col-span-4">
        <Skeleton className="h-64 w-full" />
      </div>
    </div>
  );
}
