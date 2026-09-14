// DataGrid (DS §4.8): TanStack Table + cursor pagination "Muat lebih banyak", bulk action bar, ⋯ quick actions
// FilterBar (DS §4.9): Status · Priority · Location · Assignee · Team · Date range + search; filter di URL query; preset chips
import { useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef, type RowSelectionState } from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ArrowUpDown, Download, MoreHorizontal, RotateCcw, Search, X } from "lucide-react";
import { Button, Checkbox, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger, Input, NativeSelect, THead, TBody, TD, TH, TR, Table } from "@/components/ui/primitives";
import { LocationPicker, TeamPicker, UserPicker } from "./pickers";
import { ListSkeleton, EmptyState } from "./common";
import { cn } from "@/lib/utils";
import { useAuth } from "@/lib/auth";

export interface DataGridProps<T> {
  columns: ColumnDef<T, unknown>[];
  rows: T[];
  rowId: (r: T) => string;
  onRowClick?: (r: T) => string | void; // return route to navigate
  loading?: boolean;
  isFiltered?: boolean;
  empty?: { message: string; cta?: React.ReactNode };
  hasMore?: boolean;
  onLoadMore?: () => void;
  loadingMore?: boolean;
  total?: number;
  selectable?: boolean;
  bulkActions?: (ids: string[], clear: () => void) => React.ReactNode;
  rowActions?: (r: T) => { label: string; onSelect: () => void; destructive?: boolean }[];
  sort?: string;
  onSort?: (s: string) => void;
  rowClassName?: (r: T) => string | undefined;
}

