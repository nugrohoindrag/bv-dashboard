// Growth (Website PRD v1.1): signup, verifikasi email, onboarding checklist, trial lifecycle, plan, funnel events.
// Endpoint publik: /api/v1/public/* (tanpa token); terautentikasi: /onboarding, /trial, /growth/events.
import { useQuery } from "@tanstack/react-query";
import { api, tokenStore } from "./api";

export type TrialStatus = "none" | "trial" | "trial_ending_soon" | "trial_expired" | "converted" | "cancelled";

export interface TrialInfo {
  status: TrialStatus;
  started_at: string | null;
  ends_at: string | null;
  converted_at: string | null;
  cancelled_at: string | null;
  days_left: number;
  plan_code: string | null;
  is_trial_org: boolean;
  locked: boolean;
}

export interface Plan {
  code: string;
  name: string;
  tagline: string;
  price_label: string;
  period: string;
  profiles: string[];
  staff_limit: string;
  properties: string;
  includes: string[];
  trial_available: boolean;
  highlighted: boolean;
  cta: "start_trial" | "contact_sales";
}

export interface ChecklistItem {
  key: string;
  label: string;
  hint: string;
  done: boolean;
  link: string;
}

export interface Onboarding {
  profile: "hotel" | "apartment" | "office" | null;
  property_id: string | null;
  property_name: string | null;
  items: ChecklistItem[];
  completed: number;
  total: number;
  activated: boolean;
  activated_at: string | null;
  sample_data_added_at: string | null;
  dismissed: boolean;
  trial: TrialInfo | null;
}

export const useOnboarding = (enabled = true) => useQuery({ queryKey: ["onboarding"], queryFn: () => api<Onboarding>("onboarding"), enabled, staleTime: 30_000 });
export const useTrial = (enabled = true) => useQuery({ queryKey: ["trial"], queryFn: () => api<{ trial: TrialInfo; plans: Plan[] }>("trial"), enabled, staleTime: 60_000 });

export const TRIAL_LABEL: Record<TrialStatus, string> = {
  none: "",
  trial: "Free trial",
  trial_ending_soon: "Trial ending soon",
  trial_expired: "Trial expired",
  converted: "Active plan",
  cancelled: "Trial cancelled",
};

// ---------- Funnel events (§40) — tanpa data pribadi ----------
type EventName =
  | "dashboard_opened" | "profile_selected" | "property_created" | "request_created" | "task_created" | "work_order_created"
  | "staff_invited" | "task_completed" | "evidence_added" | "request_resolved" | "tenant_invited" | "onboarding_completed"
  | "subscription_started" | "start_trial_clicked" | "signup_started";

const ANON_KEY = "bv.anon_id";
export function anonymousId(): string | undefined {
  try {
    let v = localStorage.getItem(ANON_KEY);
    if (!v) {
      v = crypto.randomUUID();
      localStorage.setItem(ANON_KEY, v);
    }
    return v;
  } catch {
    return undefined;
  }
}

let queue: { event: string; source_page?: string; anonymous_id?: string; properties?: Record<string, unknown>; occurred_at: string }[] = [];
let timer: number | undefined;

export function track(event: EventName | string, properties?: Record<string, unknown>) {
  queue.push({ event, source_page: typeof location !== "undefined" ? location.pathname : undefined, anonymous_id: anonymousId(), properties, occurred_at: new Date().toISOString() });
  if (timer) return;
  timer = window.setTimeout(flush, 1500);
}

async function flush() {
  timer = undefined;
  const events = queue.splice(0, 20);
  if (events.length === 0) return;
  try {
    const path = tokenStore.get() ? "growth/events" : "public/events";
    await api(path, { body: { events }, retry: false });
  } catch {
    /* analytics tidak boleh mengganggu alur */
  }
}
