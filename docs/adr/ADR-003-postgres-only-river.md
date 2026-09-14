# ADR-003 — PostgreSQL-only: River untuk job queue, tanpa Redis

Status: **Diterima** · Tanggal: 2026-09-15 · Sumber: TAD v1.0 §14

Keputusan: PostgreSQL 17 sebagai satu-satunya stateful store: data, job queue (River, transactional outbox `EnqueueEventTx`), search (tsvector + pg_trgm), cache overview 30 dtk in-memory per proses, idempotency keys, sync mutations.
Konsekuensi: lebih sedikit komponen untuk backup/monitor (pgBackRest saja); job dijamin konsisten dengan data (enqueue di tx yang sama). Redis dapat ditambahkan bila rate-limit lintas replika atau cache bersama diperlukan (belum di P0).
