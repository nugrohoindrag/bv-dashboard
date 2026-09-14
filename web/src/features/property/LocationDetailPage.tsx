// Detail lokasi (PRD §8.3): path, detail per level, anak langsung, open work items di subtree, aset di lokasi, QR (unit/area/space).
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { QRCodeSVG } from "qrcode.react";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { AssetStatusBadge, StatusBadge } from "@/components/bv/badges";
import { AsyncState, DetailSkeleton, KeyValue, LocationPath } from "@/components/bv/common";
import { itemLink } from "@/components/bv/cards";
import { useAll, useOne } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import type { Asset, Location, WorkItem } from "@/api/types";
import { LocationDialog, typeLabel } from "./LocationsPage";
import { CreateWorkItemDialog } from "@/features/operations/dialogs";

export default function LocationDetailPage() {
  const { id } = useParams();
  const { t } = useTranslation();
  const { can } = useAuth();
  const loc = useOne<Location>("locations", id);
  const children = useAll<Location>("locations", { parent_id: id, include_inactive: true }, { enabled: !!id });
  const wos = useAll<WorkItem>("work-orders", { location_id: id, open: true }, { enabled: !!id });
  const tasks = useAll<WorkItem>("tasks", { location_id: id, open: true }, { enabled: !!id });
  const assets = useAll<Asset>("assets", { location_id: id }, { enabled: !!id });
  const [edit, setEdit] = useState(false);
  const [wo, setWo] = useState(false);
  return (
    <AsyncState query={loc} skeleton={<DetailSkeleton />}>
      {(l) => (
        <div>
          <PageHeader
            breadcrumb={<LocationPath path={l.path.slice(0, -1)} pathText={l.path_text} linkTo={(lid) => `/property/locations/${lid}`} />}
            title={<span><span className="font-mono">{l.code}</span> <span className="font-normal text-muted-foreground">·</span> {l.name}</span>}
            badges={<span className="rounded-full bg-neutral-soft px-2 py-0.5 text-xs text-neutral-text">{typeLabel[l.location_type]}</span>}
            subtitle={l.is_active ? undefined : "Nonaktif"}
            actions={
              <>
                {can("operations.work_orders.create") && <Button variant="secondary" size="sm" onClick={() => setWo(true)}>{t("action.create_work_order")}</Button>}
                {can("property.locations.update") && <Button size="sm" onClick={() => setEdit(true)}>Edit</Button>}
              </>
            }
          />
          <div className="grid grid-cols-12 gap-5">
            <div className="col-span-8">
              <Tabs defaultValue="children">
                <TabsList>
                  <TabsTrigger value="children">Sub-lokasi ({children.data?.length ?? 0})</TabsTrigger>
                  <TabsTrigger value="work">Work item open ({(wos.data?.length ?? 0) + (tasks.data?.length ?? 0)})</TabsTrigger>
                  <TabsTrigger value="assets">{t("label.asset")} ({assets.data?.length ?? 0})</TabsTrigger>
                </TabsList>
                <TabsContent value="children" className="pt-4">
                  <ul className="divide-y divide-border rounded-lg border border-border">
                    {(children.data ?? []).map((c) => (
                      <li key={c.id} className="flex items-center justify-between px-4 py-2.5"><Link to={`/property/locations/${c.id}`} className="hover:underline"><span className="mr-2 text-[10px] uppercase text-muted-foreground">{c.location_type}</span><span className="font-mono text-[13px] font-semibold">{c.code}</span> {c.name}</Link><span className="text-xs text-muted-foreground">{c.child_count} anak{c.is_active ? "" : " · nonaktif"}</span></li>
                    ))}
                    {(children.data ?? []).length === 0 && <li className="px-4 py-6 text-center text-sm text-muted-foreground">Tidak ada sub-lokasi.</li>}
                  </ul>
                </TabsContent>
                <TabsContent value="work" className="pt-4">
                  <ul className="divide-y divide-border rounded-lg border border-border">
                    {[...(wos.data ?? []), ...(tasks.data ?? [])].map((w) => (
                      <li key={w.id} className="flex items-center justify-between px-4 py-2.5"><Link to={itemLink(w.object_type, w.id)} className="hover:underline"><span className="font-mono text-[13px] font-semibold">{w.number}</span> {w.title}</Link><span className="flex items-center gap-2"><StatusBadge objectType={w.object_type} status={w.status} /><span className="text-xs text-muted-foreground">Due {fmtDateTime(w.due_at)}</span></span></li>
                    ))}
                    {(wos.data?.length ?? 0) + (tasks.data?.length ?? 0) === 0 && <li className="px-4 py-6 text-center text-sm text-muted-foreground">Tidak ada work item open di lokasi ini.</li>}
                  </ul>
                </TabsContent>
                <TabsContent value="assets" className="pt-4">
                  <ul className="divide-y divide-border rounded-lg border border-border">
                    {(assets.data ?? []).map((a) => (
                      <li key={a.id} className="flex items-center justify-between px-4 py-2.5"><Link to={`/assets/${a.id}`} className="hover:underline"><span className="font-mono text-[13px] font-semibold">{a.asset_code}</span> {a.name}</Link><AssetStatusBadge status={a.status} /></li>
                    ))}
                    {(assets.data ?? []).length === 0 && <li className="px-4 py-6 text-center text-sm text-muted-foreground">Tidak ada aset di lokasi ini.</li>}
                  </ul>
                </TabsContent>
              </Tabs>
            </div>
            <div className="col-span-4 space-y-4">
              <Card><CardHeader><CardTitle>Detail</CardTitle></CardHeader><CardContent>
                <KeyValue items={[{ label: "Tipe", value: typeLabel[l.location_type] }, { label: "Path", value: l.path_text }, { label: "Kedalaman", value: l.depth }, ...Object.entries(l.details ?? {}).filter(([, v]) => v !== null && v !== "").map(([k, v]) => ({ label: k, value: String(v) }))]} />
              </CardContent></Card>
              {l.qr_code && (
                <Card><CardHeader><CardTitle>QR Lokasi</CardTitle></CardHeader><CardContent>
                  <div className="flex justify-center rounded bg-white p-2"><QRCodeSVG value={l.qr_code} size={140} /></div>
                  <div className="mt-2 rounded-md bg-muted p-2 text-center font-mono text-xs break-all">{l.qr_code}</div>
                </CardContent></Card>
              )}
            </div>
          </div>
          {edit && <LocationDialog locationType={l.location_type} item={l} onClose={() => setEdit(false)} />}
          <CreateWorkItemDialog objectType="work_order" open={wo} onOpenChange={setWo} defaults={{ location_id: l.id, type: "corrective" }} />
        </div>
      )}
    </AsyncState>
  );
}
