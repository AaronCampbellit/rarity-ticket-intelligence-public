-- +goose Up
CREATE TABLE platform_metadata (
  key text PRIMARY KEY,
  value text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (key IN ('schema_contract_version'))
);

INSERT INTO platform_metadata (key, value)
VALUES ('schema_contract_version', '1');

-- +goose Down
DROP TABLE platform_metadata;
