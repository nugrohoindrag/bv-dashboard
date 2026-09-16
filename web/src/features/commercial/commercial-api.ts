// Tipe & label Apartment Unit Sales & Rental Management (PRD P1 v1.3 §3.10, WF-P1-008/009; Onboarding Brief §16 status).
export type Tone = "warning" | "success" | "error" | "neutral" | "info" | "primary";
export interface Doc { name: string; reference?: string | null; attachment_id?: string | null; note?: string | null; added_at?: string | null }

export interface Listing {
  id: string; listing_code: string; property_id: string; unit_location_id: string; unit_number: string; unit_name: string; floor_name: string | null; unit_occupancy_status: string;
  title: string; description: string | null; asking_price: number; currency_code: string; price_negotiable: boolean; bedrooms: number | null; bathrooms: number | null; area_m2: number | null; furnishing: string | null; features: string[];
  status: string; documents: Doc[]; published_at: string | null; sold_at: string | null; archived_at: string | null; lead_count: number; active_reservation_id: string | null; allowed_actions: string[]; created_at: string; updated_at: string; version: number;
}
export interface Lead {
  id: string; lead_code: string; property_id: string; listing_id: string | null; listing_code: string | null; listing_title: string | null; unit_number: string | null;
  full_name: string; phone: string | null; email: string | null; company: string | null; source: string; budget_min: number | null; budget_max: number | null; inquiry: string | null; status: string;
  assigned_to: string | null; assigned_name: string | null; next_follow_up_at: string | null; last_activity_at: string | null; lost_reason: string | null; notes: string | null; reservation_id: string | null; allowed_actions: string[]; created_at: string; updated_at: string; version: number;
}
export interface Activity { id: string; lead_id: string; activity_type: string; summary: string; occurred_at: string; created_by: string | null; created_by_name: string | null; created_at: string }
export interface SaleReservation {
  id: string; reservation_number: string; property_id: string; listing_id: string; listing_code: string; listing_title: string; unit_location_id: string; unit_number: string; lead_id: string; lead_code: string; buyer_name: string; buyer_phone: string | null; buyer_email: string | null;
  agreed_price: number; booking_fee: number; currency_code: string; reserved_until: string | null; status: string; contract_reference: string | null; documents: Doc[]; notes: string | null; invoice_id: string | null; tenant_id: string | null; occupant_id: string | null; tenant_user_id: string | null;
  contract_signed_at: string | null; sold_at: string | null; handed_over_at: string | null; cancelled_at: string | null; cancel_reason: string | null; allowed_actions: string[]; created_at: string; updated_at: string; version: number;
}
export interface RentalListing {
  id: string; listing_code: string; property_id: string; unit_location_id: string; unit_number: string; unit_name: string; floor_name: string | null; unit_occupancy_status: string; title: string; description: string | null;
  rate_daily: number | null; rate_weekly: number | null; rate_monthly: number | null; deposit_amount: number; currency_code: string; min_stay_days: number; max_occupants: number | null; bedrooms: number | null; bathrooms: number | null; area_m2: number | null; furnishing: string | null; features: string[];
  available_from: string | null; available_until: string | null; status: string; documents: Doc[]; published_at: string | null; archived_at: string | null; active_reservation_id: string | null; next_start_date: string | null; open_inquiries: number; allowed_actions: string[]; created_at: string; updated_at: string; version: number;
}
export interface Quote { listing_id: string; unit_number: string; rental_period: string; period_count: number; start_date: string; end_date: string; days: number; rate_amount: number; total_amount: number; deposit_amount: number; currency_code: string; available: boolean; reason?: string; conflicts?: string[] }
export interface RentalReservation {
  id: string; reservation_number: string; property_id: string; listing_id: string; listing_code: string; listing_title: string; unit_location_id: string; unit_number: string; prospect_name: string; prospect_phone: string | null; prospect_email: string | null; company: string | null; occupants: number;
  rental_period: string; period_count: number; start_date: string; end_date: string; days: number; rate_amount: number; total_amount: number; deposit_amount: number; currency_code: string; status: string; source: string; special_requests: string | null; notes: string | null;
  invoice_id: string | null; invoice_number: string | null; invoice_status: string | null; tenant_id: string | null; tenant_code: string | null; occupant_id: string | null; tenant_user_id: string | null;
  reserved_at: string | null; activated_at: string | null; completed_at: string | null; cancelled_at: string | null; cancel_reason: string | null; allowed_actions: string[]; created_at: string; updated_at: string; version: number;
}
export interface Onboarding { tenant_id: string; tenant_code: string; occupant_id: string; tenant_user_id?: string; account_email?: string; temporary_password?: string }
export interface SalesSummary { listings: Record<string, number>; leads: Record<string, number>; reservations: Record<string, number>; sold_value: number; follow_ups_due: number }
export interface RentalSummary { listings: Record<string, number>; reservations: Record<string, number>; active_rentals: number; ending_soon: number; upcoming_start: number; open_inquiries: number; occupancy_pct: number }

