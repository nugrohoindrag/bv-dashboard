# ADR-011 — Observability minimum P0: slog JSON + Prometheus + Loki

Status: **Diterima** · Tanggal: 2026-09-15 · Sumber: TAD v1.0 §14

Keputusan: log terstruktur `slog` (request_id, route, status, latency, org/user), metrik Prometheus di port internal (`BV_METRICS_ADDR`): RED per route pattern chi, job River (processed/error/durasi/queue depth), sweep lag, pool DB; alert sesuai TAD §11.5 (`infra/prometheus/alerts.yml`). OTel Collector disediakan untuk trace bila diperlukan; Sentry opsional lewat DSN.
Konsekuensi: kardinalitas label rendah (route pattern, bukan path); dashboard `infra/grafana/dashboards/bv-ops.json`.
