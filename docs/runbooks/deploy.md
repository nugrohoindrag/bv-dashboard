# Runbook — Deploy (staging & production)

Topologi: `infra/docker-compose.yml` (ADR-009). Server: `/opt/buildingvision` (clone repo) + `infra/.env` (dari `infra/.env.example`).

## Persiapan server (sekali)
1. VPS Ubuntu 24.04, Docker Engine + Compose v2, firewall hanya 22/80/443, snapshot harian di provider.
2. `git clone https://github.com/nugrohoindrag/bv-dashboard /opt/buildingvision && cd /opt/buildingvision/infra && cp .env.example .env`.
3. Isi `.env`: domain, `POSTGRES_PASSWORD`, kunci JWT (`go run ./cmd/bvctl keygen` di mesin dev → PEM ke `BV_JWT_PRIVATE_KEY`/`BV_JWT_PUBLIC_KEY`), kredensial S3/R2 untuk **pgBackRest repo di lokasi berbeda** (`infra/pgbackrest/pgbackrest.conf`), token Telegram alert.
4. `docker compose up -d postgres minio minio-init && docker compose run --rm migrate` lalu **wajib** `BV_APP_DB_PASSWORD=… BV_WORKER_DB_PASSWORD=… ./scripts/set-db-passwords.sh` (migrasi membuat role dengan password dev).
5. Seed org pertama: `docker compose run --rm -e BV_DATABASE_URL=postgres://postgres:$POSTGRES_PASSWORD@postgres:5432/buildingvision api /app/bvctl seed` (tanpa `--demo` di produksi), lalu buat Org Admin & property lewat Settings.
6. `docker compose up -d` — stack inti (caddy, api ×2, worker, web, website, postgres, minio). Caddy mengambil sertifikat Let's Encrypt otomatis untuk `BV_DOMAIN` dan `BV_WEBSITE_DOMAIN` (+ `www.`). Observability & backup opsional: `docker compose --profile observability --profile backup up -d` (butuh RAM ≥ 8 GB / repo S3 pgBackRest).
7. Inisialisasi stanza backup: `docker compose exec pgbackrest pgbackrest --stanza=bv stanza-create && docker compose exec pgbackrest pgbackrest --stanza=bv check`.
8. Grafana: `https://<domain>/grafana` (ganti password admin), dashboard "BuildingVision — Operasional".

## Rilis rutin
- Staging: otomatis setelah CI hijau di default branch (`.github/workflows/deploy.yml`, image tag `latest`).
- Production: buat tag `vX.Y.Z` → CI build image → job `production` menunggu approval reviewer (GitHub Environment) → SSH `BV_VERSION=vX.Y.Z ./scripts/deploy.sh`.
- Urutan `deploy.sh`: pull image → `migrate` (job terpisah, backward-compatible) → api rollout start-first (2 replika, health `/ready`) → worker → web dist → Caddy reload. Zero-downtime untuk API; worker restart ≤ 30 dtk (job River di-retry).

## Verifikasi pasca-deploy (≤ 5 menit)
1. `curl -fsS https://<domain>/health` → `min_supported_app_version`; `/ready` 200.
2. Login web, buka Overview (semua panel terisi), buat 1 Task uji lalu cancel.
3. Grafana: error rate < 2%, p95 < 2 dtk, sweep lag < 5 menit, queue depth turun.
4. `docker compose logs --since 5m api worker | grep -i error` kosong.

## Variabel penting
`BV_ENV=production` mewajibkan kunci JWT; `BV_COOKIE_SECURE=true`; `BV_CORS_ORIGINS` = origin web; `BV_QR_BASE_URL=https://<domain>/q/` (QR aset/checkpoint diarahkan ke SPA); `BV_MIN_MOBILE_APP_VERSION` untuk memaksa update mobile.
