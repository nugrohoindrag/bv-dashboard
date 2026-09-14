import { describe, expect, it } from "vitest";
import { fmtMinutes, fmtMoney, fmtNumber, initials, setTimezone, fmtDateTime } from "@/lib/format";

describe("format helpers", () => {
  it("fmtMinutes", () => {
    expect(fmtMinutes(45)).toMatch(/45/);
    expect(fmtMinutes(150)).toMatch(/2/);
  });
  it("fmtMoney IDR", () => {
    expect(fmtMoney(1500000, "IDR")).toMatch(/1\.500\.000|1,500,000/);
  });
  it("fmtNumber", () => expect(fmtNumber(1234)).toMatch(/1[.,]234/));
  it("initials", () => expect(initials("Budi Santoso")).toBe("BS"));
  it("fmtDateTime mengikuti timezone property", () => {
    setTimezone("Asia/Jakarta");
    expect(fmtDateTime("2026-01-01T00:00:00Z")).toMatch(/07[.:]00/);
  });
});
