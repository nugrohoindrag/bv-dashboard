// Tab Roster (PRD P2 v2.1 P2-SHF-02): grid mingguan shift × tanggal, penugasan massal (shift × tanggal × staf), salin ke
// minggu berikut, batalkan penugasan (riwayat tetap). Status kehadiran per penugasan = status map `roster_attendance`.
import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import { Alert, Button, ConfirmDialog, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, TBody, TD, TH, THead, TR, Table } from "@/components/ui/primitives";
import { StatusBadge } from "@/components/bv/badges";
import { QueryErrorState, TableSkeleton } from "@/components/bv/states";
import { useToast } from "@/components/bv/common";
import { TeamPicker } from "@/components/bv/pickers";
import { useInvalidate } from "@/api/hooks";
import { api, type ListResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDate } from "@/lib/format";
import { fieldErrorsOf } from "@/lib/problem";
import { cn } from "@/lib/utils";
import type { RosterAssignResult, RosterEntry, ShiftDefinition, ShiftDomain } from "@/api/types";
import { PropertyField } from "@/features/security/shared";
import { usePropertyChoice } from "@/features/security/hooks";
import { useShifts } from "./hooks";
import { ShiftDot, StaffMultiSelect } from "./components";
import { addDays, dayLabel, groupRoster, isoDay, shiftText, weekDays, weekStart } from "./workforce";

