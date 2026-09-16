#!/usr/bin/env bash
# Ganti password role aplikasi (migrasi 00001 membuat bv_app/bv_worker dengan password dev). WAJIB dijalankan sekali di produksi
# setelah migrate pertama, lalu isi BV_APP_DB_PASSWORD / BV_WORKER_DB_PASSWORD di .env dan restart api/worker.
set -euo pipefail
: "${BV_APP_DB_PASSWORD:?}" ; : "${BV_WORKER_DB_PASSWORD:?}"
docker compose exec -T postgres psql -U postgres -d buildingvision -v ON_ERROR_STOP=1 \
  -c "ALTER ROLE bv_app PASSWORD '${BV_APP_DB_PASSWORD}';" \
  -c "ALTER ROLE bv_worker PASSWORD '${BV_WORKER_DB_PASSWORD}';"
echo "password role diperbarui — restart: docker compose up -d api worker"
