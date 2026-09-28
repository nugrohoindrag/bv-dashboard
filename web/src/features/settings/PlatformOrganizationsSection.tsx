// Registry Organization (PRD P0 v2 §6, Platform Admin): daftar seluruh organization + jumlah user/property, buat
// organization baru beserta admin pertama, edit profil, Activate / Deactivate / Suspend (alasan wajib untuk dua terakhir).
// Hanya tampil bila /me/permissions is_platform_admin; server tetap mensyaratkan platform.org_registry.*.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Alert, Badge, Button, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, SearchInput } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { StatusBadge } from "@/components/bv/badges";
import { CellText, CellTitle } from "@/components/bv/cells";
import { KeyValue, ReasonDialog, RelativeTime, useToast } from "@/components/bv/common";
import { ForbiddenState } from "@/components/bv/states";
import { useInvalidate } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { statusOptions } from "@/lib/status";
import type { OrganizationSummary } from "@/api/types";
import { emptyOrgForm, orgFormFrom, orgFormToBody, type OrgForm } from "./organization-form";
import { OrganizationProfileForm } from "./OrganizationProfileForm";

export default function PlatformOrganizationsSection() {
  const { principal } = useAuth();
  if (!principal?.is_platform_admin) return <ForbiddenState />;
  return <Registry />;
}

