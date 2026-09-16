// Billing › Invoices (PRD P1 v1.3 §23; NC §34–§35): tagihan tenant (service charge/utility/rental/…), issue, cancel, pencatatan pembayaran manual.
import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useAll, useList, useOne } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import type { Tenant } from "@/api/types";

export interface InvoiceItem { id?: string; description: string; quantity: number; unit: string | null; unit_price: number; amount: number }
export interface Invoice {
  id: string; invoice_number: string; property_id: string; tenant_id: string | null; tenant_name: string | null; unit_location_id: string | null; unit_label: string | null; invoice_type: string; period_start: string | null; period_end: string | null;
  description: string | null; currency_code: string; subtotal_amount: number; tax_amount: number; total_amount: number; paid_amount: number; outstanding_amount: number; issued_at: string | null; due_at: string; paid_at: string | null;
  status: string; source: string; external_ref: string | null; notes: string | null; cancel_reason: string | null; items: InvoiceItem[]; payment_count: number; created_at: string; created_by_name: string | null; allowed_actions: string[]; version: number;
}
export interface Payment {
  id: string; payment_number: string; property_id: string; invoice_id: string; invoice_number: string; tenant_name: string | null; amount: number; currency_code: string; provider_code: string; method: string; status: string;
  provider_ref: string | null; checkout_url: string | null; va_number: string | null; instructions: string | null; expires_at: string | null; paid_at: string | null; verified_at: string | null; verification: string | null; verified_by_name: string | null;
  receipt_number: string | null; failure_reason: string | null; notes: string | null; created_at: string; allowed_actions: string[];
}
export const INVOICE_STATUS: Record<string, { label: string; tone: "warning" | "success" | "error" | "neutral" | "info" | "primary" }> = {
  draft: { label: "Draft", tone: "neutral" }, issued: { label: "Diterbitkan", tone: "info" }, partially_paid: { label: "Dibayar sebagian", tone: "warning" }, paid: { label: "Lunas", tone: "success" }, overdue: { label: "Overdue", tone: "error" }, cancelled: { label: "Dibatalkan", tone: "neutral" },
};
export const PAYMENT_STATUS: Record<string, { label: string; tone: "warning" | "success" | "error" | "neutral" | "info" | "primary" }> = {
  initiated: { label: "Diinisiasi", tone: "neutral" }, pending: { label: "Menunggu", tone: "warning" }, paid: { label: "Paid", tone: "success" }, failed: { label: "Gagal", tone: "error" }, expired: { label: "Kedaluwarsa", tone: "neutral" }, cancelled: { label: "Dibatalkan", tone: "neutral" }, refunded: { label: "Refund", tone: "info" },
};
const TYPES: Record<string, string> = { service_charge: "Service Charge", utility: "Utility", rental: "Rental", facility: "Facility", deposit: "Deposit", other: "Lainnya" };
export const rp = (n: number) => "Rp " + new Intl.NumberFormat("id-ID").format(n);

