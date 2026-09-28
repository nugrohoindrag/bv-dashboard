// SignaturePad (PRD P0 v2 §12.2, item `signature`): canvas + pointer events (mouse, pen, sentuh), Hapus / Simpan →
// PNG (lib/signature). Pemanggil mengunggahnya sebagai attachment bertipe "signature".
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/primitives";
import { canvasToPngBlob, themeInk } from "@/lib/signature";
import { cn } from "@/lib/utils";

export function SignaturePad({ onSave, saving, height = 160, className }: { onSave: (png: Blob) => void | Promise<void>; saving?: boolean; height?: number; className?: string }) {
  const { t } = useTranslation();
  const ref = useRef<HTMLCanvasElement>(null);
  const drawing = useRef(false);
  const last = useRef<{ x: number; y: number } | null>(null);
  const [dirty, setDirty] = useState(false);

  const paint = () => {
    const c = ref.current;
    const ctx = c?.getContext("2d");
    if (!c || !ctx) return;
    const { paper } = themeInk();
    ctx.fillStyle = paper;
    ctx.fillRect(0, 0, c.width, c.height);
  };
  useEffect(() => {
    const c = ref.current;
    if (!c) return;
    // resolusi canvas = ukuran tampil × devicePixelRatio agar garis tajam
    const ratio = typeof window !== "undefined" ? window.devicePixelRatio || 1 : 1;
    const w = c.clientWidth || 320;
    c.width = Math.round(w * ratio);
    c.height = Math.round(height * ratio);
    const ctx = c.getContext("2d");
    ctx?.scale?.(ratio, ratio);
    paint();
  }, [height]);

  const point = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    return { x: e.clientX - r.left, y: e.clientY - r.top };
  };
  const down = (e: React.PointerEvent<HTMLCanvasElement>) => {
    e.currentTarget.setPointerCapture?.(e.pointerId);
    drawing.current = true;
    last.current = point(e);
  };
  const move = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!drawing.current || !last.current) return;
    const ctx = e.currentTarget.getContext("2d");
    if (!ctx) return;
    const p = point(e);
    ctx.strokeStyle = themeInk().ink;
    ctx.lineWidth = e.pointerType === "pen" ? Math.max(1.5, 3 * (e.pressure || 0.5)) : 2.2;
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    ctx.beginPath();
    ctx.moveTo(last.current.x, last.current.y);
    ctx.lineTo(p.x, p.y);
    ctx.stroke();
    last.current = p;
    if (!dirty) setDirty(true);
  };
  const up = () => {
    drawing.current = false;
    last.current = null;
  };
  const clear = () => {
    paint();
    setDirty(false);
  };
  const save = async () => {
    if (!ref.current) return;
    const blob = await canvasToPngBlob(ref.current);
    await onSave(blob);
  };
  return (
    <div className={cn("space-y-2", className)}>
      <canvas
        ref={ref}
        data-testid="signature-canvas"
        aria-label={t("checklist.signature_area")}
        role="img"
        className="block w-full touch-none rounded-[var(--radius-md)] border border-dashed border-outline-variant bg-surface"
        style={{ height }}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={up}
        onPointerLeave={up}
        onPointerCancel={up}
      />
      <div className="flex items-center justify-between gap-2">
        <span className="text-caption text-on-surface-variant">{t("checklist.signature_hint")}</span>
        <span className="flex gap-2">
          <Button size="sm" variant="ghost" onClick={clear} disabled={!dirty || saving}>{t("checklist.signature_clear")}</Button>
          <Button size="sm" onClick={save} disabled={!dirty} loading={saving}>{t("checklist.signature_save")}</Button>
        </span>
      </div>
    </div>
  );
}
