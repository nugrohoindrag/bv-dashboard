// Form Aset (FR-AST-001/002): equipment (kategori/tipe), lokasi, status, kritikalitas, manufacturer/model/serial, garansi.
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { LocationPicker } from "@/components/bv/pickers";
import { useAll, useCreate, useUpdate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import type { Asset, Equipment } from "@/api/types";

export function AssetDialog({ asset, onClose }: { asset: Asset | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId, properties } = useAuth();
  const toast = useToast();
  const equipment = useAll<Equipment>("equipment");
  const [pid, setPid] = useState(asset?.property_id ?? propertyId ?? properties[0]?.id ?? "");
  const [form, setForm] = useState({ name: asset?.name ?? "", equipment_id: asset?.equipment_id ?? "", location_id: asset?.location_id ?? null as string | null, status: asset?.status ?? "active", criticality: asset?.criticality ?? "", manufacturer: asset?.manufacturer ?? "", model: asset?.model ?? "", serial_number: asset?.serial_number ?? "", installed_at: asset?.installed_at?.slice(0, 10) ?? "", warranty_until: asset?.warranty_until?.slice(0, 10) ?? "", notes: asset?.notes ?? "", asset_code: "" });
  const set = (k: keyof typeof form, v: string | null) => setForm((s) => ({ ...s, [k]: v }));
  const create = useCreate<Record<string, unknown>, Asset>("assets");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("assets");
  const submit = async () => {
    if (!form.name.trim() || !form.equipment_id || !form.location_id) return toast.error(new Error("Nama, equipment, dan lokasi wajib diisi"));
    const body: Record<string, unknown> = { property_id: pid, name: form.name.trim(), equipment_id: form.equipment_id, location_id: form.location_id, status: form.status, criticality: form.criticality || null, manufacturer: form.manufacturer || null, model: form.model || null, serial_number: form.serial_number || null, installed_at: form.installed_at ? new Date(form.installed_at).toISOString() : null, warranty_until: form.warranty_until ? new Date(form.warranty_until).toISOString() : null, notes: form.notes || null };
    if (!asset && form.asset_code.trim()) body.asset_code = form.asset_code.trim();
    try {
      if (asset) await update.mutateAsync({ id: asset.id, version: asset.version, ...body });
      else await create.mutateAsync(body);
      toast.success("Aset disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={asset ? `Edit ${asset.asset_code}` : "Tambah Aset"}>
        <div className="space-y-4">
          {!asset && properties.length > 1 && <Field label="Property" required><NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); set("location_id", null); }}>{properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</NativeSelect></Field>}
          <Field label="Nama aset" required><Input value={form.name} onChange={(e) => set("name", e.target.value)} /></Field>
          <Field label="Equipment (kategori · tipe)" required>
            <NativeSelect value={form.equipment_id} onChange={(e) => set("equipment_id", e.target.value)}>
              <option value="">Pilih…</option>
              {(equipment.data ?? []).filter((e) => e.is_active).map((e) => <option key={e.id} value={e.id}>{e.category_name}{e.type_name ? ` · ${e.type_name}` : ""}</option>)}
            </NativeSelect>
          </Field>
          <Field label={t("label.location")} required><LocationPicker propertyId={pid} value={form.location_id} onChange={(id) => set("location_id", id)} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("label.status")}><NativeSelect value={form.status} onChange={(e) => set("status", e.target.value)}>{["active", "inactive", "under_maintenance", "decommissioned"].map((s) => <option key={s} value={s}>{s.replace("_", " ")}</option>)}</NativeSelect></Field>
            <Field label="Kritikalitas"><NativeSelect value={form.criticality} onChange={(e) => set("criticality", e.target.value)}><option value="">Default equipment</option>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
            <Field label="Manufacturer"><Input value={form.manufacturer} onChange={(e) => set("manufacturer", e.target.value)} /></Field>
            <Field label="Model"><Input value={form.model} onChange={(e) => set("model", e.target.value)} /></Field>
            <Field label="Serial number"><Input value={form.serial_number} onChange={(e) => set("serial_number", e.target.value)} /></Field>
            {!asset && <Field label="Kode aset (opsional)" help="Kosongkan untuk otomatis"><Input value={form.asset_code} onChange={(e) => set("asset_code", e.target.value)} placeholder="AST-…" /></Field>}
            <Field label="Tanggal instalasi"><Input type="date" value={form.installed_at} onChange={(e) => set("installed_at", e.target.value)} /></Field>
            <Field label="Garansi sampai"><Input type="date" value={form.warranty_until} onChange={(e) => set("warranty_until", e.target.value)} /></Field>
          </div>
          <Field label="Catatan"><Textarea rows={2} value={form.notes} onChange={(e) => set("notes", e.target.value)} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>
          <Button loading={create.isPending || update.isPending} onClick={submit}>{t("action.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
