// Smoke render layar PRD P4 v2.1 Financial Operations dengan API tiruan: Receivables (aging drill-down, deep link log penagihan,
// statement), Utilities (deep link pembacaan), Rekonsiliasi (konfirmasi massal), Budget (matriks & aksi), Dashboard Finance
// (attention dari respons + deep link dinormalkan, KPI uang ringkas), Accounting (webhook tingkat organization), Sinking Fund,
// Deposit, dan laporan keuangan (uang fmtMoney + tautan breakdown).
import { render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/lib/i18n";

const auth = { perms: [] as string[], orgWide: false };
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    can: (p: string) => auth.perms.includes(p),
    principal: { id: "u1", full_name: "Finance", organization_id: "o1", roles: [], team_ids: [], lead_team_ids: [], permissions: auth.perms, properties: [{ property_id: auth.orgWide ? null : "p1", permissions: auth.perms }] },
    propertyId: "p1",
    properties: [{ id: "p1", name: "Menara Demo", code: "MD" }],
  }),
}));

import { ToastProvider } from "@/components/bv/common";
import ReceivablesPage from "@/features/billing/ReceivablesPage";
import UtilitiesPage from "@/features/billing/UtilitiesPage";
import ReconciliationPage from "@/features/billing/ReconciliationPage";
import SinkingFundPage from "@/features/billing/SinkingFundPage";
import DepositsPage from "@/features/billing/DepositsPage";
import BudgetsPage from "./BudgetsPage";
import AccountingPage from "./AccountingPage";
import DomainDashboardPage from "@/features/dashboards/DomainDashboardPage";
import ReportsPage from "@/features/reports/ReportsPage";

const now = new Date().toISOString();
const list = (data: unknown[]) => ({ data, next_cursor: null });
const agingRow = { kind: "tenant", id: "t1", label: "PT Maju Jaya", property_id: "p1", property_name: "Menara Demo", tenant_id: "t1", unit_location_id: null, current: 0, d1_30: 1_500_000, d31_60: 0, d61_90: 0, d90_plus: 2_000_000, total: 3_500_000, invoice_count: 2, oldest_days_overdue: 95 };
const aging = { group_by: "tenant", as_of: now, buckets: [], rows: [agingRow], totals: { ...agingRow, kind: "total", id: null, label: "Total", tenant_id: null }, currency_code: "IDR" };
const log = { id: "l1", property_id: "p1", tenant_id: "t1", tenant_name: "PT Maju Jaya", unit_location_id: null, unit_label: null, invoice_ids: ["i1"], invoice_numbers: ["INV-2026-000001"], channel: "phone", contact_person: "Bu Rina", outcome: "promise_to_pay", notes: "Janji transfer Jumat", promise_date: "2026-10-02", promise_amount: 3_500_000, promise_status: "open", follow_up_on: null, created_by_name: "Finance", created_at: now, allowed_actions: ["view", "update", "mark_kept", "mark_broken", "cancel_promise"], version: 1 };
const statement = { property_id: "p1", property_name: "Menara Demo", tenant_id: "t1", tenant_name: "PT Maju Jaya", unit_location_id: null, unit_label: null, from: "2026-04-01", to: "2026-09-28", opening_balance: 1_000_000, total_debit: 3_500_000, total_credit: 1_000_000, closing_balance: 3_500_000, outstanding_amount: 3_500_000, credit_balance: 0, deposit_balance: 5_000_000, currency_code: "IDR", generated_at: now,
  entries: [{ date: "2026-05-01T03:00:00Z", kind: "invoice", reference: "INV-2026-000001", description: "IPL Mei", debit: 3_500_000, credit: 0, balance: 4_500_000, object_type: "invoice", object_id: "i1" }, { date: "2026-05-10T03:00:00Z", kind: "payment", reference: "RCP-2026-000001", description: "Pembayaran", debit: 0, credit: 1_000_000, balance: 3_500_000, object_type: "payment", object_id: "pay1" }] };
