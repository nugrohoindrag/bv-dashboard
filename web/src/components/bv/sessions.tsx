// Daftar sesi login (My Profile & drawer User) — PRD P0 v2 §24.1.
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { Badge, Button } from "@/components/ui/primitives";
import { RelativeTime } from "./common";
import { deviceLabel } from "@/lib/device";
import { fmtDateTime } from "@/lib/format";
import type { Session } from "@/api/types";

export function SessionList({ sessions, onRevoke, revoking }: { sessions: Session[]; onRevoke?: (s: Session) => void; revoking?: string | null }) {
  const { t } = useTranslation();
  if (!sessions.length) return <p className="py-6 text-center text-sm text-on-surface-variant">{t("profile.no_sessions")}</p>;
  return (
    <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border" data-testid="session-list">
      {sessions.map((s) => (
        <li key={s.id} className="flex flex-wrap items-center gap-3 px-3 py-2.5">
          <Icon name={s.client === "web" ? "language" : "smartphone"} size={20} className="shrink-0 text-on-surface-variant" />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2 text-body font-medium text-on-surface">
              {deviceLabel(s)}
              {s.current && <Badge tone="primary">{t("profile.this_device")}</Badge>}
            </div>
            <div className="text-caption text-on-surface-variant">
              {s.ip ?? "—"} · {t("profile.signed_in")} {fmtDateTime(s.created_at)} · {t("profile.last_active")} <RelativeTime value={s.last_used_at ?? s.created_at} />
            </div>
          </div>
          {onRevoke && !s.current && (
            <Button size="sm" variant="secondary" loading={revoking === s.id} onClick={() => onRevoke(s)}>{t("profile.revoke")}</Button>
          )}
        </li>
      ))}
    </ul>
  );
}
