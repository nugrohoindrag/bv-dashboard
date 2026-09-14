// SLA Policies (PRD §10.5): matriks object_type × priority; response/resolution menit, ambang risk, kalender (24x7 | business_hours); default org atau per property.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button, Card, CardContent, CardHeader, CardTitle, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { useAll, useCreate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtMinutes } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { SLAPolicy } from "@/api/types";

const OBJECTS = ["work_order", "task", "service_request", "incident"];
const PRIOS = ["critical", "high", "medium", "low"];

export default function SLASection() {
  const { t } = useTranslation();
  const { properties, can } = useAuth();
  const [scope, setScope] = useState("");
  const list = useAll<SLAPolicy>("sla-policies");
  const [edit, setEdit] = useState<{ object_type: string; priority: string; existing?: SLAPolicy } | null>(null);
  const policies = useMemo(() => (list.data ?? []).filter((p) => (scope ? p.property_id === scope : p.property_id === null)), [list.data, scope]);
  const find = (o: string, p: string) => policies.find((x) => x.object_type === o && x.priority === p);
  const canEdit = can("operations.sla_policies.create") || can("operations.sla_policies.update");
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <NativeSelect className="w-64" value={scope} onChange={(e) => setScope(e.target.value)}><option value="">Default organisasi</option>{properties.map((p) => <option key={p.id} value={p.id}>Override: {p.name}</option>)}</NativeSelect>
        <span className="text-sm text-muted-foreground">Policy per property menimpa default organisasi. Sel kosong = mengikuti default.</span>
      </div>
      {OBJECTS.map((o) => (
        <Card key={o}>
          <CardHeader><CardTitle>{t(`nav.${o}s`, { defaultValue: o })}</CardTitle></CardHeader>
          <CardContent className="grid grid-cols-4 gap-3">
            {PRIOS.map((p) => {
              const pol = find(o, p);
              return (
                <button key={p} type="button" disabled={!canEdit} onClick={() => setEdit({ object_type: o, priority: p, existing: pol })} className={cn("rounded-md border p-3 text-left text-sm hover:bg-muted", pol?.is_active ? "border-border" : "border-dashed border-border text-muted-foreground")}>
                  <div className="mb-1 font-medium">{t(`priority.${p}`)}</div>
                  {pol ? (
                    <div className="space-y-0.5 text-xs">
                      <div>Response: <span className="tnum">{pol.response_minutes ? fmtMinutes(pol.response_minutes) : "—"}</span></div>
                      <div>Resolution: <span className="tnum">{fmtMinutes(pol.resolution_minutes)}</span></div>
                      <div>Risk {pol.risk_threshold_pct}% · {pol.calendar}{pol.is_active ? "" : " · nonaktif"}</div>
                    </div>
                  ) : <div className="text-xs">{scope ? "Ikuti default" : "Belum diatur"}</div>}
                </button>
              );
            })}
          </CardContent>
        </Card>
      ))}
      {edit && <SLADialog objectType={edit.object_type} priority={edit.priority} propertyId={scope || null} existing={edit.existing} onClose={() => setEdit(null)} />}
    </div>
  );
}

function SLADialog({ objectType, priority, propertyId, existing, onClose }: { objectType: string; priority: string; propertyId: string | null; existing?: SLAPolicy; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [form, setForm] = useState({ response: existing?.response_minutes?.toString() ?? "", resolution: existing?.resolution_minutes?.toString() ?? "", risk: existing?.risk_threshold_pct?.toString() ?? "80", calendar: existing?.calendar ?? "24x7", is_active: existing?.is_active ?? true });
  const upsert = useCreate<Record<string, unknown>>("sla-policies");
  const submit = () => {
    if (!form.resolution) return toast.error(new Error("Resolution (menit) wajib"));
    upsert.mutateAsync({ property_id: propertyId, object_type: objectType, priority, response_minutes: form.response ? Number(form.response) : null, resolution_minutes: Number(form.resolution), risk_threshold_pct: Number(form.risk), calendar: form.calendar, is_active: form.is_active }).then(() => { toast.success("SLA disimpan"); onClose(); }).catch(toast.error);
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`SLA · ${objectType} · ${t(`priority.${priority}`)}`} description={propertyId ? "Override untuk property terpilih" : "Default organisasi"}>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Response (menit)" help="Kosong = tanpa target response"><Input type="number" min={0} value={form.response} onChange={(e) => setForm({ ...form, response: e.target.value })} /></Field>
          <Field label="Resolution (menit)" required><Input type="number" min={1} value={form.resolution} onChange={(e) => setForm({ ...form, resolution: e.target.value })} /></Field>
          <Field label="Ambang SLA risk (%)"><Input type="number" min={1} max={99} value={form.risk} onChange={(e) => setForm({ ...form, risk: e.target.value })} /></Field>
          <Field label="Kalender"><NativeSelect value={form.calendar} onChange={(e) => setForm({ ...form, calendar: e.target.value })}><option value="24x7">24x7</option><option value="business_hours">Jam kerja (08:00–17:00, Sen–Jum)</option></NativeSelect></Field>
        </div>
        <label className="mt-3 flex items-center gap-2 text-sm"><Checkbox checked={form.is_active} onCheckedChange={(v) => setForm({ ...form, is_active: !!v })} /> Aktif</label>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={upsert.isPending} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
