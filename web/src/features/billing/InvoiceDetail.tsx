// Detail invoice (drawer /billing/invoices/:id — PRD P4 v2.1 §5.2, §6): item + pajak, total/dibayar/koreksi/sisa, aksi dari
// allowed_actions server (terbitkan, edit draft, batalkan draft, void dengan alasan, catat pembayaran, pakai saldo kredit /
// potong deposit, ajukan credit note), PDF + tautan bertanda tangan, WhatsApp manual (email di-hold), pembayaran, denda,
// credit note, dan tautan sumber (Work Order / Billing Run).
import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import { Alert, Badge, Button, ConfirmDialog, DatePicker, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, Table, TBody, TD, TH, THead, TR, Textarea } from "@/components/ui/primitives";
import { AsyncState, DetailSkeleton, KeyValue, ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText } from "@/components/bv/cells";
import { WhatsAppButton } from "@/components/bv/WhatsAppButton";
import { useAll, useInvalidate, useOne } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { CreditNoteRequestDialog } from "./CreditNoteDialog";
import { InvoiceForm } from "./InvoiceForm";
import { DocActions, ItemMeta, SectionLabel, SummaryTile } from "./shared";
import {
  INVOICE_SOURCES, MANUAL_METHODS, dueDay, errCode, fmtDay, fmtPeriod, invoiceTypeLabel, labelOf, methodLabel, money, paidAtISO, pct, qtyText, todayISO,
  type CreditNote, type Invoice, type PartyBalance, type Payment, type Penalty,
} from "./types";

export function InvoiceDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const inv = useOne<Invoice>("invoices", id);
  return (
    <Drawer open onClose={onClose} title="Invoice" width={780}>
      <AsyncState query={inv} skeleton={<DetailSkeleton />}>{(v) => <InvoiceDetail inv={v} />}</AsyncState>
    </Drawer>
  );
}

type DialogKind = null | "edit" | "issue" | "cancel" | "void" | "pay" | "credit" | "deposit" | "credit_note";

function sourceLink(v: Invoice): { to?: string; label: string } {
  const label = labelOf(INVOICE_SOURCES, v.source);
  if (v.source === "work_order" && v.source_id) return { to: `/operations/work-orders/${v.source_id}`, label };
  if (v.source === "service_request" && v.source_id) return { to: `/operations/service-requests/${v.source_id}`, label };
  if (v.source === "billing_run" && (v.billing_run_id || v.source_id)) return { to: `/billing/runs/${v.billing_run_id ?? v.source_id}`, label };
  return { label };
}

