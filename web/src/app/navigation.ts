// Navigation Foundation (PRD P0 §21): arsitektur navigasi desktop yang permission- & capability-aware.
// ACCESS = sumber tunggal kode permission/capability untuk menu (AppShell) DAN route guard (App.tsx) — satu kode, dua tempat.
// Enforcement tetap server-side (TAD §9.2); ini hanya mencerminkan akses di UI.

export interface Access {
  /** Salah satu permission cukup (OR). */
  perm?: string | string[];
  /** Semua permission wajib (AND) — mis. dashboard domain = overview.dashboard.view + view domain (PRD P2 v2.1 §5.1). */
  permAll?: string[];
  /** Capability profile property (Onboarding Brief §12). */
  capability?: string;
  /** Hanya admin_internal (Website PRD §16). */
  internalOnly?: boolean;
  /** Hanya Platform Admin (PRD P0 v2 §6; /me/permissions is_platform_admin). */
  platformAdmin?: boolean;
}
export interface NavItem extends Access {
  to: string;
  label: string;
  icon?: string;
  /** Path lain yang juga mengaktifkan item ini (alias lama / halaman detail). */
  match?: RegExp[];
  /** Aktif hanya pada path persis (item induk yang punya sub-halaman sendiri). */
  exact?: boolean;
}
export interface NavGroup extends Access {
  key: string;
  label: string;
  icon: string;
  items?: NavItem[];
  to?: string;
}

