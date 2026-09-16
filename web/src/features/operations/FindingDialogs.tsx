// Dialog pembuatan Finding (FR-FND), Incident (FR-INC), Service Request (FR-SR) — dipakai dari detail & daftar.
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { AssetPicker, LocationPicker, TeamPicker, UserPicker } from "@/components/bv/pickers";
import { useToast } from "@/components/bv/common";
import { useAll, useCreate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import type { Finding, Incident, ServiceRequest, Tenant } from "@/api/types";
import { errMessage } from "./dialogs";

const LEVELS = ["low", "medium", "high", "critical"];
export const FINDING_TYPES = ["general", "patrol", "inspection", "housekeeping", "checklist"];
export const INCIDENT_TYPES = ["security", "safety", "engineering", "housekeeping", "general"];
export const INCIDENT_CATEGORIES = ["theft", "intrusion", "fire", "medical", "vandalism", "suspicious", "accident", "flood", "power_outage", "other"];

function usePropertySelect() {
  const { propertyId, properties } = useAuth();
  const [pid, setPid] = useState(propertyId ?? properties[0]?.id ?? "");
  useEffect(() => setPid(propertyId ?? properties[0]?.id ?? ""), [propertyId, properties]);
  return { pid, setPid, properties };
}

export function CreateFindingDialog({ open, onOpenChange, defaults, onCreated }: { open: boolean; onOpenChange: (o: boolean) => void; defaults?: Partial<{ finding_type: string; title: string; location_id: string | null; asset_id: string | null; source_type: string; source_id: string }>; onCreated?: (f: Finding) => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const { pid, setPid, properties } = usePropertySelect();
  const [form, setForm] = useState({ finding_type: defaults?.finding_type ?? "general", title: defaults?.title ?? "", description: "", severity: "medium", category: "" });
  const [locationId, setLocationId] = useState<string | null>(defaults?.location_id ?? null);
  const [assetId, setAssetId] = useState<string | null>(defaults?.asset_id ?? null);
  const [error, setError] = useState<string | null>(null);
  const create = useCreate<Record<string, unknown>, Finding>("findings");
  useEffect(() => {
    if (open) {
      setForm({ finding_type: FINDING_TYPES.includes(defaults?.finding_type ?? "") ? defaults!.finding_type! : "general", title: defaults?.title ?? "", description: "", severity: "medium", category: "" });
      setLocationId(defaults?.location_id ?? null);
      setAssetId(defaults?.asset_id ?? null);
      setError(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const submit = async () => {
    if (!form.title.trim() || !locationId) return setError("Judul dan lokasi wajib diisi.");
    try {
      const f = await create.mutateAsync({ property_id: pid, finding_type: form.finding_type, category: form.category || null, title: form.title.trim(), description: form.description || null, location_id: locationId, asset_id: assetId, severity: form.severity, source_type: defaults?.source_type ?? null, source_id: defaults?.source_id ?? null });
      toast.success(`${f.finding_number} dicatat`);
      onOpenChange(false);
      onCreated?.(f);
    } catch (e) {
      setError(errMessage(e));
    }
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent side="right" title="Catat Finding">
        <div className="space-y-4">
          {error && <p className="rounded-md bg-critical-soft px-3 py-2 text-sm text-critical-text">{error}</p>}
          {properties.length > 1 && <Field label="Property" required><NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); setLocationId(null); }}>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>}
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("label.type")} required><NativeSelect value={form.finding_type} onChange={(e) => setForm({ ...form, finding_type: e.target.value })}>{FINDING_TYPES.map((x) => <option key={x} value={x}>{x}</option>)}</NativeSelect></Field>
            <Field label={t("label.severity")} required><NativeSelect value={form.severity} onChange={(e) => setForm({ ...form, severity: e.target.value })}>{LEVELS.map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
          </div>
          <Field label={t("label.title")} required><Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} /></Field>
          <Field label={t("label.category")}><Input value={form.category} onChange={(e) => setForm({ ...form, category: e.target.value })} placeholder="mis. kebocoran, kerusakan, kebersihan" /></Field>
          <Field label={t("label.description")}><Textarea rows={3} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} /></Field>
          <Field label={t("label.location")} required><LocationPicker propertyId={pid} value={locationId} onChange={(id) => setLocationId(id)} /></Field>
          <Field label={t("label.asset")}><AssetPicker propertyId={pid} locationId={locationId} value={assetId} onChange={setAssetId} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>{t("action.discard")}</Button>
          <Button onClick={submit} loading={create.isPending}>{t("action.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function CreateIncidentDialog({ open, onOpenChange, defaults, onCreated }: { open: boolean; onOpenChange: (o: boolean) => void; defaults?: Partial<{ incident_type: string; title: string; location_id: string | null; source_type: string; source_id: string }>; onCreated?: (i: Incident) => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const { pid, setPid, properties } = usePropertySelect();
  const [form, setForm] = useState({ incident_type: defaults?.incident_type ?? "security", category: "other", title: defaults?.title ?? "", description: "", severity: "medium", priority: "", occurred_at: "" });
  const [locationId, setLocationId] = useState<string | null>(defaults?.location_id ?? null);
  const [teamId, setTeamId] = useState<string | null>(null);
  const [userId, setUserId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const create = useCreate<Record<string, unknown>, Incident>("incidents");
  useEffect(() => {
    if (open) {
      setForm({ incident_type: defaults?.incident_type ?? "security", category: "other", title: defaults?.title ?? "", description: "", severity: "medium", priority: "", occurred_at: "" });
      setLocationId(defaults?.location_id ?? null);
      setError(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const submit = async () => {
    if (!form.title.trim() || !locationId) return setError("Judul dan lokasi wajib diisi.");
    try {
      const i = await create.mutateAsync({ property_id: pid, incident_type: form.incident_type, category: form.category, title: form.title.trim(), description: form.description || null, location_id: locationId, severity: form.severity, priority: form.priority || form.severity, occurred_at: form.occurred_at ? new Date(form.occurred_at).toISOString() : null, assignee_team_id: teamId, assignee_user_id: userId, source_type: defaults?.source_type ?? null, source_id: defaults?.source_id ?? null });
      toast.success(`${i.incident_number} dilaporkan`);
      onOpenChange(false);
      onCreated?.(i);
    } catch (e) {
      setError(errMessage(e));
    }
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent side="right" title={t("action.report_incident")}>
        <div className="space-y-4">
          {error && <p className="rounded-md bg-critical-soft px-3 py-2 text-sm text-critical-text">{error}</p>}
          {properties.length > 1 && <Field label="Property" required><NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); setLocationId(null); }}>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>}
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("label.type")} required><NativeSelect value={form.incident_type} onChange={(e) => setForm({ ...form, incident_type: e.target.value })}>{INCIDENT_TYPES.map((x) => <option key={x} value={x}>{x}</option>)}</NativeSelect></Field>
            <Field label={t("label.category")} required><NativeSelect value={form.category} onChange={(e) => setForm({ ...form, category: e.target.value })}>{INCIDENT_CATEGORIES.map((x) => <option key={x} value={x}>{x.replace("_", " ")}</option>)}</NativeSelect></Field>
            <Field label={t("label.severity")} required><NativeSelect value={form.severity} onChange={(e) => setForm({ ...form, severity: e.target.value })}>{LEVELS.map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
            <Field label={t("label.priority")}><NativeSelect value={form.priority} onChange={(e) => setForm({ ...form, priority: e.target.value })}><option value="">Ikuti severity</option>{LEVELS.map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
          </div>
          <Field label={t("label.title")} required><Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} /></Field>
          <Field label={t("label.description")}><Textarea rows={3} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} /></Field>
          <Field label={t("label.location")} required><LocationPicker propertyId={pid} value={locationId} onChange={(id) => setLocationId(id)} /></Field>
          <Field label="Waktu kejadian"><Input type="datetime-local" value={form.occurred_at} onChange={(e) => setForm({ ...form, occurred_at: e.target.value })} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("label.team")}><TeamPicker propertyId={pid} value={teamId} onChange={(v) => { setTeamId(v); setUserId(null); }} /></Field>
            <Field label={t("label.assignee")}><UserPicker propertyId={pid} teamId={teamId} value={userId} onChange={setUserId} /></Field>
          </div>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>{t("action.discard")}</Button>
          <Button onClick={submit} loading={create.isPending}>{t("action.report_incident")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export interface SRCategory { id: string; code: string; name: string; default_domain: string | null; default_priority: string; is_active: boolean }
export function useSRCategories() {
  return useAll<SRCategory>("service-request-categories");
}

export function CreateServiceRequestDialog({ open, onOpenChange, defaults, onCreated }: { open: boolean; onOpenChange: (o: boolean) => void; defaults?: Partial<{ tenant_id: string; location_id: string | null; property_id: string; requester_name: string; requester_phone: string; title: string; channel: string }>; onCreated?: (s: ServiceRequest) => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const { pid, setPid, properties } = usePropertySelect();
  const cats = useSRCategories();
  const tenants = useAll<Tenant>("tenants", { property_id: pid || undefined, status: "active" }, { enabled: !!pid });
  const [form, setForm] = useState({ category_code: "", title: defaults?.title ?? "", description: "", priority: "", channel: defaults?.channel ?? "phone", tenant_id: defaults?.tenant_id ?? "", requester_name: defaults?.requester_name ?? "", requester_phone: defaults?.requester_phone ?? "" });
  const [locationId, setLocationId] = useState<string | null>(defaults?.location_id ?? null);
  const [error, setError] = useState<string | null>(null);
  const create = useCreate<Record<string, unknown>, ServiceRequest>("service-requests");
  useEffect(() => {
    if (open) {
      setForm((f) => ({ ...f, title: defaults?.title ?? "", description: "", tenant_id: defaults?.tenant_id ?? "", requester_name: defaults?.requester_name ?? f.requester_name, requester_phone: defaults?.requester_phone ?? f.requester_phone }));
      if (defaults?.property_id) setPid(defaults.property_id);
      setLocationId(defaults?.location_id ?? null);
      setError(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const submit = async () => {
    const code = form.category_code || cats.data?.[0]?.code;
    if (!form.title.trim() || !code) return setError("Judul dan kategori wajib diisi.");
    try {
      const s = await create.mutateAsync({ property_id: pid, category_code: code, title: form.title.trim(), description: form.description || null, tenant_id: form.tenant_id || null, requester_name: form.requester_name || null, requester_phone: form.requester_phone || null, location_id: locationId, priority: form.priority || undefined, channel: form.channel });
      toast.success(`${s.request_number} dibuat`);
      onOpenChange(false);
      onCreated?.(s);
    } catch (e) {
      setError(errMessage(e));
    }
  };
  const tenant = tenants.data?.find((x) => x.id === form.tenant_id);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent side="right" title={t("action.create_service_request")}>
        <div className="space-y-4">
          {error && <p className="rounded-md bg-critical-soft px-3 py-2 text-sm text-critical-text">{error}</p>}
          {properties.length > 1 && <Field label="Property" required><NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); setLocationId(null); }}>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>}
          <Field label={t("label.tenant")}>
            <NativeSelect value={form.tenant_id} onChange={(e) => { const tid = e.target.value; setForm({ ...form, tenant_id: tid }); const tn = tenants.data?.find((x) => x.id === tid); if (tn?.units[0]) setLocationId(tn.units[0].location_id); }}>
              <option value="">— Non-tenant / walk-in —</option>
              {(tenants.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.tenant_code} · {x.name}</option>)}
            </NativeSelect>
          </Field>
          {tenant && tenant.units.length > 1 && <p className="text-xs text-muted-foreground">Unit: {tenant.units.map((u) => u.unit_number).join(", ")}</p>}
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("label.category")} required><NativeSelect value={form.category_code} onChange={(e) => setForm({ ...form, category_code: e.target.value })}>{(cats.data ?? []).map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}</NativeSelect></Field>
            <Field label={t("label.priority")}><NativeSelect value={form.priority} onChange={(e) => setForm({ ...form, priority: e.target.value })}><option value="">Default kategori</option>{LEVELS.map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
          </div>
          <Field label={t("label.title")} required><Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} /></Field>
          <Field label={t("label.description")}><Textarea rows={3} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} /></Field>
          <Field label={t("label.location")}><LocationPicker propertyId={pid} value={locationId} onChange={(id) => setLocationId(id)} /></Field>
          <div className="grid grid-cols-3 gap-3">
            <Field label="Nama pemohon"><Input value={form.requester_name} onChange={(e) => setForm({ ...form, requester_name: e.target.value })} /></Field>
            <Field label="Telepon"><Input value={form.requester_phone} onChange={(e) => setForm({ ...form, requester_phone: e.target.value })} /></Field>
            <Field label="Channel"><NativeSelect value={form.channel} onChange={(e) => setForm({ ...form, channel: e.target.value })}>{["staff", "phone", "walk_in", "email", "whatsapp"].map((c) => <option key={c} value={c}>{c.replace("_", " ")}</option>)}</NativeSelect></Field>
          </div>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>{t("action.discard")}</Button>
          <Button onClick={submit} loading={create.isPending}>{t("action.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
