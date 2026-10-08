-- +goose Up
CREATE TABLE teams_connections (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  name text NOT NULL,
  webhook_secret_ref text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  health_state text NOT NULL DEFAULT 'pending',
  last_success_at timestamptz,
  last_error_code text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, name),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (health_state IN ('pending', 'healthy', 'degraded', 'failed', 'disabled'))
);

CREATE TABLE teams_delivery_attempts (
  id uuid PRIMARY KEY,
  connection_id uuid NOT NULL REFERENCES teams_connections(id),
  event_id uuid NOT NULL,
  work_record_id uuid NOT NULL,
  attempt integer NOT NULL CHECK (attempt > 0),
  first_attempt_at timestamptz NOT NULL,
  attempted_at timestamptz NOT NULL,
  state text NOT NULL,
  error_code text,
  UNIQUE (connection_id, event_id, attempt),
  CHECK (state IN ('delivered', 'retrying', 'failed')),
  CHECK (attempted_at >= first_attempt_at),
  CHECK (attempted_at <= first_attempt_at + interval '24 hours' OR state = 'failed')
);

CREATE INDEX teams_delivery_history_idx
  ON teams_delivery_attempts (connection_id, attempted_at DESC);

CREATE VIEW integration_health_signals AS
SELECT
  id AS connection_id,
  'graph'::text AS integration_kind,
  enabled,
  health_state,
  last_success_at,
  last_error_code,
  0::bigint AS pending_failures
FROM graph_mailbox_connections
UNION ALL
SELECT
  id,
  'datto'::text,
  enabled,
  health_state,
  last_completed_at,
  last_error_code,
  0::bigint
FROM datto_connections
UNION ALL
SELECT
  id,
  'forwarding'::text,
  enabled,
  health_state,
  last_received_at,
  last_error_code,
  0::bigint
FROM forwarding_intake_connections
UNION ALL
SELECT
  connection.id,
  'teams'::text,
  connection.enabled,
  connection.health_state,
  connection.last_success_at,
  connection.last_error_code,
  count(attempt.id) FILTER (WHERE attempt.state IN ('retrying', 'failed'))::bigint
FROM teams_connections connection
LEFT JOIN teams_delivery_attempts attempt ON attempt.connection_id = connection.id
GROUP BY connection.id;

-- +goose Down
DROP VIEW integration_health_signals;
DROP TABLE teams_delivery_attempts;
DROP TABLE teams_connections;
