// API client BuildingVision (TAD §6.1): Bearer access token di memori, refresh via cookie HttpOnly,
// error RFC 9457 problem+json, X-Request-Id, Idempotency-Key untuk POST create/transition.

export interface Problem {
  type: string;
  title: string;
  status: number;
  detail?: string;
  code: string;
  errors?: { field: string; message: string }[];
  request_id?: string;
}

export class ApiError extends Error {
  problem: Problem;
  constructor(p: Problem) {
    super(p.detail || p.title);
    this.problem = p;
  }
  get status() {
    return this.problem.status;
  }
  get code() {
    return this.problem.code;
  }
}

export interface ListResponse<T> {
  data: T[];
  next_cursor: string | null;
  total?: number;
}

type Listener = (token: string | null) => void;

class TokenStore {
  private token: string | null = null;
  private listeners = new Set<Listener>();
  get() {
    return this.token;
  }
  set(t: string | null) {
    this.token = t;
    this.listeners.forEach((l) => l(t));
  }
  subscribe(l: Listener) {
    this.listeners.add(l);
    return () => this.listeners.delete(l);
  }
}
export const tokenStore = new TokenStore();

let refreshing: Promise<boolean> | null = null;

// single-flight: seluruh pemanggil (bootstrap sesi, retry 401) berbagi satu request refresh — refresh token dirotasi
// sehingga dua request paralel dengan cookie yang sama akan terdeteksi server sebagai reuse.
export async function refreshToken(): Promise<boolean> {
  if (!refreshing) {
    refreshing = (async () => {
      try {
        const res = await fetch("/api/v1/auth/refresh", { method: "POST", credentials: "include", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ client: "web" }) });
        if (!res.ok) {
          tokenStore.set(null);
          return false;
        }
        const body = await res.json();
        tokenStore.set(body.access_token);
        return true;
      } catch {
        tokenStore.set(null);
        return false;
      } finally {
        refreshing = null;
      }
    })();
  }
  return refreshing;
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined | null>;
  idempotencyKey?: string;
  ifMatch?: number;
  signal?: AbortSignal;
  retry?: boolean;
}

export function buildQuery(q?: RequestOptions["query"]): string {
  if (!q) return "";
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(q)) {
    if (v === undefined || v === null || v === "") continue;
    p.set(k, String(v));
  }
  const s = p.toString();
  return s ? "?" + s : "";
}

export async function api<T = unknown>(path: string, opts: RequestOptions = {}): Promise<T> {
  const url = (path.startsWith("/") ? path : "/api/v1/" + path) + buildQuery(opts.query);
  const headers: Record<string, string> = { Accept: "application/json" };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  const tok = tokenStore.get();
  if (tok) headers["Authorization"] = "Bearer " + tok;
  if (opts.idempotencyKey) headers["Idempotency-Key"] = opts.idempotencyKey;
  if (opts.ifMatch !== undefined) headers["If-Match"] = `"${opts.ifMatch}"`;
  const res = await fetch(url, { method: opts.method || (opts.body !== undefined ? "POST" : "GET"), headers, body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined, credentials: "include", signal: opts.signal });
  // 401 (termasuk TOKEN_STALE: permission_version naik setelah revoke sesi/ubah role, PRD P0 v2 §24.1) →
  // refresh access token dari cookie lalu ulangi SEKALI secara transparan.
  if (res.status === 401 && opts.retry !== false && !url.includes("/auth/")) {
    if (await refreshToken()) return api<T>(path, { ...opts, retry: false });
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let json: unknown = null;
  try {
    json = text ? JSON.parse(text) : null;
  } catch {
    json = null;
  }
  if (!res.ok) {
    const p = (json as Problem) || { type: "", title: res.statusText, status: res.status, code: "HTTP_" + res.status };
    throw new ApiError(p);
  }
  return json as T;
}

/**
 * Unduh file dari endpoint ber-auth (mis. `reports/{name}?format=csv`): fetch dengan Bearer (+ refresh sekali bila 401),
 * error problem+json → ApiError, nama file dari Content-Disposition.
 */
export async function downloadFile(path: string, query?: RequestOptions["query"], fallbackName = "download"): Promise<void> {
  const url = (path.startsWith("/") ? path : "/api/v1/" + path) + buildQuery(query);
  const get = () => {
    const tok = tokenStore.get();
    return fetch(url, { headers: tok ? { Authorization: "Bearer " + tok } : {}, credentials: "include" });
  };
  let res = await get();
  if (res.status === 401 && (await refreshToken())) res = await get();
  if (!res.ok) {
    let p: Problem | null = null;
    try {
      p = (await res.json()) as Problem;
    } catch {
      p = null;
    }
    throw new ApiError(p ?? { type: "", title: res.statusText, status: res.status, code: "HTTP_" + res.status });
  }
  const blob = await res.blob();
  const name = /filename="?([^";]+)"?/.exec(res.headers.get("Content-Disposition") ?? "")?.[1] ?? fallbackName;
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
}

export const apiBase = "/api/v1";

export function uuid(): string {
  return crypto.randomUUID();
}
