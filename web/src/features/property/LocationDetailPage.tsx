// Detail lokasi (PRD §8.3; PRD P0 v2 §7): path, status, detail per level, metadata, anak langsung, lantai (building),
// open work items di subtree, aset di lokasi, denah (PRD P1 v2 §7), QR (unit/area/space); aktifkan/nonaktifkan dan hapus (non-property).
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
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
import { LocationDialog, LocationStatusBadge, typeLabel } from "./LocationsPage";
import { DeleteLocationDialog, LocationStatusDialog } from "./LocationActions";
import { TYPED_PATH } from "./location-meta";
import { CreateWorkItemDialog } from "@/features/operations/dialogs";
import { LocationFloorPlans } from "./FloorPlans";

export default function LocationDetailPage() {
  const { id } = useParams();
  const { t } = useTranslation();
  const { can } = useAuth();
  const nav = useNavigate();
  const loc = useOne<Location>("locations", id);
  const isBuilding = loc.data?.location_type === "building";
  const floors = useAll<Location>(`buildings/${id}/floors`, {}, { enabled: !!id && isBuilding });
  const [status, setStatus] = useState<"activate" | "deactivate" | null>(null);
  const [del, setDel] = useState(false);
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
            badges={<><span className="rounded-full bg-neutral-soft px-2 py-0.5 text-xs text-neutral-text">{typeLabel[l.location_type]}</span><LocationStatusBadge loc={l} /></>}
            actions={
              <>
                {can("operations.work_orders.create") && <Button variant="secondary" size="sm" onClick={() => setWo(true)}>{t("action.create_work_order")}</Button>}
                {(can("property.locations.update") || (l.location_type === "property" && can("property.properties.update"))) && (
                  l.is_active ? <Button variant="secondary" size="sm" onClick={() => setStatus("deactivate")}>{t("loc.deactivate")}</Button> : <Button variant="success" size="sm" onClick={() => setStatus("activate")}>{t("loc.activate")}</Button>
                )}
                {l.location_type !== "property" && can("property.locations.delete") && <Button variant="ghost" size="sm" icon="delete" onClick={() => setDel(true)}>{t("loc.delete")}</Button>}
                {can("property.locations.update") && <Button size="sm" onClick={() => setEdit(true)}>Edit</Button>}
              </>
            }
          />
          <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
            <div className="min-w-0 lg:col-span-8">
              <Tabs defaultValue="children">
                <TabsList>
                  <TabsTrigger value="children">Sub-lokasi ({children.data?.length ?? 0})</TabsTrigger>
                  {isBuilding && <TabsTrigger value="floors">Lantai ({floors.data?.length ?? 0})</TabsTrigger>}
                  <TabsTrigger value="work">Work item open ({(wos.data?.length ?? 0) + (tasks.data?.length ?? 0)})</TabsTrigger>
                  <TabsTrigger value="assets">{t("label.asset")} ({assets.data?.length ?? 0})</TabsTrigger>
                  {can("property.floor_plans.view") && l.location_type !== "property" && <TabsTrigger value="floor_plan">Denah</TabsTrigger>}
                </TabsList>
                <TabsContent value="children" className="pt-4">
                  <ul className="divide-y divide-border rounded-lg border border-border">
                    {(children.data ?? []).map((c) => (
                      <li key={c.id} className="flex items-center justify-between px-4 py-2.5"><Link to={`/property/locations/${c.id}`} className="hover:underline"><span className="mr-2 text-[10px] uppercase text-muted-foreground">{c.location_type}</span><span className="font-mono text-[13px] font-semibold">{c.code}</span> {c.name}</Link><span className="text-xs text-muted-foreground">{c.child_count} anak{c.is_active ? "" : " · nonaktif"}</span></li>
                    ))}
                    {(children.data ?? []).length === 0 && <li className="px-4 py-6 text-center text-sm text-muted-foreground">Tidak ada sub-lokasi.</li>}
                  </ul>
                </TabsContent>
                <TabsContent value="floors" className="pt-4">
                  <ul className="divide-y divide-border rounded-lg border border-border">
                    {(floors.data ?? []).map((c) => (
                      <li key={c.id} className="flex items-center justify-between px-4 py-2.5"><Link to={`/property/locations/${c.id}`} className="hover:underline"><span className="font-mono text-[13px] font-semibold">{c.code}</span> {c.name}</Link><span className="text-xs text-muted-foreground">{c.path_text}</span></li>
                    ))}
                    {(floors.data ?? []).length === 0 && <li className="px-4 py-6 text-center text-sm text-muted-foreground">Belum ada lantai.</li>}
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
                {can("property.floor_plans.view") && l.location_type !== "property" && <TabsContent value="floor_plan" className="pt-4"><LocationFloorPlans location={l} /></TabsContent>}
              </Tabs>
            </div>
            <div className="min-w-0 space-y-4 lg:col-span-4">
              <Card><CardHeader><CardTitle>Detail</CardTitle></CardHeader><CardContent>
                <KeyValue items={[{ label: "Tipe", value: typeLabel[l.location_type] }, { label: "Path", value: l.path_text }, { label: "Kedalaman", value: l.depth }, ...Object.entries(l.details ?? {}).filter(([, v]) => v !== null && v !== "" && typeof v !== "object").map(([k, v]) => ({ label: k, value: String(v) }))]} />
              </CardContent></Card>
              {Object.keys(l.metadata ?? {}).length > 0 && (
                <Card><CardHeader><CardTitle>Metadata</CardTitle></CardHeader><CardContent>
                  <KeyValue items={Object.entries(l.metadata ?? {}).map(([k, v]) => ({ label: k, value: typeof v === "string" ? v : JSON.stringify(v) }))} />
                </CardContent></Card>
              )}
              {l.qr_code && (
                <Card><CardHeader><CardTitle>QR Lokasi</CardTitle></CardHeader><CardContent>
                  <div className="flex justify-center rounded bg-white p-2"><QRCodeSVG value={l.qr_code} size={140} /></div>
                  <div className="mt-2 rounded-md bg-muted p-2 text-center font-mono text-xs break-all">{l.qr_code}</div>
                </CardContent></Card>
              )}
            </div>
          </div>
          {edit && <LocationDialog locationType={l.location_type} item={l} onClose={() => setEdit(false)} />}
          {status && <LocationStatusDialog loc={l} action={status} onClose={() => setStatus(null)} />}
          {del && <DeleteLocationDialog loc={l} onClose={() => setDel(false)} onDone={() => nav(`/property/${TYPED_PATH[l.location_type]}`)} />}
          <CreateWorkItemDialog objectType="work_order" open={wo} onOpenChange={setWo} defaults={{ location_id: l.id, type: "corrective" }} />
        </div>
      )}
    </AsyncState>
  );
}
