// Komponen bersama Unit Sales & Rental: pemilih unit, editor dokumen/referensi, kartu kredensial akun Tenant App.
import { useState } from "react";
import { Icon } from "@buildingvision/ui";
import { Alert, Button, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { useAll } from "@/api/hooks";
import type { Location } from "@/api/types";
import type { Doc, Onboarding } from "./commercial-api";

/** Unit milik property (bukan kamar hotel); menampilkan status hunian. */
export function UnitSelect({ propertyId, value, onChange, disabled }: { propertyId: string; value: string; onChange: (id: string) => void; disabled?: boolean }) {
  const units = useAll<Location>("units", { property_id: propertyId }, { enabled: !!propertyId });
  const rows = (units.data ?? []).filter((u) => (u.details as Record<string, unknown> | undefined)?.unit_type !== "hotel_room");
  return (
    <NativeSelect value={value} onChange={(e) => onChange(e.target.value)} disabled={disabled}>
      <option value="">— pilih unit —</option>
      {rows.map((u) => {
        const d = (u.details ?? {}) as Record<string, unknown>;
        return <option key={u.id} value={u.id}>{String(d.unit_number ?? u.name)} · {u.name}{d.occupancy_status ? ` · ${String(d.occupancy_status)}` : ""}</option>;
      })}
    </NativeSelect>
  );
}

/** Document/reference tracking (PRD §3.10): nama + nomor referensi + catatan; isi dokumen sensitif tidak disimpan di sini. */
export function DocumentsEditor({ value, onChange }: { value: Doc[]; onChange: (docs: Doc[]) => void }) {
  const [draft, setDraft] = useState<Doc>({ name: "", reference: "", note: "" });
  const add = () => { if (!draft.name.trim()) return; onChange([...value, { name: draft.name.trim(), reference: draft.reference || null, note: draft.note || null }]); setDraft({ name: "", reference: "", note: "" }); };
  return (
    <div className="space-y-2">
      {value.length > 0 && (
        <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
          {value.map((d, i) => (
            <li key={`${d.name}-${i}`} className="flex items-center justify-between gap-2 px-3 py-1.5">
              <span><span className="font-medium">{d.name}</span>{d.reference ? <span className="ml-1 font-mono text-xs text-muted-foreground">{d.reference}</span> : null}{d.note ? <span className="ml-1 text-xs text-muted-foreground">· {d.note}</span> : null}</span>
              <button type="button" className="text-muted-foreground hover:text-error" onClick={() => onChange(value.filter((_, j) => j !== i))} aria-label="Hapus dokumen"><Icon name="close" size={16} /></button>
            </li>
          ))}
        </ul>
      )}
      <div className="grid grid-cols-12 gap-2">
        <Field label="Dokumen" className="col-span-4"><Input placeholder="mis. PPJB, KTP" value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></Field>
        <Field label="Referensi" className="col-span-4"><Input placeholder="nomor / link" value={draft.reference ?? ""} onChange={(e) => setDraft({ ...draft, reference: e.target.value })} /></Field>
        <Field label="Catatan" className="col-span-3"><Input value={draft.note ?? ""} onChange={(e) => setDraft({ ...draft, note: e.target.value })} /></Field>
        <div className="col-span-1 flex items-end"><Button type="button" variant="secondary" size="sm" onClick={add} disabled={!draft.name.trim()} aria-label="Tambah"><Icon name="add" size={16} /></Button></div>
      </div>
    </div>
  );
}

export function DocList({ docs }: { docs: Doc[] }) {
  if (docs.length === 0) return <span className="text-muted-foreground">—</span>;
  return <ul className="space-y-0.5">{docs.map((d, i) => <li key={`${d.name}-${i}`}>{d.name}{d.reference ? <span className="ml-1 font-mono text-xs text-muted-foreground">{d.reference}</span> : null}{d.note ? <span className="ml-1 text-xs text-muted-foreground">· {d.note}</span> : null}</li>)}</ul>;
}

/** Kredensial akun Tenant App yang dibuat saat onboarding — tampil sekali (AC-11). */
export function OnboardingCard({ ob }: { ob: Onboarding }) {
  return (
    <Alert variant="success" title={`Tenant onboarding selesai · ${ob.tenant_code}`}>
      Tenant & Occupant dibuat pada unit. {ob.temporary_password ? <>Akun Tenant App: <code>{ob.account_email}</code> · password sementara <code className="font-mono text-base">{ob.temporary_password}</code> — sampaikan ke penghuni (tampil sekali).</> : "Tidak ada akun Tenant App yang dibuat."}
    </Alert>
  );
}
