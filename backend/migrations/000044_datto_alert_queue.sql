-- +goose Up
CREATE TABLE datto_alert_queue (
  id uuid PRIMARY KEY,
  connection_id uuid NOT NULL REFERENCES datto_connections(id),
  msp_id uuid NOT NULL,
  client_id uuid,
  external_alert_id text NOT NULL,
  external_device_id text,
  datto_site_id text NOT NULL,
  priority text NOT NULL,
  title text NOT NULL,
  diagnostics text NOT NULL,
  fingerprint text NOT NULL,
  alert_state text NOT NULL,
  observed_at timestamptz NOT NULL,
  source_payload_ref text NOT NULL,
  processing_state text NOT NULL DEFAULT 'pending',
  attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  lease_until timestamptz,
  last_error_code text,
  created_at timestamptz NOT NULL,
  processed_at timestamptz,
  UNIQUE (
    connection_id, external_alert_id, alert_state, observed_at
  ),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  CHECK (alert_state IN ('active', 'cleared')),
  CHECK (
    processing_state IN (
      'pending', 'processing', 'processed', 'blocked_mapping', 'failed'
    )
  ),
  CHECK (
    (processing_state = 'processed') = (processed_at IS NOT NULL)
  )
);

CREATE INDEX datto_alert_queue_pending_idx
  ON datto_alert_queue (processing_state, observed_at, id)
  WHERE processing_state IN ('pending', 'processing');

CREATE INDEX datto_alert_queue_mapping_idx
  ON datto_alert_queue (connection_id, datto_site_id, observed_at)
  WHERE processing_state = 'blocked_mapping';

-- +goose Down
DROP TABLE datto_alert_queue;
