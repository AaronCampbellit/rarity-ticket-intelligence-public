-- +goose Up
CREATE TABLE workflows (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  key text NOT NULL,
  name text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  priority integer NOT NULL DEFAULT 0,
  stable_order integer NOT NULL DEFAULT 0,
  fallback boolean NOT NULL DEFAULT false,
  effective_from timestamptz,
  effective_to timestamptz,
  conditions jsonb NOT NULL DEFAULT '{}'::jsonb,
  current_version bigint NOT NULL DEFAULT 1 CHECK (current_version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, client_id, key),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from)
);

CREATE UNIQUE INDEX workflows_global_key_idx
  ON workflows (msp_id, key)
  WHERE client_id IS NULL;

CREATE TABLE workflow_versions (
  workflow_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  definition jsonb NOT NULL,
  published_at timestamptz NOT NULL DEFAULT now(),
  published_by uuid NOT NULL,
  PRIMARY KEY (workflow_id, version),
  FOREIGN KEY (workflow_id, msp_id) REFERENCES workflows(id, msp_id)
);

-- +goose StatementBegin
CREATE FUNCTION reject_immutable_version_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'published version records are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workflow_versions_immutable
BEFORE UPDATE OR DELETE ON workflow_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE work_record_workflows (
  work_record_id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  workflow_id uuid NOT NULL,
  workflow_version bigint NOT NULL,
  selected_at timestamptz NOT NULL,
  matched_trace jsonb NOT NULL,
  FOREIGN KEY (work_record_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  FOREIGN KEY (workflow_id, workflow_version)
    REFERENCES workflow_versions(workflow_id, version)
);

CREATE TABLE business_calendars (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  key text NOT NULL,
  name text NOT NULL,
  timezone text NOT NULL,
  weekly_schedule jsonb NOT NULL,
  holidays jsonb NOT NULL DEFAULT '[]'::jsonb,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, client_id, key),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id)
);

CREATE UNIQUE INDEX business_calendars_global_key_idx
  ON business_calendars (msp_id, key)
  WHERE client_id IS NULL;

CREATE TABLE sla_policies (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  key text NOT NULL,
  name text NOT NULL,
  calendar_id uuid NOT NULL,
  conditions jsonb NOT NULL DEFAULT '{}'::jsonb,
  response_target_seconds integer NOT NULL CHECK (response_target_seconds > 0),
  resolution_target_seconds integer NOT NULL CHECK (resolution_target_seconds > 0),
  warning_percent integer NOT NULL DEFAULT 80 CHECK (warning_percent BETWEEN 1 AND 99),
  pause_states jsonb NOT NULL DEFAULT '[]'::jsonb,
  enabled boolean NOT NULL DEFAULT true,
  priority integer NOT NULL DEFAULT 0,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, client_id, key),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (calendar_id, msp_id) REFERENCES business_calendars(id, msp_id)
);

CREATE UNIQUE INDEX sla_policies_global_key_idx
  ON sla_policies (msp_id, key)
  WHERE client_id IS NULL;

CREATE TABLE work_record_slas (
  id uuid PRIMARY KEY,
  work_record_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  policy_id uuid NOT NULL,
  policy_version bigint NOT NULL,
  response_due_at timestamptz NOT NULL,
  resolution_due_at timestamptz NOT NULL,
  responded_at timestamptz,
  resolved_at timestamptz,
  paused_at timestamptz,
  paused_seconds bigint NOT NULL DEFAULT 0 CHECK (paused_seconds >= 0),
  state text NOT NULL DEFAULT 'running',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (work_record_id, policy_id),
  FOREIGN KEY (work_record_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  FOREIGN KEY (policy_id, msp_id) REFERENCES sla_policies(id, msp_id),
  CHECK (state IN ('running', 'paused', 'warning', 'breached', 'met', 'cancelled'))
);

CREATE INDEX work_record_slas_due_idx
  ON work_record_slas (msp_id, client_id, state, resolution_due_at);

CREATE TABLE notification_policies (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  key text NOT NULL,
  name text NOT NULL,
  event_type text NOT NULL,
  conditions jsonb NOT NULL DEFAULT '{}'::jsonb,
  channels jsonb NOT NULL,
  quiet_period_seconds integer NOT NULL DEFAULT 0 CHECK (quiet_period_seconds >= 0),
  critical_bypass boolean NOT NULL DEFAULT false,
  enabled boolean NOT NULL DEFAULT true,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, client_id, key),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id)
);

CREATE UNIQUE INDEX notification_policies_global_key_idx
  ON notification_policies (msp_id, key)
  WHERE client_id IS NULL;

CREATE TABLE notification_deliveries (
  id uuid PRIMARY KEY,
  policy_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid,
  work_record_id uuid,
  event_id uuid NOT NULL,
  channel text NOT NULL,
  recipient_ref text NOT NULL,
  content_classification text NOT NULL,
  state text NOT NULL DEFAULT 'pending',
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  delivered_at timestamptz,
  suppressed_at timestamptz,
  failure_code text,
  FOREIGN KEY (policy_id, msp_id) REFERENCES notification_policies(id, msp_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (channel IN ('in_app', 'email', 'teams', 'webhook')),
  CHECK (state IN ('pending', 'delivered', 'suppressed', 'failed'))
);

CREATE INDEX notification_deliveries_pending_idx
  ON notification_deliveries (next_attempt_at)
  WHERE state = 'pending';

CREATE TABLE saved_searches (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  owner_id uuid NOT NULL,
  name text NOT NULL,
  query jsonb NOT NULL,
  audience_type text NOT NULL DEFAULT 'private',
  audience_id uuid,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (owner_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (audience_type IN ('private', 'team', 'department', 'queue', 'msp'))
);

CREATE TABLE dashboards (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  owner_id uuid NOT NULL,
  name text NOT NULL,
  layout jsonb NOT NULL,
  audience_type text NOT NULL DEFAULT 'private',
  audience_id uuid,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (owner_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (audience_type IN ('private', 'team', 'department', 'queue', 'msp'))
);

CREATE TABLE knowledge_articles (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  display_id text NOT NULL,
  title text NOT NULL,
  state text NOT NULL DEFAULT 'draft',
  current_version bigint NOT NULL DEFAULT 1 CHECK (current_version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, client_id, display_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (state IN ('draft', 'published', 'archived'))
);

CREATE TABLE knowledge_article_versions (
  article_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  body text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  published_at timestamptz,
  published_by uuid,
  PRIMARY KEY (article_id, version),
  FOREIGN KEY (article_id, msp_id) REFERENCES knowledge_articles(id, msp_id)
);

CREATE TRIGGER knowledge_article_versions_immutable
BEFORE UPDATE OR DELETE ON knowledge_article_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

-- +goose Down
DROP TABLE knowledge_article_versions;
DROP TABLE knowledge_articles;
DROP TABLE dashboards;
DROP TABLE saved_searches;
DROP TABLE notification_deliveries;
DROP TABLE notification_policies;
DROP TABLE work_record_slas;
DROP TABLE sla_policies;
DROP TABLE business_calendars;
DROP TABLE work_record_workflows;
DROP TABLE workflow_versions;
DROP FUNCTION reject_immutable_version_mutation();
DROP TABLE workflows;
