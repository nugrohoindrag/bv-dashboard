// ActivityTimeline (DS §4.5): {User} {Action} {Object} di-render dari action + payload; ikon per jenis; sumber sebagai chip.
import { Icon } from "@buildingvision/ui";
import { Badge } from "@/components/ui/primitives";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Activity } from "@/api/types";
import { statusDef, type ObjectType } from "@/lib/status-map";

const iconFor: Record<string, string> = { created: "auto_awesome", status_changed: "swap_horiz", assigned: "how_to_reg", unassigned: "how_to_reg", commented: "chat_bubble", attachment_added: "photo_camera", checklist_item_answered: "check_box", checklist_completed: "check_box", checklist_started: "check_box", checkpoint_scanned: "check_box", checkpoint_missed: "cancel", finding_created: "flag", resolved: "check_box", reopened: "refresh", cancelled: "cancel", evidence_recorded_offline: "cloud_upload", late_evidence: "cloud_upload", clock_skew: "cloud_upload", sync_conflict_acknowledged: "merge_type", overdue_flagged: "play_circle" };

function statusLabel(objectType: string, s?: string | null): string {
  if (!s) return "";
  const def = statusDef(objectType as ObjectType, s);
  return def?.label_id ?? s;
}

// Sentence {User} {Action} {Object} (PRD §24, NC §63) — Bahasa Indonesia untuk UI.
export function describeActivity(a: Activity, objectLabel: string): string {
  const p = a.payload ?? {};
  const who = a.actor_name || "System";
  switch (a.action) {
    case "created":
      return `${who} membuat ${objectLabel}`;
    case "status_changed": {
      const to = statusLabel(a.object_type, a.to_value);
      const verb: Record<string, string> = { in_progress: "memulai", completed: "menyelesaikan", closed: "menutup", cancelled: "membatalkan", on_hold: "menunda", assigned: "menugaskan", resolved: "menyelesaikan (resolve)", acknowledged: "menerima", scheduled: "menjadwalkan" };
      const base = `${who} ${verb[a.to_value ?? ""] ?? "mengubah status"} ${objectLabel}`;
      const isReopen = p.action === "reopen";
      return isReopen ? `${who} membuka kembali ${objectLabel}` : verb[a.to_value ?? ""] ? base : `${base} dari ${statusLabel(a.object_type, a.from_value)} ke ${to}`;
    }
    case "assigned":
      return `${who} menugaskan ${objectLabel} ke ${(p.assignee_name as string) || (p.assignee_team_name as string) || "—"}`;
    case "unassigned":
      return `${who} membatalkan penugasan ${objectLabel}`;
    case "commented":
      return `${who} berkomentar`;
    case "attachment_added":
      return `${who} menambahkan ${p.attachment_type === "photo_before" ? "foto Before" : p.attachment_type === "photo_after" ? "foto After" : "foto"}${p.pending_upload ? " (menunggu unggah)" : ""}`;
    case "checklist_item_answered":
      return `${who} menjawab "${p.label}": ${p.value ?? p.number ?? p.text ?? ""}`;
    case "checklist_completed":
      return `${who} menyelesaikan checklist`;
    case "checkpoint_scanned":
      return `${who} scan checkpoint ${p.checkpoint_name ?? ""}`;
    case "checkpoint_missed":
      return `Checkpoint ${p.checkpoint_name ?? ""} terlewat${p.auto ? " (otomatis)" : ""}`;
    case "finding_created":
      return `${who} membuat Finding ${p.finding_number ?? ""}: ${p.title ?? ""}`;
    case "resolved":
      return `${who} menyelesaikan${p.via ? " melalui " + p.via : ""}${p.resolution ? ": " + p.resolution : ""}`;
    case "priority_changed":
      return `${who} mengubah prioritas dari ${a.from_value} ke ${a.to_value}`;
    case "due_changed":
      return `${who} mengubah due date`;
    case "updated":
      return `${who} memperbarui ${objectLabel}`;
    case "linked":
      return `${who} menautkan ke ${(p.to_type ?? p.from_type ?? "") as string}`;
    case "evidence_recorded_offline":
      return `${who} — evidence offline tersimpan (${p.action}) · sync ditolak: ${p.reason_code}`;
    case "late_evidence":
      return `${who} — evidence terlambat (object sudah terminal)`;
    case "clock_skew":
      return `Jam device menyimpang (client_time ${p.client_time ? fmtDateTime(p.client_time as string) : "?"})`;
    case "overdue_flagged":
      return `${objectLabel} melewati due time (Overdue)`;
    case "sla_risk_flagged":
      return `${objectLabel} mendekati batas SLA (SLA Risk)`;
    case "sla_breached":
      return `${objectLabel} melewati SLA (Breach)`;
    case "escalated":
      return `${who} mengeskalasi`;
    case "sync_conflict_acknowledged":
      return `${who} meninjau sync conflict`;
    default:
      return `${who} ${a.action.replace(/_/g, " ")}${a.to_value ? " → " + a.to_value : ""}`;
  }
}

export function ActivityTimeline({ items, objectLabel, attachmentsById }: { items: Activity[]; objectLabel: string; attachmentsById?: Record<string, { thumb_url?: string; url?: string }> }) {
  if (!items.length) return <p className="py-6 text-center text-sm text-muted-foreground">Belum ada aktivitas.</p>;
  return (
    <ol className="relative ml-2 border-l border-border pl-5">
      {items.map((a) => {
        const icon = iconFor[a.action] ?? "swap_horiz";
        const attId = a.payload?.attachment_id as string | undefined;
        const att = attId && attachmentsById?.[attId];
        const isSync = a.source === "sync" || a.action.startsWith("evidence_") || a.action === "late_evidence";
        return (
          <li key={a.id} className="relative pb-5 last:pb-0">
            <span className={cn("absolute -left-[29px] flex h-6 w-6 items-center justify-center rounded-full border border-border bg-surface text-on-surface-variant", isSync && "border-warning text-warning")}>
              <Icon name={icon} size={14} aria-hidden />
            </span>
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
              <time className="tnum text-xs text-muted-foreground" dateTime={a.occurred_at}>{fmtDateTime(a.occurred_at)}</time>
              <span className="text-body">{describeActivity(a, objectLabel)}</span>
              <Badge>{a.source}{a.payload?.gps_status === "captured" ? " · GPS ✓" : ""}</Badge>
            </div>
            {a.action === "commented" && a.payload?.excerpt ? <p className="mt-1 rounded-[var(--radius-sm)] bg-surface-container px-3 py-2 text-sm">{String(a.payload.excerpt)}</p> : null}
            {a.payload?.reason ? <p className="mt-1 text-sm text-muted-foreground">Alasan: {String(a.payload.reason)}</p> : null}
            {att && (att.thumb_url || att.url) && (
              <a href={att.url} target="_blank" rel="noreferrer" className="mt-2 inline-block">
                <img src={att.thumb_url ?? att.url} alt="" className="h-16 w-16 rounded-md border border-border object-cover" />
              </a>
            )}
          </li>
        );
      })}
    </ol>
  );
}
