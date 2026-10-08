-- +goose Up
DROP VIEW integration_health_signals;

CREATE VIEW integration_health_signals AS
SELECT
  id AS connection_id,
  msp_id,
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
  msp_id,
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
  msp_id,
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
  connection.msp_id,
  'teams'::text,
  connection.enabled,
  connection.health_state,
  connection.last_success_at,
  connection.last_error_code,
  count(attempt.id) FILTER (WHERE attempt.state IN ('retrying', 'failed'))::bigint
FROM teams_connections connection
LEFT JOIN teams_delivery_attempts attempt ON attempt.connection_id = connection.id
GROUP BY connection.id, connection.msp_id;

-- +goose Down
DROP VIEW integration_health_signals;

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
