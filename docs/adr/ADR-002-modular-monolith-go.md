# ADR-002 — Modular monolith Go, bukan microservices

Status: **Diterima** · Tanggal: 2026-09-15 · Sumber: TAD v1.0 §14

Konteks: tim kecil, 1–5 property pilot, domain saling terkait (Task Engine dipakai semua modul).
Keputusan: satu binary `api` + satu `worker` + `bvctl`; modul di `internal/<domain>` dengan service + http per modul; ekstensi (engineering, security, housekeeping, tenantservice, notification, search, exports, overview, sync) didaftarkan lewat `app.Extension`.
Konsekuensi: deploy sederhana (Compose), transaksi lintas modul dalam satu tx Postgres (hook `CreateHook`/`ExecutionHook`); batas modul dijaga lewat interface (`Enqueuer`, `Storage`, `ObjectAccess`).
