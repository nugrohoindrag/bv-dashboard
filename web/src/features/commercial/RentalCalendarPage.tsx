// Commercial › Unit Rental › Calendar (PRD P1 v1.3 §3.10 rental availability): unit × tanggal; inquiry (New) tampil transparan,
// Reserved/Active memblokir periode.
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button } from "@/components/ui/primitives";
import { AsyncState } from "@/components/bv/common";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { PERIOD_LABEL, RLISTING_STATUS, RRES_STATUS, type RentalListing } from "./commercial-api";

interface Entry { reservation_id: string; reservation_number: string; prospect_name: string; unit_location_id: string; unit_number: string; rental_period: string; start_date: string; end_date: string; status: string }
interface Calendar { from: string; to: string; listings: RentalListing[]; entries: Entry[] }

const addDays = (d: Date, n: number) => { const x = new Date(d); x.setDate(x.getDate() + n); return x; };
const iso = (d: Date) => d.toISOString().slice(0, 10);

export default function RentalCalendarPage() {
  const { propertyId } = useAuth();
  const nav = useNavigate();
  const [start, setStart] = useState(() => { const d = new Date(); d.setHours(0, 0, 0, 0); return addDays(d, -2); });
  const days = 28;
  const from = iso(start);
  const to = iso(addDays(start, days));
  const cal = useQuery({ queryKey: ["unit-rental-calendar", propertyId, from, to], enabled: !!propertyId, queryFn: () => api<Calendar>("unit-rental/reservations/calendar", { query: { property_id: propertyId, from, to } }) });
  if (!propertyId) return <Alert variant="info">Pilih property (profile Apartment) di header.</Alert>;
  const dates = Array.from({ length: days }, (_, i) => addDays(start, i));
  return (
    <div>
      <PageHeader title="Rental Calendar" subtitle="Unit × tanggal (4 minggu). Blok penuh = Reserved/Active; blok putus-putus = inquiry." actions={<div className="flex items-center gap-1"><Button size="sm" variant="secondary" onClick={() => setStart(addDays(start, -14))}><Icon name="chevron_left" size={16} /></Button><Button size="sm" variant="secondary" onClick={() => { const d = new Date(); d.setHours(0, 0, 0, 0); setStart(addDays(d, -2)); }}>Hari ini</Button><Button size="sm" variant="secondary" onClick={() => setStart(addDays(start, 14))}><Icon name="chevron_right" size={16} /></Button></div>} />
      <AsyncState query={cal}>
        {(c) => (
          <div className="overflow-x-auto rounded-[var(--radius-md)] border border-border">
            <table className="w-full border-collapse text-xs">
              <thead>
                <tr className="bg-surface-container">
                  <th className="sticky left-0 z-10 w-44 bg-surface-container p-2 text-left">Unit</th>
                  {dates.map((d) => <th key={iso(d)} className={`min-w-[40px] p-1 text-center font-medium ${iso(d) === iso(new Date()) ? "text-primary" : ""} ${d.getDay() === 0 ? "text-error" : ""}`}>{d.getDate()}<br /><span className="text-[10px] text-muted-foreground">{d.toLocaleDateString("id-ID", { month: "short" })}</span></th>)}
                </tr>
              </thead>
              <tbody>
                {c.listings.map((l) => (
                  <tr key={l.id} className="border-t border-border">
                    <td className="sticky left-0 z-10 bg-surface p-2"><div className="font-semibold">{l.unit_number}</div><div className="truncate text-[10px] text-muted-foreground">{l.title}</div><Badge tone={RLISTING_STATUS[l.status]?.tone ?? "neutral"} className="mt-0.5">{RLISTING_STATUS[l.status]?.label}</Badge></td>
                    {dates.map((d) => {
                      const ds = iso(d);
                      const blocking = c.entries.find((x) => x.unit_location_id === l.unit_location_id && x.status !== "new" && x.start_date.slice(0, 10) <= ds && x.end_date.slice(0, 10) > ds);
                      const e = blocking ?? c.entries.find((x) => x.unit_location_id === l.unit_location_id && x.start_date.slice(0, 10) <= ds && x.end_date.slice(0, 10) > ds);
                      const first = e && (e.start_date.slice(0, 10) === ds || ds === from);
                      return (
                        <td key={ds} className="h-10 border-l border-border p-0.5 align-top">
                          {e && (
                            <button type="button" onClick={() => nav(`/commercial/rental/reservations/${e.reservation_id}`)} title={`${e.reservation_number} · ${e.prospect_name} · ${PERIOD_LABEL[e.rental_period]} · ${RRES_STATUS[e.status]?.label}`}
                              className={`block h-full w-full truncate rounded px-1 py-1 text-left text-[10px] ${e.status === "active" ? "bg-primary text-on-primary" : e.status === "reserved" ? "bg-primary/70 text-on-primary" : "border border-dashed border-outline text-muted-foreground"}`}>
                              {first ? e.prospect_name : " "}
                            </button>
                          )}
                        </td>
                      );
                    })}
                  </tr>
                ))}
                {c.listings.length === 0 && <tr><td colSpan={days + 1} className="p-4 text-center text-muted-foreground">Belum ada listing sewa.</td></tr>}
              </tbody>
            </table>
          </div>
        )}
      </AsyncState>
    </div>
  );
}
