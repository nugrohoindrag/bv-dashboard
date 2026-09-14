// Equipment (master kategori/tipe aset, PRD §13.1): tabel + form tambah/edit; aktif/nonaktif.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { PriorityBadge } from "@/components/bv/badges";
import { useToast } from "@/components/bv/common";
import { useAll, useCreate, useUpdate } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import type { Equipment } from "@/api/types";

type Eq = Equipment & { default_criticality: string | null; version: number };

export default function EquipmentPage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const [q, setQ] = useState("");
  const list = useAll<Eq>("equipment", { q: q || undefined });
  const [edit, setEdit] = useState<Eq | null | "new">(null);
  const columns = useMemo<ColumnDef<Eq, unknown>[]>(
    () => [
      { id: "code", header: "Kode kategori", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.category_code}</span>, size: 150 },
      { id: "cat", header: "Kategori", cell: ({ row }) => row.original.category_name },
      { id: "type", header: "Tipe", cell: ({ row }) => row.original.type_name ?? <span className="text-muted-foreground">—</span> },
      { id: "crit", header: "Kritikalitas default", cell: ({ row }) => row.original.default_criticality ? <PriorityBadge priority={row.original.default_criticality} /> : "—", size: 140 },
      { id: "count", header: "Aset", cell: ({ row }) => <span className="tnum">{row.original.asset_count}</span>, size: 70 },
      { id: "active", header: "Aktif", cell: ({ row }) => <span className={cn("text-xs", row.original.is_active ? "text-success-text" : "text-muted-foreground")}>{row.original.is_active ? "Aktif" : "Nonaktif"}</span>, size: 80 },
    ],
    [],
  );
  return (
    <div>
      <PageHeader title={t("nav.equipment")} subtitle="Master kategori & tipe peralatan untuk Asset Register." actions={can("engineering.equipment.create") && <Button onClick={() => setEdit("new")}><Plus /> Tambah Equipment</Button>}>
        <Input className="w-72" placeholder="Cari kategori / tipe…" value={q} onChange={(e) => setQ(e.target.value)} />
      </PageHeader>
      <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} onRowClick={(r) => { if (can("engineering.equipment.update")) setEdit(r); }} loading={list.isLoading} empty={{ message: "Belum ada equipment." }} />
      {edit && <EquipmentDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

function EquipmentDialog({ item, onClose }: { item: Eq | null; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [form, setForm] = useState({ category_code: item?.category_code ?? "", category_name: item?.category_name ?? "", type_name: item?.type_name ?? "", default_criticality: item?.default_criticality ?? "", is_active: item?.is_active ?? true });
  const create = useCreate<Record<string, unknown>>("equipment");
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("equipment");
  const submit = async () => {
    if (!form.category_code.trim() || !form.category_name.trim()) return toast.error(new Error("Kode dan nama kategori wajib"));
    const body = { category_code: form.category_code.trim().toUpperCase(), category_name: form.category_name.trim(), type_name: form.type_name || null, default_criticality: form.default_criticality || null, is_active: form.is_active };
    try {
      if (item) await update.mutateAsync({ id: item.id, version: item.version, ...body });
      else await create.mutateAsync(body);
      toast.success("Equipment disimpan");
      onClose();
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={item ? "Edit Equipment" : "Tambah Equipment"}>
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Kode kategori" required help="mis. HVAC, ELEC, PLMB"><Input value={form.category_code} onChange={(e) => setForm({ ...form, category_code: e.target.value })} disabled={!!item} /></Field>
            <Field label="Nama kategori" required><Input value={form.category_name} onChange={(e) => setForm({ ...form, category_name: e.target.value })} /></Field>
            <Field label="Tipe" help="mis. Chiller, Genset, Lift"><Input value={form.type_name} onChange={(e) => setForm({ ...form, type_name: e.target.value })} /></Field>
            <Field label="Kritikalitas default"><NativeSelect value={form.default_criticality} onChange={(e) => setForm({ ...form, default_criticality: e.target.value })}><option value="">—</option>{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{t(`priority.${p}`)}</option>)}</NativeSelect></Field>
          </div>
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={form.is_active} onCheckedChange={(v) => setForm({ ...form, is_active: !!v })} /> Aktif</label>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>
          <Button loading={create.isPending || update.isPending} onClick={submit}>{t("action.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
