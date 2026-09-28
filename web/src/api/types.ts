// Tipe API (snake_case sesuai backend; sumber: contracts/openapi/v1.yaml).
export type Priority = "low" | "medium" | "high" | "critical";
export type ObjectType = "task" | "work_order" | "service_request" | "incident" | "finding" | "asset" | "maintenance_schedule" | "inspection";

export interface AssigneeRef {
  user_id: string | null;
  user_name: string | null;
  team_id: string | null;
  team_name: string | null;
}
export interface LocationRef {
  id: string | null;
  name: string | null;
  path_text: string | null;
}
export interface AssetRef {
  id: string | null;
  asset_code: string | null;
  name: string | null;
  status: string | null;
  equipment_id?: string | null;
  equipment_name?: string | null;
}
export interface SLAInfo {
  policy_id: string | null;
  response_due_at: string | null;
  resolution_due_at: string | null;
  responded_at: string | null;
  resolved_at: string | null;
  sla_risk_at: string | null;
  sla_breached_at: string | null;
  escalated_at: string | null;
  elapsed_pct: number | null;
  remaining_minutes: number | null;
  /** PRD P1 v2 §21.3: on_track | at_risk | breached | completed. */
  status?: SLAStatus | null;
  /** pending | met | breached | "" (tanpa target respons). */
  response_status?: string | null;
  response_breached_at?: string | null;
  /** true/false bila sudah selesai; null bila belum. */
  met?: boolean | null;
}
export type SLAStatus = "on_track" | "at_risk" | "breached" | "completed";
export interface Money {
  currency_code: string;
  amount: number;
}
export interface ObjectLink {
  id: string;
  link_type: string;
  direction: "from" | "to";
  object_type: string;
  object_id: string;
  label: string;
  title: string;
  status: string;
}
export interface ChecklistSummary {
  run_id: string;
  status: string;
  total_items: number;
  answered_items: number;
  not_ok_items: number;
  photo_missing: number;
}
export interface WorkItem {
  object_type: "task" | "work_order";
  id: string;
  property_id: string;
  number: string;
  type: string;
  title: string;
  description: string | null;
  location: LocationRef;
  asset: AssetRef;
  priority: Priority;
  status: string;
  scheduled_start_at: string | null;
  due_at: string | null;
  started_at: string | null;
  completed_at: string | null;
  closed_at: string | null;
  cancelled_at: string | null;
  checklist_template_id: string | null;
  requires_evidence: boolean;
  completion_notes: string | null;
  is_overdue: boolean;
  sla_risk_at: string | null;
  sla_breached_at: string | null;
  evidence_incomplete: boolean;
  assignee: AssigneeRef;
  source_type: string | null;
  source_id: string | null;
  created_at: string;
  created_by: string | null;
  created_by_name: string | null;
  updated_at: string;
  version: number;
  resolution?: string | null;
  estimated_cost?: Money | null;
  actual_cost?: Money | null;
  parts_usage?: string | null;
  vendor_reference?: string | null;
  vendor_id?: string | null;
  vendor_name?: string | null;
  vendor_notes?: string | null;
  reopen_count?: number | null;
  last_reopened_at?: string | null;
  notes?: string | null;
  requester_user_id?: string | null;
  maintenance_schedule_id?: string | null;
  sla?: SLAInfo;
  allowed_actions: string[];
  flags: string[];
  checklist_summary?: ChecklistSummary;
  attachment_count: number;
  comment_count: number;
  links?: ObjectLink[];
  extension?: Record<string, unknown>;
  // PRD P1 v2 §13, §21, §26
  /** Kategori task (kode bebas, mis. hvac/electrical); null untuk WO. */
  category?: string | null;
  /** "" bila tanpa SLA/due. */
  sla_status?: SLAStatus | "" | null;
  escalated_at?: string | null;
  escalation_level?: number | null;
  parts_cost?: Money | null;
  service_cost?: Money | null;
  other_cost?: Money | null;
  submitted_at?: string | null;
  // PRD P1 v2.1 §5.15: rantai keluhan lintas tim
  /** Service Request asal rantai (langsung maupun tidak langsung). */
  origin_service_request_id?: string | null;
  origin_service_request_number?: string | null;
  /** Task tindak lanjut dari WO: final_inspection | re_clean | security_verification | inspection | follow_up. */
  follow_up_purpose?: string | null;
}
/** Satu langkah dalam rantai lintas tim SR (GET /service-requests/{id}/chain, P1-XMW-04). */
export interface ChainItem {
  object_type: "task" | "work_order" | "finding" | "incident";
  id: string;
  number: string;
  title: string;
  type: string;
  status: string;
  priority: string;
  team_id: string | null;
  team_name: string | null;
  team_domain: string | null;
  assignee_name: string | null;
  sla_status: string;
  due_at: string | null;
  created_at: string;
  done_at: string | null;
  parent_type: string | null;
  parent_id: string | null;
  parent_label: string;
  depth: number;
  follow_up_purpose?: string | null;
  open: boolean;
  deep_link: string;
}
export interface ChainView {
  service_request_id: string;
  request_number: string;
  status: string;
  open_count: number;
  team_count: number;
  items: ChainItem[];
}
export interface ServiceRequest {
  id: string;
  property_id: string;
  request_number: string;
  category_code: string;
  category_name: string | null;
  title: string;
  description: string | null;
  tenant_id: string | null;
  tenant_name: string | null;
  requester_name: string | null;
  requester_phone: string | null;
  location: LocationRef;
  priority: Priority;
  status: string;
  channel: string;
  assignee: AssigneeRef;
  acknowledged_at: string | null;
  resolved_at: string | null;
  closed_at: string | null;
  resolution: string | null;
  sla?: SLAInfo;
  sla_risk_at: string | null;
  sla_breached_at: string | null;
  flags: string[];
  links: ObjectLink[];
  attachment_count: number;
  comment_count: number;
  allowed_actions: string[];
  created_at: string;
  created_by_name: string | null;
  version: number;
  // P1 (PRD v1.3)
  tenant_user_id: string | null;
  area_scope: "unit" | "common_area" | "other" | null;
  reopen_count: number;
  confirmed_at: string | null;
  due_estimate_at: string | null;
  domain: string | null;
  message_count: number;
  unread_tenant_messages: number;
  feedback?: { rating: number; comment: string | null; created_at: string } | null;
  // PRD P1 v2 §27
  request_type?: string | null;
  sla_status?: SLAStatus | "" | null;
}
export interface Incident {
  id: string;
  property_id: string;
  incident_number: string;
  incident_type: string;
  category: string;
  title: string;
  description: string | null;
  location: LocationRef;
  severity: Priority;
  priority: Priority;
  status: string;
  reported_by_name: string | null;
  reported_at: string;
  occurred_at: string | null;
  assignee: AssigneeRef;
  resolution: string | null;
  resolved_at: string | null;
  closed_at: string | null;
  flags: string[];
  links: ObjectLink[];
  attachment_count: number;
  comment_count: number;
  allowed_actions: string[];
  created_at: string;
  version: number;
  // PRD P1 v2 §33
  action_taken?: string | null;
  sla_status?: SLAStatus | "" | null;
  /** Asal incident (mis. emergency_alert / parking_violation — object Security P2 tanpa link). */
  source_type?: string | null;
  source_id?: string | null;
  // PRD P2 v2.1 §6.3: eskalasi (P2-SIN-05), investigasi (P2-SIN-06), people involved (P2-SIN-03), video (P2-SIN-04)
  escalation_level?: number;
  escalated_at?: string | null;
  investigation?: IncidentInvestigation | null;
  people_count?: number;
  video_count?: number;
}
export interface IncidentInvestigation {
  /** not_started | in_progress | completed */
  status: string;
  investigator_user_id: string | null;
  investigator_name: string | null;
  findings: string | null;
  root_cause: string | null;
  corrective_action: string | null;
  corrective_owner_user_id: string | null;
  corrective_owner_name: string | null;
  investigated_at: string | null;
}
/** Orang terlibat incident (data pribadi: security.incident_people.*; akses diaudit). */
export interface IncidentPerson {
  id: string;
  incident_id: string;
  /** reporter | victim | witness | suspect | other */
  person_role: string;
  name: string;
  contact: string | null;
  identity_number: string | null;
  tenant_id: string | null;
  tenant_name: string | null;
  notes: string | null;
  created_at: string;
  version: number;
}
export interface Finding {
  id: string;
  property_id: string;
  finding_number: string;
  finding_type: string;
  category: string | null;
  title: string;
  description: string | null;
  location: LocationRef;
  asset: AssetRef;
  severity: Priority;
  status: string;
  source_type: string | null;
  source_id: string | null;
  source_label: string;
  reported_by_name: string | null;
  reported_at: string;
  resolution: string | null;
  resolved_at: string | null;
  links: ObjectLink[];
  attachment_count: number;
  allowed_actions: string[];
  /** PRD P2 v2.1 P2-PAT-08: temuan patroli terhubung ke checkpoint asal. */
  checkpoint_id?: string | null;
  checkpoint_name?: string | null;
  version: number;
}
export interface Activity {
  id: string;
  object_type: string;
  object_id: string;
  actor_user_id: string | null;
  actor_name: string;
  action: string;
  from_value: string | null;
  to_value: string | null;
  payload: Record<string, unknown>;
  occurred_at: string;
  client_recorded_at: string | null;
  source: "web" | "mobile" | "system" | "sync";
}
export interface Comment {
  id: string;
  author_id: string;
  author_name: string;
  body: string;
  source: string;
  created_at: string;
}
export interface Attachment {
  id: string;
  object_type: string;
  object_id: string;
  attachment_type: string;
  original_filename: string | null;
  content_type: string;
  size_bytes: number;
  captured_at: string | null;
  uploaded_by: string;
  uploaded_by_name: string;
  uploaded_at: string;
  gps_lat: number | null;
  gps_lng: number | null;
  gps_status: "captured" | "unavailable" | "denied";
  status: "pending" | "ready" | "failed";
  caption: string | null;
  url?: string;
  thumb_url?: string;
}
export type ChecklistItemType = "ok_notok_na" | "yes_no" | "pass_fail" | "numeric" | "text" | "photo" | "selection" | "signature";
export interface ChecklistOption {
  value: string;
  label: string;
}
export interface ChecklistRunItem {
  id: string;
  sort_order: number;
  section: string | null;
  label: string;
  item_type: ChecklistItemType;
  is_required: boolean;
  photo_required: boolean;
  numeric_unit: string | null;
  numeric_min: number | null;
  numeric_max: number | null;
  options?: ChecklistOption[];
  expected_value?: string | null;
  /** true = jawaban menyimpang dari expected result (PRD P0 v2 §12.2). */
  is_deviation?: boolean | null;
  help_text?: string | null;
  result_value: string | null;
  result_number: number | null;
  result_text: string | null;
  attachment_id: string | null;
  note: string | null;
  answered_by_name: string | null;
  answered_at: string | null;
  answered_source: string | null;
  finding_id: string | null;
  out_of_range: boolean;
}
export interface ChecklistRun {
  id: string;
  object_type: string;
  object_id: string;
  template_id: string;
  template_version: number;
  template_name: string;
  status: string;
  total_items: number;
  answered_items: number;
  not_ok_items: number;
  items: ChecklistRunItem[];
}
export interface ChecklistTemplateItem {
  id?: string;
  sort_order: number;
  section: string | null;
  label: string;
  item_type: ChecklistRunItem["item_type"];
  is_required: boolean;
  photo_required: boolean;
  numeric_unit: string | null;
  numeric_min: number | null;
  numeric_max: number | null;
  help_text: string | null;
  options?: ChecklistOption[];
  expected_value?: string | null;
}
export interface ChecklistTemplate {
  id: string;
  code: string;
  name: string;
  description: string | null;
  domain: string | null;
  category?: string | null;
  applies_to: string[];
  status: "draft" | "published" | "archived";
  current_version: number;
  items: ChecklistTemplateItem[];
  usage_count: number;
  version: number;
}
export interface Location {
  id: string;
  property_id: string;
  location_type: "property" | "building" | "tower" | "floor" | "area" | "space" | "unit";
  parent_id: string | null;
  name: string;
  code: string;
  depth: number;
  sort_order: number;
  is_active: boolean;
  qr_code: string | null;
  path: { id: string; type: string; name: string }[];
  path_text: string;
  details: Record<string, unknown>;
  /** key-value bebas (PRD P0 v2 §7). */
  metadata?: Record<string, unknown>;
  child_count: number;
  version: number;
}
export interface TreeNode extends Location {
  children: TreeNode[];
}
export interface Asset {
  id: string;
  property_id: string;
  asset_code: string;
  name: string;
  equipment_id: string;
  category_code: string;
  category_name: string;
  type_name: string | null;
  location_id: string;
  location_name: string;
  location_path: string;
  status: "active" | "inactive" | "under_maintenance" | "decommissioned";
  criticality: Priority | null;
  manufacturer: string | null;
  model: string | null;
  serial_number: string | null;
  installed_at: string | null;
  warranty_until: string | null;
  specifications: Record<string, unknown>;
  notes: string | null;
  qr_code: string | null;
  qr_url: string | null;
  open_work_orders: number;
  next_pm_due: string | null;
  last_maintenance_at: string | null;
  // PRD P2 v2.1 §5.6: equipment health (NC §14) & dokumen equipment
  health_score?: number | null;
  /** healthy | warning | critical | offline | unknown (status map `asset_health`). */
  health_status?: string | null;
  health_factors?: HealthFactor[] | null;
  health_updated_at?: string | null;
  document_count?: number;
  /** Dokumen/warranty kedaluwarsa atau ≤ 30 hari. */
  expiring_documents?: number;
  version: number;
}
export interface Equipment {
  id: string;
  category_code: string;
  category_name: string;
  type_name: string | null;
  parent_id: string | null;
  is_active: boolean;
  asset_count: number;
}
export interface MaintenancePlan {
  id: string;
  property_id: string;
  plan_code: string;
  name: string;
  asset_id: string;
  asset_code: string;
  asset_name: string;
  frequency: string;
  interval_days: number | null;
  start_date: string;
  end_date: string | null;
  checklist_template_id: string | null;
  default_priority: Priority;
  responsible_team_id: string | null;
  responsible_team_name: string | null;
  lead_time_days: number;
  duration_minutes?: number | null;
  description?: string | null;
  status: "draft" | "published" | "archived";
  next_due: string | null;
  schedule_count: number;
  /** PRD P2 v2.1 P2-INS-02: work_order (PM) | inspection (inspeksi terjadwal berulang). */
  output_type?: "work_order" | "inspection" | string;
  version: number;
}
export interface MaintenanceSchedule {
  id: string;
  property_id: string;
  plan_id: string;
  plan_code: string;
  plan_name: string;
  asset_id: string;
  asset_code: string;
  asset_name: string;
  location_path: string;
  due_date: string;
  due_at: string;
  status: string;
  work_order_id: string | null;
  work_order_number: string | null;
  work_order_status: string | null;
  /** PRD P2 v2.1 P2-INS-02: plan output_type=inspection → task inspeksi. */
  output_type?: string;
  task_id?: string | null;
  task_number?: string | null;
  task_status?: string | null;
  completed_at?: string | null;
  skipped_reason?: string | null;
  priority: Priority;
  team_name: string | null;
}
export interface Tenant {
  id: string;
  property_id: string;
  tenant_code: string;
  name: string;
  tenant_type: string;
  contact_name: string | null;
  contact_phone: string | null;
  contact_email: string | null;
  status: string;
  units: { location_id: string; unit_number: string; path_text: string }[];
  open_requests: number;
  version: number;
}
export interface User {
  id: string;
  user_code: string;
  email: string | null;
  username: string | null;
  full_name: string;
  phone: string | null;
  is_active: boolean;
  roles: RoleAssignment[];
  teams: { team_id: string; team_name?: string; domain?: string; is_lead: boolean }[];
  last_login_at: string | null;
  vendor_id?: string | null;
  vendor_name?: string | null;
  preferred_locale?: string;
  created_at?: string;
  version: number;
}
export interface RoleAssignment {
  role_id: string;
  role_code?: string;
  role_name?: string;
  property_id: string | null;
  /** Scope Building/Tower dalam property (PRD P0 v2 §8.4). */
  scope_location_id?: string | null;
  scope_location_name?: string | null;
}
export interface Session {
  id: string;
  client: string;
  device_id: string | null;
  ip: string | null;
  user_agent: string | null;
  created_at: string;
  last_used_at: string | null;
  expires_at: string;
  current: boolean;
}
export interface Organization {
  id: string;
  code: string;
  slug: string;
  name: string;
  legal_name: string | null;
  email: string | null;
  phone: string | null;
  website: string | null;
  address: string | null;
  city: string | null;
  province: string | null;
  postal_code: string | null;
  country: string;
  tax_id: string | null;
  industry: string | null;
  timezone: string;
  status: "active" | "inactive" | "suspended" | string;
  status_reason: string | null;
  status_changed_at: string | null;
  is_active: boolean;
  is_internal: boolean;
  plan_code: string | null;
  trial_status: string;
  settings: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  version: number;
}
export interface OrganizationSummary extends Organization {
  user_count: number;
  property_count: number;
}
export interface Portfolio {
  id: string;
  code: string;
  name: string;
  description: string | null;
  is_active: boolean;
  property_count: number;
  created_at: string;
  updated_at: string;
  version: number;
}
export interface ExportJob {
  id: string;
  resource: string;
  format: string;
  status: "pending" | "processing" | "ready" | "failed" | "expired" | string;
  row_count: number | null;
  download_url: string | null;
  error: string | null;
  created_at: string;
  completed_at: string | null;
  expires_at: string | null;
}
export interface NotificationPreference {
  type: string;
  inapp: boolean;
  push: boolean;
  email: boolean;
  email_available: boolean;
}
export interface Role {
  id: string;
  code: string;
  name: string;
  is_system: boolean;
  domain: string | null;
  permissions: string[];
  user_count: number;
}
export interface Team {
  id: string;
  property_id: string | null;
  name: string;
  domain: string;
  is_active: boolean;
  members: { user_id: string; full_name: string; is_lead: boolean }[];
}
export interface Notification {
  id: string;
  type: string;
  title: string;
  body: string;
  object_type: string | null;
  object_id: string | null;
  object_label: string | null;
  deep_link: string | null;
  severity: "info" | "warning" | "critical" | "success";
  created_at: string;
  read_at: string | null;
}
export interface SearchResult {
  object_type: string;
  object_id: string;
  business_id: string | null;
  title: string;
  subtitle: string | null;
  location_path: string | null;
  status: string | null;
  deep_link: string;
}
export interface Checkpoint {
  id: string;
  property_id: string;
  name: string;
  location_id: string;
  location_path: string;
  qr_code: string | null;
  instructions: string | null;
  is_active: boolean;
  sort_order?: number;
}
export interface PatrolRoute {
  id: string;
  property_id: string;
  name: string;
  description: string | null;
  estimated_minutes: number | null;
  checklist_template_id: string | null;
  is_active: boolean;
  checkpoints: Checkpoint[];
}
export interface PatrolSchedule {
  id: string;
  property_id: string;
  route_id: string;
  route_name: string;
  name: string;
  start_time: string;
  duration_minutes: number;
  weekdays: number[];
  responsible_team_id: string | null;
  default_assignee_user_id: string | null;
  priority: Priority;
  is_active: boolean;
  /** PRD P2 v2.1 P2-SHF-04: jadwal patrol dapat dikaitkan ke Security Shift. */
  shift_id?: string | null;
  valid_from?: string | null;
  valid_until?: string | null;
  version?: number;
}
export interface CheckpointScan {
  id: string;
  checkpoint_id: string;
  checkpoint_name: string;
  location_path: string;
  sort_order: number;
  status: "pending" | "scanned" | "missed";
  scanned_at: string | null;
  scan_method: string | null;
  gps_status: string | null;
  missed_reason: string | null;
}
export interface CleaningSchedule {
  id: string;
  property_id: string;
  name: string;
  location_id: string;
  location_path: string;
  cleaning_type: string;
  start_time: string;
  duration_minutes: number;
  weekdays: number[];
  checklist_template_id: string | null;
  responsible_team_id: string | null;
  default_assignee_user_id: string | null;
  priority: Priority;
  requires_photo: boolean;
  is_active: boolean;
  valid_from?: string | null;
  valid_until?: string | null;
  /** PRD P2 v2.1 P2-SHF-04: jadwal cleaning dapat dikaitkan ke Housekeeping Shift. */
  shift_id?: string | null;
  shift_name?: string | null;
  version?: number;
}
export interface SLAPolicy {
  id: string;
  property_id: string | null;
  object_type: string;
  priority: Priority;
  response_minutes: number | null;
  resolution_minutes: number;
  risk_threshold_pct: number;
  calendar: string;
  is_active: boolean;
}
export interface SyncConflict {
  client_mutation_id: string;
  object_type: string;
  object_id: string;
  object_label: string;
  object_title: string;
  object_status: string;
  action: string;
  reason_code: string | null;
  detail: string | null;
  worker_name: string;
  device_id: string;
  received_at: string;
  acknowledged_at: string | null;
  deep_link: string;
}
export interface AuditLog {
  id: string;
  actor_name: string;
  action: string;
  entity_type: string;
  entity_id: string | null;
  entity_label: string | null;
  occurred_at: string;
  message: string;
}
// Overview (PRD §19)
export interface Counter {
  value: number;
  breakdown?: Record<string, number>;
  link: string;
}
export interface OverviewToday {
  open_work_orders: Counter;
  overdue: Counter;
  sla_risk: Counter;
  pm_due: Counter;
  incidents: Counter;
  tenant_requests: Counter;
  // PRD P1 v2 §10
  open_tasks?: Counter;
  due_today?: Counter;
  overdue_tasks?: Counter;
  completed_today?: Counter;
  generated_at: string;
}
export interface AttentionItem {
  category: string;
  severity: "critical" | "warning";
  object_type: string;
  object_id: string;
  label: string;
  title: string;
  status: string;
  priority: string | null;
  location_path: string | null;
  due_at: string | null;
  age_minutes: number;
  since: string;
  assignee_name: string | null;
  allowed_actions: string[];
  deep_link: string;
}
export interface TodaysOperations {
  domain: string;
  items: WorkItem[];
  summary: Record<string, number>;
}
export interface WorkloadRow {
  team_id: string | null;
  team_name: string;
  domain: string;
  user_id: string | null;
  assignee_name: string;
  open_tasks: number;
  open_work_orders: number;
  overdue: number;
  in_progress: number;
  // PRD P1 v2 §12 (group=team: satu baris per team)
  due_today?: number;
  open?: number;
  members?: number;
}
export interface BuildingState {
  location_id: string;
  /** property pemilik building/tower (untuk membedakan nama kembar pada "Semua properti"). */
  property_id?: string;
  location_type: string;
  name: string;
  path_text: string;
  open: number;
  overdue: number;
  sla_risk: number;
  incidents: number;
}
export interface TenantRequestsPanel {
  new_today: number;
  open: number;
  sla_risk: number;
  recent: { id: string; request_number: string; title: string; status: string; priority: string; tenant_name: string | null; created_at: string; sla_risk: boolean }[];
}