const reading = { id: "r1", meter_id: "m1", meter_number: "EL-1203", meter_type: "electricity", property_id: "p1", location_name: "Unit 1203", unit_number: "1203", reading_value: 900, previous_value: 1000, usage: 0, unit_label: "kWh", read_at: now, period: "2026-09", source: "web", status: "flagged", anomaly: "rollback", notes: null, recorded_by_name: "Teknisi", reviewed_by_name: null, reviewed_at: null, review_note: null, billed: false, photo_count: 0, allowed_actions: ["view", "approve", "reject"], version: 1 };
const line = (id: string, over: Record<string, unknown>) => ({ id, line_no: 2, txn_date: "2026-09-01", description: "TRSF INV-2026-000001", reference: null, amount: 3_500_000, balance: null, status: "suggested", match_type: "invoice", suggested_payment_id: null, suggested_payment_number: null, suggested_invoice_id: "i1", suggested_invoice_number: "INV-2026-000001", suggested_party: "PT Maju Jaya", match_score: 95, payment_id: null, payment_number: null, invoice_id: null, invoice_number: null, difference: 0, matched_by_name: null, matched_at: null, note: null, allowed_actions: ["view", "confirm", "match", "ignore"], ...over });
const imp = { id: "s1", property_id: "p1", property_name: "Menara Demo", bank_account_id: null, bank_account: null, file_name: "mutasi-sep.csv", format: "csv", period_from: "2026-09-01", period_to: "2026-09-30", line_count: 2, credit_count: 2, matched_count: 0, suggested_count: 1, unmatched_count: 1, ignored_count: 0, credit_total: 4_000_000, matched_total: 0, status: "open", imported_by_name: "Finance", imported_at: now,
  lines: [line("sl1", {}), line("sl2", { status: "unmatched", match_score: null, suggested_invoice_id: null, suggested_invoice_number: null, amount: 500_000, allowed_actions: ["view", "match", "ignore"] })] };
const budget = { id: "b1", property_id: "p1", property_name: "Menara Demo", fiscal_year: 2026, revision: 1, name: "Budget operasional", status: "draft", notes: null, approved_by_name: null, approved_at: null, created_by_name: "Manager", created_at: now, revenue_total: 1_200_000, cost_total: 600_000,
  lines: [{ kind: "revenue", category: "ipl", label: "IPL", months: Array(12).fill(100_000), total: 1_200_000 }, { kind: "cost", category: "cleaning", label: "Kebersihan", months: Array(12).fill(50_000), total: 600_000 }], allowed_actions: ["view", "update", "approve"], version: 3 };
const dashboard = {
  domain: "finance", property_id: "p1", location_id: null, from: "2026-08-30", to: "2026-09-28", generated_at: now,
  kpis: [{ key: "outstanding", label: "Outstanding", value: 2_500_000_000, unit: "idr", severity: "normal", hint: "Sisa tagihan terbuka", drill_down: "/billing/invoices?open=true" }, { key: "collection_rate", label: "Collection Rate", value: 91.5, unit: "pct", severity: "normal", hint: "…", drill_down: "/reports/collection" }],
  breakdowns: { aging: [], overdue_tenant: [], revenue_type: [{ key: "service_charge", label: "service_charge", values: { amount: 5_000_000 }, drill_down: "/billing/invoices?type=service_charge" }], cost_category: [] },
  attention: [{ category: "meter_flagged", severity: "warning", object_type: "meter_reading", object_id: "r1", label: "EL-1203", title: "Pembacaan meter perlu review — rollback", status: "", priority: null, location_path: null, due_at: null, age_minutes: 30, since: now, assignee_name: null, allowed_actions: ["view"], deep_link: "/billing/meter-readings/r1" }],
  attention_total: 1,
};
const report = { name: "aging", property_id: "p1", from: "2026-09-01T00:00:00Z", to: "2026-09-28T00:00:00Z", summary: { current: 0, d1_30: 1_500_000, d31_60: 0, d61_90: 0, d90_plus: 2_000_000, total: 3_500_000, invoices: 2 }, series: [], breakdowns: { tenant: [{ key: "t1", label: "PT Maju Jaya", values: { current: 0, d1_30: 1_500_000, d31_60: 0, d61_90: 0, d90_plus: 2_000_000, total: 3_500_000, invoices: 2 } }], property: [], type: [] } };

