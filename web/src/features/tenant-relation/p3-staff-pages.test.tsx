// Smoke render layar staf PRD P3 v2.1 Tenant Experience dengan API tiruan (jsdom): dashboard KPI + drill-down (B-07),
// pengumuman (drawer, pelacakan baca, publish), feedback umum (anonim, tanggapi), isu berulang (SR terkait, tangani), akun tenant
// (filter URL, reset password + WhatsApp), log komunikasi SR & lampiran foto pesan, drill-down daftar Service Request.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/lib/i18n";

const auth = { perms: [] as string[] };
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    can: (p: string) => auth.perms.includes(p),
    principal: { id: "u1", full_name: "TR Officer", properties: [] },
    propertyId: "p1",
    properties: [{ id: "p1", name: "Menara Demo", code: "MD" }],
  }),
}));
vi.mock("@/lib/profile", async (importOriginal) => ({ ...(await importOriginal<typeof import("@/lib/profile")>()), useProfile: () => ({ has: () => true, loading: false, profile: "office", context: null, capabilities: new Set(), term: (k: string) => ({ relation_module: "Tenant Relation", request: "Tenant Service Request", customer: "Tenant", inventory_unit: "Unit" })[k] ?? k }) }));

import { ToastProvider } from "@/components/bv/common";
import TenantRelationDashboardPage from "./TenantRelationDashboardPage";
import AnnouncementsPage from "./AnnouncementsPage";
import FeedbackPage from "./FeedbackPage";
import RecurringIssuesPage from "./RecurringIssuesPage";
import TenantUsersPage from "./TenantUsersPage";
import { CommunicationsPanel } from "./CommunicationsPanel";
import { TenantMessagesPanel } from "./TenantMessagesPanel";
import ServiceRequestListPage from "@/features/operations/ServiceRequestListPage";

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString();
const list = (data: unknown[]) => ({ data, next_cursor: null });
const SR = "/operations/service-requests";

