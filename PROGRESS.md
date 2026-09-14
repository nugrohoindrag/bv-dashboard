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
- ✅ design-tokens/tokens.json (DS §2) · ⏳ generate tokens.css
- ✅ DB migrations 00001–00008: platform, IAM, property/locations (ltree), audit, attachments, asset, operations (task engine), engineering/security/housekeeping, tenantservice/notification/search/sync, RLS semua tabel
- ✅ api/platform: config, apperr, authctx, db (RLS SET LOCAL), ids, httpx (problem+json, cursor), storage (S3 presign + memory), jobs (River outbox + periodic), events
- ✅ api/iam: login/refresh/logout (argon2id, JWT EdDSA, rotating refresh + reuse detection, lockout), users/roles/permissions/teams/devices, authz middleware (Require, CanOnProperty), role template seed
- ✅ api/property: organizations, locations (ltree path, tree, typed aliases), tenants/occupants, Area QR
- ✅ api/audit: activities (timeline) + audit_logs ({User} {Action} {Object})
- ✅ api/attachments: presign → PUT → confirm, GPS status, signed GET, idempotent client_attachment_id
- ✅ AT-010 organization isolation test (integration test, koneksi bv_app + RLS)
- ⏳ web shell: AppShell (dark sidebar), auth, property switcher, settings

## P0.2 Core Operations
- ✅ bvctl: migrate (goose+River), seed (--demo: org Graha Pangeran, 13 user, 4 team, hierarki, tenant), keygen — **terverifikasi jalan di Postgres 17 lokal**
- ✅ Workflow state machine (data-driven, TD-001 `new`), guards IsAssignee/EvidenceSatisfied/RequiresAssignee — unit test 100% transisi
- ✅ tasks, work_orders (create/update/assign/bulk-assign/transition + hooks), assignments history, checklist templates (draft/published/archived, versioned snapshot) & runs (answer, Not OK → Finding), comments, object_links (bidirectional)
- ✅ Business ID generator (WO-/TSK-/SR-/INC-/PM-/INS-/FND-/AST-{CAT}-)
- ✅ SLA policies (org/property, business hours OD-006) + tracking, overdue sweep, SLA sweep (risk/breach/escalation), due-soon notify
- ⏳ web: Tasks & Work Orders list/detail/actions, ChecklistRunner, PhotoEvidenceUploader, ActivityTimeline
- ✅ AT-001, AT-002, AT-003 lulus (integration test) + RBAC negative + refresh rotation/reuse detection
- ✅ Findings (FND-, resolve/close, → WO / Task rework / Incident) & Incidents (INC-, report→assign→resolve→close, → WO)

## P0.3 Engineering
- ✅ equipment (kategori/tipe), assets (AST-{CAT}-{SEQ}, spesifikasi value/unit), qr_codes + /qr/{code}/resolve (validasi org+permission) + rotate
- ✅ maintenance_plans (draft/published/archived), schedule generator (horizon 60 hari, idempotent, timezone property), maintenance WO creation (lead time, idempotent), skip schedule
- ✅ inspections (INS-, task_type=inspection, result pass/fail dari checklist) → finding → WO
- ✅ asset history (WO/task/schedule/activity gabungan) + hook maintenance WO ↔ schedule status
- ⏳ web: Assets, Equipment, PM, Corrective, Inspections
- ✅ AT-004 lulus (integration test)

## P0.4 Security + Housekeeping
- ✅ patrol_routes (urutan checkpoint), checkpoints (QR), patrol_schedules → generator patrol task (7 hari, idempotent), checkpoint scan (QR/manual, GPS, idempotent client_scan_id), Missed (TD-007: auto saat complete / manual), hook BeforeComplete
- ✅ incidents (INC-) report → assign → resolve → close → WO; Finding → Incident
- ✅ cleaning_schedules → generator cleaning task (7 hari), adhoc cleaning, housekeeping_inspections (INS-, hasil pass/fail/partial → inspection_status cleaning task), Not OK → Finding → rework Task / WO
- ⏳ web: Security Patrol/Incidents, Housekeeping Cleaning/Schedule/Inspections
- ✅ AT-005, AT-006, AT-007 lulus (integration test)

## P0.5 Service Request + Notification
- 🔄 sedang dikerjakan
- ⏳ service_requests lifecycle, SR ↔ Task/WO bidirectional links, resolution hook
- ⏳ notification rules (PRD §17.1), inbox, push adapter (FCM), deep link, device tokens
- ⏳ web: Service Requests, NotificationInbox
- ⏳ AT-008

## P0.6 Overview + Pilot Hardening
- ⏳ overview endpoints: today, attention-required, todays-operations, team-workload, pm-due, tenant-requests, building-state
- ⏳ search (tsvector + pg_trgm), exports CSV/XLSX
- ⏳ sync: work-bundle, mutations (C1–C10), conflicts
- ⏳ web Overview page (TodayCounter, AttentionRequiredList, ...)
- ⏳ contracts/sync-api.md untuk repo mobile (work-bundle, mutations, C1–C10)
- ⏳ infra: docker-compose, Caddy, backup (pgBackRest), observability, CI workflows
- ⏳ docs: ADR, runbooks (restore, rollback), conflict rules, training material, acceptance test mapping
- ⏳ AT-009

## Catatan
- (kosong)