export function InvoiceDetail({ inv: v }: { inv: Invoice }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { can } = useAuth();
  const pid = v.property_id;
  const payments = useAll<Payment>("payments", { invoice_id: v.id }, { enabled: can("billing.payments.view", pid) });
  const creditNotes = useAll<CreditNote>("billing/credit-notes", { invoice_id: v.id }, { enabled: can("billing.credit_notes.view", pid) });
  const penalties = useAll<Penalty>("billing/penalties", { invoice_id: v.id }, { enabled: can("billing.penalties.view", pid) && v.invoice_type !== "penalty" });
  const [dialog, setDialog] = useState<DialogKind>(null);
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const has = (a: string) => v.allowed_actions.includes(a);
  const src = sourceLink(v);
  const issued = !!v.invoice_number && v.status !== "cancelled";
  const open = ["issued", "partially_paid", "overdue"].includes(v.status);

  const act = async (action: "issue" | "cancel" | "void", reason?: string) => {
    if (inFlight.current) return; // konfirmasi DS tidak punya state loading — cegah klik ganda
    inFlight.current = true;
    setBusy(true);
    try {
      const out = await api<Invoice>(`invoices/${v.id}/${action}`, { body: reason ? { reason } : {}, idempotencyKey: uuid() });
      invalidate("list", "one", "all");
      if (action === "issue") toast.success(`Invoice ${out.display_number} diterbitkan & dikirim ke tenant`);
      else toast.action("cancelled", action === "void" ? `Invoice ${v.display_number} (void)` : "Draft invoice");
      setDialog(null);
    } catch (e) {
      toast.error(e);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  };

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            {v.invoice_number ? <span className="font-mono text-h2 font-bold">{v.invoice_number}</span> : <span className="text-h2 font-bold">Draft invoice</span>}
            <StatusBadge objectType="invoice" status={v.status} />
            {v.days_overdue > 0 && <Badge tone="error">{v.days_overdue} hari lewat jatuh tempo</Badge>}
          </div>
          <div className="mt-0.5 text-sm text-on-surface-variant">
            {v.tenant_name ?? "Tanpa tenant"}{v.unit_label ? ` · ${v.unit_label}` : ""} · {invoiceTypeLabel(v.invoice_type)}
          </div>
          {v.description && <div className="mt-1 text-sm">{v.description}</div>}
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        {has("issue") && <Button icon="send" onClick={() => setDialog("issue")}>Terbitkan</Button>}
        {has("update") && <Button variant="secondary" icon="edit" onClick={() => setDialog("edit")}>Edit draft</Button>}
        {has("record_payment") && <Button icon="payments" onClick={() => setDialog("pay")}>Catat pembayaran</Button>}
        {has("apply_credit") && <Button variant="secondary" icon="account_balance_wallet" onClick={() => setDialog("credit")}>Pakai saldo kredit</Button>}
        {has("apply_deposit") && can("billing.deposits.manage", pid) && <Button variant="secondary" icon="savings" onClick={() => setDialog("deposit")}>Potong deposit</Button>}
        {has("credit_note") && <Button variant="secondary" icon="receipt" onClick={() => setDialog("credit_note")}>Ajukan credit note</Button>}
        {has("download_pdf") && <DocActions kind="invoice" id={v.id} fileName={v.invoice_number ?? "invoice"} />}
        {issued && v.status !== "draft" && <WhatsAppButton context="invoice" objectType="invoice" objectId={v.id} />}
        {has("void") && <Button variant="ghost" icon="block" onClick={() => setDialog("void")}>Void…</Button>}
        {has("cancel") && <Button variant="ghost" icon="delete" onClick={() => setDialog("cancel")}>Batalkan draft…</Button>}
      </div>

      {v.status === "draft" && <Alert variant="info">Draft — belum terlihat tenant. Nomor invoice (INV-{"{tahun}"}-{"{urut}"}) diberikan saat diterbitkan.</Alert>}
      {v.cancel_reason && <Alert variant="warning" title={v.invoice_number ? "Invoice di-void" : "Draft dibatalkan"}>{v.cancel_reason}</Alert>}
      {open && !has("void") && has("credit_note") && v.paid_amount + v.credited_amount > 0 && <p className="text-xs text-on-surface-variant">Invoice sudah dibayar/dikoreksi sebagian — koreksi nilai lewat credit note (void hanya untuk invoice terbit yang belum dibayar).</p>}

      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <SummaryTile label="Total" value={money(v.total_amount, v.currency_code)} />
        <SummaryTile label="Dibayar" value={money(v.paid_amount, v.currency_code)} sub={v.payment_count ? `${v.payment_count} pembayaran` : undefined} />
        <SummaryTile label="Koreksi (CN)" value={money(v.credited_amount, v.currency_code)} />
        <SummaryTile label="Sisa tagihan" value={money(open || v.status === "draft" ? v.outstanding_amount : 0, v.currency_code)} tone={v.status === "overdue" ? "error" : open && v.outstanding_amount > 0 ? "warning" : undefined} sub={v.penalty_accrued > 0 ? `+ denda berjalan ${money(v.penalty_accrued)}` : undefined} />
      </div>

      <KeyValue items={[
        { label: "Periode", value: fmtPeriod(v.period_start, v.period_end) },
        { label: "Jatuh tempo", value: <span className={cn(v.status === "overdue" && "font-semibold text-critical-text")}>{fmtDay(dueDay(v))}</span> },
        { label: "Diterbitkan", value: v.issued_at ? fmtDateTime(v.issued_at) : "—" },
        { label: "Lunas", value: v.paid_at ? fmtDateTime(v.paid_at) : "—" },
        { label: "Sumber", value: src.to ? <Link className="text-primary hover:underline" to={src.to}>{src.label}</Link> : src.label },
        ...(v.billing_run_id && v.source !== "billing_run" ? [{ label: "Billing run", value: <Link className="text-primary hover:underline" to={`/billing/runs/${v.billing_run_id}`}>Lihat run</Link> }] : []),
        { label: "Referensi akuntansi", value: v.external_ref || "—" },
        { label: "Pajak", value: v.tax_mode === "manual" ? "Manual (total)" : "Dihitung per item" },
        ...(v.notes ? [{ label: "Catatan", value: <span className="whitespace-pre-line">{v.notes}</span> }] : []),
        { label: "Dibuat", value: <><RelativeTime value={v.created_at} /> oleh {v.created_by_name ?? "sistem"}</> },
      ]} />

      <div>
        <SectionLabel>Item</SectionLabel>
        <Table>
          <THead>
            <tr><TH>Item</TH><TH className="bv-num">Qty</TH><TH className="bv-num">Harga</TH><TH className="bv-num">Jumlah</TH><TH className="bv-num">Pajak</TH></tr>
          </THead>
          <TBody>
            {v.items.map((it, i) => (
              <TR key={it.id ?? i}>
                <TD>
                  <CellText max={320}>{it.description}</CellText>
                  {it.charge_type && it.charge_type !== v.invoice_type && <div className="text-xs text-on-surface-variant">Komponen: {invoiceTypeLabel(it.charge_type)}</div>}
                  <ItemMeta meta={it.meta} unit={it.unit} />
                  {it.source_type === "work_order" && it.source_id && <Link className="text-xs text-primary hover:underline" to={`/operations/work-orders/${it.source_id}`}>Work Order</Link>}
                </TD>
                <TD className="bv-num whitespace-nowrap">{qtyText(it.quantity)} {it.unit ?? ""}</TD>
                <TD className="bv-num whitespace-nowrap">{money(it.unit_price, v.currency_code)}</TD>
                <TD className="bv-num whitespace-nowrap">{money(it.amount, v.currency_code)}</TD>
                <TD className="bv-num whitespace-nowrap">{it.tax_amount > 0 || (it.tax_rate ?? 0) > 0 ? <><span className="text-xs text-on-surface-variant">{pct(it.tax_rate)}</span> {money(it.tax_amount, v.currency_code)}</> : <span className="text-xs text-on-surface-variant">—</span>}</TD>
              </TR>
            ))}
          </TBody>
        </Table>
        <dl className="ml-auto mt-2 grid max-w-xs grid-cols-[1fr_auto] gap-x-6 gap-y-1 text-sm">
          <dt className="text-on-surface-variant">Subtotal</dt><dd className="tnum text-right">{money(v.subtotal_amount, v.currency_code)}</dd>
          <dt className="text-on-surface-variant">Pajak{v.tax_mode === "manual" ? " (manual)" : ""}</dt><dd className="tnum text-right">{money(v.tax_amount, v.currency_code)}</dd>
          <dt className="font-semibold">Total</dt><dd className="tnum text-right font-semibold">{money(v.total_amount, v.currency_code)}</dd>
          <dt className="text-on-surface-variant">Dibayar</dt><dd className="tnum text-right">− {money(v.paid_amount, v.currency_code)}</dd>
          {v.credited_amount > 0 && <><dt className="text-on-surface-variant">Credit note</dt><dd className="tnum text-right">− {money(v.credited_amount, v.currency_code)}</dd></>}
          <dt className="font-semibold">Sisa</dt><dd className="tnum text-right font-semibold">{money(open || v.status === "draft" ? v.outstanding_amount : 0, v.currency_code)}</dd>
        </dl>
      </div>

      {can("billing.payments.view", pid) && (
        <div>
          <SectionLabel>Pembayaran</SectionLabel>
          <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
            {(payments.data ?? []).map((p) => (
              <li key={p.id} className="flex flex-wrap items-center justify-between gap-3 px-3 py-2">
                <span className="min-w-0">
                  <Link to={`/billing/payments/${p.id}`} className="font-mono font-semibold text-primary hover:underline">{p.payment_number}</Link>
                  <span className="text-on-surface-variant"> · {methodLabel(p.method)}{p.reference ? ` · ${p.reference}` : ""}{p.receipt_group ? ` · ${p.receipt_group}` : ""}</span>
                  <div className="text-xs text-on-surface-variant">{p.paid_at ? fmtDateTime(p.paid_at) : <RelativeTime value={p.created_at} />}{p.verified_by_name ? ` · diverifikasi ${p.verified_by_name}` : ""}{p.proof_count ? ` · ${p.proof_count} bukti` : ""}</div>
                </span>
                <span className="flex items-center gap-2"><span className="tnum font-semibold">{money(p.amount, p.currency_code)}</span><StatusBadge objectType="payment" status={p.status} /></span>
              </li>
            ))}
            {payments.isLoading && <li className="px-3 py-2 text-on-surface-variant">Memuat…</li>}
            {!payments.isLoading && (payments.data ?? []).length === 0 && <li className="px-3 py-2 text-on-surface-variant">Belum ada pembayaran.</li>}
          </ul>
        </div>
      )}

      {can("billing.credit_notes.view", pid) && (creditNotes.data ?? []).length > 0 && (
        <div>
          <SectionLabel>Credit note</SectionLabel>
          <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
            {(creditNotes.data ?? []).map((c) => (
              <li key={c.id} className="flex flex-wrap items-center justify-between gap-3 px-3 py-2">
                <span className="min-w-0">
                  <Link to={`/billing/credit-notes/${c.id}`} className="font-semibold text-primary hover:underline">{c.credit_note_number ?? "Pengajuan"}</Link>
                  <span className="text-on-surface-variant"> · {c.reason}</span>
                  <div className="text-xs text-on-surface-variant">Diajukan {c.requested_by_name ?? "—"} · <RelativeTime value={c.requested_at} /></div>
                </span>
                <span className="flex items-center gap-2"><span className="tnum font-semibold">{money(c.amount)}</span><StatusBadge objectType="credit_note" status={c.status} /></span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {can("billing.penalties.view", pid) && (penalties.data ?? []).length > 0 && (
        <div>
          <SectionLabel action={<Link to="/billing/penalties" className="text-xs text-primary hover:underline">Kelola denda</Link>}>Denda keterlambatan</SectionLabel>
          <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
            {(penalties.data ?? []).map((p) => (
              <li key={p.id} className="flex flex-wrap items-center justify-between gap-3 px-3 py-2">
                <span className="min-w-0">
                  <span className="font-medium">{p.rule_name}</span><span className="text-on-surface-variant"> · {p.days_late} hari terlambat</span>
                  <div className="text-xs text-on-surface-variant">Ditagihkan {money(p.billed_amount)}{p.billed_invoice_number ? ` (${p.billed_invoice_number})` : ""} · belum ditagihkan {money(p.unbilled_amount)}{p.waive_reason ? ` · dihapuskan: ${p.waive_reason}` : ""}</div>
                </span>
                <span className="flex items-center gap-2"><span className="tnum font-semibold">{money(p.accrued_amount)}</span><StatusBadge objectType="invoice_penalty" status={p.status} /></span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {dialog === "edit" && <InvoiceForm invoice={v} onClose={() => setDialog(null)} />}
      <ConfirmDialog open={dialog === "issue"} onOpenChange={(o) => !o && setDialog(null)} title="Terbitkan invoice?" description={`Nomor invoice diberikan saat terbit dan tenant menerima notifikasi. Total ${money(v.total_amount)} · jatuh tempo ${fmtDay(dueDay(v))}. Invoice terbit tidak dapat diedit.`} confirmLabel="Terbitkan" onConfirm={() => act("issue")} loading={busy} />
      <ReasonDialog open={dialog === "cancel"} onOpenChange={(o) => !o && setDialog(null)} title="Batalkan draft invoice" description="Draft dibatalkan (tidak dihapus). Baris billing run terkait dilepas agar dapat ditagihkan ulang." confirmLabel="Batalkan draft" destructive loading={busy} onConfirm={(r) => act("cancel", r)} />
      <ReasonDialog open={dialog === "void"} onOpenChange={(o) => !o && setDialog(null)} title={`Void ${v.display_number}`} description="Invoice terbit yang belum dibayar dibatalkan dengan alasan; nomor tetap tercatat dan tenant diberi tahu. Pembayaran menunggu verifikasi ikut dibatalkan." label="Alasan void" confirmLabel="Void invoice" destructive loading={busy} onConfirm={(r) => act("void", r)} />
      {dialog === "pay" && <RecordPaymentDialog inv={v} onClose={() => setDialog(null)} />}
      {(dialog === "credit" || dialog === "deposit") && <ApplyBalanceDialog inv={v} kind={dialog} onClose={() => setDialog(null)} />}
      {dialog === "credit_note" && <CreditNoteRequestDialog invoice={v} onClose={() => setDialog(null)} />}
    </div>
  );
}

/** Catat pembayaran manual (P4-PAY-01): kelebihan bayar → saldo kredit (P4-PAY-04); pembayaran tenant yang menunggu verifikasi mencegah catat ganda. */
function RecordPaymentDialog({ inv, onClose }: { inv: Invoice; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [f, setF] = useState(() => ({ amount: String(inv.outstanding_amount), method: "transfer", date: todayISO(), reference: "", external_ref: "", notes: "" }));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ code?: string; message: string } | null>(null);
  const amount = Math.round(Number(f.amount) || 0);
  const excess = Math.max(0, amount - inv.outstanding_amount);
  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const p = await api<Payment>(`invoices/${inv.id}/payments`, {
        body: { amount, method: f.method, paid_at: paidAtISO(f.date), reference: f.reference.trim() || null, external_ref: f.external_ref.trim() || null, notes: f.notes.trim() || null },
        idempotencyKey: uuid(),
      });
      invalidate("list", "one", "all");
      toast.success(`Pembayaran ${p.payment_number} tercatat${excess > 0 ? ` — kelebihan ${money(excess)} menjadi saldo kredit` : ""}`, { to: `/billing/payments/${p.id}`, label: "Lihat" });
      onClose();
    } catch (e) {
      setError({ code: errCode(e), message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Catat pembayaran · ${inv.display_number}`} description="Untuk transfer/tunai yang sudah diterima (langsung terverifikasi). Pembayaran gateway hanya lewat callback provider.">
        <div className="space-y-3">
          {error && (
            <Alert variant={error.code === "PAYMENT_PENDING" ? "warning" : "critical"} title={error.code === "PAYMENT_PENDING" ? "Ada pembayaran tenant yang menunggu verifikasi" : undefined}>
              {error.message}
              {error.code === "PAYMENT_PENDING" && <div className="mt-1"><Link className="font-semibold underline" to={`/billing/payments?q=${encodeURIComponent(inv.invoice_number ?? "")}&status=initiated,pending`}>Buka daftar pembayaran</Link></div>}
            </Alert>
          )}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Nominal diterima" required help={`Sisa tagihan ${money(inv.outstanding_amount)}`}><Input type="number" inputMode="numeric" min={1} value={f.amount} onChange={(e) => setF({ ...f, amount: e.target.value })} autoFocus /></Field>
            <Field label="Metode"><NativeSelect value={f.method} onChange={(e) => setF({ ...f, method: e.target.value })}>{MANUAL_METHODS.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}</NativeSelect></Field>
            <Field label="Tanggal bayar" required><DatePicker value={f.date} max={todayISO()} onChange={(v) => setF({ ...f, date: v })} /></Field>
            <Field label="Referensi bank / bukti"><Input value={f.reference} onChange={(e) => setF({ ...f, reference: e.target.value })} placeholder="No. referensi transfer" /></Field>
            <Field label="Referensi akuntansi"><Input value={f.external_ref} onChange={(e) => setF({ ...f, external_ref: e.target.value })} /></Field>
            <Field label="Catatan"><Input value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
          </div>
          {excess > 0 && <Alert variant="info">Kelebihan {money(excess)} dicatat sebagai saldo kredit tenant (dapat dipakai untuk tagihan berikutnya).</Alert>}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} disabled={amount <= 0 || !f.date} onClick={submit}>Simpan pembayaran</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Pakai saldo kredit (P4-PAY-05) / potong deposit (P4-PND-04) untuk sisa invoice. */
function ApplyBalanceDialog({ inv, kind, onClose }: { inv: Invoice; kind: "credit" | "deposit"; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { can } = useAuth();
  const canView = can(kind === "credit" ? "billing.payments.view" : "billing.deposits.view", inv.property_id);
  const balances = useAll<PartyBalance>("billing/balances", { property_id: inv.property_id, kind, tenant_id: inv.tenant_id ?? undefined }, { enabled: canView });
  const entry = (balances.data ?? []).find((b) => (inv.tenant_id ? b.tenant_id === inv.tenant_id : !b.tenant_id && b.unit_location_id === inv.unit_location_id));
  const balance = entry?.balance ?? 0;
  const suggested = Math.max(0, Math.min(balance, inv.outstanding_amount));
  const [amount, setAmount] = useState("");
  const [notes, setNotes] = useState("");
  const [busy, setBusy] = useState(false);
  const n = amount === "" ? suggested : Math.round(Number(amount) || 0);
  const label = kind === "credit" ? "saldo kredit" : "deposit";
  const submit = async () => {
    setBusy(true);
    try {
      const p = await api<Payment>(`invoices/${inv.id}/apply-${kind}`, { body: { amount: n, notes: notes.trim() }, idempotencyKey: uuid() });
      invalidate("list", "one", "all");
      toast.success(`${kind === "credit" ? "Saldo kredit" : "Deposit"} ${money(p.amount)} diterapkan ke ${inv.display_number}`);
      onClose();
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={kind === "credit" ? "Pakai saldo kredit" : "Potong deposit"} description={kind === "credit" ? "Saldo kredit (kelebihan bayar / credit note / sisa penerimaan) dipakai untuk melunasi invoice ini." : "Deposit tenant dipotong untuk tunggakan invoice ini (tercatat di ledger deposit)."}>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-2">
            <SummaryTile label={`Saldo ${label}`} value={canView ? (balances.isLoading ? "…" : money(balance)) : "—"} sub={inv.tenant_name ?? inv.unit_label ?? undefined} />
            <SummaryTile label="Sisa invoice" value={money(inv.outstanding_amount)} />
          </div>
          {canView && !balances.isLoading && balance <= 0 && <Alert variant="warning">Tidak ada saldo {label} untuk {inv.tenant_name ?? inv.unit_label ?? "pihak ini"}.</Alert>}
          <Field label="Nominal" help={`Kosongkan untuk memakai sebesar mungkin (${money(suggested)}).`}>
            <Input type="number" inputMode="numeric" min={1} placeholder={String(suggested)} value={amount} onChange={(e) => setAmount(e.target.value)} />
          </Field>
          <Field label="Catatan"><Textarea rows={2} value={notes} onChange={(e) => setNotes(e.target.value)} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} disabled={n <= 0 || n > inv.outstanding_amount || (canView && n > balance)} onClick={submit}>Terapkan {money(n)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
