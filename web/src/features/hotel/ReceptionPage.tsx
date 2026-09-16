// Reception (brief §7; PRD P1 v1.3 §3.3 Hotel Reservation Operations): Guest · Reservation · Room · Room Assignment · Check-in ·
// Check-out · Guest Request · Housekeeping Status — front desk workflow di Dashboard (bukan aplikasi terpisah).
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { MetricCard } from "@buildingvision/ui/bv";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardTitle } from "@/components/ui/primitives";
import { AsyncState, useToast } from "@/components/bv/common";
import { useAction, useAll } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { CreateServiceRequestDialog } from "@/features/operations/FindingDialogs";
import { CreateReservationDialog, ReservationDrawer } from "./HotelReservationsPage";
import { RES_STATUS, ROOM_STATUS, STAY_LABEL, type Occupancy, type Reservation, type Room } from "./hotel-api";

export default function ReceptionPage() {
  const { propertyId, can } = useAuth();
  const nav = useNavigate();
  const toast = useToast();
  const today = new Date().toISOString().slice(0, 10);
  const occ = useQuery({ queryKey: ["hotel-occupancy", propertyId], enabled: !!propertyId, queryFn: () => api<Occupancy>("hotel/occupancy", { query: { property_id: propertyId } }), refetchInterval: 60_000 });
  const arrivals = useAll<Reservation>("hotel/reservations", { property_id: propertyId ?? undefined, arrival_date: today, status: "new,confirmed" }, { enabled: !!propertyId });
  const inHouse = useAll<Reservation>("hotel/reservations", { property_id: propertyId ?? undefined, status: "checked_in" }, { enabled: !!propertyId });
  const rooms = useAll<Room>("hotel/rooms", { property_id: propertyId ?? undefined }, { enabled: !!propertyId });
  const act = useAction<{ id: string; action: string }, { reservation: Reservation }>((i) => `hotel/reservations/${i.id}/${i.action}`, { body: () => ({}), invalidate: ["all", "one", "hotel"] });
  const [openRes, setOpenRes] = useState<string | null>(null);
  const [newRes, setNewRes] = useState(false);
  const [guestReq, setGuestReq] = useState<Reservation | null>(null);
  if (!propertyId) return <Alert variant="info">Pilih property (profile Hotel) di header.</Alert>;
  const departing = (inHouse.data ?? []).filter((r) => r.check_out_date.slice(0, 10) <= today);
  return (
    <div>
      <PageHeader title="Reception" subtitle="Front desk: kedatangan, keberangkatan, tamu in-house, status kamar, guest request." actions={<>{can("hotel.reservations.create") && <Button onClick={() => setNewRes(true)}><Icon name="add" size={16} /> Walk-in / Reservasi</Button>}<Link to="/commercial/hotel/rooms"><Button variant="secondary"><Icon name="hotel" size={16} /> Rooms</Button></Link></>} />
      <AsyncState query={occ}>
        {(o) => (
          <div className="mb-5 grid grid-cols-6 gap-4">
            <MetricCard label="Occupancy" value={`${o.occupancy_pct.toFixed(0)}%`} subValue={`${o.occupied}/${o.total_rooms} kamar`} tone="primary" />
            <MetricCard label="Arrivals Today" value={String(o.arrivals_today)} tone={o.arrivals_today > 0 ? "info" : "neutral"} />
            <MetricCard label="Departures Today" value={String(o.departures_today)} tone={o.departures_today > 0 ? "warning" : "neutral"} />
            <MetricCard label="In-house" value={String(o.in_house)} tone="neutral" />
            <MetricCard label="Dirty Rooms" value={String(o.dirty_rooms)} subValue="menunggu housekeeping" tone={o.dirty_rooms > 0 ? "warning" : "success"} />
            <MetricCard label="Guest Requests" value={String(o.open_guest_requests)} subValue="terbuka" tone={o.open_guest_requests > 0 ? "warning" : "neutral"} />
          </div>
        )}
      </AsyncState>
      <div className="grid grid-cols-12 gap-5">
        <Card className="col-span-6">
          <CardHeader><CardTitle>Kedatangan hari ini</CardTitle></CardHeader>
          <CardContent>
            <ul className="divide-y divide-border text-sm">
              {(arrivals.data ?? []).map((r) => (
                <li key={r.id} className="flex items-center justify-between gap-2 py-2">
                  <button type="button" className="text-left" onClick={() => setOpenRes(r.id)}><div className="font-medium">{r.guest_name}</div><div className="text-xs text-muted-foreground">{r.reservation_number} · {r.room_type_name} · {r.room_number ? `Kamar ${r.room_number}` : "belum ada kamar"} · {r.nights} malam</div></button>
                  <span className="flex items-center gap-2"><Badge tone={RES_STATUS[r.status]?.tone ?? "neutral"}>{RES_STATUS[r.status]?.label}</Badge>{r.allowed_actions.includes("check_in") && <Button size="sm" onClick={() => setOpenRes(r.id)}>Check-in</Button>}</span>
                </li>
              ))}
              {(arrivals.data ?? []).length === 0 && <li className="py-2 text-muted-foreground">Tidak ada kedatangan terjadwal hari ini.</li>}
            </ul>
          </CardContent>
        </Card>
        <Card className="col-span-6">
          <CardHeader><CardTitle>In-house & keberangkatan</CardTitle></CardHeader>
          <CardContent>
            <ul className="divide-y divide-border text-sm">
              {(inHouse.data ?? []).map((r) => (
                <li key={r.id} className="flex items-center justify-between gap-2 py-2">
                  <button type="button" className="text-left" onClick={() => setOpenRes(r.id)}><div className="font-medium">{r.guest_name} <span className="text-xs text-muted-foreground">· Kamar {r.room_number}</span></div><div className="text-xs text-muted-foreground">{r.reservation_number} · check-out {new Date(r.check_out_date).toLocaleDateString("id-ID")} · {STAY_LABEL[r.stay_status]}{r.open_requests ? ` · ${r.open_requests} guest request` : ""}</div></button>
                  <span className="flex items-center gap-2">
                    {can("tenant.service_requests.create") && <Button size="sm" variant="secondary" onClick={() => setGuestReq(r)}>Guest request</Button>}
                    {r.allowed_actions.includes("check_out") && <Button size="sm" variant={departing.includes(r) ? "primary" : "secondary"} loading={act.isPending} onClick={() => act.mutateAsync({ id: r.id, action: "check_out" }).then(() => toast.success(`${r.guest_name} check-out · kamar ${r.room_number} → Dirty (Housekeeping)`)).catch(toast.error)}>Check-out</Button>}
                  </span>
                </li>
              ))}
              {(inHouse.data ?? []).length === 0 && <li className="py-2 text-muted-foreground">Tidak ada tamu in-house.</li>}
            </ul>
          </CardContent>
        </Card>
        <Card className="col-span-12">
          <CardHeader><CardTitle>Status kamar (Housekeeping)</CardTitle></CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-2">
              {(rooms.data ?? []).map((rm) => <Link key={rm.location_id} to="/commercial/hotel/rooms" className="rounded-[var(--radius-md)] border border-border px-3 py-2 text-sm hover:bg-surface-container"><span className="font-semibold">{rm.room_number}</span> <Badge tone={ROOM_STATUS[rm.room_status]?.tone ?? "neutral"} className="ml-1">{ROOM_STATUS[rm.room_status]?.label}</Badge>{rm.current_guest && <span className="ml-1 text-xs text-muted-foreground">{rm.current_guest}</span>}</Link>)}
              {(rooms.data ?? []).length === 0 && <span className="text-sm text-muted-foreground">Belum ada kamar.</span>}
            </div>
          </CardContent>
        </Card>
      </div>
      {openRes && <ReservationDrawer id={openRes} onClose={() => setOpenRes(null)} />}
      {newRes && <CreateReservationDialog propertyId={propertyId} onClose={() => setNewRes(false)} onCreated={(r) => setOpenRes(r.id)} />}
      {guestReq && <CreateServiceRequestDialog open onOpenChange={(o) => !o && setGuestReq(null)} defaults={{ property_id: propertyId, location_id: guestReq.room_location_id ?? undefined, requester_name: guestReq.guest_name, requester_phone: guestReq.guest_phone ?? undefined, title: `Guest request · Kamar ${guestReq.room_number ?? ""} · ${guestReq.guest_name}`, channel: "walk_in" }} onCreated={(sr) => { setGuestReq(null); nav(`/tenant-relation/service-requests/${sr.id}`); }} />}
    </div>
  );
}
