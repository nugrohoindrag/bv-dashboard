import { useState } from "react";
import { Navigate, useLocation, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { BuildingVisionLogo } from "@buildingvision/ui/bv";
import { Alert, Button, Card, Field, Input } from "@/components/ui/primitives";
import { useAuth } from "@/lib/auth";

export function LoginPage() {
  const { t } = useTranslation();
  const { login, principal, loading } = useAuth();
  const nav = useNavigate();
  const loc = useLocation();
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  if (!loading && principal) return <Navigate to={(loc.state as { from?: string })?.from || "/overview"} replace />;
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(identifier, password);
      nav((loc.state as { from?: string })?.from || "/overview", { replace: true });
    } catch (err) {
      setError((err as { message?: string })?.message ?? "Login gagal");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4">
      <Card className="w-full max-w-sm p-8">
        <div className="mb-6 flex flex-col items-start gap-4">
          <BuildingVisionLogo height={40} tagline={t("auth.subtitle")} />
          <h1 className="text-h2 font-bold">{t("auth.title")}</h1>
        </div>
        <form onSubmit={submit} className="space-y-4">
          {error && <Alert variant="critical">{error}</Alert>}
          <Field label={t("auth.identifier")} required>
            <Input autoFocus autoComplete="username" value={identifier} onChange={(e) => setIdentifier(e.target.value)} />
          </Field>
          <Field label={t("auth.password")} required>
            <Input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <Button type="submit" className="w-full" loading={busy}>{busy ? t("auth.signing_in") : t("auth.login")}</Button>
        </form>
        <p className="mt-6 text-center text-xs text-muted-foreground">BuildingVision Web · Desktop ≥1280px</p>
      </Card>
    </div>
  );
}
