// Settings › Demo Data (Demo Seed Database v1.1 §29–§31, §39): Admin Internal menyiapkan tiga environment demo
// (Hotel, Apartment, Office) — seed, reset (dengan konfirmasi), reseed, verifikasi coverage. Hanya role admin_internal
// (menu disembunyikan untuk yang lain; backend memeriksa server-side lewat RequireInternalAdmin).
import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardTitle, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input } from "@/components/ui/primitives";
import { RelativeTime, useToast } from "@/components/bv/common";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";

type Profile = "hotel" | "apartment" | "office";
const PROFILES: { key: Profile; label: string; property: string; hint: string }[] = [
  { key: "hotel", label: "Hotel", property: "Grand Vision Hotel", hint: "30 kamar · 4 tipe · reservasi · Guest App · BVRooms customer booking" },
  { key: "apartment", label: "Apartment", property: "Vision Residence", hint: "41 unit · 10 tenant · unit sales & rental (daily/weekly/monthly) · BVRooms" },
  { key: "office", label: "Office", property: "Vision Business Center", hint: "30 office unit · 10 tenant company · meeting room · visitor" },
];

interface ProfileState { property_id?: string; seeded_at?: string; record_count: number; seed_version?: string }
interface DemoState {
  present: boolean;
  organization_id?: string;
  organization_slug: string;
  state: string;
  error?: string;
  seed_version?: string;
  last_seeded_at?: string;
  last_reset_at?: string;
  profiles: Record<Profile, ProfileState>;
  running: boolean;
  enabled: boolean;
}
interface Check { name: string; ok: boolean; detail?: string }
interface Report { sections: { title: string; checks: Check[] }[]; pass: boolean; failed: number; total: number }

export default function DemoDataSection() {
  const { principal } = useAuth();
  const toast = useToast();
  const qc = useQueryClient();
  const status = useQuery({ queryKey: ["admin-demo"], queryFn: () => api<DemoState>("admin/demo"), enabled: !!principal?.is_internal_admin, refetchInterval: (q) => (q.state.data?.running ? 3000 : false) });
  const [busy, setBusy] = useState<string | null>(null);
  const [resetTarget, setResetTarget] = useState<Profile[] | "all" | null>(null);
  const [report, setReport] = useState<{ report: Report; text: string } | null>(null);
  const refresh = () => qc.invalidateQueries({ queryKey: ["admin-demo"] });

  if (!principal?.is_internal_admin) {
    return <Alert variant="warning" title="Akses terbatas">Demo Data hanya untuk Admin Internal BuildingVision.</Alert>;
  }
  const st = status.data;
  const seed = async (profiles: Profile[] | "all") => {
    setBusy(profiles === "all" ? "all" : profiles.join(","));
    try {
      const res = await api<{ took: number; log: string[] }>("admin/demo/seed", { body: { profiles: profiles === "all" ? [] : profiles } });
      toast.success(`Seed selesai (${Math.round(res.took / 1e9)} detik). ${res.log[res.log.length - 1] ?? ""}`);
      refresh();
    } catch (e) {
      toast.error(e);
      refresh();
    } finally {
      setBusy(null);
    }
  };
  const verify = async () => {
    setBusy("verify");
    try {
      const res = await api<{ report: Report; text: string }>("admin/demo/verify", { method: "POST", body: {} });
      setReport(res);
      if (res.report.pass) toast.success(`Verifikasi PASS (${res.report.total} pemeriksaan)`);
      else toast.error(new Error(`Verifikasi FAIL: ${res.report.failed} dari ${res.report.total} pemeriksaan`));
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(null);
    }
  };
  const seeded = (p: Profile) => !!st?.profiles?.[p]?.property_id;
  const anySeeded = PROFILES.some((p) => seeded(p.key));

  return (
    <div className="space-y-4">
      <Alert variant="info" title="Demo Environment">
        Tiga environment demo (Hotel, Apartment, Office) berada dalam satu organization <code>demo</code> (BuildingVision Demo) agar Property Switcher dan BVRooms white-label konsisten.
        Seed idempotent; <strong>Reset</strong> menghapus seluruh data organization demo (production/customer data tidak disentuh). Kredensial & panduan: <code>docs/demo/BuildingVision-Demo-Guide.md</code>.
        {st && !st.enabled && <div className="mt-1 font-medium">Tooling demo dinonaktifkan pada environment ini (BV_DEMO_ENABLED).</div>}
      </Alert>
      {st?.error && <Alert variant="critical" title={`Operasi terakhir gagal (state: ${st.state})`}>{st.error}</Alert>}
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone={st?.state === "ready" ? "success" : st?.state === "failed" ? "critical" : st?.running ? "warning" : "neutral"} dot>{st?.running ? "Sedang berjalan…" : st?.state ?? "-"}</Badge>
        {st?.seed_version && <span className="text-sm text-on-surface-variant">Seed version {st.seed_version}</span>}
        {st?.last_seeded_at && <span className="text-sm text-on-surface-variant">· Last seeded <RelativeTime value={st.last_seeded_at} /></span>}
        <div className="ml-auto flex gap-2">
          <Button variant="ghost" onClick={verify} loading={busy === "verify"} disabled={!anySeeded || !!busy}><Icon name="checklist" size={16} /> Verify</Button>
          {anySeeded ? (
            <Button variant="ghost" onClick={() => setResetTarget("all")} disabled={!!busy || st?.running}><Icon name="restart_alt" size={16} /> Reset & Reseed All</Button>
          ) : null}
          <Button onClick={() => seed("all")} loading={busy === "all"} disabled={!!busy || st?.running || !st?.enabled}><Icon name="database" size={16} /> Seed All Profiles</Button>
        </div>
      </div>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        {PROFILES.map((p) => {
          const ps = st?.profiles?.[p.key];
          const ok = seeded(p.key);
          return (
            <Card key={p.key} railTone={ok ? "success" : "neutral"}>
              <CardHeader>
                <CardTitle>{p.label}</CardTitle>
                <div className="text-xs text-on-surface-variant">{p.property}</div>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
                <div className="text-xs text-on-surface-variant">{p.hint}</div>
                <dl className="grid grid-cols-2 gap-x-2 gap-y-1">
                  <dt className="text-on-surface-variant">Status</dt><dd><Badge tone={ok ? "success" : "neutral"}>{ok ? "Seeded" : "Empty"}</Badge></dd>
                  <dt className="text-on-surface-variant">Seed Version</dt><dd>{ps?.seed_version ?? "-"}</dd>
                  <dt className="text-on-surface-variant">Last Seeded</dt><dd>{ps?.seeded_at ? <RelativeTime value={ps.seeded_at} /> : "-"}</dd>
                  <dt className="text-on-surface-variant">Record Count</dt><dd>{ok ? ps?.record_count : "-"}</dd>
                  <dt className="text-on-surface-variant">Last Reset</dt><dd>{st?.last_reset_at ? <RelativeTime value={st.last_reset_at} /> : "-"}</dd>
                </dl>
                <div className="flex gap-2 pt-1">
                  {ok ? (
                    <Button size="sm" variant="ghost" onClick={() => setResetTarget([p.key])} disabled={!!busy || st?.running}>Reset & Reseed</Button>
                  ) : (
                    <Button size="sm" onClick={() => seed([p.key])} loading={busy === p.key} disabled={!!busy || st?.running || !st?.enabled}>Seed {p.label} Demo</Button>
                  )}
                </div>
              </CardContent>
            </Card>
          );
        })}
      </div>
      {report && (
        <Card>
          <CardHeader><CardTitle>Verification Report {report.report.pass ? <Badge tone="success">PASS</Badge> : <Badge tone="critical">FAIL {report.report.failed}/{report.report.total}</Badge>}</CardTitle></CardHeader>
          <CardContent>
            <pre className="max-h-[480px] overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 text-xs">{report.text}</pre>
          </CardContent>
        </Card>
      )}
      {resetTarget && <ResetDialog target={resetTarget} onClose={() => setResetTarget(null)} onDone={() => { setResetTarget(null); refresh(); }} />}
    </div>
  );
}

