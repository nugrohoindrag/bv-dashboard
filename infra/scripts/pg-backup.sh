#!/usr/bin/env bash
# Backup Postgres via pgBackRest di dalam container postgres. Pemakaian (dari folder infra/):
#   ./scripts/pg-backup.sh setup   # render pgbackrest.prod.conf dari .env (kredensial MinIO), stanza-create, check
#   ./scripts/pg-backup.sh full|incr|verify|info
# Cron (user bv): 0 2 * * * cd /opt/buildingvision/infra && ./scripts/pg-backup.sh full >> /var/log/bv-backup.log 2>&1
set -euo pipefail
cd "$(dirname "$0")/.."
cmd="${1:-info}"
conf=pgbackrest/pgbackrest.prod.conf
case "$cmd" in
  setup)
    key="$(grep ^BV_S3_ACCESS_KEY .env | cut -d= -f2-)"; sec="$(grep ^BV_S3_SECRET_KEY .env | cut -d= -f2-)"
    sed -e "s|^repo1-s3-key=.*|repo1-s3-key=${key}|" -e "s|^repo1-s3-key-secret=.*|repo1-s3-key-secret=${sec}|" pgbackrest/pgbackrest.conf > "$conf"
    chmod 600 "$conf"
    grep -q "^PGBACKREST_CONF=" .env || echo "PGBACKREST_CONF=./pgbackrest/pgbackrest.prod.conf" >> .env
    docker compose up -d postgres
    for i in $(seq 1 30); do docker compose exec -T postgres pg_isready -U postgres -q && break; sleep 2; done
    docker compose exec -T -u postgres postgres pgbackrest --stanza=bv stanza-create
    docker compose exec -T -u postgres postgres pgbackrest --stanza=bv check
    echo "pgbackrest siap — tambahkan cron: crontab -e";;
  full|incr) docker compose exec -T -u postgres postgres pgbackrest --stanza=bv --type="$cmd" backup;;
  verify)    docker compose exec -T -u postgres postgres pgbackrest --stanza=bv verify;;
  info)      docker compose exec -T -u postgres postgres pgbackrest --stanza=bv info;;
  *) echo "usage: $0 setup|full|incr|verify|info"; exit 2;;
esac
