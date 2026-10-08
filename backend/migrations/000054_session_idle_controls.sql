-- +goose Up
ALTER TABLE sessions
  ADD COLUMN idle_expires_at timestamptz,
  ADD COLUMN idle_timeout_seconds integer;

UPDATE sessions
SET idle_timeout_seconds = 1800,
    idle_expires_at = LEAST(last_seen_at + interval '30 minutes', expires_at);

ALTER TABLE sessions
  ALTER COLUMN idle_expires_at SET NOT NULL,
  ALTER COLUMN idle_timeout_seconds SET NOT NULL,
  ADD CONSTRAINT sessions_idle_timeout_positive
    CHECK (idle_timeout_seconds > 0),
  ADD CONSTRAINT sessions_idle_before_absolute
    CHECK (idle_expires_at <= expires_at);

-- +goose Down
ALTER TABLE sessions
  DROP CONSTRAINT sessions_idle_before_absolute,
  DROP CONSTRAINT sessions_idle_timeout_positive,
  DROP COLUMN idle_timeout_seconds,
  DROP COLUMN idle_expires_at;
