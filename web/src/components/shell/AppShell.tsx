// AppShell (DS §3): Sidebar 260px gelap (Metronic Demo 10) + Header 64px (Property Switcher, Global Search ⌘K, Inbox, Avatar)
// Sidebar mengikuti IA PRD §26 & route NC §48; menu disaring permission; menu non-P0 tidak ditampilkan.
import { useEffect, useMemo, useState } from "react";
import { Link, NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Bell, Building2, ChevronDown, ChevronsLeft, ChevronsRight, ClipboardList, Cog, Gauge, HardHat, LogOut, Package, Search, ShieldCheck, SprayCan, Users, Wrench, type LucideIcon } from "lucide-react";
import { Avatar, Badge, Button, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger, Popover, PopoverContent, PopoverTrigger, TooltipProvider } from "@/components/ui/primitives";
import { OfflineBanner } from "@/components/bv/common";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { GlobalSearch } from "./GlobalSearch";
import { NotificationInbox } from "./NotificationInbox";
import { useNotifications } from "@/api/hooks";
import { setTimezone } from "@/lib/format";

interface NavItem { to: string; label: string; perm?: string | string[]; }
interface NavGroup { key: string; label: string; icon: LucideIcon; items?: NavItem[]; to?: string; perm?: string | string[] }

// IA PRD §26 (P0). Billing/Reports/Vendor tidak ditampilkan.
const NAV: NavGroup[] = [
  { key: "overview", label: "nav.overview", icon: Gauge, to: "/overview", perm: "overview.dashboard.view" },
  { key: "operations", label: "nav.operations", icon: ClipboardList, items: [
    { to: "/operations/tasks", label: "nav.tasks", perm: "operations.tasks.view" },
    { to: "/operations/work-orders", label: "nav.work_orders", perm: "operations.work_orders.view" },
    { to: "/operations/service-requests", label: "nav.service_requests", perm: "tenant.service_requests.view" },
    { to: "/operations/incidents", label: "nav.incidents", perm: "operations.incidents.view" },
  ] },
  { key: "engineering", label: "nav.engineering", icon: Wrench, items: [
    { to: "/engineering/preventive-maintenance", label: "nav.preventive_maintenance", perm: "engineering.maintenance_plans.view" },
    { to: "/engineering/corrective-maintenance", label: "nav.corrective_maintenance", perm: "operations.work_orders.view" },
    { to: "/engineering/inspections", label: "nav.inspections", perm: "engineering.inspections.view" },
    { to: "/engineering/assets", label: "nav.assets", perm: "engineering.assets.view" },
    { to: "/engineering/equipment", label: "nav.equipment", perm: "engineering.equipment.view" },
  ] },
  { key: "security", label: "nav.security", icon: ShieldCheck, items: [
    { to: "/security/patrol", label: "nav.patrol", perm: "security.patrol.view" },
    { to: "/security/incidents", label: "nav.incidents", perm: "operations.incidents.view" },
  ] },
  { key: "housekeeping", label: "nav.housekeeping", icon: SprayCan, items: [
    { to: "/housekeeping/cleaning", label: "nav.cleaning", perm: "housekeeping.cleaning.view" },
    { to: "/housekeeping/schedule", label: "nav.schedule", perm: "housekeeping.cleaning_schedules.view" },
    { to: "/housekeeping/inspections", label: "nav.inspections", perm: "housekeeping.inspections.view" },
  ] },
  { key: "property", label: "nav.property", icon: Building2, items: [
    { to: "/property/properties", label: "nav.properties", perm: "property.locations.view" },
    { to: "/property/buildings", label: "nav.buildings", perm: "property.locations.view" },
    { to: "/property/towers", label: "nav.towers", perm: "property.locations.view" },
    { to: "/property/floors", label: "nav.floors", perm: "property.locations.view" },
    { to: "/property/areas", label: "nav.areas", perm: "property.locations.view" },
    { to: "/property/spaces", label: "nav.spaces", perm: "property.locations.view" },
    { to: "/property/units", label: "nav.units", perm: "property.locations.view" },
  ] },
  { key: "tenant", label: "nav.tenant", icon: Users, items: [
    { to: "/tenant/tenants", label: "nav.tenants", perm: "property.tenants.view" },
    { to: "/tenant/service-requests", label: "nav.service_requests", perm: "tenant.service_requests.view" },
  ] },
  { key: "assets", label: "nav.asset_management", icon: Package, items: [
    { to: "/assets", label: "nav.assets", perm: "engineering.assets.view" },
    { to: "/assets/equipment", label: "nav.equipment", perm: "engineering.equipment.view" },
    { to: "/assets/history", label: "nav.history", perm: "engineering.assets.view" },
  ] },
  { key: "settings", label: "nav.settings", icon: Cog, items: [
    { to: "/settings/organization", label: "nav.organization", perm: "platform.organizations.view" },
    { to: "/settings/users", label: "nav.users", perm: "iam.users.view" },
    { to: "/settings/roles", label: "nav.roles", perm: "iam.roles.view" },
    { to: "/settings/teams", label: "nav.teams", perm: "iam.teams.view" },
    { to: "/settings/checklists", label: "nav.checklists", perm: "operations.checklists.view" },
    { to: "/settings/sla-policies", label: "nav.sla_policies", perm: "operations.sla_policies.view" },
    { to: "/settings/master-data", label: "nav.master_data", perm: "engineering.equipment.view" },
    { to: "/settings/notifications", label: "nav.notifications", perm: "notification.inbox.view" },
    { to: "/settings/sync-conflicts", label: "nav.sync_conflicts", perm: "sync.conflicts.view" },
    { to: "/settings/audit-logs", label: "nav.audit", perm: "platform.audit_logs.view" },
  ] },
];

