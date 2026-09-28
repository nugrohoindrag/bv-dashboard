// Komponen bersama layar Financial Operations (PRD P4 v2.1): pilihan property halaman, input uang berformat, kartu
// ringkasan, picker Work Order / tenant / pihak (tenant atau unit), pemilih tahun, dan kolom salin. Helper murni ada di
// fin-utils.ts; semua uang memakai fmtMoney (P4-FIN-04).
import { useEffect, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import type { Tone } from "@buildingvision/ui/bv";
import { Alert, Button, Card, Field, Input, NativeSelect, Popover, Segmented, inputClass } from "@/components/ui/primitives";
import { StatusBadge } from "@/components/bv/badges";
import { useToast } from "@/components/bv/common";
import { LocationPicker } from "@/components/bv/pickers";
import { useAll } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import type { Tenant, WorkItem } from "@/api/types";
import { copyText, fmtGroup, parseAmount } from "./fin-utils";

// ---------- property ----------
/** Pilihan property halaman (disembunyikan bila user hanya punya satu property). `allowAll` = opsi "Semua property". */
export function PropertySelect({ value, onChange, allowAll, className }: { value: string | null; onChange: (id: string | null) => void; allowAll?: boolean; className?: string }) {
  const { properties } = useAuth();
  if (properties.length <= 1) return null;
  return (
    <NativeSelect className={cn("w-full sm:w-56", className)} value={value ?? ""} onChange={(e) => onChange(e.target.value || null)} aria-label="Property">
      {allowAll ? <option value="">Semua property</option> : !value && <option value="">Pilih property…</option>}
      {properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
    </NativeSelect>
  );
}

/** Pesan ketika halaman membutuhkan satu property. */
export function NeedProperty({ what = "data ini" }: { what?: string }) {
  return <Alert variant="info" title="Pilih property">Pilih property di header atau filter halaman untuk melihat {what}.</Alert>;
}

// ---------- uang ----------
/**
 * Input rupiah berformat ("1.500.000") — nilai `number | null` (bulat, ≥ 0). `compact` = tanpa awalan "Rp" dan tinggi 32px
 * (sel matriks budget).
 */
export function MoneyInput({ value, onChange, compact, className, placeholder, disabled, autoFocus, id, "aria-label": ariaLabel }: { value: number | null | undefined; onChange: (v: number | null) => void; compact?: boolean; className?: string; placeholder?: string; disabled?: boolean; autoFocus?: boolean; id?: string; "aria-label"?: string }) {
  return (
    <span className={cn("relative flex w-full items-center", className)}>
      {!compact && <span aria-hidden className="pointer-events-none absolute left-3 text-sm text-on-surface-variant">Rp</span>}
      <input
        id={id}
        inputMode="numeric"
        autoComplete="off"
        aria-label={ariaLabel}
        disabled={disabled}
        autoFocus={autoFocus}
        placeholder={placeholder ?? (compact ? "0" : "")}
        className={cn(inputClass, "text-right tnum", compact ? "h-8 px-2 text-sm" : "pl-9")}
        value={fmtGroup(value)}
        onChange={(e) => onChange(parseAmount(e.target.value))}
        onFocus={(e) => e.currentTarget.select()}
      />
    </span>
  );
}

// ---------- kartu ringkasan ----------
/** Kartu angka ringkas (KPI halaman): label, nilai, keterangan; rail tone opsional; seluruh kartu tautan bila `to`. */
export function StatTile({ label, value, sub, tone, to, title, className, testId }: { label: string; value: ReactNode; sub?: ReactNode; tone?: Tone; to?: string; title?: string; className?: string; testId?: string }) {
  const body = (
    <div className="flex h-full flex-col p-4" title={title}>
      <span className="text-sm font-medium text-on-surface-variant">{label}</span>
      <span className="mt-1 break-words text-h2 font-extrabold tnum text-on-surface">{value}</span>
      {sub && <span className="mt-1 text-caption text-on-surface-variant">{sub}</span>}
    </div>
  );
  return (
    <Card railTone={tone} interactive={!!to} className={cn("min-w-0", className)} data-testid={testId}>
      {to ? <Link to={to} className="block h-full">{body}</Link> : body}
    </Card>
  );
}

/** Judul kecil bagian (uppercase). */
export function SectionLabel({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant", className)}>{children}</div>;
}

// ---------- tahun ----------
export function YearSelect({ value, onChange, years, className, allowAll }: { value: number | null; onChange: (y: number | null) => void; years: number[]; className?: string; allowAll?: boolean }) {
  return (
    <NativeSelect className={cn("w-full sm:w-32", className)} value={value ?? ""} onChange={(e) => onChange(e.target.value ? Number(e.target.value) : null)} aria-label="Tahun">
      {allowAll && <option value="">Semua tahun</option>}
      {years.map((y) => <option key={y} value={y}>{y}</option>)}
    </NativeSelect>
  );
}

// ---------- picker remote (cari di server) ----------
function PickerButton({ children, empty, disabled, onClear }: { children: ReactNode; empty?: boolean; disabled?: boolean; onClear?: () => void }) {
  return (
    <Button type="button" variant="secondary" disabled={disabled} className="w-full justify-between font-normal" style={{ borderRadius: "var(--radius-input)", height: 40 }}>
      <span className={cn("truncate", empty && "text-outline")}>{children}</span>
      <span className="flex items-center gap-1">
        {onClear && (
          <span role="button" aria-label="Hapus pilihan" className="inline-flex text-on-surface-variant hover:text-on-surface" onClick={(e) => { e.stopPropagation(); onClear(); }}>
            <Icon name="close" size={14} />
          </span>
        )}
        <Icon name="expand_more" size={16} className="opacity-60" />
      </span>
    </Button>
  );
}

/**
 * Combobox dengan pencarian server (`?q=`, debounce 300 ms): dipakai untuk Work Order, invoice, dan pembayaran.
 * `selectedLabel` = label nilai awal (form edit) sebelum user memilih ulang.
 */
export function RemotePicker<T extends { id: string }>({ resource, query, value, onChange, label, render, placeholder, selectedLabel, disabled, className, emptyText = "Tidak ada hasil." }: {
  resource: string; query: Record<string, string | number | boolean | undefined | null>; value: string | null; onChange: (id: string | null, item?: T) => void; label: (t: T) => string; render?: (t: T) => ReactNode;
  placeholder: string; selectedLabel?: string | null; disabled?: boolean; className?: string; emptyText?: string;
}) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [dq, setDq] = useState("");
  const [picked, setPicked] = useState<T | null>(null);
  useEffect(() => {
    const t = setTimeout(() => setDq(q.trim()), 300);
    return () => clearTimeout(t);
  }, [q]);
  const list = useAll<T>(resource, { ...query, q: dq || undefined }, { enabled: open && !disabled });
  const text = value ? (picked?.id === value ? label(picked) : selectedLabel ?? "Terpilih") : null;
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      align="start"
      width={440}
      className="p-0"
      triggerClassName={cn("w-full", className)}
      trigger={<PickerButton disabled={disabled} empty={!text} onClear={value ? () => { setPicked(null); onChange(null); } : undefined}>{text ?? placeholder}</PickerButton>}
    >
      <div className="text-sm">
        <div className="flex items-center gap-2 border-b border-border px-3 py-2">
          <Icon name="search" size={16} className="text-on-surface-variant" />
          <input className="w-full bg-transparent outline-none" placeholder="Cari…" value={q} onChange={(e) => setQ(e.target.value)} autoFocus />
        </div>
        <div role="listbox" className="max-h-72 overflow-y-auto p-1">
          {list.isLoading && <div className="p-3 text-on-surface-variant">Memuat…</div>}
          {list.isError && <div className="p-3 text-on-error-container">Gagal memuat daftar.</div>}
          {!list.isLoading && !list.isError && (list.data ?? []).length === 0 && <div className="p-3 text-on-surface-variant">{emptyText}</div>}
          {(list.data ?? []).map((it) => (
            <button type="button" role="option" aria-selected={value === it.id} key={it.id} onClick={() => { setPicked(it); onChange(it.id, it); setOpen(false); }} className={cn("w-full cursor-pointer rounded-[var(--radius-sm)] px-2 py-1.5 text-left hover:bg-surface-container", value === it.id && "bg-primary-soft")}>
              {render ? render(it) : label(it)}
            </button>
          ))}
        </div>
      </div>
    </Popover>
  );
}

/** Work Order properti (bukti penggunaan sinking fund / potongan deposit — P4-SCF-04, P4-PND-04). */
export function WorkOrderPicker({ propertyId, value, onChange, selectedLabel, disabled }: { propertyId: string | null; value: string | null; onChange: (id: string | null, wo?: WorkItem) => void; selectedLabel?: string | null; disabled?: boolean }) {
  const { can } = useAuth();
  if (!can("operations.work_orders.view", propertyId)) return <p className="text-sm text-on-surface-variant">Butuh izin melihat Work Order untuk menautkan bukti.</p>;
  return (
    <RemotePicker<WorkItem>
      resource="work-orders"
      query={{ property_id: propertyId ?? undefined }}
      value={value}
      onChange={onChange}
      disabled={disabled || !propertyId}
      placeholder="Cari nomor / judul Work Order…"
      selectedLabel={selectedLabel}
      label={(w) => `${w.number} · ${w.title}`}
      render={(w) => (
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0"><div className="font-mono text-[13px] font-semibold">{w.number}</div><div className="truncate text-xs text-on-surface-variant">{w.title}</div></div>
          <StatusBadge objectType="work_order" status={w.status} />
        </div>
      )}
    />
  );
}

// ---------- tenant & pihak ----------
export function TenantSelect({ propertyId, value, onChange, placeholder = "Pilih tenant…", disabled }: { propertyId: string | null; value: string | null; onChange: (id: string | null, t?: Tenant) => void; placeholder?: string; disabled?: boolean }) {
  const tenants = useAll<Tenant>("tenants", { property_id: propertyId ?? undefined }, { enabled: !!propertyId });
  const list = tenants.data ?? [];
  return (
    <NativeSelect value={value ?? ""} disabled={disabled || !propertyId || tenants.isLoading} onChange={(e) => onChange(e.target.value || null, list.find((t) => t.id === e.target.value))} aria-label="Tenant">
      <option value="">{tenants.isLoading ? "Memuat tenant…" : tenants.isError ? "Gagal memuat tenant" : placeholder}</option>
      {list.map((t) => <option key={t.id} value={t.id}>{t.name} ({t.tenant_code})</option>)}
    </NativeSelect>
  );
}

export interface Party { tenant_id: string | null; unit_location_id: string | null }

/** Pihak piutang/ledger: tenant (seluruh unitnya) atau unit tanpa tenant. */
export function PartyPicker({ propertyId, value, onChange, disabled }: { propertyId: string | null; value: Party; onChange: (p: Party) => void; disabled?: boolean }) {
  const [mode, setMode] = useState<"tenant" | "unit">(value.unit_location_id && !value.tenant_id ? "unit" : "tenant");
  return (
    <div className="space-y-2">
      <Segmented<"tenant" | "unit">
        value={mode}
        disabled={disabled}
        onChange={(m) => { setMode(m); onChange({ tenant_id: null, unit_location_id: null }); }}
        options={[{ value: "tenant", label: "Tenant" }, { value: "unit", label: "Unit" }]}
      />
      {mode === "tenant" ? (
        <TenantSelect propertyId={propertyId} value={value.tenant_id} disabled={disabled} onChange={(id) => onChange({ tenant_id: id, unit_location_id: null })} />
      ) : (
        <LocationPicker propertyId={propertyId} allowTypes={["unit"]} value={value.unit_location_id} disabled={disabled} placeholder="Pilih unit…" onChange={(id) => onChange({ tenant_id: null, unit_location_id: id })} />
      )}
    </div>
  );
}

// ---------- salin ----------
export function CopyField({ label, value, help }: { label: string; value: string; help?: string }) {
  const toast = useToast();
  return (
    <Field label={label} help={help}>
      <div className="flex gap-2">
        <Input readOnly value={value} className="font-mono text-sm" onFocus={(e) => e.currentTarget.select()} />
        <Button variant="secondary" icon="content_copy" onClick={() => copyText(value).then(() => toast.success("Disalin ke clipboard")).catch(toast.error)}>Salin</Button>
      </div>
    </Field>
  );
}