const metrics = {
  open_tickets: 12, sla_risk: 2, overdue: 3, resolved_today: 4, reopened_30d: 1, waiting_for_tenant: 2, waiting_for_staff: 1, pending_accounts: 2,
  csat: 4.35, csat_count: 20, reopen_rate_pct: 5, tenant_app_tickets_30d: 30, avg_response_hours: 0.75, avg_resolution_hours: 6.2, sla_compliance_pct: 88.5,
  requests_30d: 44, requests_prev_30d: 40, complaints_30d: 6, complaints_prev_30d: 3, recurring_issues_open: 1, general_feedback_new: 2,
  by_category: [{ key: "ac", label: "AC tidak dingin", count: 9, drill_down: `${SR}?category=ac&property_id=p1` }],
  by_type: [{ key: "complaint", label: "Keluhan", count: 6, drill_down: `${SR}?request_type=complaint&property_id=p1` }],
  by_channel: [{ key: "tenant_app", label: "Tenant App", count: 30, drill_down: `${SR}?channel=tenant_app&property_id=p1` }],
  series: [{ date: "2026-09-26", created: 3, resolved: 2, complaints: 1 }, { date: "2026-09-27", created: 5, resolved: 4, complaints: 0 }],
  drill_down: { open_tickets: `${SR}?open=true&property_id=p1`, overdue: `${SR}?sla_status=breached&property_id=p1`, sla_risk: `${SR}?sla_status=at_risk&property_id=p1`, recurring_issues_open: "/tenant-relation/recurring-issues", pending_accounts: "/tenant-relation/tenant-users?status=pending_validation", general_feedback_new: "/tenant-relation/feedback?tab=general" },
  timezone: "Asia/Jakarta",
};
const announcement = {
  id: "a1", property_id: "p1", property_name: "Menara Demo", title: "Pemeliharaan lift Tower A", excerpt: "Sabtu 08.00–10.00", body: "Lift barang tidak beroperasi.", audience: "tenant", importance: "important",
  category: "announcement", severity: "info", image_attachment_id: null, status: "published", publish_at: ago(60), published_at: ago(60), expires_at: null, requires_ack: true,
  target_location_ids: ["l1"], target_tenant_ids: [], targets: [{ kind: "location", id: "l1", label: "Tower A · Lantai 12", type: "floor" }], recipients_count: 10, read_count: 6, ack_count: 4,
  created_at: ago(120), created_by_name: "TR Officer", version: 2, allowed_actions: ["view", "update", "archive", "view_reads"],
};
const draft = { ...announcement, id: "a2", title: "Kabar lobby baru", category: "news", status: "draft", published_at: null, publish_at: null, requires_ack: false, targets: [], target_location_ids: [], recipients_count: null, read_count: 0, ack_count: 0, allowed_actions: ["view", "update", "publish", "schedule", "archive"] };
const reads = { recipients: 10, read: 6, acknowledged: 4, items: [{ user_id: "t9", full_name: "Budi Belum Baca", tenant_name: "PT Maju", unit_label: "Unit 1208", read_at: null, acknowledged_at: null }] };
const feedback = {
  id: "f1", feedback_number: "FDB-2026-000001", property_id: "p1", property_name: "Menara Demo", category: "suggestion", subject: "Jadwal kebersihan lobby", body: "Mohon lobby dibersihkan dua kali sehari.",
  is_anonymous: true, sender_name: null, tenant_name: null, unit_label: null, status: "new", response: null, responded_by_name: null, responded_at: null, photo_count: 0, created_at: ago(30),
  allowed_actions: ["view", "review", "respond", "close"], version: 1,
};
const issue = {
  id: "ri1", property_id: "p1", location_id: "l12", location_name: "Unit 1208", location_path: "Tower A · Lantai 12 · Unit 1208", category_code: "ac", category_name: "AC tidak dingin",
  request_count: 4, complaint_count: 2, open_count: 1, window_days: 30, threshold: 3, first_seen_at: ago(60 * 24 * 10), last_seen_at: ago(60), status: "open", service_request_ids: ["sr1"],
  acknowledged_by_name: null, acknowledged_at: null, resolved_at: null, note: null, allowed_actions: ["view", "acknowledge", "resolve"], version: 1,
};
const sr = { id: "sr1", property_id: "p1", request_number: "SR-2026-000042", title: "AC Unit 1208 tidak dingin", status: "in_progress", priority: "high", request_type: "complaint", created_at: ago(90), location: { id: "l12", name: "Unit 1208", path_text: "Tower A / Lantai 12 / Unit 1208" }, category_code: "ac", category_name: "AC tidak dingin", channel: "tenant_app", assignee: { user_id: null, user_name: null, team_id: null, team_name: null }, flags: [], links: [], allowed_actions: ["view"], sla: null, sla_status: "on_track", tenant_name: "PT Maju", requester_name: "Sari", reopen_count: 0 };
const tenantUser = {
  id: "tu1", user_id: "u9", full_name: "Sari Tenant", email: "sari@tenant.test", phone: "081234567890", property_id: "p1", property_name: "Menara Demo", tenant_id: "t1", tenant_name: "PT Maju", occupant_id: null,
  role: "tenant_admin", status: "active", registration_source: "self", ownership_status: "tenant", requested_at: ago(1000), validated_at: ago(900), validated_by_name: "TR Officer", rejection_reason: null,
  suspension_reason: null, last_seen_at: ago(10), access: [], open_requests: 1, allowed_actions: ["view", "suspend", "update", "manage_access", "reset_password"], version: 3,
};
const comms = [
  { at: ago(80), channel: "inapp", direction: "to_tenant", recipient: "Sari Tenant", actor: null, title: "Permintaan diterima", body: "SR-2026-000042 diterima", status: "read", detail: null },
  { at: ago(79), channel: "push", direction: "to_tenant", recipient: "Sari Tenant", actor: null, title: "Permintaan diterima", body: "SR-2026-000042 diterima", status: "not_sent", detail: "no_device" },
  { at: ago(50), channel: "whatsapp_manual", direction: "to_tenant", recipient: "Sari Tenant", actor: "TR Officer", title: "Dikirim manual via WhatsApp", body: "Halo Sari…", status: "sent_manually", detail: "+6281234567890" },
];
const messages = [{ id: "m1", author_kind: "tenant", author_name: "Sari Tenant", body: "Masih panas pak", attachment_ids: ["x1"], attachments: [{ id: "x1", content_type: "image/jpeg", file_name: "ac.jpg", url: "https://files.test/ac.jpg", thumb_url: "https://files.test/ac-thumb.jpg" }], created_at: ago(20), read_at: null }];