function ResetDialog({ target, onClose, onDone }: { target: Profile[] | "all"; onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const [confirm, setConfirm] = useState("");
  const [reseed, setReseed] = useState(true);
  const [busy, setBusy] = useState(false);
  const labels = target === "all" ? PROFILES.map((p) => p.label) : PROFILES.filter((p) => target.includes(p.key)).map((p) => p.label);
  const run = async () => {
    setBusy(true);
    try {
      const res = await api<{ rows_deleted: number; reseeded?: string[]; took: number }>("admin/demo/reset", { body: { profiles: target === "all" ? [] : target, reseed, confirm } });
      toast.success(`Reset selesai: ${res.rows_deleted} baris dihapus${res.reseeded?.length ? `; di-seed ulang: ${res.reseeded.join(", ")}` : ""} (${Math.round(res.took / 1e9)} detik)`);
      onDone();
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title="Reset Demo Data?" description="Tindakan ini menghapus data demo. Production/customer data tidak akan terpengaruh.">
        <div className="space-y-3 text-sm">
          <p>Seluruh data demo untuk: <strong>{labels.join(", ")}</strong> akan dihapus.
            {target !== "all" && <span> Ketiga profile berbagi satu organization demo; profile lain akan di-seed ulang otomatis setelah reset.</span>}
          </p>
          <Checkbox checked={reseed} onCheckedChange={(v) => setReseed(!!v)} label="Seed ulang setelah reset (Reset & Reseed)" />
          <Field label='Ketik "RESET" untuk konfirmasi' required><Input value={confirm} onChange={(e) => setConfirm(e.target.value)} placeholder="RESET" /></Field>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="destructive" onClick={run} loading={busy} disabled={confirm !== "RESET"}>Reset Demo Data</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
