-- +goose Up
CREATE TABLE setup_verification_evidence (
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  section text NOT NULL CHECK (section IN ('object_storage', 'backups')),
  state text NOT NULL
    CHECK (state IN ('ready_to_verify', 'verified', 'attention')),
  safe_code text NOT NULL,
  checked_at timestamptz NOT NULL,
  valid_until timestamptz NOT NULL,
  details jsonb NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(details) = 'object'),
  evidence_hash bytea,
  consumed_nonce text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  updated_by uuid NOT NULL,
  PRIMARY KEY (msp_id, section)
);

CREATE UNIQUE INDEX setup_verification_evidence_nonce_idx
ON setup_verification_evidence (msp_id, consumed_nonce)
WHERE consumed_nonce IS NOT NULL;

-- +goose Down
DROP TABLE setup_verification_evidence;
