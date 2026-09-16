# Design System --- Fino Dashboard / Material 3 Morphic UI

## 1. Purpose

This document translates the uploaded reference video into an
implementation-ready visual and interaction specification for Vibe
Coding.

**Reference:** `threadsdownloader.com_de2ea4(1).mp4`\
**Reference duration:** \~66.9 seconds\
**Reference viewport:** 1152 × 720\
**Stated foundation:** Material Design 3

The target is to reproduce the **same visual language, hierarchy,
density, component behavior, light/dark themes, and motion character**
visible in the reference.

> Important: Material Design 3 is the foundation. The reference is a
> customized product design built on top of M3. Do not generate a
> generic Material 3 application.

------------------------------------------------------------------------

# 2. Product Architecture

This design system is a **general-purpose UI system**, not a
single-product UI kit.

The architecture must follow:

``` text
Foundation
    ↓
Primitive
    ↓
Component
    ↓
Pattern
    ↓
Template
    ↓
Application
```

## Product Layers

### Foundation

Visual and interaction tokens:

-   Color
-   Typography
-   Spacing
-   Sizing
-   Grid
-   Breakpoints
-   Radius
-   Border
-   Elevation
-   Motion
-   Accessibility
-   Z-index

### Primitives

Low-level composable building blocks:

-   Box
-   Stack
-   Inline
-   Grid
-   Flex
-   Text
-   Heading
-   Icon
-   Divider
-   Image
-   Avatar
-   Badge
-   Scroll Area

### Components

Reusable UI components:

-   Buttons
-   Inputs
-   Navigation
-   Cards
-   Tables
-   Lists
-   Dialogs
-   Feedback
-   Charts
-   Selection controls

### Patterns

Reusable combinations of components:

-   Dashboard Header
-   KPI Overview
-   Filter + Table
-   Search + Results
-   Form Section
-   Detail Header
-   Activity Section
-   Analytics Section
-   Map + Detail Panel

### Templates

Ready-to-use application compositions:

-   SaaS Dashboard
-   Analytics Dashboard
-   Admin Dashboard
-   CRM Dashboard
-   Finance Dashboard
-   E-commerce Dashboard
-   HR Dashboard
-   AI Dashboard
-   Project Management
-   Authentication
-   Onboarding
-   Settings

### Themes

The visual system must support theme customization independently from
component structure.

``` text
Theme
├── Light
├── Dark
├── Lime
├── Blue
├── Violet
├── Orange
├── Rose
└── Custom
```

Core components must never hard-code a single brand color.

------------------------------------------------------------------------

# 3. Component Catalog

The component catalog is organized by **function and abstraction
level**, not by application screen or industry.

## 3.1 Foundations

``` text
Color
Typography
Iconography
Spacing
Sizing
Grid
Breakpoints
Shape
Radius
Border
Elevation
Opacity
Motion
Accessibility
Z-Index
```

## 3.2 Primitives

``` text
Box
Stack
Inline
Grid
Flex
Center
Spacer
Text
Heading
Icon
Divider
Image
Avatar
Badge
Scroll Area
Visually Hidden
```

## 3.3 Actions

``` text
Button
Icon Button
Floating Action Button
Button Group
Split Button
Toggle Button
Menu
```

### Button variants

``` text
Filled
Tonal
Outlined
Text
Elevated
Icon
FAB
```

### Button states

``` text
Default
Hover
Focus
Pressed
Disabled
Loading
```

## 3.4 Navigation

``` text
Sidebar
Navigation Rail
Navigation Bar
Top App Bar
Header
Navigation Item
Breadcrumb
Tabs
Tab Bar
Pagination
Stepper
```

## 3.5 Forms & Inputs

``` text
Text Field
Text Area
Search
Number Input
Password Input
Select
Multi Select
Combobox
Autocomplete
Date Picker
Date Range Picker
Time Picker
Checkbox
Radio
Switch
Slider
Range Slider
File Upload
Color Picker
```

## 3.6 Selection

``` text
Chip
Filter Chip
Choice Chip
Input Chip
Tag
Segmented Control
Toggle Group
Tree Select
```

## 3.7 Surfaces & Containers

``` text
Card
Elevated Card
Filled Card
Outlined Card
Hero Card
Section Card
Panel
Surface
Sheet
```

## 3.8 Data Display

``` text
Stat
KPI Card
Metric Card
Summary Card
Insight Card
Progress Card
Status Card
List
List Item
Data Table
Data Grid
Data Row
Timeline
Tree
Accordion
Calendar
```

### Data Grid capabilities

The Data Grid should support:

