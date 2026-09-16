import React from 'react';
import { AdvancedDataTable } from '../components/data-display/AdvancedDataTable.js';
import type { AdvancedDataTableProps } from '../components/data-display/AdvancedDataTable.js';

/**
 * The BuildingVision data table: `AdvancedDataTable` with the console's
 * reference configuration as its defaults.
 *
 * The reference is the Customer Order list — a title with the row count under
 * it, the search box and density toggle, a solid primary header, and only the
 * columns the screen needs. The mirror component reaches that look only when
 * every caller remembers the same three props, and across forty-odd tables
 * they did not: some opened with a checkbox column and a "Delete Selected"
 * bar that deleted nothing, some with an expand chevron that expanded into
 * nothing, some with no title at all. The rules live here instead:
 *
 * - **Selection exists only when something acts on it.** The checkbox column
 *   and bulk bar appear when `onBulkDelete` or `onBulkExport` is given.
 * - **The expand chevron exists only when there is a row to expand into.**
 *   It appears when `renderExpandedRow` is given.
 * - **Every table is titled**, and `count` puts the row count under the title
 *   the way the reference does ("9 order"), so the subtitle is not restated
 *   by hand at every call site.
 *
 * `selectable` and `expandable` can still be forced, for the rare table that
 * needs it, but a caller should not need to.
 */
export interface DataTableProps<T extends { id: string }> extends Omit<AdvancedDataTableProps<T>, 'title'> {
  title: string;
  /**
   * Row count and its noun, rendered under the title as "12 order". Pass the
   * noun in the product language and the singular form; the count is not
   * pluralised, which is how Indonesian counts things anyway.
   */
  count?: { total: number; noun: string };
}

export function DataTable<T extends { id: string; [key: string]: any }>({
  title,
  count,
  subtitle,
  selectable,
  expandable,
  onBulkDelete,
  onBulkExport,
  renderExpandedRow,
  searchable = true,
  ...rest
}: DataTableProps<T>) {
  const resolvedSubtitle =
    subtitle ?? (count ? `${count.total.toLocaleString('id-ID')} ${count.noun}` : undefined);

  return (
    <AdvancedDataTable
      {...rest}
      title={title}
      subtitle={resolvedSubtitle}
      searchable={searchable}
      selectable={selectable ?? Boolean(onBulkDelete || onBulkExport)}
      expandable={expandable ?? Boolean(renderExpandedRow)}
      onBulkDelete={onBulkDelete}
      onBulkExport={onBulkExport}
      renderExpandedRow={renderExpandedRow}
    />
  );
}
