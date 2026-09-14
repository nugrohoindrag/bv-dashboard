# Runbook — Rollback

Prinsip (TAD §7.7, §11.4): migrasi **backward-compatible** (tambah kolom nullable/tabel baru dulu, hapus di rilis berikutnya) sehingga rollback aplikasi = deploy image versi sebelumnya **tanpa** membalikkan migrasi.

## Rollback aplikasi (api/worker/web)
1. Tentukan versi terakhir yang sehat (tag `vX.Y.Z-1`, lihat `docker compose ps` / GHCR).
2. `cd /opt/buildingvision/infra && BV_VERSION=vX.Y.Z-1 ./scripts/deploy.sh` — langkah `migrate` idempoten (goose hanya menerapkan versi baru; tidak ada yang baru → no-op).
3. Verifikasi pasca-deploy (`docs/runbooks/deploy.md`). Waktu tipikal < 5 menit.
4. Bila hanya web bermasalah: `BV_VERSION=… docker compose up --no-deps web` (dist ditukar, Caddy tidak perlu restart).

## Rollback migrasi (hanya bila migrasi merusak data/kinerja)
- Setiap migrasi goose punya blok `-- +goose Down`. Jalankan **hanya** setelah api/worker versi baru dihentikan: `docker compose stop api worker && docker compose run --rm -e BV_DATABASE_URL=postgres://postgres:…@postgres:5432/buildingvision api /app/bvctl migrate --down` (satu langkah per eksekusi), lalu deploy image lama.
- Bila `Down` tidak aman (drop kolom berisi data): gunakan PITR ke titik sebelum migrasi (`backup-restore.md` skenario B) — data setelah titik itu hilang; komunikasikan ke property manager.

## Rollback konfigurasi
- `.env` di-versi manual: simpan salinan `infra/.env.<tanggal>` sebelum perubahan; kembalikan lalu `docker compose up -d`.
- Caddyfile: `git checkout -- infra/caddy/Caddyfile && docker compose exec caddy caddy reload --config /etc/caddy/Caddyfile`.

## Keputusan rollback vs fix-forward
Rollback bila: error rate > 2% selama 5 menit, login/overview gagal, sync mutations menolak massal (`rejected` VALIDATION_ERROR meningkat), atau alert kritis pasca-deploy. Fix-forward bila dampak terisolasi (satu modul, ada workaround) dan perbaikan < 1 jam.
