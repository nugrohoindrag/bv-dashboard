// Property Profile (PRD P1 v1.3 §3, Onboarding Brief §18–§19): profile aktif, capability, konfigurasi OD-P1-004..008,
// override terminologi, dan aksi administratif Ubah Profile (validasi server + guard data tidak kompatibel).
import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Checkbox, Dialog, DialogContent, DialogFooter, Field, Icon, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { AsyncState, KeyValue, useToast } from "@/components/bv/common";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fetchPropertyContext, PROFILE_ICON, PROFILE_LABEL, type ProfileCode, type PropertyContext, type Term } from "@/lib/profile";

const CAP_LABEL: Record<string, string> = {
  housekeeping: "Housekeeping",
  security: "Security",
  engineering: "Engineering",
  tenant_relation: "Tenant Relation",
  tenant_app: "Tenant App",
  facility_booking: "Facility Booking",
  visitor_management: "Visitor Management",
  billing: "Billing",
  vendor_management: "Vendor Management",
  inventory: "Inventory",
  reports: "Reports",
  hotel_booking: "Hotel Booking Management",
  reception: "Reception",
  unit_sales: "Unit Sales Management",
  unit_rental: "Unit Rental Management",
  resident_management: "Resident Management",
  tenant_management: "Tenant Management",
  workplace_services: "Workplace Services",
};
const MANDATORY = new Set(["housekeeping", "security", "engineering", "tenant_relation"]);
const TERM_KEYS: { key: string; label: string }[] = [
  { key: "customer", label: "End customer" },
  { key: "customer_plural", label: "End customer (jamak)" },
  { key: "occupant", label: "Occupying person" },
  { key: "relation_module", label: "Relationship module" },
  { key: "request", label: "Customer request" },
  { key: "stay", label: "Stay / occupancy context" },
  { key: "inventory_unit", label: "Commercial inventory" },
  { key: "my_unit", label: "Label 'My Unit' (Tenant App)" },
  { key: "cleaning_task", label: "Cleaning task" },
];

