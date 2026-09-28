// Komunikasi tenant ↔ building management pada Service Request (PRD §15; TD-P1-006) — thread TERPISAH dari komentar internal.
// Pesan di sini terlihat oleh tenant di Tenant App; komentar internal (CommentsPanel) tidak pernah.
// PRD P3 v2.1 P3-SRQ-05: pesan dapat membawa foto (tenant & staf). Foto staf diunggah ke object Service Request yang sama
// (syarat server: lampiran pesan staf = file SR ini), lalu dikirim sebagai `attachment_ids`.
import { useEffect, useRef, useState } from "react";
import { Icon } from "@buildingvision/ui";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Textarea } from "@/components/ui/primitives";
import { RelativeTime, useToast } from "@/components/bv/common";
import { uploadAttachment, useAction } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import type { MessageAttachment, TenantMessage } from "./types";
import { useTenantMessages } from "./hooks";

const MAX_PHOTOS = 5;

/** Foto terpilih; `attachmentId` terisi setelah terunggah (kirim ulang tidak mengunggah dua kali). */
interface Picked { key: string; file: File; url: string; attachmentId?: string }

export function TenantMessagesPanel({ srId, terminal, channel }: { srId: string; terminal: boolean; channel: string }) {
  const { can } = useAuth();
  const toast = useToast();
  const canView = can("tenant_relation.messages.view");
  const msgs = useTenantMessages(srId, canView);
  const [body, setBody] = useState("");
  const [picked, setPicked] = useState<Picked[]>([]);
  const [uploading, setUploading] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const pickedRef = useRef<Picked[]>([]);
  useEffect(() => {
    pickedRef.current = picked;
  }, [picked]);
  // bebaskan object URL pratinjau saat panel ditutup
  useEffect(() => () => pickedRef.current.forEach((p) => URL.revokeObjectURL(p.url)), []);
  const send = useAction<{ id: string; body: string; attachment_ids: string[] }, TenantMessage>((i) => `service-requests/${i.id}/messages`, {
    body: (i) => ({ body: i.body, attachment_ids: i.attachment_ids }),
    invalidate: ["sr-messages", "sr-communications", "attachments", "one", "list"],
  });
  if (!canView) return null;
  const items = msgs.data ?? [];
  const canSend = !terminal && can("tenant_relation.messages.create");
  const busy = send.isPending || uploading !== null;

  const addFiles = (files: FileList | null) => {
    if (!files?.length) return;
    const room = MAX_PHOTOS - picked.length;
    const images = Array.from(files).filter((f) => f.type.startsWith("image/"));
    if (images.length < files.length) toast.warning("Hanya file foto (JPG, PNG, WebP) yang dapat dilampirkan.");
    if (images.length > room) toast.warning(`Maksimal ${MAX_PHOTOS} foto per pesan.`);
    const next = images.slice(0, Math.max(0, room)).map((file) => ({ key: `${file.name}-${file.size}-${Math.random().toString(36).slice(2)}`, file, url: URL.createObjectURL(file) }));
    setPicked((s) => [...s, ...next]);
  };
  const removePicked = (key: string) => {
    const p = picked.find((x) => x.key === key);
    if (p) URL.revokeObjectURL(p.url);
    setPicked((s) => s.filter((x) => x.key !== key));
  };
  const submit = async () => {
    const text = body.trim();
    if (!text || busy) return;
    const ids: string[] = [];
    try {
      for (let i = 0; i < picked.length; i++) {
        const pick = picked[i];
        if (pick.attachmentId) {
          ids.push(pick.attachmentId);
          continue;
        }
        setUploading(`Mengunggah foto ${i + 1}/${picked.length}…`);
        const a = await uploadAttachment(pick.file, "service_request", srId, "photo");
        ids.push(a.id);
        setPicked((s) => s.map((x) => (x.key === pick.key ? { ...x, attachmentId: a.id } : x)));
      }
    } catch (e) {
      setUploading(null);
      toast.failed("uploaded", e, "Foto");
      return;
    }
    setUploading(null);
    try {
      await send.mutateAsync({ id: srId, body: text, attachment_ids: ids });
      picked.forEach((p) => URL.revokeObjectURL(p.url));
      setPicked([]);
      setBody("");
    } catch (e) {
      toast.failed("sent", e, "Pesan");
    }
  };

  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Pesan ke Tenant ({items.length})</CardTitle>
          <CardSubtitle>Terlihat oleh tenant di Tenant App. Gunakan Komentar untuk catatan internal.</CardSubtitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {channel !== "tenant_app" && items.length === 0 && <Alert variant="info">Ticket ini tidak dibuat dari Tenant App; pesan hanya terbaca bila requester memiliki akun Tenant App.</Alert>}
        {msgs.isError && <Alert variant="critical" title="Pesan gagal dimuat" action={<Button size="sm" variant="secondary" onClick={() => msgs.refetch()}>Coba lagi</Button>} />}
        <ul className="space-y-2">
          {items.map((m) => (
            <li key={m.id} className={m.author_kind === "tenant" ? "mr-10 rounded-[var(--radius-md)] bg-surface-container px-3 py-2" : "ml-10 rounded-[var(--radius-md)] bg-primary-soft px-3 py-2"}>
              <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
                <span className="inline-flex items-center gap-1.5 font-medium text-foreground">
                  <Badge tone={m.author_kind === "tenant" ? "info" : "primary"}>{m.author_kind === "tenant" ? "Tenant" : "Building Management"}</Badge>
                  {m.author_name}
                </span>
                <span><RelativeTime value={m.created_at} />{m.read_at ? " · dibaca" : ""}</span>
              </div>
              <p className="mt-1 whitespace-pre-line text-body">{m.body}</p>
              <MessagePhotos items={m.attachments ?? []} missing={Math.max(0, (m.attachment_ids?.length ?? 0) - (m.attachments?.length ?? 0))} />
            </li>
          ))}
          {msgs.isLoading && <li className="text-sm text-muted-foreground">Memuat pesan…</li>}
          {!msgs.isLoading && items.length === 0 && <li className="text-sm text-muted-foreground">Belum ada pesan.</li>}
        </ul>
        {canSend && (
          <form className="space-y-2" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
            <Textarea rows={2} placeholder="Tulis pesan untuk tenant…" value={body} onChange={(e) => setBody(e.target.value)} aria-label="Pesan untuk tenant" maxLength={4000} />
            {picked.length > 0 && (
              <ul className="flex flex-wrap gap-2" aria-label="Foto terlampir">
                {picked.map((p) => (
                  <li key={p.key} className="relative">
                    <img src={p.url} alt={p.file.name} className="h-16 w-16 rounded-[var(--radius-md)] border border-border object-cover" />
                    <button type="button" onClick={() => removePicked(p.key)} disabled={busy} aria-label={`Hapus ${p.file.name}`} className="absolute -right-1.5 -top-1.5 inline-flex h-6 w-6 items-center justify-center rounded-full border border-border bg-surface text-on-surface-variant hover:text-on-surface">
                      <Icon name="close" size={14} />
                    </button>
                  </li>
                ))}
              </ul>
            )}
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="inline-flex items-center gap-2">
                <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" multiple className="hidden" onChange={(e) => { addFiles(e.target.files); e.target.value = ""; }} />
                <Button type="button" variant="secondary" size="sm" icon="photo_camera" disabled={busy || picked.length >= MAX_PHOTOS} onClick={() => fileRef.current?.click()}>Lampirkan foto</Button>
                <span className="text-xs text-on-surface-variant">{uploading ?? `${picked.length}/${MAX_PHOTOS} foto · dikompres otomatis`}</span>
              </span>
              <Button type="submit" icon="send" loading={busy} disabled={!body.trim()}>Kirim ke Tenant</Button>
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  );
}

function MessagePhotos({ items, missing }: { items: MessageAttachment[]; missing: number }) {
  if (!items.length && !missing) return null;
  return (
    <div className="mt-2 flex flex-wrap gap-2">
      {items.map((a) =>
        a.content_type.startsWith("image/") && a.url ? (
          <a key={a.id} href={a.url} target="_blank" rel="noreferrer" className="block" title={a.file_name ?? "Foto"}>
            <img src={a.thumb_url || a.url} alt={a.file_name ?? "Foto lampiran pesan"} className="h-20 w-20 rounded-[var(--radius-md)] border border-border object-cover" />
          </a>
        ) : (
          <a key={a.id} href={a.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 rounded-[var(--radius-md)] border border-border bg-surface px-2 py-1 text-xs hover:bg-surface-container">
            <Icon name="description" size={14} aria-hidden />{a.file_name ?? "Lampiran"}
          </a>
        ),
      )}
      {missing > 0 && <span className="inline-flex items-center gap-1 text-xs text-on-surface-variant"><Icon name="hide_image" size={14} aria-hidden />{missing} lampiran tidak tersedia</span>}
    </div>
  );
}
