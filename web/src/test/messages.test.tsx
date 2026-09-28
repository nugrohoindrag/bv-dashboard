// Katalog pesan Success/Error (PRD P0 §23): setiap aksi punya kalimat id & en; useToast().action memakai katalog.
import { act, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import i18n from "@/lib/i18n";
import { TOAST_ACTIONS, TRANSITION_TOAST, toastFailure, toastMessage } from "@/lib/messages";
import { ToastProvider, useToast } from "@/components/bv/common";

describe("toast catalog", () => {
  afterEach(() => i18n.changeLanguage("id"));
  it("aksi wajib PRD (create/update/assign/complete/delete) + ekstra ada di katalog", () => {
    for (const a of ["created", "updated", "assigned", "completed", "closed", "deleted", "cancelled", "exported"]) expect(TOAST_ACTIONS).toContain(a);
  });
  it("setiap aksi menghasilkan kalimat (tidak ada key mentah) di id dan en", async () => {
    for (const lng of ["id", "en"]) {
      await i18n.changeLanguage(lng);
      for (const a of TOAST_ACTIONS) {
        const m = toastMessage(a, "Work Order WO-1");
        expect(m).not.toMatch(/toast\./);
        expect(m).toContain("Work Order WO-1");
        expect(toastMessage(a)).not.toMatch(/toast\./);
      }
    }
  });
  it("kalimat Indonesia & kegagalan", () => {
    expect(toastMessage("created", "Work Order WO-1")).toBe("Work Order WO-1 berhasil dibuat");
    expect(toastMessage("deleted")).toBe("Berhasil dihapus");
    expect(toastFailure("saved", "Vendor", "Versi berubah.")).toBe("Vendor gagal disimpan. Versi berubah.");
  });
  it("aksi transisi server dipetakan ke katalog", () => {
    expect(TRANSITION_TOAST.start).toBe("started");
    expect(TRANSITION_TOAST.complete).toBe("completed");
    expect(TRANSITION_TOAST.close).toBe("closed");
  });
  it("useToast().action & transition menampilkan pesan katalog", () => {
    function Demo() {
      const toast = useToast();
      return (
        <>
          <button onClick={() => toast.action("assigned", "Task TSK-9")}>a</button>
          <button onClick={() => toast.transition("complete", "WO-7")}>b</button>
        </>
      );
    }
    render(<MemoryRouter><ToastProvider><Demo /></ToastProvider></MemoryRouter>);
    act(() => fireEvent.click(screen.getByText("a")));
    expect(screen.getByText("Task TSK-9 berhasil ditugaskan")).toBeInTheDocument();
    act(() => fireEvent.click(screen.getByText("b")));
    expect(screen.getByText("WO-7 berhasil diselesaikan")).toBeInTheDocument();
  });
});