export const ACCESS = {
  overview: { perm: "overview.dashboard.view" },
  tasks: { perm: "operations.tasks.view" },
  workOrders: { perm: "operations.work_orders.view" },
  serviceRequests: { perm: "tenant.service_requests.view" },
  incidents: { perm: "operations.incidents.view" },
  findings: { perm: "operations.findings.view" },
  preventiveMaintenance: { perm: "engineering.maintenance_plans.view" },
  inspections: { perm: "engineering.inspections.view" },
  assets: { perm: "engineering.assets.view" },
  equipment: { perm: "engineering.equipment.view" },
  patrol: { perm: "security.patrol.view" },
  visitors: { perm: "security.visitors.view", capability: "visitor_management" },
  // PRD P2 v2.1 §6.4/§6.6/§6.7: pelapor Emergency (raise) boleh membuka daftar — server hanya mengembalikan alert miliknya
  emergency: { perm: ["security.emergency_alerts.view", "security.emergency_alerts.raise"] },
  parking: { perm: "security.parking.view" },
  lostFound: { perm: "security.lost_found.view" },
  cleaning: { perm: "housekeeping.cleaning.view" },
  // PRD P2 v2.1 §5.1/§6.1/§7.1: dashboard domain — server mensyaratkan overview.dashboard.view + view domain
  engineeringDashboard: { permAll: ["overview.dashboard.view", "engineering.assets.view"] },
  securityDashboard: { permAll: ["overview.dashboard.view", "security.incidents.view"] },
  housekeepingDashboard: { permAll: ["overview.dashboard.view", "housekeeping.cleaning.view"] },
  // PRD P2 v2.1 §5.6 P2-DOC-02: dokumen equipment & warranty kedaluwarsa
  assetDocuments: { perm: "engineering.asset_documents.view" },
  // PRD P2 v2.1 §8.1 (D-P2-05): shift per domain — Security Shift Management & Housekeeping Shift
  securityShifts: { perm: "security.shifts.view" },
  housekeepingShifts: { perm: "housekeeping.shifts.view" },
  // PRD P2 v2.1 §7.3: Cleaning Route
  cleaningRoutes: { perm: "housekeeping.cleaning_routes.view" },
  cleaningSchedule: { perm: "housekeeping.cleaning_schedules.view" },
  housekeepingInspections: { perm: "housekeeping.inspections.view" },
  locations: { perm: "property.locations.view" },
  portfolios: { perm: "property.portfolios.view" },
  tenants: { perm: "property.tenants.view" },
  // PRD P1 v2 §6–§8: Building Management — facility operasional (property.* atau booking.*), denah, occupancy
  propertyFacilities: { perm: ["property.facilities.view", "booking.facilities.view"] },
  floorPlans: { perm: "property.floor_plans.view" },
  occupancy: { perm: "property.occupancy.view" },
  tenantRelation: { perm: "tenant.service_requests.view", capability: "tenant_relation" },
  tenantUsers: { perm: "tenant_relation.tenant_users.view", capability: "tenant_relation" },
  feedback: { perm: "tenant_relation.feedback.view", capability: "tenant_relation" },
  announcements: { perm: "tenant_relation.announcements.view", capability: "tenant_relation" },
  bookings: { perm: "booking.bookings.view", capability: "facility_booking" },
  facilities: { perm: "booking.facilities.view", capability: "facility_booking" },
  reception: { perm: "hotel.reservations.view", capability: "reception" },
  hotelReservations: { perm: "hotel.reservations.view", capability: "hotel_booking" },
  hotelRooms: { perm: "hotel.rooms.view", capability: "hotel_booking" },
  unitListings: { perm: "commercial.unit_listings.view", capability: "unit_sales" },
  salesLeads: { perm: "commercial.sales_leads.view", capability: "unit_sales" },
  saleReservations: { perm: "commercial.sale_reservations.view", capability: "unit_sales" },
  rentalListings: { perm: "commercial.rental_listings.view", capability: "unit_rental" },
  rentalReservations: { perm: "commercial.rental_reservations.view", capability: "unit_rental" },
  invoices: { perm: "billing.invoices.view", capability: "billing" },
  payments: { perm: "billing.payments.view", capability: "billing" },
  // PRD P4 v2.1 (Financial Operations; D-P4-08 permission per aksi) — NC §34 Billing: Invoices · Service Charges (billing run) ·
  // Utilities · Payments · Receivables; + Finance (budget vs actual, biaya, ekspor akuntansi)
  financeDashboard: { perm: "billing.invoices.view", capability: "billing" },
  billingRuns: { perm: "billing.runs.view", capability: "billing" },
  billingRules: { perm: "billing.rules.view", capability: "billing" },
  utilities: { perm: ["billing.meters.view", "billing.meter_readings.view"], capability: "billing" },
  receivables: { perm: ["billing.collections.view", "billing.invoices.view"], capability: "billing" },
  reconciliation: { perm: "billing.reconciliation.view", capability: "billing" },
  creditNotes: { perm: "billing.credit_notes.view", capability: "billing" },
  penalties: { perm: "billing.penalties.view", capability: "billing" },
  sinkingFund: { perm: "billing.sinking_fund.view", capability: "billing" },
  deposits: { perm: "billing.deposits.view", capability: "billing" },
  billingSettings: { perm: "billing.settings.view", capability: "billing" },
  budgets: { perm: "billing.budgets.view", capability: "billing" },
  costs: { perm: "billing.costs.view", capability: "billing" },
  accounting: { perm: "billing.accounting.view", capability: "billing" },
  // PRD P3 v2.1 (Tenant Experience): paket, isu berulang (izin parkir = tab Parking)
  packages: { perm: "security.packages.view" },
  recurringIssues: { perm: "tenant_relation.recurring_issues.view", capability: "tenant_relation" },
  vendors: { perm: "vendor.vendors.view", capability: "vendor_management" },
  inventory: { perm: "inventory.items.view", capability: "inventory" },
  reports: { perm: "reports.reports.view", capability: "reports" },
  designSystem: {},
  profile: {},
  exports: { perm: "platform.exports.create" },
  broadcast: { perm: "platform.notifications.broadcast" },
  platformOrganizations: { platformAdmin: true }, // server tetap mensyaratkan platform.org_registry.*
} satisfies Record<string, Access>;

