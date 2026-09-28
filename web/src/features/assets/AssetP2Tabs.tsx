// Asset 360 — PRD P2 v2.1 §5.5–§5.6: tab Health (skor 0–100 dari pengurang yang dapat ditelusuri, NC §14; P2-EQH-02..04),
// Dokumen (manual/warranty/sertifikat/izin/…, masa berlaku & file; P2-DOC-01..03), dan Biaya & Kinerja (biaya per periode,
// parts, vendor, PM compliance; P2-AST-04..06). Downtime/MTTR/MTBF tidak dihitung di P2 (D-P2-02).
import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { Icon } from "@buildingvision/ui";
import { FilterChip } from "@buildingvision/ui/bv";
import { Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Checkbox, ConfirmDialog, DatePicker, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, TBody, TD, TH, THead, TR, Table, Textarea } from "@/components/ui/primitives";
import { StatusBadge, workTypeLabel } from "@/components/bv/badges";
import { RelativeTime, useToast } from "@/components/bv/common";
import { CardSkeleton, EmptyState, ForbiddenState, KpiSkeleton, QueryErrorState } from "@/components/bv/states";
import { uploadAttachment, useInvalidate } from "@/api/hooks";
import { api, type ListResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDate, fmtMoney, fmtNumber } from "@/lib/format";
import { fieldErrorsOf, problemOf } from "@/lib/problem";
import { cn } from "@/lib/utils";
import type { Asset, AssetDocument, AssetHealth, AssetInsight, Attachment } from "@/api/types";
import { ValidityBadge } from "@/features/workforce/components";
import { DOCUMENT_TYPES, documentTypeLabel, internalLink, lastMonths } from "./asset-p2";
import { clearableText } from "@/lib/clearable";

/** Badge health + skor ("Waspada · 82"); status dari server (status map asset_health). */
export function HealthBadge({ status, score, className }: { status?: string | null; score?: number | null; className?: string }) {
  if (!status) return <span className="text-on-surface-variant">—</span>;
  return (
    <span className={cn("inline-flex items-center gap-1.5", className)}>
      <StatusBadge objectType="asset_health" status={status} />
      {score !== null && score !== undefined && <span className="text-sm font-semibold tnum" title="Health score 0–100">{score}</span>}
    </span>
  );
}

