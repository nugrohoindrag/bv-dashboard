// PRD P4 v2.1 Financial Operations — helper murni layar web: format uang ringkas, periode, tautan drill-down aging/statement/
// penagihan, filter daftar kerja, rekonsiliasi (pemetaan kolom, skor, progres), tarif utilitas (blok progresif = server),
// ledger bertanda, matriks budget, pemetaan akun, URL webhook, tampilan KPI dashboard finance.
import { describe, expect, it } from "vitest";
import "@/lib/i18n";
import { appLink, fmtAxisMoney, fmtMoneyShort, fmtPct, fmtSigned, hasOrgWide, monthBounds, parseAmount, periodLabel, shiftPeriod, yearOptions } from "./fin-utils";
import { achievement, favorable, linesFromMatrix, mappingSource, matrixEqual, matrixFromLines, monthTotals, spreadAnnual, txnQuery, webhookUrlError, webhookUrlWarning, zeros, type BudgetLine, type Mapping } from "./finance-model";
import { agingInvoicesLink, bucketShare, bucketTone, collectionsLink, filterWorklist, statementLink, statementObjectLink, worklistSummary, type WorkItem } from "@/features/billing/receivables-model";
import { acceptedFile, cleanColumns, confirmableCount, differenceText, matchProgress, parseDetected, scoreTone, type StmtLine } from "@/features/billing/reconciliation-model";
import { estimateUsage, parseDecimal, tariffSummary, fmtRate, usageCharge, validateBlocks } from "@/features/billing/utilities-model";
import { apiAmount, balanceAfter, balanceTotals, signedAmount } from "@/features/billing/ledger-model";
import { attentionLink, breakdownLabel, formatKpiDisplay } from "@/features/dashboards/dashboard";

describe("format uang & periode (P4-FIN-04)", () => {
  it("uang ringkas, sumbu grafik, bertanda, persen", () => {
    expect(fmtMoneyShort(1_250_000_000)).toBe("Rp 1,25 M");
    expect(fmtMoneyShort(350_000_000)).toBe("Rp 350 jt");
    expect(fmtMoneyShort(950_000)).toBe("Rp 950.000");
    expect(fmtMoneyShort(-12_500_000)).toBe("−Rp 12,5 jt");
    expect(fmtAxisMoney(2_500_000)).toBe("2,5 jt");
    expect(fmtAxisMoney(900_000)).toBe("900 rb");
    expect(fmtSigned(1500)).toBe("+Rp 1.500");
    expect(fmtSigned(-1500)).toBe("−Rp 1.500");
    expect(fmtSigned(0)).toBe("Rp 0");
    expect(fmtPct(5.25, { signed: true })).toBe("+5,3%");
    expect(parseAmount("Rp 1.500.000")).toBe(1_500_000);
    expect(parseAmount("")).toBeNull();
  });
  it("periode & tahun", () => {
    expect(periodLabel("2026-09")).toBe("September 2026");
    expect(shiftPeriod("2026-01", -1)).toBe("2025-12");
    expect(shiftPeriod("2026-12", 1)).toBe("2027-01");
    expect(monthBounds(2026, 2)).toEqual({ from: "2026-02-01", to: "2026-02-28" });
    expect(yearOptions(2026, 1, 1)).toEqual([2027, 2026, 2025]);
  });
  it("tautan server dinormalkan ke route web; eksternal ditolak", () => {
    expect(appLink("/billing/meter-readings/r1")).toBe("/billing/meters/readings/r1");
    expect(appLink("/work-orders?status=completed,closed&property_id=p1")).toBe("/operations/work-orders?status=completed,closed&property_id=p1");
    expect(appLink("/work-orders/w1")).toBe("/work-orders/w1");
    expect(appLink("https://evil.test")).toBeNull();
    expect(appLink("//evil.test")).toBeNull();
    expect(attentionLink("/billing/meter-readings/x")).toBe("/billing/meters/readings/x");
  });
  it("grant tingkat organization (webhook & pemetaan default)", () => {
    const principal = { id: "u", full_name: "F", organization_id: "o", roles: [], team_ids: [], lead_team_ids: [], permissions: [], properties: [{ property_id: null, permissions: ["billing.*"] }, { property_id: "p1", permissions: ["billing.accounting.view"] }] };
    expect(hasOrgWide(principal, "billing.accounting.manage")).toBe(true);
    expect(hasOrgWide({ ...principal, properties: [{ property_id: "p1", permissions: ["billing.*"] }] }, "billing.accounting.manage")).toBe(false);
    expect(hasOrgWide({ ...principal, properties: [{ property_id: null, scope_location_id: "b1", permissions: ["billing.*"] }] }, "billing.accounting.view")).toBe(false);
    expect(hasOrgWide(null, "billing.accounting.view")).toBe(false);
  });
});

