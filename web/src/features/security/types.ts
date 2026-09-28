// Tipe API Security P2 (snake_case sesuai backend: api/internal/security/emergency.go, parking.go, lostfound.go).
import type { LocationRef } from "@/api/types";

// ---------- Emergency (PRD P2 v2.1 §6.4) ----------
export type EmergencyStatus = "raised" | "acknowledged" | "responding" | "resolved" | "cancelled";

export interface EmergencyEvent {
  id: string;
  /** raised | acknowledged | responding | action | escalated | incident_created | resolved | cancelled | note */
  event_type: string;
  note: string | null;
  actor_user_id: string | null;
  actor_name: string | null;
  payload: Record<string, unknown> | null;
  occurred_at: string;
}

export interface EmergencyContact {
  id: string;
  property_id: string;
  name: string;
  /** fire | ambulance | police | electricity | water | internal | other */
  contact_type: string;
  phone: string;
  notes: string | null;
  sort_order: number;
  is_active: boolean;
  version: number;
}

export interface EmergencyAlert {
  id: string;
  property_id: string;
  alert_number: string;
  emergency_type: string;
  emergency_type_label: string;
  status: EmergencyStatus | string;
  location: LocationRef;
  description: string | null;
  gps_lat: number | null;
  gps_lng: number | null;
  gps_status: string | null;
  /** panic_button | mobile | web | sync */
  channel: string;
  raised_by: string | null;
  raised_by_name: string | null;
  raised_at: string;
  client_raised_at: string | null;
  acknowledged_by: string | null;
  acknowledged_by_name: string | null;
  acknowledged_at: string | null;
  responder_user_id: string | null;
  responder_name: string | null;
  responding_at: string | null;
  resolved_by: string | null;
  resolved_at: string | null;
  resolution: string | null;
  cancelled_at: string | null;
  cancel_reason: string | null;
  escalation_level: number;
  escalated_at: string | null;
  incident_id: string | null;
  incident_number: string | null;
  /** Waktu respons raised → acknowledged (KPI Security Response Time). */
  ack_seconds: number | null;
  active: boolean;
  allowed_actions: string[];
  timeline?: EmergencyEvent[];
  contacts?: EmergencyContact[];
  created_at: string;
  version: number;
}

// ---------- Parking (PRD P2 v2.1 §6.6) ----------
export interface ParkingArea {
  id: string;
  property_id: string;
  location_id: string | null;
  location_path: string | null;
  code: string;
  name: string;
  /** tenant | visitor | staff | public | loading | mixed */
  area_type: string;
  capacity: number;
  occupied: number;
  available: number;
  is_active: boolean;
  notes: string | null;
  version: number;
}

export interface Vehicle {
  id: string;
  property_id: string;
  plate_number: string;
  vehicle_type: string;
  brand: string | null;
  color: string | null;
  owner_type: string;
  tenant_id: string | null;
  tenant_name: string | null;
  unit_location_id: string | null;
  unit_name: string | null;
  user_id: string | null;
  user_name: string | null;
  visitor_id: string | null;
  owner_name: string | null;
  owner_phone: string | null;
  parking_area_id: string | null;
  parking_area_name: string | null;
  /** YYYY-MM-DD */
  permit_until: string | null;
  permit_valid: boolean;
  /** active | inactive | blacklisted */
  status: string;
  notes: string | null;
  open_violations: number;
  /** Tercatat masuk & belum keluar. */
  inside: boolean;
  version: number;
}

export interface ParkingLog {
  id: string;
  property_id: string;
  parking_area_id: string | null;
  parking_area_name: string | null;
  vehicle_id: string | null;
  plate_number: string;
  owner_type: string | null;
  registered: boolean;
  entered_at: string;
  exited_at: string | null;
  duration_minutes: number;
  gate: string | null;
  entry_by_name: string | null;
  note: string | null;
}

export interface ParkingViolation {
  id: string;
  property_id: string;
  violation_number: string;
  parking_area_id: string | null;
  parking_area_name: string | null;
  location: LocationRef;
  vehicle_id: string | null;
  plate_number: string;
  owner_name: string | null;
  violation_type: string;
  description: string | null;
  action_taken: string;
  /** open | resolved | escalated */
  status: string;
  incident_id: string | null;
  incident_number: string | null;
  recorded_by: string | null;
  recorded_by_name: string | null;
  recorded_at: string;
  resolved_at: string | null;
  resolution: string | null;
  attachment_count: number;
  allowed_actions: string[];
  version: number;
}

// ---------- Lost & Found (PRD P2 v2.1 §6.7) ----------
export interface LostFoundItem {
  id: string;
  property_id: string;
  item_number: string;
  category: string;
  description: string;
  found_location: LocationRef;
  found_at: string;
  found_by_user_id: string | null;
  found_by_name: string | null;
  finder_name: string | null;
  storage_location: string | null;
  /** stored | returned | disposed */
  status: string;
  /** YYYY-MM-DD */
  retention_until: string;
  disposal_due: boolean;
  matched_report_id: string | null;
  matched_report_number: string | null;
  claimant_name: string | null;
  claimant_contact: string | null;
  /** Hanya untuk pemegang security.lost_found.manage (data pribadi). */
  claimant_identity?: string | null;
  returned_at: string | null;
  returned_by_name: string | null;
  signature_attachment_id: string | null;
  disposed_at: string | null;
  disposal_method: string | null;
  disposal_note: string | null;
  attachment_count: number;
  allowed_actions: string[];
  created_at: string;
  version: number;
}

export interface LostReport {
  id: string;
  property_id: string;
  report_number: string;
  category: string;
  description: string;
  lost_location: LocationRef;
  lost_at: string | null;
  reporter_name: string;
  reporter_contact: string | null;
  tenant_id: string | null;
  tenant_name: string | null;
  /** open | matched | closed | cancelled */
  status: string;
  matched_item_id: string | null;
  matched_item_number: string | null;
  created_at: string;
  version: number;
}
