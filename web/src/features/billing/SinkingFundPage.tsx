// Billing › Sinking Fund (PRD P4 v2.1 §5.4 P4-SCF-02..04; Roadmap §25.3 Apartment): dana terpisah per property — penerimaan
// otomatis dari pembayaran komponen sinking fund (porsi proporsional, koreksi saat refund/credit note), penggunaan tertaut
// Work Order sebagai bukti (tanpa alur persetujuan PPPSRS — P6), penyesuaian & saldo awal. Saldo tidak boleh negatif.
import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { FilterChip } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, DatePicker, Dialog, DialogContent, DialogFooter, Field, Input, Segmented, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { useToast } from "@/components/bv/common";
import { CellText } from "@/components/bv/cells";
import { KpiSkeleton, QueryErrorState } from "@/components/bv/states";
import { useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtMoney } from "@/lib/format";
import { MoneyInput, NeedProperty, PropertySelect, StatTile, WorkOrderPicker } from "@/features/finance/fin-ui";
import { fmtDay, fmtMoneyTile, fmtSigned, isoDay, todayISO, useGet, usePropertyParam } from "@/features/finance/fin-utils";
import { FUND_INPUT_TYPES, FUND_TYPES, apiAmount, balanceAfter, type FundEntry, type SinkingFundSummary } from "./ledger-model";

const PRESETS = [
  { key: "ytd", label: "Tahun ini" },
  { key: "12m", label: "12 bulan" },
  { key: "all", label: "Semua" },
] as const;

function presetRange(key: string): { from: string; to: string } | null {
  const now = new Date();
  if (key === "ytd") return { from: `${now.getFullYear()}-01-01`, to: isoDay(now) };
  if (key === "12m") {
    const f = new Date(now.getFullYear() - 1, now.getMonth(), now.getDate() + 1);
    return { from: isoDay(f), to: isoDay(now) };
  }
  return null;
}

