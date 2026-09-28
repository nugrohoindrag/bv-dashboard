// Smoke render layar Security P2 (Emergency, Parking, Lost & Found) + ekstensi detail Incident dengan API tiruan:
// memastikan halaman merender data, tombol aksi mengikuti allowed_actions, dan deep link notifikasi punya route.
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/lib/i18n";

const auth = { perms: [] as string[] };
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    can: (p: string) => auth.perms.includes(p),
    principal: { id: "u1", full_name: "Satpam Satu", properties: [] },
    propertyId: "p1",
    properties: [{ id: "p1", name: "Menara Demo", code: "MD" }],
  }),
}));

import { ToastProvider } from "@/components/bv/common";
import EmergencyPage from "@/features/security/EmergencyPage";
import EmergencyDetailPage from "@/features/security/EmergencyDetailPage";
import ParkingPage from "@/features/security/ParkingPage";
import LostFoundPage from "@/features/security/LostFoundPage";
import LostFoundDetailPage from "@/features/security/LostFoundDetailPage";
import IncidentDetailPage from "@/features/operations/IncidentDetailPage";

const loc = { id: "l1", name: "Lobby", path_text: "Tower A / Lantai 1 / Lobby" };
const alert = {
  id: "e1", property_id: "p1", alert_number: "EMG-2026-0001", emergency_type: "fire", emergency_type_label: "Kebakaran", status: "raised", location: loc, description: "Asap di pantry",
  gps_lat: -6.2, gps_lng: 106.8, gps_status: "captured", channel: "panic_button", raised_by: "u2", raised_by_name: "Budi", raised_at: new Date(Date.now() - 90_000).toISOString(), client_raised_at: null,
  acknowledged_by: null, acknowledged_by_name: null, acknowledged_at: null, responder_user_id: null, responder_name: null, responding_at: null, resolved_by: null, resolved_at: null, resolution: null,
  cancelled_at: null, cancel_reason: null, escalation_level: 1, escalated_at: new Date().toISOString(), incident_id: "i1", incident_number: "INC-2026-0009", ack_seconds: null, active: true,
  allowed_actions: ["view", "acknowledge", "respond", "note", "resolve", "cancel"], created_at: new Date().toISOString(), version: 1,
  timeline: [
    { id: "t1", event_type: "raised", note: "Asap di pantry", actor_user_id: "u2", actor_name: "Budi", payload: { channel: "panic_button" }, occurred_at: new Date().toISOString() },
    { id: "t2", event_type: "incident_created", note: null, actor_user_id: null, actor_name: null, payload: { incident_number: "INC-2026-0009" }, occurred_at: new Date().toISOString() },
  ],
  contacts: [{ id: "c1", property_id: "p1", name: "Damkar Setiabudi", contact_type: "fire", phone: "113", notes: null, sort_order: 1, is_active: true, version: 1 }],
};
const area = { id: "a1", property_id: "p1", location_id: null, location_path: null, code: "B1", name: "Basement 1", area_type: "tenant", capacity: 40, occupied: 30, available: 10, is_active: true, notes: null, version: 1 };
const vehicle = { id: "v1", property_id: "p1", plate_number: "B1234XYZ", vehicle_type: "car", brand: "Avanza", color: "Hitam", owner_type: "tenant", tenant_id: null, tenant_name: "PT Maju", unit_location_id: null, unit_name: null, user_id: null, user_name: null, visitor_id: null, owner_name: "Andi", owner_phone: null, parking_area_id: "a1", parking_area_name: "Basement 1", permit_until: null, permit_valid: true, status: "active", notes: null, open_violations: 1, inside: true, version: 1 };
const log = { id: "g1", property_id: "p1", parking_area_id: "a1", parking_area_name: "Basement 1", vehicle_id: null, plate_number: "D123AB", owner_type: null, registered: false, entered_at: new Date().toISOString(), exited_at: null, duration_minutes: 42, gate: "Gate Utara", entry_by_name: "Satpam Satu", note: null };
const violation = { id: "pv1", property_id: "p1", violation_number: "PV-2026-0001", parking_area_id: "a1", parking_area_name: "Basement 1", location: { id: null, name: null, path_text: null }, vehicle_id: "v1", plate_number: "B1234XYZ", owner_name: "Andi", violation_type: "blocking", description: "Menghalangi jalur", action_taken: "warning", status: "open", incident_id: null, incident_number: null, recorded_by: "u1", recorded_by_name: "Satpam Satu", recorded_at: new Date().toISOString(), resolved_at: null, resolution: null, attachment_count: 0, allowed_actions: ["view", "resolve", "update", "create_incident", "attach"], version: 1 };
const item = { id: "lf1", property_id: "p1", item_number: "LF-2026-0001", category: "wallet", description: "Dompet coklat", found_location: loc, found_at: new Date().toISOString(), found_by_user_id: "u1", found_by_name: "Satpam Satu", finder_name: null, storage_location: "Loker Pos 1", status: "stored", retention_until: "2026-01-01", disposal_due: true, matched_report_id: null, matched_report_number: null, claimant_name: null, claimant_contact: null, returned_at: null, returned_by_name: null, signature_attachment_id: null, disposed_at: null, disposal_method: null, disposal_note: null, attachment_count: 0, allowed_actions: ["view", "update", "return", "match", "dispose", "attach"], created_at: new Date().toISOString(), version: 1 };
const report = { id: "r1", property_id: "p1", report_number: "LR-2026-0001", category: "wallet", description: "Dompet hilang", lost_location: loc, lost_at: null, reporter_name: "Sari", reporter_contact: "0811", tenant_id: null, tenant_name: null, status: "open", matched_item_id: null, matched_item_number: null, created_at: new Date().toISOString(), version: 1 };
const incident = {
  id: "i1", property_id: "p1", incident_number: "INC-2026-0009", incident_type: "safety", category: "emergency", title: "EMERGENCY — Kebakaran", description: null, location: loc, severity: "critical", priority: "critical", status: "in_progress",
  reported_by_name: "Budi", reported_at: new Date().toISOString(), occurred_at: null, assignee: { user_id: null, user_name: null, team_id: null, team_name: null }, resolution: null, resolved_at: null, closed_at: null, flags: ["critical", "escalated"], links: [], attachment_count: 0, comment_count: 0,
  allowed_actions: ["view", "resolve", "update", "attach", "escalate", "investigate", "view_people", "manage_people"], created_at: new Date().toISOString(), version: 3, source_type: "emergency_alert", source_id: "e1", escalation_level: 2, escalated_at: new Date().toISOString(),
  investigation: { status: "in_progress", investigator_user_id: "u1", investigator_name: "Satpam Satu", findings: "CCTV lantai 1", root_cause: null, corrective_action: null, corrective_owner_user_id: null, corrective_owner_name: null, investigated_at: null }, people_count: 2, video_count: 0,
};

