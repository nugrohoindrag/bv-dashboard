// Spesifikasi item checklist (PRD P0 v2 §12): validasi builder = cermin server; editor opsi & expected result.
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import "@/lib/i18n";
import { expectedHint, normalizeItem, validateItemSpec } from "@/lib/checklist-spec";
import { ChecklistItemEditor } from "@/features/settings/ChecklistsSection";
import type { ChecklistTemplateItem } from "@/api/types";

const base = (p: Partial<ChecklistTemplateItem>): ChecklistTemplateItem => ({ sort_order: 1, section: null, label: "Kondisi toilet", item_type: "ok_notok_na", is_required: true, photo_required: false, numeric_unit: null, numeric_min: null, numeric_max: null, help_text: null, options: [], expected_value: null, ...p });
const keys = (it: ChecklistTemplateItem) => validateItemSpec(it).map((e) => e.key);

describe("validateItemSpec", () => {
  it("label wajib", () => {
    expect(keys(base({ label: "  " }))).toContain("checklist.err_label");
  });
  it("selection: minimal 2 opsi unik; expected harus nilai opsi (CSV)", () => {
    expect(keys(base({ item_type: "selection", options: [{ value: "bersih", label: "Bersih" }] }))).toContain("checklist.err_options_min");
    expect(keys(base({ item_type: "selection", options: [{ value: "a", label: "A" }, { value: "a", label: "A2" }] }))).toContain("checklist.err_options_unique");
    expect(keys(base({ item_type: "selection", options: [{ value: "bersih", label: "Bersih" }, { value: "kotor", label: "Kotor" }], expected_value: "bersih,wangi" }))).toContain("checklist.err_expected_option");
    expect(keys(base({ item_type: "selection", options: [{ value: "bersih", label: "Bersih" }, { value: "kotor", label: "Kotor" }], expected_value: "bersih" }))).toEqual([]);
  });
  it("expected per tipe: yes_no yes|no, pass_fail pass|fail, ok_notok_na hanya ok", () => {
    expect(keys(base({ item_type: "yes_no", expected_value: "maybe" }))).toContain("checklist.err_expected");
    expect(keys(base({ item_type: "pass_fail", expected_value: "pass" }))).toEqual([]);
    expect(keys(base({ item_type: "ok_notok_na", expected_value: "not_ok" }))).toContain("checklist.err_expected");
  });
  it("numeric_min tidak boleh > numeric_max", () => {
    expect(keys(base({ item_type: "numeric", numeric_min: 30, numeric_max: 10 }))).toContain("checklist.err_range");
  });
});

describe("normalizeItem & expectedHint", () => {
  it("membuang opsi/expected/rentang yang tidak relevan; photo selalu wajib foto", () => {
    const n = normalizeItem(base({ item_type: "text", options: [{ value: "x", label: "X" }], expected_value: "ok", numeric_min: 1 }));
    expect(n.options).toEqual([]);
    expect(n.expected_value).toBeNull();
    expect(n.numeric_min).toBeNull();
    expect(normalizeItem(base({ item_type: "photo" })).photo_required).toBe(true);
  });
  it("hint: label opsi untuk selection, rentang untuk angka", () => {
    expect(expectedHint(base({ item_type: "selection", options: [{ value: "bersih", label: "Bersih" }, { value: "kotor", label: "Kotor" }], expected_value: "bersih" }))).toBe("Diharapkan: Bersih");
    expect(expectedHint(base({ item_type: "numeric", numeric_min: 18, numeric_max: 24, numeric_unit: "°C" }))).toBe("Diharapkan: 18–24 °C");
    expect(expectedHint(base({ item_type: "pass_fail", expected_value: "pass" }))).toBe("Diharapkan: Lulus");
  });
});

describe("ChecklistItemEditor", () => {
  it("tipe selection menampilkan editor opsi + pilihan expected; error ditampilkan", () => {
    const onChange = vi.fn();
    const item = base({ item_type: "selection", options: [{ value: "bersih", label: "Bersih" }] });
    render(<ChecklistItemEditor item={item} index={0} errors={validateItemSpec(item)} onChange={onChange} />);
    expect(screen.getByTestId("options-editor")).toBeInTheDocument();
    expect(screen.getByText("Pilihan memerlukan minimal 2 opsi.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /tambah opsi/i }));
    expect(onChange).toHaveBeenCalledWith({ options: [{ value: "bersih", label: "Bersih" }, { value: "opsi_2", label: "" }] });
  });
  it("mengganti tipe mengosongkan expected_value; label deskripsi = 'Deskripsi'", () => {
    const onChange = vi.fn();
    render(<ChecklistItemEditor item={base({ expected_value: "ok" })} index={0} onChange={onChange} />);
    expect(screen.getByLabelText("Deskripsi")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Tipe item"), { target: { value: "pass_fail" } });
    expect(onChange).toHaveBeenCalledWith({ item_type: "pass_fail", expected_value: null });
  });
});
