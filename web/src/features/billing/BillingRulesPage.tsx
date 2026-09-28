// Billing › Billing Rules (PRD P4 v2.1 P4-BRL-01; D-P4-05): aturan tagihan berulang per property — dasar hitung (tetap per
// unit, per m², per tipe unit, pemakaian meter, persentase, per kendaraan), tarif, periode, pajak, cakupan. Detail/edit =
// drawer /billing/rules/:id. Rule aktif dipakai Billing Run (preview → draft → terbit) dan job generate otomatis.
import { useMemo, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button, Drawer } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { QueryErrorState } from "@/components/bv/common";
import { CellText, CellTitle } from "@/components/bv/cells";
import { useAll, useOne } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { RuleForm } from "./RuleForm";
import { FREQUENCIES, RULE_BASES, basisLabel, invoiceTypeLabel, labelOf, pct, ruleRateSummary, usePropertyName, type BillingRule } from "./types";

export default function BillingRulesPage() {
  const { t } = useTranslation();
  const { id } = useParams();
  const nav = useNavigate();
  const location = useLocation();
  const { propertyId, properties, can } = useAuth();
  const propertyName = usePropertyName();
  const f = useUrlFilters();
  const [createOpen, setCreateOpen] = useState(false);
  const list = useAll<BillingRule>("billing/rules", { property_id: propertyId ?? undefined });
  const q = f.get("q").toLowerCase();
  const rows = (list.data ?? []).filter(
    (r) =>
      (!f.get("type") || r.basis === f.get("type")) &&
      (!f.get("status") || (f.get("status") === "active") === r.is_active) &&
      (!q || r.code.toLowerCase().includes(q) || r.name.toLowerCase().includes(q)),
  );
  const canManage = can("billing.rules.manage");
  const multiProperty = !propertyId && properties.length > 1;

  const columns = useMemo<ColumnDef<BillingRule, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): kode + nama satu kolom, satu baris per sel, status + satu flag
    // (Otomatis). Jatuh tempo, pajak & cakupan unit ada di drawer detail rule.
    { id: "name", header: "Rule", meta: { mobile: "primary" }, cell: ({ row: { original: r } }) => <CellTitle code={r.code} title={r.name} /> },
    { id: "charge", header: "Jenis", size: 150, meta: { mobile: "hidden" }, cell: ({ row: { original: r } }) => <CellText max={150}>{invoiceTypeLabel(r.charge_type)}</CellText> },
    ...(multiProperty ? [{ id: "property", header: "Property", meta: { mobile: "hidden" as const }, cell: ({ row: { original: r } }: { row: { original: BillingRule } }) => <CellText max={160}>{propertyName(r.property_id)}</CellText> }] : []),
    {
      id: "basis", header: "Dasar hitung", size: 180, meta: { mobile: "secondary" },
      cell: ({ row: { original: r } }) => (
        <div className="flex min-w-0 items-center gap-1.5">
          <Icon name={RULE_BASES.find((b) => b.value === r.basis)?.icon ?? "rule"} size={16} className="shrink-0 text-on-surface-variant" />
          <CellText max={160}>{basisLabel(r.basis)}</CellText>
        </div>
      ),
    },
    { id: "rate", header: "Tarif", size: 180, meta: { mobile: "secondary" }, cell: ({ row: { original: r } }) => <CellText max={180} className="tnum">{ruleRateSummary(r)}</CellText> },
    {
      id: "schedule", header: "Periode", size: 160, meta: { mobile: "hidden" },
      cell: ({ row: { original: r } }) => (
        <CellText max={160} title={`${labelOf(FREQUENCIES, r.frequency)} · terbit tgl ${r.issue_day} · jatuh tempo +${r.due_days} hari · pajak ${r.tax_rate === null ? "ikut pengaturan" : pct(r.tax_rate)}`}>
          {`${labelOf(FREQUENCIES, r.frequency)} · tgl ${r.issue_day}`}
        </CellText>
      ),
    },
    {
      id: "status", header: t("label.status"), size: 150, meta: { mobile: "status" },
      cell: ({ row: { original: r } }) => (
        <div className="flex flex-wrap items-center gap-1">
          {r.is_active ? <Badge tone="success">Aktif</Badge> : <Badge tone="neutral">Nonaktif</Badge>}
          {r.auto_generate && <Badge tone="info">Otomatis</Badge>}
        </div>
      ),
    },
  ], [t, multiProperty, propertyName]);

  return (
    <div>
      <PageHeader
        title={t("nav.billing_rules")}
        subtitle="Aturan tagihan berulang per property: dasar hitung, tarif, periode, pajak, dan cakupan unit. Dipakai Billing Run untuk membuat tagihan bulanan."
        actions={canManage && <Button icon="add" onClick={() => setCreateOpen(true)}>Tambah rule</Button>}
      >
        <FilterBar
          spec={{
            type: RULE_BASES.map((b) => ({ value: b.value, label: b.label })),
            status: [{ value: "active", label: "Aktif" }, { value: "inactive", label: "Nonaktif" }],
          }}
        />
      </PageHeader>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/billing/rules/${r.id}${location.search}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={f.isFiltered}
        empty={{ icon: "rule", title: "Belum ada billing rule", description: "Buat rule untuk service charge / IPL (per m² atau tetap), sinking fund (% IPL), utilitas (meter), dan parkir (per kendaraan).", action: canManage ? <Button icon="add" onClick={() => setCreateOpen(true)}>Tambah rule</Button> : undefined }}
        rowActions={canManage ? (r) => [{ label: "Edit", icon: "edit", onSelect: () => nav(`/billing/rules/${r.id}${location.search}`) }] : undefined}
      />
      {createOpen && <RuleForm onClose={() => setCreateOpen(false)} />}
      {id && <RuleLoader id={id} onClose={() => nav({ pathname: "/billing/rules", search: location.search })} />}
    </div>
  );
}

/** /billing/rules/:id — muat rule lalu buka form. */
function RuleLoader({ id, onClose }: { id: string; onClose: () => void }) {
  const q = useOne<BillingRule>("billing/rules", id);
  if (q.isError) return <Drawer open onClose={onClose} title="Billing rule"><QueryErrorState error={q.error} onRetry={() => q.refetch()} /></Drawer>;
  if (!q.data) return null;
  return <RuleForm key={`${q.data.id}-${q.data.version}`} rule={q.data} onClose={onClose} />;
}