export const LISTING_STATUS: Record<string, { label: string; tone: Tone }> = {
  draft: { label: "Draft", tone: "neutral" }, published: { label: "Available", tone: "success" }, reserved: { label: "Reserved", tone: "warning" }, sold: { label: "Sold", tone: "primary" }, archived: { label: "Archived", tone: "neutral" },
};
export const RLISTING_STATUS: Record<string, { label: string; tone: Tone }> = {
  draft: { label: "Draft", tone: "neutral" }, published: { label: "Published", tone: "success" }, archived: { label: "Archived", tone: "neutral" },
};
// Pipeline Unit Sale (Onboarding Brief §16): New → Contacted → Qualified → Reserved → Sold; Lost/Cancelled
export const LEAD_STATUS: Record<string, { label: string; tone: Tone }> = {
  new: { label: "New", tone: "neutral" }, contacted: { label: "Contacted", tone: "info" }, qualified: { label: "Qualified", tone: "primary" }, reserved: { label: "Reserved", tone: "warning" }, sold: { label: "Sold", tone: "success" }, lost: { label: "Lost", tone: "error" }, cancelled: { label: "Cancelled", tone: "neutral" },
};
export const LEAD_ACTION_LABEL: Record<string, string> = { contact: "Tandai dihubungi", qualify: "Qualified", reserve: "Reservasi unit…", lose: "Lost…", cancel: "Batalkan…", reopen: "Buka kembali" };
export const SRES_STATUS: Record<string, { label: string; tone: Tone }> = {
  reserved: { label: "Reserved", tone: "warning" }, contract_signed: { label: "Contract Signed", tone: "info" }, sold: { label: "Sold", tone: "primary" }, handed_over: { label: "Handed Over", tone: "success" }, cancelled: { label: "Cancelled", tone: "neutral" },
};
// Unit Rental (Onboarding Brief §16): New → Reserved → Active → Completed | Cancelled
export const RRES_STATUS: Record<string, { label: string; tone: Tone }> = {
  new: { label: "New (inquiry)", tone: "neutral" }, reserved: { label: "Reserved", tone: "warning" }, active: { label: "Active", tone: "primary" }, completed: { label: "Completed", tone: "success" }, cancelled: { label: "Cancelled", tone: "neutral" },
};
export const PERIOD_LABEL: Record<string, string> = { daily: "Harian", weekly: "Mingguan", monthly: "Bulanan" };
export const PERIOD_UNIT: Record<string, string> = { daily: "hari", weekly: "minggu", monthly: "bulan" };
export const FURNISHING: Record<string, string> = { unfurnished: "Unfurnished", semi_furnished: "Semi furnished", furnished: "Furnished" };
export const SOURCES = ["walk_in", "phone", "email", "website", "referral", "agent", "other"];
export const ACTIVITY_TYPES: Record<string, string> = { note: "Catatan", call: "Telepon", meeting: "Meeting", site_visit: "Kunjungan unit", email: "Email", whatsapp: "WhatsApp", document: "Dokumen", status_change: "Perubahan status", reservation: "Reservasi" };
export const rp = (n: number) => "Rp " + new Intl.NumberFormat("id-ID").format(n);
export const fmtD = (s: string) => new Date(s).toLocaleDateString("id-ID", { day: "2-digit", month: "short", year: "numeric" });
export const num = (v: string) => (v === "" ? null : Number(v.replace(/[^\d]/g, "")) || 0);
