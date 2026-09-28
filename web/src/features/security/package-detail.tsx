// Detail Package (PRD P3 v2.1 §5.9 P3-PKG-01..04; deep link notifikasi /security/packages/{id}): data paket & penerima, foto,
// serah terima (tanda tangan & foto), retur, riwayat (diterima → diberi tahu → pengingat → diambil/retur + WhatsApp manual), aksi dari
// allowed_actions (Serah terima · Beri tahu/Kirim pengingat · Retur · Edit) dan tombol WhatsApp manual (konteks package, P3-WAM-01).
import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardTitle } from "@/components/ui/primitives";
import { AsyncState, DetailSkeleton, KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { AttachmentGrid, PhotoEvidenceUploader } from "@/components/bv/checklist";
import { MobileActionBar } from "@/components/bv/mobile";
import { StatusBadge } from "@/components/bv/badges";
import { WhatsAppButton } from "@/components/bv/WhatsAppButton";
import { useAction, useActivities, useAttachments, useOne } from "@/api/hooks";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Activity } from "@/api/types";
import { EditPackageDialog, PackageTypeLabel, PickupDialog, ReturnPackageDialog, type Package } from "./package-dialogs";

type Dlg = "pickup" | "return" | "update";

export function PackageDetail() {
  const { id } = useParams();
  const toast = useToast();
  const q = useOne<Package>("packages", id);
  const attachments = useAttachments("package", id);
  const activities = useActivities("package", id);
  const [dlg, setDlg] = useState<Dlg | null>(null);
  const notify = useAction<void, Package>(() => `packages/${id}/notify`, { body: () => ({}), invalidate: ["list", "one", "activities"] });
  const sendNotify = (p: Package) => notify.mutateAsync().then((r) => toast.success(p.notified_at ? `Pengingat ${r.package_number} terkirim ke tenant` : `Tenant diberi tahu tentang ${r.package_number}`)).catch((e) => toast.failed("sent", e, "Notifikasi paket"));
  return (
    <AsyncState query={q} skeleton={<DetailSkeleton />}>
      {(p) => {
        const all = attachments.data ?? [];
        const photos = all.filter((a) => a.attachment_type === "photo");
        const handover = all.filter((a) => a.attachment_type === "photo_after");
        const signature = all.find((a) => a.attachment_type === "signature") ?? null;
        const waiting = p.status === "received" || p.status === "notified";
        const actions: { key: Dlg | "notify"; label: string; icon: string }[] = [
          ...(p.allowed_actions.includes("pickup") ? [{ key: "pickup" as const, label: "Serah terima", icon: "handshake" }] : []),
          ...(p.allowed_actions.includes("notify") ? [{ key: "notify" as const, label: p.notified_at ? "Kirim pengingat" : "Beri tahu tenant", icon: "notifications_active" }] : []),
          ...(p.allowed_actions.includes("update") ? [{ key: "update" as const, label: "Edit", icon: "edit" }] : []),
          ...(p.allowed_actions.includes("return") ? [{ key: "return" as const, label: "Retur", icon: "assignment_return" }] : []),
        ];
        const trigger = (k: Dlg | "notify") => (k === "notify" ? sendNotify(p) : setDlg(k));
        const primary = actions[0] ?? null;
        return (
          <div>
            <PageHeader
              breadcrumb={<><Link to="/security/packages" className="hover:underline">Packages</Link> / <span className="font-mono">{p.package_number}</span></>}
              title={<span><span className="font-mono">{p.package_number}</span> <span className="font-normal text-muted-foreground">·</span> {p.recipient_name}</span>}
              badges={<><StatusBadge objectType="package" status={p.status} />{waiting && p.days_waiting > 0 && <Badge tone={p.days_waiting >= 3 ? "warning" : "neutral"}>Menunggu {p.days_waiting} hari</Badge>}</>}
              subtitle={<>Diterima <RelativeTime value={p.received_at} />{p.received_by_name ? <> · dicatat {p.received_by_name}</> : null}{p.unit_name ? <> · {p.unit_name}</> : null}</>}
              actions={
                <span className="hidden flex-wrap gap-2 md:flex">
                  {waiting && <WhatsAppButton context="package" objectType="package" objectId={p.id} label="WhatsApp" onSent={() => activities.refetch()} />}
                  {actions.map((a) => <Button key={a.key} size="sm" icon={a.icon} variant={a.key === primary?.key ? "primary" : "secondary"} loading={a.key === "notify" && notify.isPending} onClick={() => trigger(a.key)}>{a.label}</Button>)}
                </span>
              }
            />
            {waiting && !p.notified_at && <Alert variant="warning" className="mb-4" title="Tenant belum diberi tahu">Tekan “Beri tahu tenant” untuk mengirim notifikasi Tenant App, atau kabari lewat WhatsApp.</Alert>}
            <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
              <div className="min-w-0 space-y-4 lg:col-span-8">
                <Card>
                  <CardHeader><CardTitle>Paket</CardTitle></CardHeader>
                  <CardContent>
                    <KeyValue items={[
                      { label: "Jenis", value: <PackageTypeLabel type={p.package_type} withIcon /> },
                      { label: "Kurir", value: p.courier || "—" },
                      { label: "Nomor resi", value: p.tracking_number ? <span className="font-mono">{p.tracking_number}</span> : "—" },
                      { label: "Deskripsi", value: p.description ? <span className="whitespace-pre-line">{p.description}</span> : "—" },
                      { label: "Lokasi simpan", value: p.storage_location || "—" },
                      { label: "Diterima", value: <>{fmtDateTime(p.received_at)}{p.received_by_name && <span className="block text-xs text-on-surface-variant">oleh {p.received_by_name}</span>}</> },
                    ]} />
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader><CardTitle>Penerima</CardTitle></CardHeader>
                  <CardContent>
                    <KeyValue items={[
                      { label: "Nama", value: p.recipient_name },
                      { label: "Unit", value: p.unit_name ?? "—" },
                      { label: "Tenant", value: p.tenant_id ? <Link to={`/tenant/tenants/${p.tenant_id}`} className="text-primary hover:underline">{p.tenant_name}</Link> : "—" },
                      { label: "Akun Tenant App", value: p.recipient_user_id ? "Terhubung — notifikasi langsung ke akun penerima" : "—" },
                    ]} />
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader><CardTitle>Foto paket</CardTitle><span className="text-xs text-on-surface-variant">{photos.length} foto</span></CardHeader>
                  <CardContent className="space-y-3">
                    {waiting && p.allowed_actions.includes("update") && <PhotoEvidenceUploader objectType="package" objectId={p.id} attachmentType="photo" compact label="Unggah foto paket" onUploaded={() => attachments.refetch()} />}
                    <AttachmentGrid items={photos} emptyLabel="Belum ada foto paket." />
                  </CardContent>
                </Card>
                {p.status === "picked_up" && (
                  <Card>
                    <CardHeader><CardTitle>Serah terima</CardTitle></CardHeader>
                    <CardContent className="space-y-3">
                      <KeyValue items={[
                        { label: "Diambil oleh", value: p.picked_up_by_name ?? "—" },
                        { label: "Waktu", value: fmtDateTime(p.picked_up_at) },
                        { label: "Diserahkan staf", value: p.handed_over_by_name ?? "—" },
                        { label: "Catatan", value: p.handover_note ? <span className="whitespace-pre-line">{p.handover_note}</span> : "—" },
                        { label: "Tanda tangan", value: signature?.url ? <img src={signature.url} alt="Tanda tangan pengambil" className="h-24 rounded-[var(--radius-md)] border border-border bg-surface object-contain" /> : signature ? "Tersimpan" : "Tanpa tanda tangan" },
                      ]} />
                      {handover.length > 0 && <AttachmentGrid items={handover} />}
                    </CardContent>
                  </Card>
                )}
                {p.status === "returned" && (
                  <Card>
                    <CardHeader><CardTitle>Retur</CardTitle></CardHeader>
                    <CardContent>
                      <KeyValue items={[{ label: "Waktu", value: fmtDateTime(p.returned_at) }, { label: "Alasan", value: p.return_reason ? <span className="whitespace-pre-line">{p.return_reason}</span> : "—" }]} />
                    </CardContent>
                  </Card>
                )}
              </div>
              <div className="min-w-0 space-y-4 lg:col-span-4">
                <Card>
                  <CardHeader><CardTitle>Notifikasi tenant</CardTitle></CardHeader>
                  <CardContent className="space-y-2 text-sm">
                    <p>{p.notified_at ? <>Diberi tahu {fmtDateTime(p.notified_at)}</> : "Belum diberi tahu."}</p>
                    {p.reminder_count > 0 && <p className="text-on-surface-variant">{p.reminder_count}× pengingat{p.last_reminded_at ? ` · terakhir ${fmtDateTime(p.last_reminded_at)}` : ""}</p>}
                    {waiting && <p className="text-xs text-on-surface-variant">Pengingat otomatis dikirim harian (maks. 3×) setelah paket belum diambil sesuai pengaturan property.</p>}
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader><CardTitle>Riwayat</CardTitle></CardHeader>
                  <CardContent><PackageTimeline pkg={p} activities={activities.data ?? []} /></CardContent>
                </Card>
              </div>
            </div>
            {(actions.length > 0 || waiting) && (
              <MobileActionBar
                primary={primary ? { label: primary.label, icon: primary.icon, onSelect: () => trigger(primary.key), loading: primary.key === "notify" && notify.isPending } : null}
                secondary={actions.slice(1).map((a) => ({ label: a.label, icon: a.icon, onSelect: () => trigger(a.key), destructive: a.key === "return" }))}
              />
            )}
            {waiting && <div className="mt-4 md:hidden"><WhatsAppButton context="package" objectType="package" objectId={p.id} label="Kirim via WhatsApp" size="md" onSent={() => activities.refetch()} /></div>}
            {dlg === "pickup" && <PickupDialog pkg={p} onClose={() => setDlg(null)} />}
            {dlg === "return" && <ReturnPackageDialog pkg={p} onClose={() => setDlg(null)} />}
            {dlg === "update" && <EditPackageDialog pkg={p} onClose={() => setDlg(null)} />}
          </div>
        );
      }}
    </AsyncState>
  );
}

