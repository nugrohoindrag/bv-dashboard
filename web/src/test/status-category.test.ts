// Status System (PRD P0 §20.2): grup common, flag reopened/critical, on_hold = "Pending", pemetaan kategori lengkap.
import { describe, expect, it } from "vitest";
import { statusMap, type ObjectType } from "@/lib/status-map";
import { STATUS_CATEGORIES, STATUS_CATEGORY_MAP, statusCategory, statusesInCategory } from "@/lib/status-category";
import { deriveFlags, statusOptions } from "@/lib/status";

describe("contracts/status-map.yaml", () => {
  it("grup common berisi tepat 8 status PRD", () => {
    expect(Object.keys(statusMap.common)).toEqual(["new", "in_progress", "pending", "completed", "closed", "cancelled", "overdue", "critical"]);
    for (const d of Object.values(statusMap.common)) expect(d.label_id && d.label_en && d.icon).toBeTruthy();
  });
  it("flag reopened & critical tersedia", () => {
    expect(statusMap.flags.reopened.label_en).toBe("Reopened");
    expect(statusMap.flags.critical.semantic).toBe("critical");
  });
  it("on_hold task & work_order berlabel Pending", () => {
    expect(statusMap.task.on_hold.label_id).toBe("Pending");
    expect(statusMap.work_order.on_hold.label_en).toBe("Pending");
  });
  it("grup pengganti peta status lokal tersedia", () => {
    for (const g of ["invoice", "payment", "booking", "visitor", "hotel_reservation", "hotel_room", "unit_listing", "rental_listing", "sales_lead", "sale_reservation", "rental_reservation", "tenant_user"] as ObjectType[]) {
      expect(Object.keys(statusMap[g]).length).toBeGreaterThan(0);
      expect(statusOptions(g).every((o) => o.label && o.label !== o.value)).toBe(true);
    }
  });
});

describe("statusCategory", () => {
  it("setiap status di grup yang dipetakan punya kategori umum", () => {
    for (const ot of Object.keys(STATUS_CATEGORY_MAP) as ObjectType[]) {
      for (const s of Object.keys(statusMap[ot])) {
        if (ot === "common" && (s === "overdue" || s === "critical")) continue; // flag, bukan lifecycle
        expect(statusCategory(ot, s), `${ot}.${s}`).toBeDefined();
      }
    }
  });
  it("modul inti menjangkau keenam kategori lifecycle", () => {
    const reached = new Set<string>();
    for (const ot of ["task", "work_order", "service_request", "incident", "finding", "inspection", "booking", "invoice"] as ObjectType[]) {
      for (const s of Object.keys(statusMap[ot])) reached.add(statusCategory(ot, s)!);
    }
    for (const c of STATUS_CATEGORIES) expect(reached.has(c), c).toBe(true);
  });
  it("pemetaan kunci", () => {
    expect(statusCategory("task", "assigned")).toBe("new");
    expect(statusCategory("work_order", "on_hold")).toBe("pending");
    expect(statusCategory("service_request", "waiting_for_tenant")).toBe("pending");
    expect(statusCategory("incident", "resolved")).toBe("completed");
    expect(statusCategory("invoice", "paid")).toBe("completed");
    expect(statusCategory("booking", "rejected")).toBe("cancelled");
    expect(statusCategory("task", "unknown")).toBeUndefined();
    expect(statusesInCategory("work_order", "new")).toEqual(["draft", "new", "scheduled", "assigned"]);
  });
});

describe("deriveFlags", () => {
  it("reopened dari flags.reopened atau reopen_count; critical opt-in; sla_breach menggantikan sla_risk", () => {
    expect(deriveFlags({ flags: ["sla_risk"], reopen_count: 2 })).toEqual(["sla_risk", "reopened"]);
    expect(deriveFlags({ flags: { reopened: true, overdue: false } })).toEqual(["reopened"]);
    expect(deriveFlags({ flags: [], priority: "critical" })).toEqual([]);
    expect(deriveFlags({ flags: ["sla_breach", "sla_risk"], priority: "critical" }, { critical: true })).toEqual(["sla_breach", "critical"]);
    expect(deriveFlags({ flags: [], is_overdue: true, severity: "critical" }, { critical: true })).toEqual(["overdue", "critical"]);
  });
});
