// Form invoice (PRD P4 v2.1 P4-INV-01..06, B-07, B-13): buat invoice manual atau edit draft dari Web. Tenant opsional (unit
// saja boleh), jatuh tempo dikirim sebagai tanggal YYYY-MM-DD (akhir hari zona waktu property — bukan UTC tengah malam),
// pajak per item dari pengaturan billing, pratinjau subtotal/pajak/total sama dengan hitungan server. Edit draft = PATCH If-Match.
import { useState } from "react";
import { Alert, Button, Checkbox, DatePicker, DialogFooter, Drawer, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { FormSkeleton, useToast } from "@/components/bv/common";
import { useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { InvoiceItemsEditor } from "./InvoiceItemsEditor";
import { PartyPicker, PropertySelect } from "./shared";
import {
  INVOICE_TYPES, addDaysISO, computeTotals, dateOnly, draftToItem, errCode, errFields, itemToDraft, money, newItemDraft, pct, todayISO, useBillingSettings, usePropertyChoice,
  type BillingSettings, type Invoice, type ItemDraft,
} from "./types";

const NIL = "00000000-0000-0000-0000-000000000000";
const FALLBACK_SETTINGS: BillingSettings = { property_id: null, inherited: true, tax_enabled: false, tax_name: "PPN", tax_rate: 11, seller_name: null, seller_tax_id: null, seller_address: null, invoice_footer: null, payment_instructions: null, default_due_days: 14, reminder_offsets: [], updated_at: null, version: 0 };

export function InvoiceForm({ invoice, onClose, onSaved }: { invoice?: Invoice | null; onClose: () => void; onSaved?: (inv: Invoice) => void }) {
  const [pid, setPid] = usePropertyChoice(invoice?.property_id);
  const settings = useBillingSettings(pid || null, !!pid);
  const editing = !!invoice;
  const ready = !settings.isLoading || settings.isError || !pid;
  const st = settings.data ?? FALLBACK_SETTINGS;
  return (
    <Drawer open onClose={onClose} width={780} title={editing ? "Edit draft invoice" : "Buat Invoice"} description={editing ? "Nomor invoice diberikan saat diterbitkan. Invoice terbit tidak dapat diedit — koreksi lewat void atau credit note." : "Invoice manual untuk tenant atau unit. Jumlah baris dihitung server (qty × harga, dibulatkan)."}>
      {!ready ? <FormSkeleton fields={6} /> : <InvoiceFormBody key={invoice?.id ?? "new"} invoice={invoice ?? null} pid={pid} setPid={setPid} settings={st} settingsMissing={!settings.data} onClose={onClose} onSaved={onSaved} />}
    </Drawer>
  );
}

/** Mode pajak awal item draft yang diedit: tarif = default → ikut default; 0 → bebas pajak (bila invoice dikenai pajak); selain itu tarif khusus. */
function initialDrafts(inv: Invoice, st: BillingSettings): { items: ItemDraft[]; applyTax: boolean } {
  const taxed = inv.items.filter((it) => (it.tax_rate ?? 0) > 0);
  const applyTax = inv.tax_mode === "manual" ? st.tax_enabled : taxed.length > 0;
  const items = inv.items.map((it) => {
    const d = itemToDraft(it, inv.invoice_type);
    const rate = it.tax_rate ?? 0;
    if (inv.tax_mode === "manual") return d;
    if (rate > 0 && Math.abs(rate - st.tax_rate) < 0.0001) return { ...d, tax_mode: "default" as const };
    if (rate > 0) return { ...d, tax_mode: "custom" as const, tax_rate: String(rate) };
    return { ...d, tax_mode: applyTax ? ("exempt" as const) : ("default" as const) };
  });
  return { items: items.length ? items : [newItemDraft()], applyTax };
}

function InvoiceFormBody({ invoice, pid, setPid, settings: st, settingsMissing, onClose, onSaved }: { invoice: Invoice | null; pid: string; setPid: (v: string) => void; settings: BillingSettings; settingsMissing: boolean; onClose: () => void; onSaved?: (inv: Invoice) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { can } = useAuth();
  const editing = !!invoice;
  const [init] = useState(() => (invoice ? initialDrafts(invoice, st) : null));
  const [f, setF] = useState(() => ({
    tenant_id: invoice?.tenant_id ?? "",
    unit_location_id: invoice?.unit_location_id ?? "",
    invoice_type: invoice?.invoice_type ?? "service_charge",
    period_start: dateOnly(invoice?.period_start),
    period_end: dateOnly(invoice?.period_end),
    description: invoice?.description ?? "",
    due_date: invoice?.due_date ?? addDaysISO(todayISO(), st.default_due_days || 14),
    external_ref: invoice?.external_ref ?? "",
    notes: invoice?.notes ?? "",
    issue_now: false,
  }));
  const [items, setItems] = useState<ItemDraft[]>(() => init?.items ?? [newItemDraft()]);
  // null = ikut pengaturan billing property (tax_enabled)
  const [applyTaxChoice, setApplyTaxChoice] = useState<boolean | null>(() => (init ? init.applyTax : null));
  const applyTax = applyTaxChoice ?? st.tax_enabled;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ message: string; fields: Record<string, string> } | null>(null);
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((s) => ({ ...s, [k]: v }));
  const filled = items.filter((x) => x.description.trim());
  const totals = computeTotals(filled, applyTax, st.tax_rate);
  const canIssue = can("billing.invoices.issue", pid || null);
  const problems: string[] = [];
  if (!pid) problems.push("Pilih property");
  if (!f.tenant_id && !f.unit_location_id) problems.push("Pilih tenant atau unit");
  if (!f.due_date) problems.push("Isi tanggal jatuh tempo");
  if (!filled.length) problems.push("Isi minimal satu item");
  if (f.period_start && f.period_end && f.period_end < f.period_start) problems.push("Periode selesai harus setelah periode mulai");
  if (filled.some((x) => Number(x.unit_price) < 0)) problems.push("Harga satuan tidak boleh negatif");
  if (filled.some((x) => x.tax_mode === "custom" && (x.tax_rate === "" || Number(x.tax_rate) < 0 || Number(x.tax_rate) > 100))) problems.push("Tarif pajak khusus harus 0–100%");
  if (f.issue_now && totals.total <= 0) problems.push("Total harus lebih dari 0 untuk diterbitkan");

  const submit = async () => {
    if (problems.length) return;
    setBusy(true);
    setError(null);
    try {
      const common = {
        invoice_type: f.invoice_type,
        period_start: f.period_start || null,
        period_end: f.period_end || null,
        due_date: f.due_date,
        apply_tax: applyTax,
        items: filled.map(draftToItem),
      };
      let out: Invoice;
      if (invoice) {
        out = await api<Invoice>(`invoices/${invoice.id}`, {
          method: "PATCH",
          ifMatch: invoice.version,
          body: { ...common, tenant_id: f.tenant_id || NIL, unit_location_id: f.unit_location_id || NIL, description: f.description.trim(), external_ref: f.external_ref.trim(), notes: f.notes.trim() },
        });
      } else {
        out = await api<Invoice>("invoices", {
          body: { ...common, property_id: pid, tenant_id: f.tenant_id || null, unit_location_id: f.unit_location_id || null, description: f.description.trim() || null, external_ref: f.external_ref.trim() || null, notes: f.notes.trim() || null, issue_now: f.issue_now && canIssue },
          idempotencyKey: uuid(),
        });
      }
      invalidate("list", "one", "all");
      toast.success(invoice ? `Draft invoice disimpan` : out.status === "draft" ? "Invoice disimpan sebagai draft" : `Invoice ${out.display_number} diterbitkan & dikirim ke tenant`);
      onSaved?.(out);
      onClose();
    } catch (e) {
      const code = errCode(e);
      setError({ message: code === "STALE_VERSION" ? "Invoice telah diubah pengguna lain. Tutup form dan buka ulang untuk memuat versi terbaru." : code === "INVOICE_NOT_DRAFT" ? "Invoice sudah bukan draft — koreksi lewat void atau credit note." : (e as Error).message, fields: errFields(e) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-4">
      {error && <Alert variant="critical" title="Invoice belum tersimpan">{error.message}{Object.keys(error.fields).length > 0 && <ul className="mt-1 list-disc pl-5">{Object.entries(error.fields).map(([k, m]) => <li key={k}><code>{k}</code>: {m}</li>)}</ul>}</Alert>}
      {invoice?.tax_mode === "manual" && <Alert variant="info">Draft ini memakai pajak manual (mode lama, {money(invoice.tax_amount)}). Setelah disimpan, pajak dihitung per item dari pengaturan billing.</Alert>}
      {!editing && <PropertySelect value={pid} onChange={(v) => { setPid(v); setF((s) => ({ ...s, tenant_id: "", unit_location_id: "" })); }} required />}
      {pid && <PartyPicker propertyId={pid} tenantId={f.tenant_id} unitId={f.unit_location_id} onChange={(t, u) => setF((s) => ({ ...s, tenant_id: t, unit_location_id: u }))} />}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label="Jenis invoice" required>
          <NativeSelect value={f.invoice_type} onChange={(e) => set("invoice_type", e.target.value)}>{INVOICE_TYPES.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}</NativeSelect>
        </Field>
        <Field label="Jatuh tempo" required error={error?.fields.due_date} help="Akhir hari tanggal ini di zona waktu property.">
          <DatePicker value={f.due_date} onChange={(v) => set("due_date", v)} />
        </Field>
        <Field label="Periode mulai"><DatePicker value={f.period_start} onChange={(v) => set("period_start", v)} /></Field>
        <Field label="Periode selesai" error={error?.fields.period_end}><DatePicker value={f.period_end} onChange={(v) => set("period_end", v)} /></Field>
        <Field label="Deskripsi" className="sm:col-span-2"><Input value={f.description} onChange={(e) => set("description", e.target.value)} placeholder="mis. Tagihan periode Oktober 2026" /></Field>
      </div>
      <div className="rounded-[var(--radius-md)] bg-surface-container-low px-3 py-2">
        <Checkbox label={`Kenakan ${st.tax_name} ${pct(st.tax_rate)} (default item)`} checked={applyTax} onCheckedChange={(v) => setApplyTaxChoice(v)} />
        <p className="mt-1 text-xs text-on-surface-variant">
          {settingsMissing ? "Pengaturan billing tidak dapat dimuat — tarif default diasumsikan." : applyTaxChoice === null ? `Mengikuti pengaturan billing (${st.tax_enabled ? "pajak aktif" : "pajak nonaktif"}).` : "Diatur manual untuk invoice ini."} Item dapat dibebaskan pajak atau memakai tarif khusus. Tenant bebas pajak dihitung server.
        </p>
      </div>
      <div>
        <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Item</div>
        <InvoiceItemsEditor items={items} onChange={setItems} invoiceType={f.invoice_type} applyTax={applyTax} defaultRate={st.tax_rate} taxName={st.tax_name} error={error?.fields.items} />
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label="Referensi akuntansi (external_ref)" help="Nomor dokumen di sistem akuntansi (P4-INT-03)."><Input value={f.external_ref} onChange={(e) => set("external_ref", e.target.value)} /></Field>
        <Field label="Catatan internal"><Textarea rows={2} className="min-h-[40px]" value={f.notes} onChange={(e) => set("notes", e.target.value)} /></Field>
      </div>
      {!editing && canIssue && <Checkbox label="Terbitkan sekarang (nomor INV diberikan & tenant menerima notifikasi)" checked={f.issue_now} onCheckedChange={(v) => set("issue_now", v)} />}
      {problems.length > 0 && <p className="text-xs text-on-surface-variant">Lengkapi: {problems.join(" · ")}.</p>}
      <DialogFooter className="sticky bottom-0 bg-surface pb-1">
        <span className="mr-auto self-center text-sm text-on-surface-variant">Total <span className="tnum text-h3 font-bold text-on-surface">{money(totals.total)}</span></span>
        <Button variant="secondary" onClick={onClose}>Batal</Button>
        <Button loading={busy} disabled={problems.length > 0} onClick={submit}>{editing ? "Simpan draft" : f.issue_now ? "Simpan & terbitkan" : "Simpan draft"}</Button>
      </DialogFooter>
    </div>
  );
}
