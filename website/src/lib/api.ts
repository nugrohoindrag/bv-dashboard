// Public API client (Website PRD §22, §33–§34, §46): website hanya mengonsumsi endpoint publik.
import { API_BASE } from "./config";

export interface PublicApp {
  name: string;
  app_type: "staff" | "tenant" | "customer";
  platform: "android" | "ios" | "other";
  downloadUrl: string;
  status: "active";
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

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, { headers: { Accept: "application/json" } });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return (await res.json()) as T;
}

export const fetchApps = () => get<{ apps: PublicApp[] }>("/public/app-downloads").then((r) => r.apps);
export const fetchPlans = () => get<{ plans: Plan[]; trial_days: number }>("/public/plans");

export interface DemoRequest {
  full_name: string;
  email: string;
  company?: string;
  phone?: string;
  property_profile?: "hotel" | "apartment" | "office" | "";
  property_count?: number;
  message?: string;
  source_page?: string;
}

export async function requestDemo(body: DemoRequest): Promise<void> {
  const res = await fetch(`${API_BASE}/public/demo-requests`, { method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify({ ...body, property_profile: body.property_profile || undefined }) });
  if (!res.ok) {
    let detail = "Something went wrong. Please try again.";
    try {
      const p = await res.json();
      detail = p.detail || p.title || detail;
    } catch {
      /* ignore */
    }
    throw new Error(detail);
  }
}
