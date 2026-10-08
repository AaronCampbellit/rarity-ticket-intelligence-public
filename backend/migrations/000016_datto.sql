-- +goose Up
CREATE TABLE datto_connections (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  name text NOT NULL,
  credential_secret_ref text NOT NULL,
  sync_interval interval NOT NULL DEFAULT interval '15 minutes',
  cursor_secret_ref text,
  enabled boolean NOT NULL DEFAULT true,
  health_state text NOT NULL DEFAULT 'pending',
  last_completed_at timestamptz,
  last_error_code text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, name),
  CHECK (sync_interval > interval '0 seconds'),
  CHECK (health_state IN ('pending', 'healthy', 'degraded', 'failed', 'disabled'))
);

CREATE TABLE datto_site_mappings (
  id uuid PRIMARY KEY,
  connection_id uuid NOT NULL REFERENCES datto_connections(id),
  msp_id uuid NOT NULL,
  datto_site_id text NOT NULL,
  client_id uuid NOT NULL,
  mapped_at timestamptz NOT NULL,
  mapped_by uuid NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (connection_id, datto_site_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id)
);

CREATE TABLE datto_sync_runs (
  id uuid PRIMARY KEY,
  connection_id uuid NOT NULL REFERENCES datto_connections(id),
  kind text NOT NULL,
  state text NOT NULL,
  started_at timestamptz NOT NULL,
  completed_at timestamptz,
  assets_seen integer NOT NULL DEFAULT 0 CHECK (assets_seen >= 0),
  assets_changed integer NOT NULL DEFAULT 0 CHECK (assets_changed >= 0),
  rate_limit_state jsonb NOT NULL DEFAULT '{}'::jsonb,
  error_code text,
  CHECK (kind IN ('full', 'incremental', 'manual')),
  CHECK (state IN ('queued', 'running', 'succeeded', 'failed', 'rate_limited'))
);

CREATE TABLE datto_asset_snapshots (
  id uuid PRIMARY KEY,
  connection_id uuid NOT NULL REFERENCES datto_connections(id),
  msp_id uuid NOT NULL,
  client_id uuid,
  external_asset_id text NOT NULL,
  datto_site_id text NOT NULL,
  rarity_asset_id uuid,
  hostname text,
  serial_number text,
  mac_addresses jsonb NOT NULL DEFAULT '[]'::jsonb,
  source_payload_ref text NOT NULL,
  source_updated_at timestamptz,
  first_seen_at timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL,
  state text NOT NULL DEFAULT 'active',
  stale_since timestamptz,
  UNIQUE (connection_id, external_asset_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (state IN ('active', 'stale', 'inactive'))
);

CREATE TABLE datto_reconciliation_decisions (
  id uuid PRIMARY KEY,
  snapshot_id uuid NOT NULL REFERENCES datto_asset_snapshots(id),
  msp_id uuid NOT NULL,
  client_id uuid,
  rarity_asset_id uuid,
  decision text NOT NULL,
  reason text NOT NULL,
  decided_at timestamptz NOT NULL,
  decided_by uuid NOT NULL,
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (decision IN ('link', 'choose_rarity', 'choose_datto', 'keep_separate'))
);

CREATE TABLE datto_alert_incidents (
  id uuid PRIMARY KEY,
  connection_id uuid NOT NULL REFERENCES datto_connections(id),
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  external_alert_id text NOT NULL,
  fingerprint text NOT NULL,
  incident_id uuid NOT NULL,
  alert_state text NOT NULL,
  first_observed_at timestamptz NOT NULL,
  last_observed_at timestamptz NOT NULL,
  UNIQUE (connection_id, external_alert_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (alert_state IN ('active', 'cleared'))
);

CREATE INDEX datto_alert_fingerprint_idx
  ON datto_alert_incidents (connection_id, fingerprint, last_observed_at DESC);

CREATE TABLE datto_alert_observations (
  id uuid PRIMARY KEY,
  datto_alert_incident_id uuid NOT NULL REFERENCES datto_alert_incidents(id),
  external_observation_id text NOT NULL,
  observed_at timestamptz NOT NULL,
  state text NOT NULL,
  recovery boolean NOT NULL DEFAULT false,
  source_payload_ref text NOT NULL,
  UNIQUE (datto_alert_incident_id, external_observation_id),
  CHECK (state IN ('active', 'cleared')),
  CHECK (NOT recovery OR state = 'cleared')
);

-- +goose Down
DROP TABLE datto_alert_observations;
DROP TABLE datto_alert_incidents;
DROP TABLE datto_reconciliation_decisions;
DROP TABLE datto_asset_snapshots;
DROP TABLE datto_sync_runs;
DROP TABLE datto_site_mappings;
DROP TABLE datto_connections;
