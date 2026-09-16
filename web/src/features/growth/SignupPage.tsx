// Start Free Trial → Create Account (Website PRD §24–§25). Copy in English, humanized, no em dash (§37–§38).
import { useState } from "react";
import { Link, Navigate, useSearchParams } from "react-router-dom";
import { BuildingVisionLogo } from "@buildingvision/ui/bv";
import { Alert, Button, Card, Field, Input } from "@/components/ui/primitives";
import { Icon } from "@/components/ui/primitives";
import { api, ApiError } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { anonymousId, track } from "@/lib/growth";

const HIGHLIGHTS = [
  "14-day free trial, no credit card required",
  "One platform for Hotel, Apartment, and Office operations",
  "Staff App and Tenant App included",
];

export function SignupPage() {
  const { principal, loading } = useAuth();
  const [params] = useSearchParams();
  const [form, setForm] = useState({ full_name: "", email: "", organization_name: "", password: "" });
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState<{ email: string; devUrl?: string } | null>(null);
  const [resent, setResent] = useState(false);
  if (!loading && principal) return <Navigate to="/onboarding" replace />;

  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm((f) => ({ ...f, [k]: e.target.value }));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setFieldErrors({});
    try {
      track("signup_started");
      const res = await api<{ email: string; dev_verification_url?: string }>("public/signup", {
        body: { ...form, source_page: params.get("source") || "/signup", anonymous_id: anonymousId() },
        retry: false,
      });
      setSent({ email: res.email, devUrl: res.dev_verification_url });
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.problem.errors?.length) setFieldErrors(Object.fromEntries(err.problem.errors.map((x) => [x.field, x.message])));
        setError(err.code === "EMAIL_TAKEN" ? "An account with this email already exists. Try logging in instead." : err.message);
      } else setError("Something went wrong. Please try again.");
    } finally {
      setBusy(false);
    }
  };

  const resend = async () => {
    if (!sent) return;
    try {
      await api("public/signup/resend", { body: { email: sent.email }, retry: false });
      setResent(true);
    } catch {
      setResent(true);
    }
  };

  return (
    <div className="min-h-screen bg-background">
      <div className="mx-auto grid min-h-screen max-w-6xl grid-cols-1 items-center gap-10 px-6 py-10 lg:grid-cols-12">
        <div className="hidden lg:col-span-6 lg:block">
          <BuildingVisionLogo height={40} tagline="Building Operations Platform" />
          <h1 className="mt-10 text-[36px] font-extrabold leading-tight tracking-tight text-on-surface">Run your building operations from one place.</h1>
          <p className="mt-4 max-w-md text-base text-on-surface-variant">Housekeeping, engineering, security, and tenant relation, working from the same list of what needs attention today.</p>
          <ul className="mt-8 space-y-3">
            {HIGHLIGHTS.map((h) => (
              <li key={h} className="flex items-start gap-3 text-sm text-on-surface">
                <Icon name="check_circle" size={20} className="mt-0.5 shrink-0 text-primary" />
                <span>{h}</span>
              </li>
            ))}
          </ul>
        </div>
        <div className="lg:col-span-6">
          <Card className="mx-auto w-full max-w-md p-8">
            <div className="mb-6 lg:hidden"><BuildingVisionLogo height={32} /></div>
            {sent ? (
              <div className="space-y-4">
                <div className="flex h-12 w-12 items-center justify-center rounded-full bg-primary-soft text-primary"><Icon name="mark_email_read" size={26} /></div>
                <h1 className="text-h2 font-bold text-on-surface">Check your inbox</h1>
                <p className="text-sm text-on-surface-variant">We sent a confirmation link to <strong className="text-on-surface">{sent.email}</strong>. Open it to finish creating your workspace. The link is valid for 24 hours.</p>
                {sent.devUrl && (
                  <Alert variant="info" title="Local development">No mail server is configured, so here is your link: <a className="underline" href={sent.devUrl}>Confirm email</a></Alert>
                )}
                <p className="text-sm text-on-surface-variant">
                  Did not get it? Check your spam folder or{" "}
                  {resent ? <span className="text-primary">a new link is on its way.</span> : <button type="button" className="font-medium text-primary underline" onClick={resend}>send it again</button>}
                </p>
                <p className="pt-4 text-xs text-on-surface-variant">Already verified? <Link to="/login" className="text-primary underline">Log in</Link></p>
              </div>
            ) : (
              <form onSubmit={submit} className="space-y-4">
                <div>
                  <h1 className="text-h2 font-bold text-on-surface">Start your free trial</h1>
                  <p className="mt-1 text-sm text-on-surface-variant">14 days, all features, no credit card.</p>
                </div>
                {error && <Alert variant="critical">{error}</Alert>}
                <Field label="Your name" required error={fieldErrors.full_name}>
                  <Input autoFocus autoComplete="name" value={form.full_name} onChange={set("full_name")} placeholder="Ana Pratama" />
                </Field>
                <Field label="Work email" required error={fieldErrors.email}>
                  <Input type="email" autoComplete="email" value={form.email} onChange={set("email")} placeholder="ana@yourcompany.com" />
                </Field>
                <Field label="Organization or company" required error={fieldErrors.organization_name} help="You can rename it later.">
                  <Input autoComplete="organization" value={form.organization_name} onChange={set("organization_name")} placeholder="Pangeran Property Group" />
                </Field>
                <Field label="Password" required error={fieldErrors.password} help="At least 8 characters.">
                  <Input type="password" autoComplete="new-password" value={form.password} onChange={set("password")} />
                </Field>
                <Button type="submit" className="w-full" loading={busy}>{busy ? "Creating your account" : "Create account"}</Button>
                <p className="text-center text-xs text-on-surface-variant">By continuing you agree to the BuildingVision terms of service and privacy policy.</p>
                <p className="text-center text-sm text-on-surface-variant">Already have an account? <Link to="/login" className="font-medium text-primary underline">Log in</Link></p>
              </form>
            )}
          </Card>
        </div>
      </div>
    </div>
  );
}

export default SignupPage;
