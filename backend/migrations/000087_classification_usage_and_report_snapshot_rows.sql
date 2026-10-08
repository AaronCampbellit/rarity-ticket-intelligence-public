-- +goose Up
ALTER TABLE ai_usage_records
  ALTER COLUMN recommendation_id DROP NOT NULL,
  ADD COLUMN generation_job_id uuid REFERENCES ai_generation_jobs(id);

ALTER TABLE ai_usage_records
  ADD CONSTRAINT ai_usage_records_exactly_one_subject
  CHECK ((recommendation_id IS NOT NULL) <> (generation_job_id IS NOT NULL));

CREATE UNIQUE INDEX ai_usage_records_generation_job_idx
  ON ai_usage_records(generation_job_id)
  WHERE generation_job_id IS NOT NULL;

CREATE TABLE tag_report_snapshot_rows (
  snapshot_id uuid NOT NULL REFERENCES tag_report_snapshots(id) ON DELETE CASCADE,
  ordinal bigint NOT NULL CHECK (ordinal >= 0),
  payload jsonb NOT NULL,
  payload_bytes bigint NOT NULL CHECK (payload_bytes >= 0),
  PRIMARY KEY (snapshot_id, ordinal)
);

INSERT INTO tag_report_snapshot_rows(snapshot_id, ordinal, payload, payload_bytes)
SELECT snapshot.id, item.ordinality - 1, item.value,
       octet_length(item.value::text)
FROM tag_report_snapshots snapshot
CROSS JOIN LATERAL jsonb_array_elements(snapshot.payload) WITH ORDINALITY item(value, ordinality);

ALTER TABLE tag_report_snapshots
  ADD COLUMN total_rows bigint NOT NULL DEFAULT 0 CHECK (total_rows >= 0),
  ADD COLUMN payload_bytes bigint NOT NULL DEFAULT 0 CHECK (payload_bytes >= 0),
  ADD COLUMN materialization_state text NOT NULL DEFAULT 'ready'
    CHECK (materialization_state IN ('building','ready')),
  ALTER COLUMN payload DROP NOT NULL;

UPDATE tag_report_snapshots snapshot
SET total_rows = aggregate.total_rows,
    payload_bytes = aggregate.payload_bytes
FROM (
  SELECT snapshot_id, count(*) total_rows, sum(payload_bytes) payload_bytes
  FROM tag_report_snapshot_rows
  GROUP BY snapshot_id
) aggregate
WHERE snapshot.id = aggregate.snapshot_id;

CREATE INDEX tag_report_snapshot_rows_page_idx
  ON tag_report_snapshot_rows(snapshot_id, ordinal);
DELETE FROM tag_report_snapshots older
USING tag_report_snapshots newer
WHERE older.id < newer.id
  AND older.msp_id = newer.msp_id AND older.client_id = newer.client_id
  AND older.report_kind = newer.report_kind AND older.query_hash = newer.query_hash
  AND older.projection_as_of = newer.projection_as_of;
CREATE UNIQUE INDEX tag_report_snapshots_materialization_key
  ON tag_report_snapshots(msp_id, client_id, report_kind, query_hash, projection_as_of);
CREATE INDEX tag_report_snapshots_active_lookup_idx
  ON tag_report_snapshots(msp_id, client_id, report_kind, query_hash, projection_as_of, expires_at);

-- +goose Down
UPDATE tag_report_snapshots snapshot
SET payload = COALESCE((
  SELECT jsonb_agg(row.payload ORDER BY row.ordinal)
  FROM tag_report_snapshot_rows row
  WHERE row.snapshot_id = snapshot.id
), '[]'::jsonb);

DROP INDEX tag_report_snapshots_active_lookup_idx;
DROP INDEX tag_report_snapshots_materialization_key;
DROP INDEX tag_report_snapshot_rows_page_idx;
DROP TABLE tag_report_snapshot_rows;
ALTER TABLE tag_report_snapshots
  DROP COLUMN materialization_state,
  DROP COLUMN payload_bytes,
  DROP COLUMN total_rows,
  ALTER COLUMN payload SET NOT NULL;

DROP INDEX ai_usage_records_generation_job_idx;
ALTER TABLE ai_usage_records DROP CONSTRAINT ai_usage_records_exactly_one_subject;
DELETE FROM ai_usage_records WHERE recommendation_id IS NULL;
ALTER TABLE ai_usage_records
  DROP COLUMN generation_job_id,
  ALTER COLUMN recommendation_id SET NOT NULL;
