// Tab Shift (PRD P2 v2.1 P2-SHF-01): definisi shift per domain & property — kode, nama, jam mulai–selesai (lintas tengah
// malam), istirahat, minimal staf on-duty (kapasitas), warna, aktif. Kelola: {domain}.shifts.manage.
import { useMemo, useState } from "react";
import type { ColumnDef } from "@tanstack/react-table";
import { Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { CellTitle } from "@/components/bv/cells";
import { useToast } from "@/components/bv/common";
import { useInvalidate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fieldErrorsOf, problemOf } from "@/lib/problem";
import type { ShiftDefinition, ShiftDomain } from "@/api/types";
import { PropertyField } from "@/features/security/shared";
import { usePropertyChoice } from "@/features/security/hooks";
import { useShifts } from "./hooks";
import { ShiftDot } from "./components";
import { SHIFT_DOMAIN_LABEL, SHIFT_TONES, fmtHours, shiftTone, shiftWindow } from "./workforce";
import { clearableText } from "@/lib/clearable";

export function ShiftDefinitionsTab({ domain }: { domain: ShiftDomain }) {
  const { propertyId, properties, can } = useAuth();
  const shifts = useShifts(domain, propertyId);
  const canManage = can(`${domain}.shifts.manage`);
  const [edit, setEdit] = useState<ShiftDefinition | "new" | null>(null);
  const columns = useMemo<ColumnDef<ShiftDefinition, unknown>[]>(() => {
    const propName = (id: string) => properties.find((p) => p.id === id)?.name ?? "";
    // Tabel disederhanakan (29 Sep 2026): kode (kecil) di atas nama shift; property (lintas property) & istirahat di tooltip.
    return [
      {
        id: "name", header: "Shift", meta: { mobile: "primary" },
        cell: ({ row: { original: s } }) => (
          <div className="flex min-w-0 items-center gap-2" title={!propertyId && properties.length > 1 ? propName(s.property_id) || undefined : undefined}>
            <ShiftDot color={s.color} />
            <CellTitle code={s.code} title={s.name} />
          </div>
        ),
      },
      { id: "time", header: "Jam", meta: { mobile: "secondary" }, size: 170, cell: ({ row: { original: s } }) => <span className="tnum whitespace-nowrap">{s.start_time}–{s.end_time}{s.crosses_midnight && <span className="ml-1 text-xs text-on-surface-variant">(+1 hari)</span>}</span> },
      { id: "duration", header: "Durasi", meta: { mobile: "secondary" }, size: 120, cell: ({ row: { original: s } }) => <span className="tnum whitespace-nowrap" title={s.break_minutes ? `Istirahat ${s.break_minutes} mnt` : undefined}>{fmtHours(s.duration_minutes)}</span> },
      { id: "min_staff", header: "Min. staf", meta: { mobile: "secondary" }, size: 100, cell: ({ row: { original: s } }) => <span className="tnum whitespace-nowrap">{s.min_staff}</span> },
      { id: "status", header: "Status", meta: { mobile: "status" }, size: 110, cell: ({ row: { original: s } }) => (s.is_active ? <Badge tone="success">Aktif</Badge> : <Badge>Nonaktif</Badge>) },
    ];
  }, [propertyId, properties]);
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-on-surface-variant">Shift {SHIFT_DOMAIN_LABEL[domain]} dipakai roster, papan on-duty, jadwal {domain === "security" ? "patrol" : "cleaning & Cleaning Route"}, dan kapasitas tim.</p>
        {canManage && <Button icon="add" onClick={() => setEdit("new")}>Tambah Shift</Button>}
      </div>
      <DataGrid
        columns={columns}
        rows={shifts.data ?? []}
        rowId={(r) => r.id}
        onRowClick={canManage ? (r) => { setEdit(r); } : undefined}
        loading={shifts.isLoading}
        error={shifts.error}
        onRetry={() => shifts.refetch()}
        empty={{ icon: "schedule", title: "Belum ada shift", description: `Definisikan shift ${SHIFT_DOMAIN_LABEL[domain]} (mis. Pagi 07:00–15:00, Malam 23:00–07:00) sebelum menyusun roster.`, action: canManage ? <Button icon="add" onClick={() => setEdit("new")}>Tambah Shift</Button> : undefined }}
      />
      {edit && <ShiftDialog domain={domain} shift={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

function ShiftDialog({ domain, shift, onClose }: { domain: ShiftDomain; shift: ShiftDefinition | null; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [pid, setPid] = usePropertyChoice(shift?.property_id);
  const [f, setF] = useState({
    code: shift?.code ?? "", name: shift?.name ?? "", start_time: shift?.start_time ?? "07:00", end_time: shift?.end_time ?? "15:00",
    break_minutes: String(shift?.break_minutes ?? 0), min_staff: String(shift?.min_staff ?? 1), color: shiftTone(shift?.color) ?? "", is_active: shift?.is_active ?? true,
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const win = shiftWindow(f.start_time, f.end_time);
  const timesChanged = !!shift && (shift.start_time !== f.start_time || shift.end_time !== f.end_time);
  const set = (k: keyof typeof f, v: string | boolean) => setF((s) => ({ ...s, [k]: v }));
  const submit = async () => {
    const e: Record<string, string> = {};
    if (!f.code.trim()) e.code = "Kode wajib";
    if (!f.name.trim()) e.name = "Nama wajib";
    if (!win) e.end_time = "Jam selesai harus berbeda dari jam mulai (format HH:MM)";
    if (Number(f.min_staff) < 0) e.min_staff = "Tidak boleh negatif";
    if (Number(f.break_minutes) < 0) e.break_minutes = "Tidak boleh negatif";
    if (!shift && !pid) e.property_id = "Pilih property";
    setErrors(e);
    if (Object.keys(e).length) return;
    const body = { code: f.code.trim().toUpperCase(), name: f.name.trim(), start_time: f.start_time, end_time: f.end_time, break_minutes: Number(f.break_minutes) || 0, min_staff: Number(f.min_staff) || 0, color: clearableText(f.color, shift?.color) };
    setBusy(true);
    try {
      if (shift) await api(`${domain}/shifts/${shift.id}`, { method: "PATCH", body: { ...body, is_active: f.is_active } });
      else await api(`${domain}/shifts`, { body: { ...body, property_id: pid } });
      invalidate("shifts", "roster", "on-duty");
      toast.action(shift ? "updated" : "created", `Shift ${body.code}`);
      onClose();
    } catch (err) {
      const fe = fieldErrorsOf(err);
      if (problemOf(err).code === "DUPLICATE_CODE") fe.code = "Kode shift sudah dipakai di property ini";
      setErrors(fe);
      toast.failed(shift ? "updated" : "created", err, "Shift");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={shift ? `Shift ${shift.code} · ${shift.name}` : `Tambah Shift ${SHIFT_DOMAIN_LABEL[domain]}`}>
        <div className="space-y-4">
          {!shift && <PropertyField value={pid} onChange={setPid} />}
          {errors.property_id && <p className="text-sm text-on-error-container">{errors.property_id}</p>}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Field label="Kode" required error={errors.code}><Input value={f.code} onChange={(e) => set("code", e.target.value)} placeholder="PAGI" maxLength={20} /></Field>
            <Field label="Nama" required error={errors.name} className="sm:col-span-2"><Input value={f.name} onChange={(e) => set("name", e.target.value)} placeholder="Shift Pagi" /></Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Jam mulai" required error={errors.start_time}><Input type="time" value={f.start_time} onChange={(e) => set("start_time", e.target.value)} /></Field>
            <Field label="Jam selesai" required error={errors.end_time}><Input type="time" value={f.end_time} onChange={(e) => set("end_time", e.target.value)} /></Field>
          </div>
          <p className="text-sm text-on-surface-variant" data-testid="shift-preview">
            {win ? <>Durasi {fmtHours(win.durationMinutes)}{win.crossesMidnight ? " · lintas tengah malam (selesai hari berikutnya)" : ""}</> : "Jam selesai harus berbeda dari jam mulai."}
          </p>
          {timesChanged && <p className="rounded-[var(--radius-md)] bg-info-container px-3 py-2 text-sm text-on-info-container">Jam roster mulai hari ini ke depan ikut dihitung ulang.</p>}
          <div className="grid grid-cols-2 gap-3">
            <Field label="Istirahat (menit)" error={errors.break_minutes}><Input type="number" min={0} value={f.break_minutes} onChange={(e) => set("break_minutes", e.target.value)} /></Field>
            <Field label="Minimal staf on-duty" help="Dasar deteksi kekurangan staf (Attention Required)" error={errors.min_staff}><Input type="number" min={0} value={f.min_staff} onChange={(e) => set("min_staff", e.target.value)} /></Field>
          </div>
          <Field label="Warna penanda">
            <NativeSelect value={f.color} onChange={(e) => set("color", e.target.value)}>
              <option value="">Default</option>
              {SHIFT_TONES.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
            </NativeSelect>
          </Field>
          {shift && <Checkbox label="Aktif (shift nonaktif tidak dapat dipakai untuk roster baru)" checked={f.is_active} onCheckedChange={(v) => set("is_active", v)} />}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} onClick={() => void submit()}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
