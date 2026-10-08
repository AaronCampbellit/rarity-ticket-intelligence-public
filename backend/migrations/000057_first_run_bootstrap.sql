-- +goose Up
CREATE TABLE installation_bootstrap_tokens (
  token_hash bytea PRIMARY KEY,
  issued_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  CHECK (expires_at > issued_at),
  CHECK (expires_at <= issued_at + interval '15 minutes')
);

CREATE TABLE installation_setup (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  msp_id uuid NOT NULL UNIQUE REFERENCES msp_organizations(id),
  entra_tenant_id text NOT NULL,
  entra_client_id text NOT NULL,
  entra_secret_version integer NOT NULL,
  entra_secret_nonce bytea NOT NULL,
  entra_secret_ciphertext bytea NOT NULL,
  entra_redirect_url text NOT NULL,
  intake_config jsonb NOT NULL,
  object_storage_config jsonb NOT NULL,
  backup_config jsonb NOT NULL,
  completed_at timestamptz NOT NULL,
  completed_by uuid NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  FOREIGN KEY (completed_by, msp_id) REFERENCES technicians(id, msp_id)
);

-- Only one unconsumed bootstrap token may exist. Issuing a replacement first
-- invalidates the previous token through the operator command.
CREATE UNIQUE INDEX installation_bootstrap_one_active_idx
  ON installation_bootstrap_tokens ((true))
  WHERE consumed_at IS NULL;

-- +goose Down
DROP TABLE installation_setup;
DROP TABLE installation_bootstrap_tokens;
