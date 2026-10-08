-- +goose Up
ALTER TABLE webhook_connections
  ALTER COLUMN secret_ref DROP NOT NULL,
  ADD COLUMN secret_version integer,
  ADD COLUMN secret_nonce bytea,
  ADD COLUMN secret_ciphertext bytea,
  ADD CONSTRAINT webhook_connection_secret_shape CHECK (
    (
      secret_ref IS NOT NULL
      AND secret_ref LIKE 'env://RARITY_WEBHOOK_SECRET_%'
      AND secret_version IS NULL
      AND secret_nonce IS NULL
      AND secret_ciphertext IS NULL
    )
    OR (
      secret_ref IS NULL
      AND secret_version IS NOT NULL
      AND secret_nonce IS NOT NULL
      AND secret_ciphertext IS NOT NULL
    )
  );

-- +goose Down
ALTER TABLE webhook_connections
  DROP CONSTRAINT webhook_connection_secret_shape,
  DROP COLUMN secret_ciphertext,
  DROP COLUMN secret_nonce,
  DROP COLUMN secret_version,
  ALTER COLUMN secret_ref SET NOT NULL;
