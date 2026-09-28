// Detail pembayaran (drawer /billing/payments/:id — B-13: deep link membuka pembayaran ini). PRD P4 v2.1 §6: bukti transfer
// (P4-VRF-02), verifikasi dengan penyesuaian jumlah diterima & pending kedaluwarsa tetap dapat diverifikasi (B-09), tandai
// gagal, refund (P4-PAY-06, billing.payments.refund), ubah referensi/external_ref (P4-INT-03), kwitansi PDF + tautan (P4-RCP-02).
import { useState } from "react";
import { Link } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import { Alert, Button, Checkbox, DatePicker, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, Textarea } from "@/components/ui/primitives";
import { AsyncState, DetailSkeleton, KeyValue, ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { useAll, useInvalidate, useOne } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { fmtDateTime } from "@/lib/format";
import { DocActions, SectionLabel, SummaryTile } from "./shared";
import { PROVIDERS, VERIFICATION_LABEL, errCode, labelOf, methodLabel, money, paidAtISO, todayISO, type Payment, type Proof } from "./types";

export function PaymentDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const q = useOne<Payment>("payments", id);
  return (
    <Drawer open onClose={onClose} title="Pembayaran" width={720}>
      <AsyncState query={q} skeleton={<DetailSkeleton />}>{(p) => <PaymentDetail p={p} />}</AsyncState>
    </Drawer>
  );
}

type DialogKind = null | "verify" | "fail" | "refund" | "edit";

