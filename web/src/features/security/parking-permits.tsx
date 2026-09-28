// Izin Parkir tenant (PRD P3 v2.1 §5.8 P3-PRK-02, di atas entitas parkir P2 D-P2-06): tab "Izin Parkir" di Security › Parking
// (/security/parking/permits, detail drawer /security/parking/permits/:id — deep link notifikasi). Daftar GET /parking-permits
// (property_id, status, q) → detail (kendaraan & dokumen STNK, tenant/unit, jenis izin, masa berlaku, stiker, tarif) → Setujui (area,
// masa berlaku YYYY-MM-DD, nomor stiker, tarif) / Tolak (alasan) / Cabut (alasan) / Ubah; tombol WhatsApp manual (konteks
// parking_permit). Aksi mengikuti allowed_actions (security.parking_permits.approve).
import { useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { Alert, Badge, Button, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid, useUrlFilters } from "@/components/bv/datagrid";
import { AsyncState, FormSkeleton, KeyValue, ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { AttachmentGrid } from "@/components/bv/checklist";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { WhatsAppButton } from "@/components/bv/WhatsAppButton";
import { useAll, useAttachments, useInvalidate, useList, useOne } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { statusOptions } from "@/lib/status";
import { fmtDate, fmtDateTime, fmtMoney } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { ParkingArea } from "./types";
import { VEHICLE_TYPES, displayPlate, labelOf } from "./p2";
import { FilterRow, PlateText, SearchBox, SelectFilter } from "./shared";
import { TOUCH } from "./hooks";

export interface ParkingPermit {
  id: string;
  permit_number: string;
  property_id: string;
  vehicle_id: string;
  plate_number: string;
  vehicle_type: string;
  /** merek + warna */
  vehicle_label: string;
  tenant_id: string | null;
  tenant_name: string | null;
  unit_location_id: string | null;
  unit_name: string | null;
  requested_by_name: string | null;
  parking_area_id: string | null;
  parking_area_name: string | null;
  /** monthly | annual | temporary */
  permit_type: string;
  /** requested | approved | rejected | cancelled | expired | revoked */
  status: string;
  /** Disetujui & dalam masa berlaku. */
  is_active: boolean;
  /** YYYY-MM-DD */
  valid_from: string | null;
  valid_until: string | null;
  sticker_number: string | null;
  fee_amount: number | null;
  notes: string | null;
  decision_reason: string | null;
  decided_by_name: string | null;
  decided_at: string | null;
  requested_at: string;
  allowed_actions: string[];
  version: number;
}

const PERMIT_TYPES: Record<string, string> = { monthly: "Bulanan", annual: "Tahunan", temporary: "Sementara (7 hari)" };
const permitTypeLabel = (t: string) => PERMIT_TYPES[t] ?? t.replace(/_/g, " ");

function Validity({ p }: { p: Pick<ParkingPermit, "valid_from" | "valid_until" | "is_active" | "status"> }) {
  if (!p.valid_from && !p.valid_until) return <span className="text-on-surface-variant">—</span>;
  return (
    <span className="inline-flex flex-wrap items-center gap-1">
      <span className="tnum">{p.valid_from ? fmtDate(p.valid_from) : "…"} – {p.valid_until ? fmtDate(p.valid_until) : "…"}</span>
      {p.is_active && <Badge tone="success">Berlaku</Badge>}
    </span>
  );
}

export function PermitsTab({ selectedId }: { selectedId?: string }) {
  const { propertyId, can } = useAuth();
  const nav = useNavigate();
  const { search } = useLocation();
  const f = useUrlFilters();
  const canView = can("security.parking_permits.view");
  const status = f.get("status");
  const q = f.get("q");
  const list = useList<ParkingPermit>("parking-permits", { property_id: propertyId ?? undefined, status: status || undefined, q: q || undefined }, { enabled: canView });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const columns = useMemo<ColumnDef<ParkingPermit, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): no. izin + plat satu kolom (jenis/label kendaraan di tooltip), tenant satu baris
    // (unit & pemohon di tooltip), masa berlaku satu baris, area (stiker di tooltip). Jenis izin & detail lengkap di drawer.
    { id: "vehicle", header: "Izin parkir", meta: { mobile: "primary" }, cell: ({ row: { original: p } }) => <div title={[labelOf(VEHICLE_TYPES, p.vehicle_type), p.vehicle_label].filter(Boolean).join(" · ") || undefined}><CellTitle code={p.permit_number} title={displayPlate(p.plate_number)} /></div> },
    { id: "tenant", header: "Tenant / unit", meta: { mobile: "secondary" }, cell: ({ row: { original: p } }) => (
      <CellText max={180} title={[p.tenant_name ?? p.requested_by_name, p.unit_name, p.tenant_name && p.requested_by_name ? `oleh ${p.requested_by_name}` : null].filter(Boolean).join(" · ") || undefined}>{p.tenant_name ?? p.requested_by_name ?? "—"}</CellText>
    ) },
    { id: "validity", header: "Masa berlaku", meta: { mobile: "secondary" }, size: 220, cell: ({ row: { original: p } }) => (!p.valid_from && !p.valid_until ? <span className="text-sm text-on-surface-variant">—</span> : (
      <span className="inline-flex items-center gap-1 whitespace-nowrap text-sm">
        <span className="tnum">{p.valid_from ? fmtDate(p.valid_from) : "…"} – {p.valid_until ? fmtDate(p.valid_until) : "…"}</span>
        {p.is_active && <Badge tone="success">Berlaku</Badge>}
      </span>
    )) },
    { id: "area", header: "Area", meta: { mobile: "hidden" }, size: 150, cell: ({ row: { original: p } }) => <CellText max={150} title={[p.parking_area_name, p.sticker_number ? `stiker ${p.sticker_number}` : null].filter(Boolean).join(" · ") || undefined}>{p.parking_area_name ?? "—"}</CellText> },
    { id: "status", header: "Status", meta: { mobile: "status" }, size: 130, cell: ({ row }) => <StatusBadge objectType="parking_permit" status={row.original.status} /> },
    { id: "requested_at", header: "Diajukan", meta: { mobile: "secondary" }, size: 130, cell: ({ row }) => <RelativeTime value={row.original.requested_at} className="whitespace-nowrap text-xs text-on-surface-variant" /> },
  ], []);
  if (!canView) return <Alert variant="info">Anda tidak memiliki akses ke izin parkir tenant (security.parking_permits.view).</Alert>;
  const close = () => nav(`/security/parking/permits${search}`);
  const isFiltered = !!status || !!q;
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Tampilan izin">
        <FilterChip selected={!status} onClick={() => f.set({ status: null })}>Semua</FilterChip>
        <FilterChip selected={status === "requested"} onClick={() => f.set({ status: status === "requested" ? null : "requested" })}>Menunggu persetujuan</FilterChip>
        <FilterChip selected={status === "approved"} onClick={() => f.set({ status: status === "approved" ? null : "approved" })}>Disetujui</FilterChip>
      </div>
      <FilterRow className="mb-0">
        <SearchBox value={q} onSubmit={(v) => f.set({ q: v })} placeholder="Cari plat / no. izin / tenant…" />
        <SelectFilter label="Status" value={status} onChange={(v) => f.set({ status: v })} options={statusOptions("parking_permit")} />
        {isFiltered && <Button variant="ghost" size="sm" icon="replay" onClick={() => f.set({ status: null, q: null })}>Reset filter</Button>}
      </FilterRow>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/security/parking/permits/${r.id}${search}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={isFiltered}
        empty={{ icon: "badge", title: "Belum ada permohonan izin parkir", description: "Tenant mengajukan izin/stiker parkir dari Tenant App setelah mendaftarkan kendaraannya; permohonan tampil di sini untuk disetujui Security/Building Management." }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowClassName={(r) => (r.id === selectedId ? "bg-primary-soft" : r.status === "requested" ? "border-l-4 border-l-warning" : undefined)}
      />
      {selectedId && <PermitDrawer id={selectedId} onClose={close} />}
    </div>
  );
}

