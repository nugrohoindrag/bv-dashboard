# ADR-007 — Task & Work Order tabel terpisah, execution sub-entity bersama

Status: **Diterima** · Tanggal: 2026-09-15 · Sumber: TAD v1.0 §14

Keputusan: `tasks` (patrol, cleaning, inspection, general) dan `work_orders` (maintenance, corrective, repair, service) terpisah karena atribut & SLA berbeda; sub-entitas eksekusi bersama: `assignments`, `checklist_runs(+items)`, `attachments`, `comments`, `object_links`, `sla_tracking`, `activities` — dikunci `(object_type, object_id)`.
State machine data-driven (`internal/operations/workflow`, status awal `new` — TD-001) dengan guard IsAssignee / EvidenceSatisfied / RequiresAssignee dan hook per `objectType:type` (patrol missed, cleaning inspection, maintenance schedule).
