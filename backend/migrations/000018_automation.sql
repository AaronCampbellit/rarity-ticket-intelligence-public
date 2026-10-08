-- +goose Up
CREATE TABLE connections (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  name text NOT NULL,
  kind text NOT NULL,
  endpoint_url text,
  secret_refs jsonb NOT NULL DEFAULT '{}'::jsonb,
  client_scopes uuid[] NOT NULL,
  capabilities text[] NOT NULL,
  data_scopes text[] NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  health_state text NOT NULL DEFAULT 'pending',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, name),
  CHECK (kind IN ('external_http', 'graph_mailbox', 'datto', 'teams', 'webhook')),
  CHECK (cardinality(client_scopes) > 0),
  CHECK (cardinality(capabilities) > 0),
  CHECK (cardinality(data_scopes) > 0),
  CHECK (health_state IN ('pending', 'healthy', 'degraded', 'failed', 'disabled'))
);

CREATE TABLE automation_definitions (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  name text NOT NULL,
  current_version bigint NOT NULL DEFAULT 0 CHECK (current_version >= 0),
  enabled boolean NOT NULL DEFAULT false,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, name)
);

CREATE TABLE automation_versions (
  id uuid PRIMARY KEY,
  automation_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  state text NOT NULL,
  trigger_event_type text NOT NULL,
  client_scopes uuid[] NOT NULL,
  capabilities text[] NOT NULL,
  definition_json jsonb NOT NULL,
  max_depth integer NOT NULL DEFAULT 8,
  published_at timestamptz,
  published_by uuid,
  UNIQUE (id, msp_id),
  UNIQUE (automation_id, version),
  FOREIGN KEY (automation_id, msp_id)
    REFERENCES automation_definitions(id, msp_id),
  CHECK (state IN ('draft', 'published', 'retired')),
  CHECK (cardinality(client_scopes) > 0),
  CHECK (cardinality(capabilities) > 0),
  CHECK (max_depth BETWEEN 1 AND 8),
  CHECK ((state = 'published') = (published_at IS NOT NULL AND published_by IS NOT NULL))
);

CREATE TRIGGER automation_versions_immutable
BEFORE UPDATE OR DELETE ON automation_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE automation_version_connections (
  automation_version_id uuid NOT NULL,
  connection_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  PRIMARY KEY (automation_version_id, connection_id),
  FOREIGN KEY (automation_version_id, msp_id)
    REFERENCES automation_versions(id, msp_id),
  FOREIGN KEY (connection_id, msp_id)
    REFERENCES connections(id, msp_id)
);

-- +goose Down
DROP TABLE automation_version_connections;
DROP TRIGGER automation_versions_immutable ON automation_versions;
DROP TABLE automation_versions;
DROP TABLE automation_definitions;
DROP TABLE connections;
