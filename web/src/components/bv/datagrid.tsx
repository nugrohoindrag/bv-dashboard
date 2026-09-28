// DataGrid: TanStack Table di atas tabel design system (.bv-table: header solid primary, hairline baris), cursor pagination
// "Muat lebih banyak", bulk action bar, aksi baris via RowActionMenu (bv).
// PRD P0 §22–§23: state loading/error/empty first-class (error query ≠ "kosong"); < 640px baris dirender sebagai kartu
// (meta kolom `mobile`: primary | secondary | status | hidden; default = kolom pertama + 2 berikutnya + status + aksi).
// FilterBar (DS §4.9): Status · Priority · Location · Assignee · Team · Date range + search; filter di URL query; preset chips
import { useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef, type RowSelectionState } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { FilterChip, RowActionMenu } from "@buildingvision/ui/bv";
import { Button, Checkbox, NativeSelect, SearchInput, THead, TBody, TD, TH, TR, Table } from "@/components/ui/primitives";
import { LocationPicker, TeamPicker, UserPicker } from "./pickers";
import { FilterPanelButton } from "./filter-panel";
import { activeIn, splitExtra, type FilterCategory } from "./filter-helpers";
import { EmptyState, QueryErrorState, TableSkeleton } from "./states";
import { cn } from "@/lib/utils";
import { useAuth } from "@/lib/auth";
import { MQ, useMediaQuery } from "@/lib/responsive";

/** Meta kolom yang dibaca DataGrid (ColumnDef.meta). */
export interface DataGridColumnMeta { sortKey?: string; nowrap?: boolean; mobile?: "primary" | "secondary" | "status" | "hidden" }

export interface DataGridProps<T> {
  columns: ColumnDef<T, unknown>[];
  rows: T[];
  rowId: (r: T) => string;
  onRowClick?: (r: T) => string | void; // return route to navigate
  /** Alias `isLoading`. */
  loading?: boolean;
  isLoading?: boolean;
  /** Error query (mis. `list.error`) → ErrorState di area tabel, bukan "kosong". */
  error?: unknown;
  onRetry?: () => void;
  isFiltered?: boolean;
  empty?: { title?: string; description?: string; action?: React.ReactNode; icon?: string; /** @deprecated */ message?: string; /** @deprecated */ cta?: React.ReactNode };
  /** auto = kartu < 640px; table = selalu tabel (scroll horizontal); cards = selalu kartu. */
  layout?: "auto" | "table" | "cards";
  hasMore?: boolean;
  onLoadMore?: () => void;
  loadingMore?: boolean;
  total?: number;
  selectable?: boolean;
  bulkActions?: (ids: string[], clear: () => void) => React.ReactNode;
  rowActions?: (r: T) => { label: string; onSelect: () => void; destructive?: boolean; icon?: string }[];
  sort?: string;
  onSort?: (s: string) => void;
  rowClassName?: (r: T) => string | undefined;
}

// Kolom identitas/waktu/aksi tidak boleh terpotong baris; kolom judul/lokasi boleh membungkus.
function nowrapColumn(id: string, meta: unknown): boolean {
  if ((meta as { nowrap?: boolean } | undefined)?.nowrap) return true;
  return id === "_select" || id === "_actions" || id === "actions" || /(^id$|number|_at$|^due|^code$|status|priority|severity)/.test(id);
}

const PAGE_SIZES = [10, 20, 30] as const;
const PAGE_SIZE_KEY = "bv.pageSize";
function storedPageSize(): number {
  try {
    const v = Number(localStorage.getItem(PAGE_SIZE_KEY));
    return (PAGE_SIZES as readonly number[]).includes(v) ? v : 10;
  } catch {
    return 10;
  }
}

