// /resources + Documentation, Help Center, Guides, Blog (Website PRD §5 "Resources structure"). Konten awal: struktur + artikel ringkas.
import { Link, useParams, Navigate } from "react-router-dom";
import { Seo } from "@/lib/head";
import { CtaBand, Hero } from "@/components/blocks";
import { Picture } from "@/components/Picture";
import { Icon } from "@/components/Icon";
import { Card, Eyebrow, Heading, Lead, Section } from "@/components/ui";
import { RESOURCES } from "@/content/site";
import { LINKS, SUPPORT_EMAIL } from "@/lib/config";

interface Article { title: string; body: string; tag?: string }
const CONTENT: Record<string, { title: string; description: string; intro: string; items: Article[] }> = {
  documentation: {
    title: "Documentation",
    description: "How to set up BuildingVision: organization, properties and profiles, locations, roles, checklists, SLA policies, and the apps.",
    intro: "Everything you need to set up and configure BuildingVision, written for the person doing the setup.",
    items: [
      { tag: "Getting started", title: "Create your organization and first property", body: "Sign up, confirm your email, choose a profile (Hotel, Apartment, Office), and create the property. Timezone and address matter for SLA and reports." },
      { tag: "Getting started", title: "Locations: buildings, towers, floors, areas, units", body: "Model the places your team works in. Areas carry QR codes for patrols and cleaning; units link tenants and residents." },
      { tag: "People", title: "Users, roles, and teams", body: "Invite staff, assign a role per property or organization-wide, and group people into teams so work can be assigned to a team." },
      { tag: "Operations", title: "Checklists and SLA policies", body: "Build versioned checklist templates per domain and set response and resolution targets per object and priority." },
      { tag: "Engineering", title: "Assets and preventive maintenance", body: "Register assets, print QR labels, publish maintenance plans, and let the scheduler create work orders ahead of time." },
      { tag: "Apps", title: "Staff App and Tenant App", body: "How to distribute the apps, how offline sync works, and how tenants register and get validated." },
      { tag: "Admin", title: "App Downloads (internal)", body: "How the BuildingVision team publishes app builds through the dashboard without redeploying the website." },
    ],
  },
  "help-center": {
    title: "Help Center",
    description: "Answers to everyday questions about BuildingVision: logging in, requests, work orders, the Staff App, and the Tenant App.",
    intro: "Short answers for everyday questions. If you cannot find it here, write to support and a real person will reply.",
    items: [
      { tag: "Account", title: "I did not receive the verification email", body: "Check spam, then use the resend link on the sign up page. Verification links are valid for 24 hours." },
      { tag: "Account", title: "My trial ended. Is my data gone?", body: "No. The workspace becomes read-only. Choose a plan from Settings to continue exactly where you left off." },
      { tag: "Work", title: "Why can I not close this work order?", body: "Required evidence is missing, or the work order is not in a state that allows closing. The available actions come from the server, so check the checklist and photos first." },
      { tag: "Staff App", title: "The app says offline. Will I lose my work?", body: "No. Changes queue on the device with a timestamp and upload when you are back online. Conflicts are resolved by clear rules and evidence is always kept." },
      { tag: "Tenant App", title: "A tenant registered but cannot log in", body: "Their account is waiting for validation. Tenant Relation approves it from the dashboard, and the tenant gets a notification." },
      { tag: "Download", title: "Where do I get the apps?", body: "From the Download Apps page. The links are managed centrally and always point to the current build." },
    ],
  },
  guides: {
    title: "Guides",
    description: "Practical playbooks for property teams: rolling out a Staff App, running preventive maintenance, and proving service levels to tenants.",
    intro: "Practical playbooks from teams who run buildings every day.",
    items: [
      { tag: "Playbook", title: "Rolling out the Staff App in a week", body: "Start with one team and one workflow. Cleaning schedules with required photos are a good first win: the change is visible to everyone within days." },
      { tag: "Playbook", title: "From reactive to preventive maintenance", body: "Register your ten most critical assets first, publish monthly plans, and let the scheduler create the work. Review the asset history after 60 days." },
      { tag: "Playbook", title: "Proving SLAs to office tenants", body: "Map request categories to priorities, set business hours, and share the monthly SLA report. Tenants notice consistency more than speed." },
      { tag: "Playbook", title: "Hotel turnover without the radio", body: "Connect check-out to housekeeping. Turnover tasks are created automatically and the room board replaces the call to the desk." },
      { tag: "Playbook", title: "Onboarding residents to the Tenant App", body: "Announce it, put a QR code in the lobby, validate accounts within a day, and answer the first requests fast. Adoption follows response time." },
    ],
  },
  blog: {
    title: "Blog",
    description: "Product updates and notes on building operations from the BuildingVision team.",
    intro: "Product updates and notes from the team.",
    items: [
      { tag: "Product", title: "Introducing property profiles: Hotel, Apartment, Office", body: "One platform, three operational contexts. Why we chose profiles per property instead of separate products, and what changes when you switch." },
      { tag: "Product", title: "Self-serve trial and the onboarding checklist", body: "You can now start a trial from the website, confirm your email, and be in the dashboard in minutes. No wizard, just a checklist that follows your progress." },
      { tag: "Product", title: "App downloads, managed from the dashboard", body: "App builds are now distributed through a link the BuildingVision team manages centrally, with an audit trail and no website redeploys." },
      { tag: "Operations", title: "Why photo evidence changes team behavior", body: "It is not about surveillance. A required after-photo makes the standard visible, and teams start holding it themselves." },
    ],
  },
};

