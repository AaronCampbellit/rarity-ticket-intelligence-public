-- +goose Up
ALTER TABLE ai_action_proposals
  ADD COLUMN target_client_id uuid;

UPDATE ai_action_proposals
SET target_client_id = client_id
WHERE client_id IS NOT NULL;

ALTER TABLE ai_action_proposals
  ADD CONSTRAINT ai_action_proposals_target_client_msp_fkey
  FOREIGN KEY (target_client_id, msp_id)
  REFERENCES client_organizations(id, msp_id);

-- +goose Down
ALTER TABLE ai_action_proposals
  DROP CONSTRAINT ai_action_proposals_target_client_msp_fkey;

ALTER TABLE ai_action_proposals
  DROP COLUMN target_client_id;
