// Utilities › Tarif (PRD P4 v2.1 P4-UTL-04): tarif per kWh / m³ per property — flat atau blok progresif [{up_to, rate}] (blok
// terakhir tanpa batas = sisa pemakaian), biaya beban (fixed charge) dan biaya minimum; pratinjau perhitungan sama dengan server.
import { useMemo, useState } from "react";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { Alert, Badge, Button, Checkbox, DatePicker, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Segmented, TBody, TD, TH, THead, TR, Table, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { CellText, CellTitle } from "@/components/bv/cells";
import { useToast } from "@/components/bv/common";
import { useAll, useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtMoney } from "@/lib/format";
import { MoneyInput, NeedProperty, PropertySelect } from "@/features/finance/fin-ui";
import { fmtDay, fmtQty, usePropertyParam } from "@/features/finance/fin-utils";
import { METER_TYPES, fmtRate, meterTypeLabel, parseDecimal, tariffSummary, usageCharge, validateBlocks, type Tariff, type TariffBlock } from "./utilities-model";

export function TariffsTab() {
  const { can } = useAuth();
  const [pid, setPid] = usePropertyParam();
  const list = useAll<Tariff>("billing/utility-tariffs", { property_id: pid ?? undefined });
  const [edit, setEdit] = useState<Tariff | "new" | null>(null);
  const canManage = can("billing.meters.manage", pid ?? undefined);
  const columns = useMemo<ColumnDef<Tariff, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): kode · jenis di atas nama tarif (tanpa kolom kode terpisah), ringkasan harga satu
    // baris (dipotong "…", lengkap di tooltip), biaya beban & minimum rata kanan, tanggal berlaku, status.
    { id: "name", header: "Tarif", meta: { mobile: "primary" }, cell: ({ row: { original: t } }) => <CellTitle code={`${t.code} · ${meterTypeLabel(t.meter_type)}`} title={t.name} /> },
    { id: "rate", header: "Harga pemakaian", meta: { mobile: "secondary" }, cell: ({ row }) => <CellText max={240} className="tnum">{tariffSummary(row.original, fmtRate)}</CellText> },
    { id: "fixed", header: "Biaya beban", size: 140, meta: { mobile: "secondary" }, cell: ({ row }) => <span className="block whitespace-nowrap text-right tnum text-sm">{row.original.fixed_charge ? fmtMoney(row.original.fixed_charge) : "—"}</span> },
    { id: "minimum", header: "Minimum", size: 140, meta: { mobile: "hidden" }, cell: ({ row }) => <span className="block whitespace-nowrap text-right tnum text-sm">{row.original.minimum_charge ? fmtMoney(row.original.minimum_charge) : "—"}</span> },
    { id: "effective", header: "Berlaku sejak", size: 130, meta: { mobile: "hidden" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm">{fmtDay(row.original.effective_from)}</span> },
    { id: "status", header: "Status", size: 100, meta: { mobile: "status" }, cell: ({ row }) => (row.original.is_active ? <Badge tone="success">Aktif</Badge> : <Badge tone="neutral">Nonaktif</Badge>) },
  ], []);
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <PropertySelect value={pid} onChange={setPid} allowAll />
        {canManage && <Button icon="add" className="ml-auto" onClick={() => setEdit("new")}>Tambah Tarif</Button>}
      </div>
      <DataGrid
        columns={columns}
        rows={list.data ?? []}
        rowId={(r) => r.id}
        onRowClick={canManage ? (t) => { setEdit(t); } : undefined}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        empty={{ icon: "price_change", title: "Belum ada tarif utilitas", description: "Tarif per kWh / m³ (flat atau blok progresif) beserta biaya beban & minimum dipakai billing rule pemakaian meter.", action: canManage ? <Button icon="add" onClick={() => setEdit("new")}>Tambah Tarif</Button> : undefined }}
        rowActions={canManage ? (t) => [{ label: "Edit", icon: "edit", onSelect: () => setEdit(t) }] : undefined}
      />
      {edit && <TariffDialog item={edit === "new" ? null : edit} propertyId={pid} onClose={() => setEdit(null)} />}
    </div>
  );
}

interface BlockRow { up_to: string; rate: string }

