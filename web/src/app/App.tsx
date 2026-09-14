// Router (React Router v7 library mode) — route sesuai Naming Convention §48 & IA PRD §26; auth guard; code splitting per modul.
import { lazy, Suspense } from "react";
import { BrowserRouter, Navigate, Outlet, Route, Routes, useLocation } from "react-router-dom";
import { AppShell } from "@/components/shell/AppShell";
import { DetailSkeleton } from "@/components/bv/common";
import { useAuth } from "@/lib/auth";
import { LoginPage } from "@/features/auth/LoginPage";

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
const TenantList = lazy(() => import("@/features/tenant/TenantListPage"));
const Settings = lazy(() => import("@/features/settings/SettingsPage"));

function Protected() {
  const { principal, loading } = useAuth();
  const loc = useLocation();
  if (loading) return <div className="p-10"><DetailSkeleton /></div>;
  if (!principal) return <Navigate to="/login" state={{ from: loc.pathname }} replace />;
  return <Outlet />;
}

function Page({ children }: { children: React.ReactNode }) {
  return <Suspense fallback={<DetailSkeleton />}>{children}</Suspense>;
}

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<Protected />}>
          <Route element={<AppShell />}>
            <Route index element={<Navigate to="/overview" replace />} />
            <Route path="/overview" element={<Page><Overview /></Page>} />
            {/* Operations */}
            <Route path="/operations" element={<Navigate to="/operations/work-orders" replace />} />
            <Route path="/operations/tasks" element={<Page><WorkItemList objectType="task" /></Page>} />
            <Route path="/operations/tasks/:id" element={<Page><WorkItemDetail objectType="task" /></Page>} />
            <Route path="/operations/work-orders" element={<Page><WorkItemList objectType="work_order" /></Page>} />
            <Route path="/operations/work-orders/:id" element={<Page><WorkItemDetail objectType="work_order" /></Page>} />
            <Route path="/work-orders/:id" element={<Page><WorkItemDetail objectType="work_order" /></Page>} />
            <Route path="/tasks/:id" element={<Page><WorkItemDetail objectType="task" /></Page>} />
            <Route path="/operations/service-requests" element={<Page><ServiceRequestList /></Page>} />
            <Route path="/operations/service-requests/:id" element={<Page><ServiceRequestDetail /></Page>} />
            <Route path="/service-requests/:id" element={<Page><ServiceRequestDetail /></Page>} />
            <Route path="/operations/incidents" element={<Page><IncidentList /></Page>} />
            <Route path="/operations/incidents/:id" element={<Page><IncidentDetail /></Page>} />
            <Route path="/findings" element={<Page><FindingList /></Page>} />
            <Route path="/findings/:id" element={<Page><FindingDetail /></Page>} />
            {/* Engineering */}
            <Route path="/engineering" element={<Navigate to="/engineering/preventive-maintenance" replace />} />
            <Route path="/engineering/preventive-maintenance" element={<Page><PreventiveMaintenance /></Page>} />
            <Route path="/engineering/preventive-maintenance/:scheduleId" element={<Page><PreventiveMaintenance /></Page>} />
            <Route path="/engineering/corrective-maintenance" element={<Page><WorkItemList objectType="work_order" fixedType="corrective,repair" title="Corrective Maintenance" /></Page>} />
            <Route path="/engineering/inspections" element={<Page><WorkItemList objectType="task" fixedType="inspection" title="Inspections" /></Page>} />
            <Route path="/engineering/assets" element={<Page><AssetList /></Page>} />
            <Route path="/engineering/equipment" element={<Page><EquipmentPage /></Page>} />
            {/* Security */}
            <Route path="/security" element={<Navigate to="/security/patrol" replace />} />
            <Route path="/security/patrol" element={<Page><Patrol /></Page>} />
            <Route path="/security/patrol/:tab" element={<Page><Patrol /></Page>} />
            <Route path="/security/incidents" element={<Page><IncidentList security /></Page>} />
            {/* Housekeeping */}
            <Route path="/housekeeping" element={<Navigate to="/housekeeping/cleaning" replace />} />
            <Route path="/housekeeping/cleaning" element={<Page><WorkItemList objectType="task" fixedType="cleaning" title="Cleaning" /></Page>} />
            <Route path="/housekeeping/schedule" element={<Page><HousekeepingSchedule /></Page>} />
            <Route path="/housekeeping/inspections" element={<Page><WorkItemList objectType="task" fixedType="inspection" title="Housekeeping Inspections" housekeeping /></Page>} />
            {/* Property */}
            <Route path="/property" element={<Navigate to="/property/properties" replace />} />
            <Route path="/property/:type" element={<Page><PropertyPage /></Page>} />
            <Route path="/property/locations/:id" element={<Page><LocationDetail /></Page>} />
            {/* Tenant */}
            <Route path="/tenant" element={<Navigate to="/tenant/tenants" replace />} />
            <Route path="/tenant/tenants" element={<Page><TenantList /></Page>} />
            <Route path="/tenant/tenants/:id" element={<Page><TenantList /></Page>} />
            <Route path="/tenant/service-requests" element={<Page><ServiceRequestList /></Page>} />
            {/* Asset Management */}
            <Route path="/assets" element={<Page><AssetList /></Page>} />
            <Route path="/assets/equipment" element={<Page><EquipmentPage /></Page>} />
            <Route path="/assets/history" element={<Page><AssetHistory /></Page>} />
            <Route path="/assets/:id" element={<Page><AssetDetail /></Page>} />
            {/* Settings */}
            <Route path="/settings" element={<Navigate to="/settings/organization" replace />} />
            <Route path="/settings/:section" element={<Page><Settings /></Page>} />
            <Route path="/sync/conflicts/:id" element={<Navigate to="/settings/sync-conflicts" replace />} />
            <Route path="*" element={<div className="py-20 text-center text-muted-foreground">Halaman tidak ditemukan.</div>} />
          </Route>
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
