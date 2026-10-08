-- +goose Up
ALTER TABLE graph_mailbox_connections
  ADD COLUMN client_state_secret_ref text,
  ADD COLUMN subscription_lease_until timestamptz,
  ADD COLUMN last_subscription_attempt_at timestamptz,
  ADD COLUMN last_subscription_success_at timestamptz,
  ADD CONSTRAINT graph_subscription_lease_valid
    CHECK (
      subscription_lease_until IS NULL
      OR (
        last_subscription_attempt_at IS NOT NULL
        AND subscription_lease_until > last_subscription_attempt_at
      )
    );

-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT connection_id
    FROM graph_subscriptions
    GROUP BY connection_id
    HAVING count(*) > 1
  ) THEN
    RAISE EXCEPTION
      'graph subscription runtime requires one subscription per connection';
  END IF;
END
$$;
-- +goose StatementEnd

UPDATE graph_mailbox_connections connection
SET client_state_secret_ref = subscription.client_state_secret_ref
FROM graph_subscriptions subscription
WHERE subscription.connection_id = connection.id;

CREATE UNIQUE INDEX graph_subscriptions_connection_unique
  ON graph_subscriptions (connection_id);

CREATE INDEX graph_mailbox_connections_subscription_due_idx
  ON graph_mailbox_connections (
    COALESCE(subscription_lease_until, '-infinity'::timestamptz),
    id
  )
  WHERE enabled AND client_state_secret_ref IS NOT NULL;

-- +goose Down
DROP INDEX graph_mailbox_connections_subscription_due_idx;
DROP INDEX graph_subscriptions_connection_unique;

ALTER TABLE graph_mailbox_connections
  DROP CONSTRAINT graph_subscription_lease_valid,
  DROP COLUMN last_subscription_success_at,
  DROP COLUMN last_subscription_attempt_at,
  DROP COLUMN subscription_lease_until,
  DROP COLUMN client_state_secret_ref;