// Urutan grup = PRD P0 §21: Overview · Operations · Engineering · Security · Housekeeping · Property · Tenant · Finance ·
// Vendor · Inventory · Reports · Settings. Grup khusus profile (Reception, Commercial) bersifat aditif (roadmap §25.9)
// dan ditempatkan setelah Tenant. URL tidak berubah; alias lama tetap mengaktifkan item yang sama lewat `match`.
export const NAV: NavGroup[] = [
  { key: "overview", label: "nav.overview", icon: "space_dashboard", to: "/overview", ...ACCESS.overview },
  { key: "operations", label: "nav.operations", icon: "assignment", items: [
    { to: "/operations/tasks", label: "nav.tasks", icon: "task_alt", match: [/^\/tasks\//], ...ACCESS.tasks },
    { to: "/operations/work-orders", label: "nav.work_orders", icon: "construction", match: [/^\/work-orders\//], ...ACCESS.workOrders },
    { to: "/operations/service-requests", label: "nav.service_requests", icon: "support_agent", match: [/^\/service-requests\//], ...ACCESS.serviceRequests },
    { to: "/operations/incidents", label: "nav.incidents", icon: "emergency_home", ...ACCESS.incidents },
    { to: "/findings", label: "nav.findings", icon: "report_problem", ...ACCESS.findings },
  ] },
  { key: "engineering", label: "nav.engineering", icon: "build", items: [
    { to: "/engineering", label: "nav.dashboard", icon: "space_dashboard", exact: true, ...ACCESS.engineeringDashboard },
    { to: "/engineering/preventive-maintenance", label: "nav.preventive_maintenance", icon: "event_repeat", ...ACCESS.preventiveMaintenance },
    { to: "/engineering/corrective-maintenance", label: "nav.corrective_maintenance", icon: "handyman", ...ACCESS.workOrders },
    { to: "/engineering/inspections", label: "nav.inspections", icon: "fact_check", ...ACCESS.inspections },
    { to: "/engineering/assets", label: "nav.assets", icon: "precision_manufacturing", match: [/^\/assets(\/(?!history$|equipment$)[^/]+)?$/], ...ACCESS.assets },
    { to: "/engineering/equipment", label: "nav.equipment", icon: "settings_input_component", match: [/^\/assets\/equipment$/], ...ACCESS.equipment },
    { to: "/assets/history", label: "nav.asset_history", icon: "history", ...ACCESS.assets },
    { to: "/engineering/documents", label: "nav.asset_documents", icon: "description", ...ACCESS.assetDocuments },
  ] },
  // urutan submodule Security = NC §15 (Patrol · Incidents · Visitors · Parking · Emergency · … · Lost & Found · Shift Management);
  // Dashboard domain lebih dulu (PRD P2 v2.1 §6.1)
  { key: "security", label: "nav.security", icon: "verified_user", items: [
    { to: "/security", label: "nav.dashboard", icon: "space_dashboard", exact: true, ...ACCESS.securityDashboard },
    { to: "/security/patrol", label: "nav.patrol", icon: "directions_walk", ...ACCESS.patrol },
    { to: "/security/incidents", label: "nav.incidents", icon: "emergency_home", ...ACCESS.incidents },
    { to: "/security/visitors", label: "nav.visitors", icon: "badge", ...ACCESS.visitors },
    { to: "/security/parking", label: "nav.parking", icon: "local_parking", ...ACCESS.parking },
    { to: "/security/emergency", label: "nav.emergency", icon: "e911_emergency", ...ACCESS.emergency },
    { to: "/security/lost-found", label: "nav.lost_found", icon: "inventory_2", ...ACCESS.lostFound },
    { to: "/security/packages", label: "nav.packages", icon: "package_2", ...ACCESS.packages },
    { to: "/security/shifts", label: "nav.security_shifts", icon: "schedule", ...ACCESS.securityShifts },
  ] },
  // NC §17: Cleaning · Schedule · Inspection · … · Shift; Cleaning Route (P2 v2.1 §7.3) setelah Schedule
  { key: "housekeeping", label: "nav.housekeeping", icon: "cleaning_services", items: [
    { to: "/housekeeping", label: "nav.dashboard", icon: "space_dashboard", exact: true, ...ACCESS.housekeepingDashboard },
    { to: "/housekeeping/cleaning", label: "nav.cleaning", icon: "mop", ...ACCESS.cleaning },
    { to: "/housekeeping/schedule", label: "nav.schedule", icon: "calendar_month", ...ACCESS.cleaningSchedule },
    { to: "/housekeeping/routes", label: "nav.cleaning_routes", icon: "route", ...ACCESS.cleaningRoutes },
    { to: "/housekeeping/inspections", label: "nav.inspections", icon: "fact_check", ...ACCESS.housekeepingInspections },
    { to: "/housekeeping/shifts", label: "nav.housekeeping_shifts", icon: "schedule", ...ACCESS.housekeepingShifts },
  ] },
  { key: "property", label: "nav.property", icon: "apartment", items: [
    { to: "/property/portfolios", label: "nav.portfolios", icon: "hub", ...ACCESS.portfolios },
    { to: "/property/properties", label: "nav.properties", icon: "domain", ...ACCESS.locations },
    { to: "/property/buildings", label: "nav.buildings", icon: "location_city", ...ACCESS.locations },
    { to: "/property/towers", label: "nav.towers", icon: "corporate_fare", ...ACCESS.locations },
    { to: "/property/floors", label: "nav.floors", icon: "layers", ...ACCESS.locations },
    { to: "/property/areas", label: "nav.areas", icon: "grid_view", ...ACCESS.locations },
    { to: "/property/spaces", label: "nav.spaces", icon: "meeting_room", ...ACCESS.locations },
    { to: "/property/units", label: "nav.units", icon: "door_front", ...ACCESS.locations },
    { to: "/property/facilities", label: "nav.facilities", icon: "meeting_room", ...ACCESS.propertyFacilities },
    { to: "/property/floor-plans", label: "nav.floor_plans", icon: "location_on", ...ACCESS.floorPlans },
    { to: "/property/occupancy", label: "nav.occupancy", icon: "apartment", ...ACCESS.occupancy },
  ] },
  // Tenant = Tenant Relation (modul operasional mandatory, PRD v1.3 §3.2) + data Tenant + Facility Booking
  { key: "tenant", label: "nav.tenant", icon: "group", items: [
    { to: "/tenant-relation", label: "nav.tenant_relation", icon: "space_dashboard", exact: true, ...ACCESS.tenantRelation },
    { to: "/tenant/tenants", label: "nav.tenants", icon: "badge", ...ACCESS.tenants },
    { to: "/tenant-relation/service-requests", label: "nav.service_requests", icon: "support_agent", match: [/^\/tenant\/service-requests/], ...ACCESS.serviceRequests },
    { to: "/tenant-relation/tenant-users", label: "nav.tenant_users", icon: "how_to_reg", ...ACCESS.tenantUsers },
    { to: "/tenant-relation/feedback", label: "nav.feedback", icon: "reviews", ...ACCESS.feedback },
    { to: "/tenant-relation/recurring-issues", label: "nav.recurring_issues", icon: "repeat", ...ACCESS.recurringIssues },
    { to: "/tenant-relation/announcements", label: "nav.announcements", icon: "campaign", ...ACCESS.announcements },
    { to: "/booking/bookings", label: "nav.bookings", icon: "event_available", ...ACCESS.bookings },
    { to: "/booking/facilities", label: "nav.facilities", icon: "meeting_room", ...ACCESS.facilities },
  ] },
  { key: "reception", label: "nav.reception", icon: "room_service", to: "/reception", ...ACCESS.reception },
  { key: "commercial", label: "nav.commercial", icon: "storefront", items: [
    { to: "/commercial/hotel/reservations", label: "nav.hotel_reservations", icon: "hotel", ...ACCESS.hotelReservations },
    { to: "/commercial/hotel/calendar", label: "nav.hotel_calendar", icon: "calendar_month", ...ACCESS.hotelReservations },
    { to: "/commercial/hotel/rooms", label: "nav.hotel_rooms", icon: "bed", ...ACCESS.hotelRooms },
    { to: "/commercial/sales/listings", label: "nav.unit_listings", icon: "real_estate_agent", ...ACCESS.unitListings },
    { to: "/commercial/sales/leads", label: "nav.sales_leads", icon: "group", ...ACCESS.salesLeads },
    { to: "/commercial/sales/reservations", label: "nav.sale_reservations", icon: "verified", ...ACCESS.saleReservations },
    { to: "/commercial/rental/listings", label: "nav.rental_listings", icon: "key", ...ACCESS.rentalListings },
    { to: "/commercial/rental/reservations", label: "nav.rental_reservations", icon: "event_available", ...ACCESS.rentalReservations },
    { to: "/commercial/rental/calendar", label: "nav.rental_calendar", icon: "calendar_month", ...ACCESS.rentalReservations },
  ] },
  { key: "finance", label: "nav.finance", icon: "receipt_long", capability: "billing", items: [
    { to: "/finance", label: "nav.dashboard", icon: "space_dashboard", exact: true, ...ACCESS.financeDashboard },
    { to: "/billing/invoices", label: "nav.invoices", icon: "request_quote", ...ACCESS.invoices },
    { to: "/billing/runs", label: "nav.billing_runs", icon: "event_repeat", ...ACCESS.billingRuns },
    { to: "/billing/meters", label: "nav.utilities", icon: "electric_meter", ...ACCESS.utilities },
    { to: "/billing/payments", label: "nav.payments", icon: "payments", ...ACCESS.payments },
    { to: "/billing/reconciliation", label: "nav.reconciliation", icon: "account_balance", ...ACCESS.reconciliation },
    { to: "/billing/receivables", label: "nav.receivables", icon: "pending_actions", match: [/^\/billing\/(aging|collections|statement)(\/|$)/], ...ACCESS.receivables },
    { to: "/billing/credit-notes", label: "nav.credit_notes", icon: "receipt", ...ACCESS.creditNotes },
    { to: "/billing/penalties", label: "nav.penalties", icon: "gavel", ...ACCESS.penalties },
    { to: "/billing/sinking-fund", label: "nav.sinking_fund", icon: "savings", ...ACCESS.sinkingFund },
    { to: "/billing/deposits", label: "nav.deposits", icon: "account_balance_wallet", ...ACCESS.deposits },
    { to: "/finance/budgets", label: "nav.budgets", icon: "request_page", ...ACCESS.budgets },
    { to: "/finance/budget-actual", label: "nav.budget_actual", icon: "compare_arrows", ...ACCESS.budgets },
    { to: "/finance/costs", label: "nav.operating_costs", icon: "price_check", ...ACCESS.costs },
    { to: "/finance/accounting", label: "nav.accounting", icon: "sync_alt", ...ACCESS.accounting },
    { to: "/billing/rules", label: "nav.billing_rules", icon: "rule", ...ACCESS.billingRules },
    { to: "/billing/settings", label: "nav.billing_settings", icon: "tune", ...ACCESS.billingSettings },
  ] },
  { key: "vendor", label: "nav.vendor", icon: "handshake", to: "/vendors", ...ACCESS.vendors },
  { key: "inventory", label: "nav.inventory", icon: "inventory", to: "/inventory", ...ACCESS.inventory },
  { key: "reports", label: "nav.reports", icon: "analytics", to: "/reports", ...ACCESS.reports },
  { key: "settings", label: "nav.settings", icon: "settings", items: [
    { to: "/settings/profile", label: "nav.my_profile", icon: "person", ...ACCESS.profile },
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
    { to: "/settings/broadcast", label: "nav.broadcast", icon: "campaign", ...ACCESS.broadcast },
    { to: "/settings/exports", label: "nav.exports", icon: "download", ...ACCESS.exports },
    { to: "/settings/sync-conflicts", label: "nav.sync_conflicts", icon: "merge_type", perm: "sync.conflicts.view" },
    { to: "/settings/audit-logs", label: "nav.audit", icon: "receipt_long", perm: "platform.audit_logs.view" },
    { to: "/settings/app-downloads", label: "nav.app_downloads", icon: "download", perm: "platform.app_downloads.view", internalOnly: true }, // Website PRD §16: admin_internal
    { to: "/settings/demo-data", label: "nav.demo_data", icon: "database", perm: "platform.demo_data.view", internalOnly: true }, // Demo Seed §29: admin_internal
    { to: "/settings/platform-organizations", label: "nav.platform_organizations", icon: "domain", ...ACCESS.platformOrganizations }, // PRD P0 v2 §6: Platform Admin
    { to: "/settings/design-system", label: "nav.design_system", icon: "palette", ...ACCESS.designSystem }, // PRD P0 §20: katalog komponen, semua user
  ] },
];

export interface AccessContext {
  can: (perm: string) => boolean;
  hasCapability: (cap: string) => boolean;
  isInternalAdmin: boolean;
  isPlatformAdmin?: boolean;
}

export function hasPerm(can: (p: string) => boolean, perm?: string | string[]): boolean {
  if (!perm || (Array.isArray(perm) && perm.length === 0)) return true;
  return (Array.isArray(perm) ? perm : [perm]).some((p) => can(p));
}

/** Hasil pemeriksaan akses; `capability` dibedakan agar 403 bisa menjelaskan "modul tidak aktif". */
export function checkAccess(a: Access | undefined, ctx: AccessContext): "allowed" | "permission" | "capability" | "internal" {
  if (!a) return "allowed";
  if (a.internalOnly && !ctx.isInternalAdmin) return "internal";
  if (a.platformAdmin && !ctx.isPlatformAdmin) return "internal";
  if (a.capability && !ctx.hasCapability(a.capability)) return "capability";
  if (!hasPerm(ctx.can, a.perm)) return "permission";
  if (a.permAll && !a.permAll.every((p) => ctx.can(p))) return "permission";
  return "allowed";
}

/** Menu yang terlihat: item disaring permission + capability + internalOnly; grup kosong disembunyikan. */
export function visibleNav(nav: NavGroup[], ctx: AccessContext): NavGroup[] {
  return nav
    .filter((g) => !g.capability || ctx.hasCapability(g.capability))
    .map((g) => ({ ...g, items: g.items?.filter((i) => checkAccess(i, ctx) === "allowed") }))
    .filter((g) => (g.to ? checkAccess(g, ctx) === "allowed" : (g.items?.length ?? 0) > 0));
}

export function isItemActive(i: NavItem, pathname: string): boolean {
  if (pathname === i.to) return true;
  if (!i.exact && pathname.startsWith(i.to + "/")) return true;
  return !!i.match?.some((re) => re.test(pathname));
}
export function isGroupActive(g: NavGroup, pathname: string): boolean {
  return g.to ? pathname === g.to || pathname.startsWith(g.to + "/") : !!g.items?.some((i) => isItemActive(i, pathname));
}

/** Tujuan pertama yang boleh dibuka (redirect "/" untuk user tanpa akses Overview). */
export function firstDestination(nav: NavGroup[]): string {
  const g = nav[0];
  return g ? (g.to ?? g.items?.[0]?.to ?? "/settings/design-system") : "/settings/design-system";
}
