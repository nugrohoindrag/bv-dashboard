// Property (PRD §8; PRD P0 v2 §7): hierarki Property → Building → Tower → Floor → Area/Space/Unit. Halaman per tipe
// (route /property/:type) dengan tree di kiri (filter subtree = ancestor_id) dan tabel di kanan; filter portfolio untuk
// property; tambah/edit lokasi per level (kode kustom, detail kontak/geo, metadata key-value); aktif/nonaktif + hapus.
import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, SearchInput, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { AsyncState, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellLocation, CellText, CellTitle } from "@/components/bv/cells";
import { PortfolioPicker } from "@/components/bv/pickers";
import { ExportButton } from "@/components/bv/export";
import { useAll, useCreate, useLocationTree, useUpdate } from "@/api/hooks";
import { DeleteLocationDialog, LocationStatusDialog } from "./LocationActions";
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
  const [parentId, setParentId] = useState<string | null>(null); // node tree terpilih → filter subtree (ancestor_id)
  const [portfolioId, setPortfolioId] = useState<string | null>(null);
  const filters = { property_id: lt === "property" ? undefined : propertyId ?? undefined, ancestor_id: parentId ?? undefined, portfolio_id: lt === "property" ? portfolioId ?? undefined : undefined, q: q || undefined, include_inactive: true };
  const list = useAll<Location>(TYPES.includes(type as (typeof TYPES)[number]) ? type : "locations", filters);
  const [edit, setEdit] = useState<Location | null | "new">(null);
  const [status, setStatus] = useState<{ loc: Location; action: "activate" | "deactivate" } | null>(null);
  const [del, setDel] = useState<Location | null>(null);
  const columns = useMemo<ColumnDef<Location, unknown>[]>(
    () => [
      // Pola tabel Tasks (29 Sep 2026): kode + nama satu baris; path lengkap diganti kolom induk (nama induk, path di tooltip).
      { id: "name", header: "Nama", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.code} title={row.original.name} /> },
      ...(lt === "property" ? [] : [{ id: "parent", header: "Induk", meta: { mobile: "secondary" }, cell: ({ row }) => <CellLocation path={parentPathText(row.original.path_text)} max={160} /> } satisfies ColumnDef<Location, unknown>]),
      { id: "detail", header: "Detail", meta: { mobile: "secondary" }, cell: ({ row }) => { const txt = detailSummary(row.original); return <CellText max={240} muted className="text-xs">{txt || "—"}</CellText>; } },
      { id: "children", header: "Anak", meta: { mobile: "hidden", nowrap: true }, cell: ({ row }) => <span className="tnum">{row.original.child_count}</span>, size: 70 },
      { id: "status", header: "Status", meta: { mobile: "status" }, cell: ({ row }) => <LocationStatusBadge loc={row.original} />, size: 100 },
    ],
    [lt],
  );
  return (
    <div>
      <PageHeader title={`${t("nav.property")} · ${t(`nav.${type}`)}`} actions={<><ExportButton resource="locations" filters={{ ...filters, location_type: lt, include_inactive: "true" } as Record<string, string | undefined>} />{can(lt === "property" ? "property.properties.create" : "property.locations.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Tambah {typeLabel[lt]}</Button>}</>}>
        <div className="flex flex-wrap items-center gap-2">
          <div className="w-full sm:w-72"><SearchInput placeholder={`Cari ${typeLabel[lt].toLowerCase()}…`} value={q} onChange={(e) => setQ(e.target.value)} /></div>
          {lt === "property" && can("property.portfolios.view") && <PortfolioPicker className="w-full sm:w-56" value={portfolioId} onChange={setPortfolioId} placeholder={t("loc.all_portfolios")} />}
          {parentId && <Button variant="ghost" size="sm" onClick={() => setParentId(null)}>{t("loc.reset_subtree")}</Button>}
        </div>
      </PageHeader>
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
        <div className="min-w-0 lg:col-span-3">
          <div className="rounded-lg border border-border bg-card p-2">
            <div className="px-2 pb-2 text-xs font-semibold uppercase text-muted-foreground">Hierarki</div>
            {propertyId ? <TreeView propertyId={propertyId} selected={parentId} onSelect={(n) => setParentId(n.id === parentId ? null : n.id)} onOpen={(n) => nav(`/property/locations/${n.id}`)} /> : <p className="px-2 text-sm text-muted-foreground">Pilih property di header untuk melihat hierarki.</p>}
          </div>
        </div>
        <div className="min-w-0 lg:col-span-9">
          <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} onRowClick={(r) => `/property/locations/${r.id}`} loading={list.isLoading} error={list.error} onRetry={() => list.refetch()} isFiltered={!!q || !!parentId || !!portfolioId}
            empty={{ icon: "domain", title: t("loc.empty", { type: typeLabel[lt] }), description: t("loc.empty_desc"), action: can(lt === "property" ? "property.properties.create" : "property.locations.create") ? <Button icon="add" onClick={() => setEdit("new")}>Tambah {typeLabel[lt]}</Button> : undefined }}
            rowActions={(r) => {
              const canUpd = can("property.locations.update") || (r.location_type === "property" && can("property.properties.update"));
              return [
                ...(canUpd ? [{ label: "Edit", icon: "edit", onSelect: () => setEdit(r) }] : []),
                ...(canUpd ? [r.is_active ? { label: t("loc.deactivate"), icon: "block", destructive: true, onSelect: () => setStatus({ loc: r, action: "deactivate" }) } : { label: t("loc.activate"), icon: "check_circle", onSelect: () => setStatus({ loc: r, action: "activate" }) }] : []),
                ...(r.location_type !== "property" && can("property.locations.delete") ? [{ label: t("loc.delete"), icon: "delete", destructive: true, onSelect: () => setDel(r) }] : []),
              ];
            }} />
        </div>
      </div>
      {edit && <LocationDialog locationType={lt} item={edit === "new" ? null : edit} defaultParent={parentId} onClose={() => setEdit(null)} onSaved={() => { if (lt === "property") refreshPrincipal(); }} />}
      {status && <LocationStatusDialog loc={status.loc} action={status.action} onClose={() => setStatus(null)} onDone={() => { if (lt === "property") refreshPrincipal(); }} />}
      {del && <DeleteLocationDialog loc={del} onClose={() => setDel(null)} />}
    </div>
  );
}

/** Status lokasi: property memakai details.status (draft|active|inactive); level lain is_active. */
export function LocationStatusBadge({ loc }: { loc: Location }) {
  const st = (loc.location_type === "property" && typeof loc.details?.status === "string" ? loc.details.status : loc.is_active ? "active" : "inactive") as string;
  return <StatusBadge objectType="location_status" status={st} />;
}

/** Path induk dari path_text ("Gedung / Lantai / Ruang" → "Gedung / Lantai"); kosong untuk lokasi puncak. */
function parentPathText(path: string | null | undefined): string | null {
  const parts = (path ?? "").split(/\s*[/›>]\s*/).map((x) => x.trim()).filter(Boolean);
  return parts.length > 1 ? parts.slice(0, -1).join(" / ") : null;
}

const HIDDEN_DETAIL = new Set(["status", "portfolio_id", "profile", "notes"]);
function detailSummary(l: Location): string {
  return Object.entries(l.details ?? {}).filter(([k, v]) => !HIDDEN_DETAIL.has(k) && v !== null && v !== "" && v !== undefined && typeof v !== "object").slice(0, 4).map(([k, v]) => `${k}: ${String(v)}`).join(" · ");
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
        <button type="button" className="h-4 w-4 shrink-0 text-muted-foreground" onClick={() => setOpen(!open)} aria-label={open ? "Tutup" : "Buka"}>{kids.length > 0 ? open ? <Icon name="expand_more" size={16} /> : <Icon name="chevron_right" size={16} /> : null}</button>
        <button type="button" className={cn("min-w-0 flex-1 truncate text-left", !selectable && "text-muted-foreground")} onClick={() => selectable && onSelect?.(node)} onDoubleClick={() => onOpen?.(node)} title={node.path_text}>
          <span className="mr-1 text-[10px] uppercase text-muted-foreground">{node.location_type.slice(0, 3)}</span>{node.name}
        </button>
      </div>
      {open && kids.length > 0 && <ul>{kids.map((c) => <TreeNodeRow key={c.id} node={c} depth={depth + 1} selected={selected} onSelect={onSelect} onOpen={onOpen} allowTypes={allowTypes} />)}</ul>}
    </li>
  );
}

