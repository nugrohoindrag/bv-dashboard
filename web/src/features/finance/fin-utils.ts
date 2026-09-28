// Helper bersama layar Financial Operations (PRD P4 v2.1 §5–§9): format uang (P4-FIN-04 — satu helper `fmtMoney` +
// turunan ringkas untuk KPI/grafik), periode & tahun, property halaman (URL ?property_id → property header), izin tingkat
// organization, unduhan file berkas → base64, dan normalisasi tautan drill-down server. Tanpa JSX (lihat fin-ui.tsx).
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { api } from "@/lib/api";
import { useAuth, type Principal } from "@/lib/auth";
import { fmtMoney } from "@/lib/format";
import type { Query } from "@/api/hooks";

// ---------- angka & uang ----------
const grouper = new Intl.NumberFormat("id-ID", { maximumFractionDigits: 0 });

/** "1.500.000" (tanpa "Rp") — input uang & sel matriks. */
export function fmtGroup(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "";
  return grouper.format(n);
}

/** Teks input uang → rupiah bulat (hanya digit; titik/koma/spasi/"Rp" diabaikan). Kosong → null. */
export function parseAmount(text: string): number | null {
  const digits = text.replace(/[^0-9]/g, "");
  if (!digits) return null;
  const n = Number(digits);
  return Number.isSafeInteger(n) ? n : null;
}

/** Uang bertanda eksplisit: "+Rp 1.000" / "−Rp 1.000" / "Rp 0". */
export function fmtSigned(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  if (n === 0) return fmtMoney(0);
  return `${n > 0 ? "+" : "−"}${fmtMoney(Math.abs(n))}`;
}

const dec = (v: number) => new Intl.NumberFormat("id-ID", { maximumFractionDigits: v < 10 ? 2 : 1 }).format(v);

/** Uang ringkas untuk kartu KPI & ringkasan: "Rp 1,25 M" (miliar), "Rp 12,5 jt", di bawah 1 juta nilai penuh. */
export function fmtMoneyShort(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  const a = Math.abs(n);
  const s = n < 0 ? "−" : "";
  if (a >= 1e12) return `${s}Rp ${dec(a / 1e12)} T`;
  if (a >= 1e9) return `${s}Rp ${dec(a / 1e9)} M`;
  if (a >= 1e6) return `${s}Rp ${dec(a / 1e6)} jt`;
  return `${s}${fmtMoney(a)}`;
}

/** Uang di kartu ringkasan: nilai penuh bila < Rp 100 juta (presisi), ringkas bila lebih besar agar muat ("Rp 1,85 M"). */
export function fmtMoneyTile(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  return Math.abs(n) >= 1e8 ? fmtMoneyShort(n) : fmtMoney(n);
}

/** Label sumbu grafik uang: "1,2 M", "250 jt", "900 rb", "0". */
export function fmtAxisMoney(v: number): string {
  const a = Math.abs(v);
  const s = v < 0 ? "−" : "";
  if (a >= 1e12) return `${s}${dec(a / 1e12)} T`;
  if (a >= 1e9) return `${s}${dec(a / 1e9)} M`;
  if (a >= 1e6) return `${s}${dec(a / 1e6)} jt`;
  if (a >= 1e3) return `${s}${dec(a / 1e3)} rb`;
  return `${s}${a}`;
}

/** Persen dengan satu desimal: "12,5%"; `signed` menambah "+" untuk nilai positif. */
export function fmtPct(n: number | null | undefined, opts: { signed?: boolean; digits?: number } = {}): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  const s = new Intl.NumberFormat("id-ID", { maximumFractionDigits: opts.digits ?? 1 }).format(n);
  return `${opts.signed && n > 0 ? "+" : ""}${s}%`;
}

/** Kuantitas meter (desimal ≤ 3): "1.234,5". */
export function fmtQty(n: number | null | undefined, digits = 3): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  return new Intl.NumberFormat("id-ID", { maximumFractionDigits: digits }).format(n);
}

/** Share bagian terhadap total (0 bila total 0), satu desimal. */
export function sharePct(part: number, total: number): number {
  if (!total) return 0;
  return Math.round((part / total) * 1000) / 10;
}

// ---------- tanggal & periode ----------
export const MONTHS_SHORT = ["Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"];
export const MONTHS_LONG = ["Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"];

