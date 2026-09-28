// Notifications: inbox penuh + preferensi per tipe (in-app / push / email bila tersedia) — PRD §20, PRD P0 v2 §14.3.
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Card, CardContent, CardHeader, CardTitle, Checkbox, THead, TBody, TD, TH, TR, Table } from "@/components/ui/primitives";
import { AsyncState, useToast } from "@/components/bv/common";
import { NotificationInbox } from "@/components/shell/NotificationInbox";
import { CellText } from "@/components/bv/cells";
import { useInvalidate } from "@/api/hooks";
import { api } from "@/lib/api";
import type { NotificationPreference as Pref } from "@/api/types";

export default function NotificationsSection() {
  const { t } = useTranslation();
  const toast = useToast();
  const invalidate = useInvalidate();
  const prefs = useQuery({ queryKey: ["notification-prefs"], queryFn: () => api<{ data: Pref[] }>("notifications/preferences").then((r) => r.data) });
  const set = (p: Pref) => api("notifications/preferences", { method: "PUT", body: { type: p.type, inapp: p.inapp, push: p.push, email: p.email_available ? p.email : false } }).then(() => invalidate("notification-prefs")).catch((e) => toast.failed("saved", e, t("notif.preferences")));
  return (
    <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
      <Card className="min-w-0 lg:col-span-7">
        <CardContent className="pt-2"><NotificationInbox full /></CardContent>
      </Card>
      <Card className="min-w-0 lg:col-span-5">
        <CardHeader><CardTitle>Preferensi</CardTitle></CardHeader>
        <CardContent className="px-0">
          <AsyncState query={prefs}>
            {(list) => {
              const anyEmail = list.some((p) => p.email_available);
              return (
              <Table>
                <THead><tr><TH>Tipe notifikasi</TH><TH className="text-center">In-app</TH><TH className="text-center">Push</TH>{anyEmail && <TH className="text-center">Email</TH>}</tr></THead>
                <TBody>
                  {list.map((p) => (
                    <TR key={p.type}>
                      <TD><CellText max={220} className="font-mono text-xs">{p.type}</CellText></TD>
                      <TD className="text-center"><Checkbox checked={p.inapp} onCheckedChange={(v) => set({ ...p, inapp: !!v })} aria-label={`In-app ${p.type}`} /></TD>
                      <TD className="text-center"><Checkbox checked={p.push} onCheckedChange={(v) => set({ ...p, push: !!v })} aria-label={`Push ${p.type}`} /></TD>
                      {anyEmail && <TD className="text-center">{p.email_available ? <Checkbox checked={p.email} onCheckedChange={(v) => set({ ...p, email: !!v })} aria-label={`Email ${p.type}`} /> : <span className="text-xs text-on-surface-variant">—</span>}</TD>}
                    </TR>
                  ))}
                  {list.length === 0 && <TR><TD colSpan={anyEmail ? 4 : 3} className="py-6 text-center text-sm text-muted-foreground">Belum ada aturan notifikasi aktif.</TD></TR>}
                </TBody>
              </Table>
              );
            }}
          </AsyncState>
          <p className="px-4 pt-3 text-xs text-muted-foreground">Notifikasi kritis (SLA breach, incident critical) tetap dikirim in-app.</p>
        </CardContent>
      </Card>
    </div>
  );
}
