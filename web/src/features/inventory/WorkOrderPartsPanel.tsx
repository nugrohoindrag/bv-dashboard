// Parts Usage & Vendor pada detail Work Order (PRD P1 v1.3 §24–§25): Asset → Work Order → Parts Usage → Inventory;
// Vendor Work Order (NC §37). Biaya part menambah biaya aktual WO (internal; tidak pernah ke tenant).
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { useAction, useAll } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useProfile } from "@/lib/profile";
import { rp } from "@/features/billing/InvoicesPage";
import type { Item, StockLocation } from "./InventoryPage";
import type { Vendor } from "@/features/vendor/VendorsPage";

interface PartUsage { id: string; item_id: string; item_code: string; item_name: string; unit: string; stock_location_name: string; quantity: number; unit_cost: number; total_cost: number; note: string | null; recorded_by_name: string | null; recorded_at: string }

export function WorkOrderPartsPanel({ woId, propertyId, terminal, editable }: { woId: string; propertyId: string; terminal: boolean; editable: boolean }) {
  const { can } = useAuth();
  const prof = useProfile();
  const toast = useToast();
  const parts = useQuery({ queryKey: ["wo-parts", woId], queryFn: () => api<{ data: PartUsage[]; total_cost: number }>(`work-orders/${woId}/parts`) });
  const [open, setOpen] = useState(false);
  if (!prof.has("inventory")) return null;
  const canAdd = can("inventory.parts_usage.create") && !terminal;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Parts Usage {parts.data ? `(${parts.data.data.length})` : ""}</CardTitle>
        <CardSubtitle>Pemakaian spare part mengurangi stok gudang dan menambah biaya aktual Work Order.</CardSubtitle>
        {canAdd && <Button size="sm" variant="secondary" onClick={() => setOpen(true)}>Catat pemakaian</Button>}
      </CardHeader>
      <CardContent>
        <ul className="divide-y divide-border text-sm">
          {(parts.data?.data ?? []).map((p) => (
            <li key={p.id} className="flex items-center justify-between gap-3 py-1.5">
              <span><span className="font-medium">{p.item_name}</span> <span className="text-xs text-muted-foreground">{p.item_code} · {p.stock_location_name}{p.note ? ` · ${p.note}` : ""}</span></span>
              <span className="flex items-center gap-3"><span className="tnum">{p.quantity} {p.unit}</span><span className="tnum text-muted-foreground">{rp(p.total_cost)}</span>{editable && !terminal && can("inventory.parts_usage.create") && <Button size="sm" variant="ghost" onClick={() => api(`work-orders/${woId}/parts/${p.id}`, { method: "DELETE" }).then(() => { toast.success("Pemakaian dibatalkan, stok dikembalikan"); parts.refetch(); }).catch(toast.error)}>Koreksi</Button>}</span>
            </li>
          ))}
          {(parts.data?.data ?? []).length === 0 && <li className="py-2 text-muted-foreground">Belum ada pemakaian part.</li>}
          {parts.data && parts.data.total_cost > 0 && <li className="flex justify-between py-1.5 font-semibold"><span>Total biaya part</span><span className="tnum">{rp(parts.data.total_cost)}</span></li>}
        </ul>
      </CardContent>
      {open && <AddPartDialog woId={woId} propertyId={propertyId} onClose={() => setOpen(false)} onAdded={() => parts.refetch()} />}
    </Card>
  );
}

