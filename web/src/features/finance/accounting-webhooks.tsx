// Finance › Accounting › Webhook keluar (PRD P4 v2.1 P4-INT-04; D-P4-03): endpoint integrasi sistem akuntansi yang berlangganan
// event keuangan (invoice.issued, payment.paid, …) — dikelola di tingkat organization (grant billing.accounting.* tanpa scope
// property). Secret hanya tampil sekali (saat dibuat / rotasi); tanda tangan HMAC-SHA256; tes kirim; riwayat pengiriman & retry.
import { useMemo, useState } from "react";
import type { ColumnDef } from "@tanstack/react-table";
import { FilterChip } from "@buildingvision/ui/bv";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Checkbox, ConfirmDialog, Dialog, DialogContent, DialogFooter, Drawer, Field, Input } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { RelativeTime, useToast } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { useAll, useInvalidate, useList } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime, fmtNumber } from "@/lib/format";
import { statusOptions } from "@/lib/status";
import { CopyField } from "./fin-ui";
import { hasOrgWide } from "./fin-utils";
import { FINANCE_EVENTS, eventLabel, webhookUrlError, webhookUrlWarning, type WebhookDelivery, type WebhookEndpoint } from "./finance-model";

export function WebhooksTab() {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { principal } = useAuth();
  const orgView = hasOrgWide(principal, "billing.accounting.view");
  const orgManage = hasOrgWide(principal, "billing.accounting.manage");
  const list = useAll<WebhookEndpoint>("finance/webhooks", {}, { enabled: orgView });
  const [edit, setEdit] = useState<WebhookEndpoint | "new" | null>(null);
  const [secret, setSecret] = useState<{ name: string; secret: string } | null>(null);
  const [deliveries, setDeliveries] = useState<WebhookEndpoint | null>(null);
  const [confirm, setConfirm] = useState<{ kind: "delete" | "rotate"; ep: WebhookEndpoint } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const test = async (ep: WebhookEndpoint) => {
    setBusy(ep.id);
    try {
      const d = await api<WebhookDelivery>(`finance/webhooks/${ep.id}/test`, { body: {}, idempotencyKey: uuid() });
      invalidate("all", "list");
      if (d.status === "delivered") toast.success(`Tes terkirim ke ${ep.name} (HTTP ${d.response_code ?? 200})`);
      else toast.warning(`Tes gagal: ${d.error ?? `HTTP ${d.response_code ?? "?"}`}`);
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(null);
    }
  };
  const patch = async (ep: WebhookEndpoint, body: Record<string, unknown>, done: string) => {
    setBusy(ep.id);
    try {
      const out = await api<WebhookEndpoint>(`finance/webhooks/${ep.id}`, { method: "PATCH", body });
      invalidate("all", "list");
      if (out.secret) setSecret({ name: out.name, secret: out.secret });
      toast.success(done);
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(null);
    }
  };
  const remove = async (ep: WebhookEndpoint) => {
    setBusy(ep.id);
    try {
      await api(`finance/webhooks/${ep.id}`, { method: "DELETE" });
      invalidate("all", "list");
      toast.action("deleted", `Webhook ${ep.name}`);
    } catch (e) {
      toast.failed("deleted", e, "Webhook");
    } finally {
      setBusy(null);
    }
  };
  const columns = useMemo<ColumnDef<WebhookEndpoint, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): URL kecil di atas nama endpoint, event satu baris (daftar lengkap di tooltip), status +
    // satu flag antrean terpenting (gagal, lalu menunggu), pengiriman terakhir satu baris. Petunjuk secret & rincian antrean ada di
    // riwayat pengiriman / menu aksi.
    { id: "name", header: "Endpoint", meta: { mobile: "primary" }, cell: ({ row: { original: e } }) => <CellTitle code={<span title={e.url}>{e.url}</span>} title={e.name} /> },
    { id: "events", header: "Event", meta: { mobile: "secondary" }, cell: ({ row: { original: e } }) => (e.event_types.length === 0 ? <CellText max={200}>Semua event</CellText> : <CellText max={200} title={e.event_types.map((t) => `${t} — ${eventLabel(t)}`).join("\n")}>{e.event_types.length === 1 ? e.event_types[0] : `${fmtNumber(e.event_types.length)} event · ${e.event_types[0]}, …`}</CellText>) },
    {
      id: "status", header: "Status", size: 170, meta: { mobile: "status" },
      cell: ({ row: { original: e } }) => (
        <div className="flex flex-wrap items-center gap-1">
          {e.is_active ? <Badge tone="success">Aktif</Badge> : <Badge tone="neutral">Nonaktif</Badge>}
          {e.failed_count > 0 ? <Badge tone="error">{fmtNumber(e.failed_count)} gagal</Badge> : e.pending_count > 0 ? <Badge tone="info">{fmtNumber(e.pending_count)} menunggu</Badge> : null}
        </div>
      ),
    },
    { id: "last", header: "Pengiriman terakhir", size: 170, meta: { mobile: "secondary" }, cell: ({ row: { original: e } }) => (e.last_delivery_at ? <span className={e.last_status && e.last_status !== "delivered" ? "whitespace-nowrap text-sm text-on-error-container" : "whitespace-nowrap text-sm"} title={e.last_status === "delivered" ? "terkirim" : e.last_status ?? undefined}><RelativeTime value={e.last_delivery_at} /></span> : <span className="whitespace-nowrap text-sm text-on-surface-variant">Belum pernah</span>) },
  ], []);
  if (!orgView) return <Alert variant="info" title="Tingkat organization">Webhook keluar dikelola untuk seluruh organization dan memerlukan izin <b>billing.accounting.view</b> (lihat) / <b>billing.accounting.manage</b> (kelola) tanpa batasan property.</Alert>;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-on-surface-variant">Event keuangan dikirim sebagai POST JSON bertanda tangan ke sistem akuntansi Anda; gagal dicoba ulang otomatis (backoff eksponensial, maks. 8 kali).</p>
        {orgManage && <Button icon="add" onClick={() => setEdit("new")}>Tambah Webhook</Button>}
      </div>
      <DataGrid
        columns={columns}
        rows={list.data ?? []}
        rowId={(r) => r.id}
        onRowClick={(r) => { setDeliveries(r); }}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        empty={{ icon: "webhook", title: "Belum ada webhook", description: "Tambahkan endpoint https sistem akuntansi untuk menerima event invoice, pembayaran, credit note, dan draft tagihan periode.", action: orgManage ? <Button icon="add" onClick={() => setEdit("new")}>Tambah Webhook</Button> : undefined }}
        rowActions={(ep) => [
          { label: "Riwayat pengiriman", icon: "history", onSelect: () => setDeliveries(ep) },
          ...(orgManage ? [
            { label: "Edit", icon: "edit", onSelect: () => setEdit(ep) },
            { label: busy === ep.id ? "Mengirim…" : "Tes kirim", icon: "send", onSelect: () => void test(ep) },
            { label: ep.is_active ? "Nonaktifkan" : "Aktifkan", icon: ep.is_active ? "pause_circle" : "play_circle", onSelect: () => void patch(ep, { is_active: !ep.is_active }, ep.is_active ? "Webhook dinonaktifkan" : "Webhook diaktifkan") },
            { label: "Rotasi secret…", icon: "key", onSelect: () => setConfirm({ kind: "rotate", ep }) },
            { label: "Hapus…", icon: "delete", destructive: true, onSelect: () => setConfirm({ kind: "delete", ep }) },
          ] : []),
        ]}
      />
      <SignatureDocs />
      {edit && <EndpointDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} onSecret={(name, s) => setSecret({ name, secret: s })} />}
      {secret && <SecretDialog name={secret.name} secret={secret.secret} onClose={() => setSecret(null)} />}
      {deliveries && <DeliveriesDrawer endpoint={deliveries} canManage={orgManage} onClose={() => setDeliveries(null)} />}
      {confirm && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setConfirm(null)}
          title={confirm.kind === "delete" ? `Hapus webhook ${confirm.ep.name}?` : `Rotasi secret ${confirm.ep.name}?`}
          description={confirm.kind === "delete" ? "Endpoint & riwayat pengirimannya dihapus; event berikutnya tidak lagi dikirim." : "Secret lama langsung tidak berlaku. Perbarui secret di sistem penerima segera setelah ini."}
          confirmLabel={confirm.kind === "delete" ? "Hapus" : "Rotasi secret"}
          destructive
          onConfirm={() => { const c = confirm; setConfirm(null); if (c.kind === "delete") void remove(c.ep); else void patch(c.ep, { rotate_secret: true }, "Secret dirotasi"); }}
        />
      )}
    </div>
  );
}

