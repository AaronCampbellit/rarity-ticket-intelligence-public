-- +goose Up
CREATE TABLE forwarding_intake_connections (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  intake_address text NOT NULL,
  allowed_sender_domains text[] NOT NULL,
  max_message_bytes bigint NOT NULL CHECK (max_message_bytes > 0),
  rate_limit_per_minute integer NOT NULL CHECK (rate_limit_per_minute > 0),
  enabled boolean NOT NULL DEFAULT true,
  health_state text NOT NULL DEFAULT 'pending',
  last_received_at timestamptz,
  last_error_code text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, intake_address),
  CHECK (cardinality(allowed_sender_domains) > 0),
  CHECK (health_state IN ('pending', 'healthy', 'degraded', 'failed', 'disabled'))
);

CREATE TABLE inbound_events (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  source text NOT NULL,
  external_id text NOT NULL,
  received_at timestamptz NOT NULL,
  authentication_result text NOT NULL,
  raw_payload_ref text NOT NULL,
  normalized_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  correlation_fingerprint text,
  processing_state text NOT NULL DEFAULT 'received',
  quarantine_reason text,
  error_code text,
  work_record_id uuid,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, source, external_id),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  CHECK (source IN ('forwarded_email', 'direct_api', 'inbound_webhook', 'graph_email', 'datto_alert')),
  CHECK (authentication_result IN ('passed', 'failed')),
  CHECK (processing_state IN ('received', 'normalized', 'processed', 'quarantined', 'failed')),
  CHECK (
    (processing_state = 'quarantined' AND quarantine_reason IS NOT NULL)
    OR processing_state <> 'quarantined'
  )
);

CREATE INDEX inbound_events_processing_idx
  ON inbound_events (msp_id, processing_state, received_at);

CREATE TABLE inbound_event_attempts (
  id uuid PRIMARY KEY,
  inbound_event_id uuid NOT NULL REFERENCES inbound_events(id),
  attempted_at timestamptz NOT NULL,
  outcome text NOT NULL,
  error_code text,
  actor_type text NOT NULL,
  actor_id uuid NOT NULL,
  CHECK (outcome IN ('processed', 'failed', 'quarantined', 'replayed'))
);

-- +goose Down
DROP TABLE inbound_event_attempts;
DROP TABLE inbound_events;
DROP TABLE forwarding_intake_connections;
