// Routes (Website PRD §4–§5, §12–§14): seluruh route statis (prerender) (lihat content/site.ts ALL_ROUTES).
import { Navigate, Route, Routes } from "react-router-dom";
import { Layout } from "./components/Layout";
import HomePage from "./pages/HomePage";
import SolutionPage from "./pages/SolutionPage";
import PlatformPage, { PlatformIndexPage } from "./pages/PlatformPage";
import PricingPage from "./pages/PricingPage";
import { SHOW_PRICING } from "@/lib/config";
import DownloadPage from "./pages/DownloadPage";
import ResourcePage, { ResourcesIndexPage } from "./pages/ResourcesPages";
import { AboutPage, BookDemoPage, LegalPage, LoginRedirect, NotFoundPage, SecurityPage, StartTrialRedirect } from "./pages/misc";

export function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<HomePage />} />
        <Route path="/solutions" element={<Navigate to="/solutions/hotel" replace />} />
        <Route path="/solutions/:slug" element={<SolutionPage />} />
        <Route path="/platform" element={<PlatformIndexPage />} />
        <Route path="/platform/:slug" element={<PlatformPage />} />
        <Route path="/pricing" element={SHOW_PRICING ? <PricingPage /> : <Navigate to="/" replace />} />
        <Route path="/resources" element={<ResourcesIndexPage />} />
        <Route path="/resources/:section" element={<ResourcePage />} />
        <Route path="/download" element={<DownloadPage />} />
        <Route path="/about" element={<AboutPage />} />
        <Route path="/security" element={<SecurityPage />} />
        <Route path="/book-a-demo" element={<BookDemoPage />} />
        <Route path="/start-free-trial" element={<StartTrialRedirect />} />
        <Route path="/signup" element={<StartTrialRedirect />} />
        <Route path="/login" element={<LoginRedirect />} />
        <Route path="/legal/:doc" element={<LegalPage />} />
        <Route path="/404" element={<NotFoundPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
