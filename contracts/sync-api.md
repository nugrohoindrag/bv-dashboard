# Kontrak Sync API untuk aplikasi mobile (P0 Offline-Lite)

Base: `https://<domain>/api/v1` · Auth: `Authorization: Bearer <access_token>` (15 menit; refresh via `POST /auth/refresh` body `{refresh_token, client:"mobile"}` — mobile menyimpan refresh token di secure storage, rotasi setiap refresh, reuse → semua sesi dicabut).
Header wajib: `X-Device-Id` (UUID stabil per instalasi), `X-App-Version` (semver); `GET /health` mengembalikan `min_supported_app_version` — bila lebih besar dari versi app, tampilkan layar update wajib.
Format: JSON snake_case, waktu RFC 3339 UTC, error RFC 9457 (`application/problem+json` dengan `code`, `errors[]`).
Tipe lengkap: `contracts/openapi/v1.yaml`; status & warna: `contracts/status-map.yaml` (`contracts/build/status_map.dart`); token: `contracts/build/tokens.dart`; permission: `contracts/permissions.yaml`.

## 1. Work bundle — `GET /sync/work-bundle?device_id=…&since=<cursor>`
Mengembalikan semua yang dibutuhkan untuk bekerja offline hari ini (permission `sync.work_bundle.view`):
```json
{
  "server_time": "2026-09-15T01:00:00Z",
  "cursor": "2026-09-15T01:00:00Z",
  "tasks": [WorkItem], "work_orders": [WorkItem], "cleaning_tasks": [WorkItem],
  "patrol_tasks": [{ "task": WorkItem, "route": {...}, "checkpoints": [{ "checkpoint_id", "name", "qr_code", "sort_order", "location_path", "status", "scanned_at" }] }],
  "checklist_runs": [ChecklistRun],
  "locations": [{ "id", "name", "location_type", "path_text", "qr_code" }],
  "assets": [{ "id", "asset_code", "name", "location_id", "qr_code" }],
  "master": { "finding_types": [...], "incident_categories": [...], "priorities": [...] },
  "removed": [{ "object_type", "object_id" }],
  "me": { "id", "full_name", "permissions": [...], "team_ids": [...] }
}
```
Isi: item milik user/team-nya yang **dijadwalkan hari ini (timezone property)** + open/overdue, beserta checklist run yang sudah ada. `since` = cursor terakhir → hanya perubahan sejak itu + `removed` (objek yang tidak lagi relevan: closed/cancelled/reassigned). Panggil saat app dibuka, saat online kembali, dan tiap 15 menit di foreground. `WorkItem.allowed_actions` menunjukkan tombol yang boleh ditampilkan; `requires_evidence`/`checklist_summary.photo_missing` untuk guard lokal.

## 2. Push mutasi — `POST /sync/mutations` (`sync.mutations.create`, `Idempotency-Key` opsional)
```json
{ "device_id": "…", "mutations": [
  { "client_mutation_id": "uuidv7", "object_type": "task|work_order", "object_id": "…", "action": "start",
    "seq": 1, "client_time": "2026-09-15T08:00:12+07:00", "payload": { "gps_lat": -6.2, "gps_lng": 106.8, "gps_status": "captured" } }
]}
```
`seq`: bilangan naik per `(device_id, object_id)` (mulai 1, tidak boleh ada lubang — C9). Batch ≤ 200 mutasi; kirim per object secara berurutan; jangan mengirim ulang mutasi yang sudah `applied|duplicate|conflict|rejected`.

| `action` | `object_type` | `payload` | `response` |
|---|---|---|---|
| `start` / `hold` / `resume` / `complete` | task, work_order | `TransitionInput`: `reason`, `completion_notes`, `resolution`, `gps_lat/lng/status` | `{status}` |
| `checklist_item_result` | task, work_order | `{ item_id, result_value: ok|not_ok|na|yes|no, result_number, result_text, attachment_id|client_attachment_id, note, create_finding, finding_severity }` | `{run_id}` |
| `checkpoint_scan` | task (patrol) | `{ checkpoint_id | qr_code, scan_method: qr|manual, gps_lat, gps_lng, gps_status, note, client_scan_id }` | — |
| `add_comment` | task, work_order, service_request, incident, finding | `{ body }` | `{comment_id}` |
| `attach_photo` | semua di atas | `{ client_attachment_id, attachment_type: photo|photo_before|photo_after|checklist_item_photo (alias: before|after|checklist), content_type, size_bytes, sha256, captured_at, gps_lat, gps_lng, gps_status: captured|unavailable|denied, caption }` | `{ attachment_id, upload_url, storage_key, confirm_path }` |
| `add_finding` | task, work_order | `CreateFindingInput` (`finding_type`, `title`, `severity`, `location_id`, `asset_id`, `attachment_id`) — `source_*` otomatis | `{finding_id}` |
| `report_incident` | task | `CreateIncidentInput` | `{incident_id}` |

Respons:
```json
{ "server_time": "…", "results": [
  { "client_mutation_id": "…", "status": "applied", "server_version": 7, "response": {...} },
  { "client_mutation_id": "…", "status": "conflict", "reason_code": "INVALID_TRANSITION", "detail": "…" },
  { "client_mutation_id": "…", "status": "rejected", "reason_code": "SEQ_GAP" }
]}
```
Perlakuan client per status: `applied`/`duplicate` → hapus dari queue; `conflict` → hapus dari queue, tandai item "Konflik — ditinjau supervisor", refresh bundle; `rejected SEQ_GAP` → kirim ulang mulai mutasi yang hilang; `rejected VALIDATION_ERROR|FORBIDDEN|NOT_FOUND` → buang, tampilkan pesan. Aturan lengkap: `docs/conflict-rules.md`.

## 3. Foto
Alur online: `POST /attachments/presign` → `PUT upload_url` (Content-Type sama) → `POST /attachments/{id}/confirm` `{captured_at, gps_status, sha256}`. Offline: kirim `attach_photo` lewat `/sync/mutations` dulu (mendapat `upload_url` 24 jam), unggah file saat koneksi memungkinkan, lalu `confirm`. Kompres ≤ 1600 px sisi terpanjang, JPEG q≈80, EXIF orientasi diterapkan; `client_attachment_id` mencegah duplikasi.

## 4. QR
`GET /qr/{code}/resolve` → `{ object_type: asset|checkpoint|location, object_id, label, deep_link }` (validasi organisasi & permission). Untuk patrol offline, cocokkan `qr_code` dari bundle secara lokal; kirim `checkpoint_scan` dengan `qr_code`.

## 5. Push notification
`POST /me/devices` `{platform: android|ios, token, device_id, app_version}` saat login/refresh token FCM; `DELETE /me/devices/{token}` saat logout. Payload data: `{type, object_type, object_id, deep_link}`; deep link web `/tasks/{id}`, `/work-orders/{id}`, `/service-requests/{id}`, `/findings/{id}`, `/operations/incidents/{id}`.

## 6. Versi kontrak
Perubahan breaking pada bagian ini menaikkan `BV_MIN_MOBILE_APP_VERSION` di server dan dicatat di tabel ini.

| Versi | Tanggal | Perubahan |
|---|---|---|
| 1.0 | 2026-09-15 | Rilis awal P0 |