export function PaymentDetail({ p }: { p: Payment }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [dialog, setDialog] = useState<DialogKind>(null);
  const [busy, setBusy] = useState(false);
  const has = (a: string) => p.allowed_actions.includes(a);
  const siblings = useAll<Payment>("payments", { receipt_group: p.receipt_group ?? undefined }, { enabled: !!p.receipt_group });
  const fail = async (reason: string) => {
    setBusy(true);
    try {
      await api(`payments/${p.id}/fail`, { body: { reason }, idempotencyKey: uuid() });
      invalidate("list", "one", "all");
      toast.success(`Pembayaran ${p.payment_number} ditandai gagal`);
      setDialog(null);
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-h2 font-bold">{p.payment_number}</span>
            <StatusBadge objectType="payment" status={p.status} />
          </div>
          <div className="mt-0.5 text-sm text-on-surface-variant">
            {p.tenant_name ?? "Tanpa tenant"} · Invoice <Link className="font-mono text-primary hover:underline" to={`/billing/invoices/${p.invoice_id}`}>{p.invoice_number}</Link>
          </div>
        </div>
        <div className="text-right">
          <div className="tnum text-h2 font-bold">{money(p.amount, p.currency_code)}</div>
          <div className="text-xs text-on-surface-variant">{methodLabel(p.method)} · {labelOf(PROVIDERS, p.provider_code)}</div>
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        {has("verify") && <Button icon="task_alt" onClick={() => setDialog("verify")}>Verifikasi…</Button>}
        {has("fail") && <Button variant="secondary" icon="block" onClick={() => setDialog("fail")}>Tandai gagal…</Button>}
        {has("download_receipt") && <DocActions kind="receipt" id={p.id} fileName={p.receipt_number ?? p.payment_number} />}
        {has("update") && <Button variant="secondary" icon="edit" onClick={() => setDialog("edit")}>Ubah referensi</Button>}
        {has("refund") && <Button variant="ghost" icon="undo" onClick={() => setDialog("refund")}>Refund…</Button>}
      </div>

      {p.status === "expired" && has("verify") && <Alert variant="warning" title="Pembayaran kedaluwarsa">Batas waktu pembayaran tenant sudah lewat, tetapi tetap dapat diverifikasi bila dana benar-benar diterima (cek mutasi rekening).</Alert>}
      {p.failure_reason && <Alert variant="critical" title="Gagal">{p.failure_reason}</Alert>}
      {p.status === "refunded" && <Alert variant="warning" title={`Refund ${p.refunded_at ? fmtDateTime(p.refunded_at) : ""}`}>{p.refund_reason ?? "—"}</Alert>}
      {p.instructions && ["initiated", "pending"].includes(p.status) && <Alert variant="info" title="Instruksi pembayaran ke tenant"><span className="whitespace-pre-line">{p.instructions}</span></Alert>}

      <KeyValue items={[
        { label: "Metode", value: `${methodLabel(p.method)} · ${labelOf(PROVIDERS, p.provider_code)}` },
        { label: "Referensi bank", value: p.reference || "—" },
        { label: "Referensi akuntansi", value: p.external_ref || "—" },
        ...(p.provider_ref ? [{ label: "Ref. provider", value: <span className="font-mono text-xs">{p.provider_ref}</span> }] : []),
        ...(p.va_number ? [{ label: "Nomor VA", value: <span className="font-mono">{p.va_number}</span> }] : []),
        { label: "Kwitansi", value: p.receipt_number ? <span className="font-mono">{p.receipt_number}</span> : "—" },
        ...(p.receipt_group ? [{ label: "Grup penerimaan", value: <Link className="font-mono text-primary hover:underline" to={`/billing/payments?receipt_group=${encodeURIComponent(p.receipt_group)}`}>{p.receipt_group}</Link> }] : []),
        { label: "Dibayar", value: p.paid_at ? fmtDateTime(p.paid_at) : "—" },
        { label: "Verifikasi", value: p.verification ? `${VERIFICATION_LABEL[p.verification] ?? p.verification}${p.verified_by_name ? ` · ${p.verified_by_name}` : ""}${p.verified_at ? ` · ${fmtDateTime(p.verified_at)}` : ""}` : "—" },
        ...(p.expires_at && !p.paid_at ? [{ label: "Berlaku s/d", value: fmtDateTime(p.expires_at) }] : []),
        ...(p.tenant_user_name ? [{ label: "Diajukan tenant", value: p.tenant_user_name }] : []),
        ...(p.notes ? [{ label: "Catatan", value: <span className="whitespace-pre-line">{p.notes}</span> }] : []),
        { label: "Dibuat", value: <RelativeTime value={p.created_at} /> },
      ]} />

      <div>
        <SectionLabel>Bukti transfer ({p.proof_count})</SectionLabel>
        <ProofGallery paymentId={p.id} count={p.proof_count} />
      </div>

      {p.receipt_group && (siblings.data ?? []).length > 1 && (
        <div>
          <SectionLabel>Satu penerimaan {p.receipt_group}</SectionLabel>
          <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
            {(siblings.data ?? []).map((s) => (
              <li key={s.id} className="flex items-center justify-between gap-3 px-3 py-2">
                <span><Link to={`/billing/payments/${s.id}`} className="font-mono font-semibold text-primary hover:underline">{s.payment_number}</Link> <span className="text-on-surface-variant">→ {s.invoice_number}</span></span>
                <span className="tnum">{money(s.amount, s.currency_code)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {dialog === "verify" && <VerifyPaymentDialog payment={p} onClose={() => setDialog(null)} />}
      <ReasonDialog open={dialog === "fail"} onOpenChange={(o) => !o && setDialog(null)} title={`Tandai gagal ${p.payment_number}`} description="Mis. dana tidak masuk atau bukti tidak valid. Tenant dapat mengajukan pembayaran ulang." confirmLabel="Tandai gagal" destructive loading={busy} onConfirm={fail} />
      {dialog === "refund" && <RefundDialog payment={p} onClose={() => setDialog(null)} />}
      {dialog === "edit" && <EditPaymentDialog payment={p} onClose={() => setDialog(null)} />}
    </div>
  );
}

/** Bukti transfer: foto (thumbnail) atau PDF — dibuka di tab baru. */
export function ProofGallery({ paymentId, count }: { paymentId: string; count?: number }) {
  const q = useAll<Proof>(`payments/${paymentId}/proofs`, {}, { enabled: count === undefined || count > 0 });
  if (count === 0) return <p className="text-sm text-on-surface-variant">Belum ada bukti transfer diunggah tenant.</p>;
  if (q.isLoading) return <p className="text-sm text-on-surface-variant">Memuat bukti…</p>;
  if (q.isError) return <Alert variant="critical">Bukti tidak dapat dimuat: {(q.error as Error)?.message}</Alert>;
  const list = q.data ?? [];
  if (!list.length) return <p className="text-sm text-on-surface-variant">Belum ada bukti transfer diunggah tenant.</p>;
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
      {list.map((pr) => {
        const img = pr.content_type.startsWith("image/");
        return (
          <a key={pr.id} href={pr.url} target="_blank" rel="noopener noreferrer" className="group block overflow-hidden rounded-[var(--radius-md)] border border-border bg-surface-container-low hover:border-primary">
            {img ? (
              <img src={pr.thumb_url || pr.url} alt={pr.file_name ?? "Bukti transfer"} className="aspect-square w-full object-cover" loading="lazy" />
            ) : (
              <div className="flex aspect-square w-full flex-col items-center justify-center gap-1 text-on-surface-variant">
                <Icon name="picture_as_pdf" size={32} />
                <span className="text-xs">PDF</span>
              </div>
            )}
            <div className="truncate px-2 py-1 text-[11px] text-on-surface-variant" title={pr.file_name ?? undefined}>{pr.file_name ?? "bukti"} · {fmtDateTime(pr.uploaded_at)}</div>
          </a>
        );
      })}
    </div>
  );
}

/** Verifikasi pembayaran manual tenant (VRF-01/03): jumlah yang benar-benar diterima dapat disesuaikan (B-09). */
export function VerifyPaymentDialog({ payment: p, onClose }: { payment: Payment; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [f, setF] = useState(() => ({ amount: String(p.amount), date: todayISO(), reference: p.reference ?? "", external_ref: p.external_ref ?? "", notes: "" }));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const received = Math.round(Number(f.amount) || 0);
  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const out = await api<Payment>(`payments/${p.id}/verify`, {
        body: { amount: received, paid_at: paidAtISO(f.date), reference: f.reference.trim() || null, external_ref: f.external_ref.trim() || null, notes: f.notes.trim() || null },
        idempotencyKey: uuid(),
      });
      invalidate("list", "one", "all");
      toast.success(`Pembayaran ${out.payment_number} terverifikasi — kwitansi ${out.receipt_number ?? ""}`.trim());
      onClose();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Verifikasi ${p.payment_number}`} description="Cocokkan bukti transfer dengan mutasi rekening. Isi jumlah yang benar-benar diterima bila berbeda dari pengajuan tenant." maxWidth="680px">
        <div className="space-y-3">
          {error && <Alert variant="critical">{error}</Alert>}
          <div className="grid grid-cols-2 gap-2">
            <SummaryTile label="Diajukan tenant" value={money(p.amount, p.currency_code)} sub={`${methodLabel(p.method)}${p.reference ? ` · ${p.reference}` : ""}`} />
            <SummaryTile label="Invoice" value={<span className="font-mono text-body">{p.invoice_number}</span>} sub={p.tenant_name ?? undefined} />
          </div>
          <ProofGallery paymentId={p.id} count={p.proof_count} />
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Jumlah diterima" required help={received !== p.amount && received > 0 ? `Berbeda ${money(received - p.amount)} dari pengajuan` : "Kelebihan dari sisa tagihan menjadi saldo kredit tenant."}>
              <Input type="number" inputMode="numeric" min={1} value={f.amount} onChange={(e) => setF({ ...f, amount: e.target.value })} />
            </Field>
            <Field label="Tanggal dana diterima" required><DatePicker value={f.date} max={todayISO()} onChange={(v) => setF({ ...f, date: v })} /></Field>
            <Field label="Referensi bank"><Input value={f.reference} onChange={(e) => setF({ ...f, reference: e.target.value })} /></Field>
            <Field label="Referensi akuntansi"><Input value={f.external_ref} onChange={(e) => setF({ ...f, external_ref: e.target.value })} /></Field>
            <Field label="Catatan verifikasi" className="sm:col-span-2"><Input value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
          </div>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="task_alt" loading={busy} disabled={received <= 0 || !f.date} onClick={submit}>Verifikasi {money(received)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Refund pembayaran lunas (P4-PAY-06): uang keluar atau dikembalikan sebagai saldo kredit. */
function RefundDialog({ payment: p, onClose }: { payment: Payment; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [reason, setReason] = useState("");
  const [toCredit, setToCredit] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ code?: string; message: string } | null>(null);
  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      await api<Payment>(`payments/${p.id}/refund`, { body: { reason: reason.trim(), to_credit: toCredit }, idempotencyKey: uuid() });
      invalidate("list", "one", "all");
      toast.success(`Pembayaran ${p.payment_number} direfund${toCredit ? " ke saldo kredit" : ""}`);
      onClose();
    } catch (e) {
      setError({ code: errCode(e), message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Refund ${p.payment_number}`} description={`Status pembayaran menjadi Refund; invoice ${p.invoice_number} dikoreksi (sisa tagihan bertambah). Tindakan tercatat di audit.`}>
        <div className="space-y-3">
          {error && <Alert variant="critical" title={error.code === "CREDIT_ALREADY_USED" ? "Kelebihan bayar sudah terpakai" : undefined}>{error.message}</Alert>}
          <SummaryTile label="Nominal pembayaran" value={money(p.amount, p.currency_code)} />
          <Field label="Alasan refund" required><Textarea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} autoFocus /></Field>
          <Checkbox label="Kembalikan sebagai saldo kredit tenant (tidak ada uang keluar)" checked={toCredit} onCheckedChange={setToCredit} />
          <p className="text-xs text-on-surface-variant">{toCredit ? "Nominal yang diterapkan ke invoice menjadi saldo kredit; dapat dipakai untuk tagihan lain." : "Refund tunai/transfer: lakukan pengembalian dana di luar sistem, lalu catat referensinya di catatan."}</p>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button variant="destructive" icon="undo" loading={busy} disabled={!reason.trim()} onClick={submit}>Refund</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Ubah referensi bank / nomor dokumen akuntansi / catatan (P4-INT-03). */
function EditPaymentDialog({ payment: p, onClose }: { payment: Payment; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [f, setF] = useState({ reference: p.reference ?? "", external_ref: p.external_ref ?? "", notes: p.notes ?? "" });
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    try {
      await api<Payment>(`payments/${p.id}`, { method: "PATCH", body: { reference: f.reference.trim(), external_ref: f.external_ref.trim(), notes: f.notes.trim() } });
      invalidate("list", "one", "all");
      toast.action("saved", `Pembayaran ${p.payment_number}`);
      onClose();
    } catch (e) {
      toast.failed("saved", e, "Pembayaran");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Ubah referensi · ${p.payment_number}`}>
        <div className="space-y-3">
          <Field label="Referensi bank"><Input value={f.reference} onChange={(e) => setF({ ...f, reference: e.target.value })} /></Field>
          <Field label="Referensi akuntansi (external_ref)" help="Nomor dokumen di sistem akuntansi."><Input value={f.external_ref} onChange={(e) => setF({ ...f, external_ref: e.target.value })} /></Field>
          <Field label="Catatan"><Textarea rows={3} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
