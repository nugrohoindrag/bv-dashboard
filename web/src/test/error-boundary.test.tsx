// ErrorBoundary (PRD P0 §23): error render → halaman pemulihan (apa yang terjadi · dampak · aksi), bukan layar putih.
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/lib/i18n";
import { ErrorBoundary } from "@/components/bv/ErrorBoundary";

let shouldThrow = true;
function Boom() {
  if (shouldThrow) throw new Error("Cannot read properties of undefined (reading 'name')");
  return <p>Halaman pulih</p>;
}

describe("ErrorBoundary", () => {
  beforeEach(() => {
    shouldThrow = true;
    vi.spyOn(console, "error").mockImplementation(() => undefined);
  });
  afterEach(() => vi.restoreAllMocks());

  it("menampilkan halaman pemulihan dengan dampak dan aksi pemulihan", () => {
    render(<ErrorBoundary><Boom /></ErrorBoundary>);
    expect(screen.getByTestId("error-recovery")).toBeInTheDocument();
    expect(screen.getByText("Terjadi kesalahan pada halaman ini")).toBeInTheDocument();
    expect(screen.getByText(/Dampak/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /muat ulang/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /kembali ke overview/i })).toBeInTheDocument();
    expect(screen.getByText(/reading 'name'/)).toBeInTheDocument(); // detail teknis
  });

  it("Coba lagi me-render ulang anak; resetKey baru juga memulihkan", () => {
    const { rerender } = render(<ErrorBoundary resetKey="/a"><Boom /></ErrorBoundary>);
    shouldThrow = false;
    fireEvent.click(screen.getByRole("button", { name: /coba lagi/i }));
    expect(screen.getByText("Halaman pulih")).toBeInTheDocument();
    shouldThrow = true;
    rerender(<ErrorBoundary resetKey="/b"><Boom /></ErrorBoundary>);
    expect(screen.getByTestId("error-recovery")).toBeInTheDocument();
    shouldThrow = false;
    rerender(<ErrorBoundary resetKey="/c"><Boom /></ErrorBoundary>);
    expect(screen.getByText("Halaman pulih")).toBeInTheDocument();
  });

  it("chunk gagal dimuat (deploy baru) → ajakan muat ulang", () => {
    function Chunk(): never {
      throw new Error("Failed to fetch dynamically imported module: /assets/Page.js");
    }
    render(<ErrorBoundary><Chunk /></ErrorBoundary>);
    expect(screen.getByText("Versi baru tersedia")).toBeInTheDocument();
  });
});
