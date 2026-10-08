-- +goose Up
ALTER TABLE datto_connections
  ADD COLUMN cursor_key_version integer,
  ADD COLUMN cursor_nonce bytea,
  ADD COLUMN cursor_ciphertext bytea,
  ADD COLUMN lease_until timestamptz,
  ADD COLUMN last_started_at timestamptz,
  ADD COLUMN current_run_id uuid REFERENCES datto_sync_runs(id),
  ADD CONSTRAINT datto_cursor_ciphertext_complete
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
  ADD CONSTRAINT datto_connection_lease_valid
    CHECK (
      lease_until IS NULL
      OR last_started_at IS NOT NULL
      AND lease_until > last_started_at
    );

CREATE INDEX datto_connections_due_idx
  ON datto_connections (
    COALESCE(lease_until, '-infinity'::timestamptz),
    COALESCE(last_completed_at, '-infinity'::timestamptz),
    id
  )
  WHERE enabled;

ALTER TABLE assets
  ADD COLUMN serial_number text NOT NULL DEFAULT '',
  ADD COLUMN mac_addresses jsonb NOT NULL DEFAULT '[]'::jsonb;

CREATE TABLE datto_reconciliation_candidates (
  id uuid PRIMARY KEY,
  snapshot_id uuid NOT NULL REFERENCES datto_asset_snapshots(id),
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  rarity_asset_id uuid NOT NULL,
  evidence jsonb NOT NULL,
  state text NOT NULL DEFAULT 'pending',
  created_at timestamptz NOT NULL,
  decided_at timestamptz,
  UNIQUE (snapshot_id, rarity_asset_id),
  FOREIGN KEY (rarity_asset_id, msp_id, client_id)
    REFERENCES assets(id, msp_id, client_id),
  CHECK (state IN ('pending', 'decided')),
  CHECK (
    (state = 'decided' AND decided_at IS NOT NULL)
    OR (state = 'pending' AND decided_at IS NULL)
  )
);

CREATE INDEX datto_reconciliation_candidates_pending_idx
  ON datto_reconciliation_candidates (msp_id, client_id, created_at, id)
  WHERE state = 'pending';

-- +goose Down
DROP TABLE datto_reconciliation_candidates;

ALTER TABLE assets
  DROP COLUMN mac_addresses,
  DROP COLUMN serial_number;

DROP INDEX datto_connections_due_idx;

ALTER TABLE datto_connections
  DROP CONSTRAINT datto_connection_lease_valid,
  DROP CONSTRAINT datto_cursor_ciphertext_complete,
  DROP COLUMN current_run_id,
  DROP COLUMN last_started_at,
  DROP COLUMN lease_until,
  DROP COLUMN cursor_ciphertext,
  DROP COLUMN cursor_nonce,
  DROP COLUMN cursor_key_version;
