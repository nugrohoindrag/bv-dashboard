// Panel detail Cleaning Task (PRD P2 v2.1 §7.3 P2-RTE-02/03, §7.5 P2-CNS-02): posisi stop dalam Cleaning Route run dan
// pemakaian consumable (mengurangi stok gudang property; 409 INSUFFICIENT_STOCK bila stok kurang).
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, ConfirmDialog, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { StatusBadge } from "@/components/bv/badges";
import { KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { CardSkeleton, QueryErrorState } from "@/components/bv/states";
import { useInvalidate } from "@/api/hooks";
import { api, type ListResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtMoney, fmtNumber } from "@/lib/format";
import { fieldErrorsOf, problemOf } from "@/lib/problem";
import type { ConsumableUsage, WorkItem } from "@/api/types";
import type { Item } from "@/features/inventory/InventoryPage";
import { consumableUsageError } from "./routes";

/** Posisi stop pada Cleaning Route run (extension cleaning task). Tidak dirender bila task bukan bagian route. */
export function RoutePositionCard({ task }: { task: WorkItem }) {
  const ext = (task.extension ?? {}) as { route_code?: string; route_name?: string; route_run_id?: string; route_stop_order?: number; route_total_stops?: number; route_completed_stops?: number; route_run_status?: string };
  if (!ext.route_code || !ext.route_run_id) return null;
  const total = ext.route_total_stops ?? 0;
  const done = ext.route_completed_stops ?? 0;
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Cleaning Route</CardTitle>
        {ext.route_run_status && <StatusBadge objectType="cleaning_route_run" status={ext.route_run_status} />}
      </CardHeader>
      <CardContent className="space-y-2">
        <Link to={`/housekeeping/routes?run=${ext.route_run_id}`} className="block font-medium text-primary hover:underline"><span className="font-mono text-[13px]">{ext.route_code}</span> {ext.route_name}</Link>
        <KeyValue items={[{ label: "Stop", value: <span className="tnum">{ext.route_stop_order ?? "—"} dari {total || "—"}</span> }, { label: "Progres route", value: <span className="tnum">{done}/{total} area selesai</span> }]} />
        <div className="h-2 w-full overflow-hidden rounded-full bg-surface-container-high" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label="Progres route">
          <div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
        </div>
      </CardContent>
    </Card>
  );
}

const TERMINAL = ["completed", "closed", "cancelled"];

export function ConsumablesCard({ task }: { task: WorkItem }) {
  const { can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const canView = can("inventory.consumable_usage.view") || can("housekeeping.cleaning.view");
  const canAdd = can("inventory.consumable_usage.create") && !TERMINAL.includes(task.status);
  const q = useQuery({
    queryKey: ["consumables", task.id],
    enabled: canView,
    queryFn: ({ signal }) => api<{ data: ConsumableUsage[]; total_cost: number }>(`tasks/${task.id}/consumables`, { signal }),
  });
  const [open, setOpen] = useState(false);
  const [del, setDel] = useState<ConsumableUsage | null>(null);
  const [busy, setBusy] = useState(false);
  if (!canView) return null;
  const remove = async (u: ConsumableUsage) => {
    setBusy(true);
    try {
      await api(`tasks/${task.id}/consumables/${u.id}`, { method: "DELETE" });
      invalidate("consumables", "inventory");
      toast.success(`Pemakaian ${u.item_name} dibatalkan, stok dikembalikan`);
      setDel(null);
    } catch (err) {
      toast.failed("deleted", err, "Pemakaian consumable");
    } finally {
      setBusy(false);
    }
  };
  const items = q.data?.data ?? [];
  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1 basis-48">
          <CardTitle>Consumable {q.data ? `(${items.length})` : ""}</CardTitle>
          <CardSubtitle>Pemakaian sabun, tisu, cairan pembersih — mengurangi stok gudang property.</CardSubtitle>
        </div>
        {canAdd && <Button size="sm" variant="secondary" icon="add" onClick={() => setOpen(true)}>Catat pemakaian</Button>}
      </CardHeader>
      <CardContent>
        {q.isLoading ? (
          <CardSkeleton lines={2} />
        ) : q.error && !q.data ? (
          <QueryErrorState error={q.error} onRetry={() => q.refetch()} compact />
        ) : (
          <ul className="divide-y divide-border text-sm">
            {items.map((u) => (
              <li key={u.id} className="flex flex-wrap items-center justify-between gap-2 py-1.5">
                <span className="min-w-0"><span className="font-medium">{u.item_name}</span> <span className="text-xs text-on-surface-variant">{u.item_code} · {u.stock_location_name}{u.note ? ` · ${u.note}` : ""} · {u.recorded_by_name ?? "—"}, <RelativeTime value={u.recorded_at} /></span></span>
                <span className="flex items-center gap-3">
                  <span className="tnum">{fmtNumber(u.quantity)} {u.unit}</span>
                  {u.total_cost !== null && <span className="tnum text-on-surface-variant">{fmtMoney(u.total_cost)}</span>}
                  {canAdd && <Button size="sm" variant="ghost" onClick={() => setDel(u)}>Koreksi</Button>}
                </span>
              </li>
            ))}
            {items.length === 0 && <li className="py-2 text-on-surface-variant">Belum ada pemakaian consumable.</li>}
            {q.data && q.data.total_cost > 0 && <li className="flex justify-between py-1.5 font-semibold"><span>Total biaya consumable</span><span className="tnum">{fmtMoney(q.data.total_cost)}</span></li>}
          </ul>
        )}
      </CardContent>
      {open && <AddConsumableDialog task={task} onClose={() => setOpen(false)} />}
      <ConfirmDialog open={!!del} onOpenChange={(o) => !o && setDel(null)} title={`Batalkan pemakaian ${del?.item_name ?? ""}?`} description="Stok dikembalikan ke gudang; koreksi tercatat di mutasi stok." confirmLabel="Batalkan pemakaian" destructive loading={busy} onConfirm={() => del && void remove(del)} />
    </Card>
  );
}

