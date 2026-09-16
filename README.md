# BuildingVision

Platform operasional gedung (Hotel, Apartment, Office): dashboard manajemen gedung, API, website publik, serta kontrak untuk aplikasi mobile Staff, Tenant, dan BVRooms (customer booking).

## Struktur repo

| Folder | Isi |
|---|---|
| `api/` | Backend Go (modular monolith): REST API `/api/v1`, worker job queue (River), CLI `bvctl` |
| `web/` | Dashboard web (React 19 + Vite + Tailwind v4) |
| `website/` | Website publik (statis, prerender) |
| `packages/ui/` | Design system komponen bersama (dipakai `web/` dan `website/`) |
| `contracts/` | OpenAPI, katalog permission, status-map (dikonsumsi repo mobile) |
| `design-tokens/` | Token desain (warna, tipografi) |
| `infra/` | Docker Compose (dev & produksi), Caddy, backup (pgBackRest), observability, CI/CD |
| `docs/` | ADR, ERD, runbook (deploy, backup/restore, rollback, operasi), acceptance test, training, panduan demo |

## Prasyarat

- Go 1.26+, Node.js 20+ (npm), Docker (untuk Postgres 17, MinIO, Mailpit di dev)
- Postgres 17 (ekstensi `ltree`, `pg_trgm`, `btree_gist`)

## Menjalankan di lokal

```bash
docker compose -f infra/docker-compose.dev.yml up -d      # postgres:5432, minio:9000, mailpit:8025

cd api
go run ./cmd/bvctl migrate                                # migrasi skema (goose + River)
go run ./cmd/bvctl seed                                   # katalog permission & role sistem
go run ./cmd/api                                          # API di :8080
go run ./cmd/worker                                       # worker (job, sweep, generator jadwal)

cd ../web && npm ci --legacy-peer-deps && npm run dev     # dashboard :5173 (proxy /api → :8080)
cd ../website && npm ci --legacy-peer-deps && npm run dev # website :5175
```

Konfigurasi lewat environment variable — daftar lengkap dan nilai contoh ada di [`infra/.env.example`](infra/.env.example). Aplikasi memakai role database `bv_app` (Row Level Security aktif); worker memakai `bv_worker`.

Data demo (tiga environment Hotel/Apartment/Office) dapat dibuat dengan `go run ./cmd/bvctl demo seed` atau dari dashboard menu **Settings → Demo Data** (role `admin_internal`). Panduan: [`docs/demo/BuildingVision-Demo-Guide.md`](docs/demo/BuildingVision-Demo-Guide.md).

## Perintah penting

| Area | Perintah |
|---|---|
| Test API (unit + integrasi, butuh Postgres lokal `buildingvision_test`) | `cd api && go test ./...` |
| Regenerate OpenAPI | `cd api && go run ./cmd/bvctl openapi ../contracts/openapi/v1.yaml` |
| Kunci JWT / VAPID | `go run ./cmd/bvctl keygen` · `go run ./cmd/bvctl vapid-keygen` |
| Verifikasi data demo | `go run ./cmd/bvctl demo verify` |
| Web: typecheck / test / build | `cd web && npm run typecheck` · `npm test` · `npm run build` |
| Website: build / cek copy / gambar | `cd website && npm run build` · `npm run check:copy` · `npm run images` |
| Design system: cek ikon & font | `cd packages/ui && npm run ds:check` · `npm run fonts:vendor` |
| Generate token & status-map (web + Dart) | `cd web && npm run gen` |

## Deploy

Ikuti [`docs/runbooks/deploy.md`](docs/runbooks/deploy.md) (Docker Compose produksi di `infra/`, Caddy sebagai reverse proxy, migrasi lewat `bvctl migrate`). Runbook lain: [backup & restore](docs/runbooks/backup-restore.md), [rollback](docs/runbooks/rollback.md), [operasi harian](docs/runbooks/operations.md).

## Dokumentasi

- Arsitektur & keputusan: [`docs/adr/`](docs/adr/) · ERD: [`docs/erd.md`](docs/erd.md)
- Kontrak API: [`contracts/openapi/v1.yaml`](contracts/openapi/v1.yaml) · permission: [`contracts/permissions.yaml`](contracts/permissions.yaml) · status: [`contracts/status-map.yaml`](contracts/status-map.yaml) · sync offline mobile: [`contracts/sync-api.md`](contracts/sync-api.md)
- Pedoman UI: [`web/docs/DESIGN-SYSTEM-GUIDELINE.md`](web/docs/DESIGN-SYSTEM-GUIDELINE.md) · website: [`website/README.md`](website/README.md)
- Acceptance test: [`docs/acceptance-tests.md`](docs/acceptance-tests.md) · training: [`docs/training/`](docs/training/)

## Konvensi

- Semua transisi status lewat Task Engine (`api/internal/operations/workflow`); aksi yang tersedia diambil dari `allowed_actions` server.
- `contracts/permissions.yaml` harus identik dengan `api/internal/iam/catalog/permissions.yaml` (dicek CI).
- Migrasi backward-compatible; rollback aplikasi = deploy image sebelumnya.
- UI web mengikuti design system di `packages/ui` (tidak ada warna literal, ikon Material Symbols Rounded, tema terang & gelap).
