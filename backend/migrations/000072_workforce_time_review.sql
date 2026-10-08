-- +goose Up
CREATE TABLE labor_roles (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  key text NOT NULL CHECK (btrim(key) <> ''),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, key),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id)
);

CREATE TABLE labor_role_versions (
  id uuid PRIMARY KEY,
  labor_role_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  name text NOT NULL CHECK (btrim(name) <> ''),
  internal_cost_minor bigint NOT NULL CHECK (internal_cost_minor >= 0),
  bill_rate_minor bigint NOT NULL CHECK (bill_rate_minor >= 0),
  currency char(3) NOT NULL CHECK (currency = upper(currency)),
  effective_from timestamptz NOT NULL,
  effective_until timestamptz,
  enabled boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (labor_role_id, effective_from),
  FOREIGN KEY (labor_role_id, msp_id) REFERENCES labor_roles(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (effective_until IS NULL OR effective_until > effective_from)
);

CREATE INDEX labor_role_versions_effective_lookup
  ON labor_role_versions (msp_id, labor_role_id, effective_from DESC);

CREATE TRIGGER labor_role_versions_immutable
BEFORE UPDATE OR DELETE ON labor_role_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE ticket_timer_sessions (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  work_record_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  state text NOT NULL DEFAULT 'running'
    CHECK (state IN ('running', 'stopped', 'consumed', 'discarded')),
  started_at timestamptz NOT NULL,
  stopped_at timestamptz,
  duration_seconds bigint,
  consumed_time_entry_id uuid,
  idempotency_key text NOT NULL CHECK (btrim(idempotency_key) <> ''),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  UNIQUE (msp_id, technician_id, idempotency_key),
  FOREIGN KEY (work_record_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (updated_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (consumed_time_entry_id, msp_id, client_id)
    REFERENCES time_entries(id, msp_id, client_id),
  CHECK (
    (state = 'running' AND stopped_at IS NULL AND duration_seconds IS NULL
      AND consumed_time_entry_id IS NULL)
    OR
    (state IN ('stopped', 'discarded') AND stopped_at IS NOT NULL
      AND duration_seconds > 0 AND consumed_time_entry_id IS NULL)
    OR
    (state = 'consumed' AND stopped_at IS NOT NULL
      AND duration_seconds > 0 AND consumed_time_entry_id IS NOT NULL)
  )
);

CREATE UNIQUE INDEX ticket_timer_one_running_per_ticket_technician
  ON ticket_timer_sessions (msp_id, client_id, work_record_id, technician_id)
  WHERE state = 'running';

CREATE INDEX ticket_timer_ticket_history
  ON ticket_timer_sessions
    (msp_id, client_id, work_record_id, technician_id, created_at DESC);

ALTER TABLE time_entries
  ADD COLUMN labor_role_version_id uuid,
  ADD COLUMN internal_cost_minor bigint,
  ADD COLUMN bill_rate_minor bigint,
  ADD COLUMN rate_currency char(3),
  ADD COLUMN reversed_at timestamptz,
  ADD COLUMN reversed_by uuid,
  ADD COLUMN reversal_reason text,
  ADD COLUMN replacement_time_entry_id uuid,
  ADD CONSTRAINT time_entries_labor_role_version_fk
    FOREIGN KEY (labor_role_version_id, msp_id)
      REFERENCES labor_role_versions(id, msp_id),
  ADD CONSTRAINT time_entries_reversed_by_fk
    FOREIGN KEY (reversed_by, msp_id) REFERENCES technicians(id, msp_id),
  ADD CONSTRAINT time_entries_replacement_fk
    FOREIGN KEY (replacement_time_entry_id, msp_id, client_id)
      REFERENCES time_entries(id, msp_id, client_id),
  ADD CONSTRAINT time_entries_rate_snapshot_check
    CHECK (
      (labor_role_version_id IS NULL AND internal_cost_minor IS NULL
        AND bill_rate_minor IS NULL AND rate_currency IS NULL)
      OR
      (labor_role_version_id IS NOT NULL AND internal_cost_minor >= 0
        AND bill_rate_minor >= 0 AND rate_currency = upper(rate_currency))
    ),
  ADD CONSTRAINT time_entries_reversal_evidence_check
    CHECK (
      (reversed_at IS NULL AND reversed_by IS NULL
        AND reversal_reason IS NULL AND replacement_time_entry_id IS NULL)
      OR
      (reversed_at IS NOT NULL AND reversed_by IS NOT NULL
        AND btrim(reversal_reason) <> '' AND replacement_time_entry_id IS NOT NULL)
    );

CREATE TABLE time_entry_amendments (
  id uuid PRIMARY KEY,
  time_entry_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  prior_version bigint NOT NULL CHECK (prior_version > 0),
  resulting_version bigint NOT NULL CHECK (resulting_version = prior_version + 1),
  before_values jsonb NOT NULL CHECK (jsonb_typeof(before_values) = 'object'),
  after_values jsonb NOT NULL CHECK (jsonb_typeof(after_values) = 'object'),
  reason text NOT NULL CHECK (btrim(reason) <> ''),
  amended_at timestamptz NOT NULL,
  amended_by uuid NOT NULL,
  UNIQUE (time_entry_id, resulting_version),
  FOREIGN KEY (time_entry_id, msp_id, client_id)
    REFERENCES time_entries(id, msp_id, client_id),
  FOREIGN KEY (amended_by, msp_id) REFERENCES technicians(id, msp_id)
);

CREATE TRIGGER time_entry_amendments_immutable
BEFORE UPDATE OR DELETE ON time_entry_amendments
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

-- +goose Down
DROP TRIGGER time_entry_amendments_immutable ON time_entry_amendments;
DROP TABLE time_entry_amendments;

ALTER TABLE time_entries
  DROP CONSTRAINT time_entries_reversal_evidence_check,
  DROP CONSTRAINT time_entries_rate_snapshot_check,
  DROP CONSTRAINT time_entries_replacement_fk,
  DROP CONSTRAINT time_entries_reversed_by_fk,
  DROP CONSTRAINT time_entries_labor_role_version_fk,
  DROP COLUMN replacement_time_entry_id,
  DROP COLUMN reversal_reason,
  DROP COLUMN reversed_by,
  DROP COLUMN reversed_at,
  DROP COLUMN rate_currency,
  DROP COLUMN bill_rate_minor,
  DROP COLUMN internal_cost_minor,
  DROP COLUMN labor_role_version_id;

DROP TABLE ticket_timer_sessions;
DROP TRIGGER labor_role_versions_immutable ON labor_role_versions;
DROP TABLE labor_role_versions;
DROP TABLE labor_roles;
