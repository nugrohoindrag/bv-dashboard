// Billing › Deposit & Kredit (PRD P4 v2.1 §5.5 P4-PND-03..04, §6.1 P4-PAY-04..05): saldo per tenant (seluruh unitnya) / unit
// tanpa tenant → ledger mutasi. Deposit: diterima (juga otomatis dari item deposit invoice), dipotong (kerusakan/tunggakan,
// tertaut Work Order), dikembalikan saat move-out, penyesuaian — saldo tidak boleh negatif. Saldo Kredit: kelebihan bayar &
// credit note, dipakai untuk invoice berikutnya (baca saja; penerapan di invoice). Tab di URL: ?tab=deposit|credit.
import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, DatePicker, Dialog, DialogContent, DialogFooter, Drawer, Field, SearchInput, Segmented, Tabs, TabsContent, TabsList, TabsTrigger, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { RelativeTime, useToast } from "@/components/bv/common";
import { CellText } from "@/components/bv/cells";
import { KpiSkeleton } from "@/components/bv/states";
import { useAll, useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtMoney, fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Tenant } from "@/api/types";
import { MoneyInput, NeedProperty, PartyPicker, PropertySelect, StatTile, WorkOrderPicker, type Party } from "@/features/finance/fin-ui";
import { fmtDay, fmtMoneyTile, fmtSigned, todayISO, usePropertyParam } from "@/features/finance/fin-utils";
import { statementLink } from "./receivables-model";
import { CREDIT_TYPES, DEPOSIT_INPUT_TYPES, DEPOSIT_TYPES, apiAmount, balanceAfter, balanceKey, balanceParty, balanceTotals, type LedgerEntry, type PartyBalance } from "./ledger-model";

type Kind = "deposit" | "credit";

