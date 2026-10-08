-- +goose Up
CREATE TABLE graph_notification_hints (
  id uuid PRIMARY KEY,
  connection_id uuid NOT NULL REFERENCES graph_mailbox_connections(id),
  subscription_id uuid NOT NULL REFERENCES graph_subscriptions(id),
  message_id text NOT NULL CHECK (length(btrim(message_id)) > 0),
  received_at timestamptz NOT NULL,
  state text NOT NULL DEFAULT 'pending',
  attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  lease_until timestamptz,
  processed_at timestamptz,
  last_error_code text,
  UNIQUE (subscription_id, message_id),
  CHECK (state IN ('pending', 'processed', 'failed')),
  CHECK (
    (state = 'processed' AND processed_at IS NOT NULL)
    OR state <> 'processed'
  )
);

CREATE INDEX graph_notification_hints_due_idx
  ON graph_notification_hints (received_at, connection_id, id)
  WHERE state = 'pending';

-- +goose Down
DROP TABLE graph_notification_hints;
