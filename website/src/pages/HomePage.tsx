// Homepage (Website PRD §6–§7): Hero → Built for Your Property → Operational Problems → Solution → Core Operations →
// Dashboard + Mobile Staff + Tenant App → How it works → Product Screens → Industry Solutions → Social Proof → Pricing →
// Security & Trust → FAQ → Download Apps → Final CTA.
import { Link } from "react-router-dom";
import { Seo } from "@/lib/head";
import { CtaBand, FaqList, FeatureGrid, Hero, ImageSplit, OnTheGo, Quote, StatRow, Steps } from "@/components/blocks";
import { DashboardMock, PhoneMock } from "@/components/mocks";
import { Picture } from "@/components/Picture";
import { Icon } from "@/components/Icon";
import { ButtonLink, Card, Check, Eyebrow, Heading, Lead, Section, cn } from "@/components/ui";
import { SOLUTIONS, PLATFORM } from "@/content/site";
import { HOME_FAQ } from "@/content/faq";
import { SITE_URL, TRIAL_DAYS, signupHref, SHOW_PRICING } from "@/lib/config";
import { track } from "@/lib/analytics";
import { PlansPreview } from "@/components/PlansPreview";

const PROBLEMS = [
  { icon: "chat", title: "Requests scattered across chats and calls", body: "Tenant and guest issues arrive in WhatsApp groups, emails, and radio calls. They get answered by whoever is nearest, and nobody sees the whole queue." },
  { icon: "visibility_off", title: "Work that cannot be proven", body: "Cleaning done, patrol completed, filter replaced. Without time, place, and a photo, it is a claim, not a record." },
  { icon: "event_busy", title: "Maintenance that waits for failure", body: "Preventive schedules live in one engineer's spreadsheet. The lift is serviced when it stops." },
  { icon: "group_off", title: "Field teams working blind", body: "Staff in basements and plant rooms have no signal, no list, and no way to report what they found." },
];