-   Sorting
-   Filtering
-   Column resize
-   Column reorder
-   Column visibility
-   Row selection
-   Bulk actions
-   Expandable rows
-   Sticky header
-   Pinned columns
-   Pagination
-   Density control
-   Empty state
-   Loading state

## 3.9 Data Visualization

### Charts

``` text
Line Chart
Area Chart
Bar Chart
Column Chart
Stacked Bar
Grouped Bar
Donut Chart
Pie Chart
Radar Chart
Scatter Plot
Bubble Chart
Funnel
Gauge
Progress Chart
Heatmap
Treemap
Sparkline
```

### Chart primitives

``` text
Chart Container
Chart Header
Chart Toolbar
Chart Legend
Chart Tooltip
Chart Axis
Chart Grid
Chart Label
Chart Cursor
Chart Data Point
Chart Empty State
Chart Loading State
```

## 3.10 Feedback & Status

``` text
Alert
Banner
Callout
Snackbar
Toast
Notification
Progress
Circular Progress
Linear Progress
Skeleton
Spinner
Loading Overlay
Empty State
Error State
Success State
Warning State
Offline State
```

## 3.11 Overlays

``` text
Dialog
Modal
Drawer
Side Sheet
Bottom Sheet
Popover
Tooltip
Dropdown
Context Menu
Command Menu
Command Palette
```

## 3.12 Layout

``` text
Container
Stack
Inline
Grid
Flex
Aspect Ratio
Center
Spacer
Split Layout
Sidebar Layout
Resizable Panel
Sticky
Scrollable
```

### Layout presets

``` text
1 Column
2 Columns
3 Columns
4 Columns
8 / 4
7 / 5
6 / 6
9 / 3
```

## 3.13 Utilities

``` text
Search
Filter Bar
Date Filter
Sort Control
View Switcher
Export
Import
Refresh
Bulk Actions
Pagination
Column Visibility
Density Control
Fullscreen
Share
Copy
```

## 3.14 Patterns

Patterns combine multiple components to solve common product problems.

``` text
Dashboard
├── Dashboard Header
├── KPI Overview
├── Analytics Section
├── Activity Section
└── Dashboard Toolbar

Data
├── Data Management
├── Filter + Table
├── Search + Results
└── Bulk Management

Forms
├── Create Form
├── Edit Form
├── Multi-step Form
└── Form Wizard

Settings
├── Settings Navigation
├── Settings Section
└── Profile Settings

Detail
├── Detail Header
├── Detail Overview
├── Detail Tabs
└── Detail Activity

Monitoring
├── Status Overview
├── Alert Summary
├── Activity Feed
└── Event Timeline
```

## 3.15 Templates

Templates are complete screen-level compositions built from patterns.

``` text
SaaS Dashboard
Analytics Dashboard
Admin Dashboard
CRM Dashboard
Finance Dashboard
E-commerce Dashboard
HR Dashboard
AI Dashboard
Project Management Dashboard
Settings
Authentication
Onboarding
```

Templates must use only generic business entities.

Do not encode a specific company, industry, or business vocabulary into
Core Components.

------------------------------------------------------------------------

# 4. Domain-Neutral Design Rules

The Core Design System must remain industry agnostic.

Avoid core components such as:

``` text
Warehouse Card
Asset Card
Polda Card
Delivery Card
Employee Card
Invoice Card
```

Prefer generic components:

``` text
Entity Card
Resource Card
Location Card
Transaction Card
Profile Card
Activity Card
Status Card
```

Industry-specific examples may exist in the **Examples / Templates**
layer.

Example:

``` text
Generic:
Resource Card

Example:
Asset Management Resource Card
```

This preserves reusability while still demonstrating real-world
applications.

------------------------------------------------------------------------

# 5. Component Specification Standard

Every production component must document the following:

``` text
Component
├── Purpose
├── Anatomy
├── Variants
├── Sizes
├── States
├── Properties
├── Design Tokens
├── Typography
├── Color
├── Spacing
├── Shape
├── Elevation
├── Interaction
├── Motion
├── Responsive Behavior
├── Accessibility
├── Usage Rules
├── Do
├── Don't
└── Implementation Notes
```

## Example Anatomy

``` text
KPI Card
├── Header
│   ├── Label
│   └── Icon
├── Value
├── Trend
├── Comparison
└── Visualization
```

This anatomy is a specification for AI and developers, not merely
documentation for designers.

------------------------------------------------------------------------

# 6. Theme Architecture

Components must consume semantic tokens.

Never hard-code product-specific colors inside components.

Bad:

``` css
background: #6FAF39;
```

Preferred:

