// Smoke render layar PRD P2 v2.1 Workforce Operations dengan API tiruan (jsdom): dashboard domain + drill-down, beranda
// domain, shift per domain (Shift/Roster/On-duty/Serah Terima + deep link), kompetensi, Cleaning Route run (deep link),
// Asset 360 (Health/Dokumen), dokumen kedaluwarsa, kapasitas tim, saran teknisi, consumable (409 stok), inventory ?item=,
// laporan Security/generik, filter drill-down daftar.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/lib/i18n";

const auth = { perms: [] as string[] };
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    can: (p: string) => auth.perms.includes(p),
    principal: { id: "u1", full_name: "Supervisor Satu", properties: [] },
    propertyId: "p1",
    properties: [{ id: "p1", name: "Menara Demo", code: "MD" }],
  }),
}));
vi.mock("@/lib/profile", async (importOriginal) => ({ ...(await importOriginal<typeof import("@/lib/profile")>()), useProfile: () => ({ has: () => true, loading: false }) }));

import { ToastProvider } from "@/components/bv/common";
import { DomainHome } from "@/app/guards";
import DomainDashboardPage from "@/features/dashboards/DomainDashboardPage";
import ShiftsPage from "@/features/workforce/ShiftsPage";
import { WorkforceCapacityPanel } from "@/features/workforce/CapacityPanel";
import CleaningRoutesPage from "@/features/housekeeping/CleaningRoutesPage";
import { ConsumablesCard, RoutePositionCard } from "@/features/housekeeping/CleaningTaskPanels";
import AssetDetailPage from "@/features/assets/AssetDetailPage";
import ExpiringDocumentsPage from "@/features/assets/ExpiringDocumentsPage";
import InventoryPage from "@/features/inventory/InventoryPage";
import ReportsPage from "@/features/reports/ReportsPage";
import FindingListPage from "@/features/operations/FindingListPage";
import WorkItemListPage from "@/features/operations/WorkItemListPage";
import type { WorkItem } from "@/api/types";

