// Building Map / Floor Plan (PRD P1 v2 §7): unggah & lihat denah per building/tower/floor/area, tempatkan marker untuk
// lokasi (subtree), facility, atau aset; marker berwarna sesuai sinyal pekerjaan (ok · attention · critical) dan membuka
// daftar task/work order/incident/request terkait (deep_link). Posisi marker = persentase (x_pct/y_pct) dari gambar.
// Dipakai sebagai tab "Denah" di detail lokasi (LocationFloorPlans) dan halaman Property › Denah (/property/floor-plans).
import { useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Button, Card, CardContent, CardHeader, CardTitle, Checkbox, ConfirmDialog, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { AsyncState, useToast } from "@/components/bv/common";
import { EmptyState } from "@/components/bv/states";
import { StatusBadge, objectTypeLabel } from "@/components/bv/badges";
import { AssetPicker, LocationPicker } from "@/components/bv/pickers";
import { uploadAttachment, useAll, useInvalidate, useOne } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import type { Facility, FloorPlan, FloorPlanMarker, Location } from "@/api/types";
import type { ObjectType } from "@/lib/status-map";

/** Level yang boleh memiliki denah (server: building | tower | floor | area). */
export const PLAN_LOCATION_TYPES = ["building", "tower", "floor", "area"];
const SIGNAL_COLOR: Record<string, string> = { ok: "var(--color-success)", attention: "var(--color-warning)", critical: "var(--color-error)" };
const SIGNAL_LABEL: Record<string, string> = { ok: "Normal", attention: "Perlu perhatian", critical: "Kritis" };
const TARGET_LABEL: Record<string, string> = { location: "Lokasi", facility: "Facility", asset: "Aset" };
const openCount = (m: FloorPlanMarker) => m.work.open_tasks + m.work.open_work_orders + m.work.open_incidents + m.work.open_requests;

async function imageSize(file: File): Promise<{ w: number; h: number } | null> {
  const bmp = await createImageBitmap(file).catch(() => null);
  if (!bmp) return null;
  const out = { w: bmp.width, h: bmp.height };
  bmp.close?.();
  return out;
}
/** Unggah gambar denah (attachment object_type=floor_plan) lalu tautkan ke denah. */
async function uploadPlanImage(planId: string, file: File) {
  const size = await imageSize(file);
  const att = await uploadAttachment(file, "floor_plan", planId, "photo");
  await api(`floor-plans/${planId}`, { method: "PATCH", body: { attachment_id: att.id, image_width: size?.w, image_height: size?.h } });
}

// ---------- Halaman Property › Denah ----------
export default function FloorPlansPage() {
  const { t } = useTranslation();
  const { planId } = useParams();
  const nav = useNavigate();
  const { propertyId, can } = useAuth();
  const plans = useAll<FloorPlan>("floor-plans", { property_id: propertyId ?? undefined });
  const [createOpen, setCreateOpen] = useState(false);
  const selected = planId ?? plans.data?.[0]?.id;
  return (
    <div>
      <PageHeader title={t("nav.floor_plans")} subtitle="Denah building/tower/lantai/area dengan marker lokasi, facility, dan aset beserta pekerjaan terbuka." actions={can("property.floor_plans.create") && <Button icon="add" disabled={!propertyId} onClick={() => setCreateOpen(true)}>Tambah Denah</Button>} />
      {!propertyId && <Alert variant="info" className="mb-4">Pilih property di header untuk menambah denah.</Alert>}
      <AsyncState query={plans}>
        {(list) => list.length === 0 ? (
          <EmptyState icon="location_on" title="Belum ada denah." description="Unggah gambar denah untuk building, tower, lantai, atau area." action={can("property.floor_plans.create") && propertyId ? <Button icon="add" onClick={() => setCreateOpen(true)}>Tambah Denah</Button> : undefined} />
        ) : (
          <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
            <ul className="space-y-2 lg:col-span-3">
              {list.map((p) => (
                <li key={p.id}>
                  <button type="button" onClick={() => nav(`/property/floor-plans/${p.id}`)} className={cn("flex w-full gap-3 rounded-[var(--radius-md)] border p-2 text-left transition-colors hover:bg-surface-container", selected === p.id ? "border-primary bg-primary-soft" : "border-border")}>
                    {p.image?.thumb_url || p.image?.url ? <img src={p.image.thumb_url ?? p.image.url} alt="" className="h-12 w-16 shrink-0 rounded object-cover" /> : <span className="flex h-12 w-16 shrink-0 items-center justify-center rounded bg-surface-container-high"><Icon name="location_on" size={18} /></span>}
                    <span className="min-w-0">
                      <span className="block truncate text-sm font-semibold">{p.name}</span>
                      <span className="block truncate text-xs text-muted-foreground">{p.location_path}</span>
                      <span className="text-xs text-muted-foreground">{p.marker_count} marker{p.is_active ? "" : " · nonaktif"}</span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
            <div className="min-w-0 lg:col-span-9">{selected && <FloorPlanViewer key={selected} planId={selected} onDeleted={() => nav("/property/floor-plans")} />}</div>
          </div>
        )}
      </AsyncState>
      {createOpen && propertyId && <FloorPlanDialog propertyId={propertyId} onClose={() => setCreateOpen(false)} onSaved={(p) => nav(`/property/floor-plans/${p.id}`)} />}
    </div>
  );
}

// ---------- Tab "Denah" pada detail lokasi ----------
export function LocationFloorPlans({ location }: { location: Location }) {
  const { can } = useAuth();
  // server: denah di lokasi ini, leluhurnya, atau turunannya (unit/space → denah lantai tempatnya)
  const plans = useAll<FloorPlan>("floor-plans", { location_id: location.id });
  const [picked, setPicked] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const canCreate = can("property.floor_plans.create") && PLAN_LOCATION_TYPES.includes(location.location_type);
  return (
    <AsyncState query={plans}>
      {(list) => {
        const selected = picked ?? list.find((p) => p.location_id === location.id)?.id ?? list[0]?.id;
        return (
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2">
              {list.length > 1 && (
                <NativeSelect className="w-full sm:w-80" value={selected ?? ""} onChange={(e) => setPicked(e.target.value)} aria-label="Pilih denah">
                  {list.map((p) => <option key={p.id} value={p.id}>{p.name} · {p.location_name}</option>)}
                </NativeSelect>
              )}
              {canCreate && <Button size="sm" variant="secondary" icon="add" onClick={() => setCreateOpen(true)}>Tambah Denah</Button>}
            </div>
            {list.length === 0 ? (
              <EmptyState compact title="Belum ada denah untuk lokasi ini." description={PLAN_LOCATION_TYPES.includes(location.location_type) ? "Unggah gambar denah lalu tempatkan marker lokasi, facility, atau aset." : "Denah dibuat di level building, tower, lantai, atau area; unit/space tampil sebagai marker di denah tersebut."} />
            ) : (
              selected && <FloorPlanViewer key={selected} planId={selected} highlightTargetId={location.id} onDeleted={() => setPicked(null)} />
            )}
            {createOpen && <FloorPlanDialog propertyId={location.property_id} locationId={location.id} onClose={() => setCreateOpen(false)} onSaved={(p) => setPicked(p.id)} />}
          </div>
        );
      }}
    </AsyncState>
  );
}

// ---------- Viewer / editor ----------
type Target = { type: "location" | "facility" | "asset"; id: string | null };

export function FloorPlanViewer({ planId, highlightTargetId, onDeleted }: { planId: string; highlightTargetId?: string; onDeleted?: () => void }) {
  const { can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const plan = useOne<FloorPlan>("floor-plans", planId);
  const boxRef = useRef<HTMLDivElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const [editing, setEditing] = useState(false);
  const [target, setTarget] = useState<Target>({ type: "location", id: null });
  const [active, setActive] = useState<string | null>(null);
  const [drag, setDrag] = useState<{ id: string; x: number; y: number; moved: boolean } | null>(null);
  const [busy, setBusy] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [delOpen, setDelOpen] = useState(false);
  const canUpdate = can("property.floor_plans.update");
  const facilities = useAll<Facility>("facilities", { property_id: plan.data?.property_id }, { enabled: editing && target.type === "facility" && !!plan.data });
  const refresh = () => invalidate("floor-plans");

  const pctAt = (clientX: number, clientY: number) => {
    const r = boxRef.current!.getBoundingClientRect();
    const clamp = (n: number) => Math.min(100, Math.max(0, Math.round(n * 100) / 100));
    return { x: clamp(((clientX - r.left) / r.width) * 100), y: clamp(((clientY - r.top) / r.height) * 100) };
  };
  const run = async (fn: () => Promise<unknown>, ok?: string) => {
    setBusy(true);
    try {
      await fn();
      refresh();
      if (ok) toast.success(ok);
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };
  const place = (e: React.MouseEvent) => {
    if (!editing) return setActive(null); // klik di luar marker menutup popover
    if (!target.id) return setActive(null);
    if (drag) return;
    const { x, y } = pctAt(e.clientX, e.clientY);
    const tid = target.id;
    run(() => api(`floor-plans/${planId}/markers`, { body: { target_type: target.type, target_id: tid, x_pct: x, y_pct: y } }), "Marker ditempatkan").then(() => setTarget((t) => ({ ...t, id: null })));
  };
  const onUpload = (file?: File) => file && run(() => uploadPlanImage(planId, file), "Gambar denah diunggah");

  return (
    <AsyncState query={plan}>
      {(p) => {
        const markers = p.markers ?? [];
        const url = p.image?.url ?? p.image?.thumb_url;
        const sel = markers.find((m) => m.id === active);
        return (
          <Card>
            <CardHeader>
              <div className="min-w-0">
                <CardTitle>{p.name}</CardTitle>
                <div className="truncate text-xs text-muted-foreground">{p.location_path} · {markers.length} marker{p.description ? ` · ${p.description}` : ""}</div>
              </div>
              {canUpdate && (
                <div className="flex flex-wrap gap-2">
                  <input ref={fileRef} type="file" accept="image/*" className="hidden" onChange={(e) => { onUpload(e.target.files?.[0]); e.target.value = ""; }} />
                  <Button size="sm" variant="secondary" icon="cloud_upload" loading={busy && !editing} onClick={() => fileRef.current?.click()}>{url ? "Ganti gambar" : "Unggah gambar"}</Button>
                  <Button size="sm" variant="secondary" icon="edit" onClick={() => setEditOpen(true)}>Ubah</Button>
                  {url && <Button size="sm" variant={editing ? "primary" : "secondary"} icon="location_on" onClick={() => { setEditing((v) => !v); setActive(null); }}>{editing ? "Selesai edit marker" : "Edit marker"}</Button>}
                  {can("property.floor_plans.delete") && <Button size="sm" variant="ghost" icon="delete" onClick={() => setDelOpen(true)}>Hapus</Button>}
                </div>
              )}
            </CardHeader>
            <CardContent className="space-y-3">
              {editing && (
                <div className="flex flex-wrap items-end gap-2 rounded-[var(--radius-md)] bg-surface-container-low p-2">
                  <Field label="Tipe target" className="w-36">
                    <NativeSelect value={target.type} onChange={(e) => setTarget({ type: e.target.value as Target["type"], id: null })}>{Object.entries(TARGET_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</NativeSelect>
                  </Field>
                  <Field label="Target" className="min-w-[240px] flex-1">
                    {target.type === "location" ? (
                      <LocationPicker propertyId={p.property_id} value={target.id} onChange={(id) => setTarget({ type: "location", id })} />
                    ) : target.type === "asset" ? (
                      <AssetPicker propertyId={p.property_id} locationId={p.location_id} value={target.id} onChange={(id) => setTarget({ type: "asset", id })} />
                    ) : (
                      <NativeSelect value={target.id ?? ""} onChange={(e) => setTarget({ type: "facility", id: e.target.value || null })}>
                        <option value="">Pilih facility…</option>
                        {(facilities.data ?? []).map((f) => <option key={f.id} value={f.id}>{f.facility_code} · {f.name}{f.location_path ? ` (${f.location_path})` : ""}</option>)}
                      </NativeSelect>
                    )}
                  </Field>
                  <p className="basis-full text-xs text-on-surface-variant">{target.id ? "Klik pada denah untuk menempatkan marker (target yang sudah ada dipindahkan)." : "Pilih target, lalu klik pada denah. Seret marker untuk memindahkan; klik marker untuk menghapus."}</p>
                </div>
              )}
              {!url ? (
                <EmptyState compact title="Gambar denah belum diunggah." description={canUpdate ? "Unggah gambar (JPG/PNG/WebP) untuk mulai menempatkan marker." : undefined} action={canUpdate ? <Button size="sm" icon="cloud_upload" loading={busy} onClick={() => fileRef.current?.click()}>Unggah gambar</Button> : undefined} />
              ) : (
                <div ref={boxRef} className={cn("relative w-full select-none rounded-[var(--radius-md)] border border-border bg-surface-container-low", editing && target.id && "cursor-crosshair")} onClick={place} data-testid="floor-plan-canvas">
                  <img src={url} alt={`Denah ${p.name}`} className="block w-full rounded-[var(--radius-md)]" draggable={false} />
                  {markers.map((m) => {
                    const pos = drag?.id === m.id ? drag : { x: m.x_pct, y: m.y_pct };
                    const n = openCount(m);
                    const hl = highlightTargetId === m.target_id;
                    return (
                      <button
                        key={m.id}
                        type="button"
                        title={`${m.target_label || m.target_name} · ${SIGNAL_LABEL[m.work.signal] ?? m.work.signal}`}
                        aria-label={`Marker ${m.target_name}`}
                        className={cn("absolute z-10 flex h-6 min-w-6 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full border-2 border-white px-1 text-[11px] font-bold text-white shadow-md", hl && "ring-4 ring-primary/50", editing && "cursor-move", active === m.id && "ring-2 ring-on-surface")}
                        style={{ left: `${pos.x}%`, top: `${pos.y}%`, backgroundColor: SIGNAL_COLOR[m.work.signal] ?? SIGNAL_COLOR.ok, touchAction: "none" }}
                        onClick={(e) => { e.stopPropagation(); if (!drag?.moved) setActive(active === m.id ? null : m.id); }}
                        onPointerDown={editing ? (e) => { e.stopPropagation(); e.currentTarget.setPointerCapture(e.pointerId); setDrag({ id: m.id, x: m.x_pct, y: m.y_pct, moved: false }); } : undefined}
                        onPointerMove={editing ? (e) => { if (drag?.id !== m.id) return; const q = pctAt(e.clientX, e.clientY); if (Math.abs(q.x - drag.x) + Math.abs(q.y - drag.y) > 0.3 || drag.moved) setDrag({ id: m.id, ...q, moved: true }); } : undefined}
                        onPointerUp={editing ? () => {
                          const d = drag;
                          if (d?.id === m.id && d.moved) run(() => api(`floor-plans/${planId}/markers/${m.id}`, { method: "PATCH", body: { x_pct: d.x, y_pct: d.y }, ifMatch: m.version })).finally(() => setDrag(null));
                          else setTimeout(() => setDrag(null), 0);
                        } : undefined}
                      >
                        {n > 0 ? n : ""}
                      </button>
                    );
                  })}
                  {sel && <MarkerPopover marker={sel} editing={editing} onClose={() => setActive(null)} onDelete={() => run(() => api(`floor-plans/${planId}/markers/${sel.id}`, { method: "DELETE" }), "Marker dihapus").then(() => setActive(null))} />}
                </div>
              )}
              {url && (
                <div className="flex flex-wrap items-center gap-3 text-xs text-on-surface-variant">
                  {Object.entries(SIGNAL_LABEL).map(([k, v]) => <span key={k} className="inline-flex items-center gap-1"><span className="inline-block h-2.5 w-2.5 rounded-full" style={{ backgroundColor: SIGNAL_COLOR[k] }} />{v}</span>)}
                  <span>· angka = pekerjaan terbuka</span>
                </div>
              )}
              {markers.length > 0 && (
                <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border text-sm">
                  {markers.map((m) => (
                    <li key={m.id} className={cn("flex cursor-pointer items-center justify-between gap-2 px-3 py-2 hover:bg-surface-container-low", active === m.id && "bg-primary-soft")} onClick={() => setActive(m.id)}>
                      <span className="min-w-0 truncate"><span className="inline-block h-2 w-2 rounded-full align-middle" style={{ backgroundColor: SIGNAL_COLOR[m.work.signal] }} /> <span className="text-xs text-muted-foreground">{TARGET_LABEL[m.target_type]}</span> {m.label || m.target_name} <span className="font-mono text-xs text-muted-foreground">{m.target_label}</span></span>
                      <span className="shrink-0 text-xs tnum text-on-surface-variant">{openCount(m)} terbuka{m.work.overdue ? ` · ${m.work.overdue} overdue` : ""}</span>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
            {editOpen && <FloorPlanDialog propertyId={p.property_id} item={p} onClose={() => setEditOpen(false)} />}
            <ConfirmDialog open={delOpen} onOpenChange={setDelOpen} title={`Hapus denah ${p.name}?`} confirmLabel="Hapus" destructive description="Denah dan seluruh marker-nya dihapus. Lokasi, facility, dan aset tidak terpengaruh." onConfirm={() => run(() => api(`floor-plans/${p.id}`, { method: "DELETE" }), "Denah dihapus").then(() => { setDelOpen(false); onDeleted?.(); })} />
          </Card>
        );
      }}
    </AsyncState>
  );
}

function MarkerPopover({ marker: m, editing, onClose, onDelete }: { marker: FloorPlanMarker; editing: boolean; onClose: () => void; onDelete: () => void }) {
  const targetLink = m.target_type === "location" ? `/property/locations/${m.target_id}` : m.target_type === "asset" ? `/assets/${m.target_id}` : null;
  const counts: [string, number][] = [["Task", m.work.open_tasks], ["WO", m.work.open_work_orders], ["Incident", m.work.open_incidents], ["Request", m.work.open_requests], ["Overdue", m.work.overdue], ["Kritis", m.work.critical]];
  return (
    <div
      className="absolute z-20 w-72 rounded-[var(--radius-lg)] border border-border bg-surface p-3 text-sm text-on-surface"
      style={{ left: `${m.x_pct}%`, top: `${m.y_pct}%`, transform: `translate(${m.x_pct > 60 ? "calc(-100% - 14px)" : "14px"}, ${m.y_pct > 60 ? "calc(-100% + 8px)" : "-8px"})`, boxShadow: "var(--elevation-3)" }}
      onClick={(e) => e.stopPropagation()}
      role="dialog"
      aria-label={m.target_name}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">{TARGET_LABEL[m.target_type]}{m.target_kind ? ` · ${m.target_kind.replace(/_/g, " ")}` : ""}</div>
          <div className="truncate font-semibold">{targetLink ? <Link to={targetLink} className="hover:underline">{m.label || m.target_name}</Link> : m.label || m.target_name}</div>
          {m.target_label && <div className="truncate font-mono text-xs text-muted-foreground">{m.target_label}</div>}
        </div>
        <button type="button" aria-label="Tutup" onClick={onClose} className="inline-flex text-on-surface-variant"><Icon name="close" size={16} /></button>
      </div>
      <div className="mt-2 flex flex-wrap gap-1.5 text-xs">
        {counts.filter(([, n]) => n > 0).map(([k, n]) => <span key={k} className={cn("rounded-full px-2 py-0.5 tnum", k === "Overdue" || k === "Kritis" ? "bg-critical-soft text-critical-text" : "bg-surface-container-high")}>{n} {k}</span>)}
        {counts.every(([, n]) => n === 0) && <span className="text-muted-foreground">Tidak ada pekerjaan terbuka.</span>}
      </div>
      {m.work.items.length > 0 && (
        <ul className="mt-2 space-y-1.5">
          {m.work.items.map((it) => (
            <li key={it.object_type + it.object_id} className="flex items-center justify-between gap-2">
              <Link to={it.deep_link} className="min-w-0 truncate hover:underline"><span className="font-mono text-xs font-semibold">{it.number}</span> {it.title}</Link>
              {["task", "work_order", "incident", "service_request"].includes(it.object_type) ? <StatusBadge objectType={it.object_type as ObjectType} status={it.status} /> : <span className="text-xs">{objectTypeLabel[it.object_type] ?? it.object_type}</span>}
            </li>
          ))}
        </ul>
      )}
      {m.target_type === "location" && openCount(m) > m.work.items.length && (
        <div className="mt-2 flex gap-3 text-xs">
          <Link to={`/operations/tasks?location_id=${m.target_id}&open=true`} className="font-semibold text-primary hover:underline">Semua task</Link>
          <Link to={`/operations/work-orders?location_id=${m.target_id}&open=true`} className="font-semibold text-primary hover:underline">Semua WO</Link>
        </div>
      )}
      {editing && (
        <div className="mt-3 flex justify-end">
          <Button size="sm" variant="destructive" icon="delete" onClick={onDelete}>Hapus marker</Button>
        </div>
      )}
    </div>
  );
}

// ---------- Buat / ubah denah ----------
function FloorPlanDialog({ propertyId, locationId, item, onClose, onSaved }: { propertyId: string; locationId?: string; item?: FloorPlan; onClose: () => void; onSaved?: (p: FloorPlan) => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const invalidate = useInvalidate();
  const [f, setF] = useState({ name: item?.name ?? "", description: item?.description ?? "", location_id: item?.location_id ?? locationId ?? null as string | null, is_active: item?.is_active ?? true });
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!f.name.trim() || !f.location_id) return toast.error(new Error("Nama dan lokasi wajib diisi"));
    setBusy(true);
    try {
      const body = { name: f.name.trim(), description: f.description.trim() || null, location_id: f.location_id, ...(item ? { is_active: f.is_active } : {}) };
      // If-Match: tolak perubahan bila denah sudah diubah orang lain (PRD P1 v2.1 P1-BLD-06)
      const plan = item ? await api<FloorPlan>(`floor-plans/${item.id}`, { method: "PATCH", body, ifMatch: item.version }) : await api<FloorPlan>("floor-plans", { body });
      if (file) await uploadPlanImage(plan.id, file);
      invalidate("floor-plans");
      toast.action(item ? "saved" : "created", `Denah ${plan.name}`);
      onSaved?.(plan);
      onClose();
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={item ? `Ubah denah · ${item.name}` : "Tambah Denah"} description="Denah dapat dibuat untuk building, tower, lantai, atau area.">
        <div className="space-y-4">
          <Field label="Nama" required><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="mis. Denah Lantai 12" /></Field>
          {!locationId && <Field label={t("label.location")} required><LocationPicker propertyId={propertyId} value={f.location_id} onChange={(id) => setF({ ...f, location_id: id })} allowTypes={PLAN_LOCATION_TYPES} /></Field>}
          <Field label={t("label.description")}><Textarea rows={2} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
          <Field label={item?.image ? "Ganti gambar (opsional)" : "Gambar denah"} help="JPG/PNG/WebP; dikompres ≤1600px. Dapat diunggah nanti."><Input type="file" accept="image/*" onChange={(e) => setFile(e.target.files?.[0] ?? null)} /></Field>
          {item && <Checkbox label="Aktif" checked={f.is_active} onCheckedChange={(v) => setF({ ...f, is_active: v })} />}
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={busy} disabled={!f.name.trim() || !f.location_id} onClick={submit}>{t("action.save")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
