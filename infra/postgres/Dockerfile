# Postgres 17 + pgBackRest (archive_command & backup dijalankan di dalam container postgres — lihat runbook backup-restore).
# Dibangun lokal di VPS: docker compose build postgres
FROM postgres:17-alpine
RUN apk add --no-cache pgbackrest && mkdir -p /var/spool/pgbackrest /var/log/pgbackrest /var/lib/pgbackrest \
    && chown -R postgres:postgres /var/spool/pgbackrest /var/log/pgbackrest /var/lib/pgbackrest
