-- +goose Up
CREATE TABLE automation_runs (
  id uuid PRIMARY KEY,
  automation_version_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  trigger_event_id uuid NOT NULL,
  idempotency_key text NOT NULL,
  causation_id uuid,
  depth integer NOT NULL CHECK (depth BETWEEN 0 AND 8),
  attempt integer NOT NULL CHECK (attempt > 0),
  max_attempts integer NOT NULL CHECK (max_attempts >= attempt),
  state text NOT NULL,
  input_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  changed_object_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  started_at timestamptz NOT NULL,
  completed_at timestamptz,
  error_code text,
  retry_at timestamptz,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, idempotency_key),
  FOREIGN KEY (automation_version_id, msp_id)
    REFERENCES automation_versions(id, msp_id),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  CHECK (state IN ('running', 'waiting', 'succeeded', 'retrying', 'failed'))
);

CREATE TABLE automation_step_runs (
  id uuid PRIMARY KEY,
  run_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  step_id text NOT NULL,
  action_kind text,
  state text NOT NULL,
  started_at timestamptz NOT NULL,
  completed_at timestamptz,
  error_code text,
  changed_object_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  FOREIGN KEY (run_id, msp_id, client_id)
    REFERENCES automation_runs(id, msp_id, client_id),
  CHECK (state IN ('running', 'waiting', 'succeeded', 'failed'))
);

CREATE TABLE automation_suspensions (
  id uuid PRIMARY KEY,
  run_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  step_id text NOT NULL,
  resume_at timestamptz NOT NULL,
  input_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  resumed_at timestamptz,
  UNIQUE (run_id, step_id),
  FOREIGN KEY (run_id, msp_id, client_id)
    REFERENCES automation_runs(id, msp_id, client_id)
);

CREATE TABLE automation_dead_letters (
  id uuid PRIMARY KEY,
  run_id uuid NOT NULL,
  automation_version_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  trigger_event_id uuid NOT NULL,
  created_at timestamptz NOT NULL,
  error_code text NOT NULL,
  safe_message text NOT NULL,
  state text NOT NULL DEFAULT 'open',
  resolved_at timestamptz,
  resolved_by uuid,
  resolution_reason text,
  UNIQUE (run_id),
  FOREIGN KEY (run_id, msp_id, client_id)
    REFERENCES automation_runs(id, msp_id, client_id),
  FOREIGN KEY (automation_version_id, msp_id)
    REFERENCES automation_versions(id, msp_id),
  CHECK (state IN ('open', 'retrying', 'replayed', 'dismissed'))
);

CREATE TABLE automation_dead_letter_actions (
  id uuid PRIMARY KEY,
  dead_letter_id uuid NOT NULL REFERENCES automation_dead_letters(id),
  action text NOT NULL,
  reason text NOT NULL,
  acted_at timestamptz NOT NULL,
  acted_by uuid NOT NULL,
  correlation_id uuid NOT NULL,
  CHECK (action IN ('inspect', 'retry', 'replay', 'dismiss'))
);

CREATE INDEX automation_runs_retry_idx
  ON automation_runs (state, retry_at)
  WHERE state = 'retrying';

CREATE INDEX automation_dead_letters_open_idx
  ON automation_dead_letters (msp_id, client_id, created_at DESC)
  WHERE state = 'open';

-- +goose Down
DROP TABLE automation_dead_letter_actions;
DROP TABLE automation_dead_letters;
DROP TABLE automation_suspensions;
DROP TABLE automation_step_runs;
DROP TABLE automation_runs;
