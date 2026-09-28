// Label dan helper murni Security P2 (PRD P2 v2.1 §6.3 incident lengkap, §6.4 Emergency, §6.6 Parking, §6.7 Lost & Found).
// Kode = nilai yang divalidasi backend (api/internal/security/*.go, operations/incident_ext.go). Status object (emergency_alert,
// parking_violation, lost_found_item, lost_report, vehicle, investigation) memakai status map GENERATED (contracts/status-map.yaml)
// lewat StatusBadge / statusLabel / statusOptions — tidak ada peta status lokal di sini (DS Guideline §4).

// ---------- Label kode → teks ----------
export type LabelMap = Record<string, string>;
/** Label dari peta; kode tak dikenal → kode dengan spasi; kosong → "—". */
export function labelOf(map: LabelMap, code: string | null | undefined): string {
  if (!code) return "—";
  return map[code] ?? code.replace(/_/g, " ");
}
export const optionsOf = (map: LabelMap) => Object.entries(map).map(([value, label]) => ({ value, label }));

// Emergency (NC §16: user-facing "Emergency", aksi "Panic Button", entitas "Emergency Alert")
export const EMERGENCY_TYPES: LabelMap = {
  fire: "Kebakaran", medical: "Medis", security_threat: "Ancaman keamanan", intrusion: "Penyusupan",
  natural_disaster: "Bencana alam", evacuation: "Evakuasi", utility_failure: "Gangguan utilitas", other: "Lainnya",
};
export const EMERGENCY_TYPE_ICON: LabelMap = {
  fire: "local_fire_department", medical: "medical_services", security_threat: "shield", intrusion: "lock",
  natural_disaster: "flood", evacuation: "directions_walk", utility_failure: "bolt", other: "warning",
};
export const EMERGENCY_CHANNELS: LabelMap = { panic_button: "Panic Button", mobile: "Staff App", web: "Web", sync: "Staff App (offline)" };
export const GPS_STATUS: LabelMap = { captured: "Lokasi GPS tertangkap", unavailable: "GPS tidak tersedia", denied: "Izin GPS ditolak" };
/** Aksi Emergency Alert (allowed_actions server) → label tombol. */
export const EMERGENCY_ACTION_LABEL: LabelMap = { acknowledge: "Terima", respond: "Tiba / Tangani", note: "Catat tindakan", resolve: "Selesaikan", cancel: "Alarm palsu" };
export const EMERGENCY_ACTION_ICON: LabelMap = { acknowledge: "mark_email_read", respond: "directions_walk", note: "chat_bubble", resolve: "check_circle", cancel: "cancel" };
/** Urutan tampil tombol aksi (aksi utama lebih dulu). */
export const EMERGENCY_ACTION_ORDER = ["acknowledge", "respond", "resolve", "note", "cancel"];
export const EMERGENCY_EVENT_LABEL: LabelMap = {
  raised: "Emergency dilaporkan", acknowledged: "Diterima security", responding: "Security tiba / menangani", action: "Tindakan dicatat",
  escalated: "Eskalasi", incident_created: "Incident dibuat otomatis", resolved: "Emergency selesai", cancelled: "Dibatalkan (alarm palsu)", note: "Catatan",
};
export const EMERGENCY_EVENT_ICON: LabelMap = {
  raised: "notification_important", acknowledged: "mark_email_read", responding: "directions_walk", action: "chat_bubble",
  escalated: "priority_high", incident_created: "emergency_home", resolved: "check_circle", cancelled: "cancel", note: "chat_bubble",
};
/** Kalimat satu event timeline Emergency Alert (payload server: channel, implicit, level, incident_number). */
export function emergencyEventText(e: { event_type: string; payload?: Record<string, unknown> | null }): string {
  const p = e.payload ?? {};
  switch (e.event_type) {
    case "raised":
      return p.channel ? `${EMERGENCY_EVENT_LABEL.raised} · ${labelOf(EMERGENCY_CHANNELS, String(p.channel))}` : EMERGENCY_EVENT_LABEL.raised;
    case "acknowledged":
      return p.implicit ? "Diterima (otomatis saat security tiba)" : EMERGENCY_EVENT_LABEL.acknowledged;
    case "escalated":
      return p.level ? `Eskalasi level ${String(p.level)}` : EMERGENCY_EVENT_LABEL.escalated;
    case "incident_created":
      return p.incident_number ? `Incident ${String(p.incident_number)} dibuat otomatis` : EMERGENCY_EVENT_LABEL.incident_created;
    default:
      return EMERGENCY_EVENT_LABEL[e.event_type] ?? e.event_type.replace(/_/g, " ");
  }
}
/**
 * Judul Emergency dari server. Format lama berisi kode tipe ("EMERGENCY — fire", masih dipakai snapshot serah terima shift) →
 * diberi label ("EMERGENCY — Kebakaran"); format baru sudah berlabel ("Emergency — Kebakaran") dan dibiarkan apa adanya.
 */
