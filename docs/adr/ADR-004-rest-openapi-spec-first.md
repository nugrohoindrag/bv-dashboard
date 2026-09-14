# ADR-004 — REST + OpenAPI 3.1 sebagai kontrak

Status: **Diterima** · Tanggal: 2026-09-15 · Sumber: TAD v1.0 §14

Keputusan: REST JSON snake_case, error RFC 9457 problem+json, cursor pagination, `Idempotency-Key` untuk POST create/transition, `If-Match` (version) untuk PATCH. Spec digenerate dari router+struct (`bvctl openapi`) dan di-commit di `contracts/openapi/v1.yaml`; CI gagal bila hasil generate berbeda.
Konsekuensi: web dan mobile memakai tipe dari spec; breaking change = versi path baru (`/api/v2`).
