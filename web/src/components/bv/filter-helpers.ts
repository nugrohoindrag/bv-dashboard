// Helper murni filter lanjutan (FilterBar / FilterPanelButton): tipe kategori, hitung filter aktif, pecah `FilterSpec.extra`.
import { Children, Fragment, cloneElement, isValidElement, type ReactElement, type ReactNode } from "react";
import { cn } from "@/lib/utils";

export type FilterOption = { value: string; label: string };
export type FilterCategory =
  | { key: string; label: string; icon: string; kind: "options"; param: string; options: FilterOption[] }
  | { key: string; label: string; icon: string; kind: "node"; node: ReactNode; params: string[] }
  | { key: string; label: string; icon: string; kind: "date"; from: string; to: string };

export type UrlFilters = { get: (k: string) => string; set: (patch: Record<string, string | null | undefined>) => void };

/** Jumlah filter aktif pada satu kategori (kategori picker dari halaman tanpa param diketahui = 0). */
export function activeIn(c: FilterCategory, f: UrlFilters): number {
  if (c.kind === "options") return f.get(c.param) ? 1 : 0;
  if (c.kind === "date") return f.get(c.from) || f.get(c.to) ? 1 : 0;
  return c.params.filter((p) => f.get(p)).length;
}

/** Semua param yang dikendalikan kategori (untuk "Hapus semua" & chip). */
export function paramsOf(c: FilterCategory): string[] {
  if (c.kind === "options") return [c.param];
  if (c.kind === "date") return [c.from, c.to];
  return c.params;
}

/**
 * Pecah `FilterSpec.extra`: FilterSelect → kategori daftar pilihan; picker/filter lain → kategori berisi node itu sendiri;
 * Button (aksi halaman, mis. "Generate patrol") → tetap di bar, bukan filter.
 */
export function splitExtra(extra: ReactNode, isFilterSelect: (el: ReactElement) => boolean, isButton: (el: ReactElement) => boolean): { cats: FilterCategory[]; actions: ReactNode[] } {
  const cats: FilterCategory[] = [];
  const actions: ReactNode[] = [];
  const walk = (node: ReactNode) => {
    Children.forEach(node, (ch) => {
      if (!isValidElement(ch)) return;
      if (ch.type === Fragment) return walk((ch.props as { children?: ReactNode }).children);
      const p = ch.props as { param?: string; label?: string; placeholder?: string; options?: FilterOption[]; className?: string };
      if (isButton(ch)) {
        actions.push(cloneElement(ch as ReactElement<{ key?: string }>, { key: `a${actions.length}` }));
      } else if (isFilterSelect(ch) && p.param) {
        cats.push({ key: `x-${p.param}`, label: p.label ?? p.param, icon: "tune", kind: "options", param: p.param, options: p.options ?? [] });
      } else {
        const label = p.label ?? p.placeholder?.replace(/…$/, "") ?? "Lainnya";
        cats.push({ key: `n-${cats.length}-${label}`, label, icon: "tune", kind: "node", params: [], node: cloneElement(ch as ReactElement<{ className?: string }>, { className: cn(p.className, "!w-full") }) });
      }
    });
  };
  walk(extra);
  return { cats, actions };
}
