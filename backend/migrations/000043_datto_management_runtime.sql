-- +goose Up
ALTER TABLE datto_connections
  ADD COLUMN manual_requested_at timestamptz,
  ADD COLUMN manual_requested_by uuid,
  ADD COLUMN manual_request_id uuid,
  ADD CONSTRAINT datto_manual_request_actor_fkey
    FOREIGN KEY (manual_requested_by, msp_id)
    REFERENCES technicians(id, msp_id),
  ADD CONSTRAINT datto_manual_request_complete
    CHECK (
      (
        manual_requested_at IS NULL
        AND manual_requested_by IS NULL
        AND manual_request_id IS NULL
      )
      OR (
        manual_requested_at IS NOT NULL
        AND manual_requested_by IS NOT NULL
        AND manual_request_id IS NOT NULL
      )
    );

ALTER TABLE datto_sync_runs
  ADD COLUMN manual_request_id uuid,
  ADD CONSTRAINT datto_sync_runs_manual_request_id_key
    UNIQUE (manual_request_id);

-- +goose Down
ALTER TABLE datto_sync_runs
  DROP CONSTRAINT datto_sync_runs_manual_request_id_key,
  DROP COLUMN manual_request_id;

ALTER TABLE datto_connections
  DROP CONSTRAINT datto_manual_request_complete,
  DROP CONSTRAINT datto_manual_request_actor_fkey,
  DROP COLUMN manual_request_id,
  DROP COLUMN manual_requested_by,
  DROP COLUMN manual_requested_at;