const now = new Date().toISOString();
const kpi = (key: string, label: string, value: number, unit: string, severity: string, drill: string) => ({ key, label, value, unit, severity, hint: `Definisi ${label}`, drill_down: drill });
const dash = (domain: string, extra: Record<string, unknown>) => ({ domain, property_id: "p1", location_id: null, from: "2026-08-29", to: "2026-09-27", generated_at: now, kpis: [], breakdowns: {}, attention: [], attention_total: 0, ...extra });
const engDash = dash("engineering", {
  kpis: [
    kpi("pm_overdue", "Overdue PM", 3, "count", "warning", "/engineering/preventive-maintenance?status=overdue&property_id=p1"),
    kpi("pm_compliance", "PM Compliance", 64.5, "pct", "critical", "/reports/maintenance?from=2026-08-29&to=2026-09-27&property_id=p1"),
    kpi("maintenance_cost", "Maintenance Cost", 1250000, "idr", "normal", "/reports/work-orders?from=2026-08-29&to=2026-09-27"),
  ],
  distribution: { healthy: 6, warning: 3, critical: 1, offline: 0, unknown: 2 },
  breakdowns: {
    building: [{ key: "b1", label: "Tower A", values: { open_work_orders: 4, overdue_pm: 1, at_risk_assets: 2 }, drill_down: "/operations/work-orders?open=true&location_id=b1" }],
    equipment_category: [{ key: "HVAC", label: "HVAC", values: { assets: 5, at_risk_assets: 2, critical_assets: 1, open_work_orders: 3 }, drill_down: "/assets?category=HVAC" }],
    team: [],
  },
  attention: [{ category: "document_expiring", severity: "warning", object_type: "asset_document", object_id: "d1", label: "AST-HVAC-000001 · Warranty", title: "Chiller — berlaku s/d 10 Okt 2026", status: "expiring", priority: null, location_path: null, due_at: null, age_minutes: 0, since: now, assignee_name: null, allowed_actions: ["view"], deep_link: "/assets/a1?tab=documents" }],
  attention_total: 1,
});
const secDash = dash("security", {
  kpis: [kpi("active_emergencies", "Active Emergency", 1, "count", "critical", "/security/emergency?status=raised,acknowledged,responding"), kpi("security_response_time", "Security Response Time", 12.5, "minutes", "normal", "/reports/incidents")],
  breakdowns: {
    route: [{ key: "r1", label: "Rute Lobby", values: { patrols: 10, completed: 9, checkpoints_scanned: 40, checkpoints_missed: 2 }, drill_down: "/security/patrol-routes/r1" }],
    incident_category: [{ key: "unauthorized_access", label: "unauthorized_access", values: { reported: 2, open: 1, critical: 0 }, drill_down: "/operations/incidents?category=unauthorized_access" }],
    building: [],
  },
});
const hkDash = dash("housekeeping", {
  kpis: [kpi("inspection_score", "Inspection Score", 88.4, "score", "warning", "/housekeeping/inspections?from=2026-08-29&to=2026-09-27")],
  breakdowns: {
    routes_today: [{ key: "rr1", label: "CRT-000001 Route Lantai 12", values: { total_stops: 5, completed_stops: 2, status: 1 }, drill_down: "/housekeeping/routes?run=rr1" }],
    area: [{ key: "l1", label: "Toilet Lt. 12", values: { avg_score: 62, inspections: 3, condition: 0 }, drill_down: "/housekeeping/inspections?location_id=l1" }],
    team: [], consumables: [],
  },
});
const shifts = [
  { id: "s1", property_id: "p1", domain: "security", code: "PAGI", name: "Pagi", start_time: "07:00", end_time: "15:00", crosses_midnight: false, duration_minutes: 480, break_minutes: 60, min_staff: 2, color: "info", sort_order: 1, is_active: true, version: 1 },
  { id: "s2", property_id: "p1", domain: "security", code: "MALAM", name: "Malam", start_time: "23:00", end_time: "07:00", crosses_midnight: true, duration_minutes: 480, break_minutes: 0, min_staff: 1, color: null, sort_order: 2, is_active: true, version: 1 },
];
const roster = [{ id: "ra1", property_id: "p1", domain: "security", shift_id: "s1", shift_code: "PAGI", shift_name: "Pagi", shift_date: "2026-09-28", user_id: "u2", user_name: "Budi Satpam", team_id: null, team_name: null, post: "Pos Lobby", starts_at: now, ends_at: now, status: "scheduled", note: null, clock_in_at: now, clock_out_at: null, late_minutes: 10, attendance: "late" }];
const onDuty = { domain: "security", property_id: "p1", generated_at: now, scheduled: 3, on_duty: 2, absent: 1, shortage: 1, shifts: [{ shift_id: "s1", name: "Pagi", starts_at: now, ends_at: now, min_staff: 3, scheduled: 3, on_duty: 2, shortage: 1 }], teams: [{ team_id: null, team_name: "(tanpa team)", scheduled: 3, on_duty: 2, absent: 1 }], staff: roster, unscheduled: [], link: "/security/shifts?tab=on-duty" };
const handover = {
  id: "h1", property_id: "p1", domain: "security", handover_number: "HOV-2026-000001", shift_id: "s1", shift_name: "Pagi", shift_date: "2026-09-27", handed_over_by: "u2", handed_over_by_name: "Budi Satpam", received_by: null, received_by_name: null, post: "Pos Lobby", notes: "CCTV lantai 3 mati",
  open_items: { generated_at: now, groups: { active_emergencies: [{ object_type: "emergency_alert", id: "e1", number: "EMG-2026-0001", title: "EMERGENCY — fire", status: "raised", deep_link: "/security/emergency/e1" }] }, counts: { active_emergencies: 1 } },
  status: "submitted", handed_over_at: now, acknowledged_at: null, allowed_actions: ["view", "acknowledge"],
};
const run = {
  id: "rr1", property_id: "p1", route_id: "cr1", route_code: "CRT-000001", route_name: "Route Lantai 12", cleaning_type: "routine", shift_name: "Pagi", run_date: "2026-09-27", assignee_user_id: "u3", assignee_name: "Sari HK", team_id: null, team_name: null,
  status: "in_progress", total_stops: 3, completed_stops: 1, progress_pct: 33, started_at: now, completed_at: null,
  stops: [
    { task_id: "t1", task_number: "TSK-2026-000001", title: "Routine Cleaning — Route Lantai 12 · 1/3 Toilet", status: "completed", sort_order: 1, location_id: "l1", location_name: "Toilet Pria", scheduled_start_at: now, due_at: now, completed_at: now },
    { task_id: "t2", task_number: "TSK-2026-000002", title: "Routine Cleaning — Route Lantai 12 · 2/3 Pantry", status: "assigned", sort_order: 2, location_id: "l2", location_name: "Pantry", scheduled_start_at: now, due_at: now, completed_at: null },
    { task_id: "t3", task_number: "TSK-2026-000003", title: "Routine Cleaning — Route Lantai 12 · 3/3 Koridor", status: "assigned", sort_order: 3, location_id: "l3", location_name: "Koridor", scheduled_start_at: now, due_at: now, completed_at: null },
  ],
  next_stop: null as unknown,
};
run.next_stop = run.stops[1];
const route = { id: "cr1", property_id: "p1", route_code: "CRT-000001", name: "Route Lantai 12", description: null, cleaning_type: "routine", shift_id: null, shift_name: null, start_time: "08:00", weekdays: [1, 2, 3, 4, 5], responsible_team_id: null, responsible_team_name: null, default_assignee_user_id: "u3", default_assignee_name: "Sari HK", checklist_template_id: null, requires_photo: false, priority: "medium", is_active: true, total_minutes: 45, stops: [], today_run: run, version: 1 };
const asset = { id: "a1", property_id: "p1", asset_code: "AST-HVAC-000001", name: "Chiller 1", equipment_id: "eq1", category_code: "HVAC", category_name: "HVAC", type_name: "Chiller", location_id: "l1", location_name: "Atap", location_path: "Tower A / Atap", status: "active", criticality: "high", manufacturer: null, model: null, serial_number: null, installed_at: null, warranty_until: null, specifications: {}, notes: null, qr_code: null, qr_url: null, open_work_orders: 1, next_pm_due: null, last_maintenance_at: null, health_score: 72, health_status: "warning", health_factors: [], health_updated_at: now, document_count: 1, expiring_documents: 1, version: 3 };
const doc = { id: "d1", property_id: "p1", asset_id: "a1", asset_code: "AST-HVAC-000001", asset_name: "Chiller 1", document_code: "DOC-2026-000001", document_type: "warranty", type_label: "Warranty", title: "Warranty Chiller", document_number: "W-1", issuer: "Vendor HVAC", issued_on: "2025-10-01", expires_on: "2026-10-09", status: "expiring", days_to_expire: 12, attachment_id: null, file_name: null, notes: null, is_active: true, created_at: now, version: 1 };
const health = { asset_id: "a1", score: 72, status: "warning", updated_at: now, factors: [{ code: "pm_overdue", label: "PM overdue", count: 1, points: -20, link: "/engineering/preventive-maintenance?asset_id=a1&status=overdue" }, { code: "open_findings", label: "Finding terbuka", count: 1, points: -8, link: "/findings?asset_id=a1&unresolved=true" }] };
const capacity = {
  property_id: "p1", generated_at: now,
  domains: [
    { domain: "engineering", scheduled: 0, on_duty: 2, absent: 0, min_staff: 0, shortage: 0, capacity_minutes: 480, workload_minutes: 300, open_items: 4, load_ratio: 0.63, status: "ok", link: "/engineering" },
    { domain: "security", scheduled: 3, on_duty: 2, absent: 1, min_staff: 3, shortage: 1, capacity_minutes: 240, workload_minutes: 400, open_items: 6, load_ratio: 1.67, status: "over", link: "/security/shifts" },
    { domain: "housekeeping", scheduled: 0, on_duty: 0, absent: 0, min_staff: 0, shortage: 0, capacity_minutes: 0, workload_minutes: 0, open_items: 0, load_ratio: 0, status: "idle", link: "/housekeeping/shifts" },
  ],
  total: { domain: "total", scheduled: 3, on_duty: 4, absent: 1, min_staff: 3, shortage: 1, capacity_minutes: 720, workload_minutes: 700, open_items: 10, load_ratio: 0.97, status: "", link: "/overview" },
};

