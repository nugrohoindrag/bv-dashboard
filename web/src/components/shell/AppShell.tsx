// AppShell — pola konsol Factory Vision di atas design system Morphic/Nexus:
// sidebar panel dalam fill primary (bv/sidebar.css) dengan grup aktif "dipotong" sebagai tab di ground halaman,
// top bar (menu, Property Switcher, pencarian ⌘K, tema terang/gelap, inbox, akun). Menu = Navigation Foundation PRD P0 §21
// (app/navigation.ts), disaring permission & capability.
// Responsive (PRD P0 §22): desktop ≥1280 sidebar penuh (bisa diciutkan); tablet 768–1279 rail ikon otomatis;
// mobile <768 sidebar menjadi drawer off-canvas (hamburger, scrim, tutup saat navigasi/Esc) dan top bar ringkas.
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
import { useBreakpoint } from "@/lib/responsive";
import { cn } from "@/lib/utils";
import { NAV, isGroupActive, isItemActive, visibleNav, type NavGroup } from "@/app/navigation";
import { useAccessContext } from "@/app/access";
import { GlobalSearch } from "./GlobalSearch";
import { NotificationInbox } from "./NotificationInbox";
import { useNotifications } from "@/api/hooks";
import { setTimezone } from "@/lib/format";
import { track, useTrial } from "@/lib/growth";

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
const DRAWER_W = 280;

