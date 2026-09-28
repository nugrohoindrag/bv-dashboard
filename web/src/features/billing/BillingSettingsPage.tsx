// Billing › Pengaturan Billing (PRD P4 v2.1 P4-INV-03, P4-VRF-04, P4-COL-02; D-P4-02; B-15): default organization dengan
// override per property — pajak (PPN), identitas penjual & NPWP pada PDF, footer invoice, instruksi pembayaran, jatuh tempo
// default, jadwal pengingat bertahap; rekening bank untuk instruksi transfer & rekonsiliasi. Dikelola Finance
// (billing.settings.manage — default organization butuh grant tingkat organization), bukan izin organisasi platform.
import { useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { AsyncState, FormSkeleton, useToast } from "@/components/bv/common";
import { useAll, useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import { hasOrgGrant, pct, usePropertyName, type BankAccount, type BillingSettings } from "./types";

/** "−3, 1, 7" → [-3, 1, 7]; null bila ada nilai tidak valid (server: bulat −30..180). */
function parseOffsets(s: string): number[] | null {
  const parts = s.replace(/−/g, "-").split(/[\s,;]+/).filter(Boolean);
  const out: number[] = [];
  for (const p of parts) {
    const n = Number(p.replace(/^h/i, ""));
    if (!Number.isInteger(n) || n < -30 || n > 180) return null;
    out.push(n);
  }
  return [...new Set(out)].sort((a, b) => a - b);
}
const offsetLabel = (n: number) => (n < 0 ? `H${n}` : n === 0 ? "Hari H" : `H+${n}`);

export default function BillingSettingsPage() {
  const { t } = useTranslation();
  const { propertyId, properties, principal, can } = useAuth();
  const [scope, setScope] = useState<string>(propertyId ?? "");
  const orgManage = hasOrgGrant(principal, "billing.settings.manage");
  const canManage = scope ? can("billing.settings.manage", scope) : orgManage;
  const settings = useQuery({
    queryKey: ["billing-settings", scope || "org"],
    queryFn: ({ signal }) => api<BillingSettings>("billing/settings", { query: { property_id: scope || undefined }, signal }),
  });
  return (
    <div>
      <PageHeader
        title={t("nav.billing_settings")}
        subtitle="Pajak, identitas penjual, instruksi pembayaran, jatuh tempo, jadwal pengingat, dan rekening bank. Default organization berlaku untuk semua property kecuali yang punya pengaturan sendiri."
        actions={
          <span className="inline-flex w-full items-center gap-2 sm:w-72">
            <Icon name="domain" size={18} className="shrink-0 text-on-surface-variant" />
            <NativeSelect value={scope} onChange={(e) => setScope(e.target.value)} aria-label="Cakupan pengaturan">
              <option value="">Default organization</option>
              {properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </NativeSelect>
          </span>
        }
      />
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
        <div className="min-w-0 lg:col-span-7">
          <AsyncState query={settings} skeleton={<FormSkeleton fields={8} />}>
            {(st) => <SettingsForm key={`${scope}:${st.version}:${st.updated_at ?? ""}:${st.inherited}`} scope={scope} st={st} canManage={canManage} />}
          </AsyncState>
        </div>
        <div className="min-w-0 space-y-5 lg:col-span-5">
          <BankAccounts scope={scope} orgManage={orgManage} />
          <Card>
            <CardHeader><div><CardTitle>Provider pembayaran</CardTitle><CardSubtitle>Transfer/tunai diverifikasi staf aktif. Pembayaran online (Midtrans/Xendit) sedang ditunda.</CardSubtitle></div></CardHeader>
            <CardContent>
              {can("billing.payments.view") ? <Link to="/settings/payment-providers" className="inline-flex items-center gap-1 text-sm font-semibold text-primary hover:underline">Buka Payment Providers <Icon name="arrow_forward" size={16} /></Link> : <p className="text-sm text-on-surface-variant">Perlu billing.payments.view.</p>}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

function SettingsForm({ scope, st, canManage }: { scope: string; st: BillingSettings; canManage: boolean }) {
  const toast = useToast();
  const qc = useQueryClient();
  const propertyName = usePropertyName();
  const [f, setF] = useState(() => ({
    tax_enabled: st.tax_enabled,
    tax_name: st.tax_name,
    tax_rate: String(st.tax_rate),
    seller_name: st.seller_name ?? "",
    seller_tax_id: st.seller_tax_id ?? "",
    seller_address: st.seller_address ?? "",
    invoice_footer: st.invoice_footer ?? "",
    payment_instructions: st.payment_instructions ?? "",
    default_due_days: String(st.default_due_days),
    reminder_offsets: (st.reminder_offsets ?? []).join(", "),
  }));
  const [busy, setBusy] = useState(false);
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((s) => ({ ...s, [k]: v }));
  const offsets = parseOffsets(f.reminder_offsets);
  const rate = Number(f.tax_rate);
  const due = Number(f.default_due_days);
  const problems: string[] = [];
  if (!(rate >= 0 && rate <= 100) || f.tax_rate === "") problems.push("Tarif pajak 0–100%");
  if (!(Number.isInteger(due) && due >= 0 && due <= 120)) problems.push("Jatuh tempo default 0–120 hari");
  if (!offsets) problems.push("Jadwal pengingat: bilangan bulat −30..180");
  if (!f.tax_name.trim()) problems.push("Nama pajak wajib");

  const save = async () => {
    if (problems.length || !offsets) return;
    setBusy(true);
    try {
      const out = await api<BillingSettings>("billing/settings", {
        method: "PUT",
        query: { property_id: scope || undefined },
        body: {
          tax_enabled: f.tax_enabled, tax_name: f.tax_name.trim(), tax_rate: rate, seller_name: f.seller_name, seller_tax_id: f.seller_tax_id, seller_address: f.seller_address,
          invoice_footer: f.invoice_footer, payment_instructions: f.payment_instructions, default_due_days: due, reminder_offsets: offsets,
        },
      });
      qc.setQueryData(["billing-settings", scope || "org"], out);
      qc.invalidateQueries({ queryKey: ["billing-settings"] });
      toast.action("saved", scope ? `Pengaturan billing ${propertyName(scope)}` : "Pengaturan billing default organization");
    } catch (e) {
      toast.failed("saved", e, "Pengaturan billing");
    } finally {
      setBusy(false);
    }
  };

  const dis = !canManage;
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>{scope ? propertyName(scope) : "Default organization"}</CardTitle>
          <CardSubtitle>{st.updated_at && !st.inherited ? `Diperbarui ${fmtDateTime(st.updated_at)}` : scope ? "Belum ada pengaturan khusus property" : "Nilai default sistem"}</CardSubtitle>
        </div>
        {scope && (st.inherited ? <Badge tone="neutral">Mengikuti default organization</Badge> : <Badge tone="info">Pengaturan khusus property</Badge>)}
      </CardHeader>
      <CardContent className="space-y-5">
        {!canManage && <Alert variant="info">Anda hanya dapat melihat pengaturan ini. Mengubah memerlukan billing.settings.manage{scope ? " pada property ini" : " tingkat organization"}.</Alert>}
        {scope && st.inherited && canManage && <Alert variant="info">Property ini memakai default organization. Menyimpan form ini membuat pengaturan khusus property (override).</Alert>}

        <section className="space-y-3">
          <h3 className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Pajak (P4-INV-03)</h3>
          <Checkbox label="Kenakan pajak secara default pada invoice manual & billing run" checked={f.tax_enabled} onCheckedChange={(v) => set("tax_enabled", v)} disabled={dis} />
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Nama pajak" required><Input value={f.tax_name} onChange={(e) => set("tax_name", e.target.value)} disabled={dis} /></Field>
            <Field label="Tarif (%)" required help={`Pajak per item = jumlah × ${pct(rate || 0)}, dibulatkan ke rupiah.`}><Input type="number" min={0} max={100} step="0.01" value={f.tax_rate} onChange={(e) => set("tax_rate", e.target.value)} disabled={dis} /></Field>
          </div>
        </section>

        <section className="space-y-3">
          <h3 className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Identitas penjual (PDF invoice & kwitansi)</h3>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Nama penjual / badan pengelola"><Input value={f.seller_name} onChange={(e) => set("seller_name", e.target.value)} disabled={dis} /></Field>
            <Field label="NPWP"><Input value={f.seller_tax_id} onChange={(e) => set("seller_tax_id", e.target.value)} disabled={dis} placeholder="00.000.000.0-000.000" /></Field>
          </div>
          <Field label="Alamat"><Textarea rows={2} value={f.seller_address} onChange={(e) => set("seller_address", e.target.value)} disabled={dis} /></Field>
        </section>

        <section className="space-y-3">
          <h3 className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Invoice & pembayaran</h3>
          <Field label="Jatuh tempo default (hari setelah terbit)"><Input type="number" min={0} max={120} value={f.default_due_days} onChange={(e) => set("default_due_days", e.target.value)} disabled={dis} className="sm:w-40" /></Field>
          <Field label="Instruksi pembayaran" help="Tampil di PDF invoice & Tenant App. Rekening aktif di samping ditambahkan otomatis."><Textarea rows={3} value={f.payment_instructions} onChange={(e) => set("payment_instructions", e.target.value)} disabled={dis} /></Field>
          <Field label="Footer invoice"><Textarea rows={2} value={f.invoice_footer} onChange={(e) => set("invoice_footer", e.target.value)} disabled={dis} /></Field>
        </section>

        <section className="space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Jadwal pengingat penagihan (P4-COL-02)</h3>
          <Field label="Hari relatif terhadap jatuh tempo" help="Negatif = sebelum jatuh tempo, positif = setelah. Mis. −3, 1, 7, 14, 30. Dikirim in-app & push (email di-hold)." error={offsets ? undefined : "Isi bilangan bulat −30..180 dipisah koma"}>
            <Input value={f.reminder_offsets} onChange={(e) => set("reminder_offsets", e.target.value)} disabled={dis} placeholder="-3, 1, 7, 14, 30" />
          </Field>
          {offsets && offsets.length > 0 && <div className="flex flex-wrap gap-1.5">{offsets.map((n) => <Badge key={n} tone={n < 0 ? "info" : n === 0 ? "warning" : "error"}>{offsetLabel(n)}</Badge>)}</div>}
          {offsets && offsets.length === 0 && <p className="text-xs text-on-surface-variant">Tanpa pengingat bertahap.</p>}
        </section>

        {canManage && (
          <div className="flex flex-wrap items-center justify-end gap-3 border-t border-border pt-4">
            {problems.length > 0 && <span className="mr-auto text-xs text-on-surface-variant">Periksa: {problems.join(" · ")}</span>}
            <Button loading={busy} disabled={problems.length > 0} onClick={save}>Simpan pengaturan</Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function BankAccounts({ scope, orgManage }: { scope: string; orgManage: boolean }) {
  const { can } = useAuth();
  const list = useAll<BankAccount>("billing/bank-accounts", { property_id: scope || undefined });
  const [edit, setEdit] = useState<BankAccount | "new" | null>(null);
  const rows = (list.data ?? []).filter((b) => (scope ? true : b.property_id === null));
  const canEdit = (b: BankAccount | null) => (b ? (b.property_id ? can("billing.settings.manage", b.property_id) : orgManage) : scope ? can("billing.settings.manage", scope) : orgManage);
  return (
    <Card>
      <CardHeader>
        <div><CardTitle>Rekening bank</CardTitle><CardSubtitle>Instruksi transfer ke tenant & rekonsiliasi mutasi (P4-VRF-04, P4-REC-01).</CardSubtitle></div>
        {canEdit(null) && <Button size="sm" icon="add" onClick={() => setEdit("new")}>Tambah</Button>}
      </CardHeader>
      <CardContent>
        <AsyncState query={{ isLoading: list.isLoading, isError: list.isError, error: list.error, refetch: list.refetch, data: rows }} empty={{ icon: "account_balance", title: "Belum ada rekening", description: scope ? "Tanpa rekening property, rekening organization dipakai." : "Tambahkan rekening penerimaan organization." }}>
          {(items) => (
            <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
              {items.map((b) => (
                <li key={b.id} className="flex items-start justify-between gap-3 px-3 py-2.5">
                  <div className="min-w-0 text-sm">
                    <div className="flex flex-wrap items-center gap-1.5 font-semibold">{b.bank_name}{b.is_default && <Badge tone="primary">Utama</Badge>}{!b.is_active && <Badge tone="neutral">Nonaktif</Badge>}{scope && !b.property_id && <Badge>Organization</Badge>}</div>
                    <div className="font-mono">{b.account_number}</div>
                    <div className="text-xs text-on-surface-variant">a.n. {b.account_name}{b.branch ? ` · ${b.branch}` : ""}</div>
                  </div>
                  {canEdit(b) && <Button variant="ghost" size="sm" icon="edit" onClick={() => setEdit(b)}>Edit</Button>}
                </li>
              ))}
            </ul>
          )}
        </AsyncState>
      </CardContent>
      {edit && <BankAccountDialog account={edit === "new" ? null : edit} scope={scope} onClose={() => setEdit(null)} />}
    </Card>
  );
}

function BankAccountDialog({ account, scope, onClose }: { account: BankAccount | null; scope: string; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [f, setF] = useState(() => ({ bank_name: account?.bank_name ?? "", account_number: account?.account_number ?? "", account_name: account?.account_name ?? "", branch: account?.branch ?? "", notes: account?.notes ?? "", is_default: account?.is_default ?? false, is_active: account?.is_active ?? true }));
  const [busy, setBusy] = useState(false);
  const valid = !!f.bank_name.trim() && !!f.account_number.trim() && !!f.account_name.trim();
  const submit = async () => {
    setBusy(true);
    try {
      const body = { bank_name: f.bank_name.trim(), account_number: f.account_number.trim(), account_name: f.account_name.trim(), branch: f.branch.trim(), notes: f.notes.trim(), is_default: f.is_default, is_active: f.is_active };
      if (account) await api(`billing/bank-accounts/${account.id}`, { method: "PATCH", body });
      else await api("billing/bank-accounts", { body: { ...body, property_id: scope || null }, idempotencyKey: uuid() });
      invalidate("all", "list");
      toast.action(account ? "saved" : "created", `Rekening ${body.bank_name} ${body.account_number}`);
      onClose();
    } catch (e) {
      toast.failed(account ? "saved" : "created", e, "Rekening");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={account ? `Edit rekening · ${account.bank_name}` : "Tambah rekening"} description={scope || account?.property_id ? "Rekening property — diutamakan pada instruksi transfer property ini." : "Rekening organization — dipakai property yang tidak punya rekening sendiri."}>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Bank" required><Input value={f.bank_name} onChange={(e) => setF({ ...f, bank_name: e.target.value })} placeholder="BCA" /></Field>
          <Field label="Nomor rekening" required><Input value={f.account_number} onChange={(e) => setF({ ...f, account_number: e.target.value })} inputMode="numeric" /></Field>
          <Field label="Atas nama" required className="sm:col-span-2"><Input value={f.account_name} onChange={(e) => setF({ ...f, account_name: e.target.value })} /></Field>
          <Field label="Cabang"><Input value={f.branch} onChange={(e) => setF({ ...f, branch: e.target.value })} /></Field>
          <Field label="Catatan"><Input value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
        </div>
        <div className="mt-3 flex flex-wrap gap-4">
          <Checkbox label="Rekening utama" checked={f.is_default} onCheckedChange={(v) => setF({ ...f, is_default: v })} />
          <Checkbox label="Aktif" checked={f.is_active} onCheckedChange={(v) => setF({ ...f, is_active: v })} />
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} disabled={!valid} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
