#!/usr/bin/env bash
# Backup Postgres via pgBackRest di dalam container postgres (repo: BV_BACKUP_DIR host, default /opt/bv-backup).
#   ./scripts/pg-backup.sh setup            # mkdir repo, stanza-create, check
#   ./scripts/pg-backup.sh full|incr|verify|info
#   ./scripts/pg-backup.sh mirror           # salin repo ke MinIO bucket bv-backup (mc mirror) — jalankan setelah full
# Cron user bv (WIB): 0 2 * * * full && mirror; 0 8,14,20 * * * incr; 30 3 * * 0 verify  → /var/log/bv-backup.log
set -euo pipefail
cd "$(dirname "$0")/.."
cmd="${1:-info}"
dir="$(grep ^BV_BACKUP_DIR .env 2>/dev/null | cut -d= -f2-)"; dir="${dir:-/opt/bv-backup}"
pgb() { docker compose exec -T -u postgres postgres pgbackrest --stanza=bv "$@"; }
case "$cmd" in
  setup)
    sudo mkdir -p "$dir" && sudo chown 70:70 "$dir" && sudo chmod 750 "$dir"   # uid postgres di image alpine = 70
    docker compose up -d postgres
    for i in $(seq 1 30); do docker compose exec -T postgres pg_isready -U postgres -q && break; sleep 2; done
    pgb stanza-create; pgb check; echo "pgbackrest siap (repo $dir)";;
  full|incr) pgb --type="$cmd" backup;;
  verify)    pgb verify;;
  info)      pgb info;;
  mirror)
    key="$(grep ^BV_S3_ACCESS_KEY .env | cut -d= -f2-)"; sec="$(grep ^BV_S3_SECRET_KEY .env | cut -d= -f2-)"
    docker run --rm --network buildingvision_default -v "$dir":/repo:ro -e MC_HOST_local="http://${key}:${sec}@minio:9000" \
      quay.io/minio/mc:RELEASE.2025-04-16T18-13-26Z mirror --overwrite --remove /repo local/bv-backup/pgbackrest | tail -2;;
  *) echo "usage: $0 setup|full|incr|verify|info|mirror"; exit 2;;
esac
