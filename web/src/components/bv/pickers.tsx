// LocationPicker (tree per property) · AssetPicker · TeamPicker · UserPicker: combobox di atas Popover terkontrol (primitives) + ikon Material Symbols
import { useMemo, useState } from "react";
import { Icon } from "@buildingvision/ui";
import { Button, Popover } from "@/components/ui/primitives";
import { AssetStatusBadge } from "./badges";
import { useAssets, useLocationTree, useTeams, useUsers } from "@/api/hooks";
import { cn } from "@/lib/utils";
import type { TreeNode } from "@/api/types";

const typeIcon: Record<string, string> = { property: "domain", building: "location_city", tower: "corporate_fare", floor: "layers", area: "grid_view", space: "meeting_room", unit: "door_front" };

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
        <div className={cn("flex items-center gap-1 rounded px-1 py-1 text-sm hover:bg-surface-container", value === n.id && "bg-primary-soft text-on-primary-container")} style={{ paddingLeft: 4 + depth * 14 }}>
          {n.children?.length ? (
            <button type="button" className="rounded p-0.5 hover:bg-surface-container-high" onClick={() => setExpanded((e) => ({ ...e, [n.id]: !isOpen }))} aria-label={isOpen ? "Tutup" : "Buka"}>
              {isOpen ? <Icon name="expand_more" size={14} /> : <Icon name="chevron_right" size={14} />}
            </button>
          ) : (
            <span className="w-[18px]" />
          )}
          <button type="button" disabled={!selectable} className={cn("flex flex-1 items-center gap-1.5 text-left", !selectable && "cursor-default text-on-surface-variant")} onClick={() => { if (!selectable) return; onChange(n.id, n); setOpen(false); }}>
            <Icon name={typeIcon[n.location_type] ?? "place"} size={16} className="text-on-surface-variant" aria-hidden />
            <span>{n.name}</span>
            <span className="text-xs text-on-surface-variant">{n.location_type}</span>
          </button>
        </div>
        {isOpen && n.children?.map((c) => renderNode(c, depth + 1))}
      </div>
    );
  };

  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      align="start"
      width={420}
      className="p-0"
      triggerClassName={cn("w-full", className)}
      trigger={
        <PickerTrigger disabled={disabled || !propertyId} empty={!selected} onClear={value ? () => onChange(null) : undefined}>
          {selected ? selected.path : placeholder}
        </PickerTrigger>
      }
    >
        <div className="flex items-center gap-2 border-b border-border px-3 py-2">
          <Icon name="search" size={16} className="text-on-surface-variant" />
          <input className="w-full bg-transparent text-sm outline-none" placeholder="Cari lokasi…" value={q} onChange={(e) => setQ(e.target.value)} autoFocus />
        </div>
        <div className="max-h-80 overflow-y-auto p-1">
          {tree.isLoading && <p className="p-3 text-sm text-on-surface-variant">Memuat…</p>}
          {filtered
            ? filtered.slice(0, 50).map((f) => (
                <button key={f.node.id} type="button" disabled={!!allowTypes && !allowTypes.includes(f.node.location_type)} className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-surface-container disabled:opacity-50" onClick={() => { onChange(f.node.id, f.node); setOpen(false); }}>
                  <Icon name={typeIcon[f.node.location_type] ?? "place"} size={16} className="text-on-surface-variant" aria-hidden />
                  <span className="truncate">{f.path}</span>
                </button>
              ))
            : tree.data && renderNode(tree.data, 0)}
        </div>
    </Popover>
  );
}

