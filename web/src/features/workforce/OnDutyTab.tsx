// Tab On-duty & Kehadiran (PRD P2 v2.1 §8.2 P2-DTY-01..02): papan on-duty domain saat ini (dijadwalkan vs on-duty vs tidak
// hadir per shift & team, staf tanpa roster) dan rekap kehadiran harian (clock-in/out Staff App).
import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { Badge, Card, CardContent, CardHeader, CardSubtitle, CardTitle, DatePicker, TBody, TD, TH, THead, TR, Table } from "@/components/ui/primitives";
import { StatusBadge } from "@/components/bv/badges";
import { DataGrid } from "@/components/bv/datagrid";
import { CellText, CellTitle } from "@/components/bv/cells";
import { EmptyState, KpiSkeleton, QueryErrorState } from "@/components/bv/states";
import { RelativeTime } from "@/components/bv/common";
import { api, type ListResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtNumber, fmtTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { AttendanceRecord, OnDutyBoard, ShiftDomain } from "@/api/types";
import { CLOCK_SOURCE, GPS_LABEL, SHIFT_DOMAIN_LABEL, fmtHours, isoDay, labelOf } from "./workforce";

const REFRESH_MS = 60_000;

export function OnDutyTab({ domain }: { domain: ShiftDomain }) {
  const { propertyId } = useAuth();
  const q = useQuery({
    queryKey: ["on-duty", domain, propertyId ?? null],
    queryFn: ({ signal }) => api<OnDutyBoard>(`${domain}/on-duty`, { query: { property_id: propertyId ?? undefined }, signal }),
    refetchInterval: REFRESH_MS,
    staleTime: 15_000,
  });
  if (q.isLoading) return <KpiSkeleton count={4} />;
  if (q.error && !q.data) return <QueryErrorState error={q.error} onRetry={() => q.refetch()} />;
  const b = q.data;
  if (!b) return null;
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Counter label="Dijadwalkan" value={b.scheduled} hint="Roster pada shift yang sedang berjalan" />
        <Counter label="On duty" value={b.on_duty} hint="Sesi clock-in aktif (termasuk tanpa roster)" tone={b.on_duty > 0 ? "success" : undefined} />
        <Counter label="Tidak hadir" value={b.absent} hint="Dijadwalkan, lewat 15 menit belum clock-in" tone={b.absent > 0 ? "error" : undefined} />
        <Counter label="Kekurangan staf" value={b.shortage} hint="Minimal staf shift − staf on-duty" tone={b.shortage > 0 ? "warning" : undefined} />
      </div>
      <p className="flex items-center gap-1 text-xs text-on-surface-variant"><Icon name="sync" size={14} aria-hidden />Diperbarui <RelativeTime value={b.generated_at} /> · otomatis tiap menit</p>
      {b.shifts.length === 0 && b.unscheduled.length === 0 ? (
        <EmptyState icon="schedule" title={`Tidak ada shift ${SHIFT_DOMAIN_LABEL[domain]} yang sedang berjalan`} description="Papan on-duty menampilkan roster shift yang sedang berjalan dan staf yang clock-in dari Staff App." />
      ) : (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <Card>
            <CardHeader><CardTitle>Per shift</CardTitle></CardHeader>
            <CardContent className="px-0 pb-0">
              <Table>
                <THead><tr><TH>Shift</TH><TH className="bv-num">Min.</TH><TH className="bv-num">Jadwal</TH><TH className="bv-num">On duty</TH><TH className="bv-num">Kurang</TH></tr></THead>
                <TBody>
                  {b.shifts.map((s) => (
                    <TR key={s.shift_id}>
                      <TD className="whitespace-nowrap"><span className="font-medium">{s.name}</span> <span className="text-xs text-on-surface-variant tnum">{fmtTime(s.starts_at)}–{fmtTime(s.ends_at)}</span></TD>
                      <TD className="bv-num tnum">{s.min_staff}</TD>
                      <TD className="bv-num tnum">{s.scheduled}</TD>
                      <TD className="bv-num tnum">{s.on_duty}</TD>
                      <TD className="bv-num">{s.shortage > 0 ? <Badge tone="warning">{s.shortage}</Badge> : <span className="tnum text-on-surface-variant">0</span>}</TD>
                    </TR>
                  ))}
                  {b.shifts.length === 0 && <TR><TD colSpan={5} className="text-center text-sm text-on-surface-variant">Tidak ada roster pada shift berjalan.</TD></TR>}
                </TBody>
              </Table>
            </CardContent>
          </Card>
          <Card>
            <CardHeader><CardTitle>Per team</CardTitle></CardHeader>
            <CardContent className="px-0 pb-0">
              <Table>
                <THead><tr><TH>Team</TH><TH className="bv-num">Jadwal</TH><TH className="bv-num">On duty</TH><TH className="bv-num">Tidak hadir</TH></tr></THead>
                <TBody>
                  {b.teams.map((t) => (
                    <TR key={t.team_id ?? "-"}>
                      <TD><CellText max={220}>{t.team_name}</CellText></TD>
                      <TD className="bv-num tnum">{t.scheduled}</TD>
                      <TD className="bv-num tnum">{t.on_duty}</TD>
                      <TD className={cn("bv-num tnum", t.absent > 0 && "font-semibold text-on-error-container")}>{t.absent}</TD>
                    </TR>
                  ))}
                  {b.teams.length === 0 && <TR><TD colSpan={4} className="text-center text-sm text-on-surface-variant">—</TD></TR>}
                </TBody>
              </Table>
            </CardContent>
          </Card>
          <Card className="xl:col-span-2">
            <CardHeader><div><CardTitle>Staf shift berjalan ({b.staff.length})</CardTitle><CardSubtitle>Status kehadiran dari clock-in Staff App.</CardSubtitle></div></CardHeader>
            <CardContent className="px-0 pb-0">
              <Table>
                <THead><tr><TH>Staf</TH><TH>Shift</TH><TH>Pos / team</TH><TH>Clock-in</TH><TH>Status</TH></tr></THead>
                <TBody>
                  {b.staff.map((r) => (
                    <TR key={r.id}>
                      <TD><CellText max={200} className="font-medium">{r.user_name}</CellText></TD>
                      <TD className="whitespace-nowrap"><span className="font-mono text-xs">{r.shift_code}</span> {r.shift_name}</TD>
                      <TD><CellText max={200} muted>{[r.post, r.team_name].filter(Boolean).join(" · ") || "—"}</CellText></TD>
                      <TD className="tnum whitespace-nowrap">{r.clock_in_at ? fmtTime(r.clock_in_at) : "—"}{r.late_minutes ? <span className="ml-1 text-xs text-on-warning-container">+{r.late_minutes} mnt</span> : null}</TD>
                      <TD><StatusBadge objectType="roster_attendance" status={r.attendance} /></TD>
                    </TR>
                  ))}
                  {b.staff.length === 0 && <TR><TD colSpan={5} className="text-center text-sm text-on-surface-variant">Tidak ada roster pada shift berjalan.</TD></TR>}
                </TBody>
              </Table>
            </CardContent>
          </Card>
          {b.unscheduled.length > 0 && (
            <Card className="xl:col-span-2">
              <CardHeader><div><CardTitle>On duty tanpa roster ({b.unscheduled.length})</CardTitle><CardSubtitle>Clock-in tanpa penugasan roster — tambahkan ke roster bila perlu.</CardSubtitle></div></CardHeader>
              <CardContent>
                <ul className="divide-y divide-border text-sm">
                  {b.unscheduled.map((a) => <li key={a.id} className="flex flex-wrap items-center justify-between gap-2 py-2"><span className="font-medium">{a.user_name}</span><span className="text-xs text-on-surface-variant tnum">clock-in {fmtTime(a.clock_in_at)} · {labelOf(CLOCK_SOURCE, a.clock_in_source)}</span></li>)}
                </ul>
              </CardContent>
            </Card>
          )}
        </div>
      )}
    </div>
  );
}

