// Billing › Penalties (PRD P4 v2.1 P4-PND-01..02): denda keterlambatan yang dihitung otomatis pada invoice lewat jatuh tempo —
// tagihkan (invoice denda per tenant/unit, hanya selisih yang belum ditagihkan) atau hapuskan/waive dengan alasan
// (billing.penalties.waive); dan aturan denda per property (persen/nominal, per hari/bulan/sekali, tenggang, batas).
import { useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, DatePicker, DialogFooter, Drawer, Field, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { ReasonDialog, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { useAll, useInvalidate, useList } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { statusOptions } from "@/lib/status";
import { PenaltyRuleDialog } from "./PenaltyRuleDialog";
import { InvoiceLink, PropertySelect, SummaryTile, TenantFilter } from "./shared";
import { addDaysISO, invoiceTypeLabel, money, todayISO, useBillingSettings, usePropertyName, type BillPenaltiesResult, type Penalty, type PenaltyRule } from "./types";

export default function PenaltiesPage() {
  const { t } = useTranslation();
  const [sp] = useSearchParams();
  const [tab, setTab] = useState(sp.get("tab") === "rules" ? "rules" : "penalties");
  return (
    <div>
      <PageHeader title={t("nav.penalties")} subtitle="Denda keterlambatan dihitung otomatis setiap hari pada invoice lewat jatuh tempo (zona waktu property) dan berhenti saat invoice lunas. Tagihkan lewat invoice denda atau hapuskan dengan alasan." />
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="penalties">Denda berjalan</TabsTrigger>
          <TabsTrigger value="rules">Aturan denda</TabsTrigger>
        </TabsList>
        <TabsContent value="penalties"><PenaltiesTab /></TabsContent>
        <TabsContent value="rules"><RulesTab /></TabsContent>
      </Tabs>
    </div>
  );
}

function PenaltiesTab() {
  const { propertyId, can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const f = useUrlFilters();
  const list = useList<Penalty>("billing/penalties", {
    property_id: propertyId ?? undefined,
    status: f.get("status") || undefined,
    unbilled: f.get("unbilled") === "true" || undefined,
    tenant_id: f.get("tenant_id") || undefined,
    invoice_id: f.get("invoice_id") || undefined,
  });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [bill, setBill] = useState<{ penalties: Penalty[] | null } | null>(null);
  const [waive, setWaive] = useState<Penalty | null>(null);
  const [busy, setBusy] = useState(false);
  const canBill = can("billing.penalties.manage") && can("billing.invoices.create");
  const unbilledTotal = rows.reduce((a, p) => a + (p.allowed_actions.includes("bill") ? p.unbilled_amount : 0), 0);

  const doWaive = async (reason: string) => {
    if (!waive) return;
    setBusy(true);
    try {
      await api(`billing/penalties/${waive.id}/waive`, { body: { reason }, idempotencyKey: uuid() });
      invalidate("list", "one", "all");
      toast.success(`Denda ${money(waive.unbilled_amount || waive.accrued_amount)} pada ${waive.invoice_number ?? "invoice"} dihapuskan`);
      setWaive(null);
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };

  const columns = useMemo<ColumnDef<Penalty, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): invoice + tenant satu kolom, satu badge status (alasan waive di tooltip).
    // Status invoice, unit & nomor invoice denda ada di detail invoice.
    { id: "invoice", header: "Invoice", meta: { mobile: "primary" }, cell: ({ row: { original: p } }) => <CellTitle code={<InvoiceLink id={p.invoice_id} number={p.invoice_number} />} title={p.tenant_name ?? "Tanpa tenant"} /> },
    { id: "rule", header: "Aturan", meta: { mobile: "hidden" }, cell: ({ row: { original: p } }) => <CellText max={180}>{p.rule_name}</CellText> },
    { id: "days_late", header: "Terlambat", size: 100, meta: { mobile: "hidden" }, cell: ({ row: { original: p } }) => <span className="tnum whitespace-nowrap text-sm">{p.days_late} hari</span> },
    { id: "accrued", header: "Akrual", size: 120, meta: { mobile: "secondary" }, cell: ({ row: { original: p } }) => <div className="tnum whitespace-nowrap text-right font-semibold">{money(p.accrued_amount)}</div> },
    { id: "billed", header: "Ditagihkan", size: 120, meta: { mobile: "hidden" }, cell: ({ row: { original: p } }) => <div className="tnum whitespace-nowrap text-right" title={p.billed_invoice_number ?? undefined}>{money(p.billed_amount)}</div> },
    { id: "unbilled", header: "Belum ditagih", size: 130, meta: { mobile: "secondary" }, cell: ({ row: { original: p } }) => <div className="tnum whitespace-nowrap text-right font-semibold">{p.unbilled_amount > 0 ? money(p.unbilled_amount) : "—"}</div> },
    { id: "status", header: "Status", size: 130, meta: { mobile: "status" }, cell: ({ row: { original: p } }) => <span className="inline-flex" title={p.waive_reason ? `${p.waive_reason}${p.waived_by_name ? ` · ${p.waived_by_name}` : ""}` : undefined}><StatusBadge objectType="invoice_penalty" status={p.status} /></span> },
  ], []);

  return (
    <div className="space-y-3">
      <FilterBar
        spec={{
          status: statusOptions("invoice_penalty"),
          presets: [
            { key: "all", label: "Semua", params: {} },
            { key: "unbilled", label: "Belum ditagihkan", params: { unbilled: "true" } },
          ],
          extra: <TenantFilter propertyId={propertyId} value={f.get("tenant_id")} onChange={(v) => f.set({ tenant_id: v })} />,
        }}
      />
      {canBill && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-[var(--radius-lg)] border border-border px-4 py-3">
          <div className="text-sm">
            <div className="font-semibold">Tagihkan denda</div>
            <div className="text-on-surface-variant">Satu invoice denda per tenant/unit; hanya selisih yang belum pernah ditagihkan. Pilih baris untuk menagih sebagian{unbilledTotal > 0 ? ` · belum ditagih pada daftar ini ${money(unbilledTotal)}` : ""}.</div>
          </div>
          <Button icon="request_quote" onClick={() => setBill({ penalties: null })}>Tagihkan semua…</Button>
        </div>
      )}
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={f.isFiltered}
        empty={{ icon: "gavel", title: "Belum ada denda", description: "Denda muncul otomatis untuk invoice lewat jatuh tempo bila ada aturan denda aktif pada property." }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        selectable={canBill}
        bulkActions={canBill ? (ids, clear) => (
          <Button size="sm" icon="request_quote" onClick={() => { const sel = rows.filter((r) => ids.includes(r.id) && r.allowed_actions.includes("bill")); if (!sel.length) toast.info("Tidak ada denda yang dapat ditagihkan pada pilihan ini"); else { setBill({ penalties: sel }); clear(); } }}>Tagihkan terpilih</Button>
        ) : undefined}
        rowActions={(p) => [
          ...(p.allowed_actions.includes("bill") ? [{ label: "Tagihkan…", icon: "request_quote", onSelect: () => setBill({ penalties: [p] }) }] : []),
          ...(p.allowed_actions.includes("waive") ? [{ label: "Hapuskan (waive)…", icon: "money_off", destructive: true, onSelect: () => setWaive(p) }] : []),
        ]}
      />
      {bill && <BillPenaltiesDialog penalties={bill.penalties} onClose={() => setBill(null)} />}
      <ReasonDialog open={!!waive} onOpenChange={(o) => !o && setWaive(null)} title={`Hapuskan denda ${waive?.invoice_number ?? ""}`} description={waive ? `Denda ${money(waive.unbilled_amount || waive.accrued_amount)} (${waive.rule_name}) dihapuskan dan tidak akan ditagihkan. Alasan tercatat di audit.` : undefined} label="Alasan penghapusan" confirmLabel="Hapuskan denda" destructive loading={busy} onConfirm={doWaive} />
    </div>
  );
}

/** Tagihkan denda (P4-PND-02): `penalties` null = semua denda belum ditagihkan di property. */
function BillPenaltiesDialog({ penalties, onClose }: { penalties: Penalty[] | null; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const nav = useNavigate();
  const { propertyId, properties, can } = useAuth();
  const propertyName = usePropertyName();
  const [pid, setPid] = useState(propertyId ?? properties[0]?.id ?? "");
  const settings = useBillingSettings(penalties?.[0]?.property_id ?? pid ?? null);
  const [due, setDue] = useState("");
  const [issueNow, setIssueNow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<BillPenaltiesResult | null>(null);
  const byProperty = useMemo(() => {
    const m = new Map<string, Penalty[]>();
    for (const p of penalties ?? []) m.set(p.property_id, [...(m.get(p.property_id) ?? []), p]);
    return m;
  }, [penalties]);
  const total = (penalties ?? []).reduce((a, p) => a + p.unbilled_amount, 0);
  const parties = new Set((penalties ?? []).map((p) => `${p.tenant_id ?? ""}|${p.unit_location_id ?? ""}`)).size;
  const defaultDue = addDaysISO(todayISO(), settings.data?.default_due_days ?? 14);
  const targetProps = penalties ? [...byProperty.keys()] : [pid];
  const canIssue = targetProps.every((x) => can("billing.invoices.issue", x));

  const submit = async () => {
    setBusy(true);
    setError(null);
    const agg: BillPenaltiesResult = { invoice_ids: [], penalties: 0, total_amount: 0 };
    try {
      for (const prop of targetProps) {
        const ids = penalties ? (byProperty.get(prop) ?? []).map((p) => p.id) : [];
        const out = await api<BillPenaltiesResult>("billing/penalties/bill", { body: { property_id: prop, penalty_ids: ids, due_date: due || null, issue_now: issueNow && canIssue }, idempotencyKey: uuid() });
        agg.invoice_ids.push(...out.invoice_ids);
        agg.penalties += out.penalties;
        agg.total_amount += out.total_amount;
      }
      invalidate("list", "one", "all");
      toast.success(`${agg.penalties} denda ditagihkan → ${agg.invoice_ids.length} invoice denda (${money(agg.total_amount)})`);
      setDone(agg);
    } catch (e) {
      setError((e as Error).message);
      if (agg.invoice_ids.length) invalidate("list", "one", "all");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Drawer open onClose={onClose} width={560} title="Tagihkan denda" description="Denda yang belum ditagihkan dijadikan invoice denda (satu per tenant/unit, tanpa pajak).">
      {done ? (
        <div className="space-y-4">
          <Alert variant="success" title="Invoice denda dibuat">{done.penalties} denda → {done.invoice_ids.length} invoice ({money(done.total_amount)}){issueNow ? ", sudah diterbitkan." : ", sebagai draft."}</Alert>
          <DialogFooter>
            <Button variant="secondary" icon="request_quote" onClick={() => { nav("/billing/invoices?type=penalty"); onClose(); }}>Lihat invoice denda</Button>
            <Button onClick={onClose}>Selesai</Button>
          </DialogFooter>
        </div>
      ) : (
        <div className="space-y-4">
          {error && <Alert variant="critical" title="Gagal menagihkan">{error}</Alert>}
          {penalties ? (
            <div className="grid grid-cols-3 gap-2">
              <SummaryTile label="Denda" value={penalties.length} />
              <SummaryTile label="Invoice" value={parties} sub="per tenant/unit" />
              <SummaryTile label="Total" value={money(total)} />
            </div>
          ) : (
            <>
              <PropertySelect value={pid} onChange={setPid} required />
              <Alert variant="info">Semua denda yang belum ditagihkan{properties.length > 1 ? ` di ${propertyName(pid)}` : ""} akan ditagihkan.</Alert>
            </>
          )}
          {penalties && byProperty.size > 1 && <p className="text-xs text-on-surface-variant">Pilihan mencakup {byProperty.size} property — ditagihkan per property.</p>}
          <Field label="Jatuh tempo invoice denda" help={due ? undefined : `Kosong = ${defaultDue} (hari ini + ${settings.data?.default_due_days ?? 14} hari)`}>
            <DatePicker value={due} min={todayISO()} onChange={setDue} />
          </Field>
          {canIssue && <Checkbox label="Terbitkan langsung (tenant menerima notifikasi)" checked={issueNow} onCheckedChange={setIssueNow} />}
          <DialogFooter>
            <Button variant="secondary" onClick={onClose}>Batal</Button>
            <Button icon="request_quote" loading={busy} disabled={!penalties && !pid} onClick={submit}>{penalties ? `Tagihkan ${money(total)}` : "Tagihkan semua"}</Button>
          </DialogFooter>
        </div>
      )}
    </Drawer>
  );
}

function RulesTab() {
  const { propertyId, properties, can } = useAuth();
  const propertyName = usePropertyName();
  const list = useAll<PenaltyRule>("billing/penalty-rules", { property_id: propertyId ?? undefined });
  const [edit, setEdit] = useState<PenaltyRule | "new" | null>(null);
  const canManage = can("billing.penalties.manage");
  const multi = !propertyId && properties.length > 1;
  const columns = useMemo<ColumnDef<PenaltyRule, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): nama & ketentuan masing-masing satu baris.
    { id: "name", header: "Aturan", meta: { mobile: "primary" }, cell: ({ row: { original: r } }) => <CellText max={200} className="font-medium">{r.name}</CellText> },
    { id: "summary", header: "Ketentuan", meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <CellText max={260} muted>{r.summary}</CellText> },
    ...(multi ? [{ id: "property", header: "Property", size: 180, meta: { mobile: "secondary" as const }, cell: ({ row: { original: r } }: { row: { original: PenaltyRule } }) => <CellText max={180}>{r.property_name || propertyName(r.property_id)}</CellText> }] : []),
    { id: "types", header: "Jenis invoice", size: 220, meta: { mobile: "hidden" }, cell: ({ row: { original: r } }) => <CellText max={220}>{r.invoice_types.length ? r.invoice_types.map(invoiceTypeLabel).join(", ") : "Semua (kecuali denda)"}</CellText> },
    { id: "status", header: "Status", size: 110, meta: { mobile: "status" }, cell: ({ row: { original: r } }) => (r.is_active ? <Badge tone="success">Aktif</Badge> : <Badge tone="neutral">Nonaktif</Badge>) },
  ], [multi, propertyName]);
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-on-surface-variant">Satu property dapat memiliki beberapa aturan (mis. per jenis invoice). Perubahan aturan berlaku untuk akrual berikutnya.</p>
        {canManage && <Button icon="add" onClick={() => setEdit("new")}>Tambah aturan</Button>}
      </div>
      <DataGrid
        columns={columns}
        rows={list.data ?? []}
        rowId={(r) => r.id}
        onRowClick={(r) => { setEdit(r); }}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        empty={{ icon: "gavel", title: "Belum ada aturan denda", description: "Tanpa aturan aktif, invoice lewat jatuh tempo tidak dikenai denda.", action: canManage ? <Button icon="add" onClick={() => setEdit("new")}>Tambah aturan</Button> : undefined }}
        rowActions={canManage ? (r) => [{ label: "Edit", icon: "edit", onSelect: () => setEdit(r) }] : undefined}
      />
      {edit && <PenaltyRuleDialog key={edit === "new" ? "new" : edit.id} rule={edit === "new" ? null : edit} canManage={canManage} onClose={() => setEdit(null)} />}
    </div>
  );
}
