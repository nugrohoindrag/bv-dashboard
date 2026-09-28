// Router (React Router v7 library mode) — route sesuai Naming Convention §48 & IA PRD §26; auth guard; code splitting per modul.
// PRD P0 §21/§23: setiap route fitur mendeklarasikan Access (kode = menu) → 403; ErrorBoundary app & per route; 404 khusus.
import { lazy, Suspense } from "react";
import { BrowserRouter, Navigate, Outlet, Route, Routes, useLocation, useParams } from "react-router-dom";
import { AppShell } from "@/components/shell/AppShell";
import { DetailSkeleton } from "@/components/bv/states";
import { ErrorBoundary } from "@/components/bv/ErrorBoundary";
import { NotFoundPage } from "@/components/shell/SystemPages";
import { ACCESS, type Access } from "./navigation";
import { DomainHome, HomeRedirect, RouteGuard } from "./guards";
import { useAuth } from "@/lib/auth";
import { LoginPage } from "@/features/auth/LoginPage";
import { SignupPage } from "@/features/growth/SignupPage";
import { VerifyEmailPage } from "@/features/growth/VerifyEmailPage";
import { AcceptInvitePage } from "@/features/auth/AcceptInvitePage";

const Overview = lazy(() => import("@/features/overview/OverviewPage"));
const WorkItemList = lazy(() => import("@/features/operations/WorkItemListPage"));
const WorkItemDetail = lazy(() => import("@/features/operations/WorkItemDetailPage"));
const ServiceRequestList = lazy(() => import("@/features/operations/ServiceRequestListPage"));
const ServiceRequestDetail = lazy(() => import("@/features/operations/ServiceRequestDetailPage"));
const IncidentList = lazy(() => import("@/features/operations/IncidentListPage"));
const IncidentDetail = lazy(() => import("@/features/operations/IncidentDetailPage"));
const FindingList = lazy(() => import("@/features/operations/FindingListPage"));
const FindingDetail = lazy(() => import("@/features/operations/FindingDetailPage"));
const PreventiveMaintenance = lazy(() => import("@/features/engineering/PreventiveMaintenancePage"));
const AssetList = lazy(() => import("@/features/assets/AssetListPage"));
const AssetDetail = lazy(() => import("@/features/assets/AssetDetailPage"));
const EquipmentPage = lazy(() => import("@/features/assets/EquipmentPage"));
const AssetHistory = lazy(() => import("@/features/assets/AssetHistoryPage"));
const Patrol = lazy(() => import("@/features/security/PatrolPage"));
const HousekeepingSchedule = lazy(() => import("@/features/housekeeping/CleaningSchedulePage"));
const PropertyPage = lazy(() => import("@/features/property/LocationsPage"));
const LocationDetail = lazy(() => import("@/features/property/LocationDetailPage"));
const Portfolios = lazy(() => import("@/features/property/PortfoliosPage"));
const FloorPlans = lazy(() => import("@/features/property/FloorPlans"));
const Occupancy = lazy(() => import("@/features/property/OccupancyPage"));
const TenantList = lazy(() => import("@/features/tenant/TenantListPage"));
const Settings = lazy(() => import("@/features/settings/SettingsPage"));
const TenantRelationDashboard = lazy(() => import("@/features/tenant-relation/TenantRelationDashboardPage"));
const TenantUsers = lazy(() => import("@/features/tenant-relation/TenantUsersPage"));
const TenantFeedback = lazy(() => import("@/features/tenant-relation/FeedbackPage"));
const Announcements = lazy(() => import("@/features/tenant-relation/AnnouncementsPage"));
const Facilities = lazy(() => import("@/features/booking/FacilitiesPage"));
const Bookings = lazy(() => import("@/features/booking/BookingsPage"));
const Visitors = lazy(() => import("@/features/security/VisitorsPage"));
const Emergency = lazy(() => import("@/features/security/EmergencyPage"));
const EmergencyDetail = lazy(() => import("@/features/security/EmergencyDetailPage"));
const Parking = lazy(() => import("@/features/security/ParkingPage"));
const LostFound = lazy(() => import("@/features/security/LostFoundPage"));
const LostFoundDetail = lazy(() => import("@/features/security/LostFoundDetailPage"));
const Invoices = lazy(() => import("@/features/billing/InvoicesPage"));
const Payments = lazy(() => import("@/features/billing/PaymentsPage"));
// PRD P4 v2.1 — Financial Operations
const BillingRuns = lazy(() => import("@/features/billing/BillingRunsPage"));
const BillingRules = lazy(() => import("@/features/billing/BillingRulesPage"));
const Utilities = lazy(() => import("@/features/billing/UtilitiesPage"));
const Reconciliation = lazy(() => import("@/features/billing/ReconciliationPage"));
const Receivables = lazy(() => import("@/features/billing/ReceivablesPage"));
const CreditNotes = lazy(() => import("@/features/billing/CreditNotesPage"));
const Penalties = lazy(() => import("@/features/billing/PenaltiesPage"));
const SinkingFund = lazy(() => import("@/features/billing/SinkingFundPage"));
const Deposits = lazy(() => import("@/features/billing/DepositsPage"));
const BillingSettings = lazy(() => import("@/features/billing/BillingSettingsPage"));
const Budgets = lazy(() => import("@/features/finance/BudgetsPage"));
const BudgetActual = lazy(() => import("@/features/finance/BudgetActualPage"));
const Costs = lazy(() => import("@/features/finance/CostsPage"));
const Accounting = lazy(() => import("@/features/finance/AccountingPage"));
// PRD P3 v2.1 — Tenant Experience
const Packages = lazy(() => import("@/features/security/PackagesPage"));
const RecurringIssues = lazy(() => import("@/features/tenant-relation/RecurringIssuesPage"));
const Vendors = lazy(() => import("@/features/vendor/VendorsPage"));
const Inventory = lazy(() => import("@/features/inventory/InventoryPage"));
const HotelReservations = lazy(() => import("@/features/hotel/HotelReservationsPage"));
const HotelRooms = lazy(() => import("@/features/hotel/HotelRoomsPage"));
const HotelCalendar = lazy(() => import("@/features/hotel/HotelCalendarPage"));
const Reception = lazy(() => import("@/features/hotel/ReceptionPage"));
const UnitListings = lazy(() => import("@/features/commercial/UnitListingsPage"));
const SalesLeads = lazy(() => import("@/features/commercial/SalesLeadsPage"));
const SaleReservations = lazy(() => import("@/features/commercial/SaleReservationsPage"));
const RentalListings = lazy(() => import("@/features/commercial/RentalListingsPage"));
const RentalReservations = lazy(() => import("@/features/commercial/RentalReservationsPage"));
const RentalCalendar = lazy(() => import("@/features/commercial/RentalCalendarPage"));
const CommercialIndex = lazy(() => import("@/features/commercial/CommercialIndex"));
const Reports = lazy(() => import("@/features/reports/ReportsPage"));
const OnboardingPage = lazy(() => import("@/features/growth/OnboardingPage"));
const DesignSystemPage = lazy(() => import("@/features/settings/DesignSystemPage"));
// PRD P2 v2.1 (Workforce Operations): dashboard domain, shift per domain, kompetensi, Cleaning Route, dokumen equipment
const DomainDashboard = lazy(() => import("@/features/dashboards/DomainDashboardPage"));
const Shifts = lazy(() => import("@/features/workforce/ShiftsPage"));
const CleaningRoutes = lazy(() => import("@/features/housekeeping/CleaningRoutesPage"));
const ExpiringDocuments = lazy(() => import("@/features/assets/ExpiringDocumentsPage"));

