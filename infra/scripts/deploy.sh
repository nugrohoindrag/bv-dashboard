#!/usr/bin/env bash
# Deploy ke VPS (dipanggil GitHub Actions lewat SSH atau manual): pull image versi baru → migrate → rollout api (start-first) → worker → web.
# Pemakaian: BV_VERSION=v0.1.3 ./scripts/deploy.sh   (dari folder infra/, .env sudah terisi)
set -euo pipefail
cd "$(dirname "$0")/.."
: "${BV_VERSION:?BV_VERSION wajib (tag image, mis. v0.1.3)}"
export BV_VERSION
echo "== deploy BuildingVision $BV_VERSION"
docker compose pull api worker web
echo "== migrate (job terpisah, backward-compatible)"
docker compose run --rm migrate
echo "== rollout api (2 replika, start-first)"
docker compose up -d --no-deps --scale api=2 api
for i in $(seq 1 30); do
  if docker compose ps api | grep -q "healthy"; then break; fi; sleep 3
done
docker compose ps api | grep -q "healthy" || { echo "api tidak healthy — rollback: BV_VERSION=<versi sebelumnya> ./scripts/deploy.sh"; exit 1; }
echo "== rollout worker & web"
docker compose up -d --no-deps worker
docker compose up --no-deps web
docker compose exec caddy caddy reload --config /etc/caddy/Caddyfile || true
echo "== selesai: $BV_VERSION"