const list = (data: unknown[]) => ({ data, next_cursor: null });
function respond(url: string): unknown {
  const u = new URL(url, "http://localhost");
  const p = u.pathname.replace("/api/v1/", "");
  if (p === "emergency-alerts") return list([alert]);
  if (p === "emergency-alerts/e1") return alert;
  if (p === "emergency-contacts") return list(alert.contacts);
  if (p === "parking-areas") return list([area]);
  if (p === "vehicles") return list([vehicle]);
  if (p === "parking-logs") return list([log]);
  if (p === "parking-violations") return list([violation]);
  if (p === "parking-violations/pv1") return violation;
  if (p === "lost-found/items") return list([item]);
  if (p === "lost-found/items/lf1") return item;
  if (p === "lost-found/reports") return list([report]);
  if (p === "incidents/i1") return incident;
  if (p === "attachments" || p === "activities" || p.endsWith("/comments")) return list([]);
  return list([]);
}

beforeEach(() => {
  auth.perms = [];
  vi.stubGlobal("fetch", vi.fn(async (url: string) => ({ ok: true, status: 200, text: async () => JSON.stringify(respond(url)), json: async () => respond(url), headers: new Headers() })));
});
afterEach(() => vi.unstubAllGlobals());

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

describe("Emergency", () => {
  it("daftar: Emergency aktif di atas dengan aksi cepat dari allowed_actions + riwayat", async () => {
    auth.perms = ["security.emergency_alerts.view", "security.emergency_alerts.raise"];
    renderAt("/security/emergency", "/security/emergency", <EmergencyPage />);
    expect(await screen.findByRole("button", { name: "Terima" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Tiba / Tangani" })).toBeInTheDocument();
    expect(screen.getAllByText("EMG-2026-0001").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Eskalasi L1").length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Laporkan Emergency/).length).toBeGreaterThan(0);
  });
  it("pelapor tanpa .view memakai mine=true (server menolak property_id tanpa mine) dan tanpa tab kontak", async () => {
    auth.perms = ["security.emergency_alerts.raise"];
    renderAt("/security/emergency", "/security/emergency", <EmergencyPage />);
    expect((await screen.findAllByText("EMG-2026-0001")).length).toBeGreaterThan(0);
    const urls = vi.mocked(fetch).mock.calls.map((c) => String(c[0])).filter((u) => u.includes("/emergency-alerts"));
    expect(urls.length).toBeGreaterThanOrEqual(2); // aktif + riwayat
    expect(urls.every((u) => u.includes("mine=true") && u.includes("property_id=p1"))).toBe(true);
    expect(screen.queryByText("Kontak Darurat")).not.toBeInTheDocument();
  });
  it("tab Kontak Darurat hanya dengan izin kontak", async () => {
    auth.perms = ["security.emergency_alerts.view", "security.emergency_contacts.view"];
    renderAt("/security/emergency/contacts", "/security/emergency/contacts", <EmergencyPage tab="contacts" />);
    expect(await screen.findByText("Damkar Setiabudi")).toBeInTheDocument();
    expect(screen.getByText("113").closest("a")).toHaveAttribute("href", "tel:113");
  });
  it("detail: aksi besar, timeline, kontak, GPS", async () => {
    auth.perms = ["security.emergency_alerts.view", "security.emergency_alerts.respond"];
    renderAt("/security/emergency/e1", "/security/emergency/:id", <EmergencyDetailPage />);
    expect(await screen.findByText(/Menunggu respons security/)).toBeInTheDocument();
    for (const label of ["Terima", "Tiba / Tangani", "Selesaikan", "Catat tindakan", "Alarm palsu"]) expect(screen.getAllByRole("button", { name: new RegExp(label.replace("/", "\\/")) }).length).toBeGreaterThan(0);
    expect(screen.getByText("Incident INC-2026-0009 dibuat otomatis")).toBeInTheDocument();
    expect(screen.getByText(/Buka di Google Maps/).closest("a")).toHaveAttribute("href", "https://www.google.com/maps?q=-6.2,106.8");
    expect(screen.getByLabelText(/Telepon Damkar Setiabudi/)).toHaveAttribute("href", "tel:113");
  });
});

describe("Parking", () => {
  it("tab area: okupansi 75%", async () => {
    auth.perms = ["security.parking.view"];
    renderAt("/security/parking/areas", "/security/parking/:tab", <ParkingPage />);
    expect(await screen.findByText("Basement 1")).toBeInTheDocument();
    expect(screen.getAllByText("75%").length).toBeGreaterThan(0);
  });
  it("tab kendaraan: plat berformat, badge izin & pelanggaran", async () => {
    auth.perms = ["security.parking.view"];
    renderAt("/security/parking/vehicles", "/security/parking/:tab", <ParkingPage />);
    expect(await screen.findByText("B 1234 XYZ")).toBeInTheDocument();
    expect(screen.getByText("1 pelanggaran")).toBeInTheDocument();
  });
  it("tab log: kendaraan tidak terdaftar + Catat Keluar untuk petugas", async () => {
    auth.perms = ["security.parking.view", "security.parking.record"];
    renderAt("/security/parking/logs", "/security/parking/:tab", <ParkingPage />);
    expect(await screen.findByText("D 123 AB")).toBeInTheDocument();
    expect(screen.getByText("Tidak terdaftar")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Catat Keluar/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Catat Masuk/ })).toBeInTheDocument();
  });
  it("deep link pelanggaran membuka drawer dengan aksi dari allowed_actions", async () => {
    auth.perms = ["security.parking.view", "security.parking.record"];
    renderAt("/security/parking/violations/pv1", "/security/parking/violations/:id", <ParkingPage />);
    expect(await screen.findByRole("button", { name: /Buat Incident/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Ubah tindakan/ })).toBeInTheDocument();
    expect(screen.getAllByText("PV-2026-0001").length).toBeGreaterThan(0);
  });
});

describe("Lost & Found", () => {
  it("daftar barang: badge Lewat masa simpan", async () => {
    auth.perms = ["security.lost_found.view"];
    renderAt("/security/lost-found", "/security/lost-found", <LostFoundPage />);
    expect(await screen.findByText("LF-2026-0001")).toBeInTheDocument();
    // chip filter + badge baris
    expect(screen.getAllByText("Lewat masa simpan")).toHaveLength(2);
  });
  it("tab laporan: Cari kecocokan untuk laporan open", async () => {
    auth.perms = ["security.lost_found.view"];
    renderAt("/security/lost-found/reports", "/security/lost-found/reports", <LostFoundPage tab="reports" />);
    expect(await screen.findByText("LR-2026-0001")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Cari kecocokan/ })).toBeInTheDocument();
  });
  it("detail: aksi serah terima, cocokkan, disposal dari allowed_actions", async () => {
    auth.perms = ["security.lost_found.view", "security.lost_found.manage"];
    renderAt("/security/lost-found/lf1", "/security/lost-found/:id", <LostFoundDetailPage />);
    expect((await screen.findAllByRole("button", { name: /Serah terima/ })).length).toBeGreaterThan(0);
    expect(screen.getAllByRole("button", { name: /Disposal/ }).length).toBeGreaterThan(0);
    expect(screen.getByText("Loker Pos 1")).toBeInTheDocument();
  });
});

describe("Incident lengkap (P2 v2.1 §6.3)", () => {
  it("badge Eskalasi, kartu investigasi & orang terlibat (data pribadi dimuat atas permintaan), sumber Emergency Alert", async () => {
    auth.perms = ["operations.incidents.view", "operations.incidents.update"];
    renderAt("/operations/incidents/i1", "/operations/incidents/:id", <IncidentDetailPage />);
    expect(await screen.findByText("Eskalasi L2")).toBeInTheDocument();
    expect(screen.queryByText("Dieskalasi")).not.toBeInTheDocument(); // flag generik tidak dobel
    expect(screen.getByRole("button", { name: /Eskalasi/ })).toBeInTheDocument();
    expect(screen.getByText("Investigasi")).toBeInTheDocument();
    expect(screen.getByText("CCTV lantai 1")).toBeInTheDocument();
    expect(screen.getByText("Orang terlibat (2)")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Tampilkan 2 orang terlibat/ })).toBeInTheDocument();
    // tombol non-transisi tidak dirender sebagai aksi
    expect(screen.queryByRole("button", { name: /view_people|manage_people|investigate/ })).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Emergency Alert").closest("a")).toHaveAttribute("href", "/security/emergency/e1"));
  });
});
