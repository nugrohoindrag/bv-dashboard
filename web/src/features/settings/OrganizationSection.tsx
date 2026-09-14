// Organization: nama, timezone (tampil), pengaturan umum, public intake per property (link form publik + key).
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Button, Card, CardContent, CardHeader, CardTitle, Field, Input } from "@/components/ui/primitives";
import { AsyncState, KeyValue, useToast } from "@/components/bv/common";
import { useAction, useInvalidate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";

interface Org { id: string; code: string; slug: string; name: string; timezone: string; is_active: boolean; settings: Record<string, unknown>; version: number }

export default function OrganizationSection() {
  const { t } = useTranslation();
  const { properties, can, refreshPrincipal } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const org = useQuery({ queryKey: ["org"], queryFn: () => api<Org>("organizations/me") });
  const [name, setName] = useState("");
  useEffect(() => { if (org.data) setName(org.data.name); }, [org.data]);
  const [saving, setSaving] = useState(false);
  const intake = useAction<{ property_id: string; enabled: boolean }, { intake_key: string | null; enabled: boolean }>(() => "service-requests/public-intake");
  const [keys, setKeys] = useState<Record<string, string | null>>({});
  const save = async () => {
    setSaving(true);
    try {
      await api("organizations/me", { method: "PATCH", body: { name } });
      invalidate("org");
      await refreshPrincipal();
      toast.success("Organisasi disimpan");
    } catch (e) {
      toast.error(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <div className="space-y-5">
      <Card>
        <CardHeader><CardTitle>Organisasi</CardTitle></CardHeader>
        <CardContent>
          <AsyncState query={org}>
            {(o) => (
              <div className="grid grid-cols-2 gap-5">
                <div className="space-y-3">
                  <Field label="Nama organisasi"><Input value={name} onChange={(e) => setName(e.target.value)} disabled={!can("platform.organizations.update")} /></Field>
                  {can("platform.organizations.update") && <Button loading={saving} onClick={save}>{t("action.save")}</Button>}
                </div>
                <KeyValue items={[{ label: "Kode", value: o.code }, { label: "Slug", value: o.slug }, { label: "Timezone", value: o.timezone }, { label: "Status", value: o.is_active ? "Aktif" : "Nonaktif" }]} />
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
                <li key={p.id} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
                  <span><span className="font-medium">{p.name}</span> <span className="font-mono text-xs text-muted-foreground">{p.code}</span></span>
                  <span className="flex items-center gap-2">
                    {key && <code className="max-w-[360px] truncate rounded bg-muted px-1.5 py-0.5 text-xs">{key.startsWith("(") ? key : `${window.location.origin}/public/intake?key=${key}`}</code>}
                    {can("platform.organizations.update") && (
                      <>
                        <Button size="sm" variant="secondary" loading={intake.isPending} onClick={() => intake.mutateAsync({ property_id: p.id, enabled: true }).then((r) => { setKeys((k) => ({ ...k, [p.id]: r.intake_key })); toast.success("Public intake aktif"); }).catch(toast.error)}>Aktifkan / rotate key</Button>
                        <Button size="sm" variant="ghost" onClick={() => intake.mutateAsync({ property_id: p.id, enabled: false }).then(() => { setKeys((k) => ({ ...k, [p.id]: null })); toast.success("Public intake dinonaktifkan"); }).catch(toast.error)}>Nonaktifkan</Button>
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
