// Global UX States (PRD P0 §23): Empty / Error / 403 / Loading + AsyncState.
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import "@/lib/i18n";
import { ApiError } from "@/lib/api";
import { AsyncState } from "@/components/bv/common";
import { EmptyState, ErrorState, FormSkeleton, PageSkeleton, QueryErrorState, TableSkeleton } from "@/components/bv/states";

const wrap = (ui: React.ReactNode) => render(<MemoryRouter>{ui}</MemoryRouter>);
const apiErr = (status: number, detail = "detail") => new ApiError({ type: "about:blank", title: "x", status, detail, code: "X", request_id: "req-1" });

describe("EmptyState", () => {
  it("menampilkan apa yang kosong, mengapa, dan aksi berikutnya", () => {
    wrap(<EmptyState title="Belum ada Work Order." description="Dibuat dari Task atau manual." action={<button>Buat Work Order</button>} />);
    expect(screen.getByText("Belum ada Work Order.")).toBeInTheDocument();
    expect(screen.getByText("Dibuat dari Task atau manual.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Buat Work Order" })).toBeInTheDocument();
  });
  it("tetap kompatibel dengan prop lama message/cta", () => {
    wrap(<EmptyState message="Belum ada data lama." cta={<button>CTA</button>} />);
    expect(screen.getByText("Belum ada data lama.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "CTA" })).toBeInTheDocument();
  });
});

describe("ErrorState & QueryErrorState", () => {
  it("apa yang terjadi + dampak + aksi pemulihan", () => {
    const retry = vi.fn();
    wrap(<ErrorState title="Gagal memuat Work Order" impact="Daftar belum tampil." onRetry={retry} requestId="req-9" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Gagal memuat Work Order");
    expect(screen.getByText(/Daftar belum tampil/)).toBeInTheDocument();
    expect(screen.getByText("req-9")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /coba lagi/i }));
    expect(retry).toHaveBeenCalledOnce();
  });
  it("403 problem+json → Akses ditolak; 404 → data tidak ditemukan; 5xx → server + request id", () => {
    const { unmount } = wrap(<QueryErrorState error={apiErr(403)} />);
    expect(screen.getByTestId("forbidden-state")).toBeInTheDocument();
    unmount();
    const r2 = wrap(<QueryErrorState error={apiErr(404, "")} />);
    expect(screen.getByText("Data tidak ditemukan")).toBeInTheDocument();
    r2.unmount();
    wrap(<QueryErrorState error={apiErr(503, "upstream timeout")} onRetry={() => undefined} />);
    expect(screen.getByText("Server sedang bermasalah")).toBeInTheDocument();
    expect(screen.getByText("req-1")).toBeInTheDocument();
  });
});

describe("AsyncState", () => {
  const q = (over: Partial<{ isLoading: boolean; isError: boolean; error: unknown; data: unknown }>) => ({ isLoading: false, isError: false, error: null, data: undefined, refetch: vi.fn(), ...over });
  it("loading → skeleton; error → ErrorState dengan retry; 403 → Akses ditolak", () => {
    const { container, unmount } = wrap(<AsyncState query={q({ isLoading: true })}>{() => <p>data</p>}</AsyncState>);
    expect(container.querySelector("[aria-busy]")).not.toBeNull();
    unmount();
    const query = q({ isError: true, error: new Error("boom") });
    const r = wrap(<AsyncState query={query}>{() => <p>data</p>}</AsyncState>);
    fireEvent.click(screen.getByRole("button", { name: /coba lagi/i }));
    expect(query.refetch).toHaveBeenCalled();
    r.unmount();
    wrap(<AsyncState query={q({ isError: true, error: apiErr(403) })}>{() => <p>data</p>}</AsyncState>);
    expect(screen.getByText("Akses ditolak")).toBeInTheDocument();
  });
  it("kosong → EmptyState; ada data → children", () => {
    const r = wrap(<AsyncState query={q({ data: [] })} empty={{ title: "Belum ada Finding.", description: "Finding dicatat saat inspeksi." }}>{() => <p>data</p>}</AsyncState>);
    expect(screen.getByText("Belum ada Finding.")).toBeInTheDocument();
    r.unmount();
    wrap(<AsyncState query={q({ data: [1] })}>{(d) => <p>{`n=${(d as number[]).length}`}</p>}</AsyncState>);
    expect(screen.getByText("n=1")).toBeInTheDocument();
  });
});

describe("Loading skeletons", () => {
  it("page / table / form punya penanda aria-busy", () => {
    wrap(<><PageSkeleton /><TableSkeleton /><FormSkeleton /></>);
    expect(screen.getByTestId("page-skeleton")).toHaveAttribute("aria-busy");
    expect(screen.getAllByTestId("table-skeleton").length).toBeGreaterThan(0);
    expect(screen.getByTestId("form-skeleton")).toHaveAttribute("aria-busy");
  });
});