function ComboBox<T extends { id: string }>({ items, value, onChange, render, label, placeholder, filter, className, disabled, loading }: { items: T[]; value?: string | null; onChange: (id: string | null, item?: T) => void; render: (t: T) => React.ReactNode; label: (t: T) => string; placeholder: string; filter: (t: T, q: string) => boolean; className?: string; disabled?: boolean; loading?: boolean }) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const sel = items.find((i) => i.id === value);
  const list = q ? items.filter((i) => filter(i, q.toLowerCase())) : items;
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      align="start"
      width={380}
      className="p-0"
      triggerClassName={cn("w-full", className)}
      trigger={
        <PickerTrigger disabled={disabled} empty={!sel} onClear={value ? () => onChange(null) : undefined}>
          {sel ? label(sel) : placeholder}
        </PickerTrigger>
      }
    >
      <div className="text-sm">
        <div className="flex items-center gap-2 border-b border-border px-3 py-2">
          <Icon name="search" size={16} className="text-on-surface-variant" />
          <input className="w-full bg-transparent outline-none" placeholder="Cari…" value={q} onChange={(e) => setQ(e.target.value)} autoFocus />
        </div>
        <div role="listbox" className="max-h-72 overflow-y-auto p-1">
          {loading && <div className="p-3 text-on-surface-variant">Memuat…</div>}
          {!loading && list.length === 0 && <div className="p-3 text-on-surface-variant">Tidak ada hasil.</div>}
          {list.slice(0, 100).map((it) => (
            <button type="button" role="option" aria-selected={value === it.id} key={it.id} onClick={() => { onChange(it.id, it); setOpen(false); }} className={cn("w-full cursor-pointer rounded-[var(--radius-sm)] px-2 py-1.5 text-left hover:bg-surface-container", value === it.id && "bg-primary-soft")}>
              {render(it)}
            </button>
          ))}
        </div>
      </div>
    </Popover>
  );
}

/** Tombol pemicu picker: tampilan outlined field DS + tombol hapus nilai. */
function PickerTrigger({ children, disabled, className, empty, onClear }: { children: React.ReactNode; disabled?: boolean; className?: string; empty?: boolean; onClear?: () => void }) {
  return (
    <span className={cn("inline-flex w-full", className)}>
      <Button type="button" variant="secondary" disabled={disabled} className="w-full justify-between font-normal" style={{ borderRadius: "var(--radius-input)", height: 40 }}>
        <span className={cn("truncate", empty && "text-outline")}>{children}</span>
        <span className="flex items-center gap-1">
          {onClear && (
            <span role="button" aria-label="Hapus" className="inline-flex text-on-surface-variant hover:text-on-surface" onClick={(e) => { e.stopPropagation(); onClear(); }}>
              <Icon name="close" size={14} />
            </span>
          )}
          <Icon name="expand_more" size={16} className="opacity-60" />
        </span>
      </Button>
    </span>
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
            <div className="truncate text-xs text-on-surface-variant">{a.location_path}</div>
          </div>
          <AssetStatusBadge status={a.status} />
        </div>
      )}
    />
  );
}

export function TeamPicker({ propertyId, domain, value, onChange, className, disabled }: { propertyId?: string | null; domain?: string; value?: string | null; onChange: (id: string | null) => void; className?: string; disabled?: boolean }) {
  const teams = useTeams(propertyId, domain);
  return <ComboBox items={teams.data ?? []} loading={teams.isLoading} value={value} onChange={(id) => onChange(id)} placeholder="Pilih team…" className={className} disabled={disabled} label={(t) => t.name} filter={(t, q) => t.name.toLowerCase().includes(q)} render={(t) => <div className="flex justify-between"><span>{t.name}</span><span className="text-xs text-on-surface-variant">{t.domain} · {t.members.length} anggota</span></div>} />;
}

export function UserPicker({ propertyId, teamId, role, value, onChange, className, disabled, placeholder = "Pilih user…" }: { propertyId?: string | null; teamId?: string | null; role?: string; value?: string | null; onChange: (id: string | null) => void; className?: string; disabled?: boolean; placeholder?: string }) {
  const users = useUsers({ property_id: propertyId ?? undefined, team_id: teamId ?? undefined, role, is_active: true });
  return <ComboBox items={users.data ?? []} loading={users.isLoading} value={value} onChange={(id) => onChange(id)} placeholder={placeholder} className={className} disabled={disabled} label={(u) => u.full_name} filter={(u, q) => u.full_name.toLowerCase().includes(q) || (u.email ?? "").toLowerCase().includes(q)} render={(u) => <div className="flex justify-between"><span>{u.full_name}</span><span className="text-xs text-on-surface-variant">{u.roles.map((r) => r.role_name).filter(Boolean).join(", ")}</span></div>} />;
}