function hasPerm(can: (p: string) => boolean, perm?: string | string[]): boolean {
  if (!perm) return true;
  return (Array.isArray(perm) ? perm : [perm]).some((p) => can(p));
}

export function AppShell() {
  const { t } = useTranslation();
  const { principal, properties, propertyId, setPropertyId, logout, can } = useAuth();
  const nav = useNavigate();
  const loc = useLocation();
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    try {
      return localStorage.getItem("bv.sidebar") === "collapsed";
    } catch {
      return false;
    }
  });
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [searchOpen, setSearchOpen] = useState(false);
  const notif = useNotifications(false);

  useEffect(() => {
    try {
      localStorage.setItem("bv.sidebar", collapsed ? "collapsed" : "open");
    } catch {
      /* ignore */
    }
  }, [collapsed]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setSearchOpen(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  useEffect(() => {
    // auto-expand group aktif
    const g = NAV.find((n) => n.items?.some((i) => loc.pathname.startsWith(i.to)));
    if (g) setOpen((o) => ({ ...o, [g.key]: true }));
  }, [loc.pathname]);
  const currentProperty = properties.find((p) => p.id === propertyId);
  useEffect(() => {
    setTimezone((currentProperty?.details?.timezone as string) || "Asia/Jakarta");
  }, [currentProperty]);
  const canAll = useMemo(() => principal?.properties.some((s) => s.property_id === null) ?? false, [principal]);
  const visibleNav = NAV.map((g) => ({ ...g, items: g.items?.filter((i) => hasPerm(can, i.perm)) })).filter((g) => (g.to ? hasPerm(can, g.perm) : (g.items?.length ?? 0) > 0));

  return (
    <TooltipProvider>
      <div className="flex h-full min-h-screen">
        {/* Sidebar */}
        <aside className={cn("flex shrink-0 flex-col bg-sidebar text-sidebar-foreground transition-[width]", collapsed ? "w-[72px]" : "w-[260px]")}>
          <div className="flex h-16 items-center gap-2 px-4">
            <span className="flex h-8 w-8 items-center justify-center rounded-md bg-brand-600 text-white"><HardHat className="h-5 w-5" /></span>
            {!collapsed && <span className="text-h3 font-semibold text-white">BuildingVision</span>}
          </div>
          <nav className="flex-1 overflow-y-auto px-2 pb-4" aria-label="Navigasi utama">
            {visibleNav.map((g) => {
              const Icon = g.icon;
              if (g.to) {
                return (
                  <NavLink key={g.key} to={g.to} className={({ isActive }) => cn("mb-0.5 flex items-center gap-3 rounded-md px-3 py-2 text-body hover:bg-white/5 hover:text-white", isActive && "bg-sidebar-accent text-white")} title={t(g.label)}>
                    <Icon className="h-4 w-4 shrink-0" /> {!collapsed && t(g.label)}
                  </NavLink>
                );
              }
              const active = g.items?.some((i) => loc.pathname.startsWith(i.to));
              const isOpen = !collapsed && (open[g.key] ?? active);
              return (
                <div key={g.key} className="mb-0.5">
                  <button type="button" onClick={() => (collapsed ? nav(g.items![0].to) : setOpen((o) => ({ ...o, [g.key]: !isOpen })))} className={cn("flex w-full items-center gap-3 rounded-md px-3 py-2 text-body hover:bg-white/5 hover:text-white", active && "text-white")} title={t(g.label)} aria-expanded={isOpen}>
                    <Icon className="h-4 w-4 shrink-0" />
                    {!collapsed && <span className="flex-1 text-left">{t(g.label)}</span>}
                    {!collapsed && <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", isOpen && "rotate-180")} />}
                  </button>
                  {isOpen && (
                    <div className="ml-4 border-l border-white/10 pl-2">
                      {g.items!.map((i) => (
                        <NavLink key={i.to} to={i.to} className={({ isActive }) => cn("block rounded-md px-3 py-1.5 text-sm hover:bg-white/5 hover:text-white", isActive && "bg-sidebar-accent text-white")}>
                          {t(i.label)}
                        </NavLink>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </nav>
          <button type="button" onClick={() => setCollapsed((c) => !c)} className="flex h-10 items-center justify-center border-t border-white/10 text-sidebar-foreground hover:text-white" aria-label={collapsed ? "Buka sidebar" : "Ciutkan sidebar"}>
            {collapsed ? <ChevronsRight className="h-4 w-4" /> : <ChevronsLeft className="h-4 w-4" />}
          </button>
        </aside>

        {/* Main */}
        <div className="flex min-w-0 flex-1 flex-col">
          <header className="flex h-16 shrink-0 items-center gap-3 border-b border-border bg-card px-6">
            {/* Property Switcher (wajib, DS §3.1) */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="secondary" className="min-w-[220px] justify-between">
                  <span className="inline-flex items-center gap-2 truncate"><Building2 className="h-4 w-4 text-brand-600" />{currentProperty?.name ?? (propertyId ? "…" : t("label.all_properties"))}</span>
                  <ChevronDown className="h-4 w-4 opacity-60" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="min-w-[260px]">
                <DropdownMenuLabel>Property</DropdownMenuLabel>
                {canAll && <DropdownMenuItem onSelect={() => setPropertyId(null)}>{t("label.all_properties")}</DropdownMenuItem>}
                {properties.map((p) => (
                  <DropdownMenuItem key={p.id} onSelect={() => setPropertyId(p.id)} className={cn(p.id === propertyId && "bg-brand-50")}>{p.name}<span className="ml-auto text-xs text-muted-foreground">{p.code}</span></DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <Button variant="secondary" className="w-72 justify-start text-muted-foreground" onClick={() => setSearchOpen(true)}>
              <Search /> Cari… <kbd className="ml-auto rounded border border-border bg-muted px-1.5 text-[10px]">Ctrl K</kbd>
            </Button>
            <div className="ml-auto flex items-center gap-2">
              <Popover>
                <PopoverTrigger asChild>
                  <Button variant="ghost" size="icon" aria-label="Notifikasi" className="relative">
                    <Bell />
                    {(notif.data?.unread_count ?? 0) > 0 && <Badge className="absolute -right-1 -top-1 h-[18px] min-w-[18px] justify-center bg-critical px-1 text-[10px] text-white">{notif.data!.unread_count}</Badge>}
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-[400px] p-0">
                  <NotificationInbox />
                </PopoverContent>
              </Popover>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" className="gap-2 px-2">
                    <Avatar name={principal?.full_name} />
                    <span className="max-w-[160px] truncate text-body">{principal?.full_name}</span>
                    <ChevronDown className="h-4 w-4 opacity-60" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent>
                  <DropdownMenuLabel>{principal?.roles.join(", ")}</DropdownMenuLabel>
                  <DropdownMenuItem asChild><Link to="/settings/notifications">{t("nav.notifications")}</Link></DropdownMenuItem>
                  <DropdownMenuItem asChild><Link to="/settings/profile">Profil & password</Link></DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onSelect={() => logout().then(() => nav("/login"))}><LogOut /> {t("auth.logout")}</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </header>
          <div className="bv-narrow-banner hidden bg-warning-soft px-8 py-2 text-sm text-warning-text">{t("state.narrow")}</div>
          <OfflineBanner />
          <main className="flex-1 overflow-y-auto">
            <div className="mx-auto max-w-[1440px] px-8 py-6">
              <Outlet />
            </div>
          </main>
        </div>
        <GlobalSearch open={searchOpen} onOpenChange={setSearchOpen} />
      </div>
    </TooltipProvider>
  );
}

// ---------- PageHeader (DS §3.3): breadcrumb → judul → status/flag → aksi (maks 1 Primary CTA) ----------
export function PageHeader({ title, subtitle, breadcrumb, badges, actions, children }: { title: React.ReactNode; subtitle?: React.ReactNode; breadcrumb?: React.ReactNode; badges?: React.ReactNode; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <div className="mb-5">
      {breadcrumb && <div className="mb-1 text-sm text-muted-foreground">{breadcrumb}</div>}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-h1 font-bold text-foreground">{title}</h1>
            {badges}
          </div>
          {subtitle && <p className="mt-0.5 text-sm text-muted-foreground">{subtitle}</p>}
        </div>
        {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children && <div className="mt-4">{children}</div>}
    </div>
  );
}
