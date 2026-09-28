// PRD P2 v2.1 Workforce Operations (dashboard domain, shift per domain, kompetensi, Cleaning Route, dokumen equipment, drill-down):
// helper murni, navigasi (permAll dashboard, urutan menu), label object baru.
import { describe, expect, it } from "vitest";
import "@/lib/i18n";
import { ACCESS, NAV, checkAccess, isItemActive, visibleNav, type AccessContext } from "@/app/navigation";
import { attentionCategoryLabel, itemLink } from "@/components/bv/cards";
import { objectTypeLabel } from "@/components/bv/badges";
import { statusMap } from "@/lib/status-map";
import { normalizeWeekdays, toggleWeekday, weekdaysText } from "@/lib/weekdays";
import { findingQuery, incidentQuery, resolveDay, visitorDate, workItemQuery } from "@/lib/drilldown";
import { addDays, capacityTone, dayLabel, fmtHours, groupRoster, loadPct, shiftText, shiftTone, shiftWindow, validityText, weekDays, weekStart } from "@/features/workforce/workforce";
import { consumableUsageError, cleaningTypeLabel, moveItem, stopTimings } from "@/features/housekeeping/routes";
import { BREAKDOWNS, areaCondition, formatKpiValue, healthSegments, incidentCategoryLabel, lastDays, runStatusFromCode, severityTone } from "@/features/dashboards/dashboard";
import { assetTab, documentTypeLabel, healthStatusFor, internalLink, lastMonths } from "@/features/assets/asset-p2";
import { guessFmt, initialRange, summaryLabel } from "@/features/reports/report-view";
import type { RosterEntry } from "@/api/types";

const ctx = (perms: string[]): AccessContext => ({ can: (p) => perms.includes(p), hasCapability: () => true, isInternalAdmin: false });
const items = (key: string, perms: string[]) => visibleNav(NAV, ctx(perms)).find((g) => g.key === key)?.items?.map((i) => i.to) ?? [];

describe("navigasi P2 v2.1", () => {
  it("dashboard domain butuh overview.dashboard.view DAN view domain (permAll)", () => {
    expect(checkAccess(ACCESS.engineeringDashboard, ctx(["overview.dashboard.view"]))).toBe("permission");
    expect(checkAccess(ACCESS.engineeringDashboard, ctx(["engineering.assets.view"]))).toBe("permission");
    expect(checkAccess(ACCESS.engineeringDashboard, ctx(["overview.dashboard.view", "engineering.assets.view"]))).toBe("allowed");
    expect(checkAccess(ACCESS.securityDashboard, ctx(["overview.dashboard.view", "security.incidents.view"]))).toBe("allowed");
    expect(checkAccess(ACCESS.housekeepingDashboard, ctx(["overview.dashboard.view", "housekeeping.cleaning.view"]))).toBe("allowed");
  });
  it("Dashboard jadi item pertama grup domain; tanpa overview tetap disembunyikan", () => {
    expect(items("engineering", ["overview.dashboard.view", "engineering.assets.view"])[0]).toBe("/engineering");
    expect(items("engineering", ["engineering.assets.view"])).not.toContain("/engineering");
    expect(items("security", ["overview.dashboard.view", "security.incidents.view", "operations.incidents.view"])).toEqual(["/security", "/security/incidents"]);
  });
  it("urutan Security = NC §15 (… Lost & Found · Shift Management) dan Housekeeping = NC §17", () => {
    expect(items("security", ["security.patrol.view", "security.lost_found.view", "security.shifts.view"])).toEqual(["/security/patrol", "/security/lost-found", "/security/shifts"]);
    expect(items("housekeeping", ["overview.dashboard.view", "housekeeping.cleaning.view", "housekeeping.cleaning_schedules.view", "housekeeping.cleaning_routes.view", "housekeeping.inspections.view", "housekeeping.shifts.view"]))
      .toEqual(["/housekeeping", "/housekeeping/cleaning", "/housekeeping/schedule", "/housekeeping/routes", "/housekeeping/inspections", "/housekeeping/shifts"]);
  });
  it("shift per domain (D-P2-05): izin security tidak membuka shift housekeeping", () => {
    expect(items("security", ["security.shifts.view"])).toEqual(["/security/shifts"]);
    expect(items("housekeeping", ["security.shifts.view"])).toEqual([]);
  });
  it("Dokumen Equipment di Engineering, Kompetensi Staf di Settings", () => {
    expect(items("engineering", ["engineering.asset_documents.view"])).toEqual(["/engineering/documents"]);
  });
  it("item Dashboard hanya aktif pada path persis; detail kompetensi mengaktifkan item Kompetensi", () => {
    const eng = NAV.find((g) => g.key === "engineering")!.items!;
    const dash = eng.find((i) => i.to === "/engineering")!;
    expect(isItemActive(dash, "/engineering")).toBe(true);
    expect(isItemActive(dash, "/engineering/preventive-maintenance")).toBe(false);
  });
});

