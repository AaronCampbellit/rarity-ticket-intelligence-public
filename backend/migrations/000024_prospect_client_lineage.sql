-- +goose Up
ALTER TABLE client_organizations
  ADD COLUMN originating_prospect_id uuid,
  ADD UNIQUE (msp_id, originating_prospect_id),
  ADD FOREIGN KEY (originating_prospect_id, msp_id)
    REFERENCES prospects(id, msp_id);

-- +goose Down
ALTER TABLE client_organizations
  DROP COLUMN originating_prospect_id;
