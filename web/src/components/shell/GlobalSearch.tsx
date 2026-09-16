// Global search ⌘K (PRD §23; DS §3.1): command palette → hasil Object · Type · Location · Status
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import { Modal } from "@buildingvision/ui";
import { StatusBadge, objectTypeLabel } from "@/components/bv/badges";
import { useSearch } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import type { ObjectType } from "@/lib/status-map";
import { cn } from "@/lib/utils";

export function GlobalSearch({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const [q, setQ] = useState("");
  const [debounced, setDebounced] = useState("");
  const { propertyId } = useAuth();
  const nav = useNavigate();
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q), 250);
    return () => clearTimeout(t);
  }, [q]);
  const res = useSearch(debounced, propertyId);
  const [cursor, setCursor] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);
  const rows = res.data ?? [];
  useEffect(() => setCursor(0), [debounced]);
  useEffect(() => {
    if (!open) setQ("");
  }, [open]);
  const go = (i: number) => {
    const r = rows[i];
    if (!r) return;
    onOpenChange(false);
    nav(r.deep_link);
  };
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") { e.preventDefault(); setCursor((c) => Math.min(rows.length - 1, c + 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setCursor((c) => Math.max(0, c - 1)); }
    else if (e.key === "Enter") { e.preventDefault(); go(cursor); }
  };
  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-idx="${cursor}"]`)?.scrollIntoView({ block: "nearest" });
  }, [cursor]);
  return (
    <Modal isOpen={open} onClose={() => onOpenChange(false)} maxWidth="720px" className="bv-search-modal">
      <div role="combobox" aria-expanded={open} aria-haspopup="listbox" aria-label="Pencarian global" onKeyDown={onKey}>
        <div className="flex items-center gap-2 border-b border-border pb-3">
          <Icon name="search" size={20} className="text-on-surface-variant" />
          <input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Cari Work Order, Task, Service Request, Asset, Tenant, Unit… (mis. WO-2026-000123)" className="w-full bg-transparent text-body text-on-surface outline-none placeholder:text-outline" aria-autocomplete="list" />
          <kbd className="rounded-[var(--radius-xs)] border border-border px-1.5 text-[10px] text-on-surface-variant">Esc</kbd>
        </div>
        <div ref={listRef} role="listbox" className="-mx-2 mt-2 max-h-[420px] overflow-y-auto">
          {debounced.length < 2 && <p className="px-4 py-6 text-center text-sm text-on-surface-variant">Ketik minimal 2 karakter: nomor object, judul, aset, tenant, atau unit.</p>}
          {debounced.length >= 2 && res.isFetched && !rows.length && <p className="p-4 text-sm text-on-surface-variant">Tidak ada hasil untuk “{debounced}”.</p>}
          {rows.map((r, i) => (
            <button
              type="button"
              role="option"
              aria-selected={i === cursor}
              data-idx={i}
              key={r.object_type + r.object_id}
              onMouseEnter={() => setCursor(i)}
              onClick={() => go(i)}
              className={cn("flex w-full cursor-pointer items-center gap-3 rounded-[var(--radius-md)] px-3 py-2 text-left", i === cursor && "bg-surface-container")}
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  {r.business_id && <span className="font-mono text-[13px] font-bold">{r.business_id}</span>}
                  <span className="truncate text-body">{r.title}</span>
                </div>
                <div className="text-xs text-on-surface-variant">
                  {objectTypeLabel[r.object_type] ?? r.object_type}
                  {r.subtitle ? ` · ${r.subtitle}` : ""}
                  {r.location_path ? ` · ${r.location_path}` : ""}
                </div>
              </div>
              {r.status && <StatusBadge objectType={(r.object_type === "location" || r.object_type === "tenant" || r.object_type === "property" || r.object_type === "building" || r.object_type === "unit" ? "asset" : r.object_type) as ObjectType} status={r.status} />}
            </button>
          ))}
        </div>
      </div>
    </Modal>
  );
}
