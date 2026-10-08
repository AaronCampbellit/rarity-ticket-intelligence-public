-- +goose Up
ALTER TABLE proposal_acceptance_grants
  ALTER COLUMN client_id DROP NOT NULL;

-- +goose Down
ALTER TABLE proposal_acceptance_grants
  ALTER COLUMN client_id SET NOT NULL;
