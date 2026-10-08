-- +goose Up
ALTER TABLE graph_mailbox_connections
  ALTER COLUMN credential_secret_ref DROP NOT NULL,
  ADD COLUMN graph_tenant_id text,
  ADD COLUMN graph_client_id text,
  ADD COLUMN configuration_generation bigint NOT NULL DEFAULT 1
    CHECK (configuration_generation > 0),
  ADD COLUMN credential_secret_version integer,
  ADD COLUMN credential_secret_nonce bytea,
  ADD COLUMN credential_secret_ciphertext bytea,
  ADD COLUMN client_state_secret_version integer,
  ADD COLUMN client_state_secret_nonce bytea,
  ADD COLUMN client_state_secret_ciphertext bytea,
  ADD CONSTRAINT graph_credential_secret_shape CHECK (
    (
      credential_secret_ref IS NOT NULL
      AND credential_secret_ref LIKE 'env://RARITY_GRAPH_CREDENTIAL_%'
      AND credential_secret_version IS NULL
      AND credential_secret_nonce IS NULL
      AND credential_secret_ciphertext IS NULL
    )
    OR (
      credential_secret_ref IS NULL
      AND credential_secret_version IS NOT NULL
      AND credential_secret_nonce IS NOT NULL
      AND credential_secret_ciphertext IS NOT NULL
    )
  ),
  ADD CONSTRAINT graph_client_state_secret_shape CHECK (
    (
      client_state_secret_ref IS NULL
      AND client_state_secret_version IS NULL
      AND client_state_secret_nonce IS NULL
      AND client_state_secret_ciphertext IS NULL
    )
    OR (
      client_state_secret_ref IS NOT NULL
      AND client_state_secret_ref LIKE 'env://RARITY_GRAPH_CLIENT_STATE_%'
      AND client_state_secret_version IS NULL
      AND client_state_secret_nonce IS NULL
      AND client_state_secret_ciphertext IS NULL
    )
    OR (
      client_state_secret_ref IS NULL
      AND client_state_secret_version IS NOT NULL
      AND client_state_secret_nonce IS NOT NULL
      AND client_state_secret_ciphertext IS NOT NULL
    )
  );

DROP INDEX graph_mailbox_connections_subscription_due_idx;
CREATE INDEX graph_mailbox_connections_subscription_due_idx
  ON graph_mailbox_connections (
    COALESCE(subscription_lease_until, '-infinity'::timestamptz),
    id
  )
  WHERE enabled
    AND (client_state_secret_ref IS NOT NULL OR client_state_secret_version IS NOT NULL);

-- +goose Down
DROP INDEX graph_mailbox_connections_subscription_due_idx;
CREATE INDEX graph_mailbox_connections_subscription_due_idx
  ON graph_mailbox_connections (
    COALESCE(subscription_lease_until, '-infinity'::timestamptz),
    id
  )
  WHERE enabled AND client_state_secret_ref IS NOT NULL;

ALTER TABLE graph_mailbox_connections
  DROP CONSTRAINT graph_client_state_secret_shape,
  DROP CONSTRAINT graph_credential_secret_shape,
  DROP COLUMN client_state_secret_ciphertext,
  DROP COLUMN client_state_secret_nonce,
  DROP COLUMN client_state_secret_version,
  DROP COLUMN credential_secret_ciphertext,
  DROP COLUMN credential_secret_nonce,
  DROP COLUMN credential_secret_version,
  DROP COLUMN graph_client_id,
  DROP COLUMN graph_tenant_id,
  DROP COLUMN configuration_generation,
  ALTER COLUMN credential_secret_ref SET NOT NULL;
