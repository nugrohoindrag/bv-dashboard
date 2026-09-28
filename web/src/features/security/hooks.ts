// Hook & konstanta bersama layar Security P2 (dipisah dari shared.tsx agar file komponen hanya mengekspor komponen).
import { useEffect, useState } from "react";
import { useAuth } from "@/lib/auth";

/** Target sentuh ≥ 44px di lebar mobile (PRD P0 §22) untuk tombol aksi layar Security. */
export const TOUCH = "max-md:min-h-11";

/** Waktu sekarang (ms) yang diperbarui tiap `intervalMs` (mis. hitungan "menunggu respons"); 0 = tidak berdetak. */
export function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!intervalMs) return;
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

/** Property untuk form create: property aktif di header → property pertama yang dapat diakses. */
export function usePropertyChoice(initial?: string | null) {
  const { propertyId, properties } = useAuth();
  return useState<string>(initial ?? propertyId ?? properties[0]?.id ?? "");
}