const item = {
  id: "i1", item_code: "ITM-000001", name: "Sabun cair", description: null, category: "consumable", equipment_category_code: null, unit: "liter",
  min_stock: 8, unit_cost: 20000, barcode: null, is_active: true, total_quantity: 2, low_stock: true, version: 1,
  levels: [{ stock_location_id: "sl1", stock_location_name: "Gudang HK", property_id: "p1", quantity: 2 }],
};
const securityReport = {
  name: "security", property_id: null, from: "2026-09-01T00:00:00+07:00", to: "2026-09-27T00:00:00+07:00",
  summary: { patrol_total: 24, patrol_completed: 23, patrol_completion_pct: 91.7, checkpoints_total: 96, checkpoints_scanned: 92, checkpoints_missed: 4, checkpoint_compliance_pct: 95.8,
    incidents_reported: 3, incidents_critical: 1, incidents_resolved: 2, unauthorized_entry: 1, avg_response_minutes: 12.5, emergencies_raised: 1, emergencies_resolved: 1,
    emergencies_false_alarm: 0, emergencies_escalated: 0, avg_emergency_ack_minutes: 2, visitors_checked_in: 40, parking_violations: 2 },
  series: [],
  breakdowns: {
    route: [],
    missed_checkpoint: [{ key: "c1", label: "Pintu Darurat B1", values: { missed: 3 } }],
    incident_category: [{ key: "unauthorized_access", label: "unauthorized_access", values: { count: 1, critical: 0 } }],
    emergency_type: [{ key: "fire", label: "fire", values: { count: 1, avg_ack_minutes: 2 } }],
  },
};

