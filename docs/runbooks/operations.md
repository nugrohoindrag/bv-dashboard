# Runbook — Operasi harian & penanganan insiden

## Cek harian (5 menit)
- Grafana "BuildingVision — Operasional": API up = 2, error rate, p95, sweep lag (< 5 menit), queue depth (turun ke ~0), backup terakhir < 30 jam, disk < 80%.
- Settings → Sync Conflicts: jumlah konflik baru; tindak lanjuti lewat aksi normal (assign/complete on-behalf/reopen).
- `docker compose ps` semua `healthy`/`running`.

## Gejala → tindakan
| Gejala | Kemungkinan | Tindakan |
|---|---|---|
| Overview kosong / 503 `/ready` | DB down / pool habis | `docker compose logs postgres`; `SELECT count(*) FROM pg_stat_activity`; restart api bila pool bocor; cek alert PostgresConnectionsHigh |
| Task overdue tidak berubah, SLA tidak naik | worker mati / sweep lag | `docker compose logs worker`; `SELECT state, count(*) FROM river_job GROUP BY 1`; restart worker; periodic job otomatis kembali |
| Push mobile tidak terkirim | FCM kredensial | log `push` job (`state=retryable`), cek `BV_FCM_*`; in-app tetap jalan |
| Foto tidak tampil | presign/CORS MinIO/S3 | cek `BV_S3_*`, bucket policy, jam server (presign kedaluwarsa jika clock skew) |
| Export "processing" lama | worker/queue | lihat queue depth; job export retry otomatis; unduh via Inbox |
| Login 429 | rate limit login (10/menit/IP) | tunggu 1 menit; cek serangan brute force di log `auth.login_failed` |
| Sync `rejected SEQ_GAP` massal | device mengirim tidak urut | mobile harus kirim ulang dari seq hilang; tidak perlu tindakan server |
| Sertifikat TLS gagal | DNS/port 80 tertutup | `docker compose logs caddy`; pastikan A record & port 80/443 terbuka |

## Perawatan
- Idempotency keys & sync mutations dibersihkan job periodik (24 jam / 90 hari). Audit log tidak dihapus (P0).
- Rotasi kunci JWT: generate pasangan baru, set `BV_JWT_PRIVATE_KEY/PUBLIC_KEY`, restart api (sesi refresh lama tetap valid karena refresh token di DB; access token lama gagal → refresh otomatis).
- Menambah property: Settings → Property → Tambah Property (timezone IANA wajib); beri role per property ke user.
- Menonaktifkan user: Settings → Users → nonaktif (sesi dicabut). Reset password: aksi "Reset password".

## Eskalasi
Sev-1 (login/overview/sync down > 15 menit): rollback (runbook) → PIC backend → catat post-mortem di `docs/incidents/YYYY-MM-DD.md`.