function respond(url: string, method: string): { status?: number; body: unknown } {
  const u = new URL(url, "http://localhost");
  const p = u.pathname.replace("/api/v1/", "");
  const ok = (body: unknown) => ({ body });
  if (p === "tenant-relation/metrics") return ok(metrics);
  if (p === "announcements" && method === "GET") return ok(list([announcement, draft]));
  if (p === "announcements/a1") return ok(announcement);
  if (p === "announcements/a2") return ok(draft);
  if (p === "announcements/a1/reads") return ok(reads);
  if (p === "announcements/a2/publish" && method === "POST") return ok({ ...draft, status: "published", recipients_count: 25 });
  if (p === "tenant-relation/general-feedback") return ok(list([feedback]));
  if (p === "tenant-relation/general-feedback/f1") return ok(feedback);
  if (p === "tenant-relation/general-feedback/f1/respond" && method === "POST") return ok({ ...feedback, status: "responded", response: "Terima kasih", allowed_actions: ["view", "respond", "close"] });
  if (p === "tenant-relation/feedback") return ok(list([]));
  if (p === "recurring-issues") return ok(list([issue]));
  if (p === "recurring-issues/ri1") return ok(issue);
  if (p === "recurring-issues/ri1/acknowledge" && method === "POST") return ok({ ...issue, status: "acknowledged", allowed_actions: ["view", "resolve"] });
  if (p === "service-requests") return ok(list([sr]));
  if (p === "tenant-users") return ok(list([tenantUser]));
  if (p === "tenant-users/tu1") return ok(tenantUser);
  if (p === "tenant-users/tu1/reset-password" && method === "POST") return ok({ tenant_user: tenantUser, temporary_password: "BvTemp1234!" });
  if (p === "service-requests/sr1/communications") return ok(list(comms));
  if (p === "service-requests/sr1/messages" && method === "GET") return ok(list(messages));
  if (p === "whatsapp/compose") return ok({ context: "password_reset", recipient_key: "user:u9", recipient_name: "Sari Tenant", phone: "6281234567890", phone_valid: true, disabled_reason: null, text: "Halo Sari", url: "https://wa.me/6281234567890", candidates: [] });
  return ok(list([]));
}

class ResizeObserverStub { observe() {} unobserve() {} disconnect() {} }

beforeEach(() => {
  auth.perms = [];
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const r = respond(url, (init?.method ?? "GET").toUpperCase());
    const status = r.status ?? 200;
    return { ok: status < 400, status, statusText: String(status), text: async () => JSON.stringify(r.body), json: async () => r.body, headers: new Headers() };
  }));
});
afterEach(() => vi.unstubAllGlobals());

const calls = (part: string, method?: string) => vi.mocked(fetch).mock.calls.filter((c) => String(c[0]).includes(part) && (!method || ((c[1] as RequestInit | undefined)?.method ?? "GET").toUpperCase() === method));

function renderAt(path: string, pattern: string, element: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ToastProvider>
        <MemoryRouter initialEntries={[path]}>
          <Routes><Route path={pattern} element={element} /></Routes>
        </MemoryRouter>
      </ToastProvider>
    </QueryClientProvider>,
  );
}