describe("label object & attention P2", () => {
  it("kategori attention workforce/dokumen punya label; object baru punya label & tautan", () => {
    expect(attentionCategoryLabel.workforce_shortage).toBe("Kekurangan Staf On-Duty");
    expect(attentionCategoryLabel.document_expiring).toBe("Dokumen Kedaluwarsa");
    expect(objectTypeLabel.asset_document).toBe("Dokumen Equipment");
    expect(itemLink("cleaning_route_run", "r1")).toBe("/housekeeping/routes?run=r1");
    expect(itemLink("inventory_item", "i1")).toBe("/inventory?item=i1");
  });
  it("grup status map baru tersedia (GENERATED)", () => {
    for (const g of ["roster_attendance", "attendance", "shift_handover", "asset_health", "validity", "cleaning_route_run"] as const) expect(Object.keys(statusMap[g]).length).toBeGreaterThan(0);
  });
});

describe("hari jadwal berulang (Go time.Weekday 0 = Minggu)", () => {
  it("normalisasi, teks, toggle", () => {
    expect(normalizeWeekdays([7, 1, 1, 9, -1])).toEqual([0, 1]);
    expect(weekdaysText([0, 1, 2, 3, 4, 5, 6])).toBe("Setiap hari");
    expect(weekdaysText([0, 2, 1])).toBe("Sen Sel Min");
    expect(weekdaysText([])).toBe("—");
    expect(toggleWeekday([1, 2], 2)).toEqual([1]);
    expect(toggleWeekday([1], 0)).toEqual([0, 1]);
  });
});

describe("shift & roster", () => {
  it("shiftWindow mencerminkan backend: lintas tengah malam, durasi, jam sama/invalid", () => {
    expect(shiftWindow("07:00", "15:00")).toEqual({ durationMinutes: 480, crossesMidnight: false });
    expect(shiftWindow("23:00", "07:00")).toEqual({ durationMinutes: 480, crossesMidnight: true });
    expect(shiftWindow("07:00", "07:00")).toBeNull();
    expect(shiftWindow("25:00", "07:00")).toBeNull();
    expect(fmtHours(450)).toBe("7 jam 30 menit");
    expect(fmtHours(45)).toBe("45 menit");
    expect(shiftText({ name: "Pagi", start_time: "07:00", end_time: "15:00" })).toBe("Pagi · 07:00–15:00");
  });
  it("minggu roster dimulai Senin", () => {
    expect(weekStart("2026-09-27")).toBe("2026-09-21"); // Minggu → Senin sebelumnya
    expect(weekStart("2026-09-28")).toBe("2026-09-28");
    expect(weekDays("2026-09-28")).toHaveLength(7);
    expect(addDays("2026-09-28", 7)).toBe("2026-10-05");
    expect(dayLabel("2026-09-28")).toBe("Sen 28/9");
  });
  it("groupRoster per shift × tanggal", () => {
    const e = (id: string, shift: string, date: string) => ({ id, shift_id: shift, shift_date: date }) as RosterEntry;
    const g = groupRoster([e("1", "s1", "2026-09-28"), e("2", "s1", "2026-09-28"), e("3", "s2", "2026-09-28")]);
    expect(g.get("s1|2026-09-28")?.map((x) => x.id)).toEqual(["1", "2"]);
    expect(g.get("s2|2026-09-28")).toHaveLength(1);
  });
  it("warna shift = tone token; hex (klien lama) tidak dipakai", () => {
    expect(shiftTone("info")).toBe("info");
    expect(shiftTone("custom-red")).toBeUndefined();
    expect(shiftTone(null)).toBeUndefined();
  });
  it("masa berlaku & kapasitas", () => {
    expect(validityText("expiring", 12)).toBe("Berlaku 12 hari lagi");
    expect(validityText("expired", -3)).toBe("Kedaluwarsa 3 hari lalu");
    expect(validityText("no_expiry", null)).toBe("Tanpa masa berlaku");
    expect(capacityTone("over")).toBe("error");
    expect(capacityTone("tight")).toBe("warning");
    expect(capacityTone("ok")).toBe("success");
    expect(capacityTone("idle")).toBe("neutral");
    expect(loadPct(0.834)).toBe(83);
    expect(loadPct(1.7)).toBe(100);
    expect(loadPct(undefined)).toBe(0);
  });
});

