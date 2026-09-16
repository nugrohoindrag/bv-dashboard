#!/bin/sh
# Tulis metrik backup terakhir (textfile collector node-exporter) — dipanggil setelah pgbackrest backup di crontab.
# node-exporter perlu flag --collector.textfile.directory=/host/var/lib/node_exporter (mount folder ini). Lihat docs/runbooks/backup-restore.md.
set -e
OUT=${1:-/var/lib/node_exporter/bv_backup.prom}
INFO=$(pgbackrest --stanza=bv --output=json info)
TS=$(echo "$INFO" | sed -n 's/.*"timestamp":{"start":[0-9]*,"stop":\([0-9]*\)}.*/\1/p' | tail -n1)
[ -n "$TS" ] || TS=0
printf '# HELP bv_backup_last_success_timestamp Unix time backup pgBackRest terakhir sukses.\n# TYPE bv_backup_last_success_timestamp gauge\nbv_backup_last_success_timestamp %s\n' "$TS" > "$OUT.tmp"
mv "$OUT.tmp" "$OUT"
