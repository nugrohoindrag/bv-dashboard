# Runbook — Backup & Restore (RPO ≤ 15 menit, RTO ≤ 4 jam)

Sumber: TAD §11.6, Release Criteria #25.

## Skema backup
| Komponen | Mekanisme | Jadwal | Retensi | Lokasi |
|---|---|---|---|---|
| Postgres | pgBackRest full | 02:00 UTC harian | 30 hari | repo S3/R2 **lokasi berbeda** (`repo1-*` di `infra/pgbackrest/pgbackrest.conf`) |
| Postgres | pgBackRest incremental | 08:00/14:00/20:00 UTC | 30 hari | idem |
| Postgres WAL | `archive_command=pgbackrest archive-push` (kontinu, `archive_timeout=60`) | terus-menerus | 30 hari | idem → PITR, RPO ≈ 1 menit |
| Object storage (foto/export) | MinIO versioning + `mc replicate add` ke bucket kedua (lihat di bawah) | kontinu | versi 30 hari | bucket region lain |
| VPS | snapshot provider | harian | 7 hari | provider |

Catatan implementasi: `archive_command` dijalankan oleh proses postgres, sehingga **image postgres produksi harus menyertakan biner pgbackrest** (gunakan `infra/postgres/Dockerfile` bila memakai image alpine: `apk add pgbackrest`), atau ganti `archive_command` ke `pgbackrest` via TLS server pada container `pgbackrest` (`pg1-host-type=tls`). Verifikasi dengan `pgbackrest --stanza=bv check` — wajib sukses sebelum UAT.

Replikasi MinIO: `mc admin bucket remote add local/buildingvision https://KEY:SECRET@r2-endpoint/bv-replica --service replication && mc replicate add local/buildingvision --remote-bucket <arn>`.

## Monitoring backup
`infra/scripts/backup-metrics.sh` menulis `bv_backup_last_success_timestamp` (textfile collector node-exporter; tambahkan `--collector.textfile.directory` dan mount folder). Alert `BackupStale` (> 30 jam) → Telegram. Verifikasi mingguan: `pgbackrest --stanza=bv verify` (crontab Minggu 03:30).

## Restore — skenario
### A. Restore penuh ke VPS baru (VPS hilang) — target RTO 4 jam
1. Siapkan VPS + Docker, clone repo, salin `.env` (dari password manager) dan `pgbackrest.conf` (kredensial repo).
2. `docker compose up -d minio` (atau arahkan `BV_S3_*` ke bucket replika), **jangan** start postgres dulu.
3. `docker volume create buildingvision_pg-data`; restore: `docker run --rm -v buildingvision_pg-data:/var/lib/postgresql/data -v $PWD/pgbackrest/pgbackrest.conf:/etc/pgbackrest/pgbackrest.conf:ro woblerr/pgbackrest:2.54 pgbackrest --stanza=bv restore` (tambahkan `--type=time --target="YYYY-MM-DD HH:MM:SS+07" --target-action=promote` untuk PITR).
4. `docker compose up -d postgres` → tunggu healthy; cek `SELECT max(created_at) FROM activities;` sesuai target.
5. `docker compose up -d` lengkap; arahkan DNS; jalankan verifikasi pasca-deploy; `stanza-create` ulang bila repo baru.
6. Catat waktu mulai/selesai di log insiden (bukti RTO).

### B. Kesalahan data (hapus massal, migrasi salah) — PITR
1. Tentukan waktu sebelum kejadian (audit log / activities).
2. Jalankan `infra/scripts/restore-drill.sh --target "<waktu>"` → instance `bv-postgres-restore` terpisah.
3. Ekspor data yang perlu dipulihkan (`pg_dump -t tabel`) dan terapkan selektif ke produksi, atau bila seluruh DB: hentikan api/worker, tukar volume (`pg-data` ↔ `bv-restore-data`), start kembali.

### C. Restore drill (wajib sebelum UAT & tiap kuartal)
`cd infra && ./scripts/restore-drill.sh` → memulihkan backup terbaru ke container sementara, menjalankan smoke query (jumlah organisasi/WO, WO terakhir), mencetak durasi. Dokumentasikan hasil di tabel berikut.

| Tanggal | Ukuran DB | Durasi restore | Target PITR | Hasil | PIC |
|---|---|---|---|---|---|
| (isi saat drill) | | | | | |
