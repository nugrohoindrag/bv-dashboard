// Komponen bersama Workforce (PRD P2 v2.1 §8): pemilih staf (tunggal & banyak), pemilih shift domain, penanda warna shift,
// dan badge masa berlaku sertifikat/dokumen (status map `validity` + sisa hari).
import { useMemo, useState } from "react";
import { Icon } from "@buildingvision/ui";
import { toneColor } from "@buildingvision/ui/bv";
import { Checkbox, NativeSelect } from "@/components/ui/primitives";
import { ComboBox } from "@/components/bv/pickers";
import { StatusBadge } from "@/components/bv/badges";
import { cn } from "@/lib/utils";
import type { ShiftDefinition, ShiftDomain } from "@/api/types";
import { useShifts, useStaffOptions, type StaffOption } from "./hooks";
import { shiftText, shiftTone, validityText } from "./workforce";

/** Staf tunggal (penerima serah terima, assignee default Cleaning Route). */
export function StaffPicker({ domain, propertyId, teamId, value, onChange, placeholder = "Pilih staf…", className, disabled }: { domain: ShiftDomain; propertyId?: string | null; teamId?: string | null; value?: string | null; onChange: (id: string | null) => void; placeholder?: string; className?: string; disabled?: boolean }) {
  const staff = useStaffOptions(domain, propertyId, teamId);
  return (
    <ComboBox<StaffOption>
      items={staff.options}
      loading={staff.isLoading}
      value={value}
      onChange={(id) => onChange(id)}
      placeholder={staff.available ? placeholder : "Daftar staf tidak tersedia untuk role Anda"}
      className={className}
      disabled={disabled || !staff.available}
      label={(s) => s.name}
      filter={(s, q) => s.name.toLowerCase().includes(q) || (s.hint ?? "").toLowerCase().includes(q)}
      render={(s) => <div className="flex justify-between gap-2"><span>{s.name}</span><span className="truncate text-xs text-on-surface-variant">{s.hint}</span></div>}
    />
  );
}

/** Banyak staf sekaligus (penugasan roster massal P2-SHF-02): cari + centang. */
export function StaffMultiSelect({ domain, propertyId, teamId, value, onChange }: { domain: ShiftDomain; propertyId?: string | null; teamId?: string | null; value: string[]; onChange: (ids: string[]) => void }) {
  const staff = useStaffOptions(domain, propertyId, teamId);
  const [q, setQ] = useState("");
  const list = useMemo(() => {
    const s = q.trim().toLowerCase();
    return s ? staff.options.filter((o) => o.name.toLowerCase().includes(s) || (o.hint ?? "").toLowerCase().includes(s)) : staff.options;
  }, [staff.options, q]);
  if (!staff.available) return <p className="text-sm text-on-surface-variant">Daftar staf tidak tersedia untuk role Anda (butuh iam.users.view atau izin lihat shift domain).</p>;
  const toggle = (id: string) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id]);
  return (
    <div className="rounded-[var(--radius-md)] border border-border">
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        <Icon name="search" size={16} className="text-on-surface-variant" aria-hidden />
        <input className="w-full bg-transparent text-sm outline-none" placeholder="Cari staf…" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Cari staf" />
        {value.length > 0 && <span className="shrink-0 text-xs font-semibold text-primary tnum">{value.length} dipilih</span>}
      </div>
      <div className="max-h-56 space-y-0.5 overflow-y-auto p-1.5">
        {staff.isLoading && <p className="p-2 text-sm text-on-surface-variant">Memuat…</p>}
        {!staff.isLoading && list.length === 0 && <p className="p-2 text-sm text-on-surface-variant">Tidak ada staf.</p>}
        {list.map((o) => (
          <label key={o.id} className="flex cursor-pointer items-center gap-2 rounded-[var(--radius-sm)] px-2 py-1.5 text-sm hover:bg-surface-container">
            <Checkbox checked={value.includes(o.id)} onCheckedChange={() => toggle(o.id)} aria-label={o.name} />
            <span className="min-w-0 flex-1 truncate">{o.name}</span>
            {o.hint && <span className="max-w-[45%] truncate text-xs text-on-surface-variant">{o.hint}</span>}
          </label>
        ))}
      </div>
    </div>
  );
}

/** Titik warna shift dari tone token (warna non-token ditampilkan netral). */
export function ShiftDot({ color, className }: { color?: string | null; className?: string }) {
  const tone = shiftTone(color) ?? "neutral";
  return <span aria-hidden className={cn("inline-block h-2.5 w-2.5 shrink-0 rounded-full", className)} style={{ backgroundColor: toneColor[tone] }} />;
}

/**
 * Pilihan shift domain untuk jadwal (P2-SHF-04): hanya shift aktif property tsb (server menolak shift domain/property lain).
 * Tidak dirender bila user tidak boleh melihat shift domain.
 */
export function ShiftSelect({ domain, propertyId, value, onChange, disabled, id }: { domain: ShiftDomain; propertyId?: string | null; value: string; onChange: (id: string, shift?: ShiftDefinition) => void; disabled?: boolean; id?: string }) {
  const shifts = useShifts(domain, propertyId, { enabled: !!propertyId });
  const list = (shifts.data ?? []).filter((s) => (s.is_active || s.id === value) && (!propertyId || s.property_id === propertyId));
  return (
    <NativeSelect id={id} value={value} disabled={disabled || !propertyId} onChange={(e) => onChange(e.target.value, list.find((s) => s.id === e.target.value))}>
      <option value="">Tanpa shift</option>
      {list.map((s) => <option key={s.id} value={s.id}>{s.code} · {shiftText(s)}{s.is_active ? "" : " (nonaktif)"}</option>)}
    </NativeSelect>
  );
}

/** Masa berlaku sertifikat/dokumen: badge status map `validity` + sisa hari. */
export function ValidityBadge({ status, days, className }: { status: string; days?: number | null; className?: string }) {
  return (
    <span className={cn("inline-flex flex-wrap items-center gap-1.5", className)}>
      <StatusBadge objectType="validity" status={status} />
      {status !== "no_expiry" && days !== null && days !== undefined && <span className="text-xs text-on-surface-variant tnum">{validityText(status, days)}</span>}
    </span>
  );
}
