-- +goose Up
ALTER TABLE time_entries
  ADD COLUMN approval_state text NOT NULL DEFAULT 'pending',
  ADD COLUMN approved_at timestamptz,
  ADD COLUMN approved_by uuid,
  ADD CONSTRAINT time_entries_approval_state_check
    CHECK (approval_state IN ('pending', 'approved', 'rejected')),
  ADD CONSTRAINT time_entries_approval_evidence_check
    CHECK (
      (approval_state = 'approved' AND approved_at IS NOT NULL AND approved_by IS NOT NULL)
      OR (approval_state <> 'approved' AND approved_at IS NULL AND approved_by IS NULL)
    ),
  ADD CONSTRAINT time_entries_approval_actor_fk
    FOREIGN KEY (approved_by, msp_id) REFERENCES technicians(id, msp_id);

CREATE TABLE time_entry_approval_decisions (
  id uuid PRIMARY KEY,
  time_entry_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  time_entry_version bigint NOT NULL CHECK (time_entry_version > 1),
  decision text NOT NULL,
  reason text NOT NULL CHECK (btrim(reason) <> ''),
  decided_at timestamptz NOT NULL,
  decided_by uuid NOT NULL,
  UNIQUE (time_entry_id, time_entry_version),
  FOREIGN KEY (time_entry_id, msp_id, client_id)
    REFERENCES time_entries(id, msp_id, client_id),
  FOREIGN KEY (decided_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (decision IN ('approved', 'rejected'))
);

CREATE TRIGGER time_entry_approval_decisions_immutable
BEFORE UPDATE OR DELETE ON time_entry_approval_decisions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE billing_exports (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  from_at timestamptz NOT NULL,
  through_at timestamptz NOT NULL,
  entry_count integer NOT NULL CHECK (entry_count >= 0),
  csv_sha256 bytea NOT NULL CHECK (octet_length(csv_sha256) = 32),
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  UNIQUE (id, msp_id, client_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (through_at > from_at)
);

CREATE TRIGGER billing_exports_immutable
BEFORE UPDATE OR DELETE ON billing_exports
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE billing_export_entries (
  export_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  time_entry_id uuid NOT NULL,
  time_entry_version bigint NOT NULL CHECK (time_entry_version > 0),
  work_record_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  duration_seconds integer NOT NULL CHECK (duration_seconds > 0),
  billable boolean NOT NULL,
  approved boolean NOT NULL,
  PRIMARY KEY (export_id, time_entry_id),
  FOREIGN KEY (export_id, msp_id, client_id)
    REFERENCES billing_exports(id, msp_id, client_id),
  FOREIGN KEY (time_entry_id, msp_id, client_id)
    REFERENCES time_entries(id, msp_id, client_id)
);

CREATE TRIGGER billing_export_entries_immutable
BEFORE UPDATE OR DELETE ON billing_export_entries
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

-- +goose Down
DROP TRIGGER billing_export_entries_immutable ON billing_export_entries;
DROP TABLE billing_export_entries;
DROP TRIGGER billing_exports_immutable ON billing_exports;
DROP TABLE billing_exports;
DROP TRIGGER time_entry_approval_decisions_immutable ON time_entry_approval_decisions;
DROP TABLE time_entry_approval_decisions;
ALTER TABLE time_entries
  DROP CONSTRAINT time_entries_approval_actor_fk,
  DROP CONSTRAINT time_entries_approval_evidence_check,
  DROP CONSTRAINT time_entries_approval_state_check,
  DROP COLUMN approved_by,
  DROP COLUMN approved_at,
  DROP COLUMN approval_state;