export const pad2 = (n: number) => String(n).padStart(2, "0");
/** Tanggal lokal YYYY-MM-DD. */
export const isoDay = (d: Date) => `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
/** Periode YYYY-MM (tanggal lokal). */
export const periodOf = (d: Date) => `${d.getFullYear()}-${pad2(d.getMonth() + 1)}`;
export const currentPeriod = () => periodOf(new Date());
export const currentYear = () => new Date().getFullYear();
export const todayISO = () => isoDay(new Date());

/** "2026-09" → "September 2026". */
export function periodLabel(p: string | null | undefined): string {
  if (!p) return "—";
  const [y, m] = p.split("-").map(Number);
  return y && m >= 1 && m <= 12 ? `${MONTHS_LONG[m - 1]} ${y}` : p;
}

/** Tanggal ISO (YYYY-MM-DD) → "14 Sep 2026" tanpa konversi zona waktu. */
export function fmtDay(v: string | null | undefined): string {
  if (!v) return "—";
  const [y, m, d] = v.slice(0, 10).split("-").map(Number);
  if (!y || !m || !d) return v;
  return `${d} ${MONTHS_SHORT[m - 1]} ${y}`;
}

/** Periode sebelumnya/berikutnya dari YYYY-MM. */
export function shiftPeriod(p: string, delta: number): string {
  const [y, m] = p.split("-").map(Number);
  const d = new Date(y, m - 1 + delta, 1);
  return periodOf(d);
}

/** Rentang tanggal satu bulan / satu tahun (inklusif). */
export function monthBounds(year: number, month: number): { from: string; to: string } {
  const last = new Date(year, month, 0).getDate();
  return { from: `${year}-${pad2(month)}-01`, to: `${year}-${pad2(month)}-${pad2(last)}` };
}
export function yearBounds(year: number): { from: string; to: string } {
  return { from: `${year}-01-01`, to: `${year}-12-31` };
}

/** Pilihan tahun: `back` tahun ke belakang s/d `fwd` ke depan dari tahun acuan (menurun). */
export function yearOptions(center: number, back = 3, fwd = 1): number[] {
  const out: number[] = [];
  for (let y = center + fwd; y >= center - back; y--) out.push(y);
  return out;
}

/** Nilai `<input type="datetime-local">` untuk waktu lokal sekarang. */
export function localDateTimeInput(d: Date = new Date()): string {
  return `${isoDay(d)}T${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

// ---------- izin ----------
function permMatch(list: string[], perm: string): boolean {
  const set = new Set(list);
  if (set.has(perm) || set.has("*")) return true;
  const [m, o, a] = perm.split(".");
  return set.has(`${m}.*`) || set.has(`${m}.${o}.*`) || set.has(`${m}.*.${a}`);
}

/** Grant tingkat organization (scope tanpa property & tanpa building) — webhook keluar & pemetaan akun default (P4-INT-02/04). */
export function hasOrgWide(principal: Principal | null | undefined, perm: string): boolean {
  if (!principal) return false;
  return principal.properties.some((s) => s.property_id === null && !s.scope_location_id && permMatch(s.permissions, perm));
}

// ---------- data ----------
/** GET objek tunggal (laporan/ringkasan) dengan kunci "one" — ikut di-invalidate aksi generik `useAction`. */
export function useGet<T>(path: string, query: Query = {}, opts: { enabled?: boolean; staleTime?: number; refetchInterval?: number } = {}) {
  return useQuery({
    queryKey: ["one", path, query],
    enabled: opts.enabled,
    queryFn: ({ signal }) => api<T>(path, { query, signal }),
    staleTime: opts.staleTime ?? 15_000,
    refetchInterval: opts.refetchInterval,
  });
}

/**
 * Property halaman: `?property_id` di URL (ada tapi kosong = semua property) → property header → (bila `required`)
 * property pertama yang dapat diakses. Setter menulis ke URL (replace) sehingga tab/segarkan tetap konsisten; `reset` =
 * parameter lain yang ikut diubah/dihapus (null) dalam SATU pembaruan URL (dua setSearchParams berturut-turut saling menimpa).
 */
export function usePropertyParam(required = false): [string | null, (id: string | null, reset?: Record<string, string | null>) => void] {
  const { propertyId, properties } = useAuth();
  const [sp, setSp] = useSearchParams();
  let pid: string | null = sp.has("property_id") ? sp.get("property_id") || null : propertyId;
  if (!pid && required) pid = propertyId ?? properties[0]?.id ?? null;
  const set = (id: string | null, reset: Record<string, string | null> = {}) => {
    const n = new URLSearchParams(sp);
    n.set("property_id", id ?? "");
    for (const [k, v] of Object.entries(reset)) {
      if (v) n.set(k, v);
      else n.delete(k);
    }
    setSp(n, { replace: true });
  };
  return [pid, set];
}

/** Baca berkas → base64 tanpa prefiks data URL (impor mutasi bank). */
export function fileToBase64(file: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => {
      const s = String(r.result ?? "");
      const i = s.indexOf(",");
      resolve(i >= 0 ? s.slice(i + 1) : s);
    };
    r.onerror = () => reject(r.error ?? new Error("Berkas tidak dapat dibaca"));
    r.readAsDataURL(file);
  });
}

/**
 * Tautan drill-down dari server → route SPA. Menormalkan tautan yang tidak cocok dengan route web
 * (`/billing/meter-readings/{id}` → `/billing/meters/readings/{id}`, `/work-orders?…` → `/operations/work-orders?…`)
 * dan menolak tautan eksternal.
 */
export function appLink(link: string | null | undefined): string | null {
  if (!link || !link.startsWith("/") || link.startsWith("//")) return null;
  if (link.startsWith("/billing/meter-readings/")) return link.replace("/billing/meter-readings/", "/billing/meters/readings/");
  if (link === "/work-orders" || link.startsWith("/work-orders?")) return "/operations" + link;
  return link;
}

/** Salin teks ke clipboard (fallback: seleksi textarea sementara). */
export async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const ta = document.createElement("textarea");
  ta.value = text;
  document.body.appendChild(ta);
  ta.select();
  document.execCommand("copy");
  ta.remove();
}
