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
}
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
  reopen_count?: number | null;
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
export interface ChecklistRunItem {
  id: string;
  sort_order: number;
  section: string | null;
  label: string;
  item_type: "ok_notok_na" | "yes_no" | "numeric" | "text" | "photo";
  is_required: boolean;
  photo_required: boolean;
  numeric_unit: string | null;
  numeric_min: number | null;
  numeric_max: number | null;
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
}
export interface ChecklistTemplate {
  id: string;
  code: string;
  name: string;
  description: string | null;
  domain: string | null;
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
  status: "draft" | "published" | "archived";
  next_due: string | null;
  schedule_count: number;
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
  roles: { role_id: string; role_code?: string; role_name?: string; property_id: string | null }[];
  teams: { team_id: string; team_name?: string; domain?: string; is_lead: boolean }[];
  last_login_at: string | null;
  version: number;
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
}
export interface BuildingState {
  location_id: string;
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
