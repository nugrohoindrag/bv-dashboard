// Security › Visitors (PRD P1 v1.3 §22, WF-P1-005, AT-P1-011): daftar tamu hari ini, verifikasi kode/QR pass, check-in/out,
// approval (OD-P1-008), registrasi walk-in oleh Security/Reception.
import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Button, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { KeyValue, useToast } from "@/components/bv/common";
import { useAction, useAll, useList } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import type { Tenant } from "@/api/types";
import { TreeView } from "@/features/property/LocationsPage";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { statusLabel, statusOptions } from "@/lib/status";
import { visitorDate } from "@/lib/drilldown";

export interface Visitor {
  id: string; visitor_number: string; property_id: string; tenant_user_id: string | null; tenant_id: string | null; tenant_name: string | null; host_name: string | null; host_unit_location_id: string | null; host_unit_label: string | null;
  visitor_name: string; visitor_phone: string | null; visitor_company: string | null; id_number_masked: string | null; purpose: string | null; vehicle_plate: string | null; headcount: number; expected_at: string; expected_until: string | null;
  status: string; channel: string; approved_at: string | null; denied_reason: string | null; verified_by_name: string | null; checked_in_at: string | null; checked_out_at: string | null; checkin_note: string | null;
  pass?: { pass_code: string; qr_payload: string; valid_from: string; valid_until: string; status: string } | null; allowed_actions: string[]; version: number;
}
const ACTION_LABEL: Record<string, string> = { approve: "Setujui", deny: "Tolak…", check_in: "Check-in", check_out: "Check-out", cancel: "Batalkan" };

