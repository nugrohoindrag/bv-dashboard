// Global search ⌘K (PRD §23; DS §3.1): command palette → hasil Object · Type · Location · Status
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Command } from "cmdk";
import { Search } from "lucide-react";
import { Dialog, DialogContent } from "@/components/ui/primitives";
import { StatusBadge, objectTypeLabel } from "@/components/bv/badges";
import { useSearch } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import type { ObjectType } from "@/lib/status-map";

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
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl p-0 [&>div]:p-0" aria-label="Pencarian global">
        <Command shouldFilter={false} label="Pencarian global">
          <div className="flex items-center gap-2 border-b border-border px-4 py-3">
            <Search className="h-4 w-4 text-muted-foreground" />
            <Command.Input autoFocus value={q} onValueChange={setQ} placeholder="Cari Work Order, Task, Service Request, Asset, Tenant, Unit… (mis. WO-2026-000123)" className="w-full bg-transparent text-body outline-none" />
          </div>
          <Command.List className="max-h-[420px] overflow-y-auto p-2">
            {debounced.length >= 2 && res.isFetched && !res.data?.length && <Command.Empty className="p-4 text-sm text-muted-foreground">Tidak ada hasil untuk “{debounced}”.</Command.Empty>}
            {res.data?.map((r) => (
              <Command.Item key={r.object_type + r.object_id} value={r.object_id} onSelect={() => { onOpenChange(false); nav(r.deep_link); }} className="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 aria-selected:bg-muted">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    {r.business_id && <span className="font-mono text-[13px] font-semibold">{r.business_id}</span>}
                    <span className="truncate text-body">{r.title}</span>
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {objectTypeLabel[r.object_type] ?? r.object_type}
                    {r.subtitle ? ` · ${r.subtitle}` : ""}
                    {r.location_path ? ` · ${r.location_path}` : ""}
                  </div>
                </div>
                {r.status && <StatusBadge objectType={(r.object_type === "location" || r.object_type === "tenant" || r.object_type === "property" || r.object_type === "building" || r.object_type === "unit" ? "asset" : r.object_type) as ObjectType} status={r.status} />}
              </Command.Item>
            ))}
          </Command.List>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
