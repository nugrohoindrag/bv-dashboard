// Attention Required dashboard Finance (PRD P4 v2.1 P4-FIN-01; Roadmap Principle 5/8 exception-driven): item datang di respons
// GET /dashboards/finance (pembayaran menunggu verifikasi, janji bayar ingkar, draft tagihan siap terbit, mutasi bank belum
// cocok, pembacaan meter flagged) — ditampilkan dengan label kategori finance dan deep link yang dinormalkan ke route web.
import { Link } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import { RelativeTime } from "@/components/bv/common";
import type { AttentionItem } from "@/api/types";
import { cn } from "@/lib/utils";
import { FINANCE_ATTENTION, FINANCE_OBJECT_LABEL, attentionLink } from "./dashboard";

export function FinanceAttentionList({ items, total, limit = 10, onSeeAll }: { items: AttentionItem[]; total: number; limit?: number; onSeeAll?: () => void }) {
  if (!items.length) return <p className="py-8 text-center text-sm text-on-surface-variant">Tidak ada yang perlu perhatian di keuangan. Semua terkendali.</p>;
  const shown = items.slice(0, limit);
  return (
    <div className="divide-y divide-border" data-testid="finance-attention">
      {shown.map((it) => {
        const meta = FINANCE_ATTENTION[it.category] ?? { label: it.category, icon: "warning" };
        const to = attentionLink(it.deep_link);
        const critical = it.severity === "critical";
        return (
          <div key={it.category + it.object_id} className="flex items-start gap-3 py-2.5">
            <span aria-label={critical ? "kritis" : "perlu perhatian"} className={cn("mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full", critical ? "bg-error" : "bg-warning")} />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className={cn("inline-flex items-center gap-1 text-xs font-semibold uppercase tracking-wide", critical ? "text-on-error-container" : "text-on-warning-container")}>
                  <Icon name={meta.icon} size={14} aria-hidden />
                  {meta.label}
                </span>
                <Link to={to} className="font-mono text-[13px] font-semibold hover:underline">{it.label}</Link>
              </div>
              <div className="truncate text-body">{it.title}</div>
              <div className="mt-0.5 flex flex-wrap items-center gap-x-3 text-xs text-on-surface-variant">
                <span>{FINANCE_OBJECT_LABEL[it.object_type] ?? it.object_type}</span>
                <span className="tnum"><RelativeTime value={it.since} /></span>
              </div>
            </div>
            <Link to={to} className="inline-flex h-8 shrink-0 items-center rounded-[var(--radius-md)] px-3 text-sm font-semibold text-primary hover:bg-surface-container">Buka</Link>
          </div>
        );
      })}
      {total > shown.length && onSeeAll && (
        <div className="pt-3 text-sm">
          <button type="button" onClick={onSeeAll} className="font-semibold text-primary hover:underline">Lihat semua ({total})</button>
        </div>
      )}
    </div>
  );
}