``` css
background: var(--color-primary);
```

The theme controls the value:

``` text
Component
   ↓
Semantic Token
   ↓
Theme
   ↓
Actual Color
```

This allows the same component library to support different brands
without modifying component structure.

------------------------------------------------------------------------

# 7. Theme Token Groups

Every theme should define:

``` text
Primary
Primary Container
Secondary
Secondary Container
Tertiary
Tertiary Container

Background
Surface
Surface Container
Surface Container Low
Surface Container High
Surface Container Highest

On Background
On Surface
On Surface Variant

Outline
Outline Variant

Success
Warning
Error
Info

Chart Primary
Chart Secondary
Chart Tertiary
Chart Neutral
```

The original green palette remains the **reference theme**, not the
mandatory product identity.

------------------------------------------------------------------------

# 8. Design Token Contract

All components must reference tokens rather than raw values.

``` css
var(--color-primary)
var(--color-surface)
var(--color-on-surface)

var(--space-1)
var(--space-2)
var(--space-4)

var(--radius-sm)
var(--radius-md)
var(--radius-lg)

var(--elevation-1)
var(--elevation-2)

var(--motion-fast)
var(--motion-standard)
```

This makes the system:

-   themeable
-   scalable
-   maintainable
-   AI-readable
-   framework-independent

------------------------------------------------------------------------

# 9. Accessibility Contract

Every interactive component must define:

``` text
Keyboard behavior
Focus state
Focus visibility
Screen reader label
Semantic HTML
Color contrast
Disabled state
Error state
Touch target
Reduced motion behavior
```

Minimum target:

-   WCAG AA color contrast
-   keyboard accessible
-   visible focus
-   minimum 44 × 44px touch target where applicable
-   support `prefers-reduced-motion`

------------------------------------------------------------------------

# 10. Responsive Contract

Every component must define responsive behavior.

``` text
Desktop
Tablet
Mobile
```

Do not simply scale dimensions down.

Components may:

-   stack
-   collapse
-   transform
-   scroll horizontally
-   change navigation mode
-   simplify controls
-   convert tables to lists

------------------------------------------------------------------------

# 11. State Contract

Every interactive component should support:

``` text
Default
Hover
Focus
Pressed
Selected
Disabled
Loading
Error
Success
Empty
```

Data components additionally support:

``` text
No Data
Partial Data
Stale Data
Permission Restricted
Offline
```

------------------------------------------------------------------------

# 12. Vibe Coding Contract

AI-generated implementations must follow this hierarchy:

``` text
Design Tokens
      ↓
Primitive
      ↓
Component
      ↓
Pattern
      ↓
Template
```

Do not create one-off visual styles when an existing component or token
already exists.

Before generating a new component:

1.  Search the component catalog.
2.  Check whether an existing component can be configured through
    variants.
3.  Reuse existing tokens.
4.  Reuse existing interaction patterns.
5.  Create a new component only when the behavior or anatomy is
    genuinely different.

### AI implementation priority

``` text
1. Layout geometry
2. Component anatomy
3. Spacing
4. Typography
5. Color tokens
6. Shape
7. Density
8. Interaction
9. Motion
10. Decoration
```

------------------------------------------------------------------------

# 13. Product Packaging

The design system should be positioned as a product rather than a single
dashboard template.

Recommended package structure:

``` text
Morphic Design System
│
├── Design Tokens
├── Core Components
├── Data Visualization
├── Dashboard Patterns
├── Application Patterns
├── Templates
├── Themes
├── Accessibility Guidelines
├── Vibe Coding Specification
└── Developer Documentation
```

Suggested product statement:

> **Morphic Design System is a Material 3-based UI system for building
> modern, responsive, accessible web applications with a soft, premium,
> information-dense visual language.**

------------------------------------------------------------------------

# 14. Product Differentiation

The product should emphasize:

``` text
Material 3 Foundation
+
Soft Morphic Visual Language
+
100+ Reusable Components
+
Rich Data Visualization
+
Dashboard Patterns
+
Application Templates
+
Light / Dark Themes
+
Multi-theme Tokens
+
Responsive System
+
Accessibility
+
AI / Vibe Coding Ready
```

Do not position the product as a clone of Material 3.

Material 3 provides the structural and interaction foundation.

Morphic Design System provides the customized visual language, component
compositions, tokens, patterns, templates, and implementation
specification.

------------------------------------------------------------------------

# 15. Core Visual Direction

The reference visual language remains:

**Material 3 + Soft Morphic + Modern Enterprise UI**

Characteristics:

-   soft layered surfaces
-   large rounded containers
-   low-contrast borders
-   subtle elevation
-   controlled use of gradients
-   compact information density
-   premium dashboard aesthetics
-   strong visual hierarchy
-   consistent light/dark geometry
-   restrained motion

The visual language must be **brand-neutral at the architecture level**.

The reference green palette is a theme preset.

------------------------------------------------------------------------

# 16. Commercial Design System Quality Bar

A component is production-ready only when it has:

-   [ ] Anatomy documented
-   [ ] Variants documented
-   [ ] Sizes documented
-   [ ] States documented
-   [ ] Design tokens documented
-   [ ] Responsive behavior documented
-   [ ] Accessibility behavior documented
-   [ ] Interaction behavior documented
-   [ ] Light theme
-   [ ] Dark theme
-   [ ] Usage example
-   [ ] Do / Don't guidance
-   [ ] AI implementation notes
-   [ ] Reusable API/props specification

A pattern is production-ready when:

-   [ ] It uses existing components
-   [ ] It has a clear use case
-   [ ] It is responsive
-   [ ] It supports empty/loading/error states
-   [ ] It does not introduce one-off tokens
-   [ ] It can be reused across industries

A template is production-ready when:

-   [ ] It is composed from documented patterns
-   [ ] It is responsive
-   [ ] It supports light/dark themes
-   [ ] It uses generic domain language
-   [ ] It can be adapted to multiple industries

------------------------------------------------------------------------

# 17. Final Architecture

``` text
MORPHIC DESIGN SYSTEM
│
├── 01 Foundations
│
├── 02 Primitives
│
├── 03 Actions
│
├── 04 Navigation
│
├── 05 Forms & Inputs
│
├── 06 Selection
│
├── 07 Surfaces
│
├── 08 Data Display
│
├── 09 Data Visualization
│
├── 10 Feedback
│
├── 11 Overlays
│
├── 12 Layout
│
├── 13 Utilities
│
├── 14 Patterns
│
├── 15 Templates
│
├── 16 Themes
│
└── 17 Accessibility
```

The conceptual hierarchy is:

``` text
FOUNDATION
    ↓
PRIMITIVE
    ↓
COMPONENT
    ↓
PATTERN
    ↓
TEMPLATE
    ↓
APPLICATION
```

The commercial product should remain **generic in Core**, while
industry-specific examples live only in **Patterns, Templates, and
Example Applications**.

# 18. Reference Visual Specification

## Core Style

**Material 3 + Soft Morphism + Modern Fintech Dashboard + Dense
Information UI**

The visual identity combines:

-   Material 3 component logic
-   Extremely soft, rounded surfaces
-   Pale green monochromatic surfaces
-   Bright lime/green accent
-   Low-contrast borders
-   Minimal elevation
-   Layered cards instead of heavy shadows
-   Compact enterprise dashboard density
-   Rounded data visualizations
-   Strong use of negative space
-   Light and dark themes using the same semantic palette

The UI should feel:

-   calm
-   premium
-   modern
-   lightweight
-   financial/productivity oriented
-   information dense without looking crowded

Avoid:

-   default Material 3 purple/blue palettes
-   generic Google Material dashboard appearance
-   excessive glass blur
-   neumorphic heavy shadows
-   excessive gradients
-   sharp rectangular cards
-   high-contrast borders
-   overly colorful charts
-   excessive 3D effects

------------------------------------------------------------------------

# 19. Visual DNA

## 3.1 Surface Language

The dominant visual technique is **soft layered morphism**.

Use:

-   large rounded containers
-   subtle surface-to-surface contrast
-   thin low-contrast borders
-   very soft shadows only where necessary
-   nested cards
-   floating controls
-   rounded chart containers
-   pill-shaped filters and actions

Cards should visually merge into the page rather than look like separate
white boxes.

### Surface hierarchy

``` text
Page background
 └── Main content surface
      ├── Primary feature card
      ├── Secondary metric cards
      ├── Chart cards
      └── Data/list cards
           └── Small nested controls
```

Do not use strong drop shadows between every layer.

------------------------------------------------------------------------

# 20. Color System

The reference is strongly green-driven.

## 4.1 Light Theme --- Reference Approximation

These values are extracted/approximated from the uploaded video and
should be treated as the starting palette.

  Token                            Value Purpose
  -------------------------- ----------- --------------------------
  `primary`                    `#6FAF39` Main brand/action
  `primary-container`          `#B8EA86` Hero/highlight surfaces
  `primary-soft`               `#C7ECA2` Soft accent
  `surface`                    `#F0F7E5` Main application surface
  `surface-container`          `#E5EFD4` Cards/containers
  `surface-container-high`     `#D5E3BE` Elevated/nested surfaces
  `on-surface`                 `#254E09` Primary text
  `on-surface-muted`           `#546B41` Secondary text
  `border`                     `#D5E3BE` Subtle separators
  `success`                    `#6FAF39` Positive state
  `warning`                    `#E2934F` Warning
  `error`                      `#FD7C69` Error

