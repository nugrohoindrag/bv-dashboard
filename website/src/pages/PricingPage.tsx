// /pricing (Website PRD §33): plans, included capabilities, profile applicability, staff limits, trial, upgrade + demo CTA.
import { useEffect } from "react";
import { Seo } from "@/lib/head";
import { CtaBand, FaqList, ImageSplit } from "@/components/blocks";
import { PlansPreview, usePlans } from "@/components/PlansPreview";
import { Icon } from "@/components/Icon";
import { ButtonLink, Container, Eyebrow, Heading, Lead, Section } from "@/components/ui";
import { PRICING_FAQ } from "@/content/faq";
import { LINKS, TRIAL_DAYS } from "@/lib/config";
import { track } from "@/lib/analytics";

const MATRIX: { label: string; starter: boolean | string; growth: boolean | string; enterprise: boolean | string }[] = [
  { label: "Properties", starter: "1", growth: "Up to 5", enterprise: "Unlimited" },
  { label: "Staff users", starter: "25", growth: "100", enterprise: "Unlimited" },
  { label: "Property profiles (Hotel, Apartment, Office)", starter: true, growth: true, enterprise: true },
  { label: "Housekeeping, Engineering, Security", starter: true, growth: true, enterprise: true },
  { label: "Work Orders, Tasks, Findings, Incidents", starter: true, growth: true, enterprise: true },
  { label: "Service Requests and Tenant Relation", starter: true, growth: true, enterprise: true },
  { label: "Staff App (offline) and Tenant App", starter: true, growth: true, enterprise: true },
  { label: "Preventive Maintenance and Asset Management", starter: false, growth: true, enterprise: true },
  { label: "Facility Booking and Visitor Management", starter: false, growth: true, enterprise: true },
  { label: "Billing (manual verification)", starter: false, growth: true, enterprise: true },
  { label: "Vendor and Inventory", starter: false, growth: true, enterprise: true },
  { label: "Reports and CSV export", starter: false, growth: true, enterprise: true },
  { label: "Hotel Booking Management and Reception", starter: false, growth: false, enterprise: true },
  { label: "Apartment Unit Sales and Rental", starter: false, growth: false, enterprise: true },
  { label: "SSO and audit exports", starter: false, growth: false, enterprise: true },
  { label: "Support", starter: "Email", growth: "Priority", enterprise: "SLA-backed" },
];

function Cell({ v }: { v: boolean | string }) {
  if (typeof v === "string") return <span className="text-sm font-medium text-on-surface">{v}</span>;
  return v ? <Icon name="check_circle" size={20} className="text-primary" /> : <Icon name="remove" size={20} className="text-on-surface-variant/50" />;
}

export default function PricingPage() {
  const plans = usePlans();
  useEffect(() => {
    track("pricing_viewed");
  }, []);
  return (
    <>
      <Seo title="Pricing | BuildingVision" description="Simple plans per property for hotels, apartments, and office buildings. Start with a free 14-day trial, no credit card required." path="/pricing" image="/images/planning-board-1440.webp" />
      <section className="bv-hero-bg">
        <Container className="py-16 text-center sm:py-20">
          <Eyebrow>Pricing</Eyebrow>
          <Heading as="h1" className="mx-auto max-w-3xl">Plans that grow with your portfolio</Heading>
          <Lead className="mx-auto">Every plan starts with a {TRIAL_DAYS}-day free trial and works for Hotel, Apartment, and Office properties. Prices are per property per month, billed monthly or annually.</Lead>
        </Container>
      </section>
      <Section className="pt-0 sm:pt-0"><PlansPreview full /></Section>

      <Section tone="muted">
        <div className="max-w-2xl"><Eyebrow>Compare</Eyebrow><Heading>What is included</Heading></div>
        <div className="mt-8 overflow-x-auto rounded-[var(--radius-xl)] border border-border bg-surface">
          <table className="w-full min-w-[640px] text-left">
            <thead>
              <tr className="bg-primary text-on-primary">
                <th className="px-5 py-3 text-xs font-bold uppercase tracking-wider">Capability</th>
                {plans.map((p) => <th key={p.code} className="px-5 py-3 text-xs font-bold uppercase tracking-wider">{p.name}</th>)}
              </tr>
            </thead>
            <tbody>
              {MATRIX.map((r) => (
                <tr key={r.label} className="border-t border-border">
                  <td className="px-5 py-3 text-sm text-on-surface">{r.label}</td>
                  <td className="px-5 py-3"><Cell v={r.starter} /></td>
                  <td className="px-5 py-3"><Cell v={r.growth} /></td>
                  <td className="px-5 py-3"><Cell v={r.enterprise} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="mt-6 flex flex-wrap gap-3">
          <ButtonLink to={LINKS.demo} variant="secondary" onClick={() => track("book_demo_clicked", { placement: "pricing" })}>Book a Demo</ButtonLink>
        </div>
      </Section>

      <ImageSplit eyebrow="Getting started" title="Try everything first, decide later" lead="During the trial, every module is unlocked so you can see what fits. When you choose a plan, nothing is deleted." bullets={["Sample property available on day one", "Onboarding checklist instead of a long wizard", "Your team can be invited during the trial", "Read-only mode if the trial ends before you decide"]} image="planning-board" />
      <FaqList items={PRICING_FAQ} tone="muted" title="Pricing questions" />
      <CtaBand source="pricing_cta" />
    </>
  );
}