const list = (data: unknown[]) => ({ data, next_cursor: null });
type Reply = { status?: number; body: unknown };
const overrides = new Map<string, Reply>();
function respond(url: string, method: string): Reply {
  const u = new URL(url, "http://localhost");
  const p = u.pathname.replace("/api/v1/", "");
  const hit = overrides.get(`${method} ${p}`);
  if (hit) return hit;
  const ok = (body: unknown) => ({ body });
  if (p === "dashboards/engineering") return ok(engDash);
  if (p === "dashboards/security") return ok(secDash);
  if (p === "dashboards/housekeeping") return ok(hkDash);
  if (p === "locations/tree") return ok({ id: "p1", name: "Menara Demo", location_type: "property", children: [] });
  if (p === "security/shifts") return ok(list(shifts));
  if (p === "security/roster") return ok(list(roster));
  if (p === "security/on-duty") return ok(onDuty);
  if (p === "security/handovers") return ok(list([handover]));
  if (p === "security/staff") return ok(list([{ user_id: "u2", full_name: "Budi Satpam", team_ids: ["tm1"], team_names: ["Security Pagi"] }]));
  if (p === "cleaning-routes") return ok(list([route]));
  if (p === "cleaning-route-runs/rr1") return ok(run);
  if (p === "assets/a1") return ok(asset);
  if (p === "assets/a1/documents") return ok(list([doc]));
  if (p === "assets/a1/health") return ok(health);
  if (p === "asset-documents/expiring") return ok(list([{ kind: "warranty", id: "a1", asset_id: "a1", asset_code: "AST-HVAC-000001", asset_name: "Chiller 1", title: "Warranty Chiller 1", document_type: "warranty", expires_on: "2026-10-09", status: "expiring", days_to_expire: 12, deep_link: "/assets/a1?tab=documents", property_id: "p1" }]));
  if (p === "overview/workforce-capacity") return ok(capacity);
  if (p === "tasks/t2/consumables") return ok({ data: [{ id: "cu1", task_id: "t2", item_id: "i1", item_code: "ITM-000001", item_name: "Sabun cair", unit: "liter", stock_location_id: "sl1", stock_location_name: "Gudang HK", quantity: 1, unit_cost: 15000, total_cost: 15000, note: null, recorded_by_name: "Sari HK", recorded_at: now }], total_cost: 15000 });
  if (p === "inventory/items/i1") return ok(item);
  if (p === "inventory/items") return ok(list([item]));
  if (p === "reports") return ok(list([{ name: "security", title: "Security Report", description: "Patrol & checkpoint compliance" }]));
  if (p === "reports/security") return ok(securityReport);
  if (p === "reports/new-report") return ok({ name: "new-report", property_id: null, from: now, to: now, summary: { foo_count: 3, bar_pct: 50 }, series: [], breakdowns: {} });
  return ok(list([]));
}

