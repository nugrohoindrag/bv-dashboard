# BuildingVision P0 — Progress List

> Diperbarui otomatis oleh Claude selama pengerjaan. Legend: ✅ selesai · 🔄 sedang dikerjakan · ⏳ belum · ⚠️ catatan/blokir

Referensi: PRD P0 v1.0 · Technical Architecture v1.0 · Dashboard Design System v1.0 · Naming Convention v1.0

## 0. Setup
- ✅ Baca 4 dokumen acuan
- ✅ Cek toolchain (Go 1.26 ✓, Node 22 ✓, Docker ✓)
- ✅ Keputusan user: **mobile = repo terpisah nanti**; monorepo ini = api + web + contracts + infra + docs. Kontrak untuk mobile (OpenAPI, status-map, tokens, sync C1–C10) tetap disiapkan di contracts/
- ✅ Monorepo skeleton (TAD §13)

## P0.1 Foundation
- ✅ contracts/permissions.yaml (PRD §18, NC §61)
- ✅ contracts/status-map.yaml (DS §2.2)
- ✅ design-tokens/tokens.json (DS §2) · ✅ generate tokens.css + status-map.ts + i18n status + Dart untuk mobile (`npm run gen`)
- ✅ DB migrations 00001–00008: platform, IAM, property/locations (ltree), audit, attachments, asset, operations (task engine), engineering/security/housekeeping, tenantservice/notification/search/sync, RLS semua tabel
- ✅ api/platform: config, apperr, authctx, db (RLS SET LOCAL), ids, httpx (problem+json, cursor), storage (S3 presign + memory), jobs (River outbox + periodic), events
- ✅ api/iam: login/refresh/logout (argon2id, JWT EdDSA, rotating refresh + reuse detection, lockout), users/roles/permissions/teams/devices, authz middleware (Require, CanOnProperty), role template seed
- ✅ api/property: organizations, locations (ltree path, tree, typed aliases), tenants/occupants, Area QR
- ✅ api/audit: activities (timeline) + audit_logs ({User} {Action} {Object})
- ✅ api/attachments: presign → PUT → confirm, GPS status, signed GET, idempotent client_attachment_id
- ✅ AT-010 organization isolation test (integration test, koneksi bv_app + RLS)
- ✅ web shell: AppShell (sidebar 260px gelap, header 64px, Property Switcher, Global Search ⌘K, Inbox, avatar), auth (login, refresh cookie, can()), i18n id/en, tokens Tailwind v4, primitives (Radix), DataGrid/FilterBar (filter di URL), pickers (lokasi tree, aset, team, user)
- ✅ web settings: Organization (+public intake), Users (role per property, team, reset password), Roles (matriks permission), Teams, Checklist Templates (builder), SLA Policies (matriks object×priority, override per property), Master Data, Notifications (inbox + preferensi), Sync Conflicts (tinjau), Audit Log (before/after)

## P0.2 Core Operations
- ✅ bvctl: migrate (goose+River), seed (--demo: org Graha Pangeran, 13 user, 4 team, hierarki, tenant), keygen — **terverifikasi jalan di Postgres 17 lokal**
- ✅ Workflow state machine (data-driven, TD-001 `new`), guards IsAssignee/EvidenceSatisfied/RequiresAssignee — unit test 100% transisi
- ✅ tasks, work_orders (create/update/assign/bulk-assign/transition + hooks), assignments history, checklist templates (draft/published/archived, versioned snapshot) & runs (answer, Not OK → Finding), comments, object_links (bidirectional)
- ✅ Business ID generator (WO-/TSK-/SR-/INC-/PM-/INS-/FND-/AST-{CAT}-)
- ✅ SLA policies (org/property, business hours OD-006) + tracking, overdue sweep, SLA sweep (risk/breach/escalation), due-soon notify
- ✅ web: Tasks & Work Orders list (FilterBar, preset, bulk assign, export) + detail (tab Detail/Checklist/Evidence/Activity/Terkait, aksi dari allowed_actions, ReasonDialog, edit If-Match), ChecklistRunner, PhotoEvidenceUploader (kompresi ≤1600px, presign→PUT→confirm), ActivityTimeline, Findings list/detail (→ WO / Incident), Incidents list/detail (→ WO)
- ✅ AT-001, AT-002, AT-003 lulus (integration test) + RBAC negative + refresh rotation/reuse detection
- ✅ Findings (FND-, resolve/close, → WO / Task rework / Incident) & Incidents (INC-, report→assign→resolve→close, → WO)