function Registry() {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const [status, setStatus] = useState("");
  const list = useQuery({ queryKey: ["list", "platform-orgs", q, status], queryFn: () => api<{ data: OrganizationSummary[] }>("platform/organizations", { query: { q: q || undefined, status: status || undefined } }).then((r) => r.data) });
  const [edit, setEdit] = useState<OrganizationSummary | "new" | null>(null);
  const columns = useMemo<ColumnDef<OrganizationSummary, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026): kode (+ penanda internal) di atas nama; slug & badge internal ada di drawer detail.
      { id: "name", header: t("org.name"), meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.is_internal ? `${row.original.code} · internal` : row.original.code} title={row.original.name} /> },
      { id: "users", header: t("org.users"), meta: { mobile: "secondary" }, cell: ({ row }) => <span className="tnum whitespace-nowrap">{row.original.user_count}</span>, size: 80 },
      { id: "properties", header: t("org.properties"), meta: { mobile: "secondary" }, cell: ({ row }) => <span className="tnum whitespace-nowrap">{row.original.property_count}</span>, size: 90 },
      { id: "plan", header: "Plan", meta: { mobile: "hidden" }, cell: ({ row }) => <CellText max={160}>{`${row.original.plan_code ?? "—"} · ${row.original.trial_status}`}</CellText>, size: 160 },
      { id: "created_at", header: t("label.created"), meta: { mobile: "hidden" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm"><RelativeTime value={row.original.created_at} /></span>, size: 130 },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <StatusBadge objectType="organization" status={row.original.status} />, size: 120 },
    ],
    [t],
  );
  return (
    <div className="space-y-3" data-testid="platform-orgs">
      <div className="flex flex-wrap items-center gap-2">
        <div className="w-full sm:w-72"><SearchInput placeholder={t("org.search")} value={q} onChange={(e) => setQ(e.target.value)} /></div>
        <NativeSelect className="w-full sm:w-44" value={status} onChange={(e) => setStatus(e.target.value)} aria-label={t("label.status")}>
          <option value="">{t("label.status")}: {t("label.all")}</option>
          {statusOptions("organization").map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </NativeSelect>
        <span className="ml-auto"><Button icon="add" onClick={() => setEdit("new")}>{t("org.create")}</Button></span>
      </div>
      <DataGrid columns={columns} rows={list.data ?? []} rowId={(r) => r.id} onRowClick={(r) => setEdit(r)} isLoading={list.isLoading} error={list.error} onRetry={() => list.refetch()} isFiltered={!!q || !!status}
        empty={{ icon: "domain", title: t("org.empty"), description: t("org.empty_desc"), action: <Button icon="add" onClick={() => setEdit("new")}>{t("org.create")}</Button> }} />
      {edit === "new" && <CreateOrgDialog onClose={() => setEdit(null)} />}
      {edit && edit !== "new" && <OrgDrawer org={edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

function CreateOrgDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const invalidate = useInvalidate();
  const [form, setForm] = useState<OrgForm>(emptyOrgForm());
  const [slug, setSlug] = useState("");
  const [admin, setAdmin] = useState({ full_name: "", email: "", password: "" });
  const [err, setErr] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const submit = async () => {
    setErr(null);
    if (!form.name.trim()) return setErr(t("org.name_required"));
    if (!admin.full_name.trim() || !admin.email.trim()) return setErr(t("org.admin_required"));
    if (admin.password.length < 8) return setErr(t("profile.pw_min"));
    setSaving(true);
    try {
      const o = await api<OrganizationSummary>("platform/organizations", { body: { ...orgFormToBody(form), slug: slug.trim() || undefined, admin } });
      invalidate("platform-orgs");
      toast.action("created", `Organization ${o.name}`);
      onClose();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={t("org.create")}>
        <div className="space-y-4">
          {err && <Alert variant="critical">{err}</Alert>}
          <OrganizationProfileForm value={form} onChange={setForm} />
          <Field label="Slug" help={t("org.slug_help")}><Input value={slug} onChange={(e) => setSlug(e.target.value.toLowerCase())} placeholder="menara-demo" /></Field>
          <div className="rounded-[var(--radius-md)] border border-border p-3">
            <div className="mb-2 text-label uppercase tracking-wide text-on-surface-variant">{t("org.first_admin")}</div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label={t("profile.name")} required><Input value={admin.full_name} onChange={(e) => setAdmin({ ...admin, full_name: e.target.value })} /></Field>
              <Field label="Email" required><Input type="email" value={admin.email} onChange={(e) => setAdmin({ ...admin, email: e.target.value })} /></Field>
              <Field label={t("auth.password")} required help={t("profile.pw_min")}><Input type="password" autoComplete="new-password" value={admin.password} onChange={(e) => setAdmin({ ...admin, password: e.target.value })} /></Field>
            </div>
          </div>
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button><Button loading={saving} onClick={submit}>{t("org.create")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function OrgDrawer({ org, onClose }: { org: OrganizationSummary; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const invalidate = useInvalidate();
  const [form, setForm] = useState<OrgForm>(orgFormFrom(org));
  const [current, setCurrent] = useState(org);
  const [saving, setSaving] = useState(false);
  const [reasonFor, setReasonFor] = useState<"deactivate" | "suspend" | null>(null);
  const [busy, setBusy] = useState(false);
  const save = async () => {
    setSaving(true);
    try {
      const o = await api<OrganizationSummary>(`platform/organizations/${org.id}`, { method: "PATCH", body: orgFormToBody(form), ifMatch: current.version });
      setCurrent({ ...current, ...o });
      invalidate("platform-orgs");
      toast.action("saved", `Organization ${o.name}`);
    } catch (e) {
      toast.failed("saved", e, "Organization");
    } finally {
      setSaving(false);
    }
  };
  const setStatus = async (action: "activate" | "deactivate" | "suspend", reason?: string) => {
    setBusy(true);
    try {
      const o = await api<OrganizationSummary>(`platform/organizations/${org.id}/${action}`, { body: { reason: reason ?? "" } });
      setCurrent({ ...current, ...o });
      invalidate("platform-orgs");
      toast.action(action === "activate" ? "approved" : action === "deactivate" ? "cancelled" : "held", `Organization ${org.name}`);
      setReasonFor(null);
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={current.name} description={`${current.code} · ${current.slug}`}>
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            <StatusBadge objectType="organization" status={current.status} />
            {current.is_internal && <Badge tone="info">internal</Badge>}
            <span className="ml-auto flex flex-wrap gap-2">
              {current.status !== "active" && <Button size="sm" variant="success" loading={busy} onClick={() => setStatus("activate")}>{t("org.activate")}</Button>}
              {current.status !== "suspended" && <Button size="sm" variant="secondary" onClick={() => setReasonFor("suspend")}>{t("org.suspend")}</Button>}
              {current.status !== "inactive" && !current.is_internal && <Button size="sm" variant="destructive" onClick={() => setReasonFor("deactivate")}>{t("org.deactivate")}</Button>}
            </span>
          </div>
          {current.status_reason && <Alert variant={current.status === "active" ? "info" : "warning"} title={t("org.status_reason")}>{current.status_reason}</Alert>}
          <KeyValue items={[{ label: t("org.users"), value: current.user_count }, { label: t("org.properties"), value: current.property_count }, { label: "Plan", value: `${current.plan_code ?? "—"} · ${current.trial_status}` }, { label: t("label.created"), value: <RelativeTime value={current.created_at} /> }]} />
          <OrganizationProfileForm value={form} onChange={setForm} />
        </div>
        <DialogFooter><Button variant="secondary" onClick={onClose}>{t("action.close")}</Button><Button loading={saving} onClick={save}>{t("action.save")}</Button></DialogFooter>
        {reasonFor && (
          <ReasonDialog open onOpenChange={(o) => !o && setReasonFor(null)} destructive={reasonFor === "deactivate"} loading={busy}
            title={reasonFor === "deactivate" ? t("org.deactivate") : t("org.suspend")} description={reasonFor === "deactivate" ? t("org.deactivate_desc") : t("org.suspend_desc")}
            confirmLabel={reasonFor === "deactivate" ? t("org.deactivate") : t("org.suspend")} onConfirm={(r) => setStatus(reasonFor, r)} />
        )}
      </DialogContent>
    </Dialog>
  );
}
