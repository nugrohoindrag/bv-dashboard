// NotificationInbox (DS §4): popover dengan tab Belum dibaca / Semua; item: severity dot, judul, object, waktu relatif, deep link
import { useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { Button } from "@/components/ui/primitives";
import { RelativeTime } from "@/components/bv/common";
import { useNotifications, useAction } from "@/api/hooks";
import { cn } from "@/lib/utils";
import type { Notification } from "@/api/types";

function Item({ n, onRead, onNavigate }: { n: Notification; onRead: (id: string) => void; onNavigate?: () => void }) {
  const dot = { critical: "bg-error", warning: "bg-warning", success: "bg-success", info: "bg-info" }[n.severity];
  const body = (
    <div className={cn("flex items-start gap-3 rounded-[var(--radius-md)] px-3 py-2 hover:bg-surface-container", !n.read_at && "bg-primary-soft")}>
      <span className={cn("mt-1.5 h-2 w-2 shrink-0 rounded-full", dot)} aria-hidden />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <span className={cn("text-body", !n.read_at && "font-bold")}>{n.title}</span>
          <RelativeTime value={n.created_at} className="shrink-0 text-xs text-on-surface-variant" />
        </div>
        <div className="whitespace-pre-line text-sm text-on-surface-variant">{n.body}</div>
      </div>
    </div>
  );
  return n.deep_link ? (
    <Link to={n.deep_link} onClick={() => { if (!n.read_at) onRead(n.id); onNavigate?.(); }} className="block">
      {body}
    </Link>
  ) : (
    <div onClick={() => !n.read_at && onRead(n.id)}>{body}</div>
  );
}

export function NotificationInbox({ full, onNavigate }: { full?: boolean; onNavigate?: () => void }) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"unread" | "all">("unread");
  const unread = useNotifications(true);
  const all = useNotifications(false);
  const markRead = useAction<{ id: string }>((i) => `notifications/${i.id}/read`, { invalidate: ["notifications"], body: () => ({}) });
  const markAll = useAction<void>(() => "notifications/read-all", { invalidate: ["notifications"], body: () => ({}) });
  const list = (tab === "unread" ? unread : all).data?.data ?? [];
  return (
    <div className={cn(full ? "" : "max-h-[520px]", "flex flex-col")}>
      <div className="flex items-center justify-between px-3 pt-3">
        <span className="inline-flex items-center gap-2 text-h3 font-bold"><Icon name="inbox" size={18} className="text-primary" /> Inbox</span>
        <Button variant="link" size="sm" onClick={() => markAll.mutate()}>{t("action.mark_all_read")}</Button>
      </div>
      <div className="flex gap-2 px-3 pt-2">
        <FilterChip selected={tab === "unread"} onClick={() => setTab("unread")}>{t("label.unread")}{unread.data?.unread_count ? ` (${unread.data.unread_count})` : ""}</FilterChip>
        <FilterChip selected={tab === "all"} onClick={() => setTab("all")}>{t("label.all")}</FilterChip>
      </div>
      <div className={cn("mt-2 space-y-0.5 overflow-y-auto px-1", !full && "max-h-[380px]")}>
        {list.length === 0 && <p className="py-8 text-center text-sm text-on-surface-variant">{t("empty.notifications")}</p>}
        {list.map((n) => (
          <Item key={n.id} n={n} onRead={(id) => markRead.mutate({ id })} onNavigate={onNavigate} />
        ))}
      </div>
      {!full && (
        <div className="border-t border-border px-3 py-2 text-sm">
          <Link to="/settings/notifications" onClick={onNavigate} className="font-semibold text-primary hover:underline">Buka inbox penuh →</Link>
        </div>
      )}
    </div>
  );
}
