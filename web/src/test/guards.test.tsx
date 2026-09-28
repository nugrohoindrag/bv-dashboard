// Route guard (PRD P0 §21, §24.1): route tanpa permission → halaman 403 "Akses ditolak".
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/lib/i18n";

const auth = { perms: [] as string[], caps: [] as string[], loading: false };
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ can: (p: string) => auth.perms.includes(p), principal: { id: "u1", is_internal_admin: false } }),
}));
vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ has: (c: string) => auth.caps.includes(c), loading: auth.loading }),
}));

import { RouteGuard } from "@/app/guards";
import { ACCESS } from "@/app/navigation";

function renderGuard(access: Parameters<typeof RouteGuard>[0]["access"]) {
  return render(
    <MemoryRouter>
      <RouteGuard access={access}>
        <p>Konten Work Orders</p>
      </RouteGuard>
    </MemoryRouter>,
  );
}

describe("RouteGuard", () => {
  beforeEach(() => {
    auth.perms = [];
    auth.caps = [];
    auth.loading = false;
  });
  it("merender konten bila permission ada", () => {
    auth.perms = ["operations.work_orders.view"];
    renderGuard(ACCESS.workOrders);
    expect(screen.getByText("Konten Work Orders")).toBeInTheDocument();
  });
  it("merender halaman 403 bila permission tidak ada", () => {
    renderGuard(ACCESS.workOrders);
    expect(screen.queryByText("Konten Work Orders")).not.toBeInTheDocument();
    expect(screen.getByTestId("forbidden-state")).toBeInTheDocument();
    expect(screen.getByText("Akses ditolak")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /kembali ke beranda/i })).toBeInTheDocument();
  });
  it("capability tidak aktif → 403 dengan penjelasan modul; saat masih memuat → skeleton", () => {
    auth.perms = ["billing.invoices.view"];
    auth.loading = true;
    const { unmount } = renderGuard(ACCESS.invoices);
    expect(screen.getByTestId("detail-skeleton")).toBeInTheDocument();
    unmount();
    auth.loading = false;
    renderGuard(ACCESS.invoices);
    expect(screen.getByText(/tidak aktif pada profile property/i)).toBeInTheDocument();
  });
  it("route tanpa deklarasi akses (Design System) selalu terbuka", () => {
    renderGuard(ACCESS.designSystem);
    expect(screen.getByText("Konten Work Orders")).toBeInTheDocument();
  });
});
