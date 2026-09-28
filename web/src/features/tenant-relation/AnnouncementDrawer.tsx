// Detail pengumuman (PRD P3 v2.1 P3-ANN-01..06): pratinjau isi & gambar, kategori/tingkat, sasaran, jadwal/kedaluwarsa, aksi dari
// allowed_actions (edit · publikasikan · jadwalkan · batalkan jadwal · arsipkan) dan pelacakan baca & konfirmasi
// (GET /announcements/{id}/reads — jumlah penerima, dibaca, dikonfirmasi, serta siapa yang belum membaca).
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { Alert, Badge, Button, Dialog, DialogContent, DialogFooter } from "@/components/ui/primitives";
import { AsyncState, FormSkeleton, KeyValue, QueryErrorState, RelativeTime, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { useAction, useOne } from "@/api/hooks";
import { api } from "@/lib/api";
import { fmtDateTime, fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Announcement, AnnouncementReads } from "./types";
import { ANNOUNCEMENT_AUDIENCES, ANNOUNCEMENT_CATEGORIES, ANNOUNCEMENT_CATEGORY_ICON, ANNOUNCEMENT_SEVERITIES, SEVERITY_TONE, labelOf } from "./labels";
import { AnnouncementImage, TargetSummary } from "./AnnouncementForm";
import { useNow } from "./hooks";

export type AnnAction = "publish" | "schedule" | "unschedule" | "archive";
const ACTION_LABEL: Record<AnnAction, string> = { publish: "Publikasikan sekarang", schedule: "Jadwalkan", unschedule: "Batalkan jadwal", archive: "Arsipkan" };

/** Badge kategori (+ tingkat untuk Alert) dan penanda konfirmasi baca. */
export function AnnouncementBadges({ a }: { a: Pick<Announcement, "category" | "severity" | "requires_ack" | "importance"> }) {
  return (
    <span className="inline-flex flex-wrap items-center gap-1">
      <Badge><Icon name={ANNOUNCEMENT_CATEGORY_ICON[a.category] ?? "campaign"} size={12} aria-hidden />{labelOf(ANNOUNCEMENT_CATEGORIES, a.category)}</Badge>
      {a.category === "alert" && <Badge tone={SEVERITY_TONE[a.severity] ?? "info"}>{labelOf(ANNOUNCEMENT_SEVERITIES, a.severity)}</Badge>}
      {a.importance === "important" && <Badge tone="warning"><Icon name="priority_high" size={12} aria-hidden />Penting</Badge>}
      {a.requires_ack && <Badge tone="info"><Icon name="task_alt" size={12} aria-hidden />Wajib konfirmasi</Badge>}
    </span>
  );
}

export function AnnouncementDrawer({ id, onClose, onEdit }: { id: string; onClose: () => void; onEdit: (a: Announcement) => void }) {
  const q = useOne<Announcement>("announcements", id);
  // dialog konfirmasi dirender di dalam drawer (Modal DS tidak di-portal — di luar drawer ia tertutup drawer)
  const [pending, setPending] = useState<AnnAction | null>(null);
  const now = useNow(30_000);
  const a = q.data;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={a ? a.title : "Announcement"} description={a ? `${a.property_name ?? "Seluruh organisasi"} · audiens ${labelOf(ANNOUNCEMENT_AUDIENCES, a.audience)}` : undefined}>
        <AsyncState query={q} skeleton={<FormSkeleton fields={6} />}>
          {(a) => {
            const expired = !!a.expires_at && new Date(a.expires_at).getTime() < now;
            const canSchedule = a.allowed_actions.includes("schedule");
            const futurePublish = !!a.publish_at && new Date(a.publish_at).getTime() > now;
            return (
              <div className="space-y-5">
                <div className="flex flex-wrap items-center gap-2">
                  <StatusBadge objectType="announcement" status={a.status} />
                  <AnnouncementBadges a={a} />
                  {expired && <Badge tone="neutral"><Icon name="timer_off" size={12} aria-hidden />Kedaluwarsa</Badge>}
                </div>
                {a.status === "scheduled" && a.publish_at && <Alert variant="info" title={`Terbit otomatis ${fmtDateTime(a.publish_at)}`}>Tenant sasaran menerima notifikasi saat pengumuman terbit.</Alert>}
                {a.status === "draft" && <Alert variant="info">Draft belum terlihat tenant. Publikasikan sekarang atau atur jadwal publish lalu jadwalkan.</Alert>}
                <article className="space-y-2 rounded-[var(--radius-lg)] border border-border p-4">
                  {a.image_attachment_id && <AnnouncementImage attachmentId={a.image_attachment_id} />}
                  <h3 className="text-h3 font-bold text-on-surface">{a.title}</h3>
                  {a.excerpt && <p className="text-sm font-medium text-on-surface-variant">{a.excerpt}</p>}
                  <p className="whitespace-pre-line text-body">{a.body}</p>
                </article>
                <KeyValue items={[
                  { label: "Lingkup", value: a.property_name ?? "Seluruh organisasi" },
                  { label: "Audiens", value: labelOf(ANNOUNCEMENT_AUDIENCES, a.audience) },
                  { label: "Sasaran", value: <TargetSummary targets={a.targets} /> },
                  { label: "Jadwal publish", value: a.publish_at ? fmtDateTime(a.publish_at) : "Manual" },
                  { label: "Terbit", value: a.published_at ? <>{fmtDateTime(a.published_at)} <span className="text-on-surface-variant">(<RelativeTime value={a.published_at} />)</span></> : "—" },
                  { label: "Kedaluwarsa", value: a.expires_at ? fmtDateTime(a.expires_at) : "Tidak ada" },
                  { label: "Dibuat", value: <>{fmtDateTime(a.created_at)}{a.created_by_name ? ` · ${a.created_by_name}` : ""}</> },
                ]} />
                {(a.status === "published" || a.status === "archived") && (a.audience === "tenant" || a.audience === "all") && <ReadsSection a={a} />}
                <DialogFooter className="flex-wrap">
                  {a.allowed_actions.includes("archive") && <Button variant="ghost" icon="archive" className="mr-auto" onClick={() => setPending("archive")}>Arsipkan</Button>}
                  {a.allowed_actions.includes("update") && <Button variant="secondary" icon="edit" onClick={() => onEdit(a)}>Edit</Button>}
                  {a.allowed_actions.includes("unschedule") && <Button variant="secondary" icon="event_busy" onClick={() => setPending("unschedule")}>Batalkan jadwal</Button>}
                  {canSchedule && futurePublish && <Button variant="secondary" icon="schedule" onClick={() => setPending("schedule")}>Jadwalkan</Button>}
                  {a.allowed_actions.includes("publish") && <Button icon="publish" onClick={() => setPending("publish")}>Publikasikan sekarang</Button>}
                </DialogFooter>
                {canSchedule && !futurePublish && <p className="-mt-3 text-right text-xs text-on-surface-variant">Untuk menjadwalkan, isi Jadwal publish di masa depan lewat Edit.</p>}
                {pending && <AnnouncementActionDialog a={a} action={pending} onClose={() => setPending(null)} />}
              </div>
            );
          }}
        </AsyncState>
      </DialogContent>
    </Dialog>
  );
}

