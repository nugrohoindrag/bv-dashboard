// Struktur situs (Website PRD §4–§5, §12): navigasi, solutions, platform pages. Sumber tunggal untuk header, footer, sitemap.
import { SHOW_PRICING } from "@/lib/config";
export interface NavLeaf { to: string; label: string; blurb?: string; icon?: string }

export const SOLUTIONS: NavLeaf[] = [
  { to: "/solutions/hotel", label: "Hotel", blurb: "Rooms, guests, housekeeping turnover, front desk.", icon: "hotel" },
  { to: "/solutions/apartment", label: "Apartment", blurb: "Residents, units, building services, billing.", icon: "apartment" },
  { to: "/solutions/office", label: "Office", blurb: "Tenants, floors, access, shared facilities.", icon: "business" },
];

export const PLATFORM: NavLeaf[] = [
  { to: "/platform/property-operations", label: "Property Operations", blurb: "One view of what needs attention today.", icon: "space_dashboard" },
  { to: "/platform/housekeeping", label: "Housekeeping", blurb: "Cleaning schedules, inspections, room status.", icon: "cleaning_services" },
  { to: "/platform/engineering", label: "Engineering", blurb: "Preventive maintenance, assets, corrective work.", icon: "engineering" },
  { to: "/platform/security", label: "Security", blurb: "Patrols with QR checkpoints, incidents, visitors.", icon: "shield" },
  { to: "/platform/tenant-relation", label: "Tenant Relation", blurb: "Requests, messages, announcements, feedback.", icon: "forum" },
  { to: "/platform/work-orders", label: "Work Orders", blurb: "Tasks, checklists, evidence, SLA.", icon: "construction" },
  { to: "/platform/asset-management", label: "Asset Management", blurb: "Asset registry, QR codes, history.", icon: "inventory_2" },
  { to: "/platform/mobile-staff", label: "Mobile Staff", blurb: "Staff App that works offline.", icon: "smartphone" },
  { to: "/platform/tenant-app", label: "Tenant App", blurb: "Requests, bookings, visitors, bills.", icon: "phone_iphone" },
];

export const RESOURCES: NavLeaf[] = [
  { to: "/resources/documentation", label: "Documentation", blurb: "Set up and configure BuildingVision.", icon: "menu_book" },
  { to: "/resources/help-center", label: "Help Center", blurb: "Answers for everyday questions.", icon: "help" },
  { to: "/resources/guides", label: "Guides", blurb: "Practical playbooks for property teams.", icon: "auto_stories" },
  { to: "/resources/blog", label: "Blog", blurb: "Product updates and operations notes.", icon: "article" },
];

export const COMPANY: NavLeaf[] = [
  { to: "/about", label: "About" },
  { to: "/security", label: "Security & Trust" },
  ...(SHOW_PRICING ? [{ to: "/pricing", label: "Pricing" }] : []),
  { to: "/book-a-demo", label: "Book a Demo" },
  { to: "/download", label: "Download Apps" },
];

export const LEGAL: NavLeaf[] = [
  { to: "/legal/terms", label: "Terms of Service" },
  { to: "/legal/privacy", label: "Privacy Policy" },
];

/** Seluruh route statis untuk prerender + sitemap. */
export const ALL_ROUTES: string[] = [
  "/",
  ...SOLUTIONS.map((s) => s.to),
  "/platform",
  ...PLATFORM.map((p) => p.to),
  ...(SHOW_PRICING ? ["/pricing"] : []),
  "/resources",
  ...RESOURCES.map((r) => r.to),
  "/download",
  "/about",
  "/security",
  "/book-a-demo",
  "/start-free-trial",
  "/login",
  ...LEGAL.map((l) => l.to),
  "/404",
];
