// Billing › Invoices (PRD P4 v2.1 §5.2; NC §34–§35): daftar tagihan dengan filter lengkap (status, jenis, sumber, umur piutang,
// rentang jatuh tempo/terbit, terbuka/lewat jatuh tempo, tenant, pencarian) yang dibaca dari URL — halaman lain (Receivables,
// Aging, laporan, Billing Run) menautkan ke sini dengan ?aging=…&tenant_id=…&type=…&issued_from=…&status=…&billing_run_id=…&open=true.
// Detail = drawer /billing/invoices/:id. Buat invoice manual, edit draft, impor CSV, terbitkan draft massal.
import { useMemo, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Drawer } from "@/components/ui/primitives";
import { DataGrid, FilterBar, FilterSelect, useUrlFilters } from "@/components/bv/datagrid";
import { QueryErrorState, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { useInvalidate, useList, useOne } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { statusOptions } from "@/lib/status";
import { cn } from "@/lib/utils";
import { ImportInvoicesDialog } from "./ImportInvoicesDialog";
import { InvoiceDrawer } from "./InvoiceDetail";
import { InvoiceForm } from "./InvoiceForm";
import { DateRangeFilter, TenantFilter } from "./shared";
import { AGING_BUCKETS, INVOICE_SOURCES, INVOICE_TYPES, dateOnly, dueDay, fmtDay, fmtPeriod, invoiceTypeLabel, money, rangeParam, type Invoice } from "./types";

/** Periode ringkas: satu bulan kalender penuh → "Sep 2026"; selain itu rentang tanggal. */
function shortPeriod(start: string | null, end: string | null): string {
  const a = dateOnly(start);
  const b = dateOnly(end);
  const [y, m, d] = a.split("-").map(Number);
  if (y && m && d === 1 && b) {
    const last = new Date(Date.UTC(y, m, 0)).toISOString().slice(0, 10);
    if (b === last) return fmtDay(a).replace(/^1 /, "");
  }
  return fmtPeriod(start, end);
}

/** @deprecated Pakai `fmtMoney` (@/lib/format, P4-FIN-04). Diekspor ulang untuk impor lama (Inventory). */
export { rp } from "./types";

export default function InvoicesPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const location = useLocation();
  const toast = useToast();
  const invalidate = useInvalidate();
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  const [createOpen, setCreateOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [editDraft, setEditDraft] = useState<Invoice | null>(null);
  const pid = f.get("property_id") || propertyId;
  const query = {
    property_id: pid ?? undefined,
    status: f.get("status") || undefined,
    type: f.get("type") || undefined,
    source: f.get("source") || undefined,
    source_id: f.get("source_id") || undefined,
    billing_run_id: f.get("billing_run_id") || undefined,
    aging: f.get("aging") || undefined,
    tenant_id: f.get("tenant_id") || undefined,
    unit_location_id: f.get("unit_location_id") || undefined,
    due_from: rangeParam(f.get("due_from"), "from"),
    due_to: rangeParam(f.get("due_to"), "to"),
    issued_from: rangeParam(f.get("issued_from"), "from"),
    issued_to: rangeParam(f.get("issued_to"), "to"),
    q: f.get("q") || undefined,
    open: f.get("open") === "true" || undefined,
    overdue: f.get("overdue") === "true" || undefined,
  };
  const list = useList<Invoice>("invoices", query);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const canCreate = can("billing.invoices.create");
  const canIssue = can("billing.invoices.issue");

  const columns = useMemo<ColumnDef<Invoice, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): nomor + tenant satu kolom, satu badge status. Sumber, deskripsi,
    // rincian dibayar/kredit, hari lewat & denda ada di drawer detail (menu ⋮ → "Lihat detail").
    {
      id: "number", header: "Invoice", meta: { mobile: "primary" },
      cell: ({ row: { original: r } }) => <CellTitle code={r.invoice_number ?? "Belum bernomor"} title={r.tenant_name ?? "Tanpa tenant"} />,
    },
    { id: "unit", header: "Unit", meta: { mobile: "hidden" }, cell: ({ row: { original: r } }) => <CellText max={150} muted={!r.unit_label}>{r.unit_label ?? "—"}</CellText> },
    {
      id: "type", header: "Jenis · periode", size: 190, meta: { mobile: "hidden" },
      cell: ({ row: { original: r } }) => {
        const label = r.period_start ? `${invoiceTypeLabel(r.invoice_type)} · ${shortPeriod(r.period_start, r.period_end)}` : invoiceTypeLabel(r.invoice_type);
        return <CellText max={200} title={r.period_start ? `${invoiceTypeLabel(r.invoice_type)} · ${fmtPeriod(r.period_start, r.period_end)}` : undefined}>{label}</CellText>;
      },
    },
    { id: "total", header: "Total", size: 130, meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <div className="tnum whitespace-nowrap text-right font-semibold">{money(r.total_amount, r.currency_code)}</div> },
    {
      id: "outstanding", header: "Sisa", size: 130, meta: { mobile: "secondary" },
      cell: ({ row: { original: r } }) => {
        const open = ["issued", "partially_paid", "overdue"].includes(r.status);
        if (!open) return <div className="whitespace-nowrap text-right text-sm text-on-surface-variant">{r.status === "paid" ? "Lunas" : "—"}</div>;
        return <div className={cn("tnum whitespace-nowrap text-right font-semibold", r.status === "overdue" && "text-critical-text")}>{money(r.outstanding_amount, r.currency_code)}</div>;
      },
    },
    {
      id: "due", header: "Jatuh tempo", size: 130, meta: { mobile: "secondary" },
      cell: ({ row: { original: r } }) => (
        <span className={cn("tnum whitespace-nowrap text-sm", r.days_overdue > 0 && "font-semibold text-critical-text")} title={r.days_overdue > 0 ? `${r.days_overdue} hari lewat jatuh tempo` : undefined}>{fmtDay(dueDay(r))}</span>
      ),
    },
    { id: "status", header: t("label.status"), size: 130, meta: { mobile: "status" }, cell: ({ row: { original: r } }) => <StatusBadge objectType="invoice" status={r.status} /> },
  ], [t]);

  const issueSelected = async (ids: string[], clear: () => void) => {
    const drafts = rows.filter((r) => ids.includes(r.id) && r.allowed_actions.includes("issue"));
    if (!drafts.length) return toast.info("Tidak ada draft yang dapat diterbitkan pada pilihan ini");
    let ok = 0;
    const failed: string[] = [];
    for (const d of drafts) {
      try {
        await api(`invoices/${d.id}/issue`, { body: {}, idempotencyKey: uuid() });
        ok += 1;
      } catch (e) {
        failed.push(`${d.tenant_name ?? d.unit_label ?? "draft"}: ${(e as Error).message}`);
      }
    }
    invalidate("list", "one", "all");
    clear();
    if (ok) toast.success(`${ok} invoice diterbitkan & dikirim ke tenant`);
    if (failed.length) toast.warning(`${failed.length} gagal diterbitkan — ${failed.slice(0, 2).join("; ")}`);
  };

  const closeDrawer = () => nav({ pathname: "/billing/invoices", search: location.search });

  return (
    <div>
      <PageHeader
        title={t("nav.invoices")}
        subtitle="Tagihan tenant/unit: service charge / IPL, utilitas, parkir, sinking fund, denda, biaya tambahan. Nomor INV diberikan saat terbit; invoice terbit dikoreksi lewat void atau credit note."
        actions={
          <>
            {can("billing.invoices.import") && <Button variant="secondary" icon="upload_file" onClick={() => setImportOpen(true)}>Impor CSV</Button>}
            {canCreate && <Button icon="add" onClick={() => setCreateOpen(true)}>Buat Invoice</Button>}
          </>
        }
      >
        <FilterBar
          spec={{
            status: statusOptions("invoice"),
            type: INVOICE_TYPES,
            presets: [
              { key: "all", label: "Semua", params: {} },
              { key: "open", label: "Belum lunas", params: { open: "true" } },
              { key: "overdue", label: "Lewat jatuh tempo", params: { overdue: "true" } },
              { key: "draft", label: "Draft", params: { status: "draft" } },
            ],
            extra: (
              <>
                <FilterSelect param="source" label="Sumber" options={INVOICE_SOURCES} />
                <FilterSelect param="aging" label="Umur piutang" options={AGING_BUCKETS} />
                <TenantFilter propertyId={pid} value={f.get("tenant_id")} onChange={(v) => f.set({ tenant_id: v })} />
                <DateRangeFilter label="Jatuh tempo" from={f.get("due_from")} to={f.get("due_to")} onChange={(a, b) => f.set({ due_from: a, due_to: b })} />
                <DateRangeFilter label="Terbit" from={f.get("issued_from")} to={f.get("issued_to")} onChange={(a, b) => f.set({ issued_from: a, issued_to: b })} />
              </>
            ),
          }}
        />
      </PageHeader>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/billing/invoices/${r.id}${location.search}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={f.isFiltered}
        empty={{ icon: "request_quote", title: "Belum ada invoice", description: "Invoice dibuat manual, dari Billing Run bulanan, impor CSV, biaya Work Order, atau modul sewa/hotel.", action: canCreate ? <Button icon="add" onClick={() => setCreateOpen(true)}>Buat Invoice</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        selectable={canIssue}
        bulkActions={canIssue ? (ids, clear) => <Button size="sm" icon="send" onClick={() => issueSelected(ids, clear)}>Terbitkan draft terpilih</Button> : undefined}
        rowActions={(r) => [
          ...(r.allowed_actions.includes("update") ? [{ label: "Edit draft", icon: "edit", onSelect: () => setEditDraft(r) }] : []),
        ]}
      />
      {createOpen && <InvoiceForm onClose={() => setCreateOpen(false)} onSaved={(v) => nav(`/billing/invoices/${v.id}${location.search}`)} />}
      {editDraft && <EditDraftLoader id={editDraft.id} onClose={() => setEditDraft(null)} />}
      {importOpen && <ImportInvoicesDialog onClose={() => setImportOpen(false)} />}
      {id && <InvoiceDrawer id={id} onClose={closeDrawer} />}
    </div>
  );
}

/** Daftar tidak memuat item — ambil invoice lengkap sebelum membuka form edit draft. */
function EditDraftLoader({ id, onClose }: { id: string; onClose: () => void }) {
  const q = useOne<Invoice>("invoices", id);
  if (q.isError) return <Drawer open onClose={onClose} title="Edit draft invoice"><QueryErrorState error={q.error} onRetry={() => q.refetch()} /></Drawer>;
  if (!q.data) return null;
  return <InvoiceForm invoice={q.data} onClose={onClose} />;
}
