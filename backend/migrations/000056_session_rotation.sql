-- +goose Up
ALTER TABLE sessions
  ADD COLUMN rotated_at timestamptz;

UPDATE sessions
SET rotated_at = created_at
WHERE rotated_at IS NULL;

ALTER TABLE sessions
  ALTER COLUMN rotated_at SET NOT NULL,
  ALTER COLUMN rotated_at SET DEFAULT now();

-- +goose Down
ALTER TABLE sessions
  DROP COLUMN rotated_at;
