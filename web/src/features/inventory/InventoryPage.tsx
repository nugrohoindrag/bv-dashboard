// Inventory (PRD P1 v1.3 §25; NC §38): Item Master + stok per lokasi, Stock In/Out/Adjustment, Stock Locations, Stock Movements.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Tabs, TabsContent, TabsList, TabsTrigger, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useAll, useList, useUpdate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { rp } from "@/features/billing/InvoicesPage";

export interface Item { id: string; item_code: string; name: string; description: string | null; category: string; equipment_category_code: string | null; unit: string; min_stock: number; unit_cost: number; barcode: string | null; is_active: boolean; total_quantity: number; low_stock: boolean; levels: { stock_location_id: string; stock_location_name: string; property_id: string; quantity: number }[]; version: number }
export interface StockLocation { id: string; property_id: string; name: string; location_id: string | null; is_default: boolean; is_active: boolean; item_count: number; version: number }
interface Movement { id: string; transaction_number: string; item_code: string; item_name: string; unit: string; stock_location_name: string; transaction_type: string; quantity: number; balance_after: number; unit_cost: number | null; reference_type: string | null; reference_label: string | null; note: string | null; performed_by_name: string | null; performed_at: string }
const CATS: Record<string, string> = { spare_part: "Spare Part", consumable: "Consumable", tool: "Tool", other: "Lainnya" };
const TX_TYPES: Record<string, string> = { in: "Stock In", out: "Stock Out", adjustment: "Adjustment", usage: "Parts Usage", transfer_in: "Transfer In", transfer_out: "Transfer Out" };

export default function InventoryPage() {
  const { propertyId, can } = useAuth();
  const [q, setQ] = useState("");
  const [lowOnly, setLowOnly] = useState(false);
  const [edit, setEdit] = useState<Item | "new" | null>(null);
  const [txFor, setTxFor] = useState<{ item: Item; type: "in" | "out" | "adjustment" } | null>(null);
  const items = useList<Item>("inventory/items", { q: q || undefined, property_id: propertyId ?? undefined, low_stock: lowOnly || undefined });
  const rows = items.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<Item, unknown>[]>(() => [
    { id: "code", header: "Kode", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.item_code}</span>, size: 120 },
    { id: "name", header: "Item", cell: ({ row }) => <div><div className="font-medium">{row.original.name}</div><div className="text-xs text-muted-foreground">{CATS[row.original.category] ?? row.original.category}{row.original.equipment_category_code ? ` · ${row.original.equipment_category_code}` : ""}{row.original.barcode ? ` · ${row.original.barcode}` : ""}</div></div> },
    { id: "stock", header: "Stok", cell: ({ row }) => <div><span className={`tnum font-semibold ${row.original.low_stock ? "text-error" : ""}`}>{row.original.total_quantity}</span> <span className="text-xs text-muted-foreground">{row.original.unit} · min {row.original.min_stock}</span>{row.original.low_stock && <Badge tone="error" className="ml-1">low stock</Badge>}</div>, size: 200 },
    { id: "levels", header: "Per lokasi", cell: ({ row }) => <span className="text-xs text-muted-foreground">{row.original.levels.map((l) => `${l.stock_location_name}: ${l.quantity}`).join(" · ") || "—"}</span> },
    { id: "cost", header: "Harga satuan", cell: ({ row }) => <span className="tnum text-sm">{rp(row.original.unit_cost)}</span>, size: 130 },
  ], []);
  return (
    <div>
      <PageHeader title="Inventory" subtitle="Item master, stok per lokasi, mutasi stok, dan parts usage terhadap Work Order." actions={can("inventory.items.create") && <Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Tambah Item</Button>} />
      <Tabs defaultValue="items">
        <TabsList><TabsTrigger value="items">Items & Stock</TabsTrigger><TabsTrigger value="movements">Stock Movements</TabsTrigger><TabsTrigger value="locations">Stock Locations</TabsTrigger></TabsList>
        <TabsContent value="items" className="pt-4">
          <div className="mb-3 flex items-center gap-2">
            <Input className="w-64" placeholder="Cari item / kode / barcode…" value={q} onChange={(e) => setQ(e.target.value)} />
            <Checkbox label="Hanya low stock" checked={lowOnly} onCheckedChange={setLowOnly} />
          </div>
          <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} loading={items.isLoading} isFiltered={!!q || lowOnly} empty={{ message: "Belum ada item." }} hasMore={items.hasNextPage} onLoadMore={() => items.fetchNextPage()} loadingMore={items.isFetchingNextPage}
            rowActions={(r) => [
              ...(can("inventory.transactions.create") ? [{ label: "Stock In", icon: "add_box", onSelect: () => setTxFor({ item: r, type: "in" }) }, { label: "Stock Out", icon: "indeterminate_check_box", onSelect: () => setTxFor({ item: r, type: "out" }) }] : []),
              ...(can("inventory.stock.adjust") ? [{ label: "Adjustment (opname)", icon: "tune", onSelect: () => setTxFor({ item: r, type: "adjustment" }) }] : []),
              ...(can("inventory.items.update") ? [{ label: "Edit item", icon: "edit", onSelect: () => setEdit(r) }] : []),
            ]} />
        </TabsContent>
        <TabsContent value="movements" className="pt-4"><MovementsTab /></TabsContent>
        <TabsContent value="locations" className="pt-4"><LocationsTab /></TabsContent>
      </Tabs>
      {edit && <ItemDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
      {txFor && <TxDialog item={txFor.item} type={txFor.type} onClose={() => setTxFor(null)} />}
    </div>
  );
}