type PermitAction = "approve" | "update" | "reject" | "revoke";

function PermitDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { can } = useAuth();
  const q = useOne<ParkingPermit>("parking-permits", id);
  const p = q.data;
  const docs = useAttachments("vehicle", p && can("security.parking.view", p.property_id) ? p.vehicle_id : undefined);
  const [dlg, setDlg] = useState<PermitAction | null>(null);
  const [busy, setBusy] = useState(false);
  const decide = async (action: "reject" | "revoke", reason: string) => {
    setBusy(true);
    try {
      const r = await api<ParkingPermit>(`parking-permits/${id}/${action}`, { body: { reason }, idempotencyKey: uuid() });
      invalidate("list", "one", "all");
      toast.success(action === "reject" ? `Izin ${r.permit_number} ditolak` : `Izin ${r.permit_number} dicabut`);
      setDlg(null);
    } catch (e) {
      toast.failed(action === "reject" ? "rejected" : "updated", e, "Izin parkir");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={p ? `Izin parkir ${p.permit_number}` : "Izin parkir"} description={p ? `${permitTypeLabel(p.permit_type)} · diajukan ${fmtDateTime(p.requested_at)}` : undefined}>
        <AsyncState query={q} skeleton={<FormSkeleton fields={6} />}>
          {(p) => (
            <div className="space-y-5">
              <div className="flex flex-wrap items-center gap-2">
                <StatusBadge objectType="parking_permit" status={p.status} />
                {p.is_active && <Badge tone="success"><Icon name="verified" size={12} aria-hidden />Berlaku</Badge>}
              </div>
              {p.status === "requested" && <Alert variant="warning" title="Menunggu persetujuan">Periksa kendaraan & dokumen (STNK) lalu tetapkan area, masa berlaku, nomor stiker, dan tarif.</Alert>}
              {p.decision_reason && (p.status === "rejected" || p.status === "revoked") && <Alert variant="critical" title={p.status === "rejected" ? "Ditolak" : "Dicabut"}>{p.decision_reason}</Alert>}
              <KeyValue items={[
                { label: "Kendaraan", value: <span className="inline-flex flex-wrap items-center gap-1.5"><PlateText plate={p.plate_number} /><span className="text-sm text-on-surface-variant">{[labelOf(VEHICLE_TYPES, p.vehicle_type), p.vehicle_label].filter(Boolean).join(" · ")}</span></span> },
                { label: "Tenant", value: p.tenant_name ?? "—" },
                { label: "Unit", value: p.unit_name ?? "—" },
                { label: "Diajukan oleh", value: <>{p.requested_by_name ?? "—"}<span className="block text-xs text-on-surface-variant">{fmtDateTime(p.requested_at)}</span></> },
                { label: "Jenis izin", value: permitTypeLabel(p.permit_type) },
                { label: "Area parkir", value: p.parking_area_name ?? "—" },
                { label: "Masa berlaku", value: <Validity p={p} /> },
                { label: "Nomor stiker", value: p.sticker_number ? <span className="font-mono">{p.sticker_number}</span> : "—" },
                { label: "Tarif", value: p.fee_amount != null ? fmtMoney(p.fee_amount) : "—" },
                ...(p.notes ? [{ label: "Catatan", value: <span className="whitespace-pre-line">{p.notes}</span> }] : []),
                ...(p.decided_at ? [{ label: "Diputuskan", value: <>{fmtDateTime(p.decided_at)}{p.decided_by_name ? ` · ${p.decided_by_name}` : ""}</> }] : []),
              ]} />
              {can("security.parking.view", p.property_id) && (
                <div>
                  <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Dokumen kendaraan (STNK / foto)</div>
                  {docs.isLoading ? <p className="text-sm text-on-surface-variant">Memuat dokumen…</p> : docs.isError ? <p className="text-sm text-on-surface-variant">Dokumen tidak dapat dimuat.</p> : <AttachmentGrid items={docs.data ?? []} emptyLabel="Tenant tidak melampirkan dokumen." />}
                </div>
              )}
              <DialogFooter className="flex-wrap">
                <span className="mr-auto"><WhatsAppButton context="parking_permit" objectType="parking_permit" objectId={p.id} label="WhatsApp" /></span>
                {p.allowed_actions.includes("revoke") && <Button variant="secondary" icon="block" className={TOUCH} onClick={() => setDlg("revoke")}>Cabut…</Button>}
                {p.allowed_actions.includes("update") && <Button variant="secondary" icon="edit" className={TOUCH} onClick={() => setDlg("update")}>Ubah</Button>}
                {p.allowed_actions.includes("reject") && <Button variant="secondary" icon="cancel" className={TOUCH} onClick={() => setDlg("reject")}>Tolak…</Button>}
                {p.allowed_actions.includes("approve") && <Button icon="check_circle" className={TOUCH} onClick={() => setDlg("approve")}>Setujui…</Button>}
              </DialogFooter>
              {(dlg === "approve" || dlg === "update") && <PermitDecisionDialog permit={p} action={dlg} onClose={() => setDlg(null)} />}
              <ReasonDialog open={dlg === "reject"} onOpenChange={(o) => !o && setDlg(null)} title={`Tolak izin ${p.permit_number}`} description="Alasan dikirim ke tenant dan tercatat di audit log." confirmLabel="Tolak izin" destructive loading={busy} onConfirm={(r) => decide("reject", r)} />
              <ReasonDialog open={dlg === "revoke"} onOpenChange={(o) => !o && setDlg(null)} title={`Cabut izin ${p.permit_number}`} description="Izin berhenti berlaku seketika; kendaraan tidak lagi dianggap ber-izin saat pemeriksaan security." confirmLabel="Cabut izin" destructive loading={busy} onConfirm={(r) => decide("revoke", r)} />
            </div>
          )}
        </AsyncState>
      </DialogContent>
    </Dialog>
  );
}

