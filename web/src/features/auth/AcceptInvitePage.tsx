// Terima undangan (PRD P0 v2 §26.1): halaman publik /accept-invite?token=… — set password + konfirmasi →
// POST /auth/accept-invite {token, password} → 204 → tautan ke /login. Error 400 INVITE_INVALID / INVITE_EXPIRED.
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { BuildingVisionLogo } from "@buildingvision/ui/bv";
import { Icon } from "@buildingvision/ui";
import { Alert, Button, Card, Field, Input } from "@/components/ui/primitives";
import { api, ApiError } from "@/lib/api";

export function AcceptInvitePage() {
  const { t } = useTranslation();
  const [sp] = useSearchParams();
  const token = sp.get("token") ?? "";
  const [pw, setPw] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (pw.length < 8) return setError(t("profile.pw_min"));
    if (pw !== confirm) return setError(t("profile.pw_mismatch"));
    setBusy(true);
    try {
      await api("auth/accept-invite", { body: { token, password: pw }, retry: false });
      setDone(true);
    } catch (err) {
      const code = err instanceof ApiError ? err.code : "";
      setError(code === "INVITE_EXPIRED" ? t("invite.expired") : code === "INVITE_INVALID" ? t("invite.invalid") : (err as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4">
      <Card className="w-full max-w-sm p-8">
        <div className="mb-6 flex flex-col items-start gap-4">
          <BuildingVisionLogo height={40} tagline={t("auth.subtitle")} />
          <h1 className="text-h2 font-bold">{t("invite.title")}</h1>
          {!done && <p className="text-sm text-on-surface-variant">{t("invite.desc")}</p>}
        </div>
        {!token ? (
          <Alert variant="critical" title={t("invite.invalid")}>{t("invite.no_token")}</Alert>
        ) : done ? (
          <div className="space-y-4" data-testid="invite-done">
            <Alert variant="success" title={t("invite.done_title")}>{t("invite.done_desc")}</Alert>
            <Link to="/login" className="flex h-10 items-center justify-center gap-2 rounded-[var(--radius-md)] bg-primary text-sm font-semibold text-on-primary"><Icon name="login" size={18} /> {t("auth.login")}</Link>
          </div>
        ) : (
          <form onSubmit={submit} className="space-y-4">
            {error && <Alert variant="critical">{error}</Alert>}
            <Field label={t("profile.new_password")} required help={t("profile.pw_min")}>
              <Input type="password" autoFocus autoComplete="new-password" value={pw} onChange={(e) => setPw(e.target.value)} />
            </Field>
            <Field label={t("profile.confirm_password")} required>
              <Input type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
            </Field>
            <Button type="submit" className="w-full" loading={busy}>{t("invite.submit")}</Button>
          </form>
        )}
        <p className="mt-6 text-center text-xs text-muted-foreground"><Link to="/login" className="hover:underline">{t("invite.back_login")}</Link></p>
      </Card>
    </div>
  );
}