function Counter({ label, value, hint, tone }: { label: string; value: number; hint: string; tone?: "success" | "warning" | "error" }) {
  return (
    <Card className="p-4" railTone={tone}>
      <div className="text-sm text-on-surface-variant" title={hint}>{label}</div>
      <div className="mt-1 text-display font-extrabold tnum leading-9 text-on-surface">{fmtNumber(value)}</div>
    </Card>
  );
}

export function AttendanceTab({ domain }: { domain: ShiftDomain }) {
  const { propertyId } = useAuth();
  const [date, setDate] = useState(() => isoDay(new Date()));
  const q = useQuery({
    queryKey: ["attendance", domain, propertyId ?? null, date],
    queryFn: ({ signal }) => api<ListResponse<AttendanceRecord>>(`${domain}/attendance`, { query: { property_id: propertyId ?? undefined, date }, signal }).then((r) => r.data),
  });
  const columns = useMemo<ColumnDef<AttendanceRecord, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026): satu baris per kolom; jam shift, sumber clock-in/out & status GPS di tooltip.
      { id: "user", header: "Staf", meta: { mobile: "primary" }, cell: ({ row: { original: a } }) => <CellTitle title={a.user_name} /> },
      { id: "shift", header: "Shift", meta: { mobile: "secondary" }, size: 170, cell: ({ row: { original: a } }) => (a.shift_name ? <CellText max={170} title={`${a.shift_name} · ${fmtTime(a.shift_starts_at)}–${fmtTime(a.shift_ends_at)}`}>{a.shift_name}</CellText> : <CellText max={170} muted>Tanpa roster</CellText>) },
      { id: "clock_in_at", header: "Clock-in", meta: { mobile: "secondary" }, size: 150, cell: ({ row: { original: a } }) => <span className="whitespace-nowrap" title={`${labelOf(CLOCK_SOURCE, a.clock_in_source)}${a.clock_in_gps_status ? ` · ${labelOf(GPS_LABEL, a.clock_in_gps_status)}` : ""}`}><span className="tnum">{fmtTime(a.clock_in_at)}</span>{a.late_minutes > 0 && <span className="ml-1 text-xs font-semibold text-on-warning-container">+{a.late_minutes} mnt</span>}</span> },
      { id: "clock_out_at", header: "Clock-out", meta: { mobile: "secondary" }, size: 110, cell: ({ row: { original: a } }) => (a.clock_out_at ? <span className="tnum whitespace-nowrap" title={a.clock_out_source ? labelOf(CLOCK_SOURCE, a.clock_out_source) : undefined}>{fmtTime(a.clock_out_at)}</span> : "—") },
      { id: "worked", header: "Durasi kerja", meta: { mobile: "hidden" }, size: 130, cell: ({ row: { original: a } }) => <span className="tnum whitespace-nowrap">{fmtHours(a.worked_minutes)}</span> },
      { id: "status", header: "Status", meta: { mobile: "status" }, size: 150, cell: ({ row: { original: a } }) => <StatusBadge objectType="attendance" status={a.status} /> },
    ],
    [],
  );
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <DatePicker className="w-44" value={date} onChange={(v) => setDate(v || isoDay(new Date()))} aria-label="Tanggal kehadiran" />
        <span className="text-sm text-on-surface-variant">Rekap clock-in/clock-out {SHIFT_DOMAIN_LABEL[domain]} per tanggal (zona waktu property).</span>
      </div>
      <DataGrid
        columns={columns}
        rows={q.data ?? []}
        rowId={(r) => r.id}
        loading={q.isLoading}
        error={q.error}
        onRetry={() => q.refetch()}
        empty={{ icon: "fact_check", title: "Belum ada kehadiran", description: "Clock-in/clock-out dilakukan staf dari Staff App; rekap tanggal ini masih kosong." }}
      />
    </div>
  );
}
