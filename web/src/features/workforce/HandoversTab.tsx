// Tab Serah Terima (PRD P2 v2.1 P2-SHF-03): catatan + snapshot item terbuka saat serah terima (Security: emergency, incident,
// patrol tertunda, finding; Housekeeping: area belum selesai, rework, finding). Diterima oleh penerima shift berikut atau
// supervisor (allowed_actions server). Deep link notifikasi: /{domain}/shifts?tab=handovers&id={id}.
import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Alert, Button, Dialog, DialogContent, DialogFooter, Field, Input, Textarea } from "@/components/ui/primitives";
import { StatusBadge } from "@/components/bv/badges";
import { DataGrid } from "@/components/bv/datagrid";
import { CellText, CellTitle } from "@/components/bv/cells";
import { KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { CardSkeleton, QueryErrorState } from "@/components/bv/states";
import { useInvalidate } from "@/api/hooks";
import { api, type ListResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDate, fmtDateTime } from "@/lib/format";
import { fieldErrorsOf } from "@/lib/problem";
import type { HandoverOpenItem, HandoverSnapshot, ShiftDomain, ShiftHandover } from "@/api/types";
import { PropertyField } from "@/features/security/shared";
import { usePropertyChoice } from "@/features/security/hooks";
import { emergencyAttentionTitle } from "@/features/security/p2";
import { ShiftSelect, StaffPicker } from "./components";
import { HANDOVER_GROUPS, HANDOVER_GROUP_LABEL, SHIFT_DOMAIN_LABEL, isoDay, labelOf } from "./workforce";

const LIMIT = 100;

export function HandoversTab({ domain }: { domain: ShiftDomain }) {
  const { propertyId } = useAuth();
  const [sp, setSp] = useSearchParams();
  const openId = sp.get("id");
  const list = useQuery({
    queryKey: ["handovers", domain, propertyId ?? null],
    queryFn: ({ signal }) => api<ListResponse<ShiftHandover>>(`${domain}/handovers`, { query: { property_id: propertyId ?? undefined, limit: LIMIT }, signal }).then((r) => r.data),
  });
  const [createOpen, setCreateOpen] = useState(false);
  const inList = openId ? list.data?.find((h) => h.id === openId) : undefined;
  // deep link ke serah terima yang tidak ada di daftar terbaru → ambil langsung (GET /{domain}/handovers/{id})
  const single = useQuery({
    queryKey: ["handover", domain, openId],
    queryFn: ({ signal }) => api<ShiftHandover>(`${domain}/handovers/${openId}`, { signal }),
    enabled: !!openId && !!list.data && !inList,
    retry: false,
  });
  const selected = inList ?? single.data;
  const setOpen = (id: string | null) => { const n = new URLSearchParams(sp); if (id) n.set("id", id); else n.delete("id"); setSp(n, { replace: true }); };
  const columns = useMemo<ColumnDef<ShiftHandover, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026): nomor (kecil) di atas shift + tanggal; pos, catatan & snapshot di drawer detail.
      { id: "number", header: "Serah terima", meta: { mobile: "primary" }, cell: ({ row: { original: h } }) => <CellTitle code={h.handover_number} title={[h.shift_name ?? "Tanpa shift", h.shift_date ? fmtDate(h.shift_date) : null].filter(Boolean).join(" · ")} /> },
      { id: "from", header: "Diserahkan", meta: { mobile: "secondary" }, cell: ({ row: { original: h } }) => <CellText max={160}>{h.handed_over_by_name}</CellText>, size: 160 },
      { id: "at", header: "Waktu", meta: { mobile: "hidden" }, cell: ({ row: { original: h } }) => <span className="whitespace-nowrap text-sm" title={fmtDateTime(h.handed_over_at)}><RelativeTime value={h.handed_over_at} /></span>, size: 130 },
      { id: "to", header: "Penerima", meta: { mobile: "secondary" }, cell: ({ row: { original: h } }) => (h.received_by_name ? <CellText max={160}>{h.received_by_name}</CellText> : <CellText max={160} muted>Shift berikut</CellText>), size: 160 },
      { id: "open", header: "Item terbuka", meta: { mobile: "hidden" }, cell: ({ row: { original: h } }) => <CellText max={200} className="tnum">{openItemsText(h.open_items, domain)}</CellText>, size: 200 },
      { id: "status", header: "Status", meta: { mobile: "status" }, size: 170, cell: ({ row: { original: h } }) => <StatusBadge objectType="shift_handover" status={h.status} /> },
    ],
    [domain],
  );
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-on-surface-variant">Serah terima menyimpan snapshot item terbuka saat dibuat, sehingga shift berikut melihat kondisi persis saat itu.</p>
        <Button icon="assignment_return" onClick={() => setCreateOpen(true)}>Buat serah terima</Button>
      </div>
      {openId && list.data && !selected && single.isError && <Alert variant="warning" title="Serah terima tidak ditemukan">Serah terima tidak ada atau Anda tidak memiliki akses.</Alert>}
      <DataGrid
        columns={columns}
        rows={list.data ?? []}
        rowId={(r) => r.id}
        onRowClick={(r) => { setOpen(r.id); }}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        rowClassName={(r) => (r.id === openId ? "bg-primary-soft" : undefined)}
        empty={{ icon: "assignment_return", title: "Belum ada serah terima", description: `Serah terima shift ${SHIFT_DOMAIN_LABEL[domain]} dibuat petugas di akhir shift (Web atau Staff App).`, action: <Button icon="add" onClick={() => setCreateOpen(true)}>Buat serah terima</Button> }}
      />
      {selected && <HandoverDetail domain={domain} handover={selected} onClose={() => setOpen(null)} />}
      {createOpen && <CreateHandoverDialog domain={domain} onClose={() => setCreateOpen(false)} onCreated={(h) => { setCreateOpen(false); setOpen(h.id); }} />}
    </div>
  );
}