function Meter({ label, value, total }: { label: string; value: number; total: number }) {
  const pct = total > 0 ? Math.round((value / total) * 100) : 0;
  return (
    <div>
      <div className="flex items-baseline justify-between gap-2 text-sm">
        <span className="text-on-surface-variant">{label}</span>
        <span><span className="font-bold text-on-surface">{fmtNumber(value)}</span><span className="text-on-surface-variant"> / {fmtNumber(total)} · {pct}%</span></span>
      </div>
      <div className="mt-1 h-2 w-full overflow-hidden rounded-full bg-primary-soft" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={`${label} ${pct}%`}>
        <div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

function ReadsSection({ a }: { a: Announcement }) {
  const [unreadOnly, setUnreadOnly] = useState(true);
  const q = useQuery({ queryKey: ["announcement-reads", a.id, unreadOnly], queryFn: ({ signal }) => api<AnnouncementReads>(`announcements/${a.id}/reads`, { query: { unread: unreadOnly || undefined }, signal }), refetchInterval: 60_000 });
  const d = q.data;
  return (
    <section className="space-y-3" aria-labelledby={`reads-${a.id}`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h4 id={`reads-${a.id}`} className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Pelacakan baca</h4>
        <span className="flex gap-1.5">
          <FilterChip selected={unreadOnly} onClick={() => setUnreadOnly(true)}>Belum membaca</FilterChip>
          <FilterChip selected={!unreadOnly} onClick={() => setUnreadOnly(false)}>Semua penerima</FilterChip>
        </span>
      </div>
      {q.isError ? (
        <QueryErrorState error={q.error} onRetry={() => q.refetch()} compact />
      ) : !d ? (
        <p className="text-sm text-on-surface-variant">Memuat status baca…</p>
      ) : (
        <>
          <div className="space-y-2 rounded-[var(--radius-md)] bg-surface-container-low p-3">
            <Meter label="Dibaca" value={d.read} total={d.recipients} />
            {a.requires_ack && <Meter label="Dikonfirmasi" value={d.acknowledged} total={d.recipients} />}
          </div>
          {d.items.length === 0 ? (
            <p className="text-sm text-on-surface-variant">{unreadOnly ? (d.recipients ? "Semua penerima sudah membaca." : "Belum ada penerima aktif untuk sasaran ini.") : "Belum ada penerima."}</p>
          ) : (
            <ul className="max-h-80 divide-y divide-border overflow-y-auto rounded-[var(--radius-md)] border border-border">
              {d.items.map((r) => (
                <li key={r.user_id} className="flex items-start justify-between gap-3 px-3 py-2 text-sm">
                  <span className="min-w-0">
                    <span className="block truncate font-medium">{r.full_name}</span>
                    <span className="block truncate text-xs text-on-surface-variant">{[r.tenant_name, r.unit_label].filter(Boolean).join(" · ") || "—"}</span>
                  </span>
                  <span className="shrink-0 text-right text-xs">
                    {r.read_at ? <span className="inline-flex items-center gap-1 text-success-text"><Icon name="done_all" size={14} aria-hidden />Dibaca <RelativeTime value={r.read_at} /></span> : <span className="text-on-surface-variant">Belum dibaca</span>}
                    {a.requires_ack && <span className={cn("block", r.acknowledged_at ? "text-success-text" : "text-on-surface-variant")}>{r.acknowledged_at ? <>Dikonfirmasi <RelativeTime value={r.acknowledged_at} /></> : "Belum konfirmasi"}</span>}
                  </span>
                </li>
              ))}
            </ul>
          )}
          {d.recipients >= 1000 && <p className="text-xs text-on-surface-variant">Menampilkan maksimal 1.000 penerima.</p>}
        </>
      )}
    </section>
  );
}

/** Konfirmasi aksi transisi (publish/archive memerlukan konfirmasi; schedule/unschedule langsung). */
export function AnnouncementActionDialog({ a, action, onClose }: { a: Announcement; action: AnnAction; onClose: () => void }) {
  const toast = useToast();
  const act = useAction<void, Announcement>(() => `announcements/${a.id}/${action}`, { body: () => ({}), invalidate: ["list", "one", "announcement-reads", "tr-metrics"] });
  const run = () =>
    act.mutateAsync().then((r) => {
      if (action === "publish") toast.success(`Pengumuman terbit · dikirim ke ${fmtNumber(r.recipients_count ?? 0)} penerima`);
      else if (action === "schedule") toast.success(`Pengumuman dijadwalkan terbit ${fmtDateTime(r.publish_at)}`);
      else if (action === "unschedule") toast.success("Jadwal dibatalkan — pengumuman kembali menjadi draft");
      else toast.action("archived", "Announcement");
      onClose();
    }).catch((e) => toast.failed(action === "archive" ? "archived" : action === "publish" ? "published" : "scheduled", e, "Announcement"));
  const text: Record<AnnAction, string> = {
    publish: `"${a.title}" langsung terbit dan tenant sasaran menerima notifikasi (in-app & push).`,
    schedule: `"${a.title}" akan terbit otomatis ${fmtDateTime(a.publish_at)}.`,
    unschedule: `Jadwal terbit "${a.title}" dibatalkan dan pengumuman kembali menjadi draft.`,
    archive: `"${a.title}" tidak lagi tampil di Tenant App. Pengumuman yang diarsipkan tidak dapat diubah.`,
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`${ACTION_LABEL[action]}?`} description={text[action]}>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button variant={action === "archive" ? "destructive" : "primary"} loading={act.isPending} onClick={run}>{ACTION_LABEL[action]}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