function respond(url: string): unknown {
  const u = new URL(url, "http://localhost");
  const p = u.pathname.replace("/api/v1/", "");
  switch (p) {
    case "billing/aging": return aging;
    case "billing/collections/worklist": return list([]);
    case "billing/collection-logs": return list([log]);
    case "billing/statement": return statement;
    case "tenants": return list([{ id: "t1", property_id: "p1", tenant_code: "T-01", name: "PT Maju Jaya", tenant_type: "company", contact_name: null, contact_phone: null, contact_email: null, status: "active", units: [], open_requests: 0, version: 1 }]);
    case "billing/meter-readings": return list([reading]);
    case "billing/meter-readings/r1": return reading;
    case "billing/bank-statements/s1": return imp;
    case "finance/budgets/b1": return budget;
    case "finance/budgets": return list([budget]);
    case "finance/categories": return { revenue: [{ key: "ipl", label: "IPL" }], cost: [{ key: "cleaning", label: "Kebersihan" }] };
    case "dashboards/finance": return dashboard;
    case "finance/webhooks": return list([{ id: "w1", name: "Accurate Online", url: "https://erp.contoh.co.id/hook", event_types: [], is_active: true, secret_hint: "••••ab12", last_delivery_at: null, last_status: null, pending_count: 0, failed_count: 2, created_at: now, version: 1 }]);
    case "billing/sinking-fund": return { property_id: "p1", balance: 150_000_000, opening: 100_000_000, receipts: 60_000_000, usage: 10_000_000, adjustments: 0, receipts_period: 20_000_000, usage_period: 10_000_000, billed: 70_000_000, outstanding: 10_000_000, entries: [{ id: "f1", property_id: "p1", entry_type: "usage", amount: -10_000_000, entry_date: "2026-09-10", description: "Ganti pompa", invoice_id: null, invoice_number: null, payment_id: null, work_order_id: "w1", work_order_number: "WO-2026-000010", reference: null, created_by_name: "Manager", created_at: now }] };
    case "billing/balances": return list([{ tenant_id: "t1", tenant_name: "PT Maju Jaya", unit_location_id: null, unit_label: null, balance: 5_000_000, last_entry_at: now }]);
    case "billing/ledger-entries": return list([]);
    case "reports": return { data: [{ name: "aging", title: "Aging Piutang", description: "Umur piutang" }] };
    case "reports/aging": return report;
    default: return list([]);
  }
}

beforeEach(() => {
  auth.perms = [];
  auth.orgWide = false;
  vi.stubGlobal("fetch", vi.fn(async (url: string) => ({ ok: true, status: 200, text: async () => JSON.stringify(respond(url)), json: async () => respond(url), headers: new Headers() })));
});
afterEach(() => vi.unstubAllGlobals());

function renderAt(path: string, pattern: string, element: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ToastProvider>
        <MemoryRouter initialEntries={[path]}>
          <Routes><Route path={pattern} element={element} /></Routes>
        </MemoryRouter>
      </ToastProvider>
    </QueryClientProvider>,
  );
}

