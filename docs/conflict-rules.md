# Aturan Konflik Offline Sync (OD-004, TAD §6.6) — C1–C10

Model: **server-authoritative** dengan explicit conflict handling. Tidak ada last-write-wins untuk transisi status, tidak ada merge engine. Evidence (checklist result, foto, checkpoint scan, finding, comment) **selalu dipertahankan** walau transisi yang menyertainya konflik.

Implementasi: `api/internal/sync/service.go` (`Push`, `dispatch`, `classify`), diuji di `internal/app/sync_overview_test.go` (TestOfflineSync). Tampilan supervisor: Settings → Sync Conflicts; notifikasi `sync.conflict` ke supervisor property.

| No | Situasi | `status` | `reason_code` | State server | Evidence |
|---|---|---|---|---|---|
| C1 | Transisi valid dari status server saat ini | `applied` | — | Diterapkan; activity `source=sync`, `client_recorded_at` disimpan | Disimpan normal |
| C2 | Transisi tidak valid (mis. `complete` saat server `on_hold`; `start` saat sudah `in_progress` oleh user lain) | `conflict` | `INVALID_TRANSITION` | Tidak berubah | Mutasi evidence dalam batch yang sama tetap diterapkan; activity `evidence_recorded_offline` |
| C3 | Object sudah `closed`/`cancelled` di server | `conflict` | `OBJECT_TERMINAL` | Tidak berubah (terminal immutable) | Disimpan sebagai activity `late_evidence` + attachment; tampil di timeline & daftar konflik |
| C4 | Object di-reassign ke user/team lain saat worker offline | `conflict` | `REASSIGNED` | Assignment server tetap; transisi assignee lama ditolak | Disimpan (seperti C2); supervisor memutuskan: reassign kembali atau `complete` on-behalf (`*.manage`) |
| C5 | Dua device user yang sama mengirim transisi object yang sama | `conflict` (yang kedua) | `DUPLICATE_SESSION` | Transisi pertama yang tiba menang | Evidence dari kedua device disimpan |
| C6 | Mutasi yang sama dikirim ulang (retry) | `duplicate` | — | Tidak berubah | Tidak diduplikasi (`client_mutation_id`, `client_attachment_id`, `client_scan_id`, `client_comment_id`) |
| C7 | Checklist item dijawab ulang oleh worker yang sama | `applied` | — | Jawaban baru menjadi nilai saat ini | Jawaban lama tetap di activity (tidak ada overwrite diam-diam) |
| C8 | Jam device menyimpang > 10 menit | `applied` | — | Diproses normal; urutan memakai `seq`, bukan `client_time` | `client_time` disimpan apa adanya; activity ditandai `clock_skew` |
| C9 | `seq` tidak monoton per (device, object) dalam batch | `rejected` | `SEQ_GAP` | Mutasi tersebut dan sesudahnya untuk object yang sama tidak diproses | Client mengirim ulang dari mutasi yang hilang; item tetap `Pending Sync` |
| C10 | Payload tidak valid / tanpa izin / object tidak dikenal | `rejected` | `VALIDATION_ERROR` / `FORBIDDEN` / `NOT_FOUND` | Tidak berubah | Tidak disimpan; baris `sync_mutations` tetap ada untuk audit |

## Aturan tambahan
- Urutan pemrosesan: per batch, per `(device_id, object_id)` menurut `seq`; mutasi lintas object independen.
- `attach_photo` offline: server membuat attachment `pending` + `upload_url` (24 jam); guard evidence menerima `pending` **dari sync** selama file tiba ≤ 24 jam, jika tidak object ditandai `evidence_incomplete` (flag di web).
- `checkpoint_scan` setelah patrol `completed` → disimpan sebagai `late_evidence` (C3) dan checkpoint tidak lagi dihitung.
- Transisi `complete` yang gagal guard evidence (foto wajib belum ada) → `conflict INVALID_TRANSITION` detail `EVIDENCE_REQUIRED`; evidence menyusul lalu client mengirim ulang `complete` dengan `client_mutation_id` baru.
- Supervisor menandai konflik "sudah ditinjau" (`POST /sync/conflicts/{id}/acknowledge`); tidak ada UI merge.
