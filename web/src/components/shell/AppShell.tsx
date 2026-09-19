// AppShell — pola konsol Factory Vision di atas design system Morphic/Nexus:
// sidebar panel dalam fill primary (bv/sidebar.css) dengan grup aktif "dipotong" sebagai tab di ground halaman,
// top bar (toggle sidebar, Property Switcher, pencarian ⌘K, tema terang/gelap, inbox, akun). Menu mengikuti IA PRD §26
// & route NC §48; disaring permission; menu non-P0 tidak ditampilkan.
import { useEffect, useMemo, useState } from "react";
import { Link, Outlet, useLocation, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { AnimatePresence, motion } from "motion/react";
import { Icon, IconButton } from "@buildingvision/ui";
import { BuildingVisionIcon, BuildingVisionLogo } from "@buildingvision/ui/bv";
import { Avatar, Badge, Button, Menu, Popover } from "@/components/ui/primitives";
import { OfflineBanner } from "@/components/bv/common";
import { useAuth } from "@/lib/auth";
import { PROFILE_ICON, PROFILE_LABEL, useProfile, type ProfileCode } from "@/lib/profile";
import { cn } from "@/lib/utils";
import { GlobalSearch } from "./GlobalSearch";
import { NotificationInbox } from "./NotificationInbox";
import { useNotifications } from "@/api/hooks";
import { setTimezone } from "@/lib/format";
import { track, useTrial } from "@/lib/growth";

// capability: modul hanya tampil bila profile property mengaktifkannya (Onboarding Brief §12; enforcement tetap server-side).
interface NavItem { to: string; label: string; icon?: string; perm?: string | string[]; capability?: string; internalOnly?: boolean }
interface NavGroup { key: string; label: string; icon: string; items?: NavItem[]; to?: string; perm?: string | string[]; capability?: string }

// IA PRD §26 (P0). Billing/Reports/Vendor tidak ditampilkan. Ikon: Material Symbols Rounded (DS Guideline §2.6).
const NAV: NavGroup[] = [
  { key: "overview", label: "nav.overview", icon: "space_dashboard", to: "/overview", perm: "overview.dashboard.view" },
  { key: "operations", label: "nav.operations", icon: "assignment", items: [
    { to: "/operations/tasks", label: "nav.tasks", icon: "task_alt", perm: "operations.tasks.view" },
    { to: "/operations/work-orders", label: "nav.work_orders", icon: "construction", perm: "operations.work_orders.view" },
    { to: "/operations/service-requests", label: "nav.service_requests", icon: "support_agent", perm: "tenant.service_requests.view" },
    { to: "/operations/incidents", label: "nav.incidents", icon: "emergency_home", perm: "operations.incidents.view" },
  ] },
  { key: "engineering", label: "nav.engineering", icon: "build", items: [
    { to: "/engineering/preventive-maintenance", label: "nav.preventive_maintenance", icon: "event_repeat", perm: "engineering.maintenance_plans.view" },
    { to: "/engineering/corrective-maintenance", label: "nav.corrective_maintenance", icon: "handyman", perm: "operations.work_orders.view" },
    { to: "/engineering/inspections", label: "nav.inspections", icon: "fact_check", perm: "engineering.inspections.view" },
    { to: "/engineering/assets", label: "nav.assets", icon: "precision_manufacturing", perm: "engineering.assets.view" },
    { to: "/engineering/equipment", label: "nav.equipment", icon: "settings_input_component", perm: "engineering.equipment.view" },
  ] },
  { key: "security", label: "nav.security", icon: "verified_user", items: [
    { to: "/security/patrol", label: "nav.patrol", icon: "directions_walk", perm: "security.patrol.view" },
    { to: "/security/incidents", label: "nav.incidents", icon: "emergency_home", perm: "operations.incidents.view" },
    { to: "/security/visitors", label: "nav.visitors", icon: "badge", perm: "security.visitors.view", capability: "visitor_management" },
  ] },
  { key: "housekeeping", label: "nav.housekeeping", icon: "cleaning_services", items: [
    { to: "/housekeeping/cleaning", label: "nav.cleaning", icon: "mop", perm: "housekeeping.cleaning.view" },
    { to: "/housekeeping/schedule", label: "nav.schedule", icon: "calendar_month", perm: "housekeeping.cleaning_schedules.view" },
    { to: "/housekeeping/inspections", label: "nav.inspections", icon: "fact_check", perm: "housekeeping.inspections.view" },
  ] },
  { key: "property", label: "nav.property", icon: "apartment", items: [
    { to: "/property/properties", label: "nav.properties", icon: "domain", perm: "property.locations.view" },
    { to: "/property/buildings", label: "nav.buildings", icon: "location_city", perm: "property.locations.view" },
    { to: "/property/towers", label: "nav.towers", icon: "corporate_fare", perm: "property.locations.view" },
    { to: "/property/floors", label: "nav.floors", icon: "layers", perm: "property.locations.view" },
    { to: "/property/areas", label: "nav.areas", icon: "grid_view", perm: "property.locations.view" },
    { to: "/property/spaces", label: "nav.spaces", icon: "meeting_room", perm: "property.locations.view" },
    { to: "/property/units", label: "nav.units", icon: "door_front", perm: "property.locations.view" },
  ] },
  // Tenant Relation = modul operasional mandatory (PRD v1.3 §3.2; NC v2.0 §71)
  { key: "tenant_relation", label: "nav.tenant_relation", icon: "support_agent", capability: "tenant_relation", items: [
    { to: "/tenant-relation", label: "nav.overview", icon: "space_dashboard", perm: "tenant.service_requests.view" },
    { to: "/tenant-relation/service-requests", label: "nav.service_requests", icon: "support_agent", perm: "tenant.service_requests.view" },
    { to: "/tenant-relation/tenant-users", label: "nav.tenant_users", icon: "how_to_reg", perm: "tenant_relation.tenant_users.view" },
    { to: "/tenant-relation/feedback", label: "nav.feedback", icon: "reviews", perm: "tenant_relation.feedback.view" },
    { to: "/tenant-relation/announcements", label: "nav.announcements", icon: "campaign", perm: "tenant_relation.announcements.view" },
  ] },
  { key: "booking", label: "nav.booking", icon: "event_available", capability: "facility_booking", items: [
    { to: "/booking/bookings", label: "nav.bookings", icon: "event_available", perm: "booking.bookings.view" },
    { to: "/booking/facilities", label: "nav.facilities", icon: "meeting_room", perm: "booking.facilities.view" },
  ] },
  { key: "reception", label: "nav.reception", icon: "room_service", to: "/reception", perm: "hotel.reservations.view", capability: "reception" },
  { key: "commercial", label: "nav.commercial", icon: "storefront", items: [
    { to: "/commercial/hotel/reservations", label: "nav.hotel_reservations", icon: "hotel", perm: "hotel.reservations.view", capability: "hotel_booking" },
    { to: "/commercial/hotel/calendar", label: "nav.hotel_calendar", icon: "calendar_month", perm: "hotel.reservations.view", capability: "hotel_booking" },
    { to: "/commercial/hotel/rooms", label: "nav.hotel_rooms", icon: "bed", perm: "hotel.rooms.view", capability: "hotel_booking" },
    { to: "/commercial/sales/listings", label: "nav.unit_listings", icon: "real_estate_agent", perm: "commercial.unit_listings.view", capability: "unit_sales" },
    { to: "/commercial/sales/leads", label: "nav.sales_leads", icon: "group", perm: "commercial.sales_leads.view", capability: "unit_sales" },
    { to: "/commercial/sales/reservations", label: "nav.sale_reservations", icon: "verified", perm: "commercial.sale_reservations.view", capability: "unit_sales" },
    { to: "/commercial/rental/listings", label: "nav.rental_listings", icon: "key", perm: "commercial.rental_listings.view", capability: "unit_rental" },
    { to: "/commercial/rental/reservations", label: "nav.rental_reservations", icon: "event_available", perm: "commercial.rental_reservations.view", capability: "unit_rental" },
    { to: "/commercial/rental/calendar", label: "nav.rental_calendar", icon: "calendar_month", perm: "commercial.rental_reservations.view", capability: "unit_rental" },
  ] },
  { key: "billing", label: "nav.billing", icon: "receipt_long", capability: "billing", items: [
    { to: "/billing/invoices", label: "nav.invoices", icon: "request_quote", perm: "billing.invoices.view" },
    { to: "/billing/payments", label: "nav.payments", icon: "payments", perm: "billing.payments.view" },
  ] },
  { key: "tenant", label: "nav.tenant", icon: "group", items: [
    { to: "/tenant/tenants", label: "nav.tenants", icon: "badge", perm: "property.tenants.view" },
    { to: "/tenant/service-requests", label: "nav.service_requests", icon: "support_agent", perm: "tenant.service_requests.view" },
  ] },
  { key: "assets", label: "nav.asset_management", icon: "inventory_2", items: [
    { to: "/assets", label: "nav.assets", icon: "precision_manufacturing", perm: "engineering.assets.view" },
    { to: "/assets/equipment", label: "nav.equipment", icon: "settings_input_component", perm: "engineering.equipment.view" },
    { to: "/assets/history", label: "nav.history", icon: "history", perm: "engineering.assets.view" },
  ] },
  { key: "vendor", label: "nav.vendor_management", icon: "handshake", to: "/vendors", perm: "vendor.vendors.view", capability: "vendor_management" },
  { key: "inventory", label: "nav.inventory", icon: "inventory", to: "/inventory", perm: "inventory.items.view", capability: "inventory" },
  { key: "reports", label: "nav.reports", icon: "analytics", to: "/reports", perm: "reports.reports.view", capability: "reports" },
  { key: "settings", label: "nav.settings", icon: "settings", items: [
    { to: "/settings/organization", label: "nav.organization", icon: "corporate_fare", perm: "platform.organizations.view" },
    { to: "/settings/plan", label: "nav.plan", icon: "workspace_premium", perm: "platform.organizations.view" },
    { to: "/settings/property-profile", label: "nav.property_profile", icon: "tune", perm: "property.properties.view" },
    { to: "/settings/users", label: "nav.users", icon: "manage_accounts", perm: "iam.users.view" },
    { to: "/settings/roles", label: "nav.roles", icon: "admin_panel_settings", perm: "iam.roles.view" },
    { to: "/settings/teams", label: "nav.teams", icon: "groups", perm: "iam.teams.view" },
    { to: "/settings/checklists", label: "nav.checklists", icon: "checklist", perm: "operations.checklists.view" },
    { to: "/settings/sla-policies", label: "nav.sla_policies", icon: "timer", perm: "operations.sla_policies.view" },
    { to: "/settings/master-data", label: "nav.master_data", icon: "database", perm: "engineering.equipment.view" },
    { to: "/settings/notifications", label: "nav.notifications", icon: "notifications", perm: "notification.inbox.view" },
    { to: "/settings/sync-conflicts", label: "nav.sync_conflicts", icon: "merge_type", perm: "sync.conflicts.view" },
    { to: "/settings/audit-logs", label: "nav.audit", icon: "receipt_long", perm: "platform.audit_logs.view" },
    { to: "/settings/app-downloads", label: "nav.app_downloads", icon: "download", perm: "platform.app_downloads.view", internalOnly: true }, // Website PRD §16: admin_internal
    { to: "/settings/demo-data", label: "nav.demo_data", icon: "database", perm: "platform.demo_data.view", internalOnly: true }, // Demo Seed §29: admin_internal
  ] },
];

function hasPerm(can: (p: string) => boolean, perm?: string | string[]): boolean {
  if (!perm) return true;
  return (Array.isArray(perm) ? perm : [perm]).some((p) => can(p));
}

type ThemeMode = "light" | "dark";
function useTheme(): [ThemeMode, () => void] {
  const [mode, setMode] = useState<ThemeMode>(() => {
    try {
      const saved = localStorage.getItem("bv.theme");
      if (saved === "dark" || saved === "light") return saved;
      return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    } catch {
      return "light";
    }
  });
  useEffect(() => {
    document.documentElement.setAttribute("data-theme", mode);
    try {
      localStorage.setItem("bv.theme", mode);
    } catch {
      /* ignore */
    }
  }, [mode]);
  return [mode, () => setMode((m) => (m === "dark" ? "light" : "dark"))];
}

const SIDEBAR_W = 232;
const SIDEBAR_W_COLLAPSED = 72;

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
  const [hoverGroup, setHoverGroup] = useState<string | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [inboxOpen, setInboxOpen] = useState(false);
  const [theme, toggleTheme] = useTheme();
  const notif = useNotifications(false);
  const prof = useProfile();

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
    const g = NAV.find((n) => n.items?.some((i) => loc.pathname.startsWith(i.to)));
    if (g) setOpen((o) => ({ ...o, [g.key]: true }));
    setHoverGroup(null);
  }, [loc.pathname]);
  const currentProperty = properties.find((p) => p.id === propertyId);
  useEffect(() => {
    setTimezone((currentProperty?.details?.timezone as string) || "Asia/Jakarta");
  }, [currentProperty]);
  const canAll = useMemo(() => principal?.properties.some((s) => s.property_id === null) ?? false, [principal]);
  const hasCap = (c?: string) => !c || prof.has(c);
  const visibleNav = NAV.filter((g) => hasCap(g.capability))
    .map((g) => ({ ...g, items: g.items?.filter((i) => hasPerm(can, i.perm) && hasCap(i.capability) && (!i.internalOnly || !!principal?.is_internal_admin)) }))
    .filter((g) => (g.to ? hasPerm(can, g.perm) : (g.items?.length ?? 0) > 0));
  const isGroupActive = (g: NavGroup) => (g.to ? loc.pathname.startsWith(g.to) : !!g.items?.some((i) => loc.pathname.startsWith(i.to)));
  const exactOnly = new Set(["/assets", "/tenant-relation"]); // item induk yang punya sub-item: aktif hanya pada path persis
  const isItemActive = (i: NavItem) => loc.pathname === i.to || (!exactOnly.has(i.to) && loc.pathname.startsWith(i.to + "/")) || (i.to === "/assets" && /^\/assets\/[^/]+$/.test(loc.pathname) && !loc.pathname.startsWith("/assets/equipment") && !loc.pathname.startsWith("/assets/history"));

  return (
    <div className="flex h-dvh min-h-screen overflow-hidden bg-background text-on-background">
      {/* Sidebar: panel fill primary; grup aktif dipotong sebagai tab di ground halaman (bv/sidebar.css) */}
      <aside className="bv-sidebar" style={{ width: collapsed ? SIDEBAR_W_COLLAPSED : SIDEBAR_W }}>
        <div className="flex h-16 items-center" style={{ padding: collapsed ? "16px 8px 8px" : "16px 14px 8px", justifyContent: collapsed ? "center" : "flex-start" }}>
          {collapsed ? (
            <button type="button" onClick={() => setCollapsed(false)} className="rounded-[var(--radius-md)] p-1" title="Buka sidebar" aria-label="Buka sidebar">
              <BuildingVisionIcon size={36} tone="white" />
            </button>
          ) : (
            <BuildingVisionLogo height={34} tone="white" />
          )}
        </div>

        <nav className="bv-sidebar__nav flex flex-1 flex-col overflow-y-auto overflow-x-hidden" style={{ padding: collapsed ? "12px 0 12px" : "12px 0 12px 8px", gap: collapsed ? 8 : 4 }} aria-label="Navigasi utama">
          {visibleNav.map((g) => {
            const active = isGroupActive(g);
            const expanded = open[g.key] ?? active;
            const items = g.items ?? [];

            // ----- collapsed: ikon + flyout saat hover -----
            if (collapsed) {
              return (
                <div key={g.key} className="relative flex justify-center" onMouseEnter={() => setHoverGroup(g.key)} onMouseLeave={() => setHoverGroup(null)}>
                  <button
                    type="button"
                    onClick={() => nav(g.to ?? items[0]!.to)}
                    className={cn("flex h-11 w-11 items-center justify-center rounded-[var(--radius-md)] transition-colors", active ? "bv-sidebar__tab" : "hover:bg-[var(--bv-sidebar-fill-raised)]")}
                    style={active ? { borderRadius: "var(--radius-pill) 0 0 var(--radius-pill)", width: 52, marginLeft: 20 } : undefined}
                    title={t(g.label)}
                    aria-label={t(g.label)}
                  >
                    {active && (
                      <>
                        <span className="bv-sidebar__corner bv-sidebar__corner--top" aria-hidden />
                        <span className="bv-sidebar__corner bv-sidebar__corner--bottom" aria-hidden />
                      </>
                    )}
                    <Icon name={g.icon} size={22} color={active ? "var(--color-primary)" : "var(--bv-sidebar-ink)"} />
                  </button>
                  <AnimatePresence>
                    {hoverGroup === g.key && items.length > 0 && (
                      <motion.div initial={{ opacity: 0, x: -6 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: -6 }} transition={{ duration: 0.15 }} className="absolute left-full top-0 z-50 ml-2 min-w-[220px] rounded-[var(--radius-lg)] border border-border bg-surface p-2 text-on-surface" style={{ boxShadow: "var(--elevation-3)" }}>
                        <div className="px-2 py-1.5 text-xs font-extrabold uppercase tracking-wide text-on-surface-variant">{t(g.label)}</div>
                        {items.map((i) => (
                          <Link key={i.to} to={i.to} className={cn("flex items-center gap-2 rounded-[var(--radius-sm)] px-2 py-1.5 text-sm hover:bg-surface-container", isItemActive(i) && "bg-primary-soft font-bold text-primary")}>
                            <Icon name={i.icon ?? "circle"} size={16} />
                            {t(i.label)}
                          </Link>
                        ))}
                      </motion.div>
                    )}
                  </AnimatePresence>
                </div>
              );
            }

            // ----- expanded: header grup (tab bila aktif) + accordion sub-menu -----
            const header = (
              <>
                {active && (
                  <>
                    <span className="bv-sidebar__corner bv-sidebar__corner--top" aria-hidden />
                    <span className="bv-sidebar__corner bv-sidebar__corner--bottom" aria-hidden />
                  </>
                )}
                <span className="flex min-w-0 items-center gap-2">
                  <span className="flex h-[26px] w-[26px] shrink-0 items-center justify-center rounded-full" style={{ backgroundColor: active ? "var(--color-primary)" : "var(--bv-sidebar-fill-raised)" }}>
                    <Icon name={g.icon} size={17} color={active ? "var(--color-on-primary)" : "var(--bv-sidebar-ink)"} />
                  </span>
                  <span className="truncate">{t(g.label)}</span>
                </span>
                {items.length > 0 && (
                  <motion.span animate={{ rotate: expanded ? 180 : 0 }} transition={{ duration: 0.2 }} className="flex items-center">
                    <Icon name="expand_more" size={16} color={active ? "var(--color-primary)" : "var(--bv-sidebar-ink-muted)"} />
                  </motion.span>
                )}
              </>
            );
            const headerClass = cn("flex w-full items-center justify-between text-left text-[11.5px] transition-colors", active ? "bv-sidebar__tab font-extrabold" : "rounded-[var(--radius-pill)] font-bold hover:bg-[var(--bv-sidebar-fill-raised)]");
            const headerStyle: React.CSSProperties = { marginRight: active ? 0 : 8, padding: active ? "8px 16px 8px 8px" : "8px 8px", color: active ? "var(--color-primary)" : "var(--bv-sidebar-ink)" };
            return (
              <div key={g.key} className="flex flex-col">
                {g.to ? (
                  <Link to={g.to} className={headerClass} style={headerStyle}>
                    {header}
                  </Link>
                ) : (
                  <button type="button" onClick={() => setOpen((o) => ({ ...o, [g.key]: !expanded }))} className={headerClass} style={headerStyle} aria-expanded={expanded}>
                    {header}
                  </button>
                )}
                <AnimatePresence initial={false}>
                  {items.length > 0 && expanded && (
                    <motion.div initial={{ height: 0, opacity: 0 }} animate={{ height: "auto", opacity: 1 }} exit={{ height: 0, opacity: 0 }} transition={{ duration: 0.2, ease: "easeOut" }} className="mr-2 mt-1.5 flex flex-col gap-0.5 overflow-hidden pl-5">
                      {items.map((i) => {
                        const a = isItemActive(i);
                        return (
                          <Link
                            key={i.to}
                            to={i.to}
                            className={cn("flex items-center gap-2 rounded-[var(--radius-pill)] px-2 py-1.5 text-[11.5px] transition-colors", a ? "font-extrabold" : "font-medium hover:bg-[var(--bv-sidebar-fill-raised)]")}
                            style={{ backgroundColor: a ? "var(--bv-sidebar-fill-raised)" : undefined, color: a ? "var(--bv-sidebar-ink)" : "var(--bv-sidebar-ink-muted)" }}
                          >
                            <Icon name={i.icon ?? "circle"} size={14} />
                            <span className="truncate">{t(i.label)}</span>
                          </Link>
                        );
                      })}
                    </motion.div>
                  )}
                </AnimatePresence>
              </div>
            );
          })}
        </nav>

        {/* Footer panel: versi + tombol ciutkan */}
        <div className="flex items-center justify-between px-3 py-2" style={{ backgroundColor: "var(--bv-sidebar-fill-raised)", borderRadius: "0 0 var(--radius-xl) 0", color: "var(--bv-sidebar-ink-muted)" }}>
          {!collapsed && <span className="text-[10.5px] font-semibold">BuildingVision</span>}
          <button type="button" onClick={() => setCollapsed((c) => !c)} className="mx-auto flex h-8 w-8 items-center justify-center rounded-full hover:bg-[var(--bv-sidebar-fill)]" style={{ color: "var(--bv-sidebar-ink)", marginLeft: collapsed ? "auto" : 0 }} aria-label={collapsed ? "Buka sidebar" : "Ciutkan sidebar"}>
            <Icon name={collapsed ? "keyboard_double_arrow_right" : "keyboard_double_arrow_left"} size={18} />
          </button>
        </div>
      </aside>

      {/* Main */}
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-16 shrink-0 items-center gap-3 px-5">
          <IconButton icon={<Icon name={collapsed ? "menu" : "menu_open"} size={22} />} aria-label={collapsed ? "Buka sidebar" : "Ciutkan sidebar"} onClick={() => setCollapsed((c) => !c)} />
          {/* Property Switcher (wajib, DS §3.1) */}
          <Menu
            align="start"
            width={280}
            header="Property"
            trigger={
              <Button variant="secondary" size="sm" className="min-w-[220px] justify-between">
                <span className="inline-flex items-center gap-2 truncate">
                  <Icon name={prof.profile ? PROFILE_ICON[prof.profile] : "apartment"} size={18} className="text-primary" />
                  <span className="truncate">{currentProperty?.name ?? (propertyId ? "…" : t("label.all_properties"))}</span>
                  {prof.profile && <span className="shrink-0 rounded-full bg-primary-soft px-1.5 text-[10px] font-semibold uppercase tracking-wide text-primary">{PROFILE_LABEL[prof.profile]}</span>}
                </span>
                <Icon name="expand_more" size={18} className="opacity-60" />
              </Button>
            }
            items={[
              ...(canAll ? [{ label: t("label.all_properties"), onSelect: () => setPropertyId(null), icon: "domain" }] : []),
              ...properties.map((p) => ({
                label: (
                  <span className="flex w-full items-center">
                    {p.name}
                    <span className="ml-auto text-xs text-on-surface-variant">{PROFILE_LABEL[(p.details?.profile as ProfileCode) ?? "office"] ?? p.code}</span>
                  </span>
                ),
                onSelect: () => setPropertyId(p.id),
                icon: p.id === propertyId ? "radio_button_checked" : "radio_button_unchecked",
              })),
            ]}
          />
          <button type="button" onClick={() => setSearchOpen(true)} className="flex h-9 w-72 items-center gap-2 rounded-[var(--radius-pill)] border border-border bg-surface px-3 text-sm text-on-surface-variant hover:bg-surface-container">
            <Icon name="search" size={18} /> Cari…
            <kbd className="ml-auto rounded-[var(--radius-xs)] border border-border bg-surface-container px-1.5 text-[10px]">Ctrl K</kbd>
          </button>
          <div className="ml-auto flex items-center gap-1">
            <TrialBadge />
            <IconButton icon={<Icon name={theme === "dark" ? "light_mode" : "dark_mode"} size={20} />} aria-label={theme === "dark" ? "Tema terang" : "Tema gelap"} onClick={toggleTheme} />
            <Popover open={inboxOpen} onOpenChange={setInboxOpen} width={400} className="p-0" trigger={
              <span className="relative inline-flex">
                <IconButton icon={<Icon name="notifications" size={20} />} aria-label="Notifikasi" />
                {(notif.data?.unread_count ?? 0) > 0 && (
                  <Badge tone="error" className="pointer-events-none absolute -right-0.5 -top-0.5 h-[18px] min-w-[18px] justify-center px-1 text-[10px]">
                    {notif.data!.unread_count}
                  </Badge>
                )}
              </span>
            }>
              <NotificationInbox onNavigate={() => setInboxOpen(false)} />
            </Popover>
            <Menu
              width={240}
              header={principal?.roles.join(", ")}
              trigger={
                <button type="button" className="flex items-center gap-2 rounded-[var(--radius-pill)] px-2 py-1 hover:bg-surface-container">
                  <Avatar name={principal?.full_name} size={32} />
                  <span className="max-w-[160px] truncate text-sm font-semibold">{principal?.full_name}</span>
                  <Icon name="expand_more" size={18} className="opacity-60" />
                </button>
              }
              items={[
                { label: t("nav.notifications"), icon: "notifications", onSelect: () => nav("/settings/notifications") },
                { label: "Profil & password", icon: "person", onSelect: () => nav("/settings/profile") },
                "separator",
                { label: t("auth.logout"), icon: "logout", destructive: true, onSelect: () => logout().then(() => nav("/login")) },
              ]}
            />
          </div>
        </header>
        <div className="bv-narrow-banner hidden px-8 py-2 text-sm" style={{ backgroundColor: "var(--color-warning-container)", color: "var(--color-on-warning-container)" }}>{t("state.narrow")}</div>
        <OfflineBanner />
        <TrialLockedBanner />
        <main className="flex-1 overflow-y-auto">
          <div className="mx-auto max-w-[1440px] px-6 pb-8 pt-2">
            <Outlet />
          </div>
        </main>
      </div>
      <GlobalSearch open={searchOpen} onOpenChange={setSearchOpen} />
    </div>
  );
}

