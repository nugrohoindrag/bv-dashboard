#!/usr/bin/env bash
# Restore drill ke instance sementara (TAD §11.6, RTO ≤ 4 jam): restore backup terbaru (atau PITR --target) ke container postgres-restore,
# jalankan smoke query, lalu laporkan durasi. Pemakaian: ./scripts/restore-drill.sh [--target "2026-03-01 10:00:00+07"]
set -euo pipefail
cd "$(dirname "$0")/.."
TARGET_OPT=""
if [ "${1:-}" = "--target" ]; then TARGET_OPT="--type=time --target=\"$2\" --target-action=promote"; fi
START=$(date +%s)
docker volume create bv-restore-data >/dev/null
echo "== restore ke volume bv-restore-data"
docker run --rm --network buildingvision_default -v bv-restore-data:/var/lib/postgresql/data \
  -v "$PWD/pgbackrest/pgbackrest.conf:/etc/pgbackrest/pgbackrest.conf:ro" woblerr/pgbackrest:2.54 \
  sh -c "pgbackrest --stanza=bv --delta $TARGET_OPT restore"
echo "== start postgres-restore"
docker rm -f bv-postgres-restore >/dev/null 2>&1 || true
docker run -d --name bv-postgres-restore --network buildingvision_default -v bv-restore-data:/var/lib/postgresql/data \
  -e POSTGRES_PASSWORD=postgres postgres:17-alpine >/dev/null
for i in $(seq 1 60); do docker exec bv-postgres-restore pg_isready -U postgres >/dev/null 2>&1 && break; sleep 2; done
echo "== smoke query"
docker exec bv-postgres-restore psql -U postgres -d buildingvision -c "SELECT count(*) AS organizations FROM organizations;" \
  -c "SELECT count(*) AS work_orders, max(created_at) AS last_wo FROM work_orders;"
END=$(date +%s)
echo "== restore drill selesai dalam $((END-START)) detik. Bersihkan: docker rm -f bv-postgres-restore && docker volume rm bv-restore-data"