interface TimelineEvent { key: string; at: string; icon: string; text: string; sub?: string | null; tone?: string }

const TIMELINE_ICONS = { received: "inventory_2", notified: "notifications_active", reminder: "alarm", picked: "handshake", returned: "assignment_return", whatsapp: "chat" };

function PackageTimeline({ pkg: p, activities }: { pkg: Package; activities: Activity[] }) {
  const events = useMemo(() => {
    const ev: TimelineEvent[] = [{ key: "received", at: p.received_at, icon: TIMELINE_ICONS.received, text: "Paket diterima & dicatat", sub: [p.received_by_name, p.courier].filter(Boolean).join(" · ") || null }];
    if (p.notified_at) ev.push({ key: "notified", at: p.notified_at, icon: TIMELINE_ICONS.notified, text: "Tenant diberi tahu (Tenant App)" });
    if (p.last_reminded_at && p.reminder_count > 0) ev.push({ key: "reminder", at: p.last_reminded_at, icon: TIMELINE_ICONS.reminder, text: `Pengingat ke-${p.reminder_count} terkirim`, tone: "border-warning text-warning" });
    for (const a of activities) {
      if (a.action === "whatsapp_manual_sent") {
        const pl = a.payload ?? {};
        ev.push({ key: a.id, at: a.occurred_at, icon: TIMELINE_ICONS.whatsapp, text: "Dikirim manual via WhatsApp", sub: [a.actor_name, pl.recipient ? `ke ${String(pl.recipient)}` : null].filter(Boolean).join(" · ") || null, tone: "border-success text-success" });
      }
    }
    if (p.picked_up_at) ev.push({ key: "picked", at: p.picked_up_at, icon: TIMELINE_ICONS.picked, text: `Diambil oleh ${p.picked_up_by_name ?? "—"}`, sub: p.handed_over_by_name ? `diserahkan ${p.handed_over_by_name}` : null, tone: "border-success text-success" });
    if (p.returned_at) ev.push({ key: "returned", at: p.returned_at, icon: TIMELINE_ICONS.returned, text: "Dikembalikan (retur)", sub: p.return_reason, tone: "text-on-surface-variant" });
    return ev.sort((x, y) => new Date(x.at).getTime() - new Date(y.at).getTime());
  }, [p, activities]);
  return (
    <ol className="relative ml-2 border-l border-border pl-5">
      {events.map((e) => (
        <li key={e.key} className="relative pb-4 last:pb-0">
          <span className={cn("absolute -left-[29px] flex h-6 w-6 items-center justify-center rounded-full border border-border bg-surface text-on-surface-variant", e.tone)}>
            <Icon name={e.icon} size={14} aria-hidden />
          </span>
          <time className="block tnum text-xs text-muted-foreground" dateTime={e.at}>{fmtDateTime(e.at)}</time>
          <span className="text-sm font-medium">{e.text}</span>
          {e.sub && <span className="block text-xs text-on-surface-variant">{e.sub}</span>}
        </li>
      ))}
    </ol>
  );
}
