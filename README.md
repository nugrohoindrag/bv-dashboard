# BuildingVision — Building Operations Platform (P0)

Monorepo: `api/` (Go 1.26 modular monolith), `web/` (React 19 SPA), `contracts/` (OpenAPI, permissions, status-map — juga dipakai repo mobile),
`design-tokens/`, `infra/` (Docker Compose, Caddy, pgBackRest, observability), `docs/` (ADR, runbook, ERD, conflict rules, training).
Mobile (Flutter) berada di repo terpisah dan mengonsumsi `contracts/`.

Acuan: PRD P0 v1.0 · Technical Architecture v1.0 · Dashboard Design System v1.0 · Naming Convention v1.0. Progres: [PROGRESS.md](PROGRESS.md).

## Mulai cepat (dev lokal)

```bash
docker compose -f infra/docker-compose.dev.yml up -d           # postgres:5432, minio:9000, mailpit:8025
cd api
go run ./cmd/bvctl migrate                                     # goose + River (BV_ADMIN_DATABASE_URL opsional)
go run ./cmd/bvctl seed --demo                                 # org demo "Graha Pangeran", 13 user, data operasional
go run ./cmd/api                                               # :8080   (BV_METRICS_ADDR=:9091 untuk /metrics)
go run ./cmd/worker                                            # job queue, sweep, generator PM/patrol/cleaning
cd ../web && npm ci --legacy-peer-deps && npm run dev          # http://localhost:5173 (proxy /api → :8080)
```

Login demo: `admin@example.com` / `Admin12345!` (Org Admin) · `pm@demo.buildingvision.id`, `bm@…`, `ops@…`, `eng.manager@…`, `eng.spv@…`,
`budi@…` (technician), `sec.manager@…`, `sec.spv@…`, `wawan@…` (officer), `hk.manager@…`, `hk.spv@…`, `siti@…` (staff) / `Demo12345!`.

DB dev: `postgres://postgres:postgres@localhost:5432/buildingvision`; aplikasi memakai role `bv_app` (RLS aktif), worker `bv_worker`.

## Perintah penting

| Area | Perintah |
|---|---|
| API test (unit + integrasi AT-001..AT-010, RLS) | `cd api && go test ./...` |
| OpenAPI regenerate | `cd api && go run ./cmd/bvctl openapi ../contracts/openapi/v1.yaml` |
| Kunci JWT | `go run ./cmd/bvctl keygen` |
| Reindex search / import CSV | `bvctl reindex` · `bvctl import --org <slug> --type assets --file x.csv --dry-run` |
| Web typecheck / test / build | `npm run typecheck` · `npm test` · `npm run build` |
| Generate token/status-map (web + Dart) | `npm run gen` |
| Produksi | lihat [docs/runbooks/deploy.md](docs/runbooks/deploy.md) |

## Peta dokumen

- Arsitektur & keputusan: [docs/adr/](docs/adr/) · ERD: [docs/erd.md](docs/erd.md)
- Offline sync (mobile): [contracts/sync-api.md](contracts/sync-api.md) · aturan konflik C1–C10: [docs/conflict-rules.md](docs/conflict-rules.md)
- Runbook: [deploy](docs/runbooks/deploy.md) · [backup & restore](docs/runbooks/backup-restore.md) · [rollback](docs/runbooks/rollback.md) · [incident/ops harian](docs/runbooks/operations.md)
- Acceptance test mapping: [docs/acceptance-tests.md](docs/acceptance-tests.md) · Materi training: [docs/training/](docs/training/)
- Kontrak: [contracts/openapi/v1.yaml](contracts/openapi/v1.yaml) · [contracts/permissions.yaml](contracts/permissions.yaml) · [contracts/status-map.yaml](contracts/status-map.yaml)

## Konvensi

- Bahasa UI/dokumen Indonesia; object kanonik (Work Order, Task, Service Request, Incident, Finding, Asset) tidak diterjemahkan (NC §44).
- Semua transisi status lewat Task Engine (`internal/operations/workflow`); aksi yang tersedia diambil dari `allowed_actions` server.
- `contracts/permissions.yaml` harus identik dengan `api/internal/iam/catalog/permissions.yaml` (dicek CI).
- Migrasi backward-compatible; rollback aplikasi = deploy image sebelumnya (lihat runbook rollback).
