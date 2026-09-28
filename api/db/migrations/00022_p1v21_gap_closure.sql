-- +goose Up
-- +goose StatementBegin
-- PRD P1 v2.1 (Roadmap v2.1) — penutupan gap P1: rantai keluhan tenant lintas tim (GAP-P1-07, Roadmap §17 contoh 1),
-- occupancy hotel mengikuti status kamar (GAP-P1-03). Backward-compatible (ADD COLUMN, perluasan CHECK, backfill). Lihat ADR-016.

-- ---------- P1-XMW-01: setiap Task / Finding / WO / Incident dalam rantai tertaut ke Service Request asal ----------
ALTER TABLE tasks       ADD COLUMN origin_service_request_id uuid REFERENCES service_requests(id);
ALTER TABLE work_orders ADD COLUMN origin_service_request_id uuid REFERENCES service_requests(id);
ALTER TABLE findings    ADD COLUMN origin_service_request_id uuid REFERENCES service_requests(id);
ALTER TABLE incidents   ADD COLUMN origin_service_request_id uuid REFERENCES service_requests(id);
CREATE INDEX idx_tasks_origin_sr       ON tasks (origin_service_request_id)       WHERE origin_service_request_id IS NOT NULL;
CREATE INDEX idx_work_orders_origin_sr ON work_orders (origin_service_request_id) WHERE origin_service_request_id IS NOT NULL;
CREATE INDEX idx_findings_origin_sr    ON findings (origin_service_request_id)    WHERE origin_service_request_id IS NOT NULL;
CREATE INDEX idx_incidents_origin_sr   ON incidents (origin_service_request_id)   WHERE origin_service_request_id IS NOT NULL;
-- anak langsung (dipakai penelusuran rantai & verifikasi lintas tim P2-XTW)
CREATE INDEX IF NOT EXISTS idx_tasks_source       ON tasks (source_type, source_id)       WHERE source_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_work_orders_source ON work_orders (source_type, source_id) WHERE source_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_findings_source    ON findings (source_type, source_id)    WHERE source_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_incidents_source   ON incidents (source_type, source_id)   WHERE source_id IS NOT NULL;

-- backfill: anak langsung SR, lalu turunan bertingkat (finding ← task/WO, WO/task ← finding/WO/task, incident ← finding/task)
UPDATE tasks       SET origin_service_request_id = source_id WHERE source_type = 'service_request' AND source_id IS NOT NULL;
UPDATE work_orders SET origin_service_request_id = source_id WHERE source_type = 'service_request' AND source_id IS NOT NULL;
DO $$
DECLARE n int; i int := 0;
BEGIN
  LOOP
    n := 0;
    WITH src AS (
      SELECT 'task'::text AS t, id, origin_service_request_id AS o FROM tasks WHERE origin_service_request_id IS NOT NULL
      UNION ALL SELECT 'work_order', id, origin_service_request_id FROM work_orders WHERE origin_service_request_id IS NOT NULL
      UNION ALL SELECT 'finding', id, origin_service_request_id FROM findings WHERE origin_service_request_id IS NOT NULL
      UNION ALL SELECT 'incident', id, origin_service_request_id FROM incidents WHERE origin_service_request_id IS NOT NULL
    ), u1 AS (
      UPDATE findings f SET origin_service_request_id = src.o FROM src
       WHERE f.origin_service_request_id IS NULL AND f.source_type = src.t AND f.source_id = src.id RETURNING 1
    ), u2 AS (
      UPDATE work_orders w SET origin_service_request_id = src.o FROM src
       WHERE w.origin_service_request_id IS NULL AND w.source_type = src.t AND w.source_id = src.id RETURNING 1
    ), u3 AS (
      UPDATE tasks t SET origin_service_request_id = src.o FROM src
       WHERE t.origin_service_request_id IS NULL AND t.source_type = src.t AND t.source_id = src.id RETURNING 1
    ), u4 AS (
      UPDATE incidents x SET origin_service_request_id = src.o FROM src
       WHERE x.origin_service_request_id IS NULL AND x.source_type = src.t AND x.source_id = src.id RETURNING 1
    )
    SELECT (SELECT count(*) FROM u1) + (SELECT count(*) FROM u2) + (SELECT count(*) FROM u3) + (SELECT count(*) FROM u4) INTO n;
    i := i + 1;
    EXIT WHEN n = 0 OR i >= 10;
  END LOOP;
END $$;

-- ---------- P1-XMW-03 / P2-XTW-01: Task tindak lanjut dari WO (inspeksi akhir, re-clean, verifikasi security) ----------
ALTER TABLE object_links DROP CONSTRAINT IF EXISTS object_links_link_type_check;
ALTER TABLE object_links ADD CONSTRAINT object_links_link_type_check CHECK (link_type IN
  ('generated_from','related_to','rework_of','escalated_to','follow_up_of'));
ALTER TABLE tasks ADD COLUMN follow_up_purpose text CHECK (follow_up_purpose IS NULL OR follow_up_purpose IN
  ('final_inspection','re_clean','security_verification','inspection','follow_up'));

-- ---------- P1-BLD-08 (D-P1-02): occupancy unit hotel mengikuti status kamar ----------
UPDATE units u SET occupancy_status = CASE
    WHEN hr.room_status = 'occupied' THEN 'occupied'
    WHEN hr.room_status IN ('out_of_order','out_of_service') THEN 'inactive'
    ELSE 'vacant' END
  FROM hotel_rooms hr WHERE hr.location_id = u.location_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tasks DROP COLUMN IF EXISTS follow_up_purpose;
UPDATE object_links SET link_type = 'related_to' WHERE link_type = 'follow_up_of';
ALTER TABLE object_links DROP CONSTRAINT IF EXISTS object_links_link_type_check;
ALTER TABLE object_links ADD CONSTRAINT object_links_link_type_check CHECK (link_type IN ('generated_from','related_to','rework_of','escalated_to'));
DROP INDEX IF EXISTS idx_incidents_source;
DROP INDEX IF EXISTS idx_findings_source;
DROP INDEX IF EXISTS idx_work_orders_source;
DROP INDEX IF EXISTS idx_tasks_source;
ALTER TABLE incidents   DROP COLUMN IF EXISTS origin_service_request_id;
ALTER TABLE findings    DROP COLUMN IF EXISTS origin_service_request_id;
ALTER TABLE work_orders DROP COLUMN IF EXISTS origin_service_request_id;
ALTER TABLE tasks       DROP COLUMN IF EXISTS origin_service_request_id;
-- +goose StatementEnd