// ---------- Building Management (PRD P1 v2 §6–§8) ----------
export interface Facility {
  id: string;
  property_id: string;
  facility_code: string;
  name: string;
  description: string | null;
  facility_type: string;
  location_id: string | null;
  location_path: string | null;
  capacity: number | null;
  requires_approval: boolean | null;
  effective_approval: boolean;
  slot_minutes: number;
  min_duration_minutes: number;
  max_duration_minutes: number;
  advance_booking_days: number;
  open_time: string;
  close_time: string;
  weekdays: number[];
  rules: string | null;
  image_attachment_id?: string | null;
  is_active: boolean;
  upcoming_bookings: number;
  version: number;
  /** operational | under_maintenance | closed | inactive */
  status?: string;
  is_bookable?: boolean;
  /** task + WO terbuka di lokasi facility (subtree). */
  open_work?: number;
}
export interface FloorPlanWorkLink {
  object_type: string;
  object_id: string;
  number: string;
  title: string;
  status: string;
  priority: string;
  deep_link: string;
}
export interface FloorPlanMarker {
  id: string;
  floor_plan_id: string;
  target_type: "location" | "facility" | "asset";
  target_id: string;
  target_label: string;
  target_name: string;
  target_kind: string;
  x_pct: number;
  y_pct: number;
  label: string | null;
  version: number;
  work: { open_tasks: number; open_work_orders: number; open_incidents: number; open_requests: number; overdue: number; critical: number; signal: "ok" | "attention" | "critical"; items: FloorPlanWorkLink[] };
}
export interface FloorPlan {
  id: string;
  property_id: string;
  location_id: string;
  location_name: string;
  location_type: string;
  location_path: string;
  name: string;
  description: string | null;
  attachment_id: string | null;
  image: Attachment | null;
  image_width: number | null;
  image_height: number | null;
  is_active: boolean;
  marker_count: number;
  markers?: FloorPlanMarker[];
  created_at: string;
  updated_at: string;
  version: number;
}
export interface OccupancyCounts {
  total: number;
  vacant: number;
  occupied: number;
  reserved: number;
  inactive: number;
  occupancy_pct: number;
}
export interface OccupancyGroup extends OccupancyCounts {
  location_id: string;
  location_type: string;
  name: string;
  /** membedakan lantai/gedung bernama sama (dikirim server). */
  path_text?: string;
}
export interface OccupancySummary extends OccupancyCounts {
  by_unit_type: Record<string, number>;
  buildings: OccupancyGroup[];
  floors: OccupancyGroup[];
}
export interface OccupancyUnit {
  location_id: string;
  code: string;
  unit_number: string;
  unit_type: string;
  path_text: string;
  unit_status: "active" | "inactive";
  occupancy_status: "vacant" | "occupied" | "reserved" | "inactive";
  area_m2: number | null;
  tenant: { id: string; name: string } | null;
  occupants: { id: string; name: string }[];
}

