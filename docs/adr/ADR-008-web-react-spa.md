# ADR-008 — Web sebagai React SPA (Vite), tanpa SSR

Status: **Diterima** · Tanggal: 2026-09-15 · Sumber: TAD v1.0 §14

Keputusan: React 19 + Vite 8 + TypeScript strict + Tailwind v4 (token dari `design-tokens/tokens.json`) + Radix primitives + TanStack Query/Table; desktop-first ≥1280px; statis di belakang Caddy; auth access token di memori + refresh cookie HttpOnly.
Konsekuensi: tanpa server Node; code splitting per modul (lazy route); i18n `id` default. Status/label bersumber dari `contracts/status-map.yaml` (generate `npm run gen`).