export default function VisitorsPage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const toast = useToast();
  // drill-down Security Dashboard "Visitor Volume": ?date=today | YYYY-MM-DD (default hari ini, kalender lokal)
  const [sp] = useSearchParams();
  const [date, setDate] = useState(() => visitorDate(sp.get("date")));
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const [code, setCode] = useState("");
  const [resolved, setResolved] = useState<Visitor | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [denyFor, setDenyFor] = useState<Visitor | null>(null);
  const [reason, setReason] = useState("");
  const list = useList<Visitor>("visitors", { property_id: propertyId ?? undefined, date: date || undefined, status: status || undefined, q: q || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const act = useAction<{ id: string; action: string; reason?: string; note?: string }, Visitor>((i) => `visitors/${i.id}/${i.action}`, { body: (i) => ({ reason: i.reason, note: i.note }), invalidate: ["list", "all"] });
  const run = (v: Visitor, action: string) => {
    if (action === "deny") return setDenyFor(v);
    act.mutateAsync({ id: v.id, action }).then((r) => { toast.success(`${r.visitor_name}: ${statusLabel("visitor", r.status)}`); setResolved(null); }).catch(toast.error);
  };
  const resolve = () => api<Visitor>("visitors/resolve", { query: { code: code.trim() } }).then(setResolved).catch(toast.error);
  const columns = useMemo<ColumnDef<Visitor, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): no. + nama tamu satu kolom (perusahaan · telepon · plat di tooltip), host · unit satu
    // baris, tujuan dipotong "…". Identitas, kendaraan & pass tampil saat verifikasi kode pass.
    { id: "visitor", header: "Tamu", meta: { mobile: "primary" }, cell: ({ row: { original: v } }) => <div title={[v.visitor_company, v.visitor_phone, v.vehicle_plate].filter(Boolean).join(" · ") || undefined}><CellTitle code={v.visitor_number} title={`${v.visitor_name}${v.headcount > 1 ? ` (+${v.headcount - 1})` : ""}`} /></div> },
    { id: "host", header: "Host", meta: { mobile: "secondary" }, cell: ({ row: { original: v } }) => <CellText max={180}>{[v.host_name, v.host_unit_label ?? v.tenant_name].filter(Boolean).join(" · ") || "—"}</CellText> },
    { id: "expected_at", header: "Rencana", meta: { mobile: "secondary" }, cell: ({ row }) => <span className="tnum whitespace-nowrap text-sm">{fmtDateTime(row.original.expected_at)}</span>, size: 160 },
    { id: "purpose", header: "Tujuan", meta: { mobile: "hidden" }, cell: ({ row }) => <CellText max={160}>{row.original.purpose ?? "—"}</CellText> },
    { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <StatusBadge objectType="visitor" status={row.original.status} />, size: 160 },
    { id: "checked_in_at", header: "Masuk / keluar", meta: { mobile: "hidden" }, cell: ({ row }) => <span className="tnum whitespace-nowrap text-xs text-muted-foreground">{row.original.checked_in_at ? new Date(row.original.checked_in_at).toLocaleTimeString("id-ID", { hour: "2-digit", minute: "2-digit" }) : "—"} / {row.original.checked_out_at ? new Date(row.original.checked_out_at).toLocaleTimeString("id-ID", { hour: "2-digit", minute: "2-digit" }) : "—"}</span>, size: 130 },
  ], [t]);
  return (
    <div>
      <PageHeader title="Visitors" subtitle="Registrasi tamu (Tenant App / walk-in) → verifikasi Security → masuk → keluar." actions={can("security.visitors.create") && <Button onClick={() => setCreateOpen(true)} disabled={!propertyId}><Icon name="person_add" size={16} /> Registrasi Walk-in</Button>}>
        <div className="flex flex-wrap items-center gap-2">
          <form className="flex items-center gap-2" onSubmit={(e) => { e.preventDefault(); if (code.trim()) resolve(); }}>
            <Input className="w-64" placeholder="Scan / ketik kode pass tamu…" value={code} onChange={(e) => setCode(e.target.value)} />
            <Button type="submit" variant="secondary" icon="qr_code_scanner">Verifikasi</Button>
          </form>
          <Input type="date" className="w-40" value={date} onChange={(e) => setDate(e.target.value)} />
          <NativeSelect className="w-48" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option>{statusOptions("visitor").map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect>
          <Input className="w-56" placeholder="Cari nama / plat / host…" value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
      </PageHeader>
      {resolved && (
        <Alert variant={resolved.status === "registered" ? "success" : "warning"} className="mb-4" title={`${resolved.visitor_number} · ${resolved.visitor_name} — ${statusLabel("visitor", resolved.status)}`}
          action={<div className="flex gap-2">{resolved.allowed_actions.filter((a) => a !== "view").map((a) => <Button key={a} size="sm" variant={a === "deny" || a === "cancel" ? "secondary" : "primary"} onClick={() => run(resolved, a)}>{ACTION_LABEL[a] ?? a}</Button>)}<Button size="sm" variant="ghost" onClick={() => setResolved(null)}>Tutup</Button></div>}>
          <KeyValue items={[{ label: "Host", value: `${resolved.host_name ?? "—"} · ${resolved.host_unit_label ?? resolved.tenant_name ?? ""}` }, { label: "Rencana", value: fmtDateTime(resolved.expected_at) }, { label: "Tujuan", value: resolved.purpose ?? "—" }, { label: "Identitas", value: resolved.id_number_masked ?? "—" }, { label: "Kendaraan", value: resolved.vehicle_plate ?? "—" }, { label: "Pass", value: resolved.pass ? `${resolved.pass.status} · berlaku s/d ${fmtDateTime(resolved.pass.valid_until)}` : "—" }]} />
        </Alert>
      )}
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} loading={list.isLoading} error={list.error} onRetry={() => list.refetch()} isFiltered={!!q || !!status} empty={{ message: "Tidak ada tamu pada tanggal ini." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage}
        rowActions={(r) => r.allowed_actions.filter((a) => a !== "view").map((a) => ({ label: ACTION_LABEL[a] ?? a, destructive: a === "deny" || a === "cancel", onSelect: () => run(r, a) }))} />
      {createOpen && propertyId && <CreateVisitorDialog propertyId={propertyId} onClose={() => setCreateOpen(false)} />}
      {denyFor && (
        <Dialog open onOpenChange={(o) => !o && setDenyFor(null)}>
          <DialogContent title={`Tolak tamu ${denyFor.visitor_name}`}>
            <Field label="Alasan" required><Textarea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} /></Field>
            <DialogFooter><Button variant="secondary" onClick={() => setDenyFor(null)}>Batal</Button><Button variant="destructive" disabled={!reason.trim()} loading={act.isPending} onClick={() => act.mutateAsync({ id: denyFor.id, action: "deny", reason: reason.trim() }).then(() => { setDenyFor(null); setReason(""); }).catch(toast.error)}>Tolak</Button></DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function CreateVisitorDialog({ propertyId, onClose }: { propertyId: string; onClose: () => void }) {
  const toast = useToast();
  const tenants = useAll<Tenant>("tenants", { property_id: propertyId });
  const [f, setF] = useState({ visitor_name: "", visitor_phone: "", visitor_company: "", id_number: "", purpose: "", vehicle_plate: "", headcount: "1", expected_at: new Date(Date.now() + 5 * 60_000).toISOString().slice(0, 16), tenant_id: "", host_name: "", host_unit_location_id: null as string | null });
  const create = useAction<Record<string, unknown>, Visitor>(() => "visitors", { invalidate: ["list", "all"] });
  const submit = () => create.mutateAsync({ property_id: propertyId, visitor_name: f.visitor_name.trim(), visitor_phone: f.visitor_phone || null, visitor_company: f.visitor_company || null, id_number: f.id_number || null, purpose: f.purpose || null, vehicle_plate: f.vehicle_plate || null, headcount: Number(f.headcount) || 1, expected_at: new Date(f.expected_at).toISOString(), tenant_id: f.tenant_id || null, host_name: f.host_name || null, host_unit_location_id: f.host_unit_location_id })
    .then((v) => { toast.success(`Tamu ${v.visitor_number} terdaftar${v.pass ? ` · pass ${v.pass.pass_code}` : ""}`); onClose(); }).catch(toast.error);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Registrasi Tamu Walk-in">
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Nama tamu" required><Input value={f.visitor_name} onChange={(e) => setF({ ...f, visitor_name: e.target.value })} /></Field>
            <Field label="Telepon"><Input value={f.visitor_phone} onChange={(e) => setF({ ...f, visitor_phone: e.target.value })} /></Field>
            <Field label="Perusahaan"><Input value={f.visitor_company} onChange={(e) => setF({ ...f, visitor_company: e.target.value })} /></Field>
            <Field label="No. identitas (disimpan 4 digit terakhir)"><Input value={f.id_number} onChange={(e) => setF({ ...f, id_number: e.target.value })} /></Field>
            <Field label="Plat kendaraan"><Input value={f.vehicle_plate} onChange={(e) => setF({ ...f, vehicle_plate: e.target.value })} /></Field>
            <Field label="Jumlah orang"><Input type="number" value={f.headcount} onChange={(e) => setF({ ...f, headcount: e.target.value })} /></Field>
            <Field label="Waktu kedatangan" required><Input type="datetime-local" value={f.expected_at} onChange={(e) => setF({ ...f, expected_at: e.target.value })} /></Field>
            <Field label="Tujuan"><Input value={f.purpose} onChange={(e) => setF({ ...f, purpose: e.target.value })} /></Field>
            <Field label="Tenant tujuan"><NativeSelect value={f.tenant_id} onChange={(e) => setF({ ...f, tenant_id: e.target.value })}><option value="">—</option>{(tenants.data ?? []).map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}</NativeSelect></Field>
            <Field label="Nama host"><Input value={f.host_name} onChange={(e) => setF({ ...f, host_name: e.target.value })} /></Field>
          </div>
          <Field label="Unit tujuan"><div className="max-h-40 overflow-y-auto rounded-[var(--radius-md)] border border-border p-1"><TreeView propertyId={propertyId} selected={f.host_unit_location_id} onSelect={(n) => setF({ ...f, host_unit_location_id: n.id })} allowTypes={["unit", "space", "area"]} /></div></Field>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>Batal</Button><Button loading={create.isPending} disabled={!f.visitor_name.trim() || !f.expected_at} onClick={submit}>Daftarkan</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
