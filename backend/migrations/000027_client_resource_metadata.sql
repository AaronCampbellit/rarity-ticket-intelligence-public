-- +goose Up
ALTER TABLE assets
  ADD COLUMN source_system text NOT NULL DEFAULT '',
  ADD COLUMN external_id text NOT NULL DEFAULT '',
  ADD COLUMN authority text NOT NULL DEFAULT 'discovered',
  ADD CONSTRAINT assets_authority_check
    CHECK (authority IN ('discovered', 'technician_confirmed'));

CREATE UNIQUE INDEX assets_external_identity_unique
  ON assets (msp_id, client_id, source_system, external_id)
  WHERE source_system <> '' AND external_id <> '';

ALTER TABLE services
  ADD COLUMN criticality text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE services
  DROP COLUMN criticality;

DROP INDEX assets_external_identity_unique;

ALTER TABLE assets
  DROP CONSTRAINT assets_authority_check,
  DROP COLUMN authority,
  DROP COLUMN external_id,
  DROP COLUMN source_system;
