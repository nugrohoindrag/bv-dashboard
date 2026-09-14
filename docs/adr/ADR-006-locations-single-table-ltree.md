# ADR-006 — Tabel `locations` tunggal (ltree) + tabel detail per level

Status: **Diterima** · Tanggal: 2026-09-15 · Sumber: TAD v1.0 §14

Keputusan: hierarki Property→Building→Tower→Floor→Area/Space/Unit di satu tabel `locations` (`path` ltree, `location_type`, `code`), detail spesifik di `properties/buildings/towers/floors/areas/spaces/units` (1:1). Alias endpoint per tipe (`/buildings`, `/units`, …) untuk Naming Convention §47.
Konsekuensi: filter subtree (`location_id` → `path <@`) murah; `path_text` didenormalisasi untuk tampilan; QR untuk area/space/unit di `qr_codes`.
