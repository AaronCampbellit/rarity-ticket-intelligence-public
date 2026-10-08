-- +goose Up
CREATE TABLE proposal_acceptance_grants (
  id uuid PRIMARY KEY,
  proposal_version_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  token_sha256 char(64) NOT NULL UNIQUE,
  signer_name text NOT NULL,
  signer_email text NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (proposal_version_id, msp_id)
    REFERENCES proposal_versions(id, msp_id),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  CHECK (length(btrim(signer_name)) > 0),
  CHECK (length(btrim(signer_email)) > 0),
  CHECK (jsonb_typeof(evidence) = 'object')
);

CREATE INDEX proposal_acceptance_grants_lookup_idx
  ON proposal_acceptance_grants
    (token_sha256, proposal_version_id, msp_id, client_id)
  WHERE consumed_at IS NULL;

-- +goose Down
DROP TABLE proposal_acceptance_grants;
