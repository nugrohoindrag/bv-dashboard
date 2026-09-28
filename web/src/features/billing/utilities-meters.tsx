// Utilities › Meter (PRD P4 v2.1 P4-UTL-01..02): meter listrik & air per unit/area (nomor, pengali, angka awal, tarif, status),
// filter "belum dicatat periode ini", catat pembacaan (angka, waktu, catatan, foto opsional → attachment meter_reading setelah
// pembacaan dibuat) dengan pratinjau pemakaian & peringatan angka mundur; detail meter + riwayat pembacaan.
import { useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { Alert, Badge, Button, DatePicker, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, SearchInput, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { LocationPicker } from "@/components/bv/pickers";
import { CellText, CellTitle } from "@/components/bv/cells";
import { uploadAttachment, useAll, useInvalidate, useList } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { CLEAR_ID } from "@/lib/clearable";
import { statusLabel } from "@/lib/status";
import { fmtDateTime } from "@/lib/format";
import { PropertySelect } from "@/features/finance/fin-ui";
import { fmtQty, localDateTimeInput, periodLabel, usePropertyParam } from "@/features/finance/fin-utils";
import { ANOMALY, METER_STATUS, METER_TYPES, estimateUsage, meterTypeLabel, parseDecimal, type Meter, type Reading, type Tariff } from "./utilities-model";

export function MetersTab() {
  const nav = useNavigate();
  const { can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const [pid, setPid] = usePropertyParam();
  const q = sp.get("q") ?? "";
  const meterType = sp.get("meter_type") ?? "";
  const status = sp.get("status") ?? "active";
  const unread = sp.get("unread") === "true";
  const locationId = sp.get("location_id") || null;
  const [text, setText] = useState(q);
  const set = (patch: Record<string, string | null>) => {
    const n = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (v) n.set(k, v);
      else n.delete(k);
    }
    setSp(n, { replace: true });
  };
  const list = useList<Meter>("billing/meters", { property_id: pid ?? undefined, q: q || undefined, meter_type: meterType || undefined, status, unread: unread || undefined, location_id: locationId ?? undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const canManage = can("billing.meters.manage", pid ?? undefined);
  const canRecord = can("billing.meter_readings.create", pid ?? undefined);
  const [edit, setEdit] = useState<Meter | "new" | null>(null);
  const [record, setRecord] = useState<Meter | null>(null);
  const [detail, setDetail] = useState<Meter | null>(null);
  const filtered = !!q || !!meterType || status !== "active" || unread || !!locationId;
  const columns = useMemo<ColumnDef<Meter, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): nomor meter di atas jenis, lokasi satu baris (path lengkap & tenant di tooltip), tarif,
    // pembacaan terakhir satu baris (periode, pemakaian & status pembacaan di tooltip), status meter + satu flag "Belum dicatat".
    // Pengali, tenant, angka awal & riwayat ada di drawer detail meter.
    { id: "number", header: "Meter", size: 190, meta: { mobile: "primary" }, cell: ({ row: { original: m } }) => <CellTitle code={m.meter_number} title={`${meterTypeLabel(m.meter_type)}${m.multiplier !== 1 ? ` · ×${fmtQty(m.multiplier)}` : ""}`} /> },
    { id: "location", header: "Lokasi", meta: { mobile: "secondary" }, cell: ({ row: { original: m } }) => <CellText max={180} title={[m.location_path, m.tenant_name].filter(Boolean).join(" · ")}>{m.unit_number ? `Unit ${m.unit_number}` : m.location_name}</CellText> },
    { id: "tariff", header: "Tarif", size: 170, meta: { mobile: "hidden" }, cell: ({ row: { original: m } }) => <CellText max={160} muted={!m.tariff_name}>{m.tariff_name ?? "Tarif billing rule"}</CellText> },
    {
      id: "last", header: "Pembacaan terakhir", size: 170, meta: { mobile: "secondary" },
      cell: ({ row: { original: m } }) => m.last_reading
        ? <span className="whitespace-nowrap tnum text-sm font-semibold" title={`${periodLabel(m.last_reading.period)}${m.last_reading.usage !== null ? ` · +${fmtQty(m.last_reading.usage)} ${m.unit_label}` : ""} · ${statusLabel("meter_reading", m.last_reading.status)}`}>{fmtQty(m.last_reading.value)} {m.unit_label}</span>
        : <span className="whitespace-nowrap text-sm text-on-surface-variant">Angka awal {fmtQty(m.initial_reading)}</span>,
    },
    {
      id: "status", header: "Status", size: 170, meta: { mobile: "status" },
      cell: ({ row: { original: m } }) => (
        <div className="flex flex-wrap items-center gap-1">
          <Badge tone={METER_STATUS[m.status]?.tone}>{METER_STATUS[m.status]?.label ?? m.status}</Badge>
          {m.status === "active" && !m.read_this_period && <Badge tone="warning">Belum dicatat</Badge>}
        </div>
      ),
    },
    {
      id: "actions", header: "", size: 110,
      cell: ({ row: { original: m } }) => (canRecord && m.status === "active" ? <span onClick={(e) => e.stopPropagation()}><Button size="sm" variant="secondary" icon="edit_note" onClick={() => setRecord(m)}>Catat</Button></span> : null),
    },
  ], [canRecord]);
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <PropertySelect value={pid} onChange={(id) => setPid(id, { location_id: null })} allowAll />
        <form className="w-full sm:w-64" onSubmit={(e) => { e.preventDefault(); set({ q: text.trim() || null }); }}>
          <SearchInput placeholder="Cari nomor meter / unit / lokasi…" value={text} onChange={(e) => setText(e.target.value)} aria-label="Cari meter" />
        </form>
        <NativeSelect className="w-[calc(50%-4px)] sm:w-36" value={meterType} onChange={(e) => set({ meter_type: e.target.value || null })} aria-label="Jenis meter">
          <option value="">Jenis: Semua</option>
          {Object.entries(METER_TYPES).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}
        </NativeSelect>
        <NativeSelect className="w-[calc(50%-4px)] sm:w-36" value={status} onChange={(e) => set({ status: e.target.value === "active" ? null : e.target.value })} aria-label="Status meter">
          {Object.entries(METER_STATUS).map(([k, v]) => <option key={k} value={k}>Status: {v.label}</option>)}
        </NativeSelect>
        {pid && <LocationPicker propertyId={pid} allowTypes={["building", "tower", "floor", "area"]} value={locationId} onChange={(id) => set({ location_id: id })} placeholder="Gedung / lantai: semua" className="w-full sm:w-56" />}
        <FilterChip selected={unread} onClick={() => set({ unread: unread ? null : "true" })}>Belum dicatat periode ini</FilterChip>
        {filtered && <Button variant="ghost" size="sm" icon="replay" onClick={() => { setText(""); set({ q: null, meter_type: null, status: null, unread: null, location_id: null }); }}>Reset filter</Button>}
        {canManage && <Button icon="add" className="ml-auto" onClick={() => setEdit("new")}>Tambah Meter</Button>}
      </div>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(m) => { setDetail(m); }}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={filtered}
        empty={{ icon: "electric_meter", title: unread ? "Semua meter aktif sudah dicatat periode ini" : "Belum ada meter", description: "Daftarkan meter listrik & air per unit/area; pembacaan bulanan menjadi dasar tagihan utilitas lewat billing rule.", action: canManage && !unread ? <Button icon="add" onClick={() => setEdit("new")}>Tambah Meter</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowActions={(m) => [
          ...(canRecord && m.status === "active" ? [{ label: "Catat pembacaan", icon: "edit_note", onSelect: () => setRecord(m) }] : []),
          ...(canManage ? [{ label: "Edit meter", icon: "edit", onSelect: () => setEdit(m) }] : []),
          { label: "Riwayat pembacaan", icon: "history", onSelect: () => nav(`/billing/meters/readings?meter_id=${m.id}&period=`) },
        ]}
      />
      {edit && <MeterDialog item={edit === "new" ? null : edit} propertyId={pid} onClose={() => setEdit(null)} />}
      {record && <RecordReadingDialog meter={record} onClose={() => setRecord(null)} />}
      {detail && <MeterDrawer meter={detail} onClose={() => setDetail(null)} onRecord={canRecord && detail.status === "active" ? () => setRecord(detail) : undefined} onEdit={canManage ? () => { setEdit(detail); setDetail(null); } : undefined} />}
    </div>
  );
}

// ---------- detail meter ----------
function MeterDrawer({ meter: m, onClose, onRecord, onEdit }: { meter: Meter; onClose: () => void; onRecord?: () => void; onEdit?: () => void }) {
  const nav = useNavigate();
  const readings = useList<Reading>("billing/meter-readings", { meter_id: m.id }, { limit: 12 });
  const rows = readings.data?.pages[0]?.data ?? [];
  return (
    <Drawer open onClose={onClose} title={`Meter ${m.meter_number}`} description={`${meterTypeLabel(m.meter_type)} · ${m.unit_number ? `Unit ${m.unit_number}` : m.location_name}`} width={640}>
      <div className="space-y-5">
        <div className="flex flex-wrap gap-2">
          {onRecord && <Button icon="edit_note" onClick={onRecord}>Catat pembacaan</Button>}
          {onEdit && <Button variant="secondary" icon="edit" onClick={onEdit}>Edit</Button>}
        </div>
        <KeyValue items={[
          { label: "Lokasi", value: m.location_path },
          { label: "Tenant", value: m.tenant_name ?? "—" },
          { label: "Status", value: <Badge tone={METER_STATUS[m.status]?.tone}>{METER_STATUS[m.status]?.label ?? m.status}</Badge> },
          { label: "Pengali", value: `×${fmtQty(m.multiplier)}` },
          { label: "Angka awal", value: `${fmtQty(m.initial_reading)} ${m.unit_label}` },
          { label: "Tarif", value: m.tariff_name ?? "Tarif billing rule" },
          { label: "Dipasang", value: m.installed_on ?? "—" },
          { label: "Catatan", value: m.notes ?? "—" },
        ]} />
        <div>
          <div className="mb-2 flex items-center justify-between">
            <div className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Pembacaan terakhir</div>
            <Button size="sm" variant="ghost" onClick={() => nav(`/billing/meters/readings?meter_id=${m.id}&period=`)}>Semua</Button>
          </div>
          {readings.isLoading ? <p className="text-sm text-on-surface-variant">Memuat…</p> : rows.length === 0 ? <p className="text-sm text-on-surface-variant">Belum ada pembacaan.</p> : (
            <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
              {rows.map((r) => (
                <li key={r.id}>
                  <button type="button" className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm hover:bg-surface-container-low" onClick={() => nav(`/billing/meters/readings/${r.id}`)}>
                    <span><span className="font-medium">{periodLabel(r.period)}</span><span className="block text-xs text-on-surface-variant">{fmtDateTime(r.read_at)}</span></span>
                    <span className="flex items-center gap-2 text-right"><span className="tnum">{fmtQty(r.reading_value)}<span className="block text-xs text-on-surface-variant">{r.usage !== null ? `+${fmtQty(r.usage)} ${r.unit_label}` : "—"}</span></span><StatusBadge objectType="meter_reading" status={r.status} /></span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </Drawer>
  );
}

// ---------- tambah / edit meter ----------
function MeterDialog({ item, propertyId, onClose }: { item: Meter | null; propertyId: string | null; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { properties } = useAuth();
  const [pid, setPid] = useState<string | null>(item?.property_id ?? propertyId ?? (properties.length === 1 ? properties[0].id : null));
  const [f, setF] = useState({
    location_id: item?.location_id ?? (null as string | null), meter_type: item?.meter_type ?? "electricity", meter_number: item?.meter_number ?? "", multiplier: String(item?.multiplier ?? 1).replace(".", ","),
    initial_reading: String(item?.initial_reading ?? 0).replace(".", ","), tariff_id: item?.tariff_id ?? "", status: item?.status ?? "active", installed_on: item?.installed_on ?? "", notes: item?.notes ?? "",
  });
  const tariffs = useAll<Tariff>("billing/utility-tariffs", { property_id: pid ?? undefined }, { enabled: !!pid });
  const options = (tariffs.data ?? []).filter((t) => t.meter_type === f.meter_type && (t.is_active || t.id === f.tariff_id));
  const mult = parseDecimal(f.multiplier);
  const init = parseDecimal(f.initial_reading);
  const [busy, setBusy] = useState(false);
  const errors = { multiplier: mult === null || mult <= 0 ? "Harus > 0" : undefined, initial_reading: init === null || init < 0 ? "Harus ≥ 0" : undefined };
  const valid = !!f.meter_number.trim() && !errors.multiplier && !errors.initial_reading && (!!item || (!!pid && !!f.location_id));
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    try {
      if (item) {
        await api(`billing/meters/${item.id}`, {
          method: "PATCH",
          body: { meter_number: f.meter_number.trim(), multiplier: mult, initial_reading: init, tariff_id: f.tariff_id || (item.tariff_id ? CLEAR_ID : null), status: f.status, installed_on: f.installed_on || null, notes: f.notes.trim() },
        });
      } else {
        await api("billing/meters", {
          body: { location_id: f.location_id, meter_type: f.meter_type, meter_number: f.meter_number.trim(), multiplier: mult, initial_reading: init, tariff_id: f.tariff_id || null, installed_on: f.installed_on || null, notes: f.notes.trim() || null },
          idempotencyKey: uuid(),
        });
      }
      invalidate("list", "one", "all");
      toast.action(item ? "saved" : "created", `Meter ${f.meter_number.trim()}`);
      onClose();
    } catch (e) {
      toast.failed(item ? "saved" : "created", e, "Meter");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit meter ${item.meter_number}` : "Tambah Meter"} description={item ? "Lokasi dan jenis meter tidak dapat diubah; ganti meter dengan status Diganti lalu daftarkan meter baru." : undefined}>
        <div className="space-y-4">
          {!item && properties.length > 1 && (
            <Field label="Property" required>
              <PropertySelect value={pid} onChange={(v) => { setPid(v); setF({ ...f, location_id: null, tariff_id: "" }); }} className="sm:w-full" />
            </Field>
          )}
          <Field label="Lokasi (unit / area)" required={!item}>
            {item ? <Input readOnly value={item.location_path} /> : <LocationPicker propertyId={pid} value={f.location_id} onChange={(id) => setF({ ...f, location_id: id })} placeholder="Pilih unit / area…" />}
          </Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Jenis" required>
              <NativeSelect value={f.meter_type} disabled={!!item} onChange={(e) => setF({ ...f, meter_type: e.target.value, tariff_id: "" })}>{Object.entries(METER_TYPES).map(([k, v]) => <option key={k} value={k}>{v.label} ({v.unit})</option>)}</NativeSelect>
            </Field>
            <Field label="Nomor meter" required help="Unik per property.">
              <Input value={f.meter_number} onChange={(e) => setF({ ...f, meter_number: e.target.value })} maxLength={60} autoFocus={!item} className="font-mono" />
            </Field>
            <Field label="Faktor pengali" required error={errors.multiplier} help="Mis. 40 untuk meter CT; 1 untuk meter biasa.">
              <Input inputMode="decimal" value={f.multiplier} onChange={(e) => setF({ ...f, multiplier: e.target.value })} className="text-right tnum" />
            </Field>
            <Field label={`Angka awal (${METER_TYPES[f.meter_type]?.unit ?? ""})`} error={errors.initial_reading} help="Dipakai sebagai angka sebelumnya untuk pembacaan pertama.">
              <Input inputMode="decimal" value={f.initial_reading} onChange={(e) => setF({ ...f, initial_reading: e.target.value })} className="text-right tnum" />
            </Field>
            <Field label="Tarif" className="sm:col-span-2" help={tariffs.isError ? "Tarif tidak dapat dimuat." : "Kosong = tarif dari billing rule pemakaian meter."}>
              <NativeSelect value={f.tariff_id} onChange={(e) => setF({ ...f, tariff_id: e.target.value })} disabled={!pid}>
                <option value="">Pakai tarif billing rule</option>
                {options.map((t) => <option key={t.id} value={t.id}>{t.code} · {t.name}{t.is_active ? "" : " (nonaktif)"}</option>)}
              </NativeSelect>
            </Field>
            {item && (
              <Field label="Status">
                <NativeSelect value={f.status} onChange={(e) => setF({ ...f, status: e.target.value })}>{Object.entries(METER_STATUS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}</NativeSelect>
              </Field>
            )}
            <Field label="Tanggal pasang">
              <DatePicker value={f.installed_on} onChange={(v) => setF({ ...f, installed_on: v })} aria-label="Tanggal pasang" />
            </Field>
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

// ---------- catat pembacaan ----------
export function RecordReadingDialog({ meter: m, onClose }: { meter: Meter; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [value, setValue] = useState("");
  const [readAt, setReadAt] = useState(() => localDateTimeInput());
  const [notes, setNotes] = useState("");
  const [photo, setPhoto] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const v = parseDecimal(value);
  const prev = m.last_reading?.value ?? m.initial_reading;
  const est = v !== null ? estimateUsage(prev, v, m.multiplier) : null;
  const future = !!readAt && new Date(readAt).getTime() > new Date().getTime() + 10 * 60_000;
  const valid = v !== null && v >= 0 && !!readAt && !future;
  const submit = async () => {
    if (!valid || v === null) return;
    setBusy(true);
    try {
      const r = await api<Reading>(`billing/meters/${m.id}/readings`, { body: { reading_value: v, read_at: new Date(readAt).toISOString(), notes: notes.trim() || null }, idempotencyKey: uuid() });
      let photoFailed = false;
      if (photo) {
        try {
          await uploadAttachment(photo, "meter_reading", r.id, "photo");
        } catch (e) {
          photoFailed = true;
          toast.error(e);
        }
      }
      invalidate("list", "one", "all", "attachments", "dashboard");
      if (r.status === "flagged") toast.warning(`Pembacaan ${m.meter_number} dicatat tetapi ditandai "${ANOMALY[r.anomaly ?? ""] ?? "perlu dicek"}" — perlu review.`);
      else toast.success(`Pembacaan ${m.meter_number} dicatat · pemakaian ${fmtQty(r.usage)} ${r.unit_label}${photoFailed ? " (foto gagal diunggah)" : ""}`);
      onClose();
    } catch (e) {
      toast.failed("created", e, "Pembacaan meter");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Catat pembacaan · ${m.meter_number}`} description={`${meterTypeLabel(m.meter_type)} · ${m.unit_number ? `Unit ${m.unit_number}` : m.location_name}${m.multiplier !== 1 ? ` · pengali ×${fmtQty(m.multiplier)}` : ""}`}>
        <div className="space-y-4">
          <div className="rounded-[var(--radius-md)] bg-surface-container px-3 py-2 text-sm">
            Angka sebelumnya: <b className="tnum">{fmtQty(prev)} {m.unit_label}</b>
            {m.last_reading ? <span className="text-on-surface-variant"> · {periodLabel(m.last_reading.period)} (<RelativeTime value={m.last_reading.read_at} />)</span> : <span className="text-on-surface-variant"> · angka awal meter</span>}
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={`Angka meter (${m.unit_label})`} required>
              <Input inputMode="decimal" value={value} onChange={(e) => setValue(e.target.value)} autoFocus className="text-right tnum" placeholder="mis. 12345,6" />
            </Field>
            <Field label="Waktu baca" required error={future ? "Tidak boleh di masa depan" : undefined}>
              <DatePicker type="datetime-local" value={readAt} onChange={setReadAt} aria-label="Waktu baca" />
            </Field>
          </div>
          {est && (est.rollback ? (
            <Alert variant="warning" title="Angka mundur">Angka lebih kecil dari sebelumnya — pembacaan akan ditandai <b>Perlu Dicek</b> (pemakaian 0) sampai direview Finance.</Alert>
          ) : (
            <p className="text-sm">Perkiraan pemakaian: <b className="tnum">{fmtQty(est.usage)} {m.unit_label}</b> <span className="text-on-surface-variant">(lonjakan &gt; 3× rata-rata akan ditandai otomatis)</span></p>
          ))}
          <Field label="Catatan"><Textarea rows={2} value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="mis. meter sulit dibaca, segel rusak" /></Field>
          <div>
            <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Foto meter (opsional)</div>
            <div className="flex flex-wrap items-center gap-2">
              <Button variant="secondary" icon="photo_camera" onClick={() => fileRef.current?.click()}>{photo ? "Ganti foto" : "Pilih foto"}</Button>
              {photo && <span className="flex items-center gap-1 text-sm">{photo.name}<button type="button" aria-label="Hapus foto" className="text-on-surface-variant hover:text-on-surface" onClick={() => setPhoto(null)}><Icon name="close" size={14} /></button></span>}
              <input ref={fileRef} type="file" accept="image/*" capture="environment" hidden onChange={(e) => setPhoto(e.target.files?.[0] ?? null)} aria-label="Foto meter" />
            </div>
            <p className="mt-1 text-xs text-on-surface-variant">Foto dikompres ≤ 500 KB dan dilampirkan ke pembacaan setelah tersimpan.</p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="save" disabled={!valid} loading={busy} onClick={submit}>Simpan pembacaan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

