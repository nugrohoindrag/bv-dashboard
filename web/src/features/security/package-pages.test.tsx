// Smoke render Package (PRD P3 v2.1 §5.9) dengan API tiruan: daftar "Menunggu diambil" (waiting=true), detail deep link dengan
// aksi dari allowed_actions + riwayat (termasuk WhatsApp manual), serah terima → POST /pickup, retur wajib alasan.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/lib/i18n";

const auth = { perms: [] as string[] };
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    can: (p: string) => auth.perms.includes(p),
    principal: { id: "u1", full_name: "Resepsionis", properties: [] },
    propertyId: "p1",
    properties: [{ id: "p1", name: "Menara Demo", code: "MD" }],
  }),
}));
vi.mock("@/lib/profile", async (importOriginal) => ({ ...(await importOriginal<typeof import("@/lib/profile")>()), useProfile: () => ({ has: () => true, loading: false, profile: "office", context: null, capabilities: new Set(), term: (k: string) => k }) }));

import { ToastProvider } from "@/components/bv/common";
import PackagesPage from "./PackagesPage";

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString();
const list = (data: unknown[]) => ({ data, next_cursor: null });
const pkg = {
  id: "pk1", package_number: "PKG-2026-000007", property_id: "p1", unit_location_id: "l12", unit_name: "Unit 1208", tenant_id: "t1", tenant_name: "PT Maju", recipient_user_id: null, recipient_name: "Sari",
  package_type: "parcel", courier: "JNE", tracking_number: "JNE123456", description: "Kardus sedang", storage_location: "Rak A-3", status: "notified", received_at: ago(60 * 24 * 4), received_by_name: "Resepsionis",
  notified_at: ago(60 * 24 * 4), reminder_count: 1, last_reminded_at: ago(60), picked_up_at: null, picked_up_by_name: null, handed_over_by_name: null, handover_note: null, returned_at: null, return_reason: null,
  days_waiting: 4, photos: [], allowed_actions: ["view", "pickup", "notify", "update", "return"], version: 2,
};
const activity = { id: "act1", object_type: "package", object_id: "pk1", actor_user_id: "u1", actor_name: "Resepsionis", action: "whatsapp_manual_sent", from_value: null, to_value: null, payload: { context: "package", recipient: "Sari" }, occurred_at: ago(30), client_recorded_at: null, source: "web" };

function respond(url: string, method: string): unknown {
  const u = new URL(url, "http://localhost");
  const p = u.pathname.replace("/api/v1/", "");
  if (p === "packages" && method === "GET") return list([pkg]);
  if (p === "packages/pk1") return pkg;
  if (p === "packages/pk1/pickup" && method === "POST") return { ...pkg, status: "picked_up", picked_up_by_name: "Sari", picked_up_at: new Date().toISOString(), allowed_actions: ["view"] };
  if (p === "activities") return list([activity]);
  return list([]);
}

beforeEach(() => {
  auth.perms = [];
  // SignaturePad (dialog serah terima) memakai canvas — jsdom tanpa paket canvas
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const body = respond(url, (init?.method ?? "GET").toUpperCase());
    return { ok: true, status: 200, statusText: "200", text: async () => JSON.stringify(body), json: async () => body, headers: new Headers() };
  }));
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const calls = (part: string, method?: string) => vi.mocked(fetch).mock.calls.filter((c) => String(c[0]).includes(part) && (!method || ((c[1] as RequestInit | undefined)?.method ?? "GET").toUpperCase() === method));

function renderAt(path: string, pattern: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ToastProvider>
        <MemoryRouter initialEntries={[path]}>
          <Routes><Route path={pattern} element={<PackagesPage />} /></Routes>
        </MemoryRouter>
      </ToastProvider>
    </QueryClientProvider>,
  );
}

describe("Packages (P3-PKG-01..04)", () => {
  it("daftar default: menunggu diambil (waiting=true), badge lama menunggu, tombol Catat Paket", async () => {
    auth.perms = ["security.packages.view", "security.packages.record"];
    renderAt("/security/packages", "/security/packages");
    expect((await screen.findAllByText("PKG-2026-000007")).length).toBeGreaterThan(0);
    expect(screen.getByText("4 hari")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /Catat Paket/ }).length).toBeGreaterThan(0);
    expect(calls("/api/v1/packages?").some((c) => String(c[0]).includes("waiting=true"))).toBe(true);
  });
  it("detail deep link: aksi dari allowed_actions, riwayat + WhatsApp manual, serah terima → POST /pickup", async () => {
    auth.perms = ["security.packages.view", "security.packages.handover"];
    renderAt("/security/packages/pk1", "/security/packages/:id");
    expect((await screen.findAllByText("PKG-2026-000007")).length).toBeGreaterThan(0);
    for (const label of ["Serah terima", "Kirim pengingat", "Edit", "Retur"]) expect(screen.getAllByRole("button", { name: new RegExp(label) }).length).toBeGreaterThan(0);
    expect(await screen.findByText("Dikirim manual via WhatsApp")).toBeInTheDocument();
    expect(screen.getByText("Pengingat ke-1 terkirim")).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole("button", { name: /Serah terima/ })[0]);
    fireEvent.click(await screen.findByRole("button", { name: /Serahkan paket/ }));
    await waitFor(() => expect(calls("packages/pk1/pickup", "POST").length).toBe(1));
    expect(JSON.parse(String((calls("packages/pk1/pickup", "POST")[0][1] as RequestInit).body))).toMatchObject({ picked_up_by_name: "Sari", signature_attachment_id: null });
  });
});