### Color rule

Green should dominate the experience.

Use secondary colors primarily for semantic data states, not decoration.

------------------------------------------------------------------------

# 5. Dark Theme

The dark mode in the reference is a **true dark green interface**, not a
generic black Material theme.

## Recommended tokens

  Token                                 Value
  ------------------------------- -----------
  `dark-background`                 `#0F150C`
  `dark-surface`                    `#191E14`
  `dark-surface-container`          `#272F20`
  `dark-surface-container-high`     `#3B4631`
  `dark-primary`                    `#9CEC5D`
  `dark-primary-container`          `#74AF42`
  `dark-on-surface`                 `#E6F0DD`
  `dark-on-surface-muted`           `#8BA289`
  `dark-border`                     `#3B4631`

The dark theme should preserve the same layout and component geometry as
light mode.

Do not redesign the interface for dark mode.

------------------------------------------------------------------------

# 6. Typography

Use **Material 3 typography roles** as the semantic foundation.

Recommended font:

**Inter** or **Roboto Flex**

If Material 3 fidelity is prioritized, use **Roboto Flex**.

## Hierarchy

### Page title

-   24--28px
-   650--700 weight
-   compact line height

### Section title

-   16--18px
-   600--650 weight

### Metric value

-   20--28px
-   650--700 weight
-   tabular numbers preferred

### Body

-   13--14px
-   400--500 weight

### Metadata

-   11--12px
-   400--500 weight

### Navigation

-   12--13px
-   500 weight

Avoid oversized SaaS typography.

The reference is information-dense.

------------------------------------------------------------------------

# 7. Shape System

Shape is one of the strongest visual characteristics.

Use a rounded shape scale:

  Token        Radius
  -------- ----------
  `xs`            6px
  `sm`           10px
  `md`           14px
  `lg`           18px
  `xl`           22px
  `hero`     26--30px
  `pill`        999px

## Rules

-   Main dashboard cards: 18--22px
-   Hero card: 24--30px
-   Input/filter: 10--14px
-   Buttons: pill or 10--14px
-   Small badges: pill
-   Charts: 18--22px
-   Tables/list containers: 16--20px

Do not mix many unrelated radii.

------------------------------------------------------------------------

# 8. Layout

## Desktop baseline

Reference viewport:

**1152 × 720**

Use a desktop-first dashboard layout.

### Global structure

``` text
┌──────────────────────────────────────────────────────────────┐
│ Sidebar │ Top Bar / Context                                 │
│         ├────────────────────────────────────────────────────┤
│         │ Page Header                                        │
│         │                                                     │
│         │ Hero / Primary Feature                             │
│         │                                                     │
│         │ Metrics                                             │
│         │                                                     │
│         │ Charts / Analytics                                  │
│         │                                                     │
│         │ Lists / Transactions                               │
└─────────┴────────────────────────────────────────────────────┘
```

## Sidebar

Approximate width:

**64--72px collapsed / 180--210px expanded**

Characteristics:

-   vertically centered navigation
-   compact icon + label
-   very low visual noise
-   selected state uses a soft green container
-   rounded selected background
-   no heavy divider
-   logo at top
-   user/profile utility near bottom

Sidebar should visually feel like part of the background.

------------------------------------------------------------------------

# 9. Header

The top area is compact.

Include:

-   breadcrumb/page context
-   current date or period
-   profile/avatar
-   theme toggle
-   utility actions where required

Avoid large navigation bars.

Header should consume minimal vertical space.

------------------------------------------------------------------------

# 10. Dashboard Composition

The dashboard follows a **modular card grid**.

Typical order:

1.  Greeting / contextual hero
2.  Date or period control
3.  Main balance/financial feature
4.  Key metrics
5.  Main chart
6.  Secondary analytics
7.  Quick insight / activity
8.  Transaction list

The user should understand the most important KPI within the first
viewport.

------------------------------------------------------------------------

# 11. Hero Card

The hero card is one of the signature components.

Characteristics:

-   bright green gradient or tonal green surface
-   very large radius
-   white/dark-green typography depending on theme
-   decorative organic shapes
-   subtle line/outline decorations
-   compact action pills
-   large primary number
-   secondary metadata

### Decorative shapes

Use simple abstract shapes:

-   rounded rectangle outline
-   circle
-   organic arc
-   soft blob
-   thin curved line

These shapes should remain subtle.

Do not turn the dashboard into an illustration-heavy landing page.

------------------------------------------------------------------------

# 12. Metric Cards

Metric cards are compact and highly rounded.

Each card should contain:

``` text
Label
Primary value
Optional delta/status
Small supporting visual
```

Example:

``` text
Pendapatan
Rp 6.702.000

+12.4%
[mini sparkline]
```

Rules:

-   primary value dominates
-   labels are muted
-   icons are small
-   use mini charts where useful
-   avoid excessive iconography
-   maintain equal card heights within a row

------------------------------------------------------------------------

# 13. Charts

Charts are integrated into the UI rather than treated as separate
analytical software.

## Line chart

Characteristics:

-   thin line
-   rounded stroke
-   green primary line
-   minimal axis
-   low-contrast grid
-   no heavy chart frame
-   optional soft area fill

## Bar chart

Characteristics:

-   rounded bar tops
-   compact spacing
-   green primary bars
-   semantic colors only where needed
-   subtle axis labels

## Donut chart

Characteristics:

-   thick donut ring
-   rounded/clean appearance
-   small center label where applicable
-   limited palette
-   legend integrated into card

## Chart principle

Charts should look like part of the design system.

Do not use default Chart.js/Recharts styling.

------------------------------------------------------------------------

# 14. Filters

Filters are compact pills.

Examples:

``` text
[ Hari ini ] [ 7 hari ] [ 30 hari ]
```

or:

``` text
[ Agustus 2026 ▼ ]
```

Use:

-   rounded container
-   subtle surface contrast
-   compact height
-   small typography
-   selected state using primary container

Avoid traditional rectangular dropdown styling.

------------------------------------------------------------------------

# 15. Buttons

Primary button:

-   green filled surface
-   dark/white text depending on contrast
-   pill or 10--14px radius
-   compact height
-   small icon optional

Secondary:

-   tonal surface
-   no heavy outline

Tertiary:

-   transparent
-   primary text

Buttons should feel compact and soft.

------------------------------------------------------------------------

# 16. Tables / Transaction Lists

The reference uses a very soft data-list treatment.

Do not create a conventional enterprise table with hard rows and strong
borders.

Preferred structure:

``` text
Transaction
────────────────────────────────────────────
Icon  Title / category          Amount
      Metadata                  Status
────────────────────────────────────────────
Icon  Title / category          Amount
      Metadata                  Status
```

Use:

-   soft row surfaces
-   subtle separators
-   compact density
-   right-aligned financial values
-   semantic status indicators
-   rounded outer container

------------------------------------------------------------------------

# 17. Settings / Management Screens

The reference includes a settings/management screen with the same visual
language.

Structure:

``` text
Page title
Supporting description

Section card
 ├── Setting row
 ├── Setting row
 ├── Setting row
 └── Setting row
```

Each row:

-   icon
-   label
-   supporting text
-   optional toggle/action
-   edit icon at right

Use large soft containers and consistent vertical rhythm.

------------------------------------------------------------------------

# 18. Motion Design

The interface uses subtle movement.

Motion should communicate hierarchy and state.

## Recommended durations

  Interaction            Duration
  ------------------ ------------
  Hover                120--160ms
  Button state         120--180ms
  Card interaction     180--220ms
  Modal                220--280ms
  Page transition      250--350ms
  Chart update         400--700ms

Use easing similar to Material motion:

-   standard: `cubic-bezier(0.2, 0, 0, 1)`
-   emphasized: `cubic-bezier(0.2, 0, 0, 1)`

Avoid bouncy animation.

------------------------------------------------------------------------

# 19. Micro-interactions

Implement:

### Card hover

``` text
rest
→ slightly brighter surface
→ subtle elevation
→ translateY(-1px)
```

### Button

``` text
rest
→ hover surface shift
→ pressed scale ~0.98
```

### Navigation

Selected item should smoothly transition its background container.

### Charts

Charts can animate when first rendered or when filters change.

### Theme switch

Light/dark theme should transition smoothly.

Avoid full-screen flashy transitions.

------------------------------------------------------------------------

# 20. Material 3 Mapping

Use Material 3 concepts as the component architecture.

  Reference UI       M3 foundation
  ------------------ -------------------------------------
  Primary button     Filled Button
  Secondary action   Tonal Button
  Filter             Assist / Filter Chip
  Date selection     Date Picker / Menu
  Navigation         Navigation Rail / Navigation Drawer
  Dialog             Dialog
  Toggle             Switch
  Status             Assist/Filter Chip
  Input              Outlined / Filled Text Field
  Tooltip            Tooltip
  Notification       Snackbar
  Card               Card / elevated or filled surface

