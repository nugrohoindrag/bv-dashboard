// Editor item invoice (PRD P4 v2.1 P4-INV-03/04, B-07): deskripsi, qty, satuan, harga satuan, komponen tagihan, pajak per item
// (ikut default · bebas pajak · tarif khusus) + pratinjau jumlah (qty × harga, half-up) dan pajak — sama dengan hitungan server.
// Dipakai form invoice (buat / edit draft) dan tagihan biaya Work Order.
import { Icon } from "@buildingvision/ui";
import { Button, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { cn } from "@/lib/utils";
import { INVOICE_TYPES, computeTotals, invoiceTypeLabel, money, newItemDraft, pct, type ItemDraft, type ItemTaxMode } from "./types";
import { ItemMeta } from "./shared";

export function InvoiceItemsEditor({ items, onChange, invoiceType, applyTax, defaultRate, taxName, currency = "IDR", disabled, error }: {
  items: ItemDraft[];
  onChange: (items: ItemDraft[]) => void;
  invoiceType: string;
  applyTax: boolean;
  defaultRate: number;
  taxName: string;
  currency?: string;
  disabled?: boolean;
  error?: string;
}) {
  const totals = computeTotals(items, applyTax, defaultRate);
  const set = (key: string, patch: Partial<ItemDraft>) => onChange(items.map((x) => (x.key === key ? { ...x, ...patch } : x)));
  const remove = (key: string) => onChange(items.filter((x) => x.key !== key));
  const defaultTaxText = applyTax ? `Default (${taxName} ${pct(defaultRate)})` : "Default (tanpa pajak)";
  return (
    <div>
      <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
        {items.map((it, i) => {
          const line = totals.lines[i];
          return (
            <li key={it.key} className="space-y-2 px-3 py-3">
              <div className="flex items-start gap-2">
                <span className="tnum mt-2.5 w-5 shrink-0 text-xs text-on-surface-variant">{i + 1}.</span>
                <div className="min-w-0 flex-1">
                  <Input placeholder="Deskripsi item (mis. Service charge Oktober 2026)" value={it.description} onChange={(e) => set(it.key, { description: e.target.value })} disabled={disabled} aria-label={`Deskripsi item ${i + 1}`} aria-invalid={!!error && !it.description.trim() ? true : undefined} />
                  <ItemMeta meta={it.meta} unit={it.unit} />
                </div>
                <Button type="button" variant="ghost" size="icon-sm" aria-label={`Hapus item ${i + 1}`} onClick={() => remove(it.key)} disabled={disabled || items.length <= 1}>
                  <Icon name="close" size={16} />
                </Button>
              </div>
              <div className="grid grid-cols-2 gap-2 pl-7 sm:grid-cols-4">
                <Field label="Qty"><Input type="number" inputMode="decimal" min={0} step="0.01" value={it.quantity} onChange={(e) => set(it.key, { quantity: e.target.value })} disabled={disabled} aria-label={`Qty item ${i + 1}`} /></Field>
                <Field label="Satuan"><Input placeholder="m², kWh, bln" value={it.unit} onChange={(e) => set(it.key, { unit: e.target.value })} disabled={disabled} aria-label={`Satuan item ${i + 1}`} /></Field>
                <Field label="Harga satuan" className="col-span-2"><Input type="number" inputMode="numeric" min={0} step="1" value={it.unit_price} onChange={(e) => set(it.key, { unit_price: e.target.value })} disabled={disabled} aria-label={`Harga satuan item ${i + 1}`} /></Field>
                <Field label="Komponen" className="col-span-2">
                  <NativeSelect value={it.charge_type} onChange={(e) => set(it.key, { charge_type: e.target.value })} disabled={disabled} aria-label={`Komponen item ${i + 1}`}>
                    <option value="">Ikut invoice ({invoiceTypeLabel(invoiceType)})</option>
                    {INVOICE_TYPES.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
                  </NativeSelect>
                </Field>
                <Field label="Pajak" className={cn(it.tax_mode === "custom" ? "col-span-1" : "col-span-2")}>
                  <NativeSelect value={it.tax_mode} onChange={(e) => set(it.key, { tax_mode: e.target.value as ItemTaxMode })} disabled={disabled} aria-label={`Pajak item ${i + 1}`}>
                    <option value="default">{defaultTaxText}</option>
                    <option value="exempt">Bebas pajak</option>
                    <option value="custom">Tarif khusus…</option>
                  </NativeSelect>
                </Field>
                {it.tax_mode === "custom" && (
                  <Field label="Tarif %"><Input type="number" inputMode="decimal" min={0} max={100} step="0.01" value={it.tax_rate} onChange={(e) => set(it.key, { tax_rate: e.target.value })} disabled={disabled} aria-label={`Tarif pajak item ${i + 1}`} /></Field>
                )}
              </div>
              <div className="flex flex-wrap justify-end gap-x-4 pl-7 text-sm">
                <span className="text-on-surface-variant">Jumlah <span className="tnum font-semibold text-on-surface">{money(line?.amount, currency)}</span></span>
                <span className="text-on-surface-variant">{taxName} {pct(line?.rate ?? 0)} <span className="tnum text-on-surface">{money(line?.tax, currency)}</span></span>
              </div>
            </li>
          );
        })}
      </ul>
      {error && <p className="mt-1 text-xs text-error">{error}</p>}
      <div className="mt-2 flex flex-wrap items-start justify-between gap-3">
        <Button type="button" size="sm" variant="secondary" icon="add" onClick={() => onChange([...items, newItemDraft()])} disabled={disabled}>Tambah item</Button>
        <dl className="grid min-w-[240px] grid-cols-[1fr_auto] gap-x-6 gap-y-1 text-sm">
          <dt className="text-on-surface-variant">Subtotal</dt><dd className="tnum text-right">{money(totals.subtotal, currency)}</dd>
          <dt className="text-on-surface-variant">{taxName}</dt><dd className="tnum text-right">{money(totals.tax, currency)}</dd>
          <dt className="font-semibold">Total</dt><dd className="tnum text-right text-h3 font-bold">{money(totals.total, currency)}</dd>
        </dl>
      </div>
    </div>
  );
}
