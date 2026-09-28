// Buat Tagihan Bulanan (PRD P4 v2.1 P4-BRL-02; desain "Buat Tagihan Bulanan"): pilih property, periode, billing rule aktif,
// gabung per unit, jenis invoice, tanggal terbit & jatuh tempo → pratinjau tersimpan (billing run status preview) yang dapat
// direview (pengecualian, sertakan/kecualikan baris) sebelum membuat draft invoice dan menerbitkan massal.
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import { Alert, Button, Checkbox, DatePicker, DialogFooter, Drawer, Field, NativeSelect, Textarea } from "@/components/ui/primitives";
import { ListSkeleton, useToast } from "@/components/bv/common";
import { useAll, useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { PropertySelect, SectionLabel } from "./shared";
import { INVOICE_TYPES, RULE_BASES, addDaysISO, basisLabel, fmtDay, invoiceTypeLabel, ruleRateSummary, todayISO, useBillingSettings, usePropertyChoice, type BillingRule, type BillingRun } from "./types";

export function RunWizard({ onClose }: { onClose: () => void }) {
  const toast = useToast();
  const nav = useNavigate();
  const invalidate = useInvalidate();
  const [pid, setPid] = usePropertyChoice();
  const settings = useBillingSettings(pid || null, !!pid);
  const rulesQ = useAll<BillingRule>("billing/rules", { property_id: pid || undefined, active: true }, { enabled: !!pid });
  const rules = useMemo(() => rulesQ.data ?? [], [rulesQ.data]);
  const [picked, setPicked] = useState<string[] | null>(null); // null = semua rule aktif
  const [f, setF] = useState(() => ({ period: todayISO().slice(0, 7), combine: true, invoice_type: "", issue_date: todayISO(), due_date: "", notes: "" }));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const selected = picked ?? rules.map((r) => r.id);
  const selectedRules = rules.filter((r) => selected.includes(r.id));
  // server: jatuh tempo default = tanggal terbit + due_days terkecil rule terpilih (atau default pengaturan)
  const dueDays = selectedRules.reduce((m, r) => Math.min(m, r.due_days), settings.data?.default_due_days ?? 14);
  const defaultDue = f.issue_date ? addDaysISO(f.issue_date, dueDays) : "";
  const percentMissingBase = selectedRules.filter((r) => r.basis === "percentage" && r.base_rule_id && !selected.includes(r.base_rule_id));
  const valid = !!pid && /^\d{4}-\d{2}$/.test(f.period) && selected.length > 0 && !!f.issue_date && (!f.due_date || f.due_date >= f.issue_date);

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const body: Record<string, unknown> = { property_id: pid, period: f.period, rule_ids: picked === null ? [] : selected, combine: f.combine, issue_date: f.issue_date };
      if (f.invoice_type) body.invoice_type = f.invoice_type;
      if (f.due_date) body.due_date = f.due_date;
      if (f.notes.trim()) body.notes = f.notes.trim();
      const run = await api<BillingRun>("billing/runs", { body, idempotencyKey: uuid() });
      invalidate("list", "all");
      toast.success(`Pratinjau ${run.run_number} dibuat — ${run.line_count} baris, ${run.exception_count} pengecualian`);
      onClose();
      nav(`/billing/runs/${run.id}`);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const toggle = (id: string, on: boolean) => setPicked((cur) => {
    const base = cur ?? rules.map((r) => r.id);
    return on ? [...new Set([...base, id])] : base.filter((x) => x !== id);
  });

  return (
    <Drawer open onClose={onClose} width={680} title="Buat Tagihan Bulanan" description="Hitung tagihan periode untuk semua unit dalam cakupan billing rule. Hasilnya pratinjau — belum ada invoice yang dibuat sampai Anda menekan Buat draft.">
      <div className="space-y-5">
        {error && <Alert variant="critical" title="Pratinjau gagal dibuat">{error}</Alert>}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <PropertySelect value={pid} onChange={(v) => { setPid(v); setPicked(null); }} required />
          <Field label="Periode" required help="Satu bulan kalender (rule triwulan/tahunan hanya ditagih pada bulan jatuhnya).">
            <DatePicker type="month" value={f.period} onChange={(v) => setF({ ...f, period: v })} />
          </Field>
        </div>

        <div>
          <SectionLabel action={rules.length > 1 && (
            <span className="flex gap-2">
              <Button variant="link" size="sm" onClick={() => setPicked(null)}>Semua</Button>
              <Button variant="link" size="sm" onClick={() => setPicked([])}>Kosongkan</Button>
            </span>
          )}>Billing rule aktif ({selected.length}/{rules.length})</SectionLabel>
          {rulesQ.isLoading ? <ListSkeleton rows={3} /> : rules.length === 0 ? (
            <Alert variant="warning" title="Belum ada billing rule aktif">Buat rule tarif terlebih dahulu di <Link className="font-semibold underline" to="/billing/rules">Billing Rules</Link>.</Alert>
          ) : (
            <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
              {rules.map((r) => (
                <li key={r.id} className="flex items-start gap-3 px-3 py-2">
                  <Checkbox checked={selected.includes(r.id)} onCheckedChange={(v) => toggle(r.id, v)} aria-label={`Pilih ${r.name}`} />
                  <Icon name={RULE_BASES.find((b) => b.value === r.basis)?.icon ?? "rule"} size={18} className="mt-0.5 text-on-surface-variant" />
                  <div className="min-w-0 flex-1 text-sm">
                    <div className="font-medium"><span className="font-mono text-xs text-on-surface-variant">{r.code}</span> {r.name}</div>
                    <div className="text-xs text-on-surface-variant">{basisLabel(r.basis)} · {ruleRateSummary(r)} · {invoiceTypeLabel(r.charge_type)}{r.frequency !== "monthly" ? ` · ${r.frequency === "quarterly" ? "triwulan" : "tahunan"}` : ""}{r.prorate ? " · prorata" : ""}</div>
                  </div>
                </li>
              ))}
            </ul>
          )}
          {percentMissingBase.length > 0 && <p className="mt-1 text-xs text-on-surface-variant">Rule persentase ({percentMissingBase.map((r) => r.code).join(", ")}) tetap dihitung dari rule dasarnya walau rule dasar tidak dipilih.</p>}
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="sm:col-span-2">
            <Checkbox label="Gabungkan komponen per unit (satu invoice per unit & pihak tagih)" checked={f.combine} onCheckedChange={(v) => setF({ ...f, combine: v })} />
            <p className="mt-1 pl-7 text-xs text-on-surface-variant">{f.combine ? "Mis. IPL + sinking fund + air dalam satu invoice." : "Satu invoice per baris; jenis invoice mengikuti komponen tiap rule."}</p>
          </div>
          <Field label="Jenis invoice" help={f.combine ? undefined : "Tidak dipakai bila komponen tidak digabung."}>
            <NativeSelect value={f.invoice_type} onChange={(e) => setF({ ...f, invoice_type: e.target.value })} disabled={!f.combine}>
              <option value="">Otomatis (IPL untuk apartemen, selain itu Service Charge)</option>
              {INVOICE_TYPES.filter((t) => t.value !== "penalty").map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
            </NativeSelect>
          </Field>
          <Field label="Tanggal terbit" required><DatePicker value={f.issue_date} onChange={(v) => setF({ ...f, issue_date: v })} /></Field>
          <Field label="Jatuh tempo" help={f.due_date ? undefined : `Kosong = ${fmtDay(defaultDue)} (terbit + ${dueDays} hari)`} error={f.due_date && f.due_date < f.issue_date ? "Harus setelah tanggal terbit" : undefined}>
            <DatePicker value={f.due_date} onChange={(v) => setF({ ...f, due_date: v })} />
          </Field>
          <Field label="Catatan"><Textarea rows={2} className="min-h-[40px]" value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
        </div>

        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="calculate" loading={busy} disabled={!valid} onClick={submit}>Hitung pratinjau</Button>
        </DialogFooter>
      </div>
    </Drawer>
  );
}
