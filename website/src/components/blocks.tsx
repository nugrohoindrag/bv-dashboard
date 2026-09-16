// Blok halaman yang dipakai ulang (Website PRD §7, §9–§12, §23): hero, grid fitur, langkah, split gambar, FAQ, CTA band, download.
import { useEffect, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { Icon } from "./Icon";
import { Picture, type ImageKey } from "./Picture";
import { ButtonLink, Card, Check, Container, Eyebrow, Heading, Lead, Section, cn } from "./ui";
import { fetchApps, type PublicApp } from "@/lib/api";
import { LINKS, signupHref } from "@/lib/config";
import { track } from "@/lib/analytics";

export interface Feature { icon: string; title: string; body: string; to?: string }
export interface Step { title: string; body: string }
export interface Faq { q: string; a: string }

export function Hero({ eyebrow, title, lead, image, children, primary = { to: signupHref("hero"), label: "Start Free Trial" }, secondary = { to: LINKS.demo, label: "Book a Demo" }, source = "hero", visual }: { eyebrow?: string; title: ReactNode; lead: ReactNode; image?: ImageKey; children?: ReactNode; primary?: { to: string; label: string }; secondary?: { to: string; label: string } | null; source?: string; visual?: ReactNode }) {
  return (
    <section className="bv-hero-bg">
      <Container className="grid items-center gap-10 py-16 sm:py-20 lg:grid-cols-12 lg:py-24">
        <div className="lg:col-span-6">
          {eyebrow && <Eyebrow>{eyebrow}</Eyebrow>}
          <Heading as="h1">{title}</Heading>
          <Lead>{lead}</Lead>
          <div className="mt-8 flex flex-wrap gap-3">
            <ButtonLink to={primary.to} size="lg" icon="arrow_forward" onClick={() => primary.to.includes("/signup") && track("start_trial_clicked", { placement: source })}>{primary.label}</ButtonLink>
            {secondary && <ButtonLink to={secondary.to} variant="secondary" size="lg" onClick={() => secondary.to === LINKS.demo && track("book_demo_clicked", { placement: source })}>{secondary.label}</ButtonLink>}
          </div>
          {children}
        </div>
        <div className="lg:col-span-6">{visual ?? (image && <Picture image={image} priority sizes="(min-width: 1024px) 560px, 100vw" />)}</div>
      </Container>
    </section>
  );
}

export function FeatureGrid({ eyebrow, title, lead, items, cols = 3, tone }: { eyebrow?: string; title: ReactNode; lead?: ReactNode; items: Feature[]; cols?: 2 | 3 | 4; tone?: "muted" }) {
  return (
    <Section tone={tone}>
      <div className="max-w-2xl">{eyebrow && <Eyebrow>{eyebrow}</Eyebrow>}<Heading>{title}</Heading>{lead && <Lead>{lead}</Lead>}</div>
      <div className={cn("mt-10 grid gap-5", cols === 2 && "md:grid-cols-2", cols === 3 && "md:grid-cols-2 lg:grid-cols-3", cols === 4 && "sm:grid-cols-2 lg:grid-cols-4")}>
        {items.map((f) => {
          const inner = (
            <>
              <div className="mb-4 inline-flex h-11 w-11 items-center justify-center rounded-[var(--radius-lg)] bg-primary-soft text-primary"><Icon name={f.icon} size={24} /></div>
              <h3 className="text-base font-bold text-on-surface">{f.title}</h3>
              <p className="mt-1.5 text-sm leading-relaxed text-on-surface-variant">{f.body}</p>
              {f.to && <span className="mt-3 inline-flex items-center gap-1 text-sm font-semibold text-primary">Learn more <Icon name="arrow_forward" size={16} /></span>}
            </>
          );
          return f.to ? <Link key={f.title} to={f.to} className="rounded-[var(--radius-xl)] border border-border bg-surface p-6 transition-colors hover:border-primary/40 hover:bg-primary-soft/40">{inner}</Link> : <Card key={f.title}>{inner}</Card>;
        })}
      </div>
    </Section>
  );
}

export function Steps({ eyebrow, title, lead, steps, image, tone }: { eyebrow?: string; title: ReactNode; lead?: ReactNode; steps: Step[]; image?: ImageKey; tone?: "muted" }) {
  return (
    <Section tone={tone}>
      <div className="grid items-center gap-10 lg:grid-cols-12">
        <div className="lg:col-span-6">
          {eyebrow && <Eyebrow>{eyebrow}</Eyebrow>}<Heading>{title}</Heading>{lead && <Lead>{lead}</Lead>}
          <ol className="mt-8 space-y-5">
            {steps.map((s, i) => (
              <li key={s.title} className="flex gap-4">
                <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-primary text-sm font-bold text-on-primary">{i + 1}</span>
                <div><div className="font-bold text-on-surface">{s.title}</div><p className="mt-0.5 text-sm text-on-surface-variant">{s.body}</p></div>
              </li>
            ))}
          </ol>
        </div>
        {image && <div className="lg:col-span-6"><Picture image={image} sizes="(min-width: 1024px) 560px, 100vw" /></div>}
      </div>
    </Section>
  );
}

export function ImageSplit({ eyebrow, title, lead, bullets, image, reverse, cta, children, tone, visual }: { eyebrow?: string; title: ReactNode; lead?: ReactNode; bullets?: string[]; image?: ImageKey; reverse?: boolean; cta?: { to: string; label: string }; children?: ReactNode; tone?: "muted"; visual?: ReactNode }) {
  return (
    <Section tone={tone}>
      <div className="grid items-center gap-10 lg:grid-cols-12">
        <div className={cn("lg:col-span-6", reverse && "lg:order-2")}>
          {eyebrow && <Eyebrow>{eyebrow}</Eyebrow>}<Heading>{title}</Heading>{lead && <Lead>{lead}</Lead>}
          {bullets && <ul className="mt-6 space-y-2.5">{bullets.map((b) => <Check key={b}>{b}</Check>)}</ul>}
          {children}
          {cta && <div className="mt-6"><ButtonLink to={cta.to} variant="secondary" icon="arrow_forward">{cta.label}</ButtonLink></div>}
        </div>
        <div className={cn("lg:col-span-6", reverse && "lg:order-1")}>{visual ?? (image && <Picture image={image} sizes="(min-width: 1024px) 560px, 100vw" />)}</div>
      </div>
    </Section>
  );
}

export function FaqList({ items, title = "Frequently asked questions", tone }: { items: Faq[]; title?: string; tone?: "muted" }) {
  return (
    <Section tone={tone}>
      <div className="grid gap-10 lg:grid-cols-12">
        <div className="lg:col-span-4"><Eyebrow>FAQ</Eyebrow><Heading>{title}</Heading><Lead>Still curious? <Link to="/book-a-demo" className="text-primary underline">Talk to us</Link> and we will walk you through it.</Lead></div>
        <div className="lg:col-span-8">
          {items.map((f) => (
            <details key={f.q} className="group border-b border-border py-4">
              <summary className="flex cursor-pointer list-none items-center justify-between gap-4 text-base font-semibold text-on-surface">
                {f.q}<Icon name="expand_more" size={22} className="shrink-0 text-on-surface-variant transition-transform group-open:rotate-180" />
              </summary>
              <p className="mt-2 max-w-2xl text-sm leading-relaxed text-on-surface-variant">{f.a}</p>
            </details>
          ))}
        </div>
      </div>
    </Section>
  );
}

export function CtaBand({ title = "See your building the way your team sees it", lead = "Start a 14-day free trial. No credit card required, and setup takes minutes.", source = "final_cta" }: { title?: string; lead?: string; source?: string }) {
  return (
    <Section tone="band">
      <div className="flex flex-col items-start gap-6 lg:flex-row lg:items-center lg:justify-between">
        <div><Heading inverse>{title}</Heading><Lead inverse>{lead}</Lead></div>
        <div className="flex flex-wrap gap-3">
          <ButtonLink to={signupHref(source)} variant="inverse" size="lg" icon="arrow_forward" onClick={() => track("start_trial_clicked", { placement: source })}>Start Free Trial</ButtonLink>
          <ButtonLink to={LINKS.demo} variant="ghost" size="lg" className="text-on-primary hover:bg-on-primary/10" onClick={() => track("book_demo_clicked", { placement: source })}>Book a Demo</ButtonLink>
        </div>
      </div>
    </Section>
  );
}

export function StatRow({ items, tone }: { items: { value: string; label: string }[]; tone?: "muted" }) {
  return (
    <Section tone={tone} className="py-10 sm:py-12">
      <dl className="grid gap-6 sm:grid-cols-2 lg:grid-cols-4">
        {items.map((s) => <div key={s.label}><dt className="text-3xl font-extrabold text-primary">{s.value}</dt><dd className="mt-1 text-sm text-on-surface-variant">{s.label}</dd></div>)}
      </dl>
    </Section>
  );
}

export function Quote({ quote, name, role, tone }: { quote: string; name: string; role: string; tone?: "muted" }) {
  return (
    <Section tone={tone}>
      <figure className="mx-auto max-w-3xl text-center">
        <Icon name="format_quote" size={40} className="text-primary" />
        <blockquote className="mt-2 text-xl font-semibold leading-relaxed text-on-surface sm:text-2xl">{quote}</blockquote>
        <figcaption className="mt-5 text-sm text-on-surface-variant"><span className="font-semibold text-on-surface">{name}</span>, {role}</figcaption>
      </figure>
    </Section>
  );
}

// ---------- Download Apps (Website PRD §13–§14, §22–§23, §47) ----------
const APP_COPY = {
  staff: { title: "Staff App", who: "For building staff and operational teams.", icon: "engineering", points: ["Tasks, work orders, and checklists", "QR checkpoint scans and photo evidence", "Works offline and syncs when back online"] },
  tenant: { title: "Tenant App", who: "For tenants, residents, and guests.", icon: "apartment", points: ["Report issues and follow progress", "Book facilities and register visitors", "View bills and announcements"] },
} as const;

export function useApps() {
  const [state, setState] = useState<{ apps: PublicApp[] | null; error: boolean; loading: boolean }>({ apps: null, error: false, loading: true });
  useEffect(() => {
    let alive = true;
    fetchApps().then((apps) => alive && setState({ apps, error: false, loading: false })).catch(() => alive && setState({ apps: null, error: true, loading: false }));
    return () => {
      alive = false;
    };
  }, []);
  return state;
}

export function DownloadCards({ apps, loading, error, compact }: { apps: PublicApp[] | null; loading: boolean; error: boolean; compact?: boolean }) {
  const platformLabel = (p: PublicApp["platform"]) => (p === "android" ? "Android" : p === "ios" ? "iOS" : "Other");
  return (
    <div className="grid gap-5 md:grid-cols-2">
      {(["staff", "tenant"] as const).map((type) => {
        const c = APP_COPY[type];
        const links = (apps ?? []).filter((a) => a.app_type === type);
        return (
          <Card key={type} className="flex flex-col">
            <div className="flex items-center gap-3">
              <div className="inline-flex h-12 w-12 items-center justify-center rounded-[var(--radius-lg)] bg-primary text-on-primary"><Icon name={c.icon} size={26} /></div>
              <div><h3 className="text-lg font-bold text-on-surface">BuildingVision {c.title}</h3><p className="text-sm text-on-surface-variant">{c.who}</p></div>
            </div>
            {!compact && <ul className="mt-4 space-y-2">{c.points.map((p) => <Check key={p}>{p}</Check>)}</ul>}
            <div className="mt-5 flex flex-wrap gap-2">
              {loading && <span className="inline-flex h-11 items-center rounded-[var(--radius-pill)] bg-surface-container px-5 text-sm text-on-surface-variant">Checking availability</span>}
              {!loading && links.length > 0 && links.map((a) => (
                <a key={a.platform + a.downloadUrl} href={a.downloadUrl} target="_blank" rel="noopener noreferrer" className="inline-flex h-11 items-center gap-2 rounded-[var(--radius-pill)] bg-primary px-5 text-sm font-semibold text-on-primary hover:bg-primary-strong" onClick={() => track("app_download_clicked", { app: type, platform: a.platform })}>
                  <Icon name={a.platform === "ios" ? "phone_iphone" : "android"} size={20} /> Download for {platformLabel(a.platform)}
                </a>
              ))}
              {!loading && links.length === 0 && (
                <span className="inline-flex min-h-11 items-center rounded-[var(--radius-pill)] bg-surface-container px-5 py-2 text-sm text-on-surface-variant">
                  {error ? "Downloads are temporarily unavailable. Please try again in a few minutes." : "The app is currently unavailable for download."}
                </span>
              )}
            </div>
          </Card>
        );
      })}
    </div>
  );
}

export function OnTheGo() {
  return (
    <Section tone="muted">
      <div className="grid items-center gap-10 lg:grid-cols-12">
        <div className="lg:col-span-5">
          <Eyebrow>Mobile</Eyebrow>
          <Heading>BuildingVision on the Go</Heading>
          <Lead>Keep your building operations moving wherever your team works. The Staff App runs offline in basements and plant rooms; the Tenant App keeps residents and guests in the loop.</Lead>
          <div className="mt-6"><ButtonLink to="/download" icon="download" trailing={false}>Download Apps</ButtonLink></div>
        </div>
        <div className="lg:col-span-7"><Picture image="devices-table" sizes="(min-width: 1024px) 640px, 100vw" /></div>
      </div>
    </Section>
  );
}