describe("receivables (P4-AGE-01, P4-OUT-02, P4-COL-04)", () => {
  it("drill-down aging → invoice dengan bucket & pihak", () => {
    expect(agingInvoicesLink({ kind: "tenant", tenant_id: "t1", unit_location_id: null, property_id: "p1" }, "31_60")).toBe("/billing/invoices?aging=31_60&tenant_id=t1");
    expect(agingInvoicesLink({ kind: "unit", tenant_id: null, unit_location_id: "u1", property_id: "p1" }, "open", "p1")).toBe("/billing/invoices?open=true&property_id=p1&unit_location_id=u1");
    expect(agingInvoicesLink({ kind: "property", tenant_id: null, unit_location_id: null, property_id: "p2" }, "90_plus")).toBe("/billing/invoices?aging=90_plus&property_id=p2");
    expect(agingInvoicesLink(null, "current", null)).toBe("/billing/invoices?aging=current");
    expect(statementLink("p1", { tenant_id: "t1", unit_location_id: "u1" })).toBe("/billing/statement?property_id=p1&tenant_id=t1");
    expect(statementLink("p1", { tenant_id: null, unit_location_id: "u1" })).toBe("/billing/statement?property_id=p1&unit_location_id=u1");
    expect(collectionsLink("p1", { tenant_id: null, unit_location_id: "u1" })).toBe("/billing/collections?property_id=p1&party=u1");
    expect(bucketShare(25, 200)).toBe(12.5);
    expect(bucketTone("90_plus", 10)).toBe("error");
    expect(bucketTone("90_plus", 0)).toBeUndefined();
    expect(statementObjectLink({ object_type: "payment", object_id: "pay1" })).toBe("/billing/payments/pay1");
    expect(statementObjectLink({ object_type: "x", object_id: "1" })).toBeNull();
  });
  it("daftar kerja: filter prioritas/teks/pihak & ringkasan", () => {
    const w = (over: Partial<WorkItem>): WorkItem => ({ kind: "tenant", tenant_id: "t1", unit_location_id: null, label: "PT Maju", property_id: "p1", property_name: "Menara", outstanding_amount: 100, overdue_amount: 80, overdue_count: 1, oldest_days_overdue: 40, last_contact_at: null, last_outcome: null, promise_date: null, promise_amount: null, promise_status: null, follow_up_on: null, priority: "high", reasons: [], contact_phone: null, ...over });
    const items = [w({}), w({ tenant_id: null, unit_location_id: "u9", label: "Unit 1203", priority: "low", overdue_amount: 20, promise_date: "2026-10-01" })];
    expect(filterWorklist(items, { priority: "low" }).map((x) => x.label)).toEqual(["Unit 1203"]);
    expect(filterWorklist(items, { party: "u9" })).toHaveLength(1);
    expect(filterWorklist(items, { q: "maju" })).toHaveLength(1);
    expect(worklistSummary(items)).toEqual({ high: 1, medium: 0, low: 1, overdue: 100, promisesDue: 1 });
  });
});

