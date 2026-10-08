-- +goose Up
CREATE TABLE calendar_domain_requests (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  operation text NOT NULL CHECK (btrim(operation) <> ''),
  idempotency_key text NOT NULL CHECK (btrim(idempotency_key) <> ''),
  request_fingerprint text NOT NULL CHECK (btrim(request_fingerprint) <> ''),
  response jsonb NOT NULL CHECK (jsonb_typeof(response) = 'object'),
  created_at timestamptz NOT NULL,
  UNIQUE NULLS NOT DISTINCT (msp_id, client_id, operation, idempotency_key),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id)
);

-- +goose StatementBegin
CREATE FUNCTION calendar_timezone_is_valid(candidate text)
RETURNS boolean
LANGUAGE sql
STABLE
AS $$
  SELECT candidate IS NOT NULL
     AND btrim(candidate) <> ''
     AND EXISTS (SELECT 1 FROM pg_timezone_names WHERE name = candidate);
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION calendar_recurrence_until_is_valid(candidate text)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
  parsed_date date;
  parsed_timestamp timestamptz;
BEGIN
  IF candidate ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN
    BEGIN
      parsed_date := candidate::date;
      RETURN to_char(parsed_date, 'YYYY-MM-DD') = candidate;
    EXCEPTION WHEN datetime_field_overflow OR invalid_datetime_format THEN
      RETURN false;
    END;
  END IF;
  IF candidate ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$' THEN
    BEGIN
      parsed_timestamp := candidate::timestamptz;
      RETURN parsed_timestamp IS NOT NULL;
    EXCEPTION WHEN datetime_field_overflow OR invalid_datetime_format THEN
      RETURN false;
    END;
  END IF;
  RETURN false;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION calendar_recurrence_rule_is_valid(rule jsonb)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
  weekday_count integer;
  distinct_weekday_count integer;
BEGIN
  IF rule IS NULL THEN
    RETURN true;
  END IF;
  IF jsonb_typeof(rule) <> 'object'
     OR NOT (rule ? 'frequency')
     OR jsonb_typeof(rule->'frequency') <> 'string'
     OR rule->>'frequency' NOT IN ('daily','weekly','monthly','yearly')
     OR rule - ARRAY['frequency','interval','weekdays','count','until'] <> '{}'::jsonb THEN
    RETURN false;
  END IF;
  IF NOT (rule ? 'interval')
     OR jsonb_typeof(rule->'interval') <> 'number'
     OR rule->>'interval' !~ '^[1-9][0-9]*$' THEN
    RETURN false;
  END IF;
  IF rule ? 'count'
     AND (jsonb_typeof(rule->'count') <> 'number'
       OR rule->>'count' !~ '^[1-9][0-9]*$') THEN
    RETURN false;
  END IF;
  IF rule ? 'until'
     AND (jsonb_typeof(rule->'until') <> 'string'
       OR NOT calendar_recurrence_until_is_valid(rule->>'until')) THEN
    RETURN false;
  END IF;
  IF rule ? 'count' AND rule ? 'until' THEN
    RETURN false;
  END IF;
  IF rule ? 'weekdays' THEN
    IF rule->>'frequency' <> 'weekly'
       OR jsonb_typeof(rule->'weekdays') <> 'array'
       OR jsonb_array_length(rule->'weekdays') = 0 THEN
      RETURN false;
    END IF;
    SELECT count(*), count(DISTINCT value)
      INTO weekday_count, distinct_weekday_count
    FROM jsonb_array_elements_text(rule->'weekdays');
    IF weekday_count <> distinct_weekday_count OR EXISTS (
      SELECT 1 FROM jsonb_array_elements_text(rule->'weekdays')
      WHERE value !~ '^[0-6]$'
    ) THEN
      RETURN false;
    END IF;
  END IF;
  RETURN true;
END;
$$;
-- +goose StatementEnd

