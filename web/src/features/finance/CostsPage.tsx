// Finance › Operating Cost (PRD P4 v2.1 P4-CST-02..04; D-P4-07 tanpa payroll): ringkasan biaya operasional setahun per kategori
// (maintenance, utilitas, keamanan, kebersihan, staf, administrasi, lainnya — termasuk WO selesai & consumable cleaning) dan
// entri biaya manual per property/kategori/tanggal dengan bukti (lampiran cost_entry). Route: /finance/costs · /finance/costs/:id
// (tautan drill-down Budget vs Actual); filter ?category&from&to&q&property_id di URL.
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, ConfirmDialog, DatePicker, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, SearchInput, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { CellText, CellTitle } from "@/components/bv/cells";
import { KeyValue, useToast } from "@/components/bv/common";
import { AttachmentGrid, PhotoEvidenceUploader } from "@/components/bv/checklist";
import { CardSkeleton } from "@/components/bv/states";
import { uploadAttachment, useAttachments, useInvalidate, useList } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime, fmtMoney } from "@/lib/format";
import { MoneyInput, PropertySelect, StatTile, YearSelect } from "./fin-ui";
import { MONTHS_SHORT, currentYear, fmtAxisMoney, fmtDay, fmtMoneyTile, fmtPct, todayISO, useGet, usePropertyParam, yearBounds, yearOptions } from "./fin-utils";
import { FALLBACK_CATEGORIES, type Categories, type CostEntry, type OperatingCost } from "./finance-model";

