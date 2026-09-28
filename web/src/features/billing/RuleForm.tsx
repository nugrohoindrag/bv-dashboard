// Form Billing Rule (PRD P4 v2.1 P4-BRL-01, D-P4-05 — semua dasar hitung tersedia per rule): tetap per unit, per m², per tipe
// unit, pemakaian meter (tarif utilitas), persentase rule lain, per kendaraan. Periode (bulanan/triwulan/tahunan), tanggal terbit
// & jatuh tempo, pajak (kosong = ikut pengaturan billing), prorata (P4-BRL-04), pihak tagih, cakupan unit/okupansi/lokasi,
// generate otomatis (P4-BRL-03), masa berlaku.
import { useMemo, useState } from "react";
import { Icon } from "@buildingvision/ui";
import { Alert, Button, Checkbox, DatePicker, DialogFooter, Drawer, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { FormSkeleton, useToast } from "@/components/bv/common";
import { LocationPicker } from "@/components/bv/pickers";
import { useAll, useInvalidate, useLocationTree } from "@/api/hooks";
import type { TreeNode } from "@/api/types";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { PropertySelect, SectionLabel } from "./shared";
import {
  BILL_TO, FREQUENCIES, INVOICE_TYPES, METER_TYPES, OCCUPANCY_STATUSES, RULE_BASES, UNIT_TYPES, VEHICLE_TYPES, errFields, money, usePropertyChoice,
  type BillingRule, type UtilityTariff,
} from "./types";

const NIL = "00000000-0000-0000-0000-000000000000";
type TaxChoice = "settings" | "none" | "custom";

function flatten(node: TreeNode | undefined, out: Record<string, string> = {}, prefix: string[] = []): Record<string, string> {
  if (!node) return out;
  const p = node.location_type === "property" ? [] : [...prefix, node.name];
  if (node.location_type !== "property") out[node.id] = p.join(" / ");
  node.children?.forEach((c) => flatten(c, out, p));
  return out;
}

function toggle(list: string[], v: string, on: boolean) {
  return on ? [...new Set([...list, v])] : list.filter((x) => x !== v);
}

export function RuleForm({ rule, onClose }: { rule?: BillingRule | null; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { can } = useAuth();
  const [pid, setPid] = usePropertyChoice(rule?.property_id);
  const canManage = can("billing.rules.manage", pid || null);
  const editing = !!rule;
  const [f, setF] = useState(() => ({
    code: rule?.code ?? "",
    name: rule?.name ?? "",
    description: rule?.description ?? "",
    basis: rule?.basis ?? "fixed_per_unit",
    rate: rule && rule.rate ? String(rule.rate) : "",
    unit_label: rule?.unit_label ?? "",
    meter_type: rule?.meter_type ?? "electricity",
    tariff_id: rule?.tariff_id ?? "",
    base_rule_id: rule?.base_rule_id ?? "",
    charge_type: rule?.charge_type ?? "",
    frequency: rule?.frequency ?? "monthly",
    issue_day: String(rule?.issue_day ?? 1),
    due_days: String(rule?.due_days ?? 14),
    tax: (rule ? (rule.tax_rate === null ? "settings" : rule.tax_rate === 0 ? "none" : "custom") : "settings") as TaxChoice,
    tax_rate: rule?.tax_rate ? String(rule.tax_rate) : "",
    prorate: rule?.prorate ?? false,
    bill_to: rule?.bill_to ?? "tenant",
    unit_types: rule?.unit_types ?? [],
    occupancy_statuses: rule?.occupancy_statuses ?? ["occupied"],
    scope_location_ids: rule?.scope_location_ids ?? [],
    auto_generate: rule?.auto_generate ?? false,
    is_active: rule?.is_active ?? true,
    effective_from: rule?.effective_from ?? "",
    effective_until: rule?.effective_until ?? "",
  }));
  const [rates, setRates] = useState<Record<string, string>>(() => Object.fromEntries(Object.entries(rule?.rates ?? {}).map(([k, v]) => [k, String(v)])));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ message: string; fields: Record<string, string> } | null>(null);
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((s) => ({ ...s, [k]: v }));

  const rulesQ = useAll<BillingRule>("billing/rules", { property_id: pid || undefined }, { enabled: !!pid && f.basis === "percentage" });
  const tariffsQ = useAll<UtilityTariff>("billing/utility-tariffs", { property_id: pid || undefined }, { enabled: !!pid && f.basis === "meter_usage" && can("billing.meters.view", pid || null) });
  const tree = useLocationTree(pid || null);
  const locNames = useMemo(() => flatten(tree.data), [tree.data]);
  const baseRules = (rulesQ.data ?? []).filter((r) => r.basis !== "percentage" && r.id !== rule?.id);
  const tariffs = (tariffsQ.data ?? []).filter((t) => t.meter_type === f.meter_type);

  const rateNum = Number(f.rate);
  const problems: string[] = [];
  if (!pid) problems.push("Pilih property");
  if (!f.code.trim() || !f.name.trim()) problems.push("Kode dan nama wajib");
  if (["fixed_per_unit", "per_area_m2", "per_vehicle"].includes(f.basis) && !(rateNum > 0) && !(f.basis === "per_vehicle" && Object.values(rates).some((v) => Number(v) > 0))) problems.push("Isi tarif");
  if (f.basis === "per_unit_type" && !UNIT_TYPES.some((u) => rates[u.value] !== undefined && rates[u.value] !== "")) problems.push("Isi tarif minimal satu tipe unit");
  if (f.basis === "percentage" && (!f.base_rule_id || !(rateNum > 0) || rateNum > 100)) problems.push("Pilih rule dasar dan persentase 0–100");
  if (Object.values(rates).some((v) => v !== "" && Number(v) < 0) || rateNum < 0) problems.push("Tarif tidak boleh negatif");
  const issueDay = Number(f.issue_day);
  const dueDays = Number(f.due_days);
  if (!(issueDay >= 1 && issueDay <= 28)) problems.push("Tanggal terbit 1–28");
  if (!(dueDays >= 0 && dueDays <= 120)) problems.push("Jatuh tempo 0–120 hari");
  if (f.tax === "custom" && !(Number(f.tax_rate) >= 0 && Number(f.tax_rate) <= 100 && f.tax_rate !== "")) problems.push("Tarif pajak 0–100%");
  if (f.effective_from && f.effective_until && f.effective_until < f.effective_from) problems.push("Masa berlaku tidak valid");

  const submit = async () => {
    if (problems.length || !canManage) return;
    setBusy(true);
    setError(null);
    const usedRates: Record<string, number> = {};
    if (f.basis === "per_unit_type" || f.basis === "per_vehicle") for (const [k, v] of Object.entries(rates)) if (v !== "" && Number.isFinite(Number(v))) usedRates[k] = Number(v);
    const body: Record<string, unknown> = {
      code: f.code.trim(),
      name: f.name.trim(),
      description: f.description.trim(),
      basis: f.basis,
      rate: f.basis === "per_unit_type" || f.basis === "meter_usage" ? 0 : rateNum || 0,
      rates: usedRates,
      charge_type: f.charge_type,
      frequency: f.frequency,
      issue_day: issueDay,
      due_days: dueDays,
      prorate: f.prorate,
      unit_label: f.basis === "fixed_per_unit" ? f.unit_label.trim() || null : null,
      bill_to: f.bill_to,
      unit_types: f.unit_types,
      occupancy_statuses: f.occupancy_statuses,
      scope_location_ids: f.scope_location_ids,
      auto_generate: f.auto_generate,
      is_active: f.is_active,
      effective_from: f.effective_from,
      effective_until: f.effective_until,
      meter_type: f.basis === "meter_usage" ? f.meter_type : "",
    };
    if (f.tax === "settings") body.clear_tax_rate = true;
    else body.tax_rate = f.tax === "none" ? 0 : Number(f.tax_rate);
    if (f.basis === "meter_usage" && f.tariff_id) body.tariff_id = f.tariff_id;
    else if (editing) body.tariff_id = NIL;
    if (f.basis === "percentage") body.base_rule_id = f.base_rule_id;
    else if (editing) body.base_rule_id = NIL;
    try {
      const out = rule
        ? await api<BillingRule>(`billing/rules/${rule.id}`, { method: "PATCH", body })
        : await api<BillingRule>("billing/rules", { body: { ...body, property_id: pid }, idempotencyKey: uuid() });
      invalidate("list", "all", "one");
      toast.action(editing ? "saved" : "created", `Billing rule ${out.code}`);
      onClose();
    } catch (e) {
      setError({ message: (e as Error).message, fields: errFields(e) });
    } finally {
      setBusy(false);
    }
  };

  const basis = RULE_BASES.find((b) => b.value === f.basis);
  const rateField = (label: string, help?: string) => (
    <Field label={label} required help={help} error={error?.fields.rate}>
      <Input type="number" inputMode="decimal" min={0} step="any" value={f.rate} onChange={(e) => set("rate", e.target.value)} disabled={!canManage} />
    </Field>
  );

  return (
    <Drawer open onClose={onClose} width={740} title={rule ? `Billing rule · ${rule.code}` : "Tambah Billing Rule"} description="Aturan pembentukan tagihan berulang per property. Perubahan tarif berlaku untuk periode berikutnya; invoice lama tidak berubah.">
      {!canManage && editing && <Alert variant="info" className="mb-4">Anda hanya dapat melihat rule ini (perlu billing.rules.manage untuk mengubah).</Alert>}
      <div className="space-y-5">
        {error && <Alert variant="critical" title="Rule belum tersimpan">{error.message}</Alert>}
        {!editing && <PropertySelect value={pid} onChange={setPid} required />}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <Field label="Kode" required error={error?.fields.code}><Input value={f.code} onChange={(e) => set("code", e.target.value.toUpperCase())} placeholder="IPL" disabled={!canManage} /></Field>
          <Field label="Nama" required className="sm:col-span-2"><Input value={f.name} onChange={(e) => set("name", e.target.value)} placeholder="IPL per m²" disabled={!canManage} /></Field>
        </div>

        <div>
          <SectionLabel>Dasar hitung</SectionLabel>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2" role="radiogroup" aria-label="Dasar hitung">
            {RULE_BASES.map((b) => {
              const active = f.basis === b.value;
              return (
                <button
                  key={b.value}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  disabled={!canManage}
                  onClick={() => set("basis", b.value)}
                  className={cn("flex items-start gap-2 rounded-[var(--radius-md)] border px-3 py-2 text-left transition-colors", active ? "border-primary bg-primary-container text-on-primary-container" : "border-border bg-surface hover:bg-surface-container-low")}
                >
                  <Icon name={b.icon} size={20} className="mt-0.5 shrink-0" />
                  <span><span className="block text-sm font-semibold">{b.label}</span><span className={cn("block text-xs", !active && "text-on-surface-variant")}>{b.hint}</span></span>
                </button>
              );
            })}
          </div>
        </div>

        <div className="rounded-[var(--radius-md)] border border-border p-3">
          <SectionLabel>Tarif · {basis?.label}</SectionLabel>
          {f.basis === "fixed_per_unit" && (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              {rateField("Tarif per unit (Rp)", f.rate ? `${money(rateNum)} per unit per periode` : undefined)}
              <Field label="Satuan (opsional)"><Input value={f.unit_label} onChange={(e) => set("unit_label", e.target.value)} placeholder="bln" disabled={!canManage} /></Field>
            </div>
          )}
          {f.basis === "per_area_m2" && rateField("Tarif per m² (Rp)", "Dikalikan luas unit (units.area_m2). Unit tanpa luas → pengecualian di pratinjau.")}
          {f.basis === "per_unit_type" && (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              {UNIT_TYPES.map((u) => (
                <Field key={u.value} label={`Tarif ${u.label} (Rp)`} help="Kosongkan bila tipe ini tidak ditagih.">
                  <Input type="number" inputMode="decimal" min={0} step="any" value={rates[u.value] ?? ""} onChange={(e) => setRates({ ...rates, [u.value]: e.target.value })} disabled={!canManage} />
                </Field>
              ))}
            </div>
          )}
          {f.basis === "meter_usage" && (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Jenis meter" required>
                <NativeSelect value={f.meter_type} onChange={(e) => setF((s) => ({ ...s, meter_type: e.target.value, tariff_id: "" }))} disabled={!canManage}>{METER_TYPES.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}</NativeSelect>
              </Field>
              <Field label="Tarif utilitas" help={tariffsQ.isError ? "Daftar tarif tidak dapat dimuat." : "Tarif pada meter diutamakan; tarif rule dipakai bila meter tidak punya tarif."}>
                <NativeSelect value={f.tariff_id} onChange={(e) => set("tariff_id", e.target.value)} disabled={!canManage}>
                  <option value="">Ikut tarif meter</option>
                  {tariffs.map((t) => <option key={t.id} value={t.id}>{t.name} · {money(t.rate)}/{t.unit_label}{t.fixed_charge ? ` + abonemen ${money(t.fixed_charge)}` : ""}{t.is_active ? "" : " (nonaktif)"}</option>)}
                </NativeSelect>
              </Field>
            </div>
          )}
          {f.basis === "percentage" && (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Rule dasar" required error={error?.fields.base_rule_id} help="Mis. sinking fund = persen dari IPL. Rule dasar tidak boleh berbasis persentase.">
                <NativeSelect value={f.base_rule_id} onChange={(e) => set("base_rule_id", e.target.value)} disabled={!canManage}>
                  <option value="">Pilih rule…</option>
                  {baseRules.map((r) => <option key={r.id} value={r.id}>{r.code} · {r.name}</option>)}
                </NativeSelect>
              </Field>
              <Field label="Persentase (%)" required error={error?.fields.rate}>
                <Input type="number" inputMode="decimal" min={0} max={100} step="any" value={f.rate} onChange={(e) => set("rate", e.target.value)} disabled={!canManage} />
              </Field>
            </div>
          )}
          {f.basis === "per_vehicle" && (
            <div className="space-y-3">
              {rateField("Tarif default per kendaraan (Rp)", "Berlaku untuk jenis kendaraan tanpa tarif khusus. Tarif pada izin parkir diutamakan.")}
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                {VEHICLE_TYPES.map((v) => (
                  <Field key={v.value} label={`${v.label} (Rp)`}>
                    <Input type="number" inputMode="decimal" min={0} step="any" placeholder="default" value={rates[v.value] ?? ""} onChange={(e) => setRates({ ...rates, [v.value]: e.target.value })} disabled={!canManage} />
                  </Field>
                ))}
              </div>
            </div>
          )}
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Komponen tagihan" help="Jenis item pada invoice (charge type).">
            <NativeSelect value={f.charge_type} onChange={(e) => set("charge_type", e.target.value)} disabled={!canManage}>
              <option value="">Otomatis ({f.basis === "meter_usage" ? "utilitas" : f.basis === "per_vehicle" ? "parkir" : "service charge"})</option>
              {INVOICE_TYPES.filter((t) => t.value !== "penalty").map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
            </NativeSelect>
          </Field>
          <Field label="Periode tagihan">
            <NativeSelect value={f.frequency} onChange={(e) => set("frequency", e.target.value)} disabled={!canManage}>{FREQUENCIES.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect>
          </Field>
          <Field label="Tanggal terbit (1–28)" help="Hari generate otomatis setiap periode."><Input type="number" min={1} max={28} value={f.issue_day} onChange={(e) => set("issue_day", e.target.value)} disabled={!canManage} /></Field>
          <Field label="Jatuh tempo (hari setelah terbit)"><Input type="number" min={0} max={120} value={f.due_days} onChange={(e) => set("due_days", e.target.value)} disabled={!canManage} /></Field>
          <Field label="Pajak">
            <NativeSelect value={f.tax} onChange={(e) => set("tax", e.target.value as TaxChoice)} disabled={!canManage}>
              <option value="settings">Ikut pengaturan billing</option>
              <option value="none">Tanpa pajak (0%)</option>
              <option value="custom">Tarif khusus…</option>
            </NativeSelect>
          </Field>
          {f.tax === "custom" && <Field label="Tarif pajak (%)"><Input type="number" min={0} max={100} step="0.01" value={f.tax_rate} onChange={(e) => set("tax_rate", e.target.value)} disabled={!canManage} /></Field>}
          <Field label="Ditagihkan ke">
            <NativeSelect value={f.bill_to} onChange={(e) => set("bill_to", e.target.value)} disabled={!canManage}>{BILL_TO.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect>
          </Field>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <SectionLabel>Tipe unit (kosong = semua)</SectionLabel>
            <div className="flex flex-wrap gap-3">{UNIT_TYPES.map((u) => <Checkbox key={u.value} label={u.label} checked={f.unit_types.includes(u.value)} onCheckedChange={(v) => set("unit_types", toggle(f.unit_types, u.value, v))} disabled={!canManage} />)}</div>
          </div>
          <div>
            <SectionLabel>Status hunian (kosong = semua)</SectionLabel>
            <div className="flex flex-wrap gap-3">{OCCUPANCY_STATUSES.map((o) => <Checkbox key={o.value} label={o.label} checked={f.occupancy_statuses.includes(o.value)} onCheckedChange={(v) => set("occupancy_statuses", toggle(f.occupancy_statuses, o.value, v))} disabled={!canManage} />)}</div>
          </div>
        </div>

        <div>
          <SectionLabel>Cakupan lokasi (kosong = seluruh property)</SectionLabel>
          {tree.isLoading && pid ? <FormSkeleton fields={1} /> : (
            <div className="space-y-2">
              {f.scope_location_ids.length > 0 && (
                <ul className="flex flex-wrap gap-1.5">
                  {f.scope_location_ids.map((lid) => (
                    <li key={lid} className="inline-flex items-center gap-1 rounded-full bg-surface-container-high px-2.5 py-1 text-xs">
                      {locNames[lid] ?? lid.slice(0, 8)}
                      {canManage && <button type="button" aria-label="Hapus lokasi" onClick={() => set("scope_location_ids", f.scope_location_ids.filter((x) => x !== lid))}><Icon name="close" size={12} /></button>}
                    </li>
                  ))}
                </ul>
              )}
              {canManage && pid && <LocationPicker propertyId={pid} value={null} onChange={(id) => id && set("scope_location_ids", toggle(f.scope_location_ids, id, true))} allowTypes={["building", "tower", "floor", "area", "unit"]} placeholder="Tambah building / tower / lantai…" className="sm:w-96" />}
            </div>
          )}
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Berlaku mulai"><DatePicker value={f.effective_from} onChange={(v) => set("effective_from", v)} disabled={!canManage} /></Field>
          <Field label="Berlaku sampai"><DatePicker value={f.effective_until} onChange={(v) => set("effective_until", v)} disabled={!canManage} /></Field>
        </div>
        <div className="space-y-2 rounded-[var(--radius-md)] bg-surface-container-low px-3 py-2">
          <Checkbox label="Prorata untuk penghuni masuk/keluar di tengah periode" checked={f.prorate} onCheckedChange={(v) => set("prorate", v)} disabled={!canManage} />
          <Checkbox label="Generate otomatis setiap periode (draft invoice untuk direview Finance)" checked={f.auto_generate} onCheckedChange={(v) => set("auto_generate", v)} disabled={!canManage} />
          <Checkbox label="Aktif" checked={f.is_active} onCheckedChange={(v) => set("is_active", v)} disabled={!canManage} />
        </div>
        <Field label="Deskripsi"><Textarea rows={2} value={f.description} onChange={(e) => set("description", e.target.value)} disabled={!canManage} /></Field>

        {canManage && problems.length > 0 && <p className="text-xs text-on-surface-variant">Lengkapi: {problems.join(" · ")}.</p>}
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>{canManage ? "Batal" : "Tutup"}</Button>
          {canManage && <Button loading={busy} disabled={problems.length > 0} onClick={submit}>{editing ? "Simpan" : "Tambah rule"}</Button>}
        </DialogFooter>
      </div>
    </Drawer>
  );
}
