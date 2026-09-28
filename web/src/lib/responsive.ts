// Responsive foundation (PRD P0 §22): breakpoint tunggal untuk shell, DataGrid, dan komponen aksi mobile.
// mobile < 768 · tablet 768–1279 · desktop ≥ 1280. Kartu tabel (DataGrid) berlaku < 640 (sm).
import { useSyncExternalStore } from "react";

export const BREAKPOINTS = { sm: 640, md: 768, xl: 1280 } as const;
export type Breakpoint = "mobile" | "tablet" | "desktop";

export const MQ = {
  mobile: `(max-width: ${BREAKPOINTS.md - 1}px)`,
  tablet: `(min-width: ${BREAKPOINTS.md}px) and (max-width: ${BREAKPOINTS.xl - 1}px)`,
  desktop: `(min-width: ${BREAKPOINTS.xl}px)`,
  cards: `(max-width: ${BREAKPOINTS.sm - 1}px)`,
} as const;

function matches(query: string): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return false;
  return window.matchMedia(query).matches;
}

export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (cb) => {
      if (typeof window === "undefined" || typeof window.matchMedia !== "function") return () => {};
      const mql = window.matchMedia(query);
      mql.addEventListener?.("change", cb);
      return () => mql.removeEventListener?.("change", cb);
    },
    () => matches(query),
    () => false,
  );
}

/** Tanpa matchMedia (test/SSR) dianggap desktop. */
export function useBreakpoint(): Breakpoint {
  const mobile = useMediaQuery(MQ.mobile);
  const tablet = useMediaQuery(MQ.tablet);
  return mobile ? "mobile" : tablet ? "tablet" : "desktop";
}
