// Finance › Budget (PRD P4 v2.1 P4-BGT-01; Roadmap §29 Phase 4): budget per property per tahun per kategori (pendapatan & biaya)
// dengan rincian 12 bulan, revisi & persetujuan. Draft dapat diubah (billing.budgets.manage); disetujui (billing.budgets.approve)
// menjadi acuan Budget vs Actual dan menggantikan budget disetujui sebelumnya; revisi membuat draft baru dari budget disetujui.
// Route: /finance/budgets (daftar) · /finance/budgets/:id (editor; deep link notifikasi).
import { useMemo, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { RowActionMenu } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Checkbox, ConfirmDialog, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, TBody, TD, TH, THead, TR, Table, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { DetailSkeleton } from "@/components/bv/states";
import { useAll, useInvalidate, useOne } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime, fmtMoney } from "@/lib/format";
import { cn } from "@/lib/utils";
import { MoneyInput, PropertySelect, StatTile, YearSelect } from "./fin-ui";
import { MONTHS_SHORT, currentYear, fmtMoneyShort, fmtMoneyTile, useGet, usePropertyParam, yearOptions } from "./fin-utils";
import { FALLBACK_CATEGORIES, linesFromMatrix, matrixEqual, matrixFromLines, mkey, monthTotals, spreadAnnual, sum, zeros, type Budget, type Categories, type Kind, type Matrix } from "./finance-model";

export default function BudgetsPage() {
  const { id } = useParams();
  return (
    <div>
      <PageHeader title="Budget" subtitle="Budget pendapatan & biaya per property per tahun dengan rincian bulanan, revisi, dan persetujuan — acuan Budget vs Actual." />
      {id ? <BudgetDetail id={id} /> : <BudgetList />}
    </div>
  );
}

