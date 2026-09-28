// Route guard (PRD P0 §21, §24.1): setiap route fitur mendeklarasikan Access (kode yang sama dengan menu).
// Permission gagal → halaman 403; capability belum termuat → skeleton; capability tidak aktif → 403 dengan penjelasan.
import { Navigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { DetailSkeleton } from "@/components/bv/states";
import { ForbiddenPage } from "@/components/shell/SystemPages";
import { NAV, checkAccess, firstDestination, visibleNav, type Access } from "./navigation";
import { useAccessContext } from "./access";

export function RouteGuard({ access, children }: { access?: Access; children: React.ReactNode }) {
  const { t } = useTranslation();
  const ctx = useAccessContext();
  const result = checkAccess(access, ctx);
  if (result === "allowed") return <>{children}</>;
  if (result === "capability") {
    if (ctx.capabilityLoading) return <DetailSkeleton />;
    return <ForbiddenPage detail={t("state.forbidden_capability")} />;
  }
  return <ForbiddenPage />;
}

/** "/" → tujuan pertama yang boleh dibuka user (Overview bila punya akses). */
export function HomeRedirect() {
  const ctx = useAccessContext();
  return <Navigate to={firstDestination(visibleNav(NAV, ctx))} replace />;
}

/**
 * Beranda modul domain (`/engineering`, `/security`, `/housekeeping`; PRD P2 v2.1 §5.1/§6.1/§7.1): Dashboard domain bila user
 * boleh membukanya (overview.dashboard.view + view domain), selain itu item menu pertama grup yang terlihat (perilaku lama).
 */
export function DomainHome({ domain, fallback, children }: { domain: "engineering" | "security" | "housekeeping"; fallback: string; children: React.ReactNode }) {
  const ctx = useAccessContext();
  const home = `/${domain}`;
  const dashboard = NAV.find((g) => g.key === domain)?.items?.find((i) => i.to === home);
  if (dashboard && checkAccess(dashboard, ctx) === "allowed") return <>{children}</>;
  const first = visibleNav(NAV, ctx).find((g) => g.key === domain)?.items?.find((i) => i.to !== home)?.to;
  return <Navigate to={first ?? fallback} replace />;
}
