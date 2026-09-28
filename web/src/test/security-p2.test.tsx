// PRD P2 v2.1 Security (Emergency · Parking · Lost & Found · incident lengkap): helper murni, label/status, navigasi,
// kategori Attention Required, dan badge.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import "@/lib/i18n";
import {
  EMERGENCY_TYPES, MAX_VIDEO_BYTES, PERSON_ROLES, displayPlate, emergencyAttentionTitle, emergencyEventText, escalationLabel, fmtSeconds, labelOf, mapsUrl, nextDay, normalizePlate,
  occupancyPct, occupancyTone, orderedEmergencyActions, secondsSince, telHref, validateVideoFile,
} from "@/features/security/p2";
import * as p2 from "@/features/security/p2";
import { EscalationBadge } from "@/features/security/shared";
import { StatusBadge } from "@/components/bv/badges";
import { statusLabel, statusOptions } from "@/lib/status";
import { statusMap } from "@/lib/status-map";
import { fieldErrorsOf } from "@/lib/problem";
import { ApiError } from "@/lib/api";
import { NAV, isItemActive, visibleNav, type AccessContext } from "@/app/navigation";
import { attentionCategoryLabel, itemLink } from "@/components/bv/cards";

const ctx = (perms: string[]): AccessContext => ({ can: (p) => perms.includes(p), hasCapability: () => true, isInternalAdmin: false });

describe("status & label Security P2", () => {
  it("Emergency Alert: raised → acknowledged → responding → resolved | cancelled dengan label PRD (status map GENERATED)", () => {
    expect(statusOptions("emergency_alert")).toEqual([
      { value: "raised", label: "Dilaporkan" },
      { value: "acknowledged", label: "Diterima" },
      { value: "responding", label: "Ditangani" },
      { value: "resolved", label: "Selesai" },
      { value: "cancelled", label: "Dibatalkan" },
    ]);
  });
  it("status tak dikenal → kode mentah, kosong → —", () => {
    expect(statusLabel("parking_violation", "archived")).toBe("archived");
    expect(statusLabel("lost_found_item", null)).toBe("—");
    expect(statusLabel("investigation", "completed")).toBe("Selesai");
  });
  it("grup status map GENERATED mencakup nilai CHECK backend; tidak ada lagi peta status lokal", () => {
    expect(Object.keys(statusMap.parking_violation).sort()).toEqual(["escalated", "open", "resolved"]);
    expect(Object.keys(statusMap.lost_found_item).sort()).toEqual(["disposed", "returned", "stored"]);
    expect(Object.keys(statusMap.lost_report).sort()).toEqual(["cancelled", "closed", "matched", "open"]);
    expect(Object.keys(statusMap.vehicle).sort()).toEqual(["active", "blacklisted", "inactive"]);
    expect(Object.keys(statusMap.investigation).sort()).toEqual(["completed", "in_progress", "not_started"]);
    expect("P2_STATUS" in p2).toBe(false);
  });
  it("labelOf: peta → label, kode tak dikenal → spasi, kosong → —", () => {
    expect(labelOf(EMERGENCY_TYPES, "security_threat")).toBe("Ancaman keamanan");
    expect(labelOf(EMERGENCY_TYPES, "gas_leak")).toBe("gas leak");
    expect(labelOf(PERSON_ROLES, undefined)).toBe("—");
    expect(labelOf(PERSON_ROLES, "suspect")).toBe("Terduga");
  });
});

describe("plat nomor", () => {
  it("normalisasi mengikuti NormalizePlate backend", () => {
    expect(normalizePlate(" b 1234-xyz ")).toBe("B1234XYZ");
    expect(normalizePlate(null)).toBe("");
  });
  it("tampilan plat Indonesia dengan spasi", () => {
    expect(displayPlate("B1234XYZ")).toBe("B 1234 XYZ");
    expect(displayPlate("d 123 ab")).toBe("D 123 AB");
    expect(displayPlate("AB1C")).toBe("AB 1 C");
    expect(displayPlate("B1234")).toBe("B 1234");
    expect(displayPlate("CD1234XYZW")).toBe("CD1234XYZW"); // bukan format Indonesia → apa adanya (ternormalisasi)
  });
});

