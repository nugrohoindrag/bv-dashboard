// Organization (PRD P0 v2 §6.2): profil lengkap organisasi (PATCH /organizations/me + If-Match version), kode/slug/status
// hanya-baca; public intake per property (link form publik + key).
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Alert, Button, Card, CardContent, CardHeader, CardTitle } from "@/components/ui/primitives";
import { AsyncState, KeyValue, useToast } from "@/components/bv/common";
import { FormSkeleton } from "@/components/bv/states";
import { StatusBadge } from "@/components/bv/badges";
import { useAction, useInvalidate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { Organization } from "@/api/types";
import { emptyOrgForm, orgFormFrom, orgFormToBody, validateOrgForm, type OrgForm } from "./organization-form";
import { OrganizationProfileForm } from "./OrganizationProfileForm";

export default function OrganizationSection() {
  const { t } = useTranslation();
  const { properties, can, refreshPrincipal } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const canEdit = can("platform.organizations.update");
  const org = useQuery({ queryKey: ["org"], queryFn: () => api<Organization>("organizations/me") });
  const [form, setForm] = useState<OrgForm>(emptyOrgForm());
  const [touched, setTouched] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => { if (org.data) setForm(orgFormFrom(org.data)); }, [org.data]);
  const [saving, setSaving] = useState(false);
  const intake = useAction<{ property_id: string; enabled: boolean }, { intake_key: string | null; enabled: boolean }>(() => "service-requests/public-intake");
  const [keys, setKeys] = useState<Record<string, string | null>>({});
  const save = async () => {
    setTouched(true);
    setErr(null);
    if (Object.keys(validateOrgForm(form)).length) return;
    setSaving(true);
    try {
      await api("organizations/me", { method: "PATCH", body: orgFormToBody(form), ifMatch: org.data?.version });
      invalidate("org");
      await org.refetch();
      await refreshPrincipal();
      toast.action("saved", t("nav.organization"));
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <div className="space-y-5">
      <Card>
        <CardHeader><CardTitle>{t("org.profile")}</CardTitle></CardHeader>
        <CardContent>
          <AsyncState query={org} skeleton={<FormSkeleton fields={6} />}>
            {(o) => (
              <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
                <div className="min-w-0 space-y-4 lg:col-span-8">
                  {err && <Alert variant="critical">{err}</Alert>}
                  <OrganizationProfileForm value={form} onChange={setForm} disabled={!canEdit} showErrors={touched} />
                  {canEdit && <Button loading={saving} onClick={save}>{t("action.save")}</Button>}
                </div>
                <div className="min-w-0 lg:col-span-4">
                  <KeyValue items={[
                    { label: t("org.code"), value: <span className="font-mono">{o.code}</span> },
                    { label: "Slug", value: <span className="font-mono">{o.slug}</span> },
                    { label: t("label.status"), value: <StatusBadge objectType="organization" status={o.status} /> },
                    { label: "Plan", value: o.plan_code ?? "—" },
                    { label: t("org.version"), value: o.version },
                  ]} />
                </div>
              </div>
            )}
          </AsyncState>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Public Intake (form permintaan tenant tanpa login)</CardTitle></CardHeader>
        <CardContent>
          <p className="mb-3 text-sm text-muted-foreground">Aktifkan per property untuk mendapatkan tautan form publik <code className="rounded bg-muted px-1">/public/v1/service-requests?key=…</code>. Feature flag <code className="rounded bg-muted px-1">public_intake</code> harus aktif.</p>
          <ul className="divide-y divide-border rounded-md border border-border">
            {properties.map((p) => {
              const key = keys[p.id] ?? (p.details?.public_intake_enabled ? "(aktif)" : null);
              return (
                <li key={p.id} className="flex flex-wrap items-center justify-between gap-3 px-3 py-2 text-sm">
                  <span><span className="font-medium">{p.name}</span> <span className="font-mono text-xs text-muted-foreground">{p.code}</span></span>
                  <span className="flex flex-wrap items-center gap-2">
                    {key && <code className="max-w-full truncate sm:max-w-[360px] rounded bg-muted px-1.5 py-0.5 text-xs">{key.startsWith("(") ? key : `${window.location.origin}/public/intake?key=${key}`}</code>}
                    {can("platform.organizations.update") && (
                      <>
                        <Button size="sm" variant="secondary" loading={intake.isPending} onClick={() => intake.mutateAsync({ property_id: p.id, enabled: true }).then((r) => { setKeys((k) => ({ ...k, [p.id]: r.intake_key })); toast.action("published", "Public intake"); }).catch(toast.error)}>Aktifkan / rotate key</Button>
                        <Button size="sm" variant="ghost" onClick={() => intake.mutateAsync({ property_id: p.id, enabled: false }).then(() => { setKeys((k) => ({ ...k, [p.id]: null })); toast.action("archived", "Public intake"); }).catch(toast.error)}>Nonaktifkan</Button>
                      </>
                    )}
                  </span>
                </li>
              );
            })}
          </ul>
        </CardContent>
      </Card>
    </div>
  );
}