## P0.3 Engineering
- ✅ equipment (kategori/tipe), assets (AST-{CAT}-{SEQ}, spesifikasi value/unit), qr_codes + /qr/{code}/resolve (validasi org+permission) + rotate
- ✅ maintenance_plans (draft/published/archived), schedule generator (horizon 60 hari, idempotent, timezone property), maintenance WO creation (lead time, idempotent), skip schedule
- ✅ inspections (INS-, task_type=inspection, result pass/fail dari checklist) → finding → WO
- ✅ asset history (WO/task/schedule/activity gabungan) + hook maintenance WO ↔ schedule status
- ✅ web: Assets (list/detail/QR rotate/riwayat/form), Equipment, Preventive Maintenance (jadwal + skip + run-due, plan builder + publish/generate), Corrective, Inspections (POST /inspections → INS-), Asset History
- ✅ AT-004 lulus (integration test)

## P0.4 Security + Housekeeping
- ✅ patrol_routes (urutan checkpoint), checkpoints (QR), patrol_schedules → generator patrol task (7 hari, idempotent), checkpoint scan (QR/manual, GPS, idempotent client_scan_id), Missed (TD-007: auto saat complete / manual), hook BeforeComplete
- ✅ incidents (INC-) report → assign → resolve → close → WO; Finding → Incident
- ✅ cleaning_schedules → generator cleaning task (7 hari), adhoc cleaning, housekeeping_inspections (INS-, hasil pass/fail/partial → inspection_status cleaning task), Not OK → Finding → rework Task / WO
- ✅ web: Security Patrol (tasks + checkpoint scan status/manual/missed, rute builder, checkpoint, jadwal + generate, ad-hoc), Security Incidents, Housekeeping Cleaning, Jadwal Cleaning (+ ad-hoc, generate), Housekeeping Inspections (dari detail cleaning task)
- ✅ AT-005, AT-006, AT-007 lulus (integration test)

## P0.5 Service Request + Notification
- ✅ service_requests (SR-, kategori → default priority/team, tenant → lokasi unit), lifecycle New→Acknowledged→Assigned→In Progress→Waiting for Tenant→Resolved→Closed, SR ↔ Task/WO bidirectional links, hook: WO/Task closed ⇒ SR resolved, guard resolve saat work masih open
- ✅ notification rules (seluruh tabel PRD §17.1 + sync.conflict + export.ready), resolver (assignee / team supervisor / property domain supervisor / requester), dedup, inbox API (list/unread/read/read-all/preferences), push job + FCM HTTP v1 adapter, deep link
- ✅ search (tsvector + pg_trgm, filter permission per object type), exports CSV/XLSX (job → storage → signed URL + notifikasi)
- ✅ worker binary (River): domain_event dispatch, push, attachment thumbnails (EXIF strip), search index, export, sweeps periodic, generator PM/patrol/cleaning
- ✅ web: Service Requests list/detail (SLA response/resolution, → WO dari SR, wait_tenant), Tenants (drawer: unit, occupant, SR), NotificationInbox (popover + halaman penuh + preferensi)
- ✅ AT-008 lulus (integration test) + notifikasi SR received / WO assigned / WO completed (supervisor & requester) terverifikasi

## P0.6 Overview + Pilot Hardening
- ✅ overview endpoints: today (6 counter + breakdown), attention-required (severity→umur, CTA dari permission, termasuk sync conflict), todays-operations per domain, team-workload, pm-due, tenant-requests, building-state per Building/Tower; cache 30 dtk
- ✅ (dikerjakan di P0.5) search + exports
- ✅ sync: work-bundle (today + open overdue + referensi + removed), mutations (seq per device/object, idempotent, C1–C10, evidence dipertahankan, clock skew), conflicts list/acknowledge + notifikasi supervisor
- ✅ backend pelengkap: Idempotency-Key middleware (TAD §6.4), public SR intake `/public/v1` + feature flag + intake key (OD-001), seed demo operasional (asset, template, PM, patrol, cleaning, WO/SR/incident), bvctl reindex + import CSV dry-run (OD-008), OpenAPI 3.1 ter-generate (`bvctl openapi`, 195 path) di contracts/openapi/v1.yaml
- ✅ web Overview page (6 TodayCounter + breakdown, Attention Required filter domain + aksi cepat, Tenant Requests, Today's Operations per domain, PM Due 7 hari, Team Workload, Building State) — panel dimuat independen
- ✅ web build (`npm run build` ✓, `tsc` strict ✓, vitest 10 test ✓: StatusBadge kontrak status-map, format tz)
- ⏳ contracts/sync-api.md untuk repo mobile (work-bundle, mutations, C1–C10)
- ⏳ infra: docker-compose, Caddy, backup (pgBackRest), observability, CI workflows
- ⏳ docs: ADR, runbooks (restore, rollback), conflict rules, training material, acceptance test mapping
- ✅ AT-009 lulus (integration test C1, C3, C4, C6, C8, C9, C10) + Overview test (Today, Attention, Today's Ops, Building State, Tenant Requests, Team Workload)

## Catatan
- (kosong)