describe("durasi & tanggal", () => {
  it("fmtSeconds untuk waktu respons (ack_seconds)", () => {
    expect(fmtSeconds(45)).toBe("45s");
    expect(fmtSeconds(80)).toBe("1m 20s");
    expect(fmtSeconds(120)).toBe("2m");
    expect(fmtSeconds(3700)).toBe("1j 1m");
    expect(fmtSeconds(7200)).toBe("2j");
    expect(fmtSeconds(-3)).toBe("0s");
    expect(fmtSeconds(null)).toBe("—");
  });
  it("secondsSince menghitung detik menunggu", () => {
    expect(secondsSince("2026-09-27T10:00:00Z", Date.parse("2026-09-27T10:01:30Z"))).toBe(90);
    expect(secondsSince("bukan-tanggal")).toBeNull();
    expect(secondsSince(null)).toBeNull();
  });
  it("nextDay = batas atas eksklusif filter tanggal", () => {
    expect(nextDay("2026-09-27")).toBe("2026-09-28");
    expect(nextDay("2026-12-31")).toBe("2027-01-01");
    expect(nextDay("2028-02-28")).toBe("2028-02-29");
  });
});

describe("eskalasi, GPS, telepon, okupansi", () => {
  it("escalationLabel hanya untuk level > 0", () => {
    expect(escalationLabel(0)).toBeNull();
    expect(escalationLabel(undefined)).toBeNull();
    expect(escalationLabel(2)).toBe("Eskalasi L2");
  });
  it("mapsUrl butuh koordinat lengkap", () => {
    expect(mapsUrl(-6.2, 106.8)).toBe("https://www.google.com/maps?q=-6.2,106.8");
    expect(mapsUrl(null, 106.8)).toBeNull();
    expect(mapsUrl(Number.NaN, 1)).toBeNull();
  });
  it("telHref hanya digit dan + awal", () => {
    expect(telHref("(021) 555-0101")).toBe("tel:0215550101");
    expect(telHref("+62 811 1000 200")).toBe("tel:+628111000200");
    expect(telHref("113")).toBe("tel:113");
  });
  it("okupansi & tone bar", () => {
    expect(occupancyPct(30, 40)).toBe(75);
    expect(occupancyPct(50, 40)).toBe(100);
    expect(occupancyPct(0, 0)).toBe(0);
    expect(occupancyPct(3, 0)).toBe(100);
    expect(occupancyTone(50)).toBe("success");
    expect(occupancyTone(75)).toBe("warning");
    expect(occupancyTone(95)).toBe("error");
  });
});

describe("timeline & aksi Emergency", () => {
  it("kalimat event dari payload server", () => {
    expect(emergencyEventText({ event_type: "raised", payload: { channel: "panic_button" } })).toBe("Emergency dilaporkan · Panic Button");
    expect(emergencyEventText({ event_type: "acknowledged", payload: { implicit: true } })).toBe("Diterima (otomatis saat security tiba)");
    expect(emergencyEventText({ event_type: "escalated", payload: { level: 2 } })).toBe("Eskalasi level 2");
    expect(emergencyEventText({ event_type: "incident_created", payload: { incident_number: "INC-2026-0001" } })).toBe("Incident INC-2026-0001 dibuat otomatis");
    expect(emergencyEventText({ event_type: "custom_event", payload: null })).toBe("custom event");
  });
  it("judul Attention Required Emergency diberi label tipe (format lama berkode & format baru berlabel)", () => {
    expect(emergencyAttentionTitle("EMERGENCY — fire")).toBe("EMERGENCY — Kebakaran");
    expect(emergencyAttentionTitle("EMERGENCY — security_threat")).toBe("EMERGENCY — Ancaman keamanan");
    expect(emergencyAttentionTitle("Emergency — Kebakaran")).toBe("Emergency — Kebakaran");
    expect(emergencyAttentionTitle("Emergency — Ancaman keamanan")).toBe("Emergency — Ancaman keamanan");
    expect(emergencyAttentionTitle("EMERGENCY — kode_baru")).toBe("EMERGENCY — kode_baru");
    expect(emergencyAttentionTitle("Judul lain")).toBe("Judul lain");
  });
  it("aksi dari allowed_actions diurutkan: utama lebih dulu, view diabaikan", () => {
    expect(orderedEmergencyActions(["view", "note", "cancel", "resolve", "acknowledge", "respond"])).toEqual(["acknowledge", "respond", "resolve", "note", "cancel"]);
    expect(orderedEmergencyActions(["view"])).toEqual([]);
  });
});

