-- +goose Up
ALTER TABLE automation_execution_jobs
  ADD COLUMN replay_generation integer NOT NULL DEFAULT 0
    CHECK (replay_generation >= 0),
  DROP CONSTRAINT automation_execution_jobs_event_id_automation_version_id_key,
  ADD CONSTRAINT automation_execution_jobs_event_version_generation_key
    UNIQUE (event_id, automation_version_id, replay_generation);

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM automation_execution_jobs
    WHERE replay_generation > 0
  ) THEN
    RAISE EXCEPTION
      'automation replay jobs must be retained before migration rollback';
  END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE automation_execution_jobs
  DROP CONSTRAINT automation_execution_jobs_event_version_generation_key,
  DROP COLUMN replay_generation,
  ADD CONSTRAINT automation_execution_jobs_event_id_automation_version_id_key
    UNIQUE (event_id, automation_version_id);