function TariffDialog({ item, propertyId, onClose }: { item: Tariff | null; propertyId: string | null; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { properties } = useAuth();
  const [pid, setPid] = useState<string | null>(item?.property_id ?? propertyId ?? (properties.length === 1 ? properties[0].id : null));
  const [f, setF] = useState({
    code: item?.code ?? "", name: item?.name ?? "", meter_type: item?.meter_type ?? "electricity", unit_label: item?.unit_label ?? "", rate: item ? String(item.rate).replace(".", ",") : "",
    fixed_charge: item?.fixed_charge ?? (null as number | null), minimum_charge: item?.minimum_charge ?? (null as number | null), effective_from: item?.effective_from ?? "", is_active: item?.is_active ?? true, notes: item?.notes ?? "",
  });
  const [mode, setMode] = useState<"flat" | "blocks">(item && item.blocks.length > 0 ? "blocks" : "flat");
  const [blocks, setBlocks] = useState<BlockRow[]>(() => (item?.blocks.length ? item.blocks.map((b) => ({ up_to: b.up_to === null ? "" : String(b.up_to), rate: String(b.rate).replace(".", ",") })) : [{ up_to: "", rate: "" }]));
  const [preview, setPreview] = useState("100");
  const [busy, setBusy] = useState(false);
  const unit = f.unit_label.trim() || METER_TYPES[f.meter_type]?.unit || "";
  const parsedBlocks: TariffBlock[] = blocks.map((b) => ({ up_to: b.up_to.trim() ? parseDecimal(b.up_to) : null, rate: parseDecimal(b.rate) ?? NaN }));
  const blockError = mode === "blocks" ? (blocks.length === 0 ? "Minimal satu blok" : parsedBlocks.some((b) => Number.isNaN(b.rate)) ? "Tarif tiap blok wajib diisi" : validateBlocks(parsedBlocks)) : null;
  const rate = parseDecimal(f.rate);
  const rateError = mode === "flat" && (rate === null || rate < 0) ? "Tarif wajib (≥ 0)" : undefined;
  const valid = !!f.name.trim() && (!!item || (!!f.code.trim() && !!pid)) && !rateError && !blockError;
  const calc = !rateError && !blockError ? usageCharge({ rate: mode === "flat" ? rate ?? 0 : 0, blocks: mode === "blocks" ? parsedBlocks : [], fixed_charge: f.fixed_charge ?? 0, minimum_charge: f.minimum_charge ?? 0 }, parseDecimal(preview) ?? 0) : null;
  const setBlock = (i: number, patch: Partial<BlockRow>) => setBlocks((xs) => xs.map((x, j) => (j === i ? { ...x, ...patch } : x)));
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    const common = {
      name: f.name.trim(), rate: mode === "flat" ? rate : 0, blocks: mode === "blocks" ? parsedBlocks : [], fixed_charge: f.fixed_charge ?? 0, minimum_charge: f.minimum_charge ?? 0,
      unit_label: f.unit_label.trim() || undefined, is_active: f.is_active, effective_from: f.effective_from || undefined, notes: f.notes.trim(),
    };
    try {
      if (item) await api(`billing/utility-tariffs/${item.id}`, { method: "PATCH", body: common });
      else await api("billing/utility-tariffs", { body: { ...common, property_id: pid, code: f.code.trim(), meter_type: f.meter_type, notes: f.notes.trim() || undefined }, idempotencyKey: uuid() });
      invalidate("all", "list", "one");
      toast.action(item ? "saved" : "created", `Tarif ${item?.code ?? f.code.trim()}`);
      onClose();
    } catch (e) {
      toast.failed(item ? "saved" : "created", e, "Tarif");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit tarif ${item.code}` : "Tambah Tarif Utilitas"}>
        <div className="space-y-4">
          {!item && properties.length > 1 && (
            <Field label="Property" required>
              <PropertySelect value={pid} onChange={setPid} className="sm:w-full" />
            </Field>
          )}
          {!item && !pid && <NeedProperty what="tarif" />}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Kode" required={!item} help={item ? "Kode & jenis tidak dapat diubah." : "Unik per property, mis. PLN-B2."}>
              <Input value={f.code} disabled={!!item} onChange={(e) => setF({ ...f, code: e.target.value.toUpperCase() })} maxLength={30} className="font-mono" autoFocus={!item} />
            </Field>
            <Field label="Jenis" required>
              <NativeSelect value={f.meter_type} disabled={!!item} onChange={(e) => setF({ ...f, meter_type: e.target.value })}>{Object.entries(METER_TYPES).map(([k, v]) => <option key={k} value={k}>{v.label} ({v.unit})</option>)}</NativeSelect>
            </Field>
            <Field label="Nama" required className="sm:col-span-2"><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="mis. Listrik unit hunian 2.200 VA" /></Field>
            <Field label="Satuan" help={`Default ${METER_TYPES[f.meter_type]?.unit ?? "—"}`}><Input value={f.unit_label} onChange={(e) => setF({ ...f, unit_label: e.target.value })} placeholder={METER_TYPES[f.meter_type]?.unit} maxLength={10} /></Field>
            <Field label="Berlaku sejak"><DatePicker value={f.effective_from} onChange={(v) => setF({ ...f, effective_from: v })} aria-label="Berlaku sejak" /></Field>
          </div>
          <div className="space-y-3 rounded-[var(--radius-lg)] border border-border p-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="text-sm font-semibold">Harga pemakaian</span>
              <Segmented<"flat" | "blocks"> value={mode} onChange={setMode} options={[{ value: "flat", label: "Flat" }, { value: "blocks", label: "Blok progresif" }]} />
            </div>
            {mode === "flat" ? (
              <Field label={`Tarif per ${unit}`} required error={rateError}>
                <Input inputMode="decimal" value={f.rate} onChange={(e) => setF({ ...f, rate: e.target.value })} className="text-right tnum" placeholder="mis. 1444,70" />
              </Field>
            ) : (
              <div className="space-y-2">
                <p className="text-xs text-on-surface-variant">Pemakaian dihitung bertingkat: blok 1 sampai batasnya, sisanya ke blok berikutnya. Kosongkan batas pada blok terakhir untuk seluruh sisa pemakaian.</p>
                <div className="grid grid-cols-[1fr_1fr_auto] gap-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant"><span>Sampai ({unit})</span><span>Tarif / {unit}</span><span className="w-8" /></div>
                {blocks.map((b, i) => (
                  <div key={i} className="grid grid-cols-[1fr_1fr_auto] items-center gap-2">
                    <Input inputMode="decimal" value={b.up_to} onChange={(e) => setBlock(i, { up_to: e.target.value })} placeholder={i === blocks.length - 1 ? "seterusnya" : "batas atas"} className="text-right tnum" aria-label={`Batas atas blok ${i + 1}`} />
                    <Input inputMode="decimal" value={b.rate} onChange={(e) => setBlock(i, { rate: e.target.value })} placeholder="0" className="text-right tnum" aria-label={`Tarif blok ${i + 1}`} />
                    <Button size="icon-sm" variant="ghost" icon="close" aria-label={`Hapus blok ${i + 1}`} disabled={blocks.length === 1} onClick={() => setBlocks((xs) => xs.filter((_, j) => j !== i))} />
                  </div>
                ))}
                <Button size="sm" variant="secondary" icon="add" onClick={() => setBlocks((xs) => [...xs, { up_to: "", rate: "" }])}>Tambah blok</Button>
                {blockError && <p className="text-xs text-error">{blockError}</p>}
              </div>
            )}
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Biaya beban / abonemen" help="Ditambahkan tiap periode."><MoneyInput value={f.fixed_charge} onChange={(v) => setF({ ...f, fixed_charge: v })} aria-label="Biaya beban" /></Field>
              <Field label="Biaya minimum" help="Total tagihan tidak kurang dari ini."><MoneyInput value={f.minimum_charge} onChange={(v) => setF({ ...f, minimum_charge: v })} aria-label="Biaya minimum" /></Field>
            </div>
          </div>
          <div className="space-y-2 rounded-[var(--radius-lg)] bg-surface-container-low p-3">
            <div className="flex flex-wrap items-center gap-2 text-sm font-semibold"><Icon name="calculate" size={18} aria-hidden />Simulasi tagihan
              <span className="ml-auto inline-flex items-center gap-1 font-normal">pemakaian <Input inputMode="decimal" value={preview} onChange={(e) => setPreview(e.target.value)} className="h-8 w-24 text-right tnum" aria-label="Pemakaian simulasi" /> {unit}</span>
            </div>
            {calc ? (
              <Table>
                <THead><tr><TH>Komponen</TH><TH className="bv-num">Jumlah</TH></tr></THead>
                <TBody>
                  {calc.parts.map((p, i) => <TR key={i}><TD className="text-sm">{fmtQty(p.span)} {unit} × {fmtRate(p.rate)}</TD><TD className="bv-num tnum">{fmtMoney(Math.round(p.cost))}</TD></TR>)}
                  <TR><TD className="text-sm">Pemakaian (dibulatkan)</TD><TD className="bv-num tnum">{fmtMoney(calc.energy)}</TD></TR>
                  {calc.fixed > 0 && <TR><TD className="text-sm">Biaya beban</TD><TD className="bv-num tnum">{fmtMoney(calc.fixed)}</TD></TR>}
                  <TR className="font-semibold"><TD>Total{calc.total > calc.energy + calc.fixed ? " (biaya minimum)" : ""}</TD><TD className="bv-num tnum">{fmtMoney(calc.total)}</TD></TR>
                </TBody>
              </Table>
            ) : <p className="text-sm text-on-surface-variant">Lengkapi tarif untuk melihat simulasi.</p>}
          </div>
          <Field label="Catatan"><Textarea rows={2} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
          <Checkbox label="Tarif aktif (dapat dipilih pada meter & billing rule)" checked={f.is_active} onCheckedChange={(v) => setF({ ...f, is_active: v })} />
          {item && <Alert variant="info">Perubahan tarif berlaku untuk tagihan yang dibuat setelah disimpan; invoice terbit tidak berubah.</Alert>}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