// recharts ResponsiveContainer membutuhkan ResizeObserver (tidak ada di jsdom)
class ResizeObserverStub { observe() {} unobserve() {} disconnect() {} }

beforeEach(() => {
  auth.perms = [];
  overrides.clear();
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const r = respond(url, (init?.method ?? "GET").toUpperCase());
    const status = r.status ?? 200;
    return { ok: status < 400, status, statusText: String(status), text: async () => JSON.stringify(r.body), json: async () => r.body, headers: new Headers() };
  }));
});
afterEach(() => vi.unstubAllGlobals());

const calls = (part: string) => vi.mocked(fetch).mock.calls.map((c) => String(c[0])).filter((u) => u.includes(part));

function renderAt(path: string, pattern: string, element: React.ReactNode, extra?: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ToastProvider>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path={pattern} element={element} />
            {extra}
          </Routes>
        </MemoryRouter>
      </ToastProvider>
    </QueryClientProvider>,
  );
}

describe("Dashboard domain (P2-ENG/SDB/HDB)", () => {
  it("Engineering: KPI berunit + severity + drill-down, distribusi health, breakdown, attention", async () => {
    auth.perms = ["overview.dashboard.view", "engineering.assets.view"];
    renderAt("/engineering", "/engineering", <DomainDashboardPage domain="engineering" />);
    const overdue = await screen.findByTestId("kpi-pm_overdue");
    expect(overdue).toHaveAttribute("href", "/engineering/preventive-maintenance?status=overdue&property_id=p1");
    expect(overdue).toHaveAttribute("data-severity", "warning");
    expect(within(overdue).getByText("Perlu perhatian")).toBeInTheDocument();
    expect(within(screen.getByTestId("kpi-pm_compliance")).getByText("64,5%")).toBeInTheDocument();
    expect(within(screen.getByTestId("kpi-maintenance_cost")).getByText("Rp 1.250.000")).toBeInTheDocument();
    expect(screen.getByTestId("health-critical")).toHaveAttribute("href", "/assets?health_status=critical&property_id=p1");
    expect(screen.getByText("Tower A").closest("a")).toHaveAttribute("href", "/operations/work-orders?open=true&location_id=b1");
    expect(screen.getByText("HVAC").closest("a")).toHaveAttribute("href", "/assets?category=HVAC");
    expect(screen.getByText("Tidak ada WO terbuka yang ditugaskan ke team.")).toBeInTheDocument();
    expect(screen.getByText("Dokumen Kedaluwarsa")).toBeInTheDocument();
    expect(screen.getByText("AST-HVAC-000001 · Warranty").closest("a")).toHaveAttribute("href", "/assets/a1?tab=documents");
    expect(calls("dashboards/engineering")[0]).toContain("property_id=p1");
  });
  it("Security: kategori incident berlabel, rute ke detail rute patrol, menit respons", async () => {
    auth.perms = ["overview.dashboard.view", "security.incidents.view"];
    renderAt("/security", "/security", <DomainDashboardPage domain="security" />);
    expect(await screen.findByText("Akses tidak sah")).toBeInTheDocument();
    expect(screen.getByText("Rute Lobby").closest("a")).toHaveAttribute("href", "/security/patrol-routes/r1");
    expect(within(screen.getByTestId("kpi-security_response_time")).getByText("12,5 mnt")).toBeInTheDocument();
    expect(screen.getByTestId("kpi-active_emergencies")).toHaveAttribute("href", "/security/emergency?status=raised,acknowledged,responding");
  });
  it("Housekeeping: progres route hari ini, kondisi area, skor /100; filter periode dikirim ke server", async () => {
    auth.perms = ["overview.dashboard.view", "housekeeping.cleaning.view"];
    renderAt("/housekeeping?from=2026-09-01&to=2026-09-27", "/housekeeping", <DomainDashboardPage domain="housekeeping" />);
    expect(await screen.findByText("2/5 area")).toBeInTheDocument();
    expect(screen.getByText("Buruk")).toBeInTheDocument();
    expect(screen.getByText("CRT-000001 Route Lantai 12").closest("a")).toHaveAttribute("href", "/housekeeping/routes?run=rr1");
    expect(within(screen.getByTestId("kpi-inspection_score")).getByText("/100")).toBeInTheDocument();
    expect(calls("dashboards/housekeeping")[0]).toMatch(/from=2026-09-01.*to=2026-09-27/);
  });
  it("beranda domain: Dashboard bila boleh, selain itu menu pertama grup", async () => {
    const tree = (
      <MemoryRouter initialEntries={["/engineering"]}>
        <Routes>
          <Route path="/engineering" element={<DomainHome domain="engineering" fallback="/engineering/preventive-maintenance"><p>DASHBOARD</p></DomainHome>} />
          <Route path="/engineering/preventive-maintenance" element={<p>PM LIST</p>} />
          <Route path="/engineering/corrective-maintenance" element={<p>CM LIST</p>} />
        </Routes>
      </MemoryRouter>
    );
    auth.perms = ["overview.dashboard.view", "engineering.assets.view"];
    const { unmount } = render(tree);
    expect(screen.getByText("DASHBOARD")).toBeInTheDocument();
    unmount();
    auth.perms = ["operations.work_orders.view"];
    render(tree);
    expect(screen.getByText("CM LIST")).toBeInTheDocument();
  });
});