export default function SinkingFundPage() {
  const { can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const [pid, setPid] = usePropertyParam(true);
  const preset = sp.get("range") ?? (sp.get("from") || sp.get("to") ? "custom" : "ytd");
  const range = preset === "custom" ? { from: sp.get("from") ?? "", to: sp.get("to") ?? "" } : presetRange(preset);
  const set = (patch: Record<string, string | null>) => {
    const n = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (v) n.set(k, v);
      else n.delete(k);
    }
    setSp(n, { replace: true });
  };
  const q = useGet<SinkingFundSummary>("billing/sinking-fund", { property_id: pid ?? undefined, from: range?.from || undefined, to: range?.to || undefined }, { enabled: !!pid });
  const [add, setAdd] = useState(false);
  const canManage = !!pid && can("billing.sinking_fund.manage", pid);
  const d = q.data;
  const columns = useMemo<ColumnDef<FundEntry, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): satu baris per sel; referensi di tooltip keterangan, satu tautan sumber/bukti.
    // Pencatat & waktu catat tidak ditampilkan di tabel.
    { id: "entry_date", header: "Tanggal", size: 120, meta: { mobile: "secondary" }, cell: ({ row }) => <span className="tnum whitespace-nowrap text-sm">{fmtDay(row.original.entry_date)}</span> },
    { id: "desc", header: "Keterangan", meta: { mobile: "primary" }, cell: ({ row: { original: e } }) => <CellText max={320} title={[e.description, e.reference].filter(Boolean).join(" · ") || undefined}>{e.description ?? "—"}</CellText> },
    { id: "type", header: "Jenis", size: 170, meta: { mobile: "status" }, cell: ({ row }) => <Badge tone={FUND_TYPES[row.original.entry_type]?.tone}>{FUND_TYPES[row.original.entry_type]?.label ?? row.original.entry_type}</Badge> },
    {
      id: "source", header: "Sumber / bukti", size: 170, meta: { mobile: "secondary" },
      cell: ({ row: { original: e } }) => (e.work_order_id
        ? <Link to={`/work-orders/${e.work_order_id}`} className="whitespace-nowrap font-mono text-[13px] text-primary hover:underline" title={e.invoice_number ? `Invoice ${e.invoice_number}` : undefined}>{e.work_order_number ?? "Work Order"}</Link>
        : e.invoice_id
          ? <Link to={`/billing/invoices/${e.invoice_id}`} className="whitespace-nowrap font-mono text-[13px] text-primary hover:underline">{e.invoice_number ?? "Invoice"}</Link>
          : <span className="text-sm text-on-surface-variant">—</span>),
    },
    { id: "amount", header: "Nominal", size: 150, meta: { mobile: "secondary" }, cell: ({ row }) => <span className="block whitespace-nowrap text-right tnum font-semibold">{fmtSigned(row.original.amount)}</span> },
  ], []);
  return (
    <div className="space-y-4">
      <PageHeader
        title="Sinking Fund"
        subtitle="Dana cadangan per property: penerimaan dari komponen sinking fund pada tagihan, penggunaan untuk pekerjaan besar (bukti Work Order), dan penyesuaian."
        actions={<>
          <Link to={`/reports/sinking-fund${range ? `?from=${range.from}&to=${range.to}` : ""}`} className="inline-flex h-10 items-center rounded-[var(--radius-md)] px-3 text-sm font-semibold text-primary hover:bg-surface-container">Laporan</Link>
          {canManage && <Button icon="add" onClick={() => setAdd(true)}>Tambah entri</Button>}
        </>}
      >
        <div className="flex flex-wrap items-center gap-2">
          <PropertySelect value={pid} onChange={setPid} />
          <span className="flex flex-wrap gap-1.5">
            {PRESETS.map((p) => <FilterChip key={p.key} selected={preset === p.key} onClick={() => set({ range: p.key === "ytd" ? null : p.key, from: null, to: null })}>{p.label}</FilterChip>)}
          </span>
          <span className="inline-flex items-center gap-1">
            <DatePicker className="w-40" value={range?.from ?? ""} onChange={(v) => set({ range: null, from: v || null, to: range?.to || todayISO() })} aria-label="Dari tanggal" />
            <span className="text-on-surface-variant">–</span>
            <DatePicker className="w-40" value={range?.to ?? ""} onChange={(v) => set({ range: null, to: v || null, from: range?.from || null })} aria-label="Sampai tanggal" />
          </span>
        </div>
      </PageHeader>
      {!pid ? (
        <NeedProperty what="saldo sinking fund" />
      ) : q.isLoading ? (
        <KpiSkeleton count={5} />
      ) : q.error && !d ? (
        <QueryErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : d ? (
        <>
          <section aria-label="Ringkasan sinking fund" className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5">
            <StatTile label="Saldo dana" value={fmtMoneyTile(d.balance)} title={fmtMoney(d.balance)} sub={`Saldo awal ${fmtMoney(d.opening)} · penyesuaian ${fmtSigned(d.adjustments)}`} tone="primary" />
            <StatTile label="Penerimaan periode" value={fmtMoneyTile(d.receipts_period)} title={fmtMoney(d.receipts_period)} sub={`Total sepanjang waktu ${fmtMoney(d.receipts)}`} />
            <StatTile label="Penggunaan periode" value={fmtMoneyTile(d.usage_period)} title={fmtMoney(d.usage_period)} sub={`Total sepanjang waktu ${fmtMoney(d.usage)}`} />
            <StatTile label="Ditagihkan" value={fmtMoneyTile(d.billed)} title={fmtMoney(d.billed)} sub="komponen sinking fund pada invoice terbit" />
            <StatTile label="Belum dibayar" value={fmtMoneyTile(d.outstanding)} title={fmtMoney(d.outstanding)} sub="porsi sinking fund tagihan terbuka" tone={d.outstanding > 0 ? "warning" : undefined} />
          </section>
          <DataGrid
            columns={columns}
            rows={d.entries}
            rowId={(r) => r.id}
            empty={{ icon: "savings", title: "Belum ada mutasi pada periode ini", description: "Penerimaan tercatat otomatis saat komponen sinking fund pada invoice dibayar. Catat saldo awal bila dana sudah ada sebelumnya.", action: canManage ? <Button icon="add" onClick={() => setAdd(true)}>Tambah entri</Button> : undefined }}
          />
          {d.entries.length >= 500 && <Alert variant="info">Menampilkan 500 mutasi terbaru — persempit rentang tanggal untuk melihat mutasi lain.</Alert>}
        </>
      ) : null}
      {add && pid && <FundEntryDialog propertyId={pid} balance={d?.balance ?? 0} onClose={() => setAdd(false)} />}
    </div>
  );
}

