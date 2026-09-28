// Billing › Payments (PRD P4 v2.1 §6; NC §36): daftar pembayaran dengan filter (status, metode, provider, tenant, grup
// penerimaan, rentang tanggal bayar, pencarian) dari URL; detail = drawer /billing/payments/:id (B-13: deep link membuka
// pembayaran ini). Terima Pembayaran = satu penerimaan dialokasikan ke banyak invoice (P4-PAY-05).
import { useMemo, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, FilterSelect, useUrlFilters } from "@/components/bv/datagrid";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import { statusOptions } from "@/lib/status";
import { PaymentDrawer, VerifyPaymentDialog } from "./PaymentDetail";
import { PaymentReceiveDialog } from "./PaymentReceiveDialog";
import { DateRangeFilter, TenantFilter } from "./shared";
import { PAYMENT_METHODS, PROVIDERS, labelOf, methodLabel, money, rangeParam, type Payment } from "./types";

export default function PaymentsPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const location = useLocation();
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  const [receiveOpen, setReceiveOpen] = useState(false);
  const [verifyFor, setVerifyFor] = useState<Payment | null>(null);
  const pid = f.get("property_id") || propertyId;
  const list = useList<Payment>("payments", {
    property_id: pid ?? undefined,
    status: f.get("status") || undefined,
    method: f.get("method") || undefined,
    provider: f.get("provider") || undefined,
    tenant_id: f.get("tenant_id") || undefined,
    invoice_id: f.get("invoice_id") || undefined,
    receipt_group: f.get("receipt_group") || undefined,
    paid_from: rangeParam(f.get("paid_from"), "from"),
    paid_to: rangeParam(f.get("paid_to"), "to"),
    q: f.get("q") || undefined,
  });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const canReceive = can("billing.payments.allocate") && can("billing.payments.verify");

  const columns = useMemo<ColumnDef<Payment, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): nomor pembayaran + invoice satu kolom, metode singkat, satu badge status,
    // satu tanggal. Grup penerimaan, referensi, provider, verifikator, bukti & tanggal dibuat ada di drawer detail.
    { id: "number", header: "Pembayaran", meta: { mobile: "primary" }, cell: ({ row: { original: p } }) => <CellTitle code={p.payment_number} title={p.invoice_number} /> },
    { id: "tenant", header: t("label.tenant"), meta: { mobile: "secondary" }, cell: ({ row: { original: p } }) => <CellText max={180}>{p.tenant_name ?? "—"}</CellText> },
    { id: "amount", header: "Nominal", size: 130, meta: { mobile: "secondary" }, cell: ({ row: { original: p } }) => <div className="tnum whitespace-nowrap text-right font-semibold">{money(p.amount, p.currency_code)}</div> },
    { id: "method", header: "Metode", size: 140, meta: { mobile: "hidden" }, cell: ({ row: { original: p } }) => <CellText max={140} title={`${methodLabel(p.method)} · ${labelOf(PROVIDERS, p.provider_code)}${p.reference ? ` · ${p.reference}` : ""}`}>{methodLabel(p.method)}</CellText> },
    { id: "status", header: t("label.status"), size: 130, meta: { mobile: "status" }, cell: ({ row: { original: p } }) => <StatusBadge objectType="payment" status={p.status} /> },
    {
      id: "paid_at", header: "Tanggal", size: 150, meta: { mobile: "secondary" },
      cell: ({ row: { original: p } }) => (p.paid_at
        ? <span className="tnum whitespace-nowrap text-sm">{fmtDateTime(p.paid_at)}</span>
        : <span className="tnum whitespace-nowrap text-sm text-on-surface-variant" title="Belum dibayar — tanggal dibuat">{fmtDateTime(p.created_at)}</span>),
    },
  ], [t]);

  return (
    <div>
      <PageHeader
        title={t("nav.payments")}
        subtitle="Pembayaran manual diverifikasi Finance (bukti transfer, jumlah diterima dapat disesuaikan); pembayaran online (gateway) sedang ditunda."
        actions={canReceive && <Button icon="payments" onClick={() => setReceiveOpen(true)}>Terima Pembayaran</Button>}
      >
        <FilterBar
          spec={{
            status: statusOptions("payment"),
            presets: [
              { key: "all", label: "Semua", params: {} },
              { key: "verify", label: "Perlu verifikasi", params: { status: "initiated,pending", provider: "manual" } },
              { key: "expired", label: "Kedaluwarsa", params: { status: "expired" } },
              { key: "paid", label: "Berhasil", params: { status: "paid" } },
              { key: "refunded", label: "Refund", params: { status: "refunded" } },
            ],
            extra: (
              <>
                <FilterSelect param="method" label="Metode" options={PAYMENT_METHODS} />
                <FilterSelect param="provider" label="Provider" options={PROVIDERS} />
                <TenantFilter propertyId={pid} value={f.get("tenant_id")} onChange={(v) => f.set({ tenant_id: v })} />
                <DateRangeFilter label="Dibayar" from={f.get("paid_from")} to={f.get("paid_to")} onChange={(a, b) => f.set({ paid_from: a, paid_to: b })} />
              </>
            ),
          }}
        />
      </PageHeader>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/billing/payments/${r.id}${location.search}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={f.isFiltered}
        empty={{ icon: "payments", title: "Belum ada pembayaran", description: "Pembayaran tercatat dari Tenant App (menunggu verifikasi), dicatat Finance per invoice, atau lewat Terima Pembayaran.", action: canReceive ? <Button icon="payments" onClick={() => setReceiveOpen(true)}>Terima Pembayaran</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowActions={(r) => [
          ...(r.allowed_actions.includes("verify") ? [{ label: "Verifikasi…", icon: "task_alt", onSelect: () => setVerifyFor(r) }] : []),
        ]}
      />
      {receiveOpen && <PaymentReceiveDialog onClose={() => setReceiveOpen(false)} initial={{ tenantId: f.get("tenant_id") || undefined }} />}
      {verifyFor && <VerifyPaymentDialog payment={verifyFor} onClose={() => setVerifyFor(null)} />}
      {id && <PaymentDrawer id={id} onClose={() => nav({ pathname: "/billing/payments", search: location.search })} />}
    </div>
  );
}