export function ResourcesIndexPage() {
  return (
    <>
      <Seo title="Resources | BuildingVision" description="Documentation, help center, guides, and blog for BuildingVision users and property teams." path="/resources" image="/images/office-workers-1440.webp" />
      <Hero eyebrow="Resources" title="Learn the product, run the building" lead="Setup documentation, everyday answers, practical guides, and product updates." image="office-workers" source="resources" secondary={null} primary={{ to: "/resources/documentation", label: "Open documentation" }} />
      <Section tone="muted">
        <div className="grid gap-5 md:grid-cols-2 lg:grid-cols-4">
          {RESOURCES.map((r) => (
            <Link key={r.to} to={r.to} className="rounded-[var(--radius-xl)] border border-border bg-surface p-6 hover:border-primary/40"><Icon name={r.icon ?? "article"} size={28} className="text-primary" /><div className="mt-3 text-lg font-bold text-on-surface">{r.label}</div><p className="mt-1 text-sm text-on-surface-variant">{r.blurb}</p></Link>
          ))}
        </div>
      </Section>
      <Section>
        <div className="grid items-center gap-10 lg:grid-cols-12">
          <div className="lg:col-span-6"><Picture image="boardroom" sizes="(min-width: 1024px) 560px, 100vw" /></div>
          <div className="lg:col-span-6"><Eyebrow>Need a person?</Eyebrow><Heading>Support that knows buildings</Heading><Lead>Write to <a className="text-primary underline" href={`mailto:${SUPPORT_EMAIL}`}>{SUPPORT_EMAIL}</a> or <Link className="text-primary underline" to={LINKS.demo}>book a session</Link> with someone who has run operations before.</Lead></div>
        </div>
      </Section>
      <CtaBand source="resources_cta" />
    </>
  );
}

export default function ResourcePage() {
  const { section } = useParams();
  const c = section ? CONTENT[section] : undefined;
  if (!c || !section) return <Navigate to="/404" replace />;
  return (
    <>
      <Seo title={`${c.title} | BuildingVision`} description={c.description} path={`/resources/${section}`} type="article" />
      <section className="bv-hero-bg"><div className="mx-auto max-w-[1200px] px-5 py-14 sm:px-8 sm:py-20"><Eyebrow>Resources · {c.title}</Eyebrow><Heading as="h1">{c.title}</Heading><Lead>{c.intro}</Lead></div></section>
      <Section className="pt-0 sm:pt-0">
        <div className="grid gap-10 lg:grid-cols-12">
          <aside className="lg:col-span-3">
            <div className="mb-2 text-xs font-bold uppercase tracking-[0.14em] text-primary">Browse</div>
            <ul className="space-y-1">{RESOURCES.map((r) => <li key={r.to}><Link to={r.to} className={`block rounded-[var(--radius-lg)] px-3 py-2 text-sm ${r.to.endsWith(section) ? "bg-primary-soft font-semibold text-primary" : "text-on-surface hover:bg-surface-container-low"}`}>{r.label}</Link></li>)}</ul>
          </aside>
          <div className="grid gap-4 lg:col-span-9">
            {c.items.map((a) => (
              <Card key={a.title}>
                {a.tag && <div className="mb-1 text-xs font-semibold uppercase tracking-wider text-primary">{a.tag}</div>}
                <h2 className="text-lg font-bold text-on-surface">{a.title}</h2>
                <p className="mt-1.5 text-sm leading-relaxed text-on-surface-variant">{a.body}</p>
              </Card>
            ))}
          </div>
        </div>
      </Section>
      <CtaBand source={`resources_${section}_cta`} />
    </>
  );
}
