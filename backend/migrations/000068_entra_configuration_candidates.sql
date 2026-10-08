-- +goose Up
CREATE TABLE entra_configuration_candidates (
  msp_id uuid PRIMARY KEY REFERENCES msp_organizations(id),
  version bigint NOT NULL CHECK (version > 0),
  tenant_id text NOT NULL CHECK (btrim(tenant_id) <> ''),
  client_id text NOT NULL CHECK (btrim(client_id) <> ''),
  secret_version integer NOT NULL,
  secret_nonce bytea NOT NULL,
  secret_ciphertext bytea NOT NULL,
  redirect_url text NOT NULL CHECK (btrim(redirect_url) <> ''),
  staged_at timestamptz NOT NULL,
  staged_by uuid NOT NULL,
  FOREIGN KEY (staged_by, msp_id) REFERENCES technicians(id, msp_id)
);

-- +goose Down
DROP TABLE entra_configuration_candidates;
