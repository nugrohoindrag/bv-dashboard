// Finance › Accounting (PRD P4 v2.1 §9 P4-INT-02, P4-INT-04; D-P4-03 bukan general ledger): Pemetaan Akun (kode akun pelanggan per
// jenis transaksi — default organization, override per property) · Jurnal (pratinjau siap-jurnal debit = kredit + ekspor CSV/XLSX
// untuk Accurate/Jurnal/dll.) · Webhook keluar. Tab di URL: /finance/accounting/:tab (mappings | journal | webhooks).
import { useMemo, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Card, CardContent, DatePicker, Input, NativeSelect, TBody, TD, TH, THead, TR, Table, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { CardSkeleton, EmptyState, KpiSkeleton, QueryErrorState } from "@/components/bv/states";
import { useToast } from "@/components/bv/common";
import { CellText } from "@/components/bv/cells";
import { useAll, useInvalidate } from "@/api/hooks";
import { api, downloadFile } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtMoney, fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import { PropertySelect, StatTile } from "./fin-ui";
import { fmtDay, fmtMoneyTile, hasOrgWide, useGet, usePropertyParam } from "./fin-utils";
import { JOURNAL_SOURCE, mappingGroup, mappingSource, type Journal, type Mapping } from "./finance-model";
import { WebhooksTab } from "./accounting-webhooks";

const TABS = ["mappings", "journal", "webhooks"] as const;
type Tab = (typeof TABS)[number];

export default function AccountingPage() {
  const { tab: param } = useParams();
  const nav = useNavigate();
  const tab: Tab = (TABS as readonly string[]).includes(param ?? "") ? (param as Tab) : "mappings";
  return (
    <div>
      <PageHeader title="Accounting" subtitle="Integrasi sistem akuntansi pelanggan: pemetaan kode akun, ekspor siap-jurnal, dan webhook event keuangan. BuildingVision bukan general ledger." />
      <Tabs value={tab} onValueChange={(v) => nav(`/finance/accounting/${v}`)}>
        <TabsList>
          <TabsTrigger value="mappings">Pemetaan Akun</TabsTrigger>
          <TabsTrigger value="journal">Jurnal</TabsTrigger>
          <TabsTrigger value="webhooks">Webhook</TabsTrigger>
        </TabsList>
        <TabsContent value="mappings"><MappingsTab /></TabsContent>
        <TabsContent value="journal"><JournalTab /></TabsContent>
        <TabsContent value="webhooks"><WebhooksTab /></TabsContent>
      </Tabs>
    </div>
  );
}

// ---------- pemetaan akun ----------
function MappingsTab() {
  const { properties, principal, can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const scope = sp.get("scope") ?? ""; // "" = default organization; selain itu property_id
  const q = useAll<Mapping>("finance/account-mappings", { property_id: scope || undefined });
  const canEdit = scope ? can("billing.accounting.manage", scope) : hasOrgWide(principal, "billing.accounting.manage");
  const setScope = (v: string) => { const n = new URLSearchParams(sp); if (v) n.set("scope", v); else n.delete("scope"); setSp(n, { replace: true }); };
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <NativeSelect className="w-full sm:w-72" value={scope} onChange={(e) => setScope(e.target.value)} aria-label="Cakupan pemetaan">
          <option value="">Default organization</option>
          {properties.map((p) => <option key={p.id} value={p.id}>Override · {p.name}</option>)}
        </NativeSelect>
        <span className="text-sm text-on-surface-variant">{scope ? "Kode kosong = ikut default organization." : "Berlaku untuk semua property kecuali di-override."}</span>
      </div>
      {q.isLoading ? <CardSkeleton lines={8} /> : q.error && !q.data ? <QueryErrorState error={q.error} onRetry={() => q.refetch()} /> : q.data ? (
        <MappingEditor key={`${scope}:${q.dataUpdatedAt}`} items={q.data} scope={scope} canEdit={canEdit} />
      ) : null}
    </div>
  );
}

function MappingEditor({ items, scope, canEdit }: { items: Mapping[]; scope: string; canEdit: boolean }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [draft, setDraft] = useState<Record<string, { code: string; name: string }>>(() => Object.fromEntries(items.map((m) => [m.key, { code: m.account_code, name: m.account_name }])));
  const [busy, setBusy] = useState(false);
  const own = (m: Mapping) => !m.is_default && !m.inherited; // pemetaan tersimpan pada cakupan ini
  const changed = items.filter((m) => { const d = draft[m.key]; return d && (d.code.trim() !== m.account_code || d.name.trim() !== m.account_name); });
  const groups = useMemo(() => {
    const out: Record<string, Mapping[]> = {};
    for (const m of items) (out[mappingGroup(m.key)] ??= []).push(m);
    return out;
  }, [items]);
  const save = async (list: { key: string; account_code: string; account_name: string }[], done: string) => {
    setBusy(true);
    try {
      await api("finance/account-mappings", { method: "PUT", query: { property_id: scope || undefined }, body: { mappings: list } });
      invalidate("all", "one");
      toast.success(done);
    } catch (e) {
      toast.failed("saved", e, "Pemetaan akun");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card>
      <CardContent className="space-y-3 px-0 pb-0 pt-3">
        {!canEdit && <div className="px-5"><Alert variant="info">{scope ? "Butuh izin billing.accounting.manage pada property ini untuk mengubah." : "Pemetaan organization butuh izin billing.accounting.manage tingkat organization."}</Alert></div>}
        <Table data-testid="account-mappings">
          <THead><tr><TH>Akun BuildingVision</TH><TH className="w-40">Kode akun</TH><TH>Nama akun</TH><TH>Sumber</TH>{canEdit && <TH aria-label="Aksi" />}</tr></THead>
          <TBody>
            {Object.entries(groups).map(([g, list]) => (
              <GroupRows key={g} title={g} list={list} draft={draft} canEdit={canEdit} scoped={!!scope} own={own} busy={busy}
                onChange={(key, patch) => setDraft((d) => ({ ...d, [key]: { ...d[key], ...patch } }))}
                onReset={(m) => void save([{ key: m.key, account_code: "", account_name: "" }], `Pemetaan ${m.label} dikembalikan ke ${scope ? "default organization" : "bawaan sistem"}`)} />
            ))}
          </TBody>
        </Table>
        {canEdit && (
          <div className="flex flex-wrap items-center justify-end gap-2 border-t border-border px-5 py-3">
            {changed.length > 0 && <span className="mr-auto text-sm text-on-surface-variant">{changed.length} perubahan belum disimpan</span>}
            <Button variant="ghost" disabled={!changed.length || busy} onClick={() => setDraft(Object.fromEntries(items.map((m) => [m.key, { code: m.account_code, name: m.account_name }])))}>Batalkan perubahan</Button>
            <Button icon="save" disabled={!changed.length} loading={busy} onClick={() => void save(changed.map((m) => ({ key: m.key, account_code: draft[m.key].code.trim(), account_name: draft[m.key].name.trim() })), `${changed.length} pemetaan akun disimpan`)}>Simpan</Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function GroupRows({ title, list, draft, canEdit, scoped, own, busy, onChange, onReset }: {
  title: string; list: Mapping[]; draft: Record<string, { code: string; name: string }>; canEdit: boolean; scoped: boolean; own: (m: Mapping) => boolean; busy: boolean;
  onChange: (key: string, patch: Partial<{ code: string; name: string }>) => void; onReset: (m: Mapping) => void;
}) {
  return (
    <>
      <TR className="bg-surface-container-low"><TD colSpan={canEdit ? 5 : 4} className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">{title}</TD></TR>
      {list.map((m) => {
        const d = draft[m.key] ?? { code: m.account_code, name: m.account_name };
        const src = mappingSource(m, scoped);
        const dirty = d.code.trim() !== m.account_code || d.name.trim() !== m.account_name;
        return (
          <TR key={m.key} className={cn(dirty && "bg-primary-soft")}>
            <TD><CellText max={260} className="font-medium" title={`${m.label} (${m.key})`}>{m.label}</CellText></TD>
            <TD className="p-1">{canEdit ? <Input className="h-8 font-mono text-sm" value={d.code} maxLength={40} onChange={(e) => onChange(m.key, { code: e.target.value })} aria-label={`Kode akun ${m.label}`} /> : <span className="whitespace-nowrap font-mono">{m.account_code}</span>}</TD>
            <TD className="p-1">{canEdit ? <Input className="h-8 text-sm" value={d.name} onChange={(e) => onChange(m.key, { name: e.target.value })} aria-label={`Nama akun ${m.label}`} /> : <CellText max={220}>{m.account_name}</CellText>}</TD>
            <TD><Badge tone={src.tone}>{src.label}</Badge></TD>
            {canEdit && <TD className="text-right">{own(m) && <Button size="sm" variant="ghost" icon="restart_alt" disabled={busy} onClick={() => onReset(m)}>Reset</Button>}</TD>}
          </TR>
        );
      })}
    </>
  );
}

// ---------- jurnal ----------
function JournalTab() {
  const toast = useToast();
  const { can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const [pid, setPid] = usePropertyParam();
  const from = sp.get("from") ?? "";
  const to = sp.get("to") ?? "";
  const q = useGet<Journal>("finance/journal", { property_id: pid ?? undefined, from: from || undefined, to: to || undefined });
  const j = q.data;
  const [busy, setBusy] = useState<string | null>(null);
  const set = (patch: Record<string, string | null>) => { const n = new URLSearchParams(sp); for (const [k, v] of Object.entries(patch)) { if (v) n.set(k, v); else n.delete(k); } setSp(n, { replace: true }); };
  const balanced = j ? j.total_debit === j.total_credit : true;
  const exportFile = async (format: "csv" | "xlsx") => {
    setBusy(format);
    try {
      await downloadFile("finance/journal/export", { property_id: pid ?? undefined, from: from || j?.from, to: to || j?.to, format }, `jurnal.${format}`);
      toast.action("exported", "Jurnal");
    } catch (e) {
      toast.failed("exported", e, "Jurnal");
    } finally {
      setBusy(null);
    }
  };
  const canExport = can("billing.accounting.export", pid ?? undefined);
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <PropertySelect value={pid} onChange={setPid} allowAll />
        <span className="inline-flex items-center gap-1">
          <DatePicker className="w-40" value={from || j?.from || ""} onChange={(v) => set({ from: v || null, to: to || j?.to || null })} aria-label="Dari tanggal" />
          <span className="text-on-surface-variant">–</span>
          <DatePicker className="w-40" value={to || j?.to || ""} onChange={(v) => set({ to: v || null, from: from || j?.from || null })} aria-label="Sampai tanggal" />
        </span>
        <span className="text-xs text-on-surface-variant">Default bulan berjalan · maks. 1 tahun</span>
        {canExport && (
          <span className="ml-auto flex gap-2">
            <Button variant="secondary" icon="download" loading={busy === "csv"} disabled={!j} onClick={() => void exportFile("csv")}>CSV</Button>
            <Button variant="secondary" icon="table_view" loading={busy === "xlsx"} disabled={!j} onClick={() => void exportFile("xlsx")}>XLSX</Button>
          </span>
        )}
      </div>
      {q.isLoading ? <><KpiSkeleton count={4} /><CardSkeleton lines={8} /></> : q.error && !j ? <QueryErrorState error={q.error} onRetry={() => q.refetch()} /> : j ? (
        <>
          <section aria-label="Ringkasan jurnal" className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <StatTile label="Jurnal" value={fmtNumber(j.entries)} sub={`${fmtDay(j.from)} – ${fmtDay(j.to)}`} />
            <StatTile label="Total debit" value={fmtMoneyTile(j.total_debit)} title={fmtMoney(j.total_debit)} />
            <StatTile label="Total kredit" value={fmtMoneyTile(j.total_credit)} title={fmtMoney(j.total_credit)} />
            <StatTile label="Keseimbangan" value={balanced ? "Seimbang" : "Tidak seimbang"} sub={balanced ? "debit = kredit" : `selisih ${fmtMoney(j.total_debit - j.total_credit)}`} tone={balanced ? "success" : "error"} />
          </section>
          {j.truncated && <Alert variant="info">Pratinjau menampilkan 500 baris pertama — ekspor CSV/XLSX untuk seluruh jurnal.</Alert>}
          {j.lines.length === 0 ? <EmptyState icon="sync_alt" title="Tidak ada transaksi pada periode ini" description="Jurnal diturunkan dari invoice terbit/batal, pembayaran, refund, credit note, saldo kredit, sinking fund, dan deposit manual." /> : (
            <Card>
              <CardContent className="px-0 pb-0 pt-2">
                <Table data-testid="journal-table">
                  <THead><tr><TH>Tanggal</TH><TH>No. jurnal</TH><TH>Kode</TH><TH>Nama akun</TH><TH className="bv-num">Debit</TH><TH className="bv-num">Kredit</TH><TH>Keterangan</TH><TH>Pihak</TH><TH>Sumber</TH></tr></THead>
                  <TBody>
                    {j.lines.map((l, i) => {
                      const first = i === 0 || j.lines[i - 1].journal_no !== l.journal_no || j.lines[i - 1].date !== l.date;
                      return (
                        <TR key={i} className={cn(first && i > 0 && "[&>td]:border-t-2")}>
                          <TD className="whitespace-nowrap tnum">{first ? fmtDay(l.date) : ""}</TD>
                          <TD className="whitespace-nowrap font-mono text-[13px]">{first ? l.journal_no : ""}</TD>
                          <TD className="whitespace-nowrap font-mono text-[13px]">{l.account_code}</TD>
                          <TD className={cn(l.credit && !l.debit && "pl-6")}><CellText max={220}>{l.account_name}</CellText></TD>
                          <TD className="bv-num tnum whitespace-nowrap">{l.debit ? fmtMoney(l.debit) : ""}</TD>
                          <TD className="bv-num tnum whitespace-nowrap">{l.credit ? fmtMoney(l.credit) : ""}</TD>
                          <TD>{first ? <CellText max={260} title={[l.description, l.reference].filter(Boolean).join(" · ")}>{l.description}</CellText> : ""}</TD>
                          <TD>{first ? <CellText max={160}>{l.party || "—"}</CellText> : ""}</TD>
                          <TD className="whitespace-nowrap text-xs text-on-surface-variant">{first ? JOURNAL_SOURCE[l.source_type] ?? l.source_type : ""}</TD>
                        </TR>
                      );
                    })}
                    <TR className="bg-surface-container-low font-semibold"><TD colSpan={4}>Total</TD><TD className="bv-num tnum whitespace-nowrap">{fmtMoney(j.total_debit)}</TD><TD className="bv-num tnum whitespace-nowrap">{fmtMoney(j.total_credit)}</TD><TD colSpan={3}>{balanced ? <span className="inline-flex items-center gap-1 text-on-success-container"><Icon name="check_circle" size={14} aria-hidden />Seimbang</span> : "Tidak seimbang"}</TD></TR>
                  </TBody>
                </Table>
              </CardContent>
            </Card>
          )}
          <p className="text-xs text-on-surface-variant">Kode akun mengikuti tab Pemetaan Akun (override property → default organization → bawaan). Ekspor tercatat di audit.</p>
        </>
      ) : null}
    </div>
  );
}
