// Preset list (PRD P1 v2 §38): preset "Semua" membuat preset eksklusif; tanpa "Semua" preset tetap aditif.
import { describe, expect, it } from "vitest";
import { presetState } from "@/components/bv/datagrid";

const TASK: { key: string; label: string; params: Record<string, string> }[] = [
  { key: "all", label: "Semua", params: {} },
  { key: "open", label: "Open", params: { open: "true" } },
  { key: "overdue", label: "Overdue", params: { overdue: "true" } },
  { key: "completed", label: "Selesai", params: { status: "completed,closed" } },
];
const getter = (q: Record<string, string>) => (k: string) => q[k] ?? "";

describe("presetState", () => {
  it("Semua aktif tanpa kunci preset; klik menghapus semua kunci preset", () => {
    expect(presetState(TASK, TASK[0], getter({ q: "ac" }))).toEqual({ active: true, next: { open: null, overdue: null, status: null } });
    expect(presetState(TASK, TASK[0], getter({ open: "true" })).active).toBe(false);
  });
  it("preset eksklusif: klik mengganti preset lain, klik ulang menonaktifkan", () => {
    const s = presetState(TASK, TASK[2], getter({ open: "true" }));
    expect(s.active).toBe(false);
    expect(s.next).toEqual({ open: null, overdue: "true", status: null });
    expect(presetState(TASK, TASK[2], getter({ overdue: "true" }))).toEqual({ active: true, next: { overdue: null } });
  });
  it("drill-down gabungan (open + overdue) tidak menandai satu preset pun", () => {
    const q = getter({ open: "true", overdue: "true" });
    expect(TASK.slice(1).some((p) => presetState(TASK, p, q).active)).toBe(false);
  });
  it("tanpa preset Semua: aditif (perilaku lama)", () => {
    const P = TASK.slice(1);
    expect(presetState(P, P[1], getter({ open: "true" }))).toEqual({ active: false, next: { overdue: "true" } });
    expect(presetState(P, P[0], getter({ open: "true", overdue: "true" })).active).toBe(true);
  });
});
