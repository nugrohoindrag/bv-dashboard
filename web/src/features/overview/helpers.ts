// Helper murni halaman Overview (revamp 29 Sep 2026).
import type { DashboardKpi, DomainDashboard } from "@/api/types";

/**
 * Lokasi ringkas dari path server ("Gedung / Lantai / Ruang / …"): buang segmen yang terulang
 * ("Tower A / Tower A Lantai 12" → "Tower A Lantai 12"), lalu ambil dua segmen terakhir. `keepRoot` (dipakai pada
 * "Semua properti") mempertahankan nama gedung di depan agar item dari gedung berbeda tetap bisa dibedakan.
 */
export function shortLocation(path: string | null | undefined, keepRoot = false): string {
  const parts = (path ?? "").split(" / ").map((s) => s.trim()).filter(Boolean);
  const dedup = parts.filter((p, i) => !(parts[i + 1] && parts[i + 1].toLowerCase().startsWith(p.toLowerCase())));
  const tail = dedup.slice(-2);
  if (keepRoot && parts.length > 0 && !tail.includes(parts[0])) tail.unshift(parts[0]);
  return tail.join(" · ");
}

/** "Selamat pagi/siang/sore/malam" menurut jam lokal. */
export function greeting(d = new Date()): string {
  const h = d.getHours();
  if (h >= 4 && h < 11) return "Selamat pagi";
  if (h >= 11 && h < 15) return "Selamat siang";
  if (h >= 15 && h < 18) return "Selamat sore";
  return "Selamat malam";
}

/** KPI dashboard domain berdasarkan key (undefined bila dashboard belum dimuat / KPI tidak ada). */
export function kpi(dash: DomainDashboard | undefined, key: string): DashboardKpi | undefined {
  return dash?.kpis.find((k) => k.key === key);
}
