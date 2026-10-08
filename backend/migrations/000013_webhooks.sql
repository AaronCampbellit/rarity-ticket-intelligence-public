-- +goose Up
CREATE TABLE webhook_connections (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  name text NOT NULL CHECK (length(btrim(name)) > 0),
  direction text NOT NULL,
  endpoint_url text,
  secret_ref text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  event_types text[] NOT NULL DEFAULT '{}'::text[],
  retry_window interval NOT NULL DEFAULT interval '24 hours',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, name),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  CHECK (direction IN ('inbound', 'outbound', 'bidirectional')),
  CHECK (direction = 'inbound' OR endpoint_url IS NOT NULL),
  CHECK (retry_window > interval '0 seconds' AND retry_window <= interval '24 hours')
);

CREATE TABLE webhook_replay_claims (
  connection_id uuid NOT NULL,
  event_id text NOT NULL,
  claimed_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (connection_id, event_id),
  FOREIGN KEY (connection_id) REFERENCES webhook_connections(id) ON DELETE CASCADE,
  CHECK (expires_at > claimed_at)
);

CREATE INDEX webhook_replay_claims_expiry_idx
  ON webhook_replay_claims (expires_at);

CREATE TABLE webhook_delivery_attempts (
  id uuid PRIMARY KEY,
  connection_id uuid NOT NULL REFERENCES webhook_connections(id),
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  event_id text NOT NULL,
  attempt integer NOT NULL CHECK (attempt > 0),
  attempted_at timestamptz NOT NULL,
  state text NOT NULL CHECK (state IN ('succeeded', 'failed')),
  http_status integer CHECK (http_status BETWEEN 100 AND 599),
  error_code text,
  next_attempt_at timestamptz,
  UNIQUE (connection_id, event_id, attempt),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  CHECK (state = 'failed' OR error_code IS NULL)
);

CREATE INDEX webhook_delivery_attempts_history_idx
  ON webhook_delivery_attempts (connection_id, attempted_at DESC);

-- +goose Down
DROP TABLE webhook_delivery_attempts;
DROP TABLE webhook_replay_claims;
DROP TABLE webhook_connections;
