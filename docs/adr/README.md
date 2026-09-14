# Architecture Decision Records

Status: Diterima kecuali disebut lain. Format: Konteks → Keputusan → Konsekuensi. Nomor mengikuti TAD §14; ADR-010/011 ditambahkan saat implementasi.

- [ADR-001](ADR-001-mobile-stack-flutter.md) — Mobile stack: Flutter (repo terpisah)
- [ADR-002](ADR-002-modular-monolith-go.md) — Modular monolith Go, bukan microservices
- [ADR-003](ADR-003-postgres-only-river.md) — PostgreSQL-only: River untuk job queue, tanpa Redis
- [ADR-004](ADR-004-rest-openapi-spec-first.md) — REST + OpenAPI 3.1 sebagai kontrak
- [ADR-005](ADR-005-multitenancy-rls.md) — Multi-tenancy: shared DB + shared schema + RLS
- [ADR-006](ADR-006-locations-single-table-ltree.md) — Tabel `locations` tunggal (ltree) + tabel detail per level
- [ADR-007](ADR-007-task-workorder-separate-shared-execution.md) — Task & Work Order tabel terpisah, execution sub-entity bersama
- [ADR-008](ADR-008-web-react-spa.md) — Web sebagai React SPA (Vite), tanpa SSR
- [ADR-009](ADR-009-deploy-docker-compose-vps.md) — Deployment pilot: Docker Compose di VPS
- [ADR-010](ADR-010-offline-sync-server-authoritative.md) — Offline sync server-authoritative dengan explicit conflict handling (OD-004)
- [ADR-011](ADR-011-observability-minimum-p0.md) — Observability minimum P0: slog JSON + Prometheus + Loki
