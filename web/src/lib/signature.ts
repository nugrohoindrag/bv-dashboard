// Tanda tangan (PRD P0 v2 §12, item checklist `signature`): utilitas canvas → PNG untuk diunggah sebagai
// attachment bertipe "signature" (presign → PUT → confirm).

/** Ekspor canvas ke Blob PNG; gagal bila canvas kosong/toBlob tidak tersedia. */
export function canvasToPngBlob(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) => {
    if (typeof canvas.toBlob !== "function") return reject(new Error("canvas.toBlob tidak tersedia"));
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("Gagal mengekspor tanda tangan"))), "image/png");
  });
}

/** Blob PNG → File bernama untuk uploadAttachment. */
export function signatureFile(blob: Blob, name = `signature-${Date.now()}.png`): File {
  return new File([blob], name, { type: "image/png" });
}

/** Warna tinta/latar dari token tema aktif (tanpa warna literal; DS Guideline §2.1). */
export function themeInk(): { ink: string; paper: string } {
  if (typeof document === "undefined") return { ink: "currentColor", paper: "transparent" };
  const cs = getComputedStyle(document.documentElement);
  return { ink: cs.getPropertyValue("--md-sys-color-on-surface").trim() || "currentColor", paper: cs.getPropertyValue("--md-sys-color-surface").trim() || "transparent" };
}