export default function CostsPage() {
  const { id } = useParams();
  const nav = useNavigate();
  const { can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const [pid, setPid] = usePropertyParam();
  const category = sp.get("category") ?? "";
  const from = sp.get("from") ?? "";
  const to = sp.get("to") ?? "";
  const q = sp.get("q") ?? "";
  const [text, setText] = useState(q);
  const set = (patch: Record<string, string | null>) => {
    const n = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (v) n.set(k, v);
      else n.delete(k);
    }
    setSp(n, { replace: true });
  };
  const cats = useGet<Categories>("finance/categories", {}, { staleTime: 10 * 60_000, enabled: can("billing.budgets.view") });
  const costCats = (cats.data ?? FALLBACK_CATEGORIES).cost;
  const catLabel = useMemo(() => (k: string) => costCats.find((c) => c.key === k)?.label ?? k, [costCats]);
  const list = useList<CostEntry>("finance/costs", { property_id: pid ?? undefined, category: category || undefined, from: from || undefined, to: to || undefined, q: q || undefined }, { limit: id ? 100 : 50 });
  const rows = useMemo(() => list.data?.pages.flatMap((p) => p.data) ?? [], [list.data]);
  const canManage = can("billing.costs.manage", pid ?? undefined);
  const [edit, setEdit] = useState<CostEntry | "new" | null>(null);
  const selected = id ? rows.find((r) => r.id === id) : undefined;
  const pages = list.data?.pages.length ?? 0;
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = list;
  // deep link /finance/costs/:id: tidak ada GET per id → cari di halaman berikutnya (maks. 10 halaman)
  useEffect(() => {
    if (id && !selected && hasNextPage && !isFetchingNextPage && pages > 0 && pages < 10) void fetchNextPage();
  }, [id, selected, hasNextPage, isFetchingNextPage, pages, fetchNextPage]);
  const notFound = !!id && !selected && !list.isLoading && !hasNextPage && !isFetchingNextPage;
  const filtered = !!category || !!from || !!to || !!q;
  const close = () => nav(`/finance/costs${sp.toString() ? `?${sp.toString()}` : ""}`);
  const columns = useMemo<ColumnDef<CostEntry, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): referensi (· property) di atas keterangan satu baris, penerima kolom sendiri, nominal
    // rata kanan, jumlah bukti. Pencatat & waktu catat ada di drawer detail biaya.
    { id: "entry_date", header: "Tanggal", size: 120, meta: { mobile: "secondary" }, cell: ({ row }) => <span className="whitespace-nowrap tnum text-sm">{fmtDay(row.original.entry_date)}</span> },
    { id: "category", header: "Kategori", size: 140, meta: { mobile: "status" }, cell: ({ row }) => <CellText max={140} className="font-medium">{row.original.category_label || catLabel(row.original.category)}</CellText> },
    { id: "desc", header: "Keterangan", meta: { mobile: "primary" }, cell: ({ row: { original: c } }) => <CellTitle code={[c.reference, pid ? null : c.property_name].filter(Boolean).join(" · ") || undefined} title={c.description} /> },
    { id: "payee", header: "Penerima", meta: { mobile: "hidden" }, cell: ({ row }) => <CellText max={160} muted={!row.original.payee}>{row.original.payee || "—"}</CellText> },
    { id: "amount", header: "Nominal", size: 150, meta: { mobile: "secondary" }, cell: ({ row }) => <span className="block whitespace-nowrap text-right tnum font-semibold">{fmtMoney(row.original.amount)}</span> },
    { id: "attachments", header: "Bukti", size: 80, meta: { mobile: "hidden" }, cell: ({ row }) => (row.original.attachment_count > 0 ? <span className="inline-flex items-center gap-1 whitespace-nowrap text-sm"><Icon name="attach_file" size={14} aria-hidden />{row.original.attachment_count}</span> : <span className="text-xs text-on-surface-variant">—</span>) },
  ], [pid, catLabel]);
  return (
    <div className="space-y-4">
      <PageHeader title="Operating Cost" subtitle="Biaya operasional per kategori: work order selesai (actual cost), consumable cleaning, dan biaya manual dengan bukti — tanpa payroll." actions={canManage && <Button icon="add" onClick={() => setEdit("new")}>Catat biaya</Button>} />
      <CostSummary pid={pid} setPid={setPid} catLabel={catLabel} onCategory={(c, year) => { const r = yearBounds(year); set({ category: c, from: r.from, to: r.to }); }} />
      <div className="flex flex-wrap items-center gap-2">
        <form className="w-full sm:w-64" onSubmit={(e) => { e.preventDefault(); set({ q: text.trim() || null }); }}>
          <SearchInput placeholder="Cari keterangan / penerima / referensi…" value={text} onChange={(e) => setText(e.target.value)} aria-label="Cari biaya" />
        </form>
        <NativeSelect className="w-[calc(50%-4px)] sm:w-44" value={category} onChange={(e) => set({ category: e.target.value || null })} aria-label="Kategori">
          <option value="">Kategori: Semua</option>
          {costCats.map((c) => <option key={c.key} value={c.key}>{c.label}</option>)}
        </NativeSelect>
        <span className="inline-flex items-center gap-1">
          <DatePicker className="w-40" value={from} onChange={(v) => set({ from: v || null })} aria-label="Dari tanggal" />
          <span className="text-on-surface-variant">–</span>
          <DatePicker className="w-40" value={to} onChange={(v) => set({ to: v || null })} aria-label="Sampai tanggal" />
        </span>
        {filtered && <Button variant="ghost" size="sm" icon="replay" onClick={() => { setText(""); set({ category: null, from: null, to: null, q: null }); }}>Reset filter</Button>}
      </div>
      {notFound && <Alert variant="warning" title="Biaya tidak ditemukan" action={<Button size="sm" variant="ghost" onClick={close}>Tutup</Button>}>Entri biaya dari tautan tidak ada pada filter/property ini atau sudah dihapus.</Alert>}
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/finance/costs/${r.id}${sp.toString() ? `?${sp.toString()}` : ""}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={filtered}
        empty={{ icon: "price_check", title: "Belum ada biaya manual", description: "Catat biaya di luar work order — mis. tagihan PLN gedung, kontrak outsourcing keamanan/kebersihan, administrasi — beserta bukti.", action: canManage ? <Button icon="add" onClick={() => setEdit("new")}>Catat biaya</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowClassName={(r) => (r.id === id ? "bg-primary-soft" : undefined)}
        rowActions={(c) => [
          ...(c.allowed_actions.includes("update") ? [{ label: "Edit", icon: "edit", onSelect: () => setEdit(c) }] : []),
          { label: "Detail & bukti", icon: "visibility", onSelect: () => nav(`/finance/costs/${c.id}`) },
        ]}
      />
      {selected && <CostDrawer entry={selected} catLabel={catLabel} onClose={close} onEdit={() => setEdit(selected)} />}
      {edit && <CostDialog item={edit === "new" ? null : edit} propertyId={pid} categories={costCats} onClose={() => setEdit(null)} />}
    </div>
  );
}

// ---------- ringkasan tahunan per kategori ----------
function CostSummary({ pid, setPid, catLabel, onCategory }: { pid: string | null; setPid: (id: string | null) => void; catLabel: (k: string) => string; onCategory: (category: string, year: number) => void }) {
  const [year, setYear] = useState(currentYear());
  const q = useGet<OperatingCost>("finance/operating-costs", { property_id: pid ?? undefined, year });
  const d = q.data;
  const monthly = (d?.monthly ?? []).map((v, i) => ({ month: MONTHS_SHORT[i], cost: v }));
  const top = d?.categories[0];
  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1 basis-60">
          <CardTitle>Biaya operasional {year}</CardTitle>
          <CardSubtitle>Semua sumber: WO selesai (actual cost), consumable cleaning, dan biaya manual. Klik kategori untuk memfilter daftar.</CardSubtitle>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <PropertySelect value={pid} onChange={setPid} allowAll />
          <YearSelect value={year} years={yearOptions(currentYear(), 4, 0)} onChange={(y) => setYear(y ?? currentYear())} />
        </div>
      </CardHeader>
      <CardContent>
        {q.isLoading ? <CardSkeleton lines={4} /> : q.isError && !d ? <Alert variant="critical">Ringkasan biaya gagal dimuat.</Alert> : d ? (
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
              <StatTile label="Total setahun" value={fmtMoneyTile(d.total)} title={fmtMoney(d.total)} sub={`${d.categories.length} kategori`} tone="primary" />
              <StatTile label="Rata-rata / bulan" value={fmtMoneyTile(Math.round(d.total / Math.max(1, d.monthly.filter((v) => v > 0).length || 1)))} sub="bulan dengan biaya" />
              <StatTile label="Kategori terbesar" value={top ? top.label : "—"} sub={top ? `${fmtMoney(top.total)} · ${fmtPct(top.share_pct)}` : undefined} />
              <StatTile label="Bulan tertinggi" value={d.total ? MONTHS_SHORT[d.monthly.indexOf(Math.max(...d.monthly))] : "—"} sub={d.total ? fmtMoney(Math.max(...d.monthly)) : undefined} />
            </div>
            {d.total === 0 ? <p className="py-6 text-center text-sm text-on-surface-variant">Belum ada biaya tercatat pada {year}.</p> : (
              <div className="grid grid-cols-1 gap-5 xl:grid-cols-2">
                {/* per kategori: batang horizontal satu warna (besaran, bukan identitas) + label nilai & share */}
                <ul className="space-y-2.5" aria-label="Biaya per kategori">
                  {d.categories.map((c) => {
                    const max = d.categories[0]?.total || 1;
                    return (
                      <li key={c.category}>
                        <button type="button" className="w-full text-left" onClick={() => onCategory(c.category, year)} title={`Filter daftar: ${c.label}`}>
                          <div className="flex items-baseline justify-between gap-2 text-sm"><span className="font-medium hover:underline">{c.label || catLabel(c.category)}</span><span className="tnum text-on-surface-variant">{fmtMoney(c.total)} · {fmtPct(c.share_pct)}</span></div>
                          <div className="mt-1 h-2.5 w-full overflow-hidden rounded-full bg-surface-container-high"><div className="h-full rounded-full" style={{ width: `${Math.max(2, (c.total / max) * 100)}%`, backgroundColor: "var(--color-chart-primary)" }} /></div>
                        </button>
                        {d.sources[c.category] && <div className="mt-0.5 text-[11px] text-on-surface-variant">Sumber: {d.sources[c.category].join(" · ")}</div>}
                      </li>
                    );
                  })}
                </ul>
                <div className="h-56" role="img" aria-label={`Biaya operasional per bulan ${year}`}>
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart data={monthly} margin={{ left: 4, right: 8, top: 8 }}>
                      <CartesianGrid vertical={false} stroke="var(--color-chart-grid)" />
                      <XAxis dataKey="month" tick={{ fontSize: 11, fill: "var(--color-chart-axis)" }} axisLine={false} tickLine={false} />
                      <YAxis tick={{ fontSize: 11, fill: "var(--color-chart-axis)" }} width={52} axisLine={false} tickLine={false} tickFormatter={(v: number) => fmtAxisMoney(v)} />
                      <Tooltip cursor={{ fill: "var(--color-surface-container)" }} formatter={(v: number) => [fmtMoney(v), "Biaya"]} contentStyle={{ borderRadius: 8, border: "1px solid var(--color-border)", background: "var(--color-surface)", color: "var(--color-on-surface)" }} />
                      <Bar dataKey="cost" fill="var(--color-chart-primary)" radius={[4, 4, 0, 0]} maxBarSize={28} isAnimationActive={false} />
                    </BarChart>
                  </ResponsiveContainer>
                </div>
              </div>
            )}
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

// ---------- detail & bukti ----------
function CostDrawer({ entry: c, catLabel, onClose, onEdit }: { entry: CostEntry; catLabel: (k: string) => string; onClose: () => void; onEdit: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const files = useAttachments("cost_entry", c.id);
  const [del, setDel] = useState(false);
  const remove = () => {
    setDel(false);
    api(`finance/costs/${c.id}`, { method: "DELETE" })
      .then(() => { invalidate("list", "one", "dashboard"); toast.action("deleted", "Biaya"); onClose(); })
      .catch((e) => toast.failed("deleted", e, "Biaya"));
  };
  return (
    <Drawer open onClose={onClose} title={c.description} description={`${c.category_label || catLabel(c.category)} · ${fmtDay(c.entry_date)} · ${fmtMoney(c.amount)}`} width={600}>
      <div className="space-y-5" data-testid="cost-drawer">
        <div className="flex flex-wrap gap-2">
          {c.allowed_actions.includes("update") && <Button variant="secondary" icon="edit" onClick={onEdit}>Edit</Button>}
          {c.allowed_actions.includes("delete") && <Button variant="ghost" icon="delete" onClick={() => setDel(true)}>Hapus</Button>}
        </div>
        <KeyValue items={[
          { label: "Property", value: c.property_name },
          { label: "Kategori", value: c.category_label || catLabel(c.category) },
          { label: "Tanggal", value: fmtDay(c.entry_date) },
          { label: "Nominal", value: <span className="tnum font-semibold">{fmtMoney(c.amount)}</span> },
          { label: "Penerima", value: c.payee ?? "—" },
          { label: "Referensi", value: c.reference ?? "—" },
          { label: "Dicatat", value: <>{c.created_by_name ?? "—"} · {fmtDateTime(c.created_at)}</> },
        ]} />
        <div>
          <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Bukti ({(files.data ?? []).length})</div>
          <div className="space-y-2">
            {c.allowed_actions.includes("attach") && <PhotoEvidenceUploader objectType="cost_entry" objectId={c.id} attachmentType="document" compact label="Unggah bukti (foto / PDF)" onUploaded={() => { void files.refetch(); invalidate("list"); }} />}
            <AttachmentGrid items={files.data ?? []} emptyLabel="Belum ada bukti." />
          </div>
        </div>
      </div>
      {del && <ConfirmDialog open onOpenChange={(o) => !o && setDel(false)} title="Hapus biaya?" description={`${c.description} · ${fmtMoney(c.amount)} dihapus dari laporan & Budget vs Actual (tercatat di audit).`} confirmLabel="Hapus" destructive onConfirm={remove} />}
    </Drawer>
  );
}

// ---------- tambah / edit ----------
function CostDialog({ item, propertyId, categories, onClose }: { item: CostEntry | null; propertyId: string | null; categories: { key: string; label: string }[]; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { properties } = useAuth();
  const [pid, setPid] = useState<string | null>(item?.property_id ?? propertyId ?? (properties.length === 1 ? properties[0].id : null));
  const [f, setF] = useState({ category: item?.category ?? "", entry_date: item?.entry_date ?? todayISO(), amount: item?.amount ?? (null as number | null), payee: item?.payee ?? "", description: item?.description ?? "", reference: item?.reference ?? "" });
  const [receipt, setReceipt] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const valid = !!f.category && !!f.entry_date && !!f.amount && f.amount > 0 && !!f.description.trim() && (!!item || !!pid);
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    const body = { category: f.category, entry_date: f.entry_date, amount: f.amount, payee: f.payee.trim(), description: f.description.trim(), reference: f.reference.trim() };
    try {
      const out = item ? await api<CostEntry>(`finance/costs/${item.id}`, { method: "PATCH", body }) : await api<CostEntry>("finance/costs", { body: { ...body, property_id: pid }, idempotencyKey: uuid() });
      if (receipt) {
        try {
          await uploadAttachment(receipt, "cost_entry", out.id, "document");
        } catch (e) {
          toast.error(e);
        }
      }
      invalidate("list", "one", "attachments", "dashboard");
      toast.action(item ? "saved" : "created", `Biaya ${fmtMoney(out.amount)}`);
      onClose();
    } catch (e) {
      toast.failed(item ? "saved" : "created", e, "Biaya");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? "Edit biaya" : "Catat biaya operasional"} description="Biaya manual di luar work order (WO & consumable dihitung otomatis). Tanpa payroll.">
        <div className="space-y-4">
          {!item && properties.length > 1 && <Field label="Property" required><PropertySelect value={pid} onChange={setPid} className="sm:w-full" /></Field>}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Kategori" required>
              <NativeSelect value={f.category} onChange={(e) => setF({ ...f, category: e.target.value })}>
                <option value="">Pilih kategori…</option>
                {categories.map((c) => <option key={c.key} value={c.key}>{c.label}</option>)}
              </NativeSelect>
            </Field>
            <Field label="Tanggal" required><DatePicker value={f.entry_date} onChange={(v) => setF({ ...f, entry_date: v })} aria-label="Tanggal biaya" /></Field>
            <Field label="Nominal" required><MoneyInput value={f.amount} onChange={(v) => setF({ ...f, amount: v })} aria-label="Nominal" /></Field>
            <Field label="Penerima / vendor"><Input value={f.payee} onChange={(e) => setF({ ...f, payee: e.target.value })} placeholder="mis. PLN, PT Aman Sentosa" maxLength={160} /></Field>
          </div>
          <Field label="Keterangan" required><Textarea rows={2} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} placeholder="mis. Tagihan listrik area bersama September" /></Field>
          <Field label="Referensi" help="Nomor tagihan / bukti bayar."><Input value={f.reference} onChange={(e) => setF({ ...f, reference: e.target.value })} maxLength={120} /></Field>
          {!item && (
            <Field label="Bukti (opsional)" help="Foto nota atau PDF; bukti lain dapat ditambahkan di detail.">
              <input type="file" accept="image/*,application/pdf" onChange={(e) => setReceipt(e.target.files?.[0] ?? null)} className="block w-full text-sm" aria-label="Bukti biaya" />
            </Field>
          )}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
