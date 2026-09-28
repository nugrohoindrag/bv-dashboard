// WhatsApp manual click-to-chat (PRD P3 v2.1 §6.4 P3-WAM-01..04; pengganti email yang di-hold, D-P3-08).
// Server menyusun pesan dari konteks object (POST /whatsapp/compose); staf memilih penerima, menambah catatan, lalu
// "Buka WhatsApp" mencatat log (POST /whatsapp/send) dan membuka wa.me di tab baru. Tidak ada integrasi WhatsApp API.
import { useEffect, useState } from "react";
import { Alert, Button, Dialog, DialogContent, DialogFooter, Field, NativeSelect, Textarea } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { api, uuid } from "@/lib/api";

export type WhatsAppContext =
  | "account_approved" | "account_rejected" | "account_suspended" | "account_created" | "password_reset"
  | "service_request" | "invoice" | "package" | "parking_permit" | "collection_reminder" | "staff_invite";

interface Candidate { key: string; name: string; phone: string | null; phone_valid: boolean; source: string }
interface Composed {
  context: string; recipient_key: string; recipient_name: string; phone: string; phone_valid: boolean; disabled_reason: string | null;
  text: string; url: string; candidates: Candidate[]; log_id?: string;
}

export interface WhatsAppButtonProps {
  context: WhatsAppContext;
  objectType?: string;
  objectId: string;
  label?: string;
  /** Kata sandi sementara (account_created / password_reset) — tidak dicatat di log server. */
  temporaryPassword?: string;
  /** Tautan undangan (staff_invite). */
  link?: string;
  variant?: "primary" | "secondary" | "ghost";
  size?: "sm" | "md";
  onSent?: () => void;
}

export function WhatsAppButton({ label = "WhatsApp", variant = "secondary", size = "sm", ...props }: WhatsAppButtonProps) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant={variant} size={size} icon="chat" onClick={() => setOpen(true)}>{label}</Button>
      {open && <WhatsAppDialog {...props} onClose={() => setOpen(false)} />}
    </>
  );
}

export function WhatsAppDialog({ context, objectType, objectId, temporaryPassword, link, onClose, onSent }: WhatsAppButtonProps & { onClose: () => void }) {
  const toast = useToast();
  const [recipient, setRecipient] = useState("");
  const [note, setNote] = useState("");
  const [data, setData] = useState<Composed | null>(null);
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const body = (r: string) => ({ context, object_type: objectType ?? "", object_id: objectId, recipient: r, note, temporary_password: temporaryPassword ?? "", link: link ?? "" });

  useEffect(() => {
    let alive = true;
    setLoading(true);
    api<Composed>("whatsapp/compose", { method: "POST", body: body(recipient) })
      .then((c) => {
        if (!alive) return;
        setData(c);
        setError(null);
        if (!recipient) setRecipient(c.recipient_key);
      })
      .catch((e) => alive && setError(e instanceof Error ? e.message : "Gagal menyusun pesan"))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [recipient]);

  const send = async () => {
    setSending(true);
    try {
      const out = await api<Composed>("whatsapp/send", { method: "POST", body: body(recipient), idempotencyKey: uuid() });
      window.open(out.url, "_blank", "noopener");
      toast.success("Pesan WhatsApp dibuka & dicatat");
      onSent?.();
      onClose();
    } catch (e) {
      toast.error(e);
    } finally {
      setSending(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Kirim lewat WhatsApp" description="Pesan dibuka di WhatsApp Anda (click-to-chat) dan tercatat di riwayat komunikasi." maxWidth="560px">
        {error && <Alert variant="critical">{error}</Alert>}
        {data && (
          <div className="space-y-3">
            {data.candidates.length > 1 && (
              <Field label="Penerima">
                <NativeSelect value={recipient} onChange={(e) => setRecipient(e.target.value)}>
                  {data.candidates.map((c) => <option key={c.key} value={c.key} disabled={!c.phone_valid}>{c.name}{c.phone ? ` · ${c.phone}` : " · tanpa nomor"}</option>)}
                </NativeSelect>
              </Field>
            )}
            {data.candidates.length <= 1 && <p className="text-sm">Penerima: <b>{data.recipient_name || "—"}</b>{data.phone ? ` · ${data.phone}` : ""}</p>}
            {data.disabled_reason && <Alert variant="warning">{data.disabled_reason}</Alert>}
            <Field label="Pesan">
              <Textarea rows={8} readOnly value={data.text + (note.trim() ? "\n\n" + note.trim() : "")} />
            </Field>
            <Field label="Catatan tambahan (opsional)">
              <Textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} placeholder="Baris tambahan di akhir pesan" />
            </Field>
          </div>
        )}
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="chat" loading={sending || loading} disabled={!data || !data.phone_valid || !!data.disabled_reason} onClick={send}>Buka WhatsApp</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
