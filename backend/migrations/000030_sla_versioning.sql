-- +goose Up
ALTER TABLE sla_policies
  ADD COLUMN stable_order integer NOT NULL DEFAULT 0,
  ADD COLUMN fallback boolean NOT NULL DEFAULT false;

CREATE TABLE business_calendar_versions (
  calendar_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  timezone text NOT NULL,
  weekly_schedule jsonb NOT NULL,
  holidays jsonb NOT NULL,
  published_at timestamptz NOT NULL,
  published_by uuid,
  PRIMARY KEY (calendar_id, version),
  FOREIGN KEY (calendar_id, msp_id)
    REFERENCES business_calendars(id, msp_id)
);

INSERT INTO business_calendar_versions (
  calendar_id, msp_id, version, timezone, weekly_schedule, holidays,
  published_at, published_by
)
SELECT id, msp_id, version, timezone, weekly_schedule, holidays, now(), NULL
FROM business_calendars;

CREATE TRIGGER business_calendar_versions_immutable
BEFORE UPDATE OR DELETE ON business_calendar_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

CREATE TABLE sla_policy_versions (
  policy_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  calendar_id uuid NOT NULL,
  calendar_version bigint NOT NULL,
  conditions jsonb NOT NULL,
  response_target_seconds integer NOT NULL CHECK (response_target_seconds > 0),
  resolution_target_seconds integer NOT NULL CHECK (resolution_target_seconds > 0),
  warning_percent integer NOT NULL CHECK (warning_percent BETWEEN 1 AND 99),
  pause_states jsonb NOT NULL,
  enabled boolean NOT NULL,
  priority integer NOT NULL,
  stable_order integer NOT NULL,
  fallback boolean NOT NULL,
  published_at timestamptz NOT NULL,
  published_by uuid,
  PRIMARY KEY (policy_id, version),
  FOREIGN KEY (policy_id, msp_id)
    REFERENCES sla_policies(id, msp_id),
  FOREIGN KEY (calendar_id, calendar_version)
    REFERENCES business_calendar_versions(calendar_id, version)
);

INSERT INTO sla_policy_versions (
  policy_id, msp_id, version, calendar_id, calendar_version, conditions,
  response_target_seconds, resolution_target_seconds, warning_percent,
  pause_states, enabled, priority, stable_order, fallback,
  published_at, published_by
)
SELECT p.id, p.msp_id, p.version, p.calendar_id, c.version, p.conditions,
       p.response_target_seconds, p.resolution_target_seconds,
       p.warning_percent, p.pause_states, p.enabled, p.priority,
       p.stable_order, p.fallback, now(), NULL
FROM sla_policies p
JOIN business_calendars c ON c.id = p.calendar_id AND c.msp_id = p.msp_id;

CREATE TRIGGER sla_policy_versions_immutable
BEFORE UPDATE OR DELETE ON sla_policy_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_version_mutation();

ALTER TABLE work_record_slas
  ADD COLUMN calendar_id uuid,
  ADD COLUMN calendar_version bigint,
  ADD COLUMN response_warning_at timestamptz,
  ADD COLUMN resolution_warning_at timestamptz,
  ADD COLUMN pause_states jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN response_state text NOT NULL DEFAULT 'running',
  ADD COLUMN resolution_state text NOT NULL DEFAULT 'running',
  ADD COLUMN selection_trace jsonb NOT NULL DEFAULT '[]'::jsonb;

UPDATE work_record_slas wrs
SET calendar_id = p.calendar_id,
    calendar_version = c.version,
    response_warning_at = wrs.response_due_at,
    resolution_warning_at = wrs.resolution_due_at,
    pause_states = p.pause_states,
    response_state = CASE
      WHEN wrs.responded_at IS NOT NULL THEN 'met'
      WHEN wrs.state = 'cancelled' THEN 'cancelled'
      ELSE wrs.state
    END,
    resolution_state = CASE
      WHEN wrs.resolved_at IS NOT NULL THEN 'met'
      ELSE wrs.state
    END
FROM sla_policies p
JOIN business_calendars c ON c.id = p.calendar_id AND c.msp_id = p.msp_id
WHERE p.id = wrs.policy_id AND p.msp_id = wrs.msp_id;

ALTER TABLE work_record_slas
  ALTER COLUMN calendar_id SET NOT NULL,
  ALTER COLUMN calendar_version SET NOT NULL,
  ALTER COLUMN response_warning_at SET NOT NULL,
  ALTER COLUMN resolution_warning_at SET NOT NULL,
  ADD CONSTRAINT work_record_slas_policy_version_fk
    FOREIGN KEY (policy_id, policy_version)
    REFERENCES sla_policy_versions(policy_id, version),
  ADD CONSTRAINT work_record_slas_calendar_version_fk
    FOREIGN KEY (calendar_id, calendar_version)
    REFERENCES business_calendar_versions(calendar_id, version),
  ADD CONSTRAINT work_record_slas_response_state_check
    CHECK (response_state IN ('running', 'paused', 'warning', 'breached', 'met', 'cancelled')),
  ADD CONSTRAINT work_record_slas_resolution_state_check
    CHECK (resolution_state IN ('running', 'paused', 'warning', 'breached', 'met', 'cancelled'));

-- +goose Down
ALTER TABLE work_record_slas
  DROP CONSTRAINT work_record_slas_resolution_state_check,
  DROP CONSTRAINT work_record_slas_response_state_check,
  DROP CONSTRAINT work_record_slas_calendar_version_fk,
  DROP CONSTRAINT work_record_slas_policy_version_fk,
  DROP COLUMN selection_trace,
  DROP COLUMN resolution_state,
  DROP COLUMN response_state,
  DROP COLUMN pause_states,
  DROP COLUMN resolution_warning_at,
  DROP COLUMN response_warning_at,
  DROP COLUMN calendar_version,
  DROP COLUMN calendar_id;

DROP TRIGGER sla_policy_versions_immutable ON sla_policy_versions;
DROP TABLE sla_policy_versions;
DROP TRIGGER business_calendar_versions_immutable ON business_calendar_versions;
DROP TABLE business_calendar_versions;
ALTER TABLE sla_policies
  DROP COLUMN fallback,
  DROP COLUMN stable_order;
