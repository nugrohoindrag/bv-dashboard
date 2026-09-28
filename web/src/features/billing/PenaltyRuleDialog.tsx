// Aturan denda keterlambatan per property (PRD P4 v2.1 P4-PND-01): persentase dari sisa tagihan atau nominal tetap, per hari /
// per bulan / sekali, masa tenggang, batas maksimum (nominal / % total invoice), tipe invoice yang dikenai. Ringkasan kalimat
// aturan (`summary`) disusun server.
import { useState } from "react";
import { Alert, Button, Checkbox, DialogFooter, Drawer, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { PropertySelect, SectionLabel } from "./shared";
import { INVOICE_TYPES, PENALTY_METHODS, PENALTY_PERIODS, errFields, money, pct, usePropertyChoice, type PenaltyRule } from "./types";

/** Pratinjau kalimat aturan (cermin penaltySummary server) sebelum disimpan. */
function previewSummary(f: { method: string; rate: string; period: string; grace_days: string; max_amount: string; max_pct: string }): string {
  const rate = Number(f.rate) || 0;
  const parts = [f.method === "percent" ? pct(rate) : money(rate), { per_day: "per hari", per_month: "per bulan", once: "sekali" }[f.period] ?? ""];
  let s = parts.join(" ");
  if (f.method === "percent") s += " dari sisa tagihan";
  if (Number(f.grace_days) > 0) s += ` setelah ${Number(f.grace_days)} hari`;
  if (f.max_pct) s += `, maks. ${pct(Number(f.max_pct))}`;
  if (f.max_amount) s += `, maks. ${money(Number(f.max_amount))}`;
  return s;
}

export function PenaltyRuleDialog({ rule, onClose, canManage }: { rule: PenaltyRule | null; onClose: () => void; canManage: boolean }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [pid, setPid] = usePropertyChoice(rule?.property_id);
  const [f, setF] = useState(() => ({
    name: rule?.name ?? "",
    method: rule?.method ?? "percent",
    rate: rule ? String(rule.rate) : "",
    period: rule?.period ?? "per_month",
    grace_days: String(rule?.grace_days ?? 0),
    max_amount: rule?.max_amount ? String(rule.max_amount) : "",
    max_pct: rule?.max_pct ? String(rule.max_pct) : "",
    invoice_types: rule?.invoice_types ?? [],
    is_active: rule?.is_active ?? true,
  }));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ message: string; fields: Record<string, string> } | null>(null);
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((s) => ({ ...s, [k]: v }));
  const rate = Number(f.rate);
  const problems: string[] = [];
  if (!rule && !pid) problems.push("Pilih property");
  if (!f.name.trim()) problems.push("Nama wajib");
  if (!(rate > 0) || (f.method === "percent" && rate > 100)) problems.push(f.method === "percent" ? "Persentase 0–100" : "Nominal > 0");
  if (!(Number(f.grace_days) >= 0 && Number(f.grace_days) <= 365)) problems.push("Masa tenggang 0–365 hari");
  if (f.max_amount && !(Number(f.max_amount) > 0)) problems.push("Batas nominal > 0");
  if (f.max_pct && !(Number(f.max_pct) > 0 && Number(f.max_pct) <= 100)) problems.push("Batas % 0–100");

  const submit = async () => {
    setBusy(true);
    setError(null);
    const body: Record<string, unknown> = {
      name: f.name.trim(),
      method: f.method,
      rate,
      period: f.period,
      grace_days: Number(f.grace_days) || 0,
      invoice_types: f.invoice_types,
      is_active: f.is_active,
      clear_max: true, // batas diset ulang dari form (kosong = tanpa batas)
    };
    if (f.max_amount) body.max_amount = Math.round(Number(f.max_amount));
    if (f.max_pct) body.max_pct = Number(f.max_pct);
    try {
      const out = rule
        ? await api<PenaltyRule>(`billing/penalty-rules/${rule.id}`, { method: "PATCH", body })
        : await api<PenaltyRule>("billing/penalty-rules", { body: { ...body, property_id: pid }, idempotencyKey: uuid() });
      invalidate("list", "all");
      toast.action(rule ? "saved" : "created", `Aturan denda ${out.name}`);
      onClose();
    } catch (e) {
      setError({ message: (e as Error).message, fields: errFields(e) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Drawer open onClose={onClose} width={600} title={rule ? `Aturan denda · ${rule.name}` : "Tambah aturan denda"} description="Denda dihitung otomatis pada invoice lewat jatuh tempo (monoton naik, berhenti saat lunas) dan ditagihkan lewat invoice denda.">
      <div className="space-y-4">
        {error && <Alert variant="critical" title="Aturan belum tersimpan">{error.message}</Alert>}
        {!rule && <PropertySelect value={pid} onChange={setPid} required />}
        <Field label="Nama" required error={error?.fields.name}><Input value={f.name} onChange={(e) => set("name", e.target.value)} placeholder="Denda keterlambatan IPL" disabled={!canManage} /></Field>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Metode"><NativeSelect value={f.method} onChange={(e) => set("method", e.target.value)} disabled={!canManage}>{PENALTY_METHODS.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}</NativeSelect></Field>
          <Field label={f.method === "percent" ? "Tarif (%)" : "Nominal (Rp)"} required error={error?.fields.rate}><Input type="number" inputMode="decimal" min={0} step="any" value={f.rate} onChange={(e) => set("rate", e.target.value)} disabled={!canManage} /></Field>
          <Field label="Periode"><NativeSelect value={f.period} onChange={(e) => set("period", e.target.value)} disabled={!canManage}>{PENALTY_PERIODS.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}</NativeSelect></Field>
          <Field label="Masa tenggang (hari)" help="Hari setelah jatuh tempo sebelum denda mulai."><Input type="number" min={0} max={365} value={f.grace_days} onChange={(e) => set("grace_days", e.target.value)} disabled={!canManage} /></Field>
          <Field label="Batas maksimum (Rp)" help="Kosong = tanpa batas nominal."><Input type="number" min={0} value={f.max_amount} onChange={(e) => set("max_amount", e.target.value)} disabled={!canManage} /></Field>
          <Field label="Batas maksimum (% total invoice)" help="Kosong = tanpa batas persentase."><Input type="number" min={0} max={100} step="any" value={f.max_pct} onChange={(e) => set("max_pct", e.target.value)} disabled={!canManage} /></Field>
        </div>
        <div>
          <SectionLabel>Jenis invoice yang dikenai (kosong = semua kecuali denda)</SectionLabel>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {INVOICE_TYPES.filter((t) => t.value !== "penalty").map((t) => (
              <Checkbox key={t.value} label={t.label} checked={f.invoice_types.includes(t.value)} onCheckedChange={(v) => set("invoice_types", v ? [...f.invoice_types, t.value] : f.invoice_types.filter((x) => x !== t.value))} disabled={!canManage} />
            ))}
          </div>
        </div>
        <Checkbox label="Aktif" checked={f.is_active} onCheckedChange={(v) => set("is_active", v)} disabled={!canManage} />
        <Alert variant="info" title="Ringkasan aturan">{rate > 0 ? previewSummary(f) : "Isi tarif untuk melihat ringkasan."}</Alert>
        {canManage && problems.length > 0 && <p className="text-xs text-on-surface-variant">Lengkapi: {problems.join(" · ")}.</p>}
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>{canManage ? "Batal" : "Tutup"}</Button>
          {canManage && <Button loading={busy} disabled={problems.length > 0} onClick={submit}>{rule ? "Simpan" : "Tambah aturan"}</Button>}
        </DialogFooter>
      </div>
    </Drawer>
  );
}
