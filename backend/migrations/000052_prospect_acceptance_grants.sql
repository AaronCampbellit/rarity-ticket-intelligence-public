-- +goose Up
ALTER TABLE proposal_acceptance_grants
  ALTER COLUMN client_id DROP NOT NULL,
  ADD COLUMN issued_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN issued_by uuid NOT NULL REFERENCES technicians(id);

-- +goose Down
ALTER TABLE proposal_acceptance_grants
  DROP COLUMN issued_by,
  DROP COLUMN issued_at,
  ALTER COLUMN client_id SET NOT NULL;
