// My Profile sessions & gating registry Platform Admin (PRD P0 v2 §6, §24.1).
import { render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/lib/i18n";
import { SessionList } from "@/components/bv/sessions";
import { deviceLabel } from "@/lib/device";
import type { OrganizationSummary, Session } from "@/api/types";

const auth = { platform: false };
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ can: () => true, principal: { id: "u1", is_platform_admin: auth.platform }, properties: [], propertyId: null }),
}));
const apiMock = vi.fn();
vi.mock("@/lib/api", async (orig) => ({ ...(await orig<typeof import("@/lib/api")>()), api: (...a: unknown[]) => apiMock(...a) }));

import PlatformOrganizationsSection from "@/features/settings/PlatformOrganizationsSection";
import { ToastProvider } from "@/components/bv/common";

const sessions: Session[] = [
  { id: "s1", client: "web", device_id: null, ip: "10.0.0.1", user_agent: "Mozilla/5.0 (Windows NT 10.0) Chrome/130.0", created_at: "2026-09-26T01:00:00Z", last_used_at: "2026-09-27T01:00:00Z", expires_at: "2026-10-27T01:00:00Z", current: true },
  { id: "s2", client: "mobile", device_id: "dev-1", ip: "10.0.0.9", user_agent: "BuildingVision/1.4 (Android 14)", created_at: "2026-09-20T01:00:00Z", last_used_at: null, expires_at: "2026-10-20T01:00:00Z", current: false },
];

describe("SessionList (My Profile)", () => {
  it("menandai perangkat ini dan hanya sesi lain yang bisa dicabut", () => {
    const onRevoke = vi.fn();
    render(<SessionList sessions={sessions} onRevoke={onRevoke} />);
    const items = within(screen.getByTestId("session-list")).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(within(items[0]).getByText("Perangkat ini")).toBeInTheDocument();
    expect(within(items[0]).queryByRole("button", { name: "Cabut" })).not.toBeInTheDocument();
    within(items[1]).getByRole("button", { name: "Cabut" }).click();
    expect(onRevoke).toHaveBeenCalledWith(sessions[1]);
    expect(items[1]).toHaveTextContent("10.0.0.9");
  });
  it("label perangkat dari client + user agent; daftar kosong punya pesan", () => {
    expect(deviceLabel(sessions[0])).toBe("Web — Chrome · Windows");
    expect(deviceLabel(sessions[1])).toBe("Staff App — Android");
    render(<SessionList sessions={[]} />);
    expect(screen.getByText("Tidak ada sesi aktif.")).toBeInTheDocument();
  });
});

describe("Registry organization (Platform Admin)", () => {
  const wrap = () =>
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter><ToastProvider><PlatformOrganizationsSection /></ToastProvider></MemoryRouter>
      </QueryClientProvider>,
    );
  beforeEach(() => apiMock.mockReset());
  it("tersembunyi (403) bila bukan platform admin — tidak memanggil API", () => {
    auth.platform = false;
    wrap();
    expect(screen.getByTestId("forbidden-state")).toBeInTheDocument();
    expect(screen.queryByTestId("platform-orgs")).not.toBeInTheDocument();
    expect(apiMock).not.toHaveBeenCalled();
  });
  it("platform admin melihat daftar organization dengan jumlah user & status", async () => {
    auth.platform = true;
    const org = { id: "o1", code: "ORG-1", slug: "menara", name: "Menara Demo", status: "suspended", is_internal: false, user_count: 12, property_count: 3, plan_code: "pro", trial_status: "converted", created_at: "2026-01-01T00:00:00Z" } as OrganizationSummary;
    apiMock.mockResolvedValue({ data: [org] });
    wrap();
    expect(await screen.findByText("Menara Demo")).toBeInTheDocument();
    expect(within(screen.getByRole("table")).getByText("Ditangguhkan")).toBeInTheDocument();
    expect(within(screen.getByRole("table")).getByText("12")).toBeInTheDocument();
    await waitFor(() => expect(apiMock).toHaveBeenCalledWith("platform/organizations", expect.anything()));
  });
});
