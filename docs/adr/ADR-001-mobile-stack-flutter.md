# ADR-001 — Mobile stack: Flutter (repo terpisah)

Status: **Diterima** · Tanggal: 2026-09-15 · Sumber: TAD v1.0 §14

Konteks: aplikasi lapangan (technician, officer, housekeeping) butuh offline-lite, kamera, QR, GPS, satu codebase Android/iOS.
Keputusan: Flutter + Riverpod; **repo terpisah** (keputusan user 15 Sep 2026) yang mengonsumsi `contracts/` dari monorepo ini (OpenAPI, status-map, tokens, sync-api).
Konsekuensi: kontrak harus dipublikasikan sebagai artefak versi (tag `v*`); perubahan breaking pada `/sync/*` wajib menaikkan `BV_MIN_MOBILE_APP_VERSION` dan dicatat di `contracts/sync-api.md`.