export default function DepositsPage() {
  const { can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const canCredit = can("billing.payments.view");
  const tab: Kind = sp.get("tab") === "credit" && canCredit ? "credit" : "deposit";
  const setTab = (t: string) => {
    const n = new URLSearchParams(sp);
    if (t === "credit") n.set("tab", "credit");
    else n.delete("tab");
    setSp(n, { replace: true });
  };
  return (
    <div>
      <PageHeader title="Deposit & Kredit" subtitle="Ledger deposit jaminan dan saldo kredit (kelebihan bayar / credit note) per tenant atau unit. Setiap mutasi tercatat dan tidak mengubah entri lama." />
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="deposit">Deposit</TabsTrigger>
          {canCredit && <TabsTrigger value="credit">Saldo Kredit</TabsTrigger>}
        </TabsList>
        <TabsContent value="deposit"><LedgerTab kind="deposit" /></TabsContent>
        <TabsContent value="credit"><LedgerTab kind="credit" /></TabsContent>
      </Tabs>
    </div>
  );
}

function LedgerTab({ kind }: { kind: Kind }) {
  const { can } = useAuth();
  const [pid, setPid] = usePropertyParam(true);
  const [q, setQ] = useState("");
  const [selected, setSelected] = useState<PartyBalance | null>(null);
  const [add, setAdd] = useState<{ party?: Party & { label?: string }; balance?: number } | null>(null);
  const balances = useAll<PartyBalance>("billing/balances", { kind, property_id: pid ?? undefined }, { enabled: !!pid });
  const recent = useAll<LedgerEntry>("billing/ledger-entries", { kind, property_id: pid ?? undefined }, { enabled: !!pid });
  const tenants = useAll<Tenant>("tenants", { property_id: pid ?? undefined }, { enabled: !!pid && can("property.tenants.view", pid) });
  const all = useMemo(() => balances.data ?? [], [balances.data]);
  const rows = useMemo(() => (q.trim() ? all.filter((b) => balanceParty(b).toLowerCase().includes(q.trim().toLowerCase())) : all), [all, q]);
  const totals = balanceTotals(all);
  const canManage = kind === "deposit" && !!pid && can("billing.deposits.manage", pid);
  const types = kind === "deposit" ? DEPOSIT_TYPES : CREDIT_TYPES;
  // nama pihak untuk mutasi terbaru: tenant dari master tenant / saldo; unit tanpa tenant dari label saldo
  const names = useMemo(() => {
    const m = new Map<string, string>();
    for (const t of tenants.data ?? []) m.set(t.id, t.name);
    for (const b of all) {
      if (b.tenant_id && b.tenant_name && !m.has(b.tenant_id)) m.set(b.tenant_id, b.tenant_name);
      if (!b.tenant_id && b.unit_location_id && b.unit_label) m.set(b.unit_location_id, b.unit_label);
    }
    return m;
  }, [tenants.data, all]);
  const columns = useMemo<ColumnDef<PartyBalance, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): satu baris per sel; jenis pihak (tenant seluruh unit / unit tanpa tenant) di tooltip.
    { id: "party", header: "Tenant / unit", meta: { mobile: "primary" }, cell: ({ row: { original: b } }) => <CellText max={260} className="font-medium" title={`${balanceParty(b)} · ${b.tenant_id ? "Tenant (seluruh unit)" : "Unit tanpa tenant"}`}>{balanceParty(b)}</CellText> },
    { id: "balance", header: "Saldo", size: 170, meta: { mobile: "status" }, cell: ({ row }) => <span className={cn("block whitespace-nowrap text-right tnum font-semibold", row.original.balance < 0 && "text-on-error-container")}>{fmtMoney(row.original.balance)}</span> },
    { id: "last", header: "Mutasi terakhir", size: 160, meta: { mobile: "secondary" }, cell: ({ row }) => <RelativeTime value={row.original.last_entry_at} className="whitespace-nowrap text-sm" /> },
  ], []);
  const entryColumns = useMemo<ColumnDef<LedgerEntry, unknown>[]>(() => [
    { id: "entry_date", header: "Tanggal", size: 110, meta: { mobile: "secondary" }, cell: ({ row }) => <span className="tnum whitespace-nowrap text-sm">{fmtDay(row.original.entry_date)}</span> },
    { id: "party", header: "Pihak", meta: { mobile: "primary" }, cell: ({ row: { original: e } }) => <CellText max={200}>{e.tenant_id ? names.get(e.tenant_id) ?? "Tenant" : names.get(e.unit_location_id ?? "") ?? "Unit"}</CellText> },
    { id: "type", header: "Jenis", size: 170, meta: { mobile: "status" }, cell: ({ row }) => <Badge tone={types[row.original.entry_type]?.tone}>{types[row.original.entry_type]?.label ?? row.original.entry_type}</Badge> },
    { id: "desc", header: "Keterangan", meta: { mobile: "hidden" }, cell: ({ row: { original: e } }) => <div className="flex min-w-0 items-center gap-1.5"><CellText max={240}>{e.description ?? "—"}</CellText>{e.invoice_id && <Link to={`/billing/invoices/${e.invoice_id}`} onClick={(ev) => ev.stopPropagation()} className="shrink-0 whitespace-nowrap font-mono text-[13px] text-primary hover:underline">{e.invoice_number ?? "Invoice"}</Link>}</div> },
    { id: "amount", header: "Nominal", size: 150, meta: { mobile: "secondary" }, cell: ({ row }) => <span className="block whitespace-nowrap text-right tnum font-semibold">{fmtSigned(row.original.amount)}</span> },
  ], [names, types]);
  if (!pid) return <NeedProperty what={kind === "deposit" ? "saldo deposit" : "saldo kredit"} />;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <PropertySelect value={pid} onChange={(id) => { setPid(id); setSelected(null); }} />
        <SearchInput className="w-full sm:w-64" placeholder="Cari tenant / unit…" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Cari tenant atau unit" />
        {canManage && <Button icon="add" className="ml-auto" onClick={() => setAdd({})}>Catat mutasi deposit</Button>}
      </div>
      {balances.isLoading ? <KpiSkeleton count={3} /> : (
        <section aria-label="Ringkasan saldo" className="grid grid-cols-2 gap-3 md:grid-cols-3">
          <StatTile label={kind === "deposit" ? "Total deposit ditahan" : "Total saldo kredit"} value={fmtMoneyTile(totals.total)} title={fmtMoney(totals.total)} sub={kind === "deposit" ? "kewajiban ke tenant" : "dapat dipakai untuk invoice berikutnya"} tone="primary" />
          <StatTile label="Pihak dengan saldo" value={fmtNumber(totals.parties)} sub="tenant / unit" />
          <StatTile label="Saldo negatif" value={fmtNumber(totals.negative)} sub="perlu koreksi" tone={totals.negative > 0 ? "error" : undefined} />
        </section>
      )}
      {kind === "credit" && <Alert variant="info">Saldo kredit bertambah dari kelebihan bayar, credit note, dan sisa penerimaan rekonsiliasi; dipakai lewat aksi <b>Terapkan saldo kredit</b> di detail invoice.</Alert>}
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => balanceKey(r)}
        onRowClick={(b) => { setSelected(b); }}
        loading={balances.isLoading}
        error={balances.error}
        onRetry={() => balances.refetch()}
        isFiltered={!!q.trim()}
        empty={{ icon: kind === "deposit" ? "account_balance_wallet" : "credit_score", title: kind === "deposit" ? "Belum ada saldo deposit" : "Belum ada saldo kredit", description: kind === "deposit" ? "Deposit tercatat otomatis dari item deposit pada invoice yang dibayar, atau dicatat manual." : "Saldo kredit muncul saat tenant membayar lebih atau credit note disetujui." }}
      />
      <div>
        <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Mutasi terbaru (semua pihak)</div>
        <DataGrid
          columns={entryColumns}
          rows={(recent.data ?? []).slice(0, 50)}
          rowId={(r) => r.id}
          onRowClick={(e) => {
            const b = all.find((x) => (e.tenant_id ? x.tenant_id === e.tenant_id : !x.tenant_id && x.unit_location_id === e.unit_location_id));
            setSelected(b ?? { tenant_id: e.tenant_id, tenant_name: e.tenant_id ? names.get(e.tenant_id) ?? null : null, unit_location_id: e.tenant_id ? null : e.unit_location_id, unit_label: e.tenant_id ? null : names.get(e.unit_location_id ?? "") ?? null, balance: 0, last_entry_at: null });
          }}
          loading={recent.isLoading}
          error={recent.error}
          onRetry={() => recent.refetch()}
          empty={{ icon: "history", title: "Belum ada mutasi" }}
        />
      </div>
      {selected && <LedgerDrawer kind={kind} propertyId={pid} party={selected} onClose={() => setSelected(null)} onAdd={canManage ? () => setAdd({ party: { tenant_id: selected.tenant_id, unit_location_id: selected.tenant_id ? null : selected.unit_location_id, label: balanceParty(selected) }, balance: selected.balance }) : undefined} />}
      {add && <DepositEntryDialog propertyId={pid} party={add.party} balance={add.balance} balances={all} onClose={() => setAdd(null)} />}
    </div>
  );
}

