-- +goose Up
ALTER TABLE installation_setup
ADD COLUMN setup_version bigint NOT NULL DEFAULT 1
CHECK (setup_version > 0);

-- +goose Down
ALTER TABLE installation_setup
DROP COLUMN setup_version;
