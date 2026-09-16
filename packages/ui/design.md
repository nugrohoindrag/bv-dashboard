# Nexus UI — Master Design System Specification (`design.md`)

> **Version**: 14.0.0 (Master Final Architecture)  
> **System Name**: **Nexus UI — Universal Material 3 Morphic Design System**  
> **Taxonomy**: 13 Canonical Pillars  
> **Foundations**: [Google Material Design 3 (m3.material.io)](https://m3.material.io/) & Soft Morphic Surfaces  
> **Motion Engine**: [Motion for React (motion.dev)](https://motion.dev/)  
> **Typography**: Google Plus Jakarta Sans, Inter, Roboto Flex (`tabular numbers` active)  
> **Iconography**: Google Material Symbols Rounded (Variable Font)  
> **Licensing**: Open Source (MIT / Apache 2.0 / OFL 1.1 / Unsplash Open License)  
> **Status**: **MASTER CANONICAL SPECIFICATION**  

---

```
DESIGN SYSTEM
│
├── 01. Foundations
│   ├── Color (Obsidian Dark & Clean Neutral Light, 5 Dynamic Accents)
│   ├── Typography (Plus Jakarta Sans, Inter, Tabular Numerals)
│   ├── Iconography (Material Symbols Rounded)
│   ├── Spacing (4px, 8px, 12px, 16px, 20px, 24px, 32px, 48px, 64px)
│   ├── Grid (12-column responsive layout grid)
│   ├── Shape (6px, 10px, 14px, 18px, 22px, 28px, 999px pill)
│   ├── Elevation (Level 0 through Level 5 M3 Shadows)
│   └── Motion (Spring physics, Stagger, LayoutId, SVG PathLength)
│
├── 02. Layout
│   ├── Container (Responsive max-width wrapper)
│   ├── Stack (Vertical flex primitive)
│   ├── Row (Horizontal flex primitive with wrap support)
│   ├── Grid (CSS Grid wrapper with minColWidth & columns)
│   ├── Split Layout (Multi-column resizable/fixed ratio split)
│   ├── App Shell (Sticky TopBar, Sidebar, Main, Footer)
│   └── Section (Content section with title, subtitle, and action)
│
├── 03. Navigation
│   ├── Sidebar / Navigation Drawer (Modal & Standard 360px)
│   ├── Navigation Rail (Compact 80px rail with badges)
│   ├── Top Bar / Top App Bar (Center-aligned, Small, Medium, Large)
│   ├── Navigation Item (Item with active indicator pill & badge)
│   ├── Breadcrumb (M3 breadcrumb with icons)
│   ├── Tabs (Primary & Secondary indicator tabs)
│   └── Pagination (Page numbers with quick-jump & entry count)
│
├── 04. Actions
│   ├── Button (Filled, Elevated, Tonal, Outlined, Text)
│   ├── Icon Button (Standard, Filled, Tonal, Outlined)
│   ├── FAB (Small, Standard, Large, Extended Floating Action Button)
│   ├── Button Group (Attached & Spaced button groups)
│   ├── Split Button (Main action + Dropdown trigger)
│   └── Menu (Dropdown Context Menu)
│
├── 05. Inputs & Forms
│   ├── Text Field (Filled & Outlined with floating labels & validation)
│   ├── Search (Search field with clear button & debounce)
│   ├── Select (Single select with search filter & icons)
│   ├── Multi Select (Multi-select with tag chips & count)
│   ├── Date Picker (M3 28px radius calendar modal & inline)
│   ├── Date Range Picker (Range calendar modal)
│   ├── Checkbox (Unchecked, Checked, Indeterminate)
│   ├── Radio (Radio button & Radio Group)
│   ├── Switch (Toggle switch with state icons)
│   ├── Slider (Continuous & discrete range slider)
│   └── Text Area (Multiline text input with character counter)
│
├── 06. Selection
│   ├── Chip (Assist, Filter, Input, Suggestion)
│   ├── Filter Chip (Toggled filter chip with checkmark)
│   ├── Choice Chip (Single-selection choice chip)
│   ├── Segmented Control (Segmented button bar)
│   ├── Toggle Group (Icon & label toggle button group)
│   └── Filter Bar (Sticky filter toolbar with active chips & reset)
│
├── 07. Surfaces & Containers
│   ├── Card (Basic, Elevated, Filled, Outlined, Interactive)
│   ├── Hero Card (Gradient & SVG morphic hero banner)
│   ├── Section Card (Card with structured header, body, footer)
│   ├── Panel (Collapsible accordion panel with badge & icon)
│   ├── Surface (Configurable elevation & container token wrapper)
│   └── Sheet (Bottom Sheet & Side Sheet)
│
├── 08. Data Display
│   ├── KPI Card (Metric value, delta percentage, sparkline)
│   ├── Metric Card (Icon, counter, trend indicator)
│   ├── Stat (Minimal stat number & label with tabular digits)
│   ├── Badge (Notification dot & numerical badge)
│   ├── Status (Pulsing status badge: Online, Offline, Warning, Idle)
│   ├── Avatar (Image, Initials, Status ring, AvatarGroup)
│   ├── List (Structured list container)
│   ├── List Item (Two-line / three-line item with leading & trailing slot)
│   ├── Data Table (AdvancedDataTable with sort, select, density toggle)
│   ├── Data Row (Expandable sub-row & selection row)
│   └── Timeline (Vertical audit & activity event timeline)
│
├── 09. Data Visualization
│   ├── Chart Card (Card wrapper with controls, header, and legend)
│   ├── Line Chart (Smooth SVG line chart with gradient fill)
│   ├── Area Chart (Filled area telemetry chart)
│   ├── Bar Chart (Vertical & Horizontal bar charts)
│   ├── Donut Chart (Interactive donut chart with center metric)
│   ├── Gauge (Semi-circle radial progress gauge)
│   ├── Sparkline (Mini trend line)
│   ├── Legend (Interactive series toggle legend)
│   ├── Tooltip (Hover telemetry coordinate tooltip)
│   └── Chart Controls (Time range & metric switchers)
│
├── 10. Feedback & Status
│   ├── Alert (Success, Warning, Error, Info inline callout)
│   ├── Banner (InfoBanner, WarningBanner with action & dismiss)
│   ├── Snackbar (Bottom floating notification bar)
│   ├── Toast (Stacked floating toast notifications)
│   ├── Progress (Linear Determinate/Indeterminate, Circular)
│   ├── Loading (Spinner & animated pulse dots)
│   ├── Skeleton (SkeletonKPI, SkeletonChart, SkeletonTable, SkeletonCard)
│   ├── Empty State (Illustration, description, action CTA)
│   ├── Error State (Failure indicator, error message, retry trigger)
│   └── Success State (Completion confirmation, next step button)
│
├── 11. Overlays
│   ├── Dialog (M3 confirmation/action dialog with 28px radius)
│   ├── Modal (General modal frame with blur backdrop)
│   ├── Drawer (Sliding navigation drawer)
│   ├── Side Sheet (Right-side slide-over sheet)
│   ├── Bottom Sheet (Drag-to-dismiss mobile sheet)
│   ├── Popover (Anchored trigger popover)
│   ├── Dropdown (Action & menu dropdown)
│   ├── Context Menu (Right-click & button context menu)
│   └── Tooltip (Hover/focus descriptive tooltip)
│
├── 12. Utility
│   ├── Search Bar (Global search bar with leading & trailing slots)
│   ├── Date Filter (Quick period selector: Today, 7D, 30D, Quarter)
│   ├── Export (Export dropdown with Excel, PDF, CSV formats)
│   ├── Refresh (Spinning refresh button)
│   ├── View Switcher (Grid, List, Table view mode toggle)
│   ├── Sort Control (Field & ascending/descending order selector)
│   └── Bulk Actions (Floating multi-row selection toolbar)
│
└── 13. Domain Components
    ├── Map (Interactive SVG coordinate map with pulse markers)
    ├── Map Marker (Interactive location pin)
    ├── Map Cluster (Aggregated regional node cluster)
    ├── Location Card (Branch & warehouse node telemetry card)
    ├── Asset Card (Hardware & server asset card with capacity bar)
    ├── Warehouse Card (Facility utilization & active shipment card)
    ├── Delivery Card (Logistics shipment tracker with progress & ETA)
    ├── Alert Center (Operational incident & notification hub)
    ├── Activity Feed (Real-time operational audit log)
    └── Operational Status (30-day uptime, active nodes, API success scorecard)
```

---

## 🏛️ 13 Chapters Architectural Specifications

### 01. Foundations
*   **Color Token Standard**: Full W3C Design Tokens format ([src/tokens/colors.css](file:///d:/Design%20System/src/tokens/colors.css))
*   **Surfaces**: Neutral light (`#F8F9FA`) and Obsidian dark (`#0B0D10`).
*   **Dynamic Accents**: Emerald (`#10B981`), Indigo (`#6366F1`), Violet (`#8B5CF6`), Amber (`#F59E0B`), Rose (`#F43F5E`).
*   **Shape Scale**: `6px` (xs), `10px` (sm), `14px` (md), `18px` (lg), `22px` (xl), `28px` (hero), `999px` (pill).
*   **Elevation**: M3 Elevation Levels 0–5.
*   **Typography**: Tabular numerals enabled (`font-feature-settings: 'tnum' 1, 'lnum' 1`).
*   **Motion**: Motion for React spring physics (`stiffness: 380, damping: 30`), layoutId container transforms, SVG pathLength drawing.

### 02. Layout
*   `Container`: Max-width constrained wrapper (`sm` to `2xl`).
*   `Stack` & `Row`: Declarative flex primitives with typed gap, alignment, and wrapping.
*   `Grid`: CSS Grid wrapper with `minColWidth` auto-fill calculation.
*   `SplitLayout`: Multi-column split ratios (`60/40`, `70/30`, `50/50`).
*   `AppShell`: Standard enterprise page layout (Sticky header, sidebar, main area, footer).
*   `Section`: Semantic section block with title, subtitle, and action slot.

### 03. Navigation
*   `TopAppBar`: Material 3 Top App Bar with center-aligned, small, medium, and large variants.
*   `NavigationBar` & `NavigationRail`: Bottom bar (80px) and Rail (80px) with active pill indicators.
*   `NavigationDrawer`: 360px standard and modal drawer with header and badge slots.
*   `Breadcrumbs`: Hierarchy path with icons and slash/chevron separators.
*   `Tabs`: Primary indicator tabs with smooth spring underline animation.
*   `Pagination`: Accessible pagination bar with page jumper and total entity counter.

### 04. Actions
*   `Button`: 5 M3 variants (Filled, Elevated, Tonal, Outlined, Text) with loading state and icons.
*   `IconButton`: Standard, Filled, Tonal, and Outlined icon action buttons.
*   `FAB`: Floating Action Button in Small (40px), Standard (56px), Large (96px), and Extended.
*   `ButtonGroup`: Attached and Spaced button groups.
*   `SplitButton`: Primary trigger with attached chevron dropdown menu.
*   `Menu`: Floating action and context menu.

### 05. Inputs & Forms
*   `FilledTextField` & `OutlinedTextField`: Floating label transition, error message, helper text, and leading/trailing icons.
*   `Select`: Single dropdown with searchable filtering and icon support.
*   `MultiSelect`: Multi-item dropdown with removable tag chips and counter.
*   `DatePickerModal` & `DateRangePickerModal`: Official M3 calendar picker with 28px rounded dialog.
*   `Checkbox`, `Radio`, `Switch`: Custom SVG checkmark, radio circle, and toggle switch with icons.
*   `Slider`: Continuous and discrete slider with value tooltip.
*   `TextArea`: Auto-growing or fixed multiline text field with character count.

### 06. Selection
*   `Chip`: Filter, Assist, Choice, and Input chips.
*   `SegmentedButton`: Single and multi-select segmented bar.
*   `ToggleGroup`: Icon/text toggle button group.
*   `FilterBar`: Sticky horizontal filter toolbar with active chips, search slot, and clear button.

### 07. Surfaces & Containers
*   `Card`: Basic, Elevated, Filled, Outlined, and Hover Interactive cards.
*   `HeroCard`: Organic vector shape and gradient hero banner without intrusive line artifacts.
*   `SectionCard`: Card with distinct header, body, and action footer sections.
*   `Panel`: Expandable accordion container panel with badge indicator.
*   `Surface`: Tokenized surface wrapper supporting glassmorphism and custom elevation.
*   `BottomSheet` & `SideSheet`: Mobile drag-to-dismiss sheet and desktop 400px side sheet.

### 08. Data Display
*   `Stat`: Minimal stat metric with tabular digits and trend indicators.
*   `StatusBadge`: Pulsing live status indicator (Online, Offline, Warning, Idle, Busy).
*   `Avatar` & `AvatarGroup`: Photo, Initials, Status dot, and stacked group with overflow badge.
*   `Timeline`: Vertical event and audit trail stream with status nodes.
*   `AdvancedDataTable`: Enterprise table with sortable columns, multi-row selection, expandable sub-rows, search filter, and density switcher.

### 09. Data Visualization
*   `ChartCard`: Standardized card container for charts with time period and metric controls.
*   `LineChart` & `AreaChart`: High-performance SVG line/area charts with smooth bezier curves.
*   `BarChart` & `StackedBarChart` & `HorizontalBarChart`: Metric bar charts.
*   `PieChart` & `DonutChart`: Circular allocation charts with hover slice highlight.
*   `RadarChart`: 360° multi-axis system evaluation radar.
*   `HeatmapGrid`: 7-day x 12-hour activity density matrix.
*   `GaugeChart`: Radial progress arc with center numerical telemetry.
*   `Sparkline`: Inline SVG trend line for KPI cards.

### 10. Feedback & Status
*   `AlertCallout`: Inline alert box with Success, Warning, Error, Info variants.
*   `InfoBanner` & `WarningBanner`: Full-width or card banner with dismiss and action buttons.
*   `Snackbar`: Bottom floating notification with optional action.
*   `Toast`: Stacked floating notification toasts with auto-dismiss.
*   `LinearProgress` & `CircularProgress`: Determinate and indeterminate loading indicators.
*   `LoadingSpinner`: Spinning SVG progress ring.
*   `Skeleton`: Shimmer loading suite (SkeletonCard, SkeletonKPI, SkeletonChart, SkeletonTable).
*   `EmptyState`: Empty illustration with title, explanation, and action button.
*   `ErrorState`: Network/API failure screen with retry trigger.
*   `SuccessState`: Operation completed confirmation card with next action.

### 11. Overlays
*   `Dialog`: Material 3 alert/confirmation modal dialog with 28px border radius.
*   `Modal`: Universal modal frame with backdrop blur.
*   `Drawer`: Navigation drawer.
*   `SideSheet`: Slide-over inspection sheet.
*   `BottomSheet`: Modal bottom sheet.
*   `Popover`: Anchored floating popover.
*   `ContextMenu`: Floating contextual action list.
*   `Tooltip`: Rich descriptive tooltip.

### 12. Utility
*   `SearchBar`: Input with search icon, clear button, and avatar slot.
*   `DateFilter`: Quick period selector (`Today`, `7D`, `30D`, `Quarter`, `Custom`).
*   `ExportButton`: Trigger supporting Excel (.xlsx), PDF, and CSV formats.
*   `RefreshButton`: Animated spinning refresh trigger.
*   `ViewSwitcher`: Toggle between Grid, List, and Table views.
*   `SortControl`: Field dropdown with Ascending/Descending direction toggle.
*   `BulkActionsBar`: Floating selection toolbar with export and delete actions.

### 13. Domain Components
*   `MapCard`: Interactive SVG coordinate map with pulsing pins and telemetry detail card.
*   `LocationCard`: Node/facility telemetry summary.
*   `WarehouseCard`: Facility storage occupancy rate and active shipments.
*   `DeliveryCard`: Logistics shipment tracker with progress bar and ETA.
*   `AlertCenter`: Critical incident hub with severity filters.
*   `OperationalStatus`: Scorecard displaying 30-day uptime, active nodes, and API success rate.
*   `ExecutiveDashboard`: Universal executive dashboard template.
*   `EcommerceStorefront`: Digital commerce product grid template.
*   `SaasCrmWorkspace`: Team management and access control template.

---

## 💻 Developer Quick Start

```tsx
import {
  Button,
  Select,
  AdvancedDataTable,
  MapCard,
  GaugeChart,
  Stat,
  StatusBadge,
  Stack,
  Row,
  Grid,
} from 'nexus-ui';

export function OperationalDashboard() {
  return (
    <Stack gap="xl">
      <Row justify="space-between">
        <Stat label="Total Volume" value="$2.48M" change="+14.2%" trend="up" />
        <StatusBadge status="online" label="All Systems Operational" />
      </Row>

      <Grid columns={2} gap="lg">
        <MapCard />
        <GaugeChart value={88} title="Cluster Health" />
      </Grid>
    </Stack>
  );
}
```