export function DataGrid<T>({ columns, rows, rowId, onRowClick, loading, isLoading, error, onRetry, isFiltered, empty, hasMore, onLoadMore, loadingMore, total, selectable, bulkActions, rowActions, sort, onSort, rowClassName, layout = "auto" }: DataGridProps<T>) {
  const { t } = useTranslation();
  const nav = useNavigate();
  const narrow = useMediaQuery(MQ.cards);
  const asCards = layout === "cards" || (layout === "auto" && narrow);
  const [selection, setSelection] = useState<RowSelectionState>({});
  // Pagination tampilan (29 Sep 2026): default 10 baris, pilihan 10/20/30 (diingat browser). Data tetap diambil per halaman
  // API (cursor); halaman berikutnya memicu onLoadMore bila baris yang sudah dimuat habis.
  const [pageSize, setPageSize] = useState<number>(storedPageSize);
  const [pageIndex, setPageIndex] = useState(0);
  const { search } = useLocation();
  useEffect(() => setPageIndex(0), [search]);
  const start = pageIndex * pageSize;
  const end = start + pageSize;
  const lastLoadedPage = Math.max(0, Math.ceil(rows.length / pageSize) - 1);
  useEffect(() => {
    if (pageIndex > lastLoadedPage && !hasMore && !loadingMore) setPageIndex(lastLoadedPage);
  }, [pageIndex, lastLoadedPage, hasMore, loadingMore]);
  const pageRows = useMemo(() => rows.slice(start, end), [rows, start, end]);
  const canNext = end < rows.length || !!hasMore;
  const goNext = () => {
    if (end >= rows.length && hasMore) onLoadMore?.();
    setPageIndex((i) => i + 1);
  };
  const changeSize = (n: number) => {
    setPageSize(n);
    setPageIndex(0);
    try { localStorage.setItem(PAGE_SIZE_KEY, String(n)); } catch { /* abaikan */ }
  };
  const clickRow = (r: T) => {
    const to = onRowClick?.(r);
    if (typeof to === "string") nav(to);
  };
  const cols = useMemo<ColumnDef<T, unknown>[]>(() => {
    const out: ColumnDef<T, unknown>[] = [];
    // "Lihat detail" (29 Sep 2026): baris yang bisa dibuka selalu punya item ini di menu ⋮ — digabung ke rowActions bila ada,
    // atau kolom menu sendiri bila halaman tidak punya kolom aksi (halaman dengan kolom "actions" sendiri tidak diubah).
    const hasOwnActions = columns.some((c) => c.id === "actions" || c.id === "_actions");
    if (selectable) {
      out.push({
        id: "_select",
        header: ({ table }) => <Checkbox checked={table.getIsAllRowsSelected()} onCheckedChange={(v) => table.toggleAllRowsSelected(!!v)} aria-label="Pilih semua" />,
        cell: ({ row }) => <Checkbox checked={row.getIsSelected()} onCheckedChange={(v) => row.toggleSelected(!!v)} aria-label="Pilih baris" onClick={(e) => e.stopPropagation()} />,
        size: 32,
      });
    }
    out.push(...columns);
    if (rowActions || (onRowClick && !hasOwnActions)) {
      out.push({
        id: "_actions",
        header: "",
        size: 40,
        cell: ({ row }) => {
          const acts = [
            ...(onRowClick ? [{ label: "Lihat detail", icon: "visibility", onSelect: () => clickRow(row.original) }] : []),
            ...(rowActions?.(row.original) ?? []),
          ] as { label: string; onSelect: () => void; destructive?: boolean; icon?: string }[];
          if (!acts.length) return null;
          return (
            <span onClick={(e) => e.stopPropagation()} className="inline-flex">
              <RowActionMenu label="Aksi" items={acts.map((a, i) => ({ id: String(i), label: a.label, icon: a.icon, onClick: a.onSelect, danger: a.destructive }))} />
            </span>
          );
        },
      });
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- clickRow/onRowClick berganti tiap render; menu memakai versi terbaru saat diklik
  }, [columns, selectable, rowActions, !!onRowClick]);
  const table = useReactTable({ data: pageRows, columns: cols, getCoreRowModel: getCoreRowModel(), getRowId: rowId, state: { rowSelection: selection }, onRowSelectionChange: setSelection, enableRowSelection: !!selectable });
  const selectedIds = Object.keys(selection).filter((k) => selection[k]);

  // error hanya menggantikan tabel bila belum ada data (refetch yang gagal tetap menampilkan data lama)
  if (error && !rows.length) return <QueryErrorState error={error} onRetry={onRetry} />;
  if (loading || isLoading) return <TableSkeleton rows={8} columns={Math.min(6, columns.length)} />;
  if (!rows.length)
    return isFiltered ? (
      <EmptyState compact title={t("state.empty_filter")} description={t("state.empty_filter_desc")} />
    ) : (
      <EmptyState icon={empty?.icon} title={empty?.title ?? empty?.message ?? t("empty.generic")} description={empty?.description} action={empty?.action ?? empty?.cta} />
    );
  return (
    // Soft Frame: tabel selalu berada di panel putih bersudut besar (di dalam Card panel ini menyatu dengan kartu)
    <div className={cn(!asCards && "rounded-[var(--radius-xl)] bg-surface px-2 pt-1")}>
      {selectable && selectedIds.length > 0 && bulkActions && (
        <div className="mb-2 flex items-center gap-3 rounded-[var(--radius-md)] px-3 py-2 text-sm" style={{ backgroundColor: "var(--color-primary-container)", color: "var(--color-on-primary-container)" }}>
          <span className="font-bold">{selectedIds.length} dipilih</span>
          {bulkActions(selectedIds, () => setSelection({}))}
          <Button variant="ghost" size="sm" className="ml-auto" onClick={() => setSelection({})}>
            <Icon name="close" size={16} /> Batal pilih
          </Button>
        </div>
      )}
      {asCards ? (
        <MobileCards table={table} onRowClick={onRowClick ? clickRow : undefined} rowClassName={rowClassName} />
      ) : (
      <Table>
        <THead>
          {table.getHeaderGroups().map((hg) => (
            <tr key={hg.id}>
              {hg.headers.map((h) => {
                const sortKey = (h.column.columnDef.meta as DataGridColumnMeta | undefined)?.sortKey;
                const active = sortKey && sort && sort.replace("-", "") === sortKey;
                const desc = sort?.startsWith("-");
                return (
                  <TH key={h.id} style={{ width: h.getSize() !== 150 ? h.getSize() : undefined }}>
                    {sortKey && onSort ? (
                      <button type="button" className="inline-flex items-center gap-1 opacity-90 hover:opacity-100" onClick={() => onSort(active && !desc ? "-" + sortKey : sortKey)}>
                        {flexRender(h.column.columnDef.header, h.getContext())}
                        {active ? desc ? <Icon name="arrow_downward" size={12} /> : <Icon name="arrow_upward" size={12} /> : <Icon name="swap_vert" size={12} className="opacity-40" />}
                      </button>
                    ) : (
                      flexRender(h.column.columnDef.header, h.getContext())
                    )}
                  </TH>
                );
              })}
            </tr>
          ))}
        </THead>
        <TBody>
          {table.getRowModel().rows.map((row) => (
            <TR
              key={row.id}
              data-state={row.getIsSelected() ? "selected" : undefined}
              className={cn(onRowClick && "cursor-pointer", rowClassName?.(row.original))}
              onClick={() => clickRow(row.original)}
            >
              {row.getVisibleCells().map((cell) => (
                <TD key={cell.id} className={cn(nowrapColumn(cell.column.id, cell.column.columnDef.meta) && "whitespace-nowrap", (cell.column.id === "_actions" || cell.column.id === "actions") && "text-right")}>
                  {(cell.column.columnDef.meta as DataGridColumnMeta | undefined)?.mobile === "primary" ? (
                    <div className="max-w-[220px]">{flexRender(cell.column.columnDef.cell, cell.getContext())}</div>
                  ) : (
                    flexRender(cell.column.columnDef.cell, cell.getContext())
                  )}
                </TD>
              ))}
            </TR>
          ))}
        </TBody>
      </Table>
      )}
      {pageRows.length === 0 && (loadingMore || hasMore) && <div className="px-3 py-6 text-center text-sm text-on-surface-variant">Memuat data…</div>}
      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border px-3 py-2 text-sm text-on-surface-variant">
        <span className="tabular-nums">
          {pageRows.length ? `${start + 1}–${start + pageRows.length}` : "0"} dari {total ?? rows.length}{total === undefined && hasMore ? "+" : ""}
        </span>
        <span className="flex items-center gap-2">
          <label className="flex items-center gap-2">
            <span className="hidden sm:inline">Baris per halaman</span>
            <NativeSelect className="w-20" value={String(pageSize)} onChange={(e) => changeSize(Number(e.target.value))} aria-label="Baris per halaman">
              {PAGE_SIZES.map((n) => <option key={n} value={n}>{n}</option>)}
            </NativeSelect>
          </label>
          <Button variant="secondary" size="icon-sm" icon="chevron_left" aria-label="Halaman sebelumnya" disabled={pageIndex === 0} onClick={() => setPageIndex((i) => Math.max(0, i - 1))} />
          <span className="min-w-12 text-center tabular-nums">Hal. {pageIndex + 1}</span>
          <Button variant="secondary" size="icon-sm" icon="chevron_right" aria-label="Halaman berikutnya" disabled={!canNext} loading={loadingMore && pageRows.length === 0} onClick={goNext} />
        </span>
      </div>
    </div>
  );
}

// ---------- Mode kartu (< 640px) ----------
type AnyTable<T> = ReturnType<typeof useReactTable<T>>;
const SYSTEM_COLS = new Set(["_select", "_actions", "actions"]);
function pickMobileColumns(ids: { id: string; meta?: DataGridColumnMeta }[]) {
  const data = ids.filter((c) => !SYSTEM_COLS.has(c.id) && c.meta?.mobile !== "hidden");
  const explicit = data.some((c) => c.meta?.mobile);
  const status = data.find((c) => c.meta?.mobile === "status") ?? (explicit ? undefined : data.find((c) => /status/.test(c.id)));
  const rest = data.filter((c) => c !== status);
  const primary = rest.find((c) => c.meta?.mobile === "primary") ?? rest[0];
  const secondary = explicit ? rest.filter((c) => c.meta?.mobile === "secondary") : rest.filter((c) => c !== primary).slice(0, 2);
  return { primary: primary?.id, status: status?.id, secondary: secondary.map((c) => c.id) };
}
function MobileCards<T>({ table, onRowClick, rowClassName }: { table: AnyTable<T>; onRowClick?: (r: T) => void; rowClassName?: (r: T) => string | undefined }) {
  const cols = table.getAllLeafColumns().map((c) => ({ id: c.id, meta: c.columnDef.meta as DataGridColumnMeta | undefined, header: c.columnDef.header }));
  const pick = pickMobileColumns(cols);
  const actionsId = cols.find((c) => c.id === "_actions" || c.id === "actions")?.id;
  const headerOf = (id: string) => {
    const h = cols.find((c) => c.id === id)?.header;
    return typeof h === "string" ? h : "";
  };
  return (
    <ul className="divide-y divide-border overflow-hidden rounded-[var(--radius-lg)] border border-border bg-surface" data-testid="datagrid-cards">
      {table.getRowModel().rows.map((row) => {
        const cell = (id?: string) => {
          const c = id ? row.getVisibleCells().find((x) => x.column.id === id) : undefined;
          return c ? flexRender(c.column.columnDef.cell, c.getContext()) : null;
        };
        return (
          <li key={row.id} className={cn("flex flex-col gap-2 px-3 py-3", onRowClick && "cursor-pointer active:bg-surface-container-low", rowClassName?.(row.original))} onClick={onRowClick ? () => onRowClick(row.original) : undefined}>
            {/* status/flag membungkus ke baris berikut bila tidak muat; judul minimal 60% lebar kartu */}
            <div className="flex flex-wrap items-start justify-between gap-x-2 gap-y-1.5">
              <div className="min-w-[60%] flex-1 text-body font-medium text-on-surface">{cell(pick.primary)}</div>
              {pick.status && <div className="max-w-full">{cell(pick.status)}</div>}
            </div>
            {pick.secondary.length > 0 && (
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
                {pick.secondary.map((id) => (
                  <div key={id} className="contents">
                    <dt className="text-caption text-on-surface-variant">{headerOf(id)}</dt>
                    <dd className="min-w-0 break-words text-on-surface">{cell(id)}</dd>
                  </div>
                ))}
              </dl>
            )}
            {actionsId && <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>{cell(actionsId)}</div>}
          </li>
        );
      })}
    </ul>
  );
}

// ---------- FilterBar ----------
export interface FilterSpec {
  status?: { value: string; label: string }[];
  priority?: boolean;
  severity?: boolean;
  location?: boolean;
  assignee?: boolean;
  team?: boolean;
  dateRange?: boolean;
  type?: { value: string; label: string }[];
  /** Preset chip. Preset dengan `params: {}` = "Semua": bila ada, preset saling eksklusif (tab-like, PRD P1 v2 §38). */
  presets?: { key: string; label: string; params: Record<string, string> }[];
  extra?: React.ReactNode;
}

export function useUrlFilters() {
  const [sp, setSp] = useSearchParams();
  const get = (k: string) => sp.get(k) ?? "";
  const set = (patch: Record<string, string | null | undefined>) => {
    const next = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (v === null || v === undefined || v === "") next.delete(k);
      else next.set(k, v);
    }
    setSp(next, { replace: true });
  };
  const all = Object.fromEntries(sp.entries());
  return { get, set, all, reset: () => setSp(new URLSearchParams(), { replace: true }), isFiltered: [...sp.keys()].some((k) => k !== "sort") };
}

