// Konfigurasi website (Website PRD v1.1). Nilai build-time via VITE_*; default untuk dev lokal.
// Tautan Login / Start Free Trial mengarah ke dashboard (auth yang sudah ada, §35; signup §25).
export const SITE_URL = (import.meta.env.VITE_SITE_URL as string | undefined)?.replace(/\/$/, "") || "https://buildingvision.id";
export const APP_URL = (import.meta.env.VITE_APP_URL as string | undefined)?.replace(/\/$/, "") || "http://localhost:5173";
// API publik: same-origin (/api → Caddy/Vite proxy) atau absolut lewat VITE_API_URL
export const API_BASE = ((import.meta.env.VITE_API_URL as string | undefined)?.replace(/\/$/, "") || "") + "/api/v1";

export const SITE_NAME = "BuildingVision";
export const TAGLINE = "Building Operations Platform";
export const TRIAL_DAYS = 14;
export const SALES_EMAIL = "sales@buildingvision.id";
export const SUPPORT_EMAIL = "support@buildingvision.id";

export const LINKS = {
  login: `${APP_URL}/login`,
  signup: `${APP_URL}/signup`,
  demo: "/book-a-demo",
  download: "/download",
  pricing: "/pricing",
};

export function signupHref(source: string) {
  return `${LINKS.signup}?source=${encodeURIComponent(source)}`;
}
