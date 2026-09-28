// Model form profil Organization (PRD P0 v2 §6.2) — dipakai Settings › Organization dan registry Platform Admin.
import type { Organization } from "@/api/types";

export const ORG_FIELDS = ["name", "legal_name", "email", "phone", "website", "address", "city", "province", "postal_code", "country", "tax_id", "industry", "timezone"] as const;
export type OrgField = (typeof ORG_FIELDS)[number];
export type OrgForm = Record<OrgField, string>;

export function emptyOrgForm(): OrgForm {
  return { name: "", legal_name: "", email: "", phone: "", website: "", address: "", city: "", province: "", postal_code: "", country: "ID", tax_id: "", industry: "", timezone: "Asia/Jakarta" };
}
export function orgFormFrom(o: Partial<Organization>): OrgForm {
  const f = emptyOrgForm();
  for (const k of ORG_FIELDS) f[k] = (o[k] as string | null | undefined) ?? "";
  return f;
}
/** Body PATCH/POST: string kosong → null (kecuali name/country/timezone yang tidak boleh kosong). */
export function orgFormToBody(f: OrgForm): Record<string, string | null> {
  const out: Record<string, string | null> = {};
  for (const k of ORG_FIELDS) {
    const v = f[k].trim();
    if (k === "name" || k === "timezone") out[k] = v || null;
    else if (k === "country") out[k] = v ? v.toUpperCase().slice(0, 2) : null;
    else out[k] = v || null;
  }
  return out;
}
/** Validasi ringan di klien; server tetap sumber kebenaran. */
export function validateOrgForm(f: OrgForm): Partial<Record<OrgField, string>> {
  const e: Partial<Record<OrgField, string>> = {};
  if (!f.name.trim()) e.name = "required";
  if (f.country && !/^[A-Za-z]{2}$/.test(f.country.trim())) e.country = "iso2";
  if (f.email && !/^\S+@\S+\.\S+$/.test(f.email.trim())) e.email = "email";
  if (f.website && !/^https?:\/\//i.test(f.website.trim())) e.website = "url";
  return e;
}