describe("rekonsiliasi (P4-REC-01..03)", () => {
  const line = (over: Partial<StmtLine>): StmtLine => ({ id: "l", line_no: 1, txn_date: "2026-09-01", description: null, reference: null, amount: 100, balance: null, status: "unmatched", match_type: null, suggested_payment_id: null, suggested_payment_number: null, suggested_invoice_id: null, suggested_invoice_number: null, suggested_party: null, match_score: null, payment_id: null, payment_number: null, invoice_id: null, invoice_number: null, difference: null, matched_by_name: null, matched_at: null, note: null, allowed_actions: [], ...over });
  it("pemetaan kolom, deteksi, berkas", () => {
    expect(cleanColumns({ date: " Tgl ", credit: "", amount: "#4" })).toEqual({ date: "Tgl", amount: "#4" });
    expect(cleanColumns({ date: "" })).toBeUndefined();
    expect(parseDetected(["date: Tanggal Transaksi", "credit: Kredit"])[0]).toEqual({ key: "date", label: "Tanggal", header: "Tanggal Transaksi" });
    expect(acceptedFile("mutasi-BCA.XLSX")).toBe(true);
    expect(acceptedFile("mutasi.xls")).toBe(false);
  });
  it("skor, saran massal, progres, selisih", () => {
    expect(scoreTone(95)).toBe("success");
    expect(scoreTone(70)).toBe("info");
    expect(scoreTone(50)).toBe("warning");
    const lines = [line({ status: "suggested", match_score: 95, allowed_actions: ["confirm"] }), line({ status: "suggested", match_score: 80, allowed_actions: ["confirm"] }), line({ status: "ignored", amount: -50 }), line({ status: "matched" })];
    expect(confirmableCount(lines)).toBe(1);
    expect(matchProgress({ credit_count: 3, matched_count: 1, ignored_count: 1, lines })).toBe(33);
    expect(differenceText(5000)).toContain("saldo kredit");
    expect(differenceText(-5000)).toContain("sebagian");
    expect(differenceText(0)).toBeNull();
  });
});

describe("utilitas (P4-UTL-02, P4-UTL-04) — sama dengan Tariff.UsageCharge server", () => {
  it("tarif flat + beban + minimum", () => {
    expect(usageCharge({ rate: 1444.7, blocks: [], fixed_charge: 50000, minimum_charge: 0 }, 100)).toMatchObject({ energy: 144470, fixed: 50000, total: 194470 });
    expect(usageCharge({ rate: 1000, blocks: [], fixed_charge: 0, minimum_charge: 75000 }, 10).total).toBe(75000);
  });
  it("blok progresif; blok terakhir tanpa batas = sisa", () => {
    const t = { rate: 0, blocks: [{ up_to: 10, rate: 1000 }, { up_to: 20, rate: 2000 }, { up_to: null, rate: 3000 }], fixed_charge: 0, minimum_charge: 0 };
    expect(usageCharge(t, 25).energy).toBe(10 * 1000 + 10 * 2000 + 5 * 3000);
    expect(usageCharge(t, 5).energy).toBe(5000);
    expect(validateBlocks(t.blocks)).toBeNull();
    expect(validateBlocks([{ up_to: null, rate: 1 }, { up_to: 10, rate: 1 }])).toContain("terakhir");
    expect(validateBlocks([{ up_to: 10, rate: 1 }, { up_to: 5, rate: 1 }])).toContain("lebih besar");
    expect(tariffSummary({ ...t, unit_label: "kWh" }, fmtRate)).toBe("Progresif 3 blok (Rp 1.000 – Rp 3.000/kWh)");
  });
  it("pratinjau pemakaian & angka mundur, parsing desimal", () => {
    expect(estimateUsage(100, 150.5, 2)).toEqual({ usage: 101, rollback: false });
    expect(estimateUsage(100, 90, 1)).toEqual({ usage: 0, rollback: true });
    expect(parseDecimal("12.345,6")).toBe(12345.6);
    expect(parseDecimal("1444.70")).toBe(1444.7);
    expect(parseDecimal("abc")).toBeNull();
  });
});

