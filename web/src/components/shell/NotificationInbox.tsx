// NotificationInbox (DS §4): popover dengan tab Belum dibaca / Semua; item: severity dot, judul, object, waktu relatif, deep link
import { useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Button, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { RelativeTime } from "@/components/bv/common";
import { useNotifications, useAction } from "@/api/hooks";
import { cn } from "@/lib/utils";
import type { Notification } from "@/api/types";

function Item({ n, onRead }: { n: Notification; onRead: (id: string) => void }) {
  const dot = { critical: "bg-critical", warning: "bg-warning", success: "bg-success", info: "bg-info" }[n.severity];
  const body = (
    <div className={cn("flex items-start gap-3 rounded-md px-3 py-2 hover:bg-muted", !n.read_at && "bg-brand-50/60")}>
      <span className={cn("mt-1.5 h-2 w-2 shrink-0 rounded-full", dot)} aria-hidden />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <span className={cn("text-body", !n.read_at && "font-semibold")}>{n.title}</span>
          <RelativeTime value={n.created_at} className="shrink-0 text-xs text-muted-foreground" />
        </div>
        <div className="whitespace-pre-line text-sm text-muted-foreground">{n.body}</div>
      </div>
    </div>
  );
  return n.deep_link ? (
    <Link to={n.deep_link} onClick={() => !n.read_at && onRead(n.id)} className="block">
      {body}
    </Link>
  ) : (
    <div onClick={() => !n.read_at && onRead(n.id)}>{body}</div>
  );
}

export function NotificationInbox({ full }: { full?: boolean }) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"unread" | "all">("unread");
  const unread = useNotifications(true);
  const all = useNotifications(false);
  const markRead = useAction<{ id: string }>((i) => `notifications/${i.id}/read`, { invalidate: ["notifications"], body: () => ({}) });
  const markAll = useAction<void>(() => "notifications/read-all", { invalidate: ["notifications"], body: () => ({}) });
  const list = (tab === "unread" ? unread : all).data?.data ?? [];
  return (
    <div className={cn(full ? "" : "max-h-[520px]", "flex flex-col")}>
      <div className="flex items-center justify-between px-3 pt-2">
        <span className="text-h3 font-semibold">Inbox</span>
        <Button variant="link" size="sm" onClick={() => markAll.mutate()}>{t("action.mark_all_read")}</Button>
      </div>
      <Tabs value={tab} onValueChange={(v) => setTab(v as "unread" | "all")} className="px-3">
        <TabsList>
          <TabsTrigger value="unread">{t("label.unread")} {unread.data?.unread_count ? `(${unread.data.unread_count})` : ""}</TabsTrigger>
          <TabsTrigger value="all">{t("label.all")}</TabsTrigger>
        </TabsList>
        <TabsContent value={tab} className="pt-2">
          <div className={cn("space-y-0.5 overflow-y-auto", !full && "max-h-[380px]")}>
            {list.length === 0 && <p className="py-8 text-center text-sm text-muted-foreground">{t("empty.notifications")}</p>}
            {list.map((n) => (
              <Item key={n.id} n={n} onRead={(id) => markRead.mutate({ id })} />
            ))}
          </div>
        </TabsContent>
      </Tabs>
      {!full && (
        <div className="border-t border-border px-3 py-2 text-sm">
          <Link to="/settings/notifications" className="text-brand-600 hover:underline">Buka inbox penuh →</Link>
        </div>
      )}
    </div>
  );
}
