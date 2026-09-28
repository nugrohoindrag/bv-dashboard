// Tagih biaya ke tenant (PRD P4 v2.1 P4-INV-09, exit gate §29): biaya Work Order / Service Request (suku cadang + jasa = biaya
// aktual) dibebankan ke tenant sebagai invoice "Biaya Tambahan" yang tertaut ke sumbernya. Saran item & pihak dari server
// (GET /billing/charges/draft), dapat diubah; satu tagihan aktif per sumber kecuali "tagih ulang" (409 ALREADY_CHARGED).
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, Checkbox, DatePicker, DialogFooter, Drawer, Field, Input, Textarea } from "@/components/ui/primitives";
import { FormSkeleton, QueryErrorState, useToast } from "@/components/bv/common";
import { useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { InvoiceItemsEditor } from "./InvoiceItemsEditor";
import { PartyPicker, SummaryTile } from "./shared";
import { addDaysISO, computeTotals, draftToItem, errCode, errMeta, itemToDraft, money, newItemDraft, todayISO, useBillingSettings, type ChargeDraft, type Invoice, type ItemDraft } from "./types";

export function ChargeDialog({ sourceType, sourceId, onClose, onCharged }: { sourceType: "work_order" | "service_request"; sourceId: string; onClose: () => void; onCharged?: (inv: Invoice) => void }) {
  const draft = useQuery({
    queryKey: ["billing-charge-draft", sourceType, sourceId],
    queryFn: ({ signal }) => api<ChargeDraft>("billing/charges/draft", { query: { source_type: sourceType, source_id: sourceId }, signal }),
    staleTime: 0,
  });
  const settings = useBillingSettings(draft.data?.property_id ?? null, !!draft.data);
  const ready = !!draft.data && (!settings.isLoading || settings.isError);
  return (
    <Drawer open onClose={onClose} width={780} title="Tagih biaya ke tenant" description="Biaya pekerjaan dibebankan ke tenant sebagai invoice Biaya Tambahan yang tertaut ke sumbernya.">
      {draft.isError ? <QueryErrorState error={draft.error} onRetry={() => draft.refetch()} /> : !ready || !draft.data ? <FormSkeleton fields={6} /> : (
        <ChargeForm d={draft.data} taxEnabled={settings.data?.tax_enabled ?? false} taxRate={settings.data?.tax_rate ?? 11} taxName={settings.data?.tax_name ?? "PPN"} dueDays={settings.data?.default_due_days ?? 14} onClose={onClose} onCharged={onCharged} />
      )}
    </Drawer>
  );
}

function ChargeForm({ d, taxEnabled, taxRate, taxName, dueDays, onClose, onCharged }: { d: ChargeDraft; taxEnabled: boolean; taxRate: number; taxName: string; dueDays: number; onClose: () => void; onCharged?: (inv: Invoice) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { can } = useAuth();
  const [party, setParty] = useState({ tenant: d.tenant_id ?? "", unit: d.unit_location_id ?? "" });
  // sumber WO/SR bukan sumber tepercaya: server menghitung jumlah = qty × harga (jumlah saran tidak dipakai)
  const [items, setItems] = useState<ItemDraft[]>(() => (d.items.length ? d.items.map((it) => ({ ...itemToDraft(it, "additional_charge"), orig: null })) : [newItemDraft()]));
  const [f, setF] = useState(() => ({ description: `Biaya tambahan ${d.source_number} — ${d.source_title}`, due_date: addDaysISO(todayISO(), dueDays), notes: "", issue_now: false }));
  const [allowMultiple, setAllowMultiple] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ code?: string; message: string; numbers: string[] } | null>(null);
  const filled = items.filter((x) => x.description.trim());
  const totals = computeTotals(filled, taxEnabled, taxRate);
  const existing = error?.code === "ALREADY_CHARGED" && error.numbers.length ? error.numbers : d.existing_invoice_numbers;
  const canIssue = can("billing.invoices.issue", d.property_id);
  // API: tenant/unit null = saran server — tenant saran tidak dapat dikosongkan; unit kosong → unit lokasi pekerjaan tetap dipakai
  const tenantCleared = !!d.tenant_id && !party.tenant;
  const unitFallback = !!d.unit_location_id && !party.unit;
  const problems: string[] = [];
  if (!party.tenant && !party.unit && !unitFallback) problems.push("Pilih tenant atau unit");
  if (tenantCleared) problems.push("Tenant saran tidak dapat dikosongkan — pilih tenant lain");
  if (!filled.length) problems.push("Isi minimal satu item");
  if (!f.due_date) problems.push("Isi jatuh tempo");
  if (existing.length > 0 && !allowMultiple) problems.push("Centang tagih ulang atau batalkan invoice lama");
  if (f.issue_now && totals.total <= 0) problems.push("Total harus > 0 untuk diterbitkan");

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const inv = await api<Invoice>("billing/charges", {
        body: {
          source_type: d.source_type,
          source_id: d.source_id,
          tenant_id: party.tenant || null,
          unit_location_id: party.unit || null,
          items: filled.map(draftToItem),
          description: f.description.trim() || null,
          due_date: f.due_date,
          issue_now: f.issue_now && canIssue,
          allow_multiple: allowMultiple,
          notes: f.notes.trim() || null,
        },
        idempotencyKey: uuid(),
      });
      invalidate("list", "one", "all", "billing-charge-draft");
      toast.success(`Tagihan ${inv.display_number === "Draft" ? "draft" : inv.display_number} ${money(inv.total_amount)} dibuat untuk ${inv.tenant_name ?? inv.unit_label ?? "tenant"}`, { to: `/billing/invoices/${inv.id}`, label: "Buka invoice" });
      onCharged?.(inv);
      onClose();
    } catch (e) {
      const meta = errMeta(e);
      const numbers = Array.isArray(meta.invoice_numbers) ? (meta.invoice_numbers as unknown[]).map(String) : [];
      setError({ code: errCode(e), message: (e as Error).message, numbers });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
        <SummaryTile label={d.source_type === "work_order" ? "Work Order" : "Service Request"} value={<span className="font-mono text-body">{d.source_number}</span>} sub={d.source_title} />
        <SummaryTile label="Biaya aktual" value={d.actual_cost_amount !== null ? money(d.actual_cost_amount) : "—"} sub="suku cadang + jasa + lain" />
        <SummaryTile label="Total tagihan" value={money(totals.total)} tone="primary" sub={`${taxName} ${taxEnabled ? "mengikuti pengaturan" : "tidak dikenakan"}`} />
      </div>
      {error && error.code !== "ALREADY_CHARGED" && <Alert variant="critical" title="Tagihan belum dibuat">{error.message}</Alert>}
      {existing.length > 0 && (
        <Alert variant="warning" title="Sumber ini sudah ditagihkan">
          Invoice aktif: <b className="font-mono">{existing.join(", ")}</b>. Batalkan/void invoice lama bila salah, atau buat tagihan tambahan.
          <div className="mt-2"><Checkbox label="Tagih ulang (buat invoice tambahan untuk sumber ini)" checked={allowMultiple} onCheckedChange={setAllowMultiple} /></div>
        </Alert>
      )}
      {d.items.length === 0 && <Alert variant="info">Belum ada biaya tercatat pada sumber ini (suku cadang / biaya jasa) — isi item tagihan secara manual.</Alert>}
      <PartyPicker propertyId={d.property_id} tenantId={party.tenant} unitId={party.unit} onChange={(t, u) => setParty({ tenant: t, unit: u })} />
      {unitFallback && <p className="text-xs text-on-surface-variant">Unit lokasi pekerjaan ({d.unit_label ?? "unit"}) tetap dicantumkan pada invoice.</p>}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Field label="Deskripsi invoice" className="sm:col-span-2"><Input value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
        <Field label="Jatuh tempo" required><DatePicker value={f.due_date} onChange={(v) => setF({ ...f, due_date: v })} /></Field>
      </div>
      <div>
        <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Item tagihan</div>
        <InvoiceItemsEditor items={items} onChange={setItems} invoiceType="additional_charge" applyTax={taxEnabled} defaultRate={taxRate} taxName={taxName} />
      </div>
      <Field label="Catatan internal"><Textarea rows={2} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
      {canIssue && <Checkbox label="Terbitkan sekarang (nomor INV diberikan & tenant menerima notifikasi)" checked={f.issue_now} onCheckedChange={(v) => setF({ ...f, issue_now: v })} />}
      {problems.length > 0 && <p className="text-xs text-on-surface-variant">Lengkapi: {problems.join(" · ")}.</p>}
      <DialogFooter className="sticky bottom-0 bg-surface pb-1">
        <Button variant="secondary" onClick={onClose}>Batal</Button>
        <Button icon="request_quote" loading={busy} disabled={problems.length > 0} onClick={submit}>{f.issue_now ? "Buat & terbitkan" : "Buat draft invoice"}</Button>
      </DialogFooter>
    </div>
  );
}
