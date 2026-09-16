// About, Security & Trust, Book a Demo, Start Free Trial / Login (redirect ke dashboard), Legal, 404.
import { useEffect, useState, type FormEvent } from "react";
import { Link, useParams, Navigate } from "react-router-dom";
import { Seo } from "@/lib/head";
import { CtaBand, FeatureGrid, Hero, ImageSplit } from "@/components/blocks";
import { Picture } from "@/components/Picture";
import { Icon } from "@/components/Icon";
import { Button, ButtonLink, Card, Container, Eyebrow, Heading, Lead, Section } from "@/components/ui";
import { requestDemo } from "@/lib/api";
import { LINKS, SALES_EMAIL, SITE_NAME, signupHref } from "@/lib/config";
import { track } from "@/lib/analytics";

// ---------- About ----------
export function AboutPage() {
  return (
    <>
      <Seo title="About BuildingVision" description="BuildingVision is a building operations platform built for the people who keep hotels, apartments, and office buildings running." path="/about" image="/images/modern-building-1440.webp" />
      <Hero eyebrow="About" title="Built for the people who keep buildings running" lead="We started BuildingVision after watching too many property teams run world-class buildings on radios, whiteboards, and group chats. The work deserved better tools." image="modern-building" source="about" />
      <ImageSplit eyebrow="What we believe" title="Operations first, software second" lead="Every feature starts with a question from the field: who owns this, when is it due, and how do we prove it was done? If a feature does not help answer that, it does not ship." bullets={["One platform, configured per property, not three products", "Evidence over assumptions", "The field team is a first-class user", "Tenants and guests deserve a clear status, not a phone call"]} image="property-manager" reverse />
      <FeatureGrid eyebrow="Long-term positioning" title="From platform to building operating system" lead="Today: integrated building operations SaaS. Tomorrow: the operating layer every property runs on." tone="muted" items={[
        { icon: "hub", title: "One data model", body: "Locations, assets, work, and people are shared by every module." },
        { icon: "extension", title: "Profiles, not forks", body: "Hotel, Apartment, Office adjust the platform without splitting it." },
        { icon: "api", title: "Open contracts", body: "Documented API and status maps shared with the apps." },
      ]} />
      <CtaBand source="about_cta" />
    </>
  );
}

// ---------- Security & Trust ----------
export function SecurityPage() {
  return (
    <>
      <Seo title="Security & Trust | BuildingVision" description="How BuildingVision protects your organization's data: isolation between organizations and properties, role-based access, audit history, and secure APIs." path="/security" image="/images/cctv-1440.webp" />
      <Hero eyebrow="Security and trust" title="Your building's data, kept where it belongs" lead="Isolation between organizations and properties, roles that go down to a single property, and an audit trail for every administrative change." image="cctv" source="security" secondary={{ to: "/book-a-demo", label: "Talk to us" }} />
      <FeatureGrid eyebrow="Controls" title="What is in place" tone="muted" items={[
        { icon: "lock", title: "Secure authentication", body: "Argon2id password hashing, short-lived access tokens, rotating refresh tokens with reuse detection, lockout on repeated failures." },
        { icon: "mark_email_read", title: "Email verification", body: "Self-serve accounts are created only after the email is confirmed." },
        { icon: "shield", title: "Server-side authorization", body: "Every endpoint checks permissions on the server. The UI hiding a button is never the control." },
        { icon: "corporate_fare", title: "Organization isolation", body: "Row-level security in the database keeps each organization's data separate, enforced on every query." },
        { icon: "domain", title: "Property isolation", body: "Roles can be scoped to a property. A supervisor in Tower A does not see Tower B unless you say so." },
        { icon: "admin_panel_settings", title: "Role-based access control", body: "Permission catalog per module, object, and action. System roles for every persona, custom roles when you need them." },
        { icon: "history", title: "Audit history", body: "Administrative changes are logged with actor, before and after values, IP, and time, including app download configuration." },
        { icon: "verified_user", title: "Secure APIs", body: "HTTPS everywhere, input validation, idempotency keys, signed upload URLs, verified payment callbacks." },
        { icon: "link", title: "URL validation", body: "Configured download destinations are validated before they are saved. The public API only ever exposes active links." },
      ]} />
      <ImageSplit eyebrow="Operations" title="Backups, monitoring, and recovery" lead="Continuous WAL archiving with scheduled full and incremental backups, restore drills, metrics and alerts on every service, and rollback by redeploying the previous version." bullets={["Point-in-time recovery for the database", "Versioned object storage for photos and documents", "Prometheus metrics, alerting, and centralized logs", "Documented runbooks for deploy, backup, restore, and incidents"]} image="server-room" reverse />
      <Section tone="muted">
        <div className="max-w-2xl"><Eyebrow>Questions</Eyebrow><Heading>Security review or questionnaire?</Heading><Lead>We are happy to walk your IT team through architecture, data flows, and controls. Write to <a className="text-primary underline" href={`mailto:${SALES_EMAIL}`}>{SALES_EMAIL}</a>.</Lead></div>
      </Section>
      <CtaBand source="security_cta" />
    </>
  );
}

