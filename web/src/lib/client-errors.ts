// Pelaporan error klien (PRD P0 v2 §24.4): ErrorBoundary → POST /client-errors. Best-effort: hanya saat login,
// dibatasi ~5 laporan/menit, tidak pernah melempar error (pelapor error tidak boleh menjadi sumber error baru).
import { api, tokenStore } from "./api";

const WINDOW_MS = 60_000;
const MAX_PER_WINDOW = 5;
let sent: number[] = [];

export function resetClientErrorLimiter() {
  sent = [];
}

export function reportClientError(error: unknown, extra?: { componentStack?: string | null; route?: string }): boolean {
  try {
    if (!tokenStore.get()) return false;
    const now = Date.now();
    sent = sent.filter((t) => now - t < WINDOW_MS);
    if (sent.length >= MAX_PER_WINDOW) return false;
    sent.push(now);
    const e = error as { message?: string; stack?: string } | null;
    const message = (e?.message || String(error) || "unknown error").slice(0, 500);
    const stack = [e?.stack, extra?.componentStack].filter(Boolean).join("\n--- component stack ---\n").slice(0, 8000);
    const route = extra?.route ?? (typeof window !== "undefined" ? window.location.pathname + window.location.search : "");
    const release = (import.meta.env.VITE_RELEASE as string | undefined) ?? import.meta.env.MODE;
    void api("client-errors", { body: { message, stack, route, source: "web", release, user_agent: typeof navigator !== "undefined" ? navigator.userAgent : "" }, retry: false }).catch(() => undefined);
    return true;
  } catch {
    return false;
  }
}
