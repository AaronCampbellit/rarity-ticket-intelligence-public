-- +goose Up
ALTER TABLE teams_connections
  ALTER COLUMN webhook_secret_ref DROP NOT NULL,
  ADD COLUMN webhook_secret_version integer,
  ADD COLUMN webhook_secret_nonce bytea,
  ADD COLUMN webhook_secret_ciphertext bytea,
  ADD COLUMN last_tested_at timestamptz,
  ADD CONSTRAINT teams_connections_secret_source_check CHECK (
    (
      webhook_secret_ref IS NOT NULL
      AND webhook_secret_ref LIKE 'env://RARITY_TEAMS_WEBHOOK_%'
      AND webhook_secret_version IS NULL
      AND webhook_secret_nonce IS NULL
      AND webhook_secret_ciphertext IS NULL
    )
    OR
    (
      webhook_secret_ref IS NULL
      AND webhook_secret_version IS NOT NULL
      AND webhook_secret_nonce IS NOT NULL
      AND webhook_secret_ciphertext IS NOT NULL
    )
  );

ALTER TABLE teams_delivery_attempts
  ADD COLUMN connection_version bigint NOT NULL DEFAULT 1
    CHECK (connection_version > 0);

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM teams_connections
    WHERE webhook_secret_version IS NOT NULL
       OR webhook_secret_nonce IS NOT NULL
       OR webhook_secret_ciphertext IS NOT NULL
  ) THEN
    RAISE EXCEPTION 'cannot roll back Teams connection lifecycle while encrypted GUI credentials exist';
  END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE teams_connections
  DROP CONSTRAINT teams_connections_secret_source_check,
  DROP COLUMN last_tested_at,
  DROP COLUMN webhook_secret_ciphertext,
  DROP COLUMN webhook_secret_nonce,
  DROP COLUMN webhook_secret_version,
  ALTER COLUMN webhook_secret_ref SET NOT NULL;

ALTER TABLE teams_delivery_attempts
  DROP COLUMN connection_version;