// ---------- daftar ----------
function BudgetList() {
  const { can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const [pid, setPid] = usePropertyParam();
  const year = sp.has("year") ? Number(sp.get("year")) || null : null;
  const list = useAll<Budget>("finance/budgets", { property_id: pid ?? undefined, year: year ?? undefined });
  const [create, setCreate] = useState(false);
  const canManage = can("billing.budgets.manage", pid ?? undefined);
  const columns = useMemo<ColumnDef<Budget, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): tahun · revisi di atas nama budget, property, status, pendapatan/biaya/selisih rata
    // kanan satu baris. Waktu & pemberi persetujuan ada di halaman detail budget.
    { id: "year", header: "Budget", size: 200, meta: { mobile: "primary" }, cell: ({ row: { original: b } }) => <CellTitle code={`${b.fiscal_year} · rev ${b.revision}`} title={b.name || `Budget ${b.fiscal_year}`} /> },
    { id: "property", header: "Property", meta: { mobile: "secondary" }, cell: ({ row }) => <CellText max={200}>{row.original.property_name}</CellText> },
    { id: "status", header: "Status", size: 130, meta: { mobile: "status" }, cell: ({ row }) => <StatusBadge objectType="budget" status={row.original.status} /> },
    { id: "revenue", header: "Pendapatan", size: 160, meta: { mobile: "secondary" }, cell: ({ row }) => <span className="block whitespace-nowrap text-right tnum">{fmtMoney(row.original.revenue_total)}</span> },
    { id: "cost", header: "Biaya", size: 160, meta: { mobile: "secondary" }, cell: ({ row }) => <span className="block whitespace-nowrap text-right tnum">{fmtMoney(row.original.cost_total)}</span> },
    { id: "net", header: "Selisih", size: 160, meta: { mobile: "hidden" }, cell: ({ row: { original: b } }) => <span className={cn("block whitespace-nowrap text-right tnum font-semibold", b.revenue_total - b.cost_total < 0 && "text-on-error-container")}>{fmtMoney(b.revenue_total - b.cost_total)}</span> },
  ], []);
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <PropertySelect value={pid} onChange={setPid} allowAll />
        <YearSelect value={year} allowAll years={yearOptions(currentYear(), 4, 1)} onChange={(y) => { const n = new URLSearchParams(sp); if (y) n.set("year", String(y)); else n.delete("year"); setSp(n, { replace: true }); }} />
        {canManage && <Button icon="add" className="ml-auto" onClick={() => setCreate(true)}>Buat Budget</Button>}
      </div>
      <DataGrid
        columns={columns}
        rows={list.data ?? []}
        rowId={(r) => r.id}
        onRowClick={(r) => `/finance/budgets/${r.id}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={!!year}
        empty={{ icon: "request_page", title: "Belum ada budget", description: "Susun budget pendapatan & biaya per kategori per bulan; setelah disetujui menjadi acuan Budget vs Actual.", action: canManage ? <Button icon="add" onClick={() => setCreate(true)}>Buat Budget</Button> : undefined }}
        rowClassName={(b) => (b.status === "superseded" ? "opacity-70" : undefined)}
      />
      {create && <CreateBudgetDialog propertyId={pid} onClose={() => setCreate(false)} />}
    </div>
  );
}

function CreateBudgetDialog({ propertyId, onClose }: { propertyId: string | null; onClose: () => void }) {
  const toast = useToast();
  const nav = useNavigate();
  const invalidate = useInvalidate();
  const { properties } = useAuth();
  const [pid, setPid] = useState<string | null>(propertyId ?? (properties.length === 1 ? properties[0].id : null));
  const [f, setF] = useState({ fiscal_year: currentYear(), name: "", notes: "", copy_from_id: "" });
  const existing = useAll<Budget>("finance/budgets", { property_id: pid ?? undefined }, { enabled: !!pid });
  const sources = (existing.data ?? []).filter((b) => b.revenue_total + b.cost_total > 0);
  const draftExists = (existing.data ?? []).some((b) => b.fiscal_year === f.fiscal_year && b.status === "draft");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!pid) return;
    setBusy(true);
    try {
      const b = await api<Budget>("finance/budgets", { body: { property_id: pid, fiscal_year: f.fiscal_year, name: f.name.trim() || null, notes: f.notes.trim() || null, copy_from_id: f.copy_from_id || null }, idempotencyKey: uuid() });
      invalidate("all", "list", "one");
      toast.action("created", `Budget ${b.fiscal_year} rev ${b.revision}`);
      onClose();
      nav(`/finance/budgets/${b.id}`);
    } catch (e) {
      toast.failed("created", e, "Budget");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Buat Budget" description="Budget dibuat sebagai draft; isi matriks bulanan lalu ajukan persetujuan.">
        <div className="space-y-4">
          {properties.length > 1 && <Field label="Property" required><PropertySelect value={pid} onChange={(v) => { setPid(v); setF({ ...f, copy_from_id: "" }); }} className="sm:w-full" /></Field>}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Tahun anggaran" required>
              <YearSelect value={f.fiscal_year} years={yearOptions(currentYear(), 1, 2)} onChange={(y) => setF({ ...f, fiscal_year: y ?? currentYear() })} className="sm:w-full" />
            </Field>
            <Field label="Nama"><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="mis. Budget operasional" maxLength={120} /></Field>
          </div>
          <Field label="Salin dari" help="Opsional — salin seluruh baris budget lain (mis. tahun lalu) sebagai titik awal.">
            <NativeSelect value={f.copy_from_id} onChange={(e) => setF({ ...f, copy_from_id: e.target.value })} disabled={!pid || existing.isLoading}>
              <option value="">Mulai kosong</option>
              {sources.map((b) => <option key={b.id} value={b.id}>{b.fiscal_year} rev {b.revision} · {b.status === "approved" ? "disetujui" : b.status === "draft" ? "draft" : "digantikan"} · {fmtMoney(b.revenue_total)} / {fmtMoney(b.cost_total)}</option>)}
            </NativeSelect>
          </Field>
          <Field label="Catatan"><Textarea rows={2} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
          {draftExists && <Alert variant="warning">Sudah ada draft budget {f.fiscal_year} untuk property ini — lanjutkan draft tersebut dari daftar.</Alert>}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!pid || draftExists} loading={busy} onClick={submit}>Buat draft</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- detail / editor ----------
function BudgetDetail({ id }: { id: string }) {
  const q = useOne<Budget>("finance/budgets", id);
  return (
    <AsyncState query={q} skeleton={<DetailSkeleton />}>
      {(b) => <BudgetEditor key={`${b.id}:${b.version}`} budget={b} />}
    </AsyncState>
  );
}

function BudgetEditor({ budget: b }: { budget: Budget }) {
  const toast = useToast();
  const nav = useNavigate();
  const invalidate = useInvalidate();
  const cats = useGet<Categories>("finance/categories", {}, { staleTime: 10 * 60_000 });
  const categories = cats.data ?? FALLBACK_CATEGORIES;
  const revisions = useAll<Budget>("finance/budgets", { property_id: b.property_id, year: b.fiscal_year });
  const editable = b.allowed_actions.includes("update");
  const initial = useMemo(() => matrixFromLines(b.lines), [b.lines]);
  const [m, setM] = useState<Matrix>(initial);
  const [name, setName] = useState(b.name ?? "");
  const [notes, setNotes] = useState(b.notes ?? "");
  const [busy, setBusy] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<"approve" | "revise" | null>(null);
  const dirty = !matrixEqual(m, initial) || name !== (b.name ?? "") || notes !== (b.notes ?? "");
  const rev = sum(monthTotals(m, "revenue"));
  const cost = sum(monthTotals(m, "cost"));
  const setRow = (kind: Kind, cat: string, months: number[]) => setM((x) => ({ ...x, [mkey(kind, cat)]: months }));
  const save = async () => {
    setBusy("save");
    try {
      await api<Budget>(`finance/budgets/${b.id}`, { method: "PATCH", body: { name: name.trim(), notes: notes.trim(), lines: linesFromMatrix(m) } });
      invalidate("all", "list", "one");
      toast.action("saved", `Budget ${b.fiscal_year} rev ${b.revision}`);
    } catch (e) {
      toast.failed("saved", e, "Budget");
    } finally {
      setBusy(null);
    }
  };
  const act = async (action: "approve" | "revise") => {
    setConfirm(null);
    setBusy(action);
    try {
      const out = await api<Budget>(`finance/budgets/${b.id}/${action}`, { body: {}, idempotencyKey: uuid() });
      invalidate("all", "list", "one", "dashboard");
      if (action === "approve") toast.action("approved", `Budget ${b.fiscal_year} rev ${b.revision}`);
      else {
        toast.success(`Draft revisi ${out.revision} dibuat — ubah lalu setujui untuk menggantikan budget aktif.`);
        nav(`/finance/budgets/${out.id}`);
      }
    } catch (e) {
      toast.failed(action === "approve" ? "approved" : "created", e, "Budget");
    } finally {
      setBusy(null);
    }
  };
  const others = (revisions.data ?? []).filter((x) => x.id !== b.id);
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <Link to="/finance/budgets" className="inline-flex items-center gap-1 text-sm font-semibold text-primary hover:underline"><Icon name="arrow_back" size={16} aria-hidden />Semua budget</Link>
          <div className="mt-1 flex flex-wrap items-center gap-2">
            <h2 className="text-h2 font-bold">Budget {b.fiscal_year}</h2>
            <Badge>rev {b.revision}</Badge>
            <StatusBadge objectType="budget" status={b.status} />
          </div>
          <div className="text-sm text-on-surface-variant">{b.property_name} · dibuat {b.created_by_name ?? "—"} {fmtDateTime(b.created_at)}{b.approved_at ? ` · disetujui ${b.approved_by_name ?? "—"} ${fmtDateTime(b.approved_at)}` : ""}</div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Link to={`/finance/budget-actual?property_id=${b.property_id}&year=${b.fiscal_year}`} className="inline-flex h-10 items-center gap-1 rounded-[var(--radius-md)] px-3 text-sm font-semibold text-primary hover:bg-surface-container"><Icon name="compare_arrows" size={16} aria-hidden />Budget vs Actual</Link>
          {editable && <Button variant={dirty ? "primary" : "secondary"} icon="save" disabled={!dirty} loading={busy === "save"} onClick={save}>Simpan</Button>}
          {b.allowed_actions.includes("approve") && <Button variant="success" icon="task_alt" disabled={dirty || rev + cost === 0} title={dirty ? "Simpan perubahan terlebih dahulu" : rev + cost === 0 ? "Budget masih kosong" : undefined} loading={busy === "approve"} onClick={() => setConfirm("approve")}>Setujui</Button>}
          {b.allowed_actions.includes("revise") && <Button variant="secondary" icon="content_copy" loading={busy === "revise"} onClick={() => setConfirm("revise")}>Buat revisi</Button>}
        </div>
      </div>
      {b.status === "superseded" && <Alert variant="info">Revisi ini sudah digantikan revisi yang lebih baru; hanya untuk riwayat.</Alert>}
      {b.status === "approved" && <Alert variant="success" title="Budget aktif">Menjadi acuan Budget vs Actual {b.fiscal_year}. Perubahan dilakukan lewat <b>Buat revisi</b>.</Alert>}
      {dirty && <Alert variant="warning">Ada perubahan yang belum disimpan.</Alert>}
      <section aria-label="Ringkasan budget" className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <StatTile label="Pendapatan setahun" value={fmtMoneyTile(rev)} title={fmtMoney(rev)} sub={`rata-rata ${fmtMoney(Math.round(rev / 12))}/bulan`} />
        <StatTile label="Biaya setahun" value={fmtMoneyTile(cost)} title={fmtMoney(cost)} sub={`rata-rata ${fmtMoney(Math.round(cost / 12))}/bulan`} />
        <StatTile label="Selisih (pendapatan − biaya)" value={fmtMoneyTile(rev - cost)} title={fmtMoney(rev - cost)} sub={rev - cost >= 0 ? "surplus" : "defisit"} tone={rev - cost < 0 ? "error" : "success"} />
      </section>
      {(editable || b.name || b.notes) && (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Field label="Nama"><Input value={name} readOnly={!editable} onChange={(e) => setName(e.target.value)} maxLength={120} /></Field>
          <Field label="Catatan"><Input value={notes} readOnly={!editable} onChange={(e) => setNotes(e.target.value)} /></Field>
        </div>
      )}
      <MatrixSection kind="revenue" title="Pendapatan" categories={categories.revenue} m={m} editable={editable} onRow={setRow} />
      <MatrixSection kind="cost" title="Biaya operasional" categories={categories.cost} m={m} editable={editable} onRow={setRow} />
      <NetTable m={m} />
      {others.length > 0 && (
        <Card>
          <CardHeader><CardTitle>Revisi lain {b.fiscal_year}</CardTitle></CardHeader>
          <CardContent>
            <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
              {others.map((x) => (
                <li key={x.id}>
                  <Link to={`/finance/budgets/${x.id}`} className="flex items-center justify-between gap-3 px-3 py-2 text-sm hover:bg-surface-container-low">
                    <span className="flex items-center gap-2"><Badge>rev {x.revision}</Badge><StatusBadge objectType="budget" status={x.status} />{x.name}</span>
                    <span className="tnum text-on-surface-variant">{fmtMoney(x.revenue_total)} / {fmtMoney(x.cost_total)}</span>
                  </Link>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}
      {confirm && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setConfirm(null)}
          title={confirm === "approve" ? `Setujui Budget ${b.fiscal_year} rev ${b.revision}?` : `Buat revisi Budget ${b.fiscal_year}?`}
          description={confirm === "approve" ? "Budget menjadi acuan Budget vs Actual. Budget disetujui sebelumnya untuk tahun ini menjadi Digantikan." : "Seluruh baris budget disalin menjadi draft revisi baru; budget aktif tetap berlaku sampai revisi disetujui."}
          confirmLabel={confirm === "approve" ? "Setujui" : "Buat revisi"}
          onConfirm={() => void act(confirm)}
        />
      )}
    </div>
  );
}

function MatrixSection({ kind, title, categories, m, editable, onRow }: { kind: Kind; title: string; categories: { key: string; label: string }[]; m: Matrix; editable: boolean; onRow: (kind: Kind, cat: string, months: number[]) => void }) {
  const [annualFor, setAnnualFor] = useState<{ key: string; label: string } | null>(null);
  const [annual, setAnnual] = useState<number | null>(null);
  const [onlyFilled, setOnlyFilled] = useState(false);
  const filled = (c: { key: string }) => (m[mkey(kind, c.key)] ?? []).some((v) => v);
  const rows = editable && !onlyFilled ? categories : categories.filter(filled);
  const totals = monthTotals(m, kind);
  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1 basis-60">
          <CardTitle>{title}</CardTitle>
          <CardSubtitle>{editable ? "Nilai rupiah per bulan. Menu baris: isi dari total tahunan, salin Januari ke semua bulan, atau kosongkan." : "Rincian bulanan (hanya kategori bernilai)."}</CardSubtitle>
        </div>
        <span className="flex flex-wrap items-center gap-3 text-sm text-on-surface-variant">
          {editable && <Checkbox label="Hanya kategori terisi" checked={onlyFilled} onCheckedChange={setOnlyFilled} />}
          <span>Total <b className="tnum text-on-surface">{fmtMoney(sum(totals))}</b></span>
        </span>
      </CardHeader>
      <CardContent className="px-0 pb-0">
        {rows.length === 0 ? <p className="px-5 pb-5 text-sm text-on-surface-variant">Tidak ada nilai budget {title.toLowerCase()}.</p> : (
          <Table data-testid={`budget-matrix-${kind}`}>
            <THead>
              <tr>
                <TH className="sticky left-0 z-[1] min-w-[160px]">Kategori</TH>
                {MONTHS_SHORT.map((mm) => <TH key={mm} className="bv-num">{mm}</TH>)}
                <TH className="bv-num">Total</TH>
                {editable && <TH aria-label="Aksi baris" />}
              </tr>
            </THead>
            <TBody>
              {rows.map((c) => {
                const months = m[mkey(kind, c.key)] ?? zeros();
                return (
                  <TR key={c.key}>
                    <TD className="sticky left-0 z-[1] bg-surface font-medium">{c.label}</TD>
                    {months.map((v, i) => (
                      <TD key={i} className={cn(editable ? "p-1" : "bv-num tnum")}>
                        {editable ? (
                          <MoneyInput compact className="min-w-[96px]" value={v || null} onChange={(nv) => onRow(kind, c.key, months.map((x, j) => (j === i ? nv ?? 0 : x)))} aria-label={`${c.label} ${MONTHS_SHORT[i]}`} />
                        ) : v ? fmtMoney(v) : <span className="text-on-surface-variant">—</span>}
                      </TD>
                    ))}
                    <TD className="bv-num tnum font-semibold">{fmtMoney(sum(months))}</TD>
                    {editable && (
                      <TD className="text-right">
                        <RowActionMenu label={`Aksi ${c.label}`} items={[
                          { id: "annual", label: "Isi dari total tahunan…", icon: "calculate", onClick: () => { setAnnual(sum(months) || null); setAnnualFor(c); } },
                          { id: "copy", label: "Salin Januari ke semua bulan", icon: "content_copy", onClick: () => onRow(kind, c.key, months.map(() => months[0] || 0)) },
                          { id: "clear", label: "Kosongkan", icon: "backspace", danger: true, onClick: () => onRow(kind, c.key, zeros()) },
                        ]} />
                      </TD>
                    )}
                  </TR>
                );
              })}
              <TR className="bg-surface-container-low font-semibold">
                <TD className="sticky left-0 z-[1] bg-surface-container-low">Total {title.toLowerCase()}</TD>
                {totals.map((v, i) => <TD key={i} className="bv-num tnum">{fmtMoney(v)}</TD>)}
                <TD className="bv-num tnum">{fmtMoney(sum(totals))}</TD>
                {editable && <TD />}
              </TR>
            </TBody>
          </Table>
        )}
      </CardContent>
      {annualFor && (
        <Dialog open onOpenChange={(o) => !o && setAnnualFor(null)}>
          <DialogContent title={`Total tahunan · ${annualFor.label}`} description="Dibagi rata ke 12 bulan (sisa pembulatan masuk Desember).">
            <Field label="Total setahun"><MoneyInput value={annual} onChange={setAnnual} autoFocus aria-label="Total setahun" /></Field>
            {annual ? <p className="mt-2 text-sm text-on-surface-variant">≈ {fmtMoney(Math.floor(annual / 12))} per bulan</p> : null}
            <DialogFooter>
              <Button variant="secondary" onClick={() => setAnnualFor(null)}>Batal</Button>
              <Button onClick={() => { onRow(kind, annualFor.key, spreadAnnual(annual ?? 0)); setAnnualFor(null); }}>Terapkan</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </Card>
  );
}

function NetTable({ m }: { m: Matrix }) {
  const rev = monthTotals(m, "revenue");
  const cost = monthTotals(m, "cost");
  const net = rev.map((v, i) => v - cost[i]);
  return (
    <Card>
      <CardHeader><CardTitle>Selisih bulanan</CardTitle></CardHeader>
      <CardContent className="px-0 pb-0">
        <Table>
          <THead><tr><TH className="min-w-[160px]">&nbsp;</TH>{MONTHS_SHORT.map((mm) => <TH key={mm} className="bv-num">{mm}</TH>)}<TH className="bv-num">Total</TH></tr></THead>
          <TBody>
            <TR><TD>Pendapatan</TD>{rev.map((v, i) => <TD key={i} className="bv-num tnum whitespace-nowrap">{fmtMoneyShort(v)}</TD>)}<TD className="bv-num tnum whitespace-nowrap">{fmtMoney(sum(rev))}</TD></TR>
            <TR><TD>Biaya</TD>{cost.map((v, i) => <TD key={i} className="bv-num tnum whitespace-nowrap">{fmtMoneyShort(v)}</TD>)}<TD className="bv-num tnum whitespace-nowrap">{fmtMoney(sum(cost))}</TD></TR>
            <TR className="font-semibold"><TD>Selisih</TD>{net.map((v, i) => <TD key={i} className={cn("bv-num tnum whitespace-nowrap", v < 0 && "text-on-error-container")}>{fmtMoneyShort(v)}</TD>)}<TD className={cn("bv-num tnum whitespace-nowrap", sum(net) < 0 && "text-on-error-container")}>{fmtMoney(sum(net))}</TD></TR>
          </TBody>
        </Table>
      </CardContent>
    </Card>
  );
}