function MovementsTab() {
  const { propertyId } = useAuth();
  const [type, setType] = useState("");
  const list = useList<Movement>("inventory/stock-transactions", { property_id: propertyId ?? undefined, type: type || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<Movement, unknown>[]>(() => [
    { id: "number", header: "No.", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.transaction_number}</span>, size: 150 },
    { id: "item", header: "Item", cell: ({ row }) => <div><div className="font-medium">{row.original.item_name}</div><div className="text-xs text-muted-foreground">{row.original.item_code} · {row.original.stock_location_name}</div></div> },
    { id: "type", header: "Jenis", cell: ({ row }) => <Badge tone={row.original.quantity >= 0 ? "success" : "warning"}>{TX_TYPES[row.original.transaction_type] ?? row.original.transaction_type}</Badge>, size: 130 },
    { id: "qty", header: "Qty", cell: ({ row }) => <span className={`tnum font-semibold ${row.original.quantity < 0 ? "text-warning" : ""}`}>{row.original.quantity > 0 ? "+" : ""}{row.original.quantity} {row.original.unit}</span>, size: 110 },
    { id: "balance", header: "Saldo", cell: ({ row }) => <span className="tnum">{row.original.balance_after}</span>, size: 80 },
    { id: "ref", header: "Referensi", cell: ({ row }) => <span className="text-xs">{row.original.reference_label ?? row.original.reference_type ?? "—"}{row.original.note ? ` · ${row.original.note}` : ""}</span> },
    { id: "performed_at", header: "Waktu", cell: ({ row }) => <span className="text-xs text-muted-foreground"><RelativeTime value={row.original.performed_at} /> · {row.original.performed_by_name ?? "system"}</span>, size: 170 },
  ], []);
  return (
    <div>
      <div className="mb-3"><NativeSelect className="w-48" value={type} onChange={(e) => setType(e.target.value)}><option value="">Jenis: semua</option>{Object.entries(TX_TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</NativeSelect></div>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} loading={list.isLoading} isFiltered={!!type} empty={{ message: "Belum ada mutasi stok." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} />
    </div>
  );
}

function LocationsTab() {
  const { propertyId, can } = useAuth();
  const toast = useToast();
  const list = useAll<StockLocation>("inventory/stock-locations", { property_id: propertyId ?? undefined });
  const [name, setName] = useState("");
  const create = useAction<Record<string, unknown>, StockLocation>(() => "inventory/stock-locations", { invalidate: ["all"] });
  return (
    <div className="space-y-3">
      {!propertyId && <Alert variant="info">Pilih property untuk menambah stock location.</Alert>}
      <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
        {(list.data ?? []).map((l) => <li key={l.id} className="flex items-center justify-between px-3 py-2"><span><span className="font-medium">{l.name}</span> {l.is_default && <Badge tone="primary" className="ml-1">default</Badge>} {!l.is_active && <Badge tone="neutral" className="ml-1">nonaktif</Badge>}</span><span className="text-xs text-muted-foreground">{l.item_count} item berstok</span></li>)}
        {(list.data ?? []).length === 0 && <li className="px-3 py-2 text-muted-foreground">Belum ada stock location — parts usage membutuhkan minimal satu gudang per property.</li>}
      </ul>
      {can("inventory.stock_locations.create") && propertyId && (
        <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); if (!name.trim()) return; create.mutateAsync({ property_id: propertyId, name: name.trim(), is_default: (list.data ?? []).length === 0 }).then(() => { setName(""); toast.success("Stock location ditambahkan"); }).catch(toast.error); }}>
          <Input className="w-72" placeholder="Nama gudang, mis. Gudang Teknik B1" value={name} onChange={(e) => setName(e.target.value)} />
          <Button type="submit" variant="secondary" loading={create.isPending} disabled={!name.trim()}>Tambah</Button>
        </form>
      )}
    </div>
  );
}

