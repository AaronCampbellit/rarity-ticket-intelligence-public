-- +goose Up
ALTER TABLE locations
  ADD CONSTRAINT locations_lifecycle_state_check
    CHECK (lifecycle_state IN ('active', 'inactive')) NOT VALID;
ALTER TABLE contacts
  ADD CONSTRAINT contacts_lifecycle_state_check
    CHECK (lifecycle_state IN ('active', 'inactive')) NOT VALID;
ALTER TABLE assets
  ADD CONSTRAINT assets_lifecycle_state_check
    CHECK (lifecycle_state IN ('active', 'inactive')) NOT VALID;
ALTER TABLE services
  ADD CONSTRAINT services_lifecycle_state_check
    CHECK (lifecycle_state IN ('active', 'inactive')) NOT VALID;
ALTER TABLE contracts
  ADD CONSTRAINT contracts_lifecycle_state_check
    CHECK (lifecycle_state IN ('active', 'inactive')) NOT VALID;

ALTER TABLE locations VALIDATE CONSTRAINT locations_lifecycle_state_check;
ALTER TABLE contacts VALIDATE CONSTRAINT contacts_lifecycle_state_check;
ALTER TABLE assets VALIDATE CONSTRAINT assets_lifecycle_state_check;
ALTER TABLE services VALIDATE CONSTRAINT services_lifecycle_state_check;
ALTER TABLE contracts VALIDATE CONSTRAINT contracts_lifecycle_state_check;

CREATE INDEX contacts_active_location_dependencies_idx
  ON contacts (msp_id, client_id, location_id)
  WHERE lifecycle_state = 'active' AND location_id IS NOT NULL;
CREATE INDEX assets_active_location_dependencies_idx
  ON assets (msp_id, client_id, location_id)
  WHERE lifecycle_state = 'active' AND location_id IS NOT NULL;

-- +goose Down
DROP INDEX assets_active_location_dependencies_idx;
DROP INDEX contacts_active_location_dependencies_idx;

ALTER TABLE contracts DROP CONSTRAINT contracts_lifecycle_state_check;
ALTER TABLE services DROP CONSTRAINT services_lifecycle_state_check;
ALTER TABLE assets DROP CONSTRAINT assets_lifecycle_state_check;
ALTER TABLE contacts DROP CONSTRAINT contacts_lifecycle_state_check;
ALTER TABLE locations DROP CONSTRAINT locations_lifecycle_state_check;