export function emergencyAttentionTitle(title: string): string {
  return title.replace(/—\s*([a-z_]+)\s*$/, (m, code: string) => (EMERGENCY_TYPES[code] ? `— ${EMERGENCY_TYPES[code]}` : m));
}
/** Aksi Emergency Alert yang diizinkan server, urut aksi utama lebih dulu (acknowledge → respond → resolve → note → cancel). */
export function orderedEmergencyActions(allowed: string[]): string[] {
  return EMERGENCY_ACTION_ORDER.filter((a) => allowed.includes(a));
}
export const CONTACT_TYPES: LabelMap = { fire: "Pemadam kebakaran", ambulance: "Ambulans", police: "Polisi", electricity: "Listrik (PLN)", water: "Air (PDAM)", internal: "Internal", other: "Lainnya" };
export const CONTACT_TYPE_ICON: LabelMap = { fire: "local_fire_department", ambulance: "medical_services", police: "local_police", electricity: "bolt", water: "water", internal: "verified_user", other: "call" };

// Parking (D-P2-06)
export const PARKING_AREA_TYPES: LabelMap = { tenant: "Tenant", visitor: "Tamu", staff: "Staf", public: "Umum", loading: "Loading dock", mixed: "Campuran" };
export const VEHICLE_TYPES: LabelMap = { car: "Mobil", motorcycle: "Motor", truck: "Truk", bicycle: "Sepeda", other: "Lainnya" };
export const VEHICLE_OWNER_TYPES: LabelMap = { tenant: "Tenant", staff: "Staf", visitor: "Tamu", other: "Lainnya" };
export const VIOLATION_TYPES: LabelMap = { illegal_parking: "Parkir liar", no_permit: "Tanpa izin", blocking: "Menghalangi", overstay: "Melebihi waktu", reserved_spot: "Slot khusus", other: "Lainnya" };
export const VIOLATION_ACTIONS: LabelMap = { none: "Belum ada", warning: "Teguran", sticker: "Stiker", wheel_lock: "Gembok roda", towed: "Diderek", reported: "Dilaporkan" };

// Lost & Found
export const LOST_FOUND_CATEGORIES: LabelMap = { electronics: "Elektronik", wallet: "Dompet", document: "Dokumen", keys: "Kunci", jewelry: "Perhiasan", bag: "Tas", clothing: "Pakaian", other: "Lainnya" };
export const DISPOSAL_METHODS: LabelMap = { donated: "Didonasikan", destroyed: "Dimusnahkan", handed_to_police: "Diserahkan ke polisi", auctioned: "Dilelang", other: "Lainnya" };

// Incident lengkap (P2-SIN-03 people involved)
export const PERSON_ROLES: LabelMap = { reporter: "Pelapor", victim: "Korban", witness: "Saksi", suspect: "Terduga", other: "Lainnya" };

/** Interval auto-refresh daftar Emergency aktif & detail (respons real-time di dashboard). */
export const EMERGENCY_REFRESH_MS = 15_000;

// ---------- Helper ----------
/** Durasi detik ringkas: 45 → "45s", 80 → "1m 20s", 120 → "2m", 3700 → "1j 1m". */
export function fmtSeconds(sec: number | null | undefined): string {
  if (sec === null || sec === undefined || !Number.isFinite(sec)) return "—";
  const s = Math.max(0, Math.round(sec));
  if (s < 60) return `${s}s`;
  if (s < 3600) {
    const m = Math.floor(s / 60);
    const r = s % 60;
    return r ? `${m}m ${r}s` : `${m}m`;
  }
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  return m ? `${h}j ${m}m` : `${h}j`;
}

