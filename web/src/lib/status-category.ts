// Status System umum (PRD P0 §20.2): status kanonik tiap object → kategori lifecycle bersama
// New · In Progress · Pending · Completed · Closed · Cancelled. Overdue & Critical adalah flag (lihat FlagBadges),
// bukan kategori lifecycle. Label/warna kategori ada di contracts/status-map.yaml grup `common`.
import { statusMap, type ObjectType } from "./status-map";

export type StatusCategory = "new" | "in_progress" | "pending" | "completed" | "closed" | "cancelled";
export const STATUS_CATEGORIES: StatusCategory[] = ["new", "in_progress", "pending", "completed", "closed", "cancelled"];

// Draft (WO, PRD P1 v2 §23)/Assigned/Scheduled = belum dikerjakan (New). on_hold = "Pending" PRD §10.4. Resolved = Completed (menunggu Close).
const WORK: Record<string, StatusCategory> = { draft: "new", new: "new", scheduled: "new", assigned: "new", in_progress: "in_progress", on_hold: "pending", completed: "completed", closed: "closed", cancelled: "cancelled" };

export const STATUS_CATEGORY_MAP: Partial<Record<ObjectType, Record<string, StatusCategory>>> = {
  task: WORK,
  work_order: WORK,
  inspection: WORK,
  service_request: { new: "new", acknowledged: "new", assigned: "new", in_progress: "in_progress", waiting_for_tenant: "pending", resolved: "completed", closed: "closed", cancelled: "cancelled" },
  service_request_tenant: { submitted: "new", received: "new", being_assigned: "new", in_progress: "in_progress", need_your_response: "pending", resolved: "completed", closed: "closed", cancelled: "cancelled" },
  incident: { new: "new", assigned: "new", in_progress: "in_progress", resolved: "completed", closed: "closed", cancelled: "cancelled" },
  finding: { open: "new", in_progress: "in_progress", resolved: "completed", closed: "closed" },
  maintenance_schedule: { scheduled: "new", due: "pending", overdue: "pending", in_progress: "in_progress", completed: "completed", skipped: "cancelled", cancelled: "cancelled" },
  booking: { pending: "pending", confirmed: "new", checked_in: "in_progress", completed: "completed", cancelled: "cancelled", rejected: "cancelled", no_show: "cancelled" },
  visitor: { pending_approval: "pending", registered: "new", checked_in: "in_progress", checked_out: "completed", expired: "closed", cancelled: "cancelled", denied: "cancelled" },
  invoice: { draft: "new", issued: "pending", partially_paid: "in_progress", overdue: "pending", paid: "completed", cancelled: "cancelled" },
  payment: { initiated: "new", pending: "pending", paid: "completed", failed: "cancelled", expired: "cancelled", cancelled: "cancelled", refunded: "closed" },
  hotel_reservation: { new: "new", confirmed: "pending", checked_in: "in_progress", checked_out: "completed", cancelled: "cancelled", no_show: "cancelled" },
  rental_reservation: { new: "new", reserved: "pending", active: "in_progress", completed: "completed", cancelled: "cancelled" },
  sale_reservation: { reserved: "pending", contract_signed: "in_progress", sold: "completed", handed_over: "closed", cancelled: "cancelled" },
  sales_lead: { new: "new", contacted: "in_progress", qualified: "in_progress", reserved: "pending", sold: "completed", lost: "closed", cancelled: "cancelled" },
  tenant_user: { pending_validation: "pending", active: "completed", rejected: "cancelled", suspended: "closed" },
  checkpoint: { pending: "pending", scanned: "completed", missed: "cancelled" },
  // PRD P2 v2.1: lifecycle object Security & Workforce (grup status-map baru). Kondisi (asset_health, validity, vehicle) bukan lifecycle.
  emergency_alert: { raised: "new", acknowledged: "in_progress", responding: "in_progress", resolved: "completed", cancelled: "cancelled" },
  parking_violation: { open: "new", escalated: "in_progress", resolved: "completed" },
  lost_found_item: { stored: "pending", returned: "completed", disposed: "closed" },
  lost_report: { open: "new", matched: "in_progress", closed: "closed", cancelled: "cancelled" },
  investigation: { not_started: "new", in_progress: "in_progress", completed: "completed" },
  roster_attendance: { upcoming: "new", on_duty: "in_progress", late: "in_progress", completed: "completed", absent: "cancelled" },
  attendance: { on_duty: "in_progress", completed: "completed", auto_closed: "closed" },
  shift_handover: { submitted: "pending", acknowledged: "completed" },
  cleaning_route_run: { scheduled: "new", in_progress: "in_progress", completed: "completed", cancelled: "cancelled" },
  common: { new: "new", in_progress: "in_progress", pending: "pending", completed: "completed", closed: "closed", cancelled: "cancelled" },
};

/** Kategori umum untuk status object; `undefined` bila status tidak dikenal (StatusBadge tetap menampilkan status mentah). */
export function statusCategory(objectType: ObjectType | string, status: string | null | undefined): StatusCategory | undefined {
  if (!status) return undefined;
  const table = STATUS_CATEGORY_MAP[objectType as ObjectType];
  const hit = table?.[status];
  if (hit) return hit;
  // Fallback nama generik (object type baru sebelum tabelnya ditambahkan)
  return (STATUS_CATEGORIES as string[]).includes(status) ? (status as StatusCategory) : status === "on_hold" ? "pending" : undefined;
}

/** Status kanonik suatu object type yang termasuk kategori tertentu (mis. filter "Pending" lintas modul). */
export function statusesInCategory(objectType: ObjectType, category: StatusCategory): string[] {
  return Object.keys(statusMap[objectType] ?? {}).filter((s) => statusCategory(objectType, s) === category);
}
