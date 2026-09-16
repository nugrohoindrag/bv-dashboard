// Komunikasi tenant ↔ building management pada Service Request (PRD §15; TD-P1-006) — thread TERPISAH dari komentar internal.
// Pesan di sini terlihat oleh tenant di Tenant App; komentar internal (CommentsPanel) tidak pernah.
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Textarea } from "@/components/ui/primitives";
import { RelativeTime, useToast } from "@/components/bv/common";
import { useAction } from "@/api/hooks";
import { api, type ListResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";

export interface TenantMessage {
  id: string;
  author_kind: "tenant" | "staff" | "system";
  author_name: string | null;
  body: string;
  attachment_ids: string[];
  created_at: string;
  read_at: string | null;
}

export function useTenantMessages(srId?: string, enabled = true) {
  return useQuery({ queryKey: ["sr-messages", srId], enabled: !!srId && enabled, queryFn: () => api<ListResponse<TenantMessage>>(`service-requests/${srId}/messages`).then((r) => r.data), refetchInterval: 30_000 });
}

export function TenantMessagesPanel({ srId, terminal, channel }: { srId: string; terminal: boolean; channel: string }) {
  const { can } = useAuth();
  const toast = useToast();
  const canView = can("tenant_relation.messages.view");
  const msgs = useTenantMessages(srId, canView);
  const [body, setBody] = useState("");
  const send = useAction<{ id: string; body: string }, TenantMessage>((i) => `service-requests/${i.id}/messages`, { body: (i) => ({ body: i.body }), invalidate: ["sr-messages", "one", "list"] });
  if (!canView) return null;
  const items = msgs.data ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>Pesan ke Tenant ({items.length})</CardTitle>
        <CardSubtitle>Terlihat oleh tenant di Tenant App. Gunakan Komentar untuk catatan internal.</CardSubtitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {channel !== "tenant_app" && items.length === 0 && <Alert variant="info">Ticket ini tidak dibuat dari Tenant App; pesan hanya terbaca bila requester memiliki akun Tenant App.</Alert>}
        <ul className="space-y-2">
          {items.map((m) => (
            <li key={m.id} className={m.author_kind === "tenant" ? "mr-10 rounded-[var(--radius-md)] bg-surface-container px-3 py-2" : "ml-10 rounded-[var(--radius-md)] bg-primary-soft px-3 py-2"}>
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span className="inline-flex items-center gap-1.5 font-medium text-foreground">
                  <Badge tone={m.author_kind === "tenant" ? "info" : "primary"}>{m.author_kind === "tenant" ? "Tenant" : "Building Management"}</Badge>
                  {m.author_name}
                </span>
                <span><RelativeTime value={m.created_at} />{m.read_at ? " · dibaca" : ""}</span>
              </div>
              <p className="mt-1 whitespace-pre-line text-body">{m.body}</p>
            </li>
          ))}
          {items.length === 0 && <li className="text-sm text-muted-foreground">Belum ada pesan.</li>}
        </ul>
        {!terminal && can("tenant_relation.messages.create") && (
          <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); if (!body.trim()) return; send.mutateAsync({ id: srId, body: body.trim() }).then(() => setBody("")).catch(toast.error); }}>
            <Textarea rows={2} className="flex-1" placeholder="Tulis pesan untuk tenant…" value={body} onChange={(e) => setBody(e.target.value)} />
            <Button type="submit" loading={send.isPending} disabled={!body.trim()}>Kirim ke Tenant</Button>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
