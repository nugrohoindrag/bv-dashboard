// Security › Parking (PRD P2 v2.1 §6.6, D-P2-06): area parkir (kapasitas & okupansi), registri kendaraan, log keluar-masuk
// manual oleh security, pelanggaran parkir → Incident. Tab di URL: /security/parking/:tab (areas | vehicles | logs |
// violations | permits); detail pelanggaran = drawer /security/parking/violations/:id (deep link notifikasi).
// PRD P3 v2.1 §5.8 P3-PRK-02: tab "Izin Parkir" (permits) — permohonan izin/stiker parkir tenant; detail drawer
// /security/parking/permits/:id (parking-permits.tsx).
import { useMemo, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Tabs, TabsContent, TabsList, TabsTrigger, Textarea } from "@/components/ui/primitives";
import { DataGrid, useUrlFilters } from "@/components/bv/datagrid";
import { AsyncState, FormSkeleton, KeyValue, LocationPath, RelativeTime, useToast } from "@/components/bv/common";
import { AttachmentGrid, PhotoEvidenceUploader } from "@/components/bv/checklist";
import { LocationPicker } from "@/components/bv/pickers";
import { useAction, useAll, useAttachments, useInvalidate, useList, useOne } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDate, fmtDateTime, fmtMinutes } from "@/lib/format";
import { statusOptions } from "@/lib/status";
import { cn } from "@/lib/utils";
import type { ParkingArea, ParkingLog, ParkingViolation, Vehicle } from "./types";
import { PARKING_AREA_TYPES, VEHICLE_OWNER_TYPES, VEHICLE_TYPES, VIOLATION_ACTIONS, VIOLATION_TYPES, displayPlate, labelOf, normalizePlate, occupancyPct, occupancyTone, optionsOf } from "./p2";
import { FilterRow, PlateText, PropertyField, SearchBox, SelectFilter } from "./shared";
import { StatusBadge } from "@/components/bv/badges";
import { CellLocation, CellText, CellTitle } from "@/components/bv/cells";
import { TOUCH, usePropertyChoice } from "./hooks";
import { PermitsTab } from "./parking-permits";

const TABS = ["areas", "vehicles", "logs", "violations", "permits"] as const;
type Tab = (typeof TABS)[number];

export default function ParkingPage() {
  const params = useParams();
  const nav = useNavigate();
  const { pathname } = useLocation();
  const { can } = useAuth();
  // /security/parking/permits/:id → tab izin parkir + drawer; /security/parking/violations/:id → tab pelanggaran + drawer
  const permitDeepLink = !!params.id && pathname.startsWith("/security/parking/permits/");
  const tab: Tab = permitDeepLink ? "permits" : params.id ? "violations" : (TABS as readonly string[]).includes(params.tab ?? "") ? (params.tab as Tab) : "areas";
  return (
    <div>
      <PageHeader title="Parking" subtitle="Area & kapasitas parkir, registri kendaraan, log keluar-masuk oleh security, pelanggaran parkir → Incident, dan izin parkir tenant." />
      <Tabs value={tab} onValueChange={(v) => nav(`/security/parking/${v}`)}>
        <TabsList>
          <TabsTrigger value="areas">Area</TabsTrigger>
          <TabsTrigger value="vehicles">Kendaraan</TabsTrigger>
          <TabsTrigger value="logs">Keluar-Masuk</TabsTrigger>
          <TabsTrigger value="violations">Pelanggaran</TabsTrigger>
          {(can("security.parking_permits.view") || tab === "permits") && <TabsTrigger value="permits">Izin Parkir</TabsTrigger>}
        </TabsList>
        <TabsContent value="areas"><AreasTab /></TabsContent>
        <TabsContent value="vehicles"><VehiclesTab /></TabsContent>
        <TabsContent value="logs"><LogsTab /></TabsContent>
        <TabsContent value="violations"><ViolationsTab selectedId={permitDeepLink ? undefined : params.id} /></TabsContent>
        <TabsContent value="permits"><PermitsTab selectedId={permitDeepLink ? params.id : undefined} /></TabsContent>
      </Tabs>
    </div>
  );
}