describe("Dashboard Tenant Relation (P3-TSH-01..09)", () => {
  it("KPI menautkan drill-down server; Overdue = SLA breached (B-07); breakdown & attention", async () => {
    auth.perms = ["tenant.service_requests.view", "tenant_relation.tenant_users.view", "tenant_relation.feedback.view"];
    renderAt("/tenant-relation", "/tenant-relation", <TenantRelationDashboardPage />);
    const overdue = await screen.findByTestId("kpi-overdue");
    expect(overdue).toHaveAttribute("href", `${SR}?sla_status=breached&property_id=p1`);
    expect(within(overdue).getByText("Melewati SLA")).toBeInTheDocument();
    expect(screen.getByTestId("kpi-requests")).toHaveTextContent("+10%");
    expect(screen.getByTestId("kpi-complaints")).toHaveTextContent("+100%");
    expect(screen.getByTestId("kpi-sla-compliance")).toHaveTextContent("88,5%");
    expect(screen.getByTestId("kpi-response")).toHaveTextContent("6 j 12 m");
    expect(screen.getByTestId("kpi-response")).toHaveTextContent("respons 45 m");
    expect(screen.getByRole("link", { name: /AC tidak dingin: 9 permintaan/ })).toHaveAttribute("href", `${SR}?category=ac&property_id=p1`);
    expect(screen.getByText("1 isu berulang terbuka").closest("a")).toHaveAttribute("href", "/tenant-relation/recurring-issues");
    expect(screen.getByText("2 pendaftaran Tenant App menunggu validasi")).toBeInTheDocument();
  });
});

describe("Announcements (P3-ANN-01..06)", () => {
  it("daftar: kategori, sasaran, dibaca; drawer deep link menampilkan pelacakan baca", async () => {
    auth.perms = ["tenant_relation.announcements.view", "tenant_relation.announcements.create", "tenant_relation.announcements.broadcast"];
    renderAt("/tenant-relation/announcements/a1", "/tenant-relation/announcements/:id", <AnnouncementsPage />);
    expect((await screen.findAllByText("Pemeliharaan lift Tower A")).length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: /Broadcast darurat/ })).toBeInTheDocument();
    expect(await screen.findByText("Budi Belum Baca")).toBeInTheDocument();
    expect(screen.getAllByText("Tower A · Lantai 12").length).toBeGreaterThan(0);
    await waitFor(() => expect(calls("announcements/a1/reads").some((c) => String(c[0]).includes("unread=true"))).toBe(true));
  });
  it("publish dari drawer meminta konfirmasi lalu POST /publish", async () => {
    auth.perms = ["tenant_relation.announcements.view", "tenant_relation.announcements.publish"];
    renderAt("/tenant-relation/announcements/a2", "/tenant-relation/announcements/:id", <AnnouncementsPage />);
    fireEvent.click(await screen.findByRole("button", { name: /Publikasikan sekarang/ }));
    const confirm = await screen.findAllByRole("button", { name: /Publikasikan sekarang/ });
    fireEvent.click(confirm[confirm.length - 1]);
    await waitFor(() => expect(calls("announcements/a2/publish", "POST").length).toBe(1));
  });
});

describe("Feedback Umum (P3-FDB-02..03)", () => {
  it("deep link: identitas anonim disembunyikan; tanggapan dikirim", async () => {
    auth.perms = ["tenant_relation.feedback.view", "tenant_relation.feedback.respond"];
    renderAt("/tenant-relation/feedback/general/f1", "/tenant-relation/feedback/general/:id", <FeedbackPage />);
    expect(await screen.findByText(/secara anonim/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /^Tanggapi$/ }));
    fireEvent.change(await screen.findByPlaceholderText(/Terima kasih atas masukannya/), { target: { value: "Terima kasih, jadwal kami tambah." } });
    fireEvent.click(screen.getByRole("button", { name: /Kirim tanggapan/ }));
    await waitFor(() => expect(calls("general-feedback/f1/respond", "POST").length).toBe(1));
    expect(JSON.parse(String((calls("general-feedback/f1/respond", "POST")[0][1] as RequestInit).body))).toEqual({ response: "Terima kasih, jadwal kami tambah." });
  });
});

