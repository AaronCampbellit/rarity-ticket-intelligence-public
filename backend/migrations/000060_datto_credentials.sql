-- +goose Up
ALTER TABLE datto_connections
  ALTER COLUMN credential_secret_ref DROP NOT NULL,
  ADD COLUMN api_url text,
  ADD COLUMN configuration_generation bigint NOT NULL DEFAULT 1
    CHECK (configuration_generation > 0),
  ADD COLUMN credential_secret_version integer,
  ADD COLUMN credential_secret_nonce bytea,
  ADD COLUMN credential_secret_ciphertext bytea,
  ADD CONSTRAINT datto_credential_secret_shape CHECK (
    (
      credential_secret_ref IS NOT NULL
      AND credential_secret_ref LIKE 'env://RARITY_DATTO_CREDENTIAL_%'
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
  );

-- +goose Down
ALTER TABLE datto_connections
  DROP CONSTRAINT datto_credential_secret_shape,
  DROP COLUMN credential_secret_ciphertext,
  DROP COLUMN credential_secret_nonce,
  DROP COLUMN credential_secret_version,
  DROP COLUMN configuration_generation,
  DROP COLUMN api_url,
  ALTER COLUMN credential_secret_ref SET NOT NULL;