function AddPartDialog({ woId, propertyId, onClose, onAdded }: { woId: string; propertyId: string; onClose: () => void; onAdded: () => void }) {
  const toast = useToast();
  const [q, setQ] = useState("");
  const items = useAll<Item>("inventory/items", { q: q || undefined, property_id: propertyId });
  const locs = useAll<StockLocation>("inventory/stock-locations", { property_id: propertyId });
  const [f, setF] = useState({ item_id: "", stock_location_id: "", quantity: "1", note: "" });
  const add = useAction<Record<string, unknown>, PartUsage>(() => `work-orders/${woId}/parts`, { invalidate: ["one", "list", "all"] });
  const item = items.data?.find((x) => x.id === f.item_id);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Catat Parts Usage" description="Stok berkurang dari gudang property. Stok tidak boleh negatif (INSUFFICIENT_STOCK).">
        <div className="space-y-3">
          <Field label="Cari item"><Input placeholder="nama / kode / barcode" value={q} onChange={(e) => setQ(e.target.value)} /></Field>
          <Field label="Item" required><NativeSelect value={f.item_id} onChange={(e) => setF({ ...f, item_id: e.target.value })}><option value="">— pilih —</option>{(items.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.name} ({x.item_code}) · stok {x.total_quantity} {x.unit}</option>)}</NativeSelect></Field>
          {item && <p className="text-xs text-muted-foreground">Harga satuan {rp(item.unit_cost)} · per lokasi: {item.levels.map((l) => `${l.stock_location_name} ${l.quantity}`).join(", ") || "—"}{item.low_stock && <Badge tone="error" className="ml-1">low stock</Badge>}</p>}
          <div className="grid grid-cols-2 gap-3">
            <Field label="Gudang"><NativeSelect value={f.stock_location_id} onChange={(e) => setF({ ...f, stock_location_id: e.target.value })}><option value="">default property</option>{(locs.data ?? []).map((l) => <option key={l.id} value={l.id}>{l.name}</option>)}</NativeSelect></Field>
            <Field label="Jumlah" required><Input type="number" min={0.01} step="0.01" value={f.quantity} onChange={(e) => setF({ ...f, quantity: e.target.value })} /></Field>
          </div>
          <Field label="Catatan"><Input value={f.note} onChange={(e) => setF({ ...f, note: e.target.value })} /></Field>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={add.isPending} disabled={!f.item_id || Number(f.quantity) <= 0} onClick={() => add.mutateAsync({ item_id: f.item_id, stock_location_id: f.stock_location_id || null, quantity: Number(f.quantity), note: f.note || null }).then(() => { toast.success("Pemakaian part tercatat"); onAdded(); onClose(); }).catch(toast.error)}>Simpan</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function WorkOrderVendorPanel({ woId, vendorId, vendorName, vendorNotes, terminal }: { woId: string; vendorId: string | null; vendorName: string | null; vendorNotes: string | null; terminal: boolean }) {
  const { can } = useAuth();
  const prof = useProfile();
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const vendors = useAll<Vendor>("vendors", { status: "active" }, { enabled: open });
  const [sel, setSel] = useState(vendorId ?? "");
  const [notes, setNotes] = useState(vendorNotes ?? "");
  const assign = useAction<Record<string, unknown>, unknown>(() => `work-orders/${woId}/vendor`, { invalidate: ["one", "list"] });
  if (!prof.has("vendor_management")) return null;
  return (
    <Card>
      <CardHeader><CardTitle>Vendor</CardTitle>{can("vendor.assignments.create") && !terminal && <Button size="sm" variant="link" onClick={() => setOpen(true)}>{vendorId ? "Ubah" : "Tugaskan vendor"}</Button>}</CardHeader>
      <CardContent className="text-sm">
        {vendorId ? <div><Link to={`/vendors/${vendorId}`} className="font-medium text-primary hover:underline">{vendorName}</Link>{vendorNotes && <div className="text-xs text-muted-foreground">{vendorNotes}</div>}</div> : <span className="text-muted-foreground">Dikerjakan internal (tanpa vendor).</span>}
      </CardContent>
      {open && (
        <Dialog open onOpenChange={(o) => !o && setOpen(false)}>
          <DialogContent title="Vendor Work Order">
            <div className="space-y-3">
              <Field label="Vendor"><NativeSelect value={sel} onChange={(e) => setSel(e.target.value)}><option value="">— lepas vendor (internal) —</option>{(vendors.data ?? []).map((v) => <option key={v.id} value={v.id}>{v.name} · {v.service_categories.join(", ")}</option>)}</NativeSelect></Field>
              <Field label="Catatan (SPK / kontrak)"><Input value={notes} onChange={(e) => setNotes(e.target.value)} /></Field>
            </div>
            <DialogFooter><Button variant="secondary" onClick={() => setOpen(false)}>Batal</Button><Button loading={assign.isPending} onClick={() => assign.mutateAsync({ vendor_id: sel || null, vendor_notes: notes || null }).then(() => { toast.success(sel ? "Vendor ditugaskan" : "Vendor dilepas"); setOpen(false); }).catch(toast.error)}>Simpan</Button></DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </Card>
  );
}