// ---------- PRD P2 v2.1 (Workforce Operations) — sumber: api/internal/{overview/domains,asset,workforce,housekeeping,inventory}.go ----------

/** Dashboard domain (GET /dashboards/{engineering|security|housekeeping}). */
export type KpiUnit = "count" | "pct" | "minutes" | "idr" | "score";
export type KpiSeverity = "normal" | "warning" | "critical";
export interface DashboardKpi {
  key: string;
  label: string;
  value: number;
  unit: KpiUnit | string;
  severity: KpiSeverity | string;
  hint: string;
  drill_down: string;
}
export interface DashboardBreakdownRow {
  key: string;
  label: string;
  values: Record<string, number>;
  drill_down?: string;
}
export interface DomainDashboard {
  domain: "engineering" | "security" | "housekeeping" | string;
  property_id: string | null;
  location_id: string | null;
  from: string;
  to: string;
  generated_at: string;
  kpis: DashboardKpi[];
  /** engineering: jumlah asset per health status. */
  distribution?: Partial<Record<"healthy" | "warning" | "critical" | "offline" | "unknown", number>> | null;
  breakdowns: Record<string, DashboardBreakdownRow[]>;
  attention: AttentionItem[];
  attention_total: number;
}

/** Equipment health (NC §14): pengurang skor yang dapat ditelusuri. */
export interface HealthFactor {
  code: string;
  label: string;
  count: number;
  /** Pengurang (negatif). */
  points: number;
  link?: string;
}
export interface AssetHealth {
  asset_id: string;
  score: number | null;
  status: string;
  factors: HealthFactor[];
  updated_at: string | null;
}
export interface AssetDocument {
  id: string;
  property_id: string;
  asset_id: string;
  asset_code: string;
  asset_name: string;
  document_code: string;
  /** manual | warranty | certificate | permit | inspection_report | drawing | contract | other */
  document_type: string;
  type_label: string;
  title: string;
  document_number: string | null;
  issuer: string | null;
  issued_on: string | null;
  expires_on: string | null;
  /** valid | expiring | expired | no_expiry (status map "validity"). */
  status: string;
  days_to_expire: number | null;
  attachment_id: string | null;
  file_url?: string | null;
  file_name: string | null;
  notes: string | null;
  is_active: boolean;
  created_at: string;
  version: number;
}
export interface ExpiringDocument {
  kind: "document" | "warranty";
  id: string;
  asset_id: string;
  asset_code: string;
  asset_name: string;
  title: string;
  document_type: string | null;
  expires_on: string;
  status: string;
  days_to_expire: number | null;
  deep_link: string;
  property_id: string;
}
export interface CostBucket {
  key: string;
  label: string;
  work_orders: number;
  actual_cost: number;
  parts_cost: number;
  service_cost: number;
  other_cost: number;
}
export interface AssetInsight {
  asset_id: string;
  from: string;
  to: string;
  currency_code: string;
  total: CostBucket;
  by_month: CostBucket[];
  by_type: CostBucket[];
  parts: { item_id: string; item_code: string; name: string; unit: string | null; quantity: number; total_cost: number; last_used_at: string }[];
  vendors: { vendor_id: string; name: string; work_orders: number; completed: number; total_cost: number; last_at: string | null }[];
  pm: { scheduled: number; completed: number; completed_on_time: number; overdue: number; skipped: number; compliance_pct: number };
  health: AssetHealth | null;
}

