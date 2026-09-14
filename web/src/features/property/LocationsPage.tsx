// Property (PRD §8): hierarki Property → Building → Tower → Floor → Area/Space/Unit. Halaman per tipe (route /property/:type)
// dengan tree di kiri (LocationPicker tree) dan tabel di kanan; tambah/edit lokasi per level.
import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronRight, Plus } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, useToast } from "@/components/bv/common";
import { useAll, useCreate, useLocationTree, useUpdate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import type { Location, TreeNode } from "@/api/types";

const TYPES = ["properties", "buildings", "towers", "floors", "areas", "spaces", "units"] as const;
const singular: Record<string, Location["location_type"]> = { properties: "property", buildings: "building", towers: "tower", floors: "floor", areas: "area", spaces: "space", units: "unit" };
const parentOf: Record<string, Location["location_type"][]> = { property: [], building: ["property"], tower: ["building"], floor: ["building", "tower"], area: ["floor", "building", "property"], space: ["floor", "area", "building"], unit: ["floor", "tower", "building"] };
export const typeLabel: Record<string, string> = { property: "Property", building: "Building", tower: "Tower", floor: "Floor", area: "Area", space: "Space", unit: "Unit" };

export default function LocationsPage() {
  const { type = "properties" } = useParams();
  const { t } = useTranslation();
  const { propertyId, can, refreshPrincipal } = useAuth();
  const nav = useNavigate();
  const lt = singular[type] ?? "property";
  const [q, setQ] = useState("");
  const [parentId, setParentId] = useState<string | null>(null);
  const list = useAll<Location>(TYPES.includes(type as (typeof TYPES)[number]) ? type : "locations", { property_id: lt === "property" ? undefined : propertyId ?? undefined, parent_id: parentId ?? undefined, q: q || undefined, include_inactive: true });
  const [edit, setEdit] = useState<Location | null | "new">(null);
  const columns = useMemo<ColumnDef<Location, unknown>[]>(
    () => [
      { id: "code", header: "Kode", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.code}</span>, size: 140 },
      { id: "name", header: "Nama", cell: ({ row }) => <div><div className="font-medium">{row.original.name}</div><div className="text-xs text-muted-foreground">{row.original.path_text}</div></div> },
      { id: "detail", header: "Detail", cell: ({ row }) => <span className="text-xs text-muted-foreground">{Object.entries(row.original.details ?? {}).filter(([, v]) => v !== null && v !== "" && v !== undefined).slice(0, 4).map(([k, v]) => `${k}: ${String(v)}`).join(" · ")}</span> },
      { id: "children", header: "Anak", cell: ({ row }) => <span className="tnum">{row.original.child_count}</span>, size: 70 },
      { id: "active", header: "Aktif", cell: ({ row }) => <span className={cn("text-xs", row.original.is_active ? "text-success-text" : "text-muted-foreground")}>{row.original.is_active ? "Aktif" : "Nonaktif"}</span>, size: 80 },
    ],
    [],
  );
  return (
    <div>
      <PageHeader title={`${t("nav.property")} · ${t(`nav.${type}`)}`} actions={can(lt === "property" ? "property.properties.create" : "property.locations.create") && <Button onClick={() => setEdit("new")}><Plus /> Tambah {typeLabel[lt]}</Button>}>
        <div className="flex items-center gap-2">
          <Input className="w-72" placeholder={`Cari ${typeLabel[lt].toLowerCase()}…`} value={q} onChange={(e) => setQ(e.target.value)} />
          {parentId && <Button variant="ghost" size="sm" onClick={() => setParentId(null)}>Reset induk</Button>}
        </div>
      </PageHeader>
      <div className="grid grid-cols-12 gap-5">
        <div className="col-span-3">
          <div className="rounded-lg border border-border bg-card p-2">
            <div className="px-2 pb-2 text-xs font-semibold uppercase text-muted-foreground">Hierarki</div>
            {propertyId ? <TreeView propertyId={propertyId} selected={parentId} onSelect={(n) => setParentId(n.id === parentId ? null : n.id)} onOpen={(n) => nav(`/property/locations/${n.id}`)} /> : <p className="px-2 text-sm text-muted-foreground">Pilih property di header untuk melihat hierarki.</p>}
          </div>
        </div>
        <div className="col-span-9">
          <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} onRowClick={(r) => `/property/locations/${r.id}`} loading={list.isLoading} isFiltered={!!q || !!parentId} empty={{ message: `Belum ada ${typeLabel[lt].toLowerCase()}.` }} rowActions={(r) => (can("property.locations.update") ? [{ label: "Edit", onSelect: () => setEdit(r) }] : [])} />
        </div>
      </div>
      {edit && <LocationDialog locationType={lt} item={edit === "new" ? null : edit} defaultParent={parentId} onClose={() => setEdit(null)} onSaved={() => { if (lt === "property") refreshPrincipal(); }} />}
    </div>
  );
}

