// Property Context di client (Onboarding Brief §8–§12): profile + capability + terminologi diambil dari server
// (`GET /properties/{id}/capabilities`). Nav/label hanya mencerminkan; authorization tetap server-side (§12, §22).
import { useMemo } from "react";
import { useQueries } from "@tanstack/react-query";
import { api } from "./api";
import { useAuth } from "./auth";

export type ProfileCode = "hotel" | "apartment" | "office";
export interface Term {
  id: string;
  en: string;
}
export interface ProfileConfig {
  terminology: Record<string, Term>;
  expose_sla_to_tenant: boolean;
  tenant_confirmation_required: boolean;
  csat_enabled: boolean;
  booking_approval_required: boolean;
  visitor_approval_required: boolean;
  tenant_self_registration: boolean;
  auto_close_resolved_hours: number;
  settings: Record<string, unknown>;
  updated_at: string;
  version: number;
}
export interface PropertyContext {
  property_id: string;
  property_name: string;
  profile: ProfileCode;
  status: string;
  capabilities: string[];
  terminology: Record<string, Term>;
  config: ProfileConfig;
}

export const PROFILE_LABEL: Record<ProfileCode, string> = { hotel: "Hotel", apartment: "Apartment", office: "Office" };
export const PROFILE_ICON: Record<ProfileCode, string> = { hotel: "hotel", apartment: "apartment", office: "business" };

export function fetchPropertyContext(propertyId: string, signal?: AbortSignal) {
  return api<PropertyContext>(`properties/${propertyId}/capabilities`, { signal });
}

export interface ProfileState {
  /** Profile property aktif; null bila "Semua properti" (konteks campuran). */
  profile: ProfileCode | null;
  context: PropertyContext | null;
  /** Capability gabungan: property aktif, atau union seluruh property yang dapat diakses saat "Semua properti". */
  capabilities: Set<string>;
  has: (cap: string) => boolean;
  /** Label terminologi profile (NC v2.0 §7) — fallback ke default Office. */
  term: (key: string, lang?: "id" | "en") => string;
  loading: boolean;
}

const OFFICE_TERMS: Record<string, Term> = {
  customer: { id: "Tenant", en: "Tenant" },
  customer_plural: { id: "Tenant", en: "Tenants" },
  occupant: { id: "Penghuni", en: "Occupant" },
  relation_module: { id: "Tenant Relation", en: "Tenant Relation" },
  request: { id: "Tenant Service Request", en: "Tenant Service Request" },
  stay: { id: "Tenancy", en: "Tenancy" },
  inventory_unit: { id: "Unit", en: "Unit" },
  inventory_units: { id: "Unit", en: "Units" },
  my_unit: { id: "Unit Saya", en: "My Unit" },
  cleaning_task: { id: "Cleaning", en: "Cleaning" },
  commercial_module: { id: "Commercial", en: "Commercial" },
};

export function useProfile(): ProfileState {
  const { principal, properties, propertyId } = useAuth();
  const ids = useMemo(() => (propertyId ? [propertyId] : properties.map((p) => p.id)), [propertyId, properties]);
  const queries = useQueries({
    queries: ids.map((id) => ({
      queryKey: ["property-context", id],
      enabled: !!principal,
      staleTime: 5 * 60_000,
      queryFn: ({ signal }: { signal: AbortSignal }) => fetchPropertyContext(id, signal),
    })),
  });
  return useMemo(() => {
    const ctxs = queries.map((q) => q.data).filter((x): x is PropertyContext => !!x);
    const active = propertyId ? ctxs.find((c) => c.property_id === propertyId) ?? null : null;
    const caps = new Set<string>();
    for (const c of ctxs) for (const cap of c.capabilities) caps.add(cap);
    // mandatory selalu ada (PRD §3.2) — jaga UI tetap konsisten bahkan sebelum fetch selesai
    for (const m of ["housekeeping", "security", "engineering", "tenant_relation"]) caps.add(m);
    const terms = active?.terminology ?? OFFICE_TERMS;
    return {
      profile: active?.profile ?? null,
      context: active,
      capabilities: caps,
      has: (cap) => caps.has(cap),
      term: (key, lang = "id") => (terms[key] ?? OFFICE_TERMS[key] ?? { id: key, en: key })[lang],
      loading: queries.some((q) => q.isLoading),
    };
  }, [queries, propertyId]);
}
