import { describe, expect, it } from "vitest";
import { greeting, shortLocation } from "@/features/overview/helpers";

describe("Overview helpers", () => {
  it("shortLocation: dua segmen terakhir, segmen terulang dibuang", () => {
    expect(shortLocation("Podium Vision Residence / Tower A / Tower A Lantai 12 / Unit A-1208")).toBe("Tower A Lantai 12 · Unit A-1208");
    expect(shortLocation("Main Tower / Lantai 10 / Ruang Mesin / Mechanical Room")).toBe("Ruang Mesin · Mechanical Room");
    expect(shortLocation("Main Tower")).toBe("Main Tower");
    expect(shortLocation(null)).toBe("");
  });
  it("shortLocation keepRoot: nama gedung dipertahankan di depan", () => {
    expect(shortLocation("Main Tower / Lantai 10 / Ruang Mesin / Mechanical Room", true)).toBe("Main Tower · Ruang Mesin · Mechanical Room");
    expect(shortLocation("Main Tower / Lantai 5", true)).toBe("Main Tower · Lantai 5");
  });
  it("greeting mengikuti jam", () => {
    const at = (h: number) => new Date(2026, 8, 29, h, 0);
    expect(greeting(at(7))).toBe("Selamat pagi");
    expect(greeting(at(12))).toBe("Selamat siang");
    expect(greeting(at(16))).toBe("Selamat sore");
    expect(greeting(at(23))).toBe("Selamat malam");
  });
});