type Preset = NonNullable<FilterSpec["presets"]>[number];
/**
 * Status aktif preset + patch URL saat diklik. Tanpa preset "Semua" (params kosong): preset aditif (perilaku lama).
 * Dengan "Semua": preset eksklusif — klik mengganti semua kunci preset lain; "Semua" aktif bila tak satu pun kunci preset terisi.
 */
export function presetState(presets: Preset[], p: Preset, get: (k: string) => string): { active: boolean; next: Record<string, string | null> } {
  const exclusive = presets.some((x) => Object.keys(x.params).length === 0);
  const keys = [...new Set(presets.flatMap((x) => Object.keys(x.params)))];
  const clearAll = Object.fromEntries(keys.map((k) => [k, null])) as Record<string, string | null>;
  const own = Object.entries(p.params);
  if (own.length === 0) return { active: !keys.some((k) => get(k)), next: clearAll };
  const matches = own.every(([k, v]) => get(k) === v);
  const active = matches && (!exclusive || keys.every((k) => k in p.params || !get(k)));
  const off = Object.fromEntries(own.map(([k]) => [k, null])) as Record<string, string | null>;
  return { active, next: active ? off : exclusive ? { ...clearAll, ...p.params } : p.params };
}

/** Select filter tambahan yang tersimpan di URL (mis. sla_status, category, request_type) — untuk `FilterSpec.extra`. */
export function FilterSelect({ param, label, options, className }: { param: string; label: string; options: { value: string; label: string }[]; className?: string }) {
  const { t } = useTranslation();
  const f = useUrlFilters();
  return (
    <NativeSelect className={cn("w-[calc(50%-4px)] sm:w-40", className)} value={f.get(param)} onChange={(e) => f.set({ [param]: e.target.value })} aria-label={label}>
      <option value="">{label}: {t("label.all")}</option>
      {options.map((o) => (
        <option key={o.value} value={o.value}>{o.label}</option>
      ))}
    </NativeSelect>
  );
}