// ---------- Book a Demo (§34) ----------
export function BookDemoPage() {
  const [form, setForm] = useState({ full_name: "", email: "", company: "", phone: "", property_profile: "" as "" | "hotel" | "apartment" | "office", property_count: "", message: "" });
  const [state, setState] = useState<{ status: "idle" | "busy" | "done" | "error"; message?: string }>({ status: "idle" });
  useEffect(() => {
    track("book_demo_clicked", { placement: "page" });
  }, []);
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }));
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setState({ status: "busy" });
    try {
      await requestDemo({ full_name: form.full_name, email: form.email, company: form.company || undefined, phone: form.phone || undefined, property_profile: form.property_profile, property_count: form.property_count ? Number(form.property_count) : undefined, message: form.message || undefined, source_page: "/book-a-demo" });
      track("demo_requested", { profile: form.property_profile || undefined });
      setState({ status: "done" });
    } catch (err) {
      setState({ status: "error", message: (err as Error).message });
    }
  };
  const input = "h-11 w-full rounded-[var(--radius-md)] border border-border bg-surface px-3 text-sm text-on-surface placeholder:text-on-surface-variant/70 focus:border-primary";
  return (
    <>
      <Seo title="Book a Demo | BuildingVision" description="See BuildingVision with someone who has run building operations. A 30-minute walkthrough for your hotel, apartment, or office property." path="/book-a-demo" image="/images/meeting-room-1440.webp" />
      <section className="bv-hero-bg">
        <Container className="grid gap-10 py-16 lg:grid-cols-12 sm:py-20">
          <div className="lg:col-span-6">
            <Eyebrow>Book a demo</Eyebrow>
            <Heading as="h1">See it with someone who has run a building</Heading>
            <Lead>A 30-minute walkthrough tailored to your property. We will show the workflow that matters most to you, from request to documented fix, and answer the hard questions.</Lead>
            <ul className="mt-6 space-y-2.5 text-sm text-on-surface">
              {["Your profile: hotel, apartment, or office", "Dashboard, Staff App, and Tenant App in one session", "Rollout plan and pricing that fits your portfolio"].map((b) => <li key={b} className="flex items-start gap-2"><Icon name="check_circle" size={20} className="mt-0.5 text-primary" />{b}</li>)}
            </ul>
            <p className="mt-6 text-sm text-on-surface-variant">Prefer to explore on your own? <a href={signupHref("book_demo")} className="font-semibold text-primary underline" onClick={() => track("start_trial_clicked", { placement: "book_demo" })}>Start a free trial</a>.</p>
            <div className="mt-8"><Picture image="meeting-room" sizes="(min-width: 1024px) 520px, 100vw" /></div>
          </div>
          <div className="lg:col-span-6">
            <Card className="p-6 sm:p-8">
              {state.status === "done" ? (
                <div>
                  <div className="mb-3 inline-flex h-12 w-12 items-center justify-center rounded-full bg-success-container text-on-success-container"><Icon name="check" size={26} /></div>
                  <h2 className="text-xl font-bold text-on-surface">Thanks, we got it</h2>
                  <p className="mt-2 text-sm text-on-surface-variant">Someone from our team will reach out within one business day to find a time that works for you. We also sent a confirmation to {form.email}.</p>
                  <div className="mt-6"><ButtonLink to="/" variant="secondary">Back to home</ButtonLink></div>
                </div>
              ) : (
                <form onSubmit={submit} className="grid gap-4 sm:grid-cols-2">
                  <h2 className="text-xl font-bold text-on-surface sm:col-span-2">Tell us about your property</h2>
                  {state.status === "error" && <div className="rounded-[var(--radius-md)] bg-error-container px-3 py-2 text-sm text-on-error-container sm:col-span-2">{state.message}</div>}
                  <label className="text-sm font-medium text-on-surface">Your name<input required className={input} value={form.full_name} onChange={set("full_name")} autoComplete="name" /></label>
                  <label className="text-sm font-medium text-on-surface">Work email<input required type="email" className={input} value={form.email} onChange={set("email")} autoComplete="email" /></label>
                  <label className="text-sm font-medium text-on-surface">Company<input className={input} value={form.company} onChange={set("company")} autoComplete="organization" /></label>
                  <label className="text-sm font-medium text-on-surface">Phone<input className={input} value={form.phone} onChange={set("phone")} autoComplete="tel" /></label>
                  <label className="text-sm font-medium text-on-surface">Property type
                    <select className={input} value={form.property_profile} onChange={set("property_profile")}>
                      <option value="">Choose one</option><option value="hotel">Hotel</option><option value="apartment">Apartment</option><option value="office">Office</option>
                    </select>
                  </label>
                  <label className="text-sm font-medium text-on-surface">Number of properties<input type="number" min={1} className={input} value={form.property_count} onChange={set("property_count")} /></label>
                  <label className="text-sm font-medium text-on-surface sm:col-span-2">What would you like to see?<textarea rows={3} className={`${input} h-auto py-2`} value={form.message} onChange={set("message")} /></label>
                  <div className="sm:col-span-2"><Button type="submit" size="lg" className="w-full" disabled={state.status === "busy"}>{state.status === "busy" ? "Sending" : "Request a demo"}</Button></div>
                  <p className="text-xs text-on-surface-variant sm:col-span-2">We only use these details to arrange your demo.</p>
                </form>
              )}
            </Card>
          </div>
        </Container>
      </section>
    </>
  );
}

