// Plans (Website PRD §33): dari public API (`/public/plans`) agar sama dengan dashboard; fallback statis saat API tidak tersedia.
import { useEffect, useState } from "react";
import { Icon } from "./Icon";
import { ButtonLink, Card, cn } from "./ui";
import { fetchPlans, type Plan } from "@/lib/api";
import { LINKS, SALES_EMAIL, signupHref } from "@/lib/config";
import { track } from "@/lib/analytics";

export const FALLBACK_PLANS: Plan[] = [
  { code: "starter", name: "Starter", tagline: "For a single property getting organized.", price_label: "Rp 1.500.000", period: "per property / month", profiles: ["hotel", "apartment", "office"], staff_limit: "Up to 25 staff users", properties: "1 property", includes: ["Housekeeping, Engineering, Security", "Work Orders, Tasks, Findings", "Service Requests and Tenant Relation", "Staff App with offline mode", "Tenant App", "Email support"], trial_available: true, highlighted: false, cta: "start_trial" },
  { code: "growth", name: "Growth", tagline: "For teams running several buildings.", price_label: "Rp 3.500.000", period: "per property / month", profiles: ["hotel", "apartment", "office"], staff_limit: "Up to 100 staff users", properties: "Up to 5 properties", includes: ["Everything in Starter", "Preventive Maintenance and Asset Management", "Facility Booking and Visitor Management", "Billing with manual verification", "Vendor and Inventory", "Reports and CSV export", "Priority support"], trial_available: true, highlighted: true, cta: "start_trial" },
  { code: "enterprise", name: "Enterprise", tagline: "For portfolios with custom needs.", price_label: "Custom", period: "annual agreement", profiles: ["hotel", "apartment", "office"], staff_limit: "Unlimited staff users", properties: "Unlimited properties", includes: ["Everything in Growth", "Hotel Booking Management and Reception", "Apartment Unit Sales and Rental", "SSO and audit exports", "Dedicated onboarding and training", "SLA-backed support"], trial_available: false, highlighted: false, cta: "contact_sales" },
];

export function usePlans() {
  const [plans, setPlans] = useState<Plan[]>(FALLBACK_PLANS);
  useEffect(() => {
    let alive = true;
    fetchPlans().then((r) => alive && r.plans?.length && setPlans(r.plans)).catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);
  return plans;
}

export function PlansPreview({ full }: { full?: boolean }) {
  const plans = usePlans();
  return (
    <div className="grid gap-5 lg:grid-cols-3">
      {plans.map((p) => (
        <Card key={p.code} className={cn("flex flex-col", p.highlighted && "ring-2 ring-primary")}>
          <div className="flex items-center justify-between">
            <h3 className="text-lg font-bold text-on-surface">{p.name}</h3>
            {p.highlighted && <span className="rounded-[var(--radius-pill)] bg-primary-container px-2.5 py-0.5 text-xs font-semibold text-on-primary-container">Most popular</span>}
          </div>
          <p className="mt-1 text-sm text-on-surface-variant">{p.tagline}</p>
          <div className="mt-5 text-3xl font-extrabold text-on-surface">{p.price_label}</div>
          <div className="text-xs text-on-surface-variant">{p.period}</div>
          <ul className="mt-5 space-y-2 text-sm text-on-surface">
            <li className="flex gap-2"><Icon name="domain" size={18} className="text-primary" />{p.properties}</li>
            <li className="flex gap-2"><Icon name="group" size={18} className="text-primary" />{p.staff_limit}</li>
            {(full ? p.includes : p.includes.slice(0, 4)).map((i) => <li key={i} className="flex gap-2"><Icon name="check" size={18} className="text-primary" />{i}</li>)}
          </ul>
          <div className="mt-auto pt-6">
            {p.cta === "contact_sales" ? (
              <ButtonLink to={`mailto:${SALES_EMAIL}?subject=BuildingVision%20Enterprise`} variant="secondary" className="w-full">Contact sales</ButtonLink>
            ) : (
              <ButtonLink to={signupHref(`pricing_${p.code}`)} variant={p.highlighted ? "primary" : "secondary"} className="w-full" onClick={() => track("start_trial_clicked", { placement: "pricing", plan: p.code })}>Start free trial</ButtonLink>
            )}
            {p.trial_available && <div className="mt-2 text-center text-xs text-on-surface-variant">14-day trial included</div>}
            {!p.trial_available && <div className="mt-2 text-center text-xs text-on-surface-variant"><a href={LINKS.demo} className="underline">Book a demo</a> for a guided walkthrough</div>}
          </div>
        </Card>
      ))}
    </div>
  );
}
