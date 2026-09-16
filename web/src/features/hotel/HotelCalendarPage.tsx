// Commercial › Hotel › Reservation Calendar (PRD P1 v1.3 §3.3 "Reservation calendar, room assignment, stay status, occupancy visibility").
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button } from "@/components/ui/primitives";
import { AsyncState } from "@/components/bv/common";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { RES_STATUS, ROOM_STATUS, type Room } from "./hotel-api";

interface Entry { reservation_id: string; reservation_number: string; guest_name: string; room_type_id: string; room_type_name: string; room_location_id: string | null; room_number: string | null; check_in_date: string; check_out_date: string; status: string }
interface Calendar { from: string; to: string; rooms: Room[]; entries: Entry[] }

const addDays = (d: Date, n: number) => { const x = new Date(d); x.setDate(x.getDate() + n); return x; };
const iso = (d: Date) => d.toISOString().slice(0, 10);

export default function HotelCalendarPage() {
  const { propertyId } = useAuth();
  const nav = useNavigate();
  const [start, setStart] = useState(() => { const d = new Date(); d.setHours(0, 0, 0, 0); return addDays(d, -1); });
  const days = 14;
  const from = iso(start);
  const to = iso(addDays(start, days));
  const cal = useQuery({ queryKey: ["hotel-calendar", propertyId, from, to], enabled: !!propertyId, queryFn: () => api<Calendar>("hotel/reservations/calendar", { query: { property_id: propertyId, from, to } }) });
  if (!propertyId) return <Alert variant="info">Pilih property (profile Hotel) di header.</Alert>;
  const dates = Array.from({ length: days }, (_, i) => addDays(start, i));
  return (
    <div>
      <PageHeader title="Reservation Calendar" subtitle="Kamar × tanggal. Blok = reservasi aktif; klik untuk membuka." actions={<div className="flex items-center gap-1"><Button size="sm" variant="secondary" onClick={() => setStart(addDays(start, -7))}><Icon name="chevron_left" size={16} /></Button><Button size="sm" variant="secondary" onClick={() => { const d = new Date(); d.setHours(0, 0, 0, 0); setStart(addDays(d, -1)); }}>Hari ini</Button><Button size="sm" variant="secondary" onClick={() => setStart(addDays(start, 7))}><Icon name="chevron_right" size={16} /></Button></div>} />
      <AsyncState query={cal}>
        {(c) => {
          const unassigned = c.entries.filter((e) => !e.room_location_id);
          return (
            <div className="space-y-4">
              {unassigned.length > 0 && <Alert variant="warning" title={`${unassigned.length} reservasi belum memiliki kamar`}>{unassigned.map((e) => <button key={e.reservation_id} type="button" className="mr-2 underline" onClick={() => nav(`/commercial/hotel/reservations/${e.reservation_id}`)}>{e.reservation_number} · {e.guest_name} ({e.room_type_name}, {e.check_in_date.slice(0, 10)})</button>)}</Alert>}
              <div className="overflow-x-auto rounded-[var(--radius-md)] border border-border">
                <table className="w-full border-collapse text-xs">
                  <thead>
                    <tr className="bg-surface-container">
                      <th className="sticky left-0 z-10 w-36 bg-surface-container p-2 text-left">Kamar</th>
                      {dates.map((d) => <th key={iso(d)} className={`min-w-[72px] p-2 text-center font-medium ${iso(d) === iso(new Date()) ? "text-primary" : ""}`}>{d.toLocaleDateString("id-ID", { weekday: "short" })}<br />{d.getDate()}/{d.getMonth() + 1}</th>)}
                    </tr>
                  </thead>
                  <tbody>
                    {c.rooms.map((rm) => (
                      <tr key={rm.location_id} className="border-t border-border">
                        <td className="sticky left-0 z-10 bg-surface p-2"><div className="font-semibold">{rm.room_number}</div><div className="text-[10px] text-muted-foreground">{rm.room_type_name}</div><Badge tone={ROOM_STATUS[rm.room_status]?.tone ?? "neutral"} className="mt-0.5">{ROOM_STATUS[rm.room_status]?.label}</Badge></td>
                        {dates.map((d) => {
                          const ds = iso(d);
                          const e = c.entries.find((x) => x.room_location_id === rm.location_id && x.check_in_date.slice(0, 10) <= ds && x.check_out_date.slice(0, 10) > ds);
                          const first = e && e.check_in_date.slice(0, 10) === ds;
                          return (
                            <td key={ds} className="h-12 border-l border-border p-0.5 align-top">
                              {e && (
                                <button type="button" onClick={() => nav(`/commercial/hotel/reservations/${e.reservation_id}`)} className={`block h-full w-full truncate rounded px-1 py-1 text-left text-[11px] text-on-primary ${e.status === "checked_in" ? "bg-primary" : e.status === "checked_out" ? "bg-outline" : "bg-primary/70"}`} title={`${e.reservation_number} · ${e.guest_name} · ${RES_STATUS[e.status]?.label}`}>
                                  {first || ds === from ? e.guest_name : " "}
                                </button>
                              )}
                            </td>
                          );
                        })}
                      </tr>
                    ))}
                    {c.rooms.length === 0 && <tr><td colSpan={days + 1} className="p-4 text-center text-muted-foreground">Belum ada kamar.</td></tr>}
                  </tbody>
                </table>
              </div>
            </div>
          );
        }}
      </AsyncState>
    </div>
  );
}
