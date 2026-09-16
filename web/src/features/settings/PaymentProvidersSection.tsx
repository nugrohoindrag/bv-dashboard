// Settings › Payment Providers (TD-P1-007; OD-P1-009): aktifkan provider, metode, konfigurasi publik, rotasi webhook secret.
// BuildingVision tidak menyimpan kredensial pembayaran tenant (guardrail #10).
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Checkbox, Field, Input, Textarea } from "@/components/ui/primitives";
import { AsyncState, useToast } from "@/components/bv/common";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";

interface ProviderInfo { code: string; name: string; is_active: boolean; methods: string[]; config: Record<string, unknown>; has_secret: boolean }
const ALL_METHODS = ["transfer", "cash", "va", "qris", "ewallet", "card"];
const KNOWN = ["manual", "mock_gateway", "midtrans", "xendit"];

export default function PaymentProvidersSection() {
  const { can } = useAuth();
  const toast = useToast();
  const q = useQuery({ queryKey: ["payment-providers"], queryFn: () => api<{ data: ProviderInfo[] }>("payment-providers").then((r) => r.data) });
  const canEdit = can("platform.organizations.update");
  return (
    <div className="space-y-5">
      <Alert variant="info" title="Payment gateway">Provider nyata (Midtrans/Xendit) menunggu keputusan OD-P1-009; alur, verifikasi callback, dan idempotensi sudah berjalan lewat <code>mock_gateway</code>. Webhook: <code>POST /api/v1/webhooks/payments/{"{provider}"}</code> dengan header <code>X-BV-Signature</code> (HMAC-SHA256 body).</Alert>
      <AsyncState query={q}>
        {(list) => (
          <div className="grid grid-cols-2 gap-5">
            {KNOWN.map((code) => <ProviderCard key={code} code={code} info={list.find((x) => x.code === code)} canEdit={canEdit} onSaved={() => { q.refetch(); toast.success(`Provider ${code} disimpan`); }} />)}
          </div>
        )}
      </AsyncState>
    </div>
  );
}

function ProviderCard({ code, info, canEdit, onSaved }: { code: string; info?: ProviderInfo; canEdit: boolean; onSaved: () => void }) {
  const toast = useToast();
  const [active, setActive] = useState(info?.is_active ?? false);
  const [methods, setMethods] = useState<string[]>(info?.methods ?? []);
  const [config, setConfig] = useState(JSON.stringify(info?.config ?? {}, null, 2));
  const [secret, setSecret] = useState("");
  const [saving, setSaving] = useState(false);
  const save = async () => {
    setSaving(true);
    try {
      let cfg: Record<string, unknown> = {};
      try { cfg = config.trim() ? JSON.parse(config) : {}; } catch { throw new Error("Config harus JSON valid"); }
      await api(`payment-providers/${code}`, { method: "PUT", body: { is_active: active, methods, config: cfg, webhook_secret: secret || undefined } });
      setSecret("");
      onSaved();
    } catch (e) {
      toast.error(e);
    } finally {
      setSaving(false);
    }
  };
  const realGateway = code === "midtrans" || code === "xendit";
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><code>{code}</code> {info?.is_active && <Badge tone="success">aktif</Badge>}{realGateway && <Badge tone="neutral">adapter belum tersedia (OD-P1-009)</Badge>}</CardTitle>
        <CardSubtitle>{info?.name ?? (code === "manual" ? "Transfer / tunai diverifikasi staf" : code === "mock_gateway" ? "Simulasi gateway untuk uji end-to-end" : "Payment gateway")}</CardSubtitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <Checkbox label="Aktif untuk tenant" checked={active} onCheckedChange={setActive} disabled={!canEdit || realGateway} />
        <Field label="Metode"><div className="flex flex-wrap gap-2">{ALL_METHODS.map((m) => <Checkbox key={m} label={m} checked={methods.includes(m)} onCheckedChange={(v) => setMethods((s) => (v ? [...s, m] : s.filter((x) => x !== m)))} disabled={!canEdit} />)}</div></Field>
        <Field label="Config publik (JSON: instructions, bank_account, merchant_id…)"><Textarea rows={4} value={config} onChange={(e) => setConfig(e.target.value)} disabled={!canEdit} className="font-mono text-xs" /></Field>
        {code !== "manual" && <Field label={`Webhook secret ${info?.has_secret ? "(sudah diatur — isi untuk rotasi)" : "(belum diatur)"}`}><Input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} disabled={!canEdit} autoComplete="new-password" /></Field>}
        {canEdit && <Button size="sm" loading={saving} onClick={save}>Simpan</Button>}
      </CardContent>
    </Card>
  );
}
