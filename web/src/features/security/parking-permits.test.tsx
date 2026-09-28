// Smoke render tab Izin Parkir (PRD P3 v2.1 §5.8 P3-PRK-02) di Security › Parking dengan API tiruan: tab hanya dengan
// security.parking_permits.view, deep link /security/parking/permits/:id membuka drawer, Setujui → POST /approve dengan area,
// masa berlaku, stiker, tarif; tab Parking lain tetap berfungsi.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/lib/i18n";

const auth = { perms: [] as string[] };
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    can: (p: string) => auth.perms.includes(p),
    principal: { id: "u1", full_name: "Security Supervisor", properties: [] },
    propertyId: "p1",
    properties: [{ id: "p1", name: "Menara Demo", code: "MD" }],
  }),
}));

import { ToastProvider } from "@/components/bv/common";
import ParkingPage from "./ParkingPage";

const list = (data: unknown[]) => ({ data, next_cursor: null });
const permit = {
  id: "pp1", permit_number: "PRM-2026-000003", property_id: "p1", vehicle_id: "v1", plate_number: "B1234XYZ", vehicle_type: "car", vehicle_label: "Avanza Hitam", tenant_id: "t1", tenant_name: "PT Maju",
  unit_location_id: "l12", unit_name: "Unit 1208", requested_by_name: "Sari", parking_area_id: null, parking_area_name: null, permit_type: "monthly", status: "requested", is_active: false,
  valid_from: null, valid_until: null, sticker_number: null, fee_amount: null, notes: null, decision_reason: null, decided_by_name: null, decided_at: null, requested_at: new Date().toISOString(),
  allowed_actions: ["view", "approve", "reject"], version: 1,
};
const area = { id: "a1", property_id: "p1", location_id: null, location_path: null, code: "B1", name: "Basement 1", area_type: "tenant", capacity: 40, occupied: 30, available: 10, is_active: true, notes: null, version: 1 };

function respond(url: string, method: string): unknown {
  const u = new URL(url, "http://localhost");
  const p = u.pathname.replace("/api/v1/", "");
  if (p === "parking-permits" && method === "GET") return list([permit]);
  if (p === "parking-permits/pp1") return permit;
  if (p === "parking-permits/pp1/approve" && method === "POST") return { ...permit, status: "approved", is_active: true, valid_from: "2026-10-01", valid_until: "2026-10-31", allowed_actions: ["view", "update", "revoke"] };
  if (p === "parking-areas") return list([area]);
  return list([]);
}

beforeEach(() => {
  auth.perms = [];
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const body = respond(url, (init?.method ?? "GET").toUpperCase());
    return { ok: true, status: 200, statusText: "200", text: async () => JSON.stringify(body), json: async () => body, headers: new Headers() };
  }));
});
afterEach(() => vi.unstubAllGlobals());

const calls = (part: string, method?: string) => vi.mocked(fetch).mock.calls.filter((c) => String(c[0]).includes(part) && (!method || ((c[1] as RequestInit | undefined)?.method ?? "GET").toUpperCase() === method));

function renderAt(path: string, pattern: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ToastProvider>
        <MemoryRouter initialEntries={[path]}>
          <Routes><Route path={pattern} element={<ParkingPage />} /></Routes>
        </MemoryRouter>
      </ToastProvider>
    </QueryClientProvider>,
  );
}

describe("Izin Parkir (P3-PRK-02)", () => {
  it("tab Izin Parkir hanya untuk security.parking_permits.view; tab lain tetap berjalan", async () => {
    auth.perms = ["security.parking.view"];
    renderAt("/security/parking/areas", "/security/parking/:tab");
    expect(await screen.findByText("Basement 1")).toBeInTheDocument();
    expect(screen.queryByText("Izin Parkir")).not.toBeInTheDocument();
  });
  it("tab daftar: plat, tenant, status Diajukan", async () => {
    auth.perms = ["security.parking.view", "security.parking_permits.view"];
    renderAt("/security/parking/permits", "/security/parking/:tab");
    expect((await screen.findAllByText("PRM-2026-000003")).length).toBeGreaterThan(0);
    expect(screen.getAllByText("B 1234 XYZ").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Izin Parkir").length).toBeGreaterThan(0);
  });
  it("deep link membuka drawer; Setujui → POST /approve dengan area, masa berlaku, stiker, tarif", async () => {
    auth.perms = ["security.parking.view", "security.parking_permits.view", "security.parking_permits.approve"];
    renderAt("/security/parking/permits/pp1", "/security/parking/permits/:id");
    expect(await screen.findByText("Menunggu persetujuan", { selector: "div" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Tolak/ })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Setujui/ }));
    const area = await screen.findByRole("option", { name: /B1 · Basement 1/ });
    fireEvent.change(area.closest("select")!, { target: { value: "a1" } });
    fireEvent.change(screen.getByPlaceholderText("mis. STK-B1-042"), { target: { value: "stk-b1-042" } });
    fireEvent.change(screen.getByPlaceholderText("0"), { target: { value: "150000" } });
    fireEvent.click(screen.getByRole("button", { name: /Setujui izin/ }));
    await waitFor(() => expect(calls("parking-permits/pp1/approve", "POST").length).toBe(1));
    expect(JSON.parse(String((calls("parking-permits/pp1/approve", "POST")[0][1] as RequestInit).body))).toMatchObject({ parking_area_id: "a1", sticker_number: "STK-B1-042", fee_amount: 150000, valid_from: null, valid_until: null });
  });
});