export function DataGrid<T>({ columns, rows, rowId, onRowClick, loading, isFiltered, empty, hasMore, onLoadMore, loadingMore, selectable, bulkActions, rowActions, sort, onSort, rowClassName }: DataGridProps<T>) {
  const { t } = useTranslation();
  const nav = useNavigate();
  const [selection, setSelection] = useState<RowSelectionState>({});
  const cols = useMemo<ColumnDef<T, unknown>[]>(() => {
    const out: ColumnDef<T, unknown>[] = [];
    if (selectable) {
      out.push({
        id: "_select",
        header: ({ table }) => <Checkbox checked={table.getIsAllRowsSelected()} onCheckedChange={(v) => table.toggleAllRowsSelected(!!v)} aria-label="Pilih semua" />,
        cell: ({ row }) => <Checkbox checked={row.getIsSelected()} onCheckedChange={(v) => row.toggleSelected(!!v)} aria-label="Pilih baris" onClick={(e) => e.stopPropagation()} />,
        size: 32,
      });
    }
    out.push(...columns);
    if (rowActions) {
      out.push({
        id: "_actions",
        header: "",
        size: 40,
        cell: ({ row }) => {
          const acts = rowActions(row.original);
          if (!acts.length) return null;
          return (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon-sm" aria-label="Aksi" onClick={(e) => e.stopPropagation()}>
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent onClick={(e) => e.stopPropagation()}>
                {acts.map((a) => (
                  <DropdownMenuItem key={a.label} destructive={a.destructive} onSelect={a.onSelect}>
                    {a.label}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          );
        },
      });
    }
    return out;
  }, [columns, selectable, rowActions]);
  const table = useReactTable({ data: rows, columns: cols, getCoreRowModel: getCoreRowModel(), getRowId: rowId, state: { rowSelection: selection }, onRowSelectionChange: setSelection, enableRowSelection: !!selectable });
  const selectedIds = Object.keys(selection).filter((k) => selection[k]);

  if (loading) return <ListSkeleton rows={8} />;
  if (!rows.length) return isFiltered ? <EmptyState compact message={t("state.empty_filter")} /> : <EmptyState message={empty?.message ?? t("empty.generic")} cta={empty?.cta} />;

  return (
    <div>
      {selectable && selectedIds.length > 0 && bulkActions && (
        <div className="mb-2 flex items-center gap-3 rounded-md border border-brand-100 bg-brand-50 px-3 py-2 text-sm">
          <span className="font-medium">{selectedIds.length} dipilih</span>
          {bulkActions(selectedIds, () => setSelection({}))}
          <Button variant="ghost" size="sm" className="ml-auto" onClick={() => setSelection({})}>
            <X /> Batal pilih
          </Button>
        </div>
      )}
      <Table>
        <THead>
          {table.getHeaderGroups().map((hg) => (
            <tr key={hg.id}>
              {hg.headers.map((h) => {
                const sortKey = (h.column.columnDef.meta as { sortKey?: string } | undefined)?.sortKey;
                const active = sortKey && sort && sort.replace("-", "") === sortKey;
                const desc = sort?.startsWith("-");
                return (
                  <TH key={h.id} style={{ width: h.getSize() !== 150 ? h.getSize() : undefined }}>
                    {sortKey && onSort ? (
                      <button type="button" className="inline-flex items-center gap-1 hover:text-foreground" onClick={() => onSort(active && !desc ? "-" + sortKey : sortKey)}>
                        {flexRender(h.column.columnDef.header, h.getContext())}
                        {active ? desc ? <ArrowDown className="h-3 w-3" /> : <ArrowUp className="h-3 w-3" /> : <ArrowUpDown className="h-3 w-3 opacity-40" />}
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
              onClick={() => {
                const to = onRowClick?.(row.original);
                if (typeof to === "string") nav(to);
              }}
            >
              {row.getVisibleCells().map((cell) => (
                <TD key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TD>
              ))}
            </TR>
          ))}
        </TBody>
      </Table>
      <div className="flex items-center justify-between border-t border-border px-3 py-2 text-sm text-muted-foreground">
        <span>{t("label.showing", { n: rows.length })}</span>
        {hasMore && (
          <Button variant="secondary" size="sm" onClick={onLoadMore} loading={loadingMore}>
            {t("label.load_more")}
          </Button>
        )}
      </div>
    </div>
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

export function FilterBar({ spec, onExport }: { spec: FilterSpec; onExport?: () => void }) {
  const { t } = useTranslation();
  const f = useUrlFilters();
  const { propertyId } = useAuth();
  const [q, setQ] = useState(f.get("q"));
  const chips = Object.entries(f.all).filter(([k]) => k !== "sort" && k !== "cursor");
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <form
          className="relative"
          onSubmit={(e) => {
            e.preventDefault();
            f.set({ q });
          }}
        >
          <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input className="w-64 pl-8" placeholder={t("label.search")} value={q} onChange={(e) => setQ(e.target.value)} />
        </form>
        {spec.status && (
          <NativeSelect className="w-40" value={f.get("status")} onChange={(e) => f.set({ status: e.target.value })} aria-label="Status">
            <option value="">{t("label.status")}: {t("label.all")}</option>
            {spec.status.map((s) => (
              <option key={s.value} value={s.value}>{s.label}</option>
            ))}
          </NativeSelect>
        )}
        {spec.type && (
          <NativeSelect className="w-40" value={f.get("type")} onChange={(e) => f.set({ type: e.target.value })} aria-label="Tipe">
            <option value="">{t("label.type")}: {t("label.all")}</option>
            {spec.type.map((s) => (
              <option key={s.value} value={s.value}>{s.label}</option>
            ))}
          </NativeSelect>
        )}
        {spec.priority && (
          <NativeSelect className="w-36" value={f.get("priority")} onChange={(e) => f.set({ priority: e.target.value })} aria-label="Prioritas">
            <option value="">{t("label.priority")}: {t("label.all")}</option>
            {["critical", "high", "medium", "low"].map((p) => (
              <option key={p} value={p}>{t(`priority.${p}`)}</option>
            ))}
          </NativeSelect>
        )}
        {spec.severity && (
          <NativeSelect className="w-36" value={f.get("severity")} onChange={(e) => f.set({ severity: e.target.value })} aria-label="Severity">
            <option value="">Severity: {t("label.all")}</option>
            {["critical", "high", "medium", "low"].map((p) => (
              <option key={p} value={p}>{t(`priority.${p}`)}</option>
            ))}
          </NativeSelect>
        )}
        {spec.location && <LocationPicker propertyId={propertyId} value={f.get("location_id") || null} onChange={(id) => f.set({ location_id: id })} placeholder={t("label.location")} className="w-56" />}
        {spec.assignee && <UserPicker propertyId={propertyId} value={f.get("assignee_id") || null} onChange={(id) => f.set({ assignee_id: id })} placeholder={t("label.assignee")} className="w-48" />}
        {spec.team && <TeamPicker propertyId={propertyId} value={f.get("team_id") || null} onChange={(id) => f.set({ team_id: id })} className="w-44" />}
        {spec.dateRange && (
          <span className="inline-flex items-center gap-1">
            <Input type="date" className="w-36" value={f.get("created_from").slice(0, 10)} onChange={(e) => f.set({ created_from: e.target.value })} aria-label="Dari tanggal" />
            <span className="text-muted-foreground">–</span>
            <Input type="date" className="w-36" value={f.get("created_to").slice(0, 10)} onChange={(e) => f.set({ created_to: e.target.value ? e.target.value + "T23:59:59Z" : "" })} aria-label="Sampai tanggal" />
          </span>
        )}
        {spec.extra}
        <span className="ml-auto flex items-center gap-2">
          {onExport && (
            <Button variant="secondary" size="sm" onClick={onExport}>
              <Download /> {t("action.export")}
            </Button>
          )}
          {f.isFiltered && (
            <Button variant="ghost" size="sm" onClick={() => { setQ(""); f.reset(); }}>
              <RotateCcw /> {t("action.reset_filter")}
            </Button>
          )}
        </span>
      </div>
      {(spec.presets?.length || chips.length > 0) && (
        <div className="flex flex-wrap items-center gap-1.5">
          {spec.presets?.map((p) => {
            const active = Object.entries(p.params).every(([k, v]) => f.get(k) === v);
            return (
              <button key={p.key} type="button" onClick={() => f.set(active ? Object.fromEntries(Object.keys(p.params).map((k) => [k, null])) : p.params)} className={cn("rounded-full border px-2.5 py-0.5 text-xs", active ? "border-brand-600 bg-brand-600 text-white" : "border-border bg-card text-muted-foreground hover:bg-muted")}>
                {p.label}
              </button>
            );
          })}
          {chips.map(([k, v]) => (
            <span key={k} className="inline-flex items-center gap-1 rounded-full bg-neutral-soft px-2 py-0.5 text-xs text-neutral-text">
              {k}: {v.length > 14 ? v.slice(0, 8) + "…" : v}
              <button type="button" aria-label={`Hapus filter ${k}`} onClick={() => f.set({ [k]: null })}>
                <X className="h-3 w-3" />
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
