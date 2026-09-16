// /solutions/{hotel|apartment|office} (Website PRD §9–§11)
import { useEffect } from "react";
import { Link, Navigate, useParams } from "react-router-dom";
import { Seo } from "@/lib/head";
import { CtaBand, FaqList, FeatureGrid, Hero, ImageSplit, OnTheGo, Quote, Steps } from "@/components/blocks";
import { Icon } from "@/components/Icon";
import { Card, Eyebrow, Heading, Lead, Section } from "@/components/ui";
import { SOLUTIONS_CONTENT, type Solution } from "@/content/solutions";
import { SOLUTIONS } from "@/content/site";
import { track } from "@/lib/analytics";

export default function SolutionPage() {
  const { slug } = useParams();
  const s = slug && (SOLUTIONS_CONTENT as Record<string, Solution>)[slug];
  useEffect(() => {
    if (s) track("solution_viewed", { solution: s.slug });
  }, [s]);
  if (!s) return <Navigate to="/404" replace />;
  return (
    <>
      <Seo title={`${s.seoTitle} | BuildingVision`} description={s.seoDescription} path={`/solutions/${s.slug}`} image={`/images/${s.hero}-1440.webp`} jsonLd={{ "@context": "https://schema.org", "@type": "WebPage", name: s.seoTitle, description: s.seoDescription }} />
      <Hero eyebrow={s.eyebrow} title={s.title} lead={s.lead} image={s.hero} source={`solution_${s.slug}`}>
        <div className="mt-6 flex flex-wrap gap-2 text-sm">
          {SOLUTIONS.map((o) => (
            <Link key={o.to} to={o.to} className={`inline-flex items-center gap-1.5 rounded-[var(--radius-pill)] border px-3 py-1.5 ${o.to.endsWith(s.slug) ? "border-primary bg-primary-soft text-primary" : "border-border text-on-surface-variant hover:bg-surface-container-low"}`}>
              <Icon name={o.icon ?? "domain"} size={16} />{o.label}
            </Link>
          ))}
        </div>
      </Hero>

      <Section tone="muted">
        <div className="max-w-2xl"><Eyebrow>Sound familiar?</Eyebrow><Heading>The problems we hear from {s.name.toLowerCase()} teams</Heading></div>
        <div className="mt-10 grid gap-5 md:grid-cols-3">
          {s.problems.map((p) => (
            <Card key={p.title}><div className="mb-3 inline-flex h-10 w-10 items-center justify-center rounded-[var(--radius-lg)] bg-error-container text-on-error-container"><Icon name="priority_high" size={22} /></div><h3 className="font-bold text-on-surface">{p.title}</h3><p className="mt-1 text-sm text-on-surface-variant">{p.body}</p></Card>
          ))}
        </div>
      </Section>

      <Steps eyebrow="Main workflow" title={`How a ${s.name.toLowerCase()} runs on BuildingVision`} lead="One request, followed all the way to a documented fix." steps={s.workflow} image={s.workflowImage} />

      <FeatureGrid eyebrow="What you get" title={`Everything a ${s.name.toLowerCase()} operation needs`} lead="One platform. These are the modules and capabilities the profile switches on." items={s.capabilities} cols={4} tone="muted" />

      {s.splits.map((sp, i) => (
        <ImageSplit key={sp.title} title={sp.title} lead={sp.lead} bullets={sp.bullets} image={sp.image} reverse={i % 2 === 1} />
      ))}

      <Quote {...s.quote} tone="muted" />
      <Section>
        <div className="max-w-2xl"><Eyebrow>Not this profile?</Eyebrow><Heading>Same platform, other operations</Heading><Lead>One organization can run properties with different profiles. Users switch property, the profile follows.</Lead></div>
        <div className="mt-8 grid gap-4 md:grid-cols-3">
          {SOLUTIONS.filter((o) => !o.to.endsWith(s.slug)).map((o) => (
            <Link key={o.to} to={o.to} className="rounded-[var(--radius-xl)] border border-border bg-surface p-5 hover:border-primary/40"><Icon name={o.icon ?? "domain"} size={26} className="text-primary" /><div className="mt-2 font-bold text-on-surface">{o.label}</div><p className="text-sm text-on-surface-variant">{o.blurb}</p></Link>
          ))}
        </div>
      </Section>
      <FaqList items={s.faq} tone="muted" title={`${s.name} questions`} />
      <OnTheGo />
      <CtaBand source={`solution_${s.slug}_cta`} />
    </>
  );
}
