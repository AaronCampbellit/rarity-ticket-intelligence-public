-- +goose Up
CREATE TABLE msp_organizations (
  id uuid PRIMARY KEY,
  display_id text NOT NULL UNIQUE,
  name text NOT NULL,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  CHECK (lifecycle_state IN ('active', 'inactive', 'archived', 'deleted'))
);

CREATE TABLE client_organizations (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  display_id text NOT NULL,
  name text NOT NULL,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  deleted_at timestamptz,
  deleted_by uuid,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, display_id),
  CHECK (lifecycle_state IN ('active', 'inactive', 'archived', 'deleted')),
  CHECK ((deleted_at IS NULL) = (deleted_by IS NULL)),
  CHECK (lifecycle_state = 'deleted' OR deleted_at IS NULL)
);

CREATE TABLE audit_ledger (
  id uuid PRIMARY KEY,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  actor_type text NOT NULL,
  actor_id uuid NOT NULL,
  action text NOT NULL,
  subject_type text NOT NULL,
  subject_id uuid NOT NULL,
  subject_version bigint NOT NULL CHECK (subject_version > 0),
  source text NOT NULL,
  reason text,
  correlation_id uuid NOT NULL,
  causation_id uuid,
  safe_diff jsonb NOT NULL DEFAULT '{}'::jsonb,
  authorization_context jsonb NOT NULL DEFAULT '{}'::jsonb,
  CHECK (client_id IS NULL OR msp_id IS NOT NULL),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id)
);

CREATE INDEX audit_ledger_scope_time_idx
  ON audit_ledger (msp_id, client_id, occurred_at DESC);
CREATE INDEX audit_ledger_subject_idx
  ON audit_ledger (msp_id, subject_type, subject_id, occurred_at DESC);
CREATE INDEX audit_ledger_correlation_idx
  ON audit_ledger (correlation_id);

-- +goose StatementBegin
CREATE FUNCTION reject_audit_ledger_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'audit ledger records are append-only';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_ledger_append_only
BEFORE UPDATE OR DELETE ON audit_ledger
FOR EACH ROW EXECUTE FUNCTION reject_audit_ledger_mutation();

CREATE TABLE event_outbox (
  event_id uuid PRIMARY KEY,
  event_type text NOT NULL,
  schema_version integer NOT NULL CHECK (schema_version > 0),
  occurred_at timestamptz NOT NULL,
  published_at timestamptz,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  actor_type text NOT NULL,
  actor_id uuid NOT NULL,
  subject_type text NOT NULL,
  subject_id uuid NOT NULL,
  subject_version bigint NOT NULL CHECK (subject_version > 0),
  correlation_id uuid NOT NULL,
  causation_id uuid,
  source text NOT NULL,
  data jsonb NOT NULL DEFAULT '{}'::jsonb,
  delivery_attempts integer NOT NULL DEFAULT 0 CHECK (delivery_attempts >= 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  last_error_code text,
  CHECK (client_id IS NULL OR msp_id IS NOT NULL),
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id)
);

CREATE INDEX event_outbox_pending_idx
  ON event_outbox (next_attempt_at, occurred_at)
  WHERE published_at IS NULL;
CREATE INDEX event_outbox_subject_idx
  ON event_outbox (msp_id, subject_type, subject_id, subject_version);
CREATE INDEX event_outbox_correlation_idx
  ON event_outbox (correlation_id);

-- +goose Down
DROP TABLE event_outbox;
DROP TABLE audit_ledger;
DROP FUNCTION reject_audit_ledger_mutation();
DROP TABLE client_organizations;
DROP TABLE msp_organizations;
