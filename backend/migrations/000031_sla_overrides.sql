-- +goose Up
CREATE TABLE sla_overrides (
  id uuid PRIMARY KEY,
  sla_id uuid NOT NULL REFERENCES work_record_slas(id),
  work_record_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  work_record_version bigint NOT NULL CHECK (work_record_version > 0),
  sla_version_before bigint NOT NULL CHECK (sla_version_before > 0),
  sla_version_after bigint NOT NULL CHECK (sla_version_after > sla_version_before),
  response_warning_at_before timestamptz,
  response_warning_at_after timestamptz,
  response_due_at_before timestamptz,
  response_due_at_after timestamptz,
  resolution_warning_at_before timestamptz,
  resolution_warning_at_after timestamptz,
  resolution_due_at_before timestamptz,
  resolution_due_at_after timestamptz,
  reason text NOT NULL,
  overridden_at timestamptz NOT NULL,
  overridden_by uuid NOT NULL,
  FOREIGN KEY (work_record_id, msp_id, client_id)
    REFERENCES work_records(id, msp_id, client_id),
  CHECK (length(btrim(reason)) > 0),
  CHECK ((response_due_at_before IS NULL) = (response_due_at_after IS NULL)),
  CHECK ((resolution_due_at_before IS NULL) = (resolution_due_at_after IS NULL))
);

CREATE TRIGGER sla_overrides_immutable
BEFORE UPDATE OR DELETE ON sla_overrides
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE INDEX sla_overrides_work_record_idx
  ON sla_overrides (msp_id, client_id, work_record_id, overridden_at DESC);

-- +goose Down
DROP TABLE sla_overrides;
