// /platform and /platform/{slug} (Website PRD §12): Problem → How BuildingVision addresses it → Main workflow → Screens → Capabilities → CTA
import { Link, Navigate, useParams } from "react-router-dom";
import { Seo } from "@/lib/head";
import { CtaBand, FeatureGrid, Hero, ImageSplit, Steps } from "@/components/blocks";
import { DashboardMock, PhoneMock } from "@/components/mocks";
import { Icon } from "@/components/Icon";
import { ButtonLink, Card, Eyebrow, Heading, Lead, Section } from "@/components/ui";
import { PLATFORM_CONTENT, platformBySlug } from "@/content/platform";
import { PLATFORM } from "@/content/site";

export function PlatformIndexPage() {
  return (
    <>
      <Seo title="Platform: Building Operations Modules | BuildingVision" description="Property operations, housekeeping, engineering, security, tenant relation, work orders, asset management, Staff App, and Tenant App on one platform." path="/platform" />
      <Hero eyebrow="Platform" title="Every module, one work engine" lead="Each part of BuildingVision solves a real operational problem, and all of them share the same requests, work orders, evidence, and reports." image="laptop-dashboard" source="platform_index" />
      <FeatureGrid title="Explore the platform" items={PLATFORM.map((p) => ({ icon: p.icon ?? "circle", title: p.label, body: p.blurb ?? "", to: p.to }))} cols={3} tone="muted" />
      <CtaBand source="platform_index_cta" />
    </>
  );
}

export default function PlatformPage() {
  const { slug } = useParams();
  const p = slug ? platformBySlug(slug) : undefined;
  if (!p) return <Navigate to="/404" replace />;
  const idx = PLATFORM_CONTENT.findIndex((x) => x.slug === p.slug);
  const prev = PLATFORM_CONTENT[(idx + PLATFORM_CONTENT.length - 1) % PLATFORM_CONTENT.length];
  const next = PLATFORM_CONTENT[(idx + 1) % PLATFORM_CONTENT.length];
  const mock = p.mock === "dashboard" ? <DashboardMock /> : p.mock === "staff" ? <PhoneMock variant="staff" /> : p.mock === "tenant" ? <PhoneMock variant="tenant" /> : undefined;
  return (
    <>
      <Seo title={`${p.seoTitle} | BuildingVision`} description={p.seoDescription} path={`/platform/${p.slug}`} image={`/images/${p.hero}-1440.webp`} />
      <Hero eyebrow={`Platform · ${p.name}`} title={p.title} lead={p.lead} image={p.hero} source={`platform_${p.slug}`} secondary={p.downloadCta ? { to: p.downloadCta.to, label: p.downloadCta.label } : { to: "/book-a-demo", label: "Book a Demo" }} />

      <Section tone="muted">
        <div className="grid gap-8 lg:grid-cols-12">
          <div className="lg:col-span-5">
            <Eyebrow>The problem</Eyebrow><Heading as="h3" className="text-[26px] sm:text-[30px]">{p.problem.title}</Heading>
            <Lead>{p.problem.body}</Lead>
          </div>
          <div className="lg:col-span-7">
            <Card className="h-full border-primary/30 bg-primary-soft/40">
              <Eyebrow>How BuildingVision addresses it</Eyebrow>
              <h3 className="text-xl font-bold text-on-surface">{p.solution.title}</h3>
              <p className="mt-2 text-sm leading-relaxed text-on-surface-variant">{p.solution.body}</p>
              <ul className="mt-4 grid gap-2 sm:grid-cols-2">
                {p.solution.bullets.map((b) => <li key={b} className="flex items-start gap-2 text-sm text-on-surface"><Icon name="check_circle" size={18} className="mt-0.5 shrink-0 text-primary" />{b}</li>)}
              </ul>
            </Card>
          </div>
        </div>
      </Section>

      <Steps eyebrow="Main workflow" title="How it works, step by step" steps={p.workflow} image={p.solution.image} />

      {mock && (
        <Section tone="muted">
          <div className="grid items-center gap-10 lg:grid-cols-12">
            <div className="lg:col-span-5"><Eyebrow>Product screen</Eyebrow><Heading>What your team sees</Heading><Lead>{p.mock === "dashboard" ? "The dashboard view your managers and supervisors work from." : p.mock === "staff" ? "The Staff App view your field team uses on site." : "The Tenant App view your tenants, residents, or guests use."}</Lead>
              {p.downloadCta && <div className="mt-6"><ButtonLink to={p.downloadCta.to} icon="download" trailing={false}>{p.downloadCta.label}</ButtonLink></div>}
            </div>
            <div className="lg:col-span-7">{mock}</div>
          </div>
        </Section>
      )}

      <FeatureGrid eyebrow="Key capabilities" title={`${p.name} at a glance`} items={p.capabilities} cols={3} />

      <ImageSplit eyebrow="In context" title="Built for real environments" lead="BuildingVision is used in plant rooms, corridors, lobbies, and gates, not just at a desk. The apps and the dashboard are designed for that." image={p.hero} reverse tone="muted">
        <div className="mt-6 flex gap-3">
          <Link to={`/platform/${prev.slug}`} className="inline-flex items-center gap-1 text-sm font-semibold text-primary"><Icon name="arrow_back" size={18} />{prev.name}</Link>
          <span className="text-on-surface-variant">·</span>
          <Link to={`/platform/${next.slug}`} className="inline-flex items-center gap-1 text-sm font-semibold text-primary">{next.name}<Icon name="arrow_forward" size={18} /></Link>
        </div>
      </ImageSplit>
      <CtaBand source={`platform_${p.slug}_cta`} />
    </>
  );
}
