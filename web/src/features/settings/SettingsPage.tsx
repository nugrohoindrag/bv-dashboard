// Settings (PRD §22–§24): organization · users · roles · teams · checklists · sla-policies · master-data · notifications · sync-conflicts · audit-logs
import { lazy, Suspense } from "react";
import { NavLink, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shell/AppShell";
import { DetailSkeleton } from "@/components/bv/common";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";

const OrganizationSection = lazy(() => import("./OrganizationSection"));
const PropertyProfileSection = lazy(() => import("./PropertyProfileSection"));
const PaymentProvidersSection = lazy(() => import("./PaymentProvidersSection"));
const UsersSection = lazy(() => import("./UsersSection"));
const RolesSection = lazy(() => import("./RolesSection"));
const TeamsSection = lazy(() => import("./TeamsSection"));
const ChecklistsSection = lazy(() => import("./ChecklistsSection"));
const SLASection = lazy(() => import("./SLASection"));
const MasterDataSection = lazy(() => import("./MasterDataSection"));
const NotificationsSection = lazy(() => import("./NotificationsSection"));
const SyncConflictsSection = lazy(() => import("./SyncConflictsSection"));
const AuditLogsSection = lazy(() => import("./AuditLogsSection"));
const PlanSection = lazy(() => import("./PlanSection"));
const AppDownloadsSection = lazy(() => import("./AppDownloadsSection"));
const DemoDataSection = lazy(() => import("./DemoDataSection"));

const SECTIONS: { key: string; label: string; perm: string; el: React.LazyExoticComponent<() => React.JSX.Element>; internalOnly?: boolean }[] = [
  { key: "organization", label: "nav.organization", perm: "platform.organizations.view", el: OrganizationSection },
  { key: "plan", label: "nav.plan", perm: "platform.organizations.view", el: PlanSection },
  { key: "property-profile", label: "nav.property_profile", perm: "property.properties.view", el: PropertyProfileSection },
  { key: "users", label: "nav.users", perm: "iam.users.view", el: UsersSection },
  { key: "roles", label: "nav.roles", perm: "iam.roles.view", el: RolesSection },
  { key: "teams", label: "nav.teams", perm: "iam.teams.view", el: TeamsSection },
  { key: "checklists", label: "nav.checklists", perm: "operations.checklists.view", el: ChecklistsSection },
  { key: "sla-policies", label: "nav.sla_policies", perm: "operations.sla_policies.view", el: SLASection },
  { key: "payment-providers", label: "nav.payment_providers", perm: "billing.payments.view", el: PaymentProvidersSection },
  { key: "master-data", label: "nav.master_data", perm: "engineering.equipment.view", el: MasterDataSection },
  { key: "notifications", label: "nav.notifications", perm: "notification.inbox.view", el: NotificationsSection },
  { key: "sync-conflicts", label: "nav.sync_conflicts", perm: "sync.conflicts.view", el: SyncConflictsSection },
  { key: "audit-logs", label: "nav.audit", perm: "platform.audit_logs.view", el: AuditLogsSection },
  // Website PRD §16/§44: hanya admin_internal (menu disembunyikan; backend tetap menolak yang lain)
  { key: "app-downloads", label: "nav.app_downloads", perm: "platform.app_downloads.view", el: AppDownloadsSection, internalOnly: true },
  // Demo Seed Database §29: Admin Internal → Demo Data (seed/reset/verify tiga environment demo)
  { key: "demo-data", label: "nav.demo_data", perm: "platform.demo_data.view", el: DemoDataSection, internalOnly: true },
];

export default function SettingsPage() {
  const { section = "organization" } = useParams();
  const { t } = useTranslation();
  const { can, principal } = useAuth();
  const visible = SECTIONS.filter((s) => can(s.perm) && (!s.internalOnly || principal?.is_internal_admin));
  const current = visible.find((s) => s.key === section) ?? visible[0];
  const Section = current?.el;
  return (
    <div>
      <PageHeader title={t("nav.settings")} />
      <div className="grid grid-cols-12 gap-5">
        <nav className="col-span-2">
          <ul className="space-y-0.5">
            {visible.map((s) => (
              <li key={s.key}><NavLink to={`/settings/${s.key}`} className={({ isActive }) => cn("block rounded-md px-3 py-1.5 text-sm hover:bg-muted", isActive && "bg-brand-50 font-medium text-brand-700")}>{t(s.label)}</NavLink></li>
            ))}
          </ul>
        </nav>
        <div className="col-span-10">
          <Suspense fallback={<DetailSkeleton />}>{Section ? <Section /> : <p className="text-sm text-muted-foreground">Tidak ada akses ke pengaturan.</p>}</Suspense>
        </div>
      </div>
    </div>
  );
}