/** Detik sejak waktu ISO sampai `now` (untuk "Menunggu respons …"). */
export function secondsSince(iso: string | null | undefined, now: number = Date.now()): number | null {
  if (!iso) return null;
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return null;
  return Math.max(0, Math.round((now - t) / 1000));
}

/** Badge eskalasi: level 0/kosong → null; n → "Eskalasi L{n}". */
export function escalationLabel(level: number | null | undefined): string | null {
  return level && level > 0 ? `Eskalasi L${level}` : null;
}

/** Cermin NormalizePlate backend: "b 1234-xyz" → "B1234XYZ" (kunci pencarian & keunikan per property). */
export function normalizePlate(plate: string | null | undefined): string {
  return (plate ?? "").toUpperCase().replace(/[^A-Z0-9]/g, "");
}

/** Tampilan plat nomor Indonesia: "B1234XYZ" → "B 1234 XYZ"; format lain dikembalikan ternormalisasi. */
export function displayPlate(plate: string | null | undefined): string {
  const p = normalizePlate(plate);
  const m = /^([A-Z]{1,2})(\d{1,4})([A-Z]{0,3})$/.exec(p);
  if (!m) return p;
  return [m[1], m[2], m[3]].filter(Boolean).join(" ");
}

/** Tautan Google Maps dari koordinat GPS; null bila koordinat tidak lengkap/tidak valid. */
export function mapsUrl(lat: number | null | undefined, lng: number | null | undefined): string | null {
  if (lat === null || lat === undefined || lng === null || lng === undefined || !Number.isFinite(lat) || !Number.isFinite(lng)) return null;
  return `https://www.google.com/maps?q=${lat},${lng}`;
}

/** href `tel:` — hanya digit dan "+" awal ("(021) 555-0101" → "tel:0215550101"). */
export function telHref(phone: string | null | undefined): string {
  const raw = (phone ?? "").trim();
  const plus = raw.startsWith("+") ? "+" : "";
  return `tel:${plus}${raw.replace(/\D/g, "")}`;
}

/** Persentase okupansi area parkir (0–100); kapasitas 0 → 0 (atau 100 bila ada kendaraan). */
export function occupancyPct(occupied: number, capacity: number): number {
  if (capacity <= 0) return occupied > 0 ? 100 : 0;
  return Math.min(100, Math.max(0, Math.round((occupied / capacity) * 100)));
}
/** Tone bar okupansi: < 75% success, 75–94% warning, ≥ 95% error (penuh). */
export function occupancyTone(pct: number): "success" | "warning" | "error" {
  return pct >= 95 ? "error" : pct >= 75 ? "warning" : "success";
}

/** Batas atas filter tanggal eksklusif (`to` backend = `<`): "2026-09-27" → "2026-09-28". */
export function nextDay(date: string): string {
  const d = new Date(`${date}T00:00:00Z`);
  if (Number.isNaN(d.getTime())) return date;
  d.setUTCDate(d.getUTCDate() + 1);
  return d.toISOString().slice(0, 10);
}

/** Nilai awal input datetime-local (waktu lokal browser, menit). */
export function localDateTimeInput(d: Date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// ---------- Video evidence incident (P2-SIN-04: mp4/mov/webm, maks 50 MB, tanpa kompresi) ----------
export const VIDEO_ACCEPT: Record<string, string[]> = { "video/mp4": [".mp4"], "video/quicktime": [".mov"], "video/webm": [".webm"] };
export const MAX_VIDEO_BYTES = 50 * 1024 * 1024;
/** null bila valid; selain itu pesan kesalahan untuk pengguna. */
export function validateVideoFile(file: { type: string; size: number; name?: string }): string | null {
  if (!VIDEO_ACCEPT[file.type]) return `Format ${file.name ? `"${file.name}" ` : ""}tidak didukung — gunakan MP4, MOV, atau WebM.`;
  if (file.size <= 0) return "File video kosong.";
  if (file.size > MAX_VIDEO_BYTES) return `Ukuran video ${Math.ceil(file.size / 1024 / 1024)} MB melebihi batas 50 MB.`;
  return null;
}
