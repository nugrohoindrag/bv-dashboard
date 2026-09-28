// Terima Pembayaran (PRD P4 v2.1 P4-PAY-05): satu penerimaan (mis. satu transfer untuk beberapa tagihan) dialokasikan ke
// invoice terbuka pihak tagih — FIFO jatuh tempo terlama (default) atau manual per invoice; sisa menjadi saldo kredit tenant
// (P4-PAY-04). Hasil: satu payment per invoice dengan grup kwitansi RCV-….
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Alert, Button, DatePicker, DialogFooter, Drawer, Field, Input, NativeSelect, Segmented, Table, TBody, TD, TH, THead, TR } from "@/components/ui/primitives";
import { CellText } from "@/components/bv/cells";
import { useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { useAll, useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { cn } from "@/lib/utils";
import { PartyPicker, PropertySelect, SectionLabel, SummaryTile } from "./shared";
import { MANUAL_METHODS, dueDay, fmtDay, money, paidAtISO, todayISO, usePropertyChoice, type Invoice, type ReceiveResult } from "./types";

export function PaymentReceiveDialog({ onClose, initial }: { onClose: () => void; initial?: { propertyId?: string; tenantId?: string; unitId?: string } }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const nav = useNavigate();
  const [pid, setPid] = usePropertyChoice(initial?.propertyId);
  const [party, setParty] = useState({ tenant: initial?.tenantId ?? "", unit: initial?.unitId ?? "" });
  const [f, setF] = useState(() => ({ amount: "", method: "transfer", date: todayISO(), reference: "", external_ref: "", notes: "" }));
  const [mode, setMode] = useState<"fifo" | "manual">("fifo");
  const [manual, setManual] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<ReceiveResult | null>(null);

  const hasParty = !!party.tenant || !!party.unit;
  const openQ = useAll<Invoice>("invoices", { property_id: pid || undefined, open: true, tenant_id: party.tenant || undefined, unit_location_id: party.tenant ? undefined : party.unit || undefined }, { enabled: !!pid && hasParty });
  // server: tenant → seluruh invoice terbuka tenant; tanpa tenant → invoice unit yang tidak bertenant. Urut jatuh tempo terlama (FIFO).
  const opens = useMemo(
    () =>
      (openQ.data ?? [])
        .filter((i) => i.outstanding_amount > 0 && (party.tenant ? true : !i.tenant_id))
        .sort((a, b) => Date.parse(a.due_at) - Date.parse(b.due_at) || Date.parse(a.issued_at ?? a.created_at) - Date.parse(b.issued_at ?? b.created_at)),
    [openQ.data, party.tenant],
  );
  const amount = Math.round(Number(f.amount) || 0);
  const totalOpen = opens.reduce((a, i) => a + i.outstanding_amount, 0);

  const plan = useMemo(() => {
    const out: Record<string, number> = {};
    if (mode === "fifo") {
      let rest = amount;
      for (const i of opens) {
        if (rest <= 0) break;
        const a = Math.min(i.outstanding_amount, rest);
        out[i.id] = a;
        rest -= a;
      }
    } else {
      for (const i of opens) {
        const v = Math.round(Number(manual[i.id]) || 0);
        if (v > 0) out[i.id] = v;
      }
    }
    return out;
  }, [mode, amount, opens, manual]);
  const allocated = Object.values(plan).reduce((a, b) => a + b, 0);
  const remainder = amount - allocated;
  const overAllocated = mode === "manual" && allocated > amount;
  const overInvoice = mode === "manual" && opens.some((i) => (plan[i.id] ?? 0) > i.outstanding_amount);
  const valid = !!pid && hasParty && amount > 0 && !!f.date && !overAllocated && !overInvoice && (mode === "fifo" || allocated > 0 || opens.length === 0);

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const out = await api<ReceiveResult>("payments/receive", {
        body: {
          property_id: pid,
          tenant_id: party.tenant || null,
          unit_location_id: party.unit || null,
          amount,
          method: f.method,
          paid_at: paidAtISO(f.date),
          reference: f.reference.trim() || null,
          external_ref: f.external_ref.trim() || null,
          notes: f.notes.trim() || null,
          allocations: mode === "manual" ? Object.entries(plan).map(([invoice_id, a]) => ({ invoice_id, amount: a })) : [],
        },
        idempotencyKey: uuid(),
      });
      invalidate("list", "one", "all");
      setResult(out);
      toast.success(`Penerimaan ${out.receipt_group} tercatat — ${out.payments.length} pembayaran`);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  if (result) {
    return (
      <Drawer open onClose={onClose} width={720} title="Penerimaan tercatat" description="Satu kwitansi per invoice dengan grup penerimaan yang sama.">
        <div className="space-y-4">
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
            <SummaryTile label="Grup penerimaan" value={<span className="font-mono text-body">{result.receipt_group}</span>} tone="success" />
            <SummaryTile label="Dialokasikan" value={money(result.allocated_amount)} sub={`${result.payments.length} invoice`} />
            <SummaryTile label="Menjadi saldo kredit" value={money(result.credit_amount)} tone={result.credit_amount > 0 ? "info" : undefined} />
          </div>
          {result.payments.length > 0 && (
            <Table>
              <THead><tr><TH>Pembayaran</TH><TH>Invoice</TH><TH className="bv-num">Nominal</TH><TH>Status</TH></tr></THead>
              <TBody>
                {result.payments.map((p) => (
                  <TR key={p.id}>
                    <TD><Link className="font-mono font-semibold text-primary hover:underline" to={`/billing/payments/${p.id}`}>{p.payment_number}</Link></TD>
                    <TD><Link className="font-mono text-primary hover:underline" to={`/billing/invoices/${p.invoice_id}`}>{p.invoice_number}</Link></TD>
                    <TD className="bv-num whitespace-nowrap">{money(p.amount, p.currency_code)}</TD>
                    <TD><StatusBadge objectType="payment" status={p.status} /></TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          )}
          <DialogFooter>
            <Button variant="secondary" icon="list" onClick={() => { nav(`/billing/payments?receipt_group=${encodeURIComponent(result.receipt_group)}`); onClose(); }}>Lihat di daftar</Button>
            <Button onClick={onClose}>Selesai</Button>
          </DialogFooter>
        </div>
      </Drawer>
    );
  }

  return (
    <Drawer open onClose={onClose} width={760} title="Terima Pembayaran" description="Catat satu penerimaan dana untuk satu tenant/unit dan alokasikan ke tagihan terbuka. Sisa yang tidak dialokasikan menjadi saldo kredit.">
      <div className="space-y-4">
        {error && <Alert variant="critical" title="Penerimaan belum tersimpan">{error}</Alert>}
        <PropertySelect value={pid} onChange={(v) => { setPid(v); setParty({ tenant: "", unit: "" }); setManual({}); }} required />
        {pid && <PartyPicker propertyId={pid} tenantId={party.tenant} unitId={party.unit} onChange={(t, u) => { setParty({ tenant: t, unit: u }); setManual({}); }} tenantLabel="Diterima dari tenant" unitLabel="Unit" />}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <Field label="Nominal diterima" required><Input type="number" inputMode="numeric" min={1} value={f.amount} onChange={(e) => setF({ ...f, amount: e.target.value })} /></Field>
          <Field label="Metode"><NativeSelect value={f.method} onChange={(e) => setF({ ...f, method: e.target.value })}>{MANUAL_METHODS.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}</NativeSelect></Field>
          <Field label="Tanggal diterima" required><DatePicker value={f.date} max={todayISO()} onChange={(v) => setF({ ...f, date: v })} /></Field>
          <Field label="Referensi bank"><Input value={f.reference} onChange={(e) => setF({ ...f, reference: e.target.value })} placeholder="No. referensi transfer" /></Field>
          <Field label="Referensi akuntansi"><Input value={f.external_ref} onChange={(e) => setF({ ...f, external_ref: e.target.value })} /></Field>
          <Field label="Catatan"><Input value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
        </div>

        {hasParty && (
          <div>
            <SectionLabel action={<Segmented value={mode} onChange={(v) => setMode(v)} options={[{ value: "fifo", label: "FIFO (jatuh tempo terlama)" }, { value: "manual", label: "Manual" }]} />}>
              Tagihan terbuka ({opens.length}) · {money(totalOpen)}
            </SectionLabel>
            {openQ.isLoading ? (
              <p className="text-sm text-on-surface-variant">Memuat tagihan terbuka…</p>
            ) : opens.length === 0 ? (
              <Alert variant="info">Tidak ada tagihan terbuka untuk pihak ini — seluruh penerimaan menjadi saldo kredit.</Alert>
            ) : (
              <Table>
                <THead><tr><TH>Invoice</TH><TH>Jatuh tempo</TH><TH className="bv-num">Sisa</TH><TH className="bv-num">Alokasi</TH></tr></THead>
                <TBody>
                  {opens.map((i) => {
                    const a = plan[i.id] ?? 0;
                    const over = a > i.outstanding_amount;
                    return (
                      <TR key={i.id}>
                        <TD>
                          <div className="whitespace-nowrap font-mono text-[13px] font-semibold">{i.display_number}</div>
                          {(i.unit_label || i.description) && <CellText muted max={240} className="text-xs">{[i.unit_label, i.description].filter(Boolean).join(" · ")}</CellText>}
                        </TD>
                        <TD className="whitespace-nowrap"><span className="inline-flex items-center gap-1.5">{fmtDay(dueDay(i))}{i.status === "overdue" && <StatusBadge objectType="invoice" status="overdue" />}</span></TD>
                        <TD className="bv-num whitespace-nowrap">{money(i.outstanding_amount)}</TD>
                        <TD className="bv-num">
                          {mode === "fifo" ? (
                            <span className={cn("tnum", a > 0 ? "font-semibold" : "text-on-surface-variant")}>{a > 0 ? money(a) : "—"}</span>
                          ) : (
                            <span className="inline-flex items-center gap-1">
                              <Input className={cn("h-9 w-32 text-right", over && "border-error")} type="number" inputMode="numeric" min={0} max={i.outstanding_amount} value={manual[i.id] ?? ""} onChange={(e) => setManual({ ...manual, [i.id]: e.target.value })} aria-label={`Alokasi ${i.display_number}`} aria-invalid={over || undefined} />
                              <Button type="button" variant="ghost" size="sm" onClick={() => setManual({ ...manual, [i.id]: String(i.outstanding_amount) })}>Lunas</Button>
                            </span>
                          )}
                        </TD>
                      </TR>
                    );
                  })}
                </TBody>
              </Table>
            )}
          </div>
        )}

        {amount > 0 && hasParty && (
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
            <SummaryTile label="Diterima" value={money(amount)} />
            <SummaryTile label="Dialokasikan" value={money(allocated)} tone={overAllocated ? "error" : undefined} sub={overAllocated ? "Melebihi nominal diterima" : overInvoice ? "Ada alokasi melebihi sisa invoice" : undefined} />
            <SummaryTile label="Sisa → saldo kredit" value={money(Math.max(0, remainder))} tone={remainder > 0 ? "info" : undefined} />
          </div>
        )}

        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="payments" loading={busy} disabled={!valid} onClick={submit}>Simpan penerimaan</Button>
        </DialogFooter>
      </div>
    </Drawer>
  );
}
