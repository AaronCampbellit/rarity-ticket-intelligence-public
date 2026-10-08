-- +goose Up
ALTER TABLE graph_delta_cursors
  ADD COLUMN cursor_key_version integer,
  ADD COLUMN cursor_nonce bytea,
  ADD COLUMN cursor_ciphertext bytea,
  ADD COLUMN lease_until timestamptz,
  ADD COLUMN last_attempt_at timestamptz,
  ADD CONSTRAINT graph_delta_cursor_ciphertext_complete
    CHECK (
      (
        cursor_key_version IS NULL
        AND cursor_nonce IS NULL
        AND cursor_ciphertext IS NULL
      )
      OR (
        cursor_key_version IS NOT NULL
        AND cursor_key_version > 0
        AND cursor_nonce IS NOT NULL
        AND octet_length(cursor_nonce) > 0
        AND cursor_ciphertext IS NOT NULL
        AND octet_length(cursor_ciphertext) > 0
      )
    ),
  ADD CONSTRAINT graph_delta_cursor_lease_valid
    CHECK (
      lease_until IS NULL
      OR last_attempt_at IS NOT NULL
      AND lease_until > last_attempt_at
    );

CREATE INDEX graph_delta_cursors_due_idx
  ON graph_delta_cursors (
    COALESCE(lease_until, '-infinity'::timestamptz),
    COALESCE(last_completed_at, '-infinity'::timestamptz),
    connection_id,
    folder_id
  );

-- +goose Down
DROP INDEX graph_delta_cursors_due_idx;

ALTER TABLE graph_delta_cursors
  DROP CONSTRAINT graph_delta_cursor_lease_valid,
  DROP CONSTRAINT graph_delta_cursor_ciphertext_complete,
  DROP COLUMN last_attempt_at,
  DROP COLUMN lease_until,
  DROP COLUMN cursor_ciphertext,
  DROP COLUMN cursor_nonce,
  DROP COLUMN cursor_key_version;
