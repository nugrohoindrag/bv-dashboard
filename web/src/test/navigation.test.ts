// Navigation Foundation (PRD P0 §21): urutan grup, filter permission/capability, tidak ada duplikasi menu.
import { describe, expect, it } from "vitest";
import { ACCESS, NAV, checkAccess, firstDestination, isItemActive, visibleNav, type AccessContext } from "@/app/navigation";

const ctx = (perms: string[], caps: string[] = [], internal = false): AccessContext => ({
  can: (p) => perms.includes("*") || perms.includes(p),
  hasCapability: (c) => caps.includes(c),
  isInternalAdmin: internal,
});
const ALL_CAPS = ["tenant_relation", "facility_booking", "visitor_management", "billing", "vendor_management", "inventory", "reports", "reception", "hotel_booking", "unit_sales", "unit_rental"];

describe("NAV (PRD P0 §21)", () => {
  it("urutan grup inti mengikuti PRD; grup profile (Reception, Commercial) aditif setelah Tenant", () => {
    const keys = NAV.map((g) => g.key);
    expect(keys).toEqual(["overview", "operations", "engineering", "security", "housekeeping", "property", "tenant", "reception", "commercial", "finance", "vendor", "inventory", "reports", "settings"]);
  });
  it("Service Requests hanya di Operations dan Tenant", () => {
    const owners = NAV.filter((g) => g.items?.some((i) => i.label === "nav.service_requests")).map((g) => g.key);
    expect(owners).toEqual(["operations", "tenant"]);
  });
  it("Assets/Equipment/Asset History ada di Engineering tanpa duplikat grup Asset Management", () => {
    const eng = NAV.find((g) => g.key === "engineering")!;
    expect(eng.items!.map((i) => i.label)).toEqual(expect.arrayContaining(["nav.assets", "nav.equipment", "nav.asset_history"]));
    expect(NAV.some((g) => g.key === "assets")).toBe(false);
    const labels = NAV.flatMap((g) => (g.items ?? []).map((i) => `${g.key}:${i.to}`));
    expect(new Set(labels).size).toBe(labels.length);
  });
});

describe("visibleNav (permission- & capability-aware)", () => {
  it("user tanpa permission hanya melihat Settings › Profil Saya & Design System", () => {
    const v = visibleNav(NAV, ctx([]));
    expect(v.map((g) => g.key)).toEqual(["settings"]);
    expect(v[0].items!.map((i) => i.to)).toEqual(["/settings/profile", "/settings/design-system"]);
  });
  it("registry organization hanya untuk Platform Admin (is_platform_admin)", () => {
    const items = (c: AccessContext) => visibleNav(NAV, c).find((g) => g.key === "settings")!.items!.map((i) => i.to);
    expect(items(ctx(["*"], [], true))).not.toContain("/settings/platform-organizations");
    expect(items({ ...ctx([]), isPlatformAdmin: true })).toContain("/settings/platform-organizations");
  });
  it("Portfolios di grup Property dengan permission property.portfolios.view", () => {
    const v = visibleNav(NAV, ctx(["property.portfolios.view"]));
    expect(v.find((g) => g.key === "property")!.items!.map((i) => i.to)).toEqual(["/property/portfolios"]);
  });
  it("teknisi: Overview + Operations (tasks/work orders) saja, grup kosong disembunyikan", () => {
    const v = visibleNav(NAV, ctx(["overview.dashboard.view", "operations.tasks.view", "operations.work_orders.view"], ALL_CAPS));
    expect(v.map((g) => g.key)).toEqual(["overview", "operations", "engineering", "settings"]);
    expect(v.find((g) => g.key === "operations")!.items!.map((i) => i.to)).toEqual(["/operations/tasks", "/operations/work-orders"]);
    expect(v.find((g) => g.key === "engineering")!.items!.map((i) => i.to)).toEqual(["/engineering/corrective-maintenance"]);
  });
  it("capability tidak aktif menyembunyikan modul walau permission ada", () => {
    const perms = ["billing.invoices.view", "billing.payments.view", "booking.bookings.view", "vendor.vendors.view"];
    expect(visibleNav(NAV, ctx(perms, [])).map((g) => g.key)).toEqual(["settings"]);
    const withCaps = visibleNav(NAV, ctx(perms, ["billing", "facility_booking", "vendor_management"])).map((g) => g.key);
    expect(withCaps).toEqual(["tenant", "finance", "vendor", "settings"]);
  });
  it("menu internalOnly hanya untuk admin_internal", () => {
    const perms = ["platform.app_downloads.view", "platform.demo_data.view"];
    const regular = visibleNav(NAV, ctx(perms)).find((g) => g.key === "settings")!.items!.map((i) => i.to);
    expect(regular).not.toContain("/settings/app-downloads");
    const internal = visibleNav(NAV, ctx(perms, [], true)).find((g) => g.key === "settings")!.items!.map((i) => i.to);
    expect(internal).toEqual(expect.arrayContaining(["/settings/app-downloads", "/settings/demo-data"]));
  });
  it("firstDestination: Overview bila diizinkan, selain itu menu pertama yang terlihat", () => {
    expect(firstDestination(visibleNav(NAV, ctx(["*"], ALL_CAPS)))).toBe("/overview");
    expect(firstDestination(visibleNav(NAV, ctx(["operations.incidents.view"])))).toBe("/operations/incidents");
  });
});

describe("checkAccess & isItemActive", () => {
  it("membedakan penyebab penolakan", () => {
    expect(checkAccess(ACCESS.workOrders, ctx([]))).toBe("permission");
    expect(checkAccess(ACCESS.invoices, ctx(["billing.invoices.view"]))).toBe("capability");
    expect(checkAccess({ internalOnly: true }, ctx(["*"]))).toBe("internal");
    expect(checkAccess(ACCESS.designSystem, ctx([]))).toBe("allowed");
  });
  it("alias URL lama tetap mengaktifkan item yang sama", () => {
    const eng = NAV.find((g) => g.key === "engineering")!.items!;
    const assets = eng.find((i) => i.label === "nav.assets")!;
    expect(isItemActive(assets, "/assets/123")).toBe(true);
    expect(isItemActive(assets, "/assets/history")).toBe(false);
    const wo = NAV.find((g) => g.key === "operations")!.items!.find((i) => i.label === "nav.work_orders")!;
    expect(isItemActive(wo, "/work-orders/abc")).toBe(true);
    const tr = NAV.find((g) => g.key === "tenant")!.items!.find((i) => i.to === "/tenant-relation")!;
    expect(isItemActive(tr, "/tenant-relation/feedback")).toBe(false);
  });
});

describe("PRD P1 v2 — Building Management & Findings", () => {
  it("Property: Facilities (property.* atau booking.*), Denah, Occupancy sesuai permission", () => {
    const items = (perms: string[]) => visibleNav(NAV, ctx(perms)).find((g) => g.key === "property")?.items?.map((i) => i.to) ?? [];
    expect(items(["property.facilities.view", "property.floor_plans.view", "property.occupancy.view"])).toEqual(["/property/facilities", "/property/floor-plans", "/property/occupancy"]);
    expect(items(["booking.facilities.view"])).toEqual(["/property/facilities"]);
    expect(items(["property.occupancy.view"])).toEqual(["/property/occupancy"]);
  });
  it("Operations › Findings dengan operations.findings.view", () => {
    const ops = visibleNav(NAV, ctx(["operations.findings.view"])).find((g) => g.key === "operations")!;
    expect(ops.items!.map((i) => i.to)).toEqual(["/findings"]);
    expect(isItemActive(ops.items![0], "/findings/abc")).toBe(true);
  });
});
