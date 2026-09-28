// Tipe API PRD P3 v2.1 Tenant Experience sisi staf (snake_case sesuai backend): tenantrelation/{metrics,announcements,p3}.go,
// tenantservice/p3.go (isu berulang, log komunikasi), tenantapp (pesan SR + lampiran), waassist (log WhatsApp manual).

// ---------- KPI layanan tenant (GET /tenant-relation/metrics, P3-TSH-01..09) ----------
export interface CountRow {
  key: string;
  label: string;
  count: number;
  /** Tautan drill-down ke daftar Service Request (URL Web). */
  drill_down: string;
}
export interface DayPoint {
  /** YYYY-MM-DD di zona waktu property. */
  date: string;
  created: number;
  resolved: number;
  complaints: number;
}
export interface TRMetrics {
  open_tickets: number;
  sla_risk: number;
  /** B-07: permintaan terbuka yang melewati SLA (sla_status = breached). */
  overdue: number;
  resolved_today: number;
  reopened_30d: number;
  waiting_for_tenant: number;
  /** Permintaan dengan pesan tenant yang belum dibaca staf. */
  waiting_for_staff: number;
  pending_accounts: number;
  csat: number | null;
  csat_count: number;
  reopen_rate_pct: number | null;
  tenant_app_tickets_30d: number;
  avg_response_hours: number | null;
  avg_resolution_hours: number | null;
  sla_compliance_pct: number | null;
  requests_30d: number;
  requests_prev_30d: number;
  complaints_30d: number;
  complaints_prev_30d: number;
  recurring_issues_open: number;
  general_feedback_new: number;
  by_category: CountRow[];
  by_type: CountRow[];
  by_channel: CountRow[];
  series: DayPoint[];
  drill_down: Record<string, string>;
  timezone: string;
}

// ---------- Pengumuman (P3-ANN-01..06, P3-BRC-01) ----------
export interface AnnouncementTarget {
  kind: "location" | "tenant";
  id: string;
  label: string;
  /** Tipe lokasi (building | tower | floor | unit | area …). */
  type?: string;
}
export interface Announcement {
  id: string;
  property_id: string | null;
  property_name: string | null;
  title: string;
  excerpt: string | null;
  body: string;
  /** tenant | staff | all */
  audience: string;
  /** normal | important */
  importance: string;
  /** announcement | news | alert */
  category: string;
  /** info | warning | critical */
  severity: string;
  image_attachment_id: string | null;
  /** draft | scheduled | published | archived */
  status: string;
  publish_at: string | null;
  published_at: string | null;
  expires_at: string | null;
  requires_ack: boolean;
  target_location_ids: string[];
  target_tenant_ids: string[];
  targets: AnnouncementTarget[];
  recipients_count: number | null;
  read_count: number;
  ack_count: number;
  created_at: string;
  created_by_name: string | null;
  version: number;
  allowed_actions: string[];
}
export interface AnnouncementReader {
  user_id: string;
  full_name: string;
  tenant_name: string | null;
  unit_label: string | null;
  read_at: string | null;
  acknowledged_at: string | null;
}
export interface AnnouncementReads {
  recipients: number;
  read: number;
  acknowledged: number;
  items: AnnouncementReader[];
}

// ---------- Feedback umum (P3-FDB-02..03) ----------
export interface GeneralFeedback {
  id: string;
  feedback_number: string;
  property_id: string;
  property_name: string;
  /** suggestion | compliment | complaint | question | other */
  category: string;
  subject: string | null;
  body: string;
  is_anonymous: boolean;
  /** null bila anonim (identitas disembunyikan server). */
  sender_name: string | null;
  tenant_name: string | null;
  unit_label: string | null;
  /** new | in_review | responded | closed */
  status: string;
  response: string | null;
  responded_by_name: string | null;
  responded_at: string | null;
  photo_count: number;
  created_at: string;
  allowed_actions: string[];
  version: number;
}

// ---------- Isu berulang (P3-TSH-07, P3-CMP-03) ----------
export interface RecurringIssue {
  id: string;
  property_id: string;
  location_id: string;
  location_name: string;
  location_path: string;
  category_code: string;
  category_name: string | null;
  request_count: number;
  complaint_count: number;
  /** Permintaan terkait yang belum selesai. */
  open_count: number;
  window_days: number;
  threshold: number;
  first_seen_at: string;
  last_seen_at: string;
  /** open | acknowledged | resolved */
  status: string;
  service_request_ids: string[];
  acknowledged_by_name: string | null;
  acknowledged_at: string | null;
  resolved_at: string | null;
  note: string | null;
  allowed_actions: string[];
  version: number;
}

// ---------- Log komunikasi per Service Request (P3-TRC-03) ----------
export interface CommunicationEntry {
  at: string;
  /** inapp | push | message | whatsapp_manual */
  channel: string;
  /** to_tenant | from_tenant */
  direction: string;
  recipient: string | null;
  actor: string | null;
  title: string;
  body: string;
  /** delivered | read | unread | failed | sent_manually | not_sent */
  status: string;
  detail: string | null;
}

// ---------- Pesan SR tenant ↔ building management (P3-SRQ-04..05) ----------
export interface MessageAttachment {
  id: string;
  content_type: string;
  file_name: string | null;
  url: string;
  thumb_url?: string;
}
export interface TenantMessage {
  id: string;
  author_kind: "tenant" | "staff" | "system";
  author_name: string | null;
  body: string;
  attachment_ids: string[];
  attachments?: MessageAttachment[];
  created_at: string;
  read_at: string | null;
}

// ---------- WhatsApp manual (P3-WAM-03): GET /whatsapp/logs ----------
export interface WhatsAppLog {
  id: string;
  context: string;
  recipient_name: string | null;
  phone: string;
  message_preview: string | null;
  sent_by_name: string;
  sent_at: string;
}
