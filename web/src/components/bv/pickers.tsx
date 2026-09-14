// LocationPicker (tree per property) · AssetPicker · TeamPicker · UserPicker (DS §4) — combobox berbasis Popover + cmdk
import { useMemo, useState } from "react";
import { Command } from "cmdk";
import { ChevronDown, ChevronRight, Search, X } from "lucide-react";
import { Button, Popover, PopoverContent, PopoverTrigger } from "@/components/ui/primitives";
import { AssetStatusBadge } from "./badges";
import { useAssets, useLocationTree, useTeams, useUsers } from "@/api/hooks";
import { cn } from "@/lib/utils";
import type { TreeNode } from "@/api/types";

const typeIcon: Record<string, string> = { property: "🏢", building: "🏬", tower: "🏗", floor: "▭", area: "▫", space: "▫", unit: "🚪" };

function flatten(node: TreeNode | undefined, out: { node: TreeNode; path: string }[] = [], prefix: string[] = []): { node: TreeNode; path: string }[] {
  if (!node) return out;
  const p = [...prefix, node.name];
  if (node.location_type !== "property") out.push({ node, path: p.slice(1).join(" / ") });
  node.children?.forEach((c) => flatten(c, out, p));
  return out;
}

export function LocationPicker({ propertyId, value, onChange, placeholder = "Pilih lokasi…", allowTypes, className, disabled }: { propertyId?: string | null; value?: string | null; onChange: (id: string | null, node?: TreeNode) => void; placeholder?: string; allowTypes?: string[]; className?: string; disabled?: boolean }) {
  const tree = useLocationTree(propertyId);
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const flat = useMemo(() => flatten(tree.data), [tree.data]);
  const selected = flat.find((f) => f.node.id === value);
  const filtered = q ? flat.filter((f) => f.path.toLowerCase().includes(q.toLowerCase())) : null;

  const renderNode = (n: TreeNode, depth: number): React.ReactNode => {
    if (n.location_type === "property") return n.children?.map((c) => renderNode(c, 0));
    const isOpen = expanded[n.id] ?? depth < 1;
    const selectable = !allowTypes || allowTypes.includes(n.location_type);
    return (
      <div key={n.id}>
        <div className={cn("flex items-center gap-1 rounded px-1 py-1 text-sm hover:bg-muted", value === n.id && "bg-brand-50 text-brand-900")} style={{ paddingLeft: 4 + depth * 14 }}>
          {n.children?.length ? (
            <button type="button" className="rounded p-0.5 hover:bg-neutral-soft" onClick={() => setExpanded((e) => ({ ...e, [n.id]: !isOpen }))} aria-label={isOpen ? "Tutup" : "Buka"}>
              {isOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
            </button>
          ) : (
            <span className="w-[18px]" />
          )}
          <button type="button" disabled={!selectable} className={cn("flex flex-1 items-center gap-1.5 text-left", !selectable && "cursor-default text-muted-foreground")} onClick={() => { if (!selectable) return; onChange(n.id, n); setOpen(false); }}>
            <span aria-hidden className="text-xs">{typeIcon[n.location_type]}</span>
            <span>{n.name}</span>
            <span className="text-xs text-muted-foreground">{n.location_type}</span>
          </button>
        </div>
        {isOpen && n.children?.map((c) => renderNode(c, depth + 1))}
      </div>
    );
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button type="button" variant="secondary" disabled={disabled || !propertyId} className={cn("w-full justify-between font-normal", className)}>
          <span className={cn("truncate", !selected && "text-muted-foreground")}>{selected ? selected.path : placeholder}</span>
          <span className="flex items-center gap-1">
            {value && (
              <X className="h-3.5 w-3.5 text-muted-foreground hover:text-foreground" onClick={(e) => { e.stopPropagation(); onChange(null); }} aria-label="Hapus" />
            )}
            <ChevronDown className="h-4 w-4 opacity-60" />
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[420px] p-0">
        <div className="flex items-center gap-2 border-b border-border px-3 py-2">
          <Search className="h-4 w-4 text-muted-foreground" />
          <input className="w-full bg-transparent text-sm outline-none" placeholder="Cari lokasi…" value={q} onChange={(e) => setQ(e.target.value)} autoFocus />
        </div>
        <div className="max-h-80 overflow-y-auto p-1">
          {tree.isLoading && <p className="p-3 text-sm text-muted-foreground">Memuat…</p>}
          {filtered
            ? filtered.slice(0, 50).map((f) => (
                <button key={f.node.id} type="button" disabled={!!allowTypes && !allowTypes.includes(f.node.location_type)} className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-muted disabled:opacity-50" onClick={() => { onChange(f.node.id, f.node); setOpen(false); }}>
                  <span aria-hidden className="text-xs">{typeIcon[f.node.location_type]}</span>
                  <span className="truncate">{f.path}</span>
                </button>
              ))
            : tree.data && renderNode(tree.data, 0)}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function ComboBox<T extends { id: string }>({ items, value, onChange, render, label, placeholder, filter, className, disabled, loading }: { items: T[]; value?: string | null; onChange: (id: string | null, item?: T) => void; render: (t: T) => React.ReactNode; label: (t: T) => string; placeholder: string; filter: (t: T, q: string) => boolean; className?: string; disabled?: boolean; loading?: boolean }) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const sel = items.find((i) => i.id === value);
  const list = q ? items.filter((i) => filter(i, q.toLowerCase())) : items;
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button type="button" variant="secondary" disabled={disabled} className={cn("w-full justify-between font-normal", className)}>
          <span className={cn("truncate", !sel && "text-muted-foreground")}>{sel ? label(sel) : placeholder}</span>
          <span className="flex items-center gap-1">
            {value && <X className="h-3.5 w-3.5 text-muted-foreground hover:text-foreground" onClick={(e) => { e.stopPropagation(); onChange(null); }} aria-label="Hapus" />}
            <ChevronDown className="h-4 w-4 opacity-60" />
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[380px] p-0">
        <Command shouldFilter={false} className="text-sm">
          <div className="flex items-center gap-2 border-b border-border px-3 py-2">
            <Search className="h-4 w-4 text-muted-foreground" />
            <Command.Input className="w-full bg-transparent outline-none" placeholder="Cari…" value={q} onValueChange={setQ} autoFocus />
          </div>
          <Command.List className="max-h-72 overflow-y-auto p-1">
            {loading && <div className="p-3 text-muted-foreground">Memuat…</div>}
            {!loading && list.length === 0 && <Command.Empty className="p-3 text-muted-foreground">Tidak ada hasil.</Command.Empty>}
            {list.slice(0, 100).map((it) => (
              <Command.Item key={it.id} value={it.id} onSelect={() => { onChange(it.id, it); setOpen(false); }} className={cn("cursor-pointer rounded px-2 py-1.5 aria-selected:bg-muted", value === it.id && "bg-brand-50")}>
                {render(it)}
              </Command.Item>
            ))}
          </Command.List>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

export function AssetPicker({ propertyId, locationId, value, onChange, className, disabled }: { propertyId?: string | null; locationId?: string | null; value?: string | null; onChange: (id: string | null) => void; className?: string; disabled?: boolean }) {
  const assets = useAssets({ property_id: propertyId ?? undefined, location_id: locationId ?? undefined });
  return (
    <ComboBox
      items={assets.data ?? []}
      loading={assets.isLoading}
      value={value}
      onChange={(id) => onChange(id)}
      placeholder="Pilih aset (opsional)…"
      className={className}
      disabled={disabled}
      label={(a) => `${a.asset_code} · ${a.name}`}
      filter={(a, q) => a.asset_code.toLowerCase().includes(q) || a.name.toLowerCase().includes(q)}
      render={(a) => (
        <div className="flex items-center justify-between gap-2">
          <div className="min-w-0">
            <div className="font-mono text-xs">{a.asset_code} <span className="font-sans text-body">{a.name}</span></div>
            <div className="truncate text-xs text-muted-foreground">{a.location_path}</div>
          </div>
          <AssetStatusBadge status={a.status} />
        </div>
      )}
    />
  );
}

export function TeamPicker({ propertyId, domain, value, onChange, className, disabled }: { propertyId?: string | null; domain?: string; value?: string | null; onChange: (id: string | null) => void; className?: string; disabled?: boolean }) {
  const teams = useTeams(propertyId, domain);
  return <ComboBox items={teams.data ?? []} loading={teams.isLoading} value={value} onChange={(id) => onChange(id)} placeholder="Pilih team…" className={className} disabled={disabled} label={(t) => t.name} filter={(t, q) => t.name.toLowerCase().includes(q)} render={(t) => <div className="flex justify-between"><span>{t.name}</span><span className="text-xs text-muted-foreground">{t.domain} · {t.members.length} anggota</span></div>} />;
}

export function UserPicker({ propertyId, teamId, role, value, onChange, className, disabled, placeholder = "Pilih user…" }: { propertyId?: string | null; teamId?: string | null; role?: string; value?: string | null; onChange: (id: string | null) => void; className?: string; disabled?: boolean; placeholder?: string }) {
  const users = useUsers({ property_id: propertyId ?? undefined, team_id: teamId ?? undefined, role, is_active: true });
  return <ComboBox items={users.data ?? []} loading={users.isLoading} value={value} onChange={(id) => onChange(id)} placeholder={placeholder} className={className} disabled={disabled} label={(u) => u.full_name} filter={(u, q) => u.full_name.toLowerCase().includes(q) || (u.email ?? "").toLowerCase().includes(q)} render={(u) => <div className="flex justify-between"><span>{u.full_name}</span><span className="text-xs text-muted-foreground">{u.roles.map((r) => r.role_name).filter(Boolean).join(", ")}</span></div>} />;
}