export function RosterTab({ domain }: { domain: ShiftDomain }) {
  const { propertyId, properties, can } = useAuth();
  const canManage = can(`${domain}.shifts.manage`);
  const toast = useToast();
  const invalidate = useInvalidate();
  const [sp, setSp] = useSearchParams();
  const today = isoDay(new Date());
  const week = weekStart(/^\d{4}-\d{2}-\d{2}$/.test(sp.get("week") ?? "") ? sp.get("week")! : today);
  const days = weekDays(week);
  const setWeek = (d: string) => { const n = new URLSearchParams(sp); n.set("week", d); setSp(n, { replace: true }); };
  const shifts = useShifts(domain, propertyId);
  const roster = useQuery({
    queryKey: ["roster", domain, propertyId ?? null, week],
    queryFn: ({ signal }) => api<ListResponse<RosterEntry>>(`${domain}/roster`, { query: { property_id: propertyId ?? undefined, from: week, to: days[6] }, signal }).then((r) => r.data),
  });
  const byCell = useMemo(() => groupRoster(roster.data ?? []), [roster.data]);
  const rows = useMemo(() => {
    const used = new Set((roster.data ?? []).map((e) => e.shift_id));
    return (shifts.data ?? []).filter((s) => s.is_active || used.has(s.id)).sort((a, b) => a.sort_order - b.sort_order || a.start_time.localeCompare(b.start_time));
  }, [shifts.data, roster.data]);
  const [assign, setAssign] = useState<{ shiftId?: string; date?: string } | null>(null);
  const [cancelOf, setCancelOf] = useState<RosterEntry | null>(null);
  const [copyOpen, setCopyOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  // salin minggu butuh satu property: header property, atau satu-satunya property user
  const copyProperty = propertyId ?? (properties.length === 1 ? properties[0].id : null);

  const cancel = async (e: RosterEntry) => {
    setBusy(true);
    try {
      await api(`${domain}/roster/${e.id}`, { method: "DELETE" });
      invalidate("roster", "on-duty");
      toast.action("cancelled", `Roster ${e.user_name} (${fmtDate(e.shift_date)})`);
      setCancelOf(null);
    } catch (err) {
      toast.failed("cancelled", err, "Roster");
    } finally {
      setBusy(false);
    }
  };
  const copyWeek = async () => {
    if (!copyProperty) return;
    setBusy(true);
    try {
      const r = await api<RosterAssignResult>(`${domain}/roster/copy-week`, { body: { property_id: copyProperty, from_week_start: week, to_week_start: addDays(week, 7) } });
      invalidate("roster", "on-duty");
      toast.success(`Roster disalin ke minggu ${fmtDate(addDays(week, 7))}: ${r.created} penugasan dibuat${r.skipped ? `, ${r.skipped} sudah ada` : ""}`);
      setCopyOpen(false);
    } catch (err) {
      toast.failed("created", err, "Salinan roster");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Button size="icon-sm" variant="secondary" aria-label="Minggu sebelumnya" onClick={() => setWeek(addDays(week, -7))}><Icon name="chevron_left" size={18} /></Button>
        <span className="min-w-44 text-center text-body font-semibold tnum" aria-live="polite">{fmtDate(week)} – {fmtDate(days[6])}</span>
        <Button size="icon-sm" variant="secondary" aria-label="Minggu berikutnya" onClick={() => setWeek(addDays(week, 7))}><Icon name="chevron_right" size={18} /></Button>
        {week !== weekStart(today) && <Button size="sm" variant="ghost" onClick={() => setWeek(weekStart(today))}>Minggu ini</Button>}
        <span className="ml-auto flex flex-wrap gap-2">
          {canManage && (
            <span title={copyProperty ? undefined : "Pilih property di header untuk menyalin roster"}>
              <Button size="sm" variant="secondary" icon="event_repeat" disabled={!copyProperty || !(roster.data ?? []).length} onClick={() => setCopyOpen(true)}>Salin ke minggu depan</Button>
            </span>
          )}
          {canManage && <Button size="sm" icon="add" disabled={!rows.some((s) => s.is_active)} onClick={() => setAssign({})}>Assign roster</Button>}
        </span>
      </div>
      {shifts.data && rows.length === 0 && (
        <Alert variant="info" title="Belum ada shift aktif">Definisikan shift di tab Shift terlebih dahulu; roster disusun per shift × tanggal.</Alert>
      )}
      {roster.error && !roster.data ? (
        <QueryErrorState error={roster.error} onRetry={() => roster.refetch()} />
      ) : roster.isLoading || shifts.isLoading ? (
        <TableSkeleton rows={4} columns={8} />
      ) : rows.length > 0 ? (
        <Table className="min-w-[960px]" data-testid="roster-grid">
          <THead>
            <tr>
              <TH style={{ width: 170 }}>Shift</TH>
              {days.map((d) => <TH key={d} aria-current={d === today ? "date" : undefined} className={cn(d === today && "underline underline-offset-4")}>{dayLabel(d)}</TH>)}
            </tr>
          </THead>
          <TBody>
            {rows.map((s) => (
              <TR key={s.id}>
                <TD className="align-top">
                  <div className="flex items-center gap-2"><ShiftDot color={s.color} /><span className="font-mono text-[13px] font-semibold">{s.code}</span></div>
                  <div className="whitespace-nowrap text-xs text-on-surface-variant tnum">{shiftText(s)}</div>
                  <div className="whitespace-nowrap text-xs text-on-surface-variant">min. {s.min_staff} staf{s.is_active ? "" : " · nonaktif"}</div>
                </TD>
                {days.map((d) => (
                  <RosterCell key={d} shift={s} date={d} entries={byCell.get(`${s.id}|${d}`) ?? []} canManage={canManage} onAdd={() => setAssign({ shiftId: s.id, date: d })} onCancel={setCancelOf} />
                ))}
              </TR>
            ))}
          </TBody>
        </Table>
      ) : null}
      {assign && <AssignRosterDialog domain={domain} shifts={shifts.data ?? []} days={days} initial={assign} onClose={() => setAssign(null)} />}
      <ConfirmDialog open={!!cancelOf} onOpenChange={(o) => !o && setCancelOf(null)} title={`Batalkan roster ${cancelOf?.user_name ?? ""}?`} description={cancelOf ? `${cancelOf.shift_name} · ${fmtDate(cancelOf.shift_date)}. Penugasan dibatalkan (riwayat tetap tersimpan); tidak dapat dibatalkan bila staf sudah clock-in.` : undefined} confirmLabel="Batalkan roster" destructive loading={busy} onConfirm={() => cancelOf && void cancel(cancelOf)} />
      <ConfirmDialog open={copyOpen} onOpenChange={setCopyOpen} title="Salin roster ke minggu depan?" description={`Penugasan ${fmtDate(week)} – ${fmtDate(days[6])} disalin ke ${fmtDate(addDays(week, 7))} – ${fmtDate(addDays(week, 13))}. Penugasan yang sudah ada dilewati; shift nonaktif tidak disalin.`} confirmLabel="Salin" loading={busy} onConfirm={() => void copyWeek()} />
    </div>
  );
}

function RosterCell({ shift, date, entries, canManage, onAdd, onCancel }: { shift: ShiftDefinition; date: string; entries: RosterEntry[]; canManage: boolean; onAdd: () => void; onCancel: (e: RosterEntry) => void }) {
  const short = shift.is_active && entries.length < shift.min_staff;
  return (
    <TD className="align-top">
      <div className="flex min-h-10 flex-col gap-1">
        <div className="flex items-center justify-between gap-1 text-xs tnum">
          <span className={cn("text-on-surface-variant", short && entries.length > 0 && "font-semibold text-on-warning-container")} title={`${entries.length} dari minimal ${shift.min_staff} staf`}>{entries.length}/{shift.min_staff}</span>
          {canManage && shift.is_active && (
            <button type="button" onClick={onAdd} className="inline-flex rounded-[var(--radius-sm)] p-0.5 text-on-surface-variant hover:bg-surface-container hover:text-primary" aria-label={`Tambah staf ${shift.code} ${dayLabel(date)}`}>
              <Icon name="add" size={16} />
            </button>
          )}
        </div>
        {entries.map((e) => (
          <div key={e.id} className="rounded-[var(--radius-sm)] bg-surface-container px-2 py-1 text-xs">
            <div className="flex items-start justify-between gap-1">
              <span className="min-w-0 truncate font-medium text-on-surface" title={e.user_name}>{e.user_name}</span>
              {canManage && !e.clock_in_at && (
                <button type="button" onClick={() => onCancel(e)} className="inline-flex shrink-0 text-on-surface-variant hover:text-on-error-container" aria-label={`Batalkan roster ${e.user_name}`}>
                  <Icon name="close" size={14} />
                </button>
              )}
            </div>
            {(e.post || e.team_name) && <div className="truncate text-on-surface-variant" title={[e.post, e.team_name].filter(Boolean).join(" · ")}>{[e.post, e.team_name].filter(Boolean).join(" · ")}</div>}
            {e.attendance !== "upcoming" && <StatusBadge objectType="roster_attendance" status={e.attendance} className="mt-1" />}
          </div>
        ))}
      </div>
    </TD>
  );
}

function AssignRosterDialog({ domain, shifts, days, initial, onClose }: { domain: ShiftDomain; shifts: ShiftDefinition[]; days: string[]; initial: { shiftId?: string; date?: string }; onClose: () => void }) {
  const { can } = useAuth();
  const toast = useToast();
  const invalidate = useInvalidate();
  const first = shifts.find((s) => s.id === initial.shiftId);
  const [pid, setPid] = usePropertyChoice(first?.property_id);
  const options = shifts.filter((s) => s.is_active && (!pid || s.property_id === pid));
  const [f, setF] = useState({ shift_id: initial.shiftId ?? options[0]?.id ?? "", dates: initial.date ? [initial.date] : ([] as string[]), team_id: null as string | null, user_ids: [] as string[], post: "", note: "" });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const toggleDate = (d: string) => setF((s) => ({ ...s, dates: s.dates.includes(d) ? s.dates.filter((x) => x !== d) : [...s.dates, d].sort() }));
  const submit = async () => {
    const e: Record<string, string> = {};
    if (!f.shift_id) e.shift_id = "Pilih shift";
    if (!f.dates.length) e.dates = "Pilih minimal satu tanggal";
    if (!f.user_ids.length) e.user_ids = "Pilih minimal satu staf";
    setErrors(e);
    if (Object.keys(e).length) return;
    setBusy(true);
    try {
      const r = await api<RosterAssignResult>(`${domain}/roster`, { body: { shift_id: f.shift_id, dates: f.dates, user_ids: f.user_ids, team_id: f.team_id, post: f.post.trim() || null, note: f.note.trim() || null } });
      invalidate("roster", "on-duty");
      toast.success(`${r.created} penugasan roster dibuat${r.skipped ? ` · ${r.skipped} sudah ada (dilewati)` : ""}`);
      onClose();
    } catch (err) {
      setErrors(fieldErrorsOf(err));
      toast.failed("created", err, "Roster");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Assign roster" description="Satu staf × satu tanggal × satu shift = satu penugasan. Penugasan yang sudah ada dilewati.">
        <div className="space-y-4">
          {!first && <PropertyField value={pid} onChange={(v) => { setPid(v); setF((s) => ({ ...s, shift_id: "", user_ids: [] })); }} />}
          <Field label="Shift" required error={errors.shift_id}>
            <NativeSelect value={f.shift_id} onChange={(e) => setF({ ...f, shift_id: e.target.value })}>
              <option value="">Pilih shift…</option>
              {options.map((s) => <option key={s.id} value={s.id}>{s.code} · {shiftText(s)}</option>)}
            </NativeSelect>
          </Field>
          <Field label={`Tanggal (${f.dates.length})`} required error={errors.dates}>
            <div className="flex flex-wrap gap-1" role="group" aria-label="Tanggal">
              {days.map((d) => {
                const on = f.dates.includes(d);
                return <button key={d} type="button" aria-pressed={on} onClick={() => toggleDate(d)} className={cn("h-9 rounded-[var(--radius-md)] border px-2 text-xs font-semibold tnum", on ? "border-primary bg-primary text-on-primary" : "border-border bg-surface text-on-surface-variant hover:bg-surface-container")}>{dayLabel(d)}</button>;
              })}
            </div>
          </Field>
          {can("iam.teams.view") && <Field label="Team (opsional)"><TeamPicker propertyId={pid} domain={domain} value={f.team_id} onChange={(v) => setF({ ...f, team_id: v, user_ids: [] })} /></Field>}
          <Field label={`Staf (${f.user_ids.length})`} required error={errors.user_ids}>
            <StaffMultiSelect domain={domain} propertyId={pid} teamId={f.team_id} value={f.user_ids} onChange={(ids) => setF({ ...f, user_ids: ids })} />
          </Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label={domain === "security" ? "Pos jaga" : "Zona"}><Input value={f.post} onChange={(e) => setF({ ...f, post: e.target.value })} placeholder={domain === "security" ? "mis. Pos Lobby" : "mis. Lantai 1–5"} /></Field>
            <Field label="Catatan"><Input value={f.note} onChange={(e) => setF({ ...f, note: e.target.value })} /></Field>
          </div>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} onClick={() => void submit()}>Simpan roster</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
