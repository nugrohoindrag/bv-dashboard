// DataGrid (PRD P0 §22–§23): error query tampil sebagai ErrorState (bukan kosong) + mode kartu di layar sempit.
import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { afterEach, describe, expect, it, vi } from "vitest";
import "@/lib/i18n";
import { DataGrid } from "@/components/bv/datagrid";
import { StatusBadge } from "@/components/bv/badges";

interface Row { id: string; number: string; title: string; location: string; status: string; assignee: string; due: string }
const rows: Row[] = [
  { id: "1", number: "WO-1", title: "AC bocor", location: "Tower A / L12", status: "in_progress", assignee: "Budi", due: "Besok" },
  { id: "2", number: "WO-2", title: "Lampu koridor", location: "Tower B / L3", status: "on_hold", assignee: "Sari", due: "Lusa" },
];
const cols: ColumnDef<Row, unknown>[] = [
  { id: "number", header: "ID", cell: ({ row }) => row.original.number },
  { id: "title", header: "Judul", cell: ({ row }) => row.original.title },
  { id: "location", header: "Lokasi", cell: ({ row }) => row.original.location },
  { id: "status", header: "Status", cell: ({ row }) => <StatusBadge objectType="work_order" status={row.original.status} /> },
  { id: "assignee", header: "Assignee", cell: ({ row }) => row.original.assignee },
  { id: "due", header: "Due", cell: ({ row }) => row.original.due },
];
const wrap = (ui: React.ReactNode) => render(<MemoryRouter>{ui}</MemoryRouter>);

describe("DataGrid states", () => {
  it("error query → ErrorState dengan retry, bukan empty state", () => {
    const retry = vi.fn();
    wrap(<DataGrid columns={cols} rows={[]} rowId={(r) => r.id} error={new Error("Network down")} onRetry={retry} empty={{ title: "Belum ada Work Order." }} />);
    expect(screen.queryByText("Belum ada Work Order.")).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Gagal memuat data");
    fireEvent.click(screen.getByRole("button", { name: /coba lagi/i }));
    expect(retry).toHaveBeenCalledOnce();
  });
  it("isLoading → skeleton tabel; kosong → EmptyState; filter → pesan filter", () => {
    const a = wrap(<DataGrid columns={cols} rows={[]} rowId={(r) => r.id} isLoading />);
    expect(screen.getByTestId("table-skeleton")).toBeInTheDocument();
    a.unmount();
    const b = wrap(<DataGrid columns={cols} rows={[]} rowId={(r) => r.id} empty={{ title: "Belum ada Work Order.", description: "Dibuat dari Task." }} />);
    expect(screen.getByText("Dibuat dari Task.")).toBeInTheDocument();
    b.unmount();
    wrap(<DataGrid columns={cols} rows={[]} rowId={(r) => r.id} isFiltered />);
    expect(screen.getByText("Tidak ada hasil untuk filter ini.")).toBeInTheDocument();
  });
  it("error saat refetch tidak menyembunyikan data lama", () => {
    wrap(<DataGrid columns={cols} rows={rows} rowId={(r) => r.id} error={new Error("x")} layout="table" />);
    expect(screen.getByText("AC bocor")).toBeInTheDocument();
  });
});

describe("DataGrid mobile card mode", () => {
  afterEach(() => {
    // @ts-expect-error bersihkan mock
    delete window.matchMedia;
  });
  it("layout=cards: kolom pertama + 2 berikutnya + status; kolom lain tidak ditampilkan", () => {
    wrap(<DataGrid columns={cols} rows={rows} rowId={(r) => r.id} layout="cards" />);
    const list = screen.getByTestId("datagrid-cards");
    const first = within(list).getAllByRole("listitem")[0];
    expect(first).toHaveTextContent("WO-1");
    expect(first).toHaveTextContent("Judul");
    expect(first).toHaveTextContent("Tower A / L12");
    expect(first).toHaveTextContent("Sedang Dikerjakan");
    expect(first).not.toHaveTextContent("Budi");
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
  it("meta kolom mobile menentukan isi kartu", () => {
    const withMeta = cols.map((c) => ({ ...c, meta: { mobile: c.id === "title" ? "primary" : c.id === "status" ? "status" : c.id === "assignee" ? "secondary" : "hidden" } })) as ColumnDef<Row, unknown>[];
    wrap(<DataGrid columns={withMeta} rows={rows} rowId={(r) => r.id} layout="cards" />);
    const first = within(screen.getByTestId("datagrid-cards")).getAllByRole("listitem")[0];
    expect(first).toHaveTextContent("AC bocor");
    expect(first).toHaveTextContent("Budi");
    expect(first).not.toHaveTextContent("WO-1");
  });
  it("layout=auto memakai kartu bila viewport < 640px", () => {
    window.matchMedia = vi.fn().mockImplementation((q: string) => ({ matches: q.includes("max-width: 639px"), media: q, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
    wrap(<DataGrid columns={cols} rows={rows} rowId={(r) => r.id} />);
    expect(screen.getByTestId("datagrid-cards")).toBeInTheDocument();
  });
  it("layout=auto memakai tabel di desktop", () => {
    wrap(<DataGrid columns={cols} rows={rows} rowId={(r) => r.id} />);
    expect(screen.getByRole("table")).toBeInTheDocument();
  });
});
