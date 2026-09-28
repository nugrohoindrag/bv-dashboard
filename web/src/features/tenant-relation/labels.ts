// Label & ikon Tenant Relation PRD P3 v2.1 (kode = nilai yang divalidasi backend). Status object (announcement, tenant_feedback,
// recurring_issue, tenant_user, package, parking_permit) memakai status map GENERATED lewat StatusBadge/statusOptions — bukan di sini.
export type LabelMap = Record<string, string>;

/** Label dari peta; kode tak dikenal → kode dengan spasi; kosong → "—". */
export function labelOf(map: LabelMap, code: string | null | undefined): string {
  if (!code) return "—";
  return map[code] ?? code.replace(/_/g, " ");
}
export const optionsOf = (map: LabelMap) => Object.entries(map).map(([value, label]) => ({ value, label }));

// Pengumuman (D-P3-06: News = kategori pengumuman; alert = broadcast darurat/operasional)
export const ANNOUNCEMENT_CATEGORIES: LabelMap = { announcement: "Pengumuman", news: "News", alert: "Alert" };
export const ANNOUNCEMENT_CATEGORY_ICON: LabelMap = { announcement: "campaign", news: "article", alert: "emergency_home" };
export const ANNOUNCEMENT_SEVERITIES: LabelMap = { info: "Info", warning: "Waspada", critical: "Kritis" };
export const SEVERITY_TONE: Record<string, "info" | "warning" | "error"> = { info: "info", warning: "warning", critical: "error" };
export const ANNOUNCEMENT_AUDIENCES: LabelMap = { tenant: "Tenant", staff: "Staf", all: "Semua" };
/** Tipe lokasi yang dapat menjadi target pengumuman (server menolak target bertipe property). */
export const TARGET_LOCATION_TYPES = ["building", "tower", "floor", "area", "unit"];
export const LOCATION_TYPE_LABELS: LabelMap = { building: "Building", tower: "Tower", floor: "Lantai", area: "Area", space: "Space", unit: "Unit" };

// Feedback umum (tenantapp feedbackCategories)
export const FEEDBACK_CATEGORIES: LabelMap = { suggestion: "Saran", compliment: "Pujian", complaint: "Keluhan", question: "Pertanyaan", other: "Lainnya" };
export const FEEDBACK_CATEGORY_ICON: LabelMap = { suggestion: "auto_awesome", compliment: "star", complaint: "report", question: "help", other: "chat_bubble" };

// Kanal Service Request (tenantrelation/metrics.go channelLabel)
export const SR_CHANNELS: LabelMap = { tenant_app: "Tenant App", staff: "Staf", whatsapp: "WhatsApp", phone: "Telepon", email: "Email", walk_in: "Datang langsung", public_intake: "Formulir publik (QR)" };

// Log komunikasi SR (tenantservice/p3.go CommunicationEntry)
export const COMM_CHANNELS: LabelMap = { inapp: "In-app", push: "Push", message: "Pesan", whatsapp_manual: "WhatsApp manual" };
export const COMM_CHANNEL_ICON: LabelMap = { inapp: "notifications", push: "smartphone", message: "forum", whatsapp_manual: "chat" };
export const COMM_STATUS: LabelMap = { delivered: "Terkirim", read: "Dibaca", unread: "Belum dibaca", failed: "Gagal", sent_manually: "Dikirim manual", not_sent: "Tidak terkirim" };
export const COMM_STATUS_TONE: Record<string, "success" | "info" | "warning" | "error" | "neutral"> = { delivered: "success", read: "success", unread: "neutral", failed: "error", sent_manually: "info", not_sent: "warning" };

// Akun Tenant App
export const REGISTRATION_SOURCES: LabelMap = { self: "Registrasi mandiri", staff: "Dibuat staf", import: "Impor", rental_onboarding: "Onboarding sewa", hotel_checkin: "Check-in hotel", sale_handover: "Serah terima unit", tenant_admin: "Dibuat Tenant Admin" };
export const OWNERSHIP_STATUSES: LabelMap = { owner: "Pemilik", tenant: "Penyewa", family: "Keluarga", employee: "Karyawan" };

// Konteks pesan WhatsApp manual (waassist contextObject)
export const WA_CONTEXTS: LabelMap = {
  account_approved: "Akun disetujui", account_rejected: "Pendaftaran ditolak", account_suspended: "Akun ditangguhkan", account_created: "Akun baru",
  password_reset: "Reset password", service_request: "Update permintaan", invoice: "Tagihan", package: "Paket tiba", parking_permit: "Izin parkir",
  collection_reminder: "Pengingat tagihan", staff_invite: "Undangan staf",
};
