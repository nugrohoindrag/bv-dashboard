// Validasi parameter sort (server mengembalikan 400 untuk field sort tak dikenal): web hanya mengirim field whitelist.
export const WORK_ITEM_SORTS = ["due_at", "priority", "created_at", "updated_at", "status", "title"] as const;
export const CASE_SORTS = ["created_at", "updated_at", "priority", "severity", "status", "title"] as const;

/** "-due_at" / "due_at" → dipertahankan bila field ada di whitelist; selain itu undefined (pakai default server). */
export function safeSort(sort: string | null | undefined, allowed: readonly string[]): string | undefined {
  if (!sort) return undefined;
  const parts = sort.split(",").map((s) => s.trim()).filter((s) => allowed.includes(s.replace(/^-/, "")));
  return parts.length ? parts.join(",") : undefined;
}
