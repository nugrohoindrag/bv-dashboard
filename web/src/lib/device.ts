// Label perangkat sesi login (PRD P0 v2 §24.1) dari client + user agent.
import type { Session } from "@/api/types";

/** Label perangkat ringkas dari user agent. */
export function deviceLabel(s: Pick<Session, "client" | "user_agent">): string {
  const ua = s.user_agent ?? "";
  const os = /Android/i.test(ua) ? "Android" : /iPhone|iPad|iOS/i.test(ua) ? "iOS" : /Windows/i.test(ua) ? "Windows" : /Mac OS/i.test(ua) ? "macOS" : /Linux/i.test(ua) ? "Linux" : "";
  const br = /Edg\//.test(ua) ? "Edge" : /Chrome\//.test(ua) ? "Chrome" : /Firefox\//.test(ua) ? "Firefox" : /Safari\//.test(ua) ? "Safari" : "";
  const client = { web: "Web", mobile: "Staff App", tenant: "Tenant App", staff: "Staff App" }[s.client] ?? s.client;
  return [client, [br, os].filter(Boolean).join(" · ")].filter(Boolean).join(" — ");
}