describe("Cleaning Route (jadwal berantai stop)", () => {
  it("offset = Σ estimasi stop sebelumnya; estimasi kosong/0 → 15 menit", () => {
    const t = stopTimings("08:00", [15, "30", 0]);
    expect(t.stops.map((s) => s.offset)).toEqual([0, 15, 45]);
    expect(t.stops.map((s) => s.start)).toEqual(["08:00", "08:15", "08:45"]);
    expect(t.totalMinutes).toBe(60);
    expect(t.end).toBe("09:00");
  });
  it("tanpa jam mulai → jam null; lintas tengah malam ditandai", () => {
    expect(stopTimings(null, [10]).stops[0].start).toBeNull();
    const t = stopTimings("23:50", [15]);
    expect(t.stops[0].nextDay).toBe(true);
    expect(t.end).toBe("00:05");
  });
  it("urutan stop, label tipe, error consumable", () => {
    expect(moveItem(["a", "b", "c"], 1, -1)).toEqual(["b", "a", "c"]);
    expect(moveItem(["a", "b"], 1, 1)).toEqual(["a", "b"]);
    expect(cleaningTypeLabel("deep")).toBe("Deep Cleaning");
    expect(consumableUsageError("INSUFFICIENT_STOCK")).toMatch(/Stok tidak mencukupi/);
    expect(consumableUsageError("OTHER")).toBeNull();
  });
});

describe("drill-down dashboard → filter daftar", () => {
  const now = new Date(2026, 8, 27, 10, 0, 0);
  it("date=today → scheduled_on; from/to → rentang completed/due; result diteruskan", () => {
    const a = workItemQuery({ date: "today", status: "completed,closed", team_id: "t1" }, "completed", now);
    expect(a.query).toEqual({ status: "completed,closed", team_id: "t1", scheduled_on: "2026-09-27T00:00:00Z" });
    const b = workItemQuery({ from: "2026-09-01", to: "2026-09-27", result: "fail" }, "completed", now);
    expect(b.query.completed_from).toBe(new Date(2026, 8, 1).toISOString());
    expect(b.query.completed_to).toBe(new Date(2026, 8, 27, 23, 59, 59, 999).toISOString());
    expect(b.query.from).toBeUndefined();
    expect(b.query.result).toBe("fail");
    expect(b.notes).toHaveLength(0);
    expect(workItemQuery({ from: "2026-09-01" }, "due", now).query.due_from).toBeDefined();
  });
  it("incident reported=today → created_from/created_to; finding from/to → created_*, asset_id diteruskan", () => {
    const i = incidentQuery({ reported: "today", open: "true" }, now);
    expect(i.query.created_from).toBe(new Date(2026, 8, 27).toISOString());
    expect(i.query.open).toBe("true");
    const f = findingQuery({ type: "housekeeping", from: "2026-09-01", to: "2026-09-27", asset_id: "a1" }, now);
    expect(f.query).toEqual({ type: "housekeeping", asset_id: "a1", created_from: new Date(2026, 8, 1).toISOString(), created_to: new Date(2026, 8, 27, 23, 59, 59, 999).toISOString() });
    expect(f.notes).toHaveLength(0);
  });
  it("tanggal tamu & resolveDay", () => {
    expect(visitorDate("today", now)).toBe("2026-09-27");
    expect(visitorDate("2026-09-01", now)).toBe("2026-09-01");
    expect(visitorDate("besok", now)).toBe("2026-09-27");
    expect(resolveDay("x", now)).toBeNull();
  });
});