describe("Shift per domain (D-P2-05)", () => {
  it("tab Shift: jam, durasi, lintas tengah malam; tombol kelola hanya dengan manage", async () => {
    auth.perms = ["security.shifts.view"];
    const { unmount } = renderAt("/security/shifts", "/security/shifts", <ShiftsPage domain="security" />);
    expect(await screen.findByText("PAGI")).toBeInTheDocument();
    expect(screen.getByText("(+1 hari)")).toBeInTheDocument();
    expect(screen.getAllByText(/8 jam/).length).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: /Tambah Shift/ })).not.toBeInTheDocument();
    expect(screen.getByText("Security Shift Management")).toBeInTheDocument();
    unmount();
    auth.perms = ["security.shifts.view", "security.shifts.manage"];
    renderAt("/security/shifts", "/security/shifts", <ShiftsPage domain="security" />);
    expect(await screen.findByRole("button", { name: /Tambah Shift/ })).toBeInTheDocument();
  });
  it("tab Roster: grid minggu dari URL, status kehadiran, jumlah vs minimal staf", async () => {
    auth.perms = ["security.shifts.view", "security.shifts.manage"];
    renderAt("/security/shifts?tab=roster&week=2026-09-28", "/security/shifts", <ShiftsPage domain="security" />);
    expect(await screen.findByText("Budi Satpam")).toBeInTheDocument();
    expect(screen.getByText("Terlambat")).toBeInTheDocument();
    expect(screen.getByText("Pos Lobby")).toBeInTheDocument();
    expect(screen.getByTitle("1 dari minimal 2 staf")).toBeInTheDocument();
    expect(calls("security/roster")[0]).toMatch(/from=2026-09-28.*to=2026-10-04/);
    expect(screen.getByRole("button", { name: /Assign roster/ })).toBeInTheDocument();
  });
  it("tab On-duty (deep link Attention Required ?tab=on-duty): kekurangan staf per shift", async () => {
    auth.perms = ["security.shifts.view"];
    renderAt("/security/shifts?tab=on-duty", "/security/shifts", <ShiftsPage domain="security" />);
    expect(await screen.findByText("Kekurangan staf")).toBeInTheDocument();
    expect(screen.getAllByText("Budi Satpam").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Tidak hadir").length).toBeGreaterThan(0);
  });
  it("deep link notifikasi ?tab=handovers&id= membuka serah terima dengan snapshot & aksi terima", async () => {
    auth.perms = ["security.shifts.view"];
    renderAt("/security/shifts?tab=handovers&id=h1", "/security/shifts", <ShiftsPage domain="security" />);
    expect(await screen.findByText("CCTV lantai 3 mati")).toBeInTheDocument();
    expect(screen.getByText("EMERGENCY — Kebakaran")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Terima serah terima/ })).toBeInTheDocument();
  });
});