function openItemsText(snap: HandoverSnapshot | null | undefined, domain: ShiftDomain): string {
  const counts = snap?.counts ?? {};
  const parts = HANDOVER_GROUPS[domain].filter((g) => (counts[g] ?? 0) > 0).map((g) => `${counts[g]} ${labelOf(HANDOVER_GROUP_LABEL, g).toLowerCase()}`);
  return parts.join(" · ") || "Tidak ada";
}

function OpenItemGroups({ snap, domain }: { snap: HandoverSnapshot | null | undefined; domain: ShiftDomain }) {
  const groups = HANDOVER_GROUPS[domain].filter((g) => (snap?.groups?.[g] ?? []).length > 0);
  if (!groups.length) return <p className="text-sm text-on-surface-variant">Tidak ada item terbuka saat serah terima.</p>;
  return (
    <div className="space-y-3">
      {groups.map((g) => (
        <section key={g} aria-label={labelOf(HANDOVER_GROUP_LABEL, g)}>
          <h4 className="mb-1 text-label font-semibold uppercase tracking-wide text-on-surface-variant">{labelOf(HANDOVER_GROUP_LABEL, g)} ({snap!.groups[g].length})</h4>
          <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
            {snap!.groups[g].map((it: HandoverOpenItem) => (
              <li key={it.object_type + it.id} className="px-3 py-2 text-sm">
                <Link to={it.deep_link} className="hover:underline"><span className="font-mono text-[13px] font-semibold">{it.number}</span> {it.object_type === "emergency_alert" ? emergencyAttentionTitle(it.title) : it.title}</Link>
                <div className="text-xs text-on-surface-variant">{[it.location, it.due_at ? `due ${fmtDateTime(it.due_at)}` : null, it.status.replace(/_/g, " ")].filter(Boolean).join(" · ")}</div>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

function HandoverDetail({ domain, handover: h, onClose }: { domain: ShiftDomain; handover: ShiftHandover; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [busy, setBusy] = useState(false);
  const acknowledge = async () => {
    setBusy(true);
    try {
      await api(`${domain}/handovers/${h.id}/acknowledge`, { body: {} });
      invalidate("handovers");
      toast.action("acknowledged", `Serah terima ${h.handover_number}`);
    } catch (err) {
      toast.failed("acknowledged", err, "Serah terima");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={`Serah terima ${h.handover_number}`} description={`${SHIFT_DOMAIN_LABEL[domain]} · ${fmtDateTime(h.handed_over_at)}`}>
        <div className="space-y-4">
          <StatusBadge objectType="shift_handover" status={h.status} />
          <KeyValue items={[
            { label: "Shift", value: h.shift_name ? `${h.shift_name}${h.shift_date ? ` · ${fmtDate(h.shift_date)}` : ""}` : "—" },
            { label: domain === "security" ? "Pos" : "Zona", value: h.post },
            { label: "Diserahkan oleh", value: h.handed_over_by_name },
            { label: "Penerima", value: h.received_by_name ?? "Shift berikut (siapa pun yang menerima)" },
            { label: "Diterima", value: h.acknowledged_at ? fmtDateTime(h.acknowledged_at) : "Belum" },
          ]} />
          <section>
            <h4 className="mb-1 text-label font-semibold uppercase tracking-wide text-on-surface-variant">Catatan</h4>
            <p className="whitespace-pre-line rounded-[var(--radius-md)] bg-surface-container-low px-3 py-2 text-body">{h.notes}</p>
          </section>
          <OpenItemGroups snap={h.open_items} domain={domain} />
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Tutup</Button>
          {h.allowed_actions.includes("acknowledge") && <Button icon="mark_email_read" loading={busy} onClick={() => void acknowledge()}>Terima serah terima</Button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CreateHandoverDialog({ domain, onClose, onCreated }: { domain: ShiftDomain; onClose: () => void; onCreated: (h: ShiftHandover) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [pid, setPid] = usePropertyChoice();
  const draft = useQuery({
    queryKey: ["handover-draft", domain, pid],
    enabled: !!pid,
    queryFn: ({ signal }) => api<HandoverSnapshot>(`${domain}/handovers/draft`, { query: { property_id: pid }, signal }),
  });
  const [f, setF] = useState({ shift_id: "", shift_date: isoDay(new Date()), received_by: null as string | null, post: "", notes: "" });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!f.notes.trim()) return setErrors({ notes: "Catatan serah terima wajib" });
    if (!pid) return setErrors({ property_id: "Pilih property" });
    setBusy(true);
    try {
      const h = await api<ShiftHandover>(`${domain}/handovers`, { body: { property_id: pid, shift_id: f.shift_id || null, shift_date: f.shift_date || null, received_by: f.received_by, post: f.post.trim() || null, notes: f.notes.trim() } });
      invalidate("handovers");
      toast.action("submitted", `Serah terima ${h.handover_number}`);
      onCreated(h);
    } catch (err) {
      setErrors(fieldErrorsOf(err));
      toast.failed("submitted", err, "Serah terima");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={`Serah terima shift ${SHIFT_DOMAIN_LABEL[domain]}`} description="Item terbuka di bawah disimpan sebagai snapshot saat serah terima dikirim.">
        <div className="space-y-4">
          <PropertyField value={pid} onChange={(v) => { setPid(v); setF((s) => ({ ...s, shift_id: "", received_by: null })); }} />
          {errors.property_id && <p className="text-sm text-on-error-container">{errors.property_id}</p>}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Shift" error={errors.shift_id}><ShiftSelect domain={domain} propertyId={pid} value={f.shift_id} onChange={(v) => setF({ ...f, shift_id: v })} /></Field>
            <Field label="Tanggal shift" error={errors.shift_date}><Input type="date" value={f.shift_date} onChange={(e) => setF({ ...f, shift_date: e.target.value })} /></Field>
            <Field label="Penerima (opsional)" help="Kosong = siapa pun di shift berikut dapat menerima" error={errors.received_by}><StaffPicker domain={domain} propertyId={pid} value={f.received_by} onChange={(v) => setF({ ...f, received_by: v })} /></Field>
            <Field label={domain === "security" ? "Pos jaga" : "Zona"}><Input value={f.post} onChange={(e) => setF({ ...f, post: e.target.value })} /></Field>
          </div>
          <Field label="Catatan serah terima" required error={errors.notes}><Textarea rows={4} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} placeholder={domain === "security" ? "Kondisi pos, kejadian penting, instruksi untuk shift berikut…" : "Area yang belum selesai, kendala, instruksi untuk shift berikut…"} /></Field>
          <section aria-label="Item terbuka">
            <h4 className="mb-2 text-h3 font-bold">Item terbuka saat ini</h4>
            {draft.isLoading ? <CardSkeleton lines={3} /> : draft.error ? <QueryErrorState error={draft.error} onRetry={() => draft.refetch()} compact /> : <OpenItemGroups snap={draft.data} domain={domain} />}
          </section>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="send" loading={busy} onClick={() => void submit()}>Kirim serah terima</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
