// Spesifikasi item checklist (PRD P0 v2 §12.1–12.2): tipe, opsi, expected result, rentang angka. Validasi di sini
// mencerminkan validateItemSpec server (api/internal/operations/checklist.go) agar builder memberi umpan balik sebelum
// menyimpan; server tetap sumber kebenaran (400 bila spesifikasi tidak valid).
import i18n from "./i18n";
import type { ChecklistItemType, ChecklistOption, ChecklistTemplateItem } from "@/api/types";

export const ITEM_TYPES: ChecklistItemType[] = ["ok_notok_na", "yes_no", "pass_fail", "numeric", "text", "photo", "selection", "signature"];
/** Tipe yang mendukung expected result. */
export const EXPECTED_TYPES: ChecklistItemType[] = ["ok_notok_na", "yes_no", "pass_fail", "selection"];

export type SpecError = { field: "label" | "options" | "expected_value" | "numeric"; key: string; params?: Record<string, unknown> };

export function validateItemSpec(it: Pick<ChecklistTemplateItem, "label" | "item_type" | "options" | "expected_value" | "numeric_min" | "numeric_max">): SpecError[] {
  const errs: SpecError[] = [];
  if (!it.label?.trim()) errs.push({ field: "label", key: "checklist.err_label" });
  if (!ITEM_TYPES.includes(it.item_type)) errs.push({ field: "label", key: "checklist.err_type" });
  const exp = it.expected_value?.trim() || null;
  switch (it.item_type) {
    case "selection": {
      const opts = it.options ?? [];
      if (opts.length < 2) errs.push({ field: "options", key: "checklist.err_options_min" });
      const vals = opts.map((o) => o.value.trim());
      if (vals.some((v) => !v) || new Set(vals).size !== vals.length) errs.push({ field: "options", key: "checklist.err_options_unique" });
      if (exp && exp.split(",").map((v) => v.trim()).some((v) => !vals.includes(v))) errs.push({ field: "expected_value", key: "checklist.err_expected_option" });
      break;
    }
    case "yes_no":
      if (exp && exp !== "yes" && exp !== "no") errs.push({ field: "expected_value", key: "checklist.err_expected", params: { allowed: "yes|no" } });
      break;
    case "pass_fail":
      if (exp && exp !== "pass" && exp !== "fail") errs.push({ field: "expected_value", key: "checklist.err_expected", params: { allowed: "pass|fail" } });
      break;
    case "ok_notok_na":
      if (exp && exp !== "ok") errs.push({ field: "expected_value", key: "checklist.err_expected", params: { allowed: "ok" } });
      break;
  }
  const min = it.numeric_min, max = it.numeric_max;
  if (min !== null && min !== undefined && max !== null && max !== undefined && Number(min) > Number(max)) errs.push({ field: "numeric", key: "checklist.err_range" });
  return errs;
}

/** Normalisasi sebelum dikirim: buang opsi/expected yang tidak relevan untuk tipe (sama seperti server). */
export function normalizeItem<T extends ChecklistTemplateItem>(it: T): T {
  const num = (v: unknown) => (v === null || v === undefined || v === "" ? null : Number(v));
  const out: T = { ...it, numeric_min: num(it.numeric_min), numeric_max: num(it.numeric_max), expected_value: it.expected_value?.trim() || null };
  if (it.item_type !== "selection") out.options = [];
  else out.options = (it.options ?? []).map((o) => ({ value: o.value.trim(), label: o.label.trim() || o.value.trim() }));
  if (!EXPECTED_TYPES.includes(it.item_type)) out.expected_value = null;
  if (it.item_type !== "numeric") {
    out.numeric_min = null;
    out.numeric_max = null;
    out.numeric_unit = null;
  }
  if (it.item_type === "photo") out.photo_required = true;
  return out;
}

const RESULT_LABEL: Record<string, string> = { ok: "OK", not_ok: "Not OK", na: "N/A", yes: "checklist.yes", no: "checklist.no", pass: "checklist.pass", fail: "checklist.fail" };
export function resultLabel(v: string, options?: ChecklistOption[]): string {
  const opt = options?.find((o) => o.value === v);
  if (opt) return opt.label;
  const k = RESULT_LABEL[v];
  return k ? (k.startsWith("checklist.") ? i18n.t(k) : k) : v;
}

/** Petunjuk hasil yang diharapkan, mis. "Diharapkan: Bersih" / "Diharapkan: 18–24 °C". */
export function expectedHint(it: { item_type: ChecklistItemType; expected_value?: string | null; options?: ChecklistOption[]; numeric_min?: number | null; numeric_max?: number | null; numeric_unit?: string | null }): string | null {
  if (it.item_type === "numeric" && (it.numeric_min != null || it.numeric_max != null)) {
    const range = it.numeric_min != null && it.numeric_max != null ? `${it.numeric_min}–${it.numeric_max}` : it.numeric_min != null ? `≥ ${it.numeric_min}` : `≤ ${it.numeric_max}`;
    return i18n.t("checklist.expected", { value: `${range}${it.numeric_unit ? " " + it.numeric_unit : ""}` });
  }
  if (!it.expected_value) return null;
  const vals = it.expected_value.split(",").map((v) => v.trim()).filter(Boolean);
  return i18n.t("checklist.expected", { value: vals.map((v) => resultLabel(v, it.options)).join(" / ") });
}