describe("Receivables (P4-AGE-01, P4-COL-03, P4-OUT-02)", () => {
  it("aging: sel bucket menautkan ke daftar invoice dengan filter aging & tenant", async () => {
    auth.perms = ["billing.invoices.view", "billing.collections.view"];
    renderAt("/billing/aging", "/billing/aging", <ReceivablesPage tab="aging" />);
    const table = await screen.findByTestId("aging-table");
    expect(within(table).getByText("PT Maju Jaya")).toBeInTheDocument();
    expect(within(table).getAllByText("Rp 2.000.000")[0].closest("a")).toHaveAttribute("href", "/billing/invoices?aging=90_plus&property_id=p1&tenant_id=t1");
    expect(screen.getByTestId("aging-90_plus")).toBeInTheDocument();
  });
  it("deep link ?log= membuka detail log & aksi janji bayar", async () => {
    auth.perms = ["billing.collections.view", "billing.collections.manage", "billing.invoices.view"];
    renderAt("/billing/collections?log=l1", "/billing/collections", <ReceivablesPage tab="collections" />);
    const drawer = await screen.findByTestId("collection-log-drawer");
    expect(within(drawer).getByText("Tandai janji ditepati")).toBeInTheDocument();
    expect(within(drawer).getByText("INV-2026-000001")).toBeInTheDocument();
  });
  it("detail log: catat kontak baru → POST log lanjutan untuk pihak yang sama, invoice log asal terpilih", async () => {
    auth.perms = ["billing.collections.view", "billing.collections.manage", "billing.invoices.view"];
    const posted: Record<string, unknown>[] = [];
    const invoices = [
      { id: "i1", invoice_number: "INV-2026-000001", invoice_type: "service_charge", description: null, total_amount: 3_500_000, outstanding_amount: 3_500_000, due_at: now, status: "issued", tenant_name: "PT Maju Jaya", unit_label: null },
      { id: "i2", invoice_number: "INV-2026-000002", invoice_type: "service_charge", description: null, total_amount: 1_000_000, outstanding_amount: 1_000_000, due_at: now, status: "overdue", tenant_name: "PT Maju Jaya", unit_label: null },
    ];
    const created = { ...log, id: "l2", outcome: "contacted", promise_date: null, promise_amount: null, promise_status: null, allowed_actions: ["view", "update"] };
    vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
      const p = new URL(url, "http://localhost").pathname.replace("/api/v1/", "");
      let body: unknown = respond(url);
      if (p === "invoices") body = list(invoices);
      if (p === "billing/collection-logs" && init?.method === "POST") {
        posted.push(JSON.parse(String(init.body)));
        body = created;
      }
      return { ok: true, status: 200, text: async () => JSON.stringify(body), json: async () => body, headers: new Headers() };
    }));
    renderAt("/billing/collections?log=l1", "/billing/collections", <ReceivablesPage tab="collections" />);
    const drawer = await screen.findByTestId("collection-log-drawer");
    within(drawer).getByRole("button", { name: /Catat kontak baru/ }).click();
    const dialog = await screen.findByRole("dialog", { name: "Catat kontak lanjutan" });
    expect(within(dialog).getByText("Pihak: PT Maju Jaya")).toBeInTheDocument();
    await within(dialog).findByText("INV-2026-000002");
    await waitFor(() => expect(within(dialog).getByText("Invoice terkait (1 dipilih)")).toBeInTheDocument());
    within(dialog).getByRole("button", { name: "Simpan log" }).click();
    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).toMatchObject({ property_id: "p1", tenant_id: "t1", unit_location_id: null, invoice_ids: ["i1"], channel: "phone", outcome: "contacted", promise_date: null });
  });
  it("statement: saldo awal, mutasi dengan saldo berjalan, unduh PDF", async () => {
    auth.perms = ["billing.invoices.view", "property.tenants.view"];
    renderAt("/billing/statement?tenant_id=t1", "/billing/statement", <ReceivablesPage tab="statement" />);
    const table = await screen.findByTestId("statement-table");
    expect(within(table).getByText("Saldo awal")).toBeInTheDocument();
    expect(within(table).getByText("INV-2026-000001").closest("a")).toHaveAttribute("href", "/billing/invoices/i1");
    expect(screen.getByText("Unduh PDF")).toBeInTheDocument();
  });
});

describe("Utilities (P4-UTL-02)", () => {
  it("deep link pembacaan: drawer dengan anomali angka mundur & aksi review", async () => {
    auth.perms = ["billing.meters.view", "billing.meter_readings.view", "billing.meter_readings.review"];
    renderAt("/billing/meters/readings/r1", "/billing/meters/readings/:id", <UtilitiesPage tab="readings" />);
    const drawer = await screen.findByTestId("reading-drawer");
    expect(within(drawer).getAllByText("Angka mundur").length).toBeGreaterThan(0);
    expect(within(drawer).getByText("Setujui")).toBeInTheDocument();
    expect(within(drawer).getByText("Tolak…")).toBeInTheDocument();
  });
});

