// UI mockups ringan (Website PRD §8.3 "UI Mockups"): layar dashboard & Staff App digambar dari token, bukan screenshot statis,
// sehingga selalu konsisten dengan identitas produk dan ringan untuk web.
import { Icon } from "./Icon";
import { cn } from "./ui";

const ROWS = [
  { id: "WO-2026-000418", title: "AHU-03 not cooling", where: "Tower A · Mechanical Room", status: "In Progress", tone: "info", prio: "High" },
  { id: "SR-2026-001102", title: "Water leak in unit 1201", where: "Tower A · Floor 12", status: "Assigned", tone: "primary", prio: "High" },
  { id: "TSK-2026-002210", title: "Night patrol, Route A", where: "Tower A · 3 checkpoints", status: "Scheduled", tone: "neutral", prio: "Medium" },
  { id: "WO-2026-000421", title: "Lobby lighting flicker", where: "Ground Floor · Lobby", status: "Completed", tone: "success", prio: "Low" },
];

// nama ikon di lookup ini dipindai scripts/vendor-fonts.py (pola `const *ICONS*`)
const NAV_ICONS = { staff: ["home", "task_alt", "qr_code_scanner", "person"], tenant: ["home", "list_alt", "event", "person"] };

const badge: Record<string, string> = {
  info: "bg-info-container text-on-info-container",
  primary: "bg-primary-container text-on-primary-container",
  neutral: "bg-surface-container-high text-on-surface-variant",
  success: "bg-success-container text-on-success-container",
  warning: "bg-warning-container text-on-warning-container",
};

export function DashboardMock({ className, compact }: { className?: string; compact?: boolean }) {
  return (
    <div className={cn("bv-frame overflow-hidden rounded-[var(--radius-xl)] border border-border bg-surface", className)} role="img" aria-label="BuildingVision dashboard showing today's work orders, requests, and attention items">
      <div className="flex">
        <div className="hidden w-40 shrink-0 flex-col gap-1 bg-primary p-3 text-on-primary sm:flex">
          <div className="mb-3 flex items-center gap-2 px-1 text-xs font-bold"><Icon name="apartment" size={16} /> Graha Pangeran</div>
          {["Overview", "Operations", "Engineering", "Security", "Housekeeping", "Tenant Relation", "Reports"].map((n, i) => (
            <div key={n} className={cn("rounded-[var(--radius-pill)] px-2 py-1.5 text-[11px] font-medium", i === 0 ? "bg-on-primary/15" : "opacity-80")}>{n}</div>
          ))}
        </div>
        <div className="min-w-0 flex-1 p-4">
          <div className="mb-3 flex items-center justify-between">
            <div>
              <div className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant">Today</div>
              <div className="text-sm font-extrabold text-on-surface">What needs attention</div>
            </div>
            <div className="rounded-[var(--radius-pill)] bg-primary px-2.5 py-1 text-[10px] font-semibold text-on-primary">+ Work Order</div>
          </div>
          <div className="mb-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
            {[["Open WO", "18"], ["Overdue", "2"], ["SLA risk", "3"], ["Requests", "7"]].map(([l, v], i) => (
              <div key={l} className="rounded-[var(--radius-lg)] border border-border bg-surface-container-lowest p-2">
                <div className="text-[9px] font-semibold uppercase tracking-wide text-on-surface-variant">{l}</div>
                <div className={cn("text-lg font-extrabold", i === 1 ? "text-error" : i === 2 ? "text-warning" : "text-on-surface")}>{v}</div>
              </div>
            ))}
          </div>
          <div className="overflow-hidden rounded-[var(--radius-lg)] border border-border">
            <div className="grid grid-cols-[1.6fr_1fr_auto] gap-2 bg-primary px-3 py-1.5 text-[9px] font-bold uppercase tracking-wide text-on-primary sm:grid-cols-[1.6fr_1fr_auto_auto]">
              <span>Work item</span><span>Location</span><span className="hidden sm:block">Priority</span><span>Status</span>
            </div>
            {(compact ? ROWS.slice(0, 3) : ROWS).map((r) => (
              <div key={r.id} className="grid grid-cols-[1.6fr_1fr_auto] items-center gap-2 border-t border-border px-3 py-2 text-[11px] sm:grid-cols-[1.6fr_1fr_auto_auto]">
                <div className="min-w-0"><div className="truncate font-semibold text-on-surface">{r.title}</div><div className="text-[10px] text-on-surface-variant">{r.id}</div></div>
                <div className="truncate text-on-surface-variant">{r.where}</div>
                <div className="hidden sm:block"><span className={cn("rounded-[var(--radius-pill)] px-2 py-0.5 text-[10px] font-semibold", r.prio === "High" ? badge.warning : badge.neutral)}>{r.prio}</span></div>
                <span className={cn("rounded-[var(--radius-pill)] px-2 py-0.5 text-[10px] font-semibold", badge[r.tone])}>{r.status}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

export function PhoneMock({ variant = "staff", className }: { variant?: "staff" | "tenant"; className?: string }) {
  const staff = variant === "staff";
  return (
    <div className={cn("bv-frame mx-auto w-full max-w-[260px] overflow-hidden rounded-[36px] border-[6px] border-inverse-surface bg-surface", className)} role="img" aria-label={staff ? "BuildingVision Staff App showing today's tasks" : "BuildingVision Tenant App showing a request in progress"}>
      <div className="bg-primary px-4 pb-4 pt-6 text-on-primary">
        <div className="text-[10px] opacity-80">{staff ? "Good morning, Budi" : "Unit 1201"}</div>
        <div className="text-base font-extrabold">{staff ? "4 tasks today" : "My requests"}</div>
        {staff && <div className="mt-2 inline-flex items-center gap-1 rounded-[var(--radius-pill)] bg-on-primary/15 px-2 py-0.5 text-[10px]"><Icon name="cloud_off" size={12} /> Offline, 2 changes queued</div>}
      </div>
      <div className="space-y-2 p-3">
        {(staff
          ? [["PM Monthly AHU-03", "Due 10:00 · Checklist 5 items", "In Progress", "info"], ["Patrol Route A", "22:00 · 3 checkpoints", "Scheduled", "neutral"], ["Toilet Floor 12", "Cleaning · Photo required", "Assigned", "primary"]]
          : [["Water leak in bathroom", "Technician on the way", "In Progress", "info"], ["Facility booking", "Function room · Sat 10:00", "Approved", "success"], ["Invoice August", "Rp 2.450.000 · Due 20 Aug", "Unpaid", "warning"]]
        ).map(([t, s, st, tone]) => (
          <div key={t} className="rounded-[var(--radius-lg)] border border-border p-2.5">
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0"><div className="truncate text-[12px] font-semibold text-on-surface">{t}</div><div className="text-[10px] text-on-surface-variant">{s}</div></div>
              <span className={cn("shrink-0 rounded-[var(--radius-pill)] px-1.5 py-0.5 text-[9px] font-semibold", badge[tone])}>{st}</span>
            </div>
          </div>
        ))}
        <div className="mt-2 rounded-[var(--radius-pill)] bg-primary py-2 text-center text-[11px] font-semibold text-on-primary">{staff ? "Start task" : "Report an issue"}</div>
      </div>
      <div className="flex justify-around border-t border-border py-2 text-on-surface-variant">
        {(staff ? NAV_ICONS.staff : NAV_ICONS.tenant).map((n, i) => <Icon key={n} name={n} size={20} className={i === 0 ? "text-primary" : ""} />)}
      </div>
    </div>
  );
}
