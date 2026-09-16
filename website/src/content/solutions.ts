// Solution pages (Website PRD §9–§11): satu platform, dikonfigurasi per profile. Kapabilitas mengikuti daftar PRD per profile.
import type { ImageKey } from "@/components/Picture";
import type { Faq, Feature, Step } from "@/components/blocks";

export interface Solution {
  slug: "hotel" | "apartment" | "office";
  name: string;
  seoTitle: string;
  seoDescription: string;
  eyebrow: string;
  title: string;
  lead: string;
  hero: ImageKey;
  problems: { title: string; body: string }[];
  capabilities: Feature[];
  workflow: Step[];
  workflowImage: ImageKey;
  splits: { title: string; lead: string; bullets: string[]; image: ImageKey }[];
  faq: Faq[];
  quote: { quote: string; name: string; role: string };
}

export const SOLUTIONS_CONTENT: Record<Solution["slug"], Solution> = {
  hotel: {
    slug: "hotel",
    name: "Hotel",
    seoTitle: "Hotel Operations Software",
    seoDescription: "Run housekeeping, engineering, security, and guest relation from one platform. BuildingVision keeps hotel rooms, requests, and work orders moving. Free 14-day trial.",
    eyebrow: "Solutions · Hotel",
    title: "Hotel operations that keep every room ready",
    lead: "From turnover cleaning to a guest request at 2 a.m., BuildingVision gives your housekeeping, engineering, security, and front desk one shared list of what needs attention now.",
    hero: "hotel-pool",
    problems: [
      { title: "Room status lives in people's heads", body: "Housekeeping knows a room is dirty, the front desk thinks it is ready, and the guest finds out first." },
      { title: "Guest requests get lost between shifts", body: "A request taken by phone at the desk has no owner, no due time, and no photo of the fix." },
      { title: "Maintenance is reactive", body: "Chillers, lifts, and pumps fail on the busiest weekend because preventive schedules live in a spreadsheet." },
    ],
    capabilities: [
      { icon: "hotel", title: "Hotel Operations", body: "Arrivals, departures, in-house guests, and room status on one screen for the front desk and reception." },
      { icon: "cleaning_services", title: "Housekeeping", body: "Turnover cleaning created automatically at check-out, room inspections, and photo evidence per room." },
      { icon: "engineering", title: "Engineering", body: "Preventive maintenance for HVAC, lifts, and pumps, plus corrective work orders from any guest complaint." },
      { icon: "shield", title: "Security", body: "Patrol routes with QR checkpoints, incident reports, and shift handover that does not depend on memory." },
      { icon: "room_service", title: "Guest Relation", body: "Guest requests from the app or the desk become tracked work with a clear owner and a visible status." },
      { icon: "bed", title: "Room Operations", body: "Dirty, clean, inspected, available: room state changes flow from housekeeping to reception without a call." },
      { icon: "meeting_room", title: "Facility Management", body: "Function rooms, pools, and gyms with schedules, closures, and bookings." },
      { icon: "badge", title: "Visitor Management", body: "Visitors and contractors registered, approved, and checked in with a QR pass." },
      { icon: "construction", title: "Work Orders", body: "Every job with priority, due time, checklist, and before-and-after photos." },
      { icon: "inventory", title: "Inventory", body: "Spare parts and consumables used against work orders, with low-stock alerts." },
      { icon: "handshake", title: "Vendor Management", body: "Assign outside vendors to work orders and track their performance over time." },
      { icon: "calendar_month", title: "Hotel Booking Operations", body: "Reservations, room assignment, check-in and check-out, and invoices, connected to housekeeping." },
    ],
    workflow: [
      { title: "Guest checks out", body: "Reception marks the departure. The room turns Dirty and a turnover cleaning task is created for housekeeping." },
      { title: "Housekeeping cleans and documents", body: "Staff follow the checklist in the Staff App, attach photos, and mark the room Clean." },
      { title: "Supervisor inspects", body: "A quick inspection sets the room to Available. Anything not OK becomes a finding and a work order." },
      { title: "Front desk sees it instantly", body: "No radio calls, no waiting. The next guest gets a room that is actually ready." },
    ],
    workflowImage: "hotel-room",
    splits: [
      { title: "Reception and housekeeping, finally in sync", lead: "Room status is a shared fact, not a phone call.", bullets: ["Room board: dirty, clean, inspected, available, out of order", "Turnover tasks created automatically at check-out", "Guest requests logged at the desk with one tap"], image: "hotel-suite" },
      { title: "Events and facilities without the chaos", lead: "Function rooms and amenities booked, prepared, and reset on time.", bullets: ["Facility schedules and closures", "Booking approvals when you need them", "Setup and reset tasks tied to each booking"], image: "banquet-hall" },
    ],
    faq: [
      { q: "Do we need a separate product for hotels?", a: "No. BuildingVision is one platform. When you create a property with the Hotel profile, the terminology, workflows, and modules adjust to hotel operations." },
      { q: "Does BuildingVision replace our PMS or channel manager?", a: "It is not a channel manager. Hotel Booking Management covers reservations, room assignment, check-in and check-out, and connects them to housekeeping and engineering. Integrations with external systems are handled case by case." },
      { q: "Can guests use the app?", a: "Yes. The Tenant App works as a Guest App for hotels, with guest requests, announcements, and facility bookings during their stay." },
    ],
    quote: { quote: "Turnover used to be a radio conversation. Now reception sees the room go from dirty to available on the board, and nobody has to ask.", name: "Rina S.", role: "Front Office Manager, 180-room city hotel" },
  },
  apartment: {
    slug: "apartment",
    name: "Apartment",
    seoTitle: "Apartment Management Software",
    seoDescription: "Resident requests, housekeeping, engineering, security, billing, and unit sales or rental in one apartment management platform. Free 14-day trial.",
    eyebrow: "Solutions · Apartment",
    title: "Apartment management residents actually feel",
    lead: "Give residents a simple app to report issues and pay bills, and give your building team a clear plan for every request, patrol, and maintenance job.",
    hero: "apartment-exterior",
    problems: [
      { title: "Residents chase updates on WhatsApp", body: "Requests arrive in group chats, get answered by whoever is awake, and nobody knows what was fixed." },
      { title: "Billing and operations do not talk", body: "Finance chases payments in one system while operations tracks work in another. Residents get mixed messages." },
      { title: "Unit turnover is manual", body: "Move-in, move-out, and rental activation involve paper forms and re-typing the same resident details." },
    ],
    capabilities: [
      { icon: "space_dashboard", title: "Property Operations", body: "One overview of open requests, overdue work, patrols, and cleaning across every tower." },
      { icon: "cleaning_services", title: "Housekeeping", body: "Cleaning schedules for lobbies, corridors, and amenities with inspections and photo evidence." },
      { icon: "engineering", title: "Engineering", body: "Preventive maintenance for lifts, pumps, and gensets. Corrective work orders from any resident request." },
      { icon: "shield", title: "Security", body: "Patrol routes with QR checkpoints, incident reports, and visitor verification at the gate." },
      { icon: "forum", title: "Tenant Relation", body: "Validate resident accounts, reply to messages, publish announcements, and read feedback." },
      { icon: "group", title: "Resident Management", body: "Units, owners, occupants, and access, kept accurate through move-in and move-out." },
      { icon: "sell", title: "Unit Sales", body: "Listings, leads, reservations, and handover for units your organization sells." },
      { icon: "key", title: "Unit Rental", body: "Daily, weekly, and monthly rental with availability, deposits, and automatic resident onboarding." },
      { icon: "meeting_room", title: "Facility Management", body: "Function rooms, pools, and courts bookable by residents with approval rules you set." },
      { icon: "badge", title: "Visitor Management", body: "Residents pre-register guests. Security scans the QR pass at the gate." },
      { icon: "phone_iphone", title: "Tenant App", body: "Residents report issues, follow progress, book facilities, register visitors, and view bills." },
      { icon: "construction", title: "Work Orders", body: "Every job with an owner, due time, checklist, and before-and-after photos." },
      { icon: "inventory", title: "Inventory", body: "Spare parts used against work orders with low-stock alerts." },
      { icon: "handshake", title: "Vendor Management", body: "Assign vendors to work orders and track their performance." },
      { icon: "receipt_long", title: "Billing", body: "Invoices per unit, resident bills in the app, and payments verified by Finance." },
    ],
    workflow: [
      { title: "Resident reports a leak", body: "From the Tenant App: pick the unit, choose a category, add a photo. The request lands with the right team." },
      { title: "Tenant Relation acknowledges", body: "The resident sees the status change. Internal notes stay internal." },
      { title: "Engineering creates and completes the work order", body: "A technician starts the job in the Staff App, follows the checklist, and uploads the after photo." },
      { title: "Resident confirms and rates", body: "The resident confirms the fix or reopens it. Feedback goes straight to your CSAT report." },
    ],
    workflowImage: "apartment-living",
    splits: [
      { title: "A resident app people keep on their phone", lead: "Requests, bookings, visitors, bills, and announcements in one place.", bullets: ["Report an issue in under a minute", "Book the function room for Saturday", "Pre-register a guest and share the QR pass", "See this month's bill and payment status"], image: "apartment-lounge" },
      { title: "Sales and rental connected to operations", lead: "When a unit is sold or rented, the resident, the unit, and the app access are set up in one flow.", bullets: ["Listings, leads, and reservations", "Rental calendar with availability", "Move-in and move-out without re-typing data"], image: "house-keys" },
    ],
    faq: [
      { q: "Can residents register themselves?", a: "Yes. Residents register from the Tenant App and your Tenant Relation team validates the account before it becomes active." },
      { q: "How does billing work?", a: "You issue invoices per unit, residents see them in the app, and payments are recorded and verified by Finance. Online payment gateways can be enabled when you are ready." },
      { q: "Is this a public marketplace for units?", a: "No. Unit Sales and Rental manage the inventory your organization owns or manages. It is not a public listing marketplace." },
    ],
    quote: { quote: "Residents stopped messaging the manager's personal number. They open the app, and the team sees the request with a photo before anyone picks up the phone.", name: "Dimas P.", role: "Building Manager, 2-tower residence" },
  },
  office: {
    slug: "office",
    name: "Office",
    seoTitle: "Office Building Management Software",
    seoDescription: "Tenant requests, facility operations, access and security, work orders, and billing for office buildings. One platform for property teams. Free 14-day trial.",
    eyebrow: "Solutions · Office",
    title: "Office building management that tenants notice",
    lead: "Give tenant companies a professional way to raise requests and book facilities, and give your operations team a clear, accountable plan for every floor.",
    hero: "hero-towers",
    problems: [
      { title: "Tenant complaints arrive by email and phone", body: "Every request needs to be re-typed, forwarded, and followed up manually. SLAs are a promise, not a measurement." },
      { title: "Patrols and cleaning are hard to prove", body: "You know the team walked the floors, but you cannot show a tenant when, where, or what was found." },
      { title: "Assets are aging in silence", body: "AHUs, chillers, and lifts run past their service dates because the schedule lives with one engineer." },
    ],
    capabilities: [
      { icon: "space_dashboard", title: "Property Operations", body: "One overview of requests, overdue work, SLA risk, patrols, and cleaning across buildings." },
      { icon: "cleaning_services", title: "Housekeeping", body: "Cleaning schedules per floor and toilet, inspections with pass or fail, and photo evidence." },
      { icon: "engineering", title: "Engineering", body: "Preventive maintenance plans, inspections, and corrective work orders with asset history." },
      { icon: "shield", title: "Security", body: "Patrol routes, QR checkpoints, missed checkpoint alerts, and incident reports." },
      { icon: "forum", title: "Tenant Relation", body: "Tenant accounts, messages, announcements, and satisfaction feedback." },
      { icon: "corporate_fare", title: "Tenant Management", body: "Tenant companies, units, occupants, and contacts kept in one place." },
      { icon: "phone_iphone", title: "Tenant App", body: "Tenant staff report issues, book meeting rooms, register visitors, and see invoices." },
      { icon: "meeting_room", title: "Facility Management", body: "Meeting rooms, auditoriums, and shared amenities with schedules and approvals." },
      { icon: "badge", title: "Visitor Management", body: "Tenants pre-register visitors. Reception and security verify the QR pass." },
      { icon: "lock", title: "Access & Security Operations", body: "Visitor verification, incident handling, and patrol compliance you can show tenants." },
      { icon: "construction", title: "Work Orders", body: "Every job with priority, due time, checklist, and before-and-after photos." },
      { icon: "inventory", title: "Inventory", body: "Spare parts used against work orders with low-stock alerts." },
      { icon: "handshake", title: "Vendor Management", body: "Assign vendors to work orders and track on-time performance." },
      { icon: "receipt_long", title: "Billing", body: "Invoices per tenant, visible in the app, with payments verified by Finance." },
    ],
    workflow: [
      { title: "Tenant reports the AC is warm on Floor 12", body: "From the Tenant App or the reception desk, the request is created with the location and a photo." },
      { title: "SLA starts, team is assigned", body: "Priority and response time come from the category. Engineering sees it on their list immediately." },
      { title: "Technician fixes and documents", body: "Work order started in the Staff App, checklist completed, after photo attached." },
      { title: "Tenant is updated, report is ready", body: "The tenant confirms the fix. Your monthly SLA report already includes it." },
    ],
    workflowImage: "open-office",
    splits: [
      { title: "Show tenants the service they pay for", lead: "SLA response and resolution, patrol completion, cleaning inspections, all in reports you can share.", bullets: ["SLA compliance per category and priority", "Patrol and cleaning completion with findings", "Facility utilization and visitor volumes"], image: "meeting-room" },
      { title: "Assets with a memory", lead: "Every AHU, chiller, and lift has a QR code, a maintenance plan, and a history.", bullets: ["Scan the QR to see history and open a work order", "Preventive schedules generated automatically", "Parts and vendor costs tracked per asset"], image: "server-room" },
    ],
    faq: [
      { q: "Can tenant companies have multiple users?", a: "Yes. A tenant admin can manage users within their company, and each user can raise requests and book facilities." },
      { q: "Can we run several office buildings in one account?", a: "Yes. An organization can manage multiple properties. Users switch between properties, and reports can be per property or across the portfolio." },
      { q: "How do we onboard reception and security teams?", a: "Invite them with a role. Reception works from the dashboard; security officers use the Staff App for patrols and visitor verification." },
    ],
    quote: { quote: "Our tenants get an SLA report every month now. Not because we promised it, but because the system already has the numbers.", name: "Andi W.", role: "Property Manager, Grade A office tower" },
  },
};