function EndpointDialog({ item, onClose, onSecret }: { item: WebhookEndpoint | null; onClose: () => void; onSecret: (name: string, secret: string) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [f, setF] = useState({ name: item?.name ?? "", url: item?.url ?? "", all: item ? item.event_types.length === 0 : false, events: item?.event_types ?? ["invoice.issued", "invoice.paid", "payment.paid"], is_active: item?.is_active ?? true });
  const [busy, setBusy] = useState(false);
  const urlErr = f.url.trim() ? webhookUrlError(f.url) : null;
  const urlWarn = !urlErr && f.url.trim() ? webhookUrlWarning(f.url) : null;
  const valid = !!f.name.trim() && !!f.url.trim() && !urlErr && (f.all || f.events.length > 0);
  const toggle = (v: string, on: boolean) => setF({ ...f, events: on ? [...new Set([...f.events, v])] : f.events.filter((x) => x !== v) });
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    const body = { name: f.name.trim(), url: f.url.trim(), event_types: f.all ? [] : f.events, is_active: f.is_active };
    try {
      const out = item ? await api<WebhookEndpoint>(`finance/webhooks/${item.id}`, { method: "PATCH", body }) : await api<WebhookEndpoint>("finance/webhooks", { body, idempotencyKey: uuid() });
      invalidate("all", "list");
      toast.action(item ? "saved" : "created", `Webhook ${out.name}`);
      onClose();
      if (out.secret) onSecret(out.name, out.secret);
    } catch (e) {
      toast.failed(item ? "saved" : "created", e, "Webhook");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit webhook ${item.name}` : "Tambah Webhook"} description={item ? undefined : "Secret penandatangan dibuat otomatis dan hanya ditampilkan sekali setelah disimpan."}>
        <div className="space-y-4">
          <Field label="Nama" required><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="mis. Accurate Online" maxLength={120} autoFocus={!item} /></Field>
          <Field label="URL" required error={urlErr ?? undefined} help={urlWarn ?? "Endpoint https publik yang menerima POST JSON."}>
            <Input type="url" value={f.url} onChange={(e) => setF({ ...f, url: e.target.value })} placeholder="https://erp.contoh.co.id/webhooks/buildingvision" className="font-mono text-sm" />
          </Field>
          <div>
            <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Event</div>
            <Checkbox label="Semua event keuangan (termasuk event baru di masa depan)" checked={f.all} onCheckedChange={(v) => setF({ ...f, all: v })} />
            {!f.all && (
              <div className="mt-2 grid grid-cols-1 gap-1.5 rounded-[var(--radius-md)] border border-border p-3 sm:grid-cols-2">
                {FINANCE_EVENTS.map((e) => (
                  <div key={e.value} className="flex flex-col">
                    <Checkbox label={e.label} checked={f.events.includes(e.value)} onCheckedChange={(v) => toggle(e.value, v)} />
                    <span className="ml-8 font-mono text-[11px] text-on-surface-variant">{e.value}</span>
                  </div>
                ))}
              </div>
            )}
            {!f.all && f.events.length === 0 && <p className="mt-1 text-xs text-error">Pilih minimal satu event.</p>}
          </div>
          <Checkbox label="Aktif (kirim event)" checked={f.is_active} onCheckedChange={(v) => setF({ ...f, is_active: v })} />
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!valid} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SecretDialog({ name, secret, onClose }: { name: string; secret: string; onClose: () => void }) {
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Secret webhook ${name}`} description="Salin & simpan sekarang — secret tidak dapat ditampilkan lagi (hanya 4 karakter terakhir). Bila hilang, lakukan rotasi.">
        <div className="space-y-3">
          <CopyField label="Secret penandatangan" value={secret} />
          <Alert variant="warning">Simpan di sistem penerima sebagai rahasia (jangan di repositori kode). Dipakai untuk memverifikasi header <code>X-BV-Signature</code>.</Alert>
        </div>
        <DialogFooter><Button onClick={onClose}>Sudah disimpan</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const SAMPLE = `// Node.js (Express, body mentah)
const crypto = require("crypto");
app.post("/webhooks/buildingvision", express.raw({ type: "application/json" }), (req, res) => {
  const ts = req.header("X-BV-Timestamp");
  const expected = Buffer.from("v1=" + crypto.createHmac("sha256", process.env.BV_WEBHOOK_SECRET)
    .update(ts + "." + req.body).digest("hex"));
  const got = Buffer.from(req.header("X-BV-Signature") || "");
  const ok = got.length === expected.length && crypto.timingSafeEqual(got, expected);
  if (!ok || Math.abs(Date.now() / 1000 - Number(ts)) > 300) return res.sendStatus(401);
  const event = JSON.parse(req.body); // { id, type, occurred_at, organization_id, property_id, object_type, object_id, data }
  // idempoten: abaikan X-BV-Delivery yang sudah diproses
  res.sendStatus(200);
});`;

function SignatureDocs() {
  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1 basis-60">
          <CardTitle>Verifikasi tanda tangan</CardTitle>
          <CardSubtitle>Setiap pengiriman adalah POST <code>application/json</code> dengan header berikut. Balas 2xx untuk menandai terkirim.</CardSubtitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <ul className="space-y-1">
          <li><code className="font-semibold">X-BV-Signature</code>: <code>v1=hex(hmac_sha256(secret, timestamp + "." + body))</code></li>
          <li><code className="font-semibold">X-BV-Timestamp</code>: detik Unix saat dikirim (tolak bila selisih &gt; 5 menit)</li>
          <li><code className="font-semibold">X-BV-Event</code>: tipe event, mis. <code>invoice.issued</code> · <code>webhook.test</code> untuk tes</li>
          <li><code className="font-semibold">X-BV-Delivery</code>: ID pengiriman unik — pakai untuk idempotensi (retry dapat mengirim ulang)</li>
        </ul>
        <pre className="overflow-x-auto rounded-[var(--radius-md)] bg-surface-container p-3 text-xs leading-5"><code>{SAMPLE}</code></pre>
      </CardContent>
    </Card>
  );
}

function DeliveriesDrawer({ endpoint, canManage, onClose }: { endpoint: WebhookEndpoint; canManage: boolean; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [status, setStatus] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const list = useList<WebhookDelivery>(`finance/webhooks/${endpoint.id}/deliveries`, { status: status || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const retry = (d: WebhookDelivery) => api(`finance/webhook-deliveries/${d.id}/retry`, { body: {}, idempotencyKey: uuid() })
    .then(() => { invalidate("list", "all"); toast.success("Pengiriman dijadwalkan ulang"); })
    .catch(toast.error);
  return (
    <Drawer open onClose={onClose} title={`Riwayat · ${endpoint.name}`} description={endpoint.url} width={720}>
      <div className="space-y-3">
        <span className="flex flex-wrap gap-1.5">
          <FilterChip selected={!status} onClick={() => setStatus("")}>Semua</FilterChip>
          {statusOptions("webhook_delivery").map((o) => <FilterChip key={o.value} selected={status === o.value} onClick={() => setStatus(status === o.value ? "" : o.value)}>{o.label}</FilterChip>)}
        </span>
        {list.isLoading ? <p className="text-sm text-on-surface-variant">Memuat…</p> : list.isError ? <Alert variant="critical">Riwayat gagal dimuat.</Alert> : rows.length === 0 ? <p className="py-6 text-center text-sm text-on-surface-variant">Belum ada pengiriman{status ? " dengan status ini" : ""}.</p> : (
          <ul className="divide-y divide-border rounded-[var(--radius-md)] border border-border">
            {rows.map((d) => (
              <li key={d.id} className="px-3 py-2 text-sm">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <StatusBadge objectType="webhook_delivery" status={d.status} />
                    <span className="font-mono text-[13px]">{d.event_type}</span>
                    <span className="text-on-surface-variant">{eventLabel(d.event_type)}</span>
                  </div>
                  <div className="flex items-center gap-1">
                    {canManage && d.status !== "delivered" && <Button size="sm" variant="secondary" icon="replay" onClick={() => void retry(d)}>Kirim ulang</Button>}
                    <Button size="sm" variant="ghost" onClick={() => setOpen(open === d.id ? null : d.id)}>{open === d.id ? "Tutup" : "Payload"}</Button>
                  </div>
                </div>
                <div className="mt-0.5 flex flex-wrap gap-x-3 text-xs text-on-surface-variant">
                  <span>{fmtDateTime(d.created_at)}</span>
                  <span>{fmtNumber(d.attempts)} percobaan</span>
                  {d.response_code !== null && <span>HTTP {d.response_code}</span>}
                  {d.delivered_at && <span>terkirim {fmtDateTime(d.delivered_at)}</span>}
                  {d.status === "pending" && <span>berikutnya {fmtDateTime(d.next_attempt_at)}</span>}
                </div>
                {d.error && <div className="mt-1 break-words text-xs text-on-error-container">{d.error}</div>}
                {open === d.id && <pre className="mt-2 max-h-72 overflow-auto rounded-[var(--radius-md)] bg-surface-container p-2 text-[11px] leading-4">{JSON.stringify(d.payload ?? {}, null, 2)}</pre>}
              </li>
            ))}
          </ul>
        )}
        {list.hasNextPage && <Button variant="secondary" size="sm" loading={list.isFetchingNextPage} onClick={() => list.fetchNextPage()}>Muat lebih banyak</Button>}
      </div>
    </Drawer>
  );
}
