// Helper murni Engineering PRD P2 v2.1 §5.5–§5.6 (Asset 360, equipment health NC §14, dokumen equipment). Kode tipe dokumen =
// nilai yang divalidasi backend (api/internal/asset/documents.go). Status health (asset_health) & masa berlaku (validity)
// memakai status map GENERATED lewat StatusBadge.

export const DOCUMENT_TYPES: Record<string, string> = {
  manual: "Manual", warranty: "Warranty", certificate: "Sertifikat", permit: "Izin/Permit", inspection_report: "Laporan inspeksi",
  drawing: "Gambar teknik", contract: "Kontrak", other: "Lainnya",
};
export const documentTypeLabel = (t: string | null | undefined) => (t ? DOCUMENT_TYPES[t] ?? t.replace(/_/g, " ") : "—");

/** Ambang health NC §14 (cermin HealthStatusFor backend) — untuk teks penjelasan, status tetap dari server. */
export function healthStatusFor(score: number): "healthy" | "warning" | "critical" {
  return score >= 90 ? "healthy" : score >= 70 ? "warning" : "critical";
}

/** Tautan faktor health (server) aman dibuka sebagai route internal. */
export function internalLink(link: string | null | undefined): string | null {
  return link && link.startsWith("/") && !link.startsWith("//") ? link : null;
}

/** Rentang default Biaya & Kinerja = 12 bulan terakhir (default server). */
export function lastMonths(months: number, today: Date = new Date()): { from: string; to: string } {
  const to = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  const from = new Date(to);
  from.setMonth(from.getMonth() - months);
  const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  return { from: iso(from), to: iso(to) };
}

/** Tab Asset 360 yang dapat dibuka lewat ?tab= (deep link notifikasi dokumen: ?tab=documents). */
export const ASSET_TABS = ["history", "wo", "pm", "health", "documents", "insight"] as const;
export type AssetTab = (typeof ASSET_TABS)[number];
export function assetTab(v: string | null | undefined): AssetTab {
  return (ASSET_TABS as readonly string[]).includes(v ?? "") ? (v as AssetTab) : "history";
}
