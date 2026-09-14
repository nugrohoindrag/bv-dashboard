// Notifications: inbox penuh + preferensi per tipe (in-app / push) — PRD §20.
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle, Checkbox, THead, TBody, TD, TH, TR, Table } from "@/components/ui/primitives";
import { AsyncState, useToast } from "@/components/bv/common";
import { NotificationInbox } from "@/components/shell/NotificationInbox";
import { useInvalidate } from "@/api/hooks";
import { api } from "@/lib/api";

interface Pref { type: string; inapp: boolean; push: boolean }

export default function NotificationsSection() {
  const toast = useToast();
  const invalidate = useInvalidate();
  const prefs = useQuery({ queryKey: ["notification-prefs"], queryFn: () => api<{ data: Pref[] }>("notifications/preferences").then((r) => r.data) });
  const set = (p: Pref) => api("notifications/preferences", { method: "PUT", body: p }).then(() => invalidate("notification-prefs")).catch(toast.error);
  return (
    <div className="grid grid-cols-12 gap-5">
      <Card className="col-span-7">
        <CardContent className="pt-2"><NotificationInbox full /></CardContent>
      </Card>
      <Card className="col-span-5">
        <CardHeader><CardTitle>Preferensi</CardTitle></CardHeader>
        <CardContent className="px-0">
          <AsyncState query={prefs}>
            {(list) => (
              <Table>
                <THead><tr><TH>Tipe notifikasi</TH><TH className="text-center">In-app</TH><TH className="text-center">Push</TH></tr></THead>
                <TBody>
                  {list.map((p) => (
                    <TR key={p.type}>
                      <TD className="font-mono text-xs">{p.type}</TD>
                      <TD className="text-center"><Checkbox checked={p.inapp} onCheckedChange={(v) => set({ ...p, inapp: !!v })} aria-label={`In-app ${p.type}`} /></TD>
                      <TD className="text-center"><Checkbox checked={p.push} onCheckedChange={(v) => set({ ...p, push: !!v })} aria-label={`Push ${p.type}`} /></TD>
                    </TR>
                  ))}
                  {list.length === 0 && <TR><TD colSpan={3} className="py-6 text-center text-sm text-muted-foreground">Belum ada aturan notifikasi aktif.</TD></TR>}
                </TBody>
              </Table>
            )}
          </AsyncState>
          <p className="px-4 pt-3 text-xs text-muted-foreground">Notifikasi kritis (SLA breach, incident critical) tetap dikirim in-app.</p>
        </CardContent>
      </Card>
    </div>
  );
}