function ItemDialog({ item, onClose }: { item: Item | null; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [f, setF] = useState({ name: item?.name ?? "", description: item?.description ?? "", category: item?.category ?? "spare_part", equipment_category_code: item?.equipment_category_code ?? "", unit: item?.unit ?? "pcs", min_stock: String(item?.min_stock ?? 0), unit_cost: String(item?.unit_cost ?? 0), barcode: item?.barcode ?? "", is_active: item?.is_active ?? true });
  const create = useAction<Record<string, unknown>, Item>(() => "inventory/items", { invalidate: ["list", "one"] });
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("inventory/items");
  const submit = async () => {
    const body = { name: f.name.trim(), description: f.description || null, category: f.category, equipment_category_code: f.equipment_category_code || null, unit: f.unit || "pcs", min_stock: Number(f.min_stock) || 0, unit_cost: Number(f.unit_cost) || 0, barcode: f.barcode || null };
    try {
      if (item) await update.mutateAsync({ id: item.id, version: item.version, ...body, is_active: f.is_active });
      else await create.mutateAsync(body);
      toast.success("Item disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit ${item.name}` : "Tambah Item"}>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Nama" required className="col-span-2"><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Kategori"><NativeSelect value={f.category} onChange={(e) => setF({ ...f, category: e.target.value })}>{Object.entries(CATS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</NativeSelect></Field>
          <Field label="Kategori equipment (NC §13)"><NativeSelect value={f.equipment_category_code} onChange={(e) => setF({ ...f, equipment_category_code: e.target.value })}><option value="">—</option>{["HVAC", "LIFT", "GEN", "PUMP", "ELEC", "FIRE", "PLMB", "SECU", "FAC", "OTH"].map((c) => <option key={c} value={c}>{c}</option>)}</NativeSelect></Field>
          <Field label="Satuan"><Input value={f.unit} onChange={(e) => setF({ ...f, unit: e.target.value })} /></Field>
          <Field label="Minimum stok"><Input type="number" value={f.min_stock} onChange={(e) => setF({ ...f, min_stock: e.target.value })} /></Field>
          <Field label="Harga satuan (Rp)"><Input type="number" value={f.unit_cost} onChange={(e) => setF({ ...f, unit_cost: e.target.value })} /></Field>
          <Field label="Barcode"><Input value={f.barcode} onChange={(e) => setF({ ...f, barcode: e.target.value })} /></Field>
          <Field label="Deskripsi" className="col-span-2"><Textarea rows={2} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
          {item && <Checkbox label="Aktif" checked={f.is_active} onCheckedChange={(v) => setF({ ...f, is_active: v })} />}
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={create.isPending || update.isPending} disabled={!f.name.trim()} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TxDialog({ item, type, onClose }: { item: Item; type: "in" | "out" | "adjustment"; onClose: () => void }) {
  const { propertyId } = useAuth();
  const toast = useToast();
  const locs = useAll<StockLocation>("inventory/stock-locations", { property_id: propertyId ?? undefined });
  const [f, setF] = useState({ stock_location_id: "", quantity: "", unit_cost: String(item.unit_cost), note: "" });
  const create = useAction<Record<string, unknown>, unknown>(() => "inventory/stock-transactions", { invalidate: ["list", "one", "all"] });
  const loc = locs.data?.find((l) => l.id === (f.stock_location_id || locs.data?.find((x) => x.is_default)?.id));
  const current = item.levels.find((l) => l.stock_location_id === loc?.id)?.quantity ?? 0;
  const submit = () => create.mutateAsync({ item_id: item.id, stock_location_id: f.stock_location_id || loc?.id, transaction_type: type, quantity: Number(f.quantity), unit_cost: type === "in" ? Number(f.unit_cost) : undefined, note: f.note || null })
    .then(() => { toast.success(`${TX_TYPES[type]} tersimpan`); onClose(); }).catch(toast.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`${TX_TYPES[type]} · ${item.name}`} description={type === "adjustment" ? "Masukkan jumlah stok hasil opname (nilai absolut). Selisih dicatat sebagai adjustment." : undefined}>
        <div className="space-y-3">
          <Field label="Stock location" required><NativeSelect value={f.stock_location_id} onChange={(e) => setF({ ...f, stock_location_id: e.target.value })}><option value="">{loc ? `${loc.name} (default)` : "— pilih —"}</option>{(locs.data ?? []).map((l) => <option key={l.id} value={l.id}>{l.name}</option>)}</NativeSelect></Field>
          <p className="text-xs text-muted-foreground">Stok saat ini di lokasi ini: <span className="tnum font-semibold">{current} {item.unit}</span></p>
          <Field label={type === "adjustment" ? "Stok baru" : "Jumlah"} required><Input type="number" min={0} value={f.quantity} onChange={(e) => setF({ ...f, quantity: e.target.value })} /></Field>
          {type === "in" && <Field label="Harga satuan (Rp)"><Input type="number" value={f.unit_cost} onChange={(e) => setF({ ...f, unit_cost: e.target.value })} /></Field>}
          <Field label="Catatan / referensi"><Input value={f.note} onChange={(e) => setF({ ...f, note: e.target.value })} placeholder={type === "in" ? "No. PO / surat jalan" : type === "adjustment" ? "Stock opname" : "Keperluan"} /></Field>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={create.isPending} disabled={f.quantity === "" || (!f.stock_location_id && !loc)} onClick={submit}>Simpan</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