/**
 * Bar filter (29 Sep 2026, referensi "Advance Filter"): satu baris bersih — preset (pill) di kiri; cari, tombol Filter
 * (panel kategori ⟶ pilihan) dan Export di kanan; baris chip filter aktif berlabel manusiawi + "Hapus semua".
 * API `FilterSpec` tidak berubah: FilterSelect di `extra` menjadi kategori daftar pilihan, picker/filter lain menjadi
 * kategori berisi node-nya, Button di `extra` (aksi halaman) tetap tampil di bar.
 */
export function FilterBar({ spec, onExport }: { spec: FilterSpec; onExport?: () => void }) {
  const { t } = useTranslation();
  const f = useUrlFilters();
  const { propertyId } = useAuth();
  const [q, setQ] = useState(f.get("q"));
  const priorities = ["critical", "high", "medium", "low"].map((p) => ({ value: p, label: t(`priority.${p}`) }));

  const { cats: extraCats, actions } = splitExtra(spec.extra, (el) => el.type === FilterSelect, (el) => el.type === Button);
  const cats: FilterCategory[] = [
    ...(spec.status ? [{ key: "status", label: t("label.status"), icon: "flag", kind: "options" as const, param: "status", options: spec.status }] : []),
    ...(spec.type ? [{ key: "type", label: t("label.type"), icon: "category", kind: "options" as const, param: "type", options: spec.type }] : []),
    ...(spec.priority ? [{ key: "priority", label: t("label.priority"), icon: "priority_high", kind: "options" as const, param: "priority", options: priorities }] : []),
    ...(spec.severity ? [{ key: "severity", label: "Severity", icon: "warning", kind: "options" as const, param: "severity", options: priorities }] : []),
    ...(spec.location ? [{ key: "location", label: t("label.location"), icon: "location_on", kind: "node" as const, params: ["location_id"], node: <LocationPicker propertyId={propertyId} value={f.get("location_id") || null} onChange={(id) => f.set({ location_id: id })} placeholder={t("label.location")} className="w-full" /> }] : []),
    ...(spec.assignee ? [{ key: "assignee", label: t("label.assignee"), icon: "person", kind: "node" as const, params: ["assignee_id"], node: <UserPicker propertyId={propertyId} value={f.get("assignee_id") || null} onChange={(id) => f.set({ assignee_id: id })} placeholder={t("label.assignee")} className="w-full" /> }] : []),
    ...(spec.team ? [{ key: "team", label: "Tim", icon: "groups", kind: "node" as const, params: ["team_id"], node: <TeamPicker propertyId={propertyId} value={f.get("team_id") || null} onChange={(id) => f.set({ team_id: id })} className="w-full" /> }] : []),
    ...(spec.dateRange ? [{ key: "date", label: "Tanggal dibuat", icon: "date_range", kind: "date" as const, from: "created_from", to: "created_to" }] : []),
    ...extraCats,
  ];
  const activeCount = cats.reduce((n, c) => n + activeIn(c, f), 0);

  // chip filter aktif: kunci preset tampil sebagai pill terpilih, bukan chip; kunci internal disembunyikan
  const presetKeys = new Set((spec.presets ?? []).flatMap((p) => Object.keys(p.params)));
  const hidden = new Set(["sort", "cursor", "new", "tab"]);
  const known = new Map<string, { label: string; value: (v: string) => string }>();
  for (const c of cats) {
    if (c.kind === "options") known.set(c.param, { label: c.label, value: (v) => c.options.find((o) => o.value === v)?.label ?? v });
    else if (c.kind === "date") {
      known.set(c.from, { label: `${c.label} dari`, value: (v) => v.slice(0, 10) });
      known.set(c.to, { label: `${c.label} s/d`, value: (v) => v.slice(0, 10) });
    } else for (const p of c.params) known.set(p, { label: c.label, value: () => "dipilih" });
  }
  known.set("q", { label: "Cari", value: (v) => v });
  const isId = (v: string) => /^[0-9a-f]{8}-[0-9a-f]{4}-/i.test(v);
  const humanKey = (k: string) => k.replace(/_id$/, "").replace(/_/g, " ");
  const chips = Object.entries(f.all)
    .filter(([k]) => !hidden.has(k) && !presetKeys.has(k))
    .map(([k, v]) => {
      const m = known.get(k);
      return { k, text: m ? `${m.label}: ${m.value(v)}` : `${humanKey(k)}: ${isId(v) ? "dipilih" : v.length > 18 ? v.slice(0, 16) + "…" : v}` };
    });

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        {spec.presets?.length ? (
          <div className="-mx-1 flex min-w-0 flex-1 basis-full items-center gap-1.5 overflow-x-auto px-1 py-0.5 [mask-image:linear-gradient(to_right,black_calc(100%-40px),transparent)] [scrollbar-width:none] lg:basis-0" role="group" aria-label="Tampilan cepat">
            {spec.presets.map((p) => {
              const { active, next } = presetState(spec.presets!, p, f.get);
              return (
                <FilterChip key={p.key} selected={active} onClick={() => f.set(next)} style={{ flexShrink: 0 }}>
                  {p.label}
                </FilterChip>
              );
            })}
          </div>
        ) : null}
        <div className={cn("flex w-full items-center gap-2 lg:w-auto", !spec.presets?.length && "lg:w-full")}>
          <form
            className={cn("min-w-0 flex-1 lg:w-64 lg:flex-none", !spec.presets?.length && "lg:w-72")}
            onSubmit={(e) => {
              e.preventDefault();
              f.set({ q });
            }}
          >
            <SearchInput placeholder={t("label.search")} value={q} onChange={(e) => setQ(e.target.value)} onBlur={() => q !== f.get("q") && f.set({ q })} />
          </form>
          <FilterPanelButton cats={cats} f={f} activeCount={activeCount} />
          {actions}
          {onExport && (
            <Button variant="secondary" size="sm" icon="download" onClick={onExport} aria-label={t("action.export")}>
              <span className="hidden sm:inline">{t("action.export")}</span>
            </Button>
          )}
        </div>
      </div>
      {chips.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          {chips.map((c) => (
            <span key={c.k} className="inline-flex h-7 items-center gap-1 rounded-full border border-border bg-surface pl-3 pr-1.5 text-xs font-medium text-on-surface">
              {c.text}
              <button type="button" aria-label={`Hapus filter ${c.text}`} onClick={() => { if (c.k === "q") setQ(""); f.set({ [c.k]: null }); }} className="inline-flex h-5 w-5 items-center justify-center rounded-full text-on-surface-variant hover:bg-surface-container">
                <Icon name="close" size={13} />
              </button>
            </span>
          ))}
          <button type="button" className="ml-1 text-xs font-semibold text-primary hover:underline" onClick={() => { setQ(""); f.set(Object.fromEntries(chips.map((c) => [c.k, null]))); }}>
            Hapus semua
          </button>
        </div>
      )}
    </div>
  );
}
