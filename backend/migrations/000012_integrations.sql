-- +goose Up
CREATE TABLE service_api_keys (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  name text NOT NULL CHECK (length(btrim(name)) > 0),
  key_prefix text NOT NULL CHECK (length(key_prefix) BETWEEN 8 AND 32),
  token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
  capabilities text[] NOT NULL,
  data_scopes text[] NOT NULL,
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  rotated_from_id uuid REFERENCES service_api_keys(id),
  last_used_at timestamptz,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, key_prefix),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  CHECK (expires_at > created_at),
  CHECK (revoked_at IS NULL OR revoked_at >= created_at),
  CHECK (cardinality(capabilities) > 0),
  CHECK (cardinality(data_scopes) > 0)
);

CREATE INDEX service_api_keys_active_expiry_idx
  ON service_api_keys (msp_id, expires_at)
  WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE service_api_keys;
