// My Profile (PRD P0 v2 §8.1, §24.1): info akun (GET /me), ganti password, sesi aktif milik sendiri (cabut satu /
// cabut lainnya / keluar dari semua perangkat). Setelah pencabutan, access token menjadi TOKEN_STALE — api client
// me-refresh otomatis sekali (lib/api.ts).
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Alert, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, ConfirmDialog, Field, Input } from "@/components/ui/primitives";
import { AsyncState, KeyValue, useToast } from "@/components/bv/common";
import { SessionList } from "@/components/bv/sessions";
import { CardSkeleton } from "@/components/bv/states";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import type { Session, User } from "@/api/types";

export default function ProfileSection() {
  const { t } = useTranslation();
  const toast = useToast();
  const nav = useNavigate();
  const qc = useQueryClient();
  const { clearSession } = useAuth();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api<{ user: User; principal: { roles: string[] } }>("me") });
  const sessions = useQuery({ queryKey: ["me-sessions"], queryFn: () => api<{ data: Session[] }>("me/sessions").then((r) => r.data) });
  const [pw, setPw] = useState({ current: "", next: "", confirm: "" });
  const [pwErr, setPwErr] = useState<string | null>(null);
  const [savingPw, setSavingPw] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<"others" | "all" | null>(null);

  const changePassword = async () => {
    setPwErr(null);
    if (pw.next.length < 8) return setPwErr(t("profile.pw_min"));
    if (pw.next !== pw.confirm) return setPwErr(t("profile.pw_mismatch"));
    setSavingPw(true);
    try {
      await api("me/password", { body: { current_password: pw.current, new_password: pw.next } });
      setPw({ current: "", next: "", confirm: "" });
      toast.action("updated", "Password");
      toast.info(t("profile.pw_sessions_revoked"));
      qc.invalidateQueries({ queryKey: ["me-sessions"] });
    } catch (e) {
      setPwErr((e as Error).message);
    } finally {
      setSavingPw(false);
    }
  };
  const revokeOne = async (s: Session) => {
    setRevoking(s.id);
    try {
      await api(`me/sessions/${s.id}`, { method: "DELETE" });
      toast.action("deleted", t("profile.session"));
      await sessions.refetch();
    } catch (e) {
      toast.failed("deleted", e, t("profile.session"));
    } finally {
      setRevoking(null);
    }
  };
  const revokeOthers = async () => {
    try {
      const r = await api<{ revoked: number }>("me/sessions/revoke-others", { body: {} });
      toast.success(t("profile.revoked_n", { n: r.revoked }));
      await sessions.refetch();
    } catch (e) {
      toast.error(e);
    }
  };
  const logoutAll = async () => {
    try {
      await api("auth/logout-all", { body: {} });
    } catch {
      /* sesi tetap dibersihkan di klien */
    }
    clearSession();
    qc.clear();
    nav("/login", { replace: true });
  };

  return (
    <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
      <div className="min-w-0 space-y-5 lg:col-span-5">
        <Card>
          <CardHeader><CardTitle>{t("profile.my_info")}</CardTitle></CardHeader>
          <CardContent>
            <AsyncState query={me} skeleton={<CardSkeleton />}>
              {(m) => (
                <KeyValue items={[
                  { label: t("profile.name"), value: m.user.full_name },
                  { label: "Email", value: m.user.email },
                  { label: "Username", value: m.user.username },
                  { label: t("profile.phone"), value: m.user.phone },
                  { label: t("profile.user_code"), value: <span className="font-mono">{m.user.user_code}</span> },
                  { label: "Role", value: (m.user.roles ?? []).map((r) => r.role_name ?? r.role_code).join(", ") || (m.principal.roles ?? []).join(", ") },
                  { label: "Vendor", value: m.user.vendor_name ?? null },
                  { label: t("profile.last_login"), value: fmtDateTime(m.user.last_login_at) },
                ]} />
              )}
            </AsyncState>
          </CardContent>
        </Card>
        <Card>
          <CardHeader><div><CardTitle>{t("profile.change_password")}</CardTitle><CardSubtitle>{t("profile.pw_hint")}</CardSubtitle></div></CardHeader>
          <CardContent className="space-y-3">
            {pwErr && <Alert variant="critical">{pwErr}</Alert>}
            <Field label={t("profile.current_password")} required><Input type="password" autoComplete="current-password" value={pw.current} onChange={(e) => setPw({ ...pw, current: e.target.value })} /></Field>
            <Field label={t("profile.new_password")} required help={t("profile.pw_min")}><Input type="password" autoComplete="new-password" value={pw.next} onChange={(e) => setPw({ ...pw, next: e.target.value })} /></Field>
            <Field label={t("profile.confirm_password")} required><Input type="password" autoComplete="new-password" value={pw.confirm} onChange={(e) => setPw({ ...pw, confirm: e.target.value })} /></Field>
            <Button loading={savingPw} disabled={!pw.current || !pw.next} onClick={changePassword}>{t("profile.change_password")}</Button>
          </CardContent>
        </Card>
      </div>
      <div className="min-w-0 space-y-5 lg:col-span-7">
        <Card>
          <CardHeader>
            <div className="min-w-0 flex-1 basis-48"><CardTitle>{t("profile.my_sessions")}</CardTitle><CardSubtitle>{t("profile.sessions_hint")}</CardSubtitle></div>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="secondary" icon="smartphone" onClick={() => setConfirm("others")}>{t("profile.revoke_others")}</Button>
              <Button size="sm" variant="destructive" icon="logout" onClick={() => setConfirm("all")}>{t("profile.logout_all")}</Button>
            </div>
          </CardHeader>
          <CardContent>
            <AsyncState query={sessions} skeleton={<CardSkeleton lines={4} />}>
              {(list) => <SessionList sessions={list} onRevoke={revokeOne} revoking={revoking} />}
            </AsyncState>
          </CardContent>
        </Card>
      </div>
      <ConfirmDialog open={confirm === "others"} onOpenChange={(o) => !o && setConfirm(null)} title={t("profile.revoke_others")} description={t("profile.revoke_others_desc")} confirmLabel={t("profile.revoke_others")} onConfirm={() => { setConfirm(null); revokeOthers(); }} />
      <ConfirmDialog open={confirm === "all"} onOpenChange={(o) => !o && setConfirm(null)} destructive title={t("profile.logout_all")} description={t("profile.logout_all_desc")} confirmLabel={t("profile.logout_all")} onConfirm={() => { setConfirm(null); logoutAll(); }} />
    </div>
  );
}