describe("Housekeeping (Cleaning Route, consumable)", () => {
  it("deep link ?run= membuka run: urutan stop, stop berikutnya, tautan task", async () => {
    auth.perms = ["housekeeping.cleaning_routes.view"];
    renderAt("/housekeeping/routes?run=rr1", "/housekeeping/routes", <CleaningRoutesPage />);
    const detail = await screen.findByTestId("route-run-detail");
    expect(within(detail).getByText(/2\. Pantry/)).toBeInTheDocument();
    expect(within(detail).getByText("· berikutnya")).toBeInTheDocument();
    expect(within(detail).getByText("TSK-2026-000002")).toHaveAttribute("href", "/operations/tasks/t2");
    expect(within(detail).getByText("1 dari 3 area selesai")).toBeInTheDocument();
  });
  it("panel consumable + posisi route; 409 INSUFFICIENT_STOCK → pesan stok di field jumlah", async () => {
    auth.perms = ["housekeeping.cleaning.view", "inventory.consumable_usage.view", "inventory.consumable_usage.create"];
    overrides.set("POST tasks/t2/consumables", { status: 409, body: { type: "", title: "Conflict", status: 409, code: "INSUFFICIENT_STOCK", detail: "Stok tidak mencukupi" } });
    const task = { id: "t2", number: "TSK-2026-000002", property_id: "p1", status: "in_progress", type: "cleaning", extension: { route_code: "CRT-000001", route_name: "Route Lantai 12", route_run_id: "rr1", route_stop_order: 2, route_total_stops: 3, route_completed_stops: 1, route_run_status: "in_progress" } } as unknown as WorkItem;
    renderAt("/t", "/t", <><RoutePositionCard task={task} /><ConsumablesCard task={task} /></>);
    expect(screen.getByText("2 dari 3")).toBeInTheDocument();
    expect(screen.getByText("Route Lantai 12").closest("a")).toHaveAttribute("href", "/housekeeping/routes?run=rr1");
    expect(await screen.findByText("Sabun cair")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Catat pemakaian/ }));
    const select = await screen.findByRole("combobox", { name: "" });
    await waitFor(() => expect(within(select).getAllByRole("option").length).toBeGreaterThan(1));
    fireEvent.change(select, { target: { value: "i1" } });
    fireEvent.change(screen.getByRole("spinbutton"), { target: { value: "50" } });
    fireEvent.click(screen.getByRole("button", { name: "Simpan" }));
    expect(await screen.findByText("Stok tidak mencukupi di gudang property ini.")).toBeInTheDocument();
    expect(calls("tasks/t2/consumables").length).toBeGreaterThanOrEqual(2);
  });
});

describe("Engineering (Asset 360, dokumen)", () => {
  const assetRoute = (path: string) => renderAt(path, "/assets/:id", <AssetDetailPage />);
  it("?tab=documents (deep link notifikasi): dokumen + masa berlaku + penanda header", async () => {
    auth.perms = ["engineering.assets.view", "engineering.asset_documents.view", "engineering.asset_documents.create"];
    assetRoute("/assets/a1?tab=documents");
    expect(await screen.findByText("Warranty Chiller")).toBeInTheDocument();
    expect(screen.getByTestId("asset-documents")).toHaveTextContent("Segera kedaluwarsa");
    expect(screen.getByRole("button", { name: /Tambah dokumen/ })).toBeInTheDocument();
    expect(screen.getAllByText("1 dokumen").length).toBeGreaterThan(0);
  });
  it("?tab=health: skor, faktor pengurang dapat ditelusuri, hitung ulang dengan izin update", async () => {
    auth.perms = ["engineering.assets.view", "engineering.assets.update"];
    assetRoute("/assets/a1?tab=health");
    expect(await screen.findByTestId("health-score")).toHaveTextContent("72");
    expect(screen.getAllByText("Telusuri")[0].closest("a")).toHaveAttribute("href", "/engineering/preventive-maintenance?asset_id=a1&status=overdue");
    expect(screen.getByRole("button", { name: /Hitung ulang/ })).toBeInTheDocument();
  });
  it("daftar dokumen & warranty kedaluwarsa → Asset 360 tab Dokumen", async () => {
    auth.perms = ["engineering.asset_documents.view"];
    renderAt("/engineering/documents", "/engineering/documents", <ExpiringDocumentsPage />);
    expect(await screen.findByText("Warranty Chiller 1")).toBeInTheDocument();
    expect(screen.getByText("Chiller 1").closest("a")).toHaveAttribute("href", "/assets/a1?tab=documents");
  });
});

