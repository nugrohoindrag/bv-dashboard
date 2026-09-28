// SignaturePad (PRD P0 v2 §12.2): gambar dengan pointer → Simpan mengekspor PNG (canvas di-mock karena jsdom tanpa canvas).
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/lib/i18n";
import { SignaturePad } from "@/components/bv/SignaturePad";
import { canvasToPngBlob, signatureFile } from "@/lib/signature";

const ctx = { fillRect: vi.fn(), beginPath: vi.fn(), moveTo: vi.fn(), lineTo: vi.fn(), stroke: vi.fn(), scale: vi.fn(), fillStyle: "", strokeStyle: "", lineWidth: 1, lineCap: "", lineJoin: "" };
let getContext: ReturnType<typeof vi.spyOn>;
let toBlob: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  getContext = vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockImplementation(() => ctx as unknown as CanvasRenderingContext2D);
  toBlob = vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(function (cb: BlobCallback, type?: string) {
    cb(new Blob(["png-bytes"], { type: type ?? "image/png" }));
  });
});
afterEach(() => vi.restoreAllMocks());

describe("SignaturePad", () => {
  it("Simpan nonaktif sebelum ada coretan; setelah menggambar mengekspor Blob PNG", async () => {
    const onSave = vi.fn();
    render(<SignaturePad onSave={onSave} />);
    const save = screen.getByRole("button", { name: /simpan tanda tangan/i });
    expect(save).toBeDisabled();
    const canvas = screen.getByTestId("signature-canvas");
    fireEvent.pointerDown(canvas, { clientX: 10, clientY: 10, pointerId: 1 });
    fireEvent.pointerMove(canvas, { clientX: 40, clientY: 30, pointerId: 1 });
    fireEvent.pointerUp(canvas, { pointerId: 1 });
    expect(ctx.stroke).toHaveBeenCalled();
    expect(save).toBeEnabled();
    fireEvent.click(save);
    await waitFor(() => expect(onSave).toHaveBeenCalledOnce());
    const blob = onSave.mock.calls[0][0] as Blob;
    expect(blob.type).toBe("image/png");
    expect(toBlob).toHaveBeenCalledWith(expect.any(Function), "image/png");
  });

  it("Hapus mengosongkan kanvas dan menonaktifkan Simpan lagi", () => {
    render(<SignaturePad onSave={vi.fn()} />);
    const canvas = screen.getByTestId("signature-canvas");
    fireEvent.pointerDown(canvas, { clientX: 1, clientY: 1, pointerId: 1 });
    fireEvent.pointerMove(canvas, { clientX: 5, clientY: 5, pointerId: 1 });
    const fills = ctx.fillRect.mock.calls.length;
    fireEvent.click(screen.getByRole("button", { name: /^hapus$/i }));
    expect(ctx.fillRect.mock.calls.length).toBeGreaterThan(fills);
    expect(screen.getByRole("button", { name: /simpan tanda tangan/i })).toBeDisabled();
    expect(getContext).toHaveBeenCalledWith("2d");
  });

  it("canvasToPngBlob + signatureFile menghasilkan File PNG untuk upload attachment signature", async () => {
    const blob = await canvasToPngBlob(document.createElement("canvas"));
    const file = signatureFile(blob, "sig.png");
    expect(file.name).toBe("sig.png");
    expect(file.type).toBe("image/png");
  });
});
