// Shell website (Website PRD §5, §23): header dengan navigasi Home · Solutions · Platform · Pricing · Resources · Download Apps ·
// Login · Start Free Trial; footer dengan Download Apps. Responsif: menu mobile penuh layar.
import { useEffect, useState } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router-dom";
import { BuildingVisionLogo } from "@bv/logo";
import { Icon } from "./Icon";
import { ButtonLink, Container, cn } from "./ui";
import { COMPANY, LEGAL, PLATFORM, RESOURCES, SOLUTIONS, type NavLeaf } from "@/content/site";
import { LINKS, SITE_NAME, TAGLINE, signupHref, SHOW_PRICING } from "@/lib/config";
import { track } from "@/lib/analytics";

function Dropdown({ label, items, to, wide }: { label: string; items: NavLeaf[]; to: string; wide?: boolean }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="relative" onMouseEnter={() => setOpen(true)} onMouseLeave={() => setOpen(false)}>
      <NavLink to={to} className={({ isActive }) => cn("inline-flex h-10 items-center gap-1 rounded-[var(--radius-pill)] px-3 text-sm font-medium hover:bg-surface-container-low", isActive ? "text-primary" : "text-on-surface")} onFocus={() => setOpen(true)} onClick={() => setOpen(false)}>
        {label} <Icon name="expand_more" size={18} />
      </NavLink>
      {open && (
        <div className={cn("absolute left-0 top-full z-40 pt-2", wide ? "w-[640px]" : "w-[320px]")}>
          <div className={cn("grid gap-1 rounded-[var(--radius-xl)] border border-border bg-surface p-2 shadow-xl", wide && "grid-cols-2")}>
            {items.map((it) => (
              <Link key={it.to} to={it.to} className="flex items-start gap-3 rounded-[var(--radius-lg)] p-3 hover:bg-surface-container-low" onClick={() => setOpen(false)}>
                {it.icon && <Icon name={it.icon} size={22} className="mt-0.5 text-primary" />}
                <span>
                  <span className="block text-sm font-semibold text-on-surface">{it.label}</span>
                  {it.blurb && <span className="block text-xs text-on-surface-variant">{it.blurb}</span>}
                </span>
              </Link>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export function Header() {
  const [mobile, setMobile] = useState(false);
  const loc = useLocation();
  useEffect(() => setMobile(false), [loc.pathname]);
  useEffect(() => {
    document.body.style.overflow = mobile ? "hidden" : "";
    return () => {
      document.body.style.overflow = "";
    };
  }, [mobile]);
  return (
    <header className="sticky top-0 z-50 border-b border-border/70 bg-background/90 backdrop-blur">
      <Container className="flex h-16 items-center gap-2">
        <Link to="/" aria-label={`${SITE_NAME} home`} className="mr-2 shrink-0"><BuildingVisionLogo height={30} tone="brand" /></Link>
        <nav className="hidden items-center gap-0.5 lg:flex" aria-label="Main">
          <Dropdown label="Solutions" items={SOLUTIONS} to="/solutions/hotel" />
          <Dropdown label="Platform" items={PLATFORM} to="/platform" wide />
          {SHOW_PRICING && <NavLink to="/pricing" className={({ isActive }) => cn("inline-flex h-10 items-center rounded-[var(--radius-pill)] px-3 text-sm font-medium hover:bg-surface-container-low", isActive ? "text-primary" : "text-on-surface")}>Pricing</NavLink>}
          <Dropdown label="Resources" items={RESOURCES} to="/resources" />
          <NavLink to="/download" className={({ isActive }) => cn("inline-flex h-10 items-center gap-1 rounded-[var(--radius-pill)] px-3 text-sm font-medium hover:bg-surface-container-low", isActive ? "text-primary" : "text-on-surface")}><Icon name="download" size={18} /> Download Apps</NavLink>
        </nav>
        <div className="ml-auto hidden items-center gap-2 lg:flex">
          <a href={LINKS.login} className="inline-flex h-10 items-center rounded-[var(--radius-pill)] px-3 text-sm font-semibold text-on-surface hover:bg-surface-container-low" onClick={() => track("login_clicked")}>Log in</a>
          <ButtonLink to={signupHref("header")} size="sm" onClick={() => track("start_trial_clicked", { placement: "header" })}>Start Free Trial</ButtonLink>
        </div>
        <button type="button" className="ml-auto inline-flex h-10 w-10 items-center justify-center rounded-full hover:bg-surface-container-low lg:hidden" aria-label={mobile ? "Close menu" : "Open menu"} aria-expanded={mobile} onClick={() => setMobile((m) => !m)}>
          <Icon name={mobile ? "close" : "menu"} size={24} />
        </button>
      </Container>
      {mobile && (
        <div className="fixed inset-x-0 bottom-0 top-16 z-50 overflow-y-auto bg-background px-5 pb-10 pt-4 lg:hidden">
          <div className="grid gap-2">
            <ButtonLink to={signupHref("mobile-menu")} size="lg" onClick={() => track("start_trial_clicked", { placement: "mobile_menu" })}>Start Free Trial</ButtonLink>
            <ButtonLink to={LINKS.login} variant="secondary" size="lg" onClick={() => track("login_clicked")}>Log in</ButtonLink>
          </div>
          <MobileGroup title="Solutions" items={SOLUTIONS} />
          <MobileGroup title="Platform" items={PLATFORM} />
          <MobileGroup title="Resources" items={RESOURCES} />
          <MobileGroup title="More" items={[...(SHOW_PRICING ? [{ to: "/pricing", label: "Pricing" }] : []), { to: "/download", label: "Download Apps" }, { to: "/about", label: "About" }, { to: "/security", label: "Security & Trust" }, { to: "/book-a-demo", label: "Book a Demo" }]} />
        </div>
      )}
    </header>
  );
}

function MobileGroup({ title, items }: { title: string; items: NavLeaf[] }) {
  return (
    <div className="mt-6">
      <div className="mb-2 text-xs font-bold uppercase tracking-[0.14em] text-primary">{title}</div>
      <ul className="grid gap-1">
        {items.map((it) => (
          <li key={it.to}><Link to={it.to} className="flex items-center gap-3 rounded-[var(--radius-lg)] px-2 py-2.5 text-base font-medium text-on-surface hover:bg-surface-container-low">{it.icon && <Icon name={it.icon} size={22} className="text-primary" />}{it.label}</Link></li>
        ))}
      </ul>
    </div>
  );
}

export function Footer() {
  const col = (title: string, items: NavLeaf[]) => (
    <div>
      <div className="mb-3 text-sm font-bold text-on-surface">{title}</div>
      <ul className="space-y-2">
        {items.map((it) => <li key={it.to}><Link to={it.to} className="text-sm text-on-surface-variant hover:text-primary">{it.label}</Link></li>)}
      </ul>
    </div>
  );
  return (
    <footer className="border-t border-border bg-surface-container-low">
      <Container className="py-14">
        <div className="grid gap-10 md:grid-cols-12">
          <div className="md:col-span-4">
            <BuildingVisionLogo height={32} tone="brand" tagline={TAGLINE} />
            <p className="mt-4 max-w-xs text-sm text-on-surface-variant">One platform for hotel, apartment, and office operations. Built for the people who keep buildings running.</p>
            <div className="mt-5 flex flex-wrap gap-2">
              <ButtonLink to="/download" variant="secondary" size="sm" icon="download" trailing={false}>Download Apps</ButtonLink>
              <ButtonLink to={signupHref("footer")} size="sm" onClick={() => track("start_trial_clicked", { placement: "footer" })}>Start Free Trial</ButtonLink>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-8 sm:grid-cols-4 md:col-span-8">
            {col("Solutions", SOLUTIONS)}
            {col("Platform", PLATFORM)}
            {col("Resources", RESOURCES)}
            {col("Company", COMPANY)}
          </div>
        </div>
        <div className="mt-12 flex flex-col gap-3 border-t border-border pt-6 text-xs text-on-surface-variant sm:flex-row sm:items-center sm:justify-between">
          <span>© {new Date().getFullYear()} {SITE_NAME}. All rights reserved.</span>
          <div className="flex flex-wrap gap-4">
            {LEGAL.map((l) => <Link key={l.to} to={l.to} className="hover:text-primary">{l.label}</Link>)}
            <a href={LINKS.login} className="hover:text-primary">Log in</a>
          </div>
        </div>
      </Container>
    </footer>
  );
}

export function Layout() {
  const loc = useLocation();
  useEffect(() => {
    window.scrollTo({ top: 0 });
    track("page_viewed");
  }, [loc.pathname]);
  return (
    <div className="flex min-h-screen flex-col">
      <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-[60] focus:rounded-md focus:bg-surface focus:px-3 focus:py-2">Skip to content</a>
      <Header />
      <main id="main" className="flex-1">
        <Outlet />
      </main>
      <Footer />
    </div>
  );
}
