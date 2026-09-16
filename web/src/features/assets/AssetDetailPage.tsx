// Detail Aset (PRD §13.3): info, QR (rotate), open WO, PM plan, riwayat (WO/Task/PM/activity), buat WO korektif.
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { QRCodeSVG } from "qrcode.react";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle, ConfirmDialog, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { AssetStatusBadge, PriorityBadge, StatusBadge, objectTypeLabel } from "@/components/bv/badges";
import { AsyncState, DetailSkeleton, KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { itemLink } from "@/components/bv/cards";
import { useAction, useAll, useOne } from "@/api/hooks";
import { api } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { useAuth } from "@/lib/auth";
import { fmtDate, fmtDateTime } from "@/lib/format";
import type { Asset, MaintenancePlan, WorkItem } from "@/api/types";
import { CreateWorkItemDialog } from "@/features/operations/dialogs";
import { AssetDialog } from "./AssetDialog";

interface HistoryItem { kind: string; occurred_at: string; object_type: string; object_id: string; label: string; title: string; status: string; actor_name: string; payload: Record<string, unknown> }

export default function AssetDetailPage() {
  const { id } = useParams();
  const { t } = useTranslation();
  const { can } = useAuth();
  const nav = useNavigate();
  const toast = useToast();
  const asset = useOne<Asset>("assets", id);
  const history = useQuery({ queryKey: ["asset-history", id], enabled: !!id, queryFn: () => api<{ data: HistoryItem[] }>(`assets/${id}/history`).then((r) => r.data) });
  const openWO = useAll<WorkItem>("work-orders", { asset_id: id, open: true }, { enabled: !!id });
  const plans = useAll<MaintenancePlan>("maintenance-plans", { asset_id: id }, { enabled: !!id });
  const rotate = useAction<{ id: string }, { qr_code: string; qr_url: string }>((i) => `assets/${i.id}/qr/rotate`, { body: () => ({}) });
  const [edit, setEdit] = useState(false);
  const [woOpen, setWoOpen] = useState(false);
  const [rotateOpen, setRotateOpen] = useState(false);
  return (
    <AsyncState query={asset} skeleton={<DetailSkeleton />}>
      {(a) => (
        <div>
          <PageHeader
            breadcrumb={<><Link to="/assets" className="hover:underline">{t("nav.assets")}</Link> / <span className="font-mono">{a.asset_code}</span></>}
            title={<span><span className="font-mono">{a.asset_code}</span> <span className="font-normal text-muted-foreground">·</span> {a.name}</span>}
            badges={<><AssetStatusBadge status={a.status} />{a.criticality && <PriorityBadge priority={a.criticality} />}</>}
            subtitle={<>{a.category_name}{a.type_name ? ` · ${a.type_name}` : ""} · {a.location_path}</>}
            actions={
              <>
                {can("engineering.assets.update") && <Button variant="secondary" size="sm" onClick={() => setEdit(true)}>Edit</Button>}
                {can("operations.work_orders.create") && <Button size="sm" onClick={() => setWoOpen(true)}>{t("action.create_work_order")}</Button>}
              </>
            }
          />
          <div className="grid grid-cols-12 gap-5">
            <div className="col-span-8">
              <Tabs defaultValue="history">
                <TabsList>
                  <TabsTrigger value="history">Riwayat</TabsTrigger>
                  <TabsTrigger value="wo">Work Order open ({openWO.data?.length ?? 0})</TabsTrigger>
                  <TabsTrigger value="pm">Maintenance Plan ({plans.data?.length ?? 0})</TabsTrigger>
                </TabsList>
                <TabsContent value="history" className="pt-4">
                  <AsyncState query={history}>
                    {(items) => items.length === 0 ? <p className="py-8 text-center text-sm text-muted-foreground">Belum ada riwayat.</p> : (
                      <ol className="relative ml-2 space-y-4 border-l border-border pl-5">
                        {items.map((h, i) => (
                          <li key={i} className="relative">
                            <span className="absolute -left-[26px] top-1.5 h-2.5 w-2.5 rounded-full border-2 border-card bg-brand-600" />
                            <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground"><span>{objectTypeLabel[h.object_type] ?? h.kind} · {h.actor_name || "System"}</span><span className="tnum">{fmtDateTime(h.occurred_at)}</span></div>
                            {h.kind === "activity" ? (
                              <div className="text-body">{h.label} {h.status && <span className="text-muted-foreground">{h.status} → {h.title}</span>}</div>
                            ) : (
                              <div className="flex items-center gap-2"><Link to={itemLink(h.object_type, h.object_id)} className="hover:underline"><span className="font-mono text-[13px] font-semibold">{h.label}</span> {h.title}</Link><StatusBadge objectType={(h.object_type === "maintenance_schedule" ? "maintenance_schedule" : h.object_type === "task" ? "task" : "work_order")} status={h.status} /></div>
                            )}
                          </li>
                        ))}
                      </ol>
                    )}
                  </AsyncState>
                </TabsContent>
                <TabsContent value="wo" className="pt-4">
                  <ul className="divide-y divide-border rounded-lg border border-border">
                    {(openWO.data ?? []).map((w) => (
                      <li key={w.id} className="flex items-center justify-between px-4 py-2.5"><Link to={itemLink("work_order", w.id)} className="hover:underline"><span className="font-mono text-[13px] font-semibold">{w.number}</span> {w.title}</Link><span className="flex items-center gap-2"><StatusBadge objectType="work_order" status={w.status} /><span className="text-xs text-muted-foreground">Due {fmtDateTime(w.due_at)}</span></span></li>
                    ))}
                    {(openWO.data ?? []).length === 0 && <li className="px-4 py-6 text-center text-sm text-muted-foreground">Tidak ada Work Order open.</li>}
                  </ul>
                </TabsContent>
                <TabsContent value="pm" className="pt-4">
                  <ul className="divide-y divide-border rounded-lg border border-border">
                    {(plans.data ?? []).map((p) => (
                      <li key={p.id} className="flex items-center justify-between px-4 py-2.5"><Link to="/engineering/preventive-maintenance" className="hover:underline"><span className="font-mono text-[13px] font-semibold">{p.plan_code}</span> {p.name}</Link><span className="flex items-center gap-2 text-xs text-muted-foreground"><StatusBadge objectType="authoring" status={p.status} />{p.frequency} · due {fmtDate(p.next_due)}</span></li>
                    ))}
                    {(plans.data ?? []).length === 0 && <li className="px-4 py-6 text-center text-sm text-muted-foreground">Belum ada Maintenance Plan.</li>}
                  </ul>
                </TabsContent>
              </Tabs>
            </div>
            <div className="col-span-4 space-y-4">
              <Card><CardHeader><CardTitle>Informasi</CardTitle></CardHeader><CardContent>
                <KeyValue items={[
                  { label: "Manufacturer", value: a.manufacturer },
                  { label: "Model", value: a.model },
                  { label: "Serial", value: a.serial_number },
                  { label: "Instalasi", value: fmtDate(a.installed_at) },
                  { label: "Garansi", value: fmtDate(a.warranty_until) },
                  { label: "PM berikutnya", value: fmtDate(a.next_pm_due) },
                  { label: "Maint. terakhir", value: a.last_maintenance_at ? <RelativeTime value={a.last_maintenance_at} /> : "—" },
                  { label: "Catatan", value: a.notes },
                ]} />
              </CardContent></Card>
              <Card><CardHeader><CardTitle><Icon name="qr_code_2" size={16} className="mr-1 inline" />QR Code</CardTitle>{can("engineering.assets.update") && <Button variant="ghost" size="sm" onClick={() => setRotateOpen(true)}><Icon name="refresh" size={16} /> Rotate</Button>}</CardHeader><CardContent>
                {a.qr_code ? (
                  <div className="space-y-2">
                    <div className="rounded-md bg-muted p-3 text-center font-mono text-xs break-all">{a.qr_code}</div>
                    <div className="flex justify-center rounded bg-white p-2"><QRCodeSVG value={a.qr_url ?? a.qr_code} size={160} /></div>
                    <Button variant="secondary" size="sm" className="w-full" onClick={() => window.print()}>{t("action.print")}</Button>
                  </div>
                ) : <p className="text-sm text-muted-foreground">QR belum dibuat.</p>}
              </CardContent></Card>
            </div>
          </div>
          {edit && <AssetDialog asset={a} onClose={() => setEdit(false)} />}
          <CreateWorkItemDialog objectType="work_order" open={woOpen} onOpenChange={setWoOpen} defaults={{ title: `${a.name} — `, location_id: a.location_id, asset_id: a.id, priority: a.criticality ?? "medium", type: "corrective" }} onCreated={(x) => nav(itemLink("work_order", x.id))} />
          <ConfirmDialog open={rotateOpen} onOpenChange={setRotateOpen} title="Rotate QR code?" description="QR lama tidak berlaku lagi. Cetak ulang label setelah rotate." confirmLabel="Rotate" loading={rotate.isPending} onConfirm={() => rotate.mutateAsync({ id: a.id }).then(() => { toast.success("QR diperbarui"); setRotateOpen(false); }).catch(toast.error)} />
        </div>
      )}
    </AsyncState>
  );
}