function AddConsumableDialog({ task, onClose }: { task: WorkItem; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [q, setQ] = useState("");
  const items = useQuery({
    queryKey: ["all", "inventory/items", "consumable", task.property_id, q],
    queryFn: ({ signal }) => api<ListResponse<Item>>("inventory/items", { query: { category: "consumable", property_id: task.property_id, q: q || undefined, limit: 200 }, signal }).then((r) => r.data.filter((i) => i.is_active)),
  });
  const [f, setF] = useState({ item_id: "", quantity: "1", note: "" });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const item = useMemo(() => items.data?.find((x) => x.id === f.item_id), [items.data, f.item_id]);
  const available = item ? item.levels.filter((l) => l.property_id === task.property_id).reduce((n, l) => n + l.quantity, 0) : null;
  const submit = async () => {
    const qty = Number(f.quantity);
    const e: Record<string, string> = {};
    if (!f.item_id) e.item_id = "Pilih item consumable";
    if (!(qty > 0)) e.quantity = "Jumlah harus lebih dari 0";
    setErrors(e);
    if (Object.keys(e).length) return;
    setBusy(true);
    try {
      await api(`tasks/${task.id}/consumables`, { body: { item_id: f.item_id, quantity: qty, note: f.note.trim() || null } });
      invalidate("consumables", "inventory");
      toast.success(`Pemakaian ${item?.name ?? "consumable"} tercatat`);
      onClose();
    } catch (err) {
      const fe = fieldErrorsOf(err);
      const msg = consumableUsageError(problemOf(err).code);
      if (msg) fe.quantity = msg;
      setErrors(fe);
      toast.failed("saved", err, "Pemakaian consumable");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Catat pemakaian consumable" description={`${task.number} · stok berkurang dari gudang default property.`}>
        <div className="space-y-3">
          <Field label="Cari item"><Input placeholder="nama / kode" value={q} onChange={(e) => setQ(e.target.value)} /></Field>
          <Field label="Item consumable" required error={errors.item_id}>
            <NativeSelect value={f.item_id} onChange={(e) => setF({ ...f, item_id: e.target.value })}>
              <option value="">{items.isLoading ? "Memuat…" : "— pilih —"}</option>
              {(items.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.name} ({x.item_code}) · stok {fmtNumber(x.total_quantity)} {x.unit}</option>)}
            </NativeSelect>
          </Field>
          {item && <p className="text-xs text-on-surface-variant">Tersedia di property ini: <span className="font-semibold tnum">{fmtNumber(available ?? 0)} {item.unit}</span>{item.low_stock && <Badge tone="warning" className="ml-1">low stock</Badge>}</p>}
          <Field label="Jumlah" required error={errors.quantity}><Input type="number" min={0.01} step="0.01" value={f.quantity} onChange={(e) => setF({ ...f, quantity: e.target.value })} /></Field>
          <Field label="Catatan" error={errors.note}><Input value={f.note} onChange={(e) => setF({ ...f, note: e.target.value })} /></Field>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={busy} onClick={() => void submit()}>Simpan</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