function LedgerDrawer({ kind, propertyId, party, onClose, onAdd }: { kind: Kind; propertyId: string; party: PartyBalance; onClose: () => void; onAdd?: () => void }) {
  const q = useAll<LedgerEntry>("billing/ledger-entries", { kind, property_id: propertyId, tenant_id: party.tenant_id ?? undefined, unit_location_id: party.tenant_id ? undefined : party.unit_location_id ?? undefined });
  const types = kind === "deposit" ? DEPOSIT_TYPES : CREDIT_TYPES;
  // saldo berjalan (lama → baru); pihak unit tanpa tenant hanya entri tanpa tenant (sama dengan perhitungan saldo server)
  const rows = useMemo(() => {
    const asc = (q.data ?? []).filter((e) => party.tenant_id || !e.tenant_id).slice().reverse();
    const out: (LedgerEntry & { running: number })[] = [];
    let bal = 0;
    for (const e of asc) {
      bal += e.amount;
      out.push({ ...e, running: bal });
    }
    return out.reverse();
  }, [q.data, party.tenant_id]);
  return (
    <Drawer open onClose={onClose} title={balanceParty(party)} description={`${kind === "deposit" ? "Ledger deposit" : "Ledger saldo kredit"} · saldo ${fmtMoney(party.balance)}`} width={680}>
      <div className="space-y-4">
        <div className="flex flex-wrap gap-2">
          {onAdd && <Button icon="add" onClick={onAdd}>Catat mutasi</Button>}
          <Link to={statementLink(propertyId, party)} className="inline-flex h-10 items-center rounded-[var(--radius-md)] px-3 text-sm font-semibold text-primary hover:bg-surface-container">Statement of account</Link>
        </div>
        {q.isLoading ? <p className="text-sm text-on-surface-variant">Memuat mutasi…</p> : q.isError ? <Alert variant="critical">Mutasi gagal dimuat.</Alert> : rows.length === 0 ? <p className="text-sm text-on-surface-variant">Belum ada mutasi.</p> : (
          <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
            {rows.map((e) => (
              <li key={e.id} className="flex items-start justify-between gap-3 px-3 py-2 text-sm">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-1.5"><Badge tone={types[e.entry_type]?.tone}>{types[e.entry_type]?.label ?? e.entry_type}</Badge><span className="tnum text-on-surface-variant">{fmtDay(e.entry_date)}</span></div>
                  <div className="mt-0.5">{e.description ?? "—"}</div>
                  <div className="flex flex-wrap gap-2 text-xs text-on-surface-variant">
                    {e.invoice_id && <Link to={`/billing/invoices/${e.invoice_id}`} className="font-mono text-primary hover:underline">{e.invoice_number ?? "Invoice"}</Link>}
                    {e.work_order_id && <Link to={`/work-orders/${e.work_order_id}`} className="text-primary hover:underline">Work Order</Link>}
                    <span>{e.created_by_name ?? "Sistem"}</span>
                  </div>
                </div>
                <div className="shrink-0 text-right"><div className="tnum font-semibold">{fmtSigned(e.amount)}</div><div className="text-xs tnum text-on-surface-variant">saldo {fmtMoney(e.running)}</div></div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Drawer>
  );
}

type DepositType = "received" | "deducted" | "refunded" | "adjustment";

function DepositEntryDialog({ propertyId, party, balance, balances, onClose }: { propertyId: string; party?: Party & { label?: string }; balance?: number; balances: PartyBalance[]; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [p, setP] = useState<Party>({ tenant_id: party?.tenant_id ?? null, unit_location_id: party?.unit_location_id ?? null });
  const [f, setF] = useState({ entry_type: "received" as DepositType, amount: null as number | null, decrease: false, entry_date: todayISO(), reason: "", work_order_id: null as string | null });
  const [busy, setBusy] = useState(false);
  const hasParty = !!(p.tenant_id || p.unit_location_id);
  const current = balance ?? balances.find((b) => (p.tenant_id ? b.tenant_id === p.tenant_id : !b.tenant_id && b.unit_location_id === p.unit_location_id))?.balance ?? 0;
  const after = f.amount ? balanceAfter(current, f.entry_type, f.amount, f.decrease) : current;
  const decreasing = f.entry_type === "deducted" || f.entry_type === "refunded" || (f.entry_type === "adjustment" && f.decrease);
  const insufficient = decreasing && after < 0;
  const valid = hasParty && !!f.amount && f.amount > 0 && !!f.reason.trim() && !insufficient;
  const submit = async () => {
    if (!valid || !f.amount) return;
    setBusy(true);
    try {
      await api("billing/deposits/entries", {
        body: { property_id: propertyId, tenant_id: p.tenant_id, unit_location_id: p.tenant_id ? null : p.unit_location_id, entry_type: f.entry_type, amount: apiAmount(f.entry_type, f.amount, f.decrease), entry_date: f.entry_date || null, reason: f.reason.trim(), work_order_id: f.work_order_id },
        idempotencyKey: uuid(),
      });
      invalidate("all", "list", "one");
      toast.action("created", `Mutasi deposit (${DEPOSIT_TYPES[f.entry_type]?.label.toLowerCase()})`);
      onClose();
    } catch (e) {
      toast.failed("created", e, "Mutasi deposit");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Catat mutasi deposit" description={party?.label ? `${party.label} · saldo ${fmtMoney(current)}` : "Deposit dari invoice (item deposit) tercatat otomatis saat dibayar."}>
        <div className="space-y-4">
          {!party && <Field label="Tenant / unit" required><PartyPicker propertyId={propertyId} value={p} onChange={setP} /></Field>}
          <Field label="Jenis" required help={DEPOSIT_INPUT_TYPES.find((t) => t.value === f.entry_type)?.help}>
            <Segmented<DepositType> value={f.entry_type} onChange={(v) => setF({ ...f, entry_type: v, decrease: false })} options={DEPOSIT_INPUT_TYPES.map((t) => ({ value: t.value, label: t.label }))} />
          </Field>
          {f.entry_type === "adjustment" && (
            <Field label="Arah penyesuaian">
              <Segmented<"up" | "down"> value={f.decrease ? "down" : "up"} onChange={(v) => setF({ ...f, decrease: v === "down" })} options={[{ value: "up", label: "Tambah saldo" }, { value: "down", label: "Kurangi saldo" }]} />
            </Field>
          )}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Nominal" required error={insufficient ? `Saldo deposit tidak cukup (saldo ${fmtMoney(current)})` : undefined}>
              <MoneyInput value={f.amount} onChange={(v) => setF({ ...f, amount: v })} aria-label="Nominal" />
            </Field>
            <Field label="Tanggal"><DatePicker value={f.entry_date} max={todayISO()} onChange={(v) => setF({ ...f, entry_date: v })} aria-label="Tanggal mutasi" /></Field>
          </div>
          <Field label="Alasan" required><Textarea rows={2} value={f.reason} onChange={(e) => setF({ ...f, reason: e.target.value })} placeholder={f.entry_type === "deducted" ? "mis. Perbaikan dinding & cat ulang saat move-out unit 1203" : f.entry_type === "refunded" ? "mis. Pengembalian deposit move-out, transfer BCA" : "mis. Deposit sewa diterima tunai"} /></Field>
          {(f.entry_type === "deducted" || f.entry_type === "adjustment") && (
            <Field label="Work Order terkait" help="Bukti kerusakan / perbaikan (inspeksi move-out).">
              <WorkOrderPicker propertyId={propertyId} value={f.work_order_id} onChange={(id) => setF({ ...f, work_order_id: id })} />
            </Field>
          )}
          {hasParty && f.amount ? <p className="text-sm">Saldo setelah mutasi: <b className="tnum">{fmtMoney(after)}</b></p> : null}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