However, customize the visual implementation heavily.

**Do not use default M3 component appearance without applying the design
tokens in this document.**

------------------------------------------------------------------------

# 21. Elevation

Elevation should be extremely subtle.

Preferred approach:

``` css
box-shadow:
  0 1px 2px rgba(...),
  0 4px 12px rgba(...);
```

Use elevation only for:

-   floating menus
-   dropdowns
-   dialogs
-   hover states
-   selected floating elements

Most dashboard cards should rely on **surface contrast + border**.

------------------------------------------------------------------------

# 22. Borders

Borders are subtle.

Recommended:

``` css
border: 1px solid rgba(...)
```

Opacity should be low.

Avoid:

``` text
1px solid #000
1px solid #999
```

Strong borders destroy the soft morphic appearance.

------------------------------------------------------------------------

# 23. Spacing System

Use an 8px base grid.

``` text
4px   micro
8px   xs
12px  sm
16px  md
20px  lg
24px  xl
32px  2xl
40px  3xl
48px  section
```

Typical dashboard gaps:

-   Card gap: 12--16px
-   Section gap: 20--24px
-   Page padding: 20--32px
-   Card internal padding: 16--20px

------------------------------------------------------------------------

# 24. Density

The reference is intentionally dense.

Target:

-   compact header
-   compact sidebar
-   multiple KPI cards visible simultaneously
-   charts visible within first 1--2 scrolls
-   minimal wasted space

Do not use excessive vertical padding.

------------------------------------------------------------------------

# 25. Responsive Behavior

## Desktop ≥ 1200px

Full dashboard grid.

## Tablet 768--1199px

-   sidebar becomes rail
-   cards reduce columns
-   charts may become 1-column
-   maintain card radius

## Mobile \< 768px

-   sidebar becomes bottom/overlay navigation
-   cards become single column
-   charts remain scrollable
-   hero card becomes stacked
-   table becomes list
-   filters become horizontal scroll

Do not simply shrink the desktop UI.

------------------------------------------------------------------------

# 26. Component Architecture

Recommended component hierarchy:

``` text
AppShell
├── Sidebar
├── TopBar
└── PageContainer
    ├── PageHeader
    ├── HeroCard
    ├── FilterBar
    ├── MetricGrid
    │   └── MetricCard
    ├── AnalyticsGrid
    │   ├── LineChartCard
    │   ├── DonutChartCard
    │   └── BarChartCard
    ├── InsightCard
    └── TransactionList
```

Shared components:

``` text
MorphicCard
MorphicSurface
MetricCard
ChartCard
FilterChip
StatusChip
PrimaryButton
SecondaryButton
IconButton
Avatar
DataList
EmptyState
Dialog
Dropdown
Tooltip
```

------------------------------------------------------------------------

# 27. CSS Architecture

Use semantic design tokens.

``` css
:root {
  --color-primary: #6FAF39;
  --color-primary-container: #B8EA86;

  --color-surface: #F0F7E5;
  --color-surface-container: #E5EFD4;
  --color-surface-container-high: #D5E3BE;

  --color-on-surface: #254E09;
  --color-on-surface-muted: #546B41;

  --radius-sm: 10px;
  --radius-md: 14px;
  --radius-lg: 18px;
  --radius-xl: 22px;
  --radius-hero: 28px;

  --space-1: 4px;
  --space-2: 8px;
  --space-3: 12px;
  --space-4: 16px;
  --space-5: 20px;
  --space-6: 24px;
  --space-8: 32px;
}
```

Dark theme should override semantic tokens rather than creating a
separate component system.

------------------------------------------------------------------------

# 28. Iconography

Use a consistent outlined icon family.

Recommended:

-   Material Symbols Rounded
-   18--20px for navigation
-   16--18px for compact actions
-   20--24px for cards

Do not mix:

-   Font Awesome
-   Lucide
-   Heroicons
-   random SVG icon packs

unless there is a specific product requirement.

------------------------------------------------------------------------

# 29. Data Visualization Palette

Primary:

``` text
Green
```

Secondary:

``` text
Orange
Red
Muted Green
```

Recommended semantic mapping:

  Meaning           Color
  ----------------- --------------------------
  Positive          `#6FAF39`
  Positive strong   `#9CEC5D`
  Warning           `#E2934F`
  Error             `#FD7C69`
  Neutral           muted surface/on-surface

Charts should generally use **one dominant color** and a small semantic
palette.

