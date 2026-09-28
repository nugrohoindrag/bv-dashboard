// Log komunikasi ke tenant per Service Request (PRD P3 v2.1 §7.2 P3-TRC-03, exit gate §29): notifikasi in-app & push beserta
// status kirim/baca, pesan dua arah, dan WhatsApp manual (aksi staf — sistem tidak tahu apakah pesan benar-benar terkirim, D-P3-05).
// Tombol "Kirim via WhatsApp" (P3-WAM-01) mencatat log lalu membuka wa.me.
import { useMemo, useState } from "react";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { Badge, Card, CardContent, CardHeader, CardSubtitle, CardTitle } from "@/components/ui/primitives";
import { QueryErrorState } from "@/components/bv/common";
import { WhatsAppButton } from "@/components/bv/WhatsAppButton";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { COMM_CHANNEL_ICON, COMM_CHANNELS, COMM_STATUS, COMM_STATUS_TONE, labelOf } from "./labels";
import { useSRCommunications } from "./hooks";

const CHANNEL_ORDER = ["inapp", "push", "message", "whatsapp_manual"];

export function CommunicationsPanel({ srId, canWhatsApp = true }: { srId: string; canWhatsApp?: boolean }) {
  const q = useSRCommunications(srId);
  const [channel, setChannel] = useState<string>("");
  const items = useMemo(() => q.data ?? [], [q.data]);
  const counts = useMemo(() => items.reduce<Record<string, number>>((acc, e) => ({ ...acc, [e.channel]: (acc[e.channel] ?? 0) + 1 }), {}), [items]);
  const push = items.filter((e) => e.channel === "push");
  const shown = channel ? items.filter((e) => e.channel === channel) : items;
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Log komunikasi ke tenant</CardTitle>
          <CardSubtitle>Notifikasi in-app & push beserta status kirim, pesan dua arah, dan WhatsApp manual (dicatat saat staf membuka WhatsApp).</CardSubtitle>
        </div>
        {canWhatsApp && <WhatsAppButton context="service_request" objectType="service_request" objectId={srId} label="Kirim via WhatsApp" onSent={() => q.refetch()} />}
      </CardHeader>
      <CardContent className="space-y-3">
        {q.isError && !items.length ? (
          <QueryErrorState error={q.error} onRetry={() => q.refetch()} compact />
        ) : q.isLoading ? (
          <p className="py-4 text-sm text-on-surface-variant">Memuat log komunikasi…</p>
        ) : items.length === 0 ? (
          <p className="py-6 text-center text-sm text-on-surface-variant">Belum ada komunikasi tercatat untuk permintaan ini.</p>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Filter kanal">
              <FilterChip selected={!channel} onClick={() => setChannel("")}>Semua ({items.length})</FilterChip>
              {CHANNEL_ORDER.filter((c) => counts[c]).map((c) => (
                <FilterChip key={c} selected={channel === c} onClick={() => setChannel(channel === c ? "" : c)}>{labelOf(COMM_CHANNELS, c)} ({counts[c]})</FilterChip>
              ))}
            </div>
            {push.length > 0 && (
              <p className="text-xs text-on-surface-variant">
                Push: {push.filter((p) => p.status === "delivered").length} terkirim · {push.filter((p) => p.status === "failed").length} gagal · {push.filter((p) => p.status === "not_sent").length} tanpa perangkat terdaftar
              </p>
            )}
            <ol className="relative ml-2 border-l border-border pl-5">
              {shown.map((e, i) => (
                <li key={`${e.at}-${e.channel}-${i}`} className="relative pb-4 last:pb-0">
                  <span className={cn("absolute -left-[29px] flex h-6 w-6 items-center justify-center rounded-full border border-border bg-surface text-on-surface-variant", e.status === "failed" && "border-error text-error", e.channel === "whatsapp_manual" && "border-success text-success")}>
                    <Icon name={COMM_CHANNEL_ICON[e.channel] ?? "chat_bubble"} size={14} aria-hidden />
                  </span>
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <time className="tnum text-xs text-on-surface-variant" dateTime={e.at}>{fmtDateTime(e.at)}</time>
                    <Badge>{labelOf(COMM_CHANNELS, e.channel)}</Badge>
                    <span className="inline-flex items-center gap-0.5 text-xs text-on-surface-variant">
                      <Icon name={e.direction === "from_tenant" ? "arrow_back" : "arrow_forward"} size={12} aria-hidden />
                      {e.direction === "from_tenant" ? "Dari tenant" : "Ke tenant"}
                    </span>
                    <Badge tone={COMM_STATUS_TONE[e.status] ?? "neutral"}>{labelOf(COMM_STATUS, e.status)}</Badge>
                  </div>
                  <div className="mt-0.5 text-sm font-medium text-on-surface">{e.title}</div>
                  {e.body && <p className="line-clamp-3 whitespace-pre-line text-sm text-on-surface-variant">{e.body}</p>}
                  <div className="mt-0.5 flex flex-wrap gap-x-3 text-xs text-on-surface-variant">
                    {e.recipient && <span>Penerima: {e.recipient}</span>}
                    {e.actor && <span>{e.channel === "message" && e.direction === "from_tenant" ? "Pengirim" : "Oleh"}: {e.actor}</span>}
                    {e.detail && <span>{e.channel === "push" ? `Keterangan: ${e.detail === "no_device" ? "tenant belum mendaftarkan perangkat" : e.detail}` : e.detail}</span>}
                  </div>
                </li>
              ))}
            </ol>
          </>
        )}
      </CardContent>
    </Card>
  );
}