// Onboarding Brief §6: pilih profile saat Create Property (Hotel / Apartment / Office)
const PROFILE_OPTIONS = [
  { code: "hotel", label: "Hotel", icon: "hotel", desc: "Hotel, kamar tamu, dan operasi hospitality" },
  { code: "apartment", label: "Apartment", icon: "apartment", desc: "Gedung hunian dan layanan penghuni" },
  { code: "office", label: "Office", icon: "business", desc: "Gedung perkantoran dan layanan tenant" },
] as const;

const DETAIL_FIELDS: Record<string, { key: string; label: string; type?: "number" | "text" | "tel"; required?: boolean; options?: string[]; help?: string }[]> = {
  property: [
    { key: "timezone", label: "Timezone (IANA)", required: true }, { key: "property_type", label: "Tipe property (deskriptif)", options: ["office", "mall", "apartment", "mixed_use", "hotel", "industrial", "other"] },
    { key: "address", label: "Alamat" }, { key: "city", label: "Kota" }, { key: "province", label: "Provinsi" }, { key: "postal_code", label: "Kode pos" }, { key: "country", label: "Negara (ISO-2)" },
    { key: "contact_name", label: "Kontak · nama" }, { key: "contact_phone", label: "Kontak · telepon" }, { key: "contact_email", label: "Kontak · email" },
    // PRD P3 v2.1 P3-WAM-05: nomor WhatsApp pengelola (dinormalisasi server ke 62…; kosongkan untuk menghapus)
    { key: "whatsapp_number", label: "WhatsApp pengelola", type: "tel", help: "Tampil di Tenant App (layar login & Akun) sebagai “Hubungi pengelola”. Format 08… atau 62…" },
    { key: "latitude", label: "Latitude", type: "number" }, { key: "longitude", label: "Longitude", type: "number" },
  ],
  building: [{ key: "building_type", label: "Tipe building", options: ["office_tower", "residential", "retail", "mixed_use", "hotel", "parking", "industrial", "other"] }, { key: "address", label: "Alamat" }, { key: "floors_count", label: "Jumlah lantai", type: "number" }, { key: "year_built", label: "Tahun dibangun", type: "number" }, { key: "gross_area_m2", label: "Luas kotor (m²)", type: "number" }],
  tower: [], // tower_type tidak disimpan server
  floor: [{ key: "floor_number", label: "Nomor lantai", type: "number", required: true }, { key: "floor_label", label: "Label lantai (mis. LG, M)" }],
  area: [{ key: "area_type", label: "Tipe area", options: ["lobby", "corridor", "parking", "toilet", "mechanical", "electrical", "rooftop", "garden", "loading_dock", "other"] }],
  space: [{ key: "space_type", label: "Tipe space", options: ["room", "storage", "office", "retail", "meeting", "other"] }, { key: "area_m2", label: "Luas (m²)", type: "number" }],
  unit: [{ key: "unit_number", label: "Nomor unit", required: true }, { key: "unit_type", label: "Tipe unit", options: ["commercial", "residential", "hotel_room"] }, { key: "area_m2", label: "Luas (m²)", type: "number" }, { key: "occupancy_status", label: "Status hunian", options: ["vacant", "occupied", "reserved", "inactive"] }],
};