describe("dashboard domain", () => {
  it("format nilai KPI per unit server", () => {
    expect(formatKpiValue("pct", 87.25)).toBe("87,3%");
    expect(formatKpiValue("minutes", 12.5)).toBe("12,5 mnt");
    expect(formatKpiValue("idr", 1250000)).toBe("Rp 1.250.000");
    expect(formatKpiValue("count", 1234)).toBe("1.234");
    expect(formatKpiValue("score", 88.4)).toBe("88,4");
  });
  it("severity → tone rail; normal tanpa rail", () => {
    expect(severityTone("critical")).toBe("error");
    expect(severityTone("warning")).toBe("warning");
    expect(severityTone("normal")).toBeUndefined();
  });
  it("distribusi health & kondisi area & status run", () => {
    const segs = healthSegments({ healthy: 6, warning: 3, critical: 1 });
    expect(segs.map((s) => s.status)).toEqual(["healthy", "warning", "critical", "offline", "unknown"]);
    expect(segs[0].pct).toBe(60);
    expect(healthSegments(null).every((s) => s.pct === 0)).toBe(true);
    expect(areaCondition(2).label).toBe("Baik");
    expect(areaCondition(0).tone).toBe("error");
    expect(runStatusFromCode(1)).toBe("in_progress");
    expect(incidentCategoryLabel("unauthorized_access")).toBe("Akses tidak sah");
  });
  it("breakdown mengikuti kunci server (overview/domains.go)", () => {
    expect(BREAKDOWNS.engineering.map((b) => b.key)).toEqual(["building", "equipment_category", "team"]);
    expect(BREAKDOWNS.security.map((b) => b.key)).toEqual(["route", "incident_category", "building"]);
    expect(BREAKDOWNS.housekeeping.map((b) => b.key)).toEqual(["routes_today", "team", "area", "consumables"]);
  });
  it("rentang default 30 hari termasuk hari ini", () => {
    expect(lastDays(30, new Date(2026, 8, 27))).toEqual({ from: "2026-08-29", to: "2026-09-27" });
  });
});

describe("Asset 360 & laporan", () => {
  it("tab dari URL, tautan internal, ambang health NC §14", () => {
    expect(assetTab("documents")).toBe("documents");
    expect(assetTab("x")).toBe("history");
    expect(internalLink("/assets/1")).toBe("/assets/1");
    expect(internalLink("//evil.test")).toBeNull();
    expect(internalLink("https://x")).toBeNull();
    expect(healthStatusFor(90)).toBe("healthy");
    expect(healthStatusFor(70)).toBe("warning");
    expect(healthStatusFor(69)).toBe("critical");
    expect(documentTypeLabel("inspection_report")).toBe("Laporan inspeksi");
    expect(lastMonths(12, new Date(2026, 8, 27))).toEqual({ from: "2025-09-27", to: "2026-09-27" });
  });
  it("laporan: rentang dari URL & label ramah kunci baru", () => {
    expect(initialRange("2026-09-01", "2026-09-27")).toEqual({ from: "2026-09-01", to: "2026-09-27" });
    expect(initialRange("2026-09-27", "2026-09-01", new Date(2026, 8, 27))).toEqual({ from: "2026-08-29", to: "2026-09-27" });
    expect(summaryLabel("checkpoint_compliance_pct")).toBe("Checkpoint compliance");
    expect(summaryLabel("avg_inspection_score")).toBe("Skor inspeksi");
    expect(summaryLabel("foo_bar_pct")).toBe("Foo bar (%)");
    expect(guessFmt("avg_emergency_ack_minutes")).toBe("minutes");
    expect(guessFmt("usage_cost")).toBe("money");
  });
});