------------------------------------------------------------------------

# 30. Dark Mode Rules

When switching to dark:

-   preserve component geometry
-   preserve spacing
-   preserve radius
-   preserve chart composition
-   preserve hierarchy
-   change semantic surfaces/colors only

Dark mode should feel like the same application at night.

------------------------------------------------------------------------

# 31. Exactness Rules for Vibe Coding

When generating the UI, prioritize in this order:

1.  **Layout geometry**
2.  **Spacing**
3.  **Color system**
4.  **Typography hierarchy**
5.  **Card radius**
6.  **Component density**
7.  **Chart composition**
8.  **Iconography**
9.  **Motion**
10. **Decorative details**

A visually attractive implementation that changes the grid, spacing, or
card geometry is considered incorrect.

------------------------------------------------------------------------

# 32. Anti-Patterns

Do NOT generate:

-   default Material 3 purple theme
-   generic admin dashboard
-   Bootstrap-like cards
-   sharp rectangular tables
-   excessive white backgrounds
-   excessive glass blur
-   excessive gradients
-   excessive shadows
-   oversized typography
-   huge navigation
-   giant hero sections
-   rainbow charts
-   generic Tailwind dashboard aesthetics
-   random border radii
-   inconsistent icon sets

------------------------------------------------------------------------

# 33. Vibe Coding Master Prompt

Use this as the system-level design instruction when generating the
application:

``` text
Build the web application using Material Design 3 as the component and interaction foundation, but create a highly customized visual system based on the provided reference.

The visual direction is Modern Enterprise Morphic UI: soft layered surfaces, large rounded cards, pale green monochromatic surfaces, bright lime green primary actions, subtle borders, extremely soft elevation, compact enterprise dashboard density, and premium fintech/productivity aesthetics.

Do NOT generate a generic Material 3 dashboard.

Use:
- Material 3 semantic color roles
- Material 3 typography roles
- Material Symbols Rounded
- 8px spacing grid
- 18–22px dashboard card radius
- 24–30px hero radius
- compact 12–14px body typography
- soft surface contrast instead of heavy shadows
- green-first semantic palette
- true dark-green dark mode
- compact navigation
- modular dashboard grid
- rounded chart containers
- subtle motion and Material-like easing

Light theme:
background #F0F7E5
surface-container #E5EFD4
surface-container-high #D5E3BE
primary #6FAF39
primary-container #B8EA86
on-surface #254E09
muted text #546B41

Dark theme:
background #0F150C
surface #191E14
surface-container #272F20
surface-container-high #3B4631
primary #9CEC5D
on-surface #E6F0DD
muted text #8BA289

The interface must feel soft, calm, premium, information-dense and highly polished.

Cards must visually merge into the page through subtle tonal contrast.

Charts must look native to the design system rather than default chart-library output.

Use the same component geometry between light and dark themes.

Prioritize pixel-level consistency in:
1. layout
2. spacing
3. typography
4. colors
5. radius
6. component density
7. charts
8. motion

Avoid generic SaaS templates, default Material styling, excessive glassmorphism, excessive shadows, excessive gradients, and oversized typography.
```

------------------------------------------------------------------------

# 34. Implementation Acceptance Criteria

The implementation is visually acceptable only when:

-   [ ] Material 3 component architecture is present
-   [ ] Green semantic palette is consistently applied
-   [ ] Light and dark themes share the same geometry
-   [ ] Dashboard cards use consistent 18--22px rounding
-   [ ] Hero cards use approximately 24--30px rounding
-   [ ] Surface contrast replaces heavy borders
-   [ ] Navigation is compact
-   [ ] Dashboard density matches the reference
-   [ ] Charts use the custom palette
-   [ ] Typography is compact and hierarchical
-   [ ] Buttons and filters use rounded geometry
-   [ ] Tables/lists use soft surfaces
-   [ ] Motion is subtle and fast
-   [ ] No generic Material purple/blue theme appears
-   [ ] No generic Bootstrap/admin-dashboard appearance appears
-   [ ] No excessive glass blur appears
-   [ ] No excessive shadows appear
-   [ ] Responsive layouts preserve the design language

------------------------------------------------------------------------

# 35. Final Design Definition

The design can be described in one sentence as:

**"A Material 3-based enterprise dashboard with a soft morphic visual
language, green tonal surfaces, large rounded cards, compact information
density, subtle elevation, and premium fintech-style data
visualization."**

For AI/Vibe Coding, the most important phrase is:

**Material 3 + Soft Morphic Enterprise UI + Green Tonal Design System +
Dense Fintech Dashboard + Light/Dark Theme**
