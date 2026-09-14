// Formatter Indonesia (Naming Convention §39–§41; PRD §27): "14 Sep 2026, 09:30", "Rp 1.450.000", relatif "45 m lalu".
import { format, formatDistanceStrict } from "date-fns";
import { id as localeID } from "date-fns/locale";
import { toZonedTime } from "date-fns-tz";

let tz = "Asia/Jakarta";
export function setTimezone(z: string) {
  tz = z || "Asia/Jakarta";
}
export function getTimezone() {
  return tz;
}

export function fmtDateTime(v?: string | Date | null): string {
  if (!v) return "—";
  const d = toZonedTime(typeof v === "string" ? new Date(v) : v, tz);
  return format(d, "d MMM yyyy, HH:mm", { locale: localeID });
}
export function fmtDate(v?: string | Date | null): string {
  if (!v) return "—";
  const d = toZonedTime(typeof v === "string" ? new Date(v) : v, tz);
  return format(d, "d MMM yyyy", { locale: localeID });
}
export function fmtTime(v?: string | Date | null): string {
  if (!v) return "—";
  return format(toZonedTime(typeof v === "string" ? new Date(v) : v, tz), "HH:mm");
}

// "45 m lalu" / "dalam 2 j" — hanya untuk Attention Required & timeline (DS §6), tooltip waktu absolut di komponen.
export function fmtRelative(v?: string | Date | null): string {
  if (!v) return "—";
  const d = typeof v === "string" ? new Date(v) : v;
  const now = new Date();
  const diffMin = Math.round((now.getTime() - d.getTime()) / 60000);
  const abs = Math.abs(diffMin);
  let s: string;
  if (abs < 1) s = "baru saja";
  else if (abs < 60) s = `${abs} m`;
  else if (abs < 60 * 24) s = `${Math.floor(abs / 60)} j ${abs % 60 ? (abs % 60) + " m" : ""}`.trim();
  else s = formatDistanceStrict(d, now, { locale: localeID });
  if (abs < 1) return s;
  return diffMin > 0 ? `${s} lalu` : `dalam ${s}`;
}

export function fmtMinutes(min?: number | null): string {
  if (min === null || min === undefined) return "—";
  const a = Math.abs(min);
  const h = Math.floor(a / 60);
  const m = a % 60;
  const s = h > 0 ? `${h} j${m ? " " + m + " m" : ""}` : `${m} m`;
  return min < 0 ? `-${s}` : s;
}

export function fmtMoney(amount?: number | null, currency = "IDR"): string {
  if (amount === null || amount === undefined) return "—";
  const s = new Intl.NumberFormat("id-ID", { maximumFractionDigits: 0 }).format(amount);
  return currency === "IDR" ? `Rp ${s}` : `${currency} ${s}`;
}

export function fmtNumber(n?: number | null): string {
  if (n === null || n === undefined) return "—";
  return new Intl.NumberFormat("id-ID").format(n);
}

export function initials(name?: string | null): string {
  if (!name) return "?";
  return name
    .split(/\s+/)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? "")
    .join("");
}

export function cn(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(" ");
}