export default function InvoicesPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const { propertyId, can } = useAuth();
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const list = useList<Invoice>("invoices", { property_id: propertyId ?? undefined, status: status || undefined, q: q || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<Invoice, unknown>[]>(() => [
    { id: "number", header: "Invoice", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.invoice_number}</span>, size: 150 },
    { id: "tenant", header: t("label.tenant"), cell: ({ row }) => <div><div className="font-medium">{row.original.tenant_name ?? "—"}</div><div className="text-xs text-muted-foreground">{row.original.unit_label ?? ""}{row.original.unit_label && row.original.description ? " · " : ""}{row.original.description ?? ""}</div></div> },
    { id: "type", header: "Jenis", cell: ({ row }) => <span className="text-xs">{TYPES[row.original.invoice_type] ?? row.original.invoice_type}{row.original.period_start ? ` · ${row.original.period_start} – ${row.original.period_end ?? ""}` : ""}</span>, size: 200 },
    { id: "total", header: "Total", cell: ({ row }) => <div className="text-right"><div className="tnum font-semibold">{rp(row.original.total_amount)}</div>{row.original.paid_amount > 0 && row.original.outstanding_amount > 0 && <div className="tnum text-xs text-muted-foreground">sisa {rp(row.original.outstanding_amount)}</div>}</div>, size: 160 },
    { id: "due_at", header: "Jatuh tempo", cell: ({ row }) => <span className="text-sm">{fmtDateTime(row.original.due_at)}</span>, size: 150 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <Badge tone={INVOICE_STATUS[row.original.status]?.tone ?? "neutral"}>{INVOICE_STATUS[row.original.status]?.label ?? row.original.status}</Badge>, size: 140 },
  ], [t]);
  return (
    <div>
      <PageHeader title="Invoices" subtitle="Tagihan tenant — bukan sistem akuntansi penuh (PRD §5). Referensi akuntansi eksternal disimpan di external_ref." actions={can("billing.invoices.create") && <Button onClick={() => setCreateOpen(true)} disabled={!propertyId}><Icon name="add" size={16} /> Buat Invoice</Button>}>
        <div className="flex items-center gap-2">
          <Input className="w-64" placeholder="Cari nomor / tenant / referensi…" value={q} onChange={(e) => setQ(e.target.value)} />
          <NativeSelect className="w-48" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option>{Object.entries(INVOICE_STATUS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}</NativeSelect>
        </div>
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/billing/invoices/${r.id}`} loading={list.isLoading} isFiltered={!!q || !!status} empty={{ message: "Belum ada invoice." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
      {createOpen && propertyId && <InvoiceDialog propertyId={propertyId} onClose={() => setCreateOpen(false)} />}
      {id && <InvoiceDrawer id={id} onClose={() => nav("/billing/invoices")} />}
    </div>
  );
}

function InvoiceDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const toast = useToast();
  const inv = useOne<Invoice>("invoices", id);
  const payments = useAll<Payment>("payments", { invoice_id: id });
  const act = useAction<{ id: string; action: string; reason?: string }, Invoice>((i) => `invoices/${i.id}/${i.action}`, { body: (i) => ({ reason: i.reason }), invalidate: ["list", "one", "all"] });
  const record = useAction<{ id: string; amount: number; method: string; notes?: string }, Payment>((i) => `invoices/${i.id}/payments`, { body: (i) => ({ amount: i.amount, method: i.method, notes: i.notes }), invalidate: ["list", "one", "all"] });
  const [cancelOpen, setCancelOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [payOpen, setPayOpen] = useState(false);
  const [pay, setPay] = useState({ amount: "", method: "transfer", notes: "" });
  return (
    <Drawer open onClose={onClose} title="Invoice" width={720}>
      <AsyncState query={inv}>
        {(v) => (
          <div className="space-y-5">
            <div className="flex items-start justify-between gap-3">
              <div><div className="font-mono text-lg font-semibold">{v.invoice_number}</div><div className="text-sm text-muted-foreground">{v.tenant_name ?? "—"} {v.unit_label ? `· ${v.unit_label}` : ""} · {TYPES[v.invoice_type] ?? v.invoice_type}</div></div>
              <Badge tone={INVOICE_STATUS[v.status]?.tone ?? "neutral"}>{INVOICE_STATUS[v.status]?.label ?? v.status}</Badge>
            </div>
            <div className="flex flex-wrap gap-2">
              {v.allowed_actions.includes("issue") && <Button onClick={() => act.mutateAsync({ id, action: "issue" }).then(() => toast.success("Invoice diterbitkan & dikirim ke tenant")).catch(toast.error)} loading={act.isPending} icon="send">Terbitkan</Button>}
              {v.allowed_actions.includes("record_payment") && <Button variant="secondary" icon="payments" onClick={() => { setPay({ amount: String(v.outstanding_amount), method: "transfer", notes: "" }); setPayOpen(true); }}>Catat pembayaran</Button>}
              {v.allowed_actions.includes("cancel") && <Button variant="ghost" onClick={() => setCancelOpen(true)}>Batalkan…</Button>}
            </div>
            {v.cancel_reason && <Alert variant="warning" title="Dibatalkan">{v.cancel_reason}</Alert>}
            <KeyValue items={[
              { label: "Periode", value: v.period_start ? `${v.period_start} – ${v.period_end ?? ""}` : "—" },
              { label: "Jatuh tempo", value: fmtDateTime(v.due_at) },
              { label: "Diterbitkan", value: v.issued_at ? fmtDateTime(v.issued_at) : "—" },
              { label: "Lunas", value: v.paid_at ? fmtDateTime(v.paid_at) : "—" },
              { label: "Sumber", value: v.source },
              { label: "Referensi eksternal", value: v.external_ref ?? "—" },
              { label: "Dibuat", value: <><RelativeTime value={v.created_at} /> oleh {v.created_by_name ?? "—"}</> },
            ]} />
            <table className="w-full text-sm">
              <thead><tr className="text-left text-xs uppercase text-on-surface-variant"><th className="py-1">Item</th><th className="py-1 text-right">Qty</th><th className="py-1 text-right">Harga</th><th className="py-1 text-right">Jumlah</th></tr></thead>
              <tbody>
                {v.items.map((it, i) => <tr key={it.id ?? i} className="border-t border-border"><td className="py-1.5">{it.description}</td><td className="tnum py-1.5 text-right">{it.quantity} {it.unit ?? ""}</td><td className="tnum py-1.5 text-right">{rp(it.unit_price)}</td><td className="tnum py-1.5 text-right">{rp(it.amount)}</td></tr>)}
                <tr className="border-t border-border"><td colSpan={3} className="py-1.5 text-right text-muted-foreground">Subtotal</td><td className="tnum py-1.5 text-right">{rp(v.subtotal_amount)}</td></tr>
                <tr><td colSpan={3} className="py-1.5 text-right text-muted-foreground">Pajak</td><td className="tnum py-1.5 text-right">{rp(v.tax_amount)}</td></tr>
                <tr className="border-t border-border font-semibold"><td colSpan={3} className="py-1.5 text-right">Total</td><td className="tnum py-1.5 text-right">{rp(v.total_amount)}</td></tr>
                <tr><td colSpan={3} className="py-1.5 text-right text-muted-foreground">Dibayar</td><td className="tnum py-1.5 text-right">{rp(v.paid_amount)}</td></tr>
                <tr className="font-semibold"><td colSpan={3} className="py-1.5 text-right">Sisa</td><td className="tnum py-1.5 text-right">{rp(v.outstanding_amount)}</td></tr>
              </tbody>
            </table>
            <div>
              <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Pembayaran</div>
              <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
                {(payments.data ?? []).map((p) => <li key={p.id} className="flex items-center justify-between gap-3 px-3 py-2"><span><span className="font-mono font-semibold">{p.payment_number}</span> · {p.provider_code}/{p.method} · {p.verification === "gateway_callback" ? "callback gateway" : p.verified_by_name ? `diverifikasi ${p.verified_by_name}` : ""}</span><span className="flex items-center gap-2"><span className="tnum">{rp(p.amount)}</span><Badge tone={PAYMENT_STATUS[p.status]?.tone ?? "neutral"}>{PAYMENT_STATUS[p.status]?.label ?? p.status}</Badge></span></li>)}
                {(payments.data ?? []).length === 0 && <li className="px-3 py-2 text-muted-foreground">Belum ada pembayaran.</li>}
              </ul>
            </div>
            {cancelOpen && (
              <Dialog open onOpenChange={(o) => !o && setCancelOpen(false)}>
                <DialogContent title={`Batalkan ${v.invoice_number}`}><Field label="Alasan" required><Textarea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} /></Field>
                  <DialogFooter><Button variant="secondary" onClick={() => setCancelOpen(false)}>Batal</Button><Button variant="destructive" disabled={!reason.trim()} loading={act.isPending} onClick={() => act.mutateAsync({ id, action: "cancel", reason: reason.trim() }).then(() => setCancelOpen(false)).catch(toast.error)}>Batalkan invoice</Button></DialogFooter>
                </DialogContent>
              </Dialog>
            )}
            {payOpen && (
              <Dialog open onOpenChange={(o) => !o && setPayOpen(false)}>
                <DialogContent title="Catat pembayaran manual" description="Untuk pembayaran tunai/transfer yang sudah diterima dan diverifikasi. Pembayaran gateway hanya lewat callback provider.">
                  <div className="grid grid-cols-2 gap-3">
                    <Field label="Nominal" required><Input type="number" value={pay.amount} onChange={(e) => setPay({ ...pay, amount: e.target.value })} /></Field>
                    <Field label="Metode"><NativeSelect value={pay.method} onChange={(e) => setPay({ ...pay, method: e.target.value })}><option value="transfer">Transfer</option><option value="cash">Tunai</option><option value="card">Kartu (EDC)</option></NativeSelect></Field>
                  </div>
                  <Field label="Catatan"><Input value={pay.notes} onChange={(e) => setPay({ ...pay, notes: e.target.value })} /></Field>
                  <DialogFooter><Button variant="secondary" onClick={() => setPayOpen(false)}>Batal</Button><Button loading={record.isPending} disabled={!pay.amount || Number(pay.amount) <= 0} onClick={() => record.mutateAsync({ id, amount: Number(pay.amount), method: pay.method, notes: pay.notes || undefined }).then((p) => { toast.success(`Pembayaran ${p.payment_number} tercatat`); setPayOpen(false); }).catch(toast.error)}>Simpan</Button></DialogFooter>
                </DialogContent>
              </Dialog>
            )}
          </div>
        )}
      </AsyncState>
    </Drawer>
  );
}

function InvoiceDialog({ propertyId, onClose }: { propertyId: string; onClose: () => void }) {
  const toast = useToast();
  const tenants = useAll<Tenant>("tenants", { property_id: propertyId });
  const [f, setF] = useState({ tenant_id: "", unit_location_id: "", invoice_type: "service_charge", period_start: "", period_end: "", description: "", due_at: "", tax_amount: "0", external_ref: "", issue_now: false });
  const [items, setItems] = useState<InvoiceItem[]>([{ description: "", quantity: 1, unit: null, unit_price: 0, amount: 0 }]);
  const create = useAction<Record<string, unknown>, Invoice>(() => "invoices", { invalidate: ["list"] });
  const tenant = tenants.data?.find((x) => x.id === f.tenant_id);
  const subtotal = items.reduce((a, it) => a + (it.amount || it.quantity * it.unit_price), 0);
  const setItem = (i: number, patch: Partial<InvoiceItem>) => setItems((xs) => xs.map((x, j) => (j === i ? { ...x, ...patch, amount: (patch.quantity ?? x.quantity) * (patch.unit_price ?? x.unit_price) } : x)));
  const submit = () => create.mutateAsync({ property_id: propertyId, tenant_id: f.tenant_id || null, unit_location_id: f.unit_location_id || null, invoice_type: f.invoice_type, period_start: f.period_start || null, period_end: f.period_end || null, description: f.description || null, due_at: new Date(f.due_at).toISOString(), tax_amount: Number(f.tax_amount) || 0, external_ref: f.external_ref || null, issue_now: f.issue_now, items: items.filter((x) => x.description.trim()) })
    .then((v) => { toast.success(`Invoice ${v.invoice_number} ${v.status === "issued" ? "diterbitkan" : "disimpan sebagai draft"}`); onClose(); }).catch(toast.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Buat Invoice" maxWidth="760px">
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Tenant" required><NativeSelect value={f.tenant_id} onChange={(e) => setF({ ...f, tenant_id: e.target.value, unit_location_id: "" })}><option value="">— pilih —</option>{(tenants.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.name} ({x.tenant_code})</option>)}</NativeSelect></Field>
            <Field label="Unit"><NativeSelect value={f.unit_location_id} onChange={(e) => setF({ ...f, unit_location_id: e.target.value })} disabled={!tenant}><option value="">—</option>{(tenant?.units ?? []).map((u) => <option key={u.location_id} value={u.location_id}>Unit {u.unit_number}</option>)}</NativeSelect></Field>
            <Field label="Jenis"><NativeSelect value={f.invoice_type} onChange={(e) => setF({ ...f, invoice_type: e.target.value })}>{Object.entries(TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</NativeSelect></Field>
            <Field label="Jatuh tempo" required><Input type="date" value={f.due_at} onChange={(e) => setF({ ...f, due_at: e.target.value })} /></Field>
            <Field label="Periode mulai"><Input type="date" value={f.period_start} onChange={(e) => setF({ ...f, period_start: e.target.value })} /></Field>
            <Field label="Periode selesai"><Input type="date" value={f.period_end} onChange={(e) => setF({ ...f, period_end: e.target.value })} /></Field>
            <Field label="Deskripsi"><Input value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
            <Field label="Referensi akuntansi"><Input value={f.external_ref} onChange={(e) => setF({ ...f, external_ref: e.target.value })} /></Field>
          </div>
          <div>
            <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Item</div>
            <div className="space-y-2">
              {items.map((it, i) => (
                <div key={i} className="grid grid-cols-12 gap-2">
                  <Input className="col-span-5" placeholder="Deskripsi" value={it.description} onChange={(e) => setItem(i, { description: e.target.value })} />
                  <Input className="col-span-2" type="number" placeholder="Qty" value={it.quantity} onChange={(e) => setItem(i, { quantity: Number(e.target.value) })} />
                  <Input className="col-span-1" placeholder="Unit" value={it.unit ?? ""} onChange={(e) => setItem(i, { unit: e.target.value || null })} />
                  <Input className="col-span-3" type="number" placeholder="Harga satuan" value={it.unit_price} onChange={(e) => setItem(i, { unit_price: Number(e.target.value) })} />
                  <Button className="col-span-1" variant="ghost" size="sm" onClick={() => setItems((xs) => xs.filter((_, j) => j !== i))} aria-label="Hapus item"><Icon name="close" size={16} /></Button>
                </div>
              ))}
            </div>
            <Button size="sm" variant="secondary" className="mt-2" onClick={() => setItems((xs) => [...xs, { description: "", quantity: 1, unit: null, unit_price: 0, amount: 0 }])}><Icon name="add" size={14} /> Tambah item</Button>
          </div>
          <div className="grid grid-cols-3 gap-3 rounded-[var(--radius-md)] bg-surface-container p-3 text-sm">
            <div>Subtotal<div className="tnum font-semibold">{rp(subtotal)}</div></div>
            <Field label="Pajak"><Input type="number" value={f.tax_amount} onChange={(e) => setF({ ...f, tax_amount: e.target.value })} /></Field>
            <div>Total<div className="tnum text-lg font-semibold">{rp(subtotal + (Number(f.tax_amount) || 0))}</div></div>
          </div>
          <Checkbox label="Terbitkan sekarang (tenant langsung menerima notifikasi)" checked={f.issue_now} onCheckedChange={(v) => setF({ ...f, issue_now: v })} />
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={create.isPending} disabled={!f.tenant_id || !f.due_at || !items.some((x) => x.description.trim())} onClick={submit}>Simpan</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
