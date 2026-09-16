// Tipe & label Hotel Booking Management (PRD P1 v1.3 §3.9; status NC/PRD §16 Hotel Reservation & Hotel Room).
export interface RoomType { id: string; property_id: string; room_type_code: string; name: string; description: string | null; capacity_adults: number; capacity_children: number; bed_type: string | null; size_m2: number | null; amenities: string[]; base_rate: number; currency_code: string; status: string; room_count: number; version: number }
export interface Room { location_id: string; property_id: string; room_code: string; room_number: string; name: string; room_type_id: string; room_type_name: string; floor_id: string | null; floor_name: string | null; room_status: string; status_note: string | null; status_changed_at: string; is_active: boolean; current_guest: string | null; current_reservation_id: string | null; next_arrival: string | null; open_cleaning_tasks: number; allowed_statuses: string[]; version: number }
export interface Rate { id: string; property_id: string; rate_code: string; room_type_id: string; room_type_name: string; name: string; rate_per_night: number; currency_code: string; valid_from: string | null; valid_until: string | null; weekdays: number[]; min_nights: number; priority: number; status: string; version: number }
export interface Guest { id?: string; full_name: string; id_type: string | null; id_number?: string | null; id_number_masked?: string | null; phone: string | null; email: string | null; nationality: string | null; is_primary: boolean }
export interface Reservation {
  id: string; reservation_number: string; property_id: string; room_type_id: string; room_type_name: string; room_location_id: string | null; room_number: string | null; room_status: string | null;
  guest_name: string; guest_phone: string | null; guest_email: string | null; adults: number; children: number; check_in_date: string; check_out_date: string; nights: number; rate_id: string | null; rate_per_night: number; total_amount: number; currency_code: string;
  status: string; stay_status: string; source: string; special_requests: string | null; notes: string | null; invoice_id: string | null; invoice_number: string | null; tenant_user_id: string | null;
  confirmed_at: string | null; checked_in_at: string | null; checked_out_at: string | null; cancelled_at: string | null; cancel_reason: string | null; guests: Guest[]; open_requests: number; created_at: string; created_by_name: string | null; allowed_actions: string[]; version: number;
}
export interface Availability { room_type_id: string; room_type_name: string; total_rooms: number; available_rooms: number; rate_per_night: number; total_amount: number; nights: number; free_room_ids: string[] }
export interface Occupancy { date: string; total_rooms: number; occupied: number; occupancy_pct: number; by_status: Record<string, number>; arrivals_today: number; departures_today: number; in_house: number; pending_arrival: number; open_guest_requests: number; dirty_rooms: number }

export const RES_STATUS: Record<string, { label: string; tone: "warning" | "success" | "error" | "neutral" | "info" | "primary" }> = {
  new: { label: "New", tone: "neutral" }, confirmed: { label: "Confirmed", tone: "info" }, checked_in: { label: "Checked In", tone: "primary" }, checked_out: { label: "Checked Out", tone: "success" }, cancelled: { label: "Cancelled", tone: "neutral" }, no_show: { label: "No Show", tone: "error" },
};
export const ROOM_STATUS: Record<string, { label: string; tone: "warning" | "success" | "error" | "neutral" | "info" | "primary" }> = {
  available: { label: "Available", tone: "success" }, occupied: { label: "Occupied", tone: "primary" }, dirty: { label: "Dirty", tone: "warning" }, clean: { label: "Clean", tone: "info" }, inspected: { label: "Inspected", tone: "success" }, out_of_order: { label: "Out of Order", tone: "error" }, out_of_service: { label: "Out of Service", tone: "neutral" },
};
export const STAY_LABEL: Record<string, string> = { upcoming: "Akan datang", arriving_today: "Tiba hari ini", arrival_overdue: "Belum tiba (lewat)", in_house: "In-house", departing_today: "Check-out hari ini", departed: "Sudah check-out", cancelled: "Batal / no show" };
export const RES_ACTION_LABEL: Record<string, string> = { confirm: "Konfirmasi", assign_room: "Tetapkan kamar…", check_in: "Check-in…", check_out: "Check-out", cancel: "Batalkan…", no_show: "No show", update: "Edit" };
export const rp = (n: number) => "Rp " + new Intl.NumberFormat("id-ID").format(n);