/** Setujui / ubah izin: area parkir, masa berlaku (YYYY-MM-DD), nomor stiker, tarif, catatan. */
function PermitDecisionDialog({ permit: p, action, onClose }: { permit: ParkingPermit; action: "approve" | "update"; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const areas = useAll<ParkingArea>("parking-areas", { property_id: p.property_id });
  const [f, setF] = useState({ parking_area_id: p.parking_area_id ?? "", valid_from: p.valid_from ?? "", valid_until: p.valid_until ?? "", sticker_number: p.sticker_number ?? "", fee_amount: p.fee_amount != null ? String(p.fee_amount) : "", notes: p.notes ?? "" });
  const [busy, setBusy] = useState(false);
  const fee = f.fee_amount.trim() === "" ? null : Number(f.fee_amount.replace(/[^\d]/g, ""));
  const rangeInvalid = !!f.valid_from && !!f.valid_until && f.valid_until < f.valid_from;
  const valid = !rangeInvalid && (fee === null || (Number.isFinite(fee) && fee >= 0));
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    try {
      const r = await api<ParkingPermit>(`parking-permits/${p.id}/${action}`, {
        body: {
          parking_area_id: f.parking_area_id || null, valid_from: f.valid_from || null, valid_until: f.valid_until || null,
          sticker_number: f.sticker_number.trim() || null, fee_amount: fee, notes: f.notes.trim() || null,
        },
        idempotencyKey: uuid(),
      });
      invalidate("list", "one", "all");
      toast.success(action === "approve" ? `Izin ${r.permit_number} disetujui${r.valid_until ? ` s/d ${fmtDate(r.valid_until)}` : ""}` : `Izin ${r.permit_number} diperbarui`);
      onClose();
    } catch (e) {
      toast.failed(action === "approve" ? "approved" : "updated", e, "Izin parkir");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={action === "approve" ? `Setujui izin · ${p.permit_number}` : `Ubah izin · ${p.permit_number}`} description={action === "approve" ? "Kendaraan menjadi ber-izin sampai tanggal berakhir; tenant diberi tahu. Tarif menjadi dasar tagihan parkir." : "Perubahan masa berlaku & area langsung berlaku untuk pemeriksaan security."}>
        <div className="space-y-4">
          <div className="rounded-[var(--radius-md)] bg-surface-container-low px-3 py-2 text-sm">
            <PlateText plate={p.plate_number} /> <span className="text-on-surface-variant">· {permitTypeLabel(p.permit_type)} · {p.tenant_name ?? p.requested_by_name ?? "—"}</span>
          </div>
          <Field label="Area parkir">
            <NativeSelect value={f.parking_area_id} onChange={(e) => setF({ ...f, parking_area_id: e.target.value })}>
              <option value="">— Tanpa area khusus —</option>
              {(areas.data ?? []).filter((a) => a.is_active || a.id === f.parking_area_id).map((a) => <option key={a.id} value={a.id}>{a.code} · {a.name} ({a.available} tersedia)</option>)}
            </NativeSelect>
          </Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Berlaku mulai"><Input type="date" value={f.valid_from} onChange={(e) => setF({ ...f, valid_from: e.target.value })} /></Field>
            <Field label="Berlaku sampai" error={rangeInvalid ? "Harus setelah tanggal mulai" : undefined}><Input type="date" value={f.valid_until} min={f.valid_from || undefined} onChange={(e) => setF({ ...f, valid_until: e.target.value })} /></Field>
          </div>
          {action === "approve" && <p className="-mt-2 text-xs text-on-surface-variant">Kosongkan untuk default: mulai hari ini, berakhir sesuai jenis izin (bulanan 1 bulan, tahunan 1 tahun, sementara 7 hari).</p>}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Nomor stiker"><Input className="font-mono uppercase" value={f.sticker_number} onChange={(e) => setF({ ...f, sticker_number: e.target.value.toUpperCase() })} maxLength={40} placeholder="mis. STK-B1-042" /></Field>
            <Field label="Tarif (Rp)" help={fee != null && Number.isFinite(fee) ? fmtMoney(fee) : "Opsional — tarif khusus izin ini."}><Input inputMode="numeric" value={f.fee_amount} onChange={(e) => setF({ ...f, fee_amount: e.target.value.replace(/[^\d]/g, "") })} placeholder="0" /></Field>
          </div>
          <Field label="Catatan"><Textarea rows={2} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon={action === "approve" ? "check_circle" : "done"} className={cn(TOUCH)} disabled={!valid} loading={busy} onClick={submit}>{action === "approve" ? "Setujui izin" : "Simpan"}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