export function LocationDialog({ locationType, item, defaultParent, onClose, onSaved }: { locationType: Location["location_type"]; item: Location | null; defaultParent?: string | null; onClose: () => void; onSaved?: () => void }) {
  const { t } = useTranslation();
  const { propertyId } = useAuth();
  const toast = useToast();
  const [name, setName] = useState(item?.name ?? "");
  const [parentId, setParentId] = useState<string | null>(item?.parent_id ?? defaultParent ?? (locationType === "building" ? propertyId : null));
  const [sortOrder, setSortOrder] = useState(item?.sort_order?.toString() ?? "0");
  const [details, setDetails] = useState<Record<string, string>>(Object.fromEntries(Object.entries(item?.details ?? {}).filter(([, v]) => typeof v !== "object" || v === null).map(([k, v]) => [k, v == null ? "" : String(v)])));
  const [notes, setNotes] = useState("");
  const [code, setCode] = useState("");
  const [portfolioId, setPortfolioId] = useState<string | null>((item?.details?.portfolio_id as string | undefined) ?? null);
  const [meta, setMeta] = useState<{ k: string; v: string }[]>(Object.entries(item?.metadata ?? {}).map(([k, v]) => ({ k, v: typeof v === "string" ? v : JSON.stringify(v) })));
  const [err, setErr] = useState<string | null>(null);
  const fields = DETAIL_FIELDS[locationType] ?? [];
  const create = useCreate<Record<string, unknown>, Location>("locations");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("locations");
  const submit = async () => {
    setErr(null);
    if (!name.trim()) return setErr("Nama wajib");
    if (locationType === "property" && !item && !details.profile) return setErr("Property Profile wajib dipilih (Hotel / Apartment / Office)");
    if (locationType !== "property" && !parentId) return setErr("Induk wajib dipilih");
    for (const f of fields) if (f.required && !details[f.key]) return setErr(`${f.label} wajib`);
    if (details.country && !/^[A-Za-z]{2}$/.test(details.country)) return setErr("Negara harus kode ISO-2 (mis. ID)");
    if (meta.some((m) => !m.k.trim() && m.v.trim())) return setErr("Kunci metadata wajib diisi");
    const d: Record<string, unknown> = {};
    // nilai opsi lama yang tidak lagi valid (mis. unit_type office/retail) tidak dikirim — server menolaknya
    for (const f of fields) if (details[f.key] !== undefined && details[f.key] !== "" && (!f.options || f.options.includes(details[f.key]))) d[f.key] = f.type === "number" ? Number(details[f.key]) : f.key === "country" ? details[f.key].toUpperCase() : details[f.key];
    if (locationType === "property" && !item && details.profile) d.profile = details.profile;
    if (locationType === "property") {
      // string kosong = hapus nomor WhatsApp pengelola (hanya bila sebelumnya terisi)
      if (!details.whatsapp_number && item?.details?.whatsapp_number) d.whatsapp_number = "";
      const before = (item?.details?.portfolio_id as string | undefined) ?? null;
      if (portfolioId) d.portfolio_id = portfolioId;
      else if (before) d.portfolio_id = ""; // "" = lepas dari portfolio
    }
    if (notes) d.notes = notes;
    const metadata = Object.fromEntries(meta.filter((m) => m.k.trim()).map((m) => [m.k.trim(), m.v]));
    try {
      if (item) await update.mutateAsync({ id: item.id, version: item.version, name: name.trim(), sort_order: Number(sortOrder), details: d, metadata });
      else await create.mutateAsync({ location_type: locationType, parent_id: parentId, name: name.trim(), code: code.trim() || undefined, sort_order: Number(sortOrder), details: d, metadata });
      toast.action(item ? "saved" : "created", `${typeLabel[locationType]} ${name.trim()}`);
      onSaved?.();
      onClose();
    } catch (e) {
      const c = (e as { code?: string }).code;
      setErr(c === "DUPLICATE_CODE" ? t("loc.duplicate_code") : (e as Error).message);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit ${typeLabel[locationType]} · ${item.name}` : `Tambah ${typeLabel[locationType]}`}>
        <div className="space-y-4">
          {err && <Alert variant="critical">{err}</Alert>}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Field label="Nama" required className="sm:col-span-2"><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
            {item ? <Field label="Kode"><Input value={item.code} disabled /></Field> : <Field label="Kode" help={t("loc.code_help")}><Input value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} placeholder="auto" /></Field>}
          </div>
          {locationType === "property" && (
            <Field label="Property Profile" required help={item ? "Perubahan profile hanya lewat Settings → Property Profile (aksi administratif)." : "Menentukan capability, terminologi, dan workflow default. Wajib dipilih sebelum property aktif."}>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
                {PROFILE_OPTIONS.map((o) => {
                  const selected = (details.profile ?? "") === o.code;
                  return (
                    <button key={o.code} type="button" disabled={!!item} onClick={() => setDetails({ ...details, profile: o.code })}
                      className={cn("flex flex-col items-start gap-1 rounded-[var(--radius-md)] border p-3 text-left transition-colors disabled:cursor-not-allowed", selected ? "border-primary bg-primary-soft" : "border-border hover:bg-surface-container")}
                      aria-pressed={selected}>
                      <span className="inline-flex items-center gap-1.5 text-sm font-semibold"><Icon name={o.icon} size={18} /> {o.label}</span>
                      <span className="text-xs text-muted-foreground">{o.desc}</span>
                    </button>
                  );
                })}
              </div>
            </Field>
          )}
          {locationType !== "property" && !item && (
            <Field label={`Induk (${parentOf[locationType].map((x) => typeLabel[x]).join(" / ")})`} required>
              {propertyId ? <div className="max-h-56 overflow-y-auto rounded-md border border-border p-1"><TreeView propertyId={propertyId} selected={parentId} onSelect={(n) => setParentId(n.id)} allowTypes={parentOf[locationType]} /></div> : <p className="text-sm text-muted-foreground">Pilih property di header.</p>}
            </Field>
          )}
          {locationType === "property" && (
            <Field label="Portfolio" help={t("loc.portfolio_help")}>
              <div className="flex gap-2"><PortfolioPicker className="flex-1" value={portfolioId} onChange={setPortfolioId} />{portfolioId && <Button variant="ghost" size="sm" onClick={() => setPortfolioId(null)}>{t("loc.clear")}</Button>}</div>
            </Field>
          )}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {fields.map((f) => (
              <Field key={f.key} label={f.label} required={f.required} help={f.help}>
                {f.options ? (
                  <NativeSelect value={details[f.key] ?? ""} onChange={(e) => setDetails({ ...details, [f.key]: e.target.value })}><option value="">—</option>{details[f.key] && !f.options.includes(details[f.key]) && <option value={details[f.key]} disabled>{details[f.key]} (tidak valid — pilih ulang)</option>}{f.options.map((o) => <option key={o} value={o}>{o}</option>)}</NativeSelect>
                ) : (
                  <Input type={f.type ?? "text"} inputMode={f.type === "tel" ? "tel" : undefined} value={details[f.key] ?? ""} onChange={(e) => setDetails({ ...details, [f.key]: e.target.value })} placeholder={f.key === "timezone" ? "Asia/Jakarta" : f.key === "whatsapp_number" ? "mis. 0812 3456 7890" : undefined} />
                )}
              </Field>
            ))}
            <Field label="Urutan"><Input type="number" value={sortOrder} onChange={(e) => setSortOrder(e.target.value)} /></Field>
          </div>
          {!item && <Field label="Catatan"><Textarea rows={2} value={notes} onChange={(e) => setNotes(e.target.value)} /></Field>}
          <Field label="Metadata" help={t("loc.metadata_help")}>
            <div className="space-y-1.5" data-testid="metadata-editor">
              {meta.map((m, i) => (
                <div key={i} className="flex gap-2">
                  <Input className="w-40" placeholder="kunci" aria-label={`Kunci ${i + 1}`} value={m.k} onChange={(e) => setMeta(meta.map((x, j) => (j === i ? { ...x, k: e.target.value } : x)))} />
                  <Input className="flex-1" placeholder="nilai" aria-label={`Nilai ${i + 1}`} value={m.v} onChange={(e) => setMeta(meta.map((x, j) => (j === i ? { ...x, v: e.target.value } : x)))} />
                  <Button size="icon-sm" variant="ghost" aria-label="Hapus" onClick={() => setMeta(meta.filter((_, j) => j !== i))}><Icon name="close" size={16} /></Button>
                </div>
              ))}
              <Button size="sm" variant="ghost" icon="add" onClick={() => setMeta([...meta, { k: "", v: "" }])}>{t("loc.add_metadata")}</Button>
            </div>
          </Field>
          {item && <p className="text-xs text-on-surface-variant">{t("loc.status_via_actions")} <Badge tone={item.is_active ? "success" : "neutral"}>{item.is_active ? "Aktif" : "Nonaktif"}</Badge></p>}
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