// ---------- PageHeader (DS §3.3): breadcrumb → judul → status/flag → aksi (maks 1 Primary CTA) ----------
export function PageHeader({ title, subtitle, breadcrumb, badges, actions, children }: { title: React.ReactNode; subtitle?: React.ReactNode; breadcrumb?: React.ReactNode; badges?: React.ReactNode; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <div className="mb-5">
      {breadcrumb && <div className="mb-1 text-sm text-on-surface-variant">{breadcrumb}</div>}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-[24px] font-extrabold leading-tight tracking-tight text-on-surface">{title}</h1>
            {badges}
          </div>
          {subtitle && <p className="mt-1 text-sm text-on-surface-variant">{subtitle}</p>}
        </div>
        {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children && <div className="mt-4">{children}</div>}
    </div>
  );
}

// ---------- Trial status (Website PRD §31): badge di top bar + banner read-only saat trial berakhir ----------
function TrialBadge() {
  const { can } = useAuth();
  const q = useTrial(can("platform.organizations.view"));
  useEffect(() => {
    track("dashboard_opened");
  }, []);
  const tr = q.data?.trial;
  if (!tr || tr.status === "none" || tr.status === "converted") return null;
  const tone = tr.locked ? { bg: "var(--color-error-container)", fg: "var(--color-on-error-container)" } : tr.status === "trial_ending_soon" ? { bg: "var(--color-warning-container)", fg: "var(--color-on-warning-container)" } : { bg: "var(--color-primary-container)", fg: "var(--color-on-primary-container)" };
  const label = tr.locked ? (tr.status === "cancelled" ? "Trial cancelled" : "Trial expired") : `Trial · ${tr.days_left} day${tr.days_left === 1 ? "" : "s"} left`;
  return (
    <Link to="/settings/plan" className="mr-1 inline-flex h-8 items-center gap-1.5 rounded-[var(--radius-pill)] px-3 text-xs font-semibold" style={{ backgroundColor: tone.bg, color: tone.fg }}>
      <Icon name={tr.locked ? "lock" : "hourglass_top"} size={16} />
      {label}
      <span className="hidden font-medium underline lg:inline">Choose a plan</span>
    </Link>
  );
}

function TrialLockedBanner() {
  const { can } = useAuth();
  const q = useTrial(can("platform.organizations.view"));
  const tr = q.data?.trial;
  if (!tr?.locked) return null;
  return (
    <div className="flex items-center gap-3 px-8 py-2 text-sm" style={{ backgroundColor: "var(--color-error-container)", color: "var(--color-on-error-container)" }}>
      <Icon name="lock" size={18} />
      <span>{tr.status === "cancelled" ? "Your trial was cancelled." : "Your trial has expired."} Your data is safe and viewing still works, but changes are paused until a plan is chosen.</span>
      <Link to="/settings/plan" className="ml-auto font-semibold underline">Choose a plan</Link>
    </div>
  );
}