// ---------- Health ----------
export function AssetHealthTab({ asset }: { asset: Asset }) {
  const { can } = useAuth();
  const toast = useToast();
  const qc = useQueryClient();
  const invalidate = useInvalidate();
  const q = useQuery({ queryKey: ["asset-health", asset.id], queryFn: ({ signal }) => api<AssetHealth>(`assets/${asset.id}/health`, { signal }) });
  const [busy, setBusy] = useState(false);
  const recompute = async () => {
    setBusy(true);
    try {
      const h = await api<AssetHealth>(`assets/${asset.id}/health/recompute`, { body: {} });
      qc.setQueryData(["asset-health", asset.id], h);
      invalidate("assets", "dashboard", "asset-history");
      toast.success(`Health ${asset.asset_code} dihitung ulang: ${h.score ?? "—"}`);
    } catch (err) {
      toast.failed("updated", err, "Health");
    } finally {
      setBusy(false);
    }
  };
  if (q.isLoading) return <CardSkeleton lines={4} />;
  if (q.error && !q.data) return <QueryErrorState error={q.error} onRetry={() => q.refetch()} />;
  const h = q.data!;
  const deducted = h.factors.reduce((n, f) => n + f.points, 0);
  return (
    <div className="space-y-4">
      <Card railTone={h.status === "critical" ? "error" : h.status === "warning" ? "warning" : undefined}>
        <CardHeader>
          <div className="min-w-0 flex-1 basis-60">
            <CardTitle>Equipment health</CardTitle>
            <CardSubtitle>Healthy 90–100 · Warning 70–89 · Critical &lt; 70 · Offline (nonaktif/decommissioned) · Belum ada data (tanpa riwayat maintenance).</CardSubtitle>
          </div>
          {can("engineering.assets.update") && <Button size="sm" variant="secondary" icon="refresh" loading={busy} onClick={() => void recompute()}>Hitung ulang</Button>}
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap items-end gap-4">
            <div>
              <div className="text-caption text-on-surface-variant">Health score</div>
              <div className="flex items-baseline gap-1"><span className="text-display font-extrabold tnum text-on-surface" data-testid="health-score">{h.score ?? "—"}</span><span className="text-sm text-on-surface-variant">/100</span></div>
            </div>
            <StatusBadge objectType="asset_health" status={h.status} />
            {h.updated_at && <span className="text-xs text-on-surface-variant">Diperbarui <RelativeTime value={h.updated_at} /></span>}
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><div className="min-w-0 flex-1 basis-60"><CardTitle>Faktor pengurang ({h.factors.length})</CardTitle><CardSubtitle>Skor = 100 − pengurang; setiap faktor dapat ditelusuri ke record penyebabnya.</CardSubtitle></div>{h.factors.length > 0 && <span className="text-sm font-semibold tnum text-on-surface-variant">Total {deducted}</span>}</CardHeader>
        <CardContent className="px-0 pb-0">
          {h.status === "offline" ? (
            <p className="px-5 pb-5 text-sm text-on-surface-variant">Asset nonaktif/decommissioned — health tidak dihitung.</p>
          ) : h.status === "unknown" ? (
            <p className="px-5 pb-5 text-sm text-on-surface-variant">Belum ada data maintenance (WO, jadwal Preventive Maintenance, task, finding) untuk asset ini.</p>
          ) : h.factors.length === 0 ? (
            <p className="px-5 pb-5 text-sm text-on-surface-variant">Tidak ada pengurang — kondisi sehat.</p>
          ) : (
            <Table>
              <THead><tr><TH>Faktor</TH><TH className="bv-num">Jumlah</TH><TH className="bv-num">Poin</TH><TH /></tr></THead>
              <TBody>
                {h.factors.map((f) => {
                  const to = internalLink(f.link);
                  return (
                    <TR key={f.code}>
                      <TD className="font-medium"><span className="block max-w-[320px] truncate" title={f.label}>{f.label}</span></TD>
                      <TD className="bv-num tnum whitespace-nowrap">{fmtNumber(f.count)}</TD>
                      <TD className="bv-num tnum whitespace-nowrap font-semibold text-on-error-container">{f.points}</TD>
                      <TD className="text-right">{to && <Link to={to} className="inline-flex items-center gap-0.5 text-sm font-semibold text-primary hover:underline">Telusuri<Icon name="chevron_right" size={14} aria-hidden /></Link>}</TD>
                    </TR>
                  );
                })}
              </TBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// ---------- Dokumen ----------
export function AssetDocumentsTab({ asset }: { asset: Asset }) {
  const { can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const canView = can("engineering.asset_documents.view");
  const q = useQuery({ queryKey: ["asset-documents", asset.id], enabled: canView, queryFn: ({ signal }) => api<ListResponse<AssetDocument>>(`assets/${asset.id}/documents`, { signal }).then((r) => r.data) });
  const [edit, setEdit] = useState<AssetDocument | "new" | null>(null);
  const [del, setDel] = useState<AssetDocument | null>(null);
  const [busy, setBusy] = useState(false);
  if (!canView) return <ForbiddenState compact detail="Dokumen equipment memerlukan izin engineering.asset_documents.view." />;
  const openFile = async (d: AssetDocument) => {
    try {
      const list = await api<ListResponse<Attachment>>("attachments", { query: { object_type: "asset_document", object_id: d.id } }).then((r) => r.data);
      const att = list.find((a) => a.id === d.attachment_id) ?? list[0];
      if (att?.url) window.open(att.url, "_blank", "noopener");
      else toast.warning("File dokumen belum tersedia.");
    } catch (err) {
      toast.error(err);
    }
  };
  const deactivate = async (d: AssetDocument) => {
    setBusy(true);
    try {
      await api(`asset-documents/${d.id}`, { method: "DELETE", ifMatch: d.version });
      invalidate("asset-documents", "assets", "expiring");
      toast.action("updated", `Dokumen ${d.document_code} dinonaktifkan`);
      setDel(null);
    } catch (err) {
      toast.failed("updated", err, "Dokumen");
    } finally {
      setBusy(false);
    }
  };
  const docs = q.data ?? [];
  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1 basis-60">
          <CardTitle>Dokumen equipment ({docs.filter((d) => d.is_active).length})</CardTitle>
          <CardSubtitle>Pengingat H-30, H-7, dan saat kedaluwarsa ke Engineering Supervisor; muncul di Attention Required.</CardSubtitle>
        </div>
        {can("engineering.asset_documents.create") && <Button size="sm" variant="secondary" icon="post_add" onClick={() => setEdit("new")}>Tambah dokumen</Button>}
      </CardHeader>
      <CardContent className="px-0 pb-0">
        {q.isLoading ? (
          <div className="px-5 pb-5"><CardSkeleton lines={3} /></div>
        ) : q.error && !q.data ? (
          <div className="px-5 pb-5"><QueryErrorState error={q.error} onRetry={() => q.refetch()} compact /></div>
        ) : docs.length === 0 ? (
          <div className="px-5 pb-5"><EmptyState compact icon="description" title="Belum ada dokumen" description="Simpan manual, warranty, sertifikat/izin, laporan inspeksi, gambar teknik, atau kontrak beserta masa berlakunya." /></div>
        ) : (
          <Table data-testid="asset-documents">
            <THead><tr><TH>Dokumen</TH><TH>Nomor / penerbit</TH><TH>Berlaku s/d</TH><TH>Status</TH><TH /></tr></THead>
            <TBody>
              {docs.map((d) => (
                <TR key={d.id} className={cn(!d.is_active && "opacity-60")}>
                  <TD><div className="max-w-[260px]"><div className="truncate font-medium" title={d.title}>{d.title}</div><div className="truncate text-xs text-on-surface-variant"><span className="font-mono">{d.document_code}</span> · {d.type_label || documentTypeLabel(d.document_type)}{!d.is_active ? " · nonaktif" : ""}</div></div></TD>
                  <TD className="text-sm"><div className="max-w-[200px]"><div className="truncate" title={d.document_number ?? undefined}>{d.document_number ?? "—"}</div><div className="truncate text-xs text-on-surface-variant">{d.issuer ?? ""}{d.issued_on ? ` · terbit ${fmtDate(d.issued_on)}` : ""}</div></div></TD>
                  <TD className="tnum whitespace-nowrap">{fmtDate(d.expires_on)}</TD>
                  <TD><ValidityBadge status={d.status} days={d.days_to_expire} /></TD>
                  <TD className="text-right">
                    <span className="inline-flex justify-end gap-1 whitespace-nowrap">
                      {d.attachment_id && <Button size="sm" variant="ghost" icon="description" onClick={() => void openFile(d)} aria-label={`Buka file ${d.title}`}>File</Button>}
                      {can("engineering.asset_documents.update") && <Button size="sm" variant="ghost" onClick={() => setEdit(d)}>Ubah</Button>}
                      {can("engineering.asset_documents.delete") && d.is_active && <Button size="sm" variant="ghost" onClick={() => setDel(d)}>Nonaktifkan</Button>}
                    </span>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </CardContent>
      {edit && <DocumentDialog asset={asset} doc={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
      <ConfirmDialog open={!!del} onOpenChange={(o) => !o && setDel(null)} title={`Nonaktifkan ${del?.title ?? "dokumen"}?`} description="Dokumen tidak lagi diingatkan; riwayat & file tetap tertelusur." confirmLabel="Nonaktifkan" destructive loading={busy} onConfirm={() => del && void deactivate(del)} />
    </Card>
  );
}

function DocumentDialog({ asset, doc, onClose }: { asset: Asset; doc: AssetDocument | null; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [f, setF] = useState({ document_type: doc?.document_type ?? "manual", title: doc?.title ?? "", document_number: doc?.document_number ?? "", issuer: doc?.issuer ?? "", issued_on: doc?.issued_on ?? "", expires_on: doc?.expires_on ?? "", notes: doc?.notes ?? "", is_active: doc?.is_active ?? true });
  const [file, setFile] = useState<File | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof f, v: string | boolean) => setF((s) => ({ ...s, [k]: v }));
  const submit = async () => {
    const e: Record<string, string> = {};
    if (!f.title.trim()) e.title = "Judul dokumen wajib";
    if (f.issued_on && f.expires_on && f.expires_on < f.issued_on) e.expires_on = "Tidak boleh sebelum tanggal terbit";
    setErrors(e);
    if (Object.keys(e).length) return;
    setBusy(true);
    try {
      // file diunggah dulu (object_type asset), lalu dirujuk lewat attachment_id — server memindahkannya ke dokumen
      const att = file ? await uploadAttachment(file, "asset", asset.id, "document") : null;
      const body: Record<string, unknown> = {
        document_type: f.document_type, title: f.title.trim(), document_number: clearableText(f.document_number, doc?.document_number), issuer: clearableText(f.issuer, doc?.issuer),
        issued_on: clearableText(f.issued_on, doc?.issued_on), expires_on: clearableText(f.expires_on, doc?.expires_on), notes: clearableText(f.notes, doc?.notes), ...(att ? { attachment_id: att.id } : {}),
      };
      if (doc) await api(`asset-documents/${doc.id}`, { method: "PATCH", body: { ...body, is_active: f.is_active }, ifMatch: doc.version });
      else await api(`assets/${asset.id}/documents`, { body });
      invalidate("asset-documents", "assets", "expiring", "asset-history");
      toast.action(doc ? "updated" : "created", `Dokumen ${body.title as string}`);
      onClose();
    } catch (err) {
      setErrors(fieldErrorsOf(err));
      if (problemOf(err).code === "STALE_VERSION") toast.warning("Dokumen sudah diubah pengguna lain. Tutup lalu buka kembali untuk memuat versi terbaru.");
      else toast.failed(doc ? "updated" : "created", err, "Dokumen");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={doc ? `Ubah ${doc.document_code}` : `Tambah dokumen · ${asset.asset_code}`}>
        <div className="space-y-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Tipe" error={errors.document_type}><NativeSelect value={f.document_type} onChange={(e) => set("document_type", e.target.value)}>{Object.entries(DOCUMENT_TYPES).map(([v, l]) => <option key={v} value={v}>{l}</option>)}</NativeSelect></Field>
            <Field label="Nomor dokumen" error={errors.document_number}><Input value={f.document_number} onChange={(e) => set("document_number", e.target.value)} /></Field>
          </div>
          <Field label="Judul" required error={errors.title}><Input value={f.title} onChange={(e) => set("title", e.target.value)} placeholder="mis. Warranty Chiller 2026, SLO Lift" /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Field label="Penerbit" error={errors.issuer}><Input value={f.issuer} onChange={(e) => set("issuer", e.target.value)} /></Field>
            <Field label="Tanggal terbit" error={errors.issued_on}><Input type="date" value={f.issued_on} onChange={(e) => set("issued_on", e.target.value)} /></Field>
            <Field label="Berlaku sampai" help="Kosong = tanpa masa berlaku" error={errors.expires_on}><Input type="date" value={f.expires_on} onChange={(e) => set("expires_on", e.target.value)} /></Field>
          </div>
          <Field label="Catatan"><Textarea rows={2} value={f.notes} onChange={(e) => set("notes", e.target.value)} /></Field>
          <Field label={doc?.attachment_id ? "Ganti file (PDF/gambar/Office)" : "File (PDF/gambar/Office)"} error={errors.attachment_id}>
            <Input type="file" accept="application/pdf,image/*,.doc,.docx,.xls,.xlsx" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
          </Field>
          {doc && <Checkbox label="Aktif (dokumen nonaktif tidak diingatkan)" checked={f.is_active} onCheckedChange={(v) => set("is_active", v)} />}
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={busy} onClick={() => void submit()}>Simpan</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Biaya & Kinerja ----------
const MONTH_PRESETS = [3, 6, 12];

export function AssetInsightTab({ asset }: { asset: Asset }) {
  const [range, setRange] = useState(() => lastMonths(12));
  const q = useQuery({ queryKey: ["asset-insight", asset.id, range.from, range.to], queryFn: ({ signal }) => api<AssetInsight>(`assets/${asset.id}/insight`, { query: range, signal }) });
  const active = MONTH_PRESETS.find((m) => { const r = lastMonths(m); return r.from === range.from && r.to === range.to; });
  const d = q.data;
  const cur = d?.currency_code ?? "IDR";
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <DatePicker className="w-40" value={range.from} onChange={(v) => v && setRange({ ...range, from: v })} aria-label="Dari tanggal" />
        <span className="text-on-surface-variant">–</span>
        <DatePicker className="w-40" value={range.to} onChange={(v) => v && setRange({ ...range, to: v })} aria-label="Sampai tanggal" />
        {MONTH_PRESETS.map((m) => <FilterChip key={m} selected={active === m} onClick={() => setRange(lastMonths(m))}>{m} bulan</FilterChip>)}
      </div>
      {q.isLoading ? (
        <KpiSkeleton count={4} />
      ) : q.error && !d ? (
        <QueryErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : d ? (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
            <Tile label="Biaya aktual" value={fmtMoney(d.total.actual_cost, cur)} hint={`${fmtNumber(d.total.work_orders)} WO selesai`} />
            <Tile label="Biaya parts" value={fmtMoney(d.total.parts_cost, cur)} />
            <Tile label="Biaya jasa" value={fmtMoney(d.total.service_cost, cur)} />
            <Tile label="Biaya lain" value={fmtMoney(d.total.other_cost, cur)} />
            <Tile label="Kepatuhan Preventive Maintenance" value={d.pm.scheduled ? `${fmtNumber(d.pm.compliance_pct)}%` : "—"} hint={`${d.pm.completed_on_time}/${d.pm.scheduled} tepat waktu · ${d.pm.overdue} overdue · ${d.pm.skipped} dilewati`} tone={!d.pm.scheduled ? undefined : d.pm.compliance_pct >= 90 ? "success" : d.pm.compliance_pct >= 70 ? "warning" : "error"} />
          </div>
          <Card>
            <CardHeader><div className="min-w-0 flex-1 basis-60"><CardTitle>Biaya aktual per bulan</CardTitle><CardSubtitle>Work Order selesai dalam periode (biaya internal, tidak pernah ke tenant).</CardSubtitle></div></CardHeader>
            <CardContent>
              {d.by_month.length === 0 ? <p className="text-sm text-on-surface-variant">Belum ada Work Order selesai pada periode ini.</p> : (
                <>
                  <div className="h-56" data-testid="insight-chart">
                    <ResponsiveContainer width="100%" height="100%">
                      <BarChart data={d.by_month.map((b) => ({ month: b.label, actual_cost: b.actual_cost, work_orders: b.work_orders }))} margin={{ left: 8, right: 8, top: 8 }}>
                        <CartesianGrid vertical={false} stroke="var(--color-border)" />
                        <XAxis dataKey="month" tick={{ fontSize: 11, fill: "var(--color-on-surface-variant)" }} axisLine={{ stroke: "var(--color-border)" }} tickLine={false} />
                        <YAxis tick={{ fontSize: 11, fill: "var(--color-on-surface-variant)" }} axisLine={false} tickLine={false} width={56} tickFormatter={(v: number) => (v >= 1e6 ? `${Math.round(v / 1e6)}jt` : fmtNumber(v))} />
                        <Tooltip cursor={{ fill: "var(--color-surface-container)" }} formatter={(v: number) => [fmtMoney(v, cur), "Biaya aktual"]} contentStyle={{ background: "var(--color-surface)", border: "1px solid var(--color-border)", borderRadius: 8, color: "var(--color-on-surface)" }} />
                        <Bar dataKey="actual_cost" fill="var(--color-primary)" radius={[4, 4, 0, 0]} maxBarSize={24} />
                      </BarChart>
                    </ResponsiveContainer>
                  </div>
                  <Table className="mt-3">
                    <THead><tr><TH>Bulan</TH><TH className="bv-num">WO</TH><TH className="bv-num">Parts</TH><TH className="bv-num">Jasa</TH><TH className="bv-num">Lain</TH><TH className="bv-num">Aktual</TH></tr></THead>
                    <TBody>{d.by_month.map((b) => <TR key={b.key}><TD className="whitespace-nowrap">{b.label}</TD><TD className="bv-num tnum whitespace-nowrap">{b.work_orders}</TD><TD className="bv-num tnum whitespace-nowrap">{fmtMoney(b.parts_cost, cur)}</TD><TD className="bv-num tnum whitespace-nowrap">{fmtMoney(b.service_cost, cur)}</TD><TD className="bv-num tnum whitespace-nowrap">{fmtMoney(b.other_cost, cur)}</TD><TD className="bv-num tnum whitespace-nowrap font-semibold">{fmtMoney(b.actual_cost, cur)}</TD></TR>)}</TBody>
                  </Table>
                </>
              )}
            </CardContent>
          </Card>
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            <Card>
              <CardHeader><CardTitle>Per tipe Work Order</CardTitle></CardHeader>
              <CardContent className="px-0 pb-0">
                {d.by_type.length === 0 ? <p className="px-5 pb-5 text-sm text-on-surface-variant">—</p> : (
                  <Table><THead><tr><TH>Tipe</TH><TH className="bv-num">WO</TH><TH className="bv-num">Biaya aktual</TH></tr></THead>
                    <TBody>{d.by_type.map((b) => <TR key={b.key}><TD className="whitespace-nowrap">{workTypeLabel[b.key] ?? b.label}</TD><TD className="bv-num tnum whitespace-nowrap">{b.work_orders}</TD><TD className="bv-num tnum whitespace-nowrap">{fmtMoney(b.actual_cost, cur)}</TD></TR>)}</TBody>
                  </Table>
                )}
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle>Vendor</CardTitle></CardHeader>
              <CardContent className="px-0 pb-0">
                {d.vendors.length === 0 ? <p className="px-5 pb-5 text-sm text-on-surface-variant">Dikerjakan internal (tanpa vendor) pada periode ini.</p> : (
                  <Table><THead><tr><TH>Vendor</TH><TH className="bv-num">WO</TH><TH className="bv-num">Selesai</TH><TH className="bv-num">Biaya</TH><TH>Terakhir</TH></tr></THead>
                    <TBody>{d.vendors.map((v) => <TR key={v.vendor_id}><TD><Link to={`/vendors/${v.vendor_id}`} className="block max-w-[220px] truncate font-medium hover:underline" title={v.name}>{v.name}</Link></TD><TD className="bv-num tnum whitespace-nowrap">{v.work_orders}</TD><TD className="bv-num tnum whitespace-nowrap">{v.completed}</TD><TD className="bv-num tnum whitespace-nowrap">{fmtMoney(v.total_cost, cur)}</TD><TD className="whitespace-nowrap text-sm">{fmtDate(v.last_at)}</TD></TR>)}</TBody>
                  </Table>
                )}
              </CardContent>
            </Card>
            <Card className="xl:col-span-2">
              <CardHeader><CardTitle>Parts yang dipakai</CardTitle></CardHeader>
              <CardContent className="px-0 pb-0">
                {d.parts.length === 0 ? <p className="px-5 pb-5 text-sm text-on-surface-variant">Belum ada pemakaian spare part pada periode ini.</p> : (
                  <Table><THead><tr><TH>Item</TH><TH className="bv-num">Qty</TH><TH className="bv-num">Biaya</TH><TH>Terakhir dipakai</TH></tr></THead>
                    <TBody>{d.parts.map((p) => <TR key={p.item_id}><TD><Link to={`/inventory?item=${p.item_id}`} className="block max-w-[320px] truncate hover:underline" title={`${p.item_code} ${p.name}`}><span className="font-mono text-xs">{p.item_code}</span> {p.name}</Link></TD><TD className="bv-num tnum whitespace-nowrap">{fmtNumber(p.quantity)} {p.unit ?? ""}</TD><TD className="bv-num tnum whitespace-nowrap">{fmtMoney(p.total_cost, cur)}</TD><TD className="whitespace-nowrap text-sm">{fmtDate(p.last_used_at)}</TD></TR>)}</TBody>
                  </Table>
                )}
              </CardContent>
            </Card>
          </div>
        </>
      ) : null}
    </div>
  );
}

function Tile({ label, value, hint, tone }: { label: string; value: string; hint?: string; tone?: "success" | "warning" | "error" }) {
  return (
    <Card className="p-4" railTone={tone}>
      <div className="text-sm text-on-surface-variant">{label}</div>
      <div className="mt-1 text-h2 font-bold tnum text-on-surface">{value}</div>
      {hint && <div className="mt-0.5 text-caption text-on-surface-variant">{hint}</div>}
    </Card>
  );
}

/** Dipakai AssetListPage: penanda dokumen/warranty kedaluwarsa (≤ 30 hari). */
export function ExpiringDocsBadge({ count }: { count?: number | null }) {
  if (!count) return null;
  return <Badge tone="warning" title="Dokumen/warranty kedaluwarsa atau ≤ 30 hari"><Icon name="description" size={12} aria-hidden />{count} dokumen</Badge>;
}
