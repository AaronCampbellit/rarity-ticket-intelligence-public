-- +goose Up
ALTER TABLE installation_setup
  ALTER COLUMN entra_tenant_id DROP NOT NULL,
  ALTER COLUMN entra_client_id DROP NOT NULL,
  ALTER COLUMN entra_secret_version DROP NOT NULL,
  ALTER COLUMN entra_secret_nonce DROP NOT NULL,
  ALTER COLUMN entra_secret_ciphertext DROP NOT NULL,
  ALTER COLUMN entra_redirect_url DROP NOT NULL;

ALTER TABLE installation_setup
  ADD COLUMN entra_state text NOT NULL DEFAULT 'not_connected'
    CHECK (entra_state IN (
      'not_connected', 'verification_required', 'connected', 'action_required'
    )),
  ADD COLUMN entra_version bigint NOT NULL DEFAULT 1 CHECK (entra_version > 0),
  ADD CONSTRAINT installation_setup_entra_group_check CHECK (
    (
      entra_tenant_id IS NULL AND
      entra_client_id IS NULL AND
      entra_secret_version IS NULL AND
      entra_secret_nonce IS NULL AND
      entra_secret_ciphertext IS NULL AND
      entra_redirect_url IS NULL
    ) OR (
      entra_tenant_id IS NOT NULL AND
      entra_client_id IS NOT NULL AND
      entra_secret_version IS NOT NULL AND
      entra_secret_nonce IS NOT NULL AND
      entra_secret_ciphertext IS NOT NULL AND
      entra_redirect_url IS NOT NULL
    )
  );

UPDATE installation_setup
SET entra_state = 'connected'
WHERE entra_tenant_id IS NOT NULL;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM installation_setup WHERE entra_tenant_id IS NULL
  ) THEN
    RAISE EXCEPTION
      'cannot restore required Entra columns while a local-only installation exists';
  END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE installation_setup
  DROP CONSTRAINT installation_setup_entra_group_check,
  DROP COLUMN entra_state,
  DROP COLUMN entra_version,
  ALTER COLUMN entra_tenant_id SET NOT NULL,
  ALTER COLUMN entra_client_id SET NOT NULL,
  ALTER COLUMN entra_secret_version SET NOT NULL,
  ALTER COLUMN entra_secret_nonce SET NOT NULL,
  ALTER COLUMN entra_secret_ciphertext SET NOT NULL,
  ALTER COLUMN entra_redirect_url SET NOT NULL;