// ---------- Redirects to the dashboard (§25, §35) ----------
export function StartTrialRedirect() {
  const to = signupHref("website_start_free_trial");
  useEffect(() => {
    track("start_trial_clicked", { placement: "redirect_page" });
    window.location.replace(to);
  }, [to]);
  return (
    <>
      <Seo title="Start Free Trial | BuildingVision" description="Start your 14-day free trial of BuildingVision. No credit card required." path="/start-free-trial" noindex />
      <RedirectBody label="Taking you to sign up" href={to} />
    </>
  );
}

export function LoginRedirect() {
  useEffect(() => {
    track("login_clicked");
    window.location.replace(LINKS.login);
  }, []);
  return (
    <>
      <Seo title="Log in | BuildingVision" description="Log in to the BuildingVision dashboard." path="/login" noindex />
      <RedirectBody label="Taking you to the dashboard" href={LINKS.login} />
    </>
  );
}

function RedirectBody({ label, href }: { label: string; href: string }) {
  return (
    <Section><div className="mx-auto max-w-md text-center"><Heading as="h3">{label}</Heading><Lead className="mx-auto">If nothing happens, <a className="text-primary underline" href={href}>continue here</a>.</Lead></div></Section>
  );
}

// ---------- Legal ----------
const LEGAL_TEXT: Record<string, { title: string; body: string[] }> = {
  terms: { title: "Terms of Service", body: [
    "These terms govern the use of BuildingVision by organizations and their users. By creating an account you agree to them on behalf of your organization.",
    "Free trials run for 14 days and require no payment details. When a trial ends without a plan, the workspace becomes read-only and data is retained for 90 days before deletion, unless a plan is chosen.",
    "Paid plans are billed per property per month or per year as agreed. Invoices are issued by BuildingVision and payable within 14 days.",
    "Customers own their data. BuildingVision processes it only to provide the service and does not sell it. Customers are responsible for the accuracy of the data they enter and for managing their users' access.",
    "The service is provided with reasonable care and skill. Liability is limited to the fees paid in the twelve months preceding a claim, except where the law does not allow such a limit.",
    "This is a summary for the initial release. The full agreement is provided with your order form.",
  ] },
  privacy: { title: "Privacy Policy", body: [
    "BuildingVision collects the information needed to run the service: account details (name, email, organization), operational data entered by customers, and technical logs for security and reliability.",
    "Website analytics are limited to funnel events (for example, page viewed, trial started) with a random anonymous identifier. We do not use third-party advertising trackers.",
    "Emails are sent for account verification, trial lifecycle notices, and support. You can reply to any of them to reach a person.",
    "Data is stored in managed infrastructure with encrypted transport, access control, backups, and audit logging. Organizations are isolated from one another at the database level.",
    "You can request access to, correction of, or deletion of your personal data by writing to support. Customers can export their operational data on request.",
    "This is a summary for the initial release. The full policy is provided with your order form.",
  ] },
};

export function LegalPage() {
  const { doc } = useParams();
  const d = doc ? LEGAL_TEXT[doc] : undefined;
  if (!d || !doc) return <Navigate to="/404" replace />;
  return (
    <>
      <Seo title={`${d.title} | ${SITE_NAME}`} description={`${SITE_NAME} ${d.title.toLowerCase()}.`} path={`/legal/${doc}`} type="article" />
      <Section><div className="mx-auto max-w-3xl"><Eyebrow>Legal</Eyebrow><Heading as="h1" className="text-[32px] sm:text-[40px]">{d.title}</Heading><div className="mt-6 space-y-4 text-base leading-relaxed text-on-surface-variant">{d.body.map((p) => <p key={p}>{p}</p>)}</div><p className="mt-8 text-sm text-on-surface-variant">Last updated: 16 September 2026</p></div></Section>
    </>
  );
}

// ---------- 404 ----------
export function NotFoundPage() {
  return (
    <>
      <Seo title="Page not found | BuildingVision" description="This page does not exist." path="/404" noindex />
      <Section><div className="mx-auto max-w-md text-center"><div className="text-6xl font-extrabold text-primary">404</div><Heading as="h3">We could not find that page</Heading><Lead className="mx-auto">The link may be old or the page may have moved.</Lead><div className="mt-6 flex justify-center gap-3"><ButtonLink to="/" variant="secondary">Home</ButtonLink><ButtonLink to="/platform">Explore the platform</ButtonLink></div><p className="mt-6 text-sm text-on-surface-variant">Looking for the apps? <Link to="/download" className="text-primary underline">Download Apps</Link></p></div></Section>
    </>
  );
}