describe("video evidence incident (P2-SIN-04)", () => {
  it("menerima mp4/mov/webm ≤ 50 MB", () => {
    expect(validateVideoFile({ type: "video/mp4", size: 10 * 1024 * 1024 })).toBeNull();
    expect(validateVideoFile({ type: "video/quicktime", size: MAX_VIDEO_BYTES })).toBeNull();
    expect(validateVideoFile({ type: "video/webm", size: 1 })).toBeNull();
  });
  it("menolak format lain, file kosong, dan > 50 MB", () => {
    expect(validateVideoFile({ type: "video/x-msvideo", size: 100, name: "a.avi" })).toMatch(/tidak didukung/);
    expect(validateVideoFile({ type: "video/mp4", size: 0 })).toMatch(/kosong/);
    expect(validateVideoFile({ type: "video/mp4", size: MAX_VIDEO_BYTES + 1 })).toMatch(/50 MB/);
  });
});

describe("error validasi per field (investigasi → root_cause wajib)", () => {
  it("fieldErrorsOf memetakan problem+json errors[]", () => {
    const err = new ApiError({ type: "", title: "Validation failed", status: 400, code: "VALIDATION_FAILED", detail: "root_cause wajib sebelum investigasi selesai", errors: [{ field: "root_cause", message: "wajib" }] });
    expect(fieldErrorsOf(err)).toEqual({ root_cause: "wajib" });
    expect(fieldErrorsOf(new Error("x"))).toEqual({});
  });
});

describe("navigasi Security & Attention Required", () => {
  const securityItems = (perms: string[]) => visibleNav(NAV, ctx(perms)).find((g) => g.key === "security")?.items?.map((i) => i.to) ?? [];
  it("Parking, Emergency, Lost & Found mengikuti permission & urutan NC §15", () => {
    expect(securityItems(["security.patrol.view", "security.parking.view", "security.emergency_alerts.view", "security.lost_found.view"])).toEqual(["/security/patrol", "/security/parking", "/security/emergency", "/security/lost-found"]);
    // pelapor (raise saja) tetap bisa membuka Emergency
    expect(securityItems(["security.emergency_alerts.raise"])).toEqual(["/security/emergency"]);
    expect(securityItems(["security.parking.record"])).toEqual([]);
  });
  it("halaman detail/deep link mengaktifkan item menu", () => {
    const sec = NAV.find((g) => g.key === "security")!.items!;
    const item = (to: string) => sec.find((i) => i.to === to)!;
    expect(isItemActive(item("/security/emergency"), "/security/emergency/0199-abc")).toBe(true);
    expect(isItemActive(item("/security/parking"), "/security/parking/violations/0199-abc")).toBe(true);
    expect(isItemActive(item("/security/lost-found"), "/security/lost-found/reports")).toBe(true);
  });
  it("label kategori Attention Required P2", () => {
    expect(attentionCategoryLabel.active_emergency).toBe("Emergency Aktif");
    expect(attentionCategoryLabel.checkpoint_missed).toBe("Checkpoint Terlewat");
    expect(attentionCategoryLabel.document_expiring).toBe("Dokumen Kedaluwarsa");
    expect(attentionCategoryLabel.workforce_shortage).toBe("Kekurangan Staf On-Duty");
  });
  it("itemLink object Security = deep link notifikasi server", () => {
    expect(itemLink("emergency_alert", "x1")).toBe("/security/emergency/x1");
    expect(itemLink("parking_violation", "x2")).toBe("/security/parking/violations/x2");
    expect(itemLink("lost_found_item", "x3")).toBe("/security/lost-found/x3");
  });
});

describe("badge Security P2", () => {
  it("StatusBadge (status map GENERATED) merender label Emergency", () => {
    render(<StatusBadge objectType="emergency_alert" status="responding" />);
    expect(screen.getByText("Ditangani")).toBeInTheDocument();
  });
  it("EscalationBadge hanya tampil bila level > 0", () => {
    const { container, rerender } = render(<EscalationBadge level={0} />);
    expect(container.textContent).toBe("");
    rerender(<EscalationBadge level={3} />);
    expect(screen.getByText(/Eskalasi L3/)).toBeInTheDocument();
  });
});
