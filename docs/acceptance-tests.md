# Pemetaan Acceptance Test P0 (PRD §35) → implementasi & bukti

Semua AT dijalankan otomatis (`cd api && go test ./...`, DB Postgres lokal; CI job `api`) memakai koneksi role `bv_app` (RLS aktif). UI web dicek manual dengan langkah di kolom "Verifikasi manual".

| AT | Skenario | Test otomatis | Endpoint/kode utama | Verifikasi manual (web) |
|---|---|---|---|---|
| AT-001 | Create Work Order → nomor unik, status awal, activity | `TestP0CoreWorkflows` (integration_test.go) | `POST /work-orders`, `ids.NextYearly`, workflow `new` (TD-001) | Operations → Work Orders → Buat Work Order; lihat tab Activity |
| AT-002 | Technician Start → Checklist → Photo → Complete; evidence; notifikasi supervisor | `TestP0CoreWorkflows` + `TestServiceRequestAndNotifications` | `/start`, `/checklist-run-items/{id}/answer`, `/attachments/presign|confirm`, `/complete` guard EvidenceSatisfied; rule `work_order.completed` | Login `budi@…`: detail WO → Mulai → Checklist → unggah foto → Selesaikan; login `eng.spv@…` lihat Inbox |
| AT-003 | Supervisor Close; close event tercatat | `TestP0CoreWorkflows` | `/close` (permission `operations.work_orders.close`), activity `closed` | `eng.spv@…` → WO Completed → Tutup |
| AT-004 | PM Generation: plan aktif → schedule due → maintenance WO | `TestEngineeringPreventiveMaintenance` | `/maintenance-plans/{id}/publish|generate`, `CreateDueWorkOrders` (lead time, idempotent) | Engineering → Preventive Maintenance → Plan → Publikasikan → tab Jadwal → "Buat WO jadwal due" |
| AT-005 | Patrol: start, scan semua checkpoint, complete | `TestSecurityPatrol` | `/patrol-tasks/{id}/scans`, hook BeforeComplete auto-missed (TD-007) | Security → Patrol → Checkpoint (manual/missed) → Selesaikan |
| AT-006 | Patrol Finding dengan foto → notifikasi → Incident/WO | `TestSecurityPatrol` | `POST /findings` (source patrol), rule `finding.created`, `/findings/{id}/work-orders|incidents` | Findings → detail → Buat Work Order dari Finding / Buat Incident |
| AT-007 | Cleaning Inspection: checklist result, Finding bila tidak sesuai | `TestHousekeepingCleaningInspection` | `POST /housekeeping-inspections`, answer `not_ok` + `create_finding` → rework Task | Housekeeping → Cleaning → detail cleaning task → Buat Inspeksi Housekeeping |
| AT-008 | Service Request: triage/assign → Task/WO, link dua arah, status mengikuti resolusi | `TestServiceRequestAndNotifications` | `POST /service-requests`, `/assign`, `/work-orders`, hook WO closed ⇒ SR resolved | Operations → Service Requests → Buat SR → Tugaskan → Buat Work Order → tab Terkait |
| AT-009 | Offline: bundle cached, mutasi Pending Sync, sinkron otomatis; C1–C10 | `TestOfflineSync` (C1, C3, C4, C6, C8, C9, C10) | `GET /sync/work-bundle`, `POST /sync/mutations` | Settings → Sync Conflicts (tinjau); mobile: repo terpisah |
| AT-010 | Isolasi organisasi | `TestP0CoreWorkflows` (bagian isolation) | RLS `app.organization_id`, 404/403 lintas org | Login org lain (seed 2 org) — object org A tidak terlihat |

Tambahan yang diuji otomatis: RBAC negatif (technician tidak bisa close), rotasi refresh token + deteksi reuse (`TestAuthRefreshRotation`), Overview 7 panel (`TestOverview`), unit test state machine 100% transisi (`internal/operations/workflow`), web: `StatusBadge` kontrak status-map & format timezone (vitest).

## Release criteria terkait infra (TAD)
- #25 Backup & restore: `docs/runbooks/backup-restore.md` + `infra/scripts/restore-drill.sh` (drill wajib sebelum UAT — hasil dicatat di tabel runbook).
- #26 Observability: `/metrics` api/worker, dashboard Grafana, alert `infra/prometheus/alerts.yml`.
- #27 Runbook deploy/rollback: `docs/runbooks/`.
