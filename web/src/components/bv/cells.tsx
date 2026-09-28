// Sel tabel standar (29 Sep 2026) — pola "clean & simple" halaman Tasks untuk seluruh tabel dashboard:
// satu baris per informasi, teks panjang dipotong "…" (teks lengkap di tooltip), lokasi = nama terakhir,
// status = satu badge + satu flag terpenting. Detail lengkap ada di halaman detail (menu ⋮ → "Lihat detail").
import type { ReactNode } from "react";
import { FlagBadge, StatusBadge } from "./badges";
import { lastSegment, topFlag } from "./cell-helpers";
import type { FlagSource } from "@/lib/status";
import type { ObjectType } from "@/lib/status-map";
import { cn } from "@/lib/utils";

/** Kolom utama: kode kecil (mono) di atas judul satu baris. */
export function CellTitle({ code, title, className }: { code?: ReactNode; title: string | null | undefined; className?: string }) {
  return (
    <div className={cn("min-w-0", className)}>
      {code ? <div className="truncate font-mono text-xs text-on-surface-variant">{code}</div> : null}
      <div className="truncate font-medium text-on-surface" title={title ?? undefined}>{title || "—"}</div>
    </div>
  );
}

/** Teks satu baris dengan lebar maksimum; teks lengkap di tooltip. */
export function CellText({ children, title, max = 180, muted, className }: { children: ReactNode; title?: string | null; max?: number; muted?: boolean; className?: string }) {
  const tip = title ?? (typeof children === "string" ? children : undefined);
  return (
    <span className={cn("block truncate text-sm", muted && "text-on-surface-variant", className)} style={{ maxWidth: max }} title={tip ?? undefined}>
      {children ?? "—"}
    </span>
  );
}

/** Lokasi: hanya nama lokasi terakhir; path lengkap di tooltip. */
export function CellLocation({ path, max = 160 }: { path: string | null | undefined; max?: number }) {
  return <CellText title={path ?? undefined} max={max}>{path ? lastSegment(path) : "—"}</CellText>;
}

/** Status: satu badge status + satu flag terpenting (tanpa tumpukan badge). */
export function CellStatus({ objectType, status, item, critical }: { objectType: ObjectType; status: string; item?: FlagSource; critical?: boolean }) {
  const flag = item ? topFlag(item, { critical }) : null;
  return (
    <div className="flex flex-wrap items-center gap-1">
      <StatusBadge objectType={objectType} status={status} />
      {flag && <FlagBadge flag={flag} />}
    </div>
  );
}