CREATE TABLE calendar_event_projections (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  client_scope_key text GENERATED ALWAYS AS (
    CASE WHEN client_id IS NULL THEN 'global'
      ELSE 'client:' || client_id::text END
  ) STORED,
  source_type text NOT NULL,
  source_id uuid NOT NULL,
  event_role text NOT NULL,
  source_role_key text NOT NULL DEFAULT '',
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  title text NOT NULL CHECK (btrim(title) <> ''),
  starts_on date,
  ends_on date,
  starts_at timestamptz,
  ends_at timestamptz,
  timezone text,
  all_day boolean NOT NULL,
  scheduling_mode text NOT NULL
    CHECK (scheduling_mode IN ('fixed_block', 'effort_allocation', 'informational')),
  capacity_bearing boolean NOT NULL DEFAULT false,
  owner_id uuid,
  assignee_id uuid,
  planned_minutes bigint NOT NULL DEFAULT 0 CHECK (planned_minutes >= 0),
  filter_dimensions jsonb NOT NULL DEFAULT '{}'::jsonb,
  recurrence_rule jsonb,
  health_inputs jsonb NOT NULL DEFAULT '{}'::jsonb,
  health_state text NOT NULL DEFAULT 'on_track'
    CHECK (health_state IN ('blocked', 'overdue', 'at_risk', 'on_track', 'terminal')),
  health_reasons jsonb NOT NULL DEFAULT '[]'::jsonb,
  health_rule_version bigint NOT NULL DEFAULT 2 CHECK (health_rule_version > 0),
  health_evaluated_at timestamptz,
  terminal_state text NOT NULL DEFAULT 'active'
    CHECK (terminal_state IN ('active', 'completed', 'cancelled')),
  projection_revision bigint NOT NULL DEFAULT 1 CHECK (projection_revision > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, msp_id),
  UNIQUE (id, msp_id, client_id),
  UNIQUE (id, msp_id, client_scope_key),
  UNIQUE (msp_id, source_type, source_id, event_role, source_role_key),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (owner_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (assignee_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (source_type IN (
    'work_record', 'task', 'project', 'phase', 'milestone',
    'technician_schedule', 'pto', 'maintenance_window',
    'commercial_commitment', 'custom_date'
  )),
  CHECK (btrim(event_role) <> ''),
  CHECK (jsonb_typeof(filter_dimensions) = 'object'),
  CHECK (jsonb_typeof(health_inputs) = 'object'),
  CHECK (jsonb_typeof(health_reasons) = 'array'),
  CHECK (calendar_recurrence_rule_is_valid(recurrence_rule)),
  CHECK (
    (all_day AND starts_on IS NOT NULL AND starts_at IS NULL
      AND ends_at IS NULL AND timezone IS NULL)
    OR
    (NOT all_day AND starts_on IS NULL AND ends_on IS NULL
      AND starts_at IS NOT NULL AND calendar_timezone_is_valid(timezone))
  ),
  CHECK (ends_on IS NULL OR ends_on >= starts_on),
  CHECK (ends_at IS NULL OR ends_at > starts_at),
  CHECK (NOT capacity_bearing OR scheduling_mode <> 'informational'),
  CHECK (NOT capacity_bearing OR assignee_id IS NOT NULL),
  CHECK (NOT capacity_bearing OR planned_minutes > 0)
);

CREATE INDEX calendar_event_window_timed
  ON calendar_event_projections (msp_id, starts_at, ends_at)
  WHERE NOT all_day AND terminal_state = 'active';
CREATE INDEX calendar_event_window_all_day
  ON calendar_event_projections (msp_id, starts_on, ends_on)
  WHERE all_day AND terminal_state = 'active';
CREATE INDEX calendar_event_client_window_timed
  ON calendar_event_projections (msp_id, client_id, starts_at)
  WHERE NOT all_day AND terminal_state = 'active';
CREATE INDEX calendar_event_assignee_window
  ON calendar_event_projections (msp_id, assignee_id, starts_at)
  WHERE assignee_id IS NOT NULL AND NOT all_day AND terminal_state = 'active';
CREATE INDEX calendar_event_filters
  ON calendar_event_projections USING gin (filter_dimensions);

CREATE TABLE calendar_recurrence_exceptions (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  client_scope_key text NOT NULL,
  projection_id uuid NOT NULL,
  original_local_key text NOT NULL CHECK (btrim(original_local_key) <> ''),
  state text NOT NULL
    CHECK (state IN ('cancelled', 'rescheduled', 'overridden')),
  starts_on date,
  ends_on date,
  starts_at timestamptz,
  ends_at timestamptz,
  timezone text,
  all_day boolean,
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (msp_id, projection_id, original_local_key),
  FOREIGN KEY (projection_id, msp_id, client_scope_key)
    REFERENCES calendar_event_projections(id, msp_id, client_scope_key)
    ON DELETE CASCADE,
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (updated_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (client_scope_key = CASE WHEN client_id IS NULL THEN 'global'
    ELSE 'client:' || client_id::text END),
  CHECK (
    (state = 'cancelled' AND all_day IS NULL AND starts_on IS NULL
      AND ends_on IS NULL AND starts_at IS NULL AND ends_at IS NULL
      AND timezone IS NULL)
    OR
    (state <> 'cancelled' AND (
      (all_day AND starts_on IS NOT NULL AND starts_at IS NULL
        AND ends_at IS NULL AND timezone IS NULL)
      OR
      (NOT all_day AND starts_on IS NULL AND ends_on IS NULL
        AND starts_at IS NOT NULL AND calendar_timezone_is_valid(timezone))
    ))
  ),
  CHECK (ends_on IS NULL OR ends_on >= starts_on),
  CHECK (ends_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX calendar_recurrence_exceptions_projection_idx
  ON calendar_recurrence_exceptions (msp_id, projection_id, original_local_key);

CREATE TABLE calendar_dependencies (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid NOT NULL,
  predecessor_projection_id uuid NOT NULL,
  successor_projection_id uuid NOT NULL,
  relationship_type text NOT NULL,
  lead_lag_minutes integer NOT NULL DEFAULT 0,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (msp_id, client_id, predecessor_projection_id, successor_projection_id, relationship_type),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (predecessor_projection_id, msp_id, client_id)
    REFERENCES calendar_event_projections(id, msp_id, client_id) ON DELETE CASCADE,
  FOREIGN KEY (successor_projection_id, msp_id, client_id)
    REFERENCES calendar_event_projections(id, msp_id, client_id) ON DELETE CASCADE,
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (updated_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (predecessor_projection_id <> successor_projection_id),
  CHECK (relationship_type IN ('finish_to_start', 'start_to_start', 'finish_to_finish'))
);

CREATE INDEX calendar_dependencies_successor_idx
  ON calendar_dependencies (msp_id, client_id, successor_projection_id);
CREATE INDEX calendar_dependencies_predecessor_idx
  ON calendar_dependencies (msp_id, client_id, predecessor_projection_id);

CREATE TABLE calendar_scheduling_proposals (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  client_scope_key text GENERATED ALWAYS AS (
    CASE WHEN client_id IS NULL THEN 'global'
      ELSE 'client:' || client_id::text END
  ) STORED,
  actor_id uuid NOT NULL,
  state text NOT NULL DEFAULT 'previewed'
    CHECK (state IN ('previewed', 'approved', 'applied', 'rejected', 'expired', 'stale')),
  authorization_context_hash bytea NOT NULL CHECK (octet_length(authorization_context_hash) > 0),
  source_revision_bindings jsonb NOT NULL,
  conflict_policy_version bigint NOT NULL CHECK (conflict_policy_version > 0),
  requires_override boolean NOT NULL DEFAULT false,
  required_reason boolean NOT NULL DEFAULT false,
  approval_reason text NOT NULL DEFAULT '',
  expires_at timestamptz NOT NULL,
  approved_at timestamptz,
  approved_by uuid,
  applied_at timestamptz,
  applied_audit_correlation_id uuid,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, msp_id),
  UNIQUE (id, msp_id, client_id),
  UNIQUE (id, msp_id, client_scope_key),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (actor_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (approved_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (jsonb_typeof(source_revision_bindings) = 'object'),
  CHECK (expires_at > created_at),
  CHECK (NOT required_reason OR requires_override),
  CHECK (NOT requires_override OR required_reason),
  CHECK (state NOT IN ('approved', 'applied') OR
    NOT required_reason OR btrim(approval_reason) <> ''),
  CHECK ((approved_at IS NULL) = (approved_by IS NULL)),
  CHECK (state NOT IN ('approved', 'applied') OR approved_at IS NOT NULL),
  CHECK (state <> 'applied' OR (applied_at IS NOT NULL AND applied_audit_correlation_id IS NOT NULL))
);

CREATE INDEX calendar_scheduling_proposals_actor_idx
  ON calendar_scheduling_proposals (msp_id, actor_id, created_at DESC);
CREATE INDEX calendar_scheduling_proposals_expiry_idx
  ON calendar_scheduling_proposals (expires_at)
  WHERE state IN ('previewed', 'approved');

CREATE TABLE calendar_proposal_changes (
  id uuid PRIMARY KEY,
  proposal_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  client_id uuid,
  client_scope_key text NOT NULL,
  ordinal integer NOT NULL CHECK (ordinal > 0),
  source_type text NOT NULL,
  source_id uuid NOT NULL,
  event_role text NOT NULL,
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  change_type text NOT NULL
    CHECK (change_type IN ('schedule', 'reschedule', 'unschedule', 'recurrence_edit')),
  current_value jsonb NOT NULL,
  proposed_value jsonb NOT NULL,
  conflicts jsonb NOT NULL DEFAULT '[]'::jsonb,
  dependency_impact jsonb NOT NULL DEFAULT '[]'::jsonb,
  requires_override boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (proposal_id, ordinal),
  FOREIGN KEY (proposal_id, msp_id, client_scope_key)
    REFERENCES calendar_scheduling_proposals(id, msp_id, client_scope_key)
    ON DELETE CASCADE,
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (client_scope_key = CASE WHEN client_id IS NULL THEN 'global'
    ELSE 'client:' || client_id::text END),
  CHECK (source_type IN (
    'work_record', 'task', 'project', 'phase', 'milestone',
    'technician_schedule', 'pto', 'maintenance_window',
    'commercial_commitment', 'custom_date'
  )),
  CHECK (btrim(event_role) <> ''),
  CHECK (jsonb_typeof(current_value) = 'object'),
  CHECK (jsonb_typeof(proposed_value) = 'object'),
  CHECK (jsonb_typeof(conflicts) = 'array'),
  CHECK (jsonb_typeof(dependency_impact) = 'array')
);

CREATE INDEX calendar_proposal_changes_source_idx
  ON calendar_proposal_changes (msp_id, source_type, source_id, source_revision);

CREATE TABLE calendar_custom_date_fields (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  object_type text NOT NULL,
  internal_key text NOT NULL,
  label text NOT NULL CHECK (btrim(label) <> ''),
  value_kind text NOT NULL CHECK (value_kind IN ('date', 'timestamp')),
  event_role text NOT NULL CHECK (event_role ~ '^[a-z][a-z0-9_]{0,62}$'),
  category text NOT NULL CHECK (btrim(category) <> ''),
  color_category text NOT NULL
    CHECK (color_category IN ('neutral', 'blue', 'green', 'amber', 'red', 'purple')),
  calendar_mode text NOT NULL DEFAULT 'read_only'
    CHECK (calendar_mode IN ('read_only', 'schedulable')),
  scheduling_mode text NOT NULL DEFAULT 'informational'
    CHECK (scheduling_mode IN ('fixed_block', 'effort_allocation', 'informational')),
  capacity_bearing boolean NOT NULL DEFAULT false,
  timezone_source text
    CHECK (timezone_source IS NULL OR timezone_source IN ('object', 'client', 'msp', 'fixed')),
  fixed_timezone text,
  planned_effort_source text,
  lifecycle_state text NOT NULL DEFAULT 'active'
    CHECK (lifecycle_state IN ('active', 'inactive', 'archived')),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (id, msp_id, object_type),
  UNIQUE (msp_id, object_type, internal_key),
  UNIQUE (msp_id, object_type, event_role),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (updated_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry')),
  CHECK (internal_key ~ '^[a-z][a-z0-9_]{0,62}$'),
  CHECK ((value_kind = 'date' AND timezone_source IS NULL AND fixed_timezone IS NULL)
    OR (value_kind = 'timestamp' AND timezone_source IS NOT NULL)),
  CHECK ((timezone_source = 'fixed' AND calendar_timezone_is_valid(fixed_timezone))
    OR (timezone_source IS DISTINCT FROM 'fixed' AND fixed_timezone IS NULL)),
  CHECK (calendar_mode = 'schedulable' OR scheduling_mode = 'informational'),
  CHECK (NOT capacity_bearing OR
    (object_type IN ('work_record', 'task')
      AND calendar_mode = 'schedulable' AND value_kind = 'timestamp'
      AND scheduling_mode IN ('fixed_block', 'effort_allocation')
      AND planned_effort_source IS NOT NULL
      AND btrim(planned_effort_source) <> ''))
);

CREATE TABLE object_custom_date_values (
  id uuid PRIMARY KEY,
  field_id uuid NOT NULL,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  object_type text NOT NULL,
  object_id uuid NOT NULL,
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  date_value date,
  timestamp_value timestamptz,
  timezone text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid NOT NULL,
  UNIQUE (msp_id, field_id, object_type, object_id),
  FOREIGN KEY (field_id, msp_id, object_type)
    REFERENCES calendar_custom_date_fields(id, msp_id, object_type)
    ON DELETE CASCADE,
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (created_by, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (updated_by, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry')),
  CHECK ((date_value IS NOT NULL)::integer + (timestamp_value IS NOT NULL)::integer = 1),
  CHECK ((timestamp_value IS NULL AND timezone IS NULL)
    OR (timestamp_value IS NOT NULL AND calendar_timezone_is_valid(timezone)))
);

CREATE INDEX object_custom_date_values_object_idx
  ON object_custom_date_values (msp_id, client_id, object_type, object_id);

CREATE TABLE calendar_projection_cursors (
  consumer_key text NOT NULL,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  last_outbox_occurred_at timestamptz NOT NULL,
  last_event_id uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (msp_id, consumer_key),
  CHECK (btrim(consumer_key) <> '')
);

CREATE TABLE calendar_health_event_claims (
  source_event_id uuid NOT NULL,
  rule_version bigint NOT NULL CHECK (rule_version > 0),
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  fact_kind text NOT NULL CHECK (fact_kind IN (
    'projection', 'source_status', 'dependency', 'schedule',
    'availability', 'capacity', 'sla'
  )),
  processed_at timestamptz NOT NULL,
  PRIMARY KEY (source_event_id, rule_version)
);

CREATE TABLE calendar_live_changes (
  cursor bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  client_scope_key text NOT NULL,
  projection_id uuid,
  source_type text NOT NULL,
  source_id uuid NOT NULL,
  event_role text NOT NULL,
  change_type text NOT NULL
    CHECK (change_type IN ('upserted', 'removed', 'health_changed')),
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  occurred_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (client_scope_key = CASE WHEN client_id IS NULL THEN 'global'
    ELSE 'client:' || client_id::text END),
  FOREIGN KEY (projection_id, msp_id, client_scope_key)
    REFERENCES calendar_event_projections(id, msp_id, client_scope_key)
    ON DELETE SET NULL (projection_id)
);

CREATE INDEX calendar_live_changes_scope_cursor_idx
  ON calendar_live_changes (msp_id, cursor);
CREATE INDEX calendar_live_changes_client_cursor_idx
  ON calendar_live_changes (msp_id, client_id, cursor);

CREATE INDEX calendar_live_changes_source_revision_idx
  ON calendar_live_changes (msp_id, source_type, source_id, source_revision DESC, cursor DESC);

CREATE TABLE calendar_reminder_facts (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  client_id uuid,
  client_scope_key text NOT NULL,
  projection_id uuid NOT NULL,
  occurrence_key text NOT NULL DEFAULT '',
  reminder_kind text NOT NULL
    CHECK (reminder_kind IN ('schedule_start', 'due', 'renewal', 'license_expiry', 'maintenance_start')),
  remind_at timestamptz NOT NULL,
  state text NOT NULL DEFAULT 'pending'
    CHECK (state IN ('pending', 'claimed', 'sent', 'cancelled')),
  delivery_attempts integer NOT NULL DEFAULT 0 CHECK (delivery_attempts BETWEEN 0 AND 100),
  claimed_at timestamptz,
  delivered_at timestamptz,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (msp_id, projection_id, occurrence_key, reminder_kind, remind_at),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (client_scope_key = CASE WHEN client_id IS NULL THEN 'global'
    ELSE 'client:' || client_id::text END),
  FOREIGN KEY (projection_id, msp_id, client_scope_key)
    REFERENCES calendar_event_projections(id, msp_id, client_scope_key)
    ON DELETE CASCADE,
  CHECK (state <> 'sent' OR delivered_at IS NOT NULL)
);

CREATE INDEX calendar_reminder_facts_pending_idx
  ON calendar_reminder_facts (remind_at, id)
  WHERE state = 'pending';

ALTER TABLE work_records
  ADD COLUMN scheduled_starts_at timestamptz,
  ADD COLUMN scheduled_ends_at timestamptz,
  ADD COLUMN schedule_timezone text,
  ADD COLUMN scheduling_mode text NOT NULL DEFAULT 'informational',
  ADD COLUMN planned_effort_minutes bigint NOT NULL DEFAULT 0,
  ADD COLUMN due_on date,
  ADD COLUMN follow_up_on date,
  ADD COLUMN schedule_recurrence jsonb,
  ADD CONSTRAINT work_records_scheduling_mode_check
    CHECK (scheduling_mode IN ('fixed_block', 'effort_allocation', 'informational')),
  ADD CONSTRAINT work_records_scheduled_shape_check
    CHECK ((scheduled_starts_at IS NULL) = (schedule_timezone IS NULL)
      AND (schedule_timezone IS NULL OR calendar_timezone_is_valid(schedule_timezone))),
  ADD CONSTRAINT work_records_scheduled_end_check
    CHECK (scheduled_ends_at IS NULL OR
      (scheduled_starts_at IS NOT NULL AND scheduled_ends_at > scheduled_starts_at)),
  ADD CONSTRAINT work_records_planned_effort_check
    CHECK (planned_effort_minutes >= 0),
  ADD CONSTRAINT work_records_capacity_schedule_check
    CHECK (scheduling_mode = 'informational' OR
      (scheduled_starts_at IS NOT NULL AND schedule_timezone IS NOT NULL
       AND planned_effort_minutes > 0)),
  ADD CONSTRAINT work_records_schedule_recurrence_check
    CHECK (schedule_recurrence IS NULL OR
      (calendar_recurrence_rule_is_valid(schedule_recurrence)
       AND scheduled_starts_at IS NOT NULL AND schedule_timezone IS NOT NULL));

ALTER TABLE tasks
  ADD COLUMN scheduled_starts_at timestamptz,
  ADD COLUMN scheduled_ends_at timestamptz,
  ADD COLUMN schedule_timezone text,
  ADD COLUMN scheduling_mode text NOT NULL DEFAULT 'informational',
  ADD COLUMN due_on date,
  ADD COLUMN schedule_recurrence jsonb,
  ADD CONSTRAINT tasks_scheduling_mode_check
    CHECK (scheduling_mode IN ('fixed_block', 'effort_allocation', 'informational')),
  ADD CONSTRAINT tasks_scheduled_shape_check
    CHECK ((scheduled_starts_at IS NULL) = (schedule_timezone IS NULL)
      AND (schedule_timezone IS NULL OR calendar_timezone_is_valid(schedule_timezone))),
  ADD CONSTRAINT tasks_scheduled_end_check
    CHECK (scheduled_ends_at IS NULL OR
      (scheduled_starts_at IS NOT NULL AND scheduled_ends_at > scheduled_starts_at)),
  ADD CONSTRAINT tasks_capacity_schedule_check
    CHECK (scheduling_mode = 'informational' OR
      (scheduled_starts_at IS NOT NULL AND schedule_timezone IS NOT NULL
       AND estimate_minutes IS NOT NULL AND estimate_minutes > 0)),
  ADD CONSTRAINT tasks_schedule_recurrence_check
    CHECK (schedule_recurrence IS NULL OR
      (calendar_recurrence_rule_is_valid(schedule_recurrence)
       AND scheduled_starts_at IS NOT NULL AND schedule_timezone IS NOT NULL));

INSERT INTO role_capabilities (role_id, msp_id, capability)
SELECT role.id, role.msp_id, required.capability
FROM roles AS role
CROSS JOIN (
  VALUES
    ('calendar.read'),
    ('calendar.schedule'),
    ('calendar.workforce.manage'),
    ('calendar.commitment.manage'),
    ('calendar.policy.manage'),
    ('calendar.ai.recommend')
) AS required(capability)
WHERE role.key = 'global-admin'
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE calendar_domain_requests;
DELETE FROM role_capabilities AS capability
USING roles AS role
WHERE capability.role_id = role.id
  AND capability.msp_id = role.msp_id
  AND role.key = 'global-admin'
  AND capability.capability IN (
    'calendar.read', 'calendar.schedule', 'calendar.workforce.manage',
    'calendar.commitment.manage', 'calendar.policy.manage', 'calendar.ai.recommend'
  );

ALTER TABLE tasks
  DROP COLUMN schedule_recurrence,
  DROP COLUMN due_on,
  DROP COLUMN scheduling_mode,
  DROP COLUMN schedule_timezone,
  DROP COLUMN scheduled_ends_at,
  DROP COLUMN scheduled_starts_at;

ALTER TABLE work_records
  DROP COLUMN schedule_recurrence,
  DROP COLUMN follow_up_on,
  DROP COLUMN due_on,
  DROP COLUMN planned_effort_minutes,
  DROP COLUMN scheduling_mode,
  DROP COLUMN schedule_timezone,
  DROP COLUMN scheduled_ends_at,
  DROP COLUMN scheduled_starts_at;

DROP TABLE calendar_reminder_facts;
DROP TABLE calendar_live_changes;
DROP TABLE calendar_health_event_claims;
DROP TABLE calendar_projection_cursors;
DROP TABLE object_custom_date_values;
DROP TABLE calendar_custom_date_fields;
DROP TABLE calendar_proposal_changes;
DROP TABLE calendar_scheduling_proposals;
DROP TABLE calendar_dependencies;
DROP TABLE calendar_recurrence_exceptions;
DROP TABLE calendar_event_projections;
DROP FUNCTION calendar_recurrence_rule_is_valid(jsonb);
DROP FUNCTION calendar_recurrence_until_is_valid(text);
DROP FUNCTION calendar_timezone_is_valid(text);