function FundEntryDialog({ propertyId, balance, onClose }: { propertyId: string; balance: number; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [f, setF] = useState({ entry_type: "usage" as "usage" | "adjustment" | "opening", amount: null as number | null, decrease: false, entry_date: todayISO(), description: "", reference: "", work_order_id: null as string | null });
  const [busy, setBusy] = useState(false);
  const after = f.amount ? balanceAfter(balance, f.entry_type, f.amount, f.decrease) : balance;
  const insufficient = (f.entry_type === "usage" || (f.entry_type === "adjustment" && f.decrease)) && after < 0;
  const valid = !!f.amount && f.amount > 0 && !!f.description.trim() && !insufficient;
  const submit = async () => {
    if (!valid || !f.amount) return;
    setBusy(true);
    try {
      await api("billing/sinking-fund/entries", {
        body: { property_id: propertyId, entry_type: f.entry_type, amount: apiAmount(f.entry_type, f.amount, f.decrease), entry_date: f.entry_date || null, description: f.description.trim(), work_order_id: f.work_order_id, reference: f.reference.trim() || null },
        idempotencyKey: uuid(),
      });
      invalidate("one", "list", "dashboard");
      toast.action("created", `Entri sinking fund (${FUND_TYPES[f.entry_type]?.label.toLowerCase()})`);
      onClose();
    } catch (e) {
      toast.failed("created", e, "Entri sinking fund");
    } finally {
      setBusy(false);
    }
  };
  const help = FUND_INPUT_TYPES.find((t) => t.value === f.entry_type)?.help;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Tambah entri sinking fund" description={`Saldo saat ini ${fmtMoney(balance)}. Penerimaan dari tagihan tercatat otomatis.`}>
        <div className="space-y-4">
          <Field label="Jenis" required help={help}>
            <Segmented<"usage" | "adjustment" | "opening"> value={f.entry_type} onChange={(v) => setF({ ...f, entry_type: v, decrease: false })} options={FUND_INPUT_TYPES.map((t) => ({ value: t.value, label: t.label }))} />
          </Field>
          {f.entry_type === "adjustment" && (
            <Field label="Arah penyesuaian">
              <Segmented<"up" | "down"> value={f.decrease ? "down" : "up"} onChange={(v) => setF({ ...f, decrease: v === "down" })} options={[{ value: "up", label: "Tambah saldo" }, { value: "down", label: "Kurangi saldo" }]} />
            </Field>
          )}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Nominal" required error={insufficient ? `Saldo tidak cukup (sisa ${fmtMoney(balance)})` : undefined}>
              <MoneyInput value={f.amount} onChange={(v) => setF({ ...f, amount: v })} autoFocus aria-label="Nominal" />
            </Field>
            <Field label="Tanggal">
              <DatePicker value={f.entry_date} max={todayISO()} onChange={(v) => setF({ ...f, entry_date: v })} aria-label="Tanggal entri" />
            </Field>
          </div>
          <Field label="Keterangan" required>
            <Textarea rows={2} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} placeholder={f.entry_type === "usage" ? "mis. Penggantian pompa air bersih tower A" : "mis. Bunga rekening dana Q3"} />
          </Field>
          {f.entry_type === "usage" && (
            <Field label="Work Order (bukti)" help="Disarankan: pekerjaan besar yang dibiayai dana ini.">
              <WorkOrderPicker propertyId={propertyId} value={f.work_order_id} onChange={(id) => setF({ ...f, work_order_id: id })} />
            </Field>
          )}
          <Field label="Referensi" help="Nomor dokumen/bukti transfer (opsional).">
            <Input value={f.reference} onChange={(e) => setF({ ...f, reference: e.target.value })} maxLength={120} />
          </Field>
          {f.amount ? <p className="text-sm">Saldo setelah entri: <b className="tnum">{fmtMoney(after)}</b></p> : null}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