function Protected() {
  const { principal, loading } = useAuth();
  const loc = useLocation();
  if (loading) return <div className="p-10"><DetailSkeleton /></div>;
  if (!principal) return <Navigate to="/login" state={{ from: loc.pathname }} replace />;
  return <Outlet />;
}

/** Drill-down Security Dashboard per rute patrol → tab Rute Patrol dengan rute terbuka. */
function PatrolRouteRedirect() {
  const { id } = useParams();
  return <Navigate to={`/security/patrol/routes${id ? `?route=${id}` : ""}`} replace />;
}

/** Route fitur: guard akses → ErrorBoundary (reset saat pindah path) → Suspense code splitting. */
function Page({ access, children }: { access?: Access; children: React.ReactNode }) {
  const loc = useLocation();
  return (
    <RouteGuard access={access}>
      <ErrorBoundary resetKey={loc.pathname} inline>
        <Suspense fallback={<DetailSkeleton />}>{children}</Suspense>
      </ErrorBoundary>
    </RouteGuard>
  );
}

export function App() {
  return (
    <BrowserRouter>
      <ErrorBoundary>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        {/* Self-serve free trial (Website PRD §24–§25): Start Free Trial → Create Account → Verify Email */}
        <Route path="/signup" element={<SignupPage />} />
        <Route path="/verify-email" element={<VerifyEmailPage />} />
        {/* Undangan user (PRD P0 v2 §26.1): publik, tanpa login */}
        <Route path="/accept-invite" element={<AcceptInvitePage />} />
        <Route element={<Protected />}>
          <Route element={<AppShell />}>
            <Route index element={<HomeRedirect />} />
            <Route path="/overview" element={<Page access={ACCESS.overview}><Overview /></Page>} />
            <Route path="/overview-2" element={<Navigate to="/overview" replace />} />
            {/* Onboarding wizard + checklist (Website PRD §26–§30) */}
            <Route path="/onboarding" element={<Page><OnboardingPage /></Page>} />
            {/* Operations */}
            <Route path="/operations" element={<Navigate to="/operations/work-orders" replace />} />
            <Route path="/operations/tasks" element={<Page access={ACCESS.tasks}><WorkItemList objectType="task" /></Page>} />
            <Route path="/operations/tasks/:id" element={<Page access={ACCESS.tasks}><WorkItemDetail objectType="task" /></Page>} />
            <Route path="/operations/work-orders" element={<Page access={ACCESS.workOrders}><WorkItemList objectType="work_order" /></Page>} />
            <Route path="/operations/work-orders/:id" element={<Page access={ACCESS.workOrders}><WorkItemDetail objectType="work_order" /></Page>} />
            <Route path="/work-orders/:id" element={<Page access={ACCESS.workOrders}><WorkItemDetail objectType="work_order" /></Page>} />
            <Route path="/tasks/:id" element={<Page access={ACCESS.tasks}><WorkItemDetail objectType="task" /></Page>} />
            <Route path="/operations/service-requests" element={<Page access={ACCESS.serviceRequests}><ServiceRequestList /></Page>} />
            <Route path="/operations/service-requests/:id" element={<Page access={ACCESS.serviceRequests}><ServiceRequestDetail /></Page>} />
            <Route path="/service-requests/:id" element={<Page access={ACCESS.serviceRequests}><ServiceRequestDetail /></Page>} />
            <Route path="/operations/incidents" element={<Page access={ACCESS.incidents}><IncidentList /></Page>} />
            <Route path="/operations/incidents/:id" element={<Page access={ACCESS.incidents}><IncidentDetail /></Page>} />
            <Route path="/findings" element={<Page access={ACCESS.findings}><FindingList /></Page>} />
            <Route path="/findings/:id" element={<Page access={ACCESS.findings}><FindingDetail /></Page>} />
            {/* Engineering */}
            {/* PRD P2 v2.1 §5.1/§6.1/§7.1: /engineering · /security · /housekeeping = Dashboard domain bila boleh, selain itu menu pertama */}
            <Route path="/engineering" element={<DomainHome domain="engineering" fallback="/engineering/preventive-maintenance"><Page access={ACCESS.engineeringDashboard}><DomainDashboard domain="engineering" /></Page></DomainHome>} />
            <Route path="/engineering/documents" element={<Page access={ACCESS.assetDocuments}><ExpiringDocuments /></Page>} />
            <Route path="/engineering/preventive-maintenance" element={<Page access={ACCESS.preventiveMaintenance}><PreventiveMaintenance /></Page>} />
            <Route path="/engineering/preventive-maintenance/:scheduleId" element={<Page access={ACCESS.preventiveMaintenance}><PreventiveMaintenance /></Page>} />
            <Route path="/engineering/corrective-maintenance" element={<Page access={ACCESS.workOrders}><WorkItemList objectType="work_order" fixedType="corrective,repair" title="Corrective Maintenance" /></Page>} />
            <Route path="/engineering/inspections" element={<Page access={ACCESS.inspections}><WorkItemList objectType="task" fixedType="inspection" title="Inspections" /></Page>} />
            <Route path="/engineering/assets" element={<Page access={ACCESS.assets}><AssetList /></Page>} />
            <Route path="/engineering/equipment" element={<Page access={ACCESS.equipment}><EquipmentPage /></Page>} />
            {/* Security */}
            <Route path="/security" element={<DomainHome domain="security" fallback="/security/patrol"><Page access={ACCESS.securityDashboard}><DomainDashboard domain="security" /></Page></DomainHome>} />
            {/* D-P2-05: shift per domain — Security Shift Management; deep link ?tab=handovers&id= / ?tab=on-duty */}
            <Route path="/security/shifts" element={<Page access={ACCESS.securityShifts}><Shifts domain="security" /></Page>} />
            <Route path="/security/patrol-routes/:id" element={<PatrolRouteRedirect />} />
            <Route path="/security/patrol" element={<Page access={ACCESS.patrol}><Patrol /></Page>} />
            <Route path="/security/patrol/:tab" element={<Page access={ACCESS.patrol}><Patrol /></Page>} />
            <Route path="/security/incidents" element={<Page access={ACCESS.incidents}><IncidentList security /></Page>} />
            <Route path="/security/visitors" element={<Page access={ACCESS.visitors}><Visitors /></Page>} />
            <Route path="/security/visitors/:id" element={<Page access={ACCESS.visitors}><Visitors /></Page>} />
            {/* PRD P2 v2.1: Emergency (§6.4), Parking (§6.6), Lost & Found (§6.7) — segmen statis mengalahkan :id; deep link
                notifikasi: /security/emergency/{id}, /security/parking/violations/{id}, /security/lost-found/{id} */}
            <Route path="/security/emergency" element={<Page access={ACCESS.emergency}><Emergency /></Page>} />
            <Route path="/security/emergency/contacts" element={<Page access={ACCESS.emergency}><Emergency tab="contacts" /></Page>} />
            <Route path="/security/emergency/:id" element={<Page access={ACCESS.emergency}><EmergencyDetail /></Page>} />
            <Route path="/security/parking" element={<Page access={ACCESS.parking}><Parking /></Page>} />
            <Route path="/security/parking/:tab" element={<Page access={ACCESS.parking}><Parking /></Page>} />
            <Route path="/security/parking/violations/:id" element={<Page access={ACCESS.parking}><Parking /></Page>} />
            {/* PRD P3 v2.1: Package (deep link /security/packages/{id}), izin parkir tenant = tab Parking (/security/parking/permits/{id}) */}
            <Route path="/security/packages" element={<Page access={ACCESS.packages}><Packages /></Page>} />
            <Route path="/security/packages/:id" element={<Page access={ACCESS.packages}><Packages /></Page>} />
            <Route path="/security/parking/permits/:id" element={<Page access={ACCESS.parking}><Parking /></Page>} />
            <Route path="/security/lost-found" element={<Page access={ACCESS.lostFound}><LostFound /></Page>} />
            <Route path="/security/lost-found/reports" element={<Page access={ACCESS.lostFound}><LostFound tab="reports" /></Page>} />
            <Route path="/security/lost-found/:id" element={<Page access={ACCESS.lostFound}><LostFoundDetail /></Page>} />
            {/* Booking (generic Booking = Facility Booking; PRD §21) */}
            <Route path="/booking" element={<Navigate to="/booking/bookings" replace />} />
            <Route path="/booking/facilities" element={<Page access={ACCESS.facilities}><Facilities /></Page>} />
            <Route path="/booking/bookings" element={<Page access={ACCESS.bookings}><Bookings /></Page>} />
            <Route path="/booking/bookings/:id" element={<Page access={ACCESS.bookings}><Bookings /></Page>} />
            {/* Billing (PRD §23; NC §34) */}
            <Route path="/billing" element={<Navigate to="/billing/invoices" replace />} />
            <Route path="/billing/invoices" element={<Page access={ACCESS.invoices}><Invoices /></Page>} />
            <Route path="/billing/invoices/:id" element={<Page access={ACCESS.invoices}><Invoices /></Page>} />
            <Route path="/billing/payments" element={<Page access={ACCESS.payments}><Payments /></Page>} />
            <Route path="/billing/payments/:id" element={<Page access={ACCESS.payments}><Payments /></Page>} />
            {/* PRD P4 v2.1 — Financial Operations (NC §34; deep link notifikasi: /billing/runs/{id}, /billing/credit-notes/{id},
                /billing/meters/readings/{id}, /billing/reconciliation/{id}, /billing/collections?log={id}, /finance/budgets/{id}) */}
            <Route path="/finance" element={<Page access={ACCESS.financeDashboard}><DomainDashboard domain="finance" /></Page>} />
            <Route path="/billing/runs" element={<Page access={ACCESS.billingRuns}><BillingRuns /></Page>} />
            <Route path="/billing/runs/:id" element={<Page access={ACCESS.billingRuns}><BillingRuns /></Page>} />
            <Route path="/billing/rules" element={<Page access={ACCESS.billingRules}><BillingRules /></Page>} />
            <Route path="/billing/rules/:id" element={<Page access={ACCESS.billingRules}><BillingRules /></Page>} />
            <Route path="/billing/meters" element={<Page access={ACCESS.utilities}><Utilities /></Page>} />
            <Route path="/billing/meters/readings/:id" element={<Page access={ACCESS.utilities}><Utilities tab="readings" /></Page>} />
            <Route path="/billing/meters/:tab" element={<Page access={ACCESS.utilities}><Utilities /></Page>} />
            <Route path="/billing/reconciliation" element={<Page access={ACCESS.reconciliation}><Reconciliation /></Page>} />
            <Route path="/billing/reconciliation/:id" element={<Page access={ACCESS.reconciliation}><Reconciliation /></Page>} />
            <Route path="/billing/receivables" element={<Page access={ACCESS.receivables}><Receivables /></Page>} />
            <Route path="/billing/aging" element={<Page access={ACCESS.receivables}><Receivables tab="aging" /></Page>} />
            <Route path="/billing/collections" element={<Page access={ACCESS.receivables}><Receivables tab="collections" /></Page>} />
            <Route path="/billing/statement" element={<Page access={ACCESS.receivables}><Receivables tab="statement" /></Page>} />
            <Route path="/billing/credit-notes" element={<Page access={ACCESS.creditNotes}><CreditNotes /></Page>} />
            <Route path="/billing/credit-notes/:id" element={<Page access={ACCESS.creditNotes}><CreditNotes /></Page>} />
            <Route path="/billing/penalties" element={<Page access={ACCESS.penalties}><Penalties /></Page>} />
            <Route path="/billing/sinking-fund" element={<Page access={ACCESS.sinkingFund}><SinkingFund /></Page>} />
            <Route path="/billing/deposits" element={<Page access={ACCESS.deposits}><Deposits /></Page>} />
            <Route path="/billing/settings" element={<Page access={ACCESS.billingSettings}><BillingSettings /></Page>} />
            <Route path="/finance/budgets" element={<Page access={ACCESS.budgets}><Budgets /></Page>} />
            <Route path="/finance/budgets/:id" element={<Page access={ACCESS.budgets}><Budgets /></Page>} />
            <Route path="/finance/budget-actual" element={<Page access={ACCESS.budgets}><BudgetActual /></Page>} />
            <Route path="/finance/budget-actual/transactions" element={<Page access={ACCESS.budgets}><BudgetActual /></Page>} />
            <Route path="/finance/costs" element={<Page access={ACCESS.costs}><Costs /></Page>} />
            <Route path="/finance/costs/:id" element={<Page access={ACCESS.costs}><Costs /></Page>} />
            <Route path="/finance/accounting" element={<Page access={ACCESS.accounting}><Accounting /></Page>} />
            <Route path="/finance/accounting/:tab" element={<Page access={ACCESS.accounting}><Accounting /></Page>} />
            {/* Vendor Management & Inventory (PRD §24–§25) */}
            <Route path="/vendors" element={<Page access={ACCESS.vendors}><Vendors /></Page>} />
            <Route path="/vendors/:id" element={<Page access={ACCESS.vendors}><Vendors /></Page>} />
            <Route path="/inventory" element={<Page access={ACCESS.inventory}><Inventory /></Page>} />
            {/* Commercial (NC §71): Hotel Booking Management (profile hotel) */}
            <Route path="/commercial" element={<Page><CommercialIndex /></Page>} />
            {/* Commercial (NC §71): Apartment Unit Sales & Rental Management (profile apartment) */}
            <Route path="/commercial/sales" element={<Navigate to="/commercial/sales/listings" replace />} />
            <Route path="/commercial/sales/listings" element={<Page access={ACCESS.unitListings}><UnitListings /></Page>} />
            <Route path="/commercial/sales/listings/:id" element={<Page access={ACCESS.unitListings}><UnitListings /></Page>} />
            <Route path="/commercial/sales/leads" element={<Page access={ACCESS.salesLeads}><SalesLeads /></Page>} />
            <Route path="/commercial/sales/leads/:id" element={<Page access={ACCESS.salesLeads}><SalesLeads /></Page>} />
            <Route path="/commercial/sales/reservations" element={<Page access={ACCESS.saleReservations}><SaleReservations /></Page>} />
            <Route path="/commercial/sales/reservations/:id" element={<Page access={ACCESS.saleReservations}><SaleReservations /></Page>} />
            <Route path="/commercial/rental" element={<Navigate to="/commercial/rental/listings" replace />} />
            <Route path="/commercial/rental/listings" element={<Page access={ACCESS.rentalListings}><RentalListings /></Page>} />
            <Route path="/commercial/rental/listings/:id" element={<Page access={ACCESS.rentalListings}><RentalListings /></Page>} />
            <Route path="/commercial/rental/reservations" element={<Page access={ACCESS.rentalReservations}><RentalReservations /></Page>} />
            <Route path="/commercial/rental/reservations/:id" element={<Page access={ACCESS.rentalReservations}><RentalReservations /></Page>} />
            <Route path="/commercial/rental/calendar" element={<Page access={ACCESS.rentalReservations}><RentalCalendar /></Page>} />
            <Route path="/commercial/hotel" element={<Navigate to="/commercial/hotel/reservations" replace />} />
            <Route path="/commercial/hotel/reservations" element={<Page access={ACCESS.hotelReservations}><HotelReservations /></Page>} />
            <Route path="/commercial/hotel/reservations/:id" element={<Page access={ACCESS.hotelReservations}><HotelReservations /></Page>} />
            <Route path="/commercial/hotel/rooms" element={<Page access={ACCESS.hotelRooms}><HotelRooms /></Page>} />
            <Route path="/commercial/hotel/calendar" element={<Page access={ACCESS.hotelReservations}><HotelCalendar /></Page>} />
            <Route path="/reception" element={<Page access={ACCESS.reception}><Reception /></Page>} />
            {/* Reports (PRD §26) */}
            <Route path="/reports" element={<Page access={ACCESS.reports}><Reports /></Page>} />
            <Route path="/reports/:name" element={<Page access={ACCESS.reports}><Reports /></Page>} />
            {/* Housekeeping */}
            <Route path="/housekeeping" element={<DomainHome domain="housekeeping" fallback="/housekeeping/cleaning"><Page access={ACCESS.housekeepingDashboard}><DomainDashboard domain="housekeeping" /></Page></DomainHome>} />
            <Route path="/housekeeping/routes" element={<Page access={ACCESS.cleaningRoutes}><CleaningRoutes /></Page>} />
            <Route path="/housekeeping/shifts" element={<Page access={ACCESS.housekeepingShifts}><Shifts domain="housekeeping" /></Page>} />
            <Route path="/housekeeping/cleaning" element={<Page access={ACCESS.cleaning}><WorkItemList objectType="task" fixedType="cleaning" title="Cleaning" /></Page>} />
            <Route path="/housekeeping/schedule" element={<Page access={ACCESS.cleaningSchedule}><HousekeepingSchedule /></Page>} />
            <Route path="/housekeeping/inspections" element={<Page access={ACCESS.housekeepingInspections}><WorkItemList objectType="task" fixedType="inspection" title="Housekeeping Inspections" housekeeping /></Page>} />
            {/* Property */}
            <Route path="/property" element={<Navigate to="/property/properties" replace />} />
            <Route path="/property/portfolios" element={<Page access={ACCESS.portfolios}><Portfolios /></Page>} />
            {/* Building Management (PRD P1 v2 §6–§8): facility operasional, denah, occupancy — segmen statis mengalahkan /property/:type */}
            <Route path="/property/facilities" element={<Page access={ACCESS.propertyFacilities}><Facilities variant="property" /></Page>} />
            <Route path="/property/floor-plans" element={<Page access={ACCESS.floorPlans}><FloorPlans /></Page>} />
            <Route path="/property/floor-plans/:planId" element={<Page access={ACCESS.floorPlans}><FloorPlans /></Page>} />
            <Route path="/property/occupancy" element={<Page access={ACCESS.occupancy}><Occupancy /></Page>} />
            <Route path="/property/:type" element={<Page access={ACCESS.locations}><PropertyPage /></Page>} />
            <Route path="/property/locations/:id" element={<Page access={ACCESS.locations}><LocationDetail /></Page>} />
            {/* Tenant Relation (modul operasional mandatory, PRD v1.3 §3.2) */}
            <Route path="/tenant-relation" element={<Page access={ACCESS.tenantRelation}><TenantRelationDashboard /></Page>} />
            <Route path="/tenant-relation/service-requests" element={<Page access={ACCESS.serviceRequests}><ServiceRequestList /></Page>} />
            <Route path="/tenant-relation/service-requests/:id" element={<Page access={ACCESS.serviceRequests}><ServiceRequestDetail /></Page>} />
            <Route path="/tenant-relation/tenant-users" element={<Page access={ACCESS.tenantUsers}><TenantUsers /></Page>} />
            <Route path="/tenant-relation/tenant-users/:id" element={<Page access={ACCESS.tenantUsers}><TenantUsers /></Page>} />
            <Route path="/tenant-relation/feedback" element={<Page access={ACCESS.feedback}><TenantFeedback /></Page>} />
            {/* PRD P3 v2.1: feedback umum (deep link /tenant-relation/feedback/general/{id}) & isu berulang */}
            <Route path="/tenant-relation/feedback/:tab" element={<Page access={ACCESS.feedback}><TenantFeedback /></Page>} />
            <Route path="/tenant-relation/feedback/general/:id" element={<Page access={ACCESS.feedback}><TenantFeedback /></Page>} />
            <Route path="/tenant-relation/recurring-issues" element={<Page access={ACCESS.recurringIssues}><RecurringIssues /></Page>} />
            <Route path="/tenant-relation/recurring-issues/:id" element={<Page access={ACCESS.recurringIssues}><RecurringIssues /></Page>} />
            <Route path="/tenant-relation/announcements" element={<Page access={ACCESS.announcements}><Announcements /></Page>} />
            <Route path="/tenant-relation/announcements/:id" element={<Page access={ACCESS.announcements}><Announcements /></Page>} />
            {/* Tenant */}
            <Route path="/tenant" element={<Navigate to="/tenant/tenants" replace />} />
            <Route path="/tenant/tenants" element={<Page access={ACCESS.tenants}><TenantList /></Page>} />
            <Route path="/tenant/tenants/:id" element={<Page access={ACCESS.tenants}><TenantList /></Page>} />
            <Route path="/tenant/service-requests" element={<Page access={ACCESS.serviceRequests}><ServiceRequestList /></Page>} />
            {/* Asset Management */}
            <Route path="/assets" element={<Page access={ACCESS.assets}><AssetList /></Page>} />
            <Route path="/assets/equipment" element={<Page access={ACCESS.equipment}><EquipmentPage /></Page>} />
            <Route path="/assets/history" element={<Page access={ACCESS.assets}><AssetHistory /></Page>} />
            <Route path="/assets/:id" element={<Page access={ACCESS.assets}><AssetDetail /></Page>} />
            {/* PRD P2 v2.1 §5.7/§6.8: kompetensi staf — detail tanpa guard menu (pemilik data boleh melihat miliknya; server memutuskan) */}
            {/* Settings */}
            <Route path="/settings" element={<Navigate to="/settings/organization" replace />} />
            <Route path="/settings/design-system" element={<Page access={ACCESS.designSystem}><DesignSystemPage /></Page>} />
            <Route path="/settings/:section" element={<Page><Settings /></Page>} />
            <Route path="/sync/conflicts/:id" element={<Navigate to="/settings/sync-conflicts" replace />} />
            <Route path="*" element={<NotFoundPage />} />
          </Route>
        </Route>
      </Routes>
      </ErrorBoundary>
    </BrowserRouter>
  );
}
