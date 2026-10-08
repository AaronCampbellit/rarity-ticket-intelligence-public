-- +goose Up
CREATE TABLE webhook_event_plans (
  event_id uuid PRIMARY KEY REFERENCES event_outbox(event_id),
  planned_at timestamptz NOT NULL
);

CREATE TABLE webhook_event_deliveries (
  connection_id uuid NOT NULL,
  event_id uuid NOT NULL REFERENCES event_outbox(event_id),
  msp_id uuid NOT NULL,
  client_id uuid,
  state text NOT NULL DEFAULT 'pending',
  retry_window_seconds integer NOT NULL,
  attempt_count integer NOT NULL DEFAULT 0,
  first_attempt_at timestamptz NOT NULL,
  next_attempt_at timestamptz NOT NULL,
  lease_until timestamptz,
  delivered_at timestamptz,
  failed_at timestamptz,
  last_error_code text,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (connection_id, event_id),
  FOREIGN KEY (connection_id, msp_id)
    REFERENCES webhook_connections(id, msp_id),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  CHECK (state IN ('pending', 'delivered', 'failed')),
  CHECK (retry_window_seconds > 0 AND retry_window_seconds <= 86400),
  CHECK (attempt_count >= 0),
  CHECK (
    (state = 'pending' AND delivered_at IS NULL AND failed_at IS NULL)
    OR (state = 'delivered' AND delivered_at IS NOT NULL AND failed_at IS NULL)
    OR (state = 'failed' AND delivered_at IS NULL AND failed_at IS NOT NULL)
  )
);

CREATE INDEX webhook_event_deliveries_pending_idx
  ON webhook_event_deliveries (next_attempt_at, first_attempt_at)
  WHERE state = 'pending';

-- +goose Down
DROP TABLE webhook_event_deliveries;
DROP TABLE webhook_event_plans;
