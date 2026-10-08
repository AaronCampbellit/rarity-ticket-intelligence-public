-- +goose Up
ALTER TABLE automation_runs
  ADD COLUMN execution_lease_until timestamptz,
  ADD COLUMN continuation_mode text,
  ADD COLUMN continuation_step_id text,
  ADD CONSTRAINT automation_run_continuation_complete
    CHECK (
      (
        continuation_mode IS NULL
        AND continuation_step_id IS NULL
      )
      OR (
        continuation_mode IN ('after_wait', 'retry_step')
        AND continuation_step_id IS NOT NULL
        AND length(btrim(continuation_step_id)) > 0
      )
    ),
  ADD CONSTRAINT automation_run_lease_requires_continuation
    CHECK (
      execution_lease_until IS NULL
      OR continuation_mode IS NOT NULL
    );

ALTER TABLE automation_step_runs
  ADD COLUMN attempt integer NOT NULL DEFAULT 1
    CHECK (attempt > 0);

ALTER TABLE automation_suspensions
  ADD COLUMN attempt integer NOT NULL DEFAULT 1
    CHECK (attempt > 0);

-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT run_id, step_id, attempt
    FROM automation_step_runs
    GROUP BY run_id, step_id, attempt
    HAVING count(*) > 1
  ) THEN
    RAISE EXCEPTION
      'automation runtime requires one step record per run attempt';
  END IF;
END
$$;
-- +goose StatementEnd

CREATE UNIQUE INDEX automation_step_runs_attempt_unique
  ON automation_step_runs (run_id, step_id, attempt);

CREATE TABLE automation_event_plans (
  event_id uuid PRIMARY KEY REFERENCES event_outbox(event_id),
  planned_at timestamptz NOT NULL
);

CREATE TABLE automation_execution_jobs (
  id uuid PRIMARY KEY,
  event_id uuid NOT NULL REFERENCES event_outbox(event_id),
  automation_version_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  run_id uuid,
  state text NOT NULL DEFAULT 'pending',
  attempt_count integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL,
  lease_until timestamptz,
  created_at timestamptz NOT NULL,
  processed_at timestamptz,
  failed_at timestamptz,
  last_error_code text,
  UNIQUE (event_id, automation_version_id),
  FOREIGN KEY (automation_version_id, msp_id)
    REFERENCES automation_versions(id, msp_id),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (run_id, msp_id, client_id)
    REFERENCES automation_runs(id, msp_id, client_id),
  CHECK (state IN ('pending', 'processing', 'processed', 'failed')),
  CHECK (attempt_count >= 0),
  CHECK (
    (state = 'pending'
      AND lease_until IS NULL
      AND processed_at IS NULL
      AND failed_at IS NULL)
    OR (state = 'processing'
      AND lease_until IS NOT NULL
      AND processed_at IS NULL
      AND failed_at IS NULL)
    OR (state = 'processed'
      AND lease_until IS NULL
      AND processed_at IS NOT NULL
      AND failed_at IS NULL)
    OR (state = 'failed'
      AND lease_until IS NULL
      AND processed_at IS NULL
      AND failed_at IS NOT NULL)
  )
);

CREATE INDEX automation_execution_jobs_due_idx
  ON automation_execution_jobs (next_attempt_at, created_at, id)
  WHERE state IN ('pending', 'processing');

-- +goose Down
DROP TABLE automation_execution_jobs;
DROP TABLE automation_event_plans;

DROP INDEX automation_step_runs_attempt_unique;

ALTER TABLE automation_suspensions
  DROP COLUMN attempt;

ALTER TABLE automation_step_runs
  DROP COLUMN attempt;

ALTER TABLE automation_runs
  DROP CONSTRAINT automation_run_lease_requires_continuation,
  DROP CONSTRAINT automation_run_continuation_complete,
  DROP COLUMN continuation_step_id,
  DROP COLUMN continuation_mode,
  DROP COLUMN execution_lease_until;