/** Shift per domain (D-P2-05). */
export type ShiftDomain = "security" | "housekeeping";
export interface ShiftDefinition {
  id: string;
  property_id: string;
  domain: ShiftDomain;
  code: string;
  name: string;
  /** HH:MM */
  start_time: string;
  end_time: string;
  crosses_midnight: boolean;
  duration_minutes: number;
  break_minutes: number;
  min_staff: number;
  color: string | null;
  sort_order: number;
  is_active: boolean;
  version: number;
}
export interface RosterEntry {
  id: string;
  property_id: string;
  domain: ShiftDomain;
  shift_id: string;
  shift_code: string;
  shift_name: string;
  shift_date: string;
  user_id: string;
  user_name: string;
  team_id: string | null;
  team_name: string | null;
  post: string | null;
  starts_at: string;
  ends_at: string;
  status: string;
  note: string | null;
  clock_in_at: string | null;
  clock_out_at: string | null;
  late_minutes: number | null;
  /** upcoming | on_duty | late | completed | absent (status map "roster_attendance"). */
  attendance: string;
}
export interface RosterAssignResult {
  created: number;
  skipped: number;
  entries: RosterEntry[];
}
export interface AttendanceRecord {
  id: string;
  property_id: string;
  domain: string;
  user_id: string;
  user_name: string;
  shift_assignment_id: string | null;
  shift_name: string | null;
  shift_starts_at: string | null;
  shift_ends_at: string | null;
  clock_in_at: string;
  clock_in_gps_status: string | null;
  clock_in_source: string;
  clock_out_at: string | null;
  clock_out_source: string | null;
  /** on_duty | completed | auto_closed (status map "attendance"). */
  status: string;
  late_minutes: number;
  worked_minutes: number;
  note: string | null;
}
export interface OnDutyShift {
  shift_id: string;
  name: string;
  starts_at: string;
  ends_at: string;
  min_staff: number;
  scheduled: number;
  on_duty: number;
  shortage: number;
}
export interface OnDutyBoard {
  domain: ShiftDomain;
  property_id: string | null;
  generated_at: string;
  scheduled: number;
  on_duty: number;
  absent: number;
  shortage: number;
  shifts: OnDutyShift[];
  teams: { team_id: string | null; team_name: string; scheduled: number; on_duty: number; absent: number }[];
  staff: RosterEntry[];
  unscheduled: AttendanceRecord[];
  link: string;
}
export interface HandoverOpenItem {
  object_type: string;
  id: string;
  number: string;
  title: string;
  status: string;
  priority?: string;
  location?: string | null;
  due_at?: string | null;
  deep_link: string;
}
export interface HandoverSnapshot {
  generated_at: string;
  /** active_incidents | active_emergencies | pending_patrols | open_findings | unfinished_cleaning | rework_tasks */
  groups: Record<string, HandoverOpenItem[]>;
  counts: Record<string, number>;
}
export interface ShiftHandover {
  id: string;
  property_id: string;
  domain: ShiftDomain;
  handover_number: string;
  shift_id: string | null;
  shift_name: string | null;
  shift_date: string | null;
  handed_over_by: string;
  handed_over_by_name: string;
  received_by: string | null;
  received_by_name: string | null;
  post: string | null;
  notes: string;
  open_items: HandoverSnapshot;
  /** submitted | acknowledged (status map "shift_handover"). */
  status: string;
  handed_over_at: string;
  acknowledged_at: string | null;
  allowed_actions: string[];
}