export default function HomePage() {
  return (
    <>
      <Seo
        title="BuildingVision: Building Operations Platform for Hotels, Apartments, and Offices"
        description="Housekeeping, engineering, security, and tenant relation on one building operations platform. Work orders, staff app, tenant app. Start a free 14-day trial."
        path="/"
        jsonLd={[
          { "@context": "https://schema.org", "@type": "Organization", name: "BuildingVision", url: SITE_URL, logo: `${SITE_URL}/favicon.svg` },
          { "@context": "https://schema.org", "@type": "SoftwareApplication", name: "BuildingVision", applicationCategory: "BusinessApplication", operatingSystem: "Web, Android", offers: { "@type": "Offer", price: "0", priceCurrency: "IDR", description: `${TRIAL_DAYS}-day free trial` } },
        ]}
      />
      <Hero
        eyebrow="Building Operations Platform"
        title={<>Run your building the way your team actually works</>}
        lead="BuildingVision brings housekeeping, engineering, security, and tenant relation into one operational picture. Requests become work orders, work orders get done with evidence, and everyone sees the same list."
        visual={<DashboardMock />}
      >
        <div className="mt-6 flex flex-wrap items-center gap-x-6 gap-y-2 text-sm text-on-surface-variant">
          <span className="inline-flex items-center gap-1.5"><Icon name="check_circle" size={18} className="text-primary" /> {TRIAL_DAYS}-day free trial</span>
          <span className="inline-flex items-center gap-1.5"><Icon name="check_circle" size={18} className="text-primary" /> No credit card required</span>
          <Link to="/download" className="inline-flex items-center gap-1.5 font-semibold text-primary"><Icon name="download" size={18} /> Download Apps</Link>
        </div>
      </Hero>

      {/* Built for Your Property */}
      <Section>
        <div className="max-w-2xl"><Eyebrow>Built for your property</Eyebrow><Heading>One platform, configured for how your property operates</Heading><Lead>Choose a profile when you create a property. Terminology, workflows, and modules adjust. Same platform, same team, same data model.</Lead></div>
        <div className="mt-10 grid gap-5 md:grid-cols-3">
          {SOLUTIONS.map((s) => (
            <Link key={s.to} to={s.to} className="group overflow-hidden rounded-[var(--radius-xl)] border border-border bg-surface transition-colors hover:border-primary/40" onClick={() => track("solution_viewed", { solution: s.label.toLowerCase(), placement: "home" })}>
              <Picture image={s.label === "Hotel" ? "hotel-exterior" : s.label === "Apartment" ? "apartment-exterior" : "office-window"} rounded="rounded-none" aspect="16/9" sizes="(min-width: 768px) 33vw, 100vw" />
              <div className="p-6">
                <div className="flex items-center gap-2 text-lg font-bold text-on-surface"><Icon name={s.icon ?? "domain"} size={22} className="text-primary" />{s.label}</div>
                <p className="mt-1 text-sm text-on-surface-variant">{s.blurb}</p>
                <span className="mt-3 inline-flex items-center gap-1 text-sm font-semibold text-primary">See the {s.label} solution <Icon name="arrow_forward" size={16} /></span>
              </div>
            </Link>
          ))}
        </div>
      </Section>

      {/* Operational Problems */}
      <Section tone="muted">
        <div className="max-w-2xl"><Eyebrow>The problem</Eyebrow><Heading>Buildings run on people. The tools usually do not help them.</Heading></div>
        <div className="mt-10 grid gap-5 md:grid-cols-2">
          {PROBLEMS.map((p) => (
            <Card key={p.title} className="flex gap-4">
              <div className="inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-[var(--radius-lg)] bg-error-container text-on-error-container"><Icon name={p.icon} size={24} /></div>
              <div><h3 className="font-bold text-on-surface">{p.title}</h3><p className="mt-1 text-sm text-on-surface-variant">{p.body}</p></div>
            </Card>
          ))}
        </div>
      </Section>

      {/* BuildingVision Solution */}
      <ImageSplit
        eyebrow="The BuildingVision way"
        title="Request to resolution, with a record at every step"
        lead="Every issue becomes a request. Every request becomes work someone owns. Every job closes with evidence. The dashboard, the Staff App, and the Tenant App all look at the same data."
        bullets={["Requests from tenants, guests, the front desk, or public forms", "Work orders and tasks with priority, due time, and checklist", "Photo evidence with time and location, even offline", "SLA tracking, escalation, and reports without exports"]}
        image="team-meeting"
        cta={{ to: "/platform/work-orders", label: "How work orders flow" }}
      />

      {/* Core Operations */}
      <FeatureGrid
        eyebrow="Core operations"
        title="Every team, on the same platform"
        lead="Modules for each domain, connected by one work engine."
        tone="muted"
        items={PLATFORM.slice(0, 7).map((p) => ({ icon: p.icon ?? "circle", title: p.label, body: p.blurb ?? "", to: p.to }))}
        cols={4}
      />

      {/* Dashboard + Mobile Staff + Tenant App */}
      <Section>
        <div className="max-w-2xl"><Eyebrow>Three apps, one platform</Eyebrow><Heading>Dashboard for managers. Staff App for the field. Tenant App for the people you serve.</Heading></div>
        <div className="mt-10 grid gap-8 lg:grid-cols-12 lg:items-end">
          <div className="lg:col-span-7">
            <DashboardMock compact />
            <div className="mt-4"><h3 className="font-bold text-on-surface">Web Dashboard</h3><p className="text-sm text-on-surface-variant">Overview, operations, settings, and reports for property managers, supervisors, and back office.</p></div>
          </div>
          <div className="grid grid-cols-2 gap-4 lg:col-span-5">
            <div>
              <PhoneMock variant="staff" className="w-full max-w-[220px]" />
              <h3 className="mt-3 font-bold text-on-surface">Staff App</h3>
              <p className="text-sm text-on-surface-variant">Tasks, scans, photos. Works offline.</p>
              <Link to="/platform/mobile-staff" className="mt-1 inline-flex items-center gap-1 text-sm font-semibold text-primary">Learn more <Icon name="arrow_forward" size={16} /></Link>
            </div>
            <div>
              <PhoneMock variant="tenant" className="w-full max-w-[220px]" />
              <h3 className="mt-3 font-bold text-on-surface">Tenant App</h3>
              <p className="text-sm text-on-surface-variant">Requests, bookings, visitors, bills.</p>
              <Link to="/platform/tenant-app" className="mt-1 inline-flex items-center gap-1 text-sm font-semibold text-primary">Learn more <Icon name="arrow_forward" size={16} /></Link>
            </div>
          </div>
        </div>
      </Section>

      {/* How BuildingVision Works */}
      <Steps
        eyebrow="How it works"
        title="From sign up to your first completed work order in a day"
        lead="No long setup wizard. Create your property, and the checklist guides you through the rest while you use the product."
        tone="muted"
        steps={[
          { title: "Start a free trial", body: "Create an account, confirm your email, and your workspace is ready." },
          { title: "Pick a profile and create your property", body: "Hotel, Apartment, or Office. Add buildings and areas as you go, or load a sample property." },
          { title: "Invite your team", body: "Supervisors on the dashboard, field staff on the Staff App, tenants on the Tenant App." },
          { title: "Run one request end to end", body: "Request, work order, completion, photo. That is the moment it clicks." },
        ]}
        image="office-team"
      />

      {/* Product Screens */}
      <Section>
        <div className="max-w-2xl"><Eyebrow>Product screens</Eyebrow><Heading>Clear, calm, and built for daily use</Heading><Lead>The same design system across the dashboard and both apps. Status colors mean the same thing everywhere.</Lead></div>
        <div className="mt-10 grid gap-6 lg:grid-cols-3">
          {[
            { title: "Overview", body: "Today's counters, attention list, building state." },
            { title: "Work order detail", body: "Checklist, evidence, activity timeline, linked items." },
            { title: "Reports", body: "SLA, maintenance, patrol, facilities, billing." },
          ].map((s, i) => (
            <div key={s.title} className={cn(i === 0 && "lg:col-span-2")}>
              <DashboardMock compact={i !== 0} />
              <h3 className="mt-3 font-bold text-on-surface">{s.title}</h3>
              <p className="text-sm text-on-surface-variant">{s.body}</p>
            </div>
          ))}
        </div>
      </Section>

      {/* Industry Solutions */}
      <Section tone="muted">
        <div className="grid gap-10 lg:grid-cols-12 lg:items-center">
          <div className="lg:col-span-5">
            <Eyebrow>Industry solutions</Eyebrow><Heading>Hotel, apartment, or office. Pick the one that fits.</Heading>
            <Lead>Each solution page shows how the same platform handles your operation, from guest turnover to tenant SLAs.</Lead>
            <ul className="mt-6 space-y-2.5">
              <Check>Profile-specific terminology: guest, resident, or tenant</Check>
              <Check>Modules switched on per profile, like Hotel Booking or Unit Rental</Check>
              <Check>Multiple properties with different profiles in one organization</Check>
            </ul>
          </div>
          <div className="grid gap-4 sm:grid-cols-3 lg:col-span-7">
            {SOLUTIONS.map((s) => (
              <Link key={s.to} to={s.to} className="rounded-[var(--radius-xl)] border border-border bg-surface p-5 hover:border-primary/40" onClick={() => track("solution_viewed", { solution: s.label.toLowerCase(), placement: "home_industry" })}>
                <Icon name={s.icon ?? "domain"} size={28} className="text-primary" />
                <div className="mt-3 font-bold text-on-surface">{s.label}</div>
                <p className="mt-1 text-xs text-on-surface-variant">{s.blurb}</p>
              </Link>
            ))}
          </div>
        </div>
      </Section>

      {/* Social proof */}
      <StatRow items={[{ value: "3", label: "Property profiles on one platform" }, { value: "9", label: "Operational modules" }, { value: "Offline", label: "Staff App keeps working without signal" }, { value: "14 days", label: "Free trial, no credit card" }]} />
      <Quote quote="We stopped asking 'did anyone check?'. The answer is on the timeline, with a photo and a time." name="Property operations lead" role="pilot customer, mixed-use building" tone="muted" />

      {/* Pricing (disembunyikan sampai harga final, lihat SHOW_PRICING) */}
      {SHOW_PRICING && (
      <Section>
        <div className="max-w-2xl"><Eyebrow>Pricing</Eyebrow><Heading>Simple plans per property</Heading><Lead>Start with a free trial. Choose a plan when your team is ready. No surprises.</Lead></div>
        <div className="mt-10"><PlansPreview /></div>
        <div className="mt-6"><ButtonLink to="/pricing" variant="secondary" icon="arrow_forward">See full pricing</ButtonLink></div>
      </Section>
      )}

      {/* Security & Trust */}
      <Section tone="muted">
        <div className="grid gap-10 lg:grid-cols-12 lg:items-center">
          <div className="lg:col-span-6"><Picture image="modern-building" sizes="(min-width: 1024px) 560px, 100vw" /></div>
          <div className="lg:col-span-6">
            <Eyebrow>Security and trust</Eyebrow><Heading>Your building's data, kept where it belongs</Heading>
            <Lead>Organization and property isolation at the database level, role-based access down to a single property, and an audit trail for every administrative change.</Lead>
            <ul className="mt-6 space-y-2.5">
              <Check>Row-level isolation between organizations</Check>
              <Check>Roles and permissions per property</Check>
              <Check>Audit log with before and after values</Check>
              <Check>Encrypted transport, signed uploads, verified callbacks</Check>
            </ul>
            <div className="mt-6"><ButtonLink to="/security" variant="secondary" icon="arrow_forward">Read about security</ButtonLink></div>
          </div>
        </div>
      </Section>

      <FaqList items={HOME_FAQ} />
      <OnTheGo />
      <CtaBand />
      <div className="sr-only"><a href={signupHref("sr")}>Start free trial</a></div>
    </>
  );
}
