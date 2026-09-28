// Field profil Organization (PRD P0 v2 §6.2): identitas, kontak, alamat, pajak, industri, timezone.
import { useTranslation } from "react-i18next";
import { Field, Input, NativeSelect } from "@/components/ui/primitives";
import { validateOrgForm, type OrgField, type OrgForm } from "./organization-form";

const TIMEZONES = ["Asia/Jakarta", "Asia/Makassar", "Asia/Jayapura", "Asia/Singapore", "Asia/Kuala_Lumpur", "Asia/Bangkok", "UTC"];

export function OrganizationProfileForm({ value, onChange, disabled, showErrors }: { value: OrgForm; onChange: (v: OrgForm) => void; disabled?: boolean; showErrors?: boolean }) {
  const { t } = useTranslation();
  const errs = showErrors ? validateOrgForm(value) : {};
  const f = (k: OrgField, opts: { required?: boolean; type?: string; placeholder?: string; help?: string; className?: string } = {}) => (
    <Field label={t(`org.f.${k}`)} required={opts.required} className={opts.className} help={opts.help} error={errs[k] ? t(`org.err.${errs[k]}`) : undefined}>
      <Input type={opts.type ?? "text"} value={value[k]} placeholder={opts.placeholder} disabled={disabled} aria-invalid={errs[k] ? true : undefined} onChange={(e) => onChange({ ...value, [k]: e.target.value })} />
    </Field>
  );
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      {f("name", { required: true })}
      {f("legal_name")}
      {f("email", { type: "email" })}
      {f("phone", { type: "tel" })}
      {f("website", { placeholder: "https://" })}
      {f("industry")}
      {f("address", { className: "sm:col-span-2" })}
      {f("city")}
      {f("province")}
      {f("postal_code")}
      {f("country", { placeholder: "ID", help: t("org.country_help") })}
      {f("tax_id")}
      <Field label={t("org.f.timezone")} required>
        <NativeSelect value={value.timezone} disabled={disabled} onChange={(e) => onChange({ ...value, timezone: e.target.value })}>
          {[...new Set([value.timezone, ...TIMEZONES])].filter(Boolean).map((z) => <option key={z} value={z}>{z}</option>)}
        </NativeSelect>
      </Field>
    </div>
  );
}
