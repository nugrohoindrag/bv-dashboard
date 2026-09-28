// Komponen bersama layar Security P2 (Emergency, Parking, Lost & Found, incident lengkap): badge eskalasi/masa simpan,
// plat nomor, catatan data pribadi, pilihan property untuk form create, dan kontrol filter. Status object memakai
// StatusBadge (status map GENERATED). Hook & konstanta ada di hooks.ts.
import { useState, type ReactNode } from "react";
import { Icon } from "@buildingvision/ui";
import { Badge, Field, NativeSelect, SearchInput } from "@/components/ui/primitives";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { displayPlate, escalationLabel } from "./p2";

/** "Eskalasi L{n}" — tidak dirender bila level 0. */
export function EscalationBadge({ level, className }: { level?: number | null; className?: string }) {
  const label = escalationLabel(level);
  if (!label) return null;
  return (
    <Badge tone="error" className={className} title="Level eskalasi">
      <Icon name="priority_high" size={12} aria-hidden />
      {label}
    </Badge>
  );
}

/** "Lewat masa simpan" — barang temuan masih disimpan setelah retention_until (boleh disposal). */
export function DisposalDueBadge({ due }: { due: boolean }) {
  if (!due) return null;
  return (
    <Badge tone="warning">
      <Icon name="timer_off" size={12} aria-hidden />
      Lewat masa simpan
    </Badge>
  );
}

export function PlateText({ plate, className }: { plate: string; className?: string }) {
  return <span className={cn("whitespace-nowrap font-mono text-[13px] font-semibold tracking-wide", className)}>{displayPlate(plate)}</span>;
}

/** Penanda data pribadi (PRD P2 v2.1 P2-NFR-04): hanya role berwenang, akses & perubahan diaudit. */
export function PrivacyNote({ children, className }: { children?: ReactNode; className?: string }) {
  return (
    <p className={cn("flex items-start gap-1.5 text-xs text-on-surface-variant", className)}>
      <Icon name="lock" size={14} className="mt-px shrink-0" aria-hidden />
      <span>{children ?? "Data pribadi — hanya untuk role berwenang; setiap akses dan perubahan tercatat di audit log."}</span>
    </p>
  );
}

/** Pilihan property (hanya tampil bila user memiliki lebih dari satu property). */
export function PropertyField({ value, onChange }: { value: string; onChange: (id: string) => void }) {
  const { properties } = useAuth();
  if (properties.length <= 1) return null;
  return (
    <Field label="Property" required>
      <NativeSelect value={value} onChange={(e) => onChange(e.target.value)}>
        {!value && <option value="">Pilih property…</option>}
        {properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
      </NativeSelect>
    </Field>
  );
}

/** Baris filter daftar: kontrol membungkus; di mobile setiap kontrol selebar layar/setengah layar. */
export function FilterRow({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("mb-3 flex flex-wrap items-center gap-2", className)}>{children}</div>;
}

/** Kotak cari (submit dengan Enter) untuk filter daftar; nilai disimpan pemanggil (mis. useUrlFilters). */
export function SearchBox({ value, onSubmit, placeholder, className }: { value: string; onSubmit: (v: string) => void; placeholder: string; className?: string }) {
  const [q, setQ] = useState(value);
  return (
    <form className={cn("w-full sm:w-64", className)} onSubmit={(e) => { e.preventDefault(); onSubmit(q.trim()); }}>
      <SearchInput placeholder={placeholder} value={q} onChange={(e) => setQ(e.target.value)} aria-label={placeholder} />
    </form>
  );
}

/** Select filter terkontrol ("Label: Semua" + opsi); pemanggil menyimpan nilainya (mis. useUrlFilters). */
export function SelectFilter({ label, value, onChange, options, className }: { label: string; value: string; onChange: (v: string) => void; options: { value: string; label: string }[]; className?: string }) {
  return (
    <NativeSelect className={cn("w-[calc(50%-4px)] sm:w-44", className)} value={value} onChange={(e) => onChange(e.target.value)} aria-label={label}>
      <option value="">{label}: Semua</option>
      {options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
    </NativeSelect>
  );
}