/** Staf anggota team domain (GET /{security|housekeeping}/staff) — pilihan roster, penerima serah terima, petugas route. */
export interface DomainStaff {
  user_id: string;
  full_name: string;
  team_ids: string[];
  team_names: string[];
}
export interface DomainCapacity {
  domain: string;
  scheduled: number;
  on_duty: number;
  absent: number;
  min_staff: number;
  shortage: number;
  capacity_minutes: number;
  workload_minutes: number;
  open_items: number;
  load_ratio: number;
  /** ok | tight | over | no_staff | idle */
  status: string;
  link: string;
}
export interface WorkforceCapacity {
  property_id: string | null;
  generated_at: string;
  domains: DomainCapacity[];
  total: DomainCapacity;
}

/** Cleaning Route (PRD P2 v2.1 §7.3). */
export interface CleaningRouteStop {
  id: string;
  location_id: string;
  location_name: string;
  location_path: string;
  sort_order: number;
  estimated_minutes: number;
  offset_minutes: number;
  checklist_template_id: string | null;
  notes: string | null;
}
export interface CleaningRouteRunStop {
  task_id: string;
  task_number: string;
  title: string;
  status: string;
  sort_order: number;
  location_id: string | null;
  location_name: string | null;
  scheduled_start_at: string | null;
  due_at: string | null;
  completed_at: string | null;
}
export interface CleaningRouteRun {
  id: string;
  property_id: string;
  route_id: string;
  route_code: string;
  route_name: string;
  cleaning_type: string;
  shift_name: string | null;
  run_date: string;
  assignee_user_id: string | null;
  assignee_name: string | null;
  team_id: string | null;
  team_name: string | null;
  /** scheduled | in_progress | completed | cancelled (status map "cleaning_route_run"). */
  status: string;
  total_stops: number;
  completed_stops: number;
  progress_pct: number;
  started_at: string | null;
  completed_at: string | null;
  stops: CleaningRouteRunStop[];
  next_stop: CleaningRouteRunStop | null;
}
export interface CleaningRoute {
  id: string;
  property_id: string;
  route_code: string;
  name: string;
  description: string | null;
  cleaning_type: string;
  shift_id: string | null;
  shift_name: string | null;
  start_time: string;
  weekdays: number[];
  responsible_team_id: string | null;
  responsible_team_name: string | null;
  default_assignee_user_id: string | null;
  default_assignee_name: string | null;
  checklist_template_id: string | null;
  requires_photo: boolean;
  priority: Priority;
  is_active: boolean;
  total_minutes: number;
  stops: CleaningRouteStop[];
  today_run?: CleaningRouteRun | null;
  version: number;
}

/** Consumable per cleaning task (PRD P2 v2.1 §7.5). */
export interface ConsumableUsage {
  id: string;
  task_id: string;
  item_id: string;
  item_code: string;
  item_name: string;
  unit: string;
  stock_location_id: string;
  stock_location_name: string;
  quantity: number;
  unit_cost: number | null;
  total_cost: number | null;
  note: string | null;
  recorded_by_name: string | null;
  recorded_at: string;
}