describe("Isu Berulang (P3-TSH-07)", () => {
  it("drawer: permintaan terkait (recurring_issue_id) + tautan daftar + tangani", async () => {
    auth.perms = ["tenant_relation.recurring_issues.view", "tenant_relation.recurring_issues.manage", "tenant.service_requests.view"];
    renderAt("/tenant-relation/recurring-issues/ri1", "/tenant-relation/recurring-issues/:id", <RecurringIssuesPage />);
    expect(await screen.findByText("SR-2026-000042")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Buka di daftar/ })).toHaveAttribute("href", `${SR}?recurring_issue_id=ri1`);
    expect(calls("service-requests").some((c) => String(c[0]).includes("recurring_issue_id=ri1"))).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: /^Tangani$/ }));
    fireEvent.click(await screen.findByRole("button", { name: /Tandai ditangani/ }));
    await waitFor(() => expect(calls("recurring-issues/ri1/acknowledge", "POST").length).toBe(1));
  });
});

describe("Tenant Users (P3-ACC-03/05/07/08)", () => {
  it("status dari URL; Tenant Admin; reset password → password sementara + WhatsApp", async () => {
    auth.perms = ["tenant_relation.tenant_users.view", "tenant_relation.tenant_users.update", "tenant_relation.tenant_users.reset_password"];
    renderAt("/tenant-relation/tenant-users/tu1?status=active", "/tenant-relation/tenant-users/:id", <TenantUsersPage />);
    expect((await screen.findAllByText("Sari Tenant")).length).toBeGreaterThan(0);
    await waitFor(() => expect(calls("tenant-users?").some((c) => String(c[0]).includes("status=active"))).toBe(true));
    expect(screen.getAllByText("Tenant Admin").length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: /Reset password/ }));
    const dialogButtons = await screen.findAllByRole("button", { name: /Reset password/ });
    fireEvent.click(dialogButtons[dialogButtons.length - 1]);
    expect(await screen.findByText("BvTemp1234!")).toBeInTheDocument();
    expect(calls("tenant-users/tu1/reset-password", "POST").length).toBe(1);
    expect(screen.getAllByRole("button", { name: /Kirim via WhatsApp/ }).length).toBeGreaterThan(0);
  });
});

describe("Service Request — komunikasi (P3-TRC-03, P3-SRQ-05)", () => {
  it("log komunikasi: kanal, status kirim, WhatsApp manual", async () => {
    auth.perms = ["tenant.service_requests.view"];
    renderAt("/x", "/x", <CommunicationsPanel srId="sr1" />);
    expect(await screen.findByText("Dikirim manual via WhatsApp")).toBeInTheDocument();
    expect(screen.getByText("Tidak terkirim")).toBeInTheDocument();
    expect(screen.getByText(/tenant belum mendaftarkan perangkat/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Kirim via WhatsApp/ })).toBeInTheDocument();
  });
  it("pesan tenant menampilkan foto lampiran; staf dapat melampirkan foto", async () => {
    auth.perms = ["tenant_relation.messages.view", "tenant_relation.messages.create"];
    renderAt("/x", "/x", <TenantMessagesPanel srId="sr1" terminal={false} channel="tenant_app" />);
    expect(await screen.findByText("Masih panas pak")).toBeInTheDocument();
    expect(screen.getByAltText("ac.jpg")).toHaveAttribute("src", "https://files.test/ac-thumb.jpg");
    expect(screen.getByRole("button", { name: /Lampirkan foto/ })).toBeInTheDocument();
  });
});

describe("Daftar Service Request — drill-down (P3-TSH-09)", () => {
  it("parameter drill-down diteruskan ke API dan diringkas", async () => {
    auth.perms = ["tenant.service_requests.view", "tenant_relation.recurring_issues.view"];
    const from = "2026-09-28T00:00:00+07:00";
    renderAt(`${SR}?resolved_from=${encodeURIComponent(from)}&channel=tenant_app&recurring_issue_id=ri1`, SR, <ServiceRequestListPage />);
    expect(await screen.findByText(/Menampilkan permintaan/)).toBeInTheDocument();
    expect(await screen.findByText(/AC tidak dingin · Tower A/)).toBeInTheDocument();
    const urls = calls("/api/v1/service-requests?").map((c) => decodeURIComponent(String(c[0])));
    expect(urls.some((u) => u.includes(`resolved_from=${from}`) && u.includes("channel=tenant_app") && u.includes("recurring_issue_id=ri1"))).toBe(true);
  });
});