// ---------- Area parkir ----------
function OccupancyBar({ occupied, capacity, compact }: { occupied: number; capacity: number; compact?: boolean }) {
  const pct = occupancyPct(occupied, capacity);
  const tone = occupancyTone(pct);
  // varian tabel: satu baris (terisi · bar tipis · persen)
  if (compact) return (
    <div className="flex min-w-[150px] items-center gap-2 whitespace-nowrap text-xs text-on-surface-variant">
      <span className="tnum">{occupied}/{capacity}</span>
      <div className="h-1.5 min-w-10 flex-1 overflow-hidden rounded-full bg-surface-container-high" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={`Okupansi ${pct}%`}>
        <div className={cn("h-full rounded-full", tone === "error" ? "bg-error" : tone === "warning" ? "bg-warning" : "bg-success")} style={{ width: `${pct}%` }} />
      </div>
      <span className="tnum">{pct}%</span>
    </div>
  );
  return (
    <div className="min-w-[140px]">
      <div className="flex justify-between gap-2 text-xs text-on-surface-variant"><span className="tnum">{occupied}/{capacity} terisi</span><span className="tnum">{pct}%</span></div>
      <div className="mt-1 h-2 w-full overflow-hidden rounded-full bg-surface-container-high" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={`Okupansi ${pct}%`}>
        <div className={cn("h-full rounded-full", tone === "error" ? "bg-error" : tone === "warning" ? "bg-warning" : "bg-success")} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

function AreasTab() {
  const { propertyId, can } = useAuth();
  const canManage = can("security.parking.manage");
  const list = useAll<ParkingArea>("parking-areas", { property_id: propertyId ?? undefined });
  const [edit, setEdit] = useState<ParkingArea | "new" | null>(null);
  const areas = list.data ?? [];
  const total = areas.filter((a) => a.is_active).reduce((s, a) => ({ capacity: s.capacity + a.capacity, occupied: s.occupied + a.occupied }), { capacity: 0, occupied: 0 });
  const columns = useMemo<ColumnDef<ParkingArea, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026): kode + nama area satu kolom (lokasi di tooltip), okupansi satu baris.
      { id: "name", header: "Area", meta: { mobile: "primary" }, cell: ({ row: { original: a } }) => <div title={a.location_path ?? undefined}><CellTitle code={a.code} title={a.name} /></div> },
      { id: "type", header: "Tipe", meta: { mobile: "secondary" }, size: 130, cell: ({ row }) => <span className="whitespace-nowrap">{labelOf(PARKING_AREA_TYPES, row.original.area_type)}</span> },
      { id: "occupancy", header: "Okupansi", meta: { mobile: "secondary" }, size: 200, cell: ({ row }) => <OccupancyBar occupied={row.original.occupied} capacity={row.original.capacity} compact /> },
      { id: "available", header: "Tersedia", meta: { mobile: "hidden" }, size: 100, cell: ({ row }) => <span className={cn("font-semibold tnum", row.original.available === 0 && row.original.capacity > 0 && "text-on-error-container")}>{row.original.available}</span> },
      { id: "status", header: "Status", meta: { mobile: "status" }, size: 110, cell: ({ row }) => (row.original.is_active ? <Badge tone="success">Aktif</Badge> : <Badge tone="neutral">Nonaktif</Badge>) },
    ],
    [],
  );
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        {areas.length > 0 ? (
          <div className="w-full max-w-md">
            <div className="mb-1 text-sm text-on-surface-variant">Total area aktif · <span className="font-semibold tnum text-on-surface">{Math.max(0, total.capacity - total.occupied)}</span> slot tersedia</div>
            <OccupancyBar occupied={total.occupied} capacity={total.capacity} />
          </div>
        ) : <span />}
        {canManage && <Button icon="add" className={TOUCH} onClick={() => setEdit("new")}>Tambah Area</Button>}
      </div>
      <DataGrid
        columns={columns}
        rows={areas}
        rowId={(r) => r.id}
        onRowClick={canManage ? (r) => { setEdit(r); } : undefined}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        empty={{ icon: "local_parking", title: "Belum ada area parkir", description: "Area parkir (basement, lot tamu, loading dock) dengan kapasitasnya dipakai untuk log keluar-masuk dan okupansi.", action: canManage ? <Button icon="add" onClick={() => setEdit("new")}>Tambah Area</Button> : undefined }}
        rowActions={canManage ? (a) => [{ label: "Edit", icon: "edit", onSelect: () => setEdit(a) }] : undefined}
      />
      {edit && <AreaDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

function AreaDialog({ item, onClose }: { item: ParkingArea | null; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [pid, setPid] = usePropertyChoice(item?.property_id);
  const [f, setF] = useState({ code: item?.code ?? "", name: item?.name ?? "", area_type: item?.area_type ?? "mixed", capacity: String(item?.capacity ?? 0), location_id: item?.location_id ?? (null as string | null), notes: item?.notes ?? "", is_active: item?.is_active ?? true });
  const [busy, setBusy] = useState(false);
  const valid = !!f.code.trim() && !!f.name.trim() && Number(f.capacity) >= 0 && (!!item || !!pid);
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    try {
      const body = { code: f.code.trim(), name: f.name.trim(), area_type: f.area_type, capacity: Number(f.capacity) || 0, location_id: f.location_id };
      if (item) await api(`parking-areas/${item.id}`, { method: "PATCH", body: { ...body, notes: f.notes.trim(), is_active: f.is_active } });
      else await api("parking-areas", { body: { ...body, property_id: pid, notes: f.notes.trim() || null }, idempotencyKey: uuid() });
      invalidate("all", "list");
      toast.action(item ? "saved" : "created", `Area parkir ${body.code.toUpperCase()}`);
      onClose();
    } catch (e) {
      toast.failed(item ? "saved" : "created", e, "Area parkir");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit area · ${item.name}` : "Tambah Area Parkir"}>
        <div className="space-y-4">
          {!item && <PropertyField value={pid} onChange={(v) => { setPid(v); setF({ ...f, location_id: null }); }} />}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Kode" required help="Unik per property, mis. B1, LOT-TAMU."><Input value={f.code} onChange={(e) => setF({ ...f, code: e.target.value.toUpperCase() })} maxLength={30} autoFocus /></Field>
            <Field label="Nama" required><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="mis. Basement 1" /></Field>
            <Field label="Tipe area"><NativeSelect value={f.area_type} onChange={(e) => setF({ ...f, area_type: e.target.value })}>{optionsOf(PARKING_AREA_TYPES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
            <Field label="Kapasitas (slot)" required><Input type="number" min={0} value={f.capacity} onChange={(e) => setF({ ...f, capacity: e.target.value })} /></Field>
          </div>
          <Field label="Lokasi" help="Opsional — lokasi di hierarki property (mis. Tower A / Basement 1)."><LocationPicker propertyId={item?.property_id ?? (pid || null)} value={f.location_id} onChange={(id) => setF({ ...f, location_id: id })} /></Field>
          <Field label="Catatan"><Textarea rows={2} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
          {item && <Checkbox label="Area aktif (dipakai untuk log masuk)" checked={f.is_active} onCheckedChange={(v) => setF({ ...f, is_active: v })} />}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Registri kendaraan ----------
function VehiclesTab() {
  const { propertyId, can } = useAuth();
  const canManage = can("security.parking.manage");
  const f = useUrlFilters();
  const list = useList<Vehicle>("vehicles", { property_id: propertyId ?? undefined, q: f.get("q") || undefined, owner_type: f.get("owner_type") || undefined, status: f.get("status") || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [edit, setEdit] = useState<Vehicle | "new" | null>(null);
  const columns = useMemo<ColumnDef<Vehicle, unknown>[]>(
    () => [
      // Satu baris per sel (29 Sep 2026): jenis/merek/warna & tipe pemilik/unit/telepon di tooltip; status = satu badge + satu
      // penanda terpenting (pelanggaran terbuka, bila tidak ada: sedang di dalam). Detail lengkap di dialog kendaraan.
      { id: "plate", header: "Plat", meta: { mobile: "primary" }, size: 170, cell: ({ row: { original: v } }) => <span title={[labelOf(VEHICLE_TYPES, v.vehicle_type), v.brand, v.color].filter(Boolean).join(" · ")}><PlateText plate={v.plate_number} /></span> },
      { id: "owner", header: "Pemilik", meta: { mobile: "secondary" }, cell: ({ row: { original: v } }) => { const name = v.owner_name || v.tenant_name || v.user_name || "—"; return <CellText max={180} title={[name, labelOf(VEHICLE_OWNER_TYPES, v.owner_type), v.unit_name, v.owner_phone].filter(Boolean).join(" · ")}>{name}</CellText>; } },
      { id: "area", header: "Area", meta: { mobile: "hidden" }, size: 140, cell: ({ row }) => <CellText max={140}>{row.original.parking_area_name ?? "—"}</CellText> },
      { id: "permit", header: "Izin parkir", meta: { mobile: "secondary" }, size: 170, cell: ({ row: { original: v } }) => <span className="whitespace-nowrap" title={v.permit_until ? `s/d ${fmtDate(v.permit_until)}` : "tanpa batas"}>{v.permit_valid ? <Badge tone="success">Berlaku</Badge> : <Badge tone="warning">Tidak berlaku</Badge>}{v.permit_until && <span className="ml-1 text-xs text-on-surface-variant tnum">s/d {fmtDate(v.permit_until)}</span>}</span> },
      { id: "status", header: "Status", meta: { mobile: "status" }, cell: ({ row: { original: v } }) => <div className="flex items-center gap-1 whitespace-nowrap"><StatusBadge objectType="vehicle" status={v.status} />{v.open_violations > 0 ? <Badge tone="error">{v.open_violations} pelanggaran</Badge> : v.inside ? <Badge tone="info"><Icon name="local_parking" size={12} aria-hidden />Di dalam</Badge> : null}</div> },
    ],
    [],
  );
  return (
    <div className="space-y-3">
      <FilterRow className="mb-0">
        <SearchBox value={f.get("q")} onSubmit={(q) => f.set({ q })} placeholder="Cari plat / nama pemilik…" />
        <SelectFilter label="Pemilik" value={f.get("owner_type")} onChange={(v) => f.set({ owner_type: v })} options={optionsOf(VEHICLE_OWNER_TYPES)} />
        <SelectFilter label="Status" value={f.get("status")} onChange={(v) => f.set({ status: v })} options={statusOptions("vehicle")} />
        {f.isFiltered && <Button variant="ghost" size="sm" icon="replay" onClick={f.reset}>Reset filter</Button>}
        {canManage && <Button icon="add" className={cn("ml-auto", TOUCH)} onClick={() => setEdit("new")}>Tambah Kendaraan</Button>}
      </FilterRow>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={canManage ? (r) => { setEdit(r); } : undefined}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={f.isFiltered}
        empty={{ icon: "directions_car", title: "Belum ada kendaraan terdaftar", description: "Kendaraan tenant/staf yang terdaftar dikenali otomatis saat dicatat masuk dan diperiksa izin parkirnya.", action: canManage ? <Button icon="add" onClick={() => setEdit("new")}>Tambah Kendaraan</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowClassName={(r) => (r.status === "blacklisted" ? "border-l-4 border-l-critical" : undefined)}
      />
      {edit && <VehicleDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

function VehicleDialog({ item, onClose }: { item: Vehicle | null; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [pid, setPid] = usePropertyChoice(item?.property_id);
  const areas = useAll<ParkingArea>("parking-areas", { property_id: (item?.property_id ?? pid) || undefined }, { enabled: !!(item?.property_id ?? pid) });
  const [f, setF] = useState({
    plate_number: item ? displayPlate(item.plate_number) : "", vehicle_type: item?.vehicle_type ?? "car", brand: item?.brand ?? "", color: item?.color ?? "", owner_type: item?.owner_type ?? "tenant",
    owner_name: item?.owner_name ?? "", owner_phone: item?.owner_phone ?? "", parking_area_id: item?.parking_area_id ?? "", permit_until: item?.permit_until ?? "", status: item?.status ?? "active", notes: item?.notes ?? "",
  });
  const [busy, setBusy] = useState(false);
  const plate = normalizePlate(f.plate_number);
  const valid = !!plate && (!!item || !!pid);
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    try {
      const opt = (v: string) => (item ? v.trim() : v.trim() || null); // edit: string kosong mengosongkan field; create: null
      const body: Record<string, unknown> = {
        plate_number: plate, vehicle_type: f.vehicle_type, brand: opt(f.brand), color: opt(f.color), owner_type: f.owner_type, owner_name: opt(f.owner_name), owner_phone: opt(f.owner_phone),
        parking_area_id: f.parking_area_id || null, permit_until: f.permit_until || null, status: f.status, notes: opt(f.notes),
      };
      if (item) await api(`vehicles/${item.id}`, { method: "PATCH", body });
      else await api("vehicles", { body: { ...body, property_id: pid }, idempotencyKey: uuid() });
      invalidate("list", "all");
      toast.action(item ? "saved" : "created", `Kendaraan ${displayPlate(plate)}`);
      onClose();
    } catch (e) {
      toast.failed(item ? "saved" : "created", e, "Kendaraan");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Kendaraan · ${displayPlate(item.plate_number)}` : "Tambah Kendaraan"}>
        <div className="space-y-4">
          {!item && <PropertyField value={pid} onChange={(v) => { setPid(v); setF({ ...f, parking_area_id: "" }); }} />}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Plat nomor" required help={plate ? `Disimpan sebagai ${plate}` : "mis. B 1234 XYZ"}><Input value={f.plate_number} onChange={(e) => setF({ ...f, plate_number: e.target.value.toUpperCase() })} autoFocus={!item} /></Field>
            <Field label="Jenis kendaraan"><NativeSelect value={f.vehicle_type} onChange={(e) => setF({ ...f, vehicle_type: e.target.value })}>{optionsOf(VEHICLE_TYPES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
            <Field label="Merek / model"><Input value={f.brand} onChange={(e) => setF({ ...f, brand: e.target.value })} /></Field>
            <Field label="Warna"><Input value={f.color} onChange={(e) => setF({ ...f, color: e.target.value })} /></Field>
            <Field label="Tipe pemilik"><NativeSelect value={f.owner_type} onChange={(e) => setF({ ...f, owner_type: e.target.value })}>{optionsOf(VEHICLE_OWNER_TYPES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
            <Field label="Nama pemilik"><Input value={f.owner_name} onChange={(e) => setF({ ...f, owner_name: e.target.value })} /></Field>
            <Field label="Telepon pemilik"><Input type="tel" inputMode="tel" value={f.owner_phone} onChange={(e) => setF({ ...f, owner_phone: e.target.value })} /></Field>
            <Field label="Area parkir"><NativeSelect value={f.parking_area_id} onChange={(e) => setF({ ...f, parking_area_id: e.target.value })}><option value="">Tanpa area khusus</option>{(areas.data ?? []).map((a) => <option key={a.id} value={a.id}>{a.code} · {a.name}</option>)}</NativeSelect></Field>
            <Field label="Izin berlaku s/d" help="Kosongkan bila tanpa batas."><Input type="date" value={f.permit_until} onChange={(e) => setF({ ...f, permit_until: e.target.value })} /></Field>
            <Field label="Status"><NativeSelect value={f.status} onChange={(e) => setF({ ...f, status: e.target.value })}>{statusOptions("vehicle").map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
          </div>
          <Field label="Catatan"><Textarea rows={2} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Log keluar-masuk ----------
const LOG_VIEWS = [
  { key: "inside", label: "Sedang parkir" },
  { key: "history", label: "Riwayat keluar" },
  { key: "all", label: "Semua" },
];

function LogsTab() {
  const { propertyId, properties, can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const canRecord = can("security.parking.record");
  const f = useUrlFilters();
  const view = f.get("view") || "inside";
  const areaId = f.get("area");
  const list = useList<ParkingLog>("parking-logs", { property_id: propertyId ?? undefined, parking_area_id: areaId || undefined, inside: view === "inside" ? true : view === "history" ? false : undefined, plate: f.get("plate") || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const areas = useAll<ParkingArea>("parking-areas", { property_id: propertyId ?? undefined });
  const [entryOpen, setEntryOpen] = useState(false);
  const [exitPlate, setExitPlate] = useState("");
  const [exitBusy, setExitBusy] = useState(false);
  // keluar cepat by plat membutuhkan property (header, atau satu-satunya property)
  const exitPid = propertyId ?? (properties.length === 1 ? properties[0].id : null);
  const quickExit = async () => {
    const plate = normalizePlate(exitPlate);
    if (!plate || !exitPid) return;
    setExitBusy(true);
    try {
      const l = await api<ParkingLog>("parking-logs/exit", { body: { property_id: exitPid, plate_number: plate }, idempotencyKey: uuid() });
      invalidate("list", "all");
      toast.success(`${displayPlate(l.plate_number)} tercatat keluar · ${fmtMinutes(l.duration_minutes)}`);
      setExitPlate("");
    } catch (e) {
      toast.error(e);
    } finally {
      setExitBusy(false);
    }
  };
  const columns = useMemo<ColumnDef<ParkingLog, unknown>[]>(() => {
    const cols: ColumnDef<ParkingLog, unknown>[] = [
      { id: "plate", header: "Plat", meta: { mobile: "primary" }, size: 190, cell: ({ row: { original: l } }) => <div className="flex items-center gap-1.5 whitespace-nowrap"><PlateText plate={l.plate_number} />{l.registered ? <Badge tone="success">Terdaftar</Badge> : <Badge tone="warning">Tidak terdaftar</Badge>}</div> },
      // satu baris per sel (29 Sep 2026): tipe pemilik & petugas pencatat di tooltip
      { id: "area", header: "Area · Gerbang", meta: { mobile: "secondary" }, cell: ({ row: { original: l } }) => { const txt = [l.parking_area_name, l.gate].filter(Boolean).join(" · ") || "—"; return <CellText max={180} title={[txt, l.owner_type ? labelOf(VEHICLE_OWNER_TYPES, l.owner_type) : null].filter(Boolean).join(" · ")}>{txt}</CellText>; } },
      { id: "entered_at", header: "Masuk", meta: { mobile: "secondary" }, size: 170, cell: ({ row: { original: l } }) => <span className="whitespace-nowrap text-sm tnum" title={l.entry_by_name ? `Dicatat oleh ${l.entry_by_name}` : undefined}>{fmtDateTime(l.entered_at)}</span> },
      { id: "status", header: "Keluar", meta: { mobile: "status" }, size: 160, cell: ({ row: { original: l } }) => (l.exited_at ? <span className="whitespace-nowrap text-sm tnum">{fmtDateTime(l.exited_at)}</span> : <Badge tone="info"><Icon name="local_parking" size={12} aria-hidden />Di dalam</Badge>) },
      { id: "duration", header: "Durasi", meta: { mobile: "secondary" }, size: 100, cell: ({ row }) => <span className="whitespace-nowrap tnum">{fmtMinutes(row.original.duration_minutes)}</span> },
      { id: "note", header: "Catatan", meta: { mobile: "hidden" }, cell: ({ row }) => <CellText max={180} muted>{row.original.note ?? "—"}</CellText> },
    ];
    if (canRecord) cols.push({ id: "actions", header: "", size: 150, cell: ({ row }) => (row.original.exited_at ? null : <ExitButton log={row.original} />) });
    return cols;
  }, [canRecord]);
  return (
    <div className="space-y-3">
      {canRecord && (
        <div className="flex flex-wrap items-end gap-2 rounded-[var(--radius-lg)] border border-border bg-surface-container-low p-3">
          <Button icon="login" className={TOUCH} onClick={() => setEntryOpen(true)}>Catat Masuk</Button>
          <form className="flex w-full flex-wrap items-center gap-2 sm:w-auto" onSubmit={(e) => { e.preventDefault(); quickExit(); }}>
            <Input className="w-full font-mono uppercase sm:w-48" placeholder="Plat keluar, mis. B1234XYZ" value={exitPlate} onChange={(e) => setExitPlate(e.target.value.toUpperCase())} aria-label="Plat kendaraan keluar" disabled={!exitPid} />
            <Button type="submit" variant="secondary" icon="logout" className={TOUCH} disabled={!normalizePlate(exitPlate) || !exitPid} loading={exitBusy}>Keluar cepat</Button>
          </form>
          {!exitPid && <span className="text-xs text-on-surface-variant">Pilih property di header untuk keluar cepat by plat.</span>}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        {LOG_VIEWS.map((v) => <FilterChip key={v.key} selected={view === v.key} onClick={() => f.set({ view: v.key === "inside" ? null : v.key })}>{v.label}</FilterChip>)}
      </div>
      <FilterRow className="mb-0">
        <SearchBox value={f.get("plate")} onSubmit={(q) => f.set({ plate: q })} placeholder="Cari plat…" />
        <SelectFilter label="Area" value={areaId} onChange={(v) => f.set({ area: v })} options={(areas.data ?? []).map((a) => ({ value: a.id, label: `${a.code} · ${a.name}` }))} />
        {(f.get("plate") || areaId) && <Button variant="ghost" size="sm" icon="replay" onClick={() => f.set({ plate: null, area: null })}>Reset filter</Button>}
      </FilterRow>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={!!f.get("plate") || !!areaId}
        empty={view === "inside"
          ? { icon: "local_parking", title: "Tidak ada kendaraan tercatat di dalam", description: "Kendaraan yang dicatat masuk dan belum keluar tampil di sini.", action: canRecord ? <Button icon="login" onClick={() => setEntryOpen(true)}>Catat Masuk</Button> : undefined }
          : { icon: "history", title: "Belum ada log keluar-masuk", description: "Log dicatat security saat kendaraan masuk dan keluar area parkir." }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
      />
      {entryOpen && <EntryDialog onClose={() => setEntryOpen(false)} />}
    </div>
  );
}

/** Catat keluar satu log (POST /parking-logs/{id}/exit). */
function ExitButton({ log }: { log: ParkingLog }) {
  const toast = useToast();
  const exit = useAction<void, ParkingLog>(() => `parking-logs/${log.id}/exit`, { body: () => ({}) });
  return (
    <Button size="sm" variant="secondary" icon="logout" className={TOUCH} loading={exit.isPending}
      onClick={(e) => { e.stopPropagation(); exit.mutateAsync().then((r) => toast.success(`${displayPlate(r.plate_number)} tercatat keluar · ${fmtMinutes(r.duration_minutes)}`)).catch(toast.error); }}>
      Catat Keluar
    </Button>
  );
}

function EntryDialog({ onClose }: { onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [pid, setPid] = usePropertyChoice();
  const areas = useAll<ParkingArea>("parking-areas", { property_id: pid || undefined }, { enabled: !!pid });
  const [f, setF] = useState({ parking_area_id: "", plate_number: "", gate: "", note: "" });
  const [busy, setBusy] = useState(false);
  const plate = normalizePlate(f.plate_number);
  const valid = !!plate && (!!f.parking_area_id || !!pid);
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    try {
      const l = await api<ParkingLog>("parking-logs", { body: { parking_area_id: f.parking_area_id || null, property_id: f.parking_area_id ? undefined : pid, plate_number: plate, gate: f.gate.trim() || null, note: f.note.trim() || null }, idempotencyKey: uuid() });
      invalidate("list", "all");
      if (l.registered) toast.success(`${displayPlate(l.plate_number)} tercatat masuk${l.parking_area_name ? ` · ${l.parking_area_name}` : ""}`);
      else toast.warning(`${displayPlate(l.plate_number)} tercatat masuk — kendaraan tidak terdaftar`);
      onClose();
    } catch (e) {
      toast.error(e); // 409 ALREADY_INSIDE → detail server
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Catat Kendaraan Masuk" description="Plat dinormalisasi otomatis; kendaraan terdaftar dikenali dari registri.">
        <div className="space-y-4">
          <PropertyField value={pid} onChange={(v) => { setPid(v); setF({ ...f, parking_area_id: "" }); }} />
          <Field label="Plat nomor" required help={plate ? `Dicatat sebagai ${displayPlate(plate)}` : undefined}><Input className="font-mono uppercase" value={f.plate_number} onChange={(e) => setF({ ...f, plate_number: e.target.value.toUpperCase() })} autoFocus /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Area parkir"><NativeSelect value={f.parking_area_id} onChange={(e) => setF({ ...f, parking_area_id: e.target.value })}><option value="">Tanpa area</option>{(areas.data ?? []).filter((a) => a.is_active).map((a) => <option key={a.id} value={a.id}>{a.code} · {a.name} ({a.available} tersedia)</option>)}</NativeSelect></Field>
            <Field label="Gerbang"><Input value={f.gate} onChange={(e) => setF({ ...f, gate: e.target.value })} placeholder="mis. Gate Utara" /></Field>
          </div>
          <Field label="Catatan"><Textarea rows={2} value={f.note} onChange={(e) => setF({ ...f, note: e.target.value })} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="login" disabled={!valid} loading={busy} onClick={submit}>Catat Masuk</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Pelanggaran parkir ----------
function ViolationsTab({ selectedId }: { selectedId?: string }) {
  const { propertyId, can } = useAuth();
  const nav = useNavigate();
  const { search } = useLocation();
  const f = useUrlFilters();
  const canRecord = can("security.parking.record");
  const list = useList<ParkingViolation>("parking-violations", { property_id: propertyId ?? undefined, status: f.get("status") || undefined, plate: f.get("plate") || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [createOpen, setCreateOpen] = useState(false);
  const columns = useMemo<ColumnDef<ParkingViolation, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026): no. + plat satu kolom, jenis pelanggaran satu baris (tindakan di tooltip), lokasi =
      // nama terakhir. Pemilik, pencatat, foto & tindakan lengkap ada di drawer detail.
      { id: "plate", header: "Pelanggaran", meta: { mobile: "primary" }, cell: ({ row: { original: v } }) => <div title={v.owner_name ?? (v.vehicle_id ? "Terdaftar" : "Tidak terdaftar")}><CellTitle code={v.violation_number} title={displayPlate(v.plate_number)} /></div> },
      { id: "type", header: "Jenis", meta: { mobile: "secondary" }, cell: ({ row: { original: v } }) => <CellText max={180} title={`${labelOf(VIOLATION_TYPES, v.violation_type)} · Tindakan: ${labelOf(VIOLATION_ACTIONS, v.action_taken)}`}>{labelOf(VIOLATION_TYPES, v.violation_type)}</CellText> },
      { id: "where", header: "Area / Lokasi", meta: { mobile: "hidden" }, cell: ({ row: { original: v } }) => (v.parking_area_name ? <CellText max={160}>{v.parking_area_name}</CellText> : <CellLocation path={v.location.path_text} />) },
      { id: "status", header: "Status", meta: { mobile: "status" }, cell: ({ row: { original: v } }) => <div className="flex items-center gap-1 whitespace-nowrap"><StatusBadge objectType="parking_violation" status={v.status} />{v.incident_id && <Link to={`/operations/incidents/${v.incident_id}`} onClick={(e) => e.stopPropagation()} className="font-mono text-xs text-primary hover:underline">{v.incident_number}</Link>}</div> },
      { id: "recorded_at", header: "Dicatat", meta: { mobile: "secondary" }, size: 150, cell: ({ row }) => <RelativeTime value={row.original.recorded_at} className="whitespace-nowrap text-sm" /> },
    ],
    [],
  );
  const close = () => nav(`/security/parking/violations${search}`);
  return (
    <div className="space-y-3">
      <FilterRow className="mb-0">
        <SearchBox value={f.get("plate")} onSubmit={(q) => f.set({ plate: q })} placeholder="Cari plat…" />
        <SelectFilter label="Status" value={f.get("status")} onChange={(v) => f.set({ status: v })} options={statusOptions("parking_violation")} />
        {f.isFiltered && <Button variant="ghost" size="sm" icon="replay" onClick={f.reset}>Reset filter</Button>}
        {canRecord && <Button icon="add" className={cn("ml-auto", TOUCH)} onClick={() => setCreateOpen(true)}>Catat Pelanggaran</Button>}
      </FilterRow>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/security/parking/violations/${r.id}${search}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={f.isFiltered}
        empty={{ icon: "report", title: "Belum ada pelanggaran parkir", description: "Pelanggaran (parkir liar, tanpa izin, menghalangi) dicatat security beserta foto dan tindakannya.", action: canRecord ? <Button icon="add" onClick={() => setCreateOpen(true)}>Catat Pelanggaran</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowClassName={(r) => (r.id === selectedId ? "bg-primary-soft" : undefined)}
      />
      {createOpen && <ViolationCreateDialog onClose={() => setCreateOpen(false)} onCreated={(v) => nav(`/security/parking/violations/${v.id}${search}`)} />}
      {selectedId && <ViolationDrawer id={selectedId} onClose={close} />}
    </div>
  );
}

function ViolationCreateDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (v: ParkingViolation) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [pid, setPid] = usePropertyChoice();
  const areas = useAll<ParkingArea>("parking-areas", { property_id: pid || undefined }, { enabled: !!pid });
  const [f, setF] = useState({ plate_number: "", parking_area_id: "", location_id: null as string | null, violation_type: "illegal_parking", action_taken: "none", description: "" });
  const [busy, setBusy] = useState(false);
  const plate = normalizePlate(f.plate_number);
  const valid = !!plate && (!!f.parking_area_id || !!f.location_id || !!pid);
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    try {
      const v = await api<ParkingViolation>("parking-violations", {
        body: { property_id: pid || undefined, parking_area_id: f.parking_area_id || null, location_id: f.location_id, plate_number: plate, violation_type: f.violation_type, action_taken: f.action_taken, description: f.description.trim() || null },
        idempotencyKey: uuid(),
      });
      invalidate("list");
      toast.success(`Pelanggaran ${v.violation_number} dicatat — tambahkan foto bukti di detail.`);
      onClose();
      onCreated(v);
    } catch (e) {
      toast.failed("created", e, "Pelanggaran parkir");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Catat Pelanggaran Parkir" description="Foto bukti diunggah di detail setelah pelanggaran tersimpan.">
        <div className="space-y-4">
          <PropertyField value={pid} onChange={(v) => { setPid(v); setF({ ...f, parking_area_id: "", location_id: null }); }} />
          <Field label="Plat nomor" required help={plate ? `Dicatat sebagai ${displayPlate(plate)}` : undefined}><Input className="font-mono uppercase" value={f.plate_number} onChange={(e) => setF({ ...f, plate_number: e.target.value.toUpperCase() })} autoFocus /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Jenis pelanggaran" required><NativeSelect value={f.violation_type} onChange={(e) => setF({ ...f, violation_type: e.target.value })}>{optionsOf(VIOLATION_TYPES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
            <Field label="Tindakan"><NativeSelect value={f.action_taken} onChange={(e) => setF({ ...f, action_taken: e.target.value })}>{optionsOf(VIOLATION_ACTIONS).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
          </div>
          <Field label="Area parkir"><NativeSelect value={f.parking_area_id} onChange={(e) => setF({ ...f, parking_area_id: e.target.value })}><option value="">— (pilih lokasi di bawah)</option>{(areas.data ?? []).map((a) => <option key={a.id} value={a.id}>{a.code} · {a.name}</option>)}</NativeSelect></Field>
          {!f.parking_area_id && <Field label="Lokasi" help="Bila pelanggaran di luar area parkir terdaftar (mis. drop-off lobby)."><LocationPicker propertyId={pid || null} value={f.location_id} onChange={(id) => setF({ ...f, location_id: id })} /></Field>}
          <Field label="Keterangan"><Textarea rows={3} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} placeholder="mis. parkir di jalur pemadam, pengemudi tidak ada di tempat" /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ViolationDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const toast = useToast();
  const q = useOne<ParkingViolation>("parking-violations", id);
  const photos = useAttachments("parking_violation", id);
  const [dlg, setDlg] = useState<"update" | "resolve" | "create_incident" | null>(null);
  const v = q.data;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={v ? `Pelanggaran ${v.violation_number}` : "Pelanggaran parkir"} description={v ? `${displayPlate(v.plate_number)} · ${labelOf(VIOLATION_TYPES, v.violation_type)}` : undefined}>
        <AsyncState query={q} skeleton={<FormSkeleton fields={5} />}>
          {(v) => (
            <div className="space-y-5">
              <div className="flex flex-wrap items-center gap-2">
                <StatusBadge objectType="parking_violation" status={v.status} />
                {v.incident_id && <Link to={`/operations/incidents/${v.incident_id}`} className="inline-flex items-center gap-1 text-sm font-semibold text-primary hover:underline"><Icon name="emergency_home" size={16} aria-hidden />Incident {v.incident_number}</Link>}
              </div>
              <KeyValue items={[
                { label: "Plat", value: <span className="inline-flex flex-wrap items-center gap-1.5"><PlateText plate={v.plate_number} />{v.vehicle_id ? <Badge tone="success">Terdaftar</Badge> : <Badge tone="warning">Tidak terdaftar</Badge>}</span> },
                { label: "Pemilik", value: v.owner_name ?? "—" },
                { label: "Pelanggaran", value: labelOf(VIOLATION_TYPES, v.violation_type) },
                { label: "Area / lokasi", value: v.parking_area_name ?? <LocationPath pathText={v.location.path_text} /> },
                { label: "Tindakan", value: labelOf(VIOLATION_ACTIONS, v.action_taken) },
                { label: "Dicatat", value: <>{v.recorded_by_name ?? "—"}<span className="block text-xs text-on-surface-variant">{fmtDateTime(v.recorded_at)}</span></> },
                ...(v.resolved_at ? [{ label: "Selesai", value: <>{fmtDateTime(v.resolved_at)}{v.resolution && <span className="block whitespace-pre-line">{v.resolution}</span>}</> }] : []),
              ]} />
              {v.description && <p className="whitespace-pre-line rounded-[var(--radius-md)] bg-surface-container px-3 py-2 text-sm">{v.description}</p>}
              <div>
                <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Foto bukti ({(photos.data ?? []).length})</div>
                <div className="space-y-2">
                  {v.allowed_actions.includes("attach") && v.status === "open" && <PhotoEvidenceUploader objectType="parking_violation" objectId={v.id} attachmentType="photo" compact label="Unggah foto bukti" onUploaded={() => { photos.refetch(); q.refetch(); }} />}
                  <AttachmentGrid items={photos.data ?? []} emptyLabel="Belum ada foto bukti." />
                </div>
              </div>
              {(v.allowed_actions.includes("update") || v.allowed_actions.includes("resolve") || v.allowed_actions.includes("create_incident")) && (
                <DialogFooter className="flex-wrap">
                  {v.allowed_actions.includes("create_incident") && <Button variant="secondary" icon="emergency_home" className={cn("mr-auto", TOUCH)} onClick={() => setDlg("create_incident")}>Buat Incident</Button>}
                  {v.allowed_actions.includes("update") && <Button variant="secondary" icon="edit" className={TOUCH} onClick={() => setDlg("update")}>Ubah tindakan</Button>}
                  {v.allowed_actions.includes("resolve") && <Button icon="check_circle" className={TOUCH} onClick={() => setDlg("resolve")}>Selesaikan</Button>}
                </DialogFooter>
              )}
              {dlg && <ViolationActionDialog violation={v} action={dlg} onClose={() => setDlg(null)} onDone={(r, action) => { if (action === "create_incident" && r.incident_id) toast.success(`Incident ${r.incident_number ?? ""} dibuat dari ${r.violation_number}`, { to: `/operations/incidents/${r.incident_id}`, label: "Buka" }); else toast.action(action === "resolve" ? "resolved" : "updated", r.violation_number); }} />}
            </div>
          )}
        </AsyncState>
      </DialogContent>
    </Dialog>
  );
}

function ViolationActionDialog({ violation: v, action, onClose, onDone }: { violation: ParkingViolation; action: "update" | "resolve" | "create_incident"; onClose: () => void; onDone: (v: ParkingViolation, action: string) => void }) {
  const toast = useToast();
  const [f, setF] = useState({ action_taken: v.action_taken, description: v.description ?? "", resolution: "", severity: "low", title: `Pelanggaran parkir ${displayPlate(v.plate_number)}` });
  const act = useAction<Record<string, unknown>, ParkingViolation>(() => `parking-violations/${v.id}/${action.replace("_", "-")}`);
  const body = action === "update" ? { action_taken: f.action_taken, description: f.description.trim() } : action === "resolve" ? { resolution: f.resolution.trim(), action_taken: f.action_taken } : { severity: f.severity, title: f.title.trim() || undefined };
  const valid = action !== "resolve" || !!f.resolution.trim();
  const submit = () => act.mutateAsync(body).then((r) => { onDone(r, action); onClose(); }).catch((e) => toast.failed(action === "create_incident" ? "created" : "updated", e, action === "create_incident" ? "Incident" : v.violation_number));
  const title = action === "update" ? "Ubah tindakan" : action === "resolve" ? "Selesaikan pelanggaran" : "Buat Incident dari pelanggaran";
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`${title} · ${v.violation_number}`} description={action === "create_incident" ? "Incident security (kategori Unauthorized Access) dibuat & ditautkan; status pelanggaran menjadi Jadi Incident." : undefined}>
        <div className="space-y-4">
          {action !== "create_incident" && <Field label="Tindakan"><NativeSelect value={f.action_taken} onChange={(e) => setF({ ...f, action_taken: e.target.value })}>{optionsOf(VIOLATION_ACTIONS).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>}
          {action === "update" && <Field label="Keterangan"><Textarea rows={3} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>}
          {action === "resolve" && <Field label="Penyelesaian" required><Textarea rows={3} autoFocus value={f.resolution} onChange={(e) => setF({ ...f, resolution: e.target.value })} placeholder="mis. kendaraan dipindahkan pemilik, teguran tertulis diberikan" /></Field>}
          {action === "create_incident" && (
            <>
              <Field label="Judul incident"><Input value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} maxLength={200} /></Field>
              <Field label="Severity"><NativeSelect value={f.severity} onChange={(e) => setF({ ...f, severity: e.target.value })}>{statusOptions("severity").map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}</NativeSelect></Field>
            </>
          )}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={act.isPending} onClick={submit}>{action === "create_incident" ? "Buat Incident" : action === "resolve" ? "Selesaikan" : "Simpan"}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
