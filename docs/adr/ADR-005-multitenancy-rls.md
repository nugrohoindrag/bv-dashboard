# ADR-005 — Multi-tenancy: shared DB + shared schema + RLS

Status: **Diterima** · Tanggal: 2026-09-15 · Sumber: TAD v1.0 §14

Keputusan: setiap tabel bisnis punya `organization_id`; RLS `app.organization_id` di-set per transaksi (`set_config`) dari principal; aplikasi memakai role `bv_app` (bukan superuser); login memakai `app.auth_lookup='on'` dalam tx terpisah.
Konsekuensi: isolasi organisasi ditegakkan DB (AT-010 diuji dengan koneksi bv_app); query lintas org hanya oleh worker (`bv_worker`) per-org loop. Trade-off: index harus menyertakan organization_id.
