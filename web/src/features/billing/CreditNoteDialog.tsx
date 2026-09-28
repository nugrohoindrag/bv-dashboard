// Ajukan credit note (PRD P4 v2.1 P4-INV-07): koreksi invoice terbit (dibayar sebagian/penuh) tanpa mengedit invoice.
// Finance Staff mengajukan (billing.credit_notes.create) → Finance Manager menyetujui; nomor CN diberikan saat disetujui.
// Bila setelah koreksi pembayaran melebihi tagihan, kelebihannya menjadi saldo kredit tenant.
import { useState } from "react";
import { Alert, Button, Dialog, DialogContent, DialogFooter, Field, Input, Textarea } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { useAll, useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { money, type CreditNote, type Invoice } from "./types";

export function CreditNoteRequestDialog({ invoice, onClose, onDone }: { invoice: Invoice; onClose: () => void; onDone?: (cn: CreditNote) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const pending = useAll<CreditNote>("billing/credit-notes", { invoice_id: invoice.id, status: "pending" });
  const pendingSum = (pending.data ?? []).reduce((a, c) => a + c.amount, 0);
  const max = Math.max(0, invoice.total_amount - invoice.credited_amount - pendingSum);
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const n = Math.round(Number(amount) || 0);
  const overflow = Math.max(0, invoice.paid_amount + invoice.credited_amount + n - invoice.total_amount);
  const valid = n > 0 && n <= max && !!reason.trim();
  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const cn = await api<CreditNote>(`invoices/${invoice.id}/credit-notes`, { body: { amount: n, reason: reason.trim() }, idempotencyKey: uuid() });
      invalidate("list", "one", "all");
      toast.success(`Credit note ${money(cn.amount)} diajukan — menunggu persetujuan`, { to: `/billing/credit-notes/${cn.id}`, label: "Lihat" });
      onDone?.(cn);
      onClose();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Ajukan credit note · ${invoice.display_number}`} description="Koreksi nilai invoice terbit. Credit note berlaku setelah disetujui Finance Manager; invoice tidak diubah atau dihapus.">
        <div className="space-y-3">
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1 rounded-[var(--radius-md)] bg-surface-container-low px-3 py-2 text-sm">
            <dt className="text-on-surface-variant">Total invoice</dt><dd className="tnum text-right">{money(invoice.total_amount)}</dd>
            <dt className="text-on-surface-variant">Sudah dikoreksi</dt><dd className="tnum text-right">{money(invoice.credited_amount)}</dd>
            {pendingSum > 0 && <><dt className="text-on-surface-variant">Menunggu persetujuan</dt><dd className="tnum text-right">{money(pendingSum)}</dd></>}
            <dt className="text-on-surface-variant">Dibayar</dt><dd className="tnum text-right">{money(invoice.paid_amount)}</dd>
            <dt className="font-semibold">Maksimal credit note</dt><dd className="tnum text-right font-semibold">{money(max)}</dd>
          </dl>
          {error && <Alert variant="critical">{error}</Alert>}
          <Field label="Nominal koreksi" required error={n > max ? `Maksimal ${money(max)}` : undefined}>
            <Input type="number" inputMode="numeric" min={1} max={max} value={amount} onChange={(e) => setAmount(e.target.value)} autoFocus />
          </Field>
          <Field label="Alasan" required help="Tercatat di audit dan terlihat oleh penyetuju.">
            <Textarea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} placeholder="mis. Salah hitung luas unit periode September" />
          </Field>
          {overflow > 0 && n <= max && <Alert variant="info">Setelah disetujui, pembayaran melebihi tagihan sebesar {money(overflow)} — kelebihan menjadi saldo kredit tenant.</Alert>}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} disabled={!valid} onClick={submit}>Ajukan credit note</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
