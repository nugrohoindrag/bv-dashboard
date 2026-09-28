// Receivables › log penagihan (PRD P4 v2.1 P4-COL-03): catat kontak (telepon / WhatsApp manual / kunjungan / surat), hasil,
// janji bayar (tanggal & nominal) dan tindak lanjut, tertaut ke invoice terbuka pihak; detail log dengan aksi janji bayar
// (ditepati / ingkar / batal) dan ubah catatan / tindak lanjut. Janji terbuka juga dinilai otomatis oleh sweep server.
import { useState } from "react";
import { Link } from "react-router-dom";
import { Alert, Badge, Button, Checkbox, ConfirmDialog, DatePicker, Dialog, DialogContent, DialogFooter, Drawer, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { KeyValue, RelativeTime, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { useAll, useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { fmtDate, fmtDateTime, fmtMoney } from "@/lib/format";
import { MoneyInput, PartyPicker, PropertySelect, type Party } from "@/features/finance/fin-ui";
import { useAuth } from "@/lib/auth";
import { fmtDay, todayISO } from "@/features/finance/fin-utils";
import { CHANNELS, LOG_ACTIONS, OUTCOMES, channelLabel, outcomeLabel, partyLabel, statementLink, type CollectionLog, type CollectionLogInput, type InvoiceLite } from "./receivables-model";

export function CollectionLogDialog({ propertyId: initialProperty, party, title, defaults, onClose, onSaved }: {
  propertyId: string | null; party?: Party & { label?: string }; title?: string; defaults?: { channel?: string; outcome?: string; invoice_ids?: string[] }; onClose: () => void; onSaved?: (log: CollectionLog) => void;
}) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { properties } = useAuth();
  // property wajib: dari halaman/baris, atau dipilih di dialog (halaman "Semua property")
  const [propertyId, setPropertyId] = useState<string | null>(initialProperty ?? (properties.length === 1 ? properties[0].id : null));
  const [p, setP] = useState<Party>({ tenant_id: party?.tenant_id ?? null, unit_location_id: party?.tenant_id ? null : party?.unit_location_id ?? null });
  const hasParty = !!(p.tenant_id || p.unit_location_id);
  const [f, setF] = useState({ channel: defaults?.channel ?? "phone", outcome: defaults?.outcome ?? "contacted", contact_person: "", notes: "", promise_date: "", promise_amount: null as number | null, follow_up_on: "" });
  const invoices = useAll<InvoiceLite>("invoices", { property_id: propertyId ?? undefined, tenant_id: p.tenant_id ?? undefined, unit_location_id: p.tenant_id ? undefined : p.unit_location_id ?? undefined, open: true }, { enabled: hasParty && !!propertyId });
  const [picked, setPicked] = useState<string[] | null>(null); // null = bawaan: invoice log asal yang masih terbuka, selain itu invoice overdue
  const list = invoices.data ?? [];
  const carried = defaults?.invoice_ids?.length ? list.filter((i) => defaults.invoice_ids!.includes(i.id)).map((i) => i.id) : [];
  const chosen = picked ?? (carried.length ? carried : list.filter((i) => i.status === "overdue").map((i) => i.id));
  const chosenTotal = list.filter((i) => chosen.includes(i.id)).reduce((s, i) => s + i.outstanding_amount, 0);
  const promise = f.outcome === "promise_to_pay";
  const today = todayISO();
  const [busy, setBusy] = useState(false);
  const errors = {
    promise_date: promise && !f.promise_date ? "Tanggal janji wajib" : promise && f.promise_date < today ? "Tidak boleh di masa lalu" : undefined,
  };
  const valid = !!propertyId && hasParty && !errors.promise_date;
  const toggle = (id: string, on: boolean) => setPicked((prev) => { const base = prev ?? chosen; return on ? [...new Set([...base, id])] : base.filter((x) => x !== id); });
  const submit = async () => {
    if (!valid || !propertyId) return;
    setBusy(true);
    const body: CollectionLogInput = {
      property_id: propertyId, tenant_id: p.tenant_id, unit_location_id: p.tenant_id ? null : p.unit_location_id, invoice_ids: chosen, channel: f.channel,
      contact_person: f.contact_person.trim() || null, outcome: f.outcome, notes: f.notes.trim() || null,
      promise_date: promise ? f.promise_date : null, promise_amount: promise ? f.promise_amount : null, follow_up_on: f.follow_up_on || null,
    };
    try {
      const out = await api<CollectionLog>("billing/collection-logs", { body, idempotencyKey: uuid() });
      invalidate("all", "list", "one");
      toast.success(promise ? `Janji bayar ${fmtDay(out.promise_date)} dicatat` : "Log penagihan dicatat");
      onSaved?.(out);
      onClose();
    } catch (e) {
      toast.failed("created", e, "Log penagihan");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={title ?? "Catat kontak / janji bayar"} description={party?.label ? `Pihak: ${party.label}` : "Log penagihan tercatat di riwayat invoice terkait (audit)."}>
        <div className="space-y-4">
          {!party && !initialProperty && properties.length > 1 && (
            <Field label="Property" required>
              <PropertySelect value={propertyId} onChange={(v) => { setPropertyId(v); setP({ tenant_id: null, unit_location_id: null }); setPicked(null); }} className="sm:w-full" />
            </Field>
          )}
          {!party && (
            <Field label="Pihak" required>
              <PartyPicker key={propertyId ?? "none"} propertyId={propertyId} value={p} onChange={(v) => { setP(v); setPicked(null); }} />
            </Field>
          )}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Kanal" required>
              <NativeSelect value={f.channel} onChange={(e) => setF({ ...f, channel: e.target.value })}>{CHANNELS.map((c) => <option key={c.value} value={c.value}>{c.label}</option>)}</NativeSelect>
            </Field>
            <Field label="Hasil" required>
              <NativeSelect value={f.outcome} onChange={(e) => setF({ ...f, outcome: e.target.value })}>{OUTCOMES.map((c) => <option key={c.value} value={c.value}>{c.label}</option>)}</NativeSelect>
            </Field>
            <Field label="Orang yang dihubungi" className="sm:col-span-2">
              <Input value={f.contact_person} onChange={(e) => setF({ ...f, contact_person: e.target.value })} placeholder="mis. Bu Rina (finance tenant)" maxLength={120} />
            </Field>
          </div>
          {promise && (
            <div className="grid grid-cols-1 gap-3 rounded-[var(--radius-md)] border border-border bg-surface-container-low p-3 sm:grid-cols-2">
              <Field label="Tanggal janji bayar" required error={errors.promise_date}>
                <DatePicker value={f.promise_date} min={today} onChange={(v) => setF({ ...f, promise_date: v })} aria-label="Tanggal janji bayar" />
              </Field>
              <Field label="Nominal janji" help={chosenTotal > 0 ? `Invoice terpilih: ${fmtMoney(chosenTotal)}` : "Opsional"}>
                <MoneyInput value={f.promise_amount} onChange={(v) => setF({ ...f, promise_amount: v })} aria-label="Nominal janji" />
              </Field>
              <p className="text-xs text-on-surface-variant sm:col-span-2">Janji otomatis <b>ditepati</b> bila penerimaan sejak dicatat ≥ nominal (atau semua invoice terpilih lunas bila nominal kosong), dan <b>ingkar</b> bila lewat tanggal.</p>
            </div>
          )}
          <Field label="Tindak lanjut pada" help="Muncul di daftar kerja saat jatuh tempo.">
            <DatePicker value={f.follow_up_on} min={today} onChange={(v) => setF({ ...f, follow_up_on: v })} aria-label="Tanggal tindak lanjut" />
          </Field>
          <Field label="Catatan">
            <Textarea rows={3} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} placeholder="Ringkasan percakapan, alasan keterlambatan, kesepakatan…" />
          </Field>
          {hasParty && (
            <div>
              <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Invoice terkait ({chosen.length} dipilih)</div>
              {invoices.isLoading ? (
                <p className="text-sm text-on-surface-variant">Memuat invoice terbuka…</p>
              ) : invoices.isError ? (
                <Alert variant="warning">Invoice terbuka tidak dapat dimuat; log tetap dapat disimpan tanpa tautan invoice.</Alert>
              ) : list.length === 0 ? (
                <p className="text-sm text-on-surface-variant">Tidak ada invoice terbuka untuk pihak ini.</p>
              ) : (
                <ul className="max-h-56 divide-y divide-border overflow-y-auto rounded-[var(--radius-md)] border border-border">
                  {list.map((i) => (
                    <li key={i.id} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
                      <Checkbox checked={chosen.includes(i.id)} onCheckedChange={(v) => toggle(i.id, v)} label={i.invoice_number ?? "Draft"} />
                      <span className="flex items-center gap-2 text-right">
                        <span className="text-xs text-on-surface-variant">jatuh tempo {fmtDate(i.due_at)}</span>
                        <span className="tnum font-semibold">{fmtMoney(i.outstanding_amount)}</span>
                        <StatusBadge objectType="invoice" status={i.status} />
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan log</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Detail log penagihan (deep link `?log=<id>` notifikasi janji ingkar) + aksi janji bayar & ubah catatan/tindak lanjut. */
export function CollectionLogDrawer({ log, onClose, onChanged, onOpenLog }: { log: CollectionLog; onClose: () => void; onChanged?: (log: CollectionLog) => void; onOpenLog?: (id: string) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [v, setV] = useState(log);
  const [confirm, setConfirm] = useState<string | null>(null);
  const [edit, setEdit] = useState(false);
  const [form, setForm] = useState({ notes: log.notes ?? "", follow_up_on: log.follow_up_on ?? "" });
  const [busy, setBusy] = useState(false);
  const [followUp, setFollowUp] = useState(false);
  const patch = async (body: Record<string, unknown>, done: string) => {
    setBusy(true);
    try {
      const out = await api<CollectionLog>(`billing/collection-logs/${v.id}`, { method: "PATCH", body });
      setV(out);
      onChanged?.(out);
      invalidate("all", "list");
      toast.success(done);
      setConfirm(null);
      setEdit(false);
    } catch (e) {
      toast.failed("updated", e, "Log penagihan");
    } finally {
      setBusy(false);
    }
  };
  const acts = Object.entries(LOG_ACTIONS).filter(([k]) => v.allowed_actions.includes(k));
  return (
    <Drawer open onClose={onClose} title="Log penagihan" description={`${partyLabel(v)} · ${channelLabel(v.channel)} · ${outcomeLabel(v.outcome)}`} width={600}>
      <div className="space-y-5" data-testid="collection-log-drawer">
        <div className="flex flex-wrap items-center gap-2">
          <Badge>{channelLabel(v.channel)}</Badge>
          <Badge tone={v.outcome === "promise_to_pay" ? "info" : v.outcome === "dispute" ? "warning" : undefined}>{outcomeLabel(v.outcome)}</Badge>
          {v.promise_status && <StatusBadge objectType="collection_promise" status={v.promise_status} />}
        </div>
        <KeyValue items={[
          { label: "Pihak", value: <>{partyLabel(v)}{v.tenant_name && v.unit_label ? <span className="text-on-surface-variant"> · {v.unit_label}</span> : null}</> },
          { label: "Dihubungi", value: v.contact_person ?? "—" },
          { label: "Janji bayar", value: v.promise_date ? <>{fmtDay(v.promise_date)}{v.promise_amount ? ` · ${fmtMoney(v.promise_amount)}` : ""}</> : "—" },
          { label: "Tindak lanjut", value: v.follow_up_on ? fmtDay(v.follow_up_on) : "—" },
          { label: "Invoice", value: v.invoice_numbers.length ? <span className="flex flex-wrap gap-1.5">{v.invoice_numbers.map((n) => <span key={n} className="font-mono text-[13px]">{n}</span>)}</span> : "—" },
          { label: "Dicatat", value: <><RelativeTime value={v.created_at} /> oleh {v.created_by_name ?? "—"}<span className="block text-xs text-on-surface-variant">{fmtDateTime(v.created_at)}</span></> },
        ]} />
        {v.notes && !edit && <p className="whitespace-pre-line rounded-[var(--radius-md)] bg-surface-container px-3 py-2 text-sm">{v.notes}</p>}
        {edit ? (
          <div className="space-y-3 rounded-[var(--radius-md)] border border-border p-3">
            <Field label="Catatan"><Textarea rows={3} value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} /></Field>
            <Field label="Tindak lanjut pada" help="Kosongkan untuk menghapus tindak lanjut."><DatePicker value={form.follow_up_on} onChange={(d) => setForm({ ...form, follow_up_on: d })} aria-label="Tindak lanjut" /></Field>
            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setEdit(false)}>Batal</Button>
              <Button loading={busy} onClick={() => patch({ action: "update", notes: form.notes.trim(), follow_up_on: form.follow_up_on }, "Log penagihan diperbarui")}>Simpan</Button>
            </div>
          </div>
        ) : null}
        <div className="flex flex-wrap gap-2 border-t border-border pt-4">
          {acts.map(([k, a]) => <Button key={k} variant={a.destructive ? "secondary" : "primary"} icon={a.icon} onClick={() => setConfirm(k)}>{a.label}</Button>)}
          {v.allowed_actions.includes("update") && <Button variant={acts.length ? "secondary" : "primary"} icon="add_call" onClick={() => setFollowUp(true)}>Catat kontak baru</Button>}
          {v.allowed_actions.includes("update") && !edit && <Button variant="secondary" icon="edit" onClick={() => { setForm({ notes: v.notes ?? "", follow_up_on: v.follow_up_on ?? "" }); setEdit(true); }}>Ubah catatan / tindak lanjut</Button>}
          <Link to={statementLink(v.property_id, v)} className="inline-flex h-10 items-center gap-1 rounded-[var(--radius-md)] px-3 text-sm font-semibold text-primary hover:bg-surface-container">Statement</Link>
        </div>
      </div>
      {followUp && (
        <CollectionLogDialog
          propertyId={v.property_id}
          party={{ tenant_id: v.tenant_id, unit_location_id: v.unit_location_id, label: partyLabel(v) }}
          title="Catat kontak lanjutan"
          defaults={{ invoice_ids: v.invoice_ids }}
          onClose={() => setFollowUp(false)}
          onSaved={(out) => onOpenLog?.(out.id)}
        />
      )}
      {confirm && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setConfirm(null)}
          title={LOG_ACTIONS[confirm].label}
          description={confirm === "mark_kept" ? "Tandai janji bayar ini sudah ditepati (mis. pembayaran diterima di luar sistem)." : confirm === "mark_broken" ? "Janji dianggap ingkar; pihak akan diprioritaskan di daftar kerja penagihan." : "Janji bayar dibatalkan (mis. dicatat keliru)."}
          confirmLabel={LOG_ACTIONS[confirm].label}
          destructive={LOG_ACTIONS[confirm].destructive}
          onConfirm={() => void patch({ action: confirm }, LOG_ACTIONS[confirm].done)}
        />
      )}
    </Drawer>
  );
}
