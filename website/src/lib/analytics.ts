// Funnel analytics (Website PRD §40): event ringan ke public API, tanpa data pribadi; anonymous id acak di localStorage.
import { API_BASE } from "./config";

export type SiteEvent =
  | "page_viewed" | "solution_viewed" | "pricing_viewed" | "download_apps_viewed" | "app_download_clicked"
  | "start_trial_clicked" | "login_clicked" | "book_demo_clicked" | "demo_requested";

const ANON_KEY = "bv.anon_id";
function anonymousId(): string | undefined {
  try {
    let v = localStorage.getItem(ANON_KEY);
    if (!v) {
      v = crypto.randomUUID();
      localStorage.setItem(ANON_KEY, v);
    }
    return v;
  } catch {
    return undefined;
  }
}

interface Queued { event: string; source_page: string; anonymous_id?: string; properties?: Record<string, unknown>; occurred_at: string }
let queue: Queued[] = [];
let timer: number | undefined;

export function track(event: SiteEvent, properties?: Record<string, unknown>) {
  if (typeof window === "undefined") return;
  queue.push({ event, source_page: location.pathname, anonymous_id: anonymousId(), properties, occurred_at: new Date().toISOString() });
  if (!timer) timer = window.setTimeout(flush, 1200);
}

export function flush() {
  timer = undefined;
  const events = queue.splice(0, 20);
  if (events.length === 0) return;
  const body = JSON.stringify({ events });
  try {
    if (navigator.sendBeacon) {
      navigator.sendBeacon(`${API_BASE}/public/events`, new Blob([body], { type: "application/json" }));
      return;
    }
  } catch {
    /* fallthrough */
  }
  fetch(`${API_BASE}/public/events`, { method: "POST", headers: { "Content-Type": "application/json" }, body, keepalive: true }).catch(() => undefined);
}

if (typeof window !== "undefined") {
  window.addEventListener("pagehide", flush);
}
