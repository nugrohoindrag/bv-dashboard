// Settings › Payment Providers (PRD P4 v2.1 §6.5, B-15, P4-ONL-03): provider pembayaran tingkat organization — dikelola Finance
// dengan billing.settings.manage (grant tingkat organization), bukan izin organisasi platform. Pembayaran online (Midtrans/Xendit)
// DITUNDA (keputusan 16 Sep 2026): provider tanpa adapter tidak dapat diaktifkan — API menolak dengan 409 PROVIDER_NOT_AVAILABLE.
// BuildingVision tidak menyimpan kredensial pembayaran tenant (guardrail #10); secret webhook tidak pernah dikembalikan API.
import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Checkbox, Field, Input, Textarea } from "@/components/ui/primitives";
import { AsyncState, useToast } from "@/components/bv/common";
import { ApiError, api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { hasOrgGrant } from "@/features/billing/types";

interface ProviderInfo { code: string; name: string; is_active: boolean; methods: string[]; config: Record<string, unknown>; has_secret: boolean }
const ALL_METHODS = ["transfer", "cash", "va", "qris", "ewallet", "card"];
const KNOWN = ["manual", "mock_gateway", "midtrans", "xendit"];
/** Tanpa adapter — pembayaran online ditunda (P4-ONL-02). */
const ON_HOLD = ["midtrans", "xendit"];
const DESCRIPTION: Record<string, string> = {
  manual: "Transfer / tunai — tenant mengunggah bukti, Finance memverifikasi.",
  mock_gateway: "Simulasi gateway untuk uji end-to-end (callback ber-signature HMAC).",
  midtrans: "Payment gateway (VA, QRIS, e-wallet) — ditunda.",
  xendit: "Payment gateway (VA, QRIS, e-wallet) — ditunda.",
};

export default function PaymentProvidersSection() {
  const { can, principal } = useAuth();
  const toast = useToast();
  const q = useQuery({ queryKey: ["payment-providers"], queryFn: () => api<{ data: ProviderInfo[] }>("payment-providers").then((r) => r.data) });
  // B-15: grant tingkat organization (API memakai PropertyIDsFor → all)
  const canEdit = hasOrgGrant(principal, "billing.settings.manage");
  const propertyOnly = !canEdit && can("billing.settings.manage", null);
  return (
    <div className="space-y-5">
      <Alert variant="warning" title="Pembayaran online ditunda">
        Integrasi payment gateway nyata (Midtrans/Xendit) sedang di-hold — tenant membayar lewat transfer/tunai dan Finance memverifikasi bukti bayar di{" "}
        <Link to="/billing/payments?status=initiated,pending" className="font-semibold underline">Payments</Link>. Provider tanpa adapter tidak dapat diaktifkan.
      </Alert>
      {!canEdit && (
        <Alert variant="info">
          {propertyOnly
            ? "Provider berlaku untuk seluruh organization — mengubahnya memerlukan billing.settings.manage tingkat organization (bukan hanya pada property)."
            : "Anda hanya dapat melihat konfigurasi provider (perlu billing.settings.manage tingkat organization untuk mengubah)."}
        </Alert>
      )}
      <AsyncState query={q}>
        {(list) => (
          <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
            {KNOWN.map((code) => <ProviderCard key={code} code={code} info={list.find((x) => x.code === code)} canEdit={canEdit} onSaved={() => { q.refetch(); toast.action("saved", `Provider ${code}`); }} />)}
          </div>
        )}
      </AsyncState>
      <p className="text-xs text-on-surface-variant">Webhook gateway: <code>POST /api/v1/webhooks/payments/{"{provider}"}</code> dengan header <code>X-BV-Signature</code> (HMAC-SHA256 body). Rekening tujuan transfer diatur di <Link to="/billing/settings" className="underline">Pengaturan Billing</Link>.</p>
    </div>
  );
}

function ProviderCard({ code, info, canEdit, onSaved }: { code: string; info?: ProviderInfo; canEdit: boolean; onSaved: () => void }) {
  const toast = useToast();
  const onHold = ON_HOLD.includes(code);
  const [active, setActive] = useState(info?.is_active ?? false);
  const [methods, setMethods] = useState<string[]>(info?.methods ?? []);
  const [config, setConfig] = useState(JSON.stringify(info?.config ?? {}, null, 2));
  const [secret, setSecret] = useState("");
  const [saving, setSaving] = useState(false);
  const editable = canEdit && !onHold;
  const save = async () => {
    setSaving(true);
    try {
      let cfg: Record<string, unknown> = {};
      try {
        cfg = config.trim() ? JSON.parse(config) : {};
      } catch {
        throw new Error("Config harus JSON valid");
      }
      await api(`payment-providers/${code}`, { method: "PUT", body: { is_active: onHold ? false : active, methods, config: cfg, webhook_secret: secret || undefined } });
      setSecret("");
      onSaved();
    } catch (e) {
      if (e instanceof ApiError && e.code === "PROVIDER_NOT_AVAILABLE") toast.warning(`Provider ${code} belum tersedia — pembayaran online sedang ditunda.`);
      else toast.failed("saved", e, `Provider ${code}`);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle className="flex flex-wrap items-center gap-2">
            <code>{code}</code>
            {onHold ? <Badge tone="warning">Ditunda</Badge> : info?.is_active ? <Badge tone="success">Aktif</Badge> : <Badge tone="neutral">Nonaktif</Badge>}
          </CardTitle>
          <CardSubtitle>{info?.name ?? DESCRIPTION[code]}</CardSubtitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {onHold ? (
          <Alert variant="info">{DESCRIPTION[code]} Adapter belum tersedia, sehingga provider ini tidak dapat diaktifkan untuk tenant (API: 409 PROVIDER_NOT_AVAILABLE).</Alert>
        ) : (
          <p className="text-sm text-on-surface-variant">{DESCRIPTION[code]}</p>
        )}
        <Checkbox label="Aktif untuk tenant" checked={onHold ? false : active} onCheckedChange={setActive} disabled={!editable} />
        <Field label="Metode">
          <div className="flex flex-wrap gap-3">{ALL_METHODS.map((m) => <Checkbox key={m} label={m} checked={methods.includes(m)} onCheckedChange={(v) => setMethods((s) => (v ? [...s, m] : s.filter((x) => x !== m)))} disabled={!editable} />)}</div>
        </Field>
        {!onHold && (
          <>
            <Field label="Config publik (JSON: instructions, bank_account, merchant_id…)"><Textarea rows={4} value={config} onChange={(e) => setConfig(e.target.value)} disabled={!editable} className="font-mono text-xs" /></Field>
            {code !== "manual" && <Field label={`Webhook secret ${info?.has_secret ? "(sudah diatur — isi untuk rotasi)" : "(belum diatur)"}`}><Input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} disabled={!editable} autoComplete="new-password" /></Field>}
          </>
        )}
        {editable && <Button size="sm" loading={saving} onClick={save}>Simpan</Button>}
      </CardContent>
    </Card>
  );
}
