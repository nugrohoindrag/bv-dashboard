# ADR-010 — Offline sync server-authoritative dengan explicit conflict handling (OD-004)

Status: **Diterima** · Tanggal: 2026-09-15 · Sumber: TAD v1.0 §14

Keputusan: mobile menyimpan queue mutasi (`client_mutation_id`, `seq` per device+object) dan mengirim berurutan ke `POST /sync/mutations`; server memvalidasi terhadap state saat ini lewat Task Engine; hasil per mutasi `applied|duplicate|rejected|conflict` dengan `reason_code` (C1–C10); evidence tidak pernah dibuang; tidak ada merge engine. Supervisor meninjau konflik di Settings → Sync Conflicts.
Detail: `docs/conflict-rules.md`, kontrak: `contracts/sync-api.md`.
