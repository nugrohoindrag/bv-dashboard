# ADR-009 — Deployment pilot: Docker Compose di VPS

Status: **Diterima** · Tanggal: 2026-09-15 · Sumber: TAD v1.0 §14

Keputusan: satu VPS (4 vCPU/8 GB) Jakarta/Singapura: Caddy (TLS otomatis) → api ×2 (start-first rollout) + worker ×1, Postgres 17 + pgBackRest (repo S3 lokasi berbeda, full harian + incr 6 jam + WAL), MinIO (versioning), Prometheus/Alertmanager/Grafana/Loki/Promtail, node & postgres exporter. CI GitHub Actions → GHCR → deploy SSH (`infra/scripts/deploy.sh`), migrasi sebagai job terpisah.
Growth path: Cloud Run/ECS + managed Postgres tanpa perubahan kode (12-factor).
