// Kapasitas Tim (PRD P2 v2.1 P2-WKL-03; Roadmap v2.1 §25.8 "Workforce Capacity"): staf on-duty × sisa jam shift vs beban
// pekerjaan hari ini per domain (D-P2-05: dihitung per domain, Overview menjumlahkan). Sumber: GET /overview/workforce-capacity.
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import { toneColor, toneContainer, toneOnContainer } from "@buildingvision/ui/bv";
import { Card, CardContent, CardHeader, CardSubtitle, CardTitle, Skeleton } from "@/components/ui/primitives";
import { QueryErrorState } from "@/components/bv/states";
import { api } from "@/lib/api";
import { fmtNumber } from "@/lib/format";
import type { DomainCapacity, WorkforceCapacity } from "@/api/types";
import { CAPACITY_LABEL, DOMAIN_LABEL, capacityTone, fmtHours, labelOf, loadPct } from "./workforce";

export function WorkforceCapacityPanel({ propertyId }: { propertyId?: string | null }) {
  const q = useQuery({
    queryKey: ["overview", "workforce-capacity", propertyId ?? null],
    queryFn: ({ signal }) => api<WorkforceCapacity>("overview/workforce-capacity", { query: { property_id: propertyId ?? undefined }, signal }),
    refetchInterval: 60_000,
    staleTime: 30_000,
  });
  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1 basis-60">
          <CardTitle>Kapasitas Tim</CardTitle>
          <CardSubtitle>Kapasitas = staf on-duty × sisa jam shift; beban = pekerjaan hari ini (jatuh tempo, overdue, sedang dikerjakan) × estimasi durasi.</CardSubtitle>
        </div>
      </CardHeader>
      <CardContent>
        {q.isLoading ? (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-3" aria-busy>{[0, 1, 2].map((i) => <Skeleton key={i} className="h-28 w-full" />)}</div>
        ) : q.error && !q.data ? (
          <QueryErrorState error={q.error} onRetry={() => q.refetch()} compact />
        ) : q.data ? (
          <div className="space-y-3">
            <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
              {q.data.domains.map((d) => <CapacityTile key={d.domain} d={d} />)}
            </div>
            <p className="text-xs text-on-surface-variant tnum">
              Total: {fmtNumber(q.data.total.on_duty)} staf on-duty dari {fmtNumber(q.data.total.scheduled)} terjadwal · kekurangan {fmtNumber(q.data.total.shortage)} ·
              beban {fmtHours(q.data.total.workload_minutes)} / kapasitas {fmtHours(q.data.total.capacity_minutes)} · {fmtNumber(q.data.total.open_items)} pekerjaan
            </p>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function CapacityTile({ d }: { d: DomainCapacity }) {
  const tone = capacityTone(d.status);
  const pct = loadPct(d.load_ratio);
  const hasShift = d.domain !== "engineering";
  return (
    <Link to={d.link || "/overview"} className="block rounded-[var(--radius-lg)] border border-border bg-surface p-3 transition-colors hover:bg-surface-container-low" data-testid={`capacity-${d.domain}`}>
      <div className="flex items-center justify-between gap-2">
        <span className="text-body font-semibold text-on-surface">{labelOf(DOMAIN_LABEL, d.domain)}</span>
        <span className="inline-flex h-[22px] items-center rounded-full px-2 text-xs font-semibold" style={{ backgroundColor: toneContainer[tone], color: toneOnContainer[tone] }}>{labelOf(CAPACITY_LABEL, d.status)}</span>
      </div>
      <div className="mt-2 flex items-baseline gap-1 tnum">
        <span className="text-h2 font-bold text-on-surface">{fmtNumber(d.on_duty)}</span>
        <span className="text-sm text-on-surface-variant">on-duty{hasShift ? ` / ${fmtNumber(d.scheduled)} terjadwal` : ""}</span>
      </div>
      {/* meter: isi = severity, track = langkah lebih terang dari tone yang sama (dataviz: meter) */}
      <div className="mt-2 h-2 w-full overflow-hidden rounded-full" style={{ backgroundColor: toneContainer[tone] }} role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={`Beban ${labelOf(DOMAIN_LABEL, d.domain)}`}>
        <div className="h-full rounded-full" style={{ width: `${pct}%`, backgroundColor: toneColor[tone] }} />
      </div>
      <div className="mt-1.5 flex flex-wrap justify-between gap-x-3 text-xs text-on-surface-variant tnum">
        <span>Beban {fmtHours(d.workload_minutes)} · {fmtNumber(d.open_items)} pekerjaan</span>
        <span>Kapasitas {fmtHours(d.capacity_minutes)}</span>
      </div>
      {hasShift && (d.shortage > 0 || d.absent > 0) && (
        <div className="mt-1.5 flex flex-wrap gap-x-3 text-xs font-semibold text-on-warning-container">
          {d.shortage > 0 && <span className="inline-flex items-center gap-1"><Icon name="group_off" size={14} aria-hidden />Kurang {fmtNumber(d.shortage)} staf</span>}
          {d.absent > 0 && <span>{fmtNumber(d.absent)} tidak hadir</span>}
        </div>
      )}
    </Link>
  );
}
