// Detail Emergency Alert (PRD P2 v2.1 §6.4; NC §16): status, jenis, lokasi (+ GPS), pelapor, Incident otomatis; aksi besar
// dari allowed_actions (Terima · Tiba/Tangani · Catat tindakan · Selesaikan · Alarm palsu); timeline Security Response;
// kontak darurat property; foto evidence. Diperbarui otomatis tiap 15 detik selama halaman terbuka.
import { Link, useParams } from "react-router-dom";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import type { Tone } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle } from "@/components/ui/primitives";
import { AsyncState, DetailSkeleton, KeyValue, LocationPath, ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { AttachmentGrid, PhotoEvidenceUploader } from "@/components/bv/checklist";
import { MobileActionBar } from "@/components/bv/mobile";
import { useAction, useAttachments } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { EmergencyAlert, EmergencyContact, EmergencyEvent } from "./types";
import { CONTACT_TYPE_ICON, CONTACT_TYPES, EMERGENCY_ACTION_ICON, EMERGENCY_ACTION_LABEL, EMERGENCY_CHANNELS, EMERGENCY_EVENT_ICON, EMERGENCY_REFRESH_MS, EMERGENCY_TYPE_ICON, EMERGENCY_TYPES, GPS_STATUS, emergencyEventText, fmtSeconds, labelOf, mapsUrl, orderedEmergencyActions, secondsSince, telHref } from "./p2";
import { EscalationBadge } from "./shared";
import { StatusBadge } from "@/components/bv/badges";
import { TOUCH, useNow } from "./hooks";

type DialogAction = "note" | "resolve" | "cancel";
const DIALOG: Record<DialogAction, { title: string; label: string; confirm: string; description: string; field: "note" | "resolution" | "reason"; destructive?: boolean }> = {
  note: { title: "Catat tindakan", label: "Tindakan yang diambil", confirm: "Simpan tindakan", description: "Tercatat di timeline Emergency (mis. evakuasi lantai 12 dimulai, APAR dipakai).", field: "note" },
  resolve: { title: "Selesaikan Emergency", label: "Penyelesaian", confirm: "Selesaikan", description: "Ringkasan penanganan; disalin ke tindakan Incident terkait. Status tidak dapat dikembalikan.", field: "resolution" },
  cancel: { title: "Tandai alarm palsu", label: "Alasan pembatalan", confirm: "Batalkan (alarm palsu)", description: "Emergency dibatalkan dan Incident otomatis ikut dibatalkan. Alasan dicatat di timeline & audit.", field: "reason", destructive: true },
};

export default function EmergencyDetailPage() {
  const { id } = useParams();
  const { principal, can } = useAuth();
  const toast = useToast();
  const q = useQuery({
    queryKey: ["one", "emergency-alerts", id],
    enabled: !!id,
    queryFn: ({ signal }) => api<EmergencyAlert>(`emergency-alerts/${id}`, { signal }),
    refetchInterval: EMERGENCY_REFRESH_MS,
    staleTime: 5_000,
  });
  const attachments = useAttachments("emergency_alert", id);
  const [dialog, setDialog] = useState<DialogAction | null>(null);
  const act = useAction<{ action: string; body?: Record<string, unknown> }, EmergencyAlert>((i) => `emergency-alerts/${id}/${i.action}`, { body: (i) => i.body ?? {} });
  return (
    <AsyncState query={q} skeleton={<DetailSkeleton />}>
      {(a) => {
        const actions = orderedEmergencyActions(a.allowed_actions);
        const run = (action: string, body?: Record<string, unknown>) =>
          act
            .mutateAsync({ action, body })
            .then(() => {
              if (action === "respond") toast.success(`${a.alert_number}: security menangani di lokasi`);
              else if (action === "note") toast.action("saved", "Tindakan");
              else toast.transition(action, a.alert_number);
              setDialog(null);
            })
            .catch((e) => toast.failed("updated", e, a.alert_number));
        const click = (action: string) => (action === "note" || action === "resolve" || action === "cancel" ? setDialog(action) : run(action));
        const primary = actions.find((x) => x !== "note" && x !== "cancel") ?? null;
        const canAttach = can("security.emergency_alerts.respond", a.property_id) || (!!principal && a.raised_by === principal.id);
        const typeLabel = a.emergency_type_label || labelOf(EMERGENCY_TYPES, a.emergency_type);
        return (
          <div>
            <PageHeader
              breadcrumb={<><Link to="/security/emergency" className="hover:underline">Emergency</Link> / <span className="font-mono">{a.alert_number}</span></>}
              title={
                <span className="inline-flex flex-wrap items-center gap-2">
                  <span className="flex h-9 w-9 items-center justify-center rounded-full bg-error-container text-on-error-container" aria-hidden><Icon name={EMERGENCY_TYPE_ICON[a.emergency_type] ?? "warning"} size={20} /></span>
                  <span className="font-mono">{a.alert_number}</span> <span className="font-normal text-muted-foreground">·</span> {typeLabel}
                </span>
              }
              badges={<><StatusBadge objectType="emergency_alert" status={a.status} /><EscalationBadge level={a.escalation_level} /></>}
              subtitle={<>Dilaporkan <RelativeTime value={a.raised_at} /> oleh {a.raised_by_name ?? "—"} · {labelOf(EMERGENCY_CHANNELS, a.channel)}{a.incident_id && <> · <Link to={`/operations/incidents/${a.incident_id}`} className="font-mono text-primary hover:underline">{a.incident_number ?? "Incident"}</Link></>}</>}
            />
            <ResponsePanel alert={a} actions={actions} primary={primary} loadingAction={act.isPending ? act.variables?.action : undefined} onAction={click} />
            <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
              <div className="min-w-0 space-y-4 lg:col-span-8">
                <Card>
                  <CardHeader><CardTitle>Informasi</CardTitle></CardHeader>
                  <CardContent className="space-y-4">
                    <p className="whitespace-pre-line text-body">{a.description || <span className="italic text-muted-foreground">Tanpa keterangan.</span>}</p>
                    <KeyValue items={[
                      { label: "Jenis", value: <span className="inline-flex items-center gap-1.5"><Icon name={EMERGENCY_TYPE_ICON[a.emergency_type] ?? "warning"} size={16} className="text-on-surface-variant" aria-hidden />{typeLabel}</span> },
                      { label: "Lokasi", value: <LocationPath pathText={a.location.path_text} locationId={a.location.id} linkTo={(lid) => `/property/locations/${lid}`} /> },
                      { label: "GPS", value: <GpsValue alert={a} /> },
                      { label: "Kanal", value: labelOf(EMERGENCY_CHANNELS, a.channel) },
                      { label: "Waktu lapor", value: <>{fmtDateTime(a.raised_at)}{a.client_raised_at && Math.abs(new Date(a.client_raised_at).getTime() - new Date(a.raised_at).getTime()) > 60_000 && <span className="block text-xs text-on-surface-variant">Ditekan di perangkat {fmtDateTime(a.client_raised_at)} (terkirim setelah online)</span>}</> },
                      { label: "Incident", value: a.incident_id ? <Link to={`/operations/incidents/${a.incident_id}`} className="font-mono text-primary hover:underline">{a.incident_number ?? "Buka Incident"}</Link> : "—" },
                      ...(a.resolution ? [{ label: "Penyelesaian", value: <span className="whitespace-pre-line">{a.resolution}</span> }] : []),
                      ...(a.cancel_reason ? [{ label: "Alasan batal", value: <span className="whitespace-pre-line">{a.cancel_reason}</span> }] : []),
                    ]} />
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader><CardTitle>Timeline respons</CardTitle><span className="text-xs text-on-surface-variant">Diperbarui otomatis tiap 15 detik</span></CardHeader>
                  <CardContent><EmergencyTimeline events={a.timeline ?? []} /></CardContent>
                </Card>
                <Card>
                  <CardHeader><CardTitle>Foto evidence</CardTitle><span className="text-xs text-on-surface-variant">{(attachments.data ?? []).length} foto</span></CardHeader>
                  <CardContent className="space-y-3">
                    {canAttach && <PhotoEvidenceUploader objectType="emergency_alert" objectId={a.id} attachmentType="photo" label="Unggah foto kondisi lokasi" onUploaded={() => attachments.refetch()} />}
                    <AttachmentGrid items={attachments.data ?? []} emptyLabel="Belum ada foto evidence." />
                  </CardContent>
                </Card>
              </div>
              <div className="min-w-0 space-y-4 lg:col-span-4">
                <Card>
                  <CardHeader><CardTitle>Security Response</CardTitle></CardHeader>
                  <CardContent>
                    <KeyValue items={[
                      { label: "Diterima oleh", value: a.acknowledged_by_name ? <>{a.acknowledged_by_name}<span className="block text-xs text-on-surface-variant">{fmtDateTime(a.acknowledged_at)}</span></> : <span className="text-on-error-container">Belum diterima</span> },
                      { label: "Waktu respons", value: a.ack_seconds !== null ? <span className="font-semibold tnum">{fmtSeconds(a.ack_seconds)}</span> : a.active ? <span className="tnum text-on-error-container">menunggu {fmtSeconds(secondsSince(a.raised_at))}</span> : "—" },
                      { label: "Penanggung jawab", value: a.responder_name ?? "—" },
                      { label: "Tiba di lokasi", value: a.responding_at ? fmtDateTime(a.responding_at) : "—" },
                      { label: "Selesai", value: a.resolved_at ? fmtDateTime(a.resolved_at) : a.cancelled_at ? `Dibatalkan ${fmtDateTime(a.cancelled_at)}` : "—" },
                      { label: "Eskalasi", value: a.escalation_level > 0 ? <>Level {a.escalation_level}<span className="block text-xs text-on-surface-variant">{fmtDateTime(a.escalated_at)}</span></> : "—" },
                    ]} />
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader><CardTitle>Kontak Darurat</CardTitle>{can("security.emergency_contacts.manage") && <Link to="/security/emergency/contacts" className="text-sm font-semibold text-primary hover:underline">Kelola</Link>}</CardHeader>
                  <CardContent><ContactsList contacts={a.contacts ?? []} /></CardContent>
                </Card>
              </div>
            </div>
            {/* < md: aksi utama menempel di bawah (PRD P0 §22) */}
            {actions.length > 0 && (
              <MobileActionBar
                primary={primary ? { label: EMERGENCY_ACTION_LABEL[primary], icon: EMERGENCY_ACTION_ICON[primary], onSelect: () => click(primary), loading: act.isPending && act.variables?.action === primary } : null}
                secondary={actions.filter((x) => x !== primary).map((x) => ({ label: EMERGENCY_ACTION_LABEL[x], icon: EMERGENCY_ACTION_ICON[x], onSelect: () => click(x), destructive: x === "cancel" }))}
              />
            )}
            {dialog && (
              <ReasonDialog
                open
                onOpenChange={(o) => !o && setDialog(null)}
                title={`${DIALOG[dialog].title} · ${a.alert_number}`}
                label={DIALOG[dialog].label}
                confirmLabel={DIALOG[dialog].confirm}
                description={DIALOG[dialog].description}
                destructive={DIALOG[dialog].destructive}
                loading={act.isPending}
                onConfirm={(text) => run(dialog, { [DIALOG[dialog].field]: text })}
              />
            )}
          </div>
        );
      }}
    </AsyncState>
  );
}

// ---------- Panel aksi besar (status respons + tombol dari allowed_actions) ----------
function ResponsePanel({ alert: a, actions, primary, loadingAction, onAction }: { alert: EmergencyAlert; actions: string[]; primary: string | null; loadingAction?: string; onAction: (a: string) => void }) {
  const tone: Tone = a.status === "raised" ? "error" : a.status === "acknowledged" ? "warning" : a.status === "responding" ? "info" : a.status === "resolved" ? "success" : "neutral";
  const now = useNow(a.status === "raised" ? 5_000 : 0); // hitungan menunggu respons berjalan
  const headline =
    a.status === "raised" ? `Menunggu respons security · ${fmtSeconds(secondsSince(a.raised_at, now))}`
    : a.status === "acknowledged" ? `Diterima${a.acknowledged_by_name ? ` oleh ${a.acknowledged_by_name}` : ""} · respons ${fmtSeconds(a.ack_seconds)} — menuju lokasi`
    : a.status === "responding" ? `Security menangani di lokasi${a.responder_name ? ` · ${a.responder_name}` : ""}`
    : a.status === "resolved" ? "Emergency selesai ditangani"
    : "Emergency dibatalkan (alarm palsu)";
  return (
    <Card railTone={tone} className="mb-5 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="text-h3 font-bold text-on-surface tnum">{headline}</div>
          <div className="mt-0.5 text-sm text-on-surface-variant">
            {a.active ? (actions.length ? "Pilih aksi sesuai kondisi di lapangan. Setiap aksi tercatat di timeline & Incident." : "Security on-duty sudah diberi tahu. Halaman ini diperbarui otomatis.") : a.resolved_at ? `Selesai ${fmtDateTime(a.resolved_at)}` : a.cancelled_at ? `Dibatalkan ${fmtDateTime(a.cancelled_at)}` : ""}
          </div>
        </div>
        {actions.length > 0 && (
          // tombol besar hanya ≥ md; di mobile pindah ke MobileActionBar
          <div className="hidden flex-wrap items-center gap-2 md:flex">
            {actions.map((x) => (
              <Button key={x} size="lg" icon={EMERGENCY_ACTION_ICON[x]} variant={x === primary ? "primary" : "secondary"} className={TOUCH} loading={loadingAction === x} onClick={() => onAction(x)}>
                {EMERGENCY_ACTION_LABEL[x]}
              </Button>
            ))}
          </div>
        )}
      </div>
    </Card>
  );
}

function GpsValue({ alert: a }: { alert: EmergencyAlert }) {
  const url = mapsUrl(a.gps_lat, a.gps_lng);
  if (!url || a.gps_lat === null || a.gps_lng === null) return <span className="text-on-surface-variant">{labelOf(GPS_STATUS, a.gps_status ?? "unavailable")}</span>;
  return (
    <span className="inline-flex flex-wrap items-center gap-x-3 gap-y-1">
      <span className="tnum">{a.gps_lat.toFixed(6)}, {a.gps_lng.toFixed(6)}</span>
      <a href={url} target="_blank" rel="noreferrer" className="inline-flex min-h-11 items-center gap-1 font-semibold text-primary hover:underline md:min-h-0">
        <Icon name="map" size={16} aria-hidden /> Buka di Google Maps
      </a>
    </span>
  );
}

const EVENT_TONE: Record<string, string> = { raised: "border-error text-error", escalated: "border-warning text-warning", resolved: "border-success text-success", cancelled: "text-on-surface-variant" };

export function EmergencyTimeline({ events }: { events: EmergencyEvent[] }) {
  if (!events.length) return <p className="py-6 text-center text-sm text-muted-foreground">Belum ada aktivitas.</p>;
  return (
    <ol className="relative ml-2 border-l border-border pl-5">
      {events.map((e) => (
        <li key={e.id} className="relative pb-5 last:pb-0">
          <span className={cn("absolute -left-[29px] flex h-6 w-6 items-center justify-center rounded-full border border-border bg-surface text-on-surface-variant", EVENT_TONE[e.event_type])}>
            <Icon name={EMERGENCY_EVENT_ICON[e.event_type] ?? "swap_horiz"} size={14} aria-hidden />
          </span>
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <time className="tnum text-xs text-muted-foreground" dateTime={e.occurred_at}>{fmtDateTime(e.occurred_at)}</time>
            <span className="text-body font-medium">{emergencyEventText(e)}</span>
            {e.actor_name && <span className="text-sm text-on-surface-variant">{e.actor_name}</span>}
          </div>
          {e.note && <p className="mt-1 whitespace-pre-line rounded-[var(--radius-sm)] bg-surface-container px-3 py-2 text-sm">{e.note}</p>}
        </li>
      ))}
    </ol>
  );
}

function ContactsList({ contacts }: { contacts: EmergencyContact[] }) {
  if (!contacts.length) return <p className="text-sm text-on-surface-variant">Belum ada kontak darurat aktif untuk property ini.</p>;
  return (
    <ul className="divide-y divide-border">
      {contacts.map((c) => (
        <li key={c.id} className="flex items-center justify-between gap-3 py-2">
          <div className="min-w-0">
            <div className="flex items-center gap-1.5 font-medium text-on-surface"><Icon name={CONTACT_TYPE_ICON[c.contact_type] ?? "call"} size={16} className="shrink-0 text-on-surface-variant" aria-hidden /><span className="truncate">{c.name}</span></div>
            <div className="truncate text-xs text-on-surface-variant">{labelOf(CONTACT_TYPES, c.contact_type)}{c.notes ? ` · ${c.notes}` : ""}</div>
          </div>
          <a href={telHref(c.phone)} aria-label={`Telepon ${c.name} ${c.phone}`} className="inline-flex min-h-11 shrink-0 items-center gap-1.5 rounded-[var(--radius-md)] px-3 text-sm font-semibold tnum text-primary hover:bg-surface-container">
            <Icon name="call" size={16} aria-hidden />{c.phone}
          </a>
        </li>
      ))}
    </ul>
  );
}