describe("Rekonsiliasi (P4-REC-02)", () => {
  it("detail impor: konfirmasi saran ≥ 90 hanya menghitung saran yakin", async () => {
    auth.perms = ["billing.reconciliation.view", "billing.reconciliation.manage"];
    renderAt("/billing/reconciliation/s1", "/billing/reconciliation/:id", <ReconciliationPage />);
    expect(await screen.findByText("Konfirmasi saran ≥ 90 (1)")).toBeInTheDocument();
    expect(screen.getAllByText("Konfirmasi").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Cocokkan").length).toBeGreaterThan(0);
  });
});

describe("Budget (P4-BGT-01)", () => {
  it("draft: matriks 12 bulan dapat diubah, tombol setujui", async () => {
    auth.perms = ["billing.budgets.view", "billing.budgets.manage", "billing.budgets.approve"];
    renderAt("/finance/budgets/b1", "/finance/budgets/:id", <BudgetsPage />);
    const matrix = await screen.findByTestId("budget-matrix-revenue");
    expect(within(matrix).getByLabelText("IPL Jan")).toHaveValue("100.000");
    expect(screen.getByText("Setujui")).toBeInTheDocument();
  });
});

describe("Dashboard Finance (P4-FIN-01)", () => {
  it("attention dari respons dashboard dengan deep link route web; uang KPI ringkas", async () => {
    auth.perms = ["billing.invoices.view"];
    renderAt("/finance", "/finance", <DomainDashboardPage domain="finance" />);
    const att = await screen.findByTestId("finance-attention");
    expect(within(att).getByText("EL-1203").closest("a")).toHaveAttribute("href", "/billing/meters/readings/r1");
    expect(within(att).getByText("Meter perlu review")).toBeInTheDocument();
    expect(within(screen.getByTestId("kpi-outstanding")).getByText("Rp 2,5 M")).toBeInTheDocument();
    expect(within(screen.getByTestId("breakdown-revenue_type")).getByText("Service Charge")).toBeInTheDocument();
    const calls = (fetch as unknown as { mock: { calls: [string][] } }).mock.calls.map((c) => String(c[0]));
    expect(calls.some((c) => c.includes("overview/attention-required"))).toBe(false);
  });
});

describe("Accounting (P4-INT-04)", () => {
  it("webhook butuh grant tingkat organization", async () => {
    auth.perms = ["billing.accounting.view", "billing.accounting.manage"];
    renderAt("/finance/accounting/webhooks", "/finance/accounting/:tab", <AccountingPage />);
    expect(await screen.findByText("Tingkat organization")).toBeInTheDocument();
  });
  it("grant organization: daftar endpoint + dokumentasi tanda tangan", async () => {
    auth.perms = ["billing.accounting.view", "billing.accounting.manage"];
    auth.orgWide = true;
    renderAt("/finance/accounting/webhooks", "/finance/accounting/:tab", <AccountingPage />);
    expect(await screen.findByText("Accurate Online")).toBeInTheDocument();
    expect(screen.getByText("2 gagal")).toBeInTheDocument();
    expect(screen.getByText("Verifikasi tanda tangan")).toBeInTheDocument();
  });
});

describe("Sinking Fund & Deposit (P4-SCF, P4-PND)", () => {
  it("sinking fund: saldo, mutasi penggunaan tertaut WO, tambah entri dengan izin", async () => {
    auth.perms = ["billing.sinking_fund.view", "billing.sinking_fund.manage", "operations.work_orders.view"];
    renderAt("/billing/sinking-fund", "/billing/sinking-fund", <SinkingFundPage />);
    expect(await screen.findByText("WO-2026-000010")).toHaveAttribute("href", "/work-orders/w1");
    expect(screen.getByText("Rp 150 jt")).toBeInTheDocument();
    expect(screen.getAllByText("Tambah entri").length).toBeGreaterThan(0);
  });
  it("deposit: saldo per tenant", async () => {
    auth.perms = ["billing.deposits.view"];
    renderAt("/billing/deposits", "/billing/deposits", <DepositsPage />);
    expect(await screen.findByText("PT Maju Jaya")).toBeInTheDocument();
    expect(screen.queryByText("Catat mutasi deposit")).not.toBeInTheDocument();
  });
});

describe("Laporan keuangan (P4-FIN-03)", () => {
  it("aging: kartu uang fmtMoney & breakdown menautkan ke daftar kerja penagihan", async () => {
    auth.perms = ["reports.reports.view"];
    renderAt("/reports/aging", "/reports/:name", <ReportsPage />);
    const bd = await screen.findByTestId("breakdown-tenant");
    expect(within(bd).getByText("PT Maju Jaya").closest("a")).toHaveAttribute("href", "/billing/collections?party=t1&property_id=p1");
    await waitFor(() => expect(screen.getAllByText("Rp 3.500.000").length).toBeGreaterThan(0));
  });
});