export function TreeView({ propertyId, selected, onSelect, onOpen, allowTypes }: { propertyId: string; selected?: string | null; onSelect?: (n: TreeNode) => void; onOpen?: (n: TreeNode) => void; allowTypes?: string[] }) {
  const tree = useLocationTree(propertyId);
  return (
    <AsyncState query={tree}>
      {(root) => <ul className="text-sm"><TreeNodeRow node={root} depth={0} selected={selected} onSelect={onSelect} onOpen={onOpen} allowTypes={allowTypes} /></ul>}
    </AsyncState>
  );
}

function TreeNodeRow({ node, depth, selected, onSelect, onOpen, allowTypes }: { node: TreeNode; depth: number; selected?: string | null; onSelect?: (n: TreeNode) => void; onOpen?: (n: TreeNode) => void; allowTypes?: string[] }) {
  const [open, setOpen] = useState(depth < 2);
  const kids = node.children ?? [];
  const selectable = !allowTypes || allowTypes.includes(node.location_type);
  return (
    <li>
      <div className={cn("flex items-center gap-1 rounded px-1 py-0.5 hover:bg-muted", selected === node.id && "bg-brand-50 text-brand-700")} style={{ paddingLeft: depth * 12 + 4 }}>
        <button type="button" className="h-4 w-4 shrink-0 text-muted-foreground" onClick={() => setOpen(!open)} aria-label={open ? "Tutup" : "Buka"}>{kids.length > 0 ? open ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" /> : null}</button>
        <button type="button" className={cn("min-w-0 flex-1 truncate text-left", !selectable && "text-muted-foreground")} onClick={() => selectable && onSelect?.(node)} onDoubleClick={() => onOpen?.(node)} title={node.path_text}>
          <span className="mr-1 text-[10px] uppercase text-muted-foreground">{node.location_type.slice(0, 3)}</span>{node.name}
        </button>
      </div>
      {open && kids.length > 0 && <ul>{kids.map((c) => <TreeNodeRow key={c.id} node={c} depth={depth + 1} selected={selected} onSelect={onSelect} onOpen={onOpen} allowTypes={allowTypes} />)}</ul>}
    </li>
  );
}

const DETAIL_FIELDS: Record<string, { key: string; label: string; type?: "number" | "text"; required?: boolean; options?: string[] }[]> = {
  property: [{ key: "timezone", label: "Timezone (IANA)", required: true }, { key: "property_type", label: "Tipe property", options: ["office", "mall", "apartment", "mixed_use", "hotel", "industrial", "other"] }, { key: "address", label: "Alamat" }, { key: "city", label: "Kota" }],
  building: [{ key: "building_type", label: "Tipe building" }, { key: "total_floors", label: "Jumlah lantai", type: "number" }],
  tower: [{ key: "tower_type", label: "Tipe tower" }],
  floor: [{ key: "floor_number", label: "Nomor lantai", type: "number", required: true }, { key: "floor_label", label: "Label lantai (mis. LG, M)" }],
  area: [{ key: "area_type", label: "Tipe area", options: ["lobby", "corridor", "parking", "toilet", "mechanical", "electrical", "rooftop", "garden", "loading_dock", "other"] }],
  space: [{ key: "space_type", label: "Tipe space", options: ["room", "storage", "office", "retail", "meeting", "other"] }, { key: "area_m2", label: "Luas (m²)", type: "number" }],
  unit: [{ key: "unit_number", label: "Nomor unit", required: true }, { key: "unit_type", label: "Tipe unit", options: ["office", "retail", "residential", "warehouse", "other"] }, { key: "area_m2", label: "Luas (m²)", type: "number" }, { key: "occupancy_status", label: "Status hunian", options: ["vacant", "occupied", "reserved"] }],
};

export function LocationDialog({ locationType, item, defaultParent, onClose, onSaved }: { locationType: Location["location_type"]; item: Location | null; defaultParent?: string | null; onClose: () => void; onSaved?: () => void }) {
  const { t } = useTranslation();
  const { propertyId } = useAuth();
  const toast = useToast();
  const [name, setName] = useState(item?.name ?? "");
  const [parentId, setParentId] = useState<string | null>(item?.parent_id ?? defaultParent ?? (locationType === "building" ? propertyId : null));
  const [sortOrder, setSortOrder] = useState(item?.sort_order?.toString() ?? "0");
  const [isActive, setIsActive] = useState(item?.is_active ?? true);
  const [details, setDetails] = useState<Record<string, string>>(Object.fromEntries(Object.entries(item?.details ?? {}).map(([k, v]) => [k, v == null ? "" : String(v)])));
  const [notes, setNotes] = useState("");
  const fields = DETAIL_FIELDS[locationType] ?? [];
  const create = useCreate<Record<string, unknown>, Location>("locations");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("locations");
  const submit = async () => {
    if (!name.trim()) return toast.error(new Error("Nama wajib"));
    if (locationType !== "property" && !parentId) return toast.error(new Error("Induk wajib dipilih"));
    for (const f of fields) if (f.required && !details[f.key]) return toast.error(new Error(`${f.label} wajib`));
    const d: Record<string, unknown> = {};
    for (const f of fields) if (details[f.key] !== undefined && details[f.key] !== "") d[f.key] = f.type === "number" ? Number(details[f.key]) : details[f.key];
    if (notes) d.notes = notes;
    try {
      if (item) await update.mutateAsync({ id: item.id, version: item.version, name: name.trim(), sort_order: Number(sortOrder), is_active: isActive, details: d });
      else await create.mutateAsync({ location_type: locationType, parent_id: parentId, name: name.trim(), sort_order: Number(sortOrder), details: d });
      toast.success(`${typeLabel[locationType]} disimpan`);
      onSaved?.();
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit ${typeLabel[locationType]} · ${item.name}` : `Tambah ${typeLabel[locationType]}`}>
        <div className="space-y-4">
          <Field label="Nama" required><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
          {locationType !== "property" && !item && (
            <Field label={`Induk (${parentOf[locationType].map((x) => typeLabel[x]).join(" / ")})`} required>
              {propertyId ? <div className="max-h-56 overflow-y-auto rounded-md border border-border p-1"><TreeView propertyId={propertyId} selected={parentId} onSelect={(n) => setParentId(n.id)} allowTypes={parentOf[locationType]} /></div> : <p className="text-sm text-muted-foreground">Pilih property di header.</p>}
            </Field>
          )}
          <div className="grid grid-cols-2 gap-3">
            {fields.map((f) => (
              <Field key={f.key} label={f.label} required={f.required}>
                {f.options ? (
                  <NativeSelect value={details[f.key] ?? ""} onChange={(e) => setDetails({ ...details, [f.key]: e.target.value })}><option value="">—</option>{f.options.map((o) => <option key={o} value={o}>{o}</option>)}</NativeSelect>
                ) : (
                  <Input type={f.type ?? "text"} value={details[f.key] ?? ""} onChange={(e) => setDetails({ ...details, [f.key]: e.target.value })} placeholder={f.key === "timezone" ? "Asia/Jakarta" : undefined} />
                )}
              </Field>
            ))}
            <Field label="Urutan"><Input type="number" value={sortOrder} onChange={(e) => setSortOrder(e.target.value)} /></Field>
          </div>
          {!item && <Field label="Catatan"><Textarea rows={2} value={notes} onChange={(e) => setNotes(e.target.value)} /></Field>}
          {item && <label className="flex items-center gap-2 text-sm"><Checkbox checked={isActive} onCheckedChange={(v) => setIsActive(!!v)} /> Aktif</label>}
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