describe("ledger dana (P4-SCF, P4-PND)", () => {
  it("tanda nominal & saldo setelah entri", () => {
    expect(signedAmount("usage", 100)).toBe(-100);
    expect(signedAmount("refunded", 100)).toBe(-100);
    expect(signedAmount("adjustment", 100, true)).toBe(-100);
    expect(apiAmount("usage", 100)).toBe(100);
    expect(apiAmount("adjustment", 100, true)).toBe(-100);
    expect(balanceAfter(500, "deducted", 600)).toBe(-100);
    expect(balanceTotals([{ tenant_id: "t", tenant_name: "A", unit_location_id: null, unit_label: null, balance: 100, last_entry_at: null }, { tenant_id: null, tenant_name: null, unit_location_id: "u", unit_label: "Unit 1", balance: -20, last_entry_at: null }])).toEqual({ total: 80, parties: 2, negative: 1 });
  });
});

describe("budget, budget vs actual, akuntansi (P4-BGT, P4-INT)", () => {
  it("matriks budget ↔ baris; total bulanan; bagi rata tahunan", () => {
    const lines: BudgetLine[] = [{ kind: "revenue", category: "ipl", label: "IPL", months: [100, 100, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0], total: 200 }];
    const m = matrixFromLines(lines);
    expect(monthTotals(m, "revenue")[0]).toBe(100);
    m["cost|staff"] = zeros();
    expect(linesFromMatrix(m)).toEqual([{ kind: "revenue", category: "ipl", months: [100, 100, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0] }]);
    expect(matrixEqual(m, matrixFromLines(lines))).toBe(true);
    expect(spreadAnnual(1000)).toEqual([83, 83, 83, 83, 83, 83, 83, 83, 83, 83, 83, 87]);
    expect(spreadAnnual(1000).reduce((a, b) => a + b, 0)).toBe(1000);
  });
  it("variance favorabel per jenis & tautan transaksi", () => {
    expect(favorable("cost", -10)).toBe(true);
    expect(favorable("revenue", -10)).toBe(false);
    expect(achievement({ budget: 200, actual: 150, variance: -50 })).toBe(75);
    expect(achievement({ budget: 0, actual: 150, variance: 150 })).toBeNull();
    expect(txnQuery({ kind: "cost", category: "maintenance", year: 2026, month: 3, propertyId: "p1" })).toBe("/finance/budget-actual/transactions?kind=cost&category=maintenance&year=2026&month=3&property_id=p1");
  });
  it("sumber pemetaan akun & URL webhook", () => {
    const m: Mapping = { key: "receivable", label: "Piutang", property_id: null, account_code: "1-1300", account_name: "Piutang", is_default: true, inherited: false };
    expect(mappingSource(m, false).label).toBe("Bawaan sistem");
    expect(mappingSource({ ...m, is_default: false, inherited: true }, true).label).toBe("Dari organization");
    expect(mappingSource({ ...m, is_default: false }, true).label).toBe("Override property");
    expect(webhookUrlError("")).toBe("URL wajib");
    expect(webhookUrlError("ftp://x")).toBe("URL harus http(s)");
    expect(webhookUrlError("https://erp.contoh.co.id/hook")).toBeNull();
    expect(webhookUrlWarning("http://erp.contoh.co.id")).toContain("https");
    expect(webhookUrlWarning("https://192.168.1.10/hook")).toContain("internal");
  });
});

describe("dashboard finance (P4-FIN-01)", () => {
  it("KPI uang ringkas ≥ 100 juta; variance bertanda; label jenis tagihan", () => {
    expect(formatKpiDisplay("idr", 1_250_000)).toEqual({ text: "Rp 1.250.000", title: "Rp 1.250.000" });
    expect(formatKpiDisplay("idr", 2_500_000_000)).toEqual({ text: "Rp 2,5 M", title: "Rp 2.500.000.000" });
    expect(formatKpiDisplay("pct", 7.5, "budget_cost_variance").text).toBe("+7,5%");
    expect(formatKpiDisplay("pct", 90, "collection_rate").text).toBe("90%");
    expect(breakdownLabel("revenue_type", { key: "service_charge", label: "service_charge" })).toBe("Service Charge");
    expect(breakdownLabel("aging", { key: "1_30", label: "1–30 hari" })).toBe("1–30 hari");
  });
});