export default function PropertyProfileSection() {
  const { t } = useTranslation();
  const { propertyId, properties, can, refreshPrincipal } = useAuth();
  const qc = useQueryClient();
  const toast = useToast();
  const ctx = useQuery({ queryKey: ["property-context", propertyId], enabled: !!propertyId, queryFn: ({ signal }) => fetchPropertyContext(propertyId!, signal) });
  const [form, setForm] = useState<Partial<PropertyContext["config"]>>({});
  const [terms, setTerms] = useState<Record<string, Term>>({});
  const [saving, setSaving] = useState(false);
  const [changeOpen, setChangeOpen] = useState(false);
  useEffect(() => {
    if (ctx.data) {
      setForm(ctx.data.config);
      setTerms(ctx.data.config.terminology ?? {});
    }
  }, [ctx.data]);
  const property = properties.find((p) => p.id === propertyId);
  const canUpdate = can("property.properties.update");
  const canChange = can("property.properties.change_profile");

  const save = async () => {
    if (!propertyId) return;
    setSaving(true);
    try {
      const cleanTerms: Record<string, Term> = {};
      for (const [k, v] of Object.entries(terms)) if (v.id || v.en) cleanTerms[k] = v;
      const res = await api<PropertyContext>(`properties/${propertyId}/profile-config`, {
        method: "PATCH",
        body: {
          expose_sla_to_tenant: form.expose_sla_to_tenant,
          tenant_confirmation_required: form.tenant_confirmation_required,
          csat_enabled: form.csat_enabled,
          booking_approval_required: form.booking_approval_required,
          visitor_approval_required: form.visitor_approval_required,
          tenant_self_registration: form.tenant_self_registration,
          auto_close_resolved_hours: Number(form.auto_close_resolved_hours ?? 72),
          terminology: cleanTerms,
        },
        ifMatch: ctx.data?.config.version || undefined,
      });
      qc.setQueryData(["property-context", propertyId], res);
      toast.success("Konfigurasi profile disimpan");
    } catch (e) {
      toast.error(e);
    } finally {
      setSaving(false);
    }
  };

  if (!propertyId) {
    return <Alert variant="info" title="Pilih property">Property Profile melekat pada Property (Onboarding Brief §4). Pilih satu property di header untuk melihat dan mengatur profile-nya.</Alert>;
  }
  return (
    <div className="space-y-5">
      <AsyncState query={ctx}>
        {(c) => (
          <>
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2"><Icon name={PROFILE_ICON[c.profile]} size={20} /> {property?.name ?? c.property_name} · {PROFILE_LABEL[c.profile]}</CardTitle>
                <CardSubtitle>Satu property memiliki tepat satu profile aktif (PS-001/PS-003). Profile menentukan capability, terminologi, kategori, dan workflow default — bukan produk terpisah.</CardSubtitle>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-3 gap-5">
                  <KeyValue items={[{ label: "Profile", value: PROFILE_LABEL[c.profile] }, { label: "Status", value: c.status }, { label: "Kode property", value: property?.code ?? "—" }]} />
                  <div className="col-span-2">
                    <div className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Capability aktif (server-side)</div>
                    <div className="flex flex-wrap gap-1.5">
                      {c.capabilities.map((cap) => (
                        <Badge key={cap} tone={MANDATORY.has(cap) ? "primary" : "neutral"}>{CAP_LABEL[cap] ?? cap}{MANDATORY.has(cap) ? " · wajib" : ""}</Badge>
                      ))}
                    </div>
                    <p className="mt-2 text-xs text-muted-foreground">Housekeeping, Security, Engineering, dan Tenant Relation tidak dapat dihapus oleh profile mana pun (PRD §3.2).</p>
                  </div>
                </div>
                {canChange && (
                  <div className="mt-4 flex items-center gap-3 border-t border-border pt-4">
                    <Button variant="secondary" icon="swap_horiz" onClick={() => setChangeOpen(true)}>Ubah Profile…</Button>
                    <span className="text-xs text-muted-foreground">Aksi administratif (PS-006): server memvalidasi dan memblokir bila ada reservasi/listing aktif yang tidak kompatibel.</span>
                  </div>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader><CardTitle>Aturan Tenant Experience (Open Decisions P1)</CardTitle><CardSubtitle>Berlaku untuk Tenant App dan alur Service Request pada property ini.</CardSubtitle></CardHeader>
              <CardContent>
                <div className="grid grid-cols-2 gap-x-8 gap-y-3 text-sm">
                  <Checkbox label="Tampilkan estimasi SLA/due ke tenant (OD-P1-004)" checked={!!form.expose_sla_to_tenant} onCheckedChange={(v) => setForm({ ...form, expose_sla_to_tenant: v })} disabled={!canUpdate} />
                  <Checkbox label="Tenant wajib konfirmasi sebelum ticket Closed (OD-P1-005)" checked={!!form.tenant_confirmation_required} onCheckedChange={(v) => setForm({ ...form, tenant_confirmation_required: v })} disabled={!canUpdate} />
                  <Checkbox label="Aktifkan Feedback / CSAT setelah Closed (OD-P1-006)" checked={!!form.csat_enabled} onCheckedChange={(v) => setForm({ ...form, csat_enabled: v })} disabled={!canUpdate} />
                  <Checkbox label="Facility Booking membutuhkan approval (OD-P1-007)" checked={!!form.booking_approval_required} onCheckedChange={(v) => setForm({ ...form, booking_approval_required: v })} disabled={!canUpdate} />
                  <Checkbox label="Visitor membutuhkan approval Security (OD-P1-008)" checked={!!form.visitor_approval_required} onCheckedChange={(v) => setForm({ ...form, visitor_approval_required: v })} disabled={!canUpdate} />
                  <Checkbox label="Izinkan tenant mendaftar sendiri dari Tenant App (validasi oleh Tenant Relation)" checked={!!form.tenant_self_registration} onCheckedChange={(v) => setForm({ ...form, tenant_self_registration: v })} disabled={!canUpdate} />
                  <Field label="Auto-close ticket Resolved tanpa respons tenant (jam, 0 = nonaktif)"><Input type="number" min={0} max={720} value={form.auto_close_resolved_hours ?? 72} onChange={(e) => setForm({ ...form, auto_close_resolved_hours: Number(e.target.value) })} disabled={!canUpdate} /></Field>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader><CardTitle>Terminologi (NC v2.0 §7)</CardTitle><CardSubtitle>Label presentasi per profile. Canonical system entity (Service Request, Work Order, Unit, Tenant) tidak berubah. Kosongkan untuk memakai default profile.</CardSubtitle></CardHeader>
              <CardContent>
                <table className="w-full text-sm">
                  <thead><tr className="text-left text-xs uppercase text-on-surface-variant"><th className="py-1 pr-3">Konsep</th><th className="py-1 pr-3">Default {PROFILE_LABEL[c.profile]}</th><th className="py-1 pr-3">Override (ID)</th><th className="py-1">Override (EN)</th></tr></thead>
                  <tbody>
                    {TERM_KEYS.map((k) => (
                      <tr key={k.key} className="border-t border-border">
                        <td className="py-1.5 pr-3 font-medium">{k.label}</td>
                        <td className="py-1.5 pr-3 text-muted-foreground">{c.terminology[k.key]?.id} / {c.terminology[k.key]?.en}</td>
                        <td className="py-1.5 pr-3"><Input value={terms[k.key]?.id ?? ""} onChange={(e) => setTerms({ ...terms, [k.key]: { id: e.target.value, en: terms[k.key]?.en ?? "" } })} disabled={!canUpdate} /></td>
                        <td className="py-1.5"><Input value={terms[k.key]?.en ?? ""} onChange={(e) => setTerms({ ...terms, [k.key]: { id: terms[k.key]?.id ?? "", en: e.target.value } })} disabled={!canUpdate} /></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {canUpdate && <div className="mt-4"><Button loading={saving} onClick={save}>{t("action.save")}</Button></div>}
              </CardContent>
            </Card>

            {changeOpen && <ChangeProfileDialog ctx={c} onClose={() => setChangeOpen(false)} onChanged={async (res) => { qc.setQueryData(["property-context", propertyId], res); qc.invalidateQueries({ queryKey: ["property-context"] }); await refreshPrincipal(); }} />}
          </>
        )}
      </AsyncState>
    </div>
  );
}

function ChangeProfileDialog({ ctx, onClose, onChanged }: { ctx: PropertyContext; onClose: () => void; onChanged: (res: PropertyContext) => Promise<void> | void }) {
  const toast = useToast();
  const [profile, setProfile] = useState<ProfileCode>(ctx.profile);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const options = useMemo(() => (Object.keys(PROFILE_LABEL) as ProfileCode[]).filter((p) => p !== ctx.profile), [ctx.profile]);
  const submit = async () => {
    setBusy(true);
    try {
      const res = await api<PropertyContext>(`properties/${ctx.property_id}/profile`, { body: { profile, reason } });
      await onChanged(res);
      toast.success(`Profile diubah menjadi ${PROFILE_LABEL[profile]}`);
      onClose();
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Ubah Property Profile" description="Perubahan profile mempengaruhi terminologi, workflow, status, checklist, dashboard, konfigurasi role, dan objek komersial. Server akan memblokir bila ada data yang tidak kompatibel (Onboarding Brief §18–§19).">
        <div className="space-y-4">
          <Alert variant="warning" title="Impact review">Tinjau dampak sebelum melanjutkan: reservasi hotel / listing & rental apartment yang masih aktif membuat perubahan ditolak. Perubahan tercatat di Audit Log.</Alert>
          <Field label="Profile baru" required>
            <NativeSelect value={profile} onChange={(e) => setProfile(e.target.value as ProfileCode)}>
              <option value={ctx.profile} disabled>{PROFILE_LABEL[ctx.profile]} (saat ini)</option>
              {options.map((p) => <option key={p} value={p}>{PROFILE_LABEL[p]}</option>)}
            </NativeSelect>
          </Field>
          <Field label="Alasan (wajib, masuk audit trail)" required><Textarea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} disabled={profile === ctx.profile || !reason.trim()} onClick={submit}>Konfirmasi perubahan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