export function AppShell() {
  const { t } = useTranslation();
  const { principal, properties, propertyId, setPropertyId, logout } = useAuth();
  const nav = useNavigate();
  const loc = useLocation();
  const bp = useBreakpoint();
  const [collapsedPref, setCollapsedPref] = useState<boolean>(() => {
    try {
      return localStorage.getItem("bv.sidebar") === "collapsed";
    } catch {
      return false;
    }
  });
  const [railExpanded, setRailExpanded] = useState(false); // tablet: lebarkan sementara (tidak disimpan)
  const [drawerOpen, setDrawerOpen] = useState(false); // mobile
  const [searchOpen, setSearchOpen] = useState(false);
  const [inboxOpen, setInboxOpen] = useState(false);
  const [theme, toggleTheme] = useTheme();
  const notif = useNotifications(false);
  const prof = useProfile();
  const collapsed = bp === "tablet" ? !railExpanded : collapsedPref;

  useEffect(() => {
    try {
      localStorage.setItem("bv.sidebar", collapsedPref ? "collapsed" : "open");
    } catch {
      /* ignore */
    }
  }, [collapsedPref]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setSearchOpen(true);
      }
      if (e.key === "Escape") setDrawerOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  const currentProperty = properties.find((p) => p.id === propertyId);
  useEffect(() => {
    setTimezone((currentProperty?.details?.timezone as string) || "Asia/Jakarta");
  }, [currentProperty]);
  const canAll = useMemo(() => principal?.properties.some((s) => s.property_id === null) ?? false, [principal]);
  const access = useAccessContext();
  const items = visibleNav(NAV, access);
  const afterNavigate = () => {
    setDrawerOpen(false);
    if (bp === "tablet") setRailExpanded(false);
  };
  const toggleSidebar = () => {
    if (bp === "mobile") setDrawerOpen((o) => !o);
    else if (bp === "tablet") setRailExpanded((o) => !o);
    else setCollapsedPref((c) => !c);
  };
  const isMobile = bp === "mobile";

  return (
    <div className="bv-shell flex h-dvh min-h-screen overflow-hidden bg-background text-on-background">
      {/* Sidebar tetap (tablet: rail; desktop: penuh/ciut) */}
      {!isMobile && (
        <aside className="bv-sidebar" style={{ width: collapsed ? SIDEBAR_W_COLLAPSED : SIDEBAR_W }} data-testid="sidebar">
          <SidebarPanel groups={items} collapsed={collapsed} pathname={loc.pathname} onNavigate={afterNavigate} onExpand={() => (bp === "tablet" ? setRailExpanded(true) : setCollapsedPref(false))} onToggle={toggleSidebar} />
        </aside>
      )}
      {/* Mobile: drawer off-canvas */}
      <AnimatePresence>
        {isMobile && drawerOpen && (
          <div className="fixed inset-0 z-[990]" role="dialog" aria-modal="true" aria-label={t("shell.navigation")}>
            <motion.button type="button" aria-label={t("action.close")} initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="absolute inset-0" style={{ backgroundColor: "var(--color-scrim)" }} onClick={() => setDrawerOpen(false)} />
            <motion.aside
              initial={{ x: -DRAWER_W }}
              animate={{ x: 0 }}
              exit={{ x: -DRAWER_W }}
              transition={{ duration: 0.22, ease: [0.2, 0, 0, 1] }}
              className="bv-sidebar absolute inset-y-0 left-0 h-dvh"
              style={{ width: DRAWER_W, maxWidth: "85vw", boxShadow: "var(--elevation-3)" }}
              data-testid="sidebar-drawer"
            >
              <SidebarPanel groups={items} collapsed={false} pathname={loc.pathname} onNavigate={afterNavigate} onClose={() => setDrawerOpen(false)} />
            </motion.aside>
          </div>
        )}
      </AnimatePresence>

      {/* Main */}
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-[var(--bv-header-h)] shrink-0 items-center gap-1.5 border-b border-border bg-surface px-2 md:gap-3 md:px-5">
          <IconButton icon={<Icon name={isMobile ? "menu" : collapsed ? "menu" : "menu_open"} size={22} />} aria-label={isMobile ? t("shell.open_menu") : collapsed ? t("shell.expand_sidebar") : t("shell.collapse_sidebar")} aria-expanded={isMobile ? drawerOpen : !collapsed} onClick={toggleSidebar} />
          {/* Property Switcher (wajib, DS §3.1) — di mobile menyusut menjadi nama terpotong */}
          <Menu
            align="start"
            width={280}
            header="Property"
            trigger={
              <Button variant="secondary" size="sm" className="min-w-0 max-w-[44vw] justify-between md:min-w-[220px] md:max-w-none" aria-label={t("shell.property_switcher")}>
                <span className="inline-flex min-w-0 items-center gap-2">
                  <Icon name={prof.profile ? PROFILE_ICON[prof.profile] : "apartment"} size={18} className="shrink-0 text-primary" />
                  <span className="truncate">{currentProperty?.name ?? (propertyId ? "…" : t("label.all_properties"))}</span>
                  {prof.profile && <span className="hidden shrink-0 rounded-full bg-primary-soft px-1.5 text-[10px] font-semibold uppercase tracking-wide text-primary lg:inline">{PROFILE_LABEL[prof.profile]}</span>}
                </span>
                <Icon name="expand_more" size={18} className="shrink-0 opacity-60" />
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
          {isMobile ? (
            <IconButton icon={<Icon name="search" size={20} />} aria-label={t("shell.search")} onClick={() => setSearchOpen(true)} />
          ) : (
            <button type="button" onClick={() => setSearchOpen(true)} className="flex h-9 w-44 items-center gap-2 rounded-[var(--radius-pill)] border border-border bg-surface px-3 text-sm text-on-surface-variant hover:bg-surface-container xl:w-72">
              <Icon name="search" size={18} /> <span className="truncate">{t("shell.search")}…</span>
              <kbd className="ml-auto hidden rounded-[var(--radius-xs)] border border-border bg-surface-container px-1.5 text-[10px] lg:inline">Ctrl K</kbd>
            </button>
          )}
          <div className="ml-auto flex shrink-0 items-center gap-0.5 md:gap-1">
            <TrialBadge />
            <IconButton icon={<Icon name={theme === "dark" ? "light_mode" : "dark_mode"} size={20} />} aria-label={theme === "dark" ? t("shell.theme_light") : t("shell.theme_dark")} onClick={toggleTheme} />
            <Popover open={inboxOpen} onOpenChange={setInboxOpen} width={Math.min(400, (typeof window !== "undefined" ? window.innerWidth : 400) - 16)} className="p-0" trigger={
              <span className="relative inline-flex">
                <IconButton icon={<Icon name="notifications" size={20} />} aria-label={t("nav.notifications")} />
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
                <button type="button" className="flex items-center gap-2 rounded-[var(--radius-pill)] px-1 py-1 hover:bg-surface-container md:px-2" aria-label={t("shell.account")}>
                  <Avatar name={principal?.full_name} size={32} />
                  <span className="hidden max-w-[160px] truncate text-sm font-semibold lg:inline">{principal?.full_name}</span>
                  <Icon name="expand_more" size={18} className="hidden opacity-60 md:inline-block" />
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
        <OfflineBanner />
        <TrialLockedBanner />
        {/* relative: elemen absolut di konten (mis. label sr-only) terkurung di area scroll ini, tidak memanjangkan dokumen */}
        <main className="relative flex-1 overflow-y-auto overflow-x-hidden">
          <div className="mx-auto max-w-[1440px] px-4 pb-8 pt-2 md:px-6">
            <Outlet />
          </div>
        </main>
      </div>
      <GlobalSearch open={searchOpen} onOpenChange={setSearchOpen} />
    </div>
  );
}

// ---------- Isi panel sidebar (dipakai sidebar tetap & drawer mobile) ----------
function SidebarPanel({ groups, collapsed, pathname, onNavigate, onExpand, onToggle, onClose }: { groups: NavGroup[]; collapsed: boolean; pathname: string; onNavigate: () => void; onExpand?: () => void; onToggle?: () => void; onClose?: () => void }) {
  const { t } = useTranslation();
  const nav = useNavigate();
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [hoverGroup, setHoverGroup] = useState<string | null>(null);
  const go = (to: string) => {
    setHoverGroup(null);
    onNavigate();
    nav(to);
  };
  return (
    <>
      <div className="flex h-16 items-center" style={{ padding: collapsed ? "16px 8px 8px" : "16px 14px 8px", justifyContent: collapsed ? "center" : "space-between" }}>
        {collapsed ? (
          <button type="button" onClick={onExpand} className="rounded-[var(--radius-md)] p-1" title={t("shell.expand_sidebar")} aria-label={t("shell.expand_sidebar")}>
            <BuildingVisionIcon size={36} tone="white" />
          </button>
        ) : (
          <BuildingVisionLogo height={34} tone="white" />
        )}
        {onClose && (
          <button type="button" onClick={onClose} className="flex h-9 w-9 items-center justify-center rounded-full hover:bg-[var(--bv-sidebar-fill-raised)]" style={{ color: "var(--bv-sidebar-ink)" }} aria-label={t("action.close")}>
            <Icon name="close" size={20} />
          </button>
        )}
      </div>

      <nav className="bv-sidebar__nav flex flex-1 flex-col overflow-y-auto overflow-x-hidden" style={{ padding: collapsed ? "12px 0 12px" : "12px 0 12px 8px", gap: collapsed ? 8 : 4 }} aria-label={t("shell.navigation")}>
        {groups.map((g) => {
          const active = isGroupActive(g, pathname);
          const expanded = open[g.key] ?? active;
          const items = g.items ?? [];

          // ----- collapsed (rail): ikon + flyout saat hover -----
          if (collapsed) {
            return (
              <div key={g.key} className="relative flex justify-center" onMouseEnter={() => setHoverGroup(g.key)} onMouseLeave={() => setHoverGroup(null)}>
                <button
                  type="button"
                  onClick={() => go(g.to ?? items[0]!.to)}
                  className={cn("flex h-11 w-11 items-center justify-center rounded-[var(--radius-md)] transition-colors", active ? "bv-sidebar__tab" : "hover:bg-[var(--bv-sidebar-fill-raised)]")}
                  style={active ? { borderRadius: "var(--radius-pill) 0 0 var(--radius-pill)", width: 52, marginLeft: 20 } : undefined}
                  title={t(g.label)}
                  aria-label={t(g.label)}
                  aria-current={active ? "page" : undefined}
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
                        <Link key={i.to} to={i.to} onClick={() => { setHoverGroup(null); onNavigate(); }} className={cn("flex items-center gap-2 rounded-[var(--radius-sm)] px-2 py-1.5 text-sm hover:bg-surface-container", isItemActive(i, pathname) && "bg-primary-soft font-bold text-primary")}>
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
                <Link to={g.to} onClick={onNavigate} className={headerClass} style={headerStyle} aria-current={active ? "page" : undefined}>
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
                      const a = isItemActive(i, pathname);
                      return (
                        <Link
                          key={i.to}
                          to={i.to}
                          onClick={onNavigate}
                          aria-current={a ? "page" : undefined}
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

      {/* Footer panel: versi + tombol ciutkan (tidak ada di drawer mobile) */}
      <div className="flex items-center justify-between px-3 py-2" style={{ backgroundColor: "var(--bv-sidebar-fill-raised)", borderRadius: onClose ? undefined : "0 0 var(--radius-xl) 0", color: "var(--bv-sidebar-ink-muted)" }}>
        {!collapsed && <span className="text-[10.5px] font-semibold">BuildingVision</span>}
        {onToggle && (
          <button type="button" onClick={onToggle} className="mx-auto flex h-8 w-8 items-center justify-center rounded-full hover:bg-[var(--bv-sidebar-fill)]" style={{ color: "var(--bv-sidebar-ink)", marginLeft: collapsed ? "auto" : 0 }} aria-label={collapsed ? t("shell.expand_sidebar") : t("shell.collapse_sidebar")}>
            <Icon name={collapsed ? "keyboard_double_arrow_right" : "keyboard_double_arrow_left"} size={18} />
          </button>
        )}
      </div>
    </>
  );
}

// ---------- PageHeader (DS §3.3): breadcrumb → judul → status/flag → aksi (maks 1 Primary CTA) ----------
// Judul memakai token tipografi h1; aksi membungkus ke baris baru di layar sempit (PRD P0 §22).
export function PageHeader({ title, subtitle, breadcrumb, badges, actions, children }: { title: React.ReactNode; subtitle?: React.ReactNode; breadcrumb?: React.ReactNode; badges?: React.ReactNode; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <div className="mb-5">
      {breadcrumb && <div className="mb-1 text-sm text-on-surface-variant">{breadcrumb}</div>}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1 basis-64">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="min-w-0 break-words text-h1 font-extrabold tracking-tight text-on-surface">{title}</h1>
            {badges}
          </div>
          {subtitle && <div className="mt-1 text-sm text-on-surface-variant">{subtitle}</div>}
        </div>
        {actions && <div className="flex max-w-full flex-wrap items-center gap-2 sm:shrink-0">{actions}</div>}
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
    <Link to="/settings/plan" title={label} aria-label={label} className="mr-1 inline-flex h-8 items-center gap-1.5 rounded-[var(--radius-pill)] px-3 text-xs font-semibold" style={{ backgroundColor: tone.bg, color: tone.fg }}>
      <Icon name={tr.locked ? "lock" : "hourglass_top"} size={16} />
      <span className="hidden sm:inline">{label}</span>
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
    <div className="flex flex-wrap items-center gap-3 px-4 py-2 text-sm md:px-8" style={{ backgroundColor: "var(--color-error-container)", color: "var(--color-on-error-container)" }}>
      <Icon name="lock" size={18} />
      <span>{tr.status === "cancelled" ? "Your trial was cancelled." : "Your trial has expired."} Your data is safe and viewing still works, but changes are paused until a plan is chosen.</span>
      <Link to="/settings/plan" className="ml-auto font-semibold underline">Choose a plan</Link>
    </div>
  );
}
