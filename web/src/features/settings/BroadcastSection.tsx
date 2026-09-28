// Broadcast notifikasi sistem (PRD P0 v2 §14.2): judul, isi, severity, target (property / role / user), deep link opsional,
// email opsional → POST /notifications/broadcast → 202 {recipients}. Perm platform.notifications.broadcast.
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Checkbox, Field, Input, NativeSelect, Segmented, Textarea } from "@/components/ui/primitives";
import { UserPicker } from "@/components/bv/pickers";
import { useToast } from "@/components/bv/common";
import { useAll } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { Role, User } from "@/api/types";

type Severity = "info" | "warning" | "critical" | "success";

export default function BroadcastSection() {
  const { t } = useTranslation();
  const toast = useToast();
  const { properties, can } = useAuth();
  const roles = useAll<Role>("roles", {}, { enabled: can("iam.roles.view") });
  const users = useAll<User>("users", {}, { enabled: can("iam.users.view") });
  const [form, setForm] = useState({ title: "", body: "", severity: "info" as Severity, property_id: "", role_codes: [] as string[], user_ids: [] as string[], deep_link: "", email: false });
  const [err, setErr] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const [last, setLast] = useState<number | null>(null);
  const submit = async () => {
    setErr(null);
    if (!form.title.trim() || !form.body.trim()) return setErr(t("broadcast.required"));
    if (form.deep_link && !form.deep_link.startsWith("/")) return setErr(t("broadcast.deep_link_invalid"));
    setSending(true);
    try {
      const r = await api<{ recipients: number }>("notifications/broadcast", {
        body: { title: form.title.trim(), body: form.body.trim(), severity: form.severity, property_id: form.property_id || null, role_codes: form.role_codes, user_ids: form.user_ids, deep_link: form.deep_link || null, email: form.email },
      });
      setLast(r.recipients);
      toast.success(t("broadcast.sent", { n: r.recipients }));
      setForm((f) => ({ ...f, title: "", body: "", deep_link: "" }));
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setSending(false);
    }
  };
  const userName = (id: string) => users.data?.find((u) => u.id === id)?.full_name ?? id.slice(0, 8);
  return (
    <Card className="max-w-3xl">
      <CardHeader><div><CardTitle>{t("nav.broadcast")}</CardTitle><CardSubtitle>{t("broadcast.hint")}</CardSubtitle></div></CardHeader>
      <CardContent className="space-y-4">
        {err && <Alert variant="critical">{err}</Alert>}
        {last !== null && <Alert variant="success">{t("broadcast.sent", { n: last })}</Alert>}
        <Field label={t("label.title")} required><Input value={form.title} maxLength={120} onChange={(e) => setForm({ ...form, title: e.target.value })} /></Field>
        <Field label={t("broadcast.body")} required><Textarea rows={3} maxLength={1000} value={form.body} onChange={(e) => setForm({ ...form, body: e.target.value })} /></Field>
        <Field label="Severity">
          <div className="max-w-full overflow-x-auto pb-1"><Segmented<Severity> value={form.severity} onChange={(v) => setForm({ ...form, severity: v })} options={[{ value: "info", label: "Info" }, { value: "success", label: "Success" }, { value: "warning", label: "Warning" }, { value: "critical", label: "Critical", tone: "critical" }]} /></div>
        </Field>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Property" help={t("broadcast.property_help")}>
            <NativeSelect value={form.property_id} onChange={(e) => setForm({ ...form, property_id: e.target.value })}><option value="">{t("label.all_properties")}</option>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect>
          </Field>
          <Field label={t("broadcast.deep_link")} help={t("broadcast.deep_link_help")}><Input placeholder="/operations/work-orders" value={form.deep_link} onChange={(e) => setForm({ ...form, deep_link: e.target.value })} /></Field>
        </div>
        {(roles.data?.length ?? 0) > 0 && (
          <Field label={t("broadcast.roles")} help={t("broadcast.roles_help")}>
            <div className="flex flex-wrap gap-x-4 gap-y-1.5">
              {(roles.data ?? []).filter((r) => r.code !== "platform_admin").map((r) => (
                <label key={r.id} className="flex items-center gap-1.5 text-sm"><Checkbox checked={form.role_codes.includes(r.code)} onCheckedChange={() => setForm((f) => ({ ...f, role_codes: f.role_codes.includes(r.code) ? f.role_codes.filter((x) => x !== r.code) : [...f.role_codes, r.code] }))} /> {r.name}</label>
              ))}
            </div>
          </Field>
        )}
        {can("iam.users.view") && (
          <Field label={t("broadcast.users")}>
            <div className="space-y-2">
              <UserPicker value={null} onChange={(id) => id && !form.user_ids.includes(id) && setForm((f) => ({ ...f, user_ids: [...f.user_ids, id] }))} placeholder={t("broadcast.add_user")} />
              {form.user_ids.length > 0 && (
                <div className="flex flex-wrap gap-1.5">
                  {form.user_ids.map((id) => (
                    <span key={id} className="inline-flex items-center gap-1 rounded-full bg-surface-container-high px-2 py-0.5 text-xs text-on-surface-variant">
                      {userName(id)}
                      <button type="button" aria-label={`${t("action.cancel")} ${userName(id)}`} onClick={() => setForm((f) => ({ ...f, user_ids: f.user_ids.filter((x) => x !== id) }))}>×</button>
                    </span>
                  ))}
                </div>
              )}
            </div>
          </Field>
        )}
        <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.email} onCheckedChange={(v) => setForm({ ...form, email: !!v })} /> {t("broadcast.email")}</label>
        <div className="flex justify-end"><Button icon="send" loading={sending} onClick={submit}>{t("broadcast.send")}</Button></div>
      </CardContent>
    </Card>
  );
}
