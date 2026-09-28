// Rekonsiliasi › pencocokan manual mutasi kredit (PRD P4 v2.1 P4-REC-02..03): ke invoice terbuka (pembayaran tenant yang
// menunggu verifikasi pada invoice itu diverifikasi otomatis — tidak ganda), ke pembayaran menunggu verifikasi, atau ke
// tenant/unit (alokasi FIFO ke tagihan tertua; sisa → saldo kredit). Hasil membuat/memverifikasi pembayaran & tercatat di audit.
import { useState } from "react";
import { Alert, Button, Dialog, DialogContent, DialogFooter, Field, Segmented, Textarea } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { fmtDate, fmtMoney } from "@/lib/format";
import { PartyPicker, RemotePicker, type Party } from "@/features/finance/fin-ui";
import { fmtDay } from "@/features/finance/fin-utils";
import type { InvoiceLite } from "./receivables-model";
import type { MatchInput, StatementImport, StmtLine } from "./reconciliation-model";

interface PaymentLite { id: string; payment_number: string; invoice_id: string; invoice_number: string; tenant_name: string | null; amount: number; method: string; status: string; created_at: string }
type Mode = "invoice" | "payment" | "party";

export function MatchDialog({ line, propertyId, onClose, onDone }: { line: StmtLine; propertyId: string; onClose: () => void; onDone: (imp: StatementImport) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [mode, setMode] = useState<Mode>(line.suggested_payment_id ? "payment" : "invoice");
  const [inv, setInv] = useState<InvoiceLite | null>(null);
  const [pay, setPay] = useState<PaymentLite | null>(null);
  const [party, setParty] = useState<Party>({ tenant_id: null, unit_location_id: null });
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const body: MatchInput | null =
    mode === "invoice" ? (inv ? { invoice_id: inv.id } : null)
    : mode === "payment" ? (pay ? { payment_id: pay.id } : null)
    : party.tenant_id ? { tenant_id: party.tenant_id } : party.unit_location_id ? { unit_location_id: party.unit_location_id } : null;
  const diff = mode === "invoice" && inv ? line.amount - inv.outstanding_amount : mode === "payment" && pay ? line.amount - pay.amount : null;
  const submit = async () => {
    if (!body) return;
    setBusy(true);
    try {
      const out = await api<StatementImport>(`billing/bank-statement-lines/${line.id}/match`, { body: { ...body, note: note.trim() || undefined }, idempotencyKey: uuid() });
      invalidate("list", "all", "overview", "dashboard");
      toast.success("Mutasi dicocokkan — pembayaran tercatat");
      onDone(out);
      onClose();
    } catch (e) {
      toast.failed("saved", e, "Pencocokan mutasi");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Cocokkan mutasi" description={`${fmtDay(line.txn_date)} · ${fmtMoney(line.amount)} · ${line.description ?? line.reference ?? "tanpa keterangan"}`}>
        <div className="space-y-4">
          <Segmented<Mode>
            value={mode}
            onChange={(m) => setMode(m)}
            options={[{ value: "invoice", label: "Invoice" }, { value: "payment", label: "Pembayaran menunggu" }, { value: "party", label: "Tenant / unit" }]}
          />
          {mode === "invoice" && (
            <Field label="Invoice terbuka" required help="Bila invoice punya pembayaran tenant yang menunggu verifikasi, pembayaran itu yang diverifikasi.">
              <RemotePicker<InvoiceLite>
                resource="invoices"
                query={{ property_id: propertyId, open: true }}
                value={inv?.id ?? null}
                onChange={(_, it) => setInv(it ?? null)}
                placeholder="Cari nomor invoice / tenant…"
                label={(i) => `${i.invoice_number ?? "—"} · ${fmtMoney(i.outstanding_amount)}`}
                render={(i) => (
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0"><div className="font-mono text-[13px] font-semibold">{i.invoice_number}</div><div className="truncate text-xs text-on-surface-variant">{[i.tenant_name, i.unit_label, `jatuh tempo ${fmtDate(i.due_at)}`].filter(Boolean).join(" · ")}</div></div>
                    <div className="text-right"><div className="tnum font-semibold">{fmtMoney(i.outstanding_amount)}</div><StatusBadge objectType="invoice" status={i.status} /></div>
                  </div>
                )}
              />
            </Field>
          )}
          {mode === "payment" && (
            <Field label="Pembayaran menunggu verifikasi" required help="Pembayaran manual tenant (transfer) yang belum diverifikasi.">
              <RemotePicker<PaymentLite>
                resource="payments"
                query={{ property_id: propertyId, status: "initiated,pending", provider: "manual" }}
                value={pay?.id ?? null}
                onChange={(_, it) => setPay(it ?? null)}
                placeholder="Cari nomor pembayaran / invoice…"
                label={(p) => `${p.payment_number} · ${fmtMoney(p.amount)}`}
                emptyText="Tidak ada pembayaran yang menunggu verifikasi."
                render={(p) => (
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0"><div className="font-mono text-[13px] font-semibold">{p.payment_number}</div><div className="truncate text-xs text-on-surface-variant">{[p.invoice_number, p.tenant_name, p.method].filter(Boolean).join(" · ")}</div></div>
                    <div className="text-right"><div className="tnum font-semibold">{fmtMoney(p.amount)}</div><StatusBadge objectType="payment" status={p.status} /></div>
                  </div>
                )}
              />
            </Field>
          )}
          {mode === "party" && (
            <Field label="Tenant / unit" required help="Dialokasikan FIFO ke tagihan tertua pihak; sisa menjadi saldo kredit.">
              <PartyPicker propertyId={propertyId} value={party} onChange={setParty} />
            </Field>
          )}
          {diff !== null && diff !== 0 && (
            <Alert variant={diff > 0 ? "info" : "warning"} title={diff > 0 ? "Lebih bayar" : "Kurang bayar"}>
              {diff > 0 ? `Mutasi ${fmtMoney(diff)} lebih besar dari tagihan — kelebihan dicatat sebagai saldo kredit pihak.` : `Mutasi ${fmtMoney(-diff)} lebih kecil — dicatat sebagai pembayaran sebagian.`}
            </Alert>
          )}
          <Field label="Catatan">
            <Textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} placeholder="mis. transfer atas nama pemilik unit, berita tanpa nomor invoice" />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="link" disabled={!body} loading={busy} onClick={submit}>Cocokkan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