describe("Overview & lintas modul", () => {
  it("Kapasitas Tim per domain: status, beban, kekurangan staf", async () => {
    renderAt("/overview", "/overview", <WorkforceCapacityPanel propertyId="p1" />);
    const sec = await screen.findByTestId("capacity-security");
    expect(within(sec).getByText("Melebihi kapasitas")).toBeInTheDocument();
    expect(within(sec).getByText("Kurang 1 staf")).toBeInTheDocument();
    expect(sec).toHaveAttribute("href", "/security/shifts");
    expect(within(screen.getByTestId("capacity-housekeeping")).getByText("Tidak ada beban")).toBeInTheDocument();
  });
  it("inventory ?item= membuka ringkasan item (deep link low stock / consumable)", async () => {
    auth.perms = ["inventory.items.view"];
    renderAt("/inventory?item=i1", "/inventory", <InventoryPage />);
    const d = await screen.findByTestId("inventory-item-detail");
    expect(within(d).getByText("low stock")).toBeInTheDocument();
    expect(within(d).getByText("Gudang HK")).toBeInTheDocument();
  });
  it("Security Report: rentang dari URL, kartu & breakdown berlabel", async () => {
    auth.perms = ["reports.reports.view"];
    renderAt("/reports/security?from=2026-09-01&to=2026-09-27", "/reports/:name", <ReportsPage />);
    expect(await screen.findByText("Checkpoint compliance")).toBeInTheDocument();
    expect(screen.getByText("95,8%")).toBeInTheDocument();
    expect(screen.getByText("Akses tidak sah")).toBeInTheDocument();
    expect(screen.getByText("Kebakaran")).toBeInTheDocument();
    expect(screen.getByText("Pintu Darurat B1")).toBeInTheDocument();
    expect(calls("reports/security")[0]).toMatch(/from=2026-09-01.*to=2026-09-27/);
  });
  it("laporan tanpa definisi presentasi dirender generik dengan label ramah", async () => {
    auth.perms = ["reports.reports.view"];
    renderAt("/reports/new-report", "/reports/:name", <ReportsPage />);
    expect(await screen.findByText("Foo count")).toBeInTheDocument();
    expect(screen.getByText("Bar (%)")).toBeInTheDocument();
  });
  it("Findings: filter checkpoint dikirim; rentang tanggal → created_from/created_to", async () => {
    auth.perms = ["operations.findings.view"];
    renderAt("/findings?checkpoint_id=c1&from=2026-09-01&to=2026-09-27", "/findings", <FindingListPage />);
    await waitFor(() => expect(calls("/api/v1/findings?").length).toBeGreaterThan(0));
    const url = calls("/api/v1/findings?")[0];
    expect(url).toContain("checkpoint_id=c1");
    expect(url).toContain("created_from=");
    expect(url).toContain("created_to=");
    expect(url).not.toContain("&from=");
  });
  it("Cleaning ?date=today → scheduled_on (jadwal hari ini, zona waktu property) + type=cleaning", async () => {
    auth.perms = ["operations.tasks.view", "housekeeping.cleaning.view"];
    renderAt("/housekeeping/cleaning?date=today&team_id=t9", "/housekeeping/cleaning", <WorkItemListPage objectType="task" fixedType="cleaning" title="Cleaning" />);
    await waitFor(() => expect(calls("/api/v1/tasks?").length).toBeGreaterThan(0));
    const url = decodeURIComponent(calls("/api/v1/tasks?")[0]);
    expect(url).toMatch(/scheduled_on=\d{4}-\d{2}-\d{2}T00:00:00Z/);
    expect(url).toContain("type=cleaning");
    expect(url).toContain("team_id=t9");
    expect(url).not.toContain("date=today");
  });
});
